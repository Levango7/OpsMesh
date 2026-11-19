package store

import (
	"os"
	"strings"
	"testing"
	"time"
)

// user_tenant_test.go —— users.tenant_id 的**静默丢弃**回归钉子（D4 / issue #27 裁决②）。
//
// 缺陷原型（2026-11-16 逐条对账外部评估报告时查出，报告只说了「用户表没有租户字段」，
// 未点出后果）：
//   - store.User 有 TenantID（store.go），proto 也有（auth.pb.go field 8）；
//   - service.CreateUser 把 resolveTenantID(req.GetTenantId()) 设进了结构体；
//   - **但** schema.sql 的 users 表无该列、mysql.go 的 INSERT/SELECT 也不含该列
//     ⇒ 归属在写入时被静默丢弃；登录时 IssueTokenWithTTL(..., u.TenantID, ...) 拿到空串，
//     claims.tenant_id 回退 "default"（jwt.go）⇒ **所有用户落进 default 租户**。
//
// 两条判据：
//  1. 静态（不需库）：DDL 与 SQL 语句必须含 tenant_id —— 防复发；
//  2. 动态（真库门控）：创建→读回必须原值返回 —— 防「DDL 加了但语句漏改」。

func TestUserTenantColumnInDDL(t *testing.T) {
	ddl, err := os.ReadFile("schema.sql")
	if err != nil {
		t.Fatalf("读 schema.sql: %v", err)
	}
	if !strings.Contains(string(ddl), "tenant_id") {
		t.Fatal("schema.sql 的 users 表缺少 tenant_id —— 归属字段会随 INSERT 静默丢弃")
	}

	src, err := os.ReadFile("mysql.go")
	if err != nil {
		t.Fatalf("读 mysql.go: %v", err)
	}
	code := string(src)
	// INSERT 必须带该列，否则 CreateUser 的 TenantID 直接丢失
	if !strings.Contains(code, "INSERT INTO users (id, tenant_id,") {
		t.Error("mysql.go 的 CreateUser INSERT 不含 tenant_id —— 写入时归属被丢弃")
	}
	// 三处 SELECT 都要读回来，否则登录签发 JWT 仍拿到空租户
	for _, frag := range []string{
		"SELECT id, tenant_id, username, email, password_hash, status, created_at, must_change_password FROM users WHERE id = ?",
		"SELECT id, tenant_id, username, email, password_hash, status, created_at, must_change_password FROM users WHERE username = ?",
		"SELECT id, tenant_id, username, email, password_hash, status, created_at, must_change_password FROM users",
	} {
		if !strings.Contains(code, frag) {
			t.Errorf("mysql.go 缺少含 tenant_id 的查询：%s", frag)
		}
	}
}

// TestUserTenantIDRoundTripMySQL 真库往返：创建时指定的租户必须原值读回。
// 未设置 OPSMESH_TEST_MYSQL_DSN 时跳过（CI 的 services job 会设置，故本例在 CI 真跑）。
func TestUserTenantIDRoundTripMySQL(t *testing.T) {
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("SKIP reason=missing OPSMESH_TEST_MYSQL_DSN; 真库往返需设置该环境变量")
	}
	s, err := NewMySQLStore(dsn)
	if err != nil {
		t.Fatalf("NewMySQLStore: %v（建表/补列失败）", err)
	}
	defer s.Close()

	name := "tenant-nail-" + time.Now().UTC().Format("20060102150405")
	want := "acme-" + name
	_, err = s.CreateUser(&User{
		Username:  name,
		Email:     name + "@example.invalid",
		TenantID:  want,
		RoleIDs:   []string{"role-viewer"},
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	got := s.GetUserByUsername(name)
	if got == nil {
		t.Fatal("GetUserByUsername 未命中——用户没落库")
	}
	defer func() { _ = s.DeleteUser(got.ID) }()
	if got.TenantID != want {
		t.Fatalf("租户归属被丢弃：写入 %q，读回 %q（JWT 会因此回退 default 租户）", want, got.TenantID)
	}
	byID := s.GetUser(got.ID)
	if byID == nil || byID.TenantID != want {
		t.Fatalf("GetUser 读回租户 = %q，want %q", byID.TenantID, want)
	}
	listed := false
	for _, u := range s.ListUsers() {
		if u.ID == got.ID {
			listed = true
			if u.TenantID != want {
				t.Fatalf("ListUsers 读回租户 = %q，want %q", u.TenantID, want)
			}
		}
	}
	if !listed {
		t.Fatal("ListUsers 未包含新建用户")
	}
}

// TestSeedAdminWritesDefaultTenant 守护 D5 的播种侧：seeded admin 必须显式写入
// DefaultTenantID，否则登录响应 tenantId="" 与 /auth/me 的 "default" 不一致。
// （网关侧 tenantOrDefault 是读侧兜底；本测试钉住写侧，防止新库再次播种空租户。）
func TestSeedAdminWritesDefaultTenant(t *testing.T) {
	src, err := os.ReadFile("mysql.go")
	if err != nil {
		t.Fatalf("读 mysql.go: %v", err)
	}
	code := string(src)
	if !strings.Contains(code, `"INSERT INTO users (id, tenant_id,`) {
		t.Fatal("seed admin INSERT 不含 tenant_id —— 播种会落空租户（D5）")
	}
	if !strings.Contains(code, `"user-admin", DefaultTenantID`) {
		t.Fatal("seed admin 未使用 DefaultTenantID 作为租户值（D5）")
	}
}
