# TD-60 task 域切流运维 Runbook（整域开关 + 增量开关）

> **适用范围**：按 `docs/td60-cutover-decisions.md` Q1（整域开关）+ Q2（增量开关）执行
> 「控制面本地 task → task-svc」的整域收敛。**task 域没有名册**——名册的键是用户名，
> 而"任务归谁"是新任务的全局决策（存量任务在某一侧的 tasks 表里，没有按请求路由的键）。
> 故 task 域切流 = 整域开关 + 增量开关；**不存在 `cutover-migrate`/`cutover-unroute` 这类搬号 job**。
> 前置条件与机制说明见 `docs/td60-cutover-signal-design.md` §10.4 与
> `docs/td60-domain-convergence-analysis-2026-10-11.md` §3。
> 全线可回退；**任何一步异常先回退再排查**，不带着半切状态过夜。

---

## 0. 前置检查（每次执行前 5 分钟）

```bash
# ① 双轨机制就绪：task 代理面通
curl -s -o /dev/null -w '%{http_code}' -H 'X-Tenant-ID: default' \
  -X POST http://<控制面>/api/v1/task-svc/tasks \
  -d '{"agentID":"agent-demo-1","type":"shell","command":"uname -a"}'   # 期望 201 或 401（端点在）

# ② task-svc 侧健康
curl -s -o /dev/null -w '%{http_code}' http://<task-svc>/health   # 期望 200

# ③ 批次已排空：无在飞批量/灰度任务（批次视图不跨切换存活）
curl -s -H 'X-Tenant-ID: default' http://<控制面>/api/v1/admin/service-traffic   # 查看 task 域在飞请求
```

**准入判据**：① 端点可达 ② task-svc 健康 ③ 无在飞批量/灰度任务。任何一条不符 → 停，先修再排期。

---

## 1. 开关语义（两个开关）

| 开关 | 环境变量 / Flag | 默认 | 含义 |
|---|---|---|---|
| **整域开关** | 控制面 `--mode=controlplane` 固定 | — | 控制面始终运行；此开关不是二选一，而是「哪侧处理新任务」的决策 |
| **增量开关** | `TASK_SVC_OWNS_NEW_TASKS` | `false` | `true` 时控制面四个建任务入口（单建/batch/batch-exec/canary）均返回 409，指引去 task-svc；读路径不受影响 |
| **代理域锁** | `OPSMESH_SERVICE_PROXY` | 空（全部转发） | 含 `task` 时表示 task 域停用（单体在服务任务），即便两个开关都打开也不得 409——否则单体"读得到任务、建不了任务"，前端没有任何一条路可走（自伤式停摆） |

**联锁规则**：`OPSMESH_SERVICE_PROXY=task` 且 `TASK_SVC_OWNS_NEW_TASKS=true` 时，控制面**不得** 409。域停用优先于增量开关。

**判据读 env 在请求时**：改 `.env` 后重启控制面即可翻转，无需重新编译。

---

## 2. 单次全域切换（五步）

### 2.1 预热（切换前 ≥3 个工作日）

在 task-svc 侧确认全部代理路由可达：`/api/v1/task-svc/tasks`、`/api/v1/task-svc/schedules`、`/api/v1/task-svc/canary`、`/api/v1/task-svc/approval`。task-svc 侧 build 201，控制面 201（代理透传）。

> task 域无账号搬号：无需在 task-svc 侧预建用户。身份由 `X-User-Id`/`X-Tenant-ID` 注入头在代理层统一治理，下游消费 `X-Tenant-ID` 做租户过滤。

### 2.2 用户通知（切换前 1 个工作日）

告知运维与开发：**新建任务将路由至 task-svc，存量任务不受影响**。无需用户端感知（前端零改造，Q4 默认值）。

### 2.3 切换（打增量开关）

```bash
# 步骤 A：以 OPSMESH_SERVICE_PROXY=task + TASK_SVC_OWNS_NEW_TASKS=true 重启控制面
# 域停用优先：即便增量开关打开，域停用时也不 409，避免自伤式停摆
OPSMESH_JWT_SECRET=*** OPSMESH_HTTP_TLS=off TASK_SVC_URL=http://task-svc:8102 \
  TASK_SVC_PROXY_ENABLED=true TASK_SVC_OWNS_NEW_TASKS=true OPSMESH_SERVICE_PROXY=task \
  opsmesh --mode=controlplane --http-port 8080 --grpc-port 9090 --metrics-port 9091 \
    --store memory --demo=false

# 步骤 B：验证四个建任务入口全部 409（域未停用前的预检，可选）
# 若域已停用（OPSMESH_SERVICE_PROXY=task），本步骤跳过——期望不 409
```

**判据**：重启后，`OPSMESH_SERVICE_PROXY` 不含 `task` 时四个入口（单建/batch/batch-exec/canary）均 409 且文案指向 task-svc；GET `/api/v1/tasks` 仍 200（读不受闸门影响）。

### 2.4 生效与抽验（切换后 30s 内）

| 抽验 | 期望 |
|---|---|
| 控制面 POST `/api/v1/tasks`（新建） | **409** + 文案指向 task-svc |
| 控制面 POST `/api/v1/tasks/batch` | **409** |
| 控制面 POST `/api/v1/tasks/batch-exec` | **409** |
| 控制面 POST `/api/v1/tasks/canary` | **409** |
| 控制面 GET `/api/v1/tasks`（读） | **200**，走本地 |
| 控制面 POST `/api/v1/task-svc/tasks`（代理） | **201**，落到 task-svc |
| task-svc 直接 POST `/api/v1/tasks` | **201** |
| 存量任务（在某一侧已建）的 GET / cancel / result | 按原侧继续工作，不受影响 |

### 2.5 观察窗（2 个工作日）

每小时看一次；触发任一回退判据（§3）立即回退。重点关注：批次聚合计数两侧口径不同（单体读时聚合；task-svc 读 `batches` 表计数列），双写期间可能出现计数与实际状态不一致。

---

## 3. 回退

**增量开关回退（首选；零停机）**：

```bash
# 把 TASK_SVC_OWNS_NEW_TASKS 改回 false 并重启控制面
OPSMESH_JWT_SECRET=*** OPSMESH_HTTP_TLS=off TASK_SVC_URL=http://task-svc:8102 \
  TASK_SVC_PROXY_ENABLED=true TASK_SVC_OWNS_NEW_TASKS=false OPSMESH_SERVICE_PROXY="" \
  opsmesh --mode=controlplane --http-port 8080 --grpc-port 9090 --metrics-port 9091 \
    --store memory --demo=false
```

**判据**：重启后本地建任务立即恢复 201（判据读 env 在请求时，无需重新编译）。

**整域回退（面级）**：把 `OPSMESH_SERVICE_PROXY` 设为空（或不含 task），全部转发回单体本地实现。

**回退前必须知道的语义边界**：
- 回退**不回滚已完成的任务迁移**：在 task-svc 侧已建的任务仍在 task-svc，只是不再被新路由过去；重切时继续用 task-svc 侧即可（幂等）。
- 批次视图不跨切换存活：在飞批量/灰度的聚合视图会丢，**切换前必须排空在飞批次**（§0 步骤 ③）。
- 本 runbook 用 memory 后端演示，生产（SQL 后端）重启不清号，回退后用户直接可登。

---

## 4. 回退判据（观察窗内任一命中即停批回退）

| 判据 | 阈值 |
|---|---|
| 控制面新建任务失败率 | > 切换前同租户基线 **+0.5 个百分点** |
| task-svc 不可用 | 任意时长（控制面代理层会 502/503） |
| 批次聚合计数与实际状态不一致 | 出现**任意一次** |
| 在飞批量/灰度任务在切换期内丢失视图 | 出现即回退（§2.5 预警） |
| `/api/v1/task-svc/*` 4xx/5xx | 绝对值 **≥10** 且持续 >5 分钟 |

---

## 5. 切换记录（每次必填）

```text
切换单 YYYY-MM-DD
  开关状态：TASK_SVC_OWNS_NEW_TASKS=()  OPSMESH_SERVICE_PROXY=()
  前驱状态：预热完成日期 / 通知发送日期 / 在飞批次清空确认
  执行：重启次数 / 409 预检结果 / 代理 201 确认
  抽验：六条抽验结果（时间戳）
  观察窗：起止 / 每小时指标摘要
  结论：继续 / 回退（原因）
```

---

*本 runbook 由 task 线于 2026-10-16 编写，随机制（cutover/router.go `TaskEnabled()`/`TaskOwnsNewTasks()`/`RouteTaskCreate()`）一同交付；
与 auth 域差异：task 域**无名册、无搬号 job、无重登**；整域开关 + 增量开关即完整切流。*
