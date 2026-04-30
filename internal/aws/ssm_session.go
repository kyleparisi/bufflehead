package aws

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

// PortForwardSession opens an SSM session and runs port forwarding using
// the official session-manager-plugin binary. Blocks until ctx is cancelled
// or the plugin exits. The onReady callback is called when the local port
// is listening. The statusFunc callback receives progress updates (may be nil).
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

	// Build the session response JSON for the plugin.
	sessionResp, err := json.Marshal(map[string]string{
		"SessionId":  awssdk.ToString(out.SessionId),
		"TokenValue": *out.TokenValue,
		"StreamUrl":  *out.StreamUrl,
	})
	if err != nil {
		return fmt.Errorf("marshal session response: %w", err)
	}

	// Build the StartSession input JSON (parameters arg for the plugin).
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("marshal start session input: %w", err)
	}

	// SSM service endpoint for this region.
	endpoint := fmt.Sprintf("https://ssm.%s.amazonaws.com", cfg.Region)

	if statusFunc != nil {
		statusFunc("Launching session-manager-plugin...")
	}

	// Use the env var approach to avoid leaking tokens on the command line.
	cmd := exec.CommandContext(ctx, "session-manager-plugin",
		"AWS_SSM_START_SESSION_RESPONSE",
		cfg.Region,
		"StartSession",
		"", // profile — empty, we already have credentials
		string(inputJSON),
		endpoint,
	)
	cmd.Env = append(os.Environ(), "AWS_SSM_START_SESSION_RESPONSE="+string(sessionResp))

	// Capture stdout to detect when the port is ready.
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start session-manager-plugin: %w", err)
	}

	// Watch stdout for the "Waiting for connections" line that signals readiness.
	readyCh := make(chan struct{})
	exitCh := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		readyFired := false
		for scanner.Scan() {
			line := scanner.Text()
			log.Printf("ssm-plugin: %s", line)
			if !readyFired && strings.Contains(line, "Waiting for connections") {
				readyFired = true
				close(readyCh)
				if onReady != nil {
					onReady()
				}
			}
		}
	}()

	// Wait for process exit in the background so we can detect early crashes.
	go func() {
		exitCh <- cmd.Wait()
	}()

	// Wait for either readiness, early exit, or cancellation.
	select {
	case <-readyCh:
		// Port is ready, plugin is running.
		if statusFunc != nil {
			statusFunc(fmt.Sprintf("Tunnel open on port %d", localPort))
		}
		log.Printf("ssm: port forwarding ready on port %d via session-manager-plugin", localPort)
	case err := <-exitCh:
		// Plugin exited before becoming ready.
		if err != nil {
			return fmt.Errorf("session-manager-plugin exited before ready: %w", err)
		}
		return errors.New("session-manager-plugin exited before ready")
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		<-exitCh
		return ctx.Err()
	}

	// Block until the plugin exits or context is cancelled.
	select {
	case err := <-exitCh:
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return fmt.Errorf("session-manager-plugin exited: %w", err)
		}
		return nil
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		<-exitCh
		return ctx.Err()
	}
}
