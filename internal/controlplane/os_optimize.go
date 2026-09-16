// Package controlplane: os_optimize.go 实现 OS 基础环境优化预置模板与 API。
//
// 提供运维场景化的 shell 脚本模板（内核/网络/安全/时间同步/SSH/磁盘/系统/用户），
// 通过 GET /api/v1/os-templates 列表、GET /api/v1/os-templates/{id} 详情、
// POST /api/v1/os-templates/{id}/execute 在指定 agent 上执行（复用 task 下发通道）。
//
// 设计要点（模板从内存常量改为 store 持久化，支持在线 CRUD）：
//   - 预置模板仍以内存常量 osTemplates 维护（版本随代码升级），启动时 seedPresetOSTemplates
//     将其幂等写入 store（按 ID 去重，已存在不覆盖，保留用户在线修改）。
//   - API 从 store 读取模板列表/详情；store 为空时回退到内存常量（向后兼容）。
//   - 新增 CRUD：POST /api/v1/os-templates 创建、PUT /api/v1/os-templates/{id} 更新、
//     DELETE /api/v1/os-templates/{id} 删除。
//   - execute 将 params 通过 `set --` 注入脚本位置参数，agent 侧 `sh -c command` 执行时 $1/$2 即可拿到。
//   - 租户隔离与审计复用 handleCreateTask 同款逻辑（requireAuth + authctx + Audit + 事件总线 + SSE）。
//
// 拆分说明：HTTP handler 位于 os_template_handlers.go，验证函数位于 os_template_validate.go，
// store 适配位于 os_template_store.go，预置模板数据位于 os_template_presets.go，
// 本文件保留类型定义、osTemplates 组合声明、seed 与路由分派。
package controlplane

import (
	"log"
	"net/http"
	"strings"

	"opsmesh/internal/controlplane/paginate"
)

// OSTemplate 预置 OS 优化任务模板。
// Commands 为一段 shell 脚本（在目标 Linux 主机以 `sh -c` 执行）；
// 需要参数的模板在脚本内通过 $1/$2/... 引用（旧模式）或 {name}/{port}/... 占位符引用（新模式），
// execute 时由控制面注入位置参数或做占位符替换。
type OSTemplate struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Category    string    `json:"category"` // kernel/network/security/time/ssh/disk/system/user
	Description string    `json:"description"`
	Commands    string    `json:"commands"`         // shell 脚本（可用 #!/bin/bash 开头）
	Risk        string    `json:"risk"`             // low/medium/high
	Tags        []string  `json:"tags"`             // 标签
	OS          string    `json:"os"`               // 适用操作系统：centos/ubuntu/all
	Params      []OSParam `json:"params,omitempty"` // 参数定义（新模式占位符替换 + 验证）
}

// OSParam OS 优化模板参数定义。
type OSParam struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Default     string `json:"default"`
	Required    bool   `json:"required"`
	Type        string `json:"type"` // string/int
}

// osTemplates 预置 OS 优化模板集合（由 osTemplatesCore + osTemplatesExt 组合，数据定义见 os_template_presets.go）。
var osTemplates = append(osTemplatesCore, osTemplatesExt...)

// seedPresetOSTemplates 启动时将预置 OS 模板幂等写入 store（按 ID 去重，已存在不覆盖）。
// 保持向后兼容：store 为空时 API 回退到内存常量 osTemplates。
// 预置模板归入 "default" 租户，对所有租户可见。
func (s *Server) seedPresetOSTemplates() {
	for i := range osTemplates {
		tpl := &osTemplates[i]
		if existing := s.store.GetOSTemplate(tpl.ID); existing != nil {
			continue // 已存在（用户可能已在线修改），不覆盖
		}
		st := osTemplateToStore(tpl, "default")
		if err := s.store.SaveOSTemplate(st); err != nil {
			log.Printf("[controlplane] seed 预置 OS 模板 %s 失败: %v", tpl.ID, err)
		}
	}
}

// handleOSTemplateRouting 统一分派 /api/v1/os-templates/{id}... 子路径：
//   - GET    /api/v1/os-templates/{id}：模板详情
//   - PUT    /api/v1/os-templates/{id}：更新模板（CRUD）
//   - DELETE /api/v1/os-templates/{id}：删除模板（CRUD）
//   - POST   /api/v1/os-templates/{id}/execute：在指定 agent 上执行模板
//
// 注意：/api/v1/os-templates（无尾斜杠）由 handleListOSTemplates 处理；
// /api/v1/os-templates/（带尾斜杠但无 id）此处返回 400。
func (s *Server) handleOSTemplateRouting(w http.ResponseWriter, r *http.Request) {
	idAndRest := strings.TrimPrefix(r.URL.Path, "/api/v1/os-templates/")
	if idAndRest == "" {
		// 兜底：/api/v1/os-templates/（带尾斜杠）转给 list handler 处理 GET/POST。
		s.handleListOSTemplates(w, r)
		return
	}
	parts := strings.SplitN(idAndRest, "/", 2)
	id := parts[0]
	if id == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "template id required"})
		return
	}
	switch {
	case len(parts) == 1:
		// /api/v1/os-templates/{id}
		switch r.Method {
		case http.MethodGet:
			s.handleOSTemplateByID(w, r, id)
		case http.MethodPut:
			s.handleUpdateOSTemplate(w, r, id)
		case http.MethodDelete:
			s.handleDeleteOSTemplate(w, r, id)
		default:
			paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
	case len(parts) == 2 && parts[1] == "execute":
		// POST /api/v1/os-templates/{id}/execute
		s.handleExecuteOSTemplate(w, r, id)
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}
