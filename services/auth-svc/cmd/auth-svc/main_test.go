package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/services/auth-svc/internal/auth"
	"github.com/Levango7/OpsMesh/services/auth-svc/internal/store"
	"github.com/Levango7/OpsMesh/services/auth-svc/pkg/config"
)

// seedAdminPassword 是 store 播种的公开弱口令，任何部署下都必须被替换掉。
const seedAdminPassword = "admin123"

func TestRotateDefaultAdminPassword_ExplicitPassword(t *testing.T) {
	st := store.NewMemoryStore()
	cfg := &config.Config{AdminPassword: "Adm1n-Str0ng!Pw"}
	if err := rotateDefaultAdminPassword(st, cfg); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	u := st.GetUserByUsername("admin")
	if !auth.VerifyPassword(u.PasswordHash, "Adm1n-Str0ng!Pw") {
		t.Fatal("AUTH_SVC_ADMIN_PASSWORD 指定的口令应生效")
	}
	if auth.VerifyPassword(u.PasswordHash, seedAdminPassword) {
		t.Fatal("公开弱口令必须失效")
	}
	if !u.MustChangePassword {
		t.Fatal("应保留 MustChangePassword=true（首登强制改密）")
	}
}

func TestRotateDefaultAdminPassword_WeakPasswordRejected(t *testing.T) {
	st := store.NewMemoryStore()
	for _, weak := range []string{"admin123", "short1!A", "alllowercase1!", "NOLOWER1!AA"} {
		err := rotateDefaultAdminPassword(st, &config.Config{AdminPassword: weak})
		if err == nil {
			t.Fatalf("弱口令 %q 应被拒绝", weak)
		}
	}
	// 校验失败不得留下半成品：库内口令仍是原弱口令，未被改写。
	if u := st.GetUserByUsername("admin"); !auth.VerifyPassword(u.PasswordHash, seedAdminPassword) {
		t.Fatal("校验失败时不应改动 admin 口令")
	}
}

func TestRotateDefaultAdminPassword_PasswordFileWritten(t *testing.T) {
	st := store.NewMemoryStore()
	path := filepath.Join(t.TempDir(), "admin-password")
	cfg := &config.Config{AdminPasswordFile: path}
	if err := rotateDefaultAdminPassword(st, cfg); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("口令文件应存在: %v", err)
	}
	password := strings.TrimSpace(string(raw))
	if len(password) != 32 {
		t.Fatalf("随机口令应为 32 字符 hex, got %d", len(password))
	}
	if !auth.VerifyPassword(st.GetUserByUsername("admin").PasswordHash, password) {
		t.Fatal("文件中写入的口令应与库内口令一致")
	}
}

func TestRotateDefaultAdminPassword_MissingDirFails(t *testing.T) {
	st := store.NewMemoryStore()
	path := filepath.Join(t.TempDir(), "no-such-dir", "admin-password")
	if err := rotateDefaultAdminPassword(st, &config.Config{AdminPasswordFile: path}); err == nil {
		t.Fatal("目录不存在时应报错，而非静默把口令写到别处")
	}
}

func TestRotateDefaultAdminPassword_Idempotent(t *testing.T) {
	st := store.NewMemoryStore()
	cfg := &config.Config{}
	if err := rotateDefaultAdminPassword(st, cfg); err != nil {
		t.Fatalf("first: %v", err)
	}
	// 管理员在界面上改了密 → 再次启动不得回滚。
	hash, err := auth.HashPassword("RotatedByAdm1n!x")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := st.ChangePassword("user-admin", hash); err != nil {
		t.Fatalf("change password: %v", err)
	}
	if err := rotateDefaultAdminPassword(st, cfg); err != nil {
		t.Fatalf("second: %v", err)
	}
	u := st.GetUserByUsername("admin")
	if !auth.VerifyPassword(u.PasswordHash, "RotatedByAdm1n!x") {
		t.Fatal("不应覆盖管理员已修改的口令")
	}
}
