package email

import (
	"strconv"
	"sync"
	"testing"
	"time"
)

func newTestVerificationManager() *VerificationManager {
	return &VerificationManager{
		ipRateLimit: make(map[string][]time.Time),
		maxIPPerMin: 5,
	}
}

func TestAllowIPSendIsAtomicUnderConcurrency(t *testing.T) {
	mgr := newTestVerificationManager()
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _, _ := mgr.AllowIPSend(" 192.0.2.77 "); ok {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if allowed != mgr.maxIPPerMin {
		t.Fatalf("concurrent allowed=%d, want exactly %d", allowed, mgr.maxIPPerMin)
	}
}

func TestAllowIPSendRejectsNewSourcesWhenBoundedTableIsFull(t *testing.T) {
	mgr := newTestVerificationManager()
	mgr.ipRateLimit = make(map[string][]time.Time, maxVerificationIPRateEntries)
	now := time.Now()
	for i := 0; i < maxVerificationIPRateEntries; i++ {
		mgr.ipRateLimit["198.51.100."+strconv.Itoa(i)] = []time.Time{now}
	}
	if ok, _, _ := mgr.AllowIPSend("203.0.113.77"); ok {
		t.Fatal("new source should be rejected when IP table is full")
	}
}

func TestAllowIPSendNormalizesEquivalentIPForms(t *testing.T) {
	mgr := newTestVerificationManager()
	for i := 0; i < mgr.maxIPPerMin; i++ {
		if ok, _, _ := mgr.AllowIPSend("2001:0db8:0:0:0:0:0:1"); !ok {
			t.Fatalf("attempt %d unexpectedly rejected", i+1)
		}
	}
	if ok, _, _ := mgr.AllowIPSend("2001:db8::1"); ok {
		t.Fatal("equivalent IPv6 form bypassed IP limit")
	}
}
