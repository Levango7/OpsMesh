package store

import (
	"os"
	"testing"

	"opsmesh/internal/proto"
)

// TestSQLStore_TenantIsolation 需要真实 MySQL + Redis，默认跳过。
// 运行：
//   OPSMESH_TEST_MYSQL_DSN="user:pass@tcp(127.0.0.1:3306)/opsmesh?parseTime=true" \
//   OPSMESH_TEST_REDIS_ADDR=127.0.0.1:6379 \
//   go test ./internal/store/ -run TestSQLStore -v
//
// 验证 U-04 数据本地化下，SQLStore 的租户隔离与 MemoryStore 行为一致。
func TestSQLStore_TenantIsolation(t *testing.T) {
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPSMESH_TEST_MYSQL_DSN not set; skipping SQL store test")
	}
	redisAddr := os.Getenv("OPSMESH_TEST_REDIS_ADDR")
	s, err := NewSQLStore(dsn, redisAddr)
	if err != nil {
		t.Fatalf("NewSQLStore: %v", err)
	}

	s.Register(&proto.AgentInfo{AgentID: "sql-a1", Segment: "seg-a", TenantID: "t1"})
	s.Register(&proto.AgentInfo{AgentID: "sql-a2", Segment: "seg-a", TenantID: "t2"})

	if got := s.Agents("t1"); len(got) != 1 || got[0].AgentID != "sql-a1" {
		t.Fatalf("SQL Agents(t1) = %+v, want exactly sql-a1", got)
	}
	if got := s.Agents("t2"); len(got) != 1 || got[0].AgentID != "sql-a2" {
		t.Fatalf("SQL Agents(t2) = %+v, want exactly sql-a2", got)
	}
	if n := countDevices(s.Snapshot("t1")); n != 1 {
		t.Fatalf("SQL Snapshot(t1) device count = %d, want 1", n)
	}
}
