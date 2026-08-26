# OpsMesh Phase 1-6 审查问题修复方案

> 依据：《docs/design/REVIEW-phase1-6.md》（31 项发现：High 11 / Medium 13 / Low 7）。
> 性质：设计方案文档，伪代码级描述，不含完整实现；粒度以可直接派发子代理执行为准。
> 策略基调：当前阶段以止血为主——安全红线与静默失效优先根治；架构级重组（M8/M13/H4 全量真实现）记录排期不动刀。

---

## 第1章 处置策略总览

表：31 项发现处置策略汇总表

| 编号 | 一句话问题 | 策略 | 批次 |
|---|---|---|---|
| H1 | 20 个 handler 经 X-Tenant-ID 头取租户可伪造越权 | 【代码修复】 | B1 |
| H2 | SQLStore 15 桩三类行为并存，切 mysql 后静默失效/假成功 | 【代码修复】 | B2 |
| H3 | multi-schema 生产工厂委托 SQL 桩，P1-P6 整体空壳 | 【缓解措施】 | B2 |
| H4 | 15 项“API 外壳、内核造假”功能 | 【记录接受】 | — |
| H5 | API Key 有 CRUD 无认证中间件，“有钥匙孔无锁” | 【代码修复】 | B2 |
| H6 | automation 引擎字符串字典序比较数值 | 【代码修复】 | B1 |
| H7 | platform 包 3/4 死代码，分层名存实亡 | 【代码修复】 | B3 |
| H8 | Phase 0 四个 High 未修复（截断丢日志/指针泄漏/零测试/缺 Is） | 【代码修复】 | B3 |
| H9 | automation/network/gateway 写路径零审计 | 【代码修复】 | B2 |
| H10 | memory_config.go 带 UTF-8 BOM，store 覆盖率无法测量 | 【代码修复】 | B1 |
| H11 | SQLStore 桩零测试锁定，MySQL 集成测试静默全跳过 | 【代码修复】 | B3 |
| M1 | Webhook URL 未复用 ValidateWebhookURL SSRF 校验 | 【代码修复】 | B2 |
| M2 | API Key PUT 全量替换可清空 Scopes 提权 | 【代码修复】 | B2 |
| M3 | 类型断言决定落库，Record 方法不在 Store 接口 | 【代码修复】 | B2 |
| M4 | CreateTenant 空 ID 路由错位产生垃圾 schema | 【代码修复】 | B2 |
| M5 | 空租户语义分叉（聚合 vs errEmptyTenant） | 【代码修复】 | B2 |
| M6 | store.go 编译期断言缺 MultiSchemaStore 全组 | 【代码修复】 | B2 |
| M7 | ensureGateway check-then-act 无锁 | 【代码修复】 | B3 |
| M8 | 接口过密＋注释漂移（称 6 个实际 36 个） | 注释【代码修复】/拆分【记录接受】 | B3 |
| M9 | auth_test 三处硬编码 72 权限计数 | 【代码修复】 | B3 |
| M10 | Phase 6 新模块测试质量下滑 | 【代码修复】 | B3 |
| M11 | 分页边界值与空 body POST 零守护用例 | 【代码修复】 | B3 |
| M12 | 四域占位实现无 simulated:true 标记 | 【代码修复】 | B3 |
| M13 | 包职责混乱、controlplane 上帝包 | 【记录接受】 | — |
| L1 | Plugin/GatewayRoute/Script 输入校验缺口 | 【代码修复】 | B3 |
| L2 | Key hash 比较未用 constant-time（无盐本身业界通行） | 【缓解措施】 | B2 |
| L3 | 删租户无级联清理、可删 default 平台租户 | 【缓解措施】 | B3 |
| L4 | MemoryStore 存调用方指针入内部 map | 【记录接受】 | — |
| L5 | 时间基准不一致（本地 vs UTC） | 【缓解措施】 | B2 |
| L6 | 前端 assets JS 零测试＋本机无 gcc 跑不了 -race | 【缓解措施】 | B4 |
| L7 | network engine 魔法数 254 未提取常量 | 【代码修复】 | B1 |

统计：【代码修复】22 项、【缓解措施】5 项、【记录接受】4 项（H4、M13、L4 及 M8 拆分半项）。“记录接受”项均在第 3~5 章给出排期建议与触发条件，不是无声搁置。

---

## 第2章 关键技术决策论证

### 2.1 决策一：H1 跨租户越权——选方案 A（函数本体收口）

#### 2.1.1 现状核实

- `k8sTenantFromRequest` 定义于 internal/controlplane/k8s_cluster.go:41-49，仅做 `authctx.FromHTTPHeader(r.Header).TenantID` 直取，缺头时 requireAuth=true 返回空串、否则归一 default。全库共 **111 处调用**、分布在约 20 个 handler 文件；gateway.go/ticket.go/slo.go 还有 3 个包装函数转发到它。
- 同库旧标准 `requireTenantContext`（http_infra.go:38-73）已实现完整行为矩阵：头↔JWT tenant_id 交叉校验（不一致 403）、头空回退 token、requireAuth 缺身份 401、demo 自动填 default、非 demo 非 auth 缺头 400。
- 关键事实：issueUserToken（auth.go:587）签发 JWT 时 `TenantID` **恒为硬编码 "default"**（用户中心为平台级）；store.User 无租户归属字段。

#### 2.1.2 方案对比

表：H1 方案 A 与方案 B 对照表

| 维度 | 方案 A：改造 k8sTenantFromRequest 本体 | 方案 B：逐 handler 替换 requireTenantContext |
|---|---|---|
| 语义收敛 | 单点收口，未来新增 handler 自动获得正确行为 | 20 文件逐点改，遗漏一处即留洞 |
| 改动方式 | 函数签名加 w 并返回 ok，111 处调用点机械替换 | 每 handler 手工调整鉴权顺序与错误响应 |
| 编译器兜底 | 签名变化 → 未改的调用点编译报错，**无遗漏可能** | 无兜底，靠 review 把关 |
| 双标消除 | 与 requireTenantContext 共享同一实现 | 两套语义并存继续漂移 |
| 测试影响 | 相同（见 2.1.4） | 相同 |

结论：**选方案 A**。理由：编译器驱动的机械替换优于人工逐点改造；“一处修改覆盖 20 个调用方”的安全收益远大于一次性替换成本；包装函数（gatewayTenantFromRequest/ticketTenantFromRequest/sloTenantFromRequest）自动继承正确行为。

#### 2.1.3 具体设计

- 新签名（伪代码）：`func (s *Server) k8sTenantFromRequest(w http.ResponseWriter, r *http.Request) (string, bool)`，函数体一行委托 `actx, ok := s.requireTenantContext(w, r); return actx.TenantID, ok`；原函数删除。
- 111 处调用点统一替换为 `tenant, ok := s.k8sTenantFromRequest(w, r); if !ok { return }`，并删除各 handler 原有的 `if tenant == "" { 401 }` 分支（401 已由 requireTenantContext 写入，保留会造成重复响应写）。
- 行为矩阵（决策依据）：

表：H1 改造后行为矩阵表

| 场景 | 头 X-Tenant-ID | JWT tenant_id | 结果 |
|---|---|---|---|
| 登录用户正常访问 default | default | default | 放行 |
| 登录用户伪造他租户 | t2 | default | **403（堵死本次漏洞）** |
| 登录用户带头、token 无 claim（理论不存在，恒签 default） | t1 | 空 | 放行（仅头注入，向后兼容） |
| 网关注入（TrustGatewayHeaders，无 JWT） | t1 | 空 | 放行 |
| 头空、登录用户 | 空 | default | 回退 token，返回 default |
| demo 模式无任何身份 | 空 | 无 | 自动填 default/demo（体验不变） |
| requireAuth=true 缺身份 | 空 | 无 | 401（原行为不变） |
| 非 demo 非 auth 缺头 | 空 | 无 | 400（原归一 default 收紧，属修复本体） |

边界说明：
1. “登录用户访问非 default 租户”在当前系统属非法场景——User 模型无租户归属、所有合法用户均属平台 default 租户，故交叉校验不会误伤现有合法流量。未来若引入“用户归属租户”模型，需扩展 issueUserToken 按 User.TenantID 签发（记入 TODO，不在本次范围）。
2. API Key 调用者（H5 引入后）：无 JWT，走“头非空＋token 空→放行”分支，但必须在 H5 认证分支内补一道 `key.TenantID == header 租户` 校验（见 2.3），否则 API Key 将成为新的越权面。
3. demo 模式行为不变：newTestServer 构造即 `Demo:true + requireAuth:false`，现有 handler 测试带 X-Tenant-ID 头或裸请求均可放行，影响面极小（已实证 endpoint_test.go newTestServer 实现）。

#### 2.1.4 对测试的影响面

- 现有 handler 测试绝大多数仅带头或裸请求（demo 放行）→ 不受影响。
- 需排查两类：a) 同时携带有效 JWT 且 X-Tenant-ID≠"default" 的断言用例（将变 403，需改为一致值）；b) 显式构造 `requireAuth:true 且无头` 的负向用例期望 401（不变，仍 401）。
- 新增正向守护测试：u1(default) 带 `X-Tenant-ID: t2` 的 JWT 访问 webhook/script/apikey 列表 → 断言 403；跨租户隔离测试在 M10 一并补齐。

### 2.2 决策二：H2/H3 SQLStore 桩统一 stub_guard 机制

#### 2.2.1 接口签名约束分析（为什么不能改签名）

- 实测 store.go：33 个领域接口的方法绝大多数**无 error 返回值**（如 CreateTicket 返回 *Ticket、CreatePolicy 返回 *TrafficPolicy、DeletePolicy 返回 bool）。
- 若统一改为带 error 签名，爆炸半径 = 33 接口 × 3 实现（MemoryStore / SQLStore / MultiSchemaStore）≈ 数百方法重写 ＋ controlplane 全部 handler 调用方增加错误分支 ＋ multi_schema_p1~p6 全部委托层同步修改。估算 >2000 行且横跨全部包，与本阶段“止血”基调冲突，回归面不可控。
- 结论：**不改接口签名**。桩策略收敛为“返回约定零值 + 统一限频告警日志”，让失败可见而非假装成功。

#### 2.2.2 stub_guard.go 设计

新增 internal/store/stub_guard.go：

```
// StubNotImplemented 统一桩入口：限频告警 + 返回约定零值。
// domain 如 "ticket"，method 如 "CreateTicket"；每 (domain,method) 首次必打，
// 之后同 key 60s 限频一次（sync.Map 记录 lastLog 时间戳，避免刷屏）。
func StubNotImplemented(domain, method string)
```

15 个 sql_*.go 桩（sql_ticket/slo/traffic/pipeline/argocd/compliance/backup/network/automation/webhook/script/tenant/apikey/plugin/billing）全部接入，桩行为统一为：

表：SQLStore 桩统一后行为对照表

| 方法类别 | 统一后返回 | 说明 |
|---|---|---|
| Create 类 | **nil**（不返回填充后的假对象） | 杜绝“201 假成功→GET 404→审计已记成功”链路；handler nil-checked 已有 500 兜底（审查正面确认） |
| Get/Update/Delete/Enable/Disable 类 | nil,false / false | 与现状一致，但补 StubNotImplemented 日志 |
| List 类 | 空切片 | 保持非 nil，防上层 range panic |
| 特例：sql_ticket.CreateTicket 现状返回填充对象 | 改为 return nil | 该文件是“假成功”类代表，必须翻转 |

配套约束：
- 生产配置加 `allowStubStores` 开关（config.go 增加 bool 字段）：false（默认）时 NewServer 若检测到 SQL 后端则启动日志 WARN 声明“以下领域 P1-P6 在 SQL 后端未持久化：…”；true 时 fatal 拒绝启动。demo/内存后端不受影响。
- 每个桩文件的误导性头注释（“DB 不可用时返回零值”）改为如实声明“未实现的桩，经 stub_guard 显式告警”。

#### 2.2.3 可行性与兼容性

- 改动量：中（stub_guard.go 约 60 行 + 15 文件每文件 5~15 行接入 + config 字段与开关逻辑约 30 行 ≈ 250 行，其中机械改动为主）。
- 兼容性：接口签名零变化 → MemoryStore/MultiSchemaStore/全部 handler 不受影响；唯一行为变化是 SQL 后端下 Create 从“假成功 201”变“500”——这是修复目标本身，需在 CHANGELOG 声明。
- 风险：sql_ticket 等 P1/P5/P6 桩翻转为 nil 后，若有单测断言“Create 返回填充对象”将红 → 第 7 章风险清单跟踪；防回归手段为 H11 的桩语义锁定测试（GetAPIKey→(nil,false)、CreateTicket→nil 等断言入库）。

### 2.3 决策三：H5 API Key 认证接入设计

#### 2.3.1 挂接点选择

对比两候选：
- userFromToken（auth.go:648）：返回 *store.User，语义是“人”。API Key 调用者非用户，强行构造假 User 会污染 caller.ID 审计与 MustChangePassword 等用户专属逻辑。❌
- **requireProd（auth.go:751）**：已是统一 RBAC 闸（联邦/Bearer/网关注入/demo 五路分发），在此加第 2.5 路“API Key”最小侵入，且天然获得权限检查收口。✅

补充在 extractBearer 层不做识别的原因：extractBearer 只取串不鉴权，前缀识别放这里会让所有调用方都要处理两种身份类型。

#### 2.3.2 认证流程设计

```
requireProd 步骤 2 改造：
  auth 头以 "Bearer om_" 开头 或 X-API-Key 头非空 → s.authorizeByAPIKey(w, r, required)

authorizeByAPIKey:
  key := 取 om_ 明文（Bearer 前缀剥掉或读 X-API-Key）
  ak, err := apiKeyMgr.ValidateKey(key); err != nil → 401（复用 platform.APIKeyManager）
  if !ak.Enabled || 过期 → 401（ValidateKey 已含，双保险）
  reqTenant := r.Header.Get("X-Tenant-ID")（空时取 ak.TenantID）
  if reqTenant != "" && reqTenant != ak.TenantID → 403 "tenant mismatch"   ← 关键：堵住 API Key 越权面
  if !apiKeyMgr.HasScope(ak, required) → 403
  返回 (nil, true)（caller 为 nil，与联邦路径一致；audit 的 UserID 用 "apikey:"+ak.Name）
```

- 权限映射：rbacPermSpecs 权限点本就是 `resource:action` 格式，与 Scopes 同构，HasScope 直接比对即可，无需翻译层。
- Server 需新增字段 `apiKeyMgr *platform.APIKeyManager`（NewServer 用 s.store 构造；platform 包已被 controlplane 依赖，无环）。

#### 2.3.3 LastUsedAt 更新的性能考量

- 禁止每请求同步写 store：Memory 下是锁竞争，MultiSchema 下跨 schema 路由，SQL 真实现后是写放大。
- 设计内存聚合批量刷写：Server 持 `apiKeyUsage map[keyID]int64 + sync.Mutex`（纯计数），后台 goroutine 每 30s 批量 UpdateAPIKey 回写 LastUsedAt（仅变更项）；进程退出丢失 ≤30s 精度可接受。MVP 可先只累计内存计数并在 GET /api/v1/apikeys 响应中附带，落库延后到 SQL 真实现。

#### 2.3.4 可行性、兼容性与风险

- 改动量：中（auth.go 分支+authorizeByAPIKey 约 80 行，server.go 字段与构造 10 行，LastUsedAt 聚合器 50 行）。
- 兼容性：JWT 用户零感知；X-API-Key 头从“无人处理”变“生效”，无存量依赖方；ValidateKey 的全租户线性扫描（ListAPIKeys("")）在 MultiSchema 下遍历全部 schema，性能可接受（Key 量小），但注意 SQL 桩下 ListAPIKeys 返回空 → 认证恒 401，符合“桩显式失效”总原则。
- 风险：a) HasScope 空 Scopes=全权限（platform/apikey.go:111 向后兼容语义）叠加 M2 PUT 清空漏洞=提权通道 → M2 必须与 H5 同批完成（已排 Batch2）；b) 防回归：新增 apikey_auth_test 覆盖 有效key/禁用/过期/租户不匹配/scope 不足/无 scopes 全通 六类用例。

### 2.4 决策四：H7 platform 死代码处置——推荐删除死代码

#### 2.4.1 二选一对比

表：H7 处置方案甲乙对照表

| 维度 | 方案甲：handler 改走 Manager（救活分层） | 方案乙：删除死代码保留 GenerateAPIKey |
|---|---|---|
| 改动量 | 大（>300 行）：billing 三组端点十余处直连 store 重排鉴权/审计，还需回答“账单何时生成”（无周期任务框架） | 小（净减约 250 行）：删 3 个 Manager 类型与方法 |
| 行为风险 | 引入新的行为面（GenerateInvoice 触发时机、CalculateUsage 数据来源），违背止血基调 | 生产零引用（审查已实证），编译期兜底误删 |
| 分层收益 | 名义上救活分层，但 webhook/script 等其他域仍绕过 platform，分层依旧残缺 | 承认现状，待 H5 让 APIKeyManager 复活、billing 真实现时按需重建 |
| 逻辑丢失 | 无 | GenerateInvoice/CalculateProration 约 80 行进 git 历史＋CHANGELOG 指引找回 |

#### 2.4.2 结论

**推荐方案乙（删除死代码）**，依据“当前阶段以止血为主”：a) 死代码生产零引用，删除无行为变化、零回归面；b) 方案甲的大改动不消灭任何缺陷，只是搬动调用位置；c) APIKeyManager 必须保留——H5 落地后 ValidateKey/HasScope 即成为 platform 包唯一在产路径，分层价值由它延续；d) GenerateInvoice 逻辑以 git 历史＋CHANGELOG 记录找回路径（见 3.7 执行细节）。执行细节见 3.7。

---

## 第3章 High 级发现逐项修复方案

> H1/H2/H5/H7 的详细论证见第 2 章，此处仅列执行要点；其余各项给全五要素。

### 3.1 H1 跨租户越权（IDOR）——见 2.1

- 策略：【代码修复】方案 A。改动点：k8s_cluster.go 函数本体收口 + 20 个 handler 文件 111 处调用点机械替换。
- 可行性：大（>200 行，但为编译器驱动的机械替换，单点语义改动仅 ~10 行）。
- 兼容性：API 契约不变；错误码从“空租户 401”细化为 401/403/400 三态（与旧代码路径对齐）；前端无需变更。
- 风险：携带不一致双身份的存量调用被 403（即漏洞本身）；防回归＝新增跨租户 403 守护测试＋M10 补隔离用例矩阵。

### 3.2 H2 SQLStore 桩体系缺陷——见 2.2

- 策略：【代码修复】stub_guard.go 统一入口，15 文件接入，Create 类一律返回 nil。
- 可行性：中（~250 行机械改动）。兼容性：签名零变化；SQL 后端 Create 由假成功变 500（修复本体）。
- 风险：断言“桩返回填充对象”的既有测试翻红；防回归＝H11 桩语义锁定测试同批落地。

### 3.3 H3 multi-schema 生产链路空壳——缓解措施

- 一句话问题：per-schema 路由结构正确但委托的是 SQLStore 桩，--multi-schema 生产部署下 Phase 1-6 整体不可持久化且无任何提示。
- 策略理由：真实现属 H4 大工程；本阶段做“让空壳可见、可测”，随 H2 的 stub_guard 与开关自然获得告警能力。
- 改动点：
  - multi_schema.go defaultStoreFactory 构造后遍历探测：调一次 CreateTicket+GetTicket 探针（内存工厂跳过），失败打 ERROR 日志声明 P1-P6 未实现；
  - 新增集成冒烟 store/multi_schema_smoke_test.go：Memory 工厂注入下断言 per-tenant 写读一致（锁住路由正确性，防 H3 回归）；SQL 工厂分支显式 Skip 并计数。
- 可行性：小（<80 行）。兼容性：纯增量，零契约变化。
- 风险：探针写脏数据 → 探针用固定 ID 创建后立即删除。

### 3.4 H4 十五项“外壳真、内核假”——记录接受

- 策略：本迭代不做全量真实现（估算 >3000 行，涉及调度器/事件总线/外部系统对接，超出止血范畴）。落地三件低成本缓解替代：
  1. M12 的 simulated:true 标记（Batch3，让 UI 不再误导运维）；
  2. 启动日志声明未真实现领域清单（随 H2 开关一并输出）；
  3. REVIEW 报告 H4 表格原样转入 CHANGELOG “已知限制”节。
- 排期建议：automation Execute→事件总线接通 Evaluate 与 pipeline executor 为 P2 首批；ArgoCD/Webhook 测试投递等需外部依赖的排 P3。触发条件：任一领域进入付费客户承诺清单前必须真实现。

### 3.5 H5 API Key 认证接入——见 2.3

- 策略：【代码修复】requireProd 加 om_ 分支 + authorizeByAPIKey + 租户归属校验 + LastUsedAt 批量聚合。
- 可行性：中。兼容性：JWT 用户零感知。风险：空 Scopes=全权限叠加 M2 清空漏洞 → M2 同批强制完成；六类认证用例防回归。

### 3.6 H6 automation 字符串比较逻辑炸弹

- 一句话问题：engine.go:164-170 `val >= thresh` 对 string 做字典序比较，"100">="80" 为 false。
- 策略：【代码修复】一处定点修复，成本极低收益明确。
- 改动点：internal/automation/engine.go Evaluate 的 TriggerTypeMetricThreshold 分支：

```
fv, err1 := strconv.ParseFloat(val, 64)
ft, err2 := strconv.ParseFloat(thresh, 64)
if err1 != nil || err2 != nil { log 告警; return false }   // 解析失败不触发并留痕
return fv >= ft
```

- 可行性：小（<15 行含 import）。兼容性：Evaluate 无外部契约；现有测试若断言字典序行为将红（预期内）。风险：阈值配置非数值时行为从“随机成立”变“恒 false+日志”，属修复目标；防回归＝补表驱动测试（"100"/"80"→true、"9"/"10"→false、非数值→false）。

### 3.7 H7 platform 死代码处置——推荐删除死代码

- 论证（结合“止血为主”）：
  - 方案甲“handler 改走 Manager”：billing.go handler 有 plans/subscriptions/invoices 三组端点共十余处直连 store，改造要重排鉴权/审计顺序并救活 GenerateInvoice 的调用时机（何时生成账单？无周期任务框架），估算 >300 行且引入新的行为面——违背止血基调。❌
  - 方案乙“删除死代码保留 GenerateAPIKey”：BillingManager/TenantManager/MarketplaceManager 三个类型及方法整体删除（GenerateInvoice/CalculateUsage/CalculateProration 约 80 行逻辑以 git 历史+CHANGELOG 记录找回路径）；**APIKeyManager 全保留**——H5 即将启用 ValidateKey/HasScope，它将从“死代码”变“唯一活口”。✅
- 改动点：删 internal/platform/billing.go、tenant.go、marketplace.go 中未被引用的 Manager 类型与方法（保留类型别名 type X = store.Y 行，handler 可能引用）；跑 go build ./... 验证 controlplane 引用不破。
- 可行性：小（净减约 250 行）。兼容性：死代码生产零引用（审查已实证），无 API 变化。风险：误删仍被引用符号 → 编译期立即暴露；防回归＝build+vet 双过。

### 3.8 H8 Phase 0 四个被遗忘的 High

- 策略：【代码修复】四子项一次清偿。
- C-1 截断丢日志：agent/log_collect.go:329-332 与 398-400，把“offset 先推进再 break”改为先按剩余缓冲截取内容、offset 只推进实际读取量（伪代码：`n := min(len(payload)-offset, bufRoom); append(buf, payload[offset:offset+n]); offset += n`），break 条件改在 n==0 时。
- C-2 读路径返回内部指针：store/memory_discovery.go / memory_config.go / memory_secret.go 的 GetConfig/GetSecret/发现结果读方法统一改为“锁内 clone 后返回”（参照 cloneAPIKey 既有模式）；写侧入 map 前同样拷贝。
- C-3 地基领域零测试：新增 agent/log_collect_test.go（覆盖截断边界：payload 恰好等于/大于缓冲两轮读取拼接还原）与 store/memory_config_secret_test.go（并发读写跑 goroutine 循环 + go test -race 断言无 race、返回副本修改不影响内部态）。
- C-4 缺 Is() 方法：logCollectError 增加 `func (e *logCollectError) Is(target error) bool { _, ok := target.(*logCollectError); return ok }`（或按错误类别字段比对），使 errors.Is(err, ErrLogCollect) 生效；补一个 errors.Is 正反用例。
- 可行性：中（合计 ~150 行含测试）。兼容性：C-2 返回副本可能改变“外部改返回值影响内部”的隐式依赖——审查确认 Memory 层深拷贝是项目标准模式，风险低；防回归＝C-3 新增测试。
- 风险：log_collect 截断算法改动影响既有日志采集回归 → 用例覆盖“多轮分片重组”场景兜底。

### 3.9 H9 写路径零审计（automation/network/gateway）

- 策略：【代码修复】对照 ticket/slo/webhook 的三次审计模式补齐。
- 改动点：
  - automation.go：create/update/delete/enable/disable 五处成功分支补 `s.audit(r.Context(), &proto.AuditEvent{TenantID: tenant, UserID: callerID(r), Action: "automation_xxx", Target: id})`；
  - network.go：create/update/delete 三处同上；
  - gateway.go：create/update/enable/disable 四处同上，并把 `_ = caller` 改为使用 caller.ID；
  - caller 为 nil（demo/联邦路径）时 UserID 落 "demo" 或 "federation"，与既有审计风格一致。
- platform_config.go PUT 假审计不在本项处理（归 Batch3 T3.2，避免同文件跨批冲突）。
- 可行性：小（~60 行机械插入）。兼容性：审计事件为纯增量，AuditStore 已有通道。风险：审计 Detail 泄漏敏感头 → 复用 sanitizeAuditDetail；防回归＝各补一条“POST 后 audit 列表含对应 Action”断言。

### 3.10 H10 memory_config.go UTF-8 BOM

- 策略：【代码修复】一行级修复解锁覆盖率。
- 改动点：去除 internal/store/memory_config.go 文件头 EF BB BF 三字节（已实测确认存在）；顺手 `gofmt -l ./internal/store` 校验全包格式。
- 验收命令：`go build ./internal/store/ && go test -cover ./internal/store/` 双绿。
- 可行性：极小。兼容性：无。风险：编辑器再次引入 BOM → 在 .golangci.yml 或 CI 加 gofmt 检查兜底（Batch4 验证步骤覆盖）。

### 3.11 H11 桩零测试锁定 + MySQL 集成测试静默跳过

- 策略：【代码修复】两层守护。
- 改动点：
  1. 新增 store/stub_semantics_test.go：不经 DSN 直接构造 SQLStore{} 零值，表驱动断言 15 领域桩语义（Create→nil、Get→(nil,false)、List→空切片、Delete→false），锁定 2.2.2 统一策略；
  2. sql_test.go / migration_test.go 的 t.Skip 前打 `t.Logf("SKIP reason=missing OPSMESH_TEST_MYSQL_DSN")` 并累加计数；CI（ci.yml）加一步：解析 go test 输出统计 skip 数，超过基线（当前 MySQL 相关 skip 数）即 fail 并提示“集成测试静默跳过”；
  3. 可选增强：引入纯 Go 的 sqlite 驱动跑 migration 冒烟（评估 modernc.org/sqlite 依赖体积后再定，若依赖过重则降级为仅做第 1、2 步）。
- 可行性：中（~120 行）。兼容性：纯测试与 CI 增量。风险：skip 基线设太紧导致 CI 常 red → 基线取实测当前值并在注释注明调整方式。
---

## 第4章 Medium 级发现逐项修复方案

### 4.1 M1 Webhook SSRF 校验双标

- 一句话问题：webhook.go:78-81 创建时未调 ValidateWebhookURL（notify-channels 有防护），file:// 与私网 URL 可入库。
- 策略：【代码修复】复用同库既有校验，不新造轮子。
- 改动点：controlplane/webhook.go handleCreateWebhook 在 decodeJSONBody 后加 `if err := ValidateWebhookURL(body.URL, s.allowPrivateWebhook()); err != nil { 400 }`；Update 同理；allowPrivate 取值与 notify-channels 保持同一配置来源（server_netsec.go:417 已有实现与私网黑名单/DNS rebinding 防护）。
- 可行性：小（<20 行）。兼容性：存量已入库的恶意 URL 不回扫（记录在 CHANGELOG；List/投递路径可后续加二次校验）；新建非法 URL 从“入库成功”变 400——修复本体。风险：合法内网回调地址被拒 → allowPrivate 开关与 notify-channels 行为对齐即可；防回归＝补 file://、http://127.0.0.1、http://169.254.169.254 三条 400 用例。

### 4.2 M2 API Key PUT 全量替换提权

- 一句话问题：handleUpdateAPIKey 把 body 直接覆盖存储（memory_apikey.go:81-84 仅保护 ID/TenantID/CreatedAt），清空 Scopes 即得全权限，还能翻转 Enabled 绕过禁用审计意图。
- 策略：【代码修复】PUT 改白名单字段更新，服务端合并而非客户端全量。
- 改动点：
  - controlplane/apikey.go handleUpdateAPIKey：读出 existing 后仅接受 name/scopes/rateLimitPerSec/expiresAt/description 字段；`if body.Scopes 存在且 len==0 → 400 "scopes cannot be emptied"`（配合 H5 空 Scopes=全权限语义收紧）；Enabled 不接受 PUT 修改；
  - 新增 POST /api/v1/apikeys/{id}/enable|disable 子端点承载 Enabled 变更并各写一条审计（对齐 gateway enable/disable 风格）；
  - Key hash 字段强制保留 existing.Key（body 传入任何 hash 值都忽略）——防替换 hash 接管他人 key。
- 可行性：小-中（~80 行含新端点）。兼容性：前端若用 PUT 传全量对象，多传字段被忽略不影响成功响应；Enabled 变更走新端点属新增能力。风险：前端启用/禁用按钮失效 → 需同步 web 前端调用点（排查 web/ 中 apikey 相关请求）；防回归＝apikey_test 补“PUT 清 scopes→400”“PUT 改 hash 无效”“disable 后认证 401”三用例。

### 4.3 M3 Record 落库依赖类型断言

- 一句话问题：webhook.go:224/script.go:242 用 `.(*store.MemoryStore)` 决定是否落投递/执行记录，MultiSchema 下静默丢弃且编译期无从发现。
- 策略：【代码修复】把 Record 方法提升进 Store 组合接口，编译期暴露。
- 改动点：
  - store.go：RecordDelivery(tenantID, webhookID string, rec *DeliveryRecord) bool 与 RecordScriptExecution(...) bool 两方法加入对应子接口（WebhookStore/ScriptStore）并随之进入 Store 组合接口与 M6 断言块；
  - MemoryStore 已有实现签名对齐；SQLStore 桩接入 stub_guard 返回 false；MultiSchemaStore 加委托路由（照抄同文件既有模式 ~10 行）；
  - webhook.go/script.go 删除类型断言分支，直接 `s.store.RecordXxx(...)`（返回 false 时 logx WARN 一条，不再静默）。
- 可行性：中（~120 行跨 store+handler）。兼容性：MemoryStore 单测行为不变；MultiSchema 下从“丢库”变“落库”——修复本体。风险：接口扩方法导致其他 Store 实现者（若有 mock）编译红 → 全库 rg "Store interface" 实现面确认只有 3 个真实现；防回归＝multi_schema 冒烟测试断言 Record 后 List 可见。

### 4.4 M4 CreateTenant 空 ID 路由错位

- 策略：【代码修复】MultiSchema 层预生成租户 ID 再路由。
- 改动点：store/multi_schema_p6.go CreateTenant：tenant.ID=="" 时先 `tenant.ID = randTenantID()`（复用包内既有 randID 族函数）再 storeFor(tenant.ID) 创建；删除空串归一 default 的分支。MemoryStore 底层分配逻辑保持不动（单后端无路由问题）。
- 可行性：小（<15 行）。兼容性：CreateTenant 响应体本就回传分配后 ID，调用方无感知；唯一变化是“新租户数据落在自己的 schema 而非 default”。风险：ID 生成器与底层实现重复 → 注释声明“路由层负责预生成，底层对非空 ID 不再改写”；防回归＝multi_schema_smoke_test 补“空 ID 创建→GetTenant(返回 ID) 必中”用例。

### 4.5 M5 空租户语义分叉

- 策略：【代码修复】统一为“空串=跨租户聚合”。
- 改动点：store/multi_schema_p6.go 的 ListSubscriptions("") / ListInvoices("") / ListBillingPlans 相关分支去掉 errEmptyTenant 提前返回，改为 allStores() 遍历聚合（照抄同文件 ListAPIKeys("") 的既有模式）。MemoryStore 侧已是聚合语义，无需动。
- 可行性：小（<30 行）。兼容性：SQL/MultiSchema 下空租户查询从 (nil,false) 变全量列表——与 Memory 对齐即修复目标；无生产调用方传空串（rg 验证后再动手，若 handler 有传空场景需评估可见性扩大）。风险：越权聚合暴露他租户账单 → 该方法仅在 admin 权限路径调用（billing:read 为 admin-only，审查正面确认 P6 write 仅 admin），并在注释标注“仅 admin 聚合视图使用”。

### 4.6 M6 编译期断言缺口

- 策略：【代码修复】补齐第三组断言。
- 改动点：store.go:769-843 断言块追加 `_ TenantStore = (*MultiSchemaStore)(nil)` 等 MultiSchemaStore 全量 33 条（照抄 MemoryStore 组逐行替换类型）；同时修正文件头与注释漂移（“拆为 6 个小接口”改为如实计数）。
- 可行性：小（纯声明行）。兼容性：若 MultiSchemaStore 缺方法将立即编译红 → 这正是设计意图，M3/M5 先行的原因。风险：无。

### 4.7 M7 ensureGateway 并发竞态

- 策略：【代码修复】sync.Once 收口。
- 改动点：controlplane/gateway.go Server 增加 `gatewayOnce sync.Once`；ensureGateway 改为 `s.gatewayOnce.Do(func(){ if s.gateway == nil { s.gateway = newGatewayState() } }); return s.gateway`；修正“并发安全”谎言注释为如实描述。gatewayState 内部已有 mu，双层锁职责分离（once 只保初始化，mu 保状态）。
- 可行性：极小（<10 行）。兼容性：无契约变化。风险：Server 结构体加字段影响按值拷贝处 → rg 确认 *Server 全指针传递；防回归＝Batch4 CI Linux runner 上 `go test -race ./internal/controlplane/ -run Gateway` 兜底（L6 一并解决执行环境）。

### 4.8 M8 接口过密与注释漂移

- 策略：注释修正【代码修复】（Batch3 顺手）；BillingStore 拆分【记录接受】P3。
- 改动点：store.go 头注释与小接口分组注释如实更新（36 个接口、按 Phase 分组目录化排版）；BillingStore 拆 Plan/Subscription/Invoice 三接口的方案写入本文档附录待排期（拆分涉及 3 实现×13 方法+handler 引用调整，估 >200 行）。
- 可行性：本次 <20 行。兼容性：注释无契约。风险：无。

### 4.9 M9 auth_test 硬编码权限计数

- 策略：【代码修复】动态派生＋下限守护双保险。
- 改动点：auth_test.go L102/L125/L568 三处 `!= 72` 改为 `want := len(collectPermSpecs())`（测试内遍历 rbacPermSpecs 计数，或直接引用导出的注册表长度）；另保留 `if want < 72 { t.Fatalf("permissions regressed below baseline") }` 下限断言防“删一组恰好凑数”。
- 可行性：极小（<15 行）。兼容性：纯测试。风险：无；收益是此后每扩权限零维护。

### 4.10 M10 Phase 6 测试质量补强

- 策略：【代码修复】对照 webhook/script 测试基线补齐。
- 改动点：
  - marketplace_test.go：补 404（未知 id）、405（错误 method）、400（坏 JSON）三类用例；断言从只看状态码升级到解析 body 关键字段（error 文案/回显字段）；
  - apikey_test.go/billing_test.go：补错误路径（不存在 id 更新/删除、越权租户访问）；
  - 跨租户隔离矩阵：t1 用户带 X-Tenant-ID:t2 访问 marketplace/apikey/billing/tenant 四域 → 统一断言 403（依赖 Batch1 H1 完成，故排 Batch3）。
- 可行性：中（~200 行测试代码）。兼容性：纯增量。风险：无；这是 Batch1/Batch2 所有安全修复的验收网之一。

### 4.11 M11 边界值守护用例

- 策略：【代码修复】补锁定用例。
- 改动点：server_paginate_extra_test.go 增加 page=0/pageSize=0/page=-1/pageSize=100000 表驱动用例（当前实现走同一分支安全，用例目的是锁行为防回归）；另在各 create 类测试中抽 webhook/marketplace 两代表补空 body POST→400 用例（decodeJSONBody 对 EOF 的行为显式锁定）。
- 可行性：小（~60 行测试）。兼容性：纯增量。风险：无。

### 4.12 M12 占位实现缺 simulated 标记

- 策略：【代码修复】四域统一打标。
- 改动点：backup_api.go/compliance/engine.go:230/canary_enhance.go:110-136/ha.go 四处占位实现的响应体增加 `"simulated": true` 字段（JSON 序列化自动带上）；UI 侧 web/ 如有详情页可在后续迭代展示角标（本次仅后端字段，前端不改不破坏）。
- 可行性：小（<20 行）。兼容性：响应体新增字段为向后兼容变更（前端忽略未知字段）。风险：无；防回归＝各补一条“响应含 simulated:true”弱断言。

### 4.13 M13 包职责混乱——记录接受

- 策略：P3 技术债专项（extension/gateway 归位、platform/controlplane 职责重划、130 文件上帝包拆域）。触发条件：controlplane 文件数突破 160 或任一新人 onboarding 因包结构阻塞时启动。本迭代仅要求：新代码遵循“引擎进领域包、handler 留 controlplane”约定，并在 CONTRIBUTING 或 README 架构节补一句约定说明（一行文档改动随 Batch4 提交）。

---

## 第5章 Low 级发现逐项修复方案

### 5.1 L1 输入校验缺口（Plugin/GatewayRoute/Script）

- 策略：【代码修复】白名单校验，三文件分散落地。
- 改动点：
  - controlplane/marketplace.go:55-62：pluginType ∈ {data,logic,integration} 白名单（以 engine.ValidActionType 同款常量表实现）；downloadURL 仅允许 http/https 前缀（复用 url.Parse 校验 scheme）;
  - controlplane/gateway.go:137-140：targetBackend 校验 host:port 格式（net.SplitHostPort + scheme 白名单 http/https/grpc）；
  - controlplane/script.go:213-233：timeoutSec 上限 clamp 至 [1,600]；execute 前检查 script.Enabled，禁用脚本 execute 返回 409。
- 可行性：小（合计 ~60 行）。兼容性：此前能创建的非法数据将被 400 拒绝——修复本体；存量脏数据不回扫。风险：前端下拉值与白名单不一致 → 以 web/ 现有表单枚举为准对齐；防回归＝每条校验一用例。

### 5.2 L2 SHA-256 无盐比较侧加固

- 策略：【缓解措施】无盐 SHA-256 配 128 位 crypto/rand 熵属业界通行（GitHub/GitHub Actions 同款做法），加盐反而破坏 hash 查找可行性；本项只修比较侧时序侧信道。
- 改动点：platform/apikey.go:91 `k.Key == hash` 改为 `subtle.ConstantTimeCompare([]byte(k.Hash), []byte(hash)) == 1`（注：ValidateKey 实际比对的是 k.Key 存的 hash 值，改造时顺带把字段语义注释澄清）。
- 可行性：极小（3 行）。兼容性：无。风险：无；防回归＝现有 apikey 平台包测试通过即可。

### 5.3 L3 删除租户无级联清理

- 策略：【缓解措施】做两件低风险事，完整级联（跨 33 接口清理全部资源）超出止血范畴记入排期。
- 改动点：controlplane/tenant.go DeleteTenant handler：a) 目标租户=="default" 时拒绝 409（平台租户保护，当前所有登录用户归属 default，删除等于自毁）；b) 删除成功后对该租户的 APIKey/Webhook/Script 三域执行循环 Delete（store 已有按 tenantID 的 delete 方法，handler 侧聚合调用 ~20 行）；其余域列入 TODO 注释。
- 可行性：小（~40 行）。兼容性：删 default 从“成功”变 409 属预期防护。风险：级联删除误伤 → 仅清三域且先删后删租户顺序固定；防回归＝tenant_test 补两用例。

### 5.4 L4 MemoryStore 存调用方指针——记录接受

- 策略：项目既定通病且新增领域已全面深拷贝（审查正面确认 cloneAPIKey/cloneInvoice 等），存量 30+ 方法逐个治理估 >300 行且回归面大。记录至技术债清单，规则固化：**今后 review 卡口——凡 store 写入 map 前必须 clone**。触发条件：出现一次真实的外部污染 bug 时启动专项。

### 5.5 L5 时间基准不一致

- 策略：【缓解措施】H2 桩翻转为显式失败后，sql_ticket.go:29 的 UTC 假时间戳不复存在，问题自然消解大半。
- 改动点：仅在团队规范层固化“store 新增代码一律 time.Now().UTC()”；不回改 MemoryStore 既有本地时间（回改会使全部 JSON 时间戳从 "+08:00" 变 "Z"，破坏快照对比与前端展示兼容）。
- 可行性：极小。兼容性：零改动即零风险。

### 5.6 L6 前端 assets 零测试与本机 -race 受限

- 策略：【缓解措施】执行环境问题不在本地修，交 CI Linux runner。
- 改动点：.github/workflows/ci.yml 增加 job 步骤：ubuntu-latest 上 `go test -race ./...`（覆盖 M7 race 验证与 C-2 并发拷贝验证）；internal/controlplane/web/assets/*.js 共 8436 行的 Vitest 化列入前端专项排期（依赖其构建链路，不在本迭代）。
- 可行性：小（workflow 约 15 行 YAML）。兼容性：CI 变严可能暴露存量 race → 若首跑即 red，允许临时 `-run` 圈定已知干净包并开 issue 跟进，不得静默跳过。风险：runner 时长增加 → race 仅跑 go 测试不跑前端构建，可控。

### 5.7 L7 魔法数 254

- 策略：【代码修复】提取常量。
- 改动点：network/engine.go:190 定义 `const maxHostsPerCClassSubnet = 254` 并替换字面量（附注释 /24 主机数=2^8-2）。
- 可行性：极小。兼容性：无。风险：无。
---

## 第6章 批次执行计划与文件冲突矩阵

> 排布原则：同一文件不得出现在同批的两个并行子任务中；跨批触碰同文件天然串行，允许。

### 6.1 Batch1 安全红线（3 子代理并行）

表：Batch1 子任务与文件清单对照表

| 子任务 | 内容 | 独占文件清单 | 产出验收 |
|---|---|---|---|
| T1.1 | H1 跨租户改造（方案 A） | k8s_cluster.go ＋ 20 个 handler 文件的调用点替换（webhook/script/apikey/tenant/billing/slo/ticket/traffic/pipeline/argocd/network/automation/gateway/marketplace/audit_query/compliance/config_hotpush/backup_api/canary_enhance 等）＋ 跨租户 403 守护测试 | go build ./... 绿；403 用例过 |
| T1.2 | H10 去 BOM | store/memory_config.go | go test -cover ./internal/store/ 绿 |
| T1.3 | H6 数值比较 ＋ L7 常量 | automation/engine.go、network/engine.go ＋ engine 表驱动测试 | ParseFloat 用例全绿 |

冲突说明：T1.1 触碰 controlplane/automation.go、network.go（handler），T1.3 触碰 internal/automation、network（引擎包），**文件不相交**。T1.1 改动面最大（111 处机械替换），建议最先启动。

### 6.2 Batch2 安全防线（2-3 子代理）

表：Batch2 子任务依赖与文件清单对照表

| 子任务 | 内容 | 独占文件清单 | 依赖 |
|---|---|---|---|
| T2.1 | H5 API Key 认证 ＋ L2 constant-time | controlplane/auth.go、server.go、platform/apikey.go ＋ apikey_auth_test.go(新) | 无 |
| T2.2 | H2 stub_guard ＋ H3 探针 ＋ M4/M5/M6 ＋ allowStubStores 开关 | store/stub_guard.go(新)、15 个 sql_*.go、multi_schema_p6.go、multi_schema.go、store.go、config.go(Config 字段)、multi_schema_smoke_test.go(新) | 无 |
| T2.3 | M1 Webhook SSRF | controlplane/webhook.go ＋ ssrf 用例 | T2.2 |
| T2.4 | M2 API Key PUT 白名单 ＋ enable/disable 端点 | controlplane/apikey.go ＋ 用例更新 | T2.1（语义联动） |
| T2.5 | H9 审计补齐 | automation.go、network.go、gateway.go | T2.2 |
| T2.6 | M3 Record 进接口 | store.go（WebhookStore/ScriptStore 加方法）、sql_webhook.go、sql_script.go、multi_schema_p5.go、webhook.go、script.go | **T2.2 且 T2.3**（store.go 与 webhook.go 串行让位） |

说明：T2.1∥T2.2 先并行；完成后 T2.3∥T2.4∥T2.5 并行；T2.6 最后收口。L5（时间基准）随 T2.2 桩翻转自然消解，无独立改动。

### 6.3 Batch3 正确性与债务（3 子代理）

表：Batch3 子任务与文件清单对照表

| 子任务 | 内容 | 独占文件清单 | 依赖 |
|---|---|---|---|
| T3.1 | H8 Phase0 四项清偿 | agent/log_collect.go（C-1 截断＋C-4 Is）、store/memory_discovery.go、memory_config.go、memory_secret.go（C-2 拷贝）、log_collect_test.go 与 memory_config_secret_test.go(新，C-3) | Batch1/2 |
| T3.2 | M7 ensureGateway ＋ H7 死代码删除 ＋ platform_config 假审计修正 ＋ L1 校验 ＋ L3 租户保护 | controlplane/gateway.go（once＋targetBackend 校验）、platform/billing.go、platform/tenant.go、platform/marketplace.go（删 Manager 保别名）、platform_config.go（审计 Action 改 *_simulated＋响应加 simulated:true）、tenant.go（default 保护＋三域级联）、controlplane/marketplace.go、script.go（timeout clamp＋禁用 execute 409） | Batch1/2 |
| T3.3 | 测试补强 M10/M11/H11/M9 ＋ L6 race job | marketplace_test.go、apikey_test.go、billing_test.go、server_paginate_extra_test.go、stub_semantics_test.go(新)、auth_test.go、ci.yml（skip 计数＋ubuntu -race job） | **全部前序** |

说明：T3.3 的跨租户隔离矩阵依赖 T1.1 行为落地；stub 语义锁定依赖 T2.2 统一策略定稿，故排尾。

### 6.4 Batch4 验证提交（单代理串行）

1. 全局验证：`go build ./...`、`go vet ./...`、`go test ./...`、`go test -cover ./internal/store/ ./internal/controlplane/ ./internal/platform/` 全绿；gofmt/golangci-lint 过（防 BOM 复发）。
2. 报告标记：REVIEW-phase1-6.md 每项发现追加状态列（已修复/已缓解/已接受排期），链接本方案章节号。
3. CHANGELOG 补“已知限制”节（H4 十五项＋M8/M13/L4/L6 排期声明）。
4. commit（建议拆 4 个提交对应 4 批）＋ push。

### 6.5 文件冲突矩阵汇总

表：热点文件跨任务占用对照表

| 文件 | B1 | B2 | B3 | 说明 |
|---|---|---|---|---|
| store.go | — | T2.2 → T2.6 | — | 同批两次占用，靠 T2.6 依赖串行化解 |
| webhook.go / script.go | T1.1 | T2.3 / T2.6 | T3.2(script) | 批内串行（T2.6 after T2.3） |
| gateway.go | T1.1 | T2.5 | T3.2 | 三批各一次，天然串行 |
| automation.go / network.go(handler) | T1.1 | T2.5 | — | 引擎包与 handler 分属不同任务 |
| memory_config.go | T1.2 | — | T3.1 | 跨批串行 |
| apikey.go(handler) | T1.1 | T2.4 | T3.3(仅测试) | 测试与实现分离 |
| tenant.go(handler) | T1.1 | — | T3.2 | 跨批串行 |
| ci.yml | — | — | T3.3 | 单点占用 |
| platform/apikey.go | — | T2.1 | — | 与 controlplane/apikey.go 不同文件 |

---

## 第7章 回归风险清单

表：回归风险与预防措施对照表

| # | 最可能破坏的现有测试/行为 | 根源修复项 | 预防/修复方式 |
|---|---|---|---|
| R1 | auth_test 三处 72 权限计数断言 | 任何触碰 RBAC 的改动 | 本方案不增删权限点；M9 在 T3.3 改动态派生后彻底解除 |
| R2 | k8s_cluster/k8s_manage 测试对 k8sTenantFromRequest 缺头行为的假设 | H1 | 现有 newTestServer=Demo:true 缺头自动放行不受影响；逐一排查显式 requireAuth:true 的构造点，期望“缺头 401”的负向用例语义不变；“非 demo 非 auth 缺头”从归一 default 变 400 属修复本体，相关用例改断言 |
| R3 | 带 JWT 且 X-Tenant-ID≠default 的用例 | H1 交叉校验 | rg 全库排查该组合；改为一致值或显式断言 403（守护用例） |
| R4 | webhook/script 测试对 (*store.MemoryStore) 类型断言模拟分支的断言 | M3 | 删断言后两后端行为统一，更新 mock 分支断言为直连接口；MultiSchema 冒烟兜底 |
| R5 | sql_ticket 等桩“Create 返回填充对象”类断言 | H2 翻转 nil | 预期翻红，随 T2.2 更新断言并由 stub_semantics_test 锁定新语义 |
| R6 | apikey PUT 全量替换既有用例（含 Enabled 字段） | M2 | name/scopes 仍在白名单内旧用例主体可过；Enabled 断言迁移到新 enable/disable 端点用例 |
| R7 | controlplane/billing.go 等对 platform 类型别名的引用 | H7 删除 | 保留 `type X = store.Y` 别名行仅删 Manager 方法；build 即验 |
| R8 | CI skip 计数基线过紧常 red | H11 | 基线取实测当前值，注释注明调整方式；只告警新增 skip |
| R9 | -race 首跑暴露 M7 之外存量 race | L6 | 允许首期 `-run` 圈定已知干净包＋开 issue 跟进，禁止静默跳过 |
| R10 | e2e（docker-compose.e2e-sec.yaml）中 webhook 回调地址触发 SSRF 拦截 | M1 | e2e 使用公网可达回调地址或 allowPrivate 开关显式开启 |
| R11 | 前端 apikey 启用/禁用按钮调 PUT 失效 | M2 | T2.4 同步排查 web/ 调用点并切新端点；前端不改则功能降级可见，须在 PR 描述标注 |
| R12 | log_collect 截断算法改动破坏日志重组 | H8 C-1 | C-3 新增多轮分片拼接还原用例先行覆盖边界 |

---

## 附录 执行注意事项

- 所有子任务开工前先拉取最新主干，避免跨批漂移；每子任务交付必须自带 `go build ./... && go vet ./... && go test ./受影响包/` 通过证据。
- “记录接受”四项（H4/M13/L4/M8拆分）由 Batch4 统一落入 CHANGELOG 已知限制节，附触发条件与排期建议，确保债务可见。
- 本方案未覆盖的新发现一律先补录 REVIEW 报告再回填本方案，禁止执行代理现场自行扩大改动面。