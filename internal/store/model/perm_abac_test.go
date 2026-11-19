package model

// perm_abac_test.go — abac 族权限点与角色派生契约（2026-11-17）。
//
// 背景：/api/v1/abac 曾复用 role:read/role:write 作为闸门，而 operator 不持有
// role:read ⇒ 值班角色失去 evaluate/policy-list（评估只返回决策、不改变授权，
// 本应可用）。abac 族独立后必须钉住两侧：
//   - admin 持有 abac:read + abac:write（策略写面仅 admin）；
//   - operator/viewer 持有 abac:read 且不持有 abac:write。
//
// 目录缺失的后果与仓库历次补种同形：端点对含 admin 在内的全部角色恒 403。

import (
	"testing"
)

func TestABACPermissionCatalogAndRoles(t *testing.T) {
	catalog := map[string]bool{}
	for _, p := range PermSpecs {
		catalog[p.Name] = true
	}
	for _, want := range []string{"abac:read", "abac:write"} {
		if !catalog[want] {
			t.Fatalf("权限目录缺 %q：/api/v1/abac 将对全部角色（含 admin）恒 403", want)
		}
	}

	roles := RolePermissions()
	admin := permSet(roles["admin"])
	if !admin["abac:read"] {
		t.Error("admin 应持有 abac:read（admin=全量目录）")
	}
	if !admin["abac:write"] {
		t.Error("admin 应持有 abac:write（策略写面仅 admin，否则无人能创建策略）")
	}

	for _, role := range []string{"operator", "viewer"} {
		got := permSet(roles[role])
		if !got["abac:read"] {
			t.Errorf("%s 应持有 abac:read（evaluate/list 是咨询面，不得低于只读角色）", role)
		}
		if got["abac:write"] {
			t.Errorf("%s 不得持有 abac:write（创建/删除策略会改变决策面，最小权限仅 admin）", role)
		}
	}
}
