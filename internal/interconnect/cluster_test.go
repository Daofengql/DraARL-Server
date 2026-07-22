package interconnect

import "testing"

func TestClusterDomainTargetsExcludeSourceAndDisabledRoutes(t *testing.T) {
	m := NewClusterManager(1)
	m.nodeProjection["a"] = &Projection{ClusterEpoch: 1, Version: 1, Devices: map[uint64]DeviceRoute{1: {SessionID: 1, DomainID: 5}}}
	m.nodeProjection["b"] = &Projection{ClusterEpoch: 1, Version: 1, Devices: map[uint64]DeviceRoute{2: {SessionID: 2, DomainID: 5, DisableRecv: true}}}
	m.nodeProjection["c"] = &Projection{ClusterEpoch: 1, Version: 1, Devices: map[uint64]DeviceRoute{3: {SessionID: 3, DomainID: 5}}}
	m.RebuildDomainNodes()
	targets := m.TargetNodes(5, "a")
	if len(targets) != 1 || targets[0] != "c" {
		t.Fatalf("targets=%v", targets)
	}
}

func TestClusterReconnectRemovesOldNodeRoutesBeforeNewSession(t *testing.T) {
	m := NewClusterManager(1)
	defer m.Close()
	oldSession := &NodeSession{NodeID: "edge-a", SessionID: 10}
	m.OnConnect(oldSession)
	if err := m.UpsertNodeRoute("edge-a", DeviceRoute{SessionID: 101, DomainID: 7, SessionEpoch: 1}); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.ResolveRoute(101); !ok {
		t.Fatal("old route was not installed")
	}
	newSession := &NodeSession{NodeID: "edge-a", SessionID: 11}
	m.OnConnect(newSession)
	if _, ok := m.ResolveRoute(101); ok {
		t.Fatal("old route survived same-node reconnect")
	}
	if m.OnDisconnect(oldSession, nil) {
		t.Fatal("stale disconnect was treated as current")
	}
	if status := m.NodeStatus("edge-a"); !status.Online {
		t.Fatal("stale disconnect removed the replacement session")
	}
}
