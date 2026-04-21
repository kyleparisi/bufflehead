package aws

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/gorilla/websocket"

	ssmlib "bufflehead/internal/ssm"
)

// PortForwardSession opens an SSM session and runs port forwarding.
// Blocks until ctx is cancelled. The onReady callback is called when the listener is up.
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

	ws, _, err := websocket.DefaultDialer.Dial(*out.StreamUrl, http.Header{})
	if err != nil {
		return fmt.Errorf("websocket dial: %w", err)
	}

	session, err := ssmlib.NewSession(ctx, ws, *out.TokenValue, statusFunc)
	if err != nil {
		ws.Close()
		return err
	}
	defer session.Close()

	return session.PortForward(ctx, localPort, onReady)
}
