package interconnect

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultMaxNodes                    = 256
	defaultMaxPendingHandshakes        = 64
	defaultAuthAttemptsPerMinutePerIP  = 30
	defaultDataSoftPPSPerNode          = 50_000
	defaultDataHardPPSPerNode          = 100_000
	defaultDataHardMbpsPerNode         = 1_000
	defaultDataQueuePerNode            = 512
	defaultDataQueueGlobal             = 4_096
	defaultDataMaxQueueAge             = 200 * time.Millisecond
	defaultControlSoftPPSPerNode       = 1_000
	defaultControlHardPPSPerNode       = 2_000
	defaultControlHardMbpsPerNode      = 256
	defaultDeviceAuthPPSPerNode        = 500
	defaultMaxDeviceSessionsPerNode    = 25_000
	maxConfiguredNodeWorkers           = 64
	maxConfiguredNodeQueue             = 65_536
	maxConfiguredNodeSessions          = 1_000_000
	maxConfiguredNodeRate              = 10_000_000
	maxConfiguredNodeBandwidthMbps     = 100_000
	maxConfiguredPendingNodeHandshakes = 4_096
)

// ResourceLimits protects each authenticated node independently. Soft limits
// are observable only; hard limits reject work before it can occupy shared
// queues or expensive business handlers.
type ResourceLimits struct {
	MaxNodes                   int
	MaxPendingHandshakes       int
	AuthAttemptsPerMinutePerIP int
	DataSoftPPSPerNode         int
	DataHardPPSPerNode         int
	DataHardMbpsPerNode        int
	DataQueuePerNode           int
	DataQueueGlobal            int
	DataWorkers                int
	DataMaxQueueAge            time.Duration
	ControlSoftPPSPerNode      int
	ControlHardPPSPerNode      int
	ControlHardMbpsPerNode     int
	DeviceAuthPPSPerNode       int
	MaxDeviceSessionsPerNode   int
}

func (limits ResourceLimits) normalized() (ResourceLimits, error) {
	if limits.MaxNodes == 0 {
		limits.MaxNodes = defaultMaxNodes
	}
	if limits.MaxPendingHandshakes == 0 {
		limits.MaxPendingHandshakes = defaultMaxPendingHandshakes
	}
	if limits.AuthAttemptsPerMinutePerIP == 0 {
		limits.AuthAttemptsPerMinutePerIP = defaultAuthAttemptsPerMinutePerIP
	}
	if limits.DataSoftPPSPerNode == 0 {
		limits.DataSoftPPSPerNode = defaultDataSoftPPSPerNode
	}
	if limits.DataHardPPSPerNode == 0 {
		limits.DataHardPPSPerNode = defaultDataHardPPSPerNode
	}
	if limits.DataHardMbpsPerNode == 0 {
		limits.DataHardMbpsPerNode = defaultDataHardMbpsPerNode
	}
	if limits.DataQueuePerNode == 0 {
		limits.DataQueuePerNode = defaultDataQueuePerNode
	}
	if limits.DataQueueGlobal == 0 {
		limits.DataQueueGlobal = defaultDataQueueGlobal
	}
	if limits.DataWorkers == 0 {
		limits.DataWorkers = runtime.GOMAXPROCS(0)
		if limits.DataWorkers < 2 {
			limits.DataWorkers = 2
		}
		if limits.DataWorkers > 16 {
			limits.DataWorkers = 16
		}
	}
	if limits.DataMaxQueueAge == 0 {
		limits.DataMaxQueueAge = defaultDataMaxQueueAge
	}
	if limits.ControlSoftPPSPerNode == 0 {
		limits.ControlSoftPPSPerNode = defaultControlSoftPPSPerNode
	}
	if limits.ControlHardPPSPerNode == 0 {
		limits.ControlHardPPSPerNode = defaultControlHardPPSPerNode
	}
	if limits.ControlHardMbpsPerNode == 0 {
		limits.ControlHardMbpsPerNode = defaultControlHardMbpsPerNode
	}
	if limits.DeviceAuthPPSPerNode == 0 {
		limits.DeviceAuthPPSPerNode = defaultDeviceAuthPPSPerNode
	}
	if limits.MaxDeviceSessionsPerNode == 0 {
		limits.MaxDeviceSessionsPerNode = defaultMaxDeviceSessionsPerNode
	}

	if limits.MaxNodes < 1 || limits.MaxNodes > maxConfiguredNodeSessions {
		return limits, errors.New("MaxNodes is outside the supported range")
	}
	if limits.MaxPendingHandshakes < 1 || limits.MaxPendingHandshakes > maxConfiguredPendingNodeHandshakes {
		return limits, errors.New("MaxPendingHandshakes is outside the supported range")
	}
	if limits.AuthAttemptsPerMinutePerIP < 1 || limits.AuthAttemptsPerMinutePerIP > maxConfiguredNodeRate {
		return limits, errors.New("AuthAttemptsPerMinutePerIP is outside the supported range")
	}
	if limits.DataSoftPPSPerNode < 1 || limits.DataHardPPSPerNode < limits.DataSoftPPSPerNode || limits.DataHardPPSPerNode > maxConfiguredNodeRate {
		return limits, errors.New("data PPS limits are invalid")
	}
	if limits.DataHardMbpsPerNode < 1 || limits.DataHardMbpsPerNode > maxConfiguredNodeBandwidthMbps {
		return limits, errors.New("DataHardMbpsPerNode is outside the supported range")
	}
	if limits.DataQueueGlobal < 1 || limits.DataQueueGlobal > maxConfiguredNodeQueue || limits.DataQueuePerNode < 1 || limits.DataQueuePerNode > limits.DataQueueGlobal {
		return limits, errors.New("node data queue limits are invalid")
	}
	if limits.DataWorkers < 1 || limits.DataWorkers > maxConfiguredNodeWorkers {
		return limits, errors.New("DataWorkers is outside the supported range")
	}
	if limits.DataMaxQueueAge < time.Millisecond || limits.DataMaxQueueAge > 10*time.Second {
		return limits, errors.New("DataMaxQueueAge is outside the supported range")
	}
	if limits.ControlSoftPPSPerNode < 1 || limits.ControlHardPPSPerNode < limits.ControlSoftPPSPerNode || limits.ControlHardPPSPerNode > maxConfiguredNodeRate {
		return limits, errors.New("control PPS limits are invalid")
	}
	if limits.ControlHardMbpsPerNode < 1 || limits.ControlHardMbpsPerNode > maxConfiguredNodeBandwidthMbps {
		return limits, errors.New("ControlHardMbpsPerNode is outside the supported range")
	}
	if limits.DeviceAuthPPSPerNode < 1 || limits.DeviceAuthPPSPerNode > limits.ControlHardPPSPerNode {
		return limits, errors.New("DeviceAuthPPSPerNode is outside the supported range")
	}
	if limits.MaxDeviceSessionsPerNode < 1 || limits.MaxDeviceSessionsPerNode > maxConfiguredNodeSessions {
		return limits, errors.New("MaxDeviceSessionsPerNode is outside the supported range")
	}
	return limits, nil
}

func DefaultResourceLimits() ResourceLimits {
	limits, _ := (ResourceLimits{}).normalized()
	return limits
}

func megabitsPerSecondBytes(value int) int64 {
	return int64(value) * 1_000_000 / 8
}

type NodeProtectionSnapshot struct {
	DataSoftLimitEvents    uint64 `json:"data_soft_limit_events"`
	DataHardLimitDrops     uint64 `json:"data_hard_limit_drops"`
	DataQueueDrops         uint64 `json:"data_queue_drops"`
	DataStaleDrops         uint64 `json:"data_stale_drops"`
	ControlSoftLimitEvents uint64 `json:"control_soft_limit_events"`
	ControlHardLimitDrops  uint64 `json:"control_hard_limit_drops"`
	DeviceAuthLimitDrops   uint64 `json:"device_auth_limit_drops"`
	SessionLimitRejects    uint64 `json:"session_limit_rejects"`
	InvalidAuthTags        uint64 `json:"invalid_auth_tags"`
	IdentityRejects        uint64 `json:"identity_rejects"`
	ExpiredDrops           uint64 `json:"expired_drops"`
	ReplayDrops            uint64 `json:"replay_drops"`
	UnboundAddressDrops    uint64 `json:"unbound_address_drops"`
	DataBindRejects        uint64 `json:"data_bind_rejects"`
	QueuedData             int64  `json:"queued_data"`
}

type nodeResourceWindow struct {
	second              int64
	dataPackets         int
	dataBytes           int64
	controlPackets      int
	controlBytes        int64
	deviceAuthRequests  int
	dataSoftReported    bool
	controlSoftReported bool
}

type nodeProtection struct {
	limits ResourceLimits
	mu     sync.Mutex
	window nodeResourceWindow
	queued atomic.Int64

	dataSoftLimitEvents    atomic.Uint64
	dataHardLimitDrops     atomic.Uint64
	dataQueueDrops         atomic.Uint64
	dataStaleDrops         atomic.Uint64
	controlSoftLimitEvents atomic.Uint64
	controlHardLimitDrops  atomic.Uint64
	deviceAuthLimitDrops   atomic.Uint64
	sessionLimitRejects    atomic.Uint64
	invalidAuthTags        atomic.Uint64
	identityRejects        atomic.Uint64
	expiredDrops           atomic.Uint64
	replayDrops            atomic.Uint64
	unboundAddressDrops    atomic.Uint64
	dataBindRejects        atomic.Uint64
}

func newNodeProtection(limits ResourceLimits) *nodeProtection {
	normalized, err := limits.normalized()
	if err != nil {
		normalized = DefaultResourceLimits()
	}
	return &nodeProtection{limits: normalized}
}

func (p *nodeProtection) resetWindowLocked(now time.Time) {
	second := now.Unix()
	if p.window.second == second {
		return
	}
	p.window = nodeResourceWindow{second: second}
}

func (p *nodeProtection) allowData(size int, now time.Time) bool {
	if p == nil || size < 0 {
		return false
	}
	p.mu.Lock()
	p.resetWindowLocked(now)
	nextPackets := p.window.dataPackets + 1
	nextBytes := p.window.dataBytes + int64(size)
	if nextPackets > p.limits.DataHardPPSPerNode || nextBytes > megabitsPerSecondBytes(p.limits.DataHardMbpsPerNode) {
		p.mu.Unlock()
		p.dataHardLimitDrops.Add(1)
		return false
	}
	p.window.dataPackets, p.window.dataBytes = nextPackets, nextBytes
	soft := nextPackets > p.limits.DataSoftPPSPerNode && !p.window.dataSoftReported
	if soft {
		p.window.dataSoftReported = true
	}
	p.mu.Unlock()
	if soft {
		p.dataSoftLimitEvents.Add(1)
	}
	return true
}

func (p *nodeProtection) allowControl(size int, now time.Time) bool {
	if p == nil || size < 0 {
		return false
	}
	p.mu.Lock()
	p.resetWindowLocked(now)
	nextPackets := p.window.controlPackets + 1
	nextBytes := p.window.controlBytes + int64(size)
	if nextPackets > p.limits.ControlHardPPSPerNode || nextBytes > megabitsPerSecondBytes(p.limits.ControlHardMbpsPerNode) {
		p.mu.Unlock()
		p.controlHardLimitDrops.Add(1)
		return false
	}
	p.window.controlPackets, p.window.controlBytes = nextPackets, nextBytes
	soft := nextPackets > p.limits.ControlSoftPPSPerNode && !p.window.controlSoftReported
	if soft {
		p.window.controlSoftReported = true
	}
	p.mu.Unlock()
	if soft {
		p.controlSoftLimitEvents.Add(1)
	}
	return true
}

func (p *nodeProtection) allowDeviceAuth(now time.Time) bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	p.resetWindowLocked(now)
	if p.window.deviceAuthRequests >= p.limits.DeviceAuthPPSPerNode {
		p.mu.Unlock()
		p.deviceAuthLimitDrops.Add(1)
		return false
	}
	p.window.deviceAuthRequests++
	p.mu.Unlock()
	return true
}

func (p *nodeProtection) reserveQueue() bool {
	if p == nil {
		return false
	}
	for {
		queued := p.queued.Load()
		if queued >= int64(p.limits.DataQueuePerNode) {
			p.dataQueueDrops.Add(1)
			return false
		}
		if p.queued.CompareAndSwap(queued, queued+1) {
			return true
		}
	}
}

func (p *nodeProtection) releaseQueue() {
	if p != nil {
		p.queued.Add(-1)
	}
}

func (p *nodeProtection) recordQueueDrop() {
	if p != nil {
		p.dataQueueDrops.Add(1)
	}
}

func (p *nodeProtection) recordStaleDrop() {
	if p != nil {
		p.dataStaleDrops.Add(1)
	}
}

func (p *nodeProtection) recordSessionLimitReject() {
	if p != nil {
		p.sessionLimitRejects.Add(1)
	}
}

func (p *nodeProtection) recordInvalidAuthTag() {
	if p != nil {
		p.invalidAuthTags.Add(1)
	}
}

func (p *nodeProtection) recordIdentityReject() {
	if p != nil {
		p.identityRejects.Add(1)
	}
}

func (p *nodeProtection) recordExpiredDrop() {
	if p != nil {
		p.expiredDrops.Add(1)
	}
}

func (p *nodeProtection) recordReplayDrop() {
	if p != nil {
		p.replayDrops.Add(1)
	}
}

func (p *nodeProtection) recordUnboundAddressDrop() {
	if p != nil {
		p.unboundAddressDrops.Add(1)
	}
}

func (p *nodeProtection) recordDataBindReject() {
	if p != nil {
		p.dataBindRejects.Add(1)
	}
}

func (p *nodeProtection) snapshot() NodeProtectionSnapshot {
	if p == nil {
		return NodeProtectionSnapshot{}
	}
	return NodeProtectionSnapshot{
		DataSoftLimitEvents: p.dataSoftLimitEvents.Load(), DataHardLimitDrops: p.dataHardLimitDrops.Load(),
		DataQueueDrops: p.dataQueueDrops.Load(), DataStaleDrops: p.dataStaleDrops.Load(),
		ControlSoftLimitEvents: p.controlSoftLimitEvents.Load(), ControlHardLimitDrops: p.controlHardLimitDrops.Load(),
		DeviceAuthLimitDrops: p.deviceAuthLimitDrops.Load(), SessionLimitRejects: p.sessionLimitRejects.Load(),
		InvalidAuthTags: p.invalidAuthTags.Load(), IdentityRejects: p.identityRejects.Load(),
		ExpiredDrops: p.expiredDrops.Load(), ReplayDrops: p.replayDrops.Load(),
		UnboundAddressDrops: p.unboundAddressDrops.Load(), DataBindRejects: p.dataBindRejects.Load(),
		QueuedData: p.queued.Load(),
	}
}
