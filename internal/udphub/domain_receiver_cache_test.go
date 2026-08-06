package udphub

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"draarl/internal/ghostsession"
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
	GlobalUDPGhostManager = newUDPGhostManager()

	t.Cleanup(func() {
		StopDomainReceiverCache()
		clearDomainReceiverCacheForTest()
		resetHalfDuplexDomainCache()
		globalGroupCacheAtomic.Store(map[int]*models.Group{})
		GlobalUDPGhostManager = newUDPGhostManager()
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
		ID:              -1,
		OwnerID:         1,
		Username:        "ghost-user",
		SSID:            101,
		GroupID:         groupID,
		GhostSessionID:  "cache-session-1",
		GhostSessionTag: 42001,
		GhostRxGroupIDs: []int{groupID},
		ISOnline:        true,
		UDPAddr:         oldAddr,
		LastPacketTime:  time.Now(),
	}
	if _, err := GlobalUDPGhostManager.RegisterSession(ghost); err != nil {
		t.Fatal(err)
	}

	entries := getDomainReceiverEntries(groupID)
	if len(entries) != 1 || entries[0].addr != oldAddr.AddrPort() {
		t.Fatalf("registered ghost not visible immediately: %#v", entries)
	}

	newAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 30002}
	ghost.UDPAddr = newAddr
	if _, err := GlobalUDPGhostManager.RegisterSession(ghost); err != nil {
		t.Fatal(err)
	}
	entries = getDomainReceiverEntries(groupID)
	if len(entries) != 1 || entries[0].addr != newAddr.AddrPort() {
		t.Fatalf("updated ghost address not visible immediately: %#v", entries)
	}

	GlobalUDPGhostManager.RemoveSession(ghost.GhostSessionID)
	if entries = getDomainReceiverEntries(groupID); len(entries) != 0 {
		t.Fatalf("removed ghost remained cached: %#v", entries)
	}
}

func TestGhostRouteChangeDoesNotLeakThroughStalePhysicalPool(t *testing.T) {
	const (
		groupA = 42004
		groupB = 42005
	)
	pool := setupDomainReceiverTest(t, groupA)
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 30003}
	ghost := &models.Device{
		ID:              -2,
		OwnerID:         2,
		Username:        "routing-ghost",
		SSID:            101,
		GroupID:         groupA,
		GhostSessionID:  "routing-session-1",
		GhostSessionTag: 42004,
		GhostRxGroupIDs: []int{groupA},
		ISOnline:        true,
		UDPAddr:         addr,
		LastPacketTime:  time.Now(),
	}
	if _, err := GlobalUDPGhostManager.RegisterSession(ghost); err != nil {
		t.Fatal(err)
	}

	// Reproduce the pre-fix state: a heartbeat placed the ghost in A's
	// physical pool before the session API changed its routing to B.
	syncDeviceConnPool(pool, ghost, addr)
	if entries := getDomainReceiverEntries(groupA); len(entries) != 1 {
		t.Fatalf("initial receiver count = %d, want 1", len(entries))
	}
	if err := GlobalUDPGhostManager.SetSessionRouting(ghost.GhostSessionID, ghostsession.Routing{
		TxGroupID: groupB, RxGroupIDs: []int{groupB},
	}); err != nil {
		t.Fatal(err)
	}
	if entries := getDomainReceiverEntries(groupA); len(entries) != 0 {
		t.Fatalf("ghost routed to B leaked through stale A pool: %#v", entries)
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

func TestDomainReceiverMetricsCountCandidatesAndDeduplication(t *testing.T) {
	const groupID = 42003
	sharedAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 32001}
	physical := &models.Device{
		ID: 11, OwnerID: 12, Username: "physical", SSID: 1, GroupID: groupID,
		ISOnline: true, UDPAddr: sharedAddr,
	}
	setupDomainReceiverTest(t, groupID, physical)
	ghost := &models.Device{
		ID: -12, OwnerID: 13, Username: "ghost", SSID: 101, GroupID: groupID,
		GhostSessionID: "cache-session-2", GhostSessionTag: 42002, GhostRxGroupIDs: []int{groupID},
		ISOnline: true, UDPAddr: sharedAddr, LastPacketTime: time.Now(),
	}
	if _, err := GlobalUDPGhostManager.RegisterSession(ghost); err != nil {
		t.Fatal(err)
	}
	before := GetDomainReceiverCacheStats()
	entries := getDomainReceiverEntries(groupID)
	after := GetDomainReceiverCacheStats()
	if len(entries) != 1 {
		t.Fatalf("deduplicated receiver count=%d want=1", len(entries))
	}
	if after["candidates"]-before["candidates"] != 2 ||
		after["deduplicated"]-before["deduplicated"] != 1 ||
		after["entries_built"]-before["entries_built"] != 1 {
		t.Fatalf("unexpected receiver metrics before=%v after=%v", before, after)
	}
}

func TestDomainReceiverCacheCanRestart(t *testing.T) {
	StopDomainReceiverCache()
	InitDomainReceiverCache()
	StopDomainReceiverCache()
	InitDomainReceiverCache()
	StopDomainReceiverCache()
}
