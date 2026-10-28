package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"testing"

	gateway "github.com/Levango7/OpsMesh/internal/ai/gateway"
)

// fakeProvider 是可编排的 LLMProvider 替身：embedding 与补全结果由测试指定，
// 让混合检索/重排序可以在完全离线的条件下被验证。
type fakeProvider struct {
	embed    []float64
	embedErr error
	resp     *gateway.CompletionResponse
	compErr  error
	reqs     []gateway.CompletionRequest
}

func (p *fakeProvider) Name() string { return "fake" }

func (p *fakeProvider) Complete(_ context.Context, req gateway.CompletionRequest) (*gateway.CompletionResponse, error) {
	p.reqs = append(p.reqs, req)
	return p.resp, p.compErr
}

func (p *fakeProvider) Embed(context.Context, string) ([]float64, error) {
	return p.embed, p.embedErr
}

func (p *fakeProvider) ListModels() []string { return []string{"gpt-4o-mini"} }

func (p *fakeProvider) HealthCheck(context.Context) error { return nil }

func newFakeGateway(fp *fakeProvider) *gateway.Gateway {
	g := gateway.NewGateway()
	g.RegisterProvider(gateway.ProviderConfig{Name: "openai", Models: []string{"gpt-4o-mini"}, RateLimit: 1000}, fp)
	return g
}

// spyStore 记录检索参数并返回预设文档。
type spyStore struct {
	docs    []KnowledgeDoc
	lastTop int
	err     error
}

func (s *spyStore) Upsert(context.Context, []KnowledgeDoc) error { return nil }

func (s *spyStore) Search(_ context.Context, _ string, topK int, _ map[string]interface{}) ([]KnowledgeDoc, error) {
	s.lastTop = topK
	if s.err != nil {
		return nil, s.err
	}
	return s.docs, nil
}

func (s *spyStore) Delete(context.Context, []string) error { return nil }

func (s *spyStore) Count(context.Context) (int, error) { return len(s.docs), nil }

func (s *spyStore) ListBySource(context.Context, KnowledgeSource) ([]KnowledgeDoc, error) {
	return s.docs, nil
}

func TestHybridRetriever_Errors(t *testing.T) {
	t.Run("embedding 失败", func(t *testing.T) {
		sentinel := errors.New("embed down")
		fp := &fakeProvider{embedErr: sentinel}
		r := NewHybridRetriever(&spyStore{}, newFakeGateway(fp), 5)
		if _, err := r.Retrieve(context.Background(), "q", nil); !errors.Is(err, sentinel) {
			t.Fatalf("应返回 embedding 错误，得到 %v", err)
		}
	})

	t.Run("存储检索失败", func(t *testing.T) {
		sentinel := errors.New("store down")
		fp := &fakeProvider{embed: []float64{1, 0}}
		r := NewHybridRetriever(&spyStore{err: sentinel}, newFakeGateway(fp), 5)
		if _, err := r.Retrieve(context.Background(), "q", nil); !errors.Is(err, sentinel) {
			t.Fatalf("应返回存储错误，得到 %v", err)
		}
	})
}

// TestHybridRetriever_HybridScoring 用可算出的两条得分钉住加权公式：
// 向量命中（cos=1）应压过仅关键词命中，且每条结果的 Score 就是混合分。
func TestHybridRetriever_HybridScoring(t *testing.T) {
	query := "cpu"
	vectorHit := KnowledgeDoc{ID: "vector", Content: "unrelated text", Embedding: []float64{1, 0}}
	keywordHit := KnowledgeDoc{ID: "keyword", Content: "cpu cpu", Embedding: []float64{0, 1}}

	store := &spyStore{docs: []KnowledgeDoc{vectorHit, keywordHit}}
	fp := &fakeProvider{embed: []float64{1, 0}}
	r := NewHybridRetriever(store, newFakeGateway(fp), 10)

	out, err := r.Retrieve(context.Background(), query, nil)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(out) != 2 || out[0].ID != "vector" || out[1].ID != "keyword" {
		t.Fatalf("排序不对：%v", docsIDs(out))
	}

	wantVector := 0.6 // 0.6*cos(1,0)·(1,0)=0.6 + 0.4*bm25(无命中)=0.6
	wantKeyword := 0.4 * bm25Score(query, keywordHit.Content)
	if math.Abs(out[0].Score-wantVector) > 1e-9 {
		t.Errorf("向量命中得分 = %v，期望 %v", out[0].Score, wantVector)
	}
	if math.Abs(out[1].Score-wantKeyword) > 1e-9 {
		t.Errorf("关键词命中得分 = %v，期望 %v", out[1].Score, wantKeyword)
	}
}

func TestHybridRetriever_TopKAndOverFetch(t *testing.T) {
	docs := make([]KnowledgeDoc, 5)
	for i := range docs {
		docs[i] = KnowledgeDoc{ID: string(rune('a' + i))}
	}
	store := &spyStore{docs: docs}
	fp := &fakeProvider{embed: []float64{1}}
	r := NewHybridRetriever(store, newFakeGateway(fp), 2)

	out, err := r.Retrieve(context.Background(), "q", nil)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(out) != 2 {
		t.Errorf("topK=2 应只返回 2 条，得到 %d", len(out))
	}
	if store.lastTop != 6 {
		t.Errorf("存储层应被要求取 topK*3=6 条，实际 %d", store.lastTop)
	}
}

func docsIDs(docs []KnowledgeDoc) []string {
	out := make([]string, len(docs))
	for i, d := range docs {
		out[i] = d.ID
	}
	return out
}

func TestReranker_FallsBackOnError(t *testing.T) {
	fp := &fakeProvider{compErr: errors.New("llm down")}
	r := NewReranker(newFakeGateway(fp))
	docs := []KnowledgeDoc{{ID: "d1"}, {ID: "d2"}}

	got, err := r.Rerank(context.Background(), "q", docs)
	if err != nil {
		t.Fatalf("LLM 失败时应沿用原顺序且不报错，得到 %v", err)
	}
	if len(got) != 2 || got[0].ID != "d1" || got[1].ID != "d2" {
		t.Errorf("回退顺序不对：%v", docsIDs(got))
	}
}

// completionResp 按网关真实响应形状构造补全结果（choices[0].message.content）。
func completionResp(content string) *gateway.CompletionResponse {
	body := `{"choices":[{"index":0,"message":{"role":"assistant","content":` + strconv.Quote(content) + `}}]}`
	var resp gateway.CompletionResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		panic("构造补全响应失败: " + err.Error())
	}
	return &resp
}

func TestReranker_ReordersByModelOutput(t *testing.T) {

	cases := []struct {
		name    string
		content string
		want    []string
	}{
		{"标准 JSON 数组", "[2,1]", []string{"d2", "d1"}},
		{"夹杂散文", "排序结果如下：[2,1] 请查收", []string{"d2", "d1"}},
		{"越界与重复被忽略后补齐", "[2,2,9]", []string{"d2", "d1"}},
		{"非法 JSON 回退原序", "[x]", []string{"d1", "d2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fp := &fakeProvider{resp: completionResp(tc.content)}
			r := NewReranker(newFakeGateway(fp))
			docs := []KnowledgeDoc{{ID: "d1", Title: "t1", Content: "c1"}, {ID: "d2", Title: "t2", Content: "c2"}}

			got, err := r.Rerank(context.Background(), "q", docs)
			if err != nil {
				t.Fatalf("Rerank: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("结果数量 = %d，期望 %d", len(got), len(tc.want))
			}
			for i, w := range tc.want {
				if got[i].ID != w {
					t.Errorf("顺序[%d] = %s，期望 %s（全部：%v）", i, got[i].ID, w, docsIDs(got))
				}
			}
			if len(fp.reqs) != 1 {
				t.Errorf("应调用一次 LLM，实际 %d", len(fp.reqs))
			}
		})
	}
}
