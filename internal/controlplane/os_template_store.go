// os_template_store.go OS 模板的 store 查询回退（TD-87 批 2 第三批·切片 3：三个纯转换/查找
// 函数已迁入 internal/controlplane/presets——类型与转换同住一个包；本文件保留两个 Server 方法与薄包装）。
package controlplane

import (
	"github.com/Levango7/OpsMesh/internal/controlplane/presets"
	"github.com/Levango7/OpsMesh/internal/store"
)

// osTemplateByID / osTemplateToStore / osTemplateFromStore 是 presets 包同名函数的薄包装
// （父包 os_optimize.go 与测试的调用点零改动）。
func osTemplateByID(id string) *OSTemplate { return presets.ByID(id) }

func osTemplateToStore(t *OSTemplate, tenantID string) *store.OSTemplate {
	return presets.ToStore(t, tenantID)
}

func osTemplateFromStore(st *store.OSTemplate) *OSTemplate { return presets.FromStore(st) }

// Package controlplane: os_template_store.go 实现 OS 模板的 store 持久化适配与查询辅助。
//
// 从 os_optimize.go 拆分而来，包含 osTemplateToStore/osTemplateFromStore 转换、
// listOSTemplatesFromStore/getOSTemplateByID 查询回退、osTemplateByID 内存查找。
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
//
// **无租户过滤**：预置模板 seed 在 default 租户，却要对所有租户可解析——执行链路
// （handleExecuteOSTemplate）靠它解析共享预置模板，故此处**不得**加租户闸。
// 面向租户隔离的 CRUD 路径请用 getOSTemplateScoped。
func (s *Server) getOSTemplateByID(id string) *OSTemplate {
	if st := s.store.GetOSTemplate(id); st != nil {
		return osTemplateFromStore(st)
	}
	// 回退到预置模板（store 未 seed 或为空）。
	return osTemplateByID(id)
}

// getOSTemplateScoped 按租户归属读取模板；不存在或不属于该租户一律返回 nil。
//
// 与无租户过滤的 getOSTemplateByID 的区别：这里做**归属判定**，
// 属他租户的行与不存在的行返回同一个结果（nil），调用方据此回 404，
// 不泄露"该 ID 是否存在"（防跨租户枚举）。
func (s *Server) getOSTemplateScoped(tenantID, id string) *OSTemplate {
	if t := s.store.GetOSTemplate(id); t != nil {
		if t.TenantID != tenantID {
			return nil
		}
		return osTemplateFromStore(t)
	}
	return nil
}
