package clientcontract

import (
	"errors"
	"reflect"
	"testing"

	"draarl/internal/ghostsession"
	"draarl/internal/protocol"
)

func TestNormalizeServerVersion(t *testing.T) {
	for input, want := range map[string]string{
		"1.2.3":         "1.2.3",
		"v1.2.3-beta.1": "1.2.3-beta.1",
		"V2.0.0":        "2.0.0",
		" dev ":         "dev",
	} {
		if got := NormalizeServerVersion(input); got != want {
			t.Errorf("NormalizeServerVersion(%q)=%q want=%q", input, got, want)
		}
	}
}

func TestCurrentContractDeclaresModernGhostProtocol(t *testing.T) {
	contract := Current()
	if contract.ProtocolVersion != protocol.GhostAuthPayloadVersion {
		t.Fatalf("protocol version=%d", contract.ProtocolVersion)
	}
	want := []string{ghostsession.CapabilityMultiReceiveV1, ghostsession.CapabilitySourceGroupV1}
	if !reflect.DeepEqual(contract.Capabilities, want) {
		t.Fatalf("capabilities=%v want=%v", contract.Capabilities, want)
	}
}

func TestCheck(t *testing.T) {
	contract := Contract{ServerVersion: "1.2.3", ProtocolVersion: 1, Capabilities: []string{"multi_receive_v1", "source_group_v1"}}
	if err := Check(contract, Requirement{MinServerVersion: "1.2.0", RequiredProtocolVersion: 1, RequiredCapabilities: []string{"source_group_v1"}}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name        string
		contract    Contract
		requirement Requirement
		kind        FailureKind
	}{
		{name: "development version", contract: Contract{ServerVersion: "dev"}, requirement: Requirement{MinServerVersion: "1.0.0"}, kind: FailureUnknownServerVersion},
		{name: "old server", contract: contract, requirement: Requirement{MinServerVersion: "2.0.0"}, kind: FailureServerVersionTooLow},
		{name: "old protocol", contract: contract, requirement: Requirement{RequiredProtocolVersion: 2}, kind: FailureProtocolVersionTooLow},
		{name: "missing capability", contract: contract, requirement: Requirement{RequiredCapabilities: []string{"future_v1"}}, kind: FailureMissingCapability},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var failure *Failure
			if err := Check(test.contract, test.requirement); !errors.As(err, &failure) || failure.Kind != test.kind {
				t.Fatalf("failure=%#v err=%v", failure, err)
			}
		})
	}
}
