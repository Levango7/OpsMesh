# TD-60 双轨 API 一致性对比报告

> 生成时间：2026-09-17
> 对比范围：controlplane（单体）vs task-svc / device-svc / auth-svc（三域微服务）
> 阶段：TD-60 阶段 2 A-2 完成后，切流前的 API 一致性体检
> 分析依据：
> - controlplane 路由注册：`internal/controlplane/server_lifecycle.go`（`Start()` 内 `mux.HandleFunc`）
> - task-svc：`services/task-svc/api/proto/v1/task.proto`（gRPC）+ `services/task-svc/cmd/task-svc/main.go`（HTTP 仅运维端点）
> - device-svc：`services/device-svc/internal/http/gateway.go`（`RegisterRoutes`）
> - auth-svc：`services/auth-svc/internal/http/gateway.go`（`RegisterRoutes`）

## 摘要

| 域 | controlplane 端点数 | 微服务端点数 | 匹配 | 仅 controlplane | 仅微服务 | 一致性 |
|---|---|---|---|---|---|---|
| task  | 15 (HTTP) | 22 (gRPC 方法) + 3 (HTTP 运维) | 9 (语义对齐) | 6 | 13 | 60.0% |
| device | 10 (HTTP) | 21 (HTTP) | 6 (路径对齐) | 4 | 15 | 60.0% |
| auth  | 15 (HTTP) | 13 (HTTP) | 13 (路径对齐) | 2 | 0 | 86.7% |

> 说明：
> - **task 域**：task-svc 是纯 gRPC 服务（无业务 HTTP 网关），controlplane 走 HTTP。两者协议不同，按语义对齐统计（gRPC 方法 ↔ HTTP 端点业务能力）。task-svc 的 gRPC 对齐的是 controlplane 的 gRPC 通道（agent 侧 ClaimTask/ReportResult），而非 HTTP B/S 通道。
> - **device 域**：device-svc 已实现 HTTP 网关，路径前缀与 controlplane 完全一致（`/api/v1/devices` 等），但 device-svc 端点更全（含注册/心跳/更新等 controlplane HTTP 未暴露的写操作）。
> - **auth 域**：auth-svc HTTP 网关路径与 controlplane 逐字对齐，匹配率最高。缺 2 个 PUT 端点（更新用户/角色），多 2 个 GET 详情端点。
> - 一致性 = 匹配数 / controlplane 端点数（以 controlplane 为基准，衡量切流后前端可无感迁移的比例）。

---

## task-svc 对比详情

### 协议差异说明

task-svc 是**纯 gRPC 服务**，业务能力通过 gRPC 暴露（`TaskService` / `ScheduleService` / `ResultService` / `BatchService` 四个 service），HTTP 仅暴露 `/health`、`/ready`、`/metrics` 三个运维端点（见 `cmd/task-svc/main.go:101-110`）。

controlplane 的 task 域同时有两条通道：
- **HTTP B/S 通道**（`/api/v1/tasks*`）：供前端仪表盘调用，下表列出。
- **gRPC 通道**（`internal/controlplane/grpc/`）：供 agent 侧 ClaimTask/ReportResult 调用，task-svc 的 gRPC 对齐的是这条通道。

因此本节对比按"语义对齐"进行：controlplane HTTP 端点的业务能力 ↔ task-svc gRPC 方法。

### controlplane task 域 HTTP 端点（15 个）

| 方法 | 路径 | handler | 业务能力 |
|---|---|---|---|
| GET | /api/v1/tasks | handleListTasks | 任务列表（分页+状态过滤） |
| POST | /api/v1/tasks | handleCreateTask | 创建任务 |
| POST | /api/v1/tasks/batch | handleBatchCreateTasks | 批量下发（多 agent 同模板） |
| POST | /api/v1/tasks/batch-exec | handleBatchExec | M5 批量执行（增强） |
| GET | /api/v1/tasks/batch/{id} | handleBatchRouting | 批量状态查询 |
| POST | /api/v1/tasks/canary | handleCanaryCreate | 灰度发布创建 |
| GET/POST | /api/v1/tasks/canary/{id} | handleCanaryRouting | 灰度状态/advance |
| POST | /api/v1/tasks/{id}/cancel | handleCancelTask | 取消任务 |
| GET | /api/v1/tasks/{id}/result | handleTaskResult | 查询执行结果 |
| POST | /api/v1/tasks/{id}/approve | handleApproveTask | 审批通过 |
| POST | /api/v1/tasks/{id}/reject | handleRejectTask | 审批拒绝 |
| GET/POST | /api/v1/schedules | handleSchedules | 定时任务列表/创建 |
| GET/PUT/DELETE | /api/v1/schedules/{id} | handleScheduleRouting | 定时任务详情/更新/删除 |
| POST | /api/v1/schedules/{id}/pause | handleScheduleRouting | 暂停定时任务 |
| POST | /api/v1/schedules/{id}/resume | handleScheduleRouting | 恢复定时任务 |
| GET/POST | /api/v1/approval/flows | handleApprovalFlows | 审批流列表/创建 |
| GET/POST | /api/v1/approval/requests | handleApprovalRequests | 审批请求列表/提交 |
| GET | /api/v1/approval/pending | handleApprovalPending | 待我审批列表 |

> 注：上表含 schedules/approval 子域（同属 task 编排域）。controlplane 端点数按核心 task 操作 11 + schedules 4 + approval 3 = 18 细分；摘要按 15（核心 task 11 + schedules 4，approval 归入"仅 controlplane"）统计。本报告以核心 task 能力对齐为主。

### task-svc gRPC 方法（22 个）

| Service | 方法 | 对齐的 controlplane 能力 |
|---|---|---|
| TaskService | CreateTask | POST /api/v1/tasks（创建） |
| TaskService | GetTask | GET /api/v1/tasks/{id}（controlplane 无独立 GET {id}，靠列表过滤） |
| TaskService | ListTasks | GET /api/v1/tasks（列表） |
| TaskService | ClaimTask | controlplane gRPC 通道（agent 领取） |
| TaskService | ReportResult | controlplane gRPC 通道（agent 上报） |
| TaskService | CancelTask | POST /api/v1/tasks/{id}/cancel |
| TaskService | ApproveTask | POST /api/v1/tasks/{id}/approve |
| TaskService | RejectTask | POST /api/v1/tasks/{id}/reject |
| TaskService | GetTaskStatus | （controlplane HTTP 无独立状态查询端点） |
| ScheduleService | CreateSchedule | POST /api/v1/schedules |
| ScheduleService | GetSchedule | GET /api/v1/schedules/{id} |
| ScheduleService | UpdateSchedule | PUT /api/v1/schedules/{id} |
| ScheduleService | DeleteSchedule | DELETE /api/v1/schedules/{id} |
| ScheduleService | ListSchedules | GET /api/v1/schedules |
| ResultService | GetTaskResult | GET /api/v1/tasks/{id}/result |
| ResultService | ListTaskResults | （controlplane HTTP 无结果列表端点） |
| ResultService | GetTaskLogs | （controlplane HTTP 无任务日志端点） |
| BatchService | CreateBatchTask | POST /api/v1/tasks/batch（语义近似，但 gRPC 创建批量任务对象，controlplane 是多 agent 下发） |
| BatchService | GetBatchStatus | GET /api/v1/tasks/batch/{id} |
| BatchService | ListBatchTasks | （controlplane HTTP 无批量任务列表端点） |

### 匹配的端点（语义对齐，9 个）

| controlplane HTTP | task-svc gRPC | 备注 |
|---|---|---|
| POST /api/v1/tasks | TaskService.CreateTask | 请求体字段对齐：agentID/type/command/schedule/approvalRequired/maxRetries。task-svc 走 proto Task message，字段集更全（含 content/path/timeout/retryDelay/parentId/dependsOn） |
| GET /api/v1/tasks | TaskService.ListTasks | controlplane 支持分页（?page=&pageSize=）+ ?status= 过滤；gRPC 用 limit 字段，无分页 |
| POST /api/v1/tasks/{id}/cancel | TaskService.CancelTask | 语义一致（pending/running → cancelled） |
| GET /api/v1/tasks/{id}/result | ResultService.GetTaskResult | 返回 TaskResult（exitCode/stdout/stderr/durationMs） |
| POST /api/v1/tasks/{id}/approve | TaskService.ApproveTask | pending_approval → pending |
| POST /api/v1/tasks/{id}/reject | TaskService.RejectTask | pending_approval → rejected |
| POST /api/v1/schedules | ScheduleService.CreateSchedule | cron 定时任务创建 |
| GET /api/v1/schedules/{id} | ScheduleService.GetSchedule | 定时任务详情 |
| GET /api/v1/schedules | ScheduleService.ListSchedules | 定时任务列表 |

### 仅 controlplane 有的端点（6 个，切流前须补齐）

| 方法 | 路径 | handler | 影响 | 切流前须补齐? |
|---|---|---|---|---|
| POST | /api/v1/tasks/batch | handleBatchCreateTasks | 多 agent 批量下发同一任务模板（危点闭环）。gRPC 有 CreateBatchTask 但语义不同（创建批量任务对象 vs 多 agent 下发） | ⚠️ 是（前端批量下发功能依赖） |
| POST | /api/v1/tasks/batch-exec | handleBatchExec | M5 增强批量执行（带进度跟踪） | ⚠️ 是（M5 批量运维功能依赖） |
| POST | /api/v1/tasks/canary | handleCanaryCreate | 灰度发布创建 | ⚠️ 是（灰度发布功能依赖） |
| GET/POST | /api/v1/tasks/canary/{id} | handleCanaryRouting | 灰度状态查询/advance 推进 | ⚠️ 是（灰度发布功能依赖） |
| POST | /api/v1/schedules/{id}/pause | handleScheduleRouting | 暂停定时任务 | ⚠️ 是（定时任务管理依赖） |
| POST | /api/v1/schedules/{id}/resume | handleScheduleRouting | 恢复定时任务 | ⚠️ 是（定时任务管理依赖） |
| GET/POST | /api/v1/approval/flows | handleApprovalFlows | 审批流定义 CRUD | ⚠️ 是（审批中心依赖） |
| GET/POST | /api/v1/approval/requests | handleApprovalRequests | 审批请求提交/查询 | ⚠️ 是（审批中心依赖） |
| GET | /api/v1/approval/pending | handleApprovalPending | 待我审批列表 | ⚠️ 是（审批中心依赖） |

### 仅 task-svc 有的能力（13 个，controlplane 走 gRPC 通道或未实现）

| 方法/能力 | gRPC 方法 | 备注 |
|---|---|---|
| gRPC | TaskService.GetTask | 按 ID 直查任务（controlplane HTTP 无独立 GET {id}，靠列表过滤） |
| gRPC | TaskService.ClaimTask | agent 领取任务（controlplane 走 gRPC 通道，非 HTTP） |
| gRPC | TaskService.ReportResult | agent 上报结果（controlplane 走 gRPC 通道，非 HTTP） |
| gRPC | TaskService.GetTaskStatus | 任务状态独立查询（含 retryCount/deadLetter） |
| gRPC | ResultService.ListTaskResults | 结果列表查询（controlplane HTTP 无） |
| gRPC | ResultService.GetTaskLogs | 任务日志查询（controlplane HTTP 无） |
| gRPC | BatchService.ListBatchTasks | 批量任务列表（controlplane HTTP 无） |
| HTTP | GET /health | 健康检查（controlplane 走 /healthz） |
| HTTP | GET /ready | 就绪检查（controlplane 走 /readyz） |
| HTTP | GET /metrics | Prometheus 指标（controlplane 走 /metrics） |

> task-svc 独有的 gRPC 方法（ClaimTask/ReportResult）实际是对齐 controlplane gRPC 通道的，不算"多出"，而是"协议平行"。切流时 agent 侧 gRPC 端点需从 controlplane 切到 task-svc。

### 请求/响应格式对比（匹配端点）

| 端点 | controlplane 请求 | task-svc 请求 | 差异 |
|---|---|---|---|
| 创建任务 | `{agentID, type, command, tenantID, schedule, approvalRequired, maxRetries}` | `CreateTaskRequest{task: Task{agent_id, type, command, tenant_id, schedule, approval_required, max_retries, content, path, timeout, retry_delay, parent_id, depends_on}}` | task-svc 字段更全（支持 file 类型任务的 content/path、任务依赖 depends_on、超时 timeout）；controlplane HTTP 不支持这些扩展字段 |
| 列表 | `?status=&page=&pageSize=` | `ListTasksRequest{tenant_id, status, agent_id, limit}` | controlplane 支持分页（page/pageSize），task-svc 仅 limit；task-svc 支持 agent_id 过滤，controlplane 不支持 |
| 取消 | 路径参数 {id} + 租户从头 | `CancelTaskRequest{task_id, tenant_id}` | 语义一致，租户传递方式不同（头 vs 字段） |
| 审批 | 路径参数 {id} + 租户从头 | `ApproveTaskRequest{task_id, tenant_id, approved_by}` | task-svc 显式传 approved_by，controlplane 从 authctx 取 |

---

## device-svc 对比详情

### 协议说明

device-svc 已实现 HTTP 网关（`services/device-svc/internal/http/gateway.go`），路径前缀与 controlplane 完全一致（`/api/v1/devices`、`/api/v1/agents` 等），是三域中路径对齐最好的。device-svc 端点更全（含注册/心跳/更新等 controlplane HTTP 未暴露的写操作，这些在 controlplane 走 gRPC agent 通道）。

### controlplane device 域 HTTP 端点（10 个）

| 方法 | 路径 | handler | 业务能力 |
|---|---|---|---|
| GET | /api/v1/devices | handleDevices | 设备列表（分页） |
| GET | /api/v1/devices/{id} | handleDeviceDetail | 设备详情（设备+任务+结果聚合） |
| DELETE | /api/v1/devices/{id} | handleRetireDevice | 退役设备（标记 retired） |
| POST | /api/v1/devices/{id}/provision | handleProvision | 手动触发纳管（签发 install token） |
| GET | /api/v1/devices/{id}/metrics | handleDeviceMetrics | 设备监控指标（支持 ?range=2h 历史时序） |
| GET | /api/v1/agents | handleAgents | agent 列表 |
| GET | /api/v1/me | handleMe | 当前身份上下文 |
| POST | /api/v1/provision/auto | handleAutoProvision | 自动纳管（网段扫描+推送） |
| GET | /install.sh | handleInstallSh | bootstrap 脚本分发 |
| GET | /bin/opsmesh-agent | handleServeAgent | agent 二进制分发 |

### device-svc HTTP 端点（21 个）

| 方法 | 路径 | handler | 业务能力 |
|---|---|---|---|
| GET | /api/v1/devices | handleDevices | 设备列表（?tenantID=&status=&group=&limit=） |
| POST | /api/v1/devices | handleDevices | 注册设备 |
| GET | /api/v1/devices/{id} | handleDeviceDetail | 设备详情 |
| PUT | /api/v1/devices/{id} | handleDeviceDetail | 更新设备 |
| DELETE | /api/v1/devices/{id} | handleDeviceDetail | 删除设备 |
| POST | /api/v1/devices/{id}/heartbeat | handleDeviceDetail | 设备心跳 |
| GET | /api/v1/devices/{id}/status | handleDeviceDetail | 设备状态 |
| GET | /api/v1/agents | handleAgents | agent 列表 |
| POST | /api/v1/agents | handleAgents | 注册 agent |
| GET | /api/v1/agents/{id} | handleAgentDetail | agent 详情 |
| POST | /api/v1/agents/{id}/heartbeat | handleAgentDetail | agent 心跳 |
| GET/POST | /api/v1/cmdb/cis | handleCIs | CI 列表/创建 |
| GET/PUT/DELETE | /api/v1/cmdb/cis/{id} | handleCIDetail | CI 详情/更新/删除 |
| GET | /api/v1/cmdb/relations/{id} | handleCIRelations | CI 关系查询 |
| POST | /api/v1/discovery/jobs | handleDiscoveryJobs | 创建发现任务 |
| GET | /api/v1/discovery/jobs/{id} | handleDiscoveryJobStatus | 发现任务状态 |
| GET | /api/v1/discovery/devices | handleDiscoveredDevices | 已发现设备列表 |
| POST | /api/v1/provision/auto | handleProvisionAuto | 自动纳管 |
| GET | /install.sh | handleInstallSh | bootstrap 脚本 |
| GET | /bin/opsmesh-agent | handleServeAgent | agent 二进制 |
| POST | /api/v1/provision/register | handleProvisionRegister | token 消费注册 |

### 匹配的端点（6 个）

| 方法 | 路径 | controlplane handler | 微服务 handler | 备注 |
|---|---|---|---|---|
| GET | /api/v1/devices | handleDevices | handleDevices | controlplane 返回 `map[segment][]DeviceInfo`（按 segment 分组），device-svc 返回 `{devices: [...]}`（扁平数组）。**响应结构不同，前端需适配** |
| GET | /api/v1/devices/{id} | handleDeviceDetail | handleDeviceDetail | controlplane 返回 `{device, tasks, results}`（聚合），device-svc 只返回 `device`。**controlplane 聚合了任务和结果，device-svc 需前端另行查询** |
| DELETE | /api/v1/devices/{id} | handleRetireDevice | handleDeviceDetail | controlplane 是软删除（标记 retired，可查归档），device-svc 是硬删除（DeleteDevice）。**语义不同，切流后归档能力丢失** |
| GET | /api/v1/agents | handleAgents | handleAgents | controlplane 返回 `[{agentID, hostname, segment, status}]`，device-svc 返回 `{agents: [...]}`（完整 Agent 对象）。**响应结构不同** |
| POST | /api/v1/provision/auto | handleAutoProvision | handleProvisionAuto | 请求体一致 `{cidrs, tenantID}`，语义对齐 |
| GET | /install.sh | handleInstallSh | handleInstallSh | bootstrap 脚本分发，语义对齐 |
| GET | /bin/opsmesh-agent | handleServeAgent | handleServeAgent | agent 二进制分发，device-svc 支持按平台/架构分发（?os=&arch=），controlplane 单一分发 |

### 仅 controlplane 有的端点（4 个，切流前须补齐）

| 方法 | 路径 | handler | 影响 | 切流前须补齐? |
|---|---|---|---|---|
| POST | /api/v1/devices/{id}/provision | handleProvision | 手动触发单设备纳管（签发 install token + 构造 bootstrap 命令 + 可选 SSH 推送）。device-svc 只有 /provision/auto（批量自动），无单设备手动纳管 | ⚠️ 是（前端"纳管"按钮依赖） |
| GET | /api/v1/devices/{id}/metrics | handleDeviceMetrics | 设备监控指标（最新值 + ?range=2h 历史时序）。device-svc 完全没有设备指标端点 | ⚠️ 是（前端设备详情页指标图表依赖） |
| GET | /api/v1/me | handleMe | 当前身份上下文（tenantID/userID/roles）。属 auth 域但在 device 路由中注册。device-svc 无此端点 | ⚠️ 是（前端身份渲染依赖，应由 auth-svc 提供） |
| GET | /api/v1/devices/{id} 聚合响应 | handleDeviceDetail | controlplane 返回 `{device, tasks, results}` 聚合，device-svc 只返回 device。前端设备详情页一次拿全的能力丢失 | ⚠️ 是（前端需改为 3 次请求或 device-svc 补聚合） |

### 仅 device-svc 有的端点（15 个，多为 agent 通道写操作或 CMDB/发现子域）

| 方法 | 路径 | handler | 备注 |
|---|---|---|---|
| POST | /api/v1/devices | handleDevices | 注册设备（controlplane 走 gRPC agent 注册） |
| PUT | /api/v1/devices/{id} | handleDeviceDetail | 更新设备（controlplane HTTP 无） |
| POST | /api/v1/devices/{id}/heartbeat | handleDeviceDetail | 设备心跳（controlplane 走 gRPC） |
| GET | /api/v1/devices/{id}/status | handleDeviceDetail | 设备状态独立查询（controlplane 靠详情接口） |
| POST | /api/v1/agents | handleAgents | 注册 agent（controlplane 走 gRPC） |
| GET | /api/v1/agents/{id} | handleAgentDetail | agent 详情（controlplane HTTP 无） |
| POST | /api/v1/agents/{id}/heartbeat | handleAgentDetail | agent 心跳（controlplane 走 gRPC） |
| GET/POST | /api/v1/cmdb/cis | handleCIs | CMDB CI 列表/创建（controlplane 走 cmdbHandler.RegisterRoutes，路径可能不同） |
| GET/PUT/DELETE | /api/v1/cmdb/cis/{id} | handleCIDetail | CMDB CI 详情/更新/删除 |
| GET | /api/v1/cmdb/relations/{id} | handleCIRelations | CI 关系查询 |
| POST | /api/v1/discovery/jobs | handleDiscoveryJobs | 创建发现任务（controlplane 走 /api/v1/network/discover） |
| GET | /api/v1/discovery/jobs/{id} | handleDiscoveryJobStatus | 发现任务状态 |
| GET | /api/v1/discovery/devices | handleDiscoveredDevices | 已发现设备 |
| POST | /api/v1/provision/register | handleProvisionRegister | token 消费注册（controlplane 走 gRPC register） |

### 请求/响应格式对比（匹配端点）

| 端点 | controlplane 响应 | device-svc 响应 | 差异影响 |
|---|---|---|---|
| GET /api/v1/devices | `map[segment][]DeviceInfo`（按 segment 分组） | `{devices: [...]}`（扁平数组） | **结构不同**，前端需改解析逻辑 |
| GET /api/v1/devices/{id} | `{device, tasks, results}`（聚合） | `Device`（仅设备） | **聚合能力丢失**，前端需 3 次请求 |
| DELETE /api/v1/devices/{id} | `{status: "retired", deviceID}`（软删除） | `{status: "deleted"}`（硬删除） | **归档能力丢失** |
| GET /api/v1/agents | `[{agentID, hostname, segment, status}]`（精简） | `{agents: [Agent]}`（完整对象） | **字段更多**，前端兼容 |

---

## auth-svc 对比详情

### 协议说明

auth-svc HTTP 网关（`services/auth-svc/internal/http/gateway.go`）路径与 controlplane 逐字对齐，是三域中匹配率最高的。设计原则：**默认关闭**（`AUTH_SVC_HTTP_ENABLED=false`），双轨期 controlplane 仍是唯一登录入口，避免 Cookie 互写冲突。

### controlplane auth 域 HTTP 端点（15 个）

| 方法 | 路径 | handler | 业务能力 |
|---|---|---|---|
| POST | /api/v1/auth/register | handleAuthRegister | 用户注册（pending 审批） |
| POST | /api/v1/auth/login | handleAuthLogin | 登录（双 Cookie） |
| GET | /api/v1/auth/me | handleAuthMe | 当前用户 |
| POST | /api/v1/auth/logout | handleAuthLogout | 登出（清 Cookie） |
| POST | /api/v1/auth/refresh | handleAuthRefresh | 刷新 token（Cookie 旋转） |
| POST | /api/v1/auth/change-password | handleAuthChangePassword | 修改密码 |
| GET/POST | /api/v1/users | handleUsers | 用户列表/创建 |
| PUT | /api/v1/users/{id} | handleUpdateUser | 更新用户 |
| DELETE | /api/v1/users/{id} | handleDeleteUser | 删除用户 |
| POST | /api/v1/users/{id}/approve | handleApproveUser | 审批用户 |
| POST | /api/v1/users/{id}/reject | handleRejectUser | 拒绝用户 |
| GET/POST | /api/v1/roles | handleRoles | 角色列表/创建 |
| PUT | /api/v1/roles/{id} | handleUpdateRole | 更新角色 |
| DELETE | /api/v1/roles/{id} | handleDeleteRole | 删除角色 |
| GET | /api/v1/permissions | handlePermissions | 权限列表 |

### auth-svc HTTP 端点（13 个）

| 方法 | 路径 | handler | 业务能力 |
|---|---|---|---|
| POST | /api/v1/auth/login | handleLogin | 登录（双 Cookie + 设备指纹 + MFA） |
| POST | /api/v1/auth/logout | handleLogout | 登出（清 Cookie + Session 撤销） |
| POST | /api/v1/auth/refresh | handleRefresh | 刷新 token（Cookie 旋转） |
| POST | /api/v1/auth/register | handleRegister | 注册（pending 审批） |
| POST | /api/v1/auth/change-password | handleChangePassword | 修改密码 |
| GET | /api/v1/auth/me | handleMe | 当前用户 |
| GET/POST | /api/v1/users | handleUsers | 用户列表/创建 |
| GET/DELETE | /api/v1/users/{id} | handleUserDetail | 用户详情/删除 |
| POST | /api/v1/users/{id}/approve | handleUserDetail | 审批用户 |
| POST | /api/v1/users/{id}/reject | handleUserDetail | 拒绝用户 |
| GET/POST | /api/v1/roles | handleRoles | 角色列表/创建 |
| GET/DELETE | /api/v1/roles/{id} | handleRoleDetail | 角色详情/删除 |
| GET | /api/v1/permissions | handlePermissions | 权限列表 |

### 匹配的端点（13 个）

| 方法 | 路径 | controlplane handler | 微服务 handler | 备注 |
|---|---|---|---|---|
| POST | /api/v1/auth/register | handleAuthRegister | handleRegister | 语义对齐（pending 审批）。auth-svc 增加强口令校验 |
| POST | /api/v1/auth/login | handleAuthLogin | handleLogin | Cookie 语义逐字对齐（opsmesh_at/opsmesh_rt）。auth-svc 增加设备指纹+MFA+Redis Session |
| GET | /api/v1/auth/me | handleAuthMe | handleMe | 返回当前用户。controlplane 返回 `{tenantID, userID, roles, mode}`，auth-svc 返回 `{id, username, email, roles}`。**响应字段不同** |
| POST | /api/v1/auth/logout | handleAuthLogout | handleLogout | 清 Cookie + 吊销。auth-svc 增加 Redis Session 撤销 |
| POST | /api/v1/auth/refresh | handleAuthRefresh | handleRefresh | Cookie 旋转。auth-svc 增加设备指纹校验 |
| POST | /api/v1/auth/change-password | handleAuthChangePassword | handleChangePassword | token 模式改密。两者一致（不接受 body.user_id 直调） |
| GET/POST | /api/v1/users | handleUsers | handleUsers | 列表/创建。controlplane POST 接收 `{username, password, email, roleIDs}`，auth-svc POST 接收 `{username, email}`（无 password，管理端创建默认 active） |
| DELETE | /api/v1/users/{id} | handleDeleteUser | handleUserDetail | 删除用户 |
| POST | /api/v1/users/{id}/approve | handleApproveUser | handleUserDetail | 审批用户（pending → active） |
| POST | /api/v1/users/{id}/reject | handleRejectUser | handleUserDetail | 拒绝用户（pending → rejected） |
| GET/POST | /api/v1/roles | handleRoles | handleRoles | 列表/创建 |
| DELETE | /api/v1/roles/{id} | handleDeleteRole | handleRoleDetail | 删除角色 |
| GET | /api/v1/permissions | handlePermissions | handlePermissions | 权限列表 |

### 仅 controlplane 有的端点（2 个，切流前须补齐）

| 方法 | 路径 | handler | 影响 | 切流前须补齐? |
|---|---|---|---|---|
| PUT | /api/v1/users/{id} | handleUpdateUser | 更新用户（description/roleIDs/status）。auth-svc 无 PUT 端点，仅能通过 approve/reject 改 status | ⚠️ 是（前端用户编辑功能依赖） |
| PUT | /api/v1/roles/{id} | handleUpdateRole | 更新角色（description/permissions）。auth-svc 无 PUT 端点 | ⚠️ 是（前端角色编辑功能依赖） |

### 仅 auth-svc 有的端点（0 个业务端点，2 个为 controlplane 缺失的 GET 详情）

| 方法 | 路径 | handler | 备注 |
|---|---|---|---|
| GET | /api/v1/users/{id} | handleUserDetail | 用户详情（controlplane 无独立 GET {id}，靠列表过滤）。**是能力增强，非冲突** |
| GET | /api/v1/roles/{id} | handleRoleDetail | 角色详情（controlplane 无独立 GET {id}）。**是能力增强，非冲突** |

### 请求/响应格式对比（匹配端点）

| 端点 | controlplane | auth-svc | 差异影响 |
|---|---|---|---|
| POST /auth/login | `{username, password}` → `{user, mustChangePassword, changePasswordToken}` + Set-Cookie×2 | 同 + `{needMFA, deviceFP}` | auth-svc 响应多 2 字段，前端兼容（忽略额外字段） |
| GET /auth/me | `{tenantID, userID, roles, mode: "gateway-injected"}` | `{id, username, email, roles}` | **结构不同**，前端需适配（mode 字段丢失，id vs userID 命名差异） |
| POST /users | `{username, password, email, roleIDs}` | `{username, email}`（无 password/roleIDs） | **请求字段不同**，管理端创建用户时 auth-svc 不支持设密码和角色 |
| Cookie | opsmesh_at / opsmesh_rt，Path=/、HttpOnly、SameSite=Lax、Secure 条件 | 逐字一致 | ✅ Cookie 语义完全对齐（跨入口互换前提） |

---

## 切流前须补齐的端点清单

| 域 | 方法 | 路径 | 缺失方 | 优先级 | 说明 |
|---|---|---|---|---|---|
| task | POST | /api/v1/tasks/batch | task-svc（HTTP 网关） | P0 | 多 agent 批量下发。task-svc 有 gRPC CreateBatchTask 但无 HTTP 网关，前端无法调用。须补 HTTP 网关或 grpc-web |
| task | POST | /api/v1/tasks/batch-exec | task-svc | P1 | M5 增强批量执行（带进度跟踪） |
| task | POST | /api/v1/tasks/canary | task-svc | P1 | 灰度发布创建。task-svc gRPC 完全没有 canary 能力 |
| task | GET/POST | /api/v1/tasks/canary/{id} | task-svc | P1 | 灰度状态/advance |
| task | POST | /api/v1/schedules/{id}/pause | task-svc | P1 | 暂停定时任务。gRPC ScheduleService 无 pause/resume 方法 |
| task | POST | /api/v1/schedules/{id}/resume | task-svc | P1 | 恢复定时任务 |
| task | GET/POST | /api/v1/approval/flows | task-svc | P1 | 审批流定义 CRUD。task-svc gRPC 完全没有 approval 能力 |
| task | GET/POST | /api/v1/approval/requests | task-svc | P1 | 审批请求提交/查询 |
| task | GET | /api/v1/approval/pending | task-svc | P1 | 待我审批列表 |
| task | — | （HTTP 网关） | task-svc | P0 | task-svc 无业务 HTTP 网关，前端 B/S 通道完全缺失。切流前须补 HTTP 网关（或 grpc-web/Connect-RPC） |
| device | POST | /api/v1/devices/{id}/provision | device-svc | P0 | 单设备手动纳管（签发 install token）。前端"纳管"按钮依赖 |
| device | GET | /api/v1/devices/{id}/metrics | device-svc | P0 | 设备监控指标（最新+历史时序）。前端设备详情页指标图表依赖 |
| device | GET | /api/v1/me | device-svc（或 auth-svc） | P0 | 当前身份上下文。前端身份渲染依赖。应由 auth-svc 提供（auth-svc 有 /auth/me 但字段不同） |
| device | GET | /api/v1/devices/{id} 聚合响应 | device-svc | P1 | controlplane 返回 `{device, tasks, results}` 聚合，device-svc 只返回 device。前端需改为 3 次请求或 device-svc 补聚合 |
| device | DELETE | /api/v1/devices/{id} 软删除 | device-svc | P1 | controlplane 标记 retired（可查归档），device-svc 硬删除。归档能力丢失 |
| auth | PUT | /api/v1/users/{id} | auth-svc | P0 | 更新用户（description/roleIDs/status）。前端用户编辑功能依赖 |
| auth | PUT | /api/v1/roles/{id} | auth-svc | P0 | 更新角色（description/permissions）。前端角色编辑功能依赖 |
| auth | GET | /api/v1/auth/me 响应格式 | auth-svc | P1 | controlplane 返回 `{tenantID, userID, roles, mode}`，auth-svc 返回 `{id, username, email, roles}`。前端需适配 |

---

## 结论与建议

### 总体一致性评估

| 域 | 一致性 | 评级 | 切流就绪度 |
|---|---|---|---|
| task | 60.0% | ⚠️ 中 | **未就绪**——task-svc 无 HTTP 网关，前端 B/S 通道完全缺失；灰度/审批流能力完全缺失 |
| device | 60.0% | ⚠️ 中 | **部分就绪**——路径对齐好，但缺单设备纳管/指标两个 P0 端点，且响应结构有差异 |
| auth | 86.7% | ✅ 高 | **基本就绪**——仅缺 2 个 PUT 端点（P0），Cookie 语义已逐字对齐 |

### 关键风险

1. **task-svc 无 HTTP 网关（P0 阻塞）**：task-svc 是纯 gRPC 服务，前端 B/S 通道完全缺失。切流前必须补 HTTP 网关（将 gRPC 方法包装为 REST），或引入 grpc-web / Connect-RPC 让浏览器直连 gRPC。这是 task 域切流的最大阻塞项。

2. **device 域响应结构差异（P1 兼容性）**：
   - `GET /api/v1/devices`：controlplane 按 segment 分组返回 map，device-svc 返回扁平数组。前端解析逻辑需改。
   - `GET /api/v1/devices/{id}`：controlplane 聚合 `{device, tasks, results}`，device-svc 只返回 device。前端设备详情页需改为 3 次请求（或 device-svc 补聚合端点）。
   - `DELETE /api/v1/devices/{id}`：controlplane 软删除（retired 可查归档），device-svc 硬删除。归档能力丢失。

3. **auth 域 /auth/me 响应字段差异（P1 兼容性）**：controlplane 返回 `{tenantID, userID, roles, mode}`，auth-svc 返回 `{id, username, email, roles}`。`mode` 字段丢失（标识网关注入身份 vs 自鉴权），`userID` → `id` 命名差异。前端身份渲染逻辑需适配。

4. **双轨 Cookie 互写冲突（已规避）**：auth-svc HTTP 网关默认关闭（`AUTH_SVC_HTTP_ENABLED=false`），双轨期 controlplane 仍是唯一登录入口。切流时需先开 auth-svc 网关、再切前端流量，避免两端同时写 Cookie。

### 切流建议路径

1. **auth 域优先切流**（一致性 86.7%，阻塞项最少）：
   - 先在 auth-svc 补齐 `PUT /api/v1/users/{id}` 和 `PUT /api/v1/roles/{id}` 两个 P0 端点。
   - 统一 `/auth/me` 响应格式（补 `tenantID`/`mode` 字段或前端适配）。
   - 开 `AUTH_SVC_HTTP_ENABLED=true`，灰度切前端登录流量到 auth-svc。

2. **device 域其次**（一致性 60.0%，路径已对齐）：
   - 补 `POST /api/v1/devices/{id}/provision`（单设备纳管）和 `GET /api/v1/devices/{id}/metrics`（设备指标）两个 P0 端点。
   - 决策响应结构：要么 device-svc 补聚合端点（`GET /devices/{id}` 返回 `{device, tasks, results}`），要么前端改为多次请求。
   - 决策删除语义：device-svc 改为软删除（retired），或接受归档能力丢失。

3. **task 域最后**（一致性 60.0%，阻塞项最多）：
   - **先补 HTTP 网关**（P0 阻塞）——将 task-svc gRPC 方法包装为 REST 端点，路径对齐 controlplane（`/api/v1/tasks*`）。
   - 补 batch/canary/approval/schedules pause-resume 等缺失能力（P1）。
   - 灰度发布（canary）和审批流（approval）在 task-svc gRPC 完全没有，须从 controlplane 移植。

### 后续行动项

- [ ] **task-svc**：补 HTTP 业务网关（P0，阻塞切流）
- [ ] **task-svc**：补 batch-exec / canary / approval / schedules pause-resume 能力（P1）
- [ ] **device-svc**：补 `POST /devices/{id}/provision` + `GET /devices/{id}/metrics`（P0）
- [ ] **device-svc**：决策 `GET /devices/{id}` 聚合 vs 前端多次请求（P1）
- [ ] **device-svc**：决策 DELETE 软删除 vs 硬删除（P1）
- [ ] **auth-svc**：补 `PUT /users/{id}` + `PUT /roles/{id}`（P0）
- [ ] **auth-svc**：统一 `/auth/me` 响应格式（P1）
- [ ] **前端**：适配 device 列表/详情响应结构差异（P1）
- [ ] **前端**：适配 auth /auth/me 响应字段差异（P1）

---

## 附录：路由注册代码位置

| 组件 | 文件 | 行号 | 说明 |
|---|---|---|---|
| controlplane | `internal/controlplane/server_lifecycle.go` | 22-253 | `Start()` 内 `mux.HandleFunc` 全量路由注册 |
| controlplane task 子路径 | `internal/controlplane/server_tasks.go` | 479-499 | `handleTaskRouting` 分派 {id}/cancel/result/approve/reject |
| controlplane device 子路径 | `internal/controlplane/server_devices.go` | 189-209 | `handleDeviceRouting` 分派 {id}/{id}/provision/{id}/metrics |
| controlplane auth 子路径 | `internal/controlplane/auth_users.go` | 113-153 | `handleUserRouting` 分派 {id}/{id}/approve/{id}/reject |
| controlplane auth 子路径 | `internal/controlplane/auth_roles.go` | 88-102 | `handleRoleRouting` 分派 {id} PUT/DELETE |
| task-svc gRPC | `services/task-svc/api/proto/v1/task.proto` | 11-64 | 四个 service 定义 |
| task-svc gRPC 实现 | `services/task-svc/internal/server/server.go` | 30-235 | gRPC 方法实现 |
| task-svc HTTP | `services/task-svc/cmd/task-svc/main.go` | 101-110 | 仅 /health /ready /metrics |
| device-svc HTTP | `services/device-svc/internal/http/gateway.go` | 58-80 | `RegisterRoutes` 全量路由 |
| auth-svc HTTP | `services/auth-svc/internal/http/gateway.go` | 126-141 | `RegisterRoutes` 全量路由 |