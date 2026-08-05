# JWT 生产密钥接线与强制校验 实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。

**目标：** 为生产三路径（Helm/systemd/docker-compose）提供 JWT 签发密钥显式注入，并在生产控制面模式强制稳定密钥（存在 + 长度≥32，否则 fail-fast）。

**架构：** Helm 用 `lookup` 复用已有 Secret 实现"一次生成、upgrade 不轮换"；`stringData` 存明文（取回时 `b64dec`）。`config.Validate()` 仿 H6 生产强 TLS 语法，加生产 JWT 强制与长度校验。systemd/compose 补环境占位，README 补运维指引。

**技术栈：** Go（config 校验）、Helm 模板（lookup/b64dec/randAlphaNum）、YAML。

**前置依赖**：`docs/superpowers/specs/2026-08-06-jwt-secret-wiring-design.md`（已批准，commit 0744146）。

---

## 文件结构

| 文件 | 操作 | 职责 |
|------|------|------|
| `internal/config/config.go` | 修改 | `Validate()` 新增生产 JWT 强制 + 长度校验 |
| `internal/config/config_test.go` | 修改 | 新增两个回归测试 |
| `deploy/helm/opsmesh/values.yaml` | 修改 | `controlplane` 段加 `jwtSecret` |
| `deploy/helm/opsmesh/values-production.yaml` | 修改 | `controlplane` 段加 `jwtSecret` + 注入注释 |
| `deploy/helm/opsmesh/templates/secret.yaml` | 修改 | `lookup` 复用 + `jwt-secret` 键 |
| `deploy/helm/opsmesh/templates/controlplane-deployment.yaml` | 修改 | env 段挂 `OPSMESH_JWT_SECRET` |
| `deploy/systemd/opsmesh-controlplane.env` | 修改 | 加 `OPSMESH_JWT_SECRET` 注释占位 |
| `docker-compose.yaml` | 修改 | controlplane 加 `OPSMESH_JWT_SECRET` 占位+注释 |
| `README.md` | 修改 | 补 JWT 密钥运维指引 |

依赖顺序：任务 1 独立可测，任务 2/3/4 构成 Helm 接线（3 是核心），任务 5 与二进制的部署文档，任务 6 收尾。

## 环境说明（关于验证）

本机无 GitOps 上的 `charts/`，无安装 "helm" 二进制；`helm template`（dry-run）依赖集群或已有 `lookup` 返回数据。接线验证采用：
- Go 层：`go test ./internal/config/` 跑真实编译。
- Helm 层：模板逻辑以人工核对 + 部署环境（有 helm cluster）渲染验证注释（无 helm 则文档内备注）。

---

### 任务 1：生产 JWT 密钥强制校验（`internal/config/config.go`）

**文件：**
- 修改：`internal/config/config.go`（`Validate()`，H6 TLS 校验之后）
- 测试：`internal/config/config_test.go`

- [ ] **步骤 1：编写失败的测试**

在 `internal/config/config/config.go` 的测试文件 `internal/config/config_test.go` 末尾追加两个测试（沿用 `base()` / `TestValidate_ProductionRejectsNoTLS` 组装风格）：

```go
// TestValidate_ProductionControlplaneRequiresJWTSecret：生产 controlplane 必须显式注入 JWT 密钥。
// 空 => 各副本独立随机密钥互不相认 + 重启丢会话（fail-fast）；非 production 保留随机兜底。
func TestValidate_ProductionControlplaneRequiresJWTSecret(t *testing.T) {
	// prod + 空密钥 -> 拒绝
	c := base()
	c.Production = true
	c.TLSCert = "tls.crt" // 绕过 H6 TLS，聚焦 JWT 校验
	if err := c.Validate(); err == nil {
		t.Fatal("production + 空 jwt-secret 应被拒绝，但 Validate 通过了")
	}
	// prod + 合法密钥 -> 通过
	c2 := base()
	c2.Production = true
	c2.TLSCert = "tls.crt"
	c2.JWTSecret = "0123456789abcdef0123456789abcdef" // 32 字节
	if err := c2.Validate(); err != nil {
		t.Fatalf("production + 合法 jwt-secret 应通过: %v", err)
	}
	// 非 prod + 空密钥 -> 通过（dev 随机兜底保留）
	c3 := base()
	c3.Production = false
	c3.JWTSecret = ""
	if err := c3.Validate(); err != nil {
		t.Fatalf("非 production + 空 jwt-secret 应通过: %v", err)
	}
}

// TestValidate_ProductionJWTSecretLength：生产 jwt-secret 过短（<32 字节）必须拒绝。
func TestValidate_ProductionJWTSecretLength(t *testing.T) {
	c := base()
	c.Production = true
	c.TLSCert = "tls.crt"
	c.JWTSecret = "tooshort" // 8 字节 < 32
	if err := c.Validate(); err == nil {
		t.Fatal("production + (<32) jwt-secret 应被拒绝，但通过了")
	}
	c2 := base()
	c2.Production = true
	c2.TLSCert = "tls.crt"
	c2.JWTSecret = "0123456789abcdef0123456789abcdef" // 32 字节
	if err := c2.Validate(); err != nil {
		t.Fatalf("production + 32 字节 jwt-secret 应通过: %v", err)
	}
}
```

- [ ] **步骤 2：运行测试验证失败**

运行：`go test ./internal/config/ -run 'TestValidate_ProductionJWTSecret' -v`
预期：FAIL（当前 `Validate()` 无该校验，prod+空 会通过而测试期望拒绝）。

- [ ] **步骤 3：编写最少实现（Validate() 内插入）**

在 `internal/config/config.go` 的 `Validate()` 中 H6 生产 TLS 块（`config.go:521` 附近的 `if c.Production && c.TLSCert == ""`）之后插入：

```go
	// M3-2Ab 生产控制面必须配置稳定 JWT 密钥（task 96）。
	// 语义：控机单密钥为空重启丢令牌，多副本各自独立随机密钥互不相认、
	// 用户间歇 401（auth.go 本副本验签）。生产直接 fail-fast，与 H6 生产强 TLS 同风格。
	if c.Production && c.JWTSecret == "" {
		return fmt.Errorf("生产模式（--production=true）controlplane 必须设置 --jwt-secret（或环境变量 OPSMESH_JWT_SECRET）；否则各副本独立随机密钥互相不认、重启后会话全部失效")
	}
	if c.Production && len([]byte(c.JWTSecret)) < 32 {
		return fmt.Errorf("生产模式 --jwt-secret 长度不足（%d 字节 < 32）：需强随机 256-bit 对称密钥（建议 openssl rand -hex 32）", len([]byte(c.JWTSecret)))
	}
```

> 按字节计 `len([]byte())`；仅作用于 `Production`（dev 随机兜底 config.go:117/152 保留）。

- [ ] **步骤 4：运行测试验证通过**

运行：`go test ./internal/config/ ./internal/controlplane/ && go test ./internal/config/ -run TestValidate -v`
预期：新增两个 PASS，既有 `TestValidate_*` 全 PASS（无 `Production=true` 且期望通过的既有用例受影响）。

- [ ] **步骤 5：Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): 生产 JWT 密钥强制 + 长度校验（task 96）"
```

---

### 任务 2：`values` 新增 `controlplane.jwtSecret`

**文件：**
- 修改：`deploy/helm/opsmesh/values.yaml`（`controlplane` 段）
- 修改：`deploy/helm/opsmesh/values-production.yaml`（`controlplane` 段）

- [ ] **步骤 1：values.yaml 加字段**

在 `deploy/helm/opsmesh/values.yaml` 的 `controlplane.provisionSecret` 同级（2 空格缩进）之后追加：

```yaml
  # 用户中心 JWT 签发密钥（HS256，多副本必须一致）。空=helm 首次安装随机生成并固化。
  # 生产务必 --set 注入，长度≥32 字节。
  jwtSecret: ""
```

- [ ] **步骤 2：values-production.yaml 加字段**

在 `values-production.yaml` 的 `controlplane:` 段 `provisionSecret: ""`（第 37 行）之后追加：

```yaml
  # jwtSecret 务必显式注入（多副本需一致），与 provisionSecret 同法：
  # helm install ... --set controlplane.jwtSecret=$(openssl rand -hex 32)
  jwtSecret: ""
```

- [ ] **步骤 3：YAML 可读性验证**

本机无 helm；用可解析工具人工核对（或 python3 -c yaml 校验）。预期：两个 `jwtSecret` 都在 `controlplane:` 内、缩进 2 空格、值空字符串，YAML 语法好。

- [ ] **步骤 4：Commit**

```bash
git add deploy/helm/opsmesh/values.yaml deploy/helm/opsmesh/values-production.yaml
git commit -m "feat(helm): 新增 controlplane.jwtSecret 值（task 96）"
```

---

### 任务 3：Secret 模板 `lookup` 复用 + `jwt-secret` 键

**文件：**
- 修改：`deploy/helm/opsmesh/templates/secret.yaml`

前提：任务 2 的 `.Values.controlplane.jwtSecret`。

- [ ] **步骤 1：顶部加模板变量**

在 `templates/secret.yaml` 第 1 行 `apiVersion:` 之前插入：

```yaml
{{- $secretName := printf "%s-secret" (include "opsmesh.fullname" .) }}
{{- $existingSecret := lookup "v1" "Secret" .Release.Namespace $secretName }}
{{- $existing := "" }}
{{- if $existingSecret }}
{{-   $existing = index $existingSecret.data "jwt-secret" | default "" | b64dec }}
{{- end }}
```

- [ ] **步骤 2：stringData 内加 jwt-secret 键**

在 `stringData:` 内 `provision-secret` 键（第 11 行）之后插入：

```yaml
  # jwt-secret：用户中心 JWT 签发密钥（HS256，多副本须一致）。
  # 优先级：显式值 > 已有 Secret（upgrade 复用，不轮换） > 首次随机。
  # lookup 返回 data 为 base64，必须 b64dec 还原明文再放 stringData（避免双编码）。
  jwt-secret: {{ if .Values.controlplane.jwtSecret }}{{ .Values.controlplane.jwtSecret }}{{ else if $existing }}{{ $existing }}{{ else }}{{ randAlphaNum 32 }}{{ end }}
```

- [ ] **步骤 3：人工核对优先级分支**

`{{ if .Values.controlplane.jwtSecret }}`（显式）→ `{{ else if $existing }}`（复用）→ `{{ else }}`（首次随机）。`$existing` 已经 `b64dec` 明文，放入 `stringData` 正确。`provision-secret` 行保持。

- [ ] **步骤 4：Commit**

```bash
git add deploy/helm/opsmesh/templates/secret.yaml
git commit -m "feat(helm): secret 模板 jwt-secret 键（lookup 保持 upgrade 不轮换）（task 96）"
```

---

### 任务 4：Deployment 挂载 `OPSMESH_JWT_SECRET`

**文件：**
- 修改：`deploy/helm/opsmesh/templates/controlplane-deployment.yaml`（`env` 段）

前提：任务 3（`jwt-secret` 键）。

- [ ] **步骤 1：env 段新增**

在 `controlplane-deployment.yaml` 的 `env:` 中 `OPSMESH_PROVISION_SECRET` 的 `secretKeyRef`（第 89-93 行）之后追加：

```yaml
            - name: OPSMESH_JWT_SECRET
              valueFrom:
                secretKeyRef:
                  name: {{ include "opsmesh.fullname" . }}-secret
                  key: jwt-secret
```

（flag 端无需改动：`config.Load` 已支持 env `OPSMESH_JWT_SECRET`，注入 env 即生效。）

- [ ] **步骤 2：人工核对引用一致性**

`secretKeyRef.name` 用 `{{ include "opsmesh.fullname" . }}-secret`（与 secret.yaml 同名）；`key: jwt-secret`（与 stringData 键一致）。整体由现有 `checksum/secret` 注解（第 29 行）驱动变更滚动。

- [ ] **步骤 3：Commit**

```bash
git add deploy/helm/opsmesh/templates/controlplane-deployment.yaml
git commit -m "feat(helm): 挂载 OPSMESH_JWT_SECRET 环境变量（task 96）"
```

---

### 任务 5：systemd / docker-compose 环境占位

**文件：**
- 修改：`deploy/systemd/opsmesh-controlplane.env`
- 修改：`docker-compose.yaml`

- [ ] **步骤 1：systemd .env 追加**

在 `deploy/systemd/opsmesh-controlplane.env` 末尾追加：

```text
# JWT 签发密钥（HS256，生产须一致）。生产必须注入，否则 OPSMESH_PRODUCTION=true 启动被 Validate 拒绝。
# 生成：openssl rand -hex 32
OPSMESH_JWT_SECRET=
```

> 该 .env 已设 `OPSMESH_PRODUCTION=true`（第 17 行），若留空，`Validate` 生产 JWT 校验将拦截启动（fail 符合设计）；填入实际值则通过。

- [ ] **步骤 2：docker-compose 追加**

在 `docker-compose.yaml` 的 `controlplane:` 服务新增 `environment` 段（置于 `command`/`ports` 之后）：

```yaml
    # 生产 JWT 密钥：本一键体验环境为开发演示，可留空（dev 随机兜底）；
    # 若当生产使用，务必注入 32 字节强随机密钥（openssl rand -hex 32）。
    environment:
      OPSMESH_JWT_SECRET: ${OPSMESH_JWT_SECRET:-}
```

> compose 未设 `--production`，留空不触发 Validate 拦截，符合其"开发演示"定位。

- [ ] **步骤 3：Commit**

```bash
git add docker-compose.yaml deploy/systemd/opsmesh-controlplane.env
git commit -m "chore: JWT 密钥注入（systemd/compose）占位（task 96）"
```

---

### 任务 6：README 运维指引

**文件：**
- 修改：`README.md`

- [ ] **步骤 1：在恰当章节插入指引**

在 `README.md` 部署/配置 section（如 Line 159 附近 helm install 指引之后）增补：

```markdown
## JWT 签发密钥运维（生产）

用户中心 JWT 签发密钥对应 `OPSMESH_JWT_SECRET`。生产建议开启，否则重启后会话全部失效，且多副本
各自独立随机密钥互相不认、用户间歇 401（`server.go` 空密钥时 crypto/rand 随机）。

- **生成高强度密钥**：`openssl rand -hex 32`（≥32 字节）
- **Helm 注入**：`helm install opsmesh ./deploy/helm/opsmesh -f values-production.yaml \
    --set controlplane.jwtSecret="$(openssl rand -hex 32)"`
- **systemd**：在 `deploy/systemd/opsmesh-controlplane.env` 设定 `OPSMESH_JWT_SECRET=...`
- **docker-compose**：设 `OPSMESH_JWT_SECRET` 环境变量（生产请显式注入）
- **强制要求**：`--production` 控制面缺少 `--jwt-secret` 或长度 <32 字节时启动直接 fail-fast（Validate），请避免。
- **多副本一致性**：所有副本必须读取同一 Secret 密钥，否则方签名 token 互不相认。
- **轮换副作用**：修改密钥后所有已签发 token 全部失效，用户需重新登录（预期行为）。
```

- [ ] **步骤 2：Commit**

```bash
git add README.md
git commit -m "docs: JWT 运维指引（生产）"
```

---

## 自检

**规格覆盖：** 每条规格 → 任务：
- 注入点（Helm/systemd/compose）→ 任务 2/3/4 + 任务 5
- 强制存在 + ≥32 字节 fail-fast → 任务 1
- 一次生成、upgrade 不轮换 → 任务 3（`lookup`）
- 运维文档 → 任务 6
- 回归测试 → 任务 1
- 范围边界：不增加措施、不重构 JWTPublicKey、不缩 dev 随机 → 全计划未触及，符合规格 F。

**占位符扫描：** 各步骤代码块完整，无"待定/TODO"。生成的 stdlib 密钥占位均给出明确说明（`<...>` 为生产生成示例）。

**类型一致性：** `jwtSecret` 从 values（任务 2）→ secret.yaml 模板（任务 3）→ deployment env（任务 4）三处变量名一致；`OPSMESH_JWT_SECRET` 从 deployment env 到 config 读取（`val` 的 env key）一致；`jwt-secret` 键名在 secret.yaml `stringData` 与 deployment `secretKeyRef.key` 一致（同一 Secret 对象 `{{ fullname }}-secret` + 键 `jwt-secret`）。测试用 `base()` / `Validate()` 方法，与既有 `TestValidate_OK` / `TestValidate_ProductionRejectsNoTLS` 一致。

**环境限制：** 本机无 helm、无 python、无集群/已有 Secret，故「helm 渲染验证」以"人工核对 + 部署环境验证"标注，不阻塞 Go 层测试；已在计划开头"环境说明"注明。任务 3/4 模板验证标注为人工核对而非自动测试。

**待办顺序：** 任务 5/6 为文档/环境占位，无依赖，可并行；计划按顺序列出以保证整体一致性。

---

计划已完成并保存到 `docs/superpowers/plans/2026-08-06-jwt-secret-wiring.md`。两种执行方式：

**1. 子代理驱动（推荐）** - 每个任务调度一个新的子代理，任务间进行审查，快速迭代

**2. 内联执行** - 在当前会话中使用 executing-plans 执行任务，批量执行并设有检查点

**选哪种方式？**