# TD-60 域收敛分析：现状实测 + 稳妥路径（2026-10-11）

> 原登记数字「约 27,000 行重复」出自 2026-09-05 的深度侦察。今天实测一遍，把决策建立在**今天的现实**上，
> 而非三个月前的快照。基准 `a628978`。

---

## 1. 现状实测——原 27,000 行剩多少？

### 轻四域（gpu / runbook / incident / autoscaler）：**零重复**

Controlplane 侧**没有任何域实现文件**（`server_gpu.go` 等全部不存在）——纯 `svcproxy` 代理转发。
微服务侧各行 1,174~3,944 行，但它们不是"重复"，是"唯一实现"。

**⇒ 原始七域中的四个已收敛完毕。**

### 核心三域 controlplane 侧现存规模（精确测量）

| 域 | 文件 | 行数 | `s.store.*` 调用 | 后台循环 |
|---|---|---|---|---|
| **task** | server_tasks / server_schedules / server_batch / pipeline / automation | **2,691** | **59** | leaderLoop(29) reclaimLoop(25) scheduleLoop(18) archiveLoop(24) auditMaintenanceLoop(20) deployReconcileLoop(15) workflowScheduleLoop(41) |
| **device** | server_devices / device_metrics / server_netsec / federation | **1,339** | **13** | autoProvisionLoop(74) |
| **auth** | auth / auth_login / auth_users / auth_tokens / auth_apikey / auth_cookies / auth_guard / auth_perms / auth_cutover | **1,950** | **32** | cutoverRosterLoop(26) |
| **合计** | 18 文件 | **5,980** | **104** | 9 个循环 |

微服务侧（域逻辑，排除 proto/pb/model）：

| 微服务 | 行数 | 已具备的生产能力 |
|---|---|---|
| task-svc | 6,634 | HTTP 网关 12 REST 端点、调度器四循环、K8s Lease 选主、事件总线/审计/SSE 桥接 |
| device-svc | 4,167 | agent 二进制分发、AutoProvisionLoop 指数退避、设备 metrics、StartDiscovery |
| auth-svc | 4,147 | 完整 HTTP 网关（六端点）、loginGuard、DeviceFP、Redis SessionStore、审批流 |

**⇒ 核心三域 controlplane 侧合计 5,980 行，而非 27,000。缩减 78%。**

### agent gRPC 通道（保留在 controlplane，不属"重复"）

`internal/grpcx`（agentsig+codec+service+stub）共 **828 行**——agent 管控数据面
（PullTasks / ReportResult / Heartbeat / Register 四条直写 store）。
agent 只说 `opsmesh.v1.Registration` 协议，不认微服务协议。**这段代码无论怎么收敛都留在 controlplane**，
不构成重复项。

---

## 2. 逐域现状——哪些已机械化、哪些没有

### auth 域：收敛机制**已建成并真进程验证**

| 能力 | 状态 | 证据 |
|---|---|---|
| 分端点裁决（login/me/logout/refresh/change-password/register） | ✅ 已实现 | `auth_cutover.go` + `cutover/router.go` |
| 名册（已迁移用户名集合） | ✅ 已实现 | `cutover/roster.go`，文件热更 30s、保留账号拒入、非法拒启 |
| 搬号 job（建号+验证+写名册） | ✅ 已实现 | `opsmesh cutover-migrate`，幂等、原子、一次性口令 |
| 回退 | ✅ 名册摘条目即单用户回退 | 无需重启 |
| 真进程验证 | ✅ 三方 e2e 16/16 | 控制面+auth-svc+job 联动，一次性口令经控制面登录 200 |
| 失败计数对齐 | ✅ 按转发状态码补记 | 401 记失败/2xx 清计数/状态类拒绝不记 |
| 建号口径 | ✅ AUTH_SVC_OWNS_NEW_ACCOUNTS 时拒绝本地建号 | 409+指引 |

**⇒ auth 收敛只差一件事：运维执行**（跑 job、切用户、观察、回退或推进）。不需要更多代码。

### task 域：双轨接通✅，数据面收敛**未机械化**

- task-svc 已经有完整生产路径（A-2 修复完毕），且经 `/api/v1/task-svc/*` 双轨前缀接通
  （`OPSMESH_SERVICE_PROXY` 开关 + 权限逐条镜像本地）。
- **但 controlplane 侧仍有 2,691 行、59 个 store 调用、7 个后台循环**——这不是"能删就删"的死代码，
  是活跃的**任务管控面**：
  - HTTP handler 给前端调（创建/列表/取消/审批/批量/流水线/自动化/定时）
  - 后台循环做任务调度（DAG 释放、租约回收、定时派生、离线归档、审计自检、部署对账、工作流触发）
  - agent gRPC 的 PullTasks/ReportResult 直写同一份 store
- **切流不等价于 auth**：auth 是**身份域**——"这个用户归哪边"可以按人分批；
  task 是**数据域**——"这批任务归哪边"没有按用户切的自然粒度，切了意味着**整域数据搬家**。

### device 域：同 task 域的逻辑

- device-svc 有完整生产路径（A-2 补齐），经双轨前缀接通。
- controlplane 侧 1,339 行、13 个 store 调用——主要在 netsec（SSRF/私网校验，556 行）和
  federation（联邦验签，372 行），这两块在 device-svc **没有等价物**，不是"重复"而是"独有"。

---

## 3. 稳妥路径建议

### auth 域——**立即执行**（零开发，纯运维动作）

按提案 §11 的分批参数跑：
1. 第 0 批（彩排）：内部租户，`cutover-migrate --users <内部用户>`
2. 观察窗（2 个工作日），确认登录/改密/刷新/me/登出全通
3. 逐批推进；最终 `AUTH_SVC_PROXY_ENABLED` 保持 true、名册含全部活跃用户
4. **收尾**：controlplane 侧 auth 实现降级为纯代理转发 → 本地 auth_users/auth_tokens 等
   **约 1,500 行**可以删（保留 auth_cutover.go 的路由 + loginGuard 闸面）

**前提**：对方先补 `mustChangePassword` 入口（协调板已挂，他们说补上后我方 job 直接透传）。

### task 域——**不急切、先消除耦合**

真正的收敛堵点不是"代码重复"，而是 **三个不可分的绑定点**：

| 绑定点 | 现状 | 解法 |
|---|---|---|
| **store 直写**（59 处调用） | handler 直接读写本地 store | 切到 task-svc 后，handler 改为代理——但 agent gRPC 也直写同一份 store ⇒ **必须先解决 agent 侧** |
| **后台循环**（7 个） | leaderLoop/reclaimLoop/scheduleLoop 在 controlplane 进程里 | task-svc **已有等价实现**（A-2 修复时恢复了全部四循环+选主）⇒ 只需切 leader 身份 |
| **agent gRPC** | PullTasks/ReportResult 直写 controlplane store | agent 只说 Registration 协议。给 GrpcServerImpl 加"内部转调 task-svc"的适配层 |

**稳妥序**（不浪费已有劳动、不引入数据分裂）：
1. **共享同一份 MySQL**（现在就是：`--store=mysql` 时两侧指向同一 DB）⇒ **不存在数据搬家问题**
2. task-svc 的循环**已经能跑**（K8s Lease 选主在容器内已就位）——让 task-svc 成为 leader
   而 controlplane 侧不再抢（加环境变量 `TASK_LEADER_ONLY=task-svc` 即可）
3. HTTP 面切流（`OPSMESH_SERVICE_PROXY` 已有，不需要新机制）
4. agent gRPC 适配层：GrpcServerImpl 的 PullTasks/ReportResult 改为调 task-svc HTTP/gRPC，
   不直写 store——**这是唯一需要新代码的地方**，约 200~300 行
5. 切流稳定后删 controlplane 侧 task handler + 循环

**关键**：第 2 步（leader 切换）是最稳的起点——不切 HTTP 也不动 agent，只让任务调度从
controlplane 移到 task-svc，观察定时任务和租约回收是否正常。出问题切回来（改一个环境变量）。

### device 域——**最后处理**

- netsec（556 行 SSRF 校验）和 federation（372 行联邦验签）在 device-svc 没有等价物——
  这不是"收敛"，是"下沉"。若要收敛，先把这两块**作为 pkg 共享层**下沉（netsec 已有 `internal/egress`
  的 IsPrivateIP 可复用）。
- 前置：auth 收敛完成（device 的鉴权依赖 auth）。

---

## 4. 总结——最不浪费的路径

| 域 | 路径 | 新开发 | 前置条件 | 预计收尾后删 |
|---|---|---|---|---|
| **轻四域** | 已收敛 | 0 | — | 0（已删） |
| **auth** | 运维执行（机制全建好） | 0 | 对方补 mustChangePassword | ~1,500 行 |
| **task** | leader 先切 → HTTP 切 → agent 适配 | ~300 行（gRPC 适配层） | auth 先走通 | ~2,700 行 |
| **device** | netsec/federation 先下沉 pkg → 再切 | ~200 行（下沉+接线） | auth + task | ~800 行 |
| **合计** | | **~500 行新开发** | | **~5,000 行可删** |

**"27,000 行重复"的真实残余**：controlplane 侧核心三域 5,980 行中，约 5,000 行在收敛后可删，
其余是 gRPC 通道（828 行，保留）和 netsec/federation 的独有能力（约 900 行，下沉为 pkg 而非删除）。
**不需要一次性大手术**——按 auth → task → device 顺序逐域推进，每一步都可回退。
