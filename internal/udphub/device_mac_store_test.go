package udphub

import (
	"testing"
	"time"
)

func TestDeviceMACStoreMemoryTTLAndBound(t *testing.T) {
	store := newDeviceMACStore()
	store.maxEntries = 1
	store.Set(1, 1, "aa:bb:cc:dd:ee:ff")
	if got := store.Get(1, 1); got != "AA:BB:CC:DD:EE:FF" {
		t.Fatalf("normalized MAC=%q, want AA:BB:CC:DD:EE:FF", got)
	}

	// A full store refuses an unexpired replacement, preserving the existing
	// entry instead of evicting a live binding under memory pressure.
	store.Set(2, 2, "11:22:33:44:55:66")
	if got := store.Get(2, 2); got != "" {
		t.Fatalf("full store admitted live replacement %q", got)
	}
	if got := store.Get(1, 1); got != "AA:BB:CC:DD:EE:FF" {
		t.Fatalf("existing live MAC was evicted: %q", got)
	}

	store.mu.Lock()
	entry := store.memory[getOwnerSSIDKey(1, 1)]
	entry.expiresAt = time.Now().Add(-time.Second)
	store.memory[getOwnerSSIDKey(1, 1)] = entry
	store.mu.Unlock()
	store.Set(2, 2, "11:22:33:44:55:66")
	if got := store.Get(2, 2); got != "11:22:33:44:55:66" {
		t.Fatalf("expired slot was not reused, got %q", got)
	}
	if got := store.Get(1, 1); got != "" {
		t.Fatalf("expired MAC remained visible: %q", got)
	}
}

func TestDeviceMACStoreOperationContextHasBoundedDeadline(t *testing.T) {
	store := newDeviceMACStore()
	store.operationTTL = 25 * time.Millisecond
	ctx, cancel := store.operationContext()
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("operation context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 || remaining > store.operationTTL {
		t.Fatalf("remaining=%v operationTTL=%v", remaining, store.operationTTL)
	}
}
