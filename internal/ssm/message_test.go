package ssm

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMessageRoundTrip(t *testing.T) {
	orig := newAgentMessage()
	orig.messageType = msgInputStreamData
	orig.sequenceNumber = 42
	orig.flags = flagData
	orig.payloadType = payloadOutput
	orig.payload = []byte("hello world")

	data, err := orig.marshalBinary()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	decoded := new(agentMessage)
	if err := decoded.unmarshalBinary(data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.messageType != orig.messageType {
		t.Errorf("messageType: got %q, want %q", decoded.messageType, orig.messageType)
	}
	if decoded.sequenceNumber != orig.sequenceNumber {
		t.Errorf("sequenceNumber: got %d, want %d", decoded.sequenceNumber, orig.sequenceNumber)
	}
	if decoded.flags != orig.flags {
		t.Errorf("flags: got %d, want %d", decoded.flags, orig.flags)
	}
	if decoded.payloadType != orig.payloadType {
		t.Errorf("payloadType: got %d, want %d", decoded.payloadType, orig.payloadType)
	}
	if string(decoded.payload) != string(orig.payload) {
		t.Errorf("payload: got %q, want %q", decoded.payload, orig.payload)
	}
	if decoded.messageID != orig.messageID {
		t.Errorf("messageID: got %s, want %s", decoded.messageID, orig.messageID)
	}
}

func TestMessageRoundTripAllTypes(t *testing.T) {
	cases := []struct {
		name    string
		msgType messageType
		pt      payloadType
		fl      flag
		payload []byte
	}{
		{"ack", msgAcknowledge, payloadUndefined, flagAck, []byte(`{"key":"value"}`)},
		{"data", msgInputStreamData, payloadOutput, flagData, []byte("binary data here")},
		{"handshake_response", msgInputStreamData, payloadHandshakeResponse, flagData, []byte(`{"ClientVersion":"1.0"}`)},
		{"disconnect", msgInputStreamData, payloadFlag, flagData, []byte{0, 0, 0, 1}},
		{"terminate", msgInputStreamData, payloadFlag, flagFin, []byte{0, 0, 0, 2}},
		{"empty_payload", msgOutputStreamData, payloadOutput, flagData, []byte{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := newAgentMessage()
			msg.messageType = tc.msgType
			msg.payloadType = tc.pt
			msg.flags = tc.fl
			msg.payload = tc.payload

			data, err := msg.marshalBinary()
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			decoded := new(agentMessage)
			if err := decoded.unmarshalBinary(data); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			if decoded.messageType != tc.msgType {
				t.Errorf("messageType: got %q, want %q", decoded.messageType, tc.msgType)
			}
			if decoded.payloadType != tc.pt {
				t.Errorf("payloadType: got %d, want %d", decoded.payloadType, tc.pt)
			}
			if decoded.flags != tc.fl {
				t.Errorf("flags: got %d, want %d", decoded.flags, tc.fl)
			}
			if string(decoded.payload) != string(tc.payload) {
				t.Errorf("payload: got %q, want %q", decoded.payload, tc.payload)
			}
		})
	}
}

func TestMessageTooShort(t *testing.T) {
	msg := new(agentMessage)
	err := msg.unmarshalBinary([]byte{0, 0, 0})
	if err == nil {
		t.Fatal("expected error for short message")
	}
}

func TestBuildAcknowledgeMessage(t *testing.T) {
	orig := newAgentMessage()
	orig.messageType = msgOutputStreamData
	orig.sequenceNumber = 7
	orig.payload = []byte("test")

	ack, err := buildAcknowledgeMessage(orig)
	if err != nil {
		t.Fatalf("buildAck: %v", err)
	}

	if ack.messageType != msgAcknowledge {
		t.Errorf("ack type: got %q, want %q", ack.messageType, msgAcknowledge)
	}
	if ack.sequenceNumber != 0 {
		t.Errorf("ack seq: got %d, want 0", ack.sequenceNumber)
	}
	if ack.flags != flagAck {
		t.Errorf("ack flags: got %d, want %d", ack.flags, flagAck)
	}
}

func TestBuildHandshakeMsg(t *testing.T) {
	req := newAgentMessage()
	req.messageType = msgOutputStreamData
	req.payloadType = payloadHandshakeRequest
	req.sequenceNumber = 0

	hsReq := handshakeRequest{
		AgentVersion: "3.3.131.0",
		RequestedClientActions: []requestedClientAction{
			{ActionType: actionSessionType},
		},
	}
	payload, _ := json.Marshal(hsReq)
	req.payload = payload

	resp, err := buildHandshakeMsg(req)
	if err != nil {
		t.Fatalf("buildHandshakeMsg: %v", err)
	}

	if resp.messageType != msgInputStreamData {
		t.Errorf("resp type: got %q, want %q", resp.messageType, msgInputStreamData)
	}
	if resp.payloadType != payloadHandshakeResponse {
		t.Errorf("resp payloadType: got %d, want %d", resp.payloadType, payloadHandshakeResponse)
	}
}

func TestPayloadDigestValidation(t *testing.T) {
	msg := newAgentMessage()
	msg.messageType = msgInputStreamData
	msg.sequenceNumber = 1
	msg.flags = flagData
	msg.payloadType = payloadOutput
	msg.payload = []byte("important data")

	data, err := msg.marshalBinary()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Corrupt a byte in the payload region (after the header + payload length field)
	payloadStart := int(msg.headerLength) + 4
	data[payloadStart] ^= 0xFF

	decoded := new(agentMessage)
	err = decoded.unmarshalBinary(data)
	if err == nil {
		t.Fatal("expected error for corrupted payload, got nil")
	}
	if err.Error() != "payload digest mismatch" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEmptyPayloadDigestSkipped(t *testing.T) {
	msg := newAgentMessage()
	msg.messageType = msgOutputStreamData
	msg.payloadType = payloadOutput
	msg.flags = flagData
	msg.payload = []byte{}

	data, err := msg.marshalBinary()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	decoded := new(agentMessage)
	if err := decoded.unmarshalBinary(data); err != nil {
		t.Fatalf("expected empty payload to pass validation, got: %v", err)
	}
}

func TestTimestampRoundTrip(t *testing.T) {
	msg := newAgentMessage()
	msg.messageType = msgInputStreamData
	msg.payload = []byte("x")
	msg.createdDate = time.Now().Truncate(time.Millisecond) // SSM wire format is ms precision

	data, _ := msg.marshalBinary()
	decoded := new(agentMessage)
	decoded.unmarshalBinary(data)

	if !decoded.createdDate.Equal(msg.createdDate) {
		t.Errorf("timestamp: got %v, want %v", decoded.createdDate, msg.createdDate)
	}
}
