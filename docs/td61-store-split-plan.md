# TD-61 分批拆分方案：internal/store 依赖图与首批清单（2026-10-03）

> 背景：TD-61 登记时 controlplane/store 为 134/96 文件，现已长到 **87 / 109**（生产文件口径）。
> 直接 import internal/store 的文件 **129 个**——一次性搬迁即破 129 处编译，故本方案走「分域渐进（方案 C）」。

## 1. import 方按模块分布

| 模块 | 直接 import store 的文件数 |
|---|---|
| internal controlplane | 129 |

## 2. store 包内文件清单（按行数；"包内依赖"列为粗口径）

> **口径说明**：本表「包内依赖」是文件名共现的粗口径（存在同名误匹配），仅作排序参考，
> **不能作为搬迁依据**。真正动手前需按符号级 `go list -deps` 复核目标文件被外部引用的
> 具体符号集。

| 文件 | 行数 | 包内依赖 |
|---|---|---|
| sql_legacy.go | 36 | 2 |
| memory_agent_logs.go | 56 | 2 |
| sql_agent_logs.go | 56 | 2 |
| memory_bounds.go | 96 | 2 |
| memory_os_template.go | 96 | 2 |
| stub_guard.go | 97 | 2 |
| memory_plugin.go | 101 | 2 |
| memory_apikey.go | 124 | 2 |
| sql_tokens.go | 140 | 2 |
| memory_config.go | 145 | 2 |
| memory_secret.go | 156 | 2 |
| sql_audits.go | 178 | 2 |
| memory_discovery.go | 196 | 2 |
| sql_alerts.go | 266 | 2 |
| memory_billing.go | 293 | 2 |
| redis_session.go | 293 | 2 |
| memory_alertgov.go | 324 | 2 |
| sql_devices.go | 513 | 2 |
| sql_audit_chain.go | 615 | 2 |
| sql_tasks.go | 763 | 2 |
| store.go | 930 | 2 |
| memory.go | 1789 | 2 |
| multi_schema_agent_logs.go | 35 | 3 |
| memory_compliance.go | 110 | 3 |
| memory_k8s.go | 110 | 3 |
| memory_middleware_template.go | 115 | 3 |
| memory_tenant.go | 119 | 3 |
| memory_backup.go | 131 | 3 |
| memory_argocd.go | 155 | 3 |
| sql_secret.go | 167 | 3 |
| memory_traffic.go | 174 | 3 |
| memory_slo.go | 190 | 3 |
| memory_ticket.go | 194 | 3 |
| memory_network.go | 221 | 3 |
| memory_webhook.go | 225 | 3 |
| memory_script.go | 228 | 3 |
| sql_templates.go | 229 | 3 |
| memory_pipeline.go | 244 | 3 |
| sql_discovery.go | 254 | 3 |
| memory_automation.go | 263 | 3 |
| memory_rbac.go | 308 | 3 |
| sql_rbac.go | 616 | 3 |
| models.go | 792 | 3 |
| sql.go | 993 | 3 |
| multi_schema.go | 1530 | 3 |
| memory_refresh.go | 113 | 4 |
| sql_k8s.go | 125 | 4 |
| sql_refresh.go | 158 | 4 |
| multi_schema_p03.go | 210 | 4 |
| session.go | 266 | 4 |
| failures.go | 304 | 4 |
| multi_schema_p6.go | 364 | 4 |
| sql_quota.go | 81 | 5 |
| multi_schema_p3.go | 115 | 5 |
| multi_schema_p1.go | 137 | 5 |
| sql_compliance.go | 142 | 5 |
| sql_backup.go | 147 | 5 |
| multi_schema_p5.go | 167 | 5 |
| multi_schema_p4.go | 210 | 5 |
| sql_config.go | 216 | 5 |
| multi_schema_p2.go | 245 | 5 |
| sql_m2.go | 495 | 5 |
| sql_plugin.go | 166 | 6 |
| sql_argocd.go | 195 | 6 |
| sql_tenant.go | 200 | 6 |
| sql_traffic.go | 234 | 6 |
| sql_apikey.go | 238 | 6 |
| sql_ticket.go | 247 | 6 |
| sql_network.go | 259 | 6 |
| sql_script.go | 282 | 6 |
| sql_slo.go | 289 | 6 |
| sql_webhook.go | 290 | 6 |
| sql_automation.go | 341 | 6 |
| sql_pipeline.go | 342 | 6 |
| sql_billing.go | 530 | 6 |

## 3. 首批建议（各批独立绿，批间可停）

- **批次 1（低风险起步）**：自洽度最低的一组文件先搬（见上表头部），每批控制在 **≤10 个 import 方**；
  搬迁 = 新建子包 + 改 import + 旧包留 type alias 过渡（一个发布周期后再删）。
- **批次 2 起**：按主题域推进（alerts / automation / k8s / devices / audit / compliance …），
  每批顺带补该域的 store 测试——**搬迁与补测合并做**，这是本次拆分真正的收益来源。
- **不做**：一次性全搬（破 129 处）、以及「同包分文件」的零收益改法。

## 4. 前置条件核对

| 前置 | 状态 |
|---|---|
| 依赖图（本文件） | ✅ 已产出 |
| 分批策略 | ✅ 本文第 3 节 |
| 每批独立绿 | ✅ 靠 CI（build-test + services 逐模块 vet/lint/-race） |
| type alias 过渡层 | 首批随做随定 |

> 结论：**现在可开工**，按批次 1 起步；每批独立提交、独立绿。

## 5. 批次 1 开工后的结构性发现（推翻本文档第 3 节的「按域搬」建议）

批次 1 开工第一步做了符号级复核，得到外部引用面最小的三份文件，实测它们的形态：

| 文件 | 外部引用 | 实际形态 | 能否抽子包 |
|---|---|---|---|
| `sql_legacy.go` (36 行) | 0 | `func (s *SQLStore) initSchemaExtra(ctx)` —— **SQLStore 的方法** | ✗ 方法无法用 type alias 过渡 |
| `memory_bounds.go` (96 行) | 0 | 自由函数，但引用私有类型 `metricsRing` / `deviceMetricsSeq` | ✗ 子包反向依赖主包 = 循环 |
| `stub_guard.go` (97 行) | 0 | 自由函数，但引用域常量集合（joinStubDomains） | ✗ 同上 |

**根因**：`internal/store` 的组织方式是**两个中心类型（MemoryStore / SQLStore）的方法集**，
不是自由函数集合。方法跟着类型走、私有类型与常量被同包文件共享 —— 因此：
- 「按域抽子包 + type alias 过渡」这条路线在方法集层面**根本不可行**（alias 只能过渡
  类型与函数，过不了方法）；
- 零外部引用的文件也不等于可抽：它们与主包之间有**内部依赖**（私有类型/常量），
  抽出去即成循环依赖。

### 修正后的可行形态（二选一，按批次推进）

- **形态 A（先按后端拆，推荐）**：`internal/store/memory`（全部 memory_*.go，MemoryStore
  方法集完整）+ `internal/store/sqlstore`（全部 sql_*.go）。方法集不被拆开，只有**共享
  类型与构造函数**需要上提到父包或 neutral 包。外部 129 个 import 方只感知父包的
  类型别名（`store.MemoryStore = memory.MemoryStore` 这类**类型别名**可以用，
  过渡期零改动）。**这是唯一能做到「机械迁移 + 编译期全程有信号」的路径。**
- **形态 B（先解耦后拆域）**：先把私有类型/常量（metricsRing、域常量集）抽到 neutral 包，
  再按域拆。改动面更大，但域边界最终更干净。

**批次 1 的执行结论**：不执行「按域搬」（会破编译且无过渡手段）；先出**形态 A 的
搬迁清单**（哪些文件进 memory/sqlstore、哪些共享符号需上提、上提后的父包别名面），
供下一批动手。
## 6. 形态 A 搬迁清单（实测数据，2026-10-03）

| 分类 | 文件数 | 内容 |
|---|---|---|
| `memory_*.go` | 26 | MemoryStore 方法集 |
| `sql_*.go` | 32 | SQLStore 方法集 + 迁移框架（`sql.go`） |
| 共享/其他 | 17 | `models.go`（**43 个领域类型**）、`store.go`（**29 个 Store 接口 + 全量 Store**）、`multi_schema*.go`（8 个包装层）、`session.go`/`redis_session.go`（SessionStore）、`failures.go`、`stub_guard.go` |

**关键实测结论**：

1. **跨后端类型耦合 = 0** —— `sql_*` 不引用 `memory_*` 的任何定义，两端可各拆一子包、彼此无依赖。形态 A 技术上成立。
2. **必须留在父包的**：`store.go`（29 个 Store 接口是契约，两端实现它）、`multi_schema*`（8 个聚合包装）、`session*`、`failures`、`stub_guard`、4 个构造函数（`NewMemoryStore`/`NewSQLStore`/`NewMultiSchemaStore`/`NewRedisSessionStore`）——**外部 129 个 import 方对 `store.X` 零感知**。
3. **搬迁的真实成本点**：`models.go` 的 43 个类型被两端共用。若下沉到中性包（如 `internal/store/model`），memory/sqlstore 子包 import 它、父包用**类型别名**回导（`type User = model.User` 别名合法且零外部改动）——但两个后端子包内 58 个文件里所有 `User`/`Task`/`Role` 这类短名都要加 `model.` 前缀。**这一步不能用 `gofmt -r` 做**（它基于标识符匹配，会误伤同名局部变量），需要 gopls rename 或人工逐文件——这是形态 A 的第一个真实卡点，也是批次 1 之后要解决的工具前提。
4. 可独立推进的小切口候选：`session.go`/`redis_session.go`（SessionStore，与两端无关联）与 `failures.go`（StoreFailure 统计，仅 /admin/store-failures 端点消费）——外部引用面待测，若各 ≤10 处即为低风险起步批次。
## 7. 批次3（后端拆包）执行配方——2026-10-04 快核

前置状态：批次1（storefail）与批次2（model）已落地并 CI 绿。

**障碍与解法（实测）**：

1. `recordStoreFailure` 在后端文件内 **302 处** → 机械替换为 `storefail.Record` + import
   （批次1 抽出的子包正是无环出口，无须再解耦）；
2. `var _ Store = (*MemoryStore)(nil)` 类编译期断言引用父包 `Store` 接口 →
   断言行从后端文件**删除**、在父包集中重建（一行/后端），否则子包 import 父包成环；
3. `MemoryStore`/`SQLStore`/`DB` 命中含**定义者自身**（memory_*.go 定义其方法必命中）——
   判归属时须区分 definer/consumer，不能按计数一刀切。

**执行顺序**（每步独立绿）：memory 批（26 文件 + memory.go 类型定义）先做 →
sql 批（32 文件 + sql.go）后做 → multi_schema 包装层最后。

**开工条件**：另一会话的 36 文件现场（含 10 个 internal/controlplane 文件）已提交——
58 文件搬迁不与活跃现场同树混做。

---

## 8. 批次 3-memory 执行记录（2026-10-09，已落地）

### 8.1 先修正 §6 的一个错误结论

§6 写的「**跨后端类型耦合 = 0**」**是错的**（当时的判据只看了「文件名共现」，没做符号级
反向引用检查）。符号级实测的真实共享面（memory 定义、sql/multi_schema 消费）：

| 共享面 | 定义处（原） | 消费方 |
|---|---|---|
| token 签名/随机串/bcrypt（BcryptHash/RandHex/MustRandHex/HashToken/VerifyTokenMAC/RandAlertRuleID） | memory.go | sql.go / sql_tokens.go / sql_rbac.go / sql_alerts.go / sql_devices.go / multi_schema.go + 测试 |
| 设备指标环形缓冲与内存上限（MetricsRing/NewMetricsRing/Evict…/MaxTracked…/AppendAgentLogBounded/MaxAgentLog…） | memory.go / memory_middleware_template.go / memory_bounds.go | sql.go / sql_devices.go / sql_agent_logs.go + 测试 |
| RBAC 权限目录（PermSpecs/RolePermissions） | **sql_rbac.go**（反向：memory 在消费 sql 的定义） | memory_rbac.go + 控制面（store.RolePermissions） |
| SLI 求值（MetricFieldFor/EvaluateSLI/metricColumn） | slo_eval.go / **sql_slo.go** | memory_slo.go |
| 领域 helper（13 个 CloneXxx + 27 个 RandXxxID + SortServiceInstances） | memory_*.go | sql_*.go（深拷贝返回语义、ID 分配）、multi_schema_p6.go |
| 哨兵错误（ErrRefreshTokenHashRequired） | memory_refresh.go | sql_refresh.go |
| 契约（36 个接口 + Store） | store.go（**WithDemo 签名 `WithDemo(bool) Store` 引用接口自身**） | memory/sql/multi_schema 三实现 |
| 随契约的数据类型（QuotaConfig/Usage/AuditChainVerifyResult） | store.go / **sql_audit_chain.go** | 契约 + memory + sql |

结论：**子包化之前必须先做三段下沉**，否则 memory 子包要么 import 父包（成环）、
要么引用 sql 侧定义（跨后端反向依赖）。

### 8.2 实际执行顺序（与 §7 的差异）

§7 列的三个障碍里，①（recordStoreFailure 302 处）与 ②（`var _ Store` 断言）
在实测中都不构成障碍：memory 侧 recordStoreFailure 仅 **1 处**（302 处全在 sql_*），
而断言块本来就在父包（store.go 尾部）。

真正的工作量按依赖顺序展开，每步都是「机械搬迁 + 编译器枚举」：

1. **`internal/store/storekit`（共享内核）**：crypto.go（6 个导出函数）+ metricsring.go
   （MetricsRing/NewMetricsRing/MetricsRingDefaultCap/MaxTrackedDeviceMetrics/EvictDeviceMetricsIfNeeded，
   方法导出为 Add/Latest/Since）+ bounds.go（MaxAgentLogReports/MaxAgentLogLines/AppendAgentLogBounded）。
2. **契约下沉 `internal/store/model`**：`contract.go`（36 接口，**裸类型名零限定**——契约与领域类型
   同包是选 model 而非新 contract 包的原因）+ `quota.go`/`audit.go`（3 个数据类型）。
   父包 store.go 变薄为「36 个类型别名 + 原断言块」；`WithDemo(bool) Store` 签名因别名而逐字不变，
   memory 子包以 `type Store = model.Store` 引用同一具名类型——**这是本批最关键的一步**。
3. **领域 helper 上提 model**：克隆（clone.go）/ID 生成（idgen.go）/权限目录（perm.go）/
   SLI 求值（slo_eval.go）/哨兵错误（errors.go）。两端引用加 `model.` 前缀（memory 87 处、
   父包 75 处），脚本 + 编译器双重校验；父包侧不设包装（直接限定，sql 批搬走时无需再改）。
4. **memory 拆包**：26 个生产文件 + 7 个测试文件 `git mv` 为 `internal/store/memory/`；
   包声明改 `package memory`；`recordStoreFailure(`→`storefail.Record(`；内核符号限定 `storekit.`；
   `internal/store/memory/aliases.go` 提供 50 个模型类型别名 + 常量 + 归一化包装 + `Store` 别名
   （**别名而非加前缀**：零标识符改写、零字符串误伤，方法签名与契约逐字一致）。
5. **父包回导**：`memory_shim.go`（MemoryStore 别名 + NewMemoryStore 薄包装）、
   `kernel_shim.go`（过渡件：sql/multi_schema/测试仍用旧短名，随 sql 批删除）、
   models_shim.go 扩为 model 中性层的统一回导层（数据别名 + RolePermissions/SupportedSLIMetrics 公共 API 包装）。

### 8.3 测试侧的两条实测规律

- **测试随被测私有面走**：断言私有字段/私有方法的用例（metricsRing 的 capacity、MemoryStore 的
  publish/auditCap、环内部 writeSeq）在拆包后父包不可见 ⇒ 迁入对应子包
  （`storekit/metricsring_test.go`、`memory/publish_internal_test.go`、`memory/audits_cap_test.go`）；
  断言可用公共 API 表达的则**改写为公共面表达**（如用写入顺序替代 writeSeq=0 的人为造旧，
  用例反而更确定）。
- **测试替身按包各自持有**：Go 跨包测试不能 import 别的包的 `_test.go`，
  被父包测试共用的替身（recordingBus/countDevices）在父包补一份（`parent_test_helpers_test.go`），
  与子包内的同名定义刻意重复、互不牵制。

### 8.4 验证口径与结果

`go build ./...` / `go vet ./...` 全绿；`go test -count=1 ./internal/... ./cmd/... ./pkg/...` 全绿；
`golangci-lint run ./internal/store/... ./internal/controlplane/...` 0 issues；
`-race` 复跑见 CI（build-test 作业）。**129 个 import 方零改动**（父包 store.X 公共面签名全部保留，
含 store.RolePermissions / store.SupportedSLIMetrics / store.IsValidSLIMetric 三个公共 API）。

### 8.5 下一步（批次 3-sql 的精确清单，2026-10-09 侦察已核）

**规模**：43 个 `sql*.go`（32 生产 + 11 测试）+ `sql.go` + **`migrations/` 目录**。

**本轮侦察新发现的硬约束（动手前必读）**：

1. **`migrations/` 必须随 `sql.go` 一起搬**——`sql.go:29` 是 `//go:embed migrations/*.sql`，
   embed 模式不能跨目录（`..` 非法）。随之要改的功能型引用（非注释）：
   - `deploy/docker/scripts/deploy.sh:1507`（`for f in internal/store/migrations/[0-9]*.sql` 循环）
   - `deploy/scripts/verify-runtime.sh:968`（`count_sql "${ROOT}/internal/store/migrations" no`）
   - `internal/cmdb` 的可空列门禁（TD-74 记录「cmdb 门禁改读 `../store/migrations/*.sql`」——
     **跨线文件**，需按协调板先通知）
   - 文档/CHANGELOG 内的历史叙述可保留（非功能引用），新 runbook 引用需同步。
2. **`TestMain` 与共享临时库**：`audit_chain_shared_test.go`（package store 的 `TestMain` + 共享
   `*SQLStore` + DROP 清理）供审计链集成用例复用；拆包时 TestMain 与共享库助手要跟 SQL 用例走
   （Go 每包只能一个 TestMain，且跨包不可见）。
3. **静态门禁测试随源文件走**：`nullable_scan_guard_test.go` 同时 `Glob("migrations/*.sql")` 与
   `Glob("*.go")` 逐 `.Scan(` 站点判定——它必须与 sql 源文件同目录，否则判定面退化为「只剩父包残留
   .go」的假绿。
4. **父包残留消费方**（搬完 sql 后仍需处理）：
   - `multi_schema.go`（8 个包装文件，按方案留最后）：`NewSQLStore` 薄包装 + `SQLStore` 别名即可，
     `dsnForSchema`/`ensureSchemaExists`/`validateIdent` 本就定义在 multi_schema.go，不受影响；
   - `internal/controlplane/factory/server_factory.go:140` 调 `store.NewSQLStore(...)` ——
     公共面保持不变（薄包装），**客户端零改动**；
   - `failures_test.go`（3 个用例，全部 `&SQLStore{db: ...}` 构造）→ 随 SQL 包迁移；
   - `store_extra*_test.go` 是混装件：SQL 侧用例（extra2/extra3/extra5/constructor/rbac_*）迁移，
     memory 侧的已在批次 3-memory 迁走，剩下的父包级别用例保留。
5. **连带删除**：`kernel_shim.go`（sqlstore 直接 import storekit；multi_schema 的少量调用点
   就地改 `storekit.` 限定）、`failures_shim.go`（同时把 3 个 controlplane 消费方改为直接
   import storefail——按协调板约定，动手前已发通知）。

**执行配方**（与 3-memory 同款，已被验证）：
`git mv`（含 migrations）→ 包声明改 `package sqlstore` → 机械替换（`recordStoreFailure(`→
`storefail.Record(`、内核短名→`storekit.` 限定、`errRefreshTokenHashRequired` 已在 model）→
`sqlstore/aliases.go`（50 个模型类型别名 + 常量 + `type Store = model.Store` + `rbacPermSpecs`/
`RolePermissions` 回导）→ 父包 `sql_shim.go`（`type SQLStore = sqlstore.SQLStore` +
`func NewSQLStore(...)` 薄包装）→ 编译循环（编译器枚举残留）→ `go test`/lint/`-race`。

**顺序要求**：本批会让**根模块暂时不可编译**（`git mv` 到编译修复完成之间），而
`services/*/go.mod` 通过 `replace` 指向根模块——**尽量一轮做完再停**，
不要在中间态隔夜（同树还有并行会话）。

---

## 9. 批次 3-sql 执行记录（2026-10-09，已落地）

### 9.1 实际搬迁面（比 §8.5 预估多两类）

| 分类 | 数量 | 去向 |
|---|---|---|
| `sql*.go` 生产文件（含 `sql.go` 迁移框架） | 33 | `internal/store/sqlstore/` |
| **`migrations/` 目录** | 21 对 .sql/.down.sql | `internal/store/sqlstore/migrations/`（`//go:embed` 不能跨目录） |
| SQL 侧测试（整搬，含 `TestMain` 共享临时库） | 14 | sqlstore |
| 混合测试拆分（SQL 段下沉 / 内存·多schema 段留父包） | 4 | `audit_chain_test.go`、`cleanup_refresh_tokens_test.go`、`register_tenant_guard_test.go`、`refresh_concurrency_test.go` |
| 内核函数测试外迁 | 6 | `storekit/kernel_test.go`（`model.Rand*` 前缀断言留父包） |
| 新文件（父包） | 3 | `sql_shim.go`（SQLStore 别名 + NewSQLStore 薄包装）、`stub_guard_test.go`（StubDomains 守卫自 sql_test 迁出）、`parent_dsn_test.go`（DSN 工具副本） |
| 新文件（子包） | 3 | `sqlstore/aliases.go`、`sqlstore/test_helpers_test.go`（recordingBus/countDevices 副本）、`storefail/recorder_test.go` |

### 9.2 消除的 shim 与跨线同步

- **`failures_shim.go` 删除**：3 个 controlplane 消费方改为直接 import storefail
  （`support_endpoints.go:492/493/501`、`metrics_endpoint.go:90`、`metrics_store_failures_test.go:46/48`）；
  父包 `multi_schema.go`/`redis_session.go` 的 15 处 `recordStoreFailure` 改 `storefail.Record`。
- **`kernel_shim.go` 删除**：父包短名清零（`mustRandHex`→`storekit.MustRandHex` 等）。
- **migrations 路径同步**（搬迁的必然连带，跨线三处已按协调板预告执行）：
  `deploy/docker/scripts/deploy.sh:1507`、`deploy/scripts/verify-runtime.sh:968/1008/1009`、
  `internal/cmdb/{mysql_scan_test.go,search_fulltext_test.go}`；另同步 `.golangci.yml` 路径豁免两条
  （G104→`sqlstore/sql.go`、G201→`sqlstore/sql_slo.go`）、CI 注释路标、DELIVERY/product-design 文档证据路径
  （后者由 `internal/gates` 的证据路径门禁**当场抓出** —— 门禁按设计工作）。
- **`store_extra_test.go` 里 2 处 `errString`**：sqlstore 侧改 `errors.New`（`errString` 是父包私有类型）。

### 9.3 验证口径与结果（全绿）

- `go build ./...` / `go vet ./...` / `go test -count=1 ./internal/... ./cmd/... ./pkg/...` 全绿；
  `golangci-lint`（store+controlplane）0 issues；`gofmt`/`goimports` 干净。
- `-race`：`./internal/store/...`（store 350s / memory 269s / sqlstore 5.3s）+ `./internal/controlplane`（329s）
  全绿，**0 处 DATA RACE**。
- **真库集成实测**（本机 `opsmesh-mysql-evidence-v2`）：`TestRunMigrations*` 8 用例（全新库/幂等/版本门禁/
  checksum 致命/半应用重放/并发构造/CRLF 再基线）+ `TestAuditChainIntegration*` 8 用例 + 跨租户重绑定 +
  刷新令牌清理 + SQL 并发消费 —— 全部 PASS（迁移框架与审计链在搬迁后功能不变）。
- 部署资产门禁 `validate-deploy-assets.sh`：**PASS=76 FAIL=0 SKIP=1**。

### 9.4 本批教训（供末批参考）

1. **折叠在「搬迁脚本」里的三段拼装（头 + 旧头 + 体）必须显式断言**：本轮 3 个拆分文件出现
   「新头 + 原文件旧头/package」重复、1 个文件尾部多出 `}`（删除区间的反向切片把末行复活）——
   都是**拆分脚本自身的 bug**，靠 `goimports`/编译器报错才暴露。教训：拆分产物写完先跑
   `gofmt -e`（语法）再跑 `go build`，两步分开看。
2. **`errString` 这类「父包私有小类型」会被测试跨界引用**——测试搬迁时同款私有符号（`closedDB`、
   `stripDBName`、`recordingBus`、`countDevices`）要么随迁、要么按包各持一份；本轮全部显式处置。
3. **`internal/gates` 的证据路径门禁是搬迁的安全网**：文档里过期的 `内部/store/sql.go` 会被判红，
   这正是「数字对得上但路径是假的」那类漂移的拦截面。
4. **真库验证要主动跑**：CI 的 integration job 有 DSN 才跑，本机有容器时应手动补跑迁移框架 +
   审计链集成（约 2.5 分钟），比只依赖单测的「全绿」可信得多。

### 9.5 剩余（末批：multi_schema 包装层 + 父包收口）

- 8 个 `multi_schema*.go` + 其测试（`multi_schema_test/proxy/smoke/delegation/extra4`）下沉
  `internal/store/multischema/`（或按最终形态命名）；它同时持 `memory`/`sqlstore` 两后端句柄，
  是本链最后一块。
- 之后父包 `internal/store` 只剩：`store.go`（契约别名 + 断言）、`models_shim.go`、`memory_shim.go`、
  `sql_shim.go`、`session.go`、`redis_session.go`、`stub_guard.go` —— 届时评估**删 shim 层**
  还是**保留作稳定门面**（外部 129 个 import 方按 `store.X` 编程，门面本身有产品价值）。


---

## 10. 末批执行记录（2026-10-09，已落地：multi_schema 包装层下沉 + 父包收口）

### 10.1 搬迁面（比 §9.5 预估多两类：混装测试三分 + memory 用例回流）

**prod（9 文件，`git mv` + 包声明改 `package multischema`）** →
`internal/store/multischema/`：`multi_schema.go`（1532 行）与 `_agent_logs/_p03/_p1/_p2/_p3/_p4/_p5/_p6.go`。
配套新增两件：

- `multischema/aliases.go`——同款短名回导层（47 领域结构 + 3 契约类型 + 租户常量 + 36 领域小接口 + `Store`；
  比 sqlstore 版少 `rbacPermSpecs`/`RolePermissions`/`normalizeTenantID`——多 schema 包装层只做路由与委托，实测 0 引用）；
  另补一处 `NewSQLStore` → `sqlstore.NewSQLStore`（子包不能引用父包 shim）。
- 父包 `multi_shim.go`——`SchemaNamer`/`MultiSchemaStore` 别名 + `NewMultiSchemaStore`/`DefaultSchemaNamer` 薄包装。
  外部消费方零改动：`factory/server_factory.go:133` 的构造、`server_netsec.go:402` 的 `case *store.MultiSchemaStore`
  类型分发（**别名与原名同一类型，类型分发逐字不变**）、`config.go`/`tenant_guard.go` 的注释引用。

**测试（14 文件）**：

- 整文件迁移 5：`multi_schema_test / _proxy_test / _smoke_test / _delegation_test` + `store_extra4_test.go`
  （46 个用例全多 schema）→ `multischema_extra4_test.go`；
- **混装件三分**（`store_extra_test.go` 1686 行）：前段 96 个 `TestMemoryStore_*` 边界用例 →
  `internal/store/memory/memory_extra_notfound_test.go`（回流内存子包，与既有 111 个同族用例合流）；
  中段会话组（InProcessSessionStore/RedisSessionStore/err*）留父包，文件改名 `session_extra_test.go`；
  尾段 32 个多 schema 用例 → `multischema/multischema_extra_test.go`；
- 拆分 2：`audit_chain_test.go`（2 个 MS 用例下沉 multischema，内存段留父包）、
  `cleanup_refresh_tokens_test.go`（1 个 MS 用例下沉）；
- **父包专属用例剥回 2**：`TestStubGuard_JoinAndWarnDomains`（被测 `joinStubDomains`/`StubDomains` 属 stub_guard.go）
  回 `stub_guard_test.go`；`TestErrString_Error`（`errString` 属 session.go）回 `session_test.go`；
- 测试 helper：multischema 新增 `test_helpers_test.go`（`recordingBus`/`countDevices`/`stripDBName`/`dropTestDB`
  自父包同名件迁入 + `newMemoryStore()` 测试内别名——`memory.NewMemoryStore` 的子包限定）；
  父包 `parent_test_helpers_test.go`、`parent_dsn_test.go` 因无剩余使用者**删除**（grep 三人成影后删）。

### 10.2 shim 层存废裁定：**保留作稳定门面**（不删）

§9.5 的遗留评估项，裁定与理由：

- 外部 import 方按 `store.X` 编程（129 处）；`models_shim.go` 的 47 类型别名本就是**公共契约面**
  （`store.User`/`store.Ticket`），删掉等于把 129 处改成 `model.X`——纯改名、零功能收益，还破坏
  「契约在 model、门面在 store」的分层叙事；
- 三个后端 shim 各只有 1~2 个别名 + 1~2 个薄包装（合计 ~60 行），维护成本低于一次全仓改名；
- 「同一类型」有编译期保证：`store.go` 的 37 条断言 + 别名让类型切换/方法集缺失当场判红；
- 结论：父包定位为**稳定门面 + 会话层 + 桩守卫**；「删 shim」不再作为债务项，末批以本裁定收口。

### 10.3 父包收口后的形态（实测）

`internal/store/` 顶层 **20** 个 `.go`（prod 8 + test 12）：prod = `store.go` / `models_shim.go` /
`memory_shim.go` / `sql_shim.go` / `multi_shim.go` / `session.go` / `redis_session.go` / `stub_guard.go`；
test = 会话组（session_test / session_extra_test / stub_guard_test）+ 横切语义组
（claim_tenant / schedule / timeout_retry / refresh_concurrency / register_tenant_guard / bench_m4，
均以 memory 后端驱动——**判据：测的是「门面级语义」而非某后端实现**，故按此判据留父包；
`sql_rbac_catalog_test.go` 测 `models_shim` 回导的权限目录，留父包）。
子包：memory 37 / sqlstore 54 / multischema 19（另 +model/storekit/storefail）。
**拆前 `internal/store/` 顶层 113 文件 → 20**。

### 10.4 验证口径与结果（全绿）

- 全仓 `go build ./...` 绿；`go vet ./internal/store/...` 零告警；
- 根模块 `go test -count=1 ./...`：**60 包 ok / 0 fail**；
- `golangci-lint run ./internal/store/... ./internal/controlplane/...`：**0 issues**；`gofmt -l` 净；
- `-race -p 1 ./internal/store/...`：**零 DATA RACE**（日志命中数 0）；
- 部署资产门禁：`PASS=76 FAIL=0 SKIP=1`（第 24 节台账计数复算 PASS）。

### 10.5 三批合计的三条可复用判断（TD-61 全链）

1. **别名回导层是拆包通用解**：三批（memory/sql/multischema）同一配方——`git mv` + 包声明 +
   `aliases.go`（类型别名把中性层名字按原名引入）+ 父包 shim（别名 + 薄包装）。
   搬迁面因此是「换包声明」而不是「给几百标识符加前缀」；SQL 字符串/注释里的同名标识零误伤。
2. **拆分点的判据是「测的是实现还是语义」**：memory 边界用例回流 memory 包（实现），
   跨后端语义用例（审计链、清理、租户闸、调度、超时重试、并发刷新）留父包（门面语义）。
   这条判据让「哪些测试放哪」不再靠感觉。
3. **父包专属符号是拆分边界的硬约束**：`errString`（session.go）、`joinStubDomains`/`StubDomains`
   （stub_guard.go）逼出两个「用例剥回父包」；`//go:embed migrations/` 逼出目录随迁（§9）。
   拆包前先问「这段测试/代码碰了哪些父包专属符号」，比事后编译循环省一轮返工。
