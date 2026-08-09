package udphub

import "sync/atomic"

var (
	ghostPacketInvalidTags     atomic.Uint64
	ghostPacketIdentityRejects atomic.Uint64
	ghostPacketEndpointRejects atomic.Uint64
	ghostPacketRegistryRejects atomic.Uint64
)

func GetGhostPacketMetrics() map[string]uint64 {
	return map[string]uint64{
		"invalid_tags":     ghostPacketInvalidTags.Load(),
		"identity_rejects": ghostPacketIdentityRejects.Load(),
		"endpoint_rejects": ghostPacketEndpointRejects.Load(),
		"registry_rejects": ghostPacketRegistryRejects.Load(),
	}
}
