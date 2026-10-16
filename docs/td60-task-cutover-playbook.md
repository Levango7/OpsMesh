# TD-60 task 域切换施工图（只读侦察产出，零代码零冲突）

> **定位**：本文是 task 域从控制面单体切向 task-svc 的**只读施工图**——通路勘察 + 缺口清单 + 与 auth 域的同构对照。
> **不写代码、不改配置**；执行需单独立项。
> **基准**：`fc68154`（2026-10-11）。所有结论以 `file:line` 标注，可逐条复核。
>
> **修订记录（2026-10-12）**：本文初版（`2b4659c`）的 §2.2 曾断言「五组路由未接线」，
> 经 peer 逐行复核**证伪**：其中四组早已在 `gateway_batch.go`/`gateway_canary.go` 实现并注册，
> 真正未接线的只有 `POST /api/v1/tasks/batch` 一组，且该组在前端是**死代码**。
> 错误结论已就地更正（保留修订痕迹），另补入 peer 发现的阻断项 R6（批量/灰度状态是进程内内存）。
> 「把错误结论留在仓库比没有结论更糟」——故本文不静默改写，而是标注修正。

---

## 1. 一句话结论

**task 域的"双轨机制面"已齐（代理规则 + compose URL + 权限分级），契约面仅缺 1 组路由，
且那组在前端是死代码。真正的阻断项不是路由而是数据面：批量/灰度状态是进程内内存，
切流与回退都会丢批次聚合视图（R6）。建议先做契约对账与死代码处置，再谈切流。**

---

## 2. 现状盘点（通路勘察）

### 2.1 机制面：已就位的三件

| 件 | 状态 | 证据 |
|---|---|---|
| 代理规则 | ✅ task 域完整规则 | `internal/controlplane/svcproxy/proxy.go:327-357`：`PublicPrefix: /api/v1/task-svc`、`UpstreamPrefix: /api/v1`、`EnvKey: TASK_SVC_URL`、兜底 `task:write` |
| 权限分级 | ✅ 三子域分级 | 同文件 `PermRules`：schedules（`schedule:read`/`schedule:write`）、approval（`approval:read`/`approval:approve`/`approval:write`）、tasks（`task:cancel`/`task:approve`/`task:read`） |
| 部署配置 | ✅ compose 已配 URL | `deploy/docker/docker-compose.prod.yml:513` `TASK_SVC_URL: http://task-svc:8102` |

> 权限分级注释明示「永远如实镜像单体本地要求，不做代理更宽松或更严格的偏离」——与 auth 域
> 「契约不因切流而改变」同原则。

### 2.2 契约面：九组路由的实测覆盖（初版误判，2026-10-12 更正）

> **初版错误**：曾据「`gateway.go` 只注册 4 组」断言五组未接线。实际是把 `gateway.go`
> 当成了全部注册点——`RegisterRoutes` 还会调 `RegisterBatchRoutes`/`RegisterCanaryRoutes`
> （`gateway.go:64-65`），实现在 `gateway_batch.go` 与 `gateway_canary.go`。教训：
> **看注册面必须追到 Register* 函数，不能只数一个文件的 HandleFunc**。

逐组实测（单体 9 组 vs task-svc）：

| 单体路由 | task-svc 落点 | 状态 |
|---|---|---|
| `/api/v1/tasks`（列表/创建） | `gateway.go:57` + `handleTasks` | ✅ 已接线 |
| `/api/v1/tasks/{id}` 及 cancel/result/approve/reject | `gateway.go:58` + `handleTaskRouting:267-280` | ✅ 已接线 |
| `/api/v1/schedules` 及 `{id}` | `gateway.go:60-61` + `handleScheduleRouting` | ✅ 已接线 |
| `/api/v1/tasks/batch-exec`（M5 批量执行） | `gateway_batch.go:249` `RegisterBatchRoutes` | ✅ 已接线 |
| `/api/v1/tasks/batch/{id}`（批量状态） | 同上 | ✅ 已接线 |
| `/api/v1/tasks/canary`（灰度创建） | `gateway_canary.go:24` `RegisterCanaryRoutes` | ✅ 已接线 |
| `/api/v1/tasks/canary/{id}` 及 advance | 同上 | ✅ 已接线 |
| **`/api/v1/tasks/batch`（POST 批量下发）** | **无** | ❌ **唯一缺口** |

**唯一缺口的性质（决定要不要补）**：
- 单体实现在 `server_tasks.go:414`，请求体用 `targets` 字段（非 `deviceIDs`），
  逐台建任务、返回 `created`/`fails`，**不产 batchID**。
- 而前端 `batch.js:17` 的 `batchDispatch` 调它，`stores/batch.js:39` 的 `dispatch()`
  读 `batchID` —— **契约本身就裂着**（前端读的字段后端不返回）。
- 且 `dispatch()` 的全仓调用方只有 `stores/batch.test.js`（mock），
  e2e-real 不打、无 view 调用 ⇒ **前端这条路是死代码**。

⇒ **结论：不该为死代码移植 handler**。正确处置是二选一：
① 前端删掉 `batchDispatch`/`dispatch()` 死路（连带其单测），后端这组路由随单体侧一并退役；
② 若产品要保留批量下发，先修契约裂缝（统一 `targets`/`deviceIDs` 与 `batchID`），
   再在 task-svc 接线。**无论哪条都不阻塞切流**——打开 `OPSMESH_SERVICE_PROXY=task`
   不会因缺这组而 404（没有真实调用方）。

### 2.3 数据面：分库（与 auth 域同构）

| 侧 | 库 | 证据 |
|---|---|---|
| 控制面单体 | `opsmesh` 主库 | `deploy/docker/scripts/init-databases.sql:16-18` |
| task-svc | `opsmesh_task` 独立库 | 同文件 `:21` 建库、`:29` 授权 |

⇒ 与 auth 域同构，可直接沿用 D+C 决策，不必重开一轮。

---

## 3. 阻断项 R6：批量/灰度状态是进程内内存（peer 发现，本文补入）

> **2026-10-15 更新：R6 两侧已收口（任务侧为本文最后一笔）**。① 控制面侧落库为
> `internal/store/model/task_record.go` 的「信封 + JSON 载荷」+ 迁移 `023_task_records.sql`
> （memory/sqlstore/multischema 三实现 + `batch_records.go` 接线 + 重启判据测试）；
> ② task-svc 侧本轮补齐：新增 `canary_releases` 表（同取舍）+ `MySQLStore` 的 `CanaryStore`
> 实现 + `main` 接线（此前无条件注入内存索引、SQL 模式重启即丢）。
> **同时修掉一个与 R6 同源的缺表缺陷（真库已证）**：`NewMySQLStore` 此前只建 `tasks` 与
> `schedules`，`batches`/`batch_tasks`/`task_results`/`task_logs` 在「服务自建表」路径下
> **从来不存在** ⇒ SQL 模式（compose 出厂 `TASK_STORE_TYPE=sql`）下批次视图恒 404、
> 执行结果与日志静默不落库（TD-68「假装持久化」同族）。已由 `migrateRecordTables` 补齐并
> 用 DSN 门控真库用例（`TestRecordTablesExistAfterNewMySQLStore` / `TestBatchRecordRoundTripMySQL`
> / `TestCanaryRecordSurvivesRestartMySQL`）钉死。
> **仍未闭合的边界（如实记账）**：本文下方列的「多副本下该功能不成立」仍成立——
> 记录虽已落库，但批次的**聚合计数**是读时按任务状态重算还是写入时快照，两侧实现不同
> （单体 `handleBatchStatus` 读时聚合；task-svc `GetBatchStatus` 读 `batches` 表的计数列），
> 多副本/双写期间可能让计数与任务实际状态不一致。切流 runbook 需写明「切换前排空在飞批次」。
> **深复核追加（2026-10-15 复查）**：① task-svc 的 DSN 有两条库不同的部署路径——compose 用专用库
> `opsmesh_task`，Helm 生产复用控制面的 `opsmesh` 库（`microservices.yaml` 把 `<前缀>_DSN` 渲染自
> 控制面 `mysql-dsn` Secret）。同库那一路，控制面迁移早已建了**异形** `task_results`（6 列，无
> `duration_ms`/`claim_epoch`），`CREATE TABLE IF NOT EXISTS` 会静默让位 ⇒ 运行时 `Unknown column`。
> 已按 `tasks` 既有做法加 `recordEnsureColumns` 补列收敛，并用真库用例
> `TestRecordTablesReconcileSharedDBShapeMySQL`（先建控制面形态旧表 + 历史行，再启动本服务）钉死。
> ② 真库用例顺带暴露 `SaveResult` 的同类零值缺陷：`finished_at` 直接传 `time.Time{}` 时 driver 发
> `'0000-00-00'`，严格模式 1292 ⇒ 整行写不进、执行结果静默不落库（未跑完的任务 `FinishedAt` 即零值）。
> 已改用本文件既有的 `nullTime` 转换。
> 下方原文保留（含当时的三条后果），供对照。

**这不是路由问题，是切流方案的决定性约束**：

- 单体 `server_batch.go` 文件头自述「批量状态仅内存索引（重启后丢失），任务实例本身持久化在 store 中」；
  索引是 per-Server `*batchStore`（两个 map + RWMutex，`server.go:194`）。
- task-svc 侧同构：`service.go:40` 的 `canaryStore` 是「灰度发布内存索引（可选注入，
  nil 时 canary API 返回 503）」。

三个后果：

1. **task-svc 若要承接这五组，必须连 handler 带内存索引一起移植**——所以"纯契约补齐"
   的定性不成立，应是「契约补齐 + 内存态移植」。
2. **切流与回退都会丢批次聚合视图**（任务实例在库里，丢的是批次视图）—— runbook 必须
   写明「批次视图不跨切换存活」，切换前排空在飞批次。
3. **多副本下该功能不成立**（helm 默认 `replicas: 1`，当前不炸；模板已给 task-svc 留多副本
   分支），除非落库或 sticky 路由。

**建议**："是否把批量/灰度状态落库"单独立项（schema 工作），不混进切流。

---

## 4. 与 auth 域施工顺序的同构对照

| auth 域（已完成） | task 域（本文对应状态） |
|---|---|
| ① 契约比对报告列差异 | **部分完成**：机制面对齐已核；缺逐端点响应体对账（尤其 batch-exec 的 body 字段名） |
| ② 补阻断级缺口 | **无阻断级缺口**（唯一缺的一组是死代码，见 §2.2） |
| ③ 语义对齐（会话/建号/登录防护） | **待核**：批量与灰度的审批、租户语义两侧是否一致 |
| ④ 数据面方案立项（D+C） | **可直接沿用 auth 的 D+C 决策** |
| ⑤ 切流信号化设计 | **可直接沿用**（task 域后端任务由 `X-User-Id`/`X-Tenant-ID` 注入头定位身份） |
| ⑥ 搬号 job + 真进程冒烟 + runbook | **待做**；task 的 job 形态是"切新任务"而非"搬用户" |

---

## 5. 风险清单

| # | 风险 | 性质 | 依据 |
|---|---|---|---|
| R1 | ~~五组路由未接线 ⇒ 前端 404~~ | **已证伪** | §2.2：实为 1 组且是死代码 |
| R2 | 任务数据分库：新建任务落哪侧需明确 | **阻断** | §2.3；需 `TASK_SVC_OWNS_NEW_TASKS` 等价开关 |
| R3 | 在飞任务/批次的状态连续性 | 高 | R6 的具体形态：切换丢批次视图 |
| R4 | 批量与灰度的权限点两侧一致 | 高 | `svcproxy/proxy.go:340-355` 已声明镜像单体，待对账 |
| R5 | schedules 的 pause/resume 语义 | 中 | `gateway.go:489-495` 已实现，待对账 |
| R6 | 批量/灰度状态进程内内存 | **已收口（2026-10-15）** | §3 更新：两侧均落库；同源缺表缺陷（周边四张表从未创建）已修 |

---

## 6. 建议施工顺序

1. **处置死代码**（前端侧）：删 `batchDispatch`/`dispatch()` 及其单测，或修契约裂缝后启用。
   这是 §2.2 唯一缺口的正解，工作量最小。— **✅ 已完成（TD-94）**
2. **补契约对账报告**：照 auth 体例逐端点比对响应体，重点 batch-exec 的
   `targets` vs `deviceIDs` 字段名差异。— **✅ 已完成（docs/td60-task-contract-audit.md）**
3. **定数据面开关**：`TASK_SVC_OWNS_NEW_TASKS`（语义同 auth 的 `OWNS_NEW_ACCOUNTS`）。
   — **✅ 已落地（`4e88294`，cutover/router.go + 四个建任务入口 409）**
4. **R6 单独立项**：批量/灰度状态落库（schema 工作），不混进切流。
   — **✅ 两侧已收口（2026-10-15，见 §3 更新）**
5. **切流执行**：沿用 auth 的 D+C + 信号化设计；runbook 补「切换前排空在飞批次」。
   — **待做**（下一步：切流 runbook 的 task 域版本 + 真进程冒烟；注意 §3 更新里
   「批次聚合计数两侧口径不同」这条边界）

---

## 7. 本文不做的

- 不写任何代码、不改任何配置（本文是只读施工图）。
- 不动 `internal/controlplane`、`internal/store`（并行线领地）。
- R6 的落库方案需独立立项，不在本文展开。