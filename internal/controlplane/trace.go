// trace.go 实现可观测性链路追踪查询端点（与 Jaeger / OTel collector 对齐）。
//
// 路径与 Jaeger 查询端原生路径一致，不带 /api/v1 前缀：
//   GET /api/services                列出所有服务
//   GET /api/services/{service}/operations  列出服务的操作
//   GET /api/traces                查询 traces（service, operation, start, end, limit, lookback, ...）
//   GET /api/traces/{traceID}      获取 trace 详情（spans, duration, timestamps）
//
// 当前为 graceful 降级实现：未接入真实 Jaeger 时返回空结果，避免前端 404。
// 后续可在此处添加向 Jaeger /api/services、/api/traces 等端点的转发代理。
package controlplane

import (
	"encoding/json"
	"net/http"
	"strings"
)

// TraceService 服务条目。
type TraceService struct {
	Service string `json:"service"`
}

// TraceOperation 操作条目。
type TraceOperation struct {
	Operation string `json:"operation"`
}

// TraceQueryResult trace 查询结果。
type TraceQueryResult struct {
	Data []TraceSummary `json:"data"`
	Total int `json:"total"`
}

// TraceSummary trace 摘要。
type TraceSummary struct {
	TraceID      string `json:"traceID"`
	ServiceName  string `json:"serviceName"`
	OperationName string `json:"operationName"`
	Duration     int64  `json:"duration"`     // 纳秒
	StartTime    int64  `json:"startTime"`    // 纳秒
	SpanCount    int    `json:"spanCount"`
}

// TraceDetail trace 详情。
type TraceDetail struct {
	Data []TraceDetailItem `json:"data"`
}

// TraceDetailItem trace 详情条目（与 Jaeger 返回格式兼容）。
type TraceDetailItem struct {
	TraceID string        `json:"traceID"`
	Spans   []TraceSpan   `json:"spans"`
}

// TraceSpan 追踪跨度。
type TraceSpan struct {
	SpanID       string            `json:"spanID"`
	TraceID      string            `json:"traceID"`
	ParentSpanID string            `json:"parentSpanID"`
	OperationName string           `json:"operationName"`
	Kind         string            `json:"kind"`
	StartTime    int64             `json:"startTime"`   // 纳秒
	Duration     int64             `json:"duration"`     // 纳秒
	Tags         map[string]string `json:"tags"`
}

// ============================================================================
// 可观测性链路追踪查询端点
// ============================================================================

// handleTraceServices 处理 GET /api/services。
func (s *Server) handleTraceServices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	// 未接入真实 Jaeger，返回空服务列表。
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": []TraceService{},
		"total": 0,
	})
}

// handleTraceServiceOperations 处理 GET /api/services/{service}/operations。
func (s *Server) handleTraceServiceOperations(w http.ResponseWriter, r *http.Request, service string) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	// 未接入真实 Jaeger，返回空操作列表。
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": []TraceOperation{},
		"total": 0,
	})
}

// handleListTraces 处理 GET /api/traces。
func (s *Server) handleListTraces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	// 未接入真实 Jaeger，返回空 traces 列表。
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": []TraceSummary{},
		"total": 0,
	})
}

// handleGetTrace 处理 GET /api/traces/{traceID}。
func (s *Server) handleGetTrace(w http.ResponseWriter, r *http.Request, traceID string) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	// 未接入真实 Jaeger，返回空 trace 详情。
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": []TraceDetailItem{},
	})
}

// ============================================================================
// 路由分派
// ============================================================================

// handleTraceRouting 分发 trace 子路径：/api/services/{service}/operations 和 /api/traces/{traceID}。
func (s *Server) handleTraceRouting(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/")

	switch {
	case strings.HasPrefix(path, "services/") && strings.HasSuffix(path, "/operations"):
		service := strings.TrimSuffix(strings.TrimPrefix(path, "services/"), "/operations")
		s.handleTraceServiceOperations(w, r, service)
	case strings.HasPrefix(path, "traces/"):
		traceID := strings.TrimPrefix(path, "traces/")
		if traceID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "traceID is required"})
			return
		}
		s.handleGetTrace(w, r, traceID)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

// writeJSON 写入 JSON 响应。
func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// 日志记录失败（响应已部分发送，无法回滚）。
		_ = err
	}
}

// 编译期断言：确保手动编码的路径与前端 trace.js 的调用路径一致。