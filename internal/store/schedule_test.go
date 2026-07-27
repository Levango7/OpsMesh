package store

import (
	"testing"
	"time"

	"opsmesh/internal/proto"
)

// TestMemory_FireDueSchedules 验证 F4 定时/周期调度：模板任务（ParentID 空 + Schedule 非空）
// 到点派生 pending 实例（ParentID=模板、Schedule 清空），同分钟不重复派生，且模板不可被直接领取。
func TestMemory_FireDueSchedules(t *testing.T) {
	st := NewMemoryStore().WithDemo(false)
	tpl := st.CreateTask(&proto.Task{
		AgentID: "a1", TenantID: "t1", Type: "shell", Command: "echo hi",
		Schedule: "* * * * *", // 每分钟
	})
	if tpl.ParentID != "" || tpl.Schedule == "" {
		t.Fatal("模板任务未正确构造（ParentID 应空、Schedule 应非空）")
	}

	// 首次到点：派生 1 个实例
	if n := st.FireDueSchedules(time.Now()); n != 1 {
		t.Fatalf("首次 fire = %d, want 1", n)
	}
	// 同分钟再次：不应重复派生（LastFiredAt 守卫）
	if n := st.FireDueSchedules(time.Now()); n != 0 {
		t.Fatalf("同分钟二次 fire = %d, want 0", n)
	}

	// 找到派生实例
	var inst *proto.Task
	for _, tk := range st.AllTasks("t1") {
		if tk.ParentID == tpl.TaskID {
			inst = tk
		}
	}
	if inst == nil {
		t.Fatal("未找到派生实例")
	}
	if inst.Status != "pending" || inst.Schedule != "" || inst.ParentID != tpl.TaskID {
		t.Fatalf("实例字段异常: %+v", inst)
	}

	// 模板不可被直接领取（防止误执行模板本体）
	claimed := st.ClaimTask("a1")
	if claimed != nil && claimed.TaskID == tpl.TaskID {
		t.Fatal("模板任务被 ClaimTask 误领取")
	}
	if claimed == nil || claimed.TaskID != inst.TaskID {
		t.Fatalf("应领取到派生实例，got %+v", claimed)
	}

	// 周期性：跨分钟边界后再次到点应再派生 1 个新实例（与上一实例 TaskID 不同）。
	nextMin := time.Now().Add(61 * time.Second)
	if n := st.FireDueSchedules(nextMin); n != 1 {
		t.Fatalf("下一分钟 fire = %d, want 1（周期性派生）", n)
	}
	inst2 := st.FireDueSchedules(nextMin) // 同分钟再次守卫
	if inst2 != 0 {
		t.Fatalf("同分钟重复 fire = %d, want 0", inst2)
	}
	var count int
	for _, tk := range st.AllTasks("t1") {
		if tk.ParentID == tpl.TaskID {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("派生实例总数 = %d, want 2（每分钟 1 个）", count)
	}
}
