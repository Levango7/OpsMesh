// Package aigateway HTTP handlers
package aigateway

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/authctx"
	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/store"
)

// AIGatewayHandler AI 网关 HTTP handler
type AIGatewayHandler struct {
	gateway     *Gateway
	retriever   *RAGRetriever
	compressor  *ContextCompressor
	promptStore *PromptTemplateStore
	// Authorize 鉴权回调（由 controlplane 注入 subsystemAuthorize）：校验身份并核对
	// 权限点。nil 时不启用鉴权（独立使用与单测的向后兼容路径）。
	//
	// 为什么这 9 条路由必须由 handler 自查而不是包一层：与 ai/agent 同一口径，
	// 权限点取决于 HTTP 方法（/ai/prompts 同时挂 GET 与 POST，read/write 分属
	// 两个权限点），包装器在注册处看不到方法分支。
	Authorize func(w http.ResponseWriter, r *http.Request, perm string) (authctx.Context, bool)
}

// authorize 核对权限点；ok=false 时响应已由回调写完，调用方必须立即返回。
func (h *AIGatewayHandler) authorize(w http.ResponseWriter, r *http.Request, perm string) (authctx.Context, bool) {
	if h.Authorize == nil {
		return authctx.Context{}, true
	}
	return h.Authorize(w, r, perm)
}

func NewAIGatewayHandler(gw *Gateway, vs VectorStore, topK int) *AIGatewayHandler {
	return &AIGatewayHandler{
		gateway:     gw,
		retriever:   NewRAGRetriever(vs, gw, topK),
		compressor:  NewContextCompressor(4096),
		promptStore: NewPromptTemplateStore(),
	}
}

// HandleChat 处理 POST /api/v1/ai/chat
func (h *AIGatewayHandler) HandleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	// 对话不写业务状态 ⇒ ai:read；但它会真打一次 LLM 并计入用量，匿名开放等于
	// 给公网一个免费的模型代理（烧的是本租户的额度）。
	actx, ok := h.authorize(w, r, "ai:read")
	if !ok {
		return
	}

	var req struct {
		Model     string                 `json:"model"`
		Messages  []Message              `json:"messages"`
		Tools     []Tool                 `json:"tools,omitempty"`
		MaxTokens int                    `json:"maxTokens,omitempty"`
		UseRAG    bool                   `json:"useRag,omitempty"`
		Filter    map[string]interface{} `json:"filter,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if len(req.Messages) == 0 {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "messages is required"})
		return
	}

	model := req.Model
	if model == "" {
		model = "gpt-4o-mini"
	}

	// RAG 检索（TD-97 ②：租户化 ctx——只检索本租户文档）
	if req.UseRAG {
		lastMsg := req.Messages[len(req.Messages)-1].Content
		docs, err := h.retriever.Retrieve(authctx.NewContext(r.Context(), actx), lastMsg, req.Filter)
		if err == nil && len(docs) > 0 {
			ctx := h.retriever.BuildContext(docs)
			// 注入到系统消息
			systemMsg := Message{Role: RoleSystem, Content: ctx}
			req.Messages = append([]Message{systemMsg}, req.Messages...)
		}
	}

	// 上下文压缩
	compressed := h.compressor.Compress(req.Messages)

	// LLM 调用
	resp, err := h.gateway.Complete(r.Context(), CompletionRequest{
		Model:     model,
		Messages:  compressed,
		Tools:     req.Tools,
		MaxTokens: req.MaxTokens,
	})
	if err != nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	paginate.WriteJSON(w, http.StatusOK, resp)
}

// HandleEmbed 处理 POST /api/v1/ai/embed
func (h *AIGatewayHandler) HandleEmbed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	// 只回向量、不落库 ⇒ ai:read（同 chat：消耗额度故不能匿名）。
	if _, ok := h.authorize(w, r, "ai:read"); !ok {
		return
	}

	var req struct {
		Provider string `json:"provider"`
		Text     string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if req.Text == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "text is required"})
		return
	}

	if req.Provider == "" {
		req.Provider = "openai"
	}

	embedding, err := h.gateway.Embed(r.Context(), req.Provider, req.Text)
	if err != nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"embedding": embedding,
		"dimension": len(embedding),
	})
}

// HandleRAGIngest 处理 POST /api/v1/ai/rag/ingest
func (h *AIGatewayHandler) HandleRAGIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	// 落库动作：写入的文档会立刻出现在所有人的检索结果里 ⇒ ai:write。
	actx, ok := h.authorize(w, r, "ai:write")
	if !ok {
		return
	}

	var req struct {
		Documents []VectorDocument `json:"documents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if len(req.Documents) == 0 {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "documents is required"})
		return
	}

	// 生成 embedding
	for i := range req.Documents {
		if len(req.Documents[i].Embedding) == 0 {
			emb, err := h.gateway.Embed(r.Context(), "openai", req.Documents[i].Content)
			if err != nil {
				paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("embed doc %d: %s", i, err.Error())})
				return
			}
			req.Documents[i].Embedding = emb
		}
	}

	if err := h.retriever.vectorStore.Upsert(authctx.NewContext(r.Context(), actx), req.Documents); err != nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"ingested": len(req.Documents),
	})
}

// HandleRAGSearch 处理 POST /api/v1/ai/rag/search
func (h *AIGatewayHandler) HandleRAGSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	// 检索读的是向量库内容（可能含其他动作写入的资料）⇒ 必须登录，按 ai:read。
	actx, ok := h.authorize(w, r, "ai:read")
	if !ok {
		return
	}

	var req struct {
		Query  string                 `json:"query"`
		TopK   int                    `json:"topK,omitempty"`
		Filter map[string]interface{} `json:"filter,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if req.Query == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "query is required"})
		return
	}

	docs, err := h.retriever.Retrieve(authctx.NewContext(r.Context(), actx), req.Query, req.Filter)
	if err != nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"documents": docs,
		"count":     len(docs),
	})
}

// HandleTokenUsage 处理 GET /api/v1/ai/usage
//
// 口径（TD-97 ③④，2026-11-05）：
//   - 默认返回**本租户**用量（ctx 租户；无身份时归 default）；
//   - `?scope=all` 返回平台全量，但仅当调用方同时满足：
//     ① 租户是平台租户（store.DefaultTenantID，auth_users.go:52/77 同先例）；
//     ② 持 platform:read 权限点（本函数内二次 authorize，不给默认角色）；
//   - 否则 scope=all 静默降回本租户口径（不报错——不向非平台调用方确认
//     「存在全量口径」这一事实本身）。此前该端点是网关级累计，任何持
//     ai:read 的人都能看到平台总量。
func (h *AIGatewayHandler) HandleTokenUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := h.authorize(w, r, "ai:read")
	if !ok {
		return
	}

	tenant := actx.TenantID
	if tenant == "" {
		tenant = store.DefaultTenantID
	}

	if r.URL.Query().Get("scope") == "all" {
		// 平台全量 = default 租户 + platform:readall 双重门（TD-97 ③④）。
		// 顺序有意如此：先判租户（纯读 actx，不写响应），只有平台租户才发起第二次
		// 鉴权。业务租户永远不会被问到这个权限点——既不泄露「存在全量口径」这一
		// 事实，也避免 authorize 失败时回调写入的 403 把「静默降级」变成空话
		// （实测过：任何 authorize 拒绝都会先写 403 到同一个 ResponseWriter）。
		// 平台租户被问而没权限 ⇒ 正常 403（平台运维本人就该知道缺权限）。
		if tenant == store.DefaultTenantID {
			if _, ok := h.authorize(w, r, "platform:readall"); ok {
				paginate.WriteJSON(w, http.StatusOK, h.gateway.GetTokenUsage(""))
				return
			}
			// authorize 失败时响应已由回调写入（403），直接返回。
			return
		}
		// 业务租户：静默降回本租户口径，不告知原因。
	}

	paginate.WriteJSON(w, http.StatusOK, h.gateway.GetTokenUsage(tenant))
}

// HandleHealth 处理 GET /api/v1/ai/health
func (h *AIGatewayHandler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	// 这里的 health 是各 LLM 提供商的可达性（含提供商名单），属内部拓扑 ⇒ ai:read；
	// 它不是进程存活探针，健康检查端点另有 /api/v1/health。
	if _, ok := h.authorize(w, r, "ai:read"); !ok {
		return
	}

	health := h.gateway.HealthCheck(r.Context())
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"providers": health,
	})
}

// HandlePromptTemplates 处理 GET/POST /api/v1/ai/prompts
//
// 权限点按动词分支就近声明：刻意不写成「先算 perm 再统一 authorize」，那样字面量就
// 不在 authorize(...) 调用点行上了，TestRolePermissions_CoversAuthorizeCallSites
// 这类源码守护会漏看（aiops 族当初就是这么漏的）。
func (h *AIGatewayHandler) HandlePromptTemplates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		actx, ok := h.authorize(w, r, "ai:read")
		if !ok {
			return
		}
		category := r.URL.Query().Get("category")
		templates := h.promptStore.List(authctx.NewContext(r.Context(), actx), category)
		paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"templates": templates,
		})
	case http.MethodPost:
		// 新建模板写的是 Agent 侧的提示词事实源 ⇒ ai:write，且必须在 Create 之前。
		actx, ok := h.authorize(w, r, "ai:write")
		if !ok {
			return
		}
		var tmpl PromptTemplate
		if err := json.NewDecoder(r.Body).Decode(&tmpl); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
			return
		}
		created, err := h.promptStore.Create(authctx.NewContext(r.Context(), actx), &tmpl)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		paginate.WriteJSON(w, http.StatusCreated, created)
	default:
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// HandlePromptTemplate 处理 GET/PUT/DELETE /api/v1/ai/prompts/{id}
func (h *AIGatewayHandler) HandlePromptTemplate(w http.ResponseWriter, r *http.Request, id string) {
	switch r.Method {
	case http.MethodGet:
		actx, ok := h.authorize(w, r, "ai:read")
		if !ok {
			return
		}
		tmpl, ok := h.promptStore.Get(authctx.NewContext(r.Context(), actx), id)
		if !ok {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
			return
		}
		paginate.WriteJSON(w, http.StatusOK, tmpl)
	case http.MethodPut:
		actx, ok := h.authorize(w, r, "ai:write")
		if !ok {
			return
		}
		var req struct {
			Content   string   `json:"content"`
			Variables []string `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
			return
		}
		updated, err := h.promptStore.Update(authctx.NewContext(r.Context(), actx), id, req.Content, req.Variables)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		paginate.WriteJSON(w, http.StatusOK, updated)
	case http.MethodDelete:
		actx, ok := h.authorize(w, r, "ai:write")
		if !ok {
			return
		}
		if !h.promptStore.Delete(authctx.NewContext(r.Context(), actx), id) {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// RegisterAIGatewayRoutes 注册 AI 网关路由。
//
// 权限映射（各 handler 自查，见 AIGatewayHandler.Authorize）：
//   - ai:read  → POST /ai/chat、POST /ai/embed、POST /ai/rag/search、GET /ai/usage、
//     GET /ai/health、GET /ai/prompts、GET /ai/prompts/{id}
//     （chat/embed/search 不改状态但都会打模型或读共享向量库，不能匿名）；
//   - ai:write → POST /ai/rag/ingest、POST /ai/prompts、PUT /ai/prompts/{id}、
//     DELETE /ai/prompts/{id}（写共享向量库与提示词事实源）。
//
// 修复前这 11 条注册（8 个 handler）全部匿名可达，与 ai/agent、k8slifecycle 同族
// （G1 全域匿名漏洞）。
func RegisterAIGatewayRoutes(mux *http.ServeMux, handler *AIGatewayHandler) {
	mux.HandleFunc("POST /api/v1/ai/chat", handler.HandleChat)
	mux.HandleFunc("POST /api/v1/ai/embed", handler.HandleEmbed)
	mux.HandleFunc("POST /api/v1/ai/rag/ingest", handler.HandleRAGIngest)
	mux.HandleFunc("POST /api/v1/ai/rag/search", handler.HandleRAGSearch)
	mux.HandleFunc("GET /api/v1/ai/usage", handler.HandleTokenUsage)
	mux.HandleFunc("GET /api/v1/ai/health", handler.HandleHealth)
	mux.HandleFunc("GET /api/v1/ai/prompts", handler.HandlePromptTemplates)
	mux.HandleFunc("POST /api/v1/ai/prompts", handler.HandlePromptTemplates)
	mux.HandleFunc("GET /api/v1/ai/prompts/", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len("/api/v1/ai/prompts/"):]
		handler.HandlePromptTemplate(w, r, id)
	})
	mux.HandleFunc("PUT /api/v1/ai/prompts/", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len("/api/v1/ai/prompts/"):]
		handler.HandlePromptTemplate(w, r, id)
	})
	mux.HandleFunc("DELETE /api/v1/ai/prompts/", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len("/api/v1/ai/prompts/"):]
		handler.HandlePromptTemplate(w, r, id)
	})
}
