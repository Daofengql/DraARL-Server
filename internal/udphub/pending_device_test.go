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
