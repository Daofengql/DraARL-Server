// Package observability provides bounded, opt-in tracing and structured logs.
package observability

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type Config struct {
	LogFormat        string  `yaml:"LogFormat" json:"log_format"`
	TracingEnabled   bool    `yaml:"TracingEnabled" json:"tracing_enabled"`
	OTLPEndpoint     string  `yaml:"OTLPEndpoint" json:"otlp_endpoint"`
	ServiceName      string  `yaml:"ServiceName" json:"service_name"`
	TraceSampleRatio float64 `yaml:"TraceSampleRatio" json:"trace_sample_ratio"`
}

var tracingEnabled atomic.Bool

func LogHandler(format string, output io.Writer) slog.Handler {
	options := &slog.HandlerOptions{ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
		if attr.Key == slog.TimeKey {
			return slog.String("timestamp", attr.Value.Time().UTC().Format(time.RFC3339Nano))
		}
		return attr
	}}
	if format == "json" {
		return slog.NewJSONHandler(output, options)
	}
	return slog.NewTextHandler(output, options)
}

func Init(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	slog.SetDefault(slog.New(LogHandler(cfg.LogFormat, os.Stderr)))
	noop := func(context.Context) error { return nil }
	if !cfg.TracingEnabled {
		return noop, nil
	}
	if cfg.OTLPEndpoint == "" {
		return noop, fmt.Errorf("Observability.OTLPEndpoint is required when tracing is enabled")
	}
	endpoint, err := url.Parse(cfg.OTLPEndpoint)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return noop, fmt.Errorf("Observability.OTLPEndpoint must be an absolute HTTP or HTTPS URL")
	}
	if endpoint.Path == "" || endpoint.Path == "/" {
		endpoint.Path = "/v1/traces"
	}
	initCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	exporter, err := otlptracehttp.New(initCtx, otlptracehttp.WithEndpointURL(endpoint.String()), otlptracehttp.WithTimeout(3*time.Second))
	if err != nil {
		return noop, err
	}
	name := cfg.ServiceName
	if name == "" {
		name = "draarl-server"
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter, sdktrace.WithMaxQueueSize(2048), sdktrace.WithExportTimeout(3*time.Second)),
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", name))),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.TraceSampleRatio))),
	)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	tracingEnabled.Store(true)
	return func(ctx context.Context) error { tracingEnabled.Store(false); return provider.Shutdown(ctx) }, nil
}

func Start(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return otel.Tracer("draarl").Start(ctx, name, trace.WithAttributes(attrs...))
}

// StartUDP only creates sampled spans when tracing was explicitly enabled.
func StartUDP() func() {
	if !tracingEnabled.Load() {
		return func() {}
	}
	_, span := Start(context.Background(), "udp.process")
	return func() { span.End() }
}
