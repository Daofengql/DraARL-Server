package udphub

import (
	"testing"
	"time"
)

func TestTryAcquireScheduledBroadcastQuietAndBusyResults(t *testing.T) {
	env := setupRouteTest(t, 61600, false)
	oldHooks := centerHooks()
	SetCenterInterconnectHooks(CenterInterconnectHooks{})
	t.Cleanup(func() { SetCenterInterconnectHooks(oldHooks) })
	start := time.Date(2026, 8, 9, 5, 0, 0, 0, time.UTC)
	ResetAcceptedVoiceActivity(start)

	if lease, last, result := TryAcquireScheduledBroadcast(env.groupA, 1, start.Add(5*time.Second-time.Nanosecond), 5*time.Second); lease != nil || result != ScheduledBroadcastRecentVoice || !last.Equal(start) {
		t.Fatalf("pre-boundary acquire: lease=%v last=%v result=%s", lease, last, result)
	}
	lease, _, result := TryAcquireScheduledBroadcast(env.groupA, 1, start.Add(5*time.Second), 5*time.Second)
	if lease == nil || result != ScheduledBroadcastAcquired {
		t.Fatalf("boundary acquire: lease=%v result=%s", lease, result)
	}
	if competing, _, competingResult := TryAcquireScheduledBroadcast(env.groupA, 2, start.Add(5*time.Second), 5*time.Second); competing != nil || competingResult != ScheduledBroadcastDomainBusy {
		t.Fatalf("competing acquire: lease=%v result=%s", competing, competingResult)
	}
	ReleaseScheduledBroadcast(lease)

	lease, _, result = TryAcquireScheduledBroadcast(env.groupA, 2, start.Add(5*time.Second), 5*time.Second)
	if lease == nil || result != ScheduledBroadcastAcquired {
		t.Fatalf("acquire after release: lease=%v result=%s", lease, result)
	}
	acceptedAt := start.Add(5100 * time.Millisecond)
	if !AcceptScheduledBroadcastFrame(lease, acceptedAt) {
		t.Fatal("scheduled frame did not renew its lease")
	}
	ReleaseScheduledBroadcast(lease)
	if got := LastAcceptedVoiceAt([]int{env.groupA}); !got.Equal(acceptedAt) {
		t.Fatalf("scheduled frame activity=%v want=%v", got, acceptedAt)
	}
	if next, _, nextResult := TryAcquireScheduledBroadcast(env.groupA, 3, acceptedAt.Add(5*time.Second-time.Nanosecond), 5*time.Second); next != nil || nextResult != ScheduledBroadcastRecentVoice {
		t.Fatalf("recent scheduled voice acquire: lease=%v result=%s", next, nextResult)
	}
	if next, _, nextResult := TryAcquireScheduledBroadcast(env.groupA, 3, acceptedAt.Add(5*time.Second), 5*time.Second); next == nil || nextResult != ScheduledBroadcastAcquired {
		t.Fatalf("quiet boundary after scheduled voice: lease=%v result=%s", next, nextResult)
	} else {
		ReleaseScheduledBroadcast(next)
	}
}

func TestScheduledBroadcastLeaseStopsOnTopologyChange(t *testing.T) {
	env := setupRouteTest(t, 61800, false)
	oldHooks := centerHooks()
	SetCenterInterconnectHooks(CenterInterconnectHooks{})
	t.Cleanup(func() { SetCenterInterconnectHooks(oldHooks) })
	ResetAcceptedVoiceActivity(time.Now().Add(-10 * time.Second))
	lease, _, result := TryAcquireScheduledBroadcast(env.groupA, 10, time.Now(), 5*time.Second)
	if lease == nil || result != ScheduledBroadcastAcquired {
		t.Fatalf("initial acquire: lease=%v result=%s", lease, result)
	}

	globalGroupLinkCache.Lock()
	globalGroupLinkCache.targetToPeers[env.groupA] = []int{env.groupB}
	globalGroupLinkCache.targetToPeers[env.groupB] = []int{env.groupA}
	globalGroupLinkCache.Unlock()
	resetHalfDuplexDomainCache()
	if AcceptScheduledBroadcastFrame(lease, time.Now()) {
		t.Fatal("topology-changing scheduled lease accepted another frame")
	}
	ReleaseScheduledBroadcast(lease)
}

func TestScheduledBroadcastUsesCenterSpeakerHooks(t *testing.T) {
	env := setupRouteTest(t, 62000, true)
	ResetAcceptedVoiceActivity(time.Now().Add(-10 * time.Second))
	acquired, accepted, released := 0, 0, 0
	oldHooks := centerHooks()
	SetCenterInterconnectHooks(CenterInterconnectHooks{
		Activate: func(*CenterLocalSource) error { return nil },
		AcquireBroadcast: func(runID uint, domainID uint64, now time.Time) bool {
			acquired++
			return runID == 20 && domainID == GetActiveCommunicationDomainID(env.groupA)
		},
		AcceptBroadcastFrame: func(runID uint, domainID uint64, now time.Time) bool {
			accepted++
			return true
		},
		ReleaseBroadcast: func(runID uint, domainID uint64) { released++ },
	})
	t.Cleanup(func() { SetCenterInterconnectHooks(oldHooks) })

	lease, _, result := TryAcquireScheduledBroadcast(env.groupA, 20, time.Now(), 5*time.Second)
	if lease == nil || result != ScheduledBroadcastAcquired || acquired != 1 {
		t.Fatalf("center acquire: lease=%v result=%s acquired=%d", lease, result, acquired)
	}
	if !AcceptScheduledBroadcastFrame(lease, time.Now()) || accepted != 1 {
		t.Fatalf("center frame accepted=%d", accepted)
	}
	if len(lease.DomainGroupIDs) != 2 || lease.DomainGroupIDs[0] != env.groupA || lease.DomainGroupIDs[1] != env.groupB {
		t.Fatalf("center delivery snapshot=%v", lease.DomainGroupIDs)
	}
	ReleaseScheduledBroadcast(lease)
	if released != 1 {
		t.Fatalf("center release count=%d", released)
	}
}
