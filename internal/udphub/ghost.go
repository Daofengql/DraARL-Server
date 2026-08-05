package udphub

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"draarl/internal/ghostsession"
	"draarl/internal/models"
	"draarl/internal/protocol"
)

var (
	errUDPGhostSessionRequired = errors.New("udp ghost session id is required")
	errUDPGhostSessionTag      = errors.New("udp ghost session tag is required")
	errUDPGhostEndpointInUse   = errors.New("udp ghost endpoint is already registered")
)

// UDPGhostManager keeps legacy UDP ghosts in their historical single-platform
// slot while modern clients are indexed by their exact authenticated session.
type UDPGhostManager struct {
	// Legacy index: username + SSID. Modern sessions never use this as their
	// primary identity; legacy sessions are mirrored here for compatibility.
	devices map[string]*models.Device

	// Modern/session indexes.
	sessions        map[string]*models.Device
	sessionTags     map[uint32]string
	addressSessions map[netip.AddrPort]string

	// Receive index. Keys are either the legacy username/SSID key or a
	// namespaced session key, so one session can be present in many groups.
	groupDevices map[int]map[string]*models.Device

	mu sync.RWMutex
}

var GlobalUDPGhostManager = newUDPGhostManager()

func newUDPGhostManager() *UDPGhostManager {
	return &UDPGhostManager{
		devices: make(map[string]*models.Device), sessions: make(map[string]*models.Device),
		sessionTags: make(map[uint32]string), addressSessions: make(map[netip.AddrPort]string),
		groupDevices: make(map[int]map[string]*models.Device),
	}
}

func getDeviceKey(username string, ssid byte) string {
	return fmt.Sprintf("%s-%d", username, ssid)
}

func sessionDeviceKey(sessionID string) string {
	return "session:" + sessionID
}

func (m *UDPGhostManager) ensureMapsLocked() {
	if m.devices == nil {
		m.devices = make(map[string]*models.Device)
	}
	if m.sessions == nil {
		m.sessions = make(map[string]*models.Device)
	}
	if m.sessionTags == nil {
		m.sessionTags = make(map[uint32]string)
	}
	if m.addressSessions == nil {
		m.addressSessions = make(map[netip.AddrPort]string)
	}
	if m.groupDevices == nil {
		m.groupDevices = make(map[int]map[string]*models.Device)
	}
}

func addGhostToGroupIndex(index map[int]map[string]*models.Device, groupID int, key string, device *models.Device) {
	if groupID <= 0 || device == nil {
		return
	}
	if index[groupID] == nil {
		index[groupID] = make(map[string]*models.Device)
	}
	index[groupID][key] = device
}

func removeGhostFromGroupIndex(index map[int]map[string]*models.Device, groupID int, key string) {
	if group := index[groupID]; group != nil {
		delete(group, key)
		if len(group) == 0 {
			delete(index, groupID)
		}
	}
}

func ghostReceiveGroups(device *models.Device) []int {
	if device == nil {
		return nil
	}
	if len(device.GhostRxGroupIDs) > 0 {
		return device.GhostRxGroupIDs
	}
	if device.GroupID > 0 {
		return []int{device.GroupID}
	}
	return nil
}

func validateUDPGhostRouting(routing ghostsession.Routing) error {
	for _, groupID := range routing.RxGroupIDs {
		if groupID <= 0 || uint64(groupID) > uint64(^uint32(0)) {
			return fmt.Errorf("%w: UDP group id is outside uint32 range", ghostsession.ErrInvalidRouting)
		}
	}
	return nil
}

func copyGhostRegistration(target, source *models.Device) {
	if target == nil || source == nil {
		return
	}
	target.Username = source.Username
	target.CallSign = source.CallSign
	target.Nickname = source.Nickname
	target.OwnerID = source.OwnerID
	target.SSID = source.SSID
	target.CallSignSSID = source.CallSignSSID
	target.DevModel = source.DevModel
	target.GroupID = source.GroupID
	target.Priority = source.Priority
	target.Status = source.Status
	target.ISOnline = source.ISOnline
	target.DisableSend = source.DisableSend
	target.DisableRecv = source.DisableRecv
	target.UDPAddr = source.UDPAddr
	target.LastPacketTime = source.LastPacketTime
	target.OnlineTime = source.OnlineTime
	target.GhostSessionID = source.GhostSessionID
	target.GhostSessionTag = source.GhostSessionTag
	target.ClientInstanceID = source.ClientInstanceID
	target.GhostRxGroupIDs = append(target.GhostRxGroupIDs[:0], source.GhostRxGroupIDs...)
	target.GhostProtocolVersion = source.GhostProtocolVersion
	target.GhostCapabilities = append(target.GhostCapabilities[:0], source.GhostCapabilities...)
}

// Register preserves the legacy username+SSID single-slot behavior.
func (m *UDPGhostManager) Register(device *models.Device) *models.Device {
	if device == nil {
		return nil
	}
	key := getDeviceKey(device.Username, device.SSID)
	m.mu.Lock()
	m.ensureMapsLocked()
	cacheChanged := true
	if existing := m.devices[key]; existing != nil {
		oldGroupID := existing.GroupID
		cacheChanged = oldGroupID != device.GroupID || !sameUDPAddr(existing.UDPAddr, device.UDPAddr) ||
			existing.ISOnline != device.ISOnline || existing.DisableRecv != device.DisableRecv
		removeGhostFromGroupIndex(m.groupDevices, oldGroupID, key)
		copyGhostRegistration(existing, device)
		device = existing
	} else {
		m.devices[key] = device
	}
	device.GhostRxGroupIDs = []int{device.GroupID}
	addGhostToGroupIndex(m.groupDevices, device.GroupID, key, device)
	m.mu.Unlock()
	if cacheChanged {
		InvalidateDomainReceiverCache()
	}
	log.Printf("[UDP-GHOST] legacy device registered: key=%s group=%d", key, device.GroupID)
	return device
}

// RegisterSession publishes one exact authenticated UDP ghost session.
func (m *UDPGhostManager) RegisterSession(device *models.Device) (*models.Device, error) {
	if device == nil || strings.TrimSpace(device.GhostSessionID) == "" {
		return nil, errUDPGhostSessionRequired
	}
	if device.GhostSessionTag == 0 {
		return nil, errUDPGhostSessionTag
	}
	routing, err := ghostsession.NormalizeRouting(ghostsession.Routing{
		TxGroupID: device.GroupID, RxGroupIDs: device.GhostRxGroupIDs,
	}, ghostsession.MaxSubscriptions())
	if err != nil {
		return nil, err
	}
	if err := validateUDPGhostRouting(routing); err != nil {
		return nil, err
	}
	addr, addrOK := udpAddrPort(device.UDPAddr)
	if !addrOK {
		return nil, errors.New("udp ghost endpoint is required")
	}
	device.GroupID = routing.TxGroupID
	device.GhostRxGroupIDs = append([]int(nil), routing.RxGroupIDs...)
	key := sessionDeviceKey(device.GhostSessionID)

	m.mu.Lock()
	m.ensureMapsLocked()
	if otherSessionID := m.sessionTags[device.GhostSessionTag]; otherSessionID != "" && otherSessionID != device.GhostSessionID {
		m.mu.Unlock()
		return nil, errUDPGhostSessionTag
	}
	if otherSessionID := m.addressSessions[addr]; otherSessionID != "" && otherSessionID != device.GhostSessionID {
		m.mu.Unlock()
		return nil, errUDPGhostEndpointInUse
	}
	if existing := m.sessions[device.GhostSessionID]; existing != nil {
		m.removeSessionLocked(device.GhostSessionID)
	}
	m.sessions[device.GhostSessionID] = device
	m.sessionTags[device.GhostSessionTag] = device.GhostSessionID
	m.addressSessions[addr] = device.GhostSessionID
	for _, groupID := range device.GhostRxGroupIDs {
		addGhostToGroupIndex(m.groupDevices, groupID, key, device)
	}
	if device.GhostProtocolVersion == 0 {
		m.devices[getDeviceKey(device.Username, device.SSID)] = device
	}
	m.mu.Unlock()
	InvalidateDomainReceiverCache()
	log.Printf("[UDP-GHOST] session registered: session=%s user=%d tx=%d rx=%v legacy=%v",
		device.GhostSessionID, device.OwnerID, device.GroupID, device.GhostRxGroupIDs, device.GhostProtocolVersion == 0)
	return device, nil
}

func (m *UDPGhostManager) Get(username string, ssid byte) *models.Device {
	m.mu.RLock()
	device := m.devices[getDeviceKey(username, ssid)]
	m.mu.RUnlock()
	return device
}

func (m *UDPGhostManager) GetSession(sessionID string) *models.Device {
	m.mu.RLock()
	device := m.sessions[sessionID]
	m.mu.RUnlock()
	return device
}

func (m *UDPGhostManager) FindBySessionTag(tag uint32) *models.Device {
	if tag == 0 {
		return nil
	}
	m.mu.RLock()
	device := m.sessions[m.sessionTags[tag]]
	m.mu.RUnlock()
	return device
}

func (m *UDPGhostManager) GetByUsername(username string) []*models.Device {
	m.mu.RLock()
	seen := make(map[*models.Device]struct{})
	result := make([]*models.Device, 0)
	for _, device := range m.sessions {
		if device != nil && device.Username == username {
			seen[device] = struct{}{}
			result = append(result, device)
		}
	}
	for _, device := range m.devices {
		if device != nil && device.Username == username {
			if _, exists := seen[device]; !exists {
				result = append(result, device)
			}
		}
	}
	m.mu.RUnlock()
	return result
}

func (m *UDPGhostManager) GetByGroup(groupID int) []*models.Device {
	m.mu.RLock()
	group := m.groupDevices[groupID]
	result := make([]*models.Device, 0, len(group))
	for _, device := range group {
		if device != nil && device.ISOnline {
			result = append(result, device)
		}
	}
	m.mu.RUnlock()
	return result
}

func (m *UDPGhostManager) ForEachOnlineByGroup(groupID int, fn func(*models.Device)) {
	if fn == nil {
		return
	}
	m.mu.RLock()
	for _, device := range m.groupDevices[groupID] {
		if device != nil && device.ISOnline {
			fn(device)
		}
	}
	m.mu.RUnlock()
}

func (m *UDPGhostManager) removeSessionLocked(sessionID string) *models.Device {
	device := m.sessions[sessionID]
	if device == nil {
		return nil
	}
	delete(m.sessions, sessionID)
	if m.sessionTags[device.GhostSessionTag] == sessionID {
		delete(m.sessionTags, device.GhostSessionTag)
	}
	if addr, ok := udpAddrPort(device.UDPAddr); ok && m.addressSessions[addr] == sessionID {
		delete(m.addressSessions, addr)
	}
	key := sessionDeviceKey(sessionID)
	for _, groupID := range ghostReceiveGroups(device) {
		removeGhostFromGroupIndex(m.groupDevices, groupID, key)
	}
	legacyKey := getDeviceKey(device.Username, device.SSID)
	if m.devices[legacyKey] == device {
		delete(m.devices, legacyKey)
	}
	return device
}

func (m *UDPGhostManager) RemoveSession(sessionID string) *models.Device {
	m.mu.Lock()
	m.ensureMapsLocked()
	removed := m.removeSessionLocked(sessionID)
	m.mu.Unlock()
	if removed != nil {
		InvalidateDomainReceiverCache()
		log.Printf("[UDP-GHOST] session removed: session=%s user=%d", sessionID, removed.OwnerID)
	}
	return removed
}

func (m *UDPGhostManager) Remove(username string, ssid byte) {
	key := getDeviceKey(username, ssid)
	m.mu.Lock()
	m.ensureMapsLocked()
	device := m.devices[key]
	if device != nil && device.GhostSessionID != "" {
		m.removeSessionLocked(device.GhostSessionID)
	} else if device != nil {
		delete(m.devices, key)
		removeGhostFromGroupIndex(m.groupDevices, device.GroupID, key)
	}
	m.mu.Unlock()
	if device != nil {
		InvalidateDomainReceiverCache()
	}
}

func (m *UDPGhostManager) RemoveByUDPAddr(addr string) {
	removed := make([]*models.Device, 0)
	m.mu.Lock()
	m.ensureMapsLocked()
	for sessionID, device := range m.sessions {
		if device != nil && device.UDPAddr != nil && device.UDPAddr.String() == addr {
			removed = append(removed, m.removeSessionLocked(sessionID))
		}
	}
	for key, device := range m.devices {
		if device != nil && device.GhostSessionID == "" && device.UDPAddr != nil && device.UDPAddr.String() == addr {
			delete(m.devices, key)
			removeGhostFromGroupIndex(m.groupDevices, device.GroupID, key)
			removed = append(removed, device)
		}
	}
	m.mu.Unlock()
	for _, device := range removed {
		if device == nil {
			continue
		}
		RevokeCenterLocalDevice(device)
		if device.GhostSessionID != "" {
			ghostsession.Global.Remove(device.GhostSessionID)
		}
	}
	if len(removed) > 0 {
		InvalidateDomainReceiverCache()
	}
}

func (m *UDPGhostManager) GetAll() []*models.Device {
	m.mu.RLock()
	seen := make(map[*models.Device]struct{})
	result := make([]*models.Device, 0, len(m.sessions)+len(m.devices))
	for _, device := range m.sessions {
		if device != nil {
			seen[device] = struct{}{}
			result = append(result, device)
		}
	}
	for _, device := range m.devices {
		if device != nil {
			if _, exists := seen[device]; !exists {
				result = append(result, device)
			}
		}
	}
	m.mu.RUnlock()
	return result
}

// FindBySSIDAndAddr supports diagnostics and legacy callers. Packet
// authentication uses the stricter tag or legacy-only methods below.
func (m *UDPGhostManager) FindBySSIDAndAddr(ssid byte, addr *net.UDPAddr) *models.Device {
	if addr == nil {
		return nil
	}
	m.mu.RLock()
	if ap, ok := udpAddrPort(addr); ok {
		if device := m.sessions[m.addressSessions[ap]]; device != nil && device.SSID == ssid {
			m.mu.RUnlock()
			return device
		}
	}
	for _, device := range m.devices {
		if device != nil && device.SSID == ssid && sameUDPAddr(device.UDPAddr, addr) {
			m.mu.RUnlock()
			return device
		}
	}
	m.mu.RUnlock()
	return nil
}

func (m *UDPGhostManager) FindLegacyBySSIDAndAddr(ssid byte, addr *net.UDPAddr) *models.Device {
	if addr == nil {
		return nil
	}
	m.mu.RLock()
	for _, device := range m.devices {
		if device != nil && device.GhostProtocolVersion == 0 && device.SSID == ssid && sameUDPAddr(device.UDPAddr, addr) {
			m.mu.RUnlock()
			return device
		}
	}
	m.mu.RUnlock()
	return nil
}

func (m *UDPGhostManager) GetOnlineCount() int {
	_, online := m.GetStats()
	return online
}

func (m *UDPGhostManager) CheckTimeout(timeout time.Duration) {
	now := time.Now()
	expired := make([]*models.Device, 0)
	m.mu.Lock()
	m.ensureMapsLocked()
	for sessionID, device := range m.sessions {
		if device != nil && now.Sub(device.LastPacketTime) > timeout {
			expired = append(expired, m.removeSessionLocked(sessionID))
		}
	}
	for key, device := range m.devices {
		if device != nil && device.GhostSessionID == "" && now.Sub(device.LastPacketTime) > timeout {
			delete(m.devices, key)
			removeGhostFromGroupIndex(m.groupDevices, device.GroupID, key)
			expired = append(expired, device)
		}
	}
	m.mu.Unlock()
	for _, device := range expired {
		if device == nil {
			continue
		}
		log.Printf("[UDP-GHOST] session timed out: session=%s user=%d", device.GhostSessionID, device.OwnerID)
		RevokeCenterLocalDevice(device)
		if device.GhostSessionID != "" {
			ghostsession.Global.Remove(device.GhostSessionID)
		}
	}
	if len(expired) > 0 {
		InvalidateDomainReceiverCache()
	}
}

func (m *UDPGhostManager) UpdateActivity(username string, ssid byte, addr *net.UDPAddr) {
	key := getDeviceKey(username, ssid)
	m.mu.Lock()
	device := m.devices[key]
	cacheChanged := false
	if device != nil {
		device.LastPacketTime = time.Now()
		if addr != nil {
			cacheChanged = !sameUDPAddr(device.UDPAddr, addr)
			device.UDPAddr = addr
		}
	}
	m.mu.Unlock()
	if cacheChanged {
		InvalidateDomainReceiverCache()
	}
}

func (m *UDPGhostManager) UpdateSessionActivity(sessionID string, now time.Time) bool {
	if now.IsZero() {
		now = time.Now()
	}
	m.mu.Lock()
	device := m.sessions[sessionID]
	if device != nil {
		device.LastPacketTime = now
	}
	m.mu.Unlock()
	if device != nil {
		ghostsession.Global.UpdateActivity(sessionID, "", now)
		return true
	}
	return false
}

func (m *UDPGhostManager) SetSessionRouting(sessionID string, routing ghostsession.Routing) error {
	routing, err := ghostsession.NormalizeRouting(routing, ghostsession.MaxSubscriptions())
	if err != nil {
		return err
	}
	if err := validateUDPGhostRouting(routing); err != nil {
		return err
	}
	m.mu.Lock()
	device := m.sessions[sessionID]
	if device == nil {
		m.mu.Unlock()
		return ghostsession.ErrSessionNotFound
	}
	key := sessionDeviceKey(sessionID)
	for _, groupID := range ghostReceiveGroups(device) {
		removeGhostFromGroupIndex(m.groupDevices, groupID, key)
	}
	device.GroupID = routing.TxGroupID
	device.GhostRxGroupIDs = append([]int(nil), routing.RxGroupIDs...)
	for _, groupID := range routing.RxGroupIDs {
		addGhostToGroupIndex(m.groupDevices, groupID, key, device)
	}
	m.mu.Unlock()
	InvalidateDomainReceiverCache()
	return nil
}

func (m *UDPGhostManager) SetDeviceGroup(username string, ssid byte, groupID int) error {
	key := getDeviceKey(username, ssid)
	m.mu.RLock()
	device := m.devices[key]
	sessionID := ""
	if device != nil {
		sessionID = device.GhostSessionID
	}
	m.mu.RUnlock()
	if device == nil {
		return fmt.Errorf("device not found: %s", key)
	}
	if sessionID != "" {
		return m.SetSessionRouting(sessionID, ghostsession.Routing{TxGroupID: groupID, RxGroupIDs: []int{groupID}})
	}
	m.mu.Lock()
	removeGhostFromGroupIndex(m.groupDevices, device.GroupID, key)
	device.GroupID = groupID
	device.GhostRxGroupIDs = []int{groupID}
	addGhostToGroupIndex(m.groupDevices, groupID, key, device)
	m.mu.Unlock()
	InvalidateDomainReceiverCache()
	return nil
}

func IsGhostDevice(device *models.Device) bool {
	return device != nil && protocol.IsGhostDevModel(device.DevModel)
}

func (m *UDPGhostManager) GetDeviceByKey(key string) *models.Device {
	m.mu.RLock()
	device := m.devices[key]
	m.mu.RUnlock()
	return device
}

func (m *UDPGhostManager) GetStats() (total int, online int) {
	m.mu.RLock()
	seen := make(map[*models.Device]struct{})
	for _, device := range m.sessions {
		if device != nil {
			seen[device] = struct{}{}
		}
	}
	for _, device := range m.devices {
		if device != nil {
			seen[device] = struct{}{}
		}
	}
	for device := range seen {
		total++
		if device.ISOnline {
			online++
		}
	}
	m.mu.RUnlock()
	return total, online
}

func (m *UDPGhostManager) UpdateUserCallSign(ownerID int, username, newCallSign string) {
	if ownerID <= 0 && username == "" {
		return
	}
	m.mu.Lock()
	seen := make(map[*models.Device]struct{})
	for _, device := range m.sessions {
		seen[device] = struct{}{}
	}
	for _, device := range m.devices {
		seen[device] = struct{}{}
	}
	for device := range seen {
		if device == nil || (ownerID > 0 && device.OwnerID != ownerID) || (ownerID <= 0 && device.Username != username) {
			continue
		}
		device.CallSign = newCallSign
		device.CallSignSSID = protocol.GetCallSignSSID(newCallSign, device.SSID)
	}
	m.mu.Unlock()
}
