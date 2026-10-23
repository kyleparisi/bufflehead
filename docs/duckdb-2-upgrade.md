# DuckDB 2 upgrade

Bufflehead uses duckdb-go v2.20000.0 (DuckDB 2.0.0). The Mac App Store engine's
source pin and its statically linked extensions live in `packaging/duckdb.json`;
`bin/build-duckdb-mas`, `bin/prepare-mas-bundle`, `bin/collect-licenses` and the
`build-mas.yml` workflow all read it.

## Compatibility

- **SQL behavior changes:** single-arrow lambdas, regexp matching and division
  by zero behave differently from 1.1.3. Call these out in release notes.
- **File format:** DuckDB 2 reads 1.1.3 `.duckdb` files (opened read-only, so
  they are never rewritten). Files created by DuckDB 2 may not open in older
  Bufflehead releases.
- **VARIANT:** the Go driver can't scan VARIANT values yet. Queries returning
  them fail with an error telling the user to cast to VARCHAR.

## Verifying a MAS engine

After building an engine with `bin/build-duckdb-mas` (see `mac-app-store.md`),
check it on its native architecture:

```sh
engine=/absolute/path/to/engine
CGO_LDFLAGS="-L$engine -lduckdb -lc++ -lm" \
  go test -tags mas,duckdb_use_static_lib,duckdb_mas_engine ./internal/db
```

This asserts the engine reports v2.0.0, refuses `INSTALL`/`LOAD` of external
extensions, and still has the built-in JSON extension. `build-mas.yml` runs it
for the runner's architecture before signing.

## Rollback

Revert the engine/driver change as one unit: Go module pins,
`packaging/duckdb.json`, the MAS build scripts and the regenerated notices.
