# ADR：AI 成本预算的持久化硬限（issue #150）

- 日期：2026-11-25
- 状态：已接受（2026-11-28；实现期根据真实 token 结算约束修订第 5 节）
- 关联：#150（v0.19 预算持久化硬限）、#98（BudgetStore 接口接线）、#98 后续 issue98-wiring-closeout

## 背景与现状（钉在代码上）

AI 成本预算的数据结构、SQL 存储与迁移**早已存在**：

- `internal/store/sqlstore/sql_budget.go`：`SQLStore` 实现 `GetBudget`/`UpsertBudget`/`ListBudgets`/`RecordAlert`/`ListAlerts`；`internal/store/store.go:159` 断言 `_ BudgetStore = (*SQLStore)(nil)`。
- `internal/store/sqlstore/migrations/034_budgets.sql`：`budgets`（tenant_id 主键、daily/monthly limit、used_today/used_month、alert_threshold、last_reset/month_reset）与 `budget_alerts` 表。
- `internal/cost/memory_budget_store.go`：进程内 `MemoryBudgetStore`（map + 锁）。

**但控制面强制走内存实现**：`internal/controlplane/server.go:390` 无条件 `cost.NewBudgetManager(cost.NewMemoryBudgetStore())`，与主存储是否 MySQL 无关。

强制点的记账是**读-改-写（RMW）**：`internal/cost/handler.go` 的 `BudgetManager.RecordUsage`（:254）先 `store.GetBudget` 取回、在内存结构体里累加 `usedToday/usedMonth`、再 `store.UpsertBudget` 写回；限流判定 `IsOverBudget`（:377）读的是同一可能过期的内存值。

## 问题

1. **不持久**：预算用量在进程内 map，重启即丢 ⇒ 重启后限额重置，可反复超支。
2. **不跨副本**：多副本各自一份内存 ⇒ 全局限额被放大 N 倍。
3. **RMW 非原子**：两副本/两请求并发各读 used=100、各自 +1 写回 101 ⇒ 丢失更新，硬限在并发下被穿透。
4. **判定与记账分离**：`IsOverBudget` 与 `RecordUsage` 是两步，间隔内用量可变 ⇒ 检查通过≠实际放行。

即「有预算功能」≠「有硬限」。当前只是进程内软提示。

## 目标（v0.19）

预算成为**持久化、跨副本、并发安全的后续消费硬限**：用量落 SQL、重启不丢、多副本一致；达到额度后所有副本都在真正调用付费 Provider 前拒绝后续请求，且失败语义明确。真实 token 只在响应后可知，不做估算预留，也不承诺并发在途请求绝不超额。不改预算配置 API 语义、不改告警阈值语义、不引入新计费模型。

## 方案

### 1. 接口新增「原子条件消费」

在 `cost.BudgetStore`（`internal/cost/handler.go` 的接口定义处）新增一个方法，把「检查 + 记账 + 时间窗重置」收敛为**单个原子操作**：

```go
// ConsumeIfUnderLimit 在单原子操作内完成：时间窗重置（日/月 rollover）→ 限额检查 → 累加。
// allowed=false 表示超限（未累加）；usage 返回操作后的 (usedToday, usedMonth) 供告警判定。
// 存储实现故障时返回 err（由调用方按失败语义处理）。
ConsumeIfUnderLimit(ctx context.Context, tenantID string, tokens int) (allowed bool, usage BudgetUsage, err error)
```

另新增 `AddUsage` 记录已经发生的真实 token（越限也入账，并在同一原子操作中判定告警与写去重标记），以及 `UpdateBudgetLimits`/`UpdateBudgetTimezone` 基于存储最新行更新配置，禁止陈旧快照回滚用量。接口新增方法、不改既有方法签名；`IsOverBudget` 保留为付费前新鲜预检，`RecordUsage` 保留为真实用量结算入口。

### 2. SQL 实现：短事务 + 行锁（硬限正确性的关键）

`internal/store/sqlstore/sql_budget_consume.go` 用 `REPEATABLE READ` 事务 + `SELECT ... FOR UPDATE` 锁定 `budgets` 行：

1. 读取完整预算行并加锁；无行时用方言 `InsertIgnore` 首插默认额度后重读，已存在行绝不被默认值覆盖。
2. 按 `last_reset`/`month_reset` 计算日/月 rollover，归零对应窗口用量并清理相应告警去重标记。
3. 条件消费若 `used+tokens > limit`（且 limit>0）则不加 tokens，但可提交已经发生的时间窗归一；真实结算 `AddUsage` 无论是否越限都累加实际 token。
4. 基于刚才加锁读取的最新行写回并提交；配置更新同样走该行锁，不能写回调用方的旧用量。

行锁保证同一租户同一时刻只有一个请求能完成「读-判-写」，并发/多副本下不丢失更新；负数、负存量与整数溢出在写入前拒绝。

### 3. Memory 实现：互斥锁内同语义

`MemoryBudgetStore` 与 `store.MemoryStore` 在同一把锁内实现条件消费、真实用量累加、配置更新与告警去重，语义与 SQL 一致，供 dev/单副本。

### 4. 接线：按主存储类型选择

`internal/controlplane/server.go:390` 改为：主存储为 SQL（`*store.SQLStore`）时 `NewBudgetManagerWithStore(sqlBudgetStore)`，否则回退 `NewMemoryBudgetStore()`。使 SQL 部署天然获得持久化硬限，dev/memory 形态不变。

### 5. AI 付费前新鲜预检 + 真实用量原子结算（实现期修正）

Provider 返回前无法知道实际 token，`MaxTokens` 又是可选字段；用估算值预留会改变现有计费语义，用 `tokens=0` 调条件消费则只是伪装成硬限。故 Gateway 保留两阶段边界，但收紧每一步：

1. 真打钱前 `IsOverBudget` 每次都读取 SQL 最新预算，命中热缓存也不能跳过；已越限或 DB 故障均拒绝，其他副本的入账立即可见。
2. Provider 返回后 `RecordUsage` 调 `AddUsage`，把真实 token 无条件原子入账；最后一次响应可以越过额度，但后续所有副本都会拒绝新消费。
3. `AddUsage` 在同一行锁事务内完成日/月告警判定与去重标记，`RecordAlert` 只持久化已判定事件，不再全量 upsert 预算快照。
4. 不做估算预留，不承诺并发在途请求绝不超额；该边界写入 release-notes，不把「后续消费硬限」夸大成「绝对用量封顶」。

## 失败语义（已拍板）

预算存储故障（DB 不可用）时，AI 计费请求如何处理：

- **采用 fail-closed**：付费前预算读取失败即拒绝；真实结算失败返回 false，不用本地缓存伪装成功。与既有 `storefail.Record` 可观测性一致，DB 恢复后自动恢复。
- 只读展示可保留最终一致口径；但不能反哺付费预检。

## 时间窗重置

日/月 rollover 在事务内按 `last_reset`/`month_reset` 与租户 `timezone` 计算：跨天则 `used_today=0`、跨月则 `used_month=0` 后再做限额检查，避免「月初/日凌晨被旧用量误判超限」。

## 兼容与迁移

- 034 表已在，无新迁移；接口新增方法不改既有签名。
- memory 实现保持 dev/单副本可用；SQL 部署行为变化=「预算从软提示变硬限」，属预期修复，写入 release-notes。

## 测试计划

1. **原子性**：N 并发 `ConsumeIfUnderLimit`（SQL + memory）总额恰好不超限、超限部分被拒且无丢失更新（used 精确等于限额）。
2. **跨副本真实结算**：两个 `BudgetManager` 共享同一 SQL，不丢更新；越限响应如实入账，两个副本的下一次预检都拒绝。
3. **重启装载**：消费后重建 manager，用量、配置和告警去重标记从 SQL 读回不丢。
4. **时间窗**：构造 last_reset 为昨日/上月，验证重置后再判，且不重复告警。
5. **失败语义**：DB 断开且本地缓存已热时预检仍 fail-closed，真实结算返回 false。
6. **边界**：limit=0（不限）、恰好等于 limit、tokens 为 0/负、负存量与整数溢出。
7. **陈旧快照**：配置更新、告警写回和并发首插均不得回滚另一副本已入账的用量。
8. **回归**：既有 Gateway 预算测试全绿；新增并发用例与真 MySQL 集成均 `-race` 通过。

## 不做

- 不改预算配置/告警阈值 API 语义；不引入新计费维度（按次/按模型分桶另立）。
- 不做用量实时推送/前端大屏；不把 budget 读路径全部改成强一致（只读展示可接受最终一致）。
