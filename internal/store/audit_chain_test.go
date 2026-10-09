// audit_chain_test.go 审计链的跨后端行为（内存后端不支持链式校验 + MultiSchema 聚合路由）。
//
// TD-61 批次 3-sql：SQL 侧的纯逻辑（auditEntryHash/verifyChainRows）与真实 MySQL 集成段已随
// sql_audit_chain.go 下沉 internal/store/sqlstore（见该包的 audit_chain_test.go）。
package store

import (
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// ============================================================================
// 内存后端：不支持链式校验 + 保留策略
// ============================================================================

func TestMemoryStore_VerifyAuditChainUnsupported(t *testing.T) {
	m := NewMemoryStore()
	m.Audit(&proto.AuditEvent{TenantID: "t1", Action: "a"})
	res, err := m.VerifyAuditChain("t1", 100)
	if err != nil {
		t.Fatalf("内存后端校验不应报错: %v", err)
	}
	if res == nil || res.Supported {
		t.Fatalf("内存后端必须如实回报 Supported=false；got %+v", res)
	}
	if res.Note == "" {
		t.Fatal("内存后端应给出说明性 Note（避免被误读为「校验通过」）")
	}
}

func TestMemoryStore_ArchiveAuditLog(t *testing.T) {
	m := NewMemoryStore()
	now := time.Now()
	m.Audit(&proto.AuditEvent{TenantID: "t1", Action: "old1", CreatedAt: now.AddDate(0, 0, -40)})
	m.Audit(&proto.AuditEvent{TenantID: "t1", Action: "old2", CreatedAt: now.AddDate(0, 0, -31)})
	m.Audit(&proto.AuditEvent{TenantID: "t1", Action: "fresh", CreatedAt: now.Add(-time.Minute)})

	// retainDays<=0：永久保留，不做任何事。
	if n, err := m.ArchiveAuditLog(0, 100); err != nil || n != 0 {
		t.Fatalf("retainDays=0 应为空操作；got n=%d err=%v", n, err)
	}
	if got := len(m.Audits()); got != 3 {
		t.Fatalf("retainDays=0 不应删除事件；got %d 条", got)
	}
	// retainDays=30：丢弃 40 天前与 31 天前的事件，保留 1 分钟前的。
	n, err := m.ArchiveAuditLog(30, 100)
	if err != nil {
		t.Fatalf("归档报错: %v", err)
	}
	if n != 2 {
		t.Fatalf("应归档 2 条超龄事件；got %d", n)
	}
	rest := m.Audits()
	if len(rest) != 1 || rest[0].Action != "fresh" {
		t.Fatalf("归档后应仅剩 fresh；got %+v", rest)
	}
}
