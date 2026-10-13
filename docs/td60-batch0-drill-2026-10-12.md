# TD-60 auth 收敛「批次 0」演练报告（2026-10-12）

> **定位**：批次真正开切前的**全流程演练**——真二进制、真 `cutover-migrate` job、真共享 MySQL，
> 三个模拟用户（不是断言脚本，而是以用户身份逐步操作并汇报的 agent）跑完
> 「切换前 → 切换后 → 单用户回退」三段。
> **与冒烟的分工**：`smoke-cutover.sh` 验机制（45 项判据、一次跑完即退出）；
> 本报告用 `drill-batch0.sh` 演练台（栈与名册留在原地，供人/agent 以真实用户身份反复操作）。
> **纪律**：模拟用户只操作、不"修好"任何现象；所有发现都带原始证据（状态码/错误原文/日志行）。

---

## 1. 演练配置

| 项 | 值 |
|---|---|
| 栈 | 控制面（19580/19581/19582）+ auth-svc（19590/19591），同一 HMAC 密钥 |
| 存储 | **共享 MySQL**（`opsmesh-mysql-evidence-v2` / `opsmesh_test`，start 时重建）——迁移 job 是独立进程，memory 后端下它看不到控制面建的账号，故必须共用真库（与 runbook 的生产形态一致） |
| 名册 | **文件来源** `AUTH_CUTOVER_ROSTER_FILE`（热加载只有文件来源支持，也是 runbook 的生产旋钮） |
| 账号 | `drill-u01..u06`（本侧口令 `Drill-User-2026x!`），本侧建号经 admin API |
| 批次 | u01/u02/u03 → 后追加 u04；u05/u06 留作对照 |
| 迁移 | 真 job：`opsmesh cutover-migrate --users … --force-change --credentials-out …` |
| 模拟用户 | 3 个 agent，人设分别为「细致谨慎」「急性子值班员」「不懂技术的业务同事」 |

---

## 2. 三段实测

### 2.1 切换前（三个用户，全 200）

| 步骤 | 结果 |
|---|---|
| 登录 | 200，响应体 **含 `token`**，Set-Cookie 下 `opsmesh_at`(900s) + `opsmesh_rt`(604800s) |
| `/me` | 200 |
| `refresh` | 200，at+rt **双轮换**且即时生效 |

### 2.2 切换（真 job）

```
[cutover-migrate] 本批结果：新建 3 · 收编 0 · 名册已有 0 · 失败 0 · 名册新增 3
[cutover-migrate] 一次性口令已写入 …creds-063611.txt（权限 0600）
```
名册写入带来源注释（`# cutover-migrate 2026-10-11T22:36:54Z added=3`）；追加 u04 时再次实测该 job 增量幂等（`新建 1 · 失败 0`）。

### 2.3 切换后（三个用户，路径一致）

| 步骤 | 结果（三人一致） |
|---|---|
| `/me`（旧会话） | **401 `{"error":"user not found"}`** |
| `refresh`（旧会话） | **401 `{"error":"refresh failed"}`**，且响应把本地 Cookie 清空 |
| 旧口令登录 | 401 `{"error":"invalid credentials"}`（对侧文案） |
| 一次性口令登录 | 200：`mustChangePassword:true` + `changePasswordToken`（约 5 分钟），**无 `token` 字段**，仅下发 `opsmesh_at`(300s) |
| 改密 | 200 `{"status":"password changed",…,"mustChangePassword":false}`，**会话只经 Set-Cookie** 下发（改密即得完整 at+rt） |
| 新口令登录 + `/me` | 200 / 200（`mode:"self-validated"`） |

**结论**：切换对「已登录用户」= **需重新登录一次**，且**全程零引导**（详见 F1）。

### 2.4 单用户回退（u03：摘除名册条目，热加载 0s 生效）

| 步骤 | 结果 |
|---|---|
| 用**原始本地口令**登录 | **200，响应体重新出现 `token`**（判据：应答方已切回本侧），用户 id 回到本侧形态 |
| 切换期间设的新口令 | 401 `{"error":"invalid username or password"}`（本侧文案；该口令只存在于对侧） |
| 回退后登录的会话 | `/me` 200、`refresh` 200（结构与命名也回到本侧） |
| **切换期间建的对侧会话** | 第一次 `refresh` **200 `{"status":"ok"}`**，但 `/me` **401** ——**"半死会话"**（见 F2） |

---

## 3. 发现（按严重度）

### F1 切换对用户零引导；`/me` 的 `user not found` 会吓到人（中）

三个 agent 独立得出同一结论：**流程可走通，但产品一句人话都没说**。

- 用户看到的是：`user not found`（我账号没了？被盗了？）→ `refresh failed` → `invalid credentials`（我密码记错了？）。
- `mustChangePassword` / `changePasswordToken` 是**给客户端看的字段**，不是给人看的话；没有人告诉用户"用 IT 发的一次性口令登录并改密"。
- 自愈开关不在产品里，**在 OTP 的发放渠道里**：没拿到 OTP 的用户在这套 API 面前无路可走。
- 另一层观察：`user not found` 的机械成因是「旧 JWT 的 `sub` 指向**本侧**用户 id，而对侧库里的同名人 id 不同」——`/me` 按 sub 转发过去后查不到。文案出自对侧（auth-svc）。
- **建议**：① 控制面侧已补的"会话失效原因提示"（`8f687c3`，登录页显示"登录状态已失效，请重新登录"）覆盖了界面侧；② 建议对侧把「迁移后会话查不到」的文案改得更可行动（例如"账号已完成迁移，请重新登录"），或由控制面在转发前识别该形态并给统一文案；③ runbook 的"通知本批用户需重新登录一次"必须连同 OTP 一起下发，演练证明这是唯一自救入口。

### F2 回退后对侧会话"半死"：refresh 200 而 /me 401（中）

**根因（代码级）**：`cutover.RouteRefresh` 的规则是
```go
if !localOwns { return true }   // 对侧令牌：恒转发，不看名册
return localUserInRoster        // 本侧令牌：按名册
```
对侧签发的 rt 在**本地查不到**（`localOwns=false`），于是无论该用户是否还在名册里都会转发对侧；
对侧仍持有该用户与其会话 ⇒ refresh 成功并轮换，但 `/me` 按名册裁决走本地 ⇒ 本地按 sub 查不到 ⇒ 401。
用户/客户端视角：**"刷新说还活着，一干正事就报错"**。

- **影响有界**：前端 `request.js` 的路径是「401 → refresh → 成功则重试原请求 → 重试再 401 → 清会话跳登录」，即**一轮往返后自愈**，不会卡死；风险主要在**只看 refresh 的 API 消费方/后台任务**（会把 401 当"仍然登录"）。
- **修法选项（推荐 ①）**：
  ① **回退时吊销对侧会话**：名册热加载发现**条目被摘除**时，控制面对 auth-svc 调"吊销该用户名全部会话"⇒ 之后任何 refresh 直接 401，语义干净。**前置缺口**：auth-svc 目前只有自助 `POST /api/v1/auth/logout`，**没有管理员按用户吊销会话的接口**（本次已核实其全部注册路由），需对侧补一个内部管理端点。
  ② 本地侧对「对侧 rt + 请求同时带 at-JWT」的形态，先用共享密钥解出用户名再判名册（不在名册则不转发）⇒ 只堵住 at 仍有效（≤900s）的窗口，at 过期后仍有洞，属部分修复。
  ③ 只改文档：把"回退后对侧会话仍会短暂 refresh 成功"写进 runbook。**不推荐**——留一个说谎的接口违反本仓的诚实基调。
- **在①落地前**：runbook §3 建议加一句时效说明（回退后 ≤1 次 refresh 窗口内可能出现"假活"）。

### F3 登录限流：实测突发 5 次（非 10），429 无等待提示；**文档此前写错，已修**（中）

- 代码两侧同值（已核实）：`internal/controlplane/loginguard/guard.go` burst=5、refill=1/6s；`services/auth-svc/internal/http/guard.go` 同值——**跨侧一致 ✓**。
- 实测（急性子人设，同 IP 0 间隔）：冷启动 `200×5 → 429×…`，其后约每 3~6s 才放行 1 次；429 体为 `{"error":"too many requests, slow down"}`，**无 `Retry-After`、无任何 `X-RateLimit-*` 头**。
- **批次含义**：同一办公网出口 IP 下，切换瞬间只有前 ~5 个用户能登录成功，其余全 429 且无等待指引 ⇒ 用户会反复重试、把额度持续打空，并**很可能误判为"切换把认证搞坏了"**。建议 runbook §2.1 增加一条：批次划分要考虑**同出口 IP**（同办公室/同 VPN 出口的用户不要放在同一时间窗集中登录），或在切换窗口内临时提高 burst（需评估撞库防护）。
- **文档修正**（本人已改，4 个用户可见文件）：`README.md`、`docs/api-specification.md`(×2)、`docs/api-reference.md`、`docs/feature-design.md`(×2) 原写「10 突发 / 3s 补 1」→ 改为与代码一致的「5 突发 / 6s 补 1」；E2E 用例注释同步（该旧值来自收紧前的实现）。

### F4 迁移前置：对侧 admin 必须已完成首登改密（低，且 job 已正确处理 ✓）

实测：对侧 admin 处于强制改密态时，job **fail-closed 并给出可行动提示**：
`[cutover-migrate] 对侧管理员 "admin" 处于首登强制改密态：请先用该账号人工登录完成改密，再跑迁移`。
——设计正确，无需改代码；已把这一步自动化进演练台 `start`，并建议写进 runbook（运维前置条件）。

### F5 对侧响应头重复（低）

切换后同一请求的安全头出现**两份**：`Content-Security-Policy`、`Referrer-Policy`、`X-Frame-Options`、`X-Content-Type-Options`（对侧还额外补了 CORS 与 HSTS）。疑似两层各加一遍。属卫生问题（个别客户端/缓存对重复头行为不一致），建议对侧收敛为单一来源。

### F6 两侧字段命名不一致（低-中）

同一语义字段两侧写法不同：对侧 `roleIds` vs 本侧 `roleIDs`；对侧 `/me` 同时返回
`roleIDs`+`roles`、`tenantID`+`tenantId`、`userID`+`id` 等重复语义。前端若依赖某一种写法，切换即裂。
本仓 TD-60 的老毛病（"契约面裂缝"）在 auth 域已修完探明的部分，这条是本次演练新暴露的残余。

- **状态（2026-10-13 晚，板第七十九则）**：已收口。规范名定死 = controlplane 既有契约（不新造拼写）：
  请求 `role_ids`（auth_users.go:110/303；前端 UsersView 按此发）、响应 `roleIDs`/`createdAt`/
  `mustChangePassword`（store.User 的 json tag；前端按此读）——单体与前端本就统一，**偏差全在 auth-svc**，
  本笔只收 auth-svc 侧：请求收规范名 + 旧拼写作过渡别名；列表逐项经 toPublicUser（此前直出 proto 的
  `role_ids`/`created_at`(对象)/`must_change_password` ⇒ 前端"角色列空白 + 时间列 Invalid Date"）；
  登录/注册/改密的 user 补 `permissions`/`roleIDs`/`createdAt`（B4 的对侧半边）。最险的一条：
  改号 PUT 此前只认 `roleIds` 而前端发 `role_ids` ⇒ 切流后"改角色"**静默忽略**（200、字段没动）——
  守卫 5 条 + 变异 1/1 钉住（`gateway_field_names_test.go`），规范名表见
  `docs/td60-auth-consistency-report.md` §7。过渡别名只增不减、零断裂，切流完成后统一删。

### F7 两条正向验证 ✓

- **must-change 门禁是严的**：处于强制改密态的会话打 `/api/v1/users` 与 `/auth/me` 均 **403 `password change required`**；登录虽下发 `opsmesh_at`，但**不构成越权面**。
- **未进批次的用户零影响**：u05/u06 在切换后登录 200、响应体形态不变、路由日志零增量。

---

## 4. 对 runbook 的具体修订清单

1. §2 / §2.1 增加**前置条件**：对侧 admin 完成首登改密（F4）。
2. §2.1 增加**批次划分约束**：同出口 IP 的用户不要集中在同一时间窗（F3，附实测数字）。
3. §3 增加**"批次视图/会话不跨切换存活"**的时效说明（F2，在吊销接口落地前）。
4. §4 明确 **OTP 必须与"需重新登录一次"的通知同时下发**（F1：这是唯一自救入口）。
5. §5 观察窗增加两条可得信号：切换后 `/me` 401 的**原文分布**（`user not found` 占比）与 **429 计数**（F1/F3）。

---

## 5. 复现方式

```bash
# 起演练台（真二进制 + 共享 MySQL + 6 个模拟账号；含对侧 admin 引导）
bash deploy/scripts/drill-batch0.sh start 6
bash deploy/scripts/drill-batch0.sh coords        # 坐标（URL/口令/路径），交给模拟用户
bash deploy/scripts/drill-batch0.sh migrate drill-u01,drill-u02,drill-u03
bash deploy/scripts/drill-batch0.sh unroute drill-u03   # 单用户回退（热加载 ≤30s）
bash deploy/scripts/drill-batch0.sh status
bash deploy/scripts/drill-batch0.sh stop
```
