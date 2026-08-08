package interconnect

import (
	"draarl/internal/protocol"
	"errors"
	"fmt"
	"log"
	"time"
)

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
