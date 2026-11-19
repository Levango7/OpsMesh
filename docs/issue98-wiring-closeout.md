# issue #98「写了没人接」7 条真缺口 — 收口记录

> 核验日期：2026-11-18。逐条对照当前 main 源码。

| # | 缺口 | 状态 | 证据 |
|---|---|---|---|
| 1 | AI 网关 4 个 LLM Provider 未接线 | ✅ 已合 PR #113 (`3cb1331a`) | `OPSMESH_AI_GATEWAY_PROVIDERS` JSON 装配 + 四 Provider 工厂 + 启动 fail-fast + 四消费方接线 |
| 2 | gpu-svc AscendCollector 未接线 | ✅ 本轮接线 | `GPU_SVC_ACCELERATOR=ascend` → `SetAscendCollector` → `GetGPUMetrics` 优先 npu-smi，失败回退 |
| 3a | alertengine MetricsProvider 未接线 | ✅ 已合 PR #112 (`3d3f8f08`, issue #101) | `deviceMetricsProvider` 接 store，规则可真实触发 |
| 3b | cost BudgetStore 未接线 | ✅ 本轮接线 | `MemoryBudgetStore` 实现 BudgetStore 接口，`NewBudgetManager(store)` 接上 |
| 3c | controlplane TraceService | ⚠️ 假阳性 | `trace.go` 的 TraceService 是响应 DTO（handler 返回 `[]TraceService{}`），非未接线服务 |
| 3d | k8slifecycle LifecycleJobStore | ⚠️ 假阳性/已接线 | `NewK8sLifecycleHandler(sqlStore)` 已传真实 store，类型作为参数结构满足接口 |

## 本轮新增改动
- `services/gpu-svc`：AscendCollector 接线（config.Accelerator + Service.SetAscendCollector + GetGPUMetrics 优先/回退 + 3 测试）
- `internal/cost`：MemoryBudgetStore（实现 BudgetStore 5 方法 + 2 测试），server.go 的 sharedBudgetManager 接上

## 边界
- MemoryBudgetStore 是进程内实现：解决"接口写了没人接"，不解决跨副本共享（需 SQL 实现同一接口）
- AscendCollector 默认关闭（`GPU_SVC_ACCELERATOR` 未设时不启用），不改变现有 GPU 采集行为
