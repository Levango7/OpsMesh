// audits_cap_test.go 审计环形上限截断（自 internal/store/store_extra_test.go 迁入，TD-61）。
//
// 迁入理由：断言依赖 auditCap 常量（memory 包私有，只对本包可见）。
package memory

import (
	"testing"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// TestMemoryStore_AuditsCap_Truncate 验证审计环形上限截断。
func TestMemoryStore_AuditsCap_Truncate(t *testing.T) {
	m := NewMemoryStore()
	// 写入超过 auditCap 条，验证截断
	for i := 0; i < auditCap+10; i++ {
		m.Audit(&proto.AuditEvent{TenantID: "t1", Action: "test", Target: string(rune(i))})
	}
	if got := m.Audits(); len(got) != auditCap {
		t.Fatalf("Audits after truncate = %d, want %d", len(got), auditCap)
	}
}
