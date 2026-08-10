# OpsMesh 产品方向与演进路线图

> 版本：v0.1（规划稿）  ·  编制日期：2026-08-01  ·  适用基线：MVP（ADR-001 Option A，2026-07-28 交付）
>
> 本文档基于 `README.md` 与 `DELIVERY.md` 描述的 MVP 现状，给出 OpsMesh 从 MVP 走向生产可用与规模化的演进方向。所有"计划/目标"措辞均为规划意图，不代表已实现能力；已实现能力以 `README.md` 功能矩阵为准。

---

## 1. 现状定位与问题总结

### 1.1 产品定位

OpsMesh 是**私有化单中心 B/S 自动化部署与运维平台**，核心差异在于：服务部署到某网段后，整段网络打通的设备自动纳管，各设备可并行执行各自的自动化任务（shell/服务管理/文件分发），具备失败重试、死信、取消、定时周期、告警等完整任务生命周期。管控通道采用自研 gRPC（direct + proxy，JSON codec），原蓝鲸 GSE 社区版底座已移出 MVP、降格为可选增强。

### 1.2 MVP 已落地能力概览

依据 `DELIVERY.md`，MVP 已交付六大运维模块与配套基线能力：

| 维度 | 已落地能力 |
|---|---|
| 运维中枢 | 任务下发/批量/生命周期/租约回收/取消/定时调度/失败重试+死信 |
| CMDB | Phase1 模型 + CRUD + SQL + 采集 |
| 服务部署 | 计划 + fan-out 执行 + Reconcile + Rollback |
| 日志检索 | logstore 双后端（Memory/SQL）+ 查询 API + offset 分页 |
| 监控告警 | 规则引擎 + alert（Status/Ack/Silence）+ Webhook/飞书/钉钉 |
| 作业编排 | DAG 引擎 + store 阻塞→释放链路 + 画布 |
| 平台基线 | 多副本 HA leader 选举、agent 多控制面 failover、租户行级隔离、gRPC TLS/mTLS、审计 100% 留痕、Prometheus 指标、多阶段 Dockerfile + CI |

代码规模实测：19 个 Go 包、36 个源码文件（约 6,848 行）、21 个测试文件（约 2,372 行，占比 25.7%）、外部依赖 5 个（mysql/redis/kafka/grpc/crypto），零 protobuf 代码生成。

### 1.3 现存问题总结

MVP 功能完成度高，但面向生产规模化仍存在以下结构性短板（综合代码结构与交付说明归纳）：

| 问题类别 | 具体表现 | 风险等级 |
|---|---|---|
| 文档脱节 | `README.md` 与 `DELIVERY.md` 曾对 Helm Chart 描述为"规划中"，与仓库实际（Helm Chart 已提供）不符；Argo CD GitOps 仍属规划中 | 低（已部分纠正：2026-08-02 修订 README/DELIVERY 消除 Helm 矛盾） |
| 供应链风险 | `kafka-go` 须钉 `v0.4.48`（最后兼容 Go 1.22 版本），`v0.4.49+` 要求 Go ≥ 1.23，升级窗口受限 | 中 |
| 纵深防御缺失 | agent shell 命令无白名单、file 路径无白名单、bootstrap token 编码未防 shell 注入、webhook/autoProvision URL 无 SSRF 校验 | 高 |
| 测试覆盖不足 | `sql.go` 约 57KB 仅 1 个测试；12 个 HTTP handler 无测试；8 个后台 loop 无测试；前端零测试 | 高 |
| 前端工程化弱 | 仪表盘为原生 JS 单文件约 986 行，无模块化/类型/构建/Lint | 中 |
| 架构内聚不足 | Store 巨型接口（40+ 方法）违反接口隔离原则（ISP）；`domain` 包仅有数据结构无业务行为；Registry 仅一对一转发 store；agent 每次 RPC 重新 Dial | 中 |
| 交付物缺口 | `docker-compose.yaml` 与 Helm Chart（`deploy/helm/opsmesh/`）现已提供；`goreleaser` 配置、systemd unit、Argo CD GitOps 仓库仍为规划/待完善 | 低 |

---

## 2. 架构演进方向

### 2.1 Store 接口拆分

#### 2.1.1 现状与问题

当前 `internal/store` 暴露单一巨型 `Store` 接口，聚合了设备、任务、告警、审计、令牌、leader 租约等 40+ 方法。任何依赖 Store 的模块都被迫依赖整个接口，违反接口隔离原则（ISP），导致：

- 模块边界模糊，难以单元测试（mock 需实现全部方法）
- 单一职责被打破，store 实现文件膨胀（`sql.go` 约 57KB）
- 替换或装饰某一领域存储需改动全局

#### 2.1.2 演进目标

按领域拆分为小接口，各模块按需依赖：

| 拆分接口 | 职责 | 主要消费方 |
|---|---|---|
| `DeviceStore` | 设备 CRUD、纳管状态翻转、退役归档 | controlplane 设备路由、discover、archiveLoop |
| `TaskStore` | 任务 CRUD、ClaimTask、结果回写、死信、取消 | controlplane 任务路由、agent 上报、reclaimLoop |
| `AlertStore` | 告警 CRUD、Ack、Silence | controlplane 告警路由、notifyLoop |
| `AuditStore` | 审计事件写入与查询 | authctx、所有写操作切面 |
| `TokenStore` | install token 签发与消费 | provision 路由、agent 注册 |
| `LeaderStore` | leader 租约续租与查询 | leaderLoop |
| `ScheduleStore` | 定时模板与派生实例 | scheduleLoop |
| `WorkflowStore` | DAG 作业编排状态 | workflows 路由 |
| `DeployStore` | 服务部署计划与执行记录 | deploys 路由、reconcileLoop |
| `LogStore` | 日志检索双后端 | logs 路由 |
| `CMDBStore` | 配置项模型与实例 | cmdb 路由、采集 |

实现层可由单一 `SQLStore` 同时满足多个小接口（结构体方法分组），消费方仅声明其所需的最小接口，便于 mock 与替换。

### 2.2 DDD 实质化

#### 2.2.1 现状与问题

当前 `internal/domain` 包仅包含纯数据结构与防腐层 mapper，业务逻辑散落在 controlplane handler 与 store 实现中。任务状态机、租户校验、告警规则等核心业务行为未下沉到领域实体，导致：

- 业务规则在多处重复实现，易不一致
- 领域模型退化为 DTO，丧失表达力
- 跨模块复用业务逻辑困难

#### 2.2.2 演进目标

将业务行为下沉到 domain 实体，演进为富领域模型：

| 领域实体 | 计划下沉的业务行为 |
|---|---|
| `Task` | 状态机迁移（pending→running→done/failed/cancelled）、重试计数与死信判定、取消拦截逻辑、租约超时判定 |
| `Device` | 纳管状态翻转（discovered→provisioning→onboarded）、退役资格判定（离线时长/超龄） |
| `Tenant` | 租户校验、越权判定、资源归属断言 |
| `Alert` | 规则匹配、严重级别判定、Ack/Silence 状态机 |
| `Schedule` | cron 触发判定、派生实例幂等防重复 |
| `Workflow` | DAG 节点就绪判定、子工作流展开、并行/串行/条件分支 |

防腐层 mapper 保留在 domain 包边界，负责与 proto/store 结构互转；controlplane handler 退化为薄编排层，仅做参数解析与领域方法调用。

### 2.3 gRPC 标准化

#### 2.3.1 现状与问题

当前 `internal/grpcx` 手写 `ServiceDesc` + JSON codec，消息类型为手写结构体。零 protobuf 代码生成虽降低了 MVP 依赖复杂度，但规模化后面临：

- 无 schema 演进与向后兼容保障
- 无反射能力，跨语言客户端需手写协议
- 字段变更无编译期校验，易出运行时错误

#### 2.3.2 演进目标

引入 protobuf + buf 工具链，分阶段迁移：

| 阶段 | 目标 |
|---|---|
| Phase 1 | 引入 `buf.yaml` + `buf.gen.yaml`，定义 `.proto` 描述现有 ServiceDesc，生成 Go stub 与现有手写实现并存 |
| Phase 2 | agent 与控制面切换至生成 stub，JSON codec 替换为 protobuf codec，保留兼容期 |
| Phase 3 | 启用 buf breaking 检查守护 schema 演进，提供反射服务与跨语言客户端 SDK |

迁移期间保持向后兼容，避免一次性破坏现有 agent 部署。

### 2.4 连接复用

#### 2.4.1 现状与问题

agent 侧 `grpcclient` 在每次 RPC 调用时重新 `Dial` 控制面，引入连接建立开销与 fd 抖动，在心跳/拉任务/上报高频路径上尤为明显。

#### 2.4.2 演进目标

改为长连接复用：进程启动时建立一次 `grpc.ClientConn`（带 keepalive 与多控制面 failover 轮询），所有 RPC 复用该连接。配合 `--control-addrs` 多地址按序重连，连接断开后自动重连而非每次新建。

### 2.5 Registry 去除或强化

#### 2.5.1 现状与问题

当前 `Registry` 仅对 `Store` 做一对一薄转发，未引入缓存、聚合或边界隔离价值，徒增一层间接。

#### 2.5.2 演进目标（二选一）

| 方案 | 说明 | 适用场景 |
|---|---|---|
| 方案 A：去除 Registry | 消费方直接依赖拆分后的小 Store 接口，减少无价值间接层 | 短期简化，优先推荐 |
| 方案 B：强化 Registry | 升级为聚合边界，承担跨领域事务编排、读路径缓存、写路径事件派发 | 后期引入 CQRS/事件溯源时 |

建议先采用方案 A 去除，待 CQRS 或事件溯源需求明确后再按需引入强化版 Registry。

---

## 3. 前端演进方向

### 3.1 现状与收敛策略

仪表盘原有原生 JS 单文件约 986 行，承载设备/任务双表、详情抽屉、5s 轮询、纳管操作等全部前端逻辑，无模块化、无类型、无构建、无 Lint。该版本零依赖、单文件即用，但规模化后维护成本陡增。

**前端收敛策略**（与 `README.md` "前端收敛策略" 子节一致）：

| 前端 | 状态 | 说明 |
|---|---|---|
| Vue3 企业版 (`web/enterprise/`) | ✅ 主线 | 唯一维护的前端，所有新功能优先在此开发 |
| 原生 JS 个人版 (`internal/controlplane/web/`) | ⚠️ Deprecated | 自 v0.2.0 起进入弃用期，计划在 v0.4.0 移除。期间仅修 P0 bug，不新增功能 |

新部署请直接使用 Vue3 企业版前端；现有个人版用户参考 `web/enterprise/` 功能对照表迁移。

### 3.2 演进路径（Vue3 企业版主线）

Vue3 企业版为唯一主线，按以下阶段渐进增强；原生 JS 个人版不再纳入演进路径，仅维持 P0 修复至 v0.4.0 移除（详见第 9 章收敛与弃用策略）。

| 阶段 | 目标 |
|---|---|
| Phase 1 | 组件化拆分与设计系统固化（见 3.3），建立可复用组件库基线 |
| Phase 2 | 实时推送替代轮询（见 3.4），SSE 优先 |
| Phase 3 | 类型与测试基线：TypeScript 全覆盖 + Vitest/jsdom，CI 覆盖率门禁 ≥ 60% |

### 3.3 设计系统

固化当前浅色靛蓝主题为设计 token（颜色、间距、圆角、阴影、字号），提取可复用组件库（表格、抽屉、徽章、表单、按钮）。设计 token 以 JSON/CSS 变量形式管理，作为 Vue3 企业版前端唯一的视觉契约。

### 3.4 实时推送

当前 5s 轮询存在延迟与无效请求。演进为 SSE（Server-Sent Events）或 WebSocket，复用 gRPC 流式能力：控制面将任务状态变更、告警产出、设备上下线等事件推送到前端，替代轮询。SSE 优先（单向推送、HTTP 兼容、实现简单），双向交互场景再用 WebSocket。

---

## 4. 测试补全计划

### 4.1 现状

测试占比 25.7%，但分布不均：`sql.go` 约 57KB 仅 1 个测试；12 个 HTTP handler 无测试；8 个后台 loop 无测试；前端零测试。CI 已配置 `mysql:8 + redis:7` service container 跑 SQL 集成测试，具备基础设施条件。

### 4.2 单元测试补全

| 类别 | 待补项 | 优先级 |
|---|---|---|
| HTTP handler | 退役（DELETE device）、取消（cancel task）、告警 ack、告警 silence、autoProvision、provision 签发、batch 下发、CMDB CRUD、workflow 触发、deploy rollback、logs 查询、me 解析 | 高 |
| 后台 loop | leaderLoop、notifyLoop、autoProvisionLoop、reconcileLoop、scheduleLoop、archiveLoop、reclaimLoop、cancelLoop | 高 |
| domain 行为 | 任务状态机迁移、重试/死信判定、纳管状态翻转、告警规则匹配 | 中 |

### 4.3 集成测试补全

按拆分后的 Store 小接口逐方法补全 SQL 集成测试，复用 CI 已有的 `mysql:8 + redis:7` service container：

| Store 接口 | 集成测试重点 |
|---|---|
| `DeviceStore` | CRUD、纳管状态翻转、退役归档、租户隔离 |
| `TaskStore` | ClaimTask 原子性、结果回写、死信、取消、租约超时回收 |
| `AlertStore` | Ack/Silence 状态机、租户过滤 |
| `TokenStore` | 签发、消费、一次性、限时过期 |
| `LeaderStore` | 续租、抢主、租约过期 |
| `ScheduleStore` | 派生实例幂等、cron 触发 |

### 4.4 并发测试

在 `go test -race` 下验证并发敏感路径：

| 路径 | 验证点 |
|---|---|
| worker pool | 任务并发领取无重复、worker 退出无泄漏 |
| cancelLoop | 取消信号与 worker context 取消的竞态 |
| ClaimTask | 多 agent 并发领取同一任务的原子性 |
| leaderLoop | 多副本并发续租的互斥 |

### 4.5 E2E 测试

补充安全相关 E2E 场景：

| 场景 | 验证点 |
|---|---|
| token 过期 | install token 超时后注册被拒 |
| 租户越权 | 跨租户查询/操作返回 403/404 |
| mTLS | 无客户端证书的连接被拒 |
| require-auth | 缺失租户头的请求被拒 |
| 任务取消全链路 | pending 拦截 + running 强杀 + worker 不回写 |

### 4.6 前端测试

引入 Vitest + jsdom 测试核心渲染函数与 API 封装：

| 测试对象 | 验证点 |
|---|---|
| `render.js` | 表格渲染、状态徽章、空态 |
| `api.js` | 租户头注入、错误处理、响应解析 |
| `poll.js` | 轮询启停、刷新策略 |

### 4.7 覆盖率门禁

| 指标 | 目标 | 门禁方式 |
|---|---|---|
| Go 行覆盖率 | ≥ 70% | CI 跑 `go test -cover`，低于阈值阻断合并 |
| Go 关键包 | ≥ 85% | store/domain/controlplane 单独阈值 |
| 前端行覆盖率 | ≥ 60% | Vitest + c8，CI 阻断 |

---

## 5. 多平台部署与交付

### 5.1 容器镜像

| 镜像 | Dockerfile | 基础镜像 | 说明 |
|---|---|---|---|
| controlplane | `Dockerfile` | distroless | 多阶段构建，产物为单二进制 `opsmesh`，无 shell 最小攻击面 |
| agent | `Dockerfile.agent` | base-debian12 | 含 sh（agent 需执行 shell 脚本），多阶段构建产物 `opsmesh-agent` |

### 5.2 docker-compose 一键体验

补 `docker-compose.yaml`（README 已提及但需完善交付物）：

| 服务 | 说明 |
|---|---|
| controlplane | 依赖 mysql + redis，挂载 TLS 证书 |
| mysql | mysql:8，持久化卷 |
| redis | redis:7，持久化卷 |
| agent | 注册到 controlplane，演示纳管闭环 |

目标：`docker compose up` 一键起完整体验环境。

### 5.3 Helm Chart

✅ **已交付（2026-08-02）**：`deploy/helm/opsmesh/` 已落地，含以下全套模板，兑现 README 承诺：

| 模板 | 资源 | 说明 |
|---|---|---|
| controlplane | Deployment + Service + PDB | 多副本，PDB(minAvailable=1)，liveness/readiness probe |
| agent | DaemonSet + Service | 每节点一个， tolerations 支持管理节点 |
| mysql | StatefulSet + Service | 持久化 PVC |
| redis | StatefulSet + Service | 持久化 PVC |
| configmap / secret | — | provision-secret / mysql-dsn / TLS 证书挂载 |
| values.yaml | — | 默认值 |
| values-production.yaml | — | 生产 overlay：副本数、资源、TLS、require-auth |

> 后续仅需补 Argo CD ApplicationSet 网段批量渲染（见 5.4）。

### 5.4 GitOps

建立 `opsmesh-gitops` 仓库 + Argo CD ApplicationSet，按网段批量渲染控制面与 agent 部署：

| 组件 | 说明 |
|---|---|
| `opsmesh-gitops/` | Helm Charts（controlplane / middleware）+ 网段 values 目录 |
| ApplicationSet | 按 CIDR 网段列表生成 Application，每段一套控制面 + agent 集群 |

### 5.5 二进制分发

引入 `goreleaser` 跨平台构建：

| 目标 | 产物 |
|---|---|
| linux/amd64 | `opsmesh-linux-amd64` |
| linux/arm64 | `opsmesh-linux-arm64` |

发布到 GitHub Release，附 SHA256 checksums 与 SBOM。支持 `curl ... | sh` bootstrap 拉取对应架构二进制。

### 5.6 systemd 裸机部署

提供 `deploy/systemd/` unit 文件，支持裸机/VM 部署：

| 文件 | 说明 |
|---|---|
| `opsmesh-controlplane.service` | 控制面服务，Restart=always，依赖网络就绪 |
| `opsmesh-agent.service` | agent 服务，环境变量注入控制面地址与 token |
| `opsmesh-controlplane.env` | 环境变量模板（OPSMESH_STORE、DSN、TLS 路径等） |

### 5.7 Kubernetes Operator（远期）

用 operator 管理 OpsMesh 实例 CRD，实现控制面与 agent 的声明式部署、版本升级、TLS 轮转、备份恢复。CRD 示例 `OpsMeshInstance` 描述租户、副本数、存储后端、网段等，operator 调和到目标状态。

---

## 6. 安全加固路线

### 6.1 纵深防御

| 加固项 | 现状 | 目标 |
|---|---|---|
| agent shell 命令白名单 | 无限制，任意 shell | 引入可配白名单（允许的命令前缀），超权拒绝并审计 |
| file 路径白名单 | 无限制 | 限制文件分发目标路径在允许根目录下，防越权写入 |
| bootstrap token 编码 | 未防 shell 注入 | 改用 base64url 编码，避免 token 含 shell 元字符导致注入 |
| webhook URL 校验 | 无 SSRF 防护 | 校验 webhook URL 协议与目标，禁止内网/元数据地址 |
| autoProvision CIDR 白名单 | 无限制 | 限制自动纳管扫描网段在配置白名单内 |

### 6.2 身份强化

| 加固项 | 现状 | 目标 |
|---|---|---|
| JWT 验签 | 纯依赖网关注入身份头 | 内核侧增加 JWT 验签选项（网关公钥验签），从 token 提取租户/用户，不纯依赖头注入 |
| mTLS 身份绑定 | mTLS 已支持但未绑定租户 | 强制 agent mTLS 证书 CN/SAN 与租户绑定，防 agent 跨租户冒认 |

### 6.3 SSRF 防护

- webhook 回调 URL 校验：协议白名单（https）、解析 IP 拒绝私网/loopback/元数据地址（169.254.169.254）
- autoProvision 扫描 CIDR 白名单：仅允许配置的网段，拒绝扫描未授权网段
- 任意用户可控的出站 URL 统一过 SSRF 校验中间件

### 6.4 密钥管理

| 加固项 | 目标 |
|---|---|
| 敏感配置文件读取 | 支持 `--provision-secret-file` 从文件读取密钥，文件权限 0600，避免命令行明文 |
| Vault/KMS 集成（远期） | 密钥从 Vault/KMS 动态获取，支持轮转 |
| TLS 证书轮转 | 控制面与 agent 支持证书热重载，配合 cert-manager 自动轮转 |

### 6.5 安全头收紧

前端去除 inline script/style 后，CSP 收紧为无 `unsafe-inline`：

| 头 | 目标值 |
|---|---|
| Content-Security-Policy | `default-src 'self'; script-src 'self'; style-src 'self'` |
| X-Content-Type-Options | `nosniff` |
| X-Frame-Options | `DENY` |
| Strict-Transport-Security | `max-age=31536000; includeSubDomains` |

### 6.6 依赖安全

| 措施 | 说明 |
|---|---|
| govulncheck | CI 跑 `govulncheck`，已知漏洞阻断合并 |
| Dependabot | 启用 Go 依赖自动 PR 升级 |
| go.sum 强校验 | `go mod verify` 入 CI，防供应链篡改 |
| kafka-go 升级窗口 | 跟踪 Go 1.23+ 迁移，解除 `v0.4.48` 钉版约束 |

### 6.7 等保三级

| 要求 | 目标 |
|---|---|
| 审计 6 月留存 | audit_log 定期归档至冷存储，保留 ≥ 6 月，过期前导出备份 |
| 审计定期导出 | 提供导出接口/任务，支持按租户/时间窗导出审计 |
| 入侵检测增强 | shell 命令审计增强（记录完整命令行）、异常登录检测、敏感操作告警 |

---

## 7. 模块完善与产品化

### 7.1 CMDB

| 阶段 | 目标 |
|---|---|
| 现状 | Phase1 已有模型 + CRUD + SQL + 采集 |
| 计划 | 采集自动化（定时采集主机/服务元信息）、关系图谱可视化（设备依赖/网络拓扑）、变更审批（CMDB 变更走审批流） |

### 7.2 作业编排

| 阶段 | 目标 |
|---|---|
| 现状 | DAG 引擎 + store 阻塞→释放链路 + 画布 |
| 计划 | 子工作流展开、并行/串行/条件分支节点、节点级超时与重试策略、编排执行历史与回放 |

### 7.3 服务部署

| 阶段 | 目标 |
|---|---|
| 现状 | 计划 + fan-out 执行 + Reconcile + Rollback |
| 计划 | 蓝绿发布策略、金丝雀发布策略（按比例/按标签灰度）、发布门禁（健康检查通过才推进）、自动回滚触发条件 |

### 7.4 监控告警

| 阶段 | 目标 |
|---|---|
| 现状 | 规则引擎 + alert + Ack/Silence + Webhook/飞书/钉钉 |
| 计划 | 阈值规则完善、异常检测规则（基于基线偏离）、告警通道扩展（邮件/Slack/企业微信）、告警聚合与抑制（防风暴） |

### 7.5 日志检索

| 阶段 | 目标 |
|---|---|
| 现状 | logstore 双后端（Memory/SQL）+ offset 分页 |
| 计划 | 对接 ELK / Loki 后端、全文本检索（倒排索引）、日志采集 agent 端推送、查询语法（Lucene/KQL 风格） |

### 7.6 多租户

| 阶段 | 目标 |
|---|---|
| 现状 | 行级隔离（tenant_id 列 + 查询过滤） |
| 计划 | 重租户演进到 schema 级隔离（每租户独立 schema/库），支持租户级资源配额与计费 |

### 7.7 联邦

| 阶段 | 目标 |
|---|---|
| 现状 | 单中心，跨网段规模化靠每段一套控制面 |
| 计划 | 跨网段控制面联邦、任务跨段转发、联邦级设备/任务视图（`DELIVERY.md` 提及的未来方向） |

---

## 8. 里程碑规划

### 8.1 时间线总览

| 里程碑 | 时间窗 | 主题 | 关键交付 |
|---|---|---|---|
| M1 | 1–2 周 | P0 修复与交付补全 | 本次 P0 修复完成、`docker-compose.yaml`、`goreleaser`、`golangci-lint` 全量门禁 |
| M2 | 1 个月 | 测试与工程化基线 | 测试覆盖率 ≥ 70%、Helm Chart、前端 Phase 1 组件化与设计系统、Store 接口拆分 |
| M3 | 2–3 个月 | 前端主线增强与协议标准化 | 前端 Vue 3 主线增强（SSE/类型/测试基线）、protobuf 引入、JWT 验签 |
| M4 | 远期 | 规模化与生态 | K8s operator、控制面联邦、多租户 schema 隔离、ELK/Loki 集成 |

### 8.2 M1 详细目标

| 工作项 | 验收标准 |
|---|---|
| P0 修复 | 完成本次审核发现的全部 P0 项 |
| docker-compose | `docker compose up` 一键起 controlplane + mysql + redis + agent |
| goreleaser | 跨平台构建 linux amd64/arm64，GitHub Release + checksums |
| golangci-lint | 全量 lint 入 CI，零 warning |

### 8.3 M2 详细目标

| 工作项 | 验收标准 |
|---|---|
| 测试补全 | Go 行覆盖率 ≥ 70%，关键包 ≥ 85%，CI 阻断 |
| Helm Chart | `deploy/helm/opsmesh/` 含 controlplane/agent/mysql/redis 模板 + values-production.yaml |
| 前端组件化 | Vue3 企业版组件库基线（表格/抽屉/徽章/表单/按钮）+ 设计 token 固化 |
| Store 接口拆分 | 巨型接口拆分为领域小接口，消费方按需依赖 |

### 8.4 M3 详细目标

| 工作项 | 验收标准 |
|---|---|
| 前端 Vue 3 主线增强 | Vue 3 + Vite + Pinia 持续演进（SSE/类型/测试基线）；原生 JS 个人版按 v0.4.0 移除计划收敛，仅修 P0 bug |
| protobuf 引入 | buf 工具链，生成 stub 替换手写 ServiceDesc，breaking 检查入 CI |
| JWT 验签 | 内核侧支持网关公钥验签，不纯依赖头注入 |
| SSE 实时推送 | 任务/告警/设备事件推送替代轮询 |

### 8.5 M4 详细目标

| 工作项 | 验收标准 |
|---|---|
| K8s operator | OpsMeshInstance CRD，声明式部署/升级/轮转 |
| 控制面联邦 | 跨网段任务转发，联邦视图 |
| 多租户 schema 隔离 | 重租户独立 schema，配额与计费 |
| ELK/Loki 集成 | 日志检索对接外部后端，全文本检索 |

---

## 9. 前端收敛与个人版弃用策略

### 9.1 策略定位

OpsMesh 前端采取**收敛而非分叉**策略：Vue3 企业版为唯一主线，原生 JS 个人版进入弃用期并计划移除。此策略与 `README.md` "前端收敛策略" 子节完全一致，取代早期"个人版与企业版长期并行分叉"的设想。

| 前端 | 状态 | 说明 |
|---|---|---|
| Vue3 企业版 (`web/enterprise/`) | ✅ 主线 | 唯一维护的前端，所有新功能优先在此开发 |
| 原生 JS 个人版 (`internal/controlplane/web/`) | ⚠️ Deprecated | 自 v0.2.0 起进入弃用期，计划在 v0.4.0 移除。期间仅修 P0 bug，不新增功能 |

### 9.2 弃用期维护边界

| 项 | 规则 |
|---|---|
| 个人版新增功能 | 不接受（原 Phase 1 模块化、TypeScript、Vue 3 迁移等演进路径全部取消） |
| 个人版 P0 bug | 修复至 v0.4.0 移除 |
| 个人版 P1/P2 issue | 仅记录，不修 |
| 迁移支持 | 提供 `web/enterprise/` 功能对照表，协助现有个人版用户迁移 |
| 移除节点 | v0.4.0：删除 `internal/controlplane/web/` 原生 JS 代码，控制面 B/S 仪表盘由 Vue3 企业版产物托管或 Go 模板轻量渲染 |

### 9.3 内核与部署形态

前端收敛不影响内核与部署形态的按需切换。内核（Go）仍共享同一 codebase，通过 `--store`/`--production` 等 flag 切换形态；部署形态按场景选择（单二进制 / Helm / systemd / operator），与前端版本正交。

| 层 | 策略 |
|---|---|
| 内核（Go） | 共享同一 codebase，通过 flag 切换形态，编译标签控制可选依赖（如 kafka） |
| domain | 共享富领域模型 |
| store | 共享接口，默认实现按 `--store` 选择（memory / mysql 等） |
| 前端 | **收敛**：Vue3 企业版唯一主线，原生 JS 个人版 deprecated → v0.4.0 移除 |
| 部署 | 按场景选择：单二进制 / Helm / systemd / operator，与前端版本正交 |
| 文档 | 共享 `README.md`，部署指南按场景分（`docs/deploy-personal.md` / `docs/deploy-enterprise.md`） |

### 9.4 演进节奏

Vue3 企业版按里程碑持续演进；原生 JS 个人版仅维持 P0 修复至 v0.4.0 移除，不再有独立演进节奏。

| 里程碑 | Vue3 企业版主线 | 原生 JS 个人版 |
|---|---|---|
| M1 | 组件库基线 | 仅修 P0 bug |
| M2 | 设计系统固化 + 测试基线 | 仅修 P0 bug |
| M3 | SSE 实时推送 + 类型全覆盖 | 仅修 P0 bug |
| M4 | operator 集成 + 多集群管理 | v0.4.0 移除 |

---

## 附录：规划与现状区分声明

本文档中所有"计划/目标/演进/远期"措辞均为规划意图，不代表已实现能力。已实现能力以 `README.md` 功能矩阵与 `DELIVERY.md` 交付说明为准。具体而言：

- Helm Chart、`docker-compose.yaml`：README 已提及且仓库已提供（已交付，见 5.3）。Argo CD ApplicationSet、`goreleaser`、systemd unit：仍为 M1/M2 计划交付物，仓库未提供或未完善
- Store 接口拆分、DDD 实质化、protobuf、Vue 3 主线增强（SSE/类型/测试基线）、operator、schema 隔离：均为演进规划，当前未实现。Vue3 企业版前端（`web/enterprise/`）已交付为主线；原生 JS 个人版弃用策略已定（deprecated since v0.2.0，removal target v0.4.0），不属演进规划。联邦（控制面跨网段任务转发 mTLS + HMAC 签名验签，P1-6）已于 2026-08-02 落地
- 安全加固项（命令白名单、JWT 验签、SSRF 校验、CSP 收紧等）：均为规划，当前未实现