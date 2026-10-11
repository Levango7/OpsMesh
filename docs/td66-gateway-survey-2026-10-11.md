# TD-66 勘测报告：API 网关（`/gw/`）数据面现状盘点（2026-10-11，只读勘测）

> **缘起**：台账 TD-66 行（2026-09-05 决策：预览形态，完整数据面不在路线图，生产走 APISIX/Envoy）。
> 本报告按用户要求做一轮**只读勘测**：把现状摸清楚、缺口列全、升级路径定价——为未来可能的重新立项提供底稿，
> 不改任何代码。基准 `d1dfa7c`（2026-10-11），全部结论基于读码实测。

---

## 1. 实物盘点（647 行 Go + 11 条单测）

| 组件 | 位置 | 行数 | 职责 |
|---|---|---|---|
| 控制面 handler | `internal/controlplane/gateway.go` | 525 | 路由规则 CRUD/启停/统计 + 数据面转发 `handleGatewayProxy` |
| 路由匹配引擎 | `internal/extension/gateway.go` | 122 | `RouteRule` 模型 + `MatchRoute`（PathPrefix+Methods 首条命中） |
| 单测 | `internal/controlplane/gateway_test.go` | 11 用例 | CRUD/匹配/限流/租户隔离/SSRF |

**数据面管线**（`handleGatewayProxy`，`gateway.go:430-525`）：

```
请求 /gw/<path>
  → subsystemAuthorize（gateway:read|write 按 HTTP 方法分级）     …… 鉴权 ✓
  → 路由池只取 actx.TenantID 名下                                  …… 租户隔离 ✓
  → extension.MatchRoute（PathPrefix + Methods，首条命中）        …… 匹配 ✓
  → entry.limiter.Allow()（RateLimitPerSec>0 时令牌桶，超出 429）  …… 路由级限流 ✓
  → url.Parse + scheme 白名单（http/https；grpc:// → 502）        …… 后端解析 ✓
  → ValidateWebhookURL(target, allowPrivate=true)                 …… SSRF 复检 ✓（投递期重跑，防 DNS rebinding）
  → httputil.NewSingleHostReverseProxy(target).ServeHTTP(rw, r)   …… 转发
  → gwStatResponseWriter 捕获状态码 → defer 统计（错误数 + 平均延迟增量）…… 统计 ✓
```

## 2. 已有的安全债修复（历史账，都在代码注释里留了案底）

- **P0 鉴权**：本 handler 曾是**全仓唯一不做鉴权的反向代理**（无凭证可直通内部后端、绕过全部 RBAC，
  且聚合全租户路由天然跨租户）。现与 `handleServiceProxy` 对齐走同一套 `subsystemAuthorize`
  （五路身份：JWT / API Key / 网关注入角色 / 联邦验签 / demo），权限按方法分级（读 GET/HEAD/OPTIONS
  → `gateway:read`，其余 → `gateway:write`）。
- **租户隔离**：路由池从「全量聚合」改为「只取本租户」；创建时即以 `actx.TenantID` 为键。
- **SSRF 双检**：创建时校验 `validateGatewayTargetBackend`（scheme 白名单 + host:port 可解析）；
  **投递期复检** `ValidateWebhookURL(allowPrivate=true)`——域名 DNS 可随时变更（rebinding），
  持久化配置"创建时验过"不等于"此刻仍安全"。放行私网（网关后端本就是内网微服务）但硬拒
  链路本地/云元数据/0.0.0.0/8；拒绝原因只进服务端日志不回吐（防内网拓扑泄漏）。

## 3. 缺口清单（对照"完整数据面"该有的东西）

| # | 能力 | 现状 | 缺口的实际影响 |
|---|---|---|---|
| 1 | 熔断/离群摘除 | **无**（后端挂了每次都全量转发、吃满超时） | 后端故障时网关本身被拖垮（无 fast-fail） |
| 2 | 后端健康检查 | **无**（被动靠 502 发现） | 摘除靠人 |
| 3 | 重试策略 | **无** | 瞬时抖动直接透传给调用方 |
| 4 | 超时治理 | 用 ReverseProxy 默认值 | 慢后端占连接 |
| 5 | 负载均衡/多后端 | **单后端**（RouteRule.TargetBackend 一个 string） | 无多副本分发；改模型才能加 |
| 6 | 灰度/权重路由 | **无** | 无 canary 能力 |
| 7 | gRPC 数据面 | 创建时允许 grpc:// 但数据面 502 | 网关声明支持但实际不可用（**坑：模型与实现不一致**） |
| 8 | WebSocket | 未验证/未处理 | 长连接经 ReverseProxy 行为未定义 |
| 9 | 后端 mTLS | 无（明文 http 或普通 https） | 内网纵深缺一层 |
| 10 | 外部授权委托（OPA 等） | 无 | 策略只在本进程 RBAC |
| 11 | 请求/响应变换 | 无（除剥 /gw 前缀） | 无 header 注入/改写 |
| 12 | 统计持久化与跨副本聚合 | 内存计数器，重启清零、副本各自为政 | HA 下统计不可信 |
| 13 | 路由持久化 | **内存 map，重启全丢**（注释明示"MVP 内存实现"） | 生产不可用（配置消失=全量 404） |
| 14 | 观测集成 | 无 metrics 埋点（不走 `s.metrics`） | Prometheus 看不到网关 |

## 4. 升级为正式数据面的定价（若重新立项）

- **最小可用（生产门槛）**：#13 路由持久化（store 加一张表）+ #1 熔断（进程内断路器）+ #14 metrics 埋点
  —— 约 **1.5~2 天**；不动匹配引擎与鉴权（都已过安全债修复）。
- **中等**：+#2 主动健康检查、+#3 有界重试、+#4 超时配置化、+#12 统计经 store 聚合 —— 再 **2~3 天**。
- **大改**：+#5 多后端/负载均衡（RouteRule 模型改版 + 存量迁移）—— 模型变更是**破坏性**的，需单独定价。
- **不建议做**：#9 后端 mTLS、#10 外部授权委托、#7 gRPC 数据面——每项都是独立工程量，
  且生产正解（APISIX/Envoy）都自带。

## 5. 结论与建议

1. **维持 2026-09-05 决策不变**：预览形态、生产走 APISIX/Envoy。本勘测没有发现推翻该决策的新事实——
   现有缺口清单与当时判断一致，且安全债修复（鉴权/租户/SSRF）已完成，**预览形态没有安全隐患**。
2. **一处应该修的低垂果实**（升级立项前也值得修）：#7 的**模型与实现不一致**——创建路由时
   `validateGatewayTargetBackend` 允许 `grpc://`，数据面却 502。两条路二选一：创建时就拒绝 grpc
   （改白名单一行），或文档明示 grpc 仅存储不转发。**建议前者**（防"配了才发现不可用"）。
3. 若未来要立项：从 §4 的"最小可用"三件起步即可，**匹配引擎与鉴权无需动**（这两块质量是好的）。

---
*勘测人：zcode 线，2026-10-11。基准 `d1dfa7c`。只读，未改任何代码。*
