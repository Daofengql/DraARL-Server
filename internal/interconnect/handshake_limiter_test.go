package interconnect

import (
	"net"
	"testing"
	"time"
)

func TestHandshakeLimiterReclaimsStaleEntriesFromOrderedCursor(t *testing.T) {
	limiter := newHandshakeLimiter(2)
	minute := time.Unix(120, 0)
	limiter.mu.Lock()
	for i := 0; i < maxTrackedHandshakeIPs; i++ {
		key := "stale-" + string(rune(i))
		limiter.entries[key] = handshakeAttempt{minute: minute.Unix()/60 - 1, attempts: 1}
		limiter.orderIndex[key] = len(limiter.order)
		limiter.order = append(limiter.order, key)
	}
	limiter.mu.Unlock()

	if !limiter.allow(&net.UDPAddr{IP: net.ParseIP("192.0.2.200"), Port: 42000}, "node-new", minute) {
		t.Fatal("new handshake was rejected despite all tracked entries being stale")
	}
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if _, ok := limiter.entries["192.0.2.200|node-new"]; !ok {
		t.Fatal("new handshake entry was not retained")
	}
}

func TestHandshakeLimiterPreservesCurrentMinuteLimit(t *testing.T) {
	limiter := newHandshakeLimiter(2)
	now := time.Unix(600, 0)
	addr := &net.UDPAddr{IP: net.ParseIP("198.51.100.5"), Port: 43000}
	if !limiter.allow(addr, "node-a", now) || !limiter.allow(addr, "node-a", now) {
		t.Fatal("current-minute handshake attempts were unexpectedly rejected")
	}
	if limiter.allow(addr, "node-a", now) {
		t.Fatal("handshake limit was not enforced")
	}
}
