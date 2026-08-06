package interconnect

import (
	"errors"
	"net"
	"slices"
	"testing"
	"time"

	"draarl/internal/protocol"
	"draarl/internal/udphub"
)

func modernEdgeGhost(ownerID int, username, appSessionID, instanceID string, tag uint32, groupID int, domainID uint64, rxGroups []int, rxDomains []uint64) *DeviceGrant {
	return &DeviceGrant{
		OwnerID: ownerID, Username: username, CallSign: "BG5TEST", SSID: protocol.SSIDGhostAndroid,
		DevModel: protocol.DraARLDevModelAndroid, GroupID: groupID, DomainID: domainID,
		RxGroupIDs: append([]int(nil), rxGroups...), RxDomainIDs: append([]uint64(nil), rxDomains...),
		GhostSessionID: appSessionID, ClientInstanceID: instanceID, SessionTag: tag,
		GhostProtocolVersion: protocol.GhostAuthPayloadVersion, SourceGroupV1: true,
		RecoveryTicket:  "test-recovery-ticket",
		ExpiresAtMillis: time.Now().Add(time.Minute).UnixMilli(),
	}
}

func TestModernGhostInstancesKeepIndependentCenterOwnership(t *testing.T) {
	cluster := NewClusterManager(91)
	defer cluster.Close()
	gateway := NewCenterGateway(cluster, nil)
	edgeA := &NodeSession{NodeID: "edge-a", SessionID: 101}
	edgeB := &NodeSession{NodeID: "edge-b", SessionID: 202}
	gateway.OnConnect(edgeA)
	gateway.OnConnect(edgeB)

	first := modernEdgeGhost(7, "alice", "app-session-a", "11111111-1111-4111-8111-111111111111", 101, 10, 90, []int{10, 20}, []uint64{90, 91})
	first.DisableRecv = true
	second := modernEdgeGhost(7, "alice", "app-session-b", "22222222-2222-4222-8222-222222222222", 202, 10, 90, []int{10, 30}, []uint64{90, 92})
	if err := gateway.activateDeviceSession(edgeA, first); err != nil {
		t.Fatal(err)
	}
	if err := gateway.activateDeviceSession(edgeB, second); err != nil {
		t.Fatal(err)
	}
	if first.SessionID == second.SessionID || first.SessionEpoch != 1 || second.SessionEpoch != 1 {
		t.Fatalf("independent instances shared ownership: first=%#v second=%#v", first, second)
	}
	if _, ok := cluster.ResolveRoute(first.SessionID); !ok {
		t.Fatal("first modern ghost route was replaced")
	}
	if _, ok := cluster.ResolveRoute(second.SessionID); !ok {
		t.Fatal("second modern ghost route is missing")
	}

	resolve := func(groupID int) uint64 {
		return map[int]uint64{30: 92, 40: 93}[groupID]
	}
	updated, err := gateway.UpdateActiveGhostRoute(first.GhostSessionID, 30, []int{30, 40}, resolve)
	if err != nil || !updated {
		t.Fatalf("exact ghost route update failed: updated=%v err=%v", updated, err)
	}
	firstRoute, _ := cluster.ResolveRoute(first.SessionID)
	secondRoute, _ := cluster.ResolveRoute(second.SessionID)
	if firstRoute.GroupID != 30 || !slices.Equal(firstRoute.RxDomainIDs, []uint64{92, 93}) || !firstRoute.DisableRecv {
		t.Fatalf("first route was not updated exactly: %#v", firstRoute)
	}
	if secondRoute.GroupID != 10 || len(secondRoute.RxDomainIDs) != 2 || secondRoute.RxDomainIDs[1] != 92 {
		t.Fatalf("sibling route changed with first instance: %#v", secondRoute)
	}

	revoked, err := gateway.RevokeActiveGhost(first.GhostSessionID, "test_disconnect")
	if err != nil || !revoked {
		t.Fatalf("exact ghost revoke failed: revoked=%v err=%v", revoked, err)
	}
	if _, ok := cluster.ResolveRoute(first.SessionID); ok {
		t.Fatal("revoked modern ghost route remains active")
	}
	if _, ok := cluster.ResolveRoute(second.SessionID); !ok {
		t.Fatal("exact revoke removed a sibling instance")
	}
}

func TestEdgeMultiReceiveFanoutUsesExactSessionAndTargetCapability(t *testing.T) {
	endpoint, err := udphub.NewEdgeEndpoint("127.0.0.1:0", "", func([]byte, *net.UDPAddr, *net.UDPAddr) {})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = endpoint.Close() })

	listen := func() *net.UDPConn {
		conn, listenErr := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if listenErr != nil {
			t.Fatal(listenErr)
		}
		t.Cleanup(func() { _ = conn.Close() })
		return conn
	}
	sourceConn, siblingConn, physicalConn, otherPhysicalConn := listen(), listen(), listen(), listen()
	now := time.Now()
	gateway := &EdgeGateway{
		endpoint: endpoint, sessions: map[uint64]*edgeDeviceSession{
			1: {Grant: *modernEdgeGhost(7, "alice", "app-a", "11111111-1111-4111-8111-111111111111", 101, 1001, 7, []int{1001}, []uint64{7}), Addr: sourceConn.LocalAddr().(*net.UDPAddr), LastSeen: now},
			2: {Grant: *modernEdgeGhost(7, "alice", "app-b", "22222222-2222-4222-8222-222222222222", 202, 1001, 7, []int{1001, 1002}, []uint64{7, 8}), Addr: siblingConn.LocalAddr().(*net.UDPAddr), LastSeen: now},
			3: {Grant: DeviceGrant{SessionID: 3, SessionEpoch: 1, DeviceID: 30, Username: "physical", SSID: 3, GroupID: 1001, DomainID: 7}, Addr: physicalConn.LocalAddr().(*net.UDPAddr), LastSeen: now},
			4: {Grant: DeviceGrant{SessionID: 4, SessionEpoch: 1, DeviceID: 40, Username: "other-physical", SSID: 4, GroupID: 1002, DomainID: 8}, Addr: otherPhysicalConn.LocalAddr().(*net.UDPAddr), LastSeen: now},
		},
		byIdentity: make(map[string]uint64), bySessionTag: make(map[uint32]uint64),
	}
	for id, session := range gateway.sessions {
		session.Grant.SessionID = id
	}

	wire := protocol.EncodeDraARLv1("alice", "", protocol.SSIDGhostAndroid, protocol.DraARLTypeTextMessage, protocol.DraARLDevModelAndroid, 0, "BG5TEST", []byte("hello"))
	gateway.localFanout(1, 7, wire, 1001)

	read := func(conn *net.UDPConn) []byte {
		buffer := make([]byte, 1024)
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		n, _, readErr := conn.ReadFromUDP(buffer)
		if readErr != nil {
			t.Fatal(readErr)
		}
		return append([]byte(nil), buffer[:n]...)
	}
	if got := protocol.ReservedUint32(read(siblingConn)[protocol.DraARLv1ReservedOffset:]); got != 1001 {
		t.Fatalf("capable sibling source group=%d want=1001", got)
	}
	if got := protocol.ReservedUint32(read(physicalConn)[protocol.DraARLv1ReservedOffset:]); got != 0 {
		t.Fatalf("physical edge receiver Reserved=%d want=0", got)
	}
	_ = sourceConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if _, _, readErr := sourceConn.ReadFromUDP(make([]byte, 128)); readErr == nil {
		t.Fatal("source session received its own edge fan-out")
	}
	_ = otherPhysicalConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if _, _, readErr := otherPhysicalConn.ReadFromUDP(make([]byte, 128)); readErr == nil {
		t.Fatal("unsubscribed domain receiver received the frame")
	}

	gateway.localFanout(0, 8, wire, 1002)
	if got := protocol.ReservedUint32(read(siblingConn)[protocol.DraARLv1ReservedOffset:]); got != 1002 {
		t.Fatalf("second receive domain source group=%d want=1002", got)
	}
	if got := protocol.ReservedUint32(read(otherPhysicalConn)[protocol.DraARLv1ReservedOffset:]); got != 0 {
		t.Fatalf("physical second-domain receiver Reserved=%d want=0", got)
	}
}

func TestEdgeModernGhostPacketBindingRejectsSpoofedIdentity(t *testing.T) {
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 32001}
	grant := modernEdgeGhost(7, "alice", "app-a", "11111111-1111-4111-8111-111111111111", 0x10203040, 1001, 7, []int{1001}, []uint64{7})
	grant.SessionID, grant.SessionEpoch = 11, 1
	initial := time.Now().Add(-time.Second)
	gateway := &EdgeGateway{
		sessions:   map[uint64]*edgeDeviceSession{11: {Grant: *grant, RealAddr: cloneUDPAddr(addr), Addr: cloneUDPAddr(addr), LastSeen: initial}},
		byIdentity: map[string]uint64{edgeSessionIdentity(*grant): 11}, bySessionTag: map[uint32]uint64{grant.SessionTag: 11},
	}
	link := newEdgeControlLink(&NodeClient{Session: &NodeSession{NodeID: "edge-a", SessionID: 1, Features: NodeSupportedFeatures}}, nil)
	link.markReady()
	gateway.control.Store(link)

	packet := func(username string, ssid, model byte, tag uint32) []byte {
		wire := protocol.EncodeDraARLv1(username, "", ssid, protocol.DraARLTypeTextMessage, model, 0, "", []byte("message"))
		tagged, _ := protocol.WithReservedUint32(wire, tag)
		return tagged
	}
	assertRejected := func(name string, wire []byte, endpoint *net.UDPAddr) {
		t.Helper()
		gateway.sessions[11].LastSeen = initial
		gateway.handleDevicePacket(wire, endpoint, endpoint)
		if !gateway.sessions[11].LastSeen.Equal(initial) {
			t.Fatalf("%s spoof updated session activity", name)
		}
	}
	assertRejected("tag", packet("alice", grant.SSID, grant.DevModel, grant.SessionTag+1), addr)
	assertRejected("username", packet("mallory", grant.SSID, grant.DevModel, grant.SessionTag), addr)
	assertRejected("ssid", packet("alice", protocol.SSIDGhostIOS, grant.DevModel, grant.SessionTag), addr)
	assertRejected("model", packet("alice", grant.SSID, protocol.DraARLDevModelIOS, grant.SessionTag), addr)
	otherAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: addr.Port + 1}
	assertRejected("endpoint", packet("alice", grant.SSID, grant.DevModel, grant.SessionTag), otherAddr)

	heartbeat := protocol.EncodeDraARLv1("alice", "", grant.SSID, protocol.DraARLTypeHeartbeat, grant.DevModel, 0, "", nil)
	heartbeat, _ = protocol.WithReservedUint32(heartbeat, grant.SessionTag)
	gateway.handleDevicePacket(heartbeat, addr, addr)
	if !gateway.sessions[11].LastSeen.After(initial) {
		t.Fatal("valid modern ghost packet was rejected")
	}
}

func TestEdgeRejectsSecondSessionOnAuthenticatedUDPEndpoint(t *testing.T) {
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 32010}
	first := modernEdgeGhost(7, "alice", "app-a", "11111111-1111-4111-8111-111111111111", 101, 1001, 7, []int{1001}, []uint64{7})
	first.SessionID, first.SessionEpoch = 1, 1
	second := modernEdgeGhost(8, "bob", "app-b", "22222222-2222-4222-8222-222222222222", 202, 1001, 7, []int{1001}, []uint64{7})
	second.SessionID, second.SessionEpoch = 2, 1
	var report DeviceSessionReport
	gateway := &EdgeGateway{
		sessions:   map[uint64]*edgeDeviceSession{1: {Grant: *first, Addr: cloneUDPAddr(addr), RealAddr: cloneUDPAddr(addr), LastSeen: time.Now()}},
		byIdentity: map[string]uint64{edgeSessionIdentity(*first): 1}, bySessionTag: map[uint32]uint64{first.SessionTag: 1},
		pending:         map[uint64]*pendingDeviceAuth{9: {addr: cloneUDPAddr(addr), realAddr: cloneUDPAddr(addr), identity: "pending"}},
		pendingIdentity: map[string]uint64{"pending": 9}, reportSession: func(got DeviceSessionReport) { report = got },
	}
	gateway.finishAuth(DeviceAuthResponse{RequestID: 9, Success: true, Grant: second})
	if gateway.sessions[2] != nil {
		t.Fatal("second session was installed on an authenticated UDP endpoint")
	}
	if gateway.sessions[1] == nil {
		t.Fatal("existing endpoint owner was displaced")
	}
	if report.SessionID != 2 || report.Reason != "udp_endpoint_already_authenticated" {
		t.Fatalf("center was not told to revoke rejected endpoint grant: %#v", report)
	}
}

func TestEdgeGhostRecoveryWindowExpiresOrCancelsExactSession(t *testing.T) {
	const recoveryWindow = 30 * time.Millisecond

	t.Run("expires", func(t *testing.T) {
		cluster := NewClusterManager(92)
		defer cluster.Close()
		gateway := NewCenterGateway(cluster, nil)
		gateway.SetGhostRecoveryWindow(recoveryWindow)
		revoked := make(chan string, 1)
		gateway.SetGhostSessionHandlers(nil, func(sessionID, reason string) {
			revoked <- sessionID + ":" + reason
		})
		edge := &NodeSession{NodeID: "edge-expire", SessionID: 301}
		gateway.OnConnect(edge)
		grant := modernEdgeGhost(9, "expire", "app-expire", "33333333-3333-4333-8333-333333333333", 303, 10, 90, []int{10}, []uint64{90})
		if err := gateway.activateDeviceSession(edge, grant); err != nil {
			t.Fatal(err)
		}
		gateway.OnDisconnect(edge, errors.New("test disconnect"))
		select {
		case got := <-revoked:
			want := grant.GhostSessionID + ":edge_session_recovery_expired"
			if got != want {
				t.Fatalf("recovery revoke=%q want=%q", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("disconnected edge ghost was not cleaned after the recovery window")
		}
	})

	t.Run("reconfirmed", func(t *testing.T) {
		cluster := NewClusterManager(93)
		defer cluster.Close()
		gateway := NewCenterGateway(cluster, nil)
		gateway.SetGhostRecoveryWindow(recoveryWindow)
		revoked := make(chan string, 1)
		gateway.SetGhostSessionHandlers(nil, func(sessionID, reason string) {
			revoked <- sessionID + ":" + reason
		})
		oldEdge := &NodeSession{NodeID: "edge-recover", SessionID: 401}
		gateway.OnConnect(oldEdge)
		grant := modernEdgeGhost(10, "recover", "app-recover", "44444444-4444-4444-8444-444444444444", 404, 10, 90, []int{10}, []uint64{90})
		if err := gateway.activateDeviceSession(oldEdge, grant); err != nil {
			t.Fatal(err)
		}
		gateway.OnDisconnect(oldEdge, errors.New("test disconnect"))

		newEdge := &NodeSession{NodeID: oldEdge.NodeID, SessionID: 402}
		gateway.OnConnect(newEdge)
		reconfirmed := *grant
		if err := gateway.activateDeviceSession(newEdge, &reconfirmed); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-revoked:
			t.Fatalf("reconfirmed ghost was revoked: %s", got)
		case <-time.After(3 * recoveryWindow):
		}
		if _, ok := cluster.ResolveRoute(reconfirmed.SessionID); !ok {
			t.Fatal("reconfirmed ghost route is missing")
		}
	})
}
