package knowledge

// list_limit_test.go — A2 审计 BB-05 的钉子：?source= 分支此前无上界，
// `?source=<大源>` 一次拉回整源正文（含 content），单请求内存/带宽无上界。
// 现在两条分支同一条上界（1..1000，显式可传），非法值显式 400（不静默忽略，A2/BB-02 口径）。
import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListBySourceRespectsLimit(t *testing.T) {
	s := NewInMemoryKnowledgeStore()
	const total = 7
	docs := make([]KnowledgeDoc, 0, total)
	for i := 0; i < total; i++ {
		docs = append(docs, KnowledgeDoc{
			ID:      "d" + string(rune('a'+i)),
			Source:  KnowledgeSource("manual"),
			Title:   "doc",
			Content: "content",
		})
	}
	ctx := context.Background()
	if err := s.Upsert(ctx, docs); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := s.ListBySource(ctx, KnowledgeSource("manual"), 3)
	if err != nil {
		t.Fatalf("ListBySource: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("limit=3 应截到 3 条，得到 %d", len(got))
	}

	got, err = s.ListBySource(ctx, KnowledgeSource("manual"), 0)
	if err != nil {
		t.Fatalf("ListBySource(limit=0): %v", err)
	}
	if len(got) != total {
		t.Fatalf("limit<=0 视为无界（仅内部允许），应返回全部 %d 条，得到 %d", total, len(got))
	}
}

func TestHandleListLimitParamValidation(t *testing.T) {
	h := NewKnowledgeHandler(NewInMemoryKnowledgeStore(), nil)

	// 非法 limit 显式 400（不静默回默认值——A2/BB-02：静默忽略是最容易被
	// 误当"数据缺失"的一类问题）
	rec := httptest.NewRecorder()
	h.HandleList(rec, httptest.NewRequest(http.MethodGet, "/api/v1/knowledge/docs?limit=abc", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("limit=abc 应 400，得到 %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.HandleList(rec, httptest.NewRequest(http.MethodGet, "/api/v1/knowledge/docs?limit=5000", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("limit=5000 应 400（上限 1000），得到 %d %s", rec.Code, rec.Body.String())
	}

	// 合法 limit 生效
	rec = httptest.NewRecorder()
	h.HandleList(rec, httptest.NewRequest(http.MethodGet, "/api/v1/knowledge/docs?limit=2", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("limit=2 应 200，得到 %d %s", rec.Code, rec.Body.String())
	}
}
