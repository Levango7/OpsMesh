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
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// rowScanner 兼容 *sql.Row 与 *sql.Rows 的 Scan 接口。
type rowScanner interface {
	Scan(dest ...interface{}) error
}

// scanUser 从一行扫描出 *User（role_ids 为 JSON 文本列）。无行或扫描失败返回 nil。
// 安全债：扫描 must_change_password 列（旧库无此列时回退 false，向后兼容）。
func scanUser(row rowScanner) *User {
	var u User
	var roleIDsJSON []byte
	var createdAt time.Time
	if err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Status, &roleIDsJSON, &createdAt, &u.MustChangePassword); err != nil {
		return nil
	}
	u.CreatedAt = createdAt
	if len(roleIDsJSON) > 0 {
		if err := json.Unmarshal(roleIDsJSON, &u.RoleIDs); err != nil {
			log.Printf("store: scanUser 解析 role_ids JSON 失败 (user=%s): %v", u.ID, err)
		}
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
		if err := json.Unmarshal(permsJSON, &r.Permissions); err != nil {
			log.Printf("store: scanRole 解析 permissions JSON 失败 (role=%s): %v", r.ID, err)
		}
	}
	return &r
}

// ============================================================================
// UserStore：用户中心用户领域（6 方法）
// ============================================================================

// userColumns users 表查询的列列表（含 must_change_password，安全债）。
const userColumns = `id, username, email, password_hash, status, role_ids, created_at, must_change_password`

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
		log.Printf("[store] ListUsers 遍历失败: %v", err)
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
		`INSERT INTO users (id, username, email, password_hash, status, role_ids, created_at, must_change_password)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Username, u.Email, u.PasswordHash, u.Status, roleIDs, time.Now().UTC(), u.MustChangePassword); err != nil {
		return nil
	}
	return u
}

// UpdateUser 更新用户 email/roles/status/must_change_password（按 u.ID 定位）。不存在返回 false。
// PasswordHash 不可经此方法修改（避免误覆盖登录凭据，改密走 ChangePassword）。
func (s *SQLStore) UpdateUser(u *User) bool {
	roleIDs, _ := json.Marshal(u.RoleIDs)
	res, err := s.db.ExecContext(context.Background(),
		`UPDATE users SET email=?, role_ids=?, status=?, must_change_password=? WHERE id=?`,
		u.Email, roleIDs, u.Status, u.MustChangePassword, u.ID)
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
		log.Printf("[store] ListRoles 遍历失败: %v", err)
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
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Group); err != nil {
			continue
		}
		out = append(out, &p)
	}
	if err := rows.Err(); err != nil {
		log.Printf("[store] ListPermissions 遍历失败: %v", err)
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
	{"os", "os:read", "查看OS优化模板"},
	{"os", "os:execute", "执行OS优化"},
	{"middleware", "middleware:read", "查看中间件模板"},
	{"middleware", "middleware:execute", "部署/卸载中间件"},
	{"provision", "provision:execute", "自动纳管/纳管设备"},
	{"k8s", "k8s:read", "查看K8s集群"},
	{"k8s", "k8s:write", "管理K8s集群"},
	{"k8s", "k8s:delete", "删除K8s集群"},
	{"ticket", "ticket:read", "查看工单"},
	{"ticket", "ticket:write", "编辑工单"},
	{"slo", "slo:read", "查看SLO"},
	{"slo", "slo:write", "编辑SLO"},
	{"slo", "slo:delete", "删除SLO"},
	{"traffic", "traffic:read", "查看流量策略"},
	{"traffic", "traffic:write", "编辑流量策略"},
	{"pipeline", "pipeline:read", "查看流水线"},
	{"pipeline", "pipeline:write", "编辑流水线"},
	{"argocd", "argocd:read", "查看ArgoCD应用"},
	{"argocd", "argocd:write", "编辑ArgoCD应用"},
	{"compliance", "compliance:read", "查看合规报告"},
	{"compliance", "compliance:write", "执行合规扫描"},
	{"audit", "audit:read", "查询审计事件"},
	{"ha", "ha:read", "查看HA状态"},
	{"ha", "ha:write", "手动切换leader"},
	{"backup", "backup:read", "查看备份记录"},
	{"backup", "backup:write", "创建/恢复/删除备份"},
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
	}
	// 3. 默认用户（bcrypt 哈希；与 memory.go 保持一致）。
	// 安全债：预置弱口令首登强制改密（must_change_password=1）。
	// 用 INSERT ... ON DUPLICATE KEY UPDATE 同步标记，保证老库升级后 admin 也会被标记。
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
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO users (id, username, email, password_hash, status, role_ids, created_at, must_change_password)
			 VALUES (?, ?, ?, ?, 'active', ?, ?, 1)
			 ON DUPLICATE KEY UPDATE must_change_password=1`,
			us.id, us.name, us.email, string(hash), roleIDs, now); err != nil {
			return err
		}
	}
	return nil
}

// RolePermissions 返回预置角色名→权限集合映射，与 seedRBAC 的角色定义保持一致。
// 供控制面网关注入/联邦转发身份（携带角色名而非权限字符串）做产品级 RBAC 校验。
// 这是角色权限划分的单一来源：seedRBAC 与 RBAC 闸都从此派生，杜绝定义漂移。
func RolePermissions() map[string][]string {
	allPerms := make([]string, 0, len(rbacPermSpecs))
	for _, ps := range rbacPermSpecs {
		allPerms = append(allPerms, ps.name)
	}
	viewerPerms := make([]string, 0)
	operatorPerms := make([]string, 0)
	// operatorGroups：运维角色可操作的资源组（read + write/execute）。
	// 注意：k8s 不在其中 —— K8s 集群管理仅 admin 可写/删，operator/viewer 仅读（严谨最小权限）。
	operatorGroups := map[string]bool{
		"device": true, "task": true, "alert": true, "cmdb": true,
		"deploy": true, "workflow": true, "log": true, "audit": true,
		"os": true, "middleware": true, "provision": true,
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
		if operatorGroups[group] && (action == "read" || action == "write" || action == "execute") {
			operatorPerms = append(operatorPerms, p)
		}
	}
	return map[string][]string{
		"admin":    allPerms,
		"operator": operatorPerms,
		"viewer":   viewerPerms,
	}
}
