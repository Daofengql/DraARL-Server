package interconnect

import (
	"net"
	"sync"
	"time"
)

const maxTrackedHandshakeIPs = 4096

type handshakeAttempt struct {
	minute   int64
	attempts int
}

type handshakeLimiter struct {
	mu            sync.Mutex
	limit         int
	entries       map[string]handshakeAttempt
	order         []string
	orderIndex    map[string]int
	cleanupCursor int
}

func newHandshakeLimiter(limit int) *handshakeLimiter {
	return &handshakeLimiter{limit: limit, entries: make(map[string]handshakeAttempt), orderIndex: make(map[string]int)}
}

// allow 记录并检查握手限速。
// 【NAT 修复】限速桶按 IP+NodeID 组合键，避免 NAT 后共享出口 IP 的多节点
// 被单个异常节点（或攻击流量）连带限速；无 NodeID（未解析出）时回退纯 IP。
func (l *handshakeLimiter) allow(addr net.Addr, nodeID string, now time.Time) bool {
	if l == nil || l.limit <= 0 {
		return false
	}
	host := "unknown"
	if addr != nil {
		if parsed, _, err := net.SplitHostPort(addr.String()); err == nil && parsed != "" {
			host = parsed
		} else if addr.String() != "" {
			host = addr.String()
		}
	}
	if nodeID != "" {
		host = host + "|" + nodeID
	}
	minute := now.Unix() / 60
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, exists := l.entries[host]
	if entry.minute != minute {
		entry = handshakeAttempt{minute: minute}
	}
	if entry.attempts >= l.limit {
		return false
	}
	if len(l.entries) >= maxTrackedHandshakeIPs {
		l.pruneStaleLocked(minute, maxTrackedHandshakeIPs)
		if len(l.entries) >= maxTrackedHandshakeIPs {
			if _, exists := l.entries[host]; !exists {
				return false
			}
		}
	}
	if _, stillExists := l.entries[host]; !stillExists {
		exists = false
	}
	entry.attempts++
	if !exists {
		l.orderIndex[host] = len(l.order)
		l.order = append(l.order, host)
	}
	l.entries[host] = entry
	return true
}

// deleteEntryLocked removes a key from the ordered cleanup index in O(1).
// The caller must hold l.mu.
func (l *handshakeLimiter) deleteEntryLocked(key string) {
	delete(l.entries, key)
	idx, ok := l.orderIndex[key]
	if !ok {
		return
	}
	last := len(l.order) - 1
	if idx != last {
		lastKey := l.order[last]
		l.order[idx] = lastKey
		l.orderIndex[lastKey] = idx
	}
	l.order = l.order[:last]
	delete(l.orderIndex, key)
	if l.cleanupCursor > idx {
		l.cleanupCursor--
	}
	if l.cleanupCursor >= len(l.order) {
		l.cleanupCursor = 0
	}
}

// pruneStaleLocked walks a bounded, persistent cursor. This avoids relying on
// Go's randomized map iteration when the table is full and old minute buckets
// must be reclaimed. The caller must hold l.mu.
func (l *handshakeLimiter) pruneStaleLocked(minute int64, max int) {
	for scanned := 0; scanned < max && len(l.order) > 0; {
		if l.cleanupCursor >= len(l.order) {
			l.cleanupCursor = 0
		}
		key := l.order[l.cleanupCursor]
		entry, exists := l.entries[key]
		if !exists {
			l.deleteEntryLocked(key)
			continue
		}
		scanned++
		if entry.minute != minute {
			l.deleteEntryLocked(key)
			continue
		}
		l.cleanupCursor++
	}
}
