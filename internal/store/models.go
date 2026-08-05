// models.go 定义用户中心（用户/角色/权限）领域数据模型。
//
// 设计目标：为 OpsMesh 控制面提供完整 RBAC 能力，支持：
//   - 用户：注册/登录/查询/CRUD，密码 bcrypt 哈希；
//   - 角色：CRUD，角色绑定一组权限字符串（如 "device:read"）；
//   - 权限：预定义权限列表，按组分类（device/task/alert/cmdb/...）。
//
// 与现有 6 领域（Device/Task/Alert/Audit/Token/Leader）解耦，
// 通过 UserStore/RoleStore/PermissionStore 三个小接口暴露，组合进 Store。
package store

import "time"

// User 用户实体。PasswordHash 为 bcrypt 哈希（绝不存明文）。
// RoleIDs 为该用户绑定的角色 ID 列表（用户经角色间接获得权限）。
type User struct {
	ID                string    `json:"id"`
	Username          string    `json:"username"`
	Email             string    `json:"email"`
	PasswordHash      string    `json:"-"`                // bcrypt 哈希；JSON 序列化时不输出（防泄露）
	Status            string    `json:"status"`           // "active" | "pending" | "rejected" | "disabled"（P1-7：pending=待管理员审批）
	RoleIDs           []string  `json:"roleIDs"`
	CreatedAt         time.Time `json:"createdAt"`
	MustChangePassword bool     `json:"mustChangePassword"` // 强制改密标记：预置弱口令用户首登须改密（安全债 85）
}

// Role 角色实体。Permissions 为权限字符串数组（如 ["device:read", "task:write"]）。
type Role struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Permissions []string  `json:"permissions"`
	CreatedAt   time.Time `json:"createdAt"`
}

// Permission 权限实体。Name 为权限字符串（如 "device:read"），Group 为所属分组（如 "device"）。
type Permission struct {
	ID          string `json:"id"`
	Name        string `json:"name"` // 如 "device:read"
	Description string `json:"description"`
	Group       string `json:"group"` // 如 "device", "task", "alert"
}

// K8sCluster K8s 集群配置实体（Phase 3 后端 K8s 集群管理）。
//
// 字段说明：
//   - ID：集群唯一标识（创建时由 store 分配随机 ID）；
//   - Name：集群展示名（用户输入，如 "prod-cluster"）；
//   - Server：API Server 地址（从 kubeconfig 解析得到，便于列表展示无需解 kubeconfig）；
//   - Kubeconfig：kubeconfig YAML 内容（敏感，API 返回时须脱敏为 ***）；
//   - Status：连接状态（online/offline/unknown，由 test API 刷新）；
//   - CreatedAt / UpdatedAt：创建/更新时间戳。
//
// 安全要点：Kubeconfig 含集群凭据，绝不原样返回给前端；API 层负责脱敏。
type K8sCluster struct {
	ID         string    `json:"id"`
	TenantID   string    `json:"tenantId"` // 所属租户（task 88 租户隔离；空值保存时归一为 default）
	Name       string    `json:"name"`
	Server     string    `json:"server"`     // API Server 地址
	Kubeconfig string    `json:"kubeconfig"` // kubeconfig 内容（YAML，敏感）
	Status     string    `json:"status"`     // online/offline/unknown
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}
