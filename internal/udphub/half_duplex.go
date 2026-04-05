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
)

type halfDuplexDomainState struct {
	speakerID      string
	speakerLabel   string
	lastVoiceAt    time.Time
	lastBlockLogAt time.Time
}

var (
	halfDuplexMu sync.Mutex
	// key: domainKey（同一转发域），value: 当前占用者状态
	halfDuplexDomainStates = make(map[string]*halfDuplexDomainState)
	// key: groupID，value: domainKey（缓存群组到转发域映射，避免每包都做图遍历）
	halfDuplexDomainKeyCache sync.Map
)

// tryAcquireHalfDuplex 严格半双工仲裁：
// 同一转发域内同一时刻仅允许一个说话人发包，其他说话人语音包会被丢弃。
func tryAcquireHalfDuplex(groupID int, speakerID, speakerLabel string, ts time.Time) bool {
	if groupID <= 0 || speakerID == "" {
		return true
	}
	if ts.IsZero() {
		ts = time.Now()
	}

	domainKey := getHalfDuplexDomainKey(groupID)

	halfDuplexMu.Lock()
	defer halfDuplexMu.Unlock()

	state, exists := halfDuplexDomainStates[domainKey]
	if !exists {
		halfDuplexDomainStates[domainKey] = &halfDuplexDomainState{
			speakerID:    speakerID,
			speakerLabel: speakerLabel,
			lastVoiceAt:  ts,
		}
		return true
	}

	// 当前占用者续期
	if state.speakerID == speakerID {
		state.lastVoiceAt = ts
		state.speakerLabel = speakerLabel
		return true
	}

	// 占用者超时，移交发言权
	if ts.Sub(state.lastVoiceAt) > halfDuplexVoiceHoldTimeout {
		state.speakerID = speakerID
		state.speakerLabel = speakerLabel
		state.lastVoiceAt = ts
		state.lastBlockLogAt = time.Time{}
		return true
	}

	// 被阻塞：当前有其他说话人占用发言权
	if ts.Sub(state.lastBlockLogAt) > halfDuplexBlockLogInterval {
		log.Printf("[HALF_DUPLEX] blocked speaker=%s domain=%s active=%s",
			speakerLabel, domainKey, state.speakerLabel)
		state.lastBlockLogAt = ts
	}
	return false
}

// resetHalfDuplexDomainCache 刷新群组互联关系后清理域缓存。
// 注意：不主动清空活跃占用状态，避免定时刷新期间中断正在进行的发言。
func resetHalfDuplexDomainCache() {
	halfDuplexDomainKeyCache = sync.Map{}

	// 仅回收明显过期的占用状态，防止状态表长期增长。
	expireBefore := time.Now().Add(-3 * halfDuplexVoiceHoldTimeout)
	halfDuplexMu.Lock()
	for domainKey, state := range halfDuplexDomainStates {
		if state.lastVoiceAt.Before(expireBefore) {
			delete(halfDuplexDomainStates, domainKey)
		}
	}
	halfDuplexMu.Unlock()
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
	return domainKey
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
