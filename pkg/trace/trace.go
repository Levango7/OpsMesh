// Package trace provides a shared OpenTelemetry tracing library for OpsMesh services.
// It wraps internal/otelx to expose a public API for distributed tracing across
// HTTP and gRPC boundaries.
//
// Features:
//   - Tracer initialization with OTLP gRPC or stdout export
//   - W3C Trace Context propagation (HTTP headers + gRPC metadata)
//   - HTTP middleware for automatic span creation
//   - gRPC unary interceptors (client + server)
//   - Context injection/extraction helpers
//   - Span utilities (StartSpan, RecordError)
package trace

import (
	"context"
	"net/http"

	"google.golang.org/grpc"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"opsmesh/internal/otelx"
)

// InitTracer initializes the OTel tracer provider for the given service.
// The endpoint is the OTLP gRPC collector address (e.g. "localhost:4317").
// If endpoint is empty, tracing is disabled (no-op mode, zero overhead).
// Returns a shutdown function that must be called on exit to flush spans.
func InitTracer(serviceName, endpoint string) (func(context.Context) error, error) {
	return otelx.Init(otelx.Config{
		ServiceName: serviceName,
		Endpoint:    endpoint,
	})
}

// ExtractContext extracts trace context from HTTP headers and returns
// a new context with the propagated span. Use this in HTTP handlers
// to continue traces from upstream callers.
func ExtractContext(ctx context.Context, header http.Header) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(header))
}

// InjectContext injects the current trace context into HTTP headers.
// Use this before making HTTP calls to propagate traces downstream.
func InjectContext(ctx context.Context, header http.Header) {
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(header))
}

// StartSpan starts a new named span from the context.
// Returns the updated context and the span. Caller must call span.End().
func StartSpan(ctx context.Context, name string) (context.Context, trace.Span) {
	return otelx.StartSpan(ctx, name)
}

// RecordError records an error on the span and marks it as errored.
func RecordError(span trace.Span, err error) {
	otelx.RecordError(span, err)
}

// HTTPMiddleware returns an HTTP middleware that creates spans for each request.
// The serviceName is used as the tracer name (e.g. "opsmesh/alert-svc").
// It extracts W3C Trace Context from incoming headers and records method/path/status.
func HTTPMiddleware(serviceName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return otelx.HTTPMiddleware(serviceName, next)
	}
}

// GRPCServerInterceptor returns a gRPC unary server interceptor that
// extracts trace context from incoming metadata and creates server spans.
func GRPCServerInterceptor() grpc.UnaryServerInterceptor {
	return otelx.GRPCServerUnaryInterceptor("opsmesh")
}

// GRPCClientInterceptor returns a gRPC unary client interceptor that
// injects trace context into outgoing metadata and creates client spans.
func GRPCClientInterceptor() grpc.UnaryClientInterceptor {
	return otelx.GRPCClientUnaryInterceptor("opsmesh")
}

// TraceIDFromContext extracts the trace ID string from the context.
// Returns empty string if no valid span is present.
func TraceIDFromContext(ctx context.Context) string {
	return otelx.TraceIDFromContext(ctx)
}

// SpanIDFromContext extracts the span ID string from the context.
// Returns empty string if no valid span is present.
func SpanIDFromContext(ctx context.Context) string {
	return otelx.SpanIDFromContext(ctx)
}
