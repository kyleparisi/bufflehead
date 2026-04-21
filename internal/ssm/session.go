// Package ssm implements the SSM agent wire protocol for port forwarding
// over WebSocket connections, without requiring the session-manager-plugin binary.
package ssm

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/xtaci/smux"
)

// Session manages a WebSocket connection to the SSM service for port forwarding.
type Session struct {
	ws         *websocket.Conn
	mu         sync.Mutex
	seqNum     int64
	statusFunc func(string) // optional callback for progress updates
}

// NewSession creates a Session from an already-connected WebSocket.
// It sends the channel-open message and completes the handshake.
// The statusFunc callback receives progress updates (may be nil).
func NewSession(ctx context.Context, ws *websocket.Conn, tokenValue string, statusFunc func(string)) (*Session, error) {
	if statusFunc != nil {
		statusFunc("Connecting to SSM agent...")
	}

	// Open data channel
	openMsg := map[string]string{
		"MessageSchemaVersion": "1.0",
		"RequestId":            uuid.New().String(),
		"TokenValue":           tokenValue,
	}
	if err := ws.WriteJSON(openMsg); err != nil {
		return nil, fmt.Errorf("open data channel: %w", err)
	}

	s := &Session{ws: ws, statusFunc: statusFunc}

	if statusFunc != nil {
		statusFunc("Completing handshake...")
	}

	if err := s.handshake(ctx); err != nil {
		return nil, fmt.Errorf("handshake: %w", err)
	}

	return s, nil
}

// handshake reads messages until the handshake completes.
func (s *Session) handshake(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		_, raw, err := s.ws.ReadMessage()
		if err != nil {
			return fmt.Errorf("read during handshake: %w", err)
		}

		if len(raw) < agentMsgHeaderLen+4 {
			continue
		}

		msg := new(agentMessage)
		if err := msg.unmarshalBinary(raw); err != nil {
			continue
		}

		switch msg.messageType {
		case msgOutputStreamData:
			switch msg.payloadType {
			case payloadHandshakeRequest:
				resp, err := buildHandshakeMsg(msg)
				if err != nil {
					return err
				}
				resp.sequenceNumber = atomic.AddInt64(&s.seqNum, 1) - 1
				if err := s.writeMsg(resp); err != nil {
					return err
				}
				if err := s.sendAck(msg); err != nil {
					return err
				}
			case payloadHandshakeComplete:
				_ = s.sendAck(msg)
				return nil // handshake done
			default:
				_ = s.sendAck(msg)
			}
		default:
			_ = s.sendAck(msg)
		}
	}
}

// writeMsg marshals and sends an agent message over the WebSocket.
func (s *Session) writeMsg(msg *agentMessage) error {
	data, err := msg.marshalBinary()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ws.WriteMessage(websocket.BinaryMessage, data)
}

// sendAck sends an acknowledgement for a received message.
func (s *Session) sendAck(msg *agentMessage) error {
	ack, err := buildAcknowledgeMessage(msg)
	if err != nil {
		return err
	}
	return s.writeMsg(ack)
}

// sendData sends payload data to the remote host.
func (s *Session) sendData(data []byte) error {
	seq := atomic.AddInt64(&s.seqNum, 1) - 1
	msg := newAgentMessage()
	msg.messageType = msgInputStreamData
	msg.payloadType = payloadOutput
	msg.sequenceNumber = seq
	msg.flags = flagData
	msg.payload = data
	return s.writeMsg(msg)
}

// terminateSession signals session termination.
func (s *Session) terminateSession() error {
	seq := atomic.AddInt64(&s.seqNum, 1)
	return s.writeMsg(buildTerminateSessionMessage(seq))
}

// Close terminates the session and closes the WebSocket.
func (s *Session) Close() {
	_ = s.terminateSession()
	_ = s.ws.Close()
}

// readMessages reads from the WebSocket and sends output payloads to the returned channel.
// Closes the channel when the WebSocket closes or an error occurs.
func (s *Session) readMessages(ctx context.Context) <-chan []byte {
	ch := make(chan []byte, 64)
	go func() {
		defer close(ch)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			_, raw, err := s.ws.ReadMessage()
			if err != nil {
				if ctx.Err() == nil {
					log.Printf("ssm: websocket read error: %v", err)
				}
				return
			}

			if len(raw) < agentMsgHeaderLen+4 {
				continue
			}

			msg := new(agentMessage)
			if err := msg.unmarshalBinary(raw); err != nil {
				log.Printf("ssm: unmarshal error: %v", err)
				continue
			}

			switch msg.messageType {
			case msgOutputStreamData:
				_ = s.sendAck(msg)
				if msg.payloadType == payloadOutput {
					payload := make([]byte, len(msg.payload))
					copy(payload, msg.payload)
					select {
					case ch <- payload:
					case <-ctx.Done():
						return
					}
				}
			case msgChannelClosed:
				return
			case msgAcknowledge, msgPausePublication, msgStartPublication:
				// no action needed
			default:
				_ = s.sendAck(msg)
			}
		}
	}()
	return ch
}

// PortForward runs the port forwarding loop using smux multiplexing.
// The SSM agent expects data framed through smux streams.
// Blocks until ctx is cancelled. The onReady callback is called when the listener is up.
func (s *Session) PortForward(ctx context.Context, localPort int, onReady func()) error {
	// Create a pipe to bridge smux ↔ SSM data channel
	ssmSide, muxSide := net.Pipe()

	// SSM → pipe: read WebSocket messages and write payloads to the pipe
	inCh := s.readMessages(ctx)
	go func() {
		defer ssmSide.Close()
		for {
			select {
			case data, ok := <-inCh:
				if !ok {
					return
				}
				if len(data) > 0 {
					if _, err := ssmSide.Write(data); err != nil {
						log.Printf("ssm: pipe write error: %v", err)
						return
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// pipe → SSM: read from the pipe and send as SSM data messages
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ssmSide.Read(buf)
			if n > 0 {
				if sendErr := s.sendData(buf[:n]); sendErr != nil {
					log.Printf("ssm: sendData error: %v", sendErr)
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// Create smux client session over the pipe
	smuxConfig := smux.DefaultConfig()
	smuxConfig.KeepAliveDisabled = true
	muxSession, err := smux.Client(muxSide, smuxConfig)
	if err != nil {
		return fmt.Errorf("smux client: %w", err)
	}
	defer muxSession.Close()

	// Start TCP listener
	lsnr, err := net.Listen("tcp", net.JoinHostPort("", strconv.Itoa(localPort)))
	if err != nil {
		return fmt.Errorf("listen on port %d: %w", localPort, err)
	}
	go func() {
		<-ctx.Done()
		lsnr.Close()
	}()

	if s.statusFunc != nil {
		s.statusFunc(fmt.Sprintf("Tunnel open on port %d", localPort))
	}
	log.Printf("ssm: port forwarding listener ready on port %d", localPort)

	if onReady != nil {
		onReady()
	}

	for {
		conn, err := lsnr.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				continue
			}
		}
		log.Printf("ssm: accepted local connection from %s", conn.RemoteAddr())

		// Open a new smux stream for this connection
		stream, err := muxSession.OpenStream()
		if err != nil {
			log.Printf("ssm: open stream error: %v", err)
			conn.Close()
			continue
		}
		log.Printf("ssm: opened smux stream %d", stream.ID())

		// Bidirectional relay between TCP conn and smux stream
		go func() {
			defer conn.Close()
			defer stream.Close()

			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				io.Copy(stream, conn)
			}()
			go func() {
				defer wg.Done()
				io.Copy(conn, stream)
			}()
			wg.Wait()
			log.Printf("ssm: connection closed on stream %d", stream.ID())
		}()
	}
}
