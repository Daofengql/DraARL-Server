package udphub

import (
	"sync"
	"time"
)

var acceptedVoiceActivity = struct {
	sync.RWMutex
	startedAt time.Time
	byGroup   map[int]time.Time
}{byGroup: make(map[int]time.Time)}

// ResetAcceptedVoiceActivity starts a new process-level quiet window and
// clears per-group activity. The UDP runtime invokes this once during startup;
// tests may also use it to establish a deterministic clock origin.
func ResetAcceptedVoiceActivity(startedAt time.Time) {
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	acceptedVoiceActivity.Lock()
	acceptedVoiceActivity.startedAt = startedAt
	acceptedVoiceActivity.byGroup = make(map[int]time.Time)
	acceptedVoiceActivity.Unlock()
}

// GetActiveCommunicationGroupIDs returns a copy of the entity groups that a
// packet from sourceGroupID would reach under the current enabled topology.
func GetActiveCommunicationGroupIDs(sourceGroupID int) []int {
	return append([]int(nil), activeDomainGroupIDs(sourceGroupID)...)
}

// MarkAcceptedVoice records one voice frame only after it has passed the
// active half-duplex arbiter. Every actual delivery group receives the same
// timestamp so a later topology change cannot erase recent communication.
func MarkAcceptedVoice(sourceGroupID int, acceptedAt time.Time) []int {
	groupIDs := GetActiveCommunicationGroupIDs(sourceGroupID)
	MarkAcceptedVoiceGroups(groupIDs, acceptedAt)
	return groupIDs
}

// MarkAcceptedVoiceGroups records a precomputed delivery snapshot. Broadcast
// playback uses this form so topology changes cannot expand its fixed domain.
func MarkAcceptedVoiceGroups(groupIDs []int, acceptedAt time.Time) {
	if acceptedAt.IsZero() {
		acceptedAt = time.Now()
	}
	acceptedVoiceActivity.Lock()
	for _, groupID := range groupIDs {
		if groupID <= 0 {
			continue
		}
		if previous := acceptedVoiceActivity.byGroup[groupID]; previous.Before(acceptedAt) {
			acceptedVoiceActivity.byGroup[groupID] = acceptedAt
		}
	}
	acceptedVoiceActivity.Unlock()
}

// LastAcceptedVoiceAt returns the newest accepted voice timestamp in a fixed
// group-domain snapshot. It deliberately ignores rejected frames and all
// non-voice packet types.
func LastAcceptedVoiceAt(groupIDs []int) time.Time {
	acceptedVoiceActivity.RLock()
	defer acceptedVoiceActivity.RUnlock()
	var latest time.Time
	for _, groupID := range groupIDs {
		if acceptedAt := acceptedVoiceActivity.byGroup[groupID]; latest.Before(acceptedAt) {
			latest = acceptedAt
		}
	}
	return latest
}

// IsAcceptedVoiceDomainQuiet applies both the process startup guard and the
// per-domain quiet window. Exactly quietWindow since the latest activity is
// considered quiet.
func IsAcceptedVoiceDomainQuiet(groupIDs []int, now time.Time, quietWindow time.Duration) (time.Time, bool) {
	if now.IsZero() {
		now = time.Now()
	}
	if quietWindow < 0 {
		quietWindow = 0
	}
	acceptedVoiceActivity.RLock()
	latest := acceptedVoiceActivity.startedAt
	for _, groupID := range groupIDs {
		if acceptedAt := acceptedVoiceActivity.byGroup[groupID]; latest.Before(acceptedAt) {
			latest = acceptedAt
		}
	}
	acceptedVoiceActivity.RUnlock()
	if latest.IsZero() {
		return latest, true
	}
	return latest, !now.Before(latest.Add(quietWindow))
}
