# OpsMesh — 网段运维中枢

**OpsMesh** 是私有化单中心 B/S 自动化部署与运维平台。服务部署到某网段后，整段网络打通的设备自动纳管，各设备可并行执行各自的自动化任务（shell 脚本/服务管理/文件分发），支持失败重试/死信/取消/定时周期/告警等完整任务生命周期。

管控通道采用 **自研 gRPC（direct + proxy）**。原蓝鲸 GSE 社区版底座已移出 MVP，降格为可选增强（见 `DELIVERY.md`）。

---

## 架构概览

```
+----------------------------------------------------------------------------------------------------+
|                                           客 户 端 层                                              |
|                                                                                                    |
|                 | 企业版前端 web/enterprise/ |   | 内嵌个人版引导页           |                    |
|                 | Vue3 + Vite + Pinia        |   | internal/controlplane/web/ |                    |
|                 | + VueRouter + i18n ;       |   | GET / 重定向 /enterprise/  |                    |
|                 | SPA 独立构建部署           |   | bootstrap:                 |                    |
|                 | (Nginx/CDN 托管 dist/)     |   | /install.sh                |                    |
+----------------------------------------------------------------------------------------------------+
           |  HTTP :8080   REST API (/api/v1/**)  +  SSE 实时推送 (/api/v1/events/stream)
           |  认证: JWT (at/rt HttpOnly Cookie) / Bearer om_xxxxxxxx API Key (RBAC scope)
           v
+------------------------------------------------------------------+            +--------------------+
|     控制面 controlplane ( cmd/opsmesh --mode=controlplane )      |            | 其它网段控制面     |
|            | HTTP :8080        |   | gRPC :9090    |             |            |                    |
|            | REST API + B/S    |   | agent 通道    |             |            |                    |
|            | SSE events/stream |   | mTLS 生产强制 |             |            |                    |
|            | Metrics :9091   |   | Federation :9092 |            |            |                    |
|            | Prometheus 文本 |   | 联邦监听(可选)   |            |            |                    |
|            | /metrics 端点   |   | 强制对端持证     |            |            |                    |
|                                                                  |            |                    |
|                      gRPC mTLS + HMAC 签名                       |            |                    |
|                       (--federation-peers)                       |            | peer 联邦对端      |
|    | grpcx 自研 gRPC            |   | tlsutil TLS/mTLS     |     |    <==>    | 跨段任务转发       |
|    | JSON codec + protobuf 双轨 |   | 证书热重载(fsnotify) |     |            |                    |
|    | (proto/ buf 代码生成)      |   | Production 强制 TLS  |     |            | 设备视图同步       |
|                                                                  |            |                    |
|        | alertengine           |   | orchestration+dag |         |            |                    |
|        | Z-Score+EWMA 异常检测 |   | 作业流 DAG 编排   |         |            |                    |
|     | deploy + cmdb           |   | logstore+k8s+helm     |      |            |                    |
|     | 三策略部署 / 配置库图谱 |   | Loki+ES 日志 / 多集群 |      |            |                    |
|      | automation+network    |   | API Key 认证           |      |            |                    |
|      | 自动化闭环 / 网络拓扑 |   | Bearer om_* RBAC scope |      |            |                    |
|                                                                  |            |                    |
|     | MemoryStore 零依赖   |   | MultiSchemaStore 租户隔离 |     |            |                    |
|     | SQLStore MySQL+Redis |   | SessionStore memory|redis |     |            |                    |
+------------------------------------------------------------------+            +--------------------+
           |  gRPC :9090  Register/Heartbeat/PullTasks/ReportResult/CancelTask/ReportLogs
           |  agent 侧 discovery: 多控制面 failover / round-robin
           v
+----------------------------------------------------------------------------------------------------+
|                   Agent 集群  ( 每台纳管设备一个 , cmd/opsmesh --mode=agent )                      |
|                                                                                                    |
|             | internal/agent worker pool     |   | log_collect 日志采集 v2.0      |                |
|             | shell/svc(systemctl)/file 分发 |   | 多行合并 / 过滤规则 / 增量采集 |                |
|             | exec.CommandContext 超时中止   |   | gRPC ReportLogs 上报           |                |
|             | + rlimit ; cancelLoop 取消     |   | -> Loki / Elasticsearch 直推   |                |
|                                                                                                    |
|      网段 10.30.0.0/24  ...                                                                        |
+----------------------------------------------------------------------------------------------------+

           |  Prometheus 抓取 :9091/metrics ; agent 日志直推 Loki/ES
           |  K8s Operator 经 K8s API reconcile ; helm install 部署 Chart
           v
+----------------------------------------------------------------------------------------------------+
|                                           运 维 生 态                                              |
|                                                                                                    |
|  | K8s Operator (operator/)   |   | Prometheus             |   | Loki / Elasticsearch  |           |
|  | OpsMeshInstance CRD        |   | 抓取 :9091 /metrics    |   | --log-backend=loki|es |           |
|  | reconcile: CP Deployment + |   | HTTP 延迟 / Go runtime |   | agent 直推 +          |           |
|  | agent DS + MySQL/Redis STS |   |                        |   | logstore 统一检索     |           |
|  | Helm Chart          |   | docker-compose/systemd |                                              |
|  | deploy/helm/opsmesh |   | 本地全栈 / 裸机 VM     |                                              |
|  | Ingress + HPA       |   |                        |                                              |
+----------------------------------------------------------------------------------------------------+
```

**图中组件速览**：

- **客户端层**：企业版前端（`web/enterprise/`，Vue3 + Vite + Pinia + VueRouter + i18n，SPA 独立构建部署）；内嵌个人版引导页（`internal/controlplane/web/`，GET / 重定向 `/enterprise/`，保留 bootstrap 端点）
- **四监听器**：HTTP :8080（REST API + B/S + SSE `/api/v1/events/stream` 实时推送）、gRPC :9090（agent 通道，mTLS 生产强制）、Metrics :9091（Prometheus `/metrics` 文本端点）、Federation :9092（可选独立 mTLS 联邦监听，强制对端持证）
- **传输与安全**：`internal/grpcx` 自研 gRPC（JSON codec 与 protobuf 双轨，`proto/` buf 代码生成）；`internal/tlsutil` TLS/mTLS + 证书热重载（fsnotify，`--production` 强制 TLS）；认证走 JWT Cookie / Bearer `om_*` API Key / 网关头注入三条路径，RBAC 权限校验
- **核心引擎**：`alertengine`（多条件规则 + Z-Score/EWMA 异常检测 + 静默/抑制/聚合）、`orchestration`+`dag`（作业流编排）、`deploy`+`cmdb`（三策略部署/配置库图谱）、`logstore`+`k8s`+`helm`（Loki+ES 日志检索/多集群管理）、`automation`+`network`(自动化闭环/网络拓扑)、API Key 认证引擎
- **Store 层**：MemoryStore 零依赖兜底 / SQLStore（MySQL 8 + Redis 7）/ MultiSchemaStore 多租户 schema 隔离（`--multi-schema`）/ SessionStore（memory|redis）
- **控制面联邦**：`--federation-peers` 声明对端，经 gRPC mTLS + HMAC 签名互联多个控制面（跨段任务转发 / 设备视图同步）
- **Agent 集群**：worker pool 执行 shell/svc/file 任务；log_collect 日志采集 v2.0（多行合并/过滤规则/增量采集，经 gRPC ReportLogs 上报或直推 Loki/Elasticsearch）
- **运维生态**：K8s Operator（`operator/`，OpsMeshInstance CRD 声明式拉起控制面 Deployment + agent DaemonSet + MySQL/Redis StatefulSet）；Prometheus 抓取 `:9091/metrics`；Loki/Elasticsearch 日志后端；Helm Chart / docker-compose / systemd 三套部署形态

### 技术栈

| 层 | 选型 | 版本 / 说明 |
|---|---|---|
| 后端语言 | Go | 1.26（`go.mod` 声明 `go 1.26.0`，toolchain `go1.26.6`） |
| HTTP / gRPC | `net/http` + `google.golang.org/grpc` | 自研 JSON codec + 手写 ServiceDesc，可选 protobuf 双轨 |
| 持久化 | MySQL 8 + Redis 7 | `go-sql-driver/mysql` + `redis/go-redis/v9`；MemoryStore 零依赖兜底 |
| 消息 / 事件 | `segmentio/kafka-go` | `//go:build kafka` 编译标签，默认构建不引入 |
| 安全 | `golang-jwt/jwt/v5` + `golang.org/x/crypto` | JWT 双 Token + bcrypt + HMAC 签名 + AES-256-GCM |
| K8s 客户端 | `k8s.io/client-go` | 多集群管理，无 kubectl 依赖 |
| 可观测 | OTel + 自研 Prometheus 文本指标 | `otelx` HTTP/gRPC 自动埋点 + OTLP 导出；零依赖 metrics |
| 系统采集 | `shirou/gopsutil/v3` | agent 负载上报 |
| 前端 | Vue 3 + Vite + Pinia + Vue Router | 企业版 SPA（`web/enterprise/`），独立构建部署 |
| 前端测试 | Vitest + @vue/test-utils + jsdom + Playwright | 单测 + E2E（含 e2e-real 真实后端联调） |
| 部署 | Helm Chart + docker-compose + systemd | 三套部署形态，详见 [快速启动](#快速启动零依赖-30-秒) 与 [生产部署](#生产部署mysql--redis--tls--多副本) |
| CI | GitHub Actions | build-test / integration / security / proto / frontend / E2E / image / release |

### 通信模型

| 通道 | 协议 | 端口 | 用途 |
|---|---|---|---|
| 注册 | gRPC (JSON) | 9090 | agent 上报元信息，服务端盖章租户、分配 agentID |
| 心跳 | gRPC (JSON) | 9090 | agent 每 10s 上报在线状态与负载 |
| 拉任务 | gRPC (JSON) | 9090 | agent 原子领取下一条 pending 任务（多副本安全） |
| 上报结果 | gRPC (JSON) | 9090 | 任务执行完毕回写 stdout/stderr/exitCode |
| 取消信号 | gRPC (JSON) | 9090 | agent 轮询 PollCancels，立即中止正在执行的任务 |
| 仪表盘 | HTTP | 8080 | B/S 看板：设备/任务/告警/审计/纳管操作 |
| 指标 | HTTP | 9091 | Prometheus 文本格式观测指标 |
| SSE 事件 | HTTP | 8080 | 实时推送任务状态/告警/设备上下线 |
| 联邦 | gRPC (mTLS) | 9090 | 跨控制面任务转发/设备视图同步 |
| Metrics | HTTP | 9091 | Prometheus 指标采集（HTTP 延迟/Go runtime） |

### internal 包职责（36 个）

> 完整设计见 `docs/module-design.md`。下表按 8 个领域分组列出 36 个 internal 包的职责简述。

#### 设备与纳管域

| 包 | 职责 |
|---|---|
| `internal/agent` | agent 运行时：经 gRPC 注册/心跳/拉任务/上报结果，本地 worker 池执行 shell/svc/file 任务 |
| `internal/discover` | **设备发现**：扫描指定网段（TCP 存活），发现可纳管设备（SSH/agent 已安装），返回设备清单供控制面注册 |
| `internal/discovery` | **控制面服务发现 + 负载均衡**：agent 启动时发现控制面地址（静态/动态），经 balancer 做 failover/round-robin，实现多控制面 HA |
| `internal/provision` | 自动纳管闭环：install token 签发/消费 + SSH 推送 bootstrap + 候选设备状态机 |
| `internal/domain` | 纯领域模型（DDD）：与 proto 解耦，含 Cancel/CanRetry/MarkDead 等业务行为方法 + 防腐层 mapper |

#### 控制面域

| 包 | 职责 |
|---|---|
| `internal/controlplane` | 控制面核心：HTTP 路由 + gRPC server + Registry + dashboard + 14 个功能域 handler（按 `server_*.go` 拆分） |
| `internal/config` | 统一配置：116 个 flag，命令行优先 + `OPSMESH_*` 环境变量兜底 |
| `internal/authctx` | 网关注入身份提取：从 HTTP 头 / gRPC metadata 提取 X-Tenant-ID / X-User-Id / X-User-Roles |
| `internal/grpcx` | 自研 gRPC 传输层：JSON codec + 手写 ServiceDesc + pb stub 双轨（`proto/` buf 代码生成） |
| `internal/tlsutil` | gRPC TLS / mTLS 工具 + 证书热重载（fsnotify watch，无需重启更新 TLS 配置） |
| `internal/version` | 构建版本注入：`--version` 与镜像标签 |

#### 任务与编排域

| 包 | 职责 |
|---|---|
| `internal/cron` | 5 字段 cron 表达式求值（分 时 日 月 周），F4 定时/周期调度 |
| `internal/dag` | DAG 引擎：拓扑排序（Kahn）+ 环检测 + 依赖就绪判定，M5 作业编排底座 |
| `internal/orchestration` | 作业编排（M5）：DAG 调度 + store 阻塞→释放链路 + 画布 + 子工作流 + 条件分支 + 节点级超时重试 |
| `internal/deploy` | 服务部署（M3）：计划 + fan-out 执行 + Reconcile + Rollback + 三策略（rolling/canary/bluegreen）+ 灰度自适应推进 + 多集群联邦发布 |
| `internal/approval` | 审批引擎：审批流定义 + 请求提交 + approve/reject/cancel + 越权防护 |

#### 运维能力域

| 包 | 职责 |
|---|---|
| `internal/cmdb` | 配置库 CMDB（M2）：模型 + 实例 CRUD + SQL + 采集 + 关系图谱 + 变更审批 |
| `internal/logstore` | 日志检索（M6）：双后端（Memory/SQL）+ 外部后端（Loki/ES）+ 倒排索引 + offset 分页 |
| `internal/alertengine` | 告警规则引擎：多条件匹配 + Z-Score/EWMA 异常检测 + 静默 + 抑制 + 聚合 + 通知分发 |
| `internal/notify` | 通知渠道：Webhook / 飞书 / 钉钉 / 企业微信 / Slack / 邮件（SMTP）+ 通知模板 |
| `internal/k8s` | K8s 多集群管理：client-go 封装 + ClusterManager + 资源 CRUD + scale/restart/rollback |
| `internal/helm` | Helm 应用商店：仓库管理 + Chart 搜索 + Release 部署/回滚 + 预置 24 个应用目录 |
| `internal/network` | 网络管理引擎：网络设备模型（switch/router/firewall/LB）+ 监控指标 + 拓扑邻接表 + 子网发现 |
| `internal/automation` | 自动化闭环引擎：规则（条件→动作）+ 触发器（alert/metric_threshold/schedule/event）+ 动作（execute_task/send_notify/scale/restart/isolate）+ 规则引擎 Evaluate |

#### 存储与安全域

| 包 | 职责 |
|---|---|
| `internal/store` | Store 接口 + MemoryStore + SQLStore：35 个领域子接口 + 编译期双实现断言 + 多租户 schema 隔离（MultiSchemaStore） |
| `internal/secrets` | 密钥管理：env/file/Vault/KMS 多 provider + 工厂模式 + SSRF 防护 |
| `internal/circuitbreaker` | 通用熔断器：Closed → Open → HalfOpen 状态机，agent 任务执行 + 控制 API 限流降级 |
| `internal/compliance` | 安全合规检查引擎：CIS Benchmark 基线规则（SSH 加固/防火墙/文件权限/密码策略等）+ 自定义规则 + 扫描编排（agent 执行、控制面聚合报告） |

#### 平台化与扩展域

| 包 | 职责 |
|---|---|
| `internal/platform` | 平台化业务引擎：租户管理 + API Key（`om_` 前缀 + SHA-256 hash）+ 插件市场 + 计费（计划/订阅/账单） |
| `internal/plugin` | 插件框架：Plugin 接口 + Hook 扩展点 + HookHandler + Manager（注册/钩子触发/生命周期），不改核心代码扩展控制面行为 |
| `internal/extension` | API 网关引擎：路由规则匹配（PathPrefix 前缀 + 方法白名单）+ 令牌桶限流 + 网关统计聚合 |

#### 可观测与基础设施域

| 包 | 职责 |
|---|---|
| `internal/logx` | 结构化日志（slog JSON）+ traceID 透传（优先 OTel span context，回退显式注入） |
| `internal/metrics` | 零依赖 Prometheus 文本指标：计数器/直方图/仪表盘，不引入 prometheus 客户端库 |
| `internal/otelx` | OTel 集成：HTTP/gRPC 自动埋点 + OTLP 导出 + W3C Trace Context 透传 |
| `internal/events` | 可插拔事件总线：noop / log / kafka（kafka 走 `//go:build kafka` 编译标签） |
| `internal/proto` | 共享数据类型：AgentInfo/DeviceInfo/Task 等，控制面与 agent 复用，JSON 友好 |

#### discover vs discovery 边界说明

> **常见混淆点**：`internal/discover` 与 `internal/discovery` 名字相似但职责完全不同，分属不同领域。

| 包 | 领域 | 职责 | 调用方 | 触发时机 |
|---|---|---|---|---|
| `internal/discover` | 设备纳管 | **设备发现**：扫描网段（`--segment-cidr`），TCP 存活探测，返回可纳管设备清单 | 控制面 `autoProvisionLoop`（`--discover --auto-provision` 开启时） | 控制面启动后周期扫描，或 `POST /api/v1/provision/auto` 手动触发 |
| `internal/discovery` | agent 高可用 | **控制面服务发现 + 负载均衡**：agent 启动时解析 `--control-addrs`（逗号分隔多地址），经 balancer（failover/round-robin/static）选择控制面端点 | agent 启动时 + gRPC 连接断开重连时 | agent 进程启动 + 连接故障切换 |

简记：**discover 找设备（控制面→网段），discovery 找控制面（agent→控制面）**。两者均无相互依赖，分属控制面与 agent 两侧。

---

## 功能矩阵

> 成熟度图例：✅ 功能完整（CI 验证中） ｜ 详见 `DELIVERY.md` §7 CI 状态。CI 集成测试/安全扫描/lint/race 检测需 GitHub Actions runner 真跑，当前标记「阻塞·待外部」。
>
> 14 个功能域对齐 `docs/feature-design.md` 的 F1–F18 功能模块编号与 `docs/product-roadmap.md` 的 M1–M4 里程碑编号，覆盖 OpsMesh 全部已落地能力。

| # | 功能域 | 关键能力 | 状态 | 主入口 |
|---|---|---|---|---|
| 1 | **设备管理** | Agent 即设备（零依赖）/ 真实网段发现（TCP 存活扫描，`--discover`）/ 候选设备纳管（discovered → provisioning → onboarded）/ 设备退役（离线超龄自动归档）/ SSH 自动推送 bootstrap / 设备指纹采集 | ✅ | `internal/controlplane/server_devices.go`、`internal/discover/`、`internal/provision/` |
| 2 | **任务执行** | Shell 命令 / 系统服务管理（systemctl）/ 文件分发（原子写入 + rename）/ 超时自动中止（exec.CommandContext）/ 失败重试 + 死信队列 / 任务取消（pending 拦截 + running 强杀）/ 定时周期调度（5 字段 cron）/ 批量下发 / 租约回收 / 审批门禁 | ✅ | `internal/controlplane/server_tasks.go`、`internal/agent/`、`internal/cron/` |
| 3 | **监控告警** | 任务死信 → critical 告警 / 告警面板 + HTTP 查询 / 告警规则引擎（多条件 + 静默 + 抑制 + 聚合）/ Webhook/飞书/钉钉/企业微信/Slack/邮件多通道 / 告警规则 CRUD / 通知模板 | ✅ | `internal/controlplane/server_alerts.go`、`internal/alertengine/`、`internal/notify/` |
| 4 | **CMDB** | 模型 + 实例 CRUD + SQL 持久化 + 采集自动化 / 关系图谱可视化（SVG 力导向图）/ 变更审批流 / 全文本检索倒排索引（TF-IDF + 短语/布尔/通配符） | ✅ | `internal/cmdb/`、`internal/controlplane/cmdb_*.go` |
| 5 | **日志检索** | 双后端（Memory/SQL）+ 外部后端（Loki/ES）/ offset 分页 / 关键词 + 级别 + 时间窗过滤 / agent gRPC 上报日志 / 倒排索引 | ✅ | `internal/logstore/` |
| 6 | **编排部署** | 服务部署计划 + fan-out 执行 + Reconcile + Rollback / 三策略（rolling/canary/bluegreen）/ 发布门禁（失败率/延迟阈值）+ 自动回滚 + Promote 拥级 / 灰度自适应推进 / 多集群联邦发布 | ✅ | `internal/deploy/`、`internal/controlplane/server_deploy.go` |
| 7 | **OS 优化** | 14+ 预置模板（内核/网络/安全/时间同步/SSH/磁盘/系统/用户）/ 在线 CRUD / 在指定 agent 执行 / 模板 store 持久化 + 幂等 seed | ✅ | `internal/controlplane/os_optimize.go` |
| 8 | **中间件部署** | 10+ 中间件（MySQL/Redis/Kafka/Nginx/Tomcat/Zookeeper/PostgreSQL/MongoDB/RabbitMQ/Elasticsearch）× docker/systemd 双模式 / CRUD + 实例查询 + 卸载 | ✅ | `internal/controlplane/middleware_deploy.go` |
| 9 | **K8s 管理** | 多集群接入（client-go，无 kubectl 依赖）/ 集群增删查 + 测试连接 / 资源只读 + 写（namespace/pod/deployment/service/configmap/secret/node）/ scale/restart/rollback / 租户隔离 / kubeconfig AES-256-GCM 加密落盘 | ✅ | `internal/controlplane/k8s_cluster.go`、`k8s_manage.go`、`internal/k8s/` |
| 10 | **用户中心** | 注册/登录/RBAC（用户/角色/权限三表）/ JWT 双 Token（at/rt HttpOnly Cookie）/ 防爆破 + 失败锁账号 / 注册审批 / 首登强制改密 / Refresh Token 持久化 + 设备指纹绑定 | ✅ | `internal/controlplane/auth*.go` |
| 11 | **审计日志** | 100% 留痕（AuditEvent → audit_log / memory ring）/ 审计检索（租户/动作/时间窗过滤）/ 等保三级 ≥6 月导出 / bootstrap 端点审计 | ✅ | `internal/controlplane/server_audits.go` |
| 12 | **联邦** | 跨网段任务转发 / 联邦设备视图聚合 / 独立 mTLS 监听 / HMAC 签名验签（防伪造/重放）/ 多集群联邦发布协调 | ✅ | `internal/controlplane/federation.go`、`internal/deploy/federation.go` |
| 13 | **SSE 实时推送** | 任务状态/告警/设备上下线事件流 / 替代 5s 轮询 / 心跳保活 / 契约守护测试（9 事件名 + 信封 + 心跳） | ✅ | `internal/controlplane/sse.go`、`docs/sse-protocol.md` |
| 14 | **工作流** | DAG 引擎（拓扑排序 + 环检测 + 依赖就绪判定）/ 子工作流展开 + 条件分支 + 节点级超时重试 + 执行历史回放 / 画布编辑 / cron 触发 + reconcile | ✅ | `internal/orchestration/`、`internal/dag/` |

### 横切能力

| 领域 | 功能 | 状态 |
|---|---|---|
| **HA** | 多副本 leader 选举 (leader_lease 表) | ✅ |
| | 超期任务自动回收 (reclaimLoop) | ✅ |
| | 多控制面 agent 端 failover (逗号分隔地址) | ✅ |
| **安全** | 租户行级隔离 (TenantID 行锁) | ✅ |
| | RBAC 头注入 (X-Tenant-ID / X-User-Id / X-User-Roles) | ✅ |
| | 生产模式 (--production：require-auth 默认开) | ✅ |
| | gRPC TLS / mTLS + HMAC 签名 | ✅ |
| | CSP / SSRF / 限流 / 请求体 1 MiB 上限 | ✅ |
| **观测** | Prometheus 文本指标 (agent/队列深度/duration) | ✅ |
| | /healthz 深度检查 + /readyz 就绪探针 | ✅ |
| | OTel 链路追踪（HTTP/gRPC 自动埋点 + OTLP 导出） | ✅ |
| **事件** | 可插拔事件总线 (noop/log/kafka) | ✅ |
| **部署** | 单二进制双模式 (--mode=controlplane|agent) | ✅ |
| | 零依赖启动 (MemoryStore, 无 MySQL/Redis) | ✅ |
| | 生产部署 (MySQL + Redis, 多副本) | ✅ |
| | 容器镜像 (多阶段 Dockerfile) | ✅ |
| | Helm Chart / docker-compose / systemd 三套部署 | ✅ |
| | GitHub Actions CI (lint/test/security/image/e2e-real) | ✅ |

---

## 快速启动（零依赖，30 秒）

> **平台支持**：控制面支持 Linux / Windows / macOS；**agent 仅正式支持 Linux**（任务执行依赖 shell/systemctl/rlimit，Windows 仅可编译、未提供执行能力，详见 `internal/agent/exec_other.go`）。
>
> 三种部署方式按场景选择：**二进制直跑**（开发/体验）→ **docker-compose**（本地全栈）→ **Helm Chart**（K8s 生产）。systemd 部署见 [生产部署](#生产部署mysql--redis--tls--多副本) 章节末尾。

### 方式一：二进制直跑（零依赖，开发体验）

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

### 方式二：docker-compose（本地全栈，含 MySQL + Redis）

`docker-compose.yaml` 一键拉起 controlplane + agent + mysql + redis，适合本地联调与 E2E 测试：

```bash
# 设置必需的环境变量（安全修复：不再内置弱口令）
export MYSQL_ROOT_PASSWORD=$(openssl rand -hex 16)
export OPSMESH_JWT_SECRET=$(openssl rand -hex 32)
export OPSMESH_PROVISION_SECRET=$(openssl rand -hex 32)

# 启动全栈（--build 强制重建镜像，避免缓存旧版本）
docker compose up -d --build

# 查看状态
docker compose ps
# 控制面: http://localhost:8080
# metrics: http://localhost:9091/metrics

# 停止
docker compose down
```

### 方式三：Helm Chart（K8s 生产部署）

仓库自带完整 Helm Chart（`deploy/helm/opsmesh/`，含 17 个模板），一键部署控制面 + agent DaemonSet + MySQL + Redis：

```bash
# 开发/体验：单副本 + memory store
helm install opsmesh ./deploy/helm/opsmesh -n opsmesh --create-namespace

# 生产：3 副本 + mysql 持久化 + TLS + require-auth
helm install opsmesh ./deploy/helm/opsmesh -n opsmesh --create-namespace \
  -f deploy/helm/opsmesh/values-production.yaml \
  --set controlplane.jwtSecret=$(openssl rand -hex 32) \
  --set controlplane.provisionSecret=$(openssl rand -hex 32)
```

### 演示模式

控制面加 `--demo` 启动，每个 agent 注册时自动预置一条 `uname -a` 示例任务，即刻体验下发→执行→上报闭环：

```bash
./opsmesh --mode=controlplane --demo
```

### 配置速查

```bash
# 所有配置项（116 个 flag）
./opsmesh --help
# 查看版本
./opsmesh --version
# 独立健康检查（供 docker-compose healthcheck，不依赖 curl/shell）
./opsmesh --health --control-addr=http://127.0.0.1:8080
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

### Kubernetes 部署（Helm Chart，已提供）

仓库已自带完整 Helm Chart（`deploy/helm/opsmesh/`，含 `Chart.yaml` / `values.yaml` / `values-production.yaml` / `templates/` 全套 17 个模板），可一键部署控制面 + agent DaemonSet + MySQL + Redis：

```bash
# 开发/体验：单副本 + memory store
helm install opsmesh ./deploy/helm/opsmesh -n opsmesh --create-namespace

# 生产：3 副本 + mysql 持久化 + TLS + require-auth（values-production.yaml overlay）
helm install opsmesh ./deploy/helm/opsmesh -n opsmesh --create-namespace \
  -f deploy/helm/opsmesh/values-production.yaml \
  --set controlplane.provisionSecret=$(openssl rand -hex 32)

# 已安装后升级到生产配置
helm upgrade opsmesh ./deploy/helm/opsmesh -n opsmesh \
  -f deploy/helm/opsmesh/values-production.yaml
```

Chart 要点：
- 控制面 `Deployment`（`replicaCount` 可调，>1 时务必 `store=mysql`）+ `PodDisruptionBudget`（minAvailable=1）。
- Agent `DaemonSet`（每节点一个，自动注册到控制面）。
- MySQL / Redis `StatefulSet`（持久化 PV）+ `Secret`（`provision-secret` / `mysql-dsn`）；TLS 证书走 `controlplane.tls.secretName` 预置 Secret（键 `tls.crt` / `tls.key` / `ca.crt`）。
- 进阶开关（`--metrics-allow-cidr`、`--federation-*`）可通过 `controlplane.extraEnv` 注入对应 `OPSMESH_*` 环境变量（配置层已实现 env 兜底）。

> 旧版文档曾标注"Helm 规划中"，与当前仓库实际不符——现已纠正：Chart 已落地可用。Argo CD ApplicationSet 网段批量渲染仍属规划中能力。

容器编排（非 Helm 路径）：`docker-compose.yaml`（controlplane + agent + mysql + redis 一键起）、`Dockerfile`（控制面多阶段）、`Dockerfile.agent`（agent 多阶段）。

### systemd 部署（裸机/VM 生产）

`deploy/systemd/` 提供 systemd unit 文件 + env 模板，适合裸机或 VM 部署（不含 K8s 的环境）：

```bash
# 1. 分发二进制
sudo install -m 0755 opsmesh /usr/local/bin/opsmesh

# 2. 准备配置（编辑 env 文件，填入实际 MySQL/Redis/TLS/JWT 参数）
sudo cp deploy/systemd/opsmesh-controlplane.service /etc/systemd/system/
sudo cp deploy/systemd/opsmesh-controlplane.env /etc/opsmesh/opsmesh-controlplane.env
sudo $EDITOR /etc/opsmesh/opsmesh-controlplane.env
# 关键项：
#   OPSMESH_STORE=mysql
#   OPSMESH_MYSQL_DSN=user:pass@tcp(mysql:3306)/ops_device?charset=utf8mb4
#   OPSMESH_REDIS_ADDR=redis:6379
#   OPSMESH_REPLICAS=2
#   OPSMESH_PRODUCTION=true
#   OPSMESH_JWT_SECRET=<openssl rand -hex 32>
#   OPSMESH_PROVISION_SECRET=<openssl rand -hex 32>

# 3. 启用并启动
sudo systemctl daemon-reload
sudo systemctl enable --now opsmesh-controlplane

# 4. agent 端
sudo cp deploy/systemd/opsmesh-agent.service /etc/systemd/system/
sudo cp deploy/systemd/opsmesh-agent.env /etc/opsmesh/opsmesh-agent.env
sudo $EDITOR /etc/opsmesh/opsmesh-agent.env
# 关键项：
#   OPSMESH_MODE=agent
#   OPSMESH_SEGMENT=seg-a
#   OPSMESH_CONTROL_ADDRS=cp1:9090,cp2:9090
sudo systemctl enable --now opsmesh-agent

# 5. 查看状态
systemctl status opsmesh-controlplane
journalctl -u opsmesh-controlplane -f
```

systemd unit 已含 19 条安全加固指令（NoNewPrivileges / ProtectSystem / ProtectHome / PrivateTmp / ProtectKernelTunables / ProtectKernelModules / ProtectControlGroups / RestrictAddressFamilies / RestrictNamespaces / SystemCallFilter 等），无需额外配置。

### JWT 密钥运维指引

用户中心 JWT 签发密钥，三处接线保持一致：**config（Go）+ Helm secret/deployment + systemd/compose 环境变量**，改一处需同步三处。

1. **变量名**：env `OPSMESH_JWT_SECRET`；Helm 对应 `controlplane.jwtSecret`。docker-compose 走环境变量插值 `${OPSMESH_JWT_SECRET:-}`；systemd 则直接填 `OPSMESH_JWT_SECRET=<openssl rand -hex 32>`（见 `deploy/systemd/opsmesh-controlplane.env`，无 `${...}` 展开语法）。
2. **语义**：HS256 签发密钥，**多副本集群必须一致**，否则一个副本签发的 token 其它副本校验失败（用户间歇 401）。
3. **生成**：生产建议 `openssl rand -hex 32`（32 字节 = 64 个 hex 字符，满足生产 ≥32 字节强校验）。
4. **生产强制**：`--production=true` 下此值为空或 <32 字节会直接 **fail-fast**（服务拒绝启动）；本地开发可留空（自动随机兜底，重启丢会话）。
5. **Helm 注入方式**：
   ```bash
   helm install opsmesh ./deploy/helm/opsmesh -n opsmesh --create-namespace \
     -f deploy/helm/opsmesh/values-production.yaml \
     --set controlplane.jwtSecret=$(openssl rand -hex 32)
   ```
   密钥存于 Secret 键 `jwt-secret`：首次安装为空值时会**随机生成并固化**；`helm upgrade` **不轮换**（通过 `lookup` 复用已存在 Secret）。因此 upgrade 后 token 不会意外失效——若要换密钥，请先 `kubectl delete secret`（对应 Secret）再 upgrade，让新值进模板。

> `Makefile` 与 `start.bat` 不再内置 demo 默认密钥（安全修复）：未设置 `OPSMESH_JWT_SECRET` 时二进制自动生成随机密钥（重启后旧 token 失效），生产务必显式注入强随机密钥。

---

## IAM 与租户隔离

OpsMesh 提供**两条身份路径**，按部署形态二选一（两者可同时启用，网关头优先）：

**路径 A · 内置用户中心（默认主线）**：OpsMesh 自带注册/登录/RBAC（用户/角色/权限三表），登录签发 JWT 双 Token（at/rt HttpOnly Cookie），企业版前端开箱即用。适合无独立网关的中小规模私有化部署。

**路径 B · 网关注入身份头（企业集成）**：已有 APISIX/Envoy 统一认证网关时，由网关校验后注入身份头，OpsMesh 内核直接消费：

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

**令牌闭环**例外：agent 首次注册时携带一次性 install token（HMAC-SHA256 签名），
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

## 自动纳管流程

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

> **M 编号说明**：下表中的 M 编号（M2 CMDB、M3 服务部署、M5 作业编排、M6 日志检索、M7 告警等）为**功能演进项**编号，与 `docs/feature-design.md` 的 **F1–F18** 功能模块编号、`docs/product-roadmap.md` 第 8 章的 **M1–M4** 里程碑编号相互独立。

### 仪表盘

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/` | B/S 仪表盘（设备/任务双表 + 详情抽屉 + 5s 轮询） |
| GET | `/api/v1/devices` | 设备清单（按网段分组） |
| GET | `/api/v1/devices/{id}` | 设备详情（含任务结果） |
| DELETE | `/api/v1/devices/{id}` | 退役/下线设备 |
| POST | `/api/v1/devices/{id}/provision` | **纳管**：签发 install token，返回 bootstrap |
| POST | `/api/v1/provision/auto` | 自动纳管：按网段批量签发 install token |
| GET | `/api/v1/agents` | agent 清单 |
| GET | `/api/v1/me` | 当前身份信息（解析 X-Tenant-ID / X-User-Id / X-User-Roles） |
| POST | `/api/v1/auth/register` | 用户中心：注册（受 --public-register / --allow-public-register 控制） |
| POST | `/api/v1/auth/login` | 用户中心：登录（签发 AT/RT，防爆破） |
| GET | `/api/v1/auth/me` | 用户中心：当前登录用户信息 |
| POST | `/api/v1/auth/logout` | 用户中心：登出（吊销 RT） |
| POST | `/api/v1/auth/refresh` | 用户中心：刷新 AT（凭 RT） |
| GET/POST | `/api/v1/users` | RBAC CRUD：用户列表 / 创建用户 |
| GET/POST | `/api/v1/roles` | RBAC CRUD：角色列表 / 创建角色 |
| GET/POST | `/api/v1/permissions` | RBAC CRUD：权限列表 / 创建权限 |
| GET/POST | `/api/v1/os-templates` | OS 优化模板：列表 / 创建模板 |
| GET/POST | `/api/v1/middleware-templates` | 中间件部署：模板列表 / 创建模板 |
| GET/POST | `/api/v1/middleware-instances` | 中间件部署：实例列表 / 创建实例 |
| GET/POST | `/api/v1/k8s/clusters` | K8s 集群管理：集群列表 / 接入集群 |
| GET | `/api/v1/events/stream` | SSE 实时推送：任务状态/告警/设备上下线事件流 |
| GET/POST | `/api/v1/alert-rules` | 告警规则：列表 / 创建规则 |
| GET | `/api/v1/tasks` | 任务列表（支持 `?status=pending` 过滤） |
| POST | `/api/v1/tasks` | 下发任务（单条，租户隔离 + 审计） |
| POST | `/api/v1/tasks/batch` | 批量下发（逐台查找 agent + 租户校验） |
| POST | `/api/v1/tasks/{id}/cancel` | 取消任务（pending 拦截 / running 强杀） |
| GET | `/api/v1/tasks/{id}/result` | 查询单条结果 |
| GET | `/api/v1/alerts` | 告警列表（M7） |
| GET | `/api/v1/audits` | 审计事件（可查：?tenant=&action=&from=&to=&limit=） |
| \* | `/api/v1/cmdb/*` | CMDB 配置项：模型 / 实例 CRUD + 采集（M2） |
| \* | `/api/v1/workflows/*` | 作业编排：DAG 创建 / 触发 / 状态查询（M5） |
| \* | `/api/v1/deploys/*` | 服务部署：计划 / fan-out 执行 / Reconcile / Rollback（M3） |
| GET | `/api/v1/logs` | 日志检索：双后端(Memory/SQL) + offset 分页（M6） |
| GET | `/healthz` | 健康检查 |
| GET | `/readyz` | 就绪探针（依赖 store/redis 就绪后才返回 200，K8s readinessProbe 用） |
| GET | `/metrics` | Prometheus 文本指标 |
| GET | `/install.sh` | agent bootstrap 脚本（curl ... \| sh -s -- --token=<tok>） |
| GET | `/bin/opsmesh-agent` | agent 二进制下载（纳管 bootstrap 拉取） |

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

OpsMesh 启动参数共 **116 个 flag**，全部支持"命令行 flag 优先、环境变量兜底"语义（同名环境变量前缀 `OPSMESH_`）。下表按功能分组列出全部 flag。完整定义见 `internal/config/config.go`。

### 基础配置

运行模式、监听端口、副本数与演示开关。

| Flag | 类型 | 默认值 | 环境变量 | 说明 |
|------|------|--------|----------|------|
| `--mode` | string | controlplane | OPSMESH_MODE | 运行模式: controlplane \| agent |
| `--addr` | string | 127.0.0.1 | OPSMESH_ADDR | agent 自身地址（占位，供控制面感知） |
| `--control-addr` | string | http://127.0.0.1:8080 | OPSMESH_CONTROL_ADDR | 控制面 HTTP 地址（agent 注册/心跳/拉任务用） |
| `--control-addrs` | string | "" | OPSMESH_CONTROL_ADDRS | 多控制面地址（逗号分隔，如 cp1:9090,cp2:9090）；agent 依次重试实现 HA failover；空则回退 --control-addr |
| `--segment` | string | default | OPSMESH_SEGMENT | agent 所属网段（分桶键） |
| `--http-port` | int | 8080 | OPSMESH_HTTP_PORT | 控制面 HTTP(B/S) 端口 |
| `--grpc-port` | int | 9090 | OPSMESH_GRPC_PORT | gRPC 端口（注册/心跳/拉任务/上报/取消） |
| `--metrics-port` | int | 9091 | OPSMESH_METRICS_PORT | Prometheus metrics 端口 |
| `--replicas` | int | 1 | OPSMESH_REPLICAS | 控制面副本数（A3 HA）；>1 须用 --store=mysql，否则 memory 多副本数据分裂 |
| `--production` | bool | false | OPSMESH_PRODUCTION | 生产模式：默认开启 require-auth，并对 store=memory 强告警 |
| `--demo` | bool | false | OPSMESH_DEMO | 演示模式：每个 agent 注册预置 uname -a 示例任务（生产务必关闭） |

### 存储配置

控制面持久化后端与多租户 schema 隔离。

| Flag | 类型 | 默认值 | 环境变量 | 说明 |
|------|------|--------|----------|------|
| `--store` | string | memory | OPSMESH_STORE | 持久化后端: memory（默认，零依赖） \| mysql（数据本地化） |
| `--mysql-dsn` | string | "" | OPSMESH_MYSQL_DSN | MySQL DSN（--store=mysql 时生效），如 user:pass@tcp(mysql:3306)/ops_device |
| `--redis-addr` | string | "" | OPSMESH_REDIS_ADDR | Redis 地址（--store=mysql 时作 agent/device 状态缓存），如 redis:6379 |
| `--multi-schema` | bool | false | OPSMESH_MULTI_SCHEMA | 开启多租户 schema 隔离：每租户路由到独立 MySQL schema；仅 --store=mysql 时生效 |
| `--schema-prefix` | string | opsmesh_tenant_ | OPSMESH_SCHEMA_PREFIX | schema 名前缀；最终 schema 名 = 前缀 + tenantID |
| `--data-dir` | string | ./data | OPSMESH_DATA_DIR | agent 身份文件目录；agent.id 落盘于此，重启沿用 |

### 安全配置

鉴权、TLS/mTLS、JWT、注册策略、agent 侧纵深防御。

| Flag | 类型 | 默认值 | 环境变量 | 说明 |
|------|------|--------|----------|------|
| `--require-auth` | bool | false | OPSMESH_REQUIRE_AUTH | 要求网关注入 X-Tenant-ID，缺失则拒绝（生产 hardening）；--production 下默认 true |
| `--tls-cert` | string | "" | OPSMESH_TLS_CERT | gRPC TLS 服务端证书路径（空=关闭） |
| `--tls-key` | string | "" | OPSMESH_TLS_KEY | gRPC TLS 私钥路径 |
| `--client-ca` | string | "" | OPSMESH_CLIENT_CA | 服务端要求客户端 CA（mTLS）/ 客户端校验服务端 CA |
| `--jwt-public-key` | string | "" | OPSMESH_JWT_PUBLIC_KEY | JWT 验签公钥 PEM 文件路径（RS256）；空=关闭 JWT 验签回退头注入模式 |
| `--jwt-issuer` | string | "" | OPSMESH_JWT_ISSUER | 预期 JWT issuer（iss claim）；非空时校验 iss 必须匹配 |
| `--jwt-secret` | string | "" | OPSMESH_JWT_SECRET | 用户中心 JWT 签发密钥（HS256）；空=随机生成（重启后旧 token 失效）；多副本须一致 |
| `--public-register` | bool | true | OPSMESH_PUBLIC_REGISTER | 允许公开注册：true=开放 /api/v1/auth/register 但新用户须管理员审批；false=关闭公开注册（仅管理员可创建用户） |
| `--allow-public-register` | bool | false | OPSMESH_ALLOW_PUBLIC_REGISTER | 允许公开注册免审批：true=注册即激活并立即签发 token（仅演示/内网受信环境）；false=所有注册都走 pending 审批流程 |
| `--grpc-require-signature` | bool | false | OPSMESH_GRPC_REQUIRE_SIGNATURE | gRPC agent 身份绑定：强制要求 agent 在 PullTasks/ReportResult/PollCancels/Heartbeat 携带 HMAC 签名；demo 模式强制关闭；生产模式默认开启 |
| `--trust-proxy` | bool | false | OPSMESH_TRUST_PROXY | 信任反向代理：开启后 clientIP 取 X-Forwarded-For 首段（仅当有可信 LB/网关前置时启用）；默认 false=仅用 RemoteAddr 防 XFF 伪造绕过限流 |
| `--cookie-secure` | bool | false | OPSMESH_COOKIE_SECURE | Cookie Secure 标志：true=at/rt Cookie 仅经 HTTPS 传输（防中间人窃取）；默认 false（明文内网/开发需要）；生产模式默认 true |
| `--agent-shell-whitelist` | string | "" | OPSMESH_AGENT_SHELL_WHITELIST | 安全加固：agent shell 任务允许的命令前缀列表（逗号分隔，如 ls,cat,echo,ping,systemctl,docker,kubectl）；空=不限制 |
| `--agent-file-root-whitelist` | string | "" | OPSMESH_AGENT_FILE_ROOT_WHITELIST | 安全加固：agent 文件任务允许的根目录白名单（逗号分隔）；空=不限制根目录（仍拒绝 ../ 路径遍历与符号链接） |
| `--metrics-allow-cidr` | string | "" | OPSMESH_METRICS_ALLOW_CIDR | metrics(/metrics) 访问控制：逗号分隔 CIDR 白名单；空=不限制（生产建议内网监控网段，如 10.0.0.0/8） |
| `--encryption-key` | string | "" | OPSMESH_ENCRYPTION_KEY | kubeconfig AES-256-GCM 加密密钥（32 字节，hex/base64 均可）；空=关闭 kubeconfig 落盘加密（明文存储）；多副本须一致 |
| `--grpc-signature-key` | string | "" | OPSMESH_GRPC_SIGNATURE_KEY | gRPC agent 身份绑定预共享 HMAC 签名密钥；非空时 agent 在 PullTasks/ReportResult/PollCancels/Heartbeat 携带 HMAC-SHA256 签名，服务端验签防伪造；与 --grpc-require-signature 配合使用 |
| `--session-store` | string | memory | OPSMESH_SESSION_STORE | 会话状态后端：memory（默认，单进程内存，重启丢会话） \| redis（经 --redis-addr 持久化，多副本共享会话）；生产多副本建议 redis |
| `--device-fp-deadline` | duration | 0 | OPSMESH_DEVICE_FP_DEADLINE | DeviceFP 强制非空截止时间；>0 时设备指纹为空的纳管请求在该时长后强制拒绝（防 agent 裸注册绕过指纹采集）；0=不强制 |

### 网络配置

联邦 peer 通道硬化（mTLS + HMAC 签名）。

| Flag | 类型 | 默认值 | 环境变量 | 说明 |
|------|------|--------|----------|------|
| `--federation-peers` | string | "" | OPSMESH_FEDERATION_PEERS | 控制面联邦 peer 地址列表（逗号分隔，如 http://peer1:8080,http://peer2:8080）；非空时启用联邦 API（跨网段任务转发/联邦设备视图） |
| `--federation-secret` | string | "" | OPSMESH_FEDERATION_SECRET | 联邦共享 HMAC 密钥（所有 peer 须一致）；签名/验签转发的身份头，防跨不可信网段伪造租户身份；空=不签名（仅内网信任） |
| `--federation-tls-cert` | string | "" | OPSMESH_FEDERATION_TLS_CERT | 联邦 mTLS 服务端/客户端证书（独立于 --tls-cert）；空=明文联邦（仅内网） |
| `--federation-tls-key` | string | "" | OPSMESH_FEDERATION_TLS_KEY | 联邦 mTLS 私钥 |
| `--federation-ca` | string | "" | OPSMESH_FEDERATION_CA | 联邦 mTLS 对端 CA（校验证书链/要求客户端持证） |
| `--federation-port` | int | 0 | OPSMESH_FEDERATION_PORT | 联邦独立 mTLS 监听端口（>0 启用，强制对端持证）；0=不启用独立监听（复用主 HTTP） |

### 告警配置

Webhook 通道（generic/feishu/dingtalk/slack/企业微信）与邮件通道（SMTP）。

| Flag | 类型 | 默认值 | 环境变量 | 说明 |
|------|------|--------|----------|------|
| `--alert-webhook-url` | string | "" | OPSMESH_ALERT_WEBHOOK_URL | M7 告警 Webhook 推送 URL（POST JSON 告警到此地址）；空=不推送。：URL 含 slack.com 走 Slack Block Kit，含 qyapi.weixin.qq.com 走企业微信 markdown |
| `--alert-notifier-type` | string | generic | OPSMESH_ALERT_NOTIFIER_TYPE | M7 告警通知类型：generic(直接POST Alert JSON)/feishu(飞书卡片)/dingtalk(钉钉markdown)；：Webhook URL 域名可识别时自动覆盖此值 |
| `--alert-email-host` | string | "" | OPSMESH_ALERT_EMAIL_HOST | 告警邮件 SMTP 主机（如 smtp.example.com）；空=关闭邮件通道 |
| `--alert-email-port` | int | 25 | OPSMESH_ALERT_EMAIL_PORT | 告警邮件 SMTP 端口（默认 25） |
| `--alert-email-user` | string | "" | OPSMESH_ALERT_EMAIL_USER | 告警邮件 SMTP 用户名（空=匿名发送） |
| `--alert-email-pass` | string | "" | OPSMESH_ALERT_EMAIL_PASS | 告警邮件 SMTP 密码（推荐 env 注入） |
| `--alert-email-from` | string | "" | OPSMESH_ALERT_EMAIL_FROM | 告警邮件发件人地址（如 opsmesh@example.com） |
| `--alert-email-to` | string | "" | OPSMESH_ALERT_EMAIL_TO | 告警邮件收件人列表（逗号分隔） |

### 联邦配置

> 联邦通道硬化配置已合并至上方「网络配置」分组（`--federation-*` 共 6 项），此处不再重复列出。

### 日志配置

日志检索后端（memory/sql/loki/es）与外部后端连接参数。

| Flag | 类型 | 默认值 | 环境变量 | 说明 |
|------|------|--------|----------|------|
| `--log-backend` | string | memory | OPSMESH_LOG_BACKEND | 日志检索后端: memory \| sql \| loki \| es（loki/es 模式下日志由 agent 直接推送，控制面仅查询） |
| `--log-store` | string | memory | OPSMESH_LOG_STORE | 日志后端选择: memory \| sql \| loki \| es（--log-backend 别名，；显式设置时覆盖 --log-backend） |
| `--loki-endpoint` | string | "" | OPSMESH_LOKI_ENDPOINT | Loki API endpoint（如 http://loki:3100）；--log-backend=loki 时生效 |
| `--es-endpoint` | string | "" | OPSMESH_ES_ENDPOINT | Elasticsearch endpoint（如 http://es:9200）；--log-backend=es 时生效 |
| `--es-index` | string | opsmesh-logs | OPSMESH_ES_INDEX | Elasticsearch 索引名（--log-backend=es 时生效，默认 opsmesh-logs） |

### K8s / 调度配置

任务调度、HA 选主、agent 资源限额、事件总线。

| Flag | 类型 | 默认值 | 环境变量 | 说明 |
|------|------|--------|----------|------|
| `--task-timeout` | duration | 120s | OPSMESH_TASK_TIMEOUT | agent 单任务执行超时 |
| `--shutdown-timeout` | duration | 15s | OPSMESH_SHUTDOWN_TIMEOUT | SIGTERM 优雅退出窗口 |
| `--task-lease-sec` | int | 300 | OPSMESH_TASK_LEASE_SEC | 任务租约租期秒；超期未上报结果则复位重调度 |
| `--task-max-retries` | int | 3 | OPSMESH_TASK_MAX_RETRIES | 任务失败重试上限（F2）；超出置 failed（死信），需人工处置 |
| `--leader-ttl-sec` | int | 15 | OPSMESH_LEADER_TTL_SEC | 选主租约秒；本实例持有 leader 身份的时长，到期前需续租 |
| `--leader-tick-sec` | int | 5 | OPSMESH_LEADER_TICK_SEC | 选主续租周期秒；leaderLoop 续租频率（应小于 leader-ttl-sec） |
| `--archive-age-min` | int | 1440 | OPSMESH_ARCHIVE_AGE_MIN | F5 离线超龄自动归档阈值（分钟）；agent 最后心跳早于该时长的设备自动 retired（<=0 关闭） |
| `--worker-concurrency` | int | 4 | OPSMESH_WORKER_CONCURRENCY | agent 任务 worker 池并发度 |
| `--max-procs` | int | 256 | OPSMESH_MAX_PROCS | agent RLIMIT_NPROC 上限（fork 炸弹防护；0=不限制） |
| `--max-files` | int | 4096 | OPSMESH_MAX_FILES | agent RLIMIT_NOFILE 上限（文件描述符耗尽防护；0=不限制） |
| `--max-memory-mb` | int64 | 0 | OPSMESH_MAX_MEMORY_MB | agent RLIMIT_AS 上限 MB（0=不限制） |
| `--event-bus` | string | noop | OPSMESH_EVENT_BUS | 事件总线类型：noop \| log \| kafka |
| `--kafka-brokers` | string | "" | OPSMESH_KAFKA_BROKERS | Kafka brokers（--event-bus=kafka 时生效） |
| `--kafka-topic` | string | "" | OPSMESH_KAFKA_TOPIC | Kafka topic（--event-bus=kafka 时生效） |

### 纳管配置

网段发现、自动纳管闭环、SSH 推送。

| Flag | 类型 | 默认值 | 环境变量 | 说明 |
|------|------|--------|----------|------|
| `--discover` | bool | false | OPSMESH_DISCOVER | 开启真实网段发现；关闭时采用 agent 即设备的 MVP 降级纳管 |
| `--segment-cidr` | string | "" | OPSMESH_SEGMENT_CIDR | 待扫描网段（如 10.30.0.0/24）；开启 --discover 时生效 |
| `--auto-provision` | bool | false | OPSMESH_AUTO_PROVISION | 自动纳管：discover 扫描到存活主机后自动登记候选设备并（配置 --provision-ssh-key 时）推送 agent |
| `--install-token` | string | "" | OPSMESH_INSTALL_TOKEN | 自动纳管：agent 经 bootstrap 安装时携带的一次性 install token（空=无令牌闭环） |
| `--provision-secret` | string | "" | OPSMESH_PROVISION_SECRET | 自动纳管 install token 的 HMAC 签名密钥；空则本实例随机生成（多副本需一致） |
| `--advertise-addr` | string | "" | OPSMESH_ADVERTISE_ADDR | 自动纳管控制面对外 HTTP 地址（拼接 bootstrap 安装命令）；空则回退 127.0.0.1:<http-port>（仅本机开发） |
| `--provision-ssh-user` | string | root | OPSMESH_PROVISION_SSH_USER | B1 SSH 自动推送：SSH 用户 |
| `--provision-ssh-key` | string | "" | OPSMESH_PROVISION_SSH_KEY | B1 SSH 自动推送：SSH 私钥路径（空=关闭 SSH 推送，仅返回 bootstrap 文本） |
| `--provision-ssh-key-pass` | string | "" | OPSMESH_PROVISION_SSH_KEY_PASS | B1 SSH 自动推送：SSH 密钥密码（推荐 env 注入） |
| `--provision-ssh-known-hosts` | string | "" | OPSMESH_PROVISION_SSH_KNOWN_HOSTS | B1 SSH KnownHosts 文件路径（等保加固）；空=InsecureIgnoreHostKey（生产务必配置） |

> 共 **116 个 flag**，覆盖基础/存储/安全/网络/告警/日志/调度/纳管八大领域。所有 flag 均支持同名 `OPSMESH_*` 环境变量兜底，命令行显式设置优先级最高。

---

## 从源码构建

```bash
# 依赖：Go 1.26+
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

kafka-go 版本已随 Go 升级放开（当前 v0.4.48+），仓库使用 Go 1.26.0，上个兼容限制已失效。
运行 `go test -tags kafka` 前确保已 `go mod tidy`。

> **构建说明**：`go.mod` 直接 `require kafka-go`，默认 `go build` 即编译 kafka-go 包（无 build tag 排除）。
> `-tags kafka` 用于**启用事件总线 kafka 后端**（`--event-bus=kafka`），未加 tag 时事件总线仅可用 noop/log。

---

## 前端（企业版）构建与部署

OpsMesh 控制面内置的 B/S 仪表盘为 Go 模板渲染（`/`），适合轻量内网运维。面向企业级前端体验，仓库另提供独立 SPA 前端，**与控制面解耦、独立构建、独立部署**，由 Nginx/CDN 托管静态资源，反向代理 API 到控制面 8080 端口。

### 前端收敛策略

| 前端 | 状态 | 说明 |
|------|------|------|
| Vue3 企业版 (`web/enterprise/`) | ✅ 主线 | 唯一维护的前端，所有新功能在此开发 |

> 原生 JS 个人版（`internal/controlplane/web/`）已于 v0.4.0 起收敛为极简引导页；GET / 会重定向到 `/enterprise/`。业务功能已全部迁移到 Vue3 企业版前端。

| 项 | 说明 |
|---|---|
| 源码路径 | `web/enterprise/` |
| 技术栈 | Vue 3 + Vite + Pinia + Vue Router |
| 构建命令 | `cd web/enterprise && npm ci && npm run build` |
| 产物目录 | `web/enterprise/dist/`（静态资源，含 index.html / assets/） |
| 部署方式 | 独立部署（Nginx 静态托管 / CDN 分发），API 反代到控制面 `:8080` |
| 开发模式 | `cd web/enterprise && npm install && npm run dev`（Vite dev server，端口 5174，自动代理 `/api` 到 `localhost:8080`） |

### 构建

```bash
# 依赖：Node.js 18+（推荐 20 LTS）
cd web/enterprise
npm ci              # 严格按 package-lock.json 安装依赖
npm run build       # 产出 dist/（index.html + assets/）
```

### 部署（Nginx 反代 API 到控制面 8080）

```nginx
server {
    listen 80;
    root /path/to/web/enterprise/dist;
    location / { try_files $uri $uri/ /index.html; }
    location /api/v1/ { proxy_pass http://controlplane:8080; }
}
```

> **base 前缀**：`vite.config.js` 默认 `base: '/enterprise/'`，构建产物以 `/enterprise/` 前缀分发。若改为根路径独立站点（如上 Nginx 示例 `location /`），需将 `base` 改为 `'/'` 后重新构建；若由控制面统一托管于 `/enterprise/` 子路径，则保留默认 `base` 并将 `root` 指向 `dist`、`location /enterprise/` 套 `try_files`。

> **鉴权头**：企业版前端独立部署时，`X-Tenant-ID` / `X-User-Id` / `X-User-Roles` 身份头由前置网关（APISIX/Envoy）或 Nginx 注入，控制面 `--require-auth` 开启后缺失则 401。详见 [IAM 与租户隔离](#iam-与租户隔离)。

完整部署步骤（含 Cookie 跨域、HTTPS、CDN 等注意项）见 [部署指南 - 企业版前端部署](./docs/deployment-guide.md#企业版前端部署)。

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

## 生产安全加固

面向"上线即崩 / 越权 / 爆破 / 伪造"的企业级风险，本仓库已落地以下加固（实现位于 `internal/controlplane`、`internal/tlsutil`、`internal/store`）：

| 编号 | 加固项 | 实现要点 |
|---|---|---|
| | RBAC 持久化建表 + 种子 | `SQLStore.initSchema` 新增 `users` / `roles` / `permissions` 三表；`seedRBAC` 幂等写入 24 条默认权限 + `admin` 角色 + 默认 `admin` 用户。多副本共享同一 MySQL，HA 部署身份一致（修复 mysql 后端启动即 panic） |
| | HTTP / gRPC 兜底恢复 | `recoveryMiddleware`（HTTP 兜底盘：返回 500 + JSON + traceId）+ `grpcRecoveryInterceptor`（unary 拦截器：recover 后返回 `codes.Internal`）。单处 handler panic 不再拖垮整个控制面 |
| | 请求体限流 | 统一 `decodeJSONBody` 经 `http.MaxBytesReader` 限 1 MiB，防止超大 body 占满内存 |
| | 登录防爆破 / 失败锁账号 | `loginGuard`：令牌桶限流（每 IP 10 突发 / 每 3s 补 1）+ 连续失败 5 次锁 15min；用户名不存在场景同样计入限流，避免账号枚举 |
| | metrics 访问控制 + bootstrap 审计 | `--metrics-allow-cidr` CIDR 白名单拒绝非监控网段拉 `/metrics`（403 + 审计）；`/install.sh`、`/bin/opsmesh-agent` 保留开放但记录来源 IP 审计 |
| | 联邦 mTLS + 转发签名验签 | 见下「联邦（跨网段任务转发）」 |

### 联邦（跨网段任务转发）

企业多终端环境常按网段割裂为多个控制面。OpsMesh 支持**每段一套控制面 + 控制面联邦**，将任务跨段转发到下一段的 agent 执行；转发通道硬化为 mTLS + HMAC 签名，防伪造 / 防重放：

- **入站**：启用 `--federation-port>0` 后，控制面起**独立 mTLS 监听**（仅暴露 `POST /api/v1/tasks` 与 `GET /api/v1/devices`），强制 `RequireAndVerifyClientCert`，物理隔离联邦流量与公网 B/S。
- **验签**：`verifyFederationRequest` 仅对携带 `X-Federation-Forwarded: 1` 的请求验签；签名 = HMAC-SHA256（method + path + 时间戳 + 身份头），时间戳偏差窗 ±5min 防重放；密钥缺失 / 签名不符 / 超窗 → 401 拒绝。
- **出站**：`FederationManager.ForwardTask` / `fetchPeerDevices` 经 `signFederationRequest` 主动签名，并使用 `--federation-tls-cert/key/ca` 构造的客户端 TLS 配置（明文联邦仅限内网可信场景）。
- **配置**：所有 peer 须共享同一 `--federation-secret`；启用独立监听须同时配置联邦 TLS 证书，否则 `Validate()` 直接报错。

```bash
# 控制面 A（段 1）：启用联邦独立 mTLS 监听
./opsmesh --mode=controlplane --store=mysql \
  --federation-port=9092 \
  --federation-secret=$(openssl rand -hex 32) \
  --federation-tls-cert=/etc/opsmesh/fed.crt \
  --federation-tls-key=/etc/opsmesh/fed.key \
  --federation-ca=/etc/opsmesh/fed-ca.crt

# 控制面 B（段 2）：将 A 设为联邦 peer（跨段任务转发到 A 段 agent 执行）
./opsmesh --mode=controlplane --store=mysql \
  --federation-peers=https://<a-host>:9092 \
  --federation-secret=<与 A 完全一致> \
  --federation-tls-cert=/etc/opsmesh/fed.crt \
  --federation-tls-key=/etc/opsmesh/fed.key \
  --federation-ca=/etc/opsmesh/fed-ca.crt
```

---

## 开发指引

```
cmd/opsmesh/              ← 入口 main：解析 --mode 分派 controlplane / agent
internal/                 ← 36 个包，按 8 个领域分组（详见上文"internal 包职责"）
├── agent/                ← agent 运行时（注册/心跳/worker 池/执行器 + log_collect 日志采集 v2.0）
├── alertengine/          ← 告警规则引擎（多条件 + Z-Score/EWMA 异常检测 + 静默 + 抑制 + 聚合）
├── approval/             ← 审批引擎（审批流 + 请求 + approve/reject）
├── authctx/              ← HTTP 头 / gRPC metadata 身份提取
├── automation/           ← 自动化闭环引擎（规则条件→动作 + 多类型触发器）
├── circuitbreaker/       ← 通用熔断器（Closed→Open→HalfOpen 状态机）
├── cmdb/                 ← 配置库 CMDB（M2）：模型 + 实例 CRUD + SQL + 采集 + 关系图谱
├── compliance/           ← 安全合规检查引擎（CIS Benchmark 基线 + 扫描编排）
├── config/               ← 统一配置（116 个 flag + env 兜底）
├── controlplane/         ← 控制面（HTTP 路由/gRPC server/Registry/dashboard + 14 个功能域 handler）
├── cron/                 ← 5 字段 cron 表达式匹配
├── dag/                  ← DAG 引擎（M5 作业编排）：拓扑排序 + 环检测 + 依赖就绪判定
├── deploy/               ← 服务部署（M3）：计划 + fan-out + Reconcile + Rollback + 灰度 + 联邦发布
├── discover/             ← 设备发现：TCP 存活扫描网段（控制面→网段找设备）
├── discovery/            ← 控制面服务发现 + 负载均衡（agent→控制面 failover/round-robin）
├── domain/               ← 纯领域模型（DDD）+ 防腐层 mapper
├── events/               ← 可插拔事件总线（noop/log/kafka）
├── extension/            ← API 网关引擎（路由规则 + 令牌桶限流 + 网关统计）
├── grpcx/                ← gRPC ServiceDesc / JSON codec / 消息类型
├── helm/                 ← Helm 应用商店（仓库/Chart/Release + 24 个预置应用）
├── k8s/                  ← K8s 多集群管理（client-go + ClusterManager + 资源 CRUD）
├── logstore/             ← 日志检索（M6）：双后端(Memory/SQL) + Loki/ES + 倒排索引
├── logx/                 ← slog 封装 + traceID（优先 OTel span）
├── metrics/              ← 零依赖 Prometheus 文本指标
├── network/              ← 网络管理引擎（设备模型 + 监控指标 + 拓扑 + 子网发现）
├── notify/               ← 通知渠道：Webhook / 飞书 / 钉钉 / 企业微信 / Slack / 邮件
├── orchestration/        ← 作业编排（M5）：DAG 调度 + 子工作流 + 条件分支 + 节点级超时重试
├── otelx/                ← OTel 集成（HTTP/gRPC 自动埋点 + OTLP 导出）
├── platform/             ← 平台化业务引擎（租户/API Key/插件市场/计费）
├── plugin/               ← 插件框架（Plugin/Hook/HookHandler/Manager）
├── proto/                ← 共享数据类型（AgentInfo/DeviceInfo/Task/…）
├── provision/            ← 自动纳管闭环（install token + SSH 推送 + 候选设备状态机）
├── secrets/              ← 密钥管理（env/file/Vault/KMS 多 provider）
├── store/                ← Store 接口 + MemoryStore + SQLStore（35 个领域子接口）
├── tlsutil/              ← gRPC TLS / mTLS 工具 + 证书热重载
└── version/              ← 构建版本注入
operator/                 ← K8s Operator 子模块（独立 go.mod，controller-runtime OpsMeshInstance CRD）
services/                 ← 微服务化拆分（18 个独立服务，见下表；与主模块双轨并存，收敛计划见 docs/tech-debt.md TD-60）
proto/                    ← protobuf API 定义（buf 管理，与 internal/grpcx 双轨）
web/enterprise/           ← Vue3 企业版前端（独立构建部署）
deploy/                   ← 部署资产：helm/ + systemd/ + docker-compose.yaml + Dockerfile*
docs/                     ← 24 篇设计文档（产品/架构/数据库/接口/安全/UI/模块/功能/测试/运维/AI/多系统/部署场景）
```

### services/ 微服务目录（18 个）

> 2026-08-29 起仓库新增 `services/` 微服务化拆分（详见 `docs/architecture/adr-001-microservice-strategy.md`）。
> 当前状态：**与主模块（`internal/` + `cmd/opsmesh`）双轨并存**——控制面单体仍是默认运行形态，
> 微服务为渐进式拆分产物（各服务已具备独立 main + MySQL schema，但多数默认接内存 store，
> 生产接线与双轨收敛计划见 `docs/tech-debt.md` TD-60/TD-65）。各服务职责：

| 服务 | 职责 |
|---|---|
| `auth-svc` | 认证/用户/会话 |
| `device-svc` | 设备纳管/心跳/指标 |
| `task-svc` | 任务执行/调度/批量 |
| `alert-svc` | 告警规则/静默/抑制 |
| `log-svc` | 日志检索（gRPC + 独立 health） |
| `config-svc` | 配置中心/热推送 |
| `deploy-svc` | 部署计划/灰度/回滚 |
| `workflow-svc` | DAG 工作流 |
| `aio-svc` | AIOps 智能引擎（异常检测/根因/预测/降噪/GPU 异常） |
| `gpu-svc` | GPU 资源管理/AI 工作负载调度 |
| `incident-svc` | 事件管理/升级策略 |
| `plugin-svc` | 插件服务 |
| `portal-svc` | 门户聚合 |
| `runbook-svc` | 运维手册 |
| `autoscaler-svc` | 自动扩缩容 |
| `bot-svc` | ChatOps 机器人 |
| `grafana-bridge` | Grafana 数据源桥接 |
| `tf-provider` | Terraform provider |

> **端口注意**：各服务端口经 `<NAME>_SVC_HTTP_PORT` / `<NAME>_SVC_GRPC_PORT` 环境变量配置，
> 但默认值存在重叠（多个服务默认同为 HTTP 8080/8081、gRPC 50051/50052）——同机并行运行
> 多个服务时**必须显式分配不同端口**，否则监听冲突启动失败。

> internal 包职责详细说明见上文 [internal 包职责](#internal-包职责36-个) 章节，完整设计见 `docs/module-design.md`。

---

## License

内部项目，私有部署。管控通道为自研 gRPC（direct + proxy）；原蓝鲸 GSE 社区版底座已移出 MVP，降格为可选增强（见 `DELIVERY.md`）。
