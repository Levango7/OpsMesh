// auth_perms.go 实现权限查询 handler：GET /api/v1/permissions。
//
// 从 auth.go 拆分而来（纯代码移动，未修改任何逻辑）。依赖 auth.go 中的
// userFromToken 鉴权 helper 与 server.go 中的 writeJSON 响应 helper。
package controlplane

import (
	"net/http"

	"opsmesh/internal/controlplane/paginate"
)

// ============================================================================
// 权限查询 handler：/api/v1/permissions
// ============================================================================

// handlePermissions 处理 GET /api/v1/permissions：返回全部预定义权限。
// 鉴权：仅需有效 token（登录用户均可查看权限列表，便于前端权限选择）。
func (s *Server) handlePermissions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, err := s.userFromToken(r); err != nil {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"permissions": s.store.ListPermissions()})
}
