package ssm

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/xtaci/smux"
)

// mockAgent simulates the SSM agent side of the WebSocket protocol.
// It performs the handshake, then bridges smux streams to a handler function.
type mockAgent struct {
	t         *testing.T
	ws        *websocket.Conn
	mu        sync.Mutex
	seqNum    int64
	handler   func(net.Conn) // called for each smux stream
	openMsgCh chan map[string]string
}

func newMockAgent(t *testing.T, ws *websocket.Conn, handler func(net.Conn)) *mockAgent {
	return &mockAgent{
		t:         t,
		ws:        ws,
		handler:   handler,
		openMsgCh: make(chan map[string]string, 1),
	}
}

func (a *mockAgent) run(ctx context.Context) {
	// Step 1: Read channel-open message
	_, raw, err := a.ws.ReadMessage()
	if err != nil {
		a.t.Logf("agent: read open msg: %v", err)
		return
	}
	var openMsg map[string]string
	json.Unmarshal(raw, &openMsg)
	a.openMsgCh <- openMsg

	// Step 2: Send handshake request
	hsReq := handshakeRequest{
		AgentVersion: "3.3.131.0",
		RequestedClientActions: []requestedClientAction{
			{ActionType: actionSessionType, ActionParameters: map[string]string{"SessionType": "Port"}},
		},
	}
	hsPayload, _ := json.Marshal(hsReq)
	a.sendOutputMessage(payloadHandshakeRequest, hsPayload)

	// Step 3: Read handshake response + ack
	a.readAndDiscard(2)

	// Step 4: Send handshake complete
	a.sendOutputMessage(payloadHandshakeComplete, []byte("{}"))

	// Read the ack for handshake complete
	a.readAndDiscard(1)

	// Step 5: Bridge smux over the SSM data channel
	a.bridgeSmux(ctx)
}

// sendOutputMessage constructs and sends an agentMessage with the given payload type.
func (a *mockAgent) sendOutputMessage(pt payloadType, payload []byte) {
	msg := newAgentMessage()
	msg.messageType = msgOutputStreamData
	msg.sequenceNumber = atomic.AddInt64(&a.seqNum, 1) - 1
	msg.flags = flagData
	msg.payloadType = pt
	msg.payload = payload

	data, _ := msg.marshalBinary()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ws.WriteMessage(websocket.BinaryMessage, data)
}

// readAndDiscard reads n WebSocket messages and discards them.
func (a *mockAgent) readAndDiscard(n int) {
	for i := 0; i < n; i++ {
		a.ws.ReadMessage()
	}
}

// bridgeSmux sets up a smux server over the SSM data channel and handles streams.
func (a *mockAgent) bridgeSmux(ctx context.Context) {
	agentSide, muxSide := net.Pipe()

	// Read data messages from WebSocket and write payloads to the pipe
	go func() {
		defer agentSide.Close()
		for {
			_, raw, err := a.ws.ReadMessage()
			if err != nil {
				return
			}
			if len(raw) < agentMsgHeaderLen+4 {
				continue
			}
			msg := new(agentMessage)
			if err := msg.unmarshalBinary(raw); err != nil {
				continue
			}
			switch msg.messageType {
			case msgInputStreamData:
				if msg.payloadType == payloadOutput && len(msg.payload) > 0 {
					agentSide.Write(msg.payload)
				}
			}
		}
	}()

	// Read from pipe and send as output data messages
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := agentSide.Read(buf)
			if n > 0 {
				payload := make([]byte, n)
				copy(payload, buf[:n])
				a.sendOutputMessage(payloadOutput, payload)
			}
			if err != nil {
				return
			}
		}
	}()

	// Accept smux streams
	smuxConfig := smux.DefaultConfig()
	smuxConfig.KeepAliveDisabled = true
	muxSession, err := smux.Server(muxSide, smuxConfig)
	if err != nil {
		a.t.Logf("agent: smux server: %v", err)
		return
	}
	defer muxSession.Close()

	for {
		stream, err := muxSession.AcceptStream()
		if err != nil {
			return
		}
		go a.handler(stream)
	}
}

// startMockServer creates an httptest WebSocket server running a mock SSM agent.
// The handler function is called for each smux stream (simulating a remote service).
// Returns the server URL and a cleanup function.
func startMockServer(t *testing.T, handler func(net.Conn)) (*httptest.Server, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("mock server upgrade: %v", err)
			return
		}
		defer ws.Close()
		agent := newMockAgent(t, ws, handler)
		agent.run(ctx)
	}))

	return server, func() {
		cancel()
		server.Close()
	}
}

func TestHandshake(t *testing.T) {
	server, cleanup := startMockServer(t, func(conn net.Conn) {
		conn.Close()
	})
	defer cleanup()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session, err := NewSession(ctx, ws, "test-token", nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer session.Close()

	t.Log("Handshake completed successfully")
}

func TestPortForwardEcho(t *testing.T) {
	// Mock agent echoes back whatever it receives on each stream
	server, cleanup := startMockServer(t, func(conn net.Conn) {
		defer conn.Close()
		io.Copy(conn, conn)
	})
	defer cleanup()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session, err := NewSession(ctx, ws, "test-token", nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer session.Close()

	localPort, err := findFreePort()
	if err != nil {
		t.Fatalf("findFreePort: %v", err)
	}

	ready := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- session.PortForward(ctx, localPort, func() { close(ready) })
	}()

	select {
	case <-ready:
	case err := <-errCh:
		t.Fatalf("PortForward failed: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for port forward ready")
	}

	// Connect to the local port and send data
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(localPort)), 2*time.Second)
	if err != nil {
		t.Fatalf("dial local port: %v", err)
	}
	defer conn.Close()

	testData := "hello from unit test"
	if _, err := conn.Write([]byte(testData)); err != nil {
		t.Fatalf("write: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, len(testData))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}

	if string(buf) != testData {
		t.Fatalf("echo mismatch: got %q, want %q", string(buf), testData)
	}
	t.Logf("Echo test passed: sent and received %q", testData)
}

func TestPortForwardMultipleConnections(t *testing.T) {
	// Mock agent echoes back whatever it receives
	server, cleanup := startMockServer(t, func(conn net.Conn) {
		defer conn.Close()
		io.Copy(conn, conn)
	})
	defer cleanup()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := NewSession(ctx, ws, "test-token", nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer session.Close()

	localPort, err := findFreePort()
	if err != nil {
		t.Fatalf("findFreePort: %v", err)
	}

	ready := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- session.PortForward(ctx, localPort, func() { close(ready) })
	}()

	select {
	case <-ready:
	case err := <-errCh:
		t.Fatalf("PortForward failed: %v", err)
	}

	// Open 5 concurrent connections
	const numConns = 5
	var wg sync.WaitGroup
	for i := 0; i < numConns; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(localPort)), 2*time.Second)
			if err != nil {
				t.Errorf("conn %d dial: %v", id, err)
				return
			}
			defer conn.Close()

			msg := []byte("msg-" + strconv.Itoa(id))
			if _, err := conn.Write(msg); err != nil {
				t.Errorf("conn %d write: %v", id, err)
				return
			}

			conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			buf := make([]byte, len(msg))
			if _, err := io.ReadFull(conn, buf); err != nil {
				t.Errorf("conn %d read: %v", id, err)
				return
			}

			if string(buf) != string(msg) {
				t.Errorf("conn %d mismatch: got %q want %q", id, buf, msg)
			}
		}(i)
	}
	wg.Wait()
	t.Logf("All %d concurrent connections echoed successfully", numConns)
}

func TestStatusCallbacks(t *testing.T) {
	server, cleanup := startMockServer(t, func(conn net.Conn) {
		conn.Close()
	})
	defer cleanup()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var statuses []string
	var mu sync.Mutex
	session, err := NewSession(ctx, ws, "test-token", func(msg string) {
		mu.Lock()
		statuses = append(statuses, msg)
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer session.Close()

	localPort, err := findFreePort()
	if err != nil {
		t.Fatalf("findFreePort: %v", err)
	}

	ready := make(chan struct{})
	go func() {
		session.PortForward(ctx, localPort, func() { close(ready) })
	}()

	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}

	mu.Lock()
	defer mu.Unlock()

	// Should have: "Connecting to SSM agent...", "Completing handshake...", "Tunnel open on port ..."
	if len(statuses) < 3 {
		t.Fatalf("expected at least 3 status callbacks, got %d: %v", len(statuses), statuses)
	}
	t.Logf("Status callbacks: %v", statuses)
}

func findFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port, nil
}

func init() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)
}
