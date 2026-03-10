package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	gormdb "nrllink/internal/gormdb"
)

// GetOperatorLogs 获取操作日志列表
func GetOperatorLogs(c *gin.Context) {
	// 获取查询参数
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	eventType := c.Query("operation")

	if limit <= 0 {
		limit = 20
	}
	if page <= 0 {
		page = 1
	}

	repo := gormdb.NewOperatorLogRepository()

	var logs []*gormdb.OperatorLog
	var total int64
	var err error

	// 根据是��指定事件类型选择不同的查询方法
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
