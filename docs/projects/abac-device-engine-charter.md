# ABAC 设备侧决策引擎 项目章程

> **状态：已立项（2026-11-17，owner 批准；M1 已完成（2026-11-17），M2 已完成（2026-11-17），M3 已完成（2026-11-17），M4 待启动）**
> **负责人：待定**
> **日期：2026-11-17**
> **依据**：`internal/controlplane/abac_ops.go`、`internal/controlplane/abac_engine.go`、`docs/opsmesh-authz-audit-2026-11-04.md`、`docs/projects-2026-11-05.md`
> **代码基准**：`main@9c828b8f`（本文只读核对，未改代码、未提交）

---

## 1. 一句话定义

在服务端 ABAC 评估器作为**唯一权威语义源**的前提下，为设备侧增加“签名策略包 + 本地评估 + 决策审计回传”的离线决策能力；它**不替代 RBAC 强制层**，也不改变“未实现=显式 501，不造假数据”的诚实原则。

## 2. 背景与问题证据

### 2.1 当前口径：设备脚本路径显式 501

- `internal/controlplane/abac_ops.go:43-48` 记载 owner 已裁口径：**“未实现=显式 501，不造假数据”**（与 AI Agent 工具同先例）。设备脚本路径的 `evaluate` 返回 `501 Not Implemented`，错误文案明确指向服务端评估器。
- 该分支由安全守卫钉住：`internal/controlplane/abac_ops_security_test.go:85` `TestBuildABACScript_EvaluateIsExplicit501`——防止“假 ALLOWED”回归（旧实现曾硬编码 `Result: ALLOWED (default policy)`）。

### 2.2 服务端评估器已是今天的权威引擎

| 事实 | 证据 |
|---|---|
| 评估语义：`deny-overrides` + `default-deny` | `internal/controlplane/abac_engine.go:76-105`；文件头声明 `abac_engine.go:5-12` |
| 匹配模型：tenant / subject / resource / action，支持 `*` 与 `prefix*` | `abac_engine.go:21-41` |
| 条件：context 为 JSON 对象，逐键等值；坏 JSON 视为不匹配（安全侧） | `abac_engine.go:43-74` |
| API 入口：`POST /api/v1/abac` 的 `evaluate` 走本地评估，不再下发脚本 | `abac_ops.go:76-93` |
| 评估器唯一调用点就是该 API（未接入请求强制链） | 本次 `rg 'evaluateABAC\('` 仅命中 `abac_ops.go:82` 与测试 |
| 语义用例：默认拒绝 / 允许匹配 / deny 覆盖 / 条件 / 注册表 | `abac_engine_test.go:12`、`:26`、`:39`、`:52`、`:69`（共 5 条） |
| 安全用例：501 守卫、脚本注入、参数拒绝 | `abac_ops_security_test.go:55`、`:85`、`:98`（共 3 条） |

### 2.3 作为“权威源”的成熟度缺口

1. **策略镜像不持久化**：`server.go:249-251` 的 `abacPolicies` 只是进程内 slice；控制面重启后策略全部丢失，评估器会退化为 `default-deny`。设备侧引擎若以它为权威源，必须先补持久化。
2. **deny 策略无法经 API 创建**：`abac_ops.go:112-121` 的 `policy-create` 把 `Effect` 硬编码为 `"allow"`；`deny-overrides` 只在测试里通过 `addABACPolicy` 直接构造（`abac_engine_test.go:41-42`）。真实产品路径上“deny 优先”未被行使。
3. **设备侧策略文件是“只写不读”**：`abac_ops.go:37` 仍通过 shell 往 `/etc/opsmesh/abac/policies.json` 追加 JSON；本次 `rg 'abac|policies\.json' internal/agent` **0 命中**——agent 没有读取/评估实现。今天的设备侧没有决策引擎，只有一份无人消费的文件。
4. **ABAC API 缺专用权限点**：`evaluate` 分支在返回前没有任何权限点（`abac_ops.go:76-93`）；`policy-create` 最终经 `dispatchKafkaTask` 受 `task:write` 约束（`kafka_ops.go:37-38`）。生产模式有 `defaultDenyMiddleware` 兜住身份（`server_lifecycle.go:518`），但 `evaluate` 的权限语义（是否应要 `abac:read`）**待复核**。
5. **签名/版本/回滚/审计回传均不存在**：全仓未发现策略包（bundle）的签名、版本、回滚或设备决策审计上传实现；可复用的只有许可证的 Ed25519 形状（`internal/config/license.go:10-22`、`:65-117`）。

### 2.4 组织与审计上下文

- **RBAC 是唯一强制层**：`internal/store/model/perm.go:261-265` 明确“本平面的授权模型只有角色矩阵一份”；`docs/projects-2026-11-05.md:49` 也记载“不引入归属/ABAC 授权。仓库已有裁定：角色矩阵是唯一授权模型”。
- **鉴权审计**：控制面约 **42%** 可派发路由曾无权限校验（`docs/opsmesh-authz-audit-2026-11-04.md:6`），受影响 **223** 条路径（同文件 `:27`），已通过公共派发入口收口（`:472-499`）；后续建议明确“细粒度权限应另立项”（`:515-525`）。
- **ABAC 真实评估引擎**于审计整改批次 17 落地（批次 15–19 的 DONE 回执见 `docs/CLAIMS.md:1228`，2026-11-14）。

## 3. 目标

| # | 目标 | 可验证口径 |
|---|---|---|
| G1 | **可验证的策略包** | 版本化、规范化序列化、Ed25519 签名；设备验签失败或版本回退即整包拒绝 |
| G2 | **决策语义与服务端一致** | 同一 fixture 套件上，设备端与服务端逐条结果相同（含 deny-overrides、default-deny、条件、通配/前缀） |
| G3 | **本地缓存可回滚** | A/B 双槽 + last-known-good；切换原子化；可回退到上一版本 |
| G4 | **决策可审计** | 每条决策带策略包版本、匹配策略、离线标记；离线缓冲、重连补传、幂等去重 |
| G5 | **灰度可控** | 按设备标签选择；1%→10%→100% 分批；单项回滚 ≤5 分钟 |
| G6 | **决策门先行** | 立项决策已于 2026-11-17 获批（撤销“设备端不实现”立场，保留“禁止脚本路径伪造判定”的函数级 501 守卫）；后续里程碑按 §10 的状态表推进，语义/密钥范围变更仍需 owner 拍板 |

## 4. 非目标

- **不做 XACML 全规范**：只实现最小 JSON 策略集与 `deny-overrides` + `default-deny`；不做 obligations/advice、复杂数据类型、策略组合算法全集。
- **不支持设备端策略编辑**：设备是只读消费者；策略编写只在控制面进行。
- **不替代 RBAC 强制层**：ABAC 的 `allow` 绝不单独放行请求；RBAC 仍是最终门（`perm.go:261-265`）。
- **不在本期把 ABAC 接入请求强制链**：先做“决策服务 + 审计”，强制接线另立项目并单独做越权评审。
- 不新增策略 DSL/前端编辑器；沿用现有 subject/resource/action/condition 形状。
- 不做跨设备策略合并/冲突协商（冲突语义固定为 deny-overrides）。

## 5. 工作分解与里程碑

| 阶段 | 交付物 | 退出判据 | 估算 |
|---|---|---|---|
| **M1 策略包格式与签名** | 版本化 JSON schema、规范化序列化、Ed25519 签名/验签、bundle 分发端点、密钥轮换与吊销 | 篡改 payload/签名/版本 ⇒ 设备整包拒绝；golden vectors 稳定；同一策略集签名可复现 | 4–6 人日 |
| **M2 设备端评估器（最小 JSON 策略集）** | agent 侧只读评估器：精确/`*`/`prefix*`、条件等值、deny-overrides、default-deny；与服务端共享 fixture | 共享套件逐条一致；≥10,000 条属性测试 0 不一致；策略 ≤1,000 条时单次评估 P99 ≤10ms（参考设备实测，待复核） | 8–12 人日 |
| **M3 决策审计回传** | 决策记录 schema、本地环形缓冲、批量上传、幂等键、保留策略、离线标记 | 断网 24h 后重连：决策不丢、重复上传可去重；审计记录含 bundle 版本与 `offline=true` | 5–8 人日 |
| **M4 灰度（按设备标签）与回滚** | 标签选择器、1%→10%→100% 分批、版本钉定、一键回滚、漂移告警 | 单批回滚 ≤5 分钟；灰度期间决策差异率 0；回滚后设备回到上一 bundle 且审计连续 | 4–6 人日 |

> **M0（前置，不单独计里程碑，已收口）**：owner 已于 2026-11-17 批准立项（撤销“设备端不实现”立场）；策略镜像持久化（批次47，迁移 035）与 `abac:read`/`abac:write` 权限点、评估 API 鉴权收口（批次45）均已落地。剩余 M0 项不再阻塞 M1。

### 5.1 里程碑状态

| 里程碑 | 状态 | 入口/说明 |
|---|---|---|
| **M1** 策略包格式与签名 | **已完成（2026-11-17）** | 规格：[`abac-device-engine-m1-bundle-spec.md`](./abac-device-engine-m1-bundle-spec.md)。证据：`pkg/abacbundle/`（`CanonicalJSON`/`Digest`/`Sign`/`Verify`/`KeySet`，限额与哨兵错误；`go test ./pkg/abacbundle/`）、`internal/controlplane/abac_bundle_endpoint.go`（`GET /api/v1/agent/abac-bundle`，per-agent HMAC、ETag/304、租户隔离、无钥 503；`abac_bundle_endpoint_test.go`）、`internal/agent/abac_bundle_client.go`（HMAC 拉取 + 验签 + 原子 LKG 缓存 0600 + `abac_bundle=v1` 能力上报；`abac_bundle_client_test.go`） |
| **M2** 设备端评估器 | **已完成（2026-11-17）** | 证据：`pkg/abacbundle/eval.go`（`Evaluate`/`EvaluateBundle`，deny-overrides + default-deny + 条件等值 + 通配/前缀 + 按 ID 字典序 tie-break）、共享 fixture `pkg/abacbundle/testdata/eval_fixtures.json`（**32 条**）由 `pkg/abacbundle/eval_test.go` 与 `internal/controlplane/abac_eval_parity_test.go` 逐例断言两端一致；`internal/agent/abac_local_eval.go` 的 `EvaluateLocal` 每次调用以当前 `KeySet` 重新验签、过期仅标记 `BundleExpired`；不接入请求闸门 |
| **M3** 决策审计回传 | **已完成（2026-11-17）** | 证据：控制面 `POST /api/v1/agent/abac-decisions`（per-agent HMAC + body digest、≤500 条 / ≤1 MiB、租户取验签身份；`internal/controlplane/abac_decisions_endpoint.go` + `abac_decisions_endpoint_test.go`）、`abac_decision_logs`（迁移 036 + 租户/agent 时间索引 + 保留期清理）、`model.ABACDecisionStore` 三实现（Memory/SQL/MultiSchema）、agent `ABACDecisionReporter`（500 条环形缓冲 / 30s 刷新 / 5xx 重试 / 413 折半 / `abac_decisions=v1`）。保留期默认 **720h**，清理以 **received_at** 为准 |
| **M4** 灰度与回滚 | 待启动 | 按设备标签灰度 + 回滚（1%→10%→100%，单批回滚 ≤5 分钟）；依赖 M3 的审计连续性；回滚指令格式另行评审 |

## 6. 验收标准（DoD）

| # | 判据 | 执行方式 |
|---|---|---|
| 1 | 服务端既有语义零回归 | `go test ./internal/controlplane -run TestEvaluateABAC -count=1`；`go test ./internal/controlplane -run TestBuildABACScript -count=1` |
| 2 | 服务端/设备端决策一致 | 共享 fixture 套件（含 default-deny、allow、deny-overrides、条件、通配/前缀、租户隔离、坏 JSON context）逐条比对，0 差异；属性测试 ≥10,000 随机策略/请求对，0 差异 |
| 3 | 篡改包被拒 | 修改 payload、签名或版本号任一字节 ⇒ 设备拒绝加载，保留 last-known-good，并产生 `bundle_rejected` 审计 |
| 4 | 回滚保护 | 低于当前版本的 bundle 默认拒绝；仅接受带签名的显式回滚指令 |
| 5 | 离线行为 | 控制面不可达 ⇒ 设备用 last-known-good 继续决策；每条决策带 `offline=true`；重连后补传成功且仅一次 |
| 6 | 审计完整性 | 测试中离线产生 N 条决策 ⇒ 重连后收到 N 条（幂等键去重后）；生产目标 ≥99.9% 在 5 分钟内可见 |
| 7 | 灰度/回滚 | 1% 设备批次决策差异 0；执行回滚 ≤5 分钟；回滚后审计链不断档 |
| 8 | RBAC 不被绕过 | ABAC `allow` 单独存在时，受 `requireProd` 保护的路由仍按 RBAC 拒绝；相关角色矩阵用例全绿 |
| 9 | 密钥边界 | 设备只持有验签公钥；私钥不出现在 agent 包、设备文件系统、日志与审计中 |
| 10 | 竞态 | `go test -race ./internal/controlplane ./internal/agent` 无告警 |

## 7. 依赖与前置条件（含 owner 决策项）

| 依赖/决策 | 状态 | 说明 |
|---|---|---|
| **owner 决策 D1：设备侧评估立项** | **已批准（2026-11-17）** | 决策原文：`2026-11-17 用户批准立项，撤销“设备端不实现”的 501 立场；501 仅保留为“禁止脚本路径伪造判定”的函数级守卫。` 范围：只撤销“设备端评估不实现”，**不撤销**“禁止造假判定”守卫；服务端评估仍是权威默认路径。 |
| **owner 决策 D2：设备侧决策定位** | **待定** | 建议先做“咨询/审计”（advisory），不直接阻断设备操作；强制模式另行评审。注意：**服务端**定向强制（批次53 的 `--abac-enforce`，默认关闭、按路由白名单 opt-in）是独立开关，不属于 D2，也不改变设备侧定位。 |
| **owner 决策 D3：签名私钥归属与轮换** | **待定** | 私钥存放、轮换周期、吊销流程需 owner 指定责任方；M1 先支持“配置提供密钥 + 双公钥轮换”，生产密钥托管仍依赖 D3 |
| 服务端策略镜像持久化 | **已落地**（批次47） | 迁移 035 + 启动装载 + 跨副本刷新；`abacPolicies` 降为评估缓存 |
| deny 策略创建能力 | 待补 | `policy-create` 仍把 `Effect` 硬编码为 `allow`（`abac_ops.go:229`）；需支持显式 `effect` 并做权限校验 |
| `abac:read` / `abac:write` 权限点 | **已落地**（批次45） | 入口统一 `subsystemAuthorize`；`/api/v1/abac` 读写分离 |
| 设备决策审计端点鉴权与租户隔离 | 待补 | 复用 `subsystemAuthorize`/租户隔离，不接受匿名或跨租户写入 |
| agent 版本门控与发布通道 | 已有基础 | 设备侧评估器需最低 agent 版本；旧 agent 明确回退服务端评估 |
| 策略包/审计存储 | 待定 | 需要迁移（编号落地时续）；bundle 与审计分表，带 tenant/device 索引 |
| 共享参考实现/测试向量 | **设计约束** | 服务端与设备端不得各写一套语义；至少共享 fixture 与属性测试生成器，避免漂移 |
| Ed25519 签名先例 | 已有 | `internal/config/license.go:10-22`、`:65-117` 的 payload.signature 形状可复用（不直接复用代码） |

## 8. 风险与缓解（含回滚）

| 风险 | 影响 | 缓解 | 回滚 |
|---|---|---|---|
| 服务端/设备端语义漂移 | 同一请求两套结论，审计不可信 | 共享 fixture + 属性测试 + CI 一致性门；语义变更必须同批更新向量。**现状记录**：服务端 `Effect` 非 `"deny"` 一律视为允许候选（含空串/未知值，历史口径），设备端为保持一致照此实现；若未来收紧为仅 `"allow"`，必须同步两端评估器并更新 `eval_fixtures.json` | 停用设备评估，回退服务端决策 |
| 策略包供应链攻击 | 设备执行伪造策略 | Ed25519 验签、版本单调、摘要钉定、拒绝未签名包；私钥隔离 | 回滚到上一已签名 bundle；吊销密钥并重签 |
| 离线策略过期 | 设备按旧策略放行/拒绝 | last-known-good + TTL + `offline=true` 显式标记；高风险变更要求在线确认（D2） | 强制上线刷新；必要时禁用设备评估 |
| 设备资源受限 | 评估拖慢 agent | bundle 大小/策略条数/评估时延预算；基准门禁；超限拒绝加载 | 回退上一 bundle 或服务端评估 |
| 审计数据量过大 | 存储/带宽压力 | 本地环形缓冲 + 批量 + 背压；低风险决策可采样（需 owner 批准） | 降采样/延长保留期；保留高风险全量 |
| 回滚失败 | 设备停在坏策略 | A/B 双槽 + 原子切换 + 切换前自检；保留上一版本 | 直接切回 A 槽；最终回退服务端评估 |
| ABAC 被误当强制层 | 越权风险 | 文档与测试明确“ABAC allow ≠ RBAC allow”；强制接线另立项 | 关闭 ABAC 决策服务，RBAC 行为不变 |
| 设备时钟不可信 | 审计时间线错乱 | 单调计数器 + 控制面接收时间双时间戳；时钟漂移告警 | 以控制面时间为准重排 |

## 9. 工作量估算

| 范围 | 人日 |
|---|---|
| M0 前置（持久化/权限点/API 鉴权） | 已完成（批次45/47，计入原估算 2–3） |
| M1 策略包格式与签名 | 4–6 |
| M2 设备端评估器 | 8–12 |
| M3 决策审计回传 | 5–8 |
| M4 灰度与回滚 | 4–6 |
| 安全评审与红队（穿插） | 2 |
| **合计** | **25–37** |

**关键路径**：`M1 → M2 → M3 → M4`。M1 的 schema 与签名字段一旦定稿，后续所有 fixture、审计记录、灰度元数据都依赖它——必须先冻结。

## 10. 状态与决策记录

- **当前状态：已立项**（2026-11-17，owner 批准）；**负责人：待定**；**M1 已完成（2026-11-17），M2 已完成（2026-11-17），M3 已完成（2026-11-17），M4 待启动**。
- **owner 决策原文（2026-11-17，逐字记录）**：

  > 2026-11-17 用户批准立项，撤销“设备端不实现”的 501 立场；501 仅保留为“禁止脚本路径伪造判定”的函数级守卫。

- **决策范围（解释，不扩大原文）**：
  - **撤销**：设备侧评估“永不实现”（即“设备端不实现”的 501 立场）。设备侧签名策略包 + 本地评估自此进入实施范围，按 §5.1 里程碑推进。
  - **不撤销**：`buildABACScript` 脚本通道的 501 守卫。它阻止通过设备脚本路径**伪造 ALLOWED 判定**，由 `TestBuildABACScript_EvaluateIsExplicit501` 钉住；守卫文案与断言保持不变。
  - **权威路径不变**：服务端 ABAC 评估器（`POST /api/v1/abac`）仍是默认权威决策源；设备侧判定在 M2 落地前不产生，落地后仍按 D2 保持 advisory，直到另行批准 enforcement。
  - **未获批准的部分**：D2（**设备侧** advisory → enforcement）与 D3（生产私钥托管/轮换责任方）仍待定；M1 只用配置提供的密钥打通格式与验签，不改变密钥归属。服务端定向强制（批次53，`--abac-enforce`，默认关闭）是独立 opt-in 开关，不属于 D2，也不等于全局闸门。

- **回退路径**：任一阶段出问题 ⇒ 关闭设备侧评估，保留 501 函数级守卫与服务端权威评估器；不产生设备侧策略包、不产生设备决策审计。

### 10.1 里程碑状态

| 里程碑 | 状态 | 说明 |
|---|---|---|
| **M1** 策略包格式与签名 | **已完成（2026-11-17）** | 规格 `docs/projects/abac-device-engine-m1-bundle-spec.md`（bundle v1）；实现 `pkg/abacbundle/` + `internal/controlplane/abac_bundle_endpoint.go` + `internal/agent/abac_bundle_client.go`；M2 待启动 |
| **M2** 设备端评估器 | **已完成（2026-11-17）** | 设备侧 `pkg/abacbundle.Evaluate` 与服务端 `evaluateABAC` 同语义；32 条共享 fixture 两端逐例一致（`eval_test.go` + `abac_eval_parity_test.go`）；`EvaluateLocal` 每次调用重新验签、过期仅标记；未接入请求闸门 |
| **M3** 决策审计回传 | **已完成（2026-11-17）** | `POST /api/v1/agent/abac-decisions` + `abac_decision_logs`（迁移 036）+ `ABACDecisionStore` 三实现 + agent 环形缓冲/重试；保留期默认 720h、按 received_at 清理 |
| **M4** 灰度与回滚 | 待启动 | 按设备标签灰度 + 回滚；等待 M3 审计连续性（已就绪） |

### 10.2 决策日志

| 日期 | 决策/事件 | 记录 |
|---|---|---|
| 未标注（历史口径） | 设备脚本路径 `evaluate` 显式 501；“未实现=显式 501，不造假数据” | `abac_ops.go:43-48`；守卫 `abac_ops_security_test.go:85` |
| 2026-11-04 | 鉴权审计：42% 派发路由无权限校验；223 条路径收口 | `docs/opsmesh-authz-audit-2026-11-04.md:6`、`:27`、`:472-499` |
| 2026-11-05 | 仓库裁定：角色矩阵是唯一授权模型，不引入 ABAC 授权 | `docs/projects-2026-11-05.md:49`；`perm.go:261-265` |
| 2026-11-14 | 审计整改批次 15–19 DONE；其中批次 17 落地 ABAC 真实评估引擎 | `docs/CLAIMS.md:1228` |
| 2026-11-17 | 本草程起草：设备侧引擎待批准；启动需先反转 501 | 本文件（初稿） |
| **2026-11-17** | **owner 批准立项：撤销“设备端不实现”的 501 立场；501 仅保留为“禁止脚本路径伪造判定”的函数级守卫** | 本节逐字记录；状态改为“已立项”，M1 启动 |
| **2026-11-17** | **M1 完成（bundle v1 签名库/签发端点/Agent LKG）；M2 完成（设备端评估器 + 32 条共享 fixture 两端逐例一致）；M3 完成（决策审计回传 + 保留期 + Agent 缓冲）** | §5.1 / §10.1 状态表；`CHANGELOG.md` 批次54/55/56 |

---

*起草：2026-11-17。只读核对基准 `main@9c828b8f`；未改代码、未提交。*

## 进展记录（2026-11-17）

本节记录设备脚本路径 `evaluate` 501 口径复核后的落地进展。**D1 已于 2026-11-17 批准（见 §10），本草程状态为“已立项（M1 已完成（2026-11-17），M2 已完成（2026-11-17），M3 已完成（2026-11-17），M4 待启动）”；D2/D3 仍待定。**

- **501 守卫保留（不反转）**：`buildABACScript` 的 `evaluate` 分支继续显式返回 501（`internal/controlplane/abac_ops.go:40-52`）。理由：该分支是**函数级守卫**，用于阻止设备脚本路径伪造 ALLOWED 判定；HTTP `POST /api/v1/abac action=evaluate` 早已由服务端真实引擎决策（200），守卫只覆盖不可能产出判定的脚本通道。文案改述为 “device-script evaluation is intentionally disabled; the HTTP evaluate path is served by the server-side engine with device-context enrichment”，由 `TestBuildABACScript_EvaluateIsExplicit501`（`internal/controlplane/abac_ops_security_test.go:91`）钉住 501 与关键措辞。
- **设备上下文增强已落地**：HTTP evaluate 带 `deviceID` 时，先查设备（不存在 404；跨租户 403，与不存在同形不泄露存在性），再把设备属性（`deviceID/hostname/segment/os/arch/state/taskState/agentID/managed`，空字符串跳过）注入策略上下文（`internal/controlplane/abac_ops.go:102-170`，属性展开见 `abac_ops.go:58-78`）。合并规则：**调用方自带键优先**，设备属性只补空缺，绝不覆盖调用方显式上下文；顶层扁平键供现有等值条件匹配，另附嵌套 `device` 对象副本（调用方已提供 `device` 时同样不覆盖）。响应新增 `evaluatedBy`（`server` / `server+device-context`）与 `deviceContext`（实际注入子集）供审计。用例：`abac_ops_security_test.go:163`（同租户注入并命中）、`:197`（跨租户 403 无决策字段）、`:214`（未知设备 404）、`:227`（调用方键不被覆盖）、`:256`（非法 context JSON 400）、`:266`（无 deviceID 时 `evaluatedBy=server`）。
- **仍未完成（承接第 2.3 节缺口）**：
  - 策略镜像跨重启持久化（当前仍为进程内 `abacPolicies`，重启即丢 → default-deny）；
  - 真正的设备侧离线决策引擎（含与服务端共享 fixture 的一致性门）；
  - **强制门接线（enforcement gate）**：ABAC `allow` 目前不参与请求放行，接强制链需 owner 决策 D2 另行评审；
  - 策略包签名 / 版本化 / 回滚（M1；D1 已批准，D3 密钥托管待定）。
- **路由鉴权缺口已发现并修复（2026-11-17，批次45）**：复核发现 `POST /api/v1/abac` 此前在**所有模式**都无入口鉴权——生产仅由 `defaultDenyMiddleware` 兜底“校验”且不回注请求 context，handler 内 `authctx.FromContext` 恒零值（`internal/controlplane/abac_ops.go:87` 为旧入口）；后果是同租户带 `deviceID` 的 evaluate 被误判 403、`policy-create` 落空租户镜像被当作全局策略、策略镜像键 `action|subject|resource` 跨租户互相覆盖、非生产模式匿名穿透。修复：① 入口统一走 `subsystemAuthorize`（`requireTenantContext` + `requireProd`，`abac_ops.go:122`；回调见 `internal/controlplane/auth.go:89`），租户真值只取已验签身份；② 新增 `abac:read`/`abac:write` 权限点（`internal/store/model/perm.go:216`），read 进 `operatorReadOnlyGroups`，write 仅 admin；③ 镜像键改 `tenantID|action|subject|resource`（`abacPolicyID`，`abac_ops.go:82`）；④ 链式回归 `internal/controlplane/abac_ops_chain_test.go`（匿名拒绝、跨租户 403、读写分离、镜像键租户隔离）。**其余未完成项不变**（见上一条列表：持久化 / 设备端引擎 / 强制门 / 策略包签名）。
- **策略持久化已落地（2026-11-17，批次47，取代上一条“仍未完成”列表中的第一项）**：store 提为事实源、`abacPolicies` 降为评估缓存。新增 `model.ABACPolicyStore` / `ABACPolicyRecord`（`internal/store/model/abac_policy.go:14`）与 Memory / SQL / MultiSchema 三实现（`internal/store/memory/memory_abac_policy.go:15`、`internal/store/sqlstore/sql_abac_policy.go:31`、`internal/store/multischema/multi_schema_abac_policy.go:13`），迁移 `internal/store/sqlstore/migrations/035_abac_policies.sql`（主键 `(tenant_id, id)`，`version` 每次 upsert 自增）；启动全量装载 `initABACPoliciesFromStore`（`internal/controlplane/abac_persistence.go:85`，调用点 `internal/controlplane/server.go:538`）；policy-create/delete 写穿且落库失败即 500、不改缓存、不下发脚本（`internal/controlplane/abac_engine.go:118`、`:158`，handler 见 `abac_ops.go:208`、`:223`）；多副本 5s 按 `(tenant,id)->version` 比对刷新（`abac_persistence.go:118`，接线 `internal/controlplane/server_lifecycle.go:594`），未变即跳过、`abacRev` 防本地写覆盖；租户隔离贯穿键/查询/删除。回归 `internal/store/memory/memory_abac_policy_test.go`、`internal/controlplane/abac_persistence_test.go`。
- **剩余待办（批次47 后口径）**：① shadow 模式（策略只观察不强制，需 owner 确认采样与审计口径）；② 强制闸门接线（enforcement gate，D2 owner 决策，ABAC allow 仍不等于 RBAC allow）；③ 设备侧离线决策引擎（M1/M2，签名策略包 + 本地评估 + 一致性门）；④ 策略包签名 / 版本化 / 回滚（M1；D1 已批准，D3 密钥托管待定）。
- **ABAC 影子模式已落地（2026-11-17，批次50，取代上一条待办①）**：新增 `--abac-shadow` / env `OPSMESH_ABAC_SHADOW`（默认 false，`internal/config/config.go:494-498`、`internal/config/config.go:736`、装配 `internal/config/config.go:954`）。启用后 `abacShadowMiddleware`（`internal/controlplane/abac_shadow.go:80`）挂在 `defaultDenyMiddleware` 之内、`bodyLimitMiddleware`/mux 之外（`internal/controlplane/server_lifecycle.go:519`）：production 下身份已过「头 ↔ token」交叉校验，未认证请求不会污染 would-deny；限流/CSRF 已提前拒绝的请求不消耗评估；中间件不读响应、不写 ResponseWriter，结构上不可能改写状态码。观察面仅 POST/PUT/DELETE/PATCH 且路径以 `/api/v1/` 开头，显式跳过 `/api/v1/abac`（含子路径）、`/api/v1/federation/`、`/healthz`、`/readyz`、`/metrics`、`/api/v1/events/stream`（`abac_shadow.go:97`）。输入取自已建立身份：subject `{"userID","roles","tenantID"}`、resource `{"path","method"}`、actionAttr=method、context 仅 `{"remoteAddr","userAgent"}`（不采集 Cookie/Authorization 等秘密头）；身份无租户则 debug 跳过、不猜测（`abac_shadow.go:222`）。评估调用包在 `defer recover()`（`abac_shadow.go:205`），panic 记 `decision=error` 后照常进入 handler；决策经结构化日志 `abac shadow decision`（字段 shadow/decision/matchedPolicyID/reason/method/path/tenantID/userID/durationMs，`abac_shadow.go:119`）与 Prometheus 计数器 `opsmesh_abac_shadow_decisions_total{decision,method}`（`internal/metrics/abac_shadow.go:23`，注册表字段 `internal/metrics/metrics.go:113`）双路落盘。回归见 `internal/controlplane/abac_shadow_test.go:61`（default-deny 不阻断/响应逐字不变、关闭不评估、GET 与排除路径跳过、评估器 panic 不影响响应、缺租户跳过）。
  - **观测指标（晋升前必看）**：① would-deny 率 = `sum(rate(opsmesh_abac_shadow_decisions_total{decision="deny"}[1d])) / sum(rate(opsmesh_abac_shadow_decisions_total{decision=~"allow|deny"}[1d]))`（error 单列，不并入分母）；② top would-deny 路径：指标刻意不带 path 标签（路径基数由请求方控制），按结构化日志 `path` 字段聚合；③ 分歧（divergence）：shadow deny 与同一路径上实际 RBAC 403/审计 `authz_denied` 的差异——shadow deny 而 RBAC 放行 = ABAC 覆盖过严（需确认是否策略缺口），shadow allow 而 RBAC 拒绝 = 预期内（RBAC 仍是唯一强制层），不构成晋升阻塞；④ `decision="error"` 必须长期为 0（评估器 panic 属 ABAC 数据缺陷）。
  - **晋升为 enforcement 的门槛（建议，仍需 D2 owner 决策）**：观察期 **2–4 周**且覆盖至少一个完整发布窗口；关键变更路径（`/api/v1/tasks`、`/api/v1/devices`、K8s/CMDB 写入口、自动化执行）would-deny 率 **< 1%**；**零无法解释的 deny**（每条 deny 要么命中显式 deny 策略，要么确认为 default-deny 且有补齐策略计划）；`error` 计数为 0；灰度按域推进（先低风险只读邻近域，再核心变更域），enforce 首日保留 kill switch（关闭 `--abac-shadow` 并回滚 RBAC-only 行为）。
  - **日志量考量**：每次被观察的变更请求输出一条 info 决策日志（无采样）。高写入负载（如 >50 条/s）下建议先在采集侧按 `decision="deny"` 与 top path 聚合/降采样，再评估是否给 shadow 日志加采样开关；当前实现按需求保持逐决策可审计，不引入采样丢弃。
- **影子模式实测数据（2026-11-17，批次52）**：全文 `docs/abac-shadow-simulation-2026-11-17.md`。demo 控制面（`--abac-shadow --store=memory`，无 MySQL/Redis/Kafka）75.9s 内 **690 次变更请求**，allow 300（**43.48%**）/ would-deny 390（**56.52%**）/ **error 0**；150 次 GET 未计数、零阻断。would-deny 全部来自未被策略覆盖的 PUT/DELETE/PATCH 路径与 `POST /api/v1/users`（default-deny 缺口），且因引擎是**字符串模式匹配**（非结构化 JSON 字段匹配），形如 `{"roles":["admin"]}` 的精确规则不会命中影子中间件序列化出的主体串。结论：采集链路、指标口径与日志字段可用；**当前策略覆盖率不足，模拟数据不构成晋级依据**。
  - **晋级 enforcement 的判据（重申，不变，仍需 D2 owner 决策）**：① 观察期 **2–4 周**且覆盖至少一个完整发布窗口；② 关键变更路径（`/api/v1/tasks`、`/api/v1/devices`、K8s/CMDB 写入口、自动化执行）would-deny 率 **< 1%**；③ **零无法解释的 deny**（每条 deny 要么命中显式 deny 策略，要么确认为 default-deny 且有补齐策略计划）；④ `decision="error"` 恒为 0；⑤ 灰度按域推进（先低风险只读邻近域，再核心变更域），enforce 首日保留 kill switch（关闭 `--abac-shadow` 回退 RBAC-only）。本次实测只满足 ④；在 default-deny 缺口收敛到 ② 之前不得进入 enforcement。
- **立项与 M1 规格（2026-11-17，批次53）**：owner 批准立项（决策原文见 §10）：撤销“设备端不实现”的 501 立场，501 仅保留为“禁止脚本路径伪造判定”的函数级守卫；服务端评估仍是权威默认路径。章程状态改为“已立项（M1 进行中）”，里程碑状态见 §5.1。M1 规格已定稿：`docs/projects/abac-device-engine-m1-bundle-spec.md`（bundle v1：规范化 JSON + SHA-256、Ed25519 签名、`keyId` 与双公钥轮换、`GET /api/v1/agent/abac-bundle` + ETag、last-known-good 离线缓存、过期按模式处理、拒绝降级、heartbeat `abac_bundle=v1` 能力协商、1 MiB / 1000 条上限）。本批只改文档，未改代码。
- **M1 已完成（2026-11-17，批次54）**：bundle v1 从规格落到代码。① `pkg/abacbundle/`：确定性 JSON（字段/策略定序、类型白名单）、SHA-256 digest、Ed25519 签名/验签（域分隔 `opsmesh-abac-bundle-v1\n`）、`KeySet` 按 `keyId` 选钥轮换、`MaxPolicies=1000`/`MaxBundleBytes=1 MiB`、哨兵错误分类（过期/未生效/坏签/版本/超限/未知键/非法结构/非法密钥），失败一律拒绝；测试 `bundle_test.go`（规范化黄金向量、重编码稳定、篡改必败、时间窗、schema/限额、轮换、随机包 fuzz 式签验）。② 控制面 `internal/controlplane/abac_bundle_endpoint.go`：`GET /api/v1/agent/abac-bundle`，per-agent HMAC（`X-OpsMesh-Agent-{ID,Timestamp,Signature}`，±5m 时钟窗；未知 agent 与错签同形 401，退役/禁用 403），租户只取已认证 agent 身份，按租户策略快照缓存签名 artifact 使 ETag 稳定（`If-None-Match` ⇒ 304），响应头 `X-OpsMesh-Bundle-Key-Id`/`X-OpsMesh-Bundle-Digest`，未配/非法签发密钥 503；测试 `abac_bundle_endpoint_test.go`（可验签下发、ETag/304、租户隔离、503、401、空策略仍签名、405、生产 default-deny 可达）。③ agent `internal/agent/abac_bundle_client.go`：HMAC 拉取、验签/租户/单调性校验、原子 LKG 缓存（临时文件+rename，0600）、启动先加载缓存再周期拉取（默认 10m）、304 保留现有包、被篡改/跨租户/降级包丢弃；心跳能力 `abac_bundle=v1`（`grpcx.HeartbeatReq.Capabilities`）；测试 `abac_bundle_client_test.go`（happy path、304、篡改丢弃、重启加载、过期缓存保留、能力上报、ctx 退出）。**M2（设备端评估器）待启动**。
- **M2 已完成（2026-11-17，批次55）**：设备端评估器落地，语义与服务端逐条对齐。① `pkg/abacbundle/eval.go`：`Evaluate`/`EvaluateBundle` 实现字段匹配（`""`/`*` 恒匹配、`prefix*` 前缀、其余精确）、条件逐键等值（空 conditions 不解析 context；坏 JSON/非对象 context 有条件即不匹配；非字符串值按 `json.Marshal` 文本比较；缺键不匹配、多余键忽略）、deny-overrides（首个命中 deny 策略立即拒绝，`DenyOverride=true`）、default-deny 与 reason 文案逐字对齐；**排序 tie-break**：策略按 ID 字典序稳定排序，同一策略集在任意输入顺序下 `MatchedPolicyID` 稳定（服务端一致性测试按同序插入）。② 共享 fixture `pkg/abacbundle/testdata/eval_fixtures.json` **32 条**，覆盖 default-deny/allow/deny-overrides/条件命中与不命中/通配与前缀/租户视图/坏 context/Effect 口径；设备侧 `pkg/abacbundle/eval_test.go` 与服务端守卫 `internal/controlplane/abac_eval_parity_test.go` 读同一文件逐例断言 `Allowed`/`MatchedPolicyID`/`DenyOverride`。③ `internal/agent/abac_local_eval.go`：`ABACBundleClient.EvaluateLocal` 每次调用用当前 `KeySet` 重新验签（`KeySet.VerifySignature`：只验签+结构、不做时间窗），无包返回 `ErrNoBundle`，过期不阻止评估、仅置 `Decision.BundleExpired`；**不接入任何请求闸门**（服务端评估调用点与 agent 执行路径未改，设备 enforcement 属 M4）。④ 历史口径记录：服务端 `Effect` 非 `"deny"`（含空串/未知值）一律视为允许候选，设备端为一致性照此实现；若未来收紧为仅 `"allow"`，需同步两端评估器并更新 32 条 fixture。**M3（决策审计回传）待启动**。
- **M3 已完成（2026-11-17，批次56）**：决策审计回传打通「设备判定 → 控制面留痕 → 保留期清理」。① 端点 `POST /api/v1/agent/abac-decisions`（`internal/controlplane/abac_decisions_endpoint.go`，接线 `server_lifecycle.go`，公网路径放行见 `server_middleware.go` 的 `isPublicAPIPath`）：per-agent HMAC 三头与 M1 共用实现 `verifyAgentHMACRequest`（`agent_hmac_auth.go`，域分隔 `abac-decisions`，**签名载荷含 `hex(sha256(rawBody))`**，±5m 时钟窗；未知 agent 与错签同形 401），单批 **≤500 条 / body ≤1 MiB**（413/400），`mode` 仅 `device`/`device-offline`，requestId/bundleId/policyId ≤128、reason ≤512 字符截断；**租户与 agentID 只取验签后的 `store.Agent(agentID)`**，请求体/查询参数中的 tenant 一律忽略；store 未配置返回 503（不静默丢弃）。成功返回 202 `{"accepted":N}`。② 存储：迁移 `036_abac_decision_logs.sql`（`id` 自增、`tenant_id`/`agent_id`/`request_id`/`bundle_id`/`policy_id`/`allowed`/`reason`/`mode`/`occurred_at`/`received_at`，索引 `(tenant_id,received_at)` 与 `(agent_id,received_at)`）；契约 `model.ABACDecisionStore`（Append/List/DeleteBefore）落地 Memory（租户隔离 + 单租户/全局容量上限）、SQL、MultiSchema 三实现。③ **保留期口径**：默认 **720h（30d）**，`--abac-decision-retention` / env `OPSMESH_ABAC_DECISION_RETENTION` 可调（≤0 回退 720h）；`abacDecisionRetentionLoop` 每 24h 删除 **`received_at`** 早于 `now-retention` 的记录——离线补传的 `occurred_at` 可能很旧，但按接收时间计龄，不会入库即被清。④ Agent 侧 `internal/agent/abac_decision_reporter.go`：500 条环形缓冲（满丢最旧并计 `dropped`）、每 30s 刷新、5xx/网络最多重试 2 次（1s/2s）、413 批次折半重试、202 按单调 `seq` 前缀精确摘除（飞行期间新增记录不误删，部分接受保留剩余）、401/403 保留缓冲等待下一轮；`RecordEvaluation` 由当前 LKG 包回填 bundleId，过期包决策标记 `device-offline`；心跳能力合并上报 `abac_bundle=v1` + `abac_decisions=v1`（去重）。开关 `--abac-decision-report` / env `OPSMESH_ABAC_DECISION_REPORT` 默认 true（仅 `--abac-bundle-enabled` 时生效），已落 `internal/config`（不再由 agent 包 `init()` 注册 flag），未配置时行为与引入前一致。回归：`abac_decisions_endpoint_test.go`（落库/租户隔离/认证矩阵/413/400/405/503/生产 default-deny 可达）、`abac_decision_reporter_test.go`（HMAC 与清空/部分接受/飞行期追加/500 重试/401 保留/413 折半/能力合并/ctx 退出）、`memory_abac_decision_logs_test.go`、`sql_abac_decisions_test.go`。**M4（按设备标签灰度 + 回滚）待启动**。

## 定向强制（批次53，已落地）

在「不做全局闸门」的前提下，新增**按路由白名单的 ABAC 强制**：默认关闭，只对显式列出的高风险变更路由调用服务端评估器；未列出的路由完全保持 RBAC-only 行为。

- **配置语法**：`--abac-enforce` / env `OPSMESH_ABAC_ENFORCE`，逗号分隔的 `METHOD /path/pattern`；方法仅限 `POST/PUT/PATCH/DELETE`，路径必须以 `/` 开头，`{id}` 表示匹配一个非空路径分段。非法条目在 `Config.Validate` 阶段直接拒绝启动（`internal/config/config.go`）。
- **预置**：`--abac-enforce-preset=high-risk` / env `OPSMESH_ABAC_ENFORCE_PRESET`，展开为仓库中真实存在的高风险路由：
  - `DELETE /api/v1/devices/{id}`（设备退役/删除，`server_lifecycle.go:76` → `server_devices.go:241`）
  - `DELETE /api/v1/system/users`（`userdel -r`，`server_lifecycle.go:209` → `system_ops.go:153`）
  - `POST /api/v1/system/firewall`（iptables/firewalld 规则添加，`server_lifecycle.go:211` → `system_ops.go:277`）
  - `DELETE /api/v1/system/firewall`（防火墙规则删除，同上）
  - `DELETE /api/v1/tenants/{id}`（租户删除 + 七域级联清理，`server_lifecycle.go:407` → `tenant.go:117`）
  - `POST /api/v1/fileshare/mount`（NFS 挂载/卸载 `op=mount|umount`，`fileshare_ops.go:441` → `:454`）
  - 未收录：设备 `reboot/poweroff`——仓库当前没有对应 HTTP 路由；`/api/v1/system/services` 的 action 白名单也不含主机重启。
- **行为**：命中白名单且身份/租户已建立时评估一次；`allow` 放行且不改写响应，`deny`/default-deny 返回 403 JSON（`error/reason/matchedPolicyID/enforced`），同时写审计 `abac_enforce_denied`（target=path、detail=reason）并累加 `opsmesh_abac_enforce_decisions_total{decision,method}`。评估器 panic 由 `recover` 兜住并按 fail-closed 返回受控 403。强制面硬排除 `/api/v1/abac*`、`/api/v1/federation/*`、`/healthz`、`/readyz`、`/metrics` 与 `/api/v1/events/stream`。
- **与 shadow 的关系**：强制中间件位于 `defaultDenyMiddleware` 之内、shadow 之外；shadow 在构造期编译同一份白名单并对命中路由直接让位，避免同一请求被评估两遍。未命中强制白名单的路由仍由 shadow 只观察。
- **回滚**：清空 `--abac-enforce`（同时移除 preset）即回到 RBAC-only；空白名单在中间件构造期直接返回 `next`，热路径零开销。

## M4 设备侧决策机制（默认关闭，2026-11-18）

本节记录 **M4 机制**（设备侧决策闸门 + 灰度/回滚开关）的落地口径。**默认 `off`，不改变任何既有行为**；只有显式开启 `shadow`/`enforce` 才会在 agent 任务执行路径上做本地 ABAC 决策。

### 配置语法

| 开关 | flag | env | 默认 | 说明 |
|---|---|---|---|---|
| 模式 | `--abac-device-mode` | `OPSMESH_ABAC_DEVICE_MODE` | `off` | `off`/`shadow`/`enforce`；非法值在 `Config.Validate` 拒绝启动 |
| 选择器 | `--abac-device-selector` | `OPSMESH_ABAC_DEVICE_SELECTOR` | 空 | 逗号分隔 `field=value`；空=所有设备命中 |
| 一键关停 | `--abac-device-kill-switch` | `OPSMESH_ABAC_DEVICE_KILL_SWITCH` | `false` | `true` 时无论 mode 为何都按 `off` 处理 |
| 单设备豁免 | `--abac-device-disabled-ids` | `OPSMESH_ABAC_DEVICE_DISABLED_IDS` | 空 | 逗号分隔设备 ID；命中设备始终 bypass |

选择器支持字段（全部来自 `DeviceInfo`，设备侧**没有 labels**，因此不提供 label 选择器）：

- `segment`
- `os`
- `arch`
- `hostname`
- `agent_id`

逐字段**精确匹配**（区分大小写），多个条件为 AND；任一字段不命中即 `bypass`。未知字段在启动期拒绝（`Config.Validate` + `NewDeviceGuard` 双重校验）。

### 决策语义矩阵

| 条件 | Allowed | Enforced | Marker |
|---|---|---|---|
| `mode=off` / kill switch / 设备在 disabled 列表 / 选择器不命中 | `true` | `false` | `bypass` |
| `mode=shadow`（评估成功与否） | `true` | `false` | `shadow` |
| `mode=enforce`，bundle 正常 | 按本地策略 | `true` | `device` |
| `mode=enforce`，bundle 缺失/复验失败，action != `read` | `false` | `true` | `device-offline-fail-closed` |
| `mode=enforce`，bundle 缺失/复验失败，action = `read` | `true` | `false` | `device-offline-fail-open` |
| `mode=enforce`，bundle 过期，action != `read` | `false` | `true` | `device-offline-fail-closed` |
| `mode=enforce`，bundle 过期，action = `read` | `true` | `false` | `device-offline-fail-open` |

- `shadow` 只观察：动作始终执行，但 would-be 决策（`Allowed`/`PolicyID`/`reason`）照常写入 M3 决策审计；marker 以 `shadow: ` 前缀写入审计 `reason`（服务端 `mode` 字段仍只接受 `device`/`device-offline`）。
- `bypass` 路径不做本地评估、不写 M3 审计（与「未启用」等价）。
- 默认（`off`）下 agent 不构造 `DeviceGuard`，worker 仅多一次 nil 判断，**零评估开销**。

### 接线点

- 构造：`internal/agent/agent.go` 的 `Agent.Run` 在 ABAC 策略包/审计回传初始化之后；仅 `mode != off` 且 kill switch 关闭时构造 `DeviceGuard`（`internal/agent/abac_device_guard.go`）。
- 执行前决策：`internal/agent/agent.go` 的 `(*Agent).worker`，在 `task:execute` 执行前调用 `Decide("task:execute", resource{deviceID,segment,os,arch,hostname,taskType})`。
- 拒绝路径：**不执行任务**，回传 `ExitCode=-1` + `Stderr="abac denied: <reason>"`，并写一条 M3 审计（`requestID=taskID`，mode 标记为 `device`/`device-offline`）。
- 允许路径：正常执行；`shadow` 同时记录 would-be 决策。
- 本地评估复用 M2 `ABACBundleClient.EvaluateLocal`（每次重新验签 LKG），不新增策略语义。

### 回滚杠杆（按优先级）

1. **`--abac-device-kill-switch=true`**：一键停用（agent 重配置/重启后生效），无需改 mode；true 时按 `off` 处理。
2. **`--abac-device-disabled-ids=dev-1,dev-2`**：单设备豁免，用于隔离坏设备/异常样本。
3. **`--abac-device-mode=off`**：整域关闭，回到引入前行为（agent 不构造 guard，零开销）。
4. **缩小 selector**：把灰度面收敛到指定 `segment/os/arch/hostname/agent_id`。

### 启用生产前的剩余项

本批只交付 **M4 机制**，不构成生产启用依据。按 `docs/abac-m1-m3-dogfood-2026-11-18.md` §7 的 7 天 soak 口径，至少需要：

1. **策略包拉取健康度**：每 agent 每小时 200/304 比例（稳态 >99% 304）、p95 拉取延迟、超时、401 原因分布（缺头/时钟偏移/签名不符）；`bundleId`/`digest` 仅在策略变化或 TTL 到期时变化。
2. **签名/缓存完整性**：`Verify` 失败为 0；首次成功拉取后 `ErrNoBundle` 为 0；POSIX 下缓存 0600、无 `.abac-bundle-*.tmp` 残留、重启先加载 LKG。
3. **M3 决策回传**：`accepted/failed/dropped`（目标 `dropped=0`）、`received_at - occurred_at` p50/p95（在线亚秒级，>30s 视为回归）、`abac_decision_logs` 增长与索引健康、保留期清理不误删离线补传。
4. **两端一致性**：每日重放 32 条共享 fixture（`pkg/abacbundle/testdata` + `abac_eval_parity_test.go`），差异必须为 0；对同一请求元组监控 `server.evaluate` 与 `EvaluateLocal` 分歧。
5. **决策面观察**：`shadow` 期统计 would-deny 率、`BundleExpired` 决策、allow/deny 体积；在进入 `enforce` 前定义每条路径的 fail-open/fail-closed 口径。
6. **重启/故障演练**：控制面重启后策略与决策日志不丢；agent 保留 LKG 并恢复 304 轮询；MySQL/Redis 故障时签名（本地）不受影响、store 读取可预期降级。

在 7 天 soak 数据齐备且上述指标达标前，生产部署保持 `--abac-device-mode=off`（默认）。
