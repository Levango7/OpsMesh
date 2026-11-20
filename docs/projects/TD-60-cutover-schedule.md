# TD-60 三域切流排期（auth → task → device）

> **状态：已排期（2026-11-18 拍板）**
> **适用范围**：TD-60 阶段 2（task / device / auth）从 controlplane 双份实现收敛到
> `task-svc` / `device-svc` / `auth-svc` 的执行排期。
> **上位文档**：`docs/projects/TD-60-domain-convergence-charter.md`（阶段、D+C 规则、逐域回滚）。
> **事实基线**：`docs/td60-consistency-2026-11-17.md`（2026-11-17 静态审计，`main@89012346`）、
> `scripts/td60-consistency.py` + `scripts/td60-consistency-baseline.json`（一致性门禁）、
> `docs/td60-auth-cutover-runbook.md`、`docs/td60-task-cutover-runbook.md`。
>
> 本文只排期，不代替 runbook；任何切换动作以对应 runbook 与 `GET /api/v1/admin/service-routing`
> 的运行时取证为准。**排期不是完工声明**：未完成的项在 §2 逐条列出。

---

## 2026-11-19 预演结论（变更）

> **依据**：`docs/auth-dualstack-rehearsal-2026-11-19.md`（真实 controlplane + auth-svc + 真实 MySQL 双栈预演，基线 commit `4cbb62c0`）。
> **矩阵 22/22 PASS、0 FAIL**：本地基线、直连 auth-svc、users 列表与租户隔离、管理面保持本地、
> 名册代理 login/me/logout 与停用恢复全部通过。但 **11-25 的「50/50 随机切流」判定 NO-GO**，
> 改为**名册门控的二选一批量切流（方案 C/D）**。本节结论覆盖 §5 日历中原 11-25 auth 行；
> task / device 日期不变（见 §5 注）。

### 变更依据（预演实际证据）

| 判定 | 证据 |
|---|---|
| 22/22 矩阵通过 | 预演 §3：A-E 五组（本地基线、直连 auth-svc、users 列表与租户隔离、管理面保持本地、名册代理与基线恢复）全部 PASS，0 FAIL |
| 随机比例**未实现** | `AUTH_SVC_PROXY_RATIO` 只出现在 `internal/controlplane/svcproxy/proxy.go:93` 的注释里，没有任何 Go 代码读取；实际路由是二选一：`AUTH_SVC_PROXY_ENABLED` 总开关 + 名册匹配（`internal/controlplane/cutover/router.go:42-45,104-113`），login/me/logout 逐端点裁决 |
| 会话**不可跨侧** | 两侧用户表在不同 schema（controlplane `opsmesh` vs auth-svc `opsmesh_auth`，预演 §1.1/D6），refresh token 与 session store 各自持有；随机把同一浏览器打到两侧会产生分裂会话：auth-svc 登录无法被 controlplane 刷新/校验，反之亦然（`docs/td60-auth-data-plane-proposal.md` §1；预演 §6） |
| 合同缺陷会污染 A/B 信号 | auth-svc 登录响应重复 `roleIDs`/`roleIds`（D2），users 列表混用 `created_at`/`must_change_password`/`role_ids` 别名（D3）；50/50 会表现为随机客户端故障，而不是干净对照 |
| 回退粒度不匹配 | 随机分流没有「批次」可摘，回退只能整域关闭；与 D+C 的逐批可摘原则冲突（`docs/projects/TD-60-domain-convergence-charter.md:84-88`） |

**替代方案**：同一时刻一个用户要么全走 controlplane 本地、要么全走 auth-svc；按名册批次推进，
逐批观察、逐批可摘。预演对方案 C/D 给的是 **conditional GO**，条件即 §3.1 的九项门禁；
r2 复验（2026-11-20）九项已全 PASS，11-25 维持 conditional GO，剩余前置见下方 r2 节。

### 日历变更（只动 auth）

| 日期 | 原计划 | 变更后 |
|---|---|---|
| 11-24 | auth Go/No-Go | **修复门禁（Fix Gate）**：§3.1 G1-G9 全绿并逐条签字；未过则 11-25 不切 |
| 11-25 | auth 50/50 随机切换 | **名册批次 A（二选一）**：只切 **1 个非 admin 用户**；仅在 11-24 门禁全绿时写名册；否则占用 11-24 缓冲日修缺陷并 **HOLD**，不写名册、不重启切流 |
| 11-25 ~ 11-27 | 观察窗口 | 保留，按批次 A 观察；11-27 17:00 签「继续/回退」 |
| 11-30 起 | 100% | 保留，批次 A 稳定后按 runbook 追加批次；「50/50 阶段」= 名册累计覆盖约 50% 活跃用户，仍不引入请求级随机 |

---

## 2026-11-20 r2 复验结果

> **依据**：`docs/auth-dualstack-rehearsal-2026-11-19-r2.md`（真实 controlplane + auth-svc + 真实 MySQL 8 + Redis 7，基线 `main@c2bcd884`、修复 `256d759c`）。
> **结论**：§3.1 九项门禁 **9 PASS / 0 FAIL / 0 NOT-DONE**，round-1 矩阵复跑 **30/30 PASS**，拆除干净、无产品代码改动。
> 两条门禁命令在 r2 中被证明失效（G6 判别列、G9 计数），已在 §3.1 校正；门禁本身以更强证据通过。

| # | 结果 | 一行证据（r2 §2） |
|---|---|---|
| G1 | PASS | 严格解码 `duplicates=[]`；login/user 键集合只含 `roleIDs`，`roleIds present=False`（首登与改密后登录均验） |
| G2 | PASS | auth-svc 与 controlplane users 顶层键均 `['users']`、`alias_hits=[]`、每人 9 个规范键；信封键恰为 `hasMore,page,pageSize,total,users` |
| G3 | PASS | 首登改密后 `top_mustChangePassword=False`、`nested_mustChangePassword=False`（D4 修复后回读用户） |
| G4 | PASS | 种子 admin `login user.tenantId=default`、`/me tenantId=default`（嵌套层级不同，值一致） |
| G5 | PASS | `docker compose -f docker-compose.mysql.yml config` 退出 0；`mysql-master` 仅一个端口条目，slaves 无重复 `environment` |
| G6 | PASS | DSN 分别为 `/opsmesh_auth?` 与 `/opsmesh?`；`opsmesh_auth` 有 `user_roles`/`role_permissions`，`opsmesh` 二者均无；同一用户名两侧独立成行（判别列已换，见 §3.1） |
| G7 | PASS | `redis-cli ping=PONG`；登录后 `session:<sha256(at)>` 存在，登出 `sessionRevocation=revoked` 且键被删，旧 token `/me`→401 |
| G8 | PASS | `cutover-migrate --users batch-a-user` 新建 1、OTP 写 `credentials.txt`；`opsmesh_auth.users` 为 active/`must_change_password=1`；独立影子登录 200（`changePasswordToken` len 445） |
| G9 | PASS | 名册 `admin` 精确命中 0、非注释条目 1、loader `count=1`；含 `admin` 的名册启动退出 1（fail-fast）；路由日志 3 行全为 `batch-a-user`、admin 0 行 |

**矩阵复跑**：A-E 五组 **30/30 PASS**（本地基线、直连 auth-svc、users 分页与租户隔离、管理面保持本地、名册代理与基线恢复）；结束时已恢复 proxy off / 空名册基线。

### 11-25 判定（r2 更新）

| 方案 | 判定 | 依据 |
|---|---|---|
| 50/50 按请求随机切流 | **NO-GO（维持）** | `AUTH_SVC_PROXY_RATIO` 仍无 Go 代码读取；两侧会话分属不同 schema，随机分流会产生分裂会话 |
| 名册批次 A（1 个非 admin 用户） | **conditional GO** | G1-G9 在真实进程/库/Redis 上全绿；仅在下列四项 r2 剩余前置全部签认时才写名册 |

r2 剩余前置（任一未满足即 11-25 **HOLD**，用 11-24 缓冲日修复，不写名册、不重启切流）：

1. **OTP 交付 + 目标环境影子登录**：批次 A 用户已在 auth-svc 预建为 `active`，一次性口令与「需重登一次」通知按批准渠道送达，并在目标环境完成一次影子登录（r2 只证明产物可生成，未证明人已收到）。
2. **Redis 撤销启用**：生产 auth-svc 置 `AUTH_SVC_SESSION_STORE_ENABLED=true` 并纳入监控；出现 `Redis SessionStore disabled` 即失去 G7 的即时吊销保证。
3. **7 天基线指标冻结**：§3 P5 的 5xx / p99 / 核心动作成功率 / 429 / leader / stale task / batch 计数基线已回溯导出并冻结。
4. **回退演练完成**：本批 `cutover-unroute` 软回退演练已执行并记录耗时（runbook 参考 20~23s）。

> G6/G9 命令校正见 §3.1：G6 不再用 `must_change_password` 判别（两侧 schema 现均有该列），改查 `user_roles`/`role_permissions` 表形状；G9 不再用 `wc -l`（会计入 `cutover-migrate` 注释头），改用 `grep -v '^#' | grep -c .` 的非注释条目数。

---

## 1. 排期原则

1. **逐域串行**：一次只切一个域；不允许 task / device / auth 在同一次变更里一起切
   （`docs/projects/TD-60-domain-convergence-charter.md:113`）。
2. **D + C**：整域开关 + 按增量（auth 按用户群名册；task 按新任务归属；device 按整域开关）
   切换，不搬历史数据（charter:84-88）。
3. **50/50 是观察点，不是按请求随机分流**：原 50/50 方案已被 2026-09-27 的 C 案标记
   Superseded（charter:86-88、`docs/tech-debt.md:149`）。本排期的「50/50 阶段」指
   **半量观察**：auth 用名册覆盖约 50% 活跃用户；task / device 在整域开关下用影子读对账 +
   选定一半租户/设备段做业务验证。**不存在按请求随机分流的实现**。
4. **可回退优先**：M2/M3 期间不删除 controlplane 旧实现；任何异常先把该域切回本地，
   再排查（charter:214-216）。
5. **不带半切状态过夜**：切换只在工作日 10:00 前完成，14:00 前完成首轮抽验；
   周五与法定节假日前一天不执行 100% 切换。

---

## 2. 事实边界（排期不把未完成写成已完成）

| 能力 | 截至 2026-11-18 的事实 | 证据 | 本排期如何对待 |
|---|---|---|---|
| auth 切流机制 | 名册 + 分端点裁决 + `cutover-migrate` / `cutover-unroute` + 登录失败计数已建成；真进程冒烟 31/31、批次 0 演练、软回退 20~23s / 硬回退 4~6s 已实测 | `docs/td60-auth-cutover-runbook.md:129-135`；`docs/td60-batch0-drill-2026-10-12.md`；`deploy/scripts/smoke-cutover.sh` | 机制可直接复用；**仓内默认配置为关闭**（`AUTH_SVC_PROXY_ENABLED=false`、`AUTH_SVC_HTTP_ENABLED=false`、名册默认空，见 `deploy/docker/docker-compose.prod.yml:519-527,611-614`）；未发现生产切流记录，若生产已另行执行需在 Go/No-Go 重新核对 |
| auth 管理面路径 | `/api/v1/auth-svc/users` 等管理端点经双轨前缀仍会 404（A-1） | `docs/td60-consistency-2026-11-17.md:96` | 切流准备项，必须先补代理规则或明确「管理面保持本地」 |
| task leader 切换 | controlplane 已移除本地 leaderLoop，改由 gRPC 调用 task-svc `LeaderElectorService` | `internal/controlplane/leader_elector_client.go`；提交 `e0276a38`、`7c5a835d` | **已完成**，排期不重复建设 |
| task HTTP 代理 + 增量开关 | `/api/v1/task-svc/*` 代理、`taskProxyPermission` 分级、`TASK_SVC_OWNS_NEW_TASKS` 与路由联锁已落地；`TASK_SVC_PROXY_ENABLED=true` 时 `/api/v1/tasks` 根路由与子路径改走 task-svc gRPC | `internal/controlplane/server_lifecycle.go:681-722,817-840`；`internal/controlplane/cutover/router.go:74-126`；提交 `00791d82`、`4e882941`、`5cb829c9` | **机制已完成**；默认 `TASK_SVC_PROXY_ENABLED=false`，生产未切 |
| task agent gRPC 适配层 | 只有 `storeAgentService`（直写 controlplane store）；`PullTasks` 仍走 `g.svc().ClaimTask()`，`ReportResult` 仍走 `g.svc().SubmitResult()`；远程 `rpcAgentService` 未实现 | `internal/controlplane/grpc/agent_service.go:11-23,127-143`；`internal/controlplane/grpc/grpc.go:564-590` | **未完成**，是 task 100% 切流的硬前置（见风险 R2） |
| task 批次 / 灰度归属 | batch / canary 在代理模式下仍落本地；与 task-svc 的批次视图归属未定（T-2） | `docs/td60-consistency-2026-11-17.md:65` | 准备项，Go/No-Go 前必须拍板 |
| device netsec 下沉 | `pkg/netsec` 已抽出，device-svc 已有端点与 `DeviceExtras` 代理规则 | `pkg/netsec/`；`internal/controlplane/svcproxy/proxy.go:231-318`；提交 `6cd411e5` | **已完成** |
| device federation 下沉 | device-svc 无 federation 等价物；仍在 controlplane | `docs/projects/TD-60-domain-convergence-charter.md:58,116` | **未完成**，device 切流前置 |
| device 字段契约 P0 | D-1（列表字段）、D-2（详情字段）、D-3（`/system`）仍未闭合 | `docs/td60-consistency-2026-11-17.md:80-83,157-164` | **未完成**，device 准备阶段必须清零 |
| 一致性门禁 | `make td60-consistency` 已落地；31 条已知差异登记在 baseline；新增未登记 P0 退出 1 | `Makefile:76-78`；`scripts/td60-consistency.py:16-20`；`scripts/td60-consistency-baseline.json` | **已完成**；注意它是路由级门禁，**看不见 D-1/D-2 字段级 P0** |
| 阶段 2 收敛进度 | 2026-11-16 记录约 60%；2026-10-11 实测残余 5,980 行（task 2,691 / device 1,339 / auth 1,950） | `docs/tech-debt.md:149`；`docs/td60-domain-convergence-analysis-2026-10-11.md:19-24` | 删除量以 M4 逐域实测为准，不把 5,980 当作今天精确行数（charter:24-26「待复核」） |

---

## 3. 前置条件（所有域共用，Go/No-Go 当天逐条签字）

| # | 前置 | 可执行判据 | 证据归档 | 责任 |
|---|---|---|---|---|
| P1 | 一致性门禁绿 | `make td60-consistency` 退出 0；`python3 scripts/td60-consistency.py --baseline scripts/td60-consistency-baseline.json --json` 的 `counts.p0 == 0`；`stale_baseline` 为空或逐条解释 | 归档 JSON 与退出码 | 门禁 owner |
| P2 | 零新增 P0 | 本域在审计 §6 的 P0 清单为 0；device 必须额外确认 D-1/D-2/D-3 已闭合，且契约测试能判红 | 审计清单 + 契约测试记录 | 域 owner |
| P3 | 真栈冒烟绿 | MySQL 8 + Redis 7 起真实双栈；auth 跑 `bash deploy/scripts/smoke-cutover.sh`，task 跑 `bash deploy/scripts/smoke-task-cutover.sh`；device 在 Go/No-Go 前补齐等价冒烟（审计 §7 第 5 条） | 冒烟日志与时间戳 | 执行 owner |
| P4 | 逐域回退演练完成 | auth / task / device 各完成一次软/硬回退；记录实际耗时与回退后抽验结果 | 回退演练记录（§7 模板） | 回退执行人 |
| P5 | 基线指标已冻结 | 切流前 **7 个自然日**同域基线（用既有看板回溯导出，不是从 11-19 才开始采集）：5xx 率、p99、核心动作成功率、429、leader 数量、stale task、batch 计数 | 看板快照 + CSV | 观察员 |
| P6 | 角色已填实 | §8 的角色表无「待填」 | 排期附件 | 项目 owner |
| P7 | auth 运维前置 | `AUTH_SVC_HTTP_ENABLED=true`、`AUTH_SVC_URL` 已设、对侧 admin 已完成首登改密、本批一次性口令与「需重登一次」通知同批送达、`admin` 永不入册 | runbook §0/§1.1/§2 | 运维 |
| P8 | task 功能前置 | agent gRPC 适配层（`rpcAgentService`）完成并通过 agent 领取/上报 E2E；batch/canary 归属已拍板；切换前排空在飞批次 | E2E 日志 + 决策记录 | 开发 + 域 owner |
| P9 | device 功能前置 | federation 下沉完成；D-1/D-2/D-3 契约测试红转绿；device 冒烟脚本入库 | 测试日志 | 开发 |
| P10 | 开关方向已核对 | **三套开关不要混**：① `OPSMESH_SERVICE_PROXY` 是 svcproxy 规则的**停用列表**，列出的域回落本地、默认空 = 全部转发（`internal/controlplane/svcproxy/proxy.go:517-525,534-556`）；② task 的 `/api/v1/tasks` 代理由 `TASK_SVC_PROXY_ENABLED` 独立控制（`internal/controlplane/server_lifecycle.go:700-715`）；③ auth 切流转发由 `AUTH_SVC_PROXY_ENABLED`（`Rule.IsActive`）控制（`internal/controlplane/auth_cutover.go:240-244`）。执行前在 staging 各验证一次 | staging 记录 | 回退执行人 |
| P11 | 变更记录就绪 | 每次切换使用 §10 模板；CHANGELOG / 文档更新责任人已指定 | 模板链接 | 记录员 |

> **门禁绿不等于可以切**：`scripts/td60-consistency.py` 是 grep 式静态路由/信封/状态词表比对，
> 不覆盖字段级契约（D-1/D-2 即为此类）。`counts.p0=0` 的含义是「没有**未登记**的新增 P0」；
> 审计剩余的 D-1/D-2/D-3 已在 baseline 中，必须按 P2 单独清零并签字。

### 3.1 auth 修复门禁清单（2026-11-19 新增，11-24 逐条签字）

> 依据 `docs/auth-dualstack-rehearsal-2026-11-19.md` §4 的缺陷与 §6 的 conditional GO 条件。
> **九项全绿才允许写 11-25 批次 A 名册**；任一项未过，11-25 HOLD，用 11-24 缓冲日修复。

| # | 缺陷 / 前置 | 验证命令 | 通过判据 | 责任 |
|---|---|---|---|---|
| G1 | 重复 JSON key（D2）：auth-svc 登录响应同时发 `roleIDs` 与 `roleIds`（`services/auth-svc/internal/http/gateway.go:1096-1097`） | `curl -s -X POST http://<auth-svc>:19690/api/v1/auth/login -H 'Content-Type: application/json' -d '{"username":"<user>","password":"<pwd>"}' -o login.json`，再用下方严格解码脚本 | 严格解码不报 `duplicate keys`；login 只保留 `roleIDs` | auth-svc 开发 |
| G2 | users 列表混用别名（D3）：`created_at`（protobuf 对象）、`must_change_password`、`role_ids`、`roleIds`（`gateway.go:670-675`） | `curl -s -b cookies.txt 'http://<cp>:19680/api/v1/users?page=1&pageSize=2' -o users.json`；`python3 -c "import json;d=json.load(open('users.json'));bad=[k for u in d['users'] for k in ('created_at','must_change_password','role_ids','roleIds') if k in u];assert not bad,bad;print('OK')"` | 顶层键 ⊆ `users,page,pageSize,total,hasMore`；每个 user 不含上述四个别名键 | auth-svc 开发 |
| G3 | change-password 返回过期嵌套 user（D4）：顶层 `mustChangePassword:false`，嵌套 `user.mustChangePassword` 仍为 `true`（`internal/controlplane/auth_login.go:567`） | 初始口令登录后 `curl -s -X POST http://<cp>:19680/api/v1/auth/change-password -b cookies.txt -H 'Content-Type: application/json' -d '{"oldPassword":"<old>","newPassword":"<new>"}' -o chpwd.json`；`python3 -c "import json;d=json.load(open('chpwd.json'));assert d['mustChangePassword'] is False and d['user']['mustChangePassword'] is False;print('OK')"` | 顶层与嵌套字段都为 `false` | controlplane auth 开发 |
| G4 | 种子 admin 登录体 tenant 为空（D5）：登录体 `tenantId:""`，`/auth/me` 返回 `default`（`services/auth-svc/internal/store/mysql.go:234`；`gateway.go:1148-1152`） | `curl -s -X POST http://<auth-svc>:19690/api/v1/auth/login -H 'Content-Type: application/json' -d '{"username":"admin","password":"<pwd>"}' -o login.json`；`python3 -c "import json;d=json.load(open('login.json'));assert d['tenantId']=='default',d['tenantId'];print('OK')"`；对 `/auth/me` 做同一断言 | login 与 me 的 `tenantId` 均为 `default`，响应字节一致 | auth-svc 开发 |
| G5 | compose 重复 `ports`（D1）：`docker-compose.mysql.yml:9` 与 `:19` 重复定义，Compose v5.7 拒绝解析 | `docker compose -f docker-compose.mysql.yml config >/dev/null` | 退出码 0；`mysql-master` 仅一个 `ports` 块（同时清理 slaves 重复 `environment`） | 部署 owner |
| G6 | schema 混用（D6）：auth-svc 绝不能指向 controlplane schema | `echo "$AUTH_SVC_DSN"` 必须含 `/opsmesh_auth?`；`mysql -h <host> -P <port> -u <user> -p -N -e "SELECT table_schema, table_name FROM information_schema.tables WHERE table_schema IN ('opsmesh','opsmesh_auth') AND table_name IN ('user_roles','role_permissions') ORDER BY table_schema, table_name;"` | DSN 指向独立 `opsmesh_auth`；结果里 `opsmesh_auth` 恰 2 行（`user_roles`、`role_permissions`），`opsmesh` 0 行。**不要用 `must_change_password` 判别——两侧 schema 现均有该列（2026-11-20 r2 校正）** | 运维 |
| G7 | Redis 撤销缺失（D7）：`AUTH_SVC_SESSION_STORE_ENABLED=false` 时登出只靠 jti/RT，`cutover-unroute` 无法即时吊销会话 | `redis-cli -h <redis> -p <port> ping`；启动 auth-svc 时置 `AUTH_SVC_SESSION_STORE_ENABLED=true`；登出后检查响应 `sessionRevocation` 字段 | `PONG`；logout 响应不含 `unavailable (Redis disabled)`；登出后旧 access token 访问 `/me` 返回 401 | 运维 |
| G8 | OTP 预迁移与影子登录：批次用户必须先在 auth-svc 建好、审批、收到一次性口令，并完成一次直连影子登录 | `mysql -e "SELECT username,status FROM opsmesh_auth.users WHERE username IN ('<batch users>');"`；对每个用户 `curl -s -o /dev/null -w '%{http_code}' -X POST http://<auth-svc>:19690/api/v1/auth/login ...` | 全部 `status=active`；每人影子登录 200；OTP 与「需重登一次」通知同批送达 | 运维 + auth owner |
| G9 | admin 不入册：`admin` 永不写入切流名册，loader 对保留账号 fail-fast | `grep -c '^admin$' "$AUTH_CUTOVER_ROSTER_FILE"`；`grep -v '^#' "$AUTH_CUTOVER_ROSTER_FILE" \| grep -c .` | `grep` 输出 0；非注释条目数 = 批次 A 用户数（1），名册只含该非 admin 用户。**不要用 `wc -l`——会把 `cutover-migrate` 写的注释头数进去（2026-11-20 r2 校正）** | 切流执行人 |

```bash
# G1 严格 JSON 解码：出现重复 key 直接失败
python3 - <<'PY'
import json
def strict(pairs):
    keys = [k for k, _ in pairs]
    assert len(keys) == len(set(keys)), f"duplicate keys: {keys}"
    return dict(pairs)
json.load(open("login.json", encoding="utf-8"), object_pairs_hook=strict)
print("OK: no duplicate keys")
PY
```

> G1-G4 是合同缺陷，G5-G7 是部署/环境缺陷，G8-G9 是切流卫生项；证据统一归档到
> `docs/auth-dualstack-rehearsal-2026-11-19.md` 的后续复跑记录或同目录新增复跑日志（r2 即
> `docs/auth-dualstack-rehearsal-2026-11-19-r2.md`）。

---

## 4. 逐域顺序与理由（数字来自 2026-11-17 审计）

### 4.1 审计计数与派生比率

| 顺序 | 域 | 控制面操作 | 微服务操作 | 共有 | 仅控制面 | 仅微服务 | 控制面覆盖率（共有/控制面） | 并集一致率（共有/并集） | 剩余 P0 |
|---:|---|---:|---:|---:|---:|---:|---:|---:|---:|
| 1 | auth | 18 | 19 | 18 | 0 | 1 | **100.0%**（18/18） | **94.7%**（18/19） | 0 |
| 2 | task | 41 | 34 | 34 | 7 | 0 | **82.9%**（34/41） | **82.9%**（34/41） | 0（原 T-1 已修） |
| 3 | device | 16 | 32 | 15 | 1 | 17 | **93.8%**（15/16） | **45.5%**（15/33） | 3（D-1/D-2/D-3） |

> 出处：`docs/td60-consistency-2026-11-17.md:37-42`（计数）、`:50-54`（严重度）、`:157-164`（剩余 P0）。
> 比率是本文按上表计算的**路由操作级**派生值，不是「收敛进度」，也不是字段级契约一致率；
> 旧报告 v1.2 的 100% 同样只是路径对齐率（charter:48-51）。
>
> **复跑差异（2026-11-18 实测）**：在当前 `main` 上运行
> `python3 scripts/td60-consistency.py --baseline scripts/td60-consistency-baseline.json --json`
> 得到 `p0=0 / p1=0 / p2=0 / known=31`，但原始计数为 task 44/32、device 14/23、auth 18/20。
> 与审计表不同的原因是审计后新增端点与静态解析范围变化；**本文按用户要求采用 2026-11-17 审计口径**，
> Go/No-Go 当天重新冻结并更新本表，不把复跑差异当作进度变化。

### 4.2 顺序与理由

| 顺序 | 域 | 理由 |
|---:|---|---|
| 1 | **auth** | 控制面 18 个操作全部在 auth-svc 有对应（仅 1 个服务侧多出的 `GET /roles/{id}`，能力增强）；剩余 P0 为 0；名册/搬号/回退机制与真进程冒烟已就绪；回退实测 20~23s/4~6s。堵点是 A-1/A-2/A-3 三个 P1 与运维交付，不是路由缺口。先切它可以用最小代码风险跑通「切换—观察—回退」全流程。 |
| 2 | **task** | 控制面覆盖率 82.9%，7 个仅控制面操作全部是可枚举的 P1/P2（T-2/T-3/T-4/T-5/T-8），无剩余字段级 P0；leader 选主已交给 task-svc。但 agent gRPC 适配层未实现，任务建在 task-svc 后 agent 仍从 controlplane store 领取，必须先在准备阶段补齐——这是功能性硬前置，不是路由门禁能发现的。 |
| 3 | **device** | 路由覆盖率 93.8% 看似高，但并集一致率只有 45.5%（17 个服务侧多出的 agent/自举通道）；**唯一剩余的 3 个 P0 全在 device 字段契约**（D-1/D-2/D-3），切早了就是前端空白/系统页 404；federation 尚未下沉。放最后，先完成字段 DTO、`/system`、federation 与 device 冒烟。 |

> 该顺序与 `docs/tech-debt.md:149` 的「auth 先走通（零代码）→ task leader 切换 → HTTP 切 →
> agent 适配 → device 下沉+切」一致；`docs/td60-domain-convergence-analysis-2026-10-11.md:137`
> 也明确 auth → task → device。排期只把「已完成」的 leader 切换从工作项中移除，不重复计工。

### 4.3 「50/50」在各域的载体

| 域 | 50/50 阶段的载体 | 不做的事 |
|---|---|---|
| auth | 名册覆盖约 50% 活跃用户；批次按 runbook：第 0 批彩排 + 第 1 批低风险真实租户，第 2 批起每批 ≤5% 租户且 ≤3 个租户（取小者）、稳定后翻倍；累计 25% 后必须按 runbook 重新评估再继续；最大客户单独成批 | 不按请求随机分流；不按租户切（auth-svc 用户表无租户字段，见 `docs/td60-cutover-decisions.md:7-9`） |
| task | 整域代理开启 + leader 契约已切到 task-svc（验证恰好 1 个）+ 影子读对账；选定约 50% 租户/agent 池做业务验证与对账 | 不引入请求级 50/50；不在未实现 agent 适配层时切换 |
| device | 整域代理开启后用影子读对账；选定约 50% segment/租户做 provision/metrics/系统页验证 | 不在 D-1/D-2/D-3 未闭合时切换；不把 netsec/federation 复制到 device-svc |

---

## 5. 总日历（工作日级别）

| 日期 | 星期 | 域 | 阶段 | 关键动作 | 签收 |
|---|---|---|---|---|---|
| 2026-11-19 | 四 | 全部 | 准备开始 | 门禁基线、7 天指标冻结起点、角色填实、回退演练排期 | 项目 owner |
| 2026-11-19 ~ 11-23 | 四~一 | auth | 准备 / 修复 | 关闭/接受 A-1/A-2/A-3；按 §3.1 修复 G1-G9；打开 auth HTTP 网关；admin 改密；名册与 OTP 准备；回退演练 | auth owner |
| 2026-11-24 | 二 | auth | **修复门禁（Fix Gate）** | §3.1 G1-G9 全绿并逐条签字 + §3 P1-P7/P10-P11；真栈冒烟；未过则 11-25 不写名册，用本日缓冲修缺陷 | 项目 owner + auth owner |
| 2026-11-25 | 三 | auth | **名册批次 A（二选一）** | 仅门禁全绿时执行：**1 个非 admin 用户**写入 `AUTH_CUTOVER_ROSTER_FILE`；10:00 前完成首批，14:00 前首轮抽验；门禁未过则 **HOLD**，不重启切流 | auth owner + 观察员 |
| 2026-11-25 ~ 11-27 | 三~五 | auth | 观察窗口（批次 A，3 个工作日） | 每小时记录；11-27 17:00 前签「继续/回退」 | auth owner |
| 2026-11-30 | 一 | auth | **100% 切换** | 批次 A 通过后按 runbook 追加批次；剩余用户入册；`AUTH_SVC_OWNS_NEW_ACCOUNTS=true` | auth owner |
| 2026-11-30 ~ 12-04 | 一~五 | auth | 观察窗口（5 个工作日） | 登录 5xx=0；429 不持续；12-04 17:00 签收 | auth owner |
| 2026-12-07 ~ 12-08 | 一~二 | auth | 旧实现移除 | 删除 controlplane auth 登录/会话/本地 store 直写与后台循环；若 A-1 判定「管理面保持本地」，保留对应管理 handler 并在 runbook 标注；保留 cutover router、loginGuard | 执行 owner + 回退执行人 |
| 2026-12-09 ~ 12-11 | 三~五 | auth | 移除后验证（3 个工作日） | build/test/lint/smoke；404/405 回归；12-11 17:00 签收 | auth owner |
| 2026-11-19 ~ 12-04 | 四~五 | task | 准备（与 auth 并行） | agent gRPC 适配层；batch/canary 归属；T-3/T-4/T-5/T-8；影子读对账；回退演练 | task owner |
| 2026-12-07 | 一 | task | Go/No-Go | 逐条核对 §3；不过则顺延，不切 | 项目 owner |
| 2026-12-09 | 三 | task | **50/50 切换（观察点）** | `TASK_SVC_PROXY_ENABLED=true`（`/api/v1/tasks` 根路由与子路径改走 task-svc gRPC）；leader 契约已切到 task-svc，启动后验证恰好 1 个；50% 租户/agent 池验证 | task owner |
| 2026-12-09 ~ 12-11 | 三~五 | task | 观察窗口（3 个工作日） | leader=1；claim/report 成功率；batch 计数；12-11 17:00 签收 | task owner |
| 2026-12-14 | 一 | task | **100% 切换** | `TASK_SVC_OWNS_NEW_TASKS=true`；新任务全部归 task-svc；agent 适配层在线 | task owner |
| 2026-12-14 ~ 12-18 | 一~五 | task | 观察窗口（5 个工作日） | 无失联任务增长；12-18 17:00 签收 | task owner |
| 2026-12-21 ~ 12-22 | 一~二 | task | 旧实现移除 | 按 T-2 归属决定删除范围：删除本地任务 CRUD / 循环 / store 直写；若 batch/canary 判定保留本地，则保留对应 handler 并在 runbook 标注边界；保留 agent 适配层 | 执行 owner |
| 2026-12-23 ~ 12-28 | 三~一 | task | 移除后验证（3 个工作日：12-23、12-24、12-28） | build/test/lint/smoke；12-28 17:00 签收 | task owner |
| 2026-11-19 ~ 12-22 | 四~二 | device | 准备（与 auth/task 并行） | D-1/D-2/D-3、D-5/D-6；federation 下沉；device 冒烟；回退演练 | device owner |
| 2026-12-28 | 一 | device | Go/No-Go | 逐条核对 §3；不过则顺延 | 项目 owner |
| 2026-12-29 | 二 | device | **50/50 切换** | 确认 device 代理生效（若被列入 `OPSMESH_SERVICE_PROXY` 停用列表则移除并重启）；50% segment/租户验证；影子读对账 | device owner |
| 2026-12-29 ~ 12-31 | 二~四 | device | 观察窗口（3 个工作日） | provision/metrics/系统页；12-31 17:00 签收 | device owner |
| 2027-01-04 | 一 | device | **100% 切换** | 全部设备走 device-svc；netsec/federation 用例全绿 | device owner |
| 2027-01-04 ~ 01-08 | 一~五 | device | 观察窗口（5 个工作日） | 01-08 17:00 签收 | device owner |
| 2027-01-11 ~ 01-12 | 一~二 | device | 旧实现移除 | 删除本地 device handler / 循环 / store 直写；保留 agent 自举通道 | 执行 owner |
| 2027-01-13 ~ 01-15 | 三~五 | device | 移除后验证（3 个工作日） | 01-15 17:00 签收 | device owner |
| 2027-01-18 ~ 01-20 | 一~三 | 全部 | M5 文档收口 | `docs/tech-debt.md` TD-60 行（禁区，需 owner 明确开口，由协调线更新）、三份 runbook、`docs/architecture.md`、`docs/api-reference.md` 同步；关闭 TD-60 | 项目 owner |

> 日历约 8~9 周，略高于章程 6~8 周的估算。原因是三域严格串行观察窗；压缩只能靠并行观察窗，
> 那会违反 charter 的「不做跨域大爆炸切换」非目标。元旦（2027-01-01）不排任何切换动作。
>
> **auth 批次窗口的假设**：11-25~11-27 按「owner 批准较快批次节奏」排；若坚持 runbook 默认
> 「每批 ≥2 个工作日观察 + 累计 25% 后复评」，则 50% 阶段会超过 3 个工作日，后续日期整体顺延。
> 该选择必须在 11-24 修复门禁前由 owner 拍板（§11 第 1、2 项）。
>
> **11-25 是条件执行，不是默认执行**：只有 §3.1 九项全绿才写批次 A 名册；任一项未过即 HOLD，
> 占用 11-24 缓冲日修复，后续 auth 日期从门禁实际通过日顺延。**task / device 日期不变**，
> 但两者 Go/No-Go 必须重新确认共享环境（同一控制面、共享 MySQL/Redis、OTP/运维窗口）未受
> auth 门禁失败影响；共享环境不满足时同样顺延，不得带病切域。
>
> **与章程 M4 的差异**：charter:127 的 M4 写的是「删除三域 handler / 后台循环 / 本地 store 直写」；本排期按任务要求
> 拆成**逐域删除**——每域 100% 稳定满 5 个工作日后删除该域旧实现，其他域不受影响。
> 若 owner 坚持统一删除，则把三个「旧实现移除」窗口合并到 device 100% 之后（2027-01-11 起），
> M5 顺延约 1 周。逐域删除更保守（单域回滚面更小），但一旦删除该域就不再具备本地回退路径（charter:215）。

---

## 6. 阶段门禁（Gate Criteria）

### 6.1 统一阈值

| 指标 | 阈值 | 说明 |
|---|---|---|
| 一致性门禁 | `make td60-consistency` 退出 0；未登记 P0=0；无 stale baseline 未解释 | 门禁只覆盖路由/信封/状态词表；字段级契约另测 |
| 错误率 | 本域 5xx 比例 ≤ 切流前 7 天同域基线 + 0.5pp；auth 登录 5xx = 0；device provision/metrics 5xx ≤ 1% | 阈值来自 charter:155-164 |
| 延迟 p99 | ≤ 切流前 7 天同时段 p99 × 1.2；绝对上限 = 本域 SLO（切流前实测回填，本文不预设毫秒数） | 没有基线就不切 |
| 任务成功率 | auth 登录成功率 ≥ 基线 −0.5pp；task 创建/领取/结果上报成功率 ≥ 基线；device provision/metrics 成功率 ≥ 基线 | 按域取核心动作 |
| 稳定性 | 无 P0/P1 新增；无「失联任务持续增长」；task leader 恰好 1 个；batch 计数与任务实际状态一致 | charter:159-164 |
| 429 | auth 同 IP 429 不持续超过 5 分钟；若持续，按本批回退处理 | 同出口突发实测约 5 次（runbook:45-48） |

### 6.2 每阶段判据

| 阶段 | 一致性门禁 | 错误率 | p99 | 业务成功率 | 观察 / 签收 |
|---|---|---|---|---|---|
| 准备完成（Go/No-Go） | 本域未登记 P0=0；字段级 P0=0；契约测试红转绿 | 7 天基线已记录，无未解释尖峰 | 7 天基线已记录 | 本域基线已记录 | §3 逐条签字；未过则顺延 |
| 50/50 切换 | 同上；本域契约测试全绿 | 5xx ≤ 基线 +0.5pp；auth 登录 5xx=0 | ≤ 基线 ×1.2 | ≥ 基线（auth 允许 −0.5pp） | auth 2 个工作日或覆盖一次登录高峰（取长）；task/device 3 个工作日；每日 10:00/16:00 记录；结束签收 |
| 100% 切换 | 同上；baseline 无新增未登记项 | 同上 | 同上 | 同上；task leader=1、无失联增长；device netsec/federation 全绿 | 连续 5 个工作日；结束签收 |
| 删除前 | build/test/lint/smoke 全绿；删除清单已评审 | 100% 阶段指标持续达标 | 同上 | 同上 | 100% 稳定满 5 个工作日后才允许删除 |
| 删除后 | 全量 `go build ./...`、`go test ./... -count=1`、`golangci-lint run ./...`、`golangci-lint run -c .golangci.services.yml ./services/...` 全绿；404/405 回归=0 | 同 100% | 同 100% | 同 100% | 3 个工作日；结束签收；异常则按删除清单回滚分支恢复 |

---

## 7. 回退触发与精确动作

> **开关语义先读这一段**：`OPSMESH_SERVICE_PROXY` 是 svcproxy 规则的**停用列表**——列出的域不注册代理路由，
> 请求回落到 controlplane 本地 handler；空值 = 全部转发（`internal/controlplane/svcproxy/proxy.go:517-525`；
> `deploy/docker/docker-compose.prod.yml:514-517`）。**device 的回退 = 把域加入该列表并重启**。
> **auth 不要只靠该列表**：切流转发走 `forwardAuthToSvc` 的 `Rule.IsActive()`，只认
> `AUTH_SVC_PROXY_ENABLED`（`internal/controlplane/auth_cutover.go:240-244`）；auth 软回退是摘名册 /
> `cutover-unroute`，硬回退是清空名册 + 重启，必要时同时置 `AUTH_SVC_PROXY_ENABLED=false`。
> **task 不同**：`/api/v1/tasks` 主代理由 `TASK_SVC_PROXY_ENABLED` 独立控制
> （`internal/controlplane/server_lifecycle.go:700-715`）；把 `task` 加入 `OPSMESH_SERVICE_PROXY`
> 只停用 `/api/v1/task-svc/*` 别名，**不会**把主代理切回本地。task 回退必须置
> `TASK_SVC_PROXY_ENABLED=false` 并重启。`docs/td60-task-cutover-runbook.md` §2.3/§3 原文的
> 「设为空/含 task」表述与代码方向相反，已于 2026-11-18 批次59 按本节语义校正；执行前仍以本节和 P10 的 staging 验证为准（见 §11）。

| 域 | 触发条件（任一命中立即回退） | 软回退（首选） | 硬回退 | 回退后验证 | 目标时限 |
|---|---|---|---|---|---|
| auth | 登录 5xx >0 持续 5 分钟；名册用户报「用户不存在」；401/403 率 > 基线 2 倍或 5 分钟内 ≥10 次；同 IP 429 持续 >5 分钟；p99 > 基线 ×1.2 持续 10 分钟；门禁新增 P0 | `opsmesh cutover-unroute --users <本批>`（先摘名册、后吊销对侧会话，幂等） | 清空 `AUTH_CUTOVER_ROSTER_FILE` 并重启控制面；必要时同时 `AUTH_SVC_PROXY_ENABLED=false` | 本批用户本地口令登录 200；`GET /api/v1/admin/service-routing` 显示 auth 归属本地；路由日志无该批转发；通知本批用户重登一次 | 软回退 20~23s；硬回退 4~6s（runbook:129-135） |
| task | 本地新建任务失败率 > 基线 +0.5pp；task-svc 5xx ≥10 且持续 >5 分钟；leader 数量 ≠1；pending/running 超过 2× 超时仍在增长；batch 聚合计数与任务实际状态不一致（任一例）；agent 领取/上报失败持续 | `TASK_SVC_PROXY_ENABLED=false` + `TASK_SVC_OWNS_NEW_TASKS=false` 并重启（`/api/v1/tasks` 回到本地 handler；读路径不变） | 同上 + 恢复切换前的配置/版本；确认 `/api/v1/task-svc/*` 别名不再被使用 | 本地 `POST /api/v1/tasks` 201；leader 恰好 1 个；agent 领取/结果上报正常；batch 计数按任务状态重新聚合 | ≤5 分钟（charter:95-96） |
| device | provision/metrics 5xx >1%；系统页 404/空白；D-1/D-2/D-3 契约测试失败；netsec SSRF/私网校验或 federation 验签用例失败；p99 > 基线 ×1.2 持续 10 分钟 | 无独立增量开关；直接执行硬回退 | 把 `device` 加入 `OPSMESH_SERVICE_PROXY` 并重启（停用 device 代理） | 设备 CRUD 本地 200；provision/metrics 正常；netsec/federation 用例全绿 | ≤5 分钟（charter:96） |

> 回退不删除对侧已写入的数据（共享 MySQL），也不回滚已完成的账号迁移；重切幂等继续
> （charter:175）。回退后 24h 内不重切同批。

---

## 8. 角色与签收

| 角色 | 职责 | 人选（待填） |
|---|---|---|
| 项目 owner | 批准批次名单/观察窗/回退；最终签收 | 待填 |
| 域 owner（auth / task / device） | 本域 Go/No-Go、切换执行、观察结论 | 待填 ×3 |
| 执行 owner（开发） | P0/契约/适配层/下沉实现与测试 | 待填 |
| 观察员（SRE） | 看板、每小时记录、触发回退建议 | 待填 |
| 回退执行人 | 执行开关、重启、回退命令与验证 | 待填 |
| 一致性门禁 owner | 跑 gate、维护 baseline、审 stale 条目 | 待填 |
| 变更记录员 | 填写 §10 模板、同步 CHANGELOG/docs | 待填 |

**签收规则**：每个观察窗结束当天 17:00 前，域 owner + 观察员 + 回退执行人三方签字；
缺任一方则视为「未签收」，不得进入下一阶段。

---

## 9. 风险登记（Top 5）

| # | 风险 | 触发信号 | 缓解 | 回退 |
|---|---|---|---|---|
| R1 | device 字段契约 P0 导致切流即空白/404 | `GET /devices` 返回 `id/status/group/name`；系统页 404 | 先做 DTO 映射 + `/system`；契约测试断言键集合与 `internal/proto.DeviceInfo` 完全一致；50% 观察窗 | 把 `device` 加入 `OPSMESH_SERVICE_PROXY` 并重启 |
| R2 | task agent gRPC 适配层缺失，任务建在 task-svc 但 agent 从 controlplane store 领取，任务永远领不到 | 新建任务成功但长期 pending；agent claim 空转 | 准备阶段实现 `rpcAgentService`（ClaimTask/ReportResult 幂等键）；agent E2E 必须先绿；影子读对账 | `TASK_SVC_PROXY_ENABLED=false` + `TASK_SVC_OWNS_NEW_TASKS=false` 并重启；必要时恢复切换前版本 |
| R3 | auth 名册用户被锁死（号未搬/OTP 未送达/admin 未改密） | 登录 401「用户不存在」；用户报密码错误；迁移 job fail-closed | 搬号 job 原子序：搬完才写名册；OTP 与通知同批；`admin` 永不入册；切换前 dry-run | `cutover-unroute` 摘名册；必要时对侧重置口令 |
| R4 | batch/canary 归属未定 + 聚合计数口径不同（T-2/T-4） | 两侧批次视图不一致；计数与实际状态不符 | 12-04 前拍板「task-svc 实现」或「明确本地」；切换前排空在飞批次；切换后单侧读取；契约测试钉住状态词表 | `TASK_SVC_PROXY_ENABLED=false` + `TASK_SVC_OWNS_NEW_TASKS=false` 并重启；按任务状态重新聚合 |
| R5 | 同出口 IP 429 误伤 auth 批次用户 | 一批用户集中 429，文案 `too many requests, slow down` | 同出口用户错峰分批；每批 ≤5% 租户且 ≤3 个租户；观察 429；确需集中切换时由 owner 批准临时提高 burst | 暂停该批；已切用户保留，不整批回退 |

> 未列入 Top 5 但必须盯的：一致性 baseline 掩盖新 P0（每次 Go/No-Go 审 stale 条目）、
> 共享库表形状漂移（M3 前做全表对账）、观察窗不足导致问题晚发现（每阶段至少覆盖一次业务高峰）。

---

## 10. 第一周清单（2026-11-19 起可立即执行）

| 日期 | 星期 | 检查项 | 命令 / 证据 | 责任 | 完成 |
|---|---|---|---|---|---|
| 11-19 | 四 | 跑一致性门禁并归档 | `make td60-consistency`；`python3 scripts/td60-consistency.py --baseline scripts/td60-consistency-baseline.json --json > /tmp/td60-gate.json` | 门禁 owner | ☐ |
| 11-19 | 四 | 验证门禁自测契约 | `TD60_TEST_FAIL=1 make td60-consistency` 必须退出 1 | 门禁 owner | ☐ |
| 11-19 | 四 | 冻结指标基线 | 导出切流前 7 个自然日基线并冻结（5xx、p99、登录/任务/provision 成功率、429、leader、stale task），此后每日更新 | 观察员 | ☐ |
| 11-19 | 四 | 填实 §8 角色 | 角色表无「待填」 | 项目 owner | ☐ |
| 11-20 | 五 | 核对开关方向 | staging：svcproxy 域加入 `OPSMESH_SERVICE_PROXY` 后确认回落本地、清空后确认转发；auth 另测 `AUTH_SVC_PROXY_ENABLED=false/true`；task 另测 `TASK_SVC_PROXY_ENABLED=false/true` 的 `/api/v1/tasks` 走向 | 回退执行人 | ☐ |
| 11-20 | 五 | auth 运维前置 | `AUTH_SVC_HTTP_ENABLED=true`、`AUTH_SVC_URL`、对侧 admin 改密、名册文件路径、OTP 交付清单 | 运维 | ☐ |
| 11-20 | 五 | auth 回退演练 | `bash deploy/scripts/smoke-cutover.sh` 第 10 节；记录软/硬回退秒数 | 回退执行人 | ☐ |
| 11-20 ~ 11-23 | 五~一 | auth 缺陷修复 G1-G9 | 按 §3.1 逐项修复并留证；11-24 门禁前完成 | auth-svc 开发 + 部署 owner | ☐ |
| 11-23 | 一 | device P0 红测试 | 为 D-1/D-2/D-3 写契约测试，当前必须红；分配给执行 owner | 开发 | ☐ |
| 11-23 | 一 | task 适配层设计评审 | `rpcAgentService` 接口、幂等键、claim/report 语义、影子读对账方案 | 开发 + task owner | ☐ |
| 11-23 | 一 | batch/canary 归属草案 | 二选一：task-svc 实现 / 明确本地；影响 T-2/T-4/T-8 | task owner | ☐ |
| 11-24 | 二 | 真栈冒烟 | MySQL 8 + Redis 7；`smoke-cutover.sh` + `smoke-task-cutover.sh`；记录日志 | 执行 owner | ☐ |
| 11-24 | 二 | device 冒烟缺口登记 | 确认 device 等价冒烟脚本尚未入库（审计 §7 第 5 条），排入准备阶段 | 执行 owner | ☐ |
| 11-24 | 二 | auth 修复门禁 + Go/No-Go | §3.1 G1-G9 全绿 + §3 P1-P7、P10-P11 逐条签字；真栈冒烟；未过则 11-25 不写名册，用 11-24 缓冲修缺陷并 HOLD | 项目 owner | ☐ |
| 11-25 | 三 | auth 名册批次 A（二选一，条件执行） | 仅 11-24 门禁全绿：1 个非 admin 用户写入名册；10:00 前完成；14:00 前首轮抽验；观察窗开始；门禁未过则 HOLD | auth owner | ☐ |

---

## 11. 执行前必须校正的文档口径

| 项 | 事实 | 影响 | 处置 |
|---|---|---|---|
| `OPSMESH_SERVICE_PROXY` 方向 | 代码与 compose 注释一致：列表 = 停用 svcproxy 规则、回落本地；默认空 = 全部转发（`internal/controlplane/svcproxy/proxy.go:517-525`；`deploy/docker/docker-compose.prod.yml:514-517`）。task 的 `/api/v1/tasks` 主代理另由 `TASK_SVC_PROXY_ENABLED` 控制（`internal/controlplane/server_lifecycle.go:700-715`）；auth 切流转发另由 `AUTH_SVC_PROXY_ENABLED` 控制（`internal/controlplane/auth_cutover.go:240-244`） | `docs/td60-task-cutover-runbook.md:107` 的「设为空（或不含 task），全部转发回单体本地实现」与代码相反；§2.3 的「以 `OPSMESH_SERVICE_PROXY=task` 重启」实际只停用 `/api/v1/task-svc/*` 别名，**不会**停用 `/api/v1/tasks` 主代理 | 切换前按 §7 校正 runbook（§2.3/§3 已于 2026-11-18 批次59 校正）；task 回退必须置 `TASK_SVC_PROXY_ENABLED=false`；auth 回退用名册/`AUTH_SVC_PROXY_ENABLED`；P10 staging 验证一次 |
| task leader 切换是否已完成 | 已完成：controlplane 通过 gRPC 调 task-svc `LeaderElectorService`（`internal/controlplane/leader_elector_client.go`） | 排期不再把 leader 迁移列为开发工作；但仍需在 task Go/No-Go 验证 leader=1 | 只做验证，不重复开发 |
| agent gRPC 适配层 | 未完成：`PullTasks` / `ReportResult` 仍直写 controlplane store（`internal/controlplane/grpc/grpc.go:564-590`） | 任务建在 task-svc 后 agent 领不到 | 列为 task 硬前置 P8/R2；未完成不切 task |
| device federation | 未下沉：device-svc 无 federation 等价物（charter:58,116） | 切 device 后联邦验签可能失效 | 准备阶段完成下沉；未完成不切 device |
| 一致性门禁视野 | 只做路由/信封/状态词表静态比对（`scripts/td60-consistency.py:1-35`） | D-1/D-2 字段级 P0 不会让门禁变红 | 字段级 P0 用契约测试单独钉住（P2） |

**仍需 owner 拍板的项**（charter §7）：

1. auth 批次名单与顺序（哪些租户/用户先切，最大客户是否单独成批）——截止 11-24。
2. 观察窗长度（2 个工作日还是覆盖一次登录高峰，取长）——截止 11-24。
3. 一次性口令交付渠道（谁发、何时发、如何与重登通知同步）——截止 11-24。
4. 是否接受 batch 聚合计数差异（接受则写进 runbook，不接受则先统一口径）——截止 12-04。
5. task leader 切换的生产部署形态（实现已完成：controlplane 走 gRPC `LeaderElectorService`；剩余是环境变量 / K8s Lease 参数的确认）——截止 12-07。

---

## 12. 切换记录模板（每次必填）

```text
切换单 YYYY-MM-DD / 域 / 阶段（50-50 | 100 | 删除 | 删除后验证）
前置证据：gate JSON / 冒烟日志 / 回退演练耗时 / 字段级 P0 清零记录
开关状态：OPSMESH_SERVICE_PROXY=()  AUTH_SVC_PROXY_ENABLED=()
          AUTH_SVC_OWNS_NEW_ACCOUNTS=()  TASK_SVC_PROXY_ENABLED=()
          TASK_SVC_OWNS_NEW_TASKS=()  AUTH_CUTOVER_ROSTER_FILE=()
执行：重启次数 / 首批用户或租户 / 首轮抽验时间
观察窗：起止 / 每小时指标摘要 / 异常与处置
结论：继续 / 回退（原因）
签收：域 owner / 观察员 / 回退执行人（三方）
```

---

## 13. 关联文档

- `docs/projects/TD-60-domain-convergence-charter.md` —— 上位立项书、DoD、风险与全局回滚
- `docs/td60-consistency-2026-11-17.md` —— P0/P1/P2 计数、剩余项与字段级差异
- `docs/td60-auth-cutover-runbook.md` / `docs/td60-task-cutover-runbook.md` —— 逐域执行手册
- `docs/td60-cutover-decisions.md` —— 名册 / 搬号 / 重登 / 告知四项决策
- `docs/api-versioning-policy.md` —— 切流期间的 API 兼容边界（默认形态冻结）
