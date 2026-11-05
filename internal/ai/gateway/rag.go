// Package aigateway 向量存储与 RAG 检索
package aigateway

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/Levango7/OpsMesh/internal/authctx"
	"github.com/Levango7/OpsMesh/internal/store"
)

// tenantSep 是租户隔离键的分隔符（unit separator：文档 ID 由用户经 API 提供，
// 可能含 "/" 等字符，用可打印分隔符会被构造出跨租户碰撞键；\x1f 几乎不会出现在 ID 里）。
const tenantSep = "\x1f"

// tenantKey 把「裸文档 ID」编成「租户隔离键」：tenant + \x1f + id。
// 空租户归 store.DefaultTenantID（与平台租户先例同口径，见 internal/controlplane/auth_users.go）。
// TD-97 ②：A 租户写入的文档不会出现在 B 租户的检索/删除/计数里。
func tenantKey(ctx context.Context, id string) string {
	tenant := authctx.FromContext(ctx).TenantID
	if tenant == "" {
		tenant = store.DefaultTenantID
	}
	return tenant + tenantSep + id
}

// tenantPrefix 返回当前租户的键前缀（Scan 类操作用来只扫本租户）。
func tenantPrefix(ctx context.Context) string {
	tenant := authctx.FromContext(ctx).TenantID
	if tenant == "" {
		tenant = store.DefaultTenantID
	}
	return tenant + tenantSep
}

// bareID 从租户隔离键还原裸 ID（翻过前缀即原始 ID；无前缀时原样返回，
// 兼容未经本函数编码的历史键——但注意那也意味着可被任何租户看到，
// 故 InMemory store 之外的生产存储不应有未编码键）。
func bareID(key string) string {
	if i := strings.Index(key, tenantSep); i >= 0 {
		return key[i+len(tenantSep):]
	}
	return key
}

// VectorDocument 向量文档
type VectorDocument struct {
	ID        string                 `json:"id"`
	Content   string                 `json:"content"`
	Embedding []float64              `json:"embedding"`
	Metadata  map[string]interface{} `json:"metadata"`
	Score     float64                `json:"score,omitempty"`
}

// VectorStore 向量存储接口
type VectorStore interface {
	Upsert(ctx context.Context, docs []VectorDocument) error
	Search(ctx context.Context, embedding []float64, topK int, filter map[string]interface{}) ([]VectorDocument, error)
	Delete(ctx context.Context, ids []string) error
	Count(ctx context.Context) (int, error)
}

// InMemoryVectorStore 内存向量存储 (开发/测试用)
type InMemoryVectorStore struct {
	docs map[string]VectorDocument
}

func NewInMemoryVectorStore() *InMemoryVectorStore {
	return &InMemoryVectorStore{docs: make(map[string]VectorDocument)}
}

func (s *InMemoryVectorStore) Upsert(ctx context.Context, docs []VectorDocument) error {
	for _, d := range docs {
		s.docs[tenantKey(ctx, d.ID)] = d
	}
	return nil
}

func (s *InMemoryVectorStore) Search(ctx context.Context, embedding []float64, topK int, filter map[string]interface{}) ([]VectorDocument, error) {
	type scored struct {
		doc   VectorDocument
		score float64
	}
	var results []scored

	prefix := tenantPrefix(ctx)
	for key, doc := range s.docs {
		// TD-97 ②：只检索本租户的文档（编码键的前缀即租户）。
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		// 过滤
		if !matchFilter(doc.Metadata, filter) {
			continue
		}
		score := cosineSimilarity(embedding, doc.Embedding)
		// 出口还原裸 ID：调用方（handler/上层）只见自己写入时的 ID。
		doc.ID = bareID(key)
		results = append(results, scored{doc, score})
	}

	// 排序
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	// Top-K
	if topK > len(results) {
		topK = len(results)
	}
	out := make([]VectorDocument, 0, topK)
	for i := 0; i < topK; i++ {
		d := results[i].doc
		d.Score = results[i].score
		out = append(out, d)
	}
	return out, nil
}

func (s *InMemoryVectorStore) Delete(ctx context.Context, ids []string) error {
	// 按本租户键删：A 租户传 B 租户的文档 ID 删不到（键前缀不匹配即不存在）。
	for _, id := range ids {
		delete(s.docs, tenantKey(ctx, id))
	}
	return nil
}

func (s *InMemoryVectorStore) Count(ctx context.Context) (int, error) {
	prefix := tenantPrefix(ctx)
	n := 0
	for key := range s.docs {
		if strings.HasPrefix(key, prefix) {
			n++
		}
	}
	return n, nil
}

func matchFilter(metadata map[string]interface{}, filter map[string]interface{}) bool {
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

func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// RAGRetriever RAG 检索器
type RAGRetriever struct {
	vectorStore VectorStore
	gateway     *Gateway
	topK        int
}

func NewRAGRetriever(vs VectorStore, gw *Gateway, topK int) *RAGRetriever {
	if topK <= 0 {
		topK = 5
	}
	return &RAGRetriever{
		vectorStore: vs,
		gateway:     gw,
		topK:        topK,
	}
}

// Retrieve 检索相关文档
func (r *RAGRetriever) Retrieve(ctx context.Context, query string, filter map[string]interface{}) ([]VectorDocument, error) {
	// 1. 生成查询向量
	embedding, err := r.gateway.Embed(ctx, "openai", query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	// 2. 向量检索
	docs, err := r.vectorStore.Search(ctx, embedding, r.topK, filter)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}

	return docs, nil
}

// BuildContext 构建 RAG 上下文 (用于 LLM prompt)
func (r *RAGRetriever) BuildContext(docs []VectorDocument) string {
	if len(docs) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("参考知识：\n")
	for i, doc := range docs {
		fmt.Fprintf(&sb, "[%d] %s\n", i+1, doc.Content)
		if len(doc.Metadata) > 0 {
			fmt.Fprintf(&sb, "    元数据: %v\n", doc.Metadata)
		}
	}
	return sb.String()
}
