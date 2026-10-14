package model

// perm_operator_verbs_test.go — §3.12 守卫（2026-10-14 用户裁定：RBAC 矩阵细化）：
// operator 必须持有三个**作业动词**（task:cancel / alert:ack / alert:silence），
// 同时**不得**拿到管理/毁灭性动词（approve 类 / delete 类 / diagnostics:dump）。
//
// 症状（演练 §3.12，三个人设独立复现）：operator 建得了任务（POST /tasks 201），
// 取消自己建的任务 403 `task:cancel`；alert:ack/silence 同样 403。根因：这三个
// 权限点所在的组（task/alert）本就在 operatorGroups 里，但动作名不属于
// read/write/execute 派生集，被静默丢弃——矩阵洞，不是机制缺失。
//
// 为什么钉双侧（持有 + 不持有）：只钉"该有"会让下一次"顺手放宽动作集"把
// approve/delete 一起放进 operator——最小权限的边界必须和洞一起被钉住。

import (
	"strings"
	"testing"
)

func permSet(perms []string) map[string]bool {
	m := make(map[string]bool, len(perms))
	for _, p := range perms {
		m[p] = true
	}
	return m
}

// TestOperator_HasOperationalVerbs operator 必须持有三个作业动词。
func TestOperator_HasOperationalVerbs(t *testing.T) {
	op := permSet(RolePermissions()["operator"])
	for _, want := range []string{"task:cancel", "alert:ack", "alert:silence"} {
		if !op[want] {
			t.Errorf("operator 缺作业动词 %q——值班角色能开不能关（§3.12 原状）", want)
		}
	}
}

// TestOperator_LacksAdminOnlyVerbs 管理/毁灭性动词仍 admin-only（最小权限边界）。
func TestOperator_LacksAdminOnlyVerbs(t *testing.T) {
	op := permSet(RolePermissions()["operator"])
	for _, forbidden := range []string{
		"task:approve",     // 高风险任务审批
		"cmdb:approve",     // CI 变更审批
		"approval:approve", // 审批流动作
		"device:delete",    // 退役设备（毁灭性）
		"role:delete",      // 删角色
		"user:delete",      // 删用户
		"k8s:delete",       // 删 K8s 集群
		"diagnostics:dump", // 含内部拓扑的诊断包
	} {
		if op[forbidden] {
			t.Errorf("operator 不应持有 %q（审批/毁灭性动作仍 admin-only）", forbidden)
		}
	}
}

// TestOperator_StillBelowAdmin 层级关系：operator 必须是 admin 的真子集
// （admin=全量目录）。**不断言 viewer ⊂ operator**——本平台的 operator 派生
// 刻意不是 viewer 的超集（user/role/ticket 等大量 *:read 只属 viewer，
// operator 只拿 operatorGroups 内的动作），那是既有设计不是倒挂；
// gpu/runbook/incident 的 read 倒挂已按 TD-60 §5.9 另修（见 perm.go 注释）。
func TestOperator_StillBelowAdmin(t *testing.T) {
	rp := RolePermissions()
	admin, op := permSet(rp["admin"]), permSet(rp["operator"])
	for p := range op {
		if !admin[p] {
			t.Errorf("operator 持有 admin 没有的 %q（层级越权）", p)
		}
	}
	if len(op) >= len(admin) {
		t.Errorf("operator 权限数 %d 应少于 admin %d", len(op), len(admin))
	}
}

// TestAdmin_HasEverythingIncludingNewVerbs admin 仍为全量（新增动词自动入 admin）。
func TestAdmin_HasEverythingIncludingNewVerbs(t *testing.T) {
	admin := permSet(RolePermissions()["admin"])
	for _, want := range []string{"task:cancel", "alert:ack", "alert:silence"} {
		if !admin[want] {
			t.Errorf("admin 应持有 %q（admin=全量目录）", want)
		}
	}
	if len(admin) != len(PermSpecs) {
		var missing []string
		for _, ps := range PermSpecs {
			if !admin[ps.Name] {
				missing = append(missing, ps.Name)
			}
		}
		t.Errorf("admin 权限数 %d != 目录 %d，缺：%s", len(admin), len(PermSpecs), strings.Join(missing, ","))
	}
}
