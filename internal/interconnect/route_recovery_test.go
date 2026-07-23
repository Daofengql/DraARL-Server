package interconnect

import (
	"crypto/tls"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestRouteSynchronizationRecoversFromControlPlaneFaults(t *testing.T) {
	serverTLS, roots, err := NewSelfSignedTLSConfig("localhost")
	if err != nil {
		t.Fatal(err)
	}
	center, err := StartCenterRuntime(CenterRuntimeConfig{
		ControlListen: "127.0.0.1:0",
		TLSConfig:     serverTLS,
		ValidateToken: func(nodeID, token string) bool { return nodeID == "edge-fault" && token == "test-token" },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer center.Close()

	var dropNextAck atomic.Bool
	centerHandler := center.Control.cfg.OnEnvelope
	center.Control.cfg.OnEnvelope = func(session *NodeSession, env Envelope) {
		if env.Subtype == SubtypeRouteAck && dropNextAck.CompareAndSwap(true, false) {
			return
		}
		centerHandler(session, env)
	}

	edge, err := StartEdgeRuntime(EdgeRuntimeConfig{
		NodeID: "edge-fault", Token: "test-token", CenterControl: center.Control.Addr().String(), Listen: "127.0.0.1:0",
		TLSConfig: &tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS13},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer edge.Close()
	client := waitForReadyEdgeClient(t, edge, 3*time.Second)
	normalHandler := func(env Envelope) { edge.Gateway.onEnvelopeFrom(client, env) }
	waitForVersion := func(t *testing.T, version uint64, sessionIDs ...uint64) {
		t.Helper()
		waitForCondition(t, 6*time.Second, func() bool {
			projection := edge.Gateway.projection.Snapshot()
			if projection.Version != version || center.Cluster.PendingControl("edge-fault") != 0 || edge.Gateway.currentControl(true) == nil {
				return false
			}
			for _, sessionID := range sessionIDs {
				if _, ok := projection.Devices[sessionID]; !ok {
					return false
				}
			}
			return true
		}, "edge projection did not recover to the authoritative version")
	}
	setRoute := func(t *testing.T, sessionID uint64) {
		t.Helper()
		if err := center.Cluster.SetNodeRoute("edge-fault", DeviceRoute{SessionID: sessionID, SessionEpoch: 1, DeviceID: int(sessionID), DomainID: 9}); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("lost_delta_is_retried", func(t *testing.T) {
		var deliveries atomic.Int32
		client.SetEnvelopeHandler(func(env Envelope) {
			if env.Subtype == SubtypeRouteDelta {
				if deliveries.Add(1) == 1 {
					return
				}
				// The first delivery was dropped below the replay layer. Clear the
				// local duplicate marker to model a datagram/frame lost in transit.
				env.Duplicate = false
			}
			normalHandler(env)
		})
		setRoute(t, 101)
		waitForVersion(t, 1, 101)
		if deliveries.Load() < 2 {
			t.Fatal("lost delta was not retried")
		}
	})

	t.Run("lost_ack_replays_duplicate_delta", func(t *testing.T) {
		var duplicateSeen atomic.Bool
		client.SetEnvelopeHandler(func(env Envelope) {
			if env.Subtype == SubtypeRouteDelta && env.Duplicate {
				duplicateSeen.Store(true)
			}
			normalHandler(env)
		})
		dropNextAck.Store(true)
		setRoute(t, 102)
		waitForVersion(t, 2, 101, 102)
		if dropNextAck.Load() || !duplicateSeen.Load() {
			t.Fatal("lost route ACK did not cause an idempotent delta replay")
		}
	})

	t.Run("bad_delta_checksum_requests_snapshot", func(t *testing.T) {
		var corrupted atomic.Bool
		var snapshots atomic.Int32
		client.SetEnvelopeHandler(func(env Envelope) {
			if env.Subtype == SubtypeRouteDelta && corrupted.CompareAndSwap(false, true) {
				var delta RouteDelta
				if DecodeJSON(env.Payload, &delta) == nil && len(delta.Checksum) > 0 {
					replacement := byte('0')
					if delta.Checksum[0] == replacement {
						replacement = '1'
					}
					delta.Checksum = string(replacement) + delta.Checksum[1:]
					if payload, encodeErr := EncodeJSON(delta); encodeErr == nil {
						env.Payload = payload
					}
				}
			}
			if env.Subtype == SubtypeRouteSnapshotBegin {
				snapshots.Add(1)
			}
			normalHandler(env)
		})
		setRoute(t, 103)
		waitForVersion(t, 3, 101, 102, 103)
		if !corrupted.Load() || snapshots.Load() == 0 {
			t.Fatal("bad delta checksum did not trigger a full snapshot")
		}
	})

	t.Run("version_jump_requests_snapshot", func(t *testing.T) {
		var jumped atomic.Bool
		var snapshots atomic.Int32
		client.SetEnvelopeHandler(func(env Envelope) {
			if env.Subtype == SubtypeRouteDelta && jumped.CompareAndSwap(false, true) {
				var delta RouteDelta
				if DecodeJSON(env.Payload, &delta) == nil {
					delta = NewRouteDelta(delta.ClusterEpoch, delta.BaseVersion+10, delta.NewVersion+10, delta.Operations)
					if payload, encodeErr := EncodeJSON(delta); encodeErr == nil {
						env.Payload = payload
					}
				}
			}
			if env.Subtype == SubtypeRouteSnapshotBegin {
				snapshots.Add(1)
			}
			normalHandler(env)
		})
		setRoute(t, 104)
		waitForVersion(t, 4, 101, 102, 103, 104)
		if !jumped.Load() || snapshots.Load() == 0 {
			t.Fatal("route version jump did not trigger a full snapshot")
		}
	})

	t.Run("reordered_deltas_converge_atomically", func(t *testing.T) {
		var first *Envelope
		var reordered atomic.Bool
		var snapshots atomic.Int32
		client.SetEnvelopeHandler(func(env Envelope) {
			if env.Subtype == SubtypeRouteDelta && first == nil {
				copy := env
				first = &copy
				return
			}
			if env.Subtype == SubtypeRouteDelta && first != nil && !reordered.Load() {
				reordered.Store(true)
				normalHandler(env)
				normalHandler(*first)
				first = nil
				return
			}
			if env.Subtype == SubtypeRouteSnapshotBegin {
				snapshots.Add(1)
			}
			normalHandler(env)
		})
		setRoute(t, 105)
		setRoute(t, 106)
		waitForVersion(t, 6, 101, 102, 103, 104, 105, 106)
		if !reordered.Load() || snapshots.Load() == 0 {
			t.Fatal("reordered deltas did not converge through a full snapshot")
		}
	})

	t.Run("corrupt_snapshot_is_not_committed", func(t *testing.T) {
		var corrupted atomic.Bool
		var snapshotBegins atomic.Int32
		client.SetEnvelopeHandler(func(env Envelope) {
			if env.Subtype == SubtypeRouteSnapshotBegin {
				snapshotBegins.Add(1)
			}
			if env.Subtype == SubtypeRouteSnapshotChunk && corrupted.CompareAndSwap(false, true) {
				var chunk SnapshotChunk
				if DecodeJSON(env.Payload, &chunk) == nil && len(chunk.Data) > 0 {
					chunk.Data[0] ^= 0xff
					if payload, encodeErr := EncodeJSON(chunk); encodeErr == nil {
						env.Payload = payload
					}
				}
			}
			normalHandler(env)
		})
		if err := center.Cluster.SendFullProjection("edge-fault"); err != nil {
			t.Fatal(err)
		}
		waitForVersion(t, 6, 101, 102, 103, 104, 105, 106)
		if !corrupted.Load() || snapshotBegins.Load() < 2 {
			t.Fatal("corrupt snapshot was not rejected and requested again")
		}
	})

	client.SetEnvelopeHandler(normalHandler)
}

func TestRouteAckTimeoutClosesNodeSession(t *testing.T) {
	centerConn, edgeConn := net.Pipe()
	defer edgeConn.Close()
	session := &NodeSession{NodeID: "edge-timeout", SessionID: 9, conn: centerConn}
	manager := &ClusterManager{
		epoch: 7, nodes: map[string]*NodeSession{"edge-timeout": session},
		pendingControl: map[string]map[uint64]*pendingControl{
			"edge-timeout": {11: {version: 3, attempts: controlMaxAttempts, sentAt: time.Now().Add(-2 * controlRetryInterval)}},
		},
		syncError: make(map[string]string), closed: make(chan struct{}),
	}
	defer manager.Close()
	manager.retryDueControl(time.Now())
	if manager.PendingControl("edge-timeout") != 0 || manager.SyncError("edge-timeout") != "route_ack_timeout" {
		t.Fatal("route ACK timeout did not clear pending state and record the synchronization error")
	}
	_ = edgeConn.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := edgeConn.Read(make([]byte, 1)); err == nil {
		t.Fatal("route ACK timeout did not close the stale node session")
	}
}
