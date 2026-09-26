# Mac App Store channel — implementation and validation

No release or submission is authorized by this directory. Existing Developer ID
packaging remains separate. The store channel uses the `mas` Go build tag:
updater downloader/installer files are excluded by build constraints, update
API commands reject requests, and extension-management controls are hidden.
The engine must ALSO be built with `DISABLE_EXTENSION_LOAD`; the UI tag alone
is not a security boundary.

1. Use pinned DuckDB v1.1.3 source commit19864453f7d0ed095256d848b46e7b8630989bac.
   `bin/build-duckdb-mas --source SOURCE --output BUILD --arch x86_64`
   builds from source and emits a bundled archive plus SHA256 provenance.
   Repeat for arm64 on the matching toolchain/runner. No archive-object patching.
2. Regenerate/review license materials with `bin/collect-licenses`. The current
   checked-in inventory is macOS amd64; review the other architecture/target
   graph and any new native extensions. Include Resources/Licenses BEFORE signing.
3. Export the production Godot app with the intended architecture and sandbox
   export settings. Given that exported bundle and its actual library location,
   run `bin/prepare-mas-bundle --app APP --duckdb BUILD --arch amd64
   --library-path RELATIVE_GO_LIBRARY_PATH`. It verifies clean engine provenance
   and refuses the incremental feasibility artifact.
4. Provision/sign with the appropriate Mac App Store identities and profile,
   using the narrow entitlements in this directory, not Developer ID's
   disable-library-validation entitlement. Verify nested libraries/architecture,
   package and validate on the intended store distribution toolchain. Final store signing/provisioning remains unverified; the local validation
   below used ad-hoc signing, and this script does not submit a build.
5. Rerun compile-time extension rejection AND positive Parquet/CSV/JSON,
   SQLite/Postgres, SSH, keychain, actual-app grant/relaunch and localhost API
   checks against each final signed architecture, not just the previous spike.

## Current limits to resolve before release

- Clean x86_64 and ARM64 libraries each passed 11 signed-sandbox engine checks.
  Repeat the gates against final store-signed artifacts.
- The current fixed engine set is JSON/Parquet (CSV built-in). Cloud workflows
  needing httpfs/other extensions are not established by this build.
- Folder scopes are currently held for app lifetime. Add explicit revoke/regrant
  UX before calling this production permission UX.
  Scope reuse is now deduplicated by saved folder and covered by a lifecycle test.
- The default macOS agent socket was denied by the sandbox. A dedicated agent
  using a container-local socket passed host-verified authentication and
  forwarding on Intel; this is not a finished user-facing setup flow.
- AWS SSO/cache refresh, gcloud and cross-channel
  keychain migration remain unverified. External disposable key-file testing
  must not be reported as proof of these workflows.
- Store purchase/receipt/licensing integration and App Review are separate work.

Store configuration uses Foundation Application Support URLs (container-aware).
A signed probe passed without the spike config override; normal distribution
keeps its existing path resolution. Native picker selection rejects a folder
that does not contain the requested path.


## Validation recorded September 26, 2026

- Source-built pinned DuckDB on Intel and Apple Silicon; no archive patching.
- Intel: 11 engine checks and 17 actual-app checks passed, including interactive
  folder access followed by saved-permission reopening in a fresh process.
- ARM64: 11 engine checks, 11 SSH top-level tests (including read-only Postgres
  through SSH), and 15 actual-app checks passed. Three optional SSH probes were
  skipped. Native folder-picker/reopen was not repeated on ARM64.
- ARM64 app checks cover Parquet/CSV/JSON/SQLite queries, authenticated external
  API requests, ungranted-file denial, store update restrictions and license nodes.
- Both architectures: bundled notices/source hashes and strict ad-hoc signatures
  verified. ARM64 used Go 1.26.1 and Xcode macOS SDK 26.5 on macOS 26.2.
- Windows/Linux: normal build-file selection checked; SSH/updater packages
  cross-compiled. Full native builds and runtime regression tests remain pending.
- No App Store signing, submission, purchase integration or release is implied.

The optional live-agent probe requires `SPIKE_REAL_AGENT_PROBE=1`,
`SPIKE_AGENT_HOST`, `SPIKE_AGENT_USER`, `SPIKE_AGENT_KNOWN_HOSTS`,
`SPIKE_EXPECT_SANDBOX`, and `SPIKE_OUTSIDE_FILE`. It uses agent-only authentication
(no disk-key fallback/agent forwarding), checks the trusted host key and runs
only a fixed read-only printf command. Never bypass host-key verification.
