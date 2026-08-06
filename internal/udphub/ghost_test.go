package udphub

import (
	"errors"
	"net"
	"testing"
	"time"

	"draarl/internal/ghostsession"
	"draarl/internal/models"
	"draarl/internal/protocol"
)

func modernUDPGhost(sessionID string, tag uint32, port, txGroupID int, rxGroupIDs []int) *models.Device {
	return &models.Device{
		Username: "alice", OwnerID: 1, SSID: protocol.SSIDGhostAndroid,
		DevModel: protocol.DraARLDevModelAndroid, GroupID: txGroupID,
		ISOnline: true, UDPAddr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port},
		LastPacketTime: time.Now(), GhostSessionID: sessionID, GhostSessionTag: tag,
		ClientInstanceID: "11111111-1111-4111-8111-111111111111",
		GhostRxGroupIDs:  rxGroupIDs, GhostProtocolVersion: protocol.GhostAuthPayloadVersion,
		GhostCapabilities: []string{"multi_receive_v1", "source_group_v1"},
	}
}

func TestUDPGhostManagerKeepsSameAccountSessionsIndependent(t *testing.T) {
	manager := newUDPGhostManager()
	first := modernUDPGhost("session-a", 11, 31001, 1001, []int{1001, 1002})
	second := modernUDPGhost("session-b", 12, 31002, 1002, []int{1002})
	second.ClientInstanceID = "22222222-2222-4222-8222-222222222222"
	if _, err := manager.RegisterSession(first); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RegisterSession(second); err != nil {
		t.Fatal(err)
	}
	if got := manager.GetAll(); len(got) != 2 {
		t.Fatalf("session count=%d, want 2", len(got))
	}
	if got := manager.GetOnlineCount(); got != 2 {
		t.Fatalf("multi-receive sessions counted as %d online devices, want 2", got)
	}
	if got := manager.GetByGroup(1002); len(got) != 2 {
		t.Fatalf("group 1002 receivers=%d, want 2", len(got))
	}
	if err := manager.SetSessionRouting(first.GhostSessionID, ghostsession.Routing{TxGroupID: 1003, RxGroupIDs: []int{1003, 1004}}); err != nil {
		t.Fatal(err)
	}
	if got := manager.GetByGroup(1001); len(got) != 0 {
		t.Fatalf("old receive index retained %d sessions", len(got))
	}
	if got := manager.GetByGroup(1002); len(got) != 1 || got[0] != second {
		t.Fatalf("routing one session affected its sibling: %#v", got)
	}
	if removed := manager.RemoveSession(first.GhostSessionID); removed != first {
		t.Fatalf("removed=%p, want first=%p", removed, first)
	}
	if manager.GetSession(second.GhostSessionID) != second {
		t.Fatal("removing one session removed its sibling")
	}
}

func TestOnlineGhostCountHelpersUseSessionsInsteadOfSubscriptions(t *testing.T) {
	previousManager := GlobalUDPGhostManager
	previousRouter := GlobalMessageRouter
	previousRegistry := ghostsession.Global
	GlobalUDPGhostManager = newUDPGhostManager()
	GlobalMessageRouter = nil
	ghostsession.Global = ghostsession.NewRegistry(8, 16)
	t.Cleanup(func() {
		GlobalUDPGhostManager = previousManager
		GlobalMessageRouter = previousRouter
		ghostsession.Global = previousRegistry
	})

	register := func(instanceID string, transport ghostsession.Transport, txGroupID int, rxGroupIDs []int) ghostsession.Session {
		t.Helper()
		session, err := ghostsession.Global.Register(ghostsession.Registration{
			ClientInstanceID: instanceID, OwnerID: 7, Username: "count-user",
			DevModel: protocol.DraARLDevModelAndroid, SSID: protocol.SSIDGhostAndroid, Transport: transport,
			ProtocolVersion: protocol.GhostAuthPayloadVersion,
			Capabilities:    []string{ghostsession.CapabilityMultiReceiveV1, ghostsession.CapabilitySourceGroupV1},
			Routing:         ghostsession.Routing{TxGroupID: txGroupID, RxGroupIDs: rxGroupIDs},
		}, ghostsession.Controller{})
		if err != nil {
			t.Fatal(err)
		}
		return session
	}
	firstSession := register("11111111-1111-4111-8111-111111111111", ghostsession.TransportUDP, 1001, []int{1001, 1002, 1003})
	secondSession := register("22222222-2222-4222-8222-222222222222", ghostsession.TransportUDP, 1002, []int{1002, 1003})
	register("33333333-3333-4333-8333-333333333333", ghostsession.TransportEdge, 1002, []int{1002, 1003})

	first := modernUDPGhost(firstSession.SessionID, firstSession.SessionTag, 31101, 1001, []int{1001, 1002, 1003})
	second := modernUDPGhost(secondSession.SessionID, secondSession.SessionTag, 31102, 1002, []int{1002, 1003})
	second.ClientInstanceID = "22222222-2222-4222-8222-222222222222"
	if _, err := GlobalUDPGhostManager.RegisterSession(first); err != nil {
		t.Fatal(err)
	}
	if _, err := GlobalUDPGhostManager.RegisterSession(second); err != nil {
		t.Fatal(err)
	}
	orphan := modernUDPGhost("orphan-data-plane-session", 99, 31103, 1002, []int{1002})
	orphan.ClientInstanceID = "44444444-4444-4444-8444-444444444444"
	if _, err := GlobalUDPGhostManager.RegisterSession(orphan); err != nil {
		t.Fatal(err)
	}

	if got := GetOnlineGhostCount(); got != 3 {
		t.Fatalf("server ghost online count=%d, want 3 sessions", got)
	}
	if got := GetOnlineGhostCountByGroup(1002); got != 3 {
		t.Fatalf("group ghost online count=%d, want 3 sessions", got)
	}
	udpGhost, wsNormal, wsGhost, edgeGhost := onlineTransportOnlineCounts()
	if udpGhost != 2 || wsNormal != 0 || wsGhost != 0 || edgeGhost != 1 {
		t.Fatalf("transport counts udp=%d ws_normal=%d ws_ghost=%d edge=%d", udpGhost, wsNormal, wsGhost, edgeGhost)
	}
	register("55555555-5555-4555-8555-555555555555", ghostsession.TransportWebSocket, 1002, []int{1002})
	if got := GetOnlineGhostCount(); got != 4 {
		t.Fatalf("server ghost online count after websocket registration=%d, want 4 sessions", got)
	}
	if got := GetOnlineGhostCountByGroup(1002); got != 4 {
		t.Fatalf("group ghost online count after websocket registration=%d, want 4 sessions", got)
	}
	udpGhost, wsNormal, wsGhost, edgeGhost = onlineTransportOnlineCounts()
	if udpGhost != 2 || wsNormal != 0 || wsGhost != 1 || edgeGhost != 1 {
		t.Fatalf("transport counts after websocket registration udp=%d ws_normal=%d ws_ghost=%d edge=%d", udpGhost, wsNormal, wsGhost, edgeGhost)
	}
	if got := totalOnlineDeviceCount(4, 2, 1, 3, 2); got != 12 {
		t.Fatalf("combined online count=%d, want 12", got)
	}
}

func TestUDPGhostManagerRejectsDuplicateTagAndEndpoint(t *testing.T) {
	manager := newUDPGhostManager()
	first := modernUDPGhost("session-a", 11, 31001, 1001, []int{1001})
	if _, err := manager.RegisterSession(first); err != nil {
		t.Fatal(err)
	}
	duplicateTag := modernUDPGhost("session-b", 11, 31002, 1001, []int{1001})
	if _, err := manager.RegisterSession(duplicateTag); !errors.Is(err, errUDPGhostSessionTag) {
		t.Fatalf("duplicate tag error=%v", err)
	}
	duplicateEndpoint := modernUDPGhost("session-c", 13, 31001, 1001, []int{1001})
	if _, err := manager.RegisterSession(duplicateEndpoint); !errors.Is(err, errUDPGhostEndpointInUse) {
		t.Fatalf("duplicate endpoint error=%v", err)
	}
}
