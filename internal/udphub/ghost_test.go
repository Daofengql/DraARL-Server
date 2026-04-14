package udphub

import (
	"net"
	"testing"
	"time"

	"draarl/internal/models"
	"draarl/internal/protocol"
)

func TestUDPGhostManagerRegisterRefreshesExistingDevice(t *testing.T) {
	manager := &UDPGhostManager{
		devices:      make(map[string]*models.Device),
		groupDevices: make(map[int]map[string]*models.Device),
	}

	original := &models.Device{
		Username:       "alice",
		CallSign:       "BG7OLD",
		OwnerID:        1,
		SSID:           protocol.SSIDGhostAndroid,
		CallSignSSID:   protocol.GetCallSignSSID("BG7OLD", protocol.SSIDGhostAndroid),
		GroupID:        1,
		ISOnline:       true,
		UDPAddr:        &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 30001},
		LastPacketTime: time.Now(),
	}
	if got := manager.Register(original); got != original {
		t.Fatal("expected first registration to store original pointer")
	}

	refreshed := &models.Device{
		Username:       "alice",
		CallSign:       "BG7NEW",
		OwnerID:        2,
		SSID:           protocol.SSIDGhostAndroid,
		CallSignSSID:   protocol.GetCallSignSSID("BG7NEW", protocol.SSIDGhostAndroid),
		GroupID:        2,
		ISOnline:       true,
		UDPAddr:        &net.UDPAddr{IP: net.ParseIP("127.0.0.2"), Port: 30002},
		LastPacketTime: time.Now(),
	}

	got := manager.Register(refreshed)
	if got != original {
		t.Fatal("expected refresh registration to reuse existing device pointer")
	}
	if got.CallSign != "BG7NEW" || got.OwnerID != 2 || got.GroupID != 2 {
		t.Fatalf("expected device fields refreshed, got %#v", got)
	}
	if len(manager.devices) != 1 {
		t.Fatalf("expected single logical ghost device, got %d", len(manager.devices))
	}
	if _, ok := manager.groupDevices[1]; ok {
		t.Fatal("expected old group index removed after group switch")
	}
	if manager.groupDevices[2][getDeviceKey("alice", protocol.SSIDGhostAndroid)] != original {
		t.Fatal("expected new group index to point at refreshed device")
	}
}
