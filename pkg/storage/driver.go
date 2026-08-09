package storage

import (
	"fmt"
	"strings"
	"sync"

	"draarl/internal/config"
)

// DriverFactory creates a legacy storage driver. Named storage profiles are
// resolved before this registry so multiple endpoints can coexist.
type DriverFactory func(cfg *config.Configuration) (Storage, error)

var (
	driverMu        sync.RWMutex
	driverFactories = map[string]DriverFactory{}
)

func init() {
	RegisterDriver(DriverLocal, newLocalStorage)
	RegisterDriver(DriverMinIO, newMinIOStorage)
	RegisterDriver(DriverS3, newS3Storage)
	// Cloud providers use their S3-compatible endpoint through the same driver.
	RegisterDriver("r2", newS3AliasStorage("r2"))
	RegisterDriver("cos", newS3AliasStorage("cos"))
	RegisterDriver("oss", newS3AliasStorage("oss"))
}

func newS3AliasStorage(provider string) DriverFactory {
	return func(cfg *config.Configuration) (Storage, error) {
		sc := cfg.Storage.S3
		if strings.TrimSpace(sc.Provider) == "" {
			sc.Provider = provider
		}
		return newS3StorageWithConfig(sc, provider)
	}
}

// RegisterDriver 注册存储驱动（可在 init 中调用，支持后续插件式扩展）。
func RegisterDriver(name string, factory DriverFactory) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || factory == nil {
		return
	}
	driverMu.Lock()
	driverFactories[name] = factory
	driverMu.Unlock()
}

func getDriverFactory(name string) (DriverFactory, bool) {
	driverMu.RLock()
	defer driverMu.RUnlock()
	f, ok := driverFactories[strings.ToLower(strings.TrimSpace(name))]
	return f, ok
}

// KnownDrivers 返回已注册驱动名（调试/文档用）。
func KnownDrivers() []string {
	driverMu.RLock()
	defer driverMu.RUnlock()
	out := make([]string, 0, len(driverFactories))
	for k := range driverFactories {
		out = append(out, k)
	}
	return out
}

func createDriver(cfg *config.Configuration) (Storage, error) {
	if profileName, profile, ok := cfg.ActiveStorageProfile(); ok {
		return newProfileStorage(cfg, profileName, profile)
	} else if strings.TrimSpace(cfg.Storage.ActiveProfile) != "" {
		return nil, fmt.Errorf("configured storage profile not found: %s", cfg.Storage.ActiveProfile)
	}
	return NewDriver(cfg, ResolveDriver(cfg))
}

// NewDriver 按名构造一个独立驱动实例（不设为全局 current）。
// 迁移场景下需要同时持有源/目标两个驱动，因此不能走 Init 单例。
func NewDriver(cfg *config.Configuration, name string) (Storage, error) {
	if cfg == nil {
		return nil, fmt.Errorf("storage config is nil")
	}
	profileName := strings.TrimSpace(name)
	if profile, ok := findStorageProfile(cfg, profileName); ok {
		return newProfileStorage(cfg, profileName, profile)
	}

	name = strings.ToLower(profileName)
	factory, ok := getDriverFactory(name)
	if !ok {
		return nil, fmt.Errorf("unsupported storage driver: %s (registered: %v)", name, KnownDrivers())
	}
	return factory(cfg)
}

func findStorageProfile(cfg *config.Configuration, name string) (config.StorageProfile, bool) {
	if cfg == nil || name == "" {
		return config.StorageProfile{}, false
	}
	if profile, ok := cfg.StorageProfile(name); ok {
		return profile, true
	}
	for profileName, profile := range cfg.Storage.Profiles {
		if strings.EqualFold(strings.TrimSpace(profileName), name) {
			return profile, true
		}
	}
	return config.StorageProfile{}, false
}

func newProfileStorage(cfg *config.Configuration, name string, profile config.StorageProfile) (Storage, error) {
	driver := strings.ToLower(strings.TrimSpace(profile.Driver))
	if driver == "" {
		return nil, fmt.Errorf("storage profile %s has no driver", name)
	}
	switch driver {
	case DriverLocal:
		return newLocalStorageWithConfig(profile.Local, cfg.JWT.Secret)
	case DriverS3, DriverMinIO, "r2", "cos", "oss":
		if profile.S3.Provider == "" && driver != DriverS3 {
			profile.S3.Provider = driver
		}
		return newS3StorageWithConfig(profile.S3, driver)
	default:
		return nil, fmt.Errorf("unsupported storage profile driver: %s", driver)
	}
}
