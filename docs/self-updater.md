# GitHub Releases updater exploration

## Recommendation

Keep release discovery, version policy, download verification, and UI state in Go.
Install complete platform packages through platform-specific code after the app
has exited. Bufflehead is a Godot executable plus a Go shared library and other
resources: replacing os.Executable() alone would update the wrong component or
leave an incompatible mixture of files.

The app implements **check → select → download → verify → install** (install
in place on macOS; hand-off elsewhere). On
startup (and from *Bufflehead → Check for Updates…*) it looks up the latest
stable release off the main thread; if one is newer, a modal shows the release
notes and offers to download it. The download is verified (size + SHA-256) and
staged under the user cache dir (`~/Library/Caches/Bufflehead/updates` on
macOS) with its real name. On macOS the modal then offers **Install and
Restart** (below); on Windows it offers to open the installer, on Linux the
containing folder. The prototype reuses
golang.org/x/mod/semver already in go.mod; there are no new dependencies.

The flow is a state machine, `updater.State` (idle/checking/up-to-date/
available/downloading/ready/failed), rendered by `internal/ui/update_modal.go`.
Automatic checks are silent unless an update exists; manual checks show every
outcome. Headless runs (the integration harness) skip the startup check.

## Existing release contract

Verified against the live v0.29.0 release on 2026-09-17:

| Target | Asset name pattern | Installation strategy |
| --- | --- | --- |
| macOS Intel/Apple Silicon | Bufflehead-VERSION-macOS.dmg | Replace the entire signed .app |
| Windows amd64 | Bufflehead-VERSION-Setup.exe | Run the existing signed Inno Setup installer |
| Linux amd64 | Bufflehead-VERSION-x86_64.AppImage | Replace the outer AppImage |
| Linux arm64 | Bufflehead-VERSION-aarch64.AppImage | Replace the outer AppImage |

Every current asset has a GitHub SHA-256 digest. Missing digests, missing assets,
duplicate matches, invalid versions, and unsupported target platforms produce
errors. Stable checks follow GET /repos/kyleparisi/bufflehead/releases/latest,
compare semantic versions, and reject downgrades, drafts, and prereleases.
The endpoint follows GitHub's designated latest release, not a scan for the
numerically highest tag. The app's own version is `application/short_version`
from `graphics/export_presets.cfg`, embedded at build time, so bumping the
presets for a release is the only version change needed.

GitHub metadata and HTTPS form the prototype's trust boundary. Its SHA-256 check
protects against incomplete or mismatched downloads, not repository compromise.
Before enabling installation, verify platform signatures and expected publisher;
consider a separately signed release manifest for Linux and stronger provenance.
Never ship a GitHub token in the desktop app.

## Try it

```sh
go test -race ./internal/updater ./cmd/update-check

# In the app: pretend to be an older release to see the update modal.
BUFFLEHEAD_VERSION=0.28.0 gd run
# Opt out of the startup check:
BUFFLEHEAD_NO_UPDATE_CHECK=1 gd run

# Without Godot: read-only live query, optionally staging the asset.
go run ./cmd/update-check -current 0.28.0
go run ./cmd/update-check -current 0.28.0 -os linux -arch arm64 -stage-dir /tmp
```

The control API exposes `POST /check-updates`, `POST /download-update` and
`POST /install-update` (the menu item and the modal's buttons); `/state` reports
the flow under `"update"`.

To exercise a real install without touching `/Applications`, build a bundle
(`GOOS=macos gd build`), copy `releases/darwin/universal/Bufflehead.app` into a
scratch folder, and run its binary with `BUFFLEHEAD_VERSION=0.28.0`. Installing
replaces that copy with the signed release and reopens it. The real-tool
verification can also run on its own:
`BUFFLEHEAD_TEST_DMG=… BUFFLEHEAD_TEST_TAG=v0.29.0 go test -run RealDMG ./internal/updater`.

`Stage` writes a private random name; the app's `StageNamed` renames the verified
file to the asset's own name (replacing an older copy) so the OS recognises it.
Failed downloads
remove their temporary file. Downloads have a 1 GiB cap and verify exact byte
length as well as SHA-256; check requests have a 20 second deadline and the default
HTTP client has a 10 minute download timeout. Injected clients own their timeout.
Tests use an injected HTTP transport and temporary directories, with no GitHub
network access, Godot, DuckDB, or installed app required.

## Completing install and restart

1. Done: manual “Check for Updates…” plus a startup check, network work off the
   Godot thread, state machine (`updater.State`) rendered by the UI. Still to
   do: throttle startup checks (daily, ETag cache); preserve query/session state
   across the restart.
2. macOS — done (`internal/updater/install.go`). While the app still runs,
   `PrepareInstall` resolves the running bundle (refusing anything whose bundle
   ID isn't `com.kyleparisi.bufflehead`, so a dev `godot.app` is never touched,
   and refusing disk-image / App Translocation locations), checks the parent
   folder is writable, mounts the DMG read-only at a private mount point,
   `ditto`s the app to `.Bufflehead-update.app` beside the installed one, and
   verifies **that copy**: `codesign --verify --deep --strict` against a
   Developer ID + Team ID 63GMD6U4J2 requirement, `spctl --assess` (notarized),
   bundle ID, and `CFBundleShortVersionString` equal to the release tag.
   (Verifying the copy rather than the mounted image takes ~9s instead of ~26s.)
   `Launch` then starts a detached `/bin/sh` helper (Apple-signed, so nothing new
   to sign) that waits up to 60s for the app to exit, renames the installed app
   to `.Bufflehead-backup.app`, renames the copy into place (restoring the backup
   if that fails), and `open`s it. The relaunched app's `FinishInstall` removes
   the backup and the downloaded DMG. The helper logs to
   `~/Library/Caches/Bufflehead/updates/install.log`. Any failure before the
   swap leaves the installed app untouched and the modal offers Try Again or
   Open Installer. Not handled yet: a crash of the new version (the backup stays
   for manual rollback, but nothing restores it automatically).
3. Windows: verify Authenticode and the expected publisher, then use the existing
   Inno Setup installer after exit. Inspect/test its close-app and restart behavior
   rather than overwriting a loaded DLL. Respect the existing installation path
   and privilege model. Do not assume the installer gives transactional rollback.
4. Linux: use APPIMAGE to locate the outer package, not /proc/self/exe or the
   mounted Godot executable. Verify the signed manifest, stage beside that file,
   preserve executable mode, rename with backup after exit, and relaunch. For
   package-manager and unpacked installs, direct users to their installation method.
5. Keep a transaction journal and backup until the new app confirms successful
   startup. Recover interrupted installs on next launch. Serialize update attempts;
   distinguish replacement failure, rollback failure, and failed startup. Test
   two installed versions on each OS, locked files, permission failures, disk-full,
   interrupted download/install, and startup failure before claiming self-update.

The two-rename backup/install sequence is not crash-atomic; the helper's journal
and recovery path must explicitly handle the gap. No such guarantee is implemented
by this discovery/staging prototype.

## Alternatives

[creativeprojects/go-selfupdate](https://github.com/creativeprojects/go-selfupdate)
has GitHub discovery, version checks, validation, and executable replacement. It
is a useful option for standalone Go tools, but its executable-replacement model
does not directly install this app's full Godot bundle. A small app-specific Go
layer fits the existing release assets without adding a second packaging system.

[GitHub release API](https://docs.github.com/en/rest/releases/releases) documents
latest-release metadata and asset digests. Production automatic checks should
cache metadata/ETags, throttle to daily with backoff, honor rate-limit responses,
and treat offline checks as nonfatal. Publish a release as latest only after all
platform assets are uploaded and validated.
