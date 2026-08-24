# 01 - 认证架构：Keycloak IdP + OpsMesh SP

## 冲突描述

Keycloak 和 OpsMesh 内置 Auth 都想做用户认证：
- **Keycloak**：企业级 IdP，支持 OIDC/OAuth2/SAML，统一身份管理
- **OpsMesh 内置 Auth**：本地用户名/密码 + JWT 签发 + RBAC

两者同时启用会导致：双份用户目录、Token 互不信任、SSO 失效。

## 解决方案

**职责分工**：
- **Keycloak** → IdP（身份提供者）：用户目录、登录页面、Token 签发（OIDC JWT）、SSO
- **OpsMesh** → SP（服务提供者）：Token 验签、RBAC 鉴权、租户路由、API 权限

**边界规则**：
- 开发模式：OpsMesh 内置 Auth（方便本地调试）
- 生产模式：Key>Keycloak 签发 JWT，OpsMesh 仅验签

## 数据流图

```
┌────────┐     ┌───────────┐     ┌──────────┐     ┌──────────┐
│  User  │────▶│ Keycloak  │────▶│ OpsMesh  │────▶│  RBAC    │
│        │     │  (IdP)    │     │  (SP)    │     │  Check   │
│        │     │           │     │          │     │          │
│ Login  │     │ Sign JWT  │     │ Verify   │     │ Allow/   │
│        │◀────│           │     │ JWT      │     │ Deny     │
│        │     │           │     │          │     │          │
└────────┘     └───────────┘     └──────────┘     └──────────┘
                     │                                   │
                     ▼                                   ▼
              ┌──────────┐                      ┌──────────┐
              │  User    │                      │  Tenant  │
              │  Store   │                      │  Router  │
              └──────────┘                      └──────────┘
```

## 配置示例

### Keycloak Client 配置
```json
{
  "clientId": "opsmesh",
  "enabled": true,
  "protocol": "openid-connect",
  "publicClient": false,
  "standardFlowEnabled": true,
  "bearerOnly": false,
  "redirectUris": ["https://opsmesh.example.com/*"],
  "webOrigins": ["https://opsmesh.example.com"]
}
```

### OpsMesh 生产模式配置
```yaml
auth:
  mode: production
  jwt:
    issuer: "https://keycloak.example.com/realms/opsmesh"
    public_key: "/etc/opsmesh/keycloak-public-key.pem"
    algorithm: "RS256"
  rbac:
    enabled: true
    role_mapping:
      "opsmesh-admin": "admin"
      "opsmesh-operator": "operator"
      "opsmesh-viewer": "viewer"
```

## 迁移路径

1. **Phase 1**：部署 Keycloak，创建 OpsMesh Client
2. **Phase 2**：OpsMesh 增加 JWT 验签模式（`--auth-mode=jwt`）
3. **Phase 3**：用户目录迁移到 Keycloak
4. **Phase 4**：生产模式切换到 Keycloak 签发
5. **Phase 5**：保留内置 Auth 仅用于开发模式

## 验收标准

- [ ] 生产模式 JWT 100% 由 Keycloak 签发
- [ ] OpsMesh 不签发 JWT（仅验签）
- [ ] SSO 在多个服务间正常工作
- [ ] RBAC 角色映射正确
- [ ] 开发模式仍可用内置 Auth

## 风险与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| Keycloak 不可用 | 无法登录 | Keycloak HA 部署 + 本地缓存 JWT |
| 角色映射不一致 | 权限错误 | 统一角色命名规范 + 自动化同步 |
| 开发/生产差异 | 开发环境无法复现问题 | Docker Compose 包含 Keycloak |