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

func TestCenterRestartReconfirmsActiveEdgeSessionsBeforeReady(t *testing.T) {
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
	validate := func(nodeID, token string) bool { return nodeID == "edge-confirm" && token == "token" }
	center, err := StartCenterRuntime(CenterRuntimeConfig{ControlListen: controlAddr, TLSConfig: serverTLS, ValidateToken: validate})
	if err != nil {
		t.Fatal(err)
	}
	edge, err := StartEdgeRuntime(EdgeRuntimeConfig{
		NodeID: "edge-confirm", Token: "token", CenterControl: controlAddr, Listen: "127.0.0.1:0",
		TLSConfig:      &tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS13},
		ConnectTimeout: 2 * time.Second, ReconnectMin: 20 * time.Millisecond, ReconnectMax: 100 * time.Millisecond,
	})
	if err != nil {
		center.Close()
		t.Fatal(err)
	}
	defer edge.Close()
	first := waitForReadyEdgeClient(t, edge, 3*time.Second)
	firstEpoch := center.Cluster.Epoch()
	oldGrant := DeviceGrant{
		DeviceID: 7, OwnerID: 5, Username: "alice", CallSign: "BG5ABC", SSID: 1,
		GroupID: 10, DomainID: 99, ExpiresAtMillis: time.Now().Add(2 * time.Minute).UnixMilli(),
	}
	if err := center.Gateway.activateDeviceSession(first.Session, &oldGrant); err != nil {
		center.Close()
		t.Fatal(err)
	}
	edge.Gateway.mu.Lock()
	edge.Gateway.sessions[oldGrant.SessionID] = &edgeDeviceSession{
		Grant: oldGrant, ControlSessionID: first.Session.SessionID,
		Addr: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32001}, RealAddr: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32001}, LastSeen: time.Now(),
	}
	edge.Gateway.byIdentity["alice-1"] = oldGrant.SessionID
	edge.Gateway.mu.Unlock()
	center.Close()
	waitForCondition(t, 3*time.Second, func() bool { return edge.Gateway.currentControl(false) == nil }, "edge did not detach from the first center")

	confirmStarted := make(chan []DeviceSessionConfirmItem, 1)
	releaseConfirm := make(chan struct{})
	center, err = StartCenterRuntime(CenterRuntimeConfig{
		ControlListen: controlAddr, TLSConfig: serverTLS, ValidateToken: validate,
		Confirm: func(_ *NodeSession, items []DeviceSessionConfirmItem) ([]DeviceSessionConfirmResult, error) {
			confirmStarted <- append([]DeviceSessionConfirmItem(nil), items...)
			<-releaseConfirm
			results := make([]DeviceSessionConfirmResult, 0, len(items))
			for _, item := range items {
				grant := oldGrant
				grant.SessionID, grant.SessionEpoch = 0, 0
				grant.ExpiresAtMillis = time.Now().Add(2 * time.Minute).UnixMilli()
				results = append(results, DeviceSessionConfirmResult{SessionID: item.SessionID, SessionEpoch: item.SessionEpoch, Success: true, Grant: &grant})
			}
			return results, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer center.Close()
	var confirmation []DeviceSessionConfirmItem
	select {
	case confirmation = <-confirmStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("edge did not request active session confirmation")
	}
	if len(confirmation) != 1 || confirmation[0].SessionID != oldGrant.SessionID || confirmation[0].ControlSessionID != first.Session.SessionID {
		t.Fatalf("unexpected confirmation batch: %#v", confirmation)
	}
	if edge.Gateway.allowExistingLocal(time.Now()) {
		t.Fatal("edge allowed device traffic before the new center confirmed the session")
	}
	time.Sleep(1200 * time.Millisecond)
	if edge.Gateway.ConnectionCount() != 1 || !edge.Gateway.preserveSessionsDuringRecovery() {
		t.Fatal("edge discarded the active session while confirmation was in progress")
	}
	close(releaseConfirm)
	second := waitForReadyEdgeClient(t, edge, 3*time.Second)
	if center.Cluster.Epoch() == firstEpoch || second.Session.SessionID == first.Session.SessionID {
		t.Fatal("center restart did not establish a new cluster and control session")
	}
	edge.Gateway.mu.RLock()
	var restored *edgeDeviceSession
	for _, session := range edge.Gateway.sessions {
		restored = session
	}
	_, oldStillPresent := edge.Gateway.sessions[oldGrant.SessionID]
	edge.Gateway.mu.RUnlock()
	if restored == nil || oldStillPresent || restored.Grant.SessionID == oldGrant.SessionID || restored.ControlSessionID != second.Session.SessionID {
		t.Fatalf("edge session was not atomically replaced: restored=%#v old_present=%v", restored, oldStillPresent)
	}
	if route, ok := center.Cluster.ResolveRoute(restored.Grant.SessionID); !ok || route.SessionEpoch != restored.Grant.SessionEpoch || route.DomainID != oldGrant.DomainID {
		t.Fatalf("center did not restore the confirmed route: %#v, %v", route, ok)
	}
	// A resync snapshot may arrive while the confirmation response is still in
	// flight. It must not prune the old local session before the response can
	// atomically replace its session ID.
	time.Sleep(1200 * time.Millisecond)
	if edge.Gateway.ConnectionCount() != 1 {
		t.Fatal("edge discarded the confirmed session after applying the restart snapshot")
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
