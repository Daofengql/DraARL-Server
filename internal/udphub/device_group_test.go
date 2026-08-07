package udphub

import (
	"net"
	"sync"
	"testing"

	"draarl/internal/models"
)

func newRuntimeGroupForTest(id, groupType int) *models.Group {
	return &models.Group{
		ID:       id,
		Type:     groupType,
		Status:   1,
		DevMap:   make(map[int]*models.Device),
		DevList:  make([]int, 0),
		ConnPool: newConnPool(),
	}
}

func TestChangeDeviceGroupUsesGlobalCacheAndSupportsUngrouped(t *testing.T) {
	oldGroup := newRuntimeGroupForTest(models.GroupIDPublicMin, models.GroupTypeRelay)
	privateGroup := newRuntimeGroupForTest(models.GroupIDPrivate1, models.GroupTypeReserved)
	publicGroupMap = map[int]*models.Group{
		oldGroup.ID:     oldGroup,
		privateGroup.ID: privateGroup,
	}
	globalGroupCacheAtomic.Store(publicGroupMap)
	userList = sync.Map{}

	dev := &models.Device{
		ID:           7,
		OwnerID:      10,
		SSID:         1,
		CallSignSSID: "BG7TEST-1",
		GroupID:      oldGroup.ID,
		ISOnline:     true,
		UDPAddr:      &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 20001},
	}
	attachRuntimeDeviceToGroup(oldGroup, dev)

	if _, err := changeDeviceGroup(dev, privateGroup.ID); err != nil {
		t.Fatalf("change to private group from global cache: %v", err)
	}
	if dev.GroupID != privateGroup.ID || privateGroup.DevMap[dev.ID] != dev {
		t.Fatal("device was not attached to private group")
	}
	if _, exists := oldGroup.DevMap[dev.ID]; exists {
		t.Fatal("device remained in old group")
	}

	if _, err := changeDeviceGroup(dev, 0); err != nil {
		t.Fatalf("change to ungrouped: %v", err)
	}
	if dev.GroupID != 0 {
		t.Fatalf("device group = %d, want 0", dev.GroupID)
	}
	if _, exists := privateGroup.DevMap[dev.ID]; exists {
		t.Fatal("ungrouped device remained in forwarding group")
	}
	pool := getGroupConnPool(privateGroup)
	if pool != nil && len(pool.snapshotConnList()) != 0 {
		t.Fatal("ungrouped device remained in connection pool")
	}
}

func TestCanSendFromDevice(t *testing.T) {
	if canSendFromDevice(nil, 1) {
		t.Fatal("nil device must not be allowed to send")
	}
	if canSendFromDevice(&models.Device{GroupID: 1, DisableSend: true}, 1) {
		t.Fatal("disabled device must not be allowed to send")
	}
	if !canSendFromDevice(&models.Device{GroupID: 1}, 1) {
		t.Fatal("enabled device should be allowed to send")
	}
}

func TestChangeDeviceGroupValidatesDisabledTargetBeforeDetach(t *testing.T) {
	oldGroup := newRuntimeGroupForTest(models.GroupIDPublicMin, models.GroupTypeRelay)
	targetGroup := newRuntimeGroupForTest(1001, models.GroupTypeRelay)
	targetGroup.Status = 0
	publicGroupMap = map[int]*models.Group{oldGroup.ID: oldGroup, targetGroup.ID: targetGroup}
	globalGroupCacheAtomic.Store(publicGroupMap)

	dev := &models.Device{ID: 8, CallSignSSID: "BG7TEST-1", GroupID: oldGroup.ID}
	attachRuntimeDeviceToGroup(oldGroup, dev)
	if _, err := changeDeviceGroup(dev, targetGroup.ID); err == nil {
		t.Fatal("expected disabled group rejection")
	}
	if dev.GroupID != oldGroup.ID || oldGroup.DevMap[dev.ID] != dev {
		t.Fatal("failed target validation detached device from old group")
	}
}

func TestGetOnlineDevicesByGroupUsesUnifiedCacheForLowPrivateID(t *testing.T) {
	privateGroup := newRuntimeGroupForTest(models.GroupIDPrivate1, models.GroupTypeReserved)
	online := &models.Device{ID: 9, GroupID: privateGroup.ID, ISOnline: true}
	offline := &models.Device{ID: 10, GroupID: privateGroup.ID, ISOnline: false}
	privateGroup.DevMap[online.ID] = online
	privateGroup.DevMap[offline.ID] = offline
	publicGroupMap = map[int]*models.Group{privateGroup.ID: privateGroup}
	globalGroupCacheAtomic.Store(publicGroupMap)
	userList = sync.Map{}

	devices := GetOnlineDevicesByGroup(privateGroup.ID)
	if len(devices) != 1 || devices[0] != online {
		t.Fatalf("online devices = %#v, want only device %d", devices, online.ID)
	}
}

func TestGroupDeviceSnapshotsAreSafeDuringConcurrentSwitches(t *testing.T) {
	first := newRuntimeGroupForTest(models.GroupIDPublicMin, models.GroupTypeRelay)
	second := newRuntimeGroupForTest(1001, models.GroupTypeRelay)
	publicGroupMap = map[int]*models.Group{first.ID: first, second.ID: second}
	globalGroupCacheAtomic.Store(publicGroupMap)

	dev := &models.Device{ID: 11, GroupID: first.ID, CallSignSSID: "BG7TEST-1", ISOnline: true}
	attachRuntimeDeviceToGroup(first, dev)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			target := first.ID
			if i%2 == 0 {
				target = second.ID
			}
			if _, err := changeDeviceGroup(dev, target); err != nil {
				t.Errorf("change group during concurrent snapshot: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			_ = GetOnlineDevicesByGroup(first.ID)
			_ = GetAllDevicesByGroup(second.ID)
		}
	}()
	wg.Wait()
}
