package sshtun

import (
	"bufflehead/internal/sandbox"
	"golang.org/x/crypto/ssh"
	"net"
	"os"
	"testing"
	"time"
)

// Explicit opt-in live spot check. Never uses disk-key fallback or agent forwarding.
func TestMASLiveAgent(t *testing.T) {
	if os.Getenv("SPIKE_REAL_AGENT_PROBE") != "1" {
		t.Skip("explicit live probe only")
	}
	host, user := os.Getenv("SPIKE_AGENT_HOST"), os.Getenv("SPIKE_AGENT_USER")
	if host == "" || user == "" {
		t.Fatal("SPIKE_AGENT_HOST and SPIKE_AGENT_USER are required")
	}
	expected := os.Getenv("SPIKE_EXPECT_SANDBOX") == "1"
	if sandbox.Enabled() != expected {
		t.Fatal("unexpected sandbox state")
	}
	t.Logf("sandbox=%v", sandbox.Enabled())
	if expected {
		if _, err := os.ReadFile(os.Getenv("SPIKE_OUTSIDE_FILE")); !os.IsPermission(err) {
			t.Fatalf("negative sandbox control failed: %v", err)
		}
	}
	verify, err := (Config{Host: host, Port: 22, KnownHostsPath: os.Getenv("SPIKE_AGENT_KNOWN_HOSTS")}).hostKeyCallback()
	if err != nil {
		t.Fatal(err)
	}
	auth, err := agentAuth()
	if err != nil {
		t.Fatalf("agent socket access: %v", err)
	}
	t.Log("agent socket connected; authenticating with agent only")
	conn, err := ssh.Dial("tcp", net.JoinHostPort(host, "22"), &ssh.ClientConfig{User: user, Auth: []ssh.AuthMethod{auth}, HostKeyCallback: verify, Timeout: 15 * time.Second})
	if err != nil {
		t.Fatalf("verified agent-only SSH handshake: %v", err)
	}
	defer conn.Close()
	session, err := conn.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	done := make(chan error, 1)
	go func() {
		out, e := session.Output("printf bufflehead-agent-ok")
		if e == nil && string(out) != "bufflehead-agent-ok" {
			e = errUnexpectedAgentProbeOutput{}
		}
		done <- e
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("read-only command timed out")
	}
	t.Log("PASS: host verified, agent-only authentication, read-only command")
}

type errUnexpectedAgentProbeOutput struct{}

func (errUnexpectedAgentProbeOutput) Error() string { return "unexpected spot-check output" }
