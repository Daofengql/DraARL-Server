package clientcontract

import (
	"fmt"
	"sort"
	"strings"

	"draarl/internal/buildinfo"
	"draarl/internal/clientversion"
	"draarl/internal/ghostsession"
	"draarl/internal/protocol"
)

type Contract struct {
	ServerVersion   string
	ProtocolVersion uint16
	Capabilities    []string
}

type Requirement struct {
	MinServerVersion        string
	RequiredProtocolVersion uint16
	RequiredCapabilities    []string
}

type FailureKind string

const (
	FailureUnknownServerVersion  FailureKind = "unknown_server_version"
	FailureServerVersionTooLow   FailureKind = "server_version_too_low"
	FailureProtocolVersionTooLow FailureKind = "protocol_version_too_low"
	FailureMissingCapability     FailureKind = "missing_capability"
)

type Failure struct {
	Kind       FailureKind
	Current    string
	Required   string
	Capability string
}

func (failure *Failure) Error() string {
	if failure == nil {
		return "client contract is incompatible"
	}
	switch failure.Kind {
	case FailureUnknownServerVersion:
		return fmt.Sprintf("server version %q is not comparable", failure.Current)
	case FailureServerVersionTooLow:
		return fmt.Sprintf("server version %s is below required %s", failure.Current, failure.Required)
	case FailureProtocolVersionTooLow:
		return fmt.Sprintf("protocol version %s is below required %s", failure.Current, failure.Required)
	case FailureMissingCapability:
		return fmt.Sprintf("server capability %s is missing", failure.Capability)
	default:
		return "client contract is incompatible"
	}
}

func Current() Contract {
	capabilities := []string{
		ghostsession.CapabilityMultiReceiveV1,
		ghostsession.CapabilitySourceGroupV1,
	}
	sort.Strings(capabilities)
	return Contract{
		ServerVersion:   NormalizeServerVersion(buildinfo.VersionString()),
		ProtocolVersion: protocol.GhostAuthPayloadVersion,
		Capabilities:    capabilities,
	}
}

// NormalizeServerVersion accepts the optional v prefix used by release builds
// while preserving non-release values such as "dev" for diagnostics.
func NormalizeServerVersion(value string) string {
	value = strings.TrimSpace(value)
	if clientversion.IsValid(value) {
		return value
	}
	if len(value) > 1 && (value[0] == 'v' || value[0] == 'V') && clientversion.IsValid(value[1:]) {
		return value[1:]
	}
	return value
}

func Check(contract Contract, requirement Requirement) error {
	if requirement.MinServerVersion != "" {
		if !clientversion.IsValid(contract.ServerVersion) {
			return &Failure{Kind: FailureUnknownServerVersion, Current: contract.ServerVersion, Required: requirement.MinServerVersion}
		}
		if clientversion.Compare(contract.ServerVersion, requirement.MinServerVersion) < 0 {
			return &Failure{Kind: FailureServerVersionTooLow, Current: contract.ServerVersion, Required: requirement.MinServerVersion}
		}
	}
	if contract.ProtocolVersion < requirement.RequiredProtocolVersion {
		return &Failure{
			Kind:     FailureProtocolVersionTooLow,
			Current:  fmt.Sprint(contract.ProtocolVersion),
			Required: fmt.Sprint(requirement.RequiredProtocolVersion),
		}
	}
	available := make(map[string]struct{}, len(contract.Capabilities))
	for _, capability := range contract.Capabilities {
		available[capability] = struct{}{}
	}
	for _, capability := range requirement.RequiredCapabilities {
		if _, ok := available[capability]; !ok {
			return &Failure{Kind: FailureMissingCapability, Capability: capability}
		}
	}
	return nil
}
