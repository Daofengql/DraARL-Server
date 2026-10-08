package udphub

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"draarl/internal/models"
	"draarl/internal/protocol"
)

func TestActivateCenterLocalDeviceContextHonorsCancellation(t *testing.T) {
	oldHooks := centerHooks()
	called := atomic.Bool{}
	SetCenterInterconnectHooks(CenterInterconnectHooks{
		ActivateContext: func(ctx context.Context, _ *CenterLocalSource) error {
			called.Store(true)
			return ctx.Err()
		},
	})
	t.Cleanup(func() { SetCenterInterconnectHooks(oldHooks) })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := ActivateCenterLocalDeviceContext(ctx, &models.Device{ID: 1})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled activation error=%v, want context.Canceled", err)
	}
	if called.Load() {
		t.Fatal("canceled activation invoked the interconnect hook")
	}
}

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

func TestDeliverCenterPeerPacketUsesStableVirtualIdentity(t *testing.T) {
	env := setupRouteTest(t, 9700, false)
	payload := []byte{4, 5, 6}
	if !DeliverCenterPeerAudio(env.groupA, payload, "Server-A", "A", "bridge", "bidirectional") {
		t.Fatal("centre peer packet was not delivered")
	}
	receivedWire := readRouteTestPacket(t, env.udpA1.conn)
	received, err := protocol.NewDraARLv1Packet(nil, receivedWire)
	if err != nil {
		t.Fatalf("decode virtual packet: %v", err)
	}
	if received.Username != "Server-A" || received.CallSign != "Server-A" || received.SSID != protocol.SSIDRangeInterconnectMin || received.DevModel != protocol.DraARLDevModelInterconnect {
		t.Fatalf("unexpected virtual identity: username=%q callsign=%q ssid=%d model=%d", received.Username, received.CallSign, received.SSID, received.DevModel)
	}
	if string(received.DATA) != string(payload) {
		t.Fatalf("virtual payload=%v want %v", received.DATA, payload)
	}
}
