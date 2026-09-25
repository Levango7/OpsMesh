// register_tenant_guard_test.go — P1-2 跨租户重绑定防护的存储层断言。
//
// 威胁：gRPC Register 不硬（无 install token 时任何人可注册任意 agentID），
// 若允许换租户重注册，攻击者以他人 agentID + 自己的租户重注册即可接管该 agent
// （下发任务、读日志）。存储层必须拒绝「既有非空租户 → 不同租户（含空）」。
package store

import (
	"testing"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// TestMemoryStore_RegisterRejectsCrossTenantRebind 内存 store 的四条边界：
// 换租户拒 / 空租户降级拒 / 同租户幂等放行 / 空→非空首次绑定放行。
func TestMemoryStore_RegisterRejectsCrossTenantRebind(t *testing.T) {
	m := NewMemoryStore()
	if got := m.Register(&proto.AgentInfo{AgentID: "agent-x", Segment: "s1", TenantID: "t-owner"}); got == nil {
		t.Fatal("首次注册应成功")
	}
	secret := m.AgentSecret("agent-x")
	if secret == "" {
		t.Fatal("首次注册应生成 per-agent 密钥")
	}

	// 1) 换租户重注册 → 拒绝，且原租户/密钥不变。
	if got := m.Register(&proto.AgentInfo{AgentID: "agent-x", Segment: "s1", TenantID: "t-attacker"}); got != nil {
		t.Fatalf("跨租户重注册应被拒绝，得到 %+v", got)
	}
	if a := m.Agent("agent-x"); a == nil || a.TenantID != "t-owner" {
		t.Fatalf("agent 租户被改写：%+v", a)
	}
	if got := m.AgentSecret("agent-x"); got != secret {
		t.Fatalf("密钥被重置：%q != %q", got, secret)
	}

	// 2) 空租户重注册（把租户清空以绕过隔离）→ 同样拒绝。
	if got := m.Register(&proto.AgentInfo{AgentID: "agent-x", Segment: "s1"}); got != nil {
		t.Fatalf("以空租户重注册应被拒绝，得到 %+v", got)
	}
	if a := m.Agent("agent-x"); a == nil || a.TenantID != "t-owner" {
		t.Fatalf("agent 租户被清空：%+v", a)
	}

	// 3) 同租户重注册 → 幂等放行（agent 重启/升级场景）。
	if got := m.Register(&proto.AgentInfo{AgentID: "agent-x", Segment: "s2", TenantID: "t-owner"}); got == nil {
		t.Fatal("同租户重注册应放行")
	}

	// 4) 既有租户为空 → 允许首次绑定租户（历史无租户数据迁移路径）。
	m2 := NewMemoryStore()
	m2.Register(&proto.AgentInfo{AgentID: "agent-y", Segment: "s1"})
	if got := m2.Register(&proto.AgentInfo{AgentID: "agent-y", Segment: "s1", TenantID: "t-new"}); got == nil {
		t.Fatal("空租户 → 非空租户应放行（首次归属）")
	}
	if a := m2.Agent("agent-y"); a == nil || a.TenantID != "t-new" {
		t.Fatalf("首次归属未生效：%+v", a)
	}
}

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
