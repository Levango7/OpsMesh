// memory_abac_decision_logs_test.go 验证内存后端的 M3 决策日志语义：
// 追加/查询倒序、租户隔离、保留期删除与容量上限（单租户 + 全局）。
package memory

import (
	"fmt"
	"testing"
	"time"
)

// TestMemoryABACDecisionLogs_AppendListTenantIsolation 覆盖：租户过滤 + 最新在前 + limit。
func TestMemoryABACDecisionLogs_AppendListTenantIsolation(t *testing.T) {
	m := NewMemoryStore()
	base := time.Now().UTC().Truncate(time.Second)
	logs := []ABACDecisionLog{
		{TenantID: "t1", AgentID: "a1", RequestID: "r1", Mode: "device", ReceivedAt: base.Add(-3 * time.Hour)},
		{TenantID: "t2", AgentID: "a2", RequestID: "r2", Mode: "device", ReceivedAt: base.Add(-2 * time.Hour)},
		{TenantID: "t1", AgentID: "a1", RequestID: "r3", Mode: "device-offline", ReceivedAt: base.Add(-1 * time.Hour)},
	}
	n, err := m.AppendABACDecisionLogs(logs)
	if err != nil || n != 3 {
		t.Fatalf("append n=%d err=%v, want 3/nil", n, err)
	}

	t1 := m.ListABACDecisionLogs("t1", 10)
	if len(t1) != 2 {
		t.Fatalf("t1 logs=%d, want 2", len(t1))
	}
	if t1[0].RequestID != "r3" || t1[1].RequestID != "r1" {
		t.Fatalf("t1 order=%q,%q, want r3,r1 (newest first)", t1[0].RequestID, t1[1].RequestID)
	}
	for _, l := range t1 {
		if l.TenantID != "t1" {
			t.Fatalf("tenant isolation leaked %q", l.TenantID)
		}
	}
	if got := m.ListABACDecisionLogs("t1", 1); len(got) != 1 || got[0].RequestID != "r3" {
		t.Fatalf("limit=1 got %+v", got)
	}
	if all := m.ListABACDecisionLogs("", 2); len(all) != 2 || all[0].RequestID != "r3" || all[1].RequestID != "r2" {
		t.Fatalf("platform top2=%+v", all)
	}
}

// TestMemoryABACDecisionLogs_DeleteBefore 覆盖保留期删除：仅删 ReceivedAt 早于 cutoff。
func TestMemoryABACDecisionLogs_DeleteBefore(t *testing.T) {
	m := NewMemoryStore()
	now := time.Now().UTC()
	logs := []ABACDecisionLog{
		{TenantID: "t1", AgentID: "a1", RequestID: "old", ReceivedAt: now.Add(-40 * 24 * time.Hour)},
		{TenantID: "t1", AgentID: "a1", RequestID: "edge", ReceivedAt: now.Add(-30 * 24 * time.Hour)},
		{TenantID: "t2", AgentID: "a2", RequestID: "new", ReceivedAt: now.Add(-1 * time.Hour)},
	}
	if _, err := m.AppendABACDecisionLogs(logs); err != nil {
		t.Fatalf("append: %v", err)
	}
	deleted, err := m.DeleteABACDecisionLogsBefore(now.Add(-30 * 24 * time.Hour))
	if err != nil {
		t.Fatalf("delete before: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted=%d, want 1 (strictly before cutoff)", deleted)
	}
	left := m.ListABACDecisionLogs("", 10)
	if len(left) != 2 {
		t.Fatalf("left=%d, want 2", len(left))
	}
	for _, l := range left {
		if l.RequestID == "old" {
			t.Fatal("old log survived retention delete")
		}
	}
	if n, err := m.DeleteABACDecisionLogsBefore(now); err != nil || n != 2 {
		t.Fatalf("second delete n=%d err=%v, want 2/nil", n, err)
	}
}

// TestMemoryABACDecisionLogs_PerTenantCap 覆盖单租户上限：超限丢弃最旧记录。
func TestMemoryABACDecisionLogs_PerTenantCap(t *testing.T) {
	m := NewMemoryStore()
	logs := make([]ABACDecisionLog, 0, ABACDecisionLogsPerTenantCap+1)
	base := time.Now().UTC()
	for i := 0; i < ABACDecisionLogsPerTenantCap+1; i++ {
		logs = append(logs, ABACDecisionLog{
			TenantID:   "t1",
			AgentID:    "a1",
			RequestID:  fmt.Sprintf("r-%d", i),
			Mode:       "device",
			ReceivedAt: base.Add(time.Duration(i) * time.Millisecond),
		})
	}
	n, err := m.AppendABACDecisionLogs(logs)
	if err != nil || n != len(logs) {
		t.Fatalf("append n=%d err=%v, want %d/nil", n, err, len(logs))
	}
	m.mu.RLock()
	stored := len(m.abacDecisionLogs)
	oldestID := int64(-1)
	if stored > 0 {
		oldestID = m.abacDecisionLogs[0].ID
	}
	m.mu.RUnlock()
	if stored != ABACDecisionLogsPerTenantCap {
		t.Fatalf("stored=%d, want %d", stored, ABACDecisionLogsPerTenantCap)
	}
	if oldestID != 1 {
		t.Fatalf("oldest retained ID=%d, want 1 (ID 0 evicted)", oldestID)
	}
}

// TestMemoryABACDecisionLogs_GlobalCap 覆盖全局上限：跨租户超限时丢最旧。
func TestMemoryABACDecisionLogs_GlobalCap(t *testing.T) {
	m := NewMemoryStore()
	const batch = 9000
	base := time.Now().UTC()
	total := 0
	for tenant := 0; tenant < 6; tenant++ {
		logs := make([]ABACDecisionLog, 0, batch)
		for i := 0; i < batch; i++ {
			logs = append(logs, ABACDecisionLog{
				TenantID:   fmt.Sprintf("t%d", tenant),
				AgentID:    "a1",
				Mode:       "device",
				ReceivedAt: base.Add(time.Duration(total) * time.Millisecond),
			})
			total++
		}
		if _, err := m.AppendABACDecisionLogs(logs); err != nil {
			t.Fatalf("append tenant %d: %v", tenant, err)
		}
	}
	m.mu.RLock()
	stored := len(m.abacDecisionLogs)
	oldestID := int64(-1)
	if stored > 0 {
		oldestID = m.abacDecisionLogs[0].ID
	}
	m.mu.RUnlock()
	if stored != ABACDecisionLogsGlobalCap {
		t.Fatalf("stored=%d, want %d", stored, ABACDecisionLogsGlobalCap)
	}
	if oldestID != int64(total-ABACDecisionLogsGlobalCap) {
		t.Fatalf("oldest retained ID=%d, want %d", oldestID, total-ABACDecisionLogsGlobalCap)
	}
	if got := m.ListABACDecisionLogs("", 0); len(got) != ABACDecisionLogsListCap {
		t.Fatalf("list cap=%d, want %d", len(got), ABACDecisionLogsListCap)
	}
}
