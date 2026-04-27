package aws

import (
	"context"
	"fmt"
	"log"
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

	// InstanceResolver, if set, is called before each connection attempt to
	// resolve a fresh instance ID. This supports spot instances that rotate.
	// If nil, InstanceID is used as-is.
	InstanceResolver func() (string, error)
}

// Maximum number of consecutive reconnect attempts before giving up.
const maxReconnectAttempts = 10

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

// Start launches the SSM port-forwarding session with automatic reconnection.
// If the session drops unexpectedly, it will re-establish the SSM session
// with exponential backoff (up to maxReconnectAttempts times).
func (t *TunnelManager) Start(cfg TunnelConfig) error {
	t.mu.Lock()
	if t.status == TunnelConnecting || t.status == TunnelConnected {
		t.mu.Unlock()
		return fmt.Errorf("tunnel already active")
	}
	t.localPort = cfg.LocalPort
	t.lastError = ""
	t.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	t.mu.Lock()
	t.cancel = cancel
	t.mu.Unlock()

	go t.runWithReconnect(ctx, cfg)

	return nil
}

// runWithReconnect runs PortForwardSession in a loop, reconnecting on
// unexpected disconnections with exponential backoff.
func (t *TunnelManager) runWithReconnect(ctx context.Context, cfg TunnelConfig) {
	attempt := 0

	for {
		if ctx.Err() != nil {
			t.setStatusNotify(TunnelDisconnected, "")
			return
		}

		if attempt > 0 {
			backoff := time.Duration(1<<min(attempt-1, 5)) * time.Second // 1s, 2s, 4s, 8s, 16s, 32s
			log.Printf("ssm: reconnecting in %v (attempt %d/%d)", backoff, attempt, maxReconnectAttempts)
			t.setStatusNotify(TunnelConnecting, fmt.Sprintf("Reconnecting in %v...", backoff))

			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				t.setStatusNotify(TunnelDisconnected, "")
				return
			}
		}

		// Re-resolve instance ID on reconnect if a resolver is configured
		instanceID := cfg.InstanceID
		if attempt > 0 && cfg.InstanceResolver != nil {
			t.setStatusNotify(TunnelConnecting, "Resolving bastion instance...")
			resolved, err := cfg.InstanceResolver()
			if err != nil {
				if ctx.Err() != nil {
					t.setStatusNotify(TunnelDisconnected, "")
					return
				}
				log.Printf("ssm: instance resolution error: %v", err)
				attempt++
				if attempt >= maxReconnectAttempts {
					t.mu.Lock()
					t.lastError = fmt.Sprintf("giving up after %d attempts: %v", maxReconnectAttempts, err)
					t.cancel = nil
					t.mu.Unlock()
					t.setStatusNotify(TunnelError, t.LastError())
					return
				}
				continue
			}
			instanceID = resolved
			log.Printf("ssm: resolved instance ID: %s", instanceID)
		}

		t.setStatusNotify(TunnelConnecting, "Loading AWS config...")

		opts := []func(*config.LoadOptions) error{
			config.WithRegion(cfg.AWSRegion),
		}
		if cfg.AWSProfile != "" {
			opts = append(opts, config.WithSharedConfigProfile(cfg.AWSProfile))
		}
		awsCfg, err := config.LoadDefaultConfig(ctx, opts...)
		if err != nil {
			if ctx.Err() != nil {
				t.setStatusNotify(TunnelDisconnected, "")
				return
			}
			log.Printf("ssm: aws config error: %v", err)
			attempt++
			if attempt >= maxReconnectAttempts {
				t.mu.Lock()
				t.lastError = fmt.Sprintf("giving up after %d attempts: %v", maxReconnectAttempts, err)
				t.cancel = nil
				t.mu.Unlock()
				t.setStatusNotify(TunnelError, t.LastError())
				return
			}
			continue
		}

		err = PortForwardSession(ctx, awsCfg, instanceID, cfg.RDSHost, cfg.RDSPort, cfg.LocalPort,
			func() {
				attempt = 0 // reset on successful connection
				t.setStatusNotify(TunnelConnected, fmt.Sprintf("Tunnel open on port %d", cfg.LocalPort))
			},
			func(msg string) {
				t.setStatusNotify(TunnelConnecting, msg)
			},
		)

		if ctx.Err() != nil {
			t.setStatusNotify(TunnelDisconnected, "")
			return
		}

		// Session dropped unexpectedly — try to reconnect
		errMsg := "session ended unexpectedly"
		if err != nil {
			errMsg = err.Error()
		}
		log.Printf("ssm: session dropped: %s", errMsg)

		attempt++
		if attempt >= maxReconnectAttempts {
			t.mu.Lock()
			t.lastError = fmt.Sprintf("giving up after %d attempts: %s", maxReconnectAttempts, errMsg)
			t.cancel = nil
			t.mu.Unlock()
			t.setStatusNotify(TunnelError, t.LastError())
			return
		}

		// Brief pause to let the old listener fully release the port
		select {
		case <-time.After(500 * time.Millisecond):
		case <-ctx.Done():
			t.setStatusNotify(TunnelDisconnected, "")
			return
		}
	}
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
