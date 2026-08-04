package udphub

import (
	"bytes"
	"net"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"draarl/internal/interfaces"
	"draarl/internal/models"
	"draarl/internal/protocol"
)

type routeTestWSDevice struct {
	identifier   string
	groupID      int
	userID       int
	deviceID     int
	username     string
	nickname     string
	callsign     string
	ssid         byte
	devModel     byte
	ghost        bool
	disableRecv  bool
	disableSend  bool
	sessionID    uint64
	sessionEpoch uint64
}

func (d *routeTestWSDevice) GetIdentifier() string        { return d.identifier }
func (d *routeTestWSDevice) GetCallSignSSID() string      { return d.callsign }
func (d *routeTestWSDevice) GetGroupID() int              { return d.groupID }
func (d *routeTestWSDevice) IsGhost() bool                { return d.ghost }
func (d *routeTestWSDevice) GetUserID() int               { return d.userID }
func (d *routeTestWSDevice) GetDeviceID() int             { return d.deviceID }
func (d *routeTestWSDevice) GetSessionID() string         { return d.identifier }
func (d *routeTestWSDevice) GetUsername() string          { return d.username }
func (d *routeTestWSDevice) GetNickname() string          { return d.nickname }
func (d *routeTestWSDevice) GetCallSign() string          { return d.callsign }
func (d *routeTestWSDevice) GetSSID() byte                { return d.ssid }
func (d *routeTestWSDevice) GetDevModel() byte            { return d.devModel }
func (d *routeTestWSDevice) IsDisabledRecv() bool         { return d.disableRecv }
func (d *routeTestWSDevice) IsDisabledSend() bool         { return d.disableSend }
func (d *routeTestWSDevice) GetConnectTime() time.Time    { return time.Time{} }
func (d *routeTestWSDevice) GetLastPacketTime() time.Time { return time.Time{} }
func (d *routeTestWSDevice) GetInterconnectSession() (uint64, uint64) {
	return d.sessionID, d.sessionEpoch
}
func (d *routeTestWSDevice) SetInterconnectSession(sessionID, sessionEpoch uint64) {
	d.sessionID, d.sessionEpoch = sessionID, sessionEpoch
}

type routeTestBroadcast struct {
	groups      []int
	data        []byte
	messageType int
	filter      interfaces.WSBroadcastFilter
}

type routeTestWSManager struct {
	mu         sync.Mutex
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
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]interfaces.WSDeviceInterface, 0, len(m.devices))
	for _, device := range m.devices {
		if device != nil && device.groupID == groupID {
			result = append(result, device)
		}
	}
	return result
}

func (m *routeTestWSManager) BroadcastToGroups(groupIDs []int, data []byte, messageType int, filter interfaces.WSBroadcastFilter) (sent, dropped int) {
	m.mu.Lock()
	defer m.mu.Unlock()
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
		if filter.ExcludeSessionID != "" && device.GetSessionID() == filter.ExcludeSessionID {
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
	m.mu.Lock()
	defer m.mu.Unlock()
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
func (m *routeTestWSManager) RevokeInterconnectSession(int, byte, uint64, uint64) bool { return false }

func (m *routeTestWSManager) resetDeliveries() {
	m.mu.Lock()
	m.deliveries = make(map[string][][]byte)
	m.broadcasts = nil
	m.mu.Unlock()
}

func (m *routeTestWSManager) deliveryCount(identifier string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.deliveries[identifier])
}

var (
	_ interfaces.WSDeviceInterface  = (*routeTestWSDevice)(nil)
	_ interfaces.WSManagerInterface = (*routeTestWSManager)(nil)
)

func TestBuildWSSpeakerUsesGhostSessionIdentity(t *testing.T) {
	first := &routeTestWSDevice{identifier: "session-a", userID: 7, ssid: 105, ghost: true}
	second := &routeTestWSDevice{identifier: "session-b", userID: 7, ssid: 105, ghost: true}
	if buildWSSpeaker(first).key == buildWSSpeaker(second).key {
		t.Fatal("same-account ghost sessions share a half-duplex speaker key")
	}
	if buildWSSpeaker(first).key != buildWSSpeaker(first).key {
		t.Fatal("ghost session speaker key is unstable")
	}
}

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
	return listenRouteTestUDPNetwork(t, "udp4")
}

func listenRouteTestUDPNetwork(t *testing.T, network string) *net.UDPConn {
	t.Helper()
	ip := net.IPv4(127, 0, 0, 1)
	if network == "udp6" {
		ip = net.IPv6loopback
	}
	conn, err := net.ListenUDP(network, &net.UDPAddr{IP: ip})
	if err != nil {
		t.Fatalf("listen UDP route endpoint: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func newRouteTestEndpoint(t *testing.T, id, groupID int, username, callsign string, ssid byte) routeTestEndpoint {
	return newRouteTestEndpointNetwork(t, "udp4", id, groupID, username, callsign, ssid)
}

func newRouteTestEndpointNetwork(t *testing.T, network string, id, groupID int, username, callsign string, ssid byte) routeTestEndpoint {
	t.Helper()
	conn := listenRouteTestUDPNetwork(t, network)
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
	return setupRouteTestNetwork(t, "udp4", baseGroupID, linked)
}

func setupRouteTestNetwork(t *testing.T, network string, baseGroupID int, linked bool) *routeTestEnv {
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
	env.serverConn = listenRouteTestUDPNetwork(t, network)
	env.udpA1 = newRouteTestEndpointNetwork(t, network, baseGroupID+10, env.groupA, "udp-source", "BG7UDPS", 7)
	env.udpA2 = newRouteTestEndpointNetwork(t, network, baseGroupID+11, env.groupA, "udp-a-target", "BG7UDPA", 8)
	env.udpB = newRouteTestEndpointNetwork(t, network, baseGroupID+12, env.groupB, "udp-b-target", "BG7UDPB", 9)
	env.udpC = newRouteTestEndpointNetwork(t, network, baseGroupID+13, env.groupC, "udp-c-target", "BG7UDPC", 10)

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
	oldTextBuffer := globalTextBuffer
	globalTextBuffer = NewTextMessageBuffer(100, time.Hour)
	globalTextBuffer.running = true

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
		globalTextBuffer = oldTextBuffer
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
	var expected protocol.DraARLv1Packet
	if err := expected.Decode(want); err != nil {
		t.Fatalf("decode expected routed packet: %v", err)
	}
	if decoded.DevicePassword != "" || decoded.Type != expected.Type || !bytes.Equal(decoded.DATA, payload) {
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

func routeTestUDPText(t *testing.T, env *routeTestEnv, payload []byte) []byte {
	t.Helper()
	source := env.udpA1.device
	raw := protocol.EncodeDraARLv1(
		source.Username, "topsecret!", source.SSID, protocol.DraARLTypeTextMessage,
		protocol.DraARLDevModelESP32Radio, 1, "SPOOFED", payload,
	)
	packet, err := protocol.NewDraARLv1Packet(source.UDPAddr, raw)
	if err != nil {
		t.Fatalf("decode UDP text source packet: %v", err)
	}
	group, ok := GetGroupFromCache(env.groupA)
	if !ok {
		t.Fatalf("source group %d missing", env.groupA)
	}
	handleDraARLTextMessage(packet, raw, source, env.serverConn, group)
	return protocol.EncodeDraARLv1(
		source.Username, "", source.SSID, protocol.DraARLTypeTextMessage,
		source.DevModel, source.DMRID, source.CallSign, payload,
	)
}

func routeTestWSText(env *routeTestEnv, payload []byte) []byte {
	env.router.RouteTextToUDP(env.wsSource, payload, env.groupA)
	return protocol.EncodeDraARLv1(
		env.wsSource.username, "", env.wsSource.ssid, protocol.DraARLTypeTextMessage,
		env.wsSource.devModel, 0, env.wsSource.callsign, payload,
	)
}

func TestRouteTextWithinGroupEndToEnd(t *testing.T) {
	t.Run("UDP source", func(t *testing.T) {
		env := setupRouteTest(t, 51200, false)
		payload := []byte("single-group UDP text")
		want := routeTestUDPText(t, env, payload)

		assertRouteTestPacket(t, readRouteTestPacket(t, env.udpA2.conn), want, payload)
		assertNoRouteTestPacket(t, env.udpA1.conn)
		assertNoRouteTestPacket(t, env.udpB.conn)
		assertNoRouteTestPacket(t, env.udpC.conn)
		assertRouteTestFanoutSent(t, 1)
		assertRouteTestWSDeliveries(t, env.wsManager, []string{"ws-source", "ws-a"}, want, payload, []int{env.groupA})
	})

	t.Run("WebSocket source", func(t *testing.T) {
		env := setupRouteTest(t, 51300, false)
		payload := []byte("single-group WebSocket text")
		want := routeTestWSText(env, payload)

		assertRouteTestPacket(t, readRouteTestPacket(t, env.udpA1.conn), want, payload)
		assertRouteTestPacket(t, readRouteTestPacket(t, env.udpA2.conn), want, payload)
		assertNoRouteTestPacket(t, env.udpB.conn)
		assertNoRouteTestPacket(t, env.udpC.conn)
		assertRouteTestFanoutSent(t, 2)
		assertRouteTestWSDeliveries(t, env.wsManager, []string{"ws-a"}, want, payload, []int{env.groupA})
	})
}

func TestRouteTextAcrossVirtualGroupEndToEnd(t *testing.T) {
	t.Run("UDP source", func(t *testing.T) {
		env := setupRouteTest(t, 51400, true)
		payload := []byte("linked-group UDP text")
		want := routeTestUDPText(t, env, payload)

		assertRouteTestPacket(t, readRouteTestPacket(t, env.udpA2.conn), want, payload)
		assertRouteTestPacket(t, readRouteTestPacket(t, env.udpB.conn), want, payload)
		assertNoRouteTestPacket(t, env.udpA1.conn)
		assertNoRouteTestPacket(t, env.udpC.conn)
		assertRouteTestFanoutSent(t, 2)
		assertRouteTestWSDeliveries(t, env.wsManager, []string{"ws-source", "ws-a", "ws-b"}, want, payload, []int{env.groupA, env.groupB})
	})

	t.Run("WebSocket source", func(t *testing.T) {
		env := setupRouteTest(t, 51500, true)
		payload := []byte("linked-group WebSocket text")
		want := routeTestWSText(env, payload)

		assertRouteTestPacket(t, readRouteTestPacket(t, env.udpA1.conn), want, payload)
		assertRouteTestPacket(t, readRouteTestPacket(t, env.udpA2.conn), want, payload)
		assertRouteTestPacket(t, readRouteTestPacket(t, env.udpB.conn), want, payload)
		assertNoRouteTestPacket(t, env.udpC.conn)
		assertRouteTestFanoutSent(t, 3)
		assertRouteTestWSDeliveries(t, env.wsManager, []string{"ws-a", "ws-b"}, want, payload, []int{env.groupA, env.groupB})
	})
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

func TestCenterLocalSourcesRelayOnceAcrossVirtualDomain(t *testing.T) {
	testRelay := func(t *testing.T, route func(*routeTestEnv) []byte, assertLocal func(*testing.T, *routeTestEnv, []byte)) {
		t.Helper()
		env := setupRouteTest(t, 54500, true)
		oldHooks := centerHooks()
		var nextSession atomic.Uint64
		relays := make(chan struct {
			source CenterLocalSource
			packet []byte
		}, 2)
		SetCenterInterconnectHooks(CenterInterconnectHooks{
			Activate: func(source *CenterLocalSource) error {
				if source.SessionID == 0 {
					source.SessionID = nextSession.Add(1)
					source.SessionEpoch = 1
				}
				return nil
			},
			Authorize: func(source CenterLocalSource) bool {
				return source.SessionID != 0 && source.SessionEpoch == 1
			},
			AcquireVoice: func(source CenterLocalSource) bool {
				return source.SessionID != 0 && source.SessionEpoch == 1
			},
			Relay: func(source CenterLocalSource, packet []byte) error {
				relays <- struct {
					source CenterLocalSource
					packet []byte
				}{source: source, packet: append([]byte(nil), packet...)}
				return nil
			},
		})
		t.Cleanup(func() { SetCenterInterconnectHooks(oldHooks) })

		want := route(env)
		assertLocal(t, env, want)
		select {
		case relay := <-relays:
			if relay.source.DomainID != GetActiveCommunicationDomainID(env.groupA) {
				t.Fatalf("relay domain=%d want=%d", relay.source.DomainID, GetActiveCommunicationDomainID(env.groupA))
			}
			if relay.source.SessionID == 0 || relay.source.SessionEpoch != 1 {
				t.Fatalf("relay session=%d/%d", relay.source.SessionID, relay.source.SessionEpoch)
			}
			if !bytes.Equal(relay.packet, want) {
				t.Fatalf("relayed packet mismatch\n got: %x\nwant: %x", relay.packet, want)
			}
		case <-time.After(time.Second):
			t.Fatal("centre-local frame was not uploaded to interconnect")
		}
		select {
		case duplicate := <-relays:
			t.Fatalf("centre-local frame was uploaded more than once: %#v", duplicate.source)
		case <-time.After(50 * time.Millisecond):
		}
	}

	t.Run("UDP source", func(t *testing.T) {
		payload := []byte{0x35, 0x36, 0x37}
		testRelay(t, func(env *routeTestEnv) []byte {
			if err := ActivateCenterLocalDevice(env.udpA1.device); err != nil {
				t.Fatal(err)
			}
			return routeTestUDPVoice(t, env, payload)
		}, func(t *testing.T, env *routeTestEnv, want []byte) {
			assertRouteTestPacket(t, readRouteTestPacket(t, env.udpA2.conn), want, payload)
			assertRouteTestPacket(t, readRouteTestPacket(t, env.udpB.conn), want, payload)
			assertNoRouteTestPacket(t, env.udpA1.conn)
			assertNoRouteTestPacket(t, env.udpC.conn)
			assertRouteTestFanoutSent(t, 2)
			assertRouteTestWSDeliveries(t, env.wsManager, []string{"ws-source", "ws-a", "ws-b"}, want, payload, []int{env.groupA, env.groupB})
		})
	})

	t.Run("WebSocket source", func(t *testing.T) {
		payload := []byte{0x38, 0x39, 0x3a}
		testRelay(t, func(env *routeTestEnv) []byte {
			if !AuthorizeCenterLocalWS(env.wsSource, env.groupA) {
				t.Fatal("WebSocket source could not establish centre-local interconnect ownership")
			}
			return routeTestWSVoice(env, payload)
		}, func(t *testing.T, env *routeTestEnv, want []byte) {
			assertRouteTestPacket(t, readRouteTestPacket(t, env.udpA1.conn), want, payload)
			assertRouteTestPacket(t, readRouteTestPacket(t, env.udpA2.conn), want, payload)
			assertRouteTestPacket(t, readRouteTestPacket(t, env.udpB.conn), want, payload)
			assertNoRouteTestPacket(t, env.udpC.conn)
			assertRouteTestFanoutSent(t, 3)
			assertRouteTestWSDeliveries(t, env.wsManager, []string{"ws-a", "ws-b"}, want, payload, []int{env.groupA, env.groupB})
		})
	})
}

func TestRouteVoiceIPv6AcrossVirtualGroupEndToEnd(t *testing.T) {
	t.Run("UDP to UDP and WS", func(t *testing.T) {
		env := setupRouteTestNetwork(t, "udp6", 55000, true)
		payload := []byte{0x41, 0x42, 0x43, 0x44}
		want := routeTestUDPVoice(t, env, payload)

		assertRouteTestPacket(t, readRouteTestPacket(t, env.udpA2.conn), want, payload)
		assertRouteTestPacket(t, readRouteTestPacket(t, env.udpB.conn), want, payload)
		assertNoRouteTestPacket(t, env.udpA1.conn)
		assertNoRouteTestPacket(t, env.udpC.conn)
		assertRouteTestFanoutSent(t, 2)
		assertRouteTestWSDeliveries(t, env.wsManager, []string{"ws-source", "ws-a", "ws-b"}, want, payload, []int{env.groupA, env.groupB})
	})

	t.Run("WS to UDP and WS", func(t *testing.T) {
		env := setupRouteTestNetwork(t, "udp6", 55100, true)
		payload := []byte{0x45, 0x46, 0x47, 0x48}
		want := routeTestWSVoice(env, payload)

		assertRouteTestPacket(t, readRouteTestPacket(t, env.udpA1.conn), want, payload)
		assertRouteTestPacket(t, readRouteTestPacket(t, env.udpA2.conn), want, payload)
		assertRouteTestPacket(t, readRouteTestPacket(t, env.udpB.conn), want, payload)
		assertNoRouteTestPacket(t, env.udpC.conn)
		assertRouteTestFanoutSent(t, 3)
		assertRouteTestWSDeliveries(t, env.wsManager, []string{"ws-a", "ws-b"}, want, payload, []int{env.groupA, env.groupB})
	})
}

func TestContinuousVoiceAppliesCommControlToNextFrame(t *testing.T) {
	t.Run("disable receive and restore", func(t *testing.T) {
		env := setupRouteTest(t, 56000, false)
		first := routeTestUDPVoice(t, env, []byte{1})
		assertRouteTestPacket(t, readRouteTestPacket(t, env.udpA2.conn), first, []byte{1})

		SyncDeviceCommControlByID(env.udpA2.device.ID, false, true)
		routeTestUDPVoice(t, env, []byte{2})
		assertNoRouteTestPacket(t, env.udpA2.conn)

		SyncDeviceCommControlByID(env.udpA2.device.ID, false, false)
		third := routeTestUDPVoice(t, env, []byte{3})
		assertRouteTestPacket(t, readRouteTestPacket(t, env.udpA2.conn), third, []byte{3})
	})

	t.Run("disable sender", func(t *testing.T) {
		env := setupRouteTest(t, 57000, false)
		first := routeTestUDPVoice(t, env, []byte{1})
		assertRouteTestPacket(t, readRouteTestPacket(t, env.udpA2.conn), first, []byte{1})
		env.wsManager.resetDeliveries()

		SyncDeviceCommControlByID(env.udpA1.device.ID, true, false)
		routeTestUDPVoice(t, env, []byte{2})
		assertNoRouteTestPacket(t, env.udpA2.conn)
		if len(env.wsManager.broadcasts) != 0 {
			t.Fatalf("disabled sender still broadcast %d WS frames", len(env.wsManager.broadcasts))
		}
	})
}

func TestContinuousVoiceDropsOldTargetsAfterRuntimeChanges(t *testing.T) {
	t.Run("group switch", func(t *testing.T) {
		env := setupRouteTest(t, 58000, false)
		first := routeTestUDPVoice(t, env, []byte{1})
		assertRouteTestPacket(t, readRouteTestPacket(t, env.udpA2.conn), first, []byte{1})

		if _, err := changeDeviceGroup(env.udpA2.device, env.groupC); err != nil {
			t.Fatalf("switch receiver group: %v", err)
		}
		routeTestUDPVoice(t, env, []byte{2})
		assertNoRouteTestPacket(t, env.udpA2.conn)
	})

	t.Run("offline removal", func(t *testing.T) {
		env := setupRouteTest(t, 59000, false)
		indexRuntimeDevice(env.udpA2.device)
		first := routeTestUDPVoice(t, env, []byte{1})
		assertRouteTestPacket(t, readRouteTestPacket(t, env.udpA2.conn), first, []byte{1})

		if !RemoveRuntimeDevice(env.udpA2.device.OwnerID, env.udpA2.device.SSID) {
			t.Fatal("runtime receiver was not removed")
		}
		routeTestUDPVoice(t, env, []byte{2})
		assertNoRouteTestPacket(t, env.udpA2.conn)
	})

	t.Run("link removal", func(t *testing.T) {
		env := setupRouteTest(t, 60000, true)
		first := routeTestUDPVoice(t, env, []byte{1})
		assertRouteTestPacket(t, readRouteTestPacket(t, env.udpB.conn), first, []byte{1})
		env.wsManager.resetDeliveries()

		globalGroupLinkCache.Lock()
		globalGroupLinkCache.targetToLinks = make(map[int][]int)
		globalGroupLinkCache.linkToTargets = make(map[int][]int)
		globalGroupLinkCache.targetToPeers = make(map[int][]int)
		globalGroupLinkCache.Unlock()
		resetHalfDuplexDomainCache()
		InvalidateDomainReceiverCache()

		routeTestUDPVoice(t, env, []byte{2})
		assertNoRouteTestPacket(t, env.udpB.conn)
		if got := env.wsManager.deliveryCount("ws-b"); got != 0 {
			t.Fatalf("unlinked WS target received %d frames", got)
		}
	})
}

func TestLargeRuntimeDeviceChurnKeepsReceiverSnapshotsConsistent(t *testing.T) {
	const deviceCount = 2000
	const groupA = 61000
	const groupB = 61001

	StopFanoutSender()
	StopDomainReceiverCache()
	clearDomainReceiverCacheForTest()
	clearRouteTestHalfDuplexState()
	first := newRouteTestGroup(groupA)
	second := newRouteTestGroup(groupB)
	globalGroupCacheAtomic.Store(map[int]*models.Group{groupA: first, groupB: second})
	oldGhostManager := GlobalUDPGhostManager
	GlobalUDPGhostManager = &UDPGhostManager{devices: make(map[string]*models.Device), groupDevices: make(map[int]map[string]*models.Device)}
	t.Cleanup(func() {
		StopDomainReceiverCache()
		clearDomainReceiverCacheForTest()
		clearRouteTestHalfDuplexState()
		globalGroupCacheAtomic.Store(map[int]*models.Group{})
		GlobalUDPGhostManager = oldGhostManager
		runtimeIndexMu.Lock()
		devOwnerSSIDMap = make(map[string]*models.Device)
		devUsernameSSIDMap = make(map[string]*models.Device)
		devCallsignSSIDMap = make(map[string]*models.Device)
		onlineDevMap = make(map[int]*models.Device)
		onlineDevMapDraARL = make(map[int]*models.Device)
		runtimeIndexMu.Unlock()
	})

	devices := make([]*models.Device, deviceCount)
	originalAddrs := make([]net.UDPAddr, deviceCount)
	for i := range devices {
		addr := net.UDPAddr{IP: net.IPv4(127, byte(i/65536), byte(i/256), byte(i)), Port: 10000 + i}
		dev := &models.Device{
			ID: 70000 + i, OwnerID: 80000 + i, Username: "churn-" + string(rune(i+1)),
			SSID: byte(i%99 + 1), GroupID: groupA, ISOnline: true,
			UDPAddr: &addr,
		}
		devices[i] = dev
		originalAddrs[i] = addr
		attachRuntimeDeviceToGroup(first, dev)
		indexRuntimeDevice(dev)
	}
	if got := len(getDomainReceiverEntries(groupA)); got != deviceCount {
		t.Fatalf("initial receiver count = %d, want %d", got, deviceCount)
	}

	for i, dev := range devices {
		switch i % 4 {
		case 0:
			SyncDeviceCommControlByID(dev.ID, false, true)
		case 1:
			if _, err := changeDeviceGroup(dev, groupB); err != nil {
				t.Fatalf("switch churn device %d: %v", dev.ID, err)
			}
		case 2:
			if !RemoveRuntimeDevice(dev.OwnerID, dev.SSID) {
				t.Fatalf("remove churn device %d", dev.ID)
			}
		}
	}
	if got := len(getDomainReceiverEntries(groupA)); got != deviceCount/4 {
		t.Fatalf("post-churn group A receivers = %d, want %d", got, deviceCount/4)
	}
	if got := len(getDomainReceiverEntries(groupB)); got != deviceCount/4 {
		t.Fatalf("post-churn group B receivers = %d, want %d", got, deviceCount/4)
	}

	for i, dev := range devices {
		if i%4 == 0 {
			SyncDeviceCommControlByID(dev.ID, false, false)
		}
	}
	if got := len(getDomainReceiverEntries(groupA)); got != deviceCount/2 {
		t.Fatalf("restored group A receivers = %d, want %d", got, deviceCount/2)
	}

	// 让已离线的四分之一设备重新上线，验证运行时索引、连接池和快照都能恢复。
	for i, dev := range devices {
		if i%4 != 2 {
			continue
		}
		dev.ISOnline = true
		dev.UDPAddr = &originalAddrs[i]
		dev.GroupID = groupA
		indexRuntimeDevice(dev)
		attachRuntimeDeviceToGroup(first, dev)
	}
	if got := len(getDomainReceiverEntries(groupA)); got != deviceCount*3/4 {
		t.Fatalf("re-online group A receivers = %d, want %d", got, deviceCount*3/4)
	}
	t.Logf("devices=%d final_group_a_receivers=%d final_group_b_receivers=%d", deviceCount, len(getDomainReceiverEntries(groupA)), len(getDomainReceiverEntries(groupB)))
}

func TestMixedUDPAndWSVoiceLoadKeepsDomainsOrdered(t *testing.T) {
	env := setupRouteTest(t, 62000, false)
	const frameCount = 128

	for sequence := 0; sequence < frameCount; sequence++ {
		routeTestUDPVoice(t, env, []byte{0xa1, byte(sequence)})
		env.router.RouteVoiceToUDP(env.wsC, []byte{0xc1, byte(sequence)}, env.groupC)
		// 1ms 仍远高于实际语音帧速率，同时避免把这个正确性用例
		// 变成已经单独验证过的 64 帧队列过载淘汰测试。
		time.Sleep(time.Millisecond)
	}

	readSequence := func(conn *net.UDPConn, marker byte) {
		for want := 0; want < frameCount; want++ {
			raw := readRouteTestPacket(t, conn)
			var packet protocol.DraARLv1Packet
			if err := packet.Decode(raw); err != nil {
				t.Fatalf("decode mixed route packet: %v", err)
			}
			if !bytes.Equal(packet.DATA, []byte{marker, byte(want)}) {
				t.Fatalf("mixed route marker=%x sequence=%x, want marker=%x sequence=%x", packet.DATA[0], packet.DATA[1], marker, want)
			}
			if packet.DevicePassword != "" {
				t.Fatalf("mixed route password = %q", packet.DevicePassword)
			}
		}
	}
	readSequence(env.udpA2.conn, 0xa1)
	readSequence(env.udpC.conn, 0xc1)
	assertNoRouteTestPacket(t, env.udpA1.conn)
	assertNoRouteTestPacket(t, env.udpB.conn)
	assertRouteTestFanoutSent(t, frameCount*2)

	if got := env.wsManager.deliveryCount("ws-a"); got != frameCount {
		t.Fatalf("mixed UDP->WS deliveries = %d, want %d", got, frameCount)
	}
	if got := env.wsManager.deliveryCount("ws-c"); got != 0 {
		t.Fatalf("mixed WS source received its own frames: %d", got)
	}
	t.Logf("udp_frames=%d ws_frames=%d udp_fanout_packets=%d ws_deliveries=%d", frameCount, frameCount, frameCount*2, env.wsManager.deliveryCount("ws-a"))
}
