package storage

import (
	"fmt"
	"strings"
	"sync"

	"draarl/internal/config"
)

// DriverFactory 创建存储驱动。新增 OSS/COS/R2 等只需 RegisterDriver。
type DriverFactory func(cfg *config.Configuration) (Storage, error)

var (
	driverMu        sync.RWMutex
	driverFactories = map[string]DriverFactory{}
)

func init() {
	RegisterDriver(DriverLocal, newLocalStorage)
	RegisterDriver(DriverMinIO, newMinIOStorage)
	// 后续扩展示例（实现后取消注释）:
	// RegisterDriver("oss", newAliyunOSSStorage)
	// RegisterDriver("cos", newTencentCOSStorage)
	// RegisterDriver("r2", newCloudflareR2Storage)
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
	return NewDriver(cfg, ResolveDriver(cfg))
}

// NewDriver 按名构造一个独立驱动实例（不设为全局 current）。
// 迁移场景下需要同时持有源/目标两个驱动，因此不能走 Init 单例。
func NewDriver(cfg *config.Configuration, name string) (Storage, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	factory, ok := getDriverFactory(name)
	if !ok {
		return nil, fmt.Errorf("unsupported storage driver: %s (registered: %v)", name, KnownDrivers())
	}
	return factory(cfg)
}
