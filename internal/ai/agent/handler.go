// Package agent HTTP handlers
package agent

import (
	"encoding/json"
	"net/http"

	gateway "github.com/Levango7/OpsMesh/internal/ai/gateway"
	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// AgentHandler Agent HTTP handler
type AgentHandler struct {
	orchestrator *Orchestrator
	gateway      *gateway.Gateway
	memory       *MemoryManager
}

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

	agentCtx := AgentContext{
		UserID:    req.UserID,
		TenantID:  req.TenantID,
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

// HandleMemory 处理 GET /api/v1/ai/agent/memory?sessionId=xxx
func (h *AgentHandler) HandleMemory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "sessionId is required"})
		return
	}

	entries := h.memory.GetShortTerm(sessionID)
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

	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "sessionId is required"})
		return
	}

	h.memory.ClearSession(sessionID)
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

// HandleTools 处理 GET /api/v1/ai/agent/tools
func (h *AgentHandler) HandleTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
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

// RegisterAgentRoutes 注册 Agent 路由
func RegisterAgentRoutes(mux *http.ServeMux, handler *AgentHandler) {
	mux.HandleFunc("POST /api/v1/ai/agent/execute", handler.HandleExecute)
	mux.HandleFunc("POST /api/v1/ai/agent/classify", handler.HandleClassify)
	mux.HandleFunc("POST /api/v1/ai/agent/decompose", handler.HandleDecompose)
	mux.HandleFunc("GET /api/v1/ai/agent/memory", handler.HandleMemory)
	mux.HandleFunc("DELETE /api/v1/ai/agent/memory", handler.HandleClearMemory)
	mux.HandleFunc("GET /api/v1/ai/agent/tools", handler.HandleTools)
}
