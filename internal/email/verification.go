package email

import (
	"crypto/rand"
	"fmt"
	"log"
	"sync"
	"time"
)

// Purpose 验证码用途
type Purpose string

const (
	PurposeRegister     Purpose = "register"      // 注册
	PurposeLogin        Purpose = "login"         // 登录
	PurposeResetPassword Purpose = "reset_password" // 重置密码
	PurposeChangeEmail  Purpose = "change_email"  // 修改邮箱
)

// VerificationSession 验证会话
type VerificationSession struct {
	SessionID   string    `json:"session_id"`
	Email       string    `json:"email"`
	Code        string    `json:"code"`
	Purpose     Purpose   `json:"purpose"`
	Attempts    int       `json:"attempts"`     // 尝试次数
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	VerifiedAt  *time.Time `json:"verified_at,omitempty"` // 验证成功时间
}

// VerificationManager 验证码会话管理器
type VerificationManager struct {
	sessions sync.Map // session_id -> *VerificationSession
	emailCooldown sync.Map // email -> lastSendTime
	ipRateLimit  sync.Map // ip -> []sendTime (IP 发送记录)

	codeLength    int
	codeExpiry    time.Duration
	cooldownPeriod time.Duration
	maxAttempts   int
	maxIPPerMin   int       // 同一 IP 每分钟最大发送次数
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
		}
		// 启动定时清理任务
		go verificationManager.cleanupExpired()
	})
	return verificationManager
}

// generateCode 生成6位数字验证码
func generateCode() string {
	b := make([]byte, 3)
	rand.Read(b)
	num := int(b[0])<<16 | int(b[1])<<8 | int(b[2])
	return fmt.Sprintf("%06d", num%1000000)
}

// generateSessionID 生成会话ID
func generateSessionID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// CanSend 检查是否可以发送验证码（冷却时间检查）
func (m *VerificationManager) CanSend(email string) (bool, time.Duration) {
	if lastSend, ok := m.emailCooldown.Load(email); ok {
		elapsed := time.Since(lastSend.(time.Time))
		if elapsed < m.cooldownPeriod {
			return false, m.cooldownPeriod - elapsed
		}
	}
	return true, 0
}

// CheckIPRateLimit 检查 IP 发送频率限制
// 返回: 是否允许发送, 剩余次数, 错误信息
func (m *VerificationManager) CheckIPRateLimit(ip string) (bool, int, string) {
	now := time.Now()
	oneMinuteAgo := now.Add(-time.Minute)

	// 获取该 IP 的发送记录
	var sendTimes []time.Time
	if value, ok := m.ipRateLimit.Load(ip); ok {
		sendTimes = value.([]time.Time)
		// 过滤掉一分钟前的记录
		validTimes := make([]time.Time, 0)
		for _, t := range sendTimes {
			if t.After(oneMinuteAgo) {
				validTimes = append(validTimes, t)
			}
		}
		sendTimes = validTimes
	}

	// 检查是否超过限制
	if len(sendTimes) >= m.maxIPPerMin {
		return false, 0, fmt.Sprintf("该 IP 发送过于频繁，请稍后再试")
	}

	return true, m.maxIPPerMin - len(sendTimes), ""
}

// RecordIPSend 记录 IP 发送
func (m *VerificationManager) RecordIPSend(ip string) {
	var sendTimes []time.Time
	if value, ok := m.ipRateLimit.Load(ip); ok {
		sendTimes = value.([]time.Time)
	}
	// 添加当前时间
	sendTimes = append(sendTimes, time.Now())
	m.ipRateLimit.Store(ip, sendTimes)
}

// CreateSession 创建验证会话并发送验证码
func (m *VerificationManager) CreateSession(email string, purpose Purpose) (*VerificationSession, error) {
	// 检查冷却时间
	if canSend, remaining := m.CanSend(email); !canSend {
		return nil, fmt.Errorf("请等待 %v 后再试", remaining.Round(time.Second))
	}

	// 生成验证码和会话ID
	code := generateCode()
	sessionID := generateSessionID()

	now := time.Now()
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
	m.emailCooldown.Store(email, now)

	// 发送验证码邮件
	smtpService := NewSMTPService()
	if err := smtpService.SendVerificationCode(email, code, string(purpose)); err != nil {
		m.sessions.Delete(sessionID)
		m.emailCooldown.Delete(email)
		return nil, fmt.Errorf("发送验证码失败: %v", err)
	}

	log.Printf("验证码会话创建成功: email=%s, purpose=%s, session_id=%s", email, purpose, sessionID)
	return session, nil
}

// Verify 验证验证码
func (m *VerificationManager) Verify(sessionID, code string) (*VerificationSession, error) {
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
