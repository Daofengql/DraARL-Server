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
