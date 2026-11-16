# TD-66 项目章程：API 网关数据面（`/gw/`）升级

> **状态：待批准**（T1 收口建议批准；T2 默认不启动）
> **负责人：待定**
> **日期：2026-11-17**
> **依据**：`docs/tech-debt.md:161`（TD-66 行）、`docs/td66-gateway-survey-2026-10-11.md`、`docs/td66-gateway-upgrade-scope-2026-10-11.md`
> **代码基准**：`main@9c828b8f`（本文只读核对，未改代码、未提交）

---

## 1. 一句话定义

在维持 `/gw/` **控制面预览形态**的前提下，把已落地的四个 MVP 能力补齐到“内部轻量路由编排可用线”；**不把本项目当作升级为完整 L7 数据面的默认延续**——那是一次重新立项。

## 2. 背景与问题证据

### 2.1 已记录的决策（不可被本项目静默推翻）

- **2026-09-05 决策**（`docs/tech-debt.md:161`）：`/gw/` 数据面被**明确声明为控制面预览形态**；路由规则 CRUD/统计是产品能力，数据面转发是 demo 级；完整数据面不在当前路线图——**生产流量应走 APISIX/Envoy 等正式网关**；升级为正式数据面**需重新立项**。
- **2026-10-11 勘测维持该决策**：`docs/td66-gateway-survey-2026-10-11.md:73-78` 明确“维持 2026-09-05 决策不变”，只建议修一处低垂果实（gRPC 模型/实现不一致）。
- **2026-11-05 立项书再次确认**：`docs/projects-2026-11-05.md:299-303`——“完整数据面（多后端/gRPC/OPA/WebSocket/主动健康检查）**不立项**，除非 owner 明确要替代外部网关”。

### 2.2 已落地事实（可执行核对）

| 事实 | 证据 |
|---|---|
| 数据面管线：鉴权 → 租户隔离 → 匹配 → 限流 → SSRF 双检 → 转发 → 统计 | `internal/controlplane/gateway.go:522-691`；7 步勘测见 `docs/td66-gateway-survey-2026-10-11.md:17-29` |
| 路由持久化 P1 | 提交 `27ea6879`；迁移 `internal/store/sqlstore/migrations/022_gateway_routes.sql:12-28`；store 契约 `internal/store/model/contract.go:444-450` |
| 熔断 P2 | 提交 `0f57052f`；`internal/extension/circuit.go:20-23`（阈值 5、冷却 30s）；接入 `gateway.go:615-619`、`gateway.go:679-689` |
| Prometheus 埋点 P3 | 提交 `0f57052f`；`internal/metrics/gateway_metrics.go:10-11`（`opsmesh_gateway_requests_total` counter + `opsmesh_gateway_latency_ms` gauge） |
| 超时治理 P4 | 提交 `0f57052f`；`RouteRule.TimeoutMs`（`internal/extension/gateway.go:32`）；`ResponseHeaderTimeout`（`gateway.go:638-651`） |
| gRPC 低垂果实 M0 | 提交 `a6289782`：创建时即拒 `grpc://`（`gateway.go:43-52`、`gateway.go:62-64`）；回归 `gateway_test.go:106` |
| 验收记录 | `docs/td66-gateway-upgrade-scope-2026-10-11.md:113-125` 五条全勾；真进程 16 项逐条过 |
| 测试现状（本次实测） | **39 条**网关相关用例：controlplane 23（gateway 11 + circuit 7 + persistence 5）· extension 12 · metrics 4 |

### 2.3 勘测后新发现的残余缺口（本次只读复核，M1/M2 必须收口）

1. **两条回载路径不一致，熔断器可能静默失效**：`gateway_persistence.go:71-76` 的 data-plane 懒加载只填 `rule` + `limiter`，**不填 `breaker`**；而 `gateway.go:152-159` 的 list 路径填了 `breaker`。重启后若 `/gw/` 请求先于 `GET /api/v1/gateway/routes` 到达，entry 的 breaker 恒为 nil（`loaded=true` 后不再重建），而 `gateway.go:615`、`gateway.go:680-688` 都以 `entry.breaker != nil` 为前提 ⇒ **该租户的熔断在进程生命周期内静默失效**。现有 `gateway_persistence_test.go` 只覆盖 list 路径，未覆盖 data-plane 首请求顺序（本次 `rg breaker gateway_persistence_test.go` = 0 命中）。**待复核：M1 必须用两条加载顺序的用例活体钉死。**
2. **多副本缓存不失效**：`gatewayState.loaded` 一旦为 true 不再重新加载（`gateway.go:90-95`、`gateway.go:547-555`）；A 副本新建的路由在 B 副本上不可见，直到 B 重启。立项文档只用 Prometheus 解决了统计跨副本（`td66-gateway-upgrade-scope-2026-10-11.md:54`），未解决配置跨副本。
3. **超时只约束响应头**：`ResponseHeaderTimeout`（`gateway.go:651`）不限制已建立响应体的持续时间；慢速流式后端仍可长期占用连接，与 P4 验收“不占连接”存在语义差。
4. **延迟指标是 gauge 不是 histogram**：`gateway_metrics.go:11` 明确“gauge（最近一次）”，与立项文档 P3 的“直方图”（`td66-gateway-upgrade-scope-2026-10-11.md:42`）不一致，无法计算 P95/P99。
5. **`route=":noroute"` 被标签清洗成 `:other`**：`gwLabel` 只允许字母数字与 `._~-`（`gateway_metrics.go:42-58`），冒号不在白名单；“无路由”与其它非法标签值混在同一时序里。
6. **文件头注释滞后**：`gateway.go:21-23` 仍写“MVP 内存实现……不持久化”，与 P1 已落地矛盾；属文档漂移，不是运行时缺陷。

> 第 1、2 条为本次仅凭静态阅读发现，未做活体复现；按“待复核”记账，验收必须以测试钉死。

## 3. 目标

| # | 目标 | 可验证口径 |
|---|---|---|
| G1 | **定位不漂移** | 本草程、部署文档与 UI 文案一致表述“预览/内部轻量路由编排；生产对外流量走 APISIX/Envoy” |
| G2 | **T1 正确性收口** | 重启回载的路由必带熔断器；多副本配置在 30s 内可见；均有自动化用例 |
| G3 | **可观测达标** | 每路由有 requests_total（route/tenant/status）+ 延迟 histogram（可算 P95/P99）+ 熔断状态指标 |
| G4 | **超时语义完整** | `timeoutMs` 同时约束响应头与响应体总时长；超时稳定返回 504 |
| G5 | **14 项缺口全部归档** | T1/T2/T3 分层台账逐项有状态；T3 项不得进入实现 |
| G6 | **变更受决策门约束** | 任何 T2/完整数据面工作，必须先有 owner 书面批准与需求证据 |

## 4. 非目标

- **不做 L7 全功能网关**：认证插件、JWT 校验、流量镜像、WAF、请求/响应改写均不做。
- **不做多租户计费/计量**：网关不产生账单口径。
- **不替代 APISIX/Envoy**：对外生产流量仍走正式网关；本项目只服务内部轻量路由编排。
- **不做 OPA/外部授权委托、WebSocket、主动健康检查**（T3）。
- **不在本期内把 `RouteRule.TargetBackend` 改为多后端列表**（T2，破坏性模型变更，需单独批准）。
- 不新增 gRPC 数据面转发能力；`grpc://` 保持创建时拒绝。
- 不改变既有鉴权/租户隔离/SSRF 语义（勘测确认质量好，`docs/td66-gateway-survey-2026-10-11.md:31-41`）。

## 5. 工作分解与里程碑

| 阶段 | 交付物 | 退出判据 | 估算 |
|---|---|---|---|
| **M0 一致性修复（gRPC）** | 创建时拒绝 `grpc://`；模型/文档与实现一致 | `TestHandleCreateGatewayRoute_RejectGrpcBackend` 绿；存量 `grpc://` 路由有明确处置说明 | 已落地（`a6289782`）；回归 0.1 人日 |
| **M1 T1 持久化/熔断回载** | 回载构造 breaker；两条加载顺序（list 先 / `/gw/` 先）都覆盖；多副本缓存失效机制（TTL/版本号/watch 三选一） | 两条顺序下均为：新建路由 → 重启 → 连续 5×5xx → 第 6 次 503；A 副本新建 → B 副本 ≤30s 可见 | 2–3 人日 |
| **M2 T1 可观测/超时** | 延迟 histogram（bucket）、熔断状态指标、`timeoutMs` 覆盖响应体、`route=":noroute"` 可区分 | `/metrics` 出现 `_bucket` 与熔断状态序列；`timeoutMs=1000` + 2s 慢响应 ⇒ 504；`:noroute` 不被折叠成 `:other` | 2–3 人日 |
| **M3 T2 决策门** | T2 需求证据包（谁需要重试/多后端/灰度）+ 成本与风险 | owner 明确批准或明确否决；未批准则项目在此收口 | 0.5 人日 |
| **M4（条件）有界重试** | 仅幂等方法、有限次数/预算、可关闭 | 5xx/连接故障按预算重试；非幂等方法零重试；重试指标可观测 | 2–3 人日 |
| **M5（条件）多后端/灰度** | `TargetBackend` 列表模型、迁移、UI、权重 | 旧数据双读兼容；迁移可回滚；灰度按权重生效 | 4–6 人日 |

> **M3 是硬门**：未通过则 M4/M5 不启动。M4/M5 的估算不计入本期基线。

## 6. 验收标准（DoD）

| # | 判据 | 执行方式 |
|---|---|---|
| 1 | 现有网关用例零回归 | `go test ./internal/controlplane -run TestGateway -count=1`；`go test ./internal/controlplane -run TestHandleGateway -count=1`；`go test ./internal/extension -run TestMatchRoute -count=1`；`go test ./internal/metrics -run TestGateway -count=1` |
| 2 | 熔断 fast-fail | 假后端连续 5 次 500 ⇒ 第 6 次请求 **≤1s 内 503**，后端零再打（`TestGatewayCircuitOpensAfterFiveFailures` + 真进程复跑） |
| 3 | 半开恢复 | 冷却 30s 后放行 1 个试探；后端 200 ⇒ 状态回 `closed`（`TestGatewayCircuitHalfOpenProbeRecovers`） |
| 4 | 重启保留路由且熔断仍生效 | `TestGatewayRoutesSurviveStateReset` 扩展 + 新增 data-plane 首请求用例：list 先 / `/gw/` 先两种顺序下，装载的 entry 必有 breaker；连续 5×5xx 后第 6 次 503 |
| 5 | metrics 时序可见 | `curl -s http://127.0.0.1:<port>/metrics \| grep 'opsmesh_gateway_requests_total{'` 返回 route/tenant/status 标签；延迟 histogram 出现 `_bucket`/`_sum`/`_count` |
| 6 | 超时返回 504 | `timeoutMs=1000` + 后端 sleep 2s ⇒ 1s 即 504（`TestGatewayRouteTimeoutIsApplied`） |
| 7 | gRPC 不再“配了才发现” | `POST /api/v1/gateway/routes` 带 `grpc://` ⇒ 400（`TestHandleCreateGatewayRoute_RejectGrpcBackend`） |
| 8 | 鉴权/租户/SSRF 不变 | `TestHandleGatewayProxy_RequiresAuth` / `_TenantIsolation` / `_RejectsMetadataTarget` 全绿 |
| 9 | 多副本一致性（M1 新增） | 双实例共享 MySQL：A 创建路由，B 最迟 30s 内命中；删除同理 |
| 10 | 竞态 | `go test -race ./internal/controlplane ./internal/extension ./internal/metrics` 无告警 |

## 7. 依赖与前置条件

| 依赖 | 状态 | 说明 |
|---|---|---|
| store 迁移机制 + `022_gateway_routes.sql` | 已有 | P1 已落地；T1 不新增表 |
| MySQL 真库（持久化/多副本用例） | 已有 CI integration 模式 | 本机需 DSN；内存后端仅作兜底 |
| `s.metrics` 注册表 | 已有 | `internal/metrics/gateway_metrics.go` |
| **owner 决策 D1：批准 T1 收口** | **待批准** | 本草程即申请 |
| **owner 决策 D2：T2 是否立项** | **待批准/默认否** | 多后端是破坏性模型变更 |
| owner 决策 D3：维持预览定位 | 已定（2026-09-05） | 本项目不申请变更 |
| 多副本缓存失效方案选型 | 待定 | TTL / 版本号 / watch，M1 开工前定 |

## 8. 风险与缓解（含回滚）

| 风险 | 影响 | 缓解 | 回滚 |
|---|---|---|---|
| 需求漂移成“完整网关” | 工期从人日级膨胀到人月级 | M3 硬门 + 非目标清单；L7 需求一律新立项 | 停在 M2 即收口 |
| 多后端模型破坏兼容 | 存量路由不可读 | 双读/迁移脚本/灰度；旧字段保留一个版本 | 回退迁移 + 恢复 `022` 表结构 |
| 熔断误判 | 正常业务 5xx 被当后端故障 | 4xx 不计失败（`circuit.go:10-11`）；阈值/冷却可配 | 调高阈值或关闭熔断（配置项） |
| 指标基数膨胀 | Prometheus 内存压力 | route ID 为管理员配置的有界集合；`gwLabel` 截断/白名单 | 删除该路由指标或降标签 |
| 多副本失效不彻底 | 配置漂移 | M1 以双实例用例验收；失效窗口写入文档 | 回退到单副本部署 |
| 超时改总时长误杀流式响应 | 长响应被截断 | 区分 header/body 超时；流式路由可单独放宽 | 恢复仅 header 超时 |

## 9. 工作量估算

| 范围 | 人日 |
|---|---|
| M0 回归守护 | 0.1 |
| M1 持久化/熔断回载 | 2–3 |
| M2 可观测/超时 | 2–3 |
| M3 决策门 | 0.5 |
| **T1 收口合计（含 M0）** | **4.6–6.6** |
| M4 重试（条件） | 2–3 |
| M5 多后端（条件） | 4–6 |
| **含 T2 合计** | **10.6–15.6** |

**关键路径**：`M1 → M2 → M3`。M1 的回载 breaker 是正确性缺陷，优先级高于任何新功能。

## 10. 状态与决策记录

- **当前状态：待批准**（T1 建议批准；T2 默认不启动）。
- **负责人：待定**。
- **定位声明**：本项目是“预览形态的可用线收口”，**不是**“升级为完整数据面”的延续；后者必须重新立项，并重新论证为何不直接用 APISIX/Envoy。

| 日期 | 决策/事件 | 记录 |
|---|---|---|
| 2026-09-05 | 预览形态；生产走 APISIX/Envoy；升级需重新立项 | `docs/tech-debt.md:161` |
| 2026-10-11 | 只读勘测：14 缺口 + 三档定价；维持决策 | `docs/td66-gateway-survey-2026-10-11.md` |
| 2026-10-11 | MVP P1–P4 立项并全部验收 | `27ea6879` + `0f57052f`；`docs/td66-gateway-upgrade-scope-2026-10-11.md` |
| 2026-11-05 | 再次确认完整数据面不立项 | `docs/projects-2026-11-05.md:299-303` |
| 2026-11-16 | MVP 保留，14 项缺口范围外 | `docs/tech-debt.md:161` 末注 |
| 2026-11-17 | 本草程起草：T1 收口立项申请；T2 待决策 | 本文件（待批准） |

### 附：14 项缺口分层台账

| # | 能力 | 分层 | 现状/处置 |
|---|---|---|---|
| 1 | 熔断/离群摘除 | **T1 必做** | 已落地（P2），回载断层待修（M1） |
| 2 | 后端健康检查 | **T3 不做（主动）** | 被动摘除由熔断覆盖；主动探测明确不做 |
| 3 | 重试策略 | **T2 值得做** | 条件项 M4，需幂等/预算设计 |
| 4 | 超时治理 | **T1 必做** | 已落地 header 超时；总时长待补（M2） |
| 5 | 负载均衡/多后端 | **T2 值得做** | 破坏性模型变更，条件项 M5 |
| 6 | 灰度/权重路由 | **T2 值得做** | 随多后端模型；本期不做 |
| 7 | gRPC 数据面 | **T2 值得做（一致性先行）** | 模型/实现不一致已由 **M0** 收口（`a6289782`，创建时拒绝）；真正的 gRPC 转发仍需需求证据，本期不做 |
| 8 | WebSocket | **T3 不做** | 无消费方；生产正解由 APISIX/Envoy 提供 |
| 9 | 后端 mTLS | **T3 不做** | 内网纵深由正式网关/服务网格承担 |
| 10 | 外部授权（OPA） | **T3 不做** | 无消费方；不在预览网关内建策略引擎 |
| 11 | 请求/响应变换 | **T3 不做** | 属 L7 全功能网关，非目标 |
| 12 | 统计持久化/跨副本 | **T3 不自建** | Prometheus 抓各副本已覆盖；内存统计只作本地展示 |
| 13 | 路由持久化 | **T1 必做** | 已落地（P1），多副本缓存待修（M1） |
| 14 | 观测集成 | **T1 必做** | 已落地 counter+gauge；histogram/熔断状态待补（M2） |

---

*起草：2026-11-17。只读核对基准 `main@9c828b8f`；未改代码、未提交。*
