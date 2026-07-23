package handler

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"draarl/internal/gormdb"
	"draarl/internal/interconnect"
)

func TestPublishedEdgeAccessPointRequiresFreshOnlineRegisteredNode(t *testing.T) {
	now := time.Now()
	nodeID, publicID := "edge-private-internal-id", "ap-public-id"
	registeredAt := now.Add(-time.Hour)
	lastHeartbeat := now.Add(-time.Second)
	node := &gormdb.Server{
		NodeID: &nodeID, PublicAccessID: &publicID, DisplayName: "福州电信入口",
		PublicAccessEnabled: true, PublicUDPHost: "edge.example.com", PublicUDPPort: 60050,
		PublicRegion: "福建省 福州市", PublicNetwork: "电信", PublicPriority: 10, Status: 1,
		NodeRegisteredAt: &registeredAt, Note: "private-note", NodeRemoteAddr: "10.0.0.1:1234",
	}
	status := interconnect.NodeStatus{Online: true, LastHeartbeat: &lastHeartbeat}

	item, ok := publishedEdgeAccessPoint(node, status, now, 20*time.Second)
	if !ok {
		t.Fatal("fresh online node was not published")
	}
	if item.ID != publicID || item.DisplayName != node.DisplayName || item.UDPHost != "edge.example.com" {
		t.Fatalf("unexpected public item: %+v", item)
	}

	encoded, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{nodeID, node.Note, node.NodeRemoteAddr, "node_id", "remote_addr", "metrics", "credential", "token"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("public item leaked %q: %s", forbidden, encoded)
		}
	}

	tests := []struct {
		name   string
		mutate func(*gormdb.Server, *interconnect.NodeStatus)
	}{
		{"not public", func(n *gormdb.Server, _ *interconnect.NodeStatus) { n.PublicAccessEnabled = false }},
		{"disabled", func(n *gormdb.Server, _ *interconnect.NodeStatus) { n.Status = 0 }},
		{"unregistered", func(n *gormdb.Server, _ *interconnect.NodeStatus) { n.NodeRegisteredAt = nil }},
		{"offline", func(_ *gormdb.Server, s *interconnect.NodeStatus) { s.Online = false }},
		{"no heartbeat", func(_ *gormdb.Server, s *interconnect.NodeStatus) { s.LastHeartbeat = nil }},
		{"stale", func(_ *gormdb.Server, s *interconnect.NodeStatus) {
			stale := now.Add(-time.Minute)
			s.LastHeartbeat = &stale
		}},
		{"invalid host", func(n *gormdb.Server, _ *interconnect.NodeStatus) { n.PublicUDPHost = "https://bad.example" }},
		{"empty region", func(n *gormdb.Server, _ *interconnect.NodeStatus) { n.PublicRegion = "" }},
		{"province-only region", func(n *gormdb.Server, _ *interconnect.NodeStatus) { n.PublicRegion = "福建省" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			copyNode, copyStatus := *node, status
			tt.mutate(&copyNode, &copyStatus)
			if _, ok := publishedEdgeAccessPoint(&copyNode, copyStatus, now, 20*time.Second); ok {
				t.Fatal("ineligible node was published")
			}
		})
	}
}
