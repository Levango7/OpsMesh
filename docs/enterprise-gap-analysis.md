# 企业版前端差距分析报告

> 生成时间：2026-09-03
> 分析范围：后端 `internal/controlplane/` 全部 HTTP 路由 vs 企业版前端 `web/enterprise/src/api/` 已接入功能域
> 任务 ID：453（Phase 2 分析）
> **完成状态：✅ 全部 13 个子域已于 2026-09-03 补齐完成（P0: cc3f826 / P1: dee012e / P2: 2f41aac / P3: 2fc3bba）**

## 1. 总览统计

| 指标 | 数值 | 说明 |
|------|------|------|
| 后端路由总数 | 183 | 含 mux.HandleFunc/mux.Handle 注册的路径前缀及各 RegisterRoutes（cmdb/log/deploy/orchestration）注册路由，展开子路径端点后估算 |
| 前端已接入子域数 | 40 | 原有 27 + 本轮新增 13（2026-09-03 全部补齐） |
| 未接入子域数 | **0** | ✅ 2026-09-03 全部补齐完成（原 13 个差距已全部消除） |
| 排除路由数 | 8 | healthz/readyz/metrics/install.sh/bin/opsmesh-agent/assets/dashboard/gw 数据面（不需前端接入） |

### 1.1 前端已接入子域清单（27 个）

| 子域 | API 模块 | 视图组件 | 路由路径 |
|------|----------|----------|----------|
| 概览 | — | OverviewView.vue | /overview |
| 设备管理 | device.js | DevicesView.vue + DeviceDetailView.vue | /devices, /devices/:id |
| 任务（基础） | task.js | TasksView.vue | /tasks |
| 告警（活跃） | alert.js | AlertsView.vue | /alerts |
| OS 优化 | os-optimize.js | OSOptimizeView.vue | /os-optimize |
| 中间件部署 | middleware.js | MiddlewareDeployView.vue | /middleware |
| K8s 管理 | k8s.js | K8sManageView.vue | /k8s |
| CMDB（基础查询） | cmdb.js | CMDBView.vue | /cmdb |
| 作业编排 | workflow.js | WorkflowsView.vue | /workflows |
| 部署 | deploy.js | DeploysView.vue | /deploys |
| 日志检索 | log.js | LogsView.vue | /logs |
| 认证/用户/角色 | auth.js | LoginView/RegisterView/ChangePassword/UsersView/RolesView/PermissionsView | /login, /register, /change-password, /users, /roles, /permissions |
| 密钥管理 | secrets.js | secrets/SecretsView.vue | /secrets |
| 插件市场 | plugin.js | PluginView.vue | /plugins |
| 定时任务 | schedule.js | SchedulesView.vue | /schedules |
| 自动化规则 | automation.js | AutomationView.vue | /automation |
| Webhook | webhook.js | WebhooksView.vue | /webhooks |
| 自定义脚本 | script.js | ScriptsView.vue | /scripts |
| 工单 | ticket.js | TicketsView.vue | /tickets |
| SLO | slo.js | SLOsView.vue | /slos |
| 流量策略 | traffic.js | TrafficPoliciesView.vue | /traffic |
| GPU 算力 | gpu.js | GPUView.vue | /gpu |
| ChatOps | bot.js | BotView.vue | /bot |
| Runbook | runbook.js | RunbookView.vue | /runbooks |
| Incident | incident.js | IncidentView.vue | /incidents |
| 自动扩缩容 | autoscaler.js | AutoscalerView.vue | /autoscaler |
| 自助门户 | portal.js | PortalView.vue | /portal |
| CI/CD 流水线 | pipeline.js | PipelineView.vue | /pipeline |
| ArgoCD | argocd.js | ArgoCDView.vue | /argocd |
| 安全合规 | compliance.js | ComplianceView.vue | /compliance |
| HA 管理 | ha.js | HAView.vue | /ha |
| 灾备备份 | backup.js | BackupsView.vue | /backups |
| 租户配额 | quota.js | QuotasView.vue | /quotas |
| 租户管理 | tenant.js | TenantsView.vue | /tenants |
| 计费 | billing.js | BillingView.vue | /billing |
| API Key | apikeys.js | APIKeysView.vue | /apikeys |
| API 网关 | gateway.js | GatewayRoutesView.vue | /gateway-routes |
| 审计事件 | audit.js | AuditEventsView.vue | /audit-events |
| 通知渠道 | notify.js | NotifyChannelsView.vue | /notify-channels |
| SSE 实时推送 | sse.js | （App.vue 内嵌） | — |

## 2. 已补齐子域详细清单（13 个，2026-09-03 全部完成）

> ✅ 本节原列出的 13 个未接入子域已全部补齐完成。各子域完成状态与引入 commit 见下文小节标题。

### 2.1 【P0】告警规则管理（alert-rules）✅ 已完成（commit cc3f826，2026-09-03）

**优先级**：P0 - 核心运维功能
**功能描述**：告警规则的创建、编辑、删除与多条件引擎管理，包含旧版告警规则、M2 多条件引擎规则、静默规则三类。当前前端 AlertsView.vue 仅接入活跃告警列表的 ack/silence 操作，未接入规则 CRUD，运维人员无法通过界面配置告警阈值/条件/通知策略。

**后端路由**：
```
GET    /api/v1/alert-rules              → {rules: [AlertRule]} 列表
POST   /api/v1/alert-rules              → AlertRule（201）创建
GET    /api/v1/alert-rules/{id}         → AlertRule 详情
PUT    /api/v1/alert-rules/{id}         → AlertRule 更新
DELETE /api/v1/alert-rules/{id}         → {status: "deleted"} 删除

GET    /api/v1/alert-rules-engine       → {rules: [EngineRule]} M2 多条件引擎列表
POST   /api/v1/alert-rules-engine       → EngineRule（201）创建多条件规则
GET    /api/v1/alert-rules-engine/{id}  → EngineRule 详情
PUT    /api/v1/alert-rules-engine/{id}  → EngineRule 更新
DELETE /api/v1/alert-rules-engine/{id}  → {status: "deleted"} 删除

GET    /api/v1/alert-silences           → {silences: [Silence]} 静默规则列表
POST   /api/v1/alert-silences           → Silence（201）创建静默规则
DELETE /api/v1/alert-silences/{id}      → {status: "deleted"} 删除静默规则
```

**响应结构**：
- `AlertRule {id, name, metric, threshold, operator, deviceID, severity, enabled, createdAt, updatedAt}`
- `EngineRule {id, name, conditions: [{metric, operator, threshold, window}], logic: "AND|OR", severity, notifyChannelIDs, enabled, createdAt}`
- `Silence {id, matcher: {alertName, deviceID}, startsAt, endsAt, createdBy, createdAt}`

**需要新建的前端文件**：
- `web/enterprise/src/api/alert-rules.js`（API 模块：旧版规则 CRUD）
- `web/enterprise/src/api/alert-rules-engine.js`（API 模块：M2 多条件引擎 CRUD）
- `web/enterprise/src/api/alert-silences.js`（API 模块：静默规则 CRUD）
- `web/enterprise/src/stores/alert-rules.js`（Pinia store）
- `web/enterprise/src/views/AlertRulesView.vue`（视图组件：规则管理）
- `web/enterprise/src/views/AlertSilencesView.vue`（视图组件：静默规则管理）
- `router/index.js` 新增路由 `/alert-rules`、`/alert-silences`
- `i18n locales` 新增翻译键 `nav.alertRules`、`nav.alertSilences` 等

---

### 2.2 【P0】批量运维（batch）✅ 已完成（commit cc3f826，2026-09-03）

**优先级**：P0 - 核心运维功能
**功能描述**：M5 增强版批量运维，支持对多设备下发同一任务并聚合状态查询。与旧版 `/api/v1/tasks/batch`（仅返回 created IDs）不同，M5 版返回 batchID + 每设备任务详情 + 状态聚合，支持按批次追踪执行进度。当前前端 TasksView.vue 仅支持单任务下发，无法批量操作。

**后端路由**：
```
POST /api/v1/tasks/batch-exec           → {batchID, tasks: [{deviceID, taskID, status, error}]}（201）
  请求体：{deviceIDs: [], taskType, command, timeoutSec}
POST /api/v1/tasks/batch                → {created: [taskID]}（旧版批量下发）
GET  /api/v1/tasks/batch/{batchID}      → {batchID, taskType, command, status, tasks: [{deviceID, taskID, status, error}], createdAt, createdBy}
```

**响应结构**：
- `BatchTask {batchID, tenantID, taskType, command, timeout, createdAt, createdBy, tasks: [BatchTaskItem]}`
- `BatchTaskItem {deviceID, taskID, status: "pending|running|done|failed|cancelled", error}`

**需要新建的前端文件**：
- `web/enterprise/src/api/batch.js`（API 模块）
- `web/enterprise/src/stores/batch.js`（Pinia store）
- `web/enterprise/src/views/BatchExecView.vue`（视图组件：批量执行 + 状态追踪）
- `router/index.js` 新增路由 `/batch`
- `i18n locales` 新增翻译键 `nav.batch` 等

---

### 2.3 【P0】灰度发布（canary）✅ 已完成（commit cc3f826，2026-09-03）

**优先级**：P0 - 核心运维功能
**功能描述**：M5 灰度发布支持按比例/按分组/按标签分阶段执行，配合灰度增强 API（traffic-split/metrics）实现流量切分与指标观测。当前前端无灰度发布能力，运维人员只能全量下发，无法渐进式发布。

**后端路由**：
```
POST /api/v1/tasks/canary               → {canaryID, phases: [CanaryPhase]}（201）创建灰度发布
  请求体：{taskType, command, strategy: "percentage|group|label", percentage, groups, labels, deviceIDs, timeoutSec}
GET  /api/v1/tasks/canary/{canaryID}    → CanaryRelease 灰度状态查询
POST /api/v1/tasks/canary/{canaryID}/advance → CanaryRelease 推进下一阶段

GET  /api/v1/canary/{id}/traffic-split  → {split: {canary, stable}} 流量切分比例
GET  /api/v1/canary/{id}/metrics        → {canaryMetrics, stableMetrics} 灰度 vs 稳定版指标对比
```

**响应结构**：
- `CanaryRelease {canaryID, tenantID, taskType, command, strategy, percentage, groups, labels, createdAt, createdBy, phases: [CanaryPhase]}`
- `CanaryPhase {phase, deviceIDs, status: "pending|running|done|failed|aborted", tasks: [BatchTaskItem], startedAt, finishedAt}`

**需要新建的前端文件**：
- `web/enterprise/src/api/canary.js`（API 模块）
- `web/enterprise/src/stores/canary.js`（Pinia store）
- `web/enterprise/src/views/CanaryReleaseView.vue`（视图组件：灰度发布 + 阶段推进 + 指标对比）
- `router/index.js` 新增路由 `/canary`
- `i18n locales` 新增翻译键 `nav.canary` 等

---

### 2.4 【P1】平台配置（platform）✅ 已完成（commit dee012e，2026-09-03）

**优先级**：P1 - 平台管理功能
**功能描述**：平台级配置管理、健康检查与指标汇总。运维人员需查看平台版本/构建信息/功能开关、组件健康状态、资源总量统计。当前前端无平台管理入口。

**后端路由**：
```
GET  /api/v1/platform/config            → PlatformConfig 平台配置
PUT  /api/v1/platform/config            → PlatformConfig 更新配置
  请求体：{defaultTenant, maxTenants, enableMarketplace, enableBilling}
GET  /api/v1/platform/health            → PlatformHealth 平台健康检查
GET  /api/v1/platform/metrics           → PlatformMetrics 平台指标汇总
```

**响应结构**：
- `PlatformConfig {version, buildTime, goVersion, defaultTenant, maxTenants, enableMarketplace, enableBilling, updatedAt}`
- `PlatformHealth {status: "ok|degraded|down", components: {name: status}, timestamp}`
- `PlatformMetrics {tenants, devices, tasks, alerts, apiKeys, plugins, subscriptions, invoices}`

**需要新建的前端文件**：
- `web/enterprise/src/api/platform.js`（API 模块）
- `web/enterprise/src/stores/platform.js`（Pinia store）
- `web/enterprise/src/views/PlatformConfigView.vue`（视图组件：配置 + 健康 + 指标）
- `router/index.js` 新增路由 `/platform`
- `i18n locales` 新增翻译键 `nav.platform` 等

---

### 2.5 【P1】控制面联邦（federation）✅ 已完成（commit dee012e，2026-09-03）

**优先级**：P1 - 平台管理功能
**功能描述**：跨网段/跨 IDC/跨 K8s 集群的多控制面联邦管理。运维人员可查看联邦 peer 列表与在线状态、聚合查看所有 peer 的设备视图、把任务转发到指定 peer 的 agent。当前前端无联邦管理能力。

**后端路由**：
```
GET  /api/v1/federation/peers           → {peers: [PeerStatus]} peer 列表与在线状态
POST /api/v1/federation/forward/task    → {taskID, peerURL, status} 转发任务到指定 peer
  请求体：{peerURL, taskType, command, deviceID, timeoutSec}
GET  /api/v1/federation/devices         → {devices: [Device], peers: [{url, online, deviceCount}]} 聚合设备视图
```

**响应结构**：
- `PeerStatus {url, online, lastCheckAt, latencyMs}`
- 转发响应 `{taskID, peerURL, status: "forwarded|failed", error}`

**需要新建的前端文件**：
- `web/enterprise/src/api/federation.js`（API 模块）
- `web/enterprise/src/stores/federation.js`（Pinia store）
- `web/enterprise/src/views/FederationView.vue`（视图组件：peer 管理 + 设备聚合 + 任务转发）
- `router/index.js` 新增路由 `/federation`
- `i18n locales` 新增翻译键 `nav.federation` 等

---

### 2.6 【P1】多集群联邦部署（deploys-federation）✅ 已完成（commit dee012e，2026-09-03）

**优先级**：P1 - 平台管理功能
**功能描述**：跨 K8s 集群联邦部署，把同一部署任务下发到多个集群并聚合状态。与控制面联邦（peer 管理）不同，本子域聚焦部署域的跨集群能力。当前前端 DeploysView.vue 仅支持单集群部署。

**后端路由**：
```
GET  /api/v1/deploys/federation         → {deploys: [FederationDeploy]} 联邦部署列表
POST /api/v1/deploys/federation         → FederationDeploy（201）创建联邦部署
  请求体：{name, clusters: [clusterID], template, params}
GET  /api/v1/deploys/federation/{id}    → FederationDeploy 详情
```

**响应结构**：
- `FederationDeploy {id, name, clusters: [{clusterID, status, error}], template, params, createdAt, createdBy, status}`

**需要新建的前端文件**：
- `web/enterprise/src/api/deploys-federation.js`（API 模块）
- `web/enterprise/src/stores/deploys-federation.js`（Pinia store）
- `web/enterprise/src/views/FederationDeploysView.vue`（视图组件）
- `router/index.js` 新增路由 `/deploys-federation`
- `i18n locales` 新增翻译键 `nav.federationDeploys` 等

---

### 2.7 【P1】配置热推送（config）✅ 已完成（commit dee012e，2026-09-03）

**优先级**：P1 - 平台管理功能
**功能描述**：配置热推送到指定设备、灰度配置发布、配置版本历史查询。运维人员可不重启服务推送配置变更，支持灰度发布配置并查看版本回滚。当前前端无配置管理入口。

**后端路由**：
```
POST /api/v1/config/hotpush             → {taskID, configVersion}（201）热推送配置
  请求体：{agentID, key, value, path, format, description}
POST /api/v1/config/canary              → {canaryID, versions} 灰度配置发布
  请求体：{agentIDs, key, value, path, format, strategy, percentage}
GET  /api/v1/config/versions            → {versions: [ConfigVersion]} 配置版本历史
  查询参数：key, agentID, limit
```

**响应结构**：
- `ConfigVersion {key, version, value, agentID, updatedAt, updatedBy}`
- 热推送响应 `{taskID, configVersion: ConfigVersion}`

**需要新建的前端文件**：
- `web/enterprise/src/api/config.js`（API 模块）
- `web/enterprise/src/stores/config.js`（Pinia store）
- `web/enterprise/src/views/ConfigHotpushView.vue`（视图组件：热推送 + 灰度 + 版本历史）
- `router/index.js` 新增路由 `/config`
- `i18n locales` 新增翻译键 `nav.config` 等

---

### 2.8 【P1】CMDB 高级（cmdb-advanced）✅ 已完成（commit dee012e，2026-09-03）

**优先级**：P1 - 平台管理功能
**功能描述**：CMDB 采集自动化、变更审批流、关系拓扑管理、CI 导入导出、属性模板 CRUD、CI 详情 CRUD。当前前端 CMDBView.vue 仅接入 types/ci 查询和 graph 关系图谱，未接入采集触发、变更审批、关系 CRUD、导入导出、属性模板管理、CI 编辑/删除/审批等高级能力。

**后端路由**：
```
POST /api/v1/cmdb/collect               → {collected, failed}（201）手动触发全量采集

GET  /api/v1/cmdb/changes               → {changes: [CMDBChange]} 变更申请列表
POST /api/v1/cmdb/changes               → CMDBChange（201）提交变更申请
GET  /api/v1/cmdb/changes/{id}          → CMDBChange 详情
POST /api/v1/cmdb/changes/{id}/approve  → CMDBChange 审批通过
POST /api/v1/cmdb/changes/{id}/reject   → CMDBChange 审批拒绝

GET  /api/v1/cmdb/relations             → {relations: [Relation]} 关系列表
POST /api/v1/cmdb/relations             → Relation（201）创建关系

GET  /api/v1/cmdb/ci/export             → [CiItem] 导出 CI
POST /api/v1/cmdb/ci/import             → {imported, failed} 导入 CI
GET  /api/v1/cmdb/ci/pending            → {items: [CiItem]} 待审批 CI 列表
GET  /api/v1/cmdb/ci/{id}/relations     → {relations: [Relation]} CI 关系
PUT  /api/v1/cmdb/ci/{id}               → CiItem 更新 CI
DELETE /api/v1/cmdb/ci/{id}             → {status: "deleted"} 删除 CI
POST /api/v1/cmdb/ci/{id}/approve       → CiItem 审批通过
POST /api/v1/cmdb/ci/{id}/reject        → CiItem 审批拒绝

GET  /api/v1/cmdb/attr-templates        → {templates: [AttrTemplate]} 属性模板列表
POST /api/v1/cmdb/attr-templates        → AttrTemplate（201）创建
GET  /api/v1/cmdb/attr-templates/{id}   → AttrTemplate 详情
PUT  /api/v1/cmdb/attr-templates/{id}   → AttrTemplate 更新
DELETE /api/v1/cmdb/attr-templates/{id} → {status: "deleted"} 删除
```

**响应结构**：
- `CMDBChange {id, ciID, changeType: "create|update|delete", before, after, status: "pending|approved|rejected", requester, approver, createdAt, approvedAt}`
- `Relation {id, sourceCIID, targetCIID, relationType, sourceName, targetName, targetType}`
- `AttrTemplate {id, name, type, category, required, defaultValue, options, createdAt}`

**需要新建的前端文件**：
- `web/enterprise/src/api/cmdb-advanced.js`（API 模块：采集 + 变更审批 + 关系 + 导入导出）
- `web/enterprise/src/api/cmdb-attr-templates.js`（API 模块：属性模板 CRUD）
- `web/enterprise/src/stores/cmdb-advanced.js`（Pinia store）
- `web/enterprise/src/views/CMDBChangesView.vue`（视图组件：变更审批）
- `web/enterprise/src/views/CMDBAttrTemplatesView.vue`（视图组件：属性模板管理）
- `web/enterprise/src/views/CMDBCollectView.vue`（视图组件：采集管理）
- 扩展 `CMDBView.vue` 增加导入导出/CI 编辑/关系管理面板
- `router/index.js` 新增路由 `/cmdb-changes`、`/cmdb-attr-templates`、`/cmdb-collect`
- `i18n locales` 新增翻译键

---

### 2.9 【P2】审批流（approval）✅ 已完成（commit 2f41aac，2026-09-03）

**优先级**：P2 - 辅助功能
**功能描述**：M5 审批流管理，包含审批流定义（多级审批节点）、审批请求提交、审批操作（通过/拒绝/取消）、待我审批列表、审批历史。当前前端无审批流管理入口，运维变更缺少审批管控。

**后端路由**：
```
GET  /api/v1/approval/flows             → {flows: [ApprovalFlow], total} 审批流列表
POST /api/v1/approval/flows             → ApprovalFlow（201）创建审批流
GET  /api/v1/approval/flows/{id}        → ApprovalFlow 详情
PUT  /api/v1/approval/flows/{id}        → ApprovalFlow 更新
DELETE /api/v1/approval/flows/{id}      → {status: "deleted"} 删除

GET  /api/v1/approval/requests          → {requests: [ApprovalRequest]} 审批请求列表（?status=pending）
POST /api/v1/approval/requests          → ApprovalRequest（201）提交审批请求
GET  /api/v1/approval/requests/{id}     → ApprovalRequest 详情
POST /api/v1/approval/requests/{id}/approve → ApprovalRequest 审批通过
POST /api/v1/approval/requests/{id}/reject  → ApprovalRequest 审批拒绝
POST /api/v1/approval/requests/{id}/cancel  → ApprovalRequest 取消
GET  /api/v1/approval/requests/{id}/history → {history: [ApprovalHistory]} 审批历史

GET  /api/v1/approval/pending           → {requests: [ApprovalRequest]} 待我审批列表
```

**响应结构**：
- `ApprovalFlow {id, name, description, steps: [{name, approvers, order}], tenantID, createdAt, updatedAt}`
- `ApprovalRequest {id, flowID, flowName, requester, resourceType, resourceID, status: "pending|approved|rejected|cancelled", currentStep, createdAt, updatedAt}`
- `ApprovalHistory {id, requestID, step, approver, action: "approve|reject", comment, timestamp}`

**需要新建的前端文件**：
- `web/enterprise/src/api/approval.js`（API 模块）
- `web/enterprise/src/stores/approval.js`（Pinia store）
- `web/enterprise/src/views/ApprovalFlowsView.vue`（视图组件：审批流定义）
- `web/enterprise/src/views/ApprovalRequestsView.vue`（视图组件：审批请求 + 待我审批）
- `router/index.js` 新增路由 `/approval-flows`、`/approval-requests`
- `i18n locales` 新增翻译键 `nav.approvalFlows`、`nav.approvalRequests` 等

---

### 2.10 【P2】网络拓扑诊断（network）✅ 已完成（commit 2f41aac，2026-09-03）

**优先级**：P2 - 辅助功能
**功能描述**：M6 网络拓扑发现、网络诊断工具（ping/traceroute/tcping/nslookup/curl）、批量连通性检测、网络设备管理（CRUD + 指标 + 配置下发）、网络发现。当前前端无网络管理入口，运维人员无法可视化网络拓扑或诊断网络问题。

**后端路由**：
```
GET  /api/v1/network/topology           → NetworkTopology 拓扑图（?refresh=true 强制刷新）
GET  /api/v1/network/topology/cache     → NetworkTopology 缓存拓扑（不触发探测）
POST /api/v1/network/diagnose           → {taskID, status} 发起诊断任务
  请求体：{agentID, command: "ping|traceroute|tcping|nslookup|curl", target, count, timeout}
GET  /api/v1/network/diagnose/{taskID}  → {taskID, status, output, finishedAt} 诊断结果
POST /api/v1/network/connectivity       → {results: [{source, target, reachable, latencyMs, loss}]} 批量连通性检测
  请求体：{targets: [{source, target}], timeout}

GET  /api/v1/network/devices            → {devices: [NetworkDevice]} 网络设备列表
POST /api/v1/network/devices            → NetworkDevice（201）添加网络设备
GET  /api/v1/network/devices/{id}       → NetworkDevice 详情
DELETE /api/v1/network/devices/{id}     → {status: "deleted"} 删除
GET  /api/v1/network/devices/{id}/metrics → {metrics: [{timestamp, bandwidth, throughput, errors}]} 设备指标
POST /api/v1/network/devices/{id}/config → {taskID} 配置下发
  请求体：{config, format}
POST /api/v1/network/discover           → {discovered: [NetworkDevice]} 网络发现
  请求体：{segment, agentID}
```

**响应结构**：
- `NetworkTopology {nodes: [NetworkNode], edges: [NetworkEdge], generatedAt, tenantID}`
- `NetworkNode {id, hostname, ip, status: "online|offline", os, segment}`
- `NetworkEdge {source, target, latencyMs, loss}`
- `NetworkDevice {id, name, type, ip, segment, vendor, model, status, createdAt}`

**需要新建的前端文件**：
- `web/enterprise/src/api/network.js`（API 模块）
- `web/enterprise/src/stores/network.js`（Pinia store）
- `web/enterprise/src/views/NetworkTopologyView.vue`（视图组件：拓扑图 SVG 渲染）
- `web/enterprise/src/views/NetworkDiagnoseView.vue`（视图组件：诊断工具）
- `web/enterprise/src/views/NetworkDevicesView.vue`（视图组件：网络设备管理）
- `router/index.js` 新增路由 `/network-topology`、`/network-diagnose`、`/network-devices`
- `i18n locales` 新增翻译键

---

### 2.11 【P2】审计检索（audits）✅ 已完成（commit 2f41aac，2026-09-03）

**优先级**：P2 - 辅助功能
**功能描述**：旧版审计检索 API（`/api/v1/audits`），与已接入的 `/api/v1/audit/events` + `/api/v1/audit/export` 不同。旧版审计检索提供聚合查询能力，可能返回不同格式的审计数据。当前前端 AuditEventsView.vue 仅接入新版 audit/events，未接入旧版 audits 检索。

**后端路由**：
```
GET  /api/v1/audits                     → {audits: [AuditEntry], total} 审计检索
  查询参数：action, user, from, to, limit, offset
```

**响应结构**：
- `AuditEntry {id, action, user, resource, tenantID, ip, userAgent, timestamp, details}`

**需要新建的前端文件**：
- 扩展 `web/enterprise/src/api/audit.js` 增加 `getAudits()` 函数
- 扩展 `web/enterprise/src/views/AuditEventsView.vue` 增加旧版审计检索面板
- 或新建 `web/enterprise/src/views/AuditsView.vue`（独立视图）
- `router/index.js` 新增路由 `/audits`（如独立视图）
- `i18n locales` 新增翻译键

---

### 2.12 【P2】自动纳管（provision）✅ 已完成（commit 2f41aac，2026-09-03）

**优先级**：P2 - 辅助功能
**功能描述**：自动纳管触发 API，配合 `--discover` + `--auto-provision` 启动参数实现周期扫描网段并推送 agent。运维人员可手动触发自动纳管流程。当前前端无自动纳管入口。

**后端路由**：
```
POST /api/v1/provision/auto             → {discovered, provisioned, failed}（201）手动触发自动纳管
  请求体：{segment, agentVersion}
```

**响应结构**：
- 响应 `{discovered: int, provisioned: int, failed: int, devices: [{ip, hostname, status}]}`

**需要新建的前端文件**：
- `web/enterprise/src/api/provision.js`（API 模块）
- `web/enterprise/src/stores/provision.js`（Pinia store）
- `web/enterprise/src/views/AutoProvisionView.vue`（视图组件：自动纳管触发 + 结果展示）
- `router/index.js` 新增路由 `/auto-provision`
- `i18n locales` 新增翻译键 `nav.autoProvision` 等

---

### 2.13 【P3】Helm 应用商店（helm）✅ 已完成（commit 2fc3bba，2026-09-03）

**优先级**：P3 - 生态功能
**功能描述**：M3 Helm 应用商店，包含仓库管理（添加/删除/列表）、Chart 搜索、Release 管理（安装/升级/回滚/卸载/历史）、预置应用目录。运维人员可通过界面一键部署 Helm Chart 应用。当前前端无 Helm 应用商店入口。

**后端路由**：
```
GET  /api/v1/helm/repos                 → {repos: [HelmRepo]} 仓库列表
POST /api/v1/helm/repos                 → HelmRepo（201）添加仓库
  请求体：{name, url, username, password}
DELETE /api/v1/helm/repos/{name}        → {status: "deleted"} 删除仓库
GET  /api/v1/helm/repos/{name}/charts   → {charts: [Chart]} 仓库 Chart 列表
GET  /api/v1/helm/charts/search?q=xxx   → {charts: [Chart]} 搜索 Chart

GET  /api/v1/helm/releases              → {releases: [HelmRelease]} Release 列表
POST /api/v1/helm/releases              → HelmRelease（201）安装 Release
  请求体：{name, chart, namespace, values, repo}
PUT  /api/v1/helm/releases/{name}       → HelmRelease 升级 Release
DELETE /api/v1/helm/releases/{name}     → {status: "deleted"} 卸载 Release
POST /api/v1/helm/releases/{name}/rollback → HelmRelease 回滚
  请求体：{revision}
GET  /api/v1/helm/releases/{name}/history → {history: [ReleaseHistory]} Release 历史

GET  /api/v1/helm/catalog               → {categories: [CatalogCategory]} 预置应用目录
```

**响应结构**：
- `HelmRepo {name, url, username, addedAt}`
- `Chart {name, repo, version, description, appVersion, icon, keywords, home}`
- `HelmRelease {name, namespace, chart, chartVersion, status: "deployed|failed|pending", revision, updatedAt}`
- `ReleaseHistory {revision, chart, chartVersion, status, updatedAt}`
- `CatalogCategory {name, description, charts: [Chart]}`

**需要新建的前端文件**：
- `web/enterprise/src/api/helm.js`（API 模块）
- `web/enterprise/src/stores/helm.js`（Pinia store）
- `web/enterprise/src/views/HelmReposView.vue`（视图组件：仓库管理）
- `web/enterprise/src/views/HelmCatalogView.vue`（视图组件：应用目录 + Chart 搜索）
- `web/enterprise/src/views/HelmReleasesView.vue`（视图组件：Release 管理 + 历史 + 回滚）
- `router/index.js` 新增路由 `/helm-repos`、`/helm-catalog`、`/helm-releases`
- `i18n locales` 新增翻译键 `nav.helmRepos`、`nav.helmCatalog`、`nav.helmReleases` 等

## 3. 优先级排序汇总

> ✅ 全部 13 个子域已于 2026-09-03 按下述优先级全部补齐完成。

### 3.1 P0 - 核心运维功能（3 个）✅ 已完成（commit cc3f826）

| 序号 | 子域 | 后端路由数 | 业务价值 |
|------|------|-----------|----------|
| 1 | 告警规则管理 | 11 | 告警配置是运维核心能力，无规则管理则告警系统无法配置阈值/条件 |
| 2 | 批量运维 | 3 | 多设备批量操作是运维效率关键，单任务下发无法满足规模化管理 |
| 3 | 灰度发布 | 5 | 渐进式发布降低变更风险，全量下发缺乏回滚缓冲 |

### 3.2 P1 - 平台管理功能（5 个）✅ 已完成（commit dee012e）

| 序号 | 子域 | 后端路由数 | 业务价值 |
|------|------|-----------|----------|
| 4 | 平台配置 | 3 | 平台级配置/健康/指标是管理员必备能力 |
| 5 | 控制面联邦 | 3 | 跨网段/跨 IDC 场景下的统一运维视图 |
| 6 | 多集群联邦部署 | 3 | 跨 K8s 集群部署是多云场景核心能力 |
| 7 | 配置热推送 | 3 | 配置变更不重启服务，提升运维敏捷性 |
| 8 | CMDB 高级 | 17 | CMDB 采集/审批/关系/导入导出是资产管理完整能力 |

### 3.3 P2 - 辅助功能（4 个）✅ 已完成（commit 2f41aac）

| 序号 | 子域 | 后端路由数 | 业务价值 |
|------|------|-----------|----------|
| 9 | 审批流 | 11 | 运维变更审批管控，合规要求 |
| 10 | 网络拓扑诊断 | 11 | 网络可视化与诊断，辅助排障 |
| 11 | 审计检索 | 1 | 旧版审计检索兼容，补充审计查询能力 |
| 12 | 自动纳管 | 1 | 自动化设备纳管，减少人工操作 |

### 3.4 P3 - 生态功能（1 个）✅ 已完成（commit 2fc3bba）

| 序号 | 子域 | 后端路由数 | 业务价值 |
|------|------|-----------|----------|
| 13 | Helm 应用商店 | 11 | 应用商店生态能力，一键部署 Helm Chart |

## 4. 建议的实施批次

> ✅ 以下 4 个批次已于 2026-09-03 全部按计划完成。

### 4.1 第一批（P0，2 周）- 核心运维能力补齐 ✅ 已完成（commit cc3f826，2026-09-03）

**目标**：补齐运维核心能力，使前端覆盖告警配置、批量操作、灰度发布三大核心场景。

| 子域 | 预估工时 | 关键交付物 |
|------|----------|-----------|
| 告警规则管理 | 5 人日 | alert-rules.js + AlertRulesView.vue + AlertSilencesView.vue |
| 批量运维 | 3 人日 | batch.js + BatchExecView.vue |
| 灰度发布 | 4 人日 | canary.js + CanaryReleaseView.vue（含阶段推进 + 指标对比） |

**验收标准**：
- 告警规则 CRUD 全流程可用，支持多条件引擎规则
- 批量执行可选择多设备下发，状态聚合展示
- 灰度发布支持按比例/分组/标签策略，可手动推进阶段

### 4.2 第二批（P1，3 周）- 平台管理能力补齐 ✅ 已完成（commit dee012e，2026-09-03）

**目标**：补齐平台管理能力，覆盖平台配置、联邦、配置热推送、CMDB 高级功能。

| 子域 | 预估工时 | 关键交付物 |
|------|----------|-----------|
| 平台配置 | 2 人日 | platform.js + PlatformConfigView.vue |
| 控制面联邦 | 3 人日 | federation.js + FederationView.vue |
| 多集群联邦部署 | 3 人日 | deploys-federation.js + FederationDeploysView.vue |
| 配置热推送 | 3 人日 | config.js + ConfigHotpushView.vue |
| CMDB 高级 | 6 人日 | cmdb-advanced.js + 3 个视图组件 + CMDBView.vue 扩展 |

**验收标准**：
- 平台配置可查看/修改，健康检查与指标汇总展示
- 联邦 peer 管理可用，任务可转发，设备视图可聚合
- 跨集群联邦部署可创建并聚合状态
- 配置热推送可下发到指定设备，版本历史可查询
- CMDB 采集可手动触发，变更审批全流程可用，关系/导入导出/属性模板管理可用

### 4.3 第三批（P2，2 周）- 辅助功能补齐 ✅ 已完成（commit 2f41aac，2026-09-03）

**目标**：补齐审批流、网络管理、审计检索、自动纳管等辅助功能。

| 子域 | 预估工时 | 关键交付物 |
|------|----------|-----------|
| 审批流 | 4 人日 | approval.js + ApprovalFlowsView.vue + ApprovalRequestsView.vue |
| 网络拓扑诊断 | 5 人日 | network.js + 3 个视图组件（拓扑 SVG 渲染 + 诊断 + 设备管理） |
| 审计检索 | 1 人日 | 扩展 audit.js + AuditEventsView.vue |
| 自动纳管 | 1 人日 | provision.js + AutoProvisionView.vue |

**验收标准**：
- 审批流定义/请求/审批/历史全流程可用
- 网络拓扑可可视化渲染，诊断工具可执行 ping/traceroute 等
- 旧版审计检索可查询
- 自动纳管可手动触发并展示结果

### 4.4 第四批（P3，1 周）- 生态功能补齐 ✅ 已完成（commit 2fc3bba，2026-09-03）

**目标**：补齐 Helm 应用商店生态能力。

| 子域 | 预估工时 | 关键交付物 |
|------|----------|-----------|
| Helm 应用商店 | 5 人日 | helm.js + 3 个视图组件（仓库 + 目录 + Release 管理） |

**验收标准**：
- Helm 仓库可添加/删除/列表
- Chart 可搜索/查看详情
- Release 可安装/升级/回滚/卸载，历史可查询
- 预置应用目录可浏览

## 5. 实施总览

> ✅ 全部 4 个批次已于 2026-09-03 全部完成，13 个子域、45 人日工作量在 1 天内集中交付。

| 批次 | 优先级 | 子域数 | 预估工时 | 周期 | 完成状态 | 完成 commit |
|------|--------|--------|----------|------|----------|-------------|
| 第一批 | P0 | 3 | 12 人日 | 2 周 | ✅ 已完成 | cc3f826 |
| 第二批 | P1 | 5 | 17 人日 | 3 周 | ✅ 已完成 | dee012e |
| 第三批 | P2 | 4 | 11 人日 | 2 周 | ✅ 已完成 | 2f41aac |
| 第四批 | P3 | 1 | 5 人日 | 1 周 | ✅ 已完成 | 2fc3bba |
| **合计** | — | **13** | **45 人日** | **8 周** | **✅ 全部完成** | cc3f826 / dee012e / 2f41aac / 2fc3bba |

## 6. 风险与注意事项

### 6.1 技术风险

1. **CMDB 高级子域复杂度高**：涉及 17 个后端路由，需拆分为多个视图组件，建议优先实施采集 + 变更审批，关系/导入导出/属性模板可延后。
2. **网络拓扑 SVG 渲染**：需前端实现拓扑图可视化（节点 + 边 + 延迟标注），可考虑引入图可视化库（如 vis-network/d3-force）。
3. **灰度发布阶段推进**：需实现阶段状态机 UI（pending → running → done/failed），配合指标对比图表。
4. **Helm CLI 依赖**：后端 Helm API 在 helm CLI 不存在时返回 503，前端需处理 503 错误并提示安装 helm。

### 6.2 兼容性注意

1. **告警规则双版本**：旧版 `/api/v1/alert-rules` 与 M2 多条件引擎 `/api/v1/alert-rules-engine` 并存，前端需明确区分，建议统一在 AlertRulesView.vue 中分 Tab 展示。
2. **审计检索双版本**：旧版 `/api/v1/audits` 与新版 `/api/v1/audit/events` 并存，建议在 AuditEventsView.vue 中增加数据源切换。
3. **批量运维双版本**：旧版 `/api/v1/tasks/batch`（仅返回 IDs）与 M5 版 `/api/v1/tasks/batch-exec`（返回完整状态）并存，前端应优先使用 M5 版。

### 6.3 权限点规划

新增子域需在后端 RBAC 权限目录中注册对应权限点（如 `alert-rule:read/write`、`batch:write`、`canary:write`、`platform:read/write`、`federation:read/write`、`config:write`、`approval:read/write`、`network:read/write`、`helm:read/write` 等），并在前端路由 `meta.requirePerm` 中配置。

## 7. 附录

### 7.1 后端路由注册文件索引

| 文件 | 注册路由数 | 说明 |
|------|-----------|------|
| `server_lifecycle.go` | 161 | 主路由注册文件 |
| `internal/cmdb/handler.go` | 9 | CMDB 路由（RegisterRoutes） |
| `internal/logstore/handler.go` | 1 | 日志检索路由 |
| `internal/deploy/handler.go` | 4 | 部署路由（含联邦部署） |
| `internal/orchestration/handler.go` | 2 | 作业编排路由 |
| `service_proxy.go` | 10 | 微服务聚合代理（5 域 × 2 路径） |

### 7.2 前端 API 模块索引

| 模块 | 已接入子域 | 对应后端路由前缀 |
|------|-----------|-----------------|
| alert.js | 告警（活跃） | /alerts |
| apikeys.js | API Key | /apikeys |
| argocd.js | ArgoCD | /argocd/apps |
| audit.js | 审计事件 | /audit/events, /audit/export |
| auth.js | 认证/用户/角色 | /auth, /users, /roles, /permissions |
| automation.js | 自动化规则 | /automation/rules, /automation/executions |
| autoscaler.js | 自动扩缩容 | /autoscaler（service proxy） |
| backup.js | 灾备备份 | /backup |
| billing.js | 计费 | /billing |
| bot.js | ChatOps | /bot |
| cmdb.js | CMDB（基础） | /cmdb/types, /cmdb/ci, /cmdb/attr-templates |
| compliance.js | 安全合规 | /compliance |
| deploy.js | 部署 | /deploys |
| device.js | 设备 | /devices, /agents |
| gateway.js | API 网关 | /gateway |
| gpu.js | GPU 算力 | /gpu（service proxy） |
| ha.js | HA 管理 | /ha |
| incident.js | Incident | /incidents（service proxy） |
| k8s.js | K8s 管理 | /k8s/clusters |
| log.js | 日志检索 | /logs |
| middleware.js | 中间件部署 | /middleware-templates, /middleware-instances |
| notify.js | 通知渠道 | /notify-channels, /notify-templates |
| os-optimize.js | OS 优化 | /os-templates |
| pipeline.js | CI/CD 流水线 | /pipeline/templates, /pipeline/runs |
| plugin.js | 插件市场 | /marketplace/plugins |
| portal.js | 自助门户 | /portal（service proxy） |
| quota.js | 租户配额 | /quotas |
| request.js | HTTP 请求封装 | （基础设施） |
| runbook.js | Runbook | /runbooks（service proxy） |
| schedule.js | 定时任务 | /schedules |
| script.js | 自定义脚本 | /scripts |
| secrets.js | 密钥管理 | /secrets |
| slo.js | SLO | /slos |
| sse.js | SSE 实时推送 | /events/stream |
| task.js | 任务（基础） | /tasks |
| tenant.js | 租户管理 | /tenants |
| ticket.js | 工单 | /tickets |
| traffic.js | 流量策略 | /traffic/policies |
| webhook.js | Webhook | /webhooks |
| workflow.js | 作业编排 | /workflows |

### 7.3 排除的路由（不需前端接入）

| 路由 | 说明 |
|------|------|
| `/` | Dashboard（前端路由 /overview 替代） |
| `/assets/` | 静态资源 |
| `/healthz` | K8s liveness 探针 |
| `/readyz` | K8s readiness 探针 |
| `/metrics` | Prometheus 指标端点 |
| `/install.sh` | Agent 安装脚本分发 |
| `/bin/opsmesh-agent` | Agent 二进制分发 |
| `/gw/` | API 网关数据面（反向代理转发，非管理 API） |

---

> 本报告基于 2026-09-03 代码快照生成，后续后端路由变更需同步更新。