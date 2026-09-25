package store

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/go-sql-driver/mysql"
)

// 真实 MySQL 集成冒烟（对齐 internal/store 的既有约定）。
//
//	OPSMESH_TEST_MYSQL_DSN="opsmesh:pass@tcp(127.0.0.1:3306)/opsmesh_auth?parseTime=true" \
//	  go test ./internal/store/ -run TestMySQLStoreIntegration -v
//
// 未设置 DSN 时 t.Skip 并打印原因（不静默跳过）。
//
// 覆盖面：NewMySQLStore 建表 → seedDefaults 写入 → 读回。
// 建表此前从未执行过：NewMySQLStore 在 Ping 之后直接 seedDefaults 做 INSERT，
// 表不存在则启动即失败，故 RoundTrip 必须走通才算通过。
func TestMySQLStoreIntegration(t *testing.T) {
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("SKIP reason=missing OPSMESH_TEST_MYSQL_DSN; 真实 MySQL 集成冒烟需设置该环境变量")
	}

	s, err := NewMySQLStore(dsn)
	if err != nil {
		t.Fatalf("NewMySQLStore: %v（建表或 seedDefaults 失败）", err)
	}
	defer func() { _ = s.Close() }()

	// 1) 建表：schema.sql 声明的 7 张表齐备。
	assertTablesExist(t, s.db)

	// 2) seedDefaults 已写入默认权限与 admin 角色（须能读回）。
	perms := s.ListPermissions()
	if len(perms) == 0 {
		t.Error("ListPermissions 为空：seedDefaults 未生效")
	}
	if role := s.GetRoleByName("admin"); role == nil {
		t.Error("GetRoleByName(admin) 为 nil：admin 角色未种入")
	}

	// 3) 用户往返：建 → 按名读 → 改密 → 删除。
	u, err := s.CreateUser(&User{Username: "itest-user", Email: "itest@example.com", PasswordHash: "x"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u == nil || u.ID == "" {
		t.Fatal("CreateUser 未返回带 ID 的用户")
	}
	got := s.GetUserByUsername("itest-user")
	if got == nil {
		t.Fatal("GetUserByUsername 为 nil（写入后读不到）")
	}
	if got.Email != "itest@example.com" {
		t.Errorf("email 往返不一致: %q", got.Email)
	}
	if err := s.ChangePassword(u.ID, "y"); err != nil {
		t.Errorf("ChangePassword: %v", err)
	}
	if err := s.DeleteUser(u.ID); err != nil {
		t.Errorf("DeleteUser: %v", err)
	}
}

// assertTablesExist 校验 schema.sql 声明的 7 张表均已建出。
func assertTablesExist(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, table := range []string{"users", "roles", "permissions", "user_roles", "role_permissions", "refresh_tokens", "jti_blacklist"} {
		var n int
		err := db.QueryRow(
			"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name=?",
			table,
		).Scan(&n)
		if err != nil {
			t.Fatalf("查询 %s 是否存在失败: %v", table, err)
		}
		if n == 0 {
			t.Errorf("表 %s 未被创建（迁移未执行或 schema.sql 缺该表）", table)
		}
	}
}
