# A3-3 UPSERT 方言化设计（2026-11-24）

> 状态：设计已按 owner 批准的范围收口，待落实现计划。  
> 基线：`main@11bfa94bb1cc96e7dc05373e9df8f9fa3512ecce`。  
> 分支：`feat/p0-a3-upsert-dialect`，隔离工作树 `F:/Nexus/OpsMesh-wt-a3-3`。  
> 交付：一个 PR，内部按域分批提交；不修改 `.github/workflows/`。

## 1. 目标与边界

把 `internal/store/sqlstore` 的 48 条真实 MySQL `ON DUPLICATE KEY UPDATE` 写语句收口为方言-aware 的 UPSERT：

- MySQL 保持既有语义、参数顺序和写后结果；不承诺 SQL 文本逐字节不变。
- PostgreSQL 输出合法 `INSERT ... ON CONFLICT (...) DO UPDATE SET ...`，直接生成 `$n` 占位符和双引号标识符。
- 不在运行时重写 SQL，不引入通用 SQL AST 或迷你 DSL。
- 本批只收口 UPSERT 语句本身。周围读写面的 `?`、反引号、分页等仍属 A3-2；本批完成不得宣称“PG 全业务读写可用”。
- 不纳入 `internal/controlplane`、`services/*`、`internal/cmdb` 的 UPSERT。
- 达梦/金仓专用驱动另立，不承诺“全栈国产 DB”。

审计基线为 48 处真实 SQL、33 个生产文件。按最终语义形态划分为：

- **45 处通用 UPSERT**：`col=VALUES(col)`、常量覆盖、`NOW()` 覆盖均可由生成器表达。
- **3 处特例**：
  1. `RenewLeadership`：条件表达式，且 MySQL 从左到右赋值。
  2. `UpsertABACPolicyRecord`：`version=version+1` 原子自增。
  3. `CreateLifecycleJob`：依赖 MySQL 列上的隐式 `ON UPDATE CURRENT_TIMESTAMP(3)`。

## 2. 方案选择

### 采用：结构化生成器 + 少量特例适配

在 `Dialect` 增加一个有消费者的方法：

```go
Upsert(table string, cols, values, conflict, updateCols []string) string
```

- `cols`：INSERT 列名。
- `values`：与列一一对应的 SQL 片段，只使用 `?`、受控常量或 DB 中立函数（如 `NOW()`）；业务值仍通过 `ExecContext` 参数传入。
- `conflict`：PostgreSQL 的显式冲突目标；MySQL 不使用，但调用点必须显式声明，便于静态核对。
- `updateCols`：冲突时需要覆盖的列；生成 MySQL `VALUES(col)` 或 PG `EXCLUDED.col`。
- 返回值是完整 SQL；参数 slice 由调用方按原顺序传入，接口不接管参数。

MySQL 输出示例：

```sql
INSERT INTO `t` (`a`, `b`) VALUES (?, ?)
ON DUPLICATE KEY UPDATE `a`=VALUES(`a`)
```

PostgreSQL 输出示例：

```sql
INSERT INTO "t" ("a", "b") VALUES ($1, $2)
ON CONFLICT ("id") DO UPDATE SET "a"=EXCLUDED."a"
```

实现约束：

- 标识符统一按方言引用；表名和列名均来自代码常量，不接受用户输入。
- PG 先以 `?` 组装完整语句，再调用现有字面量感知的 `ConvertPlaceholders`，保证跨多个 value 片段连续编号。
- 生成器不验证空 slice 或做运行时 SQL 重写；调用点正确性由 AST 守卫和真库 PREPARE 测试保证。
- 三个特例放在独立方言适配文件中，业务文件不再出现裸 UPSERT SQL。

### 不采用

- **运行时正则重写**：无法可靠处理字符串、注释、标识符和参数编号，也不能静态证明冲突目标正确。
- **通用表达式 AST / 迷你 DSL**：48 处中只有三处需要表达式能力，抽象成本远高于收益。

## 3. 冲突目标与唯一约束

### 3.1 目标策略

- 单主键表：使用主键列。
- 联合主键表：使用完整联合主键。
- `tenants`：使用 `id`。函数契约明确“按 ID 幂等”。
- `cloud_resources`：使用 `(cloud_account_id, resource_type, resource_id)`。生产调用方把云资源自然键作为业务身份；主键 `id` 在当前生产路径中也取 `resource_id`。
- `leader_lease`、`abac_policies`、`lifecycle_jobs` 的特例分别使用 `id`、`(tenant_id,id)`、`id`。

完整映射见附录 A。

### 3.2 多唯一键行为差异

MySQL `ON DUPLICATE KEY UPDATE` 可命中任意唯一键；PG 单个 `ON CONFLICT (target)` 只能处理指定冲突目标。因此：

- `tenants` 在 PG 下若“新 ID + 已存在 name”冲突，将返回唯一键错误，不再静默更新另一行后返回新 ID。
- `cloud_resources` 在 PG 下若“相同 ID + 不同自然键”冲突，将返回唯一键错误。
- 上述 PG 行为更严格，避免假成功和身份不一致；MySQL 既有行为不改。
- MySQL/PG 字符串排序规则不同，名称类唯一键的大小写/尾空格判定仍可能有差异，本批不声称完全一致。

### 3.3 常驻守卫

新增测试同时完成：

1. 从生产 Go AST 收集全部 `s.dialect.Upsert(...)` 调用，要求五个参数均为字面量，拒绝空冲突目标。
2. 从 MySQL/PG 迁移 DDL 解析主键与唯一约束，比较 UPSERT 目标表的唯一列集合；MySQL 集合必须是 PG 集合的子集，且解析结果不得为空。
3. 对收集到的每条 UPSERT 在可用真库执行 `PREPARE`，验证表、列、冲突目标和语法真实存在。
4. 扫描业务 Go 文件，禁止裸 `ON DUPLICATE KEY UPDATE` 与裸 `ON CONFLICT`；只允许三个特例适配函数使用原始方言 SQL。

守卫必须自带窄变异测试：新增裸 SQL、错误 conflict target、漏 PG 唯一约束三种变异都要精确报红，还原后转绿。

## 4. 三个特例

### 4.1 `RenewLeadership`

MySQL 原逻辑的三个赋值都依赖同一个条件：

```text
E = 旧 expires_at < now
H = 旧 holder = 新 holder
```

MySQL 从左到右求值：第一项赋值后，后续 `holder=VALUES(holder)` 在条件成立时恒为真，条件不成立时恒为假。因此三个赋值都可归约为旧行条件 `E OR H`。PG 使用旧行与 `EXCLUDED` 的 `CASE WHEN` 与之等价：

```sql
ON CONFLICT (id) DO UPDATE SET
  holder = CASE WHEN leader_lease.expires_at < $4 OR leader_lease.holder = EXCLUDED.holder
                THEN EXCLUDED.holder ELSE leader_lease.holder END,
  expires_at = CASE WHEN leader_lease.expires_at < $5 OR leader_lease.holder = EXCLUDED.holder
                    THEN EXCLUDED.expires_at ELSE leader_lease.expires_at END,
  updated_at = CASE WHEN leader_lease.expires_at < $6 OR leader_lease.holder = EXCLUDED.holder
                    THEN EXCLUDED.updated_at ELSE leader_lease.updated_at END
```

`NULL` 条件下 MySQL `IF` 与 PG `CASE WHEN` 都走 ELSE，语义一致。

并发证明：UPSERT 对同一 `leader_lease(id=1)` 行加行锁并串行执行。过期租约下只有一个竞争者能成功改写；后续竞争者看到未过期的新 holder，条件为假，只做 no-op。读回 holder 后只有唯一实例判为主。

测试覆盖：

- 表为空时插入并持租。
- 本实例未过期续租。
- 其他实例未过期不能抢占。
- 过期租约可抢占。
- `holder/expires_at` 为 NULL 时不误判。
- 多实例并发抢占每轮恰好一个成功，连续多轮不双主。

### 4.2 `UpsertABACPolicyRecord`

PG 等价赋值：

```sql
version = abac_policies.version + 1
```

同一行锁保证并发自增不丢更新；`created_at` 不进入更新集合。新行仍由 INSERT 写 `version=1`，已有行每次写穿加一。

测试覆盖：插入、重复写穿、字段覆盖、`created_at` 不变、N 个并发写穿后版本恰好为 `N+1`。

### 4.3 `CreateLifecycleJob`

MySQL 的 `updated_at` 由列定义 `ON UPDATE CURRENT_TIMESTAMP(3)` 维护；PG 001 没有该隐式行为。直接机械翻译会导致 PG 更新时间不刷新。

处理方式：保留为第三个特例。

- MySQL 分支保留原 SQL，包括依赖列定义刷新 `updated_at`。
- PG 分支在 `DO UPDATE SET` 中显式增加 `updated_at=now()`。
- 不修改已应用的 PG 001 迁移，不引入版本号冲突的新迁移。
- 测试覆盖首次创建、同 ID 更新、`updated_at` 推进；MySQL no-op upsert 的时间行为保持原样。

PG 迁移顶部“全仓 UPDATE 均显式写 updated_at”的注释将订正为“UPSERT 的 lifecycle_jobs 由专用方言适配显式补齐”。

## 5. 时区与时间函数

`UpsertControlplaneInstance` 使用 `NOW()`；PG 的 `now()` 返回 `timestamptz`，写入 `timestamp without time zone` 时会按会话时区转换。为保证与 Go UTC 参数一致：

- PG `PrepareDSN` 强制设置 `TimeZone=UTC`，覆盖 URL query 中的同名参数。不能复用现有 `ensureDSNParam`：它只在该参数不存在时追加，遇到已有 `TimeZone` 会保留原值。
- 用 `net/url` 解析现有受支持的 URL 形态 DSN，替换 query 中的 `TimeZone`，其余参数原样保留。
- 本批不扩展 PostgreSQL keyword/value DSN：现有 `dialectForDSN` 只识别 `postgres://`、`postgresql://`、`pg://`，测试夹具也只接受 URL；避免把 DSN 识别扩张混入 UPSERT 改造。
- MySQL DSN 处理不变。
- `dialect_test.go` 的 PG DSN 恒等契约相应改为“保留其余参数并强制 UTC”。
- PG 真库测试执行 `SHOW TimeZone`，必须返回 `UTC`。

## 6. 错误与行数口径

- UPSERT 本身的 SQL 错误继续按原函数路径返回/记录，不吞错。
- 48 处 UPSERT 均不消费 `RowsAffected`，因此不要求两库行数相同：
  - MySQL 当前 `clientFoundRows=true`：常见为插入 1、改变 2、未改变 1。
  - PG：插入或更新均计 1。
- `SubmitResult` 后续三个任务状态 UPDATE 的 `RowsAffected` 分支保持不变，并补 done/retry/dead-letter 回归。
- `RowsAffected` 的差异写入 PR 说明，不把“行数相等”当验收条件。

## 7. 测试与验收

### 7.1 单元测试

- 两种方言的 SQL 形态、标识符引用、占位符连续编号。
- value 片段中的字符串字面量、双引号、反引号内 `?` 不误转。
- 常量与 `NOW()` 片段。
- 三个特例的 SQL 形态和参数数量。

### 7.2 静态与真库测试

- AST 调用点收集、冲突目标核对、唯一约束 parity。
- 全部 45 条通用 UPSERT + 3 条特例在 MySQL/PG 真库 `PREPARE`。
- 共享行为用例：插入 → 重复写 → 读回；复合键；常量覆盖；自然键；时间戳。
- 特例并发用例：领导租约不双主、ABAC 版本不丢更新。
- PG DSN 强制 UTC 与 `SHOW TimeZone`。

### 7.3 CI 与本地

- CI 不增加 PostgreSQL service；现有 Go 测试批次会运行静态守卫和 MySQL 真库用例。
- PG 用例继续由 `OPSMESH_TEST_PG_DSN` 门控；无 DSN 时明确 skip。
- 合 PR 前在本机 PG15 真库实跑全部 PG 门控用例，并在 PR 写明环境、版本、命令和结果。
- 不把 skip 当通过，不以“CI 绿”替代本机 PG 实测。

### 7.4 完成判据

- 业务生产 Go 文件中裸 `ON DUPLICATE KEY UPDATE` / `ON CONFLICT` 为 0。
- 通用调用点 45 个、特例 3 个，合计覆盖原 48 处。
- MySQL/PG UPSERT 目标唯一约束 parity 守卫绿，且三种窄变异均能报红。
- MySQL 真库行为回归绿；PG15 真库 UPSERT/PREPARE/并发用例绿。
- `go build`、`go vet`、`gofmt`、`python scripts/coord-gate.py` 通过。
- 一个 PR，required 全绿 + `MERGEABLE` + CLEAN + base == main tip 四条件齐后合并；合后 main 首验、补 DONE。

## 8. 不在本批

- A3-2 的占位符、反引号、分页、其余读写语句方言化。
- PostgreSQL CI service / workflow 修改。
- controlplane、services、cmdb 的 UPSERT。
- 国产数据库专用驱动。
- 版本发布、CHANGELOG 版本块改写。

## 附录 A：48 处 UPSERT 冲突目标

| # | 调用点 | 表 | 冲突目标 | 形态 |
|---:|---|---|---|---|
| 1 | `RenewLeadership` | `leader_lease` | `id` | 特例：条件 |
| 2 | `CreateLifecycleJob` | `lifecycle_jobs` | `id` | 特例：更新时间 |
| 3 | `UpsertControlplaneInstance` | `controlplane_instances` | `instance_id` | 通用 |
| 4 | `UpsertABACPolicyRecord` | `abac_policies` | `tenant_id,id` | 特例：自增 |
| 5 | `CreateAlertRule` | `alert_rules` | `id` | 通用 |
| 6 | `CreateAPIKey` | `api_keys` | `id` | 通用 |
| 7 | `CreateApp` | `argocd_apps` | `id` | 通用 |
| 8 | `CreateAutomationRule` | `automation_rules` | `id` | 通用 |
| 9 | `CreateAutomationExecution` | `automation_executions` | `id` | 通用 |
| 10 | `CreateBackup` | `backup_records` | `id` | 通用 |
| 11 | `CreateBillingPlan` | `billing_plans` | `id` | 通用 |
| 12 | `CreateSubscription` | `subscriptions` | `id` | 通用 |
| 13 | `CreateInvoice` | `invoices` | `id` | 通用 |
| 14 | `UpsertBudget` | `budgets` | `tenant_id` | 通用 |
| 15 | `CreateCloudAccount` | `cloud_accounts` | `id` | 通用 |
| 16 | `CreateCloudResource` | `cloud_resources` | `cloud_account_id,resource_type,resource_id` | 通用 |
| 17 | `SaveReport` | `compliance_reports` | `id` | 通用 |
| 18 | `SetConfig` | `configs` | `tenant_id,key_name` | 通用 |
| 19 | `Register` agents+secret | `agents` | `agent_id` | 通用 |
| 20 | `Register` agents-no-secret | `agents` | `agent_id` | 通用 |
| 21 | `Register` onboard | `devices` | `device_id` | 通用 |
| 22 | `Register` placeholder | `devices` | `device_id` | 通用 |
| 23 | `Register` demo-task | `tasks` | `task_id` | 通用 |
| 24 | `UpsertDevice` | `devices` | `device_id` | 通用 |
| 25 | `RegisterService` | `services` | `service_id` | 通用 |
| 26 | `SaveGatewayRoute` | `gateway_routes` | `tenant_id,id` | 通用 |
| 27 | `SaveK8sCluster` | `k8s_clusters` | `id` | 通用 |
| 28 | `CreateSilence` | `alert_silences` | `id` | 通用 |
| 29 | `CreateNotifyChannel` | `notify_channels` | `id` | 通用 |
| 30 | `CreateNotifyTemplate` | `notify_templates` | `id` | 通用 |
| 31 | `CreateNetworkDevice` | `network_devices` | `id` | 通用 |
| 32 | `CreateTemplate` | `pipeline_templates` | `id` | 通用 |
| 33 | `CreateRun` | `pipeline_runs` | `id` | 通用 |
| 34 | `CreatePlugin` | `plugins` | `id` | 通用 |
| 35 | `SetQuota` | `quota_configs` | `tenant_id` | 通用 |
| 36 | `SaveRefreshToken` | `refresh_tokens` | `token_hash` | 通用 |
| 37 | `CreateScript` | `scripts` | `id` | 通用 |
| 38 | `CreateSLO` | `slos` | `id` | 通用 |
| 39 | `SaveTaskRecord` | `task_records` | `tenant_id,kind,id` | 通用 |
| 40 | `SubmitResult` | `task_results` | `task_id` | 通用 |
| 41 | `CreateTask` | `tasks` | `task_id` | 通用 |
| 42 | `SaveOSTemplate` | `os_templates` | `id` | 通用 |
| 43 | `SaveMiddlewareTemplate` | `middleware_templates` | `id` | 通用 |
| 44 | `CreateTenant` | `tenants` | `id` | 通用 |
| 45 | `CreateTicket` | `tickets` | `id` | 通用 |
| 46 | `issueToken` | `install_tokens` | `token` | 通用 |
| 47 | `CreatePolicy` | `traffic_policies` | `id` | 通用 |
| 48 | `CreateWebhook` | `webhooks` | `id` | 通用 |

## 9. 实现结果登记（2026-11-24，Task 13 收口）

- 状态：实现完成。48 处真实 UPSERT = 45 处通用方言生成器调用 + 3 处语义特例（`upsert_special.go` 的 `upsertLeaderLease`/`upsertABACPolicy`/`upsertLifecycleJob`），业务生产文件裸 UPSERT SQL 为 0。
- 全部验收口径（§7.4）逐项在本机双真库实测达成：MySQL 8.4.13 / PostgreSQL 15.19；48 条 PREPARE 双方言共 96 条全过；并发不双主、ABAC 版本递增、lifecycle 时间戳结果 PASS；四种窄变异（裸 SQL、错误 conflict target、漏 PG 唯一约束、空集合保护）各自转红、还原后转绿。
- 完整命令、版本、PASS/skip 数与变异红绿对照见 `docs/pg-dialect-a3-2-a3-3-plan-2026-11-23.md` §6。
- 边界不变：本批不解决 A3-2 其余读写面，不宣称 PG 全业务读写可用；UPSERT 的 `RowsAffected` 不参与 MySQL/PG 等价判定。
- 计划文档订正：`docs/superpowers/plans/2026-11-24-a3-3-upsert-dialect.md` Task 2 的字面量问号示例由 `$2` 订正为 `$1`（引号内 `?` 不占占位符编号）；Task 9 的 PG leader-lease CASE 绑定 `$4`/`$5`/`$6` 三个不同下标（与实现一致，`upsert_special.go` 三处 CASE 分别绑定第 4/5/6 个 now 实参）。
