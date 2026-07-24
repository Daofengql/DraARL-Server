package interconnect

import (
	"sync"
	"testing"
	"time"
)

func TestNodeStatusDispatcherIsNonBlockingAndCoalescesPerNode(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	var observed []int
	dispatcher := NewNodeStatusDispatcher(func(_ *NodeSession, heartbeat *NodeHeartbeat, _ bool) {
		once.Do(func() { close(started) })
		<-release
		if heartbeat != nil {
			mu.Lock()
			observed = append(observed, heartbeat.ConnectionCount)
			mu.Unlock()
		}
	})
	session := &NodeSession{NodeID: "edge-a"}
	dispatcher.Submit(session, &NodeHeartbeat{ConnectionCount: 1}, true)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("status sink did not start")
	}
	begin := time.Now()
	for count := 2; count <= 1000; count++ {
		dispatcher.Submit(session, &NodeHeartbeat{ConnectionCount: count}, true)
	}
	if elapsed := time.Since(begin); elapsed > 100*time.Millisecond {
		t.Fatalf("submitting coalesced status blocked for %s", elapsed)
	}
	close(release)
	dispatcher.Close()
	mu.Lock()
	defer mu.Unlock()
	if len(observed) != 2 || observed[0] != 1 || observed[1] != 1000 {
		t.Fatalf("observed status sequence = %v, want [1 1000]", observed)
	}
}

func TestNodeStatusDispatcherFlushesOnClose(t *testing.T) {
	var mu sync.Mutex
	seen := make(map[string]bool)
	dispatcher := NewNodeStatusDispatcher(func(session *NodeSession, _ *NodeHeartbeat, online bool) {
		mu.Lock()
		seen[session.NodeID] = online
		mu.Unlock()
	})
	dispatcher.Submit(&NodeSession{NodeID: "edge-a"}, &NodeHeartbeat{ConnectionCount: 1}, false)
	dispatcher.Submit(&NodeSession{NodeID: "edge-b"}, &NodeHeartbeat{ConnectionCount: 2}, true)
	dispatcher.Close()
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || seen["edge-a"] || !seen["edge-b"] {
		t.Fatalf("close did not flush latest state: %#v", seen)
	}
}

func TestNodeStatusDispatcherAppliesLifecycleSynchronously(t *testing.T) {
	var observed []bool
	dispatcher := NewNodeStatusDispatcher(func(_ *NodeSession, heartbeat *NodeHeartbeat, online bool) {
		if heartbeat != nil {
			t.Fatal("lifecycle callback unexpectedly carried a heartbeat")
		}
		observed = append(observed, online)
	})
	dispatcher.Submit(&NodeSession{NodeID: "edge-a"}, nil, true)
	dispatcher.Submit(&NodeSession{NodeID: "edge-a"}, nil, false)
	dispatcher.Close()
	if len(observed) != 2 || !observed[0] || observed[1] {
		t.Fatalf("lifecycle order = %v, want [true false]", observed)
	}
}
