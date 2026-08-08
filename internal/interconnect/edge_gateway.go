package interconnect

import (
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"draarl/internal/udphub"
)

type EdgeGateway struct {
	listenAddr         string
	proxyProtocol      string
	endpoint           *udphub.EdgeEndpoint
	control            atomic.Pointer[edgeControlLink]
	disconnectedAt     atomic.Int64
	projection         *ProjectionStore
	mu                 sync.RWMutex
	sessions           map[uint64]*edgeDeviceSession
	byIdentity         map[string]uint64
	bySessionTag       map[uint32]uint64
	receiverGen        atomic.Uint64
	receiverCache      atomic.Pointer[edgeReceiverSnapshot]
	receiverBuildMu    sync.Mutex
	receiverHits       atomic.Uint64
	receiverMisses     atomic.Uint64
	receiverRebuilds   atomic.Uint64
	receiverBuildNS    atomic.Uint64
	receiverMaxEntries atomic.Uint64
	pending            map[uint64]*pendingDeviceAuth
	pendingIdentity    map[string]uint64
	pendingRenewals    map[uint64]pendingDeviceRenewal
	renewingSessions   map[uint64]uint64
	pendingConfirms    map[uint64]chan DeviceSessionConfirmResponse
	pendingConfigUp    map[uint64]*pendingDeviceConfigUp
	configDownResults  map[uint64]cachedDeviceConfigResult
	snapshotAssembler  *SnapshotAssembler
	nextRequest        atomic.Uint64
	metrics            Metrics
	closed             chan struct{}
	closeOnce          sync.Once
	cleanerWG          sync.WaitGroup
	sessionTimeout     time.Duration
	grantRenewBefore   time.Duration
	localGrace         time.Duration
	downstreamMaxAge   time.Duration
	downstreamMu       sync.Mutex
	pendingDownstream  []pendingDownstreamFrame
	downstreamWake     chan struct{}
	speakerDomains     map[uint64]*edgeSpeakerState
	reportSession      func(DeviceSessionReport)
	renewSession       func(DeviceSessionRenewRequest)
	installCredential  func(NodeCredentialControl) error
}

type edgeControlLink struct {
	client     *NodeClient
	peer       *NodeDatagramPeer
	ready      atomic.Bool
	recovering atomic.Bool
	confirming atomic.Bool
	readyOnce  sync.Once
	readyCh    chan struct{}
}

func newEdgeControlLink(client *NodeClient, peer *NodeDatagramPeer) *edgeControlLink {
	link := &edgeControlLink{client: client, peer: peer, readyCh: make(chan struct{})}
	link.recovering.Store(true)
	link.confirming.Store(true)
	return link
}

func (l *edgeControlLink) markReady() {
	if l == nil {
		return
	}
	l.confirming.Store(false)
	l.ready.Store(true)
	l.recovering.Store(false)
	l.readyOnce.Do(func() { close(l.readyCh) })
}

func NewEdgeGateway(listenAddr string, client *NodeClient, proxyProtocols ...string) (*EdgeGateway, error) {
	if listenAddr == "" {
		listenAddr = ":60050"
	}
	proxyProtocol := ""
	if len(proxyProtocols) > 0 {
		proxyProtocol = strings.ToLower(strings.TrimSpace(proxyProtocols[0]))
	}
	if proxyProtocol != "" && proxyProtocol != "v2" {
		return nil, errors.New("edge proxy protocol must be empty or v2")
	}
	p := NewProjection(1)
	gateway := &EdgeGateway{
		listenAddr: listenAddr, proxyProtocol: proxyProtocol, projection: NewProjectionStore(p), sessions: make(map[uint64]*edgeDeviceSession), byIdentity: make(map[string]uint64), bySessionTag: make(map[uint32]uint64),
		pending: make(map[uint64]*pendingDeviceAuth), pendingIdentity: make(map[string]uint64), pendingRenewals: make(map[uint64]pendingDeviceRenewal), renewingSessions: make(map[uint64]uint64),
		pendingConfirms: make(map[uint64]chan DeviceSessionConfirmResponse),
		pendingConfigUp: make(map[uint64]*pendingDeviceConfigUp), configDownResults: make(map[uint64]cachedDeviceConfigResult),
		speakerDomains: make(map[uint64]*edgeSpeakerState),
		closed:         make(chan struct{}), sessionTimeout: 20 * time.Second, grantRenewBefore: 30 * time.Second, localGrace: 15 * time.Second, downstreamMaxAge: 200 * time.Millisecond, downstreamWake: make(chan struct{}, 1),
	}
	if client != nil {
		gateway.control.Store(newEdgeControlLink(client, nil))
	}
	return gateway, nil
}

func (g *EdgeGateway) Start() error {
	endpoint, err := udphub.NewEdgeEndpoint(g.listenAddr, g.proxyProtocol, g.handleInbound)
	if err != nil {
		return err
	}
	g.endpoint = endpoint
	g.cleanerWG.Add(3)
	go g.sessionCleanerLoop()
	go g.downstreamBarrierLoop()
	go g.speakerLeaseCleanerLoop()
	return nil
}

func (g *EdgeGateway) attachControl(client *NodeClient, peer *NodeDatagramPeer) (*edgeControlLink, error) {
	if client == nil || client.Session == nil {
		return nil, errors.New("authenticated edge control client is required")
	}
	if peer != nil {
		if g.endpoint == nil {
			return nil, errors.New("edge UDP endpoint is not started")
		}
		peer.SetWriter(func(addr *net.UDPAddr, data []byte) error { return g.endpoint.SendTo(data, addr) })
	}
	link := newEdgeControlLink(client, peer)
	old := g.control.Swap(link)
	if old != nil && old.client != nil && old.client != client {
		_ = old.client.Close()
	}
	g.disconnectedAt.Store(0)
	g.clearPendingControlRequests()
	g.clearSpeakerStates()
	if peer != nil {
		if err := g.requestDataBind(link); err != nil {
			g.detachControl(client, time.Now())
			return nil, err
		}
	}
	return link, nil
}

func (g *EdgeGateway) requestDataBind(link *edgeControlLink) error {
	if link == nil || link.client == nil || link.peer == nil {
		return nil
	}
	payload, err := EncodeJSON(NodeDataBind{Action: NodeDataBindRequest})
	if err != nil {
		return err
	}
	env := NewEnvelope(SubtypeNodeDataBind, link.client.Session.NodeID, link.client.Session.SessionID, g.nextRequest.Add(1), payload)
	env.Flags = FlagControl
	return link.client.SendEnvelope(env)
}

func (g *EdgeGateway) detachControl(client *NodeClient, now time.Time) bool {
	for {
		link := g.control.Load()
		if link == nil || link.client != client {
			return false
		}
		if g.control.CompareAndSwap(link, nil) {
			g.disconnectedAt.Store(now.UnixMilli())
			g.clearPendingControlRequests()
			g.clearPendingDownstream()
			g.clearSpeakerStates()
			return true
		}
	}
}

func (g *EdgeGateway) clearPendingControlRequests() {
	g.mu.Lock()
	clear(g.pending)
	clear(g.pendingIdentity)
	clear(g.pendingRenewals)
	clear(g.renewingSessions)
	clear(g.pendingConfirms)
	clear(g.pendingConfigUp)
	clear(g.configDownResults)
	g.mu.Unlock()
}

func (g *EdgeGateway) currentControl(requireReady bool) *edgeControlLink {
	link := g.control.Load()
	if link == nil || (requireReady && !link.ready.Load()) {
		return nil
	}
	return link
}

func (g *EdgeGateway) allowExistingLocal(now time.Time) bool {
	if link := g.control.Load(); link != nil {
		return link.ready.Load()
	}
	disconnectedAt := g.disconnectedAt.Load()
	return disconnectedAt > 0 && g.localGrace > 0 && now.Sub(time.UnixMilli(disconnectedAt)) <= g.localGrace
}

func (g *EdgeGateway) preserveSessionsDuringRecovery() bool {
	link := g.control.Load()
	return link != nil && !link.ready.Load() && link.recovering.Load()
}

func (g *EdgeGateway) markControlReady(client *NodeClient) bool {
	link := g.control.Load()
	if link == nil || link.client != client {
		return false
	}
	link.markReady()
	return true
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
	g.closeOnce.Do(func() { close(g.closed) })
	var err error
	if g.endpoint != nil {
		err = g.endpoint.Close()
	}
	g.cleanerWG.Wait()
	return err
}

func (g *EdgeGateway) handleInbound(data []byte, remoteAddr, realAddr *net.UDPAddr) {
	if link := g.currentControl(false); link != nil && link.peer != nil && link.peer.Handle(data, remoteAddr) {
		return
	}
	if realAddr == nil {
		realAddr = remoteAddr
	}
	if !udphub.AllowEdgeDevicePacket(realAddr) {
		g.metrics.AddDrop()
		return
	}
	g.metrics.AddIn(len(data))
	g.handleDevicePacket(data, remoteAddr, realAddr)
}

func (g *EdgeGateway) MetricsSnapshot() MetricsSnapshot { return g.metrics.Snapshot() }

func (g *EdgeGateway) OnEnvelope(env Envelope) {
	link := g.currentControl(false)
	if link == nil {
		return
	}
	g.onEnvelopeFrom(link.client, env)
}

func (g *EdgeGateway) onEnvelopeFrom(client *NodeClient, env Envelope) {
	link := g.currentControl(false)
	if link == nil || link.client != client {
		return
	}
	if env.receivedAt.IsZero() {
		env.receivedAt = time.Now()
	}
	switch env.Subtype {
	case SubtypeNodeCredential:
		var message NodeCredentialControl
		if DecodeJSON(env.Payload, &message) != nil || message.Kind != NodeCredentialKindRotate || message.Validate(client.Session.NodeID) != nil {
			return
		}
		result := NodeCredentialControl{Kind: NodeCredentialKindResult, CredentialEpoch: message.CredentialEpoch, AckForMessageID: env.MessageID, Success: false}
		if g.installCredential == nil {
			result.Error = "credential persistence is unavailable"
		} else if err := g.installCredential(message); err != nil {
			result.Error = "credential persistence failed"
		} else {
			result.Success = true
		}
		payload, _ := EncodeJSON(result)
		reply := NewEnvelope(SubtypeNodeCredential, client.Session.NodeID, client.Session.SessionID, g.nextRequest.Add(1), payload)
		reply.Flags = FlagControl | FlagAck | FlagCritical
		_ = client.SendEnvelope(reply)
	case SubtypeNodeDataBind:
		var bind NodeDataBind
		if DecodeJSON(env.Payload, &bind) != nil || bind.Action != NodeDataBindChallenge || len(bind.Challenge) != 32 || link.peer == nil {
			return
		}
		_ = link.peer.ProveDataBind(bind.Challenge, g.nextRequest.Add(1))
	case SubtypeDeviceAuth:
		var response DeviceAuthResponse
		if DecodeJSON(env.Payload, &response) != nil {
			return
		}
		g.finishAuth(response)
	case SubtypeDeviceSessionRenew:
		var response DeviceSessionRenewResponse
		if DecodeJSON(env.Payload, &response) != nil {
			return
		}
		g.finishSessionRenewal(response, time.Now())
	case SubtypeDeviceSessionConfirm:
		var response DeviceSessionConfirmResponse
		if DecodeJSON(env.Payload, &response) != nil || response.Validate() != nil {
			return
		}
		g.mu.RLock()
		pending := g.pendingConfirms[response.RequestID]
		g.mu.RUnlock()
		if pending != nil {
			select {
			case pending <- response:
			default:
			}
		}
	case SubtypeDeviceConfig:
		var message DeviceConfigControl
		if DecodeJSON(env.Payload, &message) != nil || message.Validate() != nil {
			return
		}
		switch message.Kind {
		case DeviceConfigKindDown:
			g.handleDeviceConfigDown(env, message)
		case DeviceConfigKindResult:
			g.finishDeviceConfigUp(message)
		}
	case SubtypeSpeakerLease:
		var message SpeakerLeaseControl
		if DecodeJSON(env.Payload, &message) != nil || message.Validate() != nil {
			return
		}
		g.finishSpeakerLease(message, time.Now())
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
		if !link.confirming.Load() {
			g.applyRoutes(g.projection.Snapshot())
			g.drainDownstream(time.Now())
		}
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
				if !link.confirming.Load() {
					g.markControlReady(client)
				}
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
		if !link.confirming.Load() {
			g.applyRoutes(p)
			g.drainDownstream(time.Now())
		}
		g.sendRouteAck(p.Version, "", env.MessageID)
		if !link.confirming.Load() {
			g.markControlReady(client)
		}
	case SubtypeRelayDownstream:
		if !link.ready.Load() {
			return
		}
		frame, err := UnmarshalRelayFrame(env.Payload)
		if err == nil {
			g.deliverDownstream(env, frame)
		}
	case SubtypeDeviceSessionRevoke:
		var revoke DeviceSessionRevoke
		if DecodeJSON(env.Payload, &revoke) == nil {
			g.revokeSession(revoke)
		}
	}
}
