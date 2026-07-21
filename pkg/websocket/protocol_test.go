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
