package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"draarl/internal/broadcast/media"
	"draarl/internal/broadcast/repository"
	broadcastruntime "draarl/internal/broadcast/runtime"
	schedruntime "draarl/internal/broadcast/scheduler/runtime"
	"draarl/internal/config"
	"draarl/internal/gormdb"
	oplog "draarl/internal/log"

	"github.com/gin-gonic/gin"
)

const broadcastOperationTimeout = 5 * time.Second

func GetBroadcastMetrics(c *gin.Context) {
	if _, ok := requireBroadcastAdmin(c); !ok {
		return
	}
	persisted, err := repository.Default().PersistedMetrics(c.Request.Context())
	if err != nil {
		writeBroadcastError(c, http.StatusInternalServerError, "broadcast_metrics_failed", "读取自动播报指标失败")
		return
	}
	schedulerMetrics, schedulerAvailable := broadcastruntime.Metrics()
	c.Header("Cache-Control", "no-store, max-age=0")
	c.JSON(http.StatusOK, gin.H{
		"code": http.StatusOK, "message": "成功",
		"data": gin.H{
			"deployment_enabled": broadcastDeploymentEnabled(), "scheduler_available": schedulerAvailable,
			"scheduler": schedulerMetrics, "media": media.GetMetrics(), "persisted": persisted,
		},
	})
}

func GetBroadcastHealth(c *gin.Context) {
	if _, ok := requireBroadcastAdmin(c); !ok {
		return
	}
	deploymentEnabled := broadcastDeploymentEnabled()
	health, err := broadcastruntime.Health(c.Request.Context())
	if errors.Is(err, broadcastruntime.ErrUnavailable) {
		health = schedruntime.HealthSnapshot{Healthy: !deploymentEnabled}
		c.Header("Cache-Control", "no-store, max-age=0")
		c.JSON(http.StatusOK, gin.H{
			"code": http.StatusOK, "message": "成功",
			"data": gin.H{
				"deployment_enabled": deploymentEnabled, "scheduler_available": false,
				"scheduler": health, "media": media.GetMetrics(),
			},
		})
		return
	}
	if err != nil {
		writeBroadcastError(c, http.StatusInternalServerError, "broadcast_health_failed", "读取自动播报健康状态失败")
		return
	}
	c.Header("Cache-Control", "no-store, max-age=0")
	c.JSON(http.StatusOK, gin.H{
		"code": http.StatusOK, "message": "成功",
		"data": gin.H{
			"deployment_enabled": deploymentEnabled, "scheduler_available": true,
			"scheduler": health, "media": media.GetMetrics(),
		},
	})
}

func UpdateBroadcastOperationalState(c *gin.Context) {
	user, ok := requireBroadcastAdmin(c)
	if !ok {
		return
	}
	var request struct {
		Enabled *bool `json:"enabled" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.Enabled == nil {
		writeBroadcastError(c, http.StatusBadRequest, "broadcast_runtime_state_invalid", "请提供自动播报运行开关")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), broadcastOperationTimeout)
	defer cancel()
	health, err := broadcastruntime.SetOperationalEnabled(ctx, *request.Enabled)
	if err != nil {
		writeBroadcastOperationError(c, err)
		return
	}
	action := "broadcast_runtime_disable"
	message := "关闭站点自动播报运行开关"
	if *request.Enabled {
		action = "broadcast_runtime_enable"
		message = "开启站点自动播报运行开关"
	}
	oplog.AddLog(message, action, user.ID, user.Name, user.CallSign, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "自动播报运行状态已更新", "data": health})
}

func EmergencyStopBroadcasts(c *gin.Context) {
	user, ok := requireBroadcastAdmin(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), broadcastOperationTimeout)
	defer cancel()
	stopped, err := broadcastruntime.EmergencyStop(ctx)
	if err != nil {
		writeBroadcastOperationError(c, err)
		return
	}
	oplog.AddLog(
		fmt.Sprintf("紧急停止全部自动播报: 已停止 %d 个任务", stopped), "broadcast_emergency_stop",
		user.ID, user.Name, user.CallSign, c.ClientIP(),
	)
	c.JSON(http.StatusOK, gin.H{
		"code": http.StatusOK, "message": "自动播报紧急停止已完成", "data": gin.H{"stopped_runs": stopped},
	})
}

func broadcastDeploymentEnabled() bool {
	cfg := config.TryGet()
	return cfg != nil && cfg.Broadcast.Enabled
}

func isReservedBroadcastRuntimeKey(key string) bool {
	key = strings.TrimSpace(key)
	return strings.EqualFold(key, repository.OperationalEnabledKey) ||
		strings.EqualFold(key, repository.OperationalEnabledKey+".emergency_fence")
}

func requireBroadcastAdmin(c *gin.Context) (*gormdb.User, bool) {
	user, ok := requireCurrentUser(c)
	if !ok {
		return nil, false
	}
	if !isAdminUser(user) {
		writeBroadcastError(c, http.StatusForbidden, "broadcast_admin_required", "仅站点管理员可以执行该操作")
		return nil, false
	}
	return user, true
}

func writeBroadcastOperationError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, broadcastruntime.ErrUnavailable), errors.Is(err, schedruntime.ErrSchedulerStopped):
		writeBroadcastError(c, http.StatusServiceUnavailable, "broadcast_scheduler_unavailable", "自动播报调度器暂不可用")
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		writeBroadcastError(c, http.StatusGatewayTimeout, "broadcast_operation_timeout", "自动播报运行操作超时")
	default:
		writeBroadcastError(c, http.StatusInternalServerError, "broadcast_operation_failed", "自动播报运行操作失败")
	}
}
