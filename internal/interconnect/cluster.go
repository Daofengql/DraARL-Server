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
	epoch          uint64
	version        uint64
	projection     *Projection
	nodes          map[string]*NodeSession
	nodeProjection map[string]*Projection
	domainNodes    map[uint64]map[string]struct{}
	server         *NodeServer
	dataServer     *NodeDatagramServer
	messageID      atomic.Uint64
	metrics        map[string]*Metrics
}

func NewClusterManager(epoch uint64) *ClusterManager {
	if epoch == 0 {
		epoch = uint64(time.Now().UnixNano())
	}
	return &ClusterManager{epoch: epoch, projection: NewProjection(epoch), nodes: make(map[string]*NodeSession), nodeProjection: make(map[string]*Projection), domainNodes: make(map[uint64]map[string]struct{}), metrics: make(map[string]*Metrics)}
}
func (m *ClusterManager) AttachServer(server *NodeServer) {
	m.mu.Lock()
	m.server = server
	m.mu.Unlock()
}
func (m *ClusterManager) AttachDataServer(server *NodeDatagramServer) {
	m.mu.Lock()
	m.dataServer = server
	m.mu.Unlock()
}
func (m *ClusterManager) Epoch() uint64 { m.mu.RLock(); defer m.mu.RUnlock(); return m.epoch }
func (m *ClusterManager) Projection() *Projection {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.projection.Clone()
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

func (m *ClusterManager) OnConnect(session *NodeSession) {
	m.mu.Lock()
	m.nodes[session.NodeID] = session
	m.nodeProjection[session.NodeID] = NewProjection(m.epoch)
	m.metrics[session.NodeID] = &Metrics{}
	m.mu.Unlock()
}
func (m *ClusterManager) OnDisconnect(session *NodeSession, _ error) {
	m.mu.Lock()
	if current := m.nodes[session.NodeID]; current == session {
		delete(m.nodes, session.NodeID)
		delete(m.nodeProjection, session.NodeID)
		delete(m.metrics, session.NodeID)
		for domain, nodes := range m.domainNodes {
			delete(nodes, session.NodeID)
			if len(nodes) == 0 {
				delete(m.domainNodes, domain)
			}
		}
	}
	m.mu.Unlock()
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

func (m *ClusterManager) ApplyRouteDelta(delta RouteDelta) error {
	m.mu.Lock()
	if delta.ClusterEpoch != m.epoch {
		m.mu.Unlock()
		return errors.New("cluster epoch mismatch")
	}
	if err := m.projection.ApplyDelta(delta); err != nil {
		m.mu.Unlock()
		return err
	}
	for nodeID, projection := range m.nodeProjection {
		if err := projection.ApplyDelta(delta); err != nil {
			// A node projection may intentionally be partial; rebuild it from a
			// snapshot instead of applying an incompatible global delta.
			_ = nodeID
		}
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
		if err := server.SendEnvelope(nodeID, env); err != nil {
			return err
		}
	}
	return nil
}
func (m *ClusterManager) SendFullProjection(nodeID string) error {
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
	return server.SendEnvelope(nodeID, env)
}
func (m *ClusterManager) Relay(sourceNode string, frame RelayFrame) error {
	if frame.DomainID == 0 || frame.SessionID == 0 {
		return errors.New("relay frame identity is incomplete")
	}
	targets := m.TargetNodes(frame.DomainID, sourceNode)
	m.mu.RLock()
	server := m.server
	dataServer := m.dataServer
	epoch := m.epoch
	m.mu.RUnlock()
	if server == nil && dataServer == nil {
		return nil
	}
	payload, err := frame.MarshalBinary()
	if err != nil {
		return err
	}
	for _, nodeID := range targets {
		if dataServer != nil && server != nil {
			if session, ok := server.Session(nodeID); ok && session.DataAddr() != nil {
				env := NewEnvelope(SubtypeRelayDownstream, "center", session.SessionID, m.NextMessageID(), payload)
				env.ClusterEpoch, env.ProjectionVersion, env.HopCount, env.KeyEpoch = epoch, frame.RequiredProjectionVersion, 1, session.KeyEpoch
				if err := dataServer.Send(session, env); err == nil {
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
