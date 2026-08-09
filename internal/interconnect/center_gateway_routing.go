package interconnect

import (
	"slices"
	"strings"
)

func (g *CenterGateway) releaseSpeakerForRouteChange(current, next DeviceRoute) {
	if g.speaker == nil {
		return
	}
	if current.SessionEpoch != next.SessionEpoch || current.DomainID != next.DomainID || next.DomainID == 0 || next.DisableSend {
		g.speaker.ReleaseSession(current.SessionID, current.SessionEpoch)
	}
}

// UpdateActiveDeviceRoute publishes committed business state to the edge that
// currently owns the device. Ownership is serialized with authentication and
// roaming so an update can never be applied to a superseded session.
func (g *CenterGateway) UpdateActiveDeviceRoute(deviceID, groupID int, domainID uint64, disableSend, disableRecv bool) (bool, error) {
	if deviceID <= 0 || g.cluster == nil {
		return false, nil
	}
	g.ownershipMu.Lock()
	defer g.ownershipMu.Unlock()
	g.mu.RLock()
	sessionID := g.activeByID[deviceID]
	owner, ok := g.deviceSessions[sessionID]
	g.mu.RUnlock()
	if !ok || sessionID == 0 {
		return false, nil
	}
	route, ok := g.cluster.ResolveRoute(sessionID)
	if !ok || route.SessionEpoch != owner.SessionEpoch || route.DeviceID != deviceID {
		return false, nil
	}
	currentRoute := route
	route.GroupID, route.DomainID = groupID, domainID
	route.DisableSend, route.DisableRecv = disableSend, disableRecv
	g.releaseSpeakerForRouteChange(currentRoute, route)
	return true, g.cluster.SetNodeRoute(owner.NodeID, route)
}

func (g *CenterGateway) UpdateActiveIdentityRoute(ownerID int, ssid byte, groupID int, domainID uint64, disableSend, disableRecv bool) (bool, error) {
	identity := deviceOwnerIdentity(ownerID, ssid)
	if identity == "" || g.cluster == nil {
		return false, nil
	}
	g.ownershipMu.Lock()
	defer g.ownershipMu.Unlock()
	g.mu.RLock()
	sessionID := g.activeDevices[identity]
	owner, ok := g.deviceSessions[sessionID]
	g.mu.RUnlock()
	if !ok || sessionID == 0 {
		return false, nil
	}
	route, ok := g.cluster.ResolveRoute(sessionID)
	if !ok || route.SessionEpoch != owner.SessionEpoch {
		return false, nil
	}
	currentRoute := route
	route.GroupID, route.DomainID = groupID, domainID
	route.DisableSend, route.DisableRecv = disableSend, disableRecv
	g.releaseSpeakerForRouteChange(currentRoute, route)
	return true, g.cluster.SetNodeRoute(owner.NodeID, route)
}

// UpdateActiveGhostRoute updates one exact ghost session. Modern clients must
// never use the legacy owner+SSID route updater because sibling installations
// share that platform identity.
func (g *CenterGateway) UpdateActiveGhostRoute(ghostSessionID string, groupID int, rxGroupIDs []int, resolve func(int) uint64) (bool, error) {
	ghostSessionID = strings.TrimSpace(ghostSessionID)
	if ghostSessionID == "" || groupID <= 0 || resolve == nil || g.cluster == nil {
		return false, nil
	}
	rxSet := make(map[int]struct{}, len(rxGroupIDs)+1)
	rxSet[groupID] = struct{}{}
	for _, candidate := range rxGroupIDs {
		if candidate > 0 {
			rxSet[candidate] = struct{}{}
		}
	}
	normalizedGroups := make([]int, 0, len(rxSet))
	domainSet := make(map[uint64]struct{}, len(rxSet))
	for candidate := range rxSet {
		normalizedGroups = append(normalizedGroups, candidate)
		if domainID := resolve(candidate); domainID != 0 {
			domainSet[domainID] = struct{}{}
		}
	}
	slices.Sort(normalizedGroups)
	rxDomainIDs := make([]uint64, 0, len(domainSet))
	for domainID := range domainSet {
		rxDomainIDs = append(rxDomainIDs, domainID)
	}
	slices.Sort(rxDomainIDs)

	g.ownershipMu.Lock()
	defer g.ownershipMu.Unlock()
	g.mu.RLock()
	sessionID := g.activeByGhost[ghostSessionID]
	owner, ok := g.deviceSessions[sessionID]
	g.mu.RUnlock()
	if !ok || sessionID == 0 || owner.GhostSessionID != ghostSessionID {
		return false, nil
	}
	route, ok := g.cluster.ResolveRoute(sessionID)
	if !ok || route.SessionEpoch != owner.SessionEpoch || route.GhostSessionID != ghostSessionID {
		return false, nil
	}
	currentRoute := route
	route.GroupID, route.DomainID = groupID, resolve(groupID)
	route.RxGroupIDs, route.RxDomainIDs = normalizedGroups, rxDomainIDs
	g.releaseSpeakerForRouteChange(currentRoute, route)
	return true, g.cluster.SetNodeRoute(owner.NodeID, route)
}

// RefreshActiveDeviceDomains recalculates every currently owned device after
// group enablement or virtual-link topology changes. These writes are rare and
// intentionally serialized with roaming to preserve owner/session ordering.
func (g *CenterGateway) RefreshActiveDeviceDomains(resolve func(groupID int) uint64) error {
	if g.cluster == nil || resolve == nil {
		return nil
	}
	g.ownershipMu.Lock()
	defer g.ownershipMu.Unlock()
	g.mu.RLock()
	owners := make([]deviceSessionOwner, 0, len(g.activeDevices))
	for _, sessionID := range g.activeDevices {
		if owner, ok := g.deviceSessions[sessionID]; ok {
			owners = append(owners, owner)
		}
	}
	g.mu.RUnlock()
	var firstErr error
	for _, owner := range owners {
		route, ok := g.cluster.ResolveRoute(owner.SessionID)
		if !ok || route.SessionEpoch != owner.SessionEpoch {
			continue
		}
		domainID := resolve(route.GroupID)
		rxDomainSet := make(map[uint64]struct{}, len(route.RxGroupIDs))
		for _, groupID := range route.RxGroupIDs {
			if rxDomainID := resolve(groupID); rxDomainID != 0 {
				rxDomainSet[rxDomainID] = struct{}{}
			}
		}
		rxDomainIDs := make([]uint64, 0, len(rxDomainSet))
		for rxDomainID := range rxDomainSet {
			rxDomainIDs = append(rxDomainIDs, rxDomainID)
		}
		slices.Sort(rxDomainIDs)
		if route.DomainID == domainID && slices.Equal(route.RxDomainIDs, rxDomainIDs) {
			continue
		}
		currentRoute := route
		route.DomainID = domainID
		route.RxDomainIDs = rxDomainIDs
		g.releaseSpeakerForRouteChange(currentRoute, route)
		if err := g.cluster.SetNodeRoute(owner.NodeID, route); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
