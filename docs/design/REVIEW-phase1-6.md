# OpsMesh Phase 1-6 扩编质量审查报告

> 审查日期：2026-08-26 ｜ 方式：4 名专项审查员并行只读审查（安全/正确性/架构/测试质量）
> 范围：Phase 1-6 全部变更（76 个非测试 Go 文件，+18443 行；7 个前端 JS 文件 8436 行）
> 结论先行：**架构骨架健康，但激进扩编引入了系统性质量问题 —— 55 条原始发现，去重后 31 项，其中 High 11 项**

---

## 一、High 级发现（11 项）

### H1.【安全】系统性跨租户越权（IDOR）— 影响全部 Phase 1-6 数据面
- **位置**：`internal/controlplane/webhook.go` `script.go` `apikey.go` `tenant.go` `billing.go` `slo.go` `ticket.go` `traffic.go` `pipeline.go` `argocd.go` `network.go` `automation.go` `gateway.go` `marketplace.go` `audit_query.go` `compliance.go` `config_hotpush.go` `backup_api.go` `k8s_cluster.go` 等 20 个非测试文件（全部使用 `k8sTenantFromRequest`，0 处 `requireTenantContext`）
- **问题**：租户身份取自客户端可控的 `X-Tenant-ID` 头（`k8sTenantFromRequest` 仅调 `authctx.FromHTTPHeader(r.Header).TenantID`），而 JWT claims 中明明签发了 `tenant_id`（`jwt_sign.go:43`）。`requirePermission` 只验证用户身份+权限，不校验租户归属。
- **关键证据**：同库旧代码路径已经修复过同样的洞（`http_infra.go:38-73` 的 `requireTenantContext` 做 JWT↔头交叉校验，注释标明"修复 1+2"），**Phase 1-6 新代码没有对齐这一标准**。
- **攻击场景**：合法用户 u1（租户 t1，持有任意 :read/:write 权限）登录后发请求带 `X-Tenant-ID: t2` → 可读/写/删 t2 租户的全部 Webhook（含 Headers 中 token）/Script/APIKey 元数据/审计事件。
- **缓解因素**：若严格在可信网关后部署（网关剥离重注入头）则直连伪造不可达；但代码自身无纵深防御。
- **修复建议**：Phase 1-6 handler 统一改用 `requireTenantContext`，或将 `k8sTenantFromRequest` 内部改造为先走交叉校验。补跨租户隔离测试。

### H2.【正确性】SQLStore 桩体系缺陷 — 生产切 mysql 后两条生产路径全部静默失效
- **位置**：`internal/store/sql_*.go` 全系列 15 个文件
- **三类行为并存**：
  1. **P2-P4 桩 Create 返回 nil**（sql_traffic/sql_pipeline/sql_argocd/sql_compliance/sql_backup/sql_network/sql_automation）→ handler nil-checked 返回 500，功能不可用；
  2. **P1/P5/P6 桩 Create 返回填充后但不持久化的对象**（sql_ticket/sql_slo/sql_webhook/sql_script/sql_apikey/sql_tenant/sql_plugin/sql_billing）→ POST 返回 201 假成功，随后 GET 404、List 空，**数据静默丢失且审计已记录"创建成功"**；
  3. **零防护**：全部桩无 log/panic/error，头注释声称"DB 不可用时返回零值"实为误导（桩根本没接 DB，DB 可用也一样）。
- **修复建议**：统一桩策略：接入 `stubNotImplemented()` helper 显式返回错误或至少限频告警；生产配置加 `allowStubStores` 开关，启用时 fatal。

### H3.【正确性】多租户 schema 生产模式空壳链路
- **位置**：`internal/store/multi_schema.go:157-166`（defaultStoreFactory 为每租户创建 `*SQLStore`）+ multi_schema_p1~p6.go
- **问题**：MultiSchemaStore 的 per-schema 路由结构本身正确，但生产工厂委托给的仍是 SQLStore 桩 → `--multi-schema` 生产部署下 Phase 1-6 功能整体空壳。单测注入 mock 工厂全部通过，掩盖了问题。
- **修复建议**：集成冒烟断言 per-tenant store 真实持久化能力；启动日志声明 P1-P6 在 SQL 后端未实现。

### H4.【架构】15 项"有 API 外壳、内核造假"的功能
| # | 功能 | 假在何处 |
|---|---|---|
| 1 | 自动化规则触发/执行 | Execute 恒返回 succeeded；无调度器/事件总线接通 Evaluate |
| 2 | 网络发现 | 返回 2 个硬编码示例设备，不扫描，Scanned 数是公式假数 |
| 3 | 流水线运行 | 只落 pending 记录，全局无执行器推进状态，永不变终态 |
| 4 | ArgoCD 同步 | 仅改状态字段 synced/healthy，不调 API |
| 5 | Webhook 测试投递 | 伪造 StatusCode=200 记录，不发 HTTP |
| 6 | 脚本执行 | stdout="simulated"；SQL 后端甚至不落库 |
| 7 | 计费用量统计 | CalculateUsage 恒返回空结构 |
| 8 | SLO SLI 状态 | 硬编码 99.5/met |
| 9 | 备份/恢复 | 只改记录状态，无文件操作 |
| 10 | 合规扫描编排 | 不执行 CheckScript，占位结果 |
| 11 | 灰度指标对比 | 基准 vs 灰度为模拟数据 |
| 12 | HA 故障切换 | 仅返回当前实例占位 |
| 13 | 插件安装/卸载 | TODO，无下载/清理 |
| 14 | API Key 认证 | 有 CRUD 无认证中间件（见 H5） |
| 15 | SQLStore 十余领域持久化 | 桩返回零值假装成功（见 H2） |

### H5.【安全+架构】API Key 认证未接入 — "有钥匙孔无锁"
- **位置**：`internal/platform/apikey.go` ValidateKey 定义处；`X-API-Key` 头在 internal/ 全局零处理
- **问题**：ValidateKey 仅被测试调用，requirePermission/userFromToken 只支持 JWT。API Key 只能创建不能使用，外部集成方拿到 key 无处可用，还给运营者"已有程序化认证"的错误安全感。
- **修复建议**：auth 中间件增加 `X-API-Key`/Bearer om_ 分支 → ValidateKey → HasScope 映射权限检查。

### H6.【架构】automation 引擎字符串字典序比较 bug（潜伏逻辑炸弹）
- **位置**：`internal/automation/engine.go:164-170`
- **问题**：`return val >= thresh` 中 val/thench 均为 string（ctx 和 Params 都是 map[string]string），字典序比较数值："100" >= "80" 为 false（'1'<'8'）。
- **后果**：未来接入真实指标后 metric_threshold 规则判断随机出错。
- **修复建议**：strconv.ParseFloat 后数值比较，解析失败记日志返回 false。

### H7.【架构】platform 包 3/4 是死代码 — 平台化分层名存实亡
- **位置**：`internal/platform/billing.go` `tenant.go` `marketplace.go`（BillingManager/TenantManager/MarketplaceManager 生产代码零引用）；controlplane/billing.go、tenant.go、marketplace.go 直连 store 绕过分层。唯一被用的是 GenerateAPIKey 一个函数。
- **修复建议**：handler 改走 Manager（顺带救活 GenerateInvoice/CalculateProration），或删除死代码。

### H8.【架构】Phase 0 审查的 4 个 High 在扩编期间全部被遗忘（逐一查证均未修复）
| # | 问题 | 现状证据 |
|---|---|---|
| C-1 | log_collect 截断丢日志（offset 先推进再 break） | log_collect.go:329-332 + 398-400 原样存在 |
| C-2 | memory_discovery/config/secret 读路径返回内部指针（race 风险） | GetConfig 直接 return item，全文无拷贝模式 |
| C-3 | P0 三个地基领域零测试 | 无对应 _test 文件，rg 关键方法零命中 |
| C-4 | logCollectError 无 Is() 方法，errors.Is 恒 false | 仅 Error/Unwrap/wrap |

### H9.【架构】automation/network/gateway 写路径零审计 — 等保三级留痕缺口
- **位置**：三个 handler 文件 s.audit 调用数为 0（对比 ticket/slo/webhook/script 各 3 次）；gateway create/update/enable/disable 全部 `_ = caller` 丢弃调用者。
- **附带**：platform_config.go PUT 假实现（不落库原样回显）却写 Action="platform_config_update" 成功审计 — 审计与事实不符。

### H10.【测试】memory_config.go 带 UTF-8 BOM — store 包覆盖率完全无法测量
- **实证**（go1.26.6）：`go build ./internal/store/` ✅ → `go test ./internal/store/` ✅ → `go test -cover ./internal/store/` ❌ `invalid BOM in the middle of the file`
- **后果**：核心数据层成为覆盖率盲区；CI 若加 -cover 会直接红。
- **实测覆盖率**：controlplane **70.7%** ｜ platform **41.4%** ｜ store **无法测量**

### H11.【测试】SQLStore 桩零测试锁定 + MySQL 集成测试静默全跳过
- **问题**：migration_test/sql_test 需 `OPSMESH_TEST_MYSQL_DSN`，未设置即 t.Skip（实测全部 SKIP）→ CI 绿灯是虚假安全感；没有任何断言锁定桩语义（如 GetAPIKey→(nil,false)），桩改真实现或改坏语义无报警。
- **修复建议**：用 SQLite 内存库/sqlmock 锁定桩行为；CI 对 Skip 计数告警。

---

## 二、Medium 级发现（13 项）

| # | 类别 | 发现 | 位置 |
|---|---|---|---|
| M1 | 安全 | Phase 5 Webhook URL 未复用既有 ValidateWebhookURL SSRF 校验（notify-channels 有防护、webhooks 无 — 同库双标）；恶意 URL/file:// 可入库 | controlplane/webhook.go:78-81 vs server_netsec.go:417-462 |
| M2 | 安全 | API Key PUT 全量替换可清空 Scopes 提权（HasScope 空=全权限）+ 重置 Enabled 绕过 disable 审计意图 | apikey.go:148-173 + memory_apikey.go:81-84 + platform/apikey.go:111-114 |
| M3 | 正确性 | webhook/script 类型断言 `(*store.MemoryStore)` 决定落库 — MultiSchema 生产后端投递/执行记录静默不丢库且无日志；Record 方法不在 Store 接口，编译期无从发现 | webhook.go:224、script.go:242 |
| M4 | 正确性 | CreateTenant 空 ID 归一 default 路由 vs 底层分配随机 ID → 租户数据落错 schema，GetTenant(新ID) 路由到不存在的 schema 并惰性创建垃圾空 schema | multi_schema_p6.go:15-28 |
| M5 | 正确性 | 空租户语义分叉：ListAPIKeys("")=全租户聚合，ListSubscriptions("")/ListInvoices("")=errEmptyTenant 返 nil（MemoryStore 侧两者均为全量语义） | multi_schema_p6.go:111-125 vs 273-335 |
| M6 | 正确性 | store.go 编译期断言块缺 MultiSchemaStore 全部断言（仅 MemoryStore+SQLStore 两组），违背本文件"缺失即编译期暴露"的设计意图 | store.go:769-843 |
| M7 | 正确性 | ensureGateway check-then-act 全程无锁，注释谎称"并发安全：用 mu 保护"；并发触发 data race + 状态覆盖（生产 NewServer 总初始化，仅测试场景触发） | gateway.go:60-70 |
| M8 | 架构 | Store 组合 36 个小接口已过密（注释漂移：称 6 个/12 个实际 36 个）；BillingStore 13 方法横跨 Plan/Subscription/Invoice 三聚合宜拆三 | store.go |
| M9 | 架构 | auth_test.go 三处硬编码 72 权限计数（L102/L125/L568）— 每扩权限必改 3 处，且防不住"删一组恰好凑数" | auth_test.go |
| M10 | 测试 | Phase 6 新模块测试质量明显下滑：marketplace_test 0 个 404/405 用例、弱断言（只看状态码不验 body）；apikey/billing 错误路径不全；跨租户隔离测试全缺（对比 webhook/script 测试 404×5+405+400×3 全覆盖） | marketplace_test.go、apikey_test.go、billing_test.go |
| M11 | 测试 | 边界值缺口：page="0"/pageSize="0" 无锁定用例（走同一分支实现安全但回归无守护）；空 body POST 到 create handler 全库 0 测试 | server_paginate_extra_test.go 及各 *_test.go |
| M12 | 架构 | backup/compliance/canary/HA 四域占位实现无 simulated:true 标记，UI 可显示成功误导运维 | backup_api.go、compliance/engine.go:230、canary_enhance.go:110-136、ha.go |
| M13 | 架构 | 包职责混乱：extension 仅 1 文件不成层；gateway 引擎放 extension、tenant/apikey 放 platform、webhook/script 引擎写在 controlplane — 同期三种归宿；controlplane 130 文件上帝包 | internal/extension、internal/platform、internal/controlplane |

---

## 三、Low 级发现（7 项）

| # | 发现 | 位置 |
|---|---|---|
| L1 | Webhook/Plugin/GatewayRoute 输入校验缺口：Plugin Type 无白名单、DownloadURL 无协议校验；TargetBackend 无校验；Script.TimeoutSec 无上限；禁用脚本仍可 execute | marketplace.go:55-62、gateway.go:137-140、script.go:213-233 |
| L2 | SHA-256 无盐存 API Key hash — 128 位 crypto/rand 熵下业界通行可接受，建议 subtle.ConstantTimeCompare 替换 == 比较 | platform/apikey.go:53,91 |
| L3 | 删除租户无级联清理其 APIKey/Webhook/Script 数据，也无阻止删除自身所属租户保护 | tenant.go:153-166 |
| L4 | MemoryStore Create/Update 存调用方指针入内部 map（含参考标准 ticket 本身），外部事后修改污染内部状态 — 项目既定通病择机治理 | memory_webhook.go:92 等 |
| L5 | 时间基准不一致：MemoryStore 本地 time.Now() vs SQL 桩 UTC — JSON 时区偏移漂移 | memory_ticket.go:73 vs sql_ticket.go:29 |
| L6 | 内嵌前端 assets/*.js 共 8436 行（flow.js 2755+render.js 2829+i18n.js 1496…）零单元测试（注：web/enterprise Vue3 项目另有 25 个 Vitest 测试不属此列）；本机无 gcc 无法跑 -race（需 CI Linux runner 补验） | internal/controlplane/web/assets/ |
| L7 | network engine 魔法数 254 未提取常量 | network/engine.go:190 |

---

## 四、正面确认（不是所有都是坏的）

- ✅ 抽查的 13 个 handler **全部有 requirePermission 且权限点在 rbacPermSpecs 中定义**
- ✅ operator/viewer 权限收敛合理（P5/P6 各领域 write 仅 admin）
- ✅ Memory 层新增 store **锁使用全部正确、深拷贝完整**（cloneAPIKey 拷 Scopes、cloneInvoice 拷 Items 等）
- ✅ 路由子路径解析健壮（尾斜杠/双斜杠/多余段边界全部正确处理）
- ✅ handler 对 Create 返回 nil 全部有兜底（500 而非 panic）
- ✅ 错误响应格式统一 {"error": "..."} 小写风格，未见漂移
- ✅ 通知渠道已有完善 SSRF 防护（协议白名单+私网黑名单+DNS rebinding 防护）— 只是没复用到 webhooks

## 五、统计与修复优先级建议

**总计 31 项：High 11 ｜ Medium 13 ｜ Low 7**

修复优先级（按风险×成本排序）：
1. **P0（立即）**：H1 跨租户越权（安全红线）→ H10 去 BOM（一行修复解锁覆盖率）→ H6 字符串比较 bug（一处修复）
2. **P1（本迭代）**：H2/H3 SQL 桩显式失败机制 → H5 API Key 认证接入 → H9 审计补齐 → M1 Webhook SSRF 校验复用
3. **P2（排期）**：H4 假实现逐个真实现（automation/pipeline 优先）→ H7 platform 死代码处置 → H8 Phase 0 四项偿还 → M10/M11 测试补强
4. **P3（技术债）**：M8 接口重组 → M13 包职责调整 → 其余 Low