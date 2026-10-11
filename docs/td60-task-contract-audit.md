# TD-60 task 域契约对账报告（batch-exec / batch 状态 / canary）

> **定位**：task 域逐端点契约对账，task 施工图 §6 第 2 步。**结论先说：后端两侧（单体 vs
> task-svc）对账基本一致，裂口全在前端**——且前端这条路**当前就是断的**（不是切流才会断）。
> **基准**：`3d60de2`（2026-10-11）。所有结论以 `file:line` 标注，可逐条复核。

---

## 1. 结论摘要

| 端点 | 单体 vs task-svc | 前端 vs 后端 | 判定 |
|---|---|---|---|
| `POST /tasks/batch-exec` 请求体 | **逐字段一致** | ❌ 字段名两套 | **前端活缺陷：现网即 400** |
| `GET /tasks/batch/{id}` 响应 | **逐字段一致** | ❌ 前端读的 4 个统计字段两侧都不返回 | **前端活缺陷：统计位恒 0** |
| `POST /tasks/canary` / `{id}/advance` | 语义同源（策略分阶段/推进） | 未逐字段核对（见 §4） | 待核 |
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

**未逐字段核对的部分**：响应体字段名（`canaryID`/`phases[].{phase,deviceIDs,status}`）
在两侧注释中声称一致（`gateway_canary.go:3-6`），但未逐字 diff。
**前置阻塞**：canary 依赖 `canaryStore` 注入（task-svc `service.go:82`，
nil 时 API 503），接线时须确认 main 已注入。

---

## 5. 与切流的关系

| 问题 | 结论 |
|---|---|
| 这些裂口会阻碍切流吗？ | **不会**。裂口在前端与后端之间，与「走哪一侧」无关；但**让 task 域的核心功能（批量执行）现在是坏的**，优先级高于切流。 |
| 修完这些才算切流前置吗？ | 是 §6 第 2 步「契约对账」的产出物：本报告即交付；缺陷修复属独立小任务。 |
| 与 R6 的关系 | 独立。R6 是内存态，本报告是字段契约。 |

---

## 6. 待办清单（按优先级）

1. **修 batch-exec 请求字段名**（前端 `agentIDs`→`deviceIDs`、`type`→`taskType`）：
   活缺陷，2 处改动 + 单测。
2. **补 batch 状态响应四字段**（两侧 `handleBatchStatus`）：task-svc 侧有现成计数，
   单体侧按 tasks 聚合。
3. **canary 响应逐字段核对** + 确认 `canaryStore` 已注入 main。
4. 之后才是施工图 §6 第 3 步（`TASK_SVC_OWNS_NEW_TASKS` 开关，需产品）。

---

## 7. 局限

- §2/§3 的「前端现网即断」为**代码推演**（字段名比对 + handler 分支阅读），
  未起真实前端打过；建议修完后用 e2e-real 的 batch 用例补一次真实验证
  （当前 e2e-real 无 batch 用例，见 §6 可选项）。
- 未核对 `timeout` 字段在前端是否有入口（表单未见，可能产品侧不需要）。
- 未核对 batch 状态在**多副本**下的表现（R6 已记：内存态多副本不成立）。