// Package controlplane: middleware_template_store.go 实现中间件模板的验证、store 持久化适配与查询辅助。
//
// 从 middleware_deploy.go 拆分而来，包含 renderMiddlewareScript/validateMiddlewareParams 验证函数、
// middlewareTemplateToStore/middlewareTemplateFromStore 转换、seedPresetMiddlewareTemplates seed、
// listMiddlewareTemplatesFromStore/getMiddlewareTemplateByID 查询回退、middlewareTemplateByID 内存查找。
package controlplane

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"

	"opsmesh/internal/store"
)

func middlewareTemplateByID(id string) *MiddlewareTemplate {
	for i := range middlewareTemplates {
		if middlewareTemplates[i].ID == id {
			return &middlewareTemplates[i]
		}
	}
	return nil
}

// renderMiddlewareScript 将脚本中的 {name}/{port}/{password}/... 占位符替换为 params 实际值。
// 占位符语法：{key}，未提供 key 时保留原占位符（便于排查）。
func renderMiddlewareScript(script string, params map[string]string) string {
	out := script
	for k, v := range params {
		out = strings.ReplaceAll(out, "{"+k+"}", v)
	}
	return out
}

// validateMiddlewareParams 校验中间件模板参数的类型与语义。
//   - int 类型：必须为整数；若参数名为 port 或以 port 结尾则校验端口范围 1-65535。
//   - string 类型：若参数名为路径类（datadir/configdir/confpath/javahome 或以 dir/path 结尾）则校验以 / 开头。
//
// validatePort/validateNonEmpty/validatePath 定义在 os_optimize.go（同包共享）。
func validateMiddlewareParams(params []MiddlewareParam, values map[string]string) error {
	for _, p := range params {
		val, ok := values[p.Name]
		if !ok || val == "" {
			continue
		}
		switch p.Type {
		case "int":
			n, err := strconv.Atoi(val)
			if err != nil {
				return fmt.Errorf("param %s must be integer, got %s", p.Name, val)
			}
			if p.Name == "port" || strings.HasSuffix(p.Name, "port") {
				if err := validatePort(n); err != nil {
					return err
				}
			}
		case "string":
			if p.Name == "datadir" || p.Name == "configdir" || p.Name == "confpath" || p.Name == "javahome" ||
				strings.HasSuffix(p.Name, "dir") || strings.HasSuffix(p.Name, "path") {
				if err := validatePath(val); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// ============================================================================
// 中间件模板 store 持久化适配（转换 + seed + 查询回退）
// ============================================================================

// middlewareTemplateToStore 将 controlplane.MiddlewareTemplate 转换为 store.MiddlewareTemplate。
// 整个 MiddlewareTemplate 序列化为 JSON 存入 Config 字段；
// store.MiddlewareTemplate 的 Name/Type/Version 冗余存储便于 SQL 过滤。
func middlewareTemplateToStore(t *MiddlewareTemplate, tenantID string) *store.MiddlewareTemplate {
	if t == nil {
		return nil
	}
	cfg, _ := json.Marshal(t)
	return &store.MiddlewareTemplate{
		ID:       t.ID,
		TenantID: tenantID,
		Name:     t.Name,
		Type:     t.Category, // Category 映射到 Type（中间件类别）
		Version:  t.Version,
		Config:   string(cfg),
	}
}

// middlewareTemplateFromStore 将 store.MiddlewareTemplate 反转换为 controlplane.MiddlewareTemplate。
// Config 为空或反序列化失败时，用 store 行的 ID/Name/Type/Version 构造最小模板（向后兼容）。
func middlewareTemplateFromStore(st *store.MiddlewareTemplate) *MiddlewareTemplate {
	if st == nil {
		return nil
	}
	if st.Config == "" {
		return &MiddlewareTemplate{ID: st.ID, Name: st.Name, Category: st.Type, Version: st.Version}
	}
	var t MiddlewareTemplate
	if err := json.Unmarshal([]byte(st.Config), &t); err != nil {
		return &MiddlewareTemplate{ID: st.ID, Name: st.Name, Category: st.Type, Version: st.Version}
	}
	// 以 store 行的 ID/Name 为准（防 Config 中过期值）。
	if st.ID != "" {
		t.ID = st.ID
	}
	if st.Name != "" {
		t.Name = st.Name
	}
	return &t
}

// seedPresetMiddlewareTemplates 启动时将预置中间件模板幂等写入 store（按 ID 去重，已存在不覆盖）。
// 保持向后兼容：store 为空时 API 回退到内存常量 middlewareTemplates。
// 预置模板归入 "default" 租户，对所有租户可见。
func (s *Server) seedPresetMiddlewareTemplates() {
	for i := range middlewareTemplates {
		tpl := &middlewareTemplates[i]
		if existing := s.store.GetMiddlewareTemplate(tpl.ID); existing != nil {
			continue // 已存在（用户可能已在线修改），不覆盖
		}
		st := middlewareTemplateToStore(tpl, "default")
		if err := s.store.SaveMiddlewareTemplate(st); err != nil {
			log.Printf("[controlplane] seed 预置中间件模板 %s 失败: %v", tpl.ID, err)
		}
	}
}

// listMiddlewareTemplatesFromStore 从 store 读取中间件模板列表（含回退）。
// 合并当前租户的模板与 default 租户的预置模板（按 ID 去重）；
// store 完全为空时回退到内存常量 middlewareTemplates（向后兼容）。
func (s *Server) listMiddlewareTemplatesFromStore(tenantID string) []MiddlewareTemplate {
	stored := s.store.ListMiddlewareTemplates(tenantID)
	if tenantID != "" && tenantID != "default" {
		stored = append(stored, s.store.ListMiddlewareTemplates("default")...)
	}
	if len(stored) == 0 {
		// 回退到内存常量（store 未初始化或为空）。
		out := make([]MiddlewareTemplate, len(middlewareTemplates))
		copy(out, middlewareTemplates)
		return out
	}
	seen := make(map[string]bool, len(stored))
	out := make([]MiddlewareTemplate, 0, len(stored))
	for _, st := range stored {
		if seen[st.ID] {
			continue
		}
		seen[st.ID] = true
		if t := middlewareTemplateFromStore(st); t != nil {
			out = append(out, *t)
		}
	}
	return out
}

// getMiddlewareTemplateByID 从 store 读取单个中间件模板（含回退）。
// store 中不存在时回退到内存常量 middlewareTemplateByID（向后兼容）。
func (s *Server) getMiddlewareTemplateByID(id string) *MiddlewareTemplate {
	if st := s.store.GetMiddlewareTemplate(id); st != nil {
		return middlewareTemplateFromStore(st)
	}
	// 回退到预置模板（store 未 seed 或为空）。
	return middlewareTemplateByID(id)
}
