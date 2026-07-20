package udphub

import (
	"bytes"
	"net"
	"reflect"
	"testing"
	"time"

	"draarl/internal/interfaces"
	"draarl/internal/models"
	"draarl/internal/protocol"
)

type routeTestWSDevice struct {
	identifier  string
	groupID     int
	userID      int
	deviceID    int
	username    string
	callsign    string
	ssid        byte
	devModel    byte
	ghost       bool
	disableRecv bool
	disableSend bool
}

func (d *routeTestWSDevice) GetIdentifier() string        { return d.identifier }
func (d *routeTestWSDevice) GetCallSignSSID() string      { return d.callsign }
func (d *routeTestWSDevice) GetGroupID() int              { return d.groupID }
func (d *routeTestWSDevice) IsGhost() bool                { return d.ghost }
func (d *routeTestWSDevice) GetUserID() int               { return d.userID }
func (d *routeTestWSDevice) GetDeviceID() int             { return d.deviceID }
func (d *routeTestWSDevice) GetUsername() string          { return d.username }
func (d *routeTestWSDevice) GetCallSign() string          { return d.callsign }
func (d *routeTestWSDevice) GetSSID() byte                { return d.ssid }
func (d *routeTestWSDevice) GetDevModel() byte            { return d.devModel }
func (d *routeTestWSDevice) IsDisabledRecv() bool         { return d.disableRecv }
func (d *routeTestWSDevice) IsDisabledSend() bool         { return d.disableSend }
func (d *routeTestWSDevice) GetConnectTime() time.Time    { return time.Time{} }
func (d *routeTestWSDevice) GetLastPacketTime() time.Time { return time.Time{} }

type routeTestBroadcast struct {
	groups      []int
	data        []byte
	messageType int
	filter      interfaces.WSBroadcastFilter
}

type routeTestWSManager struct {
	devices    []*routeTestWSDevice
	deliveries map[string][][]byte
	broadcasts []routeTestBroadcast
}

func newRouteTestWSManager(devices ...*routeTestWSDevice) *routeTestWSManager {
	return &routeTestWSManager{
		devices:    devices,
		deliveries: make(map[string][][]byte),
	}
}

func (m *routeTestWSManager) GetDevicesByGroup(groupID int) []interfaces.WSDeviceInterface {
	result := make([]interfaces.WSDeviceInterface, 0, len(m.devices))
	for _, device := range m.devices {
		if device != nil && device.groupID == groupID {
			result = append(result, device)
		}
	}
	return result
}

func (m *routeTestWSManager) BroadcastToGroups(groupIDs []int, data []byte, messageType int, filter interfaces.WSBroadcastFilter) (sent, dropped int) {
	m.broadcasts = append(m.broadcasts, routeTestBroadcast{
		groups:      append([]int(nil), groupIDs...),
		data:        append([]byte(nil), data...),
		messageType: messageType,
		filter:      filter,
	})
	groupSet := make(map[int]struct{}, len(groupIDs))
	for _, groupID := range groupIDs {
		groupSet[groupID] = struct{}{}
	}
	for _, device := range m.devices {
		if device == nil || device.disableRecv {
			continue
		}
		if _, ok := groupSet[device.groupID]; !ok {
			continue
		}
		if filter.ExcludeDeviceID != 0 && !device.ghost && device.deviceID == filter.ExcludeDeviceID {
			continue
		}
		if filter.ExcludeUserID != 0 && device.ghost &&
			device.userID == filter.ExcludeUserID && device.ssid == filter.ExcludeSSID {
			continue
		}
		m.deliveries[device.identifier] = append(
			m.deliveries[device.identifier], append([]byte(nil), data...),
		)
		sent++
	}
	return sent, 0
}

func (m *routeTestWSManager) GetDeliveryStats() map[string]int64 { return nil }

func (m *routeTestWSManager) GetOnlineCount() (normalCount, ghostCount int) {
	for _, device := range m.devices {
		if device == nil {
			continue
		}
		if device.ghost {
			ghostCount++
		} else {
			normalCount++
		}
	}
	return normalCount, ghostCount
}

var (
	_ interfaces.WSDeviceInterface  = (*routeTestWSDevice)(nil)
	_ interfaces.WSManagerInterface = (*routeTestWSManager)(nil)
)

type routeTestEndpoint struct {
	conn   *net.UDPConn
	device *models.Device
}

type routeTestEnv struct {
	groupA     int
	groupB     int
	groupC     int
	virtual    int
	serverConn *net.UDPConn
	udpA1      routeTestEndpoint
	udpA2      routeTestEndpoint
	udpB       routeTestEndpoint
	udpC       routeTestEndpoint
	wsSource   *routeTestWSDevice
	wsA        *routeTestWSDevice
	wsB        *routeTestWSDevice
	wsC        *routeTestWSDevice
	wsManager  *routeTestWSManager
	router     *MessageRouter
}

func listenRouteTestUDP(t *testing.T) *net.UDPConn {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen UDP route endpoint: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func newRouteTestEndpoint(t *testing.T, id, groupID int, username, callsign string, ssid byte) routeTestEndpoint {
	t.Helper()
	conn := listenRouteTestUDP(t)
	return routeTestEndpoint{
		conn: conn,
		device: &models.Device{
			ID: id, OwnerID: id + 1000, Username: username, CallSign: callsign,
			SSID: ssid, DevModel: protocol.DraARLDevModelESP32NoRadio, DMRID: uint32(100000 + id),
			GroupID: groupID, ISOnline: true, UDPAddr: conn.LocalAddr().(*net.UDPAddr),
		},
	}
}

func newRouteTestGroup(id int, endpoints ...routeTestEndpoint) *models.Group {
	pool := newConnPool()
	devices := make([]*models.Device, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if endpoint.device == nil || endpoint.device.UDPAddr == nil {
			continue
		}
		pool.DevConnMap[endpoint.device.UDPAddr.String()] = endpoint.device
		devices = append(devices, endpoint.device)
	}
	pool.storeConnList(devices)
	return &models.Group{
		ID: id, Name: "route-test", Type: 1, Status: 1,
		DevMap: make(map[int]*models.Device), ConnPool: pool,
	}
}

func clearRouteTestHalfDuplexState() {
	for i := range halfDuplexShards {
		shard := &halfDuplexShards[i]
		shard.mu.Lock()
		shard.states = make(map[string]*halfDuplexDomainState)
		shard.mu.Unlock()
	}
	resetHalfDuplexDomainCache()
}

func setupRouteTest(t *testing.T, baseGroupID int, linked bool) *routeTestEnv {
	t.Helper()
	StopFanoutSender()
	StopDomainReceiverCache()
	clearDomainReceiverCacheForTest()
	clearRouteTestHalfDuplexState()

	env := &routeTestEnv{
		groupA:  baseGroupID,
		groupB:  baseGroupID + 1,
		groupC:  baseGroupID + 2,
		virtual: baseGroupID + 100,
	}
	env.serverConn = listenRouteTestUDP(t)
	env.udpA1 = newRouteTestEndpoint(t, baseGroupID+10, env.groupA, "udp-source", "BG7UDPS", 7)
	env.udpA2 = newRouteTestEndpoint(t, baseGroupID+11, env.groupA, "udp-a-target", "BG7UDPA", 8)
	env.udpB = newRouteTestEndpoint(t, baseGroupID+12, env.groupB, "udp-b-target", "BG7UDPB", 9)
	env.udpC = newRouteTestEndpoint(t, baseGroupID+13, env.groupC, "udp-c-target", "BG7UDPC", 10)

	groups := map[int]*models.Group{
		env.groupA: newRouteTestGroup(env.groupA, env.udpA1, env.udpA2),
		env.groupB: newRouteTestGroup(env.groupB, env.udpB),
		env.groupC: newRouteTestGroup(env.groupC, env.udpC),
		env.virtual: {
			ID: env.virtual, Name: "route-test-link", Type: 1, Status: 1, IsVirtual: true,
			DevMap: make(map[int]*models.Device), ConnPool: newConnPool(),
		},
	}
	globalGroupCacheAtomic.Store(groups)

	globalGroupLinkCache.Lock()
	globalGroupLinkCache.targetToLinks = make(map[int][]int)
	globalGroupLinkCache.linkToTargets = make(map[int][]int)
	globalGroupLinkCache.targetToPeers = make(map[int][]int)
	if linked {
		globalGroupLinkCache.targetToLinks[env.groupA] = []int{env.virtual}
		globalGroupLinkCache.targetToLinks[env.groupB] = []int{env.virtual}
		globalGroupLinkCache.linkToTargets[env.virtual] = []int{env.groupA, env.groupB}
		globalGroupLinkCache.targetToPeers[env.groupA] = []int{env.groupB}
		globalGroupLinkCache.targetToPeers[env.groupB] = []int{env.groupA}
	}
	globalGroupLinkCache.Unlock()
	resetHalfDuplexDomainCache()
	InvalidateDomainReceiverCache()

	oldGhostManager := GlobalUDPGhostManager
	GlobalUDPGhostManager = &UDPGhostManager{
		devices: make(map[string]*models.Device), groupDevices: make(map[int]map[string]*models.Device),
	}

	env.wsSource = &routeTestWSDevice{
		identifier: "ws-source", groupID: env.groupA, userID: baseGroupID + 201,
		deviceID: -(baseGroupID + 201), username: "ws-source", callsign: "BG7WSS",
		ssid: protocol.SSIDGhostWeb, devModel: protocol.DraARLDevModelBrowser, ghost: true,
	}
	env.wsA = &routeTestWSDevice{
		identifier: "ws-a", groupID: env.groupA, userID: baseGroupID + 202,
		deviceID: -(baseGroupID + 202), username: "ws-a", callsign: "BG7WSA",
		ssid: protocol.SSIDGhostWeb, devModel: protocol.DraARLDevModelBrowser, ghost: true,
	}
	env.wsB = &routeTestWSDevice{
		identifier: "ws-b", groupID: env.groupB, userID: baseGroupID + 203,
		deviceID: -(baseGroupID + 203), username: "ws-b", callsign: "BG7WSB",
		ssid: protocol.SSIDGhostWeb, devModel: protocol.DraARLDevModelBrowser, ghost: true,
	}
	env.wsC = &routeTestWSDevice{
		identifier: "ws-c", groupID: env.groupC, userID: baseGroupID + 204,
		deviceID: -(baseGroupID + 204), username: "ws-c", callsign: "BG7WSC",
		ssid: protocol.SSIDGhostWeb, devModel: protocol.DraARLDevModelBrowser, ghost: true,
	}
	env.wsManager = newRouteTestWSManager(env.wsSource, env.wsA, env.wsB, env.wsC)
	env.router = NewMessageRouter(env.wsManager)

	oldRouter := GlobalMessageRouter
	oldConn := globalConn
	GlobalMessageRouter = env.router
	globalConn = env.serverConn
	sender := newFanoutSenderWithMaxAge(env.serverConn, 2, 64, time.Second)
	globalFanoutMu.Lock()
	globalFanoutSender = sender
	globalFanoutMu.Unlock()

	t.Cleanup(func() {
		StopFanoutSender()
		globalConn = oldConn
		GlobalMessageRouter = oldRouter
		GlobalUDPGhostManager = oldGhostManager
		StopDomainReceiverCache()
		clearDomainReceiverCacheForTest()
		clearRouteTestHalfDuplexState()
		globalGroupCacheAtomic.Store(map[int]*models.Group{})
		globalGroupLinkCache.Lock()
		globalGroupLinkCache.targetToLinks = make(map[int][]int)
		globalGroupLinkCache.linkToTargets = make(map[int][]int)
		globalGroupLinkCache.targetToPeers = make(map[int][]int)
		globalGroupLinkCache.Unlock()
	})
	return env
}

func readRouteTestPacket(t *testing.T, conn *net.UDPConn) []byte {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set UDP read deadline: %v", err)
	}
	buf := make([]byte, protocol.DraARLv1MaxPacketSize)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("read routed UDP packet: %v", err)
	}
	return append([]byte(nil), buf[:n]...)
}

func assertNoRouteTestPacket(t *testing.T, conn *net.UDPConn) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(75 * time.Millisecond)); err != nil {
		t.Fatalf("set UDP isolation deadline: %v", err)
	}
	buf := make([]byte, protocol.DraARLv1MaxPacketSize)
	if n, _, err := conn.ReadFromUDP(buf); err == nil {
		t.Fatalf("unexpected routed UDP packet: %x", buf[:n])
	} else if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
		t.Fatalf("check unexpected UDP packet: %v", err)
	}
}

func assertRouteTestFanoutSent(t *testing.T, want int64) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		stats := GetFanoutSenderStats()
		got := stats["sent"]
		if got == want {
			return
		}
		if got > want || time.Now().After(deadline) {
			t.Fatalf("UDP fan-out sent = %d, want %d", got, want)
		}
		time.Sleep(time.Millisecond)
	}
}

func assertRouteTestPacket(t *testing.T, got, want, payload []byte) {
	t.Helper()
	if !bytes.Equal(got, want) {
		t.Fatalf("routed packet mismatch\n got: %x\nwant: %x", got, want)
	}
	if len(got) < protocol.DraARLv1HeaderSize || !bytes.Equal(got[38:48], make([]byte, 10)) {
		t.Fatalf("routed password bytes were not cleared: %x", got)
	}
	var decoded protocol.DraARLv1Packet
	if err := decoded.Decode(got); err != nil {
		t.Fatalf("decode routed packet: %v", err)
	}
	if decoded.DevicePassword != "" || decoded.Type != protocol.DraARLTypeOpus16K || !bytes.Equal(decoded.DATA, payload) {
		t.Fatalf("routed packet fields password=%q type=%d data=%x", decoded.DevicePassword, decoded.Type, decoded.DATA)
	}
}

func assertRouteTestWSDeliveries(t *testing.T, manager *routeTestWSManager, want []string, packet, payload []byte, groups []int) {
	t.Helper()
	wantSet := make(map[string]struct{}, len(want))
	for _, identifier := range want {
		wantSet[identifier] = struct{}{}
	}
	for _, device := range manager.devices {
		if device == nil {
			continue
		}
		deliveries := manager.deliveries[device.identifier]
		_, expected := wantSet[device.identifier]
		if !expected {
			if len(deliveries) != 0 {
				t.Fatalf("unexpected WS delivery to %s: %d", device.identifier, len(deliveries))
			}
			continue
		}
		if len(deliveries) != 1 {
			t.Fatalf("WS deliveries to %s = %d, want 1", device.identifier, len(deliveries))
		}
		assertRouteTestPacket(t, deliveries[0], packet, payload)
	}
	if len(manager.broadcasts) != 1 {
		t.Fatalf("WS broadcast calls = %d, want 1", len(manager.broadcasts))
	}
	broadcast := manager.broadcasts[0]
	if !reflect.DeepEqual(broadcast.groups, groups) || broadcast.messageType != 2 {
		t.Fatalf("WS broadcast groups/type = %v/%d, want %v/2", broadcast.groups, broadcast.messageType, groups)
	}
	assertRouteTestPacket(t, broadcast.data, packet, payload)
}

func routeTestUDPVoice(t *testing.T, env *routeTestEnv, payload []byte) []byte {
	t.Helper()
	source := env.udpA1.device
	raw := protocol.EncodeDraARLv1(
		source.Username, "topsecret!", source.SSID, protocol.DraARLTypeOpus16K,
		protocol.DraARLDevModelESP32Radio, 1, "SPOOFED", payload,
	)
	packet, err := protocol.NewDraARLv1Packet(source.UDPAddr, raw)
	if err != nil {
		t.Fatalf("decode UDP source packet: %v", err)
	}
	packet.TimeStamp = time.Now()
	group, ok := GetGroupFromCache(env.groupA)
	if !ok {
		t.Fatalf("source group %d missing", env.groupA)
	}
	handleDraARLVoice(packet, raw, source, env.serverConn, group)
	return protocol.EncodeDraARLv1(
		source.Username, "", source.SSID, protocol.DraARLTypeOpus16K,
		source.DevModel, source.DMRID, source.CallSign, payload,
	)
}

func routeTestWSVoice(env *routeTestEnv, payload []byte) []byte {
	env.router.RouteVoiceToUDP(env.wsSource, payload, env.groupA)
	return protocol.EncodeDraARLv1(
		env.wsSource.username, "", env.wsSource.ssid, protocol.DraARLTypeOpus16K,
		env.wsSource.devModel, 0, env.wsSource.callsign, payload,
	)
}

func TestRouteUDPVoiceWithinGroupEndToEnd(t *testing.T) {
	env := setupRouteTest(t, 51000, false)
	payload := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	want := routeTestUDPVoice(t, env, payload)

	assertRouteTestPacket(t, readRouteTestPacket(t, env.udpA2.conn), want, payload)
	assertNoRouteTestPacket(t, env.udpA1.conn)
	assertNoRouteTestPacket(t, env.udpB.conn)
	assertNoRouteTestPacket(t, env.udpC.conn)
	assertRouteTestFanoutSent(t, 1)
	assertRouteTestWSDeliveries(t, env.wsManager, []string{"ws-source", "ws-a"}, want, payload, []int{env.groupA})
}

func TestRouteWSVoiceToUDPWithinGroupEndToEnd(t *testing.T) {
	env := setupRouteTest(t, 52000, false)
	payload := []byte{0x11, 0x12, 0x13, 0x14}
	want := routeTestWSVoice(env, payload)

	assertRouteTestPacket(t, readRouteTestPacket(t, env.udpA1.conn), want, payload)
	assertRouteTestPacket(t, readRouteTestPacket(t, env.udpA2.conn), want, payload)
	assertNoRouteTestPacket(t, env.udpB.conn)
	assertNoRouteTestPacket(t, env.udpC.conn)
	assertRouteTestFanoutSent(t, 2)
	assertRouteTestWSDeliveries(t, env.wsManager, []string{"ws-a"}, want, payload, []int{env.groupA})
}

func TestRouteUDPVoiceAcrossVirtualGroupEndToEnd(t *testing.T) {
	env := setupRouteTest(t, 53000, true)
	payload := []byte{0x21, 0x22, 0x23, 0x24, 0x25}
	want := routeTestUDPVoice(t, env, payload)

	assertRouteTestPacket(t, readRouteTestPacket(t, env.udpA2.conn), want, payload)
	assertRouteTestPacket(t, readRouteTestPacket(t, env.udpB.conn), want, payload)
	assertNoRouteTestPacket(t, env.udpA1.conn)
	assertNoRouteTestPacket(t, env.udpC.conn)
	assertRouteTestFanoutSent(t, 2)
	assertRouteTestWSDeliveries(t, env.wsManager, []string{"ws-source", "ws-a", "ws-b"}, want, payload, []int{env.groupA, env.groupB})
}

func TestRouteWSVoiceToUDPAcrossVirtualGroupEndToEnd(t *testing.T) {
	env := setupRouteTest(t, 54000, true)
	payload := []byte{0x31, 0x32, 0x33, 0x34}
	want := routeTestWSVoice(env, payload)

	assertRouteTestPacket(t, readRouteTestPacket(t, env.udpA1.conn), want, payload)
	assertRouteTestPacket(t, readRouteTestPacket(t, env.udpA2.conn), want, payload)
	assertRouteTestPacket(t, readRouteTestPacket(t, env.udpB.conn), want, payload)
	assertNoRouteTestPacket(t, env.udpC.conn)
	assertRouteTestFanoutSent(t, 3)
	assertRouteTestWSDeliveries(t, env.wsManager, []string{"ws-a", "ws-b"}, want, payload, []int{env.groupA, env.groupB})
}
