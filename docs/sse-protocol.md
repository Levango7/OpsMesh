# SSE 协议说明（实时推送）

## 概述

OpsMesh 在控制面提供 SSE（Server-Sent Events）实时推送，用于任务状态变更、告警触发/确认、设备上下线等事件。本协议独立于版本管理，控制面与企业版前端按此契约解析。

## 端点

```
GET /api/v1/events/stream
```

### 请求头

| 头 | 必需 | 用途 |
|---|---|---|
| `X-Tenant-ID` | 是（require-auth 开启时） | 租户隔离，缺则 401 |
| `Accept: text/event-stream` | 是 | 声明 SSE |
| `Cache-Control: no-cache` | 是 | 防代理缓存 |

### 响应头

```
HTTP/1.1 200 OK
Content-Type: text/event-stream; charset=utf-8
Cache-Control: no-cache
Connection: keep-alive
```

## 事件格式

```
id: <event-id>
event: <event-type>
data: <JSON-string>

```

字段规则：
- `id` 是递增数字，客户端断线重连时以 `Last-Event-ID` 头带最后成功收到的 id，服务端应从此 id 之后重发（若支持）。
- `event` 描述事件类型，见下方枚举。
- `data` 是紧凑 JSON，总长度 ≤ 16KiB；字段名使用 snake_case。

## 事件类型枚举

| event | 说明 | data 关键字段 |
|---|---|---|
| `task.status` | 任务状态变更 | `task_id`、`status`、`agent_id`、`tenant_id`、`updated_at` |
| `task.result` | 任务执行完成上报 | `task_id`、`exit_code`、`stdout_len`、`stderr_len` |
| `alert.fire` | 新告警触发 | `alert_id`、`severity`、`metric`、`value`、`threshold`、`rule_id` |
| `alert.ack` | 告警被确认 | `alert_id`、`ack_by` |
| `alert.silence` | 告警被静默 | `alert_id`、`until` |
| `device.online` | 设备上线 | `device_id`、`segment`、`addr` |
| `device.offline` | 设备离线 | `device_id`、`last_seen` |
| `device.retired` | 设备退役/归档 | `device_id`、`archived_at` |

## 心跳

服务端每 15s 发送注释行保活（防代理空闲断连）：

```
: keepalive

```

企业版前端应忽略以 `:` 开头的注释行。

## 重连策略

- 网络层异常（TCP 断开 / 5xx / 超时）时，企业版前端按默认 SSE 重连机制自动重连，间隔为浏览器默认值（Chrome 约 3s）。
- 若服务端主动回复 `204 No Content`，表示该租户无数据，前端应延迟 10s 后再重连。
- 若要断点续传，客户端在重连请求头加 `Last-Event-ID: <id>`。

## 错误响应

| HTTP | 场景 | 行为 |
|---|---|---|
| 401 | 缺少 `X-Tenant-ID` 且 `--require-auth=true` | 不要自动重连，修复身份后手动刷新页面 |
| 429 | 单租户 SSE 连接数超过上限（默认 32） | 退避重连 |
| 500 | 内部异常 | 前端按指数退避重连 |

## 安全

- 事件 `data` 中的所有字段在写入前均按 `tenant_id` 强制过滤，跨租户事件不会进入 SSE 流。
- SSE 连接计入审计（`Action=subscribe_sse`），审计点位于控制面 `sse.go`。
