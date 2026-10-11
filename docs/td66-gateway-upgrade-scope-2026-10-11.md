# TD-66 立项：API 网关（`/gw/`）升级为正式数据面（2026-10-11）

> **依据**：勘测报告 `docs/td66-gateway-survey-2026-10-11.md`（同日，只读勘测，14 缺口 + 三档定价）。
> 本文档确定需求、范围与验收标准。基准 `a628978`。

---

## 1. 需求分析——为什么要升级

### 1.1 现状不足以承载的三个场景

| 场景 | 现状表现 | 后果 |
|---|---|---|
| **重启恢复** | 路由存内存 map，进程重启全丢 | 已配的路由变 404，需重新录入 |
| **后端故障** | 无熔断/健康检查，每次都全量转发 | 网关本身被慢后端拖垮（无 fast-fail） |
| **可观测** | 无 metrics 埋点，统计只在 HTTP API 里 | Prometheus 看不到网关流量（有 `/metrics` 端点但没接） |

### 1.2 为什么不走 APISIX/Envoy

2026-09-05 决策"生产走 APISIX/Envoy"仍然正确——**对外部流量**。但本网关的定位是
**内部轻量路由编排**（运维在控制面 UI 配路由规则、按租户隔离、跟 RBAC 集成），
这正是 APISIX/Envoy 需要额外搭一层管理面才能做到的事。升级它不是替代正式网关，
是让**已有**的路由编排能力达到可用线。

### 1.3 已有质量好、无需动的部分

勘测确认以下三块**质量是好的、不需要动**：
- 鉴权（`subsystemAuthorize` 双闸 + 按 HTTP 方法分级权限）
- 租户隔离（路由池只取 `actx.TenantID`）
- SSRF 双检（创建时 + 投递期复检防 DNS rebinding）

---

## 2. 范围确定

### 2.1 本期范围（MVP，约 1.5~2 天）

| # | 需求 | 实现 | 验收标准 |
|---|---|---|---|
| **P1** | **路由持久化** | store 加 `gateway_routes` 表（CRUD + 按 tenant 隔离）；启动时从 store 加载、写操作同步 store | 重启后路由规则不丢；store=memory 时行为不变（内存实现兜底） |
| **P2** | **熔断/离群摘除** | 进程内断路器（连续 N 次 5xx/超时 ⇒ 打开，冷却期后半开试探）；每路由一个实例 | 后端连续 5 次 5xx ⇒ 后续请求 fast-fail 503（不再全量转发等超时）；冷却 30s 后半开试探 |
| **P3** | **Prometheus 埋点** | 接入 `s.metrics`：每路由的 `gw_requests_total`（按 route/tenant/status 标签）+ `gw_latency_ms`（直方图） | `/metrics` 端点可抓到网关时序；Grafana 面板可按路由/租户看流量 |
| **P4** | **超时治理** | 每路由可配 `TimeoutMs`（默认 30s），ReverseProxy 加 `http.Server` 超时 | 慢后端到 30s 即 504，不占连接 |

### 2.2 明确不做（本期外）

| 项 | 理由 |
|---|---|
| 多后端/负载均衡 | RouteRule 模型改版是**破坏性变更**（TargetBackend 从 string 变列表），需单独定价 |
| gRPC 数据面 | 无消费方需求；创建时已拒（`a628978`） |
| 外部授权委托（OPA） | 无消费方需求 |
| WebSocket | 无消费方需求 |
| 主动健康检查 | 熔断（P2）的被动模式已覆盖"后端挂了"的场景；主动探测等有需求再说 |
| 统计跨副本聚合 | P3 的 Prometheus 埋点天然跨副本（Prometheus 抓各副本 + 自身聚合） |

### 2.3 依赖与前置

- **P1 依赖**：store 迁移机制（已有：`sqlstore/migrations/`，加一张表的迁移文件即可）
- **P2 无依赖**：进程内实现，不需要外部组件
- **P3 依赖**：`s.metrics` 已有 `RecordHTTP` 基础设施（`metrics_endpoint.go`），只需加路由级标签
- **P4 无依赖**：ReverseProxy 的 Transport 设 Timeout 即可

---

## 3. 技术方案要点

### P1 路由持久化
```
新增迁移：022_gateway_routes.sql
  CREATE TABLE gateway_routes (
    id VARCHAR(64) PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    path_prefix VARCHAR(512) NOT NULL,
    target_backend VARCHAR(512) NOT NULL,
    methods JSON,
    rate_limit_per_sec INT DEFAULT 0,
    timeout_ms INT DEFAULT 30000,
    enabled BOOLEAN DEFAULT TRUE,
    created_at DATETIME, updated_at DATETIME
  );
```
store 接口：`GatewayRouteStore`（List/Save/Delete by tenant）；memory 后端加内存实现。
启动时 `loadGatewayRoutes()` 从 store 恢复到 `gw.routes`。

### P2 熔断
```go
type circuitBreaker struct {
    mu          sync.Mutex
    failures    int       // 连续失败计数
    state       string    // "closed" | "open" | "half-open"
    openedAt    time.Time // 进入 open 的时刻
    threshold   int       // 连续失败阈值（默认 5）
    cooldown    time.Duration // 冷却期（默认 30s）
}
```
在 `handleGatewayProxy` 转发前判 `Allow()`；转发后按响应状态码 `Record(status)`。
断路器实例挂在 `gatewayRouteEntry` 上（每路由一个）。

### P3 Prometheus 埋点
在 `handleGatewayProxy` 的 `defer` 统计块里加：
```go
s.metrics.RecordGatewayRequest(rule.ID, actx.TenantID, strconv.Itoa(rw.status), latencyMs)
```
`metrics` 包加一个 `GatewayRequestsTotal` counter（标签 route/tenant/status）+ `GatewayLatency` histogram。

### P4 超时
RouteRule 加 `TimeoutMs int` 字段（JSON `timeoutMs`，默认 30000）。
ReverseProxy 的 `Transport` 设 `http.Transport{ResponseHeaderTimeout: timeout}`。

---

## 4. 验收标准

1. **P1**：重启控制面后 `GET /api/v1/gateway/routes` 返回重启前的规则（MySQL 后端）；
   memory 后端行为不变（重启丢失——测试环境可接受）。
2. **P2**：模拟后端连续返回 500 × 5 次 ⇒ 第 6 次请求直接 503（不再打到后端）；
   等 30s 后半开试探——后端恢复则关闭断路器。
3. **P3**：`GET /metrics` 含 `opsmesh_gw_requests_total{route=...,tenant=...,status=...}` 时序。
4. **P4**：配 `timeoutMs=1000` 的路由，后端故意 sleep 2s ⇒ 请求 504（1s 即断）。
5. **回归**：现有 11 条单测全过；鉴权/租户隔离/SSRF 行为不变。

---

## 5. 执行计划

| 步骤 | 内容 | 预计 |
|---|---|---|
| 1 | 迁移 + store 接口 + 启动加载 + CRUD 同步 | 0.5 天 |
| 2 | 熔断器 + 接入 handleGatewayProxy | 0.5 天 |
| 3 | Prometheus 埋点 + 超时 | 0.5 天 |
| 4 | 单测（每项至少 2 条：正/反）+ 变异检验 | 0.5 天 |
| 5 | 真进程验证（复用现有验证模式） | 0.5 天 |

**总计约 2.5 天**（含测试与验证）。

---
*立项人：zcode 线，2026-10-11。勘测与立项同期完成，相隔半小时。*
