# MCP Server Mode

Bufflehead can run as an [MCP](https://modelcontextprotocol.io) server so AI
assistants (Claude Desktop, Claude Code, and other MCP clients) can list your
saved connections, read their schemas and run read-only SQL against them.

This is an early version. Set it up from **Help → Connect an AI Assistant…**
(see [Setup](#setup)).

## What it does

The app's own executable serves MCP over stdio when started as:

```
Bufflehead --headless -- --mcp
```

- `--headless` is Godot's flag: no window, no Dock icon.
- `--` ends Godot's arguments; `--mcp` is read by Bufflehead.

In this mode Bufflehead builds no UI and starts no control server. It reads
your **saved bookmarks**, connects to one the first time a tool uses it, and
exits when the client closes the connection. The Bufflehead window does not
need to be open, and running both at once is fine: each process has its own
connections and tunnels.

| Tool | Arguments | Returns |
|---|---|---|
| `list_bookmarks` | none | Label, kind, env, host, database, user, whether SSH is used, whether connected. Never passwords or keys. |
| `get_schema` | `bookmark`, optional `table` | Table and view names; with `table`, that table's columns. |
| `run_sql` | `bookmark`, `sql`, optional `limit` | Columns and rows (100 by default, 1000 max) plus the total row count. |

Postgres connections are opened read-only. Bookmarks that use SSH need the key
unlocked in your ssh-agent or a key file the bookmark points at; there is no
way to type a passphrase in this mode.

## Setup

Save at least one bookmark in the app first, then open **Help → Connect an AI
Assistant…**. The dialog shows the exact command this copy of Bufflehead
should be launched with, and has one row per client:

- **Claude Desktop** (macOS and Windows) — **Add Automatically…** shows the
  entry it will add under `mcpServers` in `claude_desktop_config.json` and
  asks before writing. Every other setting in the file is kept, the old file
  is backed up next to it (`claude_desktop_config.json.bufflehead-backup-<time>`),
  and the new one is written atomically. Then quit Claude Desktop completely
  and reopen it. If you move Bufflehead later, the row offers **Update…** to
  point the entry at the new location.
- **Claude Code** — **Copy Command** copies a `claude mcp add --scope user …`
  line to paste in a terminal.
- **Other MCP clients** — **Copy JSON** copies the `{"command": …, "args": […]}`
  entry for the client's config.

The Claude Desktop row is hidden on Linux (there is no official Claude
Desktop) and in the Mac App Store build (the sandbox can't write Claude's
config); use the manual steps below there.

### Manual setup

If you'd rather edit the config yourself, or the dialog can't, you need the path to the Bufflehead executable itself, not the app bundle or
shortcut.

| OS | Executable path |
|---|---|
| macOS | `/Applications/Bufflehead.app/Contents/MacOS/Bufflehead` |
| Windows | Wherever `Bufflehead.exe` is installed, e.g. `C:\Program Files\Bufflehead\Bufflehead.exe` |
| Linux | The `.AppImage` file itself, e.g. `/home/you/Applications/Bufflehead-x86_64.AppImage` |

On Linux, point at the AppImage file, not a binary inside it: the AppImage's
own launcher sets up the environment the app needs.

#### Claude Desktop

Edit Claude Desktop's config file (create it if it doesn't exist):

- macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`
- Windows: `%APPDATA%\Claude\claude_desktop_config.json`

Add a `bufflehead` entry under `mcpServers`, keeping anything already in the
file:

```json
{
  "mcpServers": {
    "bufflehead": {
      "command": "/Applications/Bufflehead.app/Contents/MacOS/Bufflehead",
      "args": ["--headless", "--", "--mcp"]
    }
  }
}
```

On Windows, escape backslashes in the path (`"C:\\Program Files\\Bufflehead\\Bufflehead.exe"`).

Quit Claude Desktop completely and reopen it. Bufflehead should appear in the
tools menu of a new chat. Try: "What Bufflehead bookmarks do I have?"

#### Claude Code

```bash
claude mcp add --scope user bufflehead -- /Applications/Bufflehead.app/Contents/MacOS/Bufflehead --headless -- --mcp
```

The first `--` separates `claude`'s own options from the command to run.
`--scope user` makes Bufflehead available in every project, not only the
directory you ran the command in.

#### Other MCP clients

Any client that launches stdio servers works. Use the same command and
arguments: `<executable> --headless -- --mcp`.

## Testing without a client

You can drive the server by hand. Each line on stdin is one JSON-RPC message;
each response is one line on stdout. Logs go to stderr.

```bash
{ printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_bookmarks","arguments":{}}}'; sleep 2; } \
  | /Applications/Bufflehead.app/Contents/MacOS/Bufflehead --headless -- --mcp
```

The `sleep` keeps stdin open long enough to answer: the server treats a closed
stdin as the client disconnecting and exits immediately. Increase it for
`run_sql` against a bookmark that needs a tunnel.

## Troubleshooting

- **Bufflehead doesn't show up in Claude Desktop.** Check the JSON is valid and
  the path points at the executable. Claude Desktop logs each server's stderr
  to `~/Library/Logs/Claude/mcp-server-bufflehead.log` (macOS) or
  `%APPDATA%\Claude\logs\mcp-server-bufflehead.log` (Windows).
- **"no SSH agent is running".** Load the key into your agent (`ssh-add`) or set
  the bookmark to use a key file. On macOS, Bufflehead recovers the agent
  socket from launchd when the client doesn't pass it on.
- **`get_schema` returns no tables.** The bookmark has no default database (common
  for MySQL), or points at an empty one (Postgres `postgres`). Set the database
  on the bookmark in the app. A `database` argument is planned.
- **Statements other than `SELECT` fail.** Queries are wrapped in a row count,
  as in the app, so `SHOW`, `EXPLAIN` and writes are not supported.

## Platform status

| | Status |
|---|---|
| macOS | Tested with Claude Desktop. The Claude Code command above is untested. |
| Windows | Not yet tested. Stdio from the Windows build is unverified. |
| Linux | Not yet tested. |
| Mac App Store build | Not yet tested. |

## Design

See [kyleparisi/bufflehead#48](https://github.com/kyleparisi/bufflehead/pull/48)
for why this runs inside the app binary instead of as a separate bridge program.
In short: the client owns both ends of the pipe, so there is no port, key or
discovery file; and the app's own executable already has the database code,
the bookmarks and (on the Mac App Store) the right sandbox container.

Code: `internal/mcpmode/` (server and tools), `internal/connect/` (the
connection pipeline shared with the UI), `internal/mcpsetup/` (the launch
command and the Claude Desktop config merge) and
`internal/ui/mcp_setup_modal.go` (the setup dialog).
