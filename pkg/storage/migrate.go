package storage

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"draarl/internal/config"
)

// 迁移时跳过的前缀：staging 为未完成的临时对象，frontend 为 CDN 同步产物（仅 minio 需要）。
var migrateSkipPrefixes = []string{"staging/", "frontend/"}

// MigrateOptions 控制迁移行为。
type MigrateOptions struct {
	// DryRun 只统计与打印计划，不实际写入目标端。
	DryRun bool
	// DeleteSource 迁移并校验成功后删除源端对象（默认 false，保留源端以便回滚）。
	DeleteSource bool
	// ProgressEvery 每处理多少个对象打印一次进度（<=0 时取默认 100）。
	ProgressEvery int
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

// Migrate 将对象从 from 驱动迁移到 to 驱动。
//
// 断点续传：目标端已存在同 key 且大小一致的对象会被跳过，因此进程意外退出后
// 直接重跑即可，不会重复传输。两个驱动的 Put 均为原子操作，崩溃不会在目标端
// 残留半个对象。
func Migrate(ctx context.Context, cfg *config.Configuration, from, to string, opts MigrateOptions) (*MigrateResult, error) {
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

// migrateWith 在两个具体驱动实例间迁移（核心循环，可独立测试）。
func migrateWith(ctx context.Context, src, dst Storage, opts MigrateOptions) (*MigrateResult, error) {
	every := opts.ProgressEvery
	if every <= 0 {
		every = 100
	}

	res := &MigrateResult{}
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
		res.Scanned++
		if res.Scanned%every == 0 {
			log.Printf("[MIGRATE] 进度: 已扫描=%d 复制=%d 跳过=%d 失败=%d", res.Scanned, res.Copied, res.Skipped, res.Failed)
		}

		// 断点续传：目标端已有同 key 且大小一致则跳过。
		if dstSize, _, statErr := dst.Stat(ctx, key); statErr == nil && dstSize == obj.Size {
			res.Skipped++
			if opts.DeleteSource && !opts.DryRun {
				if err := src.Delete(ctx, key); err == nil {
					res.Deleted++
				}
			}
			return nil
		}

		if opts.DryRun {
			res.Copied++
			return nil
		}

		if err := copyObject(ctx, src, dst, key, obj.Size); err != nil {
			res.Failed++
			log.Printf("[MIGRATE] 复制失败 key=%s: %v", key, err)
			return nil // 单个失败不中断，最后按 Failed 汇总
		}
		res.Copied++
		res.BytesCopied += obj.Size

		if opts.DeleteSource {
			if err := src.Delete(ctx, key); err == nil {
				res.Deleted++
			} else {
				log.Printf("[MIGRATE] 删除源对象失败 key=%s: %v", key, err)
			}
		}
		return nil
	})

	log.Printf("[MIGRATE] 完成: 扫描=%d 复制=%d 跳过=%d 删除源=%d 失败=%d 传输=%d 字节",
		res.Scanned, res.Copied, res.Skipped, res.Deleted, res.Failed, res.BytesCopied)

	if walkErr != nil {
		return res, fmt.Errorf("遍历源存储失败: %w", walkErr)
	}
	if res.Failed > 0 {
		return res, fmt.Errorf("有 %d 个对象迁移失败，可重跑命令续传剩余对象", res.Failed)
	}
	return res, nil
}

func shouldSkipMigrateKey(key string) bool {
	for _, p := range migrateSkipPrefixes {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

// copyObject 从源端读取对象写入目标端，并校验大小。
func copyObject(ctx context.Context, src, dst Storage, key string, size int64) error {
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
