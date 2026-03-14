package cache

import (
	"context"
	"log"
	"sync"
	"time"
)

// Manager 缓存管理器（单例模式）
type Manager struct {
	User   *UserCache
	Device *DeviceCache
	Group  *GroupCache
	Config *ConfigCache
	Cert   *CertCache
}

var (
	manager     *Manager
	managerOnce sync.Once
	managerMu   sync.RWMutex
)

// InitManager 初始化缓存管理器（在程序启动时调用）
func InitManager() error {
	var initErr error
	managerOnce.Do(func() {
		// 创建用户缓存
		userCache, err := NewUserCache(UserCacheConfig{
			LocalTTL: 2 * time.Minute,
			RedisTTL: 10 * time.Minute,
			MaxSize:  10000,
		})
		if err != nil {
			initErr = err
			log.Printf("创建用户缓存失败: %v", err)
			return
		}

		// 创建设备缓存
		deviceCache, err := NewDeviceCache(DeviceCacheConfig{
			LocalTTL: time.Minute,
			RedisTTL: 5 * time.Minute,
			MaxSize:  10000,
		})
		if err != nil {
			initErr = err
			log.Printf("创建设备缓存失败: %v", err)
			return
		}

		// 创建群组缓存
		groupCache, err := NewGroupCache(GroupCacheConfig{
			LocalTTL: time.Minute,
			RedisTTL: 5 * time.Minute,
			MaxSize:  10000,
		})
		if err != nil {
			initErr = err
			log.Printf("创建群组缓存失败: %v", err)
			return
		}

		// 创建配置缓存
		configCache, err := NewConfigCache(ConfigCacheConfig{
			LocalTTL: 5 * time.Minute,
			RedisTTL: 30 * time.Minute,
			MaxSize:  1000,
		})
		if err != nil {
			initErr = err
			log.Printf("创建配置缓存失败: %v", err)
			return
		}

		// 创建操作证缓存
		certCache, err := NewCertCache(CertCacheConfig{
			LocalTTL: 5 * time.Minute,
			RedisTTL: 30 * time.Minute,
			MaxSize:  1000,
		})
		if err != nil {
			initErr = err
			log.Printf("创建操作证缓存失败: %v", err)
			return
		}

		manager = &Manager{
			User:   userCache,
			Device: deviceCache,
			Group:  groupCache,
			Config: configCache,
			Cert:   certCache,
		}

		log.Println("缓存管理器初始化成功")
	})

	return initErr
}

// GetManager 获取缓存管理器实例
func GetManager() *Manager {
	managerMu.RLock()
	defer managerMu.RUnlock()
	return manager
}

// GetUserCache 获取用户缓存实例
func GetUserCache() *UserCache {
	m := GetManager()
	if m == nil {
		return nil
	}
	return m.User
}

// GetDeviceCache 获取设备缓存实例
func GetDeviceCache() *DeviceCache {
	m := GetManager()
	if m == nil {
		return nil
	}
	return m.Device
}

// GetGroupCache 获取群组缓存实例
func GetGroupCache() *GroupCache {
	m := GetManager()
	if m == nil {
		return nil
	}
	return m.Group
}

// GetConfigCache 获取配置缓存实例
func GetConfigCache() *ConfigCache {
	m := GetManager()
	if m == nil {
		return nil
	}
	return m.Config
}

// GetCertCache 获取操作证缓存实例
func GetCertCache() *CertCache {
	m := GetManager()
	if m == nil {
		return nil
	}
	return m.Cert
}

// ClearAll 清空所有缓存（慎用，仅用于测试或特殊场景）
func (m *Manager) ClearAll(ctx context.Context) error {
	var lastErr error

	if m.User != nil {
		if err := m.User.cache.Clear(ctx); err != nil {
			lastErr = err
			log.Printf("清空用户缓存失败: %v", err)
		}
	}

	if m.Device != nil {
		if err := m.Device.cache.Clear(ctx); err != nil {
			lastErr = err
			log.Printf("清空设备缓存失败: %v", err)
		}
	}

	if m.Group != nil {
		if err := m.Group.cache.Clear(ctx); err != nil {
			lastErr = err
			log.Printf("清空群组缓存失败: %v", err)
		}
	}

	if m.Config != nil {
		if err := m.Config.cache.Clear(ctx); err != nil {
			lastErr = err
			log.Printf("清空配置缓存失败: %v", err)
		}
	}

	if m.Cert != nil {
		if err := m.Cert.cache.Clear(ctx); err != nil {
			lastErr = err
			log.Printf("清空操作证缓存失败: %v", err)
		}
	}

	return lastErr
}

// Warmup 预热缓存（可选，启动时加载热点数据）
func (m *Manager) Warmup(ctx context.Context) error {
	// 预热系统配置
	if m.Config != nil {
		_, _ = m.Config.GetICPConfig(ctx)
		_, _ = m.Config.GetSystemInfoConfig(ctx)
		_, _ = m.Config.GetAPRSConfig(ctx)
	}

	// 可以根据需要预热其他热点数据

	return nil
}
