# 个人版前端差距分析报告

> 生成时间：2026-09-03
> 分析范围：后端 `internal/controlplane/` 全部 HTTP 路由 vs 个人版前端 `internal/controlplane/embed/web/` 已接入功能域
> 任务 ID：455（Phase 3 分析）

## 1. 总览统计

| 指标 | 数值 | 说明 |
|------|------|------|
| 后端功能域总数 | 42 | 按业务域分组（合并子域如 alert-rules+engine+silences=告警规则管理） |
| 个人版已接入数 | 21 | 19 个 tab + 认证(Auth) + RBAC(Users/Roles/Permissions) |
| 缺失功能域数 | 21 | 后端有路由但个人版前端无对应 tab/API/UI |
| 排除路由数 | 8 | healthz/readyz/metrics/install.sh/bin/opsmesh-agent/assets/dashboard/gw 数据面 |

### 1.1 个人版已接入功能域清单（21 个）

| 功能域 | Tab 名称 | API 函数 | 说明 |
|--------|----------|----------|------|
| 工单管理 | tickets | getTickets/createTicket/... | `/api/v1/tickets` |
| SLO 管理 | slo | getSLOs/createSLO/... | `/api/v1/slos` |
| 流量治理 | traffic | getTrafficPolicies/... | `/api/v1/traffic/policies` |
| CI/CD 流水线 | pipeline | getPipelineTemplates/... + getArgoCDApps/... | `/api/v1/pipeline/*` + `/api/v1/argocd/apps` |
| 灰度发布 | canary | getCanaryReleases/... | `/api/v1/tasks/canary` |
| 配置热推 | config-push | hotpushConfig/... | `/api/v1/config/hotpush` |
| 安全合规 | compliance | getComplianceRules/... | `/api/v1/compliance/*` |
| 审计事件 | compliance(内嵌) | getAuditEvents/... | `/api/v1/audit/events` + `/api/v1/audit/export` |
| 高可用/灾备 | ha | getHAStatus/... + backupCreate/... | `/api/v1/ha/*` + `/api/v1/backup/*` |
| 网络管理 | network-mgmt | getNetworkDevices/... | `/api/v1/network/devices` + topology |
| 自动化闭环 | automation | getAutomationRules/... | `/api/v1/automation/*` |
| API 网关 | gateway | getGatewayRoutes/... | `/api/v1/gateway/*` |
| Webhook | webhook | getWebhooks/... | `/api/v1/webhooks` |
| 自定义脚本 | script | getScripts/... | `/api/v1/scripts` |
| 租户管理 | tenant | getTenants/... + getQuotas/... | `/api/v1/tenants` + `/api/v1/quotas` |
| API Key | apikey | getAPIKeys/... | `/api/v1/apikeys` |
| 插件市场 | plugin | getPlugins/... | `/api/v1/marketplace/plugins` |
| 计费 | billing | getBillingPlans/... | `/api/v1/billing/*` |
| 平台配置 | platform | getPlatformConfig/... | `/api/v1/platform/*` |
| 认证 | (登录页) | login/register/changePassword | `/api/v1/auth/*` |
| RBAC | (用户中心) | getUsers/getRoles/getPermissions | `/api/v1/users` + `/api/v1/roles` + `/api/v1/permissions` |

## 2. 缺失功能域详细清单（21 个）

### P0 — 核心运维功能（5 个）

#### 2.1 设备管理（Devices）
- **后端路由**：`GET /api/v1/devices`（列表）、`DELETE /api/v1/devices/{id}`（退役）、`GET /api/v1/devices/{id}/metrics`（指标）、`GET /api/v1/agents`
- **需要新建**：api.js 新增 getDevices/getDeviceMetrics/retireDevice；render-devices.js 新建；flow-devices.js 新建；index.html 新增 tab+page
- **功能描述**：设备纳管列表、退役、监控指标展示
- **实现复杂度**：中等

#### 2.2 任务执行（Tasks）
- **后端路由**：`GET/POST /api/v1/tasks`、`POST /api/v1/tasks/{id}/cancel`、`GET /api/v1/tasks/{id}/result`
- **需要新建**：api.js 新增 getTasks/createTask/cancelTask/getTaskResult；render-tasks.js 新建；flow-tasks.js 新建；index.html 新增 tab+page
- **功能描述**：任务下发、取消、结果查看
- **实现复杂度**：中等

#### 2.3 告警管理（Alerts）
- **后端路由**：`GET /api/v1/alerts`、`POST /api/v1/alerts/{id}/ack`、`POST /api/v1/alerts/{id}/silence`
- **需要新建**：api.js 新增 getAlerts/ackAlert/silenceAlert；render-alerts.js 新建；flow-alerts.js 新建；index.html 新增 tab+page
- **功能描述**：活跃告警列表、确认、静默
- **实现复杂度**：简单

#### 2.4 告警规则管理（Alert Rules）
- **后端路由**：`CRUD /api/v1/alert-rules`、`CRUD /api/v1/alert-rules-engine`、`CRUD /api/v1/alert-silences`
- **需要新建**：api.js 新增 alertRules CRUD；render-alert-rules.js 新建；flow-alert-rules.js 新建；index.html 新增 tab+page
- **功能描述**：告警规则 CRUD、多条件引擎、静默规则
- **实现复杂度**：复杂

#### 2.5 批量执行（Batch Exec）
- **后端路由**：`POST /api/v1/tasks/batch-exec`、`GET /api/v1/tasks/batch/{id}`、`POST /api/v1/tasks/batch`
- **需要新建**：api.js 新增 batchExec/getBatchStatus；render-batch.js 新建；flow-batch.js 新建；index.html 新增 tab+page
- **功能描述**：批量任务下发、状态查询
- **实现复杂度**：中等

### P1 — 平台管理功能（8 个）

#### 2.6 通知管理（Notify）
- **后端路由**：`CRUD /api/v1/notify-channels`、`CRUD /api/v1/notify-templates`
- **需要新建**：api.js 新增 notifyChannels/notifyTemplates CRUD；render-notify.js 新建；flow-notify.js 新建
- **功能描述**：通知渠道配置、通知模板管理
- **实现复杂度**：中等

#### 2.7 日志检索（Logs）
- **后端路由**：`GET /api/v1/logs`
- **需要新建**：api.js 新增 searchLogs；render-logs.js 新建；flow-logs.js 新建；index.html 新增 tab+page
- **功能描述**：日志检索查询
- **实现复杂度**：简单

#### 2.8 部署中心（Deploys）
- **后端路由**：`GET/POST /api/v1/deploys`、`POST /api/v1/deploys/{id}/rollback`、`GET /api/v1/deploys/federation`
- **需要新建**：api.js 新增 deploys CRUD + federation；render-deploys.js 新建；flow-deploys.js 新建
- **功能描述**：部署创建、回滚、联邦部署
- **实现复杂度**：复杂

#### 2.9 作业编排（Workflows）
- **后端路由**：`GET/POST /api/v1/workflows`、`POST /api/v1/workflows/{id}/run`、`GET /api/v1/workflows/{id}/status`
- **需要新建**：api.js 新增 workflows CRUD + run + schedule；render-workflows.js 新建；flow-workflows.js 新建
- **功能描述**：工作流编排、执行、调度
- **实现复杂度**：复杂

#### 2.10 CMDB（配置项管理）
- **后端路由**：`GET /api/v1/cmdb/ci`、`GET /api/v1/cmdb/types`、`POST /api/v1/cmdb/collect`、`GET /api/v1/cmdb/changes`
- **需要新建**：api.js 新增 CMDB CI/types/collect/changes；render-cmdb.js 新建；flow-cmdb.js 新建
- **功能描述**：配置项管理、采集、变更审批
- **实现复杂度**：复杂

#### 2.11 OS 优化（OS Optimize）
- **后端路由**：`GET /api/v1/os-templates`、`POST /api/v1/os-templates/{id}/execute`
- **需要新建**：api.js 新增 getOSTemplates/executeOSTemplate；render-os-optimize.js 新建；flow-os-optimize.js 新建
- **功能描述**：OS 优化模板列表、执行
- **实现复杂度**：中等

#### 2.12 中间件部署（Middleware）
- **后端路由**：`GET /api/v1/middleware-templates`、`POST /api/v1/middleware-templates/{id}/deploy`、`GET /api/v1/middleware-instances`
- **需要新建**：api.js 新增 middlewareTemplates/instances；render-middleware.js 新建；flow-middleware.js 新建
- **功能描述**：中间件模板部署、实例管理
- **实现复杂度**：中等

#### 2.13 K8s 集群管理（K8s）
- **后端路由**：`GET/POST /api/v1/k8s/clusters`、`POST /api/v1/k8s/clusters/{id}/test`
- **需要新建**：api.js 新增 k8sClusters CRUD + test；render-k8s.js 新建；flow-k8s.js 新建
- **功能描述**：K8s 集群管理、连接测试、资源管理
- **实现复杂度**：复杂

### P2 — 辅助功能（5 个）

#### 2.14 SSE 实时推送（SSE）
- **后端路由**：`GET /api/v1/events/stream`
- **需要新建**：api.js 新增 SSE EventSource 连接；poll.js 或 flow.js 新增 SSE 接入
- **功能描述**：实时事件推送（任务状态/告警/设备上下线）
- **实现复杂度**：中等

#### 2.15 自动纳管（Auto Provision）
- **后端路由**：`POST /api/v1/provision/auto`
- **需要新建**：api.js 新增 autoProvision；render-provision.js 新建
- **功能描述**：设备自动纳管
- **实现复杂度**：简单

#### 2.16 ChatOps（Bot）
- **后端路由**：`POST /api/v1/bot/command`、`GET /api/v1/bot/history`、`GET /api/v1/bot/platforms`
- **需要新建**：api.js 新增 bot command/history/platforms；render-bot.js 新建；flow-bot.js 新建
- **功能描述**：ChatOps 命令台、历史记录
- **实现复杂度**：中等

#### 2.17 控制面联邦（Federation）
- **后端路由**：`GET /api/v1/federation/peers`、`POST /api/v1/federation/forward/task`、`GET /api/v1/federation/devices`
- **需要新建**：api.js 新增 federation peers/forward/devices；render-federation.js 新建
- **功能描述**：联邦 peer 管理、跨网段任务转发
- **实现复杂度**：复杂

#### 2.18 定时任务（Schedules）
- **后端路由**：`CRUD /api/v1/schedules`
- **需要新建**：api.js 新增 schedules CRUD；render-schedules.js 新建；flow-schedules.js 新建
- **功能描述**：定时任务 CRUD
- **实现复杂度**：简单

### P3 — 其他（3 个）

#### 2.19 审批流（Approval）
- **后端路由**：`CRUD /api/v1/approval/flows`、`CRUD /api/v1/approval/requests`、`GET /api/v1/approval/pending`
- **需要新建**：api.js 新增 approval flows/requests/pending；render-approval.js 新建
- **功能描述**：审批流定义、审批请求处理
- **实现复杂度**：复杂

#### 2.20 密钥管理（Secrets）
- **后端路由**：`GET /api/v1/secrets/status`、`POST /api/v1/secrets/test`、`GET /api/v1/secrets/keys`
- **需要新建**：api.js 新增 secretsStatus/test/keys；render-secrets.js 新建
- **功能描述**：密钥后端状态、测试、密钥列表
- **实现复杂度**：简单

#### 2.21 Helm 应用商店（Helm）
- **后端路由**：`GET/POST /api/v1/helm/repos`、`GET /api/v1/helm/charts/search`、`GET/POST /api/v1/helm/releases`、`GET /api/v1/helm/catalog`
- **需要新建**：api.js 新增 helm repos/charts/releases/catalog；render-helm.js 新建
- **功能描述**：Helm 仓库管理、Chart 搜索、Release 管理
- **实现复杂度**：复杂

## 3. 建议实施批次

### 第一批（P0 核心运维，5 个）
设备管理 → 任务执行 → 告警管理 → 告警规则管理 → 批量执行

### 第二批（P1 平台管理，8 个）
通知管理 → 日志检索 → 部署中心 → 作业编排 → CMDB → OS 优化 → 中间件部署 → K8s 集群

### 第三批（P2 辅助功能，5 个）
SSE 实时推送 → 自动纳管 → ChatOps → 控制面联邦 → 定时任务

### 第四批（P3 其他，3 个）
审批流 → 密钥管理 → Helm 应用商店

## 4. 备注

- 个人版前端使用原生 JS（非 Vue），每个功能域需要修改 `api.js` + 新建 `render-*.js` + 新建 `flow-*.js` + 修改 `index.html` + 修改 `i18n.js`
- 个人版前端已有 19 个 tab，新增 tab 需要在 `index.html` 的 tab nav 和 page section 中添加
- `flow.js` 的 `validTabs` 数组和 `switchTab` 函数需要同步更新
- `main.js` 需要导出新函数
- 部分功能域（如 SSE）不需要独立 tab，可以集成到现有页面中