// sql_abac_decisions_test.go SQLStore 的 M3 决策日志集成测试。
//
// 运行方式与其余 MySQL 集成测试一致：
//
//	OPSMESH_TEST_MYSQL_DSN="user:pass@tcp(127.0.0.1:3306)/opsmesh?parseTime=true" \
//	  go test ./internal/store/sqlstore -run ABACDecision -count=1
//
// 未设置 DSN 时自动跳过（CI 单元任务不依赖 MySQL）。
package sqlstore

import (
	"testing"
	"time"
)

// TestSQLABACDecisionLogs_AppendListDelete 覆盖批量写入、租户过滤、倒序与保留期删除。
func TestSQLABACDecisionLogs_AppendListDelete(t *testing.T) {
	s, cleanup := newTestSQLStore(t)
	defer cleanup()

	base := time.Now().UTC().Truncate(time.Second)
	logs := []ABACDecisionLog{
		{
			TenantID: "tenant-a", AgentID: "agent-1", RequestID: "r-old", BundleID: "b1",
			PolicyID: "p1", Allowed: true, Reason: "match", Mode: "device",
			OccurredAt: base.Add(-3 * time.Hour), ReceivedAt: base.Add(-3 * time.Hour),
		},
		{
			TenantID: "tenant-a", AgentID: "agent-1", RequestID: "r-new", BundleID: "b1",
			PolicyID: "", Allowed: false, Reason: "default deny", Mode: "device-offline",
			OccurredAt: base.Add(-2 * time.Hour), ReceivedAt: base.Add(-2 * time.Hour),
		},
		{
			TenantID: "tenant-b", AgentID: "agent-2", RequestID: "r-b", BundleID: "b2",
			PolicyID: "p2", Allowed: true, Reason: "match", Mode: "device",
			OccurredAt: base.Add(-1 * time.Hour), ReceivedAt: base.Add(-1 * time.Hour),
		},
	}
	n, err := s.AppendABACDecisionLogs(logs)
	if err != nil || n != 3 {
		t.Fatalf("append n=%d err=%v, want 3/nil", n, err)
	}

	a := s.ListABACDecisionLogs("tenant-a", 10)
	if len(a) != 2 {
		t.Fatalf("tenant-a logs=%d, want 2", len(a))
	}
	if a[0].RequestID != "r-new" || a[1].RequestID != "r-old" {
		t.Fatalf("order=%q,%q, want newest first", a[0].RequestID, a[1].RequestID)
	}
	if a[0].Mode != "device-offline" || a[0].Allowed {
		t.Fatalf("r-new row mismatch: %+v", a[0])
	}
	for _, l := range a {
		if l.TenantID != "tenant-a" {
			t.Fatalf("tenant isolation leaked %q", l.TenantID)
		}
	}
	if all := s.ListABACDecisionLogs("", 10); len(all) != 3 {
		t.Fatalf("platform logs=%d, want 3", len(all))
	}

	deleted, err := s.DeleteABACDecisionLogsBefore(base.Add(-90 * time.Minute))
	if err != nil {
		t.Fatalf("delete before: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("deleted=%d, want 2 (r-old + r-new)", deleted)
	}
	if left := s.ListABACDecisionLogs("", 10); len(left) != 1 || left[0].RequestID != "r-b" {
		t.Fatalf("left=%+v, want only r-b", left)
	}
}
