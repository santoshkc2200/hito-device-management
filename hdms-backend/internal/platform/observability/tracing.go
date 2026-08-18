package observability

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

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
