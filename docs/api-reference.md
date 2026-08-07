# OpsMesh API 参考文档

本文档完整描述 OpsMesh 控制面 HTTP REST API（端口 8080）与 gRPC API（端口 9090）。

- **Base URL**：`http://<controlplane-host>:8080`
- **API 版本**：`v1`（路径前缀 `/api/v1`）
- **内容类型**：`application/json`（除 SSE 流为 `text/event-stream`、二进制下载外）
- **请求体上限**：1 MiB（`http.MaxBytesReader`，超出返回 413）
- **租户隔离**：所有业务接口通过 `X-Tenant-ID` 头做行级隔离；`--require-auth=true` 时缺失该头返回 401
- **认证**：`Authorization: Bearer <jwt>` 或 HttpOnly Cookie `at`（access token）；管理类接口需对应角色

## 目录

- [通用约定](#通用约定)
- [基础端点](#基础端点)
- [认证 API](#认证-api)
- [用户管理 API](#用户管理-api)
- [角色与权限 API](#角色与权限-api)
- [设备 API](#设备-api)
- [Agent API](#agent-api)
- [任务 API](#任务-api)
- [告警 API](#告警-api)
- [部署 API](#部署-api)
- [作业编排 API](#作业编排-api)
- [CMDB API](#cmdb-api)
- [OS 优化 API](#os-优化-api)
- [中间件部署 API](#中间件部署-api)
- [K8s 管理 API](#k8s-管理-api)
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

健康检查（K8s liveness/readiness 探针）。

- **响应**：`200 OK`，体 `ok`

### GET /metrics

Prometheus 文本格式指标。受 `--metrics-allow-cidr` 白名单控制。

- **响应**：`200 OK`，`Content-Type: text/plain; version=0.0.4`

### GET /api/v1/me

返回当前请求身份信息（解析 `X-Tenant-ID` / `X-User-Id` / `X-User-Roles` 头）。

- **响应示例**：

```json
{
  "tenant_id": "t1",
  "user_id": "u-001",
  "roles": ["admin", "ops"]
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

修改当前用户密码（安全债 85：预置弱口令强制改密）。

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

**B1 纳管**：签发一次性 install token（15 分钟有效），返回 bootstrap 安装命令。

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

告警规则列表（B1 修复 9）。

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

以下端点经指定集群代理操作 K8s 资源：

- `GET /api/v1/k8s/clusters/{id}/pods?namespace=xxx` — Pod 列表
- `GET /api/v1/k8s/clusters/{id}/pods/{name}/logs?namespace=xxx&container=&tailLines=` — Pod 日志
- `DELETE /api/v1/k8s/clusters/{id}/pods/{name}?namespace=xxx` — 删除 Pod
- `GET /api/v1/k8s/clusters/{id}/deployments?namespace=xxx` — Deployment 列表
- `GET /api/v1/k8s/clusters/{id}/deployments/{name}?namespace=xxx` — Deployment 详情
- `GET /api/v1/k8s/clusters/{id}/services?namespace=xxx` — Service 列表
- `GET /api/v1/k8s/clusters/{id}/configmaps?namespace=xxx` — ConfigMap 列表

---

## 审计 API

### GET /api/v1/audits

审计事件检索（P0-4，100% 留痕）。

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

M4-4D 控制面联邦：仅当 `--federation-peers` 非空时注册。联邦通道硬化为 mTLS + HMAC 签名。

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

M3-2B SSE 实时推送（替代 5s 轮询）。推送设备状态变更、任务状态变更、告警产出等事件。

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

### gRPC 安全

- **TLS/mTLS**：`--tls-cert` / `--tls-key` / `--client-ca` 启用服务端证书与客户端证书校验
- **HMAC 签名**：`--grpc-require-signature=true` 时 agent 请求须携带 `agent-signature` metadata（HMAC-SHA256(agent_id + timestamp + secret)），时间戳偏差 ±5min 防重放
- **Recovery 拦截器**：`grpcRecoveryInterceptor` 捕获 handler panic，返回 `codes.Internal`，单处 panic 不拖垮整个控制面