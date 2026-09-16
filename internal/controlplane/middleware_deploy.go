// Package controlplane: middleware_deploy.go 实现中间件部署预置模板与 API。
//
// 提供 10+ 个常见中间件（MySQL/Redis/Kafka/Nginx/Tomcat/Zookeeper/PostgreSQL/
// MongoDB/RabbitMQ/Elasticsearch）的部署模板，每个模板支持 docker 容器化与 systemd
// 裸机两种部署方式，通过：
//   - GET    /api/v1/middleware-templates          列出所有模板（可选 ?category= 过滤）
//   - POST   /api/v1/middleware-templates          创建新模板（CRUD）
//   - GET    /api/v1/middleware-templates/{id}     获取模板详情
//   - PUT    /api/v1/middleware-templates/{id}     更新模板（CRUD）
//   - DELETE /api/v1/middleware-templates/{id}     删除模板（CRUD）
//   - POST   /api/v1/middleware-templates/{id}/deploy 在指定 agent 上部署
//   - GET    /api/v1/middleware-instances          查询已部署实例（从任务历史推导）
//
// 设计要点（模板从内存常量改为 store 持久化，支持在线 CRUD）：
//   - 预置模板仍以内存常量 middlewareTemplates 维护（版本随代码升级），启动时
//     seedPresetMiddlewareTemplates 将其幂等写入 store（按 ID 去重，已存在不覆盖）。
//   - API 从 store 读取模板列表/详情；store 为空时回退到内存常量（向后兼容）。
//   - deploy 将 params 替换脚本占位符（{name}/{port}/...）后作为 shell task 下发，
//     复用 store.CreateTask + Audit + 事件总线 + SSE，与 os_optimize.go 同款逻辑。
//   - 租户隔离与审计复用 handleCreateTask 同款逻辑。
//
// 拆分说明：预置模板数据位于 middleware_template_presets.go，CRUD handler 位于
// middleware_template_handlers.go，部署/卸载 handler 位于 middleware_deploy_handler.go，
// 验证/store 适配位于 middleware_template_store.go，本文件保留类型定义与实例路由分派。
package controlplane

import (
	"net/http"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// MiddlewareTemplate 预置中间件部署模板。
// Scripts 按 deployType（"docker"/"systemd"）索引对应部署/验证/卸载脚本。
type MiddlewareTemplate struct {
	ID          string                      `json:"id"`
	Name        string                      `json:"name"`
	Category    string                      `json:"category"` // database/cache/message/web/search
	Version     string                      `json:"version"`
	Description string                      `json:"description"`
	DeployTypes []string                    `json:"deployTypes"` // ["docker","systemd"]
	Params      []MiddlewareParam           `json:"params"`
	Scripts     map[string]MiddlewareScript `json:"scripts"` // key: "docker"/"systemd"
	Risk        string                      `json:"risk"`    // low/medium/high
	Tags        []string                    `json:"tags"`
}

// MiddlewareParam 中间件部署参数定义。
type MiddlewareParam struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Default     string `json:"default"`
	Required    bool   `json:"required"`
	Type        string `json:"type"` // string/int/bool
}

// MiddlewareScript 部署脚本三元组：部署/验证/卸载。
// 脚本内可使用 {name}/{port}/{password}/... 等占位符，deploy 时由 params 替换。
type MiddlewareScript struct {
	Deploy    string `json:"deploy"`    // 部署命令
	Verify    string `json:"verify"`    // 验证/健康检查命令
	Uninstall string `json:"uninstall"` // 卸载命令
}

// handleMiddlewareInstanceRouting 统一分派 /api/v1/middleware-instances/{id}... 子路径：
//   - POST /api/v1/middleware-instances/{id}/uninstall：卸载实例
//
// 注意：/api/v1/middleware-instances（无尾斜杠）由 handleMiddlewareInstances 处理；
// /api/v1/middleware-instances/（带尾斜杠但无 id）此处转给 list handler 兜底。
func (s *Server) handleMiddlewareInstanceRouting(w http.ResponseWriter, r *http.Request) {
	idAndRest := strings.TrimPrefix(r.URL.Path, "/api/v1/middleware-instances/")
	if idAndRest == "" {
		// 兜底：/api/v1/middleware-instances/（带尾斜杠）转给 list handler 处理 GET。
		s.handleMiddlewareInstances(w, r)
		return
	}
	parts := strings.SplitN(idAndRest, "/", 2)
	id := parts[0]
	if id == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "instance id required"})
		return
	}
	switch {
	case len(parts) == 2 && parts[1] == "uninstall":
		// POST /api/v1/middleware-instances/{id}/uninstall
		s.handleUninstallMiddlewareInstance(w, r, id)
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}
