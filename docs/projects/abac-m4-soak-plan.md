# ABAC M4 灰度放量 Soak 计划（7 天观察 → 选择器逐步 enforce）

> **状态：已批准，未开始执行（owner 2026-11-18）**
> **适用范围：设备侧 ABAC（agent `--abac-device-mode`）；默认仍为 `off`**
> **不适用：服务端定向强制 `--abac-enforce`（独立 opt-in，见章程「定向强制」）；全站 ABAC 强制（暂不启用）**
> **关联：** [设备侧决策引擎章程](./abac-device-engine-charter.md) §M4、[M1-M3 真实栈验收](../../docs/abac-m1-m3-dogfood-2026-11-18.md) §7、[报表脚本](../../scripts/abac/README.md)
> **代码基准：** `main@9fb4b675`（只读核对；本计划不改代码）

## 0. Owner 决策（逐字记录，2026-11-18）

> 1) M4 放量：批准“先 7 天 soak，再按设备属性选择器从 shadow → enforce 逐步放量”的方案。默认仍为 off。
>
> 2) ABAC 全站强制：确认暂不启用；只有当 RBAC→ABAC 策略基线完成且 shadow would-deny 在关键路径 <1% 时才重新评估。

**解释边界（不扩大原文）：**

- 批准的是**设备侧** M4 的 7 天 soak 与“选择器内 shadow → enforce”的逐步放量；不是全站 ABAC 强制，也不是服务端 `--abac-enforce` 白名单的扩大。
- 任何阶段都必须先通过第 4 节的晋升门；未通过即停在当前阶段，必要时回滚到 `shadow` 或 `off`。默认值始终是 `off`。
- 全站强制**没有**被本计划启用；第 2 条的两个前提（RBAC→ABAC 策略基线完成、关键路径 shadow would-deny <1%）未同时满足前不得重新评估。
- 本计划不改变 D3（签名私钥托管/轮换责任方）待定状态；生产密钥仍按 M1 的配置注入方式使用。

## 1. 范围与参与设备池

### 1.1 参与池

| 池 | 选择器（逐字段精确匹配，AND） | 设备数 | 角色 | 阶段 |
|---|---|---|---|---|
| 预发 | `segment=staging` | **12** | shadow 观察 + 每日一致性样本 + D7 回滚演练 | D0–D13 全程 `shadow` |
| 生产金丝雀 | `segment=prod-edge-a` | **40** | 7 天 shadow 后按百分比逐步 enforce | D0–D8 `shadow`；D9–D13 逐步 `enforce` |
| 其余生产 | 无（保持默认） | 全部 | 不参与，保持 `off` | — |

- 设备数在 **D0 冻结**：从 CMDB 导出 `deviceID/hostname/segment/os/arch/agentID` 与 agent 版本，预发必须恰好 12 台、生产金丝雀必须恰好 40 台；数量不符则 D0 不启动，先修清单。
- 只允许**同一候选 agent 构建**（含批次58 `internal/agent/abac_device_guard.go`，且心跳上报 `abac_bundle=v1` 与 `abac_decisions=v1`）进入池。缺能力、版本更旧或无法确认构建来源的设备一律排除（保持 `off`）。
- 选择器只支持 `segment` / `os` / `arch` / `hostname` / `agent_id`，逐字段精确匹配；**设备侧没有 labels**，本计划不使用 label 选择器。多个条件为 AND，任一不命中即 bypass。
- 预发池在 D0–D13 始终保持 `shadow`，作为生产 enforce 的对照组；生产金丝雀之外的设备不产生设备侧决策，也不写 M3 审计。

### 1.2 百分比如何落地

M4 没有“百分比”开关，只有 `mode` / `selector` / `kill-switch` / `disabled-ids`。本计划用**冻结的设备 ID 队列（cohort）**实现百分比：

- 目标比例按 `max(1, ceil(pct × N / 100))` 计算，`N=40` 时对应：D9 = 1 台（特例）、D10 = 2 台、D11 = 10 台、D12 = 20 台、D13 = 40 台。
- **首选**：每台 agent 的配置独立下发（CMDB/Ansible 生成）：cohort 内 `mode=enforce`，同一 selector 内非 cohort 设备 `mode=shadow`（只观察、不阻断，便于对比）。
- **兜底**：若只能共用一份配置，则全 selector 设 `mode=enforce`，并用 `--abac-device-disabled-ids` 列出所有非 cohort 设备 ID；该列表必须由 D0 冻结清单生成，禁止手写近似。
- 禁止用主机名前缀、分批重启等“事实上的比例”替代 cohort；选择器与 cohort 都必须可审计、可复现。
- 每个阶段至少稳定 **24 小时**；不得跳级（D9→D11 不允许）。

## 2. 阶段表（D0–D13）

D0 为启动日；D1–D7 为 7 天 soak；D8 为评审闸门；D9–D13 为生产金丝雀逐级放量。每日检查固定在 **09:00 UTC**。

| 阶段 | 日期 | 目标池 / 选择器 | 设备数 | 配置 | 动作 / 进入条件 | 该阶段回滚 |
|---|---|---|---|---|---|---|
| **D0** | 启动日 | 预发 + 生产金丝雀 / `segment=staging`、`segment=prod-edge-a` | 12 + 40 | 两池均 `mode=shadow`，`kill-switch=false`，`disabled-ids=∅` | 完成 §3.3 预检；冻结两池清单；开启双池 shadow；记录基线 24h | `--abac-device-kill-switch=true` → `--abac-device-mode=off`；shadow 不阻断，回滚仅停止观测 |
| **D1** | D0+1 | 预发 + 生产金丝雀（均 shadow） | 12 + 40 | 不变 | 09:00 UTC 日报；首份 200/304、lag、审计计数 | 同 D0；当日数据保留 |
| **D2** | D0+2 | 预发 + 生产金丝雀（均 shadow） | 12 + 40 | 不变 | 日报 + 32 条 fixture 重放 | 同 D0 |
| **D3** | D0+3 | 预发 + 生产金丝雀（均 shadow） | 12 + 40 | 不变 | 日报 + 单设备 `disabled-ids` 豁免验证（仅预发） | 移除豁免；必要时 kill switch |
| **D4** | D0+4 | 预发 + 生产金丝雀（均 shadow） | 12 + 40 | 不变 | 日报 + 隔离预发控制面重启演练（不影响生产控制面） | kill switch / mode=off |
| **D5** | D0+5 | 预发 + 生产金丝雀（均 shadow） | 12 + 40 | 不变 | 日报 + 报表只读连接中断演练（不触碰生产 DB） | kill switch / mode=off |
| **D6** | D0+6 | 预发 + 生产金丝雀（均 shadow） | 12 + 40 | 不变 | 日报 + 分歧抽样复核（100 条/池） | kill switch / mode=off |
| **D7** | D0+7 | 预发（演练目标）+ 生产金丝雀（观察） | 12（演练）+ 40（观察） | 两池不变；演练期间预发临时 kill switch | **kill-switch 回滚演练**：记录 TTR；恢复 shadow | 演练本身即回滚；恢复失败则两池保持 `off` |
| **D8** | D0+8 | 预发 + 生产金丝雀 | 12 + 40 | 预发 shadow；生产仍 shadow | **评审闸门**：§4 全部硬门槛通过且签字后才允许 D9 | 未过门：保持双池 shadow/off，延长观察 |
| **D9** | D0+9 | 生产金丝雀 / `segment=prod-edge-a` | 1 enforce + 39 shadow | 1 台 `mode=enforce`；其余 `mode=shadow` | 进入条件：D8 通过 + D7 TTR ≤5 分钟；24h 观察 | 该 1 台 kill switch → 全 selector `shadow` → `off`；≤5 分钟 |
| **D10** | D0+10 | 生产金丝雀 / `segment=prod-edge-a` | 2 enforce + 38 shadow | 5%（2 台）enforce | D9 连续 24h 全门通过 | 2 台 kill switch → 全 selector `shadow`；≤5 分钟 |
| **D11** | D0+11 | 生产金丝雀 / `segment=prod-edge-a` | 10 enforce + 30 shadow | 25%（10 台）enforce | D10 连续 24h 全门通过 | 10 台 kill switch → 全 selector `shadow`；≤5 分钟 |
| **D12** | D0+12 | 生产金丝雀 / `segment=prod-edge-a` | 20 enforce + 20 shadow | 50%（20 台）enforce | D11 连续 24h 全门通过 | 20 台 kill switch → 全 selector `shadow`；≤5 分钟 |
| **D13** | D0+13 | 生产金丝雀 / `segment=prod-edge-a` | 40 enforce | 100%（40 台）enforce | D12 连续 24h 全门通过；owner 签字 | 全 selector kill switch → `mode=off`；策略内容回退由控制面重签上一已签名 bundle |

> D13 之后仍保持默认 `off` 的可回退性：kill switch 与 `mode=off` 不删除，任何时刻均可按第 5 节执行。

## 3. 每日采集指标

### 3.1 指标口径

| 指标 | 定义 / 口径 | 数据源 | 门槛 |
|---|---|---|---|
| **Bundle fetch 200/304** | 当日 `GET /api/v1/agent/abac-bundle` 的 `200`、`304`、`401`、`403`、`503` 计数；稳态 304 占比 = `304 / (200 + 304)` | 控制面访问日志（按路径聚合） | 稳态 **304 占比 ≥99%**；`200` 只允许出现在策略变更或 TTL 到期；启动后 `401/503` 为 0 |
| **签名校验失败** | agent 日志 `ABAC 策略包验签失败，已丢弃（保留现有缓存）`、`ABAC 策略包解码失败，已丢弃`、`ABAC 策略包超过大小上限，已丢弃` 的条数 | agent 错误日志（按 agentID） | **0 条/日** |
| **决策回传 accepted** | 控制面 `POST /api/v1/agent/abac-decisions` 返回 202 的批次数与条数；DB `abac_decision_logs` 当日新增行数 | 控制面访问日志 + `scripts/abac/soak-report.py` | 与设备决策量一致；无静默缺口 |
| **决策回传 failed** | 同一路径非 202 响应（401/403/413/5xx）计数 | 控制面访问日志 | 仅允许控制面重启窗口内出现，且 5 分钟内恢复；否则 0 |
| **决策回传 dropped** | agent `ABACDecisionReporter.Stats().Dropped / (Accepted + Dropped)` | **当前未导出**；见 §3.2 观测前置项 | **<0.1%**；无法观测即视为门禁未通过（不得越过 D9） |
| **上传滞后 p50/p95** | `TIMESTAMPDIFF(SECOND, occurred_at, received_at)`，按日、按 `mode` 分组（`device` 在线；`device-offline` 离线补传单列） | `abac_decision_logs`；`soak-report.py` | 在线 `mode=device` **p95 ≤30s**（目标亚秒级）；离线行不并入在线门禁；负值单列（时钟漂移） |
| **本地 vs 服务端分歧（fixture）** | 每日重放 32 条共享 fixture（`pkg/abacbundle/testdata/eval_fixtures.json`），逐例比较 `Allowed`/`MatchedPolicyID`/`DenyOverride` | 部署的控制面 + 在线 agent | **0 条差异**（硬门禁） |
| **本地 vs 服务端分歧（live 代理）** | 当日从 `abac_decision_logs` 分层抽样 **100 条**（allow/deny 各半；不足则全取），用任务记录 + 设备属性重建同一请求元组，调用 `POST /api/v1/abac action=evaluate` 重评估；`divergence_proxy = 不一致样本 / 可重建样本`；覆盖率 = 可重建样本 / 抽样样本 | `abac_decision_logs` + 控制面 evaluate API（`abac:read` 服务账号）+ 任务/设备记录 | **不一致 0**；覆盖率 **≥95%**（低于 95% 视为门禁未通过） |
| **ABAC 归因任务失败** | enforce 阶段：任务结果 `stderr` 以 `abac denied:` 开头，或 agent 日志出现对应拒绝；分母 = 同 cohort 当日执行任务数；shadow 阶段的 would-deny **不计失败** | agent 任务结果 + agent 日志 | **<0.1%/日**；且不得出现无法解释的拒绝 |
| **Agent 错误日志** | agent `level=error` 且与 `ABAC`/`bundle`/`decision` 相关的条数；控制面重启等已知窗口单独标注 | agent 日志 | 无法解释的错误 **0**；已知重启窗口需在日报注明 |
| **shadow would-deny 率** | `sum(deny) / (sum(allow) + sum(deny))`（`error` 单列，不并入分母）；按日、按 agent/路径聚合 | `abac_decision_logs`（`reason` 以 `shadow: ` 开头的行）+ shadow 日志 | 记录并趋势观察；关键路径 >1% 时不得晋升，并触发第 2 条全站强制的重新评估前置条件 |
| **Distinct bundle_id** | 当日每 agent 出现的不同 `bundle_id` 数；稳态应为 1（同一策略版本） | `soak-report.py` | 无策略变更时 >1 需解释（ETag/规范化漂移） |
| **上传量/批大小** | 每批 ≤500 条、body ≤1 MiB；413 计数 | 控制面访问日志 | 413 为 0；出现即按回归处理 |

### 3.2 观测前置项（D0 必须完成）

以下两项当前产品未导出，若不补齐则对应门禁无法计算：

1. **Agent 回传 dropped/failed/accepted**：`ABACDecisionReporter.Stats()` 只在进程内，未暴露到日志或 `/metrics`。D0 前需增加**只读观测**（每 5 分钟一条结构化日志，字段 `accepted/failed/dropped/buffered`，或等价的 Prometheus 计数器）；不改回传语义。
2. **Bundle fetch 状态码**：当前无专用指标。用控制面访问日志按路径聚合即可；若网关日志不含状态码，需在反向代理层开启访问日志。

未完成上述任一前置项时：D0–D8 仍可执行（shadow），但 **D9 不得启动**；日报中对应格填 `N/A（观测缺口）` 并在 D8 评审记录为阻塞项。

### 3.3 D0 预检清单

- [ ] 两池设备清单冻结：预发 12 台、生产金丝雀 40 台；记录 `deviceID/hostname/segment/os/arch/agentID/agent 版本/能力` 到变更单。
- [ ] 所有池内 agent 上报 `abac_bundle=v1` 与 `abac_decisions=v1`；不满足者移出池。
- [ ] 控制面 bundle 端点非 503（签发公钥已配置）；决策存储可用（非 503）；保留期 **720h**、清理以 `received_at` 为准。
- [ ] 只读报表账号可执行 `SELECT`（见 `scripts/abac/README.md`）；`mysql` CLI 可用。
- [ ] 基线 24h：池内无 ABAC 归因失败、无签名失败、无 `decision=error`；否则先修基线。
- [ ] 预发 shadow 开启后，确认两池数据库均出现 `abac_decision_logs` 且 `reason` 带 `shadow: ` 前缀；生产池无非 shadow 行（尚未 enforce）。
- [ ] 每台设备具备独立配置下发能力，或已生成 `disabled-ids` 兜底清单；回滚演练脚本/操作人已就位。

## 4. 晋升门禁（每阶段）

### 4.1 通用硬门禁（任一不满足即 HOLD）

对**最近连续 24 小时**统计（D9 起仅统计当前 enforce cohort + 预发对照池）：

1. 签名校验失败 = 0（§3.1）。
2. 决策回传 dropped 率 **<0.1%**；failed 仅出现在已记录的控制面重启窗口且已恢复。
3. 共享 fixture 重放差异 = 0。
4. live 分歧代理：不一致样本 = 0，且覆盖率 ≥95%。
5. ABAC 归因任务失败率 **<0.1%**；无“无法解释的 deny”（每条 deny 必须能对应显式 deny 策略、default-deny 且已有补齐计划，或已声明的离线 fail-closed）。
6. 在线上传滞后 p95 **≤30s**（`mode=device`；离线 `device-offline` 单列，不参与该门禁）。
7. Bundle 304 占比 **≥99%**；无策略变更时无持续 200；401/503 为 0。
8. Agent ABAC 相关错误日志：无法解释的条目 = 0。
9. D7 kill-switch 演练 TTR **≤5 分钟**（D9 前必须满足；之后每阶段回滚动作也以 5 分钟为上限）。

### 4.2 阶段附加条件

| 阶段 | 附加晋升条件 |
|---|---|
| D8 → D9 | D0–D7 每日日报齐全；预发 shadow 至少覆盖 7×24h；D7 演练记录完整；owner 在 D8 评审签字 |
| D9 → D10 | 1 台设备连续 24h 全门通过；该设备无任何 ABAC 归因失败；无新增 deny 类型 |
| D10 → D11 | 2 台设备连续 24h 全门通过；5% 队列的上传/滞后指标与预发同级 |
| D11 → D12 | 10 台设备连续 24h 全门通过；无新增无法解释 deny |
| D12 → D13 | 20 台设备连续 24h 全门通过；owner 对 100% 金丝雀签字 |

### 4.3 失败处理

- 任一硬门禁失败：**当日 HOLD**，不扩 cohort；24h 内修复并复测。
- 同一门禁连续 2 天失败，或出现安全类失败（签名失败、越权放行、无法解释 deny）：立即执行第 5 节回滚，不等待 D8。
- 若 shadow would-deny 在关键路径（`/api/v1/tasks`、`/api/v1/devices`、K8s/CMDB 写入口、自动化执行）≥1%，停止放量，转入“RBAC→ABAC 策略基线”工作；这也是第 2 条 owner 决策的重新评估前提。

## 5. 回滚（每阶段可执行）

### 5.1 回滚杠杆（按优先级）

1. **`--abac-device-kill-switch=true` / `OPSMESH_ABAC_DEVICE_KILL_SWITCH=true`**：一键停用；agent 重配置/重启后按 `off` 处理（不构造 guard，零评估开销）。
2. **`--abac-device-disabled-ids=<id1,id2>` / `OPSMESH_ABAC_DEVICE_DISABLED_IDS`**：单设备豁免，用于隔离异常样本；命中设备始终 bypass。
3. **`--abac-device-mode=off`**：整域关闭，回到引入前行为。
4. **缩小 selector**：把灰度面收敛到指定 `segment/os/arch/hostname/agent_id`，或把 cohort 缩回上一阶段。
5. **策略内容回退**：由控制面重新签发上一已签名 bundle（按 M1 签名/轮换流程）；不在设备侧改版本号。

### 5.2 各阶段回滚动作与验证

| 阶段 | 动作 | 验证（≤5 分钟） |
|---|---|---|
| D0–D8（shadow） | kill switch → `mode=off`；停止日报 | 两池 agent 不再产生新的 `abac_decision_logs` 行；任务执行无变化；shadow 从未阻断，业务无感 |
| D9–D13（enforce） | 先对当前 enforce cohort 置 kill switch；仍异常则全 selector `mode=off` 或缩小 selector | ① cohort 不再出现 `abac denied:`；② 任务恢复执行；③ 2 个上报周期（约 60s）后无该 cohort 的新决策行；④ TTR 记录进日报 |
| 全阶段（策略错误） | 控制面重签上一 bundle；agent 侧只接受已签名包 | 设备 `bundle_id` 回到上一版本；审计链不断档；决策结果恢复到基线 |

**TTR 定义**：从回滚指令下发（T0）到“目标 cohort 不再执行 enforce、任务恢复正常”的最后一台设备（T1）；`TTR = T1 - T0`，目标 ≤5 分钟。配置下发与 agent 重启耗时必须计入。

### 5.3 D7 kill-switch 演练（预发）

1. T0：对预发 12 台下发 `kill-switch=true`（或等价配置），执行 agent 重配置/重启。
2. 观察：所有设备不再构造 DeviceGuard；无新增 `abac_decision_logs`；预发任务正常执行；无 `abac denied:`。
3. T1：最后一台设备确认恢复正常，记录逐台恢复时间与 `TTR = max(T1_i - T0)`。
4. 恢复：T2 置回 `kill-switch=false`、`mode=shadow`；确认 shadow 决策恢复写入（`reason` 前缀 `shadow: `）。
5. 次日日报记录：TTR、失败设备、异常日志。TTR >5 分钟即视为 D8 门禁失败。

## 6. 日报与签字

### 6.1 日报模板（每日 09:00 UTC）

文件名建议 `docs/abac-soak/<YYYY-MM-DD>-D<n>.md`（本计划不预创建）：

```markdown
# ABAC M4 Soak 日报 — D<n> — <YYYY-MM-DD>

- 阶段/池：<D0-D8 staging shadow | D9-D13 prod-edge-a enforce>
- cohort：<n>/40 enforce，<m> shadow；agent 构建：<version/commit>
- 统计窗口：<UTC start> – <UTC end>；数据源：soak-report.py + 控制面访问日志 + agent 日志

| 指标 | 当日值 | 门槛 | 结论 |
|---|---|---|---|
| Bundle 304 占比 |  | ≥99% | PASS/HOLD/FAIL |
| Bundle 200/401/503 计数 |  | 200 仅在变更；401/503=0 |  |
| 签名校验失败 |  | 0 |  |
| Decision accepted / failed |  | failed 仅重启窗口 |  |
| Decision dropped 率 |  | <0.1% |  |
| 上传滞后 p95（在线） |  | ≤30s |  |
| 上传滞后 p95（离线，参考） |  | 单列 |  |
| Fixture 分歧 |  | 0 |  |
| Live 分歧（100 样本）/覆盖率 |  | 0 / ≥95% |  |
| ABAC 归因任务失败率 |  | <0.1% |  |
| 无法解释的 deny |  | 0 |  |
| ABAC 相关 error 日志 |  | 无法解释=0 |  |
| 回滚演练 TTR（D7） |  | ≤5 分钟 |  |

## 事件与处置
- <无 / 事件、时间、影响、处置、回滚与否>

## 签字
- 值班 SRE（记录/初签）：
- ABAC 模块 owner（复核）：
- 项目 owner（D8 / D13 必签）：
```

### 6.2 签字责任

| 角色 | 职责 |
|---|---|
| 值班 SRE | 每日采集、填表、异常首响应；有权直接执行 kill switch，事后 24h 内补记录 |
| ABAC 模块 owner | 复核指标口径、判定 PASS/HOLD/FAIL；确认分歧样本与 deny 解释 |
| 项目 owner（章程负责人，当前待定） | D8 晋升评审与 D13 全量金丝雀签字；决定延长观察或回滚 |
| 安全评审（按需） | 任何“无法解释的 deny”、越权放行或签名异常必须介入复核 |

## 7. 报表工具

- `scripts/abac/soak-report.py`：只读报表，stdout 输出 Markdown，可 `--out` 落盘；连接方式见 `scripts/abac/README.md`。
- 输出内容：按日/按 mode 的 allow/deny 计数、按日/agent/mode 的明细、上传滞后 p50/p95（`TIMESTAMPDIFF`）、distinct bundle_id、error-ish reason Top N、shadow 行与 enforce 行区分；可选抓取控制面 `/metrics` 中的 ABAC 计数器。
- 脚本**只执行 SELECT**，不写数据库；无 `mysql` CLI 时明确报错并以退出码 2 结束。
- 报表不替代第 3 节的日志侧指标（200/304、签名失败、任务失败归因）；日报需合并两边数据。

## 8. 退出/结束条件

- **成功**：D13 完成且连续 24h 全门通过；owner 签字；默认值仍保持 `off`（放量通过配置显式开启），kill switch 保留。
- **部分成功**：停在某一百分比且稳定；记录未完成阶段与原因；不自动继续。
- **回滚**：任一安全门禁失败或 owner 决定；所有 cohort 回到 `shadow`/`off`；保留审计与日报；重新评估前不得再次放量。
- **全站强制**：不在本计划的退出条件内；第 2 条 owner 决策的两个前提未满足前，不重新评估。
