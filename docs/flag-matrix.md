# OpsMesh 配置项治理矩阵（flag-matrix）

> 目的：约束 135 个 flag 的语义边界、组合约束与生产默认值，避免"能配但不知道该怎么配"。
> 权威定义在 `internal/config/config.go` 与 `Config.Validate()`（启动期 fail-fast 校验）。
> 本文件从属于 README「配置参考」，只记录**组合约束**与**默认值差异**，不再单独罗列字段。

---

## 1. 治理原则

| 原则 | 说明 |
|---|---|
| fail-fast > 警告 > 静默 | 会在运行期造成诡异错误的组合，启动即拒绝；影响合规但不阻塞功能的，启动警告（stderr）。 |
| flag 优先 / env 兜底 | 每个 flag 都有 `OPSMESH_*` 同名兜底；命令行显式值永远覆盖 env。 |
| 多副本一致性 | 任何"对称密钥"类字段（JWT、HMAC、AES、provision）跨副本**必须字面相等**，否则对应功能间断性失败（详见 §4）。 |

---

## 2. 按"必须先想清楚"分层的 Core / Advanced

仅 12 个 flag 属于 **Core**——部署任何一个非 demo 环境前必须有答案：

| Flag | 为什么 Core |
|---|---|
| `--mode` | 决定进程是控制面还是 agent，一切行为分叉于此 |
| `--store` | 数据落 memory 还是 mysql，决定数据丢失面与多副本可行性 |
| `--mysql-dsn` | store=mysql 命脉，空则拒绝启动 |
| `--production` | 一键切换整套安全基线（TLS 强制 / require-auth / cookie-secure / grpc 签名） |
| `--jwt-secret` | 用户中心会话命脉；多副本必须一致 |
| `--provision-secret` | 纳管 install token 签名密钥；多副本必须一致 |
| `--advertise-addr` | 拼接 bootstrap URL；错则 agent 拿不到安装脚本 |
| `--require-auth` | 是否信任网关注入身份头；生产默认 true |
| `--tls-cert` / `--tls-key` / `--client-ca` | agent↔控制面通信加密；生产强制 |
| `--http-port` / `--grpc-port` | 进程对外端口契约 |

其余 121 项皆为 **Advanced**，详见 README 配置参考表。

---

## 2.1 商业授权 flag（Open-Core）

| Flag | 语义 | 缺失后果 |
|---|---|---|
| `--license-public-key` | 厂商 Ed25519 公钥（PEM 文本 / 文件路径 / 裸 base64 / 裸 hex） | **不配即社区版**。不是配置错误——Apache-2.0 内核本就无需授权 |
| `--license-key` | 授权凭据（`base64url(payloadJSON).base64url(signature)`） | 配了公钥但没配凭据 → 社区版，`GET /api/v1/license` 的 `reason` 会说明缺失 |

关键约束（与 §3 高危组合表风格一致，此处为**不阻止启动**的刻意设计）：

| 情形 | 行为 | 理由 |
|---|---|---|
| 公钥配错 / 凭据过期 / 签名不符 / edition 不符 | ⚠️ 降级为社区版 + `reason` 说明原因，**不拒绝启动** | 授权降级不该让已交付的内核不可用；这是商业问题不是运行问题 |
| 多副本部署 | `--license-public-key` **必须各副本一致** | 不一致会出现"同一集群里部分副本是企业版、部分是社区版"，负载均衡到不同副本时功能时有时无 |
| `--license-key` | 可用 Secret 注入，无需硬编码 | 凭据本身是公开信息（不含机密），但仍建议走 Secret 以便轮换与留痕 |

状态查询：`GET /api/v1/license`（需登录；**未授权也可查**——否则用户无从得知原因）。校验逻辑见 `internal/config/license.go`，闸门见 `internal/controlplane/license_gate.go`，决策论证见 `license-decision-2026-10-03.md`。

---

## 3. 高危组合表（启动期会被 fail-fast 或强警告）

| 组合 | 行为 |
|---|---|
| `--store=memory` + `--replicas>1` | ❌ 拒绝启动（数据分裂） |
| `--production=true` + `--tls-cert=""` | ❌ 拒绝启动（等保三级明文违规） |
| `--production=true` + `--jwt-secret=""` 或 `<32B` | ❌ 拒绝启动 |
| `--production=true` + `--encryption-key=""` | ❌ 拒绝启动（kubeconfig 明文风险） |
| `--multi-schema=true` + `--store!=mysql` | ❌ 拒绝启动 |
| `--log-backend=loki/es` + 对应 endpoint 缺 | ❌ 拒绝启动 |
| `--federation-peers` 非空 + `--federation-secret` 空 | ❌ 拒绝启动（防伪造租户身份） |
| `--federation-port>0` + 证 TLS 证书缺 | ❌ 拒绝启动 |
| `--replicas>1` + `--session-store=""` | ⚠️ stderr 警告（会话不跨副本） |
| `--session-store` 不是 `redis://` 前缀 | ❌ 拒绝启动 |
| `--metrics-allow-cidr` 任意项非合法 CIDR | ❌ 拒绝启动 |
| `--allow-public-register=true` + `--production=true` | 未强校验，**文档建议不要启用** |
| `--production=true` + `--demo=true` | ❌ 拒绝启动（demo 会关掉 bootstrap 凭据校验、CSRF Origin 校验、RBAC 兜底与 gRPC agent 身份签名四道防线） |

---

| `--plugin-manifest` 指向的文件解析或校验失败 + `--production=true` | ❌ 拒绝启动（非生产模式打 Warning 并按「未启用插件宿主」继续，不留半注册状态） |
| `--plugin-manifest` 非空 + 清单里 `tokenEnv` 指向的环境变量为空 | ❌ 拒绝启动：没有令牌的控制面调用无法被插件侧鉴权，等于开一条不可验证的准入通道 |
| 清单里 URL 指向私网/环回 + `--plugin-allow-private=false` | ❌ 拒绝启动（SSRF 基线与通知渠道同一条 `internal/egress` 策略；链路本地/云元数据段即使 true 也恒拒） |
| `--plugin-allow-private` 与 `--webhook-allow-private` | 刻意**各自独立**：合并会让「放开通知渠道」顺带放开插件出站，两个出路面各开一次 |
| 插件进程不可达 + `config.preSet` / `task.preClaim` | ⚠️ **fail-closed**：平台配置写入被拒 400、任务领取返回空。停掉一个准入插件不等于绕过准入；出厂告警 `OpsMeshPluginHookFailed` |

## 4. 多副本必须一致的密钥清单

| 字段 | 缺失后果 |
|---|---|
| `--jwt-secret` | 副本 A 签的 token 在副本 B 校验不过，用户间歇 401 |
| `--provision-secret` | B1 install token 跨副本不可用，纳管失败 |
| `--federation-secret` | 跨网段任务转发验签失败 |
| `--encryption-key` | kubeconfig 解密失败，K8s 管理能力损坏 |
| `--grpc-signature-key` | agent 请求验签失败，agent 无法工作 |

运维操作指引见 README「JWT 密钥运维指引」（该章适用于所有对称密钥，操作方式相同）。

---

## 5. Demo 模式边界

`--demo` 是"开发体验开关"而非"演示环境模板"，影响面：

- 每个 agent 注册自动预置一条 `uname -a` 示例任务；
- bootstrap 端点（`/install.sh`、`/bin/opsmesh-agent`）放宽 token 校验；
- 控制面 `/` 仪表盘跳过 require-auth。

**生产环境严禁开启**。不存在"生产 + demo 关闭后自动变安全"的隐式假设——必须由 `--production=true` 显式接管安全基线。

---

## 6. 演进约定

- 新增 flag 时**必须同步**：`Load()` 定义、`Validate()` 组合校验（如涉及）、README 配置参考表、本矩阵（如涉及组合约束）。
- 同一语义不得出现两个 flag（避免出现 `--log-backend` 与 `--log-store` 这种历史双开关——保留 `--log-store` 仅为兼容，新代码统一用 `--log-backend`）。
