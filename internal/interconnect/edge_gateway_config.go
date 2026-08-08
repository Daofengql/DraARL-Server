package interconnect

import (
	"errors"
	"net"
	"time"

	"draarl/internal/protocol"
)

type pendingDeviceConfigUp struct {
	envelope     Envelope
	sessionID    uint64
	sessionEpoch uint64
	requestedAt  time.Time
	lastSentAt   time.Time
	attempts     int
}

const maxEdgePendingDeviceConfigs = 256

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
