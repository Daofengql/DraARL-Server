package interconnect

import (
	"errors"
	"net"
	"slices"
	"sync/atomic"
	"time"

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

type pendingDownstreamFrame struct {
	envelope Envelope
	frame    RelayFrame
}

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
