# OpsMesh 模拟用户演练 · 发现汇总（2026-10-12）

> **定位与来历**：本文原落在仓库外（`F:/tmp/opsmesh-sim-user-findings.md`）——因为 2026-10-12
> 16:00–16:45 之间 `F:\Nexus\OpsMesh` 被持续清空（顶层条目 39 → 2，`.git` 消失，连
> `services/auth-svc/internal/` 都在我们构建中途消失）。仓库稳定后于 **2026-10-13 并入 `docs/`**，
> 正文保持发现当时的原样，只**追加**每条的最新状态（不回头改写当时的观察）。
>
> **演练方式**：真二进制（从当时 HEAD `aaad5a1` 导出构建，**不依赖工作树**）+ 3 个真 agent 进程 +
> 6 个 RBAC 角色账号，配 3 个 subagent 以不同岗位真实使用（值班运维 / 只读审计含越权试探 / 新同事）。
> 全部发现带原始证据（状态码、响应原文、**代码行号**——行号以发现时 HEAD 为准，修复后已漂移）。
>
> **状态索引（2026-10-13 晚）**：A1 `04cbb92`(+`b18c42a`) ✅ · A2 `3ff1ac6` ✅ · A3 `087a9bd` ✅ ·
> B1 `219b5e4`(peer 续修 `01fb528`) ✅ · B3 `56ac092`(peer 续修 `703fd75`) ✅ · B4 `9a6156e` ✅ ·
> B2 半闭（环境台层已解，产品问题开放）· B5 属环境台文档错，修正清单见 §B5。

---

## A. 高严重度（生产会踩）

### A1 非 demo 部署下，agent 注册的设备**在控制面永远看不见**（三个 agent 独立复现）
- 现象：`GET /api/v1/devices` → `{"seg-1":[],"seg-2":[],"seg-3":[]}`、`GET /api/v1/agents` → `[]`，
  而三个 agent 进程都在心跳、CMDB 里能列出真实主机（AMD Ryzen 9 7945HX / 32 核）。
- 根因（读码确认）：agent 经 gRPC 注册**不带租户声明**；唯一把空租户兜底成 `default` 的分支是
  `internal/controlplane/grpc/grpc.go` 里的 `g.Cfg.Demo`——**demo 关着就落 `TenantID=""`**；
  占位设备 `dev-<agentID>` 同样为空租户；而 `GET /api/v1/devices` 走 `Snapshot("default")`、
  `GET /api/v1/agents` 走 `Agents("default")`，`memory.go` 的过滤是
  `if tenantID != "" && a.TenantID != tenantID { continue }` ⇒ **空租户被 default 视图整体滤掉**。
- 佐证：`grpc.go:236-239` 的注释**自己就预言过这一幕**（"否则裸注册 agent 落 tenant=""，
  被 handleAgents("default") 过滤导致控制面看板/API 永远看不到该 agent（e2e-real firstAgentID 空列表根因）"）
  ⇒ **已知失败模式，只靠 demo 兜底盖住**。
- 判别实验（值班 agent 做的）：`GET /api/v1/devices/totally-fake-id` → `404 device not found`；
  `GET /api/v1/devices/dev-<真agentID>` → **`403 tenant mismatch`**（是 403 不是 404 ⇒ 数据在，租户不对）。
- 修法方向：① 注册路径对空租户做**显式兜底**（不再依赖 demo），或 ② 拒绝无租户注册并让 agent 必须声明，
  ③ 列表接口对"空租户数据"给出可见提示而不是静默过滤。
- **状态（2026-10-13）**：已修——`04cbb92` 按方向①落地（裸注册落**默认租户**并打 hint 日志，
  不再挂在 demo 开关上；网关注入租户仍优先），守卫 `internal/controlplane/grpc/agent_tenant_visibility_test.go`
  两条（非 demo 裸注册在 `Agents("default")` 可见 / 网关注入租户仍胜出），变异（恢复 demo 门控）判红；
  连带修两条既有测试红 `b18c42a`（旧"空租户宽松"被正名，非回归）。

### A2 `GET /api/v1/me` 在**按文档带 `X-Tenant-ID` 时**丢失身份（两个 agent 独立复现）
- 带 `X-Tenant-ID: default` → `{"mode":"gateway-injected","roles":null,"tenantID":"default","userID":""}`
- **不带**该头 → `userID` 正确回填 `user-e4f2fa8...`
- 对照 `GET /api/v1/auth/me`（同 token）→ 完整 username/roleIDs/26 条 permissions。
- 根因：`internal/controlplane/http_infra.go:129-144`——**租户头非空的分支提前 return，从不从 JWT 回填 UserID**；
  只有"头为空"的分支（147-152）才回填。而接线单/常识都要求"带租户头"⇒**照做就触发**。
- 影响：新同事人设据此判定"我没登录成功/身份注入没接上"；同一进程两个 `me` 端点各说各话。
- **状态（2026-10-13）**：已修——`3ff1ac6`（`tenantFromBearer` 一并取回 roles，两个分支都回填
  `UserID` 与 `Roles`），守卫 `internal/controlplane/me_identity_backfill_test.go`（带/不带
  `X-Tenant-ID` 必须得到同一身份；头与 token 租户不一致仍 403），变异判红。

### A3 被拒的越权尝试**不留审计**（两个 agent 独立复现）
- 值班 agent：2 次越权 DELETE + 1 次失败建任务；审计 `count` 前后恒为 21，动作只有
  `user_login/user_create/user_change_password`。
- 审计员 agent：6 项越权尝试全部无痕。
- 对值班/等保场景，**越权尝试恰恰是最该留痕的**。
- **状态（2026-10-13）**：已修——`087a9bd`：只对"已认证但被拒"的 403 记审计（权限不足/租户不匹配/
  用户不匹配三类），401 一律不记（扫描器噪音）；query 串永不入账（`sanitizeAuditDetail`）；
  5 处 403 站点接线；守卫 `internal/controlplane/authz_audit_test.go` 三条，变异判红。

---

## B. 中severity（体验/契约）

### B1 `cmd/device-sim` 报"20/20 registered"是**假成功**
- 它 POST `/api/v1/devices/register` —— **该路由不存在**（实测 `404 {"error":"not found","path":"/api/v1/devices/register"}`）；
  代码 `main.go:334-340`：**非 2xx 直接 `return nil`**（注释："Non-2xx but we continue"）⇒ 404 被当成功计数。
- 心跳 `POST /api/v1/devices/{id}/heartbeat` 同样 404，且 `main.go:393-399` **根本不检查状态码** ⇒ `errors=0` 恒为 0，无观测价值。
- 该工具也从不发 `X-Tenant-ID`。
- ⇒ 结论：**日志里的 20 台设备从未存在过**；平台上真实设备只有 3 条 agent 占位设备。修法：状态码非 2xx 必须计入 errors 并打印响应体。
- **状态（2026-10-13）**：已修——`219b5e4`（register 非 2xx 如实报错 + 指引改用 `opsmesh --mode=agent`；
  心跳查状态码；文件头写明端点不存在与工具去向，留待立项）；peer 续修 `01fb528`（两处 `io.ReadAll`
  的错误码被 errcheck 判红，如实并入错误详情）。

### B2 三个 agent 共用同一 agentID ⇒ 数据串味
- `internal/agent/agent.go:194-215` 的 `loadOrCreateAgentID` 把身份持久化到 `<DataDir>/agent.id`；
  同机同 DataDir 的三个 agent ⇒ **同一 ID**。
- 后果：同租户内 3 个 segment 各建一条同 DeviceID 的设备 ⇒ CMDB `collected:3`，但 CI ID 相同互相 upsert
  ⇒ **CI 只剩 1 条**（同一份数据被数 3 次又合并）。修法：每个 agent 实例独立 DataDir（部署文档/脚本层）+ 需评估"同 ID 多 segment"是否该允许。
- **状态（2026-10-13）**：**半闭**——环境台层已解（每个 agent 实例独立 `--data-dir`，见 §D 重建要点）；
  **产品问题开放**：同机同 DataDir 多实例共用身份是否该允许/该警告，未裁定，留账。

### B3 404 面不统一（代码注释与实现不符）
- 注释称"修复 4：用 jsonErrorMux 包装 mux，将 404 统一为 JSON"，实测未匹配路由仍返回
  `text/plain` + `404 page not found`；而个别业务 404 是 `{"detail":"Not Found"}`（FastAPI 风格）。
- 影响：新同事人设"完全无法区分拼错路径/服务未部署/权限不足"，反复猜路径。
- **状态（2026-10-13）**：已修——`56ac092`（`JSONErrorMux` 拦截 catch-all 写出的纯文本 404，
  统一成 JSON 信封且带实际路径；handler 有意写的 JSON 404 原样放行；延迟 WriteHeader 不缓冲大响应），
  守卫 3 条 + 变异判红；peer 续修 `703fd75`——包装器漏转发 `Flush()` 导致 SSE 长连接 500
  （"包装器×流式"这条交互两侧测试都没覆盖，补 `json404_stream_test.go` 两条 + 变异）。

### B4 登录响应 `permissions: null`，权限只能靠解 JWT 或再调 `/auth/me`
- 三个人设均报同一现象：登录体里 `user.permissions` 为 null，而 JWT claims / `auth/me` 里有 43-26 条权限。
- 影响：用户无法从登录响应知道"我能做什么"。
- **状态（2026-10-13）**：已修——`9a6156e`（登录/注册/刷新响应统一走 `authUserWithPerms`，
  浅拷贝 + 现算权限，**不改动 store 对象**），守卫 `auth_login_perms_test.go`，变异判红。

### B5 接线单文档错（我方演练台的问题，非产品缺陷，但同批记）
- `/api/v1/overview` **不存在**（全路由表无此路径）；网关路由真实路径是 `/api/v1/gateway/routes`（不是 `/gw/routes`）。
- operator 角色**无 `quota:read` / `platform:read`**，而接线单让"看配额" ⇒ 文档与权限模型对不上。
- **状态（2026-10-13）**：属环境台文档错——修正清单已落在本文 §D（重建 `sim-users.sh` 时按此写），
  不再按"产品缺陷"跟踪。

---

## C. 正向验证（可信的部分）

- **RBAC 面可信**：5 项写操作（任务/建号/改角色/删设备/建网关路由）全部 `403 permission denied: <具体权限名>`；
  跨租户头、伪造 `X-User-Roles: admin`、伪造 `X-Federation-Forwarded: 1`、无凭证四类旁路全部被拒；
  **零越权成功**。
- **权限校验先于存在性校验**（不存在的 ID 也先 403）⇒ 不泄露资源是否存在。
- 错误文案里 `permission denied: task:write` 这类**是清晰的**（新同事人设称其为"全平台唯一让我读懂下一步的一句话"）。
- 审计能查到登录事件（含 userID/username/tenantID）；`/api/v1/auth/me` 返回完整权限集。

---

## D. 复现方式

环境台（从 HEAD 导出构建，**不受工作树被清空影响**）：
```bash
# 控制面 19680 + 3 个 agent（同一二进制 --mode=agent --control-addrs=127.0.0.1:19681）+ 6 个角色账号
bash deploy/scripts/sim-users.sh start --agents 3      # 脚本本身随工作树一起丢了，需重放
```
三个 subagent 的原始报告要点已并入本文；它们各自的会话仍可续问细节。

### 重建 `sim-users.sh` 的要点（重放时按此写，含演练中交的学费）
- **agent 必须逐个 `--data-dir`**（B2：同 DataDir 会共用 `agent.id`，数据串味）；
- agent 端点形态：`opsmesh --mode=agent --control-addrs=127.0.0.1:<gRPC 口>`；控制面 HTTP 与 gRPC
  端口分开（演练用 19680 / 19681）；
- 账号按 RBAC 六角色建（`role-admin/role-operator/role-viewer/…`），口令满足 12 位+特殊字符策略；
- **不要照抄接线单**：`/api/v1/overview` 不存在；网关路由是 `/api/v1/gateway/routes`；
  operator 无 `quota:read`/`platform:read`（B5）；
- 二进制从 `git archive` 导出构建（演练当日正是靠这点在仓库被清空中继续跑完）。
