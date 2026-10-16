package aiops

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Levango7/OpsMesh/internal/authctx"
)

// Handler 是 AIOps HTTP 处理器。
// 路由：
//
//	POST /api/v1/aiops/analyze      告警根因分析
//	POST /api/v1/aiops/remediate    生成修复方案
//	POST /api/v1/aiops/remediate-nl 自然语言转结构化任务
//	POST /api/v1/aiops/execute      自动执行修复（下发任务到 agent）
//	GET  /api/v1/aiops/status        引擎状态（启用/模型/统计）
type Handler struct {
	eng   *Engine
	authz func(w http.ResponseWriter, r *http.Request, perm string) (authctx.Context, bool)
}

// NewHandler 构造 AIOps HTTP 处理器。
func NewHandler(eng *Engine) *Handler {
	return &Handler{eng: eng}
}

// SetAuthorize 设置鉴权回调。
func (h *Handler) SetAuthorize(authz func(w http.ResponseWriter, r *http.Request, perm string) (authctx.Context, bool)) {
	h.authz = authz
}

// RegisterRoutes 注入 AIOps 路由到 mux。
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/aiops/analyze", h.authorize(h.handleAnalyze, "aiops:read", "aiops:write"))
	mux.HandleFunc("/api/v1/aiops/remediate", h.authorize(h.handleRemediate, "aiops:read", "aiops:write"))
	mux.HandleFunc("/api/v1/aiops/remediate-nl", h.authorize(h.handleRemediateNL, "aiops:read", "aiops:write"))
	mux.HandleFunc("/api/v1/aiops/execute", h.authorize(h.handleExecute, "aiops:write", "aiops:write"))
	mux.HandleFunc("/api/v1/aiops/status", h.authorize(h.handleStatus, "aiops:read", "aiops:write"))
}

func (h *Handler) authorize(fn http.HandlerFunc, readPerm, writePerm string) http.HandlerFunc {
	if h.authz == nil {
		return fn
	}
	return func(w http.ResponseWriter, r *http.Request) {
		perm := readPerm
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			perm = writePerm
		}
		actx, ok := h.authz(w, r, perm)
		if !ok {
			return
		}
		if actx.TenantID != "" && r.Header.Get("X-Tenant-ID") == "" {
			r.Header.Set("X-Tenant-ID", actx.TenantID)
		}
		fn(w, r)
	}
}

func (h *Handler) respondJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (h *Handler) respondError(w http.ResponseWriter, code int, msg string) {
	h.respondJSON(w, code, map[string]string{"error": msg})
}

// handleAnalyze 执行故障根因分析。
// Body: {"alert":{"alertID":"...","deviceID":"...","severity":"critical","metric":"cpu_usage","message":"..."},"metrics":{"cpu":{"usage":95},"memory":{"usage":80}},"logs":[{"level":"ERROR","source":"nginx","message":"connection refused"}]}
func (h *Handler) handleAnalyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.respondError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	var req AlertContext
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	rc, err := h.eng.Analyze(r.Context(), &req)
	if err != nil {
		if err == ErrDisabled {
			h.respondError(w, http.StatusServiceUnavailable, "aiops engine not enabled")
		} else {
			h.respondError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	h.respondJSON(w, http.StatusOK, rc)
}

// handleRemediate 生成修复方案。
// Body: {"rootCause":{"category":"软件","component":"nginx","description":"nginx 进程异常退出","confidence":0.9},"deviceContext":{...},"constraints":{"maxSteps":5,"riskTolerance":"low","requireApproval":false,"timeoutSec":30,"autoExecute":false}}
func (h *Handler) handleRemediate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.respondError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	var req RemediationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	plan, err := h.eng.GenerateRemediation(r.Context(), &req)
	if err != nil {
		if err == ErrDisabled {
			h.respondError(w, http.StatusServiceUnavailable, "aiops engine not enabled")
		} else {
			h.respondError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	h.respondJSON(w, http.StatusOK, plan)
}

// handleRemediateNL 自然语言转结构化任务。
// Body: {"input":"重启设备 abc 的 nginx 服务","devices":["abc"]}
func (h *Handler) handleRemediateNL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.respondError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	var body struct {
		Input   string   `json:"input"`
		Devices []string `json:"devices"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	plan, err := h.eng.NaturalLanguage(r.Context(), body.Input, body.Devices)
	if err != nil {
		if err == ErrDisabled {
			h.respondError(w, http.StatusServiceUnavailable, "aiops engine not enabled")
		} else {
			h.respondError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	h.respondJSON(w, http.StatusOK, plan)
}

// handleExecute 自动执行修复方案：将 RemediationPlan 下发为 proto.Task。
// Body: {"plan":{...},"autoExecute":true}
func (h *Handler) handleExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.respondError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	var req ExecuteRemediationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	results, err := h.eng.ExecuteRemediation(r.Context(), &req)
	if err != nil {
		if err == ErrDisabled {
			h.respondError(w, http.StatusServiceUnavailable, "aiops engine not enabled")
		} else {
			h.respondError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	// 累计成功下发数
	dispatched := int64(0)
	for _, res := range results {
		if res.Status == "dispatched" {
			dispatched++
		}
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"results":    results,
		"dispatched": dispatched,
		"total":      len(results),
	})
}

// handleStatus 返回 AIOps 引擎状态。
func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.respondError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}

	stats := h.eng.Stats()
	resp := map[string]interface{}{
		"enabled":   stats.Enabled,
		"provider":  stats.Provider,
		"model":     stats.Model,
		"startedAt": stats.StartedAt.Format(time.RFC3339),
		"stats": map[string]int64{
			"analyzesTotal":        stats.AnalyzesTotal,
			"remediations":         stats.Remediations,
			"nlParses":             stats.NLParses,
			"executedRemediations": stats.ExecutedRemediations,
		},
		"lastErr": stats.LastErr,
	}
	h.respondJSON(w, http.StatusOK, resp)
}
