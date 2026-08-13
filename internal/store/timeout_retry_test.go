// timeout_retry_test.go 验证 P2-B2 节点级超时与重试（任务 261）的 store 层透传：
//   - CreateTask 写入的 Timeout/RetryDelay/MaxRetries 经 ClaimTask 原样返回给 agent；
//   - FireDueSchedules 派生实例继承模板的 Timeout/RetryDelay/MaxRetries。
//
// 仅覆盖 MemoryStore（SQLStore 走真实 MySQL，由 integration/migration 测试覆盖）。
package store

import (
	"testing"
	"time"

	"opsmesh/internal/proto"
)

// TestMemoryStore_TaskTimeoutRetryPassthrough 验证 CreateTask 设置的 Timeout/RetryDelay/MaxRetries
// 经 ClaimTask 原样返回（agent 端据此应用节点级超时）。
func TestMemoryStore_TaskTimeoutRetryPassthrough(t *testing.T) {
	st := NewMemoryStore().WithDemo(false)
	st.Register(&proto.AgentInfo{AgentID: "a1", TenantID: "t1", Status: "online"})

	tk := st.CreateTask(&proto.Task{
		AgentID: "a1", TenantID: "t1", Type: "shell", Command: "echo hi",
		Timeout: 30, MaxRetries: 2, RetryDelay: 5,
	})
	if tk.Timeout != 30 || tk.MaxRetries != 2 || tk.RetryDelay != 5 {
		t.Fatalf("CreateTask 返回字段异常: timeout=%d maxRetries=%d retryDelay=%d",
			tk.Timeout, tk.MaxRetries, tk.RetryDelay)
	}

	claimed := st.ClaimTask("a1")
	if claimed == nil || claimed.TaskID != tk.TaskID {
		t.Fatalf("ClaimTask 未领到刚下发的任务: %+v", claimed)
	}
	if claimed.Timeout != 30 {
		t.Errorf("ClaimTask timeout=%d, want 30", claimed.Timeout)
	}
	if claimed.MaxRetries != 2 {
		t.Errorf("ClaimTask maxRetries=%d, want 2", claimed.MaxRetries)
	}
	if claimed.RetryDelay != 5 {
		t.Errorf("ClaimTask retryDelay=%d, want 5", claimed.RetryDelay)
	}
}

// TestMemoryStore_TaskTimeoutZeroBackcompat 验证未设节点级超时（Timeout=0）时向后兼容：
// ClaimTask 返回 Timeout=0，agent 端回退全局 taskTimeout。
func TestMemoryStore_TaskTimeoutZeroBackcompat(t *testing.T) {
	st := NewMemoryStore().WithDemo(false)
	st.Register(&proto.AgentInfo{AgentID: "a1", TenantID: "t1", Status: "online"})

	st.CreateTask(&proto.Task{
		AgentID: "a1", TenantID: "t1", Type: "shell", Command: "echo hi",
		// 不设 Timeout/MaxRetries/RetryDelay
	})

	claimed := st.ClaimTask("a1")
	if claimed == nil {
		t.Fatal("ClaimTask = nil")
	}
	if claimed.Timeout != 0 || claimed.MaxRetries != 0 || claimed.RetryDelay != 0 {
		t.Errorf("零值回退失败: timeout=%d maxRetries=%d retryDelay=%d, want 全 0",
			claimed.Timeout, claimed.MaxRetries, claimed.RetryDelay)
	}
}

// TestMemoryStore_FireDueSchedulesInheritsTimeoutRetry 验证 FireDueSchedules 派生实例
// 继承模板任务的 Timeout/RetryDelay/MaxRetries（P2-B2 节点级超时与重试）。
func TestMemoryStore_FireDueSchedulesInheritsTimeoutRetry(t *testing.T) {
	st := NewMemoryStore().WithDemo(false)
	tpl := st.CreateTask(&proto.Task{
		AgentID: "a1", TenantID: "t1", Type: "shell", Command: "echo hi",
		Schedule:   "* * * * *", // 每分钟
		Timeout:    42,
		MaxRetries: 3,
		RetryDelay: 7,
	})
	if tpl.Timeout != 42 || tpl.MaxRetries != 3 || tpl.RetryDelay != 7 {
		t.Fatalf("模板字段异常: timeout=%d maxRetries=%d retryDelay=%d",
			tpl.Timeout, tpl.MaxRetries, tpl.RetryDelay)
	}

	if n := st.FireDueSchedules(time.Now()); n != 1 {
		t.Fatalf("首次 fire = %d, want 1", n)
	}

	// 找到派生实例并校验继承的字段。
	var inst *proto.Task
	for _, tk := range st.AllTasks("t1") {
		if tk.ParentID == tpl.TaskID {
			inst = tk
		}
	}
	if inst == nil {
		t.Fatal("未找到派生实例")
	}
	if inst.Timeout != 42 {
		t.Errorf("派生实例 timeout=%d, want 42（应继承模板）", inst.Timeout)
	}
	if inst.MaxRetries != 3 {
		t.Errorf("派生实例 maxRetries=%d, want 3（应继承模板）", inst.MaxRetries)
	}
	if inst.RetryDelay != 7 {
		t.Errorf("派生实例 retryDelay=%d, want 7（应继承模板）", inst.RetryDelay)
	}
}
