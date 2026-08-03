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
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`      // bcrypt 哈希；JSON 序列化时不输出（防泄露）
	Status       string    `json:"status"` // "active" | "pending" | "rejected" | "disabled"（P1-7：pending=待管理员审批）
	RoleIDs      []string  `json:"roleIDs"`
	CreatedAt    time.Time `json:"createdAt"`
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
