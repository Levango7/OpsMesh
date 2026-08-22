# OpsMesh 网络安全机制文档

> 文档版本：v1.0
> 最后更新：2026-08-17
> 适用范围：OpsMesh 控制面（controlplane）+ Agent + 联邦通道
> 编写依据：源码审计 + 等保三级要求 + OWASP Top 10 防护对照
> 配套文档：[tech-debt.md](./tech-debt.md)（技术债追踪）、[deployment-guide.md](./deployment-guide.md)

---

## 第1章 认证机制

OpsMesh 采用"网关注入 + 内核二次校验"的纵深防御模型，并内置用户中心支持无网关直连场景。两种 JWT 签名算法互补：

- **RS256（非对称）**：网关签发，内核用公钥验签，用于"网关注入 + 内核二次校验"路径。
- **HS256（对称）**：内核自签自发，用于用户中心登录后下发 token，密钥不外发。

### 1.1 JWT 签发

#### 1.1.1 HS256 内核签发

签发入口 `authctx.SignJWT`（`internal/authctx/jwt_sign.go:58`）：

- **密钥强制非空**：`len(secret) == 0` 返回 error，防配置遗漏导致任何人可伪造 token。
- **默认过期 24h**：`ExpiresAt.IsZero()` 时填充 `now + 24h`，避免永不过期 token 泄露后无法失效。
- **jti 自动生成**：`crypto/rand` 生成 16 字节 hex（32 字符），用于登出吊销。
- **iat/nbf 自动填充**：签发时刻，防回旋攻击。
- **算法固定 HS256**：`jwt.SigningMethodHS256`，防 `alg=none` 降级攻击。

控制面调用入口 `Server.issueUserToken`（`internal/controlplane/auth.go:579`），claims 包含 `sub/user_id/username/roles/permissions/tenant_id/jti/exp/iat`。Access token 实际过期时间 `accessTokenExpiry = 15 * time.Minute`（`auth.go:56`）。

#### 1.1.2 RS256 网关验签

验签入口 `authctx.FromJWT`（"internal/authctx/authctx.go:175`）：

- **算法白名单**：`jwt.WithValidMethods([]string{"RS256"})`，仅接受 RS256。
- **双重算法断言**：keyFunc 内再断言 `t.Method.(*jwt.SigningMethodRSA)`，防 alg 降级。
- **issuer 校验**：配置 `JWTConfig.Issuer` 非空时校验 `iss` claim 必须匹配。
- **过期校验**：jwt/v5 默认校验 `exp`，过期 token 拒绝。
- **claims 提取**：从 `tenant_id/user_id/user_roles` 构造 `authctx.Context`，`user_roles` 兼容字符串数组与逗号分隔字符串两种签发格式。

公钥加载 `LoadJWTPublicKey`（`authctx.go:141`）从 PEM 文件读取 RSA 公钥。

#### 1.1.3 路径选择

`authctx.FromRequest`（`authctx.go:230`）按配置选择身份提取路径：

| 配置 | 携带 token | 行为 |
|------|-----------|------|
| `Enabled=true && PublicKey!=nil` | 是 | 走 JWT 验签，失败返回 error（调用方应 401） |
| `Enabled=true && PublicKey!=nil` | 否 | 回退头注入模式（兼容混合部署） |
| `Enabled=false \|\| PublicKey==nil` | 任意 | 头注入模式（MVP 兼容） |

### 1.2 密码哈希

`hashPassword`（`internal/controlplane/auth.go:306`）使用 bcrypt，`bcryptCost = 12`（生产推荐基线，DefaultCost=10 偏低）：

```go
const bcryptCost = 12
func hashPassword(password string) (string, error) {
    hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
    ...
}
```

`verifyPassword`（`auth.go:315`）调用 `bcrypt.CompareHashAndPassword`，自动适配不同 cost（旧哈希 cost=10 无需迁移）。

### 1.3 强口令校验

`validateStrongPassword`（`auth.go:612`）：

- 至少 8 字符（`changePasswordMinLen = 8`）。
- 必须包含大写字母、小写字母、数字。
- 注册、创建用户、改密均强制校验（`auth_login.go:64`、`auth_users.go:66`、`auth_login.go` 改密 handler）。

### 1.4 默认 admin 弱口令加固

`rotateDefaultAdminPassword`（`auth.go:332`）在非 demo 模式启动时执行：

- 仅当 admin 当前密码仍是弱口令 `admin123`（bcrypt 比对命中）才重置，避免覆盖管理员已改的密码。
- 生成 16 字节 hex 随机密码（`crypto/rand`），bcrypt 哈希后写库。
- 恢复 `MustChangePassword=true`，首登仍强制改密。
- 随机密码一次性打印到日志，提示管理员复制。
- 幂等：SQLStore 持久化后重启不重复重置（密码已非 admin123，bcrypt 比对不命中）。

### 1.5 双 HttpOnly Cookie 方案

`internal/controlplane/auth.go:53` 定义双 Cookie：

| Cookie | 名称 | 寿命 | 用途 |
|--------|------|------|------|
| access token | `opsmesh_at` | 15min | 短期 JWT，仅标识身份，XSS 窃取后利用窗口极小 |
| refresh token | `opsmesh_rt` | 7d | 长期不透明随机串，服务端可吊销/旋转，用于静默续期 |

`setCookie`（`auth.go:63`）统一设置：

- `HttpOnly: true`：防 XSS 读取。
- `SameSite: http.SameSiteLaxMode`：防 CSRF 跨站携带。
- `Secure: s.cookieSecure()`：HTTPS 部署才置 Secure。
- `Path: "/"`：同源由浏览器自动携带。

`cookieSecure`（`auth.go:79`）优先级：`cfg.CookieSecure` 显式 true → true；否则回退 `TLSCert` 非空（HTTPS 直连自动启用）。

### 1.6 Refresh Token 旋转与设备绑定

`createRefreshToken`（`auth.go:142`）：

- `crypto/rand` 生成 32 字节十六进制 token。
- **明文不落库**：库内只存 SHA-256 摘要（`hashRefreshToken`，`auth.go:124`），DB 泄露不等于活体 refresh token 泄露。
- 持久化 `TokenHash + UserID + TenantID + DeviceFP + ExpiresAt`，多副本共享同一 MySQL 时跨副本续期一致。

`consumeRefreshToken`（`auth.go:173`）：

- **原子消费**：`store.ConsumeRefreshToken` 读取+删除在单次互斥操作内完成，防并发双消费。
- **过期校验**：`time.Now().After(rt.ExpiresAt)` 拒绝过期 token。
- **DeviceFP deadline**：超过 `cfg.DeviceFPDeadline` 之后签发的 refresh token 必须绑定 DeviceFP（非空），deadline 前保持向后兼容。
- **设备绑定校验**：存储的 DeviceFP 非空且请求携带 DeviceFP 时，两者必须匹配，防 token 跨设备重放。

### 1.7 JWT 吊销

#### 1.7.1 Access Token 黑名单

`revokeAccessTokenFromRequest`（`auth.go:210`）登出时调用：

- 提取 token（优先 `Authorization: Bearer`，回退 HttpOnly Cookie）。
- `ParseHSJWT` 解析 jti。
- 计算剩余 TTL：`time.Until(claims.ExpiresAt)`，已过期 token 无需吊销。
- `sessionStore.Blacklist(claims.JTI, ttl)` 加入黑名单。

`userFromToken`（`auth.go:646`）校验时检查 `sessionStore.IsBlacklisted(claims.JTI)`，命中则拒绝（`"token has been revoked"`）。

**多副本共享**：黑名单经 `SessionStore` 接口持久化，InProcess 为进程内 map（单副本/demo），Redis 后端时多副本 HA 共享，登出后所有副本立即拒绝该 token。

#### 1.7.2 非活跃用户即时吊销

`userFromToken`（`auth.go:672`）：

```go
if u.Status != "active" {
    return nil, errors.New("user account is not active")
}
```

管理员禁用/删除账号后无需等待 15min 自然过期即收回访问。

### 1.8 改密令牌（首登强制改密）

`createChangePasswordToken`（`auth.go:267`）：

- `crypto/rand` 生成 32 字节十六进制 token。
- 有效期 5 分钟（`changePasswordTokenExpiry = 5 * time.Minute`）。
- 经 `SessionStore` 持久化，多副本共享。

`consumeChangePasswordToken`（`auth.go:282`）一次性消费，校验通过即删除，防重放。

`MustChangePassword=true` 用户登录时不签发 access token，仅签发 changePasswordToken，仅可用于 `/api/v1/auth/change-password`。改密成功后才签发正式 at + rt（`auth_login.go:204`）。

`requirePermission` 中间件（`auth.go:706`）拒绝 `MustChangePassword=true` 用户访问受保护 API，避免弱口令长期在线。

### 1.9 注册审批

`handleAuthRegister`（`auth_login.go:35`）：

- `--public-register=false` 时返回 403 拒绝公开注册（生产默认）。
- `--allow-public-register=true` 时新用户 `Status="active"` 并立即签发 token（仅演示/内网受信环境）。
- 默认 `--allow-public-register=false`：新用户 `Status="pending"`，不签发 token，返回 `201 {"message": "registration submitted, pending admin approval"}`。
- 注册用户硬编码绑定 `role-viewer`，前置校验角色存在性，避免写入无效角色引用。

`handleApproveUser`/`handleRejectUser`（`auth_users.go:156`/`190`）需 `user:approve` 权限，仅 `pending` 状态可审批/拒绝。

### 1.10 防爆破与限流

`loginGuard`（`auth.go:390`）双维度限流：

| 维度 | 机制 | 参数 | 共享 |
|------|------|------|------|
| IP 限流 | 令牌桶 | 容量 5，补充速率 1/6s（≈10/min） | 进程内（多副本各自限流，可接受） |
| 用户名防爆破 | 失败计数 + 锁定 | 5 次失败锁定 15 分钟 | SessionStore 共享（多副本全局一致） |

`recordFail`（`auth.go:450`）失败计数经 `SessionStore.IncrRateLimit` 共享，触发锁定时 `Blacklist(loginLockKey, loginLockDur)` 加入黑名单，多副本下任一副本触发锁定后其他副本也拒绝。

`clientIP`（`auth.go:526`）：`trustProxy=false`（默认，安全）仅用 `RemoteAddr`，防客户端伪造 `X-Forwarded-For` 绕过限流；`trustProxy=true` 信任 XFF 首段（确有可信反代前置时）。

---

## 第2章 授权机制

### 2.1 RBAC 角色/权限/资源映射

#### 2.1.1 权限目录

`rbacPermSpecs`（`internal/store/sql_rbac.go:244`）预置 33 个权限，按资源组分租：

| 资源组 | 权限 | 说明 |
|--------|------|------|
| device | device:read / device:write / device:delete | 设备查看/操作/退役 |
| task | task:read / task:write / task:cancel | 任务查看/下发/取消 |
| alert | alert:read / alert:ack / alert:silence | 告警查看/确认/静默 |
| cmdb | cmdb:read / cmdb:write | 配置项查看/编辑 |
| deploy | deploy:read / deploy:write | 部署查看/执行 |
| workflow | workflow:read / workflow:write | 工作流查看/编辑 |
| log | log:read | 日志查看 |
| audit | audit:read | 审计查看 |
| user | user:read / user:write / user:delete / user:approve | 用户查看/编辑/删除/审批 |
| role | role:read / role:write / role:delete | 角色查看/编辑/删除 |
| federation | federation:read / federation:write | 联邦查看/编辑 |
| os | os:read / os:execute | OS 优化模板查看/执行 |
| middleware | middleware:read / middleware:execute | 中间件模板查看/部署 |
| provision | provision:execute | 自动纳管 |
| k8s | k8s:read / k8s:write / k8s:delete | K8s 集群查看/管理/删除 |

#### 2.1.2 预置角色

`RolePermissions`（`sql_rbac.go:351`）定义角色→权限映射（单一来源，杜绝定义漂移）：

| 角色 | ID | 权限范围 |
|------|----|---------|
| admin | role-admin | 全部 33 个权限 |
| operator | role-operator | device/task/alert/cmdb/deploy/workflow/log/audit/os/middleware/provision 的 read + write/execute，**不含** k8s:write/delete、user/role/federation 的写权限 |
| viewer | role-viewer | 全部资源的 read 权限 |

`seedRBAC`（`sql_rbac.go:286`）幂等预置权限/角色/用户，启动时自动执行。预置用户 admin/operator/viewer 均标记 `must_change_password=1`，首登强制改密（安全债）。

#### 2.1.3 权限展开

`userPermissions`（`auth.go:544`）展开用户经角色获得的全部权限字符串（去重）：

```
用户 → RoleIDs → 各 Role.Permissions → 合并去重
```

### 2.2 鉴权中间件

#### 2.2.1 requirePermission

`requirePermission`（`auth.go:698`）用户中心 JWT 路径：

1. `userFromToken` 提取并验签 JWT，返回用户。
2. **强制改密检查**：`MustChangePassword=true` 拒绝（403），仅 `/api/v1/auth/change-password` 可访问。
3. 展开用户权限，检查是否含 `required` 权限。
4. 不含返回 403 `permission denied: <required>`。

#### 2.2.2 requireProd

`requireProd`（`auth.go:747`）统一产品级 RBAC 闸，兼容三种身份来源：

1. **联邦入站**（`X-Federation-Forwarded=1`）：必须经 `verifyFederationRequest` 验签 HMAC 通过，信任来自可信控制面 peer 的请求（用户级 RBAC 已在来源控制面执行）；验签失败 → 403。
2. **JWT Bearer / Cookie**：走 `requirePermission` 路径。
3. **网关注入 X-User-Roles**：走 `authorizeByRoles` 路径，展开角色名→权限集合后校验。
4. **demo 模式且无身份头**：放行，保持本地一键体验。
5. 其余：401 `missing identity`。

`authorizeByRoles`（`auth.go:782`）动态查询 `store.RolePermissions()`，保证管理员修改角色权限后立即生效（无陈旧缓存）。

#### 2.2.3 状态变更提权

`handleUpdateUser`（`auth_users.go:248`）：

```go
if body.Status != "" {
    if _, ok := s.requirePermission(w, r, "user:approve"); !ok {
        return
    }
}
```

仅 `user:write` 不能激活/禁用账号，须 `user:approve`（与 审批模型一致），防低权限用户自行把 Status 置 active/rejected 绕过审批流。

### 2.3 租户隔离

#### 2.3.1 requireTenantContext

`requireTenantContext`（`internal/controlplane/http_infra.go:38`）行为矩阵（修复 1+2）：

| X-Tenant-ID 头 | Bearer token tenant_id | 行为 |
|----------------|----------------------|------|
| 非空 | 一致 | 返回 actx, true |
| 非空 | 不一致 | 403 Forbidden（防绕过网关伪造租户头） |
| 非空 | 空 | 返回 actx, true（仅头注入，向后兼容） |
| 空 | 非空 | 回退 token 中的 tenant_id |
| 空 | 空 + requireAuth=true | 401 Unauthorized |
| 空 | 空 + demo=true | 自动填充 default/demo |
| 空 | 空 + 其他 | 400 Bad Request |

**安全语义**：Bearer token 中的 tenant_id 与 X-Tenant-ID 头交叉校验，防绕过网关伪造租户头；头空时回退到 token，支持无网关直连场景。

#### 2.3.2 BelongsTo 行级隔离

`authctx.Context.BelongsTo`（`authctx.go:96`）：

```go
func (c Context) BelongsTo(resourceTenant string) bool {
    if c.TenantID == "" {
        return true // 空 tenantID=开发模式，放行全部
    }
    return c.TenantID == resourceTenant
}
```

所有资源查询经此判断过滤，仅返回当前租户的数据。

#### 2.3.3 gRPC 租户校验

`checkAgentTenant`（`grpc.go:169`）gRPC 入站校验：

- `requireAuth` 关闭时放行（开发/内网友好网络降级）。
- `actx.TenantID == ""` 返回 Unauthenticated。
- agent 不存在时不拒绝（交由后续业务逻辑处理）。
- agent 已绑定租户且与 ctx 租户不一致 → PermissionDenied `cross-tenant access denied`。

#### 2.3.4 isAdmin 判定

`isAdmin`（`quota.go:379`）：

```go
func (s *Server) isAdmin(actx authctx.Context) bool {
    for _, r := range roles {
        if r == "admin" || r == "role-admin" {
            return true
        }
    }
    return false
}
```

用于配额管理 API 跨租户访问（admin 可查看/修改任意租户配额，非 admin 仅可查看本租户）。

---

## 第3章 传输安全

### 3.1 TLS 1.2+ 强制

`internal/tlsutil/tlsutil.go` 所有 TLS 配置均显式设置 `MinVersion: tls.VersionTLS12`：

- `ServerCreds`（`tlsutil.go:24`）：gRPC 服务端凭证。
- `ClientCreds`（`tlsutil.go:45`）：gRPC 客户端凭证。
- `HTTPClientTLSConfig`（`tlsutil.go:72`）：联邦出站 HTTP 客户端。
- `HTTPServerTLSConfig`（`tlsutil.go:104`）：联邦入站 HTTP 服务端。
- 热重载模式（`server_netsec.go:50`）：`tlsCfg.MinVersion = tls.VersionTLS12`。

**加固**：禁用 SSLv3/TLSv1.0/TLSv1.1 等弱协议版本；不显式设置 CipherSuites，保留 Go 默认强套件（Go 1.17+ 默认已排除不安全套件）。

### 3.2 mTLS gRPC

`buildGRPC`（`server_netsec.go:35`）构造 gRPC 服务端：

- **热重载模式**（`cfg.TLSWatch=true`）：`tlsutil.NewCertificateReloader` 监听证书文件变更，无需重启服务即可更新 TLS 配置。
- **mTLS**：`clientCA` 非空时要求客户端持证：

```go
tlsCfg.ClientCAs = pool
tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
```

- 日志记录：`logx.Info("gRPC 已启用 TLS", "mtls", s.clientCA != "")`。

### 3.3 联邦独立 mTLS 监听

`buildFederationServer`（`server_netsec.go:154`）构造联邦独立监听：

- 仅暴露联邦必需的入站端点（`/api/v1/tasks` POST、`/api/v1/devices` GET）。
- 强制对端持证：`tlsutil.HTTPServerTLSConfig` 设置 `RequireAndVerifyClientCert`。
- `ReadHeaderTimeout: 10 * time.Second` 防 Slowloris 攻击。

### 3.4 HSTS

`securityHeadersMiddleware`（`server_middleware.go:20`）：

```go
if s.tlsCert != "" {
    w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
}
```

仅 HTTPS 部署（`tlsCert` 非空）时注入，max-age=1 年，含 `includeSubDomains` 覆盖子域。

### 3.5 CSP

`server_middleware.go:45` 每请求生成 16 字节随机 nonce（hex 编码 32 字符），注入 CSP 头：

```
default-src 'self';
script-src 'self' 'nonce-{nonce}';
style-src 'self' 'unsafe-inline' 'nonce-{nonce}';
img-src 'self' data:;
connect-src 'self'
```

**收紧**：

- `script-src` 已移除 `'unsafe-inline'`，仅保留 `'self' + 'nonce-{nonce}'`（个人版前端 v0.6.1 收敛为引导页，企业版 Vue3+Vite 编译产物 `<script>` 均为外部 src 引用）。
- `style-src` 保留 `'unsafe-inline'`：企业版 Vue 组件使用 `:style` 绑定（运行时注入 inline style），style 的 inline 安全风险显著低于 script，保留是可接受的安全取舍。
- 随机数生成失败时回退固定 nonce（仅影响 CSP 强度，不阻断请求）。

### 3.6 其他安全头

| 头 | 值 | 代码位置 | 防护目标 |
|----|----|---------|---------|
| X-Content-Type-Options | nosniff | server_middleware.go:22 | 防 MIME 嗅探 |
| X-Frame-Options | DENY | server_middleware.go:23 | 防点击劫持 |
| Referrer-Policy | no-referrer | server_middleware.go:24 | 防 Referer 泄露 |
| Permissions-Policy | camera=(), microphone=(), geolocation=() | server_middleware.go:26 | 禁用敏感设备权限 |

### 3.7 TLS 证书热重载

`tlsutil.NewCertificateReloader`（`internal/tlsutil/reloader.go`）：

- `--tls-watch=true` 时启用 fsnotify 监听证书文件变更。
- 证书更新后无需重启服务，`GetCertificate` 回调自动返回新证书。
- 优雅关闭：`shutdownTLSReloader`（`server.go:224`）关闭 watcher 与退出 watchLoop goroutine。

---

## 第4章 输入安全

### 4.1 SQL 注入防护

**全量参数化查询**：所有 SQL 操作使用 `?` 占位符 + `ExecContext`/`QueryRowContext`/`QueryContext`，不拼接用户输入到 SQL 字符串。

示例（`internal/store/sql_devices.go:36`）：

```go
qerr := s.db.QueryRowContext(ctx, `SELECT secret FROM agents WHERE agent_id=?`, a.AgentID).Scan(&existingSecret)
```

**Schema 名白名单**：`DefaultSchemaNamer`（`multi_schema.go:45`）对 tenant 做白名单校验，只允许 `[a-zA-Z0-9_]`，含任何其他字符（如 `' ; -- 空格`）直接返回 error，避免拼进 DSN/SQL 造成注入。`validateIdent`（`multi_schema.go:62`）逐字符校验。

**DDL 语句白名单**：`sql.go:489` 索引/列添加等 DDL 语句经 `indexExists`/`columnExists` 检查后执行，表名/列名来自代码常量而非用户输入。

### 4.2 命令注入防护

#### 4.2.1 控制面侧校验

`validateCommand`（`server_tasks.go:42`）控制面侧命令内容校验（安全加固纵深防御）：

- 非空校验。
- 长度 ≤ `maxCommandLen = 4096`，防超长命令撑爆存储/日志或携带二进制载荷。
- 不含危险 shell 元字符：换行符 `\n \r`、分号 `;`、命令替换 `$()` 反引号、单个 `&`（后台执行）。
- 与 agent 端 `checkShellMetachars` 保持一致策略，在控制面侧提前拦截，避免恶意命令进入任务队列。

#### 4.2.2 Agent 侧 shell 白名单

`checkShellWhitelist`（`agent.go:1018`）：

- 白名单为空时放行所有命令（向后兼容，demo/受信内网环境）。
- 白名单非空时，取命令第一个 token 的 basename，检查是否匹配白名单条目。
- **匹配规则**（安全加固修正，防前缀过宽绕过）：
  - 条目以 `*` 结尾（如 `system*`）→ 前缀匹配。
  - 条目不含 `*` → 精确匹配（`ls` 仅匹配 `ls`，不匹配 `lsusb`）。
- **网络诊断命令内置白名单**：`isNetworkDiagnoseCommand`（`agent.go:1071`）放行 ping/traceroute/tracert/nslookup/dig/host/curl/wget/nc/netcat/powershell，即使 `--agent-shell-whitelist` 未显式包含。

#### 4.2.3 Agent 侧元字符拦截

`checkShellMetachars`（`agent.go:966`）纵深防御：

| 元字符 | 拦截理由 | 例外 |
|--------|---------|------|
| `\n` `\r` | 换行/回车符，可拼接任意命令 | 无 |
| `;` | 命令分隔符，可拼接任意命令 | 无 |
| `&` | 单个 & 为后台执行符，可脱离 agent 控制 | `&&`（条件拼接）、`>&`/`&>`（fd 重定向）合法模式 |
| `$(` | 命令替换注入，可执行任意子命令 | 无 |
| 反引号 | 命令替换，可执行任意子命令 | 无 |
| `\|` | 管道符 | **暂不拦截**（合法运维用途多，如 `systemctl status nginx \| grep Active`） |

实现：先剔除合法模式（`>&`/`&>`/`&&`）再检测剩余 `&`。

#### 4.2.4 Service verb 白名单

`execService`（`agent.go:1120`）+ `serviceVerbWhitelist`（`agent.go:1150`）：

```go
var serviceVerbWhitelist = map[string]bool{
    "start": true, "stop": true, "restart": true, "status": true,
    "reload": true, "enable": true, "disable": true,
    "is-active": true, "is-enabled": true,
}
```

verb 解析完成后、执行 systemctl 前校验，拒绝任意 verb 注入（防 `cat /etc/shadow` 等经 Command 字段拼装绕过）。扩展动词需经安全评审后显式新增。

### 4.3 XSS 防护

### 4.3.1 CSP nonce-based

见 §3.5，`script-src` 已移除 `'unsafe-inline'`，仅保留 `'self' + 'nonce-{nonce}'`。

### 4.3.2 HttpOnly Cookie

Access token 与 refresh token 均设置 `HttpOnly: true`，防 XSS 读取 token。

### 4.3.3 审计日志脱敏

`sanitizeAuditDetail`（`server_tasks.go:29`）对写入审计/事件 Detail 的用户输入做脱敏：

- 移除换行符（`\n`→空格、`\r`→删除），防日志注入/解析错位。
- 截断超过 200 字符的内容，避免长命令撑爆日志、可能携带的敏感尾部外泄。

全项目 57 处 `store.Audit()` 调用，所有包含用户输入的 Detail 字段均已使用 `sanitizeAuditDetail` 脱敏（见 [security-issues.md §4](./security-issues.md)）。

### 4.4 CSRF 防护

#### 4.4.1 SameSite Cookie

`setCookie`（`auth.go:63`）统一设置 `SameSite: http.SameSiteLaxMode`，防 CSRF 跨站携带。Lax 模式允许顶层导航携带 Cookie（保持链接跳转体验），但拒绝跨站 POST/PUT/DELETE 携带。

#### 4.4.2 Origin 校验

`csrfOriginCheck`（`server_middleware.go:70`）CSRF Origin 校验中间件：

- **仅状态变更方法**（POST/PUT/DELETE/PATCH）校验；GET/HEAD/OPTIONS 等读方法无 CSRF 风险。
- **demo 模式跳过**（保持本地体验）。
- **AdvertiseAddr 未配置跳过**（开发模式兼容；生产应由 `Validate` 强制配置）。
- **Origin 头为空放行**（同源请求或非浏览器客户端如 curl/agent，不破坏程序化调用）。
- **Origin 非空**：解析其 host:port，与 `cfg.AdvertiseAddr` 的 host:port 比对；不匹配 → 403 Forbidden + 审计日志 `csrf_origin_rejected`。

设计取舍：采用 Origin 头而非 Referer，因 Origin 在跨站 POST 中始终存在且不含路径，比 Referer 更稳定（Referer 可能被 `Referrer-Policy=no-referrer` 剥离）。

### 4.5 请求体大小限制

`maxBodyBytes = 1 << 20`（1 MiB，`http_infra.go:13`），`decodeJSONBody`（`http_infra.go:18`）使用 `http.MaxBytesReader` 约束请求体大小，防超大 body 直接 413，避免 JSON 解析拖垮内存（防 DoS）。

联邦验签同样限读 `maxBodyBytes+1` 防超大请求体内存攻击（`server_netsec.go:242`）。

---

## 第5章 SSRF 防护

### 5.1 私网地址拦截

`isPrivateIP`（`server_security.go:47`）判断 IP 是否为私网/环回/链路本地/元数据地址：

| 地址范围 | 类型 | 拒绝理由 |
|---------|------|---------|
| 127.0.0.0/8 | IPv4 loopback | 环回地址，可访问本机服务 |
| 10.0.0.0/8 | IPv4 私网 A | 内网地址 |
| 172.16.0.0/12 | IPv4 私网 B | 内网地址 |
| 192.168.0.0/16 | IPv4 私网 C | 内网地址 |
| 169.254.0.0/16 | 链路本地 + 云元数据 | 含 169.254.169.254 云元数据端点 |
| 0.0.0.0/8 | 本网/未指定 | 增强，防 0.x.x.x 绕过 SSRF 校验访问本机网络栈 |
| ::1 | IPv6 loopback | IPv6 环回 |
| fe80::/10 | IPv6 link-local | IPv6 链路本地 |
| fc00::/7 | IPv6 ULA | IPv6 私网 |

### 5.2 Webhook URL 校验

`validateURLSSRF`（`server_security.go:14`）旧版无参 SSRF 校验（恒拒私网）：

1. 协议白名单：仅允许 http/https（拒绝 `file://`、`gopher://`、`dict://` 等危险协议）。
2. 主机名非空校验。
3. DNS 解析主机名，校验每个返回 IP 是否为私网地址。

供 `notifyLoop` 启动期校验 `AlertWebhookURL` 与 `AdvertiseAddr`（仅警告不阻止，控制面常部署内网）。

### 5.3 通知渠道 URL 校验

`ValidateWebhookURL`（`server_netsec.go:417`）新版带 `allowPrivate` 参数：

- `allowPrivate=true` 时放行私网地址（用于内网部署场景，如钉钉/飞书内网网关）。
- `allowPrivate=false` 时拒私网地址。
- **DNS 解析超时 5 秒**（`ssrfDNSTimeout`，`server_netsec.go:392`），避免恶意域名拖垮 API。
- **DNS rebinding 防护**：域名解析到内网地址时拒绝（任一 IP 落入私网即拒绝）。

供通知渠道 CRUD（`createNotifyChannel`/`updateNotifyChannel`）保存前校验。

### 5.4 autoProvision CIDR 白名单

`ValidateCIDR`（`server_netsec.go:478`）校验目标 CIDR 是否在允许的白名单内：

- 白名单为空时不校验（向后兼容，由调用方决定是否启用白名单）。
- 目标 CIDR 必须是合法 CIDR 表示。
- **范围包含校验**：目标 CIDR 的起始 IP 与结束 IP 都必须在同一个允许的 CIDR 内（避免目标 CIDR 范围超出白名单，如允许 `10.0.0.0/16` 但目标 `10.0.0.0/8` 应被拒）。

供 `handleAutoProvision`（`server_bootstrap.go:155`）与 `autoProvisionLoop`（`server_bootstrap.go:196`）扫描前校验，防运维误配置或攻击者构造请求扫描任意网段（如扫描 `169.254.169.254` 所在网段获取云元数据）。

### 5.5 密钥测试端点 SSRF 校验

`server_secrets.go:94` 密钥测试端点对 Vault 地址做 SSRF 校验（复用 `validateURLSSRF`），拒绝私网/环回地址。

---

## 第6章 密钥管理

### 6.1 SecretProvider 接口

`internal/secrets/provider.go:29` 定义统一密钥管理抽象：

```go
type SecretProvider interface {
    Get(key string) (string, error)  // 密钥不存在返回 ("", ErrSecretNotFound)
    Name() string                    // 提供者名称（"env"/"file"/"vault"）
}
```

### 6.2 三种密钥来源

#### 6.2.1 EnvProvider

`EnvProvider`（`provider.go:42`）从环境变量读取密钥，可选 prefix（如 `OPSMESH_`）按命名空间隔离。适合 K8s Secret 注入。

#### 6.2.2 FileProvider

`FileProvider`（`provider.go:76`）从 JSON 文件读取密钥：

- JSON 结构：`{"key1":"value1","key2":"value2"}`。
- 支持嵌套：key 用 `/` 分隔，如 `notify/dingtalk/webhook_url` 对应 `{"notify":{"dingtalk":{"webhook_url":"..."}}}`。
- 文件在 `NewFileProvider` 时一次性加载到内存，后续 Get 不再访问磁盘。
- 适合本地开发/CI 流水线。

#### 6.2.3 VaultProvider

`VaultProvider`（`internal/secrets/vault.go:25`）从 HashiCorp Vault KV v2 引擎读取密钥：

- key 格式：`path/to/secret#field`（如 `notify/dingtalk#webhook_url`）。
- 上下文带 10s 超时，避免 Vault 不可达时阻塞调用方。
- Vault API 返回 404 时识别为 `ErrSecretNotFound`。
- 推荐配合 Vault Agent 注入 token，避免在配置文件中硬编码 token。

### 6.3 ChainProvider 链式查找

`ChainProvider`（`provider.go:141`）按优先级依次尝试多个 provider：

- 第一个返回非 `ErrSecretNotFound` 的结果胜出。
- 全部 provider 都返回 `ErrSecretNotFound` 时才返回 `ErrSecretNotFound`。
- 任一 provider 返回其他错误（如 Vault 不可达）则立即返回该错误（不继续尝试后续 provider）。

### 6.4 密钥引用解析

`ResolveSecret`（`provider.go:185`）支持 `${provider:key}` 引用语法，向后兼容明文配置：

- `${vault:notify/dingtalk#secret}`：指定 provider 名称解析。
- `${notify/dingtalk/webhook_url}`：用传入的 provider 解析（默认 provider）。
- 非引用格式（不以 `${` 开头）直接返回明文 value。

`NewServer`（`server.go:291`）调用 `secrets.FromConfig(cfg)` 构造 SecretProvider 并注入到 `alertNotifier`，使渠道构造支持 `${key}` 格式密钥引用解析。构造失败时：生产模式 fail-fast，非生产模式打 Warning 继续。

### 6.5 密钥外置

所有密钥 flag 均支持环境变量注入（见 [security-issues.md §2.1](./security-issues.md)）：

| Flag | 环境变量 | 默认值 |
|------|----------|--------|
| --jwt-secret | OPSMESH_JWT_SECRET | ""（空=随机生成） |
| --federation-secret | OPSMESH_FEDERATION_SECRET | "" |
| --provision-secret | OPSMESH_PROVISION_SECRET | "" |
| --alert-email-pass | OPSMESH_ALERT_EMAIL_PASS | "" |
| --provision-ssh-key-pass | OPSMESH_PROVISION_SSH_KEY_PASS | "" |
| --mysql-dsn | OPSMESH_MYSQL_DSN | "" |
| --redis-addr | OPSMESH_REDIS_ADDR | "" |
| --install-token | OPSMESH_INSTALL_TOKEN | "" |

配置加载采用"flag 优先、env 兜底"语义，生产环境可通过环境变量注入密钥，避免命令行参数泄露（ps 可见）。

### 6.6 token 文件权限 0600

#### 6.6.1 install token

`provision/install.go:47` 安装脚本将 token 写入文件（0600），agent 启动时从 `<dataDir>/install.token` 读取：

```bash
echo -n "$TOKEN" > "$DATA_DIR/install.token"
chmod 600 "$DATA_DIR/install.token"
```

**安全加固**：systemd `ExecStart` 不再包含 `--install-token` 参数（即使指向文件路径也移除），避免 ps 透露 token 文件位置；agent 启动时通过 `--data-dir` 自动查找 `install.token` 文件。

`agent.go:207` agent 端写入 agent ID 文件同样使用 `0o600` 权限。

#### 6.6.2 SSH 私钥

`provision/push_test.go:48` 测试中 SSH 私钥文件以 `0o600` 权限写入，生产部署应确保私钥文件权限为 0600。

### 6.7 kubeconfig 静态加密

`server.go:79` `encryptionKey` 字段（AES-256-GCM，来自 `config.EncryptionKey` base64 解码）：

- `k8s_cluster.go` 的 `encryptKubeconfig`/`decryptKubeconfig` 用此密钥对 kubeconfig 做加解密。
- 空=未配置（非生产模式），加解密退化为明文透传（保持 demo 兼容）。
- 生产模式由 `config.Validate` 强制非空（`config.go:909`），DB 泄露时明文 kubeconfig = 所有 K8s 集群沦陷。

---

## 第7章 审计日志

### 7.1 audit_log 表

`internal/store/migrations/001_initial.sql:78` 定义审计日志表：

```sql
CREATE TABLE IF NOT EXISTS audit_log (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    tenant_id VARCHAR(64),
    user_id VARCHAR(64),
    action VARCHAR(64),
    target VARCHAR(128),
    detail TEXT,
    created_at DATETIME
);
```

`migrations/004_add_audit_trace_id.sql` 增加 `trace_id` 列（VARCHAR(64)，存 32 字符 hex trace_id），使审计日志可与 OTel 链路追踪/日志/SSE 事件跨域关联检索：

```sql
ALTER TABLE audit_log ADD COLUMN trace_id VARCHAR(64);
CREATE INDEX idx_audit_trace ON audit_log (trace_id);
```

### 7.2 写入接口

`SQLStore.Audit`（`internal/store/sql_audits.go:16`）：

- 自动填充 `CreatedAt`（UTC）。
- `columnExists` 检测 `trace_id` 列是否存在，存在时写入 trace_id，不存在时降级到无 trace_id 写入（兼容老库未迁移场景）。
- 参数化 INSERT，防 SQL 注入。

`Server.audit`（`server.go:518`）与 `grpcServerImpl.audit`（`grpc.go:267`）helper 从 ctx 提取 OTel trace_id 注入 `AuditEvent.TraceID`，然后转发到 `store.Audit`，使审计日志与链路追踪关联。

### 7.3 覆盖度

全项目 57 处 `store.Audit()` 调用，覆盖所有敏感写操作（见 [security-issues.md §4.5](./security-issues.md)）：

| 操作类型 | Action | 覆盖状态 |
|---------|---------|---------|
| 登录/登出/注册/改密 | user_login/logout/register/change_password | ✅ |
| 用户 CRUD + 审批/拒绝 | user_create/update/delete/approve/reject | ✅ |
| 角色 CRUD | role_create/update/delete | ✅ |
| 任务 CRUD + 审批/拒绝/取消 | create_task/cancel_task/approve_task/reject_task | ✅ |
| 设备退役 + agent 注册 | retire_device/register | ✅ |
| 部署/OS 优化/K8s 管理 | deploy_middleware/execute_os_template/k8s_cluster_* | ✅ |
| 告警 ack/silence/规则创建 | ack_alert/silence_alert/create_alert_rule | ✅ |
| 联邦转发 | federation_forward_task | ✅ |
| bootstrap 访问 | bootstrap_install_sh/serve_agent | ✅ |
| CSRF 拒绝 | csrf_origin_rejected | ✅ |
| token 校验失败 | register_token_rejected | ✅ |

### 7.4 脱敏

`sanitizeAuditDetail`（`server_tasks.go:29`）对所有包含用户输入的 Detail 字段脱敏：

- 移除换行符（`\n`→空格、`\r`→删除），防日志注入/解析错位。
- 截断超过 200 字符的内容，避免长命令撑爆日志、可能携带的敏感尾部外泄。

### 7.5 检索

`QueryAudits`（`sql_audits.go:84`）按租户/动作/时间窗过滤审计事件（审计可查；等保三级留痕必须可检索）：

- `tenant`/`action` 为空表示不限。
- `since`/`until` 为零值表示不限。
- `limit <= 0` 表示不限制。
- 返回按时间倒序。

### 7.6 不可篡改

审计日志采用只追加（INSERT）模式，不提供 UPDATE/DELETE 接口。`audit_log` 表无对应更新/删除方法，确保日志一旦写入不可篡改。生产环境建议配合 MySQL binlog 或 WORM 存储进一步加固。

---

## 第8章 租户隔离

### 8.1 行级隔离（tenant_id）

所有业务表均含 `tenant_id VARCHAR(64)` 列（`migrations/001_initial.sql`），按租户过滤：

- `Snapshot(tenantID)`、`AllTasks(tenantID)`、`Alerts(tenantID)` 等方法显式按 tenant 过滤。
- 索引 `idx_tenant` 加速按租户过滤（`migrations/005_m2_alert_governance.sql:36`）。
- `BelongsTo`（`authctx.go:96`）行级隔离判断。

### 8.2 Schema 隔离（multi-schema）

`MultiSchemaStore`（`internal/store/multi_schema.go:92`）多租户 schema 隔离存储：

- 每个租户路由到独立的 `*SQLStore`（独立 schema/database），实现物理级数据隔离。
- **路由策略**：
  - 显式 `tenantID` 参数的方法直接路由。
  - payload 内含 `TenantID` 的方法从 payload 提取。
  - 无 tenant 上下文的方法经反查索引（`agentTenant`/`deviceTenant`/`taskTenant`）定位租户。
  - 跨租户聚合方法遍历所有 schema 求和/合并。
- **惰性创建**：第一次访问某 tenant 时创建对应的 SQLStore（建表）。
- **隔离边界优势**：
  - 单租户故障/误删不影响其他租户。
  - 单租户可独立备份/迁移/清理。
  - 跨租户查询必须显式聚合，避免误漏 tenant_id 过滤导致越权。

启用方式：`--multi-schema=true --schema-prefix=opsmesh_tenant_`（`config.go:192`），最终 schema 名 = `SchemaPrefix + tenantID`。

### 8.3 Schema 名 SQL 注入防护

`DefaultSchemaNamer`（`multi_schema.go:45`）对 tenant 做白名单校验：

- 只允许 `[a-zA-Z0-9_]`，含任何其他字符（如 `' ; -- 空格`）直接返回 error。
- prefix 本身也做同样校验，防止运维配置的 prefix 含非法字符。
- `validateIdent`（`multi_schema.go:62`）逐字符校验，非法字符不会拼进 DSN/SQL。

### 8.4 配额限制

`QuotaManager`（`internal/controlplane/quota.go`）租户级资源配额管理：

- `--quota-enabled=true` 时构造，启用后设备/任务/告警创建路径调用 `CheckDevice`/`CheckTask`/`CheckAlert` 校验是否超额。
- 配额配置表 `quota_configs`（`migrations/006_quota_configs.sql`），按 `tenant_id` 主键。
- API 路由 `/api/v1/quotas[/{tenantID}]`，admin 可跨租户管理，非 admin 仅可查看本租户。
- `isAdmin`（`quota.go:379`）判定 admin/role-admin 角色。

### 8.5 gRPC 租户校验

`checkAgentTenant`（`grpc.go:169`）gRPC 入站校验 agent 归属租户，跨租户访问返回 PermissionDenied（见 §2.3.3）。

### 8.6 Bearer token 与头交叉校验

`requireTenantContext`（`http_infra.go:38`）校验 X-Tenant-ID 头与 Bearer token 中的 tenant_id 一致，防绕过网关伪造租户头（见 §2.3.1）。

---

## 第9章 联邦安全

### 9.1 联邦验签（HMAC）

`verifyFederationRequest`（`server_netsec.go:216`）校验入站请求的联邦签名：

1. **标记检查**：仅当请求携带 `X-Federation-Forwarded=1` 标记时才验签；未携带则视为普通网关注入请求。
2. **密钥检查**：`cfg.FederationSecret == ""` 返回 error（已由 `Validate` 强制非空）。
3. **签名头提取**：`X-Federation-Ts`（时间戳）+ `X-Federation-Sig`（签名）。
4. **时间戳校验**：`federationSigMaxSkew = 5 * 60`（±5min），防重放。
5. **请求体纳入签名**：`sha256(body)` 摘要防中间人篡改转发任务体。
6. **签名计算**：`HMAC-SHA256(secret, method|path|ts|tenant|user|roles|sha256(body))`。
7. **常量时间比对**：`hmac.Equal` 防时序侧信道。

### 9.2 跨网段 mTLS

#### 9.2.1 出站 mTLS

`FederationManager`（`federation.go:59`）持有 `tlsConfig`，非空时出站请求走 mTLS：

- 呈现客户端证书（防伪 peer）。
- 校验证书链（防 MITM）。
- `HTTPClientTLSConfig`（`tlsutil.go:68`）构造，强制 TLS 1.2+。

#### 9.2.2 入站 mTLS

`buildFederationServer`（`server_netsec.go:154`）联邦独立监听强制对端持证：

- `HTTPServerTLSConfig` 设置 `RequireAndVerifyClientCert`。
- 仅暴露联邦必需端点（`/api/v1/tasks` POST、`/api/v1/devices` GET）。
- `ReadHeaderTimeout: 10s` 防 Slowloris。

### 9.3 requireProd 联邦路径

`requireProd`（`auth.go:747`）联邦入站路径：

```go
if r.Header.Get("X-Federation-Forwarded") == "1" {
    if err := s.verifyFederationRequest(r); err != nil {
        writeJSON(w, http.StatusForbidden, ...)
        return nil, false
    }
    return nil, true
}
```

原实现仅判断头存在即放行，未验签，任意客户端伪造 `X-Federation-Forwarded=1` 即可绕过 RBAC。现必须验签 HMAC 通过才信任 peer（用户 RBAC 已在来源侧执行）。

### 9.4 联邦配置校验

`config.Validate`（`config.go:933`）启动期校验：

- peer 地址必须是合法 URL（含 scheme + host），fail-fast。
- `--federation-port>0` 但 `--federation-tls-cert/key` 为空 → 拒绝（独立 mTLS 监听需要服务端证书）。
- 启用联邦但缺失共享密钥 → 拒绝（强校验，防跨不可信网段伪造租户身份头）。

---

## 第10章 Agent 安全

### 10.1 安装 token 一次性

`grpc.go:78` Register 时校验 install token：

```go
devID, tokTenant, tokOK := g.store.ConsumeToken(info.InstallToken)
if !tokOK {
    g.audit(ctx, &proto.AuditEvent{Action: "register_token_rejected", ...})
    return nil, status.Error(codes.Unauthenticated, "invalid or expired install token")
}
dom.OnboardDeviceID = devID
dom.TenantID = tokTenant // token 权威：纳管设备归属以 token 内租户为准
```

- `ConsumeToken` 一次性消费，校验通过即标记 consumed，防重放。
- token 权威：纳管设备归属以 token 内租户为准，agent 不可伪造所属租户。
- 无 token 时 `OnboardDeviceID` 显式清空，agent 自报该字段一律不信任。

### 10.2 SSH known_hosts 强制

`provision.PushAndExec`（`internal/provision/provision.go:88`）：

- `knownHostsPath` 非空时调用 `knownHostsCallback`（`provision.go:19`）加载主机公钥，校验 SSH 连接的主机指纹。
- `knownHostsPath` 为空时回退 `ssh.InsecureIgnoreHostKey()` 并打印显眼警告（仅开发/内网调试）。
- **MITM 风险说明**：无主机指纹校验时，攻击者可劫持 SSH 连接注入恶意 agent 二进制（供应链 RCE）。
- **Production 模式强制**：`auto.go` 在 `--production=true` 且 `--provision-ssh-known-hosts` 为空时拒绝 SSH 推送，绝不应用于生产。

`knownHostsCallback` 支持：

- 非哈希的 known_hosts 格式（`hostname key-type base64-key`），不支持 `|1|hash` 格式。
- 通配符匹配：`*.example.com` 匹配 `sub.example.com`。
- 多主机名条目（逗号分隔）。

### 10.3 shell 白名单

见 §4.2.2，`checkShellWhitelist`（`agent.go:1018`）按 `--agent-shell-whitelist` 配置校验命令。

### 10.4 元字符拦截

见 §4.2.3，`checkShellMetachars`（`agent.go:966`）拦截危险 shell 元字符。

### 10.5 service' verb 白名单

见 §4.2.4，`serviceVerbWhitelist`（`agent.go:1150`）限制 systemctl 动词。

### 10.6 文件遍历防护

`execFile`（`agent.go:1168`）原子写入文件，路径遍历防护：

1. **Clean 后残留 `..` 检查**：`filepath.Clean(t.Path)` 后仍含 `..` 说明试图逃逸根目录，拒绝。纯相对路径如 `../../etc/passwd` Clean 后仍含 `..`，拒绝。
2. **绝对路径解析**：`filepath.Abs` 内部再 Clean 一次，确保路径规范。
3. **符号链接拒绝**：`os.Lstat` 检查 `os.ModeSymlink`，避免经符号链接逃逸到任意路径。
4. **根目录白名单**：`checkFileRootWhitelist`（`agent.go:1224`）按 `--agent-file-root-whitelist` 校验路径是否落在允许的根目录之下（`filepath.Rel` 判断相对路径不以 `..` 开头）。白名单为空时不限制（向后兼容，仍拒绝 `../` 与符号链接）。

### 10.7 输出截断

`maxOutputBytes = 10 * 1024 * 1024`（10MB，`agent.go:900`），`limitedBuffer`（`agent.go:904`）限制单个任务 stdout/stderr 内存占用，超过即截断并追加提示 `...[output truncated at 10MB]...`，避免 cat 大文件耗尽 agent 内存。

### 10.8 gRPC HMAC 签名

`verifyAgentSignature`（`grpc.go:205`）gRPC agent 身份绑定：

- `--grpc-require-signature=true` 时，agent 必须在 gRPC metadata 中携带：
  - `agent-timestamp`：签名生成时刻（Unix 秒）。
  - `agent-signature`：`HMAC-SHA256(secret, timestamp + agentID)` 的 hex 编码。
- **时间戳校验**：`agentSignatureMaxSkew = 5 * time.Minute`，超过 5 分钟偏移视为重放/过期，拒绝。
- **预共享密钥优先**：`signatureKey`（`--grpc-signature-key`）非空时优先使用，为空时回退 `store.AgentSecret(agentID)`（向后兼容）。
- **常量时间比对**：`hmac.Equal` 防时序侧信道。
- **Register 不下发密钥**：`Register` 响应 `Secret` 字段始终为空，防注册不硬时密钥外泄。控制面与 agent 两侧通过 `--grpc-signature-key` 手动配置同一密钥。
- **生产模式默认开启**：`config.go:695` Production 模式且未显式设置时默认开启（纵深防御）。

### 10.9 进程组隔离

`executeShell`（`agent.go:1090`）：

- `setProcessGroup(cmd)` 平台特定：Linux/Darwin 设 `Setpgid=true`，使子进程成为新进程组 leader。
- ctx 取消/超时时杀整个进程组（包括子进程 fork 出的后台进程），避免孤儿后台进程继续运行。
- Windows 上 `Setpgid` 无效，取消时用 `cmd.Process.Kill()` 杀父进程 + `taskkill /T /F /PID` 杀进程树。

### 10.10 bootstrap token 校验

`verifyBootstrapToken`（`server_bootstrap.go:22`）校验 agent 分发端点（`/install.sh`、`/bin/opsmesh-agent`）的访问令牌：

- demo 模式放宽（本地体验不要求 token）。
- 否则接受 `?token=xxx` 查询参数 或 `Authorization: Bearer xxx` 头。
- 与 `cfg.ProvisionSecret` 做 `hmac.Equal` 常量时间比对，防时序侧信道。
- 无 token 或 token 不匹配 → 401 Unauthorized。

安全加固：原端点完全开放，任何人可下载 agent 二进制与安装脚本，存在供应链投毒风险。

---

## 第11章 已知安全配置

### 11.1 Production 模式强制项清单

`config.Validate`（`internal/config/config.go:848`）+ Load 时自动启用（`config.go:673`）：

| 强制项 | 校验位置 | 说明 |
|--------|---------|------|
| TLS 证书必配 | `config.go:895` | `Production && TLSCert == ""` 拒绝启动，明文通信不满足等保三级 |
| JWT 密钥必配 + ≥32 字节 | `config.go:901-905` | 防各副本独立随机密钥互相不认、重启后会话失效 |
| EncryptionKey 必配 | `config.go:909` | kubeconfig 明文存储不满足等保三级 |
| require-auth 默认开启 | `config.go:674` | Production 且未显式设置时默认 true |
| grpc-require-signature 默认开启 | `config.go:695` | 纵深防御，防冒领任务/伪造上报 |
| cookie-secure 默认开启 | `config.go:701` | HTTPS 反代终止 TLS 时须显式开启 |
| PublicRegister 默认关闭 | `config.go:686` | 生产模式默认拒绝公开注册 |
| PublicRegister + AllowPublicRegister 互斥 | `config.go:704` | Production && PublicRegister 拒绝（防免审批开放注册） |
| memory store 强告警 | `config.go:710` | 生产模式 + store=memory 告警（数据不持久化） |
| FederationSecret 必配 | `config.go:964` | 启用联邦但缺失共享密钥拒绝启动 |
| FederationPort mTLS 证书必配 | `config.go:959` | 独立 mTLS 监听需要服务端证书 |
| MultiSchema 仅 mysql | `config.go:930` | 多 schema 隔离仅支持 mysql 后端 |
| memory store 多副本拒绝 | `config.go:880` | `store=memory && replicas>1` 拒绝（数据分裂） |
| metrics CIDR 白名单格式校验 | `config.go:944` | 每项必须是合法 CIDR，启动 fail-fast |
| ProvisionCIDRWhitelist 格式校验 | `config.go:1000` | 每项必须是合法 CIDR，启动 fail-fast |
| SessionStore 格式校验 | `config.go:969` | 须为 `redis://host:port` 格式 |

### 11.2 已知漏洞与处置

见 [security-issues.md](./security-issues.md)：

| 漏洞 | 模块 | 处置 |
|------|------|------|
| GO-2026-4503 | filippo.io/edwards25519 v1.1.0 | ✅ 已升级到 v1.1.1 |
| GO-2026-5932 | golang.org/x/crypto/openpgp（已弃用） | ⚠️ 项目代码未调用，实际风险为 0 |

`govulncheck ./...` 项目代码直接调用的漏洞数为 0。

---

## 第12章 安全检查清单

### 12.1 部署前必检项

#### 12.1.1 配置检查

- [ ] `--production=true` 已设置。
- [ ] `--tls-cert` / `--tls-key` 已配置有效证书（非自签或已更新 CA 信任）。
- [ ] `--client-ca` 已配置（启用 mTLS）。
- [ ] `--jwt-secret` 已配置 ≥32 字节强随机密钥（`openssl rand -hex 32`）。
- [ ] `--encryption-key` 已配置 base64 编码的 32 字节 AES-256 密钥（`openssl rand 32 \| base64`）。
- [ ] `--provision-secret` 已配置强随机 token。
- [ ] `--provision-ssh-known-hosts` 已配置（生产必须，防 SSH MITM）。
- [ ] `--cookie-secure=true`（HTTPS 反代终止 TLS 时显式开启）。
- [ ] `--public-register=false`（生产默认，拒绝公开注册）。
- [ ] `--grpc-require-signature=true` + `--grpc-signature-key` 已配置预共享密钥。
- [ ] `--store=mysql` + `--mysql-dsn` 已配置（非 memory store）。
- [ ] `--session-store=redis://host:port` 已配置（多副本 HA 共享会话状态）。
- [ ] `--metrics-allow-cidr` 已配置（限制 metrics 端点访问来源）。
- [ ] `--provision-cidr-whitelist` 已配置（限制 autoProvision 扫描网段）。
- [ ] `--advertise-addr` 已配置（CSRF Origin 校验需要）。

#### 12.1.2 联邦检查（如启用）

- [ ] `--federation-secret` 已配置强随机共享密钥。
- [ ] `--federation-tls-cert` / `--federation-tls-key` / `--federation-ca` 已配置 mTLS 凭证。
- [ ] `--federation-port` 已配置独立监听端口。
- [ ] 所有 peer 地址格式合法（含 scheme + host）。

#### 12.1.3 多租户检查（如启用）

- [ ] `--multi-schema=true` + `--store=mysql`。
- [ ] `--schema-prefix` 仅含 `[a-zA-Z0-9_]`（SQL 注入防护）。
- [ ] `--quota-enabled=true`（配额限制）。

#### 12.1.4 密钥外置检查（如启用）

- [ ] `--secret-provider` 已配置（env/file/vault/chain）。
- [ ] Vault token 从环境变量 `OPSMESH_VAULT_TOKEN` 注入（非配置文件硬编码）。
- [ ] 通知渠道密钥使用 `${provider:key}` 引用格式（非明文）。

### 12.2 启动期校验

`config.Validate`（`config.go:848`）启动期自动校验以下项，失败即 `os.Exit(1)`：

- [ ] `--mode` 合法（controlplane 或 agent）。
- [ ] 端口范围 1-65535。
- [ ] `--store=mysql` 时 `--mysql-dsn` 非空。
- [ ] `--task-lease-sec > 0`。
- [ ] `store=memory && replicas>1` 拒绝。
- [ ] `--discover` 时 `--segment-cidr` 合法 CIDR。
- [ ] Production 模式 TLS/JWT/EncryptionKey 必配。
- [ ] `--log-backend` 合法（memory/sql/loki/es）。
- [ ] `--multi-schema` 时 `--store=mysql`。
- [ ] 联邦 peer 地址合法 URL。
- [ ] `--metrics-allow-cidr` 每项合法 CIDR。
- [ ] `--federation-port` 范围 + mTLS 证书。
- [ ] `--federation-peers` 非空时 `--federation-secret` 必配。
- [ ] `--session-store` 格式 `redis://host:port`。
- [ ] 通知渠道配置完整（type/webhook_url/smtp_*）。
- [ ] `--provision-cidr-whitelist` 每项合法 CIDR。
- [ ] `--inhibit-rules-file` 文件存在可读。
- [ ] `--log-push-enabled` 时 files/endpoint 非空 + backend 合法。

### 12.3 运行期检查

- [ ] `govulncheck ./...` 项目代码 0 漏洞。
- [ ] `go vet ./...` 无警告。
- [ ] `golangci-lint run` 无错误（按 `.golangci.yml` 配置）。
- [ ] 默认 admin 密码已修改（首登强制改密 + 启动期随机替换）。
- [ ] 所有审计日志 Detail 字段已脱敏（`sanitizeAuditDetail`）。
- [ ] 所有 SQL 查询使用参数化（无字符串拼接用户输入）。
- [ ] 所有密钥 flag 支持环境变量注入（无命令行明文密钥）。
- [ ] Cookie 设置 `HttpOnly + SameSite=Lax + Secure`。
- [ ] 安全头注入完整（CSP nonce + HSTS + X-Frame-Options + X-Content-Type-Options + Referrer-Policy + Permissions-Policy）。
- [ ] gRPC 启用 mTLS + HMAC 签名。
- [ ] 联邦通道启用 mTLS + HMAC 验签。
- [ ] Agent 启用 shell 白名单 + 元字符拦截 + 文件根目录白名单。
- [ ] SSH 推送启用 known_hosts 校验。
- [ ] install token 文件权限 0600。

### 12.4 网络层检查

- [ ] 控制面不直接暴露（必须经网关/APISIX/IAM 前置）。
- [ ] 网关剥离客户端自带的 `X-Tenant-ID`，重注入经鉴权的真实租户。
- [ ] 网络策略拒绝直连控制面（绕过网关）的请求。
- [ ] metrics 端点（9091）仅内网访问（CIDR 白名单）。
- [ ] 联邦端口独立监听，网络策略限制来源 IP。
- [ ] Agent 出站仅允许控制面 gRPC 端口（白名单出站）。

### 12.5 监控检查

- [ ] 审计日志写入监控（`audit_log` 表增长速率）。
- [ ] 登录失败告警（`loginGuard.recordFail` 触发锁定）。
- [ ] CSRF 拒绝告警（`csrf_origin_rejected` 审计事件）。
- [ ] 联邦验签失败告警（`federation signature verification failed`）。
- [ ] gRPC 签名失败告警（`agent-signature mismatch`）。
- [ ] SSRF 校验失败告警（webhook URL / CIDR 白名单拒绝）。
- [ ] TLS 证书过期告警（热重载 + 监控证书剩余天数）。
- [ ] JWT 黑名单增长率监控（异常登出活动）。

---

## 附录 A：代码位置索引

| 安全机制 | 代码位置 |
|---------|---------|
| JWT RS256 验签 | `internal/authctx/authctx.go:175` |
| JWT HS256 签发/验签 | `internal/authctx/jwt_sign.go:58`/`104` |
| bcrypt 密码哈希 | `internal/controlplane/auth.go:306` |
| 双 HttpOnly Cookie | `internal/controlplane/auth.go:53`/`63` |
| Refresh token 旋转 | `internal/controlplane/auth.go:142`/`173` |
| JWT 黑名单吊销 | `internal/controlplane/auth.go:210`/`646` |
| 改密令牌 | `internal/controlplane/auth.go:267`/`282` |
| 默认 admin 弱口令加固 | `internal/controlplane/auth.go:332` |
| loginGuard 防爆破 | `internal/controlplane/auth.go:390` |
| 强口令校验 | `internal/controlplane/auth.go:612` |
| requirePermission | `internal/controlplane/auth.go:698` |
| requireProd | `internal/controlplane/auth.go:747` |
| requireTenantContext | `internal/controlplane/http_infra.go:38` |
| isAdmin | `internal/controlplane/quota.go:379` |
| RBAC 权限目录 | `internal/store/sql_rbac.go:244` |
| seedRBAC | `internal/store/sql_rbac.go:286` |
| RolePermissions | `internal/store/sql_rbac.go:351` |
| 安全头中间件 | `internal/controlplane/server_middleware.go:20` |
| CSRF Origin 校验 | `internal/controlplane/server_middleware.go:70` |
| TLS 1.2+ 强制 | `internal/tlsutil/tlsutil.go:24`/`45`/`72`/`104` |
| mTLS gRPC | `internal/controlplane/server_netsec.go:35` |
| 联邦 mTLS 监听 | `internal/controlplane/server_netsec.go:154` |
| 联邦验签 | `internal/controlplane/server_netsec.go:216` |
| gRPC HMAC 签名 | `internal/controlplane/grpc.go:205` |
| gRPC 租户校验 | `internal/controlplane/grpc.go:169` |
| SSRF 防护 | `internal/controlplane/server_security.go:14`/`47` |
| Webhook URL 校验 | `internal/controlplane/server_netsec.go:417` |
| CIDR 白名单校验 | `internal/controlplane/server_netsec.go:478` |
| SecretProvider 接口 | `internal/secrets/provider.go:29` |
| VaultProvider | `internal/secrets/vault.go:25` |
| 密钥引用解析 | `internal/secrets/provider.go:185` |
| install token 0600 | `internal/provision/install.go:47` |
| SSH known_hosts | `internal/provision/provision.go:19`/`88` |
| shell 白名单 | `internal/agent/agent.go:1018` |
| shell 元字符拦截 | `internal/agent/agent.go:966` |
| service verb 白名单 | `internal/agent/agent.go:1150` |
| 文件遍历防护 | `internal/agent/agent.go:1168`/`1224` |
| 输出截断 | `internal/agent/agent.go:900` |
| 审计日志写入 | `internal/store/sql_audits.go:16` |
| 审计脱敏 | `internal/controlplane/server_tasks.go:29` |
| 审计检索 | `internal/store/sql_audits.go:84` |
| MultiSchemaStore | `internal/store/multi_schema.go:92` |
| Schema 名 SQL 注入防护 | `internal/store/multi_schema.go:45`/`62` |
| QuotaManager | `internal/controlplane/quota.go` |
| Production 模式校验 | `internal/config/config.go:848` |
| bootstrap token 校验 | `internal/controlplane/server_bootstrap.go:22` |
| install token 消费 | `internal/controlplane/grpc.go:78` |

---

## 附录 B：威胁模型与防护对照

| 威胁（OWASP Top 10） | 防护机制 | 代码位置 |
|---------------------|---------|---------|
| A01 失效的访问控制 | RBAC + 租户隔离 + isAdmin | §2, §8 |
| A02 加密失败 | TLS 1.2+ + mTLS + AES-256-GCM + bcrypt cost=12 | §3, §1.2, §6.7 |
| A03 注入 | SQL 参数化 + 命令白名单 + 元字符拦截 + Schema 名白名单 | §4.1, §4.2, §8.3 |
| A04 不安全设计 | 双 Cookie + token 旋转 + 设备绑定 + jti 黑名单 | §1.5, §1.6, §1.7 |
| A05 安全配置错误 | Production 模式强制 + Validate 启动期校验 | §11, §12.2 |
| A06 易受攻击的组件 | govulncheck=0 + 依赖升级 | §11.2 |
| A07 身份认证失败 | JWT 双模式 + 防爆破 + 强口令 + 注册审批 | §1 |
| A08 软件与数据完整性失败 | CSP nonce + 联邦 HMAC 验签 | §3.5, §9.1 |
| A09 安全日志与监控失败 | 57 处审计调用 + sanitizeAuditDetail + trace_id 关联 | §7 |
| A10 服务端请求伪造 | validateURLSSRF + ValidateWebhookURL + ValidateCIDR | §5 |