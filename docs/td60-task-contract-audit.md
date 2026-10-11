# TD-60 task 域契约对账报告（batch-exec / batch 状态 / canary）

> **定位**：task 域逐端点契约对账，task 施工图 §6 第 2 步。**结论先说：后端两侧（单体 vs
> task-svc）对账基本一致，裂口全在前端**——且前端这条路**当前就是断的**（不是切流才会断）。
> **基准**：`3d60de2`（2026-10-11）。所有结论以 `file:line` 标注，可逐条复核。

---

## 1. 结论摘要

| 端点 | 单体 vs task-svc | 前端 vs 后端 | 判定 |
|---|---|---|---|
| `POST /tasks/batch-exec` 请求体 | **逐字段一致** | ~~❌ 字段名两套~~ | ✅ **已修**（前端 `f23a0b0`） |
| `GET /tasks/batch/{id}` 响应 | **逐字段一致** | ~~❌ 前端读的 4 个统计字段两侧都不返回~~ | ✅ **task-svc 侧已补**；单体侧待并行线 |
| `POST /tasks/canary` / `{id}/advance` | 语义同源 + 响应字段逐字一致 | 未逐字段核对（前端无 canary 调用） | ✅ 已核对无裂口 |
| `POST /tasks/batch` | 已在上一轮拆除（死代码） | — | 已处置 |

---

## 2. batch-exec 请求体：后端一致，前端字段名不符

| 字段 | 单体 `server_batch.go:126` | task-svc `gateway_batch.go:103-108` | 前端 `BatchView.vue:194-200` |
|---|---|---|---|
| 设备集合 | `deviceIDs` | `deviceIDs` | **`agentIDs`** ❌ |
| 任务类型 | `taskType` | `taskType` | **`type`** ❌ |
| 命令 | `command` | `command` | `command` ✅ |
| 内容 | `content` | `content` | `content` ✅ |
| 路径 | `path` | `path` | `path` ✅ |
| 超时 | `timeout` | `timeout` | 未传（可空，后端默认 0） |

**后果（实测推演，非推断）**：前端发 `agentIDs=[]` 时的 `deviceIDs` 为空 ⇒
两侧 handler 都走 `400 deviceIDs is required` ⇒ **批量执行在前端从来没能成功提交**。
`type` 不符是第二重：即使设备字段修好，`taskType` 为空会落到后端默认 `shell`
（行为降级但不报错）。

> 这是切流之前就该修的**既有缺陷**——与 TD-60 无关，是前端契约漂移。
> 修法：前端把 `agentIDs`/`type` 改为 `deviceIDs`/`taskType`（与后端对齐），
> 或后端加兼容层。**建议改前端**：后端两侧已一致，再加兼容层会制造三套写法。

## 3. batch 状态响应：后端一致，前端读统计字段恒 0

两侧响应完全相同（单体 `server_batch.go:258-267` vs task-svc `gateway_batch.go` 尾部）：

```
{batchID, taskType, command, createdAt, createdBy, tasks: [{deviceID, taskID, status, error}]}
```

而前端 `BatchView.vue:78-90` 读 `current.total / succeeded / failed / status`——**四个字段两侧都不返回**：

| 前端读取 | 后端是否提供 | UI 表现 |
|---|---|---|
| `current.batchID` | ✅ `batchID` | 正常 |
| `current.total` | ❌ 无（有 `tasks[]` 数组可 len） | **恒 0** |
| `current.succeeded` | ❌ 无（需前端按 status 聚合） | **恒 0** |
| `current.failed` | ❌ 无（同上） | **恒 0** |
| `current.status` | ❌ 无（批次级状态未序列化） | **恒 `—`** |

> 说明：task-svc 的 store 层**有**这些计数（`BatchTask.TotalCount/SuccessCount/
> FailedCount/PendingCount/Status`，见 `api/proto/v1/task.pb.go:514-519`），
> 只是 `handleBatchStatus` 没把它们写进 HTTP 响应。所以这是**序列化遗漏**，
> 不是能力缺失。单体侧同（其 `batchTask` 结构体只存 Tasks 数组）。

**修法**（任选一，建议 ①）：
1. 两侧 `handleBatchStatus` 补 `total`/`succeeded`/`failed`/`status` 四字段
   （task-svc 侧可直接从 store 计数取，单体侧按 `tasks[].status` 聚合）；
2. 或前端改读 `tasks[]` 自行聚合——但 12 个服务口径已定「后端给统计」，
   前端聚合会让同一份逻辑散到多端。

## 4. canary：语义同源，字段待核

| 端点 | 单体 | task-svc |
|---|---|---|
| `POST /tasks/canary` | `server_batch.go:272`（body: deviceIDs/taskType/command/strategy/percentage/groups/labels） | `gateway_canary.go:35`（同名字段集） |
| `GET /tasks/canary/{id}` | `handleCanaryStatus`（刷新阶段任务状态） | `service.go:883 GetCanaryStatus`（同刷新逻辑） |
| `POST /tasks/canary/{id}/advance` | `handleCanaryAdvance` | `service.go:911 AdvanceCanary`（找下一个 pending 阶段执行） |

**字段已逐字核对（2026-10-11 补）**：状态响应两侧**逐字段一致**——`canaryID`/`strategy`/
`taskType`/`command`/`createdAt`/`createdBy`/`phases[].{phase,deviceIDs,status,tasks,
startedAt,finishedAt}`（单体 `server_batch.go` handleCanaryStatus 尾部 vs task-svc
`gateway_canary.go` handleCanaryStatus 尾部）。task-svc 侧无缺失字段。

**`canaryStore` 注入已确认**：`services/task-svc/cmd/task-svc/main.go:144`
`svc.SetCanaryStore(store.NewMemoryStore())`，故 canary API **不会 503**。
内存态属 R6 已记的阻断项（多副本不成立、重启丢视图），与字段契约无关。

---

## 5. 「新任务归哪侧」缺口：需要 TASK_SVC_OWNS_NEW_TASKS（第 4 项）

施工图 §6 第 3 步。经核验，**该开关目前不存在，且默认状态对切流是坏的**：

| 侧 | 建号入口 | 开关 | 默认 |
|---|---|---|---|
| auth-svc | `POST /api/v1/users`（管理建号）、`/auth/register` | `AUTH_SVC_HTTP_ENABLED` | **false**（gRPC-only） |
| task-svc | `POST /api/v1/tasks` | `TASK_SVC_HTTP_GATEWAY_ENABLED` | **true**（HTTP 网关开） |

⇒ 现状是「控制面与 task-svc 两侧都能建任务」，没有任何开关表达「新任务归哪侧持有」。
切流打开 `OPSMESH_SERVICE_PROXY=task` 后，两侧同时可写 ⇒ 与 auth 域曾有的
「账号建在被切走的一侧，用户登录时对侧查无此人」是同一形态的问题。

**设计（照 auth 域同构，不另造平行设施）**：
- 在 peer 的 `cutoverRouter`（`internal/controlplane/auth_cutover.go` 那套）中加
  `OwnsNewTasks()`，env `TASK_SVC_OWNS_NEW_TASKS`，默认 **false**；
- 判据照 `auth_users.go:98-105`：开关为 true 且总闸开启时，本地建号
  **409 + 指引去 task-svc 侧**，而不是静默建在本地；
- compose 补 `TASK_SVC_OWNS_NEW_TASKS: ${TASK_SVC_OWNS_NEW_TASKS:-false}`
  （挨着 `AUTH_SVC_OWNS_NEW_ACCOUNTS:527` 放，保持两个开关相邻可读）。

**为何不由本线实现**：`cutoverRouter` 与 `auth_cutover.go` 属并行线领地，且它是
auth/task 两域共用的切流路由基础设施——在同构处加方法是唯一正确做法，
另造一套会让两域的切流语义分叉。本报告给出设计与落点，实现归并行线。

> 附：auth 域「名册管存量、开关管增量，两者同时打开才意味着这批切完」
> （`auth_login.go:54` 注释）这一原则对 task 域同样适用：名册/cutover 机制
> task 域已具备（`svcproxy/proxy.go:327`），缺的只是这个增量开关。



| 问题 | 结论 |
|---|---|
| 这些裂口会阻碍切流吗？ | **不会**。裂口在前端与后端之间，与「走哪一侧」无关；但**让 task 域的核心功能（批量执行）现在是坏的**，优先级高于切流。 |
| 修完这些才算切流前置吗？ | 是 §6 第 2 步「契约对账」的产出物：本报告即交付；缺陷修复属独立小任务。 |
| 与 R6 的关系 | 独立。R6 是内存态，本报告是字段契约。 |

---

## 6. 与切流的关系

| 问题 | 结论 |
|---|---|
| 这些裂口会阻碍切流吗？ | **不会**。裂口在前端与后端之间，与「走哪一侧」无关；但**让 task 域的核心功能（批量执行）现在是坏的**，优先级高于切流。 |
| 修完这些才算切流前置吗？ | 是本报告 §1 待办 1/2 的交付物；§1 待办 4（`OWNS_NEW_TASKS`）是最后一个切流前置。 |
| 与 R6 的关系 | 独立。R6 是内存态，本报告是字段契约。 |

## 7. 待办清单与当前状态

| # | 事项 | 状态 |
|---|---|---|
| 1 | 修前端 batch-exec 字段名（`agentIDs`→`deviceIDs`、`type`→`taskType`） | ✅ **已完成**（`f23a0b0`） |
| 2 | 补 batch 状态响应四字段（`total`/`succeeded`/`failed`/`status`/`pending`） | ✅ **task-svc 侧已完成**（本轮）；单体侧按 tasks 聚合，属并行线领地 |
| 3 | canary 响应逐字段核对 + `canaryStore` 注入确认 | ✅ **已核对**：字段两侧逐字一致，store 已注入（`main.go:144`） |
| 4 | `TASK_SVC_OWNS_NEW_TASKS` 开关 | 📋 **设计已给出**（§5），实现需并行线在 `cutoverRouter` 落地 |

## 8. 局限

- §2/§3 的「前端现网即断」为**代码推演**（字段名比对 + handler 分支阅读），
  未起真实前端打过；建议修完后用 e2e-real 的 batch 用例补一次真实验证
  （当前 e2e-real 无 batch 用例，见 §7 可选项）。
- 未核对 `timeout` 字段在前端是否有入口（表单未见，可能产品侧不需要）。
- 未核对 batch 状态在**多副本**下的表现（R6 已记：内存态多副本不成立）。
- §2 的统计字段按**实际任务项聚合**（而非 store 的 BatchTask 计数）：内存索引与
  store 不保证同步，按 items 聚合才不说谎；若将来 R6 落库，应改为读 store 计数。