# DuckDB 2 upgrade validation

The candidate uses duckdb-go v2.20000.0 / DuckDB 2.0.0. The MAS source pin and
approved static extensions live in `packaging/duckdb.json`. The runtime stays
read-only for opened DuckDB files; do not rewrite customer files for testing.

## Current local evidence

- Full Linux Go unit suite passes on Go 1.27.1, including real Parquet metadata values,
  error propagation, VARIANT rejection and subsequent connection reuse.
- Explicit VARIANT-to-VARCHAR output works. Automatic VARIANT scanning remains
  unsupported by the Go driver; the app provides an actionable error.
- The upstream generated loader compiles against the pinned v2 headers.
- Linux custom-static tests with the official v2 archives, registering only
  core_functions/json/parquet, pass. INSTALL/LOAD httpfs are rejected because
  the loadable_extensions capability is absent. This validates library selection,
  not the from-source Apple SDK build or App Store sandbox/signature.
- CMake accepts the restricted configuration with no linked capabilities and
  an explicit v2.0.0 release version. The version override prevents a pinned
  commit-only checkout from identifying itself as a development build.
- Packaging tests reject old engines, absent loaders, extra extensions,
  enabled extension loading and mismatched archive hashes before replacing an app.

- The live Linux Godot 4.6.1 suite passed 110 tests, with 3 Claude Desktop
  platform-specific cases skipped. The added VARIANT UI error/cast recovery
  regression passed separately against the running app.
- The legacy fixture accepts SELECT, rejects direct UPDATE/DELETE/CREATE, and
  stays byte-for-byte unchanged after close, with both default and restricted
  static libraries.
- Six fail-closed packaging tests pass. Notices were regenerated for the local
  Linux build; a Darwin arm64/MAS dependency-graph inventory was also collected.
  That inventory is not an Apple SDK compile or signature verification.

## Remaining native gates

No Mac SDK, native Mac host, native Windows host or cross compiler is available
in this session. Native workflows are prepared locally and have not been pushed
or run. No signing or upload was performed.

1. Run Tests CI on Windows with the existing pinned GCC toolchain and on both
   macOS runners. The macOS matrix runs natively, not cross-compiled. If Windows
   linking fails, inspect new archive dependencies before changing the toolchain.
2. On each Mac architecture, build the locked engine using the commands in
   `mac-app-store.md`, then run:

   ```sh
   engine=/absolute/path/to/matching-engine
   CGO_LDFLAGS="-L$engine -lduckdb -lc++ -lm" \
     go test -tags mas,duckdb_use_static_lib,duckdb_mas_engine ./internal/db
   ```

   Both native slices must report v2.0.0 and reject external extension loading.
   The App Store workflow includes this check for its host architecture before signing.
3. Recollect MAS notices against the exact source, prepare the universal app,
   then inspect both slices, minimum OS, signatures and sandbox entitlements.
   Do not enable `upload` merely to perform preflight testing.
4. Exercise the running UI using `test/integration_test.sh` with a pinned Godot
   4.6.1 executable and pytest/requests in a temporary venv. Linux may require
   `xvfb-run -a` even with Godot's `--headless` option. The runner imports a clean project and uses the shipped Linux preload
   strategy for static TLS. It isolates app
   settings and Claude Desktop config and kills only its own Godot child.
   Native macOS sandbox/file bookmarks and Windows packaging require their own
   execution; Linux GUI results do not certify them.

## Compatibility and rollback

Do not automatically convert database files. DuckDB 2 can read the tested old
1.1.3 file without changing it; newly created v2 files may not open in the old
engine. Single-arrow lambdas, regexp matching and division by zero change SQL
behavior; include these in release notes. Keep the prior signed v0.33.0 installers.
Rollback code as one scoped migration, including Go pins, MAS lock/build scripts
and notices; rebuild/re-sign through the existing release process. Never use an
old engine on a file rewritten to the new format. No release has been authorized
by this development/test work.

## Upstream references

- https://github.com/duckdb/duckdb-go#linking-a-static-library
- https://github.com/duckdb/duckdb-go-bindings#upgrading-to-duckdb-v2
- https://duckdb.org/docs/2.0/operations_manual/migration
- https://docs.github.com/en/actions/reference/runners/github-hosted-runners
