package websocket

import (
	"net/http"
	"net/http/httptest"
	"testing"

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
