package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestStructuredLogHasUTCAndFields(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(LogHandler("json", &output))
	logger.Info("device connected", "event", "device_online", "device_id", 42, "user_id", 7, "trace_id", "abc")
	var fields map[string]any
	if err := json.Unmarshal(output.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	if fields["device_id"] != float64(42) || fields["event"] != "device_online" || fields["trace_id"] != "abc" {
		t.Fatalf("missing fields: %v", fields)
	}
	stamp, err := time.Parse(time.RFC3339Nano, fields["timestamp"].(string))
	if err != nil || stamp.Location() != time.UTC {
		t.Fatalf("timestamp=%v err=%v", stamp, err)
	}
}

func TestOTLPExportIsExplicitAndFlushes(t *testing.T) {
	var requests atomic.Int64
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/traces" {
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || len(body) == 0 {
			t.Errorf("missing OTLP payload: %v", err)
		}
		requests.Add(1)
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	defer collector.Close()
	shutdown, err := Init(context.Background(), Config{LogFormat: "json", TracingEnabled: true, OTLPEndpoint: collector.URL, TraceSampleRatio: 1})
	if err != nil {
		t.Fatal(err)
	}
	ctx, span := Start(context.Background(), "http.test")
	_, query := Start(ctx, "db.devices")
	query.End()
	span.End()
	StartUDP()()
	flush, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown(flush); err != nil {
		t.Fatal(err)
	}
	if requests.Load() == 0 {
		t.Fatal("no OTLP export received")
	}
	if tracingEnabled.Load() {
		t.Fatal("tracing remained enabled after shutdown")
	}
}
