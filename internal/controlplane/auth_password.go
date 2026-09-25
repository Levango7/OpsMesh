// auth_password.go 密码哈希 + 首启凭据加固（预置弱口令处置）+ 强口令校验。
package controlplane

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/store"

	"golang.org/x/crypto/bcrypt"
)

// hashPassword 用 bcrypt 哈希密码。
// 使用 cost=12（生产推荐基线，DefaultCost=10 偏低）。
// 注意：现有用户密码哈希可能用 cost=10 生成，bcrypt.CompareHashAndPassword
// 会自动适配不同 cost，因此无需迁移旧哈希；新哈希与改密后哈希均使用 cost=12。
func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// verifyPassword 校验 bcrypt 哈希与明文密码是否匹配。
func verifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// seedUserPasswords 随源码公开的预置口令（账号 ID → 口令）。
// 这些字面量在仓库、文档、镜像里都可见，因此**任何**可登录的部署里它们都等同于公开后门；
// MustChangePassword 也拦不住——持有口令者可以自己走完改密流程拿到正式 token。
// 非 demo 模式下 enforceInitialCredentials 必须把它们全部清除。
var seedUserPasswords = map[string]string{
	"user-admin":    "admin123",
	"user-operator": "operator123",
	"user-viewer":   "viewer123",
}

// randomPassword 生成 16 字节 hex（32 字符）随机口令，crypto/rand 密码学安全。
func randomPassword() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// enforceInitialCredentials 在非 demo 模式下加固首启凭据，返回致命错误时调用方应中止启动。
//
// 解决两个叠加的生产事故：
//  1. 内置 admin 的公开弱口令必须被替换，但替换后的口令必须交得到运维手里——此前实现
//     只是生成随机口令后不输出、不落盘、也没有对应 flag，导致管理员被静默锁死（既进不去、也无恢复路径）。
//     现按 cfg.AdminPassword / cfg.AdminPasswordFile 交付；生产两者皆空时 fail-fast 并给出可执行指引。
//  2. operator/viewer 的公开弱口令此前完全不处理（demo 之外的部署同样可登录），
//     现一律替换为随机不可知口令，账号保留给管理员删除或重建。
//
// 幂等性：口令替换只在账号当前口令仍等于公开弱口令时发生（bcrypt 比对命中），
// 因此 SQLStore 重启后不会覆盖管理员已修改的口令；MemoryStore 每次启动都是新实例，每次都重置。
// AdminPasswordForceReset=true 时 admin 口令按 cfg.AdminPassword 强制覆盖（口令遗失后的恢复手段）。
func enforceInitialCredentials(cfg *config.Config, st store.Store) error {
	if err := deliverAdminCredential(cfg, st); err != nil {
		return err
	}
	revokeSeedCredentials(st)
	return nil
}

// deliverAdminCredential 处置内置 admin 的公开弱口令，并保证新口令有交付通道。
func deliverAdminCredential(cfg *config.Config, st store.Store) error {
	u := st.GetUserByUsername("admin")
	if u == nil {
		return nil
	}
	seedPw := seedUserPasswords["user-admin"]
	isSeed := verifyPassword(u.PasswordHash, seedPw)
	// 非首启（口令已非公开弱口令）：默认尊重现状。仅显式开启恢复开关时用 cfg.AdminPassword 覆盖，
	// 避免运维残留的配置在每次重启时静默回滚管理员在界面上做过的改密。
	if !isSeed && !(cfg.AdminPasswordForceReset && cfg.AdminPassword != "") {
		return nil
	}
	password := cfg.AdminPassword
	generated := false
	if password != "" {
		if msg := validateStrongPassword(password); msg != "" {
			return fmt.Errorf("--admin-password（或 OPSMESH_ADMIN_PASSWORD）不满足强口令要求: %s", msg)
		}
		if password == seedPw {
			return fmt.Errorf("--admin-password（或 OPSMESH_ADMIN_PASSWORD）不能等于公开的预置弱口令 %q", seedPw)
		}
	} else {
		p, err := randomPassword()
		if err != nil {
			return fmt.Errorf("生成随机 admin 口令失败: %w", err)
		}
		password = p
		generated = true
		// 交付通道：文件优先；生产无任何通道时拒绝启动（静默锁死是最坏结果，宁可启动失败）。
		switch {
		case cfg.AdminPasswordFile != "":
			if err := writeAdminPasswordFile(cfg.AdminPasswordFile, password); err != nil {
				return err
			}
			log.Printf("[controlplane] 初始 admin 口令已写入 %s（权限 0600，内含明文，请读取后妥善保管并删除该文件）", cfg.AdminPasswordFile)
		case cfg.Production:
			return fmt.Errorf("生产模式（--production=true）检测到内置 admin 仍为公开弱口令 admin123，" +
				"但未提供初始口令交付通道：请设置 OPSMESH_ADMIN_PASSWORD（推荐，可用 Secret 注入）" +
				"或 --admin-password-file 后重启；否则替换出的随机口令无人知晓，管理员将无法登录")
		default:
			// 非生产（含本地开发）：打印到 stderr，避免重蹈「口令随机生成但无人知道」的锁死。
			log.Printf("[controlplane] 警告：非生产模式未提供初始口令通道，已生成随机 admin 口令：" +
				"（请复制保存；生产模式请改用 OPSMESH_ADMIN_PASSWORD 或 --admin-password-file）")
			fmt.Fprintf(os.Stderr, "[controlplane] 初始 admin 口令（仅本地开发/调试可见）: %s\n", password)
		}
	}
	hash, err := hashPassword(password)
	if err != nil {
		return fmt.Errorf("哈希 admin 口令失败: %w", err)
	}
	// ChangePassword 写入新哈希但会清除 MustChangePassword 标记。
	if !st.ChangePassword(u.ID, hash) {
		return fmt.Errorf("更新 admin 口令失败（用户 %s 不存在或存储不可写）", u.ID)
	}
	// 恢复 MustChangePassword=true（首登仍强制改密），同时保留原有 email/roles/status。
	cp := *u
	cp.MustChangePassword = true
	st.UpdateUser(&cp)
	if generated {
		log.Printf("[controlplane] 安全提示：内置 admin 的公开弱口令已替换（首登仍须改密）。")
	} else {
		log.Printf("[controlplane] 安全提示：admin 口令已按 --admin-password 设置（口令遗失后的强制重置；首登仍须改密）。")
	}
	return nil
}

// revokeSeedCredentials 把仍是公开弱口令的预置账号替换为随机不可知口令。
// 账号本身保留（角色/引用不失效），但已无法登录；管理员可用 admin 身份在用户管理里删除或重建它们。
func revokeSeedCredentials(st store.Store) {
	for id, seedPw := range seedUserPasswords {
		if id == "user-admin" {
			continue // admin 由 deliverAdminCredential 处理（需交付通道）
		}
		u := st.GetUser(id)
		if u == nil || !verifyPassword(u.PasswordHash, seedPw) {
			continue
		}
		pw, err := randomPassword()
		if err != nil {
			log.Printf("[controlplane] 生成随机口令失败，预置账号 %s 仍为公开弱口令: %v", u.Username, err)
			continue
		}
		hash, err := hashPassword(pw)
		if err != nil {
			log.Printf("[controlplane] 哈希随机口令失败，预置账号 %s 仍为公开弱口令: %v", u.Username, err)
			continue
		}
		if !st.ChangePassword(u.ID, hash) {
			log.Printf("[controlplane] 替换预置账号 %s 口令失败，该账号仍为公开弱口令，请尽快删除或改密", u.Username)
			continue
		}
		cp := *u
		cp.MustChangePassword = true
		st.UpdateUser(&cp)
		log.Printf("[controlplane] 安全提示：预置账号 %q 的公开弱口令已替换为随机口令（该账号已不可登录）；"+
			"如需继续使用，请以 admin 身份删除后重建", u.Username)
	}
}

// writeAdminPasswordFile 把口令写入 path（权限 0600，先截断再写）。
// 目录不存在时报错而非静默创建——配置写错的路径应该被发现，而不是把口令写到意料之外的位置。
func writeAdminPasswordFile(path, password string) error {
	dir := filepath.Dir(path)
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("--admin-password-file 目录不可用 %s: %w", dir, err)
	}
	if err := os.WriteFile(path, []byte(password+"\n"), 0o600); err != nil {
		return fmt.Errorf("写入 --admin-password-file %s 失败: %w", path, err)
	}
	// Windows 无法用 POSIX 权限位表达 0600（os.Chmod 只映射只读位），
	// 生产环境是 Linux，但开发机 Windows 上这个差异必须说清楚，避免误以为文件已受保护。
	if runtime.GOOS == "windows" {
		log.Printf("[controlplane] 警告：当前为 Windows，文件权限位不生效（见上方 0600 仅为 POSIX 语义）；" +
			"该口令文件仅受目录 ACL 保护，请读取后立即删除")
	}
	return nil
}

// ============================================================================
// 强口令校验（安全债）：跨域 helper，供 auth_login.go 与 auth_users.go 共用。
// ============================================================================

// validateStrongPassword 强口令校验（安全债）：至少 8 字符，包含大小写字母与数字。
// 返回不满足时的可读提示（满足返回空串）。
func validateStrongPassword(pw string) string {
	if len(pw) < changePasswordMinLen {
		return "password too short (min 8 chars)"
	}
	var hasUpper, hasLower, hasDigit bool
	for _, c := range pw {
		switch {
		case c >= 'A' && c <= 'Z':
			hasUpper = true
		case c >= 'a' && c <= 'z':
			hasLower = true
		case c >= '0' && c <= '9':
			hasDigit = true
		}
	}
	if !hasUpper {
		return "password must contain at least one uppercase letter"
	}
	if !hasLower {
		return "password must contain at least one lowercase letter"
	}
	if !hasDigit {
		return "password must contain at least one digit"
	}
	return ""
}
