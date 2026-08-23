package email

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"strings"
	"sync"
	"time"
)

// Purpose 验证码用途
type Purpose string

const (
	PurposeRegister      Purpose = "register"       // 注册
	PurposeLogin         Purpose = "login"          // 登录
	PurposeResetPassword Purpose = "reset_password" // 重置密码
	PurposeChangeEmail   Purpose = "change_email"   // 修改邮箱
)

// VerificationSession 验证会话
type VerificationSession struct {
	SessionID  string     `json:"session_id"`
	Email      string     `json:"email"`
	Code       string     `json:"code"`
	Purpose    Purpose    `json:"purpose"`
	Attempts   int        `json:"attempts"` // 尝试次数
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	VerifiedAt *time.Time `json:"verified_at,omitempty"` // 验证成功时间
}

// VerificationManager 验证码会话管理器
type VerificationManager struct {
	sessions        sync.Map // session_id -> *VerificationSession
	sessionMu       sync.Mutex
	emailCooldown   map[string]time.Time // email -> lastSendTime
	emailCooldownMu sync.Mutex
	ipRateMu        sync.Mutex
	ipRateLimit     map[string][]time.Time // ip -> []sendTime (IP 发送记录)

	codeLength     int
	codeExpiry     time.Duration
	cooldownPeriod time.Duration
	maxAttempts    int
	maxIPPerMin    int // 同一 IP 每分钟最大发送次数
}

var (
	verificationManager *VerificationManager
	once                sync.Once
)

// GetVerificationManager 获取验证码管理器实例
func GetVerificationManager() *VerificationManager {
	once.Do(func() {
		verificationManager = &VerificationManager{
			codeLength:     6,
			codeExpiry:     10 * time.Minute,
			cooldownPeriod: 60 * time.Second,
			maxAttempts:    5,
			maxIPPerMin:    5, // 同一 IP 每分钟最多 5 次
			ipRateLimit:    make(map[string][]time.Time),
			emailCooldown:  make(map[string]time.Time),
		}
		// 启动定时清理任务
		go verificationManager.cleanupExpired()
	})
	return verificationManager
}

// generateCode 生成均匀分布的6位数字验证码。
func generateCode() (string, error) {
	return generateCodeFromReader(rand.Reader)
}

func generateCodeFromReader(reader io.Reader) (string, error) {
	num, err := rand.Int(reader, big.NewInt(1_000_000))
	if err != nil {
		return "", fmt.Errorf("read verification code randomness: %w", err)
	}
	return fmt.Sprintf("%06d", num.Int64()), nil
}

// generateSessionID 生成128位随机会话ID。
func generateSessionID() (string, error) {
	return generateSessionIDFromReader(rand.Reader)
}

func generateSessionIDFromReader(reader io.Reader) (string, error) {
	b := make([]byte, 16)
	if _, err := io.ReadFull(reader, b); err != nil {
		return "", fmt.Errorf("read verification session randomness: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// CanSend 检查是否可以发送验证码（冷却时间检查）
func (m *VerificationManager) CanSend(email string) (bool, time.Duration) {
	m.emailCooldownMu.Lock()
	defer m.emailCooldownMu.Unlock()
	return m.canSendLocked(normalizeVerificationEmailKey(email), time.Now())
}

func (m *VerificationManager) canSendLocked(email string, now time.Time) (bool, time.Duration) {
	if lastSend, ok := m.emailCooldown[email]; ok {
		elapsed := now.Sub(lastSend)
		if elapsed < m.cooldownPeriod {
			return false, m.cooldownPeriod - elapsed
		}
	}
	return true, 0
}

// reserveEmailCooldown atomically checks and reserves the per-email cooldown.
func (m *VerificationManager) reserveEmailCooldown(email string, now time.Time) (bool, time.Duration) {
	key := normalizeVerificationEmailKey(email)
	m.emailCooldownMu.Lock()
	defer m.emailCooldownMu.Unlock()
	if m.emailCooldown == nil {
		m.emailCooldown = make(map[string]time.Time)
	}
	if allowed, remaining := m.canSendLocked(key, now); !allowed {
		return false, remaining
	}
	if _, exists := m.emailCooldown[key]; !exists && len(m.emailCooldown) >= maxVerificationEmailCooldownEntries {
		m.pruneEmailCooldownLocked(now, 256)
		if len(m.emailCooldown) >= maxVerificationEmailCooldownEntries {
			return false, m.cooldownPeriod
		}
	}
	m.emailCooldown[key] = now
	return true, 0
}

func (m *VerificationManager) releaseEmailCooldown(email string, reservedAt time.Time) {
	key := normalizeVerificationEmailKey(email)
	m.emailCooldownMu.Lock()
	defer m.emailCooldownMu.Unlock()
	if current, ok := m.emailCooldown[key]; ok && current.Equal(reservedAt) {
		delete(m.emailCooldown, key)
	}
}

func normalizeVerificationEmailKey(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

const maxVerificationEmailCooldownEntries = 100_000

func (m *VerificationManager) pruneEmailCooldownLocked(now time.Time, max int) {
	for email, lastSent := range m.emailCooldown {
		if max <= 0 {
			return
		}
		if now.Sub(lastSent) >= m.cooldownPeriod {
			delete(m.emailCooldown, email)
		}
		max--
	}
}

// CheckIPRateLimit 检查 IP 发送频率限制
// 返回: 是否允许发送, 剩余次数, 错误信息
func (m *VerificationManager) CheckIPRateLimit(ip string) (bool, int, string) {
	key := normalizeRateLimitIP(ip)
	now := time.Now()
	oneMinuteAgo := now.Add(-time.Minute)

	m.ipRateMu.Lock()
	defer m.ipRateMu.Unlock()
	m.pruneIPRateLimitLocked(oneMinuteAgo, 256)
	sendTimes := m.ipRateLimit[key]

	// 检查是否超过限制
	if len(sendTimes) >= m.maxIPPerMin {
		return false, 0, fmt.Sprintf("该 IP 发送过于频繁，请稍后再试")
	}

	return true, m.maxIPPerMin - len(sendTimes), ""
}

// RecordIPSend 记录 IP 发送
func (m *VerificationManager) RecordIPSend(ip string) {
	key := normalizeRateLimitIP(ip)
	m.ipRateMu.Lock()
	defer m.ipRateMu.Unlock()
	if m.ipRateLimit == nil {
		m.ipRateLimit = make(map[string][]time.Time)
	}
	sendTimes := append(m.ipRateLimit[key], time.Now())
	m.ipRateLimit[key] = sendTimes
}

const maxVerificationIPRateEntries = 100_000

func normalizeRateLimitIP(ip string) string {
	key := strings.TrimSpace(ip)
	if key == "" {
		return "unknown"
	}
	if parsed := net.ParseIP(key); parsed != nil {
		return parsed.String()
	}
	return key
}

// AllowIPSend atomically checks and consumes one IP send budget. Keeping the
// check and append under one lock prevents concurrent requests from both
// observing the same remaining budget and bypassing the limit.
func (m *VerificationManager) AllowIPSend(ip string) (bool, int, string) {
	key := normalizeRateLimitIP(ip)
	now := time.Now()
	oneMinuteAgo := now.Add(-time.Minute)
	m.ipRateMu.Lock()
	defer m.ipRateMu.Unlock()
	if m.ipRateLimit == nil {
		m.ipRateLimit = make(map[string][]time.Time)
	}
	m.pruneIPRateLimitLocked(oneMinuteAgo, 256)
	sendTimes := m.ipRateLimit[key]
	if len(sendTimes) >= m.maxIPPerMin {
		return false, 0, "该 IP 发送过于频繁，请稍后再试"
	}
	if _, exists := m.ipRateLimit[key]; !exists && len(m.ipRateLimit) >= maxVerificationIPRateEntries {
		return false, 0, "该 IP 发送过于频繁，请稍后再试"
	}
	sendTimes = append(sendTimes, now)
	m.ipRateLimit[key] = sendTimes
	return true, m.maxIPPerMin - len(sendTimes), ""
}

func (m *VerificationManager) pruneIPRateLimitLocked(cutoff time.Time, max int) {
	for key, sendTimes := range m.ipRateLimit {
		if max <= 0 {
			return
		}
		valid := sendTimes[:0]
		for _, sentAt := range sendTimes {
			if sentAt.After(cutoff) {
				valid = append(valid, sentAt)
			}
		}
		if len(valid) == 0 {
			delete(m.ipRateLimit, key)
		} else {
			m.ipRateLimit[key] = valid
		}
		max--
	}
}

// CreateSession 创建验证会话并发送验证码
func (m *VerificationManager) CreateSession(email string, purpose Purpose) (*VerificationSession, error) {
	reservedAt := time.Now()
	// 检查并占用冷却时间，避免并发请求同时通过检查。
	if canSend, remaining := m.reserveEmailCooldown(email, reservedAt); !canSend {
		return nil, fmt.Errorf("请等待 %v 后再试", remaining.Round(time.Second))
	}

	// 生成验证码和会话ID
	code, err := generateCode()
	if err != nil {
		m.releaseEmailCooldown(email, reservedAt)
		return nil, err
	}
	sessionID, err := generateSessionID()
	if err != nil {
		m.releaseEmailCooldown(email, reservedAt)
		return nil, err
	}

	now := reservedAt
	session := &VerificationSession{
		SessionID: sessionID,
		Email:     email,
		Code:      code,
		Purpose:   purpose,
		Attempts:  0,
		CreatedAt: now,
		ExpiresAt: now.Add(m.codeExpiry),
	}

	// 存储会话
	m.sessions.Store(sessionID, session)

	// 发送验证码邮件
	smtpService := NewSMTPService()
	if err := smtpService.SendVerificationCode(email, code, string(purpose)); err != nil {
		m.sessions.Delete(sessionID)
		m.releaseEmailCooldown(email, reservedAt)
		return nil, fmt.Errorf("发送验证码失败: %v", err)
	}

	log.Printf("验证码会话创建成功: email=%s, purpose=%s, session_id=%s", email, purpose, sessionID)
	return session, nil
}

// Verify 验证验证码
func (m *VerificationManager) Verify(sessionID, code string) (*VerificationSession, error) {
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()
	value, ok := m.sessions.Load(sessionID)
	if !ok {
		return nil, fmt.Errorf("会话不存在或已过期")
	}

	session := value.(*VerificationSession)

	// 检查是否过期
	if time.Now().After(session.ExpiresAt) {
		m.sessions.Delete(sessionID)
		return nil, fmt.Errorf("验证码已过期")
	}

	// 检查尝试次数
	if session.Attempts >= m.maxAttempts {
		m.sessions.Delete(sessionID)
		return nil, fmt.Errorf("尝试次数过多，请重新获取验证码")
	}
	if session.VerifiedAt != nil {
		return nil, fmt.Errorf("验证码已使用")
	}

	// 验证码错误
	if session.Code != code {
		session.Attempts++
		return nil, fmt.Errorf("验证码错误，还剩 %d 次机会", m.maxAttempts-session.Attempts)
	}

	// 验证成功
	now := time.Now()
	session.VerifiedAt = &now

	return session, nil
}

// GetSession 获取会话信息
func (m *VerificationManager) GetSession(sessionID string) (*VerificationSession, bool) {
	if value, ok := m.sessions.Load(sessionID); ok {
		session := value.(*VerificationSession)
		// 检查是否过期
		if time.Now().After(session.ExpiresAt) {
			m.sessions.Delete(sessionID)
			return nil, false
		}
		return session, true
	}
	return nil, false
}

// DeleteSession 删除会话
func (m *VerificationManager) DeleteSession(sessionID string) {
	m.sessions.Delete(sessionID)
}

// cleanupExpired 定期清理过期会话
func (m *VerificationManager) cleanupExpired() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		now := time.Now()
		m.sessions.Range(func(key, value interface{}) bool {
			session := value.(*VerificationSession)
			if now.After(session.ExpiresAt) {
				m.sessions.Delete(key)
			}
			return true
		})
	}
}
