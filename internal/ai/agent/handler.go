// Package agent HTTP handlers
package agent

import (
	"encoding/json"
	"net/http"
	"strings"

	gateway "github.com/Levango7/OpsMesh/internal/ai/gateway"
	"github.com/Levango7/OpsMesh/internal/authctx"
	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// AgentHandler Agent HTTP handler
type AgentHandler struct {
	orchestrator *Orchestrator
	gateway      *gateway.Gateway
	memory       *MemoryManager
	// Authorize 鉴权回调（由 controlplane 注入 subsystemAuthorize）：校验身份并核对
	// 权限点，返回**已验签**的上下文。nil 时不启用鉴权（独立使用与单测的向后兼容路径）。
	//
	// 为什么 handlers 自己调而不是在 RegisterAgentRoutes 里包一层：这些端点要把
	// 已验签的 tenantId/userId 写回业务上下文（见 HandleExecute），包装器拿不到
	// 返回值就得再解析一次或塞进 request context，多一层状态。
	Authorize func(w http.ResponseWriter, r *http.Request, perm string) (authctx.Context, bool)
}

// authorize 核对权限点；ok=false 时响应已由回调写完，调用方必须立即返回。
func (h *AgentHandler) authorize(w http.ResponseWriter, r *http.Request, perm string) (authctx.Context, bool) {
	if h.Authorize == nil {
		return authctx.Context{}, true
	}
	return h.Authorize(w, r, perm)
}

// identity 以已验签上下文为准覆盖请求自报身份。
// 未启用鉴权（回调为 nil 或开放模式返回空上下文）时沿用原值，保持独立部署可用。
func identity(actx authctx.Context, bodyTenant, bodyUser string) (string, string) {
	if actx.TenantID != "" {
		bodyTenant = actx.TenantID
	}
	if actx.UserID != "" {
		bodyUser = actx.UserID
	}
	return bodyTenant, bodyUser
}

// NewAgentHandler 装配 Agent handler；identity 从请求头/已验签上下文取，不信自报字段。
func NewAgentHandler(orch *Orchestrator, gw *gateway.Gateway, mem *MemoryManager) *AgentHandler {
	return &AgentHandler{
		orchestrator: orch,
		gateway:      gw,
		memory:       mem,
	}
}

// HandleExecute 处理 POST /api/v1/ai/agent/execute
func (h *AgentHandler) HandleExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	// Agent 执行会驱动工具（含 execute_command 这类远程 shell），按写权限把关。
	actx, ok := h.authorize(w, r, "ai:write")
	if !ok {
		return
	}

	var req struct {
		Input     string `json:"input"`
		UserID    string `json:"userId"`
		TenantID  string `json:"tenantId"`
		SessionID string `json:"sessionId"`
		UseDAG    bool   `json:"useDag,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if req.Input == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "input is required"})
		return
	}

	// 身份以已验签上下文为准：userId/tenantId 是客户端自报字段，采信它们等于允许
	// 调用方冒充任意租户去驱动 Agent、并读写其会话记忆。
	tenantID, userID := identity(actx, req.TenantID, req.UserID)
	agentCtx := AgentContext{
		UserID:    userID,
		TenantID:  tenantID,
		SessionID: req.SessionID,
		Metadata:  make(map[string]interface{}),
	}

	var result interface{}
	var err error
	if req.UseDAG {
		result, err = h.orchestrator.ExecuteDAG(r.Context(), req.Input, agentCtx)
	} else {
		result, err = h.orchestrator.Execute(r.Context(), req.Input, agentCtx)
	}

	if err != nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	paginate.WriteJSON(w, http.StatusOK, result)
}

// HandleClassify 处理 POST /api/v1/ai/agent/classify
func (h *AgentHandler) HandleClassify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	// 分类只回一个意图标签，不改任何状态 ⇒ 归 ai:read（但要登录）。
	// 它仍会真打一次 LLM（消耗租户额度），所以不能匿名开放。
	if _, ok := h.authorize(w, r, "ai:read"); !ok {
		return
	}

	var req struct {
		Input string `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	classifier := NewIntentClassifier(h.gateway)
	agentType, confidence, err := classifier.Classify(r.Context(), req.Input)
	if err != nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"agentType":  agentType,
		"confidence": confidence,
	})
}

// HandleDecompose 处理 POST /api/v1/ai/agent/decompose
func (h *AgentHandler) HandleDecompose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	// 只产出子任务计划文本、不执行 ⇒ 与 classify 同为 ai:read。
	if _, ok := h.authorize(w, r, "ai:read"); !ok {
		return
	}

	var req struct {
		Input string `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	classifier := NewIntentClassifier(h.gateway)
	tasks, err := classifier.DecomposePrompt(r.Context(), req.Input)
	if err != nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"subTasks": tasks,
	})
}

// memoryTenant 决定读/删会话时用哪个租户：已验签上下文优先；仅在未启用鉴权
// （回调为 nil，或开放模式返回空上下文）时才接受客户端自报的 tenantId——
// 与 HandleExecute 的 identity 口径保持一致，否则写入与读取的作用域会错位。
func memoryTenant(actx authctx.Context, r *http.Request) string {
	if actx.TenantID != "" {
		return actx.TenantID
	}
	return strings.TrimSpace(r.URL.Query().Get("tenantId"))
}

// HandleMemory 处理 GET /api/v1/ai/agent/memory?sessionId=xxx
func (h *AgentHandler) HandleMemory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := h.authorize(w, r, "ai:read")
	if !ok {
		return
	}

	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "sessionId is required"})
		return
	}

	entries := h.memory.GetShortTerm(memoryTenant(actx, r), sessionID)
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"entries": entries,
		"count":   len(entries),
	})
}

// HandleClearMemory 处理 DELETE /api/v1/ai/agent/memory?sessionId=xxx
func (h *AgentHandler) HandleClearMemory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := h.authorize(w, r, "ai:write")
	if !ok {
		return
	}

	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "sessionId is required"})
		return
	}

	h.memory.ClearSession(memoryTenant(actx, r), sessionID)
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

// HandleTools 处理 GET /api/v1/ai/agent/tools
func (h *AgentHandler) HandleTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := h.authorize(w, r, "ai:read"); !ok {
		return
	}

	tools := DefaultToolRegistry().List()
	toolList := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		toolList = append(toolList, map[string]interface{}{
			"name":        t.Name(),
			"description": t.Description(),
			"parameters":  t.Parameters(),
		})
	}

	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"tools": toolList,
		"count": len(toolList),
	})
}

// RegisterAgentRoutes 注册 Agent 路由。
//
// 权限映射（每个 handler 自查，见 AgentHandler.Authorize）：
//   - ai:read  → GET /memory、GET /tools、POST /classify、POST /decompose
//     （后两条只回标签/计划文本，不改任何状态，但会打 LLM 故不能匿名）；
//   - ai:write → POST /execute（会驱动工具，含 execute_command 远程 shell）、
//     DELETE /memory（销毁他人会话上下文是破坏性动作）。
//
// 会话记忆一律按**已验签租户**作用域存取：sessionId 由客户端自报，历史上
// 仅凭它就能读/删任意租户的会话记忆。
func RegisterAgentRoutes(mux *http.ServeMux, handler *AgentHandler) {
	mux.HandleFunc("POST /api/v1/ai/agent/execute", handler.HandleExecute)
	mux.HandleFunc("POST /api/v1/ai/agent/classify", handler.HandleClassify)
	mux.HandleFunc("POST /api/v1/ai/agent/decompose", handler.HandleDecompose)
	mux.HandleFunc("GET /api/v1/ai/agent/memory", handler.HandleMemory)
	mux.HandleFunc("DELETE /api/v1/ai/agent/memory", handler.HandleClearMemory)
	mux.HandleFunc("GET /api/v1/ai/agent/tools", handler.HandleTools)
}
