// retrieval_tenant_test.go — TD-97 ② knowledge 共享存储租户隔离契约测试。
package knowledge

import (
	"context"
	"testing"

	"github.com/Levango7/OpsMesh/internal/authctx"
	"github.com/Levango7/OpsMesh/internal/store"
)

func ktCtx(tenant string) context.Context {
	return authctx.NewContext(context.Background(), authctx.Context{TenantID: tenant})
}

func TestInMemoryKnowledgeStore_TenantIsolation(t *testing.T) {
	ks := NewInMemoryKnowledgeStore()
	ctxA := ktCtx("tenant-a")
	ctxB := ktCtx("tenant-b")

	// 同 ID 不同租户互不覆盖
	if err := ks.Upsert(ctxA, []KnowledgeDoc{{ID: "k1", Content: "A 的知识"}}); err != nil {
		t.Fatal(err)
	}
	if err := ks.Upsert(ctxB, []KnowledgeDoc{{ID: "k1", Content: "B 的知识"}}); err != nil {
		t.Fatal(err)
	}

	docsA, err := ks.Search(ctxA, "", 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(docsA) != 1 || docsA[0].Content != "A 的知识" || docsA[0].ID != "k1" {
		t.Fatalf("A 检索异常：%+v", docsA)
	}
	docsB, _ := ks.Search(ctxB, "", 10, nil)
	if len(docsB) != 1 || docsB[0].Content != "B 的知识" {
		t.Fatalf("B 检索异常：%+v", docsB)
	}

	// Delete 按租户收窄
	if err := ks.Delete(ctxA, []string{"k1"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := ks.Count(ctxB); n != 1 {
		t.Fatalf("A 的删除影响了 B：B Count=%d", n)
	}
	// ListBySource 同口径（B 的还在）
	listB, _ := ks.ListBySource(ctxB, KnowledgeSource(""), 0)
	for _, d := range listB {
		if d.ID != "k1" {
			t.Fatalf("B ListBySource 出现非本租户文档：%+v", d)
		}
	}
	// Count 只数本租户
	if n, _ := ks.Count(ctxA); n != 0 {
		t.Fatalf("A Count 应为 0，得到 %d", n)
	}
	// 空租户归 default
	def := ktCtx(store.DefaultTenantID)
	_ = ks.Upsert(def, []KnowledgeDoc{{ID: "p1", Content: "平台知识"}})
	docsDef, _ := ks.Search(def, "", 10, nil)
	if len(docsDef) != 1 {
		t.Fatalf("default 租户检索异常：%+v", docsDef)
	}
	// 业务租户看不到平台知识
	docsBiz, _ := ks.Search(ctxB, "", 10, nil)
	for _, d := range docsBiz {
		if d.ID == "p1" {
			t.Fatal("业务租户看到了 default 租户的知识文档")
		}
	}
}
