package interconnect

import (
	"errors"
	"testing"
	"time"

	"draarl/internal/protocol"
)

func TestDeviceSessionMigrationReplacesOwnerAndAdvancesEpoch(t *testing.T) {
	cluster := NewClusterManager(41)
	defer cluster.Close()
	gateway := NewCenterGateway(cluster, nil)
	edgeA := &NodeSession{NodeID: "edge-a", SessionID: 101}
	edgeB := &NodeSession{NodeID: "edge-b", SessionID: 202}
	cluster.OnConnect(edgeA)
	cluster.OnConnect(edgeB)

	first := &DeviceGrant{DeviceID: 7, OwnerID: 5, Username: "alice", SSID: 1, GroupID: 10, DomainID: 99}
	if err := gateway.activateDeviceSession(edgeA, first); err != nil {
		t.Fatal(err)
	}
	if first.SessionID == 0 || first.SessionEpoch != 1 {
		t.Fatalf("unexpected first session: %#v", first)
	}
	if route, ok := cluster.ResolveRoute(first.SessionID); !ok || route.SessionEpoch != 1 {
		t.Fatalf("first route was not installed: %#v, %v", route, ok)
	}

	second := &DeviceGrant{DeviceID: 7, OwnerID: 5, Username: "alice", SSID: 1, GroupID: 20, DomainID: 100}
	if err := gateway.activateDeviceSession(edgeB, second); err != nil {
		t.Fatal(err)
	}
	if second.SessionID == first.SessionID || second.SessionEpoch != 2 {
		t.Fatalf("migration did not allocate a new owner epoch: first=%#v second=%#v", first, second)
	}
	if _, ok := cluster.ResolveRoute(first.SessionID); ok {
		t.Fatal("old route remained authoritative after migration")
	}
	if route, ok := cluster.ResolveRoute(second.SessionID); !ok || route.SessionEpoch != 2 || route.GroupID != 20 {
		t.Fatalf("new route was not authoritative: %#v, %v", route, ok)
	}

	gateway.mu.RLock()
	_, oldAccepted := gateway.deviceSessions[first.SessionID]
	newOwner := gateway.deviceSessions[second.SessionID]
	gateway.mu.RUnlock()
	if oldAccepted {
		t.Fatal("old upstream session remained accepted by the centre")
	}
	if newOwner.NodeID != "edge-b" || newOwner.SessionEpoch != 2 {
		t.Fatalf("unexpected active owner: %#v", newOwner)
	}
}

func TestDeviceRoamsBetweenCenterAndEdgeWithMonotonicEpoch(t *testing.T) {
	cluster := NewClusterManager(48)
	defer cluster.Close()
	gateway := NewCenterGateway(cluster, nil)
	edge := &NodeSession{NodeID: "edge-a", SessionID: 101}
	gateway.OnConnect(edge)
	var revoked []deviceSessionOwner
	gateway.SetLocalRevocationHandler(func(deviceID, ownerID int, ssid byte, sessionID, epoch uint64) {
		revoked = append(revoked, deviceSessionOwner{DeviceID: deviceID, OwnerID: ownerID, SSID: ssid, SessionID: sessionID, SessionEpoch: epoch, NodeID: CenterLocalNodeID})
	})

	local := &DeviceGrant{DeviceID: 7, OwnerID: 5, Username: "alice", CallSign: "BG5ABC", SSID: 1, GroupID: 10, DomainID: 99}
	if err := gateway.ActivateLocalDevice(local); err != nil {
		t.Fatal(err)
	}
	if local.SessionID == 0 || local.SessionEpoch != 1 || !gateway.AuthorizeLocalDevice(*local) {
		t.Fatalf("unexpected local owner: %#v", local)
	}
	if targets := cluster.TargetNodes(99, "edge-a"); len(targets) != 0 {
		t.Fatalf("local owner appeared as edge target: %v", targets)
	}

	remote := &DeviceGrant{DeviceID: 7, OwnerID: 5, Username: "alice", CallSign: "BG5ABC", SSID: 1, GroupID: 10, DomainID: 99}
	if err := gateway.activateDeviceSession(edge, remote); err != nil {
		t.Fatal(err)
	}
	if remote.SessionEpoch != 2 || gateway.AuthorizeLocalDevice(*local) {
		t.Fatalf("edge migration did not revoke local owner: local=%#v remote=%#v", local, remote)
	}
	if len(revoked) != 1 || revoked[0].SessionID != local.SessionID || revoked[0].SessionEpoch != local.SessionEpoch {
		t.Fatalf("local revoke callbacks=%#v", revoked)
	}
	if _, ok := cluster.ResolveRoute(local.SessionID); ok {
		t.Fatal("local route survived migration to edge")
	}

	returned := &DeviceGrant{DeviceID: 7, OwnerID: 5, Username: "alice", CallSign: "BG5ABC", SSID: 1, GroupID: 20, DomainID: 100}
	if err := gateway.ActivateLocalDevice(returned); err != nil {
		t.Fatal(err)
	}
	if returned.SessionEpoch != 3 || returned.SessionID == local.SessionID || returned.SessionID == remote.SessionID {
		t.Fatalf("return to centre did not allocate a new epoch: %#v", returned)
	}
	if _, ok := cluster.ResolveRoute(remote.SessionID); ok {
		t.Fatal("edge route survived migration to centre")
	}
	if !gateway.AuthorizeLocalDevice(*returned) || gateway.RevokeLocalDevice(local.SessionID, local.SessionEpoch) {
		t.Fatal("stale local revoke affected the current centre owner")
	}
}

func TestLocalRelayValidatesAuthorityPolicyAndInnerPacket(t *testing.T) {
	cluster := NewClusterManager(49)
	defer cluster.Close()
	gateway := NewCenterGateway(cluster, nil)
	grant := &DeviceGrant{DeviceID: 7, OwnerID: 5, Username: "alice", CallSign: "BG5ABC", SSID: 1, GroupID: 10, DomainID: 99}
	if err := gateway.ActivateLocalDevice(grant); err != nil {
		t.Fatal(err)
	}
	valid := protocol.EncodeDraARLv1("alice", "", 1, protocol.DraARLTypeOpus16K, protocol.DraARLDevModelESP32NoRadio, 0, "BG5ABC", []byte{1})
	if !gateway.AcquireLocalVoice(*grant) {
		t.Fatal("valid local speaker did not acquire the domain")
	}
	if err := gateway.RelayLocalDevice(*grant, valid); err != nil {
		t.Fatalf("valid local relay failed: %v", err)
	}
	for name, wire := range map[string][]byte{
		"credential": protocol.EncodeDraARLv1("alice", "secret", 1, protocol.DraARLTypeOpus16K, 0, 0, "BG5ABC", []byte{1}),
		"config":     protocol.EncodeDraARLv1("alice", "", 1, protocol.DraARLTypeConfig, 0, 0, "BG5ABC", []byte{1}),
		"identity":   protocol.EncodeDraARLv1("mallory", "", 1, protocol.DraARLTypeOpus16K, 0, 0, "BG5ABC", []byte{1}),
	} {
		if err := gateway.RelayLocalDevice(*grant, wire); err == nil {
			t.Fatalf("%s local relay was accepted", name)
		}
	}
	updated, err := gateway.UpdateActiveDeviceRoute(grant.DeviceID, grant.GroupID, grant.DomainID, true, false)
	if err != nil || !updated {
		t.Fatalf("disable send update failed: updated=%v err=%v", updated, err)
	}
	if err := gateway.RelayLocalDevice(*grant, valid); err == nil {
		t.Fatal("disabled local sender was accepted")
	}
}

func TestEdgeSessionRevokeCannotRemoveNewerSession(t *testing.T) {
	gateway := &EdgeGateway{
		sessions:   make(map[uint64]*edgeDeviceSession),
		byIdentity: make(map[string]uint64),
	}
	oldGrant := DeviceGrant{SessionID: 11, SessionEpoch: 1, DeviceID: 7, Username: "alice", SSID: 1}
	newGrant := DeviceGrant{SessionID: 22, SessionEpoch: 2, DeviceID: 7, Username: "alice", SSID: 1}
	gateway.sessions[oldGrant.SessionID] = &edgeDeviceSession{Grant: oldGrant}
	gateway.sessions[newGrant.SessionID] = &edgeDeviceSession{Grant: newGrant}
	gateway.byIdentity["alice-1"] = newGrant.SessionID

	if !gateway.revokeSession(DeviceSessionRevoke{SessionID: oldGrant.SessionID, SessionEpoch: oldGrant.SessionEpoch}) {
		t.Fatal("matching old session revoke was ignored")
	}
	if gateway.sessions[newGrant.SessionID] == nil || gateway.byIdentity["alice-1"] != newGrant.SessionID {
		t.Fatal("old revoke removed the newer session or identity mapping")
	}
	if gateway.revokeSession(DeviceSessionRevoke{SessionID: newGrant.SessionID, SessionEpoch: oldGrant.SessionEpoch}) {
		t.Fatal("stale epoch revoked a newer session")
	}
}

func TestActivationFailureKeepsExistingOwner(t *testing.T) {
	cluster := NewClusterManager(42)
	defer cluster.Close()
	fail := false
	gateway := NewCenterGateway(cluster, nil, func(_ *NodeSession, _ *DeviceGrant) error {
		if fail {
			return errors.New("persist failed")
		}
		return nil
	})
	edgeA := &NodeSession{NodeID: "edge-a", SessionID: 101}
	edgeB := &NodeSession{NodeID: "edge-b", SessionID: 202}
	cluster.OnConnect(edgeA)
	cluster.OnConnect(edgeB)
	first := &DeviceGrant{DeviceID: 7, OwnerID: 5, Username: "alice", SSID: 1, DomainID: 9}
	if err := gateway.activateDeviceSession(edgeA, first); err != nil {
		t.Fatal(err)
	}
	fail = true
	second := &DeviceGrant{DeviceID: 7, OwnerID: 5, Username: "alice", SSID: 1, DomainID: 10}
	if err := gateway.activateDeviceSession(edgeB, second); err == nil {
		t.Fatal("expected activation callback failure")
	}
	if _, ok := cluster.ResolveRoute(first.SessionID); !ok {
		t.Fatal("failed activation removed the existing route")
	}
	gateway.mu.RLock()
	owner := gateway.deviceSessions[first.SessionID]
	_, secondInstalled := gateway.deviceSessions[second.SessionID]
	gateway.mu.RUnlock()
	if owner.NodeID != "edge-a" || secondInstalled {
		t.Fatalf("failed activation changed owner: old=%#v second=%v", owner, secondInstalled)
	}
}

func TestLateOldEdgeDisconnectCannotClearMigratedSession(t *testing.T) {
	cluster := NewClusterManager(43)
	defer cluster.Close()
	gateway := NewCenterGateway(cluster, nil)
	edgeA := &NodeSession{NodeID: "edge-a", SessionID: 101}
	edgeB := &NodeSession{NodeID: "edge-b", SessionID: 202}
	gateway.OnConnect(edgeA)
	gateway.OnConnect(edgeB)

	oldGrant := &DeviceGrant{DeviceID: 7, OwnerID: 5, Username: "alice", SSID: 1, GroupID: 10, DomainID: 99}
	if err := gateway.activateDeviceSession(edgeA, oldGrant); err != nil {
		t.Fatal(err)
	}
	newGrant := &DeviceGrant{DeviceID: 7, OwnerID: 5, Username: "alice", SSID: 1, GroupID: 20, DomainID: 100}
	if err := gateway.activateDeviceSession(edgeB, newGrant); err != nil {
		t.Fatal(err)
	}

	gateway.OnDisconnect(edgeA, errors.New("late disconnect"))
	if route, ok := cluster.ResolveRoute(newGrant.SessionID); !ok || route.SessionEpoch != newGrant.SessionEpoch || route.GroupID != 20 {
		t.Fatalf("late old-edge disconnect changed the new route: %#v, %v", route, ok)
	}
	gateway.mu.RLock()
	activeSessionID := gateway.activeDevices[deviceGrantIdentity(newGrant)]
	activeOwner := gateway.deviceSessions[activeSessionID]
	gateway.mu.RUnlock()
	if activeSessionID != newGrant.SessionID || activeOwner.NodeID != edgeB.NodeID {
		t.Fatalf("late old-edge disconnect changed the active owner: session=%d owner=%#v", activeSessionID, activeOwner)
	}

	gateway.OnDisconnect(edgeB, errors.New("current disconnect"))
	if _, ok := cluster.ResolveRoute(newGrant.SessionID); ok {
		t.Fatal("current edge disconnect left its route active")
	}
	gateway.mu.RLock()
	_, ownerStillActive := gateway.activeDevices[deviceGrantIdentity(newGrant)]
	gateway.mu.RUnlock()
	if ownerStillActive {
		t.Fatal("current edge disconnect left its device owner active")
	}
}

func TestLateControlDisconnectCannotClearReconnectedNodeSession(t *testing.T) {
	cluster := NewClusterManager(44)
	defer cluster.Close()
	gateway := NewCenterGateway(cluster, nil)
	oldControl := &NodeSession{NodeID: "edge-a", SessionID: 101}
	newControl := &NodeSession{NodeID: "edge-a", SessionID: 202}
	gateway.OnConnect(oldControl)

	oldGrant := &DeviceGrant{DeviceID: 7, OwnerID: 5, Username: "alice", SSID: 1, GroupID: 10, DomainID: 99}
	if err := gateway.activateDeviceSession(oldControl, oldGrant); err != nil {
		t.Fatal(err)
	}
	gateway.OnConnect(newControl)
	if _, ok := cluster.ResolveRoute(oldGrant.SessionID); ok {
		t.Fatal("node reconnect kept a route owned by the replaced control session")
	}

	newGrant := &DeviceGrant{DeviceID: 7, OwnerID: 5, Username: "alice", SSID: 1, GroupID: 20, DomainID: 100}
	if err := gateway.activateDeviceSession(newControl, newGrant); err != nil {
		t.Fatal(err)
	}
	if newGrant.SessionEpoch != oldGrant.SessionEpoch+1 {
		t.Fatalf("node reconnect did not advance device epoch: old=%d new=%d", oldGrant.SessionEpoch, newGrant.SessionEpoch)
	}

	gateway.OnDisconnect(oldControl, errors.New("stale control disconnect"))
	if route, ok := cluster.ResolveRoute(newGrant.SessionID); !ok || route.SessionEpoch != newGrant.SessionEpoch {
		t.Fatalf("stale control disconnect changed the replacement route: %#v, %v", route, ok)
	}
	gateway.mu.RLock()
	owner := gateway.deviceSessions[newGrant.SessionID]
	gateway.mu.RUnlock()
	if owner.ControlSessionID != newControl.SessionID {
		t.Fatalf("stale control disconnect changed the replacement owner: %#v", owner)
	}
}

func TestActiveRoutePolicyAndGroupChangesUpdateTargets(t *testing.T) {
	cluster := NewClusterManager(45)
	defer cluster.Close()
	gateway := NewCenterGateway(cluster, nil)
	edgeA := &NodeSession{NodeID: "edge-a", SessionID: 101}
	edgeB := &NodeSession{NodeID: "edge-b", SessionID: 202}
	gateway.OnConnect(edgeA)
	gateway.OnConnect(edgeB)

	source := &DeviceGrant{DeviceID: 7, OwnerID: 5, Username: "alice", SSID: 1, GroupID: 10, DomainID: 99}
	receiver := &DeviceGrant{DeviceID: 8, OwnerID: 6, Username: "bob", SSID: 1, GroupID: 10, DomainID: 99}
	if err := gateway.activateDeviceSession(edgeA, source); err != nil {
		t.Fatal(err)
	}
	if err := gateway.activateDeviceSession(edgeB, receiver); err != nil {
		t.Fatal(err)
	}
	assertTargets := func(domainID uint64, want ...string) {
		t.Helper()
		got := cluster.TargetNodes(domainID, edgeA.NodeID)
		if len(got) != len(want) {
			t.Fatalf("domain %d targets=%v want=%v", domainID, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("domain %d targets=%v want=%v", domainID, got, want)
			}
		}
	}
	assertTargets(99, edgeB.NodeID)

	updated, err := gateway.UpdateActiveDeviceRoute(receiver.DeviceID, 10, 99, false, true)
	if err != nil || !updated {
		t.Fatalf("disable receive update failed: updated=%v err=%v", updated, err)
	}
	assertTargets(99)

	updated, err = gateway.UpdateActiveDeviceRoute(receiver.DeviceID, 10, 99, false, false)
	if err != nil || !updated {
		t.Fatalf("resume receive update failed: updated=%v err=%v", updated, err)
	}
	assertTargets(99, edgeB.NodeID)

	updated, err = gateway.UpdateActiveDeviceRoute(source.DeviceID, 10, 99, true, false)
	if err != nil || !updated {
		t.Fatalf("disable send update failed: updated=%v err=%v", updated, err)
	}
	if route, ok := cluster.ResolveRoute(source.SessionID); !ok || !route.DisableSend {
		t.Fatalf("disable send did not reach the authoritative route: %#v, %v", route, ok)
	}

	updated, err = gateway.UpdateActiveDeviceRoute(receiver.DeviceID, 20, 100, false, false)
	if err != nil || !updated {
		t.Fatalf("group change failed: updated=%v err=%v", updated, err)
	}
	assertTargets(99)
	if targets := cluster.TargetNodes(100, ""); len(targets) != 1 || targets[0] != edgeB.NodeID {
		t.Fatalf("new domain targets=%v want=[%s]", targets, edgeB.NodeID)
	}
}

func TestRefreshActiveDomainsAndIdentityRoute(t *testing.T) {
	cluster := NewClusterManager(46)
	defer cluster.Close()
	gateway := NewCenterGateway(cluster, nil)
	edgeA := &NodeSession{NodeID: "edge-a", SessionID: 101}
	edgeB := &NodeSession{NodeID: "edge-b", SessionID: 202}
	gateway.OnConnect(edgeA)
	gateway.OnConnect(edgeB)

	physical := &DeviceGrant{DeviceID: 7, OwnerID: 5, Username: "alice", SSID: 1, GroupID: 10, DomainID: 90}
	ghost := &DeviceGrant{OwnerID: 6, Username: "bob", SSID: 101, GroupID: 20, DomainID: 91}
	if err := gateway.activateDeviceSession(edgeA, physical); err != nil {
		t.Fatal(err)
	}
	if err := gateway.activateDeviceSession(edgeB, ghost); err != nil {
		t.Fatal(err)
	}

	updated, err := gateway.UpdateActiveIdentityRoute(ghost.OwnerID, ghost.SSID, 21, 92, true, true)
	if err != nil || !updated {
		t.Fatalf("identity route update failed: updated=%v err=%v", updated, err)
	}
	if route, ok := cluster.ResolveRoute(ghost.SessionID); !ok || route.GroupID != 21 || route.DomainID != 92 || !route.DisableSend || !route.DisableRecv {
		t.Fatalf("identity route was not updated: %#v, %v", route, ok)
	}

	if err := gateway.RefreshActiveDeviceDomains(func(groupID int) uint64 {
		switch groupID {
		case 10:
			return 200
		case 21:
			return 0
		default:
			return 0
		}
	}); err != nil {
		t.Fatal(err)
	}
	if route, ok := cluster.ResolveRoute(physical.SessionID); !ok || route.DomainID != 200 {
		t.Fatalf("physical topology refresh failed: %#v, %v", route, ok)
	}
	if route, ok := cluster.ResolveRoute(ghost.SessionID); !ok || route.DomainID != 0 {
		t.Fatalf("disabled/missing group did not leave the forwarding domain: %#v, %v", route, ok)
	}
}

func TestDeviceAndOwnerRevocationRemoveOnlyCurrentSessions(t *testing.T) {
	cluster := NewClusterManager(47)
	defer cluster.Close()
	gateway := NewCenterGateway(cluster, nil)
	edge := &NodeSession{NodeID: "edge-a", SessionID: 101}
	gateway.OnConnect(edge)

	physical := &DeviceGrant{DeviceID: 7, OwnerID: 5, Username: "alice", SSID: 1, GroupID: 10, DomainID: 90}
	ghost := &DeviceGrant{OwnerID: 5, Username: "alice", SSID: 101, GroupID: 10, DomainID: 90}
	other := &DeviceGrant{DeviceID: 8, OwnerID: 6, Username: "bob", SSID: 1, GroupID: 10, DomainID: 90}
	for _, grant := range []*DeviceGrant{physical, ghost, other} {
		if err := gateway.activateDeviceSession(edge, grant); err != nil {
			t.Fatal(err)
		}
	}

	revoked, err := gateway.RevokeActiveDevice(physical.DeviceID, "device_deleted")
	if err != nil || !revoked {
		t.Fatalf("device revoke failed: revoked=%v err=%v", revoked, err)
	}
	if _, ok := cluster.ResolveRoute(physical.SessionID); ok {
		t.Fatal("device revoke left the physical route active")
	}
	if _, ok := cluster.ResolveRoute(ghost.SessionID); !ok {
		t.Fatal("device revoke removed another session of the same owner")
	}

	count, err := gateway.RevokeActiveOwner(physical.OwnerID, "user_disabled")
	if err != nil || count != 1 {
		t.Fatalf("owner revoke count=%d err=%v", count, err)
	}
	if _, ok := cluster.ResolveRoute(ghost.SessionID); ok {
		t.Fatal("owner revoke left the ghost route active")
	}
	if _, ok := cluster.ResolveRoute(other.SessionID); !ok {
		t.Fatal("owner revoke removed another user's route")
	}
}

func TestDeviceRevocationCallbackIsBoundToCurrentEntrySession(t *testing.T) {
	cluster := NewClusterManager(51)
	defer cluster.Close()
	gateway := NewCenterGateway(cluster, nil)
	edge := &NodeSession{NodeID: "edge-a", SessionID: 101}
	gateway.OnConnect(edge)
	type revokedEntry struct {
		nodeID    string
		controlID uint64
		deviceID  int
		reason    string
	}
	var callbacks []revokedEntry
	gateway.SetDeviceRevocationHandler(func(nodeID string, controlSessionID uint64, deviceID int, reason string) {
		callbacks = append(callbacks, revokedEntry{nodeID: nodeID, controlID: controlSessionID, deviceID: deviceID, reason: reason})
	})
	grant := &DeviceGrant{DeviceID: 7, OwnerID: 5, Username: "alice", SSID: 1, DomainID: 9}
	if err := gateway.activateDeviceSession(edge, grant); err != nil {
		t.Fatal(err)
	}
	revoked, err := gateway.RevokeActiveDevice(grant.DeviceID, "user_disabled")
	if err != nil || !revoked {
		t.Fatalf("revoke failed: revoked=%v err=%v", revoked, err)
	}
	if len(callbacks) != 1 || callbacks[0] != (revokedEntry{nodeID: edge.NodeID, controlID: edge.SessionID, deviceID: grant.DeviceID, reason: "user_disabled"}) {
		t.Fatalf("revocation callbacks=%#v", callbacks)
	}
}

func TestSameEdgeGrantRenewalKeepsSessionAndEpoch(t *testing.T) {
	cluster := NewClusterManager(52)
	defer cluster.Close()
	gateway := NewCenterGateway(cluster, nil)
	edge := &NodeSession{NodeID: "edge-a", SessionID: 101}
	gateway.OnConnect(edge)
	first := &DeviceGrant{DeviceID: 7, OwnerID: 5, Username: "alice", SSID: 1, GroupID: 10, DomainID: 90}
	if err := gateway.activateDeviceSession(edge, first); err != nil {
		t.Fatal(err)
	}
	renewed := &DeviceGrant{DeviceID: 7, OwnerID: 5, Username: "alice", SSID: 1, GroupID: 20, DomainID: 91}
	if err := gateway.activateDeviceSession(edge, renewed); err != nil {
		t.Fatal(err)
	}
	if renewed.SessionID != first.SessionID || renewed.SessionEpoch != first.SessionEpoch {
		t.Fatalf("renewal churned owner: first=%#v renewed=%#v", first, renewed)
	}
	if route, ok := cluster.ResolveRoute(first.SessionID); !ok || route.GroupID != 20 || route.DomainID != 91 {
		t.Fatalf("renewal did not refresh route: %#v, %v", route, ok)
	}
}

func TestOfflineSessionReportRejectsStaleOwnerAndRemovesCurrent(t *testing.T) {
	cluster := NewClusterManager(53)
	defer cluster.Close()
	gateway := NewCenterGateway(cluster, nil)
	edgeA := &NodeSession{NodeID: "edge-a", SessionID: 101}
	edgeB := &NodeSession{NodeID: "edge-b", SessionID: 202}
	gateway.OnConnect(edgeA)
	gateway.OnConnect(edgeB)
	first := &DeviceGrant{DeviceID: 7, OwnerID: 5, Username: "alice", SSID: 1, DomainID: 90}
	if err := gateway.activateDeviceSession(edgeA, first); err != nil {
		t.Fatal(err)
	}
	if gateway.handleDeviceSessionReport(edgeA, DeviceSessionReport{SessionID: first.SessionID, SessionEpoch: first.SessionEpoch + 1, DeviceID: first.DeviceID, Reason: "device_timeout"}) {
		t.Fatal("wrong epoch offline report was accepted")
	}
	second := &DeviceGrant{DeviceID: 7, OwnerID: 5, Username: "alice", SSID: 1, DomainID: 91}
	if err := gateway.activateDeviceSession(edgeB, second); err != nil {
		t.Fatal(err)
	}
	if gateway.handleDeviceSessionReport(edgeA, DeviceSessionReport{SessionID: first.SessionID, SessionEpoch: first.SessionEpoch, DeviceID: first.DeviceID, Reason: "late_timeout"}) {
		t.Fatal("late old-edge offline report was accepted")
	}
	if route, ok := cluster.ResolveRoute(second.SessionID); !ok || route.SessionEpoch != second.SessionEpoch {
		t.Fatalf("late report changed current route: %#v, %v", route, ok)
	}
	if !gateway.handleDeviceSessionReport(edgeB, DeviceSessionReport{SessionID: second.SessionID, SessionEpoch: second.SessionEpoch, DeviceID: second.DeviceID, Reason: "device_timeout"}) {
		t.Fatal("current offline report was rejected")
	}
	if _, ok := cluster.ResolveRoute(second.SessionID); ok {
		t.Fatal("current offline report left route active")
	}
}

func TestCenterSessionRenewalRequiresCurrentOwner(t *testing.T) {
	now := time.Now()
	cluster := NewClusterManager(54)
	defer cluster.Close()
	gateway := NewCenterGateway(cluster, nil)
	edgeA := &NodeSession{NodeID: "edge-a", SessionID: 101}
	edgeB := &NodeSession{NodeID: "edge-b", SessionID: 202}
	gateway.OnConnect(edgeA)
	gateway.OnConnect(edgeB)
	grant := &DeviceGrant{DeviceID: 7, OwnerID: 5, Username: "alice", SSID: 1, DomainID: 90}
	if err := gateway.activateDeviceSession(edgeA, grant); err != nil {
		t.Fatal(err)
	}
	request := DeviceSessionRenewRequest{RequestID: 1, SessionID: grant.SessionID, SessionEpoch: grant.SessionEpoch}
	if response := gateway.renewDeviceSession(edgeB, request, now); response.Success || response.Error != "session_not_owned" {
		t.Fatalf("foreign renewal response=%#v", response)
	}
	response := gateway.renewDeviceSession(edgeA, request, now)
	if !response.Success || response.ExpiresAtMillis != now.Add(defaultDeviceGrantTTL).UnixMilli() {
		t.Fatalf("owner renewal response=%#v", response)
	}
}

func TestDeviceConfigPendingDeliveryFailsImmediatelyOnMigration(t *testing.T) {
	cluster := NewClusterManager(55)
	defer cluster.Close()
	gateway := NewCenterGateway(cluster, nil)
	edgeA := &NodeSession{NodeID: "edge-a", SessionID: 101}
	edgeB := &NodeSession{NodeID: "edge-b", SessionID: 202}
	gateway.OnConnect(edgeA)
	gateway.OnConnect(edgeB)
	first := &DeviceGrant{DeviceID: 7, OwnerID: 5, Username: "alice", CallSign: "BG5CFG", SSID: 1, DomainID: 90}
	if err := gateway.activateDeviceSession(edgeA, first); err != nil {
		t.Fatal(err)
	}
	gateway.mu.RLock()
	owner := gateway.deviceSessions[first.SessionID]
	gateway.mu.RUnlock()
	result := make(chan error, 1)
	gateway.configMu.Lock()
	gateway.configPending[999] = &pendingDeviceConfigDelivery{owner: owner, envelope: Envelope{MessageID: 999}, result: result}
	gateway.configMu.Unlock()
	second := &DeviceGrant{DeviceID: 7, OwnerID: 5, Username: "alice", CallSign: "BG5CFG", SSID: 1, DomainID: 91}
	if err := gateway.activateDeviceSession(edgeB, second); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("migration completed a stale config delivery successfully")
		}
	case <-time.After(time.Second):
		t.Fatal("migration did not fail the stale config delivery")
	}
	gateway.configMu.Lock()
	_, pending := gateway.configPending[999]
	gateway.configMu.Unlock()
	if pending {
		t.Fatal("migration left a stale config delivery pending")
	}
}

func TestCenterGatewayLimitsDeviceSessionsPerNodeControlSession(t *testing.T) {
	cluster := NewClusterManager(56)
	defer cluster.Close()
	gateway := NewCenterGateway(cluster, nil)
	if err := gateway.SetResourceLimits(ResourceLimits{MaxDeviceSessionsPerNode: 1}); err != nil {
		t.Fatal(err)
	}
	edge := &NodeSession{NodeID: "edge-a", SessionID: 101}
	gateway.OnConnect(edge)
	first := &DeviceGrant{DeviceID: 7, OwnerID: 5, Username: "alice", SSID: 1, DomainID: 90}
	if err := gateway.activateDeviceSession(edge, first); err != nil {
		t.Fatal(err)
	}
	renewed := &DeviceGrant{DeviceID: 7, OwnerID: 5, Username: "alice", SSID: 1, DomainID: 91}
	if err := gateway.activateDeviceSession(edge, renewed); err != nil {
		t.Fatalf("idempotent renewal was counted as a new session: %v", err)
	}
	second := &DeviceGrant{DeviceID: 8, OwnerID: 6, Username: "bob", SSID: 1, DomainID: 90}
	if err := gateway.activateDeviceSession(edge, second); err == nil {
		t.Fatal("second device exceeded the node session limit")
	}
	if got := edge.ProtectionSnapshot().SessionLimitRejects; got != 1 {
		t.Fatalf("session limit rejects=%d", got)
	}
	if !gateway.handleDeviceSessionReport(edge, DeviceSessionReport{SessionID: first.SessionID, SessionEpoch: first.SessionEpoch, DeviceID: first.DeviceID, Reason: "device_timeout"}) {
		t.Fatal("first device was not removed")
	}
	if err := gateway.activateDeviceSession(edge, second); err != nil {
		t.Fatalf("released quota was not reusable: %v", err)
	}
}
