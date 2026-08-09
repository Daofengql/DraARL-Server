package udphub

import (
	"bytes"
	"net"
	"testing"
	"time"

	"draarl/internal/protocol"
)

func TestTryAcquireScheduledBroadcastQuietAndBusyResults(t *testing.T) {
	env := setupRouteTest(t, 61600, false)
	oldHooks := centerHooks()
	SetCenterInterconnectHooks(CenterInterconnectHooks{})
	t.Cleanup(func() { SetCenterInterconnectHooks(oldHooks) })
	start := time.Date(2026, 8, 9, 5, 0, 0, 0, time.UTC)
	ResetAcceptedVoiceActivity(start)

	if lease, last, result := TryAcquireScheduledBroadcast(env.groupA, 1, start.Add(5*time.Second-time.Nanosecond), 5*time.Second); lease != nil || result != ScheduledBroadcastRecentVoice || !last.Equal(start) {
		t.Fatalf("pre-boundary acquire: lease=%v last=%v result=%s", lease, last, result)
	}
	lease, _, result := TryAcquireScheduledBroadcast(env.groupA, 1, start.Add(5*time.Second), 5*time.Second)
	if lease == nil || result != ScheduledBroadcastAcquired {
		t.Fatalf("boundary acquire: lease=%v result=%s", lease, result)
	}
	if competing, _, competingResult := TryAcquireScheduledBroadcast(env.groupA, 2, start.Add(5*time.Second), 5*time.Second); competing != nil || competingResult != ScheduledBroadcastDomainBusy {
		t.Fatalf("competing acquire: lease=%v result=%s", competing, competingResult)
	}
	ReleaseScheduledBroadcast(lease)

	lease, _, result = TryAcquireScheduledBroadcast(env.groupA, 2, start.Add(5*time.Second), 5*time.Second)
	if lease == nil || result != ScheduledBroadcastAcquired {
		t.Fatalf("acquire after release: lease=%v result=%s", lease, result)
	}
	acceptedAt := start.Add(5100 * time.Millisecond)
	if !AcceptScheduledBroadcastFrame(lease, acceptedAt) {
		t.Fatal("scheduled frame did not renew its lease")
	}
	ReleaseScheduledBroadcast(lease)
	if got := LastAcceptedVoiceAt([]int{env.groupA}); !got.Equal(acceptedAt) {
		t.Fatalf("scheduled frame activity=%v want=%v", got, acceptedAt)
	}
	if next, _, nextResult := TryAcquireScheduledBroadcast(env.groupA, 3, acceptedAt.Add(5*time.Second-time.Nanosecond), 5*time.Second); next != nil || nextResult != ScheduledBroadcastRecentVoice {
		t.Fatalf("recent scheduled voice acquire: lease=%v result=%s", next, nextResult)
	}
	if next, _, nextResult := TryAcquireScheduledBroadcast(env.groupA, 3, acceptedAt.Add(5*time.Second), 5*time.Second); next == nil || nextResult != ScheduledBroadcastAcquired {
		t.Fatalf("quiet boundary after scheduled voice: lease=%v result=%s", next, nextResult)
	} else {
		ReleaseScheduledBroadcast(next)
	}
}

func TestScheduledBroadcastLeaseStopsOnTopologyChange(t *testing.T) {
	env := setupRouteTest(t, 61800, false)
	oldHooks := centerHooks()
	SetCenterInterconnectHooks(CenterInterconnectHooks{})
	t.Cleanup(func() { SetCenterInterconnectHooks(oldHooks) })
	ResetAcceptedVoiceActivity(time.Now().Add(-10 * time.Second))
	lease, _, result := TryAcquireScheduledBroadcast(env.groupA, 10, time.Now(), 5*time.Second)
	if lease == nil || result != ScheduledBroadcastAcquired {
		t.Fatalf("initial acquire: lease=%v result=%s", lease, result)
	}

	globalGroupLinkCache.Lock()
	globalGroupLinkCache.targetToPeers[env.groupA] = []int{env.groupB}
	globalGroupLinkCache.targetToPeers[env.groupB] = []int{env.groupA}
	globalGroupLinkCache.Unlock()
	resetHalfDuplexDomainCache()
	if AcceptScheduledBroadcastFrame(lease, time.Now()) {
		t.Fatal("topology-changing scheduled lease accepted another frame")
	}
	ReleaseScheduledBroadcast(lease)
}

func TestScheduledBroadcastUsesCenterSpeakerHooks(t *testing.T) {
	env := setupRouteTest(t, 62000, true)
	ResetAcceptedVoiceActivity(time.Now().Add(-10 * time.Second))
	acquired, accepted, released := 0, 0, 0
	oldHooks := centerHooks()
	SetCenterInterconnectHooks(CenterInterconnectHooks{
		Activate: func(*CenterLocalSource) error { return nil },
		AcquireBroadcast: func(runID uint, domainID uint64, now time.Time) bool {
			acquired++
			return runID == 20 && domainID == GetActiveCommunicationDomainID(env.groupA)
		},
		AcceptBroadcastFrame: func(runID uint, domainID uint64, now time.Time) bool {
			accepted++
			return true
		},
		ReleaseBroadcast: func(runID uint, domainID uint64) { released++ },
	})
	t.Cleanup(func() { SetCenterInterconnectHooks(oldHooks) })

	lease, _, result := TryAcquireScheduledBroadcast(env.groupA, 20, time.Now(), 5*time.Second)
	if lease == nil || result != ScheduledBroadcastAcquired || acquired != 1 {
		t.Fatalf("center acquire: lease=%v result=%s acquired=%d", lease, result, acquired)
	}
	if !AcceptScheduledBroadcastFrame(lease, time.Now()) || accepted != 1 {
		t.Fatalf("center frame accepted=%d", accepted)
	}
	if len(lease.DomainGroupIDs) != 2 || lease.DomainGroupIDs[0] != env.groupA || lease.DomainGroupIDs[1] != env.groupB {
		t.Fatalf("center delivery snapshot=%v", lease.DomainGroupIDs)
	}
	ReleaseScheduledBroadcast(lease)
	if released != 1 {
		t.Fatalf("center release count=%d", released)
	}
}

func TestScheduledBroadcastSkipsWithoutAnyReachableReceiver(t *testing.T) {
	env := setupRouteTest(t, 62100, true)
	for _, endpoint := range []routeTestEndpoint{env.udpA1, env.udpA2, env.udpB, env.udpC} {
		endpoint.device.ISOnline = false
	}
	for _, device := range env.wsManager.devices {
		device.disableRecv = true
	}
	InvalidateDomainReceiverCache()
	ResetAcceptedVoiceActivity(time.Now().Add(-10 * time.Second))

	if lease, _, result := TryAcquireScheduledBroadcast(env.groupA, 21, time.Now(), 5*time.Second); lease != nil || result != ScheduledBroadcastNoReceiver {
		t.Fatalf("receiverless acquire: lease=%v result=%s", lease, result)
	}

	// A no-receiver decision must release the local arbiter immediately.
	env.wsA.disableRecv = false
	lease, _, result := TryAcquireScheduledBroadcast(env.groupA, 22, time.Now(), 5*time.Second)
	if lease == nil || result != ScheduledBroadcastAcquired {
		t.Fatalf("WS-only acquire after receiverless skip: lease=%v result=%s", lease, result)
	}
	ReleaseScheduledBroadcast(lease)
	env.wsA.disableRecv = true

	oldHooks := centerHooks()
	edgeReachable := false
	released := 0
	SetCenterInterconnectHooks(CenterInterconnectHooks{
		Activate:             func(*CenterLocalSource) error { return nil },
		AcquireBroadcast:     func(uint, uint64, time.Time) bool { return true },
		ReleaseBroadcast:     func(uint, uint64) { released++ },
		HasBroadcastReceiver: func(uint64) bool { return edgeReachable },
	})
	t.Cleanup(func() { SetCenterInterconnectHooks(oldHooks) })

	if lease, _, result = TryAcquireScheduledBroadcast(env.groupA, 23, time.Now(), 5*time.Second); lease != nil || result != ScheduledBroadcastNoReceiver || released != 1 {
		t.Fatalf("edge-unreachable acquire: lease=%v result=%s released=%d", lease, result, released)
	}
	edgeReachable = true
	lease, _, result = TryAcquireScheduledBroadcast(env.groupA, 24, time.Now(), 5*time.Second)
	if lease == nil || result != ScheduledBroadcastAcquired {
		t.Fatalf("edge-only acquire: lease=%v result=%s", lease, result)
	}
	ReleaseScheduledBroadcast(lease)
	if released != 2 {
		t.Fatalf("edge-only release count=%d", released)
	}
}

func TestBroadcastSourceRoutesFixedSnapshotAndRetainsNormalTailHold(t *testing.T) {
	env := setupRouteTest(t, 62200, true)
	ghostConn := listenRouteTestUDP(t)
	ghostAddr := ghostConn.LocalAddr().(*net.UDPAddr)
	udpGhost := modernUDPGhost("broadcast-udp-ghost", 62299, ghostAddr.Port, env.groupA, []int{env.groupA, env.groupB})
	udpGhost.UDPAddr = ghostAddr
	if _, err := GlobalUDPGhostManager.RegisterSession(udpGhost); err != nil {
		t.Fatal(err)
	}
	oldHooks := centerHooks()
	var relayedRunID uint
	var relayedSourceGroupID int
	var relayedDomainID uint64
	var relayedPacket []byte
	SetCenterInterconnectHooks(CenterInterconnectHooks{
		RelayBroadcast: func(runID uint, sourceGroupID int, domainID uint64, data []byte) error {
			relayedRunID, relayedSourceGroupID, relayedDomainID = runID, sourceGroupID, domainID
			relayedPacket = append([]byte(nil), data...)
			return nil
		},
	})
	t.Cleanup(func() { SetCenterInterconnectHooks(oldHooks) })

	start := time.Now()
	ResetAcceptedVoiceActivity(start.Add(-10 * time.Second))
	lease, _, result := TryAcquireScheduledBroadcast(env.groupA, 30, start, 5*time.Second)
	if lease == nil || result != ScheduledBroadcastAcquired {
		t.Fatalf("acquire source lease: lease=%v result=%s", lease, result)
	}
	source, err := NewBroadcastSource(lease)
	if err != nil {
		t.Fatal(err)
	}

	// This receiver joins after acquisition. Invalidating the live cache must
	// not expand the run's frozen UDP snapshot.
	groupA, ok := GetGroupFromCache(env.groupA)
	if !ok {
		t.Fatal("source group disappeared")
	}
	pool := groupA.ConnPool.(*CurrentConnPool)
	env.udpC.device.GroupID = env.groupA
	pool.storeConnList(append(pool.snapshotConnList(), env.udpC.device))
	InvalidateDomainReceiverCache()

	payload := []byte{0, 3, 1, 2, 3}
	frame, err := source.SendVoice(payload, start.Add(time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if !frame.UDPQueued || !frame.EdgeRelayed || frame.WSSent != 3 || frame.WSDropped != 0 {
		t.Fatalf("route result=%#v", frame)
	}
	stats := source.Finish()
	if stats.SentPackets != 1 || stats.DroppedPackets != 0 || stats.UDPTargetsSent != 4 || stats.WSTargetsSent != 3 || stats.EdgeRelayErrors != 0 {
		t.Fatalf("source stats=%#v", stats)
	}

	want := protocol.EncodeDraARLv1(
		SystemBroadcastUsername, "", SystemBroadcastSSID,
		protocol.DraARLTypeOpus16K, protocol.DraARLDevModelUnknown,
		0, SystemBroadcastCallSign, payload,
	)
	for _, endpoint := range []routeTestEndpoint{env.udpA1, env.udpA2, env.udpB} {
		assertRouteTestPacket(t, readRouteTestPacket(t, endpoint.conn), want, payload)
	}
	ghostWant, ok := protocol.WithSourceGroupID(want, env.groupA)
	if !ok {
		t.Fatal("build UDP ghost source-group packet")
	}
	assertRouteTestPacket(t, readRouteTestPacket(t, ghostConn), ghostWant, payload)
	assertNoRouteTestPacket(t, ghostConn)
	assertNoRouteTestPacket(t, env.udpC.conn)
	assertRouteTestWSDeliveries(t, env.wsManager, []string{"ws-source", "ws-a", "ws-b"}, want, payload, []int{env.groupA, env.groupB})
	if relayedRunID != 30 || relayedSourceGroupID != env.groupA || relayedDomainID != lease.domainID || !bytes.Equal(relayedPacket, want) {
		t.Fatalf("edge relay run=%d source=%d domain=%d packet_match=%t", relayedRunID, relayedSourceGroupID, relayedDomainID, bytes.Equal(relayedPacket, want))
	}

	other := halfDuplexSpeaker{key: 1234, labelBase: "real-device", ssid: 1}
	if tryAcquireHalfDuplex(env.groupA, other, start.Add(time.Millisecond+halfDuplexVoiceHoldTimeout)) {
		t.Fatal("real device acquired before the post-broadcast hold elapsed")
	}
	if !tryAcquireHalfDuplex(env.groupA, other, start.Add(time.Millisecond+halfDuplexVoiceHoldTimeout+time.Nanosecond)) {
		t.Fatal("real device did not acquire after the post-broadcast hold elapsed")
	}
}

func TestBroadcastSourceCancelReleasesImmediately(t *testing.T) {
	env := setupRouteTest(t, 62400, false)
	oldHooks := centerHooks()
	SetCenterInterconnectHooks(CenterInterconnectHooks{})
	t.Cleanup(func() { SetCenterInterconnectHooks(oldHooks) })
	now := time.Now()
	ResetAcceptedVoiceActivity(now.Add(-10 * time.Second))
	lease, _, result := TryAcquireScheduledBroadcast(env.groupA, 31, now, 5*time.Second)
	if lease == nil || result != ScheduledBroadcastAcquired {
		t.Fatalf("acquire source lease: lease=%v result=%s", lease, result)
	}
	source, err := NewBroadcastSource(lease)
	if err != nil {
		t.Fatal(err)
	}
	source.Cancel()
	if _, err := source.SendVoice([]byte{0, 1, 1}, now); err != ErrBroadcastLeaseLost {
		t.Fatalf("send after cancel error=%v", err)
	}
	if !tryAcquireHalfDuplex(env.groupA, halfDuplexSpeaker{key: 5678, labelBase: "real-device"}, now) {
		t.Fatal("cancelled broadcast retained the half-duplex lease")
	}
}
