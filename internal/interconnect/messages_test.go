package interconnect

import (
	"testing"
	"time"
)

func TestNodeCredentialControlValidation(t *testing.T) {
	credential, err := NewLongTermCredential("edge-test")
	if err != nil {
		t.Fatal(err)
	}
	rotation := NodeCredentialControl{Kind: NodeCredentialKindRotate, Credential: credential, CredentialEpoch: 2, PreviousValidUntilMillis: time.Now().Add(time.Minute).UnixMilli()}
	if err := rotation.Validate("edge-test"); err != nil {
		t.Fatalf("valid rotation rejected: %v", err)
	}
	rotation.Credential = "redacted"
	if err := rotation.Validate("edge-test"); err == nil {
		t.Fatal("credential for the wrong node was accepted")
	}
	result := NodeCredentialControl{Kind: NodeCredentialKindResult, CredentialEpoch: 2, AckForMessageID: 9, Success: true}
	if err := result.Validate("edge-test"); err != nil {
		t.Fatalf("valid rotation result rejected: %v", err)
	}
}

func TestDeviceSessionConfirmValidationRejectsDuplicateIdentity(t *testing.T) {
	base := DeviceSessionConfirmItem{SessionID: 10, SessionEpoch: 2, ControlSessionID: 30, DeviceID: 40, OwnerID: 50, SSID: 1}
	request := DeviceSessionConfirmRequest{RequestID: 1, Sessions: []DeviceSessionConfirmItem{base}}
	if err := request.Validate(); err != nil {
		t.Fatalf("valid session confirmation rejected: %v", err)
	}
	duplicate := base
	duplicate.SessionID = 11
	request.Sessions = append(request.Sessions, duplicate)
	if err := request.Validate(); err == nil {
		t.Fatal("duplicate device confirmation identity was accepted")
	}
	grant := &DeviceGrant{SessionID: 60, SessionEpoch: 3, DeviceID: 40, OwnerID: 50, SSID: 1, ExpiresAtMillis: time.Now().Add(time.Minute).UnixMilli()}
	response := DeviceSessionConfirmResponse{RequestID: 1, Results: []DeviceSessionConfirmResult{{SessionID: 10, SessionEpoch: 2, Success: true, Grant: grant}}}
	if err := response.Validate(); err != nil {
		t.Fatalf("valid session confirmation response rejected: %v", err)
	}
}

func TestDeviceSessionConfirmRequiresModernGhostRecoveryTicket(t *testing.T) {
	item := DeviceSessionConfirmItem{
		SessionID: 10, SessionEpoch: 2, ControlSessionID: 30, OwnerID: 50,
		SSID: 101, DevModel: 101, GhostSessionID: "ghost-session",
		ClientInstanceID: "11111111-1111-4111-8111-111111111111",
	}
	request := DeviceSessionConfirmRequest{RequestID: 1, Sessions: []DeviceSessionConfirmItem{item}}
	if err := request.Validate(); err == nil {
		t.Fatal("ghost confirmation without a recovery ticket was accepted")
	}
	request.Sessions[0].RecoveryTicket = "signed-ticket"
	if err := request.Validate(); err != nil {
		t.Fatalf("modern ghost confirmation rejected: %v", err)
	}
}

func TestDeviceConfigControlValidation(t *testing.T) {
	base := DeviceConfigControl{SessionID: 11, SessionEpoch: 2, DeviceID: 7}
	tests := []struct {
		name    string
		message DeviceConfigControl
		valid   bool
	}{
		{name: "sync", message: func() DeviceConfigControl { m := base; m.Kind = DeviceConfigKindSync; return m }(), valid: true},
		{name: "report", message: func() DeviceConfigControl { m := base; m.Kind, m.Data = DeviceConfigKindReport, []byte{2, 0}; return m }(), valid: true},
		{name: "down", message: func() DeviceConfigControl {
			m := base
			m.Kind, m.Packet = DeviceConfigKindDown, make([]byte, DraARLHeaderSize)
			return m
		}(), valid: true},
		{name: "result", message: func() DeviceConfigControl {
			m := base
			m.Kind, m.AckForMessageID = DeviceConfigKindResult, 99
			m.Success = true
			return m
		}(), valid: true},
		{name: "missing session", message: DeviceConfigControl{Kind: DeviceConfigKindSync, SessionEpoch: 1, DeviceID: 1}},
		{name: "empty report", message: func() DeviceConfigControl { m := base; m.Kind = DeviceConfigKindReport; return m }()},
		{name: "oversized report", message: func() DeviceConfigControl {
			m := base
			m.Kind, m.Data = DeviceConfigKindReport, make([]byte, 711)
			return m
		}()},
		{name: "short down", message: func() DeviceConfigControl {
			m := base
			m.Kind, m.Packet = DeviceConfigKindDown, make([]byte, DraARLHeaderSize-1)
			return m
		}()},
		{name: "result without ack", message: func() DeviceConfigControl { m := base; m.Kind = DeviceConfigKindResult; return m }()},
		{name: "unknown", message: func() DeviceConfigControl { m := base; m.Kind = "other"; return m }()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.message.Validate()
			if test.valid && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("Validate() accepted invalid message")
			}
		})
	}
}

func TestProjectionSnapshotChunksAndCommits(t *testing.T) {
	p := NewProjection(7)
	p.Version = 12
	for i := 1; i <= 3000; i++ {
		p.Devices[uint64(i)] = DeviceRoute{SessionID: uint64(i), DeviceID: i, Username: "operator", DomainID: 3, SessionEpoch: 1}
	}
	begin, chunks, err := SplitProjection(99, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatal("expected chunked snapshot")
	}
	assembler, err := NewSnapshotAssembler(begin)
	if err != nil {
		t.Fatal(err)
	}
	for i := len(chunks) - 1; i >= 0; i-- {
		if err := assembler.Add(chunks[i]); err != nil {
			t.Fatal(err)
		}
	}
	got, err := assembler.Commit(SnapshotCommit{SnapshotID: 99})
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != p.Version || len(got.Devices) != len(p.Devices) {
		t.Fatalf("snapshot mismatch: version=%d devices=%d", got.Version, len(got.Devices))
	}
}

func TestSnapshotAssemblerRejectsOversizedAndConflictingChunks(t *testing.T) {
	p := NewProjection(7)
	p.Version = 1
	begin, chunks, err := SplitProjection(9, p)
	if err != nil {
		t.Fatal(err)
	}
	invalidBegin := begin
	invalidBegin.Chunks++
	if _, err := NewSnapshotAssembler(invalidBegin); err == nil {
		t.Fatal("snapshot accepted a chunk count inconsistent with total bytes")
	}
	invalidBegin = begin
	invalidBegin.Checksum += "00"
	if _, err := NewSnapshotAssembler(invalidBegin); err == nil {
		t.Fatal("snapshot accepted a checksum with an invalid length")
	}
	assembler, err := NewSnapshotAssembler(begin)
	if err != nil {
		t.Fatal(err)
	}
	oversized := chunks[0]
	oversized.Data = append(oversized.Data, 0)
	if err := assembler.Add(oversized); err == nil {
		t.Fatal("snapshot accepted bytes beyond its declared length")
	}
	if err := assembler.Add(chunks[0]); err != nil {
		t.Fatal(err)
	}
	conflict := chunks[0]
	conflict.Data = append([]byte(nil), conflict.Data...)
	if len(conflict.Data) == 0 {
		conflict.Data = []byte{1}
	} else {
		conflict.Data[0] ^= 0xff
	}
	if err := assembler.Add(conflict); err == nil {
		t.Fatal("snapshot accepted a conflicting duplicate chunk")
	}
}

func TestRelayFrameRoundTrip(t *testing.T) {
	inner := make([]byte, 90)
	copy(inner, "DraA")
	f := RelayFrame{SessionID: 1, SessionEpoch: 2, DomainID: 3, RequiredProjectionVersion: 4, SpeakerLeaseID: 5, InnerPacket: inner}
	wire, err := f.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnmarshalRelayFrame(wire)
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionID != f.SessionID || got.DomainID != f.DomainID || got.SpeakerLeaseID != f.SpeakerLeaseID || len(got.InnerPacket) != 90 {
		t.Fatalf("relay mismatch: %#v", got)
	}
}

func TestRelayFrameRejectsIncompleteIdentity(t *testing.T) {
	inner := make([]byte, DraARLHeaderSize)
	for _, frame := range []RelayFrame{
		{SessionEpoch: 1, DomainID: 1, InnerPacket: inner},
		{SessionID: 1, DomainID: 1, InnerPacket: inner},
		{SessionID: 1, SessionEpoch: 1, InnerPacket: inner},
	} {
		if _, err := frame.MarshalBinary(); err == nil {
			t.Fatal("relay frame accepted an incomplete identity")
		}
	}
	valid := RelayFrame{SessionID: 1, SessionEpoch: 1, DomainID: 1, InnerPacket: inner}
	wire, err := valid.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	for _, offset := range []int{0, 8, 16} {
		invalid := append([]byte(nil), wire...)
		clear(invalid[offset : offset+8])
		if _, err := UnmarshalRelayFrame(invalid); err == nil {
			t.Fatal("relay decoder accepted an incomplete identity")
		}
	}
}
