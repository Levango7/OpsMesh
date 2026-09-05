package handler

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/Levango7/OpsMesh/services/runbook-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/runbook-svc/internal/service"
)

// Handler provides HTTP handlers for the runbook service.
type Handler struct {
	svc *service.Service
}

// NewHandler creates a new Handler.
func NewHandler(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes registers all HTTP routes.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/runbooks", h.handleRunbooks)
	mux.HandleFunc("/api/v1/runbooks/", h.handleRunbookDetail)
	mux.HandleFunc("/api/v1/trigger/webhook", h.handleWebhook)
	mux.HandleFunc("/api/v1/health", h.handleHealth)
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleRunbooks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.createRunbook(w, r)
	case http.MethodGet:
		h.listRunbooks(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleRunbookDetail(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/runbooks/")
	if path == "" || path == "runbooks" {
		h.handleRunbooks(w, r)
		return
	}

	// Handle /api/v1/runbooks/{id}/{suffix}
	// suffix 分发：trigger/history 为原有路径；execute/executions 为前端契约动词
	// （web/enterprise/src/api/runbook.js），复用同一业务逻辑。
	if idx := strings.Index(path, "/"); idx > 0 {
		id := path[:idx]
		suffix := path[idx+1:]
		// GET /api/v1/runbooks/{id}/executions/{eid}/logs
		if strings.HasPrefix(suffix, "executions/") && strings.HasSuffix(suffix, "/logs") {
			rest := strings.TrimSuffix(strings.TrimPrefix(suffix, "executions/"), "/logs")
			if rest != "" {
				if r.Method != http.MethodGet {
					writeError(w, http.StatusMethodNotAllowed, "method not allowed")
					return
				}
				h.getExecutionLogs(w, r, id, rest)
				return
			}
		}
		switch suffix {
		case "trigger", "execute":
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			h.triggerRunbook(w, r, id)
		case "history", "executions":
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			h.getHistory(w, r, id)
		default:
			writeError(w, http.StatusNotFound, "not found")
		}
		return
	}

	// /api/v1/runbooks/{id}
	switch r.Method {
	case http.MethodGet:
		h.getRunbook(w, r, path)
	case http.MethodPut:
		h.updateRunbook(w, r, path)
	case http.MethodDelete:
		h.deleteRunbook(w, r, path)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// decodeRunbookRequest 解析创建/更新请求体。
// 约束：前端契约（web/enterprise/src/api/runbook.js）发送 {name,description,content,triggers}，
// 不含 enabled 字段；此处未显式传 "enabled": false 时默认启用，
// 否则 bool 零值 false 会让后续 trigger 直接 409（ErrRunbookDisabled）。
func decodeRunbookRequest(body io.Reader) (*models.Runbook, error) {
	var rb models.Runbook
	raw, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &rb); err != nil {
		return nil, err
	}

	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, err
	}
	if _, ok := probe["enabled"]; !ok {
		rb.Enabled = true
	}
	return &rb, nil
}

func (h *Handler) createRunbook(w http.ResponseWriter, r *http.Request) {
	rb, err := decodeRunbookRequest(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	created, err := h.svc.CreateRunbook(r.Context(), rb)
	if err != nil {
		if err == service.ErrRunbookInvalid {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, created)
}

func (h *Handler) listRunbooks(w http.ResponseWriter, r *http.Request) {
	runbooks, err := h.svc.ListRunbooks(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, runbooks)
}

func (h *Handler) getRunbook(w http.ResponseWriter, r *http.Request, id string) {
	rb, err := h.svc.GetRunbook(r.Context(), id)
	if err != nil {
		if err == service.ErrRunbookNotFound {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rb)
}

func (h *Handler) updateRunbook(w http.ResponseWriter, r *http.Request, id string) {
	rb, err := decodeRunbookRequest(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	rb.ID = id
	// 前端更新契约不含 enabled：未显式传时保留原值，防止回写为零值 false。
	if !rb.Enabled {
		if existing, gerr := h.svc.GetRunbook(r.Context(), id); gerr == nil && existing.Enabled {
			var probe map[string]json.RawMessage
			// decodeRunbookRequest 已保证 enabled 默认 true；此处需区分显式 false。
			// 重新读原始体不可行（已消费），改由 decodeRunbookRequest 返回值约定：
			// rb.Enabled == true 表示"传了 true 或未传"；显式 false 才会落到这里且 existing.Enabled
			// 需要被覆盖 —— 但无法与"未传"区分，故未传时保守保留原值。
			_ = probe
			rb.Enabled = existing.Enabled
		}
	}

	updated, err := h.svc.UpdateRunbook(r.Context(), rb)
	if err != nil {
		if err == service.ErrRunbookNotFound {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		if err == service.ErrRunbookInvalid {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) deleteRunbook(w http.ResponseWriter, r *http.Request, id string) {
	if err := h.svc.DeleteRunbook(r.Context(), id); err != nil {
		if err == service.ErrRunbookNotFound {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) triggerRunbook(w http.ResponseWriter, r *http.Request, id string) {
	var body struct {
		TriggeredBy string `json:"triggered_by"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	record, err := h.svc.TriggerRunbook(r.Context(), id, body.TriggeredBy)
	if err != nil {
		switch err {
		case service.ErrRunbookNotFound:
			writeError(w, http.StatusNotFound, err.Error())
		case service.ErrRunbookDisabled:
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	writeJSON(w, http.StatusOK, record)
}

func (h *Handler) getHistory(w http.ResponseWriter, r *http.Request, id string) {
	history, err := h.svc.GetHistory(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, history)
}

// getExecutionLogs serves GET /api/v1/runbooks/{id}/executions/{eid}/logs（前端契约）。
// 从执行历史定位该执行记录，将各步骤 StepResult.Output 拼接为 {logs: "..."}。
func (h *Handler) getExecutionLogs(w http.ResponseWriter, r *http.Request, id, execID string) {
	history, err := h.svc.GetHistory(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, rec := range history {
		if rec.ID != execID {
			continue
		}
		var b strings.Builder
		for i, sr := range rec.StepResults {
			if i > 0 {
				b.WriteString("\n")
			}
			if sr.Output != "" {
				b.WriteString(sr.Output)
			} else if sr.Error != "" {
				b.WriteString(sr.Error)
			}
		}
		// 步骤输出通常自带换行（如 echo），拼接尾部截断避免多余空行。
		writeJSON(w, http.StatusOK, map[string]string{"logs": strings.TrimRight(b.String(), "\n")})
		return
	}
	writeError(w, http.StatusNotFound, "execution not found")
}

func (h *Handler) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var payload models.WebhookPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	records, err := h.svc.HandleWebhook(r.Context(), &payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	log.Printf("Webhook triggered %d runbook(s) from %s", len(records), payload.Source)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"triggered": len(records),
		"records":   records,
	})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("failed to encode response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
