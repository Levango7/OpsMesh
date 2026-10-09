// sql_rbac_catalog_test.go 锁住「权限目录完整性」与角色派生效应。
//
// 缺陷背景（2026-09-29 sim 取证发现）：15 个权限点被 controlplane handler 的
// requireProd/requirePermission 校验引用，但从未进入 rbacPermSpecs——RolePermissions
// 派生集因此不含它们，任何内置角色（含 admin）都无法持有，对应端点对全部角色
// 恒 403（实测：admin GET /api/v1/schedules → 403 permission denied: schedule:read；
// 本地单体路径与 task-svc 平行代理路径行为一致）。
//
// 测试策略：纯函数测试（无需 MySQL）——RolePermissions() 是 seedRBAC 与 RBAC 闸
// 的共同单一来源，直接断言目录覆盖与派生集合。新增 requireProd 引用新权限点时，
// 请同步在 rbacPermSpecs 补齐并追加到 handlerRequiredPerms。
//
// 覆盖面说明（2026-10-10）：本守护只覆盖**手工维护的 handler 权限点清单**。
// 由**规则数据**在运行期解析出的权限点（微服务聚合代理）不在本清单内，
// 那条链路由 internal/controlplane/svcproxy/perm_catalog_test.go 守护
// （三张规则表的 Perm/PermRules[].Perm 必须 ⊆ 目录 ∪ PermAuthenticated 哨兵）——
// 起因：auth 代理规则曾引用目录外的 auth:read/auth:write，导致双轨开关打开即恒 403，
// 而本文件的手工清单对此是盲的。
package store

import (
	"strings"
	"testing"
)

// handlerRequiredPerms 2026-09-29 一次性盘点的缺口清单（controlplane 全部
// requireProd/requirePermission 字符串字面量 vs 目录比对的结果，15 项）。
var handlerRequiredPerms = []string{
	"alert:write",
	"approval:read", "approval:write", "approval:approve",
	"helm:read", "helm:write",
	"middleware:write",
	"os:write",
	"quota:read", "quota:write",
	"schedule:read", "schedule:write",
	"secrets:read", "secrets:write",
	"task:approve",
}

// TestRolePermissions_CoversHandlerRequiredPerms 目录覆盖守护：
// handler 引用的权限点必须在目录中，否则 admin 也不持有 → 端点恒 403。
func TestRolePermissions_CoversHandlerRequiredPerms(t *testing.T) {
	admin := map[string]bool{}
	for _, p := range RolePermissions()["admin"] {
		admin[p] = true
	}
	for _, p := range handlerRequiredPerms {
		if !admin[p] {
			t.Errorf("权限目录缺 %q：handler 校验引用它但 admin 不持有，端点将对全部角色 403", p)
		}
	}
}

// TestRolePermissions_DerivationEffects 锁定 15 项补齐后的角色派生效应：
//   - viewer = 全部 *:read（新增 read 自动落入，viewer 永远不可持有写权限）；
//   - operator = operatorGroups × {read,write,execute} ∪ operatorReadOnlyGroups × {read}
//     （新增 write 中仅 alert/os/middleware 三组在列；approve 类不属派生动作集，仅 admin）。
//   - operatorReadOnlyGroups（gpu/runbook/incident/k8s，2026-09-30 补）：修
//     「operator 低于 viewer」的层级倒挂 —— operator 派生不是全部 *:read，
//     这三域此前既无 read 也无 write，而 viewer 反而持有其 read。
func TestRolePermissions_DerivationEffects(t *testing.T) {
	perms := RolePermissions()

	viewer := map[string]bool{}
	for _, p := range perms["viewer"] {
		viewer[p] = true
		if !strings.HasSuffix(p, ":read") {
			t.Errorf("viewer 派生含非 read 权限 %q（viewer 只读）", p)
		}
	}
	for _, p := range []string{"approval:read", "helm:read", "quota:read", "schedule:read", "secrets:read"} {
		if !viewer[p] {
			t.Errorf("viewer 应随 *:read 派生获得 %q", p)
		}
	}

	operator := map[string]bool{}
	for _, p := range perms["operator"] {
		operator[p] = true
	}
	for _, p := range []string{"alert:write", "middleware:write", "os:write"} {
		if !operator[p] {
			t.Errorf("operator 应随 operatorGroups×read/write 派生获得 %q", p)
		}
	}

	// operatorReadOnlyGroups（gpu/runbook/incident/k8s）：operator 必须拿到 read，
	// 且不得拿到 write —— 前端路由门 requirePerm 只要求 *:read；write 下放属产品语义，未决前不下放。
	for _, p := range []string{"gpu:read", "runbook:read", "incident:read", "k8s:read"} {
		if !operator[p] {
			t.Errorf("operator 应随 operatorReadOnlyGroups 获得只读权限 %q（否则低于 viewer，角色层级倒挂）", p)
		}
	}
	for _, p := range []string{"gpu:write", "runbook:write", "incident:write", "k8s:write"} {
		if operator[p] {
			t.Errorf("operator 不应获得 %q（operatorReadOnlyGroups 只授 read，write 下放未决）", p)
		}
	}
	for _, p := range []string{"approval:approve", "task:approve", "schedule:write", "quota:write", "secrets:write", "helm:write"} {
		if operator[p] {
			t.Errorf("operator 不应获得 %q（最小权限：审批/敏感域仅 admin）", p)
		}
	}
}
