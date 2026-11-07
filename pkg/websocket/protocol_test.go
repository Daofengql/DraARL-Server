package websocket

import (
	"strings"
	"testing"
	"unicode/utf8"

	"draarl/internal/protocol"
)

func TestHeartbeatEncoderBoundsAndUTF8(t *testing.T) {
	if EncodeHeartbeatResponse(nil, "CALL") != nil || EncodeWSPacket(nil) != nil {
		t.Fatal("nil packet must be rejected")
	}
	if EncodeHeartbeatResponse(&WSPacket{DATA: make([]byte, 65536)}, "CALL") != nil {
		t.Fatal("unrepresentable heartbeat length must be rejected")
	}
	req := &WSPacket{Username: strings.Repeat("中", 12), Type: protocol.DraARLTypeHeartbeat}
	data := EncodeHeartbeatResponse(req, strings.Repeat("呼", 12))
	if !utf8.Valid(data[6:38]) || !utf8.Valid(data[54:86]) {
		t.Fatal("heartbeat split a UTF-8 character")
	}
}

func TestDecodeWSPacketRejectsUnsupportedType(t *testing.T) {
	for _, packetType := range []byte{0, 6, 7, 255} {
		raw := protocol.EncodeDraARLv1(
			"test", "", 105, packetType, protocol.DraARLDevModelBrowser, 0, "", nil,
		)
		_, err := DecodeWSPacket(raw)
		if err == nil || !strings.Contains(err.Error(), "unsupported packet type") {
			t.Fatalf("DecodeWSPacket type %d error = %v", packetType, err)
		}
	}
}

func TestDecodeWSPacketRejectsPayloadBeyondProtocolMaximum(t *testing.T) {
	payload := make([]byte, protocol.DraARLv1MaxPacketSize-protocol.DraARLv1HeaderSize+1)
	raw := protocol.EncodeDraARLv1(
		"test", "", 105, protocol.DraARLTypeOpus16K, protocol.DraARLDevModelBrowser, 0, "", payload,
	)
	if len(raw) != protocol.DraARLv1MaxPacketSize+1 {
		t.Fatalf("test packet length=%d", len(raw))
	}
	if _, err := DecodeWSPacket(raw); err == nil || !strings.Contains(err.Error(), "packet too large") {
		t.Fatalf("DecodeWSPacket oversized error=%v", err)
	}
}
