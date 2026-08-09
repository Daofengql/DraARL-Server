package auth

import (
	"strings"
	"sync"
	"time"
)

const expiredTokenRetention = 24 * time.Hour

type memoryRefreshTokenStore struct {
	mu         sync.RWMutex
	tokens     map[string]*RefreshTokenRecord
	userTokens map[int]map[string]struct{}
}

func newMemoryRefreshTokenStore() *memoryRefreshTokenStore {
	return &memoryRefreshTokenStore{
		tokens:     make(map[string]*RefreshTokenRecord),
		userTokens: make(map[int]map[string]struct{}),
	}
}

func (m *memoryRefreshTokenStore) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens = make(map[string]*RefreshTokenRecord)
	m.userTokens = make(map[int]map[string]struct{})
	return nil
}

func (m *memoryRefreshTokenStore) Create(token *RefreshTokenRecord) error {
	if token == nil {
		return nil
	}
	hash := strings.TrimSpace(token.TokenHash)
	if hash == "" {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupExpiredLocked(time.Now())
	m.tokens[hash] = cloneRefreshTokenRecord(token)
	m.addUserTokenLocked(token.UserID, hash)
	return nil
}

func (m *memoryRefreshTokenStore) GetByTokenHash(hash string) (*RefreshTokenRecord, error) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return nil, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupExpiredLocked(time.Now())

	record, ok := m.tokens[hash]
	if !ok {
		return nil, nil
	}
	return cloneRefreshTokenRecord(record), nil
}

func (m *memoryRefreshTokenStore) Rotate(oldTokenHash string, newToken *RefreshTokenRecord, revokeReason string, now time.Time) error {
	oldTokenHash = strings.TrimSpace(oldTokenHash)
	if oldTokenHash == "" || newToken == nil || strings.TrimSpace(newToken.TokenHash) == "" {
		return ErrRefreshTokenNotActive
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupExpiredLocked(now)

	oldRecord, ok := m.tokens[oldTokenHash]
	if !ok || oldRecord.RevokedAt != nil {
		return ErrRefreshTokenNotActive
	}

	if oldRecord.UserID != newToken.UserID {
		return ErrRefreshTokenNotActive
	}

	nowCopy := now
	oldRecord.RevokedAt = &nowCopy
	oldRecord.ReplacedByHash = newToken.TokenHash
	oldRecord.RevokeReason = strings.TrimSpace(revokeReason)
	oldRecord.LastUsedAt = &nowCopy

	m.tokens[newToken.TokenHash] = cloneRefreshTokenRecord(newToken)
	m.addUserTokenLocked(newToken.UserID, newToken.TokenHash)
	return nil
}

func (m *memoryRefreshTokenStore) RevokeByTokenHash(hash, reason string, now time.Time) error {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupExpiredLocked(now)

	record, ok := m.tokens[hash]
	if !ok || record.RevokedAt != nil {
		return nil
	}

	nowCopy := now
	record.RevokedAt = &nowCopy
	record.RevokeReason = strings.TrimSpace(reason)
	record.LastUsedAt = &nowCopy
	return nil
}

func (m *memoryRefreshTokenStore) RevokeAllByUser(userID int, reason string, now time.Time) error {
	if userID <= 0 {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupExpiredLocked(now)

	hashSet, ok := m.userTokens[userID]
	if !ok {
		return nil
	}

	nowCopy := now
	reason = strings.TrimSpace(reason)

	for hash := range hashSet {
		record, exists := m.tokens[hash]
		if !exists {
			delete(hashSet, hash)
			continue
		}
		if record.RevokedAt == nil {
			record.RevokedAt = &nowCopy
			record.RevokeReason = reason
			record.LastUsedAt = &nowCopy
		}
	}

	if len(hashSet) == 0 {
		delete(m.userTokens, userID)
	}
	return nil
}

func (m *memoryRefreshTokenStore) addUserTokenLocked(userID int, tokenHash string) {
	if userID <= 0 || tokenHash == "" {
		return
	}
	hashSet, ok := m.userTokens[userID]
	if !ok {
		hashSet = make(map[string]struct{})
		m.userTokens[userID] = hashSet
	}
	hashSet[tokenHash] = struct{}{}
}

func (m *memoryRefreshTokenStore) cleanupExpiredLocked(now time.Time) {
	for hash, record := range m.tokens {
		if now.After(record.ExpiresAt.Add(expiredTokenRetention)) {
			delete(m.tokens, hash)
			if set, ok := m.userTokens[record.UserID]; ok {
				delete(set, hash)
				if len(set) == 0 {
					delete(m.userTokens, record.UserID)
				}
			}
		}
	}
}

func cloneRefreshTokenRecord(record *RefreshTokenRecord) *RefreshTokenRecord {
	if record == nil {
		return nil
	}
	out := *record
	if record.RevokedAt != nil {
		revokedAtCopy := *record.RevokedAt
		out.RevokedAt = &revokedAtCopy
	}
	if record.LastUsedAt != nil {
		lastUsedCopy := *record.LastUsedAt
		out.LastUsedAt = &lastUsedCopy
	}
	return &out
}
