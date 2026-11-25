# ADR：AI 成本预算的持久化硬限（issue #150）

- 日期：2026-11-25
- 状态：提议（待评审）
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

预算成为**持久化、跨副本、并发安全的硬限**：用量落 SQL、重启不丢、多副本一致、超限在单原子操作内拒绝，且失败语义明确。不改预算配置 API 语义、不改告警阈值语义、不引入新计费模型。

## 方案

### 1. 接口新增「原子条件消费」

在 `cost.BudgetStore`（`internal/cost/handler.go` 的接口定义处）新增一个方法，把「检查 + 记账 + 时间窗重置」收敛为**单个原子操作**：

```go
// ConsumeIfUnderLimit 在单原子操作内完成：时间窗重置（日/月 rollover）→ 限额检查 → 累加。
// allowed=false 表示超限（未累加）；usage 返回操作后的 (usedToday, usedMonth) 供告警判定。
// 存储实现故障时返回 err（由调用方按失败语义处理）。
ConsumeIfUnderLimit(ctx context.Context, tenantID string, tokens int) (allowed bool, usage BudgetUsage, err error)
```

接口新增方法、**不改既有签名**；`IsOverBudget`/`RecordUsage` 保留供只读展示，AI 计费放行改走单原子调用。

### 2. SQL 实现：短事务 + 行锁（硬限正确性的关键）

`internal/store/sqlstore/sql_budget.go` 新增 `ConsumeIfUnderLimit`，用 `REPEATABLE READ` 事务 + `SELECT ... FOR UPDATE` 锁定 `budgets` 行：

1. `SELECT used_today, used_month, daily_limit, monthly_limit, last_reset, month_reset, alert_threshold FROM budgets WHERE tenant_id=? FOR UPDATE`（无行则按默认 limit 插入并锁定）。
2. 按 `last_reset`/`month_reset` 计算日/月 rollover，归零对应窗口用量。
3. 若 `used_today+tokens > daily_limit`（或 `used_month+tokens > monthly_limit`，且 limit>0）→ `allowed=false`，回滚（不累加）。
4. 否则 `UPDATE budgets SET used_today=?, used_month=?, last_reset=?, month_reset=? WHERE tenant_id=?`，提交，`allowed=true`。

行锁保证：同一租户同一时刻只有一个请求能完成「读-判-写」，并发/多副本下不会丢失更新、不会穿透硬限。行不存在时的并发首插用 `INSERT ... ON DUPLICATE KEY UPDATE` 或事务重试处理。

### 3. Memory 实现：互斥锁内同语义

`internal/cost/memory_budget_store.go` 新增 `ConsumeIfUnderLimit`，在同一把锁内做「重置→检查→累加」，语义与 SQL 一致，供 dev/单副本。

### 4. 接线：按主存储类型选择

`internal/controlplane/server.go:390` 改为：主存储为 SQL（`*store.SQLStore`）时 `NewBudgetManagerWithStore(sqlBudgetStore)`，否则回退 `NewMemoryBudgetStore()`。使 SQL 部署天然获得持久化硬限，dev/memory 形态不变。

### 5. AI 放行改单原子调用

AI 网关/计费放行点（现 `IsOverBudget` + `RecordUsage` 两步处）改为一次 `ConsumeIfUnderLimit`：`allowed=false` 即按超限拒绝（429），`err` 按下方失败语义处理。

## 失败语义（需评审拍板的点）

预算存储故障（DB 不可用）时，AI 计费请求如何处理：

- **推荐：fail-closed（拒绝，503 + 可重试）**。硬限的意义是防超支；存储不可用时放行等于放弃硬限。与既有 `storefail.Record` 可观测性一致，DB 恢复后自动恢复。
- 备选：fail-open（放行并记 warn），保可用性但放弃硬限——不推荐用于「硬限」定位。

评审需确认采用 fail-closed；若担心 AI 可用性与 DB 强耦合，可接受「仅预算消费类 AI 调用 fail-closed，只读展示 fail-open」的折中。

## 时间窗重置

日/月 rollover 在事务内按 `last_reset`/`month_reset` 与租户 `timezone` 计算：跨天则 `used_today=0`、跨月则 `used_month=0` 后再做限额检查，避免「月初/日凌晨被旧用量误判超限」。

## 兼容与迁移

- 034 表已在，无新迁移；接口新增方法不改既有签名。
- memory 实现保持 dev/单副本可用；SQL 部署行为变化=「预算从软提示变硬限」，属预期修复，写入 release-notes。

## 测试计划

1. **原子性**：N 并发 `ConsumeIfUnderLimit`（SQL + memory）总额恰好不超限、超限部分被拒且无丢失更新（used 精确等于限额）。
2. **跨副本**：两个 `BudgetManager` 共享同一 SQL，合计不超全局限额。
3. **重启装载**：消费后重建 manager，用量从 SQL 读回不丢。
4. **时间窗**：构造 last_reset 为昨日/上月，验证重置后再判。
5. **失败语义**：DB 断开时 `ConsumeIfUnderLimit` 返回 err，AI 放行按 fail-closed 拒绝并 `storefail.Record`。
6. **边界**：limit=0（不限）、恰好等于 limit、tokens 为 0/负（拒绝）。
7. **回归**：既有 `budget_enforce_test.go`/`budget_enforcement_test.go` 全绿；新增并发用例 `-race` 通过。

## 不做

- 不改预算配置/告警阈值 API 语义；不引入新计费维度（按次/按模型分桶另立）。
- 不做用量实时推送/前端大屏；不把 budget 读路径全部改成强一致（只读展示可接受最终一致）。
