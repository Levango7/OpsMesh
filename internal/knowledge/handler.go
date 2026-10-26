// Package knowledge HTTP handlers
package knowledge

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	gateway "github.com/Levango7/OpsMesh/internal/ai/gateway"
	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// KnowledgeHandler 知识库 HTTP handler
type KnowledgeHandler struct {
	store     KnowledgeStore
	syncEng   *SyncEngine
	retriever *HybridRetriever
	reranker  *Reranker
	citation  *CitationTracker
	gw   *gateway.Gateway
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
		gw:   gw,
	}
}

// HandleSync 处理 POST /api/v1/knowledge/sync
func (h *KnowledgeHandler) HandleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		Source string `json:"source"` // empty = sync all
	}
	json.NewDecoder(r.Body).Decode(&req)

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
		docs, _ = h.reranker.Rerank(r.Context(), req.Query, docs)
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

	if err := h.store.Upsert(r.Context(), req.Documents); err != nil {
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

	source := r.URL.Query().Get("source")
	var docs []KnowledgeDoc
	var err error

	if source != "" {
		docs, err = h.store.ListBySource(r.Context(), KnowledgeSource(source))
	} else {
		docs, err = h.store.Search(r.Context(), "", 1000, nil)
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

	if err := h.store.Delete(r.Context(), []string{id}); err != nil {
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

	count, _ := h.store.Count(r.Context())
	configs := h.syncEng.GetConfigs()

	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"totalDocs":    count,
		"syncConfigs":  configs,
	})
}

// HandleSyncConfigs 处理 GET /api/v1/knowledge/sync-configs
func (h *KnowledgeHandler) HandleSyncConfigs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
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

	var req struct {
		Enabled  bool   `json:"enabled"`
		Interval string `json:"interval"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	interval, _ := time.ParseDuration(req.Interval)
	h.syncEng.UpdateConfig(KnowledgeSource(source), req.Enabled, interval)
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// RegisterKnowledgeRoutes 注册知识库路由
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
