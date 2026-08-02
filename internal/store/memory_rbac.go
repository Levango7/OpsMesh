// memory_rbac.go 实现 MemoryStore 的 UserStore / RoleStore / PermissionStore 三个子接口。
//
// 用户中心（RBAC）内存实现：
//   - users / usersByName / roles / permissions 字段在 MemoryStore struct 中定义；
//   - 预填充数据（权限/角色/用户）在 NewMemoryStore.seedRBAC() 中完成；
//   - 本文件实现 CRUD 方法，全部经 m.mu 互斥保护，并发安全。
//
// 设计要点：
//   - GetUserByUsername 维护独立的 usersByName 索引，登录 O(1) 直查；
//   - CreateUser 用户名重复返回 nil（调用方据此判断冲突，不抛错）；
//   - ListUsers / ListRoles 返回深拷贝避免外部修改破坏内部状态；
//   - permissions 为预定义只读列表，ListPermissions 直接返回副本。
package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// randUserID 生成随机用户 ID（16 字节十六进制，crypto/rand 密码学安全）。
// 用于 CreateUser 分配 ID（调用方未填 ID 时）。
func randUserID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// 熵源失败回退时间戳（降级但可容忍，用户 ID 唯一性由 usersByName 索引兜底）。
		return fmt.Sprintf("user-%d", time.Now().UnixNano())
	}
	return "user-" + hex.EncodeToString(b)
}

// randRoleID 生成随机角色 ID。
func randRoleID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("role-%d", time.Now().UnixNano())
	}
	return "role-" + hex.EncodeToString(b)
}

// ============================================================================
// UserStore 实现（6 方法）
// ============================================================================

// GetUser 按 ID 返回单用户（深拷贝，避免外部修改破坏内部状态）。
func (m *MemoryStore) GetUser(id string) *User {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.users[id]
	if !ok {
		return nil
	}
	c := *u
	if u.RoleIDs != nil {
		c.RoleIDs = append([]string(nil), u.RoleIDs...)
	}
	return &c
}

// GetUserByUsername 按用户名返回单用户（登录用；O(1) 直查 usersByName 索引）。
func (m *MemoryStore) GetUserByUsername(username string) *User {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.usersByName[username]
	if !ok {
		return nil
	}
	c := *u
	if u.RoleIDs != nil {
		c.RoleIDs = append([]string(nil), u.RoleIDs...)
	}
	return &c
}

// ListUsers 返回全部用户（按创建时间升序；深拷贝）。
func (m *MemoryStore) ListUsers() []*User {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*User, 0, len(m.users))
	for _, u := range m.users {
		c := *u
		if u.RoleIDs != nil {
			c.RoleIDs = append([]string(nil), u.RoleIDs...)
		}
		out = append(out, &c)
	}
	// 按创建时间升序（稳定排序，避免依赖 sort 包）。
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j].CreatedAt.Before(out[j-1].CreatedAt) {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}

// CreateUser 创建用户。调用方须先 bcrypt 哈希密码填入 u.PasswordHash。
// 用户名重复时返回 nil（调用方据此判断冲突）。
// u.ID 为空时由 store 分配随机 ID；CreatedAt 为空时填当前时间。
func (m *MemoryStore) CreateUser(u *User) *User {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u == nil || u.Username == "" {
		return nil
	}
	// 用户名唯一性校验。
	if _, exists := m.usersByName[u.Username]; exists {
		return nil
	}
	if u.ID == "" {
		u.ID = randUserID()
	}
	if u.Status == "" {
		u.Status = "active"
	}
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now()
	}
	// 防御性拷贝 RoleIDs，避免外部切片被后续修改影响内部。
	if u.RoleIDs != nil {
		u.RoleIDs = append([]string(nil), u.RoleIDs...)
	}
	m.users[u.ID] = u
	m.usersByName[u.Username] = u
	return u
}

// UpdateUser 更新用户 email/roles/status（按 u.ID 定位）。不存在返回 false。
// Username 与 PasswordHash 不可经此方法修改（避免误覆盖登录凭据）。
func (m *MemoryStore) UpdateUser(u *User) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u == nil {
		return false
	}
	existing, ok := m.users[u.ID]
	if !ok {
		return false
	}
	// 仅更新允许的字段（email/roles/status），保留 Username/PasswordHash/CreatedAt。
	if u.Email != "" {
		existing.Email = u.Email
	}
	if u.Status != "" {
		existing.Status = u.Status
	}
	if u.RoleIDs != nil {
		existing.RoleIDs = append([]string(nil), u.RoleIDs...)
	}
	return true
}

// DeleteUser 按 ID 删除用户。不存在返回 false。
// 同时清理 usersByName 索引，避免删除后同名用户无法重新创建。
func (m *MemoryStore) DeleteUser(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		return false
	}
	delete(m.users, id)
	delete(m.usersByName, u.Username)
	return true
}

// ============================================================================
// RoleStore 实现（5 方法）
// ============================================================================

// GetRole 按 ID 返回单角色（深拷贝）。
func (m *MemoryStore) GetRole(id string) *Role {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.roles[id]
	if !ok {
		return nil
	}
	c := *r
	if r.Permissions != nil {
		c.Permissions = append([]string(nil), r.Permissions...)
	}
	return &c
}

// ListRoles 返回全部角色（深拷贝）。
func (m *MemoryStore) ListRoles() []*Role {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Role, 0, len(m.roles))
	for _, r := range m.roles {
		c := *r
		if r.Permissions != nil {
			c.Permissions = append([]string(nil), r.Permissions...)
		}
		out = append(out, &c)
	}
	// 按创建时间升序。
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j].CreatedAt.Before(out[j-1].CreatedAt) {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}

// CreateRole 创建角色。角色名重复时返回 nil。
// r.ID 为空时由 store 分配随机 ID；CreatedAt 为空时填当前时间。
func (m *MemoryStore) CreateRole(r *Role) *Role {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r == nil || r.Name == "" {
		return nil
	}
	// 角色名唯一性校验。
	for _, existing := range m.roles {
		if existing.Name == r.Name {
			return nil
		}
	}
	if r.ID == "" {
		r.ID = randRoleID()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now()
	}
	if r.Permissions != nil {
		r.Permissions = append([]string(nil), r.Permissions...)
	}
	m.roles[r.ID] = r
	return r
}

// UpdateRole 更新角色 description/permissions（按 r.ID 定位）。不存在返回 false。
// Name 不可经此方法修改（避免误覆盖角色标识）。
func (m *MemoryStore) UpdateRole(r *Role) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r == nil {
		return false
	}
	existing, ok := m.roles[r.ID]
	if !ok {
		return false
	}
	if r.Description != "" {
		existing.Description = r.Description
	}
	if r.Permissions != nil {
		existing.Permissions = append([]string(nil), r.Permissions...)
	}
	return true
}

// DeleteRole 按 ID 删除角色。不存在返回 false。
func (m *MemoryStore) DeleteRole(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.roles[id]; !ok {
		return false
	}
	delete(m.roles, id)
	return true
}

// ============================================================================
// PermissionStore 实现（1 方法）
// ============================================================================

// ListPermissions 返回全部预定义权限（按组分类；深拷贝）。
// permissions 为只读列表，初始化时填充，运行期不修改。
func (m *MemoryStore) ListPermissions() []*Permission {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Permission, len(m.permissions))
	for i, p := range m.permissions {
		c := *p
		out[i] = &c
	}
	return out
}
