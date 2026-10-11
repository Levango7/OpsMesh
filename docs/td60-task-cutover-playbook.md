# TD-60 task 域切换施工图（只读侦察产出，零代码零冲突）

> **定位**：本文是 task 域从控制面单体切向 task-svc 的**只读施工图**——通路勘察 + 缺口清单 + 与 auth 域的同构对照。
> **不写代码、不改配置**；执行需单独立项。
> **基准**：`90eecf0`（2026-10-11）。所有结论以 `file:line` 标注，可逐条复核。
>
> **为什么现在做**：auth 域切流的机制面（名册/转发/回退/搬号 job/真进程冒烟）已由 peer 全部落地并验证，
> task 域是 TD-60 阶段 2 核心三域（task/device/auth）中**尚未做契约对账**的下一站。等真实用户名单期间，
> 这份图能让"开切"从估算变成照单施工。

---

## 1. 一句话结论

**task 域的"双轨机制面"已经齐了（代理规则 + compose URL + 权限分级），但"契约面"有五组路由未接线、
数据面分库未对齐。现阶段打开 `OPSMESH_SERVICE_PROXY=task` 会让前端批量/灰度功能直接 404，
不能切。缺口清单见 §3，施工顺序见 §5。**

---

## 2. 现状盘点（通路勘察）

### 2.1 机制面：已就位的三件

| 件 | 状态 | 证据 |
|---|---|---|
| 代理规则 | ✅ 已有 task 域完整规则 | `internal/controlplane/svcproxy/proxy.go:327-357`：`PublicPrefix: /api/v1/task-svc`、`UpstreamPrefix: /api/v1`、`EnvKey: TASK_SVC_URL`、兜底 `task:write` |
| 权限分级 | ✅ 三子域分级比单体更细 | 同文件 `PermRules`：schedules（`schedule:read`/`schedule:write`）、approval（`approval:read`/`approval:approve`/`approval:write`，approve/reject 走专属权限且在通用 write 之前命中）、tasks（`task:cancel`/`task:approve`/`task:read`） |
| 部署配置 | ✅ compose 已配 URL | `deploy/docker/docker-compose.prod.yml:513` `TASK_SVC_URL: http://task-svc:8102` |

> 权限分级注释明示「永远如实镜像单体本地要求，不做代理更宽松或更严格的偏离」——这与 auth 域
> 「契约不因切流而改变」是同一条原则。

### 2.2 契约面：缺口五组路由（本文最重要的发现）

单体 `server_lifecycle.go` 注册的 task 家族共 **9 组**路由，task-svc 网关（`services/task-svc/internal/http/gateway.go`）只暴露 **2 组**入口：

| 单体路由 | task-svc 是否接线 | 影响 |
|---|---|---|
| `/api/v1/tasks`（列表/创建） | ✅ `gateway.go:57` | 无 |
| `/api/v1/tasks/{id}` 及 cancel/result/approve/reject | ✅ `gateway.go:58` + `handleTaskRouting:260-276` | 无 |
| `/api/v1/schedules` 及 `{id}`、pause/resume | ✅ `gateway.go:60-61` + `handleScheduleRouting:440-495` | 无 |
| **`/api/v1/tasks/batch`**（POST 批量下发） | ❌ **无** | 前端批量下发 **404** |
| **`/api/v1/tasks/batch-exec`**（POST 批量执行，M5） | ❌ **无** | M5 批量执行 **404** |
| **`/api/v1/tasks/batch/{id}`**（GET 批量状态） | ❌ **无** | 批量状态查询 **404** |
| **`/api/v1/tasks/canary`**（POST 灰度发布） | ❌ **无** | 灰度发布 **404** |
| **`/api/v1/tasks/canary/{id}`** 及 advance | ❌ **无** | 灰度状态/推进 **404** |

**判据**：这五组在 `gateway.go` 无任何 `HandleFunc` 注册，`handleTaskRouting` 的 `switch` 也只认
`{id}`/`cancel`/`result`/`approve`/`reject` 五个分支（`:268-276`）。即**即使代理规则命中，
转发到 task-svc 后仍由其 404 收尾**——这与 auth 域「权限点不存在导致恒 403」是同一类
「机制通、契约断」缺陷，只是错位在服务侧而非代理侧。

### 2.3 数据面：分库（与 auth 域同构）

| 侧 | 库 | 证据 |
|---|---|---|
| 控制面单体 | `opsmesh` 主库 | `deploy/docker/scripts/init-databases.sql:16-18`（主库由 MYSQL_DATABASE/MYSQL_USER 供给，承载 users/roles/devices/agents/ci_items） |
| task-svc | `opsmesh_task` 独立库 | 同文件 `:21` 建库、`:29` 授权 |

⇒ 与 auth 域**完全同构**：切流前必须先定「数据面归属」。auth 域的结论（`docs/td60-cutover-decisions.md`）
是「近期 D+C：整域开关 + 按用户群分批，A 不立即立项」——task 域可直接沿用该决策，不必重开一轮。

---

## 3. 与 auth 域施工顺序的同构对照

auth 域从「发现缺口」到「机制就绪」的六步，task 域可逐句照搬：

| auth 域（已完成） | task 域（本文对应状态） |
|---|---|
| ① 契约比对报告列出响应体/策略/权限差异 | **缺口已找到**（§2.2 五组路由），但尚缺逐端点响应体对账 |
| ② 补 `/auth/me` 阻断级缺口（permissions） | **待做**：补 batch/batch-exec/canary 五组路由（属 services/ 侧，本线可做） |
| ③ 语义对齐（改密会话/建号透传/登录防护等价） | **待核**：批量/灰度的审批与租户语义两侧是否一致 |
| ④ 数据面方案立项（D+C） | **可直接沿用 auth 的 D+C 决策**，无需重开 |
| ⑤ 切流信号化设计（用户名作信号） | **可直接沿用**：task 域后端任务由 `X-User-Id`/`X-Tenant-ID` 注入头定位身份，与 auth 同 |
| ⑥ 搬号 job + 真进程冒烟 + runbook | **待做**：task 的 job 形态不同（不是"搬用户"而是"确认任务数据在哪侧"），见 §5 |

---

## 4. 风险清单（切流前必须清）

| # | 风险 | 性质 | 依据 |
|---|---|---|---|
| R1 | 五组路由未接线 ⇒ 前端功能 404 | **阻断** | §2.2 |
| R2 | 任务数据分库：切流后「新建任务落哪侧」需明确 | **阻断** | §2.3；auth 域用 `AUTH_SVC_OWNS_NEW_ACCOUNTS` 表达，task 需等价开关 |
| R3 | 进行中任务的状态连续性：切换瞬间在飞任务/审批不应半途丢失 | 高 | 任务是有状态长流程，比 auth 的"重登一次"重得多 |
| R4 | 批量与灰度在单体侧有独立权限点（`task:cancel`/`task:approve`/approval 三分支），服务侧补路由时须同权限 | 高 | `proxy.go:340-355` 的 PermRules 已声明镜像单体 |
| R5 | schedules 的 pause/resume 语义两侧是否一致 | 中 | `gateway.go:489-495` 已实现，待对账 |

---

## 5. 建议施工顺序

1. **补五组路由**（`services/task-svc/internal/http/gateway.go`）：batch/batch-exec/batch/{id}/
   canary/canary/{id}。这是纯契约补齐，属本线可做范围；做完即可让
   `OPSMESH_SERVICE_PROXY=task` 打开而不 404。**注意**：单体这五组走 `handleBatchCreateTasks`/
   `handleBatchExec`/`handleBatchRouting`/`handleCanaryCreate`/`handleCanaryRouting`，
   语义须逐组对齐（尤其 canary 的 advance 是 POST）。
2. **补契约对账报告**：照 `docs/td60-auth-consistency-report.md` 体例，逐端点比对响应体。
3. **定数据面开关**：新增 `TASK_SVC_OWNS_NEW_TASKS`（语义同 auth 的 `OWNS_NEW_ACCOUNTS`：
   开 ⇒ 本地拒绝新建并指引去服务侧；关 ⇒ 本地照建）。
4. **定在飞任务处置**：R3 是 task 域独有风险，建议「切换窗口内不允许进行中的批量/灰度任务跨切换」，
   即切流前排空在飞任务——这需要 runbook 明确写。
5. **搬号 job 的 task 变体**：auth 的 job 是「搬用户账号」；task 是「搬/对齐任务数据」，
   因两侧分库，最轻形态是**声明只切新任务**（`OWNS_NEW_TASKS` + 存量任务留在原侧只读），
   而不是搬历史任务——这与 auth「按用户群分批」是同一种"增量切换"哲学。

---

## 6. 本文不做的（留待立项）

- 不写任何代码、不改任何配置（本文是只读施工图）。
- 不替代 §5 的执行；第 1 步虽属本线可做范围，但一次性涉及 5 组路由 + 语义对齐，
  建议作为独立任务单独验收，不与其他改动混批。
- 不动 `internal/controlplane`、`internal/store`（并行线领地）。
