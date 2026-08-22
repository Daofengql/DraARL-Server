package auth

import (
	"errors"
	"testing"

	"draarl/internal/config"
)

func TestInitRefreshTokenStoreFailsClosedInRelease(t *testing.T) {
	previousRelease := config.IsReleaseBuild()
	config.SetReleaseBuild(true)
	t.Cleanup(func() { config.SetReleaseBuild(previousRelease) })

	previousFactory := redisStoreFactory
	redisStoreFactory = func(*config.Configuration) (*redisRefreshTokenStore, error) {
		return nil, errors.New("injected redis outage")
	}
	t.Cleanup(func() {
		redisStoreFactory = previousFactory
		CloseRefreshTokenStore()
	})

	if err := InitRefreshTokenStore(&config.Configuration{}); err == nil {
		t.Fatal("release refresh-token initialization unexpectedly downgraded to memory")
	}
	storeMu.RLock()
	store := refreshTokenStore
	storeMu.RUnlock()
	if store != nil {
		t.Fatalf("release failure retained refresh-token store %T", store)
	}
}

func TestInitRefreshTokenStoreKeepsDevelopmentMemoryFallback(t *testing.T) {
	previousRelease := config.IsReleaseBuild()
	config.SetReleaseBuild(false)
	t.Cleanup(func() { config.SetReleaseBuild(previousRelease) })

	previousFactory := redisStoreFactory
	redisStoreFactory = func(*config.Configuration) (*redisRefreshTokenStore, error) {
		return nil, errors.New("injected redis outage")
	}
	t.Cleanup(func() {
		redisStoreFactory = previousFactory
		CloseRefreshTokenStore()
	})

	if err := InitRefreshTokenStore(&config.Configuration{}); err != nil {
		t.Fatalf("development fallback returned error: %v", err)
	}
	storeMu.RLock()
	store := refreshTokenStore
	storeMu.RUnlock()
	if _, ok := store.(*memoryRefreshTokenStore); !ok {
		t.Fatalf("development fallback store=%T, want memoryRefreshTokenStore", store)
	}
}
