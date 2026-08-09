package aprs

import "testing"

func TestIsSameEndpoint(t *testing.T) {
	tests := []struct {
		name           string
		hostA, portA   string
		hostB, portB   string
		expectSameNode bool
	}{
		{
			name:           "same host and same port",
			hostA:          "192.168.1.10",
			portA:          "7788",
			hostB:          "192.168.1.10",
			portB:          "7788",
			expectSameNode: true,
		},
		{
			name:           "same host but different port should not be filtered",
			hostA:          "192.168.1.10",
			portA:          "7789",
			hostB:          "192.168.1.10",
			portB:          "7788",
			expectSameNode: false,
		},
		{
			name:           "domain comparison should be case-insensitive",
			hostA:          "PTT.4L2.CN",
			portA:          "9000",
			hostB:          "ptt.4l2.cn",
			portB:          "9000",
			expectSameNode: true,
		},
		{
			name:           "self address with scheme and port should still match",
			hostA:          "example.com",
			portA:          "9000",
			hostB:          "udp://EXAMPLE.COM:9000",
			portB:          "9000",
			expectSameNode: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isSameEndpoint(tc.hostA, tc.portA, tc.hostB, tc.portB)
			if got != tc.expectSameNode {
				t.Fatalf("isSameEndpoint(%q:%q, %q:%q) = %v, want %v",
					tc.hostA, tc.portA, tc.hostB, tc.portB, got, tc.expectSameNode)
			}
		})
	}
}

func TestDecodeMsgFromAPRS(t *testing.T) {
	t.Run("valid endpoint", func(t *testing.T) {
		host, port, err := decodeMsgFromAPRS("@udp://10.0.0.5:7788,TestSite")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if host != "10.0.0.5" || port != "7788" {
			t.Fatalf("unexpected decode result: host=%q port=%q", host, port)
		}
	})

	t.Run("invalid short payload", func(t *testing.T) {
		if _, _, err := decodeMsgFromAPRS(""); err == nil {
			t.Fatal("expected error for empty payload")
		}
	})

	t.Run("invalid endpoint without port", func(t *testing.T) {
		if _, _, err := decodeMsgFromAPRS("@udp://10.0.0.5,TestSite"); err == nil {
			t.Fatal("expected error for missing port")
		}
	})
}

