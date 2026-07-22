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
