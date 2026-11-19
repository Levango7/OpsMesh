// tenant_isolation_test.go — 批次49：KnowledgeDoc.TenantID 跨租户隔离契约。
//
// 口径（与 retrieval.go 的 ctxTenant 一致）：
//   - 租户只取自已验签的请求上下文（authctx），文档自带的 TenantID 不具权威性；
//   - 空租户（ctx 无身份）归一为 store.DefaultTenantID，即「平台默认租户」，
//     不是全局可见、也不是任何租户都能读写的通配符；
//   - 存储键 = 租户 + tenantSep + 文档 ID，同 ID 在不同租户下互不覆盖。
package knowledge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/authctx"
	"github.com/Levango7/OpsMesh/internal/store"
)

func TestKnowledgeTenantIsolation_SearchCountListDelete(t *testing.T) {
	ks := NewInMemoryKnowledgeStore()
	ctxA := ktCtx("tenant-a")
	ctxB := ktCtx("tenant-b")

	// 同 ID、不同租户：键里带租户，后写的不覆盖先写的。
	if err := ks.Upsert(ctxA, []KnowledgeDoc{{ID: "same-id", Source: SourceRunbook, Title: "A", Content: "secret-a"}}); err != nil {
		t.Fatalf("Upsert A: %v", err)
	}
	if err := ks.Upsert(ctxB, []KnowledgeDoc{{ID: "same-id", Source: SourceRunbook, Title: "B", Content: "secret-b"}}); err != nil {
		t.Fatalf("Upsert B: %v", err)
	}

	docsA, err := ks.Search(ctxA, "", 10, nil)
	if err != nil {
		t.Fatalf("Search A: %v", err)
	}
	if len(docsA) != 1 || docsA[0].ID != "same-id" || docsA[0].Content != "secret-a" {
		t.Fatalf("A 检索命中异常（同 ID 碰撞？）：%+v", docsA)
	}
	if docsA[0].TenantID != "tenant-a" {
		t.Fatalf("A 检索结果 TenantID = %q，期望 tenant-a", docsA[0].TenantID)
	}
	docsB, err := ks.Search(ctxB, "", 10, nil)
	if err != nil {
		t.Fatalf("Search B: %v", err)
	}
	if len(docsB) != 1 || docsB[0].Content != "secret-b" {
		t.Fatalf("B 检索命中异常：%+v", docsB)
	}

	// Count / ListBySource 同口径只数本租户。
	if n, _ := ks.Count(ctxA); n != 1 {
		t.Fatalf("A Count = %d，期望 1", n)
	}
	if n, _ := ks.Count(ctxB); n != 1 {
		t.Fatalf("B Count = %d，期望 1", n)
	}
	listA, _ := ks.ListBySource(ctxA, SourceRunbook, 0)
	if len(listA) != 1 || listA[0].Content != "secret-a" {
		t.Fatalf("A ListBySource 异常：%+v", listA)
	}
	listB, _ := ks.ListBySource(ctxB, SourceRunbook, 0)
	if len(listB) != 1 || listB[0].Content != "secret-b" {
		t.Fatalf("B ListBySource 异常：%+v", listB)
	}

	// B 删不掉 A 的文档（同 ID 也不行）。
	if err := ks.Delete(ctxB, []string{"same-id"}); err != nil {
		t.Fatalf("Delete B: %v", err)
	}
	if n, _ := ks.Count(ctxA); n != 1 {
		t.Fatalf("B 的删除影响了 A：A Count = %d", n)
	}
	docsA, _ = ks.Search(ctxA, "", 10, nil)
	if len(docsA) != 1 || docsA[0].Content != "secret-a" {
		t.Fatalf("B 的删除影响了 A 的内容：%+v", docsA)
	}
	if n, _ := ks.Count(ctxB); n != 0 {
		t.Fatalf("B 自己的文档应已删除，Count = %d", n)
	}
}

func TestKnowledgeTenantIsolation_EmptyTenantIsDefaultTenant(t *testing.T) {
	ks := NewInMemoryKnowledgeStore()

	// 空租户 == store.DefaultTenantID（平台默认租户），读写两侧同一口径。
	if err := ks.Upsert(context.Background(), []KnowledgeDoc{{ID: "platform-doc", Source: SourceManual, Content: "platform"}}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	docs, err := ks.Search(context.Background(), "", 10, nil)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(docs) != 1 || docs[0].TenantID != store.DefaultTenantID {
		t.Fatalf("空租户应归 %q：%+v", store.DefaultTenantID, docs)
	}
	if n, _ := ks.Count(ktCtx(store.DefaultTenantID)); n != 1 {
		t.Fatalf("显式 default 与空租户应是同一租户，Count = %d", n)
	}

	// 业务租户既看不到、也删不掉平台默认租户的文档。
	if docsB, _ := ks.Search(ktCtx("tenant-b"), "", 10, nil); len(docsB) != 0 {
		t.Fatalf("业务租户看到了 default 租户文档：%+v", docsB)
	}
	if err := ks.Delete(ktCtx("tenant-b"), []string{"platform-doc"}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if n, _ := ks.Count(context.Background()); n != 1 {
		t.Fatalf("业务租户删掉了 default 租户文档，Count = %d", n)
	}
}

func TestKnowledgeTenantIsolation_UpsertOverridesCallerTenant(t *testing.T) {
	ks := NewInMemoryKnowledgeStore()
	ctxA := ktCtx("tenant-a")

	// 文档自带的 TenantID 不可信：Upsert 必须以 ctx 租户为准，防止载荷伪造归属。
	if err := ks.Upsert(ctxA, []KnowledgeDoc{{ID: "spoof", TenantID: "tenant-b", Content: "x"}}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if n, _ := ks.Count(ktCtx("tenant-b")); n != 0 {
		t.Fatalf("载荷里的 TenantID 生效了：tenant-b Count = %d", n)
	}
	docsA, _ := ks.Search(ctxA, "", 10, nil)
	if len(docsA) != 1 || docsA[0].TenantID != "tenant-a" {
		t.Fatalf("文档未归入 ctx 租户：%+v", docsA)
	}
}

func doTenantRequest(t *testing.T, h http.HandlerFunc, method, target, body, tenant string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("X-Test-Tenant", tenant)
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

// TestKnowledgeHandler_SearchAndSyncUseRequestTenant 钉住 HTTP 面：
// 检索与同步都必须携带已验签的请求租户，裸 r.Context() 会静默落到 default 租户。
func TestKnowledgeHandler_SearchAndSyncUseRequestTenant(t *testing.T) {
	ks := NewInMemoryKnowledgeStore()
	h := NewKnowledgeHandler(ks, newFakeGateway(&fakeProvider{embed: []float64{1, 0}}))
	h.Authorize = func(_ http.ResponseWriter, r *http.Request, _ string) (authctx.Context, bool) {
		return authctx.Context{TenantID: r.Header.Get("X-Test-Tenant")}, true
	}

	rec := doTenantRequest(t, h.HandleIngest, http.MethodPost, "/api/v1/knowledge/ingest",
		`{"documents":[{"id":"a-doc","source":"manual","title":"A","content":"tenant a secret"}]}`, "tenant-a")
	if rec.Code != http.StatusOK {
		t.Fatalf("ingest 失败：code=%d body=%s", rec.Code, rec.Body.String())
	}

	decodeDocs := func(rec *httptest.ResponseRecorder) []KnowledgeDoc {
		t.Helper()
		var payload struct {
			Documents []KnowledgeDoc `json:"documents"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("响应不是 JSON：%v（body=%s）", err, rec.Body.String())
		}
		return payload.Documents
	}

	if got := decodeDocs(doTenantRequest(t, h.HandleSearch, http.MethodPost, "/api/v1/knowledge/search", `{"query":"secret"}`, "tenant-a")); len(got) != 1 || got[0].ID != "a-doc" {
		t.Fatalf("tenant-a 检索应命中自己的文档：%+v", got)
	}
	if got := decodeDocs(doTenantRequest(t, h.HandleSearch, http.MethodPost, "/api/v1/knowledge/search", `{"query":"secret"}`, "tenant-b")); len(got) != 0 {
		t.Fatalf("tenant-b 检索看到了 tenant-a 的文档：%+v", got)
	}

	// 同步结果也必须落在触发方租户（此前裸 r.Context() 会写进 default）。
	rec = doTenantRequest(t, h.HandleSync, http.MethodPost, "/api/v1/knowledge/sync", `{"source":"git"}`, "tenant-b")
	if rec.Code != http.StatusOK {
		t.Fatalf("sync 失败：code=%d body=%s", rec.Code, rec.Body.String())
	}
	if n, _ := ks.Count(ktCtx("tenant-a")); n != 1 {
		t.Fatalf("tenant-a Count 被 tenant-b 的同步污染：%d", n)
	}
	if n, _ := ks.Count(ktCtx("tenant-b")); n != 1 {
		t.Fatalf("tenant-b 同步结果未落本租户：Count = %d", n)
	}
	if n, _ := ks.Count(ktCtx(store.DefaultTenantID)); n != 0 {
		t.Fatalf("同步结果写进了 default 租户：Count = %d", n)
	}
}
