package store

// operator_seed_test.go — role-operator 种子守卫（2026-10-13 演练移交项）。
//
// 背景：controlplane 预置角色三件套 admin/operator/viewer，本侧此前只 seed
// admin/viewer ⇒ controlplane 持 role-operator 的用户被 cutover-migrate 搬过来时
// DroppedRoles=[role-operator]、/me 出现 roleIDs:[]/permissions:null（迁移成功
// 但身份面残废）。本文件钉住新种子，防回归。

import (
	"sort"
	"strings"
	"testing"
)

// TestSeedOperatorRole_PresentWithReadOnlyProjection 内存后端必须 seed
// role-operator，且权限 = 目录内全部 `*:read`（与 viewer 同集——这是控制面
// operator 语义投影到用户中心目录的**正确结果**：控制面 operator 不持
// user/role 任何权限，业务面 read 它才有）。
func TestSeedOperatorRole_PresentWithReadOnlyProjection(t *testing.T) {
	s := NewMemoryStore()
	r := s.GetRole("role-operator")
	if r == nil {
		t.Fatal("role-operator 未 seed——迁移用户会丢这个角色（/me roleIDs:[]/permissions:null）")
	}
	if r.Name != "operator" {
		t.Fatalf("role-operator 的 name = %q, want operator（role_permissions/rolesByName 按名查找）", r.Name)
	}

	// 与 viewer 的事件顺序无关：集合必须逐项相等（目录内全部只读）。
	vr := s.GetRole("role-viewer")
	if vr == nil {
		t.Fatal("前置失败：role-viewer 不存在")
	}
	got := append([]string{}, r.Permissions...)
	want := append([]string{}, vr.Permissions...)
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("operator 权限应=目录内全部只读（与 viewer 同集）：\n got=%v\nwant=%v", got, want)
	}
	// 且每条确实是 :read（防将来有人误加 write）。
	for _, p := range r.Permissions {
		if !strings.HasSuffix(p, ":read") {
			t.Errorf("operator 不应持有非只读权限 %q（用户/角色管理是 admin 专属）", p)
		}
	}

	// 管理员面不受影响：role-admin 仍持有全部权限。
	ar := s.GetRole("role-admin")
	if ar == nil || len(ar.Permissions) < len(r.Permissions) {
		t.Fatalf("role-admin 应仍持全部权限（不少于 operator）")
	}
}

// TestSeedOperatorRole_LookupByName 按名可达（网关/迁移侧可能按角色名查）。
func TestSeedOperatorRole_LookupByName(t *testing.T) {
	s := NewMemoryStore()
	r := s.GetRoleByName("operator")
	if r == nil || r.ID != "role-operator" {
		t.Fatalf("GetRoleByName(operator) 应命中 role-operator，实得 %+v", r)
	}
}
