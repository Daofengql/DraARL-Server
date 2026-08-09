package udphub

import (
	"net"
	"testing"
	"time"

	"draarl/internal/models"
	"draarl/internal/protocol"
)

func TestDraARLPacketTypeDispatch(t *testing.T) {
	tests := []struct {
		name string
		run  func(*testing.T)
	}{
		{name: "authentication", run: testMalformedAuthDispatch},
		{name: "heartbeat", run: testHeartbeatDispatch},
		{name: "voice", run: func(t *testing.T) {
			parseDraARL(testDispatchPacket(protocol.DraARLTypeOpus16K), nil, &models.Device{}, nil, nil, nil, false)
		}},
		{name: "text", run: func(t *testing.T) {
			parseDraARL(testDispatchPacket(protocol.DraARLTypeTextMessage), nil, &models.Device{}, nil, nil, nil, false)
		}},
		{name: "config", run: func(t *testing.T) {
			packet := testDispatchPacket(protocol.DraARLTypeConfig)
			packet.DATA = []byte{ConfigTypeQuery}
			parseDraARL(packet, nil, &models.Device{CallSign: "TEST", SSID: 1}, nil, nil, nil, false)
		}},
		{name: "unknown", run: func(t *testing.T) {
			parseDraARL(testDispatchPacket(0xff), nil, &models.Device{}, nil, nil, nil, false)
		}},
		{name: "malformed", run: func(t *testing.T) {
			processDraARLPacket([]byte{1, 2, 3}, nil, nil, nil)
		}},
	}

	for _, test := range tests {
		t.Run(test.name, test.run)
	}
}

func testMalformedAuthDispatch(t *testing.T) {
	server, client, clientAddr := testDispatchSockets(t)
	wire := protocol.EncodeDraARLv1(
		"ghost", "", protocol.SSIDGhostWeb, protocol.DraARLTypeJWTAuth,
		protocol.DraARLDevModelBrowser, 0, "", nil,
	)
	processDraARLPacket(wire, clientAddr, clientAddr, server)
	assertDispatchResponseType(t, client, protocol.DraARLTypeJWTAuth)
}

func testHeartbeatDispatch(t *testing.T) {
	server, client, clientAddr := testDispatchSockets(t)
	now := time.Now()
	packet := testDispatchPacket(protocol.DraARLTypeHeartbeat)
	packet.UDPAddr = clientAddr
	packet.TimeStamp = now
	device := &models.Device{
		Username:         "physical",
		CallSign:         "BG0TEST",
		SSID:             1,
		ISOnline:         true,
		Loged:            true,
		LastVoiceEndTime: now,
	}
	parseDraARL(packet, nil, device, server, nil, nil, false)
	assertDispatchResponseType(t, client, protocol.DraARLTypeHeartbeat)
}

func testDispatchPacket(packetType byte) *protocol.DraARLv1Packet {
	return &protocol.DraARLv1Packet{Type: packetType, TimeStamp: time.Now()}
}

func testDispatchSockets(t *testing.T) (*net.UDPConn, *net.UDPConn, *net.UDPAddr) {
	t.Helper()
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	client, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		server.Close()
		client.Close()
	})
	return server, client, client.LocalAddr().(*net.UDPAddr)
}

func assertDispatchResponseType(t *testing.T, client *net.UDPConn, packetType byte) {
	t.Helper()
	if err := client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, protocol.DraARLv1MaxPacketSize)
	n, _, err := client.ReadFromUDP(buffer)
	if err != nil {
		t.Fatalf("read type %d response: %v", packetType, err)
	}
	packet, err := protocol.NewDraARLv1Packet(client.LocalAddr().(*net.UDPAddr), buffer[:n])
	if err != nil {
		t.Fatalf("decode type %d response: %v", packetType, err)
	}
	if packet.Type != packetType {
		t.Fatalf("response type = %d, want %d", packet.Type, packetType)
	}
}
