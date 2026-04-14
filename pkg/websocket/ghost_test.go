package websocket

import "testing"

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

	ghost := manager.CreateGhostDevice(conn, 9, "alice", "BG7AAA", "Alice", 10)

	if ghost.SSID != fixedWebGhostSSID {
		t.Fatalf("expected ghost ssid fixed to %d, got %d", fixedWebGhostSSID, ghost.SSID)
	}
	if conn.SSID != fixedWebGhostSSID {
		t.Fatalf("expected websocket device ssid fixed to %d, got %d", fixedWebGhostSSID, conn.SSID)
	}
}
