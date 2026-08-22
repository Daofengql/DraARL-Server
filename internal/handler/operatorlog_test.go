package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGetOperatorLogsRejectsMalformedPagination(t *testing.T) {
	tests := []string{
		"/operator-logs?page_size=bad",
		"/operator-logs?page=bad",
		"/operator-logs?page=9223372036854775807",
	}
	for _, path := range tests {
		gin.SetMode(gin.TestMode)
		router := gin.New()
		router.GET("/operator-logs", GetOperatorLogs)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("path=%q status=%d body=%s", path, recorder.Code, recorder.Body.String())
		}
	}
}
