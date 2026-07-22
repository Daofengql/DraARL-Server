package interconnect

import (
	"net"
	"testing"
	"time"

	"draarl/internal/protocol"
)

func TestEdgeExpiresInactiveAndExpiredGrantSessions(t *testing.T) {
	now := time.Now()
	reports := make([]DeviceSessionReport, 0, 2)
	gateway := &EdgeGateway{
		sessions: make(map[uint64]*edgeDeviceSession), byIdentity: make(map[string]uint64),
		pending:         map[uint64]*pendingDeviceAuth{11: {identity: "old-1", requestedAt: now.Add(-6 * time.Second)}, 12: {identity: "fresh-1", requestedAt: now}},
		pendingIdentity: map[string]uint64{"old-1": 11, "fresh-1": 12},
		sessionTimeout:  20 * time.Second, reportSession: func(report DeviceSessionReport) { reports = append(reports, report) },
	}
	gateway.sessions[1] = &edgeDeviceSession{Grant: DeviceGrant{SessionID: 1, SessionEpoch: 1, DeviceID: 7, Username: "alice", SSID: 1, ExpiresAtMillis: now.Add(time.Minute).UnixMilli()}, LastSeen: now.Add(-21 * time.Second)}
	gateway.sessions[2] = &edgeDeviceSession{Grant: DeviceGrant{SessionID: 2, SessionEpoch: 2, DeviceID: 8, Username: "bob", SSID: 1, ExpiresAtMillis: now.Add(-time.Second).UnixMilli()}, LastSeen: now}
	gateway.sessions[3] = &edgeDeviceSession{Grant: DeviceGrant{SessionID: 3, SessionEpoch: 3, DeviceID: 9, Username: "carol", SSID: 1, ExpiresAtMillis: now.Add(time.Minute).UnixMilli()}, LastSeen: now}
	gateway.byIdentity["alice-1"], gateway.byIdentity["bob-1"], gateway.byIdentity["carol-1"] = 1, 2, 3
	if expired := gateway.expireDeviceSessions(now); expired != 2 {
		t.Fatalf("expired sessions=%d want=2", expired)
	}
	if len(gateway.sessions) != 1 || gateway.sessions[3] == nil || gateway.byIdentity["carol-1"] != 3 {
		t.Fatalf("fresh session was changed: sessions=%#v identities=%#v", gateway.sessions, gateway.byIdentity)
	}
	if gateway.pending[11] != nil || gateway.pendingIdentity["old-1"] != 0 || gateway.pending[12] == nil || gateway.pendingIdentity["fresh-1"] != 12 {
		t.Fatalf("pending auth expiry failed: pending=%#v identities=%#v", gateway.pending, gateway.pendingIdentity)
	}
	reasons := make(map[string]int, len(reports))
	for _, report := range reports {
		reasons[report.Reason]++
	}
	if len(reports) != 2 || reasons["device_timeout"] != 1 || reasons["grant_expired"] != 1 {
		t.Fatalf("offline reports=%#v", reports)
	}
}

func TestEdgeGrantRenewalUpdatesExistingSessionInPlace(t *testing.T) {
	now := time.Now()
	wire := protocol.EncodeDraARLv1("alice", "devpass1", 1, protocol.DraARLTypeHeartbeat, protocol.DraARLDevModelESP32NoRadio, 0, "", nil)
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 40001}
	gateway := &EdgeGateway{
		sessions:   map[uint64]*edgeDeviceSession{10: {Grant: DeviceGrant{SessionID: 10, SessionEpoch: 4, Username: "alice", SSID: 1}, LastSeen: now.Add(-time.Minute)}},
		byIdentity: map[string]uint64{"alice-1": 10}, pending: map[uint64]*pendingDeviceAuth{1: {addr: addr, realAddr: addr, wire: wire, identity: "alice-1", requestedAt: now}}, pendingIdentity: map[string]uint64{"alice-1": 1},
	}
	gateway.finishAuth(DeviceAuthResponse{RequestID: 1, Success: true, Grant: &DeviceGrant{SessionID: 10, SessionEpoch: 4, Username: "alice", SSID: 1, DomainID: 9, ExpiresAtMillis: now.Add(2 * time.Minute).UnixMilli()}})
	if len(gateway.sessions) != 1 {
		t.Fatalf("renewal created duplicate sessions: %#v", gateway.sessions)
	}
	session := gateway.sessions[10]
	if session == nil || session.Grant.ExpiresAtMillis <= now.UnixMilli() || !udpAddrEqual(session.RealAddr, addr) {
		t.Fatalf("renewed session not updated in place: %#v", session)
	}
}

func TestEdgeSessionRenewalIsBoundToCurrentSession(t *testing.T) {
	now := time.Now()
	var request DeviceSessionRenewRequest
	gateway := &EdgeGateway{
		sessions:         map[uint64]*edgeDeviceSession{10: {Grant: DeviceGrant{SessionID: 10, SessionEpoch: 4, Username: "alice", SSID: 1, ExpiresAtMillis: now.Add(10 * time.Second).UnixMilli()}, LastSeen: now}},
		byIdentity:       map[string]uint64{"alice-1": 10},
		pendingRenewals:  make(map[uint64]pendingDeviceRenewal),
		renewingSessions: make(map[uint64]uint64),
		renewSession:     func(got DeviceSessionRenewRequest) { request = got },
	}
	gateway.requestSessionRenewal(10, 4, now)
	if request.RequestID == 0 || request.SessionID != 10 || request.SessionEpoch != 4 {
		t.Fatalf("renew request=%#v", request)
	}
	if gateway.finishSessionRenewal(DeviceSessionRenewResponse{RequestID: request.RequestID, SessionID: 10, SessionEpoch: 5, Success: true, ExpiresAtMillis: now.Add(time.Minute).UnixMilli()}, now) {
		t.Fatal("wrong epoch renewal was accepted")
	}
	if gateway.sessions[10].Grant.ExpiresAtMillis != now.Add(10*time.Second).UnixMilli() {
		t.Fatal("wrong epoch renewal changed expiry")
	}
	gateway.requestSessionRenewal(10, 4, now)
	if !gateway.finishSessionRenewal(DeviceSessionRenewResponse{RequestID: request.RequestID, SessionID: 10, SessionEpoch: 4, Success: true, ExpiresAtMillis: now.Add(time.Minute).UnixMilli()}, now) {
		t.Fatal("valid renewal was rejected")
	}
	if gateway.sessions[10].Grant.ExpiresAtMillis != now.Add(time.Minute).UnixMilli() {
		t.Fatal("valid renewal did not update expiry")
	}
}
