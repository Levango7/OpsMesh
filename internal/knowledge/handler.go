// Package knowledge HTTP handlers
package knowledge

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	gateway "github.com/Levango7/OpsMesh/internal/ai/gateway"
	"github.com/Levango7/OpsMesh/internal/authctx"
	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// KnowledgeHandler 知识库 HTTP handler
type KnowledgeHandler struct {
	store     KnowledgeStore
	syncEng   *SyncEngine
	retriever *HybridRetriever
	reranker  *Reranker
	citation  *CitationTracker
	gw        *gateway.Gateway
	// Authorize 鉴权回调（由 controlplane 注入 subsystemAuthorize）：校验身份并核对权限点。
	// nil 时不启用鉴权（独立使用与单测的向后兼容路径）。口径与 ai/agent、ai/gateway 一致。
	Authorize func(w http.ResponseWriter, r *http.Request, perm string) (authctx.Context, bool)
}

// authorize 核对权限点；ok=false 时响应已由回调写完，调用方必须立即返回。
func (h *KnowledgeHandler) authorize(w http.ResponseWriter, r *http.Request, perm string) (authctx.Context, bool) {
	if h.Authorize == nil {
		return authctx.Context{}, true
	}
	return h.Authorize(w, r, perm)
}

func NewKnowledgeHandler(store KnowledgeStore, gw *gateway.Gateway) *KnowledgeHandler {
	syncEng := NewSyncEngine(store, gw)
	retriever := NewHybridRetriever(store, gw, 10)
	reranker := NewReranker(gw)
	citation := NewCitationTracker()
	return &KnowledgeHandler{
		store:     store,
		syncEng:   syncEng,
		retriever: retriever,
		reranker:  reranker,
		citation:  citation,
		gw:        gw,
	}
}

// HandleSync 处理 POST /api/v1/knowledge/sync
func (h *KnowledgeHandler) HandleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	// 同步会从外部源拉资料并覆盖文档库 ⇒ knowledge:write。
	_, ok := h.authorize(w, r, "knowledge:write")
	if !ok {
		return
	}

	var req struct {
		Source string `json:"source"` // empty = sync all
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if req.Source == "" {
		results, err := h.syncEng.SyncAll(r.Context())
		if err != nil {
			paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"results": results,
		})
	} else {
		count, err := h.syncEng.SyncNow(r.Context(), KnowledgeSource(req.Source))
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"source": req.Source,
			"count":  count,
		})
	}
}

// HandleSearch 处理 POST /api/v1/knowledge/search
func (h *KnowledgeHandler) HandleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	// 检索读的是文档库正文（rerank 还会打一次模型）⇒ knowledge:read，不能匿名。
	_, ok := h.authorize(w, r, "knowledge:read")
	if !ok {
		return
	}

	var req struct {
		Query  string                 `json:"query"`
		TopK   int                    `json:"topK,omitempty"`
		Filter map[string]interface{} `json:"filter,omitempty"`
		Rerank bool                   `json:"rerank,omitempty"`
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

	// 重排序
	if req.Rerank && len(docs) > 1 {
		reranked, err := h.reranker.Rerank(r.Context(), req.Query, docs)
		if err != nil {
			paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		docs = reranked
	}

	// 引用溯源
	docs = h.citation.Track(docs, req.Query)

	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"documents": docs,
		"count":     len(docs),
		"citations": h.citation.FormatCitations(docs),
	})
}

// HandleIngest 处理 POST /api/v1/knowledge/ingest
func (h *KnowledgeHandler) HandleIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	// 导入直接写文档库 ⇒ knowledge:write。
	actx, ok := h.authorize(w, r, "knowledge:write")
	if !ok {
		return
	}

	var req struct {
		Documents []KnowledgeDoc `json:"documents"`
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
			emb, err := h.gw.Embed(r.Context(), "openai", req.Documents[i].Content)
			if err != nil {
				paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("embed doc %d: %s", i, err.Error())})
				return
			}
			req.Documents[i].Embedding = emb
		}
		if req.Documents[i].CreatedAt.IsZero() {
			req.Documents[i].CreatedAt = time.Now()
		}
		req.Documents[i].UpdatedAt = time.Now()
	}

	if err := h.store.Upsert(authctx.NewContext(r.Context(), actx), req.Documents); err != nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"ingested": len(req.Documents),
	})
}

// HandleList 处理 GET /api/v1/knowledge/docs
func (h *KnowledgeHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	// 列出的是文档正文（一次最多 1000 条）⇒ knowledge:read。
	actx, ok := h.authorize(w, r, "knowledge:read")
	if !ok {
		return
	}

	source := r.URL.Query().Get("source")
	// 两条取数分支回同一条上界：Search 固定 1000；ListBySource 此前无 limit
	//（?source=<大源> 可一次拉回整源正文，A2 审计 BB-05）。limit 可显式传，1..1000 夹紧。
	listLimit := 1000
	if v := r.URL.Query().Get("limit"); v != "" {
		n, perr := strconv.Atoi(v)
		if perr != nil || n < 1 || n > 1000 {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "limit must be an integer in [1,1000]"})
			return
		}
		listLimit = n
	}
	var docs []KnowledgeDoc
	var err error

	if source != "" {
		docs, err = h.store.ListBySource(authctx.NewContext(r.Context(), actx), KnowledgeSource(source), listLimit)
	} else {
		docs, err = h.store.Search(authctx.NewContext(r.Context(), actx), "", listLimit, nil)
	}
	if err != nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"documents": docs,
		"count":     len(docs),
	})
}

// HandleDelete 处理 DELETE /api/v1/knowledge/docs/{id}
func (h *KnowledgeHandler) HandleDelete(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodDelete {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	// 删除是破坏性动作 ⇒ knowledge:write。
	actx, ok := h.authorize(w, r, "knowledge:write")
	if !ok {
		return
	}

	if err := h.store.Delete(authctx.NewContext(r.Context(), actx), []string{id}); err != nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleStats 处理 GET /api/v1/knowledge/stats
func (h *KnowledgeHandler) HandleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	// 统计含各同步源的配置 ⇒ knowledge:read。
	actx, ok := h.authorize(w, r, "knowledge:read")
	if !ok {
		return
	}

	count, err := h.store.Count(authctx.NewContext(r.Context(), actx))
	if err != nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	configs := h.syncEng.GetConfigs()

	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"totalDocs":   count,
		"syncConfigs": configs,
	})
}

// HandleSyncConfigs 处理 GET /api/v1/knowledge/sync-configs
func (h *KnowledgeHandler) HandleSyncConfigs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	// 同步配置里含外部源标识 ⇒ knowledge:read（与 stats 同一口径）。
	_, ok := h.authorize(w, r, "knowledge:read")
	if !ok {
		return
	}

	configs := h.syncEng.GetConfigs()
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"configs": configs,
	})
}

// HandleUpdateSyncConfig 处理 PUT /api/v1/knowledge/sync-configs/{source}
func (h *KnowledgeHandler) HandleUpdateSyncConfig(w http.ResponseWriter, r *http.Request, source string) {
	if r.Method != http.MethodPut {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	// 改同步开关/周期会决定后续对外部源的拉取 ⇒ knowledge:write。
	_, ok := h.authorize(w, r, "knowledge:write")
	if !ok {
		return
	}

	var req struct {
		Enabled  bool   `json:"enabled"`
		Interval string `json:"interval"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	// interval 为空表示仅更新 enabled 开关，保留原同步周期（UpdateConfig 对 <=0 不覆盖）；
	// 非空但非法则拒绝请求，避免客户端误以为新周期已生效。
	var interval time.Duration
	if req.Interval != "" {
		parsed, err := time.ParseDuration(req.Interval)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid interval: " + err.Error()})
			return
		}
		interval = parsed
	}
	h.syncEng.UpdateConfig(KnowledgeSource(source), req.Enabled, interval)
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// RegisterKnowledgeRoutes 注册知识库路由。
//
// 权限映射（每个 handler 自查，见 KnowledgeHandler.Authorize）：
//   - knowledge:read  → POST /knowledge/search、GET /knowledge/docs、
//     GET /knowledge/stats、GET /knowledge/sync-configs；
//   - knowledge:write → POST /knowledge/sync、POST /knowledge/ingest、
//     DELETE /knowledge/docs/{id}、PUT /knowledge/sync-configs/{source}。
//
// 修复前这 8 条全部匿名可达（G1 同族：ai/agent、ai/gateway、k8slifecycle、aiops）。
func RegisterKnowledgeRoutes(mux *http.ServeMux, handler *KnowledgeHandler) {
	mux.HandleFunc("POST /api/v1/knowledge/sync", handler.HandleSync)
	mux.HandleFunc("POST /api/v1/knowledge/search", handler.HandleSearch)
	mux.HandleFunc("POST /api/v1/knowledge/ingest", handler.HandleIngest)
	mux.HandleFunc("GET /api/v1/knowledge/docs", handler.HandleList)
	mux.HandleFunc("DELETE /api/v1/knowledge/docs/", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len("/api/v1/knowledge/docs/"):]
		handler.HandleDelete(w, r, id)
	})
	mux.HandleFunc("GET /api/v1/knowledge/stats", handler.HandleStats)
	mux.HandleFunc("GET /api/v1/knowledge/sync-configs", handler.HandleSyncConfigs)
	mux.HandleFunc("PUT /api/v1/knowledge/sync-configs/", func(w http.ResponseWriter, r *http.Request) {
		source := r.URL.Path[len("/api/v1/knowledge/sync-configs/"):]
		handler.HandleUpdateSyncConfig(w, r, source)
	})
}
