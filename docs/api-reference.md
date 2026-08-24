# OpsMesh API 参考文档

本文档完整描述 OpsMesh 控制面 HTTP REST API（端口 8080）与 gRPC API（端口 9090），覆盖 `internal/controlplane/` 下所有 `server_*.go` 注册的 HTTP 路由（含 helm、approval、schedules、secrets、quotas、os-templates、middleware-templates、k8s/clusters、k8s/resources、network、notify、alert-rules-engine 等模块）。

- **Base URL**：`http://<controlplane-host>:8080`
- **Metrics 端点**：`http://<controlplane-host>:9091/metrics`（独立端口，非 8080）
- **API 版本**：`v1`（路径前缀 `/api/v1`）
- **内容类型**：`application/json`（除 SSE 流为 `text/event-stream`、二进制下载外）
- **请求体上限**：1 MiB（`http.MaxBytesReader`，超出返回 413）
- **租户隔离**：所有业务接口通过 `X-Tenant-ID` 头做行级隔离；`--require-auth=true` 时缺失该头返回 401
- **认证**：`Authorization: Bearer <jwt>` 或 HttpOnly Cookie `at`（access token）；管理类接口需对应角色
- **权限模型**：写操作普遍要求 `<module>:write`，读操作要求 `<module>:read`，审批类要求 `<module>:approve`；admin 角色拥有 `*` 通配权限

## 目录

- [通用约定](#通用约定)
- [基础端点](#基础端点)
- [认证 API](#认证-api)
- [用户管理 API](#用户管理-api)
- [角色与权限 API](#角色与权限-api)
- [设备 API](#设备-api)
- [Agent API](#agent-api)
- [任务 API](#任务-api)
- [批量运维与灰度发布 API](#批量运维与灰度发布-api)
- [定时任务管理 API](#定时任务管理-api)
- [告警 API](#告警-api)
- [告警规则引擎 API](#告警规则引擎-api)
- [告警静默 API](#告警静默-api)
- [通知渠道 API](#通知渠道-api)
- [通知模板 API](#通知模板-api)
- [部署 API](#部署-api)
- [Helm 应用商店 API](#helm-应用商店-api)
- [作业编排 API](#作业编排-api)
- [审批 API](#审批-api)
- [CMDB API](#cmdb-api)
- [CMDB 采集与变更审批 API](#cmdb-采集与变更审批-api)
- [OS 优化 API](#os-优化-api)
- [中间件部署 API](#中间件部署-api)
- [K8s 管理 API](#k8s-管理-api)
- [网络拓扑与诊断 API](#网络拓扑与诊断-api)
- [密钥管理 API](#密钥管理-api)
- [租户配额 API](#租户配额-api)
- [审计 API](#审计-api)
- [日志 API](#日志-api)
- [联邦 API](#联邦-api)
- [SSE 事件流 API](#sse-事件流-api)
- [纳管 Bootstrap 端点](#纳管-bootstrap-端点)
- [gRPC API](#grpc-api)

---

## 通用约定

### 请求头

| 头 | 必填 | 说明 |
|----|------|------|
| `X-Tenant-ID` | 生产模式必填 | 租户隔离键，行级过滤 |
| `X-User-Id` | 否 | 操作人，写入审计 |
| `X-User-Roles` | 否 | 用户角色（逗号分隔），供 RBAC |
| `Authorization` | 登录后接口必填 | `Bearer <jwt>`，与 Cookie `at` 二选一 |
| `Content-Type` | POST/PUT 必填 | `application/json` |

### 错误响应

所有错误统一 JSON 格式：

```json
{
  "error": "错误描述",
  "trace_id": "可选链路追踪 ID"
}
```

常见状态码：`200` 成功、`201` 创建成功、`400` 参数错误、`401` 未认证、`403` 越权、`404` 不存在、`409` 冲突、`413` 请求体过大、`429` 限流、`500` 内部错误。

### 分页

列表类接口支持 `page` / `pageSize` 查询参数（部分接口用 `limit` / `offset`），响应统一包装：

```json
{
  "items": [],
  "page": 1,
  "pageSize": 20,
  "total": 100
}
```

---

## 基础端点

### GET /healthz

深度健康检查（K8s liveness 探针，增强）。含 Store 连接深度检查，2 秒超时保护。

- **响应**：
  - 正常：`200 OK`，`Content-Type: application/json`

```json
{
  "status": "ok",
  "checks": {"store": "ok"}
}
```

  - Store 不可用：`503 Service Unavailable`

```json
{
  "status": "unhealthy",
  "error": "store unavailable"
}
```

### GET /readyz

就绪检查（K8s readiness 探针，新增）。与 liveness 的区别：失败时从 Service endpoints 摘除但不重启容器。
就绪条件：Store 连接可用 + 本实例持有 leader 租约（避免非 leader 副本接写流量造成脑裂/抖动）。2 秒超时保护。

- **响应**：
  - 就绪：`200 OK`，`{"status": "ready"}`
  - 未就绪：`503 Service Unavailable`，`{"status": "not_ready", "reason": "..."}`

### GET /metrics

Prometheus 文本格式指标。**监听在独立端口 9091**（非主 8080 端口），受 `--metrics-allow-cidr` 白名单控制（白名单非空时仅允许授权来源，否则 403）。

- **请求**：`GET http://<controlplane-host>:9091/metrics`
- **响应**：`200 OK`，`Content-Type: text/plain; version=0.0.4`

### GET /api/v1/me

返回当前请求身份信息（解析 `X-Tenant-ID` / `X-User-Id` / `X-User-Roles` 头；内核不自鉴权，身份由前置网关注入）。

- **响应示例**：

```json
{
  "tenantID": "t1",
  "userID": "u-001",
  "roles": ["admin", "ops"],
  "mode": "gateway-injected"
}
```

---

## 认证 API

### POST /api/v1/auth/register

注册新用户。受 `--public-register` 开关控制（false 时返回 403）。

- **请求体**：

```json
{
  "username": "alice",
  "password": "strong-password",
  "email": "alice@example.com",
  "display_name": "Alice"
}
```

- **响应**：`201 Created`

```json
{
  "user_id": "u-002",
  "username": "alice",
  "status": "pending",
  "message": "注册成功，待管理员审批"
}
```

> `--allow-public-register=true` 时 `status` 为 `active` 并立即签发 token；否则为 `pending` 须管理员激活。

### POST /api/v1/auth/login

登录，签发 JWT（HS256）。受登录限流保护（每 IP 10 突发 / 每 3s 补 1，连续失败 5 次锁 15min）。

- **请求体**：

```json
{
  "username": "admin",
  "password": "password",
  "device_fp": "可选设备指纹"
}
```

- **响应**：`200 OK`，同时设置 HttpOnly Cookie `at`（access token）与 `rt`（refresh token）

```json
{
  "access_token": "eyJ...",
  "refresh_token": "eyJ...",
  "expires_in": 3600,
  "user": {
    "id": "u-001",
    "username": "admin",
    "roles": ["admin"],
    "tenant_id": "t1"
  }
}
```

### GET /api/v1/auth/me

返回当前登录用户信息（基于 Bearer token 或 Cookie）。

- **响应**：`200 OK`

```json
{
  "id": "u-001",
  "username": "admin",
  "roles": ["admin"],
  "tenant_id": "t1",
  "status": "active"
}
```

### POST /api/v1/auth/logout

登出，清除 HttpOnly Cookie（`at` / `rt`）并撤销 refresh token。

- **请求体**：无
- **响应**：`200 OK`，`{"message": "已登出"}`

### POST /api/v1/auth/refresh

用 refresh token 静默换发新的 access + refresh token（旋转）。

- **认证**：依赖 Cookie `rt` 或请求体

```json
{
  "refresh_token": "eyJ..."
}
```

- **响应**：`200 OK`，同登录响应格式，刷新 Cookie

### POST /api/v1/auth/change-password

修改当前用户密码（安全债：预置弱口令强制改密）。

- **请求体**：

```json
{
  "old_password": "old",
  "new_password": "new-strong-password"
}
```

- **响应**：`200 OK`，`{"message": "密码已更新"}`

---

## 用户管理 API

### GET /api/v1/users

用户列表（管理员）。

- **查询参数**：`status`（active/pending/locked）、`page`、`pageSize`
- **响应**：`200 OK`

```json
{
  "items": [
    {"id": "u-001", "username": "admin", "status": "active", "roles": ["admin"], "tenant_id": "t1"}
  ],
  "total": 1
}
```

### POST /api/v1/users

创建用户（管理员，`--public-register=false` 时的唯一用户创建途径）。

- **请求体**：同注册接口
- **响应**：`201 Created`

### GET /api/v1/users/{id}

用户详情。

### PUT /api/v1/users/{id}

更新用户（角色绑定、状态激活/锁定等）。

- **请求体**：

```json
{
  "roles": ["ops"],
  "status": "active"
}
```

### DELETE /api/v1/users/{id}

删除用户。

---

## 角色与权限 API

### GET /api/v1/roles

角色列表。

- **响应**：

```json
[
  {"id": "r-001", "name": "admin", "permissions": ["*"]},
  {"id": "r-002", "name": "ops", "permissions": ["task:create", "device:read"]}
]
```

### POST /api/v1/roles

创建角色。

- **请求体**：

```json
{
  "name": "viewer",
  "permissions": ["device:read", "task:read"]
}
```

### GET /api/v1/roles/{id}

角色详情。

### PUT /api/v1/roles/{id}

更新角色（修改权限绑定）。

### DELETE /api/v1/roles/{id}

删除角色。

### GET /api/v1/permissions

权限列表（预置 24 条默认权限）。

- **响应**：

```json
[
  {"code": "device:read", "name": "查看设备"},
  {"code": "task:create", "name": "下发任务"}
]
```

---

## 设备 API

### GET /api/v1/devices

设备清单（按网段分组）。

- **查询参数**：`segment`、`status`（online/offline/retired）、`managed`
- **响应**：

```json
[
  {
    "id": "d-001",
    "hostname": "host-1",
    "segment": "seg-a",
    "agent_id": "a-001",
    "state": "online",
    "managed": true,
    "last_heartbeat": "2026-08-07T10:00:00Z",
    "os": "linux",
    "arch": "amd64"
  }
]
```

### GET /api/v1/devices/{id}

设备详情（含最近任务结果）。

- **响应**：同上单个对象，附加 `recent_tasks` 字段

### DELETE /api/v1/devices/{id}

退役/下线设备（`state` → `retired`）。

- **响应**：`200 OK`，`{"message": "设备已退役"}`

### POST /api/v1/devices/{id}/provision

**纳管**：签发一次性 install token（15 分钟有效），返回 bootstrap 安装命令。

- **响应**：

```json
{
  "install_token": "tok-xxxx",
  "expires_at": "2026-08-07T10:15:00Z",
  "bootstrap": "curl -fsSL http://controlplane:8080/install.sh | sh -s -- --token=tok-xxxx --control-addr=http://controlplane:8080"
}
```

### POST /api/v1/provision/auto

自动纳管：按网段批量签发 install token。

- **请求体**：

```json
{
  "segment_cidr": "10.30.0.0/24",
  "ssh_user": "root"
}
```

- **响应**：

```json
{
  "candidates": [
    {"host": "10.30.0.5", "install_token": "tok-aaa", "bootstrap": "curl ... | sh -s -- --token=tok-aaa"}
  ],
  "total": 1
}
```

### GET /api/v1/devices/{id}/metrics

设备监控指标（agent 采集 + 控制面环形缓冲）。由 agent 端 `internal/agent/metrics_collect.go` 每 30s 采集，经心跳上报到控制面，控制面 `store.metricsRing` 保留最近 2h 历史快照。

- **认证**：需 `device:read` 权限
- **租户隔离**：`require-auth` 时仅返回本租户设备指标（经 Device 归属校验）
- **查询参数**：
  - 无 `range`：返回最新值（`proto.DeviceMetrics`，向后兼容）
  - `range=2h`：返回历史时序数据（`proto.MetricsSeries`），支持 `15m` / `1h` / `2h` / `6h` / `24h`
- **响应（最新值）**：`200 OK`

```json
{
  "cpu_percent": 12.5,
  "mem_percent": 45.2,
  "mem_used_mb": 1843,
  "mem_total_mb": 4096,
  "disk_percent": 60.1,
  "load_1": 0.34,
  "load_5": 0.42,
  "load_15": 0.38,
  "net_in_kb": 1234,
  "net_out_kb": 567,
  "collected_at": "2026-08-24T10:00:00Z"
}
```

- **响应（历史时序）**：`200 OK`

```json
{
  "device_id": "d-001",
  "range": "2h",
  "points": [
    {"timestamp": "2026-08-24T08:00:00Z", "cpu_percent": 10.2, "mem_percent": 44.1},
    {"timestamp": "2026-08-24T08:30:00Z", "cpu_percent": 15.8, "mem_percent": 45.0}
  ],
  "total": 240
}
```

- **无数据**：`404 Not Found`（agent 未上报过指标，可能是刚注册尚未到首个 30s 采集周期）
- **说明**：更长历史请查 Prometheus（控制面 `/metrics` 端点暴露 `opsmesh_device_cpu_percent` 等指标）

---

## Agent API

### GET /api/v1/agents

agent 清单。

- **响应**：

```json
[
  {
    "id": "a-001",
    "hostname": "host-1",
    "segment": "seg-a",
    "state": "online",
    "last_heartbeat": "2026-08-07T10:00:00Z",
    "version": "0.1.0",
    "load": {"cpu": 0.3, "mem_mb": 256}
  }
]
```

---

## 任务 API

### GET /api/v1/tasks

任务列表。

- **查询参数**：`status`（pending/running/done/failed/cancelled）、`agent_id`、`page`、`pageSize`
- **响应**：

```json
[
  {
    "id": "t-001",
    "agent_id": "a-001",
    "type": "shell",
    "command": "uname -a",
    "status": "done",
    "created_at": "2026-08-07T09:00:00Z",
    "started_at": "2026-08-07T09:00:01Z",
    "finished_at": "2026-08-07T09:00:02Z",
    "retry_count": 0,
    "max_retries": 3,
    "schedule": ""
  }
]
```

### POST /api/v1/tasks

下发单条任务（租户隔离 + 审计）。

- **请求体**：

```json
{
  "agent_id": "a-001",
  "type": "shell",
  "command": "uname -a",
  "timeout_sec": 120,
  "max_retries": 3,
  "schedule": "*/5 * * * *"
}
```

- `type`：`shell` | `service` | `file`
- `schedule`：5 字段 cron 表达式（定时/周期任务，留空为一次性）
- **响应**：`201 Created`，返回完整 Task 对象

### POST /api/v1/tasks/batch

批量下发（逐台查找 agent + 租户校验）。

- **请求体**：

```json
{
  "agent_ids": ["a-001", "a-002"],
  "type": "shell",
  "command": "uptime"
}
```

- **响应**：`201 Created`

```json
{
  "created": [{"id": "t-001", "agent_id": "a-001"}, {"id": "t-002", "agent_id": "a-002"}],
  "total": 2
}
```

### POST /api/v1/tasks/{id}/cancel

取消任务（pending 拦截 / running 强杀）。

- **响应**：`200 OK`，`{"message": "任务已取消"}`

### GET /api/v1/tasks/{id}/result

查询单条任务执行结果。

- **响应**：

```json
{
  "task_id": "t-001",
  "status": "done",
  "exit_code": 0,
  "stdout": "Linux host-1 5.15.0 ...",
  "stderr": "",
  "started_at": "2026-08-07T09:00:01Z",
  "finished_at": "2026-08-07T09:00:02Z"
}
```

---

## 批量运维与灰度发布 API

M5 增强：批量执行（多设备 + 同一任务，返回 batchID + 每设备任务详情）与灰度发布（按比例/分组/标签分阶段执行）。状态仅内存索引（重启后丢失，可通过 batchID 查询活跃批次），任务实例本身持久化在 store 中。

### POST /api/v1/tasks/batch-exec

批量执行同一任务到多台设备。每台设备经 `lookupAgent` 解析 + 租户校验，逐台创建任务并审计。

- **认证**：需 `task:write` 权限
- **请求体**：

```json
{
  "deviceIDs": ["d-001", "d-002"],
  "taskType": "shell",
  "command": "uptime",
  "content": "",
  "path": "",
  "timeout": 120
}
```

- `taskType`：`shell` | `service` | `file`（默认 `shell`）
- `command`：必填；`taskType=shell` 时经 `validateCommand` 校验
- **响应**：`201 Created`

```json
{
  "batchID": "batch-3f2a1b8c9d0e",
  "tasks": [
    {"deviceID": "d-001", "taskID": "t-001", "status": "pending"},
    {"deviceID": "d-002", "status": "failed", "error": "agent not found or tenant mismatch"}
  ]
}
```

### GET /api/v1/tasks/batch/{id}

查询批量任务状态（实时刷新每个子任务状态）。

- **认证**：需 `task:read` 权限
- **响应**：`200 OK`

```json
{
  "batchID": "batch-3f2a1b8c9d0e",
  "taskType": "shell",
  "command": "uptime",
  "createdAt": "2026-08-17T09:00:00Z",
  "createdBy": "u-001",
  "tasks": [
    {"deviceID": "d-001", "taskID": "t-001", "status": "done"}
  ]
}
```

- 不存在：`404`；租户不匹配：`403`

### POST /api/v1/tasks/canary

创建灰度发布。按策略划分阶段，立即执行第一阶段，其余阶段标记 pending（手动 advance 或后续自动推进）。

- **认证**：需 `task:write` 权限
- **请求体**：

```json
{
  "deviceIDs": ["d-001", "d-002", "d-003", "d-004"],
  "taskType": "shell",
  "command": "deploy.sh",
  "strategy": "percentage",
  "percentage": 25
}
```

- `strategy`：`percentage`（按比例分两阶段）/ `group`（按分组多阶段）/ `label`（按标签单阶段）
- `percentage`：1-100，strategy=percentage 时有效，默认 10
- `groups`：string[]，strategy=group 时有效，按组数等分 deviceIDs
- `labels`：map<string,string>，strategy=label 时有效（实际筛选由调用方在 deviceIDs 中完成）
- **响应**：`201 Created`

```json
{
  "canaryID": "canary-1a2b3c4d5e6f",
  "phases": [
    {"phase": 1, "deviceIDs": ["d-001"], "status": "running"},
    {"phase": 2, "deviceIDs": ["d-002", "d-003", "d-004"], "status": "pending"}
  ]
}
```

### GET /api/v1/tasks/canary/{id}

查询灰度发布状态（实时刷新各阶段任务状态）。

- **认证**：需 `task:read` 权限
- **响应**：`200 OK`

```json
{
  "canaryID": "canary-1a2b3c4d5e6f",
  "strategy": "percentage",
  "taskType": "shell",
  "command": "deploy.sh",
  "createdAt": "2026-08-17T09:00:00Z",
  "createdBy": "u-001",
  "phases": [
    {
      "phase": 1,
      "deviceIDs": ["d-001"],
      "status": "done",
      "tasks": [{"deviceID": "d-001", "taskID": "t-001", "status": "done"}],
      "startedAt": "2026-08-17T09:00:00Z",
      "finishedAt": "2026-08-17T09:00:05Z"
    }
  ]
}
```

### POST /api/v1/tasks/canary/{id}/advance

推进灰度到下一 pending 阶段。

- **认证**：需 `task:write` 权限
- **请求体**：无
- **响应**：`200 OK`，`{"canaryID": "canary-...", "phase": 2, "status": "running"}`
- 无 pending 阶段：`400`，`{"error": "no pending phase to advance"}`

---

## 定时任务管理 API

M5 定时任务管理：基于 `internal/cron.Manager`，对已有任务附加 cron 表达式实现周期触发。Server 持有 `*cron.Manager` 实例。

### GET /api/v1/schedules

定时任务列表（按租户过滤）。

- **认证**：需 `schedule:read` 权限
- **查询参数**：`status`（active/paused/error）
- **响应**：`200 OK`

```json
{
  "schedules": [
    {
      "id": "sch-001",
      "taskID": "t-template-001",
      "name": "每5分钟健康检查",
      "cronExpr": "*/5 * * * *",
      "status": "active",
      "tenantID": "t1",
      "createdBy": "u-001"
    }
  ],
  "total": 1
}
```

### POST /api/v1/schedules

创建定时任务（绑定到已有模板任务，模板任务须设置 Schedule 字段）。

- **认证**：需 `schedule:write` 权限
- **请求体**：

```json
{
  "taskID": "t-template-001",
  "name": "每5分钟健康检查",
  "cronExpr": "*/5 * * * *"
}
```

- `taskID`：必填，指向已存在的模板任务
- `cronExpr`：5 字段标准 cron 表达式
- **响应**：`201 Created`，返回完整 `ScheduleEntry`

### GET /api/v1/schedules/{id}

定时任务详情。

- **认证**：需 `schedule:read` 权限
- **响应**：`200 OK`，返回 `ScheduleEntry`
- 不存在：`404`；租户不匹配：`403`

### PUT /api/v1/schedules/{id}

更新定时任务（名称、cron 表达式、状态）。

- **认证**：需 `schedule:write` 权限
- **请求体**：

```json
{
  "name": "每10分钟健康检查",
  "cronExpr": "*/10 * * * *",
  "status": "active"
}
```

- **响应**：`200 OK`，返回更新后的 `ScheduleEntry`

### DELETE /api/v1/schedules/{id}

删除定时任务。

- **认证**：需 `schedule:write` 权限
- **响应**：`200 OK`，`{"status": "deleted", "id": "sch-001"}`

### POST /api/v1/schedules/{id}/pause

暂停定时任务。

- **认证**：需 `schedule:write` 权限
- **请求体**：无
- **响应**：`200 OK`，返回状态为 `paused` 的 `ScheduleEntry`
- 同时发布 SSE 事件 `schedule_status`，data: `{"scheduleID":"sch-001","status":"paused"}`

### POST /api/v1/schedules/{id}/resume

恢复定时任务。

- **认证**：需 `schedule:write` 权限
- **请求体**：无
- **响应**：`200 OK`，返回状态为 `active` 的 `ScheduleEntry`
- 同时发布 SSE 事件 `schedule_status`，data: `{"scheduleID":"sch-001","status":"active"}`

---

## 告警 API

### GET /api/v1/alerts

活跃告警列表（M7）。

- **查询参数**：`severity`（critical/warning/info）、`status`（active/acked/silenced）
- **响应**：

```json
[
  {
    "id": "al-001",
    "severity": "critical",
    "title": "任务死信",
    "message": "任务 t-001 重试耗尽进入死信",
    "status": "active",
    "created_at": "2026-08-07T09:00:00Z",
    "source": "task"
  }
]
```

### POST /api/v1/alerts/{id}/ack

确认告警。

- **请求体**：`{"note": "已确认，处理中"}`
- **响应**：`200 OK`

### POST /api/v1/alerts/{id}/silence

静默告警（指定时长内不再通知）。

- **请求体**：`{"duration_min": 60}`
- **响应**：`200 OK`

### GET /api/v1/alert-rules

告警规则列表（修复 9）。

### POST /api/v1/alert-rules

创建告警规则。

- **请求体**：

```json
{
  "name": "agent 离线告警",
  "metric": "agent_offline",
  "condition": ">",
  "threshold": 0,
  "severity": "critical",
  "duration_sec": 300
}
```

### DELETE /api/v1/alert-rules/{id}

删除告警规则。

---

## 告警规则引擎 API

M2 多条件告警规则引擎：基于 `alertengine.Engine`，支持多条件 + 逻辑组合（AND/OR/NOT）+ 持续时长 + 通知渠道选择 + 静默关联。走独立路由 `/api/v1/alert-rules-engine` 以与旧版单条件 `/api/v1/alert-rules` 向后兼容。

### GET /api/v1/alert-rules-engine

列出当前租户的多条件告警规则。

- **认证**：需 `alert:read` 权限
- **响应**：`200 OK`，返回 `AlertRule[]`

```json
[
  {
    "id": "ar-eng-3f2a1b8c",
    "tenantID": "t1",
    "name": "CPU 与内存双高",
    "conditions": [
      {"metric": "cpu_usage", "op": ">", "threshold": 0.9},
      {"metric": "mem_usage", "op": ">", "threshold": 0.85}
    ],
    "logic": "AND",
    "duration": 300000000000,
    "severity": "critical",
    "notifyChannels": ["ch-001"],
    "silenceID": "",
    "labels": {"team": "sre"}
  }
]
```

### POST /api/v1/alert-rules-engine

创建多条件告警规则。

- **认证**：需 `alert:write` 权限
- **请求体**：同上 `AlertRule`（`id` 未提供时自动生成 `ar-eng-<8hex>`，`tenantID` 由请求上下文注入）
- **响应**：`201 Created`，返回完整 `AlertRule`

### GET /api/v1/alert-rules-engine/{id}

获取单条规则详情。

- **认证**：需 `alert:read` 权限
- **响应**：`200 OK`，返回 `AlertRule`
- 不存在或跨租户：`404`

### PUT /api/v1/alert-rules-engine/{id}

更新规则。

- **认证**：需 `alert:write` 权限
- **请求体**：完整 `AlertRule`（`id` 取路径参数，`tenantID` 由请求上下文注入）
- **响应**：`200 OK`，返回更新后的 `AlertRule`

### DELETE /api/v1/alert-rules-engine/{id}

删除规则。

- **认证**：需 `alert:write` 权限
- **响应**：`200 OK`，`{"status": "deleted", "id": "ar-eng-3f2a1b8c"}`
- 不存在或跨租户：`404`

---

## 告警静默 API

M2 静默规则：基于标签匹配 + 时间窗口抑制。`store.SilenceRule` 持久化，`alertengine.Silencer` 内存索引同步注入使评估循环立即生效。

### GET /api/v1/alert-silences

列出当前租户的静默规则。

- **认证**：需 `alert:read` 权限
- **响应**：`200 OK`，返回 `SilenceRule[]`

```json
[
  {
    "id": "sil-001",
    "tenantID": "t1",
    "matchLabels": {"severity": "warning"},
    "startAt": "2026-08-17T09:00:00Z",
    "endAt": "2026-08-17T10:00:00Z",
    "createdBy": "u-001",
    "reason": "维护窗口静默"
  }
]
```

### POST /api/v1/alert-silences

创建静默规则（同步注入 `alertengine.Silencer`）。

- **认证**：需 `alert:write` 权限
- **请求体**：同上 `SilenceRule`（`tenantID`、`createdBy` 由请求上下文注入）
- **响应**：`201 Created`，返回完整 `SilenceRule`

### DELETE /api/v1/alert-silences/{id}

删除静默规则（同步从 `alertengine.Silencer` 移除）。

- **认证**：需 `alert:write` 权限
- **响应**：`200 OK`，`{"status": "deleted", "id": "sil-001"}`
- 不存在或跨租户：`404`

---

## 通知渠道 API

M2 通知渠道：支持 webhook / 钉钉 / 飞书 / 企微 / SMTP / Slack 等。`Config` 字段在列表/创建返回时脱敏（`secret/password/token/pass/apiKey/api_key` 替换为 `***`）。Webhook URL 经 SSRF 校验（`validateNotifyChannelWebhook`，受 `--webhook-allow-private` 控制）。

### GET /api/v1/notify-channels

通知渠道列表（Config 脱敏）。

- **认证**：需 `alert:read` 权限
- **响应**：`200 OK`，返回 `NotifyChannel[]`

```json
[
  {
    "id": "ch-001",
    "tenantID": "t1",
    "name": "SRE 钉钉群",
    "type": "dingtalk",
    "enabled": true,
    "config": "{\"token\":\"***\"}"
  }
]
```

### POST /api/v1/notify-channels

创建通知渠道。

- **认证**：需 `alert:write` 权限
- **请求体**：`NotifyChannel`（`tenantID` 由请求上下文注入）
- **响应**：`201 Created`，返回脱敏后的 `NotifyChannel`
- Webhook SSRF 校验失败：`400`，`{"error": "webhook URL SSRF validation failed: ..."}`

### PUT /api/v1/notify-channels/{id}

更新渠道。

- **认证**：需 `alert:write` 权限
- **请求体**：完整 `NotifyChannel`
- **响应**：`200 OK`，返回更新后的 `NotifyChannel`
- 不存在：`404`

### DELETE /api/v1/notify-channels/{id}

删除渠道。

- **认证**：需 `alert:write` 权限
- **响应**：`200 OK`，`{"status": "deleted", "id": "ch-001"}`
- 不存在或跨租户：`404`

### POST /api/v1/notify-channels/{id}/test

测试发送一条通知到指定渠道（不进入聚合 / 静默 / 抑制链路）。

- **认证**：需 `alert:read` 权限
- **请求体**（可选）：

```json
{
  "title": "测试通知",
  "body": "这是一条测试通知"
}
```

- 缺省使用内置测试消息：`title="OpsMesh 测试通知"`，`body="来自渠道 <name>（类型 <type>）的测试通知，发送时间 <now>"`
- **响应**：`200 OK`

```json
{"status": "ok", "message": "test notification sent"}
```

- 发送失败：`200 OK`，`{"status": "fail", "error": "..."}`
- 渠道不存在或跨租户：`404`；渠道配置错误：`400`

---

## 通知模板 API

M2 通知模板：按事件类型 / 严重度定制通知正文（Markdown / 纯文本 / HTML）。

### GET /api/v1/notify-templates

通知模板列表。

- **认证**：需 `alert:read` 权限
- **响应**：`200 OK`，返回 `NotifyTemplate[]`

```json
[
  {
    "id": "tpl-001",
    "tenantID": "t1",
    "name": "critical 告警模板",
    "type": "alert",
    "format": "markdown",
    "titleTmpl": "[CRITICAL] {{.DeviceID}}",
    "bodyTmpl": "设备 {{.DeviceID}} 触发 {{.Metric}} = {{.Value}}"
  }
]
```

### POST /api/v1/notify-templates

创建通知模板。

- **认证**：需 `alert:write` 权限
- **请求体**：同上 `NotifyTemplate`（`tenantID` 由请求上下文注入）
- **响应**：`201 Created`，返回完整 `NotifyTemplate`

### PUT /api/v1/notify-templates/{id}

更新模板。

- **认证**：需 `alert:write` 权限
- **请求体**：完整 `NotifyTemplate`
- **响应**：`200 OK`，返回更新后的 `NotifyTemplate`
- 不存在：`404`

### DELETE /api/v1/notify-templates/{id}

删除模板。

- **认证**：需 `alert:write` 权限
- **响应**：`200 OK`，`{"status": "deleted", "id": "tpl-001"}`
- 不存在或跨租户：`404`

---

## 部署 API

M3 部署中心：计划 + fan-out 执行 + Reconcile + Rollback。

### GET /api/v1/deploys

部署列表。

- **查询参数**：`status`（planning/running/success/failed/rolled_back）、`page`、`pageSize`
- **响应**：

```json
[
  {
    "id": "dp-001",
    "name": "nginx 滚动升级",
    "status": "success",
    "target_agents": ["a-001", "a-002"],
    "created_by": "u-001",
    "created_at": "2026-08-07T09:00:00Z"
  }
]
```

### POST /api/v1/deploys

创建部署计划。

- **请求体**：

```json
{
  "name": "nginx 滚动升级",
  "target_agents": ["a-001", "a-002"],
  "steps": [
    {"name": "拉取镜像", "type": "shell", "command": "docker pull nginx:1.25"},
    {"name": "重启服务", "type": "service", "action": "restart", "unit": "nginx"}
  ],
  "strategy": "rolling",
  "rollback_on_failure": true
}
```

- **响应**：`201 Created`，返回完整 DeployTask

### GET /api/v1/deploys/{id}

部署详情（含各 step 执行状态）。

### POST /api/v1/deploys/{id}/rollback

回滚部署。

- **响应**：`200 OK`，`{"message": "回滚已触发", "rollback_id": "dp-001-rb"}`

### GET /api/v1/deploys/federation

列出联邦发布计划（多集群联邦发布，复用 deployMux）。

- **认证**：需 `deploy:read` 权限
- **查询参数**：`status`（planning/running/success/failed/rolled_back）
- **响应**：`200 OK`，返回 `FederationDeploy[]`
- 联邦未启用：`501 Not Implemented`，`{"error": "federation not enabled"}`

### POST /api/v1/deploys/federation

创建联邦发布计划（跨多集群成员的统一部署）。

- **认证**：需 `deploy:write` 权限
- **请求体**：`FederationDeploy`（`tenantID`、`createdBy` 由请求上下文注入）

```json
{
  "name": "nginx 跨集群发布",
  "members": [
    {"clusterID": "k8s-001", "weight": 50},
    {"clusterID": "k8s-002", "weight": 50}
  ],
  "deployID": "dp-001",
  "strategy": "canary"
}
```

- **响应**：`201 Created`，返回完整 `FederationDeploy`

### GET /api/v1/deploys/federation/{id}

查询联邦发布详情。

- **认证**：需 `deploy:read` 权限
- **响应**：`200 OK`，返回 `FederationDeploy`
- 不存在：`404`

### POST /api/v1/deploys/federation/{id}/execute

启动联邦发布（派发首批成员）。

- **认证**：需 `deploy:write` 权限
- **请求体**：无
- **响应**：`200 OK`，返回更新后的 `FederationDeploy`

### POST /api/v1/deploys/federation/{id}/promote

推进到下一批成员（按 weight 分批）。

- **认证**：需 `deploy:write` 权限
- **请求体**：无
- **响应**：`200 OK`，返回更新后的 `FederationDeploy`

### POST /api/v1/deploys/federation/{id}/rollback

回滚全部已派发成员。

- **认证**：需 `deploy:write` 权限
- **请求体**：无
- **响应**：`200 OK`，返回更新后的 `FederationDeploy`

### GET /api/v1/deploys/federation/{id}/status

联邦级状态聚合视图（各成员集群执行状态汇总）。

- **认证**：需 `deploy:read` 权限
- **响应**：`200 OK`

```json
{
  "id": 1,
  "overall": "running",
  "members": [
    {"clusterID": "k8s-001", "status": "success", "progress": 100},
    {"clusterID": "k8s-002", "status": "running", "progress": 50}
  ]
}
```

- 不存在：`404`，`{"error": "federation not found"}`

---

## Helm 应用商店 API

M3 集成：Helm 仓库 / Chart / Release / 预置目录管理。后端依赖 `internal/helm` 包（`RepoManager` / `ReleaseManager` / `DefaultCatalog`），HTTP 层仅做参数解析与错误转换。helm CLI 不存在时各端点返回 `503`，不阻断控制面启动。

### GET /api/v1/helm/repos

列出所有已注册的 Helm 仓库。

- **认证**：需 `helm:read` 权限
- **响应**：`200 OK`

```json
{
  "repos": [
    {"name": "bitnami", "url": "https://charts.bitnami.com/bitnami", "type": "https"}
  ]
}
```

- helm 未初始化：`503`，`{"error": "helm not initialized"}`

### POST /api/v1/helm/repos

添加 Helm 仓库。

- **认证**：需 `helm:write` 权限
- **请求体**：

```json
{
  "name": "bitnami",
  "url": "https://charts.bitnami.com/bitnami",
  "type": "https"
}
```

- `name`、`url`：必填
- `type`：仓库类型（`https` / `oci` / `local`，可空）
- **响应**：`201 Created`，返回 `ChartRepo`

### DELETE /api/v1/helm/repos/{name}

删除 Helm 仓库。

- **认证**：需 `helm:write` 权限
- **响应**：`200 OK`，`{"status": "deleted", "name": "bitnami"}`
- 仓库不存在：`404`；helm CLI 不存在：`503`

### GET /api/v1/helm/repos/{name}/charts

列出指定仓库的所有 chart。

- **认证**：需 `helm:read` 权限
- **响应**：`200 OK`，`{"charts": [...]}`

### GET /api/v1/helm/charts/search

跨仓库搜索 chart。

- **认证**：需 `helm:read` 权限
- **查询参数**：`q`（必填，关键词）
- **响应**：`200 OK`，`{"charts": [...], "query": "nginx"}`
- 缺 `q`：`400`，`{"error": "q parameter required"}`

### GET /api/v1/helm/releases

列出 release。

- **认证**：需 `helm:read` 权限
- **查询参数**：`namespace`（空则列所有 namespace）
- **响应**：`200 OK`，`{"releases": [...]}`

### POST /api/v1/helm/releases

安装 release。

- **认证**：需 `helm:write` 权限
- **请求体**：

```json
{
  "namespace": "default",
  "name": "my-nginx",
  "chart": "bitnami/nginx",
  "values": {"service": {"type": "LoadBalancer"}}
}
```

- `namespace`、`name`、`chart`：必填
- **响应**：`201 Created`，返回 `Release`

### PUT /api/v1/helm/releases/{name}

升级 release。

- **认证**：需 `helm:write` 权限
- **请求体**：

```json
{
  "namespace": "default",
  "chart": "bitnami/nginx",
  "values": {"service": {"type": "ClusterIP"}}
}
```

- `namespace`、`chart`：必填
- **响应**：`200 OK`，返回更新后的 `Release`

### DELETE /api/v1/helm/releases/{name}

卸载 release。

- **认证**：需 `helm:write` 权限
- **查询参数**：`namespace`（必填）
- **响应**：`200 OK`，`{"status": "uninstalled", "name": "my-nginx"}`
- 缺 `namespace`：`400`

### POST /api/v1/helm/releases/{name}/rollback

回滚 release 到指定版本（省略 `revision` 则回滚到上一版本）。

- **认证**：需 `helm:write` 权限
- **请求体**：

```json
{
  "namespace": "default",
  "revision": 2
}
```

- `namespace`：必填
- **响应**：`200 OK`，返回回滚后的 `Release`

### GET /api/v1/helm/releases/{name}/history

获取 release 历史。

- **认证**：需 `helm:read` 权限
- **查询参数**：`namespace`（必填）
- **响应**：`200 OK`，`{"history": [...], "name": "my-nginx"}`

### GET /api/v1/helm/catalog

预置应用目录（`helm.DefaultCatalog` 静态目录）。

- **认证**：需 `helm:read` 权限
- **查询参数**：`category`（按分类过滤）、`q`（搜索关键词）
- **响应**：`200 OK`

```json
{
  "categories": ["web", "database", "queue"],
  "charts": [
    {"name": "nginx", "category": "web", "version": "1.25", "description": "Nginx web server"}
  ]
}
```

---

## 作业编排 API

M5 作业编排：DAG 创建 / 触发 / 状态查询。

### GET /api/v1/workflows

工作流列表。

- **响应**：

```json
[
  {
    "id": "wf-001",
    "name": "发布流水线",
    "status": "active",
    "cron": "0 2 * * *",
    "agent_id": "a-001",
    "created_at": "2026-08-07T09:00:00Z"
  }
]
```

### POST /api/v1/workflows

创建工作流（含 DAG 定义，自动环检测）。

- **请求体**：

```json
{
  "name": "发布流水线",
  "agent_id": "a-001",
  "cron": "0 2 * * *",
  "dag": {
    "nodes": [
      {"id": "n1", "name": "构建", "task": {"type": "shell", "command": "make build"}},
      {"id": "n2", "name": "部署", "task": {"type": "shell", "command": "make deploy"}}
    ],
    "edges": [
      {"from": "n1", "to": "n2"}
    ]
  }
}
```

- **响应**：`201 Created`

### GET /api/v1/workflows/{id}

工作流详情。

### POST /api/v1/workflows/{id}/trigger

手动触发工作流执行。

- **响应**：`200 OK`，`{"run_id": "run-001", "status": "running"}`

### GET /api/v1/workflows/{id}/runs

工作流执行历史。

---

## 审批 API

M5 审批中心：审批流定义 + 审批请求生命周期。依赖 `internal/approval.Engine`，Server 持有 `*approval.Engine` 实例。所有写操作触发审计 + 事件总线发布；审批状态变更同步发布 SSE 事件 `approval_status`。

### GET /api/v1/approval/flows

列出审批流（按租户过滤）。

- **认证**：需 `approval:read` 权限
- **响应**：`200 OK`

```json
{
  "flows": [
    {
      "id": "flow-001",
      "tenantID": "t1",
      "name": "上线审批",
      "steps": [
        {"order": 1, "approver": "u-002", "name": "主管审批"}
      ]
    }
  ],
  "total": 1
}
```

### POST /api/v1/approval/flows

创建审批流。

- **认证**：需 `approval:write` 权限
- **请求体**：`ApprovalFlow`（`id` 未提供时自动生成 `flow-<8hex>`，`tenantID` 由请求上下文注入）
- **响应**：`201 Created`，返回完整 `ApprovalFlow`

### GET /api/v1/approval/flows/{id}

审批流详情。

- **认证**：需 `approval:read` 权限
- **响应**：`200 OK`，返回 `ApprovalFlow`
- 不存在：`404`；跨租户：`403`

### PUT /api/v1/approval/flows/{id}

更新审批流。

- **认证**：需 `approval:write` 权限
- **请求体**：完整 `ApprovalFlow`（`id` 取路径参数）
- **响应**：`200 OK`，返回更新后的 `ApprovalFlow`

### DELETE /api/v1/approval/flows/{id}

删除审批流。

- **认证**：需 `approval:write` 权限
- **响应**：`200 OK`，`{"status": "deleted", "id": "flow-001"}`

### GET /api/v1/approval/requests

列出审批请求。

- **认证**：需 `approval:read` 权限
- **查询参数**：`status`（pending/approved/rejected/cancelled）
- **响应**：`200 OK`，`{"requests": [...], "total": N}`

### POST /api/v1/approval/requests

提交审批请求。

- **认证**：需 `approval:write` 权限
- **请求体**：`ApprovalRequest`（`id` 自动生成 `apr-<8hex>`，`operator` 默认取当前用户，`status` 默认 `pending`）

```json
{
  "flowID": "flow-001",
  "triggerType": "deploy",
  "target": "dp-001",
  "payload": {"name": "nginx 滚动升级"}
}
```

- **响应**：`201 Created`，返回完整 `ApprovalRequest`

### GET /api/v1/approval/requests/{id}

审批请求详情。

- **认证**：需 `approval:read` 权限
- **响应**：`200 OK`，返回 `ApprovalRequest`
- 不存在：`404`；跨租户：`403`

### POST /api/v1/approval/requests/{id}/approve

审批通过（推进到下一审批节点或最终通过）。

- **认证**：需 `approval:approve` 权限
- **请求体**：`{"comment": "同意，可上线"}`
- **响应**：`200 OK`，`{"status": "approved", "id": "apr-001"}`
- 同时发布 SSE 事件 `approval_status`，data: `{"requestID":"apr-001","status":"approved"}`

### POST /api/v1/approval/requests/{id}/reject

审批拒绝。

- **认证**：需 `approval:approve` 权限
- **请求体**：`{"comment": "风险过大，驳回"}`
- **响应**：`200 OK`，`{"status": "rejected", "id": "apr-001"}`
- 同时发布 SSE 事件 `approval_status`，data: `{"requestID":"apr-001","status":"rejected"}`

### POST /api/v1/approval/requests/{id}/cancel

取消审批请求（提交者主动撤销）。

- **认证**：需 `approval:write` 权限
- **请求体**：无
- **响应**：`200 OK`，`{"status": "cancelled", "id": "apr-001"}`
- 同时发布 SSE 事件 `approval_status`，data: `{"requestID":"apr-001","status":"cancelled"}`

### GET /api/v1/approval/requests/{id}/history

审批历史（每步审批节点 + 操作人 + 时间 + 备注）。

- **认证**：需 `approval:read` 权限
- **响应**：`200 OK`，返回 `ApprovalHistory[]`
- 跨租户：`403`

### GET /api/v1/approval/pending

待我审批列表（按当前 `X-User-Id` 过滤，再按租户隔离）。

- **认证**：需 `approval:read` 权限
- **响应**：`200 OK`，`{"pending": [...], "total": N}`

---

## CMDB API

M2 配置库：CI 类型 / CI 实例 / 关系 / 属性模板。

### GET /api/v1/cmdb/types

CI 类型列表。

### POST /api/v1/cmdb/types

创建 CI 类型。

- **请求体**：

```json
{
  "name": "host",
  "display_name": "主机",
  "attributes": [
    {"name": "ip", "type": "string", "required": true},
    {"name": "cpu_cores", "type": "int"}
  ]
}
```

### GET /api/v1/cmdb/ci

CI 实例列表。

- **查询参数**：`type`、`status`（默认 active）

### POST /api/v1/cmdb/ci

创建 CI 实例（进入待审批状态）。

- **请求体**：

```json
{
  "type": "host",
  "name": "web-01",
  "attributes": {"ip": "10.30.0.5", "cpu_cores": 8}
}
```

- **响应**：`201 Created`

### GET /api/v1/cmdb/ci/{id}

CI 实例详情。

### PUT /api/v1/cmdb/ci/{id}

更新 CI 实例。

### DELETE /api/v1/cmdb/ci/{id}

删除 CI 实例。

### GET /api/v1/cmdb/ci/pending

待审批 CI 列表。

### POST /api/v1/cmdb/ci/{id}/approve

审批 CI 实例（管理员）。

### GET /api/v1/cmdb/ci/export

导出 CI（CSV）。

- **查询参数**：`type`
- **响应**：`200 OK`，`Content-Type: text/csv`

### POST /api/v1/cmdb/ci/import

导入 CI（CSV）。

- **请求体**：`multipart/form-data`，字段 `file`
- **响应**：`200 OK`，`{"imported": 10, "failed": 0}`

### GET /api/v1/cmdb/relations

关系列表。

### POST /api/v1/cmdb/relations

创建关系（如依赖、包含）。

- **请求体**：

```json
{
  "from_ci_id": "ci-001",
  "to_ci_id": "ci-002",
  "relation_type": "depends_on"
}
```

### GET /api/v1/cmdb/attr-templates

属性模板列表。

### POST /api/v1/cmdb/attr-templates

创建属性模板。

### GET /api/v1/cmdb/attr-templates/{id}

属性模板详情。

---

## CMDB 采集与变更审批 API

M2/M3 增强：CI 自动采集 + 变更审批流。CI 创建/修改/删除走审批，审批通过后才执行实际变更。

### POST /api/v1/cmdb/collect

手动触发全量采集（不经过 leader 校验，适合运维干预场景；定时采集仅 leader 副本执行）。

- **认证**：需 `cmdb:write` 权限
- **请求体**：无
- **响应**：`200 OK`

```json
{
  "collected": 12,
  "failed": 0
}
```

- 采集器未初始化：`503`，`{"error": "cmdb collector not initialized"}`
- 采集失败：`500`

### GET /api/v1/cmdb/changes

列出变更申请。

- **认证**：需 `cmdb:read` 权限
- **查询参数**：`status`（pending/approved/rejected/cancelled，省略返回全部）
- **响应**：`200 OK`

```json
{
  "changes": [
    {
      "id": "chg-001",
      "tenantID": "t1",
      "requester": "u-001",
      "operation": "create",
      "ciType": "host",
      "ciName": "web-01",
      "payload": {"ip": "10.30.0.5"},
      "status": "pending",
      "createdAt": "2026-08-17T09:00:00Z"
    }
  ],
  "total": 1
}
```

### POST /api/v1/cmdb/changes

提交变更申请（CI 创建/修改/删除均经此端点进入审批流）。

- **认证**：需 `cmdb:write` 权限
- **请求体**：`CMDBChangeRequest`（`tenantID`、`requester` 由请求上下文注入）

```json
{
  "operation": "create",
  "ciType": "host",
  "ciName": "web-01",
  "payload": {"ip": "10.30.0.5", "cpu_cores": 8}
}
```

- **响应**：`201 Created`，返回完整 `CMDBChangeRequest`

### GET /api/v1/cmdb/changes/{id}

变更详情。

- **认证**：需 `cmdb:read` 权限
- **响应**：`200 OK`，返回 `CMDBChangeRequest`
- 不存在：`404`；跨租户：`403`

### POST /api/v1/cmdb/changes/{id}/approve

审批通过变更（执行实际 CI 变更）。

- **认证**：需 `cmdb:approve` 权限
- **请求体**：`{"comment": "同意"}`
- **响应**：`200 OK`，`{"status": "approved", "id": "chg-001"}`
- 跨租户：`403`

### POST /api/v1/cmdb/changes/{id}/reject

驳回变更。

- **认证**：需 `cmdb:approve` 权限
- **请求体**：`{"comment": "IP 已被占用"}`
- **响应**：`200 OK`，`{"status": "rejected", "id": "chg-001"}`
- 跨租户：`403`

---

## OS 优化 API

预置 OS 基础环境优化模板（14+ 模板），可在指定 agent 上执行。

### GET /api/v1/os-templates

模板列表。

- **查询参数**：`category`（kernel/network/security/performance）、`risk`（low/medium/high）、`os`
- **响应**：

```json
[
  {
    "id": "tpl-001",
    "name": "内核参数调优",
    "category": "kernel",
    "risk": "low",
    "os": "linux",
    "description": "调整 swappiness 与文件句柄"
  }
]
```

### GET /api/v1/os-templates/{id}

模板详情（含具体执行步骤）。

### POST /api/v1/os-templates/{id}/execute

在指定 agent 上执行优化模板。

- **请求体**：

```json
{
  "agent_id": "a-001",
  "params": {"swappiness": "10"}
}
```

- **响应**：`200 OK`，`{"task_id": "t-001", "status": "pending"}`

---

## 中间件部署 API

预置中间件部署模板（15+ 模板：nginx/redis/mysql/kafka 等），支持部署/卸载/实例查询。

### GET /api/v1/middleware-templates

模板列表。

- **查询参数**：`category`、`risk`
- **响应**：

```json
[
  {
    "id": "mw-001",
    "name": "Nginx 1.25",
    "category": "web",
    "risk": "low",
    "version": "1.25"
  }
]
```

### GET /api/v1/middleware-templates/{id}

模板详情（含部署参数 schema）。

### POST /api/v1/middleware-templates/{id}/deploy

在指定 agent 上部署中间件。

- **请求体**：

```json
{
  "agent_id": "a-001",
  "params": {"port": 80, "workers": 4}
}
```

- **响应**：`200 OK`，`{"instance_id": "mi-001", "task_id": "t-001"}`

### GET /api/v1/middleware-instances

已部署中间件实例列表。

- **查询参数**：`agentID`、`category`

### POST /api/v1/middleware-instances/{id}/uninstall

卸载中间件实例。

- **响应**：`200 OK`，`{"message": "卸载已触发", "task_id": "t-002"}`

---

## K8s 管理 API

Phase 3 K8s 多集群管理（client-go 集成）。

### GET /api/v1/k8s/clusters

集群列表。

- **响应**：

```json
[
  {
    "id": "k8s-001",
    "name": "prod-cluster",
    "api_server": "https://1.2.3.4:6443",
    "version": "1.28.0",
    "status": "connected"
  }
]
```

### POST /api/v1/k8s/clusters

注册集群。

- **请求体**：

```json
{
  "name": "prod-cluster",
  "api_server": "https://1.2.3.4:6443",
  "kube_config": "可选 kubeconfig 内容",
  "insecure_skip_tls": false
}
```

- **响应**：`201 Created`

### GET /api/v1/k8s/clusters/{id}

集群详情。

### DELETE /api/v1/k8s/clusters/{id}

删除集群注册。

### POST /api/v1/k8s/clusters/{id}/test

测试集群连接。

- **响应**：`200 OK`，`{"ok": true, "version": "1.28.0", "nodes": 3}`

### K8s 资源管理（经集群代理）

以下端点经指定集群代理操作 K8s 资源，均要求 `k8s:read`（读）/ `k8s:write`（写）权限，并做租户隔离（校验集群归属当前租户，不泄露存在性）。路径中 `{ns}` 为 namespace。

#### Namespace

- `GET /api/v1/k8s/clusters/{id}/namespaces` — Namespace 列表
  - 响应：`{"namespaces": [{"name": "default", "status": "Active", "createdAt": "2026-08-01T00:00:00Z"}]}`

#### Pod

- `GET /api/v1/k8s/clusters/{id}/pods?namespace={ns}` — Pod 列表（`namespace` 为空时跨所有 namespace）
  - 响应：`{"pods": [{"name": "app-xxx", "namespace": "default", "status": "Running", "podIP": "10.244.0.5", "nodeIP": "10.30.0.5", "restarts": 0, "age": "1h"}]}`
- `GET /api/v1/k8s/clusters/{id}/pods/{ns}/{name}/logs?container=&tailLines=` — Pod 日志（`container` 多容器 pod 时必填；`tailLines` 限制返回行数）
  - 响应：`{"logs": "..."}`
- `DELETE /api/v1/k8s/clusters/{id}/pods/{ns}/{name}` — 删除 Pod

#### Deployment

- `GET /api/v1/k8s/clusters/{id}/deployments?namespace={ns}` — Deployment 列表
- `GET /api/v1/k8s/clusters/{id}/deployments/{ns}/{name}` — Deployment 详情
- `POST /api/v1/k8s/clusters/{id}/deployments/{ns}/{name}/scale` — 扩缩容
  - 请求体：`{"replicas": 5}`
- `POST /api/v1/k8s/clusters/{id}/deployments/{ns}/{name}/restart` — 重启（rolling restart）
- `POST /api/v1/k8s/clusters/{id}/deployments/{ns}/{name}/rollback` — 回滚到上一版本

#### Service / ConfigMap / Secret

- `GET /api/v1/k8s/clusters/{id}/services?namespace={ns}` — Service 列表
- `GET /api/v1/k8s/clusters/{id}/configmaps?namespace={ns}` — ConfigMap 列表
- `GET /api/v1/k8s/clusters/{id}/secrets?namespace={ns}` — Secret 列表（仅返回元数据，不返回 `data`）

#### Node

- `GET /api/v1/k8s/clusters/{id}/nodes` — Node 列表（含角色、容量、可调度性）

#### 集群概览

- `GET /api/v1/k8s/clusters/{id}/dashboard` — 集群仪表盘概览（节点数 / Pod 数 / 健康度 / 资源使用率）
- `GET /api/v1/k8s/clusters/{id}/health` — 集群健康检查（控制面 / 数据面 / 调度器 / etcd 状态）

---

## 网络拓扑与诊断 API

M6 集成：网络拓扑发现 + 网络诊断工具 + 批量连通性检测。诊断命令（ping/traceroute/tcping/nslookup/curl）通过下发 shell task 到指定 agent 执行，复用现有任务机制；结果经 `/api/v1/network/diagnose/{taskId}` 或 `/api/v1/tasks/{id}/result` 查询。拓扑探测结果缓存 5 分钟。

### GET /api/v1/network/topology

返回网络拓扑图（节点=设备/agent，边=连通性+延迟）。

- **认证**：需 `device:read` 权限
- **查询参数**：`refresh`（`true` 强制刷新，忽略缓存重新探测）
- **响应**：`200 OK`

```json
{
  "nodes": [
    {"id": "d-001", "hostname": "host-1", "ip": "10.30.0.5", "status": "online", "os": "linux", "segment": "seg-a"}
  ],
  "edges": [
    {"source": "d-001", "target": "d-002", "latencyMs": 0.234, "loss": 0, "alive": true}
  ],
  "generatedAt": "2026-08-17T09:00:00Z",
  "tenantID": "t1"
}
```

- 探测策略：对在线设备两两组合下发 ping，同步等待最多 3 秒收集结果；未完成的不阻塞，下次刷新补齐

### GET /api/v1/network/topology/cache

返回最近一次缓存的拓扑（不触发探测）。

- **认证**：需 `device:read` 权限
- **响应**：`200 OK`

```json
{
  "nodes": [...],
  "edges": [...],
  "generatedAt": "2026-08-17T09:00:00Z",
  "tenantID": "t1",
  "cached": true
}
```

- 无缓存：`200 OK`，`{"nodes": [], "edges": [], "generatedAt": "0001-01-01T00:00:00Z", "cached": false}`

### POST /api/v1/network/diagnose

发起网络诊断任务。

- **认证**：需 `task:write` 权限
- **请求体**：

```json
{
  "agentId": "a-001",
  "tool": "ping",
  "target": "10.30.0.2",
  "options": {"count": 4, "timeout": 5, "port": 0}
}
```

- `agentId`、`target`：必填
- `tool`：`ping` / `traceroute` / `tcping` / `nslookup` / `curl`
- `options.count`：1-100，默认 4
- `options.timeout`：1-30 秒，默认 5
- `options.port`：`tool=tcping` 时必填
- 命令构造按 agent.OS 区分 Linux/Windows（如 Windows ping 用 `-n`/`-w`，traceroute 用 `tracert`，tcping 用 PowerShell `Test-NetConnection`）
- **响应**：`202 Accepted`

```json
{"taskId": "t-001"}
```

- agent 不存在或跨租户：`404`

### GET /api/v1/network/diagnose/{taskId}

查询诊断任务结果（复用 `store.TaskResult`）。

- **认证**：需 `task:read` 权限
- **响应**：`200 OK`

```json
{
  "taskId": "t-001",
  "exitCode": 0,
  "stdout": "PING 10.30.0.2 ... 3 packets transmitted, 3 received, 0% packet loss",
  "stderr": "",
  "durationMs": 2002,
  "finishedAt": "2026-08-17T09:00:02Z",
  "pending": false
}
```

- 任务仍在执行：`200 OK`，`{"taskId": "t-001", "status": "running", "pending": true}`
- 任务不存在：`404`；跨租户：`403`

### POST /api/v1/network/connectivity

批量连通性检测（对每个 target 发起 tcping/ping，同步等待最多 5 秒收集结果）。

- **认证**：需 `task:write` 权限
- **请求体**：

```json
{
  "sourceAgentId": "a-001",
  "targets": [
    {"ip": "10.30.0.2", "port": 22},
    {"ip": "10.30.0.3"}
  ]
}
```

- `targets[].port` > 0 时用 tcping（`nc -zv` / `Test-NetConnection`），否则用 ping
- **响应**：`200 OK`

```json
{
  "results": [
    {"target": "10.30.0.2", "alive": true, "latencyMs": 0},
    {"target": "10.30.0.3", "alive": true, "latencyMs": 0.234}
  ]
}
```

- source agent 不存在或跨租户：`404`

---

## 密钥管理 API

安全增强：密钥 provider（env / file / vault / chain）配置概览 + 连接测试 + key 枚举。所有端点不返回 Vault token 与密钥值（仅 key 名称 + 来源 provider）。

### GET /api/v1/secrets/status

返回当前 provider 配置概览（不包含 Vault token）。

- **认证**：需 `secrets:read` 权限
- **响应**：`200 OK`

```json
{
  "provider": "chain:env,file,vault",
  "enabled": true,
  "addr": "https://vault.example.com:8200",
  "mount": "secret",
  "file": "/etc/opsmesh/secrets.json"
}
```

- `enabled`：`cfg.SecretProvider` 非空时为 true
- `addr` / `mount`：仅 vault/chain:*vault 时非空
- `file`：仅 file/chain:*file 时非空

### POST /api/v1/secrets/test

测试 Vault provider 连接（构造临时 `VaultProvider` 发起一次轻量探测）。

- **认证**：需 `secrets:write` 权限
- **请求体**：

```json
{
  "addr": "https://vault.example.com:8200",
  "token": "s.xxxxxxxx",
  "mount": "secret"
}
```

- `addr`：必填；`token` 为空时回退环境变量 `OPSMESH_VAULT_TOKEN`；`mount` 默认 `secret`
- SSRF 校验：拒绝私网/环回/元数据地址（`validateURLSSRF`）
- **响应**：`200 OK`

```json
{"ok": true, "latencyMs": 42}
```

- SSRF 拦截 / token 缺失 / Vault 不可达：`200 OK`，`{"ok": false, "latencyMs": N, "error": "..."}`
- 注：探测到 404 也视为 Vault 可达（`ok: true`）

### GET /api/v1/secrets/keys

返回密钥 key 列表（仅名称 + 来源 provider，不含值）。

- **认证**：需 `secrets:read` 权限
- **响应**：`200 OK`

```json
[
  {"key": "DB_PASSWORD", "provider": "env"},
  {"key": "a/b", "provider": "file"},
  {"key": "api_key", "provider": "file"}
]
```

- 数据来源：
  - `env` provider：扫描 `OPSMESH_` 前缀环境变量
  - `file` provider：从 `cfg.SecretFile` 加载 JSON 并扁平化 key 路径（如 `{"a":{"b":"v"}}` → `a/b`）
  - `vault` provider：KV v2 不支持全量 list，返回空（前端展示"暂无密钥"）
  - `chain:` 多 provider 合并按 key 去重（优先级高者保留）
- 未配置 provider：`200 OK`，`[]`

---

## 租户配额 API

多租户资源配额：限制每租户的设备数 / 任务数 / 告警数上限，超额拒绝（创建路径返回 `429` 或 `403`）。配额为 0 表示不限；未设置配额的租户使用默认配额（来自 `--quota-max-devices` / `--quota-max-tasks` / `--quota-max-alerts`）。

### GET /api/v1/quotas

列出当前租户配额 + 用量（管理端用）。

- **认证**：需 `quota:read` 权限
- **响应**：`200 OK`

```json
{
  "enabled": true,
  "current": {
    "quota": {"maxDevices": 100, "maxTasks": 1000, "maxAlerts": 50},
    "usage": {"devices": 12, "tasks": 34, "alerts": 3}
  }
}
```

- 未启用配额检查：`200 OK`，`{"enabled": false, "quotas": []}`

### GET /api/v1/quotas/{tenantID}

获取指定租户配额 + 当前用量。

- **认证**：需 `quota:read` 权限；非 admin 用户仅能查看自己租户的配额（跨租户访问返回 `403`）
- **响应**：`200 OK`，返回 `QuotaUsage`

```json
{
  "quota": {"maxDevices": 100, "maxTasks": 1000, "maxAlerts": 50},
  "usage": {"devices": 12, "tasks": 34, "alerts": 3}
}
```

### PUT /api/v1/quotas/{tenantID}

设置租户配额（仅 admin 角色可跨租户设置）。

- **认证**：需 `quota:write` 权限 + admin 角色
- **请求体**：

```json
{
  "maxDevices": 200,
  "maxTasks": 2000,
  "maxAlerts": 100
}
```

- 各字段为 0 表示不限；负数返回 `400`
- **响应**：`200 OK`，返回更新后的 `QuotaConfig`
- 非 admin：`403`，`{"error": "admin role required to set quota"}`

### DELETE /api/v1/quotas/{tenantID}

清除租户配额（回退到默认配额）。

- **认证**：需 `quota:write` 权限 + admin 角色
- **响应**：`200 OK`，`{"status": "deleted", "tenantID": "t1"}`

---

## 审计 API

### GET /api/v1/audits

审计事件检索（100% 留痕）。

- **查询参数**：
  - `tenant` — 租户过滤
  - `action` — 动作类型（如 `task:create`、`device:delete`、`auth:login`）
  - `from` — 起始时间（RFC3339）
  - `to` — 结束时间（RFC3339）
  - `limit` — 上限（默认 100）
- **响应**：

```json
[
  {
    "id": "au-001",
    "tenant_id": "t1",
    "user_id": "u-001",
    "action": "task:create",
    "resource": "t-001",
    "detail": "下发 shell 任务 uname -a",
    "ip": "10.0.0.1",
    "created_at": "2026-08-07T09:00:00Z"
  }
]
```

---

## 日志 API

M6 日志检索：双后端（Memory/SQL/Loki/ES）+ offset 分页。

### GET /api/v1/logs

日志检索。

- **查询参数**：
  - `deviceID`、`agentID` — 来源过滤
  - `level` — 日志级别（debug/info/warn/error）
  - `source` — 来源
  - `keyword` — 关键词
  - `from`、`to` — 时间范围（RFC3339）
  - `limit`、`offset` — 分页
- **响应**：

```json
[
  {
    "id": "log-001",
    "device_id": "d-001",
    "agent_id": "a-001",
    "level": "info",
    "source": "task",
    "message": "任务执行完成",
    "timestamp": "2026-08-07T09:00:00Z"
  }
]
```

### POST /api/v1/logs

追加日志（agent 经 gRPC 上报时由控制面代写入；loki/es 模式下为 noop）。

- **请求体**：

```json
{
  "device_id": "d-001",
  "level": "info",
  "source": "task",
  "message": "自定义日志条目"
}
```

---

## 联邦 API

控制面联邦：仅当 `--federation-peers` 非空时注册。联邦通道硬化为 mTLS + HMAC 签名。

### GET /api/v1/federation/peers

联邦 peer 列表（含可达性状态）。

- **响应**：

```json
[
  {"address": "http://peer1:8080", "reachable": true, "latency_ms": 5},
  {"address": "http://peer2:8080", "reachable": false, "latency_ms": 0}
]
```

### POST /api/v1/federation/forward/task

跨网段转发任务到 peer 控制面执行。

- **请求体**：标准 Task 对象
- **响应**：`200 OK`，`{"forwarded_to": "http://peer1:8080", "task_id": "t-001"}`

### GET /api/v1/federation/devices

联邦设备视图（聚合所有 peer 的设备清单）。

- **响应**：

```json
[
  {"peer": "http://peer1:8080", "devices": [...]},
  {"peer": "http://peer2:8080", "devices": [...], "error": "unreachable"}
]
```

---

## SSE 事件流 API

### GET /api/v1/events/stream

SSE 实时推送（替代 5s 轮询）。推送设备状态变更、任务状态变更、告警产出等事件。

- **响应**：`Content-Type: text/event-stream`，长连接

```
event: task_status
data: {"task_id":"t-001","status":"done","exit_code":0}

event: device_state
data: {"device_id":"d-001","state":"online"}

event: alert
data: {"id":"al-001","severity":"critical","title":"任务死信"}
```

---

## 纳管 Bootstrap 端点

### GET /install.sh

agent bootstrap 安装脚本（curl 管道执行）。

- **响应**：`200 OK`，`Content-Type: text/x-shellscript`

```bash
# 用法
curl -fsSL http://controlplane:8080/install.sh | sh -s -- --token=<tok> --control-addr=http://controlplane:8080
```

### GET /bin/opsmesh-agent

agent 二进制下载（纳管 bootstrap 拉取）。

- **响应**：`200 OK`，`Content-Type: application/octet-stream`

---

## gRPC API

gRPC 服务监听 9090 端口（JSON codec），agent 通过此通道注册/心跳/拉任务/上报/取消。

### 服务 opsmesh.v1.Registration

| 方法 | 请求 | 响应 | 说明 |
|------|------|------|------|
| `Register` | `{install_token, hostname, segment, os, arch, version}` | `{agent_id, tenant_id, accepted}` | 注册 agent；携带 install_token 时可自动纳管候选设备 |
| `Heartbeat` | `{agent_id, state, load}` | `{accepted}` | 上报在线状态与负载（每 10s） |
| `PullTasks` | `{agent_id}` | `{task}` | 原子领取下一条 pending 任务（多副本安全） |
| `ReportResult` | `{agent_id, task_id, status, exit_code, stdout, stderr}` | `{accepted}` | 上报任务执行结果（成功/失败/重试/死信） |
| `CancelTask` | `{agent_id, task_id}` | `{cancelled}` | 取消指定任务（服务端按租户隔离） |
| `PollCancels` | `{agent_id}` | `{task_ids: []}` | agent 轮询本机被取消的任务 ID（每 2s） |
| `ReportLogs` | `{agent_id, log_name, lines[]}` | `{accepted}` | agent 上报任务执行日志（日志采集，含 timestamp/level/message） |

### gRPC 安全

- **TLS/mTLS**：`--tls-cert` / `--tls-key` / `--client-ca` 启用服务端证书与客户端证书校验
- **HMAC 签名**：`--grpc-require-signature=true` 时 agent 请求须携带 `agent-signature` metadata（HMAC-SHA256(agent_id + timestamp + secret)），时间戳偏差 ±5min 防重放
- **Recovery 拦截器**：`grpcRecoveryInterceptor` 捕获 handler panic，返回 `codes.Internal`，单处 panic 不拖垮整个控制面