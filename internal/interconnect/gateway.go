package interconnect

import (
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"draarl/internal/protocol"
	"draarl/internal/udphub"
)

type DeviceAuthHandler func(session *NodeSession, request DeviceAuthRequest) (DeviceAuthResponse, error)
type DeviceActivationHandler func(session *NodeSession, grant *DeviceGrant) error

const defaultDeviceGrantTTL = 2 * time.Minute

type CenterGateway struct {
	cluster        *ClusterManager
	server         *NodeServer
	data           *NodeDatagramBridge
	auth           DeviceAuthHandler
	activate       DeviceActivationHandler
	mu             sync.RWMutex
	ownershipMu    sync.Mutex
	deviceSessions map[uint64]deviceSessionOwner
	activeDevices  map[string]uint64
	activeByID     map[int]uint64
	deviceEpochs   map[string]uint64
	metrics        map[string]*Metrics
	onNodeStatus   func(*NodeSession, *NodeHeartbeat, bool)
	onLocalRevoke  func(deviceID, ownerID int, ssid byte, sessionID, sessionEpoch uint64)
	onDeviceRevoke func(nodeID string, controlSessionID uint64, deviceID int, reason string)
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
}

func NewCenterGateway(cluster *ClusterManager, auth DeviceAuthHandler, activators ...DeviceActivationHandler) *CenterGateway {
	var activate DeviceActivationHandler
	if len(activators) > 0 {
		activate = activators[0]
	}
	return &CenterGateway{cluster: cluster, auth: auth, activate: activate, deviceSessions: make(map[uint64]deviceSessionOwner), activeDevices: make(map[string]uint64), activeByID: make(map[int]uint64), deviceEpochs: make(map[string]uint64), metrics: make(map[string]*Metrics)}
}
func (g *CenterGateway) Bind(server *NodeServer, data *NodeDatagramBridge) {
	g.server, g.data = server, data
	if g.cluster != nil {
		g.cluster.AttachServer(server)
		g.cluster.AttachDataBridge(data)
	}
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
	for id, owner := range g.deviceSessions {
		if owner.NodeID == session.NodeID {
			g.removeOwnerMapsLocked(id, owner)
		}
	}
	g.mu.Unlock()
	if g.cluster != nil {
		g.cluster.OnConnect(session)
	}
	g.ownershipMu.Unlock()
	if g.onNodeStatus != nil {
		g.onNodeStatus(session, nil, true)
	}
}
func (g *CenterGateway) OnDisconnect(session *NodeSession, err error) {
	g.ownershipMu.Lock()
	currentSession := true
	if g.cluster != nil {
		currentSession = g.cluster.OnDisconnect(session, err)
	}
	g.mu.Lock()
	for id, owner := range g.deviceSessions {
		if owner.NodeID == session.NodeID && owner.ControlSessionID == session.SessionID {
			g.removeOwnerMapsLocked(id, owner)
		}
	}
	if currentSession {
		delete(g.metrics, session.NodeID)
	}
	g.mu.Unlock()
	g.ownershipMu.Unlock()
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
			if err := g.activateDeviceSession(session, response.Grant); err != nil {
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
	case SubtypeRelayUpstream:
		frame, err := UnmarshalRelayFrame(env.Payload)
		if err != nil {
			return
		}
		if err := protocol.ValidateRelayInnerPacket(frame.InnerPacket); err != nil {
			return
		}
		g.mu.RLock()
		owner := g.deviceSessions[frame.SessionID]
		g.mu.RUnlock()
		if owner.NodeID != session.NodeID || owner.ControlSessionID != session.SessionID {
			return
		}
		if g.cluster == nil {
			return
		}
		route, ok := g.cluster.ResolveRoute(frame.SessionID)
		if !ok || route.DisableSend || route.SessionEpoch != frame.SessionEpoch || route.DomainID == 0 {
			return
		}
		if !protocol.RelayInnerIdentityMatches(frame.InnerPacket, route.Username, route.CallSign, route.SSID) {
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
	response.Success = true
	response.ExpiresAtMillis = now.Add(defaultDeviceGrantTTL).UnixMilli()
	return response
}

func deviceGrantIdentity(grant *DeviceGrant) string {
	if grant == nil {
		return ""
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

func deviceOwnerIdentity(ownerID int, ssid byte) string {
	if ownerID <= 0 {
		return ""
	}
	return fmt.Sprintf("owner:%d:ssid:%d", ownerID, ssid)
}

func (g *CenterGateway) activateDeviceSession(session *NodeSession, grant *DeviceGrant) error {
	if session == nil || grant == nil || g.cluster == nil {
		return errors.New("device session activation is incomplete")
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
	if hadOld && oldOwner.NodeID == session.NodeID && oldOwner.ControlSessionID == session.SessionID &&
		oldOwner.DeviceID == grant.DeviceID && oldOwner.OwnerID == grant.OwnerID && oldOwner.SSID == grant.SSID {
		grant.SessionID, grant.SessionEpoch = oldOwner.SessionID, oldOwner.SessionEpoch
		route, ok := g.cluster.ResolveRoute(oldOwner.SessionID)
		if !ok || route.SessionEpoch != oldOwner.SessionEpoch {
			return errors.New("edge owner route is missing")
		}
		next := grant.Route()
		if route != next {
			return g.cluster.SetNodeRoute(session.NodeID, next)
		}
		return nil
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
	owner := deviceSessionOwner{NodeID: session.NodeID, ControlSessionID: session.SessionID, SessionID: grant.SessionID, SessionEpoch: epoch, DeviceID: grant.DeviceID, OwnerID: grant.OwnerID, SSID: grant.SSID, Identity: identity}
	g.deviceSessions[grant.SessionID] = owner
	g.activeDevices[identity] = grant.SessionID
	if grant.DeviceID > 0 {
		g.activeByID[grant.DeviceID] = grant.SessionID
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
		if route != next {
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
	owner := deviceSessionOwner{NodeID: CenterLocalNodeID, ControlSessionID: grant.SessionID, SessionID: grant.SessionID, SessionEpoch: epoch, DeviceID: grant.DeviceID, OwnerID: grant.OwnerID, SSID: grant.SSID, Identity: identity}
	g.deviceSessions[grant.SessionID] = owner
	g.activeDevices[identity] = grant.SessionID
	if grant.DeviceID > 0 {
		g.activeByID[grant.DeviceID] = grant.SessionID
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
	return g.cluster.Relay(CenterLocalNodeID, RelayFrame{
		SessionID: route.SessionID, SessionEpoch: route.SessionEpoch, DomainID: route.DomainID,
		InnerPacket: inner,
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
	delete(g.deviceSessions, sessionID)
	if g.activeDevices[owner.Identity] == sessionID {
		delete(g.activeDevices, owner.Identity)
	}
	if owner.DeviceID > 0 && g.activeByID[owner.DeviceID] == sessionID {
		delete(g.activeByID, owner.DeviceID)
	}
}

func (g *CenterGateway) restoreOwnerMapsLocked(owner deviceSessionOwner) {
	g.deviceSessions[owner.SessionID] = owner
	g.activeDevices[owner.Identity] = owner.SessionID
	if owner.DeviceID > 0 {
		g.activeByID[owner.DeviceID] = owner.SessionID
	}
}

func (g *CenterGateway) notifyOwnerRevoke(owner deviceSessionOwner, reason string) {
	g.mu.RLock()
	deviceHandler := g.onDeviceRevoke
	localHandler := g.onLocalRevoke
	g.mu.RUnlock()
	if deviceHandler != nil && owner.DeviceID > 0 {
		deviceHandler(owner.NodeID, owner.ControlSessionID, owner.DeviceID, reason)
	}
	if owner.NodeID == CenterLocalNodeID && localHandler != nil {
		localHandler(owner.DeviceID, owner.OwnerID, owner.SSID, owner.SessionID, owner.SessionEpoch)
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
	route.GroupID, route.DomainID = groupID, domainID
	route.DisableSend, route.DisableRecv = disableSend, disableRecv
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
	route.GroupID, route.DomainID = groupID, domainID
	route.DisableSend, route.DisableRecv = disableSend, disableRecv
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
		if route.DomainID == domainID {
			continue
		}
		route.DomainID = domainID
		if err := g.cluster.SetNodeRoute(owner.NodeID, route); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

type EdgeGateway struct {
	client            *NodeClient
	listenAddr        string
	proxyProtocol     string
	endpoint          *udphub.EdgeEndpoint
	peer              *NodeDatagramPeer
	projection        *ProjectionStore
	mu                sync.RWMutex
	sessions          map[uint64]*edgeDeviceSession
	byIdentity        map[string]uint64
	pending           map[uint64]*pendingDeviceAuth
	pendingIdentity   map[string]uint64
	pendingRenewals   map[uint64]pendingDeviceRenewal
	renewingSessions  map[uint64]uint64
	snapshotAssembler *SnapshotAssembler
	nextRequest       atomic.Uint64
	metrics           Metrics
	closed            chan struct{}
	closeOnce         sync.Once
	cleanerWG         sync.WaitGroup
	sessionTimeout    time.Duration
	grantRenewBefore  time.Duration
	reportSession     func(DeviceSessionReport)
	renewSession      func(DeviceSessionRenewRequest)
}
type edgeDeviceSession struct {
	Grant    DeviceGrant
	Addr     *net.UDPAddr
	RealAddr *net.UDPAddr
	LastSeen time.Time
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

const edgeAuthRequestTimeout = 5 * time.Second

func NewEdgeGateway(listenAddr string, client *NodeClient, proxyProtocols ...string) (*EdgeGateway, error) {
	if client == nil {
		return nil, errors.New("edge node client is required")
	}
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
	return &EdgeGateway{client: client, listenAddr: listenAddr, proxyProtocol: proxyProtocol, projection: NewProjectionStore(p), sessions: make(map[uint64]*edgeDeviceSession), byIdentity: make(map[string]uint64), pending: make(map[uint64]*pendingDeviceAuth), pendingIdentity: make(map[string]uint64), pendingRenewals: make(map[uint64]pendingDeviceRenewal), renewingSessions: make(map[uint64]uint64), closed: make(chan struct{}), sessionTimeout: 20 * time.Second, grantRenewBefore: 30 * time.Second}, nil
}
func (g *EdgeGateway) Start() error {
	endpoint, err := udphub.NewEdgeEndpoint(g.listenAddr, g.proxyProtocol, g.handleInbound)
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
	g.cleanerWG.Add(1)
	go g.sessionCleanerLoop()
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
	g.closeOnce.Do(func() { close(g.closed) })
	var err error
	if g.endpoint != nil {
		err = g.endpoint.Close()
	}
	g.cleanerWG.Wait()
	return err
}
func (g *EdgeGateway) handleInbound(data []byte, remoteAddr, realAddr *net.UDPAddr) {
	if g.peer != nil && g.peer.Handle(data, remoteAddr) {
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
func (g *EdgeGateway) handleDevicePacket(data []byte, remoteAddr, realAddr *net.UDPAddr) {
	packet, err := protocol.NewDraARLv1RoutingPacket(remoteAddr, data)
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
			if candidate.Grant.SSID == packet.SSID && udpAddrEqual(candidate.RealAddr, realAddr) {
				sessionID, session, exists = id, candidate, true
				break
			}
		}
	}
	g.mu.RUnlock()
	if !exists || session == nil {
		if packet.Type == protocol.DraARLTypeHeartbeat || packet.Type == protocol.DraARLTypeJWTAuth {
			g.requestAuth(data, packet, remoteAddr, realAddr)
		}
		return
	}
	// The device identity in a UDP packet is not proof of session ownership.
	// A changed direct/PROXY-advertised endpoint must authenticate again; text
	// or voice packets may never take over an existing identity or its FRP
	// return path. A transport-only FRP address change is allowed when the
	// authenticated real client endpoint remains the same.
	if !udpAddrEqual(session.RealAddr, realAddr) {
		if packet.Type == protocol.DraARLTypeHeartbeat || packet.Type == protocol.DraARLTypeJWTAuth {
			g.requestAuth(data, packet, remoteAddr, realAddr)
		}
		return
	}
	// Update the endpoint and take an immutable grant snapshot while holding
	// the gateway lock. RouteDelta may update the same grant concurrently.
	now := time.Now()
	g.mu.Lock()
	current := g.sessions[sessionID]
	if current == nil {
		g.mu.Unlock()
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
	current.Addr = cloneUDPAddr(remoteAddr)
	current.RealAddr = cloneUDPAddr(realAddr)
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
	if g.sessionTimeout <= 0 {
		return 0
	}
	reports := make([]DeviceSessionReport, 0)
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
	for sessionID, session := range g.sessions {
		reason := ""
		if now.Sub(session.LastSeen) > g.sessionTimeout {
			reason = "device_timeout"
		} else if session.Grant.ExpiresAtMillis > 0 && now.UnixMilli() >= session.Grant.ExpiresAtMillis {
			reason = "grant_expired"
		}
		if reason != "" {
			reports = append(reports, g.removeEdgeSessionLocked(sessionID, reason, now))
		}
	}
	g.mu.Unlock()
	for _, report := range reports {
		g.sendDeviceSessionReport(report)
	}
	return len(reports)
}

func (g *EdgeGateway) requestSessionRenewal(sessionID, sessionEpoch uint64, now time.Time) {
	if sessionID == 0 || sessionEpoch == 0 {
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
	if err == nil && g.client != nil {
		env := NewEnvelope(SubtypeDeviceSessionRenew, g.client.Session.NodeID, g.client.Session.SessionID, requestID, payload)
		env.Flags = FlagControl | FlagAck
		err = g.client.SendEnvelope(env)
	}
	if err != nil || g.client == nil {
		g.mu.Lock()
		delete(g.pendingRenewals, requestID)
		if g.renewingSessions[sessionID] == requestID {
			delete(g.renewingSessions, sessionID)
		}
		g.mu.Unlock()
	}
}

func (g *EdgeGateway) removeEdgeSessionLocked(sessionID uint64, reason string, now time.Time) DeviceSessionReport {
	session := g.sessions[sessionID]
	if session == nil {
		return DeviceSessionReport{}
	}
	delete(g.sessions, sessionID)
	key := fmt.Sprintf("%s-%d", session.Grant.Username, session.Grant.SSID)
	if g.byIdentity[key] == sessionID {
		delete(g.byIdentity, key)
	}
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
	payload, err := EncodeJSON(report)
	if err != nil {
		return
	}
	env := NewEnvelope(SubtypeDeviceSessionReport, g.client.Session.NodeID, g.client.Session.SessionID, g.nextRequest.Add(1), payload)
	env.Flags = FlagControl
	_ = g.client.SendEnvelope(env)
}
func (g *EdgeGateway) requestAuth(data []byte, packet *protocol.DraARLv1Packet, remoteAddr, realAddr *net.UDPAddr) {
	identity := g.identity(packet)
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
	case SubtypeDeviceSessionRenew:
		var response DeviceSessionRenewResponse
		if DecodeJSON(env.Payload, &response) != nil {
			return
		}
		g.finishSessionRenewal(response, time.Now())
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
	delete(g.sessions, revoke.SessionID)
	key := fmt.Sprintf("%s-%d", session.Grant.Username, session.Grant.SSID)
	if g.byIdentity[key] == revoke.SessionID {
		delete(g.byIdentity, key)
	}
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
	session := &edgeDeviceSession{Grant: grant, Addr: cloneUDPAddr(pending.addr), RealAddr: cloneUDPAddr(pending.realAddr), LastSeen: time.Now()}
	key := fmt.Sprintf("%s-%d", grant.Username, grant.SSID)
	g.mu.Lock()
	if existing := g.sessions[grant.SessionID]; existing != nil {
		existing.Grant = grant
		existing.Addr = session.Addr
		existing.RealAddr = session.RealAddr
		existing.LastSeen = session.LastSeen
	} else {
		if previousID := g.byIdentity[key]; previousID != 0 && previousID != grant.SessionID {
			delete(g.sessions, previousID)
		}
		g.sessions[grant.SessionID] = session
	}
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
			key := fmt.Sprintf("%s-%d", session.Grant.Username, session.Grant.SSID)
			if g.byIdentity[key] == id {
				delete(g.byIdentity, key)
			}
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
	if g.endpoint != nil && g.endpoint.Fanout(data, targets, 0, "", 0, func(result udphub.EdgeFanoutResult) {
		if result.Sent > 0 {
			g.metrics.AddOutBulk(uint64(result.Sent), uint64(result.Sent)*uint64(len(data)))
		}
		if result.Errors > 0 {
			g.metrics.AddErrorBulk(uint64(result.Errors))
		}
		if result.Dropped > 0 {
			g.metrics.AddDropBulk(uint64(result.Dropped))
		}
	}) {
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

func udpAddrEqual(a, b *net.UDPAddr) bool {
	if a == nil || b == nil || a.Port != b.Port || a.Zone != b.Zone {
		return false
	}
	return a.IP.Equal(b.IP)
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
