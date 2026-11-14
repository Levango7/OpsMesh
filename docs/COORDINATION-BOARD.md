# 多 agent 协作板（Coordination Board）v2

> 目的：多条 agent 线在同一仓库施工时，把"谁在动哪片 / 谁听谁指挥 / 谁在等谁"变成
> **可机械检查**的状态，而不是埋在散文日志里的叙事。
>
> 生效范围：`main` 分支。真源 = 远端仓库 `Levango7/OpsMesh`；
> 各线本地工作区只是施工面（kilo 线 `F:\Agent\kilo\workspace\OpsMesh`，
> ZCode 线 `F:\Nexus\OpsMesh`）。
>
> v2 变更（2026-10-26，ZCode 线执行）：双线协议扩为多线 + 层级分工；
> 队头约定从"互投对方 INBOX"改为"`CLAIMS.md` 单一追加日志 + INBOX 只收定向消息"。

---

## 线名册

| 线 | 角色 | 常态 |
|---|---|---|
| `kilo` | **主力线（lead）** | 只有 lead 可发 DISPATCH；merge 前门禁由它跑 |
| `workbuddy` | 机动线 | 接 DISPATCH 施工；未经派遣只做隔离面（文档/测试/脚本） |
| `qoder` | 机动线 | 同上 |
| `ZCode` | 机动线（本线） | 同上；归版/板面维护类事务默认归本线 |
| `trae` | —（遗留，见 §9 清理项） | 无 |

角色判定写死：**同一时间只有一个 lead**。lead 变更（lead 主动让位或 ttl 过期接管）
必须在 `CLAIMS.md` 留一行 `[ROLE] <ts> lead=<新线>`，不许口头默认。

---

## 文件布局

- `docs/CLAIMS.md` —— **唯一**的领地日志，append-only。CLAIM/RELEASE/DONE/
  CONFLICT/TAKEOVER/DISPATCH/STATUS/BLOCKED/ROLE 全部落这里。
- `docs/INBOX-<线名>.md` —— 只收**定向**消息（NOTE、问答、通报），线名=名册里的名字。
  例：给 kilo 的就发 `INBOX-KILO.md`。
- `scripts/coord-gate.py` —— 解析上述两类文件，回写下方「当前状态」块并出退出码。
  板面与脚本同时改时，先改脚本跑一遍，再提交两边。

历史文件 `docs/INBOX-ZCODE.md` / `docs/INBOX-LEVANGO.md` 保留不动（只读档案，
门禁仍解析——旧 CLAIM 都会重放，但既有 DONE/RELEASE 已闭环，不会误判）。

---

## 协议

### 1. 留言格式（所有类型共用）

**必须单行**，键值成对、顺序不限（`Add-Content` 会拆续行，解析器只认同行——踩过的坑）：

```
[CLAIM] 2026-10-26T02:00:00Z from=ZCode ttl=4h scope=internal/controlplane/svcproxy/
[DISPATCH] 2026-10-26T02:00:00Z from=kilo to=workbuddy task=T-012 scope=web/enterprise/ ddl=2026-10-28T00:00:00Z
[STATUS] 2026-10-26T06:00:00Z from=workbuddy task=T-012 note=building-ok
[BLOCKED] 2026-10-26T06:30:00Z from=workbuddy task=T-012 reason=runner-queued
[DONE] 2026-10-26T09:00:00Z from=workbuddy task=T-012 scope=web/enterprise/ commit=abc1234
```

### 2. 认领（CLAIM）与完成（DONE）

- `scope` 必须是**目录或单个文件**，不许 `internal/` 这种粒度；多 scope 就多行。
- `ttl` 到期无 DONE/RELEASE/LANDED，任何一线可单方面 TAKEOVER（见 §6）。
- 同一 scope 只允许一个 CLAIM 存活；重复认领 = 撞车（§5）。
- DISPATCH 含 scope 时**等价于一次性 CLAIM**（有效期到 ddl），无需再补 CLAIM。
- 闭环销号认 `DONE`/`RELEASE`/`TAKEOVER`/`LANDED`，均须带 `scope=`；与 CLAIM 的
  scope 逐字一致或集合同一（顺序/空格差异不算撞车；候选唯一才销，部分重叠不销）。
- `[LANDED]` 是「PR 合入 main」回执；缺 `scope=` 的存量 LANDED 行由任一线 append
  带 scope 的补配对行（from=代登线，note 注明代登，原行不动）。

### 3. 层级：派遣（DISPATCH）与上报（BLOCKED）

- 只有 **lead** 可发 `[DISPATCH]`；`task=` 引用下方任务队列的行号。
- 执行线接活后开始施工；**中途自助重定向必须先回 `[BLOCKED]`**（不得绕道施工
  别的 scope——那正是本板要防的形态）。
- `BLOCKED` 由 lead 裁定：继续等 / 改派（新 DISPATCH）/ 挂起（`[NOTE]` 说明）。
- 机动线发现活没人派，不许私自领：在 `INBOX-<lead>.md` 留 `[NOTE]` 问。

### 4. 撞车（CONFLICT）

同 scope 两个存活 CLAIM，或某线在你已认领 scope 里有未提交改动：

```
[CONFLICT] 2026-10-26T02:05:00Z from=kilo scope=internal/controlplane/svcproxy/ other=ZCode
```

**裁定写死**：先 CLAIM（含 DISPATCH 隐含认领）者保留；后到者立即停手并回
`[CONFLICT]`。都称先时，`git log` 里留言提交时间早的赢。
**撞车不许互相 checkout / stash 对方文件**——那静默吞掉在途工作。

### 5. 禁区（FROZEN）

各线（含 lead）都不碰，除非用户明确开口：
- `.github/workflows/`（发布链，动前必须双线确认）
- `CHANGELOG.md` 顶部 `> 当前最新已发布版本` 引用块
- `docs/tech-debt.md` 的标记行（只能由跑门禁的一方改）

### 6. 接管（TAKEOVER）

CLAIM ttl 过期且无 DONE/RELEASE/LANDED，或 DISPATCH 过了 ddl 无进展：

```
[TAKEOVER] 2026-10-26T06:00:00Z from=kilo scope=... reason=ttl-expired
```

接管前必须留痕本板；接管方承担该 scope 全部未提交改动。

### 7. 纪律（各线通用，写进各 harness 的指令文件）

1. **开工先 `git pull`**（含本板），有活先看「当前状态」块。
2. 动文件**之前**留 CLAIM / 接到 DISPATCH 才算开闸。
3. **收工 `[DONE] commit=<sha>`，并当天 `push`**——未提交状态不得过夜，
   共享工作区里"已提交"才是真相（10-14 两次清场事故的根因）。
4. 不读板不动禁区；不在他人未提交的 in-flight 面上施工。

---

## 当前状态

（由 `python scripts/coord-gate.py` 解析 CLAIMS.md + INBOX-*.md 回写）

```
LEAD: kilo
ACTIVE CLAIMS:
  docker-compose.yaml,Dockerfile.task-svc,internal/controlplane/server_netsec.go <- opencode  (ttl=24h by opencode)
  internal/agent/agent.go,internal/agent/task_service_client.go <- opencode  (ttl=24h by opencode)
  internal/config/config.go,internal/agent/agent.go,docker-compose.yaml <- opencode  (ttl=24h by opencode)
  internal/controlplane/harbor_ops.go,internal/controlplane/b25_injection_test.go,CHANGELOG.md <- opencode  (ttl=24h by opencode)
  internal/controlplane/lb_ops.go,internal/controlplane/b24_injection_test.go,CHANGELOG.md <- opencode  (ttl=24h by opencode)
  internal/controlplane/olap_ops.go,internal/controlplane/b23_injection_test.go,CHANGELOG.md <- opencode  (ttl=24h by opencode)
  internal/k8slifecycle/handler.go,internal/controlplane/ha.go,internal/controlplane/canary_enhance.go,internal/controlplane/server_lifecycle.go,internal/controlplane/task_service_client.go,services/portal-svc/internal/service/service.go,services/auth-svc/internal/cache/session.go,services/task-svc/pkg/config/config.go,services/task-svc/cmd/task-svc/main.go,web/enterprise/src/views,web/enterprise/src/styles <- kilo  (ttl=24h by kilo)
  internal/platform/cloudprovider/tencent/provider.go <- kilo  (ttl=12h by kilo)
  services/task-svc/internal/leader/mysql.go,services/task-svc/internal/leader/mysql_test.go <- opencode  (ttl=24h by opencode)

TASKS:
  (无)

ISSUES:
  CLAIM 过期未闭环：go.mod,.github/workflows/ci.yml,.github/workflows/release.yml（认领人 opencode，ttl 6h）
  CLAIM 过期未闭环：go.mod,go.sum,Dockerfile,Dockerfile.agent,docs/CLAIMS.md（认领人 opencode，ttl 12h）
  CLAIM 过期未闭环：internal/controlplane/dns_ops.go,internal/controlplane/b19_injection_test.go,CHANGELOG.md（认领人 opencode，ttl 24h）
  CLAIM 过期未闭环：internal/controlplane/fileshare_ops.go,internal/controlplane/b22_injection_test.go,CHANGELOG.md（认领人 opencode，ttl 24h）
  CLAIM 过期未闭环：internal/controlplane/ha.go,internal/controlplane/leader_elector_client.go,internal/controlplane/server.go,internal/config/config.go（认领人 opencode，ttl 12h）
  CLAIM 过期未闭环：internal/controlplane/ha.go,internal/controlplane/leader_elector_client.go,internal/controlplane/server.go,internal/config/config.go,services/task-svc/internal/server/server.go（认领人 opencode，ttl 12h）
  CLAIM 过期未闭环：internal/controlplane/mongo_ops.go,internal/controlplane/b21_injection_test.go,CHANGELOG.md（认领人 opencode，ttl 24h）
  CLAIM 过期未闭环：internal/controlplane/server.go,internal/controlplane/k8s_lifecycle_auth_wiring_test.go（认领人 opencode，ttl 12h）
  CLAIM 过期未闭环：internal/controlplane/server_lifecycle.go,internal/controlplane/task_service_client.go,internal/controlplane/routing_fixes_test.go,docs/CLAIMS.md（认领人 opencode，ttl 12h）
  CLAIM 过期未闭环：internal/controlplane/storage_ops.go,internal/controlplane/b20_injection_test.go,CHANGELOG.md（认领人 opencode，ttl 24h）
  CLAIM 过期未闭环：internal/controlplane/vector_ops.go,internal/controlplane/kafka_advanced_ops.go,internal/controlplane/b17_injection_test.go,CHANGELOG.md（认领人 opencode，ttl 24h）
  CLAIM 过期未闭环：internal/controlplane/virt_ops.go,internal/controlplane/b18_injection_test.go,CHANGELOG.md（认领人 opencode，ttl 24h）
  CLAIM 过期未闭环：internal/gates/tenant_store_verdict_gate_test.go（认领人 zcode，ttl 72h）
  CLAIM 过期未闭环：proto/buf.yaml,proto/opsmesh/task/v1/task.proto,internal/proto/buf_module_scope_test.go,internal/controlplane/ha.go,internal/config/config.go,internal/controlplane/leader_elector_client.go,internal/controlplane/routing_fixes_test.go,internal/controlplane/server.go,internal/controlplane/server_lifecycle.go,internal/controlplane/task_service_client.go,services/task-svc/internal/server/server.go（认领人 opencode，ttl 12h）
  CLAIM 过期未闭环：proto/buf.yaml,proto/opsmesh/task/v1/task.proto,internal/proto/buf_module_scope_test.go,internal/controlplane/ha.go,internal/config/config.go,internal/controlplane/leader_elector_client.go,internal/controlplane/routing_fixes_test.go,internal/controlplane/server.go,internal/controlplane/server_lifecycle.go,internal/controlplane/task_service_client.go,services/task-svc/internal/server/server.go,internal/controlplane/grpc_dial_blocking.go（认领人 opencode，ttl 12h）
  CLAIM 过期未闭环：proto/opsmesh/task/v1/task.proto（认领人 opencode，ttl 12h）

CONFLICT: 同 scope 重复认领：docker-compose.yaml,Dockerfile.task-svc,internal/controlplane/server_netsec.go, internal/platform/cloudprovider/tencent/provider.go, proto/buf.yaml,proto/opsmesh/task/v1/task.proto,internal/proto/buf_module_scope_test.go,internal/controlplane/ha.go,internal/config/config.go,internal/controlplane/leader_elector_client.go,internal/controlplane/routing_fixes_test.go,internal/controlplane/server.go,internal/controlplane/server_lifecycle.go,internal/controlplane/task_service_client.go,services/task-svc/internal/server/server.go, proto/buf.yaml,proto/opsmesh/task/v1/task.proto,internal/proto/buf_module_scope_test.go,internal/controlplane/ha.go,internal/config/config.go,internal/controlplane/leader_elector_client.go,internal/controlplane/routing_fixes_test.go,internal/controlplane/server.go,internal/controlplane/server_lifecycle.go,internal/controlplane/task_service_client.go,services/task-svc/internal/server/server.go,internal/controlplane/grpc_dial_blocking.go
```

---

## 为什么不用协议互相对话

不同 harness 的会话互相隔离，进程级消息（MCP board 等）到不了对方 session——
**仓库是唯一双方都能读写的共享介质，所以板子必须是文件。**
A2A/ACP 这类协议服务的是"独立 agent 服务"，coding CLI 不在它的生态里；
本板就是为同一仓库多 CLI agent 的场景量身定做的最小公共介质。
