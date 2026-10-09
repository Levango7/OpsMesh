// register_tenant_guard_sql_test.go SQL 侧跨租户重绑定防护（需真实 MySQL）。
//
// 自 internal/store/register_tenant_guard_test.go 拆出（TD-61 批次 3-sql）。内存侧断言见父包同名测试。
package sqlstore

import (
	"testing"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// TestSQLStore_RegisterRejectsCrossTenantRebind SQL store 同规则（需真实 MySQL）。
func TestSQLStore_RegisterRejectsCrossTenantRebind(t *testing.T) {
	s, cleanup := newTestSQLStore(t)
	defer cleanup()

	if got := s.Register(&proto.AgentInfo{AgentID: "agent-sql-x", Segment: "s1", TenantID: "t-owner"}); got == nil {
		t.Fatal("首次注册应成功")
	}
	secret := s.AgentSecret("agent-sql-x")
	if secret == "" {
		t.Fatal("首次注册应生成 per-agent 密钥")
	}

	// 换租户重注册 → 拒绝（返回 nil），原行租户与密钥不变。
	if got := s.Register(&proto.AgentInfo{AgentID: "agent-sql-x", Segment: "s1", TenantID: "t-attacker"}); got != nil {
		t.Fatalf("跨租户重注册应被拒绝，得到 %+v", got)
	}
	if a := s.Agent("agent-sql-x"); a == nil || a.TenantID != "t-owner" {
		t.Fatalf("agent 租户被改写：%+v", a)
	}
	if got := s.AgentSecret("agent-sql-x"); got != secret {
		t.Fatalf("密钥被重置：%q != %q", got, secret)
	}

	// 空租户降级重注册 → 同样拒绝。
	if got := s.Register(&proto.AgentInfo{AgentID: "agent-sql-x", Segment: "s1"}); got != nil {
		t.Fatalf("以空租户重注册应被拒绝，得到 %+v", got)
	}

	// 同租户重注册 → 幂等放行（重启/升级路径），密钥保持。
	if got := s.Register(&proto.AgentInfo{AgentID: "agent-sql-x", Segment: "s2", TenantID: "t-owner"}); got == nil {
		t.Fatal("同租户重注册应放行")
	}
	if got := s.AgentSecret("agent-sql-x"); got != secret {
		t.Fatalf("同租户重注册后密钥变化：%q != %q", got, secret)
	}

	// 空租户 → 非空首次归属放行。
	if got := s.Register(&proto.AgentInfo{AgentID: "agent-sql-y", Segment: "s1"}); got == nil {
		t.Fatal("空租户首次注册应成功")
	}
	if got := s.Register(&proto.AgentInfo{AgentID: "agent-sql-y", Segment: "s1", TenantID: "t-new"}); got == nil {
		t.Fatal("空租户 → 非空租户应放行（首次归属）")
	}
}
