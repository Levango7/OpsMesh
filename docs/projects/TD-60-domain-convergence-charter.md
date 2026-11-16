# TD-60 域收敛立项书（task / device / auth）

> 本文按 `docs/projects/README.md` 的统一模板编写。事实以 `file:line` 或文档引用标注；
> 无法从仓库复核的条目明确写「待复核」，不把估算写成实测。

## 1. 一句话定义

在「整域开关 + 按增量切换 + 不搬历史数据」的既定决策下，把 task / device / auth 三域从
controlplane 双份实现收敛到 task-svc / device-svc / auth-svc；agent gRPC 管控通道保留在
controlplane，只加内部转调适配层；最终删除 controlplane 侧约 5,000 行旧实现。

## 2. 背景与问题证据

### 2.1 债务规模：27k 是历史快照，当前残余 5,980 行

| 证据 | 数值 | 出处 |
|---|---|---|
| 初始重复规模（2026-09-05 深度侦察） | 约 27,000 行 | `docs/tech-debt.md:149` |
| 2026-10-11 实测残余 | 5,980 行（task 2,691 / device 1,339 / auth 1,950） | `docs/td60-domain-convergence-analysis-2026-10-11.md:19-24` |
| controlplane 侧 store 直写 | 104 处（task 59 / device 13 / auth 32） | 同上 |
| controlplane 侧后台循环 | 9 个（task 7 / device 1 / auth 1） | `docs/td60-domain-convergence-analysis-2026-10-11.md:21-23` |
| 收敛后可删 | 约 5,000 行；保留 agent gRPC 约 828 行 + netsec/federation 约 900 行下沉 | `docs/td60-domain-convergence-analysis-2026-10-11.md:127-136` |

> 口径提醒：27,000 行是 2026-09-05 的侦察快照，不是今天的待办量。任何引用都必须同时给出
> 「5,980 行残余」这个当前实测值，否则会把已收敛的轻四域重复计债。当前数字是否仍精确，
> 以下一轮实测为准（**待复核**：2026-10-11 之后 controlplane 仍有新功能提交）。

### 2.2 阶段状态

| 阶段 | 状态 | 证据 |
|---|---|---|
| 阶段 1：轻四域（gpu / runbook / incident / autoscaler）代理化 | **已完成** | controlplane 侧无域实现文件，纯 `svcproxy` 转发；遗留前端契约缺口（4 个必 404 + 2 个 405 + 3 处字段断裂）已修，补 22 个 handler 防回归测试（`e2e757c` + `67a1050`），CI 全绿（`docs/tech-debt.md:149`） |
| 阶段 2：核心三域 task / device / auth | **进行中，约 60%** | `docs/tech-debt.md:149` 末段（2026-11-16 更新：cascade cleanup 18 域完成；phase 2 进度 60%） |
| A-2 生产路径补齐 | **已完成**（2026-09-17，`4a85fef`） | task-svc 恢复 EventBus/AuditSink/SSEBridge + K8s Lease 选主；device-svc 补 agent 分发 / AutoProvisionLoop / metrics；auth-svc 补 DeviceFP / 密码策略 / Redis SessionStore |
| P0 阻塞项补齐 | **已完成**（2026-09-17，`1258289`） | task-svc 12 个 REST 端点 HTTP 网关；device-svc provision/metrics/聚合响应；auth-svc PUT users/roles + requirePermission |
| 双轨本地 Docker 验证 | **已完成**（2026-09-17，`1d74d34`） | 6 容器全部健康，P0 端点可达；修复 device-svc 路由冲突 panic |

### 2.3 双轨一致性：区分「端点路径对齐率」与「收敛进度」

| 域 | 2026-09-17 初版（`b7d28b5`） | 2026-09-19 v1.2 报告 | 说明 |
|---|---|---|---|
| task | 60.0%（9/15） | 100.0%（15/15） | v1.2 补齐 batch-exec / canary / approval / schedules pause-resume 后，**路径与字段**全对齐 |
| device | 60.0%（6/10） | 100.0%（10/10） | v1.2 补齐 provision / metrics / 聚合响应后，路径全对齐 |
| auth | 86.7%（13/15） | 100.0%（15/15） | v1.2 补齐 2 个 PUT 端点后，路径全对齐 |

出处：`docs/td60-consistency-report.md:14-18`、`:481-483`。

> **为什么两个数字同时存在**：100% 是「以 controlplane 端点数为基准的契约路径对齐率」，
> 不是「域已经收敛完」。截至 2026-11-16，阶段 2 的实际收敛进度是 **60%**；controlplane 侧
> 仍有 5,980 行活跃实现与 9 个后台循环。把 100% 当成完工信号是本项目最容易犯的误读。
> 另：v1.2 报告自述依据是静态读码与端到端验证，**未覆盖每一项运行时数据面差异**（见 §2.5）。

### 2.4 三域各自的真实堵点

| 域 | 已具备 | 未闭合 | 证据 |
|---|---|---|---|
| task | task-svc HTTP 网关 12 个 REST 端点；调度器四循环（schedule 30s / reclaim 30s / leader 5s）；K8s Lease 选主；事件总线 / 审计 / SSE 桥接 | agent gRPC 通道（PullTasks / ReportResult / Heartbeat / Register）仍直写 controlplane store，需约 200~300 行内部转调适配层；leader 身份切换的部署开关**待复核**；批次聚合计数两侧口径不同 | `docs/td60-domain-convergence-analysis-2026-10-11.md:61-114`；`services/task-svc/internal/scheduler/scheduler.go:28-29,53-54`；`docs/tech-debt.md:149` |
| device | HTTP 网关、provision / metrics / 聚合响应、agent 二进制分发、AutoProvisionLoop、metrics | netsec（SSRF / 私网校验，约 556 行）与 federation（联邦验签，约 372 行）在 device-svc **没有等价物**，属「独有能力下沉」而非「去重」；需先下沉 pkg 再切 | `docs/td60-domain-convergence-analysis-2026-10-11.md:73-78,116-121` |
| auth | 分端点裁决、名册热更 30s、搬号 job、回退 job、e2e 16/16、失败计数按转发状态码补记 | 机制全建好，只差运维执行；auth-svc HTTP 网关默认关闭（`AUTH_SVC_HTTP_ENABLED=false`），切流前必须显式打开；首登改密令牌与一次性口令交接依赖人工流程 | `docs/td60-domain-convergence-analysis-2026-10-11.md:47-59`；`deploy/docker/docker-compose.prod.yml:614`；`docs/td60-auth-cutover-runbook.md:39-60` |

### 2.5 已知不一致与运行时边界（切流前必须显式接受）

| 项 | 事实 | 影响 | 处置 |
|---|---|---|---|
| 批次聚合计数 | 单体读时按 task 状态聚合；task-svc 读 `batches` 表计数列 | 双写 / 多副本期间两边计数可能不一致 | 切流 runbook 必须写「切换前排空在飞批次」；切换后以单一侧为准 |
| 共享库表形状 | task-svc 同库路径下 `task_results` 可能与控制面迁移建出的 6 列旧表异形共存；`CREATE TABLE IF NOT EXISTS` 会静默让位 | 写路径 best-effort ⇒ 执行结果可能静默不落库 | `recordEnsureColumns` 预检补列 + 真库用例 `TestRecordTablesReconcileSharedDBShapeMySQL`（`docs/tech-debt.md:149`） |
| auth 失败计数 | 名册用户的失败计数产生在 auth-svc 侧；本地 `loginguard` 曾看不到 | 账号锁定对名册用户可能不生效 | 已按转发状态码补记（401 记失败 / 2xx 清计数）；运行时**待复核** |
| 同出口 IP 登录突发 | 登录闸同 IP 令牌桶实测突发约 5 次，其后每约 6 秒补 1 枚，429 无 Retry-After | 同办公室 / 同 VPN 用户集中登录会被误判为「切流把认证搞坏了」 | 同出口用户错峰分批（`docs/td60-auth-cutover-runbook.md:45-48`） |
| auth-svc HTTP 默认关闭 | compose 默认 `AUTH_SVC_HTTP_ENABLED=false`，auth-svc 只出 gRPC | 不开则 auth 域无 HTTP 可切 | 切流前置显式打开（`deploy/docker/docker-compose.prod.yml:611-614`） |

### 2.6 切流机制现状（可直接复用的部分）

| 能力 | 落点 | 证据 |
|---|---|---|
| 逐域总闸 | `OPSMESH_SERVICE_PROXY`（逗号分隔域名） | `deploy/docker/docker-compose.prod.yml:517` |
| auth 总闸 | `AUTH_SVC_PROXY_ENABLED=true` | `internal/controlplane/cutover/router.go:43` |
| 增量归属闸（auth） | `AUTH_SVC_OWNS_NEW_ACCOUNTS=true`，本地建号 409 + 指引 | `internal/controlplane/cutover/router.go:60`；`internal/controlplane/auth_login.go:54` |
| 增量归属闸（task） | `TASK_SVC_OWNS_NEW_TASKS=true`，与 `OPSMESH_SERVICE_PROXY` 联锁 | `internal/controlplane/cutover/router.go:74`；`deploy/docker/docker-compose.prod.yml:530-536` |
| auth 名册热更 | 文件 / 内联二选一，歧义 fail-fast，30s 轮询 | `internal/controlplane/auth_cutover.go:41,72`；`internal/controlplane/cutover/roster.go:42-46` |
| 搬号 / 回退 job | `opsmesh cutover-migrate` / `cutover-unroute`（幂等，先摘名册后吊销会话） | `docs/td60-auth-cutover-runbook.md:67-127` |
| 运行时取证 | `GET /api/v1/admin/service-routing`（一域当前归属 / 后端 / 自检结论） | `deploy/docker/docker-compose.prod.yml:503` |
| 回退实测 | 软回退 20~23s（上限 = 30s 重载周期 + 一次请求）；硬回退约 4~6s | `docs/td60-auth-cutover-runbook.md:129-135` |

> 记录的切流决策是 **D + C**：整域开关 + 按增量（租户 / 用户群）分批切换，不搬历史数据
> （`docs/COORDINATION.md:1465`；`docs/td60-cutover-decisions.md:7-9,72`）。
> 原 `docs/td60-cutover-plan.md` 的 50/50 方案已被 2026-09-27 的 C 案重构标记 Superseded
> （`docs/tech-debt.md:149`）。本立项书保留 M2「50/50」作为**观察点**，不把它理解成
> 「按请求随机分流」——切换单位仍是整域开关 + 名册/增量。

## 3. 目标（可验证）

1. **三域 100% 切流**：task / device / auth 的前端 HTTP 请求全部经根路由转发到对应微服务；
   agent gRPC 的 PullTasks / ReportResult / Heartbeat / Register 经内部适配层转调 task-svc，
   controlplane 不再直接读写三域业务表。
2. **每个域都有独立回退闸门**：auth 软回退 ≤ 30s、硬回退 ≤ 10s（已有实测 20~23s / 4~6s）；
   task / device 通过环境变量 + 重启回退，目标 ≤ 5 分钟，且不丢已提交数据。
3. **一致性报告零未解释差异**：每个仍存在的差异都必须有 owner、到期日与处置结论；
   不允许「已知但没人认领」的 P0 差异。
4. **删除量可核**：controlplane 侧删除不少于 5,000 行旧实现；保留项只有 agent gRPC 通道
   （约 828 行）与已下沉 pkg 的 netsec/federation 能力，删除清单随 M4 提交。
5. **回归稳定**：三域双轨 E2E + 契约测试全绿，连续观察 1 周（或覆盖一次业务高峰）无 P0/P1；
   auth 登录 5xx = 0、`user not found` 类错误 = 0；task 无「失联任务持续增长」；device
   provision / metrics / netsec / federation 行为与基线一致。
6. **文档收口**：`docs/tech-debt.md` TD-60 行、三份 cutover runbook、`docs/architecture.md`
   的域归属描述、`docs/api-reference.md` 的路由说明全部与代码一致。

## 4. 非目标

- **不做历史数据搬迁**：两侧共享同一 MySQL（`--store=mysql` 时），切流改变的是「谁处理请求」，
  不是「数据存在哪」。
- **不改 proto 契约**：`proto/opsmesh/v1/task.proto` / `services/task-svc/api/proto/v1/task.proto`
  的字段与语义不在本项目内变更；需要变更时另立 ADR。
- **不做跨域大爆炸切换**：不允许 task / device / auth 同一次变更一起切；每域独立开关、独立观察窗。
- **不删除 agent gRPC 通道**：agent 只说 `opsmesh.v1.Registration` 协议，不认微服务协议；
  该通道保留在 controlplane，只加适配层。
- **不把 netsec / federation 复制到 device-svc**：这两块是独有能力，先下沉共享 pkg，再切 device。
- **不解决 TD-66（API 网关数据面）/ TD-61（controlplane 拆包）/ TD-97（AI 成本域）**：
  它们是独立立项，不与本项目的里程碑耦合。

## 5. 工作分解与里程碑

| 阶段 | 交付物 | 退出判据 | 预估 |
|---|---|---|---|
| **M1 双轨一致性报告** | auth / task / device 三份当前态报告：逐端点矩阵、运行时差异、数据面差异、差异台账（owner + 到期日）；可复跑的取证脚本 | 三份报告评审通过；无未解释 P0；基线指标已记录（登录失败率、5xx、P99、stale task、leader 数量、batch 计数） | 4~6 人日 |
| **M2 50/50 切流** | auth：名册覆盖 50% 活跃用户（按批）；task：leader 切到 task-svc + 影子读对账 + 50% 流量观察；device：netsec/federation 下沉后 + 影子读 + 50% 流量观察。50% 的载体（LB 权重 / 名册 / 小租户）**待复核**，但不得绕过整域开关 | 每域至少 2 个观察窗（每窗 ≥ 2 个工作日或覆盖一次业务高峰）；每域完成一次回退演练；无 P0/P1；指标在 §6 阈值内 | 6~8 人日 |
| **M3 100% 切流** | 三域全量走微服务；`AUTH_SVC_OWNS_NEW_ACCOUNTS` / `TASK_SVC_OWNS_NEW_TASKS` 置 true；controlplane 本地写路径关闭；`GET /api/v1/admin/service-routing` 显示三域归属对侧 | 连续 1 周稳定；零回退触发；数据对账（列表 / 详情 / 计数）通过；agent 任务领取与结果上报无丢失 | 4~6 人日（不含观察窗日历时间） |
| **M4 移除 controlplane 旧实现** | 删除三域 handler / 后台循环 / 本地 store 直写；保留 cutover router、agent gRPC 适配层、loginGuard 闸面；提交删除清单与路由表 diff | `go build ./...` + 全量测试 + lint 绿；无 404/405 回归；删除行数 ≥ 5,000 或对剩余项逐条说明 | 4~6 人日 |
| **M5 回归与文档收口** | 全量 E2E、故障演练（Redis / MySQL / 微服务不可达）、文档更新、TD-60 行关闭 | CI 全部腿绿；`docs/tech-debt.md` TD-60 移入已收口；runbook 与 `service-routing` 输出一致；owner 签收 | 4~6 人日 |

**关键路径**：M1（一致性报告）与 M2/M3 的回归观察窗，不是编码。新增代码总量约 500 行
（task agent 适配层约 200~300 行 + device 下沉约 200 行），删除约 5,000 行；排期风险主要来自
「等观察窗」与「等 owner 拍板批次」，不是开发工作量。

## 6. 验收标准 DoD

### 6.1 可执行命令

```bash
go build ./...
go test ./... -count=1
go test ./internal/controlplane/... ./internal/controlplane/cutover/... -count=1
go test ./services/task-svc/... ./services/device-svc/... ./services/auth-svc/... -count=1
golangci-lint run ./...
golangci-lint run -c .golangci.services.yml ./services/...
bash deploy/scripts/smoke-cutover.sh
```

> 说明：`smoke-cutover.sh` 是现有 auth 切流真进程冒烟脚本（含回退实测段）；
> task / device 的等价冒烟脚本在 M2 前补齐，命令名**待复核**后写入本节。

### 6.2 可观测指标

| 域 | 指标 | 通过阈值 |
|---|---|---|
| auth | 登录 5xx | 0 |
| auth | `user not found` / `invalid credentials` 类错误 | 0 个「用户不存在」误报；401 可高于基线但必须是真实凭证失败 |
| auth | 登录失败率 | ≤ 基线 + 0.5pp |
| auth | 401/403 率 | ≤ 基线 2 倍；且 5 分钟窗口内 < 10 次 |
| task | leader 数量 | 恰好 1 个（`GET /api/v1/admin/service-routing` + task-svc leader 指标） |
| task | 超过 2× 任务超时仍 pending/running 的任务 | 不随观察窗增长 |
| task | 批次聚合计数 | 切换前排空在飞批次；切换后与任务实际状态一致 |
| task | claim / reclaim / schedule fire | 与基线同量级，无持续失败 |
| device | provision / metrics 端点 | 成功率不低于基线；5xx ≤ 1% |
| device | netsec SSRF / 私网校验、federation 验签 | 对应用例全绿，无绕过 |
| 全局 | `GET /api/v1/admin/service-routing` | 三域 owner / backend / 自检结论与预期一致 |

### 6.3 回退演练（每域必须做一次）

| 域 | 软回退 | 硬回退 | 回退后验证 |
|---|---|---|---|
| auth | `opsmesh cutover-unroute --users <本批>`（摘名册 + 吊销对侧会话） | 清空 `AUTH_CUTOVER_ROSTER_FILE` 并重启控制面 | 本批用户本地口令登录 200；控制面路由日志无该批转发；实测软回退 20~23s、硬回退 4~6s |
| task | 从 `OPSMESH_SERVICE_PROXY` 移除 `task`，置 `TASK_SVC_OWNS_NEW_TASKS=false`，重启控制面 | 同上 + 确认 task-svc 释放 leader | 单 leader；本地建任务 201；任务领取 / 结果上报正常 |
| device | 从 `OPSMESH_SERVICE_PROXY` 移除 `device`，重启控制面 | 同上 | 设备 CRUD 本地 200；netsec / federation 用例全绿 |

> 回退不删除对侧已写入的数据（共享 MySQL），也不回滚已完成的账号迁移；重切时幂等继续。

## 7. 依赖与前置条件

| 依赖 / 前置 | 当前状态 | 需要谁 | 阻塞里程碑 |
|---|---|---|---|
| auth-svc HTTP 网关开启 | 默认 false（`deploy/docker/docker-compose.prod.yml:614`） | 运维 / owner | M2 |
| auth 名册 job + 一次性口令交付 | 机制就绪（`cutover-migrate` / `cutover-unroute`） | 运维 | M2 |
| 对侧 admin 完成首登改密 | 必需，否则迁移 job fail-closed | 运维 | M2 |
| task agent gRPC 适配层 | 未实现（约 200~300 行） | 开发 | M2 / M3 |
| task leader 身份切换方式 | task-svc 已有四循环 + K8s Lease 选主；具体部署开关**待复核** | 开发 + 运维 | M2 |
| device netsec / federation 下沉 pkg | 未实施（约 200 行） | 开发 | M2 |
| 批次聚合计数口径 | 已知不一致 | 开发 + 运维 | M2（排空） |
| 共享 MySQL 与表形状对账 | `recordEnsureColumns` 已覆盖 `task_results` 已知差异；其余表**待复核** | 开发 | M3 |
| owner 决策：批次名单 / 观察窗 / 口令交付方式 / 是否接受计数差异 | 未拍板 | owner | M2 |
| 按 `AGENTS.md` 先 CLAIM 再动文件 | 流程要求 | 执行线 | 全程 |

需要 owner 明确决策的项：

1. **批次名单与顺序**：哪些租户 / 用户先切，最大客户是否单独成批。
2. **观察窗长度**：2 个工作日还是覆盖一次登录高峰（取长）。
3. **一次性口令交付渠道**：谁发、何时发、如何避免与「需重新登录」通知脱节。
4. **是否接受批次计数差异**：接受则写进 runbook；不接受则先统一口径再切。
5. **task leader 切换的部署形态**：环境变量、K8s Lease 参数还是独立部署单元（**待复核**当前实现后定）。

## 8. 风险与缓解（含回滚）

| 风险 | 触发信号 | 缓解 | 回滚 |
|---|---|---|---|
| 用户被锁死（名册写了但号没搬 / 口令未送达） | 登录 401「用户不存在」；用户报「密码不对」 | 搬号 job 原子序：搬完才写名册；OTP 与通知同批送达；保留 `admin` 永不入册 | `cutover-unroute` 摘名册；必要时对侧重置口令 |
| auth 名册用户锁定不生效 | 暴力尝试未被 429 | 已按转发状态码补记失败计数；观察窗重点看 429 比例 | 回退该批到本地 |
| task 双 leader / 任务重复执行 | leader 指标 > 1；任务重复 claim | task-svc 选主 + controlplane 不再抢；切换前确认旧 leader 释放 | 从代理移除 task；恢复 controlplane leader |
| task 在飞批次计数错乱 | 两侧 batch 计数不一致 | 切换前排空在飞批次；切换后单侧读取 | 回退后以任务状态重新聚合 |
| 共享库表形状漂移 | 写入报 `Unknown column` / 结果不落库 | `recordEnsureColumns` + 真库用例；M3 前做一次全表对账 | 回退到 controlplane 写路径；修复迁移后重切 |
| agent 适配层重复写 / 丢写 | 任务结果重复或缺失 | 适配层幂等键 + 单写者原则；先影子读对账再切 | 关闭适配层开关，agent 回直写 controlplane |
| device netsec / federation 回归 | SSRF 校验被绕过；联邦验签失败 | 先下沉 pkg 并复用现有用例；切前跑安全用例 | 从代理移除 device |
| 同出口 IP 429 误伤 | 一批用户集中 429 | 同出口错峰；必要时临时提高 burst（需 owner 拍板） | 暂停该批，已切用户保留 |
| 观察窗不足导致问题晚发现 | 切换后 1~2 天才出现 | 每批 ≥ 2 个工作日或覆盖一次高峰；关键指标看板常驻 | 按域回退，不带半切状态过夜 |

**全局回滚原则**：M2 / M3 期间**不删除** controlplane 旧实现；任何一域异常，先把该域从
`OPSMESH_SERVICE_PROXY` 摘除并重启，再排查。M4 删除是不可逆步骤，必须在 M3 稳定 1 周后执行，
且保留删除清单与回滚分支。

## 9. 工作量估算（人日 + 关键路径）

| 阶段 | 人日 |
|---|---|
| M1 一致性报告 | 4~6 |
| M2 50/50 切流（含 auth 首批、task leader + 适配层、device 下沉） | 6~8 |
| M3 100% 切流 | 4~6 |
| M4 移除旧实现 | 4~6 |
| M5 回归与文档收口 | 4~6 |
| **合计** | **22~32 人日** |

- **日历周期**：6~8 周（含观察窗与 owner 决策等待）。
- **关键路径**：M1 一致性报告 → M2/M3 的回归观察窗。编码不是关键路径；新增代码约 500 行，
  删除约 5,000 行，真正的排期变量是「批次节奏」与「观察窗是否一次通过」。
- **不确定性**：agent gRPC 适配层的实际复杂度取决于 controlplane 与 task-svc 的 store 语义差异
  （**待复核**）；device netsec/federation 下沉的复用面也需先做一次代码盘点。

## 10. 状态与决策记录

**当前状态：已立项（阶段 2 进行中，约 60%）**
**负责人：待定**

| 日期 | 决策 / 事件 | 出处 |
|---|---|---|
| 2026-09-05 | 产品决策：选项 A，微服务化为正式方向；同日深度侦察拆两阶段 | `docs/tech-debt.md:149` |
| 2026-09-17 | A-2 生产路径补齐（`4a85fef`）、P0 阻塞项补齐（`1258289`）、双轨 Docker 验证（`1d74d34`）、一致性初版（`b7d28b5`，task 60% / device 60% / auth 86.7%） | `docs/tech-debt.md:149`；`docs/td60-consistency-report.md:481` |
| 2026-09-19 | 一致性 v1.2：三域端点路径对齐 100% | `docs/td60-consistency-report.md:483` |
| 2026-09-27 | 切流路径按 C 案重构：「先接通、再裁决」，逐域开关 + 自环 503；原 50/50 方案标记 Superseded | `docs/tech-debt.md:149` |
| 2026-10-10 | 数据面决策：近期 **D + C**（整域开关 + 按租户/用户群分批切换）；A 不立即立项 | `docs/COORDINATION.md:1465` |
| 2026-10-11 | 实测收敛分析：残余 5,980 行；auth 机制全建好；task 堵点 = agent 适配层 + leader；device 先下沉 netsec/federation | `docs/td60-domain-convergence-analysis-2026-10-11.md` |
| 2026-10-15 | task 域 R6 收口：批次 / 灰度落库；共享库表形状补列；真库用例钉住 | `docs/tech-debt.md:149` |
| 2026-11-16 | 阶段 2 进度约 60%；cascade cleanup 18 域完成 | `docs/tech-debt.md:149` 末段 |
| 待定 | 批次名单、观察窗、口令交付、task leader 切换形态 | 本文 §7 |
