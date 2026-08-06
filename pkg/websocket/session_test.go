package websocket

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"draarl/internal/ghostsession"
	"draarl/internal/interfaces"
	"draarl/internal/protocol"
)

func newTestGhostSession(sessionID string, ownerID, txGroupID int, rxGroupIDs []int, capabilities []string) *WSDevice {
	capabilities = []string{ghostsession.CapabilityMultiReceiveV1, ghostsession.CapabilitySourceGroupV1}
	return &WSDevice{
		SessionID: sessionID, UserID: ownerID, Username: "owner", CallSign: "BG7AAA",
		SSID: protocol.SSIDGhostWeb, DevModel: protocol.DraARLDevModelBrowser,
		DeviceType: DeviceTypeGhost, IsOnline: true, ConnState: StateOnline,
		ConnectTime: time.Now(), LastPacketTime: time.Now(),
		GroupID: txGroupID, RxGroupIDs: append([]int(nil), rxGroupIDs...),
		Capabilities: append([]string(nil), capabilities...), writeCh: make(chan *writeRequest, 8),
	}
}

func dequeueWriteRequest(t *testing.T, device *WSDevice) *writeRequest {
	t.Helper()
	select {
	case request := <-device.writeCh:
		if request == nil || request.payload == nil {
			t.Fatal("queued request has no payload")
		}
		return request
	default:
		t.Fatal("expected a queued WebSocket write")
		return nil
	}
}

func TestConnectionManagerKeepsMultipleOwnerSessionsAndExactCleanup(t *testing.T) {
	manager := NewWSConnectionManager()
	first := newTestGhostSession("session-first", 7, 1001, []int{1001, 1002}, nil)
	second := newTestGhostSession("session-second", 7, 1002, []int{1002}, nil)
	if err := manager.RegisterGhostDevice(first, first.UserID, first.Username, first.CallSign, "", first.SSID); err != nil {
		t.Fatal(err)
	}
	if err := manager.RegisterGhostDevice(second, second.UserID, second.Username, second.CallSign, "", second.SSID); err != nil {
		t.Fatal(err)
	}
	if got := len(manager.GetGhostDevicesByUser(7)); got != 2 {
		t.Fatalf("owner sessions=%d, want 2", got)
	}
	if got := len(manager.GetDevicesByGroup(1002)); got != 2 {
		t.Fatalf("group 1002 sessions=%d, want 2", got)
	}
	if got := manager.GetOnlineCount(); got != 2 {
		t.Fatalf("multi-receive sessions counted as %d online devices, want 2", got)
	}

	manager.UnregisterDevice(first)
	if _, exists := manager.GetGhostSession(first.SessionID); exists {
		t.Fatal("old session survived exact cleanup")
	}
	if current, exists := manager.GetGhostSession(second.SessionID); !exists || current != second {
		t.Fatal("old cleanup removed the other owner session")
	}
	if got := len(manager.GetDevicesByGroup(1002)); got != 1 || manager.GetDevicesByGroup(1002)[0] != second {
		t.Fatalf("group index after cleanup=%#v", manager.GetDevicesByGroup(1002))
	}
}

func TestBroadcastDeduplicatesSubscriptionsExcludesExactSessionAndAddsSourceGroup(t *testing.T) {
	beforeMetrics := getWSDeliveryStats()
	manager := NewWSConnectionManager()
	source := newTestGhostSession("source-session", 7, 1001, []int{1001}, nil)
	sibling := newTestGhostSession("sibling-session", 7, 1002, []int{1001, 1002}, nil)
	target := newTestGhostSession("target-session", 8, 1001, []int{1001, 1002}, nil)
	third := newTestGhostSession("third-session", 9, 1001, []int{1001}, nil)
	for _, device := range []*WSDevice{source, sibling, target, third} {
		if err := manager.RegisterGhostDevice(device, device.UserID, device.Username, device.CallSign, "", device.SSID); err != nil {
			t.Fatal(err)
		}
	}
	adapter := &WSManagerAdapter{manager: manager}
	packet := protocol.EncodeDraARLv1("owner", "", protocol.SSIDGhostWeb, protocol.DraARLTypeTextMessage, protocol.DraARLDevModelBrowser, 0, "BG7AAA", []byte("hello"))
	sent, dropped := adapter.BroadcastToGroups([]int{1001, 1002}, packet, 2, interfaces.WSBroadcastFilter{
		ExcludeSessionID: source.SessionID, SourceGroupID: 1001,
	})
	if sent != 3 || dropped != 0 {
		t.Fatalf("sent=%d dropped=%d, want 3/0", sent, dropped)
	}
	afterMetrics := getWSDeliveryStats()
	if afterMetrics["fanout_candidates"]-beforeMetrics["fanout_candidates"] != 5 ||
		afterMetrics["fanout_deduplicated"]-beforeMetrics["fanout_deduplicated"] != 2 ||
		afterMetrics["fanout_sent"]-beforeMetrics["fanout_sent"] != 3 ||
		afterMetrics["fanout_dropped"]-beforeMetrics["fanout_dropped"] != 0 {
		t.Fatalf("unexpected fanout metrics before=%v after=%v", beforeMetrics, afterMetrics)
	}
	if len(source.writeCh) != 0 {
		t.Fatal("source session received its own packet")
	}
	if len(sibling.writeCh) != 1 || len(target.writeCh) != 1 {
		t.Fatalf("multi-subscription delivery duplicated: sibling=%d target=%d", len(sibling.writeCh), len(target.writeCh))
	}
	siblingRequest := dequeueWriteRequest(t, sibling)
	targetRequest := dequeueWriteRequest(t, target)
	thirdRequest := dequeueWriteRequest(t, third)
	defer siblingRequest.payload.release()
	defer targetRequest.payload.release()
	defer thirdRequest.payload.release()
	for _, request := range []*writeRequest{siblingRequest, targetRequest, thirdRequest} {
		if got := binary.BigEndian.Uint32(request.payload.data[protocol.DraARLv1ReservedOffset:protocol.DraARLv1HeaderSize]); got != 1001 {
			t.Fatalf("source group=%d, want 1001", got)
		}
	}
}

func TestSetDeviceRoutingAtomicallyMovesAllSubscriptionIndexes(t *testing.T) {
	manager := NewWSConnectionManager()
	device := newTestGhostSession("routing-session", 11, 1001, []int{1001, 1002}, nil)
	if err := manager.RegisterGhostDevice(device, device.UserID, device.Username, device.CallSign, "", device.SSID); err != nil {
		t.Fatal(err)
	}
	if err := manager.SetDeviceRouting(device, ghostsession.Routing{TxGroupID: 1003, RxGroupIDs: []int{1004}}); err != nil {
		t.Fatal(err)
	}
	if len(manager.GetDevicesByGroup(1001)) != 0 || len(manager.GetDevicesByGroup(1002)) != 0 {
		t.Fatal("old subscription index was not removed")
	}
	if len(manager.GetDevicesByGroup(1003)) != 1 || len(manager.GetDevicesByGroup(1004)) != 1 {
		t.Fatalf("new indexes missing: tx=%d rx=%d", len(manager.GetDevicesByGroup(1003)), len(manager.GetDevicesByGroup(1004)))
	}
	if got := device.GetRxGroupIDs(); len(got) != 2 || got[0] != 1003 || got[1] != 1004 {
		t.Fatalf("routing=%#v", got)
	}
}

func TestAuthenticatedRoutingRollsBackIndexesWhenCenterProjectionFails(t *testing.T) {
	manager := NewWSConnectionManager()
	device := newTestGhostSession("routing-rollback", 12, 1001, []int{1001, 1002}, nil)
	if err := manager.RegisterGhostDevice(device, device.UserID, device.Username, device.CallSign, "", device.SSID); err != nil {
		t.Fatal(err)
	}

	authorizeCalls := make([]int, 0, 2)
	err := applyAuthenticatedRouting(manager, device, ghostsession.Routing{TxGroupID: 1003, RxGroupIDs: []int{1003, 1004}}, func(_ *WSDevice, groupID int) bool {
		authorizeCalls = append(authorizeCalls, groupID)
		return groupID == 1001
	})
	if err == nil {
		t.Fatal("failed center projection unexpectedly committed WebSocket routing")
	}
	if got := device.GetGroupID(); got != 1001 {
		t.Fatalf("tx group after rollback=%d want=1001", got)
	}
	if got := device.GetRxGroupIDs(); len(got) != 2 || got[0] != 1001 || got[1] != 1002 {
		t.Fatalf("rx groups after rollback=%v", got)
	}
	if len(manager.GetDevicesByGroup(1003)) != 0 || len(manager.GetDevicesByGroup(1004)) != 0 {
		t.Fatal("failed routing remained in subscription indexes")
	}
	if len(manager.GetDevicesByGroup(1001)) != 1 || len(manager.GetDevicesByGroup(1002)) != 1 {
		t.Fatal("previous subscription indexes were not restored")
	}
	if len(authorizeCalls) != 2 || authorizeCalls[0] != 1003 || authorizeCalls[1] != 1001 {
		t.Fatalf("projection calls=%v want=[1003 1001]", authorizeCalls)
	}
}

func TestAuthenticationSuccessAdvertisesSessionRouting(t *testing.T) {
	device := newTestGhostSession("auth-session", 31, 1001, []int{1001, 1002}, nil)
	device.ClientInstanceID = "11111111-1111-4111-8111-111111111111"
	device.ProtocolVersion = 1
	sendAuthenticationSuccess(device)
	request := dequeueWriteRequest(t, device)
	defer request.payload.release()
	var envelope struct {
		Type string `json:"type"`
		Data struct {
			SessionID  string `json:"session_id"`
			TxGroupID  int    `json:"tx_group_id"`
			RxGroupIDs []int  `json:"rx_group_ids"`
		} `json:"data"`
	}
	if err := json.Unmarshal(request.payload.data, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Type != "auth_success" || envelope.Data.SessionID != device.SessionID || envelope.Data.TxGroupID != 1001 || len(envelope.Data.RxGroupIDs) != 2 {
		t.Fatalf("auth success=%s", request.payload.data)
	}
}

func TestConcurrentRoutingBroadcastAndDisconnect(t *testing.T) {
	manager := NewWSConnectionManager()
	device := newTestGhostSession("concurrent-session", 41, 1001, []int{1001, 1002}, nil)
	device.writeCh = make(chan *writeRequest, 4096)
	if err := manager.RegisterGhostDevice(device, device.UserID, device.Username, device.CallSign, "", device.SSID); err != nil {
		t.Fatal(err)
	}
	adapter := &WSManagerAdapter{manager: manager}
	packet := protocol.EncodeDraARLv1("owner", "", protocol.SSIDGhostWeb, protocol.DraARLTypeTextMessage, protocol.DraARLDevModelBrowser, 0, "BG7AAA", []byte("race"))
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			adapter.BroadcastToGroups([]int{1001, 1002, 1003}, packet, 2, interfaces.WSBroadcastFilter{SourceGroupID: 1001})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			txGroupID := 1001 + i%3
			err := manager.SetDeviceRouting(device, ghostsession.Routing{TxGroupID: txGroupID, RxGroupIDs: []int{1001, 1002}})
			if err != nil && !errors.Is(err, ErrDeviceNotFound) {
				t.Errorf("routing update: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = manager.GetDevicesByGroup(1001)
		}
		manager.UnregisterDevice(device)
	}()
	wg.Wait()
	drainWriteRequests(device.writeCh)
}
