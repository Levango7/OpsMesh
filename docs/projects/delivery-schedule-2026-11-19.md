# 交付日程（2026-11-19 → 2027-01-20）：TD-60 三域切流 + ABAC M4 放量

> **状态：执行中（2026-11-19 起）**
> **适用范围**：TD-60 阶段 2 的 auth → task → device 逐域切流，与设备侧 ABAC M4 的
> shadow → 选择器 enforce 阶梯，共用一条逐日时间线。
> **本文只排期**，不代替 runbook 与 soak 计划：切换动作以
> `docs/td60-auth-cutover-runbook.md`、`docs/td60-task-cutover-runbook.md`、
> `docs/projects/abac-m4-soak-plan.md` 和 `GET /api/v1/admin/service-routing`
> 的运行时取证为准。**排期不是完工声明**。
>
> 上位文档：`docs/projects/TD-60-cutover-schedule.md`（切流排期）、
> `docs/projects/abac-m4-soak-plan.md`（M4 soak）、`docs/api-versioning-policy.md`（v1 默认形态冻结）。
> 事实基线：`docs/td60-consistency-2026-11-17.md`、`docs/race-sweep-2026-11-18.md`、
> `docs/abac-m1-m3-dogfood-2026-11-18.md`。

---

## 1. 目标与假设

### 1.1 目标

1. 每天开工前不用重新决策「今天做什么」：日历表一天一行，主块/次块/闸门/回退杠杆齐全。
2. 任何时刻只有一个**整域**高风险变更在飞；ABAC 阶梯不与 TD-60 的整域动作争用同一时间盒。
3. 每个闸门有可归档的证据；每个回退有精确到环境变量的动作与 ≤5 分钟目标。
4. 空档日不空转：用固定 backlog 填满等待观察窗的时间，不引入新的高风险变更。

### 1.2 假设（A1–A8）

| # | 假设 | 说明 |
|---|---|---|
| A1 | **工作日 = 每个自然日** | 每天两个工作块：**09:30–12:00**、**14:00–17:30**。周末为轻值班：只做日检 + soak 日报，不排变更；若闸门要求动作（回退未完成、告警处置），闸门优先于轻值班。 |
| A2 | **一人执行 + owner 签字** | 执行者为单一 agent 线；所有 Go/No-Go、观察窗收口、100% 切换由 owner 或域 owner 签字。签字人不在场时按「未签收」处理，不进入下一阶段。 |
| A3 | **TD-60 日期沿用已批准排期** | auth：11-25 50/50、11-30 100%、12-07/08 旧实现移除；task：12-09 50/50、12-14 100%、12-21/22 旧实现移除；device：12-29 50/50、01-04 100%、01-11/12 旧实现移除；M5 收口 01-18/20。另保留源排期的 task Go/No-Go 12-07、device Go/No-Go 12-28 与各域删除后 3 个工作日验证。 |
| A4 | **M4 soak 起始与放量窗** | 11-19 开 shadow（staging 12 台 + prod-edge-a 40 台）；shadow **延长到 12-04**，使 enforce 阶梯不与 auth 100% 观察窗（11-30–12-04）重叠；enforce 阶梯 **12-07 → 12-11**：1 台 → 5%（2 台）→ 25%（10 台）→ 50%（20 台）→ 100%（40 台），全部在 `segment=prod-edge-a` 选择器内，逐级受 soak 门禁约束。 |
| A5 | **48h 内不并行两个高风险变更** | 本文件把该规则解释为**整域变更之间**的约束；ABAC 阶梯属设备级配置（≤40 台、≤5 分钟 kill switch），按低爆炸半径处理。该解释需在 S2（12-04）由 owner 确认，见 §6.4。 |
| A6 | **时区与截数点** | 所有日期/时刻为本地时间（UTC+8）。soak 计划规定日报固定在 **09:00 UTC = 17:00 本地**，即第二块结束前 30 分钟；闸门签字默认在 17:00 前完成。 |
| A7 | **节假日** | 2027-01-01（元旦）不排任何变更与验证，只保留告警值守（源排期 §5 明确）。其余非法定假日按 A1 处理。 |
| A8 | **校准基准不是承诺** | 本文写就时（2026-11-18）一致性门禁实跑 `p0=0 p1=0 p2=0 known=31 stale=0`，退出 0。该数字只用于说明门禁当前是绿的，Go/No-Go 当天必须重新冻结。 |

### 1.3 时间盒与产能

- 表内每日 6.0 小时（2.5 + 3.5）；扣除日检 0.5 小时后约 **5.5 小时**可用于施工。
- 闸门日（Go/No-Go、100% 切换、删除）实际施工时间按 **3 小时**估算，其余时间用于观察与记录。
- 全量 `-race` 分片实测墙钟：controlplane 692s、store 594s、其余 internal 约 1560s（并行）、
  pkg/agent/core 合计约 2 分钟（`docs/race-sweep-2026-11-18.md` §Coverage summary）。因此周检必须分片，
  不能塞进一个块。

### 1.4 阶段编号（S / E），消除 soak 计划 D-day 的歧义

soak 计划用相对日（D0、D8、D9–D13）。直接以 D0=11-19 套算会得到 D8=11-27、D13=12-02，
与「shadow 延到 12-04」冲突（差异记录见 §8）。本文件改用两套编号，日历只引用它们：

| 编号 | 含义 | 日期 |
|---|---|---|
| **S0** | shadow 启动（= 计划 D0） | 11-19 |
| **S7** | 计划 D7：kill-switch 回滚演练日（演练本身不放量） | 11-26 |
| **S0–S15** | shadow 观察期（S15 = 计划的 D8 评审闸门） | 11-19 → 12-04 |
| **S16–S17** | 维持 shadow，无阶段推进 | 12-05 → 12-06 |
| **E1** | 1 台 enforce（特例，0%→1 台） | 12-07 |
| **E2** | 5%（2 台 enforce + 38 shadow） | 12-08 |
| **E3** | 25%（10 台） | 12-09 |
| **E4** | 50%（20 台） | 12-10 |
| **E5** | 100%（40 台，owner 签字） | 12-11 |

> S 编号 = 11-19 起第 n 个自然日（S0=11-19、S7=11-26、S15=12-04）。
> E1–E5 每级至少稳定 24h 且不得跳级；因为 12-07→12-11 是连续 5 天，阶梯**没有内部缓冲**，
> 任一级 HOLD 都会把后续梯级顺延（处理见 §6.1、§6.4）。

---

## 2. 每日固定动作（15–30 分钟）

| # | 时点 | 动作 | 命令 / 判据 | 归档 |
|---|---|---|---|---|
| D-1 | 开工 | 同步与看板 | `git fetch origin` + `git rebase origin/main`；本地有未提交改动先提交，**不 reset 他人改动**；`git status --short` 应为空 | 无 |
| D-2 | 开工 | TD-60 一致性门禁 | `python scripts/td60-consistency.py --baseline scripts/td60-consistency-baseline.json --json`；退出 0，`counts.p0==0`，`stale_baseline` 为空（2026-11-18 实跑：p0=0 p1=0 p2=0 known=31 stale=0）。`make td60-consistency` 是同一门禁的别名 | 当日 JSON 贴入变更单/日报附件 |
| D-3 | 开工 | soak 日报（仅 11-19 → 12-11 活跃） | `python scripts/abac/soak-report.py --dsn "$OPSMESH_SOAK_DSN" --days 1 --out docs/abac-soak/<date>-S<n>.md`；闸门日加跑 `--since 2026-11-19` 累计版；日报字段按 soak 计划 §6.1，17:00 本地截数 | 日报文件 + 签字行 |
| D-4 | 开工 | CI / PR 巡检 | `gh run list --limit 10`；红的第一优先修复，当日不推进任何闸门 | 无 |
| D-5 | 收工 | 全量测试 + 提交 | `go build ./...`；`go test ./... -count=1`；commit + push；`docs/CLAIMS.md` 追加 `[DONE] ... commit=<sha>`（仓库规则，未提交不得过夜） | 提交 sha |

> 门禁日（Go/No-Go、100%/删除切换）D-5 允许延后到次日开工，但必须当天把结果与回退状态写入变更单。

---

## 3. 每周固定动作

| 频率 | 动作 | 命令 | 判据 |
|---|---|---|---|
| 每周（分 4 片） | 全量 `-race` 扫描 | 下方命令块 | 0 `DATA RACE`、0 `--- FAIL`；每片日志归档，周报引用最近一次完整结果 |
| 每周 | gosec | 与 CI 同版同参（见下） | 0 findings；与 `ci.yml` 的 gosec job 结果一致 |
| 每周 | loadgen 基线 | 50 VU × 30s，沿用同一脚本与同一端点集；工具版本、命令、端点清单写进周报 | p95 ≤ 基线 ×1.2；5xx ≤ 基线 +0.5pp；429 有记录 |
| 每周 | 文档漂移 | `DELIVERY.md` 迁移数/测试数/端点计数、API reference、`CHANGELOG.md`（**不动顶部版本引用块**，禁区） | 数字与当日实测一致；差异当日修 |

```powershell
$env:PATH = "C:\Users\winge\AppData\Local\msys64\ucrt64\bin;" + $env:PATH
$env:CGO_ENABLED = '1'

# 分片 1（约 2 分钟）
go test -race ./pkg/... ./internal/agent/... ./internal/cmdb/... ./internal/k8slifecycle/... ./internal/ai/... ./internal/cost/... -count=1 -timeout 25m

# 分片 2（约 10 分钟）
go test -race ./internal/store/... -count=1 -timeout 25m

# 分片 3（约 12 分钟）
go test -race ./internal/controlplane/ -count=1 -timeout 30m

# 分片 4（约 26 分钟）：66 包清单见 docs/race-sweep-2026-11-18.md:51-53；
# 清单按当周 go list 结果补充新包后再执行
go test -race <分片 4 包清单> -count=1 -timeout 30m -p 2
```

```powershell
go install github.com/securego/gosec/v2/cmd/gosec@v2.25.0
& "$(go env GOPATH)\bin\gosec" -quiet `
  -exclude=G103,G101,G104,G106,G115,G118,G124,G201,G202,G204,G301,G304,G404,G704,G705 `
  ./...
```

> 周一若撞上切换/Go/No-Go 日（11-30、12-07、12-14、12-21、12-28、01-04、01-11），
> 周检顺延到本周第一个非闸门日；顺延不改变周检内容。

---

## 4. 日历表（2026-11-19 → 2027-01-20，每天一行）

> 图例：`日检` = §2 全套；`S<n>`/`E<n>` = §1.4 阶段编号；`B1–B5` = §5 backlog；
> `P5/P6/P7/P10` = `docs/projects/TD-60-cutover-schedule.md` §3 前置条件；
> 「缓冲」= 当日不排 backlog 新工作，只做当日闸门/观察/收口（定义见 §6.2）。

| 日期 | 星期 | 主块 09:30–12:00 | 次块 14:00–17:30 | 闸门 / 签字 | 回退杠杆 |
|---|---|---|---|---|---|
| 2026-11-19 | 四 | TD-60 门禁基线 + 自测（`TD60_TEST_FAIL=1` 必红）+ 从既有看板导出并冻结切流前 7 天基线（P5）+ 角色表填实（P6） | M4 S0：D0 预检 + 冻结 12+40 清单 + 双池开 shadow + 记录 24h 基线 | 项目 owner：准备开始 + 角色无待填 | ABAC kill-switch → `mode=off` |
| 2026-11-20 | 五 | P10 三套开关方向 staging 验证 + auth 运维前置（P7） | auth 回退演练（`smoke-cutover.sh` §10）+ S1 日报 | 回退执行人：开关方向已验证 | `cutover-unroute`；清名册 + 重启 |
| 2026-11-21 | 六 | 日检 + S2 日报（17:00） | 无（周末轻值班） | 无 | 无（只读） |
| 2026-11-22 | 日 | 日检 + S3 日报 | 无 | 无 | 无 |
| 2026-11-23 | 一 | 周检：`-race` 分片 1+2 + gosec | device P0 契约测试先红（D-1/D-2/D-3）+ task 适配层评审 + batch/canary 归属草案 | 周检证据归档 | 不适用 |
| 2026-11-24 | 二 | **缓冲**；真栈冒烟（MySQL 8 + Redis 7）两脚本 + device 冒烟缺口登记 | auth 批次名单/观察窗/OTP 三项拍板（截止今日）+ device 冒烟排期 | **S1 auth Go/No-Go**（P1–P7、P10–P11） | 不切；门禁不过顺延 |
| 2026-11-25 | 三 | **auth 50/50**：10:00 前写名册，14:00 前首轮抽验 | 每小时记录起 + S6 日报 | auth owner：首批抽验通过 | 软回退 20–23s；硬回退 4–6s |
| 2026-11-26 | 四 | auth 观察（每小时）+ 第 2 批（≤5% 且 ≤3 租户） | M4 S7：kill-switch 演练（staging 12 台，记 TTR ≤5min）+ B1（tasks legacy 契约） | 回退执行人：TTR 记录 | ABAC kill-switch；auth 名册 |
| 2026-11-27 | 五 | auth 50/50 窗收口（继续/回退） | B4 文档漂移 + 周检归档 + S8 日报 | 域内三方签收（auth 50/50） | auth 名册 |
| 2026-11-28 | 六 | 日检 + S9 日报 | 无 | 无 | 无 |
| 2026-11-29 | 日 | 日检 + S10 日报 | 无 | 无 | 无 |
| 2026-11-30 | 一 | **auth 100%**：剩余用户入册 + `AUTH_SVC_OWNS_NEW_ACCOUNTS=true` | 每小时记录；周检顺延 | auth owner：100% 执行记录 | 清名册 + 重启；`AUTH_SVC_PROXY_ENABLED=false` |
| 2026-12-01 | 二 | auth 100% 观察 + 周检 `-race` 分片 1/4 + loadgen | B3 MySQL 65 条（临时库 + DSN） | 无 | auth 名册 |
| 2026-12-02 | 三 | auth 100% 观察 + `-race` 分片 2/4（store） | B2 Playwright e2e（独立 compose project） | 无 | auth 名册 |
| 2026-12-03 | 四 | auth 100% 观察 + `-race` 分片 3/4（controlplane）+ gosec | B1（users/tenants 顶层键契约） | 无 | auth 名册 |
| 2026-12-04 | 五 | auth 100% 5 天窗收口（登录 5xx=0、429、p99 归档） | **S2 双签**：auth 窗收口 + M4 D8 升门（S0–S15 日报、fixture=0、覆盖率 ≥95%） | **S2**（项目 owner + auth owner + ABAC owner） | auth 名册；ABAC 留 shadow |
| 2026-12-05 | 六 | 日检 + S16 日报 | 无 | 无 | 无 |
| 2026-12-06 | 日 | 日检 + S17 日报 | 无 | 无 | 无 |
| 2026-12-07 | 一 | **auth 旧实现移除 day1**（登录/会话/store 直写/后台循环；保留 cutover router、loginGuard） | **S3 task Go/No-Go**；通过且 15:00 后才做 ABAC E1（1 台） | **S3** + auth 删除单评审 | auth 删除清单回滚分支；ABAC 1 台 kill switch |
| 2026-12-08 | 二 | **缓冲**；auth 移除 day2 收尾 + build/test/lint/smoke | ABAC E2（5% = 2 台，2 enforce + 38 shadow） | auth 删除完成记录 | auth 分支恢复；ABAC 2 台 kill switch |
| 2026-12-09 | 三 | **task 50/50**：`TASK_SVC_PROXY_ENABLED=true`，验证 leader=1，50% 租户/agent 池 | auth 移除后验证 day1（404/405 回归）；ABAC E3（25% = 10 台）仅在 task 首轮抽验通过后 | task owner：切换记录 | `TASK_SVC_PROXY_ENABLED=false` + `TASK_SVC_OWNS_NEW_TASKS=false` 重启 |
| 2026-12-10 | 四 | task 观察（leader/claim/report/batch）+ auth 验证 day2 + `-race` 分片 4/4 | ABAC E4（50% = 20 台）+ soak 日报 | 无 | task 开关；ABAC 20 台 kill switch |
| 2026-12-11 | 五 | task 50/50 窗收口 + auth 移除后验证 day3 收口 | **S4**：追加 ABAC E5（40/40 = 100%）；三项证据分别归档 | **S4**（task owner + auth owner + ABAC owner） | task 开关；ABAC 全 selector kill switch |
| 2026-12-12 | 六 | 日检 | 无 | 无 | 无 |
| 2026-12-13 | 日 | 日检 | 无 | 无 | 无 |
| 2026-12-14 | 一 | **task 100%**：`TASK_SVC_OWNS_NEW_TASKS=true`，新任务全归 task-svc，agent 适配层在线 | 观察起（每小时）；周检顺延 | task owner：100% 执行记录 | task 开关 |
| 2026-12-15 | 二 | task 100% 观察 + `-race` 分片 1/4 | B5 P2 清理（T-6 状态词表 / T-7 死代码） | 无 | task 开关 |
| 2026-12-16 | 三 | task 观察 + `-race` 分片 2/4 + loadgen | B2 e2e 第二轮（含 task-svc 链路） | 无 | task 开关 |
| 2026-12-17 | 四 | task 观察 + `-race` 分片 3/4 | B1（batch 状态词表 T-4 契约） | 无 | task 开关 |
| 2026-12-18 | 五 | **缓冲**；task 100% 5 天窗收口（无失联增长、leader=1、batch 一致） | 域内签收 + gosec + 文档漂移 | 域内三方签收（task 100%） | task 开关 |
| 2026-12-19 | 六 | 日检 | 无 | 无 | 无 |
| 2026-12-20 | 日 | 日检 | 无 | 无 | 无 |
| 2026-12-21 | 一 | **task 旧实现移除 day1**（按 T-2 归属定删除范围；保留 agent 适配层） | 删除清单评审 + `go build ./...` | 执行 owner：删除清单评审 | 删除前回滚分支（删除后仅剩配置回退） |
| 2026-12-22 | 二 | task 移除 day2 + build/test/lint | B3 MySQL 65 条（第二遍：迁移/审计链） | 无 | 分支恢复 |
| 2026-12-23 | 三 | task 移除后验证 day1（404/405 = 0、smoke） | `-race` 分片 4/4 | 无 | 分支恢复 |
| 2026-12-24 | 四 | task 移除后验证 day2 | B4 文档漂移 + ABAC 收尾归档（默认 off、kill switch 保留） | 无 | 分支恢复 |
| 2026-12-25 | 五 | task 移除后验证（源排期跳过的普通工作日，见 §8.1） | B5 P2（D-7/D-8 语义统一） | 无 | 分支恢复 |
| 2026-12-26 | 六 | 日检 | 无 | 无 | 无 |
| 2026-12-27 | 日 | 日检 | 无 | 无 | 无 |
| 2026-12-28 | 一 | **缓冲**；task 移除验证 day3 收口 | **S5 device Go/No-Go**（D-1/D-2/D-3 绿、federation、冒烟、回退演练） | **S5**（执行 owner + 项目 owner） | 不切；task 分支恢复 |
| 2026-12-29 | 二 | **device 50/50**：确认代理生效（必要时移出 `OPSMESH_SERVICE_PROXY` 并重启）+ 50% segment/租户 | 观察起（每小时）+ 影子读对账 | device owner：切换记录 | `device` 加入停用列表 + 重启 ≤5min |
| 2026-12-30 | 三 | device 观察 + 周检 `-race` 分片 1/4 | B2 e2e（系统页/设备详情/provision） | 无 | device 停用列表 |
| 2026-12-31 | 四 | device 50/50 窗收口（provision/metrics/系统页） | 域内签收 + loadgen + gosec | 域内三方签收（device 50/50） | device 停用列表 |
| 2027-01-01 | 五 | 元旦：不排变更与验证；仅告警值守 | — | 无 | 无 |
| 2027-01-02 | 六 | 日检 | 无 | 无 | 无 |
| 2027-01-03 | 日 | 日检 | 无 | 无 | 无 |
| 2027-01-04 | 一 | **device 100%**：全部设备走 device-svc；netsec/federation 用例全绿 | 观察起（每小时）；周检顺延 | device owner：100% 执行记录 | device 停用列表 |
| 2027-01-05 | 二 | device 观察 + `-race` 分片 3/4 | B5 P2（D-7/D-8 收尾） | 无 | device 停用列表 |
| 2027-01-06 | 三 | device 观察 + `-race` 分片 4/4 + loadgen | B1（device 分页 D-5 / agents 字段 D-6 契约） | 无 | device 停用列表 |
| 2027-01-07 | 四 | device 观察 | B2 e2e 收尾 | 无 | device 停用列表 |
| 2027-01-08 | 五 | **缓冲**；device 100% 5 天窗收口（01-08 17:00） | **S6 工程签**：证据汇总（gate JSON、soak 报表、race 报告、回退演练） | **S6**（device owner + 项目 owner） | device 停用列表 |
| 2027-01-09 | 六 | 日检 | 无 | 无 | 无 |
| 2027-01-10 | 日 | 日检 | 无 | 无 | 无 |
| 2027-01-11 | 一 | **device 旧实现移除 day1**（本地 handler/循环/store 直写；保留 agent 自举通道） | 删除清单评审 | 执行 owner | 删除前分支（之后仅配置回退） |
| 2027-01-12 | 二 | device 移除 day2 + build/test/lint | B3 MySQL 65 条（第三遍回归） | 无 | 分支恢复 |
| 2027-01-13 | 三 | device 移除后验证 day1 | `-race` 分片 1/4 | 无 | 分支恢复 |
| 2027-01-14 | 四 | device 移除后验证 day2 | B4 `DELIVERY.md` / API reference 数字 | 无 | 分支恢复 |
| 2027-01-15 | 五 | device 移除后验证 day3 收口（17:00） | 域内签收 + M5 资料准备（TD-60 行需 owner 明确开口，禁区） | 域内三方签收（device 删除后） | 分支恢复 |
| 2027-01-16 | 六 | 日检 | 无 | 无 | 无 |
| 2027-01-17 | 日 | 日检 | 无 | 无 | 无 |
| 2027-01-18 | 一 | M5 文档 day1：三份 runbook 同步 + TD-60 行（需 owner 明确开口） | 全量 build/test/lint/smoke + gosec 终验 | 无 | 不适用（文档） |
| 2027-01-19 | 二 | M5 文档 day2：`architecture.md`、`api-reference.md`、项目索引状态 | 证据汇总（门禁 JSON、soak、race、回退演练） | 无 | 不适用 |
| 2027-01-20 | 三 | M5 终验：关闭 TD-60 + 交付报告 | 复盘（不接新工作） | **S6 终验签**（项目 owner） | 不适用 |

---

## 5. 空档日 backlog（等观察窗时按序取用）

> 取用规则：只填**次块**或缓冲日的空余；不挤占当日闸门准备。B1/B2/B4 可随时插入，
> B3 需要临时 MySQL 实例，B5 需要 owner 决策（修复或书面接受）。

| ID | 事项 | 依据 / 起点 | 完成判据 | 预排日 |
|---|---|---|---|---|
| **B1** | v1 默认形态契约测试（tasks / agents / users / tenants） | `docs/api-versioning-policy.md` §4 表缺口：tasks 裸数组未钉住、agents 无默认/信封形状测试、users/tenants 顶层键未单独断言 | 新增 `TestTasksLegacyShape` 等；默认形态顶层键**多加/少加都判红**，且不含 `data/total/count`；opt-in 五键齐全、空集 `[]`、非法参数 `400 {"error":"invalid page/pageSize"}`；`go test ./internal/controlplane/ -run Legacy` 与 `-run Envelope` 均绿 | 11-26、12-03、12-17、01-06 |
| **B2** | Playwright e2e（真实 compose 栈：mysql+redis+controlplane+agent） | 现有门禁不覆盖浏览器侧；系统页/详情页是 device 切流的高风险面 | 独立 compose project，不占用观察栈端口；登录 → 设备列表/详情/系统页 → 任务创建/领取/上报 → batch 全绿；产物与 trace 归档 | 12-02、12-16、12-30、01-07 |
| **B3** | MySQL 门控测试（65 条）带 DSN | `docs/race-sweep-2026-11-18.md` §Skips：`OPSMESH_TEST_MYSQL_DSN` 未设时真实库用例全部跳过（迁移、审计链、租户隔离、并发领取代） | 临时库上 65 条全部执行、0 FAIL；`go test -json` 归档；不指向生产库 | 12-01、12-22、01-12 |
| **B4** | 文档漂移：`DELIVERY.md` / API reference 数字 | 迁移数、测试数、端点计数来自批次快照，易落后 | 与当日实测一致（迁移数、`go test` 计数、端点表）；`CHANGELOG.md` 有本批条目但**不动顶部版本引用块**（禁区） | 11-27、12-18、12-24、01-14 |
| **B5** | 剩余 P1/P2（不在切流范围） | `docs/td60-consistency-2026-11-17.md` §6：P1 十条已全部并入三域准备项（A-1/A-2/A-3、T-2/T-3/T-4/T-5/T-8、D-5/D-6），范围外只剩 8 条 P2 | A-4/A-5/A-6/A-7、T-6/T-7、D-7/D-8 每条「修复 + 测试」或「owner 书面接受」二选一，记入决策文档；不接受口头接受 | 12-15、12-25、01-05 |

---

## 6. 关键风险与缓冲

### 6.1 闸门失败：冻结该域 → 精确回退 → 次日只做根因

**规则**：任一闸门不过，当日**冻结该域**，不做该域任何新变更；执行下表的精确回退；次日只做根因与复测，
**不启动下一域**；48h 内不重切同批（`docs/projects/TD-60-cutover-schedule.md` §7）。

| 域 | 冻结动作 | 精确回退（环境变量级） | 回退后验证 | 次日 |
|---|---|---|---|---|
| auth | 停止本批新增入册 | `opsmesh cutover-unroute --users <本批>`；异常扩大则清空 `AUTH_CUTOVER_ROSTER_FILE` 并重启，必要时置 `AUTH_SVC_PROXY_ENABLED=false` | 本批用户本地口令登录 200；`GET /api/v1/admin/service-routing` 显示 auth 归属本地 | 只做根因；不启动 task |
| task | 停止代理/归属推进 | `TASK_SVC_PROXY_ENABLED=false` + `TASK_SVC_OWNS_NEW_TASKS=false` + 重启。**把 `task` 加入 `OPSMESH_SERVICE_PROXY` 只停别名，不会把主代理切回本地** | 本地 `POST /api/v1/tasks` 201；leader=1；claim/report 正常 | 只做根因；不启动 device |
| device | 停止 device 变更 | 把 `device` 加入 `OPSMESH_SERVICE_PROXY` 并重启 | 设备 CRUD 本地 200；provision/metrics/系统页正常 | 只做根因 |
| ABAC | 停止本日阶梯，不扩 cohort | `--abac-device-kill-switch=true` → `--abac-device-mode=off`（可先只对当前 cohort） | 无新 `abac denied:`；2 个上报周期（约 60s）内无该 cohort 新决策行；TTR 记入日报 | 停在当前百分比，不跳级 |

> 删除后（12-07 起 auth、12-21 起 task、01-11 起 device）该域**不再具备本地回退路径**，
> 回退只能走删除清单里的分支恢复；这是「逐域删除」的代价，排期已把删除放在 100% 稳定 5 个工作日之后。

### 6.2 缓冲日：11-24、12-08、12-18、12-28、01-08

**定义**：缓冲日 = **不排 backlog 新工作**，不接新需求；当日已有的闸门、观察、收口、回退照做。
它保护的是「前一日遗留 + 当日闸门」的叠加，不是休假。

| 缓冲日 | 保护什么 |
|---|---|
| 11-24 | auth Go/No-Go 的决策质量：当天不写新代码，只做冒烟、拍板与签字 |
| 12-08 | auth 删除 day2 的收尾余量；同时吸收 ABAC E2 |
| 12-18 | task 100% 观察窗的异常处置余量，避免带着未解释现象进入删除 |
| 12-28 | task 删除后验证 day3 + device Go/No-Go 的双闸门余量 |
| 01-08 | device 100% 窗的异常处置余量，避免带着未解释现象进入删除 |

### 6.3 已知的闸门叠加日（不改日期，只定优先级）

| 日期 | 叠加项 | 处理 |
|---|---|---|
| 12-04 | auth 100% 窗收口 + M4 D8 升门 | 同日两签，证据各自独立（§7 S2） |
| 12-07 | auth 删除 day1 + task Go/No-Go + ABAC E1 | 主块只做 auth 删除；次块先做 task Go/No-Go；两项都过且 15:00 后才做 E1，任一失败则 E1 取消 |
| 12-08 | auth 删除 day2 + ABAC E2 + 缓冲 | 删除收尾优先；E2 仅在 day2 无异常时执行 |
| 12-09 | task 50/50 + auth 验证 day1 + ABAC E3 | task 切换 10:00 前完成；E3 需 task 首轮抽验通过 |
| 12-11 | task 50/50 收口 + auth 验证 day3 + ABAC E5 | 三项都需签字；task 若要回退，E5 顺延且 ABAC 停在 50% |
| 12-28 | task 验证 day3 + device Go/No-Go + 缓冲 | 验证收口在前，Go/No-Go 在后；Go/No-Go 不过则 12-29 不切 |

### 6.4 48h 规则与不可避免的重叠

TD-60 的固定日期使 enforce 阶梯（12-07 → 12-11）必然与 auth 删除（12-07/08）、task 50/50（12-09）、
task 观察窗（12-09 → 12-11）重叠：**在 01-20 之前不存在一个 5 天连续、且不撞任何整域变更的窗口**。

处理（需 owner 在 S2 确认）：

1. 「48h 不并行两个高风险变更」按**整域变更之间**执行：auth / task / device 三域任意两域的切换、
   100%、删除不得落在同一 48h 窗口内。本排期满足该解释（三域严格串行）。
2. ABAC 阶梯按**低爆炸半径变更**处理：改动对象是 `segment=prod-edge-a` 内的设备配置（≤40 台），
   回退是 ≤5 分钟的 kill switch，不触碰共享库表与路由归属。因此允许与整域动作同日，但必须遵守：
   主块只做 TD-60；ABAC 阶梯只在 15:00 后执行；当日 TD-60 门禁失败则 ABAC 当日不推进。
3. 阶梯零缓冲：E1–E5 各需 24h 稳定。任一 HOLD 顺延到下一个「无 TD-60 闸门」的日子
   （可用窗口 12-15 → 12-17；12-18 是 task 收口缓冲日，不排阶梯）。若 12-17 前未完成，
   按 soak 计划 §8 记「部分成功」，停在当前百分比，不跳级、不带病进入 100%。
4. 若 owner 不接受第 2 条，则 ABAC 阶梯只能整体移到 device 100% 观察窗之后（最早 01-13 起），
   会与 device 删除、M5 收口重叠；该情形下建议直接停在 shadow，把 enforce 放量推到下一个交付周期。

---

## 7. 签字点（6 个）

> 每个签字点的证据包必须**同时**包含：当日门禁 JSON（`counts.p0==0`、无未解释 stale）、
> 最近一次全量 `-race` 报告（0 DATA RACE）、相关 soak 日报（若在 soak 期）。
> 域内观察窗收口（11-27、12-18、12-31、01-15）由域 owner + 观察员 + 回退执行人三方签收，
> 不单独占用下列主签字点，但缺少该签收则不得进入下一动作。

| # | 日期 | 签字内容 | 解锁 | 必需证据 |
|---|---|---|---|---|
| **S1** | 2026-11-24 | auth Go/No-Go（P1–P7、P10–P11 逐条） | 11-25 写名册 | 门禁 JSON + `smoke-cutover.sh`/`smoke-task-cutover.sh` 日志 + 软/硬回退实测秒数 + 7 天基线导出 + 角色表无待填 + 三套开关 staging 记录 |
| **S2** | 2026-12-04 | auth 100% 5 天窗收口 + M4 D8 升门 | 12-07 auth 删除 + ABAC E1 | auth：5 个工作日指标（登录 5xx=0、429 不持续、p99 ≤ 基线 ×1.2）；ABAC：S0–S15 日报齐全、32 条 fixture 差异 0、live 分歧 0 且覆盖率 ≥95%、dropped <0.1%、上传滞后 p95 ≤30s、S7 TTR ≤5min |
| **S3** | 2026-12-07 | task Go/No-Go | 12-09 task 50/50 | 门禁 JSON + task 冒烟 + agent 适配层 E2E（claim/report 幂等）+ batch/canary 归属决策记录 + 回退演练 + leader=1 预演 |
| **S4** | 2026-12-11 | auth 删除后验证收口 + task 50/50 窗收口 + ABAC E5 | 12-14 task 100% | 全量 build/test/lint/smoke + 404/405=0；task：leader=1、claim/report 成功率 ≥ 基线、batch 计数一致；ABAC：E1–E4 每级连续 24h 全门通过 + 100% 金丝雀 owner 签 |
| **S5** | 2026-12-28 | task 删除后验证收口 + device Go/No-Go | 12-29 device 50/50 | task 删除后 3 天全绿；device：D-1/D-2/D-3 契约测试红转绿、federation 下沉、device 冒烟脚本入库、回退演练、7 天基线 |
| **S6** | 2027-01-08（工程）+ 01-18/20（终验） | device 100% 5 天窗收口；M5 终验 | 01-11 device 删除；关闭 TD-60 | device 5 天指标 + netsec/federation 全绿 + 删除清单评审；M5：文档 diff、全量门禁、证据归档清单、项目索引状态 |

---

## 8. 与源排期的差异（已在本文件校正）

### 8.1 日期矛盾（2 条）

| # | 源文档口径 | 冲突点 | 本文件处理 |
|---|---|---|---|
| 1 | `TD-60-cutover-schedule.md` §5 把 task 删除后验证写成「3 个工作日：12-23、12-24、12-28」，跳过 12-25（周五） | 按 A1，12-25 是正常工作日；3 个工作日应为 12-23/24/25 或写明 12-25 不计 | 12-25 排为普通工作日（验证可提前做 + B5）；**验证收口仍按源排期的 12-28 17:00**，不改源日期 |
| 2 | `abac-m4-soak-plan.md` §2 用相对日：D8 = D0+8、D9–D13 = D0+9…D0+13；若 D0=11-19，则 D8=11-27、D13=12-02 | 该套算会把 enforce 阶梯放进 auth 100% 观察窗（11-30–12-04），与 A4 冲突 | 新增 S/E 编号（§1.4）：S7=11-26 保留 D7 演练，S15=12-04 作为 D8 评审，D9–D13 映射为 E1–E5（12-07–12-11）。计划的硬门槛「shadow ≥7×24h」仍满足，但 7 天 soak 实际变成 16 天，**需在 S2 由 owner 追认** |

### 8.2 约束冲突（2 条）

| # | 约束 | 冲突点 | 本文件处理 |
|---|---|---|---|
| 3 | 指定 11-24、12-08、12-18、12-28、01-08 为「缓冲/不接新工作」日 | 这 5 天在 TD-60 §5 分别是 auth Go/No-Go、auth 删除 day2、task 100% 观察窗收口、device Go/No-Go、device 100% 观察窗收口 | 重定义缓冲为「不排 backlog 新工作，闸门/观察/收口照做」（§6.2），日期不变 |
| 4 | 「48h 内不并行两个高风险变更」 | TD-60 固定日期使 ABAC 阶梯与 auth 删除、task 切换必然重叠（§6.4） | 48h 规则按整域解释；ABAC 阶梯按低爆炸半径例外处理，需 owner 在 S2 确认；备选方案已登记 |

> 未修改 `TD-60-cutover-schedule.md` 与 `abac-m4-soak-plan.md`；两条源排期仍是日期与门禁的权威来源，
> 本文件只做逐日展开与上述差异的显式记录。

---

## 9. 关联文档

- `docs/projects/TD-60-cutover-schedule.md` —— 逐域顺序、前置条件、门禁与回退（日期权威）
- `docs/projects/abac-m4-soak-plan.md` —— M4 阶段表、晋升门禁、日报模板（soak 权威）
- `docs/projects/TD-60-domain-convergence-charter.md` —— 上位立项书、D+C 规则、全局回滚
- `docs/td60-consistency-2026-11-17.md` —— P0/P1/P2 清单与证据
- `docs/race-sweep-2026-11-18.md` —— `-race` 分片命令、实测墙钟、72 条 skip 明细
- `docs/abac-m1-m3-dogfood-2026-11-18.md` —— M1–M3 真实栈验收与 M4 观测建议
- `docs/api-versioning-policy.md` —— v1 默认形态冻结与契约测试缺口（B1 依据）
