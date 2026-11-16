package controlplane

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

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

// middlewareAuditDetailKeys 是中间件实例审计 Detail 允许的键集合。
// 解析时未知键一律忽略（防值里夹带 k=v 伪造字段）。
var middlewareAuditDetailKeys = map[string]struct{}{
	"template":   {},
	"deployType": {},
	"agent":      {},
	"version":    {},
	"instance":   {},
}

// formatMiddlewareAuditDetail 将键值对编码为中间件审计 Detail。
// 格式："key=value;key=value;"，值经 strconv.Quote 转义，因此可以安全包含空格、';'、'='；
// parseAuditDetail 是它的逆操作，未知键与畸形片段在解析时忽略。
func formatMiddlewareAuditDetail(kv ...string) string {
	var b strings.Builder
	for i := 0; i+1 < len(kv); i += 2 {
		key := kv[i]
		if _, ok := middlewareAuditDetailKeys[key]; !ok {
			continue
		}
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(strconv.Quote(kv[i+1]))
		b.WriteByte(';')
	}
	return b.String()
}

// splitQuotedSegments 按**未加引号**的 ';' 切分 Detail 片段。
// 引号内的 ';'（如 agent="a;b"）属于值的一部分，不能作为片段边界。
func splitQuotedSegments(s string) []string {
	var segs []string
	start := 0
	inQuote := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			continue
		}
		if inQuote && c == '\\' {
			escaped = true
			continue
		}
		if c == '"' {
			inQuote = !inQuote
			continue
		}
		if c == ';' && !inQuote {
			segs = append(segs, s[start:i])
			start = i + 1
		}
	}
	return append(segs, s[start:])
}

// parseAuditDetail 将审计 Detail 字符串解析为 key/value 映射。
// 新格式由 formatMiddlewareAuditDetail 生成："key=value;key=value;"（值带引号、可含空格/';'/'='）。
// 兼容旧格式："template=foo deployType=docker agent=ag1 ..."（空格分隔，值不能含空格）。
func parseAuditDetail(detail string) map[string]string {
	out := make(map[string]string)
	if detail == "" {
		return out
	}
	// 新格式带引号值（key="...";），按引号感知的 ';' 切分；未知键与畸形片段忽略。
	if strings.Contains(detail, `="`) {
		for _, seg := range splitQuotedSegments(detail) {
			seg = strings.TrimSpace(seg)
			key, raw, ok := strings.Cut(seg, "=")
			if !ok {
				continue
			}
			key = strings.TrimSpace(key)
			if _, known := middlewareAuditDetailKeys[key]; !known {
				continue
			}
			val := strings.TrimSpace(raw)
			if unq, err := strconv.Unquote(val); err == nil {
				val = unq
			}
			if val != "" {
				out[key] = val
			}
		}
		return out
	}
	// 旧格式：空格分隔 key=value，仅识别已知键。
	for _, token := range strings.Fields(detail) {
		key, val, ok := strings.Cut(token, "=")
		if !ok || key == "" || val == "" {
			continue
		}
		if _, known := middlewareAuditDetailKeys[key]; !known {
			continue
		}
		out[key] = val
	}
	return out
}

// middlewareInstanceIDMaxLen URL 路径实例键的最大长度。
// 复合键现实上限约 150 字符（模板 ID + agent ID + 部署方式 + 版本）；
// 取 256 留余量，同时拒绝 10k 级超长键进入审计 Detail 与存储。
const middlewareInstanceIDMaxLen = 256

// validMiddlewareInstanceID 校验 URL 路径中的实例键：不得包含空白、控制字符或
// k=v/条目分隔符（'='、';'），且长度不超过 middlewareInstanceIDMaxLen，
// 防止审计 Detail 注入、键混淆与超长键放大。
// 空串表示调用方未提供，由请求体推导。
func validMiddlewareInstanceID(id string) bool {
	if id == "" {
		return true
	}
	if len(id) > middlewareInstanceIDMaxLen {
		return false
	}
	if strings.ContainsAny(id, "=;") {
		return false
	}
	for _, r := range id {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// instanceKey 构建中间件实例的复合键。
// 格式：<templateID>@<agentID>:<deployType>:<version>
func instanceKey(templateID, agentID, deployType, version string) string {
	return fmt.Sprintf("%s@%s:%s:%s", templateID, agentID, deployType, version)
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

// resolveMiddlewareVersionAndKey 解析请求版本并计算实例复合键。
// deploy 与 uninstall 共用本函数，保证两侧的版本解析与 instanceKey 推导完全一致
// （否则卸载审计写入的键与部署键不等，实例会永久停留在 deployed）。
func resolveMiddlewareVersionAndKey(tpl *MiddlewareTemplate, templateID, agentID, deployType, requestedVersion string, params map[string]string) (version, key string) {
	version = selectedMiddlewareVersion(requestedVersion, params)
	if v := resolveMiddlewareVersion(tpl, version); v != nil {
		version = v.Version
	}
	return version, instanceKey(templateID, agentID, deployType, version)
}

var _ = presets.MiddlewareTemplates
var _ = proto.Task{}
