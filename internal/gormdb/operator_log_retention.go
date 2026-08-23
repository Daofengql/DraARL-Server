package gormdb

import (
	"log"
	"sync"
	"time"

	"gorm.io/gorm"
)

// 操作日志保留策略：
// operator_log 无保留策略会无限增长，且统计接口按 DATE()/YEARWEEK() 全表扫描。
// 默认保留 180 天，每天清理一次（有界批量，避免长事务锁表）。

const (
	defaultOperatorLogRetentionDays = 180
	operatorLogPruneBatchSize       = 5000
)

var operatorLogRetentionOnce sync.Once

// StartOperatorLogRetention 启动操作日志保留清理协程（幂等，可在多个入口安全调用）。
// 多次调用时保留第一次调用的 retentionDays，避免重复清理协程并行扫描/删除同一张表。
func StartOperatorLogRetention(retentionDays int) {
	if retentionDays <= 0 {
		retentionDays = defaultOperatorLogRetentionDays
	}
	operatorLogRetentionOnce.Do(func() {
		go func() {
			for {
				now := time.Now()
				// 启动后先清理一次，随后每天清理
				pruneOperatorLogs(retentionDays)
				next := now.Add(24 * time.Hour)
				time.Sleep(time.Until(next))
			}
		}()
		log.Printf("[LOG] 操作日志保留清理已启动: 保留 %d 天", retentionDays)
	})
}

// pruneOperatorLogs 分页删除超过保留期的操作日志（有界批量，避免长事务）。
func pruneOperatorLogs(retentionDays int) {
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	db := Get()
	if db == nil {
		return
	}
	var deleted int64
	for {
		var ids []int
		if err := db.Model(&OperatorLog{}).
			Where("timestamp < ?", cutoff).
			Order("id ASC").Limit(operatorLogPruneBatchSize).
			Pluck("id", &ids).Error; err != nil {
			log.Printf("[LOG] 查询待清理操作日志失败: %v", err)
			return
		}
		if len(ids) == 0 {
			break
		}
		res := db.Where("id IN ?", ids).Delete(&OperatorLog{})
		if res.Error != nil {
			log.Printf("[LOG] 清理操作日志失败: %v", res.Error)
			return
		}
		deleted += res.RowsAffected
		if int64(len(ids)) < operatorLogPruneBatchSize {
			break
		}
	}
	if deleted > 0 {
		log.Printf("[LOG] 已清理 %d 条过期操作日志（保留 %d 天）", deleted, retentionDays)
	}
}

// startOfDay 返回当天 0 点（本地时区）。
func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

var _ = gorm.ErrRecordNotFound
