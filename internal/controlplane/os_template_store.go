// Package controlplane: os_template_store.go 实现 OS 模板的 store 持久化适配与查询辅助。
//
// 从 os_optimize.go 拆分而来，包含 osTemplateToStore/osTemplateFromStore 转换、
// listOSTemplatesFromStore/getOSTemplateByID 查询回退、osTemplateByID 内存查找。
package controlplane

import (
	"encoding/json"

	"github.com/Levango7/OpsMesh/internal/store"
)

// osTemplateByID 按 ID 查找预置模板，未找到返回 nil。
func osTemplateByID(id string) *OSTemplate {
	for i := range osTemplates {
		if osTemplates[i].ID == id {
			return &osTemplates[i]
		}
	}
	return nil
}

// osTemplateToStore 将 controlplane.OSTemplate 转换为 store.OSTemplate。
// 整个 OSTemplate 序列化为 JSON 存入 Config 字段；store.OSTemplate 的 Name/OS 冗余存储便于 SQL 过滤。
func osTemplateToStore(t *OSTemplate, tenantID string) *store.OSTemplate {
	if t == nil {
		return nil
	}
	cfg, _ := json.Marshal(t)
	return &store.OSTemplate{
		ID:       t.ID,
		TenantID: tenantID,
		Name:     t.Name,
		OS:       t.OS,
		Config:   string(cfg),
	}
}

// osTemplateFromStore 将 store.OSTemplate 反转换为 controlplane.OSTemplate（从 Config 反序列化）。
// Config 为空或反序列化失败时，用 store 行的 ID/Name/OS 构造最小 OSTemplate（向后兼容）。
func osTemplateFromStore(st *store.OSTemplate) *OSTemplate {
	if st == nil {
		return nil
	}
	if st.Config == "" {
		return &OSTemplate{ID: st.ID, Name: st.Name, OS: st.OS}
	}
	var t OSTemplate
	if err := json.Unmarshal([]byte(st.Config), &t); err != nil {
		return &OSTemplate{ID: st.ID, Name: st.Name, OS: st.OS}
	}
	// 以 store 行的 ID/Name/OS 为准（防 Config 中过期值）。
	if st.ID != "" {
		t.ID = st.ID
	}
	if st.Name != "" {
		t.Name = st.Name
	}
	if st.OS != "" {
		t.OS = st.OS
	}
	return &t
}

// listOSTemplatesFromStore 从 store 读取 OS 模板列表（含回退）。
// 合并当前租户的模板与 default 租户的预置模板（按 ID 去重）；
// store 完全为空时回退到内存常量 osTemplates（向后兼容）。
func (s *Server) listOSTemplatesFromStore(tenantID string) []OSTemplate {
	// 取当前租户模板 + default 租户预置模板（合并去重）。
	stored := s.store.ListOSTemplates(tenantID)
	if tenantID != "" && tenantID != "default" {
		stored = append(stored, s.store.ListOSTemplates("default")...)
	}
	if len(stored) == 0 {
		// 回退到内存常量（store 未初始化或为空）。
		out := make([]OSTemplate, len(osTemplates))
		copy(out, osTemplates)
		return out
	}
	seen := make(map[string]bool, len(stored))
	out := make([]OSTemplate, 0, len(stored))
	for _, st := range stored {
		if seen[st.ID] {
			continue
		}
		seen[st.ID] = true
		if t := osTemplateFromStore(st); t != nil {
			out = append(out, *t)
		}
	}
	return out
}

// getOSTemplateByID 从 store 读取单个 OS 模板（含回退）。
// store 中不存在时回退到内存常量 osTemplateByID（向后兼容）。
func (s *Server) getOSTemplateByID(id string) *OSTemplate {
	if st := s.store.GetOSTemplate(id); st != nil {
		return osTemplateFromStore(st)
	}
	// 回退到预置模板（store 未 seed 或为空）。
	return osTemplateByID(id)
}
