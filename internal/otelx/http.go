// http.go 提供 OTel HTTP 中间件，为每个 HTTP 请求自动创建 span 并记录 method/path/status/latency。
// 控制面 HTTP 埋点：从请求头提取 W3C Trace Context（traceparent），使上游 trace 贯穿。
package otelx

import (
	"net/http"
	"strconv"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// httpSpanName 由 method + 路由路径生成 span 名（如 "HTTP GET /api/v1/devices"）。
// 用路由路径而非实际 URL（含 query）避免高基数 span 名。
func httpSpanName(method, path string) string {
	if path == "" {
		path = "/"
	}
	return "HTTP " + method + " " + path
}

// statusRecorder 包装 http.ResponseWriter 捕获最终状态码，供 span 记录。
// 透传 Flush() 以支持 SSE（sse.go 依赖 http.Flusher）。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Flush 透传到底层 ResponseWriter（若实现 http.Flusher），支持 SSE。
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// HTTPMiddleware 返回 OTel HTTP 中间件：为每个请求自动创建 span。
//   - 从请求头提取 W3C Trace Context（traceparent），接续上游 trace。
//   - 记录 http.method、http.route、http.status_code、http.duration 属性。
//   - 状态码 >=500 时标记 span status 为 Error。
//
// 该中间件应包在业务 handler 外层（recoveryMiddleware 之内或之外均可，
// 此处置于内层使 panic 被 recovery 转为 500 后仍能记录 span）。
func HTTPMiddleware(handlerName string, next http.Handler) http.Handler {
	tracer := otel.Tracer(handlerName)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 从请求头提取 W3C Trace Context（traceparent + baggage）。
		ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))

		// 用归一化路径作为 span 名（避免 query 产生高基数）。
		path := r.URL.Path
		spanName := httpSpanName(r.Method, path)

		// 创建 span，记录 HTTP 语义约定属性。
		ctx, span := tracer.Start(ctx, spanName,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				attribute.String("http.method", r.Method),
				attribute.String("http.route", path),
				attribute.String("http.scheme", schemeOf(r)),
				attribute.Int("net.host.port", portOf(r)),
			),
		)
		defer span.End()

		// 用 statusRecorder 捕获最终状态码。
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()

		// 将携带 span 的 ctx 注入请求 context。
		r = r.WithContext(ctx)
		next.ServeHTTP(rec, r)

		// 记录状态码与延迟。
		elapsed := time.Since(start)
		span.SetAttributes(
			attribute.Int("http.status_code", rec.status),
			attribute.Int("http.duration_ms", int(elapsed.Milliseconds())),
		)
		// 状态码 >=500 标记 Error。
		if rec.status >= 500 {
			span.SetStatus(codes.Error, http.StatusText(rec.status))
		}
	})
}

// schemeOf 返回请求协议（http/https）。
func schemeOf(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if x := r.Header.Get("X-Forwarded-Proto"); x != "" {
		return x
	}
	return "http"
}

// portOf 返回监听端口（best-effort，从 Host 头解析）。
func portOf(r *http.Request) int {
	if r.Host == "" {
		return 0
	}
	// 剥离 host，取 port。
	host := r.Host
	if i := indexByte(host, ':'); i >= 0 {
		p, err := strconv.Atoi(host[i+1:])
		if err == nil {
			return p
		}
	}
	if schemeOf(r) == "https" {
		return 443
	}
	return 80
}

// indexByte 返回 c 在 s 中首次出现的索引（无则 -1）。等价于 strings.IndexByte 但避免额外 import。
func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
