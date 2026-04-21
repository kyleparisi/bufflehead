# Bufflehead Gateway: Implementation Plan

## Overview

Transform Bufflehead from a local Parquet/DuckDB viewer into a **database gateway application** that connects non-technical users to remote PostgreSQL databases in AWS private subnets via SSM tunneling. The app handles AWS authentication, tunnel lifecycle, and exposes a familiar SQL editor UI — while also making the tunnel available to any local tool (Claude MCP, Python, DBeaver, etc.).

The existing Bufflehead architecture (Go + graphics.gd + Godot) is preserved. DuckDB remains available for local file queries. Postgres connectivity is added alongside it.

---

## Architecture

```
┌──────────────────────────────────────────────────────┐
│  Bufflehead Gateway (Go + Godot)                     │
│                                                      │
│  ┌──────────────┐  ┌─────────────────────────────┐   │
│  │ Connection   │  │ SQL Editor + Data Grid       │   │
│  │ Rail         │  │ (existing Bufflehead UI)     │   │
│  │              │  │                              │   │
│  │ [staging ●]  │  │ Schema browser, tabs,        │   │
│  │ [prod    ○]  │  │ query history, nav stack     │   │
│  │ [local   ○]  │  │                              │   │
│  └──────┬───────┘  └──────────┬──────────────────┘   │
│         │                     │                      │
│  ┌──────▼───────┐      ┌──────▼──────────────────┐   │
│  │ AWS Auth     │      │ db.PostgresDB           │   │
│  │ Manager      │      │ (pgx driver)            │   │
│  └──────┬───────┘      └──────────┬──────────────┘   │
│         │                         │                  │
│  ┌──────▼───────┐          localhost:PORT             │
│  │ SSM Tunnel   │◄────────────────┘                  │
│  │ Manager      │                                    │
│  │ (subprocess) │                                    │
│  └──────┬───────┘                                    │
│         │                                            │
│  ┌──────▼────────────────────────────────────────┐   │
│  │ Gateway Info Panel                            │   │
│  │                                               │   │
│  │  Status: Connected                            │   │
│  │  Local endpoint: localhost:5432                │   │
│  │  Database: cradle                             │   │
│  │  User: readonly                               │   │
│  │                                               │   │
│  │  Use this connection in other tools:          │   │
│  │  ┌──────────────────────────────────────────┐ │   │
│  │  │ psql -h localhost -p 5432 -U readonly    │ │   │
│  │  │ -d cradle                          [Copy]│ │   │
│  │  └──────────────────────────────────────────┘ │   │
│  │  ┌──────────────────────────────────────────┐ │   │
│  │  │ postgresql://readonly:***@localhost:5432  │ │   │
│  │  │ /cradle                            [Copy]│ │   │
│  │  └──────────────────────────────────────────┘ │   │
│  └───────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────┘
          │
          ▼ (SSM tunnel subprocess)
   ┌──────────────┐
   │ AWS SSM      │──────▶ RDS Postgres (private subnet)
   │ Session Mgr  │
   └──────────────┘
          ▲
          │ localhost:PORT is also available to:
   ┌──────┴────────┐
   │ psql          │
   │ Python/pandas │
   │ Claude MCP    │
   │ DBeaver       │
   │ Jupyter       │
   └───────────────┘
```

---

## Configuration

Create a config file at the platform-appropriate config directory (same pattern as `internal/models/history.go`):
- macOS: `~/Library/Application Support/bufflehead/gateway.yaml`
- Linux: `~/.config/bufflehead/gateway.yaml`
- Windows: `%APPDATA%/bufflehead/gateway.yaml`

```yaml
gateways:
  - name: "Staging"
    aws_profile: "staging-readonly"      # AWS SSO profile name
    aws_region: "us-east-1"
    # Instance discovery (pick one):
    instance_id: "i-0abc123def456"       # Direct instance ID
    # OR tag-based discovery:
    instance_tags:
      Role: bastion
      Environment: staging
    # RDS target:
    rds_host: "mydb.cluster-ro-xyz.us-east-1.rds.amazonaws.com"
    rds_port: 5432
    local_port: 5432                     # Port on localhost
    db_name: "cradle"
    db_user: "readonly"
    # db_password is NOT stored here — see credential resolution below

  - name: "Production"
    aws_profile: "prod-readonly"
    aws_region: "us-east-1"
    instance_tags:
      Role: bastion
      Environment: production
    rds_host: "prod-db.cluster-ro-abc.us-east-1.rds.amazonaws.com"
    rds_port: 5432
    local_port: 5433                     # Different port to allow simultaneous connections
    db_name: "cradle"
    db_user: "readonly"
```

### Credential Resolution (in priority order)

The app resolves AWS credentials using the standard SDK chain. No custom credential code needed — just use the AWS SDK for Go v2 default credential provider, which checks:

1. Environment variables (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`)
2. Shared credentials file (`~/.aws/credentials`)
3. AWS SSO cache (after `aws sso login --profile <name>`)
4. EC2 instance metadata (not relevant for desktop, but handled automatically)

For the database password, use one of:
- `db_password` field in the config (simplest, for non-sensitive envs)
- `db_password_env` field pointing to an env var name
- AWS Secrets Manager lookup (future enhancement)

---

## Implementation Phases

### Phase 1: AWS Credential & SSO Management

Create `internal/aws/auth.go`:

```go
package aws

// Uses: github.com/aws/aws-sdk-go-v2/config, .../credentials, .../sso

type AuthManager struct {
    profile string
    region  string
}

// Status returns the current credential state for a profile.
// Returns one of: Valid, Expired, NoCredentials.
func (a *AuthManager) Status() CredentialStatus

// SSOLogin launches `aws sso login --profile <name>` as a subprocess.
// This opens the user's browser for SSO approval.
// Returns a channel that signals when login completes or fails.
func (a *AuthManager) SSOLogin() <-chan error

// ResolveInstanceID finds a bastion instance by tags using EC2 DescribeInstances.
// Falls back to the configured instance_id if tags aren't set.
func (a *AuthManager) ResolveInstanceID(tags map[string]string) (string, error)
```

**Key decisions:**
- Use `os/exec` to run `aws sso login` rather than implementing the OIDC device flow directly. This reuses the user's existing AWS CLI configuration and avoids reimplementing the browser-based auth flow. The user must have the AWS CLI installed — this is a prerequisite.
- Credential status is checked via the AWS SDK's credential provider. If creds are expired, the UI shows a "Login" button.
- For non-SSO users (those using env vars, credential files, or session tokens), skip the SSO login step entirely — credentials are already available.

### Phase 2: SSM Tunnel Manager

Create `internal/aws/tunnel.go`:

```go
package aws

// Uses: os/exec, net

type TunnelManager struct {
    process   *exec.Cmd
    localPort int
    status    TunnelStatus // Disconnected, Connecting, Connected, Error
    onStatus  func(TunnelStatus)
}

type TunnelConfig struct {
    InstanceID string
    RDSHost    string
    RDSPort    int
    LocalPort  int
    AWSProfile string
    AWSRegion  string
}

// Start launches the SSM port-forwarding session as a subprocess:
//   aws ssm start-session \
//     --target <instance-id> \
//     --document-name AWS-StartPortForwardingSessionToRemoteHost \
//     --parameters '{"host":["<rds-host>"],"portNumber":["<rds-port>"],"localPortNumber":["<local-port>"]}' \
//     --profile <profile> \
//     --region <region>
//
// Monitors stdout for "Port <port> opened" to confirm connection.
// Monitors the process for unexpected exit (spot instance rotation, session timeout).
// On unexpected exit, updates status and optionally auto-reconnects.
func (t *TunnelManager) Start(cfg TunnelConfig) error

// Stop sends SIGTERM to the subprocess and waits for exit.
func (t *TunnelManager) Stop() error

// IsPortReady does a TCP dial to localhost:PORT to verify the tunnel is accepting connections.
func (t *TunnelManager) IsPortReady() bool

// WaitReady blocks until the tunnel port is accepting TCP connections or timeout.
func (t *TunnelManager) WaitReady(timeout time.Duration) error
```

**Key decisions:**
- The tunnel is a managed child process. When the app exits, the tunnel is killed.
- Parse stdout from the `aws ssm` process — it prints `"Port <N> opened for sessionId"` when ready.
- If the subprocess dies unexpectedly (bastion replaced, session timeout), update UI status and offer reconnect.
- Prerequisite: `session-manager-plugin` must be installed. Check for it at startup and show a helpful error if missing.

### Phase 3: Postgres Database Driver

Create `internal/db/postgres.go`:

This integrates alongside the existing `duck.go`. Both implement the same query patterns (Schema, Query, Tables, etc.) so the UI layer doesn't need to know which backend is active.

```go
package db

// Uses: github.com/jackc/pgx/v5/stdlib (or pgx/v5 directly)

type PostgresDB struct {
    conn *sql.DB
}

func NewPostgres(host string, port int, dbName, user, password string) (*PostgresDB, error)

// Close releases the connection pool.
func (p *PostgresDB) Close() error

// Tables lists tables and views from information_schema, filtered to relevant schemas.
// Exclude pg_catalog, information_schema, and other system schemas.
func (p *PostgresDB) Tables() ([]TableInfo, error)

// TableSchema returns column info for a table from information_schema.columns.
func (p *PostgresDB) TableSchema(tableName string) ([]Column, error)

// Query runs a paginated query. Same signature as DB.Query().
// Uses the same VirtualSQL wrapping (ORDER BY + LIMIT/OFFSET) that DuckDB uses.
func (p *PostgresDB) Query(virtualSQL string, offset, limit int) (*QueryResult, error)

// SchemaNames returns available schema names (public, etc.) for the schema browser.
func (p *PostgresDB) SchemaNames() ([]string, error)
```

**Key decisions:**
- Use `pgx` via the `database/sql` interface (`pgx/v5/stdlib`) so that `PostgresDB` has the same `*sql.DB` foundation as the existing `DB` struct. This keeps the query execution path nearly identical.
- The existing `Column`, `QueryResult`, `TableInfo` types in `duck.go` are already generic enough — reuse them as-is.
- Consider extracting a `Querier` interface that both `DB` (DuckDB) and `PostgresDB` implement:
  ```go
  type Querier interface {
      Tables() ([]TableInfo, error)
      TableSchema(name string) ([]Column, error)
      Query(sql string, offset, limit int) (*QueryResult, error)
      Close() error
  }
  ```
  This lets `ConnWorker` and the UI operate on either backend without type switches.
- Read-only enforcement: connect with a read-only Postgres role AND set `default_transaction_read_only = on` at the session level.

### Phase 4: Gateway Configuration & Persistence

Create `internal/models/gateway.go`:

```go
package models

type GatewayConfig struct {
    Gateways []GatewayEntry `yaml:"gateways"`
}

type GatewayEntry struct {
    Name         string            `yaml:"name"`
    AWSProfile   string            `yaml:"aws_profile"`
    AWSRegion    string            `yaml:"aws_region"`
    InstanceID   string            `yaml:"instance_id,omitempty"`
    InstanceTags map[string]string `yaml:"instance_tags,omitempty"`
    RDSHost      string            `yaml:"rds_host"`
    RDSPort      int               `yaml:"rds_port"`
    LocalPort    int               `yaml:"local_port"`
    DBName       string            `yaml:"db_name"`
    DBUser       string            `yaml:"db_user"`
    DBPassword   string            `yaml:"db_password,omitempty"`
    DBPasswordEnv string           `yaml:"db_password_env,omitempty"`
}

func LoadGatewayConfig() (*GatewayConfig, error)
func (g *GatewayEntry) ResolvePassword() string
```

**Dependency:** Add `gopkg.in/yaml.v3` to go.mod.

### Phase 5: UI — Connection Screen & Gateway Info Panel

This phase modifies existing UI code and adds new panels.

#### 5a. Connection Screen (new)

When the app launches with gateway config present, show a connection screen instead of the empty file-drop view. This is a new panel in `internal/ui/`:

```
┌─────────────────────────────────────────────┐
│                                             │
│           Bufflehead Gateway                │
│                                             │
│  ┌───────────────────────────────────────┐  │
│  │  Staging                              │  │
│  │  mydb.cluster-ro-xyz.rds.amazonaws..  │  │
│  │  AWS Profile: staging-readonly        │  │
│  │  Status: ● Credentials valid          │  │
│  │                            [Connect]  │  │
│  └───────────────────────────────────────┘  │
│                                             │
│  ┌───────────────────────────────────────┐  │
│  │  Production                           │  │
│  │  prod-db.cluster-ro-abc.rds.amazon..  │  │
│  │  AWS Profile: prod-readonly           │  │
│  │  Status: ○ Credentials expired        │  │
│  │                          [SSO Login]  │  │
│  └───────────────────────────────────────┘  │
│                                             │
│  ┌───────────────────────────────────────┐  │
│  │  Open Local File...                   │  │
│  │  (Parquet, CSV, DuckDB)               │  │
│  └───────────────────────────────────────┘  │
│                                             │
│  [Edit Gateway Config]                      │
└─────────────────────────────────────────────┘
```

**Behavior:**
- Each gateway card shows: name, RDS host (truncated), AWS profile, credential status
- Credential status is checked on launch and refreshed periodically
- "SSO Login" button runs `aws sso login` and transitions to "Connecting..." once creds are valid
- "Connect" starts the SSM tunnel, waits for port readiness, connects pgx, then transitions to the main SQL editor view
- "Open Local File" preserves existing Bufflehead behavior (DuckDB/Parquet)
- "Edit Gateway Config" opens the YAML config file in the system default editor

#### 5b. Gateway Info Panel (new)

Once connected, add a panel below the connection rail (or as a toggle from the status bar) showing gateway details. This is the key piece that enables users to connect other tools:

```
┌─────────────────────────────────┐
│ Gateway: Staging         [Copy] │
│                                 │
│ Status:    ● Connected          │
│ Endpoint:  localhost:5432       │
│ Database:  cradle               │
│ User:      readonly             │
│ Uptime:    12m 34s              │
│                                 │
│ ── Connect with other tools ──  │
│                                 │
│ psql:                           │
│ ┌─────────────────────────┐     │
│ │ psql -h localhost       │     │
│ │ -p 5432 -U readonly    [📋]│  │
│ │ -d cradle               │     │
│ └─────────────────────────┘     │
│                                 │
│ Connection URL:                 │
│ ┌─────────────────────────┐     │
│ │ postgresql://readonly:  │     │
│ │ @localhost:5432/cradle  [📋]│  │
│ └─────────────────────────┘     │
│                                 │
│ Python:                         │
│ ┌─────────────────────────┐     │
│ │ engine = create_engine( │     │
│ │  "postgresql://readonly │     │
│ │  :@localhost:5432/      │     │
│ │  cradle")              [📋]│  │
│ └─────────────────────────┘     │
│                                 │
│ Claude MCP config:              │
│ ┌─────────────────────────┐     │
│ │ {"mcpServers":{"pg":{  │     │
│ │  "command":"npx",      │     │
│ │  "args":["-y",         │     │
│ │  "@modelcontextprotocol│     │
│ │  /server-postgres",    │     │
│ │  "postgresql://readonly│     │
│ │  :@localhost:5432/     [📋]│  │
│ │  cradle"]}}}            │     │
│ └─────────────────────────┘     │
│                                 │
│              [Disconnect]       │
└─────────────────────────────────┘
```

Each snippet block has a copy-to-clipboard button. The password is excluded from displayed snippets (shown as empty) but included in copied text if `db_password` is configured.

#### 5c. Status Bar Updates

Modify the existing `StatusBar` to show tunnel status when in gateway mode:
- `● Connected to Staging (localhost:5432)` — green dot
- `◐ Connecting to Staging...` — yellow, animated
- `○ Disconnected` — gray
- `● Connected to Staging (localhost:5432) | 1,234 rows | 45ms` — when a query has results

#### 5d. Connection Rail Updates

The existing connection rail (`connRail` in `AppWindow`) gains gateway entries alongside local DuckDB connections. A gateway connection shows:
- Name from config
- Green/yellow/red status indicator
- Click to switch active connection (if multiple gateways are connected simultaneously)

### Phase 6: Integrate with ConnWorker

The existing `ConnWorker` pattern works well. When a gateway connection is established:

1. Create a `PostgresDB` instance connected to `localhost:<local_port>`
2. Wrap it in a `ConnWorker` (since `ConnWorker` takes a `*db.DB`, refactor to accept the `Querier` interface)
3. Add it as a `Connection` in the `AppWindow.connections` slice
4. The rest of the UI (SQL editor, data grid, schema panel, query history) works unchanged

The `Connection` struct in `appwindow.go` currently holds a `*db.DB`. Update it to hold a `Querier` interface instead:

```go
type Connection struct {
    Name    string
    Path    string
    DB      db.Querier  // was *db.DB
    Tables  []db.TableInfo
    button  Button.Instance
    worker  *ConnWorker
    // New gateway fields:
    Gateway *GatewayConnection // nil for local connections
}

type GatewayConnection struct {
    Config  models.GatewayEntry
    Auth    *aws.AuthManager
    Tunnel  *aws.TunnelManager
}
```

### Phase 7: Prerequisites Check

On startup, check for required external tools:
- `aws` CLI — required for SSO login and SSM sessions
- `session-manager-plugin` — required for SSM port forwarding

If missing, show a helpful panel with install instructions:
```
Missing: session-manager-plugin

Install with:
  macOS:   brew install session-manager-plugin
  Linux:   See https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html
  Windows: Download from AWS docs
```

This check only runs if gateway config exists. Pure local Parquet usage doesn't require AWS tools.

---

## New Dependencies

Add to `go.mod`:
```
github.com/jackc/pgx/v5          # Postgres driver
github.com/aws/aws-sdk-go-v2     # AWS SDK (credential checking, EC2 DescribeInstances)
github.com/aws/aws-sdk-go-v2/config
github.com/aws/aws-sdk-go-v2/service/ec2
github.com/aws/aws-sdk-go-v2/service/ssm
gopkg.in/yaml.v3                  # Gateway config parsing
```

Note: The actual SSM session is run via `os/exec` calling the `aws` CLI, not via the SDK. The SDK is used only for credential status checks and EC2 instance discovery by tags.

---

## File Summary

### New files:
```
internal/aws/auth.go          # AWS credential management, SSO login
internal/aws/tunnel.go         # SSM tunnel subprocess management
internal/db/postgres.go        # Postgres driver (alongside duck.go)
internal/db/querier.go         # Querier interface
internal/models/gateway.go     # Gateway config loading/parsing
internal/ui/gateway_screen.go  # Connection screen UI
internal/ui/gateway_info.go    # Gateway info panel (connection snippets)
```

### Modified files:
```
main.go                        # Load gateway config, conditional startup flow
internal/db/duck.go            # Implement Querier interface (no logic changes)
internal/ui/appwindow.go       # Connection type uses Querier; gateway status in rail
internal/ui/connworker.go      # Accept Querier instead of *db.DB
internal/ui/app.go             # Gateway screen routing, menu updates
internal/ui/theme.go           # Colors/styles for gateway status indicators
go.mod                         # New dependencies
```

---

## What This Does NOT Include

- **Embedded AI / text-to-SQL**: The app is a gateway and SQL editor. Users bring their own AI tools (Claude Desktop + MCP, ChatGPT, Cursor, etc.) and connect them to the tunnel endpoint.
- **Database write support**: The app enforces read-only access. The Postgres role and session are read-only.
- **Secrets Manager integration**: Password comes from config or env var. Secrets Manager lookup is a future enhancement.
- **Auto-reconnect on tunnel drop**: Phase 1 shows disconnected status and a reconnect button. Auto-reconnect with backoff is a future enhancement.
- **Multiple simultaneous gateway connections**: Supported by design (different local ports), but not a Phase 1 priority. The UI supports it via the connection rail.
