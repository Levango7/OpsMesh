// auth_users.go 实现用户管理 handler：/api/v1/users CRUD + 审批/拒绝。
//
// 从 auth.go 拆分而来（纯代码移动，未修改任何逻辑）。依赖 auth.go 中的
// requirePermission 鉴权中间件、credentials.HashPassword、randHexID、credentials.ValidateStrongPassword helper，
// 以及 server.go 中的 writeJSON/decodeJSONBody 响应 helper。
package controlplane

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"

	"github.com/Levango7/OpsMesh/internal/controlplane/credentials"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
)

// ============================================================================
// 用户管理 handler：/api/v1/users
// ============================================================================

// handleUsers 统一处理 /api/v1/users：
//   - GET：列出全部用户（需 user:read 权限）
//   - POST：创建用户（需 user:write 权限）
func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListUsers(w, r)
	case http.MethodPost:
		s.handleCreateUser(w, r)
	default:
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListUsers 处理 GET /api/v1/users：列出用户（需 user:read 权限）。
//
// 租户隔离（P0-6）：非平台租户的调用者仅返回本租户用户；平台租户（default）保持全量视图
// ——平台管理员负责跨租户的用户编排，租户管理员不应看到他租户的账号清单。
func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.requirePermission(w, r, "user:read")
	if !ok {
		return
	}
	users := s.store.ListUsers()
	if t := tenantOrDefault(caller.TenantID); t != store.DefaultTenantID {
		scoped := make([]*store.User, 0, len(users))
		for _, u := range users {
			if u != nil && tenantOrDefault(u.TenantID) == t {
				scoped = append(scoped, u)
			}
		}
		users = scoped
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"users": users})
}

// resolveUserTenant 解析创建/更新用户的目标租户（P0-6）。
//
// 规则：
//   - 未指定 tenantId：归属调用方自己的租户（平台管理员不填 → default，与迁移前一致）；
//   - 显式指定他租户：仅平台租户（default）调用者可指派（防止租户 A 的管理员
//     把用户创建进租户 B，或以他租户身份制造账号）；
//   - 字面量恒经 validateTenantID 校验（租户 ID 会流入 schema 名与各域过滤参数）。
//
// 返回 (tenant, ok)：ok=false 时已写响应。
func resolveUserTenant(w http.ResponseWriter, caller *store.User, requested string) (string, bool) {
	callerTenant := tenantOrDefault(caller.TenantID)
	target := callerTenant
	if requested != "" && requested != callerTenant {
		if callerTenant != store.DefaultTenantID {
			paginate.WriteJSON(w, http.StatusForbidden, map[string]string{
				"error": "only platform tenant (default) users may assign other tenants",
			})
			return "", false
		}
		target = requested
	}
	if err := validateTenantID(target); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return "", false
	}
	return target, true
}

// handleCreateUser 处理 POST /api/v1/users：创建用户（需 user:write 权限）。
// 请求体：{username, password, email?, role_ids?, tenantId?}；密码最短 6 字符，bcrypt 哈希后存库。
func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.requirePermission(w, r, "user:write")
	if !ok {
		return
	}
	// 切流（TD-60 方案 C）：AUTH_SVC_OWNS_NEW_ACCOUNTS=true（且总闸开启）表示「新账号归 auth-svc 持有」，
	// 此时本地建号会与该声明矛盾——账号建在被切走的一侧，用户登录时对侧查无此人。
	// 故明确拒绝并指向该侧，而不是静默建在本地（设计 §4 的「/api/v1/users 建号走哪一侧」由此开关表达；
	// auth-svc 侧无等价的运维建号 API，所以这里是"拒绝+指引"而非转发）。判据与 RouteRegister 同源。
	if s.cutoverRouter != nil && s.cutoverRouter.Enabled() && s.cutoverRouter.OwnsNewAccounts() {
		paginate.WriteJSON(w, http.StatusConflict, map[string]string{
			"error": "new accounts are owned by auth-svc (AUTH_SVC_OWNS_NEW_ACCOUNTS=true); create it on the auth-svc side",
		})
		return
	}
	var body struct {
		Username           string   `json:"username"`
		Password           string   `json:"password"`
		Email              string   `json:"email"`
		RoleIDs            []string `json:"role_ids"`
		TenantID           string   `json:"tenantId"`
		MustChangePassword bool     `json:"mustChangePassword"`
		// must_change_password 为 snake 别名（与 role_ids/roleIDs 双名同策）：
		// 两键同时出现时取 OR——管理脚本用 snake、前端用 camel，任一为真即强制改密。
		MustChangePasswordSnake bool `json:"must_change_password"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		log.Printf("controlplane: handleCreateUser 解析请求体失败: %v", err)
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if body.Username == "" || body.Password == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password are required"})
		return
	}
	if msg := credentials.ValidateStrongPassword(body.Password); msg != "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return
	}
	targetTenant, ok := resolveUserTenant(w, caller, body.TenantID)
	if !ok {
		return
	}
	// P3 角色引用校验：role_ids 若存在须全部指向真实角色，避免写入无效角色引用。
	for _, rid := range body.RoleIDs {
		if rid != "" && s.store.GetRole(rid) == nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown role id: " + rid})
			return
		}
	}
	if s.store.GetUserByUsername(body.Username) != nil {
		paginate.WriteJSON(w, http.StatusConflict, map[string]string{"error": "username already exists"})
		return
	}
	hash, err := credentials.HashPassword(body.Password)
	if err != nil {
		log.Printf("controlplane: handleCreateUser 哈希密码失败: %v", err)
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	u := &store.User{
		ID:           randHexID("user"),
		Username:     body.Username,
		Email:        body.Email,
		TenantID:     targetTenant,
		PasswordHash: hash,
		Status:       "active",
		RoleIDs:      body.RoleIDs,
		// 首登强制改密透传（2026-10-14 演练 §3.2）：此前请求体不收该字段 ⇒ 管理员经
		// 单体 HTTP 面造不出"首登强制改密"账号，而 auth-svc 侧支持
		// （cutover-migrate --force-change 走的就是对侧）⇒ 两侧不对称，而首登改密
		// 恰是切流交接口令的关键机制（一次性口令的承重墙）。
		MustChangePassword: body.MustChangePassword || body.MustChangePasswordSnake,
	}
	if s.store.CreateUser(u) == nil {
		paginate.WriteJSON(w, http.StatusConflict, map[string]string{"error": "username already exists"})
		return
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	// 审计租户取目标用户租户：用户编排发生在哪个租户域，审计就归属哪个租户。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: targetTenant, UserID: caller.ID, Action: "user_create", Target: u.ID,
		Detail: sanitizeAuditDetail("username=" + u.Username + " tenant=" + targetTenant),
	})
	// 回读落库对象再响应（2026-10-14 演练 §3.4）：此前直接回请求体 u，SQL 后端实测
	// createdAt:"0001-01-01T00:00:00Z"（store 在自己的副本上补时间，不回写请求对象）、
	// permissions:null ⇒ 管理面建号响应是"请求回显"而不是"落库事实"，客户端据
	// "建号时间丢了"误判。回读后与 GET /users/{id} 同源，并按 login 同规则展开
	// 有效权限（authUserWithPerms）。极端并发（建完即删）退回请求体，不 500。
	stored := s.store.GetUser(u.ID)
	if stored == nil {
		stored = u
	}
	paginate.WriteJSON(w, http.StatusCreated, s.authUserWithPerms(stored))
}

// handleUserRouting 分派 /api/v1/users/{id} 子路径：
//   - PUT /api/v1/users/{id}：更新用户（需 user:write 权限）
//   - DELETE /api/v1/users/{id}：删除用户（需 user:delete 权限）
//   - POST /api/v1/users/{id}/approve：审批用户（需 user:approve 权限，注册安全）
//   - POST /api/v1/users/{id}/reject：拒绝用户（需 user:approve 权限，注册安全）
func (s *Server) handleUserRouting(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/users/")
	if rest == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "user id required"})
		return
	}
	// 解析 {id} 或 {id}/approve 或 {id}/reject。
	id := rest
	subAction := ""
	if idx := strings.Index(rest, "/"); idx > 0 {
		id = rest[:idx]
		subAction = rest[idx+1:]
	}
	if id == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "user id required"})
		return
	}
	// 子路径分发（approve/reject）。
	if subAction != "" {
		switch subAction {
		case "approve":
			s.handleApproveUser(w, r, id)
			return
		case "reject":
			s.handleRejectUser(w, r, id)
			return
		default:
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-path: " + subAction})
			return
		}
	}
	// 主路径分发（PUT/DELETE）。
	switch r.Method {
	case http.MethodPut:
		s.handleUpdateUser(w, r, id)
	case http.MethodDelete:
		s.handleDeleteUser(w, r, id)
	default:
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleApproveUser 处理 POST /api/v1/users/{id}/approve：管理员审批用户注册（注册安全）。
// 将用户 Status 从 "pending" 改为 "active"；仅 pending 状态可审批，其他状态返回 409。
// 鉴权：需 user:approve 权限（admin 角色自动拥有）。
func (s *Server) handleApproveUser(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	caller, ok := s.requirePermission(w, r, "user:approve")
	if !ok {
		return
	}
	existing := s.store.GetUser(id)
	if existing == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	if existing.Status != "pending" {
		paginate.WriteJSON(w, http.StatusConflict, map[string]string{"error": "user is not pending (current status: " + existing.Status + ")"})
		return
	}
	existing.Status = "active"
	if !s.store.UpdateUser(existing) {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: tenantOrDefault(existing.TenantID), UserID: caller.ID, Action: "user_approve", Target: id, Detail: sanitizeAuditDetail("approved user " + existing.Username),
	})
	paginate.WriteJSON(w, http.StatusOK, s.store.GetUser(id))
}

// handleRejectUser 处理 POST /api/v1/users/{id}/reject：管理员拒绝用户注册（注册安全）。
// 将用户 Status 改为 "rejected"；仅 pending 状态可拒绝，其他状态返回 409。
// 鉴权：需 user:approve 权限（admin 角色自动拥有）。
// 请求体可选：{reason?: "拒绝原因"}，记录到审计日志。
func (s *Server) handleRejectUser(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	caller, ok := s.requirePermission(w, r, "user:approve")
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	// 请求体可选；解析失败时记录日志（兼容空 body 调用）。
	if err := decodeJSONBody(w, r, &body); err != nil && err != io.EOF {
		log.Printf("controlplane: handleRejectUser 解析请求体失败: %v", err)
	}
	existing := s.store.GetUser(id)
	if existing == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	if existing.Status != "pending" {
		paginate.WriteJSON(w, http.StatusConflict, map[string]string{"error": "user is not pending (current status: " + existing.Status + ")"})
		return
	}
	existing.Status = "rejected"
	if !s.store.UpdateUser(existing) {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	detail := "rejected user " + existing.Username
	if body.Reason != "" {
		detail += " reason: " + body.Reason
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: tenantOrDefault(existing.TenantID), UserID: caller.ID, Action: "user_reject", Target: id, Detail: sanitizeAuditDetail(detail),
	})
	paginate.WriteJSON(w, http.StatusOK, s.store.GetUser(id))
}

// userUpdateFields 是 PUT /api/v1/users/{id} 认可的键集（含 F6 过渡别名 roleIDs）。
// 顺序即报错信息里的展示顺序（调用方照抄即可）。
var userUpdateFields = []string{"email", "role_ids", "roleIDs", "status", "tenantId"}

// unknownUserUpdateKeys 返回请求体里不在认可集内的键（排序后；空体/空对象返回空）。
// 纯函数：handler 只能黑盒断言状态码，报错里"点了哪些键、排序是否稳定"这类契约
// 必须由可测函数承载（与 plaintextSecretKeys / DroppedRoleLines 同一文化）。
func unknownUserUpdateKeys(raw []byte) ([]string, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	allowed := make(map[string]bool, len(userUpdateFields))
	for _, f := range userUpdateFields {
		allowed[f] = true
	}
	var unknown []string
	for k := range m {
		if !allowed[k] {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	return unknown, nil
}

// handleUpdateUser 处理 PUT /api/v1/users/{id}：更新用户 email/roles/status/tenantId（需 user:write 权限）。
// 请求体：{email?, role_ids?, status?, tenantId?}；仅更新非空字段。
//
// 未识别键一律 400（2026-10-14 演练 §3.3）：此前 struct 解码静默丢弃未知键 ⇒ 客户端
// 拼错字段名时拿到 200 no-op，据"成功"假象得出反的结论——演练里"HTTP 面无法授予角色"
// 那条**误报**的根因就在此（人设用 roles，规范名是 role_ids；PUT 200 但库里没动）。
// 与 http_infra.go decodeJSONBody 的全局宽容策略（不启用 DisallowUnknownFields、保前端
// 多传字段）不冲突：那是全局默认，本端点是管理面精确语义端点，逐点声明严格。
// roleIDs（驼峰）为 role_ids 的过渡别名（F6 双名策略；两键同时出现以 role_ids 为准）。
func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "user:write")
	if !ok {
		return
	}

	// 先取原始体做键集校验（bufferRequestBody 已带与 decodeJSONBody 同款上限并还原 r.Body）。
	raw := bufferRequestBody(w, r)
	unknown, err := unknownUserUpdateKeys(raw)
	if err != nil {
		log.Printf("controlplane: handleUpdateUser 解析请求体失败: %v", err)
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if len(unknown) > 0 {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"error": "unknown field(s): " + strings.Join(unknown, ",") +
				"; recognized: " + strings.Join(userUpdateFields, ","),
		})
		return
	}

	var body struct {
		Email        string   `json:"email"`
		RoleIDs      []string `json:"role_ids"`
		RoleIDsCamel []string `json:"roleIDs"`
		Status       string   `json:"status"`
		TenantID     string   `json:"tenantId"`
	}
	if err := json.Unmarshal(raw, &body); err != nil && err != io.EOF {
		log.Printf("controlplane: handleUpdateUser 解析请求体失败: %v", err)
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	// F6 双名：仅 camel 出现时按 role_ids 语义生效（两键都有以规范名为准）。
	if body.RoleIDs == nil && body.RoleIDsCamel != nil {
		body.RoleIDs = body.RoleIDsCamel
	}
	// 状态变更需更高权限：仅 user:write 不能激活/禁用账号，须 user:approve（与 审批模型一致），
	// 防止低权限用户自行把 Status 置 active/rejected 绕过审批流。
	if body.Status != "" {
		if _, ok := s.requirePermission(w, r, "user:approve"); !ok {
			return
		}
	}
	existing := s.store.GetUser(id)
	if existing == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	// 租户迁移（P0-6）：与创建同规则——仅平台租户调用者可跨租户指派；非平台租户的调用者
	// 只能把用户留在自己租户（显式指定他租户 → 403）。空值=不改租户。
	if body.TenantID != "" {
		targetTenant, ok := resolveUserTenant(w, caller, body.TenantID)
		if !ok {
			return
		}
		existing.TenantID = targetTenant
	}
	if body.Email != "" {
		existing.Email = body.Email
	}
	if body.RoleIDs != nil {
		existing.RoleIDs = body.RoleIDs
	}
	if body.Status != "" {
		existing.Status = body.Status
	}
	if !s.store.UpdateUser(existing) {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: tenantOrDefault(existing.TenantID), UserID: caller.ID, Action: "user_update", Target: id, Detail: "updated via HTTP",
	})
	paginate.WriteJSON(w, http.StatusOK, s.store.GetUser(id))
}

// handleDeleteUser 处理 DELETE /api/v1/users/{id}：删除用户（需 user:delete 权限）。
func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "user:delete")
	if !ok {
		return
	}
	target := s.store.GetUser(id)
	if target == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	if !s.store.DeleteUser(id) {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	// 携带 ctx 的 trace_id，使审计日志与链路追踪关联。
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: tenantOrDefault(target.TenantID), UserID: caller.ID, Action: "user_delete", Target: id, Detail: "deleted via HTTP",
	})
	w.WriteHeader(http.StatusNoContent)
}
