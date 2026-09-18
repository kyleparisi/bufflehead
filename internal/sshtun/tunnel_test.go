package sshtun

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// --- test doubles -----------------------------------------------------------

// echoServer stands in for the database behind the jump host: it echoes back
// whatever it is sent, so a round trip proves bytes crossed the tunnel.
func echoServer(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("echo listen: %v", err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				io.Copy(c, c)
			}()
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return ln
}

// newKey returns a fresh ed25519 SSH key as both a signer and OpenSSH PEM bytes.
func newKey(t *testing.T) (ssh.Signer, []byte) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	return signer, pem.EncodeToMemory(block)
}

// sshServer is a minimal SSH server that accepts one public key and serves
// direct-tcpip (port forward) channels. It is enough to exercise a real
// handshake and a real forward without needing sshd.
type sshServer struct {
	ln         net.Listener
	hostKey    ssh.Signer
	authorized ssh.PublicKey
}

func startSSHServer(t *testing.T, hostKey, clientKey ssh.Signer) *sshServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ssh listen: %v", err)
	}
	s := &sshServer{ln: ln, hostKey: hostKey, authorized: clientKey.PublicKey()}
	go s.serve()
	t.Cleanup(func() { ln.Close() })
	return s
}

func (s *sshServer) addr() string { return s.ln.Addr().String() }

func (s *sshServer) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

func (s *sshServer) serve() {
	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if string(key.Marshal()) == string(s.authorized.Marshal()) {
				return &ssh.Permissions{}, nil
			}
			return nil, fmt.Errorf("unauthorized key")
		},
	}
	cfg.AddHostKey(s.hostKey)

	for {
		nConn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handleConn(nConn, cfg)
	}
}

func (s *sshServer) handleConn(nConn net.Conn, cfg *ssh.ServerConfig) {
	conn, chans, reqs, err := ssh.NewServerConn(nConn, cfg)
	if err != nil {
		nConn.Close()
		return
	}
	defer conn.Close()
	// Answer keepalives so the tunnel's keepalive goroutine stays happy.
	go func() {
		for req := range reqs {
			if req.WantReply {
				req.Reply(false, nil)
			}
		}
	}()

	for newChan := range chans {
		if newChan.ChannelType() != "direct-tcpip" {
			newChan.Reject(ssh.UnknownChannelType, "only direct-tcpip is supported")
			continue
		}
		go serveForward(newChan)
	}
}

// directTCPIP is the payload of a direct-tcpip channel open request (RFC 4254 §7.2).
type directTCPIP struct {
	DestHost string
	DestPort uint32
	SrcHost  string
	SrcPort  uint32
}

func serveForward(newChan ssh.NewChannel) {
	var req directTCPIP
	if err := ssh.Unmarshal(newChan.ExtraData(), &req); err != nil {
		newChan.Reject(ssh.ConnectionFailed, "bad direct-tcpip payload")
		return
	}
	target, err := net.Dial("tcp", net.JoinHostPort(req.DestHost, fmt.Sprint(req.DestPort)))
	if err != nil {
		newChan.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	ch, chReqs, err := newChan.Accept()
	if err != nil {
		target.Close()
		return
	}
	go ssh.DiscardRequests(chReqs)
	go func() {
		defer ch.Close()
		defer target.Close()
		io.Copy(target, ch)
	}()
	go func() {
		defer ch.Close()
		defer target.Close()
		io.Copy(ch, target)
	}()
}

// writeKnownHosts records the server's host key so verification succeeds.
func writeKnownHosts(t *testing.T, addr string, hostKey ssh.Signer) string {
	t.Helper()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split addr: %v", err)
	}
	pattern := host
	if port != "22" {
		pattern = fmt.Sprintf("[%s]:%s", host, port)
	}
	path := filepath.Join(t.TempDir(), "known_hosts")
	line := fmt.Sprintf("%s %s\n", pattern, strings.TrimSpace(string(ssh.MarshalAuthorizedKey(hostKey.PublicKey()))))
	if err := os.WriteFile(path, []byte(line), 0600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}
	return path
}

// testConfig wires a Config at a running server + echo target, authenticating
// with a key file written to a temp dir.
func testConfig(t *testing.T, srv *sshServer, hostKey ssh.Signer, clientPEM []byte, target net.Listener) Config {
	t.Helper()
	keyPath := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(keyPath, clientPEM, 0600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return Config{
		Host:           "127.0.0.1",
		Port:           srv.port(),
		User:           "tester",
		Method:         AuthKey,
		KeyPath:        keyPath,
		RemoteHost:     "127.0.0.1",
		RemotePort:     target.Addr().(*net.TCPAddr).Port,
		KnownHostsPath: writeKnownHosts(t, srv.addr(), hostKey),
		Timeout:        5 * time.Second,
	}
}

// --- tests ------------------------------------------------------------------

// TestStartForwardsBytes is the end-to-end case: a driver dialling the tunnel's
// local port reaches the service behind the jump host.
func TestStartForwardsBytes(t *testing.T) {
	hostKey, _ := newKey(t)
	clientKey, clientPEM := newKey(t)
	echo := echoServer(t)
	srv := startSSHServer(t, hostKey, clientKey)

	tun, err := Start(context.Background(), testConfig(t, srv, hostKey, clientPEM, echo), nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tun.Stop()

	if tun.LocalPort() == 0 {
		t.Fatal("LocalPort is 0")
	}

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", tun.LocalPort()))
	if err != nil {
		t.Fatalf("dial tunnel: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	const msg = "select 1"
	if _, err := conn.Write([]byte(msg)); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != msg {
		t.Errorf("echo = %q, want %q", buf, msg)
	}
	if !tun.Alive() {
		t.Error("tunnel should be alive")
	}
	if err := tun.Err(); err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
}

// TestStopClosesListener verifies Stop tears the forward down so the port stops
// accepting — a connection close must not leave a listener behind.
func TestStopClosesListener(t *testing.T) {
	hostKey, _ := newKey(t)
	clientKey, clientPEM := newKey(t)
	echo := echoServer(t)
	srv := startSSHServer(t, hostKey, clientKey)

	tun, err := Start(context.Background(), testConfig(t, srv, hostKey, clientPEM, echo), nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	port := tun.LocalPort()

	if err := tun.Stop(); err != nil && !strings.Contains(err.Error(), "closed") {
		t.Fatalf("Stop: %v", err)
	}
	if tun.Alive() {
		t.Error("tunnel should not be alive after Stop")
	}
	if err := tun.Stop(); err != nil {
		t.Errorf("second Stop: %v", err)
	}

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err == nil {
		conn.Close()
		t.Error("local port still accepting after Stop")
	}
}

// TestUnknownHostKeyIsRefused is the security case: a host absent from
// known_hosts must not connect, and the error must say how to trust it.
func TestUnknownHostKeyIsRefused(t *testing.T) {
	hostKey, _ := newKey(t)
	clientKey, clientPEM := newKey(t)
	echo := echoServer(t)
	srv := startSSHServer(t, hostKey, clientKey)

	cfg := testConfig(t, srv, hostKey, clientPEM, echo)
	// Empty known_hosts: the host key is valid but has never been trusted.
	empty := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(empty, nil, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg.KnownHostsPath = empty

	tun, err := Start(context.Background(), cfg, nil)
	if err == nil {
		tun.Stop()
		t.Fatal("expected unknown host key to be refused")
	}
	if !strings.Contains(err.Error(), "not in") || !strings.Contains(err.Error(), "ssh 127.0.0.1") {
		t.Errorf("error should name known_hosts and the ssh command to fix it, got: %v", err)
	}
}

// TestMismatchedHostKeyIsRefused covers the alarming case: a recorded key that
// no longer matches must fail loudly rather than connect.
func TestMismatchedHostKeyIsRefused(t *testing.T) {
	hostKey, _ := newKey(t)
	otherKey, _ := newKey(t)
	clientKey, clientPEM := newKey(t)
	echo := echoServer(t)
	srv := startSSHServer(t, hostKey, clientKey)

	cfg := testConfig(t, srv, hostKey, clientPEM, echo)
	cfg.KnownHostsPath = writeKnownHosts(t, srv.addr(), otherKey)

	tun, err := Start(context.Background(), cfg, nil)
	if err == nil {
		tun.Stop()
		t.Fatal("expected mismatched host key to be refused")
	}
	if !strings.Contains(err.Error(), "HOST KEY MISMATCH") {
		t.Errorf("error should flag the mismatch, got: %v", err)
	}
	if !strings.Contains(err.Error(), "ssh-keygen -R") {
		t.Errorf("error should suggest ssh-keygen -R, got: %v", err)
	}
}

// TestMissingKnownHostsFile checks the first-run case: no known_hosts at all.
func TestMissingKnownHostsFile(t *testing.T) {
	cfg := Config{
		Host:           "db.example.com",
		Port:           2222,
		User:           "tester",
		Method:         AuthAgent,
		RemoteHost:     "127.0.0.1",
		RemotePort:     5432,
		KnownHostsPath: filepath.Join(t.TempDir(), "nope", "known_hosts"),
	}
	if _, err := cfg.hostKeyCallback(); err == nil {
		t.Fatal("expected an error for a missing known_hosts file")
	} else if !strings.Contains(err.Error(), "db.example.com:2222") {
		t.Errorf("error should name the host, got: %v", err)
	}
}

// TestWrongKeyIsRejected verifies authentication actually happens.
func TestWrongKeyIsRejected(t *testing.T) {
	hostKey, _ := newKey(t)
	clientKey, _ := newKey(t)
	_, otherPEM := newKey(t)
	echo := echoServer(t)
	srv := startSSHServer(t, hostKey, clientKey)

	cfg := testConfig(t, srv, hostKey, otherPEM, echo)
	tun, err := Start(context.Background(), cfg, nil)
	if err == nil {
		tun.Stop()
		t.Fatal("expected an unauthorized key to be rejected")
	}
	if !strings.Contains(err.Error(), "handshake") {
		t.Errorf("error should mention the handshake, got: %v", err)
	}
}

// TestStartValidation covers the cheap guards before any network work.
func TestStartValidation(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{"no host", Config{RemoteHost: "db", RemotePort: 5432}, "SSH host is required"},
		{"no target", Config{Host: "jump"}, "no forward target"},
		{"password without secret", Config{
			Host: "jump", RemoteHost: "db", RemotePort: 5432, Method: AuthPassword,
		}, "no password was provided"},
		{"key without path", Config{
			Host: "jump", RemoteHost: "db", RemotePort: 5432, Method: AuthKey,
		}, "empty path"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Start(context.Background(), tc.cfg, nil)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

// TestEncryptedKeyNeedsPassphrase checks the message a user gets when their key
// is passphrase-protected and they left the field blank.
func TestEncryptedKeyNeedsPassphrase(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	block, err := ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte("hunter2"))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := loadKeyFile(path, ""); err == nil {
		t.Fatal("expected an error for an encrypted key with no passphrase")
	} else if !strings.Contains(err.Error(), "passphrase") {
		t.Errorf("error should mention the passphrase, got: %v", err)
	}

	if _, err := loadKeyFile(path, "hunter2"); err != nil {
		t.Errorf("loadKeyFile with the right passphrase: %v", err)
	}
	if _, err := loadKeyFile(path, "wrong"); err == nil {
		t.Error("expected an error for the wrong passphrase")
	}
}

// TestExpandPath covers the "~" handling used by the key and known_hosts paths.
func TestExpandPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	got, err := ExpandPath("~/.ssh/id_rsa")
	if err != nil {
		t.Fatalf("ExpandPath: %v", err)
	}
	if want := filepath.Join(home, ".ssh", "id_rsa"); got != want {
		t.Errorf("ExpandPath(~/.ssh/id_rsa) = %q, want %q", got, want)
	}
	if got, _ := ExpandPath("/abs/path"); got != "/abs/path" {
		t.Errorf("absolute path should be unchanged, got %q", got)
	}
	if _, err := ExpandPath(""); err == nil {
		t.Error("empty path should error")
	}
}

// TestHostForKeygen checks the ssh-keygen -R argument we print on a mismatch.
func TestHostForKeygen(t *testing.T) {
	if got := hostForKeygen("db.example.com:22"); got != "db.example.com" {
		t.Errorf("default port should be bare, got %q", got)
	}
	if got := hostForKeygen("db.example.com:2222"); got != "[db.example.com]:2222" {
		t.Errorf("non-default port should be bracketed, got %q", got)
	}
}
