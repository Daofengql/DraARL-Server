package udphub

import (
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// 语音占用保持时长：超过该时长未收到占用者语音包则自动释放发言权。
	halfDuplexVoiceHoldTimeout = 900 * time.Millisecond
	// 限制阻塞日志频率，避免并发争抢时刷屏。
	halfDuplexBlockLogInterval = 2 * time.Second
	// 半双工状态分片数，降低多群并发时的全局锁竞争。
	halfDuplexShardCount = 32
)

type halfDuplexDomainState struct {
	speaker        halfDuplexSpeaker
	lastVoiceAt    time.Time
	lastBlockLogAt time.Time
}

type halfDuplexSpeaker struct {
	key       uint64
	labelBase string
	ssid      byte
}

func (s halfDuplexSpeaker) label() string {
	base := s.labelBase
	if base == "" {
		base = "unknown"
	}
	return base + "-" + strconv.Itoa(int(s.ssid))
}

type halfDuplexShard struct {
	mu     sync.Mutex
	states map[string]*halfDuplexDomainState
}

var (
	halfDuplexShards [halfDuplexShardCount]halfDuplexShard
	// key: groupID，value: domainKey（缓存群组到转发域映射，避免每包都做图遍历）
	halfDuplexDomainKeyCache sync.Map
	// key: domainKey，value: []int 连通域内群组 ID 快照（用于一帧多组转发）
	halfDuplexDomainGroupsCache sync.Map
)

func init() {
	for i := 0; i < halfDuplexShardCount; i++ {
		halfDuplexShards[i].states = make(map[string]*halfDuplexDomainState)
	}
}

func halfDuplexShardIndex(domainKey string) int {
	return int(fnv32String(domainKey) & (halfDuplexShardCount - 1))
}

// tryAcquireHalfDuplex 严格半双工仲裁：
// 同一转发域内同一时刻仅允许一个说话人发包，其他说话人语音包会被丢弃。
func tryAcquireHalfDuplex(groupID int, speaker halfDuplexSpeaker, ts time.Time) bool {
	if groupID <= 0 || speaker.key == 0 {
		return true
	}
	if ts.IsZero() {
		ts = time.Now()
	}

	domainKey := getHalfDuplexDomainKey(groupID)
	shard := &halfDuplexShards[halfDuplexShardIndex(domainKey)]

	shard.mu.Lock()
	defer shard.mu.Unlock()

	state, exists := shard.states[domainKey]
	if !exists {
		shard.states[domainKey] = &halfDuplexDomainState{
			speaker:     speaker,
			lastVoiceAt: ts,
		}
		return true
	}

	// 当前占用者续期
	if state.speaker.key == speaker.key {
		state.lastVoiceAt = ts
		return true
	}

	// 占用者超时，移交发言权
	if ts.Sub(state.lastVoiceAt) > halfDuplexVoiceHoldTimeout {
		state.speaker = speaker
		state.lastVoiceAt = ts
		state.lastBlockLogAt = time.Time{}
		return true
	}

	// 被阻塞：当前有其他说话人占用发言权
	if ts.Sub(state.lastBlockLogAt) > halfDuplexBlockLogInterval {
		log.Printf("[HALF_DUPLEX] blocked speaker=%s domain=%s active=%s",
			speaker.label(), domainKey, state.speaker.label())
		state.lastBlockLogAt = ts
	}
	return false
}

// resetHalfDuplexDomainCache 刷新群组互联关系后清理域缓存。
// 注意：不主动清空活跃占用状态，避免定时刷新期间中断正在进行的发言。
func resetHalfDuplexDomainCache() {
	halfDuplexDomainKeyCache = sync.Map{}
	halfDuplexDomainGroupsCache = sync.Map{}
	resetDomainGroupReverseCache()

	// 仅回收明显过期的占用状态，防止状态表长期增长。
	expireBefore := time.Now().Add(-3 * halfDuplexVoiceHoldTimeout)
	for i := 0; i < halfDuplexShardCount; i++ {
		shard := &halfDuplexShards[i]
		shard.mu.Lock()
		for domainKey, state := range shard.states {
			if state.lastVoiceAt.Before(expireBefore) {
				delete(shard.states, domainKey)
			}
		}
		shard.mu.Unlock()
	}
}

func getHalfDuplexDomainKey(groupID int) string {
	if cached, ok := halfDuplexDomainKeyCache.Load(groupID); ok {
		return cached.(string)
	}

	ids := collectHalfDuplexDomainGroupIDs(groupID)
	sort.Ints(ids)
	domainKey := encodeHalfDuplexDomainKey(ids)

	// 将同一连通域的 groupID 都映射到同一个 key，后续可直接命中缓存。
	for _, id := range ids {
		halfDuplexDomainKeyCache.Store(id, domainKey)
	}
	// 缓存连通域群组列表，供转发路径一次取全量目标组。
	halfDuplexDomainGroupsCache.Store(domainKey, append([]int(nil), ids...))
	return domainKey
}

// GetHalfDuplexDomainGroupIDs 返回 groupID 所在连通域的全部群组 ID（已排序快照）。
func GetHalfDuplexDomainGroupIDs(groupID int) []int {
	domainKey := getHalfDuplexDomainKey(groupID)
	if v, ok := halfDuplexDomainGroupsCache.Load(domainKey); ok {
		if ids, ok := v.([]int); ok {
			return ids
		}
	}
	ids := collectHalfDuplexDomainGroupIDs(groupID)
	sort.Ints(ids)
	halfDuplexDomainGroupsCache.Store(domainKey, append([]int(nil), ids...))
	return ids
}

// GetCommunicationDomainID returns a stable opaque ID for the current linked
// group domain. It is shared with Type 0 grants so centre and edge make the
// same local/remote routing decision without sharing database state.
func GetCommunicationDomainID(groupID int) uint64 {
	key := getHalfDuplexDomainKey(groupID)
	if key == "" {
		return 0
	}
	return uint64(fnv32String(key))
}

// GetActiveCommunicationDomainID also applies the authoritative group enabled
// state. Disabled, missing, ungrouped and virtual groups never form a live
// forwarding domain on an edge.
func GetActiveCommunicationDomainID(groupID int) uint64 {
	if groupID <= 0 {
		return 0
	}
	group, ok := GetGroupFromCache(groupID)
	if !ok || group == nil || group.Status != 1 || group.IsVirtual {
		return 0
	}
	return GetCommunicationDomainID(groupID)
}

// collectHalfDuplexDomainGroupIDs 计算一个群组可达的“语音转发连通域”。
// 同一连通域内应共享半双工占用状态，不同域互不影响。
func collectHalfDuplexDomainGroupIDs(groupID int) []int {
	visited := make(map[int]struct{}, 8)
	queue := []int{groupID}
	visited[groupID] = struct{}{}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		peers := GetLinkedTargetGroups(current)
		for _, peerID := range peers {
			if _, ok := visited[peerID]; ok {
				continue
			}
			visited[peerID] = struct{}{}
			queue = append(queue, peerID)
		}
	}

	ids := make([]int, 0, len(visited))
	for id := range visited {
		ids = append(ids, id)
	}
	return ids
}

func encodeHalfDuplexDomainKey(ids []int) string {
	if len(ids) == 0 {
		return ""
	}
	if len(ids) == 1 {
		return strconv.Itoa(ids[0])
	}

	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, strconv.Itoa(id))
	}
	return strings.Join(parts, ",")
}
