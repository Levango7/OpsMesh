// sql_tasks_mysql_test.go — tasks 读路径的真库往返守卫（DSN 门控）。
//
// 为什么要有它：TD-90 的「SELECT 列数 ↔ Scan 目标数」对账门禁
// （scan_arity_guard_test.go）上线即抓出真缺陷——`GetTasks` 的 SELECT 漏了
// `tenant_id`（8 列）而 `scanTaskListRow` 扫 9 个目标 ⇒ `rows.Scan` 必报
// `expected 8 destination arguments in Scan, not 9` ⇒ **该函数在 MySQL 后端恒返回空列表**
// （内存后端正确，故单测全绿）。此前该函数只有「DB 不可达 ⇒ 返回 nil」一条用例，
// 正常路径没有真库执行方 ⇒ 与 k8s 那次（SELECT 7 列 / Scan 8 目标）同族、同样潜伏。
//
// 本文件把 `scanTaskListRow` 的三个调用方（GetTasks / AllTasks / TaskByID）都用真库往返钉住：
// 它们共享同一个 helper，任一处 SELECT 少列都会让读侧静默变空。
package sqlstore

import (
	"os"
	"testing"

	"github.com/Levango7/OpsMesh/internal/proto"
)

func TestTasksReadPaths_MySQL(t *testing.T) {
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPSMESH_TEST_MYSQL_DSN not set; skipping tasks read-path integration test")
	}
	s, err := NewSQLStore(dsn, "", "")
	if err != nil {
		t.Fatalf("NewSQLStore: %v", err)
	}
	defer s.db.Close()

	const agentID = "agent-arity-mysql"
	const tenantID = "tenant-arity"
	cleanup := func() {
		_, _ = s.db.Exec("DELETE FROM tasks WHERE agent_id=?", agentID)
	}
	cleanup()
	defer cleanup()

	// 写入两类任务：pending（GetTasks 应返回）与 done（GetTasks 不应返回）。
	created := s.CreateTask(&proto.Task{
		TaskID: "task-arity-1", AgentID: agentID, TenantID: tenantID,
		Type: "shell", Command: "echo hi", Path: "/tmp", Status: "pending",
	})
	if created == nil || created.TaskID != "task-arity-1" {
		t.Fatalf("CreateTask 应返回已写任务；got=%+v", created)
	}
	s.CreateTask(&proto.Task{
		TaskID: "task-arity-2", AgentID: agentID, TenantID: tenantID,
		Type: "shell", Command: "echo done", Status: "done",
	})

	// 1. GetTasks（修前：SELECT 8 列 vs 9 目标 ⇒ 恒返回空;修后应返回 pending 的那条）。
	tasks := s.GetTasks(agentID)
	if len(tasks) != 1 {
		t.Fatalf("GetTasks 应返回 1 条 pending 任务；got=%d（0 条即列数错位缺陷复发形态）", len(tasks))
	}
	if tasks[0].TaskID != "task-arity-1" {
		t.Errorf("GetTasks 返回 %s, want task-arity-1", tasks[0].TaskID)
	}
	// 关键断言：tenant_id 是 SELECT 与 Scan 错位时首当其冲的列——它必须读得回来。
	if tasks[0].TenantID != tenantID {
		t.Errorf("GetTasks 的 TenantID = %q, want %q（tenant_id 列缺失/错位的直接症状）", tasks[0].TenantID, tenantID)
	}
	if tasks[0].Command != "echo hi" || tasks[0].Status != "pending" {
		t.Errorf("GetTasks 字段错位: Command=%q Status=%q", tasks[0].Command, tasks[0].Status)
	}

	// 2. AllTasks（同一 helper 的第二个调用方）。
	var found bool
	for _, tk := range s.AllTasks(tenantID) {
		if tk.TaskID == "task-arity-1" {
			found = true
			if tk.TenantID != tenantID {
				t.Errorf("AllTasks 的 TenantID = %q, want %q", tk.TenantID, tenantID)
			}
		}
	}
	if !found {
		t.Error("AllTasks(tenant) 未返回刚写入的任务——列数错位的另一处症状")
	}

	// 3. TaskByID（第三个调用方）。
	one := s.TaskByID("task-arity-1")
	if one == nil {
		t.Fatal("TaskByID 应返回已写入的任务")
	}
	if one.TenantID != tenantID || one.Command != "echo hi" {
		t.Errorf("TaskByID 字段错位: TenantID=%q Command=%q", one.TenantID, one.Command)
	}
}
