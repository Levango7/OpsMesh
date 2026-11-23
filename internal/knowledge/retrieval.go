// Package knowledge 混合检索、重排序、引用溯源
package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	gateway "github.com/Levango7/OpsMesh/internal/ai/gateway"
	"github.com/Levango7/OpsMesh/internal/authctx"
	"github.com/Levango7/OpsMesh/internal/store"
)

// tenantSep / tenantKey / tenantPrefix / bareID：TD-97 ② 租户隔离键
// （与 internal/ai/gateway 同语义——knowledge 的 InMemory store 也曾全局单份）。
const tenantSep = "\x1f"

func tenantKey(ctx context.Context, id string) string {
	tenant := authctx.FromContext(ctx).TenantID
	if tenant == "" {
		tenant = store.DefaultTenantID
	}
	return tenant + tenantSep + id
}

func tenantPrefix(ctx context.Context) string {
	tenant := authctx.FromContext(ctx).TenantID
	if tenant == "" {
		tenant = store.DefaultTenantID
	}
	return tenant + tenantSep
}

func bareID(key string) string {
	if i := strings.Index(key, tenantSep); i >= 0 {
		return key[i+len(tenantSep):]
	}
	return key
}

// ctxTenant 返回当前请求的规范化租户 ID（空归 default）。
func ctxTenant(ctx context.Context) string {
	tenant := authctx.FromContext(ctx).TenantID
	if tenant == "" {
		tenant = store.DefaultTenantID
	}
	return tenant
}

// HybridRetriever 混合检索器 (向量 + 关键词 + 图谱)
type HybridRetriever struct {
	store    KnowledgeStore
	gw       *gateway.Gateway
	topK     int
	vectorW  float64 // 向量权重
	keywordW float64 // 关键词权重
}

func NewHybridRetriever(store KnowledgeStore, gw *gateway.Gateway, topK int) *HybridRetriever {
	if topK <= 0 {
		topK = 10
	}
	return &HybridRetriever{
		store:    store,
		gw:       gw,
		topK:     topK,
		vectorW:  0.6,
		keywordW: 0.4,
	}
}

// Retrieve 混合检索
func (r *HybridRetriever) Retrieve(ctx context.Context, query string, filter map[string]interface{}) ([]KnowledgeDoc, error) {
	// 1. 向量检索
	embedding, err := r.gw.Embed(ctx, "openai", query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	// 从 store 获取所有文档
	allDocs, err := r.store.Search(ctx, query, r.topK*3, filter)
	if err != nil {
		return nil, fmt.Errorf("store search: %w", err)
	}

	// 2. 计算混合分数
	type scoredDoc struct {
		doc   KnowledgeDoc
		score float64
	}
	var scored []scoredDoc

	for _, doc := range allDocs {
		vectorScore := 0.0
		if len(doc.Embedding) > 0 && len(embedding) > 0 {
			vectorScore = cosineSimilarity(embedding, doc.Embedding)
		}
		keywordScore := bm25Score(query, doc.Content)
		hybridScore := r.vectorW*vectorScore + r.keywordW*keywordScore

		doc.Score = hybridScore
		scored = append(scored, scoredDoc{doc: doc, score: hybridScore})
	}

	// 3. 排序
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// 4. Top-K。limit 必须是局部变量：r.topK 是共享实例的检索上界，
	// 把「本次结果数」写回去会将后续所有查询的上界永久收窄到首查结果数。
	limit := r.topK
	if limit > len(scored) {
		limit = len(scored)
	}
	out := make([]KnowledgeDoc, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, scored[i].doc)
	}

	return out, nil
}

// cosineSimilarity 计算两个向量的余弦相似度
func cosineSimilarity(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
func bm25Score(query, doc string) float64 {
	queryTerms := strings.Fields(strings.ToLower(query))
	docLower := strings.ToLower(doc)
	docTerms := strings.Fields(docLower)

	if len(queryTerms) == 0 || len(docTerms) == 0 {
		return 0
	}

	score := 0.0
	for _, qt := range queryTerms {
		tf := 0
		for _, dt := range docTerms {
			if strings.Contains(dt, qt) {
				tf++
			}
		}
		if tf > 0 {
			// 简化 IDF: 假设文档频率为 1
			idf := math.Log(2.0)
			score += idf * float64(tf) / (float64(tf) + 1.5)
		}
	}
	return score / float64(len(queryTerms))
}

// ============================================================================
// Re-Ranker 重排序器
// ============================================================================

// Reranker 重排序器
type Reranker struct {
	gw *gateway.Gateway
}

func NewReranker(gw *gateway.Gateway) *Reranker {
	return &Reranker{gw: gw}
}

// Rerank 使用 LLM 对检索结果进行重排序
func (r *Reranker) Rerank(ctx context.Context, query string, docs []KnowledgeDoc) ([]KnowledgeDoc, error) {
	if len(docs) <= 1 {
		return docs, nil
	}

	// 构建重排序 prompt
	var sb strings.Builder
	fmt.Fprintf(&sb, "查询: %s\n\n", query)
	sb.WriteString("请对以下文档按与查询的相关性进行排序，输出排序后的文档编号列表（JSON 数组）：\n")
	for i, doc := range docs {
		fmt.Fprintf(&sb, "[%d] %s: %s\n", i+1, doc.Title, truncate(doc.Content, 200))
	}

	resp, err := r.gw.Complete(ctx, gateway.CompletionRequest{
		Model:       "gpt-4o-mini",
		Messages:    []gateway.Message{{Role: gateway.RoleUser, Content: sb.String()}},
		Temperature: 0.1,
		MaxTokens:   200,
	})
	if err != nil {
		// 重排序失败，返回原始顺序
		return docs, nil
	}

	// 解析排序结果
	var order []int
	if len(resp.Choices) == 0 {
		// 上游返回空 choices（同为 LLM 侧失败形态）：沿用上方失败兜底，原始顺序返回。
		return docs, nil
	}
	content := resp.Choices[0].Message.Content
	// 尝试提取 JSON 数组
	start := strings.Index(content, "[")
	end := strings.LastIndex(content, "]")
	if start >= 0 && end > start {
		jsonStr := content[start : end+1]
		if err := json.Unmarshal([]byte(jsonStr), &order); err != nil {
			// 排序结果解析失败：放弃重排序，沿用原始顺序（与上方 LLM 调用失败同一兜底策略）
			order = nil
		}
	}

	if len(order) == 0 {
		return docs, nil
	}

	// 重新排序
	reranked := make([]KnowledgeDoc, 0, len(docs))
	used := make(map[int]bool)
	for _, idx := range order {
		if idx >= 1 && idx <= len(docs) && !used[idx-1] {
			reranked = append(reranked, docs[idx-1])
			used[idx-1] = true
		}
	}
	// 添加未排序的
	for i, doc := range docs {
		if !used[i] {
			reranked = append(reranked, doc)
		}
	}

	return reranked, nil
}

// ============================================================================
// Citation Tracker 引用溯源器
// ============================================================================

// CitationTracker 引用溯源器
type CitationTracker struct{}

func NewCitationTracker() *CitationTracker {
	return &CitationTracker{}
}

// Track 为检索结果生成引用溯源
func (ct *CitationTracker) Track(docs []KnowledgeDoc, query string) []KnowledgeDoc {
	for i := range docs {
		if docs[i].Citation == nil {
			docs[i].Citation = &Citation{
				Source:    docs[i].Source,
				SourceID:  docs[i].ID,
				Excerpt:   truncate(docs[i].Content, 150),
				Relevance: docs[i].Score,
			}
		}
	}
	return docs
}

// FormatCitations 格式化引用为 Markdown
func (ct *CitationTracker) FormatCitations(docs []KnowledgeDoc) string {
	if len(docs) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\n---\n**参考来源：**\n")
	for i, doc := range docs {
		if doc.Citation != nil {
			fmt.Fprintf(&sb, "%d. [%s] %s (相关度: %.2f)\n", i+1, doc.Citation.Source, doc.Title, doc.Citation.Relevance)
		} else {
			fmt.Fprintf(&sb, "%d. [%s] %s\n", i+1, doc.Source, doc.Title)
		}
	}
	return sb.String()
}

// ============================================================================
// InMemoryKnowledgeStore 内存知识存储
// ============================================================================

type InMemoryKnowledgeStore struct {
	mu   sync.RWMutex
	docs map[string]KnowledgeDoc
}

func NewInMemoryKnowledgeStore() *InMemoryKnowledgeStore {
	return &InMemoryKnowledgeStore{docs: make(map[string]KnowledgeDoc)}
}

// cloneKnowledgeDoc 隔离调用方与存储内的可变引用；Citation 也必须复制，
// 否则 CitationTracker 在锁外补引用时会回写存储对象。
func cloneKnowledgeDoc(doc KnowledgeDoc) KnowledgeDoc {
	cp := doc
	if doc.Embedding != nil {
		cp.Embedding = make([]float64, len(doc.Embedding))
		copy(cp.Embedding, doc.Embedding)
	}
	if doc.Metadata != nil {
		cp.Metadata = make(map[string]interface{}, len(doc.Metadata))
		for key, value := range doc.Metadata {
			cp.Metadata[key] = value
		}
	}
	if doc.Citation != nil {
		citation := *doc.Citation
		cp.Citation = &citation
	}
	return cp
}

func (s *InMemoryKnowledgeStore) Upsert(ctx context.Context, docs []KnowledgeDoc) error {
	tenant := ctxTenant(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range docs {
		doc := cloneKnowledgeDoc(docs[i])
		doc.TenantID = tenant
		s.docs[tenantKey(ctx, doc.ID)] = doc
	}
	return nil
}

func (s *InMemoryKnowledgeStore) Search(ctx context.Context, query string, topK int, filter map[string]interface{}) ([]KnowledgeDoc, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var results []KnowledgeDoc
	prefix := tenantPrefix(ctx)
	currentTenant := ctxTenant(ctx)
	for key, doc := range s.docs {
		// TD-97 ②：只检索本租户的文档。
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		// 显式按 TenantID 过滤。
		docTenant := doc.TenantID
		if docTenant == "" {
			docTenant = store.DefaultTenantID
		}
		if docTenant != currentTenant {
			continue
		}
		if !matchKnowledgeFilter(doc.Metadata, filter) {
			continue
		}
		doc = cloneKnowledgeDoc(doc)
		doc.ID = bareID(key) // 出口还原裸 ID
		if doc.TenantID == "" {
			doc.TenantID = currentTenant
		}
		results = append(results, doc)
	}
	if topK > 0 && topK < len(results) {
		results = results[:topK]
	}
	return results, nil
}

func (s *InMemoryKnowledgeStore) Delete(ctx context.Context, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	currentTenant := ctxTenant(ctx)
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	for key, doc := range s.docs {
		if !idSet[bareID(key)] {
			continue
		}
		docTenant := doc.TenantID
		if docTenant == "" {
			docTenant = store.DefaultTenantID
		}
		if docTenant != currentTenant {
			continue
		}
		delete(s.docs, key)
	}
	return nil
}

func (s *InMemoryKnowledgeStore) Count(ctx context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	prefix := tenantPrefix(ctx)
	currentTenant := ctxTenant(ctx)
	n := 0
	for key, doc := range s.docs {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		docTenant := doc.TenantID
		if docTenant == "" {
			docTenant = store.DefaultTenantID
		}
		if docTenant != currentTenant {
			continue
		}
		n++
	}
	return n, nil
}

// ListBySource 列出指定来源的文档；limit<=0 视为无界（仅内部调用允许），
// HTTP 面必须传正上限——旧版无 limit，`?source=<大源>` 一次拉回整源正文（含 content），
// 单请求内存/带宽无上界，与同端点 Search 的 1000 条承诺自相矛盾（A2 审计 BB-05）。
func (s *InMemoryKnowledgeStore) ListBySource(ctx context.Context, source KnowledgeSource, limit int) ([]KnowledgeDoc, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var results []KnowledgeDoc
	prefix := tenantPrefix(ctx)
	currentTenant := ctxTenant(ctx)
	for key, doc := range s.docs {
		// TD-97 ②：只列本租户的文档。
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		// 显式按 TenantID 过滤。
		docTenant := doc.TenantID
		if docTenant == "" {
			docTenant = store.DefaultTenantID
		}
		if docTenant != currentTenant {
			continue
		}
		if doc.Source == source {
			doc = cloneKnowledgeDoc(doc)
			doc.ID = bareID(key)
			if doc.TenantID == "" {
				doc.TenantID = currentTenant
			}
			results = append(results, doc)
			if limit > 0 && len(results) >= limit {
				break
			}
		}
	}
	return results, nil
}

func matchKnowledgeFilter(metadata map[string]interface{}, filter map[string]interface{}) bool {
	if filter == nil {
		return true
	}
	for k, v := range filter {
		if metadata[k] != v {
			return false
		}
	}
	return true
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
