package interconnect

import (
	"sync"
	"sync/atomic"
	"time"
)

const (
	SpeakerLeaseTTL         = 1200 * time.Millisecond
	SpeakerLeaseIdleTimeout = 900 * time.Millisecond
	SpeakerLeaseRenewBefore = 450 * time.Millisecond
	SpeakerClaimTimeout     = 300 * time.Millisecond
	speakerLeaseShardCount  = 64
)

type speakerLeaseOwner struct {
	nodeID           string
	controlSessionID uint64
	sessionID        uint64
	sessionEpoch     uint64
}

func (o speakerLeaseOwner) matches(other speakerLeaseOwner) bool {
	return o == other
}

type speakerLeaseState struct {
	owner       speakerLeaseOwner
	leaseID     uint64
	lastVoiceAt time.Time
	expiresAt   time.Time
}

type speakerLeaseShard struct {
	mu      sync.Mutex
	domains map[uint64]speakerLeaseState
}

// SpeakerLeaseManager is the centre-authoritative half-duplex arbiter. Its
// state is deliberately independent of route locks so realtime validation is
// a single short critical section per voice frame.
type SpeakerLeaseManager struct {
	shards [speakerLeaseShardCount]speakerLeaseShard
	nextID atomic.Uint64
}

func NewSpeakerLeaseManager() *SpeakerLeaseManager {
	manager := &SpeakerLeaseManager{}
	for index := range manager.shards {
		manager.shards[index].domains = make(map[uint64]speakerLeaseState)
	}
	return manager
}

func (m *SpeakerLeaseManager) shard(domainID uint64) *speakerLeaseShard {
	mixed := domainID ^ (domainID >> 32) ^ (domainID >> 16)
	return &m.shards[mixed&(speakerLeaseShardCount-1)]
}

func leaseOwner(nodeID string, controlSessionID, sessionID, sessionEpoch uint64) speakerLeaseOwner {
	return speakerLeaseOwner{nodeID: nodeID, controlSessionID: controlSessionID, sessionID: sessionID, sessionEpoch: sessionEpoch}
}

func (m *SpeakerLeaseManager) nextLeaseID() uint64 {
	for {
		if id := m.nextID.Add(1); id != 0 {
			return id
		}
	}
}

func speakerLeaseExpired(state speakerLeaseState, now time.Time) bool {
	return !now.Before(state.expiresAt) || now.Sub(state.lastVoiceAt) > SpeakerLeaseIdleTimeout
}

func (m *SpeakerLeaseManager) Claim(nodeID string, controlSessionID uint64, claim SpeakerLeaseControl, now time.Time) SpeakerLeaseControl {
	response := claim
	response.LeaseID = 0
	response.TTLMillis = 0
	response.RetryAfterMillis = 0
	if now.IsZero() {
		now = time.Now()
	}
	owner := leaseOwner(nodeID, controlSessionID, claim.SessionID, claim.SessionEpoch)

	shard := m.shard(claim.DomainID)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	state, exists := shard.domains[claim.DomainID]
	if exists && speakerLeaseExpired(state, now) {
		delete(shard.domains, claim.DomainID)
		exists = false
	}
	if exists && !state.owner.matches(owner) {
		wait := state.lastVoiceAt.Add(SpeakerLeaseIdleTimeout).Sub(now)
		if expiryWait := state.expiresAt.Sub(now); expiryWait < wait {
			wait = expiryWait
		}
		if wait < 0 {
			wait = 0
		}
		response.Action = SpeakerLeaseActionDeny
		response.RetryAfterMillis = wait.Milliseconds()
		return response
	}
	if exists && claim.LeaseID != 0 && claim.LeaseID != state.leaseID {
		response.Action = SpeakerLeaseActionDeny
		response.RetryAfterMillis = SpeakerLeaseRenewBefore.Milliseconds()
		return response
	}
	if !exists {
		state = speakerLeaseState{owner: owner, leaseID: m.nextLeaseID()}
	}
	state.lastVoiceAt = now
	state.expiresAt = now.Add(SpeakerLeaseTTL)
	shard.domains[claim.DomainID] = state
	response.Action = SpeakerLeaseActionGrant
	response.LeaseID = state.leaseID
	response.TTLMillis = SpeakerLeaseTTL.Milliseconds()
	return response
}

func (m *SpeakerLeaseManager) AcceptFrame(nodeID string, controlSessionID uint64, frame RelayFrame, now time.Time) bool {
	if frame.SpeakerLeaseID == 0 || frame.DomainID == 0 {
		return false
	}
	if now.IsZero() {
		now = time.Now()
	}
	owner := leaseOwner(nodeID, controlSessionID, frame.SessionID, frame.SessionEpoch)
	shard := m.shard(frame.DomainID)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	state, ok := shard.domains[frame.DomainID]
	if !ok || speakerLeaseExpired(state, now) || state.leaseID != frame.SpeakerLeaseID || !state.owner.matches(owner) {
		if ok && speakerLeaseExpired(state, now) {
			delete(shard.domains, frame.DomainID)
		}
		return false
	}
	state.lastVoiceAt = now
	state.expiresAt = now.Add(SpeakerLeaseTTL)
	shard.domains[frame.DomainID] = state
	return true
}

func (m *SpeakerLeaseManager) AcquireLocal(sessionID, sessionEpoch, domainID uint64, now time.Time) (uint64, bool) {
	claim := SpeakerLeaseControl{Action: SpeakerLeaseActionClaim, RequestID: 1, SessionID: sessionID, SessionEpoch: sessionEpoch, DomainID: domainID}
	response := m.Claim(CenterLocalNodeID, sessionID, claim, now)
	return response.LeaseID, response.Action == SpeakerLeaseActionGrant
}

func (m *SpeakerLeaseManager) CurrentLocal(sessionID, sessionEpoch, domainID uint64, now time.Time) (uint64, bool) {
	if now.IsZero() {
		now = time.Now()
	}
	owner := leaseOwner(CenterLocalNodeID, sessionID, sessionID, sessionEpoch)
	shard := m.shard(domainID)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	state, ok := shard.domains[domainID]
	if !ok || speakerLeaseExpired(state, now) || !state.owner.matches(owner) {
		if ok && speakerLeaseExpired(state, now) {
			delete(shard.domains, domainID)
		}
		return 0, false
	}
	state.lastVoiceAt = now
	state.expiresAt = now.Add(SpeakerLeaseTTL)
	shard.domains[domainID] = state
	return state.leaseID, true
}

func (m *SpeakerLeaseManager) Release(nodeID string, controlSessionID uint64, release SpeakerLeaseControl) bool {
	owner := leaseOwner(nodeID, controlSessionID, release.SessionID, release.SessionEpoch)
	shard := m.shard(release.DomainID)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	state, ok := shard.domains[release.DomainID]
	if !ok || state.leaseID != release.LeaseID || !state.owner.matches(owner) {
		return false
	}
	delete(shard.domains, release.DomainID)
	return true
}

func (m *SpeakerLeaseManager) ReleaseSession(sessionID, sessionEpoch uint64) int {
	if sessionID == 0 {
		return 0
	}
	released := 0
	for index := range m.shards {
		shard := &m.shards[index]
		shard.mu.Lock()
		for domainID, state := range shard.domains {
			if state.owner.sessionID == sessionID && (sessionEpoch == 0 || state.owner.sessionEpoch == sessionEpoch) {
				delete(shard.domains, domainID)
				released++
			}
		}
		shard.mu.Unlock()
	}
	return released
}

func (m *SpeakerLeaseManager) ReleaseNode(nodeID string, controlSessionID uint64) int {
	released := 0
	for index := range m.shards {
		shard := &m.shards[index]
		shard.mu.Lock()
		for domainID, state := range shard.domains {
			if state.owner.nodeID == nodeID && (controlSessionID == 0 || state.owner.controlSessionID == controlSessionID) {
				delete(shard.domains, domainID)
				released++
			}
		}
		shard.mu.Unlock()
	}
	return released
}
