package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"draarl/internal/config"
)

// 迁移时跳过的前缀：staging 为未完成的临时对象，frontend 为 CDN 同步产物（仅 minio 需要）。
var migrateSkipPrefixes = []string{"staging/", "frontend/"}

var errMigrateHashMismatch = errors.New("source and destination object hashes differ")

// MigrateOptions 控制迁移行为。
type MigrateOptions struct {
	// DryRun 只统计与打印计划，不实际写入目标端。
	DryRun bool
	// DeleteSource 迁移并校验成功后删除源端对象（默认 false，保留源端以便回滚）。
	DeleteSource bool
	// ProgressEvery 每处理多少个对象打印一次进度（<=0 时取默认 100）。
	ProgressEvery int
	// Workers 并发复制 worker 数（0 默认 4，负数或 1 串行，上限 16）。
	Workers int
	// RetryAttempts 为单对象复制的最大尝试次数（<=0 默认 3，上限 5）。
	RetryAttempts int
	// RetryDelay 为对象复制失败后的等待时间（<=0 默认 200ms）。
	RetryDelay time.Duration
	// SkipExistingVerification 跳过目标端同尺寸对象的 SHA-256 校验。默认
	// false；仅在已由外部完整性校验覆盖且需要减少重跑读取成本时启用。
	SkipExistingVerification bool
	// MaxBytesPerSecond 限制所有迁移 worker 合计读取源对象的速率（<=0 不限速）。
	// 这是全局预算，而不是每个 worker 的独立预算，避免并发数放大源端压力。
	MaxBytesPerSecond int64
}

const (
	defaultMigrateWorkers       = 4
	maxMigrateWorkers           = 16
	defaultMigrateRetryAttempts = 3
	maxMigrateRetryAttempts     = 5
	defaultMigrateRetryDelay    = 200 * time.Millisecond
)

func migrateWorkerCount(configured int) int {
	if configured == 0 {
		return defaultMigrateWorkers
	}
	if configured < 1 {
		return 1
	}
	if configured > maxMigrateWorkers {
		return maxMigrateWorkers
	}
	return configured
}

func migrateRetrySettings(opts MigrateOptions) (int, time.Duration) {
	attempts := opts.RetryAttempts
	if attempts <= 0 {
		attempts = defaultMigrateRetryAttempts
	}
	if attempts > maxMigrateRetryAttempts {
		attempts = maxMigrateRetryAttempts
	}
	delay := opts.RetryDelay
	if delay <= 0 {
		delay = defaultMigrateRetryDelay
	}
	return attempts, delay
}

// MigrateResult 迁移结果统计。
type MigrateResult struct {
	Scanned     int
	Copied      int
	Skipped     int // 目标端已存在且大小一致（断点续传）
	Deleted     int // DeleteSource 时删除的源端对象数
	Failed      int
	BytesCopied int64
}

type migrateRateLimiter struct {
	mu     sync.Mutex
	rate   float64
	burst  float64
	tokens float64
	last   time.Time
}

func newMigrateRateLimiter(bytesPerSecond int64) *migrateRateLimiter {
	if bytesPerSecond <= 0 {
		return nil
	}
	rate := float64(bytesPerSecond)
	burst := rate
	if burst < 64*1024 {
		burst = 64 * 1024
	}
	return &migrateRateLimiter{rate: rate, burst: burst, tokens: burst, last: time.Now()}
}

func (l *migrateRateLimiter) wait(ctx context.Context, bytes int) error {
	if l == nil || bytes <= 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	remaining := float64(bytes)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		l.mu.Lock()
		now := time.Now()
		elapsed := now.Sub(l.last).Seconds()
		if elapsed > 0 {
			l.tokens += elapsed * l.rate
			if l.tokens > l.burst {
				l.tokens = l.burst
			}
			l.last = now
		}
		available := l.tokens
		if available > remaining {
			available = remaining
		}
		if available > 0 {
			l.tokens -= available
			remaining -= available
			l.mu.Unlock()
			if remaining <= 0 {
				return nil
			}
			continue
		}
		wait := time.Duration((remaining - l.tokens) / l.rate * float64(time.Second))
		if wait < time.Millisecond {
			wait = time.Millisecond
		}
		l.mu.Unlock()
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

type throttledMigrateReader struct {
	reader io.ReadCloser
	limit  *migrateRateLimiter
	ctx    context.Context
}

func (r throttledMigrateReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		if waitErr := r.limit.wait(r.ctx, n); waitErr != nil {
			return n, waitErr
		}
	}
	return n, err
}

func (r throttledMigrateReader) Close() error { return r.reader.Close() }

// Migrate 将对象从 from 驱动迁移到 to 驱动。
//
// 断点续传：目标端已存在同 key 且大小一致的对象会被跳过，因此进程意外退出后
// 直接重跑即可，不会重复传输。两个驱动的 Put 均为原子操作，崩溃不会在目标端
// 残留半个对象。
func Migrate(ctx context.Context, cfg *config.Configuration, from, to string, opts MigrateOptions) (*MigrateResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	from = strings.ToLower(strings.TrimSpace(from))
	to = strings.ToLower(strings.TrimSpace(to))
	if from == "" || to == "" {
		return nil, fmt.Errorf("源/目标驱动不能为空")
	}
	// 仅支持跨引擎迁移。同引擎（local↔local、minio↔minio）换桶/换路径属于高级场景，
	// 请使用对应的专用工具（如 mc mirror、rsync），本命令不处理。
	if from == to {
		return nil, fmt.Errorf("不支持同引擎迁移（%s -> %s）；同引擎换桶/换路径请使用专用工具（如 mc mirror、rsync）", from, to)
	}

	src, err := NewDriver(cfg, from)
	if err != nil {
		return nil, fmt.Errorf("初始化源驱动 %s 失败: %w", from, err)
	}
	dst, err := NewDriver(cfg, to)
	if err != nil {
		return nil, fmt.Errorf("初始化目标驱动 %s 失败: %w", to, err)
	}

	log.Printf("[MIGRATE] 开始迁移: %s -> %s (dry_run=%t, delete_source=%t)", from, to, opts.DryRun, opts.DeleteSource)
	return migrateWith(ctx, src, dst, opts)
}

// migrateObject 迁移队列中的待处理对象。
type migrateObject struct {
	key  string
	size int64
}

// migrateWith 在两个具体驱动实例间迁移（核心循环，可独立测试）。
// 【并发优化】Workers>1 时有界并发复制（默认 4、上限 16），统计经互斥保护；
// 单个对象失败不中断，最后按 Failed 汇总。
func migrateWith(ctx context.Context, src, dst Storage, opts MigrateOptions) (*MigrateResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	every := opts.ProgressEvery
	if every <= 0 {
		every = 100
	}
	workers := migrateWorkerCount(opts.Workers)
	rateLimiter := newMigrateRateLimiter(opts.MaxBytesPerSecond)

	res := &MigrateResult{}
	var resMu sync.Mutex

	objects := make(chan migrateObject, workers*2)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for obj := range objects {
				processMigrateObject(ctx, src, dst, obj.key, obj.size, opts, rateLimiter, res, &resMu, every)
			}
		}()
	}

	walkErr := src.Walk(ctx, "", func(obj ObjectInfo) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		key := strings.TrimLeft(obj.Key, "/")
		if key == "" || shouldSkipMigrateKey(key) {
			return nil
		}
		resMu.Lock()
		res.Scanned++
		scanned := res.Scanned
		resMu.Unlock()
		if scanned%every == 0 {
			resMu.Lock()
			log.Printf("[MIGRATE] 进度: 已扫描=%d 复制=%d 跳过=%d 失败=%d", scanned, res.Copied, res.Skipped, res.Failed)
			resMu.Unlock()
		}
		select {
		case objects <- migrateObject{key: key, size: obj.Size}:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	close(objects)
	wg.Wait()

	log.Printf("[MIGRATE] 完成: 扫描=%d 复制=%d 跳过=%d 删除源=%d 失败=%d 传输=%d 字节 (workers=%d)",
		res.Scanned, res.Copied, res.Skipped, res.Deleted, res.Failed, res.BytesCopied, workers)

	if walkErr != nil {
		return res, fmt.Errorf("遍历源存储失败: %w", walkErr)
	}
	if res.Failed > 0 {
		return res, fmt.Errorf("有 %d 个对象迁移失败，可重跑命令续传剩余对象", res.Failed)
	}
	return res, nil
}

// processMigrateObject 处理单个对象的迁移（复制/跳过/删除源），统计经互斥保护。
func processMigrateObject(ctx context.Context, src, dst Storage, key string, size int64, opts MigrateOptions, rateLimiter *migrateRateLimiter, res *MigrateResult, resMu *sync.Mutex, every int) {
	attempts, delay := migrateRetrySettings(opts)
	// 断点续传：目标端已有同 key 且大小一致则跳过。
	if dstSize, _, statErr := dst.Stat(ctx, key); statErr == nil && dstSize == size {
		verified := opts.SkipExistingVerification && !opts.DeleteSource
		if !verified {
			// Same-size objects are not necessarily identical. Verify before
			// treating a destination as a resumable skip; DeleteSource always
			// requires this check regardless of the opt-out performance flag.
			verified = retryMigrateObjectHash(ctx, src, dst, key, attempts, delay)
		}
		if verified {
			resMu.Lock()
			res.Skipped++
			resMu.Unlock()
			if opts.DeleteSource && !opts.DryRun {
				if err := retryMigrateErrorOperation(ctx, attempts, delay, func() error {
					return src.Delete(ctx, key)
				}); err == nil {
					resMu.Lock()
					res.Deleted++
					resMu.Unlock()
				} else {
					resMu.Lock()
					res.Failed++
					resMu.Unlock()
					log.Printf("[MIGRATE] 删除源对象失败 key=%s: %v", key, err)
				}
			}
			return
		}
		log.Printf("[MIGRATE] 目标同尺寸对象校验未通过，重新复制 key=%s", key)
	}

	if opts.DryRun {
		resMu.Lock()
		res.Copied++
		resMu.Unlock()
		return
	}

	if err := retryMigrateErrorOperation(ctx, attempts, delay, func() error {
		return copyObject(ctx, src, dst, key, size, rateLimiter)
	}); err != nil {
		resMu.Lock()
		res.Failed++
		resMu.Unlock()
		log.Printf("[MIGRATE] 复制失败 key=%s: %v", key, err)
		return // 单个失败不中断，最后按 Failed 汇总
	}
	resMu.Lock()
	res.Copied++
	res.BytesCopied += size
	resMu.Unlock()

	if opts.DeleteSource {
		// A size check alone cannot prove a successful copy. Verify content
		// before deleting the only known-good source object.
		if !retryMigrateObjectHash(ctx, src, dst, key, attempts, delay) {
			resMu.Lock()
			res.Failed++
			resMu.Unlock()
			log.Printf("[MIGRATE] 复制后哈希校验失败，保留源对象 key=%s", key)
			return
		}
		if err := retryMigrateErrorOperation(ctx, attempts, delay, func() error {
			return src.Delete(ctx, key)
		}); err == nil {
			resMu.Lock()
			res.Deleted++
			resMu.Unlock()
		} else {
			resMu.Lock()
			res.Failed++
			resMu.Unlock()
			log.Printf("[MIGRATE] 删除源对象失败 key=%s: %v", key, err)
		}
	}
}

func retryMigrateErrorOperation(ctx context.Context, attempts int, delay time.Duration, operation func() error) error {
	_, err := retryMigrateOperation(ctx, attempts, delay, func() (struct{}, error) {
		return struct{}{}, operation()
	})
	return err
}

func retryMigrateOperation[T any](ctx context.Context, attempts int, delay time.Duration, operation func() (T, error)) (T, error) {
	var zero T
	if ctx == nil {
		ctx = context.Background()
	}
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		value, err := operation()
		if err == nil {
			return value, nil
		}
		lastErr = err
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return zero, err
		}
		if attempt == attempts {
			break
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return zero, ctx.Err()
		case <-timer.C:
		}
	}
	return zero, fmt.Errorf("operation failed after %d attempts: %w", attempts, lastErr)
}

func shouldSkipMigrateKey(key string) bool {
	for _, p := range migrateSkipPrefixes {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

// migrateVerifyObjectHash 比较源/目标对象内容 SHA-256 是否一致。
func migrateVerifyObjectHash(ctx context.Context, src, dst Storage, key string) bool {
	equal, err := migrateObjectHashEqual(ctx, src, dst, key)
	if err != nil {
		log.Printf("[MIGRATE] 对象哈希校验失败 key=%s: %v", key, err)
	}
	return equal
}

func migrateObjectHashEqual(ctx context.Context, src, dst Storage, key string) (bool, error) {
	srcHash, err := objectSHA256(ctx, src, key)
	if err != nil {
		return false, fmt.Errorf("计算源端哈希: %w", err)
	}
	dstHash, err := objectSHA256(ctx, dst, key)
	if err != nil {
		return false, fmt.Errorf("计算目标端哈希: %w", err)
	}
	if srcHash != dstHash {
		return false, errMigrateHashMismatch
	}
	return true, nil
}

func retryMigrateObjectHash(ctx context.Context, src, dst Storage, key string, attempts int, delay time.Duration) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	if attempts < 1 {
		attempts = 1
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		verified, err := migrateObjectHashEqual(ctx, src, dst, key)
		if err == nil {
			return verified
		}
		// A content mismatch is deterministic for the current object versions;
		// rereading both large objects cannot turn it into a successful retry.
		if errors.Is(err, errMigrateHashMismatch) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false
		}
		if attempt == attempts {
			return false
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return false
		case <-timer.C:
		}
	}
	return false
}

// objectSHA256 计算存储对象内容的 SHA-256 十六进制摘要。
func objectSHA256(ctx context.Context, st Storage, key string) (string, error) {
	reader, err := st.Open(ctx, key)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	h := sha256.New()
	if _, err := io.Copy(h, reader); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// copyObject 从源端读取对象写入目标端，并校验大小。
func copyObject(ctx context.Context, src, dst Storage, key string, size int64, rateLimiter *migrateRateLimiter) error {
	_, contentType, statErr := src.Stat(ctx, key)
	if statErr != nil {
		return fmt.Errorf("stat 源对象: %w", statErr)
	}
	if contentType == "" {
		contentType = GuessContentType(ExtFromFilename(key), "application/octet-stream")
	}

	reader, err := src.Open(ctx, key)
	if err != nil {
		return fmt.Errorf("打开源对象: %w", err)
	}
	defer reader.Close()

	if rateLimiter != nil {
		reader = throttledMigrateReader{reader: reader, limit: rateLimiter, ctx: ctx}
	}
	if err := dst.Put(ctx, key, reader, size, contentType); err != nil {
		return fmt.Errorf("写入目标对象: %w", err)
	}

	// 校验目标端大小，不一致则删除目标残留对象，交由下次续传重传。
	if dstSize, _, err := dst.Stat(ctx, key); err != nil || dstSize != size {
		_ = dst.Delete(ctx, key)
		if err != nil {
			return fmt.Errorf("校验目标对象: %w", err)
		}
		return fmt.Errorf("目标对象大小不一致: got %d want %d", dstSize, size)
	}
	return nil
}

// migrateContext 供 CLI 使用的默认超时上下文（大迁移可能耗时较久）。
func MigrateBackgroundContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 6*time.Hour)
}
