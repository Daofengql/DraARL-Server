package udphub

import (
	"testing"
	"time"
)

func TestPendingDeviceCodeExpiresAfterFiveMinutes(t *testing.T) {
	manager := newPendingDeviceManager()
	defer manager.Stop()

	startedAt := time.Now()
	device, err := manager.RequestCode("AA:BB:CC:DD:EE:FF", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}

	duration := device.CodeExpires.Sub(startedAt)
	if duration < 5*time.Minute || duration > 5*time.Minute+time.Second {
		t.Fatalf("code duration = %s, want approximately 5m", duration)
	}
}

func TestBindingRetriesAndDeliverySurviveCleanup(t *testing.T) {
	manager := newPendingDeviceManager()
	defer manager.Stop()
	device, err := manager.RequestCode("AA:BB:CC:DD:EE:FF", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.BindDevice(device.Code, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.BindDevice(device.Code, 1); err != nil {
		t.Fatalf("lost response retry failed: %v", err)
	}
	if _, err := manager.BindDevice(device.Code, 2); err == nil {
		t.Fatal("another user claimed consumed code")
	}
	config := &PendingDeviceConfig{Username: "owner", DevicePassword: "test", SSID: 1}
	if err := manager.SetDeviceConfig(device.MAC, 2, config); err == nil {
		t.Fatal("another user configured bound device")
	}
	if err := manager.SetDeviceConfig(device.MAC, 1, config); err != nil {
		t.Fatal(err)
	}
	manager.cleanupExpired(time.Now().Add(time.Minute))
	pending, err := manager.ConfirmBind(device.MAC)
	if err != nil || !pending.ConfigReady || pending.Config.SSID != 1 {
		t.Fatalf("undelivered configuration was removed: %v", err)
	}
	pending.Config.SSID = 99
	pending.Bound = false
	config.Username = "modified"
	again, err := manager.ConfirmBind(device.MAC)
	if err != nil || !again.Bound || again.Config.SSID != 1 || again.Config.Username != "owner" {
		t.Fatal("manager exposed mutable session state")
	}
	request, err := manager.RequestCode(device.MAC, "127.0.0.1")
	if err != nil || !request.Bound {
		t.Fatal("repeated code request lost accepted bind")
	}
	manager.cleanupExpired(time.Now().Add(11 * time.Minute))
	if _, err := manager.ConfirmBind(device.MAC); err == nil {
		t.Fatal("expired session retained")
	}
}

func TestRenewedCodeRemovesOldMapping(t *testing.T) {
	manager := newPendingDeviceManager()
	defer manager.Stop()
	old, err := manager.RequestCode("AA:BB:CC:DD:EE:FF", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	manager.byMAC[old.MAC].CodeExpires = time.Now().Add(-time.Second)
	manager.mu.Unlock()
	if _, err := manager.ConfirmBind(old.MAC); err == nil {
		t.Fatal("expired unbound code accepted")
	}
	renewed, err := manager.RequestCode(old.MAC, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := manager.GetByCode(old.Code); exists && renewed.Code != old.Code {
		t.Fatal("stale code mapping retained after refresh")
	}
}
