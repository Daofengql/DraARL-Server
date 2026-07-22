package udphub

import (
	"sync/atomic"
	"testing"

	"draarl/internal/protocol"
)

func TestDeliverInterconnectPacketFansOutLocallyWithoutUpstreamLoop(t *testing.T) {
	env := setupRouteTest(t, 9500, true)
	oldHooks := centerHooks()
	var relayed atomic.Int64
	SetCenterInterconnectHooks(CenterInterconnectHooks{Relay: func(CenterLocalSource, []byte) error {
		relayed.Add(1)
		return nil
	}})
	t.Cleanup(func() { SetCenterInterconnectHooks(oldHooks) })

	payload := []byte{9, 8, 7}
	wire := protocol.EncodeDraARLv1("edge-source", "", 3, protocol.DraARLTypeOpus16K, protocol.DraARLDevModelESP32NoRadio, 0, "BG5EDGE", payload)
	domainID := GetActiveCommunicationDomainID(env.groupA)
	if domainID == 0 || !DeliverInterconnectPacket(domainID, wire) {
		t.Fatalf("interconnect local delivery failed: domain=%d", domainID)
	}
	for _, endpoint := range []routeTestEndpoint{env.udpA1, env.udpA2, env.udpB} {
		assertRouteTestPacket(t, readRouteTestPacket(t, endpoint.conn), wire, payload)
	}
	assertNoRouteTestPacket(t, env.udpC.conn)
	assertRouteTestWSDeliveries(t, env.wsManager, []string{"ws-source", "ws-a", "ws-b"}, wire, payload, []int{env.groupA, env.groupB})
	if relayed.Load() != 0 {
		t.Fatalf("edge downstream was uploaded again: relays=%d", relayed.Load())
	}
}

func TestDeliverInterconnectPacketRejectsCredentialAndUnknownDomain(t *testing.T) {
	env := setupRouteTest(t, 9600, false)
	wire := protocol.EncodeDraARLv1("edge-source", "secret", 3, protocol.DraARLTypeTextMessage, protocol.DraARLDevModelESP32NoRadio, 0, "BG5EDGE", []byte("hello"))
	if DeliverInterconnectPacket(GetActiveCommunicationDomainID(env.groupA), wire) {
		t.Fatal("credential-bearing downstream was accepted")
	}
	if DeliverInterconnectPacket(0xdeadbeef, protocol.EncodeDraARLv1("edge-source", "", 3, protocol.DraARLTypeTextMessage, 0, 0, "BG5EDGE", []byte("hello"))) {
		t.Fatal("unknown communication domain was accepted")
	}
	for _, endpoint := range []routeTestEndpoint{env.udpA1, env.udpA2, env.udpB, env.udpC} {
		assertNoRouteTestPacket(t, endpoint.conn)
	}
}
