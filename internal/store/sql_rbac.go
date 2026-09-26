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
	var roleIDsJSON []byte
	var createdAt time.Time
	var tenantID sql.NullString
	if err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Status, &roleIDsJSON, &createdAt, &u.MustChangePassword, &tenantID); err != nil {
		return nil
	}
	u.CreatedAt = createdAt
	u.TenantID = normalizeTenantID(strings.TrimSpace(tenantID.String))
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
	// cmdb:approve CI 变更审批（G1/SEC-5）：cmdb_approval.go 的 approve/reject 端点经
	// requireProd 校验该权限点，但此前未在权限目录定义——已建库的 SQLStore 走 INSERT
	// IGNORE 幂等补种；operator 角色按 RolePermissions 派生规则（仅 read/write/execute）
	// 不会获得审批权，审批仅 admin 可用（最小权限：CI 变更审批属敏感操作）。
	{"cmdb", "cmdb:approve", "审批配置项变更"},
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
	{"network", "network:read", "查看网络设备"},
	{"network", "network:write", "管理网络设备"},
	{"automation", "automation:read", "查看自动化规则"},
	{"automation", "automation:write", "管理自动化规则"},
	{"webhook", "webhook:read", "查看 Webhook"},
	{"webhook", "webhook:write", "管理 Webhook"},
	{"script", "script:read", "查看自定义脚本"},
	{"script", "script:write", "管理自定义脚本"},
	{"gateway", "gateway:read", "查看 API 网关"},
	{"gateway", "gateway:write", "管理 API 网关"},
	{"tenant", "tenant:read", "查看租户"},
	{"tenant", "tenant:write", "管理租户"},
	{"apikey", "apikey:read", "查看 API Key"},
	{"apikey", "apikey:write", "管理 API Key"},
	{"plugin", "plugin:read", "查看插件"},
	{"plugin", "plugin:write", "管理插件"},
	{"billing", "billing:read", "查看计费"},
	{"billing", "billing:write", "管理计费"},
	{"platform", "platform:read", "查看平台配置"},
	{"platform", "platform:write", "管理平台配置"},
	// M13 六域接线新增（聚合代理/ChatOps 命令台的权限点）：
	// 前端 router requirePerm 已引用（nav.gpu/bot/runbooks/incidents/autoscaler/portal），
	// 此前缺目录——requirePermission 对 admin 不受影响（admin=全量），但角色无法被
	// 显式授予、权限管理页不可见。补种后 SQLStore INSERT IGNORE 幂等/MemoryStore
	// 构造期全量重建；viewer/operator 派生规则自动生效（viewer=全部 *:read）。
	{"gpu", "gpu:read", "查看GPU资源"},
	{"gpu", "gpu:write", "管理GPU工作负载/模型"},
	{"bot", "bot:read", "查看ChatOps命令台"},
	{"bot", "bot:write", "执行ChatOps命令"},
	{"runbook", "runbook:read", "查看Runbook"},
	{"runbook", "runbook:write", "编辑/执行Runbook"},
	{"incident", "incident:read", "查看事件"},
	{"incident", "incident:write", "编辑事件"},
	{"autoscaler", "autoscaler:read", "查看扩缩容规则"},
	{"autoscaler", "autoscaler:write", "编辑扩缩容规则"},
	{"portal", "portal:read", "查看服务门户"},
	{"portal", "portal:write", "审批门户请求"},
	// P1-6 可支撑性：配置转储（/api/v1/admin/config）与诊断包（/api/v1/admin/diagnostics）。
	// 刻意**不以 `:read` 结尾**——派生规则会把所有 `*:read` 自动授予 viewer，而配置转储
	// 与诊断包含内部拓扑与配置细节，只应给 admin（admin 自动获得全部权限点）。
	{"diagnostics", "diagnostics:dump", "导出脱敏配置转储与诊断包（仅 admin）"},
	// 运行期改日志级别：动作名刻意用 :execute 而非 :read/:dump，配合 operatorGroups 里的
	// "diagnostics" 让 operator 也能提级别（现场排障的人不该为此找管理员要 token），
	// 同时 diagnostics:dump（含内部拓扑）仍严格 admin-only。
	{"diagnostics", "diagnostics:execute", "调整进程日志级别"},
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
		// diagnostics 组在此 = operator 可执行"调级别"这类动作型权限点；
		// 但 diagnostics:dump 的动作名是 dump（不属 read/write/execute），故 operator 拿不到。
		"diagnostics": true,
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
