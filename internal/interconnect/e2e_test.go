package interconnect

import (
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"draarl/internal/protocol"
)

func TestTwoEdgesRouteOneFrameThroughCentreAndLocalFanout(t *testing.T) {
	serverTLS, roots, err := NewSelfSignedTLSConfig("localhost")
	if err != nil {
		t.Fatal(err)
	}
	var nextSession atomic.Uint64
	var authSources sync.Map
	auth := func(_ *NodeSession, req DeviceAuthRequest) (DeviceAuthResponse, error) {
		p, err := protocol.NewDraARLv1RoutingPacket(nil, req.Packet)
		if err != nil {
			return DeviceAuthResponse{RequestID: req.RequestID, Error: err.Error()}, nil
		}
		defer protocol.ReleaseDraARLv1RoutingPacket(p)
		id := nextSession.Add(1)
		call := p.Username
		authSources.Store(p.Username, req.SourceIP)
		grant := &DeviceGrant{SessionID: id, SessionEpoch: 1, DeviceID: int(id), Username: p.Username, CallSign: call, SSID: p.SSID, DevModel: p.DevModel, DMRID: p.DMRID, GroupID: 1, DomainID: 99}
		return DeviceAuthResponse{RequestID: req.RequestID, Success: true, Grant: grant, ResponsePacket: protocol.EncodeHeartbeatResponse(p, call)}, nil
	}
	center, err := StartCenterRuntime(CenterRuntimeConfig{ControlListen: "127.0.0.1:0", TLSConfig: serverTLS, ValidateToken: func(_, token string) bool { return token == "token" }, Auth: auth})
	if err != nil {
		t.Fatal(err)
	}
	defer center.Close()
	// The test owns exactly one UDP socket, matching production where udphub
	// owns the existing device port and feeds Type 0 into the bridge.
	centerUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer centerUDP.Close()
	center.UDPBridge.SetWriter(func(addr *net.UDPAddr, wire []byte) error {
		_, err := centerUDP.WriteToUDP(wire, addr)
		return err
	})
	dataDone := make(chan struct{})
	go func() {
		defer close(dataDone)
		buf := make([]byte, NodeMaxDatagramSize)
		for {
			n, addr, readErr := centerUDP.ReadFromUDP(buf)
			if readErr != nil {
				return
			}
			center.UDPBridge.Handle(append([]byte(nil), buf[:n]...), addr)
		}
	}()
	clientTLS := func() *tls.Config {
		return &tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS13}
	}
	edgeA, err := StartEdgeRuntime(EdgeRuntimeConfig{NodeID: "edge-a", Token: "token", CenterControl: center.Control.Addr().String(), CenterUDP: centerUDP.LocalAddr().String(), Listen: "127.0.0.1:0", ProxyProtocol: "v2", TLSConfig: clientTLS()})
	if err != nil {
		t.Fatal(err)
	}
	defer edgeA.Close()
	edgeB, err := StartEdgeRuntime(EdgeRuntimeConfig{NodeID: "edge-b", Token: "token", CenterControl: center.Control.Addr().String(), CenterUDP: centerUDP.LocalAddr().String(), Listen: "127.0.0.1:0", TLSConfig: clientTLS()})
	if err != nil {
		t.Fatal(err)
	}
	defer edgeB.Close()
	devA, err := net.ListenUDP("udp", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer devA.Close()
	devB, err := net.ListenUDP("udp", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer devB.Close()
	login := func(conn *net.UDPConn, edge net.Addr, username string, proxySource *net.UDPAddr) {
		packet := protocol.EncodeDraARLv1(username, "device-password", 1, protocol.DraARLTypeHeartbeat, protocol.DraARLDevModelESP32NoRadio, uint32(len(username)), "", nil)
		if proxySource != nil {
			packet = wrapProxyV2IPv4(t, proxySource, packet)
		}
		edgeAddr, _ := net.ResolveUDPAddr("udp", edge.String())
		if _, err := conn.WriteToUDP(packet, edgeAddr); err != nil {
			t.Fatal(err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		buf := make([]byte, 1400)
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := protocol.NewDraARLv1RoutingPacket(nil, buf[:n])
		if err != nil || decoded.Type != protocol.DraARLTypeHeartbeat {
			t.Fatalf("login response decode: %v", err)
		}
		protocol.ReleaseDraARLv1RoutingPacket(decoded)
	}
	proxySourceA := &net.UDPAddr{IP: net.IPv4(198, 51, 100, 27), Port: 23456}
	login(devA, edgeA.Gateway.Addr(), "alice", proxySourceA)
	login(devB, edgeB.Gateway.Addr(), "bob", nil)
	if source, ok := authSources.Load("alice"); !ok || source != proxySourceA.IP.String() {
		t.Fatalf("proxied device auth source=%v want=%s", source, proxySourceA.IP)
	}
	// Wait for an application-level ACK from each edge; TCP delivery alone is
	// not enough to prove the projection was atomically applied.
	deadline := time.Now().Add(3 * time.Second)
	for {
		versionA, _ := center.Cluster.RouteAck("edge-a")
		versionB, _ := center.Cluster.RouteAck("edge-b")
		if versionA >= 1 && versionB >= 1 && center.Cluster.PendingControl("edge-a") == 0 && center.Cluster.PendingControl("edge-b") == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("route ACK timeout: edge-a=%d pending=%d edge-b=%d pending=%d", versionA, center.Cluster.PendingControl("edge-a"), versionB, center.Cluster.PendingControl("edge-b"))
		}
		time.Sleep(10 * time.Millisecond)
	}
	spoofedVoice := protocol.EncodeDraARLv1("alice", "", 1, protocol.DraARLTypeOpus16K, protocol.DraARLDevModelESP32NoRadio, 1, "", []byte{9, 9, 9})
	spoofedVoice = wrapProxyV2IPv4(t, &net.UDPAddr{IP: net.IPv4(203, 0, 113, 99), Port: 45678}, spoofedVoice)
	edgeAddrA, _ := net.ResolveUDPAddr("udp", edgeA.Gateway.Addr().String())
	if _, err := devA.WriteToUDP(spoofedVoice, edgeAddrA); err != nil {
		t.Fatal(err)
	}
	_ = devB.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	buf := make([]byte, 1400)
	if _, _, err := devB.ReadFromUDP(buf); err == nil {
		t.Fatal("a different PROXY source endpoint hijacked an authenticated device identity")
	}
	// Subsequent compact device packets may omit Username. The proxy-advertised
	// client address, rather than the FRP transport address, must still find
	// the authenticated session.
	voice := protocol.EncodeDraARLv1("", "", 1, protocol.DraARLTypeOpus16K, protocol.DraARLDevModelESP32NoRadio, 1, "", []byte{1, 2, 3, 4})
	voice = wrapProxyV2IPv4(t, proxySourceA, voice)
	if _, err := devA.WriteToUDP(voice, edgeAddrA); err != nil {
		t.Fatal(err)
	}
	_ = devB.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, _, err := devB.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("remote edge did not receive voice: %v", err)
	}
	got, err := protocol.NewDraARLv1RoutingPacket(nil, buf[:n])
	if err != nil {
		t.Fatal(err)
	}
	defer protocol.ReleaseDraARLv1RoutingPacket(got)
	if got.Type != protocol.DraARLTypeOpus16K || string(got.DATA) != string([]byte{1, 2, 3, 4}) {
		t.Fatalf("unexpected downstream packet type=%d data=%v", got.Type, got.DATA)
	}
}

func TestCenterSendsOneDownstreamPerReceivingEdge(t *testing.T) {
	serverTLS, roots, err := NewSelfSignedTLSConfig("localhost")
	if err != nil {
		t.Fatal(err)
	}
	var nextSession atomic.Uint64
	auth := func(_ *NodeSession, req DeviceAuthRequest) (DeviceAuthResponse, error) {
		packet, decodeErr := protocol.NewDraARLv1RoutingPacket(nil, req.Packet)
		if decodeErr != nil {
			return DeviceAuthResponse{RequestID: req.RequestID, Error: decodeErr.Error()}, nil
		}
		defer protocol.ReleaseDraARLv1RoutingPacket(packet)
		domainID := uint64(99)
		if packet.Username == "outside" {
			domainID = 100
		}
		id := nextSession.Add(1)
		grant := &DeviceGrant{
			SessionID: id, SessionEpoch: 1, DeviceID: int(id), Username: packet.Username,
			CallSign: packet.Username, SSID: packet.SSID, DevModel: packet.DevModel,
			DMRID: packet.DMRID, GroupID: int(domainID), DomainID: domainID,
		}
		return DeviceAuthResponse{
			RequestID: req.RequestID, Success: true, Grant: grant,
			ResponsePacket: protocol.EncodeHeartbeatResponse(packet, grant.CallSign),
		}, nil
	}
	center, err := StartCenterRuntime(CenterRuntimeConfig{
		ControlListen: "127.0.0.1:0", TLSConfig: serverTLS,
		ValidateToken: func(_, token string) bool { return token == "token" }, Auth: auth,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer center.Close()
	centerUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer centerUDP.Close()

	var captureMu sync.Mutex
	captureDownstream := false
	downstreamByAddr := make(map[string]int)
	center.UDPBridge.SetWriter(func(addr *net.UDPAddr, wire []byte) error {
		if len(wire) > DraARLHeaderSize+1 && wire[DraARLHeaderSize+1] == SubtypeRelayDownstream {
			captureMu.Lock()
			if captureDownstream {
				downstreamByAddr[addr.String()]++
			}
			captureMu.Unlock()
		}
		_, writeErr := centerUDP.WriteToUDP(wire, addr)
		return writeErr
	})
	go func() {
		buf := make([]byte, NodeMaxDatagramSize)
		for {
			n, addr, readErr := centerUDP.ReadFromUDP(buf)
			if readErr != nil {
				return
			}
			center.UDPBridge.Handle(append([]byte(nil), buf[:n]...), addr)
		}
	}()

	clientTLS := func() *tls.Config {
		return &tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS13}
	}
	edges := make(map[string]*EdgeRuntime)
	for _, nodeID := range []string{"edge-a", "edge-b", "edge-c", "edge-d", "edge-empty"} {
		edge, startErr := StartEdgeRuntime(EdgeRuntimeConfig{
			NodeID: nodeID, Token: "token", CenterControl: center.Control.Addr().String(),
			CenterUDP: centerUDP.LocalAddr().String(), Listen: "127.0.0.1:0", TLSConfig: clientTLS(),
		})
		if startErr != nil {
			t.Fatal(startErr)
		}
		edges[nodeID] = edge
		defer edge.Close()
	}

	devices := make(map[string]*net.UDPConn)
	login := func(nodeID, username string) {
		t.Helper()
		device, listenErr := net.ListenUDP("udp", nil)
		if listenErr != nil {
			t.Fatal(listenErr)
		}
		devices[username] = device
		packet := protocol.EncodeDraARLv1(username, "device-password", 1, protocol.DraARLTypeHeartbeat, protocol.DraARLDevModelESP32NoRadio, uint32(len(username)), "", nil)
		edgeAddr, resolveErr := net.ResolveUDPAddr("udp", edges[nodeID].Gateway.Addr().String())
		if resolveErr != nil {
			t.Fatal(resolveErr)
		}
		if _, writeErr := device.WriteToUDP(packet, edgeAddr); writeErr != nil {
			t.Fatal(writeErr)
		}
		_ = device.SetReadDeadline(time.Now().Add(3 * time.Second))
		buf := make([]byte, 1400)
		n, _, readErr := device.ReadFromUDP(buf)
		if readErr != nil {
			t.Fatal(readErr)
		}
		response, decodeErr := protocol.NewDraARLv1RoutingPacket(nil, buf[:n])
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if response.Type != protocol.DraARLTypeHeartbeat {
			protocol.ReleaseDraARLv1RoutingPacket(response)
			t.Fatalf("login response type=%d want=%d", response.Type, protocol.DraARLTypeHeartbeat)
		}
		protocol.ReleaseDraARLv1RoutingPacket(response)
	}
	login("edge-a", "alice")
	login("edge-b", "bob-1")
	login("edge-b", "bob-2")
	login("edge-c", "carol")
	login("edge-d", "dave")
	login("edge-empty", "outside")
	for _, device := range devices {
		defer device.Close()
	}

	expectedVersions := map[string]uint64{"edge-a": 1, "edge-b": 2, "edge-c": 1, "edge-d": 1, "edge-empty": 1}
	deadline := time.Now().Add(3 * time.Second)
	for {
		ready := true
		for nodeID, expectedVersion := range expectedVersions {
			version, _ := center.Cluster.RouteAck(nodeID)
			session, online := center.Control.Session(nodeID)
			if version < expectedVersion || center.Cluster.PendingControl(nodeID) != 0 || !online || session.DataAddr() == nil {
				ready = false
				break
			}
		}
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("edge route ACK or UDP binding did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}

	nodeDataAddr := make(map[string]string, len(edges))
	for nodeID := range edges {
		session, _ := center.Control.Session(nodeID)
		nodeDataAddr[nodeID] = session.DataAddr().String()
	}
	captureMu.Lock()
	captureDownstream = true
	captureMu.Unlock()

	payload := []byte("one downstream per receiving edge")
	message := protocol.EncodeDraARLv1("", "", 1, protocol.DraARLTypeTextMessage, protocol.DraARLDevModelESP32NoRadio, 1, "", payload)
	sourceAddr, err := net.ResolveUDPAddr("udp", edges["edge-a"].Gateway.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = devices["alice"].WriteToUDP(message, sourceAddr); err != nil {
		t.Fatal(err)
	}

	readOnce := func(username string) {
		t.Helper()
		device := devices[username]
		_ = device.SetReadDeadline(time.Now().Add(3 * time.Second))
		buf := make([]byte, 1400)
		n, _, readErr := device.ReadFromUDP(buf)
		if readErr != nil {
			t.Fatalf("%s did not receive relayed text: %v", username, readErr)
		}
		packet, decodeErr := protocol.NewDraARLv1RoutingPacket(nil, buf[:n])
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if packet.Type != protocol.DraARLTypeTextMessage || string(packet.DATA) != string(payload) {
			protocol.ReleaseDraARLv1RoutingPacket(packet)
			t.Fatalf("%s received type=%d data=%q", username, packet.Type, packet.DATA)
		}
		protocol.ReleaseDraARLv1RoutingPacket(packet)
		_ = device.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
		if _, _, duplicateErr := device.ReadFromUDP(buf); duplicateErr == nil {
			t.Fatalf("%s received the same relayed text more than once", username)
		}
	}
	for _, username := range []string{"bob-1", "bob-2", "carol", "dave"} {
		readOnce(username)
	}
	for _, username := range []string{"alice", "outside"} {
		_ = devices[username].SetReadDeadline(time.Now().Add(150 * time.Millisecond))
		if _, _, readErr := devices[username].ReadFromUDP(make([]byte, 1400)); readErr == nil {
			t.Fatalf("%s received a frame outside the intended remote domain", username)
		}
	}

	captureMu.Lock()
	defer captureMu.Unlock()
	for _, nodeID := range []string{"edge-b", "edge-c", "edge-d"} {
		if got := downstreamByAddr[nodeDataAddr[nodeID]]; got != 1 {
			t.Errorf("%s downstream datagrams=%d want=1", nodeID, got)
		}
	}
	for _, nodeID := range []string{"edge-a", "edge-empty"} {
		if got := downstreamByAddr[nodeDataAddr[nodeID]]; got != 0 {
			t.Errorf("%s downstream datagrams=%d want=0", nodeID, got)
		}
	}
	if got := len(downstreamByAddr); got != 3 {
		t.Errorf("downstream target addresses=%d want=3: %#v", got, downstreamByAddr)
	}
}

func TestModernGhostMultiSessionAcrossEdgesRoutingMigrationAndRecovery(t *testing.T) {
	serverTLS, roots, err := NewSelfSignedTLSConfig("localhost")
	if err != nil {
		t.Fatal(err)
	}
	const (
		groupA  = 101
		groupB  = 202
		domainA = uint64(9001)
		domainB = uint64(9002)
	)
	type ghostFixture struct {
		instanceID string
		sessionID  string
		sessionTag uint32
		groupID    int
		domainID   uint64
	}
	fixtures := map[string]ghostFixture{
		"ghost-token-a": {
			instanceID: "11111111-1111-4111-8111-111111111111",
			sessionID:  "ghost-session-a", sessionTag: 0x10203041,
			groupID: groupA, domainID: domainA,
		},
		"ghost-token-b": {
			instanceID: "22222222-2222-4222-8222-222222222222",
			sessionID:  "ghost-session-b", sessionTag: 0x10203042,
			groupID: groupB, domainID: domainB,
		},
	}
	grantFor := func(fixture ghostFixture) DeviceGrant {
		return DeviceGrant{
			OwnerID: 77, Username: "alice", CallSign: "BG5GHOST",
			SSID: protocol.SSIDGhostAndroid, DevModel: protocol.DraARLDevModelAndroid,
			GroupID: fixture.groupID, DomainID: fixture.domainID,
			RxGroupIDs: []int{groupA, groupB}, RxDomainIDs: []uint64{domainA, domainB},
			GhostSessionID: fixture.sessionID, ClientInstanceID: fixture.instanceID,
			SessionTag: fixture.sessionTag, GhostProtocolVersion: protocol.GhostAuthPayloadVersion,
			SourceGroupV1: true, RecoveryTicket: "test-recovery-ticket",
			ExpiresAtMillis: time.Now().Add(2 * time.Minute).UnixMilli(),
		}
	}
	auth := func(_ *NodeSession, req DeviceAuthRequest) (DeviceAuthResponse, error) {
		packet, decodeErr := protocol.NewDraARLv1RoutingPacket(nil, req.Packet)
		if decodeErr != nil {
			return DeviceAuthResponse{RequestID: req.RequestID, Error: "invalid_packet"}, nil
		}
		defer protocol.ReleaseDraARLv1RoutingPacket(packet)
		request, decodeErr := protocol.DecodeGhostAuthRequest(packet.DATA)
		fixture, ok := fixtures[request.Token]
		if decodeErr != nil || !ok || request.ClientInstanceID != fixture.instanceID ||
			!slices.Contains(request.Capabilities, "multi_receive_v1") ||
			!slices.Contains(request.Capabilities, "source_group_v1") {
			return DeviceAuthResponse{RequestID: req.RequestID, Error: "invalid_ghost_auth"}, nil
		}
		grant := grantFor(fixture)
		responseData, encodeErr := protocol.EncodeGhostAuthSuccessData(protocol.GhostAuthSuccess{
			SessionID: fixture.sessionID, SessionTag: fixture.sessionTag,
			ClientInstanceID: fixture.instanceID, TxGroupID: fixture.groupID,
			RxGroupIDs: []int{groupA, groupB},
		})
		if encodeErr != nil {
			return DeviceAuthResponse{}, encodeErr
		}
		responsePacket := protocol.EncodeDraARLv1(
			packet.Username, "", packet.SSID, protocol.DraARLTypeJWTAuth,
			packet.DevModel, 0, grant.CallSign, responseData,
		)
		responsePacket, _ = protocol.WithReservedUint32(responsePacket, fixture.sessionTag)
		return DeviceAuthResponse{
			RequestID: req.RequestID, Success: true, Grant: &grant, ResponsePacket: responsePacket,
		}, nil
	}
	confirm := func(_ *NodeSession, items []DeviceSessionConfirmItem) ([]DeviceSessionConfirmResult, error) {
		results := make([]DeviceSessionConfirmResult, 0, len(items))
		for _, item := range items {
			var fixture ghostFixture
			for _, candidate := range fixtures {
				if candidate.sessionID == item.GhostSessionID && candidate.instanceID == item.ClientInstanceID {
					fixture = candidate
					break
				}
			}
			if fixture.sessionID == "" {
				results = append(results, DeviceSessionConfirmResult{
					SessionID: item.SessionID, SessionEpoch: item.SessionEpoch, Error: "unknown_session",
				})
				continue
			}
			grant := grantFor(fixture)
			results = append(results, DeviceSessionConfirmResult{
				SessionID: item.SessionID, SessionEpoch: item.SessionEpoch, Success: true, Grant: &grant,
			})
		}
		return results, nil
	}
	center, err := StartCenterRuntime(CenterRuntimeConfig{
		ControlListen: "127.0.0.1:0", TLSConfig: serverTLS,
		ValidateToken: func(_, token string) bool { return token == "node-token" },
		Auth:          auth, Confirm: confirm,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer center.Close()
	centerUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer centerUDP.Close()
	center.UDPBridge.SetWriter(func(addr *net.UDPAddr, wire []byte) error {
		_, writeErr := centerUDP.WriteToUDP(wire, addr)
		return writeErr
	})
	go func() {
		buf := make([]byte, NodeMaxDatagramSize)
		for {
			n, addr, readErr := centerUDP.ReadFromUDP(buf)
			if readErr != nil {
				return
			}
			center.UDPBridge.Handle(append([]byte(nil), buf[:n]...), addr)
		}
	}()

	clientTLS := func() *tls.Config {
		return &tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS13}
	}
	edges := make(map[string]*EdgeRuntime)
	for _, nodeID := range []string{"edge-a", "edge-b", "edge-c"} {
		edge, startErr := StartEdgeRuntime(EdgeRuntimeConfig{
			NodeID: nodeID, Token: "node-token", CenterControl: center.Control.Addr().String(),
			CenterUDP: centerUDP.LocalAddr().String(), Listen: "127.0.0.1:0", TLSConfig: clientTLS(),
			ReconnectMin: 20 * time.Millisecond, ReconnectMax: 100 * time.Millisecond,
		})
		if startErr != nil {
			t.Fatal(startErr)
		}
		edges[nodeID] = edge
		defer edge.Close()
	}
	devices := make(map[string]*net.UDPConn)
	for token := range fixtures {
		device, listenErr := net.ListenUDP("udp", nil)
		if listenErr != nil {
			t.Fatal(listenErr)
		}
		devices[token] = device
		defer device.Close()
	}
	login := func(token, nodeID string) {
		t.Helper()
		fixture := fixtures[token]
		requestData, marshalErr := json.Marshal(protocol.GhostAuthRequest{
			Version: protocol.GhostAuthPayloadVersion, Token: token,
			ClientInstanceID: fixture.instanceID,
			Capabilities:     []string{"multi_receive_v1", "source_group_v1"},
		})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		wire := protocol.EncodeDraARLv1(
			"alice", "", protocol.SSIDGhostAndroid, protocol.DraARLTypeJWTAuth,
			protocol.DraARLDevModelAndroid, 0, "", requestData,
		)
		edgeAddr, resolveErr := net.ResolveUDPAddr("udp", edges[nodeID].Gateway.Addr().String())
		if resolveErr != nil {
			t.Fatal(resolveErr)
		}
		if _, writeErr := devices[token].WriteToUDP(wire, edgeAddr); writeErr != nil {
			t.Fatal(writeErr)
		}
		_ = devices[token].SetReadDeadline(time.Now().Add(3 * time.Second))
		buf := make([]byte, 1400)
		n, _, readErr := devices[token].ReadFromUDP(buf)
		if readErr != nil {
			t.Fatalf("%s authentication response: %v", token, readErr)
		}
		response, decodeErr := protocol.NewDraARLv1RoutingPacket(nil, buf[:n])
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		defer protocol.ReleaseDraARLv1RoutingPacket(response)
		success, decodeErr := protocol.DecodeGhostAuthSuccessData(response.DATA)
		if decodeErr != nil || success.SessionID != fixture.sessionID ||
			success.SessionTag != fixture.sessionTag || protocol.ReservedUint32(response.Reserved) != fixture.sessionTag {
			t.Fatalf("%s invalid authentication response: success=%#v err=%v", token, success, decodeErr)
		}
	}
	waitRoute := func(sessionID, nodeID string, rxDomains []uint64) DeviceRoute {
		t.Helper()
		var route DeviceRoute
		waitForCondition(t, 3*time.Second, func() bool {
			center.Gateway.mu.RLock()
			wireSessionID := center.Gateway.activeByGhost[sessionID]
			owner := center.Gateway.deviceSessions[wireSessionID]
			center.Gateway.mu.RUnlock()
			var ok bool
			route, ok = center.Cluster.ResolveRoute(wireSessionID)
			if !ok || owner.NodeID != nodeID || !slices.Equal(route.RxDomainIDs, rxDomains) {
				return false
			}
			edge := edges[nodeID]
			edge.Gateway.mu.RLock()
			local := edge.Gateway.sessions[wireSessionID]
			var localGrant DeviceGrant
			if local != nil {
				localGrant = local.Grant
			}
			edge.Gateway.mu.RUnlock()
			return local != nil && localGrant.SessionEpoch == route.SessionEpoch &&
				slices.Equal(localGrant.RxDomainIDs, rxDomains)
		}, "modern ghost route did not reach the target edge")
		return route
	}
	readText := func(token string, timeout time.Duration, wantPayload string, wantGroup int) {
		t.Helper()
		device := devices[token]
		_ = device.SetReadDeadline(time.Now().Add(timeout))
		buf := make([]byte, 1400)
		n, _, readErr := device.ReadFromUDP(buf)
		if readErr != nil {
			t.Fatalf("%s did not receive %q: %v", token, wantPayload, readErr)
		}
		packet, decodeErr := protocol.NewDraARLv1RoutingPacket(nil, buf[:n])
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		defer protocol.ReleaseDraARLv1RoutingPacket(packet)
		if packet.Type != protocol.DraARLTypeTextMessage || string(packet.DATA) != wantPayload ||
			int(protocol.ReservedUint32(packet.Reserved)) != wantGroup {
			t.Fatalf("%s received type=%d group=%d data=%q", token, packet.Type, protocol.ReservedUint32(packet.Reserved), packet.DATA)
		}
	}
	assertNoPacket := func(token string) {
		t.Helper()
		_ = devices[token].SetReadDeadline(time.Now().Add(180 * time.Millisecond))
		if _, _, readErr := devices[token].ReadFromUDP(make([]byte, 1400)); readErr == nil {
			t.Fatalf("%s received an unexpected or duplicate packet", token)
		}
	}
	sendText := func(token, nodeID, payload string) {
		t.Helper()
		fixture := fixtures[token]
		wire := protocol.EncodeDraARLv1(
			"alice", "", protocol.SSIDGhostAndroid, protocol.DraARLTypeTextMessage,
			protocol.DraARLDevModelAndroid, 0, "", []byte(payload),
		)
		wire, _ = protocol.WithReservedUint32(wire, fixture.sessionTag)
		edgeAddr, resolveErr := net.ResolveUDPAddr("udp", edges[nodeID].Gateway.Addr().String())
		if resolveErr != nil {
			t.Fatal(resolveErr)
		}
		if _, writeErr := devices[token].WriteToUDP(wire, edgeAddr); writeErr != nil {
			t.Fatal(writeErr)
		}
	}

	login("ghost-token-a", "edge-a")
	login("ghost-token-b", "edge-b")
	waitRoute("ghost-session-a", "edge-a", []uint64{domainA, domainB})
	waitRoute("ghost-session-b", "edge-b", []uint64{domainA, domainB})

	sendText("ghost-token-a", "edge-a", "from-a-group-a")
	readText("ghost-token-b", 3*time.Second, "from-a-group-a", groupA)
	assertNoPacket("ghost-token-a")
	assertNoPacket("ghost-token-b")

	sendText("ghost-token-b", "edge-b", "from-b-group-b")
	readText("ghost-token-a", 3*time.Second, "from-b-group-b", groupB)
	assertNoPacket("ghost-token-b")

	updated, updateErr := center.Gateway.UpdateActiveGhostRoute("ghost-session-b", groupB, []int{groupB}, func(groupID int) uint64 {
		if groupID == groupA {
			return domainA
		}
		if groupID == groupB {
			return domainB
		}
		return 0
	})
	if updateErr != nil || !updated {
		t.Fatalf("remove receive subscription: updated=%v err=%v", updated, updateErr)
	}
	waitRoute("ghost-session-b", "edge-b", []uint64{domainB})
	sendText("ghost-token-a", "edge-a", "not-subscribed")
	assertNoPacket("ghost-token-b")

	updated, updateErr = center.Gateway.UpdateActiveGhostRoute("ghost-session-b", groupB, []int{groupA, groupB}, func(groupID int) uint64 {
		if groupID == groupA {
			return domainA
		}
		if groupID == groupB {
			return domainB
		}
		return 0
	})
	if updateErr != nil || !updated {
		t.Fatalf("restore receive subscription: updated=%v err=%v", updated, updateErr)
	}
	waitRoute("ghost-session-b", "edge-b", []uint64{domainA, domainB})

	login("ghost-token-b", "edge-c")
	waitRoute("ghost-session-b", "edge-c", []uint64{domainA, domainB})
	waitForCondition(t, 3*time.Second, func() bool {
		return edges["edge-b"].Gateway.ConnectionCount() == 0
	}, "migrated ghost session remained active on the old edge")
	waitRoute("ghost-session-a", "edge-a", []uint64{domainA, domainB})
	sendText("ghost-token-a", "edge-a", "after-node-migration")
	readText("ghost-token-b", 3*time.Second, "after-node-migration", groupA)

	oldControl := edges["edge-c"].CurrentClient()
	if oldControl == nil || !center.Control.Disconnect("edge-c") {
		t.Fatal("could not force the edge-c control reconnect")
	}
	waitForCondition(t, 3*time.Second, func() bool {
		client := edges["edge-c"].CurrentClient()
		return client != nil && client != oldControl && edges["edge-c"].Gateway.currentControl(true) != nil
	}, "edge-c control session did not reconnect")
	waitRoute("ghost-session-b", "edge-c", []uint64{domainA, domainB})
	sendText("ghost-token-a", "edge-a", "after-control-recovery")
	readText("ghost-token-b", 3*time.Second, "after-control-recovery", groupA)
	assertNoPacket("ghost-token-b")
}

func TestEdgeDeviceConfigurationUsesReliableExactSessionControl(t *testing.T) {
	serverTLS, roots, err := NewSelfSignedTLSConfig("localhost")
	if err != nil {
		t.Fatal(err)
	}
	const (
		deviceID = 77
		username = "alice"
		callsign = "BG5CFG"
	)
	var reportCount atomic.Int64
	reports := make(chan []byte, 4)
	configPacket := func(data []byte) []byte {
		return protocol.EncodeDraARLv1(username, "", 1, protocol.DraARLTypeConfig, 0, 0, callsign, data)
	}
	auth := func(_ *NodeSession, req DeviceAuthRequest) (DeviceAuthResponse, error) {
		packet, decodeErr := protocol.NewDraARLv1RoutingPacket(nil, req.Packet)
		if decodeErr != nil {
			return DeviceAuthResponse{RequestID: req.RequestID, Error: "invalid"}, nil
		}
		defer protocol.ReleaseDraARLv1RoutingPacket(packet)
		grant := &DeviceGrant{DeviceID: deviceID, OwnerID: 7, Username: username, CallSign: callsign, SSID: 1, DevModel: protocol.DraARLDevModelESP32NoRadio, GroupID: 1, DomainID: 99}
		return DeviceAuthResponse{RequestID: req.RequestID, Success: true, Grant: grant, ResponsePacket: protocol.EncodeHeartbeatResponse(packet, callsign)}, nil
	}
	config := func(gotDeviceID int, kind string, data []byte) ([][]byte, error) {
		if gotDeviceID != deviceID {
			t.Fatalf("config device ID = %d, want %d", gotDeviceID, deviceID)
		}
		switch kind {
		case DeviceConfigKindSync:
			return [][]byte{configPacket([]byte{1})}, nil
		case DeviceConfigKindReport:
			reportCount.Add(1)
			reports <- append([]byte(nil), data...)
			return [][]byte{configPacket([]byte{3, 0, 0, 0, 0, 0, 0, 0, 0, 1})}, nil
		default:
			t.Fatalf("unexpected config kind %q", kind)
			return nil, nil
		}
	}
	center, err := StartCenterRuntime(CenterRuntimeConfig{
		ControlListen: "127.0.0.1:0", TLSConfig: serverTLS,
		ValidateToken: func(_, token string) bool { return token == "token" }, Auth: auth, Config: config,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer center.Close()
	clientTLS := &tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS13}
	edge, err := StartEdgeRuntime(EdgeRuntimeConfig{NodeID: "edge-config", Token: "token", CenterControl: center.Control.Addr().String(), Listen: "127.0.0.1:0", TLSConfig: clientTLS})
	if err != nil {
		t.Fatal(err)
	}
	defer edge.Close()
	device, err := net.ListenUDP("udp", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer device.Close()
	edgeAddr, _ := net.ResolveUDPAddr("udp", edge.Gateway.Addr().String())
	heartbeat := protocol.EncodeDraARLv1(username, "device-password", 1, protocol.DraARLTypeHeartbeat, protocol.DraARLDevModelESP32NoRadio, 0, "", nil)
	if _, err := device.WriteToUDP(heartbeat, edgeAddr); err != nil {
		t.Fatal(err)
	}
	readPacket := func(timeout time.Duration) *protocol.DraARLv1Packet {
		t.Helper()
		_ = device.SetReadDeadline(time.Now().Add(timeout))
		buf := make([]byte, 1400)
		n, _, readErr := device.ReadFromUDP(buf)
		if readErr != nil {
			t.Fatal(readErr)
		}
		packet, decodeErr := protocol.NewDraARLv1Packet(nil, buf[:n])
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		return packet
	}
	if packet := readPacket(3 * time.Second); packet.Type != protocol.DraARLTypeHeartbeat {
		t.Fatalf("first response type = %d, want heartbeat", packet.Type)
	}
	if packet := readPacket(3 * time.Second); packet.Type != protocol.DraARLTypeConfig || string(packet.DATA) != string([]byte{1}) {
		t.Fatalf("initial config = type %d data %v", packet.Type, packet.DATA)
	}

	direct := configPacket([]byte{2, 0})
	if handled, err := center.Gateway.SendDeviceConfig(deviceID, direct, 2*time.Second); !handled || err != nil {
		t.Fatalf("direct config delivery: handled=%v err=%v", handled, err)
	}
	if packet := readPacket(time.Second); string(packet.DATA) != string([]byte{2, 0}) {
		t.Fatalf("direct config DATA = %v", packet.DATA)
	}

	reportData := []byte{2, 0}
	reportPacket := protocol.EncodeDraARLv1(username, "", 1, protocol.DraARLTypeConfig, protocol.DraARLDevModelESP32NoRadio, 0, "", reportData)
	if _, err := device.WriteToUDP(reportPacket, edgeAddr); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-reports:
		if string(got) != string(reportData) {
			t.Fatalf("reported DATA = %v, want %v", got, reportData)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("center did not process edge config report")
	}
	if packet := readPacket(3 * time.Second); packet.DATA[0] != 3 {
		t.Fatalf("report ACK DATA = %v", packet.DATA)
	}

	center.Gateway.mu.RLock()
	owner := center.Gateway.deviceSessions[center.Gateway.activeByID[deviceID]]
	center.Gateway.mu.RUnlock()
	duplicatePacket := configPacket([]byte{1, 9})
	duplicate := DeviceConfigControl{Kind: DeviceConfigKindDown, SessionID: owner.SessionID, SessionEpoch: owner.SessionEpoch, DeviceID: deviceID, Packet: duplicatePacket}
	payload, _ := EncodeJSON(duplicate)
	env := NewEnvelope(SubtypeDeviceConfig, "center", 0, center.Cluster.NextMessageID(), payload)
	env.ClusterEpoch, env.Flags = center.Cluster.Epoch(), FlagControl|FlagAck
	if err := center.Control.SendEnvelope(owner.NodeID, env); err != nil {
		t.Fatal(err)
	}
	if packet := readPacket(time.Second); string(packet.DATA) != string([]byte{1, 9}) {
		t.Fatalf("duplicate test DATA = %v", packet.DATA)
	}
	if err := center.Control.SendEnvelope(owner.NodeID, env); err != nil {
		t.Fatal(err)
	}
	_ = device.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, _, err := device.ReadFromUDP(make([]byte, 1400)); err == nil {
		t.Fatal("duplicate DeviceConfigDown was written to the device twice")
	}

	wrongIdentity := protocol.EncodeDraARLv1("mallory", "", 1, protocol.DraARLTypeConfig, 0, 0, callsign, []byte{1})
	if handled, err := center.Gateway.SendDeviceConfig(deviceID, wrongIdentity, time.Second); !handled || err == nil {
		t.Fatalf("identity-mismatched config: handled=%v err=%v", handled, err)
	}
	before := reportCount.Load()
	stale := DeviceConfigControl{Kind: DeviceConfigKindReport, SessionID: owner.SessionID, SessionEpoch: owner.SessionEpoch + 1, DeviceID: deviceID, Data: []byte{2, 0}}
	stalePayload, _ := EncodeJSON(stale)
	staleEnv := NewEnvelope(SubtypeDeviceConfig, owner.NodeID, owner.ControlSessionID, edge.Gateway.nextRequest.Add(1), stalePayload)
	staleEnv.Flags = FlagControl | FlagAck
	if err := edge.CurrentClient().SendEnvelope(staleEnv); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if reportCount.Load() != before {
		t.Fatal("center accepted a stale device config session epoch")
	}
}

func wrapProxyV2IPv4(t *testing.T, source *net.UDPAddr, payload []byte) []byte {
	t.Helper()
	sourceIP := source.IP.To4()
	if sourceIP == nil {
		t.Fatal("PROXY v2 test source must be IPv4")
	}
	const addressLength = 12
	wire := make([]byte, 16+addressLength+len(payload))
	copy(wire[:12], []byte{0x0d, 0x0a, 0x0d, 0x0a, 0x00, 0x0d, 0x0a, 0x51, 0x55, 0x49, 0x54, 0x0a})
	wire[12] = 0x21 // version 2, PROXY command
	wire[13] = 0x12 // AF_INET, datagram
	binary.BigEndian.PutUint16(wire[14:16], addressLength)
	copy(wire[16:20], sourceIP)
	copy(wire[20:24], net.IPv4(127, 0, 0, 1).To4())
	binary.BigEndian.PutUint16(wire[24:26], uint16(source.Port))
	binary.BigEndian.PutUint16(wire[26:28], 60050)
	copy(wire[28:], payload)
	return wire
}
