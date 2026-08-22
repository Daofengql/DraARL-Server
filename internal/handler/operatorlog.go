package handler

import (
	"fmt"
	"net/http"
	"strconv"

	gormdb "draarl/internal/gormdb"

	"github.com/gin-gonic/gin"
)

// GetOperatorLogs 获取操作日志列表
func GetOperatorLogs(c *gin.Context) {
	// 获取查询参数
	limitStr := c.Query("page_size")
	if limitStr == "" {
		limitStr = c.DefaultQuery("limit", "20")
	}
	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "无效的日志分页大小"})
		return
	}

	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "无效的日志页码"})
		return
	}
	eventType := c.Query("event_type")

	limit, page, _, err = gormdb.NormalizeOperatorLogPagination(limit, page)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": fmt.Sprintf("无效的日志分页参数: %v", err)})
		return
	}

	repo := gormdb.NewOperatorLogRepository()

	var logs []*gormdb.OperatorLog
	var total int64

	// 根据是否指定事件类型选择不同的查询方法
	if eventType != "" {
		logs, total, err = repo.ListLogsByEventType(eventType, limit, page)
	} else {
		logs, total, err = repo.ListLogs(limit, page)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询操作日志失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"total": total,
			"items": logs,
		},
	})
}

// GetOperatorLogStats 获取操作日志统计信息
func GetOperatorLogStats(c *gin.Context) {
	repo := gormdb.NewOperatorLogRepository()
	stats, err := repo.GetLogStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取统计信息失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data":    stats,
	})
}
