package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestErrorResponsesKeepDomainCodesAndHideDiagnostics(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	writeErrorResponse(c, 500, gin.H{"code": 400, "message": "保存失败", "error_code": "save_failed", "error": "SQL secret", "detail": "private path"})
	var data map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if data["code"] != float64(500) || data["error_code"] != "save_failed" || data["error"] != nil || data["detail"] != nil {
		t.Fatalf("invalid response: %v", data)
	}
}

func TestRepositoryErrorsDistinguishCanceledAndMissing(t *testing.T) {
	for _, tt := range []struct {
		err    error
		status int
	}{{context.Canceled, http.StatusRequestTimeout}, {context.DeadlineExceeded, http.StatusGatewayTimeout}, {gorm.ErrRecordNotFound, http.StatusNotFound}} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		writeRepositoryError(c, tt.err, "查询失败")
		if w.Code != tt.status {
			t.Errorf("%v: status=%d", tt.err, w.Code)
		}
	}
}
