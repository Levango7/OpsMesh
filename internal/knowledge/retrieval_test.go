package knowledge

import (
	"context"
	"math"
	"strings"
	"testing"
)

func TestCosineSimilarity(t *testing.T) {
	cases := []struct {
		name string
		a, b []float64
		want float64
	}{
		{"同向量", []float64{1, 2, 3}, []float64{1, 2, 3}, 1},
		{"正交", []float64{1, 0}, []float64{0, 1}, 0},
		{"反向", []float64{1, 0}, []float64{-1, 0}, -1},
		{"长度不等", []float64{1, 2}, []float64{1}, 0},
		{"空向量", nil, []float64{1}, 0},
		{"零向量", []float64{0, 0}, []float64{1, 1}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := cosineSimilarity(tc.a, tc.b)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("cosineSimilarity() = %v，期望 %v", got, tc.want)
			}
		})
	}
}

// TestBM25Score 钉住简化打分公式：idf×tf/(tf+1.5) 再按查询词数取平均。
// 语义变化（如改成求和而非平均）会直接改变检索排序，故按数值断言。
func TestBM25Score(t *testing.T) {
	single := math.Log(2.0) / 2.5

	cases := []struct {
		name  string
		query string
		doc   string
		want  float64
	}{
		{"空查询", "", "cpu usage", 0},
		{"空文档", "cpu", "", 0},
		{"无命中", "zzz", "cpu usage", 0},
		{"单次命中", "cpu", "CPU usage high", single},
		{"大小写不敏感", "CPU", "cpu usage high", single},
		{"子串也算命中", "cpu", "cpuusage", single},
		{"词频越高分越高", "cpu", "cpu cpu cpu", math.Log(2.0) * 3 / 4.5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := bm25Score(tc.query, tc.doc)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("bm25Score(%q, %q) = %v，期望 %v", tc.query, tc.doc, got, tc.want)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("abc", 5); got != "abc" {
		t.Errorf("未超长不应截断，得到 %q", got)
	}
	if got := truncate("abcde", 5); got != "abcde" {
		t.Errorf("恰好等于上限不应截断，得到 %q", got)
	}
	if got := truncate("abcdef", 3); got != "abc..." {
		t.Errorf("超长应截断并加省略号，得到 %q", got)
	}
}

func TestMatchKnowledgeFilter(t *testing.T) {
	meta := map[string]interface{}{"ci_type": "server", "status": "online"}
	cases := []struct {
		name   string
		meta   map[string]interface{}
		filter map[string]interface{}
		want   bool
	}{
		{"无过滤", meta, nil, true},
		{"空过滤", meta, map[string]interface{}{}, true},
		{"全部命中", meta, map[string]interface{}{"ci_type": "server"}, true},
		{"一个不命中", meta, map[string]interface{}{"ci_type": "server", "status": "offline"}, false},
		{"键不存在", meta, map[string]interface{}{"nope": "x"}, false},
		{"nil metadata 配非空过滤", nil, map[string]interface{}{"k": "v"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchKnowledgeFilter(tc.meta, tc.filter); got != tc.want {
				t.Errorf("matchKnowledgeFilter() = %v，期望 %v", got, tc.want)
			}
		})
	}
}

func TestInMemoryKnowledgeStore(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryKnowledgeStore()

	docs := []KnowledgeDoc{
		{ID: "d1", Source: SourceCMDB, Title: "web-01", Content: "server", Metadata: map[string]interface{}{"ci_type": "server"}},
		{ID: "d2", Source: SourceAlert, Title: "cpu high", Content: "alert", Metadata: map[string]interface{}{"severity": "critical"}},
		{ID: "d3", Source: SourceCMDB, Title: "db-01", Content: "database", Metadata: map[string]interface{}{"ci_type": "database"}},
	}
	if err := s.Upsert(ctx, docs); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if n, _ := s.Count(ctx); n != 3 {
		t.Fatalf("Count() = %d，期望 3", n)
	}

	// 覆盖写入同 ID 不应增加计数。
	if err := s.Upsert(ctx, []KnowledgeDoc{{ID: "d1", Source: SourceCMDB, Title: "web-01-renamed", Metadata: map[string]interface{}{"ci_type": "server"}}}); err != nil {
		t.Fatalf("Upsert 覆盖: %v", err)
	}
	if n, _ := s.Count(ctx); n != 3 {
		t.Errorf("覆盖写入后 Count() = %d，期望 3", n)
	}

	got, _ := s.Search(ctx, "", 0, nil)
	if len(got) != 3 {
		t.Errorf("topK<=0 应返回全部，得到 %d", len(got))
	}
	if got, _ := s.Search(ctx, "", 2, nil); len(got) != 2 {
		t.Errorf("topK=2 应截断到 2，得到 %d", len(got))
	}
	filtered, _ := s.Search(ctx, "", 0, map[string]interface{}{"ci_type": "server"})
	if len(filtered) != 1 || filtered[0].ID != "d1" {
		t.Errorf("过滤 ci_type=server 应只返回 d1，得到 %+v", filtered)
	}

	cmdb, _ := s.ListBySource(ctx, SourceCMDB)
	if len(cmdb) != 2 {
		t.Errorf("ListBySource(cmdb) 数量 = %d，期望 2", len(cmdb))
	}

	if err := s.Delete(ctx, []string{"d2", "nope"}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if n, _ := s.Count(ctx); n != 2 {
		t.Errorf("删除后 Count() = %d，期望 2（不存在的 ID 应忽略）", n)
	}
}

func TestCitationTracker(t *testing.T) {
	ct := NewCitationTracker()
	existing := &Citation{Source: SourceGit, SourceID: "keep", Excerpt: "保留"}

	long := strings.Repeat("字", 200)
	docs := []KnowledgeDoc{
		{ID: "d1", Source: SourceCMDB, Title: "web-01", Content: "short content", Score: 0.42},
		{ID: "d2", Source: SourceAlert, Title: "cpu high", Content: long, Score: 0.9, Citation: existing},
	}
	tracked := ct.Track(docs, "cpu")

	if tracked[0].Citation == nil {
		t.Fatal("无引用时应生成 Citation")
	}
	if tracked[0].Citation.SourceID != "d1" || tracked[0].Citation.Source != SourceCMDB {
		t.Errorf("Citation 溯源字段不对：%+v", tracked[0].Citation)
	}
	if tracked[0].Citation.Relevance != 0.42 {
		t.Errorf("Relevance 应取文档 Score，得到 %v", tracked[0].Citation.Relevance)
	}
	if tracked[0].Citation.Excerpt != "short content" {
		t.Errorf("短内容不应截断，得到 %q", tracked[0].Citation.Excerpt)
	}
	if tracked[1].Citation != existing {
		t.Error("已有 Citation 不应被覆盖")
	}

	if got := ct.FormatCitations(nil); got != "" {
		t.Errorf("空文档应返回空串，得到 %q", got)
	}
	md := ct.FormatCitations(tracked)
	for _, want := range []string{"参考来源", "web-01", "相关度: 0.42", "cpu high"} {
		if !strings.Contains(md, want) {
			t.Errorf("格式化引用缺少 %q：\n%s", want, md)
		}
	}

	// 未 Track 的文档走 else 分支：用 Source 输出、不带相关度。
	raw := ct.FormatCitations([]KnowledgeDoc{{Source: SourceManual, Title: "手工文档"}})
	if !strings.Contains(raw, "[manual] 手工文档") || strings.Contains(raw, "相关度") {
		t.Errorf("未溯源文档格式不对：\n%s", raw)
	}
}

func TestNewHybridRetriever_DefaultTopK(t *testing.T) {
	r := NewHybridRetriever(NewInMemoryKnowledgeStore(), nil, 0)
	if r.topK != 10 {
		t.Errorf("topK<=0 应回落为 10，得到 %d", r.topK)
	}
	if r.vectorW != 0.6 || r.keywordW != 0.4 {
		t.Errorf("默认权重 = (%v, %v)，期望 (0.6, 0.4)", r.vectorW, r.keywordW)
	}
}

// TestReranker_SingleDocShortCircuit 钉住"单条结果不调用 LLM"的短路：
// 传入的 gateway 为 nil，一旦提前返回失效就会 panic。
func TestReranker_SingleDocShortCircuit(t *testing.T) {
	r := NewReranker(nil)
	docs := []KnowledgeDoc{{ID: "d1"}}
	got, err := r.Rerank(context.Background(), "q", docs)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if len(got) != 1 || got[0].ID != "d1" {
		t.Errorf("单条结果应原样返回，得到 %+v", got)
	}
}
