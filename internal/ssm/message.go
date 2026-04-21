package ssm

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Wire protocol constants.
const agentMsgHeaderLen = 116

// messageType labels used in the AgentMessage header.
type messageType string

const (
	msgAcknowledge      messageType = "acknowledge"
	msgChannelClosed    messageType = "channel_closed"
	msgOutputStreamData messageType = "output_stream_data"
	msgInputStreamData  messageType = "input_stream_data"
	msgPausePublication messageType = "pause_publication"
	msgStartPublication messageType = "start_publication"
)

// flag indicates where in the stream a message belongs.
type flag uint64

const (
	flagData flag = 0
	flagSyn  flag = 1
	flagFin  flag = 2
	flagAck  flag = 3
)

// payloadType indicates the data format of the Payload field.
type payloadType uint32

const (
	payloadUndefined         payloadType = 0
	payloadOutput            payloadType = 1
	payloadHandshakeRequest  payloadType = 5
	payloadHandshakeResponse payloadType = 6
	payloadHandshakeComplete payloadType = 7
	payloadFlag              payloadType = 10
)

// payloadTypeFlag values for control messages.
type payloadTypeFlag uint32

const (
	flagDisconnectToPort payloadTypeFlag = 1
	flagTerminateSession payloadTypeFlag = 2
)

// Handshake types.
type actionType string

const (
	actionSessionType actionType = "SessionType"
)

type actionStatus int

const (
	actionSuccess actionStatus = 1
)

// agentMessage is the binary message format for SSM agent communication.
type agentMessage struct {
	headerLength   uint32
	messageType    messageType
	schemaVersion  uint32
	createdDate    time.Time
	sequenceNumber int64
	flags          flag
	messageID      uuid.UUID
	payloadDigest  []byte
	payloadType    payloadType
	payloadLength  uint32
	payload        []byte
}

func newAgentMessage() *agentMessage {
	return &agentMessage{
		headerLength:  agentMsgHeaderLen,
		schemaVersion: 1,
		createdDate:   time.Now(),
		messageID:     uuid.New(),
	}
}

func (m *agentMessage) marshalBinary() ([]byte, error) {
	m.payloadLength = uint32(len(m.payload))
	m.computeDigest()

	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, m.headerLength)
	binary.Write(buf, binary.BigEndian, m.paddedMessageType())
	binary.Write(buf, binary.BigEndian, m.schemaVersion)
	binary.Write(buf, binary.BigEndian, uint64(m.createdDate.UnixNano()/int64(time.Millisecond)))
	binary.Write(buf, binary.BigEndian, m.sequenceNumber)
	binary.Write(buf, binary.BigEndian, m.flags)
	binary.Write(buf, binary.BigEndian, formatUUID(m.messageID[:]))
	binary.Write(buf, binary.BigEndian, m.payloadDigest[:sha256.Size])
	binary.Write(buf, binary.BigEndian, m.payloadType)
	binary.Write(buf, binary.BigEndian, m.payloadLength)
	binary.Write(buf, binary.BigEndian, m.payload)
	return buf.Bytes(), nil
}

func (m *agentMessage) unmarshalBinary(data []byte) error {
	if len(data) < agentMsgHeaderLen+4 {
		return errors.New("message too short")
	}

	m.headerLength = binary.BigEndian.Uint32(data)
	m.messageType = parseMessageType(data[4:36])
	m.schemaVersion = binary.BigEndian.Uint32(data[36:40])
	m.createdDate = parseTime(data[40:48])
	m.sequenceNumber = int64(binary.BigEndian.Uint64(data[48:56]))
	m.flags = flag(binary.BigEndian.Uint64(data[56:64]))
	m.messageID = uuid.Must(uuid.FromBytes(formatUUID(data[64:80])))
	m.payloadDigest = data[80 : 80+sha256.Size]

	if m.headerLength == agentMsgHeaderLen {
		m.payloadType = payloadType(binary.BigEndian.Uint32(data[112:m.headerLength]))
	}

	payloadLenEnd := m.headerLength + 4
	m.payloadLength = binary.BigEndian.Uint32(data[m.headerLength:payloadLenEnd])
	m.payload = data[payloadLenEnd : payloadLenEnd+m.payloadLength]

	return m.validate()
}

func (m *agentMessage) validate() error {
	if m.headerLength > agentMsgHeaderLen || m.headerLength < agentMsgHeaderLen-4 {
		return errors.New("invalid message header length")
	}
	if len(m.payload) != int(m.payloadLength) {
		return fmt.Errorf("payload length mismatch: want %d, got %d", m.payloadLength, len(m.payload))
	}
	return nil
}

func (m *agentMessage) computeDigest() {
	h := sha256.New()
	h.Write(m.payload)
	m.payloadDigest = h.Sum(nil)
}

func (m *agentMessage) paddedMessageType() []byte {
	b := []byte(m.messageType)
	if len(b) >= 32 {
		return b[:32]
	}
	return append(b, bytes.Repeat([]byte{0x20}, 32-len(b))...)
}

func parseMessageType(data []byte) messageType {
	return messageType(bytes.TrimSpace(bytes.TrimRight(data, string(rune(0x00)))))
}

func parseTime(data []byte) time.Time {
	ts := binary.BigEndian.Uint64(data)
	return time.Unix(0, int64(ts)*int64(time.Millisecond))
}

func formatUUID(data []byte) []byte {
	out := make([]byte, 16)
	copy(out, data[8:])
	copy(out[8:], data[:8])
	return out
}

// Handshake payload types.

type handshakeRequest struct {
	AgentVersion           string
	RequestedClientActions []requestedClientAction
}

type requestedClientAction struct {
	ActionType       actionType
	ActionParameters any
}

type handshakeResponse struct {
	ClientVersion          string                  `json:"ClientVersion"`
	ProcessedClientActions []processedClientAction `json:"ProcessedClientActions"`
	Errors                 []string                `json:"Errors"`
}

type processedClientAction struct {
	ActionType   actionType   `json:"ActionType"`
	ActionStatus actionStatus `json:"ActionStatus"`
	ActionResult any          `json:"ActionResult"`
	Error        string       `json:"Error"`
}

func buildHandshakeResponsePayload(actions []requestedClientAction) *handshakeResponse {
	res := &handshakeResponse{
		ClientVersion:          "1.2.694.0",
		ProcessedClientActions: make([]processedClientAction, len(actions)),
	}
	for i, a := range actions {
		if a.ActionType == actionSessionType {
			res.ProcessedClientActions[i] = processedClientAction{
				ActionType:   a.ActionType,
				ActionStatus: actionSuccess,
			}
		}
	}
	return res
}

// buildAcknowledgeMessage creates an acknowledgement for a received message.
func buildAcknowledgeMessage(msg *agentMessage) (*agentMessage, error) {
	ack := map[string]any{
		"AcknowledgedMessageType":           string(msg.messageType),
		"AcknowledgedMessageId":             msg.messageID.String(),
		"AcknowledgedMessageSequenceNumber": msg.sequenceNumber,
		"IsSequentialMessage":               true,
	}
	payload, err := json.Marshal(ack)
	if err != nil {
		return nil, err
	}

	out := newAgentMessage()
	out.messageType = msgAcknowledge
	out.sequenceNumber = 0 // plugin always uses 0 for ACKs
	out.flags = flagAck
	out.payloadType = payloadUndefined
	out.payload = payload
	return out, nil
}

// buildHandshakeMsg creates the handshake response message.
func buildHandshakeMsg(req *agentMessage) (*agentMessage, error) {
	var hsReq handshakeRequest
	if err := json.Unmarshal(req.payload, &hsReq); err != nil {
		return nil, fmt.Errorf("unmarshal handshake request: %w", err)
	}

	resp := buildHandshakeResponsePayload(hsReq.RequestedClientActions)
	payload, err := json.Marshal(resp)
	if err != nil {
		return nil, err
	}

	out := newAgentMessage()
	out.messageType = msgInputStreamData
	out.sequenceNumber = req.sequenceNumber
	out.flags = flagData
	out.payloadType = payloadHandshakeResponse
	out.payload = payload
	return out, nil
}

// buildDisconnectPortMessage creates a DisconnectToPort flag message.
func buildDisconnectPortMessage(seqNum int64) *agentMessage {
	msg := newAgentMessage()
	msg.messageType = msgInputStreamData
	msg.sequenceNumber = seqNum
	msg.flags = flagData
	msg.payloadType = payloadFlag
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, uint32(flagDisconnectToPort))
	msg.payload = buf
	return msg
}

// buildTerminateSessionMessage creates a TerminateSession flag message.
func buildTerminateSessionMessage(seqNum int64) *agentMessage {
	msg := newAgentMessage()
	msg.messageType = msgInputStreamData
	msg.sequenceNumber = seqNum
	msg.flags = flagFin
	msg.payloadType = payloadFlag
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, uint32(flagTerminateSession))
	msg.payload = buf
	return msg
}

func (m *agentMessage) String() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "AgentMessage{type=%s seq=%d flags=%d payloadType=%d len=%d}",
		m.messageType, m.sequenceNumber, m.flags, m.payloadType, m.payloadLength)
	return sb.String()
}
