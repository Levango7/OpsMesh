// audit_chain_multischema_test.go 多租户 schema 隔离下的审计链校验/归档聚合与路由
// （TD-61 末批：自父包 audit_chain_test.go 尾段拆分）。
package multischema

import (
	"strings"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// ============================================================================
// 多租户 schema 隔离：审计链校验/归档的聚合与路由
// ============================================================================

// TestMultiSchemaStore_VerifyAuditChain_UnsupportedAggregate 验证平台级（tenant 为空）
// 询所有 schema 聚合：内存 schema 如实回报不支持，且不被当成「校验通过」。
func TestMultiSchemaStore_VerifyAuditChain_UnsupportedAggregate(t *testing.T) {
	m, _ := newTestMultiSchema()
	m.Audit(&proto.AuditEvent{TenantID: "tA", Action: "a"})
	m.Audit(&proto.AuditEvent{TenantID: "tB", Action: "b"})

	res, err := m.VerifyAuditChain("", 100)
	if err != nil {
		t.Fatalf("聚合校验不应报错: %v", err)
	}
	if res == nil || res.Supported || res.OK {
		t.Fatalf("全内存 schema 应回报 Supported=false / OK=false；got %+v", res)
	}
	if !strings.Contains(res.Note, "不提供链式校验") {
		t.Fatalf("Note 应说明不支持；got %q", res.Note)
	}
	// 租户路由：tA 的校验路由到 tA 的 schema（内存 → 同样不支持，但 Note 来自该 schema）。
	resA, err := m.VerifyAuditChain("tA", 100)
	if err != nil {
		t.Fatalf("租户校验不应报错: %v", err)
	}
	if resA.Supported {
		t.Fatalf("内存 schema 不应回报支持；got %+v", resA)
	}
}

// TestMultiSchemaStore_ArchiveAuditLog 验证归档逐 schema 转发（内存 schema 只保留在保留期内的事件）。
func TestMultiSchemaStore_ArchiveAuditLog(t *testing.T) {
	m, _ := newTestMultiSchema()
	now := time.Now()
	m.Audit(&proto.AuditEvent{TenantID: "tA", Action: "old", CreatedAt: now.AddDate(0, 0, -40)})
	m.Audit(&proto.AuditEvent{TenantID: "tA", Action: "fresh", CreatedAt: now})
	m.Audit(&proto.AuditEvent{TenantID: "tB", Action: "old", CreatedAt: now.AddDate(0, 0, -40)})

	if n, err := m.ArchiveAuditLog(0, 100); err != nil || n != 0 {
		t.Fatalf("retainDays=0 应为空操作；got n=%d err=%v", n, err)
	}
	n, err := m.ArchiveAuditLog(30, 100)
	if err != nil {
		t.Fatalf("归档报错: %v", err)
	}
	if n != 2 {
		t.Fatalf("两个 schema 各应归档 1 条超龄事件（共 2）；got %d", n)
	}
	if got := m.QueryAudits("tA", "", time.Time{}, time.Time{}, 0); len(got) != 1 || got[0].Action != "fresh" {
		t.Fatalf("tA 归档后应仅剩 fresh；got %+v", got)
	}
	if got := m.QueryAudits("tB", "", time.Time{}, time.Time{}, 0); len(got) != 0 {
		t.Fatalf("tB 的旧事件应被归档；got %+v", got)
	}
}
