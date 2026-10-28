package controlplane

import (
	"sort"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/internal/controlplane/presets"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// middlewareInstance 派生出的已部署中间件实例。
//
// OpsMesh 不维护独立的"实例表"——实例清单从审计日志中按 action=deploy_middleware /
// uninstall_middleware 推导而来。这样做的好处：
//  1. 单一事实源（审计日志是等保三级要求的留痕，天然可信）
//  2. 部署/卸载的状态机与审计记录强一致，不会出现"装了但没记录"的幽灵实例
//  3. 无需额外迁移与数据对账
type middlewareInstance struct {
	ID           string `json:"id"` // 复合键 <templateID>@<agentID>:<deployType>:<version>
	TemplateID   string `json:"templateID"`
	TemplateName string `json:"templateName"`
	AgentID      string `json:"agentID"`
	DeployType   string `json:"deployType"`
	Version      string `json:"version"`
	Status       string `json:"status"` // deployed | uninstalling
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
}

// parseAuditDetail 将审计 Detail 字符串解析为 key/value 映射。
// Detail 格式："template=foo deployType=docker agent=ag1 version=8.0 instance=foo@ag1:docker:8.0"
// 解析失败的键值对被静默跳过（保持健壮性）。
func parseAuditDetail(detail string) map[string]string {
	out := make(map[string]string)
	for _, token := range strings.Fields(detail) {
		idx := strings.IndexByte(token, '=')
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(token[:idx])
		val := strings.TrimSpace(token[idx+1:])
		if key == "" || val == "" {
			continue
		}
		out[key] = val
	}
	return out
}

// instanceKey 构建中间件实例的复合键。
// 格式：<templateID>@<agentID>:<deployType>:<version>
func instanceKey(templateID, agentID, deployType, version string) string {
	return templateID + "@" + agentID + ":" + deployType + ":" + version
}

// deriveMiddlewareInstances 从审计日志推导已部署的中间件实例列表。
//
// 算法：
//  1. 查询当前租户的 deploy_middleware / uninstall_middleware 审计事件
//  2. 按复合键构建实例映射
//  3. deploy 事件 → 创建/刷新实例（status=deployed，createdAt 取首次 deploy 时间）
//  4. uninstall 事件 → 标记 status=uninstalling
//  5. 按 createdAt 倒序排序
//  6. 支持 agentID / category / templateID 过滤
//
// cap 审计扫描上限（默认 2000），避免无限制查询。
func (s *Server) deriveMiddlewareInstances(tenantID string, filterAgentID, filterCategory, filterTemplateID string, includeRemoved bool) []middlewareInstance {
	if s.store == nil {
		return []middlewareInstance{}
	}

	// 查询部署与卸载两类审计事件（各取 2000 条，按时间倒序）。
	deploys := s.store.QueryAudits(tenantID, "deploy_middleware", time.Time{}, time.Time{}, 2000)
	uninstalls := s.store.QueryAudits(tenantID, "uninstall_middleware", time.Time{}, time.Time{}, 2000)

	// 按复合键构建实例映射。
	type entry struct {
		inst middlewareInstance
	}
	m := make(map[string]*entry)

	for _, ev := range deploys {
		d := parseAuditDetail(ev.Detail)
		tplID := d["template"]
		agentID := d["agent"]
		deployType := d["deployType"]
		version := d["version"]
		key := d["instance"]
		if key == "" {
			key = instanceKey(tplID, agentID, deployType, version)
		}
		if key == "" || tplID == "" || agentID == "" {
			continue
		}
		// 过滤
		if filterAgentID != "" && agentID != filterAgentID {
			continue
		}
		if filterTemplateID != "" && tplID != filterTemplateID {
			continue
		}
		// category 过滤需要模板查找
		if filterCategory != "" {
			tpl := s.getMiddlewareTemplateByID(tplID)
			if tpl == nil || tpl.Category != filterCategory {
				continue
			}
		}

		tplName := tplID
		if tpl := s.getMiddlewareTemplateByID(tplID); tpl != nil && tpl.Name != "" {
			tplName = tpl.Name
		}

		e, ok := m[key]
		if !ok {
			e = &entry{inst: middlewareInstance{
				ID:           key,
				TemplateID:   tplID,
				TemplateName: tplName,
				AgentID:      agentID,
				DeployType:   deployType,
				Version:      version,
				Status:       "deployed",
				CreatedAt:    ev.CreatedAt.UTC().Format(time.RFC3339),
				UpdatedAt:    ev.CreatedAt.UTC().Format(time.RFC3339),
			}}
			m[key] = e
		} else {
			// 已有条目：更新 updatedAt，但 createdAt 取首次 deploy
			e.inst.UpdatedAt = ev.CreatedAt.UTC().Format(time.RFC3339)
			e.inst.Status = "deployed"
		}
		_ = e
	}

	// 处理卸载事件：标记对应实例为 uninstalling
	for _, ev := range uninstalls {
		d := parseAuditDetail(ev.Detail)
		key := d["instance"]
		if key == "" {
			tplID := d["template"]
			agentID := d["agent"]
			deployType := d["deployType"]
			version := d["version"]
			key = instanceKey(tplID, agentID, deployType, version)
		}
		if key == "" {
			continue
		}
		e, ok := m[key]
		if !ok {
			continue
		}
		// 有卸载事件 → 标记为 uninstalling
		e.inst.Status = "uninstalling"
		e.inst.UpdatedAt = ev.CreatedAt.UTC().Format(time.RFC3339)
	}

	// 收集结果
	result := make([]middlewareInstance, 0, len(m))
	for _, e := range m {
		if !includeRemoved && e.inst.Status == "uninstalling" {
			// 默认不显示正在卸载的实例
			continue
		}
		result = append(result, e.inst)
	}

	// 按 createdAt 倒序排序
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt > result[j].CreatedAt
	})

	if result == nil {
		result = []middlewareInstance{}
	}
	return result
}

// resolveMiddlewareVersion resolves the version string from a requested version.
// Delegates to the version resolution in middleware_version.go.
func resolveMiddlewareVersionStr(tpl *MiddlewareTemplate, requestedVersion string) string {
	v := resolveMiddlewareVersion(tpl, requestedVersion)
	if v == nil {
		if requestedVersion != "" {
			return requestedVersion
		}
		return tpl.Version
	}
	return v.Version
}

// selectedMiddlewareVersionImage returns the DockerImage for the selected version.
func selectedMiddlewareVersionImage(tpl *MiddlewareTemplate, version string) string {
	v := resolveMiddlewareVersion(tpl, version)
	if v != nil && v.DockerImage != "" {
		return v.DockerImage
	}
	// Fallback: resolve default version and use its image.
	if defaultV := resolveMiddlewareVersion(tpl, ""); defaultV != nil && defaultV.DockerImage != "" {
		return defaultV.DockerImage
	}
	return ""
}

var _ = presets.MiddlewareTemplates
var _ = proto.Task{}
