package interconnect

import (
	"errors"
	"net"
	"sync"
	"time"
)

type CenterGateway struct {
	cluster            *ClusterManager
	server             *NodeServer
	data               *NodeDatagramBridge
	auth               DeviceAuthHandler
	activate           DeviceActivationHandler
	confirm            DeviceSessionConfirmHandler
	mu                 sync.RWMutex
	ownershipMu        sync.Mutex
	deviceSessions     map[uint64]deviceSessionOwner
	activeDevices      map[string]uint64
	activeByID         map[int]uint64
	activeByGhost      map[string]uint64
	deviceEpochs       map[string]uint64
	ghostRecovery      map[string]uint64
	ghostRecoverySeq   uint64
	ghostRecoveryAfter time.Duration
	metrics            map[string]*Metrics
	onNodeStatus       func(*NodeSession, *NodeHeartbeat, bool)
	onLocalRevoke      func(deviceID, ownerID int, ssid byte, sessionID, sessionEpoch uint64)
	onDeviceRevoke     func(nodeID string, controlSessionID uint64, deviceID int, reason string)
	onCredentialResult func(*NodeSession, NodeCredentialControl)
	onAcceptedRelay    AcceptedRelayHandler
	onGhostRenew       GhostSessionRenewHandler
	onGhostRevoke      func(string, string)
	configHandler      DeviceConfigHandler
	configMu           sync.Mutex
	configPending      map[uint64]*pendingDeviceConfigDelivery
	configUpCache      map[deviceConfigCacheKey]cachedDeviceConfigResult
	configClosing      bool
	configClosed       chan struct{}
	configClose        sync.Once
	configLoopOnce     sync.Once
	configWG           sync.WaitGroup
	speaker            *SpeakerLeaseManager
	resourceLimits     ResourceLimits
	sessionCounts      map[nodeControlSession]int
}

func NewCenterGateway(cluster *ClusterManager, auth DeviceAuthHandler, activators ...DeviceActivationHandler) *CenterGateway {
	var activate DeviceActivationHandler
	if len(activators) > 0 {
		activate = activators[0]
	}
	limits := DefaultResourceLimits()
	return &CenterGateway{
		cluster: cluster, auth: auth, activate: activate,
		deviceSessions: make(map[uint64]deviceSessionOwner), activeDevices: make(map[string]uint64), activeByID: make(map[int]uint64), activeByGhost: make(map[string]uint64), deviceEpochs: make(map[string]uint64), ghostRecovery: make(map[string]uint64), ghostRecoveryAfter: 3 * time.Minute, metrics: make(map[string]*Metrics),
		resourceLimits: limits, sessionCounts: make(map[nodeControlSession]int),
		configPending: make(map[uint64]*pendingDeviceConfigDelivery), configUpCache: make(map[deviceConfigCacheKey]cachedDeviceConfigResult), configClosed: make(chan struct{}),
		speaker: NewSpeakerLeaseManager(),
	}
}

func (g *CenterGateway) SetResourceLimits(limits ResourceLimits) error {
	normalized, err := limits.normalized()
	if err != nil {
		return err
	}
	g.mu.Lock()
	g.resourceLimits = normalized
	g.mu.Unlock()
	return nil
}

func (g *CenterGateway) Bind(server *NodeServer, data *NodeDatagramBridge) {
	g.server, g.data = server, data
	if g.cluster != nil {
		g.cluster.AttachServer(server)
		g.cluster.AttachDataBridge(data)
	}
	g.configLoopOnce.Do(func() {
		g.configWG.Add(1)
		go g.deviceConfigRetryLoop()
	})
}

func (g *CenterGateway) SetLocalRevocationHandler(handler func(deviceID, ownerID int, ssid byte, sessionID, sessionEpoch uint64)) {
	g.mu.Lock()
	g.onLocalRevoke = handler
	g.mu.Unlock()
}

func (g *CenterGateway) SetDeviceRevocationHandler(handler func(nodeID string, controlSessionID uint64, deviceID int, reason string)) {
	g.mu.Lock()
	g.onDeviceRevoke = handler
	g.mu.Unlock()
}

func (g *CenterGateway) SetDeviceConfigHandler(handler DeviceConfigHandler) {
	g.mu.Lock()
	g.configHandler = handler
	g.mu.Unlock()
}

func (g *CenterGateway) SetDeviceSessionConfirmHandler(handler DeviceSessionConfirmHandler) {
	g.mu.Lock()
	g.confirm = handler
	g.mu.Unlock()
}

func (g *CenterGateway) SetAcceptedRelayHandler(handler AcceptedRelayHandler) {
	g.mu.Lock()
	g.onAcceptedRelay = handler
	g.mu.Unlock()
}

func (g *CenterGateway) SetGhostSessionHandlers(renew GhostSessionRenewHandler, revoke func(string, string)) {
	g.mu.Lock()
	g.onGhostRenew = renew
	g.onGhostRevoke = revoke
	g.mu.Unlock()
}

func (g *CenterGateway) IdentityOwnedByRemote(ownerID int, ssid byte) bool {
	identity := deviceOwnerIdentity(ownerID, ssid)
	if identity == "" {
		return false
	}
	g.mu.RLock()
	sessionID := g.activeDevices[identity]
	owner, ok := g.deviceSessions[sessionID]
	g.mu.RUnlock()
	return ok && owner.NodeID != CenterLocalNodeID
}

func (g *CenterGateway) OnConnect(session *NodeSession) {
	g.ownershipMu.Lock()
	g.mu.Lock()
	recovering := make([]deviceSessionOwner, 0)
	for id, owner := range g.deviceSessions {
		if owner.NodeID == session.NodeID {
			recovering = append(recovering, owner)
			g.removeOwnerMapsLocked(id, owner)
		}
	}
	g.mu.Unlock()
	tasks := g.markGhostRecovery(recovering)
	if g.cluster != nil {
		g.cluster.OnConnect(session)
	}
	g.ownershipMu.Unlock()
	g.startGhostRecoveryTimers(tasks)
	if g.onNodeStatus != nil {
		g.onNodeStatus(session, nil, true)
	}
}

func (g *CenterGateway) OnDisconnect(session *NodeSession, err error) {
	if g.speaker != nil {
		g.speaker.ReleaseNode(session.NodeID, session.SessionID)
	}
	g.ownershipMu.Lock()
	currentSession := true
	if g.cluster != nil {
		currentSession = g.cluster.OnDisconnect(session, err)
	}
	g.mu.Lock()
	recovering := make([]deviceSessionOwner, 0)
	for id, owner := range g.deviceSessions {
		if owner.NodeID == session.NodeID && owner.ControlSessionID == session.SessionID {
			recovering = append(recovering, owner)
			g.removeOwnerMapsLocked(id, owner)
		}
	}
	if currentSession {
		delete(g.metrics, session.NodeID)
	}
	g.mu.Unlock()
	tasks := g.markGhostRecovery(recovering)
	g.ownershipMu.Unlock()
	g.startGhostRecoveryTimers(tasks)
	g.failDeviceConfigsForSession(session, errors.New("node control session disconnected"))
	if g.onNodeStatus != nil {
		g.onNodeStatus(session, nil, false)
	}
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
			if g.onNodeStatus != nil {
				g.onNodeStatus(session, &heartbeat, true)
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
	if env.Subtype != SubtypeRelayUpstream {
		if session != nil {
			session.DataMetrics.AddDrop()
		}
		return
	}
	if !g.handleRelayUpstream(session, env) && session != nil {
		session.DataMetrics.AddDrop()
	}
}

func (g *CenterGateway) OnEnvelope(session *NodeSession, env Envelope) {
	if env.Subtype == SubtypeNodeDataBind {
		g.handleDataBindRequest(session, env)
		return
	}
	g.handleEnvelope(session, env)
}

func (g *CenterGateway) handleDataBindRequest(session *NodeSession, env Envelope) {
	if session == nil || g.server == nil {
		return
	}
	var request NodeDataBind
	if DecodeJSON(env.Payload, &request) != nil || request.Action != NodeDataBindRequest || len(request.Challenge) != 0 {
		session.resourceProtection().recordDataBindReject()
		return
	}
	challenge, err := session.IssueDataBindChallenge(time.Now())
	if err != nil {
		return
	}
	payload, _ := EncodeJSON(NodeDataBind{Action: NodeDataBindChallenge, Challenge: challenge})
	reply := NewEnvelope(SubtypeNodeDataBind, "center", 0, g.cluster.NextMessageID(), payload)
	reply.Flags = FlagControl
	_ = g.server.SendEnvelope(session.NodeID, reply)
}

func (g *CenterGateway) handleEnvelope(session *NodeSession, env Envelope) {
	switch env.Subtype {
	case SubtypeNodeCredential:
		var message NodeCredentialControl
		if DecodeJSON(env.Payload, &message) != nil || message.Kind != NodeCredentialKindResult || message.Validate(session.NodeID) != nil {
			return
		}
		if g.onCredentialResult != nil {
			g.onCredentialResult(session, message)
		}
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
			if err := g.activateDeviceSession(session, response.Grant); err != nil {
				g.rejectGhostGrant(response.Grant, "activate_device_session_failed")
				response.Success, response.Grant = false, nil
				response.Error = "activate_device_session_failed"
			}
		}
		payload, _ := EncodeJSON(response)
		reply := NewEnvelope(SubtypeDeviceAuth, "center", 0, g.cluster.NextMessageID(), payload)
		reply.ClusterEpoch = g.cluster.Epoch()
		reply.Flags = FlagControl | FlagAck
		_ = g.server.SendEnvelope(session.NodeID, reply)
	case SubtypeDeviceSessionRenew:
		var request DeviceSessionRenewRequest
		if DecodeJSON(env.Payload, &request) != nil {
			return
		}
		response := g.renewDeviceSession(session, request, time.Now())
		payload, _ := EncodeJSON(response)
		reply := NewEnvelope(SubtypeDeviceSessionRenew, "center", 0, g.cluster.NextMessageID(), payload)
		reply.ClusterEpoch = g.cluster.Epoch()
		reply.Flags = FlagControl | FlagAck
		if g.server != nil {
			_ = g.server.SendEnvelope(session.NodeID, reply)
		}
	case SubtypeDeviceSessionConfirm:
		var request DeviceSessionConfirmRequest
		if DecodeJSON(env.Payload, &request) != nil || request.Validate() != nil {
			return
		}
		response := g.confirmDeviceSessions(session, request)
		payload, _ := EncodeJSON(response)
		reply := NewEnvelope(SubtypeDeviceSessionConfirm, "center", 0, g.cluster.NextMessageID(), payload)
		reply.ClusterEpoch = g.cluster.Epoch()
		reply.Flags = FlagControl | FlagAck | FlagCritical
		_ = session.SendEnvelope(reply)
	case SubtypeDeviceConfig:
		var message DeviceConfigControl
		if DecodeJSON(env.Payload, &message) != nil || message.Validate() != nil {
			return
		}
		switch message.Kind {
		case DeviceConfigKindSync, DeviceConfigKindReport:
			g.handleDeviceConfigUp(session, env, message)
		case DeviceConfigKindResult:
			g.finishDeviceConfigDelivery(session, message)
		}
	case SubtypeSpeakerLease:
		var message SpeakerLeaseControl
		if DecodeJSON(env.Payload, &message) != nil || message.Validate() != nil {
			return
		}
		g.handleSpeakerLease(session, message)
	case SubtypeRelayUpstream:
		g.handleRelayUpstream(session, env)
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
			if g.onNodeStatus != nil {
				g.onNodeStatus(session, &heartbeat, true)
			}
		}
	case SubtypeDeviceSessionReport:
		var report DeviceSessionReport
		if DecodeJSON(env.Payload, &report) == nil {
			g.handleDeviceSessionReport(session, report)
		}
	}
}

func (g *CenterGateway) Close() {
	if g == nil {
		return
	}
	g.configClose.Do(func() { close(g.configClosed) })
	g.configWG.Wait()
	failed := make([]*pendingDeviceConfigDelivery, 0)
	g.configMu.Lock()
	g.configClosing = true
	for messageID, pending := range g.configPending {
		delete(g.configPending, messageID)
		failed = append(failed, pending)
	}
	clear(g.configUpCache)
	g.configMu.Unlock()
	for _, pending := range failed {
		select {
		case pending.result <- errors.New("center gateway closed"):
		default:
		}
	}
}
