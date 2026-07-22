package interconnect

import (
	"crypto/tls"
	"encoding/binary"
	"net"
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
