package websocket

import (
	"strings"
	"testing"

	"draarl/internal/protocol"
)

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
