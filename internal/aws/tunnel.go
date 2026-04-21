package aws

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
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

// TunnelManager manages an SSM port-forwarding session.
type TunnelManager struct {
	mu        sync.Mutex
	cancel    context.CancelFunc
	localPort int
	status    TunnelStatus
	statusMsg string
	lastError string
	onStatus  func(TunnelStatus, string) // status + message
}

// NewTunnelManager creates a new tunnel manager.
// onStatus is called (from a goroutine) whenever the tunnel status changes.
// The string parameter is a human-readable progress message.
func NewTunnelManager(onStatus func(TunnelStatus, string)) *TunnelManager {
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

// StatusMsg returns the current progress message.
func (t *TunnelManager) StatusMsg() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.statusMsg
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

// setStatusLocked updates status and calls the callback WITHOUT holding the lock.
func (t *TunnelManager) setStatusNotify(s TunnelStatus, msg string) {
	t.mu.Lock()
	t.status = s
	t.statusMsg = msg
	cb := t.onStatus
	t.mu.Unlock()

	if cb != nil {
		cb(s, msg)
	}
}

// Start launches the SSM port-forwarding session.
func (t *TunnelManager) Start(cfg TunnelConfig) error {
	t.mu.Lock()
	if t.status == TunnelConnecting || t.status == TunnelConnected {
		t.mu.Unlock()
		return fmt.Errorf("tunnel already active")
	}
	t.localPort = cfg.LocalPort
	t.lastError = ""
	t.mu.Unlock()

	t.setStatusNotify(TunnelConnecting, "Loading AWS config...")

	opts := []func(*config.LoadOptions) error{
		config.WithRegion(cfg.AWSRegion),
	}
	if cfg.AWSProfile != "" {
		opts = append(opts, config.WithSharedConfigProfile(cfg.AWSProfile))
	}
	awsCfg, err := config.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		t.mu.Lock()
		t.lastError = err.Error()
		t.mu.Unlock()
		t.setStatusNotify(TunnelError, err.Error())
		return fmt.Errorf("load aws config: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.mu.Lock()
	t.cancel = cancel
	t.mu.Unlock()

	go func() {
		err := PortForwardSession(ctx, awsCfg, cfg.InstanceID, cfg.RDSHost, cfg.RDSPort, cfg.LocalPort,
			func() {
				// Called when the listener is ready (handshake complete, port open)
				t.setStatusNotify(TunnelConnected, fmt.Sprintf("Tunnel open on port %d", cfg.LocalPort))
			},
			func(msg string) {
				// Progress updates from the SSM session — no lock contention
				t.setStatusNotify(TunnelConnecting, msg)
			},
		)

		t.mu.Lock()
		cancelled := ctx.Err() != nil
		if !cancelled {
			if err != nil {
				t.lastError = err.Error()
			} else {
				t.lastError = "session ended unexpectedly"
			}
		}
		t.cancel = nil
		t.mu.Unlock()

		if cancelled {
			t.setStatusNotify(TunnelDisconnected, "")
		} else {
			t.setStatusNotify(TunnelError, t.LastError())
		}
	}()

	return nil
}

// Stop cancels the port forwarding session.
func (t *TunnelManager) Stop() error {
	t.mu.Lock()
	cancel := t.cancel
	t.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	return nil
}

// IsPortReady checks if the tunnel is connected.
func (t *TunnelManager) IsPortReady() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.status == TunnelConnected
}

// WaitReady blocks until the tunnel is connected or timeout.
func (t *TunnelManager) WaitReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if t.IsPortReady() {
			return nil
		}
		// Also check for error so we fail fast
		t.mu.Lock()
		s := t.status
		e := t.lastError
		t.mu.Unlock()
		if s == TunnelError {
			return fmt.Errorf("tunnel error: %s", e)
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("tunnel not ready after %v", timeout)
}

// FindFreePort asks the OS for an available TCP port on localhost.
func FindFreePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("find free port: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port, nil
}
