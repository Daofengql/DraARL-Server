package interconnect

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ClusterManager is the centre's in-memory authoritative node/session index.
// Database/API code can publish committed state through ApplyRouteDelta; the
// manager then pushes the same projection to affected edge sessions.
type ClusterManager struct {
	mu             sync.RWMutex
	publishMu      sync.Mutex
	epoch          uint64
	version        uint64
	projection     *Projection
	nodes          map[string]*NodeSession
	nodeProjection map[string]*Projection
	domainNodes    map[uint64]map[string]struct{}
	server         *NodeServer
	dataBridge     *NodeDatagramBridge
	messageID      atomic.Uint64
	metrics        map[string]*Metrics
	status         map[string]NodeHeartbeat
	statusReceived map[string]time.Time
	ackedVersion   map[string]uint64
	ackedMessage   map[string]uint64
	pendingControl map[string]map[uint64]*pendingControl
	syncError      map[string]string
	rateTrackers   map[string]*nodeRateTracker
	closed         chan struct{}
	closeOnce      sync.Once
	retryWG        sync.WaitGroup
}

type pendingControl struct {
	envelope Envelope
	version  uint64
	sentAt   time.Time
	attempts int
}

const (
	controlRetryInterval = time.Second
	controlMaxAttempts   = 5
)

func NewClusterManager(epoch uint64) *ClusterManager {
	if epoch == 0 {
		epoch = uint64(time.Now().UnixNano())
	}
	m := &ClusterManager{epoch: epoch, projection: NewProjection(epoch), nodes: make(map[string]*NodeSession), nodeProjection: make(map[string]*Projection), domainNodes: make(map[uint64]map[string]struct{}), metrics: make(map[string]*Metrics), status: make(map[string]NodeHeartbeat), statusReceived: make(map[string]time.Time), ackedVersion: make(map[string]uint64), ackedMessage: make(map[string]uint64), pendingControl: make(map[string]map[uint64]*pendingControl), syncError: make(map[string]string), rateTrackers: make(map[string]*nodeRateTracker), closed: make(chan struct{})}
	m.retryWG.Add(1)
	go m.retryControlLoop()
	return m
}
func (m *ClusterManager) AttachServer(server *NodeServer) {
	m.mu.Lock()
	m.server = server
	m.mu.Unlock()
}
func (m *ClusterManager) AttachDataBridge(bridge *NodeDatagramBridge) {
	m.mu.Lock()
	m.dataBridge = bridge
	m.mu.Unlock()
}
func (m *ClusterManager) Epoch() uint64 { m.mu.RLock(); defer m.mu.RUnlock(); return m.epoch }
func (m *ClusterManager) Projection() *Projection {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.projection.Clone()
}
func (m *ClusterManager) ResolveRoute(sessionID uint64) (DeviceRoute, bool) {
	m.mu.RLock()
	route, ok := m.projection.Devices[sessionID]
	m.mu.RUnlock()
	return route, ok
}
func (m *ClusterManager) NextMessageID() uint64 { return m.messageID.Add(1) }
func (m *ClusterManager) Metrics(nodeID string) *Metrics {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.metrics[nodeID] == nil {
		m.metrics[nodeID] = &Metrics{}
	}
	return m.metrics[nodeID]
}
func (m *ClusterManager) UpdateNodeHeartbeat(nodeID string, heartbeat NodeHeartbeat) {
	receivedAt := time.Now()
	m.mu.Lock()
	m.status[nodeID] = heartbeat
	m.statusReceived[nodeID] = receivedAt
	tracker := m.rateTrackers[nodeID]
	if tracker == nil {
		tracker = &nodeRateTracker{}
		m.rateTrackers[nodeID] = tracker
	}
	center := MetricsSnapshot{}
	if session := m.nodes[nodeID]; session != nil {
		center = AddMetricsSnapshots(session.DataMetrics.Snapshot(), session.ControlMetrics.Snapshot())
	}
	tracker.observe(heartbeat.InstanceID, heartbeat.Device, heartbeat.Interconnect, center, receivedAt)
	m.mu.Unlock()
}

type NodeStatus struct {
	NodeID         string           `json:"node_id"`
	Online         bool             `json:"online"`
	RemoteAddr     string           `json:"remote_addr,omitempty"`
	ConnectedAt    *time.Time       `json:"connected_at,omitempty"`
	LastHeartbeat  *time.Time       `json:"last_heartbeat,omitempty"`
	Heartbeat      NodeHeartbeat    `json:"heartbeat"`
	CenterData     MetricsSnapshot  `json:"center_interconnect"`
	TrafficRates   NodeTrafficRates `json:"traffic_rates"`
	AckedVersion   uint64           `json:"acked_projection_version"`
	PendingControl int              `json:"pending_control"`
	SyncError      string           `json:"sync_error,omitempty"`
}

func (m *ClusterManager) NodeStatus(nodeID string) NodeStatus {
	m.mu.RLock()
	status := NodeStatus{NodeID: nodeID, Heartbeat: m.status[nodeID], AckedVersion: m.ackedVersion[nodeID], PendingControl: len(m.pendingControl[nodeID]), SyncError: m.syncError[nodeID]}
	if receivedAt, ok := m.statusReceived[nodeID]; ok {
		copyTime := receivedAt
		status.LastHeartbeat = &copyTime
	}
	if session := m.nodes[nodeID]; session != nil {
		status.Online = true
		status.RemoteAddr = session.RemoteAddr
		connectedAt := session.ConnectedAt
		status.ConnectedAt = &connectedAt
		status.CenterData = AddMetricsSnapshots(session.DataMetrics.Snapshot(), session.ControlMetrics.Snapshot())
	}
	metricsCurrent := status.Online && status.LastHeartbeat != nil && (status.ConnectedAt == nil || !status.LastHeartbeat.Before(*status.ConnectedAt))
	status.TrafficRates = m.rateTrackers[nodeID].snapshot(metricsCurrent)
	m.mu.RUnlock()
	return status
}
func (m *ClusterManager) NodeHeartbeat(nodeID string) (NodeHeartbeat, bool) {
	m.mu.RLock()
	heartbeat, ok := m.status[nodeID]
	m.mu.RUnlock()
	return heartbeat, ok
}

// MarkRouteAck records the highest route projection version applied by a node.
// It is intentionally non-blocking for the relay hot path, while exposing an
// exact synchronization point to management and retry logic.
func (m *ClusterManager) MarkRouteAck(nodeID string, ack RouteAck) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ack.ClusterEpoch != m.epoch || ack.AckForMessageID == 0 {
		return false
	}
	pendingByNode := m.pendingControl[nodeID]
	pending := pendingByNode[ack.AckForMessageID]
	if pending == nil || pending.version != ack.ProjectionVersion {
		return false
	}
	delete(pendingByNode, ack.AckForMessageID)
	if len(pendingByNode) == 0 {
		delete(m.pendingControl, nodeID)
	}
	if ack.Error != "" {
		m.syncError[nodeID] = ack.Error
		return true
	}
	delete(m.syncError, nodeID)
	if ack.ProjectionVersion > m.ackedVersion[nodeID] {
		m.ackedVersion[nodeID] = ack.ProjectionVersion
		if session := m.nodes[nodeID]; session != nil {
			session.AckedProjectionVersion.Store(ack.ProjectionVersion)
		}
	}
	if ack.AckForMessageID != 0 {
		m.ackedMessage[nodeID] = ack.AckForMessageID
	}
	return true
}
func (m *ClusterManager) RouteAck(nodeID string) (version, messageID uint64) {
	m.mu.RLock()
	version, messageID = m.ackedVersion[nodeID], m.ackedMessage[nodeID]
	m.mu.RUnlock()
	return version, messageID
}
func (m *ClusterManager) PendingControl(nodeID string) int {
	m.mu.RLock()
	count := len(m.pendingControl[nodeID])
	m.mu.RUnlock()
	return count
}
func (m *ClusterManager) SyncError(nodeID string) string {
	m.mu.RLock()
	err := m.syncError[nodeID]
	m.mu.RUnlock()
	return err
}

func (m *ClusterManager) OnConnect(session *NodeSession) {
	m.mu.Lock()
	m.removeNodeProjectionLocked(session.NodeID)
	m.nodes[session.NodeID] = session
	m.nodeProjection[session.NodeID] = NewProjection(m.epoch)
	m.metrics[session.NodeID] = &Metrics{}
	m.mu.Unlock()
}
func (m *ClusterManager) OnDisconnect(session *NodeSession, _ error) bool {
	m.mu.Lock()
	currentSession := false
	if current := m.nodes[session.NodeID]; current == session {
		currentSession = true
		m.removeNodeProjectionLocked(session.NodeID)
		delete(m.nodes, session.NodeID)
		delete(m.nodeProjection, session.NodeID)
		delete(m.metrics, session.NodeID)
		delete(m.ackedVersion, session.NodeID)
		delete(m.ackedMessage, session.NodeID)
		delete(m.pendingControl, session.NodeID)
		delete(m.syncError, session.NodeID)
		for domain, nodes := range m.domainNodes {
			delete(nodes, session.NodeID)
			if len(nodes) == 0 {
				delete(m.domainNodes, domain)
			}
		}
	}
	m.mu.Unlock()
	return currentSession
}

func (m *ClusterManager) removeNodeProjectionLocked(nodeID string) {
	projection := m.nodeProjection[nodeID]
	if projection == nil {
		return
	}
	for sessionID := range projection.Devices {
		delete(m.projection.Devices, sessionID)
	}
	delete(m.nodeProjection, nodeID)
	for domainID, nodes := range m.domainNodes {
		delete(nodes, nodeID)
		if len(nodes) == 0 {
			delete(m.domainNodes, domainID)
		}
	}
}
func (m *ClusterManager) RegisterProjectionNode(nodeID string, session *NodeSession) {
	m.mu.Lock()
	m.nodes[nodeID] = session
	if _, ok := m.nodeProjection[nodeID]; !ok {
		m.nodeProjection[nodeID] = NewProjection(m.epoch)
	}
	m.mu.Unlock()
}
func (m *ClusterManager) RebuildDomainNodes() {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := make(map[uint64]map[string]struct{})
	for nodeID, projection := range m.nodeProjection {
		for _, route := range projection.Devices {
			if route.DisableRecv || route.DomainID == 0 {
				continue
			}
			set := next[route.DomainID]
			if set == nil {
				set = make(map[string]struct{})
				next[route.DomainID] = set
			}
			set[nodeID] = struct{}{}
		}
	}
	m.domainNodes = next
}
func (m *ClusterManager) TargetNodes(domainID uint64, sourceNode string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	set := m.domainNodes[domainID]
	result := make([]string, 0, len(set))
	for nodeID := range set {
		if nodeID != sourceNode {
			result = append(result, nodeID)
		}
	}
	return result
}
func (m *ClusterManager) UpsertNodeRoute(nodeID string, route DeviceRoute) error {
	m.mu.Lock()
	if m.projection.Devices == nil {
		m.projection.Devices = make(map[uint64]DeviceRoute)
	}
	m.projection.Devices[route.SessionID] = route
	projection := m.nodeProjection[nodeID]
	if projection == nil {
		projection = NewProjection(m.epoch)
		m.nodeProjection[nodeID] = projection
	}
	if projection.Devices == nil {
		projection.Devices = make(map[uint64]DeviceRoute)
	}
	projection.Devices[route.SessionID] = route
	m.mu.Unlock()
	m.RebuildDomainNodes()
	return nil
}

// SetNodeRoute publishes one authoritative route change to one edge. Each
// edge has its own contiguous projection version, so unrelated nodes cannot
// create version holes in this node's stream.
func (m *ClusterManager) SetNodeRoute(nodeID string, route DeviceRoute) error {
	m.publishMu.Lock()
	defer m.publishMu.Unlock()
	m.mu.Lock()
	if nodeID == "" || route.SessionID == 0 {
		m.mu.Unlock()
		return errors.New("node route identity is incomplete")
	}
	projection := m.nodeProjection[nodeID]
	if projection == nil {
		projection = NewProjection(m.epoch)
		m.nodeProjection[nodeID] = projection
	}
	base := projection.Version
	delta := NewRouteDelta(m.epoch, base, base+1, []DeltaOperation{{Kind: "upsert", Route: &route}})
	if err := projection.ApplyDelta(delta); err != nil {
		m.mu.Unlock()
		return err
	}
	if m.projection.Devices == nil {
		m.projection.Devices = make(map[uint64]DeviceRoute)
	}
	m.projection.Devices[route.SessionID] = route
	server := m.server
	m.mu.Unlock()
	m.RebuildDomainNodes()
	if server == nil {
		return nil
	}
	payload, _ := EncodeJSON(delta)
	env := NewEnvelope(SubtypeRouteDelta, "center", 0, m.NextMessageID(), payload)
	env.ClusterEpoch, env.ProjectionVersion, env.Flags = delta.ClusterEpoch, delta.NewVersion, FlagControl|FlagAck
	return m.sendReliable(nodeID, env, delta.NewVersion)
}

func (m *ClusterManager) RemoveNodeRoute(nodeID string, sessionID uint64) error {
	m.publishMu.Lock()
	defer m.publishMu.Unlock()
	m.mu.Lock()
	projection := m.nodeProjection[nodeID]
	if projection == nil {
		m.mu.Unlock()
		return errors.New("node projection not found")
	}
	base := projection.Version
	delta := NewRouteDelta(m.epoch, base, base+1, []DeltaOperation{{Kind: "remove", SessionID: sessionID}})
	if err := projection.ApplyDelta(delta); err != nil {
		m.mu.Unlock()
		return err
	}
	delete(m.projection.Devices, sessionID)
	server := m.server
	m.mu.Unlock()
	m.RebuildDomainNodes()
	if server == nil {
		return nil
	}
	payload, _ := EncodeJSON(delta)
	env := NewEnvelope(SubtypeRouteDelta, "center", 0, m.NextMessageID(), payload)
	env.ClusterEpoch, env.ProjectionVersion, env.Flags = delta.ClusterEpoch, delta.NewVersion, FlagControl|FlagAck
	return m.sendReliable(nodeID, env, delta.NewVersion)
}

func (m *ClusterManager) ApplyRouteDelta(delta RouteDelta) error {
	m.publishMu.Lock()
	defer m.publishMu.Unlock()
	m.mu.Lock()
	if delta.ClusterEpoch != m.epoch {
		m.mu.Unlock()
		return errors.New("cluster epoch mismatch")
	}
	if err := m.projection.ApplyDelta(delta); err != nil {
		m.mu.Unlock()
		return err
	}
	m.version = delta.NewVersion
	m.mu.Unlock()
	m.RebuildDomainNodes()
	return m.pushDelta(delta)
}
func (m *ClusterManager) pushDelta(delta RouteDelta) error {
	payload, err := EncodeJSON(delta)
	if err != nil {
		return err
	}
	m.mu.RLock()
	server := m.server
	nodes := make([]string, 0, len(m.nodes))
	for nodeID := range m.nodes {
		nodes = append(nodes, nodeID)
	}
	m.mu.RUnlock()
	if server == nil {
		return nil
	}
	for _, nodeID := range nodes {
		env := NewEnvelope(SubtypeRouteDelta, "center", 0, m.NextMessageID(), payload)
		env.ClusterEpoch, env.ProjectionVersion, env.Flags = delta.ClusterEpoch, delta.NewVersion, FlagControl|FlagAck
		if err := m.sendReliable(nodeID, env, delta.NewVersion); err != nil {
			return err
		}
	}
	return nil
}
func (m *ClusterManager) SendFullProjection(nodeID string) error {
	m.publishMu.Lock()
	defer m.publishMu.Unlock()
	m.mu.RLock()
	p := m.projection.Clone()
	if local := m.nodeProjection[nodeID]; local != nil {
		p = local.Clone()
	}
	server := m.server
	m.mu.RUnlock()
	if server == nil {
		return nil
	}
	begin, chunks, err := SplitProjection(m.NextMessageID(), p)
	if err != nil {
		return err
	}
	for _, item := range []any{begin} {
		payload, _ := EncodeJSON(item)
		env := NewEnvelope(SubtypeRouteSnapshotBegin, "center", 0, m.NextMessageID(), payload)
		env.ClusterEpoch, env.ProjectionVersion, env.Flags = begin.ClusterEpoch, begin.ProjectionVersion, FlagControl|FlagChunked
		if err := server.SendEnvelope(nodeID, env); err != nil {
			return err
		}
	}
	for _, chunk := range chunks {
		payload, _ := EncodeJSON(chunk)
		env := NewEnvelope(SubtypeRouteSnapshotChunk, "center", 0, m.NextMessageID(), payload)
		env.ClusterEpoch, env.ProjectionVersion, env.Flags = begin.ClusterEpoch, begin.ProjectionVersion, FlagControl|FlagChunked
		if err := server.SendEnvelope(nodeID, env); err != nil {
			return err
		}
	}
	payload, _ := EncodeJSON(SnapshotCommit{SnapshotID: begin.SnapshotID})
	env := NewEnvelope(SubtypeRouteSnapshotCommit, "center", 0, m.NextMessageID(), payload)
	env.ClusterEpoch, env.ProjectionVersion, env.Flags = begin.ClusterEpoch, begin.ProjectionVersion, FlagControl|FlagAck
	return m.sendReliable(nodeID, env, begin.ProjectionVersion)
}

func (m *ClusterManager) sendReliable(nodeID string, env Envelope, version uint64) error {
	m.mu.Lock()
	server := m.server
	if server == nil {
		m.mu.Unlock()
		return nil
	}
	byNode := m.pendingControl[nodeID]
	if byNode == nil {
		byNode = make(map[uint64]*pendingControl)
		m.pendingControl[nodeID] = byNode
	}
	byNode[env.MessageID] = &pendingControl{envelope: env, version: version, sentAt: time.Now(), attempts: 1}
	m.mu.Unlock()
	if err := server.SendEnvelope(nodeID, env); err != nil {
		m.mu.Lock()
		if current := m.pendingControl[nodeID]; current != nil {
			delete(current, env.MessageID)
			if len(current) == 0 {
				delete(m.pendingControl, nodeID)
			}
		}
		m.mu.Unlock()
		return err
	}
	return nil
}

func (m *ClusterManager) retryControlLoop() {
	defer m.retryWG.Done()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-m.closed:
			return
		case now := <-ticker.C:
			m.retryDueControl(now)
		}
	}
}

type controlRetry struct {
	nodeID string
	env    Envelope
}

func (m *ClusterManager) retryDueControl(now time.Time) {
	var retries []controlRetry
	var closeSessions []*NodeSession
	m.mu.Lock()
	for nodeID, byMessage := range m.pendingControl {
		for messageID, pending := range byMessage {
			if now.Sub(pending.sentAt) < controlRetryInterval {
				continue
			}
			if pending.attempts >= controlMaxAttempts {
				delete(byMessage, messageID)
				m.syncError[nodeID] = "route_ack_timeout"
				if session := m.nodes[nodeID]; session != nil {
					closeSessions = append(closeSessions, session)
				}
				continue
			}
			pending.attempts++
			pending.sentAt = now
			retries = append(retries, controlRetry{nodeID: nodeID, env: pending.envelope})
		}
		if len(byMessage) == 0 {
			delete(m.pendingControl, nodeID)
		}
	}
	server := m.server
	m.mu.Unlock()
	if server != nil {
		for _, retry := range retries {
			_ = server.SendEnvelope(retry.nodeID, retry.env)
		}
	}
	for _, session := range closeSessions {
		_ = session.Close()
	}
}

func (m *ClusterManager) Close() {
	if m == nil {
		return
	}
	m.closeOnce.Do(func() {
		close(m.closed)
		m.retryWG.Wait()
	})
}
func (m *ClusterManager) Relay(sourceNode string, frame RelayFrame) error {
	if frame.DomainID == 0 || frame.SessionID == 0 {
		return errors.New("relay frame identity is incomplete")
	}
	targets := m.TargetNodes(frame.DomainID, sourceNode)
	m.mu.RLock()
	server := m.server
	dataBridge := m.dataBridge
	epoch := m.epoch
	m.mu.RUnlock()
	if server == nil && dataBridge == nil {
		return nil
	}
	payload, err := frame.MarshalBinary()
	if err != nil {
		return err
	}
	for _, nodeID := range targets {
		if dataBridge != nil && server != nil {
			if session, ok := server.Session(nodeID); ok && session.DataAddr() != nil {
				env := NewEnvelope(SubtypeRelayDownstream, "center", session.SessionID, m.NextMessageID(), payload)
				env.ClusterEpoch, env.ProjectionVersion, env.HopCount, env.KeyEpoch = epoch, frame.RequiredProjectionVersion, 1, session.KeyEpoch
				if err := dataBridge.Send(session, env); err == nil {
					continue
				}
			}
		}
		if server == nil {
			continue
		}
		env := NewEnvelope(SubtypeRelayDownstream, "center", 0, m.NextMessageID(), payload)
		env.ClusterEpoch, env.ProjectionVersion, env.HopCount = epoch, frame.RequiredProjectionVersion, 1
		if err := server.SendEnvelope(nodeID, env); err != nil {
			return err
		}
	}
	return nil
}
func (m *ClusterManager) String() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return fmt.Sprintf("epoch=%d version=%d nodes=%d", m.epoch, m.version, len(m.nodes))
}
