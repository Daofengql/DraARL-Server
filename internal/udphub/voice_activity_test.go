package udphub

import (
	"testing"
	"time"
)

func TestAcceptedVoiceQuietBoundaryAndStartupGuard(t *testing.T) {
	start := time.Date(2026, 8, 9, 4, 0, 0, 0, time.UTC)
	ResetAcceptedVoiceActivity(start)
	groups := []int{101, 102}

	if last, quiet := IsAcceptedVoiceDomainQuiet(groups, start.Add(5*time.Second-time.Nanosecond), 5*time.Second); quiet || !last.Equal(start) {
		t.Fatalf("startup guard before boundary: last=%v quiet=%t", last, quiet)
	}
	if last, quiet := IsAcceptedVoiceDomainQuiet(groups, start.Add(5*time.Second), 5*time.Second); !quiet || !last.Equal(start) {
		t.Fatalf("startup guard at boundary: last=%v quiet=%t", last, quiet)
	}

	acceptedAt := start.Add(7 * time.Second)
	MarkAcceptedVoiceGroups(groups, acceptedAt)
	MarkAcceptedVoiceGroups(groups, acceptedAt.Add(-time.Second))
	if got := LastAcceptedVoiceAt(groups); !got.Equal(acceptedAt) {
		t.Fatalf("last accepted voice regressed: got=%v want=%v", got, acceptedAt)
	}
	if _, quiet := IsAcceptedVoiceDomainQuiet(groups, acceptedAt.Add(5*time.Second-time.Nanosecond), 5*time.Second); quiet {
		t.Fatal("domain became quiet before five seconds")
	}
	if _, quiet := IsAcceptedVoiceDomainQuiet(groups, acceptedAt.Add(5*time.Second), 5*time.Second); !quiet {
		t.Fatal("domain was not quiet at exactly five seconds")
	}
}

func TestAcceptedVoiceTracksOnlyActiveDeliveryTopology(t *testing.T) {
	t.Run("closed virtual association", func(t *testing.T) {
		env := setupRouteTest(t, 61000, false)
		virtual, _ := GetGroupFromCache(env.virtual)
		virtual.Status = 0
		globalGroupLinkCache.Lock()
		globalGroupLinkCache.targetToLinks[env.groupA] = []int{env.virtual}
		globalGroupLinkCache.targetToLinks[env.groupB] = []int{env.virtual}
		globalGroupLinkCache.linkToTargets[env.virtual] = []int{env.groupA, env.groupB}
		globalGroupLinkCache.targetToPeers = make(map[int][]int)
		globalGroupLinkCache.Unlock()
		resetHalfDuplexDomainCache()
		sourceGroupID := uint(env.groupA)
		if snapshot := SnapshotDeliveryGroupIDs(&sourceGroupID); len(snapshot) != 1 || snapshot[0] != sourceGroupID {
			t.Fatalf("closed virtual delivery snapshot=%v", snapshot)
		}

		ResetAcceptedVoiceActivity(time.Now().Add(-10 * time.Second))
		env.router.RouteVoiceToUDP(env.wsSource, []byte{1, 2, 3}, env.groupA)
		if LastAcceptedVoiceAt([]int{env.groupA}).IsZero() {
			t.Fatal("source group activity was not recorded")
		}
		if !LastAcceptedVoiceAt([]int{env.groupB}).IsZero() {
			t.Fatal("closed virtual association expanded voice activity")
		}
	})

	t.Run("enabled virtual domain", func(t *testing.T) {
		env := setupRouteTest(t, 61200, true)
		sourceGroupID := uint(env.groupA)
		snapshot := SnapshotDeliveryGroupIDs(&sourceGroupID)
		if len(snapshot) != 2 || snapshot[0] != uint(env.groupA) || snapshot[1] != uint(env.groupB) {
			t.Fatalf("enabled virtual delivery snapshot=%v", snapshot)
		}
		ResetAcceptedVoiceActivity(time.Now().Add(-10 * time.Second))
		env.router.RouteVoiceToUDP(env.wsSource, []byte{4, 5, 6}, env.groupA)
		groupAAt := LastAcceptedVoiceAt([]int{env.groupA})
		groupBAt := LastAcceptedVoiceAt([]int{env.groupB})
		if groupAAt.IsZero() || !groupAAt.Equal(groupBAt) {
			t.Fatalf("enabled delivery domain activity mismatch: A=%v B=%v", groupAAt, groupBAt)
		}
		if !LastAcceptedVoiceAt([]int{env.groupC}).IsZero() {
			t.Fatal("unlinked group activity was updated")
		}
	})
}

func TestRejectedVoiceAndTextDoNotRefreshAcceptedActivity(t *testing.T) {
	env := setupRouteTest(t, 61400, false)
	ResetAcceptedVoiceActivity(time.Now().Add(-10 * time.Second))
	env.router.RouteTextToUDP(env.wsSource, []byte("status"), env.groupA)
	if got := LastAcceptedVoiceAt([]int{env.groupA}); !got.IsZero() {
		t.Fatalf("text refreshed voice activity: %v", got)
	}

	env.router.RouteVoiceToUDP(env.wsSource, []byte{1, 2}, env.groupA)
	acceptedAt := LastAcceptedVoiceAt([]int{env.groupA})
	if acceptedAt.IsZero() {
		t.Fatal("accepted voice did not refresh activity")
	}
	env.router.RouteVoiceToUDP(env.wsA, []byte{3, 4}, env.groupA)
	if got := LastAcceptedVoiceAt([]int{env.groupA}); !got.Equal(acceptedAt) {
		t.Fatalf("blocked competing voice refreshed activity: got=%v want=%v", got, acceptedAt)
	}
}
