package gormdb

import (
	"context"
	"os"
	"strings"
	"testing"

	"draarl/internal/observability"
	drivermysql "github.com/go-sql-driver/mysql"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestDatabaseSpansFollowRequestAndRestoreContextMySQL(t *testing.T) {
	if os.Getenv("DRAARL_OBSERVABILITY_E2E") != "true" {
		t.Skip("requires disposable MySQL")
	}
	parsed, err := drivermysql.ParseDSN(os.Getenv("DRAARL_TEST_MYSQL_DSN"))
	if err != nil || !strings.HasPrefix(parsed.DBName, "draarl_test_") {
		t.Fatal("disposable draarl_test_ database required")
	}
	if err := Init(&Config{DSN: parsed.FormatDSN(), MaxOpenConns: 3, MaxIdleConns: 1, LogLevel: "silent"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close() })
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })
	ctx, parent := observability.Start(t.Context(), "http.request")
	requestDB := Get().WithContext(ctx)
	queriesBefore, _, _ := observability.DatabaseMetrics()
	for range 2 {
		var result int
		if err := requestDB.Raw("SELECT 1").Scan(&result).Error; err != nil || result != 1 {
			t.Fatalf("query failed: %v", err)
		}
	}
	parent.End()
	spans := exporter.GetSpans()
	children := 0
	for _, span := range spans {
		if span.Name == "http.request" {
			continue
		}
		children++
		if span.Parent.SpanID() != parent.SpanContext().SpanID() || span.SpanContext.TraceID() != parent.SpanContext().TraceID() {
			t.Fatalf("query inherited a previous ended query instead of the HTTP parent: %s", span.Name)
		}
		for _, attr := range span.Attributes {
			if strings.Contains(string(attr.Key), "statement") || strings.Contains(string(attr.Key), "query.text") {
				t.Fatal("SQL text was exported")
			}
		}
	}
	queriesAfter, _, _ := observability.DatabaseMetrics()
	if children != 2 || queriesAfter-queriesBefore != 2 {
		t.Fatalf("spans=%d metric delta=%d", children, queriesAfter-queriesBefore)
	}
}
