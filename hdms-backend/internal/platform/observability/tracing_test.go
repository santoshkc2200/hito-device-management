package observability_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hito-hospital/hdms/internal/platform/observability"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// TestTokenNeverAppearsInSpanAttributes verifies that OpenTelemetry spans
// created either directly or via otelhttp HTTP server instrumentation
// never leak credential tokens or sensitive fields into span attributes.
func TestTokenNeverAppearsInSpanAttributes(t *testing.T) {
	const plaintextToken = "HD-U-7K3M9QXA2F-4"

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithSpanProcessor(observability.NewRedactingSpanProcessor()),
	)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracer := tp.Tracer("test-tracer")

	// 1. Direct span with sensitive attributes and query string target
	_, span := tracer.Start(context.Background(), "test-operation",
		trace.WithAttributes(
			attribute.String("token", plaintextToken),
			attribute.String("password", "superSecret123"),
			attribute.String("kioskToken", "kiosk-bearer-xyz"),
			attribute.String("http.target", "/v1/credentials/resolve?token="+plaintextToken+"&other=1"),
			attribute.String("log.note", "Card scanned: "+plaintextToken),
		),
	)
	span.End()

	// 2. HTTP request through otelhttp with query string token
	otelHandler := otelhttp.NewHandler(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		"http-test",
		otelhttp.WithTracerProvider(tp),
	)

	req := httptest.NewRequest(http.MethodGet, "/v1/credentials/resolve?token="+plaintextToken, nil)
	rec := httptest.NewRecorder()
	otelHandler.ServeHTTP(rec, req)

	// Inspect all exported spans
	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least one span to be recorded")
	}

	for _, s := range spans {
		for _, attr := range s.Attributes {
			valStr := attr.Value.AsString()
			keyStr := string(attr.Key)

			if strings.Contains(valStr, plaintextToken) {
				t.Fatalf("span %q attribute %q value contains plaintext token: %s", s.Name, keyStr, valStr)
			}
			if strings.Contains(keyStr, plaintextToken) {
				t.Fatalf("span %q attribute key contains plaintext token: %s", s.Name, keyStr)
			}
			if strings.Contains(valStr, "superSecret123") {
				t.Fatalf("span %q attribute %q value contains password", s.Name, keyStr)
			}
			if strings.Contains(valStr, "kiosk-bearer-xyz") {
				t.Fatalf("span %q attribute %q value contains kiosk token", s.Name, keyStr)
			}
		}
	}
}
