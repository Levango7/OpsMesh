package store

import (
	"sync"
	"testing"
	"time"
)

func TestMemoryStoreAgentLogs(t *testing.T) {
	m := NewMemoryStore()
	if err := m.AppendAgentLog(nil); err != nil {
		t.Fatalf("AppendAgentLog(nil) = %v, want nil", err)
	}

	now := time.Now()
	e1 := &LogEntry{TenantID: "t1", AgentID: "a1", Level: "info", Message: "first", Timestamp: now}
	e2 := &LogEntry{TenantID: "t1", AgentID: "a1", Level: "error", Message: "second"}
	e3 := &LogEntry{TenantID: "t2", AgentID: "a2", Level: "info", Message: "third", Timestamp: now.Add(time.Second)}
	for _, e := range []*LogEntry{e1, e2, e3} {
		if err := m.AppendAgentLog(e); err != nil {
			t.Fatalf("AppendAgentLog: %v", err)
		}
	}
	if e2.Timestamp.IsZero() {
		t.Fatal("AppendAgentLog 未补零值时间戳")
	}

	// 租户过滤，且结果按时间正序。
	got := m.ListAgentLogs("t1", "", "", 0)
	if len(got) != 2 || got[0].Message != "first" || got[1].Message != "second" {
		t.Fatalf("ListAgentLogs(t1) = %v, want [first second]", messages(got))
	}
	// level / agent 过滤。
	if got := m.ListAgentLogs("t1", "", "error", 0); len(got) != 1 || got[0].Message != "second" {
		t.Fatalf("ListAgentLogs(level=error) = %v, want [second]", messages(got))
	}
	if got := m.ListAgentLogs("", "a2", "", 0); len(got) != 1 || got[0].Message != "third" {
		t.Fatalf("ListAgentLogs(agent=a2) = %v, want [third]", messages(got))
	}
	// limit 取最新 N 条，窗口内仍按时间正序。
	if got := m.ListAgentLogs("", "", "", 2); len(got) != 2 || got[0].Message != "second" || got[1].Message != "third" {
		t.Fatalf("ListAgentLogs(limit=2) = %v, want [second third]", messages(got))
	}
}

func TestMemoryStoreAuditLogs(t *testing.T) {
	m := NewMemoryStore()
	if err := m.AppendAuditLog(nil); err != nil {
		t.Fatalf("AppendAuditLog(nil) = %v, want nil", err)
	}

	a1 := &AuditLog{TenantID: "t1", UserID: "u1", Action: "create", Target: "dev-1"}
	a2 := &AuditLog{TenantID: "t2", UserID: "u2", Action: "login", Timestamp: time.Now()}
	for _, a := range []*AuditLog{a1, a2} {
		if err := m.AppendAuditLog(a); err != nil {
			t.Fatalf("AppendAuditLog: %v", err)
		}
	}
	if a1.Timestamp.IsZero() {
		t.Fatal("AppendAuditLog 未补零值时间戳")
	}

	if got := m.ListAuditLogs("t1", 0); len(got) != 1 || got[0].Action != "create" {
		t.Fatalf("ListAuditLogs(t1) = %v, want [create]", actions(got))
	}
	if got := m.ListAuditLogs("", 1); len(got) != 1 || got[0].Action != "login" {
		t.Fatalf("ListAuditLogs(limit=1) = %v, want 最新一条 [login]", actions(got))
	}
}

// 并发追加与读取不得触发竞态（services job 以 -race 执行本包）。
func TestMemoryStoreConcurrentAppend(t *testing.T) {
	m := NewMemoryStore()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); _ = m.AppendAgentLog(&LogEntry{TenantID: "t1", Message: "m"}) }()
		go func() { defer wg.Done(); _ = m.ListAgentLogs("t1", "", "", 10) }()
	}
	wg.Wait()
	if n := len(m.ListAgentLogs("t1", "", "", 0)); n != 20 {
		t.Fatalf("并发追加后条数 = %d, want 20", n)
	}
}

func messages(entries []*LogEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Message)
	}
	return out
}

func actions(entries []*AuditLog) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Action)
	}
	return out
}
