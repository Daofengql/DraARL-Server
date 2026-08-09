package interconnect

import (
	"testing"
	"time"

	"draarl/internal/protocol"
)

func speakerClaim(requestID, sessionID, epoch, domainID, leaseID uint64) SpeakerLeaseControl {
	return SpeakerLeaseControl{Action: SpeakerLeaseActionClaim, RequestID: requestID, SessionID: sessionID, SessionEpoch: epoch, DomainID: domainID, LeaseID: leaseID}
}

func TestSpeakerLeaseArbitratesAndRefreshesOneDomain(t *testing.T) {
	manager := NewSpeakerLeaseManager()
	start := time.Unix(100, 0)
	first := manager.Claim("edge-a", 10, speakerClaim(1, 101, 1, 7, 0), start)
	if first.Action != SpeakerLeaseActionGrant || first.LeaseID == 0 || first.TTLMillis != SpeakerLeaseTTL.Milliseconds() {
		t.Fatalf("first claim=%#v", first)
	}
	denied := manager.Claim("edge-b", 20, speakerClaim(2, 202, 1, 7, 0), start.Add(100*time.Millisecond))
	if denied.Action != SpeakerLeaseActionDeny || denied.RetryAfterMillis <= 0 {
		t.Fatalf("competing claim=%#v", denied)
	}
	frame := RelayFrame{SessionID: 101, SessionEpoch: 1, DomainID: 7, SpeakerLeaseID: first.LeaseID}
	if !manager.AcceptFrame("edge-a", 10, frame, start.Add(500*time.Millisecond)) {
		t.Fatal("current speaker frame was rejected")
	}
	if manager.AcceptFrame("edge-a", 10, RelayFrame{SessionID: 101, SessionEpoch: 1, DomainID: 7, SpeakerLeaseID: first.LeaseID + 1}, start.Add(510*time.Millisecond)) {
		t.Fatal("wrong lease ID was accepted")
	}
	stillDenied := manager.Claim("edge-b", 20, speakerClaim(3, 202, 1, 7, 0), start.Add(1200*time.Millisecond))
	if stillDenied.Action != SpeakerLeaseActionDeny {
		t.Fatalf("refreshed speaker was displaced early: %#v", stillDenied)
	}
	granted := manager.Claim("edge-b", 20, speakerClaim(4, 202, 1, 7, 0), start.Add(1401*time.Millisecond))
	if granted.Action != SpeakerLeaseActionGrant || granted.LeaseID == first.LeaseID {
		t.Fatalf("idle domain was not transferred: %#v", granted)
	}
}

func TestSpeakerLeasesAreIndependentAndReleaseWithLifecycle(t *testing.T) {
	manager := NewSpeakerLeaseManager()
	now := time.Unix(200, 0)
	for domain := uint64(1); domain <= 5; domain++ {
		grant := manager.Claim("edge-a", 10, speakerClaim(domain, 100+domain, 1, domain, 0), now)
		if grant.Action != SpeakerLeaseActionGrant {
			t.Fatalf("domain %d claim=%#v", domain, grant)
		}
	}
	if released := manager.ReleaseSession(103, 1); released != 1 {
		t.Fatalf("released session domains=%d", released)
	}
	if got := manager.Claim("edge-b", 20, speakerClaim(10, 999, 1, 3, 0), now.Add(time.Millisecond)); got.Action != SpeakerLeaseActionGrant {
		t.Fatalf("released domain remained blocked: %#v", got)
	}
	if released := manager.ReleaseNode("edge-a", 10); released != 4 {
		t.Fatalf("released node domains=%d", released)
	}
}

func TestSpeakerLeaseReleaseRequiresExactOwnerAndLease(t *testing.T) {
	manager := NewSpeakerLeaseManager()
	now := time.Unix(300, 0)
	grant := manager.Claim("edge-a", 10, speakerClaim(1, 101, 2, 9, 0), now)
	release := SpeakerLeaseControl{Action: SpeakerLeaseActionRelease, SessionID: 101, SessionEpoch: 2, DomainID: 9, LeaseID: grant.LeaseID}
	if manager.Release("edge-a", 11, release) || manager.Release("edge-a", 10, SpeakerLeaseControl{Action: SpeakerLeaseActionRelease, SessionID: 101, SessionEpoch: 2, DomainID: 9, LeaseID: grant.LeaseID + 1}) {
		t.Fatal("stale owner or lease released the active speaker")
	}
	if !manager.Release("edge-a", 10, release) {
		t.Fatal("exact owner failed to release its lease")
	}
}

func TestSpeakerLeaseControlValidation(t *testing.T) {
	valid := []SpeakerLeaseControl{
		speakerClaim(1, 2, 3, 4, 0),
		{Action: SpeakerLeaseActionGrant, RequestID: 1, SessionID: 2, SessionEpoch: 3, DomainID: 4, LeaseID: 5, TTLMillis: 1200},
		{Action: SpeakerLeaseActionDeny, RequestID: 1, SessionID: 2, SessionEpoch: 3, DomainID: 4, RetryAfterMillis: 100},
		{Action: SpeakerLeaseActionRelease, SessionID: 2, SessionEpoch: 3, DomainID: 4, LeaseID: 5},
	}
	for _, message := range valid {
		if err := message.Validate(); err != nil {
			t.Fatalf("valid message %#v: %v", message, err)
		}
	}
	invalid := valid[0]
	invalid.RequestID = 0
	if invalid.Validate() == nil {
		t.Fatal("claim without request ID was accepted")
	}
}

func TestCenterLocalAndEdgeSpeakersShareAuthorityAndRouteRelease(t *testing.T) {
	cluster := NewClusterManager(77)
	defer cluster.Close()
	gateway := NewCenterGateway(cluster, nil)
	edgeSession := &NodeSession{NodeID: "edge-a", SessionID: 10}
	remote := &DeviceGrant{DeviceID: 1, OwnerID: 1, Username: "remote", CallSign: "REMOTE", SSID: 1, GroupID: 1, DomainID: 99}
	if err := gateway.activateDeviceSession(edgeSession, remote); err != nil {
		t.Fatal(err)
	}
	remoteGrant := gateway.speaker.Claim(edgeSession.NodeID, edgeSession.SessionID, speakerClaim(1, remote.SessionID, remote.SessionEpoch, remote.DomainID, 0), time.Now())
	if remoteGrant.Action != SpeakerLeaseActionGrant {
		t.Fatalf("remote claim=%#v", remoteGrant)
	}
	local := &DeviceGrant{DeviceID: 2, OwnerID: 2, Username: "local", CallSign: "LOCAL", SSID: 1, GroupID: 1, DomainID: 99}
	if err := gateway.ActivateLocalDevice(local); err != nil {
		t.Fatal(err)
	}
	if gateway.AcquireLocalVoice(*local) {
		t.Fatal("centre-local speaker displaced an active edge speaker")
	}
	updated, err := gateway.UpdateActiveDeviceRoute(remote.DeviceID, remote.GroupID, remote.DomainID, true, false)
	if err != nil || !updated {
		t.Fatalf("disable remote speaker: updated=%v err=%v", updated, err)
	}
	if !gateway.AcquireLocalVoice(*local) {
		t.Fatal("route disable did not immediately release the edge speaker")
	}
	updated, err = gateway.UpdateActiveDeviceRoute(local.DeviceID, 2, 100, false, false)
	if err != nil || !updated {
		t.Fatalf("move local speaker domain: updated=%v err=%v", updated, err)
	}
	if got := gateway.speaker.Claim("edge-b", 20, speakerClaim(2, 300, 1, 99, 0), time.Now()); got.Action != SpeakerLeaseActionGrant {
		t.Fatalf("old local domain remained leased after move: %#v", got)
	}
}

func TestEdgeSpeakerGrantReleasesBoundedBufferAndRouteChangeClearsLease(t *testing.T) {
	gateway, err := NewEdgeGateway("127.0.0.1:0", nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	grant := DeviceGrant{SessionID: 10, SessionEpoch: 2, DeviceID: 1, Username: "alice", SSID: 1, DomainID: 9}
	gateway.sessions[grant.SessionID] = &edgeDeviceSession{Grant: grant, LastSeen: now}
	gateway.speakerDomains[grant.DomainID] = &edgeSpeakerState{
		sessionID: grant.SessionID, sessionEpoch: grant.SessionEpoch, pendingRequest: 7, pendingSince: now,
		buffered: []edgeBufferedVoice{{grant: grant, inner: make([]byte, DraARLHeaderSize), receivedAt: now}, {grant: grant, inner: make([]byte, DraARLHeaderSize), receivedAt: now}},
	}
	response := SpeakerLeaseControl{Action: SpeakerLeaseActionGrant, RequestID: 7, SessionID: grant.SessionID, SessionEpoch: grant.SessionEpoch, DomainID: grant.DomainID, LeaseID: 99, TTLMillis: SpeakerLeaseTTL.Milliseconds()}
	if !gateway.finishSpeakerLease(response, now.Add(time.Millisecond)) {
		t.Fatal("matching grant was not applied")
	}
	state := gateway.speakerDomains[grant.DomainID]
	if state == nil || state.leaseID != response.LeaseID || state.pendingRequest != 0 || len(state.buffered) != 0 {
		t.Fatalf("edge speaker state=%#v", state)
	}
	projection := NewProjection(1)
	projection.Devices[grant.SessionID] = DeviceRoute{SessionID: grant.SessionID, SessionEpoch: grant.SessionEpoch, DeviceID: grant.DeviceID, DomainID: grant.DomainID, DisableSend: true}
	gateway.applyRoutes(projection)
	if gateway.speakerDomains[grant.DomainID] != nil {
		t.Fatal("disable-send route retained an edge speaker lease")
	}
}

func TestEdgeSpeakerClaimTimeoutDropsBufferedFrames(t *testing.T) {
	gateway, err := NewEdgeGateway("127.0.0.1:0", nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	gateway.speakerDomains[9] = &edgeSpeakerState{
		sessionID: 1, sessionEpoch: 1, pendingRequest: 2, pendingSince: now.Add(-SpeakerClaimTimeout - time.Millisecond),
		buffered: []edgeBufferedVoice{{inner: []byte{1}}, {inner: []byte{2}}},
	}
	gateway.expireSpeakerStates(now)
	state := gateway.speakerDomains[9]
	if state == nil || state.pendingRequest != 0 || len(state.buffered) != 0 || !state.blockedUntil.After(now) {
		t.Fatalf("timed out claim state=%#v", state)
	}
}

func TestScheduledBroadcastSpeakerLeaseCompetesWithDeviceVoice(t *testing.T) {
	gateway := NewCenterGateway(nil, nil)
	now := time.Now()
	domainID := uint64(77)
	if _, ok := gateway.speaker.AcquireLocal(100, 1, domainID, now); !ok {
		t.Fatal("device voice did not acquire speaker lease")
	}
	if gateway.AcquireScheduledBroadcast(9, domainID, now.Add(100*time.Millisecond)) {
		t.Fatal("scheduled broadcast displaced active device voice")
	}
	if !gateway.AcquireScheduledBroadcast(9, domainID, now.Add(SpeakerLeaseIdleTimeout+time.Millisecond)) {
		t.Fatal("scheduled broadcast did not acquire expired domain")
	}
	if !gateway.AcceptScheduledBroadcastFrame(9, domainID, now.Add(SpeakerLeaseIdleTimeout+100*time.Millisecond)) {
		t.Fatal("scheduled broadcast frame did not renew lease")
	}
	if _, ok := gateway.speaker.AcquireLocal(101, 1, domainID, now.Add(SpeakerLeaseIdleTimeout+100*time.Millisecond)); ok {
		t.Fatal("device voice displaced active scheduled broadcast")
	}
	gateway.ReleaseScheduledBroadcast(9, domainID)
	if _, ok := gateway.speaker.AcquireLocal(101, 1, domainID, now.Add(SpeakerLeaseIdleTimeout+101*time.Millisecond)); !ok {
		t.Fatal("scheduled broadcast release retained speaker lease")
	}
}

func TestScheduledBroadcastRelayRequiresItsActiveSpeakerLease(t *testing.T) {
	cluster := NewClusterManager(1)
	defer cluster.Close()
	gateway := NewCenterGateway(cluster, nil)
	now := time.Now()
	domainID := uint64(88)
	wire := protocol.EncodeDraARLv1("system-broadcast", "", 255, protocol.DraARLTypeOpus16K, 0, 0, "AUTO", []byte{0, 1, 1})
	if !gateway.AcquireScheduledBroadcast(10, domainID, now) {
		t.Fatal("scheduled broadcast did not acquire speaker lease")
	}
	if err := gateway.RelayScheduledBroadcast(10, 7, domainID, wire); err != nil {
		t.Fatalf("relay with active lease: %v", err)
	}
	gateway.ReleaseScheduledBroadcast(10, domainID)
	if err := gateway.RelayScheduledBroadcast(10, 7, domainID, wire); err == nil {
		t.Fatal("relay without active lease was accepted")
	}
}

func TestScheduledBroadcastReceiverRequiresConnectedReceivableEdgeRoute(t *testing.T) {
	cluster := NewClusterManager(1)
	defer cluster.Close()
	gateway := NewCenterGateway(cluster, nil)
	domainID := uint64(89)
	route := DeviceRoute{SessionID: 1001, SessionEpoch: 1, DeviceID: 7, DomainID: domainID}
	if err := cluster.UpsertNodeRoute("edge-a", route); err != nil {
		t.Fatal(err)
	}
	if gateway.HasScheduledBroadcastReceiver(domainID) {
		t.Fatal("disconnected edge projection counted as a receiver")
	}

	session := &NodeSession{NodeID: "edge-a", SessionID: 44}
	cluster.OnConnect(session)
	if err := cluster.UpsertNodeRoute(session.NodeID, route); err != nil {
		t.Fatal(err)
	}
	if !gateway.HasScheduledBroadcastReceiver(domainID) {
		t.Fatal("connected receivable edge route was not counted")
	}

	route.DisableRecv = true
	if err := cluster.UpsertNodeRoute(session.NodeID, route); err != nil {
		t.Fatal(err)
	}
	if gateway.HasScheduledBroadcastReceiver(domainID) {
		t.Fatal("receive-disabled edge route counted as a receiver")
	}

	route.DisableRecv = false
	if err := cluster.UpsertNodeRoute(session.NodeID, route); err != nil {
		t.Fatal(err)
	}
	cluster.OnDisconnect(session, nil)
	if gateway.HasScheduledBroadcastReceiver(domainID) {
		t.Fatal("disconnected edge route remained reachable")
	}
}

func BenchmarkSpeakerLeaseAcceptFrameSameDomain(b *testing.B) {
	manager := NewSpeakerLeaseManager()
	now := time.Unix(400, 0)
	grant := manager.Claim("edge-a", 10, speakerClaim(1, 101, 1, 1, 0), now)
	frame := RelayFrame{SessionID: 101, SessionEpoch: 1, DomainID: 1, SpeakerLeaseID: grant.LeaseID}

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if !manager.AcceptFrame("edge-a", 10, frame, now) {
				b.Fatal("active frame was rejected")
			}
		}
	})
}

func BenchmarkSpeakerLeaseAcceptFrameIndependentDomains(b *testing.B) {
	const domainCount = 64
	manager := NewSpeakerLeaseManager()
	now := time.Unix(500, 0)
	var frames [domainCount]RelayFrame
	for index := range frames {
		domainID := uint64(index + 1)
		sessionID := uint64(1000 + index)
		grant := manager.Claim("edge-a", 10, speakerClaim(domainID, sessionID, 1, domainID, 0), now)
		frames[index] = RelayFrame{SessionID: sessionID, SessionEpoch: 1, DomainID: domainID, SpeakerLeaseID: grant.LeaseID}
	}

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		index := 0
		for pb.Next() {
			if !manager.AcceptFrame("edge-a", 10, frames[index], now) {
				b.Fatal("active frame was rejected")
			}
			index = (index + 1) % domainCount
		}
	})
}
