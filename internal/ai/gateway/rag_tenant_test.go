// rag_tenant_test.go — TD-97 ② 共享存储租户隔离的行为契约测试。
//
// 背景：InMemoryVectorStore / PromptTemplateStore / InMemoryKnowledgeStore 此前都是
// 全局单份 map、按裸 ID 键——A 租户写入的文档会出现在 B 租户的检索结果里
// （tech-debt.md TD-97 ②）。本文件把修好后的隔离语义钉住：
//   - Upsert 后只有同租户能 Search/Delete/Count 到；
//   - 跨租户同 ID 不互相覆盖（各自独立命名空间）；
//   - 出口还原裸 ID（调用方只见自己写入时的 ID，不见内部隔离键）；
//   - 空租户（无 ctx 注入）归 DefaultTenantID，单机/开发形态可用。
package aigateway

import (
	"context"
	"testing"

	"github.com/Levango7/OpsMesh/internal/authctx"
	"github.com/Levango7/OpsMesh/internal/store"
)

func tenantCtx(tenant string) context.Context {
	return authctx.NewContext(context.Background(), authctx.Context{TenantID: tenant})
}

func TestInMemoryVectorStore_TenantIsolation(t *testing.T) {
	vs := NewInMemoryVectorStore()
	ctxA := tenantCtx("tenant-a")
	ctxB := tenantCtx("tenant-b")

	// 同 ID、不同租户：互不覆盖
	if err := vs.Upsert(ctxA, []VectorDocument{{ID: "d1", Content: "A 的文档"}}); err != nil {
		t.Fatal(err)
	}
	if err := vs.Upsert(ctxB, []VectorDocument{{ID: "d1", Content: "B 的文档"}}); err != nil {
		t.Fatal(err)
	}

	// A 只检索到自己的
	docsA, err := vs.Search(ctxA, nil, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(docsA) != 1 || docsA[0].Content != "A 的文档" {
		t.Fatalf("A 租户检索结果异常：%+v", docsA)
	}
	// 出口是裸 ID（不是内部隔离键）
	if docsA[0].ID != "d1" {
		t.Fatalf("出口 ID 应还原为裸 ID，得到 %q", docsA[0].ID)
	}
	// B 只检索到自己的
	docsB, _ := vs.Search(ctxB, nil, 10, nil)
	if len(docsB) != 1 || docsB[0].Content != "B 的文档" {
		t.Fatalf("B 租户检索结果异常：%+v", docsB)
	}

	// 跨租户删除无效：A 删不到 B 的 d1
	if err := vs.Delete(ctxA, []string{"d1"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := vs.Count(ctxB); n != 1 {
		t.Fatalf("A 的删除影响了 B 的数据：B Count=%d", n)
	}
	if n, _ := vs.Count(ctxA); n != 0 {
		t.Fatalf("A 自己的删除没生效：A Count=%d", n)
	}

	// 空租户 ctx 归 DefaultTenantID（单机形态可用）
	defCtx := context.Background()
	_ = vs.Upsert(defCtx, []VectorDocument{{ID: "d9", Content: "平台文档"}})
	docsDef, _ := vs.Search(tenantCtx(store.DefaultTenantID), nil, 10, nil)
	if len(docsDef) != 1 || docsDef[0].ID != "d9" {
		t.Fatalf("空租户未归 DefaultTenantID：%+v", docsDef)
	}
	// default 租户的 ctx 与裸 ctx 是同一命名空间
	docsDef2, _ := vs.Search(defCtx, nil, 10, nil)
	if len(docsDef2) != 1 {
		t.Fatalf("裸 ctx 与 default 租户 ctx 应同一空间：%+v", docsDef2)
	}
	// business 租户看不到平台文档
	docsBiz, _ := vs.Search(tenantCtx("tenant-a"), nil, 10, nil)
	for _, d := range docsBiz {
		if d.ID == "d9" {
			t.Fatal("业务租户看到了 default 租户的文档")
		}
	}
}

func TestPromptTemplateStore_TenantIsolation(t *testing.T) {
	ps := NewPromptTemplateStore()
	ctxA := tenantCtx("tenant-a")
	ctxB := tenantCtx("tenant-b")

	if _, err := ps.Create(ctxA, &PromptTemplate{ID: "t1", Content: "A 模板"}); err != nil {
		t.Fatal(err)
	}
	// B 建同 ID 不冲突（此前全局键会报 already exists）
	if _, err := ps.Create(ctxB, &PromptTemplate{ID: "t1", Content: "B 模板"}); err != nil {
		t.Fatalf("跨租户同 ID 被误判冲突：%v", err)
	}
	// 各自 List 只见自己的
	listA := ps.List(ctxA, "")
	if len(listA) != 1 || listA[0].Content != "A 模板" || listA[0].ID != "t1" {
		t.Fatalf("A List 异常：%+v", listA)
	}
	listB := ps.List(ctxB, "")
	if len(listB) != 1 || listB[0].Content != "B 模板" {
		t.Fatalf("B List 异常：%+v", listB)
	}
	// 跨租户 Get 找不到
	if _, ok := ps.Get(ctxB, "t1"); !ok {
		t.Fatal("B 应能 Get 自己的 t1")
	}
	// A 删掉自己的后 B 不受影响
	if !ps.Delete(ctxA, "t1") {
		t.Fatal("A 删除自己的模板失败")
	}
	if len(ps.List(ctxB, "")) != 1 {
		t.Fatal("A 的删除影响了 B")
	}
	// B 删除自己的 t1 返回 true（租户命名空间独立）
	if !ps.Delete(ctxB, "t1") {
		t.Fatal("B 应能删除自己的 t1（租户命名空间独立）")
	}
	if ps.Delete(ctxB, "t1") {
		t.Fatal("B 第二次删除应 false")
	}
}
