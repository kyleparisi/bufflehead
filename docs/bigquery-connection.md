# BigQuery Connection Type — Design

Status: **proposed** (design only, no code yet)
Author: design session, 2026-07-31

## 1. Goal & context

Add BigQuery as a new connection type alongside the existing DuckDB (in-memory /
file) and Postgres (AWS-gateway / direct) backends.

Distinguishing facts that shape the design:

- **Primary entry point is AI.** The agent drives queries through the `/sql`
  control endpoint. The design optimizes for the agent writing safe, cheap SQL
  and recovering from failures on its own.
- **Queries cost money.** Unlike every other source, BigQuery bills by *bytes
  scanned*. Cost-safety is a first-class concern, not an afterthought.
- **Corporate environment.** Service accounts are hard to obtain; `gcloud` is
  already set up. We avoid host-level runtime dependencies (never shell out to
  the `gcloud` binary).

## 2. Authentication — ADC (Application Default Credentials)

**Decision: use ADC. No service account, no in-app OAuth (initially).**

- The Go client (`cloud.google.com/go/bigquery` + `golang.org/x/oauth2/google`)
  reads `~/.config/gcloud/application_default_credentials.json` directly and
  refreshes access tokens over HTTPS to `oauth2.googleapis.com`. **The `gcloud`
  binary is never invoked** — this is file + network only, not a host dependency.
- That ADC file is the product of the user's corporate Google SSO
  (`gcloud auth application-default login`). Reusing it means Bufflehead rides
  the existing SSO login rather than re-running the browser flow itself.
- Why not in-app OAuth ("the SSO thing"): it requires registering an OAuth
  client ID and a full auth-manager component, and in a locked-down org the
  consent screen / client may be admin-blocked — often *harder* than the service
  account we're avoiding. Defer it.

**Extensibility:** `db.NewBigQuery(...)` takes a `[]option.ClientOption` slice so
a future service-account or in-app-OAuth credential source slots in without
touching the `Querier`.

**Form UX:**
- Auto-detect the ADC file; show *"Signed in as you@corp.com"* (parse the
  account) so the active identity is visible.
- Auto-fill the project from the ADC `quota_project_id`; allow override.
- If ADC is missing, show a one-line setup hint:
  *"Run `gcloud auth application-default login`"* — a setup instruction, not a
  runtime dependency.

## 3. Connection model

New `ConnKind` in `internal/models/gateway.go`:

```go
KindBigQuery ConnKind = "bigquery"
```

Add an `IsBigQuery()` helper next to `IsDirect()`.

Reuse `GatewayEntry` / `Bookmark`, mapping/adding fields:

| Field | Meaning for BigQuery |
|---|---|
| `Name` | connection label |
| `GCPProject` (new) | billing/query project |
| `DefaultDataset` (new) | scopes schema loading & prompt (see §7) |
| `CredentialsPath` (new, optional) | explicit creds file; empty ⇒ ADC |
| `MaxBytesBilled` (new) | per-query bytes-scanned cap (see §4) |
| `RowLimit` (new, optional) | `/sql` default row cap override |
| `QueryTimeout` (new, optional) | job timeout override |
| `DBName` | unused (or default dataset alias) |

Credentials reference (if ever a keychain secret is needed) flows through the
existing `SecretKind` machinery. Add YAML/JSON tags + defaults in
`LoadGatewayConfig`.

## 4. Cost safety — three layers, in order of reliability

**Row limit ≠ cost control.** `SELECT * ... LIMIT 10` still scans every column of
every non-pruned partition. The row limit only bounds the JSON payload; the cost
knob is a different unit (bytes scanned).

1. **`maximumBytesBilled` (hard guarantee).** Set on every job. The job *fails
   before* scanning more than the cap, and a failed job isn't billed. This is
   the real safety net. Default ~20 GB/query; per-connection configurable so a
   "prod-warehouse" connection can be tighter than a "sandbox" one.
2. **Dry-run (pre-flight estimate).** The native client can dry-run
   (`JobConfig.DryRun`) to get exact bytes-to-be-scanned *without running or
   billing*. Use it two ways:
   - Auto dry-run every `/sql` call; if the estimate exceeds the cap, reject and
     **return the estimate in the error** so the agent self-corrects (adds a
     partition filter, drops columns) and retries.
   - Always return `bytes_processed` in the `/sql` response so the agent learns
     what each query cost.
3. **Cancel (stop in-flight).** See §5.

Row limit / timeout defaults (per-connection, configurable):

| Knob | Postgres today | BigQuery default |
|---|---|---|
| Row limit (`/sql`) | 100 | 1,000 |
| Bytes billed cap | n/a | ~20 GB / query |
| Timeout | 30s | 60–120s |

## 5. Query cancellation

Native client: `job.Cancel(ctx)` → the `jobs.cancel` API.

**Nuance:** cancelling the Go `context` stops *waiting* for results; it does NOT
kill the server-side job. To actually stop the work, call `job.Cancel` with the
job ID.

Design:
- `BigQueryDB.Query` starts the job, keeps the `*bigquery.Job` handle, waits with
  the request ctx, and **if the ctx is done, calls `job.Cancel` before
  returning.** This automatically covers agent disconnect / HTTP timeout — the
  `/sql` path's `baseCtx` (connworker.go:176) already cancels on client
  disconnect.
- **`POST /sql/cancel {"connection":"..."}`** — explicit cancel. The worker is
  sequential (one in-flight query per connection), so connection-scoped cancel
  is sufficient; it keys off `connection` like `/reconnect` does. (Optional
  future: return `job_id` in `/sql` responses and accept an optional `job_id`
  for precise targeting.)
- UI **Cancel button / Esc**: the generation counter already discards stale
  results, but for BQ we must actually stop the job (it's spending money). The
  worker holds the in-flight job handle; Cancel reaches it.

Billing note: a cancelled query is generally not charged, but we don't rely on
that — `maximumBytesBilled` is the guarantee; cancel is about "stop now / free
the slot / stop waiting."

## 6. `BigQueryDB` — the `Querier` implementation

New file `internal/db/bigquery.go`, mirroring `PostgresDB` and satisfying
`db.Querier` (Tables, TableSchema, Query, Ping, Close).

- **Construction:** `NewBigQuery(project string, opts ...option.ClientOption)`.
  Empty creds ⇒ ADC. Verify with a cheap `Ping` (`SELECT 1` dry-run or dataset
  list).
- **`Tables()`:** list via `INFORMATION_SCHEMA` (see §7), names as
  `dataset.table`.
- **Bulk schema (`AllTableSchemas`)**: one `INFORMATION_SCHEMA.COLUMNS` query
  (+ `TABLE_STORAGE`) per scoped dataset — **not** per-table `Metadata()`, which
  is N+1 (a project can have thousands of tables). This mirrors the Postgres
  bulk-schema approach.
- **`Query()`:**
  - Set `maximumBytesBilled`, timeout, and job labels (§8) on every job.
  - **Skip the exact `COUNT(*)`** for the agent path — BQ doesn't need the
    interactive grid's total, and the count is a full extra scan. For the
    interactive grid, prefer "1,000+" over an exact count.
  - Prefer **result-set page tokens** (cached results, free) over `LIMIT/OFFSET`
    re-scans for interactive pagination.
- **Dialect:** GoogleSQL, identifiers backtick-quoted `` `project.dataset.table` ``.
  This requires a per-dialect quoter (§9).

## 7. Schema loading & the AI prompt

The prompt is what makes the agent write cheap SQL. New `buildBigQueryAIPrompt`
(analog of `buildAIPrompt`, appwindow.go:2106).

**Pull metadata in bulk, cheaply, via `INFORMATION_SCHEMA`:**
- `` `dataset`.INFORMATION_SCHEMA.COLUMNS `` → columns + types, plus
  `is_partitioning_column` and `clustering_ordinal_position` (the partition /
  cluster hints).
- `` `dataset`.INFORMATION_SCHEMA.TABLE_STORAGE `` → row counts + bytes for the
  size annotations.

These metadata queries are effectively free.

**Scope to a default dataset — don't dump a whole project.** A project's full
schema can blow the context window (this is where BQ breaks the Postgres "load
all tables" assumption). So:
- Load full column/partition schema for the connection's **default dataset**
  into the prompt (like Postgres scopes to a database).
- List other dataset/table *names* as available but unexpanded.
- Add an on-demand schema lookup (a `/schema` endpoint, or fold into table
  listing) so the agent can pull a specific table's columns + partitioning when
  it needs them, rather than front-loading everything.

**Prompt content (BQ-specific):**
- GoogleSQL dialect; tables are backtick-quoted `` `project.dataset.table` ``.
- Per-table annotations: partition column, cluster columns, approx size, e.g.
  *"`events` — partitioned by `event_date` (daily), clustered by `user_id`,
  ~4.2 TB. Filter on `event_date` to prune."* — the single biggest cost lever.
- Cost hygiene: never `SELECT *` on large/wide tables; filter the partition
  column; select only needed columns; `LIMIT` previews but does **not** cut cost.
- The `maximumBytesBilled` cap in force, and that over-cap queries fail with a
  byte estimate the agent should use to narrow the query.
- The dry-run / estimate behavior and that responses include `bytes_processed`.

## 8. Job labels

BigQuery already records the authenticated principal on every job as
`user_email` (a column in `INFORMATION_SCHEMA.JOBS`) — so *who ran it* is free
and more reliable than a label (label values are lowercase `[a-z0-9_-]`, ≤63
chars, no `@`/`.`, so an email can't be stored cleanly). **Do not label the
user.**

Label what BQ does *not* record:
- `app=bufflehead` — separates Bufflehead's queries from the same user's
  console/dbt/Looker queries.
- `source=agent` vs `source=ui` — distinguishes AI-driven spend from interactive
  spend. More useful than a username, which `user_email` already provides.

Cheap FinOps win: the org can attribute and policy-cap Bufflehead spend by label.

## 9. Cross-cutting: SQL dialect / identifier quoting

`state.VirtualSQL()` (state.go:166) and `db.QuoteQualifiedName` hardcode
double-quotes (Postgres/DuckDB). BigQuery needs **backticks**. Introduce a
per-dialect quoter selected off the active connection's `ConnKind`, applied
wherever identifiers are emitted (the `VirtualSQL` wrapper and the
click-to-query `SELECT * FROM <table>` path at appwindow.go:1394).

## 10. Wiring (mechanical, mirrors the `IsDirect()` branches)

- `internal/ui/connworker.go`: `RunOpenBigQuery(...)` one-shot goroutine
  (copy of `RunOpenPostgres`, connworker.go:466) → connect, `Tables()`,
  `AllTableSchemas()`, post `ReqOpenGateway` (already generic — carries a
  `Querier`).
- `internal/ui/app.go` `onGatewayConnected` (line 2480): add an
  `entry.IsBigQuery()` branch alongside `entry.IsDirect()`; set
  `pendingGateway = &GatewayConnection{Config: entry}` (no auth/tunnel).
- `internal/ui/gateway_connect.go`: `openBigQueryDB(entry)` mirroring
  `openDirectPostgresDB` (line 359); branch on `IsBigQuery()` in
  `reconnectConnection` / `switchDatabase` (trivial — no tunnel to rebuild).
- Titlebar / labels: add a `"BigQuery"` path where `"PostgreSQL"` is hardcoded
  (appwindow.go:997, 1402; AI prompt).
- `internal/ui/gateway_screen.go`: third type tile in `buildTypeSelector`
  (line 310), a `buildBQForm()` panel toggled in `renderConnKind` (line 383),
  and `onBQConnect()` building a `KindBigQuery` entry (model after `onPGConnect`,
  line 1210).

## 11. Control API changes

- `POST /sql`: accept optional `max_bytes` override (default from connection
  config); return `bytes_processed` (and optionally `job_id`) in the response.
- `POST /sql/cancel {"connection":"..."}`: cancel the connection's in-flight
  job.
- (Optional) `POST /schema` or extend table listing: on-demand column +
  partition metadata for a specific `dataset.table`.
- `buildBigQueryAIPrompt` documents all of the above for the agent.

## 12. Phasing

1. **Backend — DONE.** `internal/db/bigquery.go` behind `Querier` — ADC connect,
   `Ping`, `Tables`, bulk `INFORMATION_SCHEMA` schema, `Query` with
   `maximumBytesBilled` + labels + cancel-on-ctx, `CancelRunning`,
   `CredentialsOption`, `QuoteBigQueryName`. Unit-tested. (Live smoke test still
   pending — needs emulator or real project.)
2. **Wiring — DONE.** `KindBigQuery` + `GatewayEntry`/`Bookmark` fields +
   `IsBigQuery`/`EffectiveMaxBytesBilled`; `RunOpenBigQuery` + refresh branch;
   `openBigQueryDB` + reconnect branch; `onGatewayConnected` dispatch; dialect
   quoter (`AppState.Dialect` + `quoteIdent`); title/path/label helpers
   (`connDisplayKind`/`connDBSegment`/`connPathFor`); `buildBigQueryAIPrompt`
   (routed via `buildAIPrompt`). Builds + unit tests + vet clean.
3. **Control + AI — DONE.** `POST /sql/cancel` → `CancelExecutor` →
   `BigQueryDB.CancelRunning` (app.go, BigQuery-only); `bytes_processed` on
   `QueryResult` → `SQLResult` (populated from job stats). Prompt done. Endpoint
   unit-tested (`TestCancelEndpoint_*`). **Deferred:** per-request `max_bytes`
   override (needs a Querier-interface change; the connection-level cap already
   protects, so low priority).
4. **UI — DONE (form).** BigQuery selector tile + `buildBQForm` +
   `onBQConnect`/`onBQTest`/`validateBQ`; `renderConnKind` extended to 4 kinds;
   `buildBookmarkCard` treats BigQuery as a no-AWS (connect-immediately) card.
   BigQuery connections now open end-to-end from the UI and save/restore as
   bookmarks. **Deferred:** a visible in-grid Cancel button (the AI path uses
   `/sql/cancel`; an interactive Cancel button needs per-tab running-query
   tracking — a follow-up).

## 12b. Testing strategy

- **Unit tests** (no network) cover the pure logic already: `bqPaged`,
  `isReadOnlyQuery` / `stripLeadingSQLNoise`, `bqFormatValue`,
  `QuoteBigQueryName`, `splitDatasetTable`. See `internal/db/bigquery_test.go`.
- **No in-SDK emulator.** Confirmed empirically: `cloud.google.com/go/bigquery`
  ships no consumer-facing fake/emulator (unlike Pub/Sub's `pstest` or Spanner's
  `spannertest` — BigQuery is the exception). So live testing needs one of:
  - `goccy/bigquery-emulator` — third-party, run as an external process/Docker.
  - An httptest mock at the HTTP layer.
  - Real BigQuery (a scratch project).
- **Injection hook is already in place.** `NewBigQuery` appends caller-supplied
  `option.ClientOption`s and honors `BIGQUERY_EMULATOR_HOST`
  (→ `option.WithEndpoint` + `WithoutAuthentication`), so any of the above plugs
  in without changing the constructor.
- **Cost semantics can't be emulated** (dry-run bytes, `maximumBytesBilled`,
  labels are no-ops in emulators) — verify those with unit tests around our own
  request-building plus one manual pass against real BigQuery.

## 13. Open questions

- Default `maximumBytesBilled` cap value (20 GB starting point?) and whether the
  agent may raise it per-query up to a hard ceiling.
- Where the default dataset is chosen: required form field vs. optional with
  "all datasets" listing.
- Whether interactive-grid pagination uses result page tokens (free, preferred)
  now or later (`LIMIT/OFFSET` as a stopgap).
- Verify `cloud.google.com/go/bigquery` dependency weight is acceptable for the
  shipped binaries (macOS arm64, Windows).
