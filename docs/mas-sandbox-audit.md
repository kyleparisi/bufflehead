# App Sandbox audit for the Mac App Store

The Mac App Store requires App Sandbox (`com.apple.security.app-sandbox`).
`graphics/export_presets.cfg:199` currently sets `app_sandbox/enabled=false`.
This is an audit of what turning it on would actually break.

**Summary: three of Bufflehead's core capabilities cannot survive the sandbox
in their current form, and one of them cannot survive it at all.** Details and
evidence below; recommendation at the end.

---

## A. Banned outright — no entitlement exists

### A1. DuckDB downloads and loads extensions at runtime

| Evidence | |
|---|---|
| `internal/db/extensions.go:43` | `InstallExtension` runs `INSTALL <name>`, which downloads a `.duckdb_extension` dylib |
| `internal/db/duck.go:78` | `OpenSQLite` runs `INSTALL sqlite; LOAD sqlite` on every SQLite file open |
| `internal/ui/extensions.go` | A whole UI panel for browsing and installing extensions |
| `packaging/macos/entitlements.plist` | Already documents `disable-library-validation` as REQUIRED for exactly this |

App Store Review Guideline **2.5.2** forbids downloading and executing code.
Separately, `com.apple.security.cs.disable-library-validation` is a Developer ID
hardened-runtime exception and is not available to App Store builds.

There is no entitlement that makes this legal. The only route is to build DuckDB
with every needed extension **statically linked**, remove the extensions UI, and
disable remote extension installation. That means producing a custom DuckDB
build rather than using `go-duckdb`'s prebuilt static libraries — real work, and
it permanently caps which extensions users can have.

Immediate casualty: **SQLite file support breaks**, because `OpenSQLite` installs
the extension on demand.

---

## B. Sandbox breaks it; a workaround exists but is substantial

### B1. Keychain access shells out to a subprocess

`internal/models/secret.go` uses `github.com/zalando/go-keyring`, whose macOS
implementation (`keyring_darwin.go`) is:

```go
const execPathKeychain = "/usr/bin/security"
```

It spawns `/usr/bin/security`. A sandboxed app cannot spawn arbitrary
executables outside its bundle, so **every saved database password stops
working**.

Fix: replace it with a Security.framework binding (cgo, e.g.
`keybase/go-keychain`) plus `com.apple.security.keychain-access-groups`.
Tractable, but it is a dependency swap in the credential path, and it adds cgo.

### B2. AWS SSO config sharing breaks by design

`internal/aws/auth.go` reads and writes the user's real AWS config:

- `:118`, `:204`, `:234`, `:241` — `~/.aws/config` (writes the `sso-session` block)
- `:439`, `:451`, `:470` — `~/.aws/sso/cache/*.json` (the SSO token cache)

Under the sandbox, `os.UserHomeDir()` resolves **into the app container**
(`~/Library/Containers/<bundle-id>/Data`). The code keeps working — it just
silently stops sharing state with the user's actual AWS CLI.

That is the whole premise of the feature. "Log in with your existing SSO
session" becomes "log in again, separately, in a private copy the `aws` CLI
cannot see." There is no entitlement for arbitrary `~/.aws` access.

The workable route is a **security-scoped bookmark**: the user picks `~/.aws`
once through an `NSOpenPanel`, and the app persists a bookmark it can re-resolve
on later launches. See the DarwinKit section below — this is reachable from Go,
so B2 is an awkward-UX problem rather than an impossible one.

### B3. BigQuery via gcloud ADC breaks the same way

`internal/db/bigquery.go` defaults to Application Default Credentials, which
read `~/.config/gcloud/application_default_credentials.json`. Same container
redirection, same outcome: **ADC-based BigQuery does not work sandboxed**. The
explicit credentials-file path (`gateway_screen.go:1359`) survives, because the
user picks that file through the open panel and powerbox grants access to it.

### B4. Browser launch spawns `open`

`internal/aws/auth.go:425`:

```go
exec.Command("open", url).Start()
```

Blocked by the sandbox. This one is genuinely easy — Godot's `OS.shell_open`
goes through `NSWorkspace` and is sandbox-safe.

---

## C. Just needs entitlements

| Capability | Evidence | Entitlement |
|---|---|---|
| Outbound connections (Postgres, BigQuery, S3, SSM) | `internal/db/`, `internal/aws/` | `com.apple.security.network.client` |
| SSM tunnel listener | `internal/aws/tunnel.go:344` binds `127.0.0.1:0` | `com.apple.security.network.server` |
| Control server listener | `internal/control/server.go` | `com.apple.security.network.server` |

Two things already work correctly and need no change:

- **File open dialogs** use `DisplayServer.FileDialogShow`
  (`internal/ui/app.go:394`, `internal/ui/appwindow.go:1978`), which is Godot's
  *native* macOS panel. Powerbox grants sandbox access to whatever the user
  picks, so opening files keeps working.
- **Config storage** uses `os.UserConfigDir()`
  (`internal/models/config_dir.go`), which redirects into the container
  automatically. Fine for a fresh App Store install.

The SSM tunnel itself is fine: `session-manager-plugin` is imported as a Go
library, not spawned as a subprocess.

---

## D. Unrelated finding: the control server ships enabled and unauthenticated

Found while auditing the network entitlements. **This affects the app you ship
today and has nothing to do with the App Store.**

`cmd/viewer/main.go:37`:

```go
ctrlServer := control.New(9900)
ctrlServer.Start()
```

- Started unconditionally in **every** build, including releases.
- Bound to a **fixed, predictable** port (`internal/control/server.go`,
  `net.Listen("tcp", "127.0.0.1:9900")`).
- **No authentication, no token, no `Origin` check** anywhere in `buildMux`.
- Exposes `POST /query` (arbitrary SQL on whatever the user has connected),
  `POST /open` (read any path), `GET /state`, `GET /screenshot`.

Any web page the user visits can send a cross-origin
`POST http://127.0.0.1:9900/query`. With `Content-Type: text/plain` this is a
CORS *simple request*: no preflight, and the browser blocking the **response**
does not stop the request from **executing**. So a malicious page can run
`DROP`/`DELETE`/`UPDATE` against a production database the user is connected to.
DNS rebinding defeats the response-reading restriction too, making
`/state` and `/screenshot` readable.

`CLAUDE.md` describes this as binding "to a random available port" — that is not
what the code does.

### Status: OPEN — deferred by decision, not fixed

Filed for later on 2026-08-06. No code change has been made; the behaviour
described above is what ships today. When it is picked up, the fix is:

1. Start the control server only when explicitly enabled
   (`BUFFLEHEAD_CONTROL=1`), off in release builds.
2. Bind port `0` (actually random), as the docs already claim.
3. Require a per-run bearer token, printed to stdout beside the port.
4. Reject requests carrying an `Origin` header.

`test/integration_test.sh:63` already parses the port from stdout, so the
harness only needs the env var and the token — the test infrastructure is
already port-agnostic and will not need restructuring.

Separately, `CLAUDE.md`'s "Testing" section should be corrected: it states the
control server "binds to a random available port", which does not match
`cmd/viewer/main.go:37`.

---

## E. Would DarwinKit help?

[DarwinKit](https://github.com/progrium/darwinkit) (formerly MacDriver) gives Go
cgo bindings to macOS Objective-C frameworks. Evaluated against v0.5.0, the
latest release. Coverage checked directly against the module source:

| Need | DarwinKit v0.5.0 | Verdict |
|---|---|---|
| Security-scoped bookmarks (B2, B3) | `foundation.URL.StartAccessingSecurityScopedResource`, `StopAccessingSecurityScopedResource`, `BookmarkDataWithOptions…` (`macos/foundation/url.gen.go`) | **Solves it** |
| File picker to grant that access | `appkit.OpenPanel` (`macos/appkit/open_panel.gen.go`) | **Solves it** |
| Open a URL in the browser (B4) | `appkit.Workspace` (`macos/appkit/workspace.gen.go`) | Solves it, but Godot's `OS.shell_open` already does |
| Keychain Services (B1) | **Absent.** No `SecItemAdd`, `SecItemCopyMatching` or `kSecClassGenericPassword` anywhere in the module. The bundled `securityinterface` package is SecurityInterface.framework (auth/certificate *panels*), not Security.framework | Use `keybase/go-keychain` instead |
| StoreKit | **Absent** — no `storekit` package | Not needed; exit(173) covers receipt refresh |
| DuckDB extension download + `dlopen` (A1) | Not applicable | **Solves nothing** |

So DarwinKit is a real answer to the hardest *API* problem — it turns "the app
can never see the user's real `~/.aws`" into "the user grants it once through a
file picker." That is a genuine improvement over the earlier assessment, and cgo
is already required (`go-duckdb` is a cgo package), so it adds no new build
constraint.

Three caveats worth weighing:

1. **It does nothing for A1**, which is the fatal blocker. Downloading and
   loading a dylib is banned by review policy, not by an API Go cannot reach.
   No binding library can change that.
2. **v0.5.0 is dated June 2024** with no newer release — roughly two years stale.
   That is a maintenance risk for code sitting in the credential path.
3. **Godot owns the `NSApplication` and the main run loop.** Driving an
   `NSOpenPanel` through DarwinKit alongside graphics.gd needs main-thread care
   and may contend with Godot's event loop. Untested here, and a real
   integration risk.

Also worth noting: even with a working bookmark flow, asking a reviewer to
approve an app that requests access to a hidden credentials directory is a
plausible point of friction. Not an automatic rejection, but not free either.

---

## Recommendation

**Don't ship to the Mac App Store** — but for a narrower reason than the first
draft of this audit claimed.

DarwinKit (section E) genuinely defuses B2 and B3: the security-scoped bookmark
API is right there, so shared AWS SSO and gcloud ADC are recoverable at the cost
of a one-time "pick your `~/.aws` folder" step. B1 is a dependency swap. B4 is a
one-liner. None of those are the problem.

**A1 is the problem, and nothing fixes it.** DuckDB downloading and loading
extensions is banned by App Store review policy, not by a macOS API that a Go
binding could reach. It is on the default path — opening any SQLite file calls
`INSTALL sqlite` — so SQLite support dies unless DuckDB is rebuilt with every
extension statically linked, the extensions UI is removed, and remote extension
installation is disabled. That is a custom DuckDB toolchain to maintain
indefinitely, and it permanently caps which extensions users can ever have.

The other three channels have none of these constraints:

- **Developer ID self-distribution** already works and is what ships today.
- **Setapp** distributes Developer ID-signed apps — no sandbox requirement.
- **Corporate MDM**, the channel you wanted next, explicitly does not involve
  the App Store.

If the App Store is strategically necessary, the viable shape is a deliberately
reduced MAS build: local files only (Parquet/CSV/JSON/DuckDB), no AWS, no
BigQuery, no extension management. That is a product decision, not a packaging
one — and worth making before any more licensing work targets that channel.
