package handler

import (
	"bytes"
	"errors"
	"testing"
	"testing/iotest"
	"time"
)

func TestGenerateStateFromReader(t *testing.T) {
	state, err := generateStateFromReader(bytes.NewReader([]byte{
		0, 1, 2, 3, 4, 5, 6, 7,
		8, 9, 10, 11, 12, 13, 14, 15,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if state != "000102030405060708090a0b0c0d0e0f" {
		t.Fatalf("state=%q", state)
	}
}

func TestGenerateStateFailsClosed(t *testing.T) {
	state, err := generateStateFromReader(iotest.ErrReader(errors.New("rng unavailable")))
	if err == nil {
		t.Fatal("expected CSPRNG failure")
	}
	if state != "" {
		t.Fatalf("state=%q, want empty on CSPRNG failure", state)
	}
}

func TestSaveStateDoesNotDeadlockWhileCleaningExpiredEntries(t *testing.T) {
	stateMutex.Lock()
	original := stateStore
	stateStore = map[string]stateEntry{
		"expired": {ExpiresAt: time.Now().Add(-time.Minute)},
	}
	stateMutex.Unlock()
	t.Cleanup(func() {
		stateMutex.Lock()
		stateStore = original
		stateMutex.Unlock()
	})

	done := make(chan struct{})
	go func() {
		saveState("fresh", "login", 0)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("saveState deadlocked while cleaning expired entries")
	}

	stateMutex.RLock()
	_, expiredPresent := stateStore["expired"]
	_, freshPresent := stateStore["fresh"]
	stateMutex.RUnlock()
	if expiredPresent || !freshPresent {
		t.Fatalf("state cleanup result: expired=%v fresh=%v", expiredPresent, freshPresent)
	}
}

func TestSaveLoginCodeDoesNotDeadlockWhileCleaningExpiredEntries(t *testing.T) {
	loginCodeMutex.Lock()
	original := loginCodeStore
	loginCodeStore = map[string]loginCodeEntry{
		"expired": {ExpiresAt: time.Now().Add(-time.Minute)},
	}
	loginCodeMutex.Unlock()
	t.Cleanup(func() {
		loginCodeMutex.Lock()
		loginCodeStore = original
		loginCodeMutex.Unlock()
	})

	done := make(chan struct{})
	go func() {
		_, _ = saveLoginCode(0, map[string]any{"ok": true})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("saveLoginCode deadlocked while cleaning expired entries")
	}

	loginCodeMutex.RLock()
	_, expiredPresent := loginCodeStore["expired"]
	loginCodeMutex.RUnlock()
	if expiredPresent {
		t.Fatal("expired login code was not removed")
	}
}
