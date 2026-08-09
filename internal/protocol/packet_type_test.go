package protocol

import (
	"strconv"
	"strings"
	"testing"
)

func TestDraARLCorePacketTypeValues(t *testing.T) {
	tests := []struct {
		name string
		got  byte
		want byte
	}{
		{name: "JWTAuth", got: DraARLTypeJWTAuth, want: 1},
		{name: "Heartbeat", got: DraARLTypeHeartbeat, want: 2},
		{name: "Config", got: DraARLTypeConfig, want: 3},
		{name: "TextMessage", got: DraARLTypeTextMessage, want: 4},
		{name: "Opus16K", got: DraARLTypeOpus16K, want: 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("packet type = %d, want %d", tt.got, tt.want)
			}
			if !IsSupportedDraARLType(tt.got) {
				t.Fatalf("packet type %d is not supported", tt.got)
			}
		})
	}
}

func TestDraARLDecoderRejectsUnsupportedPacketTypes(t *testing.T) {
	for _, packetType := range []byte{0, 6, 7, 255} {
		t.Run(strconv.Itoa(int(packetType)), func(t *testing.T) {
			raw := EncodeDraARLv1("test", "", 1, packetType, DraARLDevModelESP32NoRadio, 0, "", nil)
			var packet DraARLv1Packet
			err := packet.Decode(raw)
			if err == nil || !strings.Contains(err.Error(), "unsupported packet type") {
				t.Fatalf("Decode type %d error = %v", packetType, err)
			}
		})
	}
}
