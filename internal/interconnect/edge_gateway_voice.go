package interconnect

import (
	"slices"
	"time"

	"draarl/internal/ghostsession"
	"draarl/internal/protocol"
	"draarl/internal/udphub"
)

type edgeBufferedVoice struct {
	grant      DeviceGrant
	inner      []byte
	receivedAt time.Time
}

type edgeSpeakerState struct {
	sessionID      uint64
	sessionEpoch   uint64
	leaseID        uint64
	expiresAt      time.Time
	lastVoiceAt    time.Time
	pendingRequest uint64
	pendingSince   time.Time
	blockedUntil   time.Time
	fallback       bool
	buffered       []edgeBufferedVoice
}

func (g *EdgeGateway) clearSpeakerStates() {
	g.mu.Lock()
	clear(g.speakerDomains)
	g.mu.Unlock()
}

func (g *EdgeGateway) sendEdgeRelay(grant DeviceGrant, inner []byte, leaseID uint64) {
	link := g.currentControl(true)
	if link == nil {
		return
	}
	relayInner := inner
	if link.client != nil && link.client.Session != nil && link.client.Session.Features&NodeFeatureGhostMultiSession != 0 {
		if tagged, ok := protocol.WithSourceGroupID(inner, grant.GroupID); ok {
			relayInner = tagged
		}
	}
	frame := RelayFrame{SessionID: grant.SessionID, SessionEpoch: grant.SessionEpoch, DomainID: grant.DomainID, RequiredProjectionVersion: g.projection.Snapshot().Version, SpeakerLeaseID: leaseID, InnerPacket: relayInner}
	payload, err := frame.MarshalBinary()
	if err != nil {
		return
	}
	env := NewEnvelope(SubtypeRelayUpstream, link.client.Session.NodeID, link.client.Session.SessionID, g.nextRequest.Add(1), payload)
	env.ProjectionVersion = frame.RequiredProjectionVersion
	if link.peer != nil {
		err = link.peer.Send(env)
	} else {
		err = link.client.SendEnvelope(env)
	}
	if err != nil {
		g.metrics.AddError()
	}
}

func (g *EdgeGateway) handleEdgeVoice(grant DeviceGrant, inner []byte, now time.Time) {
	if now.IsZero() {
		now = time.Now()
	}
	link := g.currentControl(true)
	if link == nil {
		if g.allowFallbackVoice(grant, now) {
			g.localFanout(grant.SessionID, grant.DomainID, inner, grant.GroupID)
		}
		return
	}

	var claim *SpeakerLeaseControl
	leaseID := uint64(0)
	deliver := false
	g.mu.Lock()
	session := g.sessions[grant.SessionID]
	if session == nil || session.Grant.SessionEpoch != grant.SessionEpoch || session.Grant.DomainID != grant.DomainID || session.Grant.DisableSend || grant.DomainID == 0 {
		g.mu.Unlock()
		return
	}
	ghostsession.Global.MarkPTTActive(session.Grant.GhostSessionID, now)
	state := g.speakerDomains[grant.DomainID]
	if state != nil && state.leaseID != 0 {
		active := !state.fallback && now.Before(state.expiresAt) && now.Sub(state.lastVoiceAt) <= SpeakerLeaseIdleTimeout
		if active && state.sessionID == grant.SessionID && state.sessionEpoch == grant.SessionEpoch {
			state.lastVoiceAt = now
			leaseID, deliver = state.leaseID, true
			if state.pendingRequest == 0 && state.expiresAt.Sub(now) <= SpeakerLeaseRenewBefore {
				requestID := g.nextRequest.Add(1)
				state.pendingRequest, state.pendingSince = requestID, now
				message := SpeakerLeaseControl{Action: SpeakerLeaseActionClaim, RequestID: requestID, SessionID: grant.SessionID, SessionEpoch: grant.SessionEpoch, DomainID: grant.DomainID, LeaseID: state.leaseID}
				claim = &message
			}
			g.mu.Unlock()
			if claim != nil {
				g.sendSpeakerClaim(link, *claim)
			}
			if deliver {
				g.deliverEdgeVoice(grant, inner, leaseID)
			}
			return
		}
		if active {
			g.mu.Unlock()
			g.metrics.AddDrop()
			return
		}
		delete(g.speakerDomains, grant.DomainID)
		state = nil
	}
	if state != nil && state.pendingRequest != 0 {
		buffered := false
		if state.sessionID == grant.SessionID && state.sessionEpoch == grant.SessionEpoch && len(state.buffered) < 2 && now.Sub(state.pendingSince) <= SpeakerClaimTimeout {
			state.buffered = append(state.buffered, edgeBufferedVoice{grant: grant, inner: append([]byte(nil), inner...), receivedAt: now})
			buffered = true
		}
		g.mu.Unlock()
		if !buffered {
			g.metrics.AddDrop()
		}
		return
	}
	if state != nil && now.Before(state.blockedUntil) {
		g.mu.Unlock()
		g.metrics.AddDrop()
		return
	}
	requestID := g.nextRequest.Add(1)
	state = &edgeSpeakerState{
		sessionID: grant.SessionID, sessionEpoch: grant.SessionEpoch,
		pendingRequest: requestID, pendingSince: now, lastVoiceAt: now,
		buffered: []edgeBufferedVoice{{grant: grant, inner: append([]byte(nil), inner...), receivedAt: now}},
	}
	g.speakerDomains[grant.DomainID] = state
	message := SpeakerLeaseControl{Action: SpeakerLeaseActionClaim, RequestID: requestID, SessionID: grant.SessionID, SessionEpoch: grant.SessionEpoch, DomainID: grant.DomainID}
	g.mu.Unlock()
	g.sendSpeakerClaim(link, message)
}

func (g *EdgeGateway) sendSpeakerClaim(link *edgeControlLink, message SpeakerLeaseControl) {
	if link == nil || link.client == nil || message.Validate() != nil {
		return
	}
	payload, err := EncodeJSON(message)
	if err == nil {
		env := NewEnvelope(SubtypeSpeakerLease, link.client.Session.NodeID, link.client.Session.SessionID, message.RequestID, payload)
		env.Flags = FlagControl | FlagAck
		err = link.client.SendEnvelope(env)
	}
	if err == nil {
		return
	}
	g.metrics.AddError()
	g.mu.Lock()
	state := g.speakerDomains[message.DomainID]
	if state != nil && state.pendingRequest == message.RequestID {
		state.pendingRequest = 0
		state.pendingSince = time.Time{}
		if state.leaseID == 0 {
			state.buffered = nil
			state.blockedUntil = time.Now().Add(50 * time.Millisecond)
		}
	}
	g.mu.Unlock()
}

func (g *EdgeGateway) finishSpeakerLease(message SpeakerLeaseControl, now time.Time) bool {
	if message.Action != SpeakerLeaseActionGrant && message.Action != SpeakerLeaseActionDeny {
		return false
	}
	if now.IsZero() {
		now = time.Now()
	}
	frames := make([]edgeBufferedVoice, 0, 2)
	g.mu.Lock()
	state := g.speakerDomains[message.DomainID]
	if state == nil || state.pendingRequest != message.RequestID || state.sessionID != message.SessionID || state.sessionEpoch != message.SessionEpoch {
		g.mu.Unlock()
		return false
	}
	session := g.sessions[message.SessionID]
	if session == nil || session.Grant.SessionEpoch != message.SessionEpoch || session.Grant.DomainID != message.DomainID || session.Grant.DisableSend {
		delete(g.speakerDomains, message.DomainID)
		g.mu.Unlock()
		return false
	}
	if message.Action == SpeakerLeaseActionDeny {
		retry := time.Duration(message.RetryAfterMillis) * time.Millisecond
		if retry < 50*time.Millisecond {
			retry = 50 * time.Millisecond
		}
		if retry > SpeakerLeaseTTL {
			retry = SpeakerLeaseTTL
		}
		dropped := len(state.buffered)
		g.speakerDomains[message.DomainID] = &edgeSpeakerState{sessionID: message.SessionID, sessionEpoch: message.SessionEpoch, blockedUntil: now.Add(retry)}
		g.mu.Unlock()
		if dropped > 0 {
			g.metrics.AddDropBulk(uint64(dropped))
		}
		return true
	}
	ttl := time.Duration(message.TTLMillis) * time.Millisecond
	if ttl <= 0 {
		delete(g.speakerDomains, message.DomainID)
		g.mu.Unlock()
		return false
	}
	if ttl > 2*SpeakerLeaseTTL {
		ttl = 2 * SpeakerLeaseTTL
	}
	frames = append(frames, state.buffered...)
	state.buffered = nil
	state.leaseID = message.LeaseID
	state.expiresAt = now.Add(ttl)
	state.lastVoiceAt = now
	state.pendingRequest = 0
	state.pendingSince = time.Time{}
	state.blockedUntil = time.Time{}
	state.fallback = false
	g.mu.Unlock()
	for _, frame := range frames {
		if now.Sub(frame.receivedAt) <= SpeakerClaimTimeout {
			g.deliverEdgeVoice(frame.grant, frame.inner, message.LeaseID)
		} else {
			g.metrics.AddDrop()
		}
	}
	return true
}

func (g *EdgeGateway) deliverEdgeVoice(grant DeviceGrant, inner []byte, leaseID uint64) {
	if leaseID == 0 {
		return
	}
	g.mu.RLock()
	session := g.sessions[grant.SessionID]
	state := g.speakerDomains[grant.DomainID]
	valid := session != nil && session.Grant.SessionEpoch == grant.SessionEpoch && session.Grant.DomainID == grant.DomainID && !session.Grant.DisableSend &&
		state != nil && !state.fallback && state.sessionID == grant.SessionID && state.sessionEpoch == grant.SessionEpoch && state.leaseID == leaseID
	if valid {
		grant = session.Grant
	}
	g.mu.RUnlock()
	if !valid {
		return
	}
	g.localFanout(grant.SessionID, grant.DomainID, inner, grant.GroupID)
	g.sendEdgeRelay(grant, inner, leaseID)
}

func (g *EdgeGateway) allowFallbackVoice(grant DeviceGrant, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ensureSessionIndexesLocked()
	session := g.sessions[grant.SessionID]
	if session == nil || session.Grant.SessionEpoch != grant.SessionEpoch || session.Grant.DomainID != grant.DomainID || session.Grant.DisableSend || grant.DomainID == 0 {
		return false
	}
	state := g.speakerDomains[grant.DomainID]
	if state == nil || !state.fallback || now.Sub(state.lastVoiceAt) > SpeakerLeaseIdleTimeout {
		g.speakerDomains[grant.DomainID] = &edgeSpeakerState{sessionID: grant.SessionID, sessionEpoch: grant.SessionEpoch, lastVoiceAt: now, fallback: true}
		return true
	}
	if state.sessionID != grant.SessionID || state.sessionEpoch != grant.SessionEpoch {
		return false
	}
	state.lastVoiceAt = now
	return true
}

func (g *EdgeGateway) speakerLeaseCleanerLoop() {
	defer g.cleanerWG.Done()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-g.closed:
			return
		case now := <-ticker.C:
			g.expireSpeakerStates(now)
		}
	}
}

func (g *EdgeGateway) expireSpeakerStates(now time.Time) {
	dropped := 0
	g.mu.Lock()
	for domainID, state := range g.speakerDomains {
		if state.fallback && now.Sub(state.lastVoiceAt) > SpeakerLeaseIdleTimeout {
			delete(g.speakerDomains, domainID)
			continue
		}
		if state.pendingRequest != 0 && now.Sub(state.pendingSince) > SpeakerClaimTimeout {
			dropped += len(state.buffered)
			state.buffered = nil
			state.pendingRequest = 0
			state.pendingSince = time.Time{}
			if state.leaseID == 0 {
				state.blockedUntil = now.Add(50 * time.Millisecond)
			}
		}
		if state.leaseID != 0 && (!now.Before(state.expiresAt) || now.Sub(state.lastVoiceAt) > SpeakerLeaseIdleTimeout) {
			delete(g.speakerDomains, domainID)
			continue
		}
		if state.leaseID == 0 && state.pendingRequest == 0 && !state.blockedUntil.IsZero() && !now.Before(state.blockedUntil) {
			delete(g.speakerDomains, domainID)
		}
	}
	g.mu.Unlock()
	if dropped > 0 {
		g.metrics.AddDropBulk(uint64(dropped))
	}
}

func (g *EdgeGateway) removeSpeakerSessionLocked(sessionID, sessionEpoch uint64) {
	for domainID, state := range g.speakerDomains {
		if state.sessionID == sessionID && (sessionEpoch == 0 || state.sessionEpoch == sessionEpoch) {
			delete(g.speakerDomains, domainID)
		}
	}
}

func (g *EdgeGateway) localFanout(sourceSession, domainID uint64, data []byte, sourceGroups ...int) {
	sourceGroupID := 0
	if len(sourceGroups) > 0 {
		sourceGroupID = sourceGroups[0]
	} else if len(data) >= protocol.DraARLv1HeaderSize {
		sourceGroupID = int(protocol.ReservedUint32(data[protocol.DraARLv1ReservedOffset:protocol.DraARLv1HeaderSize]))
	}
	physicalData := data
	if len(data) >= protocol.DraARLv1HeaderSize && protocol.ReservedUint32(data[protocol.DraARLv1ReservedOffset:protocol.DraARLv1HeaderSize]) != 0 {
		if cleared, ok := protocol.WithReservedUint32(data, 0); ok {
			physicalData = cleared
		}
	}
	if g.endpoint != nil {
		onComplete := func(result udphub.EdgeFanoutResult) {
			if result.Sent > 0 {
				g.metrics.AddOutBulk(uint64(result.Sent), uint64(result.Sent)*uint64(len(physicalData)))
			}
			if result.Errors > 0 {
				g.metrics.AddErrorBulk(uint64(result.Errors))
			}
			if result.Dropped > 0 {
				g.metrics.AddDropBulk(uint64(result.Dropped))
			}
		}
		for attempts := 0; attempts < 3; attempts++ {
			plan := g.receiverPlan(domainID)
			if plan == nil {
				return
			}
			sourceID, sourceUser, sourceSSID := 0, "", byte(0)
			if sourceSession != 0 {
				g.mu.RLock()
				if source := g.sessions[sourceSession]; source != nil {
					sourceID, sourceUser, sourceSSID = source.Grant.DeviceID, source.Grant.Username, source.Grant.SSID
				}
				g.mu.RUnlock()
			}
			if sourceSession != 0 || sourceGroupID != 0 {
				if g.endpoint.FanoutSessionPlan(physicalData, plan, sourceSession, sourceGroupID, onComplete) {
					return
				}
			} else if g.endpoint.FanoutPlan(physicalData, plan, sourceID, sourceUser, sourceSSID, onComplete) {
				return
			}
		}
		return
	}

	g.mu.RLock()
	targets := make([]udphub.EdgeFanoutTarget, 0, len(g.sessions))
	for id, session := range g.sessions {
		if id != sourceSession && slices.Contains(routeReceiveDomains(session.Grant.Route()), domainID) && !session.Grant.DisableRecv && session.Addr != nil {
			targets = append(targets, udphub.EdgeFanoutTarget{Addr: cloneUDPAddr(session.Addr), DeviceID: session.Grant.DeviceID, Username: session.Grant.Username, SSID: session.Grant.SSID, SessionID: id, SourceGroupV1: session.Grant.SourceGroupV1})
		}
	}
	g.mu.RUnlock()
	for _, target := range targets {
		payload := physicalData
		if target.SourceGroupV1 && sourceGroupID > 0 {
			if tagged, ok := protocol.WithSourceGroupID(physicalData, sourceGroupID); ok {
				payload = tagged
			}
		}
		g.writeDevice(payload, target.Addr)
	}
}
