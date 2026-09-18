# SSH Tunnels

How Bufflehead reaches a database that is only routable from inside a network —
a Postgres box behind a jump host, a MySQL server on a private subnet — using
the SSH access the user already has.

## The problem

Not every private database sits behind an AWS SSM bastion. The far more common
arrangement is an ordinary SSH jump host: the database listens on a private
address, and the only way in is `ssh -L 5432:db.internal:5432 bastion`, run in a
terminal that then has to stay open for as long as the query session lasts.

Bufflehead does that port forward itself, so the connection is a property of the
saved connection rather than a terminal the user has to remember to keep alive.

## Design

### An SSH tunnel is a transport, not a connection kind

The central decision. `models.ConnKind` describes *what* you are talking to —
Postgres, MySQL, BigQuery, an AWS gateway. How the bytes get there is a separate
question, so the tunnel rides on `GatewayEntry.SSH` (`*models.SSHTunnel`)
alongside the kind rather than becoming `ssh_postgres`, `ssh_mysql`, and one
more kind for every engine added later.

The practical payoff is that the dispatch sites scattered across
`internal/ui/` — `IsDirect()`, `IsMySQL()`, the reconnect branches, the
breadcrumb — stay exactly as they were. Only one thing changes: where the driver
dials.

```go
// DialTarget returns the host and port the database driver should connect to.
func (g *GatewayEntry) DialTarget() (string, int, error) {
	if g.UsesSSH() {
		if g.LocalPort <= 0 {
			return "", 0, fmt.Errorf(...) // the tunnel is not up; see below
		}
		return "127.0.0.1", g.LocalPort, nil
	}
	return g.RDSHost, g.RDSPort, nil
}
```

`SupportsSSHTunnel()` limits tunnels to the kinds where one means something:
direct Postgres and direct MySQL. An AWS gateway already tunnels over SSM, and
BigQuery is an HTTPS API with no host to forward.

### The tunnel itself

`internal/sshtun` is a self-contained `ssh -L`: it dials the jump host, listens
on an ephemeral port on 127.0.0.1, and opens a `direct-tcpip` channel per
inbound connection. It knows nothing about Bufflehead — callers hand it a
`Config` and get back a `Tunnel` with a `LocalPort()`.

Two details worth knowing:

- **Keepalives.** A `keepalive@openssh.com` request goes out every 30s. Idle SSH
  connections are a favourite victim of NAT and firewall timeouts, and the
  database pool above the tunnel assumes the forward survives between queries.
- **No reconnect loop.** Unlike the SSM tunnel, a dropped SSH forward does not
  re-establish itself — the credentials to do so (an agent, a passphrase) may
  not still be available. Instead the connection is marked dead: the rail tile
  turns red and the status bar says to use Reconnect, which rebuilds the tunnel
  from scratch through the same path as the initial connect.

### Authentication

Three methods, matching what people already have configured:

| Method | What it uses |
| --- | --- |
| **SSH Agent** (default) | Keys held by the running agent via `SSH_AUTH_SOCK`, falling back to the default identity files (`~/.ssh/id_ed25519`, `id_ecdsa`, `id_rsa`) |
| **Key File** | One named private key, decrypted with a stored passphrase when encrypted |
| **Password** | A login password, offered both as `password` and `keyboard-interactive` since many servers only accept the latter |

The agent path deliberately returns *several* auth methods — the SSH protocol
tries each in turn, so an agent holding the wrong key still falls through to the
on-disk identities. On Windows the agent is a named pipe that Go's `net.Dial`
cannot reach; that failure is treated as "no agent" rather than an error, so the
on-disk identities still work.

### Host keys are always verified

`known_hosts` verification is not optional and there is no "ignore host key"
switch. A jump host that can be impersonated is a jump host that can read every
query and credential crossing it, and an escape hatch in a GUI is one that gets
clicked.

What replaces it is error text that says exactly how to proceed. An unknown host
is a one-time `ssh` away from being trusted:

```
host key for bastion.example.com:22 is not in /Users/me/.ssh/known_hosts.

Bufflehead only connects to SSH hosts you have already trusted. Connect once
from a terminal:
  ssh bastion.example.com
accept the fingerprint, then try again.
```

A *changed* key is the case that deserves alarm, and is reported as such
(`HOST KEY MISMATCH`, with the `ssh-keygen -R` command to clear the old entry if
the host legitimately changed).

### Secrets

The key passphrase or login password follows the same rule as the database
password: never written to disk. It lives in the OS keychain under
`SSHSecretLabel(label)` — deliberately distinct from the bookmark label used for
the database password, so the two can never collide — or in a named environment
variable. The in-memory `Secret` field carries `yaml:"-" json:"-"` tags, and
`models.TestSSHSecretNeverPersisted` asserts it stays out of both `gateway.yaml`
and `bookmarks.json`.

## Connection flow

```
Form / saved bookmark
        ↓
establishSSHTunnel()        ← nil tunnel, nil error when no jump host
        ↓                     sets entry.LocalPort
openDirectDB(entry)         ← dials entry.DialTarget()
        ↓
Querier + tables → the connection
```

`establishSSHTunnel` is safe to call unconditionally: an entry with no jump host
passes straight through. That is what lets the initial connect
(`RunOpenDirect`), the reconnect path, and the database switcher all share one
shape without each testing for tunnels.

A failure anywhere after the tunnel is up stops it, so a half-open connection
never leaks a forward.

### The local port must get back to the connection

Establishing the tunnel assigns `entry.LocalPort`, and that happens inside a
background goroutine on a *copy* of the entry. The updated copy has to be
carried back to the main thread (`DBResult.Entry` / `ReconnectOutcome.Entry`)
and stored on `conn.Gateway.Config`, because operations that reuse a standing
tunnel — `switchDatabase` above all — re-dial from that stored config.

This is not a tidiness point. It caused a real wrong-database connection: a
connection whose database sat on the jump host was configured, correctly, as
`localhost:5432`. The local port was lost on the way back, so a later database
switch read `LocalPort == 0`, and `DialTarget` fell back to `RDSHost:RDSPort` —
which on the user's own machine is *their* Postgres. The switch connected to the
wrong database entirely and surfaced as a confusing
`role "kyle" does not exist`.

Both halves are now closed: the entry is propagated, and `DialTarget` returns an
error rather than a fallback when a tunnel is required but absent
(`TestDialTargetRefusesUnestablishedTunnel`). A tunneled connection can now fail
to connect, but it can never connect to the wrong thing.

## Using it

In the connection screen, pick **PostgreSQL** or **MySQL**, fill in the database
details as normal, then switch the **SSH Tunnel** section from *Direct* to *Via
SSH* and give it the jump host. The database host and port stay what they are
from the jump host's point of view — Bufflehead forwards a local port to them,
so `Host` is still `db.internal`, not `localhost`.

Saved connections remember the tunnel; their cards carry an `SSH` badge and name
the hop.

### SSL/TLS through a tunnel

The driver connects to `127.0.0.1`, so a TLS mode that verifies the server's
hostname against its certificate will not match. The default modes (`prefer` for
Postgres, `preferred` for MySQL) are unaffected. The SSH transport is itself
encrypted, so this costs nothing in practice.

## Files

| Path | What it holds |
| --- | --- |
| `internal/sshtun/tunnel.go` | The forward: dial, listen, per-connection channels, keepalive |
| `internal/sshtun/auth.go` | Agent / key file / password auth, `~` expansion |
| `internal/sshtun/hostkey.go` | `known_hosts` verification and its error messages |
| `internal/models/ssh.go` | `SSHTunnel` — the saved configuration and its secret resolution |
| `internal/ui/ssh_section.go` | The form section shared by the Postgres and MySQL forms |
| `internal/ui/gateway_connect.go` | `establishSSHTunnel`, `openDirectDB` |
