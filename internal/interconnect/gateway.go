package interconnect

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"draarl/internal/protocol"
	"draarl/internal/udphub"
)

type DeviceAuthHandler func(session *NodeSession, request DeviceAuthRequest) (DeviceAuthResponse, error)

type CenterGateway struct {
	cluster        *ClusterManager
	server         *NodeServer
	data           *NodeDatagramBridge
	auth           DeviceAuthHandler
	mu             sync.RWMutex
	deviceSessions map[uint64]string
	metrics        map[string]*Metrics
}

func NewCenterGateway(cluster *ClusterManager, auth DeviceAuthHandler) *CenterGateway {
	return &CenterGateway{cluster: cluster, auth: auth, deviceSessions: make(map[uint64]string), metrics: make(map[string]*Metrics)}
}
func (g *CenterGateway) Bind(server *NodeServer, data *NodeDatagramBridge) {
	g.server, g.data = server, data
	if g.cluster != nil {
		g.cluster.AttachServer(server)
		g.cluster.AttachDataBridge(data)
	}
}
func (g *CenterGateway) OnConnect(session *NodeSession) {
	if g.cluster != nil {
		g.cluster.OnConnect(session)
	}
}
func (g *CenterGateway) OnDisconnect(session *NodeSession, err error) {
	if g.cluster != nil {
		g.cluster.OnDisconnect(session, err)
	}
	g.mu.Lock()
	for id, node := range g.deviceSessions {
		if node == session.NodeID {
			delete(g.deviceSessions, id)
		}
	}
	delete(g.metrics, session.NodeID)
	g.mu.Unlock()
}
func (g *CenterGateway) OnMessage(session *NodeSession, msg ControlMessage) {
	if msg.Kind == "node_ready" {
		if g.cluster != nil {
			_ = g.cluster.SendFullProjection(session.NodeID)
		}
		return
	}
	if msg.Kind == controlHeartbeat {
		var heartbeat NodeHeartbeat
		if DecodeJSON(msg.Payload, &heartbeat) == nil {
			if g.cluster != nil {
				g.cluster.UpdateNodeHeartbeat(session.NodeID, heartbeat)
			}
			if g.cluster != nil {
				g.mu.Lock()
				g.metrics[session.NodeID] = g.cluster.Metrics(session.NodeID)
				g.mu.Unlock()
			}
		}
	}
}
func (g *CenterGateway) OnDatagram(session *NodeSession, env Envelope, _ *net.UDPAddr) {
	g.handleEnvelope(session, env)
}
func (g *CenterGateway) OnEnvelope(session *NodeSession, env Envelope) {
	g.handleEnvelope(session, env)
}

func (g *CenterGateway) handleEnvelope(session *NodeSession, env Envelope) {
	switch env.Subtype {
	case SubtypeDeviceAuth:
		var request DeviceAuthRequest
		if err := DecodeJSON(env.Payload, &request); err != nil || g.auth == nil {
			return
		}
		response, err := g.auth(session, request)
		if err != nil {
			response.Success = false
			response.Error = err.Error()
		}
		if response.Success && response.Grant != nil {
			if response.Grant.SessionID == 0 {
				response.Grant.SessionID = g.cluster.NextMessageID()
			}
			if response.Grant.SessionEpoch == 0 {
				response.Grant.SessionEpoch = 1
			}
			g.mu.Lock()
			g.deviceSessions[response.Grant.SessionID] = session.NodeID
			g.mu.Unlock()
			if g.cluster != nil {
				_ = g.cluster.SetNodeRoute(session.NodeID, response.Grant.Route())
			}
		}
		payload, _ := EncodeJSON(response)
		reply := NewEnvelope(SubtypeDeviceAuth, "center", 0, g.cluster.NextMessageID(), payload)
		reply.ClusterEpoch = g.cluster.Epoch()
		reply.Flags = FlagControl | FlagAck
		_ = g.server.SendEnvelope(session.NodeID, reply)
	case SubtypeRelayUpstream:
		frame, err := UnmarshalRelayFrame(env.Payload)
		if err != nil {
			return
		}
		g.mu.RLock()
		owner := g.deviceSessions[frame.SessionID]
		g.mu.RUnlock()
		if owner != session.NodeID {
			return
		}
		if g.cluster == nil {
			return
		}
		route, ok := g.cluster.ResolveRoute(frame.SessionID)
		if !ok || route.DisableSend || route.SessionEpoch != frame.SessionEpoch || route.DomainID == 0 {
			return
		}
		// Node payload is never authoritative for routing or permissions.
		frame.DomainID = route.DomainID
		if g.cluster != nil {
			_ = g.cluster.Relay(session.NodeID, frame)
		}
	case SubtypeRouteAck:
		var ack RouteAck
		if DecodeJSON(env.Payload, &ack) == nil && g.cluster != nil {
			g.cluster.MarkRouteAck(session.NodeID, ack)
		}
	case SubtypeRouteResyncRequest:
		if g.cluster != nil {
			_ = g.cluster.SendFullProjection(session.NodeID)
		}
	case SubtypeNodeHeartbeat:
		var heartbeat NodeHeartbeat
		if DecodeJSON(env.Payload, &heartbeat) == nil && g.cluster != nil {
			g.cluster.UpdateNodeHeartbeat(session.NodeID, heartbeat)
		}
	}
}

type EdgeGateway struct {
	client            *NodeClient
	listenAddr        string
	endpoint          *udphub.EdgeEndpoint
	peer              *NodeDatagramPeer
	projection        *ProjectionStore
	mu                sync.RWMutex
	sessions          map[uint64]*edgeDeviceSession
	byIdentity        map[string]uint64
	pending           map[uint64]*pendingDeviceAuth
	pendingIdentity   map[string]uint64
	snapshotAssembler *SnapshotAssembler
	nextRequest       atomic.Uint64
	metrics           Metrics
	closed            chan struct{}
}
type edgeDeviceSession struct {
	Grant    DeviceGrant
	Addr     *net.UDPAddr
	LastSeen time.Time
}
type pendingDeviceAuth struct {
	addr *net.UDPAddr
	wire []byte
}

func NewEdgeGateway(listenAddr string, client *NodeClient) (*EdgeGateway, error) {
	if client == nil {
		return nil, errors.New("edge node client is required")
	}
	if listenAddr == "" {
		listenAddr = ":60050"
	}
	p := NewProjection(1)
	return &EdgeGateway{client: client, listenAddr: listenAddr, projection: NewProjectionStore(p), sessions: make(map[uint64]*edgeDeviceSession), byIdentity: make(map[string]uint64), pending: make(map[uint64]*pendingDeviceAuth), pendingIdentity: make(map[string]uint64), closed: make(chan struct{})}, nil
}
func (g *EdgeGateway) Start() error {
	endpoint, err := udphub.NewEdgeEndpoint(g.listenAddr, g.handleInbound)
	if err != nil {
		return err
	}
	g.endpoint = endpoint
	if g.peer != nil {
		g.peer.SetWriter(func(addr *net.UDPAddr, data []byte) error {
			return endpoint.SendTo(data, addr)
		})
		if err := g.peer.Bind(); err != nil {
			_ = endpoint.Close()
			g.endpoint = nil
			return err
		}
	}
	return nil
}
func (g *EdgeGateway) Addr() net.Addr {
	if g.endpoint == nil {
		return nil
	}
	return g.endpoint.Addr()
}
func (g *EdgeGateway) ConnectionCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.sessions)
}
func (g *EdgeGateway) Close() error {
	select {
	case <-g.closed:
	default:
		close(g.closed)
	}
	if g.endpoint != nil {
		return g.endpoint.Close()
	}
	return nil
}
func (g *EdgeGateway) handleInbound(data []byte, addr *net.UDPAddr) {
	if g.peer != nil && g.peer.Handle(data, addr) {
		return
	}
	if !udphub.AllowEdgeDevicePacket(addr) {
		g.metrics.AddDrop()
		return
	}
	g.metrics.AddIn(len(data))
	g.handleDevicePacket(data, addr)
}
func (g *EdgeGateway) MetricsSnapshot() MetricsSnapshot { return g.metrics.Snapshot() }
func (g *EdgeGateway) identity(packet *protocol.DraARLv1Packet) string {
	return fmt.Sprintf("%s-%d", packet.Username, packet.SSID)
}
func (g *EdgeGateway) handleDevicePacket(data []byte, addr *net.UDPAddr) {
	packet, err := protocol.NewDraARLv1RoutingPacket(addr, data)
	if err != nil {
		return
	}
	defer protocol.ReleaseDraARLv1RoutingPacket(packet)
	key := g.identity(packet)
	g.mu.RLock()
	sessionID, exists := g.byIdentity[key]
	session := g.sessions[sessionID]
	if (!exists || session == nil) && packet.Username == "" {
		for id, candidate := range g.sessions {
			if candidate.Grant.SSID == packet.SSID && candidate.Addr != nil && candidate.Addr.String() == addr.String() {
				sessionID, session, exists = id, candidate, true
				break
			}
		}
	}
	g.mu.RUnlock()
	if !exists || session == nil {
		if packet.Type == protocol.DraARLTypeHeartbeat || packet.Type == protocol.DraARLTypeJWTAuth {
			g.requestAuth(data, packet, addr)
		}
		return
	}
	// Update the endpoint and take an immutable grant snapshot while holding
	// the gateway lock. RouteDelta may update the same grant concurrently.
	g.mu.Lock()
	current := g.sessions[sessionID]
	if current == nil {
		g.mu.Unlock()
		return
	}
	current.Addr = cloneUDPAddr(addr)
	current.LastSeen = time.Now()
	grant := current.Grant
	g.mu.Unlock()
	if packet.Type == protocol.DraARLTypeHeartbeat {
		response := protocol.EncodeHeartbeatResponse(packet, grant.CallSign)
		g.writeDevice(response, addr)
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
	g.localFanout(grant.SessionID, grant.DomainID, inner)
	frame := RelayFrame{SessionID: grant.SessionID, SessionEpoch: grant.SessionEpoch, DomainID: grant.DomainID, RequiredProjectionVersion: g.projection.Snapshot().Version, InnerPacket: inner}
	payload, err := frame.MarshalBinary()
	if err != nil {
		return
	}
	env := NewEnvelope(SubtypeRelayUpstream, g.client.Session.NodeID, g.client.Session.SessionID, g.client.Session.SessionID+uint64(time.Now().UnixNano()), payload)
	env.ProjectionVersion = frame.RequiredProjectionVersion
	if g.peer != nil {
		_ = g.peer.Send(env)
	} else {
		_ = g.client.SendEnvelope(env)
	}
}
func (g *EdgeGateway) requestAuth(data []byte, packet *protocol.DraARLv1Packet, addr *net.UDPAddr) {
	identity := g.identity(packet)
	id := g.nextRequest.Add(1)
	g.mu.Lock()
	if _, exists := g.pendingIdentity[identity]; exists {
		g.mu.Unlock()
		return
	}
	g.pending[id] = &pendingDeviceAuth{addr: cloneUDPAddr(addr), wire: append([]byte(nil), data...)}
	g.pendingIdentity[identity] = id
	g.mu.Unlock()
	request := DeviceAuthRequest{RequestID: id, SourceIP: addr.IP.String(), Packet: append([]byte(nil), data...)}
	payload, err := EncodeJSON(request)
	if err != nil {
		return
	}
	env := NewEnvelope(SubtypeDeviceAuth, g.client.Session.NodeID, g.client.Session.SessionID, id, payload)
	env.Flags = FlagControl | FlagAck
	if err := g.client.SendEnvelope(env); err != nil {
		g.mu.Lock()
		delete(g.pending, id)
		delete(g.pendingIdentity, identity)
		g.mu.Unlock()
	}
}
func (g *EdgeGateway) OnEnvelope(env Envelope) {
	switch env.Subtype {
	case SubtypeDeviceAuth:
		var response DeviceAuthResponse
		if DecodeJSON(env.Payload, &response) != nil {
			return
		}
		g.finishAuth(response)
	case SubtypeRouteDelta:
		var delta RouteDelta
		if DecodeJSON(env.Payload, &delta) != nil {
			return
		}
		if env.Duplicate {
			p := g.projection.Snapshot()
			if p.ClusterEpoch == delta.ClusterEpoch && p.Version >= delta.NewVersion {
				g.sendRouteAck(delta.NewVersion, "", env.MessageID)
			} else {
				g.requestResync("duplicate delta does not match current projection")
			}
			return
		}
		if err := g.projection.ApplyDelta(delta); err != nil {
			g.requestResync(err.Error())
			return
		}
		g.applyRoutes(g.projection.Snapshot())
		g.sendRouteAck(delta.NewVersion, "", env.MessageID)
	case SubtypeRouteSnapshotBegin:
		var begin SnapshotBegin
		if DecodeJSON(env.Payload, &begin) != nil {
			return
		}
		if assembler, err := NewSnapshotAssembler(begin); err == nil {
			g.mu.Lock()
			g.snapshotAssembler = assembler
			g.mu.Unlock()
		}
	case SubtypeRouteSnapshotChunk:
		var chunk SnapshotChunk
		if DecodeJSON(env.Payload, &chunk) != nil {
			return
		}
		g.mu.RLock()
		assembler := g.snapshotAssembler
		g.mu.RUnlock()
		if assembler != nil {
			_ = assembler.Add(chunk)
		}
	case SubtypeRouteSnapshotCommit:
		var commit SnapshotCommit
		if DecodeJSON(env.Payload, &commit) != nil {
			return
		}
		if env.Duplicate {
			p := g.projection.Snapshot()
			if p.ClusterEpoch == env.ClusterEpoch && p.Version == env.ProjectionVersion {
				g.sendRouteAck(p.Version, "", env.MessageID)
			} else {
				g.requestResync("duplicate snapshot commit does not match current projection")
			}
			return
		}
		g.mu.Lock()
		assembler := g.snapshotAssembler
		g.snapshotAssembler = nil
		g.mu.Unlock()
		if assembler == nil {
			return
		}
		p, err := assembler.Commit(commit)
		if err != nil {
			g.requestResync(err.Error())
			return
		}
		_ = g.projection.Replace(p)
		g.applyRoutes(p)
		g.sendRouteAck(p.Version, "", env.MessageID)
	case SubtypeRelayDownstream:
		frame, err := UnmarshalRelayFrame(env.Payload)
		if err == nil {
			g.deliverDownstream(frame)
		}
	}
}

func (g *EdgeGateway) finishAuth(response DeviceAuthResponse) {
	g.mu.Lock()
	pending := g.pending[response.RequestID]
	delete(g.pending, response.RequestID)
	if pending != nil {
		if packet, err := protocol.NewDraARLv1RoutingPacket(pending.addr, pending.wire); err == nil {
			delete(g.pendingIdentity, g.identity(packet))
			protocol.ReleaseDraARLv1RoutingPacket(packet)
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
	session := &edgeDeviceSession{Grant: grant, Addr: cloneUDPAddr(pending.addr), LastSeen: time.Now()}
	key := fmt.Sprintf("%s-%d", grant.Username, grant.SSID)
	g.mu.Lock()
	g.sessions[grant.SessionID] = session
	g.byIdentity[key] = grant.SessionID
	g.mu.Unlock()
	if len(response.ResponsePacket) > 0 {
		g.writeDevice(response.ResponsePacket, pending.addr)
		return
	}
	packet, err := protocol.NewDraARLv1RoutingPacket(pending.addr, pending.wire)
	if err == nil {
		responsePacket := protocol.EncodeHeartbeatResponse(packet, grant.CallSign)
		g.writeDevice(responsePacket, pending.addr)
		protocol.ReleaseDraARLv1RoutingPacket(packet)
	}
}
func (g *EdgeGateway) applyRoutes(p *Projection) {
	if p == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for id, session := range g.sessions {
		if route, ok := p.Devices[id]; ok {
			session.Grant.DisableSend, session.Grant.DisableRecv = route.DisableSend, route.DisableRecv
			session.Grant.GroupID, session.Grant.DomainID = route.GroupID, route.DomainID
			session.Grant.SessionEpoch = route.SessionEpoch
		} else {
			delete(g.sessions, id)
			delete(g.byIdentity, fmt.Sprintf("%s-%d", session.Grant.Username, session.Grant.SSID))
		}
	}
}
func (g *EdgeGateway) localFanout(sourceSession, domainID uint64, data []byte) {
	g.mu.RLock()
	targets := make([]udphub.EdgeFanoutTarget, 0, len(g.sessions))
	for id, session := range g.sessions {
		if id != sourceSession && session.Grant.DomainID == domainID && !session.Grant.DisableRecv && session.Addr != nil {
			targets = append(targets, udphub.EdgeFanoutTarget{Addr: cloneUDPAddr(session.Addr), DeviceID: session.Grant.DeviceID, Username: session.Grant.Username, SSID: session.Grant.SSID})
		}
	}
	g.mu.RUnlock()
	if g.endpoint != nil && g.endpoint.Fanout(data, targets, 0, "", 0) {
		return
	}
	for _, target := range targets {
		g.writeDevice(data, target.Addr)
	}
}

func cloneUDPAddr(addr *net.UDPAddr) *net.UDPAddr {
	if addr == nil {
		return nil
	}
	copyAddr := *addr
	copyAddr.IP = append(net.IP(nil), addr.IP...)
	return &copyAddr
}
func (g *EdgeGateway) deliverDownstream(frame RelayFrame) {
	p := g.projection.Snapshot()
	if route, ok := p.Devices[frame.SessionID]; ok && route.SessionEpoch != frame.SessionEpoch {
		return
	}
	g.localFanout(0, frame.DomainID, frame.InnerPacket)
}
func (g *EdgeGateway) sendRouteAck(version uint64, routeErr string, ackFor uint64) {
	p := g.projection.Snapshot()
	payload, _ := EncodeJSON(RouteAck{ClusterEpoch: p.ClusterEpoch, ProjectionVersion: version, AckForMessageID: ackFor, Error: routeErr})
	env := NewEnvelope(SubtypeRouteAck, g.client.Session.NodeID, g.client.Session.SessionID, g.nextRequest.Add(1), payload)
	env.Flags = FlagControl | FlagAck
	_ = g.client.SendEnvelope(env)
}
func (g *EdgeGateway) requestResync(reason string) {
	p := g.projection.Snapshot()
	payload, _ := EncodeJSON(ResyncRequest{ClusterEpoch: p.ClusterEpoch, ProjectionVersion: p.Version, Reason: reason})
	env := NewEnvelope(SubtypeRouteResyncRequest, g.client.Session.NodeID, g.client.Session.SessionID, g.nextRequest.Add(1), payload)
	env.Flags = FlagControl | FlagAck
	_ = g.client.SendEnvelope(env)
}
func (g *EdgeGateway) writeDevice(data []byte, addr *net.UDPAddr) {
	if g.endpoint == nil || addr == nil {
		return
	}
	if err := g.endpoint.SendTo(data, addr); err == nil {
		g.metrics.AddOut(len(data))
	} else {
		g.metrics.AddError()
	}
}
