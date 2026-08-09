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
	mu     sync.RWMutex
	memory map[string]string
	client *redis.Client
	prefix string
}

var runtimeDeviceMACStore = newDeviceMACStore()

func newDeviceMACStore() *deviceMACStore {
	return &deviceMACStore{
		memory: make(map[string]string),
		prefix: "draarl:device_mac",
	}
}

func initDeviceMACStore(cfg *config.Configuration) {
	runtimeDeviceMACStore = newDeviceMACStore()
	if cfg == nil || strings.TrimSpace(cfg.Redis.Host) == "" || cfg.Redis.Port <= 0 {
		return
	}

	client := redis.NewClient(&redis.Options{
		Addr:         cfg.RedisAddr(),
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		DialTimeout:  time.Duration(cfg.Redis.DialTimeoutSec) * time.Second,
		ReadTimeout:  time.Duration(cfg.Redis.ReadTimeoutSec) * time.Second,
		WriteTimeout: time.Duration(cfg.Redis.WriteTimeoutSec) * time.Second,
		PoolSize:     cfg.Redis.PoolSize,
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
	s.mu.Lock()
	s.memory[key] = mac
	s.mu.Unlock()

	if s.client != nil {
		if err := s.client.Set(context.Background(), s.key(ownerID, ssid), mac, deviceMACStoreTTL).Err(); err != nil {
			log.Printf("[UDP] Device MAC store set failed: owner_id=%d ssid=%d err=%v", ownerID, ssid, err)
		}
	}
}

func (s *deviceMACStore) Get(ownerID int, ssid byte) string {
	if ownerID <= 0 {
		return ""
	}

	key := getOwnerSSIDKey(ownerID, ssid)
	s.mu.RLock()
	if mac := s.memory[key]; mac != "" {
		s.mu.RUnlock()
		return mac
	}
	s.mu.RUnlock()

	if s.client == nil {
		return ""
	}

	mac, err := s.client.Get(context.Background(), s.key(ownerID, ssid)).Result()
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
		s.memory[key] = mac
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
		if err := s.client.Del(context.Background(), s.key(ownerID, ssid)).Err(); err != nil {
			log.Printf("[UDP] Device MAC store delete failed: owner_id=%d ssid=%d err=%v", ownerID, ssid, err)
		}
	}
}

func syncRuntimeDeviceMAC(dev *models.Device) {
	if dev == nil {
		return
	}
	if mac := protocol.NormalizeMAC(dev.MAC); dev.OwnerID > 0 && mac != "" {
		dev.MAC = mac
		runtimeDeviceMACStore.Set(dev.OwnerID, dev.SSID, mac)
	}
}

func removeRuntimeDeviceMAC(dev *models.Device) {
	if dev == nil {
		return
	}
	runtimeDeviceMACStore.Delete(dev.OwnerID, dev.SSID)
}
