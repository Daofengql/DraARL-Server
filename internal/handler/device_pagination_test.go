package handler

import (
	"net/http/httptest"
	"testing"

	gormdb "draarl/internal/gormdb"

	"github.com/gin-gonic/gin"
)

func TestDeviceListPaginationRejectsOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	if _, _, _, err := gormdb.NormalizeDevicePageOffset(100, maxInt); err == nil {
		t.Fatal("expected device list page overflow to be rejected")
	}
}

func TestParseOptionalBoolQuery(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		want    *bool
		wantErr bool
	}{
		{name: "missing", query: "", want: nil},
		{name: "true", query: "?isonline=true", want: boolPointer(true)},
		{name: "false", query: "?isonline=false", want: boolPointer(false)},
		{name: "case insensitive", query: "?isonline=TRUE", want: boolPointer(true)},
		{name: "empty", query: "?isonline=", wantErr: true},
		{name: "invalid", query: "?isonline=1", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = httptest.NewRequest("GET", "/api/devices"+tt.query, nil)
			got, err := parseOptionalBoolQuery(context, "isonline")
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%t", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got == nil && tt.want == nil {
				return
			}
			if got == nil || tt.want == nil || *got != *tt.want {
				t.Fatalf("value=%v want=%v", got, tt.want)
			}
		})
	}
}

func boolPointer(value bool) *bool {
	return &value
}
