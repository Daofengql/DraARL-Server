package interconnect

import (
	"errors"
	"fmt"
	"log"
	"net"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"draarl/internal/ghostsession"
	"draarl/internal/protocol"
	"draarl/internal/udphub"
)

type DeviceAuthHandler func(session *NodeSession, request DeviceAuthRequest) (DeviceAuthResponse, error)
type DeviceActivationHandler func(session *NodeSession, grant *DeviceGrant) error
type DeviceSessionConfirmHandler func(session *NodeSession, sessions []DeviceSessionConfirmItem) ([]DeviceSessionConfirmResult, error)
type DeviceConfigHandler func(deviceID int, kind string, data []byte) ([][]byte, error)
type AcceptedRelayHandler func(AcceptedRelay)
type GhostSessionRenewHandler func(sessionID, nodeID string, controlSessionID uint64, now, expiresAt time.Time) (string, error)

type AcceptedRelay struct {
	SessionID uint64
	DeviceID  int
	OwnerID   int
	Username  string
	CallSign  string
	Nickname  string
	SSID      byte
	DevModel  byte
	GroupID   int
	Type      byte
	Payload   []byte
}

const defaultDeviceGrantTTL = 2 * time.Minute

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

type pendingDeviceConfigDelivery struct {
	owner      deviceSessionOwner
	envelope   Envelope
	createdAt  time.Time
	lastSentAt time.Time
	attempts   int
	result     chan error
}

type deviceConfigCacheKey struct {
	nodeID           string
	controlSessionID uint64
	messageID        uint64
}

type cachedDeviceConfigResult struct {
	message  DeviceConfigControl
	storedAt time.Time
}

const (
	maxPendingDeviceConfigs = 1024
	deviceConfigRetryAfter  = 750 * time.Millisecond
	deviceConfigTimeout     = 3 * time.Second
	deviceConfigMaxAttempts = 3
	deviceConfigCacheTTL    = 30 * time.Second
)

var (
	errDeviceConfigQueueFull = errors.New("device config delivery queue is full")
	errDeviceConfigTimeout   = errors.New("device config delivery timed out")
)

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

func (g *CenterGateway) handleDeviceConfigUp(session *NodeSession, env Envelope, message DeviceConfigControl) {
	if session == nil || env.MessageID == 0 || g.cluster == nil || g.server == nil {
		return
	}
	cacheKey := deviceConfigCacheKey{nodeID: session.NodeID, controlSessionID: session.SessionID, messageID: env.MessageID}
	if env.Duplicate {
		g.configMu.Lock()
		cached, ok := g.configUpCache[cacheKey]
		g.configMu.Unlock()
		if ok {
			g.sendDeviceConfigControl(session, cached.message)
		}
		return
	}

	g.ownershipMu.Lock()
	g.mu.RLock()
	owner, ok := g.deviceSessions[message.SessionID]
	handler := g.configHandler
	g.mu.RUnlock()
	if !ok || owner.NodeID != session.NodeID || owner.ControlSessionID != session.SessionID ||
		owner.SessionEpoch != message.SessionEpoch || owner.DeviceID != message.DeviceID {
		g.ownershipMu.Unlock()
		return
	}
	if route, routeOK := g.cluster.ResolveRoute(owner.SessionID); !routeOK || route.SessionEpoch != owner.SessionEpoch || route.DeviceID != owner.DeviceID {
		g.ownershipMu.Unlock()
		return
	}
	var (
		packets [][]byte
		err     error
	)
	if handler == nil {
		err = errors.New("device configuration handler is unavailable")
	} else {
		packets, err = handler(owner.DeviceID, message.Kind, message.Data)
	}
	g.ownershipMu.Unlock()

	result := DeviceConfigControl{
		Kind: DeviceConfigKindResult, SessionID: owner.SessionID, SessionEpoch: owner.SessionEpoch,
		DeviceID: owner.DeviceID, AckForMessageID: env.MessageID, Success: err == nil,
	}
	if err != nil {
		result.Error = "processing_failed"
		log.Printf("[INTERCONNECT] process device config failed: node=%s device=%d kind=%s err=%v", session.NodeID, owner.DeviceID, message.Kind, err)
	}
	g.cacheDeviceConfigUpResult(cacheKey, result)
	g.sendDeviceConfigControl(session, result)
	if err != nil {
		return
	}
	for _, packet := range packets {
		if _, _, queueErr := g.enqueueCurrentDeviceConfig(owner, packet); queueErr != nil {
			log.Printf("[INTERCONNECT] queue device config response failed: node=%s device=%d err=%v", owner.NodeID, owner.DeviceID, queueErr)
		}
	}
}

func (g *CenterGateway) cacheDeviceConfigUpResult(key deviceConfigCacheKey, result DeviceConfigControl) {
	select {
	case <-g.configClosed:
		return
	default:
	}
	now := time.Now()
	g.configMu.Lock()
	if g.configClosing {
		g.configMu.Unlock()
		return
	}
	for existingKey, cached := range g.configUpCache {
		if now.Sub(cached.storedAt) > deviceConfigCacheTTL {
			delete(g.configUpCache, existingKey)
		}
	}
	if len(g.configUpCache) >= maxPendingDeviceConfigs {
		var oldestKey deviceConfigCacheKey
		var oldestAt time.Time
		for existingKey, cached := range g.configUpCache {
			if oldestAt.IsZero() || cached.storedAt.Before(oldestAt) {
				oldestKey, oldestAt = existingKey, cached.storedAt
			}
		}
		delete(g.configUpCache, oldestKey)
	}
	g.configUpCache[key] = cachedDeviceConfigResult{message: result, storedAt: now}
	g.configMu.Unlock()
}

func (g *CenterGateway) sendDeviceConfigControl(session *NodeSession, message DeviceConfigControl) {
	if session == nil || g.server == nil || g.cluster == nil {
		return
	}
	select {
	case <-g.configClosed:
		return
	default:
	}
	current, ok := g.server.Session(session.NodeID)
	if !ok || current != session || current.SessionID != session.SessionID {
		return
	}
	payload, err := EncodeJSON(message)
	if err != nil {
		return
	}
	env := NewEnvelope(SubtypeDeviceConfig, "center", 0, g.cluster.NextMessageID(), payload)
	env.ClusterEpoch, env.Flags = g.cluster.Epoch(), FlagControl|FlagAck
	_ = g.server.SendEnvelope(session.NodeID, env)
}

func (g *CenterGateway) validateDeviceConfigDelivery(owner deviceSessionOwner, packet []byte) error {
	decoded, err := protocol.NewDraARLv1Packet(nil, packet)
	if err != nil {
		return err
	}
	if decoded.Type != protocol.DraARLTypeConfig {
		return errors.New("downstream packet is not Type 3")
	}
	if g.cluster == nil {
		return errors.New("cluster is unavailable")
	}
	route, ok := g.cluster.ResolveRoute(owner.SessionID)
	if !ok || route.SessionEpoch != owner.SessionEpoch || route.DeviceID != owner.DeviceID {
		return errors.New("device config owner route is stale")
	}
	if decoded.SSID != route.SSID || decoded.Username != route.Username || decoded.CallSign != route.CallSign {
		return errors.New("device config packet identity mismatch")
	}
	return nil
}

func (g *CenterGateway) enqueueDeviceConfig(owner deviceSessionOwner, packet []byte) (uint64, <-chan error, error) {
	if owner.NodeID == "" || owner.NodeID == CenterLocalNodeID || owner.ControlSessionID == 0 || g.server == nil || g.cluster == nil {
		return 0, nil, errors.New("remote device config owner is unavailable")
	}
	select {
	case <-g.configClosed:
		return 0, nil, errors.New("center gateway is closed")
	default:
	}
	if err := g.validateDeviceConfigDelivery(owner, packet); err != nil {
		return 0, nil, err
	}
	messageID := g.cluster.NextMessageID()
	message := DeviceConfigControl{
		Kind: DeviceConfigKindDown, SessionID: owner.SessionID, SessionEpoch: owner.SessionEpoch,
		DeviceID: owner.DeviceID, Packet: append([]byte(nil), packet...),
	}
	payload, err := EncodeJSON(message)
	if err != nil {
		return 0, nil, err
	}
	env := NewEnvelope(SubtypeDeviceConfig, "center", 0, messageID, payload)
	env.ClusterEpoch, env.Flags = g.cluster.Epoch(), FlagControl|FlagAck
	now := time.Now()
	pending := &pendingDeviceConfigDelivery{
		owner: owner, envelope: env, createdAt: now, lastSentAt: now, attempts: 1, result: make(chan error, 1),
	}
	g.configMu.Lock()
	if g.configClosing {
		g.configMu.Unlock()
		return 0, nil, errors.New("center gateway is closed")
	}
	if len(g.configPending) >= maxPendingDeviceConfigs {
		g.configMu.Unlock()
		return 0, nil, errDeviceConfigQueueFull
	}
	g.configPending[messageID] = pending
	g.configMu.Unlock()
	if err := g.sendPendingDeviceConfig(pending); err != nil {
		g.completePendingDeviceConfig(messageID, err)
		return 0, nil, err
	}
	return messageID, pending.result, nil
}

func (g *CenterGateway) enqueueCurrentDeviceConfig(owner deviceSessionOwner, packet []byte) (uint64, <-chan error, error) {
	g.ownershipMu.Lock()
	g.mu.RLock()
	currentSessionID := g.activeByID[owner.DeviceID]
	current, ok := g.deviceSessions[owner.SessionID]
	g.mu.RUnlock()
	if !ok || currentSessionID != owner.SessionID || current != owner {
		g.ownershipMu.Unlock()
		return 0, nil, errors.New("device config owner changed")
	}
	messageID, result, err := g.enqueueDeviceConfig(owner, packet)
	g.ownershipMu.Unlock()
	return messageID, result, err
}

func (g *CenterGateway) sendPendingDeviceConfig(pending *pendingDeviceConfigDelivery) error {
	if pending == nil || g.server == nil {
		return errors.New("node control server is unavailable")
	}
	session, ok := g.server.Session(pending.owner.NodeID)
	if !ok || session.SessionID != pending.owner.ControlSessionID {
		return errors.New("device config owner control session is offline")
	}
	return g.server.SendEnvelope(pending.owner.NodeID, pending.envelope)
}

func (g *CenterGateway) completePendingDeviceConfig(messageID uint64, result error) bool {
	g.configMu.Lock()
	pending := g.configPending[messageID]
	if pending != nil {
		delete(g.configPending, messageID)
	}
	g.configMu.Unlock()
	if pending == nil {
		return false
	}
	select {
	case pending.result <- result:
	default:
	}
	return true
}

func (g *CenterGateway) finishDeviceConfigDelivery(session *NodeSession, message DeviceConfigControl) bool {
	if session == nil || message.AckForMessageID == 0 {
		return false
	}
	g.configMu.Lock()
	pending := g.configPending[message.AckForMessageID]
	if pending == nil || pending.owner.NodeID != session.NodeID || pending.owner.ControlSessionID != session.SessionID ||
		pending.owner.SessionID != message.SessionID || pending.owner.SessionEpoch != message.SessionEpoch || pending.owner.DeviceID != message.DeviceID {
		g.configMu.Unlock()
		return false
	}
	delete(g.configPending, message.AckForMessageID)
	g.configMu.Unlock()
	var result error
	if !message.Success {
		result = errors.New("edge rejected device config delivery")
		if message.Error != "" {
			result = fmt.Errorf("edge rejected device config delivery: %s", message.Error)
		}
	}
	select {
	case pending.result <- result:
	default:
	}
	return true
}

// SendDeviceConfig routes a complete Type 3 packet to the authoritative
// remote session. handled=false means the device is centre-local or offline
// and the caller should use its existing local path.
func (g *CenterGateway) SendDeviceConfig(deviceID int, packet []byte, timeout time.Duration) (handled bool, err error) {
	if deviceID <= 0 || g.cluster == nil {
		return false, nil
	}
	g.ownershipMu.Lock()
	g.mu.RLock()
	sessionID := g.activeByID[deviceID]
	owner, ok := g.deviceSessions[sessionID]
	g.mu.RUnlock()
	if !ok || owner.NodeID == CenterLocalNodeID {
		g.ownershipMu.Unlock()
		return false, nil
	}
	g.ownershipMu.Unlock()
	messageID, result, err := g.enqueueCurrentDeviceConfig(owner, packet)
	if err != nil {
		return true, err
	}
	if timeout <= 0 || timeout > deviceConfigTimeout+time.Second {
		timeout = deviceConfigTimeout + time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-result:
		return true, err
	case <-timer.C:
		g.completePendingDeviceConfig(messageID, errDeviceConfigTimeout)
		return true, errDeviceConfigTimeout
	}
}

func (g *CenterGateway) deviceConfigRetryLoop() {
	defer g.configWG.Done()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-g.configClosed:
			return
		case now := <-ticker.C:
			g.retryPendingDeviceConfigs(now)
		}
	}
}

func (g *CenterGateway) retryPendingDeviceConfigs(now time.Time) {
	resend := make([]*pendingDeviceConfigDelivery, 0)
	expired := make([]*pendingDeviceConfigDelivery, 0)
	g.configMu.Lock()
	for messageID, pending := range g.configPending {
		if now.Sub(pending.createdAt) >= deviceConfigTimeout {
			delete(g.configPending, messageID)
			expired = append(expired, pending)
			continue
		}
		if pending.attempts < deviceConfigMaxAttempts && now.Sub(pending.lastSentAt) >= deviceConfigRetryAfter {
			pending.attempts++
			pending.lastSentAt = now
			resend = append(resend, pending)
		}
	}
	for key, cached := range g.configUpCache {
		if now.Sub(cached.storedAt) > deviceConfigCacheTTL {
			delete(g.configUpCache, key)
		}
	}
	g.configMu.Unlock()
	for _, pending := range expired {
		select {
		case pending.result <- errDeviceConfigTimeout:
		default:
		}
	}
	for _, pending := range resend {
		g.ownershipMu.Lock()
		g.mu.RLock()
		currentSessionID := g.activeByID[pending.owner.DeviceID]
		current, ok := g.deviceSessions[pending.owner.SessionID]
		g.mu.RUnlock()
		if !ok || currentSessionID != pending.owner.SessionID || current != pending.owner {
			g.ownershipMu.Unlock()
			g.completePendingDeviceConfig(pending.envelope.MessageID, errors.New("device config owner changed"))
			continue
		}
		_ = g.sendPendingDeviceConfig(pending)
		g.ownershipMu.Unlock()
	}
}

func (g *CenterGateway) failDeviceConfigsForSession(session *NodeSession, result error) {
	if session == nil {
		return
	}
	failed := make([]*pendingDeviceConfigDelivery, 0)
	g.configMu.Lock()
	for messageID, pending := range g.configPending {
		if pending.owner.NodeID == session.NodeID && pending.owner.ControlSessionID == session.SessionID {
			delete(g.configPending, messageID)
			failed = append(failed, pending)
		}
	}
	for key := range g.configUpCache {
		if key.nodeID == session.NodeID && key.controlSessionID == session.SessionID {
			delete(g.configUpCache, key)
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
	_, allowed := g.speaker.AcquireLocal(route.SessionID, route.SessionEpoch, route.DomainID, time.Now())
	return allowed
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

func (g *CenterGateway) releaseSpeakerForRouteChange(current, next DeviceRoute) {
	if g.speaker == nil {
		return
	}
	if current.SessionEpoch != next.SessionEpoch || current.DomainID != next.DomainID || next.DomainID == 0 || next.DisableSend {
		g.speaker.ReleaseSession(current.SessionID, current.SessionEpoch)
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

func (g *EdgeGateway) ensureSessionIndexesLocked() {
	if g.byIdentity == nil {
		g.byIdentity = make(map[string]uint64)
	}
	if g.bySessionTag == nil {
		g.bySessionTag = make(map[uint32]uint64)
	}
}

const edgeAuthRequestTimeout = 5 * time.Second

const maxEdgePendingDeviceConfigs = 256

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

func (g *EdgeGateway) clearSpeakerStates() {
	g.mu.Lock()
	clear(g.speakerDomains)
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

func (g *EdgeGateway) removePendingDeviceConfigsLocked(sessionID uint64) {
	for requestID, pending := range g.pendingConfigUp {
		if pending.sessionID == sessionID {
			delete(g.pendingConfigUp, requestID)
		}
	}
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
