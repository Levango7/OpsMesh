// password_test.go credentials 包的凭证生命周期用例（TD-87 批 1：自父包 auth_extra_test.go 拆出，
// 连同断言助手 assertSeedCredentialsRevoked——按「测试与包同住」的既有惯例）。
package credentials

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/store"
)

// assertSeedCredentialsRevoked 断言公开预置弱口令在非 demo 部署下已不可登录。
func assertSeedCredentialsRevoked(t *testing.T, st store.Store) {
	t.Helper()
	for _, id := range []string{"user-operator", "user-viewer"} {
		u := st.GetUser(id)
		if u == nil {
			t.Fatalf("预置账号 %s 不应被删除（保留供管理员处置）", id)
		}
		if VerifyPassword(u.PasswordHash, SeedUserPasswords[id]) {
			t.Fatalf("预置账号 %s 仍可用公开弱口令 %q 登录", id, SeedUserPasswords[id])
		}
	}
}

func TestEnforceInitialCredentials_NonProductionRotatesAllSeeds(t *testing.T) {
	st := store.NewMemoryStore()
	cfg := &config.Config{} // 非生产、无交付通道 → 打印到 stderr
	if err := EnforceInitialCredentials(cfg, st); err != nil {
		t.Fatalf("EnforceInitialCredentials: %v", err)
	}
	admin := st.GetUserByUsername("admin")
	if admin == nil {
		t.Fatal("admin 用户应存在")
	}
	// admin 弱口令必须失效，且仍保留首登强制改密标记。
	if VerifyPassword(admin.PasswordHash, "admin123") {
		t.Fatal("admin 仍可用公开弱口令 admin123 登录")
	}
	if !admin.MustChangePassword {
		t.Fatal("admin 应保留 MustChangePassword=true（首登强制改密）")
	}
	assertSeedCredentialsRevoked(t, st)
}

func TestEnforceInitialCredentials_ExplicitAdminPassword(t *testing.T) {
	st := store.NewMemoryStore()
	cfg := &config.Config{AdminPassword: "Str0ngPass1"}
	if err := EnforceInitialCredentials(cfg, st); err != nil {
		t.Fatalf("EnforceInitialCredentials: %v", err)
	}
	admin := st.GetUserByUsername("admin")
	if !VerifyPassword(admin.PasswordHash, "Str0ngPass1") {
		t.Fatal("--admin-password 指定的口令应生效")
	}
}

func TestEnforceInitialCredentials_WeakAdminPasswordRejected(t *testing.T) {
	st := store.NewMemoryStore()
	for _, weak := range []string{"short1A", "alllower1", "ALLUPPER1", "NoDigitsHere"} {
		if err := EnforceInitialCredentials(&config.Config{AdminPassword: weak}, st); err == nil {
			t.Fatalf("弱口令 %q 应被拒绝", weak)
		}
	}
	// 拒绝后不应留下半成品状态：admin 口令仍是原弱口令（未写入）。
	if admin := st.GetUserByUsername("admin"); !VerifyPassword(admin.PasswordHash, "admin123") {
		t.Fatal("校验失败时不应改动 admin 口令")
	}
}

func TestEnforceInitialCredentials_ProductionWithoutChannelFails(t *testing.T) {
	st := store.NewMemoryStore()
	err := EnforceInitialCredentials(&config.Config{Production: true}, st)
	if err == nil {
		t.Fatal("生产模式无口令交付通道应 fail-fast（否则管理员被静默锁死）")
	}
	// 报错必须给出可执行指引，而非只报错不给出路。
	if !strings.Contains(err.Error(), "OPSMESH_ADMIN_PASSWORD") || !strings.Contains(err.Error(), "--admin-password-file") {
		t.Fatalf("错误信息应提示交付通道, got: %v", err)
	}
}

func TestEnforceInitialCredentials_PasswordFileWritten(t *testing.T) {
	st := store.NewMemoryStore()
	path := filepath.Join(t.TempDir(), "admin-password")
	if err := EnforceInitialCredentials(&config.Config{Production: true, AdminPasswordFile: path}, st); err != nil {
		t.Fatalf("EnforceInitialCredentials: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("口令文件应存在: %v", err)
	}
	// 权限 0600：口令明文不得对同机其他用户可读（Windows 下不校验 POSIX 位）。
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("口令文件权限 = %v, want 0600", info.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	password := strings.TrimSpace(string(raw))
	if len(password) != 32 {
		t.Fatalf("随机口令应为 32 字符 hex, got %d", len(password))
	}
	if !VerifyPassword(st.GetUserByUsername("admin").PasswordHash, password) {
		t.Fatal("文件中写入的口令应与库内口令一致")
	}
}

func TestEnforceInitialCredentials_MissingDirFails(t *testing.T) {
	st := store.NewMemoryStore()
	path := filepath.Join(t.TempDir(), "no-such-dir", "admin-password")
	if err := EnforceInitialCredentials(&config.Config{AdminPasswordFile: path}, st); err == nil {
		t.Fatal("目录不存在时应报错，而非静默把口令写到别处或直接锁死")
	}
}

func TestEnforceInitialCredentials_Idempotent(t *testing.T) {
	st := store.NewMemoryStore()
	cfg := &config.Config{AdminPassword: "Str0ngPass1"}
	if err := EnforceInitialCredentials(cfg, st); err != nil {
		t.Fatalf("first: %v", err)
	}
	// 管理员在界面上改了密。
	hash, err := HashPassword("RotatedByAdmin9")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !st.ChangePassword("user-admin", hash) {
		t.Fatal("change password failed")
	}
	// 再次启动（ForceReset=false）：不得回滚管理员的新口令。
	if err := EnforceInitialCredentials(cfg, st); err != nil {
		t.Fatalf("second: %v", err)
	}
	if !VerifyPassword(st.GetUser("user-admin").PasswordHash, "RotatedByAdmin9") {
		t.Fatal("默认配置不得覆盖管理员已修改的口令（避免重启静默回滚）")
	}
}

func TestEnforceInitialCredentials_ForceResetRecovers(t *testing.T) {
	st := store.NewMemoryStore()
	cfg := &config.Config{AdminPassword: "Recovered9Pass", AdminPasswordForceReset: true}
	if err := EnforceInitialCredentials(cfg, st); err != nil {
		t.Fatalf("first: %v", err)
	}
	// 模拟管理员忘记口令（库内为随机口令）：强制重置应把口令改回配置值。
	if err := EnforceInitialCredentials(cfg, st); err != nil {
		t.Fatalf("second: %v", err)
	}
	if !VerifyPassword(st.GetUser("user-admin").PasswordHash, "Recovered9Pass") {
		t.Fatal("--admin-password-force-reset 应覆盖已有 admin 口令")
	}
}

func TestEnforceInitialCredentials_NoAdminUser(t *testing.T) {
	st := store.NewMemoryStore()
	if u := st.GetUserByUsername("admin"); u != nil {
		st.DeleteUser(u.ID)
	}
	// 无 admin 账号时不得报错（部署方自行管理账号），但预置弱口令账号仍须被处置。
	if err := EnforceInitialCredentials(&config.Config{Production: true}, st); err != nil {
		t.Fatalf("无 admin 账号不应阻断启动: %v", err)
	}
	assertSeedCredentialsRevoked(t, st)
}

// =============================================================================
// randHexID
// =============================================================================
