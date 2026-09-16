// auth_password.go 密码哈希 + 默认 admin 弱口令轮换 + 强口令校验。
package controlplane

import (
	"crypto/rand"
	"encoding/hex"
	"log"

	"opsmesh/internal/store"

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

// rotateDefaultAdminPassword 在非 demo 模式下，若默认 admin 仍使用弱口令 "admin123"，
// 则生成随机密码替换并打印到日志（一次性提示管理员复制）。返回是否执行了替换。
//
// 安全加固：默认 admin 弱口令 "admin123" 靠 mustChangePassword 兜底，但若管理员
// 忽略改密提示，弱口令将持续可登。改为首次启动时生成随机口令（16 字节 hex），即使管理员
// 不改密，攻击者也无法用已知弱口令登录。随机密码仅打印一次到日志，须妥善保管。
//
// 幂等性：仅当 admin 当前密码仍是 "admin123"（bcrypt 比对命中）时才重置，避免覆盖管理员
// 已修改的密码。MemoryStore 每次启动都是新实例（admin 始终是 admin123），每次都重置；
// SQLStore 持久化，重启后 admin 已是随机口令，bcrypt 比对不命中，不重复重置。
//
// 保留 MustChangePassword=true：ChangePassword 会清除该标记，随后用 UpdateUser 恢复，
// 确保首登仍强制改密（与安全债一致）。
func rotateDefaultAdminPassword(st store.Store) bool {
	u := st.GetUserByUsername("admin")
	if u == nil {
		return false
	}
	// 仅当仍是默认弱口令 admin123 时才重置（避免覆盖管理员已改的密码）。
	if !verifyPassword(u.PasswordHash, "admin123") {
		return false
	}
	// 生成随机密码：16 字节 hex（32 字符，crypto/rand 密码学安全）。
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		log.Printf("[controlplane] 生成随机 admin 密码失败: %v", err)
		return false
	}
	password := hex.EncodeToString(b)
	hash, err := hashPassword(password)
	if err != nil {
		log.Printf("[controlplane] 哈希随机 admin 密码失败: %v", err)
		return false
	}
	// ChangePassword 写入新哈希但会清除 MustChangePassword 标记。
	if !st.ChangePassword(u.ID, hash) {
		log.Printf("[controlplane] 更新 admin 随机密码失败")
		return false
	}
	// 恢复 MustChangePassword=true（首登仍强制改密），同时保留原有 email/roles/status。
	cp := *u
	cp.MustChangePassword = true
	st.UpdateUser(&cp)
	// 一次性打印随机密码到日志，提示管理员复制（后续重启不重复打印，因密码已非 admin123）。
	log.Printf("[controlplane] ============================================================")
	log.Printf("[controlplane] 安全提示：默认 admin 密码已替换为随机口令（首登仍须改密）。")
	log.Printf("[controlplane]   一次性随机密码（请立即复制并登录后修改）: %s", password)
	log.Printf("[controlplane] ============================================================")
	return true
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
