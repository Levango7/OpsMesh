package grpc

// agent_tenant_visibility_test.go — A1 回归守卫（2026-10-12 模拟用户演练）。
//
// 要钉住的因果链（演练里三名"用户"都撞到、值班那位读码定到行）：
//   agent 裸注册（无 token、无网关注入租户）→ 租户落空 → **每一条按租户切片的读路径都看不到它**
//   （handleAgents("default") / 设备快照 Snapshot("default") / 前端看板）→ 用户视角
//   "设备清单是空的、任务也建不了"，而 agent 明明在心跳。
// 坏法的形态是**静默**：注册接口返回成功、心跳 200，没有任何报错。
//
// 判据用"控制面看板的眼睛"看：注册后必须能在 default 租户视图里查到该 agent（与它的占位设备）。

import (
	"context"
	"testing"

	"github.com/Levango7/OpsMesh/internal/proto"
)

func TestRegister_WithoutTenantVisibleInDefault_NonDemo(t *testing.T) {
	// 关键：demo=false（原实现只在 demo 下兜底 ⇒ 此处正是踩雷配置）
	g, st := newTestServer(t, false, false)

	resp, err := g.Register(context.Background(), &proto.AgentInfo{AgentID: "a-bare", Segment: "seg-a"})
	if err != nil {
		t.Fatalf("裸注册应成功: %v", err)
	}
	if resp == nil || resp.AgentID == "" {
		t.Fatalf("注册未返回 agentID: %+v", resp)
	}

	got := st.Agent(resp.AgentID)
	if got == nil {
		t.Fatal("注册后 agent 未落库")
	}
	if got.TenantID == "" {
		t.Fatalf("租户为空——空租户会被所有按租户切片的读路径滤掉（A1 原形状）")
	}
	if got.TenantID != "default" {
		t.Errorf("TenantID=%q，want default", got.TenantID)
	}

	// 以控制面看板的眼睛复核：default 租户视图必须看得到它
	visible := false
	for _, a := range st.Agents("default") {
		if a.AgentID == resp.AgentID {
			visible = true
			break
		}
	}
	if !visible {
		t.Error("default 租户视图看不到刚注册的 agent —— 用户会把'清单是空的'报成故障（A1 回归）")
	}
}

// TestRegister_TenantFromGatewayStillWins 有网关注入租户时仍以它为权威（修复不得改变原优先级）。
func TestRegister_TenantFromGatewayStillWins(t *testing.T) {
	g, st := newTestServer(t, false, false)
	resp, err := g.Register(tenantCtx("t1"), &proto.AgentInfo{AgentID: "a-gw", Segment: "seg-a"})
	if err != nil {
		t.Fatalf("注册应成功: %v", err)
	}
	got := st.Agent(resp.AgentID)
	if got == nil {
		t.Fatal("注册后 agent 未落库")
	}
	if got.TenantID != "t1" {
		t.Errorf("TenantID=%q，want t1（网关注入租户是权威，不得被默认值覆盖）", got.TenantID)
	}
}
