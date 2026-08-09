package interconnect

import (
	"errors"
	"fmt"
	"log"
	"reflect"
	"strings"
	"time"

	"draarl/internal/protocol"
)

func (g *CenterGateway) SetGhostRecoveryWindow(window time.Duration) {
	if window <= 0 {
		window = 3 * time.Minute
	}
	g.mu.Lock()
	g.ghostRecoveryAfter = window
	g.mu.Unlock()
}

func (g *CenterGateway) markGhostRecovery(owners []deviceSessionOwner) []ghostRecoveryTask {
	g.mu.Lock()
	if g.ghostRecovery == nil {
		g.ghostRecovery = make(map[string]uint64)
	}
	if g.ghostRecoveryAfter <= 0 {
		g.ghostRecoveryAfter = 3 * time.Minute
	}
	tasks := make([]ghostRecoveryTask, 0, len(owners))
	for _, owner := range owners {
		if owner.GhostSessionID == "" {
			continue
		}
		g.ghostRecoverySeq++
		if g.ghostRecoverySeq == 0 {
			g.ghostRecoverySeq++
		}
		g.ghostRecovery[owner.GhostSessionID] = g.ghostRecoverySeq
		tasks = append(tasks, ghostRecoveryTask{sessionID: owner.GhostSessionID, token: g.ghostRecoverySeq, after: g.ghostRecoveryAfter})
	}
	g.mu.Unlock()
	return tasks
}

func (g *CenterGateway) startGhostRecoveryTimers(tasks []ghostRecoveryTask) {
	for _, task := range tasks {
		task := task
		time.AfterFunc(task.after, func() {
			g.ownershipMu.Lock()
			g.mu.Lock()
			expired := g.ghostRecovery[task.sessionID] == task.token && g.activeByGhost[task.sessionID] == 0
			if expired {
				delete(g.ghostRecovery, task.sessionID)
			}
			handler := g.onGhostRevoke
			g.mu.Unlock()
			g.ownershipMu.Unlock()
			if expired && handler != nil {
				handler(task.sessionID, "edge_session_recovery_expired")
			}
		})
	}
}

func (g *CenterGateway) renewDeviceSession(session *NodeSession, request DeviceSessionRenewRequest, now time.Time) DeviceSessionRenewResponse {
	response := DeviceSessionRenewResponse{RequestID: request.RequestID, SessionID: request.SessionID, SessionEpoch: request.SessionEpoch}
	if session == nil || request.RequestID == 0 || request.SessionID == 0 || request.SessionEpoch == 0 || g.cluster == nil {
		response.Error = "invalid_session_renewal"
		return response
	}
	g.ownershipMu.Lock()
	defer g.ownershipMu.Unlock()
	g.mu.RLock()
	owner, ok := g.deviceSessions[request.SessionID]
	g.mu.RUnlock()
	if !ok || owner.NodeID != session.NodeID || owner.ControlSessionID != session.SessionID || owner.SessionEpoch != request.SessionEpoch {
		response.Error = "session_not_owned"
		return response
	}
	route, ok := g.cluster.ResolveRoute(request.SessionID)
	if !ok || route.SessionEpoch != request.SessionEpoch {
		response.Error = "session_route_missing"
		return response
	}
	expiresAt := now.Add(defaultDeviceGrantTTL)
	response.Success = true
	response.ExpiresAtMillis = expiresAt.UnixMilli()
	if owner.GhostSessionID != "" {
		g.mu.RLock()
		renew := g.onGhostRenew
		g.mu.RUnlock()
		if renew == nil {
			response.Success = false
			response.ExpiresAtMillis = 0
			response.Error = "ghost_recovery_ticket_unavailable"
			return response
		}
		ticket, err := renew(owner.GhostSessionID, session.NodeID, session.SessionID, now, expiresAt)
		if err != nil || strings.TrimSpace(ticket) == "" {
			response.Success = false
			response.ExpiresAtMillis = 0
			response.Error = "ghost_recovery_ticket_unavailable"
			return response
		}
		response.RecoveryTicket = ticket
	}
	return response
}

func deviceGrantIdentity(grant *DeviceGrant) string {
	if grant == nil {
		return ""
	}
	if isSessionGhostGrant(grant) {
		return fmt.Sprintf("owner:%d:ssid:%d:instance:%s", grant.OwnerID, grant.SSID, strings.ToLower(strings.TrimSpace(grant.ClientInstanceID)))
	}
	if grant.OwnerID > 0 {
		return fmt.Sprintf("owner:%d:ssid:%d", grant.OwnerID, grant.SSID)
	}
	if grant.DeviceID > 0 {
		return fmt.Sprintf("device:%d", grant.DeviceID)
	}
	if grant.Username != "" {
		return fmt.Sprintf("username:%s:ssid:%d", grant.Username, grant.SSID)
	}
	return ""
}

func isSessionGhostGrant(grant *DeviceGrant) bool {
	return grant != nil && grant.OwnerID > 0 && grant.GhostProtocolVersion > 0 && grant.GhostSessionID != "" &&
		grant.ClientInstanceID != "" && grant.SessionTag != 0 && protocol.IsGhostSSID(grant.SSID) && protocol.IsGhostDevModel(grant.DevModel)
}

func isGhostGrant(grant *DeviceGrant) bool {
	return grant != nil && grant.OwnerID > 0 && grant.GhostSessionID != "" && protocol.IsGhostSSID(grant.SSID) && protocol.IsGhostDevModel(grant.DevModel)
}

func deviceRoutesEqual(a, b DeviceRoute) bool {
	return reflect.DeepEqual(a, b)
}

func routeReceiveDomains(route DeviceRoute) []uint64 {
	if len(route.RxDomainIDs) > 0 {
		return route.RxDomainIDs
	}
	if route.DomainID != 0 {
		return []uint64{route.DomainID}
	}
	return nil
}

func deviceOwnerIdentity(ownerID int, ssid byte) string {
	if ownerID <= 0 {
		return ""
	}
	return fmt.Sprintf("owner:%d:ssid:%d", ownerID, ssid)
}

func (g *CenterGateway) activateDeviceSession(session *NodeSession, grant *DeviceGrant) error {
	g.ownershipMu.Lock()
	defer g.ownershipMu.Unlock()
	return g.activateDeviceSessionLocked(session, grant)
}

// activateDeviceSessionLocked requires ownershipMu. Keeping confirmation and
// activation under the same lock prevents a normal authentication from moving
// the device between the database ownership check and the replacement grant.
func (g *CenterGateway) activateDeviceSessionLocked(session *NodeSession, grant *DeviceGrant) error {
	if session == nil || grant == nil || g.cluster == nil {
		return errors.New("device session activation is incomplete")
	}
	identity := deviceGrantIdentity(grant)
	if identity == "" {
		return errors.New("device identity is incomplete")
	}
	if isSessionGhostGrant(grant) && session.NodeID != CenterLocalNodeID && strings.TrimSpace(grant.RecoveryTicket) == "" {
		return errors.New("edge ghost recovery ticket is required")
	}
	g.mu.RLock()
	oldSessionID := g.activeDevices[identity]
	oldOwner, hadOld := g.deviceSessions[oldSessionID]
	g.mu.RUnlock()
	if hadOld && oldOwner.NodeID == session.NodeID && oldOwner.ControlSessionID == session.SessionID &&
		oldOwner.DeviceID == grant.DeviceID && oldOwner.OwnerID == grant.OwnerID && oldOwner.SSID == grant.SSID &&
		oldOwner.GhostSessionID == grant.GhostSessionID && oldOwner.ClientInstanceID == grant.ClientInstanceID {
		grant.SessionID, grant.SessionEpoch = oldOwner.SessionID, oldOwner.SessionEpoch
		route, ok := g.cluster.ResolveRoute(oldOwner.SessionID)
		if !ok || route.SessionEpoch != oldOwner.SessionEpoch {
			return errors.New("edge owner route is missing")
		}
		next := grant.Route()
		if !deviceRoutesEqual(route, next) {
			g.releaseSpeakerForRouteChange(route, next)
			return g.cluster.SetNodeRoute(session.NodeID, next)
		}
		return nil
	}
	if session.NodeID != CenterLocalNodeID {
		g.mu.RLock()
		key := nodeControlSession{nodeID: session.NodeID, sessionID: session.SessionID}
		atLimit := g.sessionCounts[key] >= g.resourceLimits.MaxDeviceSessionsPerNode
		replacesSameSession := hadOld && oldOwner.NodeID == session.NodeID && oldOwner.ControlSessionID == session.SessionID
		g.mu.RUnlock()
		if atLimit && !replacesSameSession {
			session.resourceProtection().recordSessionLimitReject()
			return errors.New("edge device session limit reached")
		}
	}

	grant.SessionID = g.cluster.NextMessageID()
	g.mu.Lock()
	epoch := g.deviceEpochs[identity] + 1
	if epoch == 0 {
		epoch = 1
	}
	g.mu.Unlock()
	grant.SessionEpoch = epoch
	// Remove the previous owner from the authoritative lookup before any
	// persistence work. Relay hot paths therefore reject it while the new
	// entry is being committed. Restore it if activation fails.
	g.mu.Lock()
	oldSessionID = g.activeDevices[identity]
	oldOwner, hadOld = g.deviceSessions[oldSessionID]
	if hadOld {
		g.removeOwnerMapsLocked(oldSessionID, oldOwner)
	}
	g.mu.Unlock()
	if g.activate != nil {
		if err := g.activate(session, grant); err != nil {
			if hadOld {
				g.mu.Lock()
				g.restoreOwnerMapsLocked(oldOwner)
				g.mu.Unlock()
			}
			return err
		}
	}
	g.mu.Lock()
	g.deviceEpochs[identity] = epoch
	owner := deviceSessionOwner{NodeID: session.NodeID, ControlSessionID: session.SessionID, SessionID: grant.SessionID, SessionEpoch: epoch, DeviceID: grant.DeviceID, OwnerID: grant.OwnerID, SSID: grant.SSID, Identity: identity, GhostSessionID: grant.GhostSessionID, ClientInstanceID: grant.ClientInstanceID}
	g.deviceSessions[grant.SessionID] = owner
	g.sessionCounts[nodeControlSession{nodeID: owner.NodeID, sessionID: owner.ControlSessionID}]++
	g.activeDevices[identity] = grant.SessionID
	if grant.DeviceID > 0 {
		g.activeByID[grant.DeviceID] = grant.SessionID
	}
	if grant.GhostSessionID != "" {
		g.activeByGhost[grant.GhostSessionID] = grant.SessionID
		delete(g.ghostRecovery, grant.GhostSessionID)
	}
	g.mu.Unlock()

	if hadOld {
		g.notifyOwnerRevoke(oldOwner, "session_migrated")
		g.sendDeviceSessionRevoke(oldOwner, "session_migrated")
		if err := g.cluster.RemoveNodeRoute(oldOwner.NodeID, oldSessionID); err != nil {
			log.Printf("[INTERCONNECT] remove migrated route failed: node=%s session=%d err=%v", oldOwner.NodeID, oldSessionID, err)
		}
	}
	if err := g.cluster.SetNodeRoute(session.NodeID, grant.Route()); err != nil {
		log.Printf("[INTERCONNECT] publish device route failed: node=%s session=%d err=%v", session.NodeID, grant.SessionID, err)
	}
	return nil
}

func (g *CenterGateway) confirmDeviceSessions(session *NodeSession, request DeviceSessionConfirmRequest) DeviceSessionConfirmResponse {
	response := DeviceSessionConfirmResponse{RequestID: request.RequestID, Results: make([]DeviceSessionConfirmResult, 0, len(request.Sessions))}
	if session == nil || request.Validate() != nil {
		return response
	}
	g.ownershipMu.Lock()
	defer g.ownershipMu.Unlock()

	g.mu.RLock()
	handler := g.confirm
	g.mu.RUnlock()
	var candidates []DeviceSessionConfirmResult
	var err error
	if handler != nil {
		candidates, err = handler(session, request.Sessions)
	} else {
		err = errors.New("device session confirmation is unavailable")
	}
	candidateBySession := make(map[uint64]DeviceSessionConfirmResult, len(candidates))
	for _, candidate := range candidates {
		if _, exists := candidateBySession[candidate.SessionID]; !exists {
			candidateBySession[candidate.SessionID] = candidate
		}
	}
	for _, item := range request.Sessions {
		result := DeviceSessionConfirmResult{SessionID: item.SessionID, SessionEpoch: item.SessionEpoch}
		candidate, ok := candidateBySession[item.SessionID]
		if err != nil || !ok || !candidate.Success || candidate.SessionEpoch != item.SessionEpoch || candidate.Grant == nil {
			result.Error = "session_reconfirm_rejected"
			response.Results = append(response.Results, result)
			continue
		}
		grant := *candidate.Grant
		if grant.DeviceID != item.DeviceID || grant.OwnerID != item.OwnerID || grant.SSID != item.SSID ||
			grant.GhostSessionID != item.GhostSessionID || grant.ClientInstanceID != item.ClientInstanceID || deviceGrantIdentity(&grant) == "" {
			result.Error = "session_identity_mismatch"
			response.Results = append(response.Results, result)
			continue
		}
		if err := g.activateDeviceSessionLocked(session, &grant); err != nil {
			result.Error = "session_activation_failed"
			response.Results = append(response.Results, result)
			continue
		}
		result.Success = true
		result.Grant = &grant
		response.Results = append(response.Results, result)
	}
	return response
}

func (g *CenterGateway) handleDeviceSessionReport(session *NodeSession, report DeviceSessionReport) bool {
	if session == nil || report.Online || report.SessionID == 0 || report.SessionEpoch == 0 || g.cluster == nil {
		return false
	}
	g.ownershipMu.Lock()
	defer g.ownershipMu.Unlock()
	g.mu.Lock()
	owner, ok := g.deviceSessions[report.SessionID]
	if !ok || owner.NodeID != session.NodeID || owner.ControlSessionID != session.SessionID ||
		owner.SessionEpoch != report.SessionEpoch || (report.DeviceID > 0 && owner.DeviceID != report.DeviceID) {
		g.mu.Unlock()
		return false
	}
	g.removeOwnerMapsLocked(report.SessionID, owner)
	g.mu.Unlock()
	reason := strings.TrimSpace(report.Reason)
	if reason == "" {
		reason = "edge_session_offline"
	}
	g.notifyOwnerRevoke(owner, reason)
	if err := g.cluster.RemoveNodeRoute(owner.NodeID, owner.SessionID); err != nil {
		log.Printf("[INTERCONNECT] remove reported offline route failed: node=%s session=%d err=%v", owner.NodeID, owner.SessionID, err)
	}
	return true
}

// ActivateLocalDevice makes a centre-direct UDP/WS device part of the same
// owner/epoch state machine as an edge device. Repeated local activation is
// idempotent, which keeps WS and heartbeat hot paths from churning sessions.
func (g *CenterGateway) ActivateLocalDevice(grant *DeviceGrant) error {
	if grant == nil || g.cluster == nil {
		return errors.New("local device session activation is incomplete")
	}
	identity := deviceGrantIdentity(grant)
	if identity == "" {
		return errors.New("device identity is incomplete")
	}
	g.ownershipMu.Lock()
	defer g.ownershipMu.Unlock()

	g.mu.RLock()
	oldSessionID := g.activeDevices[identity]
	oldOwner, hadOld := g.deviceSessions[oldSessionID]
	g.mu.RUnlock()
	if hadOld && oldOwner.NodeID == CenterLocalNodeID && grant.SessionID == oldOwner.SessionID && grant.SessionEpoch == oldOwner.SessionEpoch {
		grant.SessionID, grant.SessionEpoch = oldOwner.SessionID, oldOwner.SessionEpoch
		route, ok := g.cluster.ResolveRoute(oldOwner.SessionID)
		if !ok || route.SessionEpoch != oldOwner.SessionEpoch {
			return errors.New("local owner route is missing")
		}
		next := grant.Route()
		if !deviceRoutesEqual(route, next) {
			g.releaseSpeakerForRouteChange(route, next)
			return g.cluster.SetNodeRoute(CenterLocalNodeID, next)
		}
		return nil
	}

	grant.SessionID = g.cluster.NextMessageID()
	g.mu.RLock()
	epoch := g.deviceEpochs[identity] + 1
	g.mu.RUnlock()
	if epoch == 0 {
		epoch = 1
	}
	grant.SessionEpoch = epoch
	pseudo := &NodeSession{NodeID: CenterLocalNodeID, SessionID: grant.SessionID}
	g.mu.Lock()
	if hadOld {
		g.removeOwnerMapsLocked(oldSessionID, oldOwner)
	}
	g.mu.Unlock()
	if g.activate != nil {
		if err := g.activate(pseudo, grant); err != nil {
			if hadOld {
				g.mu.Lock()
				g.restoreOwnerMapsLocked(oldOwner)
				g.mu.Unlock()
			}
			return err
		}
	}

	g.mu.Lock()
	g.deviceEpochs[identity] = epoch
	owner := deviceSessionOwner{NodeID: CenterLocalNodeID, ControlSessionID: grant.SessionID, SessionID: grant.SessionID, SessionEpoch: epoch, DeviceID: grant.DeviceID, OwnerID: grant.OwnerID, SSID: grant.SSID, Identity: identity, GhostSessionID: grant.GhostSessionID, ClientInstanceID: grant.ClientInstanceID}
	g.deviceSessions[grant.SessionID] = owner
	g.activeDevices[identity] = grant.SessionID
	if grant.DeviceID > 0 {
		g.activeByID[grant.DeviceID] = grant.SessionID
	}
	if grant.GhostSessionID != "" {
		g.activeByGhost[grant.GhostSessionID] = grant.SessionID
		delete(g.ghostRecovery, grant.GhostSessionID)
	}
	g.mu.Unlock()

	if hadOld {
		g.notifyOwnerRevoke(oldOwner, "session_migrated")
		g.sendDeviceSessionRevoke(oldOwner, "session_migrated")
		if err := g.cluster.RemoveNodeRoute(oldOwner.NodeID, oldSessionID); err != nil {
			log.Printf("[INTERCONNECT] remove migrated route failed: node=%s session=%d err=%v", oldOwner.NodeID, oldSessionID, err)
		}
	}
	return g.cluster.SetNodeRoute(CenterLocalNodeID, grant.Route())
}

func (g *CenterGateway) RevokeLocalDevice(sessionID, sessionEpoch uint64) bool {
	if g.cluster == nil || sessionID == 0 {
		return false
	}
	g.ownershipMu.Lock()
	defer g.ownershipMu.Unlock()
	g.mu.Lock()
	owner, ok := g.deviceSessions[sessionID]
	if !ok || owner.NodeID != CenterLocalNodeID || owner.SessionEpoch != sessionEpoch {
		g.mu.Unlock()
		return false
	}
	g.removeOwnerMapsLocked(sessionID, owner)
	g.mu.Unlock()
	_ = g.cluster.RemoveNodeRoute(CenterLocalNodeID, sessionID)
	return true
}

func (g *CenterGateway) removeOwnerMapsLocked(sessionID uint64, owner deviceSessionOwner) {
	if g.speaker != nil {
		g.speaker.ReleaseSession(sessionID, owner.SessionEpoch)
	}
	delete(g.deviceSessions, sessionID)
	key := nodeControlSession{nodeID: owner.NodeID, sessionID: owner.ControlSessionID}
	if g.sessionCounts[key] <= 1 {
		delete(g.sessionCounts, key)
	} else {
		g.sessionCounts[key]--
	}
	if g.activeDevices[owner.Identity] == sessionID {
		delete(g.activeDevices, owner.Identity)
	}
	if owner.DeviceID > 0 && g.activeByID[owner.DeviceID] == sessionID {
		delete(g.activeByID, owner.DeviceID)
	}
	if owner.GhostSessionID != "" && g.activeByGhost[owner.GhostSessionID] == sessionID {
		delete(g.activeByGhost, owner.GhostSessionID)
	}
}

func (g *CenterGateway) restoreOwnerMapsLocked(owner deviceSessionOwner) {
	g.deviceSessions[owner.SessionID] = owner
	g.sessionCounts[nodeControlSession{nodeID: owner.NodeID, sessionID: owner.ControlSessionID}]++
	g.activeDevices[owner.Identity] = owner.SessionID
	if owner.DeviceID > 0 {
		g.activeByID[owner.DeviceID] = owner.SessionID
	}
	if owner.GhostSessionID != "" {
		g.activeByGhost[owner.GhostSessionID] = owner.SessionID
	}
}

func (g *CenterGateway) notifyOwnerRevoke(owner deviceSessionOwner, reason string) {
	g.failDeviceConfigsForOwner(owner, errors.New("device config session revoked"))
	g.mu.RLock()
	deviceHandler := g.onDeviceRevoke
	localHandler := g.onLocalRevoke
	ghostHandler := g.onGhostRevoke
	g.mu.RUnlock()
	if ghostHandler != nil && owner.GhostSessionID != "" {
		ghostHandler(owner.GhostSessionID, reason)
	}
	if deviceHandler != nil && owner.DeviceID > 0 {
		deviceHandler(owner.NodeID, owner.ControlSessionID, owner.DeviceID, reason)
	}
	if owner.NodeID == CenterLocalNodeID && localHandler != nil {
		localHandler(owner.DeviceID, owner.OwnerID, owner.SSID, owner.SessionID, owner.SessionEpoch)
	}
}

func (g *CenterGateway) failDeviceConfigsForOwner(owner deviceSessionOwner, result error) {
	failed := make([]*pendingDeviceConfigDelivery, 0)
	g.configMu.Lock()
	for messageID, pending := range g.configPending {
		if pending.owner.NodeID == owner.NodeID && pending.owner.ControlSessionID == owner.ControlSessionID &&
			pending.owner.SessionID == owner.SessionID && pending.owner.SessionEpoch == owner.SessionEpoch {
			delete(g.configPending, messageID)
			failed = append(failed, pending)
		}
	}
	g.configMu.Unlock()
	for _, pending := range failed {
		select {
		case pending.result <- result:
		default:
		}
	}
}

func (g *CenterGateway) sendDeviceSessionRevoke(owner deviceSessionOwner, reason string) {
	if g.server == nil || owner.NodeID == "" || owner.NodeID == CenterLocalNodeID || owner.SessionID == 0 {
		return
	}
	payload, err := EncodeJSON(DeviceSessionRevoke{SessionID: owner.SessionID, SessionEpoch: owner.SessionEpoch, DeviceID: owner.DeviceID, Reason: reason})
	if err != nil {
		return
	}
	env := NewEnvelope(SubtypeDeviceSessionRevoke, "center", 0, g.cluster.NextMessageID(), payload)
	env.ClusterEpoch, env.Flags = g.cluster.Epoch(), FlagControl
	_ = g.server.SendEnvelope(owner.NodeID, env)
}

type nodeControlSession struct {
	nodeID    string
	sessionID uint64
}

type deviceSessionOwner struct {
	NodeID           string
	ControlSessionID uint64
	SessionID        uint64
	SessionEpoch     uint64
	DeviceID         int
	OwnerID          int
	SSID             byte
	Identity         string
	GhostSessionID   string
	ClientInstanceID string
}

type ghostRecoveryTask struct {
	sessionID string
	token     uint64
	after     time.Duration
}

func (g *CenterGateway) rejectGhostGrant(grant *DeviceGrant, reason string) {
	if grant == nil || grant.GhostSessionID == "" {
		return
	}
	g.mu.RLock()
	handler := g.onGhostRevoke
	g.mu.RUnlock()
	if handler != nil {
		handler(grant.GhostSessionID, reason)
	}
}

// RevokeActiveDevice makes the old session non-authoritative before sending
// any network message. Late upstream packets are therefore rejected even if
// the edge has not received the best-effort immediate revoke yet.
func (g *CenterGateway) RevokeActiveDevice(deviceID int, reason string) (bool, error) {
	if deviceID <= 0 || g.cluster == nil {
		return false, nil
	}
	g.ownershipMu.Lock()
	defer g.ownershipMu.Unlock()
	g.mu.Lock()
	sessionID := g.activeByID[deviceID]
	owner, ok := g.deviceSessions[sessionID]
	if ok {
		g.removeOwnerMapsLocked(sessionID, owner)
	}
	g.mu.Unlock()
	if !ok {
		return false, nil
	}
	g.notifyOwnerRevoke(owner, reason)
	g.sendDeviceSessionRevoke(owner, reason)
	err := g.cluster.RemoveNodeRoute(owner.NodeID, sessionID)
	return true, err
}

func (g *CenterGateway) RevokeActiveGhost(ghostSessionID, reason string) (bool, error) {
	ghostSessionID = strings.TrimSpace(ghostSessionID)
	if ghostSessionID == "" || g.cluster == nil {
		return false, nil
	}
	g.ownershipMu.Lock()
	defer g.ownershipMu.Unlock()
	g.mu.Lock()
	sessionID := g.activeByGhost[ghostSessionID]
	owner, ok := g.deviceSessions[sessionID]
	if ok && owner.GhostSessionID == ghostSessionID {
		g.removeOwnerMapsLocked(sessionID, owner)
	} else {
		ok = false
	}
	g.mu.Unlock()
	if !ok {
		return false, nil
	}
	g.notifyOwnerRevoke(owner, reason)
	g.sendDeviceSessionRevoke(owner, reason)
	return true, g.cluster.RemoveNodeRoute(owner.NodeID, owner.SessionID)
}

func (g *CenterGateway) RevokeActiveOwner(ownerID int, reason string) (int, error) {
	if ownerID <= 0 || g.cluster == nil {
		return 0, nil
	}
	g.ownershipMu.Lock()
	defer g.ownershipMu.Unlock()
	g.mu.Lock()
	owners := make([]deviceSessionOwner, 0)
	for sessionID, owner := range g.deviceSessions {
		if owner.OwnerID != ownerID {
			continue
		}
		owners = append(owners, owner)
		g.removeOwnerMapsLocked(sessionID, owner)
	}
	g.mu.Unlock()
	var firstErr error
	for _, owner := range owners {
		g.notifyOwnerRevoke(owner, reason)
		g.sendDeviceSessionRevoke(owner, reason)
		if err := g.cluster.RemoveNodeRoute(owner.NodeID, owner.SessionID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return len(owners), firstErr
}
