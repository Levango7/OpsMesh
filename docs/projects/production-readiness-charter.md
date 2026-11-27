# Production Readiness Charter — Core Domains First

> 本文按 `docs/projects/README.md` 的统一模板编写；事实以 `file:line` 或文档引用标注；未落地的设计参数明确写「待复核」，不把提案写成现状。Owner 决策 required；状态 = 已立项。单 CLAIM scope `docs/projects/production-readiness-charter.md`（append-only，见 `docs/CLAIMS.md`）。

## 1. 一句话定义

将 OpsMesh 从 **feature-stacked MVP+** 收口为 **production-grade 私有平台**：冻结新特性域，优先完成 **auth/task/device 切流（TD-60）**、**ABAC M4 强制落地**、**真实环境验证**、**安全与租户边界**、**声明真实性（claims truthfulness）**，以可证据的门禁而非功能堆叠证明生产可用。

## 2. 背景与问题证据

### 2.1 现状：功能堆叠，生产未装配

| 事实 | 证据 |
|---|---|
| 能力面广（设备/任务/CMDB/告警/日志/编排/K8s/中间件/联邦/ABAC/AIOps），但多以 unit/contract 测试验证 | `docs/CLAIMS.md`、`docs/tech-debt.md`；`docs/evaluation-report.md` |
| 控制面双轨：monolith + 微服务并存，契约/权限/路由/租户边界/错误语义仍在浮动 | `docs/architecture-audit-2026-11-23.md`；`docs/td60-domain-convergence-analysis-2026-10-11.md` |
| 历史缺口：fail-open 认证/租户、shell 注入、虚假成功、匿名入口、弱凭据 | `docs/security-issues.md`；`docs/security-review-2026-11-16.md`；`scripts/shell-injection-audit.py` |
| TD-60 三域切流已排期（auth→task→device），阶段 2 进行中 | `docs/projects/TD-60-domain-convergence-charter.md`；`docs/projects/TD-60-cutover-schedule.md`；`docs/projects/delivery-schedule-2026-11-19.md` |
| ABAC M4 机制完成（默认 off），shadow→enforce 放量未开始 | `docs/projects/abac-m4-soak-plan.md`；`docs/abac-m1-m3-dogfood-2026-11-18.md` |
| 联邦管理已切 per-peer 密钥 + 入站验签 | `docs/` federation；commit `a5bfa0f6` 附近 |
| TSDB/graph 写权限已收口到 `task:write` | `internal/controlplane/tsdb_ops.go`、`graph_ops.go` |

### 2.2 本章程要解决的三类风险

1. **声明虚假（truthfulness）**：文档/CHANGELOG/README 宣称的能力与实现/测试不一致，门禁无法阻断。
2. **边界未闭（security/tenant）**：匿名、fail-open、shell 注入、弱凭据、弱租户隔离在生产不可接受。
3. **未在真实环境验证（validation）**：仅 unit/contract 测试，MySQL/Redis/Kafka/etcd/CNI/HA、1k–5k 规模未跑通。

## 3. 目标（可验证）

1. **声明真实性**：所有对外声明（README/CHANGELOG/feature-design/api-reference）与代码/测试对齐；虚假能力下线或标「未实现」；guard false success（空结果/错误被吞）。
2. **TD-60 三域切流完成**：auth/task/device 全量切到微服务，整域开关 + 按增量切换，不搬历史数据；每域有 gates、rollback、evidence。
3. **ABAC M4 强制落地**：shadow → 1/5/25/50/100 enforce，带 kill switch、7 天 soak、TTR（time-to-recover）目标。
4. **真实环境验证**：MySQL/Redis/Kafka/etcd/CNI/HA 全链路；规模 1k–5k 设备/任务；性能与故障注入测试通过。
5. **安全与租户闭合**：移除匿名入口、fail-open、shell 注入；per-peer 密钥已完成；租户边界可审计。
6. **发布就绪**：文档、交付、runbook、smoke、airgap 全套可执行。

## 4. 非目标

- 不新增特性域（K8s 生命周期、AI gateway、RAG、新云厂商等）；仅收口已有。
- 不做大规模重构 UI/前端设计系统（仅与后端契约对齐所需的最小改动）。
- 不搬历史数据（TD-60 策略：整域开关 + 按增量切换）。
- 不在本章程内完成华为昇腾 NPU / HarmonyOS（已搁置，见 constraints）。
- 不引入云资源（存储最多 FastDFS 或其他分布式存储）。

## 5. 原则

1. **Freeze new feature domains**：新功能 PR 需 charter 例外审批；默认拒绝。
2. **Core domains first**：auth → task → device 顺序不可颠倒；ABAC M4 在 task/device 切流稳定后推进。
3. **Evidence over claims**：每阶段以可复现证据（命令/日志/报告）为退出判据，不以「已完成」 prose 为准。
4. **Single CLAIM per scope**：每 scope 仅一个存活 CLAIM；append-only，不改历史行。
5. **No overlap**：与 zcode/qoder/workbuddy/opencode 的 scope 不重叠；撞车按 CLAIM 时序裁定。
6. **Kill switch first**：任何强制落地必须带 `--abac-enforce` 默认空 kill switch；panic 时 fail closed + 审计 + 指标。
7. **Fail closed on auth/tenant**：认证/租户解析失败必须拒绝，不得放行。

## 6. 阶段与里程碑

### Phase P0 — Truth / Claim Cleanup（声明真实性）

- **目标**：移除虚假能力、同步文档、guard false success。
- **范围**：README/CHANGELOG/feature-design/api-reference/tech-debt；`false success` 路径（空结果/错误吞掉）；claims 门禁。
- **Owner**：kilo（lead）。
- **退出判据**：
  - `scripts/coord-gate.py` 退出码 0（无 issue/conflict/blocked）。
  - 全仓 grep「已完成/已落地」声明与代码对齐清单（`docs/` 审计报告）。
  - false-success 路径单测覆盖（空结果返回错误、错误不吞）。
  - CLAIMS 无过期未闭环项。
- **风险**：文档体量大，审计耗时；误删真实能力。
- **回滚**：仅文档/测试改动，git revert 单 commit；代码路径改动走 feature flag。

### Phase P1 — TD-60 Cutover auth/task/device

- **目标**：三域切流完成，带 gates、rollback、evidence。
- **范围**：`docs/projects/TD-60-domain-convergence-charter.md`、`TD-60-cutover-schedule.md`、`delivery-schedule-2026-11-19.md`；`internal/controlplane`、`services/task-svc`、`services/auth-svc`（如存在）。
- **Owner**：kilo（lead）调度；执行按 DISPATCH。
- **退出判据**：
  - auth 域切流完成且 7 天观察窗无回滚。
  - task 域切流完成且 7 天观察窗无回滚。
  - device 域切流完成且 7 天观察窗无回滚。
  - 每域 `td60-consistency.py` 基线通过；契约测试绿。
  - 回滚演练各执行一次并留证。
- **风险**：双轨并存期间契约漂移；历史数据不搬导致一致性 gap。
- **回滚**：整域开关回切 monolith；`--td60-domain=` 开关；保留旧路径代码至观察窗结束。

### Phase P2 — ABAC M4 Enforce

- **目标**：shadow → 1/5/25/50/100 enforce，带 kill switch、soak、TTR。
- **范围**：`docs/projects/abac-m4-soak-plan.md`；ABAC 评估器、设备侧 guard、四道回滚杠杆。
- **Owner**：kilo（lead）。
- **退出判据**：
  - shadow would-deny < 1% on 关键路径连续 7 天。
  - enforce 1/5/25/50/100 各档 soak 通过，无 TTR 超标（目标 ≤ 15 min）。
  - kill switch `--abac-enforce` 默认空；触发后 fail closed + 审计 + 指标。
  - ABAC 强制覆盖：设备删除、系统用户删除、防火墙增删、租户删除、fileshare 挂载（`--abac-enforce-preset=high-risk`）。
- **风险**：would-deny 误杀合法操作；性能回退。
- **回滚**：`--abac-enforce=` 清空即回 shadow；设备侧 guard 本地策略包回滚。

### Phase P3 — Real-Environment Integration / Perf / Failure Tests

- **目标**：MySQL/Redis/Kafka/etcd/CNI/HA 全链路；规模 1k–5k。
- **范围**：`docker-compose.*.yml`、`tests/`、`scripts/loadtest/`、`scripts/smoke/`。
- **Owner**：kilo（lead）调度；环境由用户/CI 提供。
- **退出判据**：
  - MySQL/Redis/Kafka/etcd 各组件连通与故障注入（断网/慢查询/主从切换）通过。
  - CNI/网络策略场景通过。
  - HA（主从/多副本）故障切换 RTO/RPO 达标并留证。
  - 规模 1k/2k/5k 设备与任务压测报告（吞吐/延迟/p99/错误率）。
  - 无数据丢失、无租户串扰。
- **风险**：无云资源，环境自建成本高；测试数据规模。
- **回滚**：测试环境隔离，不影响生产；失败仅阻断放量。

### Phase P4 — Shell / Tenant / Security Hardening Closure

- **目标**：移除匿名、fail-open、注入；per-peer 密钥已完成。
- **范围**：`scripts/shell-injection-audit.py`、`docs/security-issues.md`、`docs/security-review-2026-11-16.md`、auth/tenant 中间件。
- **Owner**：kilo（lead）。
- **退出判据**：
  - shell 注入审计 0 高危；所有下发值经 `shellQuote`/白名单。
  - 匿名入口移除（无未鉴权写操作）。
  - fail-open 路径清零（auth/tenant 解析失败 → 拒绝 + 审计）。
  - 弱凭据/默认密码清零；密钥轮换演练通过。
  - 租户边界测试（跨租户读写）通过。
  - per-peer 密钥轮换演练通过（已落地，复验）。
- **风险**：移除匿名可能破坏旧客户端；需兼容窗口。
- **回滚**：兼容开关保留 1 个版本；安全项不回滚（仅兼容层）。

### Phase P5 — Release Hardening

- **目标**：文档、交付、runbook、smoke、airgap 全套可执行。
- **范围**：`docs/airgap-install-sop.md`、`docs/deployment-guide.md`、`docs/dr-runbook.md`、`scripts/smoke/`、`DELIVERY.md`。
- **Owner**：kilo（lead）。
- **退出判据**：
  - airgap 安装 SOP 在离线环境跑通。
  - smoke 全套绿（CI + 本地）。
  - runbook（升级/回滚/故障）演练通过。
  - 交付包（镜像/二进制/校验和）可复现。
  - CHANGELOG/README 与发布版本一致。
- **风险**：离线环境差异；文档滞后。
- **回滚**：发布包版本回退；文档版本化。

## 7. 任务队列与协调规则

### 7.1 任务拆分（建议）

| Task ID | 阶段 | scope | 负责 | 依赖 |
|---|---|---|---|---|
| PR-0 | P0 | `docs/`、`scripts/coord-gate.py` | kilo | — |
| PR-1 | P1 | `internal/controlplane/`、`services/task-svc/`、`services/auth-svc/` | kilo | PR-0 |
| PR-2 | P2 | ABAC 评估器与设备侧 guard | kilo | PR-1 |
| PR-3 | P3 | `tests/`、`scripts/loadtest/`、`scripts/smoke/` | kilo | PR-1,PR-2 |
| PR-4 | P4 | auth/tenant 中间件、shell 审计 | kilo | PR-0 |
| PR-5 | P5 | docs、DELIVERY、smoke、airgap | kilo | PR-0..PR-4 |

### 7.2 协调规则

1. **Single CLAIM per scope**：每 scope 仅一个存活 CLAIM；append-only，不改历史行。
2. **No overlap**：与 zcode/qoder/workbuddy/opencode 的 scope 不重叠；撞车按 CLAIM 时序裁定，后到者停手并回 `[CONFLICT]`。
3. **DISPATCH 仅 lead 发出**；机动线接活后施工，中途重定向先 `[BLOCKED]`。
4. **DONE 必带 scope + commit**；当天 push，未提交状态不得过夜。
5. **CLAIM ttl 到期无闭环** → 任一线可 TAKEOVER，但必须留痕。
6. **禁区**：`.github/workflows/`、CHANGELOG 顶部引用块、`docs/tech-debt.md` 标记行，未经用户明确不碰。
7. **append-only claims**：`docs/CLAIMS.md` 仅追加；本章程 CLAIM 为 `docs/projects/production-readiness-charter.md`。

## 8. 验收标准 DoD（全局）

1. `python scripts/coord-gate.py` 退出码 0。
2. P0–P5 各阶段退出判据全部满足并留证（报告/日志/命令输出）。
3. 安全审计 0 高危；租户边界测试通过。
4. 真实环境 P3 报告齐备（MySQL/Redis/Kafka/etcd/CNI/HA + 1k–5k 规模）。
5. 发布包 + airgap SOP + smoke 全绿。
6. 声明真实性：README/CHANGELOG/feature-design 与代码对齐。

## 9. 风险与缓解

| 风险 | 缓解 |
|---|---|
| 范围蔓延（新功能混入） | Freeze 原则 + PR 例外审批；CLAIM scope 审核 |
| 双轨并存契约漂移 | TD-60 一致性基线 + 契约测试门禁 |
| ABAC 误杀 | shadow would-deny <1% 门槛 + kill switch + fail closed |
| 环境不足（无云资源） | minikube/kind + Docker 单机补充；P3 分阶段 |
| 人力/协调 | 单 lead（kilo）；append-only claims；coord-gate 门禁 |
| 文档滞后 | P0 先 truth cleanup；P5 收口 |

## 10. 回滚策略（全局）

- **P0/P5**：文档/测试改动，git revert 单 commit。
- **P1**：整域开关回切 monolith；保留旧路径代码至观察窗结束。
- **P2**：`--abac-enforce=` 清空回 shadow；设备侧 guard 本地策略包回滚。
- **P3**：测试环境隔离，失败仅阻断放量，不回滚生产。
- **P4**：兼容开关保留 1 个版本；安全项不回滚（仅兼容层）。
- **全局**：任何阶段失败可冻结后续阶段，回退到上一稳定 tag；回滚演练每阶段至少一次。

## 11. 关联文档

- `docs/projects/README.md` —— 立项索引与模板。
- `docs/projects/TD-60-domain-convergence-charter.md`、`TD-60-cutover-schedule.md`、`delivery-schedule-2026-11-19.md` —— TD-60 三域切流。
- `docs/projects/abac-m4-soak-plan.md`、`docs/abac-m1-m3-dogfood-2026-11-18.md` —— ABAC M4。
- `docs/td60-*.md` —— 切流决策单/runbook/一致性报告/契约对账。
- `docs/security-issues.md`、`docs/security-review-2026-11-16.md` —— 安全。
- `docs/airgap-install-sop.md`、`docs/dr-runbook.md`、`docs/deployment-guide.md` —— 交付/runbook。
- `docs/tech-debt.md` —— 债务台账。
- `docs/COORDINATION-BOARD.md`、`docs/CLAIMS.md` —— 协作协议与领地日志。
- `AGENTS.md` —— 各线纪律。
