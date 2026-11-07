package protocol

import "testing"

func TestUsernameSSIDParsingDoesNotWrap(t *testing.T) {
	for _, tt := range []struct {
		input string
		name  string
		ssid  byte
		valid bool
	}{
		{"user-0", "user", 0, true}, {"user-with-dash-255", "user-with-dash", 255, true},
		{"用户-12", "用户", 12, true}, {"user-256", "", 0, false}, {"user-9999999999999", "", 0, false},
		{"-1", "", 0, false}, {"user-", "", 0, false}, {"user-a", "", 0, false}, {"user", "", 0, false},
	} {
		t.Run(tt.input, func(t *testing.T) {
			name, ssid, err := ParseUsernameSSID(tt.input)
			if (err == nil) != tt.valid || name != tt.name || ssid != tt.ssid {
				t.Fatalf("got (%q,%d,%v), want (%q,%d,valid=%v)", name, ssid, err, tt.name, tt.ssid, tt.valid)
			}
		})
	}
}

func TestEncodersRejectUnrepresentableLengths(t *testing.T) {
	payload := make([]byte, 65535-DraARLv1HeaderSize+1)
	if got := EncodeDraARLv1("user", "password", 1, DraARLTypeHeartbeat, 1, 0, "CALL", payload); got != nil {
		t.Fatal("oversized encoder output")
	}
	if got := EncodeHeartbeatResponse(&DraARLv1Packet{DATA: payload}, "CALL"); got != nil {
		t.Fatal("oversized heartbeat output")
	}
	if got := RewriteForwardHeader(make([]byte, 65536), "user", "CALL", 1, DraARLTypeHeartbeat, 1, 0); got != nil {
		t.Fatal("oversized forwarded output")
	}
}

func BenchmarkForwardHeader(b *testing.B) {
	payload := make([]byte, 160)
	src := EncodeDraARLv1("bench", "secret123", 1, DraARLTypeOpus16K, 1, 123, "CALL", payload)
	b.Run("rewrite_pool", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(src)))
		for b.Loop() {
			out := RewriteForwardHeader(src, "bench", "CALL", 1, DraARLTypeOpus16K, 1, 123)
			ReleaseForwardPacket(out)
		}
	})
	b.Run("fresh_encode", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(src)))
		for b.Loop() {
			benchmarkPacket = EncodeDraARLv1("bench", "", 1, DraARLTypeOpus16K, 1, 123, "CALL", payload)
		}
	})
}

var benchmarkPacket []byte
