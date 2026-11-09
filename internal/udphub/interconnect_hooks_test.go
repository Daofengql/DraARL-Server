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

func TestActivationCanceledAfterGrantRevokesUncommittedSession(t *testing.T) {
	oldHooks := centerHooks()
	t.Cleanup(func() { SetCenterInterconnectHooks(oldHooks) })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	revoked := false
	SetCenterInterconnectHooks(CenterInterconnectHooks{
		ActivateContext: func(_ context.Context, source *CenterLocalSource) error {
			source.SessionID, source.SessionEpoch = 42, 1
			cancel()
			return nil
		},
		Revoke: func(source CenterLocalSource) {
			revoked = source.SessionID == 42 && source.SessionEpoch == 1
		},
	})
	dev := &models.Device{ID: 1}
	if err := ActivateCenterLocalDeviceContext(ctx, dev); !errors.Is(err, context.Canceled) {
		t.Fatalf("activation error=%v", err)
	}
	if !revoked || dev.RuntimeSnapshot().InterconnectSessionID != 0 {
		t.Fatal("canceled activation retained an orphaned local grant")
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
