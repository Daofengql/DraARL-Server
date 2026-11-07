package interconnect

import "slices"

func (m *ClusterManager) HasGroupBroadcastTarget(groupID int, domainID uint64) bool {
	if m == nil || groupID <= 0 || domainID == 0 {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.groupBroadcastTargetsLocked(groupID, domainID)) > 0
}

// Old edges cannot enforce group isolation. They keep receiving ordinary
// voice, but must be upgraded before receiving scheduled announcements.
func (m *ClusterManager) groupBroadcastTargetsLocked(groupID int, domainID uint64) map[string]uint64 {
	targets := make(map[string]uint64)
	for nodeID := range m.domainNodes[domainID] {
		session := m.nodes[nodeID]
		if nodeID == CenterLocalNodeID || session == nil || session.Features&NodeFeatureGroupBroadcast == 0 {
			continue
		}
		projection := m.nodeProjection[nodeID]
		if projection == nil {
			continue
		}
		for _, route := range projection.Devices {
			if !route.DisableRecv && slices.Contains(routeReceiveGroups(route), groupID) && slices.Contains(routeReceiveDomains(route), domainID) {
				targets[nodeID] = projection.Version
				break
			}
		}
	}
	return targets
}
