# OpsMesh 功能设计文档

> 本文档为 OpsMesh 网段运维中枢的完整功能设计稿，覆盖 18 个功能模块的用例、流程、业务规则、边界条件、配置项与 API 端点。文档基于 `README.md` 功能矩阵与 `docs/api-reference.md` API 端点为输入源，结合 `internal/` 源码模块边界进行功能闭环建模。

## 文档说明

### 目的与范围

本文档面向产品经理、研发工程师、测试工程师与运维实施工程师，提供 OpsMesh 各功能模块的统一设计视图。文档不重复 API 字段细节（详见 `api-reference.md`），不重复数据库表结构（详见 `database-design.md`），而聚焦于**业务流程、规则约束、边界处理与配置开关**。

### 设计原则

1. **零依赖优先**：所有模块在 `--store=memory` 模式下须可独立运行，无 MySQL/Redis 依赖。
2. **租户隔离硬约束**：所有业务读写按 `X-Tenant-ID` 行级过滤，越权返回 403/404。
3. **审计 100% 留痕**：所有变更类操作经 `AuditEvent` 落 `audit_log` 表或内存 ring（上限 10000）。
4. **HA 单调安全**：多副本控制面经 `leader_lease` 表选主，仅 leader 执行回收/调度/归档循环。
5. **可插拔后端**：存储（memory/mysql）、日志（memory/sql/loki/es）、事件总线（noop/log/kafka）、会话（memory/redis）均按接口编程，按 flag 切换。
6. **失败兜底**：所有 panic 经 recovery 中间件兜底，单处 handler 异常不拖垮整个控制面。

### 术语对照

表：术语对照表

| 术语 | 全称 | 含义 |
|------|------|------|
| 控制面 | Control Plane | OpsMesh 服务端进程（`--mode=controlplane`），承载 HTTP/gRPC/Metrics |
| Agent | Agent | 部署在被纳管设备上的轻量进程（`--mode=agent`），执行 shell/service/file 任务 |
| 网段 | Segment | 物理网络分段，作为设备分桶键与联邦边界 |
| 纳管 | Onboard | 将候选设备正式纳入运维管理的过程 |
| 死信 | Dead Letter | 任务重试耗尽后的终态，需人工处置 |
| CI | Configuration Item | CMDB 中的配置项实例 |
| DAG | Directed Acyclic Graph | 作业编排的有向无环图工作流 |
| AT / RT | Access / Refresh Token | JWT 双 Token，AT 短期签发，RT 长期旋转 |
| mTLS | mutual TLS | 双向 TLS 证书校验 |
| SSE | Server-Sent Events | 服务端推送长连接事件流 |

### 模块清单

> **编号体系说明**：本文档使用 **F1–F18** 标注 18 个功能模块（Feature module）。这与 `README.md` / `docs/module-design.md` 中的 **M** 编号（功能演进项，如 M3 部署中心、M5 作业编排、M7 监控告警）以及 `docs/product-roadmap.md` 第 8 章的 **M1–M4** 里程碑编号相互独立，不冲突、不复用。三套编号体系对照：
> - **F1–F18**：功能模块（本文档专用，覆盖全部 18 个功能域）
> - **M1–M7**：功能演进项（`README.md` API 速查表 / flag 说明 / `module-design.md`，表示已落地的演进能力）
> - **M1–M4**：里程碑（`product-roadmap.md` 第 8 章，表示规划阶段）
>
> **成熟度说明**：本文档描述的 18 个功能模块均为 **✅ 功能完整（CI 验证中）**，不标注"生产可用"。CI 集成测试/安全扫描/lint/race 检测需 GitHub Actions runner 真跑，当前标记「阻塞·待外部」（详见 `DELIVERY.md` §7）。

表：18 个功能模块清单

| 编号 | 模块 | 域 | 源码目录 |
|------|------|----|----------|
| F1 | 设备纳管 | 设备 | `internal/discover`、`internal/agent`、`internal/controlplane` |
| F2 | 任务执行 | 任务 | `internal/agent`、`internal/controlplane` |
| F3 | 配置下发 | 任务 | `internal/agent`（file 执行器） |
| F4 | 服务管理 | 任务 | `internal/agent`（service 执行器） |
| F5 | 状态监控 | 观测 | `internal/metrics`、`internal/agent` |
| F6 | 告警管理 | 告警 | `internal/notify`、`internal/controlplane` |
| F7 | CMDB | 配置库 | `internal/cmdb` |
| F8 | 作业编排 | 编排 | `internal/dag`、`internal/orchestration` |
| F9 | 部署管理 | 部署 | `internal/deploy` |
| F10 | K8s 管理 | 集群 | `internal/controlplane`（client-go 集成） |
| F11 | 日志检索 | 观测 | `internal/logstore` |
| F12 | 中间件部署 | 部署 | `internal/controlplane`（模板库） |
| F13 | OS 优化 | 部署 | `internal/controlplane`（模板库） |
| F14 | 多租户 | 安全 | `internal/store`、`internal/authctx` |
| F15 | 用户权限 | 安全 | `internal/controlplane`（RBAC） |
| F16 | 联邦 | 网络 | `internal/controlplane`（FederationManager） |
| F17 | 密钥管理 | 安全 | `internal/controlplane`（SecretProvider Chain） |
| F18 | SSE 实时推送 | 事件 | `internal/events`、`internal/controlplane` |

---

## 第1章 设备纳管

### 1.1 模块概述

设备纳管模块负责将物理或虚拟主机纳入 OpsMesh 运维管理。支持四种纳管路径：自动发现、SSH 推送、agent 安装、心跳上报。设备生命周期为 `discovered → provisioning → onboarded → online → offline → retired`。

### 1.2 用例

表：设备纳管用例

| 用例 ID | 名称 | 主参与者 | 前置条件 | 主流程 |
|---------|------|----------|----------|--------|
| UC-D-01 | 网段自动发现 | 运维工程师 | `--discover=true` 且配置 `--segment-cidr` | 控制面 TCP 扫描存活主机 → 落候选设备（`managed=false`） |
| UC-D-02 | 单设备纳管 | 运维工程师 | 候选设备已存在 | 调用 provision 接口签发 install token → 在目标机执行 bootstrap → agent 注册回控 → 设备 `managed=true` |
| UC-D-03 | 网段批量纳管 | 运维工程师 | 候选设备清单已存在 | 调用 `/provision/auto` 批量签发 token → SSH 推送 bootstrap（可选）→ 并行注册 |
| UC-D-04 | SSH 推送安装 | 运维工程师 | 配置 `--provision-ssh-key` | 控制面 SSH 登录目标机 → 执行 bootstrap 命令 → agent 启动 |
| UC-D-05 | 设备退役 | 运维工程师 | 设备 `state=offline` 或人工触发 | 调用 DELETE 接口 → `state=retired` → 不再接受任务 |
| UC-D-06 | 超龄自动归档 | 系统（leader） | `--archive-age-min>0` | `archiveLoop` 周期扫描 → 最后心跳早于阈值的设备 → `state=retired` |
| UC-D-07 | Agent 即设备 | Agent | `--discover=false`（默认） | Agent 注册时自动创建设备记录（`managed=true`） |

### 1.3 流程图

图：自动纳管流程图

```text
┌──────────────┐
│ 网段发现扫描  │  --discover --segment-cidr=10.30.0.0/24
└──────┬───────┘
       │ TCP 存活探测
       ▼
┌──────────────┐
│ 候选设备入库  │  managed=false, state=discovered
└──────┬───────┘
       │ POST /api/v1/devices/{id}/provision
       ▼
┌──────────────┐
│ 签发 install │  HMAC-SHA256 签名，15 分钟有效
│   token      │
└──────┬───────┘
       │ 返回 bootstrap 命令
       ▼
┌──────────────┐
│ 目标机执行    │  curl ... | sh -s -- --token=<tok>
│ bootstrap    │
└──────┬───────┘
       │ 下载并启动 agent
       ▼
┌──────────────┐
│ agent 注册   │  gRPC Register{install_token, hostname, segment}
└──────┬───────┘
       │ ConsumeToken 校验（限时 + 一次性）
       ▼
┌──────────────┐
│ 翻转纳管状态  │  managed=true, state=online
└──────────────┘
```

图：设备状态机示意图

```text
discovered ──provision──▶ provisioning ──register──▶ onboarded
                                                     │
                                                     │ heartbeat
                                                     ▼
                                                  online
                                                     │
                                                     │ 心跳超时
                                                     ▼
                                                  offline
                                                     │
                                                     │ 退役/超龄归档
                                                     ▼
                                                  retired (终态)
```

### 1.4 业务规则

- **BR-D-01**：install token 一次性消费，`ConsumeToken` 成功后立即失效，重复使用返回 401。
- **BR-D-02**：install token 有效期 15 分钟，超时拒绝并需重新签发。
- **BR-D-03**：install token 由 `--provision-secret` 经 HMAC-SHA256 签名，多副本控制面须共享同一密钥。
- **BR-D-04**：`--discover=false` 时采用 "agent 即设备" 降级纳管，agent 注册即创建设备记录。
- **BR-D-05**：`archiveLoop` 仅由 leader 执行，避免多副本重复归档。
- **BR-D-06**：设备 `state=retired` 后不再接受任务下发，已下发未领取的任务由 `reclaimLoop` 复位。
- **BR-D-07**：SSH 推送须配置 `--provision-ssh-known-hosts`，否则使用 `InsecureIgnoreHostKey`（生产禁用）。
- **BR-D-08**：`--device-fp-deadline>0` 时设备指纹为空的纳管请求在该时长后强制拒绝。

### 1.5 边界条件

| 边界场景 | 处理策略 |
|----------|----------|
| 网段扫描无存活主机 | 返回空候选列表，不报错 |
| install token 过期 | 401 拒绝，提示重新签发 |
| install token 重复消费 | 401 拒绝，审计记录 |
| 目标机 SSH 不可达 | bootstrap 文本仍返回，SSH 推送失败仅记录日志 |
| Agent 注册时 hostname 冲突 | 按 (tenant, segment, hostname) 唯一约束，冲突返回 409 |
| 设备指纹为空且超 deadline | 纳管请求拒绝，agent 进入重试注册 |
| 同时退役在线设备 | 标记 retired，下发 kill 任务终止 agent worker |
| 多副本同时归档同一设备 | leader lease 单点执行，避免重复 |

### 1.6 配置项

表：设备纳管配置项

| Flag | 默认值 | 说明 |
|------|--------|------|
| `--discover` | false | 开启真实网段发现 |
| `--segment-cidr` | "" | 待扫描网段 CIDR |
| `--auto-provision` | false | 发现存活主机后自动登记候选并推送 agent |
| `--install-token` | "" | agent bootstrap 携带的一次性 install token |
| `--provision-secret` | "" | install token HMAC 签名密钥 |
| `--advertise-addr` | "" | 控制面对外 HTTP 地址（拼接 bootstrap 命令） |
| `--provision-ssh-user` | root | SSH 推送用户 |
| `--provision-ssh-key` | "" | SSH 私钥路径（空=关闭 SSH 推送） |
| `--provision-ssh-key-pass` | "" | SSH 密钥密码 |
| `--provision-ssh-known-hosts` | "" | SSH KnownHosts 文件路径 |
| `--archive-age-min` | 1440 | 离线超龄自动归档阈值（分钟，<=0 关闭） |
| `--device-fp-deadline` | 0 | 设备指纹强制非空截止时间 |

### 1.7 API 端点

表：设备纳管 API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/devices` | 设备清单（按网段分组，支持 segment/status/managed 过滤） |
| GET | `/api/v1/devices/{id}` | 设备详情（含最近任务结果） |
| DELETE | `/api/v1/devices/{id}` | 退役设备（state → retired） |
| POST | `/api/v1/devices/{id}/provision` | 签发 install token，返回 bootstrap 命令 |
| POST | `/api/v1/provision/auto` | 按网段批量签发 install token |
| GET | `/api/v1/agents` | agent 清单 |
| GET | `/install.sh` | agent bootstrap 脚本 |
| GET | `/bin/opsmesh-agent` | agent 二进制下载 |
| gRPC | `Registration.Register` | agent 注册，携带 install_token 自动纳管 |

---

## 第2章 任务执行

### 2.1 模块概述

任务执行是 OpsMesh 的核心能力，支持 shell/service/file 三种执行模式，覆盖超时、重试、取消、回执完整生命周期。任务状态机为 `pending → running → done/failed/cancelled`，失败重试耗尽进入死信。

### 2.2 用例

表：任务执行用例

| 用例 ID | 名称 | 主参与者 | 主流程 |
|---------|------|----------|--------|
| UC-T-01 | 下发单条 shell 任务 | 运维工程师 | POST /tasks → pending → agent ClaimTask → running → ReportResult → done/failed |
| UC-T-02 | 批量下发任务 | 运维工程师 | POST /tasks/batch → 逐台查找 agent + 租户校验 → 并行 pending |
| UC-T-03 | 定时周期任务 | 运维工程师 | schedule 字段填 cron → scheduleLoop 派生 pending 实例 |
| UC-T-04 | 任务超时中止 | Agent | exec.CommandContext 到时 → 子进程 SIGKILL → failed |
| UC-T-05 | 失败重试 | 系统 | retry_count < max_retries → 复位 pending，retry_count++ |
| UC-T-06 | 进入死信 | 系统 | retry_count = max_retries → failed + dead_letter=true → critical 告警 |
| UC-T-07 | 取消 pending 任务 | 运维工程师 | POST /cancel → state=canceled，不进入 agent 领取 |
| UC-T-08 | 取消 running 任务 | 运维工程师 | POST /cancel → PollCancels 信号 → worker context 取消 → 子进程中止 |
| UC-T-09 | 查询任务回执 | 运维工程师 | GET /tasks/{id}/result → stdout/stderr/exitCode |
| UC-T-10 | 超期任务回收 | 系统（leader） | reclaimLoop 扫描 running 超过 task_lease_sec → 复位 pending 重调度 |

### 2.3 流程图

图：任务生命周期流程图

```text
              POST /api/v1/tasks
                     │
                     ▼
              ┌──────────┐
              │ pending  │
              └────┬─────┘
                   │  agent ClaimTask (原子领取)
                   ▼
              ┌──────────┐
              │ running  │
              └────┬─────┘
                   │
       ┌───────────┼───────────┐
       │           │           │
       ▼           ▼           ▼
   ┌──────┐  ┌─────────┐  ┌──────────┐
   │ done │  │ failed  │  │cancelled │
   └──────┘  └────┬────┘  └──────────┘
                  │
                  │ retry_count < max_retries?
                  │   是 → 复位 pending (retry_count++)
                  │   否 → dead_letter=true
                  ▼
              ┌────────────┐
              │ dead_letter │ → critical 告警
              └────────────┘
```

图：任务取消流程图

```text
POST /tasks/{id}/cancel
        │
        ▼
   读取当前状态
        │
        ├── pending ──▶ state=cancelled (运行前拦截)
        │
        └── running ──▶ state=cancelled
                            │
                            ▼
                    agent cancelLoop (每 2s)
                            │
                            ▼
                    PollCancels 命中
                            │
                            ▼
                    worker context.Cancel()
                            │
                            ▼
                    exec.CommandContext 中止子进程
                            │
                            ▼
                    worker 丢弃结果不回写
```

### 2.4 业务规则

- **BR-T-01**：`ClaimTask` 为原子操作（SQL 行锁 / memory 互斥锁），多 agent 并发领取同一任务仅一人成功。
- **BR-T-02**：任务 `max_retries` 默认 3，失败重试不跨 agent（复位 pending 后任意 agent 可领取）。
- **BR-T-03**：定时任务为模板（有 schedule 无 parentID），`scheduleLoop` 派生实例（parentID 指向模板），同周期幂等防重复。
- **BR-T-04**：取消 running 任务时 worker 丢弃结果不回写 store，避免误翻 done/failed/死信。
- **BR-T-05**：`reclaimLoop` 仅 leader 执行，复位超过 `task_lease_sec`（默认 300s）未上报的 running 任务。
- **BR-T-06**：shell 任务受 `--agent-shell-whitelist` 限制，非白名单命令前缀直接拒绝。
- **BR-T-07**：file 任务受 `--agent-file-root-whitelist` 限制，且拒绝 `../` 路径遍历与符号链接。
- **BR-T-08**：agent worker 池并发度由 `--worker-concurrency`（默认 4）控制。

### 2.5 边界条件

| 边界场景 | 处理策略 |
|----------|----------|
| Agent 领取后崩溃 | 任务超 task_lease_sec 后由 reclaimLoop 复位 pending |
| 多副本同时派发定时任务 | leader 单点执行 scheduleLoop，避免重复派生 |
| 重试次数耗尽 | 进入死信 + 产出 critical 告警 |
| 取消已完成任务 | 状态不变，返回 200 但提示当前状态 |
| 超时与取消并发 | 取消优先，状态置 cancelled |
| 命令不在白名单 | agent 拒绝执行，回写 failed + stderr="command not allowed" |
| 文件路径含 `../` | agent 拒绝执行，回写 failed |
| 批量下发部分 agent 不存在 | 跳过不存在 agent，响应中标注失败项 |
| 任务 stdout 过大 | agent 端截断至 1 MiB，超出部分丢弃并标注 |

### 2.6 配置项

表：任务执行配置项

| Flag | 默认值 | 说明 |
|------|--------|------|
| `--task-timeout` | 120s | agent 单任务执行超时 |
| `--task-lease-sec` | 300 | 任务租约租期秒，超期未上报则复位重调度 |
| `--task-max-retries` | 3 | 任务失败重试上限 |
| `--worker-concurrency` | 4 | agent 任务 worker 池并发度 |
| `--max-procs` | 256 | agent RLIMIT_NPROC 上限（0=不限制） |
| `--max-files` | 4096 | agent RLIMIT_NOFILE 上限（0=不限制） |
| `--max-memory-mb` | 0 | agent RLIMIT_AS 上限 MB（0=不限制） |
| `--agent-shell-whitelist` | "" | shell 任务允许的命令前缀列表（逗号分隔） |
| `--agent-file-root-whitelist` | "" | 文件任务允许的根目录白名单 |

### 2.7 API 端点

表：任务执行 API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/tasks` | 任务列表（支持 status/agent_id 过滤） |
| POST | `/api/v1/tasks` | 下发单条任务（type: shell/service/file） |
| POST | `/api/v1/tasks/batch` | 批量下发 |
| POST | `/api/v1/tasks/{id}/cancel` | 取消任务 |
| GET | `/api/v1/tasks/{id}/result` | 查询任务回执 |
| gRPC | `Registration.PullTasks` | agent 原子领取 pending 任务 |
| gRPC | `Registration.ReportResult` | agent 上报执行结果 |
| gRPC | `Registration.CancelTask` | 取消指定任务 |
| gRPC | `Registration.PollCancels` | agent 轮询本机被取消的任务 ID |

---

## 第3章 配置下发

### 3.1 模块概述

配置下发模块基于任务执行的 file 模式，提供文件分发、模板渲染、配置管理能力。通过原子写入 + rename 保证配置文件落盘一致性，避免部分写入导致服务异常。

### 3.2 用例

表：配置下发用例

| 用例 ID | 名称 | 主参与者 | 主流程 |
|---------|------|----------|--------|
| UC-C-01 | 文件分发 | 运维工程师 | POST /tasks (type=file) → agent 写入目标路径 → 原子 rename → 回执 |
| UC-C-02 | 模板渲染 | 运维工程师 | 提交模板 + 参数 → 控制面渲染 → 下发渲染后内容 |
| UC-C-03 | 批量配置同步 | 运维工程师 | POST /tasks/batch (type=file) → 多设备并行下发 |
| UC-C-04 | 配置回滚 | 运维工程师 | 下发旧版本文件 → 覆盖当前配置 → 服务重启 |
| UC-C-05 | 配置校验 | Agent | 写入后执行校验命令（如 `nginx -t`）→ 失败回滚至备份 |

### 3.3 流程图

图：配置下发流程图

```text
POST /api/v1/tasks (type=file)
        │
        ▼
┌──────────────┐
│ 控制面下发    │  content + target_path + checksum
└──────┬───────┘
       │ agent ClaimTask
       ▼
┌──────────────┐
│ 路径白名单校验│  --=拒绝 ../ 与符号链接
└──────┬───────┘
       │
       ▼
┌──────────────┐
│ 写入临时文件  │  target_path + ".opsmesh.tmp"
└──────┬───────┘
       │
       ▼
┌──────────────┐
│ checksum 校验│  SHA256 比对
└──────┬───────┘
       │
       ▼
┌──────────────┐
│ 原子 rename  │  tmp → target_path
└──────┬───────┘
       │
       ▼
┌──────────────┐
│ 可选校验命令  │  如 nginx -t
└──────┬───────┘
       │ 失败 → 回滚至备份
       ▼
┌──────────────┐
│ 回写回执     │  stdout/stderr/exitCode
└──────────────┘
```

### 3.4 业务规则

- **BR-C-01**：文件写入采用"临时文件 + rename"两步法，rename 为原子操作保证一致性。
- **BR-C-02**：目标路径须通过 `--agent-file-root-whitelist` 校验，空配置不限制根目录但仍拒绝 `../` 与符号链接。
- **BR-C-03**：可选 checksum（SHA256）校验，比对失败则删除临时文件并回写 failed。
- **BR-C-04**：可选校验命令（如 `nginx -t`、`systemctl daemon-reload`），失败时回滚至备份文件。
- **BR-C-05**：模板渲染在控制面完成（避免 agent 端引入模板引擎依赖），渲染后内容作为 file 任务 content 下发。
- **BR-C-06**：配置文件权限与属主由任务参数指定（mode/owner/group），写入后 `chmod`/`chown` 调整。

### 3.5 边界条件

| 边界场景 | 处理策略 |
|----------|----------|
| 目标目录不存在 | 按 `mkdir -p` 参数决定是否创建 |
| 磁盘空间不足 | 写入失败，回写 failed + stderr="no space left" |
| 路径遍历攻击 | 拒绝执行，回写 failed |
| 符号链接劫持 | 拒绝执行，回写 failed |
| checksum 不匹配 | 删除临时文件，回写 failed |
| 校验命令失败 | 回滚至备份，回写 failed |
| 并发写同一文件 | 由 agent worker 串行化（同 agent 内 worker 池并发） |
| 文件过大 | 受 1 MiB 请求体上限约束 |

### 3.6 配置项

表：配置下发配置项

| Flag | 默认值 | 说明 |
|------|--------|------|
| `--agent-file-root-whitelist` | "" | 文件任务允许的根目录白名单 |
| `--task-timeout` | 120s | 单任务执行超时（含文件写入） |
| `--task-max-retries` | 3 | 写入失败重试上限 |

### 3.7 API 端点

表：配置下发 API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/tasks` | type=file 下发文件（content/target_path/checksum/mode/owner/group） |
| POST | `/api/v1/tasks/batch` | 批量文件分发 |
| GET | `/api/v1/tasks/{id}/result` | 查询下发回执 |
| gRPC | `Registration.PullTasks` | agent 领取 file 任务 |
| gRPC | `Registration.ReportResult` | agent 上报写入结果 |

---

## 第4章 服务管理

### 4.1 模块概述

服务管理模块基于任务执行的 service 模式，封装 systemctl start/stop/restart/enable/disable/status 等操作。通过 systemd 单元管理实现服务生命周期控制，回执包含服务当前状态与最近日志。

### 4.2 用例

表：服务管理用例

| 用例 ID | 名称 | 主参与者 | 主流程 |
|---------|------|----------|--------|
| UC-S-01 | 启动服务 | 运维工程师 | POST /tasks (type=service, action=start) → systemctl start → 回执 |
| UC-S-02 | 停止服务 | 运维工程师 | action=stop → systemctl stop → 回执 |
| UC-S-03 | 重启服务 | 运维工程师 | action=restart → systemctl restart → 回执 |
| UC-S-04 | 开机自启 | 运维工程师 | action=enable → systemctl enable → 回执 |
| UC-S-05 | 关闭自启 | 运维工程师 | action=disable → systemctl disable → 回执 |
| UC-S-06 | 查询状态 | 运维工程师 | action=status → systemctl status → 解析输出回执 |
| UC-S-07 | 批量重启 | 运维工程师 | POST /tasks/batch (type=service, action=restart) → 多设备并行 |

### 4.3 流程图

图：服务管理流程图

```text
POST /api/v1/tasks (type=service, action=restart, unit=nginx)
        │
        ▼
┌──────────────┐
│ 控制面下发    │
└──────┬───────┘
       │ agent ClaimTask
       ▼
┌──────────────┐
│ 命令白名单校验│  systemctl 须在白名单
└──────┬───────┘
       │
       ▼
┌──────────────┐
│ 执行 systemctl│  exec.CommandContext("systemctl", action, unit)
└──────┬───────┘
       │
       ▼
┌──────────────┐
│ 解析输出     │  active/inactive/failed + 最近日志
└──────┬───────┘
       │
       ▼
┌──────────────┐
│ 回写回执     │  stdout=状态, exit_code=0/非0
└──────────────┘
```

### 4.4 业务规则

- **BR-S-01**：service 任务通过 `systemctl <action> <unit>` 实现，action ∈ {start, stop, restart, enable, disable, status}。
- **BR-S-02**：`systemctl` 须在 `--agent-shell-whitelist` 内（若白名单非空）。
- **BR-S-03**：action=status 时解析 systemctl 输出，提取 active/inactive/failed 状态与最近 10 行日志。
- **BR-S-04**：服务不存在时 systemctl 返回非 0 exit code，回写 failed + stderr。
- **BR-S-05**：超时由 `--task-timeout` 控制，systemctl 长时间无响应（如 start 等待依赖）触发 SIGKILL。
- **BR-S-06**：批量服务操作经 `/tasks/batch` 下发，逐台查找 agent + 租户校验。

### 4.5 边界条件

| 边界场景 | 处理策略 |
|----------|----------|
| 服务单元不存在 | systemctl 返回非 0，回写 failed |
| 服务已处于目标状态 | systemctl 幂等，返回 0 |
| systemctl 命令缺失 | 回写 failed + stderr="systemctl not found" |
| 操作超时 | exec.CommandContext 中止，回写 failed |
| 非 root 用户执行 | systemctl 返回权限错误，回写 failed |
| 服务启动后立即崩溃 | exit_code=0 但 status=failed，由后续监控告警 |

### 4.6 配置项

表：服务管理配置项

| Flag | 默认值 | 说明 |
|------|--------|------|
| `--task-timeout` | 120s | 服务操作超时 |
| `--task-max-retries` | 3 | 服务操作失败重试上限 |
| `--agent-shell-whitelist` | "" | systemctl 须在白名单 |

### 4.7 API 端点

表：服务管理 API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/tasks` | type=service, action ∈ {start, stop, restart, enable, disable, status} |
| POST | `/api/v1/tasks/batch` | 批量服务操作 |
| GET | `/api/v1/tasks/{id}/result` | 查询服务操作回执 |

---

## 第5章 状态监控

### 5.1 模块概述

状态监控模块采集并展示 CPU、内存、磁盘、网络、OS、服务、进程指标。控制面通过 Prometheus 文本格式暴露指标（端口 9091），agent 通过心跳上报负载，前端通过 SSE 实时推送设备状态变更。

### 5.2 用例

表：状态监控用例

| 用例 ID | 名称 | 主参与者 | 主流程 |
|---------|------|----------|--------|
| UC-M-01 | Agent 心跳上报 | Agent | 每 10s 上报 {state, cpu, mem_mb} → 控制面更新设备负载 |
| UC-M-02 | Prometheus 指标采集 | Prometheus | GET /metrics（端口 9091）→ 文本格式指标 |
| UC-M-03 | 设备状态实时推送 | 前端 | SSE 订阅 → 设备上下线事件推送 |
| UC-M-04 | 指标访问控制 | 运维工程师 | --metrics-allow-cidr 白名单 → 非授权来源 403 |
| UC-M-05 | 健康检查 | K8s 探针 | GET /healthz → 深度检查 store 连接 |
| UC-M-06 | 就绪检查 | K8s 探针 | GET /readyz → store + leader 租约双条件 |

### 5.3 流程图

图：状态监控数据流示意图

```text
┌──────────┐  心跳(10s)   ┌──────────────┐
│  Agent   │ ────────────▶│  控制面      │
│ cpu/mem  │              │  device.load │
│ disk/net │              └──────┬───────┘
└──────────┘                     │
                                 ├──▶ /metrics (Prometheus 文本)
                                 │
                                 ├──▶ SSE 推送 device_state 事件
                                 │
                                 └──▶ /healthz、/readyz 探针
```

图：Prometheus 指标采集流程图

```text
Prometheus ──GET /metrics──▶ 控制面:9091
                              │
                              ├── CIDR 白名单校验
                              │   └── 非授权 → 403
                              │
                              ▼
                          生成文本格式指标
                              │
                              ├── opsmesh_agent_online (在线 agent 数)
                              ├── opsmesh_task_pending (待领取任务数)
                              ├── opsmesh_task_duration_seconds (任务耗时)
                              ├── opsmesh_queue_depth (队列深度)
                              └── go_* (Go runtime 指标)
                              │
                              ▼
                          200 OK + text/plain
```

### 5.4 业务规则

- **BR-M-01**：agent 每 10s 经 gRPC Heartbeat 上报 {state, cpu, mem_mb}，控制面更新 device.load 与 last_heartbeat。
- **BR-M-02**：`/metrics` 监听独立端口 9091（非主 8080），受 `--metrics-allow-cidr` 白名单控制。
- **BR-M-03**：`/healthz` 为深度健康检查（含 store 连接检查），2 秒超时保护，K8s livenessProbe 用。
- **BR-M-04**：`/readyz` 为就绪检查，条件为 store 可用 + 本实例持有 leader 租约，K8s readinessProbe 用。
- **BR-M-05**：设备心跳超时（默认由 archive-age-min 控制）由 archiveLoop 标记 offline → retired。
- **BR-M-06**：SSE 事件流推送 device_state 变更事件，替代前端 5s 轮询。

### 5.5 边界条件

| 边界场景 | 处理策略 |
|----------|----------|
| Agent 心跳中断 | 超过 archive-age-min 后归档 retired |
| Prometheus 拉取超时 | 2 秒超时保护，返回 503 |
| 非授权来源拉 /metrics | 403 + 审计记录 |
| Store 不可用 | /healthz 返回 503，/readyz 返回 503 |
| 非 leader 副本 | /readyz 返回 503，从 Service endpoints 摘除 |
| 指标基数爆炸 | 仅暴露聚合指标（agent 数/队列深度），不暴露 per-task 指标 |

### 5.6 配置项

表：状态监控配置项

| Flag | 默认值 | 说明 |
|------|--------|------|
| `--metrics-port` | 9091 | Prometheus metrics 端口 |
| `--metrics-allow-cidr` | "" | metrics 访问 CIDR 白名单 |
| `--archive-age-min` | 1440 | 离线超龄归档阈值 |

### 5.7 API 端点

表：状态监控 API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/healthz` | 深度健康检查（K8s liveness） |
| GET | `/readyz` | 就绪检查（K8s readiness） |
| GET | `/metrics` | Prometheus 文本指标（端口 9091） |
| GET | `/api/v1/devices` | 设备清单（含负载信息） |
| GET | `/api/v1/agents` | agent 清单（含 cpu/mem 负载） |
| GET | `/api/v1/events/stream` | SSE 实时推送设备状态变更 |
| gRPC | `Registration.Heartbeat` | agent 心跳上报负载 |

---

## 第6章 告警管理

### 6.1 模块概述

告警管理模块覆盖规则定义、评估、抑制、聚合、通知、异常检测全链路。告警源包括任务死信、agent 离线、自定义规则触发。通知通道支持 Webhook（generic/feishu/dingtalk/slack/企业微信）与邮件（SMTP）。

### 6.2 用例

表：告警管理用例

| 用例 ID | 名称 | 主参与者 | 主流程 |
|---------|------|----------|--------|
| UC-A-01 | 任务死信告警 | 系统 | 任务重试耗尽 → 产出 critical 告警 → 通知通道推送 |
| UC-A-02 | Agent 离线告警 | 系统 | 心跳超时 → 触发 agent_offline 规则 → 产出告警 |
| UC-A-03 | 自定义规则告警 | 运维工程师 | 创建规则（metric/condition/threshold）→ 评估命中 → 产出告警 |
| UC-A-04 | 告警确认 | 运维工程师 | POST /alerts/{id}/ack → status=acked |
| UC-A-05 | 告警静默 | 运维工程师 | POST /alerts/{id}/silence → 指定时长内不再通知 |
| UC-A-06 | Webhook 通知 | 系统 | 告警产出 → POST JSON 到 webhook URL |
| UC-A-07 | 邮件通知 | 系统 | 告警产出 → SMTP 发送邮件 |
| UC-A-08 | 飞书/钉钉通知 | 系统 | 告警产出 → 转换为飞书卡片/钉钉 markdown 格式推送 |

### 6.3 流程图

图：告警产出与通知流程图

```text
┌─────────────┐
│ 告警源      │
│ ┌─────────┐ │
│ │任务死信 │ │
│ ├─────────┤ │
│ │agent离线│ │
│ ├─────────┤ │
│ │规则评估 │ │
│ └─────────┘ │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ 告警产出    │  Alert{severity, title, message, source}
└──────┬──────┘
       │
       ├── 抑制检查（同源 + 时间窗内 → 跳过）
       │
       ├── 聚合（同规则 + 同 severity → 合并）
       │
       ▼
┌─────────────┐
│ 通知分发    │
│ ┌─────────┐ │
│ │Webhook  │ │  generic/feishu/dingtalk/slack/企业微信
│ ├─────────┤ │
│ │Email    │ │  SMTP
│ └─────────┘ │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ 落库 + SSE  │  alerts 表 + event:alert 推送
└─────────────┘
```

### 6.4 业务规则

- **BR-A-01**：任务死信（retry_count = max_retries）自动产出 critical 告警，source=task。
- **BR-A-02**：告警规则字段为 {name, metric, condition, threshold, severity, duration_sec}，评估时检查 metric 值是否满足 condition threshold 持续 duration_sec。
- **BR-A-03**：告警抑制：同源告警在静默时长内不再通知（status=silenced）。
- **BR-A-04**：告警聚合：同规则 + 同 severity 的告警合并为一条，避免通知风暴。
- **BR-A-05**：Webhook URL 域名识别自动覆盖通知类型：含 `slack.com` 走 Slack Block Kit，含 `qyapi.weixin.qq.com` 走企业微信 markdown。
- **BR-A-06**：邮件通道须配置 SMTP host/user/pass/from/to，任一缺失则关闭邮件通道。
- **BR-A-07**：告警确认（ack）后 status=acked，不再触发通知但仍可在面板查看。
- **BR-A-08**：所有告警经审计留痕，告警产出/确认/静默均记录 AuditEvent。

### 6.5 边界条件

| 边界场景 | 处理策略 |
|----------|----------|
| Webhook URL 不可达 | 重试 3 次后放弃，记录失败日志 |
| SMTP 服务不可用 | 跳过邮件通道，仅记录日志 |
| 告警风暴（短时大量产出） | 聚合 + 抑制，避免通知通道过载 |
| 规则 metric 不存在 | 评估跳过，记录警告日志 |
| 静默时长过期 | 自动恢复 active 状态 |
| 通知通道全部关闭 | 告警仍落库 + SSE 推送，仅不外发 |
| 重复确认同一告警 | 幂等，返回 200 |

### 6.6 配置项

表：告警管理配置项

| Flag | 默认值 | 说明 |
|------|--------|------|
| `--alert-webhook-url` | "" | 告警 Webhook 推送 URL |
| `--alert-notifier-type` | generic | 通知类型：generic/feishu/dingtalk |
| `--alert-email-host` | "" | SMTP 主机 |
| `--alert-email-port` | 25 | SMTP 端口 |
| `--alert-email-user` | "" | SMTP 用户名 |
| `--alert-email-pass` | "" | SMTP 密码 |
| `--alert-email-from` | "" | 发件人地址 |
| `--alert-email-to` | "" | 收件人列表（逗号分隔） |

### 6.7 API 端点

表：告警管理 API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/alerts` | 活跃告警列表（severity/status 过滤） |
| POST | `/api/v1/alerts/{id}/ack` | 确认告警 |
| POST | `/api/v1/alerts/{id}/silence` | 静默告警 |
| GET | `/api/v1/alert-rules` | 告警规则列表 |
| POST | `/api/v1/alert-rules` | 创建告警规则 |
| DELETE | `/api/v1/alert-rules/{id}` | 删除告警规则 |
| GET | `/api/v1/events/stream` | SSE 推送 alert 事件 |

---

## 第7章 CMDB

### 7.1 模块概述

CMDB（配置管理数据库）模块管理 CI 类型、CI 实例、关系、属性模板。支持采集、审批、导入导出。CI 实例创建后进入待审批状态，管理员审批后激活。关系支持依赖、包含等类型。

### 7.2 用例

表：CMDB 用例

| 用例 ID | 名称 | 主参与者 | 主流程 |
|---------|------|----------|--------|
| UC-CI-01 | 创建 CI 类型 | 运维工程师 | POST /cmdb/types → 定义属性 schema |
| UC-CI-02 | 创建 CI 实例 | 运维工程师 | POST /cmdb/ci → status=pending → 等待审批 |
| UC-CI-03 | 审批 CI 实例 | 管理员 | POST /cmdb/ci/{id}/approve → status=active |
| UC-CI-04 | 创建关系 | 运维工程师 | POST /cmdb/relations → from_ci → to_ci (depends_on/contains) |
| UC-CI-05 | 创建属性模板 | 运维工程师 | POST /cmdb/attr-templates → 复用属性集 |
| UC-CI-06 | 导出 CI | 运维工程师 | GET /cmdb/ci/export?type=host → CSV |
| UC-CI-07 | 导入 CI | 运维工程师 | POST /cmdb/ci/import (CSV) → 批量创建 |
| UC-CI-08 | 采集 CI | 系统 | agent 上报设备元信息 → 自动创建/更新 CI 实例 |

### 7.3 流程图

图：CI 实例审批流程图

```text
POST /api/v1/cmdb/ci
        │
        ▼
┌──────────────┐
│ 类型校验     │  type 存在 + 属性 schema 校验
└──────┬───────┘
       │
       ▼
┌──────────────┐
│ 创建实例     │  status=pending
└──────┬───────┘
       │
       ▼
┌──────────────┐
│ 等待审批     │
└──────┬───────┘
       │ POST /cmdb/ci/{id}/approve (管理员)
       ▼
┌──────────────┐
│ 激活实例     │  status=active
└──────────────┘
```

图：CI 关系建模示意图

```text
┌─────────┐  depends_on   ┌─────────┐
│  web-01 │ ─────────────▶│ mysql-01│
│  (host) │               │ (mysql) │
└─────────┘               └─────────┘
     │                        │
     │ contains               │ contains
     ▼                        ▼
┌─────────┐               ┌─────────┐
│  app    │               │  data   │
│ (process)│              │ (volume)│
└─────────┘               └─────────┘
```

### 7.4 业务规则

- **BR-CI-01**：CI 类型定义属性 schema（name/type/required），创建实例时按 schema 校验。
- **BR-CI-02**：CI 实例创建后 status=pending，须经管理员审批后激活（status=active）。
- **BR-CI-03**：关系类型包括 depends_on、contains 等，关系为有向边（from_ci → to_ci）。
- **BR-CI-04**：属性模板为可复用属性集，CI 类型可引用模板继承属性。
- **BR-CI-05**：CI 导出为 CSV 格式，按 type 过滤。
- **BR-CI-06**：CI 导入为 CSV multipart 上传，逐行校验 + 批量创建，返回 imported/failed 计数。
- **BR-CI-07**：采集由 agent 上报设备元信息触发，自动创建或更新对应 CI 实例。
- **BR-CI-08**：所有 CI 操作经审计留痕，租户隔离按 X-Tenant-ID 行级过滤。

### 7.5 边界条件

| 边界场景 | 处理策略 |
|----------|----------|
| CI 类型不存在 | 创建实例返回 400 |
| 属性 schema 校验失败 | 创建实例返回 400 + 错误详情 |
| 重复审批 | 幂等，返回 200 |
| 关系形成环 | 由调用方负责检测，CMDB 不强制 |
| 导入 CSV 格式错误 | 跳过错误行，返回 failed 计数 |
| 导入大规模 CI | 受 1 MiB 请求体上限约束 |
| 删除被引用 CI | 返回 409，提示先解除关系 |

### 7.6 配置项

表：CMDB 配置项

| Flag | 默认值 | 说明 |
|------|--------|------|
| `--store` | memory | 持久化后端（CMDB 数据落库） |
| `--multi-schema` | false | 多租户 schema 隔离 |

### 7.7 API 端点

表：CMDB API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| GET/POST | `/api/v1/cmdb/types` | CI 类型列表 / 创建 |
| GET/POST | `/api/v1/cmdb/ci` | CI 实例列表 / 创建（待审批） |
| GET/PUT/DELETE | `/api/v1/cmdb/ci/{id}` | CI 实例详情 / 更新 / 删除 |
| GET | `/api/v1/cmdb/ci/pending` | 待审批 CI 列表 |
| POST | `/api/v1/cmdb/ci/{id}/approve` | 审批 CI 实例 |
| GET | `/api/v1/cmdb/ci/export` | 导出 CI（CSV） |
| POST | `/api/v1/cmdb/ci/import` | 导入 CI（CSV） |
| GET/POST | `/api/v1/cmdb/relations` | 关系列表 / 创建 |
| GET/POST | `/api/v1/cmdb/attr-templates` | 属性模板列表 / 创建 |
| GET | `/api/v1/cmdb/attr-templates/{id}` | 属性模板详情 |

---

## 第8章 作业编排

### 8.1 模块概述

作业编排模块基于 DAG 引擎，支持工作流定义、子工作流、条件分支、超时重试、执行历史。DAG 节点为任务（shell/service/file），边定义执行依赖。创建时自动环检测，触发后按拓扑序调度。

### 8.2 用例

表：作业编排用例

| 用例 ID | 名称 | 主参与者 | 主流程 |
|---------|------|----------|--------|
| UC-W-01 | 创建工作流 | 运维工程师 | POST /workflows → 定义 DAG → 环检测 → 落库 |
| UC-W-02 | 触发工作流 | 运维工程师 | POST /workflows/{id}/trigger → 创建 run → 按拓扑序调度 |
| UC-W-03 | 定时触发 | 系统（leader） | cron 字段 → scheduleLoop 派生 run |
| UC-W-04 | 子工作流 | 运维工程师 | 节点 type=workflow → 嵌套触发子工作流 |
| UC-W-05 | 条件分支 | 运维工程师 | 节点 condition 字段 → 评估决定是否执行 |
| UC-W-06 | 节点超时重试 | 系统 | 节点 timeout + max_retries → 失败重试 |
| UC-W-07 | 查询执行历史 | 运维工程师 | GET /workflows/{id}/runs → 历次执行记录 |

### 8.3 流程图

图：DAG 工作流执行流程图

```text
POST /api/v1/workflows/{id}/trigger
        │
        ▼
┌──────────────┐
│ 创建 run     │  run_id, status=running
└──────┬───────┘
       │
       ▼
┌──────────────┐
│ 拓扑排序     │  Kahn 算法
└──────┬───────┘
       │
       ▼
┌──────────────┐
│ 调度就绪节点 │  入度=0 的节点
└──────┬───────┘
       │
       ├── 条件分支评估 → 跳过不满足条件的节点
       │
       ▼
┌──────────────┐
│ 下发节点任务 │  POST /tasks (节点 task 定义)
└──────┬───────┘
       │
       ├── 超时 → 重试（< max_retries）
       │
       ├── 失败 → 工作流失败（可选回滚）
       │
       ▼
┌──────────────┐
│ 节点完成     │  释放后继节点（入度--）
└──────┬───────┘
       │
       ▼
┌──────────────┐
│ 全部完成     │  run status=success
└──────────────┘
```

图：DAG 节点状态机示意图

```text
pending ──调度──▶ running ──┬──成功──▶ done
                            │
                            ├──失败 + 重试未耗尽──▶ pending (retry_count++)
                            │
                            ├──失败 + 重试耗尽──▶ failed
                            │
                            └──超时──▶ failed (或重试)
```

### 8.4 业务规则

- **BR-W-01**：创建工作流时自动环检测（Kahn 算法或 DFS），存在环则返回 400。
- **BR-W-02**：节点 task 字段为标准 Task 对象（type/command/timeout_sec/max_retries）。
- **BR-W-03**：节点 condition 字段为表达式，评估为 false 时跳过该节点及其后继。
- **BR-W-04**：子工作流节点 type=workflow，嵌套触发子工作流并等待完成。
- **BR-W-05**：定时工作流由 scheduleLoop 派生 run，同周期幂等防重复。
- **BR-W-06**：节点失败后工作流标记失败，可选触发回滚（rollback_on_failure）。
- **BR-W-07**：执行历史记录每次 run 的节点状态、耗时、stdout/stderr 摘要。
- **BR-W-08**：工作流调度由 leader 执行，避免多副本重复派发。

### 8.5 边界条件

| 边界场景 | 处理策略 |
|----------|----------|
| DAG 含环 | 创建时拒绝，返回 400 |
| 节点任务 agent 不存在 | 调度失败，节点 failed |
| 子工作流递归过深 | 限制嵌套层级（默认 8 层） |
| 条件表达式语法错误 | 跳过节点 + 记录警告 |
| 全部节点被条件跳过 | run status=success（空执行） |
| 节点超时且重试耗尽 | 节点 failed → 工作流 failed |
| 工作流并发触发同一模板 | 允许并发，每次触发独立 run |
| 调度过程中控制面重启 | run 状态由 reclaimLoop 复位 |

### 8.6 配置项

表：作业编排配置项

| Flag | 默认值 | 说明 |
|------|--------|------|
| `--task-timeout` | 120s | 节点任务默认超时 |
| `--task-max-retries` | 3 | 节点任务默认重试上限 |
| `--task-lease-sec` | 300 | 节点任务租约 |
| `--leader-ttl-sec` | 15 | leader 租约（调度由 leader 执行） |

### 8.7 API 端点

表：作业编排 API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/workflows` | 工作流列表 |
| POST | `/api/v1/workflows` | 创建工作流（含 DAG 定义，自动环检测） |
| GET | `/api/v1/workflows/{id}` | 工作流详情 |
| POST | `/api/v1/workflows/{id}/trigger` | 手动触发 |
| GET | `/api/v1/workflows/{id}/runs` | 执行历史 |

---

## 第9章 部署管理

### 9.1 模块概述

部署管理模块支持蓝绿、金丝雀、滚动等多种发布策略，覆盖门禁检查、回滚、自动推进、联邦发布。部署计划由多 step 组合（shell/service），fan-out 到目标 agent 并行执行，Reconcile 循环检查期望状态。

### 9.2 用例

表：部署管理用例

| 用例 ID | 名称 | 主参与者 | 主流程 |
|---------|------|----------|--------|
| UC-DP-01 | 创建部署计划 | 运维工程师 | POST /deploys → 定义 steps + strategy + target_agents |
| UC-DP-02 | 滚动发布 | 运维工程师 | strategy=rolling → 分批 fan-out → 每批成功后推进下一批 |
| UC-DP-03 | 蓝绿发布 | 运维工程师 | strategy=blue_green → 部署 green → 切换流量 → 保留 blue |
| UC-DP-04 | 金丝雀发布 | 运维工程师 | strategy=canary → 小流量验证 → 逐步放量 |
| UC-DP-05 | 门禁检查 | 系统 | 每批部署后执行门禁（健康检查/指标阈值）→ 失败停止推进 |
| UC-DP-06 | 回滚 | 运维工程师 | POST /deploys/{id}/rollback → 触发回滚 step |
| UC-DP-07 | 自动推进 | 系统 | 门禁通过 + 间隔 → 自动推进下一批 |
| UC-DP-08 | 联邦发布 | 运维工程师 | 跨网段部署 → 联邦转发到各 peer 控制面 |

### 9.3 流程图

图：滚动部署流程图

```text
POST /api/v1/deploys (strategy=rolling)
        │
        ▼
┌──────────────┐
│ 创建部署计划 │  status=planning
└──────┬───────┘
       │
       ▼
┌──────────────┐
│ 分批 fan-out │  target_agents 按批次分组
└──────┬───────┘
       │
       ▼
┌──────────────┐
│ 执行第 N 批 │  并行下发 step 任务
└──────┬───────┘
       │
       ▼
┌──────────────┐
│ 等待批次完成 │
└──────┬───────┘
       │
       ├── 失败 + rollback_on_failure → 触发回滚
       │
       ▼
┌──────────────┐
│ 门禁检查     │  健康检查 + 指标阈值
└──────┬───────┘
       │
       ├── 门禁失败 → 停止推进 + 告警
       │
       ├── 门禁通过 + 自动推进 → 下一批
       │
       ▼
┌──────────────┐
│ 全部批次完成 │  status=success
└──────────────┘
```

图：蓝绿部署示意图

```text
┌─────────┐      ┌─────────┐
│  Blue   │      │  Green  │
│ (current)│     │ (new)   │
└─────────┘      └─────────┘
     ▲                │
     │                │ 部署完成
     │                ▼
     │           ┌─────────┐
     │           │ 流量切换 │
     │           └────┬────┘
     │                │
     │                ▼
     │           ┌─────────┐
     └───────────│  Green  │ (current)
                 │  Blue   │ (保留回滚)
                 └─────────┘
```

### 9.4 业务规则

- **BR-DP-01**：部署计划由 steps 数组定义，每 step 为标准 Task（shell/service）。
- **BR-DP-02**：strategy ∈ {rolling, blue_green, canary}，决定 fan-out 策略。
- **BR-DP-03**：滚动发布按批次分组 target_agents，每批成功 + 门禁通过后推进下一批。
- **BR-DP-04**：蓝绿发布部署 green 完成后切换流量，保留 blue 用于回滚。
- **BR-DP-05**：金丝雀发布先小流量验证，逐步放量至全量。
- **BR-DP-06**：门禁检查包括健康检查（/healthz）与指标阈值（Prometheus 查询）。
- **BR-DP-07**：rollback_on_failure=true 时部署失败自动触发回滚 step。
- **BR-DP-08**：联邦发布经 FederationManager.ForwardTask 转发到各 peer 控制面。
- **BR-DP-09**：Reconcile 循环周期检查部署期望状态与实际状态偏差。

### 9.5 边界条件

| 边界场景 | 处理策略 |
|----------|----------|
| 目标 agent 部分不存在 | 跳过不存在 agent，标注失败项 |
| 门禁检查超时 | 视为门禁失败，停止推进 |
| 回滚 step 失败 | 部署 status=failed + critical 告警 |
| 联邦 peer 不可达 | 跳过该 peer，标注 unreachable |
| 并发触发同一部署 | 409 冲突，拒绝重复触发 |
| 部署过程中控制面重启 | Reconcile 复位进行中状态 |
| 全量回滚后再次回滚 | 409，提示已回滚 |

### 9.6 配置项

表：部署管理配置项

| Flag | 默认值 | 说明 |
|------|--------|------|
| `--task-timeout` | 120s | 部署 step 超时 |
| `--task-max-retries` | 3 | step 失败重试上限 |
| `--federation-peers` | "" | 联邦 peer 地址（联邦发布） |
| `--federation-secret` | "" | 联邦签名密钥 |

### 9.7 API 端点

表：部署管理 API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/deploys` | 部署列表（status 过滤） |
| POST | `/api/v1/deploys` | 创建部署计划 |
| GET | `/api/v1/deploys/{id}` | 部署详情（含各 step 状态） |
| POST | `/api/v1/deploys/{id}/rollback` | 回滚部署 |

---

## 第10章 K8s 管理

### 10.1 模块概述

K8s 管理模块基于 client-go 集成，支持多集群 CRUD、Pod/Deployment/Service/ConfigMap/Node 管理。集群经 kubeconfig 接入，kubeconfig 落盘经 AES-256-GCM 加密（`--encryption-key`）。

### 10.2 用例

表：K8s 管理用例

| 用例 ID | 名称 | 主参与者 | 主流程 |
|---------|------|----------|--------|
| UC-K-01 | 注册集群 | 运维工程师 | POST /k8s/clusters → kubeconfig 加密落盘 → 测试连接 |
| UC-K-02 | 测试集群连接 | 运维工程师 | POST /k8s/clusters/{id}/test → 返回 version + nodes |
| UC-K-03 | 查询 Pod 列表 | 运维工程师 | GET /k8s/clusters/{id}/pods?namespace=xxx |
| UC-K-04 | 查询 Pod 日志 | 运维工程师 | GET /k8s/clusters/{id}/pods/{name}/logs |
| UC-K-05 | 删除 Pod | 运维工程师 | DELETE /k8s/clusters/{id}/pods/{name} |
| UC-K-06 | 查询 Deployment | 运维工程师 | GET /k8s/clusters/{id}/deployments |
| UC-K-07 | 查询 Service | 运维工程师 | GET /k8s/clusters/{id}/services |
| UC-K-08 | 查询 ConfigMap | 运维工程师 | GET /k8s/clusters/{id}/configmaps |
| UC-K-09 | 删除集群 | 运维工程师 | DELETE /k8s/clusters/{id} → 清理 kubeconfig |

### 10.3 流程图

图：K8s 集群接入流程图

```text
POST /api/v1/k8s/clusters
        │
        ▼
┌──────────────┐
│ 校验 kubeconfig│
└──────┬───────┘
       │
       ▼
┌──────────────┐
│ AES-256-GCM  │  --encryption-key 非空时加密
│   加密落盘   │
└──────┬───────┘
       │
       ▼
┌──────────────┐
│ 测试连接     │  client-go 连接 api_server
└──────┬───────┘
       │
       ├── 连接成功 → status=connected
       │
       └── 连接失败 → status=error
       │
       ▼
┌──────────────┐
│ 返回集群信息 │  version, nodes
└──────────────┘
```

图：K8s 资源代理查询示意图

```text
前端 ──GET /k8s/clusters/{id}/pods──▶ 控制面
                                       │
                                       ├── 解密 kubeconfig
                                       │
                                       ▼
                                   client-go
                                       │
                                       ▼
                                   K8s api_server
                                       │
                                       ▼
                                   返回 Pod 列表
```

### 10.4 业务规则

- **BR-K-01**：集群注册须提供 kubeconfig（内容或文件路径），控制面解析后建立 client-go 客户端。
- **BR-K-02**：kubeconfig 落盘经 AES-256-GCM 加密（`--encryption-key` 非空时），空则明文存储（生产禁用）。
- **BR-K-03**：所有 K8s 资源操作经指定集群代理，按 namespace 隔离。
- **BR-K-04**：集群连接测试返回 version + nodes 数，用于验证可达性。
- **BR-K-05**：删除集群时清理 kubeconfig 与客户端缓存。
- **BR-K-06**：Pod 日志查询支持 container + tailLines 参数。
- **BR-K-07**：多集群并行管理，每集群独立 client-go 客户端。
- **BR-K-08**：所有 K8s 操作经审计留痕，租户隔离按 X-Tenant-ID。

### 10.5 边界条件

| 边界场景 | 处理策略 |
|----------|----------|
| kubeconfig 格式错误 | 注册返回 400 |
| api_server 不可达 | 测试连接返回 ok=false |
| 加密密钥缺失 | 明文落盘 + 警告日志 |
| namespace 不存在 | 返回空列表 |
| Pod 不存在 | 返回 404 |
| 权限不足（RBAC） | client-go 返回 403，透传给前端 |
| 集群并发操作 | client-go 客户端线程安全 |
| 删除集群后查询资源 | 返回 404 |

### 10.6 配置项

表：K8s 管理配置项

| Flag | 默认值 | 说明 |
|------|--------|------|
| `--encryption-key` | "" | kubeconfig AES-256-GCM 加密密钥（32 字节） |

### 10.7 API 端点

表：K8s 管理 API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/k8s/clusters` | 集群列表 |
| POST | `/api/v1/k8s/clusters` | 注册集群 |
| GET | `/api/v1/k8s/clusters/{id}` | 集群详情 |
| DELETE | `/api/v1/k8s/clusters/{id}` | 删除集群 |
| POST | `/api/v1/k8s/clusters/{id}/test` | 测试连接 |
| GET | `/api/v1/k8s/clusters/{id}/pods` | Pod 列表 |
| GET | `/api/v1/k8s/clusters/{id}/pods/{name}/logs` | Pod 日志 |
| DELETE | `/api/v1/k8s/clusters/{id}/pods/{name}` | 删除 Pod |
| GET | `/api/v1/k8s/clusters/{id}/deployments` | Deployment 列表 |
| GET | `/api/v1/k8s/clusters/{id}/deployments/{name}` | Deployment 详情 |
| GET | `/api/v1/k8s/clusters/{id}/services` | Service 列表 |
| GET | `/api/v1/k8s/clusters/{id}/configmaps` | ConfigMap 列表 |

---

## 第11章 日志检索

### 11.1 模块概述

日志检索模块支持 Memory/SQL/Loki/ES 四种后端，提供统一查询语法与 offset 分页。memory/sql 模式下日志由控制面代写入，loki/es 模式下日志由 agent 直接推送、控制面仅查询。

### 11.2 用例

表：日志检索用例

| 用例 ID | 名称 | 主参与者 | 主流程 |
|---------|------|----------|--------|
| UC-L-01 | 追加日志 | Agent | gRPC 上报 → 控制面代写入（memory/sql） |
| UC-L-02 | 关键词检索 | 运维工程师 | GET /logs?keyword=error → 全文匹配 |
| UC-L-03 | 时间范围检索 | 运维工程师 | GET /logs?from=&to= → 时间过滤 |
| UC-L-04 | 设备/级别过滤 | 运维工程师 | GET /logs?deviceID=&level=error |
| UC-L-05 | Loki 后端查询 | 运维工程师 | GET /logs → 控制面转发到 Loki API |
| UC-L-06 | ES 后端查询 | 运维工程师 | GET /logs → 控制面转发到 ES API |

### 11.3 流程图

图：日志检索后端选择流程图

```text
GET /api/v1/logs
        │
        ▼
┌──────────────┐
│ 解析查询参数 │  deviceID/level/source/keyword/from/to/limit/offset
└──────┬───────┘
       │
       ▼
┌──────────────┐
│ 后端分发     │
└──────┬───────┘
       │
       ├── memory → 内存倒排索引查询
       │
       ├── sql    → SQL WHERE + ORDER BY timestamp
       │
       ├── loki   → 转发到 Loki API (/loki/api/v1/query_range)
       │
       └── es     → 转发到 ES API (/_search)
       │
       ▼
┌──────────────┐
│ 统一响应格式 │  [{id, device_id, level, source, message, timestamp}]
└──────────────┘
```

### 11.4 业务规则

- **BR-L-01**：日志后端由 `--log-backend`（或 `--log-store`）选择：memory/sql/loki/es。
- **BR-L-02**：memory/sql 模式下日志由控制面代写入（agent gRPC 上报时）。
- **BR-L-03**：loki/es 模式下日志由 agent 直接推送，控制面仅查询（避免转发开销）。
- **BR-L-04**：查询参数支持 deviceID/agentID/level/source/keyword/from/to/limit/offset。
- **BR-L-05**：分页采用 offset 方式（非 cursor），适合中规模检索。
- **BR-L-06**：memory 后端维护倒排索引（keyword → log IDs）加速关键词查询。
- **BR-L-07**：sql 后端按 timestamp 索引，支持时间范围与关键词 LIKE 查询。
- **BR-L-08**：所有日志查询经租户隔离，按 X-Tenant-ID 过滤 device_id。

### 11.5 边界条件

| 边界场景 | 处理策略 |
|----------|----------|
| Loki/ES 后端不可达 | 返回 503 + 错误详情 |
| 查询时间范围过大 | 由后端限制（Loki max_query_length） |
| 关键词为空 | 返回时间范围内全部日志 |
| offset 超出总数 | 返回空列表 |
| 日志量巨大 | 分页 + limit 上限保护 |
| memory 后端重启 | 日志丢失（仅适合演示） |
| 并发写入与查询 | memory 后端经互斥锁保护 |

### 11.6 配置项

表：日志检索配置项

| Flag | 默认值 | 说明 |
|------|--------|------|
| `--log-backend` | memory | 日志后端：memory/sql/loki/es |
| `--log-store` | memory | 日志后端别名（显式设置覆盖 --log-backend） |
| `--loki-endpoint` | "" | Loki API endpoint |
| `--es-endpoint` | "" | Elasticsearch endpoint |
| `--es-index` | opsmesh-logs | ES 索引名 |

### 11.7 API 端点

表：日志检索 API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/logs` | 日志检索（多过滤参数） |
| POST | `/api/v1/logs` | 追加日志（agent gRPC 上报代写入） |

---

## 第12章 中间件部署

### 12.1 模块概述

中间件部署模块预置 15+ 模板（MySQL/Redis/Kafka/Nginx 等），支持 docker 与 systemd 两种部署方式。模板含部署参数 schema，实例化后在指定 agent 上执行部署/卸载。

### 12.2 用例

表：中间件部署用例

| 用例 ID | 名称 | 主参与者 | 主流程 |
|---------|------|----------|--------|
| UC-MW-01 | 查询模板列表 | 运维工程师 | GET /middleware-templates?category=web |
| UC-MW-02 | 部署中间件 | 运维工程师 | POST /middleware-templates/{id}/deploy → 实例化 → 下发部署任务 |
| UC-MW-03 | 查询实例列表 | 运维工程师 | GET /middleware-instances?agentID= |
| UC-MW-04 | 卸载中间件 | 运维工程师 | POST /middleware-instances/{id}/uninstall → 下发卸载任务 |
| UC-MW-05 | 自定义模板 | 运维工程师 | POST /middleware-templates → 定义部署参数 schema |

### 12.3 流程图

图：中间件部署流程图

```text
POST /api/v1/middleware-templates/{id}/deploy
        │
        ▼
┌──────────────┐
│ 加载模板     │  含部署参数 schema
└──────┬───────┘
       │
       ▼
┌──────────────┐
│ 参数校验     │  按 schema 校验 params
└──────┬───────┘
       │
       ▼
┌──────────────┐
│ 实例化       │  创建 middleware_instance 记录
└──────┬───────┘
       │
       ▼
┌──────────────┐
│ 渲染部署命令 │  docker run / systemctl start
└──────┬───────┘
       │
       ▼
┌──────────────┐
│ 下发任务     │  POST /tasks (shell)
└──────┬───────┘
       │
       ▼
┌──────────────┐
│ 返回实例 ID │  instance_id + task_id
└──────────────┘
```

### 12.4 业务规则

- **BR-MW-01**：模板含 category（web/cache/mq/db 等）、risk（low/medium/high）、version 字段。
- **BR-MW-02**：部署参数 schema 定义参数类型与默认值，部署时按 schema 校验。
- **BR-MW-03**：部署方式为 docker（`docker run`）或 systemd（unit 文件 + `systemctl start`）。
- **BR-MW-04**：实例化创建 middleware_instance 记录，关联 agent_id 与模板 id。
- **BR-MW-05**：卸载触发对应清理命令（`docker rm` / `systemctl stop + disable`）。
- **BR-MW-06**：所有部署/卸载操作经审计留痕，租户隔离按 X-Tenant-ID。

### 12.5 边界条件

| 边界场景 | 处理策略 |
|----------|----------|
| 模板不存在 | 部署返回 404 |
| 参数校验失败 | 部署返回 400 + 错误详情 |
| 目标 agent 离线 | 部署任务 pending，等待 agent 上线 |
| docker 命令缺失 | 部署任务 failed + stderr |
| 端口冲突 | 部署任务 failed，提示端口占用 |
| 卸载不存在的实例 | 返回 404 |
| 重复部署同一模板 | 允许，创建新实例 |

### 12.6 配置项

表：中间件部署配置项

| Flag | 默认值 | 说明 |
|------|--------|------|
| `--task-timeout` | 120s | 部署任务超时 |
| `--task-max-retries` | 3 | 部署失败重试上限 |

### 12.7 API 端点

表：中间件部署 API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/middleware-templates` | 模板列表（category/risk 过滤） |
| GET | `/api/v1/middleware-templates/{id}` | 模板详情（含参数 schema） |
| POST | `/api/v1/middleware-templates/{id}/deploy` | 部署中间件 |
| GET | `/api/v1/middleware-instances` | 实例列表（agentID/category 过滤） |
| POST | `/api/v1/middleware-instances/{id}/uninstall` | 卸载实例 |

---

## 第13章 OS 优化

### 13.1 模块概述

OS 优化模块预置 14+ 模板（内核/网络/安全/SSH/磁盘等），按 category 与 risk 分级。模板含具体执行步骤，在指定 agent 上执行。高风险模板须人工确认。

### 13.2 用例

表：OS 优化用例

| 用例 ID | 名称 | 主参与者 | 主流程 |
|---------|------|----------|--------|
| UC-OS-01 | 查询模板列表 | 运维工程师 | GET /os-templates?category=kernel&risk=low |
| UC-OS-02 | 执行优化模板 | 运维工程师 | POST /os-templates/{id}/execute → 下发 shell 任务 |
| UC-OS-03 | 内核参数调优 | 运维工程师 | 选择 kernel 模板 → 传入 swappiness 等参数 |
| UC-OS-04 | 网络参数调优 | 运维工程师 | 选择 network 模板 → 调整 tcp_tw_reuse 等 |
| UC-OS-05 | 安全加固 | 运维工程师 | 选择 security 模板 → 高风险须确认 |
| UC-OS-06 | SSH 加固 | 运维工程师 | 选择 ssh 模板 → 禁用 root 登录等 |

### 13.3 流程图

图：OS 优化执行流程图

```text
POST /api/v1/os-templates/{id}/execute
        │
        ▼
┌──────────────┐
│ 加载模板     │  含执行步骤 + risk 等级
└──────┬───────┘
       │
       ├── risk=high → 要求确认参数（confirm=true）
       │
       ▼
┌──────────────┐
│ 渲染执行命令 │  模板步骤 + params 替换
└──────┬───────┘
       │
       ▼
┌──────────────┐
│ 下发 shell   │  POST /tasks (type=shell)
│   任务       │
└──────┬───────┘
       │
       ▼
┌──────────────┐
│ 返回任务 ID │  task_id + status=pending
└──────────────┘
```

### 13.4 业务规则

- **BR-OS-01**：模板按 category 分类：kernel/network/security/performance/ssh/disk。
- **BR-OS-02**：模板按 risk 分级：low/medium/high，high 须人工确认。
- **BR-OS-03**：模板含具体执行步骤（shell 命令序列），params 替换占位符。
- **BR-OS-04**：执行经 POST /tasks 下发 shell 任务，复用任务执行生命周期。
- **BR-OS-05**：内核参数调优须重启生效的模板须标注（require_reboot=true）。
- **BR-OS-06**：所有 OS 优化操作经审计留痕，租户隔离按 X-Tenant-ID。

### 13.5 边界条件

| 边界场景 | 处理策略 |
|----------|----------|
| 模板不存在 | 执行返回 404 |
| 高风险未确认 | 执行返回 400，要求 confirm=true |
| 参数缺失 | 执行返回 400 + 错误详情 |
| 目标 agent 离线 | 任务 pending，等待 agent 上线 |
| 内核参数无效 | shell 命令返回非 0，任务 failed |
| 需重启模板 | 标注 require_reboot，由调用方决定重启时机 |

### 13.6 配置项

表：OS 优化配置项

| Flag | 默认值 | 说明 |
|------|--------|------|
| `--task-timeout` | 120s | 优化任务超时 |
| `--task-max-retries` | 3 | 优化失败重试上限 |
| `--agent-shell-whitelist` | "" | shell 命令白名单 |

### 13.7 API 端点

表：OS 优化 API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/os-templates` | 模板列表（category/risk/os 过滤） |
| GET | `/api/v1/os-templates/{id}` | 模板详情（含执行步骤） |
| POST | `/api/v1/os-templates/{id}/execute` | 在指定 agent 上执行模板 |

---

## 第14章 多租户

### 14.1 模块概述

多租户模块提供租户隔离、schema 隔离、配额、RBAC 能力。租户隔离通过 `X-Tenant-ID` 头做行级过滤，schema 隔离为每租户路由独立 MySQL schema，配额限制每租户资源使用。

### 14.2 用例

表：多租户用例

| 用例 ID | 名称 | 主参与者 | 主流程 |
|---------|------|----------|--------|
| UC-TN-01 | 租户行级隔离 | 系统 | 所有查询/写入按 X-Tenant-ID 过滤 |
| UC-TN-02 | Schema 隔离 | 运维工程师 | --multi-schema=true → 每租户独立 schema |
| UC-TN-03 | 配额限制 | 系统 | 每租户设备/任务/告警数上限校验 |
| UC-TN-04 | RBAC 头注入 | 网关 | X-Tenant-ID/X-User-Id/X-User-Roles 注入 |
| UC-TN-05 | 越权拒绝 | 系统 | 缺失 X-Tenant-ID + require-auth → 401 |
| UC-TN-06 | 令牌闭环 | Agent | install token 携带租户，不依赖网关头 |

### 14.3 流程图

图：租户隔离数据流示意图

```text
客户端 ──▶ 网关 (APISIX/Envoy)
              │
              ├── 鉴权校验
              │
              ├── 注入身份头
              │   ├── X-Tenant-ID: t1
              │   ├── X-User-Id: u-001
              │   └── X-User-Roles: admin,ops
              │
              ▼
          控制面
              │
              ├── authctx 提取身份
              │
              ├── store 层行级过滤
              │   └── WHERE tenant_id = 't1'
              │
              ▼
          MySQL / Memory
```

图：Schema 隔离路由示意图

```text
租户 t1 ──▶ schema: opsmesh_tenant_t1
租户 t2 ──▶ schema: opsmesh_tenant_t2
租户 t3 ──▶ schema: opsmesh_tenant_t3

schema 前缀: --schema-prefix (默认 opsmesh_tenant_)
```

### 14.4 业务规则

- **BR-TN-01**：所有业务表含 tenant_id 列，查询/写入按 X-Tenant-ID 行级过滤。
- **BR-TN-02**：`--require-auth=true` 时缺失 X-Tenant-ID 头返回 401。
- **BR-TN-03**：`--multi-schema=true` 时每租户路由独立 MySQL schema（schema 名 = `--schema-prefix` + tenantID）。
- **BR-TN-04**：schema 隔离仅 `--store=mysql` 时生效，memory 模式不支持。
- **BR-TN-05**：配额限制每租户设备数/任务数/告警数，超限返回 429。
- **BR-TN-06**：令牌闭环例外：agent 首次注册携带 install token，从 token 提取租户，不依赖网关头。
- **BR-TN-07**：审计事件按 tenant_id 隔离，查询时自动过滤。
- **BR-TN-08**：联邦转发须携带原租户身份头，peer 控制面按原租户隔离。

### 14.5 边界条件

| 边界场景 | 处理策略 |
|----------|----------|
| 缺失 X-Tenant-ID + require-auth | 401 拒绝 |
| 越权访问其他租户数据 | 行级过滤，返回 404 |
| Schema 不存在 | 自动创建（首次访问） |
| 配额超限 | 返回 429 + 错误详情 |
| 联邦转发缺失租户头 | 拒绝转发 + 审计 |
| B1 token 租户与网关头不一致 | 以 token 租户为准（首次注册） |

### 14.6 配置项

表：多租户配置项

| Flag | 默认值 | 说明 |
|------|--------|------|
| `--require-auth` | false | 要求网关注入 X-Tenant-ID |
| `--multi-schema` | false | 开启多租户 schema 隔离 |
| `--schema-prefix` | opsmesh_tenant_ | schema 名前缀 |
| `--store` | memory | 持久化后端（schema 隔离须 mysql） |
| `--production` | false | 生产模式（默认开启 require-auth） |

### 14.7 API 端点

表：多租户 API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/me` | 当前身份信息（解析 X-Tenant-ID/X-User-Id/X-User-Roles） |
| \* | `/api/v1/*` | 所有业务接口经 X-Tenant-ID 行级隔离 |

---

## 第15章 用户权限

### 15.1 模块概述

用户权限模块提供用户/角色/权限三表 RBAC，JWT 双 Token 签发（AT/RT HttpOnly Cookie），注册审批流程。预置 24 条默认权限与 admin 角色。支持两条身份路径：内置用户中心与网关注入身份头。

### 15.2 用例

表：用户权限用例

| 用例 ID | 名称 | 主参与者 | 主流程 |
|---------|------|----------|--------|
| UC-| 用户注册 | 用户 | POST /auth/register → status=pending → 等待审批 |
| UC-| 注册审批 | 管理员 | POST /users/{id} → status=active |
| UC-| 用户登录 | 用户 | POST /auth/login → 签发 AT/RT Cookie |
| UC-| Token 刷新 | 前端 | POST /auth/refresh → 旋转 AT/RT |
| UC-| 用户登出 | 用户 | POST /auth/logout → 撤销 RT |
| UC-| 修改密码 | 用户 | POST /auth/change-password |
| UC-| 创建角色 | 管理员 | POST /roles → 绑定权限 |
| UC-| 角色分配 | 管理员 | PUT /users/{id} → 绑定角色 |
| UC-| 网关注入身份 | 网关 | 注入 X-Tenant-ID/X-User-Id/X-User-Roles |

### 15.3 流程图

图：用户注册审批流程图

```text
POST /api/v1/auth/register
        │
        ▼
┌──────────────┐
│ 校验参数     │  username/password/email
└──────┬───────┘
       │
       ▼
┌──────────────┐
│ 创建用户     │
└──────┬───────┘
       │
       ├── --allow-public-register=true → status=active + 签发 token
       │
       └── 否则 → status=pending
       │
       ▼
┌──────────────┐
│ 等待审批     │
└──────┬───────┘
       │ 管理员 PUT /users/{id} {status: active}
       ▼
┌──────────────┐
│ 激活用户     │  status=active
└──────────────┘
```

图：JWT 双 Token 流程图

```text
POST /auth/login
        │
        ├── loginGuard 限流（10 突发 / 3s 补 1）
        │
        ├── 失败 5 次 → 锁 15min
        │
        ▼
   校验密码
        │
        ▼
   签发 AT (1h) + RT (7d)
        │
        ▼
   Set-Cookie: at (HttpOnly)
              rt (HttpOnly)
        │
        ▼
   返回 access_token + refresh_token

AT 过期 → POST /auth/refresh (凭 RT) → 旋转 AT + RT
登出    → POST /auth/logout → 撤销 RT
```

### 15.4 业务规则

- **BR-**：用户/角色/权限三表，预置 24 条默认权限 + admin 角色 + 默认 admin 用户。
- **BR-**：JWT 签发密钥为 `--jwt-secret`（HS256），多副本须一致，生产强制 ≥32 字节。
- **BR-**：AT 短期（1h），RT 长期（7d），均经 HttpOnly Cookie 下发。
- **BR-**：登录防爆破：令牌桶限流（每 IP 10 突发 / 每 3s 补 1）+ 连续失败 5 次锁 15min。
- **BR-**：用户名不存在场景同样计入限流，避免账号枚举。
- **BR-**：注册受 `--public-register` 控制（false 时关闭公开注册，仅管理员可创建）。
- **BR-**：`--allow-public-register=true` 时注册即激活并签发 token（仅演示/内网受信）。
- **BR-**：Token 刷新旋转 AT + RT，旧 RT 撤销。
- **BR-**：网关注入身份头路径与内置用户中心可同时启用，网关头优先。
- **BR-**：`--cookie-secure=true` 时 Cookie 仅经 HTTPS 传输，生产模式默认 true。

### 15.5 边界条件

| 边界场景 | 处理策略 |
|----------|----------|
| 登录连续失败 5 次 | 锁账号 15min |
| 限流触发 | 429 + Retry-After |
| JWT 密钥 <32 字节 + 生产 | fail-fast 拒绝启动 |
| RT 过期 | 返回 401，要求重新登录 |
| RT 已撤销 | 返回 401 |
| 注册时用户名冲突 | 返回 409 |
| 公开注册关闭 | POST /auth/register 返回 403 |
| Cookie 跨域 | 须配置 SameSite + Secure |
| 网关头与 JWT 同时存在 | 网关头优先 |

### 15.6 配置项

表：用户权限配置项

| Flag | 默认值 | 说明 |
|------|--------|------|
| `--jwt-secret` | "" | JWT 签发密钥（HS256） |
| `--jwt-public-key` | "" | JWT 验签公钥 PEM（RS256，外部 IdP） |
| `--jwt-issuer` | "" | 预期 JWT issuer |
| `--public-register` | true | 允许公开注册（须审批） |
| `--allow-public-register` | false | 允许公开注册免审批 |
| `--require-auth` | false | 要求鉴权 |
| `--cookie-secure` | false | Cookie Secure 标志 |
| `--session-store` | memory | 会话后端：memory/redis |
| `--trust-proxy` | false | 信任反向代理 XFF |
| `--production` | false | 生产模式（默认开启 require-auth + cookie-secure） |

### 15.7 API 端点

表：用户权限 API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/auth/register` | 用户注册 |
| POST | `/api/v1/auth/login` | 用户登录 |
| GET | `/api/v1/auth/me` | 当前登录用户 |
| POST | `/api/v1/auth/logout` | 登出 |
| POST | `/api/v1/auth/refresh` | 刷新 Token |
| POST | `/api/v1/auth/change-password` | 修改密码 |
| GET/POST | `/api/v1/users` | 用户列表 / 创建 |
| GET/PUT/DELETE | `/api/v1/users/{id}` | 用户详情 / 更新 / 删除 |
| GET/POST | `/api/v1/roles` | 角色列表 / 创建 |
| GET/PUT/DELETE | `/api/v1/roles/{id}` | 角色详情 / 更新 / 删除 |
| GET/POST | `/api/v1/permissions` | 权限列表 / 创建 |
| GET | `/api/v1/me` | 当前身份信息（网关注入模式） |

---

## 第16章 联邦

### 16.1 模块概述

联邦模块支持跨网段任务转发、联邦设备视图、联邦验签。企业多终端环境按网段割裂为多个控制面，联邦实现跨段任务转发与设备视图聚合。转发通道硬化为 mTLS + HMAC 签名，防伪造/防重放。

### 16.2 用例

表：联邦用例

| 用例 ID | 名称 | 主参与者 | 主流程 |
|---------|------|----------|--------|
| UC-F-01 | 跨网段任务转发 | 运维工程师 | POST /federation/forward/task → 转发到 peer 控制面 |
| UC-F-02 | 联邦设备视图 | 运维工程师 | GET /federation/devices → 聚合所有 peer 设备清单 |
| UC-F-03 | 联邦 peer 状态 | 运维工程师 | GET /federation/peers → 含可达性 + 延迟 |
| UC-F-04 | 联邦验签 | 系统 | verifyFederationRequest 校验 HMAC 签名 + 时间戳 |
| UC-F-05 | 联邦 mTLS | 系统 | 独立 mTLS 监听端口，强制客户端持证 |
| UC-F-06 | 联邦发布 | 运维工程师 | 跨网段部署 → 联邦转发到各 peer |

### 16.3 流程图

图：联邦任务转发流程图

```text
控制面 A (段 1)                控制面 B (段 2)
   │                              │
   │  POST /federation/forward    │
   │  ────────────────────────────▶│
   │  X-Federation-Forwarded: 1   │
   │  HMAC 签名 + mTLS            │
   │                              │
   │                              ├── verifyFederationRequest
   │                              │   ├── 校验 HMAC 签名
   │                              │   ├── 校验时间戳 ±5min
   │                              │   └── 提取租户身份
   │                              │
   │                              ├── 执行任务
   │                              │
   │  ◀────────────────────────────│
   │  200 OK + task_id            │
   │                              │
```

图：联邦设备视图聚合示意图

```text
GET /api/v1/federation/devices
        │
        ▼
┌──────────────┐
│ 遍历 peers   │  并行查询
└──────┬───────┘
       │
       ├── peer1 → 200 → devices[]
       │
       ├── peer2 → 200 → devices[]
       │
       └── peer3 → 超时 → error: unreachable
       │
       ▼
┌──────────────┐
│ 聚合响应     │  [{peer, devices}, {peer, error}]
└──────────────┘
```

### 16.4 业务规则

- **BR-F-01**：联邦启用条件为 `--federation-peers` 非空，注册联邦 API。
- **BR-F-02**：所有 peer 须共享同一 `--federation-secret`，用于 HMAC 签名/验签。
- **BR-F-03**：`verifyFederationRequest` 仅对携带 `X-Federation-Forwarded: 1` 的请求验签。
- **BR-F-04**：签名 = HMAC-SHA256(method + path + 时间戳 + 身份头)，时间戳偏差窗 ±5min 防重放。
- **BR-F-05**：`--federation-port>0` 时启用独立 mTLS 监听，强制 `RequireAndVerifyClientCert`。
- **BR-F-06**：联邦 mTLS 证书独立于主 gRPC TLS 证书（`--federation-tls-cert/key/ca`）。
- **BR-F-07**：联邦设备视图聚合所有 peer 设备清单，不可达 peer 标注 error。
- **BR-F-08**：联邦转发须携带原租户身份头，peer 按原租户隔离。
- **BR-F-09**：启用独立监听须同时配置联邦 TLS 证书，否则 `Validate()` 报错。

### 16.5 边界条件

| 边界场景 | 处理策略 |
|----------|----------|
| Peer 不可达 | 标注 unreachable，不阻塞其他 peer |
| 签名不符 | 401 拒绝 + 审计 |
| 时间戳超窗 | 401 拒绝（防重放） |
| 密钥缺失 | 401 拒绝 |
| mTLS 客户端无证书 | TLS 握手拒绝 |
| 联邦端口未配置 TLS | Validate() 报错，拒绝启动 |
| 转发任务目标 agent 不存在 | peer 返回 404 |
| 并发转发大量任务 | 由 peer 控制面限流保护 |

### 16.6 配置项

表：联邦配置项

| Flag | 默认值 | 说明 |
|------|--------|------|
| `--federation-peers` | "" | 联邦 peer 地址列表（逗号分隔） |
| `--federation-secret` | "" | 联邦共享 HMAC 密钥 |
| `--federation-tls-cert` | "" | 联邦 mTLS 证书 |
| `--federation-tls-key` | "" | 联邦 mTLS 私钥 |
| `--federation-ca` | "" | 联邦 mTLS 对端 CA |
| `--federation-port` | 0 | 联邦独立 mTLS 监听端口 |

### 16.7 API 端点

表：联邦 API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/federation/peers` | 联邦 peer 列表（含可达性） |
| POST | `/api/v1/federation/forward/task` | 跨网段转发任务 |
| GET | `/api/v1/federation/devices` | 联邦设备视图 |

---

## 第17章 密钥管理

### 17.1 模块概述

密钥管理模块提供 Env/File/Vault/Chain 四种 SecretProvider，按 Chain 顺序解析密钥。用于管理 JWT 签发密钥、联邦密钥、kubeconfig 加密密钥等敏感配置。

### 17.2 用例

表：密钥管理用例

| 用例 ID | 名称 | 主参与者 | 主流程 |
|---------|------|----------|--------|
| UC-SC-01 | Env Provider | 系统 | 从环境变量读取密钥 |
| UC-SC-02 | File Provider | 系统 | 从文件读取密钥（PEM/文本） |
| UC-SC-03 | Vault Provider | 系统 | 从 HashiCorp Vault 读取密钥 |
| UC-SC-04 | Chain Provider | 系统 | 按 Chain 顺序解析，首个成功返回 |
| UC-SC-05 | JWT 密钥解析 | 系统 | Chain 解析 --jwt-secret |
| UC-SC-06 | 联邦密钥解析 | 系统 | Chain 解析 --federation-secret |
| UC-SC-07 | kubeconfig 加密密钥 | 系统 | Chain 解析 --encryption-key |

### 17.3 流程图

图：SecretProvider Chain 解析流程图

```text
ResolveSecret(key)
        │
        ▼
┌──────────────────────────┐
│ Chain = [Env, File, Vault]│
└──────────┬───────────────┘
           │
           ▼
       ┌──────┐
       │ Env  │ → 读取 OPSMESH_<KEY>
       └──┬───┘
          │
          ├── 成功 → 返回值
          │
          ▼
       ┌──────┐
       │ File │ → 读取 /etc/opsmesh/<key>
       └──┬───┘
          │
          ├── 成功 → 返回值
          │
          ▼
       ┌──────┐
       │ Vault│ → Vault API 读取
       └──┬───┘
          │
          ├── 成功 → 返回值
          │
          ▼
       全部失败 → 返回错误
```

### 17.4 业务规则

- **BR-SC-01**：SecretProvider 接口定义 `Resolve(key) (value, error)`。
- **BR-SC-02**：Env Provider 从环境变量读取（前缀 `OPSMESH_`）。
- **BR-SC-03**：File Provider 从文件读取（PEM/文本，支持路径配置）。
- **BR-SC-04**：Vault Provider 从 HashiCorp Vault 读取（须配置 Vault 地址 + Token）。
- **BR-SC-05**：Chain Provider 按顺序调用各 Provider，首个成功返回。
- **BR-SC-06**：JWT 密钥、联邦密钥、kubeconfig 加密密钥均经 Chain 解析。
- **BR-SC-07**：生产强制 JWT 密钥 ≥32 字节，否则 fail-fast。
- **BR-SC-08**：密钥不落审计日志，仅记录解析成功/失败。

### 17.5 边界条件

| 边界场景 | 处理策略 |
|----------|----------|
| 所有 Provider 失败 | 返回错误，启动失败 |
| Env 变量未设置 | 跳过 Env Provider |
| 文件不存在 | 跳过 File Provider |
| Vault 不可达 | 跳过 Vault Provider |
| 密钥长度不足 | 生产 fail-fast，开发警告 |
| 密钥含敏感字符 | 不落审计，仅记录解析结果 |
| Chain 顺序错误 | 按配置顺序，调用方负责 |

### 17.6 配置项

表：密钥管理配置项

| Flag | 默认值 | 说明 |
|------|--------|------|
| `--jwt-secret` | "" | JWT 签发密钥（经 Chain 解析） |
| `--federation-secret` | "" | 联邦 HMAC 密钥 |
| `--encryption-key` | "" | kubeconfig 加密密钥 |
| `--grpc-signature-key` | "" | gRPC HMAC 签名密钥 |
| `--provision-secret` | "" | install token 签名密钥 |

### 17.7 API 端点

表：密钥管理 API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| - | - | 密钥管理为内部模块，不暴露公开 API；密钥经 Chain 解析后供各模块使用 |

---

## 第18章 SSE 实时推送

### 18.1 模块概述

SSE 实时推送模块基于事件总线与 EventSource，替代前端 5s 轮询。推送设备状态变更、任务状态变更、告警产出等事件。支持降级轮询（SSE 不可用时回退）。

### 18.2 用例

表：SSE 实时推送用例

| 用例 ID | 名称 | 主参与者 | 主流程 |
|---------|------|----------|--------|
| UC-SSE-01 | 订阅事件流 | 前端 | GET /events/stream → 长连接 → 推送事件 |
| UC-SSE-02 | 任务状态推送 | 系统 | 任务状态变更 → event:task_status → 推送 |
| UC-SSE-03 | 设备状态推送 | 系统 | 设备上下线 → event:device_state → 推送 |
| UC-SSE-04 | 告警推送 | 系统 | 告警产出 → event:alert → 推送 |
| UC-SSE-05 | 降级轮询 | 前端 | SSE 不可用 → 回退 5s 轮询 |
| UC-SSE-06 | 事件总线扩展 | 系统 | event-bus=kafka → 事件经 Kafka 推送 |

### 18.3 流程图

图：SSE 事件流架构图

```text
┌─────────────┐
│  事件源     │
│ ┌─────────┐ │
│ │任务状态 │ │
│ ├─────────┤ │
│ │设备状态 │ │
│ ├─────────┤ │
│ │告警产出 │ │
│ └─────────┘ │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  事件总线   │  noop / log / kafka
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  SSE Hub    │  维护客户端连接池
└──────┬──────┘
       │
       ├── 客户端 1 (EventSource)
       │
       ├── 客户端 2 (EventSource)
       │
       └── 客户端 N (EventSource)
       │
       ▼
   text/event-stream
   event: task_status
   data: {...}
```

图：SSE 降级轮询流程图

```text
前端 EventSource 连接
        │
        ├── 成功 → SSE 长连接接收事件
        │
        ├── 失败/超时 → 降级轮询
        │
        ▼
┌──────────────┐
│ 5s 轮询模式  │
└──────┬───────┘
       │
       ├── GET /tasks?status=running
       │
       ├── GET /alerts?status=active
       │
       └── GET /devices?state=online
       │
       ▼
   周期刷新前端状态
```

### 18.4 业务规则

- **BR-SSE-01**：SSE 端点为 `GET /api/v1/events/stream`，Content-Type 为 `text/event-stream`。
- **BR-SSE-02**：事件类型包括 task_status、device_state、alert。
- **BR-SSE-03**：事件经事件总线（noop/log/kafka）分发，SSE Hub 维护客户端连接池。
- **BR-SSE-04**：SSE 连接为长连接，客户端 EventSource 自动重连。
- **BR-SSE-05**：SSE 不可用时前端降级为 5s 轮询（GET /tasks、/alerts、/devices）。
- **BR-SSE-06**：事件推送按租户隔离，仅推送当前租户事件。
- **BR-SSE-07**：事件总线为 kafka 时，事件经 Kafka topic 推送，支持多副本消费。
- **BR-SSE-08**：SSE Hub 连接数受限于控制面配置（默认无上限，由文件描述符约束）。

### 18.5 边界条件

| 边界场景 | 处理策略 |
|----------|----------|
| 客户端断开 | SSE Hub 移除连接，停止推送 |
| 控制面重启 | 客户端 EventSource 自动重连 |
| 事件总线 kafka 不可达 | 降级为 noop，事件仅本地推送 |
| 大量客户端连接 | 受文件描述符上限约束 |
| 事件推送速率过高 | SSE Hub 内置缓冲区，溢出丢弃旧事件 |
| 跨租户事件 | 按租户隔离，不推送给其他租户客户端 |
| 网络抖动 | EventSource 内置重试机制 |

### 18.6 配置项

表：SSE 实时推送配置项

| Flag | 默认值 | 说明 |
|------|--------|------|
| `--event-bus` | noop | 事件总线类型：noop/log/kafka |
| `--kafka-brokers` | "" | Kafka brokers（event-bus=kafka 时） |
| `--kafka-topic` | "" | Kafka topic |
| `--http-port` | 8080 | HTTP 端口（SSE 复用） |

### 18.7 API 端点

表：SSE 实时推送 API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/events/stream` | SSE 事件流（task_status/device_state/alert） |

---

## 附录

### 附录 A：模块依赖关系

图：18 个模块依赖关系示意图

```text
┌─────────────────────────────────────────────────────┐
│                    基础设施层                       │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐          │
│  │ F14 多租户│  │ F15 用户 │  │ F17 密钥 │          │
│  │          │  │   权限   │  │   管理   │          │
│  └──────────┘  └──────────┘  └──────────┘          │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐          │
│  │ F18 SSE  │  │ F16 联邦 │  │  事件总线 │          │
│  └──────────┘  └──────────┘  └──────────┘          │
└─────────────────────────────────────────────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────┐
│                    核心能力层                       │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐          │
│  │ F1 设备  │  │ F2 任务  │  │ F5 状态  │          │
│  │   纳管   │  │   执行   │  │   监控   │          │
│  └──────────┘  └──────────┘  └──────────┘          │
│  ┌──────────┐  ┌──────────┐                        │
│  │ F3 配置  │  │ F4 服务 │                        │
│  │   下发   │  │   管理   │                        │
│  └──────────┘  └──────────┘                        │
└─────────────────────────────────────────────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────┐
│                    业务编排层                       │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐          │
│  │ F7 CMDB  │  │ F8 作业  │  │ F9 部署  │          │
│  │          │  │   编排   │  │   管理   │          │
│  └──────────┘  └──────────┘  └──────────┘          │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐          │
│  │ F10 K8s  │  │ F11 日志 │  │ F6 告警  │          │
│  └──────────┘  └──────────┘  └──────────┘          │
│  ┌──────────┐  ┌──────────┐                        │
│  │ F12 中间 │  │ F13 OS  │                        │
│  │   件     │  │   优化   │                        │
│  └──────────┘  └──────────┘                        │
└─────────────────────────────────────────────────────┘
```

### 附录 B：配置项分组对照

表：119 个 flag 分组对照表

| 分组 | 数量 | 涵盖模块 |
|------|------|----------|
| 基础配置 | 9 | 全局 |
| 存储配置 | 6 | F14 多租户、全模块 |
| 安全配置 | 16 | F14、F15、F16、F17 |
| 网络配置 | 6 | F16 联邦 |
| 告警配置 | 8 | F6 告警 |
| 日志配置 | 5 | F11 日志 |
| K8s/调度配置 | 13 | F2 任务、F5 监控、F8 编排、F18 SSE |
| 纳管配置 | 11 | F1 设备纳管 |
| 其他 | 5 | 全局 |

### 附录 C：API 端点统计

表：API 端点统计表

| 模块 | HTTP 端点数 | gRPC 方法数 |
|------|-------------|-------------|
| F1 设备纳管 | 8 | 1 |
| F2 任务执行 | 5 | 4 |
| F3 配置下发 | 3 | 2 |
| F4 服务管理 | 3 | 0 |
| F5 状态监控 | 7 | 1 |
| F6 告警管理 | 6 | 0 |
| F7 CMDB | 11 | 0 |
| F8 作业编排 | 5 | 0 |
| F9 部署管理 | 4 | 0 |
| F10 K8s 管理 | 12 | 0 |
| F11 日志检索 | 2 | 0 |
| F12 中间件部署 | 5 | 0 |
| F13 OS 优化 | 3 | 0 |
| F14 多租户 | 2 | 0 |
| F15 用户权限 | 13 | 0 |
| F16 联邦 | 3 | 0 |
| F17 密钥管理 | 0 | 0 |
| F18 SSE 实时推送 | 1 | 0 |
| **合计** | **93** | **8** |

### 附录 D：相关文档索引

表：相关文档索引

| 文档 | 说明 |
|------|------|
| `README.md` | 项目总览与功能矩阵 |
| `docs/api-reference.md` | API 端点详细参考 |
| `docs/api-specification.md` | API 规范定义 |
| `docs/architecture.md` | 架构设计 |
| `docs/database-design.md` | 数据库表结构设计 |
| `docs/deployment-guide.md` | 部署指南 |
| `docs/product-design.md` | 产品设计 |
| `docs/product-roadmap.md` | 产品路线图 |
| `docs/security-mechanism.md` | 安全机制 |
| `docs/sse-protocol.md` | SSE 协议 |
| `docs/tech-selection.md` | 技术选型 |
| `docs/flag-matrix.md` | flag 矩阵 |