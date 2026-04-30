package aws

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/kyleparisi/session-manager-plugin/src/datachannel"
	ssmlog "github.com/kyleparisi/session-manager-plugin/src/log"
	"github.com/kyleparisi/session-manager-plugin/src/sessionmanagerplugin/session"
	_ "github.com/kyleparisi/session-manager-plugin/src/sessionmanagerplugin/session/portsession"
	"github.com/twinj/uuid"
)

// PortForwardSession opens an SSM session and runs port forwarding using
// the official session-manager-plugin library (imported as a Go dependency).
// Blocks until ctx is cancelled or the session exits.
// The onReady callback is called when the local port is listening.
// The statusFunc callback receives progress updates (may be nil).
func PortForwardSession(ctx context.Context, cfg awssdk.Config, target, host string, remotePort, localPort int, onReady func(), statusFunc func(string)) error {
	docName := "AWS-StartPortForwardingSession"
	params := map[string][]string{
		"localPortNumber": {strconv.Itoa(localPort)},
		"portNumber":      {strconv.Itoa(remotePort)},
	}
	if host != "" {
		docName = "AWS-StartPortForwardingSessionToRemoteHost"
		params["host"] = []string{host}
	}

	input := &ssm.StartSessionInput{
		DocumentName: awssdk.String(docName),
		Target:       awssdk.String(target),
		Parameters:   params,
	}

	if statusFunc != nil {
		statusFunc("Starting SSM session...")
	}

	out, err := ssm.NewFromConfig(cfg).StartSession(ctx, input)
	if err != nil {
		return fmt.Errorf("ssm StartSession: %w", err)
	}
	if out.StreamUrl == nil || out.TokenValue == nil {
		return errors.New("StartSession response missing StreamUrl or TokenValue")
	}

	if statusFunc != nil {
		statusFunc("Launching port forwarding session...")
	}

	// Skip the plugin's own credential lookup — we already have credentials
	// from the caller's AWS config.
	os.Setenv("SSM_PLUGIN_SKIP_CLIENT_CONFIGURE", "true")
	defer os.Unsetenv("SSM_PLUGIN_SKIP_CLIENT_CONFIGURE")

	uuid.SwitchFormat(uuid.FormatHex)

	endpoint := fmt.Sprintf("https://ssm.%s.amazonaws.com", cfg.Region)

	sess := &session.Session{
		SessionId:             awssdk.ToString(out.SessionId),
		StreamUrl:             awssdk.ToString(out.StreamUrl),
		TokenValue:            awssdk.ToString(out.TokenValue),
		Region:                cfg.Region,
		TargetId:              target,
		ClientId:              uuid.NewV4().String(),
		Endpoint:              endpoint,
		IsAwsCliUpgradeNeeded: false,
		DataChannel:           &datachannel.DataChannel{},
		SessionProperties: map[string]interface{}{
			"portNumber":          strconv.Itoa(remotePort),
			"localPortNumber":     strconv.Itoa(localPort),
			"type":                "LocalPortForwarding",
			"localConnectionType": "tcp",
		},
	}

	logger := ssmlog.Logger(false, "ssm-plugin")

	// Run the session in a goroutine so we can monitor for readiness
	// and handle context cancellation.
	errCh := make(chan error, 1)
	doneCh := make(chan struct{})
	go func() {
		errCh <- sess.Execute(logger)
		close(doneCh)
	}()

	// Wait for the local port to start listening (indicates the session is ready).
	readyCh := make(chan struct{})
	go func() {
		addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(localPort))
		for {
			conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
			if err == nil {
				conn.Close()
				close(readyCh)
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-doneCh:
				// Session exited before port was ready — don't keep polling
				return
			case <-time.After(200 * time.Millisecond):
			}
		}
	}()

	// Wait for readiness, early exit, or cancellation.
	select {
	case <-readyCh:
		if statusFunc != nil {
			statusFunc(fmt.Sprintf("Tunnel open on port %d", localPort))
		}
		log.Printf("ssm: port forwarding ready on port %d", localPort)
		if onReady != nil {
			onReady()
		}
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("session exited before ready: %w", err)
		}
		return errors.New("session exited before ready")
	case <-ctx.Done():
		sess.Stop()
		return ctx.Err()
	}

	// Block until the session exits or context is cancelled.
	select {
	case err := <-errCh:
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return fmt.Errorf("session exited: %w", err)
		}
		return nil
	case <-ctx.Done():
		sess.Stop()
		// Drain the error channel to let the goroutine finish.
		select {
		case <-errCh:
		case <-time.After(5 * time.Second):
		}
		return ctx.Err()
	}
}
