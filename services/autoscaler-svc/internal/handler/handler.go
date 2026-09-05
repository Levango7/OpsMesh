package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/models"
	"github.com/Levango7/OpsMesh/services/autoscaler-svc/internal/service"
)

// Handler handles HTTP requests for the autoscaler service.
type Handler struct {
	svc *service.Service
}

// NewHandler creates a new Handler.
func NewHandler(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes registers all HTTP routes.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/rules", h.handleRules)
	mux.HandleFunc("/api/v1/rules/", h.handleRuleByID)
	mux.HandleFunc("/api/v1/evaluate", h.handleEvaluate)
	mux.HandleFunc("/api/v1/decisions", h.handleDecisions)
	// 契约约束：聚合层 /api/v1/autoscaler/* 剥域前缀后到 /api/v1/*，
	// 前端 manualScale / cooldowns 两个入口必须在此注册。
	mux.HandleFunc("/api/v1/scale", h.handleScale)
	mux.HandleFunc("/api/v1/cooldowns", h.handleCooldowns)
	mux.HandleFunc("/api/v1/health", h.handleHealth)
}

func (h *Handler) handleRules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.createRule(w, r)
	case http.MethodGet:
		h.listRules(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleRuleByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/rules/")
	if id == "" {
		writeError(w, http.StatusBadRequest, "rule ID required")
		return
	}

	switch r.Method {
	case http.MethodPut:
		h.updateRule(w, r, id)
	case http.MethodDelete:
		h.deleteRule(w, r, id)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) createRule(w http.ResponseWriter, r *http.Request) {
	rule, err := decodeScaleRule(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	created, err := h.svc.CreateRule(r.Context(), rule)
	if err != nil {
		// 服务层用 %w 包装 ErrRuleInvalid，必须 errors.Is 而非 ==。
		if errors.Is(err, service.ErrRuleInvalid) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, created)
}

// decodeScaleRule 兼容两种创建请求体：
//  1. 前端简化结构 {name,metric,threshold,minReplicas,maxReplicas,cooldown}——
//     threshold 同时作 scaleUp/scaleDown 阈值，cooldown（秒）同时作
//     cooldownUp/cooldownDown，deployment/namespace 缺省 "default"，
//     enabled 缺省为 true（否则规则创建后不出现在评估里）；
//  2. 后端原始 ScaleRule 结构（deployment/namespace/scaleUpThreshold 等）。
//
// 约束：简化结构不区分升降阈值，因此 scaleDownThreshold 取 threshold-1
// 满足 AddRule 的 scaleUpThreshold > scaleDownThreshold 校验。
func decodeScaleRule(body io.Reader) (*models.ScaleRule, error) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(body).Decode(&raw); err != nil {
		return nil, err
	}

	if len(raw) == 0 {
		return nil, errors.New("empty request body")
	}

	rule := &models.ScaleRule{}
	// 两层 decode：先按完整 ScaleRule 尝试（旧结构照常支持）。
	full, _ := json.Marshal(raw)
	if err := json.Unmarshal(full, rule); err != nil {
		return nil, err
	}

	// 简化结构字段覆盖：threshold / cooldown 优先映射到双阈值/双冷却。
	if v, ok := raw["threshold"]; ok {
		var threshold float64
		if err := json.Unmarshal(v, &threshold); err == nil {
			rule.ScaleUpThreshold = threshold
			if rule.ScaleDownThreshold == 0 {
				rule.ScaleDownThreshold = threshold - 1
			}
		}
	}
	if v, ok := raw["cooldown"]; ok {
		var cooldown float64
		if err := json.Unmarshal(v, &cooldown); err == nil {
			d := time.Duration(cooldown * float64(time.Second))
			rule.CooldownUp = d
			rule.CooldownDown = d
		}
	}
	if rule.Deployment == "" {
		rule.Deployment = "default"
	}
	if rule.Namespace == "" {
		rule.Namespace = "default"
	}
	// 简化结构无 enabled 字段时默认启用。
	if _, ok := raw["enabled"]; !ok {
		rule.Enabled = true
	}

	return rule, nil
}

func (h *Handler) listRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.svc.ListRules(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, rules)
}

func (h *Handler) updateRule(w http.ResponseWriter, r *http.Request, id string) {
	var rule models.ScaleRule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	rule.ID = id

	updated, err := h.svc.UpdateRule(r.Context(), &rule)
	if err != nil {
		if err == service.ErrRuleNotFound {
			writeError(w, http.StatusNotFound, "rule not found")
			return
		}
		if err == service.ErrRuleInvalid {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) deleteRule(w http.ResponseWriter, r *http.Request, id string) {
	if err := h.svc.DeleteRule(r.Context(), id); err != nil {
		if err == service.ErrRuleNotFound {
			writeError(w, http.StatusNotFound, "rule not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleEvaluate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req models.EvaluateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	resp, err := h.svc.Evaluate(r.Context(), req.RuleID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleDecisions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	decisions := h.svc.GetDecisions(r.Context())
	writeJSON(w, http.StatusOK, decisions)
}

// handleScale 手动扩缩容（前端契约：POST /api/v1/autoscaler/scale，经聚合层
// 剥域前缀后到 /api/v1/scale，请求体 {target,replicas,reason}，响应 {status,message}）。
func (h *Handler) handleScale(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req service.ScaleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	resp, err := h.svc.Scale(r.Context(), &req)
	if err != nil {
		// 服务层用 %w 包装 ErrRuleInvalid，必须 errors.Is 而非 ==。
		if errors.Is(err, service.ErrRuleInvalid) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleCooldowns 冷却状态查询（前端契约：GET /api/v1/autoscaler/cooldowns →
// /api/v1/cooldowns，响应 {cooldowns:[{ruleId,ruleName,remaining,expiresAt}]}）。
func (h *Handler) handleCooldowns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"cooldowns": h.svc.GetCooldowns(r.Context()),
	})
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
