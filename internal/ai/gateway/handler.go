// Package aigateway HTTP handlers
package aigateway

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// AIGatewayHandler AI 网关 HTTP handler
type AIGatewayHandler struct {
	gateway     *Gateway
	retriever   *RAGRetriever
	compressor  *ContextCompressor
	promptStore *PromptTemplateStore
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

	// RAG 检索
	if req.UseRAG {
		lastMsg := req.Messages[len(req.Messages)-1].Content
		docs, err := h.retriever.Retrieve(r.Context(), lastMsg, req.Filter)
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

	if err := h.retriever.vectorStore.Upsert(r.Context(), req.Documents); err != nil {
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

	docs, err := h.retriever.Retrieve(r.Context(), req.Query, req.Filter)
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
func (h *AIGatewayHandler) HandleTokenUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	report := h.gateway.GetTokenUsage()
	paginate.WriteJSON(w, http.StatusOK, report)
}

// HandleHealth 处理 GET /api/v1/ai/health
func (h *AIGatewayHandler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	health := h.gateway.HealthCheck(r.Context())
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"providers": health,
	})
}

// HandlePromptTemplates 处理 GET/POST /api/v1/ai/prompts
func (h *AIGatewayHandler) HandlePromptTemplates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		category := r.URL.Query().Get("category")
		templates := h.promptStore.List(category)
		paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"templates": templates,
		})
	case http.MethodPost:
		var tmpl PromptTemplate
		if err := json.NewDecoder(r.Body).Decode(&tmpl); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
			return
		}
		created, err := h.promptStore.Create(&tmpl)
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
		tmpl, ok := h.promptStore.Get(id)
		if !ok {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
			return
		}
		paginate.WriteJSON(w, http.StatusOK, tmpl)
	case http.MethodPut:
		var req struct {
			Content   string   `json:"content"`
			Variables []string `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
			return
		}
		updated, err := h.promptStore.Update(id, req.Content, req.Variables)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		paginate.WriteJSON(w, http.StatusOK, updated)
	case http.MethodDelete:
		if !h.promptStore.Delete(id) {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// RegisterAIGatewayRoutes 注册 AI 网关路由
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
