# Bufflehead as an MCP server

Bufflehead exposes whatever you have open — files, folders, database files,
Postgres/MySQL/BigQuery connections — to MCP clients such as Claude Desktop
and Claude Code, through six read-only data tools. The app keeps owning every
credential, tunnel and connection; the client only ever sees query results.

## Setup

### Claude Desktop

Claude Desktop launches local MCP servers as stdio subprocesses. Bufflehead is
a GUI app, so it ships a small bridge binary, `bufflehead-mcp`, next to the app
executable. Add it to `claude_desktop_config.json` (**File → Copy Claude MCP
Config** in Bufflehead puts the right snippet on the clipboard):

```json
{
  "mcpServers": {
    "bufflehead": {
      "command": "/Applications/Bufflehead.app/Contents/MacOS/bufflehead-mcp"
    }
  }
}
```

Restart Claude Desktop. The entry is static: it keeps working across
Bufflehead restarts because the bridge discovers the running app each time it
starts (see *How discovery works*). If Bufflehead isn't running, the bridge
exits with a clear message and Claude Desktop shows the server as unavailable;
start Bufflehead and reconnect.

### Claude Code

Either the bridge over stdio:

```bash
claude mcp add bufflehead -- /Applications/Bufflehead.app/Contents/MacOS/bufflehead-mcp
```

or the in-app HTTP endpoint directly, using the port and key Bufflehead prints
on stdout (`Control server: …` / `Control key: …`):

```bash
claude mcp add --transport http bufflehead http://127.0.0.1:<port>/mcp \
  --header "Authorization: Bearer <key>"
```

The HTTP form has to be re-added after every Bufflehead launch (port and key
rotate); the bridge form does not.

### Linux and Windows

The bridge ships beside the app binary: `bufflehead-mcp.exe` next to
`Bufflehead.exe` on Windows, and `usr/bin/bufflehead-mcp` inside the Linux
AppImage. An AppImage mounts at a temporary path, so for Claude Desktop on
Linux extract it once (`./Bufflehead.AppImage --appimage-extract`) and point
the config at `squashfs-root/usr/bin/bufflehead-mcp`, or copy that file
somewhere stable.

### Development (`gd run`)

```bash
go build -o bufflehead-mcp ./cmd/bufflehead-mcp
./bufflehead-mcp --check          # finds the running app, verifies the key
./bufflehead-mcp --print-config   # claude_desktop_config.json snippet for this build
```

The bridge honours `BUFFLEHEAD_CONTROL_ADDR` + `BUFFLEHEAD_CONTROL_KEY` to
target a specific instance explicitly (the integration harness does this),
and `BUFFLEHEAD_CONFIG_DIR` to relocate the discovery file.

## Tools

| Tool | Arguments | What it does |
| --- | --- | --- |
| `list_connections` | – | Every open connection: name, kind, path, dialect, default row limit, whether cancel/S3 are supported. Call it first. |
| `get_schema` | `connection?`, `table?` | Tables and columns for one connection. For the in-memory connection the "tables" are the open files, by path; for a folder they are glob patterns. |
| `run_sql` | `sql`, `connection?`, `limit?` | Run a query; returns columns/rows (structured) plus a markdown rendering. Local connections return every row up to 10,000; remote ones default to 100. |
| `cancel_sql` | `connection?` | Cancel the running query (BigQuery only). |
| `get_s3_object` | `bucket`, `key`, `connection?`, `region?`, `max_bytes?` | Fetch an S3 object with an AWS gateway connection's credentials. |
| `reconnect` | `connection?` | Tear down and rebuild a remote connection (tunnel, pool, credentials); reports each step. |

Connection kinds: `memory`, `duckdb`, `sqlite`, `folder`, `postgres`,
`aws-postgres`, `mysql`, `bigquery`. Errors (bad SQL, unknown connection, busy
server) come back as tool errors the model can read and correct, never as
protocol failures.

The same tool set is served two ways from one implementation
(`internal/mcpserver`): in-process at `/mcp` on the control server, and by the
bridge over stdio, which calls the control API's REST endpoints. Both share
the two-query concurrency cap; a third concurrent `run_sql` is told to retry.

## How discovery works

On startup Bufflehead writes `control.json` to its config dir
(`~/Library/Application Support/Bufflehead` on macOS, `%AppData%\Bufflehead`
on Windows, `~/.config/bufflehead` on Linux):

```json
{ "addr": "127.0.0.1:54321", "key": "…", "pid": 12345, "version": "0.29.0" }
```

The file is created owner-only (0600) and removed when the app exits. The
bridge reads it, checks the pid is alive, and pings `/state` with the key
before serving anything, so a stale file fails fast with a "not running"
message instead of hanging.

**Security note.** The control key otherwise never touches disk. With
discovery it lives in that owner-only file for the lifetime of the app
process; anything running as your user could read it, which is the same
trust boundary as your bookmarks and query history in the same directory.
If two Bufflehead instances run at once, the last one to start owns the file
and each instance only removes its own on exit.

## Endpoints added to the control API

- `GET /connections[?columns=1]` — the snapshot `list_connections` and
  `get_schema` are built on.
- `POST /mcp` — MCP Streamable HTTP (stateless), behind the same bearer key.
