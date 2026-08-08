package interconnect

import (
	"errors"
	"net"
	"slices"
	"sync/atomic"
	"time"

	"draarl/internal/protocol"
	"draarl/internal/udphub"
)

const edgeReceiverCacheTTL = 30 * time.Second

type edgeReceiverSnapshot struct {
	generation uint64
	builtAt    time.Time
	plans      map[uint64]*udphub.EdgeFanoutPlan
}

type EdgeReceiverCacheSnapshot struct {
	Hits       uint64 `json:"hits"`
	Misses     uint64 `json:"misses"`
	Rebuilds   uint64 `json:"rebuilds"`
	BuildNanos uint64 `json:"build_ns"`
	MaxEntries uint64 `json:"max_entries"`
	Generation uint64 `json:"generation"`
}

type pendingDeviceConfigUp struct {
	envelope     Envelope
	sessionID    uint64
	sessionEpoch uint64
	requestedAt  time.Time
	lastSentAt   time.Time
	attempts     int
}

type pendingDownstreamFrame struct {
	envelope Envelope
	frame    RelayFrame
}

const maxEdgePendingDeviceConfigs = 256

func (g *EdgeGateway) ReceiverCacheSnapshot() EdgeReceiverCacheSnapshot {
	return EdgeReceiverCacheSnapshot{
		Hits:       g.receiverHits.Load(),
		Misses:     g.receiverMisses.Load(),
		Rebuilds:   g.receiverRebuilds.Load(),
		BuildNanos: g.receiverBuildNS.Load(),
		MaxEntries: g.receiverMaxEntries.Load(),
		Generation: g.receiverGen.Load(),
	}
}

func (g *EdgeGateway) invalidateReceiverPlansLocked() {
	g.receiverGen.Add(1)
	if g.endpoint != nil {
		g.endpoint.InvalidateFanoutPlans()
	}
}

func updateEdgeReceiverMax(target *atomic.Uint64, value uint64) {
	for {
		current := target.Load()
		if value <= current || target.CompareAndSwap(current, value) {
			return
		}
	}
}

func (g *EdgeGateway) receiverPlan(domainID uint64) *udphub.EdgeFanoutPlan {
	if domainID == 0 || g.endpoint == nil {
		return nil
	}
	generation := g.receiverGen.Load()
	if snapshot := g.receiverCache.Load(); snapshot != nil && snapshot.generation == generation && time.Since(snapshot.builtAt) < edgeReceiverCacheTTL {
		g.receiverHits.Add(1)
		return snapshot.plans[domainID]
	}

	g.receiverMisses.Add(1)
	g.receiverBuildMu.Lock()
	defer g.receiverBuildMu.Unlock()
	for attempts := 0; attempts < 3; attempts++ {
		generation = g.receiverGen.Load()
		if snapshot := g.receiverCache.Load(); snapshot != nil && snapshot.generation == generation && time.Since(snapshot.builtAt) < edgeReceiverCacheTTL {
			g.receiverHits.Add(1)
			return snapshot.plans[domainID]
		}

		started := time.Now()
		targetsByDomain := make(map[uint64][]udphub.EdgeFanoutTarget)
		seenByDomain := make(map[uint64]map[uint64]struct{})
		g.mu.RLock()
		for _, session := range g.sessions {
			if session == nil || session.Grant.DisableRecv || session.Addr == nil {
				continue
			}
			for _, domain := range routeReceiveDomains(session.Grant.Route()) {
				if domain == 0 {
					continue
				}
				if seenByDomain[domain] == nil {
					seenByDomain[domain] = make(map[uint64]struct{})
				}
				if _, duplicate := seenByDomain[domain][session.Grant.SessionID]; duplicate {
					continue
				}
				target, ok := udphub.NewEdgeSessionFanoutTarget(session.Addr, session.Grant.SessionID, session.Grant.DeviceID, session.Grant.Username, session.Grant.SSID, session.Grant.SourceGroupV1)
				if ok {
					seenByDomain[domain][session.Grant.SessionID] = struct{}{}
					targetsByDomain[domain] = append(targetsByDomain[domain], target)
				}
			}
		}
		g.mu.RUnlock()

		plans := make(map[uint64]*udphub.EdgeFanoutPlan, len(targetsByDomain))
		var maxEntries uint64
		for domain, targets := range targetsByDomain {
			plan := g.endpoint.PrepareFanout(targets)
			if plan == nil {
				continue
			}
			plans[domain] = plan
			if entries := uint64(plan.Len()); entries > maxEntries {
				maxEntries = entries
			}
		}
		if generation != g.receiverGen.Load() {
			continue
		}
		g.receiverCache.Store(&edgeReceiverSnapshot{generation: generation, builtAt: time.Now(), plans: plans})
		g.receiverRebuilds.Add(1)
		g.receiverBuildNS.Add(uint64(time.Since(started)))
		updateEdgeReceiverMax(&g.receiverMaxEntries, maxEntries)
		return plans[domainID]
	}
	return nil
}

func (g *EdgeGateway) requestDeviceConfigUp(kind string, grant DeviceGrant, data []byte) {
	if grant.SessionID == 0 || grant.SessionEpoch == 0 || grant.DeviceID <= 0 {
		return
	}
	link := g.currentControl(true)
	if link == nil {
		return
	}
	message := DeviceConfigControl{
		Kind: kind, SessionID: grant.SessionID, SessionEpoch: grant.SessionEpoch,
		DeviceID: grant.DeviceID, Data: append([]byte(nil), data...),
	}
	if err := message.Validate(); err != nil {
		return
	}
	requestID := g.nextRequest.Add(1)
	payload, err := EncodeJSON(message)
	if err != nil {
		return
	}
	env := NewEnvelope(SubtypeDeviceConfig, link.client.Session.NodeID, link.client.Session.SessionID, requestID, payload)
	env.Flags = FlagControl | FlagAck
	now := time.Now()
	g.mu.Lock()
	session := g.sessions[grant.SessionID]
	if session == nil || session.Grant.SessionEpoch != grant.SessionEpoch || len(g.pendingConfigUp) >= maxEdgePendingDeviceConfigs {
		g.mu.Unlock()
		g.metrics.AddDrop()
		return
	}
	g.pendingConfigUp[requestID] = &pendingDeviceConfigUp{
		envelope: env, sessionID: grant.SessionID, sessionEpoch: grant.SessionEpoch,
		requestedAt: now, lastSentAt: now, attempts: 1,
	}
	g.mu.Unlock()
	if err := link.client.SendEnvelope(env); err != nil {
		g.mu.Lock()
		delete(g.pendingConfigUp, requestID)
		g.mu.Unlock()
		g.metrics.AddError()
	}
}

func (g *EdgeGateway) finishDeviceConfigUp(message DeviceConfigControl) bool {
	g.mu.Lock()
	pending := g.pendingConfigUp[message.AckForMessageID]
	if pending == nil || pending.sessionID != message.SessionID || pending.sessionEpoch != message.SessionEpoch {
		g.mu.Unlock()
		return false
	}
	session := g.sessions[pending.sessionID]
	if session == nil || session.Grant.SessionEpoch != pending.sessionEpoch || session.Grant.DeviceID != message.DeviceID {
		g.mu.Unlock()
		return false
	}
	delete(g.pendingConfigUp, message.AckForMessageID)
	g.mu.Unlock()
	if !message.Success {
		g.metrics.AddError()
	}
	return true
}

func (g *EdgeGateway) handleDeviceConfigDown(env Envelope, message DeviceConfigControl) {
	if env.MessageID == 0 {
		return
	}
	if env.Duplicate {
		g.mu.RLock()
		cached, ok := g.configDownResults[env.MessageID]
		g.mu.RUnlock()
		if ok {
			g.sendEdgeDeviceConfigResult(cached.message)
		}
		return
	}
	result := DeviceConfigControl{
		Kind: DeviceConfigKindResult, SessionID: message.SessionID, SessionEpoch: message.SessionEpoch,
		DeviceID: message.DeviceID, AckForMessageID: env.MessageID,
	}
	var target *net.UDPAddr
	g.mu.RLock()
	session := g.sessions[message.SessionID]
	if session != nil && session.Grant.SessionEpoch == message.SessionEpoch && session.Grant.DeviceID == message.DeviceID {
		target = cloneUDPAddr(session.Addr)
	}
	var grant DeviceGrant
	if session != nil {
		grant = session.Grant
	}
	g.mu.RUnlock()
	if target == nil {
		result.Error = "session_not_found"
	} else if err := validateEdgeDeviceConfigPacket(message.Packet, grant); err != nil {
		result.Error = "invalid_packet"
	} else if err := g.writeDeviceResult(message.Packet, target); err != nil {
		result.Error = "delivery_failed"
	} else {
		result.Success = true
	}
	g.cacheDeviceConfigDownResult(env.MessageID, result)
	g.sendEdgeDeviceConfigResult(result)
}

func validateEdgeDeviceConfigPacket(packet []byte, grant DeviceGrant) error {
	decoded, err := protocol.NewDraARLv1Packet(nil, packet)
	if err != nil {
		return err
	}
	if decoded.Type != protocol.DraARLTypeConfig || decoded.SSID != grant.SSID || decoded.Username != grant.Username || decoded.CallSign != grant.CallSign {
		return errors.New("device config identity mismatch")
	}
	return nil
}

func (g *EdgeGateway) cacheDeviceConfigDownResult(messageID uint64, result DeviceConfigControl) {
	now := time.Now()
	g.mu.Lock()
	if len(g.configDownResults) >= maxEdgePendingDeviceConfigs {
		var oldestID uint64
		var oldestAt time.Time
		for id, cached := range g.configDownResults {
			if oldestAt.IsZero() || cached.storedAt.Before(oldestAt) {
				oldestID, oldestAt = id, cached.storedAt
			}
		}
		delete(g.configDownResults, oldestID)
	}
	g.configDownResults[messageID] = cachedDeviceConfigResult{message: result, storedAt: now}
	g.mu.Unlock()
}

func (g *EdgeGateway) sendEdgeDeviceConfigResult(result DeviceConfigControl) {
	link := g.currentControl(false)
	if link == nil {
		return
	}
	payload, err := EncodeJSON(result)
	if err != nil {
		return
	}
	env := NewEnvelope(SubtypeDeviceConfig, link.client.Session.NodeID, link.client.Session.SessionID, g.nextRequest.Add(1), payload)
	env.Flags = FlagControl | FlagAck
	_ = link.client.SendEnvelope(env)
}

func (g *EdgeGateway) removePendingDeviceConfigsLocked(sessionID uint64) {
	for requestID, pending := range g.pendingConfigUp {
		if pending.sessionID == sessionID {
			delete(g.pendingConfigUp, requestID)
		}
	}
}

func applyRouteToGrant(grant *DeviceGrant, route DeviceRoute) {
	if grant == nil {
		return
	}
	grant.DisableSend, grant.DisableRecv = route.DisableSend, route.DisableRecv
	grant.GroupID, grant.DomainID = route.GroupID, route.DomainID
	grant.RxGroupIDs = append(grant.RxGroupIDs[:0], route.RxGroupIDs...)
	grant.RxDomainIDs = append(grant.RxDomainIDs[:0], route.RxDomainIDs...)
	grant.GhostSessionID, grant.ClientInstanceID = route.GhostSessionID, route.ClientInstanceID
	grant.SessionTag, grant.GhostProtocolVersion = route.SessionTag, route.GhostProtocolVersion
	grant.SourceGroupV1, grant.SessionEpoch = route.SourceGroupV1, route.SessionEpoch
}

func (g *EdgeGateway) applyRoutes(p *Projection) {
	if p == nil {
		return
	}
	g.mu.Lock()
	g.ensureSessionIndexesLocked()
	receiversChanged := false
	for id, session := range g.sessions {
		if route, ok := p.Devices[id]; ok {
			if session.Grant.SessionEpoch != route.SessionEpoch || session.Grant.DomainID != route.DomainID || route.DisableSend {
				g.removeSpeakerSessionLocked(id, session.Grant.SessionEpoch)
			}
			if !slices.Equal(routeReceiveDomains(session.Grant.Route()), routeReceiveDomains(route)) || session.Grant.DisableRecv != route.DisableRecv || session.Grant.SourceGroupV1 != route.SourceGroupV1 {
				receiversChanged = true
			}
			oldKey, oldTag := edgeSessionIdentity(session.Grant), session.Grant.SessionTag
			applyRouteToGrant(&session.Grant, route)
			newKey := edgeSessionIdentity(session.Grant)
			if oldKey != newKey && g.byIdentity[oldKey] == id {
				delete(g.byIdentity, oldKey)
				g.byIdentity[newKey] = id
			}
			if oldTag != session.Grant.SessionTag {
				if oldTag != 0 && g.bySessionTag[oldTag] == id {
					delete(g.bySessionTag, oldTag)
				}
				if session.Grant.SessionTag != 0 {
					g.bySessionTag[session.Grant.SessionTag] = id
				}
			}
		} else {
			receiversChanged = true
			g.removeSpeakerSessionLocked(id, session.Grant.SessionEpoch)
			delete(g.sessions, id)
			g.removePendingDeviceConfigsLocked(id)
			key := edgeSessionIdentity(session.Grant)
			if g.byIdentity[key] == id {
				delete(g.byIdentity, key)
			}
			if session.Grant.SessionTag != 0 && g.bySessionTag[session.Grant.SessionTag] == id {
				delete(g.bySessionTag, session.Grant.SessionTag)
			}
		}
	}
	if receiversChanged {
		g.invalidateReceiverPlansLocked()
	}
	g.mu.Unlock()
}

func cloneUDPAddr(addr *net.UDPAddr) *net.UDPAddr {
	if addr == nil {
		return nil
	}
	copyAddr := *addr
	copyAddr.IP = append(net.IP(nil), addr.IP...)
	return &copyAddr
}

func udpAddrEqual(a, b *net.UDPAddr) bool {
	if a == nil || b == nil || a.Port != b.Port || a.Zone != b.Zone {
		return false
	}
	return a.IP.Equal(b.IP)
}
func (g *EdgeGateway) deliverDownstream(env Envelope, frame RelayFrame) {
	now := time.Now()
	if env.locallyExpired(now, g.downstreamMaxAge) {
		g.metrics.AddDrop()
		return
	}
	p := g.projection.Snapshot()
	if p.ClusterEpoch != env.ClusterEpoch || env.ProjectionVersion != frame.RequiredProjectionVersion {
		g.metrics.AddDrop()
		return
	}
	if p.Version < frame.RequiredProjectionVersion {
		g.downstreamMu.Lock()
		if len(g.pendingDownstream) >= 1024 {
			g.downstreamMu.Unlock()
			g.metrics.AddDrop()
			return
		}
		g.pendingDownstream = append(g.pendingDownstream, pendingDownstreamFrame{envelope: env, frame: frame})
		g.downstreamMu.Unlock()
		select {
		case g.downstreamWake <- struct{}{}:
		default:
		}
		return
	}
	g.localFanout(0, frame.DomainID, frame.InnerPacket)
}

func (g *EdgeGateway) downstreamBarrierLoop() {
	defer g.cleanerWG.Done()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-g.closed:
			return
		case now := <-ticker.C:
			g.drainDownstream(now)
		case <-g.downstreamWake:
			g.drainDownstream(time.Now())
		}
	}
}

func (g *EdgeGateway) drainDownstream(now time.Time) {
	p := g.projection.Snapshot()
	ready := make([]RelayFrame, 0)
	dropped := 0
	g.downstreamMu.Lock()
	remaining := g.pendingDownstream[:0]
	for _, pending := range g.pendingDownstream {
		if pending.envelope.locallyExpired(now, g.downstreamMaxAge) || pending.envelope.ClusterEpoch != p.ClusterEpoch {
			dropped++
			continue
		}
		if p.Version >= pending.frame.RequiredProjectionVersion {
			ready = append(ready, pending.frame)
			continue
		}
		remaining = append(remaining, pending)
	}
	g.pendingDownstream = remaining
	g.downstreamMu.Unlock()
	if dropped > 0 {
		g.metrics.AddDropBulk(uint64(dropped))
	}
	if g.currentControl(true) == nil {
		if len(ready) > 0 {
			g.metrics.AddDropBulk(uint64(len(ready)))
		}
		return
	}
	for _, frame := range ready {
		g.localFanout(0, frame.DomainID, frame.InnerPacket)
	}
}

func (g *EdgeGateway) clearPendingDownstream() {
	g.downstreamMu.Lock()
	dropped := len(g.pendingDownstream)
	g.pendingDownstream = nil
	g.downstreamMu.Unlock()
	if dropped > 0 {
		g.metrics.AddDropBulk(uint64(dropped))
	}
}
func (g *EdgeGateway) sendRouteAck(version uint64, routeErr string, ackFor uint64) {
	link := g.currentControl(false)
	if link == nil {
		return
	}
	p := g.projection.Snapshot()
	payload, _ := EncodeJSON(RouteAck{ClusterEpoch: p.ClusterEpoch, ProjectionVersion: version, AckForMessageID: ackFor, Error: routeErr})
	env := NewEnvelope(SubtypeRouteAck, link.client.Session.NodeID, link.client.Session.SessionID, g.nextRequest.Add(1), payload)
	env.Flags = FlagControl | FlagAck
	_ = link.client.SendEnvelope(env)
}
func (g *EdgeGateway) requestResync(reason string) {
	link := g.currentControl(false)
	if link == nil {
		return
	}
	link.ready.Store(false)
	g.clearPendingDownstream()
	p := g.projection.Snapshot()
	payload, _ := EncodeJSON(ResyncRequest{ClusterEpoch: p.ClusterEpoch, ProjectionVersion: p.Version, Reason: reason})
	env := NewEnvelope(SubtypeRouteResyncRequest, link.client.Session.NodeID, link.client.Session.SessionID, g.nextRequest.Add(1), payload)
	env.Flags = FlagControl | FlagAck
	_ = link.client.SendEnvelope(env)
}
func (g *EdgeGateway) writeDevice(data []byte, addr *net.UDPAddr) {
	_ = g.writeDeviceResult(data, addr)
}

func (g *EdgeGateway) writeDeviceResult(data []byte, addr *net.UDPAddr) error {
	if g.endpoint == nil || addr == nil {
		return errors.New("edge device endpoint is not ready")
	}
	if err := g.endpoint.SendTo(data, addr); err == nil {
		g.metrics.AddOut(len(data))
		return nil
	} else {
		g.metrics.AddError()
		return err
	}
}
