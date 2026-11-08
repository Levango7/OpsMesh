# SSE 实时推送协议（与 `internal/controlplane/sse.go` 逐字对齐）

> 本协议依据代码实际实现整理，且由 `internal/controlplane/sse_contract_test.go` 的一致性用例守护——文档与代码出现漂移时 CI 会失败。修改事件名/字段前请先改代码与测试。

## 端点

```
GET /api/v1/events/stream
```

- `Content-Type: text/event-stream`
- 身份：与其余业务端点同一口径，由 `requireTenantContext`（`internal/controlplane/http_infra.go`）提取并**校验**：
  - `X-Tenant-ID` 头与 Bearer/Cookie JWT 的 `tenant_id` 不一致 → **403**；`X-User-Id` 与令牌 user 不一致 → **403**；
  - 头非空但无任何凭证：`require-auth` 开启且未开 `--trust-gateway-headers` → **401**；显式开启 `--trust-gateway-headers`（可信网关认证后剥离凭证、只留头转发）→ 放行；
  - 头缺失 → 回退 `Authorization: Bearer` / HttpOnly Cookie JWT（浏览器 `EventSource` 设不了自定义头，走这条）；
  - 两者均无：`require-auth` → **401**；demo 模式 → 注入 `default` 租户；其余 → **400**（不允许空租户订阅全量事件）。
- **2026-11-07 收口**：此前 handler 自行提取身份、**头非空即无条件采信**，只在头为空时才回退令牌 ⇒ 匿名者伪造 `X-Tenant-ID: 受害租户` 即可订阅该租户的任务/告警/设备/审批事件流（边缘 nginx 的 SSE location 不剥离该头）。现由 `TestSSE_ForgedTenantHeaderWithoutCredentialRejected`、`TestSSE_TenantHeaderMismatchJWTRejected`、`TestSSE_HeaderAndMatchingJWTAllowed`、`TestSSE_TrustGatewayHeadersAllowsHeaderOnly`、`TestSSE_CookieJWTSubscriberGetsOwnTenantEvents` 五条用例钉住（前两条在收口前实测返回 200 + 握手帧）。
- 代理配置提示：Nginx 需 `proxy_buffering off;`，否则事件会被缓冲延迟。

## 握手与心跳

- 连接建立后服务端立即下发握手帧：`event: hello`、`data: {}`。
- **订阅在握手帧之前注册**：客户端收到 `hello` 即意味着订阅已在册 ⇒ 连接建立后立刻发布的
  事件不会丢（2026-11-07 修复：此前是「先写 hello、后注册订阅」，那个窗口内发布的事件会被
  静默丢弃，表现为首连丢第一条事件）。
- 每 15s 发送注释帧 `: ping\n\n` 保活（不触发浏览器 message 事件，仅防止代理空闲断连）。
- 当前实现**不支持 Last-Event-ID 断点续传**（事件为易失态快照，重连重新订阅即可）。如需历史回溯请走各资源的查询 API。

## 信封结构（data 行 JSON）

```json
{
  "type": "task_status",
  "tenantID": "t1",
  "data": { "taskID": "xxx", "status": "running" },
  "traceID": "可选，32 字符 hex"
}
```

字段规则：
- `type`：事件类型（固定枚举，见下表）
- `tenantID`：事件归属租户；`omitempty`（hello 等全局事件为空）。非空时服务端仅下发给同租户订阅者，跨租户事件直接丢弃。
- `data`：业务载荷，任意 JSON 对象。
- `traceID`：OTel trace_id（可选），用于关联后端日志/审计。

> **注意**：本协议字段名为 Go 结构体 tag 直出（`tenantID`/`traceID`/`taskID`），并非下划线式，与 REST API 部分端点的命名习惯不同——以本表为准。

## 事件类型枚举（全量 11 种）

| type | 触发点 | data 关键字段 |
|---|---|---|
| `hello` | 连接建立握手 | `{}` |
| `task_status` | 任务创建 / 领取 / 取消 / 上报结果 | `taskID`、`status`、`agentID` |
| `task_cancel` | 任务取消信号下发给 agent（Wave 4-D） | `taskID`、`agentID` |
| `alert_new` | 新告警产生 / ack / silence（列表变更即触发） | `alertID`、`severity`、`ruleID` |
| `device_online` | agent Register 上线 | `deviceID`、`segment` |
| `device_offline` | 设备退役 / 离线归档 | `deviceID` |
| `approval_status` | 作业审批通过/拒绝 | `requestID`、`status` |
| `schedule_status` | 定时任务触发/暂停/恢复 | `scheduleID`、`status` |
| `os_template_changed` | OS 优化模板增删改 | `templateID`、`action` |
| `mw_template_changed` | 中间件模板增删改 | `templateID`、`action` |
| `agent_logs` | agent 日志上报到达 | `agentID`、`logName`、`lines` |

## 慢消费者策略

每个订阅者独立 buffered chan（容量 16）。`publishEvent` 非阻塞广播：
- 缓冲未满：写入；
- 缓冲已满：丢弃该事件（保护广播不被单个慢客户端拖垮）。

前端若实现"告警角标"等关键场景，应配合轮询兜底，SSE 只作为加速路径。

## 安全

- 跨租户事件在 SSE 通道强制过滤（`publishEvent` 携带 tenantID，handler 只放行匹配项）；
- 身份校验拒绝（头与令牌租户/用户不一致的 403）经 `auditAuthzDenied` 计入审计；
- **订阅成功本身不写审计**：旧文档曾承诺 `Action=subscribe_sse`，但代码中从无该动作
  （2026-11-07 现测：`grep -rn "subscribe_sse" --include=*.go .` 命中 **0**，全仓仅文档两处命中）。
  本次按事实纠正该承诺，不新增审计动作；`docs/api-specification.md` §7.5 的同句一并纠正。
