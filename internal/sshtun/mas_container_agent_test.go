package sshtun

import (
	"bufflehead/internal/sandbox"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// Opt-in component probe against a separately launched disposable agent.
// Never point SPIKE_CONTAINER_AGENT at the user's normal agent.
func TestMASContainerAgent(t *testing.T) {
	if os.Getenv("SPIKE_CONTAINER_AGENT") != "1" {
		t.Skip("explicit disposable agent probe only")
	}
	if !sandbox.Enabled() {
		t.Fatal("sandbox must be enforced")
	}
	if _, err := os.ReadFile(os.Getenv("SPIKE_OUTSIDE_FILE")); !os.IsPermission(err) {
		t.Fatalf("ungranted-file negative control: %v", err)
	}
	c, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	a := agent.NewClient(c)
	keys, err := a.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Fatal("expected empty disposable agent; refusing mutation")
	}
	clientKey, pem := newKey(t)
	private, err := ssh.ParseRawPrivateKey(pem)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Add(agent.AddedKey{PrivateKey: private, Comment: "disposable-sandbox-probe"}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := a.Remove(clientKey.PublicKey()); err != nil {
			t.Error(err)
		}
	}()
	hostKey, _ := newKey(t)
	srv := startSSHServer(t, hostKey, clientKey)
	echo := echoServer(t)
	verify, err := (Config{Host: "127.0.0.1", Port: srv.port(), KnownHostsPath: writeKnownHosts(t, srv.addr(), hostKey)}).hostKeyCallback()
	if err != nil {
		t.Fatal(err)
	}
	// Production agent-only method: no private key or disk fallback in Auth.
	auth, err := agentAuth()
	if err != nil {
		t.Fatal(err)
	}
	conn, err := ssh.Dial("tcp", srv.addr(), &ssh.ClientConfig{User: "tester", Auth: []ssh.AuthMethod{auth}, HostKeyCallback: verify, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	forward, err := conn.Dial("tcp", echo.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer forward.Close()
	const message = "sandbox-agent-forward-ok"
	if _, err := forward.Write([]byte(message)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(message))
	if _, err := io.ReadFull(forward, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != message {
		t.Fatal("forwarded payload mismatch")
	}
	t.Log("PASS: enforced sandbox, container-local agent, host-verified agent-only authentication, SSH forwarded roundtrip")
}
