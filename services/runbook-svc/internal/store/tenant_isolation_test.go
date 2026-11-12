package store

import (
	"testing"

	"github.com/Levango7/OpsMesh/services/runbook-svc/internal/models"
)

// TestMemoryStoreTenantIsolation 是 D4（issue #27 裁决②）在 runbook 存储层的钉子：
// 空租户拒写、跨租户读/列/改/删/执行历史一律不可见、执行记录归属由父 runbook 决定、
// 同租户一切正常。任一条被改弱都会在这里红。
func TestMemoryStoreTenantIsolation(t *testing.T) {
	m := NewMemoryStore()

	// 空租户拒绝写入
	if got := m.CreateRunbook(&models.Runbook{ID: "no-tenant", Name: "x"}); got != nil {
		t.Fatal("无租户写入应被拒绝")
	}

	a := m.CreateRunbook(&models.Runbook{TenantID: "A", ID: "rb-1", Name: "A 的手册", Enabled: true})
	if a == nil {
		t.Fatal("A 建 runbook 失败")
	}
	m.AddExecution(&models.ExecutionRecord{ID: "e-a", RunbookID: "rb-1", TenantID: "A", Status: "success"})

	// B 视角：读/列/改/删/执行历史全部不可见或不可写
	if got := m.GetRunbook("rb-1", "B"); got != nil {
		t.Fatalf("B 读到了 A 的 runbook: %+v", got)
	}
	if list := m.ListRunbooks("B"); len(list) != 0 {
		t.Fatalf("B 列表看到 %d 条（应 0）", len(list))
	}
	if m.UpdateRunbook(&models.Runbook{TenantID: "B", ID: "rb-1", Name: "hijack"}, "B") {
		t.Fatal("B 改到了 A 的 runbook")
	}
	if m.DeleteRunbook("rb-1", "B") {
		t.Fatal("B 删掉了 A 的 runbook")
	}
	if hist := m.GetExecutions("rb-1", "B"); len(hist) != 0 {
		t.Fatalf("B 看到了 A 的执行历史: %+v", hist)
	}
	// B 不能把执行记录挂到 A 的 runbook 上（归属由父单决定）
	m.AddExecution(&models.ExecutionRecord{ID: "e-b", RunbookID: "rb-1", TenantID: "B", Status: "success"})
	if hist := m.GetExecutions("rb-1", "A"); len(hist) != 1 {
		t.Fatalf("A 的执行历史应为 1 条（B 的伪造记录不该落进来）, got %d", len(hist))
	}

	// A 自己一切正常
	if got := m.GetRunbook("rb-1", "A"); got == nil || got.TenantID != "A" {
		t.Fatal("A 自己的 runbook 读不到 / 租户错")
	}
	if list := m.ListRunbooks("A"); len(list) != 1 {
		t.Fatalf("A 列表应为 1 条, got %d", len(list))
	}
	if !m.UpdateRunbook(&models.Runbook{TenantID: "A", ID: "rb-1", Name: "A 改名"}, "A") {
		t.Fatal("A 改不动自己的 runbook")
	}
	if !m.DeleteRunbook("rb-1", "A") {
		t.Fatal("A 删不掉自己的 runbook")
	}
}
