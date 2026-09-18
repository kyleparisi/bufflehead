// Package sshtun forwards a local TCP port to a remote address through an SSH
// jump host — the equivalent of `ssh -L`. It exists so a database that is only
// reachable from inside a network (a Postgres box behind a bastion, say) can be
// dialled by an ordinary driver pointed at 127.0.0.1.
//
// It is deliberately independent of the rest of Bufflehead: callers hand it a
// Config and get back a Tunnel with a local port. Host keys are always verified
// against known_hosts.
package sshtun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// Config describes one `ssh -L localPort:RemoteHost:RemotePort user@Host:Port`.
type Config struct {
	// Jump host.
	Host string
	Port int
	User string

	// Method is one of AuthAgent, AuthKey, or AuthPassword. Secret carries the
	// key passphrase (AuthKey) or the login password (AuthPassword).
	Method  string
	KeyPath string
	Secret  string

	// Forward target, resolved from the jump host's point of view.
	RemoteHost string
	RemotePort int

	// KnownHostsPath overrides ~/.ssh/known_hosts. HostKeyCallback overrides
	// host-key verification outright; both exist for tests.
	KnownHostsPath  string
	HostKeyCallback ssh.HostKeyCallback

	// Timeout bounds the TCP dial and SSH handshake. Zero means DefaultTimeout.
	Timeout time.Duration
}

// DefaultTimeout bounds connecting to the jump host.
const DefaultTimeout = 20 * time.Second

// keepaliveInterval is how often a global keepalive request is sent. Idle SSH
// connections are a favourite victim of NAT and firewall timeouts, and the
// database pool above us assumes the forward stays up between queries.
const keepaliveInterval = 30 * time.Second

// addr renders the jump host as host:port.
func (c Config) addr() string {
	port := c.Port
	if port <= 0 {
		port = 22
	}
	return net.JoinHostPort(c.Host, fmt.Sprint(port))
}

// remoteAddr renders the forward target as host:port.
func (c Config) remoteAddr() string {
	return net.JoinHostPort(c.RemoteHost, fmt.Sprint(c.RemotePort))
}

// Tunnel is a running local-port forward. It stays up until Stop is called or
// the SSH connection dies; Err reports the latter.
type Tunnel struct {
	cfg      Config
	client   *ssh.Client
	listener net.Listener

	mu      sync.Mutex
	closed  bool
	lastErr error
	conns   map[net.Conn]struct{}

	done chan struct{} // closed when the tunnel is no longer usable
	wg   sync.WaitGroup
}

// Start dials the jump host, opens a local listener on an ephemeral port, and
// begins forwarding. It returns once the listener is accepting, so the caller
// can connect to LocalPort immediately.
//
// logf, when non-nil, receives human-readable progress messages.
// Cancelling ctx aborts the connection attempt; it does not stop a running
// tunnel (use Stop for that).
func Start(ctx context.Context, cfg Config, logf func(string)) (*Tunnel, error) {
	log := func(msg string) {
		if logf != nil {
			logf(msg)
		}
	}

	if cfg.Host == "" {
		return nil, errors.New("SSH host is required")
	}
	if cfg.RemoteHost == "" || cfg.RemotePort <= 0 {
		return nil, errors.New("SSH tunnel has no forward target")
	}

	auths, err := cfg.authMethods()
	if err != nil {
		return nil, err
	}
	hostKey, err := cfg.hostKeyCallback()
	if err != nil {
		return nil, err
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	log(fmt.Sprintf("Connecting to SSH host %s...", cfg.addr()))
	client, err := dialSSH(ctx, cfg, auths, hostKey, timeout)
	if err != nil {
		return nil, err
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("open local forward port: %w", err)
	}

	t := &Tunnel{
		cfg:      cfg,
		client:   client,
		listener: listener,
		conns:    make(map[net.Conn]struct{}),
		done:     make(chan struct{}),
	}

	t.wg.Add(2)
	go t.accept()
	go t.keepalive()

	log(fmt.Sprintf("SSH tunnel open: 127.0.0.1:%d → %s", t.LocalPort(), cfg.remoteAddr()))
	return t, nil
}

// dialSSH performs the TCP dial and SSH handshake, honouring ctx. ssh.Dial
// itself takes no context, so the handshake runs in a goroutine that we abandon
// (after closing the socket) if ctx is cancelled first.
func dialSSH(ctx context.Context, cfg Config, auths []ssh.AuthMethod, hostKey ssh.HostKeyCallback, timeout time.Duration) (*ssh.Client, error) {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", cfg.addr())
	if err != nil {
		return nil, fmt.Errorf("dial SSH host %s: %w", cfg.addr(), err)
	}

	clientCfg := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            auths,
		HostKeyCallback: hostKey,
		Timeout:         timeout,
	}

	// Cancelling ctx unblocks the handshake by closing the socket under it.
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, cfg.addr(), clientCfg)
	if err != nil {
		conn.Close()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("SSH handshake with %s failed: %w", cfg.addr(), err)
	}
	return ssh.NewClient(sshConn, chans, reqs), nil
}

// LocalPort returns the port on 127.0.0.1 that forwards to the remote address.
func (t *Tunnel) LocalPort() int {
	if t == nil || t.listener == nil {
		return 0
	}
	addr, ok := t.listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0
	}
	return addr.Port
}

// Err returns the error that took the tunnel down, or nil while it is healthy.
func (t *Tunnel) Err() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastErr
}

// Alive reports whether the tunnel is still forwarding.
func (t *Tunnel) Alive() bool {
	if t == nil {
		return false
	}
	select {
	case <-t.done:
		return false
	default:
		return true
	}
}

// Describe renders the forward for status lines and logs.
func (t *Tunnel) Describe() string {
	if t == nil {
		return ""
	}
	return fmt.Sprintf("%s → %s via %s", t.listener.Addr(), t.cfg.remoteAddr(), t.cfg.addr())
}

// Stop closes the listener, every forwarded connection, and the SSH client.
// It is safe to call more than once and from any goroutine.
func (t *Tunnel) Stop() error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	conns := make([]net.Conn, 0, len(t.conns))
	for c := range t.conns {
		conns = append(conns, c)
	}
	t.conns = nil
	close(t.done)
	t.mu.Unlock()

	t.listener.Close()
	for _, c := range conns {
		c.Close()
	}
	err := t.client.Close()
	t.wg.Wait()
	return err
}

// fail records why the tunnel died and tears it down. The first error wins —
// later ones are just fallout from the teardown.
func (t *Tunnel) fail(err error) {
	t.mu.Lock()
	if t.closed || t.lastErr != nil {
		t.mu.Unlock()
		return
	}
	t.lastErr = err
	t.mu.Unlock()

	log.Printf("ssh: tunnel to %s failed: %v", t.cfg.addr(), err)
	go t.Stop()
}

// accept forwards each inbound local connection over the SSH client.
func (t *Tunnel) accept() {
	defer t.wg.Done()
	for {
		local, err := t.listener.Accept()
		if err != nil {
			if !t.Alive() {
				return // ordinary shutdown
			}
			t.fail(fmt.Errorf("accept on local forward port: %w", err))
			return
		}
		t.wg.Add(1)
		go t.forward(local)
	}
}

// forward opens a channel to the remote address and pipes bytes both ways.
func (t *Tunnel) forward(local net.Conn) {
	defer t.wg.Done()
	defer local.Close()

	remote, err := t.client.Dial("tcp", t.cfg.remoteAddr())
	if err != nil {
		// A failure here is usually the database refusing the connection (wrong
		// port, not listening) rather than the tunnel dying, so the forward is
		// dropped without taking the tunnel with it. The driver surfaces it.
		log.Printf("ssh: forward to %s failed: %v", t.cfg.remoteAddr(), err)
		return
	}
	defer remote.Close()

	if !t.track(local, remote) {
		return // stopped while we were dialling
	}
	defer t.untrack(local, remote)

	// Copy in both directions; the first side to finish ends the pair.
	done := make(chan struct{}, 2)
	pipe := func(dst, src net.Conn) {
		io.Copy(dst, src)
		done <- struct{}{}
	}
	go pipe(remote, local)
	go pipe(local, remote)
	<-done
}

// track registers connections for shutdown, reporting false if the tunnel has
// already stopped (in which case the caller closes them itself).
func (t *Tunnel) track(conns ...net.Conn) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return false
	}
	for _, c := range conns {
		t.conns[c] = struct{}{}
	}
	return true
}

func (t *Tunnel) untrack(conns ...net.Conn) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.conns == nil {
		return
	}
	for _, c := range conns {
		delete(t.conns, c)
	}
}

// keepalive pings the server so idle NAT/firewall entries don't silently drop
// the connection, and notices promptly when the server goes away.
func (t *Tunnel) keepalive() {
	defer t.wg.Done()
	ticker := time.NewTicker(keepaliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-t.done:
			return
		case <-ticker.C:
			if _, _, err := t.client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
				t.fail(fmt.Errorf("SSH connection to %s lost: %w", t.cfg.addr(), err))
				return
			}
		}
	}
}
