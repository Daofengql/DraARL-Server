package udphub

import (
	"net"
	"sync"
	"testing"
	"time"

	"draarl/internal/models"
	"draarl/internal/protocol"
)

func TestShouldRejectNormalDeviceConflict(t *testing.T) {
	now := time.Now()
	addrA := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12000}
	addrB := &net.UDPAddr{IP: net.ParseIP("127.0.0.2"), Port: 12001}

	tests := []struct {
		name string
		dev  *models.Device
		addr *net.UDPAddr
		mac  string
		want bool
	}{
		{name: "nil device", dev: nil, addr: addrB, want: false},
		{
			name: "offline device",
			dev: &models.Device{
				ISOnline:       false,
				LastPacketTime: now,
				UDPAddr:        addrA,
			},
			addr: addrB,
			want: false,
		},
		{
			name: "stale device",
			dev: &models.Device{
				ISOnline:       true,
				LastPacketTime: now.Add(-runtimeDeviceActiveTimeout - time.Second),
				UDPAddr:        addrA,
			},
			addr: addrB,
			want: false,
		},
		{
			name: "same address",
			dev: &models.Device{
				ISOnline:       true,
				LastPacketTime: now,
				UDPAddr:        addrA,
			},
			addr: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12000},
			want: false,
		},
		{
			name: "same mac fast reconnect",
			dev: &models.Device{
				ISOnline:       true,
				LastPacketTime: now,
				UDPAddr:        addrA,
				MAC:            "AA:BB:CC:DD:EE:FF",
			},
			addr: addrB,
			mac:  "aa:bb:cc:dd:ee:ff",
			want: false,
		},
		{
			name: "different active address",
			dev: &models.Device{
				ISOnline:       true,
				LastPacketTime: now,
				UDPAddr:        addrA,
			},
			addr: addrB,
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldRejectNormalDeviceConflict(tc.dev, tc.addr, tc.mac); got != tc.want {
				t.Fatalf("shouldRejectNormalDeviceConflict() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRewriteRuntimeAllowCallSignSSID(t *testing.T) {
	rewritten, changed := rewriteRuntimeAllowCallSignSSID(" BG7OLD-1 , BG7OLD-105,OTHER-3 ", "BG7OLD", "BG7NEW")
	if !changed {
		t.Fatal("expected allow_callsign_ssid rewrite to report changed")
	}
	if rewritten != "BG7NEW-1,BG7NEW-105,OTHER-3" {
		t.Fatalf("unexpected rewritten whitelist: %q", rewritten)
	}
}

func TestSyncUserCallSignChangeUpdatesRuntimeIndexes(t *testing.T) {
	devOwnerSSIDMap = make(map[string]*models.Device)
	devUsernameSSIDMap = make(map[string]*models.Device)
	devCallsignSSIDMap = make(map[string]*models.Device)
	onlineDevMap = make(map[int]*models.Device)
	onlineDevMapDraARL = make(map[int]*models.Device)
	publicGroupMap = make(map[int]*models.Group)
	userList = sync.Map{}
	GlobalUDPGhostManager = &UDPGhostManager{
		devices:      make(map[string]*models.Device),
		groupDevices: make(map[int]map[string]*models.Device),
	}

	dev := &models.Device{
		ID:         7,
		OwnerID:    42,
		Username:   "alice",
		CallSign:   "BG7OLD",
		SSID:       7,
		GroupID:    models.GroupIDPublicMin,
		ISOnline:   true,
		UDPAddr:    &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 20001},
		OnlineTime: time.Now(),
	}
	indexRuntimeDevice(dev)
	onlineDevMap[dev.ID] = dev
	onlineDevMapDraARL[dev.ID] = dev

	gp := &models.Group{
		ID:                models.GroupIDPublicMin,
		AllowCallSignSSID: "BG7OLD-7,OTHER-1",
		DevMap: map[int]*models.Device{
			dev.ID: dev,
		},
		ConnPool: &CurrentConnPool{
			DevConnMap: map[string]*models.Device{
				dev.UDPAddr.String(): dev,
			},
			DevConnList: []*models.Device{dev},
		},
	}
	publicGroupMap[gp.ID] = gp

	userList.Store("BG7OLD", &UserInfo{
		ID:       42,
		Name:     "alice",
		CallSign: "BG7OLD",
	})

	GlobalUDPGhostManager.Register(&models.Device{
		OwnerID:        42,
		Username:       "alice",
		CallSign:       "BG7OLD",
		SSID:           protocol.SSIDGhostAndroid,
		CallSignSSID:   protocol.GetCallSignSSID("BG7OLD", protocol.SSIDGhostAndroid),
		GroupID:        models.GroupIDPublicMin,
		ISOnline:       true,
		LastPacketTime: time.Now(),
	})

	SyncUserCallSignChange(42, "alice", "BG7OLD", "BG7NEW")

	if dev.CallSign != "BG7NEW" {
		t.Fatalf("expected runtime device callsign updated, got %q", dev.CallSign)
	}
	if _, ok := devCallsignSSIDMap[protocol.GetCallSignSSID("BG7OLD", dev.SSID)]; ok {
		t.Fatal("expected old callsign-ssid runtime index removed")
	}
	if got := devCallsignSSIDMap[protocol.GetCallSignSSID("BG7NEW", dev.SSID)]; got != dev {
		t.Fatal("expected new callsign-ssid runtime index rebuilt")
	}
	if gp.AllowCallSignSSID != "BG7NEW-7,OTHER-1" {
		t.Fatalf("expected whitelist rewritten, got %q", gp.AllowCallSignSSID)
	}

	value, ok := userList.Load("BG7NEW")
	if !ok {
		t.Fatal("expected userList new callsign key")
	}
	info, ok := value.(*UserInfo)
	if !ok || info.CallSign != "BG7NEW" {
		t.Fatalf("expected userList callsign updated, got %#v", value)
	}
	if _, ok := userList.Load("BG7OLD"); ok {
		t.Fatal("expected old userList callsign key removed")
	}

	ghost := GlobalUDPGhostManager.Get("alice", protocol.SSIDGhostAndroid)
	if ghost == nil || ghost.CallSign != "BG7NEW" {
		t.Fatalf("expected udp ghost callsign updated, got %#v", ghost)
	}
}
