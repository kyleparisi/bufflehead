# GitHub Releases updater exploration

## Recommendation

Keep release discovery, version policy, download verification, and UI state in Go.
Install complete platform packages through platform-specific code after the app
has exited. Bufflehead is a Godot executable plus a Go shared library and other
resources: replacing os.Executable() alone would update the wrong component or
leave an incompatible mixture of files.

This branch prototypes **check → select → download → verify**. It does not yet
install, restart, schedule background checks, or add app UI. No production startup
behavior changes. The prototype reuses golang.org/x/mod/semver already in go.mod;
there are no new dependencies.

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
numerically highest tag. Current versions must be supplied by the caller;
production builds should embed a single version shared with export presets.

GitHub metadata and HTTPS form the prototype's trust boundary. Its SHA-256 check
protects against incomplete or mismatched downloads, not repository compromise.
Before enabling installation, verify platform signatures and expected publisher;
consider a separately signed release manifest for Linux and stronger provenance.
Never ship a GitHub token in the desktop app.

## Try it

```sh
go test -race ./internal/updater ./cmd/update-check
# Read-only live query (explicit older version demonstrates discovery):
go run ./cmd/update-check -current 0.28.0
# Use an existing directory; leaves a private verified file and prints its path:
go run ./cmd/update-check -current 0.28.0 -os linux -arch arm64 -stage-dir /tmp
```

Staged files use random names, are not executable, and are not auto-installed.
The caller must delete them after use. A future installer adapter must preserve
the appropriate extension when preparing an installer for launch. Failed downloads
remove their temporary file. Downloads have a 1 GiB cap and verify exact byte
length as well as SHA-256; check requests have a 20 second deadline and the default
HTTP client has a 10 minute download timeout. Injected clients own their timeout.
Tests use an injected HTTP transport and temporary directories, with no GitHub
network access, Godot, DuckDB, or installed app required.

## Completing install and restart

1. Expose a manual “Check for updates” action first. Run network work off the Godot
   thread. Represent idle/checking/available/downloading/ready/error explicitly
   and render UI from state. Show release notes and request “Install and restart”
   only after the download verifies. Preserve query/session state before exit.
2. macOS: mount the DMG read-only, verify the contained app's signature, expected
   Team ID (currently 63GMD6U4J2), and notarization policy. Copy the whole bundle
   to a staging sibling of the installed app so final renames stay on the same
   filesystem. A separately signed Go helper waits for exit, backs up the old
   bundle, renames the new bundle into place, and launches it. Handle read-only
   mounted DMGs and unwritable /Applications with a manual-install fallback.
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
