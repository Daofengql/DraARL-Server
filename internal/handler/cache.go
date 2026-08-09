package handler

import (
	"net/http"

	gormdb "draarl/internal/gormdb"
	oplog "draarl/internal/log"
	"draarl/pkg/cache"

	"github.com/gin-gonic/gin"
)

// CacheMetricsHandler 缓存监控处理器
type CacheMetricsHandler struct {
	metricsHandler *cache.MetricsHandler
}

// NewCacheMetricsHandler 创建缓存监控处理器
func NewCacheMetricsHandler() *CacheMetricsHandler {
	return &CacheMetricsHandler{
		metricsHandler: cache.NewMetricsHandler(),
	}
}

// GetCacheMetrics 获取缓存监控指标
func (h *CacheMetricsHandler) GetCacheMetrics(c *gin.Context) {
	snapshot := h.metricsHandler.GetSnapshot()

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "success",
		"data": gin.H{
			"l1_hits":        snapshot.L1Hits,
			"l1_misses":      snapshot.L1Misses,
			"l1_hit_rate":    snapshot.L1HitRate,
			"total_requests": snapshot.TotalRequests,
			"invalidations":  snapshot.Invalidations,
			"errors":         snapshot.Errors,
			"latency": gin.H{
				"get_avg_ns": snapshot.GetLatencyAvg.Nanoseconds(),
				"set_avg_ns": snapshot.SetLatencyAvg.Nanoseconds(),
				"del_avg_ns": snapshot.DelLatencyAvg.Nanoseconds(),
			},
			"uptime_seconds": snapshot.Uptime.Seconds(),
		},
	})
}

// ResetCacheMetrics 重置缓存监控指标（慎用）
func (h *CacheMetricsHandler) ResetCacheMetrics(c *gin.Context) {
	h.metricsHandler.GetMetricsInstance().Reset()

	// 记录审计日志
	username, _ := c.Get("username")
	userRepo := gormdb.NewUserRepository()
	currentUser, _ := userRepo.GetUserByName(username.(string))
	if currentUser != nil {
		oplog.AddLog(
			"重置缓存监控指标",
			"cache_metrics_reset",
			currentUser.ID,
			currentUser.Name,
			currentUser.CallSign,
			c.ClientIP(),
		)
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "缓存监控指标已重置",
	})
}

// ClearAllCache 清空所有缓存（慎用，仅用于调试）
func (h *CacheMetricsHandler) ClearAllCache(c *gin.Context) {
	manager := cache.GetManager()
	if manager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"code":    503,
			"message": "缓存管理器未初始化",
		})
		return
	}

	if err := manager.ClearAll(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "清空缓存失败",
		})
		return
	}

	// 记录审计日志
	username, _ := c.Get("username")
	userRepo := gormdb.NewUserRepository()
	currentUser, _ := userRepo.GetUserByName(username.(string))
	if currentUser != nil {
		oplog.AddLog(
			"清空所有缓存",
			"cache_clear_all",
			currentUser.ID,
			currentUser.Name,
			currentUser.CallSign,
			c.ClientIP(),
		)
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "所有缓存已清空",
	})
}
