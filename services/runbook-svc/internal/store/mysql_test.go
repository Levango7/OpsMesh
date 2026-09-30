package store

import (
	"os"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/services/runbook-svc/internal/models"
)

// newTestMySQLStore 打开测试用 MySQL（OPSMESH_TEST_MYSQL_DSN 门控，与兄弟服务
// 同一约定；services job 的 MySQL 容器使该测试在 CI 真跑）。
func newTestMySQLStore(t *testing.T) *MySQLStore {
	t.Helper()
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPSMESH_TEST_MYSQL_DSN not set; skipping runbook MySQL integration test")
	}
	m, err := NewMySQLStore(dsn)
	if err != nil {
		t.Fatalf("NewMySQLStore: %v", err)
	}
	return m
}

// TestMySQLStore_RunbookCRUD 全生命周期：create → get → list → update → delete。
func TestMySQLStore_RunbookCRUD(t *testing.T) {
	m := newTestMySQLStore(t)

	r := &models.Runbook{
		ID:          "rb-itest-1",
		Name:        "集成测试 Runbook",
		Description: "CRUD roundtrip",
		Content:     "echo hello",
		Triggers:    []models.TriggerCondition{{EventType: "alert", Filters: map[string]string{"severity": "high"}}},
		Steps:       []models.Step{{Name: "step-1", Action: "shell", Command: "echo hi", Timeout: 30 * time.Second, OnError: "stop"}},
		Enabled:     true,
	}
	if created := m.CreateRunbook(r); created == nil {
		t.Fatal("CreateRunbook 不应返回 nil")
	}

	got := m.GetRunbook(r.ID)
	if got == nil {
		t.Fatal("GetRunbook 应取回刚创建的 runbook")
	}
	if got.Name != r.Name || len(got.Triggers) != 1 || len(got.Steps) != 1 || !got.Enabled {
		t.Fatalf("roundtrip 字段不一致: %+v", got)
	}
	if got.Steps[0].Timeout != 30*time.Second {
		t.Errorf("Steps[0].Timeout = %v，期望 30s（JSON roundtrip）", got.Steps[0].Timeout)
	}

	got.Name = "改名后"
	if !m.UpdateRunbook(got) {
		t.Fatal("UpdateRunbook 应成功")
	}
	if m.GetRunbook(r.ID).Name != "改名后" {
		t.Fatal("更新未持久化")
	}

	list := m.ListRunbooks()
	found := false
	for _, rb := range list {
		if rb.ID == r.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("ListRunbooks 应包含该 runbook")
	}

	if !m.DeleteRunbook(r.ID) {
		t.Fatal("DeleteRunbook 应成功")
	}
	if m.GetRunbook(r.ID) != nil {
		t.Fatal("删除后 GetRunbook 应为 nil")
	}
	if m.UpdateRunbook(got) {
		t.Fatal("删除后 Update 应失败（not found）")
	}
}

// TestMySQLStore_Executions 执行历史：Add → Get，且随 runbook 删除一并清理。
func TestMySQLStore_Executions(t *testing.T) {
	m := newTestMySQLStore(t)

	r := &models.Runbook{ID: "rb-itest-exec", Name: "exec 测试"}
	m.CreateRunbook(r)

	e := &models.ExecutionRecord{
		ID:          "exec-itest-1",
		RunbookID:   r.ID,
		TriggeredBy: "itest",
		Status:      "success",
		StepResults: []models.StepResult{{StepName: "step-1", Status: "success"}},
		CompletedAt: time.Now(),
	}
	m.AddExecution(e)

	got := m.GetExecutions(r.ID)
	if len(got) != 1 {
		t.Fatalf("期望 1 条执行记录，实际 %d", len(got))
	}
	if got[0].ID != e.ID || got[0].Status != "success" || len(got[0].StepResults) != 1 {
		t.Fatalf("执行记录 roundtrip 不一致: %+v", got[0])
	}

	m.DeleteRunbook(r.ID)
	if remain := m.GetExecutions(r.ID); len(remain) != 0 {
		t.Fatalf("runbook 删除后执行历史应一并清理，剩 %d 条", len(remain))
	}
}
