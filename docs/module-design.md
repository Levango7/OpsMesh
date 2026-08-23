# OpsMesh 模块设计文档

## 第1章 概述

本文档对 OpsMesh 内核 `internal/` 下 30 个 Go 包进行系统化模块设计说明，覆盖每个包的职责、关键接口、核心数据结构、关键算法、并发安全策略与扩展点。文档面向架构 review、新成员 onboarding 与跨团队协作，作为代码导航的"地图"。

### 1.1 文档范围

- 涵盖范围：`F:\Nexus\OpsMesh\internal\` 下全部 30 个包
- 分组维度：按领域职责划分为核心 / 运维 / 告警 / 数据 / 安全 / 基础 / 其他 共 7 组
- 信息来源：每个包的 `*.go` 源文件（不含 `_test.go`）的包注释与关键类型/接口定义
- 不涵盖：第三方依赖、`cmd/` 入口、`pkg/` 公共库、前端资源

### 1.2 阅读约定

- 表中"测试覆盖率"列标注该包是否存在 `*_test.go` 以及大致测试密度（✓ 有测试 / ✓✓ 充分测试 / — 无测试）
- 接口签名采用 Go 语法简化表达，省略 `context.Context` 等显式参数的完整 import 路径
- "扩展点"指包设计上预留的可插拔位置，通常表现为接口参数或函数选项模式

### 1.3 顶层架构

OpsMesh 采用控制面 / 数据面分离的双模式单二进制架构：

- 控制面（`--mode=controlplane`）：HTTP 8080（B/S 仪表盘 + REST API）+ gRPC 9090（agent 通道）+ metrics 9091
- 数据面（`--mode=agent`）：经 gRPC 9090 注册 / 心跳 / 拉任务 / 上报结果，本地 `os/exec` 执行任务

30 个 internal 包按领域分层组合，控制面通过 `controlplane.Server` 装配各域 handler，agent 通过 `agent.Agent` 装配 gRPC 客户端 + worker 池。

## 第2章 包总览表

下表汇总 30 个 internal 包的核心属性，详细设计见第3章。

表：internal 包总览对照表

| # | 包名 | 领域组 | 职责 | 关键类型 | 主要依赖包 | 测试覆盖率 |
|---|------|--------|------|----------|------------|-----------|
| 1 | controlplane | 核心 | 控制面服务装配：HTTP+gRPC+metrics 三端口 | `Server` | alertengine/approval/cmdb/config/cron/deploy/events/helm/k8s/logstore/logx/metrics/notify/orchestration/otelx/proto/secrets/store/tlsutil | ✓✓ |
| 2 | agent | 核心 | 数据面 agent：gRPC 通道 + worker 池 + 任务执行 | `Agent`/`GRPCClient`/`runState` | circuitbreaker/config/discovery/grpcx/logx/otelx/proto | ✓✓ |
| 3 | store | 核心 | 可插拔持久化抽象 + Memory/SQL/MultiSchema 实现 | `Store`/`MemoryStore`/`SQLStore`/`MultiSchemaStore`/`SessionStore` | proto | ✓✓ |
| 4 | domain | 核心 | 纯领域模型 + 状态机行为 + 防腐映射 | `Task`/`Device`/`Alert`/`Agent` | — | ✓✓ |
| 5 | config | 核心 | 统一配置：flag + env 兜底 | `Config` | — | ✓✓ |
| 6 | provision | 运维 | 自动纳管 SSH 推送 agent | `PushAndExec` | — | ✓ |
| 7 | deploy | 运维 | M3 部署中心：滚动/金丝雀/蓝绿 + 联邦 | `Handler`/`DeployTask`/`Dispatcher` | authctx/proto | ✓✓ |
| 8 | helm | 运维 | Helm Release 生命周期管理（CLI 适配） | `ReleaseManager`/`RepoManager`/`Release` | — | ✓✓ |
| 9 | k8s | 运维 | K8s 多集群连接管理（client-go 封装） | `K8sClient`/`ClusterManager` | k8s.io/client-go | ✓ |
| 10 | discover | 运维 | 网段存活扫描（TCP-connect 探测） | `Sweep` | — | ✓ |
| 11 | discovery | 运维 | 服务注册发现抽象 + 静态/noop 实现 | `ServiceDiscovery`/`Balancer` | — | ✓✓ |
| 12 | alertengine | 告警 | 告警规则引擎 + 评估器 + 静默/抑制/聚合 | `Engine`/`AlertRule`/`Evaluator`/`Silencer`/`Aggregator`/`AlertInhibitor`/`AnomalyEngine` | — | ✓✓ |
| 13 | notify | 告警 | 多渠道通知 + 模板 + 重试 + 去重 | `Notifier`/`Channels`/`AlertAggregator`/`Deduplicator`/`RetryPolicy` | proto/secrets | ✓✓ |
| 14 | events | 告警 | 可插拔事件总线（noop/log/kafka） | `Bus`/`Event` | logx | ✓✓ |
| 15 | logstore | 数据 | M6 日志检索：Memory/SQL/Loki/ES + 倒排索引 | `LogStore`/`Handler`/`MemoryLogStore`/`SQLLogStore`/`LokiStore`/`ESStore`/`InvertedIndex` | — | ✓✓ |
| 16 | logx | 数据 | 结构化日志 + traceID 透传（OTel 关联） | `Info`/`Warn`/`Error`/`Trace` | otel | ✓ |
| 17 | metrics | 数据 | 零依赖 Prometheus 文本指标 | `M` | — | ✓ |
| 18 | otelx | 数据 | OpenTelemetry SDK 初始化 + gRPC/HTTP 埋点 | `Init`/`StartSpan`/`TraceIDFromContext` | otel | ✓ |
| 19 | authctx | 安全 | 网关注入身份上下文 + JWT 验签 | `Context`/`JWTConfig`/`FromRequest` | golang-jwt/grpc | ✓ |
| 20 | secrets | 安全 | 密钥管理抽象：env/file/vault/chain | `SecretProvider`/`EnvProvider`/`FileProvider`/`VaultProvider`/`ChainProvider` | — | ✓ |
| 21 | tlsutil | 安全 | TLS/mTLS 凭证构造 + 证书热重载 | `ServerCreds`/`ClientCreds`/`CertificateReloader` | grpc-credentials | ✓ |
| 22 | grpcx | 基础 | gRPC 服务描述 + JSON codec + 消息信封 | `RegistrationServer`/`Registration_ServiceDesc` | grpc/proto | ✓ |
| 23 | dag | 基础 | DAG 引擎：拓扑排序 + 依赖就绪判定 | `TopoOrder`/`ReadyIDs`/`Validate` | proto | ✓ |
| 24 | circuitbreaker | 基础 | 通用熔断器（Closed/Open/HalfOpen 状态机） | `CircuitBreaker`/`BreakerSet` | — | ✓ |
| 25 | approval | 基础 | 审批引擎：多步审批流 + 状态机 + 历史 | `Engine`/`ApprovalFlow`/`ApprovalRequest`/`History` | — | ✓✓ |
| 26 | cmdb | 基础 | CMDB CI 管理 + 关系拓扑 + 导入导出 + 审批 | `Handler`/`CiStore`/`CiItem`/`CiRelation` | authctx/proto | ✓ |
| 27 | cron | 基础 | cron 表达式解析 + 定时任务管理 + SLA | `Manager`/`ScheduleEntry`/`Scheduler` | — | ✓✓ |
| 28 | orchestration | 基础 | M5 作业编排：DAG 展开 + 子工作流 + 条件分支 | `Handler`/`WorkflowDef`/`WorkflowRun`/`TaskEngine` | authctx/cron/dag/proto | ✓ |
| 29 | proto | 其他 | 控制面/agent 共享数据类型（JSON 友好） | `AgentInfo`/`DeviceInfo`/`Task`/`TaskResult`/`Alert`/`AuditEvent`/`DeviceMetrics` | — | — |
| 30 | version | 其他 | 内核版本号（CI 注入 Commit/Date） | `Version`/`Commit`/`Date` | — | — |

## 第3章 详细设计

### 3.1 核心包

核心包构成 OpsMesh 的"骨架"：控制面装配、agent 运行时、持久化抽象、领域模型、统一配置。

#### 3.1.1 controlplane

**职责描述**

控制面服务装配中心，承载 HTTP(B/S 仪表盘 + REST API) + gRPC(agent 注册通道) + metrics 三端口服务。`Server` 结构体聚合所有领域 handler（CMDB / 日志 / 部署 / 编排 / 告警 / Helm / K8s / 配额 / 审批 / 联邦），通过 `NewServer(cfg)` 一次性构造并注入依赖。`Start` 启动三端口 + 后台 goroutine（leaderLoop / scheduleLoop / reclaimLoop / notifyLoop / alertEngineLoop / sseBroadcast），`Shutdown` 优雅退出。

**关键接口**

- `Server`：控制面服务主体，含 40+ 字段聚合各域 handler 与运行时状态
- `NewServer(cfg *config.Config) *Server`：构造器，按 cfg 选择 store/session/handler 后端
- `(s *Server) Start(ctx) / Shutdown()`：生命周期
- `selectStore(cfg, bus) (store.Store, error)`：按 cfg.Store 选择 MemoryStore/SQLStore/MultiSchemaStore
- `selectSessionStore(cfg) (store.SessionStore, error)`：按 cfg.SessionStore 选择 InProcess/Redis
- `storeDispatcher`：以 `store.Store` 适配 `deploy.Dispatcher` 防腐接口

**核心数据结构**

- `Server`：含 `cfg`/`store`/`bus`/`metrics`/`cmdbHandler`/`logHandler`/`deployHandler`/`orchHandler`/`alertEngine`/`alertSilencer`/`alertAggregator`/`alertNotifier`/`alertInhibitor`/`anomalyEngine`/`helmRepo`/`helmRelease`/`batches`/`scheduleMgr`/`approvalEngine`/`clusterMgr`/`quotaMgr`/`fed`/`tlsReloader`/`cmdbCollector`/`cmdbApprovalMgr`/`loginGuard`/`sessionStore`/`rateLimiter`/`eventSubs`/`jwtSecret`/`encryptionKey` 等字段
- `SSEEvent`：实时推送事件信封
- `FederationManager`：联邦管理器
- `NetworkTopologyCache`：网络拓扑缓存（5 分钟过期）
- `QuotaManager`：多租户资源配额管理器
- `CMDBCollector`/`CMDBApprovalManager`：CMDB 定时采集 / 变更审批

**关键算法**

- 后台 leaderLoop：仅 leader 副本执行协调任务（reclaim/schedule/provision/归档），避免多副本重复
- SSE 慢消费者丢弃：`publishEvent` 非阻塞广播，缓冲满丢弃事件，避免一个慢客户端拖垮广播
- 安全头中间件：每请求生成随机 CSP nonce，HSTS 仅 HTTPS 注入
- Demo 模式播种：`SeedDemoTopology` + 预置示例部署，让 6 大模块无真实 agent 也能演示

**并发安全**

- `eventMu sync.RWMutex` 保护 `eventSubs` SSE 订阅者集合
- 各域 handler 内部自带互斥；`Server` 字段在 `NewServer` 后基本只读
- `loginGuard` 失败计数 + 账号锁定经 `SessionStore` 跨副本共享；IP 限流保留进程内

**扩展点**

- `store.Store` 接口：可插拔持久化后端（Memory/SQL/MultiSchema/未来 PostgreSQL）
- `events.Bus` 接口：可插拔事件总线（noop/log/kafka/未来 Pulsar）
- `secrets.SecretProvider` 接口：可插拔密钥来源（env/file/vault/chain）
- `alertengine.MetricsProvider` 接口：可插拔指标源
- `notify.Channel` 接口：可插拔通知渠道
- `discovery.ServiceDiscovery` 接口：可插拔服务发现后端

#### 3.1.2 agent

**职责描述**

数据面 agent 运行时，与控制面共用同一份二进制。经真实 gRPC(9090) 完成四条通道：注册 / 心跳 / 拉任务 / 上报结果（+ 轮询取消 + 日志上报）。本地 `os/exec` 执行 shell/service/file 三类任务，worker 池消费任务队列。采集主机监控指标（每 30s）+ CMDB 属性（每 60s）+ 日志增量（按 offset）经心跳上报。

**关键接口**

- `Agent`：agent 运行时主体
- `New(cfg *config.Config) *Agent`：构造（含 OTel 初始化 + 熔断器初始化 + 日志采集配置）
- `(a *Agent) Run() error`：启动主循环（注册 → 心跳 → 拉任务 → worker 池 → cancelLoop → 优雅退出）
- `GRPCClient`：到控制面的 gRPC 通道（含多控制面 failover + 服务发现 balancer）
- `LogPusher`：日志推送器（批量缓冲 + flush 到 Loki/ES）

**核心数据结构**

- `Agent`：含 `cfg`/`grpc`/`agentID`/`hostname`/`dataDir`/`taskTimeout`/`workers`/`taskCh`/`running`/`cbSet`/`metricsHistory`/`logPusher`/`otelShutdown` 等字段
- `runState`：正在执行任务的控柄（`cancel context.CancelFunc` + `cancelled bool`）
- `MetricsHistory`：监控指标环形缓冲（默认 2h/240 条）

**关键算法**

- worker 池：`taskCh` 缓冲 channel + N 个 worker goroutine 消费
- 任务超时：`context.WithTimeout` + `cmd.Process.Kill` 强制终止
- 熔断器集成：worker 执行前经 `cbSet.Execute` 放行，连续失败 N 次熔断该设备
- 多控制面 failover：`ControlAddrs` 逗号分隔依次重试；`discovery.Balancer` round-robin/failover
- 日志增量采集：按文件 offset 记录上次读取位置，下次仅读增量
- CMDB 中间件探测：跨平台 `ps aux` / `tasklist`，按进程名 basename 匹配避免路径误报
- agent 身份持久化：`<dataDir>/agent.id` 落盘，重启沿用稳定 ID

**并发安全**

- `runMu sync.Mutex` 保护 `running` 任务控柄 map
- `logMu sync.Mutex` 保护 `logOffsets` 日志采集 offset map
- `taskCh` channel 自带并发安全

**扩展点**

- `circuitbreaker.BreakerSet`：可注入熔断器集合（按 deviceID 隔离）
- `discovery.ServiceDiscovery`：可注入服务发现后端
- `otelx.Init`：可注入 OTel 配置（endpoint / stdout）
- `LogPusher`：可注入日志推送目标（Loki/ES）
- 环境变量 `OPSMESH_LOG_COLLECT_PATHS` / `OPSMESH_LOG_COLLECT_INTERVAL`：日志采集配置

#### 3.1.3 store

**职责描述**

可插拔持久化抽象层。将原 37 方法巨型 `Store` 接口按领域拆为 17 个领域小接口（DeviceStore / TaskStore / AlertStore / AuditStore / TokenStore / LeaderStore / UserStore / RoleStore / PermissionStore / K8sClusterStore / TemplateStore），`Store` 保留为它们的组合接口向后兼容。提供三种实现：`MemoryStore`（默认内存）、`SQLStore`（MySQL + Redis 缓存）、`MultiSchemaStore`（每租户独立 schema）。

**关键接口**

- `Store`：组合接口，聚合全部领域小接口
- `DeviceStore`：设备/Agent 纳管（12 方法：Register/Heartbeat/Device/UpsertDevice/RetireDevice/Snapshot/Agents/Agent/RetireStaleDevices/StoreDeviceMetrics/DeviceMetrics/DeviceMetricsHistory/AgentSecret）
- `TaskStore`：任务调度（12 方法：GetTasks/TasksByParent/ClaimTask/CreateTask/SubmitResult/AllTasks/TaskByID/TaskResult/CancelTask/PendingDepth/ReclaimStaleTasks/CancelledTaskIDs/FireDueSchedules/ApproveTask/RejectTask）
- `AlertStore`：告警领域（Alerts/AddAlert/Alert/AckAlert/SilenceAlert/CreateAlertRule/ListAlertRules/DeleteAlertRule/GetAlertRule/UpdateAlertRule）
- `AuditStore`：审计领域（Audit/Audits/QueryAudits）
- `TokenStore`：install token（Provision/IssueToken/ConsumeToken/CleanupTokens）
- `LeaderStore`：HA 选主（RenewLeadership/IsLeader）
- `UserStore`/`RoleStore`/`PermissionStore`：RBAC
- `K8sClusterStore`：K8s 集群配置 CRUD
- `TemplateStore`：OS/中间件部署模板 CRUD
- `SessionStore`：会话状态（InProcessSessionStore / RedisSessionStore）

**核心数据结构**

- `MemoryStore`：内存实现，map + sync.RWMutex 保护各域
- `SQLStore`：MySQL 实现，`*sql.DB` 连接池 + Redis 缓存
- `MultiSchemaStore`：每租户独立 schema，按 tenantID 路由到独立 `*sql.DB`
- `User`/`Role`/`Permission`：RBAC 实体
- `K8sCluster`：K8s 集群配置（kubeconfig 经 AES-256-GCM 加密存储）
- `OSTemplate`/`MiddlewareTemplate`：部署模板
- `AlertRule`：告警规则

**关键算法**

- `ClaimTask` 原子领取：`UPDATE ... SET status='running', claim_epoch=claim_epoch+1 WHERE status='pending'` 单条原子
- `ReclaimStaleTasks` 失联复位：扫描 running 超 maxAge 任务翻回 pending
- `RetireStaleDevices` 离线归档：扫描最后心跳早于 maxAge 的 agent 设备标记 retired
- `FireDueSchedules` 定时派生：评估 cron 表达式 + LastFiredAt 去重，派生 pending 实例
- `RenewLeadership` 选主：`leader_lease` 表原子抢占/续租
- `Provision` install token：HMAC-SHA256 签名 + 一次性 + 限时
- kubeconfig 加密：AES-256-GCM，密钥来自 `cfg.EncryptionKey`
- `CleanupTokens` 过期清理：批量删除已消费/过期 token

**并发安全**

- `MemoryStore`：每域独立 `sync.RWMutex`，细粒度锁
- `SQLStore`：依赖 MySQL 行锁 + Redis 原子操作
- `MultiSchemaStore`：`sync.RWMutex` 保护 schema 路由 map
- `SessionStore`：InProcess 用 map + mutex；Redis 用原子命令

**扩展点**

- `Store` 接口：可新增后端实现（如 PostgreSQL/TiDB）
- `SessionStore` 接口：可新增会话后端（如 etcd）
- `WithSecret`/`WithBus`/`WithDemo`：函数选项注入依赖
- `SchemaNamer` 函数：可自定义 schema 命名规则

#### 3.1.4 domain

**职责描述**

纯领域模型层（DDD 分层中的 domain 层），与 proto（gRPC/HTTP 传输层）解耦。proto 负责线上格式，domain 负责业务语义。防腐层（ACL）由 `mapper.go` 提供，在 gRPC/HTTP 边界做 proto ↔ domain 转换。把不变的业务规则下沉到领域实体，handler 退化为薄编排层。

**关键接口**

- `Task`/`Device`/`Alert`/`Agent`/`Tenant`/`TaskResult`/`AuditEvent`：领域实体
- `(t *Task) Cancel() error`：任务取消状态机
- `(t *Task) CanRetry(maxRetries int) bool`：重试判定
- `(t *Task) IsLeaseExpired(maxAge) bool`：租约超时判定
- `(t *Task) MarkDead()`：死信标记
- `(d *Device) CanRetire(maxAge) bool`：退役资格判定
- `(d *Device) TransitionToProvisioning() error`：纳管状态翻转
- `(d *Device) IsOrphan() bool`：孤儿设备判定
- `(a *Alert) Acknowledge(by string) error`：告警确认状态机
- `(a *Alert) Silence(until, comment) error`：告警静默状态机
- `(a *Alert) IsExpired() bool`：静默过期判定

**核心数据结构**

- `Task`：含 `ClaimEpoch`（防双跑令牌）/`RetryCount`/`MaxRetries`/`DeadLetter`/`Schedule`/`ParentID`/`DependsOn`
- `Device`：含 `State`（online/offline/discovered/provisioning）/`Managed`/`Retired`
- `Alert`：含 `Status`（firing/acknowledged/silenced）/`SilencedUntil`/`Comment`
- sentinel error：`ErrTaskAlreadyDone`/`ErrTaskAlreadyFailed`/`ErrTaskAlreadyCancelled`/`ErrDeviceAlreadyProvisioning`/`ErrAlertAlreadyAcknowledged`/`ErrAlertAlreadySilenced`/`ErrAlertSilenced`

**关键算法**

- Task 状态机：pending/running → cancelled；终态返回精确 sentinel error
- Device 状态机：discovered → provisioning → online/offline；幂等拒绝重复转换
- Alert 状态机：firing → acknowledged → silenced；silenced 经 `IsExpired` 可重新 firing

**并发安全**

- 领域实体为纯值对象，无内部锁；调用方（handler/store）负责并发保护
- 状态机方法返回 error 而非 bool，使非法转换在调用方可精确区分

**扩展点**

- `mapper.go`：防腐层映射，proto ↔ domain 转换可按需扩展
- sentinel error：调用方经 `errors.Is` 精确区分，映射到不同 HTTP 状态码

#### 3.1.5 config

**职责描述**

统一配置入口，命令行 flag 优先 + 环境变量兜底。控制面与 agent 共用同一份 `Config` 结构，通过 `--mode` 切换角色。涵盖 100+ 配置项，覆盖运行模式 / 端口 / 持久化 / TLS / 服务发现 / 事件总线 / 告警通道 / 密钥管理 / 联邦 / 多租户 / 资源限额 / 自动纳管等全部维度。

**关键接口**

- `Config`：配置结构体
- `Load() (*Config, error)`：从 flag + env 解析配置
- `(c *Config) Validate() error`：配置合法性校验（生产模式强约束）

**核心数据结构**

- `Config`：100+ 字段，按功能分组
  - 运行模式：`Mode`/`Addr`/`ControlAddr`/`ControlAddrs`/`ControlplaneEndpoints`/`LBStrategy`/`Segment`
  - 端口：`HTTPPort`/`GRPCPort`/`MetricsPort`
  - 持久化：`Store`/`MySQLDSN`/`RedisAddr`/`MultiSchema`/`SchemaPrefix`/`SessionStore`
  - TLS：`TLSCert`/`TLSKey`/`ClientCA`/`TLSWatch`
  - 密钥：`SecretProvider`/`SecretFile`/`VaultAddr`/`VaultToken`/`VaultMount`/`EncryptionKey`/`JWTSecret`/`JWTPublicKey`/`JWTIssuer`
  - 告警：`AlertWebhookURL`/`AlertNotifierType`/`AlertEmail*`/`NotifyChannelsConfigFile`/`NotifyDedupTTLMin`/`NotifyRetry*`
  - 联邦：`FederationPeers`/`FederationSecret`/`FederationTLS*`/`FederationPort`
  - 自动纳管：`Discover`/`SegmentCIDR`/`AutoProvision`/`ProvisionSSH*`/`ProvisionSecret`/`AdvertiseAddr`/`InstallToken`
  - 资源限额：`MaxProcs`/`MaxFiles`/`MaxMemoryMB`/`WorkerConcurrency`/`TaskTimeout`/`ShutdownTimeout`
  - HA：`Replicas`/`Production`/`LeaderTTLSec`/`LeaderTickSec`/`TaskLeaseSec`/`TaskMaxRetries`/`ArchiveAgeMin`
  - 日志后端：`LogStore`/`LogBackend`/`LokiEndpoint`/`ESEndpoint`/`ESIndex`
  - OTel：`OTELEndpoint`/`OTELServiceName`/`OTELStdout`
  - 安全加固：`AgentShellWhitelist`/`AgentFileRootWhitelist`/`MetricsAllowCIDR`/`PublicRegister`/`AllowPublicRegister`/`RequireAuth`
- `NotifyChannelConfig`：通知渠道配置

**关键算法**

- flag + env 兜底：每个 flag 对应一个 env 变量（如 `--store` ↔ `OPSMESH_STORE`）
- `Validate` 生产模式强约束：`Production=true` 时强制 `RequireAuth=true` + `EncryptionKey` 非空 + `Store=mysql` when `Replicas>1`
- 优先级合并：`ControlplaneEndpoints` > `ControlAddrs` > `ControlAddr`
- `LogStore` 与 `LogBackend` 别名同步

**并发安全**

- `Config` 在启动期一次性解析，运行期只读，无需锁

**扩展点**

- flag 注册：可按需追加新 flag + env 兜底
- `Validate` 钩子：可按需追加生产模式强约束

### 3.2 运维包

运维包承载 OpsMesh 的核心运维能力：自动纳管、部署中心、Helm 应用商店、K8s 集群管理、网段发现、服务发现。

#### 3.2.1 provision

**职责描述**

自动纳管推送能力：通过 SSH 在候选设备上自动安装 OpsMesh agent，完成"网段发现 → SSH 推送 → agent 注册 → 设备纳管"闭环。支持 KnownHosts 主机指纹校验（等保生产必须配置）+ 私钥密码 + 10 分钟硬超时。

**关键接口**

- `PushAndExec(ctx, addr, user, keyPath, keyPass, knownHostsPath, cmd) (string, error)`：SSH 连接 + 远程执行 bootstrap 命令
- `knownHostsCallback(filePath) (ssh.HostKeyCallback, error)`：从 known_hosts 文件构造主机公钥校验回调

**核心数据结构**

- SSH 客户端配置：`ssh.ClientConfig`（User/HostKeyCallback/Timeout/Auth）
- known_hosts 条目：`{hosts []string, key ssh.PublicKey}`

**关键算法**

- known_hosts 解析：跳过 `|1|` 哈希格式，支持 `*.example.com` 通配符匹配
- 远程执行超时：`select` 监听 `ctx.Done` / `resCh` / `time.After(10min)`，超时发 SIGTERM + Close
- Insecure 回退：未配置 known_hosts 时打印显眼警告（生产应由 `auto.go` 提前拦截）

**并发安全**

- 无内部状态，纯函数式调用

**扩展点**

- `ssh.AuthMethod`：可扩展非私钥认证（如密码 / agent forwarding）
- `cmd` 参数：bootstrap 命令可由调用方自定义（curl install.sh | sh 或直接启动 binary）

#### 3.2.2 deploy

**职责描述**

M3 部署中心：支持滚动（rolling）/ 金丝雀（canary）/ 蓝绿（bluegreen）三种发布策略，含发布门禁评估 / 自动回滚 / 灰度自动推进 / 多集群联邦发布协调。HTTP 处理器暴露 `/api/v1/deploys` REST API，底层经 `Dispatcher` 防腐接口派发任务到 M4 任务引擎。

**关键接口**

- `Handler`：HTTP 处理器主体
- `NewHandler(store DeployStore, disp Dispatcher) *Handler`：构造器
- `(h *Handler) Execute(ctx, id, tenantID) error`：执行部署（按策略派发）
- `(h *Handler) Promote(ctx, id, tenantID) error`：灰度晋级
- `(h *Handler) Rollback(ctx, id, tenantID) error`：回滚
- `(h *Handler) Reconcile(ctx, id, tenantID) error`：状态对账 + 门禁评估
- `Dispatcher` 防腐接口：`CreateTask`/`Device`/`TaskStates`
- `DeployStore` 接口：`Create`/`Get`/`List`/`Update`（Memory / SQL 实现）
- `FederationCoordinator`：多集群联邦发布协调器
- `AutoAdvanceManager`：灰度自动推进管理器

**核心数据结构**

- `DeployTask`：含 `Strategy`/`TargetIDs`/`CanaryWeight`/`CanaryTargets`/`StableTargets`/`TaskIDs`/`GateConfig`/`AutoRollback`/`Status`
- 状态常量：`StatusCreated`/`StatusRunning`/`StatusCanary`/`StatusGated`/`StatusPromoting`/`StatusSuccess`/`StatusFailed`/`StatusRolledBack`
- `GateConfig`：发布门禁（`SuccessRate`/`MaxFailRate`/`MinSuccessCount`/`HealthCheckURL`）

**关键算法**

- `selectCanaryTargets`：按 weight 比例选取前 k 个目标（确定性，便于 reconcile 复现）
- `evaluateGate`：门禁判定（SuccessRate / MaxFailRate / MinSuccessCount），仅设 MaxFailRate 时补默认 SuccessRate=1 避免 0% 成功率被放行
- `Reconcile` 状态对账：底层任务全 done → success；任一 failed → failed + 可选自动回滚
- 联邦发布：`FederationCoordinator` 协调多集群发布顺序 + 状态聚合

**并发安全**

- `DeployStore` 内部自带互斥（Memory）或依赖 MySQL 行锁（SQL）
- `FederationCoordinator` 内部 mutex 保护联邦发布状态

**扩展点**

- `Dispatcher` 接口：可替换任务派发底层
- `DeployStore` 接口：可新增后端实现
- `FederationStore` 接口：联邦发布状态可持久化到 SQL
- `AutoAdvanceManager`：可注入自动推进策略

#### 3.2.3 helm

**职责描述**

Helm Release 生命周期管理 + Chart 仓库管理 + 预置目录。所有操作通过 helm CLI 完成（不引入 helm SDK 依赖），values map 经临时 JSON 文件传递（`helm -f`）。List/History/Get 解析 helm `-o json` 输出为 `Release` 结构。

**关键接口**

- `ReleaseManager`：Release 生命周期管理
- `RepoManager`：Chart 仓库管理（add/remove/list/search）
- `HelmCLI` 接口：CLI 命令封装（便于测试注入 mock）
- `(m *ReleaseManager) Install/Upgrade/Rollback/Uninstall/List/ListAll/History/Get/GetValues/GetManifest`

**核心数据结构**

- `Release`：含 `Name`/`Namespace`/`Chart`/`Version`/`AppVersion`/`Status`/`Revision`/`Values`/`CreatedAt`/`UpdatedAt`/`Description`
- `ReleaseStatus` 常量：`StatusDeployed`/`StatusFailed`/`StatusPendingInstall`/`StatusPendingUpgrade`/`StatusPendingRollback`/`StatusSuperseded`/`StatusUninstalled`/`StatusUninstalling`
- `listReleaseJSON`/`historyReleaseJSON`/`statusJSON`：helm JSON 输出解析结构

**关键算法**

- `writeValuesTemp`：values map 序列化为临时 JSON 文件，defer cleanup 删除
- `splitChartVersion`：从右往左找最后一个 `-` 且其后为数字（semver 起始），拆分 "mysql-9.10.0" 为 ("mysql", "9.10.0")
- `parseHelmTime`：多格式时间解析（RFC3339 / Go Time.String() / Unix date）
- Rollback 默认回滚到 revision-1：查询 history 取倒数第二条

**并发安全**

- 无内部状态，纯 CLI 调用；并发安全由 helm CLI 自身保证

**扩展点**

- `HelmCLI` 接口：可注入 mock CLI 用于测试
- `NewReleaseManagerWithCLI` / `NewReleaseManagerWithHelmCLI`：构造时注入自定义 CLI
- `kubeconfig` 参数：可切换不同 K8s 集群

#### 3.2.4 k8s

**职责描述**

K8s 集群客户端连接与多集群管理（Phase 3）。封装 client-go 连接（Clientset + rest.Config），支持 kubeconfig 内容（YAML 字符串）与 kubeconfig 文件路径两种构造方式。`ClusterManager` 管理多个集群连接（clusterID → K8sClient），并发安全。安全加固：拒绝含 exec/auth-provider 凭据插件的 kubeconfig（防 RCE）+ 强制关闭 insecure-skip-tls-verify。

**关键接口**

- `K8sClient`：单集群客户端封装
- `NewK8sClient(name, kubeconfigData) (*K8sClient, error)`：从 kubeconfig 内容构造
- `NewK8sClientFromPath(name, kubeconfigPath) (*K8sClient, error)`：从文件路径构造
- `(c *K8sClient) TestConnection() error`：列出 namespaces 验证连通性
- `(c *K8sClient) Close()`：释放连接资源
- `ClusterManager`：多集群连接管理器（AddCluster/RemoveCluster/TestCluster/GetCluster）

**核心数据结构**

- `K8sClient`：含 `Name`/`Server`/`Clientset kubernetes.Interface`/`Config *rest.Config`
- `ClusterManager`：`map[string]*K8sClient` + sync.RWMutex

**关键算法**

- `validateKubeConfigSafety`：遍历 AuthInfos，拒绝 `Exec` / `AuthProvider` 凭据插件
- `forceSecureTLS`：强制 `config.Insecure = false`
- `TestConnection` 错误分类：Unauthorized → 凭据无效；Forbidden → 权限不足；其他 → 透传
- 10s 超时：`context.WithTimeout` 避免阻塞 API 调用方

**并发安全**

- `ClusterManager`：`sync.RWMutex` 保护集群连接 map
- `K8sClient` 构造后只读，无需锁

**扩展点**

- `kubernetes.Interface` 字段类型：便于测试注入 `fake.NewSimpleClientset()`
- `Config *rest.Config`：调用方可据此构造 DynamicClient / DiscoveryClient 等扩展客户端

#### 3.2.5 discover

**职责描述**

网段存活扫描（真实纳管）。用标准库 `net` 对 segment CIDR 做并发受限的 TCP-connect 探测，返回存活主机 IP。ICMP 需原始套接字（特权），故默认用 TCP-connect 探测（非特权、可控）。这是产品核心差异点"服务部署后整段网络打通、设备自动纳管"的真实兑现路径。

**关键接口**

- `Sweep(ctx, cidr, ports, concurrency, timeout) ([]string, error)`：网段存活扫描

**核心数据结构**

- `MaxHosts = 1024`：单次扫描主机数上限，避免 /16 等大网段耗尽资源

**关键算法**

- `enumerateIPv4`：CIDR → 主机 IP 列表（排除网络/广播地址，/31、/32 例外）
- 并发受限：`sem chan struct{}` 信号量控制并发度（默认 64）
- `alive` 探测：对单 IP 的 ports 做 TCP 连接，任一成功即存活（默认端口 22/80/443/9090）
- ctx 取消：`select` 监听 `ctx.Done` 提前返回
- 去重 + 排序：`seen map` 去重 + `sort.Strings` 升序

**并发安全**

- `mu sync.Mutex` 保护 `seen` 存活 IP 集合
- `sem` channel 控制并发度
- `wg sync.WaitGroup` 等待所有探测 goroutine

**扩展点**

- `ports` 参数：可自定义探测端口列表
- `concurrency` / `timeout` 参数：可调并发度与超时
- 未来可扩展 ICMP 探测（需特权）

#### 3.2.6 discovery

**职责描述**

服务注册发现抽象与多种实现。解耦 agent 与控制面地址获取方式：agent 不再硬编码控制面地址，而是通过 `ServiceDiscovery` 接口动态获取控制面实例列表。配合 `Balancer` 接口实现多控制面负载均衡（round-robin/failover）。

**关键接口**

- `ServiceDiscovery` 接口：`Register`/`Deregister`/`List`/`Watch`
- `Balancer` 接口：`Next`/`OnFailure`/`OnRecover`
- 实现：`NoopDiscovery`（默认）/`StaticDiscovery`（静态配置多控制面）
- Balancer 实现：`RoundRobin`/`Failover`/`NoopBalancer`

**核心数据结构**

- `Service`：含 `ID`/`Name`/`Addr`/`Port`/`Metadata`/`Healthy`
- `ErrNotImplemented`/`ErrNoInstances`：sentinel error
- `DefaultWatchTimeout = 30s`：Watch channel 默认刷新超时

**关键算法**

- `StaticDiscovery.Watch`：周期性推送当前列表（模拟变更通知，便于 balancer 拿到最新列表）
- `RoundRobin.Next`：轮询选择实例
- `Failover.OnFailure`：主备切换，失败时切换到下一个实例

**并发安全**

- `ServiceDiscovery` 实现需保证并发安全：agent 多 goroutine（heartbeat/dispatch/cancel）会并发调用 `List`
- `Balancer` 实现内部自带互斥

**扩展点**

- `ServiceDiscovery` 接口：未来扩展 etcd/Consul 实现只需实现接口即可无缝接入
- `Balancer` 接口：可新增负载均衡策略（如加权轮询 / 最少连接）

### 3.3 告警包

告警包构成 OpsMesh 的监控告警闭环：规则引擎评估 + 多渠道通知 + 事件总线。

#### 3.3.1 alertengine

**职责描述**

告警规则引擎 + 持续时长评估器 + 静默器 + 聚合器 + 抑制器 + 异常检测引擎。`Engine` 持有所有告警规则（按 ID 索引），提供 CRUD 与设备级评估。评估流程：取启用规则快照 → MatchRule（按条件 + Logic 组合）→ ShouldFire（持续时长判定）→ 构造 AlertEvent。

**关键接口**

- `Engine`：规则引擎主体
- `NewEngine(metrics MetricsProvider, evaluator *Evaluator, now func() time.Time) *Engine`
- `(e *Engine) AddRule/UpdateRule/DeleteRule/GetRule/ListRules`
- `(e *Engine) MatchRule(rule, deviceID) (bool, error)`：单条规则评估
- `(e *Engine) Evaluate(deviceID) ([]*AlertEvent, error)`：设备级全规则评估
- `MetricsProvider` 接口：`Query(metric, deviceID, window) (float64, error)`
- `Evaluator`：持续时长评估器（60 样本）
- `Silencer`：基于时间窗口的静态抑制
- `Aggregator`：按 groupBy 字段聚合告警事件
- `AlertInhibitor`：基于活跃告警状态的动态抑制
- `AnomalyEngine`：异常检测引擎（滑动窗口 Z-Score + EWMA 突变检测）

**核心数据结构**

- `AlertRule`：含 `ID`/`Name`/`TenantID`/`Conditions`/`Logic`/`Severity`/`Duration`/`Enabled`
- `AlertEvent`：含 `RuleID`/`TenantID`/`DeviceID`/`Severity`/`Message`/`Labels`/`Values`/`FiredAt`
- `Condition`：含 `Metric`/`Operator`/`Threshold`/`Window`
- `ErrMetricUnavailable`/`ErrRuleInvalid`/`ErrRuleNotFound`：sentinel error

**关键算法**

- `MatchRule`：对每个 Condition 调用 `metrics.Query` 拿窗口聚合值，按 Operator 比较得布尔结果，按 Logic（AND/OR）组合
- `ErrMetricUnavailable` 降级：指标不可用时该条件视为 false（不中断整体评估）
- `Evaluate`：取启用规则快照（持 RLock 后释放）→ 逐条 MatchRule → ShouldFire 判断持续时长 → 构造 AlertEvent
- `buildEvent`：Labels 默认包含 ruleID/deviceID/severity/tenantID，便于 Aggregator/Silencer 使用
- 异常检测：滑动窗口 Z-Score + EWMA 突变检测，异常时产生 AnomalyAlert 转 AlertEvent

**并发安全**

- `Engine.mu sync.RWMutex` 保护 `rules` map
- 评估时取快照后释放锁，再调用 MatchRule/ShouldFire（二者各自管理自己的锁），避免长时持锁与重入死锁
- AddRule/UpdateRule/DeleteRule 经深拷贝隔离外部修改

**扩展点**

- `MetricsProvider` 接口：可注入基于 `store.DeviceMetrics` 的 Provider
- `Evaluator`：可注入自定义持续时长评估器
- `now func() time.Time`：可注入虚拟时钟（测试用）
- `AnomalyEngine`：可注入异常检测算法

#### 3.3.2 notify

**职责描述**

告警通知核心：聚合 / 抑制 + 多渠道推送（Webhook / Email / Slack / 企业微信 / 钉钉 / 飞书）。`Notifier` 集成多渠道 + 模板渲染 + 重试策略 + 消息去重。`AlertAggregator` 同源告警 5 分钟聚合 + 级别抑制（critical 抑制同源 warning）。

**关键接口**

- `Notifier`：通知管理器主体
- `NewNotifier(opts ...NotifierOption) *Notifier`：函数选项模式构造
- `(n *Notifier) Notify(a *proto.Alert) error`：通过所有渠道推送（集成去重 + 重试）
- `(n *Notifier) AddChannel(ch Channel)`：运行期动态添加渠道
- `(n *Notifier) BuildChannel(cfg ChannelConfig) (Channel, error)`：按配置构造渠道（含密钥解析）
- `Channel` 接口：`Send(a *proto.Alert) error`/`Name() string`/`Type() string`
- `AlertAggregator`：告警聚合器
- `Channels`：多通道配置（Webhook + Email）
- `Deduplicator`：消息去重器
- `RetryPolicy`：重试策略（指数退避）
- `TemplateStore`：模板存储
- `NotifierOption`：`WithChannels`/`WithTemplates`/`WithDedup`/`WithRetry`/`WithSecretProvider`

**核心数据结构**

- `AlertAggregator`：`entries map[string]aggregatorEntry`（key: metric+":"+deviceID）+ `sync.RWMutex`
- `aggregatorEntry`：`{lastPushed time.Time, severity string}`
- `EmailConfig`：`{Host, Port, User, Pass, From, To}`
- `Channels`：`{NotifierType, WebhookURL, Email *EmailConfig}`
- `AggregateWindow = 5 * time.Minute`：同源告警聚合窗口

**关键算法**

- `AlertAggregator.Allow`：同源键不存在或已过期 → 放行；同源键在窗口内且当前级别 ≤ 已推送级别 → 抑制；当前级别更高 → 放行（升级告警透传）
- `DetectChannelByURL`：按 webhook URL 域名自动识别通道（slack.com → slack；qyapi.weixin.qq.com → wecom），用 `matchDomain` 精确匹配避免子串误判
- `slackBlockKit` / `wecomMarkdown`：构造 Slack Block Kit / 企业微信 markdown 消息体
- `SendEmail`：net/smtp 发送，10s 超时（goroutine + select 模拟），`sanitizeEmailField` 移除 CR/LF 防邮件头注入
- `buildRFC822`：构造 RFC 822 邮件正文（含必要头）
- `Deduplicator`：TTL 内相同消息只发送一次
- `RetryPolicy`：指数退避重试（`NotifyRetryMaxAttempts`/`NotifyRetryInterval`/`NotifyRetryBackoff`）

**并发安全**

- `AlertAggregator.mu sync.RWMutex` 保护 entries（notifyLoop 单 goroutine 调用，但 Cleanup 可能由独立 goroutine 周期执行）
- `Notifier.channels` 构造后不变（启动期一次性注入）；`templates`/`dedup` 内部自带互斥

**扩展点**

- `Channel` 接口：可新增通知渠道（如 SMS / 电话）
- `NotifierOption` 函数选项：可注入自定义选项
- `SecretProvider`：密钥外置，渠道构造支持 `${key}` 引用语法
- `TemplateStore`：可注入自定义模板

#### 3.3.3 events

**职责描述**

可插拔事件总线（审计/告警），内核产出的事件统一经 `Bus` 发布。默认 noop/log 实现零依赖；Kafka 生产者置于 `//go:build kafka` 编译标签，默认构建不引入重依赖。事件信封含 `SchemaVersion`，跨版本演进的锚点。

**关键接口**

- `Bus` 接口：`Publish(ctx, e Event) error`
- `New(kind, brokers, topic string) Bus`：按名称构造（noop/log/kafka）
- 实现：`NoopBus`/`LogBus`/`KafkaBus`（kafka 构建标签）

**核心数据结构**

- `Event`：含 `TenantID`/`UserID`/`Action`/`Target`/`Detail`/`Level`/`Version`
- `Level` 常量：`LevelInfo`/`LevelWarn`/`LevelAlert`
- `SchemaVersion = "1.0.0"`：事件契约版本

**关键算法**

- `stamp`：发布前为事件加盖当前契约版本（若调用方已显式指定则保留）
- `stampingBus`：包装任意 Bus，强制加盖契约版本
- `New` 工厂：按 kind 选择实现，统一经 `stampingBus` 包装
- Kafka WAL：`kafka_wal.go` 提供写前日志，保证消息不丢

**并发安全**

- `NoopBus`/`LogBus`：无状态，并发安全
- `KafkaBus`：Kafka 客户端自带并发安全

**扩展点**

- `Bus` 接口：可新增实现（如 Pulsar / RabbitMQ）
- `SchemaVersion`：跨版本演进时 bump 此常量并通知下游消费者

### 3.4 数据包

数据包承载 OpsMesh 的可观测能力：日志检索、结构化日志、指标、链路追踪。

#### 3.4.1 logstore

**职责描述**

M6 日志检索：集中采集 agent / 任务 / 系统日志，支持按租户 / 设备 / 时间 / 关键字检索。四后端实现：`MemoryLogStore`（环形缓冲）/ `SQLLogStore`（MySQL）/ `LokiStore`（Grafana Loki，仅查询）/ `ESStore`（Elasticsearch，仅查询）。`MemoryLogStore` 可启用倒排索引提供全文本检索（短语/布尔/通配符/TF-IDF）。

**关键接口**

- `LogStore` 接口：`Append(ctx, e *Entry) error`/`Query(ctx, q Query) ([]Entry, error)`/`Close() error`
- `Handler`：HTTP 处理器
- `NewMemory(cap) *MemoryLogStore`：内存环形缓冲后端
- `NewMemoryWithIndex(cap) *MemoryLogStore`：启用倒排索引的内存后端
- `NewSQL(db) (*SQLLogStore, error)`：MySQL 后端
- `NewLokiStore(endpoint)`/`NewESStore(endpoint, index)`：Loki/ES 后端
- `InvertedIndex`：倒排索引

**核心数据结构**

- `Entry`：日志条目（含 `TenantID`/`DeviceID`/`AgentID`/`Timestamp`/`Level`/`Message`/`Fields`）
- `Query`：检索条件（含 `TenantID`/`DeviceID`/`AgentID`/`Start`/`End`/`Keyword`/`Level`/`Limit`）
- `MemoryLogStore`：`buf []Entry` 环形缓冲 + `cap` + 可选 `index *InvertedIndex`
- `maxQueryLimit = 1000`：单次检索硬上限

**关键算法**

- 环形缓冲：超出 cap 丢弃最旧；裁剪同步移除索引中旧文档
- 倒排索引：分词 → 倒排表 → 短语/布尔/通配符/TF-IDF 检索
- `queryparse`：查询语法解析（关键字 + 布尔操作符）
- Loki/ES 仅查询：Append 为 noop，日志由 promtail/filebeat 直接推送

**并发安全**

- `MemoryLogStore`：`sync.RWMutex` 保护 buf 与 index
- `SQLLogStore`：依赖 MySQL 行锁
- `InvertedIndex`：内部 mutex 保护倒排表

**扩展点**

- `LogStore` 接口：可新增后端实现（如 OpenSearch / ClickHouse）
- `InvertedIndex`：可扩展分词器 / 查询语法

#### 3.4.2 logx

**职责描述**

结构化日志（slog JSON）与 request/gRPC 级别的 traceID 透传。替代散落的 `log.Printf`，满足可检索 / 可关联 / 可接采集器。分布式可观测性：`Trace(ctx)` 优先从 OTel span context 提取真实 trace_id，使日志与 OTel 链路追踪自动关联。

**关键接口**

- `Info(ctx, msg, args...)`/`Warn(ctx, msg, args...)`/`Error(ctx, msg, err, args...)`：结构化日志
- `WithTrace(ctx, traceID) context.Context`：显式注入 traceID
- `Trace(ctx) string`：从 context 取 traceID

**核心数据结构**

- `logger`：默认 `slog.NewJSONHandler(os.Stderr, ...)` 输出到 stderr
- `traceKey`：context key 类型

**关键算法**

- `Trace` 优先级：OTel span context 的 TraceID → WithTrace 显式注入的 traceID → 空串
- 日志自动带 traceID：`Info`/`Warn`/`Error` 内部调用 `Trace(ctx)` 提取并附加到 args

**并发安全**

- `logger` 全局变量，slog 自带并发安全
- `WithTrace` 经 `context.WithValue` 不可变

**扩展点**

- `logger` 可替换为其他 slog.Handler（如采集器适配器）
- OTel 集成：`Trace` 自动关联 OTel span，无需修改调用点

#### 3.4.3 metrics

**职责描述**

零依赖的可观测指标（计数器/直方图/仪表盘），以 Prometheus 文本格式暴露于控制面 metrics 端口。刻意不引入 prometheus 客户端，避免 go.sum 负担。扩充：HTTP 请求延迟直方图 + HTTP 请求计数器 + Go runtime 指标。

**关键接口**

- `M`：线程安全的指标注册表
- `New() *M`：构造空指标注册表
- `(m *M) SetAgents(n)`/`IncTask(status)`/`SetQueueDepth(n)`/`ObserveDuration(seconds)`
- `(m *M) IncHTTPRequest(method, path, status)`/`ObserveHTTPRequestDuration(method, path, status, seconds)`
- `(m *M) Render() string`：Prometheus 文本格式输出

**核心数据结构**

- `M`：含 `agents`/`tasks map[string]int64`/`depth`/`durN`/`durSum`/`durMax`/`httpReqs`/`httpHist` + `sync.Mutex`
- `httpHistStats`：`bucketCounts []uint64`（len == len(defBuckets)+1，末位为 +Inf 桶）+ `sum`/`count`
- `defBuckets`：与 prometheus.DefBuckets 一致（秒）
- `processStartTime`：进程启动时间（Unix 秒）

**关键算法**

- 直方图桶：找到第一个 bucket >= seconds，落入该桶（累积语义在 Render 时展开）
- `Render` 累积桶：`bucket{le=bi} = sum(counts[0..i])`，最后 +Inf 桶 = count
- runtime 指标：`runtime.ReadMemStats` + `runtime.NumGoroutine` + `os.Getpid`，实时采集
- `splitHTTPKey`：拆分 "method|path|status" 键回三元组
- `formatBucketLabel`：去尾零，与 client_golang 一致

**并发安全**

- `M.mu sync.Mutex` 保护所有指标字段
- `Render` 持锁期间实时采集 runtime 指标

**扩展点**

- 桶边界 `defBuckets`：可自定义
- runtime 指标：可覆盖 `appendRuntimeMetrics` 提供精确值（如读 /proc/self/status）

#### 3.4.4 otelx

**职责描述**

封装 OpenTelemetry SDK 初始化与 helper，提供 gRPC + HTTP 自动埋点能力。链路追踪集成：支持导出到 OTLP gRPC（Jaeger/OTLP collector）与 stdout（调试用）。endpoint 为空且 stdout=false 时 no-op（不启用追踪，零开销），保证 OTel 可选不破坏现有功能。

**关键接口**

- `Init(cfg Config) (ShutdownFunc, error)`：初始化 OTel SDK
- `Tracer(name) trace.Tracer`：返回命名 Tracer
- `StartSpan(ctx, name) (context.Context, trace.Span)`：创建 span helper
- `SpanFromContext(ctx) trace.Span`：从 context 提取当前 span
- `RecordError(span, err)`：在 span 上记录错误
- `Enabled() bool`：返回 OTel 追踪是否启用
- `TraceIDFromContext(ctx) string`/`SpanIDFromContext(ctx) string`：提取 trace_id / span_id

**核心数据结构**

- `Config`：含 `Endpoint`/`ServiceName`/`Stdout`
- `ShutdownFunc func(ctx context.Context) error`：优雅关闭函数
- `noopShutdown`：no-op 模式下的空关闭函数

**关键算法**

- 全局 propagator 统一为 W3C Trace Context + Baggage，使 HTTP/gRPC 跨进程 trace context 提取/注入一致
- TracerProvider 用 BatchSpanProcessor（5s 批量上报）
- no-op 模式仍设置全局 propagator，使 context 提取/注入行为一致（不丢上游 traceparent）
- TLS 策略：端口 443 视为标准 TLS 端口，其余用 insecure（内网/调试）
- Resource 构造：服务名 + 版本，附加到每个 span

**并发安全**

- 全局 `otel.SetTracerProvider` / `otel.SetTextMapPropagator` 由 OTel SDK 自带并发安全
- `TraceIDFromContext` 零开销：仅从 ctx 取 span + 读 SpanContext，不创建 span、不分配

**扩展点**

- `Config` 可扩展 TLS 凭据（`WithTLSCredentials`）
- 导出器可扩展（如 OTLP HTTP / Zipkin）
- `ServiceName` 可注入自定义服务名

### 3.5 安全包

安全包承载 OpsMesh 的安全基座：身份上下文、密钥管理、TLS 凭证。

#### 3.5.1 authctx

**职责描述**

"网关注入的身份上下文"的提取与校验。设计原则（等保三级 + 复用底座，非自研登录）：内核不自行实现登录/鉴权/用户表/密码哈希；登录/SSO/MFA/RBAC 由前置网关完成，网关校验 JWT/OIDC 后把身份注入到请求头/gRPC metadata，内核只消费这些头。JWT 验签（可选启用）：配置网关 RSA 公钥时，`FromRequest` 优先从 Authorization Bearer token 提取并 RS256 验签。

**关键接口**

- `Context`：身份上下文（`TenantID`/`UserID`/`Roles`）
- `FromHTTPHeader(h http.Header) Context`：从 HTTP 头提取
- `FromGRPCMetadata(md metadata.MD) Context`：从 gRPC metadata 提取
- `FromJWT(h, publicKey, issuer) (Context, error)`：JWT 验签提取
- `FromRequest(h, cfg) (Context, error)`：按配置选择提取路径
- `(c Context) BelongsTo(resourceTenant) bool`：租户归属判定
- `(c Context) HasRole(role) bool`：角色判定
- `LoadJWTPublicKey(path)`/`ParseJWTPublicKey(data)`：加载 RSA 公钥

**核心数据结构**

- `Context`：`{TenantID, UserID, Roles []string}`
- `JWTConfig`：`{PublicKey *rsa.PublicKey, Issuer, Enabled}`
- 头约定：`x-tenant-id`/`x-user-id`/`x-user-roles`
- claim 约定：`tenant_id`/`user_id`/`user_roles`
- `ErrNoJWTToken`：sentinel error

**关键算法**

- `FromRequest` 行为矩阵：Enabled + 携带 token → JWT 验签；Enabled + 未携带 token → 回退头注入；未 Enabled → 头注入
- `FromJWT` 验签：`jwt.ParseWithClaims` + `WithValidMethods(["RS256"])` + 双重保险断言算法类型防降级攻击
- `claimRoles`：兼容字符串数组与逗号分隔字符串两种签发格式
- `BelongsTo`：空 tenantID 表示无网关/开发模式，放行全部（不强制隔离）

**并发安全**

- `Context` 为值类型，无内部锁
- `LoadJWTPublicKey` 一次性加载，运行期只读

**扩展点**

- `JWTConfig`：可注入网关公钥 + issuer
- 头约定：可扩展自定义头名
- claim 约定：可扩展自定义 claim 键

#### 3.5.2 secrets

**职责描述**

统一密钥管理抽象层。支持 3 种密钥来源：`EnvProvider`（环境变量）/ `FileProvider`（JSON 文件）/ `VaultProvider`（HashiCorp Vault KV v2）。通过 `ChainProvider` 可按优先级依次尝试多个 provider。`ResolveSecret` 辅助函数支持 `${provider:key}` 引用语法，向后兼容明文配置。

**关键接口**

- `SecretProvider` 接口：`Get(key) (string, error)`/`Name() string`
- 实现：`EnvProvider`/`FileProvider`/`VaultProvider`/`ChainProvider`
- `NewEnvProvider(prefix)`/`NewFileProvider(path)`/`NewChainProvider(providers...)`
- `ResolveSecret(value, provider) (string, error)`：解析密钥引用
- `FromConfig(cfg) (SecretProvider, error)`：从配置构造（工厂方法）

**核心数据结构**

- `EnvProvider`：`prefix string`
- `FileProvider`：`path`/`data map[string]interface{}`/`sync.RWMutex`
- `ChainProvider`：`providers []SecretProvider`
- `ErrSecretNotFound`：sentinel error

**关键算法**

- `EnvProvider.Get`：从环境变量 `prefix+key` 读取
- `FileProvider.Get`：按 "/" 分隔遍历嵌套 map，最后一段必须为 string
- `ChainProvider.Get`：依次尝试 providers，第一个非 NotFound 的结果胜出；非 NotFound 错误立即返回
- `ResolveSecret`：非引用格式直接返回明文；`${provider:key}` 引用格式剥离前缀后用 provider 解析
- 引用前缀解析：仅当首段不含 "/" 时才视为 provider 名（避免把 "notify/dingtalk#webhook_url" 误判）

**并发安全**

- `FileProvider.mu sync.RWMutex` 保护 data（虽然构造后只读，保留锁便于未来热重载）
- `EnvProvider` 经 `os.LookupEnv` 自带并发安全
- `ChainProvider` 无内部状态

**扩展点**

- `SecretProvider` 接口：可新增实现（如 AWS Secrets Manager / Azure Key Vault）
- `ChainProvider`：可组合多个 provider 按优先级查找
- `ResolveSecret` 引用语法：可扩展自定义前缀

#### 3.5.3 tlsutil

**职责描述**

gRPC 传输层 TLS / mTLS 凭证的构造助手+ TLS 证书热重载。内核默认不启用 TLS（仅内网友好网络）；等保三级生产环境建议开启 mTLS。安全加固：强制 TLS 1.2+，禁用 SSLv3/TLSv1.0/TLSv1.1 等弱协议版本。

**关键接口**

- `ServerCreds(certFile, keyFile, clientCA) (credentials.TransportCredentials, error)`：服务端传输凭证
- `ClientCreds(certFile, keyFile, caFile) (credentials.TransportCredentials, error)`：客户端传输凭证
- `HTTPClientTLSConfig(certFile, keyFile, caFile) (*tls.Config, error)`：HTTP 客户端 mTLS 配置
- `HTTPServerTLSConfig(certFile, keyFile, clientCA) (*tls.Config, error)`：HTTP 服务端 TLS 配置
- `CertificateReloader`：证书热重载器（fsnotify 监听文件变更）

**核心数据结构**

- `tls.Config`：强制 `MinVersion: tls.VersionTLS12`
- `CertificateReloader`：含 fsnotify watcher + watchLoop goroutine

**关键算法**

- `ServerCreds`：加载服务端证书 + 可选 clientCA 启用 mTLS（`RequireAndVerifyClientCert`）
- `ClientCreds`：可选客户端证书 + 可选 caFile 校验服务端
- `HTTPClientTLSConfig`：三者皆空返回 (nil, nil) 表示不启用 TLS
- `HTTPServerTLSConfig`：服务端证书必备 + 可选 clientCA 双向认证
- 证书热重载：fsnotify 监听证书文件变更，自动重载无需重启

**并发安全**

- `tls.Config` 构造后只读
- `CertificateReloader` 内部 mutex 保护证书引用

**扩展点**

- `CipherSuites`：保留 Go 默认强套件（Go 1.17+ 默认已排除不安全套件），可显式设置
- `CertificateReloader`：可扩展监听多个证书文件

### 3.6 基础包

基础包提供 OpsMesh 的通用基础设施：gRPC 服务描述、DAG 引擎、熔断器、审批引擎、CMDB、定时任务、作业编排。

#### 3.6.1 grpcx

**职责描述**

gRPC 服务描述 + JSON codec + 消息信封。手写 `ServiceDesc`，无需 protoc 生成。服务名 `opsmesh.v1.Registration`（带版本前缀，破坏性变更可灰度）。七个一元方法对应 agent↔控制面 的 注册 / 心跳 / 拉任务 / 上报结果 / 取消 / 轮询取消 / 日志上报。gRPC agent 身份绑定：控制面为每个 agent 生成 HMAC 签名密钥，agent 后续请求携带签名防冒领。

**关键接口**

- `RegistrationServer` 接口：`Register`/`Heartbeat`/`PullTasks`/`ReportResult`/`CancelTask`/`PollCancels`/`ReportLogs`
- `Registration_ServiceDesc`：手写 grpc.ServiceDesc
- `_Registration_*_Handler`：七个方法的服务端分发 Handler
- JSON codec：复用 proto.AgentInfo / proto.Task / proto.TaskResult

**核心数据结构**

- `RegisterResp`：含 `AgentID`/`ControlConfig`/`Secret`（HMAC 签名密钥）
- `HeartbeatReq`：含 `AgentID`/`Status`/`Load`/`CmdbReport`/`Metrics`
- `PullTasksReq`/`PullTasksResp`：拉取任务请求/响应
- `CancelTaskReq`/`PollCancelsReq`/`PollCancelsResp`：取消/轮询取消
- `ReportLogsReq`：日志上报请求
- `Empty`：空响应

**关键算法**

- 标准 gRPC 一元 Handler 写法：dec 解码 → interceptor 拦截器 → srv 调用
- 服务名带版本前缀 `opsmesh.v1.`：破坏性变更可灰度
- HMAC 签名：agent 收到 Secret 后保存，后续请求在 gRPC metadata 携带 `agent-signature = HMAC-SHA256(secret, timestamp+agentID)`

**并发安全**

- 无内部状态，纯服务描述
- 并发安全由实现方（controlplane.grpcServerImpl）保证

**扩展点**

- `RegistrationServer` 接口：实现方按需扩展方法
- 服务名版本前缀：可升级到 `opsmesh.v2.` 灰度新版本

#### 3.6.2 dag

**职责描述**

M5 作业编排的 DAG（有向无环图）引擎：拓扑排序（Kahn 算法）+ 依赖就绪判定 + 任务图校验。设计原则：内核只做"依赖就绪判定 + 拓扑顺序"，不下发执行（下发由 store/registry 负责）。

**关键接口**

- `TopoOrder(tasks) ([]string, error)`：拓扑排序
- `ReadyIDs(tasks) []string`：返回当前可下发任务 ID
- `AllDepsDone(t, byID) bool`：判定任务全部前置依赖是否已 done
- `Validate(tasks) error`：校验任务图合法性

**核心数据结构**

- `proto.Task`：含 `DependsOn []string` 字段
- `indexByID`：按 TaskID 建索引

**关键算法**

- Kahn 算法：统计入度 → 入度为 0 入队 → 取队首 + 减后继入度 → 入度为 0 入队 → 重复直到队空
- 环路检测：`len(order) != len(tasks)` 时找出未进入 order 的节点（环路成员）
- `Validate`：自依赖 + 缺失依赖 + 环路三重校验
- `ReadyIDs`：无依赖任务始终就绪；有依赖任务当全部依赖 done 时就绪

**并发安全**

- 纯函数式调用，无内部状态
- 输入 `tasks` 切片不被修改

**扩展点**

- 输入 `proto.Task`：可扩展节点属性
- 算法可替换（如 DFS 拓扑排序）

#### 3.6.3 circuitbreaker

**职责描述**

通用熔断器（Circuit Breaker），用于 agent 端任务执行熔断与控制面 API 熔断（限流+降级）。状态机：Closed（正常）→ Open（熔断）→ HalfOpen（半开探测）→ Closed。可选禁用：`FailureThreshold <= 0` 时退化为透传。

**关键接口**

- `CircuitBreaker`：熔断器实例
- `New(cfg Config) *CircuitBreaker`：构造
- `(cb *CircuitBreaker) Execute(fn func() error) error`：通过熔断器执行函数
- `(cb *CircuitBreaker) State() string`/`Enabled() bool`/`FailureCount() int`/`Reset()`
- `BreakerSet`：熔断器集合（按 key 隔离）
- `NewBreakerSet(template Config) *BreakerSet`
- `(bs *BreakerSet) Get(key) *CircuitBreaker`/`Execute(key, fn)`/`States()`/`Len()`

**核心数据结构**

- `Config`：含 `Name`/`FailureThreshold`/`RecoveryTimeout`/`HalfOpenMaxCalls`/`OnStateChange`
- `CircuitBreaker`：含 `cfg` + `mu sync.Mutex` + `state`/`failureCount`/`openedAt`/`halfOpenCalls`/`halfOpenSucc`
- 状态常量：`StateClosed`/`StateOpen`/`StateHalfOpen`
- `ErrCircuitOpen`：sentinel error
- `StateChangeCallback`：状态变更回调

**关键算法**

- `Execute`：禁用模式透传；申请许可（beforeCall）→ fn → 更新状态（afterCall）
- `beforeCall`：Closed 放行；Open 已过 RecoveryTimeout → 转 HalfOpen 放行首个探测；HalfOpen 探测名额未满放行
- `afterCall`：Closed 成功清零失败计数 / 失败累加达阈值转 Open；HalfOpen 成功累加达 HalfOpenMaxCalls 转 Closed / 失败立即转 Open
- `transition`：切换状态并触发回调（已持锁，回调内禁止再次调用 Execute 避免死锁）
- `BreakerSet.Get`：首次访问 key 时以 template 配置创建（Name 字段被 key 覆盖）

**并发安全**

- `CircuitBreaker.mu sync.Mutex` 保护所有状态字段
- `BreakerSet.mu sync.Mutex` 保护 breakers map

**扩展点**

- `Config`：可注入自定义配置
- `OnStateChange` 回调：可注入状态变更通知
- `BreakerSet`：可按 deviceID/tenantID/IP 隔离

#### 3.6.4 approval

**职责描述**

审批引擎：管理审批流定义与审批请求的状态机推进。支持多步审批流（sequential/parallel 模式）+ 状态机 + 历史记录 + 超时处理 + 通知回调。线程安全：所有公共方法通过 mu 保护 flows/requests/histories 索引，通知回调在锁外执行避免死锁。

**关键接口**

- `Engine`：审批引擎主体
- `New(opts ...Option) *Engine`：函数选项模式构造
- `(e *Engine) CreateFlow/UpdateFlow/DeleteFlow/GetFlow/ListFlows`：审批流 CRUD
- `(e *Engine) Submit(req) error`：提交审批请求
- `(e *Engine) Approve/Reject(requestID, userID, comment) error`：审批决策
- `(e *Engine) Cancel(requestID, userID) error`：取消请求
- `(e *Engine) CheckTimeout(requestID) (bool, error)`：超时检查
- `(e *Engine) GetRequest/ListRequests/ListPendingApprovals/GetHistory`：查询
- `Option`：`WithNotifier`/`WithNow`

**核心数据结构**

- `Engine`：含 `flows`/`requests`/`histories` map + `sync.RWMutex` + `notifier` + `now`
- `ApprovalFlow`：含 `ID`/`TenantID`/`Steps`/`Enabled`/`CreatedAt`/`UpdatedAt`
- `ApprovalRequest`：含 `ID`/`FlowID`/`TenantID`/`Status`/`CurrentStep`/`Steps`/`Operator`/`CreatedAt`
- `History`：含 `RequestID`/`Timeline []HistoryEntry`
- 状态常量：`StatusPending`/`StatusApproved`/`StatusRejected`/`StatusCancelled`/`StatusTimeout`
- 步骤模式：`StepSequential`/`StepParallel`
- sentinel error：`ErrFlowNotFound`/`ErrFlowExists`/`ErrRequestNotFound`/`ErrRequestExists`/`ErrNotPending`/`ErrInvalidTransition`

**关键算法**

- `Submit`：校验 + 关联 Flow + 初始化 Status=pending/CurrentStep=1/各步骤快照 + 触发 submit 通知
- `applyDecision`：Approve/Reject 共用实现，追加决策 → evaluateStep 判定步骤是否完成 → 完成则推进到下一步或整体完成
- `evaluateStep`：sequential 模式按顺序判定；parallel 模式按人数/比例判定
- `CheckTimeout`：整体过期 → timeout；当前步骤超时 → 步骤 reject → 整体 reject
- `ListPendingApprovals`：返回指定用户待审批的请求（sequential 模式仅当前应审批人可见）
- 深拷贝：`cloneRequest`/`cloneFlow`/`cloneHistory` 隔离外部修改

**并发安全**

- `Engine.mu sync.RWMutex` 保护 flows/requests/histories
- 通知回调在锁外执行（snapshot 深拷贝后释放锁），避免回调内再次调用 Engine 方法导致死锁

**扩展点**

- `NotifierFunc` 回调：可注入审批通知
- `WithNow`：可注入虚拟时钟（测试用）
- 步骤模式：可扩展（如会签 / 加签）

#### 3.6.5 cmdb

**职责描述**

CMDB CI 管理 + 关系拓扑 + 导入导出 + 轻量审批流。HTTP 处理器暴露 `/api/v1/cmdb/*` REST API，支持 CI CRUD / CI 类型 / 关系拓扑 / 属性模板 / 导入导出（CSV/JSON）/ 待审列表 / 审批。`HandleReport` 处理 agent 心跳中的 CMDB 增量上报，幂等 upsert。

**关键接口**

- `Handler`：HTTP 处理器主体
- `NewHandler(store CiStore) *Handler`：构造
- `(h *Handler) RegisterRoutes(mux)`：注入路由
- `(h *Handler) HandleReport(ctx, agentID, report)`：处理 agent CMDB 上报
- `CiStore` 接口：CI CRUD + 关系 + 类型 + 属性模板 + 审批
- 实现：`MemoryCiStore`/`SQLCiStore`

**核心数据结构**

- `CiItem`：含 `ID`/`CiType`/`Name`/`TenantID`/`Status`/`ApprovalStatus`/`Source`/`AgentID`/`DeviceID`/`Attrs`/`Version`/`CreatedAt`/`UpdatedAt`
- `CiType`：CI 类型定义
- `CiRelation`：CI 关系（含/源/目标/类型）
- `AttrTemplate`：属性模板
- 审批状态常量：`ApprovalPending`/`ApprovalApproved`/`ApprovalRejected`

**关键算法**

- `HandleReport`：按 agentID 匹配现有 CI，找不到则创建，更新属性（幂等 upsert）
- 导入：JSON 数组或 CSV（`?format=csv`），按 ID upsert，跨租户 ID 冲突时换新 ID
- 导出：JSON 或 CSV，`ciAttrsToCSV` 序列化属性为 `k=v;k2=v2` 形式
- 轻量审批：手动 API 创建的 CI 进入 `ApprovalPending` 状态，经 approve/reject 端点流转
- `idTakenByOtherTenant`：探测 ID 是否已被其它租户占用

**并发安全**

- `CiStore` 内部自带互斥（Memory）或依赖 MySQL 行锁（SQL）
- `Handler` 无内部状态

**扩展点**

- `CiStore` 接口：可新增后端实现
- `HandleReport`：可扩展采集的 CI 类型与属性
- 审批流：可接入 `approval.Engine` 实现完整审批

#### 3.6.6 cron

**职责描述**

cron 表达式解析 + 定时任务管理 + SLA。`Manager` 维护 `ScheduleEntry` 索引（CRUD + 暂停/恢复），与 `Scheduler` 协同：`Scheduler` 周期扫描 store 中带 Schedule 的 Task 模板并派生实例，`Manager` 维护元数据索引供 API 层 CRUD。包含 DAG 调度（`dag.go`）+ SLA 计算（`sla.go`）+ 下次执行时间计算（`nextrun.go`）。

**关键接口**

- `Manager`：定时任务管理器
- `NewManager() *Manager`：构造
- `(m *Manager) Create/Get/List/Update/Delete/Pause/Resume/MarkFired`
- `Match(expr, now) (bool, error)`：cron 表达式匹配
- `NextRun(expr, from) time.Time`：计算下次执行时间
- `Scheduler`：周期扫描派生实例
- `SLA`：SLA 计算

**核心数据结构**

- `ScheduleEntry`：含 `ID`/`TaskID`/`TenantID`/`Name`/`CronExpr`/`Status`/`CreatedAt`/`UpdatedAt`/`LastRunAt`/`NextRunAt`/`CreatedBy`
- `EntryStatus` 常量：`EntryActive`/`EntryPaused`/`EntryDeleted`
- `Manager`：含 `entries map[string]*ScheduleEntry` + `sync.RWMutex` + `counter` + `now`
- sentinel error：`ErrEntryNotFound`/`ErrEntryExists`

**关键算法**

- cron 表达式解析：5 字段（minute hour day month weekday），支持 `*` / `,` / `-` / `/` 步长
- `NextRun`：从 from 时刻向后扫描，找到第一个匹配时刻
- `MarkFired`：派生实例后更新 LastRunAt 与 NextRunAt
- `Update`：仅允许更新 Name/CronExpr/Status；TaskID/TenantID/CreatedAt 不可变
- `itoa`：简易 uint64 → string（避免引入 strconv 增加依赖）
- SLA 计算：基于预期执行时间与实际执行时间偏差

**并发安全**

- `Manager.mu sync.RWMutex` 保护 entries 索引
- 不依赖 store.Store，仅内存索引，重启后从 store.AllTasks 重建

**扩展点**

- `SetNow`：可注入虚拟时钟（测试用）
- cron 表达式：可扩展 7 字段格式（含秒/年）
- SLA 算法：可扩展自定义 SLA 计算逻辑

#### 3.6.7 orchestration

**职责描述**

M5 作业编排中心：DAG 展开 + 子工作流递归 + 条件分支求值 + cron 调度。HTTP 处理器暴露 `/api/v1/workflows` REST API，支持工作流 CRUD / 运行 / 历史运行 / 调度 / 状态查询。`Trigger` 展开工作流 DAG 为底层任务（按 ParentID 归组），复用 `proto.Task.DependsOn` + per-agent releaseDeps 引擎驱动依赖就绪。

**关键接口**

- `Handler`：HTTP 处理器主体
- `NewHandler(st WorkflowStore, eng TaskEngine) *Handler`：构造
- `(h *Handler) RegisterRoutes(mux)`：注入路由
- `(h *Handler) Trigger(ctx, id, tenantID) (*WorkflowDef, error)`：展开工作流 DAG
- `TaskEngine` 防腐接口：`CreateTask`/`TasksByParent`
- `WorkflowStore` 接口：`Create`/`Get`/`List`/`Update`/`CreateRun`/`ListRuns`（Memory / SQL 实现）

**核心数据结构**

- `WorkflowDef`：含 `ID`/`Name`/`TenantID`/`AgentID`/`DAG`/`Cron`/`Status`/`LastRunAt`/`LastRunStatus`
- `WorkflowNode`：含 `ID`/`Type`/`Command`/`Path`/`DependsOn`/`SubWorkflowID`/`Condition`/`ThenNodes`/`ElseNodes`/`Timeout`/`RetryCount`/`RetryDelay`
- 节点类型常量：`NodeShell`/`NodeFile`/`NodeService`/`NodeWorkflow`/`NodeCondition`
- `WorkflowRun`：含 `WorkflowID`/`TenantID`/`StartedAt`/`Status`/`NodeStates`
- 状态常量：`StatusDraft`/`StatusActive`/`StatusRunning`/`StatusSuccess`/`StatusFailed`
- `maxExpandDepth = 10`：子工作流递归深度上限
- sentinel error：`ErrWFNotFound`/`ErrWFTenantMismatch`

**关键算法**

- `Trigger`：解析 DAG → `dag.Validate` 校验 → `expandNodes` 递归展开 → 更新工作流状态 → 创建 WorkflowRun 记录
- `expandNodes` 递归：shell/file/service 直接创建底层任务；workflow 递归展开子工作流（depth+1）；condition 求值后跳过未选中分支
- `evalCondition`：条件表达式求值，支持 `${nodeID.status}` / `${nodeID.exitCode}` 引用 + `==` / `!=` 比较 + `&&` / `||` 组合
- `splitLogical`：按 op 分割表达式，忽略双引号内的 op
- `resolveValue`：解析 `${...}` 变量引用 / 带引号字面量 / 裸字面量
- 节点 ID 前缀：`prefix+nodeID` 避免冲突；子工作流 `prefix+nodeID+"-sub-"`
- 继承依赖：无自身依赖时继承父节点依赖（子工作流入口 / condition 分支节点）

**并发安全**

- `WorkflowStore` 内部自带互斥（Memory）或依赖 MySQL 行锁（SQL）
- `Handler` 无内部状态

**扩展点**

- `TaskEngine` 接口：可替换任务派发底层
- `WorkflowStore` 接口：可新增后端实现
- 节点类型：可扩展（如 HTTP 调用 / 等待节点）
- 条件表达式：可扩展语法（如括号嵌套 / 数学运算）

### 3.7 其他包

其他包提供 OpsMesh 的共享数据类型与版本信息。

#### 3.7.1 proto

**职责描述**

控制面与 agent 之间共享的数据类型（JSON 友好）。同一份二进制，控制面与 agent 复用这些结构。设计上刻意只使用 JSON（不引入 protobuf 工具链）：gRPC 9090 走 JSON codec 传输，HTTP 仅 B/S 仪表盘。

**关键接口**

- 全部为数据结构，无接口

**核心数据结构**

- `AgentInfo`：agent 注册/心跳元信息（含 `InstallToken`/`OnboardDeviceID`/`OS`/`Arch`）
- `DeviceInfo`：被纳管设备（含 `State`/`Managed`/`Retired`/`Hostname`/`OS`/`Arch`）
- `DeviceMetrics`：设备实时监控指标（CPU/内存/磁盘/网络/服务/进程数）
- `MetricsSeries`：设备监控指标历史时序
- `CPUMetrics`/`MemMetrics`/`DiskMetrics`/`NetMetrics`/`ServiceInfo`：分项指标
- `Task`：自动化任务（含 `ClaimEpoch`/`RetryCount`/`MaxRetries`/`DeadLetter`/`Schedule`/`ParentID`/`DependsOn`/`ApprovalRequired`/`Timeout`/`RetryDelay`）
- `TaskResult`：任务执行结果（含 `ClaimEpoch`）
- `AuditEvent`：审计事件（含 `TraceID`）
- `Alert`：告警事件（含 `Status`/`AcknowledgedBy`/`SilencedUntil`/`Comment`/`Metric`）
- `CmdbAttr`/`CmdbReport`：CMDB 属性/增量上报
- `LogLine`/`LogReport`：日志行/日志批次
- 任务类型常量：`TaskTypeShell`/`TaskTypeService`/`TaskTypeFile`
- 告警状态常量：`AlertStatusFiring`/`AlertStatusAcknowledged`/`AlertStatusSilenced`

**关键算法**

- 全部为数据结构，无算法
- JSON tag 与外部 API 契约一致（小写键）

**并发安全**

- 全部为值类型，无内部锁
- 并发安全由调用方（store/handler）保证

**扩展点**

- 字段可按需扩展（含 `omitempty` 向后兼容）
- 任务类型可新增（agent 端按 Type 选择执行器）

#### 3.7.2 version

**职责描述**

暴露 OpsMesh 内核版本，供 `--version` 与镜像标签使用。`Commit` / `Date` 由 CI 通过 `-ldflags "-X opsmesh/internal/version.Commit=..."` 注入。

**关键接口**

- 全部为变量，无接口

**核心数据结构**

- `Version = "0.7.0"`：内核语义版本
- `Commit = "dev"`：CI 注入的 git commit
- `Date = "unknown"`：CI 注入的构建时间

**关键算法**

- 无算法

**并发安全**

- 全局变量，启动期一次性设置，运行期只读

**扩展点**

- `Version` 破坏性变更（如 gRPC ServiceName 改名）须升主版本
- CI 可注入额外构建信息（如分支名 / 构建号）

## 第4章 跨包依赖关系

### 4.1 依赖层次

OpsMesh 包依赖遵循"核心 → 领域 → 基础"自顶向下分层，避免循环依赖。

图：包依赖层次示意图

```
┌─────────────────────────────────────────────────────────┐
│  controlplane（装配中心，依赖几乎全部包）              │
└─────────────────────────────────────────────────────────┘
                            │
        ┌───────────────────┼───────────────────┐
        ▼                   ▼                   ▼
┌──────────────┐  ┌──────────────┐  ┌──────────────────────┐
│  agent       │  │  deploy      │  │  orchestration       │
│  (数据面)    │  │  (M3 部署)   │  │  (M5 编排)           │
└──────────────┘  └──────────────┘  └──────────────────────┘
        │                   │                   │
        ▼                   ▼                   ▼
┌─────────────────────────────────────────────────────────┐
│  domain（纯领域模型）  ←  proto（共享数据类型）         │
└─────────────────────────────────────────────────────────┘
                            │
        ┌───────────────────┼───────────────────┐
        ▼                   ▼                   ▼
┌──────────────┐  ┌──────────────┐  ┌──────────────────────┐
│  store       │  │  alertengine │  │  notify              │
│  (持久化)    │  │  (告警引擎)  │  │  (通知)              │
└──────────────┘  └──────────────┘  └──────────────────────┘
                            │
        ┌───────────────────┼───────────────────┐
        ▼                   ▼                   ▼
┌──────────────┐  ┌──────────────┐  ┌──────────────────────┐
│  config      │  │  authctx     │  │  logx / metrics      │
│  (配置)      │  │  (身份)      │  │  (可观测)            │
└──────────────┘  └──────────────┘  └──────────────────────┘
                            │
        ┌───────────────────┼───────────────────┐
        ▼                   ▼                   ▼
┌──────────────┐  ┌──────────────┐  ┌──────────────────────┐
│  grpcx       │  │  dag         │  │  circuitbreaker      │
│  (gRPC)      │  │  (DAG)       │  │  (熔断)              │
└──────────────┘  └──────────────┘  └──────────────────────┘
                            │
        ┌───────────────────┼───────────────────┐
        ▼                   ▼                   ▼
┌──────────────┐  ┌──────────────┐  ┌──────────────────────┐
│  provision   │  │  helm / k8s  │  │  discover / discovery│
│  (SSH 推送)  │  │  (应用商店)  │  │  (网段/服务发现)     │
└──────────────┘  └──────────────┘  └──────────────────────┘
                            │
        ┌───────────────────┼───────────────────┐
        ▼                   ▼                   ▼
┌──────────────┐  ┌──────────────┐  ┌──────────────────────┐
│  secrets     │  │  tlsutil     │  │  otelx / events      │
│  (密钥)      │  │  (TLS)       │  │  (追踪/事件)         │
└──────────────┘  └──────────────┘  └──────────────────────┘
                            │
                            ▼
              ┌──────────────────────────┐
              │  version（版本号）       │
              └──────────────────────────┘
```

### 4.2 关键防腐接口

为避免反向依赖，OpsMesh 在包边界定义若干防腐接口（ACL）：

表：防腐接口对照表

| 防腐接口 | 定义包 | 实现方 | 用途 |
|---------|--------|--------|------|
| `deploy.Dispatcher` | deploy | controlplane（storeDispatcher 适配 store.Store） | M3 部署派发任务到 M4 任务引擎 |
| `orchestration.TaskEngine` | orchestration | controlplane（store.Store 直接适配） | M5 编排展开 DAG 时下发底层任务 |
| `alertengine.MetricsProvider` | alertengine | controlplane（基于 store.DeviceMetrics） | 告警规则评估查询设备指标 |
| `events.Bus` | events | controlplane（按 cfg.EventBus 选择 noop/log/kafka） | 内核产出事件统一发布 |
| `secrets.SecretProvider` | secrets | controlplane（按 cfg.SecretProvider 选择 env/file/vault/chain） | 告警通道密钥外置 |
| `discovery.ServiceDiscovery` | discovery | agent（按 cfg.ControlplaneEndpoints 选择 static/noop） | agent 动态获取控制面实例列表 |
| `helm.HelmCLI` | helm | helm（CLI 实现 / 测试 mock） | Helm Release 管理命令封装 |
| `k8s.ClusterManager` | k8s | controlplane（NewServer 构造） | K8s 多集群连接管理 |

### 4.3 共享数据类型

`proto` 包作为共享数据类型层，被以下包消费：

- 控制面侧：controlplane / store / deploy / orchestration / cmdb / alertengine / notify / events / logstore
- 数据面侧：agent / grpcx
- 防腐映射：domain（mapper.go 提供 proto ↔ domain 转换）

## 第5章 设计原则总结

### 5.1 分层与解耦

- **DDD 分层**：domain（纯领域模型）← proto（传输层）← handler（编排层）← store（持久化层）
- **防腐层（ACL）**：跨包边界定义小接口，避免反向依赖（见 4.2）
- **接口拆分**：将巨型 Store 接口按领域拆为 17 个领域小接口，消费方可按需依赖最小接口

### 5.2 并发安全策略

- **细粒度锁**：每域独立 `sync.RWMutex`，避免全局锁竞争
- **快照后释放锁**：评估/查询时取快照后释放锁，再调用外部方法，避免长时持锁与重入死锁
- **深拷贝隔离**：返回值经深拷贝隔离外部修改（如 `cloneRequest`/`cloneFlow`）
- **channel 通信**：worker 池 / SSE 广播 / 信号量并发限制优先用 channel
- **回调在锁外执行**：通知回调（如 approval.NotifierFunc）在锁外执行，避免回调内再次调用 Engine 方法导致死锁

### 5.3 可插拔扩展

- **函数选项模式**：`NewNotifier(opts ...NotifierOption)` / `NewEngine(opts ...Option)` 等构造器统一用函数选项注入依赖
- **接口注入**：`MetricsProvider` / `SecretProvider` / `ServiceDiscovery` / `Channel` 等接口可按需替换实现
- **编译标签**：Kafka 生产者置于 `//go:build kafka` 标签，默认构建不引入重依赖
- **no-op 降级**：OTel / 熔断器 / 服务发现等可选功能在未配置时退化为 no-op（零开销，向后兼容）

### 5.4 安全基座

- **等保三级**：行级租户隔离 + 操作 100% 留痕 + 身份上下文网关注入
- **安全加固**：kubeconfig AES-256-GCM 加密 + 生产模式 fail-fast + JWT 验签
- **H3 TLS 强制**：TLS 1.2+ + 禁用弱协议版本 + mTLS 双向认证
- **kubeconfig 安全**：拒绝 exec/auth-provider 凭据插件 + 强制关闭 insecure-skip-tls-verify
- **F16 / M12 SSH 安全**：KnownHosts 主机指纹校验 + 未配置时显眼警告
- **安全头**：CSP nonce + HSTS + Permissions-Policy

### 5.5 可观测性

- **链路追踪**：OTel SDK + gRPC/HTTP 自动埋点 + W3C Trace Context 透传
- **分布式可观测**：日志/SSE 事件/审计日志自动关联 OTel trace_id
- **指标**：零依赖 Prometheus 文本格式 + HTTP 请求延迟直方图 + Go runtime 指标
- **事件总线**：noop/log/kafka 三实现 + 事件信封含 SchemaVersion 跨版本演进

## 第6章 附录

### 6.1 包文件统计

表：包文件数量统计表

| 包名 | .go 文件数 | _test.go 文件数 | 总行数（估） |
|------|-----------|----------------|-------------|
| controlplane | 50 | 37 | ~15000 |
| store | 25 | 16 | ~10000 |
| agent | 12 | 11 | ~3000 |
| alertengine | 6 | 4 | ~2000 |
| notify | 9 | 6 | ~2500 |
| approval | 6 | 5 | ~2000 |
| cmdb | 5 | 1 | ~1500 |
| cron | 6 | 5 | ~1500 |
| orchestration | 4 | 5 | ~1500 |
| deploy | 6 | 4 | ~2000 |
| helm | 5 | 5 | ~1500 |
| logstore | 10 | 5 | ~2500 |
| k8s | 2 | 2 | ~500 |
| discovery | 6 | 4 | ~800 |
| config | 1 | 4 | ~1500 |
| 其他 15 个包 | — | — | ~3000 |

### 6.2 术语表

表：术语对照表

| 术语 | 含义 |
|------|------|
| | 网段自动纳管：服务部署后整段网络打通，设备自动纳管 |
| | 数据本地化 + 等保三级：私有部署 + 行级租户隔离 + 操作留痕 |
| | 双模式单二进制：控制面与 agent 共用同一份二进制，通过 --mode 切换 |
| | 任务必达：agent 失联复位 + 重试 + 死信 |
| | 真实网段发现：discover 扫描 + 候选设备 + provision 推送 |
| | 进程资源限额：RLIMIT_NPROC/NOFILE/AS + 任务超时 |
| | HA 领取：多副本控制面并发领取同一任务只会被一个副本领取 |
| | 事件总线：noop/log/kafka 三实现 |
| | TLS/mTLS：gRPC 传输层凭证 + 联邦通道硬化 |
| | Store 接口拆分：巨型接口按领域拆为 17 个领域小接口 |
| | DDD 实质化：领域实体承载业务行为（状态机/重试判定/纳管翻转） |
| M3 | 部署中心：滚动/金丝雀/蓝绿 + 门禁 + 自动回滚 |
| M5 | 作业编排：DAG 展开 + 子工作流 + 条件分支 |
| M6 | 日志检索：Memory/SQL/Loki/ES + 倒排索引 |
| M7 | 监控告警：告警事件 + ack/silence |
| B1 | 自动纳管闭环：网段发现 → SSH 推送 → agent 注册 → 设备纳管 |
| B7 | 告警通知增强：聚合 + 抑制 + 多通道 |
| F3 | 任务取消：控制面下发取消 + agent 轮询取消信号 |
| F5 | 设备退役：离线超龄自动归档 |
| ACL | 防腐层（Anti-Corruption Layer）：跨包边界定义小接口避免反向依赖 |
| DDD | 领域驱动设计（Domain-Driven Design） |

---

文档版本：v1.0  |  生成日期：2026-08-17  |  覆盖包数：30  |  维护者：OpsMesh 技术文档工程师