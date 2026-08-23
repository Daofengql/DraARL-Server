package udphub

import (
	"errors"
	"fmt"
	"log"
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

// UDPGhostManager indexes every UDP ghost by its exact authenticated session.
type UDPGhostManager struct {
	sessions        map[string]*models.Device
	sessionTags     map[uint32]string
	addressSessions map[netip.AddrPort]string

	// Receive index. One session can be present in many groups.
	groupDevices map[int]map[string]*models.Device

	mu sync.RWMutex
}

var GlobalUDPGhostManager = newUDPGhostManager()

func newUDPGhostManager() *UDPGhostManager {
	return &UDPGhostManager{
		sessions:    make(map[string]*models.Device),
		sessionTags: make(map[uint32]string), addressSessions: make(map[netip.AddrPort]string),
		groupDevices: make(map[int]map[string]*models.Device),
	}
}

func sessionDeviceKey(sessionID string) string {
	return "session:" + sessionID
}

func (m *UDPGhostManager) ensureMapsLocked() {
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
	state := device.RuntimeSnapshot()
	if len(state.GhostRxGroupIDs) > 0 {
		return state.GhostRxGroupIDs
	}
	if state.GroupID > 0 {
		return []int{state.GroupID}
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

// RegisterSession publishes one exact authenticated UDP ghost session.
func (m *UDPGhostManager) RegisterSession(device *models.Device) (*models.Device, error) {
	if device == nil {
		return nil, errUDPGhostSessionRequired
	}
	state := device.RuntimeSnapshot()
	if strings.TrimSpace(state.GhostSessionID) == "" {
		return nil, errUDPGhostSessionRequired
	}
	if state.GhostSessionTag == 0 {
		return nil, errUDPGhostSessionTag
	}
	routing, err := ghostsession.NormalizeRouting(ghostsession.Routing{
		TxGroupID: state.GroupID, RxGroupIDs: state.GhostRxGroupIDs,
	}, ghostsession.MaxSubscriptions())
	if err != nil {
		return nil, err
	}
	if err := validateUDPGhostRouting(routing); err != nil {
		return nil, err
	}
	addr, addrOK := udpAddrPort(state.UDPAddr)
	if !addrOK {
		return nil, errors.New("udp ghost endpoint is required")
	}
	device.UpdateRuntime(func(current *models.Device) {
		current.GroupID = routing.TxGroupID
		current.GhostRxGroupIDs = append([]int(nil), routing.RxGroupIDs...)
	})
	state = device.RuntimeSnapshot()
	key := sessionDeviceKey(state.GhostSessionID)

	m.mu.Lock()
	m.ensureMapsLocked()
	if otherSessionID := m.sessionTags[state.GhostSessionTag]; otherSessionID != "" && otherSessionID != state.GhostSessionID {
		m.mu.Unlock()
		return nil, errUDPGhostSessionTag
	}
	if otherSessionID := m.addressSessions[addr]; otherSessionID != "" && otherSessionID != state.GhostSessionID {
		m.mu.Unlock()
		return nil, errUDPGhostEndpointInUse
	}
	if existing := m.sessions[state.GhostSessionID]; existing != nil {
		m.removeSessionLocked(state.GhostSessionID)
	}
	m.sessions[state.GhostSessionID] = device
	m.sessionTags[state.GhostSessionTag] = state.GhostSessionID
	m.addressSessions[addr] = state.GhostSessionID
	for _, groupID := range state.GhostRxGroupIDs {
		addGhostToGroupIndex(m.groupDevices, groupID, key, device)
	}
	m.mu.Unlock()
	InvalidateDomainReceiverCache()
	log.Printf("[UDP-GHOST] session registered: session=%s user=%d tx=%d rx=%v",
		ghostsession.ShortID(state.GhostSessionID), state.OwnerID, state.GroupID, state.GhostRxGroupIDs)
	return device, nil
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
	result := make([]*models.Device, 0)
	for _, device := range m.sessions {
		if device != nil && device.RuntimeSnapshot().Username == username {
			result = append(result, device)
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
		if device != nil && device.RuntimeSnapshot().ISOnline {
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
		if device != nil && device.RuntimeSnapshot().ISOnline {
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
	state := device.RuntimeSnapshot()
	if m.sessionTags[state.GhostSessionTag] == sessionID {
		delete(m.sessionTags, state.GhostSessionTag)
	}
	if addr, ok := udpAddrPort(state.UDPAddr); ok && m.addressSessions[addr] == sessionID {
		delete(m.addressSessions, addr)
	}
	key := sessionDeviceKey(sessionID)
	for _, groupID := range ghostReceiveGroups(device) {
		removeGhostFromGroupIndex(m.groupDevices, groupID, key)
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
		log.Printf("[UDP-GHOST] session removed: session=%s user=%d", ghostsession.ShortID(sessionID), removed.RuntimeSnapshot().OwnerID)
	}
	return removed
}

func (m *UDPGhostManager) RemoveByUDPAddr(addr string) {
	removed := make([]*models.Device, 0)
	m.mu.Lock()
	m.ensureMapsLocked()
	for sessionID, device := range m.sessions {
		state := device.RuntimeSnapshot()
		if device != nil && state.UDPAddr != nil && state.UDPAddr.String() == addr {
			removed = append(removed, m.removeSessionLocked(sessionID))
		}
	}
	m.mu.Unlock()
	for _, device := range removed {
		if device == nil {
			continue
		}
		RevokeCenterLocalDevice(device)
		ghostsession.Global.Remove(device.RuntimeSnapshot().GhostSessionID)
	}
	if len(removed) > 0 {
		InvalidateDomainReceiverCache()
	}
}

func (m *UDPGhostManager) GetAll() []*models.Device {
	m.mu.RLock()
	result := make([]*models.Device, 0, len(m.sessions))
	for _, device := range m.sessions {
		if device != nil {
			result = append(result, device)
		}
	}
	m.mu.RUnlock()
	return result
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
		state := device.RuntimeSnapshot()
		if device != nil && now.Sub(state.LastPacketTime) > timeout {
			expired = append(expired, m.removeSessionLocked(sessionID))
		}
	}
	m.mu.Unlock()
	for _, device := range expired {
		if device == nil {
			continue
		}
		state := device.RuntimeSnapshot()
		log.Printf("[UDP-GHOST] session timed out: session=%s user=%d", ghostsession.ShortID(state.GhostSessionID), state.OwnerID)
		RevokeCenterLocalDevice(device)
		ghostsession.Global.Remove(state.GhostSessionID)
	}
	if len(expired) > 0 {
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
	device.UpdateRuntime(func(current *models.Device) {
		current.LastPacketTime = now
	})
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
	device.UpdateRuntime(func(current *models.Device) {
		current.GroupID = routing.TxGroupID
		current.GhostRxGroupIDs = append([]int(nil), routing.RxGroupIDs...)
	})
	for _, groupID := range routing.RxGroupIDs {
		addGhostToGroupIndex(m.groupDevices, groupID, key, device)
	}
	m.mu.Unlock()
	InvalidateDomainReceiverCache()
	return nil
}

func IsGhostDevice(device *models.Device) bool {
	return device != nil && protocol.IsGhostDevModel(device.RuntimeSnapshot().DevModel)
}

func (m *UDPGhostManager) GetStats() (total int, online int) {
	m.mu.RLock()
	for _, device := range m.sessions {
		if device == nil {
			continue
		}
		state := device.RuntimeSnapshot()
		total++
		if state.ISOnline {
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
	for _, device := range m.sessions {
		state := device.RuntimeSnapshot()
		if device == nil || (ownerID > 0 && state.OwnerID != ownerID) || (ownerID <= 0 && state.Username != username) {
			continue
		}
		device.UpdateRuntime(func(current *models.Device) {
			current.CallSign = newCallSign
			current.CallSignSSID = protocol.GetCallSignSSID(newCallSign, current.SSID)
		})
	}
	m.mu.Unlock()
}
