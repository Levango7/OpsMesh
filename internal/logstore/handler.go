package logstore

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"opsmesh/internal/authctx"
)

// Handler 是 M6 日志检索的 HTTP 处理器。
type Handler struct {
	ls LogStore
}

// NewHandler 构造日志检索处理器。
func NewHandler(ls LogStore) *Handler {
	return &Handler{ls: ls}
}

// RegisterRoutes 注入日志检索路由到 mux。
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/logs", h.handleLogs)
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
			_, _ = fmt.Sscanf(l, "%d", &q.Limit)
		}
		if o := r.URL.Query().Get("offset"); o != "" {
			_, _ = fmt.Sscanf(o, "%d", &q.Offset)
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
