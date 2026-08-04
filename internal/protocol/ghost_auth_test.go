package protocol

import (
	"encoding/json"
	"testing"
)

func TestDecodeGhostAuthRequestSupportsLegacyAndVersionedPayloads(t *testing.T) {
	legacy, isLegacy, err := DecodeGhostAuthRequest([]byte("legacy.jwt.token"))
	if err != nil || !isLegacy || legacy.Token != "legacy.jwt.token" {
		t.Fatalf("legacy=%#v isLegacy=%v err=%v", legacy, isLegacy, err)
	}

	wire, err := json.Marshal(GhostAuthRequest{
		Version: GhostAuthPayloadVersion, Token: "new.jwt.token",
		ClientInstanceID: "11111111-1111-4111-8111-111111111111",
		Capabilities:     []string{"multi_receive_v1", "source_group_v1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	request, isLegacy, err := DecodeGhostAuthRequest(wire)
	if err != nil || isLegacy || request.ClientInstanceID == "" || len(request.Capabilities) != 2 {
		t.Fatalf("request=%#v isLegacy=%v err=%v", request, isLegacy, err)
	}
}

func TestDecodeGhostAuthRequestRejectsVersionedPayloadWithoutInstanceID(t *testing.T) {
	wire, err := json.Marshal(GhostAuthRequest{Version: GhostAuthPayloadVersion, Token: "new.jwt.token"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := DecodeGhostAuthRequest(wire); err == nil {
		t.Fatal("versioned authentication without client_instance_id was accepted")
	}
}

func TestDecodeGhostAuthRequestRejectsTrailingJSON(t *testing.T) {
	wire := []byte(`{"version":1,"token":"token","client_instance_id":"11111111-1111-4111-8111-111111111111"}{}`)
	if _, _, err := DecodeGhostAuthRequest(wire); err == nil {
		t.Fatal("authentication payload with trailing JSON was accepted")
	}
}

func TestGhostAuthSuccessKeepsLegacyStatusByte(t *testing.T) {
	wire, err := EncodeGhostAuthSuccessData(GhostAuthSuccess{
		SessionID: "session-1", SessionTag: 42,
		ClientInstanceID: "11111111-1111-4111-8111-111111111111",
		TxGroupID:        999, RxGroupIDs: []int{999, 1001},
	})
	if err != nil {
		t.Fatal(err)
	}
	if wire[0] != JWTAuthSuccess {
		t.Fatalf("legacy status=%d", wire[0])
	}
	decoded, err := DecodeGhostAuthSuccessData(wire)
	if err != nil || decoded.SessionTag != 42 || len(decoded.RxGroupIDs) != 2 {
		t.Fatalf("decoded=%#v err=%v", decoded, err)
	}
}

func TestReservedUint32RoundTrip(t *testing.T) {
	packet := EncodeDraARLv1("alice", "", SSIDGhostAndroid, DraARLTypeHeartbeat, DraARLDevModelAndroid, 0, "", nil)
	updated, ok := WithReservedUint32(packet, 0x10203040)
	if !ok || ReservedUint32(updated[DraARLv1ReservedOffset:DraARLv1HeaderSize]) != 0x10203040 {
		t.Fatalf("updated=%v ok=%v", updated, ok)
	}
	if ReservedUint32(packet[DraARLv1ReservedOffset:DraARLv1HeaderSize]) != 0 {
		t.Fatal("input packet was mutated")
	}
}
