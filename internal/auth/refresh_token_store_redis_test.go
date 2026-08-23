package auth

import (
	"testing"
	"time"

	"draarl/internal/config"
)

func TestRefreshStoreOperationTimeout(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Configuration
		want time.Duration
	}{
		{name: "nil config", want: defaultRefreshStoreOperationTimeout},
		{name: "zero config", cfg: &config.Configuration{}, want: defaultRefreshStoreOperationTimeout},
		{
			name: "configured command budget",
			cfg: func() *config.Configuration {
				cfg := &config.Configuration{}
				cfg.Redis.DialTimeoutSec = 4
				cfg.Redis.ReadTimeoutSec = 3
				cfg.Redis.WriteTimeoutSec = 2
				return cfg
			}(),
			want: 16 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := refreshStoreOperationTimeout(tt.cfg); got != tt.want {
				t.Fatalf("timeout=%v, want %v", got, tt.want)
			}
		})
	}
}

func TestRedisRefreshStoreOperationContextHasTotalDeadline(t *testing.T) {
	store := &redisRefreshTokenStore{operationTimeout: 250 * time.Millisecond}
	ctx, cancel := store.operationContext()
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("operation context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 || remaining > store.operationTimeout {
		t.Fatalf("remaining=%v, timeout=%v", remaining, store.operationTimeout)
	}
}

func TestRedisRefreshStoreOperationContextUsesDefault(t *testing.T) {
	store := &redisRefreshTokenStore{}
	ctx, cancel := store.operationContext()
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("operation context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 || remaining > defaultRefreshStoreOperationTimeout {
		t.Fatalf("remaining=%v, default=%v", remaining, defaultRefreshStoreOperationTimeout)
	}
}
