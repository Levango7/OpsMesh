package controlplane

import "testing"

// 审计整改批次17：ABAC 引擎语义守卫。
// 设计口径：deny-overrides + default-deny；通配仅支持 `*` 与 `prefix*`。

func newABACTestServer() *Server {
	return &Server{}
}

func TestEvaluateABAC_DefaultDeny(t *testing.T) {
	s := newABACTestServer()
	ok, id, reason := s.evaluateABAC("t1", "alice", "vm:1", "read", "")
	if ok {
		t.Fatalf("无策略时必须 default-deny，得到 allowed=true (id=%q)", id)
	}
	if id != "" {
		t.Fatalf("default-deny 不应匹配策略，得到 id=%q", id)
	}
	if reason == "" {
		t.Fatal("default-deny 必须给出 reason")
	}
}

func TestEvaluateABAC_AllowMatch(t *testing.T) {
	s := newABACTestServer()
	s.addABACPolicy(abacPolicy{ID: "p1", TenantID: "t1", Subject: "alice", Resource: "vm:*", Action: "read", Effect: "allow"})
	ok, id, _ := s.evaluateABAC("t1", "alice", "vm:42", "read", "")
	if !ok || id != "p1" {
		t.Fatalf("应命中 p1 允许，得到 ok=%v id=%q", ok, id)
	}
	// 跨租户不生效
	if ok, _, _ := s.evaluateABAC("t2", "alice", "vm:42", "read", ""); ok {
		t.Fatal("策略不得跨租户生效")
	}
}

func TestEvaluateABAC_DenyOverrides(t *testing.T) {
	s := newABACTestServer()
	s.addABACPolicy(abacPolicy{ID: "allow-all", TenantID: "t1", Subject: "*", Resource: "*", Action: "*", Effect: "allow"})
	s.addABACPolicy(abacPolicy{ID: "deny-prod", TenantID: "t1", Subject: "*", Resource: "vm:prod-*", Action: "*", Effect: "deny"})
	ok, id, reason := s.evaluateABAC("t1", "bob", "vm:prod-1", "write", "")
	if ok || id != "deny-prod" {
		t.Fatalf("deny 必须优先，得到 ok=%v id=%q reason=%q", ok, id, reason)
	}
	if ok, id, _ := s.evaluateABAC("t1", "bob", "vm:dev-1", "write", ""); !ok || id != "allow-all" {
		t.Fatalf("非 deny 资源应命中 allow-all，得到 ok=%v id=%q", ok, id)
	}
}

func TestEvaluateABAC_Conditions(t *testing.T) {
	s := newABACTestServer()
	s.addABACPolicy(abacPolicy{
		ID: "p-cond", TenantID: "t1", Subject: "alice", Resource: "*", Action: "write", Effect: "allow",
		Condition: map[string]string{"env": "staging"},
	})
	if ok, _, _ := s.evaluateABAC("t1", "alice", "vm:1", "write", `{"env":"staging"}`); !ok {
		t.Fatal("条件满足时应允许")
	}
	if ok, _, _ := s.evaluateABAC("t1", "alice", "vm:1", "write", `{"env":"production"}`); ok {
		t.Fatal("条件不满足时不得允许")
	}
	if ok, _, _ := s.evaluateABAC("t1", "alice", "vm:1", "write", `not-json`); ok {
		t.Fatal("context 非法时不得允许（安全侧）")
	}
}

func TestABACPolicyRegistry_CRUD(t *testing.T) {
	s := newABACTestServer()
	s.addABACPolicy(abacPolicy{ID: "p1", TenantID: "t1", Subject: "a", Resource: "r", Action: "x", Effect: "allow"})
	s.addABACPolicy(abacPolicy{ID: "p2", TenantID: "t1", Subject: "b", Resource: "r", Action: "x", Effect: "deny"})
	if got := len(s.listABACPolicies("t1")); got != 2 {
		t.Fatalf("策略数=%d, want 2", got)
	}
	s.removeABACPolicy("p1")
	list := s.listABACPolicies("t1")
	if len(list) != 1 || list[0].ID != "p2" {
		t.Fatalf("删除后应仅剩 p2，得到 %+v", list)
	}
	// 重复 add 同 ID 为更新而非追加
	s.addABACPolicy(abacPolicy{ID: "p2", TenantID: "t1", Subject: "b2", Resource: "r", Action: "x", Effect: "deny"})
	if got := len(s.listABACPolicies("t1")); got != 1 {
		t.Fatalf("同 ID 更新后策略数=%d, want 1", got)
	}
}
