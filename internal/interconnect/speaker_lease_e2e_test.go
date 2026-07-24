package interconnect

import (
	"crypto/tls"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"draarl/internal/protocol"
)

func TestTwoEdgesSimultaneousSpeakersEmitOnlyCentreWinner(t *testing.T) {
	serverTLS, roots, err := NewSelfSignedTLSConfig("localhost")
	if err != nil {
		t.Fatal(err)
	}
	var nextSession atomic.Uint64
	auth := func(_ *NodeSession, request DeviceAuthRequest) (DeviceAuthResponse, error) {
		packet, decodeErr := protocol.NewDraARLv1RoutingPacket(nil, request.Packet)
		if decodeErr != nil {
			return DeviceAuthResponse{RequestID: request.RequestID, Error: decodeErr.Error()}, nil
		}
		defer protocol.ReleaseDraARLv1RoutingPacket(packet)
		id := nextSession.Add(1)
		grant := &DeviceGrant{SessionID: id, SessionEpoch: 1, DeviceID: int(id), OwnerID: int(id), Username: packet.Username, CallSign: packet.Username, SSID: packet.SSID, DevModel: packet.DevModel, GroupID: 1, DomainID: 99}
		return DeviceAuthResponse{RequestID: request.RequestID, Success: true, Grant: grant, ResponsePacket: protocol.EncodeHeartbeatResponse(packet, packet.Username)}, nil
	}
	center, err := StartCenterRuntime(CenterRuntimeConfig{ControlListen: "127.0.0.1:0", TLSConfig: serverTLS, ValidateToken: func(_, token string) bool { return token == "token" }, Auth: auth})
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
		buffer := make([]byte, NodeMaxDatagramSize)
		for {
			n, addr, readErr := centerUDP.ReadFromUDP(buffer)
			if readErr != nil {
				return
			}
			center.UDPBridge.Handle(append([]byte(nil), buffer[:n]...), addr)
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

	openDevice := func() *net.UDPConn {
		device, listenErr := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
		if listenErr != nil {
			t.Fatal(listenErr)
		}
		return device
	}
	alice, bob, listenerA, listenerB := openDevice(), openDevice(), openDevice(), openDevice()
	defer alice.Close()
	defer bob.Close()
	defer listenerA.Close()
	defer listenerB.Close()
	login := func(device *net.UDPConn, edge net.Addr, username string) {
		wire := protocol.EncodeDraARLv1(username, "password", 1, protocol.DraARLTypeHeartbeat, protocol.DraARLDevModelESP32NoRadio, 0, "", nil)
		addr := edge.(*net.UDPAddr)
		if _, writeErr := device.WriteToUDP(wire, addr); writeErr != nil {
			t.Fatal(writeErr)
		}
		_ = device.SetReadDeadline(time.Now().Add(3 * time.Second))
		if _, _, readErr := device.ReadFromUDP(make([]byte, 1400)); readErr != nil {
			t.Fatalf("login %s: %v", username, readErr)
		}
	}
	login(alice, edgeA.Gateway.Addr(), "alice")
	login(listenerA, edgeA.Gateway.Addr(), "listener-a")
	login(bob, edgeB.Gateway.Addr(), "bob")
	login(listenerB, edgeB.Gateway.Addr(), "listener-b")

	deadline := time.Now().Add(3 * time.Second)
	for center.Cluster.PendingControl("edge-a") != 0 || center.Cluster.PendingControl("edge-b") != 0 {
		if time.Now().After(deadline) {
			t.Fatal("route projections did not settle")
		}
		time.Sleep(10 * time.Millisecond)
	}
	voice := func(username string, marker byte) []byte {
		return protocol.EncodeDraARLv1(username, "", 1, protocol.DraARLTypeOpus16K, protocol.DraARLDevModelESP32NoRadio, 0, username, []byte{marker})
	}
	start := time.Now()
	ready := make(chan struct{})
	sent := make(chan error, 2)
	go func() {
		<-ready
		_, writeErr := alice.WriteToUDP(voice("alice", 0xa1), edgeA.Gateway.Addr().(*net.UDPAddr))
		sent <- writeErr
	}()
	go func() {
		<-ready
		_, writeErr := bob.WriteToUDP(voice("bob", 0xb1), edgeB.Gateway.Addr().(*net.UDPAddr))
		sent <- writeErr
	}()
	close(ready)
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	readMarker := func(device *net.UDPConn) byte {
		_ = device.SetReadDeadline(time.Now().Add(time.Second))
		buffer := make([]byte, 1400)
		n, _, readErr := device.ReadFromUDP(buffer)
		if readErr != nil {
			t.Fatalf("winner frame missing: %v", readErr)
		}
		packet, decodeErr := protocol.NewDraARLv1RoutingPacket(nil, buffer[:n])
		if decodeErr != nil || len(packet.DATA) != 1 {
			t.Fatalf("winner decode=%v data=%v", decodeErr, packet.DATA)
		}
		marker := packet.DATA[0]
		protocol.ReleaseDraARLv1RoutingPacket(packet)
		return marker
	}
	markerA, markerB := readMarker(listenerA), readMarker(listenerB)
	if markerA != markerB || (markerA != 0xa1 && markerA != 0xb1) {
		t.Fatalf("edges disagreed on speaker: edge-a=%x edge-b=%x", markerA, markerB)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("first speaker decision took %v", elapsed)
	}
	for _, listener := range []*net.UDPConn{listenerA, listenerB} {
		_ = listener.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
		if _, _, readErr := listener.ReadFromUDP(make([]byte, 1400)); readErr == nil {
			t.Fatal("losing speaker frame was forwarded")
		}
	}
}
