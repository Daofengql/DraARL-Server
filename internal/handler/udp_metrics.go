package handler

import (
	"net/http"

	"draarl/internal/udphub"

	"github.com/gin-gonic/gin"
)

func GetUDPMetrics(c *gin.Context) {
	c.Header("Cache-Control", "no-store, max-age=0")
	c.JSON(http.StatusOK, gin.H{
		"code":    http.StatusOK,
		"message": "success",
		"data":    udphub.GetUDPPerformanceStats(),
	})
}
