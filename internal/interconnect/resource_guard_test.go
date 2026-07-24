package interconnect

import (
	"net"
	"sync"
	"testing"
	"time"
)

func TestNodeProtectionSoftAndHardBudgets(t *testing.T) {
	limits := ResourceLimits{
		DataSoftPPSPerNode: 2, DataHardPPSPerNode: 3,
		ControlSoftPPSPerNode: 2, ControlHardPPSPerNode: 3,
		DeviceAuthPPSPerNode: 1, DataQueuePerNode: 1,
	}
	p := newNodeProtection(limits)
	now := time.Unix(100, 0)
	for i := 0; i < 3; i++ {
		if !p.allowData(100, now) || !p.allowControl(100, now) {
			t.Fatalf("packet %d below hard limit was rejected", i+1)
		}
	}
	if p.allowData(100, now) || p.allowControl(100, now) {
		t.Fatal("packet above hard limit was accepted")
	}
	if !p.allowDeviceAuth(now) || p.allowDeviceAuth(now) {
		t.Fatal("device authentication budget was not enforced")
	}
	if !p.reserveQueue() || p.reserveQueue() {
		t.Fatal("per-node queue budget was not enforced")
	}
	p.releaseQueue()
	snapshot := p.snapshot()
	if snapshot.DataSoftLimitEvents != 1 || snapshot.DataHardLimitDrops != 1 ||
		snapshot.ControlSoftLimitEvents != 1 || snapshot.ControlHardLimitDrops != 1 ||
		snapshot.DeviceAuthLimitDrops != 1 || snapshot.DataQueueDrops != 1 || snapshot.QueuedData != 0 {
		t.Fatalf("unexpected protection snapshot: %#v", snapshot)
	}
	if !p.allowData(100, now.Add(time.Second)) || !p.allowControl(100, now.Add(time.Second)) || !p.allowDeviceAuth(now.Add(time.Second)) {
		t.Fatal("budgets did not reset in the next window")
	}
}

func TestNodeProtectionBandwidthBudget(t *testing.T) {
	p := newNodeProtection(ResourceLimits{DataSoftPPSPerNode: 10, DataHardPPSPerNode: 20, DataHardMbpsPerNode: 1})
	now := time.Unix(100, 0)
	if !p.allowData(100_000, now) {
		t.Fatal("traffic below bandwidth limit was rejected")
	}
	if p.allowData(30_000, now) {
		t.Fatal("traffic above bandwidth limit was accepted")
	}
}

func TestNodeProtectionLimitsAreIsolatedBehindSharedNAT(t *testing.T) {
	limits := ResourceLimits{
		DataSoftPPSPerNode: 1, DataHardPPSPerNode: 1,
		ControlSoftPPSPerNode: 1, ControlHardPPSPerNode: 1,
		DeviceAuthPPSPerNode: 1, DataQueuePerNode: 1, DataQueueGlobal: 2,
	}
	limits, err := limits.normalized()
	if err != nil {
		t.Fatal(err)
	}
	sharedRemote := "203.0.113.8:45000"
	edgeA := &NodeSession{NodeID: "edge-a", SessionID: 1, RemoteAddr: sharedRemote, protection: newNodeProtection(limits)}
	edgeB := &NodeSession{NodeID: "edge-b", SessionID: 2, RemoteAddr: sharedRemote, protection: newNodeProtection(limits)}
	now := time.Unix(100, 0)
	protectionA := edgeA.resourceProtection()
	if !protectionA.allowData(100, now) || protectionA.allowData(100, now) ||
		!protectionA.allowControl(100, now) || protectionA.allowControl(100, now) ||
		!protectionA.allowDeviceAuth(now) || protectionA.allowDeviceAuth(now) ||
		!protectionA.reserveQueue() || protectionA.reserveQueue() {
		t.Fatal("edge-a did not reach all configured limits")
	}

	protectionB := edgeB.resourceProtection()
	if !protectionB.allowData(100, now) || !protectionB.allowControl(100, now) ||
		!protectionB.allowDeviceAuth(now) || !protectionB.reserveQueue() {
		t.Fatal("edge-a limits affected edge-b behind the same NAT address")
	}
	protectionA.releaseQueue()
	protectionB.releaseQueue()
	if got := edgeB.ProtectionSnapshot(); got.DataHardLimitDrops != 0 || got.ControlHardLimitDrops != 0 ||
		got.DeviceAuthLimitDrops != 0 || got.DataQueueDrops != 0 || got.QueuedData != 0 {
		t.Fatalf("edge-b protection was contaminated by edge-a: %#v", got)
	}
}

func TestNodeDatagramHardPPSLimitIsAttributedOnce(t *testing.T) {
	limits := ResourceLimits{DataSoftPPSPerNode: 1, DataHardPPSPerNode: 1}
	limits, err := limits.normalized()
	if err != nil {
		t.Fatal(err)
	}
	addr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32250}
	session := &NodeSession{
		NodeID: "edge-hard-limit", SessionID: 1, KeyEpoch: 1,
		Key: []byte("datagram-key-123"), protection: newNodeProtection(limits),
	}
	session.BindDataAddr(addr)
	delivered := make(chan uint64, 2)
	bridge, err := NewNodeDatagramBridge(
		func(nodeID string, sessionID uint64) *NodeSession {
			if nodeID == session.NodeID && sessionID == session.SessionID {
				return session
			}
			return nil
		},
		func(_ *NodeSession, env Envelope, _ *net.UDPAddr) { delivered <- env.MessageID },
		time.Second, limits,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	marshal := func(messageID uint64) []byte {
		t.Helper()
		env := NewEnvelope(SubtypeRelayUpstream, session.NodeID, session.SessionID, messageID, []byte("voice"))
		env.KeyEpoch = session.KeyEpoch
		wire, marshalErr := env.Marshal(session.Key)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		return wire
	}

	// Keep both packets away from a wall-clock second boundary used by the
	// production limiter, so this assertion remains deterministic.
	now := time.Now()
	windowStart := now.Truncate(time.Second).Add(time.Second).Add(10 * time.Millisecond)
	time.Sleep(time.Until(windowStart))
	first, second := marshal(1), marshal(2)
	if !bridge.Handle(first, addr) || !bridge.Handle(second, addr) {
		t.Fatal("authenticated Type 0 traffic was not consumed")
	}
	select {
	case messageID := <-delivered:
		if messageID != 1 {
			t.Fatalf("delivered message=%d want=1", messageID)
		}
	case <-time.After(time.Second):
		t.Fatal("first packet was not delivered")
	}
	select {
	case messageID := <-delivered:
		t.Fatalf("hard-limited packet %d reached the handler", messageID)
	case <-time.After(20 * time.Millisecond):
	}

	metrics := session.DataMetrics.Snapshot()
	if metrics.InPackets != 2 || metrics.InBytes != uint64(len(first)+len(second)) || metrics.Drops != 1 || metrics.Errors != 0 {
		t.Fatalf("hard-limit metrics=%#v", metrics)
	}
	protection := session.ProtectionSnapshot()
	if protection.DataHardLimitDrops != 1 || protection.DataSoftLimitEvents != 0 || protection.DataQueueDrops != 0 || protection.DataStaleDrops != 0 {
		t.Fatalf("hard-limit attribution=%#v", protection)
	}
}

func TestNodeDatagramQueueIsolatesNodesAndBoundsGlobalWork(t *testing.T) {
	limits := ResourceLimits{DataQueuePerNode: 1, DataQueueGlobal: 2, DataWorkers: 1, DataMaxQueueAge: time.Second}
	limits, err := limits.normalized()
	if err != nil {
		t.Fatal(err)
	}
	addr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32000}
	sessions := map[string]*NodeSession{}
	for _, nodeID := range []string{"edge-a", "edge-b", "edge-c"} {
		session := &NodeSession{NodeID: nodeID, SessionID: uint64(len(sessions) + 1), KeyEpoch: 1, Key: []byte("datagram-key-123"), protection: newNodeProtection(limits)}
		session.BindDataAddr(addr)
		sessions[nodeID] = session
	}
	started := make(chan struct{})
	release := make(chan struct{})
	delivered := make(chan string, 4)
	var once sync.Once
	bridge, err := NewNodeDatagramBridge(
		func(nodeID string, sessionID uint64) *NodeSession {
			session := sessions[nodeID]
			if session != nil && session.SessionID == sessionID {
				return session
			}
			return nil
		},
		func(session *NodeSession, _ Envelope, _ *net.UDPAddr) {
			once.Do(func() { close(started); <-release })
			delivered <- session.NodeID
		},
		time.Second, limits,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	send := func(nodeID string, messageID uint64) {
		session := sessions[nodeID]
		env := NewEnvelope(SubtypeRelayUpstream, nodeID, session.SessionID, messageID, []byte("voice"))
		env.KeyEpoch = session.KeyEpoch
		wire, marshalErr := env.Marshal(session.Key)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if !bridge.Handle(wire, addr) {
			t.Fatal("authenticated datagram was not consumed")
		}
	}
	send("edge-a", 1)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	send("edge-a", 2)
	send("edge-a", 3) // per-node queue drop
	send("edge-b", 1) // another node still gets its queue slot
	send("edge-c", 1) // global queue is full
	close(release)
	got := map[string]int{}
	for i := 0; i < 3; i++ {
		select {
		case nodeID := <-delivered:
			got[nodeID]++
		case <-time.After(time.Second):
			t.Fatalf("only delivered %#v", got)
		}
	}
	if got["edge-a"] != 2 || got["edge-b"] != 1 || got["edge-c"] != 0 {
		t.Fatalf("queue isolation result=%#v", got)
	}
	if sessions["edge-a"].ProtectionSnapshot().DataQueueDrops != 1 {
		t.Fatal("per-node queue drop was not recorded")
	}
	if got := sessions["edge-a"].DataMetrics.Snapshot(); got.InPackets != 3 || got.Drops != 1 || got.Errors != 0 {
		t.Fatalf("edge-a queue metrics=%#v", got)
	}
	if got := sessions["edge-b"].DataMetrics.Snapshot(); got.InPackets != 1 || got.Drops != 0 || got.Errors != 0 {
		t.Fatalf("edge-b queue metrics=%#v", got)
	}
	if got := sessions["edge-c"].DataMetrics.Snapshot(); got.InPackets != 1 || got.Drops != 1 || got.Errors != 0 {
		t.Fatalf("edge-c queue metrics=%#v", got)
	}
	if got := sessions["edge-c"].ProtectionSnapshot().DataQueueDrops; got != 1 {
		t.Fatalf("edge-c global queue attribution=%d", got)
	}
	if bridge.ProtectionSnapshot().GlobalQueueDrops != 1 {
		t.Fatal("global queue drop was not recorded")
	}
}

func TestNodeDatagramStaleQueueDropIsAttributedOnce(t *testing.T) {
	limits := ResourceLimits{DataQueuePerNode: 2, DataQueueGlobal: 2, DataWorkers: 1, DataMaxQueueAge: 5 * time.Millisecond}
	limits, err := limits.normalized()
	if err != nil {
		t.Fatal(err)
	}
	addr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32300}
	session := &NodeSession{NodeID: "edge-stale", SessionID: 1, KeyEpoch: 1, Key: []byte("datagram-key-123"), protection: newNodeProtection(limits)}
	session.BindDataAddr(addr)
	started := make(chan struct{})
	release := make(chan struct{})
	delivered := make(chan uint64, 2)
	bridge, err := NewNodeDatagramBridge(
		func(nodeID string, sessionID uint64) *NodeSession {
			if nodeID == session.NodeID && sessionID == session.SessionID {
				return session
			}
			return nil
		},
		func(_ *NodeSession, env Envelope, _ *net.UDPAddr) {
			if env.MessageID == 1 {
				close(started)
				<-release
			}
			delivered <- env.MessageID
		},
		time.Second, limits,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	send := func(messageID uint64) int {
		t.Helper()
		env := NewEnvelope(SubtypeRelayUpstream, session.NodeID, session.SessionID, messageID, []byte("voice"))
		env.KeyEpoch = session.KeyEpoch
		wire, marshalErr := env.Marshal(session.Key)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if !bridge.Handle(wire, addr) {
			t.Fatal("authenticated datagram was not consumed")
		}
		return len(wire)
	}
	firstBytes := send(1)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	secondBytes := send(2)
	time.Sleep(20 * time.Millisecond)
	close(release)
	select {
	case messageID := <-delivered:
		if messageID != 1 {
			t.Fatalf("delivered message=%d want=1", messageID)
		}
	case <-time.After(time.Second):
		t.Fatal("first message did not finish")
	}
	deadline := time.Now().Add(time.Second)
	for session.ProtectionSnapshot().DataStaleDrops != 1 {
		if time.Now().After(deadline) {
			t.Fatal("queued stale message was not dropped")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case messageID := <-delivered:
		t.Fatalf("stale message %d reached the handler", messageID)
	case <-time.After(20 * time.Millisecond):
	}
	metrics := session.DataMetrics.Snapshot()
	if metrics.InPackets != 2 || metrics.InBytes != uint64(firstBytes+secondBytes) || metrics.Drops != 1 || metrics.Errors != 0 {
		t.Fatalf("stale queue metrics=%#v", metrics)
	}
	protection := session.ProtectionSnapshot()
	if protection.DataStaleDrops != 1 || protection.DataQueueDrops != 0 || protection.DataHardLimitDrops != 0 || protection.QueuedData != 0 {
		t.Fatalf("stale queue attribution=%#v", protection)
	}
}

func TestReplayWindowConcurrentAndBenchmarkShape(t *testing.T) {
	var window replayWindow
	const count = 4096
	var accepted sync.Map
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for id := uint64(offset + 1); id <= count; id += 8 {
				if window.accept(id) {
					accepted.Store(id, struct{}{})
				}
			}
		}(worker)
	}
	wg.Wait()
	seen := 0
	accepted.Range(func(_, _ any) bool { seen++; return true })
	if seen != count {
		t.Fatalf("accepted %d unique IDs, want %d", seen, count)
	}
	for id := uint64(1); id <= count; id++ {
		if window.accept(id) {
			t.Fatalf("replay %d was accepted", id)
		}
	}
}

func BenchmarkReplayWindowSequential(b *testing.B) {
	var window replayWindow
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		window.accept(uint64(i + 1))
	}
}
