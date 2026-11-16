# 服务发现后端立项书（etcd v3 / Consul）

> 本文按 `docs/projects/README.md` 的统一模板编写。事实以 `file:line` 或文档引用标注；
> 未落地的设计参数明确写「待复核」，不把提案写成现状。

## 1. 一句话定义

在现有 `discovery.ServiceDiscovery` 接口（已含 `MarkHealthy`）之后新增 **etcd v3** 与
**Consul** 两个后端，补齐 TTL / 健康语义、Watch 断线重连与
`--discovery-backend=memory|static|etcd|consul` 开关；InMemory 保持单机 / 开发默认，
发现面故障不得拖垮任务下发。

## 2. 背景与问题证据

### 2.1 接口已经定义，但动态后端与消费方都不存在

| 事实 | 证据 |
|---|---|
| `ServiceDiscovery` 五个方法：`Register` / `Deregister` / `List` / `Watch` / `MarkHealthy` | `internal/discovery/discovery.go:49-72` |
| `MarkHealthy` 已进接口：更新健康状态并通知 watcher；实例不存在返回 `ErrNotRegistered` | `internal/discovery/discovery.go:70-71,83-85` |
| `List` 契约：只返回健康实例、稳定顺序、无实例返回空切片 + nil | `internal/discovery/discovery.go:59-62`；`internal/discovery/inmemory.go:157-185` |
| `Watch` 契约：全量快照（非增量）；**channel 关闭表示发现后端不可用** | `internal/discovery/discovery.go:64-68` |
| 错误哨兵：`ErrNotImplemented` / `ErrNoInstances` / `ErrNotRegistered` | `internal/discovery/discovery.go:76,81,85` |
| 默认 Watch 刷新超时 30s | `internal/discovery/discovery.go:87-89` |
| 现有实现：`NoopDiscovery`、`StaticDiscovery`、`InMemoryDiscovery` | `internal/discovery/noop.go:14-46`；`static.go:11-26`；`inmemory.go:16-26` |
| InMemory 语义：按 `(Name, Addr, Port)` 幂等 upsert；Deregister / MarkHealthy 不存在返回 `ErrNotRegistered`；Watch 订阅即推快照，ctx 取消关闭 channel；通知为非阻塞发送 | `internal/discovery/inmemory.go:16-26,102-123,157-185,188-241,244-271` |
| Static 语义：从逗号分隔地址加载；每 30s 周期性推当前列表 | `internal/discovery/static.go:11-26,45-66,182` |
| Noop 语义：Register / Deregister / MarkHealthy 返回 `ErrNotImplemented`；List 空切片；Watch 返回已关闭 channel | `internal/discovery/noop.go:22-46` |

### 2.2 关键缺口：没有后端选择器，也没有生产消费方

| 缺口 | 证据 | 后果 |
|---|---|---|
| agent 只构造 `StaticDiscovery`：`setupDiscoveryBalancer` 把地址列表拼成 `NewStaticDiscoveryFromAddrs` | `internal/agent/agent.go:599-625` | etcd / Consul 即使实现也没有接线入口 |
| 配置项只有 `ControlplaneEndpoints`（服务发现入口地址），没有 backend 类型 | `internal/config/config.go:55-60`；`docs/operations.md:343-344` | 无法表达「用哪个发现后端」 |
| `List` 调用使用 `context.Background()`，无超时 | `internal/agent/agent.go:616` | 若未来换成 etcd / Consul，后端挂起会**阻塞 agent 启动**（当前 static 实现不会出错，是因为它纯内存） |
| `Watch` 与 `MarkHealthy` 在生产代码中**零调用方**（仅接口 / 实现 / 测试出现） | `internal/discovery/*.go` 全仓检索（2026-11-16） | 动态刷新链路从未验证；重连语义没有真实消费者 |
| InMemory 实现刚落地，没有独立测试文件 | `internal/discovery/` 仅有 `discovery_test.go`（Noop + Static）与 `balancer_test.go`；无 `inmemory_test.go` | 新后端没有共享契约用例保护 |
| go.mod 无 etcd / Consul 客户端 SDK | `go.mod` 直接依赖仅 MySQL / JWT / Redis / Kafka / gRPC / protobuf 等 | 引入后端 = 新增依赖与供应链面 |
| 另有一个同名但不同层的 `ServiceDiscoveryStore`（DB 持久化的服务实例注册表） | `internal/store/model/contract.go:424-430` | 本立项**不**改这层；避免把「agent 侧发现客户端」和「控制面服务实例表」混为一谈 |

### 2.3 为什么现在做

- 多控制面 HA 已具备静态地址 failover（`--controlplane-endpoints` + balancer），但静态配置
  无法感知实例上下线；扩容 / 缩容 / 滚动重启仍需改配置并重启 agent。
- 接口层已经把 `MarkHealthy` 和 `Watch` 定义完整（`discovery.go:49-72`），差的只是后端与接线；
  再拖下去会出现「接口以为有动态发现、实现只有 static」的语义漂移。
- ADR-4 明确 v1 单主库、不引入读副本与外部 MQ，理由是「本地/单机房部署无云资源，过早引入会让
  部署与故障面翻倍」（`docs/adr/2026-11-15-scale-decisions.md:68-91`）。因此 etcd / Consul
  必须是**可选后端**，不能成为默认依赖。

## 3. 目标（可验证）

1. **etcd v3 后端**：实现 `ServiceDiscovery` 五个方法；实例注册带 lease TTL 与 keepalive；
   `List` 只返回健康实例且顺序稳定；`Watch` 推全量快照。
2. **Consul 后端**：实现同一接口；健康状态以 Consul health / TTL check 为准；
   `List` 过滤不健康实例；`Watch` 推全量快照。
3. **共享契约测试**：同一套 conformance suite 对 memory / static / etcd / consul 四个后端跑
   相同断言；新增后端不写自己的「私有正确」。
4. **Watch 重连与健康语义**：后端暂时不可用时，Watch 以指数退避 + 抖动重连（建议 1s → 30s），
   重连期间不丢最近快照、不阻塞调用方；恢复后推一次全量快照。
5. **配置开关**：`--discovery-backend=memory|static|etcd|consul`（环境变量
   `OPSMESH_DISCOVERY_BACKEND`）；非法值启动即失败；etcd / consul 缺地址启动即失败。
6. **故障隔离**：发现后端不可用不得拖垮任务下发。agent 的 heartbeat / dispatch / cancel
   循环不被 discovery 调用阻塞；热路径只读本地缓存；刷新失败保留 last-known-good。
7. **默认不变**：单机 / 开发默认仍是 InMemory（零外部依赖）；多控制面静态地址场景仍可用
   `static`；etcd / Consul 仅作为显式 opt-in。

## 4. 非目标

- **不做 DNS-SD / mDNS**：不引入组播发现、`.local` 解析或局域网自动发现。
- **不做跨集群联邦发现**：不做多集群 / 多 region 的服务目录同步、跨集群故障转移。
- **不替换 Kubernetes 原生服务发现 / Service Mesh**：K8s 部署仍可用 EndpointSlice / Service；
  本项只服务「agent 找 controlplane」这一条链路。
- **不改 `ServiceDiscovery` 的方法签名**：不新增 `ListWithMeta` 之类并行接口；若确需扩展，
  先追加 ADR 并同步四个实现与契约测试。
- **不把 etcd / Consul 变成必选依赖**：默认部署不新增容器、不新增端口、不新增证书面。
- **不改 `internal/store` 的 `ServiceDiscoveryStore`**：控制面服务实例表是另一层，不在本项内。
- **不做服务网格能力**（mTLS 自动签发、流量治理、Connect / intentions）：只做地址发现与健康过滤。

## 5. 工作分解与里程碑

| 阶段 | 交付物 | 退出判据 | 预估 |
|---|---|---|---|
| **M1 etcd 后端 + 契约测试** | `internal/discovery/etcd.go`；key 方案（建议 `/opsmesh/services/<name>/<id>` → JSON）；lease TTL + keepalive；`--discovery-backend` 配置解析；**共享 conformance suite**（memory / static / noop 先接入） | etcd 后端在真实 etcd v3 上跑通 conformance；`List` / `Watch` / `MarkHealthy` 语义与 InMemory 一致；非法配置启动失败用例通过 | 4~6 人日 |
| **M2 Consul 后端** | `internal/discovery/consul.go`；服务注册 / 注销；TTL check 或 health API 过滤；Consul 地址与 token / TLS 配置 | Consul 后端在真实 Consul 上跑通同一 conformance suite；不健康实例不出现在 `List`；注销后 `ErrNotRegistered` | 3~5 人日 |
| **M3 重连 / 健康语义 + 故障注入** | Watch supervisor：指数退避 + 抖动、last-known-good 缓存、恢复后全量快照；List 超时与回退策略；agent 侧接线（启动刷新 + 后台刷新）；故障注入测试 | 停掉后端：`List` 在 1s 内返回（成功或按策略回退），`Watch` 不关闭 channel 且重连计数增长；恢复后快照自动追平；任务下发循环不中断 | 3~5 人日 |
| **M4 文档 + compose profile** | `docs/operations.md` 参数表；`docs/architecture.md` / `module-design.md` 发现链路更新；`deploy/docker/docker-compose.discovery.yml` 可选 profile（etcd / consul）；运维 runbook | 默认 `docker compose config` 不含 etcd / consul；显式 profile 才启动；参数表与代码默认值逐字一致 | 1~2 人日 |

**关键路径**：M1 的**共享契约测试**与 M3 的**重连 / 故障语义**。后端实现本身不是难点；
难点是「四个后端对同一份契约给出同样行为」，以及「后端挂掉时 agent 仍能下发任务」。

## 6. 验收标准 DoD

### 6.1 可执行命令

```bash
go build ./...
go test ./internal/discovery/... -count=1
go test ./internal/discovery/... -run TestServiceDiscoveryConformance -count=1
go test ./internal/discovery/... -run TestWatchReconnect -count=1
go test ./internal/discovery/... -run TestBackendFaultInjection -count=1
go test ./internal/agent/... -count=1
go test ./internal/config/... -count=1
```

外部后端用例的 CI 运行方式（service containers 或 compose profile）在 M1 确定；
没有 etcd / Consul 环境时必须显式 skip 并打印原因，不允许静默通过。

### 6.2 共享契约测试（四个后端同一套断言）

| 断言 | 适用后端 |
|---|---|
| `Register` 幂等：同一 `(Name, Addr, Port)` 重复注册不产生重复实例，更新 Metadata / Healthy | 全部 |
| `Deregister` 不存在返回 `ErrNotRegistered`；存在则移除并通知 watcher | 全部 |
| `List` 只返回健康实例；顺序稳定；无实例返回空切片 + nil | 全部 |
| `Watch` 订阅立即收到当前全量快照；变更后收到全量快照（非增量） | 全部 |
| `MarkHealthy(false)` 后实例从 `List` 消失；`MarkHealthy(true)` 后恢复；不存在返回 `ErrNotRegistered` | 全部 |
| `Watch` 的 ctx 取消后 channel 关闭，且不泄漏 watcher | 全部 |
| Noop 的 Register / Deregister / MarkHealthy 返回 `ErrNotImplemented`；Watch 立即关闭 | noop |
| 后端不可用时 `List` 按策略回退（last-known-good 或静态地址），且在有界时间内返回 | etcd / consul |
| 后端重启后 `Watch` 自动重连并推全量快照；重连次数可观测 | etcd / consul |

> 当前缺口：InMemory 没有独立测试文件，且 `Watch` / `MarkHealthy` 无生产消费方。
> conformance suite 落地时先把 InMemory 纳入，再写 etcd / Consul，避免「新后端有测试、默认后端没有」。

### 6.3 故障注入判据

| 场景 | 期望 |
|---|---|
| etcd / Consul 进程停止 | agent 不退出；任务领取 / 心跳 / 结果上报继续；`List` 在 1s 内返回 last-known-good 或按策略回退；日志中出现可检索的 WARN |
| 后端恢复 | `Watch` 在退避上限（≤ 30s）内重连；重连后推一次全量快照；无重复 watcher |
| 后端网络分区（连接超时） | 单次刷新有超时上限；不因一次超时清空本地实例表；连续失败进入退避 |
| 后端返回空列表 | 按「空列表是合法结果」处理，不立即清空 last-known-good；连续 N 次空列表后才降级（N **待复核**） |
| 配置错误（未知 backend / 缺 endpoints / 坏 token） | 启动失败并给出明确错误；不静默回退到 memory |

### 6.4 配置判据

| 输入 | 期望 |
|---|---|
| 不传 `--discovery-backend` | 默认 `memory`（单机 / 开发）；若显式给 `--controlplane-endpoints`，向后兼容映射到 `static`（**待复核**优先级） |
| `--discovery-backend=memory` | 零外部依赖；进程内注册表可用 |
| `--discovery-backend=static` | 从 `--controlplane-endpoints` / `--control-addrs` / `--control-addr` 加载 |
| `--discovery-backend=etcd` | 必须给 etcd endpoints；否则启动失败 |
| `--discovery-backend=consul` | 必须给 Consul address；否则启动失败 |
| `--discovery-backend=bogus` | 启动失败，退出码非 0 |

## 7. 依赖与前置条件

| 依赖 / 前置 | 现状 | 需要谁 | 阻塞里程碑 |
|---|---|---|---|
| etcd v3 客户端 SDK（`go.etcd.io/etcd/client/v3`） | 未引入（`go.mod` 无直接依赖） | owner 批准依赖 + 许可证审查 | M1 |
| Consul 客户端 SDK（`github.com/hashicorp/consul/api`） | 未引入；仅间接存在部分 hashicorp 库（Vault 传递依赖） | owner 批准依赖 + 许可证审查 | M2 |
| etcd / Consul 测试实例 | 仓库无现成 service container | CI / 部署线 | M1 / M2 |
| `--discovery-backend` 配置解析与校验 | 无 | 开发 | M1 |
| agent 侧 Watch 消费方 | 无（`Watch` 零调用） | 开发 | M3 |
| `List` 超时与 last-known-good 策略 | 无（当前 `context.Background()`） | 开发 + owner 决策 | M3 |
| TTL / 健康检查参数默认值 | 未定（建议 etcd lease 15s、keepalive 5s；Consul TTL 15s） | owner 决策 | M1 / M2 |
| 许可证与供应链审查 | 仓库有 `docs/license-compliance-deliverable.md` 等既有流程 | owner / 合规 | M1 前 |
| 按 `AGENTS.md` 先 CLAIM 再动文件 | 流程要求 | 执行线 | 全程 |

需要 owner 明确决策的项：

1. **是否批准新增两个客户端 SDK**（依赖体积、许可证、SBOM 影响）。
2. **支持矩阵**：etcd / Consul 是「实验性可选」还是「生产支持」；若生产支持，需要哪套 SLA。
3. **故障策略**：后端不可用时是「保留 last-known-good 继续派发」（推荐，保任务面）还是
   「快速失败」（保一致性）；两者对任务下发的影响完全不同。
4. **TTL / 健康检查参数**：lease TTL、keepalive 周期、Consul check 间隔、空列表降级阈值 N。
5. **默认值兼容规则**：`memory` 作为默认后，显式 `--controlplane-endpoints` 是否自动映射
   `static`；两者同时出现时谁优先（**待复核**当前 config 合并逻辑）。
6. **是否接受 etcd / Consul 作为可选部署单元**：ADR-4 的「不翻倍部署面」原则仍然有效，
   本项只能以 opt-in profile 形式落地。

## 8. 风险与缓解（含回滚）

| 风险 | 触发信号 | 缓解 | 回滚 |
|---|---|---|---|
| 发现后端故障拖垮任务下发 | agent 启动卡住；dispatch / heartbeat 循环停止；任务积压 | 热路径只读本地缓存；`List` 有界超时（≤ 1s）；刷新失败保留 last-known-good；静态地址兜底；故障注入用例进门禁 | `--discovery-backend=memory` 或 `static` + 重启 agent |
| Watch 断线后不恢复 / 重连风暴 | 实例列表长期不更新；后端起停时连接数暴涨 | 指数退避 + 抖动（1s → 30s）；单实例单 watcher；重连计数与告警 | 关闭动态发现，回 static |
| 陈旧实例被继续派发 | 实例已下线但 TTL 未到期 | TTL + 主动 Deregister + health check；`List` 只返回健康实例；调用方 failover 兜底 | 缩短 TTL；回 static |
| 空列表误清空 | 后端返回空列表 | 空列表是合法结果但不立即清空 last-known-good；连续 N 次后才降级（N 待定） | 回退到静态地址 |
| 新依赖供应链 / 许可证问题 | `go.sum` 体积增长；license 扫描告警 | 固定版本；许可证审查；不引入非必要传递依赖 | 移除后端实现，接口与默认不变 |
| 配置默认值改变导致多控制面失效 | 升级后 agent 不再读静态地址 | 兼容映射：显式 endpoints 未给 backend 时仍走 static；发布说明写明 | 显式设 `--discovery-backend=static` |
| etcd quorum 丢失 | 写入失败、lease 无法续约 | 已有实例在 last-known-good 中保留；不因续约失败立即清空；告警 | 回 static；恢复 etcd 后重连 |
| Consul blocking query 悬挂 | Watch goroutine 不返回 | 每次请求带 deadline；supervisor 超时后重建 | 关闭 Consul 后端 |
| 与 `ServiceDiscoveryStore` 混淆导致重复实现 | 两套注册表各写一份 | 文档与代码注释明确分层；本项只动 `internal/discovery` | 不涉及数据迁移 |

**全局回滚**：切回 `memory` / `static` 并重启即可；etcd / Consul 中的数据是外部注册信息，
不承载 OpsMesh 业务数据，回滚不需要迁移。保留 `ServiceDiscovery` 接口不变，回滚不涉及代码回退。

## 9. 工作量估算（人日 + 关键路径）

| 阶段 | 人日 |
|---|---|
| M1 etcd 后端 + 契约测试 | 4~6 |
| M2 Consul 后端 | 3~5 |
| M3 重连 / 健康语义 + 故障注入 | 3~5 |
| M4 文档 + compose profile | 1~2 |
| **合计** | **11~18 人日** |

- **关键路径**：M1 的共享契约测试 → M3 的重连 / 故障语义。后端 API 调用本身是薄封装；
  真正耗时的是「四个后端行为一致」与「后端挂掉时任务面不中断」的验证。
- **不确定性**：etcd / Consul 的测试环境搭建（CI service containers、TLS / token）可能额外
  增加 1~2 人日；Consul 健康检查的语义映射（client 能否安全地 `MarkHealthy`）需在 M2 前**待复核**。
- **不包含**：生产环境 etcd / Consul 集群的部署、容量规划、备份恢复；这些属于运维项目，
  本项只交付客户端与可选 compose profile。

## 10. 状态与决策记录

**当前状态：待批准**
**负责人：待定**

| 日期 | 决策 / 事件 | 出处 |
|---|---|---|
| 2026-11-15 | ADR-4：v1 单主库、不引入读副本与外部 MQ；避免部署与故障面翻倍 | `docs/adr/2026-11-15-scale-decisions.md:68-91` |
| 2026-11-16 | `ServiceDiscovery` 已含 `MarkHealthy`；InMemory 后端落地（无独立测试） | `internal/discovery/discovery.go:70-71`；`internal/discovery/inmemory.go` |
| 2026-11-16 | 本立项书提交，状态「待批准」 | 本文 |
| 待定 | 依赖审批、支持矩阵、故障策略、TTL 参数、默认值兼容规则 | 本文 §7 |

> 本项不改变 ADR-4 的结论：etcd / Consul 是 opt-in 的可选后端，默认仍是 InMemory；
> 若未来要让动态发现成为生产默认，需要追加新 ADR，而不是在本立项书里悄悄改默认。
