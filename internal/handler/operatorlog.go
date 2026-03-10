package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"nrllink/internal/log"
)

// GetOperatorLogs 获取操作日志列表
func GetOperatorLogs(c *gin.Context) {
	// 获取查询参数
	limitStr := c.DefaultQuery("limit", "20")
	pageStr := c.DefaultQuery("page", "1")
	operation := c.Query("operation")

	limit, _ := strconv.Atoi(limitStr)
	page, _ := strconv.Atoi(pageStr)

	// 查询日志
	logs, total, err := log.QueryLogs(0, page, limit, operation)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 20000,
			"data": gin.H{
				"isok":    1,
				"message": "查询操作日志失败",
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 20000,
		"data": gin.H{
			"total": total,
			"items": logs,
		},
	})
}

// GetOperatorLogStats 获取操作日志统计信息
func GetOperatorLogStats(c *gin.Context) {
	stats, err := log.GetStats()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 20000,
			"data": gin.H{
				"message": "获取统计信息失败",
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 20000,
		"data": stats,
	})
}
