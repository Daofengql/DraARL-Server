package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// writeErrorResponse preserves the HTTP code contract and existing domain error
// codes while supplying a stable machine-readable code for every other error.
func writeErrorResponse(c *gin.Context, status int, payload gin.H) {
	if payload == nil {
		payload = gin.H{}
	}
	payload["code"] = status
	if _, exists := payload["error_code"]; !exists {
		payload["error_code"] = strings.ToLower(strings.ReplaceAll(http.StatusText(status), " ", "_"))
	}
	if payload["message"] == nil || payload["message"] == "" {
		payload["message"] = "请求未能完成，请重试"
	}
	if status >= http.StatusInternalServerError {
		// Internal diagnostics belong in server logs, never in API responses.
		delete(payload, "error")
		delete(payload, "detail")
	}
	c.JSON(status, payload)
}

func writeRepositoryError(c *gin.Context, err error, fallback string) {
	switch {
	case errors.Is(err, context.Canceled):
		writeErrorResponse(c, http.StatusRequestTimeout, gin.H{"message": "请求已取消", "error_code": "request_canceled"})
	case errors.Is(err, context.DeadlineExceeded):
		writeErrorResponse(c, http.StatusGatewayTimeout, gin.H{"message": "处理超时，请重试", "error_code": "request_timeout"})
	case errors.Is(err, gorm.ErrRecordNotFound):
		writeErrorResponse(c, http.StatusNotFound, gin.H{"message": "请求的资源不存在", "error_code": "resource_not_found"})
	default:
		writeErrorResponse(c, http.StatusInternalServerError, gin.H{"message": fallback})
	}
}
