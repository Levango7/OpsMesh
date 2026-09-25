// claim_tenant_test.go 覆盖 P0-6 领取侧租户闸（ClaimTask）。
//
// 背景：任务队列按 agent_id 寻址，ClaimTask 无租户入参。下发侧已在控制面收口
// （internal/controlplane/tenant_guard.go），但领取侧仍需独立校验——防止将来新增
// 的下发路径漏校验时，跨租户任务仍被投递到 agent。
//
// 语义：
//   - 任务 TenantID 与 agent TenantID 均非空且不一致 → 不可领取（跳过，继续找后续任务）；
//   - 任务 TenantID 为空（迁移前遗留任务）→ 兼容放行；
//   - agent 不存在 → 无租户可校验，按原行为（不因新增闸而改变既有语义）。
package store

import (
	"testing"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// TestClaimTask_TenantMismatchSkipped 验证跨租户任务不被领取。
func TestClaimTask_TenantMismatchSkipped(t *testing.T) {
	m := NewMemoryStore()
	a := m.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "tenant-a"})

	m.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "tenant-b", Type: "shell", Command: "echo cross"})

	if got := m.ClaimTask(a.AgentID); got != nil {
		t.Fatalf("跨租户任务不应被领取；got=%+v", got)
	}
	// 状态未被翻转（仍可被目标租户的 agent 领取）。
	tasks := m.GetTasks(a.AgentID)
	if len(tasks) == 0 || tasks[0].Status == "running" {
		t.Fatalf("拒绝领取不应翻转状态；got=%+v", tasks)
	}
}

// TestClaimTask_SameTenantClaimed 验证同租户任务正常领取。
func TestClaimTask_SameTenantClaimed(t *testing.T) {
	m := NewMemoryStore()
	a := m.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "tenant-a"})
	tk := m.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "tenant-a", Type: "shell", Command: "echo ok"})

	got := m.ClaimTask(a.AgentID)
	if got == nil || got.TaskID != tk.TaskID {
		t.Fatalf("同租户任务应被领取；got=%+v", got)
	}
}

// TestClaimTask_LegacyEmptyTenantClaimed 验证迁移前遗留任务（TenantID 为空）兼容放行。
func TestClaimTask_LegacyEmptyTenantClaimed(t *testing.T) {
	m := NewMemoryStore()
	a := m.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "tenant-a"})
	tk := m.CreateTask(&proto.Task{AgentID: a.AgentID, Type: "shell", Command: "echo legacy"})

	if got := m.ClaimTask(a.AgentID); got == nil || got.TaskID != tk.TaskID {
		t.Fatalf("空租户遗留任务应兼容领取；got=%+v", got)
	}
}

// TestClaimTask_TenantMismatchSkipsToClaimable 验证跨租户任务只是被跳过，不阻塞后续同租户任务。
// 顺序保证：跨租户任务创建在前，同租户任务在后，领取应拿到后者而非返回 nil。
func TestClaimTask_TenantMismatchSkipsToClaimable(t *testing.T) {
	m := NewMemoryStore()
	a := m.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "tenant-a"})

	m.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "tenant-b", Type: "shell", Command: "echo cross"})
	want := m.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "tenant-a", Type: "shell", Command: "echo own"})

	got := m.ClaimTask(a.AgentID)
	if got == nil || got.TaskID != want.TaskID {
		t.Fatalf("应跳过跨租户任务领取同租户任务；got=%+v want=%s", got, want.TaskID)
	}
}

// TestClaimTask_AgentWithoutTenant 验证 agent 无租户时按原语义放行（不引入新回归）。
func TestClaimTask_AgentWithoutTenant(t *testing.T) {
	m := NewMemoryStore()
	a := m.Register(&proto.AgentInfo{Segment: "seg-a"})
	m.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "tenant-a", Type: "shell", Command: "echo x"})

	if got := m.ClaimTask(a.AgentID); got == nil {
		t.Fatal("agent 无租户时应保持原行为（可领取）")
	}
}
