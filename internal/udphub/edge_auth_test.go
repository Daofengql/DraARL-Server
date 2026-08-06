package udphub

import (
	"encoding/json"
	"testing"

	"draarl/internal/protocol"
)

func TestProxiedGhostAuthenticationRejectsUnsupportedProtocolBeforeTokenValidation(t *testing.T) {
	encode := func(payload []byte) []byte {
		return protocol.EncodeDraARLv1(
			"alice", "", protocol.SSIDGhostAndroid, protocol.DraARLTypeJWTAuth,
			protocol.DraARLDevModelAndroid, 0, "", payload,
		)
	}

	if result := AuthenticateProxiedDevice("192.0.2.1", encode([]byte("raw.jwt.token"))); result.Success || result.Error != "ghost_protocol_upgrade_required" {
		t.Fatalf("raw JWT result=%#v", result)
	}

	payload, err := json.Marshal(protocol.GhostAuthRequest{
		Version:          protocol.GhostAuthPayloadVersion,
		Token:            "token-is-not-evaluated",
		ClientInstanceID: "11111111-1111-4111-8111-111111111111",
		Capabilities:     []string{"multi_receive_v1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result := AuthenticateProxiedDevice("192.0.2.1", encode(payload), ProxiedDeviceAuthOptions{AllowGhostMultiSession: true})
	if result.Success || result.Error != "ghost_capabilities_required" {
		t.Fatalf("missing capability result=%#v", result)
	}
}
