# OpsMesh 安全审查报告（2026-11-16）

> 审查范围：`internal/controlplane`、`internal/platform/cloudprovider`、`pkg/auth`、`pkg/netsec`、`services/device-svc`、`web/enterprise`
> 审查方法：三路源码审计（后端 stub / 路由-处理器缺口 / 业务逻辑正确性）+ 本轮整改回归 + 针对性验收测试
> 结论：**未发现新的未修复 P0 漏洞**；P0/P1 存量问题已在本轮或前序批次闭环；剩余风险均为"需要配置/网络层配合"或"已知待跟进项"，登记在第 5 章。

---

## 1. 审查基准与证据来源

| 来源 | 覆盖内容 | 结果 |
|---|---|---|
| 审计 A（后端 stub） | 云厂商、同步、地域、配额、流水线 | 已修复或已确认非缺陷（见 §2） |
| 审计 B（路由 vs 处理器） | 209 条路由、dispatcher、死代码、方法语义 | 已修复（见 §2.4） |
| 审计 C（业务逻辑） | 跨租户、审批、配额、依赖、实例清单、凭据 | P0 全部闭环（见 §2.1-2.3） |
| 本轮回填验签/权限 | 联邦 nonce、任务创建类接口权限、实例键注入 | 已闭环（见 §2.5-2.7） |

验证方式：`go build ./...`、`go vet ./...`、`go test ./...`、`npm run build`、`npx vitest run` 全绿；安全用例集中在下列测试文件：
`p0_security_holes_test.go`、`query_exec_authz_test.go`、`http_method_semantics_test.go`、`federation_test.go`、`pkg/auth/federation_test.go`、`cloud_account_test.go`、`middleware_instances_test.go`。

---

## 2. 已闭环问题（含证据）

### 2.1 跨租户模板越权（P0，已修）
- **问题**：`middleware-templates/{id}` 与 `os-templates/{id}` 的 GET/PUT/DELETE 只按 ID 取行，不校验租户归属；任意 `*:write` 用户可覆写/删除他租户或共享预置模板，进而让所有租户部署被篡改的脚本。
- **修复**：新增 `getMiddlewareTemplateScoped` / `getOSTemplateScoped`，写入/删除严格按归属，跨租户一律 404（不泄露存在性）；创建路径补"自带 ID 撞他租户行"归属闸。执行链路保留共享预置模板的只读解析。
- **证据**：`internal/controlplane/p0_security_holes_test.go`（14 用例：跨租户 GET/PUT/DELETE→404、他租户行未被改写、共享预置不可改写）。

### 2.2 云账号 RBAC 缺失 + 凭据明文落库（P0，已修）
- **问题**：9 个云账号 handler 仅 `requireTenantContext`，viewer 可创建/删除/同步云账号；`credentialsRef` 未校验，明文 AK/SK 可直接入库。
- **修复**：读接口 `cloud:read`、写接口 `cloud:write`；创建/更新两条路径强制 `model.RequireSecretReference` 形态校验，明文返回 400。
- **证据**：`p0_security_holes_test.go`（viewer 403 且未落库；明文→400；引用形态→201/200）。

### 2.3 审批与配额（P0，已修）
- **审批**：`tasks.approval_required` 已落库（迁移 028/030），SQL 后端 `CreateTask` 在命中时置 `pending_approval`，高风险任务不再被直接领取。
- **配额**：任务创建（`server_tasks.go`）与中间件部署/卸载均调用 `quotaMgr.CheckTask`，超限返回 429。
- **依赖失败**：SQL/内存两后端一致，上游失败会释放下游依赖并标记 failed/dead_letter，不再永久 blocked。

### 2.4 路由/方法语义（已修）
- `tasks/devices/alerts/users/cloud-accounts` 子资源 dispatcher：已知路径 + 错误方法返回 **405 + Allow**，未知路径 404；不再用 404 掩盖 405。
- 修复 `lb_ops.go` 尾斜杠注册导致 POST body 被 301 丢弃；`/alert-rules/{id}` 补齐 GET/PUT；`/tasks/batch/{id}`、`/tasks/canary/{id}[/advance]` 路由恢复可达。

### 2.5 任务创建类接口权限提升（P0 安全面，已修）
- **问题**：Zabbix/Nightingale/SkyWalking/Vector/OLAP/Mongo 六个"查询"接口会创建 shell 任务，却只要求 `device:read`——viewer 可借读权限在设备上执行命令。
- **修复**：统一提升为 `task:write`（与 `internal/store/model/perm.go` 的"下发任务"权限一致），租户上下文仍前置。
- **证据**：`query_exec_authz_test.go`（viewer 403 且任务数不变；operator 200 且任务数 +1）。

### 2.6 HMAC 联邦加固（P1→已修）
- 签名/验签统一到 `pkg/auth/federation.go`，消除三处重复 canonical 实现（单点定义，降低口径漂移风险）。
- 新增 `X-Federation-Nonce` + TTL nonce 缓存：同一请求重放返回 403；超出 max skew（默认 300s）返回 403。
- 修复联邦租户上下文缺口：`requireAuth=true` 下仅带签名 `X-Tenant-ID` 的 peer 请求不再 401，租户边界仍由 `X-Tenant-ID` 约束。
- **证据**：`pkg/auth/federation_test.go`（canonical 精确字节、篡改、skew、重放、legacy 兼容）、`federation_test.go`（nonce 唯一、device-op 重放拒绝、联邦禁用 404、入站租户上下文）。

### 2.7 中间件实例键注入与一致性（P2，已修）
- 实例清单不再用空白切分 `k=v`（值含空格会截断键并注入伪键）；读写统一为带引号格式，未知键忽略，兼容历史记录。
- URL `instanceID` 含空白/`=`/`;` 返回 400；部署与卸载共用同一 `instanceKey(...resolvedVersion)` 推导，避免"部署成功但卸载查不到"。

### 2.8 Shell 注入形态收口（前序批次，已落 main）
- 真洞：DNS TXT/MX 记录值、Mongo `--eval` filter（heredoc/整参数 Quote 包裹，能力零降级）。
- 形态面：storage / fileshare / virt / olap / lb / harbor / kernel / tsdb / graph / kafka 等 builder 的"已校验但直引"改为局部变量承接，脚本文本除必要处逐字节不变；每批附 AST/能力/变异守卫测试。

### 2.9 云同步真实性（已修）
- `handleSyncCloudAccount` 不再把调用方传入的 `DiscoveredCount` 当真实结果落库；凭据/厂商解析失败时状态为 `failed`、计数为 0；成功路径计数来自 provider 返回。
- `handleTestCloudAccount` 调用 `provider.ValidateCredentials`，`handleListCloudAccountRegions` 调用 `provider.ListRegions`（6 家厂商均为真实 HTTP 实现）。

---

## 3. 控制项现状（非缺陷，确认有效）

| 控制项 | 现状 |
|---|---|
| 认证 | HS256 内核签发（强制非空密钥、固定算法、jti、15min access token）；RS256 网关验签（算法白名单 + issuer 校验） |
| 授权 | `requireTenantContext` → `requireProd` 双层；默认拒绝兜底网覆盖 `/api/v1/*`（白名单显式列出） |
| 限流 | ADR-1：IP 200 req/s（fail-open）+ 租户 1000 req/s（fail-closed），支持 `--trusted-proxies` XFF 解析 |
| 密钥加密 | K8s kubeconfig AES-256-GCM（`enc:v1:` 前缀，解密失败硬失败）；凭据强制引用形态 |
| 服务代理 | 剥离客户端身份头后按验证身份重注入；同源回环目标 503；Cookie 仅 auth 域 |
| 审计 | 变更类操作统一写 `proto.AuditEvent`；`authz_denied` 可追溯 |
| 发布物 | CI 含 trivy/许可/发布物回核门禁 |

---

## 4. 验收测试覆盖（本轮新增）

| 维度 | 用例 |
|---|---|
| 异常输入 | 联邦头缺失/畸形/篡改、未知 op、非法 instanceID、分页参数越界、YAML 未闭合块 |
| 并发 | NonceCache 并发判重（同一 nonce 仅一次通过）、并行相同签名请求仅一次成功、分页并发租户隔离 |
| 重复验证 | device-op 重放 403、重复 deploy/uninstall 键一致、重复审批 409、重复删除 404、重复 suspend 幂等 |

> 注：本机无 cgo/gcc，`-race` 无法本地执行；CI 的 race 腿为最终判据。

---

## 5. 剩余风险登记（按优先级）

| ID | 风险 | 状态 | 影响 | 处置/证据 | 优先级 |
|---|---|---|---|---|---|
| R1 | 联邦使用**全局共享密钥**，无 per-peer 密钥/轮换 | **CLOSED** | — | `NewFederationManagerFromConfig` 装配 per-peer 密钥（`--federation-peer-secrets`/`OPSMESH_FEDERATION_PEER_SECRETS`，`peerID@peerURL=secret1[,secret2]`）：出站用目标 peer 的 `Secrets[0]` 签名并写 `X-Federation-Peer-ID`，接收侧接受该 peer 全部 secret 以支持轮换，全局密钥仅兜底；`federation_peer_test.go` 的 per-peer 隔离/轮换/服务装配用例全绿（含 `TestFederationServerWiring_UsesPerPeerSecrets`） | P1 |
| R2 | 验签通过的 peer 请求**跳过 per-permission RBAC**（信任源控制面） | OPEN | 源端被攻破即可横向 | 强制联邦端口 mTLS + 对端审计留痕 + 网络 ACL 仅放行已知 peer | P1 |
| R3 | `tsdb_ops.go`、`graph_ops.go` 的查询接口仍为 `device:read` 建任务 | **CLOSED** | — | 两处查询执行入口已提升为 `task:write`（`tsdb_ops.go:489`、`graph_ops.go:388`）；权限回归见 `query_exec_authz_test.go` 的 `TestQueryExecHandlersDenyViewer` / `TestQueryExecHandlersAllowOperator` | P1 |
| R4 | 云厂商地域列表仍保留静态回退 | OPEN | 展示可能过期，不影响鉴权 | 厂商 API 不可用时显式标注"回退数据"并告警 | P2 |
| R5 | `log.Printf` 未全面替换为结构化日志 | **PARTIALLY CLOSED** | 剩余包内审计检索与关联能力仍弱 | auth/user/role 路径已迁移到 `internal/logx`（`auth.go`、`auth_users.go`、`auth_roles.go`）；其余包按批跟进，优先任务/联邦路径 | P2 |
| R6 | 前端安全响应头（CSP/HSTS/X-Frame-Options）未在本轮验证 | OPEN | XSS 纵深不足 | 在反向代理/控制面统一注入并加回归断言 | P2 |
| R7 | 镜像/依赖 CVE 扫描依赖 CI 定时跑 | OPEN | 新 CVE 可能滞后发现 | 保持 trivy 门禁 + 每周定时扫描与告警 | P3 |

---

## 6. 发布前安全检查清单

- [ ] `go test ./...`、`npx vitest run` 全绿；race 腿通过
- [ ] 新增/变更路由均有租户上下文 + 对应权限闸（用 `scripts/authz-route-audit.py` 复核）
- [ ] 所有 shell 构建路径无用户输入直引（用 `scripts/shell-injection-audit.py` 复核）
- [ ] 新增配置项默认值安全（默认拒绝、默认关闭调试、密钥非空校验）
- [ ] 联邦变更：nonce/skew 配置与对端一致；peer 白名单最小化
- [ ] 迁移含审批/配额/租户列时，验证"升级库 ≡ 新装库"（`upgrade-rehearsal.sh`）
- [ ] 发布物回核与许可清单通过（`verify-release-artifacts.sh`）
