# OpsMesh 安全审计报告

> 审计日期：2026-08-07  
> 审计范围：依赖漏洞扫描、密钥管理审计、API 限流策略、审计日志覆盖度  
> 审计工具：govulncheck v1.1.4 + 手动代码审计

---

## 1. 依赖漏洞扫描

### 1.1 扫描结果

使用 `govulncheck v1.1.4` 对项目全量扫描，结果如下：

| 类别 | 数量 | 说明 |
|------|------|------|
| 项目代码直接调用的漏洞 | 0 | 项目代码未调用任何漏洞函数 |
| 导入包级别的漏洞 | 1 | filippo.io/edwards25519 |
| 模块级别的漏洞 | 1 | golang.org/x/crypto/openpgp（已弃用包） |

### 1.2 漏洞详情与处置

#### 漏洞 1：GO-2026-4503（已修复）

| 项目 | 内容 |
|------|------|
| CVE/GO-ID | GO-2026-4503 |
| 模块 | filippo.io/edwards25519 |
| 影响版本 | v1.1.0 |
| 修复版本 | v1.1.1 |
| 描述 | Invalid result or undefined behavior in filippo.io/edwards25519 |
| 详情 | https://pkg.go.dev/vuln/GO-2026-4503 |
| 处置 | ✅ 已升级到 v1.1.1（`go get filippo.io/edwards25519@v1.1.1`） |
| 项目代码是否调用 | 否（间接依赖，项目代码未直接使用 edwards25519） |

#### 漏洞 2：GO-2026-5932（无法立即修复，记录跟踪）

| 项目 | 内容 |
|------|------|
| CVE/GO-ID | GO-2026-5932 |
| 模块 | golang.org/x/crypto |
| 影响版本 | v0.54.0 |
| 修复版本 | N/A（包已弃用，无修复版本） |
| 描述 | The golang.org/x/crypto/openpgp package is unmaintained, unsafe by design, and has known security issues |
| 详情 | https://pkg.go.dev/vuln/GO-2026-5932 |
| 处置 | ⚠️ 无法通过升级修复（openpgp 包已弃用） |
| 项目代码是否调用 | 否（项目代码未使用 openpgp 包，仅作为间接依赖存在） |
| 缓解措施 | 项目代码未调用 openpgp 包的任何函数，实际风险为 0。建议后续清理 go.mod 间接依赖时关注此包是否可移除。 |

### 1.3 验证

修复后重新运行 `govulncheck ./...`，项目代码直接调用的漏洞数为 0。

---

## 2. 密钥管理审计

### 2.1 密钥环境变量注入支持

审计 `internal/config/config.go` 中所有密钥/密码相关的 flag，确认是否支持环境变量注入：

| Flag | 环境变量 | 支持状态 | 默认值 |
|------|----------|----------|--------|
| --jwt-secret | OPSMESH_JWT_SECRET | ✅ 已支持 | ""（空=随机生成） |
| --federation-secret | OPSMESH_FEDERATION_SECRET | ✅ 已支持 | "" |
| --provision-secret | OPSMESH_PROVISION_SECRET | ✅ 已支持 | "" |
| --alert-email-pass | OPSMESH_ALERT_EMAIL_PASS | ✅ 已支持 | "" |
| --provision-ssh-key-pass | OPSMESH_PROVISION_SSH_KEY_PASS | ✅ 已支持 | "" |
| --mysql-dsn | OPSMESH_MYSQL_DSN | ✅ 已支持 | "" |
| --redis-addr | OPSMESH_REDIS_ADDR | ✅ 已支持 | "" |
| --install-token | OPSMESH_INSTALL_TOKEN | ✅ 已支持 | "" |

**结论**：上述 8 个密钥类 flag 均支持环境变量注入，无需修改。

### 2.2 硬编码密钥检查

使用 grep 搜索生产代码中的硬编码密钥（password、secret、key、token 等）：

| 检查项 | 结果 |
|--------|------|
| 生产代码硬编码密钥 | ✅ 未发现 |
| 默认密钥（changeme/default/admin 等） | ✅ 未发现 |
| 测试代码中的测试密钥 | 正常（测试用，不影响生产） |

**结论**：所有密钥 flag 的默认值均为空字符串（安全），无硬编码密钥风险。

### 2.3 配置加载机制

配置加载采用"flag 优先、env 兜底"的正确语义：
- 显式设置的 flag 优先级最高
- 未显式设置时回退到环境变量
- 环境变量未设置时回退到默认值

此机制确保生产环境可通过环境变量注入密钥，避免命令行参数泄露（ps 可见）。

---

## 3. API 限流策略审计

### 3.1 限流实现概览

`loginGuard`（`internal/controlplane/auth.go`）提供双维度限流：

| 维度 | 机制 | 参数 |
|------|------|------|
| IP 限流 | 令牌桶 | 容量 5，补充速率 1/6s（≈10/min） |
| 用户名防爆破 | 失败计数 + 锁定 | 5 次失败锁定 15 分钟 |

### 3.2 各 API 限流覆盖情况

| API | IP 限流 | 用户名防爆破 | 状态 |
|-----|--------|-------------|------|
| POST /api/v1/auth/login | ✅ | ✅ | 已实现（IP + 用户名双维度） |
| POST /api/v1/auth/register | ✅ | N/A | 已实现（防批量注册） |
| POST /api/v1/auth/change-password | ✅ | N/A | ✅ 本次新增（防暴力破解旧密码） |
| POST /api/v1/auth/refresh | ❌ | N/A | 未限流（rt Cookie 有设备绑定保护） |
| POST /api/v1/auth/logout | ❌ | N/A | 未限流（无安全风险） |

### 3.3 本次改进

**新增 change-password IP 限流**：

`handleAuthChangePassword` 原无限流，可被已登录用户暴力破解旧密码。本次复用 `loginGuard` 的 IP 令牌桶添加限流：

```go
// P1-4 限流：按客户端 IP 令牌桶约束改密频率，防暴力破解旧密码。
if !s.loginGuard.allow(clientIP(r, s.cfg.TrustProxy)) {
    writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many requests, slow down"})
    return
}
```

### 3.4 全局 QPS 限流

当前未实现全局 QPS 限流中间件。中间件链为：
```
recoveryMiddleware(securityHeadersMiddleware(jsonErrorMux))
```

**未添加全局 QPS 限流的原因**：
- 内部 API（agent 心跳、任务拉取）有高频合法请求，全局限流可能误伤
- 多副本 HA 部署下进程内限流效果有限，需 Redis 共享计数
- 敏感 API（登录/注册/改密）已有个体级限流，足以防护关键攻击面
- 建议后续在网关层（如 APISIX/Nginx）实现全局 QPS 限流

### 3.5 密码重置 API

项目无独立的密码重置 API（forgot-password/reset-password），仅有 change-password API（需鉴权 + 旧密码校验），无需额外限流。

---

## 4. 审计日志覆盖度检查

### 4.1 sanitizeAuditDetail 函数

`internal/controlplane/server_tasks.go` 中的 `sanitizeAuditDetail` 函数对审计日志 Detail 字段做脱敏：

| 脱敏项 | 说明 |
|--------|------|
| 移除换行符 | `\n`→空格、`\r`→删除，防日志注入/解析错位 |
| 截断长文本 | 超过 200 字符截断 + "..."，防日志撑爆 + 敏感尾部外泄 |

### 4.2 审计日志调用覆盖度

全项目共 57 处 `store.Audit()` 调用。审计后修复以下未脱敏调用：

| 文件 | 修复数量 | 涉及的用户输入 |
|------|----------|---------------|
| auth_login.go | 4 处 | username（注册/登录/登出/改密） |
| auth_users.go | 3 处 | username + reject reason（创建/审批/拒绝用户） |
| auth_roles.go | 1 处 | role.Name（创建角色） |
| server_alerts.go | 2 处 | body.Comment + rule 字段（静默告警/创建告警规则） |
| federation.go | 1 处 | body.PeerURL + body.Task.AgentID（联邦转发） |
| middleware_deploy.go | 5 处 | tpl.Name + body 字段（模板 CRUD + 部署/卸载） |
| os_optimize.go | 4 处 | tpl.Name + body.AgentID（模板 CRUD + 执行） |
| k8s_cluster.go | 2 处 | c.Name（创建/删除集群） |
| **合计** | **22 处** | |

### 4.3 已脱敏的调用（修复前已正确使用 sanitizeAuditDetail）

| 文件 | 数量 | 涉及的用户输入 |
|------|------|---------------|
| server_tasks.go | 5 处 | body.Command + body.Reason（创建/批量/拒绝任务） |

### 4.4 无需脱敏的调用（固定字符串或数字）

| 文件 | 数量 | 说明 |
|------|------|------|
| server_tasks.go | 2 处 | "cancelled via HTTP" / "approved via HTTP" |
| server_devices.go | 2 处 | "retired via HTTP" / "token issued via HTTP" |
| auth_users.go | 2 处 | "updated via HTTP" / "deleted via HTTP" |
| auth_roles.go | 2 处 | "updated via HTTP" / "deleted via HTTP" |
| server_alerts.go | 1 处 | "acknowledged via HTTP" |
| server.go | 3 处 | r.RemoteAddr + 数字统计 |
| k8s_manage.go | 3 处 | 数字（replicas/revision）+ 时间戳 |
| grpc.go | 2 处 | 固定字符串 + 数字（exitCode） |
| sql_devices.go | 1 处 | 无 Detail 字段 |

### 4.5 审计日志操作覆盖度

审计日志覆盖以下敏感操作：

| 操作类型 | Action | 覆盖状态 |
|----------|--------|----------|
| 登录 | user_login | ✅ |
| 登出 | user_logout | ✅ |
| 注册 | user_register | ✅ |
| 改密 | user_change_password | ✅ |
| 创建用户 | user_create | ✅ |
| 审批用户 | user_approve | ✅ |
| 拒绝用户 | user_reject | ✅ |
| 更新用户 | user_update | ✅ |
| 删除用户 | user_delete | ✅ |
| 创建角色 | role_create | ✅ |
| 更新角色 | role_update | ✅ |
| 删除角色 | role_delete | ✅ |
| 创建任务 | create_task | ✅ |
| 取消任务 | cancel_task | ✅ |
| 审批任务 | approve_task | ✅ |
| 拒绝任务 | reject_task | ✅ |
| 设备退役 | retire_device | ✅ |
| agent 注册 | register | ✅ |
| 部署操作 | deploy_middleware / execute_os_template | ✅ |
| K8s 集群管理 | k8s_cluster_create / k8s_cluster_delete | ✅ |
| 告警操作 | ack_alert / silence_alert / create_alert_rule | ✅ |
| 联邦转发 | federation_forward_task | ✅ |

**结论**：审计日志覆盖所有敏感操作，且所有包含用户输入的 Detail 字段均已使用 `sanitizeAuditDetail` 脱敏。

---

## 5. 验证结果

| 验证项 | 命令 | 结果 |
|--------|------|------|
| 编译 | `go build ./...` | ✅ 通过 |
| 测试 | `go test -timeout 180s ./...` | ✅ 全部通过 |
| 静态检查 | `go vet ./...` | ✅ 无警告 |
| 漏洞扫描 | `govulncheck ./...` | ✅ 项目代码 0 漏洞 |

---

## 6. 修改文件清单

| 文件 | 修改内容 |
|------|----------|
| go.mod / go.sum | 升级 filippo.io/edwards25519 v1.1.0 → v1.1.1 |
| internal/controlplane/auth_login.go | 新增 change-password IP 限流；4 处审计日志脱敏 |
| internal/controlplane/auth_users.go | 3 处审计日志脱敏 |
| internal/controlplane/auth_roles.go | 1 处审计日志脱敏 |
| internal/controlplane/server_alerts.go | 2 处审计日志脱敏 |
| internal/controlplane/federation.go | 1 处审计日志脱敏 |
| internal/controlplane/middleware_deploy.go | 5 处审计日志脱敏 |
| internal/controlplane/os_optimize.go | 4 处审计日志脱敏 |
| internal/controlplane/k8s_cluster.go | 2 处审计日志脱敏 |
| internal/controlplane/auth_test.go | 修复 TestChangePasswordWeakNew 限流适配 |
| docs/security-issues.md | 新增本安全审计报告 |

---

## 7. 后续建议

1. **openpgp 包清理**：GO-2026-5932 无法通过升级修复，建议后续清理 go.mod 间接依赖时关注此包是否可移除。
2. **多副本限流**：当前 loginGuard 为进程内实现，多副本 HA 部署下各副本独立计数。建议后续以 Redis 共享计数实现跨副本限流。
3. **全局 QPS 限流**：建议在网关层（APISIX/Nginx）实现全局 QPS 限流，保护控制面免受 DoS 攻击。
4. **审计日志脱敏自动化**：建议后续考虑在 store.Audit 层统一注入 sanitizeAuditDetail，避免新增审计调用时遗漏脱敏。