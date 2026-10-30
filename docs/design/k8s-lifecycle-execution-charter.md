# K8s 生命周期执行链路（C 项）立项书

- 状态：A 项已交付（显式 501 + 前端收口）；C 项立项，待排期
- 裁决来源：owner 2026-10-30 —— 「选 A+C 立项」（原裁决单见 `docs/semantic-rulings-2026-10-30.md` §1）
- 代码事实源：`internal/k8slifecycle/handler.go`（下称 handler.go）

## 1. 为什么立项

升级 / 回滚 / 证书轮换三条入口此前是**假成功**：HTTP 先返回 202，再由纯内存状态机把
phase 推进到 `Completed`，集群侧不做任何动作；前端 `K8sLifecycleView` 还把这个 202 直接
渲染成"升级完成"。这不是"功能缺失"，而是**对外宣称与事实相反**——运维据此认为集群已升级，
会错过真正的窗口期。

A 项（本次）只做诚实化：三条入口一律 501 + `code=ERR_NOT_IMPLEMENTED`，前端禁用按钮并给出
说明。**能力本身仍然是零**。C 项才是把能力做出来。

## 2. 现状证据（A 交付后）

| 路由 | 现状 | 证据 |
| --- | --- | --- |
| `POST /upgrade/plan` | **真执行**：SSH 到 CP 节点跑 `kubeadm upgrade plan` | handler.go:86 |
| `POST /upgrade` | 501（能力不存在） | handler.go:473、490 |
| `POST /rollback` | 501 | handler.go:541、553 |
| `POST /cert-rotate` | 501 | handler.go:558、570 |
| `GET /upgrade/status` | 真路由，但作业表在内存里、且 A 后没有任何写入方（恒 404） | handler.go:524 |
| `POST /node-scale` | **真执行**：SSH `join` / `drain` + `delete node` | handler.go:202、231 |
| `POST /etcd-backup` | **真执行**：`etcdctl snapshot save` + `stat` | handler.go:293 |
| `POST /etcd-restore` | **真执行**：移走 manifest → 清 etcd 数据 → `snapshot restore` → 还原 manifest | handler.go:339 |
| 9 条路由鉴权 | 已接线：GET→`k8s:read`，其余→`k8s:write` | handler.go:684；回归测试 `internal/controlplane/k8s_lifecycle_auth_wiring_test.go` |

## 3. 现状里的三个真风险（C 项期间必须一并收口）

1. **`joinCmd` 由请求体直传并原样执行**（handler.go:202 起）：任何持有 `k8s:write` 的调用方
   都能让控制面在节点上以 root 执行任意 shell。C 项必须改为"服务端按集群信息生成 join 命令"，
   或至少校验必须形如 `kubeadm join <IP>:6443 --token …`。
2. **etcd 恢复无确认、无快照校验**（handler.go:339 起）：移走 manifest + `rm -rf /var/lib/etcd/member`
   是不可逆破坏动作，当前既没有 dry-run，也没有"快照可读、版本匹配、集群处于预期状态"的前置检查。
3. **作业状态不持久化**：`GET /upgrade/status` 读内存 map，控制面重启后作业全部丢失。
   真实现必须以持久化作业表为唯一事实源（见 §6），否则"升级失败后从哪一步续"无法回答。

## 4. 目标与非目标

**目标（C 项验收口径）**

- 升级 / 回滚 / 证书轮换跑在**真实 kubeadm 集群**上，状态可查、可中断、失败可定位；
- 每个破坏性步骤有前置条件检查与明确的失败语义（不是"try 一下看结果"）；
- 作业状态持久化，控制面重启不丢；重复提交幂等；
- 前端恢复入口，展示真实阶段与失败原因。

**非目标**

- 不替代集群自愈（operator/Cluster API）；不做跨云托管集群升级（EKS/AKS 走云 API 另立项）；
- 不接管 etcd 备份策略调度（已有 backup 能力，C 项只复用）；
- 不改变 9 条路由的鉴权模型与路径（如需细分权限点，先扩 RBAC 目录再谈）。

## 5. 分期

### M1 升级（最高价值，先行）

1. 预检：目标版本与当前版本同主版本内跳（kubeadm 只支持跨 minor 单步）、节点 NotReady/污点、
   etcd 健康度、控制面 Pod 数、`kubeadm upgrade plan` 输出与用户确认的 targetVersion 一致；
2. 控制面：首个 CP `kubeadm upgrade apply`（幂等，可重入）→ 其余 CP（逐台 drain / 升级 kubelet+kubectl / uncordon）；
3. Worker：按批次（默认 1 台/批，可配）drain → 升级 → uncordon，批间做 `kubectl get nodes` 健康门；
4. 验证：所有节点版本 == 目标版本、`kubectl get pods -A` 无异常、`kubeadm upgrade plan` 显示无可用升级；
5. 失败语义：任一步失败即停在当前阶段并保留作业记录（**不自动回滚**，回滚是 M2 的能力，混在一起会放大故障）。

**验收**：3 节点 kubeadm 集群（1 CP + 2 Worker）从 1.29.x → 1.30.x 实跑成功；在"第 2 台 Worker
升级中强杀控制面"后重启，作业状态仍在且能继续/终止；重复提交同一 spec 不产生第二个作业。

### M2 回滚

依赖 M1 产出的作业记录 + etcd 快照。

1. 前置：存在本次升级前的 etcd 快照（由 M1 自动备份，不是依赖人工）；目标版本 == 升级前版本；
2. 执行：停非系统 Pod → `etcdctl snapshot restore` → 降级 kubeadm/kubelet/kubectl → 重启 kubelet → 验证；
3. 明确的"不能回滚"判定：跨 minor 已升级超过一步、控制面已接受新版本 API 对象。

**验收**：M1 场景中人为让 Worker 升级失败 → 触发 M2 → 集群回到 1.29.x 且工作负载恢复。

### M3 证书轮换

1. 前置检查：`kubeadm certs check-expiration`，列出将轮换的证书；
2. 执行：`kubeadm certs renew all` → 控制面组件重启（逐台，避免同时失联）→ kubelet 证书轮换（`kubeadm kubelet ...` 或 cert rotate）；
3. 验证：新证书有效期 + apiserver/etcd 连通性。

**验收**：单 CP 集群轮换后 `kubectl` 可继续访问，且旧 sa.key 场景（外部签名的 service account key）有明确拒绝路径。

## 6. 数据模型（草案）

新建 `lifecycle_jobs` 表（迁移编号按落地时续）：

| 列 | 类型 | 说明 |
| --- | --- | --- |
| `id` | VARCHAR(64) PK | `{cluster}-{kind}-{timestamp}`，与现有 ID 形态兼容 |
| `cluster_id` | VARCHAR(64) | 作业归属 |
| `kind` | VARCHAR(16) | upgrade/rollback/cert-rotate |
| `tenant_id` | VARCHAR(64) | 鉴权与审计对齐 |
| `phase` | VARCHAR(32) | 状态机阶段（见 §5） |
| `progress` / `current_step` | INT / VARCHAR | 展示用 |
| `spec_json` | JSON | 入参快照（不含凭据） |
| `error` | TEXT | 失败原因 |
| `created_at` / `updated_at` | DATETIME | |
| `lock_owner` | VARCHAR(64) | 同一集群同一 kind 只允许一个活动作业（也行用行锁或 Redis 锁） |

约束：同一 `cluster_id+kind` 同时最多 1 条非终态作业；作业推进用乐观锁（`updated_at`/`version`）。

## 7. 通用护栏（每个 M 都要）

- **干跑**：所有写操作先出 plan（M1 已有 `upgrade/plan` 可扩展 `dryRun`）；
- **审批**：复用既有审批流（`/approval/*`）对破坏性动作设门（至少 etcd-restore 与 rollback）；
- **并发**：集群级串行（§6 的唯一活动作业约束）；控制面多副本时用 DB 行锁而非进程锁（多副本滚动升级场景）；
- **幂等/中断恢复**：每个阶段记录"已完成/进行中"，重启后从最后一个完成点续；
- **审计**：作业创建/阶段推进/失败/人工终止全部写审计链（`internal/store/sqlstore/sql_audit_chain.go` 已有能力）；
- **可观测**：作业阶段变更打结构化日志 + 指标（按 kind/phase 分桶），出现 Failed 必须可告警；
- **凭据**：SSH 凭据不得进入 `spec_json`、日志、审计（现有 `NodeSpec.Password/SSHKey` 只在 plan 请求体里出现过）。

## 8. 风险与未决问题

| 风险 | 影响 | 处置 |
| --- | --- | --- |
| 没有可长期使用的多节点测试集群 | 只能在 kind 上做"不真实"的验证（kind 不支持 kubeadm 升级语义） | 需一台 3 节点 VM 环境（可复用 `deploy/` 的 kubeadm 模板），在 CI 之外做人工验收；CI 只做静态/单测 |
| SSH 直连 CP 的假设 | 云托管集群、无 SSH 的裸金属不在覆盖面 | 明确写进非目标；按 cluster provider 分流留接口 |
| 升级过程中的工作负载中断 | 业务影响 | drain 语义 + 批次上限 + 维护窗口；默认只允许显式 force 才跳过保护 |
| kubectl/kubeadm 版本矩阵 | 跨版本命令参数漂移 | 按 minor 分支守卫，plan 阶段拒绝不支持的跨度 |

## 9. 工作量与依赖

- M1：3–5 人日（含真集群演练）；M2：2–3 人日；M3：1–2 人日；持久化与护栏：2 人日；
  合计 **8–12 人日**（与裁决单 C 项 "5–10 天" 同量级，差异来自真集群演练与持久化）。
- 依赖：`internal/platform/ssh`（已有）、etcd backup（已有）、审批流（已有）、审计链（已有）；
  新增依赖只有一张 `lifecycle_jobs` 表与 job 状态机。
- 排期建议：紧接 v0.17.0 之后（本次 A 项已消除对外误宣称，不阻塞发布）。

## 10. A 项交付的验收口径（本 PR）

- 三条入口：合法请求 → **501**，响应含 `code=ERR_NOT_IMPLEMENTED` 且**不带** phase/progress；
- 未实现动作**不登记作业**（否则 `/upgrade/status` 会返回假 Completed）；
- 前端：升级/轮换按钮 disabled + 页面说明；`plan` / etcd / node-scale 入口保持可用；
- 变异验证：状态码 501→202 判红；`code` 改名判红；临时登记作业判红。
