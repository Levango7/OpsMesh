# OpsMesh 接口规范

> 本文档是 OpsMesh 控制面 HTTP REST API（端口 8080）与 gRPC API（端口 9090）的**接口契约规范**，覆盖设计原则、OpenAPI 3.0 规范骨架、错误码、认证、版本管理、分页/过滤/排序、SSE 事件、gRPC IDL、限流与幂等性约定。
>
> - 端点清单与示例响应以 [`docs/api-reference.md`](./api-reference.md) 为准；本规范侧重"契约"而非"清单"。
> - SSE 事件协议以 [`docs/sse-protocol.md`](./sse-protocol.md) 为准；本规范仅做摘要与一致性约束。
> - 代码事实以 `internal/controlplane/server_lifecycle.go`（路由注册）+ `internal/controlplane/server_middleware.go`（中间件链）+ `internal/authctx/authctx.go`（身份上下文）为单一事实源，CI 用 `sse_contract_test.go` 等守护文档与代码不漂移。

---

## 目录

- [1. 设计原则](#1-设计原则)
- [2. OpenAPI 3.0 规范](#2-openapi-30-规范)
- [3. 错误码标准](#3-错误码标准)
- [4. 认证规范](#4-认证规范)
- [5. 版本管理](#5-版本管理)
- [6. 分页/过滤/排序约定](#6-分页过滤排序约定)
- [7. SSE 事件规范](#7-sse-事件规范)
- [8. gRPC API 规范](#8-grpc-api-规范)
- [9. 限流规范](#9-限流规范)
- [10. 幂等性约定](#10-幂等性约定)

---

## 1. 设计原则

### 1.1 RESTful 约定

OpsMesh 控制面 HTTP API 遵循 REST 风格，但允许少量"动作型"端点（RPC-over-REST）以表达难以用 CRUD 建模的运维操作（取消任务、回滚部署、确认告警等）。

| 维度 | 约定 | 示例 |
|------|------|------|
| 资源命名 | 复数名词、kebab-case、嵌套层级不超过 3 层 | `/api/v1/devices`、`/api/v1/k8s/clusters/{id}/pods` |
| 动作端点 | 资源 ID 后接动词子路径，方法固定为 POST | `POST /api/v1/tasks/{id}/cancel`、`POST /api/v1/deploys/{id}/rollback` |
| 子资源 | 父资源 ID 必出现在路径段，强制租户隔离继承 | `/api/v1/users/{id}/roles` |
| 非资源端点 | 不在 `/api/v1` 前缀下，单独根路径 | `/healthz`、`/readyz`、`/install.sh`、`/bin/opsmesh-agent` |

### 1.2 HTTP 方法语义

| 方法 | 语义 | 是否幂等 | 是否安全 | 典型状态码 |
|------|------|----------|----------|------------|
| `GET` | 读资源/列表 | 是 | 是 | 200 / 404 / 429 |
| `POST` | 创建资源 / 触发动作 | 否（除非带 `Idempotency-Key`） | 否 | 201 / 200 / 400 / 409 |
| `PUT` | 全量替换资源 | 是 | 否 | 200 / 400 / 404 |
| `DELETE` | 删除资源 / 退役 | 是 | 否 | 200 / 204 / 404 |
| `PATCH` | 部分更新（当前未广泛使用，预留） | 否 | 否 | 200 / 400 |
| `HEAD`/`OPTIONS` | 探测（当前未实现，预留 CORS） | 是 | 是 | 200 |

> **方法未实现**：对已注册路径但未实现的方法，handler 内部用 `jsonError(w, 405, ...)` 返回 JSON 405，不静默 200。

### 1.3 状态码使用

OpsMesh 仅使用以下 HTTP 状态码子集，禁止引入未列入的码（避免客户端分支爆炸）：

| 码 | 语义 | 使用场景 |
|----|------|----------|
| 200 | 成功 | GET / 动作型 POST 成功 |
| 201 | 创建成功 | POST 创建资源（用户/任务/部署/工作流/CI 实例/集群等） |
| 204 | 无内容 | 当前未使用（DELETE 一律返回 200 + JSON message），预留 |
| 400 | 参数错误 | 请求体非法 JSON / 必填字段缺失 / 枚举值非法 |
| 401 | 未认证 | 缺失 `X-Tenant-ID`（require-auth=true）/ JWT 验签失败 / Cookie 失效 |
| 403 | 越权 | RBAC 角色不足 / CSRF Origin 校验失败 / `--public-register=false` 时注册 |
| 404 | 不存在 | 路径无匹配（`jsonErrorMux` 统一 JSON 化） / 资源 ID 不存在 |
| 405 | 方法不允许 | 路径存在但方法未实现 |
| 409 | 冲突 | 资源已存在（如重复创建同名 CI 类型 / 同名角色） |
| 413 | 请求体过大 | 超过 `http.MaxBytesReader` 上限 1 MiB |
| 429 | 限流 | IP 令牌桶耗尽 / 登录连续失败 5 次锁 15min / 多租户配额超限 |
| 500 | 内部错误 | handler panic（recoveryMiddleware 兜底）/ store 不可用 / 未预期错误 |
| 503 | 服务不可用 | `/healthz` Store 不可用 / `/readyz` 未持有 leader 租约 / helm CLI 未安装 / CMDB 采集未启用 |

### 1.4 资源命名约束

- **租户隔离键**：所有业务端点通过 `X-Tenant-ID` 头做行级隔离；`--require-auth=true` 时缺失返回 401。
- **ID 风格**：资源 ID 为短横线前缀 + 序号（`u-001`、`d-001`、`t-001`、`dp-001`、`wf-001`、`al-001`、`ci-001`、`k8s-001`），便于日志肉眼区分资源类型。
- **时间字段**：统一 RFC3339 UTC（如 `2026-08-07T09:00:00Z`），由后端 `time.Time` JSON 序列化产出，客户端不应自行格式化。
- **枚举值**：统一小写蛇形（`pending`/`running`/`done`/`failed`/`cancelled`、`critical`/`warning`/`info`），不混用驼峰。

---

## 2. OpenAPI 3.0 规范

### 2.1 规范骨架

下面给出 OpenAPI 3.0 YAML 骨架，包含 `info` / `servers` / `securitySchemes` / `tags`，以及 5 个代表性端点的完整定义（覆盖认证、列表分页、资源 CRUD、动作型端点、SSE 流）。

```yaml
openapi: 3.0.3
info:
  title: OpsMesh Control Plane API
  description: |
    OpsMesh 控制面 HTTP REST API。
    - 业务端口：8080，路径前缀 /api/v1
    - metrics 端口：9091（独立监听，不在本规范内）
    - gRPC 端口：9090（见第 8 章 gRPC API 规范）
    - 请求体上限：1 MiB（超出 413）
    - 租户隔离：所有业务端点通过 X-Tenant-ID 头做行级隔离
  version: 1.0.0
  contact:
    name: OpsMesh Team
    url: https://opsmesh.example.com/support
  license:
    name: Apache-2.0

servers:
  - url: http://{host}:8080
    description: 控制面业务端口
    variables:
      host:
        default: localhost
        description: 控制面主机名或 IP
  - url: https://{host}
    description: 经前置网关（APISIX/蓝鲸 IAM）的对外端点，网关注入身份头
    variables:
      host:
        default: opsmesh.example.com

security:
  - bearerAuth: []
  - cookieAuth: []
  - gatewayInjected: []

tags:
  - name: Auth          # 认证与用户中心
    description: 注册/登录/登出/refresh/改密/me
  - name: Users         # 用户管理
    description: 用户 CRUD + 角色绑定
  - name: RBAC          # 角色与权限
    description: 角色 CRUD + 权限列表
  - name: Devices       # 设备
    description: 设备清单/详情/退役/纳管
  - name: Agents        # Agent
    description: agent 清单与状态
  - name: Tasks         # 任务
    description: 任务下发/批量/取消/结果查询
  - name: Alerts        # 告警
    description: 活跃告警/确认/静默/规则
  - name: Deploys       # 部署
    description: 部署计划/fan-out/回滚
  - name: Workflows     # 作业编排
    description: DAG 工作流/触发/执行历史
  - name: CMDB          # 配置库
    description: CI 类型/实例/关系/属性模板/变更审批
  - name: OS            # OS 优化
    description: OS 优化模板与执行
  - name: Middleware    # 中间件部署
    description: 中间件模板/部署/卸载/实例查询
  - name: K8s           # K8s 管理
    description: 多集群注册/资源代理操作
  - name: Audit         # 审计
    description: 审计事件检索
  - name: Logs          # 日志
    description: 日志检索与追加
  - name: Federation    # 联邦
    description: 控制面联邦 peer/转发/聚合视图
  - name: SSE           # 事件流
    description: SSE 实时推送
  - name: Health        # 健康检查
    description: liveness/readiness 探针
```

### 2.2 安全方案

```yaml
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
      bearerFormat: JWT
      description: |
        Authorization: Bearer <jwt>，HS256（用户中心签发）或 RS256（网关 IAM 签发，内核二次验签）。
        与 cookieAuth 二选一；优先取 Authorization 头。
    cookieAuth:
      type: apiKey
      in: cookie
      name: at
      description: |
        HttpOnly Cookie `at`（access token），由 POST /api/v1/auth/login 设置。
        配合 rt Cookie 做 refresh 旋转。SameSite=Strict, Secure=true（HTTPS 部署）。
    gatewayInjected:
      type: apiKey
      in: header
      name: X-Tenant-ID
      description: |
        前置网关注入的租户隔离键。同时注入 X-User-Id / X-User-Roles。
        内核机械读取，不校验来源真实性——必须在可信网关后部署。
    apiKeyInternal:
      type: apiKey
      in: header
      name: X-Internal-Key
      description: 内部服务间调用密钥（如联邦 peer 间），配合 mTLS 使用。
    mutualTLS:
      type: mutualTLS
      description: |
        gRPC 9090 与联邦 mTLS 监听使用；--tls-cert/--tls-key/--client-ca 启用。
        HTTP 8080 当前不强制 mTLS，由前置网关终止 TLS。
```

### 2.3 通用 schemas

```yaml
  schemas:
    Error:
      type: object
      required: [error]
      properties:
        error:
          type: object
          required: [code, message]
          properties:
            code:
              type: string
              description: 业务错误码（见第 3 章）
              example: INVALID_ARGUMENT
            message:
              type: string
              description: 面向人类的错误描述
              example: "agent_id is required"
            detail:
              type: object
              description: 可选的结构化补充信息（字段级错误、冲突资源 ID 等）
              additionalProperties: true
            requestID:
              type: string
              description: 请求 ID（同 trace_id），用于关联后端日志/审计
              example: "req-9f3a2c1b"
        trace_id:
          type: string
          description: 兼容字段，与 error.requestID 等价（旧客户端）
          example: "req-9f3a2c1b"

    Pagination:
      type: object
      properties:
        page:
          type: integer
          minimum: 1
          example: 1
        pageSize:
          type: integer
          minimum: 1
          maximum: 200
          example: 20
        total:
          type: integer
          minimum: 0
          example: 100
        hasMore:
          type: boolean
          example: false

    PageEnvelope:
      type: object
      description: 传 page 参数时的分页响应信封；不传 page 时直接返回数组（向后兼容）
      properties:
        data:
          type: array
          items: {}
        total:
          type: integer
        page:
          type: integer
        pageSize:
          type: integer
        hasMore:
          type: boolean

    User:
      type: object
      properties:
        id:
          type: string
          example: "u-001"
        username:
          type: string
        email:
          type: string
          format: email
        display_name:
          type: string
        status:
          type: string
          enum: [active, pending, locked]
        roles:
          type: array
          items:
            type: string
        tenant_id:
          type: string

    Task:
      type: object
      properties:
        id:
          type: string
          example: "t-001"
        agent_id:
          type: string
        type:
          type: string
          enum: [shell, service, file]
        command:
          type: string
        status:
          type: string
          enum: [pending, running, done, failed, cancelled]
        created_at:
          type: string
          format: date-time
        started_at:
          type: string
          format: date-time
        finished_at:
          type: string
          format: date-time
        retry_count:
          type: integer
        max_retries:
          type: integer
        schedule:
          type: string
          description: 5 字段 cron 表达式，空=一次性
        timeout_sec:
          type: integer

    Device:
      type: object
      properties:
        id:
          type: string
        hostname:
          type: string
        segment:
          type: string
        agent_id:
          type: string
        state:
          type: string
          enum: [online, offline, retired]
        managed:
          type: boolean
        last_heartbeat:
          type: string
          format: date-time
        os:
          type: string
        arch:
          type: string

    Alert:
      type: object
      properties:
        id:
          type: string
        severity:
          type: string
          enum: [critical, warning, info]
        title:
          type: string
        message:
          type: string
        status:
          type: string
          enum: [active, acked, silenced]
        created_at:
          type: string
          format: date-time
        source:
          type: string

    SSEEvent:
      type: object
      description: SSE data 行 JSON 信封（见第 7 章）
      properties:
        type:
          type: string
          description: 事件类型枚举（hello/task_status/alert_new/device_online/device_offline/approval_status/schedule_status/os_template_changed/mw_template_changed/agent_logs）
        tenantID:
          type: string
          description: 事件归属租户；hello 等全局事件为空
        data:
          type: object
          description: 业务载荷，任意 JSON 对象
        traceID:
          type: string
          description: OTel trace_id（可选）
```

### 2.4 通用 responses

```yaml
  responses:
    BadRequest:
      description: 参数错误
      content:
        application/json:
          schema:
            $ref: '#/components/schemas/Error'
    Unauthorized:
      description: 未认证
      content:
        application/json:
          schema:
            $ref: '#/components/schemas/Error'
    Forbidden:
      description: 越权
      content:
        application/json:
          schema:
            $ref: '#/components/schemas/Error'
    NotFound:
      description: 不存在
      content:
        application/json:
          schema:
            $ref: '#/components/schemas/Error'
    Conflict:
      description: 冲突
      content:
        application/json:
          schema:
            $ref: '#/components/schemas/Error'
    PayloadTooLarge:
      description: 请求体超过 1 MiB 上限
      content:
        application/json:
          schema:
            $ref: '#/components/schemas/Error'
    TooManyRequests:
      description: 限流
      headers:
        Retry-After:
          schema:
            type: integer
            example: 1
      content:
        application/json:
          schema:
            $ref: '#/components/schemas/Error'
    InternalServerError:
      description: 内部错误
      content:
        application/json:
          schema:
            $ref: '#/components/schemas/Error'
    ServiceUnavailable:
      description: 服务不可用
      content:
        application/json:
          schema:
            $ref: '#/components/schemas/Error'
```

### 2.5 代表性端点（5 个完整定义）

下面 5 个端点覆盖：认证（POST /auth/login）、列表分页（GET /tasks）、资源 CRUD（POST /devices + DELETE /devices/{id}）、动作型端点（POST /tasks/{id}/cancel）、SSE 流（GET /events/stream）。

```yaml
paths:
  /api/v1/auth/login:
    post:
      tags: [Auth]
      summary: 登录签发 JWT
      description: |
        签发 JWT（HS256），同时设置 HttpOnly Cookie at（access token）与 rt（refresh token）。
        受登录限流保护：每 IP 10 突发 / 每 3s 补 1，连续失败 5 次锁 15min。
      operationId: login
      security: []   # 登录端点不需要认证
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [username, password]
              properties:
                username:
                  type: string
                password:
                  type: string
                  format: password
                device_fp:
                  type: string
                  description: 可选设备指纹；超过 deviceFPDeadline 签发的 refresh token 必须绑定
      responses:
        '200':
          description: 登录成功
          headers:
            Set-Cookie:
              schema:
                type: string
                example: "at=eyJ...; HttpOnly; Secure; SameSite=Strict"
          content:
            application/json:
              schema:
                type: object
                properties:
                  access_token:
                    type: string
                  refresh_token:
                    type: string
                  expires_in:
                    type: integer
                    example: 3600
                  user:
                    $ref: '#/components/schemas/User'
        '400':
          $ref: '#/components/responses/BadRequest'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '429':
          $ref: '#/components/responses/TooManyRequests'
        '500':
          $ref: '#/components/responses/InternalServerError'

  /api/v1/tasks:
    get:
      tags: [Tasks]
      summary: 任务列表
      operationId: listTasks
      parameters:
        - $ref: '#/components/parameters/TenantHeader'
        - in: query
          name: status
          schema:
            type: string
            enum: [pending, running, done, failed, cancelled]
        - in: query
          name: agent_id
          schema:
            type: string
        - $ref: '#/components/parameters/PageParam'
        - $ref: '#/components/parameters/PageSizeParam'
      responses:
        '200':
          description: 任务列表
          content:
            application/json:
              schema:
                oneOf:
                  - type: array
                    items:
                      $ref: '#/components/schemas/Task'
                  - $ref: '#/components/schemas/PageEnvelope'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '429':
          $ref: '#/components/responses/TooManyRequests'
    post:
      tags: [Tasks]
      summary: 下发单条任务
      operationId: createTask
      parameters:
        - $ref: '#/components/parameters/TenantHeader'
        - $ref: '#/components/parameters/IdempotencyKey'
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [agent_id, type, command]
              properties:
                agent_id:
                  type: string
                type:
                  type: string
                  enum: [shell, service, file]
                command:
                  type: string
                timeout_sec:
                  type: integer
                  default: 120
                max_retries:
                  type: integer
                  default: 3
                schedule:
                  type: string
                  description: 5 字段 cron 表达式，空=一次性
      responses:
        '201':
          description: 创建成功
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Task'
        '400':
          $ref: '#/components/responses/BadRequest'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '403':
          $ref: '#/components/responses/Forbidden'
        '413':
          $ref: '#/components/responses/PayloadTooLarge'
        '429':
          $ref: '#/components/responses/TooManyRequests'
        '500':
          $ref: '#/components/responses/InternalServerError'

  /api/v1/tasks/{id}/cancel:
    post:
      tags: [Tasks]
      summary: 取消任务
      description: pending 拦截 / running 强杀。服务端按租户隔离校验，越权取消他租户任务返回 403。
      operationId: cancelTask
      parameters:
        - $ref: '#/components/parameters/TenantHeader'
        - in: path
          name: id
          required: true
          schema:
            type: string
      responses:
        '200':
          description: 取消成功
          content:
            application/json:
              schema:
                type: object
                properties:
                  message:
                    type: string
                    example: "任务已取消"
        '401':
          $ref: '#/components/responses/Unauthorized'
        '403':
          $ref: '#/components/responses/Forbidden'
        '404':
          $ref: '#/components/responses/NotFound'
        '409':
          $ref: '#/components/responses/Conflict'
          description: 任务已处于终态（done/failed/cancelled），不可取消

  /api/v1/devices/{id}:
    delete:
      tags: [Devices]
      summary: 退役/下线设备
      description: 将设备 state 置为 retired，不物理删除（保留审计轨迹）。
      operationId: retireDevice
      parameters:
        - $ref: '#/components/parameters/TenantHeader'
        - in: path
          name: id
          required: true
          schema:
            type: string
      responses:
        '200':
          description: 退役成功
          content:
            application/json:
              schema:
                type: object
                properties:
                  message:
                    type: string
                    example: "设备已退役"
        '401':
          $ref: '#/components/responses/Unauthorized'
        '403':
          $ref: '#/components/responses/Forbidden'
        '404':
          $ref: '#/components/responses/NotFound'

  /api/v1/events/stream:
    get:
      tags: [SSE]
      summary: SSE 实时事件流
      description: |
        长连接，Content-Type: text/event-stream。
        - 握手：连接建立后立即下发 event: hello, data: {}
        - 心跳：每 15s 发送注释帧 : ping\n\n 保活
        - 不支持 Last-Event-ID 断点续传（事件为易失态快照）
        - 慢消费者：缓冲满丢弃事件，前端关键场景应配合轮询兜底
        - 跨租户事件在 SSE 通道强制过滤
        - Nginx 需 proxy_buffering off;
      operationId: streamEvents
      parameters:
        - $ref: '#/components/parameters/TenantHeader'
      responses:
        '200':
          description: SSE 流
          content:
            text/event-stream:
              schema:
                $ref: '#/components/schemas/SSEEvent'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '429':
          $ref: '#/components/responses/TooManyRequests'

  /healthz:
    get:
      tags: [Health]
      summary: 深度健康检查（K8s liveness 探针）
      security: []
      operationId: healthz
      description: 含 Store 连接深度检查，2 秒超时保护。不受限流影响。
      responses:
        '200':
          description: 健康
          content:
            application/json:
              schema:
                type: object
                properties:
                  status:
                    type: string
                    example: "ok"
                  checks:
                    type: object
                    additionalProperties:
                      type: string
        '503':
          description: Store 不可用
          content:
            application/json:
              schema:
                type: object
                properties:
                  status:
                    type: string
                    example: "unhealthy"
                  error:
                    type: string
                    example: "store unavailable"
```

### 2.6 通用 parameters

```yaml
  parameters:
    TenantHeader:
      in: header
      name: X-Tenant-ID
      required: true
      schema:
        type: string
      description: 租户隔离键（生产模式必填，缺失返回 401）
    PageParam:
      in: query
      name: page
      schema:
        type: integer
        minimum: 1
      description: 页码，从 1 开始；不传=不分页（返回全量，向后兼容）
    PageSizeParam:
      in: query
      name: pageSize
      schema:
        type: integer
        minimum: 1
        maximum: 200
        default: 20
      description: 每页条数，上限 200
    SortParam:
      in: query
      name: sort
      schema:
        type: string
        example: "created_at"
      description: 排序字段（端点自定义）
    OrderParam:
      in: query
      name: order
      schema:
        type: string
        enum: [asc, desc]
        default: desc
      description: 排序方向
    IdempotencyKey:
      in: header
      name: Idempotency-Key
      schema:
        type: string
        maxLength: 128
      description: 幂等键（见第 10 章），24 小时内同键同请求体返回首次结果
      required: false
```

---

## 3. 错误码标准

### 3.1 统一错误响应格式

所有错误响应统一为如下 JSON 结构（`error` 为对象，包含 `code` / `message` / `detail` / `requestID`）：

```json
{
  "error": {
    "code": "INVALID_ARGUMENT",
    "message": "agent_id is required",
    "detail": {
      "field": "agent_id",
      "rule": "required"
    },
    "requestID": "req-9f3a2c1b"
  }
}
```

> **兼容说明**：当前代码（`server_paginate.go` 的 `jsonError`）输出的是 `{"error": "<message>"}` 扁平结构。本规范定义的是目标结构，迁移路径如下：
>
> 1. **Phase 1**（向后兼容）：handler 同时输出 `error`（字符串）与 `error.code`/`error.message`（对象），客户端按 `typeof error` 判断。
> 2. **Phase 2**（v2 切换）：v2 仅输出对象结构，v1 冻结。
>
> 在过渡期，`trace_id` 字段保留为顶层字段（与 `error.requestID` 等价），便于旧客户端关联日志。

### 3.2 错误码列表

错误码采用 `SCREAMING_SNAKE_CASE`，与 gRPC `codes.Code` 名称对齐（便于 HTTP↔gRPC 互转）。

| HTTP | 业务错误码 | 含义 | 典型场景 | 是否可重试 |
|------|------------|------|----------|------------|
| 400 | `INVALID_ARGUMENT` | 参数错误 | 请求体非法 JSON / 必填字段缺失 / 枚举值非法 / cron 表达式语法错误 | 否（修正参数后重试） |
| 401 | `UNAUTHENTICATED` | 未认证 | 缺失 `X-Tenant-ID`（require-auth=true）/ JWT 验签失败 / Cookie `at` 失效 / refresh token 过期 | 否（重新登录） |
| 403 | `PERMISSION_DENIED` | 越权 | RBAC 角色不足 / CSRF Origin 校验失败 / `--public-register=false` 时注册 / 跨租户访问资源 | 否 |
| 404 | `NOT_FOUND` | 不存在 | 路径无匹配（`jsonErrorMux` 统一 JSON 化）/ 资源 ID 不存在 / 联邦未启用时访问联邦端点 | 否 |
| 405 | `METHOD_NOT_ALLOWED` | 方法不允许 | 路径存在但方法未实现（如对只读资源 PUT） | 否 |
| 409 | `CONFLICT` | 冲突 | 资源已存在（重复创建同名 CI 类型 / 同名角色）/ 任务已处于终态不可取消 | 否 |
| 413 | `PAYLOAD_TOO_LARGE` | 请求体过大 | 超过 `http.MaxBytesReader` 上限 1 MiB | 否 |
| 429 | `RATE_LIMIT_EXCEEDED` | 限流 | IP 令牌桶耗尽 / 登录连续失败 5 次锁 15min / 多租户配额超限 | 是（按 `Retry-After` 等待） |
| 500 | `INTERNAL` | 内部错误 | handler panic（recoveryMiddleware 兜底）/ store 不可用 / 未预期错误 | 是（指数退避） |
| 503 | `UNAVAILABLE` | 服务不可用 | `/healthz` Store 不可用 / `/readyz` 未持有 leader 租约 / helm CLI 未安装 / CMDB 采集未启用 | 是（指数退避） |

### 3.3 错误响应示例

**400 INVALID_ARGUMENT**（字段级错误）：

```json
{
  "error": {
    "code": "INVALID_ARGUMENT",
    "message": "schedule expression invalid",
    "detail": {
      "field": "schedule",
      "value": "*/5 * *",
      "rule": "must be 5-field cron"
    },
    "requestID": "req-9f3a2c1b"
  }
}
```

**401 UNAUTHENTICATED**（require-auth=true 缺租户头）：

```json
{
  "error": {
    "code": "UNAUTHENTICATED",
    "message": "missing X-Tenant-ID header",
    "requestID": "req-7b2e9a4f"
  }
}
```

**429 RATE_LIMIT_EXCEEDED**（带 Retry-After 头）：

```http
HTTP/1.1 429 Too Many Requests
Content-Type: application/json
Retry-After: 1

{
  "error": {
    "code": "RATE_LIMIT_EXCEEDED",
    "message": "rate limit exceeded",
    "detail": {
      "bucket": "ip:10.0.0.1",
      "limit": "10/s"
    },
    "requestID": "req-1c8d3e5a"
  }
}
```

**500 INTERNAL**（panic 兜底）：

```json
{
  "error": "internal server error",
  "traceId": "trace-5a2c9f1b"
}
```

> panic 兜底响应由 `recoveryMiddleware` 产出，结构为旧版扁平格式（`error` 字符串 + `traceId`），不展开 `detail`——避免 panic 路径上再做复杂 JSON 构造引发二次 panic。

---

## 4. 认证规范

OpsMesh 采用"网关注入 + 内核二次校验"的纵深防御模型，**内核不自行实现登录/鉴权/用户表/密码哈希**（等保三级要求复用底座）。但用户中心（B/S 仪表盘登录）由内核签发 JWT，与网关注入模式并存。

### 4.1 认证方式与使用场景

| 方式 | 适用端点 | 签发方 | 验签方 | 启用条件 |
|------|----------|--------|--------|----------|
| **JWT Bearer（HS256）** | 用户中心端点（auth/users/roles/permissions） | 控制面 `auth.go`（cfg.JWTSecret） | 控制面 | 默认启用；JWTSecret 空则随机 32 字节（重启失效） |
| **JWT Bearer（RS256）** | 所有业务端点 | 前置网关 / IAM（RSA 私钥） | 控制面 `authctx.FromJWT`（RSA 公钥） | `--jwt-public-key` 非空时启用 |
| **HttpOnly Cookie `at`** | 浏览器 B/S 仪表盘 | 控制面 `handleAuthLogin` 设置 | 控制面 | 默认启用；与 Bearer 二选一，优先取 Authorization 头 |
| **网关注入身份头** | 所有业务端点（含 gRPC） | 前置网关（APISIX/蓝鲸 IAM） | 控制面 `authctx.FromHTTPHeader`（机械读取，不校验来源） | 默认启用；JWT 公钥未配置时回退此模式 |
| **API Key（X-Internal-Key）** | 内部服务间调用（预留） | 运维分配 | 控制面 | 当前未实现，预留 |
| **mTLS** | gRPC 9090 / 联邦 mTLS 监听 | CA 签发客户端证书 | 控制面 `--client-ca` | `--tls-cert` + `--tls-key` + `--client-ca` 三者非空时启用 |
| **HMAC 签名** | gRPC agent 请求 | agent（HMAC-SHA256(agent_id + timestamp + secret)） | 控制面 `grpc.go` | `--grpc-require-signature=true`；时间戳偏差 ±5min 防重放 |
| **联邦 HMAC + mTLS** | 联邦 peer 间 HTTP 调用 | peer 控制面 | peer 控制面 | `--federation-peers` 非空时硬化启用 |

### 4.2 切换逻辑

身份提取入口为 `authctx.FromRequest(h, jwtCfg)`，行为矩阵：

| 条件 | 行为 |
|------|------|
| `Enabled && PublicKey!=nil && 携带有效 Bearer token` | 走 JWT 验签路径，从 claims 提取 `tenant_id`/`user_id`/`user_roles` |
| `Enabled && PublicKey!=nil && token 验签失败` | 返回 error，调用方应 401（**不回退头注入**） |
| `Enabled && PublicKey!=nil && 未携带 token` | 回退 `FromHTTPHeader`（兼容网关仅注入头场景） |
| `!Enabled \|\| PublicKey==nil` | 直接 `FromHTTPHeader`（MVP 头注入模式） |

### 4.3 JWT claims 约定

| claim | 类型 | 说明 |
|-------|------|------|
| `tenant_id` | string | 租户隔离键 |
| `user_id` | string | 操作人，写入审计 |
| `user_roles` | string[] 或逗号分隔 string | 角色列表，两种签发格式均兼容 |
| `iss` | string | 签发方；非空时 `FromJWT` 校验必须匹配 `cfg.JWTIssuer` |
| `exp` / `iat` / `nbf` | int64 | 标准 JWT 时间 claim，`jwt/v5` 默认校验 exp |
| `jti` | string | token 唯一 ID；登出时加入黑名单（经 SessionStore 多副本共享） |
| `device_fp` | string | 设备指纹；超过 `deviceFPDeadline` 签发的 refresh token 必须绑定非空 device_fp |

### 4.4 安全约束

- **H1 安全警告**：`FromHTTPHeader` 不校验头来源真实性，仅机械读取。生产环境**必须**在可信网关后部署，网关负责：
  - 校验调用方 JWT/OIDC 后**剥离**客户端自带的 `X-Tenant-ID`，再重注入经鉴权的真实租户；
  - 拒绝直连控制面（绕过网关）的请求（网络策略 / mTLS 双向认证）。
- **`--require-auth=true`** 时控制面拒绝缺失 `X-Tenant-ID` 头的请求（401）。
- **CSRF Origin 校验**（`csrfOriginCheck` 中间件）：对 POST/PUT/DELETE/PATCH 校验 Origin 头与 `cfg.AdvertiseAddr` 匹配，不匹配返回 403。demo 模式 / 非浏览器客户端（Origin 为空）放行。
- **Cookie 安全属性**：`at` / `rt` 为 HttpOnly + Secure（HTTPS 部署）+ SameSite=Strict。
- **登出全局生效**：jti 加入 SessionStore 黑名单，多副本 HA 经 Redis 共享（`--session-store=redis://`）。

---

## 5. 版本管理

### 5.1 版本策略

OpsMesh 采用 **URL 路径版本**（`/api/v1`），不使用 Header 版本（`Accept: application/vnd.opsmesh.v1+json`）——路径版本对客户端更直观、对网关路由更友好。

| 维度 | 约定 |
|------|------|
| 版本前缀 | `/api/v1`、`/api/v2`，整数递增，不使用日期版本（如 `/2024-01-01`） |
| 当前版本 | `v1`（生产冻结，仅接受向后兼容变更） |
| 版本生命周期 | v(N) 发布后 v(N-1) 维护 12 个月（bugfix only），到期下线 |
| 版本端点共存 | v1 与 v2 可同时注册（不同路径前缀），共享同一 handler 实现（按版本做行为分支） |
| gRPC 版本 | 包名 `opsmesh.v1`，服务名 `opsmesh.v1.Registration`；破坏性变更发 `opsmesh.v2.Registration`，灰度并行 |

### 5.2 向后兼容约定

**v(N) 内允许的兼容变更**（不升版本号）：

- 新增可选请求字段（默认值保证旧行为）
- 新增响应字段（客户端按需消费，未知字段忽略）
- 新增端点
- 新增枚举值（客户端遇到未知枚举值应优雅降级，不报错）
- 放宽校验（如字符串长度上限从 100 提到 200）
- 收紧 401/403/429 等错误响应（更严格的安全策略）

**v(N) → v(N+1) 必须升版本的破坏性变更**：

- 删除 / 重命名请求或响应字段
- 改变字段类型（string → int）
- 改变字段语义（`status` 枚举值含义变更）
- 收紧校验（如必填化原可选字段）
- 改变 HTTP 方法（POST → PUT）
- 改变错误码（200 → 4xx）
- 删除端点

### 5.3 gRPC 兼容性守护

- protobuf IDL 由 `buf breaking`（FILE 策略）在 CI 守护，禁止删字段 / 改类型 / 改字段号。
- 兼容期两条路径并行：手写 JSON codec（默认）↔ 生成 stub（protobuf codec，灰度切换）。
- 字段号**永不复用**：删除字段保留为 `reserved`，避免旧客户端反序列化错位。

### 5.4 弃用流程

1. 在响应头加 `Deprecation: true` + `Sunset: <RFC1123 date>`（HTTP 8594）。
2. 文档与 CHANGELOG 标注弃用日期与替代端点。
3. 12 个月后返回 410 Gone（v(N+1) 上线且 v(N) 流量 < 1%）。

---

## 6. 分页/过滤/排序约定

### 6.1 分页

列表类接口支持 `page` / `pageSize` 查询参数，**不传 `page` 时返回全量数组**（向后兼容）；传 `page` 时返回分页信封。

| 参数 | 类型 | 默认 | 约束 | 说明 |
|------|------|------|------|------|
| `page` | int | 不传=不分页 | ≥ 1 | 页码，从 1 开始 |
| `pageSize` | int | 20 | 1 ≤ x ≤ 200 | 每页条数，上限 200（防客户端请求过大拖垮后端） |

**分页响应信封**（`paginateResult`）：

```json
{
  "data": [],
  "total": 100,
  "page": 1,
  "pageSize": 20,
  "hasMore": true
}
```

**部分端点的旧分页参数**（向后兼容，不强制统一）：

| 端点 | 旧参数 | 说明 |
|------|--------|------|
| `GET /api/v1/audits` | `limit`（默认 100） | 上限，无 offset |
| `GET /api/v1/logs` | `limit` + `offset` | offset 分页（日志按时间倒序，offset 语义更直观） |

> **新端点**统一用 `page` / `pageSize`；旧端点保留旧参数，不在 v1 内强改。

### 6.2 过滤

过滤参数直接作为查询参数，按字段名命名（不引入 `filter[field]=value` 语法，保持 URL 简洁）。

| 端点 | 过滤参数 | 示例 |
|------|----------|------|
| `GET /api/v1/tasks` | `status`, `agent_id` | `?status=running&agent_id=a-001` |
| `GET /api/v1/devices` | `segment`, `status`, `managed` | `?segment=seg-a&status=online&managed=true` |
| `GET /api/v1/alerts` | `severity`, `status` | `?severity=critical&status=active` |
| `GET /api/v1/deploys` | `status` | `?status=running` |
| `GET /api/v1/cmdb/ci` | `type`, `status` | `?type=host&status=active` |
| `GET /api/v1/audits` | `tenant`, `action`, `from`, `to` | `?action=task:create&from=2026-08-01T00:00:00Z` |
| `GET /api/v1/logs` | `deviceID`, `agentID`, `level`, `source`, `keyword`, `from`, `to` | `?level=error&keyword=timeout` |

**多值过滤**：同一参数重复传值表示 OR（如 `?status=running&status=pending`），当前实现未广泛支持，预留 v2。

### 6.3 排序

| 参数 | 类型 | 默认 | 说明 |
|------|------|------|------|
| `sort` | string | 端点自定义（通常 `created_at`） | 排序字段，单字段（多字段排序 v2 支持 `sort=field1,field2`） |
| `order` | enum | `desc` | `asc` / `desc` |

**当前实现**：大部分列表端点未显式支持 `sort` / `order`，按 store 默认顺序返回（通常按 `created_at` 倒序）。本规范定义统一约定，新端点必须实现，旧端点逐步补齐。

### 6.4 时间范围

时间范围过滤统一用 `from` / `to` 查询参数，值为 RFC3339 UTC：

```
GET /api/v1/audits?from=2026-08-01T00:00:00Z&to=2026-08-07T23:59:59Z
```

---

## 7. SSE 事件规范

> 完整协议见 [`docs/sse-protocol.md`](./sse-protocol.md)。本节为接口规范视角的摘要与一致性约束。

### 7.1 端点与握手

| 项 | 值 |
|----|----|
| 端点 | `GET /api/v1/events/stream` |
| Content-Type | `text/event-stream` |
| 身份 | require-auth=true 时缺失 `X-Tenant-ID` / `Authorization: Bearer` → 401；demo 模式缺失则注入 `default` 租户 |
| 握手 | 连接建立后立即下发 `event: hello` + `data: {}` |
| 心跳 | 每 15s 发送注释帧 `: ping\n\n`（不触发浏览器 message 事件，仅防代理空闲断连） |
| 重连 | **不支持 Last-Event-ID 断点续传**（事件为易失态快照，重连重新订阅即可）；历史回溯走各资源查询 API |
| 代理配置 | Nginx 需 `proxy_buffering off;`，否则事件被缓冲延迟 |

### 7.2 信封结构

`data` 行 JSON 信封：

```json
{
  "type": "task_status",
  "tenantID": "t1",
  "data": { "taskID": "xxx", "status": "running" },
  "traceID": "可选，32 字符 hex"
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `type` | string | 事件类型枚举（见 7.3） |
| `tenantID` | string | 事件归属租户；`omitempty`（hello 等全局事件为空）。非空时服务端仅下发给同租户订阅者，跨租户事件直接丢弃 |
| `data` | object | 业务载荷，任意 JSON 对象 |
| `traceID` | string | OTel trace_id（可选），关联后端日志/审计 |

> **字段命名注意**：信封字段名为 Go 结构体 tag 直出（`tenantID`/`traceID`/`taskID`），**非下划线式**，与 REST API 部分端点的命名习惯不同——以本表为准。

### 7.3 事件类型枚举（全量 10 种）

| type | 触发点 | data 关键字段 |
|------|--------|--------------|
| `hello` | 连接建立握手 | `{}` |
| `task_status` | 任务创建 / 领取 / 取消 / 上报结果 | `taskID`、`status`、`agentID` |
| `alert_new` | 新告警产生 / ack / silence（列表变更即触发） | `alertID`、`severity`、`ruleID` |
| `device_online` | agent Register 上线 | `deviceID`、`segment`、`addr` |
| `device_offline` | 设备退役 / 离线归档 | `deviceID`、`lastSeen` |
| `approval_status` | 作业审批通过/拒绝 | `requestID`、`action` |
| `schedule_status` | 定时任务触发/暂停/恢复 | `scheduleID`、`action` |
| `os_template_changed` | OS 优化模板增删改 | `templateID`、`action` |
| `mw_template_changed` | 中间件模板增删改 | `templateID`、`action` |
| `agent_logs` | agent 日志上报到达 | `agentID`、`logName`、`lines` |

### 7.4 慢消费者策略

每个订阅者独立 buffered chan（容量 16）。`publishEvent` 非阻塞广播：

- 缓冲未满：写入；
- 缓冲已满：**丢弃该事件**（保护广播不被单个慢客户端拖垮）。

前端关键场景（如"告警角标"）应配合轮询兜底，SSE 只作为加速路径。

### 7.5 安全

- 跨租户事件在 SSE 通道强制过滤（`publishEvent` 携带 tenantID，handler 只放行匹配项）。
- SSE 订阅计入审计（`Action=subscribe_sse`，见 `sse.go`）。

---

## 8. gRPC API 规范

### 8.1 服务定义

gRPC 服务监听 9090 端口，承载 agent ↔ 控制面 的注册/心跳/拉任务/上报/取消通道。IDL 见 `proto/opsmesh/v1/registration.proto`。

```protobuf
syntax = "proto3";
package opsmesh.v1;

option go_package = "opsmesh/internal/grpcx/pb;pbv1";

import "google/protobuf/timestamp.proto";

// agent↔控制面 的注册通道服务。
// 服务名 opsmesh.v1.Registration（带版本前缀，破坏性变更可灰度）。
// 六个一元方法，无流式方法 —— 与 internal/grpcx.RegistrationServer 接口一一对应。
service Registration {
  rpc Register(AgentInfo) returns (RegisterResp);
  rpc Heartbeat(HeartbeatReq) returns (Empty);
  rpc PullTasks(PullTasksReq) returns (PullTasksResp);
  rpc ReportResult(TaskResult) returns (Empty);
  rpc CancelTask(CancelTaskReq) returns (Empty);
  rpc PollCancels(PollCancelsReq) returns (PollCancelsResp);
}
```

### 8.2 方法语义与幂等性

| 方法 | 请求 | 响应 | 语义 | 幂等 | 频率 |
|------|------|------|------|------|------|
| `Register` | `AgentInfo`（含 `install_token`） | `RegisterResp`（分配 `agent_id` + `control_config`） | agent 首次注册；携带 install_token 时可自动纳管候选设备 | 否（首次注册分配 ID） | 一次 |
| `Heartbeat` | `HeartbeatReq`（`agent_id` + `status` + `load` + 可选 `cmdb_report`） | `Empty` | 上报在线状态与负载，附带 CMDB 增量 | 是 | 每 10s |
| `PullTasks` | `PullTasksReq`（`agent_id`） | `PullTasksResp`（`tasks[]`） | 原子领取下一条 pending 任务（多副本安全） | 是 | 每 2s（与 PollCancels 交替） |
| `ReportResult` | `TaskResult`（`task_id` + `exit_code` + `stdout` + `stderr` + `duration_ms`） | `Empty` | 上报任务执行结果（成功/失败/重试/死信） | 是（按 task_id 幂等覆盖） | 任务完成时 |
| `CancelTask` | `CancelTaskReq`（`task_id` + `tenant_id`） | `Empty` | 取消指定任务；`tenant_id` 由服务端用网关注入身份强制覆盖（防越权） | 是 | 按需 |
| `PollCancels` | `PollCancelsReq`（`agent_id`） | `PollCancelsResp`（`cancelled_task_ids[]`） | agent 轮询本机被取消的任务 ID | 是 | 每 2s |

### 8.3 错误码

gRPC 使用标准 `codes.Code`，与 HTTP 错误码（第 3 章）对齐：

| gRPC code | HTTP 等价 | 使用场景 |
|-----------|-----------|----------|
| `OK` (0) | 200 | 成功 |
| `InvalidArgument` (3) | 400 | 请求字段非法 |
| `Unauthenticated` (16) | 401 | 缺失身份 metadata / HMAC 签名失败 / mTLS 证书无效 |
| `PermissionDenied` (7) | 403 | 跨租户访问 / agent 无权操作该任务 |
| `NotFound` (5) | 404 | `agent_id` / `task_id` 不存在 |
| `FailedPrecondition` (9) | 409 | 任务已处于终态不可取消 |
| `ResourceExhausted` (8) | 429 | gRPC 限流（预留） |
| `Internal` (13) | 500 | handler panic（`grpcRecoveryInterceptor` 兜底） |
| `Unavailable` (14) | 503 | 服务关闭中 / leader 未就绪 |

### 8.4 流式接口

当前 `Registration` 服务**无流式方法**（六个一元方法）。流式接口预留场景：

| 预留方法 | 流向 | 用途 |
|----------|------|------|
| `StreamEvents` | server → client | 控制面 → agent 推送取消信号 / 配置变更（替代 PollCancels 轮询） |
| `StreamLogs` | client → server | agent → 控制面 流式上报日志（替代 POST /api/v1/logs 批量） |

流式接口在 v2 引入，v1 保持一元方法以兼容现有 agent。

### 8.5 安全

| 机制 | 启用条件 | 说明 |
|------|----------|------|
| **TLS** | `--tls-cert` + `--tls-key` 非空 | 服务端证书 |
| **mTLS** | 上述 + `--client-ca` 非空 | 客户端证书校验 |
| **HMAC 签名** | `--grpc-require-signature=true` | agent 请求须携带 `agent-signature` metadata（HMAC-SHA256(agent_id + timestamp + secret)），时间戳偏差 ±5min 防重放 |
| **Recovery 拦截器** | 始终启用 | `grpcRecoveryInterceptor` 捕获 handler panic，返回 `codes.Internal`，单处 panic 不拖垮整个控制面 |
| **身份注入** | 网关注入 metadata | `authctx.FromGRPCMetadata` 提取 `x-tenant-id` / `x-user-id` / `x-user-roles`（小写 metadata key） |

### 8.6 兼容性

- 兼容期两条路径并行：手写 JSON codec（默认）↔ 生成 stub（protobuf codec，灰度切换）。
- protobuf IDL 由 `buf breaking`（FILE 策略）在 CI 守护，禁止删字段 / 改类型 / 改字段号。
- 字段号**永不复用**：删除字段保留为 `reserved`。

---

## 9. 限流规范

### 9.1 限流维度

OpsMesh 控制面有三层限流，互不重叠：

| 层 | 维度 | 实现 | 启用条件 | 超限响应 |
|----|------|------|----------|----------|
| **API 限流** | 按客户端 IP 令牌桶 | `rateLimiter`（`server_security.go`） | `--cb-rate-limit-per-sec > 0` | 429 + `Retry-After: 1` |
| **登录防爆破** | 按 IP 令牌桶 + 按账号失败计数 | `loginGuard`（`auth_login.go`） | 始终启用 | 429（IP 桶耗尽）/ 401（账号锁定 15min） |
| **多租户配额** | 按租户资源数上限 | `quotaMgr`（`quota.go`） | `--quota-enabled=true` | 429 / 403 |

### 9.2 API 限流细节

- **算法**：令牌桶，每 IP 独立桶。
- **速率**：`cfg.CBRateLimitPerSec`（每秒补充令牌数），桶容量 = 速率（允许 1s 突发）。
- **首次访问**：满桶（容量 = ratePerSec）。
- **清理**：`sweepInterval`（10 分钟）周期清理超过 sweepInterval 未访问的 IP 条目，防内存泄漏。
- **豁免**：`/healthz` 与 `/readyz` 不限流，避免 K8s 探针被限流误杀 Pod。
- **多副本语义**：IP 令牌桶保留进程内，多副本 HA 下副本数 N 时实际阈值 N × ratePerSec（可接受；如需全局精确限流需引入 Redis）。

### 9.3 登录防爆破

- **IP 令牌桶**：每 IP 10 突发 / 每 3s 补 1。
- **账号锁定**：连续失败 5 次锁 15min。
- **多副本共享**：失败计数 + 账号锁定经 SessionStore 共享（`--session-store=redis://`），任一副本触发锁定后其他副本也拒绝。
- **IP 桶不共享**：保留进程内（多副本各自限流，副本数 N 时实际阈值 N × 10，可接受）。

### 9.4 速率限制头

> **当前实现**：仅返回 `Retry-After: 1`，**未实现**完整的 `X-RateLimit-*` 头。本规范定义目标结构，作为后续增强方向。

**目标响应头**（429 与 200 均返回，供客户端自适应限流）：

| 头 | 类型 | 说明 |
|----|------|------|
| `X-RateLimit-Limit` | int | 桶容量（每秒允许请求数），如 `10` |
| `X-RateLimit-Remaining` | int | 当前窗口剩余令牌数，如 `7` |
| `X-RateLimit-Reset` | int | 令牌桶完全重置的 Unix 时间戳（秒），如 `1723036800` |
| `Retry-After` | int | 429 时建议客户端等待秒数（与 `X-RateLimit-Reset` 互补，更直观） |

**429 响应示例**：

```http
HTTP/1.1 429 Too Many Requests
Content-Type: application/json
X-RateLimit-Limit: 10
X-RateLimit-Remaining: 0
X-RateLimit-Reset: 1723036800
Retry-After: 1

{
  "error": {
    "code": "RATE_LIMIT_EXCEEDED",
    "message": "rate limit exceeded",
    "detail": {
      "bucket": "ip:10.0.0.1",
      "limit": "10/s"
    },
    "requestID": "req-1c8d3e5a"
  }
}
```

### 9.5 多租户配额

| 资源 | 默认上限 | 配置项 | 超限响应 |
|------|----------|--------|----------|
| 设备数 | `cfg.QuotaMaxDevices`（0=不限） | `--quota-max-devices` | 429 |
| 任务数 | `cfg.QuotaMaxTasks`（0=不限） | `--quota-max-tasks` | 429 |
| 告警数 | `cfg.QuotaMaxAlerts`（0=不限） | `--quota-max-alerts` | 429 |

配额检查在创建路径调用 `CheckDevice` / `CheckTask` / `CheckAlert`；`quotaMgr.Enabled()=false` 时直接放行（向后兼容）。配额查询走 `GET /api/v1/quotas[/{tenantID}]`。

---

## 10. 幂等性约定

### 10.1 幂等键（Idempotency-Key）

对**非天然幂等**的 POST 端点，客户端可携带 `Idempotency-Key` 头实现请求重试安全：

| 头 | 类型 | 约束 |
|----|------|------|
| `Idempotency-Key` | string | maxLength 128；客户端生成的唯一键（UUID v4 推荐） |

**语义**：

- 24 小时内同 `Idempotency-Key` + 同请求体 → 返回首次执行的完整结果（含状态码与响应体）。
- 同 `Idempotency-Key` + 不同请求体 → 400 `INVALID_ARGUMENT`（detail 指明 idempotency conflict）。
- 24 小时后键失效，相同键视为新请求。

### 10.2 需要幂等键的端点

| 端点 | 是否需要 | 理由 |
|------|----------|------|
| `POST /api/v1/auth/login` | 否 | 登录天然幂等（同账号返回同 token 集合）；防爆破由 loginGuard 处理 |
| `POST /api/v1/auth/register` | **推荐** | 防止网络抖动导致重复注册（同 username 返回 409，幂等键让客户端重试拿首次结果而非 409） |
| `POST /api/v1/tasks` | **推荐** | 防止任务重复下发（同 agent + 同 command + 同 schedule 应只创建一条） |
| `POST /api/v1/tasks/batch` | **推荐** | 同上，批量场景更敏感 |
| `POST /api/v1/tasks/batch-exec` | **推荐** | 同上 |
| `POST /api/v1/tasks/canary` | **推荐** | 灰度发布重复触发会导致双倍流量 |
| `POST /api/v1/deploys` | **推荐** | 防止部署计划重复创建 |
| `POST /api/v1/workflows` | **推荐** | 防止工作流重复创建 |
| `POST /api/v1/workflows/{id}/trigger` | **推荐** | 防止工作流重复触发（同 trigger 应只产生一个 run） |
| `POST /api/v1/cmdb/ci` | **推荐** | 防止 CI 实例重复创建 |
| `POST /api/v1/cmdb/relations` | **推荐** | 防止关系重复创建 |
| `POST /api/v1/k8s/clusters` | **推荐** | 防止集群重复注册 |
| `POST /api/v1/devices/{id}/provision` | 否 | install_token 一次性，重复请求返回新 token（旧 token 仍有效 15min） |
| `POST /api/v1/tasks/{id}/cancel` | 否 | 天然幂等（取消已取消任务返回 200） |
| `POST /api/v1/deploys/{id}/rollback` | 否 | 天然幂等（回滚已回滚部署返回 200） |
| `POST /api/v1/alerts/{id}/ack` | 否 | 天然幂等（确认已确认告警返回 200） |
| `POST /api/v1/alerts/{id}/silence` | 否 | 天然幂等（静默已静默告警返回 200） |
| `POST /api/v1/auth/logout` | 否 | 天然幂等（登出已登出返回 200） |
| `POST /api/v1/auth/refresh` | 否 | refresh token 一次性，旋转后旧 token 失效 |

### 10.3 天然幂等的端点

GET / PUT / DELETE 天然幂等（同请求多次执行结果一致），无需 `Idempotency-Key`：

- `GET /api/v1/*`（读不副作用）
- `PUT /api/v1/users/{id}`（全量替换，多次执行终态一致）
- `DELETE /api/v1/devices/{id}`（退役，多次执行终态一致；第二次返回 404 或 200 均可）

### 10.4 实现状态

> **当前实现**：`Idempotency-Key` 头**未实现**，本规范定义目标约定。实现路径：
>
> 1. 中间件层缓存 `(tenantID, idempotencyKey, requestBodyHash) → (status, responseBody)`，TTL 24h，存储用 SessionStore（Redis 多副本共享）。
> 2. 命中缓存直接返回缓存响应；未命中执行 handler 后写入缓存。
> 3. 同键不同请求体返回 400。
>
> 在未实现前，客户端重试需自行处理 409（重复创建）场景。

---

## 附录 A：与代码的对应关系

| 规范章节 | 代码事实源 | 守护测试 |
|----------|------------|----------|
| 1.1 路由注册 | `internal/controlplane/server_lifecycle.go` `Start()` | `endpoint_test.go` |
| 1.3 状态码 | `internal/controlplane/server_paginate.go` `jsonError` | `handler_extra_test.go` |
| 2 OpenAPI | 本文档（手工维护） | 计划引入 `oapi-codegen` + CI 校验 |
| 3 错误码 | `internal/controlplane/server_middleware.go` `recoveryMiddleware` | `server_middleware_extra_test.go` |
| 4 认证 | `internal/authctx/authctx.go` `FromRequest` | `auth_test.go` |
| 5 版本 | 路径前缀 `/api/v1`（全端点） | — |
| 6 分页 | `internal/controlplane/server_paginate.go` `paginateJSONHandler` | `server_paginate_extra_test.go` |
| 7 SSE | `internal/controlplane/sse.go` | `sse_contract_test.go` |
| 8 gRPC | `proto/opsmesh/v1/registration.proto` + `internal/controlplane/grpc.go` | `grpc_test.go` + `buf breaking` |
| 9 限流 | `internal/controlplane/server_security.go` `rateLimitMiddleware` | `server_security_extra_test.go` |
| 10 幂等 | 未实现（规范先行） | — |

## 附录 B：变更日志

| 日期 | 版本 | 变更 | 作者 |
|------|------|------|------|
| 2026-08-17 | 1.0 | 初版：从 api-reference.md + 代码事实源提炼接口规范 | 技术文档工程师 |