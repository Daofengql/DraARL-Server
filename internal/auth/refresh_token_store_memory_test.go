package auth

import (
	"errors"
	"strconv"
	"testing"
	"time"
)

func TestMemoryRefreshRotationRejectsExpiredOrChangedSession(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name    string
		expires time.Time
		version uint64
	}{
		{"expired", now.Add(-time.Second), 2},
		{"expiry boundary", now, 2},
		{"different session version", now.Add(time.Hour), 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newMemoryRefreshTokenStore()
			old := &RefreshTokenRecord{UserID: 1, SessionVersion: 2, TokenHash: "old", ExpiresAt: tc.expires}
			if err := store.Create(old); err != nil {
				t.Fatal(err)
			}
			next := &RefreshTokenRecord{UserID: 1, SessionVersion: tc.version, TokenHash: "new", ExpiresAt: now.Add(time.Hour)}
			if err := store.Rotate("old", next, "rotated", now); !errors.Is(err, ErrRefreshTokenNotActive) {
				t.Fatalf("rotation error = %v", err)
			}
			if record, _ := store.GetByTokenHash("new"); record != nil {
				t.Fatal("rejected rotation created a session")
			}
		})
	}
}

func TestMemoryRefreshCleanupRemovesUserIndex(t *testing.T) {
	store := newMemoryRefreshTokenStore()
	now := time.Now()
	if err := store.Create(&RefreshTokenRecord{UserID: 1, TokenHash: "expired", ExpiresAt: now.Add(-expiredTokenRetention)}); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	store.cleanupExpiredLocked(now.Add(time.Minute))
	store.mu.Unlock()
	if record, _ := store.GetByTokenHash("expired"); record != nil {
		t.Fatal("expired token retained")
	}
	if len(store.userTokens) != 0 {
		t.Fatal("empty user index retained")
	}
}

func BenchmarkMemoryRefreshLookup(b *testing.B) {
	for _, count := range []int{1, 10000} {
		b.Run(strconv.Itoa(count)+" sessions", func(b *testing.B) {
			store := newMemoryRefreshTokenStore()
			expires := time.Now().Add(time.Hour)
			for i := 0; i < count; i++ {
				_ = store.Create(&RefreshTokenRecord{UserID: i + 1, TokenHash: strconv.Itoa(i), ExpiresAt: expires})
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = store.GetByTokenHash("0")
			}
		})
	}
}
