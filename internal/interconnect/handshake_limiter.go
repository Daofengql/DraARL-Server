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
	mu      sync.Mutex
	limit   int
	entries map[string]handshakeAttempt
}

func newHandshakeLimiter(limit int) *handshakeLimiter {
	return &handshakeLimiter{limit: limit, entries: make(map[string]handshakeAttempt)}
}

func (l *handshakeLimiter) allow(addr net.Addr, now time.Time) bool {
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
	minute := now.Unix() / 60
	l.mu.Lock()
	defer l.mu.Unlock()
	entry := l.entries[host]
	if entry.minute != minute {
		entry = handshakeAttempt{minute: minute}
	}
	if entry.attempts >= l.limit {
		return false
	}
	if len(l.entries) >= maxTrackedHandshakeIPs {
		for key, existing := range l.entries {
			if existing.minute != minute {
				delete(l.entries, key)
			}
		}
		if len(l.entries) >= maxTrackedHandshakeIPs {
			if _, exists := l.entries[host]; !exists {
				return false
			}
		}
	}
	entry.attempts++
	l.entries[host] = entry
	return true
}
