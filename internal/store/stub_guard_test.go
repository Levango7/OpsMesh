// stub_guard_test.go stub 领域清单守卫（自 sql_test.go 迁出，TD-61：sql 侧下沉后本组断言只依赖父包 stub_guard.go）。
package store

import (
	"os"
	"testing"
)

// ============================================================================
// H11 stub 模式 skip 计数守卫
// ============================================================================

// TestSQLStore_StubModeSkipGuard 守护 stub 模式下 SQL store 集成测试被跳过。
//
// 无 OPSMESH_TEST_MYSQL_DSN 时（CI build-test 默认），SQL store 真实集成测试
// （TestSQLStore_TenantIsolation）经 t.Skip 跳过；本测试显式断言该环境变量未设置时
// 跳过，并验证桩模式现状（P1-P6 全部持久化后 StubDomains 为空，桩语义测试已移除）。
// 有 DSN 时（integration job），本测试不跳过，断言 StubDomains 仍为空（全部持久化）。
//
// 设计目的：锁定"无 DSN → 集成测试 skip → P1-P6 持久化由 sql_p1p6_test.go 扫描函数测试兜底"
// 的测试分工契约，防止未来误删 t.Skip 导致 CI 在无 MySQL 时尝试连接而超时失败。
func TestSQLStore_StubModeSkipGuard(t *testing.T) {
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		// stub 模式：SQL store 集成测试被跳过，P1-P6 持久化由 sql_p1p6_test.go 扫描函数测试保证。
		t.Skip("OPSMESH_TEST_MYSQL_DSN not set; stub mode active (SQL store integration skipped, P1-P6 scan funcs guarded by sql_p1p6_test.go)")
	}
	// 有 DSN：断言 StubDomains 仍为空（P1-P6 全部已持久化，无桩领域）。
	if len(StubDomains) != 0 {
		t.Fatalf("StubDomains count = %d, want 0 (P1-P6 全部已持久化)", len(StubDomains))
	}
}

// TestSQLStore_StubDomainsCount 锁定桩领域数量为 0（P1-P6 全部已持久化）。
// 防止回退导致 StubDomains 清单非空（H3 缓解：让空壳可见，但现在已无空壳）。
func TestSQLStore_StubDomainsCount(t *testing.T) {
	const wantStubDomainCount = 0 // P1-P6 全部 15 个领域已实现 MySQL 持久化（sql_p01.go ~ sql_p06.go）
	if len(StubDomains) != wantStubDomainCount {
		t.Fatalf("StubDomains count = %d, want %d (P1-P6 全部已持久化，清单应为空)", len(StubDomains), wantStubDomainCount)
	}
}

// TestStubGuard_JoinAndWarnDomains 覆盖 stub_guard.go 的展示辅助：
// joinStubDomains 输出格式、WarnStubStoreDomains 不 panic、StubDomains 完整性。
// 现状：P1-P6 全部 15 个领域已实现 MySQL 持久化，StubDomains 收敛为空，
// joinStubDomains 返回空串、WarnStubStoreDomains 静默返回（不再告警）。
func TestStubGuard_JoinAndWarnDomains(t *testing.T) {
	got := joinStubDomains()
	want := ""
	if got != want {
		t.Fatalf("joinStubDomains() = %q, want %q", got, want)
	}
	if len(StubDomains) != 0 {
		t.Fatalf("StubDomains count = %d, want 0 (P1-P6 全部已持久化)", len(StubDomains))
	}
	// 构造函数接线告警：StubDomains 为空时静默返回，不应 panic 也不应告警。
	WarnStubStoreDomains("multi-schema-test")
	// StubNotImplemented 限频：同 key 连续两次调用不 panic，且第二次走窗口内静默分支。
	// 保留调用以覆盖限频逻辑（StubNotImplemented 本身与 StubDomains 解耦，仍可用）。
	StubNotImplemented("test-domain", "TestMethod")
	StubNotImplemented("test-domain", "TestMethod")
}
