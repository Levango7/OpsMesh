# TD-60 A-2 auth 域双轨契约一致性比对报告

> **审计基准**：`dd46587`（2026-10-09，TD-87 批 2 首批落地后的 main 顶点）。
> **审计范围**：controlplane 本地 `/api/v1/auth/*` 与 auth-svc HTTP 网关 `/api/v1/auth/*` 的逐端点请求/响应契约对账。
> **审计方式**：纯静态读码比对（两侧 handler 全文 + 前端消费者 + 数据模型）。**未启动双栈、未产生运行时数据**；文中每条结论都以 `file:line` 标注出处，可逐条复核。
> **与 task-svc 双轨的关系**：task 域能 50/50 对账的前提是**读写同一数据面**；auth 域不满足该前提（见 §3）。本报告的定位是「切流前的契约与数据面体检」，不是切流验收。

---

## 1. 结论摘要

| 维度 | 结论 |
|---|---|
| 端点覆盖面 | 两侧 6 个认证端点 + 3 组管理端点**全部对齐**（auth-svc 为对齐而注册同名路径） |
| Cookie 层 | **逐字段一致**（名/Path/HttpOnly/SameSite/Secure/−1 清除/at 15min/rt 7d），可互换 |
| 鉴权与权限语义 | 同构（Bearer/Cookie → token 校验 → 权限点），P0 越权透出已修 |
| 响应体契约 | **5 处差异**，其中 1 处**阻断**（`/auth/me` 缺 `permissions`）、4 处可容忍 |
| 错误码契约 | 1 处差异（非 active 账号登录 403 vs 401）+ 若干文案差异（同码） |
| **数据面** | **结构性阻断**：两套用户体系分属不同库，50/50 随机切流不可等价（§3） |

**一句话**：auth 域当前**不具备 task 域那样「50/50 随机切流 + 响应对账」的前提**；补齐契约 + 决策数据面对齐方案之后，才有对账与切流可言。

---

## 2. 逐端点对账

### 2.1 POST /api/v1/auth/register

| 项 | controlplane（`internal/controlplane/auth_login.go:38`） | auth-svc（`services/auth-svc/internal/http/gateway.go:291`） | 判定 |
|---|---|---|---|
| 非 POST | 405 | 405 | ✅ |
| 公开注册闸门 | `cfg.PublicRegister=false` → **403** `public registration is disabled`（`:44`） | **无该闸门**，端点恒开放 | ⚠️ 差异 |
| IP 限流 | `loginGuard.Allow` → 429（`:49`） | `guard.allowIP` → 429（`:306`） | ✅ |
| 强口令 | `credentials.ValidateStrongPassword` 400（`:67`） | `validateStrongPassword` 400（`:311`） | ✅ |
| 用户名重复 | 409（`:72`、`:107`） | 409 `ErrUserExists`（`:320`） | ✅ |
| 免审批立即登录模式 | `AllowPublicRegister=true` → 201 + `{token,user}`（`:115-131`） | **不支持**（R6 注释明示不进 HTTP 层） | ⚠️ 差异 |
| 默认（pending）成功体 | 201 `{message, userId, status}`（`:133-137`） | 201 `{message}`（`:332-334`） | ⚠️ 字段少 2 个 |
| 默认角色绑定 | 硬绑 `role-viewer` + seed 存在性前置校验（`:80`、`:103`） | 无默认角色绑定逻辑 | ⚠️ 差异 |

**额外风险（auth-svc 侧）**：pending 是「先 `CreateUser`（默认 active）再回写 `pending`」两步（`gateway.go:328-331`），**非原子**——并发窗口内可被登录；controlplane 是创建即定状态（`:93-104`）。

**前端影响**：`web/enterprise/src/api/auth.js:7` 注册、`stores/auth.js:91` 读 `j.user`、`RegisterView.vue:125` 走 `!j.token && j.message` 判 pending。默认配置下两侧前端行为等价；`RegisterView` 的「已直接登录」分支只有 controlplane 开 `--allow-public-register=true` 才会走到。

### 2.2 POST /api/v1/auth/login ← 双轨主战场

| 项 | controlplane（`:143`） | auth-svc（`gateway.go:170`） | 判定 |
|---|---|---|---|
| 非 POST | 405 | 405 | ✅ |
| IP 令牌桶 | 429 `too many requests, slow down`（`:150`） | 429 `too many attempts from this IP`（`:187`） | ✅ 同码，文案异 |
| 账号锁定 | 429 `account temporarily locked due to too many failed attempts, try later`（`:168`） | 429 `account temporarily locked`（`:191`） | ✅ 同码 |
| 用户不存在 | 401 `invalid username or password` + `RecordFail`（`:174`） | 401 `invalid credentials` + `recordFail`（`:202`） | ✅ 同码，均防枚举 |
| 密码错 | 401 同上（`:179`） | 401 同上（`:202`） | ✅ |
| **非 active 账号** | **403**，按 pending/disabled/rejected 分文案（`:185-197`） | **401** `invalid credentials`（`service.go:69`/`:89` 返回错误后统一 401） | ⚠️ **错误码语义不同** |
| 成功体 | 200 `{token, user}`——**体带 token**（`:235`） | 200 `{user, mustChangePassword, changePasswordToken, needMFA, deviceFP}`——**体不带 token** | ⚠️ 差异（见下） |
| 首登 mustChangePassword | 200 `{user, mustChangePassword, changePasswordToken}`，不签常规 at+rt（`:207-219`） | 同语义（`:229-235` + `resp.MustChangePassword`） | ✅ |
| TD-60 设备指纹/MFA | `deviceFingerprint(r)` 仅绑 rt（`:228`） | `collectDeviceFP` + `checkAndRegister` → **`needMFA` 字段 + 未知设备二次验证**（`:195`、`:212`） | ⚠️ auth-svc 为超集 |
| Redis Session | 无 | `sessions.Create`（`:220-228`） | ⚠️ auth-svc 为超集 |

**「体是否带 token」的影响判定**：前端 `stores/auth.js:44-56` 只消费 `user` / `mustChangePassword` / `changePasswordToken`，**刻意不持有令牌**（该文件 1-2 行注释：「前端不持有令牌」）。因此：
- 对企业版前端：两侧行为一致，可容忍；
- 对依赖控制面体 token 的 Bearer 客户端 / 脚本：切流后失效（auth-svc 只认 Cookie/Bearer，不签发体 token）。

### 2.3 POST /api/v1/auth/logout

| 项 | controlplane（`:241`） | auth-svc（`gateway.go:239`） | 判定 |
|---|---|---|---|
| 成功体 | 200 `{message:"logged out"}`（`:259`） | 200 `{status:"ok"}`（`:257`） | ⚠️ 体形状异（前端 `postEmpty` 不消费体） |
| 无 token 时 | 仍 200（跳过审计继续吊销）（`:246`） | 仍 200（`:252`） | ✅ |
| 吊销动作 | at jti 黑名单 + rt 吊销 + 清 Cookie（`:253-258`） | `service.Logout`（jti+rt）+ Redis Session `Revoke` + 清 Cookie（`:248-256`） | ✅ 等价，auth-svc 多一层 Session |

### 2.4 POST /api/v1/auth/refresh

| 项 | controlplane（`:268`） | auth-svc（`gateway.go:264`） | 判定 |
|---|---|---|---|
| 缺 rt | 401 `missing refresh token`（`:275`） | 401 `no refresh token`（`:271`） | ✅ 同码 |
| rt 无效/过期 | 401 `invalid or expired refresh token` + 清 Cookie（`:282`） | 401 `refresh failed` + 清 Cookie（`:278`） | ✅ 同码 |
| 用户非 active | 401 `user not active`（`:288`） | 401（经 `RefreshTokenWithFP` 失败统一） | ✅ 同码 |
| 成功体 | 200 **`{user}`**（**:305**） | 200 **`{status:"ok"}`**（`:284`） | ⚠️ 差异 |
| 设备绑定 | `consumeRefreshToken(rt, deviceFingerprint(r))`（`:279`） | `RefreshTokenWithFP(rt, collectDeviceFP(r))`（`:274`） | ✅ 同语义 |

前端 `api/request.js` 的 refreshing 单飞只查状态码、不消费体（gateway.go:11-12、:283 注释明示）→ 该差异对前端可容忍。

### 2.5 GET /api/v1/auth/me ← **唯一阻断项**

| 字段 | controlplane（`:311`，返回 `*store.User`） | auth-svc（`gateway.go:398`） | 判定 |
|---|---|---|---|
| `permissions` | **有**——`EffectivePermissions` 的 tag 即 `json:"permissions"`，handler 展开角色并集后填充（`internal/store/model/model.go:31` + `auth_login.go:323-337`） | **无** | ❌ **阻断** |
| 角色字段 | `roleIDs`（`model.go:26`） | `roles`（`gateway.go:423`） | ⚠️ 改名 |
| 租户字段 | `tenantId`（`model.go:23`） | `tenantID`（`gateway.go:421`） | ⚠️ 改名 |
| `status`/`createdAt`/`mustChangePassword` | 有（`model.go:25/27/28`） | 无 | ⚠️ 缺字段 |
| `mode` | 无 | 有（`self-validated`） | ⚠️ 多字段 |

**为什么是阻断**：前端侧栏/操作权限门控的唯一数据源是 `/auth/me` 的 `permissions`——
`web/enterprise/src/stores/auth.js:31` `permissions = user.value?.permissions || []`，`:36-41` `hasPerm()`：**集合为空时对一切返回 false**（注释：「避免前端权限门控形同虚设」）。走 auth-svc 时 `permissions` 恒 undefined → 侧栏与受控入口**全线隐藏**。这不是显示瑕疵，是功能性断裂。

**最小补齐**：auth-svc `/auth/me` 增加 `permissions`（按 `roleIDs` 展开角色权限并集，语义与 controlplane `:323-337` 一致），并统一 `roleIDs`/`tenantId` 字段名。

### 2.6 POST /api/v1/auth/change-password

| 项 | controlplane（`:359`） | auth-svc（`gateway.go:345`） | 判定 |
|---|---|---|---|
| 非 POST | 405 | 405 | ✅ |
| IP 限流 | 有，复用 `loginGuard`（`:366`） | **无** | ⚠️ 缺（旧密码可在线爆破） |
| 新旧密码相同 | 400 `new password must differ from old password`（`:414`） | **无校验**（`internal/service/service.go:308-323` 只验旧密） | ⚠️ 缺 |
| 首登 token 优先 | 有（`:387-399`） | 有（`:366-379`） | ✅ |
| **拒绝 `body.user_id` 直调** | 不涉（仅 token 取身份） | 有，R4 设计（`gateway.go:344` 注释） | ✅ 更严 |
| 旧密错 | 401 `old password incorrect`（`:410`） | 401 `old password does not match`（`ErrPasswordMismatch` → `:387`） | ✅ 同码 |
| 常规流成功后 | **保留现有 at 有效**，200 `{message:"password changed"}`（`:457`） | **清双 Cookie 且不重签**，200 `{status:"password changed"}`（`:393-394`） | ⚠️ **会话语义不同** |
| 首登流成功后 | 签新 at+rt + 200 authResponse（`:440-455`） | 仍清 Cookie、不重签 | ⚠️ 用户被强制重新登录 |
| `MustChangePassword` 清除 | store.ChangePassword 清 | store.ChangePassword `SET must_change_password=0`（`internal/store/mysql.go`） | ✅ |

### 2.7 管理端点（/users /roles /permissions）

路径与命名对齐（`gateway.go:130-135`，注释明示对齐 `auth_users.go` / `auth_roles.go` / `auth_perms.go`）。已核：

- `toPublicStoreUser` 显式字段白名单，**不透 `PasswordHash`**（`gateway.go:760-773`，P0 已修）。
- approve/reject 需 `user:approve`，且仅 pending 可操作、其余 409（`gateway.go:479-501`，对齐 `auth_users.go:163,197`）。
- status 变更需 `user:approve`（`gateway.go:555-559`，对齐 `auth_users.go:253`）。
- GET role/permission 列表 permission 传空串 = 仅要求登录（`gateway.go:146-149`、`:699-701`，对齐 `auth_roles.go:40` / `auth_perms.go:24`）。

**未逐行核（列为待核项，不建议在未核前宣称等价）**：`POST /users` 管理员创建的用户密码/强口令语义（auth-svc `handleUsers:444-465` 走 `CreateUser` 无密码字段，controlplane 侧需对 `auth_users.go` 创建分支）；DELETE 的 `*:delete` 权限点在 controlplane 侧的实际要求。

### 2.8 Cookie 层（一致）

`internal/controlplane/auth_cookies.go` 与 `services/auth-svc/internal/http/gateway.go:102-118` 逐字段比对：名 `opsmesh_at`/`opsmesh_rt`、`Path=/`、`HttpOnly`、`SameSite=Lax`、`Secure` 条件启用、清除时空值 + `MaxAge=-1` —— **全部一致**；at=15min（`accessTokenExpiry`）、rt=7d（auth-svc 硬编码 `7*24*3600`）—— **TTL 一致**。这是双轨/切流最关键的互换前提，已具备。

---

## 3. 数据面：结构性问题（非契约瑕疵）

auth-svc 网关包注释自述设计边界：「在 auth-svc 注册的用户只在 auth-svc 生效，R8 文档声明」（`gateway.go:6-8`）。落到实现：

- **两套实体**：控制面 `internal/store/model/model.go:16` 的 `model.User`（持久化于控制面主库）vs `services/auth-svc/internal/store` 的 `User`（持久化于 `opsmesh_auth` 库）。
- **会话状态各自为政**：rt 表、jti 黑名单、Redis Session（auth-svc 独有）均不共享。

**⇒ 直接后果**：50/50 随机切流在 auth 域**不可等价**——同一凭据在两侧一成一败；响应对账会因用户集不同而大面积误报。task 域之所以能 50/50 对账，正因读写同一数据面（`docs/td60-consistency-report.md`）。

**切流前必须先决策数据面对齐**（三选一，均需立项）：

| 方案 | 做法 | 代价/风险 |
|---|---|---|
| A. 同库同表 | auth-svc store 直连控制面主库的 users/rt 表 | 改动最大但最彻底；需处理两套 store 的列名/接口差异 |
| B. 迁移 + 双向同步 | 一次性迁移 + 运行期同步用户/rt | 同步链路本身成为新故障面；一致性窗口 |
| C. 分域用户模型 | 明确「平台账号 vs 服务账号」异构，不做随机切流，改按租户/按用户群切换 | 产品语义变化；失去「无感切流」 |

---

## 4. 待办清单（按依赖排序）

1. **契约补齐（可立即做，不涉数据面）**
   - `/auth/me` 补 `permissions` 并统一 `roleIDs`/`tenantId`（§2.5，**阻断级**）。
   - `change-password` 补「新旧相同」校验 + IP 限流（§2.6）。
   - `register` 补公开注册闸门；pending 改创建即定状态（§2.1）。
   - 非 active 登录错误码对齐（403 vs 401，§2.2）——需产品定：统一 403 还是统一 401。
2. **数据面方案决策（产品/架构，阻塞后续一切）**：§3 三选一。
3. **管理端点逐行对账**（§2.7 待核项）。
4. **运行时双轨对账**：依赖 1+2 完成后，与 task 域同法做「A 实例写、B 实例读」式响应比对（注意 auth 的写操作有状态副作用，需隔离凭据集）。

---

## 5. 局限与时效声明

- 本报告为**静态比对**，未运行双栈；所有「可容忍」级判定均基于前端当前消费面的读码，若前端后续开始消费 `token`/`roles`/`user` 体，需重判。
- 审计基准 `dd46587`。**并行会话正在执行 TD-87（`internal/controlplane` 按域拆包）**，handler 物理位置可能迁移（如 `auth_login.go` → 子包）；结论按「语义」记录，物理路径以基准提交为准，迁移后需按新路径复核引用。
- 未覆盖：auth-svc 的 gRPC 面（`api/proto/v1`）与控制面内部 JWT 验签的耦合细节（TD-60 阶段 2 A-1 遗留项）、TD-86（alert-svc gRPC 鉴权）之外的 gRPC 鉴权面。

---

## 6. 追加（2026-10-10）：并行线 TD-87 拆包后的路径/命名核对

并行会话（zcode）落地 TD-87 批 1/批 2 后，本报告引用的路径逐条复核结果：

- **本报告全部 `file:line` 引用仍然有效**：`auth_login.go`、`auth_cookies.go`、`internal/store/model/model.go`、`services/auth-svc/internal/http/gateway.go` 均未迁移（`auth_login.go` 仅内部调用点改名）。
- **本报告提到的双轨机制已被并行线保留并妥善适配**（这点比报告原文更进一层）：
  `AUTH_SVC_PROXY_ENABLED` 开关的实现随引擎切片迁入 `internal/controlplane/svcproxy/proxy.go:84` 的
  `func (r *Rule) IsActive()`（私有 `isActive` → 导出 `IsActive`），调用点为 `internal/controlplane/service_proxy.go:25`
  的 `if rule != nil && !rule.IsActive()`；auth 域规则本体在 `svcproxy/proxy.go:186-192`
  （`PublicPrefix: /api/v1/auth-svc`、`EnvKey: AUTH_SVC_URL`）。
  **并且并行线补了路径改写测试**：`internal/controlplane/service_proxy_test.go:60-61`
  断言 `/api/v1/auth-svc/login → /api/v1/auth/login`、`/api/v1/auth-svc/me → /api/v1/auth/me`。
  ⇒ 本报告 §1/§4 中「机制已备、缺运行时数据」的结论不变，且该机制现有测试覆盖。
- **若后续要改控制面 auth/代理文件，需按新布局**（并行线 COORDINATION 第三十九则）：
  `auth_password.go` → `internal/controlplane/credentials/password.go`（`credentials.X`）；
  loginGuard 实现在 `loginguard`（方法 `Allow`/`RecordFail`/`Locked`/`ResetFail`/`StartSweep`/`StopSweep`）；
  代理规则类型在 `svcproxy`（`Rule`/`Rules`/`DeviceExtras`/`TaskExtras`/`Lookup`/`ValidateTargets`/`EnvKey`）。
- **本报告结论不受拆包影响**：所有结论均为跨进程的 HTTP 契约语义比对，与文件物理位置无关。
