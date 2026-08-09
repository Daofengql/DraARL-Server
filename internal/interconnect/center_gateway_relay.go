package interconnect

import (
	"errors"
	"time"

	"draarl/internal/ghostsession"
	"draarl/internal/protocol"
)

func (g *CenterGateway) handleRelayUpstream(session *NodeSession, env Envelope) bool {
	if session == nil || g.cluster == nil {
		return false
	}
	frame, err := UnmarshalRelayFrame(env.Payload)
	if err != nil || protocol.ValidateRelayInnerPacket(frame.InnerPacket) != nil {
		return false
	}
	g.mu.RLock()
	owner := g.deviceSessions[frame.SessionID]
	onAcceptedRelay := g.onAcceptedRelay
	g.mu.RUnlock()
	if owner.NodeID != session.NodeID || owner.ControlSessionID != session.SessionID {
		return false
	}
	route, ok := g.cluster.ResolveRoute(frame.SessionID)
	if !ok || route.DisableSend || route.SessionEpoch != frame.SessionEpoch || route.DomainID == 0 {
		return false
	}
	if !protocol.RelayInnerIdentityMatches(frame.InnerPacket, route.Username, route.CallSign, route.SSID) {
		return false
	}
	// Node payload is never authoritative for routing or permissions.
	frame.DomainID = route.DomainID
	if frame.InnerPacket[48] == protocol.DraARLTypeOpus16K {
		if g.speaker == nil || !g.speaker.AcceptFrame(session.NodeID, session.SessionID, frame, time.Now()) {
			return false
		}
		ghostsession.Global.MarkPTTActive(owner.GhostSessionID, time.Now())
	} else if frame.SpeakerLeaseID != 0 {
		return false
	}
	if onAcceptedRelay != nil {
		onAcceptedRelay(AcceptedRelay{
			SessionID: frame.SessionID, DeviceID: route.DeviceID, OwnerID: owner.OwnerID,
			Username: route.Username, CallSign: route.CallSign, Nickname: route.Nickname, SSID: route.SSID, DevModel: route.DevModel, GroupID: route.GroupID,
			Type: frame.InnerPacket[48], Payload: frame.InnerPacket[DraARLHeaderSize:],
		})
	}
	if tagged, ok := protocol.WithSourceGroupID(frame.InnerPacket, route.GroupID); ok {
		frame.InnerPacket = tagged
	}
	_ = g.cluster.Relay(session.NodeID, frame)
	return true
}

func (g *CenterGateway) handleSpeakerLease(session *NodeSession, message SpeakerLeaseControl) {
	if session == nil || g.speaker == nil {
		return
	}
	if message.Action == SpeakerLeaseActionRelease {
		g.speaker.Release(session.NodeID, session.SessionID, message)
		return
	}
	if message.Action != SpeakerLeaseActionClaim {
		return
	}
	eligible := false
	if g.cluster != nil {
		g.mu.RLock()
		owner, ok := g.deviceSessions[message.SessionID]
		g.mu.RUnlock()
		if ok && owner.NodeID == session.NodeID && owner.ControlSessionID == session.SessionID && owner.SessionEpoch == message.SessionEpoch {
			route, routeOK := g.cluster.ResolveRoute(message.SessionID)
			eligible = routeOK && route.SessionEpoch == message.SessionEpoch && route.DomainID == message.DomainID && !route.DisableSend
		}
	}
	response := message
	response.Action = SpeakerLeaseActionDeny
	response.LeaseID = 0
	response.TTLMillis = 0
	response.RetryAfterMillis = 0
	if eligible {
		response = g.speaker.Claim(session.NodeID, session.SessionID, message, time.Now())
	}
	payload, err := EncodeJSON(response)
	if err != nil || g.server == nil || g.cluster == nil {
		return
	}
	env := NewEnvelope(SubtypeSpeakerLease, "center", 0, g.cluster.NextMessageID(), payload)
	env.ClusterEpoch = g.cluster.Epoch()
	env.Flags = FlagControl | FlagAck
	_ = g.server.SendEnvelope(session.NodeID, env)
}

func (g *CenterGateway) RelayLocalDevice(grant DeviceGrant, inner []byte) error {
	if g.cluster == nil || grant.SessionID == 0 || grant.SessionEpoch == 0 {
		return errors.New("local relay session is incomplete")
	}
	if err := protocol.ValidateRelayInnerPacket(inner); err != nil {
		return err
	}
	g.mu.RLock()
	owner, ok := g.deviceSessions[grant.SessionID]
	g.mu.RUnlock()
	if !ok || owner.NodeID != CenterLocalNodeID || owner.SessionEpoch != grant.SessionEpoch {
		return errors.New("local relay session is not authoritative")
	}
	route, ok := g.cluster.ResolveRoute(grant.SessionID)
	if !ok || route.SessionEpoch != grant.SessionEpoch || route.DisableSend || route.DomainID == 0 {
		return errors.New("local relay route is not eligible")
	}
	if !protocol.RelayInnerIdentityMatches(inner, route.Username, route.CallSign, route.SSID) {
		return errors.New("local relay identity mismatch")
	}
	leaseID := uint64(0)
	if inner[48] == protocol.DraARLTypeOpus16K {
		var allowed bool
		leaseID, allowed = g.speaker.CurrentLocal(route.SessionID, route.SessionEpoch, route.DomainID, time.Now())
		if !allowed {
			return errors.New("local speaker lease is not active")
		}
	}
	relayInner := inner
	if tagged, ok := protocol.WithSourceGroupID(inner, route.GroupID); ok {
		relayInner = tagged
	}
	return g.cluster.Relay(CenterLocalNodeID, RelayFrame{
		SessionID: route.SessionID, SessionEpoch: route.SessionEpoch, DomainID: route.DomainID,
		SpeakerLeaseID: leaseID, InnerPacket: relayInner,
	})
}

func (g *CenterGateway) AcquireLocalVoice(grant DeviceGrant) bool {
	if g.cluster == nil || g.speaker == nil || grant.SessionID == 0 || grant.SessionEpoch == 0 {
		return false
	}
	g.mu.RLock()
	owner, ok := g.deviceSessions[grant.SessionID]
	g.mu.RUnlock()
	if !ok || owner.NodeID != CenterLocalNodeID || owner.SessionEpoch != grant.SessionEpoch {
		return false
	}
	route, ok := g.cluster.ResolveRoute(grant.SessionID)
	if !ok || route.SessionEpoch != grant.SessionEpoch || route.DomainID == 0 || route.DisableSend {
		return false
	}
	now := time.Now()
	_, allowed := g.speaker.AcquireLocal(route.SessionID, route.SessionEpoch, route.DomainID, now)
	if allowed {
		ghostsession.Global.MarkPTTActive(grant.GhostSessionID, now)
	}
	return allowed
}

const scheduledBroadcastSessionPrefix = uint64(0xb000000000000000)

func scheduledBroadcastSessionID(runID uint) uint64 {
	return scheduledBroadcastSessionPrefix | (uint64(runID) & 0x0fffffffffffffff)
}

func (g *CenterGateway) AcquireScheduledBroadcast(runID uint, domainID uint64, now time.Time) bool {
	if g.speaker == nil || runID == 0 || domainID == 0 {
		return false
	}
	sessionID := scheduledBroadcastSessionID(runID)
	_, allowed := g.speaker.AcquireLocal(sessionID, 1, domainID, now)
	return allowed
}

func (g *CenterGateway) AcceptScheduledBroadcastFrame(runID uint, domainID uint64, now time.Time) bool {
	if g.speaker == nil || runID == 0 || domainID == 0 {
		return false
	}
	sessionID := scheduledBroadcastSessionID(runID)
	_, allowed := g.speaker.CurrentLocal(sessionID, 1, domainID, now)
	return allowed
}

func (g *CenterGateway) ReleaseScheduledBroadcast(runID uint, domainID uint64) {
	if g.speaker == nil || runID == 0 || domainID == 0 {
		return
	}
	sessionID := scheduledBroadcastSessionID(runID)
	g.speaker.ReleaseLocal(sessionID, 1, domainID)
}

func (g *CenterGateway) RelayScheduledBroadcast(runID uint, sourceGroupID int, domainID uint64, inner []byte) error {
	if g.cluster == nil || g.speaker == nil || runID == 0 || sourceGroupID <= 0 || domainID == 0 {
		return errors.New("scheduled broadcast relay is incomplete")
	}
	if err := protocol.ValidateRelayInnerPacket(inner); err != nil {
		return err
	}
	sessionID := scheduledBroadcastSessionID(runID)
	leaseID, allowed := g.speaker.CurrentLocal(sessionID, 1, domainID, time.Now())
	if !allowed {
		return errors.New("scheduled broadcast speaker lease is not active")
	}
	relayInner := inner
	if tagged, ok := protocol.WithSourceGroupID(inner, sourceGroupID); ok {
		relayInner = tagged
	}
	return g.cluster.Relay(CenterLocalNodeID, RelayFrame{
		SessionID: sessionID, SessionEpoch: 1, DomainID: domainID,
		SpeakerLeaseID: leaseID, InnerPacket: relayInner,
	})
}

func (g *CenterGateway) AuthorizeLocalDevice(grant DeviceGrant) bool {
	if grant.SessionID == 0 || grant.SessionEpoch == 0 || g.cluster == nil {
		return false
	}
	g.mu.RLock()
	owner, ok := g.deviceSessions[grant.SessionID]
	g.mu.RUnlock()
	if !ok || owner.NodeID != CenterLocalNodeID || owner.SessionEpoch != grant.SessionEpoch {
		return false
	}
	route, ok := g.cluster.ResolveRoute(grant.SessionID)
	return ok && route.SessionEpoch == grant.SessionEpoch
}
