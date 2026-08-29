package logstore

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"opsmesh/internal/authctx"
)

// Handler 是 M6 日志检索的 HTTP 处理器。
type Handler struct {
	ls LogStore
	// Authorize 鉴权回调（由 controlplane 注入）：校验租户上下文 + 所需权限。
	// perm 为当前请求所需权限（如 "log:read"）；未认证/无权限时须已写入响应并返回 ok=false。
	// nil 时不启用鉴权（向后兼容独立使用/测试场景）。
	Authorize func(w http.ResponseWriter, r *http.Request, perm string) (authctx.Context, bool)
}

// NewHandler 构造日志检索处理器。
func NewHandler(ls LogStore) *Handler {
	return &Handler{ls: ls}
}

// authorize 把 Authorize 鉴权回调包装到单个路由 handler 上（G1 鉴权修复）。
// 按请求方法映射所需权限：GET/HEAD/OPTIONS 只读 → readPerm，其余（POST）→ writePerm。
// 鉴权通过后若回调解析出的租户在请求头缺失，回填 X-Tenant-ID，使 handler 行级隔离与鉴权一致。
// 注：agent 日志上报走 gRPC ReportLogs → h.Store() 内部通道，不经此 HTTP 鉴权（G1 保留）。
func (h *Handler) authorize(fn http.HandlerFunc, readPerm, writePerm string) http.HandlerFunc {
	if h.Authorize == nil {
		return fn
	}
	return func(w http.ResponseWriter, r *http.Request) {
		perm := readPerm
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			perm = writePerm
		}
		actx, ok := h.Authorize(w, r, perm)
		if !ok {
			return
		}
		if actx.TenantID != "" && strings.TrimSpace(r.Header.Get("X-Tenant-ID")) == "" {
			r.Header.Set("X-Tenant-ID", actx.TenantID)
		}
		fn(w, r)
	}
}

// RegisterRoutes 注入日志检索路由到 mux。
// G1 鉴权修复：GET/POST 均要求 log:read（RBAC 权限目录仅有 log:read，见 rbacPermSpecs）。
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/logs", h.authorize(h.handleLogs, "log:read", "log:read"))
}

// Store 暴露底层后端（供 gRPC 侧直接落地任务日志，绕过 HTTP）。
func (h *Handler) Store() LogStore { return h.ls }

// handleLogs 处理 GET 查询 / POST 追加。
//
// GET 支持查询参数：
//   - deviceID / agentID / level / source / keyword（向后兼容，message LIKE）
//   - q：结构化查询语法（KQL/Lucene 风格，非空时优先于 keyword）
//   - from / to：RFC3339 时间窗
//   - limit / offset：分页
//
// q 参数解析失败时返回 400 Bad Request（语法错误不应视为服务端故障）。
func (h *Handler) handleLogs(w http.ResponseWriter, r *http.Request) {
	actx := authctx.FromHTTPHeader(r.Header)
	switch r.Method {
	case http.MethodGet:
		q := Query{TenantID: actx.TenantID}
		q.DeviceID = r.URL.Query().Get("deviceID")
		q.AgentID = r.URL.Query().Get("agentID")
		q.Level = r.URL.Query().Get("level")
		q.Source = r.URL.Query().Get("source")
		q.Keyword = r.URL.Query().Get("keyword")
		q.Q = r.URL.Query().Get("q")
		if f := r.URL.Query().Get("from"); f != "" {
			if t, err := time.Parse(time.RFC3339, f); err == nil {
				q.From = t
			}
		}
		if t := r.URL.Query().Get("to"); t != "" {
			if tm, err := time.Parse(time.RFC3339, t); err == nil {
				q.To = tm
			}
		}
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, scanErr := fmt.Sscanf(l, "%d", &q.Limit); scanErr != nil || n != 1 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid limit"})
				return
			}
		}
		if o := r.URL.Query().Get("offset"); o != "" {
			if n, scanErr := fmt.Sscanf(o, "%d", &q.Offset); scanErr != nil || n != 1 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid offset"})
				return
			}
		}
		// 结构化查询语法预校验：q 非空时解析失败返回 400（语法错误，非服务端故障）。
		if q.Q != "" {
			if _, err := ParseQuery(q.Q); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": fmt.Sprintf("invalid query syntax: %v", err),
				})
				return
			}
		}
		entries, err := h.ls.Query(r.Context(), q)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if entries == nil {
			entries = []Entry{}
		}
		writeJSON(w, http.StatusOK, entries)

	case http.MethodPost:
		var e Entry
		if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid JSON: %v", err)})
			return
		}
		// 强制租户隔离：忽略客户端自报的 tenant_id，以网关注入为准（等保三级）。
		e.TenantID = actx.TenantID
		if e.Timestamp.IsZero() {
			e.Timestamp = time.Now()
		}
		if e.Level == "" {
			e.Level = "info"
		}
		if e.Source == "" {
			e.Source = "system"
		}
		if err := h.ls.Append(r.Context(), &e); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, e)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// RecordTaskResult 把一次任务执行结果（stdout/stderr）作为两条日志落地（供 M6 检索）。
// 在 gRPC ReportResult 中调用：source=task，taskID/agentID 取自结果，
// 租户由调用方（agent 所属租户，取自 g.reg.Agent(agentID).TenantID）传入。
// stdout 记为 info，stderr 记为 error（exitCode!=0 时；exitCode==0 且 stderr 非空记为 warn，便于区分告警与错误）。
func (h *Handler) RecordTaskResult(ctx context.Context, tenantID, agentID, taskID string, exitCode int, stdout, stderr string) {
	if h == nil {
		return
	}
	ts := time.Now()
	if stdout != "" {
		// 落地失败不影响任务结果上报主流程。
		_ = h.ls.Append(ctx, &Entry{
			TenantID: tenantID, AgentID: agentID, TaskID: taskID,
			Timestamp: ts, Level: "info", Source: "task", Message: stdout,
		})
	}
	if stderr != "" {
		lvl := "error"
		if exitCode == 0 {
			lvl = "warn"
		}
		_ = h.ls.Append(ctx, &Entry{
			TenantID: tenantID, AgentID: agentID, TaskID: taskID,
			Timestamp: ts, Level: lvl, Source: "task", Message: stderr,
		})
	}
}

// writeJSON 写入 JSON 响应。
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
