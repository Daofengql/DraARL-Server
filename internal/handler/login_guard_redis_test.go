package handler

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"draarl/internal/config"
)

type failingLoginGuard struct {
	err error
}

func (f failingLoginGuard) CheckLock(string, string) (bool, time.Duration, error) {
	return false, 0, f.err
}

func (f failingLoginGuard) RecordFailure(string, string, int, time.Duration, time.Duration) error {
	return f.err
}

func (f failingLoginGuard) Clear(string, string) error { return f.err }

func (f failingLoginGuard) Allow(string, string, int, time.Duration) (bool, time.Duration, error) {
	return false, 0, f.err
}

func (f failingLoginGuard) Close() error { return nil }

func setLoginGuardForTest(store sharedLoginGuard) func() {
	sharedLoginGuardMu.Lock()
	previous := activeLoginGuard
	activeLoginGuard = store
	sharedLoginGuardMu.Unlock()
	return func() {
		sharedLoginGuardMu.Lock()
		activeLoginGuard = previous
		sharedLoginGuardMu.Unlock()
	}
}

func TestInitLoginGuardStoreFailsClosedInRelease(t *testing.T) {
	previousRelease := config.IsReleaseBuild()
	config.SetReleaseBuild(true)
	t.Cleanup(func() { config.SetReleaseBuild(previousRelease) })

	previousFactory := loginGuardFactory
	loginGuardFactory = func(*config.Configuration) (*redisLoginGuard, error) {
		return nil, errors.New("injected redis outage")
	}
	t.Cleanup(func() { loginGuardFactory = previousFactory })

	CloseLoginGuardStore()
	if err := InitLoginGuardStore(&config.Configuration{}); err == nil {
		t.Fatal("release login guard initialization unexpectedly downgraded to memory")
	}
	if store := currentSharedLoginGuard(); store != nil {
		t.Fatalf("release failure retained login guard store %T", store)
	}
}

func TestInitLoginGuardStoreKeepsDevelopmentMemoryFallback(t *testing.T) {
	previousRelease := config.IsReleaseBuild()
	config.SetReleaseBuild(false)
	t.Cleanup(func() { config.SetReleaseBuild(previousRelease) })

	previousFactory := loginGuardFactory
	loginGuardFactory = func(*config.Configuration) (*redisLoginGuard, error) {
		return nil, errors.New("injected redis outage")
	}
	t.Cleanup(func() { loginGuardFactory = previousFactory })

	CloseLoginGuardStore()
	if err := InitLoginGuardStore(&config.Configuration{}); err != nil {
		t.Fatalf("development fallback returned error: %v", err)
	}
	if store := currentSharedLoginGuard(); store != nil {
		t.Fatalf("development fallback unexpectedly installed shared store %T", store)
	}
}

func TestLoginGuardFailsClosedOnSharedStoreErrors(t *testing.T) {
	restore := setLoginGuardForTest(failingLoginGuard{err: errors.New("redis unavailable")})
	t.Cleanup(restore)

	if locked, retryAfter := loginGuardCheck("account"); !locked || retryAfter <= 0 {
		t.Fatalf("account check=(%v,%s), want temporary lock", locked, retryAfter)
	}
	if unknown, retryAfter := unknownLoginIPGuardCheck("192.0.2.1"); !unknown || retryAfter <= 0 {
		t.Fatalf("unknown IP check=(%v,%s), want temporary lock", unknown, retryAfter)
	}
	if loginGuardRecordFailure("account") {
		t.Fatal("account record unexpectedly allowed after shared store error")
	}
	if unknownLoginIPGuardRecordFailure("192.0.2.1") {
		t.Fatal("unknown IP record unexpectedly allowed after shared store error")
	}
	if allowRegistration("192.0.2.1") {
		t.Fatal("registration unexpectedly allowed after shared store error")
	}
	if allowed, _ := allowVerificationEmailSend(nil, "192.0.2.1"); allowed {
		t.Fatal("verification email unexpectedly allowed after shared store error")
	}
}

func TestLoginGuardNormalizesEquivalentIPForms(t *testing.T) {
	if got := normalizeLoginGuardIP(" 2001:0db8:0:0:0:0:0:1 "); got != "2001:db8::1" {
		t.Fatalf("normalized IPv6=%q, want canonical form", got)
	}
	if got := normalizeLoginGuardIP(" 192.0.2.1 "); got != "192.0.2.1" {
		t.Fatalf("normalized IPv4=%q", got)
	}
}

func TestRedisLoginGuardRejectsInvalidPolicies(t *testing.T) {
	store := &redisLoginGuard{}
	if err := store.RecordFailure("account", "user", 0, time.Minute, time.Minute); err == nil {
		t.Fatal("zero failure limit unexpectedly accepted")
	}
	if err := store.RecordFailure("account", "user", 1, 0, time.Minute); err == nil {
		t.Fatal("zero failure window unexpectedly accepted")
	}
	if err := store.RecordFailure("account", "user", 1, time.Minute, 0); err == nil {
		t.Fatal("zero lock duration unexpectedly accepted")
	}
	if allowed, _, err := store.Allow("registration_ip", "ip", 0, time.Hour); err == nil || allowed {
		t.Fatal("zero rate-limit policy unexpectedly accepted")
	}
}

func TestRedisLoginGuardOperationContextHasDeadline(t *testing.T) {
	store := &redisLoginGuard{operationTimeout: 250 * time.Millisecond}
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

func TestRedisLoginGuardAcrossInstances(t *testing.T) {
	addr := strings.TrimSpace(os.Getenv("DRAARL_REDIS_ADDR"))
	if addr == "" {
		t.Skip("set DRAARL_REDIS_ADDR to run Redis integration test")
	}
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("invalid DRAARL_REDIS_ADDR %q: %v", addr, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		t.Fatalf("invalid Redis port %q", portText)
	}

	prefix := fmt.Sprintf("codex-login-guard-%d", time.Now().UnixNano())
	newConfig := func() *config.Configuration {
		cfg := &config.Configuration{}
		cfg.Redis.Host = host
		cfg.Redis.Port = port
		cfg.Redis.Password = os.Getenv("DRAARL_REDIS_PASSWORD")
		cfg.Redis.Prefix = prefix
		cfg.Redis.DialTimeoutSec = 2
		cfg.Redis.ReadTimeoutSec = 2
		cfg.Redis.WriteTimeoutSec = 2
		cfg.Redis.PoolSize = 4
		if dbText := strings.TrimSpace(os.Getenv("DRAARL_REDIS_DB")); dbText != "" {
			cfg.Redis.DB, err = strconv.Atoi(dbText)
			if err != nil || cfg.Redis.DB < 0 {
				t.Fatalf("invalid DRAARL_REDIS_DB %q", dbText)
			}
		}
		return cfg
	}

	first, err := newRedisLoginGuard(newConfig())
	if err != nil {
		t.Fatalf("create first Redis guard: %v", err)
	}
	second, err := newRedisLoginGuard(newConfig())
	if err != nil {
		_ = first.Close()
		t.Fatalf("create second Redis guard: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		keys, scanErr := first.client.Keys(ctx, first.prefix+"*").Result()
		if scanErr == nil && len(keys) > 0 {
			_ = first.client.Del(ctx, keys...).Err()
		}
		_ = first.Close()
		_ = second.Close()
	})

	accountKey := "shared-account"
	for i := 0; i < loginMaxFailures; i++ {
		if err := first.RecordFailure("account", accountKey, loginMaxFailures, loginFailureWindow, loginLockDuration); err != nil {
			t.Fatalf("record account failure %d: %v", i+1, err)
		}
	}
	if locked, retryAfter, err := second.CheckLock("account", accountKey); err != nil || !locked || retryAfter <= 0 {
		t.Fatalf("second instance account lock=(%v,%s)", locked, retryAfter)
	}
	if err := second.Clear("account", accountKey); err != nil {
		t.Fatalf("clear account lock: %v", err)
	}
	if locked, _, err := first.CheckLock("account", accountKey); err != nil || locked {
		t.Fatal("account clear was not visible to first instance")
	}

	unknownIP := "192.0.2.77"
	for i := 0; i < unknownLoginIPMaxFailures; i++ {
		if err := first.RecordFailure("unknown_ip", unknownIP, unknownLoginIPMaxFailures, unknownLoginIPFailureWindow, unknownLoginIPLockDuration); err != nil {
			t.Fatalf("record unknown IP failure %d: %v", i+1, err)
		}
	}
	if locked, retryAfter, err := second.CheckLock("unknown_ip", unknownIP); err != nil || !locked || retryAfter <= 0 {
		t.Fatalf("second instance unknown IP lock=(%v,%s)", locked, retryAfter)
	}

	registrationIP := "192.0.2.88"
	for i := 0; i < registerMaxPerIP; i++ {
		allowed, _, err := first.Allow("registration_ip", registrationIP, registerMaxPerIP, registerWindowDur)
		if err != nil || !allowed {
			t.Fatalf("registration attempt %d allowed=%v err=%v", i+1, allowed, err)
		}
	}
	allowed, _, err := second.Allow("registration_ip", registrationIP, registerMaxPerIP, registerWindowDur)
	if err != nil {
		t.Fatalf("registration sixth attempt: %v", err)
	}
	if allowed {
		t.Fatal("registration limit was not shared across instances")
	}

	verificationIP := "192.0.2.99"
	for i := 0; i < verificationEmailMaxPerIP; i++ {
		allowed, _, err := first.Allow("verification_email_ip", verificationIP, verificationEmailMaxPerIP, verificationEmailIPWindow)
		if err != nil || !allowed {
			t.Fatalf("verification email attempt %d allowed=%v err=%v", i+1, allowed, err)
		}
	}
	allowed, _, err = second.Allow("verification_email_ip", verificationIP, verificationEmailMaxPerIP, verificationEmailIPWindow)
	if err != nil {
		t.Fatalf("verification email sixth attempt: %v", err)
	}
	if allowed {
		t.Fatal("verification email limit was not shared across instances")
	}
}
