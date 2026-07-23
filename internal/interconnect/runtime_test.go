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

func TestOnlineCredentialRotationPersistsAtEdge(t *testing.T) {
	serverTLS, roots, err := NewSelfSignedTLSConfig("localhost")
	if err != nil {
		t.Fatal(err)
	}
	center, err := StartCenterRuntime(CenterRuntimeConfig{
		ControlListen: "127.0.0.1:0", TLSConfig: serverTLS,
		ValidateToken: func(nodeID, token string) bool { return nodeID == "edge-rotate" && token == "old-token" },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer center.Close()
	persisted := make(chan EdgeIdentity, 1)
	edge, err := StartEdgeRuntime(EdgeRuntimeConfig{
		NodeID: "edge-rotate", Token: "old-token", CenterControl: center.Control.Addr().String(), Listen: "127.0.0.1:0",
		TLSConfig:    &tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS13},
		OnCredential: func(identity EdgeIdentity) error { persisted <- identity; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer edge.Close()
	waitForReadyEdgeClient(t, edge, 3*time.Second)
	credential, err := NewLongTermCredential("edge-rotate")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := center.RotateNodeCredential("edge-rotate", credential, 2, time.Now().Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	select {
	case identity := <-persisted:
		if identity.NodeID != "edge-rotate" || identity.Credential != credential || identity.CredentialEpoch != 2 {
			t.Fatalf("persisted identity=%#v", identity)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("online credential rotation was not persisted by edge")
	}
	edge.mu.RLock()
	got := edge.cfg.Token
	edge.mu.RUnlock()
	if got != credential {
		t.Fatal("edge reconnect credential was not updated after persistence")
	}
}

func TestCredentialRotationAckIsBoundToNodeSessionAndEpoch(t *testing.T) {
	result := make(chan NodeCredentialControl, 1)
	runtime := &CenterRuntime{credentialPending: map[uint64]*pendingNodeCredentialRotation{
		9: {nodeID: "edge-target", sessionID: 42, credentialEpoch: 3, result: result},
	}}
	ack := NodeCredentialControl{Kind: NodeCredentialKindResult, CredentialEpoch: 3, AckForMessageID: 9, Success: true}

	for _, session := range []*NodeSession{
		{NodeID: "edge-other", SessionID: 42},
		{NodeID: "edge-target", SessionID: 41},
	} {
		runtime.finishCredentialRotation(session, ack)
		select {
		case <-result:
			t.Fatal("credential rotation accepted an acknowledgement from the wrong node session")
		default:
		}
	}
	wrongEpoch := ack
	wrongEpoch.CredentialEpoch = 2
	runtime.finishCredentialRotation(&NodeSession{NodeID: "edge-target", SessionID: 42}, wrongEpoch)
	select {
	case <-result:
		t.Fatal("credential rotation accepted an acknowledgement for the wrong credential epoch")
	default:
	}

	runtime.finishCredentialRotation(&NodeSession{NodeID: "edge-target", SessionID: 42}, ack)
	select {
	case got := <-result:
		if got.AckForMessageID != 9 || !got.Success {
			t.Fatalf("unexpected credential acknowledgement: %#v", got)
		}
	default:
		t.Fatal("credential rotation rejected the matching acknowledgement")
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
