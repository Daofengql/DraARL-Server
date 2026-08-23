package udphub

import (
	"net"
	"testing"
)

func TestGetRealAddrCopiesProxySourceIP(t *testing.T) {
	source := net.IPv4(198, 51, 100, 27)
	info := &ProxyProtocolInfo{IsProxy: true, SourceIP: source, SourcePort: 41000}
	got := GetRealAddr(&net.UDPAddr{IP: net.IPv4(203, 0, 113, 10), Port: 60050}, info)
	if got == nil || !got.IP.Equal(source) || got.Port != 41000 {
		t.Fatalf("real address=%v, want %s:41000", got, source)
	}
	source[12] = 0
	if got.IP[12] == 0 {
		t.Fatal("real address retained mutable PROXY source IP backing storage")
	}
}

func TestSetProxyTrustedCIDRsRejectsInvalidWithoutReplacingSnapshot(t *testing.T) {
	t.Cleanup(func() { _ = setProxyTrustedCIDRs(nil) })
	if err := setProxyTrustedCIDRs([]string{"192.0.2.0/24"}); err != nil {
		t.Fatal(err)
	}
	if !isTrustedProxySource(net.ParseIP("192.0.2.10")) {
		t.Fatal("valid trusted proxy source was rejected")
	}
	if err := setProxyTrustedCIDRs([]string{"not-a-cidr"}); err == nil {
		t.Fatal("invalid trusted proxy CIDR was accepted")
	}
	if !isTrustedProxySource(net.ParseIP("192.0.2.10")) {
		t.Fatal("invalid update replaced the last valid trusted proxy snapshot")
	}
	if isTrustedProxySource(net.ParseIP("203.0.113.10")) {
		t.Fatal("invalid update widened trust to an unrelated source")
	}
}
