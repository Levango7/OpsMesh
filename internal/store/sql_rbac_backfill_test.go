// sql_rbac_backfill_test.go —— 预置角色权限并集回填的回归用例（2026-09-27）。
//
// 背景（模拟环境实测发现的缺陷）：seedRBAC 对 roles 表用 INSERT IGNORE，角色行
// 首建后永不更新。权限目录（permissions 表）每次启动都会补种新权限点，但老库上
// 预置角色的权限快照停在首建那一刻——0.9.0 建库 → 升级重启后，role-admin 缺
// diagnostics:execute/diagnostics:dump，用户中心登录的 admin 调
// /api/v1/admin/{loglevel,config,diagnostics,store-failures,service-routing} 全部 403。
package store

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"
)

// TestMergePermissionLists 并集语义：只加不减、去重、排序、忽略空串。
func TestMergePermissionLists(t *testing.T) {
	t.Run("老快照补新权限点", func(t *testing.T) {
		stored := []string{"portal:read", "portal:write"}
		computed := []string{"diagnostics:dump", "portal:read", "diagnostics:execute", "portal:write"}
		merged, added := mergePermissionLists(stored, computed)
		want := []string{"diagnostics:dump", "diagnostics:execute", "portal:read", "portal:write"}
		if added != 2 {
			t.Errorf("added = %d，期望 2", added)
		}
		if !reflect.DeepEqual(merged, want) {
			t.Errorf("merged = %v，期望 %v", merged, want)
		}
	})

	t.Run("无变化时不新增", func(t *testing.T) {
		list := []string{"a:read", "b:write"}
		merged, added := mergePermissionLists(list, []string{"b:write", "a:read"})
		if added != 0 {
			t.Errorf("added = %d，期望 0", added)
		}
		if !reflect.DeepEqual(merged, list) {
			t.Errorf("merged = %v，期望与输入一致 %v", merged, list)
		}
	})

	t.Run("computed 消失的条目不被回收", func(t *testing.T) {
		// 管理员手动授予的权限不在当前目录里，不能被启动期的重算悄悄吊销。
		stored := []string{"a:read", "legacy:custom"}
		merged, added := mergePermissionLists(stored, []string{"a:read"})
		if added != 0 || !reflect.DeepEqual(merged, stored) {
			t.Errorf("merged = %v added = %d，期望原样保留", merged, added)
		}
	})

	t.Run("空快照", func(t *testing.T) {
		merged, added := mergePermissionLists(nil, []string{"b:write", "a:read"})
		if added != 2 || !reflect.DeepEqual(merged, []string{"a:read", "b:write"}) {
			t.Errorf("merged = %v added = %d", merged, added)
		}
	})
}

// TestSeedRBAC_BackfillsPresetRolePermissions 集成回归：把 role-admin 的权限快照
// 篡改回「缺 diagnostics:* 的旧版列表」，重跑 seedRBAC 后必须被回填；
// 自定义角色的行不得被动。需真实 MySQL（OPSMESH_TEST_MYSQL_DSN），与
// migration_test.go 同一门控约定。
func TestSeedRBAC_BackfillsPresetRolePermissions(t *testing.T) {
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPSMESH_TEST_MYSQL_DSN not set; skipping RBAC backfill integration test")
	}
	s, cleanup := newTestSQLStore(t)
	defer cleanup()

	// 模拟 0.9.0 时代建库留下的旧快照：admin 缺 diagnostics:*，并带一条
	// 目录里已不存在的自定义授予（不允许被回收）。
	stale := []string{"device:read", "portal:read", "legacy:custom"}
	staleJSON, _ := json.Marshal(stale)
	if _, err := s.db.Exec(`UPDATE roles SET permissions = ? WHERE id = 'role-admin'`, string(staleJSON)); err != nil {
		t.Fatalf("篡改 role-admin 快照失败: %v", err)
	}
	// 自定义角色行（seedRBAC 从不写这个 id，若被回填逻辑误伤即是缺陷）。
	if _, err := s.db.Exec(
		`INSERT INTO roles (id, name, description, permissions, created_at) VALUES ('role-custom', 'custom', '客户自定义', '["device:read"]', ?)`,
		time.Now().UTC()); err != nil {
		t.Fatalf("建自定义角色失败: %v", err)
	}

	if err := s.seedRBAC(context.Background()); err != nil {
		t.Fatalf("seedRBAC: %v", err)
	}

	var got string
	if err := s.db.QueryRow(`SELECT permissions FROM roles WHERE id = 'role-admin'`).Scan(&got); err != nil {
		t.Fatalf("读回 role-admin: %v", err)
	}
	var merged []string
	if err := json.Unmarshal([]byte(got), &merged); err != nil {
		t.Fatalf("解析回填后的 permissions: %v", err)
	}
	set := map[string]bool{}
	for _, p := range merged {
		set[p] = true
	}
	for _, want := range []string{"legacy:custom", "device:read", "diagnostics:execute", "diagnostics:dump"} {
		if !set[want] {
			t.Errorf("回填后缺少 %q（ Got %d 项）", want, len(merged))
		}
	}

	var customPerm string
	if err := s.db.QueryRow(`SELECT permissions FROM roles WHERE id = 'role-custom'`).Scan(&customPerm); err != nil {
		t.Fatalf("读回 role-custom: %v", err)
	}
	if customPerm != `["device:read"]` {
		t.Errorf("自定义角色被误改：%s", customPerm)
	}
}
