// Package cmdb 实现配置管理数据库（CMDB）业务逻辑：CI 管理、agent 自动上报、关系拓扑。
package cmdb

import "time"

// CI 审批状态常量（Phase-3 轻量审批流）。
const (
	ApprovalPending  = "pending"  // 待审批
	ApprovalApproved = "approved" // 已通过（默认值，向后兼容旧数据）
	ApprovalRejected = "rejected" // 已驳回
)

// CiType CI 类型字典。
type CiType struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`        // machine / os / service / app / cluster
	DisplayName string    `json:"displayName"` // 物理机 / 操作系统 / 系统服务 / 应用 / 集群
	Builtin     bool      `json:"builtin"`     // 系统内置不可删
	CreatedAt   time.Time `json:"createdAt"`
}

// CiItem CI 实例。
type CiItem struct {
	ID             string            `json:"id"`             // ci-<ulid>
	CiType         string            `json:"ciType"`         // ci_types.name
	TenantID       string            `json:"tenantID"`       // 行级隔离
	Name           string            `json:"name"`           // 展示名（hostname）
	Status         string            `json:"status"`         // active / archived / deleted
	ApprovalStatus string            `json:"approvalStatus"` // Phase-3：pending / approved / rejected，默认 approved
	Attrs          map[string]string `json:"attrs"`          // 扁平化属性
	Source         string            `json:"source"`         // manual / agent / discover / api / import
	AgentID        string            `json:"agentID"`        // 上报 agent
	DeviceID       string            `json:"deviceID"`       // 关联 device
	Version        int               `json:"version"`        // 乐观锁
	CreatedAt      time.Time         `json:"createdAt"`
	UpdatedAt      time.Time         `json:"updatedAt"`
}

// CiAttrTemplate CI 属性模板：定义某类型 CI 可用的属性集合。
type CiAttrTemplate struct {
	ID           int       `json:"id"`
	CiType       string    `json:"ciType"`
	AttrKey      string    `json:"attrKey"`
	Label        string    `json:"label"`
	AttrType     string    `json:"attrType"` // string / number / boolean / text
	Required     bool      `json:"required"`
	DefaultValue string    `json:"defaultValue,omitempty"`
	TenantID     string    `json:"tenantID"`
	CreatedAt    time.Time `json:"createdAt"`
}

// CiRelation CI 间关系。
type CiRelation struct {
	ID           int64             `json:"id"`
	SourceCIID   string            `json:"sourceCIID"`
	TargetCIID   string            `json:"targetCIID"`
	RelationType string            `json:"relationType"` // runs_on / depends / connects_to / contains / member_of
	TenantID     string            `json:"tenantID"`
	Attrs        map[string]string `json:"attrs,omitempty"`
	CreatedAt    time.Time         `json:"createdAt"`
}

// CIRelationGraph CI 关系图谱查询结果。
type CIRelationGraph struct {
	CenterCI  *CiItem              `json:"centerCI"`
	Relations []RelationWithTarget `json:"relations"`
}

// RelationWithTarget 带目标 CI 信息的关系。
type RelationWithTarget struct {
	CiRelation
	SourceName string `json:"sourceName"`
	TargetName string `json:"targetName"`
	TargetType string `json:"targetType"`
}
