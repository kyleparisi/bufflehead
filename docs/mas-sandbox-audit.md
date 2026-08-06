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
cannot see." There is no entitlement for arbitrary `~/.aws` access. The options
are a security-scoped bookmark the user grants once via powerbox (workable but
awkward, and it is a hidden directory that is painful to select), or dropping
AWS SSO from the MAS build.

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

Suggested fix, independent of the MAS decision:

1. Start the control server only when explicitly enabled
   (`BUFFLEHEAD_CONTROL=1`), off in release builds.
2. Bind port `0` (actually random), as the docs already claim.
3. Require a per-run bearer token, printed to stdout beside the port.
4. Reject requests carrying an `Origin` header.

`test/integration_test.sh:63` already parses the port from stdout, so the
harness only needs the env var and the token.

---

## Recommendation

**Don't ship to the Mac App Store.**

A sandboxed Bufflehead is a materially different, weaker product. Taken
together, A1 + B2 + B3 mean the App Store build would lose SQLite file support,
lose shared AWS SSO sessions, and lose ADC-based BigQuery — that is, most of the
cloud-database functionality that distinguishes the app from a plain Parquet
viewer. Getting there still costs a custom static DuckDB build, a cgo keychain
rewrite, and a powerbox flow for `~/.aws`.

The other three channels have none of these constraints:

- **Developer ID self-distribution** already works and is what ships today.
- **Setapp** distributes Developer ID-signed apps — no sandbox requirement.
- **Corporate MDM**, the channel you wanted next, explicitly does not involve
  the App Store.

If the App Store is strategically necessary, the viable shape is a deliberately
reduced MAS build: local files only (Parquet/CSV/JSON/DuckDB), no AWS, no
BigQuery, no extension management. That is a product decision, not a packaging
one — and worth making before any more licensing work targets that channel.
