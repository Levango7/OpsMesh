// sql_rbac.go 实现 SQLStore 的 UserStore / RoleStore / PermissionStore 三个子接口（P0-1 生产就绪）。
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
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// rowScanner 兼容 *sql.Row 与 *sql.Rows 的 Scan 接口。
type rowScanner interface {
	Scan(dest ...interface{}) error
}

// scanUser 从一行扫描出 *User（role_ids 为 JSON 文本列）。无行或扫描失败返回 nil。
func scanUser(row rowScanner) *User {
	var u User
	var roleIDsJSON []byte
	var createdAt time.Time
	if err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Status, &roleIDsJSON, &createdAt); err != nil {
		return nil
	}
	u.CreatedAt = createdAt
	if len(roleIDsJSON) > 0 {
		_ = json.Unmarshal(roleIDsJSON, &u.RoleIDs)
	}
	return &u
}

// scanRole 从一行扫描出 *Role（permissions 为 JSON 文本列）。
func scanRole(row rowScanner) *Role {
	var r Role
	var permsJSON []byte
	var createdAt time.Time
	if err := row.Scan(&r.ID, &r.Name, &r.Description, &permsJSON, &createdAt); err != nil {
		return nil
	}
	r.CreatedAt = createdAt
	if len(permsJSON) > 0 {
		_ = json.Unmarshal(permsJSON, &r.Permissions)
	}
	return &r
}

// ============================================================================
// UserStore：用户中心用户领域（6 方法）
// ============================================================================

// GetUser 按 ID 返回单用户（不存在返回 nil）。
func (s *SQLStore) GetUser(id string) *User {
	row := s.db.QueryRowContext(context.Background(),
		`SELECT id, username, email, password_hash, status, role_ids, created_at FROM users WHERE id=?`, id)
	return scanUser(row)
}

// GetUserByUsername 按用户名返回单用户（登录用；不存在返回 nil）。
func (s *SQLStore) GetUserByUsername(username string) *User {
	row := s.db.QueryRowContext(context.Background(),
		`SELECT id, username, email, password_hash, status, role_ids, created_at FROM users WHERE username=?`, username)
	return scanUser(row)
}

// ListUsers 返回全部用户（按创建时间升序）。
func (s *SQLStore) ListUsers() []*User {
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT id, username, email, password_hash, status, role_ids, created_at FROM users ORDER BY created_at ASC`)
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
		`INSERT INTO users (id, username, email, password_hash, status, role_ids, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Username, u.Email, u.PasswordHash, u.Status, roleIDs, time.Now().UTC()); err != nil {
		return nil
	}
	return u
}

// UpdateUser 更新用户 email/roles/status（按 u.ID 定位）。不存在返回 false。
func (s *SQLStore) UpdateUser(u *User) bool {
	roleIDs, _ := json.Marshal(u.RoleIDs)
	res, err := s.db.ExecContext(context.Background(),
		`UPDATE users SET email=?, role_ids=?, status=? WHERE id=?`,
		u.Email, roleIDs, u.Status, u.ID)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// DeleteUser 按 ID 删除用户。不存在返回 false。
func (s *SQLStore) DeleteUser(id string) bool {
	res, err := s.db.ExecContext(context.Background(), `DELETE FROM users WHERE id=?`, id)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
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
	n, _ := res.RowsAffected()
	return n > 0
}

// DeleteRole 按 ID 删除角色。不存在返回 false。
func (s *SQLStore) DeleteRole(id string) bool {
	res, err := s.db.ExecContext(context.Background(), `DELETE FROM roles WHERE id=?`, id)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
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
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Group); err != nil {
			continue
		}
		out = append(out, &p)
	}
	return out
}

// ============================================================================
// seedRBAC 幂等预置默认权限/角色/用户（与 MemoryStore.seedRBAC 完全一致）。
// ============================================================================

// rbacPermSpecs 默认权限定义（与 memory.go 保持同步）。
var rbacPermSpecs = []struct {
	group string
	name  string
	desc  string
}{
	{"device", "device:read", "查看设备"},
	{"device", "device:write", "操作设备"},
	{"device", "device:delete", "退役设备"},
	{"task", "task:read", "查看任务"},
	{"task", "task:write", "下发任务"},
	{"task", "task:cancel", "取消任务"},
	{"alert", "alert:read", "查看告警"},
	{"alert", "alert:ack", "确认告警"},
	{"alert", "alert:silence", "静默告警"},
	{"cmdb", "cmdb:read", "查看配置项"},
	{"cmdb", "cmdb:write", "编辑配置项"},
	{"deploy", "deploy:read", "查看部署"},
	{"deploy", "deploy:write", "执行部署"},
	{"workflow", "workflow:read", "查看工作流"},
	{"workflow", "workflow:write", "编辑工作流"},
	{"log", "log:read", "查看日志"},
	{"audit", "audit:read", "查看审计"},
	{"user", "user:read", "查看用户"},
	{"user", "user:write", "编辑用户"},
	{"user", "user:delete", "删除用户"},
	{"user", "user:approve", "审批用户注册"},
	{"role", "role:read", "查看角色"},
	{"role", "role:write", "编辑角色"},
	{"role", "role:delete", "删除角色"},
	{"federation", "federation:read", "查看联邦"},
	{"federation", "federation:write", "编辑联邦"},
}

// seedRBAC 在 initSchema 末尾调用，幂等写入默认权限/角色/用户。
func (s *SQLStore) seedRBAC(ctx context.Context) error {
	// 1. 权限目录。
	for i, ps := range rbacPermSpecs {
		pid := fmt.Sprintf("perm-%s-%02d", ps.group, i+1)
		if _, err := s.db.ExecContext(ctx,
			`INSERT IGNORE INTO permissions (id, name, description, group_name) VALUES (?, ?, ?, ?)`,
			pid, ps.name, ps.desc, ps.group); err != nil {
			return err
		}
	}
	// 2. 角色权限集合（与 memory.go 的计算逻辑一致）。
	allPerms := make([]string, 0, len(rbacPermSpecs))
	for _, ps := range rbacPermSpecs {
		allPerms = append(allPerms, ps.name)
	}
	viewerPerms := make([]string, 0)
	operatorPerms := make([]string, 0)
	operatorGroups := map[string]bool{
		"device": true, "task": true, "alert": true, "cmdb": true,
		"deploy": true, "workflow": true, "log": true, "audit": true,
	}
	for _, p := range allPerms {
		idx := strings.Index(p, ":")
		if idx <= 0 {
			continue
		}
		group, action := p[:idx], p[idx+1:]
		if strings.HasSuffix(p, ":read") {
			viewerPerms = append(viewerPerms, p)
		}
		if operatorGroups[group] && (action == "read" || action == "write") {
			operatorPerms = append(operatorPerms, p)
		}
	}
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
	}
	// 3. 默认用户（bcrypt 哈希；与 memory.go 保持一致）。
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
		hash, err := bcrypt.GenerateFromPassword([]byte(us.password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		roleIDs, _ := json.Marshal(us.roleIDs)
		if _, err := s.db.ExecContext(ctx,
			`INSERT IGNORE INTO users (id, username, email, password_hash, status, role_ids, created_at) VALUES (?, ?, ?, ?, 'active', ?, ?)`,
			us.id, us.name, us.email, string(hash), roleIDs, now); err != nil {
			return err
		}
	}
	return nil
}
