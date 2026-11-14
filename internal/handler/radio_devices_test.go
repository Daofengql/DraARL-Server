package handler

import (
	"encoding/json"
	"testing"

	"draarl/internal/models"
	ws "draarl/pkg/websocket"
)

func TestRadioDeviceKeysDistinguishSessionsWithTheSameOwnerAndDeviceID(t *testing.T) {
	physical := &models.Device{ID: 7, OwnerID: 42, GroupID: 1001, ISOnline: true}
	ghost := &models.Device{OwnerID: 42, GroupID: 1001, GhostSessionID: "android", ISOnline: true}
	first := &ws.WSDevice{SessionID: "web-a", UserID: 42, GroupID: 1001, DeviceType: ws.DeviceTypeGhost}
	second := &ws.WSDevice{SessionID: "web-b", UserID: 42, GroupID: 1001, DeviceType: ws.DeviceTypeGhost}
	devices := radioGroupDevices([]*models.Device{physical, physical}, []*models.Device{ghost, ghost}, []*ws.WSDevice{first, second, first})
	if len(devices) != 4 {
		t.Fatalf("devices=%#v, want four unique runtime devices", devices)
	}
	seen := make(map[string]bool)
	for _, device := range devices {
		if device.Key == "" || seen[device.Key] {
			t.Fatalf("duplicate/empty key=%q", device.Key)
		}
		seen[device.Key] = true
	}
	for _, key := range []string{"udp-7", "udp-ghost-android", "ws-ghost-session-web-a", "ws-ghost-session-web-b"} {
		if !seen[key] {
			t.Fatalf("missing key=%q", key)
		}
	}
	encoded, err := json.Marshal(devices)
	if err != nil {
		t.Fatal(err)
	}
	var response []map[string]any
	if err := json.Unmarshal(encoded, &response); err != nil {
		t.Fatal(err)
	}
	for _, device := range response {
		if device["key"] == nil {
			t.Fatalf("key missing from JSON: %v", device)
		}
	}
}
