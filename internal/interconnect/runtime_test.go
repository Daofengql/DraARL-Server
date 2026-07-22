package interconnect

import (
	"crypto/tls"
	"net"
	"testing"
	"time"
)

func TestCenterAndEdgeRuntimeConnectWithoutExternalDependencies(t *testing.T) {
	serverTLS, roots, err := NewSelfSignedTLSConfig("localhost")
	if err != nil {
		t.Fatal(err)
	}
	center, err := StartCenterRuntime(CenterRuntimeConfig{
		ControlListen: "127.0.0.1:0", TLSConfig: serverTLS,
		ValidateToken: func(nodeID, token string) bool { return nodeID == "edge-test" && token == "token" },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer center.Close()
	clientTLS := &tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS13}
	edge, err := StartEdgeRuntime(EdgeRuntimeConfig{NodeID: "edge-test", Token: "token", CenterControl: center.Control.Addr().String(), Listen: "127.0.0.1:0", TLSConfig: clientTLS})
	if err != nil {
		t.Fatal(err)
	}
	defer edge.Close()
	if edge.Gateway.Addr() == nil || edge.Client.Session == nil {
		t.Fatal("edge runtime did not start")
	}
	select {
	case <-edge.Client.Done():
		t.Fatal("edge disconnected immediately")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestEdgeStartsBeforeCenterAndReconnectsAfterCenterRestart(t *testing.T) {
	serverTLS, roots, err := NewSelfSignedTLSConfig("localhost")
	if err != nil {
		t.Fatal(err)
	}
	reserved, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	controlAddr := reserved.Addr().String()
	_ = reserved.Close()
	clientTLS := &tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS13}
	edge, err := StartEdgeRuntime(EdgeRuntimeConfig{
		NodeID: "edge-reconnect", Token: "token", CenterControl: controlAddr, Listen: "127.0.0.1:0", TLSConfig: clientTLS,
		ConnectTimeout: 100 * time.Millisecond, ReconnectMin: 20 * time.Millisecond, ReconnectMax: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer edge.Close()
	if edge.Gateway.Addr() == nil || edge.CurrentClient() != nil {
		t.Fatalf("disconnected edge startup state: addr=%v client=%v", edge.Gateway.Addr(), edge.CurrentClient())
	}
	startCenter := func() *CenterRuntime {
		center, startErr := StartCenterRuntime(CenterRuntimeConfig{
			ControlListen: controlAddr, TLSConfig: serverTLS,
			ValidateToken: func(nodeID, token string) bool { return nodeID == "edge-reconnect" && token == "token" },
		})
		if startErr != nil {
			t.Fatal(startErr)
		}
		return center
	}
	center := startCenter()
	first := waitForReadyEdgeClient(t, edge, 3*time.Second)
	firstSessionID := first.Session.SessionID
	firstEpoch := edge.Gateway.projection.Snapshot().ClusterEpoch
	center.Close()
	waitForCondition(t, 3*time.Second, func() bool { return edge.CurrentClient() == nil && edge.Gateway.currentControl(false) == nil }, "edge did not observe center shutdown")
	center = startCenter()
	defer center.Close()
	second := waitForReadyEdgeClient(t, edge, 3*time.Second)
	if second.Session.SessionID == firstSessionID {
		t.Fatal("reconnect reused the old node session")
	}
	if epoch := edge.Gateway.projection.Snapshot().ClusterEpoch; epoch == 0 || epoch == firstEpoch {
		t.Fatalf("reconnect did not atomically replace the cluster epoch: first=%d second=%d", firstEpoch, epoch)
	}
}

func waitForReadyEdgeClient(t *testing.T, edge *EdgeRuntime, timeout time.Duration) *NodeClient {
	t.Helper()
	var client *NodeClient
	waitForCondition(t, timeout, func() bool {
		client = edge.CurrentClient()
		link := edge.Gateway.currentControl(true)
		return client != nil && link != nil && link.client == client
	}, "edge control did not become ready")
	return client
}

func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(message)
}
