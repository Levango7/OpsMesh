package cmdb

import "context"

// CiStore 是 CMDB 的存储接口，双后端（Memory / SQL）实现。
type CiStore interface {
	// CiType 查询
	CiTypes(ctx context.Context, tenantID string) ([]CiType, error)
	// CreateCiType 创建自定义（非内置）CI 类型。
	CreateCiType(ctx context.Context, t *CiType) error

	// CI 列表（支持按类型/状态/租户过滤）
	GetCIs(ctx context.Context, ciType, status, tenantID string) ([]CiItem, error)
	// GetCIsByApproval 按审批状态查询 CI 列表（Phase-3 待审列表）。
	GetCIsByApproval(ctx context.Context, approvalStatus, tenantID string) ([]CiItem, error)
	// SetApproval 设置单个 CI 的审批状态（Phase-3 审批/驳回端点）。
	SetApproval(ctx context.Context, id, tenantID, approvalStatus string) error
	// 单 CI 查询
	GetCI(ctx context.Context, id, tenantID string) (*CiItem, error)
	// 创建 CI
	CreateCI(ctx context.Context, ci *CiItem) error
	// 更新 CI（产生新版本）
	UpdateCI(ctx context.Context, ci *CiItem) error
	// 软删除
	DeleteCI(ctx context.Context, id, tenantID string) error
	// 属性历史
	GetCIHistory(ctx context.Context, ciID, tenantID string, limit int) ([]CiItem, error)

	// === Phase 2: 关系拓扑 ===

	// CreateRelation 创建 CI 间关系。
	CreateRelation(ctx context.Context, rel *CiRelation) error
	// DeleteRelation 删除关系。
	DeleteRelation(ctx context.Context, id int64, tenantID string) error
	// GetCIRelations 查询指定 CI 的所有关系（含目标 CI 名称）。
	GetCIRelations(ctx context.Context, ciID, tenantID string) ([]CiRelation, error)
	// GetCIRelationGraph 返回 CI 及其关联图谱（含源/目标 CI 的简要信息）。
	GetCIRelationGraph(ctx context.Context, ciID, tenantID string) (*CIRelationGraph, error)

	// === Phase 2: 属性模板 ===

	// CreateAttrTemplate 创建属性模板。
	CreateAttrTemplate(ctx context.Context, tmpl *CiAttrTemplate) error
	// GetAttrTemplates 查询属性模板（按 ciType 过滤）。
	GetAttrTemplates(ctx context.Context, ciType, tenantID string) ([]CiAttrTemplate, error)
	// UpdateAttrTemplate 更新属性模板。
	UpdateAttrTemplate(ctx context.Context, tmpl *CiAttrTemplate) error
	// DeleteAttrTemplate 删除属性模板。
	DeleteAttrTemplate(ctx context.Context, id int, tenantID string) error
}
