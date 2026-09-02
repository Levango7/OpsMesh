# API 契约审计报告

> 校验时间：2026-09-03 06:44:19  
> 工具：自动化脚本（grep 后端路由注册 + 前端 API 调用提取 + 路径参数规范化对比）  
> 仓库：OpsMesh-ci（`internal/controlplane` + `web/enterprise` + `embed/web`）

## 1. 概述

| 指标 | 数值 |
| --- | --- |
| 后端路由总数（含子路径前缀，去重） | 165 |
| 企业版前端 API 调用数（去重） | 243 |
| 个人版前端 API 调用数（去重） | 120 |
| 企业版匹配 API 数 | 243 |
| 企业版幽灵 API 数（前端有、后端无） | 0 |
| 个人版匹配 API 数 | 120 |
| 个人版幽灵 API 数 | 0 |
| 未接入 API 数（后端有、前端无） | 4 |
| 企业版匹配率 | 100.0% |
| 个人版匹配率 | 100.0% |
| 后端路由接入率 | 97.6% |

### 校验范围

**后端路由来源：**

- `internal/controlplane/server_lifecycle.go`（主路由注册，153+ 路由）
- `internal/cmdb/handler.go`（CMDB `RegisterRoutes`：CI/类型/关系/属性模板）
- `internal/logstore/handler.go`（日志检索 `RegisterRoutes`：`/api/v1/logs`）
- `internal/deploy/handler.go`（部署中心 `RegisterRoutes`：含联邦部署）
- `internal/orchestration/handler.go`（作业编排 `RegisterRoutes`：`/api/v1/workflows`）
- `internal/controlplane/service_proxy.go`（微服务聚合代理：gpu/runbooks/incidents/autoscaler/portal）

**前端 API 来源：**

- 企业版：`web/enterprise/src/api/*.js`（53 个模块，基于 axios，baseURL=`/api/v1`）+ `App.vue` SSE 端点
- 个人版：`internal/controlplane/embed/web/assets/api.js`（基于原生 fetch，完整路径）

### 规范化规则

- 路径参数统一：`${id}` / `${encodeURIComponent(id)}` / `:id` / `{id}` → `{id}`
- 查询参数（`?key=value`）忽略
- 后端尾斜杠路由（如 `/api/v1/devices/`）视为前缀匹配，覆盖其下所有子路径
- 忽略：静态资源（`/assets/`）、健康检查（`/healthz`、`/readyz`）、`/metrics`、`/install.sh`、`/bin/`、`/gw/`、仪表盘根路径 `/`

## 2. 幽灵 API（前端调用但后端无路由）

前端调用这些路径时将得到 404 响应，属于**契约断裂**。

### 2.1 企业版前端幽灵 API

✅ 无幽灵 API，企业版前端所有调用均有后端路由承接。

### 2.2 个人版前端幽灵 API

✅ 无幽灵 API，个人版前端所有调用均有后端路由承接。

## 3. 未接入 API（后端有路由但前端未调用）

这些后端端点已注册但无任何前端调用，可能是**待接入、内部使用或冗余**路由。

| # | 功能域 | API 路径 | 说明 |
| --- | --- | --- | --- |
| 1 | 微服务代理 | `/api/v1/autoscaler` | 精确路由 |
| 2 | 微服务代理 | `/api/v1/gpu` | 精确路由 |
| 3 | 微服务代理 | `/api/v1/portal` | 精确路由 |
| 4 | 设备/纳管 | `/api/v1/me` | 精确路由 |

> 共 **4** 个未接入 API，分布在 **2** 个功能域。

## 4. 匹配的 API（前端调用且后端有路由）

### 4.1 企业版匹配详情

| # | API 路径 | 功能域 |
| --- | --- | --- |
| 1 | `/api/v1/agents` | 设备/纳管 |
| 2 | `/api/v1/alert-rules` | 告警/通知 |
| 3 | `/api/v1/alert-rules-engine` | 告警/通知 |
| 4 | `/api/v1/alert-rules-engine/{id}` | 告警/通知 |
| 5 | `/api/v1/alert-rules/{id}` | 告警/通知 |
| 6 | `/api/v1/alert-silences` | 告警/通知 |
| 7 | `/api/v1/alert-silences/{id}` | 告警/通知 |
| 8 | `/api/v1/alerts` | 告警/通知 |
| 9 | `/api/v1/alerts/{id}/ack` | 告警/通知 |
| 10 | `/api/v1/alerts/{id}/silence` | 告警/通知 |
| 11 | `/api/v1/apikeys` | 平台/租户 |
| 12 | `/api/v1/apikeys/{id}` | 平台/租户 |
| 13 | `/api/v1/apikeys/{id}/disable` | 平台/租户 |
| 14 | `/api/v1/apikeys/{id}/enable` | 平台/租户 |
| 15 | `/api/v1/approval/flows` | 审批 |
| 16 | `/api/v1/approval/flows/{id}` | 审批 |
| 17 | `/api/v1/approval/pending` | 审批 |
| 18 | `/api/v1/approval/requests` | 审批 |
| 19 | `/api/v1/approval/requests/{id}` | 审批 |
| 20 | `/api/v1/approval/requests/{id}/approve` | 审批 |
| 21 | `/api/v1/approval/requests/{id}/cancel` | 审批 |
| 22 | `/api/v1/approval/requests/{id}/history` | 审批 |
| 23 | `/api/v1/approval/requests/{id}/reject` | 审批 |
| 24 | `/api/v1/argocd/apps` | 部署/编排 |
| 25 | `/api/v1/argocd/apps/{id}` | 部署/编排 |
| 26 | `/api/v1/argocd/apps/{id}/sync` | 部署/编排 |
| 27 | `/api/v1/audit/events` | 安全/审计 |
| 28 | `/api/v1/audits` | 安全/审计 |
| 29 | `/api/v1/auth/change-password` | 用户/权限 |
| 30 | `/api/v1/auth/login` | 用户/权限 |
| 31 | `/api/v1/auth/logout` | 用户/权限 |
| 32 | `/api/v1/auth/me` | 设备/纳管 |
| 33 | `/api/v1/auth/refresh` | 用户/权限 |
| 34 | `/api/v1/auth/register` | 用户/权限 |
| 35 | `/api/v1/automation/executions` | 自动化 |
| 36 | `/api/v1/automation/executions/{id}` | 自动化 |
| 37 | `/api/v1/automation/rules` | 自动化 |
| 38 | `/api/v1/automation/rules/{id}` | 自动化 |
| 39 | `/api/v1/automation/rules/{id}/disable` | 自动化 |
| 40 | `/api/v1/automation/rules/{id}/enable` | 自动化 |
| 41 | `/api/v1/automation/rules/{id}/test` | 自动化 |
| 42 | `/api/v1/autoscaler/cooldowns` | 微服务代理 |
| 43 | `/api/v1/autoscaler/decisions` | 微服务代理 |
| 44 | `/api/v1/autoscaler/rules` | 微服务代理 |
| 45 | `/api/v1/autoscaler/rules/{id}` | 微服务代理 |
| 46 | `/api/v1/autoscaler/scale` | 微服务代理 |
| 47 | `/api/v1/backup/create` | 安全/审计 |
| 48 | `/api/v1/backup/list` | 安全/审计 |
| 49 | `/api/v1/backup/restore` | 安全/审计 |
| 50 | `/api/v1/backup/{id}` | 安全/审计 |
| 51 | `/api/v1/billing/invoices` | 平台/租户 |
| 52 | `/api/v1/billing/invoices/{id}` | 平台/租户 |
| 53 | `/api/v1/billing/plans` | 平台/租户 |
| 54 | `/api/v1/billing/plans/{id}` | 平台/租户 |
| 55 | `/api/v1/billing/subscriptions` | 平台/租户 |
| 56 | `/api/v1/billing/subscriptions/{id}` | 平台/租户 |
| 57 | `/api/v1/billing/usage` | 平台/租户 |
| 58 | `/api/v1/bot/command` | Bot |
| 59 | `/api/v1/bot/history` | Bot |
| 60 | `/api/v1/bot/platforms` | 平台/租户 |
| 61 | `/api/v1/bot/quick-commands` | Bot |
| 62 | `/api/v1/canary/{id}/metrics` | 设备/纳管 |
| 63 | `/api/v1/canary/{id}/traffic-split` | 任务 |
| 64 | `/api/v1/cmdb/attr-templates` | CMDB |
| 65 | `/api/v1/cmdb/attr-templates/{id}` | CMDB |
| 66 | `/api/v1/cmdb/changes` | CMDB |
| 67 | `/api/v1/cmdb/changes/{id}` | CMDB |
| 68 | `/api/v1/cmdb/changes/{id}/approve` | CMDB |
| 69 | `/api/v1/cmdb/changes/{id}/reject` | CMDB |
| 70 | `/api/v1/cmdb/ci` | CMDB |
| 71 | `/api/v1/cmdb/ci/export` | CMDB |
| 72 | `/api/v1/cmdb/ci/import` | CMDB |
| 73 | `/api/v1/cmdb/ci/pending` | CMDB |
| 74 | `/api/v1/cmdb/ci/{id}` | CMDB |
| 75 | `/api/v1/cmdb/ci/{id}/approve` | CMDB |
| 76 | `/api/v1/cmdb/ci/{id}/graph` | CMDB |
| 77 | `/api/v1/cmdb/ci/{id}/reject` | CMDB |
| 78 | `/api/v1/cmdb/ci/{id}/relations` | CMDB |
| 79 | `/api/v1/cmdb/collect` | CMDB |
| 80 | `/api/v1/cmdb/relations` | CMDB |
| 81 | `/api/v1/cmdb/types` | CMDB |
| 82 | `/api/v1/compliance/reports` | 安全/审计 |
| 83 | `/api/v1/compliance/reports/{id}` | 安全/审计 |
| 84 | `/api/v1/compliance/rules` | 安全/审计 |
| 85 | `/api/v1/compliance/rules/{id}` | 安全/审计 |
| 86 | `/api/v1/compliance/scan` | 安全/审计 |
| 87 | `/api/v1/config/canary` | 任务 |
| 88 | `/api/v1/config/hotpush` | 流量/配置 |
| 89 | `/api/v1/config/versions` | 流量/配置 |
| 90 | `/api/v1/deploys` | 部署/编排 |
| 91 | `/api/v1/deploys/federation` | 部署/编排 |
| 92 | `/api/v1/deploys/federation/{id}` | 部署/编排 |
| 93 | `/api/v1/deploys/{id}` | 部署/编排 |
| 94 | `/api/v1/deploys/{id}/execute` | 部署/编排 |
| 95 | `/api/v1/deploys/{id}/rollback` | 部署/编排 |
| 96 | `/api/v1/devices` | 设备/纳管 |
| 97 | `/api/v1/devices/{id}` | 设备/纳管 |
| 98 | `/api/v1/devices/{id}/metrics` | 设备/纳管 |
| 99 | `/api/v1/devices/{id}/provision` | 设备/纳管 |
| 100 | `/api/v1/events/stream` | SSE |
| 101 | `/api/v1/federation/devices` | 设备/纳管 |
| 102 | `/api/v1/federation/forward/task` | 联邦 |
| 103 | `/api/v1/federation/peers` | 联邦 |
| 104 | `/api/v1/gateway/routes` | 网关 |
| 105 | `/api/v1/gateway/routes/{id}` | 网关 |
| 106 | `/api/v1/gateway/routes/{id}/disable` | 网关 |
| 107 | `/api/v1/gateway/routes/{id}/enable` | 网关 |
| 108 | `/api/v1/gateway/stats` | 网关 |
| 109 | `/api/v1/gpu/metrics` | 设备/纳管 |
| 110 | `/api/v1/gpu/models` | 微服务代理 |
| 111 | `/api/v1/gpu/models/{id}` | 微服务代理 |
| 112 | `/api/v1/gpu/nodes` | 微服务代理 |
| 113 | `/api/v1/gpu/quotas` | 平台/租户 |
| 114 | `/api/v1/gpu/workloads` | 微服务代理 |
| 115 | `/api/v1/gpu/workloads/{id}` | 微服务代理 |
| 116 | `/api/v1/ha/failover` | 安全/审计 |
| 117 | `/api/v1/ha/health` | 安全/审计 |
| 118 | `/api/v1/ha/instances` | 安全/审计 |
| 119 | `/api/v1/ha/status` | 安全/审计 |
| 120 | `/api/v1/helm/catalog` | Helm |
| 121 | `/api/v1/helm/charts/search` | Helm |
| 122 | `/api/v1/helm/releases` | Helm |
| 123 | `/api/v1/helm/releases/{id}` | Helm |
| 124 | `/api/v1/helm/releases/{id}/history` | Helm |
| 125 | `/api/v1/helm/releases/{id}/rollback` | Helm |
| 126 | `/api/v1/helm/repos` | Helm |
| 127 | `/api/v1/helm/repos/{id}` | Helm |
| 128 | `/api/v1/helm/repos/{id}/charts` | Helm |
| 129 | `/api/v1/incidents` | 微服务代理 |
| 130 | `/api/v1/incidents/metrics` | 设备/纳管 |
| 131 | `/api/v1/incidents/{id}` | 微服务代理 |
| 132 | `/api/v1/incidents/{id}/postmortem` | 微服务代理 |
| 133 | `/api/v1/incidents/{id}/timeline` | 微服务代理 |
| 134 | `/api/v1/k8s/clusters` | OS/中间件/K8s |
| 135 | `/api/v1/k8s/clusters/{id}` | OS/中间件/K8s |
| 136 | `/api/v1/k8s/clusters/{id}/configmaps` | 流量/配置 |
| 137 | `/api/v1/k8s/clusters/{id}/deployments` | OS/中间件/K8s |
| 138 | `/api/v1/k8s/clusters/{id}/namespaces` | OS/中间件/K8s |
| 139 | `/api/v1/k8s/clusters/{id}/nodes` | OS/中间件/K8s |
| 140 | `/api/v1/k8s/clusters/{id}/pods` | OS/中间件/K8s |
| 141 | `/api/v1/k8s/clusters/{id}/pods/{id}/{id}` | OS/中间件/K8s |
| 142 | `/api/v1/k8s/clusters/{id}/secrets` | 安全/审计 |
| 143 | `/api/v1/k8s/clusters/{id}/services` | OS/中间件/K8s |
| 144 | `/api/v1/k8s/clusters/{id}/test` | OS/中间件/K8s |
| 145 | `/api/v1/logs` | 日志 |
| 146 | `/api/v1/marketplace/plugins` | 平台/租户 |
| 147 | `/api/v1/marketplace/plugins/{id}` | 平台/租户 |
| 148 | `/api/v1/marketplace/plugins/{id}/install` | 平台/租户 |
| 149 | `/api/v1/marketplace/plugins/{id}/uninstall` | 平台/租户 |
| 150 | `/api/v1/middleware-instances` | OS/中间件/K8s |
| 151 | `/api/v1/middleware-instances/{id}/uninstall` | OS/中间件/K8s |
| 152 | `/api/v1/middleware-templates` | OS/中间件/K8s |
| 153 | `/api/v1/middleware-templates/{id}` | OS/中间件/K8s |
| 154 | `/api/v1/middleware-templates/{id}/deploy` | OS/中间件/K8s |
| 155 | `/api/v1/network/connectivity` | 网络 |
| 156 | `/api/v1/network/devices` | 设备/纳管 |
| 157 | `/api/v1/network/devices/{id}` | 设备/纳管 |
| 158 | `/api/v1/network/devices/{id}/config` | 设备/纳管 |
| 159 | `/api/v1/network/devices/{id}/metrics` | 设备/纳管 |
| 160 | `/api/v1/network/diagnose` | 网络 |
| 161 | `/api/v1/network/diagnose/{id}` | 网络 |
| 162 | `/api/v1/network/discover` | 网络 |
| 163 | `/api/v1/network/topology` | 网络 |
| 164 | `/api/v1/network/topology/cache` | 网络 |
| 165 | `/api/v1/notify-channels` | 告警/通知 |
| 166 | `/api/v1/notify-channels/{id}` | 告警/通知 |
| 167 | `/api/v1/notify-channels/{id}/test` | 告警/通知 |
| 168 | `/api/v1/notify-templates` | 告警/通知 |
| 169 | `/api/v1/notify-templates/{id}` | 告警/通知 |
| 170 | `/api/v1/os-templates` | OS/中间件/K8s |
| 171 | `/api/v1/os-templates/{id}` | OS/中间件/K8s |
| 172 | `/api/v1/os-templates/{id}/execute` | OS/中间件/K8s |
| 173 | `/api/v1/permissions` | 用户/权限 |
| 174 | `/api/v1/pipeline/runs` | 部署/编排 |
| 175 | `/api/v1/pipeline/runs/{id}` | 部署/编排 |
| 176 | `/api/v1/pipeline/templates` | 部署/编排 |
| 177 | `/api/v1/pipeline/templates/{id}` | 部署/编排 |
| 178 | `/api/v1/pipeline/templates/{id}/run` | 部署/编排 |
| 179 | `/api/v1/platform/config` | 平台/租户 |
| 180 | `/api/v1/platform/health` | 平台/租户 |
| 181 | `/api/v1/platform/metrics` | 设备/纳管 |
| 182 | `/api/v1/portal/approvals` | 审批 |
| 183 | `/api/v1/portal/approvals/{id}/approve` | 审批 |
| 184 | `/api/v1/portal/approvals/{id}/reject` | 审批 |
| 185 | `/api/v1/portal/cost` | 微服务代理 |
| 186 | `/api/v1/portal/requests` | 微服务代理 |
| 187 | `/api/v1/provision/auto` | 设备/纳管 |
| 188 | `/api/v1/quotas` | 平台/租户 |
| 189 | `/api/v1/quotas/{id}` | 平台/租户 |
| 190 | `/api/v1/roles` | 用户/权限 |
| 191 | `/api/v1/roles/{id}` | 用户/权限 |
| 192 | `/api/v1/runbooks` | 微服务代理 |
| 193 | `/api/v1/runbooks/{id}` | 微服务代理 |
| 194 | `/api/v1/runbooks/{id}/execute` | 微服务代理 |
| 195 | `/api/v1/runbooks/{id}/executions` | 微服务代理 |
| 196 | `/api/v1/runbooks/{id}/executions/{id}/logs` | 日志 |
| 197 | `/api/v1/schedules` | 任务 |
| 198 | `/api/v1/schedules/{id}` | 任务 |
| 199 | `/api/v1/schedules/{id}/pause` | 任务 |
| 200 | `/api/v1/schedules/{id}/resume` | 任务 |
| 201 | `/api/v1/scripts` | 脚本/Webhook |
| 202 | `/api/v1/scripts/{id}` | 脚本/Webhook |
| 203 | `/api/v1/scripts/{id}/execute` | 脚本/Webhook |
| 204 | `/api/v1/scripts/{id}/executions` | 脚本/Webhook |
| 205 | `/api/v1/secrets/keys` | 安全/审计 |
| 206 | `/api/v1/secrets/status` | 安全/审计 |
| 207 | `/api/v1/secrets/test` | 安全/审计 |
| 208 | `/api/v1/slos` | 服务台/SLO |
| 209 | `/api/v1/slos/{id}` | 服务台/SLO |
| 210 | `/api/v1/slos/{id}/status` | 服务台/SLO |
| 211 | `/api/v1/tasks` | 任务 |
| 212 | `/api/v1/tasks/batch` | 任务 |
| 213 | `/api/v1/tasks/batch-exec` | 任务 |
| 214 | `/api/v1/tasks/batch/{id}` | 任务 |
| 215 | `/api/v1/tasks/canary` | 任务 |
| 216 | `/api/v1/tasks/canary/{id}` | 任务 |
| 217 | `/api/v1/tasks/canary/{id}/advance` | 任务 |
| 218 | `/api/v1/tasks/{id}` | 任务 |
| 219 | `/api/v1/tasks/{id}/cancel` | 任务 |
| 220 | `/api/v1/tenants` | 平台/租户 |
| 221 | `/api/v1/tenants/{id}` | 平台/租户 |
| 222 | `/api/v1/tenants/{id}/activate` | 平台/租户 |
| 223 | `/api/v1/tenants/{id}/suspend` | 平台/租户 |
| 224 | `/api/v1/tickets` | 服务台/SLO |
| 225 | `/api/v1/tickets/{id}` | 服务台/SLO |
| 226 | `/api/v1/tickets/{id}/close` | 服务台/SLO |
| 227 | `/api/v1/traffic/policies` | 流量/配置 |
| 228 | `/api/v1/traffic/policies/{id}` | 流量/配置 |
| 229 | `/api/v1/traffic/policies/{id}/disable` | 流量/配置 |
| 230 | `/api/v1/traffic/policies/{id}/enable` | 流量/配置 |
| 231 | `/api/v1/users` | 用户/权限 |
| 232 | `/api/v1/users/{id}` | 用户/权限 |
| 233 | `/api/v1/users/{id}/approve` | 用户/权限 |
| 234 | `/api/v1/users/{id}/reject` | 用户/权限 |
| 235 | `/api/v1/webhooks` | 脚本/Webhook |
| 236 | `/api/v1/webhooks/{id}` | 脚本/Webhook |
| 237 | `/api/v1/webhooks/{id}/deliveries` | 脚本/Webhook |
| 238 | `/api/v1/webhooks/{id}/test` | 脚本/Webhook |
| 239 | `/api/v1/workflows` | 部署/编排 |
| 240 | `/api/v1/workflows/{id}` | 部署/编排 |
| 241 | `/api/v1/workflows/{id}/run` | 部署/编排 |
| 242 | `/api/v1/workflows/{id}/schedule` | 部署/编排 |
| 243 | `/api/v1/workflows/{id}/status` | 部署/编排 |

### 4.2 个人版匹配详情

| # | API 路径 | 功能域 |
| --- | --- | --- |
| 1 | `/api/v1/agents` | 设备/纳管 |
| 2 | `/api/v1/alert-rules` | 告警/通知 |
| 3 | `/api/v1/alert-rules-engine` | 告警/通知 |
| 4 | `/api/v1/alert-rules-engine/` | 告警/通知 |
| 5 | `/api/v1/alert-rules/` | 告警/通知 |
| 6 | `/api/v1/alert-silences` | 告警/通知 |
| 7 | `/api/v1/alert-silences/` | 告警/通知 |
| 8 | `/api/v1/alerts` | 告警/通知 |
| 9 | `/api/v1/alerts/` | 告警/通知 |
| 10 | `/api/v1/apikeys` | 平台/租户 |
| 11 | `/api/v1/apikeys/` | 平台/租户 |
| 12 | `/api/v1/approval/flows` | 审批 |
| 13 | `/api/v1/approval/flows/` | 审批 |
| 14 | `/api/v1/approval/pending` | 审批 |
| 15 | `/api/v1/approval/requests` | 审批 |
| 16 | `/api/v1/approval/requests/` | 审批 |
| 17 | `/api/v1/argocd/apps` | 部署/编排 |
| 18 | `/api/v1/argocd/apps/` | 部署/编排 |
| 19 | `/api/v1/audit/events` | 安全/审计 |
| 20 | `/api/v1/audit/export` | 安全/审计 |
| 21 | `/api/v1/automation/executions` | 自动化 |
| 22 | `/api/v1/automation/executions/` | 自动化 |
| 23 | `/api/v1/automation/rules` | 自动化 |
| 24 | `/api/v1/automation/rules/` | 自动化 |
| 25 | `/api/v1/backup/` | 安全/审计 |
| 26 | `/api/v1/backup/create` | 安全/审计 |
| 27 | `/api/v1/backup/list` | 安全/审计 |
| 28 | `/api/v1/backup/restore` | 安全/审计 |
| 29 | `/api/v1/billing/invoices` | 平台/租户 |
| 30 | `/api/v1/billing/invoices/` | 平台/租户 |
| 31 | `/api/v1/billing/plans` | 平台/租户 |
| 32 | `/api/v1/billing/plans/` | 平台/租户 |
| 33 | `/api/v1/billing/subscriptions` | 平台/租户 |
| 34 | `/api/v1/billing/subscriptions/` | 平台/租户 |
| 35 | `/api/v1/bot/command` | Bot |
| 36 | `/api/v1/bot/history` | Bot |
| 37 | `/api/v1/bot/platforms` | 平台/租户 |
| 38 | `/api/v1/canary/` | 任务 |
| 39 | `/api/v1/cmdb/changes` | CMDB |
| 40 | `/api/v1/cmdb/ci` | CMDB |
| 41 | `/api/v1/cmdb/collect` | CMDB |
| 42 | `/api/v1/cmdb/types` | CMDB |
| 43 | `/api/v1/compliance/reports` | 安全/审计 |
| 44 | `/api/v1/compliance/reports/` | 安全/审计 |
| 45 | `/api/v1/compliance/rules` | 安全/审计 |
| 46 | `/api/v1/compliance/rules/` | 安全/审计 |
| 47 | `/api/v1/compliance/scan` | 安全/审计 |
| 48 | `/api/v1/config/canary` | 任务 |
| 49 | `/api/v1/config/hotpush` | 流量/配置 |
| 50 | `/api/v1/config/versions` | 流量/配置 |
| 51 | `/api/v1/deploys` | 部署/编排 |
| 52 | `/api/v1/deploys/` | 部署/编排 |
| 53 | `/api/v1/deploys/federation` | 部署/编排 |
| 54 | `/api/v1/devices` | 设备/纳管 |
| 55 | `/api/v1/devices/` | 设备/纳管 |
| 56 | `/api/v1/federation/devices` | 设备/纳管 |
| 57 | `/api/v1/federation/forward/task` | 联邦 |
| 58 | `/api/v1/federation/peers` | 联邦 |
| 59 | `/api/v1/gateway/routes` | 网关 |
| 60 | `/api/v1/gateway/routes/` | 网关 |
| 61 | `/api/v1/gateway/stats` | 网关 |
| 62 | `/api/v1/ha/failover` | 安全/审计 |
| 63 | `/api/v1/ha/health` | 安全/审计 |
| 64 | `/api/v1/ha/instances` | 安全/审计 |
| 65 | `/api/v1/ha/status` | 安全/审计 |
| 66 | `/api/v1/helm/catalog` | Helm |
| 67 | `/api/v1/helm/charts/search` | Helm |
| 68 | `/api/v1/helm/releases` | Helm |
| 69 | `/api/v1/helm/releases/` | Helm |
| 70 | `/api/v1/helm/repos` | Helm |
| 71 | `/api/v1/helm/repos/` | Helm |
| 72 | `/api/v1/k8s/clusters` | OS/中间件/K8s |
| 73 | `/api/v1/k8s/clusters/` | OS/中间件/K8s |
| 74 | `/api/v1/logs` | 日志 |
| 75 | `/api/v1/marketplace/plugins` | 平台/租户 |
| 76 | `/api/v1/marketplace/plugins/` | 平台/租户 |
| 77 | `/api/v1/middleware-instances` | OS/中间件/K8s |
| 78 | `/api/v1/middleware-templates` | OS/中间件/K8s |
| 79 | `/api/v1/middleware-templates/` | OS/中间件/K8s |
| 80 | `/api/v1/network/devices` | 设备/纳管 |
| 81 | `/api/v1/network/devices/` | 设备/纳管 |
| 82 | `/api/v1/network/discover` | 网络 |
| 83 | `/api/v1/notify-channels` | 告警/通知 |
| 84 | `/api/v1/notify-channels/` | 告警/通知 |
| 85 | `/api/v1/notify-templates` | 告警/通知 |
| 86 | `/api/v1/notify-templates/` | 告警/通知 |
| 87 | `/api/v1/os-templates` | OS/中间件/K8s |
| 88 | `/api/v1/os-templates/` | OS/中间件/K8s |
| 89 | `/api/v1/pipeline/runs` | 部署/编排 |
| 90 | `/api/v1/pipeline/templates` | 部署/编排 |
| 91 | `/api/v1/pipeline/templates/` | 部署/编排 |
| 92 | `/api/v1/platform/config` | 平台/租户 |
| 93 | `/api/v1/platform/health` | 平台/租户 |
| 94 | `/api/v1/platform/metrics` | 设备/纳管 |
| 95 | `/api/v1/provision/auto` | 设备/纳管 |
| 96 | `/api/v1/schedules` | 任务 |
| 97 | `/api/v1/schedules/` | 任务 |
| 98 | `/api/v1/scripts` | 脚本/Webhook |
| 99 | `/api/v1/scripts/` | 脚本/Webhook |
| 100 | `/api/v1/secrets/keys` | 安全/审计 |
| 101 | `/api/v1/secrets/status` | 安全/审计 |
| 102 | `/api/v1/secrets/test` | 安全/审计 |
| 103 | `/api/v1/slos` | 服务台/SLO |
| 104 | `/api/v1/slos/` | 服务台/SLO |
| 105 | `/api/v1/tasks` | 任务 |
| 106 | `/api/v1/tasks/` | 任务 |
| 107 | `/api/v1/tasks/batch` | 任务 |
| 108 | `/api/v1/tasks/batch-exec` | 任务 |
| 109 | `/api/v1/tasks/batch/` | 任务 |
| 110 | `/api/v1/tasks/canary` | 任务 |
| 111 | `/api/v1/tenants` | 平台/租户 |
| 112 | `/api/v1/tenants/` | 平台/租户 |
| 113 | `/api/v1/tickets` | 服务台/SLO |
| 114 | `/api/v1/tickets/` | 服务台/SLO |
| 115 | `/api/v1/traffic/policies` | 流量/配置 |
| 116 | `/api/v1/traffic/policies/` | 流量/配置 |
| 117 | `/api/v1/webhooks` | 脚本/Webhook |
| 118 | `/api/v1/webhooks/` | 脚本/Webhook |
| 119 | `/api/v1/workflows` | 部署/编排 |
| 120 | `/api/v1/workflows/` | 部署/编排 |

## 5. 后端路由全量清单

| # | API 路径 | 注册来源 | 说明 |
| --- | --- | --- | --- |
| 1 | `/api/v1/agents` | server_lifecycle.go | 精确路由 |
| 2 | `/api/v1/alert-rules` | server_lifecycle.go | 精确路由 |
| 3 | `/api/v1/alert-rules-engine` | server_lifecycle.go | 精确路由 |
| 4 | `/api/v1/alert-rules-engine/` | server_lifecycle.go | 前缀路由；子路径：{id} GET/PUT/DELETE |
| 5 | `/api/v1/alert-rules/` | server_lifecycle.go | 前缀路由；子路径：{id} DELETE 删除 |
| 6 | `/api/v1/alert-silences` | server_lifecycle.go | 精确路由 |
| 7 | `/api/v1/alert-silences/` | server_lifecycle.go | 前缀路由；子路径：{id} DELETE |
| 8 | `/api/v1/alerts` | server_lifecycle.go | 精确路由；GET 活跃告警（M7） |
| 9 | `/api/v1/alerts/` | server_lifecycle.go | 前缀路由；子路径：{id}/ack、{id}/silence |
| 10 | `/api/v1/apikeys` | server_lifecycle.go | 精确路由 |
| 11 | `/api/v1/apikeys/` | server_lifecycle.go | 前缀路由；子路径：{id} GET/PUT/DELETE、{id}/enable|disa |
| 12 | `/api/v1/approval/flows` | server_lifecycle.go | 精确路由；GET 列表 / POST 创建审批流 |
| 13 | `/api/v1/approval/flows/` | server_lifecycle.go | 前缀路由；子路径：{id} GET/PUT/DELETE |
| 14 | `/api/v1/approval/pending` | server_lifecycle.go | 精确路由；GET 待我审批列表 |
| 15 | `/api/v1/approval/requests` | server_lifecycle.go | 精确路由；GET 列表 / POST 提交审批请求 |
| 16 | `/api/v1/approval/requests/` | server_lifecycle.go | 前缀路由；子路径：{id} GET、{id}/approve|reject|cancel| |
| 17 | `/api/v1/argocd/apps` | server_lifecycle.go | 精确路由 |
| 18 | `/api/v1/argocd/apps/` | server_lifecycle.go | 前缀路由；子路径：{id} GET/PUT/DELETE、{id}/sync |
| 19 | `/api/v1/audit/events` | server_lifecycle.go | 精确路由；GET 查询审计事件 |
| 20 | `/api/v1/audit/export` | server_lifecycle.go | 精确路由；GET 导出审计日志 |
| 21 | `/api/v1/audits` | server_lifecycle.go | 精确路由；GET 审计检索 |
| 22 | `/api/v1/auth/change-password` | server_lifecycle.go | 精确路由；安全债：预置弱口令强制改密 |
| 23 | `/api/v1/auth/login` | server_lifecycle.go | 精确路由 |
| 24 | `/api/v1/auth/logout` | server_lifecycle.go | 精确路由；登出清 HttpOnly Cookie |
| 25 | `/api/v1/auth/me` | server_lifecycle.go | 精确路由 |
| 26 | `/api/v1/auth/refresh` | server_lifecycle.go | 精确路由；双 Cookie：rt 静默换新 at+rt（旋转） |
| 27 | `/api/v1/auth/register` | server_lifecycle.go | 精确路由 |
| 28 | `/api/v1/automation/executions` | server_lifecycle.go | 精确路由 |
| 29 | `/api/v1/automation/executions/` | server_lifecycle.go | 前缀路由；子路径：{id} GET |
| 30 | `/api/v1/automation/rules` | server_lifecycle.go | 精确路由 |
| 31 | `/api/v1/automation/rules/` | server_lifecycle.go | 前缀路由；子路径：{id} GET/PUT/DELETE、{id}/enable|disa |
| 32 | `/api/v1/autoscaler` | service_proxy.go | 精确路由；微服务聚合代理根 |
| 33 | `/api/v1/autoscaler/` | service_proxy.go | 前缀路由；微服务聚合代理前缀（子路径转发） |
| 34 | `/api/v1/backup/` | server_lifecycle.go | 前缀路由；子路径：{id} DELETE |
| 35 | `/api/v1/backup/create` | server_lifecycle.go | 精确路由；POST 创建备份 |
| 36 | `/api/v1/backup/list` | server_lifecycle.go | 精确路由；GET 列出备份 |
| 37 | `/api/v1/backup/restore` | server_lifecycle.go | 精确路由；POST 恢复备份 |
| 38 | `/api/v1/billing/invoices` | server_lifecycle.go | 精确路由；GET 列表 |
| 39 | `/api/v1/billing/invoices/` | server_lifecycle.go | 前缀路由；子路径：{id} GET |
| 40 | `/api/v1/billing/plans` | server_lifecycle.go | 精确路由 |
| 41 | `/api/v1/billing/plans/` | server_lifecycle.go | 前缀路由；子路径：{id} GET/PUT/DELETE |
| 42 | `/api/v1/billing/subscriptions` | server_lifecycle.go | 精确路由；GET 列表 / POST 创建 |
| 43 | `/api/v1/billing/subscriptions/` | server_lifecycle.go | 前缀路由；子路径：{id} GET/PUT/DELETE |
| 44 | `/api/v1/billing/usage` | server_lifecycle.go | 精确路由；GET 资源用量统计 |
| 45 | `/api/v1/bot/command` | server_lifecycle.go | 精确路由 |
| 46 | `/api/v1/bot/history` | server_lifecycle.go | 精确路由 |
| 47 | `/api/v1/bot/platforms` | server_lifecycle.go | 精确路由 |
| 48 | `/api/v1/bot/quick-commands` | server_lifecycle.go | 精确路由 |
| 49 | `/api/v1/canary/` | server_lifecycle.go | 前缀路由；子路径：{id}/traffic-split、{id}/metrics |
| 50 | `/api/v1/cmdb/attr-templates` | cmdb/handler.go | 精确路由 |
| 51 | `/api/v1/cmdb/attr-templates/` | cmdb/handler.go | 前缀路由 |
| 52 | `/api/v1/cmdb/changes` | server_lifecycle.go | 精确路由；GET 列表 / POST 提交变更申请 |
| 53 | `/api/v1/cmdb/changes/` | server_lifecycle.go | 前缀路由；子路径：{id} GET、{id}/approve|reject POST |
| 54 | `/api/v1/cmdb/ci` | cmdb/handler.go | 精确路由 |
| 55 | `/api/v1/cmdb/ci/` | cmdb/handler.go | 前缀路由 |
| 56 | `/api/v1/cmdb/ci/export` | cmdb/handler.go | 精确路由 |
| 57 | `/api/v1/cmdb/ci/import` | cmdb/handler.go | 精确路由 |
| 58 | `/api/v1/cmdb/ci/pending` | cmdb/handler.go | 精确路由 |
| 59 | `/api/v1/cmdb/collect` | server_lifecycle.go | 精确路由 |
| 60 | `/api/v1/cmdb/relations` | cmdb/handler.go | 精确路由 |
| 61 | `/api/v1/cmdb/types` | cmdb/handler.go | 精确路由 |
| 62 | `/api/v1/compliance/reports` | server_lifecycle.go | 精确路由 |
| 63 | `/api/v1/compliance/reports/` | server_lifecycle.go | 前缀路由；子路径：{id} GET |
| 64 | `/api/v1/compliance/rules` | server_lifecycle.go | 精确路由 |
| 65 | `/api/v1/compliance/rules/` | server_lifecycle.go | 前缀路由；子路径：{id} GET |
| 66 | `/api/v1/compliance/scan` | server_lifecycle.go | 精确路由；POST 扫描设备合规状态 |
| 67 | `/api/v1/config/canary` | server_lifecycle.go | 精确路由 |
| 68 | `/api/v1/config/hotpush` | server_lifecycle.go | 精确路由 |
| 69 | `/api/v1/config/versions` | server_lifecycle.go | 精确路由 |
| 70 | `/api/v1/deploys` | deploy/handler.go, server_lifecycle.go | 精确路由 |
| 71 | `/api/v1/deploys/` | deploy/handler.go, server_lifecycle.go | 前缀路由 |
| 72 | `/api/v1/deploys/federation` | deploy/handler.go | 精确路由 |
| 73 | `/api/v1/deploys/federation/` | deploy/handler.go | 前缀路由 |
| 74 | `/api/v1/devices` | server_lifecycle.go | 精确路由 |
| 75 | `/api/v1/devices/` | server_lifecycle.go | 前缀路由；子路径：{id} DELETE 退役、{id}/provision |
| 76 | `/api/v1/events/stream` | server_lifecycle.go | 精确路由；SSE 实时推送（替代 5s 轮询） |
| 77 | `/api/v1/federation/devices` | server_lifecycle.go | 精确路由 |
| 78 | `/api/v1/federation/forward/task` | server_lifecycle.go | 精确路由 |
| 79 | `/api/v1/federation/peers` | server_lifecycle.go | 精确路由 |
| 80 | `/api/v1/gateway/routes` | server_lifecycle.go | 精确路由 |
| 81 | `/api/v1/gateway/routes/` | server_lifecycle.go | 前缀路由；子路径：{id} GET/PUT/DELETE、{id}/enable|disa |
| 82 | `/api/v1/gateway/stats` | server_lifecycle.go | 精确路由 |
| 83 | `/api/v1/gpu` | service_proxy.go | 精确路由；微服务聚合代理根 |
| 84 | `/api/v1/gpu/` | service_proxy.go | 前缀路由；微服务聚合代理前缀（子路径转发） |
| 85 | `/api/v1/ha/failover` | server_lifecycle.go | 精确路由；POST 手动切换 leader |
| 86 | `/api/v1/ha/health` | server_lifecycle.go | 精确路由；GET 健康检查 |
| 87 | `/api/v1/ha/instances` | server_lifecycle.go | 精确路由；GET 实例列表 |
| 88 | `/api/v1/ha/status` | server_lifecycle.go | 精确路由；GET HA 状态 |
| 89 | `/api/v1/helm/catalog` | server_lifecycle.go | 精确路由；预置应用目录 |
| 90 | `/api/v1/helm/charts/search` | server_lifecycle.go | 精确路由；?q=xxx 搜索 chart |
| 91 | `/api/v1/helm/releases` | server_lifecycle.go | 精确路由 |
| 92 | `/api/v1/helm/releases/` | server_lifecycle.go | 前缀路由；子路径：{name} PUT/DELETE、{name}/rollback、{n |
| 93 | `/api/v1/helm/repos` | server_lifecycle.go | 精确路由 |
| 94 | `/api/v1/helm/repos/` | server_lifecycle.go | 前缀路由；子路径：{name} DELETE、{name}/charts GET |
| 95 | `/api/v1/incidents` | service_proxy.go | 精确路由；微服务聚合代理根 |
| 96 | `/api/v1/incidents/` | service_proxy.go | 前缀路由；微服务聚合代理前缀（子路径转发） |
| 97 | `/api/v1/k8s/clusters` | server_lifecycle.go | 精确路由 |
| 98 | `/api/v1/k8s/clusters/` | server_lifecycle.go | 前缀路由；子路径：{id} 和 {id}/test |
| 99 | `/api/v1/logs` | logstore/handler.go | 精确路由 |
| 100 | `/api/v1/marketplace/plugins` | server_lifecycle.go | 精确路由 |
| 101 | `/api/v1/marketplace/plugins/` | server_lifecycle.go | 前缀路由；子路径：{id} GET/DELETE、{id}/install|uninsta |
| 102 | `/api/v1/me` | server_lifecycle.go | 精确路由 |
| 103 | `/api/v1/middleware-instances` | server_lifecycle.go | 精确路由 |
| 104 | `/api/v1/middleware-instances/` | server_lifecycle.go | 前缀路由；子路径：{id}/uninstall |
| 105 | `/api/v1/middleware-templates` | server_lifecycle.go | 精确路由 |
| 106 | `/api/v1/middleware-templates/` | server_lifecycle.go | 前缀路由；子路径：{id} 和 {id}/deploy |
| 107 | `/api/v1/network/connectivity` | server_lifecycle.go | 精确路由；POST 批量连通性检测 |
| 108 | `/api/v1/network/devices` | server_lifecycle.go | 精确路由 |
| 109 | `/api/v1/network/devices/` | server_lifecycle.go | 前缀路由；子路径：{id} GET/DELETE、{id}/metrics GET、{id |
| 110 | `/api/v1/network/diagnose` | server_lifecycle.go | 精确路由；POST 发起诊断任务 |
| 111 | `/api/v1/network/diagnose/` | server_lifecycle.go | 前缀路由；GET 子路径：{taskId} |
| 112 | `/api/v1/network/discover` | server_lifecycle.go | 精确路由；POST 网络发现 |
| 113 | `/api/v1/network/topology` | server_lifecycle.go | 精确路由；GET 拓扑图（?refresh=true 强制刷新） |
| 114 | `/api/v1/network/topology/cache` | server_lifecycle.go | 精确路由；GET 缓存拓扑（不触发探测） |
| 115 | `/api/v1/notify-channels` | server_lifecycle.go | 精确路由 |
| 116 | `/api/v1/notify-channels/` | server_lifecycle.go | 前缀路由；子路径：{id} PUT/DELETE、{id}/test POST |
| 117 | `/api/v1/notify-templates` | server_lifecycle.go | 精确路由 |
| 118 | `/api/v1/notify-templates/` | server_lifecycle.go | 前缀路由；子路径：{id} PUT/DELETE |
| 119 | `/api/v1/os-templates` | server_lifecycle.go | 精确路由 |
| 120 | `/api/v1/os-templates/` | server_lifecycle.go | 前缀路由；子路径：{id} 和 {id}/execute |
| 121 | `/api/v1/permissions` | server_lifecycle.go | 精确路由 |
| 122 | `/api/v1/pipeline/runs` | server_lifecycle.go | 精确路由 |
| 123 | `/api/v1/pipeline/runs/` | server_lifecycle.go | 前缀路由；子路径：{id} GET |
| 124 | `/api/v1/pipeline/templates` | server_lifecycle.go | 精确路由 |
| 125 | `/api/v1/pipeline/templates/` | server_lifecycle.go | 前缀路由；子路径：{id} GET/PUT/DELETE、{id}/run |
| 126 | `/api/v1/platform/config` | server_lifecycle.go | 精确路由；GET/PUT 平台配置 |
| 127 | `/api/v1/platform/health` | server_lifecycle.go | 精确路由；GET 平台健康检查 |
| 128 | `/api/v1/platform/metrics` | server_lifecycle.go | 精确路由；GET 平台指标汇总 |
| 129 | `/api/v1/portal` | service_proxy.go | 精确路由；微服务聚合代理根 |
| 130 | `/api/v1/portal/` | service_proxy.go | 前缀路由；微服务聚合代理前缀（子路径转发） |
| 131 | `/api/v1/provision/auto` | server_lifecycle.go | 精确路由 |
| 132 | `/api/v1/quotas` | server_lifecycle.go | 精确路由 |
| 133 | `/api/v1/quotas/` | server_lifecycle.go | 前缀路由；子路径：{tenantID} GET/PUT/DELETE |
| 134 | `/api/v1/roles` | server_lifecycle.go | 精确路由 |
| 135 | `/api/v1/roles/` | server_lifecycle.go | 前缀路由 |
| 136 | `/api/v1/runbooks` | service_proxy.go | 精确路由；微服务聚合代理根 |
| 137 | `/api/v1/runbooks/` | service_proxy.go | 前缀路由；微服务聚合代理前缀（子路径转发） |
| 138 | `/api/v1/schedules` | server_lifecycle.go | 精确路由；GET 列表 / POST 创建定时任务 |
| 139 | `/api/v1/schedules/` | server_lifecycle.go | 前缀路由；子路径：{id} GET/PUT/DELETE、{id}/pause、{id}/ |
| 140 | `/api/v1/scripts` | server_lifecycle.go | 精确路由 |
| 141 | `/api/v1/scripts/` | server_lifecycle.go | 前缀路由；子路径：{id} GET/PUT/DELETE、{id}/execute|exe |
| 142 | `/api/v1/secrets/keys` | server_lifecycle.go | 精确路由；GET 密钥 key 列表（仅名称 + provider） |
| 143 | `/api/v1/secrets/status` | server_lifecycle.go | 精确路由；GET 当前 provider 配置概览 |
| 144 | `/api/v1/secrets/test` | server_lifecycle.go | 精确路由；POST 测试 Vault 连接 |
| 145 | `/api/v1/slos` | server_lifecycle.go | 精确路由 |
| 146 | `/api/v1/slos/` | server_lifecycle.go | 前缀路由；子路径：{id} GET/PUT/DELETE、{id}/status GET |
| 147 | `/api/v1/tasks` | server_lifecycle.go | 精确路由 |
| 148 | `/api/v1/tasks/` | server_lifecycle.go | 前缀路由；子路径：{id}/cancel、{id}/result |
| 149 | `/api/v1/tasks/batch` | server_lifecycle.go | 精确路由；POST 批量下发 |
| 150 | `/api/v1/tasks/batch-exec` | server_lifecycle.go | 精确路由；POST 批量执行（M5 增强） |
| 151 | `/api/v1/tasks/batch/` | server_lifecycle.go | 前缀路由；GET 批量状态查询 |
| 152 | `/api/v1/tasks/canary` | server_lifecycle.go | 精确路由；POST 灰度发布 |
| 153 | `/api/v1/tasks/canary/` | server_lifecycle.go | 前缀路由；GET 灰度状态 / POST advance |
| 154 | `/api/v1/tenants` | server_lifecycle.go | 精确路由 |
| 155 | `/api/v1/tenants/` | server_lifecycle.go | 前缀路由；子路径：{id} GET/PUT/DELETE、{id}/suspend|act |
| 156 | `/api/v1/tickets` | server_lifecycle.go | 精确路由 |
| 157 | `/api/v1/tickets/` | server_lifecycle.go | 前缀路由；子路径：{id} GET/PUT、{id}/close POST |
| 158 | `/api/v1/traffic/policies` | server_lifecycle.go | 精确路由 |
| 159 | `/api/v1/traffic/policies/` | server_lifecycle.go | 前缀路由；子路径：{id} GET/PUT/DELETE、{id}/enable|disa |
| 160 | `/api/v1/users` | server_lifecycle.go | 精确路由 |
| 161 | `/api/v1/users/` | server_lifecycle.go | 前缀路由 |
| 162 | `/api/v1/webhooks` | server_lifecycle.go | 精确路由 |
| 163 | `/api/v1/webhooks/` | server_lifecycle.go | 前缀路由；子路径：{id} GET/PUT/DELETE、{id}/test|delive |
| 164 | `/api/v1/workflows` | orchestration/handler.go, server_lifecycle.go | 精确路由 |
| 165 | `/api/v1/workflows/` | orchestration/handler.go, server_lifecycle.go | 前缀路由 |

## 6. 结论和建议

### 6.1 结论

- **幽灵 API（契约断裂）：** 企业版 0 个，个人版 0 个，合计 **0** 个。
- **未接入 API（后端冗余/待接入）：** **4** 个。
- **企业版匹配率：** 100.0%（243/243）。
- **个人版匹配率：** 100.0%（120/120）。
- **后端路由接入率：** 97.6%（161/165）。

ℹ️ **存在 4 个未接入 API**，建议确认是待接入、内部使用还是可删除的冗余路由。

### 6.2 建议

1. **清理或接入未使用后端路由：**
   - `/api/v1/autoscaler`、`/api/v1/gpu`、`/api/v1/portal` 为微服务聚合代理根前缀，前端通过子路径（如 `/api/v1/gpu/models`）调用，根路径本身无需直接调用，属正常代理根路由。
   - `/api/v1/me` 为后端当前用户信息端点，前端已用 `/api/v1/auth/me` 替代，疑似遗留路由或 agent gRPC 通道内部使用，建议确认后删除以减少攻击面。
   - 确认是否为联邦/条件注册路由（如 `--federation-peers` 启用时才注册）。
2. **CI 集成建议：** 将本审计脚本纳入 `go test` 或 CI 流水线，每次 PR 自动校验 API 契约，幽灵 API 数 > 0 时阻断合并。

---

*本报告由 API 契约审计脚本自动生成于 2026-09-03 06:44:19，不修改任何代码，仅做静态分析。*