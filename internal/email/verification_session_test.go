package email

import (
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestVerificationSessionIsSingleUseUnderConcurrency(t *testing.T) {
	mgr := &VerificationManager{maxAttempts: 5}
	mgr.sessions.Store("session-1", &VerificationSession{
		SessionID: "session-1",
		Code:      "123456",
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Minute),
	})

	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := mgr.Verify("session-1", "123456"); err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if successes != 1 {
		t.Fatalf("concurrent verification successes=%d, want 1", successes)
	}
	if _, err := mgr.Verify("session-1", "123456"); err == nil {
		t.Fatal("verified session was reusable")
	}
}

func TestEmailCooldownReservationIsAtomic(t *testing.T) {
	mgr := &VerificationManager{cooldownPeriod: time.Minute, emailCooldown: make(map[string]time.Time)}
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _ := mgr.reserveEmailCooldown("same@example.test", time.Now()); ok {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if allowed != 1 {
		t.Fatalf("concurrent cooldown reservations=%d, want 1", allowed)
	}
}

func TestEmailCooldownReservationBoundsUniqueAddresses(t *testing.T) {
	mgr := &VerificationManager{
		cooldownPeriod: time.Minute,
		emailCooldown:  make(map[string]time.Time, maxVerificationEmailCooldownEntries),
	}
	now := time.Now()
	for i := 0; i < maxVerificationEmailCooldownEntries; i++ {
		mgr.emailCooldown["user"+strconv.Itoa(i)+"@example.test"] = now
	}
	if ok, _ := mgr.reserveEmailCooldown("new@example.test", now); ok {
		t.Fatal("new email should be rejected when cooldown table is full")
	}
}

func TestEmailCooldownNormalizesEquivalentAddresses(t *testing.T) {
	mgr := &VerificationManager{cooldownPeriod: time.Minute, emailCooldown: make(map[string]time.Time)}
	now := time.Now()
	if ok, _ := mgr.reserveEmailCooldown(" User@Example.Test ", now); !ok {
		t.Fatal("initial cooldown reservation failed")
	}
	if ok, _ := mgr.reserveEmailCooldown("user@example.test", now); ok {
		t.Fatal("equivalent email form bypassed cooldown")
	}
}
