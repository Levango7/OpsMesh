# TD-60 A-2 auth 域数据面对齐立项材料

> **基准**：`8577334`（2026-10-10 main 顶点）。
> **前置**：本文是《TD-60 A-2 auth 域双轨契约一致性比对报告》（`docs/td60-auth-consistency-report.md`）§3 的展开——
> 该报告已证：**契约层有 1 处阻断 + 5 处差异，数据层是结构性阻断**；本文只解决数据层这一个问题，并给出可执行的切流路径。
> **性质**：立项材料（只读侦察 + 方案定价 + 建议）。**不代替产品/架构决策**；所有价格都以本仓实测代码为依据，可逐条复核。

---

## 1. 问题定义

TD-60 A-2 原计划沿 task 域打法：**双轨 → 50/50 随机切流 → 全绿 1 周 → 100% → 下掉 controlplane 实现**。
该打法的隐含前提是**两侧读写同一数据面**（task 域成立，见 `docs/td60-consistency-report.md`）。

auth 域不成立，因为两套用户体系彼此独立：

| | 控制面（单体） | auth-svc（微服务） |
|---|---|---|
| 实体 | `internal/store/model/model.go:16` `User` | `services/auth-svc/internal/store` `User` |
| 落库 | 控制面主库 `users` | **独立库** `opsmesh_auth` 的 `users` |
| 会话状态 | rt 表 + jti 黑名单（进程内/MySQL） | rt 表 + jti 黑名单 + Redis SessionStore |
| 设计声明 | — | 网关包注释 R8：「在 auth-svc 注册的用户只在 auth-svc 生效」（`services/auth-svc/internal/http/gateway.go:6-8`） |

**⇒ 直接后果**：50/50 随机切流在 auth 域不可等价——同一凭据两侧一成一败，响应对账会大面积误报。**这不是实现瑕疵，是数据面分裂的必然。**

---

## 2. 现状实测：两套 schema 的结构性差异（这是定价的核心依据）

### 2.1 `users` 表对照

| 维度 | 控制面 `users` | auth-svc `users` | 差异级别 |
|---|---|---|---|
| `id` | VARCHAR(64) PK | VARCHAR(64) PK | 一致 |
| `username` | VARCHAR(64) UNIQUE | VARCHAR(128) UNIQUE | ⚠ 长度 |
| `email` | VARCHAR(255) 可空 | VARCHAR(256) **NOT NULL** | ⚠ 可空性 |
| `password_hash` | VARCHAR(255) | VARCHAR(256) NOT NULL | 兼容 |
| `status` | VARCHAR(16) | VARCHAR(32) DEFAULT 'active' | 兼容 |
| `created_at` | DATETIME | TIMESTAMP | 兼容 |
| `must_change_password` | 有（迁移追加） | TINYINT(1) | 兼容 |
| **`tenant_id`** | **有**（迁移 018，TD-70 越权修复的地基） | **无** | ❌ **结构性缺失** |
| **`role_ids`** | **JSON 数组列**（`001_initial.sql:111`） | **无此列**——`user_roles(user_id, role_id)` 关联表 | ❌ **结构性不同** |

### 2.2 角色/权限模型对照（第二处结构性差异）

| | 控制面 | auth-svc |
|---|---|---|
| 角色权限 | `roles.permissions` **JSON 数组列** | `role_permissions(role_id, permission_name)` 关联表 + **外键 CASCADE** |
| 用户角色 | `users.role_ids` JSON | `user_roles(user_id, role_id)` 关联表 + 外键 |

（控制面 DDL：`internal/store/sqlstore/migrations/001_initial.sql:106-124`；auth-svc DDL：`services/auth-svc/internal/store/schema.sql` 全表。）

### 2.3 会话/令牌面的差异（第三处，易被漏掉）

即使两套用户表完全统一，**rt 与黑名单仍不共享**：控制面 rt 表 + jti 黑名单 vs auth-svc `refresh_tokens` + Redis `SessionStore`。这意味着：

> **只统一「用户数据」不足以支撑随机切流**——登录可以两面通，但 refresh 旋转、登出吊销跨面即失效。
> 任何方案若不同时处置会话面，50/50 仍会在第二跳（refresh）上断裂。

---

## 3. 方案定价

### 方案 A：同库同表（auth-svc 直连控制面主库）

| 项 | 内容 |
|---|---|
| 做法 | auth-svc store 改为指向控制面主库的 `users`/`roles`/`permissions`/rt 表，按控制面模型重写 auth-svc store |
| 必须先做 | ① 用户表补 `tenant_id` 语义（auth-svc 目前无租户概念，需补租户隔离路径，**这是安全面不是简单加列**）；② `user_roles`/`role_permissions` 关联表 ↔ `role_ids`/`permissions` JSON 的模型转换；③ `email` NOT NULL 与 `username` 长度的兼容决策 |
| 代价 | **最大**：一个域的 store 重写 + 一次 schema 迁移 + 租户隔离补齐 |
| 收益 | 唯一能获得「真·单数据面」⇒ 之后才可能有严格意义的响应对账与 50/50 |
| 风险 | 迁移期双写/回滚复杂；auth-svc 的外键 CASCADE 语义在控制面 JSON 模型下丢失 |
| 回滚 | 难（schema 已迁移） |

### 方案 B：一次性迁移 + 双向同步

| 项 | 内容 |
|---|---|
| 做法 | 首次全量迁移用户 → 运行期双向同步（双写或 CDC）→ 稳定后停止一侧写入 |
| 必须先做 | §2.2 的模型转换成为**同步器的翻译层**（两套模型都要维护）；**且必须把 §2.3 的 rt/Session 也纳入同步**，否则 refresh 断裂 |
| 代价 | 大：同步器是新故障面（顺序、丢事件、一致性窗口）；会话同步放大故障面 |
| 收益 | 可灰度，不必一次切换 |
| 风险 | **同步链路本身成为新的单点**；一致性窗口内两边可见不同状态 |
| 回滚 | 中（停同步即回退，但已迁移数据需清理） |

### 方案 C：分域用户模型（不做统一，改按群组切换）

| 项 | 内容 |
|---|---|
| 做法 | 承认两套用户体系并存；不做随机切流，改按**租户/用户群**整批切换，每批前后都可一键回退 |
| 代价 | 小（主要是切换编排与沟通成本） |
| 收益 | 与 R8 设计声明一致；每批影响面可控、可回滚 |
| 风险 | 产品语义变化：**失去「无感切流」**，用户可能需要在另一侧重置/首次登录 |
| 回滚 | **易**（切回开关即可） |

### 方案 D（本材料新增）：二元开关整域切换，替代 50/50

| 项 | 内容 |
|---|---|
| 依据 | **该机制已落地**：`AUTH_SVC_PROXY_ENABLED` 默认 false，设 true 时 auth 域整体走 auth-svc（实现在 `internal/controlplane/svcproxy/proxy.go:84` 的 `Rule.IsActive()`，auth 规则 `:186-192`，调用点 `internal/controlplane/service_proxy.go:25`），且已有路径改写测试（`service_proxy_test.go:60-61`） |
| 做法 | 放弃「50/50 随机」，用**二元开关**做整域切换；切换窗口内以「影子对账」替代「随机对账」 |
| 为什么比 50/50 更适合 auth | 有状态系统（登录态、rt 旋转、登出吊销）无法被有意义地「切一半」——同一个浏览器会话不可能一半 cookie 在 A、一半在 B。50/50 在 auth 域只会制造**跨面会话断裂**，而不是灰度 |
| 代价 | 小（开关已存在，主要是运维流程） |
| 收益 | 一键切换、**一键回滚**（改 env 即回）；每次切换影响面整域、语义清晰 |
| 风险 | 切换是整域的，故障时影响全部请求（但回滚同样是一步） |
| 回滚 | **最易** |

---

## 4. 建议

**排序建议（工程视角，最终需产品确认）**：

1. **先做契约补齐**（不依赖数据面决策，立即可做）：见 auth 契约报告 §4 第 1 步——`/auth/me` 补 `permissions`（阻断级）、`change-password` 补新旧校验与限流、`register` 补公开注册闸门、错误码对齐。**这一步与数据面方案无关，任何方案下都要做。**
2. **切流机制采用方案 D，放弃 50/50**：auth 域用 `AUTH_SVC_PROXY_ENABLED` 二元开关；这是已落地能力，且比 50/50 更符合有状态系统的物理现实。
3. **用户面统一采用方案 C 起步、A 为目标**：短期承认两套用户体系并存，按租户/用户群分批切换；若产品要求「单数据面 + 严格对账」，再立项方案 A（代价见 §3 A，含租户隔离补齐这一安全面）。
4. **不要把方案 B 作为默认选项**：auth 的会话状态（§2.3）使同步器故障面过大，仅当产品明确要求「渐变且不停机」时再评估。

**给产品/架构的决策请求**：
- 是否可以接受「auth 不做随机 50/50、改为整域开关 + 分批切换」？（方案 D + C）
- 是否要求最终收敛到单一用户数据面？（若是，需为方案 A 的**租户隔离补齐**排期——这不是加一列，是补一条隔离路径）

---

## 5. 验证方法（若后续动工）

沿用本仓已有实践（config-svc TD-65 立的三段断言 + task-svc 双轨对账）：

1. **schema 对账**：迁移后跑「控制面 users 全部列 ⊇ auth-svc 读侧引用列」的静态断言（可照搬门禁第 21 节「建表列集合 ⊇ SQL 引用列」判据）。
2. **真库往返**：CI integration job 注入 DSN，跑「A 实例写、全新 B 实例读」（同 config-svc `mysql_integration_test.go` 判据）。
3. **切换演练**：`AUTH_SVC_PROXY_ENABLED=false/true` 各跑一遍登录 → refresh → 登出全链路，验证两种模式下会话均自洽（这是方案 D 的验收核心）。
4. **回滚演练**：切换后立即回退，验证原会话与新会话均可用。

---

## 6. 局限声明

- 本文为只读侦察，未改动任何代码或 schema。
- §2 的 DDL 引用以控制面 `001_initial.sql` + 迁移 018 与 auth-svc 内嵌 `schema.sql` 为准；若并行会话正在调整任一 schema，需按新布局复核。
- 未评估：auth-svc 的 gRPC 面消费者（若有外部客户端直连 auth-svc gRPC，切流影响面不同）；Redis 不可用时 auth-svc 的降级路径对切流的影响。
- 与并行会话关系：全程只读，`internal/controlplane`、`internal/store`、`services/auth-svc` 均未改动。

---

## 7. 附：端到端双轨开关冒烟结果（2026-10-10，实测，非推断）

**方法**：Linux 二进制（`GOOS=linux` 交叉编译）同时起 auth-svc（memory + HTTP enabled）与 controlplane（memory，`AUTH_SVC_PROXY_ENABLED=true`，`AUTH_SVC_URL` 指向 auth-svc，同一 HMAC 密钥），走真实 HTTP 请求。覆盖：代理路径、两侧登录、首登改密、代理 `/me`、开关反向验证。

| 用例 | 结果 | 结论 |
|---|---|---|
| 开关关闭时 `/api/v1/auth-svc/login` | **404** | ✅ 二元开关生效（`svcproxy.Rule.IsActive()` 实测双向有效） |
| 开关关闭时本地 `/api/v1/auth/login` | 401（端点在、需凭证） | ✅ 控制面本地路径不受开关影响 |
| 无凭证打代理前缀 | **400 `missing X-Tenant-ID`** | ⚠️ 聚合层 `requireTenantContext` 先于转发 ⇒ **代理前缀不可能作为登录入口**（登录只能走控制面本地或直连 auth-svc）。这与本地 `/api/v1/auth/login` 无需凭证的语义**不等价**，是切流后必须正视的差异 |
| 控制面本地登录 + 首登改密 | 200 → 200，下发 at/rt | ✅ 本地路径完好 |
| 直连 auth-svc 登录 + 首登改密 | 200（带 `changePasswordToken`）→ 200 | ⚠️ **改密后 cookie 被清空且不重签** ⇒ 首登改密客户端**丢失全部会话**必须重新登录（controlplane 同场景会发新 at/rt）。实证了契约报告 §2.6 该处差异 |
| **auth-svc 签发的 cookie → 控制面 `/api/v1/auth-svc/me`** | **403 `permission denied: auth:read`** | ❌ **阻断级缺陷，见下** |

### 7.1 阻断级缺陷：代理权限闸引用了控制面目录里不存在的权限点

- **实测**：持合法 cookie 请求 `/api/v1/auth-svc/me` ⇒ 403 `permission denied: auth:read`。
- **根因**：`internal/controlplane/svcproxy/proxy.go:194-201` 的 auth 规则要求 `auth:read`/`auth:write`，而控制面权限目录 `internal/store/model/perm.go` 的 `rbacPermSpecs` **没有 `auth:` 组**（有 `user:`/`role:`/`permission:`）。`requirePermission` 是「用户权限集 ∋ required」的等价判断，无空串豁免 ⇒ **任何控制面身份（含 admin）都不可能通过** ⇒ 代理认证面在生产上**恒 403**，双轨开关打开即不可用。
- **为什么门禁没抓住**：目录守护 `internal/store/sql_rbac_catalog_test.go` 断言的是**手工维护的** `handlerRequiredPerms` 清单（handler 里的字符串字面量）；而代理的权限点来自**规则数据**（`rule.ResolvePerm(method, path)` 运行时解析），不在该清单里 ⇒ 静态对账存在盲区。
- **与历史缺陷同类**：2026-09-29 那次「15 个权限点被 handler 引用却从未进入目录 ⇒ 全部角色恒 403」是同一形态；那次靠 sim 取证发现，这次靠端到端冒烟发现。
- **修复选项（均在并行线文件面，本线未动）**：
  1. **补目录**：把 `auth:read`/`auth:write` 加入 `rbacPermSpecs`。代价最小，但 admin 独有 ⇒ 普通用户的自服务端点（me/refresh/logout）仍不可达，**与本地语义不等价**。
  2. **对齐本地语义（推荐）**：auth 域自服务端点（me/login/logout/refresh/change-password）本地本就只要求「合法 token、无权限点」；应让 svcproxy 支持「仅认证、不查权限」的规则形态（如 `Perm: ""` 语义化为豁免 + `handleServiceProxy` 跳过权限循环），auth 规则的 permRules 相应改为该形态。
  3. **映射到现有目录点**（如 me→`user:read`）：可授权但仍与本地语义不等价。
- **顺带必须做**：扩展 `sql_rbac_catalog_test.go`，把 svcproxy 三张规则表的 `Perm`/`PermRules` 也纳入「引用点 ⊂ 目录」断言，否则下一处同类漂移仍不会被静态抓住。

### 7.2 对切流方案的影响

在原四方案之上追加一条**前置条件**：无论选 A/B/C/D，**auth 代理规则与权限目录/本地语义的对齐是切流的硬前置**；在此修复前，`AUTH_SVC_PROXY_ENABLED=true` 打开只会得到恒 403 的认证面。本项工作量小（一处目录或一处规则语义 + 一段门禁断言），但**必须由 `internal/controlplane` 与 `internal/store` 的负责方（并行线）实施**——本线按要求不碰其文件面。
