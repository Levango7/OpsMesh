// sql_rbac.go 实现 SQLStore 的 UserStore / RoleStore / PermissionStore 三个子接口（生产就绪）。
//
// 生产 HA 模式强制 --store=mysql（config.Validate 拒绝 memory+replicas>1），用户中心
// （登录/注册/用户角色管理）必须在此真实落地，否则控制面一碰鉴权即 panic。
//
// 表结构：users / roles / permissions；角色权限与用户角色绑定以 JSON 文本列存储
// （复用 ci_items.attrs 的 JSON 范式）。seedRBAC 幂等预置与 MemoryStore 完全一致的
// 默认权限/角色/用户，保证 mysql 后端开箱可用、多副本共享同一 MySQL 时身份一致。
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// rowScanner 兼容 *sql.Row 与 *sql.Rows 的 Scan 接口。
type rowScanner interface {
	Scan(dest ...interface{}) error
}

// scanUser 从一行扫描出 *User（role_ids 为 JSON 文本列）。无行或扫描失败返回 nil。
// 安全债：扫描 must_change_password 列（旧库无此列时回退 false，向后兼容）。
// 租户隔离：扫描 tenant_id 列（迁移 018 补列；空/NULL 归一为 default）。
func scanUser(row rowScanner) *User {
	var u User
	// users 的 email/password_hash/status/role_ids/created_at 均可空（migrations/001
	// 未声明 NOT NULL；tenant_id 是 migration 018 补列）。裸目标扫描遇 NULL 会让
	// **整行读不出来**——该用户直接查不到，表现为「账号不存在」而无法登录/改密。
	var email, passwordHash, status sql.NullString
	var roleIDsJSON []byte
	var createdAt sql.NullTime
	var tenantID sql.NullString
	if err := row.Scan(&u.ID, &u.Username, &email, &passwordHash, &status, &roleIDsJSON, &createdAt, &u.MustChangePassword, &tenantID); err != nil {
		return nil
	}
	u.Email, u.PasswordHash, u.Status = email.String, passwordHash.String, status.String
	u.CreatedAt = createdAt.Time
	u.TenantID = normalizeTenantID(strings.TrimSpace(tenantID.String))
	if len(roleIDsJSON) > 0 {
		if err := json.Unmarshal(roleIDsJSON, &u.RoleIDs); err != nil {
			recordStoreFailure("store: scanUser 解析 role_ids JSON 失败 (user=%s): %v", u.ID, err)
		}
	}
	return &u
}

// scanRole 从一行扫描出 *Role（permissions 为 JSON 文本列）。
func scanRole(row rowScanner) *Role {
	var r Role
	// roles 的 description/permissions/created_at 均可空；permissions 是 JSON 列，
	// NULL 与空数组语义不同但都读回「无权限」，裸目标扫描会让 NULL 吞掉整行（角色查不到）。
	var description sql.NullString
	var permsJSON []byte
	var createdAt sql.NullTime
	if err := row.Scan(&r.ID, &r.Name, &description, &permsJSON, &createdAt); err != nil {
		return nil
	}
	r.Description = description.String
	r.CreatedAt = createdAt.Time
	if len(permsJSON) > 0 {
		if err := json.Unmarshal(permsJSON, &r.Permissions); err != nil {
			recordStoreFailure("store: scanRole 解析 permissions JSON 失败 (role=%s): %v", r.ID, err)
		}
	}
	return &r
}

// ============================================================================
// UserStore：用户中心用户领域（6 方法）
// ============================================================================

// userColumns users 表查询的列列表（含 must_change_password，安全债；tenant_id 由迁移 018 保证）。
const userColumns = `id, username, email, password_hash, status, role_ids, created_at, must_change_password, tenant_id`

// GetUser 按 ID 返回单用户（不存在返回 nil）。
func (s *SQLStore) GetUser(id string) *User {
	row := s.db.QueryRowContext(context.Background(),
		`SELECT `+userColumns+` FROM users WHERE id=?`, id)
	return scanUser(row)
}

// GetUserByUsername 按用户名返回单用户（登录用；不存在返回 nil）。
func (s *SQLStore) GetUserByUsername(username string) *User {
	row := s.db.QueryRowContext(context.Background(),
		`SELECT `+userColumns+` FROM users WHERE username=?`, username)
	return scanUser(row)
}

// ListUsers 返回全部用户（按创建时间升序）。
func (s *SQLStore) ListUsers() []*User {
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT `+userColumns+` FROM users ORDER BY created_at ASC`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := make([]*User, 0)
	for rows.Next() {
		if u := scanUser(rows); u != nil {
			out = append(out, u)
		}
	}
	if err := rows.Err(); err != nil {
		recordStoreFailure("[store] ListUsers 遍历失败: %v", err)
	}
	return out
}

// CreateUser 创建用户。用户名重复时返回 nil（调用方据此判断冲突）。
// 入参 u.ID / u.PasswordHash 已由调用方填充（ID 默认随机、密码已 bcrypt 哈希）。
func (s *SQLStore) CreateUser(u *User) *User {
	// 用户名重复校验（唯一索引兜底，INSERT 失败也返回 nil）。
	if s.GetUserByUsername(u.Username) != nil {
		return nil
	}
	roleIDs, _ := json.Marshal(u.RoleIDs)
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO users (id, username, email, password_hash, status, role_ids, created_at, must_change_password, tenant_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Username, u.Email, u.PasswordHash, u.Status, roleIDs, time.Now().UTC(), u.MustChangePassword, normalizeTenantID(u.TenantID)); err != nil {
		return nil
	}
	u.TenantID = normalizeTenantID(u.TenantID)
	return u
}

// UpdateUser 更新用户 email/roles/status/must_change_password/tenant_id（按 u.ID 定位）。不存在返回 false。
// PasswordHash 不可经此方法修改（避免误覆盖登录凭据，改密走 ChangePassword）。
// 租户：非空时覆盖（空值视为「本次不改租户」）；实际租户迁移须调用方先经权限判定。
func (s *SQLStore) UpdateUser(u *User) bool {
	roleIDs, _ := json.Marshal(u.RoleIDs)
	res, err := s.db.ExecContext(context.Background(),
		`UPDATE users SET email=?, role_ids=?, status=?, must_change_password=?, tenant_id=COALESCE(NULLIF(?, ''), tenant_id) WHERE id=?`,
		u.Email, roleIDs, u.Status, u.MustChangePassword, u.TenantID, u.ID)
	if err != nil {
		return false
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		return false
	}
	return n > 0
}

// ChangePassword 改密（安全债）：写入新 bcrypt 哈希并清除 must_change_password 标记。
// 与 UpdateUser 分离，避免误覆盖 PasswordHash。用户不存在返回 false。
func (s *SQLStore) ChangePassword(userID, newPasswordHash string) bool {
	res, err := s.db.ExecContext(context.Background(),
		`UPDATE users SET password_hash=?, must_change_password=0 WHERE id=?`,
		newPasswordHash, userID)
	if err != nil {
		return false
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		return false
	}
	return n > 0
}

// DeleteUser 按 ID 删除用户。不存在返回 false。
func (s *SQLStore) DeleteUser(id string) bool {
	res, err := s.db.ExecContext(context.Background(), `DELETE FROM users WHERE id=?`, id)
	if err != nil {
		return false
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		return false
	}
	return n > 0
}

// ============================================================================
// RoleStore：角色领域（5 方法）
// ============================================================================

// GetRole 按 ID 返回单角色（不存在返回 nil）。
func (s *SQLStore) GetRole(id string) *Role {
	row := s.db.QueryRowContext(context.Background(),
		`SELECT id, name, description, permissions, created_at FROM roles WHERE id=?`, id)
	return scanRole(row)
}

// ListRoles 返回全部角色（按创建时间升序）。
func (s *SQLStore) ListRoles() []*Role {
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT id, name, description, permissions, created_at FROM roles ORDER BY created_at ASC`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := make([]*Role, 0)
	for rows.Next() {
		if r := scanRole(rows); r != nil {
			out = append(out, r)
		}
	}
	if err := rows.Err(); err != nil {
		recordStoreFailure("[store] ListRoles 遍历失败: %v", err)
	}
	return out
}

// CreateRole 创建角色。角色名重复时返回 nil。
func (s *SQLStore) CreateRole(r *Role) *Role {
	if s.GetRole(r.ID) != nil {
		return nil
	}
	perms, _ := json.Marshal(r.Permissions)
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO roles (id, name, description, permissions, created_at) VALUES (?, ?, ?, ?, ?)`,
		r.ID, r.Name, r.Description, perms, time.Now().UTC()); err != nil {
		return nil
	}
	return r
}

// UpdateRole 更新角色 description/permissions（按 r.ID 定位）。不存在返回 false。
func (s *SQLStore) UpdateRole(r *Role) bool {
	perms, _ := json.Marshal(r.Permissions)
	res, err := s.db.ExecContext(context.Background(),
		`UPDATE roles SET description=?, permissions=? WHERE id=?`, r.Description, perms, r.ID)
	if err != nil {
		return false
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		return false
	}
	return n > 0
}

// DeleteRole 按 ID 删除角色。不存在返回 false。
func (s *SQLStore) DeleteRole(id string) bool {
	res, err := s.db.ExecContext(context.Background(), `DELETE FROM roles WHERE id=?`, id)
	if err != nil {
		return false
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		return false
	}
	return n > 0
}

// ============================================================================
// PermissionStore：权限领域（1 方法，只读）
// ============================================================================

// ListPermissions 返回全部预定义权限（按组/名排序）。
func (s *SQLStore) ListPermissions() []*Permission {
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT id, name, description, group_name FROM permissions ORDER BY group_name, name`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := make([]*Permission, 0)
	for rows.Next() {
		var p Permission
		// permissions 表的 description/group_name 未声明 NOT NULL，
		// 裸 string 目标遇 NULL 会让整行读不出来（权限项从清单里消失）。
		var description, groupName sql.NullString
		if err := rows.Scan(&p.ID, &p.Name, &description, &groupName); err != nil {
			continue
		}
		p.Description, p.Group = description.String, groupName.String
		out = append(out, &p)
	}
	if err := rows.Err(); err != nil {
		recordStoreFailure("[store] ListPermissions 遍历失败: %v", err)
	}
	return out
}

// ============================================================================
// seedRBAC 幂等预置默认权限/角色/用户（与 MemoryStore.seedRBAC 完全一致）。
// ============================================================================

// seedRBAC 在 initSchema 末尾调用，幂等写入默认权限/角色/用户。
func (s *SQLStore) seedRBAC(ctx context.Context) error {
	// 1. 权限目录。
	for i, ps := range rbacPermSpecs {
		pid := fmt.Sprintf("perm-%s-%02d", ps.Group, i+1)
		if _, err := s.db.ExecContext(ctx,
			`INSERT IGNORE INTO permissions (id, name, description, group_name) VALUES (?, ?, ?, ?)`,
			pid, ps.Name, ps.Desc, ps.Group); err != nil {
			return err
		}
	}
	// 2. 角色权限集合（与 memory.go 的计算逻辑一致）。
	// 角色→权限映射统一取自 RolePermissions()，避免与 RBAC 闸逻辑漂移。
	rp := RolePermissions()
	allPerms := rp["admin"]
	viewerPerms := rp["viewer"]
	operatorPerms := rp["operator"]
	now := time.Now().UTC()
	roles := []struct {
		id, name, desc string
		perms          []string
	}{
		{"role-admin", "admin", "超级管理员，拥有所有权限", append([]string{}, allPerms...)},
		{"role-operator", "operator", "运维人员，可操作设备/任务/告警/部署等，不含删除权限", operatorPerms},
		{"role-viewer", "viewer", "只读用户，仅可查看各类资源", viewerPerms},
	}
	for _, r := range roles {
		perms, _ := json.Marshal(r.perms)
		if _, err := s.db.ExecContext(ctx,
			`INSERT IGNORE INTO roles (id, name, description, permissions, created_at) VALUES (?, ?, ?, ?, ?)`,
			r.id, r.name, r.desc, perms, now); err != nil {
			return err
		}
		// 权限目录补种只对「新库」够用；对老库，角色行首建后 INSERT IGNORE 永不再动——
		// 目录里新增的权限点永远到不了预置角色（2026-09-27 实测：0.9.0 建库 → 升级重启，
		// role-admin 的权限快照停在 86 项、缺 diagnostics:execute/dump，用户中心登录的
		// admin 调 /api/v1/admin/* 全部 403）。此处对预置角色做**并集回填**：只加不减，
		// 管理员经角色管理 API 的自定义授予不会被清掉；无变化则不写。
		changed, err := s.refreshPresetRolePermissions(ctx, r.id, r.perms)
		if err != nil {
			return err
		}
		if changed {
			log.Printf("[store] 预置角色 %s 的权限快照落后于权限目录，已按并集回填", r.id)
		}
	}
	// 3. 默认用户（bcrypt 哈希；与 memory.go 保持一致）。
	// 安全债：预置弱口令首登强制改密（must_change_password=1）。老库升级后仍用预置口令的账号
	// 也会被标记——但**只标一次**：判定依据是"该行的哈希是否仍等于预置口令"，见循环内注释。
	type userSpec struct {
		id, name, password, email string
		roleIDs                   []string
	}
	specs := []userSpec{
		{"user-admin", "admin", "admin123", "admin@opsmesh.local", []string{"role-admin"}},
		{"user-operator", "operator", "operator123", "operator@opsmesh.local", []string{"role-operator"}},
		{"user-viewer", "viewer", "viewer123", "viewer@opsmesh.local", []string{"role-viewer"}},
	}
	for _, us := range specs {
		hash, err := bcryptHash(us.password)
		if err != nil {
			return err
		}
		roleIDs, _ := json.Marshal(us.roleIDs)
		// 已存在的账号**只在仍使用预置口令时**才补标记。
		// 原先是无条件 `ON DUPLICATE KEY UPDATE must_change_password=1`，而 seedRBAC 由
		// runMigrations 在**每次进程启动**调用 ⇒ 客户改过口令之后，任何一次重启或升级都会把
		// must_change_password 复活成 1，登录只能拿到 changePasswordToken、拿不到会话 token
		// （2026-09-26 用真的 v0.9.0 二进制建库 → 升 0.9.2 → 重启，实测复现）。
		var stored string
		qerr := s.db.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id = ?`, us.id).Scan(&stored)
		switch {
		case qerr == nil:
			if bcrypt.CompareHashAndPassword([]byte(stored), []byte(us.password)) == nil {
				if _, err := s.db.ExecContext(ctx,
					`UPDATE users SET must_change_password = 1 WHERE id = ?`, us.id); err != nil {
					return err
				}
			}
		case errors.Is(qerr, sql.ErrNoRows):
			// INSERT IGNORE：多副本同时首启时，后到者撞主键也不报错（保持原 upsert 的并发语义）。
			if _, err := s.db.ExecContext(ctx,
				`INSERT IGNORE INTO users (id, username, email, password_hash, status, role_ids, created_at, must_change_password, tenant_id)
				 VALUES (?, ?, ?, ?, 'active', ?, ?, 1, ?)`,
				us.id, us.name, us.email, string(hash), roleIDs, now, DefaultTenantID); err != nil {
				return err
			}
		default:
			return qerr
		}
	}
	return nil
}

// refreshPresetRolePermissions 把权限点并集回填进一个预置角色的存量权限快照。
// 返回是否发生了写入。存量 JSON 缺失/损坏时按空列表处理（回填为目录并集，
// 比带着坏 JSON 让角色永远 403 好）。
func (s *SQLStore) refreshPresetRolePermissions(ctx context.Context, roleID string, computed []string) (bool, error) {
	var storedJSON string
	err := s.db.QueryRowContext(ctx, `SELECT permissions FROM roles WHERE id = ?`, roleID).Scan(&storedJSON)
	if err != nil {
		return false, fmt.Errorf("read role %s permissions: %w", roleID, err)
	}
	var stored []string
	if storedJSON != "" {
		if err := json.Unmarshal([]byte(storedJSON), &stored); err != nil {
			log.Printf("[store] 角色 %s 的 permissions 不是合法 JSON（%v），按空快照回填", roleID, err)
			stored = nil
		}
	}
	merged, addedCount := mergePermissionLists(stored, computed)
	if addedCount == 0 {
		return false, nil
	}
	mergedJSON, err := json.Marshal(merged)
	if err != nil {
		return false, fmt.Errorf("marshal merged permissions for %s: %w", roleID, err)
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE roles SET permissions = ? WHERE id = ?`, mergedJSON, roleID); err != nil {
		return false, fmt.Errorf("update role %s permissions: %w", roleID, err)
	}
	return true, nil
}

// mergePermissionLists 返回 stored ∪ computed 的去重排序结果与新增个数。
// 只加不减：computed 里消失的条目不动（权限点下线属于显式迁移的职责，不能靠
// 每次启动的重算悄悄吊销管理员手动授予的权限）。
func mergePermissionLists(stored, computed []string) ([]string, int) {
	seen := make(map[string]bool, len(stored)+len(computed))
	merged := make([]string, 0, len(stored)+len(computed))
	for _, p := range stored {
		if p != "" && !seen[p] {
			seen[p] = true
			merged = append(merged, p)
		}
	}
	added := 0
	for _, p := range computed {
		if p != "" && !seen[p] {
			seen[p] = true
			merged = append(merged, p)
			added++
		}
	}
	sort.Strings(merged)
	return merged, added
}
