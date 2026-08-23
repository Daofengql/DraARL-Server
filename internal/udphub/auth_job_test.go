package udphub

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"draarl/internal/config"
	"draarl/internal/models"
	"draarl/internal/protocol"
)

func TestAuthenticateDeviceContextStopsBeforeBackendWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := AuthenticateDeviceContext(ctx, "192.0.2.10", "alice", "secret")
	if result == nil || result.Success || result.Error != "auth_canceled" {
		t.Fatalf("canceled authentication result=%+v, want auth_canceled", result)
	}

	deadlineCtx, deadlineCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer deadlineCancel()
	result = AuthenticateDeviceContext(deadlineCtx, "192.0.2.10", "alice", "secret")
	if result == nil || result.Success || result.Error != "auth_backend_timeout" {
		t.Fatalf("expired authentication result=%+v, want auth_backend_timeout", result)
	}
}

func TestActivateCenterDeviceContextStopsBeforeBackendWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := activateAndPersistCenterDeviceContext(ctx, &models.Device{ID: 1})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled center activation error=%v, want context.Canceled", err)
	}
}

func TestShardedAuthMapGetSnapshotAndMissingKey(t *testing.T) {
	m := NewShardedAuthMap()
	if _, exists := m.Get("missing"); exists {
		t.Fatal("missing authentication failure was reported as present")
	}

	key := "192.0.2.10:alice"
	m.Set(key, AuthFailure{IP: "192.0.2.10", Username: "alice", FailCount: 2})
	snapshot, exists := m.Get(key)
	if !exists || snapshot.FailCount != 2 {
		t.Fatalf("unexpected snapshot: exists=%t value=%+v", exists, snapshot)
	}
	snapshot.FailCount = 99
	current, exists := m.Get(key)
	if !exists || current.FailCount != 2 {
		t.Fatalf("Get returned a mutable map value: exists=%t value=%+v", exists, current)
	}
}

func TestShardedAuthMapUpdateIsAtomic(t *testing.T) {
	m := NewShardedAuthMap()
	const (
		workers = 16
		updates = 100
	)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < updates; j++ {
				m.Update("198.51.100.7:alice", func(failure AuthFailure) AuthFailure {
					failure.FailCount++
					return failure
				})
			}
		}()
	}
	wg.Wait()
	failure, exists := m.Get("198.51.100.7:alice")
	if !exists || failure.FailCount != workers*updates {
		t.Fatalf("atomic update lost increments: exists=%t failure=%+v", exists, failure)
	}
}

func TestShardedAuthMapCleansUnblockedFailures(t *testing.T) {
	m := NewShardedAuthMap()
	now := time.Now()
	m.Set("203.0.113.1:one", AuthFailure{FailCount: 1, LastFailureAt: now.Add(-11 * time.Minute)})
	m.Set("203.0.113.2:two", AuthFailure{FailCount: 3, BlockedUntil: now.Add(-time.Minute), LastFailureAt: now.Add(-11 * time.Minute)})
	if removed := m.CleanExpired(now); removed != 2 || m.Len() != 0 {
		t.Fatalf("expired failure cleanup removed=%d len=%d", removed, m.Len())
	}
}

func TestShardedAuthMapEnforcesGlobalCapacityAndEvictsOnlySafeEntries(t *testing.T) {
	m := NewShardedAuthMapWithLimit(2)
	now := time.Now()
	firstKey := "198.51.100.1:expired"
	firstShard := m.getShard(firstKey)
	keyForShard := func(prefix string) string {
		for i := 0; ; i++ {
			candidate := fmt.Sprintf("%s:%d", prefix, i)
			if m.getShard(candidate) == firstShard {
				return candidate
			}
		}
	}
	activeKey := keyForShard("active")
	newKey := keyForShard("new")
	blockedKey := keyForShard("blocked")
	m.Set(firstKey, AuthFailure{LastFailureAt: now.Add(-11 * time.Minute)})
	m.Set(activeKey, AuthFailure{BlockedUntil: now.Add(time.Minute), LastFailureAt: now})
	if got := m.Len(); got != 2 {
		t.Fatalf("initial map length=%d, want 2", got)
	}

	// The expired entry may be evicted to admit a new identity.
	if _, admitted := m.UpdateBounded(newKey, func(failure AuthFailure) AuthFailure {
		failure.LastFailureAt = now
		failure.BlockedUntil = now.Add(time.Minute)
		return failure
	}); !admitted {
		t.Fatal("expected expired entry to be evicted for new identity")
	}
	if _, exists := m.Get(activeKey); !exists {
		t.Fatal("active blocked entry was evicted")
	}
	if _, exists := m.Get(firstKey); exists {
		t.Fatal("expired entry remained after capacity eviction")
	}
	if got := m.Len(); got != 2 {
		t.Fatalf("bounded map length=%d, want 2", got)
	}

	// With only active blocked entries left, admission fails closed and the
	// existing throttles remain intact.
	if _, admitted := m.UpdateBounded(blockedKey, func(failure AuthFailure) AuthFailure {
		return failure
	}); admitted {
		t.Fatal("admitted a new identity while all entries were actively blocked")
	}
	if got := m.Len(); got != 2 {
		t.Fatalf("map length after rejected admission=%d, want 2", got)
	}
}

func TestShardedAuthMapBoundedUpdateRemainsAtomic(t *testing.T) {
	m := NewShardedAuthMapWithLimit(1)
	const updates = 1000
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < updates; j++ {
				m.UpdateBounded("198.51.100.7:alice", func(failure AuthFailure) AuthFailure {
					failure.FailCount++
					return failure
				})
			}
		}()
	}
	wg.Wait()
	failure, exists := m.Get("198.51.100.7:alice")
	if !exists || failure.FailCount != 2*updates {
		t.Fatalf("bounded atomic update lost increments: exists=%t failure=%+v", exists, failure)
	}
}

func TestNewDeviceAuthJobCopiesWireData(t *testing.T) {
	wire := protocol.EncodeDraARLv1("alice", "device-secret", 7, protocol.DraARLTypeHeartbeat,
		protocol.DraARLDevModelESP32NoRadio, 0x123456, "BG7TEST", []byte{1, 2, 3})
	addr := &net.UDPAddr{IP: net.ParseIP("192.0.2.20"), Port: 41000}
	packet, err := protocol.NewDraARLv1Packet(addr, wire)
	if err != nil {
		t.Fatal(err)
	}
	job := newDeviceAuthJob(packet, wire, nil, nil, addr, "", false)
	if job == nil || len(job.packet.DATA) != 3 || job.packet.DATA[0] != 1 {
		t.Fatalf("unexpected auth job copy: %#v", job)
	}
	wire[protocol.DraARLv1HeaderSize] = 9
	packet.DATA[0] = 8
	if job.packet.DATA[0] != 1 {
		t.Fatalf("auth job retained caller-owned packet data: %v", job.packet.DATA)
	}
	if len(job.packet.Reserved) != protocol.DraARLv1HeaderSize-protocol.DraARLv1ReservedOffset {
		t.Fatalf("reserved field length=%d", len(job.packet.Reserved))
	}
}

func TestDeviceAuthAdmissionCoalescesIdentity(t *testing.T) {
	deviceAuthMu.Lock()
	oldQueues, oldPending, oldStarted, oldStopping := deviceAuthQueues, deviceAuthPending, deviceAuthStarted, deviceAuthStopping
	deviceAuthQueues = []chan *deviceAuthJob{make(chan *deviceAuthJob, 2)}
	deviceAuthPending = make(map[string]struct{})
	deviceAuthStarted = true
	deviceAuthStopping = false
	deviceAuthMu.Unlock()
	t.Cleanup(func() {
		deviceAuthMu.Lock()
		deviceAuthQueues, deviceAuthPending, deviceAuthStarted, deviceAuthStopping = oldQueues, oldPending, oldStarted, oldStopping
		deviceAuthMu.Unlock()
	})

	wire := protocol.EncodeDraARLv1("alice", "secret", 7, protocol.DraARLTypeHeartbeat,
		protocol.DraARLDevModelESP32NoRadio, 0, "", nil)
	addr := &net.UDPAddr{IP: net.ParseIP("198.51.100.30"), Port: 42000}
	packet, err := protocol.NewDraARLv1Packet(addr, wire)
	if err != nil {
		t.Fatal(err)
	}
	first := newDeviceAuthJob(packet, wire, nil, nil, addr, "", false)
	second := newDeviceAuthJob(packet, wire, nil, nil, &net.UDPAddr{IP: net.ParseIP("198.51.100.31"), Port: 42001}, "", false)
	otherWire := protocol.EncodeDraARLv1("bob", "secret", 7, protocol.DraARLTypeHeartbeat,
		protocol.DraARLDevModelESP32NoRadio, 0, "", nil)
	otherPacket, err := protocol.NewDraARLv1Packet(addr, otherWire)
	if err != nil {
		t.Fatal(err)
	}
	other := newDeviceAuthJob(otherPacket, otherWire, nil, nil, addr, "", false)
	if got := enqueueDeviceAuth(first); got != deviceAuthAccepted {
		t.Fatalf("first admission=%d, want accepted", got)
	}
	if got := enqueueDeviceAuth(second); got != deviceAuthCoalesced {
		t.Fatalf("duplicate admission=%d, want coalesced", got)
	}
	if got := enqueueDeviceAuth(other); got != deviceAuthAccepted {
		t.Fatalf("distinct identity admission=%d, want accepted", got)
	}
}

func TestDeviceAuthAdmissionRejectsWhenShardQueueIsFull(t *testing.T) {
	deviceAuthMu.Lock()
	oldQueues, oldPending, oldStarted, oldStopping := deviceAuthQueues, deviceAuthPending, deviceAuthStarted, deviceAuthStopping
	deviceAuthQueues = []chan *deviceAuthJob{make(chan *deviceAuthJob, 1)}
	deviceAuthPending = make(map[string]struct{})
	deviceAuthStarted = true
	deviceAuthStopping = false
	deviceAuthMu.Unlock()
	t.Cleanup(func() {
		deviceAuthMu.Lock()
		deviceAuthQueues, deviceAuthPending, deviceAuthStarted, deviceAuthStopping = oldQueues, oldPending, oldStarted, oldStopping
		deviceAuthMu.Unlock()
	})

	first := &deviceAuthJob{packet: protocol.DraARLv1Packet{Username: "alice", SSID: 1}, data: []byte{1}}
	second := &deviceAuthJob{packet: protocol.DraARLv1Packet{Username: "bob", SSID: 2}, data: []byte{2}}
	if got := enqueueDeviceAuth(first); got != deviceAuthAccepted {
		t.Fatalf("first admission=%d, want accepted", got)
	}
	if got := enqueueDeviceAuth(second); got != deviceAuthRejected {
		t.Fatalf("full queue admission=%d, want rejected", got)
	}
	if _, exists := deviceAuthPending[deviceAuthIdentity(second)]; exists {
		t.Fatal("rejected identity was retained as pending")
	}
}

func TestStopDeviceAuthWorkersClosesAdmissionAndDrainsAcceptedJobs(t *testing.T) {
	previousConfig := config.Config
	deviceAuthMu.Lock()
	if deviceAuthStarted {
		deviceAuthMu.Unlock()
		t.Fatal("device authentication workers unexpectedly running before test")
	}
	oldQueues, oldPending, oldStopping := deviceAuthQueues, deviceAuthPending, deviceAuthStopping
	oldCtx, oldCancel := deviceAuthCtx, deviceAuthCancel
	deviceAuthMu.Unlock()

	config.Config = &config.Configuration{}
	config.Config.UDP.DeviceAuthWorkers = 1
	config.Config.UDP.DeviceAuthQueueSize = 2
	processingStarted := make(chan string, 2)
	releaseProcessing := make(chan struct{})
	completed := make(chan string, 2)
	var releaseOnce sync.Once
	startDeviceAuthWorkersWith(func(job *deviceAuthJob) {
		processingStarted <- deviceAuthIdentity(job)
		<-releaseProcessing
		completed <- deviceAuthIdentity(job)
	})
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(releaseProcessing) })
		stopDeviceAuthWorkers()
		config.Config = previousConfig
		deviceAuthMu.Lock()
		deviceAuthQueues, deviceAuthPending, deviceAuthStopping = oldQueues, oldPending, oldStopping
		deviceAuthCtx, deviceAuthCancel = oldCtx, oldCancel
		deviceAuthMu.Unlock()
	})

	first := &deviceAuthJob{packet: protocol.DraARLv1Packet{Username: "alice", SSID: 1}, data: []byte{1}}
	second := &deviceAuthJob{packet: protocol.DraARLv1Packet{Username: "bob", SSID: 2}, data: []byte{2}}
	if got := enqueueDeviceAuth(first); got != deviceAuthAccepted {
		t.Fatalf("first admission=%d, want accepted", got)
	}
	select {
	case <-processingStarted:
	case <-time.After(time.Second):
		t.Fatal("first accepted authentication did not start")
	}
	if got := enqueueDeviceAuth(second); got != deviceAuthAccepted {
		t.Fatalf("second admission=%d, want accepted", got)
	}

	stopped := make(chan struct{})
	go func() {
		stopDeviceAuthWorkers()
		close(stopped)
	}()
	select {
	case <-stopped:
		t.Fatal("stop returned before accepted authentication jobs completed")
	case <-time.After(30 * time.Millisecond):
	}
	deviceAuthMu.Lock()
	stopping := deviceAuthStopping
	queueBeforeRestart := deviceAuthQueues[0]
	deviceAuthMu.Unlock()
	if !stopping {
		t.Fatal("authentication pool did not enter stopping state")
	}
	startDeviceAuthWorkersWith(func(*deviceAuthJob) {
		t.Fatal("started a new authentication worker generation while stopping")
	})
	deviceAuthMu.Lock()
	queueAfterRestart := deviceAuthQueues[0]
	deviceAuthMu.Unlock()
	if queueAfterRestart != queueBeforeRestart {
		t.Fatal("authentication worker queues changed while stopping")
	}
	if got := enqueueDeviceAuth(&deviceAuthJob{packet: protocol.DraARLv1Packet{Username: "carol", SSID: 3}, data: []byte{3}}); got != deviceAuthRejected {
		t.Fatalf("admission during shutdown=%d, want rejected", got)
	}

	releaseOnce.Do(func() { close(releaseProcessing) })
	for i := 0; i < 2; i++ {
		select {
		case <-completed:
		case <-time.After(time.Second):
			t.Fatal("accepted authentication was not drained during stop")
		}
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("stop did not return after accepted authentication jobs completed")
	}
}

func TestStopDeviceAuthWorkersCancelsInFlightAuthenticationContext(t *testing.T) {
	previousConfig := config.Config
	deviceAuthMu.Lock()
	if deviceAuthStarted {
		deviceAuthMu.Unlock()
		t.Fatal("device authentication workers unexpectedly running before test")
	}
	oldQueues, oldPending, oldStopping := deviceAuthQueues, deviceAuthPending, deviceAuthStopping
	deviceAuthMu.Unlock()

	config.Config = &config.Configuration{}
	config.Config.UDP.DeviceAuthWorkers = 1
	config.Config.UDP.DeviceAuthQueueSize = 1
	started := make(chan struct{})
	canceled := make(chan struct{})
	startDeviceAuthWorkersWith(func(job *deviceAuthJob) {
		close(started)
		<-job.ctx.Done()
		close(canceled)
	})
	t.Cleanup(func() {
		stopDeviceAuthWorkers()
		config.Config = previousConfig
		deviceAuthMu.Lock()
		deviceAuthQueues, deviceAuthPending, deviceAuthStopping = oldQueues, oldPending, oldStopping
		deviceAuthMu.Unlock()
	})

	job := &deviceAuthJob{packet: protocol.DraARLv1Packet{Username: "cancel-me", SSID: 1}, data: []byte{1}}
	if got := enqueueDeviceAuth(job); got != deviceAuthAccepted {
		t.Fatalf("admission=%d, want accepted", got)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("authentication job did not start")
	}

	stopped := make(chan struct{})
	go func() {
		stopDeviceAuthWorkers()
		close(stopped)
	}()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("worker context was not canceled during stop")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("stop did not finish after canceled authentication")
	}
}

func TestDeviceAuthWorkerRecoversPanicReleasesIdentityAndContinues(t *testing.T) {
	deviceAuthMu.Lock()
	oldPending := deviceAuthPending
	deviceAuthPending = map[string]struct{}{"alice-1": {}, "bob-2": {}}
	deviceAuthMu.Unlock()
	t.Cleanup(func() {
		deviceAuthMu.Lock()
		deviceAuthPending = oldPending
		deviceAuthMu.Unlock()
	})

	queue := make(chan *deviceAuthJob, 2)
	queue <- &deviceAuthJob{packet: protocol.DraARLv1Packet{Username: "alice", SSID: 1}}
	queue <- &deviceAuthJob{packet: protocol.DraARLv1Packet{Username: "bob", SSID: 2}}
	close(queue)
	processed := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		deviceAuthWorkerLoopWith(queue, func(job *deviceAuthJob) {
			if job.packet.Username == "alice" {
				panic("test authentication panic")
			}
			processed <- deviceAuthIdentity(job)
		})
		close(done)
	}()

	select {
	case identity := <-processed:
		if identity != "bob-2" {
			t.Fatalf("processed identity=%q, want bob-2", identity)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not continue after a task panic")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not exit after draining the closed queue")
	}
	deviceAuthMu.Lock()
	pending := len(deviceAuthPending)
	deviceAuthMu.Unlock()
	if pending != 0 {
		t.Fatalf("pending identities after panic recovery=%d, want 0", pending)
	}
}

func TestDeviceAuthRuntimeSettingsClamp(t *testing.T) {
	previous := config.Config
	t.Cleanup(func() { config.Config = previous })

	config.Config = &config.Configuration{}
	workers, queueSize := deviceAuthRuntimeSettings()
	if workers != defaultDeviceAuthWorkers || queueSize != defaultDeviceAuthQueueSize {
		t.Fatalf("defaults workers=%d queue=%d", workers, queueSize)
	}
	config.Config.UDP.DeviceAuthWorkers = maxDeviceAuthWorkers + 1
	config.Config.UDP.DeviceAuthQueueSize = maxDeviceAuthQueueSize + 1
	workers, queueSize = deviceAuthRuntimeSettings()
	if workers != maxDeviceAuthWorkers || queueSize != maxDeviceAuthQueueSize {
		t.Fatalf("clamped workers=%d queue=%d", workers, queueSize)
	}
}
