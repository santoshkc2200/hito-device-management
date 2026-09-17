package observability

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/hito-hospital/hdms/internal/platform/httpx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

var tokenPattern = regexp.MustCompile(`HD-[UD]-[0-9A-Z]{10}-[0-9A-Z]`)

// RedactingSpanProcessor intercepts spans at creation and redacts credential tokens
// and sensitive fields (password, tokens, etc.) from span attributes.
type RedactingSpanProcessor struct{}

// NewRedactingSpanProcessor creates a span processor that strips or redacts
// sensitive fields and tokens matching the platform sensitive-field denylist.
func NewRedactingSpanProcessor() sdktrace.SpanProcessor {
	return &RedactingSpanProcessor{}
}

func (p *RedactingSpanProcessor) OnStart(parent context.Context, s sdktrace.ReadWriteSpan) {
	attrs := s.Attributes()
	for _, attr := range attrs {
		keyStr := string(attr.Key)
		if httpx.IsSensitiveField(keyStr) {
			s.SetAttributes(attribute.String(keyStr, "[REDACTED]"))
			continue
		}
		if keyStr == "http.target" || keyStr == "http.url" || keyStr == "url.full" || keyStr == "url.query" {
			val := attr.Value.AsString()
			sanitized := SanitizeURL(val)
			if sanitized != val {
				s.SetAttributes(attribute.String(keyStr, sanitized))
			}
			continue
		}
		if attr.Value.Type() == attribute.STRING {
			val := attr.Value.AsString()
			if tokenPattern.MatchString(val) {
				s.SetAttributes(attribute.String(keyStr, tokenPattern.ReplaceAllString(val, "[REDACTED]")))
			}
		}
	}
}

func (p *RedactingSpanProcessor) OnEnd(s sdktrace.ReadOnlySpan)        {}
func (p *RedactingSpanProcessor) Shutdown(ctx context.Context) error   { return nil }
func (p *RedactingSpanProcessor) ForceFlush(ctx context.Context) error { return nil }

// SanitizeURL redacts any sensitive query parameters from a raw URL string.
func SanitizeURL(raw string) string {
	if !strings.Contains(raw, "?") {
		return tokenPattern.ReplaceAllString(raw, "[REDACTED]")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return tokenPattern.ReplaceAllString(raw, "[REDACTED]")
	}
	q := u.Query()
	changed := false
	for k := range q {
		if httpx.IsSensitiveField(k) {
			q.Set(k, "[REDACTED]")
			changed = true
		}
	}
	if changed {
		u.RawQuery = q.Encode()
		return tokenPattern.ReplaceAllString(u.String(), "[REDACTED]")
	}
	return tokenPattern.ReplaceAllString(raw, "[REDACTED]")
}

// SanitizeSpanAttributes filters or redacts sensitive key-values before adding to spans.
func SanitizeSpanAttributes(attrs ...attribute.KeyValue) []attribute.KeyValue {
	sanitized := make([]attribute.KeyValue, 0, len(attrs))
	for _, attr := range attrs {
		k := string(attr.Key)
		if httpx.IsSensitiveField(k) {
			sanitized = append(sanitized, attribute.String(k, "[REDACTED]"))
			continue
		}
		if attr.Value.Type() == attribute.STRING {
			v := attr.Value.AsString()
			if k == "http.target" || k == "http.url" || k == "url.full" || k == "url.query" {
				v = SanitizeURL(v)
			} else {
				v = tokenPattern.ReplaceAllString(v, "[REDACTED]")
			}
			sanitized = append(sanitized, attribute.String(k, v))
		} else {
			sanitized = append(sanitized, attr)
		}
	}
	return sanitized
}

// InitTracing installs the global TracerProvider. With no OTLP endpoint
// configured (the local default) it exports human-readable spans to stdout,
// so tracing is visible in `task dev` without standing up a collector.
func InitTracing(ctx context.Context, serviceName, otlpEndpoint string) (shutdown func(context.Context) error, err error) {
	exporter, err := newExporter(ctx, otlpEndpoint)
	if err != nil {
		return nil, fmt.Errorf("observability: tracing exporter: %w", err)
	}

	res, err := resource.Merge(resource.Default(),
		resource.NewSchemaless(semconv.ServiceName(serviceName)),
	)
	if err != nil {
		return nil, fmt.Errorf("observability: resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithSpanProcessor(NewRedactingSpanProcessor()),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	return tp.Shutdown, nil
}

func newExporter(ctx context.Context, otlpEndpoint string) (sdktrace.SpanExporter, error) {
	if otlpEndpoint == "" {
		return stdouttrace.New(stdouttrace.WithPrettyPrint())
	}
	return otlptracehttp.New(ctx, otlptracehttp.WithEndpoint(otlpEndpoint), otlptracehttp.WithInsecure())
}
