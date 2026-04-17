package aws

import (
	"bufio"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// TunnelStatus represents the state of an SSM tunnel.
type TunnelStatus int

const (
	TunnelDisconnected TunnelStatus = iota
	TunnelConnecting
	TunnelConnected
	TunnelError
)

func (s TunnelStatus) String() string {
	switch s {
	case TunnelDisconnected:
		return "Disconnected"
	case TunnelConnecting:
		return "Connecting"
	case TunnelConnected:
		return "Connected"
	case TunnelError:
		return "Error"
	default:
		return "Unknown"
	}
}

// TunnelConfig holds the parameters for an SSM port-forwarding session.
type TunnelConfig struct {
	InstanceID string
	RDSHost    string
	RDSPort    int
	LocalPort  int
	AWSProfile string
	AWSRegion  string
}

// TunnelManager manages an SSM port-forwarding subprocess.
type TunnelManager struct {
	mu        sync.Mutex
	process   *exec.Cmd
	localPort int
	status    TunnelStatus
	lastError string
	onStatus  func(TunnelStatus)
	stopCh    chan struct{}
}

// NewTunnelManager creates a new tunnel manager.
// onStatus is called (from a goroutine) whenever the tunnel status changes.
func NewTunnelManager(onStatus func(TunnelStatus)) *TunnelManager {
	return &TunnelManager{
		status:   TunnelDisconnected,
		onStatus: onStatus,
	}
}

// Status returns the current tunnel status.
func (t *TunnelManager) Status() TunnelStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.status
}

// LastError returns the last error message, if any.
func (t *TunnelManager) LastError() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastError
}

// LocalPort returns the local port the tunnel is bound to.
func (t *TunnelManager) LocalPort() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.localPort
}

func (t *TunnelManager) setStatus(s TunnelStatus) {
	t.status = s
	if t.onStatus != nil {
		t.onStatus(s)
	}
}

// Start launches the SSM port-forwarding session as a subprocess.
//
//	aws ssm start-session \
//	  --target <instance-id> \
//	  --document-name AWS-StartPortForwardingSessionToRemoteHost \
//	  --parameters '{"host":["<rds-host>"],"portNumber":["<rds-port>"],"localPortNumber":["<local-port>"]}' \
//	  --profile <profile> \
//	  --region <region>
//
// Monitors stdout for "Port <port> opened" to confirm connection.
// On unexpected exit, updates status.
func (t *TunnelManager) Start(cfg TunnelConfig) error {
	t.mu.Lock()
	if t.status == TunnelConnecting || t.status == TunnelConnected {
		t.mu.Unlock()
		return fmt.Errorf("tunnel already active")
	}
	t.localPort = cfg.LocalPort
	t.lastError = ""
	t.setStatus(TunnelConnecting)
	t.stopCh = make(chan struct{})
	t.mu.Unlock()

	params := fmt.Sprintf(`{"host":["%s"],"portNumber":["%d"],"localPortNumber":["%d"]}`,
		cfg.RDSHost, cfg.RDSPort, cfg.LocalPort)

	args := []string{
		"ssm", "start-session",
		"--target", cfg.InstanceID,
		"--document-name", "AWS-StartPortForwardingSessionToRemoteHost",
		"--parameters", params,
	}
	if cfg.AWSProfile != "" {
		args = append(args, "--profile", cfg.AWSProfile)
	}
	if cfg.AWSRegion != "" {
		args = append(args, "--region", cfg.AWSRegion)
	}

	cmd := exec.Command("aws", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.mu.Lock()
		t.lastError = err.Error()
		t.setStatus(TunnelError)
		t.mu.Unlock()
		return err
	}

	// Merge stderr into stdout for monitoring
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		t.mu.Lock()
		t.lastError = err.Error()
		t.setStatus(TunnelError)
		t.mu.Unlock()
		return fmt.Errorf("start ssm session: %w", err)
	}

	t.mu.Lock()
	t.process = cmd
	t.mu.Unlock()

	// Monitor stdout for readiness
	portOpened := fmt.Sprintf("Port %d opened", cfg.LocalPort)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, portOpened) || strings.Contains(line, "Waiting for connections") {
				t.mu.Lock()
				t.setStatus(TunnelConnected)
				t.mu.Unlock()
			}
		}
	}()

	// Monitor process exit
	go func() {
		err := cmd.Wait()
		t.mu.Lock()
		defer t.mu.Unlock()

		select {
		case <-t.stopCh:
			// Intentional stop — set disconnected
			t.setStatus(TunnelDisconnected)
		default:
			// Unexpected exit
			if err != nil {
				t.lastError = err.Error()
			} else {
				t.lastError = "session ended unexpectedly"
			}
			t.setStatus(TunnelError)
		}
		t.process = nil
	}()

	return nil
}

// Stop sends SIGTERM to the subprocess and waits for exit.
func (t *TunnelManager) Stop() error {
	t.mu.Lock()
	proc := t.process
	stopCh := t.stopCh
	t.mu.Unlock()

	if proc == nil || proc.Process == nil {
		return nil
	}

	// Signal intentional stop
	close(stopCh)

	if err := proc.Process.Kill(); err != nil {
		return fmt.Errorf("kill tunnel process: %w", err)
	}
	return nil
}

// IsPortReady does a TCP dial to localhost:PORT to verify the tunnel is accepting connections.
func (t *TunnelManager) IsPortReady() bool {
	t.mu.Lock()
	port := t.localPort
	t.mu.Unlock()

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 1*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// WaitReady blocks until the tunnel port is accepting TCP connections or timeout.
func (t *TunnelManager) WaitReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if t.IsPortReady() {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("tunnel not ready after %v", timeout)
}

// CheckPrerequisites verifies that the AWS CLI and session-manager-plugin are installed.
// Returns a slice of missing tool names (empty if all present).
func CheckPrerequisites() []string {
	var missing []string
	if _, err := exec.LookPath("aws"); err != nil {
		missing = append(missing, "aws")
	}
	if _, err := exec.LookPath("session-manager-plugin"); err != nil {
		missing = append(missing, "session-manager-plugin")
	}
	return missing
}
