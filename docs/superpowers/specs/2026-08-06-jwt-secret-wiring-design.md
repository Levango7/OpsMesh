# 设计：JWT 签发密钥注入接线 + 生产强制校验

- 日期：2026-08-06
- 状态：待审查
- 关联：task 94（JWT 存储加固）遗留的部署链路漏洞

## 背景与问题

复核 task 87~94 加固批次时发现，task 94 的 JWT 存储加固只改了前端（localStorage → HttpOnly Cookie），
但**用户中心 JWT 签发密钥（`config.JWTSecret`，HS256）在生产三条部署路径上均无注入点**：

- Helm：`controlplane-deployment.yaml` 的 env 只有 `PROVISION_SECRET`/`MYSQL_DSN`；`secret.yaml`、`configmap.yaml`
  均无 JWT 条目；`values-*.yaml` 无 `jwtSecret` 字段（对比 `provisionSecret` 在 values-production.yaml:36-37 有显式注入注释）。
- systemd：`deploy/systemd/opsmesh-controlplane.env` 全文无 `OPSMESH_JWT_SECRET`。
- docker-compose：同上。

**后果**：`server.go:152` 在 `cfg.JWTSecret == ""` 时对每副本各自 `crypto/rand` 随机 32 字节。
- 多副本（`values-production.yaml:19` 默认 `replicaCount: 3`）各自为政 → A 副本签发的 token 到 B 副本验签必失败
  （`auth.go:588` 用本副本 `s.jwtSecret`）→ 用户间歇性 401/被踢登录（K8s Service 默认无会话保持，加剧间歇性）。
- 即便单副本，随机密钥导致进程重启后所有已签发会话失效（task 94 的 Cookie 会话被直接波及）。

本质：demo 密钥（Makefile/start.bat）进不了生产是真的（Go 代码路径无硬编码），
但"生产已强制环境变量"不成立——生产根本没有强制，也没有注入入口。

## 目标

1. 为生产三条部署路径提供 JWT 密钥显式注入点。
2. 生产控制面模式**强制**稳定密钥：存在 + 长度 ≥32，否则 fail-fast。
3. 密钥**一次生成、永久复用**，普通 `helm upgrade` 不轮换、不踢出会话。
4. 补齐运维文档（生成/注入/轮换/多副本一致性）。
5. 覆盖回归测试。

## 方案（A+B 组合）

### A. Helm chart 接线 + 保持

**值新增（values.yaml 与 values-production.yaml 的 `controlplane` 段）：**
```yaml
# 用户中心 JWT 签发密钥（HS256，多副本必须一致）。
# 空=helm 在首次安装生成随机 32 字节并固化，初始化后改它只会在显式变更时轮换。
jwtSecret: ""
```

**`templates/secret.yaml` 新增键（用 `lookup` 跨 upgrade 保持）：**
```yaml
{{- $secretName := printf "%s-secret" (include "opsmesh.fullname" .) }}
{{- $existing := "" }}
{{- if lookup "v1" "Secret" .Release.Namespace $secretName }}
{{-   $existing = (lookup "v1" "Secret" .Release.Namespace $secretName).data.jwt-secret }}
{{- end }}
# JWT 签发密钥：显式值 > 已有 Secret（保持） > 首次随机。upgrade 天然不轮换。
jwt-secret: {{ if .Values.controlplane.jwtSecret }}{{ .Values.controlplane.jwtSecret }}{{ else if $existing }}{{ $existing }}{{ else }}{{ randAlphaNum 32 }}{{ end }}
```
- `lookup` 仅在集群内 upgrade/install 时返回已有数据；`helm template`（dry-run/CI 渲染）无集群时返回空，回退随机，仍可渲染。
- 关键即："显式值 > 已有 Secret > 随机"，普通 upgrade 走"已有 Secret"分支 → 不重生成、不轮换。
- 配合 deployment 已有 `checksum/secret` 注解（deployment:29），仅当 Secret 内容确实变化才触发滚动。

**`templates/controlplane-deployment.yaml` env 段新增：**
```yaml
- name: OPSMESH_JWT_SECRET
  valueFrom:
    secretKeyRef:
      name: {{ include "opsmesh.fullname" . }}-secret
      key: jwt-secret
```
（`config.go:343` 已支持 env `OPSMESH_JWT_SECRET`，无需改 flag 端传到 arg；env 注入即生效。）

**为什么不用 `randAlphaNum 32` 直接默认（初稿方案）？—— 否决原因记录：**
`provision-secret` 的 `randAlpha 32` 注解自述"每次 helm upgrade 轮换"。
若照搬到 JWT 上：每次 upgrade 都会轮换密钥 → 所有 Cookie 会话失效、全量踢登录，这是不可接受的——它是有状态凭证，非安装一次性口令。因此引入 `lookup` 复用策略。

### B. 生产强制校验（`internal/config/config.go` `Validate()`）

```go
// M3-2Ab 生产控制面必须配置稳定 JWT 密钥。
// 语义：单副本随机重启丢会话、多副本各自不认——生产均不可接受，直接 fail-fast。
if c.Production && c.JWTSecret == "" {
    return fmt.Errorf("生产模式（--production=true）controlplane 必须设置 --jwt-secret（或环境变量 OPSMESH_JWT_SECRET）；否则各副本独立随机密钥互相不认、重启后会话全部失效")
}
if c.Production && len([]byte(c.JWTSecret)) < 32 {
    return fmt.Errorf("生产模式 --jwt-secret 长度不足（%d 字节 < 32）：需强随机 256-bit 对称密钥（建议 openssl rand -hex 32）", len([]byte(c.JWTSecret)))
}
```
- 仅 `Production` 生效，dev/单副本随机语义（config.go:127/152）保留。
- 与 H6 生产强 TLS fail-fast（config.go:521-523）同风格。

### C. systemd / docker-compose 补环境占位

- `deploy/systemd/opsmesh-controlplane.env`：加注释说明生产必须 `OPSMESH_JWT_SECRET=<openssl rand -hex 32>`（空则启动即被 Validate 拒绝，`fail`）。
- `docker-compose.yaml`：controlplane service 加 `OPSMESH_JWT_SECRET` 环境变量占位 + 注释。

### D. 文档（README / deploy 说明）

- 生成：`openssl rand -hex 32`
- 注入三路径（helm `--set controlplane.jwtSecret=...` / systemd env / compose env）
- 多副本一致性提醒（所有副本读同一 Secret）
- 轮换副作用：改密钥即全员重登
- 长度 ≥32 的要求

### E. 测试（`internal/config/config_test.go`）

- `TestValidate_ProductionControlplaneRequiresJWTSecret`：prod+空 → err；prod+有 → nil；非 prod+空 → nil（回归现有语义）。
- `TestValidate_ProductionJWTSecretLength`：prod+长度 <32 → err；≥32 → nil。
- 需要控制 `Mode` 为 controlplane 的构造（沿用现有 `TestValidate_ProductionRejectsNoTLS` 的组装方式）。

### F. 范围边界（明确不做）

- 不引入会话保持（K8s）；密钥一致后多副本天然可用。
- 不重构 `JWTPublicKey`（网关 RS256 验签，另一话题）。
- 不缩 `server/client` 的随机兜底（保留 dev 便利）。

## 文件清单

修改：`deploy/helm/opsmesh/values.yaml`、`values-production.yaml`、`templates/secret.yaml`、`templates/controlplane-deployment.yaml`、`internal/config/config.go`、`internal/config/config_test.go`、`deploy/systemd/opsmesh-controlplane.env`、`docker-compose.yaml`、`README.md`

## 自检

- 占位符：无 TODO/待定。
- 一致性：fail 的 `Production` 强制与文档一致；`lookup` 与 deployment、密钥与`checksum` 逻辑一致。
- 范围聚焦单实现批次，无外部仓依赖（charts/ 仓库缺失的 H19 不在本 design）。倒是：`M3-2Ab` 编号为本地锚点，非产品 backlog 编号。
- 模糊性：`升32字节` 用 `len([]byte())` 计数，统一以字节计。

（待用户审查后进入实施计划）