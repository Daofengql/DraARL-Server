package udphub

import (
	"log"
	"time"

	gormdb "draarl/internal/gormdb"
	"draarl/pkg/crypto"
)

// AuthFailure 认证失败记录
type AuthFailure struct {
	IP           string
	Username     string
	FailCount    int
	BlockedUntil time.Time
}

// DeviceAuthResult 设备认证结果
type DeviceAuthResult struct {
	Success  bool
	User     *gormdb.User
	CallSign string
	Blocked  bool
	BlockEnd time.Time
	Error    string
}

var (
	// 性能优化：使用分片锁替代全局锁，减少锁竞争
	authFailures = NewShardedAuthMap() // key: ip:username

	// 阶梯封禁时间
	blockDurations = []time.Duration{
		10 * time.Second,  // 第1次封禁（连续3次失败后）
		30 * time.Second,  // 第2次封禁
		60 * time.Second,  // 第3次封禁
		300 * time.Second, // 第4次及以上
	}
)

// getBlockKey 获取封禁 key
func getBlockKey(ip, username string) string {
	return ip + ":" + username
}

// isBlocked 检查是否被封禁
func isBlocked(ip, username string) (bool, time.Time) {
	key := getBlockKey(ip, username)
	if failure, exists := authFailures.Get(key); exists {
		if !failure.BlockedUntil.IsZero() && time.Now().Before(failure.BlockedUntil) {
			return true, failure.BlockedUntil
		}
	}
	return false, time.Time{}
}

// recordFailure 记录认证失败
func recordFailure(ip, username string) time.Duration {
	key := getBlockKey(ip, username)

	var failure *AuthFailure
	var exists bool
	if failure, exists = authFailures.Get(key); !exists {
		failure = &AuthFailure{
			IP:       ip,
			Username: username,
		}
		authFailures.Set(key, failure)
	} else {
		// 已存在，需要增加计数
		failure.FailCount++
	}

	// 连续失败 3 次后开始封禁
	var blockDuration time.Duration
	if failure.FailCount >= 3 {
		blockLevel := failure.FailCount - 3
		if blockLevel >= len(blockDurations) {
			blockLevel = len(blockDurations) - 1
		}
		blockDuration = blockDurations[blockLevel]
		failure.BlockedUntil = time.Now().Add(blockDuration)
		log.Printf("[AUTH] 封禁 %s:%s，失败次数: %d，封禁时长: %v", ip, username, failure.FailCount, blockDuration)

		// 更新分片中的记录
		authFailures.Set(key, failure)
	}

	return blockDuration
}

// clearFailure 清除失败记录
func clearFailure(ip, username string) {
	key := getBlockKey(ip, username)
	authFailures.Delete(key)
}

// AuthenticateDevice 认证设备
// 返回认证结果，包含用户信息和呼号
func AuthenticateDevice(ip, username, password string) *DeviceAuthResult {
	result := &DeviceAuthResult{}

	// 检查是否被封禁
	if blocked, blockEnd := isBlocked(ip, username); blocked {
		result.Blocked = true
		result.BlockEnd = blockEnd
		result.Error = "too_many_failures"
		log.Printf("[AUTH] 设备认证被阻止（封禁中）: %s:%s，解封时间: %v", ip, username, blockEnd)
		return result
	}

	// 查询用户
	repo := gormdb.NewUserRepository()
	user, err := repo.GetUserByName(username)
	if err != nil || user == nil {
		recordFailure(ip, username)
		result.Error = "user_not_found"
		log.Printf("[AUTH] 设备认证失败（用户不存在）: %s:%s", ip, username)
		return result
	}

	// 检查用户状态
	if user.Status != 1 {
		result.Error = "user_disabled"
		log.Printf("[AUTH] 设备认证失败（用户已禁用）: %s:%s", ip, username)
		return result
	}

	// 检查审核状态（未审核时像密码错误一样丢包处理）
	if user.ApprovalStatus != 1 {
		recordFailure(ip, username)
		result.Error = "invalid_password"
		log.Printf("[AUTH] 设备认证失败（用户未审核）: %s:%s", ip, username)
		return result
	}

	// 检查设备密码是否已设置
	if user.DevicePassword == "" {
		result.Error = "device_password_not_set"
		log.Printf("[AUTH] 设备认证失败（设备密码未设置）: %s:%s", ip, username)
		return result
	}

	// 验证设备密码（兼容历史 bcrypt）
	match, legacyPassword, err := crypto.VerifyDevicePassword(user.DevicePassword, password)
	if err != nil {
		recordFailure(ip, username)
		result.Error = "invalid_password"
		log.Printf("[AUTH] 设备认证失败（密码校验失败）: %s:%s, err: %v", ip, username, err)
		return result
	}

	// 验证密码
	if !match {
		recordFailure(ip, username)
		result.Error = "invalid_password"
		log.Printf("[AUTH] 设备认证失败（密码错误）: %s:%s", ip, username)
		return result
	}

	// 历史 bcrypt 兼容迁移：认证通过后自动迁移为 AES 可逆加密
	if legacyPassword {
		encryptedPassword, encErr := crypto.Encrypt(password)
		if encErr != nil {
			log.Printf("[AUTH] 历史设备密码迁移加密失败: %s:%s, err: %v", ip, username, encErr)
		} else if updateErr := repo.UpdateUserDevicePassword(user.ID, encryptedPassword); updateErr != nil {
			log.Printf("[AUTH] 历史设备密码迁移写库失败: %s:%s, err: %v", ip, username, updateErr)
		} else {
			log.Printf("[AUTH] 历史设备密码已迁移为 AES 存储: %s:%s", ip, username)
		}
	}

	// 认证成功，清除失败记录
	clearFailure(ip, username)
	result.Success = true
	result.User = user
	result.CallSign = user.CallSign

	log.Printf("[AUTH] 设备认证成功: %s:%s (%s)", ip, username, user.CallSign)
	return result
}

// GetAuthFailureCount 获取认证失败次数
func GetAuthFailureCount(ip, username string) int {
	key := getBlockKey(ip, username)
	if failure, exists := authFailures.Get(key); exists {
		return failure.FailCount
	}
	return 0
}

// CleanExpiredAuthFailures 清理过期的认证失败记录
func CleanExpiredAuthFailures() {
	authFailures.CleanExpired(time.Now())
}

// StartAuthCleaner 启动认证失败记录清理器
func StartAuthCleaner() {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-udpShutdown:
				return
			case <-ticker.C:
				CleanExpiredAuthFailures()
			}
		}
	}()
}
