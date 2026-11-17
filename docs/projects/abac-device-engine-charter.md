# ABAC 设备侧决策引擎 项目章程

> **状态：待批准（启动需 owner 明确反转当前 501 口径）**
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
| G6 | **决策门先行** | 未获 owner 对“反转 501 口径”的书面批准前，不写实现代码 |

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

> **M0（前置，不单独计里程碑）**：owner 反转 501 口径；策略镜像持久化；`abac:read`/`abac:write` 权限点；评估 API 鉴权收口。未完成则 M1 不启动。

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
| **owner 决策 D1：反转当前 501 口径** | **未批准（硬前置）** | 当前口径为“未实现=显式 501，不造假数据”（`abac_ops.go:43-48`）；不反转则本项目不启动 |
| **owner 决策 D2：设备侧决策定位** | **待定** | 建议先做“咨询/审计”（advisory），不直接阻断设备操作；强制模式另行评审 |
| **owner 决策 D3：签名私钥归属与轮换** | **待定** | 私钥存放、轮换周期、吊销流程需 owner 指定责任方 |
| 服务端策略镜像持久化 | 待补 | `server.go:249-251` 当前仅内存；重启即丢，必须先落 store |
| deny 策略创建能力 | 待补 | `policy-create` 只写 `allow`（`abac_ops.go:112-121`）；需支持显式 `effect` 并做权限校验 |
| `abac:read` / `abac:write` 权限点 | 待补 | 当前 `evaluate` 无权限点、`policy-create` 仅 `task:write`（`abac_ops.go:76-93`；`kafka_ops.go:37-38`）；需先扩 RBAC 目录并接门 |
| 设备决策审计端点鉴权与租户隔离 | 待补 | 复用 `subsystemAuthorize`/租户隔离，不接受匿名或跨租户写入 |
| agent 版本门控与发布通道 | 已有基础 | 设备侧评估器需最低 agent 版本；旧 agent 明确回退服务端评估 |
| 策略包/审计存储 | 待定 | 需要迁移（编号落地时续）；bundle 与审计分表，带 tenant/device 索引 |
| 共享参考实现/测试向量 | **设计约束** | 服务端与设备端不得各写一套语义；至少共享 fixture 与属性测试生成器，避免漂移 |
| Ed25519 签名先例 | 已有 | `internal/config/license.go:10-22`、`:65-117` 的 payload.signature 形状可复用（不直接复用代码） |

## 8. 风险与缓解（含回滚）

| 风险 | 影响 | 缓解 | 回滚 |
|---|---|---|---|
| 服务端/设备端语义漂移 | 同一请求两套结论，审计不可信 | 共享 fixture + 属性测试 + CI 一致性门；语义变更必须同批更新向量 | 停用设备评估，回退服务端决策 |
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
| M0 前置（501 反转后的持久化/权限点/API 鉴权） | 2–3 |
| M1 策略包格式与签名 | 4–6 |
| M2 设备端评估器 | 8–12 |
| M3 决策审计回传 | 5–8 |
| M4 灰度与回滚 | 4–6 |
| 安全评审与红队（穿插） | 2 |
| **合计** | **25–37** |

**关键路径**：`M1 → M2 → M3 → M4`。M1 的 schema 与签名字段一旦定稿，后续所有 fixture、审计记录、灰度元数据都依赖它——必须先冻结。

## 10. 状态与决策记录

- **当前状态：待批准**。
- **负责人：待定**。
- **启动条件（硬）**：owner 必须以书面形式**反转当前“设备脚本路径 evaluate = 501”的口径**（`abac_ops.go:43-48`），并明确设备侧决策是 advisory 还是 enforcement（建议先 advisory）。未反转前，本项目保持关闭，现状（501 + 服务端评估器）即为正确行为。
- **不启动的默认结果**：保留 501 守卫与服务端权威评估器；不产生设备侧策略包、不产生设备决策审计。

| 日期 | 决策/事件 | 记录 |
|---|---|---|
| 未标注（现行口径） | 设备脚本路径 `evaluate` 显式 501；“未实现=显式 501，不造假数据” | `abac_ops.go:43-48`；守卫 `abac_ops_security_test.go:85` |
| 2026-11-04 | 鉴权审计：42% 派发路由无权限校验；223 条路径收口 | `docs/opsmesh-authz-audit-2026-11-04.md:6`、`:27`、`:472-499` |
| 2026-11-05 | 仓库裁定：角色矩阵是唯一授权模型，不引入 ABAC 授权 | `docs/projects-2026-11-05.md:49`；`perm.go:261-265` |
| 2026-11-14 | 审计整改批次 15–19 DONE；其中批次 17 落地 ABAC 真实评估引擎 | `docs/CLAIMS.md:1228` |
| 2026-11-17 | 本草程起草：设备侧引擎待批准；启动需先反转 501 | 本文件（待批准） |

---

*起草：2026-11-17。只读核对基准 `main@9c828b8f`；未改代码、未提交。*

## 进展记录（2026-11-17）

本节记录设备脚本路径 `evaluate` 501 口径复核后的落地进展；本草程的“待批准”状态与 D1/D2/D3 决策项不变。

- **501 守卫保留（不反转）**：`buildABACScript` 的 `evaluate` 分支继续显式返回 501（`internal/controlplane/abac_ops.go:40-52`）。理由：该分支是**函数级守卫**，用于阻止设备脚本路径伪造 ALLOWED 判定；HTTP `POST /api/v1/abac action=evaluate` 早已由服务端真实引擎决策（200），守卫只覆盖不可能产出判定的脚本通道。文案改述为 “device-script evaluation is intentionally disabled; the HTTP evaluate path is served by the server-side engine with device-context enrichment”，由 `TestBuildABACScript_EvaluateIsExplicit501`（`internal/controlplane/abac_ops_security_test.go:91`）钉住 501 与关键措辞。
- **设备上下文增强已落地**：HTTP evaluate 带 `deviceID` 时，先查设备（不存在 404；跨租户 403，与不存在同形不泄露存在性），再把设备属性（`deviceID/hostname/segment/os/arch/state/taskState/agentID/managed`，空字符串跳过）注入策略上下文（`internal/controlplane/abac_ops.go:102-170`，属性展开见 `abac_ops.go:58-78`）。合并规则：**调用方自带键优先**，设备属性只补空缺，绝不覆盖调用方显式上下文；顶层扁平键供现有等值条件匹配，另附嵌套 `device` 对象副本（调用方已提供 `device` 时同样不覆盖）。响应新增 `evaluatedBy`（`server` / `server+device-context`）与 `deviceContext`（实际注入子集）供审计。用例：`abac_ops_security_test.go:163`（同租户注入并命中）、`:197`（跨租户 403 无决策字段）、`:214`（未知设备 404）、`:227`（调用方键不被覆盖）、`:256`（非法 context JSON 400）、`:266`（无 deviceID 时 `evaluatedBy=server`）。
- **仍未完成（承接第 2.3 节缺口）**：
  - 策略镜像跨重启持久化（当前仍为进程内 `abacPolicies`，重启即丢 → default-deny）；
  - 真正的设备侧离线决策引擎（含与服务端共享 fixture 的一致性门）；
  - **强制门接线（enforcement gate）**：ABAC `allow` 目前不参与请求放行，接强制链需 owner 决策 D2 另行评审；
  - 策略包签名 / 版本化 / 回滚（M1，依赖 D1/D3）。
- **路由鉴权缺口已发现并修复（2026-11-17，批次45）**：复核发现 `POST /api/v1/abac` 此前在**所有模式**都无入口鉴权——生产仅由 `defaultDenyMiddleware` 兜底“校验”且不回注请求 context，handler 内 `authctx.FromContext` 恒零值（`internal/controlplane/abac_ops.go:87` 为旧入口）；后果是同租户带 `deviceID` 的 evaluate 被误判 403、`policy-create` 落空租户镜像被当作全局策略、策略镜像键 `action|subject|resource` 跨租户互相覆盖、非生产模式匿名穿透。修复：① 入口统一走 `subsystemAuthorize`（`requireTenantContext` + `requireProd`，`abac_ops.go:122`；回调见 `internal/controlplane/auth.go:89`），租户真值只取已验签身份；② 新增 `abac:read`/`abac:write` 权限点（`internal/store/model/perm.go:216`），read 进 `operatorReadOnlyGroups`，write 仅 admin；③ 镜像键改 `tenantID|action|subject|resource`（`abacPolicyID`，`abac_ops.go:82`）；④ 链式回归 `internal/controlplane/abac_ops_chain_test.go`（匿名拒绝、跨租户 403、读写分离、镜像键租户隔离）。**其余未完成项不变**（见上一条列表：持久化 / 设备端引擎 / 强制门 / 策略包签名）。
- **策略持久化已落地（2026-11-17，批次47，取代上一条“仍未完成”列表中的第一项）**：store 提为事实源、`abacPolicies` 降为评估缓存。新增 `model.ABACPolicyStore` / `ABACPolicyRecord`（`internal/store/model/abac_policy.go:14`）与 Memory / SQL / MultiSchema 三实现（`internal/store/memory/memory_abac_policy.go:15`、`internal/store/sqlstore/sql_abac_policy.go:31`、`internal/store/multischema/multi_schema_abac_policy.go:13`），迁移 `internal/store/sqlstore/migrations/035_abac_policies.sql`（主键 `(tenant_id, id)`，`version` 每次 upsert 自增）；启动全量装载 `initABACPoliciesFromStore`（`internal/controlplane/abac_persistence.go:85`，调用点 `internal/controlplane/server.go:538`）；policy-create/delete 写穿且落库失败即 500、不改缓存、不下发脚本（`internal/controlplane/abac_engine.go:118`、`:158`，handler 见 `abac_ops.go:208`、`:223`）；多副本 5s 按 `(tenant,id)->version` 比对刷新（`abac_persistence.go:118`，接线 `internal/controlplane/server_lifecycle.go:594`），未变即跳过、`abacRev` 防本地写覆盖；租户隔离贯穿键/查询/删除。回归 `internal/store/memory/memory_abac_policy_test.go`、`internal/controlplane/abac_persistence_test.go`。
- **剩余待办（批次47 后口径）**：① shadow 模式（策略只观察不强制，需 owner 确认采样与审计口径）；② 强制闸门接线（enforcement gate，D2 owner 决策，ABAC allow 仍不等于 RBAC allow）；③ 设备侧离线决策引擎（M1/M2，签名策略包 + 本地评估 + 一致性门）；④ 策略包签名 / 版本化 / 回滚（M1，依赖 D1/D3）。
