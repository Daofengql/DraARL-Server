package redis

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"nrllink/internal/config"
)

// 全局 Redis 客户端
var (
	client     *redis.Client
	clientOnce sync.Once
	clientErr  error
)

// Init 初始化 Redis 客户端
// Redis 是必需的服务，初始化失败会返回错误
func Init(cfg *config.Configuration) error {
	clientOnce.Do(func() {
		client = redis.NewClient(&redis.Options{
			Addr:         cfg.GetRedisAddr(),
			Password:     cfg.Redis.Password,
			DB:           cfg.Redis.DB,
			PoolSize:     cfg.Redis.PoolSize,
			MinIdleConns: cfg.Redis.MinIdleConn,
			// 连接超时
			DialTimeout:  5 * time.Second,
			ReadTimeout:  3 * time.Second,
			WriteTimeout: 3 * time.Second,
			// 空闲连接检查
			ConnMaxIdleTime: 5 * time.Minute,
			// 连接最大存活时间
			ConnMaxLifetime: 30 * time.Minute,
		})

		// 测试连接
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := client.Ping(ctx).Err(); err != nil {
			clientErr = fmt.Errorf("redis 连接失败: %w", err)
			return
		}
	})

	return clientErr
}

// GetClient 获取 Redis 客户端实例
func GetClient() *redis.Client {
	if client == nil {
		panic("redis not initialized, call Init() first")
	}
	return client
}

// Close 关闭 Redis 连接
func Close() error {
	if client != nil {
		return client.Close()
	}
	return nil
}

// IsReady 检查 Redis 是否已连接
func IsReady() bool {
	if client == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return client.Ping(ctx).Err() == nil
}

// HealthCheck 健康检查，返回详细状态
func HealthCheck() map[string]interface{} {
	status := map[string]interface{}{
		"initialized": client != nil,
	}

	if client == nil {
		status["status"] = "not_initialized"
		return status
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 获取连接池统计信息
	poolStats := client.PoolStats()
	status["pool_stats"] = map[string]interface{}{
		"total_conns":  poolStats.TotalConns,
		"idle_conns":   poolStats.IdleConns,
		"stale_conns":  poolStats.StaleConns,
		"hits":         poolStats.Hits,
		"misses":       poolStats.Misses,
		"timeouts":     poolStats.Timeouts,
	}

	// Ping 测试
	if err := client.Ping(ctx).Err(); err != nil {
		status["status"] = "error"
		status["error"] = err.Error()
	} else {
		status["status"] = "ok"
	}

	return status
}

// Set 设置键值
func Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return client.Set(ctx, key, value, expiration).Err()
}

// Get 获取值
func Get(ctx context.Context, key string) (string, error) {
	return client.Get(ctx, key).Result()
}

// GetBytes 获取字节数组
func GetBytes(ctx context.Context, key string) ([]byte, error) {
	return client.Get(ctx, key).Bytes()
}

// Del 删除键
func Del(ctx context.Context, keys ...string) error {
	return client.Del(ctx, keys...).Err()
}

// Exists 检查键是否存在
func Exists(ctx context.Context, keys ...string) (int64, error) {
	return client.Exists(ctx, keys...).Result()
}

// Expire 设置过期时间
func Expire(ctx context.Context, key string, expiration time.Duration) error {
	return client.Expire(ctx, key, expiration).Err()
}

// TTL 获取剩余过期时间
func TTL(ctx context.Context, key string) (time.Duration, error) {
	return client.TTL(ctx, key).Result()
}

// Incr 自增
func Incr(ctx context.Context, key string) (int64, error) {
	return client.Incr(ctx, key).Result()
}

// Decr 自减
func Decr(ctx context.Context, key string) (int64, error) {
	return client.Decr(ctx, key).Result()
}

// HSet 设置哈希字段
func HSet(ctx context.Context, key string, field string, value interface{}) error {
	return client.HSet(ctx, key, field, value).Err()
}

// HGet 获取哈希字段
func HGet(ctx context.Context, key, field string) (string, error) {
	return client.HGet(ctx, key, field).Result()
}

// HGetAll 获取所有哈希字段
func HGetAll(ctx context.Context, key string) (map[string]string, error) {
	return client.HGetAll(ctx, key).Result()
}

// HDel 删除哈希字段
func HDel(ctx context.Context, key string, fields ...string) error {
	return client.HDel(ctx, key, fields...).Err()
}

// LPush 列表左侧插入
func LPush(ctx context.Context, key string, values ...interface{}) error {
	return client.LPush(ctx, key, values...).Err()
}

// RPush 列表右侧插入
func RPush(ctx context.Context, key string, values ...interface{}) error {
	return client.RPush(ctx, key, values...).Err()
}

// LRange 获取列表范围
func LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	return client.LRange(ctx, key, start, stop).Result()
}

// SAdd 集合添加成员
func SAdd(ctx context.Context, key string, members ...interface{}) error {
	return client.SAdd(ctx, key, members...).Err()
}

// SMembers 获取集合所有成员
func SMembers(ctx context.Context, key string) ([]string, error) {
	return client.SMembers(ctx, key).Result()
}

// SIsMember 检查是否是集合成员
func SIsMember(ctx context.Context, key string, member interface{}) (bool, error) {
	return client.SIsMember(ctx, key, member).Result()
}

// ZAdd 有序集合添加成员
func ZAdd(ctx context.Context, key string, score float64, member string) error {
	return client.ZAdd(ctx, key, redis.Z{Score: score, Member: member}).Err()
}

// ZRange 有序集合范围查询
func ZRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	return client.ZRange(ctx, key, start, stop).Result()
}

// ZRangeByScore 按分数范围查询
func ZRangeByScore(ctx context.Context, key string, min, max string, offset, count int64) ([]string, error) {
	return client.ZRangeByScore(ctx, key, &redis.ZRangeBy{
		Min:    min,
		Max:    max,
		Offset: offset,
		Count:  count,
	}).Result()
}

// FlushDB 清空当前数据库 (慎用)
func FlushDB(ctx context.Context) error {
	return client.FlushDB(ctx).Err()
}
