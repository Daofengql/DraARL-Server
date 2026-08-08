package interconnect

import (
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

	"draarl/internal/ghostsession"
	"draarl/internal/protocol"
	"draarl/internal/udphub"
)

type edgeDeviceSession struct {
	Grant            DeviceGrant
	ControlSessionID uint64
	Addr             *net.UDPAddr
	RealAddr         *net.UDPAddr
	LastSeen         time.Time
}

type pendingDeviceAuth struct {
	addr        *net.UDPAddr
	realAddr    *net.UDPAddr
	wire        []byte
	identity    string
	requestedAt time.Time
}

type pendingDeviceRenewal struct {
	sessionID    uint64
	sessionEpoch uint64
	requestedAt  time.Time
}

func (g *EdgeGateway) ensureSessionIndexesLocked() {
	if g.byIdentity == nil {
		g.byIdentity = make(map[string]uint64)
	}
	if g.bySessionTag == nil {
		g.bySessionTag = make(map[uint32]uint64)
	}
}

const edgeAuthRequestTimeout = 5 * time.Second

func (g *EdgeGateway) identity(packet *protocol.DraARLv1Packet) string {
	return fmt.Sprintf("%s-%d", packet.Username, packet.SSID)
}

func edgeSessionIdentity(grant DeviceGrant) string {
	if isSessionGhostGrant(&grant) {
		return deviceGrantIdentity(&grant)
	}
	return fmt.Sprintf("%s-%d", grant.Username, grant.SSID)
}

func edgeAuthIdentity(packet *protocol.DraARLv1Packet, realAddr *net.UDPAddr) (string, bool, error) {
	if packet == nil {
		return "", false, errors.New("device packet is required")
	}
	if packet.Type != protocol.DraARLTypeJWTAuth || !protocol.IsGhostDevModel(packet.DevModel) {
		return fmt.Sprintf("%s-%d", packet.Username, packet.SSID), false, nil
	}
	request, err := protocol.DecodeGhostAuthRequest(packet.DATA)
	if err != nil {
		return fmt.Sprintf("%s-%d", packet.Username, packet.SSID), false, err
	}
	instanceID, err := ghostsession.NormalizeClientInstanceID(request.ClientInstanceID)
	if err != nil {
		return "", true, ghostsession.ErrInvalidClientInstance
	}
	endpoint := ""
	if realAddr != nil {
		endpoint = realAddr.String()
	}
	return fmt.Sprintf("ghost-auth:%d:%s:%s", packet.DevModel, instanceID, endpoint), true, nil
}

func (g *EdgeGateway) handleDevicePacket(data []byte, remoteAddr, realAddr *net.UDPAddr) {
	packet, err := protocol.NewDraARLv1RoutingPacket(remoteAddr, data)
	if err != nil {
		return
	}
	defer protocol.ReleaseDraARLv1RoutingPacket(packet)
	key := g.identity(packet)
	tag := protocol.ReservedUint32(packet.Reserved)
	if protocol.IsGhostSSID(packet.SSID) && tag == 0 {
		if packet.Type == protocol.DraARLTypeJWTAuth {
			g.requestAuth(data, packet, remoteAddr, realAddr)
		}
		return
	}
	g.mu.RLock()
	sessionID, exists := g.byIdentity[key]
	if tag != 0 && protocol.IsGhostSSID(packet.SSID) {
		sessionID, exists = g.bySessionTag[tag]
	}
	session := g.sessions[sessionID]
	if tag == 0 && (!exists || session == nil) && packet.Username == "" && !protocol.IsGhostSSID(packet.SSID) {
		for id, candidate := range g.sessions {
			if candidate.Grant.SSID == packet.SSID && udpAddrEqual(candidate.RealAddr, realAddr) {
				sessionID, session, exists = id, candidate, true
				break
			}
		}
	}
	realAddrMatches := exists && session != nil && udpAddrEqual(session.RealAddr, realAddr)
	identityMatches := exists && session != nil && (!isSessionGhostGrant(&session.Grant) ||
		(session.Grant.SessionTag == tag && session.Grant.Username == packet.Username && session.Grant.SSID == packet.SSID && session.Grant.DevModel == packet.DevModel))
	g.mu.RUnlock()
	if !exists || session == nil || !identityMatches {
		if packet.Type == protocol.DraARLTypeHeartbeat || packet.Type == protocol.DraARLTypeJWTAuth {
			g.requestAuth(data, packet, remoteAddr, realAddr)
		}
		return
	}
	now := time.Now()
	if !g.allowExistingLocal(now) {
		return
	}
	// The device identity in a UDP packet is not proof of session ownership.
	// A changed direct/PROXY-advertised endpoint must authenticate again; text
	// or voice packets may never take over an existing identity or its FRP
	// return path. A transport-only FRP address change is allowed when the
	// authenticated real client endpoint remains the same.
	if !realAddrMatches {
		if packet.Type == protocol.DraARLTypeHeartbeat || packet.Type == protocol.DraARLTypeJWTAuth {
			g.requestAuth(data, packet, remoteAddr, realAddr)
		}
		return
	}
	// Update the endpoint and take an immutable grant snapshot while holding
	// the gateway lock. RouteDelta may update the same grant concurrently.
	g.mu.Lock()
	current := g.sessions[sessionID]
	if current == nil || !udpAddrEqual(current.RealAddr, realAddr) ||
		(isSessionGhostGrant(&current.Grant) && (current.Grant.SessionTag != tag || current.Grant.Username != packet.Username || current.Grant.SSID != packet.SSID || current.Grant.DevModel != packet.DevModel)) {
		g.mu.Unlock()
		if packet.Type == protocol.DraARLTypeHeartbeat || packet.Type == protocol.DraARLTypeJWTAuth {
			g.requestAuth(data, packet, remoteAddr, realAddr)
		}
		return
	}
	if current.Grant.ExpiresAtMillis > 0 && now.UnixMilli() >= current.Grant.ExpiresAtMillis {
		report := g.removeEdgeSessionLocked(sessionID, "grant_expired", now)
		g.mu.Unlock()
		g.sendDeviceSessionReport(report)
		if packet.Type == protocol.DraARLTypeHeartbeat || packet.Type == protocol.DraARLTypeJWTAuth {
			g.requestAuth(data, packet, remoteAddr, realAddr)
		}
		return
	}
	if !udpAddrEqual(current.Addr, remoteAddr) {
		current.Addr = cloneUDPAddr(remoteAddr)
		g.invalidateReceiverPlansLocked()
	}
	if !udpAddrEqual(current.RealAddr, realAddr) {
		current.RealAddr = cloneUDPAddr(realAddr)
	}
	current.LastSeen = now
	grant := current.Grant
	shouldRenew := grant.ExpiresAtMillis > 0 && time.UnixMilli(grant.ExpiresAtMillis).Sub(now) <= g.grantRenewBefore
	g.mu.Unlock()
	if packet.Type == protocol.DraARLTypeJWTAuth {
		g.requestAuth(data, packet, remoteAddr, realAddr)
		return
	}
	if packet.Type == protocol.DraARLTypeHeartbeat {
		response := protocol.EncodeHeartbeatResponse(packet, grant.CallSign)
		g.writeDevice(response, remoteAddr)
		if shouldRenew {
			g.requestSessionRenewal(sessionID, grant.SessionEpoch, now)
		}
		return
	}
	if packet.Type == protocol.DraARLTypeConfig {
		if protocol.IsValidNormalSSID(grant.SSID) && grant.DeviceID > 0 && len(packet.DATA) >= 2 && packet.DATA[0] == udphub.ConfigTypeSet {
			g.requestDeviceConfigUp(DeviceConfigKindReport, grant, packet.DATA)
		}
		return
	}
	if packet.Type != protocol.DraARLTypeTextMessage && packet.Type != protocol.DraARLTypeOpus16K {
		return
	}
	if grant.DisableSend {
		return
	}
	inner := protocol.PrepareForwardPacket(data, grant.Username, grant.CallSign, grant.SSID, packet.Type, grant.DevModel, grant.DMRID, packet.DATA)
	defer protocol.ReleaseForwardPacket(inner)
	if packet.Type == protocol.DraARLTypeOpus16K {
		g.handleEdgeVoice(grant, inner, now)
		return
	}
	g.localFanout(grant.SessionID, grant.DomainID, inner, grant.GroupID)
	g.sendEdgeRelay(grant, inner, 0)
}

func (g *EdgeGateway) sessionCleanerLoop() {
	defer g.cleanerWG.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-g.closed:
			return
		case now := <-ticker.C:
			g.expireDeviceSessions(now)
		}
	}
}

func (g *EdgeGateway) expireDeviceSessions(now time.Time) int {
	reports := make([]DeviceSessionReport, 0)
	configRetries := make([]Envelope, 0)
	g.mu.Lock()
	for requestID, pending := range g.pending {
		if pending != nil && now.Sub(pending.requestedAt) > edgeAuthRequestTimeout {
			delete(g.pending, requestID)
			if g.pendingIdentity[pending.identity] == requestID {
				delete(g.pendingIdentity, pending.identity)
			}
		}
	}
	for requestID, pending := range g.pendingRenewals {
		if now.Sub(pending.requestedAt) > edgeAuthRequestTimeout {
			delete(g.pendingRenewals, requestID)
			if g.renewingSessions[pending.sessionID] == requestID {
				delete(g.renewingSessions, pending.sessionID)
			}
		}
	}
	for requestID, pending := range g.pendingConfigUp {
		if now.Sub(pending.requestedAt) > edgeAuthRequestTimeout {
			delete(g.pendingConfigUp, requestID)
			continue
		}
		if pending.attempts < deviceConfigMaxAttempts && now.Sub(pending.lastSentAt) >= deviceConfigRetryAfter {
			pending.attempts++
			pending.lastSentAt = now
			configRetries = append(configRetries, pending.envelope)
		}
	}
	for messageID, cached := range g.configDownResults {
		if now.Sub(cached.storedAt) > deviceConfigCacheTTL {
			delete(g.configDownResults, messageID)
		}
	}
	if g.sessionTimeout > 0 {
		for sessionID, session := range g.sessions {
			reason := ""
			if now.Sub(session.LastSeen) > g.sessionTimeout {
				reason = "device_timeout"
			} else if session.Grant.ExpiresAtMillis > 0 && now.UnixMilli() >= session.Grant.ExpiresAtMillis {
				reason = "grant_expired"
			} else if !g.allowExistingLocal(now) && !g.preserveSessionsDuringRecovery() {
				reason = "control_unavailable"
			}
			if reason != "" {
				reports = append(reports, g.removeEdgeSessionLocked(sessionID, reason, now))
			}
		}
	}
	g.mu.Unlock()
	if link := g.currentControl(true); link != nil {
		for _, env := range configRetries {
			_ = link.client.SendEnvelope(env)
		}
	}
	for _, report := range reports {
		g.sendDeviceSessionReport(report)
	}
	return len(reports)
}

func (g *EdgeGateway) requestSessionRenewal(sessionID, sessionEpoch uint64, now time.Time) {
	if sessionID == 0 || sessionEpoch == 0 {
		return
	}
	link := g.currentControl(true)
	if link == nil {
		return
	}
	requestID := g.nextRequest.Add(1)
	request := DeviceSessionRenewRequest{RequestID: requestID, SessionID: sessionID, SessionEpoch: sessionEpoch}
	g.mu.Lock()
	session := g.sessions[sessionID]
	if session == nil || session.Grant.SessionEpoch != sessionEpoch || g.renewingSessions[sessionID] != 0 {
		g.mu.Unlock()
		return
	}
	g.pendingRenewals[requestID] = pendingDeviceRenewal{sessionID: sessionID, sessionEpoch: sessionEpoch, requestedAt: now}
	g.renewingSessions[sessionID] = requestID
	g.mu.Unlock()
	if g.renewSession != nil {
		g.renewSession(request)
		return
	}
	payload, err := EncodeJSON(request)
	if err == nil {
		env := NewEnvelope(SubtypeDeviceSessionRenew, link.client.Session.NodeID, link.client.Session.SessionID, requestID, payload)
		env.Flags = FlagControl | FlagAck
		err = link.client.SendEnvelope(env)
	}
	if err != nil {
		g.mu.Lock()
		delete(g.pendingRenewals, requestID)
		if g.renewingSessions[sessionID] == requestID {
			delete(g.renewingSessions, sessionID)
		}
		g.mu.Unlock()
	}
}

func (g *EdgeGateway) removeEdgeSessionLocked(sessionID uint64, reason string, now time.Time) DeviceSessionReport {
	g.ensureSessionIndexesLocked()
	session := g.sessions[sessionID]
	if session == nil {
		return DeviceSessionReport{}
	}
	delete(g.sessions, sessionID)
	g.invalidateReceiverPlansLocked()
	g.removeSpeakerSessionLocked(sessionID, session.Grant.SessionEpoch)
	key := edgeSessionIdentity(session.Grant)
	if g.byIdentity[key] == sessionID {
		delete(g.byIdentity, key)
	}
	if session.Grant.SessionTag != 0 && g.bySessionTag[session.Grant.SessionTag] == sessionID {
		delete(g.bySessionTag, session.Grant.SessionTag)
	}
	g.removePendingDeviceConfigsLocked(sessionID)
	return DeviceSessionReport{SessionID: sessionID, SessionEpoch: session.Grant.SessionEpoch, DeviceID: session.Grant.DeviceID, Reason: reason, ReportedAtMillis: now.UnixMilli()}
}

func (g *EdgeGateway) sendDeviceSessionReport(report DeviceSessionReport) {
	if report.SessionID == 0 || report.SessionEpoch == 0 {
		return
	}
	if g.reportSession != nil {
		g.reportSession(report)
		return
	}
	link := g.currentControl(true)
	if link == nil {
		return
	}
	payload, err := EncodeJSON(report)
	if err != nil {
		return
	}
	env := NewEnvelope(SubtypeDeviceSessionReport, link.client.Session.NodeID, link.client.Session.SessionID, g.nextRequest.Add(1), payload)
	env.Flags = FlagControl
	_ = link.client.SendEnvelope(env)
}

func (g *EdgeGateway) confirmActiveSessions(link *edgeControlLink) error {
	if link == nil || link.client == nil || link.client.Session == nil {
		return errors.New("edge control is unavailable for session confirmation")
	}
	defer link.confirming.Store(false)
	items := g.sessionConfirmSnapshot(time.Now())
	if len(items) == 0 {
		link.recovering.Store(false)
		return nil
	}
	if link.client.Session.Features&NodeFeatureSessionReconfirm == 0 {
		g.dropUnconfirmedSessions(items, "session_reconfirm_unsupported")
		link.recovering.Store(false)
		return nil
	}
	for offset := 0; offset < len(items); offset += MaxDeviceSessionConfirmBatch {
		end := offset + MaxDeviceSessionConfirmBatch
		if end > len(items) {
			end = len(items)
		}
		request := DeviceSessionConfirmRequest{RequestID: g.nextRequest.Add(1), Sessions: append([]DeviceSessionConfirmItem(nil), items[offset:end]...)}
		responseCh := make(chan DeviceSessionConfirmResponse, 1)
		g.mu.Lock()
		g.pendingConfirms[request.RequestID] = responseCh
		g.mu.Unlock()
		payload, err := EncodeJSON(request)
		if err == nil {
			env := NewEnvelope(SubtypeDeviceSessionConfirm, link.client.Session.NodeID, link.client.Session.SessionID, request.RequestID, payload)
			env.Flags = FlagControl | FlagAck | FlagCritical
			err = link.client.SendEnvelope(env)
		}
		if err == nil {
			select {
			case response := <-responseCh:
				err = g.applySessionConfirmResponse(link, request, response, time.Now())
			case <-link.client.Done():
				err = errors.New("edge control closed during session confirmation")
			case <-g.closed:
				err = errors.New("edge gateway closed during session confirmation")
			case <-time.After(edgeAuthRequestTimeout):
				err = errors.New("device session confirmation timed out")
			}
		}
		g.mu.Lock()
		delete(g.pendingConfirms, request.RequestID)
		g.mu.Unlock()
		if err != nil {
			return err
		}
	}
	return nil
}

func (g *EdgeGateway) sessionConfirmSnapshot(now time.Time) []DeviceSessionConfirmItem {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ensureSessionIndexesLocked()
	items := make([]DeviceSessionConfirmItem, 0, len(g.sessions))
	for sessionID, session := range g.sessions {
		validIdentity := session != nil && (session.Grant.DeviceID > 0 ||
			(isGhostGrant(&session.Grant) && strings.TrimSpace(session.Grant.RecoveryTicket) != ""))
		valid := validIdentity && session.ControlSessionID != 0 && session.Grant.OwnerID > 0 && session.Grant.SSID != 0 &&
			session.Grant.SessionEpoch != 0 && now.Sub(session.LastSeen) <= g.sessionTimeout &&
			session.Grant.ExpiresAtMillis > now.UnixMilli()
		if !valid {
			g.removeEdgeSessionLocked(sessionID, "session_reconfirm_ineligible", now)
			continue
		}
		items = append(items, DeviceSessionConfirmItem{
			SessionID: sessionID, SessionEpoch: session.Grant.SessionEpoch, ControlSessionID: session.ControlSessionID,
			DeviceID: session.Grant.DeviceID, OwnerID: session.Grant.OwnerID, SSID: session.Grant.SSID, DevModel: session.Grant.DevModel,
			GhostSessionID: session.Grant.GhostSessionID, ClientInstanceID: session.Grant.ClientInstanceID,
			RecoveryTicket: session.Grant.RecoveryTicket,
		})
	}
	return items
}

func (g *EdgeGateway) dropUnconfirmedSessions(items []DeviceSessionConfirmItem, reason string) {
	g.mu.Lock()
	for _, item := range items {
		session := g.sessions[item.SessionID]
		if session != nil && session.Grant.SessionEpoch == item.SessionEpoch && session.ControlSessionID == item.ControlSessionID {
			g.removeEdgeSessionLocked(item.SessionID, reason, time.Now())
		}
	}
	g.mu.Unlock()
}

func (g *EdgeGateway) applySessionConfirmResponse(link *edgeControlLink, request DeviceSessionConfirmRequest, response DeviceSessionConfirmResponse, now time.Time) error {
	if link == nil || response.RequestID != request.RequestID || response.Validate() != nil {
		return errors.New("invalid device session confirmation response")
	}
	results := make(map[uint64]DeviceSessionConfirmResult, len(response.Results))
	for _, result := range response.Results {
		results[result.SessionID] = result
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ensureSessionIndexesLocked()
	receiversChanged := false
	defer func() {
		if receiversChanged {
			g.invalidateReceiverPlansLocked()
		}
	}()
	for _, item := range request.Sessions {
		session := g.sessions[item.SessionID]
		if session == nil || session.Grant.SessionEpoch != item.SessionEpoch || session.ControlSessionID != item.ControlSessionID {
			continue
		}
		result, ok := results[item.SessionID]
		if !ok || !result.Success || result.SessionEpoch != item.SessionEpoch || result.Grant == nil {
			g.removeEdgeSessionLocked(item.SessionID, "session_reconfirm_rejected", now)
			continue
		}
		grant := *result.Grant
		if grant.DeviceID != item.DeviceID || grant.OwnerID != item.OwnerID || grant.SSID != item.SSID || (item.DeviceID == 0 && grant.DevModel != item.DevModel) ||
			grant.GhostSessionID != item.GhostSessionID || grant.ClientInstanceID != item.ClientInstanceID ||
			(item.DeviceID == 0 && strings.TrimSpace(grant.RecoveryTicket) == "") ||
			grant.SessionID == 0 || grant.SessionEpoch == 0 || grant.ExpiresAtMillis <= now.UnixMilli() {
			return errors.New("device session confirmation changed the device identity")
		}
		if collision := g.sessions[grant.SessionID]; collision != nil && collision != session {
			return errors.New("device session confirmation reused an active session ID")
		}
		oldKey := edgeSessionIdentity(session.Grant)
		oldTag := session.Grant.SessionTag
		delete(g.sessions, item.SessionID)
		g.removeSpeakerSessionLocked(item.SessionID, item.SessionEpoch)
		g.removePendingDeviceConfigsLocked(item.SessionID)
		session.Grant = grant
		session.ControlSessionID = link.client.Session.SessionID
		g.sessions[grant.SessionID] = session
		if g.byIdentity[oldKey] == item.SessionID {
			delete(g.byIdentity, oldKey)
		}
		if oldTag != 0 && g.bySessionTag[oldTag] == item.SessionID {
			delete(g.bySessionTag, oldTag)
		}
		key := edgeSessionIdentity(grant)
		if previousID := g.byIdentity[key]; previousID != 0 && previousID != grant.SessionID {
			g.removeEdgeSessionLocked(previousID, "session_reconfirm_identity_replaced", now)
		}
		if grant.SessionTag != 0 {
			if previousID := g.bySessionTag[grant.SessionTag]; previousID != 0 && previousID != grant.SessionID {
				return errors.New("device session confirmation reused an active session tag")
			}
			g.bySessionTag[grant.SessionTag] = grant.SessionID
		}
		g.byIdentity[key] = grant.SessionID
		receiversChanged = true
	}
	return nil
}

func (g *EdgeGateway) requestAuth(data []byte, packet *protocol.DraARLv1Packet, remoteAddr, realAddr *net.UDPAddr) {
	link := g.currentControl(true)
	if link == nil {
		return
	}
	identity, sessionGhost, identityErr := edgeAuthIdentity(packet, realAddr)
	if identityErr != nil {
		if packet != nil && packet.Type == protocol.DraARLTypeJWTAuth && protocol.IsGhostDevModel(packet.DevModel) {
			ssid := protocol.GetGhostSSID(packet.DevModel)
			response := protocol.EncodeDraARLv1(packet.Username, "", ssid, protocol.DraARLTypeJWTAuth, packet.DevModel, 0, "", append([]byte{protocol.JWTAuthInvalidToken}, []byte("ghost_protocol_upgrade_required")...))
			g.writeDevice(response, remoteAddr)
		}
		return
	}
	if identity == "" {
		return
	}
	if sessionGhost && (link.client == nil || link.client.Session == nil || link.client.Session.Features&NodeFeatureGhostMultiSession == 0) {
		ssid := protocol.GetGhostSSID(packet.DevModel)
		response := protocol.EncodeDraARLv1(packet.Username, "", ssid, protocol.DraARLTypeJWTAuth, packet.DevModel, 0, "", append([]byte{protocol.JWTAuthInvalidToken}, []byte("node_ghost_multi_session_unsupported")...))
		g.writeDevice(response, remoteAddr)
		return
	}
	id := g.nextRequest.Add(1)
	g.mu.Lock()
	if _, exists := g.pendingIdentity[identity]; exists {
		g.mu.Unlock()
		return
	}
	g.pending[id] = &pendingDeviceAuth{addr: cloneUDPAddr(remoteAddr), realAddr: cloneUDPAddr(realAddr), wire: append([]byte(nil), data...), identity: identity, requestedAt: time.Now()}
	g.pendingIdentity[identity] = id
	g.mu.Unlock()
	sourceIP := ""
	if realAddr != nil && realAddr.IP != nil {
		sourceIP = realAddr.IP.String()
	}
	request := DeviceAuthRequest{RequestID: id, SourceIP: sourceIP, Packet: append([]byte(nil), data...)}
	payload, err := EncodeJSON(request)
	if err != nil {
		g.mu.Lock()
		delete(g.pending, id)
		if g.pendingIdentity[identity] == id {
			delete(g.pendingIdentity, identity)
		}
		g.mu.Unlock()
		return
	}
	env := NewEnvelope(SubtypeDeviceAuth, link.client.Session.NodeID, link.client.Session.SessionID, id, payload)
	env.Flags = FlagControl | FlagAck
	if err := link.client.SendEnvelope(env); err != nil {
		g.mu.Lock()
		delete(g.pending, id)
		delete(g.pendingIdentity, identity)
		g.mu.Unlock()
	}
}

func (g *EdgeGateway) finishSessionRenewal(response DeviceSessionRenewResponse, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	pending, ok := g.pendingRenewals[response.RequestID]
	if !ok {
		return false
	}
	delete(g.pendingRenewals, response.RequestID)
	if g.renewingSessions[pending.sessionID] == response.RequestID {
		delete(g.renewingSessions, pending.sessionID)
	}
	if !response.Success || response.SessionID != pending.sessionID || response.SessionEpoch != pending.sessionEpoch || response.ExpiresAtMillis <= now.UnixMilli() {
		return false
	}
	session := g.sessions[pending.sessionID]
	if session == nil || session.Grant.SessionEpoch != pending.sessionEpoch {
		return false
	}
	if isSessionGhostGrant(&session.Grant) {
		if strings.TrimSpace(response.RecoveryTicket) == "" {
			return false
		}
		session.Grant.RecoveryTicket = response.RecoveryTicket
	}
	session.Grant.ExpiresAtMillis = response.ExpiresAtMillis
	return true
}

func (g *EdgeGateway) revokeSession(revoke DeviceSessionRevoke) bool {
	if revoke.SessionID == 0 {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	session := g.sessions[revoke.SessionID]
	if session == nil || session.Grant.SessionEpoch != revoke.SessionEpoch {
		return false
	}
	g.removeEdgeSessionLocked(revoke.SessionID, "center_revoke", time.Now())
	return true
}

func (g *EdgeGateway) finishAuth(response DeviceAuthResponse) {
	g.mu.Lock()
	pending := g.pending[response.RequestID]
	delete(g.pending, response.RequestID)
	if pending != nil {
		if g.pendingIdentity[pending.identity] == response.RequestID {
			delete(g.pendingIdentity, pending.identity)
		}
	}
	g.mu.Unlock()
	if pending == nil {
		return
	}
	if !response.Success || response.Grant == nil {
		if len(response.ResponsePacket) > 0 {
			g.writeDevice(response.ResponsePacket, pending.addr)
		}
		return
	}
	grant := *response.Grant
	if grant.SessionID == 0 {
		return
	}
	if grant.GhostSessionID != "" && !isSessionGhostGrant(&grant) {
		return
	}
	if isSessionGhostGrant(&grant) && strings.TrimSpace(grant.RecoveryTicket) == "" {
		return
	}
	controlSessionID := uint64(0)
	if link := g.currentControl(false); link != nil && link.client != nil && link.client.Session != nil {
		controlSessionID = link.client.Session.SessionID
	}
	session := &edgeDeviceSession{Grant: grant, ControlSessionID: controlSessionID, Addr: cloneUDPAddr(pending.addr), RealAddr: cloneUDPAddr(pending.realAddr), LastSeen: time.Now()}
	key := edgeSessionIdentity(grant)
	g.mu.Lock()
	g.ensureSessionIndexesLocked()
	for previousID, previous := range g.sessions {
		if previousID == grant.SessionID || previous == nil || !udpAddrEqual(previous.RealAddr, session.RealAddr) {
			continue
		}
		if edgeSessionIdentity(previous.Grant) == key {
			g.removeEdgeSessionLocked(previousID, "authenticated_identity_replaced", time.Now())
			break
		}
		g.mu.Unlock()
		g.sendDeviceSessionReport(DeviceSessionReport{
			SessionID: grant.SessionID, SessionEpoch: grant.SessionEpoch, DeviceID: grant.DeviceID,
			Reason: "udp_endpoint_already_authenticated", ReportedAtMillis: time.Now().UnixMilli(),
		})
		return
	}
	if grant.SessionTag != 0 {
		if previousID := g.bySessionTag[grant.SessionTag]; previousID != 0 && previousID != grant.SessionID {
			g.mu.Unlock()
			g.sendDeviceSessionReport(DeviceSessionReport{
				SessionID: grant.SessionID, SessionEpoch: grant.SessionEpoch, DeviceID: grant.DeviceID,
				Reason: "session_tag_already_authenticated", ReportedAtMillis: time.Now().UnixMilli(),
			})
			return
		}
	}
	receiversChanged := false
	if existing := g.sessions[grant.SessionID]; existing != nil {
		oldKey, oldTag := edgeSessionIdentity(existing.Grant), existing.Grant.SessionTag
		receiversChanged = !slices.Equal(routeReceiveDomains(existing.Grant.Route()), routeReceiveDomains(grant.Route())) || existing.Grant.DisableRecv != grant.DisableRecv ||
			existing.Grant.DeviceID != grant.DeviceID || existing.Grant.Username != grant.Username || existing.Grant.SSID != grant.SSID ||
			existing.Grant.SourceGroupV1 != grant.SourceGroupV1 || !udpAddrEqual(existing.Addr, session.Addr)
		if existing.Grant.SessionEpoch != grant.SessionEpoch || existing.Grant.DomainID != grant.DomainID || grant.DisableSend {
			g.removeSpeakerSessionLocked(grant.SessionID, existing.Grant.SessionEpoch)
		}
		if oldKey != key && g.byIdentity[oldKey] == grant.SessionID {
			delete(g.byIdentity, oldKey)
		}
		if oldTag != 0 && oldTag != grant.SessionTag && g.bySessionTag[oldTag] == grant.SessionID {
			delete(g.bySessionTag, oldTag)
		}
		existing.Grant = grant
		existing.Addr = session.Addr
		existing.RealAddr = session.RealAddr
		existing.LastSeen = session.LastSeen
		existing.ControlSessionID = session.ControlSessionID
	} else {
		if previousID := g.byIdentity[key]; previousID != 0 && previousID != grant.SessionID {
			g.removeEdgeSessionLocked(previousID, "authenticated_identity_replaced", time.Now())
		}
		g.sessions[grant.SessionID] = session
		receiversChanged = true
	}
	g.byIdentity[key] = grant.SessionID
	if grant.SessionTag != 0 {
		g.bySessionTag[grant.SessionTag] = grant.SessionID
	}
	if receiversChanged {
		g.invalidateReceiverPlansLocked()
	}
	g.mu.Unlock()
	if len(response.ResponsePacket) > 0 {
		g.writeDevice(response.ResponsePacket, pending.addr)
	} else {
		packet, err := protocol.NewDraARLv1RoutingPacket(pending.addr, pending.wire)
		if err == nil {
			responsePacket := protocol.EncodeHeartbeatResponse(packet, grant.CallSign)
			g.writeDevice(responsePacket, pending.addr)
			protocol.ReleaseDraARLv1RoutingPacket(packet)
		}
	}
	if protocol.IsValidNormalSSID(grant.SSID) && grant.DeviceID > 0 {
		g.requestDeviceConfigUp(DeviceConfigKindSync, grant, nil)
	}
}
