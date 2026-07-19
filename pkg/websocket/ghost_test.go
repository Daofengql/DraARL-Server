package websocket

import (
	"testing"

	"draarl/internal/gormdb"
	"draarl/internal/models"
)

func TestNewGhostDeviceManagerUsesFixedWebSSID(t *testing.T) {
	manager := NewGhostDeviceManager()
	if manager.fixedSSID != 105 {
		t.Fatalf("expected fixed web ssid 105, got %d", manager.fixedSSID)
	}
}

func TestGhostDeviceManagerUpdateUserCallSignUpdatesConnection(t *testing.T) {
	manager := NewGhostDeviceManager()
	conn := &WSDevice{CallSign: "BG7OLD"}
	manager.devices[7] = &GhostDevice{
		UserID:   7,
		CallSign: "BG7OLD",
		SSID:     105,
		Conn:     conn,
		ISOnline: true,
	}

	manager.UpdateUserCallSign(7, "BG7NEW")

	ghost := manager.devices[7]
	if ghost.CallSign != "BG7NEW" {
		t.Fatalf("expected ghost callsign updated, got %q", ghost.CallSign)
	}
	if conn.CallSign != "BG7NEW" {
		t.Fatalf("expected websocket connection callsign updated, got %q", conn.CallSign)
	}
}

func TestCreateGhostDeviceAlwaysUsesFixedWebSSID(t *testing.T) {
	manager := NewGhostDeviceManager()
	conn := &WSDevice{}

	ghost := manager.CreateGhostDevice(conn, 9, "alice", "BG7AAA", "Alice", 4)

	if ghost.SSID != fixedWebGhostSSID {
		t.Fatalf("expected ghost ssid fixed to %d, got %d", fixedWebGhostSSID, ghost.SSID)
	}
	if conn.SSID != fixedWebGhostSSID {
		t.Fatalf("expected websocket device ssid fixed to %d, got %d", fixedWebGhostSSID, conn.SSID)
	}
	if ghost.GroupID != 4 || conn.GroupID != 4 {
		t.Fatalf("initial group mismatch: ghost=%d connection=%d, want 4", ghost.GroupID, conn.GroupID)
	}
}

func TestCreateGhostDeviceRefreshesExistingConnectionGroup(t *testing.T) {
	manager := NewGhostDeviceManager()
	manager.devices[9] = &GhostDevice{UserID: 9, GroupID: 999, ISOnline: true}
	conn := &WSDevice{}

	ghost := manager.CreateGhostDevice(conn, 9, "alice", "BG7AAA", "Alice", 4)
	if ghost.GroupID != 4 || conn.GroupID != 4 {
		t.Fatalf("refreshed group mismatch: ghost=%d connection=%d, want 4", ghost.GroupID, conn.GroupID)
	}
}

func TestCanUseGroupForWebGhost(t *testing.T) {
	if !canUseGroupForWebGhost(models.GroupIDPublicMin, nil) {
		t.Fatal("system group 999 must remain valid without a database row")
	}
	if !canUseGroupForWebGhost(4, &gormdb.Group{ID: 4, Type: 1, Status: 1}) {
		t.Fatal("enabled entity group should be valid")
	}
	for name, group := range map[string]*gormdb.Group{
		"missing":     nil,
		"disabled":    {ID: 4, Type: 1, Status: 0},
		"virtual":     {ID: 4, Type: 1, Status: 1, IsVirtual: true},
		"unsupported": {ID: 4, Type: 99, Status: 1},
		"wrong id":    {ID: 5, Type: 1, Status: 1},
	} {
		t.Run(name, func(t *testing.T) {
			if canUseGroupForWebGhost(4, group) {
				t.Fatal("invalid group must fall back to 999")
			}
		})
	}
}
