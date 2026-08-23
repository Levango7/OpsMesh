# OpsMesh 产品设计文档

> 版本：v0.7.0  ·  编制日期：2026-08-17  ·  适用基线：MVP（自研 gRPC 管控通道）+ 安全加固
>
> 本文档基于 `README.md`、`DELIVERY.md`、`docs/product-roadmap.md` 与 `docs/api-reference.md` 编制，描述 OpsMesh 的产品定位、目标用户、功能矩阵、竞品对比、商业模式、适用场景、非功能需求与路线图。已实现能力以 `README.md` 功能矩阵为准，规划项以 `product-roadmap.md` 为准。

---

## 第1章 产品定位

### 1.1 一句话定义

OpsMesh 是**私有化单中心 B/S 自动化部署与运维平台**，又称"网段运维中枢"：服务部署到某网段后，整段网络打通的设备自动纳管，各设备可并行执行各自的自动化任务（shell 脚本 / 服务管理 / 文件分发），具备失败重试、死信、取消、定时周期、告警等完整任务生命周期。

### 1.2 解决的核心问题

传统运维工具（Ansible/SaltStack/蓝鲸）在以下场景存在结构性短板，OpsMesh 针对性解决：

| 问题 | 传统工具表现 | OpsMesh 方案 |
|---|---|---|
| 网段割裂下的统一纳管 | Ansible 需逐台配置 inventory；SaltStack master 单点；蓝鲸依赖 CMDB 预录入 | 服务部署到某网段后，整段设备自动纳管（令牌闭环 + TCP 存活扫描） |
| 大规模并行任务执行 | Ansible SSH 慢且无状态；SaltStack 消息总线易积压 | gRPC 长连接 + agent worker 池并发执行，每 agent 默认 4 并发可调 |
| 任务生命周期完整性 | 多数工具只有成功/失败两态 | pending → running → done/failed/cancelled（dead_letter 为标记非状态） + 重试 + 死信 + 取消 + 定时调度 |
| 私有化数据合规 | 云运维产品数据出机房 | 单二进制私有部署，MySQL 数据本地化，100% 审计留痕，等保三级对照 |
| 多租户隔离 | 多数开源工具无租户概念 | 行级隔离（tenant_id）+ schema 级隔离（`--multi-schema`）+ RBAC 三表 |
| 异地多中心规模化 | 蓝鲸 GSE 级联复杂且社区版受限 | 每段一套控制面 + 控制面联邦（mTLS + HMAC 签名验签）跨网段任务转发 |

### 1.3 核心价值主张

1. **零依赖起步**：默认 MemoryStore，单二进制双模式（`--mode=controlplane|agent`），30 秒启动体验闭环；生产可平滑切换 MySQL + Redis + 多副本 HA。
2. **网段即边界**：以网段（segment）为纳管分桶键，部署到某段即自动纳管该段全部打通设备，无需逐台 inventory。
3. **完整任务生命周期**：重试 / 死信 / 取消（pending 拦截 + running 强杀）/ 定时周期 / 批量下发 / 作业审批，覆盖运维真实复杂度。
4. **企业级安全基线**：gRPC TLS/mTLS、JWT 双 Token、登录防爆破、RBAC 持久化、审计 100% 留痕、metrics CIDR 白名单、联邦 mTLS + HMAC 签名验签、SSRF 校验、shell/file 白名单。
5. **多形态交付**：单二进制 / docker-compose / Helm Chart / systemd unit / goreleaser 跨平台二进制 / Argo CD GitOps，覆盖裸机、VM、K8s、混合云。
6. **可扩展架构**：Store 接口按领域拆分为 17 个子接口，事件总线可插拔（noop/log/kafka），日志后端可切换（memory/sql/loki/es），告警通道多形态（Webhook/飞书/钉钉/Slack/企业微信/邮件）。

### 1.4 与传统工具的根本差异

OpsMesh 不是"另一个 Ansible"。Ansible 是**无中心、推送式、SSH-based** 的配置工具；OpsMesh 是**有中心、长连接、agent-based** 的运维平台，强调：

- **常驻 agent**：agent 注册后保持 gRPC 长连接，主动拉取任务并上报结果，无需控制面 SSH 推送，规避 SSH 凭证管理与防火墙打洞。
- **状态化任务**：任务有租约、重试计数、死信标记、取消信号，控制面对任务全生命周期可观测、可干预。
- **多租户原生**：从设计之初即有 tenant_id 行级隔离与 RBAC，不是事后补丁。
- **网段联邦**：跨网段规模化靠控制面联邦而非单一巨型 master，符合电信资源池、异地机房的真实拓扑。

---

## 第2章 目标用户

### 2.1 用户角色画像

| 角色 | 职责 | 在 OpsMesh 中的典型操作 | 关心指标 |
|---|---|---|---|
| 运维工程师 | 日常运维操作执行 | 下发 shell 任务、批量重启服务、分发配置文件、查看任务结果与告警 | 任务成功率、执行延迟、告警响应时长 |
| SRE | 可靠性工程化，关注自动化与可观测 | 编写作业编排 DAG、配置告警规则与异常检测、对接 ELK/Loki、灰度发布门禁 | SLO 达标率、MTTR、自动化覆盖率 |
| IT 管理员 | 资产与权限管理 | 网段发现与纳管、CMDB 模型与实例维护、RBAC 用户/角色/权限分配、租户配额 | 资产纳管率、权限合规率、审计完整性 |
| 平台团队 | 运维平台建设与运营 | 部署 OpsMesh 控制面（Helm/systemd）、配置多副本 HA、对接企业 SSO/JWT、联邦跨网段 | 平台可用性、租户接入数、联邦延迟 |

### 2.2 典型用户画像

**画像 A · 电信资源池运维工程师（核心用户）**
- 背景：负责多个地市资源池的 Linux 主机运维，单网段 200-2000 台，跨网段需统一视图。
- 痛点：Ansible SSH 慢且 inventory 难维护；蓝鲸部署重；脚本散落无审计。
- 在 OpsMesh 中：每地市部署一套控制面，agent 自动纳管本段设备；联邦跨段下发补丁任务；告警推送到飞书；审计留痕供等保检查。

**画像 B · 互联网公司 SRE**
- 背景：管理 K8s 多集群 + 裸金属中间件，需灰度发布与异常检测。
- 痛点：K8s 集群与裸金属运维割裂；发布门禁靠人肉；告警风暴严重。
- 在 OpsMesh 中：K8s 集群接入 `k8s/clusters` API 统一管理；灰度发布门禁 + 自动回滚；异常检测规则（Z-Score + EWMA）+ 告警聚合抑制。

**画像 C · 金融 IT 管理员**
- 背景：私有云 + 裸金属混合，强合规要求（等保三级），数据不出机房。
- 痛点：云运维产品数据出机房不合规；开源工具无租户隔离；审计不完整。
- 在 OpsMesh 中：单二进制私有部署，MySQL 数据本地化；多租户 schema 隔离；审计 100% 留痕 + 6 月留存；RBAC 三表 + JWT 验签。

**画像 D · 集团平台团队**
- 背景：多子公司共用运维平台，需租户隔离与配额计费。
- 痛点：单租户平台无法隔离子公司；无配额控制易被某子公司挤占资源。
- 在 OpsMesh 中：每子公司一租户，行级隔离 + schema 隔离；租户级资源配额与计费 API；联邦跨子公司任务转发。

### 2.3 非目标用户

- **个人开发者本地开发**：OpsMesh 不是个人脚本管理工具，零依赖启动虽友好但功能面向团队/企业。
- **公有云 SaaS 运维**：OpsMesh 当前定位私有化，云服务形态属远期规划（见 §5.3）。
- **无 agent 容忍场景**：OpsMesh 依赖常驻 agent，纯 SSH 推送场景请用 Ansible。

---

## 第3章 核心功能矩阵

> 成熟度标注：✅ 功能完整（CI 验证中）/ 🟡 已交付待完善 / 🔵 规划中 / ⚪ 未规划
>
> 与 `docs/product-roadmap.md`、`docs/feature-design.md` 口径一致：✅ 表示"已交付且功能完整"，不等同于"生产可用"；CI 集成测试/安全扫描/lint/race 检测需 GitHub Actions runner 真跑（详见 `DELIVERY.md` §7）。

### 3.1 运维执行核心

| 功能模块 | 能力描述 | 成熟度 | 落地文件 / 入口 |
|---|---|---|---|
| 设备纳管 | 网段 TCP 存活扫描 + 候选设备登记 + 令牌闭环自动纳管 + 设备退役归档 | ✅ | `internal/discover/`、`internal/controlplane/provision.go` |
| 任务执行 | Shell 命令 / 系统服务管理 / 文件分发（原子写入 + rename）/ 超时自动中止 / 批量下发 | ✅ | `internal/agent/`、`internal/controlplane/tasks.go` |
| 配置下发 | 文件分发 + CMDB 配置项驱动 + OS 优化模板 | ✅ | `internal/controlplane/os_optimize.go` |
| 服务管理 | systemctl start/stop/restart/status + 中间件部署（10+ 中间件 × docker/systemd） | ✅ | `internal/controlplane/middleware_deploy.go` |
| 文件分发 | 原子写入 + rename + 路径白名单（`--agent-file-root-whitelist`）+ 路径遍历防护 | ✅ | `internal/agent/`、`internal/controlplane/tasks.go` |
| 状态监控 | agent 心跳 + 设备在线状态 + Prometheus 指标 + /healthz + /readyz | ✅ | `internal/metrics/`、`internal/controlplane/server.go` |
| 告警 | 规则引擎 + alert（Status/Ack/Silence）+ Webhook/飞书/钉钉/Slack/企业微信/邮件 + 异常检测（Z-Score + EWMA）+ 聚合抑制 | ✅ | `internal/alertengine/`、`internal/notify/` |

### 3.2 资产与编排

| 功能模块 | 能力描述 | 成熟度 | 落地文件 / 入口 |
|---|---|---|---|
| CMDB | Phase1 模型 + CRUD + SQL + 采集 + 关系图谱可视化（力导向图）+ 变更审批 | ✅ | `internal/cmdb/`、`web/enterprise/src/views/CMDBView.vue` |
| 作业编排 | DAG 引擎 + store 阻塞→释放链路 + 画布 + 子工作流 + 并行/串行/条件分支 + 节点级超时重试 + 执行历史回放 | ✅ | `internal/dag/`、`internal/orchestration/` |
| 部署编排 | 计划 + fan-out 执行 + Reconcile + Rollback + 蓝绿/金丝雀/滚动 + 发布门禁 + 自动回滚 + 多集群联邦发布 + 灰度指标自适应推进 | ✅ | `internal/deploy/`、`internal/controlplane/deploys.go` |
| K8s 管理 | 集群增删查 + 测试连接 + 资源只读/写（namespace/pod/deployment/service/configmap/secret/node）+ scale/restart/rollback + client-go 无 kubectl 依赖 + 租户隔离 | ✅ | `internal/controlplane/k8s_cluster.go`、`k8s_manage.go`、`internal/k8s/` |
| OS 优化 | 预置模板（内核/网络/安全/时间同步/SSH/磁盘/系统/用户）+ 在线 CRUD + 在指定 agent 执行 + 模板 store 持久化 + 幂等 seed | ✅ | `internal/controlplane/os_optimize.go` |
| 中间件部署 | 10+ 中间件（MySQL/Redis/Kafka/Nginx/Tomcat/Zookeeper/PostgreSQL/MongoDB/RabbitMQ/Elasticsearch）× docker/systemd 双模式 + CRUD + 实例查询 | ✅ | `internal/controlplane/middleware_deploy.go` |

### 3.3 平台基线

| 功能模块 | 能力描述 | 成熟度 | 落地文件 / 入口 |
|---|---|---|---|
| 多租户 | 行级隔离（tenant_id）+ schema 级隔离（`--multi-schema`）+ 租户级资源配额与计费 | ✅ | `internal/controlplane/quota.go`、`internal/store/sql.go` |
| RBAC | 持久化三表（users/roles/permissions）+ 种子幂等 + JWT 双 Token + 网关注入身份头双路径 | ✅ | `internal/store/sql_rbac.go`、`internal/controlplane/auth.go` |
| 联邦 | 控制面联邦 mTLS + HMAC 签名验签 + 跨网段任务转发 + 联邦级设备/任务视图 + 多集群联邦发布协调 | ✅ | `internal/controlplane/federation.go`、`internal/deploy/federation.go` |
| 密钥管理 | Env/File/Vault/Chain provider + `--secret-provider` 配置 + `${vault:key}` 引用解析 + 告警通道密钥外置 + 前端 SecretsView UI + kubeconfig AES-256-GCM 加密 | ✅ | `internal/secrets/`、`web/enterprise/src/views/SecretsView.vue` |
| 日志检索 | 双后端（Memory/SQL）+ offset 分页 + 全文本检索（倒排索引 + 中英文分词 + TF-IDF + 短语/布尔/通配符）+ Loki/ES 适配 | ✅ | `internal/logstore/` |
| 审计 | 100% 留痕（AuditEvent → audit_log / memory ring）+ 可查（租户/动作/时间窗过滤）+ 6 月留存规划 | ✅ | `internal/controlplane/server.go`、`internal/store/sql.go` |
| HA | 多副本 leader 选举（leader_lease 表）+ 超期任务自动回收 + agent 多控制面 failover + PodDisruptionBudget | ✅ | `internal/controlplane/server.go`（leaderLoop） |

### 3.4 成熟度汇总

| 成熟度 | 数量 | 模块 |
|---|---|---|
| ✅ 功能完整（CI 验证中） | 18 | 全部上述模块 |
| 🟡 已交付待完善 | 0 | — |
| 🔵 规划中 | 0 | — |

> **结论**：v0.7.0 基线下，用户要求的 18 个核心运维功能模块功能完整（CI 验证中），无规划中或待完善项；"生产可用"结论以 CI 真跑通过为准（见 `DELIVERY.md` §7）。AI 增强能力（AI-03~AI-14 共 12 项，见 `docs/ai-design.md`）为独立规划线，不在此 18 模块范围内。其他深化能力（如等保三级审计 6 月留存、Vault/KMS 远期轮转）见 §8 路线图。

---

## 第4章 竞品对比

### 4.1 对比维度说明

| 维度 | 说明 |
|---|---|
| 架构 | 中心化/无中心、agent/agentless、推送/拉取 |
| 任务模型 | 状态机完整度、重试/死信/取消/定时 |
| 多租户 | 原生支持/事后补丁/不支持 |
| 私有化 | 数据是否出机房、部署复杂度 |
| 网段规模化 | 跨网段/多中心能力 |
| 生态 | K8s/中间件/CMDB/日志/告警集成度 |
| 学习曲线 | 上手成本与文档完备度 |

### 4.2 竞品对比表

| 维度 | OpsMesh | Ansible | SaltStack | 蓝鲸 | 阿里运维（云效/EMS） | 腾讯织云 |
|---|---|---|---|---|---|---|
| 架构 | 有中心 + agent + gRPC 长连接拉取 | 无中心 + agentless + SSH 推送 | 有中心 + agent + ZeroMQ 消息总线 | 有中心 + agent + GSE 通道 | SaaS / 私有化 + agent | SaaS / 私有化 + agent |
| 任务模型 | 5 态 + 重试 + 死信 + 取消 + 定时 + 审批 | 仅成功/失败，无状态机 | 成功/失败 + 重试，无死信 | 较完整但耦合 CMDB | 较完整但耦合阿里云 | 较完整但耦合腾讯云 |
| 多租户 | 原生行级 + schema 级隔离 + RBAC 三表 | 不支持 | 不支持 | 后期补丁 | 原生（云账号） | 原生（云账号） |
| 私有化 | 单二进制 + MySQL 本地化 + 100% 审计 | 无服务，纯 CLI | master 私有但依赖较多 | 私有但部署重（多组件） | 私有化版本受限 | 私有化版本受限 |
| 网段规模化 | 控制面联邦 mTLS + HMAC | 需自建 inventory 分片 | master 单点瓶颈 | GSE 级联复杂 | 依赖云骨干 | 依赖云骨干 |
| K8s 集成 | client-go 原生 + 集群管理 + 资源读写 + Operator | 需 k8s 模块 + SSH | 需 salt-cloud | 配置平台独立 | 云原生集成 | 云原生集成 |
| CMDB | 内置 Phase1 + 关系图谱 + 采集 | 无 | 无 | 强（核心组件） | 云资产自动同步 | 云资产自动同步 |
| 日志检索 | 内置 + Loki/ES 适配 + 全文本倒排 | 无 | 无 | 日志平台独立 | 云日志服务 | 云日志服务 |
| 告警 | 规则引擎 + 异常检测 + 多通道 + 聚合抑制 | 无 | 无 | 监控平台独立 | 云监控 | 云监控 |
| 学习曲线 | 单二进制 30 秒起步，116 flag 渐进 | 低（YAML + SSH） | 中（YAML + master） | 高（多组件 + CMDB） | 低（云控制台） | 低（云控制台） |
| License | 私有（内部项目） | 开源 GPL | 开源 Apache | 开源 MIT（社区版受限） | 商业 | 商业 |

### 4.3 优势与劣势总结

**OpsMesh 优势**：
1. **网段即边界**的纳管模型，针对电信资源池/异地机房场景无可替代。
2. **完整任务生命周期**（5 态 + 重试 + 死信 + 取消 + 定时 + 审批），覆盖运维真实复杂度。
3. **原生多租户**，从设计之初即隔离，平台团队可服务多子公司。
4. **零依赖起步 + 生产可平滑升级**，30 秒体验闭环，降低试用门槛。
5. **私有化合规**，数据不出机房，等保三级对照，金融/政企友好。
6. **联邦跨网段**，mTLS + HMAC 签名验签，规模化不靠单巨型 master。
7. **K8s 原生**，client-go 无 kubectl 依赖，集群管理 + 资源读写 + Operator。

**OpsMesh 劣势**：
1. **生态广度不及 Ansible**：Ansible Galaxy 模块生态庞大，OpsMesh 模块自研为主。
2. **学习曲线在 116 flag**：虽渐进但全量配置心智负担，需文档引导（已有 `flag-matrix.md`）。
3. **License 私有**：非开源，社区贡献受限，与开源竞品相比生态扩散慢。
4. **云原生场景不及阿里/腾讯**：OpsMesh 不绑定云，云原生集成（云监控/云日志/云资产自动同步）需自配。
5. **品牌与案例积累不及蓝鲸**：蓝鲸在腾讯内部大规模验证，OpsMesh 案例积累尚在早期。
6. **前端工程化历史包袱**：原生 JS 个人版虽已收敛，但 Vue3 企业版仍在持续演进。

---

## 第5章 商业模式

### 5.1 开源策略

当前 License 为**私有（内部项目）**，未开源。若未来开源，建议策略：

| 项 | 建议 |
|---|---|
| License | Apache 2.0（商业友好，便于企业采用） |
| 开源范围 | 内核（Go 控制面 + agent + Operator）+ Helm Chart + 文档 |
| 保留范围 | 企业版前端（Vue3 高级组件）、企业级 SSO/JWT 集成插件、Vault/KMS 密钥管理 UI、商业支持 |
| 社区治理 | 接受社区 PR（内核 bugfix + 模块贡献），企业版独立仓库 |
| 版本策略 | 内核开源版本与企业版同步发布，企业版功能以插件/flag 开关形式存在 |

### 5.2 企业版

企业版定位为**私有化部署的商业产品**，在开源内核基础上增加：

| 能力 | 开源版 | 企业版 |
|---|---|---|
| 前端 | 引导页 + 基础仪表盘 | Vue3 企业版（组件库 + 设计系统 + SSE 实时推送 + 全功能视图） |
| 多租户 schema 隔离 | ✅ | ✅ |
| 租户配额与计费 | 🟡 基础 | ✅ 完整计费 API + 报表 |
| 联邦跨网段 | ✅ | ✅ + 联邦级发布协调 UI |
| SSO/JWT 集成 | 头注入模式 | ✅ + OIDC/SAML/LDAP 适配器 |
| 密钥管理 | Env/File | ✅ + Vault/KMS + 前端 UI + 轮转 |
| 审计合规 | 100% 留痕 | ✅ + 6 月归档 + 导出 + 等保三级报告 |
| 商业支持 | 社区 issue | SLA 保障 + 工单系统 + 专属技术支持 |

### 5.3 云服务

云服务形态属**远期规划**，当前定位私有化。若未来推出，可能形态：

| 形态 | 说明 | 适用场景 |
|---|---|---|
| OpsMesh SaaS | 华为云/阿里云托管控制面，用户只需部署 agent | 中小企业不愿自运维控制面 |
| OpsMesh Hybrid | 云控制面 + 私有化 agent（数据不出机房，控制面出云） | 混合云，控制面统一管理 |
| OpsMesh Managed | 私有化部署但由云厂商托管运维（RDS/Redis 替代自建） | 私有化合规 + 降低运维负担 |

> **当前优先级**：私有化交付为主，云服务形态待规模化后再立项。

---

## 第6章 适用场景

### 6.1 场景矩阵

| 场景 | 适配度 | 关键能力依赖 | 说明 |
|---|---|---|---|
| 异地机房 | ✅ 强适配 | 控制面联邦 + 跨网段任务转发 | 每地一套控制面，联邦 mTLS + HMAC 签名验签跨段下发，规避单巨型 master |
| 多数据中心 | ✅ 强适配 | 控制面联邦 + 多集群联邦发布 | 多 DC 统一视图，灰度发布跨 DC 协调 |
| 电信资源池 | ✅ 核心场景 | 网段纳管 + 联邦 + 裸金属 + 中间件部署 | 单网段 200-2000 台自动纳管，OS 优化模板批量调优，中间件 docker/systemd 双模式 |
| 混合云 | ✅ 强适配 | K8s 管理 + 裸金属 + 联邦 | K8s 集群与裸金属统一纳管，联邦跨云任务转发 |
| 公有云 | 🟡 适配 | K8s 管理 + 中间件部署 | 不绑定云，云原生集成需自配；K8s 集群接入后可管理 |
| 私有云 | ✅ 强适配 | 全部能力 | 定位核心场景，数据不出机房，等保三级对照 |
| 裸金属 | ✅ 强适配 | agent + systemd 部署 + OS 优化 | agent 常驻裸金属，systemd unit 部署，OS 内核/网络/安全模板批量优化 |
| K8s | ✅ 强适配 | K8s 管理 + Operator + Helm Chart | client-go 原生管理多集群，Operator CRD 声明式部署 OpsMesh 自身，Helm Chart 一键部署 |

### 6.2 典型部署拓扑

**拓扑 A · 单网段私有化（最小形态）**
```
[控制面 + MySQL + Redis]  ←  [agent 集群（同网段）]
```
适用：单机房 200-2000 台，单租户或少量租户。

**拓扑 B · 多网段联邦（电信资源池）**
```
[控制面 A (段1)]  ←mTLS→  [控制面 B (段2)]  ←mTLS→  [控制面 C (段3)]
     ↓ gRPC                ↓ gRPC                ↓ gRPC
[agent 集群 A]         [agent 集群 B]         [agent 集群 C]
```
适用：异地多机房，每段一套控制面，联邦跨段任务转发。

**拓扑 C · 混合云 K8s + 裸金属**
```
[控制面]  →  [K8s 集群 1 (公有云)]  +  [K8s 集群 2 (私有云)]  +  [裸金属 agent 集群]
```
适用：K8s 多集群 + 裸金属中间件统一纳管，灰度发布跨集群协调。

**拓扑 D · K8s 上自部署（Helm + Operator）**
```
[K8s 集群]  →  [Helm Chart 部署 OpsMesh 控制面 + agent DaemonSet]  →  [Operator CRD 管理多实例]
```
适用：在 K8s 上运行 OpsMesh 自身，Operator 声明式管理多租户实例。

---

## 第7章 非功能需求

### 7.1 性能指标

| 指标 | 目标值 | 当前状态 | 验证方式 |
|---|---|---|---|
| 单 agent 任务并发 | 默认 4，可调 `--worker-concurrency` | ✅ 已实现 | `internal/agent/` worker 池 |
| 控制面 HTTP 请求体上限 | 1 MiB（`MaxBytesReader`） | ✅ 已实现 | `decodeJSONBody` 统一限流 |
| agent 任务执行超时 | 默认 120s，可调 `--task-timeout` | ✅ 已实现 | `exec.CommandContext` |
| 任务租约租期 | 默认 300s，可调 `--task-lease-sec` | ✅ 已实现 | 超期自动回收重调度 |
| leader 续租周期 | 默认 5s，TTL 15s，可调 | ✅ 已实现 | `leaderLoop` |
| agent 心跳间隔 | 10s | ✅ 已实现 | gRPC Heartbeat |
| 取消信号轮询间隔 | 2s | ✅ 已实现 | `cancelLoop` + PollCancels |
| agent RLIMIT_NPROC | 默认 256，可调 `--max-procs` | ✅ 已实现 | fork 炸弹防护 |
| agent RLIMIT_NOFILE | 默认 4096，可调 `--max-files` | ✅ 已实现 | fd 耗尽防护 |
| gRPC 连接复用 | 长连接池 + 淘汰重建 + 断线指标化 | ✅ 已实现 | conns + expvar `agent_grpc_conn_failures` |

### 7.2 可用性指标

| 指标 | 目标值 | 当前状态 | 验证方式 |
|---|---|---|---|
| 控制面多副本 HA | 支持，`--replicas>1` + leader 选举 | ✅ 已实现 | `leader_lease` 表 + leaderLoop |
| agent 多控制面 failover | 支持，`--control-addrs` 逗号分隔 | ✅ 已实现 | 客户端按序重连 |
| PodDisruptionBudget | minAvailable=1 | ✅ 已实现 | Helm Chart |
| 优雅退出 | SIGTERM 15s 窗口，可调 `--shutdown-timeout` | ✅ 已实现 | 信号处理 |
| HTTP/gRPC 兜底恢复 | handler panic 不拖垮控制面 | ✅ 已实现 | `recoveryMiddleware` + `grpcRecoveryInterceptor` |
| 健康检查 | /healthz + /readyz（依赖 store/redis 就绪） | ✅ 已实现 | K8s probe |
| 零依赖启动 | MemoryStore，无 MySQL/Redis | ✅ 已实现 | 默认 store=memory |

### 7.3 安全指标

| 指标 | 目标值 | 当前状态 | 落地 |
|---|---|---|---|
| 租户隔离 | 行级 + schema 级 | ✅ 已实现 | tenant_id 列 + `--multi-schema` |
| RBAC | 三表 + 种子 + JWT 双 Token | ✅ 已实现 | `sql_rbac.go` + `auth.go` |
| 通信加密 | gRPC TLS / mTLS | ✅ 已实现 | `--tls-cert/key` + `--client-ca` |
| 联邦通道 | mTLS + HMAC 签名验签 + ±5min 防重放 | ✅ 已实现 | |
| 审计留痕 | 100% | ✅ 已实现 | AuditEvent → audit_log |
| 登录防爆破 | 令牌桶 + 5 次锁 15min | ✅ 已实现 | `loginGuard` |
| 请求体限流 | 1 MiB | ✅ 已实现 | `MaxBytesReader` |
| metrics 访问控制 | CIDR 白名单 | ✅ 已实现 | `--metrics-allow-cidr` |
| shell 命令白名单 | 可配 | ✅ 已实现 | `--agent-shell-whitelist` |
| file 路径白名单 | 可配 + 路径遍历防护 | ✅ 已实现 | `--agent-file-root-whitelist` |
| SSRF 防护 | webhook URL + autoProvision CIDR | ✅ 已实现 | `ValidateWebhookURL` + `ValidateCIDR` |
| JWT 验签 | RS256 公钥验签 | ✅ 已实现 | `--jwt-public-key` |
| kubeconfig 加密 | AES-256-GCM | ✅ 已实现 | `--encryption-key` |
| TLS 证书热重载 | fsnotify 监听 + graceful reload | ✅ 已实现 | `--tls-watch` |
| Vault/KMS 密钥管理 | Env/File/Vault/Chain provider | ✅ 已实现 | `internal/secrets/` |
| CSP 收紧 | 无 unsafe-inline | ✅ 已实现 | 前端 inline 迁移到 addEventListener |
| Cookie Secure | 生产默认 true | ✅ 已实现 | `--cookie-secure` |
| 等保三级对照 | 数据本地化 + 审计 + RBAC + 加密 | ✅ 已实现 | 见 README 等保对照表 |

### 7.4 兼容性指标

| 指标 | 支持范围 | 说明 |
|---|---|---|
| 控制面 OS | Linux / Windows / macOS | 跨平台编译 |
| agent OS | Linux 正式支持 | Windows 仅可编译，无执行能力（依赖 shell/systemctl/rlimit） |
| Go 版本 | ≥ 1.26.0（主模块）/ ≥ 1.22.0（operator 子模块） | 见 `go.mod` |
| Node.js | ≥ 18（推荐 20 LTS） | 企业版前端构建 |
| MySQL | 8.x | CI 用 mysql:8 |
| Redis | 7.x | CI 用 redis:7 |
| K8s | client-go 兼容版本 | 无 kubectl 依赖 |
| 容器运行时 | Docker / containerd | Helm Chart + Dockerfile |
| 架构 | linux/amd64 + linux/arm64 | goreleaser 跨平台构建 |

### 7.5 可扩展性指标

| 指标 | 当前状态 | 说明 |
|---|---|---|
| Store 接口拆分 | ✅ 17 个领域子接口 + 编译期断言 | 消费方按需依赖，便于 mock 与替换 |
| 事件总线 | 可插拔（noop/log/kafka） | `--event-bus` |
| 日志后端 | 可切换（memory/sql/loki/es） | `--log-backend` |
| 告警通道 | 多形态（Webhook/飞书/钉钉/Slack/企业微信/邮件） | `--alert-notifier-type` + `--notify-channels-config` |
| 密钥 provider | 可插拔（Env/File/Vault/Chain） | `--secret-provider` |
| 部署形态 | 单二进制 / docker-compose / Helm / systemd / goreleaser / GitOps | 与前端版本正交 |
| 联邦 peer | 动态配置（`--federation-peers` 逗号分隔） | 跨网段规模化 |
| 多租户 schema | 动态路由（`--multi-schema` + `--schema-prefix`） | 重租户独立 schema |

---

## 第8章 路线图

> 当前版本：**v0.7.0**（2026-08-17 基线，含 MVP + 安全加固）
>
> 已实现能力以 `README.md` 功能矩阵为准；规划项以 `product-roadmap.md` 为准，所有"计划/目标"措辞均为规划意图。

### 8.1 当前版本 v0.7.0 已实现

| 类别 | 已实现能力 |
|---|---|
| 运维执行 | Shell / 服务管理 / 文件分发 / 批量下发 / 重试 + 死信 / 取消（pending 拦截 + running 强杀）/ 定时周期调度 / 作业审批 |
| 设备纳管 | 网段 TCP 发现 + 令牌闭环自动纳管 + 设备退役归档 |
| CMDB | Phase1 模型 + CRUD + SQL + 采集 + 关系图谱可视化 + 变更审批 |
| 作业编排 | DAG 引擎 + 子工作流 + 并行/串行/条件分支 + 节点级超时重试 + 执行历史回放 |
| 服务部署 | 计划 + fan-out + Reconcile + Rollback + 蓝绿/金丝雀/滚动 + 发布门禁 + 自动回滚 + 多集群联邦发布 + 灰度自适应推进 |
| K8s 管理 | 集群增删查 + 资源读写 + scale/restart/rollback + Operator CRD |
| OS 优化 | 8 类预置模板 + 在线 CRUD + 幂等 seed |
| 中间件部署 | 10+ 中间件 × docker/systemd 双模式 |
| 监控告警 | 规则引擎 + 异常检测（Z-Score + EWMA）+ 多通道 + 聚合抑制 |
| 日志检索 | 双后端 + 全文本倒排索引 + Loki/ES 适配 |
| 多租户 | 行级 + schema 级隔离 + 配额计费 |
| RBAC | 三表 + JWT 双 Token + 网关注入双路径 |
| 联邦 | mTLS + HMAC 签名验签 + 跨网段任务转发 + 联邦级发布协调 |
| 密钥管理 | Env/File/Vault/Chain + 前端 UI + kubeconfig 加密 |
| 安全加固 | 安全加固 全部落地（见 §7.3） |
| 平台基线 | 多副本 HA + agent failover + Prometheus + 审计 100% + 兜底恢复 |
| 交付物 | 单二进制 + docker-compose + Helm Chart + systemd + goreleaser + Argo CD GitOps |
| 前端 | Vue3 企业版主线 + SSE 实时推送 + 契约守护测试 |
| 代码规模 | 33 Go 包 / 161 源码文件 / 44,714 行 / 97 测试文件 / 29,818 测试行（占比 39.8%） |

### 8.2 短期规划（1-2 个月）

| 工作项 | 验收标准 | 优先级 |
|---|---|---|
| 测试覆盖率门禁 | Go 行覆盖率 ≥ 70%，关键包 ≥ 85%，CI 阻断 | 高 |
| 前端测试基线 | Vitest + jsdom，行覆盖率 ≥ 60%，CI 阻断 | 高 |
| 并发测试专项 | leader 续租 + ClaimTask 原子性 + cancelLoop 竞态用例 | 中 |
| goreleaser 完善 | 跨平台构建 + GitHub Release + checksums + SBOM | 中 |
| 文档同步 | README/DELIVERY/roadmap 与代码实际完全一致 | 中 |

### 8.3 中期规划（M3，2-3 个月）

| 工作项 | 验收标准 | 优先级 |
|---|---|---|
| 前端 Vue3 主线增强 | 设计系统固化 + 类型全覆盖 + 组件库基线 | 高 |
| protobuf 工具链 | buf + breaking 检查入 CI（注：当前 JSON codec 为正式契约，protobuf 已启用代码生成供未来迁移） | 中 |
| 等保三级审计 6 月留存 | audit_log 定期归档冷存储 + 导出接口 | 中 |
| 依赖安全自动化 | govulncheck + Dependabot + go mod verify 入 CI | 中 |
| 安全头进一步收紧 | HSTS + X-Frame-Options + X-Content-Type-Options 完整 | 低 |

### 8.4 长期规划（M4，远期）

| 工作项 | 验收标准 | 优先级 |
|---|---|---|
| K8s Operator 深化 | OpsMeshInstance CRD 声明式部署/升级/轮转/备份恢复 | 中 |
| Vault/KMS 密钥轮转 | 动态获取 + 自动轮转 + cert-manager 集成 | 中 |
| 云服务形态 | OpsMesh SaaS / Hybrid / Managed 立项 | 低 |
| 开源策略落地 | Apache 2.0 + 内核开源 + 企业版独立 | 低 |
| 蓝鲸 GSE 级联增强 | 超大规模级联独立立项（已移出 MVP，降格可选增强） | 低 |

### 8.5 演进原则

1. **收敛而非分叉**：前端 Vue3 企业版唯一主线，原生 JS 个人版已收敛移除。
2. **内核共享**：Go 内核共享同一 codebase，通过 flag 切换形态，部署形态与前端版本正交。
3. **DoD 可验收**：每个演进目标带可验收的 Definition of Done，避免"写了一整页、改没改没人能证"。
4. **规划与现状区分**：所有"计划/目标"措辞均为规划意图，已实现能力以 README 功能矩阵为准。
5. **有意为之的维持现状**：gRPC JSON codec + 版本协商是正式契约，protobuf 代码生成已启用供未来迁移，非技术债。

---

## 附录 A：参考文档

| 文档 | 说明 |
|---|---|
| `README.md` | 项目总览 + 功能矩阵 + 快速启动 + 配置参考 |
| `DELIVERY.md` | 交付说明 + 代码规模 + 验证结果 + 功能矩阵 |
| `docs/product-roadmap.md` | 产品方向与演进路线图（详细） |
| `docs/api-reference.md` | HTTP REST + gRPC API 完整参考 |
| `docs/flag-matrix.md` | 116 个 flag 全量配置矩阵 |
| `docs/deployment-guide.md` | 部署指南（控制面/agent/前端各场景） |
| `docs/sse-protocol.md` | SSE 事件流契约 |
| `docs/tech-selection.md` | 技术选型决策记录 |
| `docs/tech-debt.md` | 技术债盘点 |
| `docs/security-issues.md` | 安全议题与加固记录 |

## 附录 B：术语表

| 术语 | 说明 |
|---|---|
| 网段（segment） | OpsMesh 纳管分桶键，agent 所属网段，如 seg-a |
| 控制面（controlplane） | OpsMesh 中心服务，HTTP :8080 + gRPC :9090 + Metrics :9091 |
| agent | 部署在被纳管设备上的常驻进程，gRPC 长连接到控制面 |
| 联邦（federation） | 多控制面跨网段协作，mTLS + HMAC 签名验签 |
| 令牌闭环 | install token 一次性 + 限时 + HMAC 签名，agent 首次注册自动纳管 |
| 死信（dead_letter） | 任务失败且重试耗尽后的状态，产出 critical 告警 |
| 自研 gRPC 管控通道 | 管控通道决策：自研 gRPC（direct + proxy），蓝鲸 GSE 移出 MVP |
| DoD | Definition of Done，演进目标可验收标准 |
| 等保三级 | 信息安全等级保护三级，要求审计 6 月留存 + 数据本地化 + RBAC + 加密 |