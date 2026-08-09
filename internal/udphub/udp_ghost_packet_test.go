package udphub

import (
	"net"
	"testing"

	"draarl/internal/ghostsession"
	"draarl/internal/protocol"
)

func TestModernUDPGhostPacketRequiresExactSessionBinding(t *testing.T) {
	beforeMetrics := GetGhostPacketMetrics()
	previousManager := GlobalUDPGhostManager
	previousRegistry := ghostsession.Global
	GlobalUDPGhostManager = newUDPGhostManager()
	ghostsession.Global = ghostsession.NewRegistry(8, 16)
	t.Cleanup(func() {
		GlobalUDPGhostManager = previousManager
		ghostsession.Global = previousRegistry
	})

	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 32001}
	session, err := ghostsession.Global.Register(ghostsession.Registration{
		ClientInstanceID: "11111111-1111-4111-8111-111111111111",
		OwnerID:          1, Username: "alice", DevModel: protocol.DraARLDevModelAndroid,
		SSID: protocol.SSIDGhostAndroid, Transport: ghostsession.TransportUDP,
		Endpoint: addr.String(), ProtocolVersion: protocol.GhostAuthPayloadVersion,
		Capabilities: []string{"multi_receive_v1", "source_group_v1"},
		Routing:      ghostsession.Routing{TxGroupID: 1001, RxGroupIDs: []int{1001}},
	}, ghostsession.Controller{})
	if err != nil {
		t.Fatal(err)
	}
	device := modernUDPGhost(session.SessionID, session.SessionTag, addr.Port, 1001, []int{1001})
	if _, err := GlobalUDPGhostManager.RegisterSession(device); err != nil {
		t.Fatal(err)
	}

	decode := func(username string, ssid, model byte, tag uint32, source *net.UDPAddr) *protocol.DraARLv1Packet {
		wire := protocol.EncodeDraARLv1(username, "", ssid, protocol.DraARLTypeHeartbeat, model, 0, "", nil)
		wire, _ = protocol.WithReservedUint32(wire, tag)
		packet, decodeErr := protocol.NewDraARLv1RoutingPacket(source, wire)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		return packet
	}

	valid := decode("alice", device.SSID, device.DevModel, session.SessionTag, addr)
	defer protocol.ReleaseDraARLv1RoutingPacket(valid)
	if got, isGhost := getDeviceForPacket(valid, addr); got != device || !isGhost {
		t.Fatalf("valid packet resolved to %#v ghost=%v", got, isGhost)
	}

	tests := []struct {
		name     string
		username string
		ssid     byte
		model    byte
		tag      uint32
		addr     *net.UDPAddr
	}{
		{name: "missing tag", username: "alice", ssid: device.SSID, model: device.DevModel, tag: 0, addr: addr},
		{name: "tag", username: "alice", ssid: device.SSID, model: device.DevModel, tag: session.SessionTag + 1, addr: addr},
		{name: "username", username: "mallory", ssid: device.SSID, model: device.DevModel, tag: session.SessionTag, addr: addr},
		{name: "ssid", username: "alice", ssid: protocol.SSIDGhostIOS, model: device.DevModel, tag: session.SessionTag, addr: addr},
		{name: "model", username: "alice", ssid: device.SSID, model: protocol.DraARLDevModelIOS, tag: session.SessionTag, addr: addr},
		{name: "endpoint", username: "alice", ssid: device.SSID, model: device.DevModel, tag: session.SessionTag, addr: &net.UDPAddr{IP: addr.IP, Port: addr.Port + 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			packet := decode(test.username, test.ssid, test.model, test.tag, test.addr)
			defer protocol.ReleaseDraARLv1RoutingPacket(packet)
			if got, _ := getDeviceForPacket(packet, test.addr); got != nil {
				t.Fatalf("forged packet resolved to %#v", got)
			}
		})
	}
	afterForged := GetGhostPacketMetrics()
	if afterForged["invalid_tags"]-beforeMetrics["invalid_tags"] != 2 ||
		afterForged["identity_rejects"]-beforeMetrics["identity_rejects"] != 3 ||
		afterForged["endpoint_rejects"]-beforeMetrics["endpoint_rejects"] != 1 {
		t.Fatalf("unexpected forged packet metrics before=%v after=%v", beforeMetrics, afterForged)
	}

	ghostsession.Global.Remove(session.SessionID)
	stale := decode("alice", device.SSID, device.DevModel, session.SessionTag, addr)
	defer protocol.ReleaseDraARLv1RoutingPacket(stale)
	if got, _ := getDeviceForPacket(stale, addr); got != nil {
		t.Fatalf("removed registry session remained authenticated: %#v", got)
	}
	if after := GetGhostPacketMetrics(); after["registry_rejects"]-beforeMetrics["registry_rejects"] != 1 {
		t.Fatalf("registry reject metric before=%v after=%v", beforeMetrics, after)
	}
}

func TestSourceExclusionUsesExactGhostSession(t *testing.T) {
	exact := domainReceiverEntry{username: "alice", ssid: protocol.SSIDGhostAndroid, sessionID: "session-a"}
	sibling := domainReceiverEntry{username: "alice", ssid: protocol.SSIDGhostAndroid, sessionID: "session-b"}
	if !isSourceTarget(&exact, 0, "alice", protocol.SSIDGhostAndroid, "session-a") {
		t.Fatal("exact source session was not excluded")
	}
	if isSourceTarget(&sibling, 0, "alice", protocol.SSIDGhostAndroid, "session-a") {
		t.Fatal("same-account sibling session was incorrectly excluded")
	}
	physical := domainReceiverEntry{deviceID: 7, username: "physical", ssid: 3}
	if !isSourceTarget(&physical, 7, "physical", 3, "") {
		t.Fatal("physical source identity was not excluded")
	}
}
