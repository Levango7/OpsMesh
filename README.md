# OpsMesh — 网段运维中枢

**OpsMesh** 是私有化单中心 B/S 自动化部署与运维平台。服务部署到某网段后，整段网络打通的设备自动纳管，各设备可并行执行各自的自动化任务（shell 脚本/服务管理/文件分发），支持失败重试/死信/取消/定时周期/告警等完整任务生命周期。

底层的设备管理底座复用 **蓝鲸 GSE 社区版**（零 license，零外部依赖）。

---

## 架构概览

```
┌─────────────────────────────────────────────────────┐
│                   控制面 (controlplane)               │
│  ┌──────────────────────────────────────────────────┐│
│  │ HTTP :8080 (B/S 仪表盘 + REST API)                ││
│  │ gRPC :9090 (agent 注册/心跳/拉任务/上报/取消信号)   ││
│  │ Metrics :9091 (Prometheus 文本格式)                 ││
│  └──────────────────────────────────────────────────┘│
│  │                                                   │
│  │  Registry (Store 薄封装)                            ││
│  │  ┌──────────┐  ┌──────────┐                       ││
│  │  │MemoryStore│  │ SQLStore │ (可插拔持久化)          ││
│  │  └──────────┘  └──────────┘                       ││
│  │         │              │                           ││
│  │   默认（零依赖）    MySQL + Redis（等保本地化）         ││
│  └─────────────────────────────────────────────────────┘
                │ gRPC 9090 (JSON codec)
                ▼
┌─────────────────────────────────────────────────────┐
│  Agent 1           Agent 2     ...    Agent N         │
│  ┌──────────┐     ┌──────────┐     ┌──────────┐     │
│  │ worker pool│   │ worker pool│   │ worker pool│    │
│  │ shell/     │   │ shell/     │   │ shell/     │    │
│  │ service/   │   │ service/   │   │ service/   │    │
│  │ file exec  │   │ file exec  │   │ file exec  │    │
│  └──────────┘     └──────────┘     └──────────┘     │
│  10.30.0.0/24      10.30.0.0/24     10.30.0.0/24    │
└─────────────────────────────────────────────────────┘
```

### 通信模型

| 通道 | 协议 | 端口 | 用途 |
|---|---|---|---|
| 注册 | gRPC (JSON) | 9090 | agent 上报元信息，服务端盖章租户、分配 agentID |
| 心跳 | gRPC (JSON) | 9090 | agent 每 10s 上报在线状态与负载 |
| 拉任务 | gRPC (JSON) | 9090 | agent 原子领取 pending 任务（多副本安全） |
| 上报结果 | gRPC (JSON) | 9090 | 任务执行完毕回写 stdout/stderr/exitCode |
| 取消信号 | gRPC (JSON) | 9090 | agent 轮询 PollCancels，立即中止正在执行的任务 |
| 仪表盘 | HTTP | 8080 | B/S 看板：设备/任务/告警/审计/纳管操作 |
| 指标 | HTTP | 9091 | Prometheus 文本格式观测指标 |

---

## 功能矩阵

| 领域 | 功能 | 状态 |
|---|---|---|
| **任务** | Shell 命令执行 | ✅ |
| | 系统服务管理 (systemctl start/stop/restart/status) | ✅ |
| | 文件分发（原子写入 + rename） | ✅ |
| | 超时自动中止 (exec.CommandContext) | ✅ |
| | 失败重试（可配上限，超限进死信） | ✅ |
| | 任务取消（pending 拦截 + running 强杀） | ✅ |
| | 定时/周期调度（5 字段 cron） | ✅ |
| | 批量下发 | ✅ |
| **设备** | Agent 即设备（默认，零依赖） | ✅ |
| | 真实网段发现 (TCP 存活扫描) | ✅ --discover |
| | 候选设备纳管（discovered → provisioning → onboarded） | ✅ B1 |
| | 设备退役（离线/超龄自动归档） | ✅ |
| **HA** | 多副本 leader 选举 (leader_lease 表) | ✅ |
| | 超期任务自动回收 (reclaimLoop) | ✅ |
| | 多控制面 agent 端 failover (逗号分隔地址) | ✅ |
| **安全** | 租户行级隔离 (TenantID 行锁) | ✅ |
| | RBAC 头注入 (X-Tenant-ID / X-User-Id / X-User-Roles) | ✅ |
| | 生产模式 (--production：require-auth 默认开) | ✅ |
| | gRPC TLS / mTLS | ✅ |
| | 审计 100% 留痕 (AuditEvent → audit_log / memory ring) | ✅ |
| | 审计可查 (租户/动作/时间窗过滤) | ✅ |
| **告警** | 任务死信 → critical 告警 | ✅ |
| | 告警面板 / HTTP 查询 | ✅ |
| **观测** | Prometheus 文本指标 (agent/队列深度/duration) | ✅ |
| | /healthz 健康检查 | ✅ |
| **事件** | 可插拔事件总线 (noop/log/kafka) | ✅ |
| **部署** | 单二进制双模式 (--mode=controlplane|agent) | ✅ |
| | 零依赖启动 (MemoryStore, 无 MySQL/Redis) | ✅ |
| | 生产部署 (MySQL + Redis, 多副本) | ✅ |
| | 容器镜像 (多阶段 Dockerfile) | ✅ |
| | GitHub Actions CI (lint/test/security/image) | ✅ |

---

## 快速启动（零依赖，30 秒）

```bash
# 1. 编译
go build -o opsmesh ./cmd/opsmesh

# 2. 启动控制面（默认 memory store，无需 MySQL/Redis）
./opsmesh --mode=controlplane

# 3. 新终端，启动 agent（注册到控制面）
./opsmesh --mode=agent --segment=seg-a --control-addr=http://127.0.0.1:8080

# 4. 打开浏览器访问 http://127.0.0.1:8080
#    看到一个 agent 已上线，设备已纳管。
```

### 演示模式

控制面加 `--demo` 启动，每个 agent 注册时自动预置一条 `uname -a` 示例任务，即刻体验下发→执行→上报闭环：

```bash
./opsmesh --mode=controlplane --demo
```

### 配置速查

```bash
# 所有配置项
./opsmesh --help
# 查看版本
./opsmesh --version
```

---

## 生产部署（MySQL + Redis + TLS + 多副本）

```bash
# 1. 准备 MySQL 和 Redis（可复用，ops_device 库自动建表）
mysql -e "CREATE DATABASE IF NOT EXISTS ops_device"

# 2. 启动控制面（两个副本，共享同一 MySQL）
./opsmesh --mode=controlplane \
  --store=mysql \
  --mysql-dsn="user:pass@tcp(mysql:3306)/ops_device?charset=utf8mb4" \
  --redis-addr="redis:6379" \
  --replicas=2 \
  --tls-cert=/etc/opsmesh/tls.crt \
  --tls-key=/etc/opsmesh/tls.key \
  --client-ca=/etc/opsmesh/ca.crt \
  --production \
  --provision-secret="change-me-to-a-random-64-hex"

# 3. 启动 agent（--control-addrs 逗号分隔多地址，HA failover）
./opsmesh --mode=agent --segment=seg-a \
  --control-addrs="cp1:9090,cp2:9090" \
  --tls-cert=/etc/opsmesh/tls.crt \
  --tls-key=/etc/opsmesh/tls.key
```

### Kubernetes Helm

OpsMesh 提供 Helm Chart（`deploy/helm/opsmesh/`）和 `values-production.yaml` overlay，支持：
- 控制面 Deployment + Service
- 多副本（`replicas: 2`）+ leader 选举
- MySQL + Redis StatefulSet 自动部署
- Argo CD ApplicationSet 网段批量渲染

---

## IAM 与租户隔离

OpsMesh 内核**不自研登录/SSO/用户管理**，而是采用 **网关注入身份头** 的经典 Sidecar 模式：

```
客户端 → APISIX / Envoy (auth 校验) → OpsMesh 控制面
                     │
                     ├─ X-Tenant-ID: t1
                     ├─ X-User-Id: u-001
                     └─ X-User-Roles: admin,ops
```

| 头 | 用途 |
|---|---|
| `X-Tenant-ID` | 行级隔离键：设备/任务/审计/告警全部有 tenant_id，查询自动过滤 |
| `X-User-Id` | 审计事件记录操作人 |
| `X-User-Roles` | MVP 记录占位，供网关级 RBAC 消费 |

**`--require-auth`** 开关：生产开启后，缺失 `X-Tenant-ID` 的请求被直接拒绝（401）；
开发/内网可关闭以降低心智负担。

**B1 令牌闭环**例外：agent 首次注册时携带一次性 install token（HMAC-SHA256 签名），
服务端 `ConsumeToken` 校验通过后从 token 中提取租户，不依赖网关身份头
（因为新安装的 agent 尚不知道其网关租户身份）。

---

## 任务生命周期

```
                  ┌──────────┐
                  │  pending  │ ◄── 下发 (CreateTask / FireDueSchedules)
                  └────┬─────┘
                       │
                 ┌─────▼──────┐
                 │   running   │ ◄── ClaimTask (原子领取：pending→running)
                 └─────┬──────┘
                       │
             ┌─────────┴──────────┐
             ▼                    ▼
       ┌─────────┐          ┌──────────┐
       │   done   │          │  failed   │ ◄── 重试耗尽，进死信
       └─────────┘          └────┬─────┘
                                 │
                           ┌─────▼──────┐
                           │ dead_letter │ ◄── critical 告警产出
                           └────────────┘

取消路径：pending → cancelled（运行前拦截）
          running → cancelled（经 PollCancels 信号强杀 worker）
```

| 状态 | 含义 |
|---|---|
| pending | 等待 agent 领取（定时调度派生的实例也是 pending） |
| running | 已被某 agent 领取，正在执行 |
| done | 执行成功（exitCode=0） |
| failed | 失败且重试耗尽（enter dead letter） |
| cancelled | 人工取消（pending 拦截 / running 强杀） |

### F2 失败重试 / 死信

任务的 `max_retries`（默认 3）控制失败重试次数：
- 失败且 `retry_count < max_retries` → 复位 pending（retry_count++），重新入队
- 失败且达上限 → 置 failed + dead_letter=true → 产出 critical 告警 → 可在告警面板查看

### F4 定时/周期调度

任务的 `schedule` 字段可填 5 字段 cron 表达式（如 `*/5 * * * *` 每 5 分钟）。
控制面 `scheduleLoop` 周期评估所有模板任务（有 schedule 无 parentID 的为模板），
到点时派生一个 pending 实例（parentID 指向模板），支持同周期幂等防重复。

### F3 任务取消

- API: `POST /api/v1/tasks/{id}/cancel` 或 gRPC `CancelTask`
- pending 任务：状态改为 cancelled，不会进入 agent 领取
- running 任务：控制面置 cancelled；agent 侧 `cancelLoop` 每 2s 轮询 `PollCancels`，
  命中后将对应 worker 的 context 取消 → exec.CommandContext 立即中止子进程，
  worker 丢弃结果不回写 store（避免误翻 done/failed/死信）。

---

## B1 自动纳管流程

```
1. 网段发现 (--discover) → 扫描存活主机 → 落候选设备 (Managed=false)

2. 人工/自动化触发纳管：
   POST /api/v1/devices/{id}/provision
   → 签发一次性 install token（15 分钟有效）
   → 设备状态 → provisioning
   → 返回 { installToken, bootstrap: "curl ... | sh -s -- --token=<tok>" }

3. Operator 在目标机上执行 bootstrap 命令：
   → 下载并安装 OpsMesh agent
   → agent 以 --install-token=<tok> 启动

4. agent 携带 token 回注册：
   gRPC Register {
     installToken: "...",
     hostname: "h1",
     segment: "seg-a",
   }
   → 服务端 ConsumeToken 校验（限时 + 一次性）
   → 翻转候选设备 Managed=true, State=online
   → 设备正式纳入运维管理，可下发任务
```

---

## HA Leader 选举

多副本控制面共享同一 MySQL（`leader_lease` 表）做分布式选主：

- 每个进程启动时生成唯一 `instanceID = hostname-pid-nanotimestamp`
- `leaderLoop` 每 5s 续租（默认 15s TTL）
- 仅 leader 执行：`reclaimLoop`（回收超期任务）、`scheduleLoop`（定时调度）、`archiveLoop`（超龄设备归档）
- `--leader-ttl-sec`（租约 TTL）和 `--leader-tick-sec`（续租周期）可调

**MemoryStore 单实例**：恒为 leader；config 已拒绝 `memory+replicas>1`。

Agent 端多控制面 failover：`--control-addrs="cp1:9090,cp2:9090"`，客户端按序重连。

---

## HTTP API 速查

### 仪表盘

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/` | B/S 仪表盘（设备/任务双表 + 详情抽屉 + 5s 轮询） |
| GET | `/api/v1/devices` | 设备清单（按网段分组） |
| GET | `/api/v1/devices/{id}` | 设备详情（含任务结果） |
| DELETE | `/api/v1/devices/{id}` | 退役/下线设备 |
| POST | `/api/v1/devices/{id}/provision` | **B1 纳管**：签发 install token，返回 bootstrap |
| GET | `/api/v1/agents` | agent 清单 |
| GET | `/api/v1/me` | 当前身份信息 |
| GET | `/api/v1/tasks` | 任务列表（支持 `?status=pending` 过滤） |
| POST | `/api/v1/tasks` | 下发任务（单条，租户隔离 + 审计） |
| POST | `/api/v1/tasks/batch` | 批量下发（逐台查找 agent + 租户校验） |
| GET | `/api/v1/tasks/{id}/cancel` | 取消任务 |
| GET | `/api/v1/tasks/{id}/result` | 查询单条结果 |
| GET | `/api/v1/alerts` | 告警列表（M7） |
| GET | `/api/v1/audits` | 审计事件（P0-4 可查：?tenant=&action=&from=&to=&limit=） |
| GET | `/healthz` | 健康检查 |
| GET | `/metrics` | Prometheus 文本指标 |

**租户隔离**：require-auth 开启时，所有查询/写入操作按 `X-Tenant-ID` 头自动过滤，
越权返回 403/404。

### gRPC 方法

| 服务 | 方法 | 说明 |
|---|---|---|
| `/opsmesh.v1.Registration` | `Register` | 注册 agent，携带 InstallToken 时可自动纳管候选设备 |
| | `Heartbeat` | 上报在线状态与负载 |
| | `PullTasks` | 原子领取下一条 pending 任务 |
| | `ReportResult` | 上报任务执行结果（成功/失败/重试/死信） |
| | `CancelTask` | 取消指定任务（服务端按租户隔离） |
| | `PollCancels` | agent 轮询本机被取消的任务 ID |

---

## 配置参考

| flag | 默认值 | 环境变量 | 说明 |
|---|---|---|---|
| `--mode` | controlplane | OPSMESH_MODE | controlplane / agent |
| `--store` | memory | OPSMESH_STORE | memory / mysql |
| `--mysql-dsn` | "" | OPSMESH_MYSQL_DSN | MySQL DSN（store=mysql 必填） |
| `--redis-addr` | "" | OPSMESH_REDIS_ADDR | Redis 地址（store=mysql 推荐） |
| `--production` | false | OPSMESH_PRODUCTION | 生产模式：require-auth 默认开，无 TLS 告警 |
| `--require-auth` | false | OPSMESH_REQUIRE_AUTH | 生产模式下默认 true，缺失租户头拒绝 |
| `--provision-secret` | "" | OPSMESH_PROVISION_SECRET | B1 token 签名密钥（多副本须一致） |
| `--install-token` | "" | OPSMESH_INSTALL_TOKEN | agent 携带的 install token（bootstrap 注入） |
| `--tls-cert` | "" | OPSMESH_TLS_CERT | 服务端证书路径 |
| `--tls-key` | "" | OPSMESH_TLS_KEY | 私钥路径 |
| `--client-ca` | "" | OPSMESH_CLIENT_CA | mTLS 客户端 CA |
| `--http-port` | 8080 | OPSMESH_HTTP_PORT | HTTP(B/S) 端口 |
| `--grpc-port` | 9090 | OPSMESH_GRPC_PORT | gRPC 端口 |
| `--metrics-port` | 9091 | OPSMESH_METRICS_PORT | metrics 端口 |
| `--control-addr` | http://127.0.0.1:8080 | OPSMESH_CONTROL_ADDR | 控制面 HTTP 地址（agent 用） |
| `--control-addrs` | "" | OPSMESH_CONTROL_ADDRS | HA 多控制面地址（逗号分隔） |
| `--segment` | default | OPSMESH_SEGMENT | agent 所属网段 |
| `--discover` | false | OPSMESH_DISCOVER | 开启真实网段发现 |
| `--segment-cidr` | "" | OPSMESH_SEGMENT_CIDR | 扫描网段 |
| `--task-timeout` | 120s | OPSMESH_TASK_TIMEOUT | 单任务执行超时 |
| `--shutdown-timeout` | 15s | OPSMESH_SHUTDOWN_TIMEOUT | 优雅退出窗口 |
| `--worker-concurrency` | 4 | OPSMESH_WORKER_CONCURRENCY | agent worker 池并发度 |
| `--task-max-retries` | 3 | OPSMESH_TASK_MAX_RETRIES | 任务失败重试上限 |
| `--task-lease-sec` | 300 | OPSMESH_TASK_LEASE_SEC | 任务租约秒（超期回收） |
| `--replicas` | 1 | OPSMESH_REPLICAS | 控制面副本数 |
| `--leader-ttl-sec` | 15 | OPSMESH_LEADER_TTL_SEC | 选主租约 TTL |
| `--leader-tick-sec` | 5 | OPSMESH_LEADER_TICK_SEC | 选主续租周期 |
| `--archive-age-min` | 1440 | OPSMESH_ARCHIVE_AGE_MIN | 离线设备自动归档分钟数 |
| `--event-bus` | noop | OPSMESH_EVENT_BUS | 事件总线类型：noop/log/kafka |
| `--data-dir` | ./data | OPSMESH_DATA_DIR | agent 身份文件目录 |
| `--demo` | false | OPSMESH_DEMO | 演示模式：预置示例任务 |

---

## 从源码构建

```bash
# 依赖：Go 1.22+
git clone <repo>
cd opsmesh-src

# 构建
go mod tidy
go build -o opsmesh ./cmd/opsmesh

# 构建（含 kafka 支持的事件总线）
go build -tags kafka -o opsmesh ./cmd/opsmesh

# 测试
go test -timeout 300s ./...

# Docker
docker build -t opsmesh:latest .
```

### go.sum 注意事项

kafka-go 须钉 `v0.4.48`（最后兼容 Go 1.22 的版本；`v0.4.49+` 要求 Go ≥ 1.23）。
运行 `go test -tags kafka` 前确保已 `go mod tidy`。

---

## 等保三级合规对照

| 要求 | OpsMesh 实现 |
|---|---|
| 数据本地化 | MySQL 私有化部署（--store=mysql），数据不出机房 |
| 100% 审计留痕 | AuditEvent 入 audit_log 表 / memory ring（上限 10000），查询接口 /api/v1/audits |
| 审计≥6 月 | 运维侧定期导出 audit_log（DELETE 旧数据前先备份） |
| RBAC 隔离 | 网关注入 X-Tenant-ID，控制面/存储层行级过滤（BELONGS_TO tenant） |
| 访问控制 | --require-auth 拒绝未鉴权请求；gRPC 网关注入租户头 |
| 入侵检测 | 任务命令来源受限（仅控制面下发），shell 执行经 exec.CommandContext |
| 通信加密 | gRPC TLS / mTLS（--tls-cert, --tls-key, --client-ca） |

---

## 开发指引

```
cmd/opsmesh/              ← 入口 main：解析 --mode 分派 controlplane / agent
internal/
├── agent/                ← agent 运行时（注册/心跳/worker 池/执行器）
├── authctx/              ← HTTP 头 / gRPC metadata 身份提取
├── config/               ← 统一配置（flag + env 兜底）
├── controlplane/         ← 控制面（HTTP 路由/gRPC server/Registry/dashboard）
├── cron/                 ← 5 字段 cron 表达式匹配
├── discover/             ← TCP 存活扫描（网段发现）
├── domain/               ← 纯领域模型 + 防腐层 mapper
├── events/               ← 可插拔事件总线（noop/log/kafka）
├── grpcx/                ← gRPC ServiceDesc / JSON codec / 消息类型
├── logx/                 ← slog 封装 + traceID
├── metrics/              ← 零依赖 Prometheus 文本指标
├── proto/                ← 共享数据类型（AgentInfo/DeviceInfo/Task/…）
├── store/                ← Store 接口 + MemoryStore + SQLStore
├── tlsutil/              ← gRPC TLS / mTLS 工具
└── version/              ← 构建版本注入
```

---

## License

内部项目，私有部署。蓝鲸 GSE 社区版 — 零 license，零外部依赖。
