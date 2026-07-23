package interconnect

import (
	"bytes"
	"net"
	"testing"
	"time"

	"draarl/internal/udphub"
)

func TestEdgeReceiverPlanCachesPartitionsAndTracksPolicyChanges(t *testing.T) {
	endpoint, err := udphub.NewEdgeEndpoint("127.0.0.1:0", "", func([]byte, *net.UDPAddr, *net.UDPAddr) {})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = endpoint.Close() })

	sourceConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sourceConn.Close() })
	targetConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = targetConn.Close() })

	gateway := &EdgeGateway{
		endpoint: endpoint,
		sessions: map[uint64]*edgeDeviceSession{
			1: {Grant: DeviceGrant{SessionID: 1, DeviceID: 11, Username: "source", SSID: 1, DomainID: 7}, Addr: sourceConn.LocalAddr().(*net.UDPAddr)},
			2: {Grant: DeviceGrant{SessionID: 2, DeviceID: 12, Username: "target", SSID: 2, DomainID: 7}, Addr: targetConn.LocalAddr().(*net.UDPAddr)},
			3: {Grant: DeviceGrant{SessionID: 3, DeviceID: 13, Username: "other", SSID: 3, DomainID: 8}, Addr: targetConn.LocalAddr().(*net.UDPAddr)},
		},
		byIdentity: make(map[string]uint64),
	}
	first := gateway.receiverPlan(7)
	if first == nil || first.Len() != 2 {
		t.Fatalf("first receiver plan len = %d, want 2", first.Len())
	}
	if second := gateway.receiverPlan(7); second != first {
		t.Fatal("unchanged receiver plan was rebuilt")
	}
	stats := gateway.ReceiverCacheSnapshot()
	if stats.Rebuilds != 1 || stats.Hits == 0 || stats.MaxEntries != 2 {
		t.Fatalf("unexpected receiver cache stats: %#v", stats)
	}

	payload := []byte("edge-cached-fanout")
	gateway.localFanout(1, 7, payload)
	buffer := make([]byte, 128)
	_ = targetConn.SetReadDeadline(time.Now().Add(time.Second))
	n, _, err := targetConn.ReadFromUDP(buffer)
	if err != nil || !bytes.Equal(buffer[:n], payload) {
		t.Fatalf("target delivery n=%d err=%v payload=%q", n, err, buffer[:n])
	}
	_ = sourceConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if _, _, err := sourceConn.ReadFromUDP(buffer); err == nil {
		t.Fatal("source received its own cached fan-out")
	}

	gateway.mu.Lock()
	gateway.sessions[2].Grant.DisableRecv = true
	gateway.invalidateReceiverPlansLocked()
	gateway.mu.Unlock()
	updated := gateway.receiverPlan(7)
	if updated == nil || updated == first || updated.Len() != 1 {
		t.Fatalf("updated receiver plan = %#v len=%d", updated, updated.Len())
	}
	gateway.localFanout(1, 7, payload)
	_ = targetConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if _, _, err := targetConn.ReadFromUDP(buffer); err == nil {
		t.Fatal("disabled receiver still received cached fan-out")
	}
	stats = gateway.ReceiverCacheSnapshot()
	if stats.Generation != 1 || stats.Rebuilds != 2 {
		t.Fatalf("receiver cache did not rebuild after policy change: %#v", stats)
	}
}
