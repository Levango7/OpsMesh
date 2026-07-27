// Package authctx 定义“网关注入的身份上下文”的提取与校验。
//
// 设计原则（U-04 等保三级 + 复用底座，非自研登录）：
//   - 内核（控制面）【不】自行实现登录 / 鉴权 / 用户表 / 密码哈希；
//   - 登录、SSO、MFA、RBAC 由前置网关（APISIX / 蓝鲸 IAM）完成；
//   - 网关校验 JWT/OIDC 后，把身份注入到请求头 / gRPC metadata：
//       X-Tenant-ID  / x-tenant-id   —— 租户（行级隔离键）
//       X-User-Id   / x-user-id     —— 用户（审计留痕）
//       X-User-Roles/ x-user-roles  —— 角色（逗号分隔，垂直越权防护辅助）
//   - 内核只消费这些头，并据此做“行级租户隔离”与“审计留痕”。
//
// 缺失头时（开发 / 单机模式，无网关）视为单一隐式租户，放行全部——
// 这是 MVP 单租户（U-02 多团队逻辑隔离）的合理降级，不是越权。
package authctx

import (
	"net/http"
	"strings"

	"google.golang.org/grpc/metadata"
)

// Context 是网关注入的身份上下文。
type Context struct {
	TenantID string
	UserID   string
	Roles    []string
}

const (
	hdrTenant = "x-tenant-id"
	hdrUser   = "x-user-id"
	hdrRoles  = "x-user-roles"
)

// FromHTTPHeader 从 HTTP 头提取身份上下文（前置网关已校验 JWT 并注入）。
func FromHTTPHeader(h http.Header) Context {
	c := Context{
		TenantID: h.Get(hdrTenant),
		UserID:   h.Get(hdrUser),
	}
	c.Roles = splitRoles(h.Get(hdrRoles))
	return c
}

// FromGRPCMetadata 从 gRPC metadata 提取身份上下文（网关 / mTLS 终止点注入）。
func FromGRPCMetadata(md metadata.MD) Context {
	get := func(k string) string {
		if v := md.Get(k); len(v) > 0 {
			return v[0]
		}
		return ""
	}
	c := Context{
		TenantID: get(hdrTenant),
		UserID:   get(hdrUser),
	}
	c.Roles = splitRoles(get(hdrRoles))
	return c
}

func splitRoles(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// BelongsTo 判断资源所属租户是否与当前上下文匹配。
// 空 tenantID 表示“无网关 / 开发模式”，放行全部（不强制隔离）。
func (c Context) BelongsTo(resourceTenant string) bool {
	if c.TenantID == "" {
		return true
	}
	return c.TenantID == resourceTenant
}

// HasRole 判断上下文是否含指定角色。
// 垂直越权防护由网关 RBAC 拦截器主力完成，此为内核侧辅助判断。
func (c Context) HasRole(role string) bool {
	for _, r := range c.Roles {
		if r == role {
			return true
		}
	}
	return false
}
