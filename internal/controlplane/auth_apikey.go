// auth_apikey.go H5 认证接入：API Key 认证路径 + 用户权限展开 + 角色权限缓存。
//
// 身份模型：API Key 调用者是"程序"而非"用户"，与联邦 peer 同属非用户身份——
// 认证通过后返回 (caller=nil, true)，绝不构造假 *store.User（会污染 caller.ID 审计
// 与 MustChangePassword 等用户专属逻辑）。各 handler 侧既有 nil-safe 推导
// （if caller != nil { userID = caller.ID }），nil-caller 时审计 UserID 为空串，
// 与联邦路径行为完全一致；后续如需区分来源可统一落为 "apikey:"+ak.Name。
package controlplane

import (
	"net/http"
	"strings"
	"time"

	"opsmesh/internal/controlplane/paginate"

	"opsmesh/internal/store"
)

// userPermissions 展开用户经角色获得的全部权限字符串（去重）。
// 用户 → RoleIDs → 各 Role.Permissions → 合并去重。
func (s *Server) userPermissions(u *store.User) []string {
	if u == nil || len(u.RoleIDs) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	for _, rid := range u.RoleIDs {
		r := s.store.GetRole(rid)
		if r == nil {
			continue
		}
		for _, p := range r.Permissions {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// ============================================================================
// 统一产品级 RBAC 闸（权限控制无疏漏）—— 兼容两种身份来源。
// ============================================================================

// getRolePermCache 返回角色名→权限集合映射（取自 store.RolePermissions()）。
//
// 原实现为包初始化时计算一次的全局 var rolePermCache，管理员经 CreateRole/UpdateRole/
// DeleteRole 修改角色权限后缓存不更新，导致权限陈旧（已分配新权限的用户被旧缓存拒绝，
// 或已收回权限的用户仍被旧缓存放行）。改为每次调用动态查询 store.RolePermissions()，
// 保证权限划分始终与 seedRBAC/DB 当前定义一致，杜绝定义漂移。
//
// 性能：store.RolePermissions() 仅遍历预置 rbacPermSpecs 派生映射（纯内存计算，无 IO），
// 每次调用开销可忽略，且 authorizeByRoles 仅在网关注入/联邦转发路径（非热路径）调用。
func getRolePermCache() map[string][]string {
	return store.RolePermissions()
}

// ============================================================================
// API Key 认证（requireProd 第 2.5 路，设计见 FIXPLAN §2.3）。
// ============================================================================

// extractAPIKey 从请求中提取 API Key 明文凭据。
// 识别顺序：
//  1. Authorization: Bearer om_* —— scheme 用 EqualFold 匹配（与 extractBearer 一致，
//     HTTP auth-scheme 不区分大小写），token 部分必须以 om_ 开头才认定为 API Key；
//     Bearer 携带普通 JWT 时返回 false，交由调用方继续走 JWT 路径；
//  2. X-API-Key 头非空 —— CI/CD 等程序化客户端的推荐携带方式。
//
// 返回 (key, true) 表示该请求声明了 API Key 身份（须走 authorizeByAPIKey 完成认证，
// 不允许静默回落到 demo/拒绝路径造成语义漂移）；("", false) 表示无 API Key 凭据。
func extractAPIKey(r *http.Request) (string, bool) {
	if auth := r.Header.Get("Authorization"); auth != "" {
		const scheme = "Bearer "
		if len(auth) > len(scheme) && strings.EqualFold(auth[:len(scheme)], scheme) {
			key := strings.TrimSpace(auth[len(scheme):])
			if strings.HasPrefix(key, apikeyPrefix) {
				return key, true
			}
			return "", false // Bearer + 非 om_ token → 普通 JWT，交回 JWT 路径
		}
		return "", false // Authorization 非 Bearer 格式 → 不按 API Key 处理
	}
	if v := strings.TrimSpace(r.Header.Get("X-API-Key")); v != "" {
		return v, true
	}
	return "", false
}

// authorizeByAPIKey 校验 API Key 并检查 required scope（H5 认证闸）。
//
// 流程（任一步失败均已写入响应，调用方应直接 return）：
//  1. ValidateKey：格式校验（om_ 前缀 + 32 位 hex）→ SHA-256(明文) 与库存 hash 比对
//     → Enabled / ExpiresAt 校验；失败 → 401 {"error":"invalid api key"}；
//  2. Enabled/过期双保险：ValidateKey 已含同等校验，此处再防实现漂移或并发禁用窗口
//     （禁用经 UpdateAPIKey 全量替换，ValidateKey 返回的是快照拷贝）；失败 → 401；
//  3. 租户交叉校验（关键越权防线）：X-Tenant-ID 显式指定租户时必须与 key 归属租户一致，
//     否则 → 403 {"error":"tenant mismatch ..."}，堵住"A 租户 key 操作 B 租户资源"的 IDOR；
//     头缺省时取 key 自身租户（单 key 绑定单租户，语义上即代表其归属租户）；
//  4. HasScope：权限点 resource:action 与 Scopes 同构直接比对（支持 device:* 通配）；
//     不足 → 403 {"error":"insufficient scope"}；
//  5. 成功：内存聚合 LastUsedAt 计数 +1，返回 (nil, true)。
//
// 安全兜底：apiKeyMgr 为 nil（理论上仅测试直构 Server 可达）时一律按无效 key 拒绝，
// 绝不允许因空指针 panic 或跳过校验形成认证旁路。
func (s *Server) authorizeByAPIKey(w http.ResponseWriter, r *http.Request, required, key string) (*store.User, bool) {
	if s.apiKeyMgr == nil || key == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid api key"})
		return nil, false
	}
	ak, err := s.apiKeyMgr.ValidateKey(key)
	if err != nil || ak == nil {
		// 统一错误文案，不区分"不存在/格式错/已禁用/已过期"，防枚举探测有效 key。
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid api key"})
		return nil, false
	}
	// 双保险：Enabled 与 ExpiresAt（ValidateKey 已校验，见函数注释）。
	if !ak.Enabled {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid api key"})
		return nil, false
	}
	if !ak.ExpiresAt.IsZero() && time.Now().After(ak.ExpiresAt) {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid api key"})
		return nil, false
	}
	// 租户交叉校验：X-Tenant-ID 与 key 归属不一致即拒绝（越权防线，FIXPLAN §2.3.2）。
	reqTenant := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	if reqTenant == "" {
		reqTenant = ak.TenantID // 头缺省时取 key 自身租户
	}
	if reqTenant != ak.TenantID {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch between X-Tenant-ID header and api key tenant"})
		return nil, false
	}
	if !s.apiKeyMgr.HasScope(ak, required) {
		paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "insufficient scope"})
		return nil, false
	}
	s.recordAPIKeyUsage(ak.ID)
	return nil, true
}

// recordAPIKeyUsage 内存聚合 API Key 使用计数（LastUsedAt MVP，FIXPLAN §2.3.3）。
// 仅累计不落库：禁止每请求同步写 store（Memory 锁竞争/MultiSchema 跨 schema 路由/
// SQL 写放大）；后台批量刷写 goroutine 本期不做，重启丢失计数可接受。
// apiKeyUsage 未初始化时静默跳过（容错未初始化 map 的测试 Server，不影响鉴权结果）。
func (s *Server) recordAPIKeyUsage(keyID string) {
	if keyID == "" || s.apiKeyUsage == nil {
		return
	}
	s.apiKeyUsageMu.Lock()
	defer s.apiKeyUsageMu.Unlock()
	s.apiKeyUsage[keyID]++
}

// apiKeyUsageCount 返回指定 API Key 的内存使用计数（未记录/未初始化返回 0）。
// 供列表接口低成本附带 usage 或观测使用（当前 GET /api/v1/apikeys 响应结构位于
// apikey.go，本期不改其响应契约，仅提供读取入口）。
func (s *Server) apiKeyUsageCount(keyID string) int64 {
	if keyID == "" || s.apiKeyUsage == nil {
		return 0
	}
	s.apiKeyUsageMu.Lock()
	defer s.apiKeyUsageMu.Unlock()
	return s.apiKeyUsage[keyID]
}
