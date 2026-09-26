// sql_rbac_seed_test.go 锁住 seedRBAC 对 must_change_password 的语义（升级/重启回归）。
//
// 背景（2026-09-26 真机升级演练实测到的缺陷）：seedRBAC 由 runMigrations 在**每次进程启动**
// 调用，而它原先无条件 `ON DUPLICATE KEY UPDATE must_change_password=1` ⇒ 客户改过口令之后，
// 任何一次重启或升级都会把该标记复活成 1；登录接口于是只返回 changePasswordToken、
// 不返回会话 token，表现为"我刚升级/重启完，管理员又要被迫改一次密码"。
//
// 测试策略（与 migration_test.go 一致）：
//   - 集成层，需真实 MySQL（OPSMESH_TEST_MYSQL_DSN），未设置则 Skip；
//   - 每个用例建独立临时库，defer 删库；
//   - 断言两个方向：改过口令的账号**不**被复活；仍用预置口令的账号**要**被重新标记。
package store

import (
	"context"
	"database/sql"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func mustFlag(t *testing.T, db *sql.DB, userID string) int {
	t.Helper()
	var flag int
	if err := db.QueryRow(`SELECT must_change_password FROM users WHERE id = ?`, userID).Scan(&flag); err != nil {
		t.Fatalf("读 %s 的 must_change_password 失败: %v", userID, err)
	}
	return flag
}

func mustHash(t *testing.T, db *sql.DB, userID string) string {
	t.Helper()
	var h string
	if err := db.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&h); err != nil {
		t.Fatalf("读 %s 的 password_hash 失败: %v", userID, err)
	}
	return h
}

// TestSeedRBAC_DoesNotReviveMustChangeAfterUserChangedPassword 是本缺陷的回归用例。
func TestSeedRBAC_DoesNotReviveMustChangeAfterUserChangedPassword(t *testing.T) {
	s, cleanup := newTestSQLStore(t)
	defer cleanup()
	ctx := context.Background()

	// 首启：三个预置账号都应被标记为"必须改密"。
	for _, id := range []string{"user-admin", "user-operator", "user-viewer"} {
		if got := mustFlag(t, s.db, id); got != 1 {
			t.Fatalf("首启后 %s 的 must_change_password 应为 1，实得 %d", id, got)
		}
	}

	// 模拟运维改过 admin 的口令（真实改密路径的两件事：换哈希 + 清标记）。
	newHash, err := bcryptHash("Operator#Changed2026x")
	if err != nil {
		t.Fatalf("bcryptHash: %v", err)
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE users SET password_hash = ?, must_change_password = 0 WHERE id = 'user-admin'`, string(newHash)); err != nil {
		t.Fatalf("模拟改密失败: %v", err)
	}
	// operator 保持预置哈希不变，只把标记清成 0——用来证明"仍用预置口令者仍会被重新标记"。
	if _, err := s.db.ExecContext(ctx,
		`UPDATE users SET must_change_password = 0 WHERE id = 'user-operator'`); err != nil {
		t.Fatalf("清 operator 标记失败: %v", err)
	}
	beforeHash := mustHash(t, s.db, "user-admin")

	// 再跑一次 seedRBAC —— 等价于"升级/重启一次"。
	if err := s.seedRBAC(ctx); err != nil {
		t.Fatalf("第二次 seedRBAC: %v", err)
	}

	if got := mustFlag(t, s.db, "user-admin"); got != 0 {
		t.Errorf("改过口令的 admin 在重启后又被标记成 must_change_password=%d（缺陷复现）：期望保持 0", got)
	}
	if got := mustFlag(t, s.db, "user-operator"); got != 1 {
		t.Errorf("仍在使用预置口令的 operator 重启后未被重新标记（got=%d，期望 1）：安全语义不得放宽", got)
	}
	// 口令本身绝不能被 seed 覆盖回预置值。
	if got := mustHash(t, s.db, "user-admin"); got != beforeHash {
		t.Errorf("seedRBAC 覆盖了 admin 的 password_hash（改了口令又被写回预置哈希）")
	}
	// 再确认一次：admin 的哈希确实不再是预置口令。
	if err := bcrypt.CompareHashAndPassword([]byte(beforeHash), []byte("admin123")); err == nil {
		t.Errorf("admin 的哈希仍能匹配预置口令 admin123 —— 说明模拟改密没生效，本用例无效")
	}
}

// TestSeedRBAC_ConcurrentReSeedIsIdempotent 覆盖多副本同时首启：不得因主键冲突报错。
func TestSeedRBAC_ConcurrentReSeedIsIdempotent(t *testing.T) {
	s, cleanup := newTestSQLStore(t)
	defer cleanup()
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := s.seedRBAC(ctx); err != nil {
			t.Fatalf("第 %d 次 seedRBAC 报错（幂等性不成立）: %v", i+1, err)
		}
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE id IN ('user-admin','user-operator','user-viewer')`).Scan(&n); err != nil {
		t.Fatalf("计数失败: %v", err)
	}
	if n != 3 {
		t.Fatalf("重复 seed 后预置账号数应为 3，实得 %d", n)
	}
}
