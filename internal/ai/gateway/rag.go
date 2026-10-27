// Package aigateway 向量存储与 RAG 检索
package aigateway

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
)

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
		s.docs[d.ID] = d
	}
	return nil
}

func (s *InMemoryVectorStore) Search(ctx context.Context, embedding []float64, topK int, filter map[string]interface{}) ([]VectorDocument, error) {
	type scored struct {
		doc   VectorDocument
		score float64
	}
	var results []scored

	for _, doc := range s.docs {
		// 过滤
		if !matchFilter(doc.Metadata, filter) {
			continue
		}
		score := cosineSimilarity(embedding, doc.Embedding)
		results = append(results, scored{doc: doc, score: score})
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
	for _, id := range ids {
		delete(s.docs, id)
	}
	return nil
}

func (s *InMemoryVectorStore) Count(ctx context.Context) (int, error) {
	return len(s.docs), nil
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
