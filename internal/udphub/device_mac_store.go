package udphub

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"draarl/internal/config"
	"draarl/internal/models"
	"draarl/internal/protocol"

	"github.com/redis/go-redis/v9"
)

const deviceMACStoreTTL = 10 * time.Minute

type deviceMACStore struct {
	mu           sync.RWMutex
	memory       map[string]deviceMACEntry
	client       *redis.Client
	prefix       string
	maxEntries   int
	operationTTL time.Duration
}

type deviceMACEntry struct {
	mac       string
	expiresAt time.Time
}

var runtimeDeviceMACStore = newDeviceMACStore()

const (
	deviceMACStoreMaxEntries      = 100_000
	deviceMACStoreOperationTTL    = 500 * time.Millisecond
	deviceMACStoreEvictionScanMax = 128
)

func newDeviceMACStore() *deviceMACStore {
	return &deviceMACStore{
		memory:       make(map[string]deviceMACEntry),
		prefix:       "draarl:device_mac",
		maxEntries:   deviceMACStoreMaxEntries,
		operationTTL: deviceMACStoreOperationTTL,
	}
}

func initDeviceMACStore(cfg *config.Configuration) {
	previous := runtimeDeviceMACStore
	if previous != nil {
		previous.Close()
	}
	runtimeDeviceMACStore = newDeviceMACStore()
	if cfg == nil || strings.TrimSpace(cfg.Redis.Host) == "" || cfg.Redis.Port <= 0 {
		return
	}

	client := redis.NewClient(&redis.Options{
		Addr:                  cfg.RedisAddr(),
		Password:              cfg.Redis.Password,
		DB:                    cfg.Redis.DB,
		DialTimeout:           time.Duration(cfg.Redis.DialTimeoutSec) * time.Second,
		ReadTimeout:           time.Duration(cfg.Redis.ReadTimeoutSec) * time.Second,
		WriteTimeout:          time.Duration(cfg.Redis.WriteTimeoutSec) * time.Second,
		PoolSize:              cfg.Redis.PoolSize,
		ContextTimeoutEnabled: true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Redis.DialTimeoutSec)*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		log.Printf("[UDP] Device MAC store fallback to memory: redis unavailable: %v", err)
		_ = client.Close()
		return
	}

	runtimeDeviceMACStore.client = client
	runtimeDeviceMACStore.prefix = normalizeDeviceMACPrefix(cfg.Redis.Prefix)
	log.Printf("[UDP] Device MAC store enabled: redis(%s)", cfg.RedisAddr())
}

func normalizeDeviceMACPrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "draarl"
	}
	return prefix + ":device_mac"
}

func (s *deviceMACStore) key(ownerID int, ssid byte) string {
	return fmt.Sprintf("%s:%d:%d", s.prefix, ownerID, ssid)
}

func (s *deviceMACStore) Set(ownerID int, ssid byte, mac string) {
	mac = protocol.NormalizeMAC(mac)
	if ownerID <= 0 || mac == "" {
		return
	}

	key := getOwnerSSIDKey(ownerID, ssid)
	now := time.Now()
	s.mu.Lock()
	if _, exists := s.memory[key]; !exists && len(s.memory) >= s.maxEntries && !s.evictMemoryLocked(now) {
		s.mu.Unlock()
		return
	}
	s.memory[key] = deviceMACEntry{mac: mac, expiresAt: now.Add(deviceMACStoreTTL)}
	s.mu.Unlock()

	if s.client != nil {
		ctx, cancel := s.operationContext()
		err := s.client.Set(ctx, s.key(ownerID, ssid), mac, deviceMACStoreTTL).Err()
		cancel()
		if err != nil {
			log.Printf("[UDP] Device MAC store set failed: owner_id=%d ssid=%d err=%v", ownerID, ssid, err)
		}
	}
}

func (s *deviceMACStore) Get(ownerID int, ssid byte) string {
	if ownerID <= 0 {
		return ""
	}

	key := getOwnerSSIDKey(ownerID, ssid)
	now := time.Now()
	s.mu.RLock()
	entry, exists := s.memory[key]
	if exists && entry.mac != "" && now.Before(entry.expiresAt) {
		s.mu.RUnlock()
		return entry.mac
	}
	s.mu.RUnlock()
	if exists {
		s.mu.Lock()
		if current, ok := s.memory[key]; ok && !now.Before(current.expiresAt) {
			delete(s.memory, key)
		}
		s.mu.Unlock()
	}

	if s.client == nil {
		return ""
	}

	ctx, cancel := s.operationContext()
	mac, err := s.client.Get(ctx, s.key(ownerID, ssid)).Result()
	cancel()
	if err == redis.Nil {
		return ""
	}
	if err != nil {
		log.Printf("[UDP] Device MAC store get failed: owner_id=%d ssid=%d err=%v", ownerID, ssid, err)
		return ""
	}

	mac = protocol.NormalizeMAC(mac)
	if mac != "" {
		s.mu.Lock()
		if _, exists := s.memory[key]; !exists && len(s.memory) >= s.maxEntries && !s.evictMemoryLocked(now) {
			s.mu.Unlock()
			return mac
		}
		s.memory[key] = deviceMACEntry{mac: mac, expiresAt: time.Now().Add(deviceMACStoreTTL)}
		s.mu.Unlock()
	}
	return mac
}

func (s *deviceMACStore) Delete(ownerID int, ssid byte) {
	if ownerID <= 0 {
		return
	}

	key := getOwnerSSIDKey(ownerID, ssid)
	s.mu.Lock()
	delete(s.memory, key)
	s.mu.Unlock()

	if s.client != nil {
		ctx, cancel := s.operationContext()
		err := s.client.Del(ctx, s.key(ownerID, ssid)).Err()
		cancel()
		if err != nil {
			log.Printf("[UDP] Device MAC store delete failed: owner_id=%d ssid=%d err=%v", ownerID, ssid, err)
		}
	}
}

func (s *deviceMACStore) Close() {
	if s == nil || s.client == nil {
		return
	}
	if err := s.client.Close(); err != nil {
		log.Printf("[UDP] Device MAC store close failed: %v", err)
	}
	s.client = nil
}

func (s *deviceMACStore) operationContext() (context.Context, context.CancelFunc) {
	timeout := s.operationTTL
	if timeout <= 0 {
		timeout = deviceMACStoreOperationTTL
	}
	return context.WithTimeout(context.Background(), timeout)
}

func (s *deviceMACStore) evictMemoryLocked(now time.Time) bool {
	scanned := 0
	for key, entry := range s.memory {
		if scanned >= deviceMACStoreEvictionScanMax {
			break
		}
		scanned++
		if !now.Before(entry.expiresAt) {
			delete(s.memory, key)
			return true
		}
	}
	return false
}

func syncRuntimeDeviceMAC(dev *models.Device) {
	if dev == nil {
		return
	}
	state := dev.RuntimeSnapshot()
	if mac := protocol.NormalizeMAC(state.MAC); state.OwnerID > 0 && mac != "" {
		if mac != state.MAC {
			dev.UpdateRuntime(func(current *models.Device) {
				current.MAC = mac
			})
		}
		runtimeDeviceMACStore.Set(state.OwnerID, state.SSID, mac)
	}
}

func removeRuntimeDeviceMAC(dev *models.Device) {
	if dev == nil {
		return
	}
	state := dev.RuntimeSnapshot()
	runtimeDeviceMACStore.Delete(state.OwnerID, state.SSID)
}
