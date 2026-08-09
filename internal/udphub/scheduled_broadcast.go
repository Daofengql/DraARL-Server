package udphub

import (
	"slices"
	"sync/atomic"
	"time"

	"draarl/internal/groupaccess"
)

type ScheduledBroadcastAcquireResult string

const (
	ScheduledBroadcastAcquired    ScheduledBroadcastAcquireResult = "acquired"
	ScheduledBroadcastRecentVoice ScheduledBroadcastAcquireResult = "recent_voice"
	ScheduledBroadcastDomainBusy  ScheduledBroadcastAcquireResult = "domain_busy"
	ScheduledBroadcastNoReceiver  ScheduledBroadcastAcquireResult = "no_receiver"
	ScheduledBroadcastInvalid     ScheduledBroadcastAcquireResult = "invalid_domain"
)

type ScheduledBroadcastLease struct {
	RunID          uint
	SourceGroupID  int
	DomainGroupIDs []int
	domainKey      string
	domainID       uint64
	speakerKey     uint64
	interconnect   bool
	receiverSnap   *domainReceiverSnap
	closed         atomic.Bool
}

func (lease *ScheduledBroadcastLease) DomainKey() string {
	if lease == nil {
		return ""
	}
	return lease.domainKey
}

func scheduledBroadcastSpeakerKey(runID uint) uint64 {
	return 0x9000000000000000 | (uint64(runID) & 0x0fffffffffffffff)
}

func GetActiveCommunicationDomainKey(sourceGroupID int) string {
	if GetActiveCommunicationDomainID(sourceGroupID) == 0 {
		return ""
	}
	return getHalfDuplexDomainKey(sourceGroupID)
}

// TryAcquireScheduledBroadcast performs a quiet check, reserves the current
// half-duplex domain, then checks activity again. A real voice accepted in the
// first check/acquire window either owns the arbiter or is observed by the
// second check, so the scheduled frame never wins that race.
func TryAcquireScheduledBroadcast(sourceGroupID int, runID uint, now time.Time, quietWindow time.Duration) (*ScheduledBroadcastLease, time.Time, ScheduledBroadcastAcquireResult) {
	if sourceGroupID <= 0 || runID == 0 {
		return nil, time.Time{}, ScheduledBroadcastInvalid
	}
	if now.IsZero() {
		now = time.Now()
	}
	groupIDs := GetActiveCommunicationGroupIDs(sourceGroupID)
	if len(groupIDs) == 0 {
		return nil, time.Time{}, ScheduledBroadcastInvalid
	}
	lastVoiceAt, quiet := IsAcceptedVoiceDomainQuiet(groupIDs, now, quietWindow)
	if !quiet {
		return nil, lastVoiceAt, ScheduledBroadcastRecentVoice
	}

	lease := &ScheduledBroadcastLease{
		RunID: runID, SourceGroupID: sourceGroupID, DomainGroupIDs: append([]int(nil), groupIDs...),
		domainKey: getHalfDuplexDomainKey(sourceGroupID), domainID: GetActiveCommunicationDomainID(sourceGroupID),
		speakerKey: scheduledBroadcastSpeakerKey(runID), interconnect: CenterInterconnectActive(),
	}
	if lease.domainKey == "" || lease.domainID == 0 {
		return nil, lastVoiceAt, ScheduledBroadcastInvalid
	}
	if !acquireScheduledBroadcastArbiter(lease, now) {
		return nil, lastVoiceAt, ScheduledBroadcastDomainBusy
	}
	lastVoiceAt, quiet = IsAcceptedVoiceDomainQuiet(groupIDs, now, quietWindow)
	if !quiet {
		ReleaseScheduledBroadcast(lease)
		return nil, lastVoiceAt, ScheduledBroadcastRecentVoice
	}
	// Receiver membership is frozen only after the domain lease has been
	// acquired. Later topology or session changes cannot expand this run.
	lease.receiverSnap = getDomainReceiverSnap(sourceGroupID)
	if !scheduledBroadcastHasReceiver(lease) {
		ReleaseScheduledBroadcast(lease)
		return nil, lastVoiceAt, ScheduledBroadcastNoReceiver
	}
	return lease, lastVoiceAt, ScheduledBroadcastAcquired
}

func scheduledBroadcastHasReceiver(lease *ScheduledBroadcastLease) bool {
	if lease == nil {
		return false
	}
	if lease.receiverSnap != nil && len(lease.receiverSnap.entries) != 0 {
		return true
	}
	if GlobalMessageRouter != nil && GlobalMessageRouter.wsManager != nil {
		for _, groupID := range lease.DomainGroupIDs {
			for _, device := range GlobalMessageRouter.wsManager.GetDevicesByGroup(groupID) {
				if device == nil {
					continue
				}
				rxGroupIDs := device.GetRxGroupIDs()
				if len(rxGroupIDs) == 0 {
					rxGroupIDs = []int{device.GetGroupID()}
				}
				if groupaccess.CanReceiveRoute(device.IsDisabledRecv(), rxGroupIDs, groupID) &&
					!CenterIdentityOwnedByRemote(device.GetUserID(), device.GetSSID()) {
					return true
				}
			}
		}
	}
	if lease.interconnect {
		hooks := centerHooks()
		return hooks.HasBroadcastReceiver != nil && hooks.HasBroadcastReceiver(lease.domainID)
	}
	return false
}

func acquireScheduledBroadcastArbiter(lease *ScheduledBroadcastLease, now time.Time) bool {
	if lease.interconnect {
		hooks := centerHooks()
		return hooks.AcquireBroadcast != nil && hooks.AcquireBroadcast(lease.RunID, lease.domainID, now)
	}
	shard := &halfDuplexShards[halfDuplexShardIndex(lease.domainKey)]
	shard.mu.Lock()
	defer shard.mu.Unlock()
	state, exists := shard.states[lease.domainKey]
	if exists && now.Sub(state.lastVoiceAt) > halfDuplexVoiceHoldTimeout {
		delete(shard.states, lease.domainKey)
		exists = false
	}
	if exists && state.speaker.key != lease.speakerKey {
		return false
	}
	if !exists {
		state = &halfDuplexDomainState{speaker: halfDuplexSpeaker{key: lease.speakerKey, labelBase: "system-broadcast"}}
		shard.states[lease.domainKey] = state
	}
	state.lastVoiceAt = now
	return true
}

// AcceptScheduledBroadcastFrame renews the exact lease and records activity
// for its fixed delivery snapshot. A topology change invalidates the lease
// before the next frame can be accepted.
func AcceptScheduledBroadcastFrame(lease *ScheduledBroadcastLease, acceptedAt time.Time) bool {
	if lease == nil || lease.closed.Load() || !scheduledBroadcastTopologyMatches(lease) {
		return false
	}
	if acceptedAt.IsZero() {
		acceptedAt = time.Now()
	}
	if lease.interconnect {
		hooks := centerHooks()
		if hooks.AcceptBroadcastFrame == nil || !hooks.AcceptBroadcastFrame(lease.RunID, lease.domainID, acceptedAt) {
			return false
		}
	} else {
		shard := &halfDuplexShards[halfDuplexShardIndex(lease.domainKey)]
		shard.mu.Lock()
		state, ok := shard.states[lease.domainKey]
		if !ok || state.speaker.key != lease.speakerKey || acceptedAt.Sub(state.lastVoiceAt) > halfDuplexVoiceHoldTimeout {
			if ok && acceptedAt.Sub(state.lastVoiceAt) > halfDuplexVoiceHoldTimeout {
				delete(shard.states, lease.domainKey)
			}
			shard.mu.Unlock()
			return false
		}
		state.lastVoiceAt = acceptedAt
		shard.mu.Unlock()
	}
	MarkAcceptedVoiceGroups(lease.DomainGroupIDs, acceptedAt)
	return true
}

func scheduledBroadcastTopologyMatches(lease *ScheduledBroadcastLease) bool {
	if lease == nil || lease.RunID == 0 || lease.SourceGroupID <= 0 || CenterInterconnectActive() != lease.interconnect {
		return false
	}
	currentGroups := GetActiveCommunicationGroupIDs(lease.SourceGroupID)
	return slices.Equal(currentGroups, lease.DomainGroupIDs) && getHalfDuplexDomainKey(lease.SourceGroupID) == lease.domainKey && GetActiveCommunicationDomainID(lease.SourceGroupID) == lease.domainID
}

func ReleaseScheduledBroadcast(lease *ScheduledBroadcastLease) {
	if lease == nil || !lease.closed.CompareAndSwap(false, true) {
		return
	}
	releaseScheduledBroadcastArbiter(lease)
}

// FinishScheduledBroadcast closes the source without clearing the arbiter.
// The existing 900ms voice hold then applies after the final accepted packet,
// just as it does for a real speaker. Cancellation uses Release instead.
func FinishScheduledBroadcast(lease *ScheduledBroadcastLease) {
	if lease != nil {
		lease.closed.CompareAndSwap(false, true)
	}
}

func releaseScheduledBroadcastArbiter(lease *ScheduledBroadcastLease) {
	if lease.interconnect {
		hooks := centerHooks()
		if hooks.ReleaseBroadcast != nil {
			hooks.ReleaseBroadcast(lease.RunID, lease.domainID)
		}
		return
	}
	shard := &halfDuplexShards[halfDuplexShardIndex(lease.domainKey)]
	shard.mu.Lock()
	if state, ok := shard.states[lease.domainKey]; ok && state.speaker.key == lease.speakerKey {
		delete(shard.states, lease.domainKey)
	}
	shard.mu.Unlock()
}
