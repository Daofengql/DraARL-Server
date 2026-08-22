package handler

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func resetLoginGuardTestState() {
	loginGuardMu.Lock()
	loginGuard = make(map[string]*loginGuardEntry)
	loginGuardMu.Unlock()
	registrationMu.Lock()
	registration = make(map[string]*registrationWindow)
	registrationMu.Unlock()
	unknownLoginIPGuardMu.Lock()
	unknownLoginIPGuard = make(map[string]*loginGuardEntry)
	unknownLoginIPGuardMu.Unlock()
}

func TestLoginGuardAccumulatesFailuresAndLocks(t *testing.T) {
	resetLoginGuardTestState()
	t.Cleanup(resetLoginGuardTestState)

	for i := 0; i < loginMaxFailures-1; i++ {
		if !loginGuardRecordFailure(" User-One ") {
			t.Fatal("failure should be admitted")
		}
		if locked, _ := loginGuardCheck("USER-ONE"); locked {
			t.Fatalf("account locked after %d failures", i+1)
		}
	}
	loginGuardMu.Lock()
	if got := loginGuard["user-one"].failCount; got != loginMaxFailures-1 {
		loginGuardMu.Unlock()
		t.Fatalf("fail count=%d, want %d", got, loginMaxFailures-1)
	}
	loginGuardMu.Unlock()

	if !loginGuardRecordFailure("user-one") {
		t.Fatal("threshold failure should be admitted")
	}
	locked, retryAfter := loginGuardCheck("user-one")
	if !locked || retryAfter <= 0 {
		t.Fatalf("lock state=(%v,%s), want active lock", locked, retryAfter)
	}
}

func TestLoginGuardExpiresFailureWindowAndLock(t *testing.T) {
	resetLoginGuardTestState()
	t.Cleanup(resetLoginGuardTestState)

	loginGuardMu.Lock()
	loginGuard["window-user"] = &loginGuardEntry{
		failCount:      2,
		firstFailureAt: time.Now().Add(-loginFailureWindow - time.Second),
	}
	loginGuard["locked-user"] = &loginGuardEntry{lockedUntil: time.Now().Add(-time.Second)}
	loginGuardMu.Unlock()

	if locked, _ := loginGuardCheck("window-user"); locked {
		t.Fatal("expired failure window must not remain locked")
	}
	if locked, _ := loginGuardCheck("locked-user"); locked {
		t.Fatal("expired lock must not remain active")
	}
	loginGuardMu.Lock()
	defer loginGuardMu.Unlock()
	if len(loginGuard) != 0 {
		t.Fatalf("expired entries remain: %d", len(loginGuard))
	}
}

func TestLoginGuardClearAndRegistrationWindow(t *testing.T) {
	resetLoginGuardTestState()
	t.Cleanup(resetLoginGuardTestState)

	loginGuardRecordFailure("clear-user")
	loginGuardClear(" CLEAR-USER ")
	if locked, _ := loginGuardCheck("clear-user"); locked {
		t.Fatal("cleared user must not remain locked")
	}

	for i := 0; i < registerMaxPerIP; i++ {
		if !allowRegistration("192.0.2.10") {
			t.Fatalf("registration attempt %d unexpectedly rejected", i+1)
		}
	}
	if allowRegistration("192.0.2.10") || allowRegistration("192.0.2.10") {
		t.Fatal("registration limit was bypassed")
	}
	registrationMu.Lock()
	registration["192.0.2.10"].windowStart = time.Now().Add(-registerWindowDur - time.Second)
	registrationMu.Unlock()
	if !allowRegistration("192.0.2.10") {
		t.Fatal("expired registration window did not reset")
	}
}

func TestLoginAndRegistrationStateTablesFailClosedWhenFull(t *testing.T) {
	resetLoginGuardTestState()
	t.Cleanup(resetLoginGuardTestState)
	now := time.Now()
	loginGuardMu.Lock()
	for i := 0; i < maxLoginGuardEntries; i++ {
		loginGuard[fmt.Sprintf("active-login-%d", i)] = &loginGuardEntry{failCount: 1, firstFailureAt: now}
	}
	loginGuardMu.Unlock()
	if loginGuardRecordFailure("new-login") {
		t.Fatal("new login key must be rejected when the table is full")
	}

	registrationMu.Lock()
	for i := 0; i < maxRegistrationEntries; i++ {
		registration[fmt.Sprintf("198.51.100.%d", i)] = &registrationWindow{windowStart: now}
	}
	registrationMu.Unlock()
	if allowRegistration("203.0.113.1") {
		t.Fatal("new registration IP must be rejected when the table is full")
	}
}

func TestLoginGuardConcurrentAccess(t *testing.T) {
	resetLoginGuardTestState()
	t.Cleanup(resetLoginGuardTestState)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				loginGuardCheck("concurrent-user")
				loginGuardRecordFailure("concurrent-user")
				allowRegistration("192.0.2.20")
			}
		}()
	}
	wg.Wait()
}

func TestUnknownLoginIPGuardBlocksUsernameCycling(t *testing.T) {
	resetLoginGuardTestState()
	t.Cleanup(resetLoginGuardTestState)
	for i := 0; i < unknownLoginIPMaxFailures-1; i++ {
		if !unknownLoginIPGuardRecordFailure(" 192.0.2.44 ") {
			t.Fatalf("failure %d should be admitted", i+1)
		}
		if locked, _ := unknownLoginIPGuardCheck("192.0.2.44"); locked {
			t.Fatalf("IP locked before threshold at failure %d", i+1)
		}
	}
	if !unknownLoginIPGuardRecordFailure("192.0.2.44") {
		t.Fatal("threshold failure should be admitted")
	}
	locked, retryAfter := unknownLoginIPGuardCheck("192.0.2.44")
	if !locked || retryAfter <= 0 {
		t.Fatalf("unknown-account IP state=(%v,%s), want active lock", locked, retryAfter)
	}
}

func TestUnknownLoginIPGuardBoundsNewSources(t *testing.T) {
	resetLoginGuardTestState()
	t.Cleanup(resetLoginGuardTestState)
	now := time.Now()
	unknownLoginIPGuardMu.Lock()
	for i := 0; i < maxUnknownLoginIPEntries; i++ {
		unknownLoginIPGuard[fmt.Sprintf("198.51.100.%d", i)] = &loginGuardEntry{failCount: 1, firstFailureAt: now}
	}
	unknownLoginIPGuardMu.Unlock()
	if unknownLoginIPGuardRecordFailure("203.0.113.44") {
		t.Fatal("new source must be rejected when unknown-account IP table is full")
	}
}
