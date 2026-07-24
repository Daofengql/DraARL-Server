package udphub

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"draarl/internal/models"
)

func clearDomainReceiverCacheForTest() {
	domainReceiverCache.Range(func(key, _ any) bool {
		domainReceiverCache.Delete(key)
		return true
	})
}

func setupDomainReceiverTest(t *testing.T, groupID int, devices ...*models.Device) *CurrentConnPool {
	t.Helper()
	StopDomainReceiverCache()
	clearDomainReceiverCacheForTest()
	atomic.StoreUint64(&domainReceiverGen, 0)
	resetHalfDuplexDomainCache()

	pool := newConnPool()
	pool.mu.Lock()
	for _, dev := range devices {
		if dev != nil && dev.UDPAddr != nil {
			pool.DevConnMap[dev.UDPAddr.String()] = dev
		}
	}
	rebuildDeviceConnListLocked(pool)
	pool.mu.Unlock()

	globalGroupCacheAtomic.Store(map[int]*models.Group{
		groupID: {
			ID:       groupID,
			Status:   1,
			ConnPool: pool,
			DevMap:   make(map[int]*models.Device),
		},
	})
	GlobalUDPGhostManager = &UDPGhostManager{
		devices:      make(map[string]*models.Device),
		groupDevices: make(map[int]map[string]*models.Device),
	}

	t.Cleanup(func() {
		StopDomainReceiverCache()
		clearDomainReceiverCacheForTest()
		resetHalfDuplexDomainCache()
		globalGroupCacheAtomic.Store(map[int]*models.Group{})
		GlobalUDPGhostManager = &UDPGhostManager{
			devices:      make(map[string]*models.Device),
			groupDevices: make(map[int]map[string]*models.Device),
		}
		runtimeIndexMu.Lock()
		devOwnerSSIDMap = make(map[string]*models.Device)
		devUsernameSSIDMap = make(map[string]*models.Device)
		devCallsignSSIDMap = make(map[string]*models.Device)
		onlineDevMap = make(map[int]*models.Device)
		onlineDevMapDraARL = make(map[int]*models.Device)
		runtimeIndexMu.Unlock()
		userList = sync.Map{}
	})
	return pool
}

func TestGhostChangesInvalidateDomainReceiverCache(t *testing.T) {
	const groupID = 42001
	setupDomainReceiverTest(t, groupID)

	if entries := getDomainReceiverEntries(groupID); len(entries) != 0 {
		t.Fatalf("initial receiver count = %d, want 0", len(entries))
	}

	oldAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 30001}
	ghost := &models.Device{
		ID:             -1,
		OwnerID:        1,
		Username:       "ghost-user",
		SSID:           101,
		GroupID:        groupID,
		ISOnline:       true,
		UDPAddr:        oldAddr,
		LastPacketTime: time.Now(),
	}
	GlobalUDPGhostManager.Register(ghost)

	entries := getDomainReceiverEntries(groupID)
	if len(entries) != 1 || entries[0].addr != oldAddr.AddrPort() {
		t.Fatalf("registered ghost not visible immediately: %#v", entries)
	}

	newAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 30002}
	GlobalUDPGhostManager.UpdateActivity(ghost.Username, ghost.SSID, newAddr)
	entries = getDomainReceiverEntries(groupID)
	if len(entries) != 1 || entries[0].addr != newAddr.AddrPort() {
		t.Fatalf("updated ghost address not visible immediately: %#v", entries)
	}

	GlobalUDPGhostManager.Remove(ghost.Username, ghost.SSID)
	if entries = getDomainReceiverEntries(groupID); len(entries) != 0 {
		t.Fatalf("removed ghost remained cached: %#v", entries)
	}
}

func TestDisableRecvInvalidatesDomainReceiverCache(t *testing.T) {
	const groupID = 42002
	dev := &models.Device{
		ID:       7,
		OwnerID:  9,
		Username: "normal-user",
		CallSign: "BG7TEST",
		SSID:     7,
		GroupID:  groupID,
		ISOnline: true,
		UDPAddr:  &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 31001},
	}
	setupDomainReceiverTest(t, groupID, dev)
	indexRuntimeDevice(dev)

	if entries := getDomainReceiverEntries(groupID); len(entries) != 1 {
		t.Fatalf("initial receiver count = %d, want 1", len(entries))
	}
	SyncDeviceCommControlByID(dev.ID, false, true)
	if entries := getDomainReceiverEntries(groupID); len(entries) != 0 {
		t.Fatalf("disabled receiver remained cached: %#v", entries)
	}

	SyncDeviceCommControlByID(dev.ID, false, false)
	if entries := getDomainReceiverEntries(groupID); len(entries) != 1 {
		t.Fatalf("re-enabled receiver count = %d, want 1", len(entries))
	}

	dev.GroupID = groupID + 1
	InvalidateDomainReceiverCache()
	if entries := getDomainReceiverEntries(groupID); len(entries) != 0 {
		t.Fatalf("device from another group leaked through stale pool: %#v", entries)
	}
}

func TestDomainReceiverCacheCanRestart(t *testing.T) {
	StopDomainReceiverCache()
	InitDomainReceiverCache()
	StopDomainReceiverCache()
	InitDomainReceiverCache()
	StopDomainReceiverCache()
}
