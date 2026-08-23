package handler

import (
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"draarl/internal/email"
)

// Login brute-force protection is request-driven and bounded, so it requires
// no background goroutine or schema change.
const (
	loginMaxFailures       = 5
	loginFailureWindow     = 10 * time.Minute
	loginLockDuration      = 10 * time.Minute
	maxLoginGuardEntries   = 100_000
	loginGuardPrunePerCall = 256
	// Unknown usernames are tracked by source IP so an attacker cannot evade
	// the account guard by cycling through arbitrary names. This guard is only
	// charged for lookups that found no account; valid shared-NAT users do not
	// consume its budget.
	unknownLoginIPMaxFailures   = 20
	unknownLoginIPFailureWindow = 10 * time.Minute
	unknownLoginIPLockDuration  = 10 * time.Minute
	maxUnknownLoginIPEntries    = 100_000
	unknownLoginIPPrunePerCall  = 256
)

const (
	verificationEmailMaxPerIP = 5
	verificationEmailIPWindow = time.Minute
)

type loginGuardEntry struct {
	failCount      int
	firstFailureAt time.Time
	lockedUntil    time.Time
}

var (
	loginGuardMu          sync.Mutex
	loginGuard            = make(map[string]*loginGuardEntry)
	unknownLoginIPGuardMu sync.Mutex
	unknownLoginIPGuard   = make(map[string]*loginGuardEntry)
)

func normalizeLoginGuardKey(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

func loginGuardEntryExpired(entry *loginGuardEntry, now time.Time) bool {
	if entry == nil {
		return true
	}
	if !entry.lockedUntil.IsZero() {
		return !now.Before(entry.lockedUntil)
	}
	return entry.failCount <= 0 || entry.firstFailureAt.IsZero() ||
		!now.Before(entry.firstFailureAt.Add(loginFailureWindow))
}

// pruneLoginGuardLocked removes at most max stale entries. The bound keeps
// latency predictable when an attacker has filled the table.
func pruneLoginGuardLocked(now time.Time, max int) {
	for key, entry := range loginGuard {
		if max <= 0 {
			return
		}
		if loginGuardEntryExpired(entry, now) {
			delete(loginGuard, key)
			max--
		}
	}
}

// loginGuardCheck returns whether the account is locked. An active failure
// count is retained; checking a password must not erase pre-threshold failures.
func loginGuardCheck(username string) (bool, time.Duration) {
	key := normalizeLoginGuardKey(username)
	if key == "" {
		return false, 0
	}
	if shared := currentSharedLoginGuard(); shared != nil {
		locked, retryAfter, err := shared.CheckLock("account", key)
		if err != nil {
			log.Printf("[AUTH] shared login guard check failed: %v", err)
			return true, time.Second
		}
		return locked, retryAfter
	}
	now := time.Now()
	loginGuardMu.Lock()
	defer loginGuardMu.Unlock()
	entry, ok := loginGuard[key]
	if !ok {
		return false, 0
	}
	if entry.lockedUntil.IsZero() {
		if entry.failCount <= 0 || entry.firstFailureAt.IsZero() ||
			!now.Before(entry.firstFailureAt.Add(loginFailureWindow)) {
			delete(loginGuard, key)
		}
		return false, 0
	}
	if !now.Before(entry.lockedUntil) {
		delete(loginGuard, key)
		return false, 0
	}
	return true, entry.lockedUntil.Sub(now)
}

// loginGuardRecordFailure records one password failure. It returns false when
// a new key cannot be admitted because the bounded table is full.
func loginGuardRecordFailure(username string) bool {
	key := normalizeLoginGuardKey(username)
	if key == "" {
		return true
	}
	if shared := currentSharedLoginGuard(); shared != nil {
		if err := shared.RecordFailure("account", key, loginMaxFailures, loginFailureWindow, loginLockDuration); err != nil {
			log.Printf("[AUTH] shared login guard record failed: %v", err)
			return false
		}
		return true
	}
	now := time.Now()
	loginGuardMu.Lock()
	defer loginGuardMu.Unlock()
	entry, ok := loginGuard[key]
	if !ok {
		if len(loginGuard) >= maxLoginGuardEntries {
			pruneLoginGuardLocked(now, loginGuardPrunePerCall)
		}
		if len(loginGuard) >= maxLoginGuardEntries {
			return false
		}
		entry = &loginGuardEntry{firstFailureAt: now}
		loginGuard[key] = entry
	} else if !entry.lockedUntil.IsZero() {
		if now.Before(entry.lockedUntil) {
			return false
		}
		entry.failCount = 0
		entry.firstFailureAt = now
		entry.lockedUntil = time.Time{}
	} else if entry.firstFailureAt.IsZero() || !now.Before(entry.firstFailureAt.Add(loginFailureWindow)) {
		entry.failCount = 0
		entry.firstFailureAt = now
	}

	if entry.failCount < loginMaxFailures {
		entry.failCount++
	}
	if entry.failCount >= loginMaxFailures {
		entry.lockedUntil = now.Add(loginLockDuration)
	}
	return true
}

func loginGuardClear(username string) {
	key := normalizeLoginGuardKey(username)
	if key == "" {
		return
	}
	if shared := currentSharedLoginGuard(); shared != nil {
		if err := shared.Clear("account", key); err != nil {
			log.Printf("[AUTH] shared login guard clear failed: %v", err)
		}
		return
	}
	loginGuardMu.Lock()
	delete(loginGuard, key)
	loginGuardMu.Unlock()
}

func normalizeLoginGuardIP(ip string) string {
	key := strings.TrimSpace(ip)
	if parsed := net.ParseIP(key); parsed != nil {
		return parsed.String()
	}
	return key
}

func allowVerificationEmailSend(manager *email.VerificationManager, ip string) (bool, string) {
	key := normalizeLoginGuardIP(ip)
	if key == "" {
		key = "unknown"
	}
	if shared := currentSharedLoginGuard(); shared != nil {
		allowed, _, err := shared.Allow("verification_email_ip", key, verificationEmailMaxPerIP, verificationEmailIPWindow)
		if err != nil {
			log.Printf("[AUTH] shared verification email guard failed: %v", err)
			return false, "该 IP 发送过于频繁，请稍后再试"
		}
		if !allowed {
			return false, "该 IP 发送过于频繁，请稍后再试"
		}
		return true, ""
	}
	allowed, _, message := manager.AllowIPSend(key)
	return allowed, message
}

func unknownLoginIPEntryExpired(entry *loginGuardEntry, now time.Time) bool {
	if entry == nil {
		return true
	}
	if !entry.lockedUntil.IsZero() {
		return !now.Before(entry.lockedUntil)
	}
	return entry.failCount <= 0 || entry.firstFailureAt.IsZero() ||
		!now.Before(entry.firstFailureAt.Add(unknownLoginIPFailureWindow))
}

func pruneUnknownLoginIPGuardLocked(now time.Time, max int) {
	for key, entry := range unknownLoginIPGuard {
		if max <= 0 {
			return
		}
		if unknownLoginIPEntryExpired(entry, now) {
			delete(unknownLoginIPGuard, key)
			max--
		}
	}
}

// unknownLoginIPGuardCheck reports whether a source is temporarily blocked
// after repeated unknown-account lookups.
func unknownLoginIPGuardCheck(ip string) (bool, time.Duration) {
	key := normalizeLoginGuardIP(ip)
	if key == "" {
		return false, 0
	}
	if shared := currentSharedLoginGuard(); shared != nil {
		locked, retryAfter, err := shared.CheckLock("unknown_ip", key)
		if err != nil {
			log.Printf("[AUTH] shared unknown-login guard check failed: %v", err)
			return true, time.Second
		}
		return locked, retryAfter
	}
	now := time.Now()
	unknownLoginIPGuardMu.Lock()
	defer unknownLoginIPGuardMu.Unlock()
	entry, ok := unknownLoginIPGuard[key]
	if !ok {
		return false, 0
	}
	if entry.lockedUntil.IsZero() {
		if unknownLoginIPEntryExpired(entry, now) {
			delete(unknownLoginIPGuard, key)
		}
		return false, 0
	}
	if !now.Before(entry.lockedUntil) {
		delete(unknownLoginIPGuard, key)
		return false, 0
	}
	return true, entry.lockedUntil.Sub(now)
}

// unknownLoginIPGuardRecordFailure records an unknown-account failure. A full
// table fails closed for new source IPs, keeping memory bounded under churn.
func unknownLoginIPGuardRecordFailure(ip string) bool {
	key := normalizeLoginGuardIP(ip)
	if key == "" {
		return true
	}
	if shared := currentSharedLoginGuard(); shared != nil {
		if err := shared.RecordFailure("unknown_ip", key, unknownLoginIPMaxFailures, unknownLoginIPFailureWindow, unknownLoginIPLockDuration); err != nil {
			log.Printf("[AUTH] shared unknown-login guard record failed: %v", err)
			return false
		}
		return true
	}
	now := time.Now()
	unknownLoginIPGuardMu.Lock()
	defer unknownLoginIPGuardMu.Unlock()
	entry, ok := unknownLoginIPGuard[key]
	if !ok {
		if len(unknownLoginIPGuard) >= maxUnknownLoginIPEntries {
			pruneUnknownLoginIPGuardLocked(now, unknownLoginIPPrunePerCall)
		}
		if len(unknownLoginIPGuard) >= maxUnknownLoginIPEntries {
			return false
		}
		entry = &loginGuardEntry{firstFailureAt: now}
		unknownLoginIPGuard[key] = entry
	} else if !entry.lockedUntil.IsZero() {
		if now.Before(entry.lockedUntil) {
			return false
		}
		entry.failCount = 0
		entry.firstFailureAt = now
		entry.lockedUntil = time.Time{}
	} else if entry.firstFailureAt.IsZero() || !now.Before(entry.firstFailureAt.Add(unknownLoginIPFailureWindow)) {
		entry.failCount = 0
		entry.firstFailureAt = now
	}

	if entry.failCount < unknownLoginIPMaxFailures {
		entry.failCount++
	}
	if entry.failCount >= unknownLoginIPMaxFailures {
		entry.lockedUntil = now.Add(unknownLoginIPLockDuration)
	}
	return true
}

// Registration protection uses a one-hour, five-attempt window per IP.
const (
	registerMaxPerIP       = 5
	registerWindowDur      = time.Hour
	maxRegistrationEntries = 100_000
	registrationPruneLimit = 256
)

var (
	registrationMu sync.Mutex
	registration   = make(map[string]*registrationWindow)
)

type registrationWindow struct {
	windowStart time.Time
	count       int
}

func registrationWindowExpired(window *registrationWindow, now time.Time) bool {
	return window == nil || window.windowStart.IsZero() ||
		!now.Before(window.windowStart.Add(registerWindowDur))
}

func pruneRegistrationLocked(now time.Time, max int) {
	for key, window := range registration {
		if max <= 0 {
			return
		}
		if registrationWindowExpired(window, now) {
			delete(registration, key)
			max--
		}
	}
}

// allowRegistration consumes one attempt and fails closed for a new IP when
// the bounded table cannot admit another state entry.
func allowRegistration(ip string) bool {
	key := strings.TrimSpace(ip)
	if key == "" {
		key = "unknown"
	}
	if shared := currentSharedLoginGuard(); shared != nil {
		allowed, _, err := shared.Allow("registration_ip", key, registerMaxPerIP, registerWindowDur)
		if err != nil {
			log.Printf("[AUTH] shared registration guard failed: %v", err)
			return false
		}
		return allowed
	}
	now := time.Now()
	registrationMu.Lock()
	defer registrationMu.Unlock()
	window, ok := registration[key]
	if !ok {
		if len(registration) >= maxRegistrationEntries {
			pruneRegistrationLocked(now, registrationPruneLimit)
		}
		if len(registration) >= maxRegistrationEntries {
			return false
		}
		window = &registrationWindow{windowStart: now}
		registration[key] = window
	} else if registrationWindowExpired(window, now) {
		window.windowStart = now
		window.count = 0
	}
	if window.count < registerMaxPerIP+1 {
		window.count++
	}
	return window.count <= registerMaxPerIP
}
