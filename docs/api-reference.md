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
- [平台化 API（租户 / API Key / 插件市场 / 平台管理）](#平台化-api租户--api-key--插件市场--平台管理)
- [计费 API](#计费-api)
- [网关 API](#网关-api)
- [备份与灾备 API](#备份与灾备-api)
- [合规 API](#合规-api)
- [HA API](#ha-api)
- [工单与 SLO API](#工单与-slo-api)
- [流量治理 API](#流量治理-api)
- [流水线与 ArgoCD API](#流水线与-argocd-api)
- [配置热推送 API](#配置热推送-api)
- [自动化 API](#自动化-api)
- [Webhook 与脚本 API](#webhook-与脚本-api)
- [网络设备 API](#网络设备-api)
- [审计扩展 API](#审计扩展-api)
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

## 平台化 API（租户 / API Key / 插件市场 / 平台管理）

Phase 6 平台化：多租户管理 + 程序化访问（API Key）+ 插件市场 + 平台配置/健康/指标。
除租户与插件市场为平台级资源外，API Key 按租户隔离（`X-Tenant-ID` 必填）。

### GET /api/v1/tenants

列出租户（平台级，跨租户可见）。

- **认证**：需 `tenant:read` 权限
- **响应**：`200 OK`

```json
{
  "tenants": [
    {
      "id": "t1",
      "name": "tenant-a",
      "displayName": "租户 A",
      "status": "active",
      "quota": {"maxDevices": 100, "maxTasks": 0, "maxActiveTasks": 0, "maxAlerts": 50, "maxAgents": 0, "maxWebhooks": 0, "maxAPIKeys": 0},
      "usage": {"devices": 12, "tasks": 34, "activeTasks": 2, "alerts": 3, "agents": 12, "webhooks": 1, "apiKeys": 2},
      "createdAt": "2026-08-17T09:00:00Z",
      "updatedAt": "2026-08-17T09:00:00Z"
    }
  ]
}
```

### POST /api/v1/tenants

创建租户。

- **认证**：需 `tenant:write` 权限
- **请求体**：`Tenant` 对象（`name` 必填；`status` 缺省为 `active`）

```json
{
  "name": "tenant-a",
  "displayName": "租户 A",
  "quota": {"maxDevices": 100}
}
```

- `name`：必填
- `status`：`active` | `suspended` | `disabled`，缺省 `active`
- **响应**：`201 Created`，返回完整 `Tenant`

### GET /api/v1/tenants/{id}

租户详情。

- **认证**：需 `tenant:read` 权限
- **响应**：`200 OK`，返回 `Tenant`
- 不存在：`404`，`{"error": "tenant not found"}`

### PUT /api/v1/tenants/{id}

更新租户（请求体为完整 `Tenant`，`id` 取路径参数）。

- **认证**：需 `tenant:write` 权限
- **请求体**：同创建接口
- **响应**：`200 OK`，返回更新后的 `Tenant`
- 不存在：`404`

### DELETE /api/v1/tenants/{id}

删除租户。平台根租户 `default` 拒绝删除（409）；删除成功后级联清理该租户的
APIKey / Webhook / Script 三域子资源（其余域暂未级联）。

- **认证**：需 `tenant:write` 权限
- **响应**：`200 OK`，`{"status": "deleted"}`
- 删除 `default`：`409`，`{"error": "cannot delete platform tenant 'default'"}`
- 不存在：`404`

### POST /api/v1/tenants/{id}/suspend

暂停租户（`status` → `suspended`）。

- **认证**：需 `tenant:write` 权限
- **请求体**：无
- **响应**：`200 OK`，返回状态为 `suspended` 的 `Tenant`
- 不存在：`404`

### POST /api/v1/tenants/{id}/activate

激活租户（`status` → `active`）。

- **认证**：需 `tenant:write` 权限
- **请求体**：无
- **响应**：`200 OK`，返回状态为 `active` 的 `Tenant`
- 不存在：`404`

### GET /api/v1/apikeys

列出当前租户的 API Key（按 `X-Tenant-ID` 隔离）。

- **认证**：需 `apikey:read` 权限
- **响应**：`200 OK`

```json
{
  "apiKeys": [
    {
      "id": "ak-001",
      "tenantID": "t1",
      "name": "ci-bot",
      "scopes": ["device:read", "task:write"],
      "rateLimitPerSec": 0,
      "expiresAt": "0001-01-01T00:00:00Z",
      "lastUsedAt": "0001-01-01T00:00:00Z",
      "enabled": true,
      "createdAt": "2026-08-17T09:00:00Z"
    }
  ]
}
```

> Key 仅为 SHA-256 hash，JSON 序列化时不输出（`json:"-"`），明文只在创建时返回一次。

### POST /api/v1/apikeys

创建 API Key。服务端生成明文 key（仅此一次返回）+ SHA-256 hash 落库，`enabled` 强制为 true。

- **认证**：需 `apikey:write` 权限
- **请求体**：

```json
{
  "name": "ci-bot",
  "scopes": ["device:read", "task:write"],
  "rateLimitPerSec": 10
}
```

- `name`：必填
- **响应**：`201 Created`

```json
{
  "apiKey": {"id": "ak-001", "name": "ci-bot", "scopes": ["device:read", "task:write"], "enabled": true},
  "plainKey": "omk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
}
```

> `plainKey` 仅本次响应返回，之后不可再获取。

### GET /api/v1/apikeys/{id}

API Key 详情。

- **认证**：需 `apikey:read` 权限
- **响应**：`200 OK`，返回 `APIKey`
- 不存在：`404`，`{"error": "api key not found"}`

### PUT /api/v1/apikeys/{id}

更新 API Key。白名单字段合并（仅允许更新 `Name` 与 `Scopes`）：
`Enabled` 必须走 `/enable|/disable` 端点；`Key`（hash）/ `ID` / `TenantID` / `CreatedAt` 强制保留原值。
`scopes` 不允许清空（防提权）。

- **认证**：需 `apikey:write` 权限
- **请求体**：

```json
{
  "name": "ci-bot-v2",
  "scopes": ["device:read", "task:write", "alert:read"]
}
```

- `scopes` 为空数组：`400`，`{"error": "scopes must not be empty"}`
- **响应**：`200 OK`，返回更新后的 `APIKey`
- 不存在：`404`

### DELETE /api/v1/apikeys/{id}

删除 API Key。

- **认证**：需 `apikey:write` 权限
- **响应**：`200 OK`，`{"status": "deleted"}`
- 不存在：`404`

### POST /api/v1/apikeys/{id}/enable

启用 API Key（`enabled` → true）。

- **认证**：需 `apikey:write` 权限
- **请求体**：无
- **响应**：`200 OK`，返回更新后的 `APIKey`
- 不存在：`404`

### POST /api/v1/apikeys/{id}/disable

禁用 API Key（`enabled` → false）。

- **认证**：需 `apikey:write` 权限
- **请求体**：无
- **响应**：`200 OK`，返回更新后的 `APIKey`
- 不存在：`404`

### GET /api/v1/marketplace/plugins

列出插件市场全部插件（平台级）。

- **认证**：需 `plugin:read` 权限
- **响应**：`200 OK`

```json
{
  "plugins": [
    {
      "id": "plg-001",
      "name": "mysql-exporter",
      "version": "1.2.0",
      "description": "MySQL 指标采集插件",
      "author": "opsmesh",
      "type": "data",
      "downloadURL": "https://plugins.opsmesh.io/mysql-exporter-1.2.0.tar.gz",
      "checksum": "sha256:...",
      "installed": true,
      "enabled": true,
      "createdAt": "2026-08-17T09:00:00Z"
    }
  ]
}
```

### POST /api/v1/marketplace/plugins

注册插件到市场。

- **认证**：需 `plugin:write` 权限
- **请求体**：

```json
{
  "name": "mysql-exporter",
  "version": "1.2.0",
  "description": "MySQL 指标采集插件",
  "author": "opsmesh",
  "type": "data",
  "downloadURL": "https://plugins.opsmesh.io/mysql-exporter-1.2.0.tar.gz",
  "checksum": "sha256:..."
}
```

- `name`、`version`、`type`：必填
- `type`：白名单 `data` | `logic` | `integration`
- `downloadURL`：可空（内嵌插件）；非空时仅允许 `http` / `https` scheme（拒绝 `file://` 等）
- **响应**：`201 Created`，返回完整 `Plugin`

### GET /api/v1/marketplace/plugins/{id}

插件详情。

- **认证**：需 `plugin:read` 权限
- **响应**：`200 OK`，返回 `Plugin`
- 不存在：`404`，`{"error": "plugin not found"}`

### DELETE /api/v1/marketplace/plugins/{id}

删除插件。

- **认证**：需 `plugin:write` 权限
- **响应**：`200 OK`，`{"status": "deleted"}`
- 不存在：`404`

### POST /api/v1/marketplace/plugins/{id}/install

安装插件。`downloadURL` 非空时真实下载（SSRF 校验拒绝私网地址）→ SHA-256 校验
（`checksum` 非空时）→ 保存到 `data/plugins/<id>/plugin.bin`；已安装时幂等直接返回。

- **认证**：需 `plugin:write` 权限
- **请求体**：无
- **响应**：`200 OK`，返回 `installed=true, enabled=true` 的 `Plugin`
- 下载/校验失败：`500`，`{"error": "plugin download/verify failed: ..."}`
- 不存在：`404`

### POST /api/v1/marketplace/plugins/{id}/uninstall

卸载插件（删除 `data/plugins/<id>/` 目录，`installed=false, enabled=false`）。

- **认证**：需 `plugin:write` 权限
- **请求体**：无
- **响应**：`200 OK`，返回更新后的 `Plugin`
- 不存在：`404`

### POST /api/v1/marketplace/plugins/{id}/enable

启用插件。

- **认证**：需 `plugin:write` 权限
- **请求体**：无
- **响应**：`200 OK`，返回 `enabled=true` 的 `Plugin`
- 不存在：`404`

### POST /api/v1/marketplace/plugins/{id}/disable

禁用插件。

- **认证**：需 `plugin:write` 权限
- **请求体**：无
- **响应**：`200 OK`，返回 `enabled=false` 的 `Plugin`
- 不存在：`404`

### GET /api/v1/platform/config

查询平台配置。优先读 store（tenant=`default`，key=`platform/config`），
未设置时回退出厂默认值。

- **认证**：需 `platform:read` 权限
- **响应**：`200 OK`

```json
{
  "version": "0.6.0",
  "buildTime": "2026-08-17",
  "goVersion": "go1.22",
  "defaultTenant": "default",
  "maxTenants": 100,
  "enableMarketplace": true,
  "enableBilling": true,
  "updatedAt": "2026-08-17T09:00:00Z"
}
```

### PUT /api/v1/platform/config

更新平台配置（真实持久化：序列化为 JSON 写入 ConfigStore，`updatedAt` 由服务端覆盖）。

- **认证**：需 `platform:write` 权限
- **请求体**：同上 `PlatformConfig`
- **响应**：`200 OK`，返回实际保存的配置
- 落库失败：`500`

### GET /api/v1/platform/health

平台健康检查（组件级状态视图）。

- **认证**：需 `platform:read` 权限
- **响应**：`200 OK`

```json
{
  "status": "ok",
  "components": {"store": "ok", "agent": "ok", "task": "ok", "alert": "ok", "billing": "ok"},
  "timestamp": "2026-08-17T09:00:00Z"
}
```

### GET /api/v1/platform/metrics

平台指标汇总（各域资源计数）。

- **认证**：需 `platform:read` 权限
- **响应**：`200 OK`

```json
{
  "tenants": 5,
  "devices": 120,
  "tasks": 3,
  "alerts": 2,
  "apiKeys": 8,
  "plugins": 10,
  "subscriptions": 5,
  "invoices": 12
}
```

---

## 计费 API

Phase 6 计费：订阅计划（平台级）+ 订阅（按租户隔离）+ 账单 + 资源用量。
金额单位均为分（int，避免浮点精度问题）。

### GET /api/v1/billing/plans

列出全部订阅计划。

- **认证**：需 `billing:read` 权限
- **响应**：`200 OK`

```json
{
  "plans": [
    {
      "id": "plan-001",
      "name": "专业版",
      "price": 29900,
      "interval": "monthly",
      "features": ["无限设备", "工单系统"],
      "resourceLimits": {"maxDevices": 500, "maxTasks": 0, "maxActiveTasks": 0, "maxAlerts": 200, "maxAgents": 0, "maxWebhooks": 20, "maxAPIKeys": 10},
      "createdAt": "2026-08-17T09:00:00Z"
    }
  ]
}
```

### POST /api/v1/billing/plans

创建订阅计划。

- **认证**：需 `billing:write` 权限
- **请求体**：

```json
{
  "name": "专业版",
  "price": 29900,
  "interval": "monthly",
  "features": ["无限设备", "工单系统"],
  "resourceLimits": {"maxDevices": 500}
}
```

- `name`：必填；`interval` 缺省 `monthly`（取值 `monthly` | `yearly`）
- **响应**：`201 Created`，返回完整 `SubscriptionPlan`

### GET /api/v1/billing/plans/{id}

计划详情。

- **认证**：需 `billing:read` 权限
- **响应**：`200 OK`，返回 `SubscriptionPlan`
- 不存在：`404`，`{"error": "plan not found"}`

### PUT /api/v1/billing/plans/{id}

更新计划（请求体为完整 `SubscriptionPlan`，`id` 取路径参数）。

- **认证**：需 `billing:write` 权限
- **响应**：`200 OK`，返回更新后的计划
- 不存在：`404`

### DELETE /api/v1/billing/plans/{id}

删除计划。

- **认证**：需 `billing:write` 权限
- **响应**：`200 OK`，`{"status": "deleted"}`
- 不存在：`404`

### GET /api/v1/billing/subscriptions

列出当前租户的订阅（按 `X-Tenant-ID` 隔离）。

- **认证**：需 `billing:read` 权限
- **响应**：`200 OK`

```json
{
  "subscriptions": [
    {
      "id": "sub-001",
      "tenantID": "t1",
      "planID": "plan-001",
      "status": "active",
      "startedAt": "2026-08-17T09:00:00Z",
      "expiresAt": "2026-09-17T09:00:00Z",
      "createdAt": "2026-08-17T09:00:00Z"
    }
  ]
}
```

### POST /api/v1/billing/subscriptions

为当前租户创建订阅。

- **认证**：需 `billing:write` 权限
- **请求体**：

```json
{
  "planID": "plan-001",
  "expiresAt": "2026-09-17T09:00:00Z"
}
```

- `planID`：必填；`tenantID` 由请求上下文注入；`status` 缺省 `active`
- **响应**：`201 Created`，返回完整 `Subscription`

### GET /api/v1/billing/subscriptions/{id}

订阅详情。

- **认证**：需 `billing:read` 权限
- **响应**：`200 OK`，返回 `Subscription`
- 不存在：`404`，`{"error": "subscription not found"}`

### PUT /api/v1/billing/subscriptions/{id}

更新订阅（`id` 取路径参数）。

- **认证**：需 `billing:write` 权限
- **请求体**：完整 `Subscription`
- **响应**：`200 OK`，返回更新后的订阅
- 不存在：`404`

### DELETE /api/v1/billing/subscriptions/{id}

删除订阅。

- **认证**：需 `billing:write` 权限
- **响应**：`200 OK`，`{"status": "deleted"}`
- 不存在：`404`

### GET /api/v1/billing/invoices

列出当前租户的账单（按创建时间降序）。

- **认证**：需 `billing:read` 权限
- **响应**：`200 OK`

```json
{
  "invoices": [
    {
      "id": "inv-001",
      "tenantID": "t1",
      "subscriptionID": "sub-001",
      "amount": 29900,
      "periodStart": "2026-08-01T00:00:00Z",
      "periodEnd": "2026-08-31T00:00:00Z",
      "status": "paid",
      "items": [{"name": "专业版月费", "quantity": 1, "unitPrice": 29900, "amount": 29900}],
      "createdAt": "2026-08-17T09:00:00Z"
    }
  ]
}
```

### GET /api/v1/billing/invoices/{id}

账单详情。

- **认证**：需 `billing:read` 权限
- **响应**：`200 OK`，返回 `Invoice`
- 不存在：`404`，`{"error": "invoice not found"}`

### GET /api/v1/billing/usage

当前租户资源用量统计（实时计算）。

- **认证**：需 `billing:read` 权限
- **响应**：`200 OK`

```json
{
  "tenantID": "t1",
  "deviceCount": 12,
  "taskCount": 34,
  "alertCount": 3,
  "metricsCount": 120,
  "calculatedAt": "2026-08-17T09:00:00Z"
}
```

- 计算失败：`500`，`{"error": "failed to calculate usage"}`

---

## 网关 API

Phase 5 API 网关：路由规则 CRUD + 启停 + 统计 + `/gw/` 数据面反向代理。
路由规则保存在内存（`Server.gateway`，按租户隔离），**不持久化**——控制面重启后重置；
统计为进程级计数器，多副本各自统计未做跨副本聚合。

### GET /api/v1/gateway/routes

列出当前租户的路由规则。

- **认证**：需 `gateway:read` 权限
- **响应**：`200 OK`

```json
{
  "routes": [
    {
      "id": "gw-route-3f2a1b8c9d0e1f2a",
      "tenantID": "t1",
      "name": "device-api",
      "pathPrefix": "/api/v1/devices",
      "targetBackend": "http://backend:8080",
      "methods": ["GET", "POST"],
      "rateLimitPerSec": 100,
      "enabled": true,
      "createdAt": "2026-08-17T09:00:00Z",
      "updatedAt": "2026-08-17T09:00:00Z"
    }
  ]
}
```

### POST /api/v1/gateway/routes

创建路由规则。

- **认证**：需 `gateway:write` 权限
- **请求体**：

```json
{
  "name": "device-api",
  "pathPrefix": "/api/v1/devices",
  "targetBackend": "http://backend:8080",
  "methods": ["GET", "POST"],
  "rateLimitPerSec": 100,
  "enabled": true
}
```

- `name`、`pathPrefix`、`targetBackend`：必填
- `targetBackend`：格式 `scheme://host:port`，scheme ∈ {`http`, `https`, `grpc`}
- `methods`：空数组表示允许全部方法
- `rateLimitPerSec`：0 表示不限流
- **响应**：`201 Created`，返回完整 `RouteRule`（`id` 自动生成 `gw-route-<16hex>`）

### GET /api/v1/gateway/routes/{id}

路由规则详情。

- **认证**：需 `gateway:read` 权限
- **响应**：`200 OK`，返回 `RouteRule`
- 不存在：`404`，`{"error": "route not found"}`

### PUT /api/v1/gateway/routes/{id}

更新路由规则（`id` / `tenantID` / `createdAt` 保留原值，`updatedAt` 服务端覆盖）。

- **认证**：需 `gateway:write` 权限
- **请求体**：完整 `RouteRule`（同创建接口）
- **响应**：`200 OK`，返回更新后的 `RouteRule`
- 不存在：`404`

### DELETE /api/v1/gateway/routes/{id}

删除路由规则。

- **认证**：需 `gateway:write` 权限
- **响应**：`200 OK`，`{"status": "deleted"}`
- 不存在：`404`

### POST /api/v1/gateway/routes/{id}/enable

启用路由。

- **认证**：需 `gateway:write` 权限
- **请求体**：无
- **响应**：`200 OK`，返回 `enabled=true` 的 `RouteRule`
- 不存在：`404`

### POST /api/v1/gateway/routes/{id}/disable

禁用路由。

- **认证**：需 `gateway:write` 权限
- **请求体**：无
- **响应**：`200 OK`，返回 `enabled=false` 的 `RouteRule`
- 不存在：`404`

### GET /api/v1/gateway/stats

网关统计（进程级聚合）。

- **认证**：需 `gateway:read` 权限
- **响应**：`200 OK`

```json
{
  "totalRequests": 1024,
  "totalErrors": 3,
  "avgLatencyMs": 12.5,
  "activeRoutes": 2
}
```

- `activeRoutes` 为当前租户的 enabled 路由数；`totalRequests` / `totalErrors` / `avgLatencyMs` 为全进程统计

### /gw/（数据面反向代理）

网关最小数据面：挂载 `/gw/` 前缀（任意方法），剥前缀后按 PathPrefix + Methods 匹配
全部租户的 enabled 路由（跨租户，最小数据面不做租户鉴权），用 `httputil.ReverseProxy`
反向代理转发到 `targetBackend`。

- **匹配**：`GET/POST/PUT/DELETE http://<controlplane>:8080/gw/api/v1/devices` →
  剥 `/gw` 后以 `/api/v1/devices` 匹配路由规则，命中后转发到后端（后端按 PathPrefix 语义接收原始路径）
- **转发**：仅支持 `http` / `https` 后端；`grpc://` 及非法 scheme 返回 `502`
- **限流**：`rateLimitPerSec > 0` 时按路由令牌桶限流，超出返回 `429`
- **错误码**：
  - 无命中路由：`404`，`{"error": "no gateway route matches <path>"}`
  - 路由限流超出：`429`，`{"error": "rate limit exceeded for route <id>"}`
  - 后端不支持：`502`，`{"error": "unsupported targetBackend: ..."}`
- **统计**：每次请求计入 `totalRequests`；404 / 429 / >=500 计入 `totalErrors`；平均延迟增量更新

---

## 备份与灾备 API

Phase 3 灾备恢复（真实备份/恢复，非模拟）。归档内容与 CLI 同源：导出 store 领域数据
JSON 快照 + metadata.json 打包为 `tar.gz` 写入 `data/backups/`（Server.backupDir）。
创建为异步归档（落库 `creating` 后后台 goroutine 执行，完成后更新 `completed` + 真实
`Size`/`Path`，失败置 `failed`）。

### POST /api/v1/backup/create

创建备份（异步归档）。

- **认证**：需 `backup:write` 权限
- **请求体**：

```json
{"type": "full"}
```

- `type`：必填，`full` | `config` | `devices` | `tasks`
- **响应**：`201 Created`（status 为 `creating`，归档在后台完成后变为 `completed`）

```json
{
  "id": "bk-001",
  "tenantID": "t1",
  "type": "full",
  "status": "creating",
  "size": 0,
  "path": "",
  "createdAt": "2026-08-17T09:00:00Z"
}
```

### GET /api/v1/backup/list

列出当前租户的备份记录。

- **认证**：需 `backup:read` 权限
- **响应**：`200 OK`

```json
{
  "backups": [
    {"id": "bk-001", "tenantID": "t1", "type": "full", "status": "completed", "size": 20480, "path": "data/backups/backup-20260817-090000-bk-001.tar.gz", "createdAt": "2026-08-17T09:00:00Z"}
  ]
}
```

### POST /api/v1/backup/restore

恢复备份（真实恢复：读归档 `snapshot.json` 按字段写回 store 接口，返回各类恢复计数）。

- **认证**：需 `backup:write` 权限
- **请求体**：`{"id": "bk-001"}`
- `id`：必填
- **响应**：`200 OK`

```json
{
  "status": "restored",
  "backup": {"id": "bk-001", "status": "completed", "path": "data/backups/..."},
  "restored": {"configs": 12, "devices": 8, "agents": 8, "tasks": 3, "alertRules": 2, "templates": 1, "automationRules": 1},
  "completedAt": "2026-08-17T09:05:00Z"
}
```

- 备份记录不存在：`404`，`{"error": "backup not found"}`
- 归档路径为空 / 归档文件缺失：`404`（`"backup archive missing (path empty)"` / `"backup archive file not found: ..."`）
- 归档解析失败：`500`

### DELETE /api/v1/backup/{id}

删除备份记录。

- **认证**：需 `backup:write` 权限
- **响应**：`200 OK`，`{"status": "deleted"}`
- 不存在：`404`，`{"error": "backup not found"}`

---

## 合规 API

Phase 3 安全合规：预置 CIS Benchmark 基线规则（12 条，只读）+ 扫描聚合 + 报告查询。
引擎本身不执行 `CheckScript`（避免控制面直接 shell），实际执行由 agent 侧任务下发完成，
控制面聚合结果生成 `ComplianceReport` 落库。

### GET /api/v1/compliance/rules

列出全部预置合规规则。

- **认证**：需 `compliance:read` 权限
- **响应**：`200 OK`

```json
{
  "rules": [
    {
      "id": "cis-ssh-01",
      "name": "SSH 禁用 root 登录",
      "category": "cis",
      "severity": "high",
      "description": "PermitRootLogin 应为 no，禁止 root 直接 SSH 登录",
      "checkScript": "grep -qE \"^PermitRootLogin\\s+no\" /etc/ssh/sshd_config",
      "remediation": "编辑 /etc/ssh/sshd_config 设置 PermitRootLogin no 并重启 sshd"
    }
  ]
}
```

- `category`：`cis` | `pci_dss` | `hipaa` | `custom`；`severity`：`high` | `medium` | `low`

### GET /api/v1/compliance/rules/{id}

规则详情。

- **认证**：需 `compliance:read` 权限
- **响应**：`200 OK`，返回单条 `ComplianceRule`
- 不存在：`404`，`{"error": "rule not found"}`

### POST /api/v1/compliance/scan

提交设备合规检查结果（agent 上报格式），聚合生成报告落库。

- **认证**：需 `compliance:write` 权限
- **请求体**：

```json
{
  "deviceID": "d-001",
  "results": [
    {"ruleId": "cis-ssh-01", "passed": true, "output": "ok", "checkedAt": "2026-08-17T09:00:00Z"}
  ]
}
```

- `deviceID`：必填
- `results`：可空——为空时用引擎规则生成全 failed 占位结果（`output: "not checked"`，供测试/演示）
- 评分规则：`passed 数 / 总规则数 * 100` 向下取整
- **响应**：`201 Created`

```json
{
  "id": "rpt-001",
  "tenantID": "t1",
  "deviceID": "d-001",
  "results": [{"ruleId": "cis-ssh-01", "passed": true, "output": "ok", "checkedAt": "2026-08-17T09:00:00Z"}],
  "score": 100,
  "createdAt": "2026-08-17T09:00:00Z"
}
```

### GET /api/v1/compliance/reports

列出当前租户的合规报告。

- **认证**：需 `compliance:read` 权限
- **响应**：`200 OK`，`{"reports": [...]}`（元素同上扫描响应）

### GET /api/v1/compliance/reports/{id}

报告详情。

- **认证**：需 `compliance:read` 权限
- **响应**：`200 OK`，返回 `ComplianceReport`
- 不存在：`404`，`{"error": "report not found"}`

---

## HA API

Phase 3 控制面高可用管理。基于现有 LeaderStore（leader_lease 表续租）查询 leader 状态。
MVP 限制：非 leader 实例无法得知 leader 详情（返回占位 `unknown`）；实例列表仅含当前实例
（多副本需经 leader_lease 表查询全部活跃实例）。

### GET /api/v1/ha/status

获取 HA 状态（leader / 当前实例 / 实例列表 / 副本数）。

- **认证**：需 `ha:read` 权限
- **响应**：`200 OK`

```json
{
  "leader": {"instanceId": "host-1", "hostname": "host-1", "httpPort": 8080, "grpcPort": 9090, "role": "leader", "isLeader": true},
  "current": {"instanceId": "host-1", "hostname": "host-1", "httpPort": 8080, "grpcPort": 9090, "role": "leader", "isLeader": true},
  "instances": [{"instanceId": "host-1", "role": "leader", "isLeader": true}],
  "replicas": 3,
  "generatedAt": "2026-08-17T09:00:00Z"
}
```

- `replicas` 来自 `--replicas` 配置（内存 store 不支持多副本）
- 非 leader 实例查询时 `leader` 为占位：`{"instanceId": "unknown", "hostname": "unknown", "role": "leader", "isLeader": true}`

### GET /api/v1/ha/instances

列出控制面实例（MVP 仅返回当前实例）。

- **认证**：需 `ha:read` 权限
- **响应**：`200 OK`

```json
{
  "instances": [{"instanceId": "host-1", "hostname": "host-1", "httpPort": 8080, "grpcPort": 9090, "role": "leader", "isLeader": true}],
  "count": 1
}
```

### GET /api/v1/ha/health

实例健康检查（当前实例状态 + leader 状态，供负载均衡/监控探针使用）。

- **响应**：`200 OK`

```json
{
  "status": "healthy",
  "instance": {"instanceId": "host-1", "role": "leader", "isLeader": true},
  "timestamp": "2026-08-17T09:00:00Z"
}
```

### POST /api/v1/ha/failover

手动切换 leader。返回当前实例角色与说明：实际选主由 leader_lease 表续租驱动，
手动切换需运维摘掉当前 leader Pod 触发重新选举（本端点不直接执行切换）。

- **认证**：需 `ha:write` 权限
- **请求体**：无
- **响应**：`200 OK`

```json
{
  "status": "accepted",
  "message": "failover triggered; new leader will be elected via leader_lease renewal",
  "current": {"instanceId": "host-1", "role": "leader", "isLeader": true},
  "simulated": false
}
```

> `simulated: false` 表示响应返回的是真实当前状态（旧版本的占位 `simulated:true` 标记已移除）。

---

## 工单与 SLO API

Phase 1 服务台：工单管理 + SLO 管理，均按租户隔离（`X-Tenant-ID` 必填）。

### GET /api/v1/tickets

列出工单（支持过滤参数）。

- **认证**：需 `ticket:read` 权限
- **查询参数**：`status`（open/in_progress/resolved/closed）、`priority`（low/medium/high/urgent）、
  `category`（incident/change/request/problem）、`assigneeID`（空串表示不过滤）
- **响应**：`200 OK`

```json
{
  "tickets": [
    {
      "id": "tk-001",
      "tenantID": "t1",
      "title": "CPU 告警处理",
      "description": "host-1 CPU 持续 90%+",
      "status": "open",
      "priority": "high",
      "category": "incident",
      "assigneeID": "u-002",
      "creatorID": "u-001",
      "relatedDevice": "d-001",
      "relatedTask": "",
      "tags": ["cpu"],
      "createdAt": "2026-08-17T09:00:00Z",
      "updatedAt": "2026-08-17T09:00:00Z"
    }
  ]
}
```

### POST /api/v1/tickets

创建工单。

- **认证**：需 `ticket:write` 权限
- **请求体**：

```json
{
  "title": "CPU 告警处理",
  "description": "host-1 CPU 持续 90%+",
  "priority": "high",
  "category": "incident",
  "assigneeID": "u-002",
  "relatedDevice": "d-001",
  "relatedTask": "",
  "tags": ["cpu"]
}
```

- `title`：必填；`creatorID` 缺省填充为当前调用者（防伪造创建人）
- **响应**：`201 Created`，返回完整 `Ticket`

### GET /api/v1/tickets/{id}

工单详情。

- **认证**：需 `ticket:read` 权限
- **响应**：`200 OK`，返回 `Ticket`
- 不存在：`404`，`{"error": "ticket not found"}`

### PUT /api/v1/tickets/{id}

更新工单（含 `status` 字段，可推进 open → in_progress → resolved）。

- **认证**：需 `ticket:write` 权限
- **请求体**：同创建接口附加 `status` 字段
- **响应**：`200 OK`，返回更新后的 `Ticket`
- 不存在：`404`

### POST /api/v1/tickets/{id}/close

关闭工单（置 `closed`）。

- **认证**：需 `ticket:write` 权限
- **请求体**：无
- **响应**：`200 OK`，返回关闭后的 `Ticket`
- 不存在：`404`

### GET /api/v1/slos

列出 SLO。

- **认证**：需 `slo:read` 权限
- **响应**：`200 OK`

```json
{
  "slos": [
    {
      "id": "slo-001",
      "tenantID": "t1",
      "name": "api 可用性",
      "description": "核心 API 月度可用性",
      "serviceName": "api-gateway",
      "target": 99.9,
      "window": "30d",
      "slis": [{"name": "availability", "metric": "up", "target": 0.999, "operator": ">="}],
      "createdAt": "2026-08-17T09:00:00Z",
      "updatedAt": "2026-08-17T09:00:00Z"
    }
  ]
}
```

### POST /api/v1/slos

创建 SLO。

- **认证**：需 `slo:write` 权限
- **请求体**：

```json
{
  "name": "api 可用性",
  "description": "核心 API 月度可用性",
  "serviceName": "api-gateway",
  "target": 99.9,
  "window": "30d",
  "slis": [{"name": "availability", "metric": "up", "target": 0.999, "operator": ">="}]
}
```

- `name`：必填；`target` 如 99.9 表示 99.9%；`window` 如 `30d` / `7d`
- **响应**：`201 Created`，返回完整 `SLO`

### GET /api/v1/slos/{id}

SLO 详情。

- **认证**：需 `slo:read` 权限
- **响应**：`200 OK`，返回 `SLO`
- 不存在：`404`，`{"error": "slo not found"}`

### PUT /api/v1/slos/{id}

更新 SLO。

- **认证**：需 `slo:write` 权限
- **请求体**：同创建接口
- **响应**：`200 OK`，返回更新后的 `SLO`
- 不存在：`404`

### DELETE /api/v1/slos/{id}

删除 SLO。

- **认证**：需 `slo:delete` 权限（注意与其他端点不同，删除单独使用 `slo:delete`）
- **响应**：`204 No Content`（无响应体）
- 不存在：`404`

### GET /api/v1/slos/{id}/status

获取 SLO 各 SLI 当前状态。

- **认证**：需 `slo:read` 权限
- **响应**：`200 OK`

```json
{
  "statuses": [
    {"sliName": "availability", "currentValue": 0.9995, "targetValue": 0.999, "status": "met", "lastEvaluated": "2026-08-17T09:00:00Z"}
  ]
}
```

- `status`：`met` | `breached` | `nodata`
- SLO 不存在：`404`，`{"error": "slo not found"}`

---

## 流量治理 API

Phase 2 微服务治理：流量策略 CRUD + 启停（canary / timeout / retry / circuit_breaker / mirror），
按租户隔离。

### GET /api/v1/traffic/policies

列出流量策略。

- **认证**：需 `traffic:read` 权限
- **响应**：`200 OK`

```json
{
  "policies": [
    {
      "id": "tp-001",
      "tenantID": "t1",
      "name": "api-canary",
      "serviceName": "api-gateway",
      "type": "canary",
      "canaryWeights": {"v1": 90, "v2": 10},
      "mirrorPercent": 0,
      "timeout": "",
      "retries": 0,
      "retryTimeout": "",
      "maxConns": 0,
      "maxRequests": 0,
      "status": "active",
      "createdAt": "2026-08-17T09:00:00Z",
      "updatedAt": "2026-08-17T09:00:00Z"
    }
  ]
}
```

### POST /api/v1/traffic/policies

创建流量策略。

- **认证**：需 `traffic:write` 权限
- **请求体**：`TrafficPolicy`（`name` 必填）

```json
{
  "name": "api-canary",
  "serviceName": "api-gateway",
  "type": "canary",
  "canaryWeights": {"v1": 90, "v2": 10}
}
```

- `type`：`canary` | `timeout` | `retry` | `circuit_breaker` | `mirror`
- **响应**：`201 Created`，返回完整 `TrafficPolicy`

### GET /api/v1/traffic/policies/{id}

策略详情。

- **认证**：需 `traffic:read` 权限
- **响应**：`200 OK`，返回 `TrafficPolicy`
- 不存在：`404`，`{"error": "policy not found"}`

### PUT /api/v1/traffic/policies/{id}

更新策略（`id` 取路径参数）。

- **认证**：需 `traffic:write` 权限
- **请求体**：完整 `TrafficPolicy`
- **响应**：`200 OK`，返回更新后的策略
- 不存在：`404`

### DELETE /api/v1/traffic/policies/{id}

删除策略。

- **认证**：需 `traffic:write` 权限
- **响应**：`200 OK`，`{"status": "deleted"}`
- 不存在：`404`

### POST /api/v1/traffic/policies/{id}/enable

启用策略（`status` → `active`）。

- **认证**：需 `traffic:write` 权限
- **请求体**：无
- **响应**：`200 OK`，返回更新后的策略
- 不存在：`404`

### POST /api/v1/traffic/policies/{id}/disable

禁用策略（`status` → `inactive`）。

- **认证**：需 `traffic:write` 权限
- **请求体**：无
- **响应**：`200 OK`，返回更新后的策略
- 不存在：`404`

---

## 流水线与 ArgoCD API

Phase 2 CI/CD：流水线模板 CRUD + 触发运行 + 运行记录查询；ArgoCD 应用 CRUD + 同步。

### GET /api/v1/pipeline/templates

列出流水线模板。

- **认证**：需 `pipeline:read` 权限
- **响应**：`200 OK`

```json
{
  "templates": [
    {
      "id": "tpl-001",
      "tenantID": "t1",
      "name": "build-and-deploy",
      "description": "构建并部署",
      "type": "tekton",
      "yaml": "steps:\n  - name: build\n    command: make build",
      "agentID": "a-001",
      "parameters": [{"name": "env", "description": "目标环境", "default": "dev", "required": true}],
      "createdAt": "2026-08-17T09:00:00Z",
      "updatedAt": "2026-08-17T09:00:00Z"
    }
  ]
}
```

### POST /api/v1/pipeline/templates

创建流水线模板。

- **认证**：需 `pipeline:write` 权限
- **请求体**：`PipelineTemplate`（`name` 必填）
- **响应**：`201 Created`，返回完整模板
- 说明：`agentID` 为执行该流水线的默认 agent，为空时 run 会推进失败（记录 `template agentID not set`）

### GET /api/v1/pipeline/templates/{id}

模板详情。

- **认证**：需 `pipeline:read` 权限
- **响应**：`200 OK`，返回 `PipelineTemplate`
- 不存在：`404`，`{"error": "template not found"}`

### PUT /api/v1/pipeline/templates/{id}

更新模板（`id` / `tenantID` / `createdAt` 保留原值；实现为 Delete + Create，保留原 ID）。

- **认证**：需 `pipeline:write` 权限
- **请求体**：完整 `PipelineTemplate`
- **响应**：`200 OK`，返回更新后的模板
- 不存在：`404`

### DELETE /api/v1/pipeline/templates/{id}

删除模板。

- **认证**：需 `pipeline:write` 权限
- **响应**：`200 OK`，`{"status": "deleted"}`
- 不存在：`404`

### POST /api/v1/pipeline/templates/{id}/run

触发运行：创建 `pending` 状态的 run 记录，由后台 pipeline 执行器（10s 周期）推进为
`running` 并下发执行任务（从模板 YAML 提取首行非注释命令构造 shell task，`ParentID`
关联到 run 供状态对账）。

- **认证**：需 `pipeline:write` 权限
- **请求体**（可选，可无参数触发）：

```json
{"parameters": {"env": "prod"}}
```

- **响应**：`201 Created`

```json
{
  "id": "run-001",
  "tenantID": "t1",
  "templateID": "tpl-001",
  "templateName": "build-and-deploy",
  "status": "pending",
  "parameters": {"env": "prod"},
  "startedAt": "2026-08-17T09:00:00Z",
  "createdAt": "2026-08-17T09:00:00Z"
}
```

- 模板不存在：`404`

### GET /api/v1/pipeline/runs

列出运行记录（查询时按子任务状态对账，派生 run.Status）。

- **认证**：需 `pipeline:read` 权限
- **查询参数**：`templateID`（按模板过滤）
- **响应**：`200 OK`

```json
{
  "runs": [
    {"id": "run-001", "templateID": "tpl-001", "templateName": "build-and-deploy", "status": "succeeded", "logs": "execution task created: t-001", "createdAt": "2026-08-17T09:00:00Z"}
  ]
}
```

- `status` 对账规则：任一子任务 failed/cancelled → `failed`；全部 done → `succeeded`；否则 `running`

### GET /api/v1/pipeline/runs/{id}

运行详情（同上按子任务状态对账派生 status）。

- **认证**：需 `pipeline:read` 权限
- **响应**：`200 OK`，返回 `PipelineRun`
- 不存在：`404`，`{"error": "run not found"}`

### GET /api/v1/argocd/apps

列出 ArgoCD 应用。

- **认证**：需 `argocd:read` 权限
- **响应**：`200 OK`

```json
{
  "apps": [
    {
      "id": "app-001",
      "tenantID": "t1",
      "name": "my-app",
      "namespace": "default",
      "repoURL": "https://github.com/org/manifests",
      "path": "overlays/prod",
      "targetRevision": "main",
      "clusterURL": "https://k8s.example.com:6443",
      "syncPolicy": "manual",
      "status": "synced",
      "healthStatus": "healthy",
      "createdAt": "2026-08-17T09:00:00Z",
      "updatedAt": "2026-08-17T09:00:00Z"
    }
  ]
}
```

### POST /api/v1/argocd/apps

创建 ArgoCD 应用。

- **认证**：需 `argocd:write` 权限
- **请求体**：`ArgoCDApp`（`name` 必填）
- **响应**：`201 Created`，返回完整 `ArgoCDApp`

### GET /api/v1/argocd/apps/{id}

应用详情。

- **认证**：需 `argocd:read` 权限
- **响应**：`200 OK`，返回 `ArgoCDApp`
- 不存在：`404`，`{"error": "app not found"}`

### PUT /api/v1/argocd/apps/{id}

更新应用（`id` 取路径参数）。

- **认证**：需 `argocd:write` 权限
- **请求体**：完整 `ArgoCDApp`
- **响应**：`200 OK`，返回更新后的应用
- 不存在：`404`

### DELETE /api/v1/argocd/apps/{id}

删除应用。

- **认证**：需 `argocd:write` 权限
- **响应**：`200 OK`，`{"status": "deleted"}`
- 不存在：`404`

### POST /api/v1/argocd/apps/{id}/sync

同步应用（真实执行：调用 `argocd` CLI 执行 `app sync`，60 秒超时；
带 `namespace` / `targetRevision` 参数）。

- **认证**：需 `argocd:write` 权限
- **请求体**：无
- **响应**：`200 OK`，返回同步后的 `ArgoCDApp`（`status=synced`）
- 同步失败：`500`，`{"error": "argocd sync failed: ..."}`（应用状态置 `outofsync`）
- 应用不存在：`404`

### POST /api/v1/canary/{id}/traffic-split

设置灰度发布流量分割百分比（针对经 `POST /api/v1/tasks/canary` 创建的灰度发布）。

- **认证**：需 `task:write` 权限
- **请求体**：`{"percentage": 30}`（0-100 整数，表示灰度版本流量占比）
- **响应**：`200 OK`

```json
{
  "canaryID": "canary-1a2b3c4d5e6f",
  "percentage": 30,
  "updatedAt": "2026-08-17T09:00:00Z"
}
```

- 灰度发布不存在或跨租户：`404`，`{"error": "canary release not found"}`

### GET /api/v1/canary/{id}/metrics

获取灰度指标对比（真实指标：从 network_metrics 表查询最近 5 分钟均值）。

- **认证**：需 `task:read` 权限
- **响应**：`200 OK`

```json
{
  "canaryID": "canary-1a2b3c4d5e6f",
  "baseline": [...],
  "canary": [...],
  "percentage": 30,
  "comparedAt": "2026-08-17T09:00:00Z",
  "simulated": false
}
```

> `simulated: false` 表示返回的是真实查询指标（旧版本的模拟数据占位 `simulated:true` 已替换为真实实现）。
- 灰度发布不存在或跨租户：`404`

---

## 配置热推送 API

Phase 2 配置管理：热推送 / 灰度配置发布 / 版本历史（复用 `cmdb:read` / `cmdb:write` 权限）。

### POST /api/v1/config/hotpush

热推送配置到指定设备。先 `SetConfig` 保存配置版本，再下发 `file` 类型任务写配置文件到目标路径。

- **认证**：需 `cmdb:write` 权限
- **请求体**：

```json
{
  "agentID": "a-001",
  "key": "app/config",
  "value": "log_level=info\n",
  "path": "/etc/app/config.ini",
  "format": "text",
  "description": "应用配置"
}
```

- `agentID`、`key`、`path`：必填；`format` 缺省 `text`
- **响应**：`200 OK`

```json
{
  "configKey": "app/config",
  "configVersion": 3,
  "taskID": "t-001",
  "agentID": "a-001",
  "status": "pushed"
}
```

### POST /api/v1/config/canary

灰度配置发布：保存配置版本后向指定设备列表批量下发 `file` 类型任务。

- **认证**：需 `cmdb:write` 权限
- **请求体**：

```json
{
  "agentIDs": ["a-001", "a-002"],
  "key": "app/config",
  "value": "log_level=debug\n",
  "path": "/etc/app/config.ini",
  "format": "text",
  "percentage": 50
}
```

- `agentIDs`（非空）、`key`、`path`：必填；`percentage`：0-100
- **响应**：`200 OK`

```json
{
  "configKey": "app/config",
  "configVersion": 4,
  "percentage": 50,
  "tasks": [
    {"agentID": "a-001", "taskID": "t-001"},
    {"agentID": "a-002", "taskID": "t-002"}
  ],
  "status": "canary_pushed"
}
```

### GET /api/v1/config/versions

查询配置版本历史。

- **认证**：需 `cmdb:read` 权限
- **查询参数**：`key`（必填）
- **响应**：`200 OK`

```json
{
  "key": "app/config",
  "versions": [...]
}
```

- 缺 `key`：`400`，`{"error": "key query parameter is required"}`

---

## 自动化 API

Phase 4 自动化闭环：规则 CRUD + 启停 + 测试 + 执行历史。规则为「触发器 + 动作列表」
（TriggerType: alert/metric_threshold/schedule/event；ActionType:
execute_task/send_notify/scale/restart/isolate），由后台评估循环（`automationEvalInterval`）
周期评估 enabled 规则并执行命中动作。

### GET /api/v1/automation/rules

列出自动化规则。

- **认证**：需 `automation:read` 权限
- **响应**：`200 OK`

```json
{
  "rules": [
    {
      "id": "ar-001",
      "tenantID": "t1",
      "name": "CPU 高自动重启",
      "description": "CPU > 90% 时重启服务",
      "triggerType": "metric_threshold",
      "triggerParams": {"metric": "cpu", "op": ">", "threshold": "90"},
      "actions": [{"type": "restart", "params": {"target": "nginx"}}],
      "enabled": true,
      "createdAt": "2026-08-17T09:00:00Z",
      "updatedAt": "2026-08-17T09:00:00Z"
    }
  ]
}
```

### POST /api/v1/automation/rules

创建自动化规则（经 `automation.ValidateRule` 校验合法性）。

- **认证**：需 `automation:write` 权限
- **请求体**：`AutomationRule`（同上结构）
- **响应**：`201 Created`，返回完整规则
- 规则校验失败：`400`，`{"error": "<校验错误详情>"}`

### GET /api/v1/automation/rules/{id}

规则详情。

- **认证**：需 `automation:read` 权限
- **响应**：`200 OK`，返回 `AutomationRule`
- 不存在：`404`，`{"error": "automation rule not found"}`

### PUT /api/v1/automation/rules/{id}

更新规则（`id` 取路径参数，更新前同样经 `ValidateRule` 校验）。

- **认证**：需 `automation:write` 权限
- **请求体**：完整 `AutomationRule`
- **响应**：`200 OK`，返回更新后的规则
- 校验失败：`400`；不存在：`404`

### DELETE /api/v1/automation/rules/{id}

删除规则。

- **认证**：需 `automation:write` 权限
- **响应**：`200 OK`，`{"status": "deleted"}`
- 不存在：`404`

### POST /api/v1/automation/rules/{id}/enable

启用规则。

- **认证**：需 `automation:write` 权限
- **请求体**：无
- **响应**：`200 OK`，返回 `enabled=true` 的规则
- 不存在：`404`

### POST /api/v1/automation/rules/{id}/disable

禁用规则。

- **认证**：需 `automation:write` 权限
- **请求体**：无
- **响应**：`200 OK`，返回 `enabled=false` 的规则
- 不存在：`404`

### POST /api/v1/automation/rules/{id}/test

测试规则（dry-run：经引擎 `TestRule` 执行，不实际下发动作），执行记录落库。

- **认证**：需 `automation:write` 权限
- **请求体**：无
- **响应**：`200 OK`

```json
{
  "execution": {"id": "exec-001", "tenantID": "t1", "ruleID": "ar-001", "ruleName": "CPU 高自动重启", "status": "succeeded", "detail": "...", "startedAt": "2026-08-17T09:00:00Z"},
  "triggered": true
}
```

- 规则不存在：`404`

### GET /api/v1/automation/executions

执行历史（最近 100 条）。

- **认证**：需 `automation:read` 权限
- **响应**：`200 OK`，`{"executions": [...]}`（元素为 `AutomationExecution`）

### GET /api/v1/automation/executions/{id}

执行详情。

- **认证**：需 `automation:read` 权限
- **响应**：`200 OK`，返回 `AutomationExecution`
- 不存在：`404`，`{"error": "execution not found"}`

---

## Webhook 与脚本 API

Phase 5 扩展能力：Webhook 管理（CRUD + 测试投递 + 投递记录）与自定义脚本
（CRUD + 执行 + 执行记录），均按租户隔离。

### GET /api/v1/webhooks

列出 Webhook。

- **认证**：需 `webhook:read` 权限
- **响应**：`200 OK`

```json
{
  "webhooks": [
    {
      "id": "wh-001",
      "tenantID": "t1",
      "name": "告警推送",
      "url": "https://hooks.example.com/alert",
      "events": ["alert.created"],
      "headers": {"X-Token": "xxx"},
      "bodyTemplate": "{\"title\": \"{{.Title}}\"}",
      "enabled": true,
      "retryCount": 3,
      "retryIntervalSec": 30,
      "createdAt": "2026-08-17T09:00:00Z",
      "updatedAt": "2026-08-17T09:00:00Z"
    }
  ]
}
```

### POST /api/v1/webhooks

创建 Webhook。

- **认证**：需 `webhook:write` 权限
- **请求体**：`Webhook`（`name`、`url` 必填）
- SSRF 校验：`url` 复用 `ValidateWebhookURL`（协议白名单 + 私网/loopback/链路本地/云元数据
  拦截 + DNS rebinding 防护；`--webhook-allow-private` 显式放行私网）
- **响应**：`201 Created`，返回完整 `Webhook`
- SSRF 校验失败：`400`，`{"error": "invalid webhook url: ..."}`

### GET /api/v1/webhooks/{id}

Webhook 详情。

- **认证**：需 `webhook:read` 权限
- **响应**：`200 OK`，返回 `Webhook`
- 不存在：`404`，`{"error": "webhook not found"}`

### PUT /api/v1/webhooks/{id}

更新 Webhook（`url` 非空时同样过 SSRF 校验，防止经 PUT 绕过创建期防护）。

- **认证**：需 `webhook:write` 权限
- **请求体**：完整 `Webhook`
- **响应**：`200 OK`，返回更新后的 `Webhook`
- 不存在：`404`

### DELETE /api/v1/webhooks/{id}

删除 Webhook。

- **认证**：需 `webhook:write` 权限
- **响应**：`200 OK`，`{"status": "deleted"}`
- 不存在：`404`

### POST /api/v1/webhooks/{id}/test

测试投递：真实 HTTP POST 到 webhook URL（10 秒超时，记录真实响应）。
事件为 `test.event`，payload 为 `{"event":"test.event","message":"webhook test delivery"}`。

- **认证**：需 `webhook:write` 权限
- **请求体**：无
- **响应**：`200 OK`，返回本次投递记录（含真实 `statusCode` / `response`）
- 投递失败：`502 Bad Gateway`，`{"webhookID": "...", "event": "test.event", "error": "delivery failed: ..."}`
- Webhook 不存在：`404`

### GET /api/v1/webhooks/{id}/deliveries

投递记录（每次推送含重试产生一条记录）。

- **认证**：需 `webhook:read` 权限
- **响应**：`200 OK`

```json
{
  "deliveries": [
    {"id": "dlv-001", "tenantID": "t1", "webhookID": "wh-001", "event": "test.event", "payload": "...", "statusCode": 200, "response": "ok", "error": "", "deliveredAt": "2026-08-17T09:00:00Z"}
  ]
}
```

### GET /api/v1/scripts

列出脚本。

- **认证**：需 `script:read` 权限
- **响应**：`200 OK`

```json
{
  "scripts": [
    {
      "id": "sc-001",
      "tenantID": "t1",
      "name": "清理日志",
      "language": "shell",
      "content": "find /var/log -name '*.gz' -mtime +30 -delete",
      "params": "",
      "timeoutSec": 60,
      "enabled": true,
      "createdAt": "2026-08-17T09:00:00Z",
      "updatedAt": "2026-08-17T09:00:00Z"
    }
  ]
}
```

### POST /api/v1/scripts

创建脚本。

- **认证**：需 `script:write` 权限
- **请求体**：

```json
{
  "name": "清理日志",
  "language": "shell",
  "content": "find /var/log -name '*.gz' -mtime +30 -delete",
  "params": "",
  "timeoutSec": 60
}
```

- `name`、`content`：必填；`language`：`shell` | `python`（空则不限）
- `timeoutSec`：clamp 至 [1, 600] 秒（<1 视为 1，>600 截断为 600）
- 新建脚本默认 `enabled=true`（禁用需显式 PUT `enabled=false`）
- **响应**：`201 Created`，返回完整 `Script`

### GET /api/v1/scripts/{id}

脚本详情。

- **认证**：需 `script:read` 权限
- **响应**：`200 OK`，返回 `Script`
- 不存在：`404`，`{"error": "script not found"}`

### PUT /api/v1/scripts/{id}

更新脚本（`id` 取路径参数，`timeoutSec` 同样 clamp 至 [1, 600]）。

- **认证**：需 `script:write` 权限
- **请求体**：完整 `Script`
- **响应**：`200 OK`，返回更新后的 `Script`
- 不存在：`404`

### DELETE /api/v1/scripts/{id}

删除脚本。

- **认证**：需 `script:write` 权限
- **响应**：`200 OK`，`{"status": "deleted"}`
- 不存在：`404`

### POST /api/v1/scripts/{id}/execute

执行脚本（真实执行：创建 `type=shell` 任务下发到指定 agent，同时记录 `pending` 状态
的 ScriptExecution；agent 执行完毕后回写）。

- **认证**：需 `script:write` 权限
- **请求体**：

```json
{
  "deviceID": "d-001",
  "params": ""
}
```

- `deviceID`：必填
- **响应**：`202 Accepted`

```json
{
  "executionID": "exec-001",
  "taskID": "t-001",
  "scriptID": "sc-001",
  "deviceID": "d-001",
  "status": "pending",
  "message": "script execution task created, waiting for agent to execute",
  "startedAt": "2026-08-17T09:00:00Z"
}
```

- 脚本已禁用：`409`，`{"error": "script is disabled, enable it before execution"}`
- 脚本不存在：`404`；缺 `deviceID`：`400`

### GET /api/v1/scripts/{id}/executions

脚本执行记录。

- **认证**：需 `script:read` 权限
- **响应**：`200 OK`

```json
{
  "executions": [
    {"id": "exec-001", "tenantID": "t1", "scriptID": "sc-001", "deviceID": "d-001", "status": "succeeded", "stdout": "...", "stderr": "", "startedAt": "2026-08-17T09:00:00Z", "finishedAt": "2026-08-17T09:00:05Z"}
  ]
}
```

---

## 网络设备 API

Phase 4 网络设备管理：网络设备（switch/router/firewall/load_balancer，经 SNMP/CLI
管理而非 agent）CRUD + 监控指标 + 配置下发 + 子网发现。与「网络拓扑与诊断 API」
（`/api/v1/network/topology` 等）互补。

### GET /api/v1/network/devices

列出网络设备。

- **认证**：需 `network:read` 权限
- **响应**：`200 OK`

```json
{
  "devices": [
    {
      "id": "nd-001",
      "tenantID": "t1",
      "name": "core-switch-1",
      "type": "switch",
      "vendor": "cisco",
      "model": "C9300",
      "ip": "10.30.0.1",
      "mask": "255.255.255.0",
      "mac": "aa:bb:cc:dd:ee:ff",
      "location": "机房 A",
      "snmpCommunity": "public",
      "status": "up",
      "createdAt": "2026-08-17T09:00:00Z",
      "updatedAt": "2026-08-17T09:00:00Z"
    }
  ]
}
```

### POST /api/v1/network/devices

添加网络设备。

- **认证**：需 `network:write` 权限
- **请求体**：`NetworkDevice`（`name` 必填）

```json
{
  "name": "core-switch-1",
  "type": "switch",
  "vendor": "cisco",
  "model": "C9300",
  "ip": "10.30.0.1",
  "snmpCommunity": "public"
}
```

- `type` 非空时须为 `switch` | `router` | `firewall` | `load_balancer`
- **响应**：`201 Created`，返回完整 `NetworkDevice`

### GET /api/v1/network/devices/{id}

设备详情。

- **认证**：需 `network:read` 权限
- **响应**：`200 OK`，返回 `NetworkDevice`
- 不存在：`404`，`{"error": "network device not found"}`

### DELETE /api/v1/network/devices/{id}

删除设备。

- **认证**：需 `network:write` 权限
- **响应**：`200 OK`，`{"status": "deleted"}`
- 不存在：`404`

### GET /api/v1/network/devices/{id}/metrics

设备监控指标（SNMP 采集的 CPU / 内存 / 温度 / uptime）。

- **认证**：需 `network:read` 权限
- **响应**：`200 OK`

```json
{
  "deviceID": "nd-001",
  "metrics": [
    {"deviceID": "nd-001", "tenantID": "t1", "timestamp": "2026-08-17T09:00:00Z", "cpuUsage": 12.5, "memoryUsage": 45.0, "temperature": 38.2, "uptime": 86400}
  ]
}
```

- 设备不存在：`404`

### POST /api/v1/network/devices/{id}/config

下发网络配置（配置文本经 `network.ValidateConfig` 校验后写入设备 `config` 字段）。

- **认证**：需 `network:write` 权限
- **请求体**：

```json
{"config": "interface GigabitEthernet0/1\n description uplink"}
```

- **响应**：`200 OK`，返回更新后的 `NetworkDevice`
- 配置校验失败：`400`；设备不存在：`404`

### POST /api/v1/network/discover

网络发现：对指定子网做 TCP Connect 扫描（端口 22/80/443/3306/6379/8080/9090，
500ms/地址超时，单次最多 254 地址）发现存活设备。

- **认证**：需 `network:write` 权限
- **请求体**：`{"subnet": "192.168.1.0/24"}`
- **响应**：`200 OK`

```json
{
  "subnet": "192.168.1.0/24",
  "devices": [
    {"id": "", "name": "192.168.1.1", "type": "router", "ip": "192.168.1.1", "status": "up"}
  ],
  "scanned": 254,
  "found": 1
}
```

- 缺 `subnet` / CIDR 非法：`400`，`{"error": "subnet is required"}` / `{"error": "invalid CIDR: ..."}`

---

## 审计扩展 API

Phase 3 审计查询：事件检索与导出（与 `GET /api/v1/audits` 互补，按租户隔离 + 更细过滤）。

### GET /api/v1/audit/events

查询审计事件（支持 action/user/from/to/limit 过滤）。

- **认证**：需 `audit:read` 权限
- **查询参数**：
  - `action` — 按动作过滤（空=不限）
  - `user` — 按用户过滤（内存过滤，匹配 UserID）
  - `from` / `to` — 起止时间（RFC3339，空=不限）
  - `limit` — 返回上限（默认 100，上限 1000）
- **响应**：`200 OK`

```json
{
  "events": [
    {
      "id": "au-001",
      "tenantID": "t1",
      "userID": "u-001",
      "action": "ticket_create",
      "target": "tk-001",
      "detail": "title=CPU 告警处理",
      "timestamp": "2026-08-17T09:00:00Z"
    }
  ],
  "count": 1
}
```

- `from` / `to` 格式非法：`400`，`{"error": "invalid 'from' time (use RFC3339): ..."}`

### GET /api/v1/audit/export

导出审计日志（纯 JSON 数组，非 `{events:[]}` 包装，便于外部工具直接消费）。
查询参数同 `/api/v1/audit/events`（不含 `user`），`limit` 默认 1000、上限 10000。

- **认证**：需 `audit:read` 权限
- **响应**：`200 OK`

```json
[
  {"id": "au-001", "tenantID": "t1", "userID": "u-001", "action": "ticket_create", "target": "tk-001", "timestamp": "2026-08-17T09:00:00Z"}
]
```

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