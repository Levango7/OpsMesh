# TD-60 三域双轨一致性静态审计（2026-11-17）

> **定位**：TD-60 阶段 2（task / device / auth）M1 补测。只读静态审计 + 两处 P0 最小修复；
> **未启动双栈、未做切流、未迁移数据**。基线 `89012346`（main HEAD）。
> 工作区存在其他线的未提交改动；本审计未触碰 `services/*` 与任何脏文件。
>
> **核心结论**：`docs/td60-consistency-report.md` v1.2 的 100% 是「路径对齐率」，不是字段级契约。
> 本次按「方法 × 路径操作 + 响应字段 + 状态词表 + 错误信封」复核，发现 **5 个 P0**（已修 2、余 3），
> 其中 device 域列表/详情字段名不一致是切流后前端直接空白的阻断项。

---

## 1. 方法与工具

### 1.1 工具盘点

| 检查面 | 结果 |
|---|---|
| `scripts/` | 仅 `coord-gate.py`、`authz-route-audit.py`、`qa-matrix.py`、安全/压测脚本；无 td60/consistency/dual-track 生成器 |
| `deploy/scripts/` | 仅有运行时冒烟：`smoke-cutover.sh`（auth 真进程）、`smoke-task-cutover.sh`、`drill-batch0.sh`；均需 live 栈，不能离线出数 |
| `Makefile` | 目标为 `build/backend/frontend/test/vet/services-build/services-test/ci/run/...`；无 `td60`/`consistency`/`dual-track` 目标 |
| 结论 | **无离线一致性生成器**；本次未新建脚本，按只读读码 + 路由/模型/前端消费点 grep 完成 |

### 1.2 静态审计口径

- controlplane 路由：`internal/controlplane/server_lifecycle.go` 的 `mux.HandleFunc` 注册 + 各域子路由分派。
- 微服务路由：`services/*/internal/http/gateway*.go` 的 `Register*Routes` 与 handler 内方法分派。
- 状态词表：controlplane `server_tasks.go` 的状态白名单 / `server_batch.go` 聚合逻辑；task-svc `internal/models/models.go` 与 `gateway_batch.go`。
- 错误信封：两侧 `writeError`/`WriteJSON` 的 `{"error": ...}` 形态。
- 字段契约：`internal/proto/model.go`、`internal/domain/domain.go`、`services/*/internal/models`、前端 `web/enterprise/src/{api,views,stores}` 的实际读取键。
- 计数单位为 **方法 × 路径操作**（同一路径不同方法分别计数；子资源分派按实际动作计）。

---

## 2. 当前数字

| 域 | controlplane 操作数 | 微服务操作数 | 共有 | 仅 controlplane | 仅微服务 |
|---|---:|---:|---:|---:|---:|
| task | 41 | 34 | 34 | 7 | 0 |
| device | 16 | 32 | 15 | 1 | 17 |
| auth | 18 | 19 | 18 | 0 | 1 |
| **合计** | **75** | **85** | **67** | **8** | **18** |

> 说明：仅微服务多出的操作多为 agent 通道写操作（心跳/注册）、CMDB/发现子域、provision register，
> 以及 auth-svc 的 `GET /roles/{id}`；这些不都属于「切流必须补齐」，但必须在切流前逐项决定
> 「保留本地 / 转发 / 显式 404」。本审计只把会造成前端功能断裂或权限边界变化的问题列为 P0。

### 2.1 严重度汇总

| 严重度 | 数量 | 已修 | 剩余 |
|---|---:|---:|---:|
| P0 | 5 | 2 | 3 |
| P1 | 10 | 0 | 10 |
| P2 | 8 | 0 | 8 |

---

## 3. 差异表

### 3.1 task 域

| ID | surface | controlplane | task-svc | 严重度 | 证据（file:line） |
|---|---|---|---|---|---|
| T-1 | HTTP 代理权限分级 | 所有方法统一 `task:read`（只读可建/取消/审批；写权限反被 403） | —（代理层问题） | **P0（已修）** | `internal/controlplane/server_lifecycle.go:803-835` |
| T-2 | batch / canary 路由归属 | `TASK_SVC_PROXY_ENABLED=true` 时仍无条件注册本地 `handleBatchExec`/`handleBatchStatus`/`handleCanary*`；共享 DB 时任务仍可被 task-svc agent 领取，但 batch/canary 元数据与计数只见本地，切流后「谁拥有批次视图」未定义 | task-svc 有自己的 batch/canary 实现与内存批次索引 | P1（切流前必须定义归属，否则双写/计数漂移） | `server_lifecycle.go:366,731-741`；`services/task-svc/internal/http/gateway_batch.go:286-289`、`gateway_canary.go:24-26` |
| T-3 | DLQ retry / purge | 代理模式下 `POST /tasks/{id}/retry`、`DELETE /tasks/{id}/dead-letter` 显式 501（task-svc 无 purge/retry 接口）；前端 DLQ 页面依赖这两个动作 | 无对应接口 | P1 | `server_lifecycle.go:985,1000`；`web/enterprise/src/api/dead-letter.js:4-10` |
| T-4 | batch 聚合 status 词表 | 全成功 = `completed`，混合 = `partial`，全失败 = `failed`，默认 `running` | 全成功 = `succeeded`，全未完成 = `pending`，其余 `running`，有失败 = `failed` | P1 | `internal/controlplane/server_batch.go:386-394`；`services/task-svc/internal/http/gateway_batch.go:248-259` |
| T-5 | 任务列表分页 | `page/pageSize` 存在时返回 `{data,total,page,pageSize,hasMore}` | 只支持 `limit`，返回裸数组；代理 `handleTaskProxyRoot` 固定 `Limit=2000`、忽略 `page` | P1 | `internal/controlplane/server_tasks.go:311`；`services/task-svc/internal/http/gateway.go:185-188`；`server_lifecycle.go:1008-1026` |
| T-6 | task 状态词表 | 白名单含 `blocked`、`dead_letter` | models 常量无 `blocked`/`dead_letter`（只有 `deadLetter` 布尔） | P2 | `server_tasks.go:179`；`services/task-svc/internal/models/models.go:6-15` |
| T-7 | `POST /api/v1/tasks/batch`（多 agent 批量下发） | 有路由与 handler | 无该路由（只有 batch-exec / batch/{id}） | P2（前端已移除该入口，死代码） | `server_lifecycle.go:74-75`；`gateway_batch.go:286-289`；`web/enterprise/src/api/batch.js:8` |
| T-8 | canary 流量分割 / 指标 | `/api/v1/tasks/canary/{id}/traffic-split`、`/metrics` 与 `/api/v1/canary/{id}/...`；前端走 `/canary/{id}/...` | 无这两个路径族 | P1 | `server_lifecycle.go:736-740`、`:472`；`web/enterprise/src/api/canary.js:23-29` |

**已对齐**：core task 状态（pending/claimed/running/done/failed/cancelled/pending_approval/rejected）、
错误信封 `{"error": ...}`、cancel/result/approve/reject 的 HTTP 语义、schedules pause/resume。

### 3.2 device 域

| ID | surface | controlplane | device-svc | 严重度 | 证据（file:line） |
|---|---|---|---|---|---|
| D-1 | `GET /api/v1/devices` 列表项字段 | `DeviceInfo`：`deviceID/segment/state/taskState/managed/lastResult/lastResultAt/hostname/os/arch` | `models.Device`：`id/name/group/status/...`（直接序列化，无映射） | **P0（剩余）** | `internal/proto/model.go:39-55`；`services/device-svc/internal/models/models.go:5-21`；`services/device-svc/internal/http/gateway.go:129-131` |
| D-2 | `GET /api/v1/devices/{id}` 详情 | `{device: domain.Device, tasks, results}`，device 用 `deviceID/state/taskState/segment` | `{device: models.Device, tasks, results}`，device 用 `id/status/group/name` | **P0（剩余）** | `internal/controlplane/server_devices.go:159-185`；`services/device-svc/internal/http/gateway.go:191-204` |
| D-3 | `GET /api/v1/devices/{id}/system` | 有（系统用户/组/防火墙/服务清单） | 无该子路径（`{id}` 分派只认 heartbeat/status/provision/metrics） | **P0（剩余）** | `server_devices.go:248`；`services/device-svc/internal/http/gateway.go:149-184`；`web/enterprise/src/api/system-ops.js:15` |
| D-4 | `/api/v1/device-svc/provision/auto` 转发规则 | 双轨前缀缺该规则 ⇒ 404（前端曾因此退回本地路径） | `POST /api/v1/provision/auto` 已实现 | **P0（已修）** | `internal/controlplane/svcproxy/proxy.go:301-318`；`services/device-svc/internal/http/gateway.go:95` |
| D-5 | `GET /devices` / `GET /agents` 分页 | `page/pageSize` opt-in 信封 `{data,total,...}` | 只按 `limit` 截断，无分页信封 | P1 | `server_devices.go:44-70,112-126`；`services/device-svc/internal/http/gateway.go:120-146,391-430` |
| D-6 | `GET /api/v1/agents` 字段 | `agentID/hostname/segment/status/lastSeen/live` | `agentID/hostname/segment/status`（无 lastSeen/live） | P1 | `server_devices.go:90-103`；`services/device-svc/internal/http/gateway.go:423-428` |
| D-7 | `DELETE /api/v1/devices/{id}` 响应体 | `{status:"retired", deviceID:id}` | `{status:"retired"}`（缺 deviceID） | P2 | `server_devices.go:285`；`services/device-svc/internal/http/gateway.go:223` |
| D-8 | `GET /devices/{id}/metrics` 无数据语义 | 无设备/无数据分别按 404/空处理 | 未注入 metricsStore 也返回 200 + 空 metrics（降级语义不同） | P2 | `internal/controlplane/device_metrics.go:39`；`services/device-svc/internal/http/gateway.go:303-326` |

**已对齐**：单设备 provision 的 `{status:"provisioning", deviceID, installToken, bootstrap}`、metrics 的
`?range=` 参数族、退役的软删除语义、netsec 六端点（同一 `pkg/netsec` 脚本构建）。

### 3.3 auth 域

| ID | surface | controlplane | auth-svc | 严重度 | 证据（file:line） |
|---|---|---|---|---|---|
| A-1 | 管理端点经双轨前缀 | `/api/v1/auth-svc/*` 统一改写为 `/api/v1/auth/*`；`/api/v1/auth-svc/users` → `/api/v1/auth/users`，auth-svc 未注册该路径 ⇒ 404 | 管理端点在 `/api/v1/users`、`/roles`、`/permissions` | P1（M3 前必须决策：补规则或声明管理面保持本地） | `internal/controlplane/svcproxy/proxy.go:213-227`；`services/auth-svc/internal/http/gateway.go:138-142` |
| A-2 | 非 active 账号登录 | 403，按 pending/disabled/rejected 分文案 | 统一 401 `invalid credentials` | P1 | `internal/controlplane/auth_login.go:211-220`；`services/auth-svc/internal/http/gateway.go:209-212` |
| A-3 | `GET /api/v1/users` 分页 | `page/pageSize` 时回 `{users,total,page,pageSize,hasMore}` | 恒回 `{users:[...]}`，忽略 page/pageSize | P1（前端 UsersView 恒传 page） | `internal/controlplane/auth_users.go:61-79`；`services/auth-svc/internal/http/gateway.go:636`；`web/enterprise/src/views/UsersView.vue:215` |
| A-4 | 登录成功体 | 体带 `token` + `user` | 体不带 token，只写 Cookie | P2（前端不消费 token，有意设计） | `auth_login.go:263`；`services/auth-svc/internal/http/gateway.go:234-239` |
| A-5 | logout / refresh 成功体 | `{message:"logged out"}` / `{user}` | `{status:"ok",sessionRevocation}` / `{status:"ok"}` | P2（前端不消费体） | `auth_login.go:296` 与 refresh 分支；`services/auth-svc/internal/http/gateway.go:274,284` |
| A-6 | 口令强度 | 最小 8 位，不要求特殊字符 | 最小 12 位 + 特殊字符（更严） | P2（收紧方向，需切流公告） | `internal/controlplane/credentials/password.go:24`；`services/auth-svc/internal/http/password.go` |
| A-7 | `GET /api/v1/roles/{id}` | 无独立 GET 详情（靠列表） | 有 | P2（能力增强，非冲突） | `server_lifecycle.go:203`；`services/auth-svc/internal/http/gateway.go:141` |

**已对齐（本轮复核确认，旧报告的若干缺口已闭合）**：

- `/auth/me` 已补 `permissions`、`roleIDs`、`tenantId`、`status`、`mustChangePassword`，与 controlplane `store.User` 形状一致
  （`services/auth-svc/internal/http/gateway.go:549-580`）。
- 注册：公开注册闸门、`userId` 回包、pending 原子创建均已对齐（`gateway.go:303-376`）。
- 改密：IP 限流、新旧相同拒绝、首登 token 分支、常规会话保留均已对齐（`gateway.go:388-470`）。
- Cookie 名/Path/HttpOnly/SameSite/Secure/清除语义、at 15min / rt 7d 一致。

---

## 4. 已修复项

### 4.1 device：补 `/api/v1/device-svc/provision/auto` 代理规则（P0）

- 文件：`internal/controlplane/svcproxy/proxy.go:301-318`
- 内容：新增 `DeviceExtras` 规则，`PublicPrefix=/api/v1/device-svc/provision/auto`，
  `UpstreamPrefix=/api/v1/provision/auto`，权限 `provision:execute`。
- 为什么精确到 `/auto` 而不是 `/provision`：避免把 agent 自举的
  `/api/v1/provision/register`（无 JWT、token 自证）卷进需要租户上下文的代理鉴权面。
- 守卫：`internal/controlplane/service_proxy_test.go:356`（改写用例）、`:387`（Lookup 用例）。
- 影响：双轨前缀下自动纳管不再 404，权限与本地 `POST /api/v1/provision/auto` 一致。

### 4.2 task：代理权限按方法/路径分级（P0）

- 文件：`internal/controlplane/server_lifecycle.go:803-835`
- 内容：新增 `taskProxyPermission(method, path)`，把代理层从「一律 `task:read`」改为：
  `GET → task:read`；`POST 创建/retry → task:write`；`POST cancel → task:cancel`；
  `POST approve/reject → task:approve`；`DELETE 死信清除 → task:write`。
- 守卫：`internal/controlplane/td60_task_proxy_perm_test.go:13-33`（9 条矩阵，含写/取消/审批/重试/DLQ）。
- 影响：只读凭证不能经代理写任务；写权限不再被错误拒绝；切流前后权限边界一致。

> 未改 `services/*`，未改其他线脏文件，未做任何切流/数据迁移。

---

## 5. 验证

| 命令 | 结果 |
|---|---|
| `go build ./...` | **PASS**（exit 0） |
| `go vet ./internal/...` | **PASS**（exit 0） |
| `gofmt -l internal` | **空**（exit 0） |
| `go test ./internal/controlplane/svcproxy/ -count=1` | **PASS**（0.238s） |
| `go test ./internal/controlplane/ -run 'TestTaskProxyPermissionMatrix|TestDeviceProxyRuleRewrite|TestDeviceProxyLookup|TestRewriteProxyPath' -count=1` | **PASS**（0.355s） |
| `go test ./internal/controlplane/... -count=1` | **PASS**（全包，主包 89.3s） |

未运行 `services/*` 测试（本轮未改服务侧）；未运行 `smoke-cutover.sh` / `smoke-task-cutover.sh`
（需 live 双栈，本轮明确为静态审计）。

---

## 6. 剩余 P0 / P1 / P2 清单

### P0（剩余 3 项，切流前必须闭合）

1. **device 列表字段契约**：`GET /devices` 的 `DeviceInfo` ↔ `models.Device` 映射缺失。
   建议在 device-svc 增加与 `internal/proto.DeviceInfo` 同名的响应 DTO
   （`deviceID/segment/state/taskState/...`），不要改 controlplane 契约。
2. **device 详情字段契约**：`GET /devices/{id}` 的 `device` 对象同样使用 `id/status/group/name`，
   与 controlplane `domain.Device` 的 `deviceID/state/segment/...` 不一致。
3. **device `/system` 端点**：补齐 `GET /api/v1/devices/{id}/system`，否则设备系统清单页切流即 404。

### P1（切流前必须闭合或有 owner 签字的接受记录）

1. **task batch/canary 路由归属**：`TASK_SVC_PROXY_ENABLED=true` 时 batch/canary
   仍落本地；需在代理模式下改走 task-svc，或明确「batch/canary 保持本地」并固定批次视图 owner。
2. **task DLQ**：代理模式下 retry/purge 返回 501；需 task-svc 补 retry/purge 接口，或 DLQ 明确保持本地。
3. **task batch 聚合状态词表**：`completed/partial` vs `succeeded/pending` 二选一，写进契约测试。
4. **task 列表分页**：task-svc 补 `page/pageSize` 信封，或在代理根路由透传并保持裸数组（需前端明确）。
5. **task canary 流量分割/指标**：`/canary/{id}/traffic-split|metrics` 未在 task-svc 实现。
6. **auth 管理端点双轨路径**：补 `/api/v1/auth-svc/users|roles|permissions` 的转发规则，或在
   `docs/td60-cutover-decisions.md` 明确「管理面 M4 前保持本地」。
7. **auth 非 active 登录码**：403 vs 401 统一（建议统一 401 + 不发账号状态文案，防枚举）。
8. **auth 用户列表分页**：auth-svc 补 `{users,total,page,pageSize,hasMore}`，否则 UsersView 分页失效。
9. **device 分页**：device-svc 补 `page/pageSize` opt-in 信封，保持与 controlplane 同形。
10. **device agents 字段**：补 `lastSeen` / `live`（前端当前未消费，但演练报告已把 live 作为运维判据）。

### P2（可随域切流一并处理）

- task 状态词表补 `blocked` / `dead_letter`（`models.go`）。
- task `POST /api/v1/tasks/batch` 死代码：要么删除 controlplane 路由，要么 task-svc 补齐（前端已不用）。
- device `DELETE` 响应补 `deviceID`，与 controlplane 逐字一致。
- device metrics 无数据：404 与 200+空 二选一，写进契约。
- auth 登录体 token：保持 Cookie-only（有意），但需在 API 文档标注脚本/Bearer 客户端影响。
- auth logout/refresh 响应体：`message` vs `status` 统一（前端不消费，低风险）。
- 口令策略：8 vs 12 位 + 特殊字符，切流公告必须写明「改密/注册按更严一侧」。
- auth-svc 独有的 `GET /roles/{id}`：保留为能力增强，不算冲突。

---

## 7. 建议的下一里程碑

**M1.5「服务侧契约补齐 + 静态门禁」**（建议 3~5 人日，先于 M2 50/50）：

1. **先修 device 三个 P0**（字段 DTO、`/system`、分页），因为它们是「切流即空白/404」的确定性断裂；
   同时补 device-svc 契约测试，断言响应键集合与 `internal/proto.DeviceInfo` 完全一致。
2. **task 路由归属收口**：把 `registerBatchCanaryRoutes` 纳入 `TASK_SVC_PROXY_ENABLED` 的同一判据，
   并决定 batch/canary/DLQ 是「task-svc 实现」还是「明确本地」；batch 聚合状态词表同步定稿。
3. **auth 管理面决策**：补代理规则或写死「M4 前本地」；同时统一非 active 登录码与用户列表分页。
4. **把本次静态审计固化为可复跑脚本**（建议 `scripts/td60-consistency.py`）：
   从 `server_lifecycle.go` / `services/*/internal/http` 提取路由，从 `models`/`proto` 提取响应键，
   输出与本文同形的差异表；接入 CI 作为 `make td60-consistency` 门禁，防止路径 100% 但字段再次漂移。
5. 在 M2 前补 task/device 的等价 `smoke-*-cutover.sh`，把「批处理/字段级」差异纳入真进程冒烟；
   现有 `smoke-cutover.sh` 只覆盖 auth 自服务面。

---

## 8. 附：本次审计的可复跑命令

```bash
# 路由面
rg -n 'mux.HandleFunc|Register.*Routes' internal/controlplane/server_lifecycle.go \
  internal/controlplane/netsec_ext_ops.go services/*/internal/http/*.go

# 状态词表
rg -n 'TaskStatus|BatchStatus|case "pending"|aggStatus|batchOverallStatus' \
  internal/controlplane services/task-svc/internal

# 响应字段
rg -n 'type DeviceInfo|type Device struct|type taskResponse|toPublicUser|json:"deviceID"' \
  internal/proto internal/domain services/*/internal

# 前端实际消费键（用于判定字段差异是否 P0）
rg -n 'dev\.|current\.|\.permissions|roleIDs|tenantId' web/enterprise/src/views web/enterprise/src/stores
```

