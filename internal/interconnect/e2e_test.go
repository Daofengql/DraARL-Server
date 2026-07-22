package interconnect

import (
	"crypto/tls"
	"net"
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
	auth := func(_ *NodeSession, req DeviceAuthRequest) (DeviceAuthResponse, error) {
		p, err := protocol.NewDraARLv1RoutingPacket(nil, req.Packet)
		if err != nil {
			return DeviceAuthResponse{RequestID: req.RequestID, Error: err.Error()}, nil
		}
		defer protocol.ReleaseDraARLv1RoutingPacket(p)
		id := nextSession.Add(1)
		call := p.Username
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
	edgeA, err := StartEdgeRuntime(EdgeRuntimeConfig{NodeID: "edge-a", Token: "token", CenterControl: center.Control.Addr().String(), CenterUDP: centerUDP.LocalAddr().String(), Listen: "127.0.0.1:0", TLSConfig: clientTLS()})
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
	login := func(conn *net.UDPConn, edge net.Addr, username string) {
		packet := protocol.EncodeDraARLv1(username, "device-password", 1, protocol.DraARLTypeHeartbeat, protocol.DraARLDevModelESP32NoRadio, uint32(len(username)), "", nil)
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
	login(devA, edgeA.Gateway.Addr(), "alice")
	login(devB, edgeB.Gateway.Addr(), "bob")
	// Allow each edge's per-node delta to be applied and ACKed before traffic.
	time.Sleep(150 * time.Millisecond)
	voice := protocol.EncodeDraARLv1("alice", "", 1, protocol.DraARLTypeOpus16K, protocol.DraARLDevModelESP32NoRadio, 1, "", []byte{1, 2, 3, 4})
	edgeAddrA, _ := net.ResolveUDPAddr("udp", edgeA.Gateway.Addr().String())
	if _, err := devA.WriteToUDP(voice, edgeAddrA); err != nil {
		t.Fatal(err)
	}
	_ = devB.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 1400)
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
