package protocol

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFixedWidthTextPreservesUTF8AcrossEncoders(t *testing.T) {
	for _, value := range []string{
		strings.Repeat("中", 11),
		strings.Repeat("a", 31) + "中",
		strings.Repeat("a", 30) + "🙂",
		strings.Repeat("🙂", 9),
		strings.Repeat("a", 40),
	} {
		t.Run(value, func(t *testing.T) {
			source := EncodeDraARLv1("original", "secret", 1, DraARLTypeHeartbeat, 1, 0, "original", []byte{1, 2})
			forward := RewriteForwardHeader(source, value, value, 1, DraARLTypeHeartbeat, 1, 0)
			defer ReleaseForwardPacket(forward)
			packets := [][]byte{
				EncodeDraARLv1(value, "", 1, DraARLTypeHeartbeat, 1, 0, value, nil),
				forward,
				EncodeHeartbeatResponse(&DraARLv1Packet{Username: value, Type: DraARLTypeHeartbeat}, value),
			}
			for _, packet := range packets {
				for _, field := range [][]byte{packet[6:38], packet[54:86]} {
					text := bytes.TrimRight(field, "\x00")
					if !utf8.Valid(text) || !strings.HasPrefix(value, string(text)) || len(text) == 0 {
						t.Fatalf("invalid or corrupted text field: %q", text)
					}
				}
			}
			if !bytes.Equal(source[6:14], []byte("original")) {
				t.Fatal("forward encoding changed the source packet")
			}
		})
	}
}

func TestEncodeHeartbeatResponseRejectsNilRequest(t *testing.T) {
	if got := EncodeHeartbeatResponse(nil, ""); got != nil {
		t.Fatalf("nil request returned %v", got)
	}
}
