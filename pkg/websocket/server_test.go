package websocket

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"draarl/internal/ghostsession"
	"draarl/internal/protocol"
)

func TestCheckOriginAllowsConfiguredOrigin(t *testing.T) {
	SetAllowedOrigins([]string{"https://app.example.com"})
	t.Cleanup(func() {
		SetAllowedOrigins(nil)
	})

	req := httptest.NewRequest(http.MethodGet, "https://server.example.com/ws", nil)
	req.Header.Set("Origin", "https://app.example.com")

	if !checkOrigin(req) {
		t.Fatal("expected configured origin to pass websocket origin check")
	}
}

func TestCheckOriginRejectsUnconfiguredServerOrigin(t *testing.T) {
	SetAllowedOrigins([]string{"https://app.example.com"})
	t.Cleanup(func() {
		SetAllowedOrigins(nil)
	})

	req := httptest.NewRequest(http.MethodGet, "https://server.example.com/ws", nil)
	req.Host = "server.example.com"
	req.Header.Set("Origin", "https://server.example.com")

	if checkOrigin(req) {
		t.Fatal("expected unconfigured server origin to be rejected")
	}
}

func TestValidateGhostPreAuthRequiresVersionedSessionProtocol(t *testing.T) {
	valid := &WSPreAuthData{
		ClientInstanceID: "11111111-1111-4111-8111-111111111111",
		ProtocolVersion:  protocol.GhostAuthPayloadVersion,
		Capabilities: []string{
			ghostsession.CapabilityMultiReceiveV1,
			ghostsession.CapabilitySourceGroupV1,
		},
	}
	if instanceID, code := validateGhostPreAuth(valid); code != "" || instanceID != valid.ClientInstanceID {
		t.Fatalf("valid pre-auth instance=%q code=%q", instanceID, code)
	}

	tests := []struct {
		name string
		data WSPreAuthData
		want string
	}{
		{name: "missing version", data: WSPreAuthData{ClientInstanceID: valid.ClientInstanceID, Capabilities: valid.Capabilities}, want: "ghost_protocol_upgrade_required"},
		{name: "missing instance", data: WSPreAuthData{ProtocolVersion: protocol.GhostAuthPayloadVersion, Capabilities: valid.Capabilities}, want: "ghost_protocol_upgrade_required"},
		{name: "invalid instance", data: WSPreAuthData{ProtocolVersion: protocol.GhostAuthPayloadVersion, ClientInstanceID: "device-id", Capabilities: valid.Capabilities}, want: "invalid_client_instance_id"},
		{name: "missing capability", data: WSPreAuthData{ProtocolVersion: protocol.GhostAuthPayloadVersion, ClientInstanceID: valid.ClientInstanceID, Capabilities: []string{ghostsession.CapabilityMultiReceiveV1}}, want: "ghost_capabilities_required"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, code := validateGhostPreAuth(&test.data); code != test.want {
				t.Fatalf("code=%q want=%q", code, test.want)
			}
		})
	}
}

func TestWSDeviceVoiceRateLimitBoundsBurstAndRefill(t *testing.T) {
	device := &WSDevice{}
	now := time.Unix(1_000, 0)
	var accepted atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if device.allowVoiceFrame(now) {
					accepted.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	if got := accepted.Load(); got != int64(wsVoiceRateBurst) {
		t.Fatalf("accepted burst=%d want=%d", got, int64(wsVoiceRateBurst))
	}
	if got := device.voiceRateLimitedCount(); got != 650 {
		t.Fatalf("dropped=%d want=650", got)
	}
	if !device.allowVoiceFrame(now.Add(time.Second)) {
		t.Fatal("expected token refill after one second")
	}
}

func TestWSDeviceWriterStopSignalIsImmediateAndIdempotent(t *testing.T) {
	device := &WSDevice{closeCh: make(chan struct{})}
	stop := device.writerStopChannel()
	device.signalWriterStop(device.closeCh)
	select {
	case <-stop:
	default:
		t.Fatal("writer stop signal was not closed")
	}
	device.signalWriterStop(device.closeCh)
}
