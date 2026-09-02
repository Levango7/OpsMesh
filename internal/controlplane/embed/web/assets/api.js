// api.js — OpsMesh Phase 1 前端 API 封装。
//
// 后端端点（Phase 1）：
//   工单：GET/POST /api/v1/tickets, GET/PUT /api/v1/tickets/{id}, POST /api/v1/tickets/{id}/close
//   SLO：GET/POST /api/v1/slos, GET/PUT/DELETE /api/v1/slos/{id}, GET /api/v1/slos/{id}/status
//   指标：GET /metrics（Prometheus text exposition format）
//
// 设计要点：
//   - 统一 fetch 封装，Promise 返回；
//   - JSON API 自动解析；/metrics 返回 text/plain；
//   - 租户头 X-Tenant-ID 从 localStorage(opsmesh_tenant) 读取，默认 "default"；
//   - 错误统一抛出 ApiError（含 status + message），便于 flow 层 catch 渲染。

// ApiError 统一 API 错误（携带 HTTP 状态码 + 后端 message）。
export class ApiError extends Error {
  constructor(status, message) {
    super(message || ('HTTP ' + status));
    this.name = 'ApiError';
    this.status = status;
  }
}

// getTenantID 读取当前租户上下文（X-Tenant-ID）。
// 优先级：localStorage("opsmesh_tenant") > "default"。
export function getTenantID() {
  try {
    const t = localStorage.getItem('opsmesh_tenant');
    if (t && t.trim()) return t.trim();
  } catch (_) { /* localStorage 不可用时静默回退 */ }
  return 'default';
}

// setTenantID 持久化当前租户上下文。
export function setTenantID(tenant) {
  try {
    if (tenant && tenant.trim()) {
      localStorage.setItem('opsmesh_tenant', tenant.trim());
    }
  } catch (_) { /* 静默 */ }
}

// authHeaders 构造请求头（含租户上下文 + JSON Content-Type）。
function authHeaders(extra) {
  const h = Object.assign(
    { 'X-Tenant-ID': getTenantID(), 'Accept': 'application/json' },
    extra || {}
  );
  return h;
}

// request JSON 通用封装：解析 JSON 响应，非 2xx 抛 ApiError。
async function requestJSON(method, url, body) {
  const opts = { method, headers: authHeaders() };
  if (body !== undefined && body !== null) {
    opts.headers['Content-Type'] = 'application/json';
    opts.body = JSON.stringify(body);
  }
  const resp = await fetch(url, opts);
  let data = null;
  const text = await resp.text();
  if (text) {
    try { data = JSON.parse(text); } catch (_) { data = text; }
  }
  if (!resp.ok) {
    const msg = (data && data.error) ? data.error : ('HTTP ' + resp.status);
    throw new ApiError(resp.status, msg);
  }
  return data;
}

// buildQuery 构造 URL 查询串（跳过空值）。
function buildQuery(filter) {
  if (!filter || typeof filter !== 'object') return '';
  const params = [];
  for (const k of Object.keys(filter)) {
    const v = filter[k];
    if (v !== undefined && v !== null && String(v).trim() !== '') {
      params.push(encodeURIComponent(k) + '=' + encodeURIComponent(String(v).trim()));
    }
  }
  return params.length ? '?' + params.join('&') : '';
}

// ============================================================================
// 工单管理 API
// ============================================================================

// getTickets 列出工单（支持 status/priority/category/assigneeID 过滤）。
// GET /api/v1/tickets?status=&priority=&category=&assigneeID=
export function getTickets(filter = {}) {
  return requestJSON('GET', '/api/v1/tickets' + buildQuery(filter)).then((d) => {
    return (d && d.tickets) ? d.tickets : [];
  });
}

// createTicket 创建工单。POST /api/v1/tickets
// body: {title, description, priority, category, assigneeID, creatorID, relatedDevice, relatedTask, tags}
export function createTicket(data) {
  return requestJSON('POST', '/api/v1/tickets', data);
}

// getTicket 获取工单详情。GET /api/v1/tickets/{id}
export function getTicket(id) {
  return requestJSON('GET', '/api/v1/tickets/' + encodeURIComponent(id));
}

// updateTicket 更新工单。PUT /api/v1/tickets/{id}
export function updateTicket(id, data) {
  return requestJSON('PUT', '/api/v1/tickets/' + encodeURIComponent(id), data);
}

// closeTicket 关闭工单。POST /api/v1/tickets/{id}/close
export function closeTicket(id) {
  return requestJSON('POST', '/api/v1/tickets/' + encodeURIComponent(id) + '/close');
}

// ============================================================================
// SLO 管理 API
// ============================================================================

// getSLOs 列出 SLO。GET /api/v1/slos
export function getSLOs() {
  return requestJSON('GET', '/api/v1/slos').then((d) => {
    return (d && d.slos) ? d.slos : [];
  });
}

// createSLO 创建 SLO。POST /api/v1/slos
// body: {name, description, serviceName, target, window, slis}
export function createSLO(data) {
  return requestJSON('POST', '/api/v1/slos', data);
}

// getSLO 获取 SLO 详情。GET /api/v1/slos/{id}
export function getSLO(id) {
  return requestJSON('GET', '/api/v1/slos/' + encodeURIComponent(id));
}

// updateSLO 更新 SLO。PUT /api/v1/slos/{id}
export function updateSLO(id, data) {
  return requestJSON('PUT', '/api/v1/slos/' + encodeURIComponent(id), data);
}

// deleteSLO 删除 SLO。DELETE /api/v1/slos/{id}（成功返回 204 无 body）。
export function deleteSLO(id) {
  return requestJSON('DELETE', '/api/v1/slos/' + encodeURIComponent(id));
}

// getSLOStatus 获取 SLI 状态。GET /api/v1/slos/{id}/status
// 返回 {statuses: [{sliName, currentValue, targetValue, status, lastEvaluated}]}
export function getSLOStatus(id) {
  return requestJSON('GET', '/api/v1/slos/' + encodeURIComponent(id) + '/status').then((d) => {
    return (d && d.statuses) ? d.statuses : [];
  });
}

// ============================================================================
// 监控指标 API
// ============================================================================

// getMetrics 获取 Prometheus 指标（text exposition format）。
// GET /metrics（返回 text/plain; version=0.0.4）。
export async function getMetrics() {
  const resp = await fetch('/metrics', { headers: { 'X-Tenant-ID': getTenantID() } });
  const text = await resp.text();
  if (!resp.ok) {
    throw new ApiError(resp.status, text || ('HTTP ' + resp.status));
  }
  return text;
}

// ============================================================================
// Phase 2：流量治理 API
// ============================================================================

// getTrafficPolicies 列出流量策略。GET /api/v1/traffic/policies
export function getTrafficPolicies() {
  return requestJSON('GET', '/api/v1/traffic/policies').then((d) => {
    return (d && d.policies) ? d.policies : (Array.isArray(d) ? d : []);
  });
}

// createTrafficPolicy 创建流量策略。POST /api/v1/traffic/policies
// body: {name, service, type, timeout, retries, ...}
export function createTrafficPolicy(data) {
  return requestJSON('POST', '/api/v1/traffic/policies', data);
}

// deleteTrafficPolicy 删除流量策略。DELETE /api/v1/traffic/policies/{id}
export function deleteTrafficPolicy(id) {
  return requestJSON('DELETE', '/api/v1/traffic/policies/' + encodeURIComponent(id));
}

// enableTrafficPolicy 启用流量策略。POST /api/v1/traffic/policies/{id}/enable
export function enableTrafficPolicy(id) {
  return requestJSON('POST', '/api/v1/traffic/policies/' + encodeURIComponent(id) + '/enable');
}

// disableTrafficPolicy 禁用流量策略。POST /api/v1/traffic/policies/{id}/disable
export function disableTrafficPolicy(id) {
  return requestJSON('POST', '/api/v1/traffic/policies/' + encodeURIComponent(id) + '/disable');
}

// ============================================================================
// Phase 2：CI/CD 流水线 API
// ============================================================================

// getPipelineTemplates 列出流水线模板。GET /api/v1/pipeline/templates
export function getPipelineTemplates() {
  return requestJSON('GET', '/api/v1/pipeline/templates').then((d) => {
    return (d && d.templates) ? d.templates : (Array.isArray(d) ? d : []);
  });
}

// createPipelineTemplate 创建流水线模板。POST /api/v1/pipeline/templates
export function createPipelineTemplate(data) {
  return requestJSON('POST', '/api/v1/pipeline/templates', data);
}

// deletePipelineTemplate 删除流水线模板。DELETE /api/v1/pipeline/templates/{id}
export function deletePipelineTemplate(id) {
  return requestJSON('DELETE', '/api/v1/pipeline/templates/' + encodeURIComponent(id));
}

// runPipeline 触发流水线运行。POST /api/v1/pipeline/templates/{id}/run
export function runPipeline(id) {
  return requestJSON('POST', '/api/v1/pipeline/templates/' + encodeURIComponent(id) + '/run');
}

// getPipelineRuns 列出流水线运行记录。GET /api/v1/pipeline/runs
export function getPipelineRuns() {
  return requestJSON('GET', '/api/v1/pipeline/runs').then((d) => {
    return (d && d.runs) ? d.runs : (Array.isArray(d) ? d : []);
  });
}

// ============================================================================
// Phase 2：ArgoCD 应用 API
// ============================================================================

// getArgoCDApps 列出 ArgoCD 应用。GET /api/v1/argocd/apps
export function getArgoCDApps() {
  return requestJSON('GET', '/api/v1/argocd/apps').then((d) => {
    return (d && d.apps) ? d.apps : (Array.isArray(d) ? d : []);
  });
}

// createArgoCDApp 创建 ArgoCD 应用。POST /api/v1/argocd/apps
export function createArgoCDApp(data) {
  return requestJSON('POST', '/api/v1/argocd/apps', data);
}

// deleteArgoCDApp 删除 ArgoCD 应用。DELETE /api/v1/argocd/apps/{id}
export function deleteArgoCDApp(id) {
  return requestJSON('DELETE', '/api/v1/argocd/apps/' + encodeURIComponent(id));
}

// syncArgoCDApp 同步 ArgoCD 应用。POST /api/v1/argocd/apps/{id}/sync
export function syncArgoCDApp(id) {
  return requestJSON('POST', '/api/v1/argocd/apps/' + encodeURIComponent(id) + '/sync');
}

// ============================================================================
// Phase 2：灰度发布增强 API
// ============================================================================

// getCanaryReleases 列出灰度发布。GET /api/v1/tasks/canary
// 注：后端无 /api/v1/canary/releases，改用 /api/v1/tasks/canary（灰度任务列表）。
export function getCanaryReleases() {
  return requestJSON('GET', '/api/v1/tasks/canary').then((d) => {
    return (d && d.releases) ? d.releases : (Array.isArray(d) ? d : []);
  });
}

// setTrafficSplit 设置灰度流量分割百分比。POST /api/v1/canary/{id}/traffic-split
export function setTrafficSplit(id, percent) {
  return requestJSON('POST', '/api/v1/canary/' + encodeURIComponent(id) + '/traffic-split', { percent });
}

// getCanaryMetrics 获取灰度指标对比。GET /api/v1/canary/{id}/metrics
export function getCanaryMetrics(id) {
  return requestJSON('GET', '/api/v1/canary/' + encodeURIComponent(id) + '/metrics');
}

// ============================================================================
// Phase 2：配置热推送 API
// ============================================================================

// hotpushConfig 配置热推送。POST /api/v1/config/hotpush
// body: {deviceID, key, value, path}
export function hotpushConfig(data) {
  return requestJSON('POST', '/api/v1/config/hotpush', data);
}

// canaryConfig 灰度配置发布。POST /api/v1/config/canary
// body: {devices, percent, content, key}
export function canaryConfig(data) {
  return requestJSON('POST', '/api/v1/config/canary', data);
}

// getConfigVersions 查询配置版本历史。GET /api/v1/config/versions?key=
export function getConfigVersions(key) {
  const q = key ? '?key=' + encodeURIComponent(key) : '';
  return requestJSON('GET', '/api/v1/config/versions' + q).then((d) => {
    return (d && d.versions) ? d.versions : (Array.isArray(d) ? d : []);
  });
}

// ============================================================================
// Phase 3：安全合规 API（合规规则 / 合规扫描 / 合规报告 / 审计日志）
// ============================================================================

// getComplianceRules 获取合规规则列表。GET /api/v1/compliance/rules
export function getComplianceRules() {
  return requestJSON('GET', '/api/v1/compliance/rules').then((d) => {
    return (d && d.rules) ? d.rules : (Array.isArray(d) ? d : []);
  });
}

// getComplianceRule 获取单个合规规则详情。GET /api/v1/compliance/rules/{id}
export function getComplianceRule(id) {
  return requestJSON('GET', '/api/v1/compliance/rules/' + encodeURIComponent(id));
}

// scanCompliance 对指定设备发起合规扫描。POST /api/v1/compliance/scan
export function scanCompliance(deviceID) {
  return requestJSON('POST', '/api/v1/compliance/scan', { deviceID });
}

// getComplianceReports 获取合规报告列表。GET /api/v1/compliance/reports
export function getComplianceReports() {
  return requestJSON('GET', '/api/v1/compliance/reports').then((d) => {
    return (d && d.reports) ? d.reports : (Array.isArray(d) ? d : []);
  });
}

// getComplianceReport 获取单个合规报告详情。GET /api/v1/compliance/reports/{id}
export function getComplianceReport(id) {
  return requestJSON('GET', '/api/v1/compliance/reports/' + encodeURIComponent(id));
}

// getAuditEvents 查询审计事件。GET /api/v1/audit/events?{params}
// params: { from, to, user, action, limit }
export function getAuditEvents(params) {
  const q = buildQuery(params);
  return requestJSON('GET', '/api/v1/audit/events' + q).then((d) => {
    return (d && d.events) ? d.events : (Array.isArray(d) ? d : []);
  });
}

// exportAuditLogs 导出审计日志。GET /api/v1/audit/export?{params}
export function exportAuditLogs(params) {
  const q = buildQuery(params);
  return requestJSON('GET', '/api/v1/audit/export' + q);
}

// ============================================================================
// Phase 3：高可用 API（HA 状态 / failover / 灾备恢复）
// ============================================================================

// getHAStatus 获取 HA 状态。GET /api/v1/ha/status
export function getHAStatus() {
  return requestJSON('GET', '/api/v1/ha/status');
}

// getHAInstances 获取 HA 实例列表。GET /api/v1/ha/instances
export function getHAInstances() {
  return requestJSON('GET', '/api/v1/ha/instances').then((d) => {
    return (d && d.instances) ? d.instances : (Array.isArray(d) ? d : []);
  });
}

// failoverHA 触发手动 failover。POST /api/v1/ha/failover
export function failoverHA() {
  return requestJSON('POST', '/api/v1/ha/failover');
}

// getHAHealth 获取 HA 健康状态。GET /api/v1/ha/health
export function getHAHealth() {
  return requestJSON('GET', '/api/v1/ha/health');
}

// createBackup 创建备份。POST /api/v1/backup/create  body: {type}
export function createBackup(type) {
  return requestJSON('POST', '/api/v1/backup/create', { type });
}

// listBackups 列出备份。GET /api/v1/backup/list
export function listBackups() {
  return requestJSON('GET', '/api/v1/backup/list').then((d) => {
    return (d && d.backups) ? d.backups : (Array.isArray(d) ? d : []);
  });
}

// restoreBackup 恢复备份。POST /api/v1/backup/restore  body: {id}
export function restoreBackup(id) {
  return requestJSON('POST', '/api/v1/backup/restore', { id });
}

// deleteBackup 删除备份。DELETE /api/v1/backup/{id}
export function deleteBackup(id) {
  return requestJSON('DELETE', '/api/v1/backup/' + encodeURIComponent(id));
}

// ============================================================================
// Phase 4：网络管理
// ============================================================================

// getNetworkDevices 列出网络设备。GET /api/v1/network/devices
export function getNetworkDevices() {
  return requestJSON('GET', '/api/v1/network/devices').then((d) => {
    return Array.isArray(d) ? d : (d && d.devices ? d.devices : []);
  });
}

// createNetworkDevice 创建网络设备。POST /api/v1/network/devices
export function createNetworkDevice(data) {
  return requestJSON('POST', '/api/v1/network/devices', data);
}

// getNetworkDevice 获取网络设备详情。GET /api/v1/network/devices/{id}
export function getNetworkDevice(id) {
  return requestJSON('GET', '/api/v1/network/devices/' + encodeURIComponent(id));
}

// deleteNetworkDevice 删除网络设备。DELETE /api/v1/network/devices/{id}
export function deleteNetworkDevice(id) {
  return requestJSON('DELETE', '/api/v1/network/devices/' + encodeURIComponent(id));
}

// getNetworkDeviceMetrics 获取网络设备监控指标。GET /api/v1/network/devices/{id}/metrics
export function getNetworkDeviceMetrics(id) {
  return requestJSON('GET', '/api/v1/network/devices/' + encodeURIComponent(id) + '/metrics');
}

// configNetworkDevice 下发配置到网络设备。POST /api/v1/network/devices/{id}/config
export function configNetworkDevice(id, config) {
  return requestJSON('POST', '/api/v1/network/devices/' + encodeURIComponent(id) + '/config', { config: config });
}

// discoverNetwork 网络发现（扫描子网）。POST /api/v1/network/discover  body: {subnet}
export function discoverNetwork(subnet) {
  return requestJSON('POST', '/api/v1/network/discover', { subnet: subnet }).then((d) => {
    return Array.isArray(d) ? d : (d && d.devices ? d.devices : []);
  });
}

// ============================================================================
// Phase 4：自动化闭环
// ============================================================================

// getAutomationRules 列出自动化规则。GET /api/v1/automation/rules
export function getAutomationRules() {
  return requestJSON('GET', '/api/v1/automation/rules').then((d) => {
    return Array.isArray(d) ? d : (d && d.rules ? d.rules : []);
  });
}

// createAutomationRule 创建自动化规则。POST /api/v1/automation/rules
export function createAutomationRule(data) {
  return requestJSON('POST', '/api/v1/automation/rules', data);
}

// getAutomationRule 获取自动化规则详情。GET /api/v1/automation/rules/{id}
export function getAutomationRule(id) {
  return requestJSON('GET', '/api/v1/automation/rules/' + encodeURIComponent(id));
}

// updateAutomationRule 更新自动化规则。PUT /api/v1/automation/rules/{id}
export function updateAutomationRule(id, data) {
  return requestJSON('PUT', '/api/v1/automation/rules/' + encodeURIComponent(id), data);
}

// deleteAutomationRule 删除自动化规则。DELETE /api/v1/automation/rules/{id}
export function deleteAutomationRule(id) {
  return requestJSON('DELETE', '/api/v1/automation/rules/' + encodeURIComponent(id));
}

// enableAutomationRule 启用自动化规则。POST /api/v1/automation/rules/{id}/enable
export function enableAutomationRule(id) {
  return requestJSON('POST', '/api/v1/automation/rules/' + encodeURIComponent(id) + '/enable');
}

// disableAutomationRule 禁用自动化规则。POST /api/v1/automation/rules/{id}/disable
export function disableAutomationRule(id) {
  return requestJSON('POST', '/api/v1/automation/rules/' + encodeURIComponent(id) + '/disable');
}

// testAutomationRule 测试自动化规则。POST /api/v1/automation/rules/{id}/test
export function testAutomationRule(id) {
  return requestJSON('POST', '/api/v1/automation/rules/' + encodeURIComponent(id) + '/test');
}

// getAutomationExecutions 列出自动化执行历史。GET /api/v1/automation/executions
export function getAutomationExecutions() {
  return requestJSON('GET', '/api/v1/automation/executions').then((d) => {
    return Array.isArray(d) ? d : (d && d.executions ? d.executions : []);
  });
}

// getAutomationExecution 获取自动化执行详情。GET /api/v1/automation/executions/{id}
export function getAutomationExecution(id) {
  return requestJSON('GET', '/api/v1/automation/executions/' + encodeURIComponent(id));
}

// ============================================================================
// Phase 5：扩展能力 API（API 网关 / Webhook / 自定义脚本）
// ============================================================================

// --- API 网关 ---

// getGatewayRoutes 列出网关路由。GET /api/v1/gateway/routes
export function getGatewayRoutes() {
  return requestJSON('GET', '/api/v1/gateway/routes').then((d) => {
    return Array.isArray(d) ? d : (d && d.routes ? d.routes : []);
  });
}

// createGatewayRoute 创建网关路由。POST /api/v1/gateway/routes
// body: {name, method, path, target, ...}
export function createGatewayRoute(route) {
  return requestJSON('POST', '/api/v1/gateway/routes', route);
}

// getGatewayRoute 获取网关路由详情。GET /api/v1/gateway/routes/{id}
export function getGatewayRoute(id) {
  return requestJSON('GET', '/api/v1/gateway/routes/' + encodeURIComponent(id));
}

// updateGatewayRoute 更新网关路由。PUT /api/v1/gateway/routes/{id}
export function updateGatewayRoute(id, route) {
  return requestJSON('PUT', '/api/v1/gateway/routes/' + encodeURIComponent(id), route);
}

// deleteGatewayRoute 删除网关路由。DELETE /api/v1/gateway/routes/{id}
export function deleteGatewayRoute(id) {
  return requestJSON('DELETE', '/api/v1/gateway/routes/' + encodeURIComponent(id));
}

// toggleGatewayRoute 启用/禁用网关路由。POST /api/v1/gateway/routes/{id}/enable|disable
// action: 'enable' | 'disable'
export function toggleGatewayRoute(id, action) {
  return requestJSON('POST', '/api/v1/gateway/routes/' + encodeURIComponent(id) + '/' + (action === 'disable' ? 'disable' : 'enable'));
}

// getGatewayStats 获取网关统计。GET /api/v1/gateway/stats
export function getGatewayStats() {
  return requestJSON('GET', '/api/v1/gateway/stats');
}

// --- Webhook ---

// getWebhooks 列出 Webhook。GET /api/v1/webhooks
export function getWebhooks() {
  return requestJSON('GET', '/api/v1/webhooks').then((d) => {
    return Array.isArray(d) ? d : (d && d.webhooks ? d.webhooks : []);
  });
}

// createWebhook 创建 Webhook。POST /api/v1/webhooks
// body: {name, url, event, secret, ...}
export function createWebhook(wh) {
  return requestJSON('POST', '/api/v1/webhooks', wh);
}

// getWebhook 获取 Webhook 详情。GET /api/v1/webhooks/{id}
export function getWebhook(id) {
  return requestJSON('GET', '/api/v1/webhooks/' + encodeURIComponent(id));
}

// updateWebhook 更新 Webhook。PUT /api/v1/webhooks/{id}
export function updateWebhook(id, wh) {
  return requestJSON('PUT', '/api/v1/webhooks/' + encodeURIComponent(id), wh);
}

// deleteWebhook 删除 Webhook。DELETE /api/v1/webhooks/{id}
export function deleteWebhook(id) {
  return requestJSON('DELETE', '/api/v1/webhooks/' + encodeURIComponent(id));
}

// testWebhook 测试发送 Webhook。POST /api/v1/webhooks/{id}/test
export function testWebhook(id) {
  return requestJSON('POST', '/api/v1/webhooks/' + encodeURIComponent(id) + '/test');
}

// getWebhookDeliveries 获取 Webhook 投递记录。GET /api/v1/webhooks/{id}/deliveries
export function getWebhookDeliveries(id) {
  return requestJSON('GET', '/api/v1/webhooks/' + encodeURIComponent(id) + '/deliveries').then((d) => {
    return Array.isArray(d) ? d : (d && d.deliveries ? d.deliveries : []);
  });
}

// --- 自定义脚本 ---

// getScripts 列出自定义脚本。GET /api/v1/scripts
export function getScripts() {
  return requestJSON('GET', '/api/v1/scripts').then((d) => {
    return Array.isArray(d) ? d : (d && d.scripts ? d.scripts : []);
  });
}

// createScript 创建自定义脚本。POST /api/v1/scripts
// body: {name, runtime, code, description, ...}
export function createScript(s) {
  return requestJSON('POST', '/api/v1/scripts', s);
}

// getScript 获取自定义脚本详情。GET /api/v1/scripts/{id}
export function getScript(id) {
  return requestJSON('GET', '/api/v1/scripts/' + encodeURIComponent(id));
}

// updateScript 更新自定义脚本。PUT /api/v1/scripts/{id}
export function updateScript(id, s) {
  return requestJSON('PUT', '/api/v1/scripts/' + encodeURIComponent(id), s);
}

// deleteScript 删除自定义脚本。DELETE /api/v1/scripts/{id}
export function deleteScript(id) {
  return requestJSON('DELETE', '/api/v1/scripts/' + encodeURIComponent(id));
}

// executeScript 执行自定义脚本。POST /api/v1/scripts/{id}/execute
// body: {deviceId, params}
export function executeScript(id, body) {
  return requestJSON('POST', '/api/v1/scripts/' + encodeURIComponent(id) + '/execute', body || {});
}

// getScriptExecutions 获取脚本执行历史。GET /api/v1/scripts/{id}/executions
export function getScriptExecutions(id) {
  return requestJSON('GET', '/api/v1/scripts/' + encodeURIComponent(id) + '/executions').then((d) => {
    return Array.isArray(d) ? d : (d && d.executions ? d.executions : []);
  });
}

// ============================================================================
// Phase 6：平台化管理 API（租户 / API Key / 插件市场 / 计费订阅 / 平台配置）
// ============================================================================

// --- 租户管理 ---

// getTenants 列出租户。GET /api/v1/tenants
export function getTenants(filter = {}) {
  return requestJSON('GET', '/api/v1/tenants' + buildQuery(filter)).then((d) => {
    return Array.isArray(d) ? d : (d && d.tenants ? d.tenants : []);
  });
}

// createTenant 创建租户。POST /api/v1/tenants
// body: {name, code, description, plan, ...}
export function createTenant(data) {
  return requestJSON('POST', '/api/v1/tenants', data);
}

// getTenant 获取租户详情。GET /api/v1/tenants/{id}
export function getTenant(id) {
  return requestJSON('GET', '/api/v1/tenants/' + encodeURIComponent(id));
}

// updateTenant 更新租户。PUT /api/v1/tenants/{id}
export function updateTenant(id, data) {
  return requestJSON('PUT', '/api/v1/tenants/' + encodeURIComponent(id), data);
}

// deleteTenant 删除租户。DELETE /api/v1/tenants/{id}
export function deleteTenant(id) {
  return requestJSON('DELETE', '/api/v1/tenants/' + encodeURIComponent(id));
}

// suspendTenant 暂停租户。POST /api/v1/tenants/{id}/suspend
export function suspendTenant(id) {
  return requestJSON('POST', '/api/v1/tenants/' + encodeURIComponent(id) + '/suspend');
}

// activateTenant 激活租户。POST /api/v1/tenants/{id}/activate
export function activateTenant(id) {
  return requestJSON('POST', '/api/v1/tenants/' + encodeURIComponent(id) + '/activate');
}

// --- API Key 管理 ---

// getAPIKeys 列出 API Key。GET /api/v1/apikeys
export function getAPIKeys(filter = {}) {
  return requestJSON('GET', '/api/v1/apikeys' + buildQuery(filter)).then((d) => {
    return Array.isArray(d) ? d : (d && d.apikeys ? d.apikeys : (d && d.keys ? d.keys : []));
  });
}

// createAPIKey 创建 API Key（后端生成 key 并返回）。POST /api/v1/apikeys
// body: {name, scopes, expiresAt, ...}
export function createAPIKey(data) {
  return requestJSON('POST', '/api/v1/apikeys', data);
}

// getAPIKey 获取 API Key 详情。GET /api/v1/apikeys/{id}
export function getAPIKey(id) {
  return requestJSON('GET', '/api/v1/apikeys/' + encodeURIComponent(id));
}

// updateAPIKey 更新 API Key。PUT /api/v1/apikeys/{id}
// 安全：仅提交白名单可编辑字段 {name, scopes}。enabled 必须走 toggleAPIKey
// （enable/disable 端点）；key/id 等敏感字段后端会强制保留 existing 值。
export function updateAPIKey(id, data) {
  return requestJSON('PUT', '/api/v1/apikeys/' + encodeURIComponent(id), data);
}

// deleteAPIKey 删除 API Key。DELETE /api/v1/apikeys/{id}
export function deleteAPIKey(id) {
  return requestJSON('DELETE', '/api/v1/apikeys/' + encodeURIComponent(id));
}

// toggleAPIKey 启用/禁用 API Key。POST /api/v1/apikeys/{id}/enable|disable
// action: 'enable' | 'disable'
export function toggleAPIKey(id, action) {
  return requestJSON('POST', '/api/v1/apikeys/' + encodeURIComponent(id) + '/' + (action === 'disable' ? 'disable' : 'enable'));
}

// --- 插件市场 ---

// getPlugins 列出市场插件。GET /api/v1/marketplace/plugins
export function getPlugins(filter = {}) {
  return requestJSON('GET', '/api/v1/marketplace/plugins' + buildQuery(filter)).then((d) => {
    return Array.isArray(d) ? d : (d && d.plugins ? d.plugins : []);
  });
}

// createPlugin 注册插件。POST /api/v1/marketplace/plugins
// body: {name, version, description, source, ...}
export function createPlugin(data) {
  return requestJSON('POST', '/api/v1/marketplace/plugins', data);
}

// getPlugin 获取插件详情。GET /api/v1/marketplace/plugins/{id}
export function getPlugin(id) {
  return requestJSON('GET', '/api/v1/marketplace/plugins/' + encodeURIComponent(id));
}

// deletePlugin 删除插件。DELETE /api/v1/marketplace/plugins/{id}
export function deletePlugin(id) {
  return requestJSON('DELETE', '/api/v1/marketplace/plugins/' + encodeURIComponent(id));
}

// installPlugin 安装插件。POST /api/v1/marketplace/plugins/{id}/install
export function installPlugin(id) {
  return requestJSON('POST', '/api/v1/marketplace/plugins/' + encodeURIComponent(id) + '/install');
}

// uninstallPlugin 卸载插件。POST /api/v1/marketplace/plugins/{id}/uninstall
export function uninstallPlugin(id) {
  return requestJSON('POST', '/api/v1/marketplace/plugins/' + encodeURIComponent(id) + '/uninstall');
}

// togglePlugin 启用/禁用插件。POST /api/v1/marketplace/plugins/{id}/enable|disable
// action: 'enable' | 'disable'
export function togglePlugin(id, action) {
  return requestJSON('POST', '/api/v1/marketplace/plugins/' + encodeURIComponent(id) + '/' + (action === 'disable' ? 'disable' : 'enable'));
}

// --- 计费订阅 ---

// getBillingPlans 列出计费计划。GET /api/v1/billing/plans
export function getBillingPlans(filter = {}) {
  return requestJSON('GET', '/api/v1/billing/plans' + buildQuery(filter)).then((d) => {
    return Array.isArray(d) ? d : (d && d.plans ? d.plans : []);
  });
}

// createBillingPlan 创建计费计划。POST /api/v1/billing/plans
// body: {name, price, interval, features, ...}
export function createBillingPlan(data) {
  return requestJSON('POST', '/api/v1/billing/plans', data);
}

// getBillingPlan 获取计费计划详情。GET /api/v1/billing/plans/{id}
export function getBillingPlan(id) {
  return requestJSON('GET', '/api/v1/billing/plans/' + encodeURIComponent(id));
}

// updateBillingPlan 更新计费计划。PUT /api/v1/billing/plans/{id}
export function updateBillingPlan(id, data) {
  return requestJSON('PUT', '/api/v1/billing/plans/' + encodeURIComponent(id), data);
}

// deleteBillingPlan 删除计费计划。DELETE /api/v1/billing/plans/{id}
export function deleteBillingPlan(id) {
  return requestJSON('DELETE', '/api/v1/billing/plans/' + encodeURIComponent(id));
}

// getSubscriptions 列出订阅。GET /api/v1/billing/subscriptions
export function getSubscriptions(filter = {}) {
  return requestJSON('GET', '/api/v1/billing/subscriptions' + buildQuery(filter)).then((d) => {
    return Array.isArray(d) ? d : (d && d.subscriptions ? d.subscriptions : []);
  });
}

// createSubscription 创建订阅。POST /api/v1/billing/subscriptions
// body: {tenantID, planID, ...}
export function createSubscription(data) {
  return requestJSON('POST', '/api/v1/billing/subscriptions', data);
}

// getSubscription 获取订阅详情。GET /api/v1/billing/subscriptions/{id}
export function getSubscription(id) {
  return requestJSON('GET', '/api/v1/billing/subscriptions/' + encodeURIComponent(id));
}

// updateSubscription 更新订阅。PUT /api/v1/billing/subscriptions/{id}
export function updateSubscription(id, data) {
  return requestJSON('PUT', '/api/v1/billing/subscriptions/' + encodeURIComponent(id), data);
}

// deleteSubscription 删除订阅。DELETE /api/v1/billing/subscriptions/{id}
export function deleteSubscription(id) {
  return requestJSON('DELETE', '/api/v1/billing/subscriptions/' + encodeURIComponent(id));
}

// getInvoices 列出账单。GET /api/v1/billing/invoices
export function getInvoices(filter = {}) {
  return requestJSON('GET', '/api/v1/billing/invoices' + buildQuery(filter)).then((d) => {
    return Array.isArray(d) ? d : (d && d.invoices ? d.invoices : []);
  });
}

// getInvoice 获取账单详情。GET /api/v1/billing/invoices/{id}
export function getInvoice(id) {
  return requestJSON('GET', '/api/v1/billing/invoices/' + encodeURIComponent(id));
}

// --- 平台配置 ---

// getPlatformConfig 获取平台配置。GET /api/v1/platform/config
export function getPlatformConfig() {
  return requestJSON('GET', '/api/v1/platform/config');
}

// updatePlatformConfig 更新平台配置。PUT /api/v1/platform/config
export function updatePlatformConfig(data) {
  return requestJSON('PUT', '/api/v1/platform/config', data);
}

// getPlatformHealth 平台健康检查。GET /api/v1/platform/health
export function getPlatformHealth() {
  return requestJSON('GET', '/api/v1/platform/health');
}

// getPlatformMetrics 获取平台运行指标。GET /api/v1/platform/metrics
export function getPlatformMetrics() {
  return requestJSON('GET', '/api/v1/platform/metrics');
}

// ============================================================================
// P0 补齐功能域：设备管理 API
// ============================================================================

// getDevices 列出设备。GET /api/v1/devices
// filter: {status, type, agentID, ...}
export function getDevices(filter = {}) {
  return requestJSON('GET', '/api/v1/devices' + buildQuery(filter)).then((d) => {
    return Array.isArray(d) ? d : (d && d.devices ? d.devices : []);
  });
}

// retireDevice 退役设备。DELETE /api/v1/devices/{id}
export function retireDevice(id) {
  return requestJSON('DELETE', '/api/v1/devices/' + encodeURIComponent(id));
}

// getDeviceMetrics 获取设备监控指标。GET /api/v1/devices/{id}/metrics
export function getDeviceMetrics(id) {
  return requestJSON('GET', '/api/v1/devices/' + encodeURIComponent(id) + '/metrics');
}

// getAgents 列出代理。GET /api/v1/agents
export function getAgents(filter = {}) {
  return requestJSON('GET', '/api/v1/agents' + buildQuery(filter)).then((d) => {
    return Array.isArray(d) ? d : (d && d.agents ? d.agents : []);
  });
}

// ============================================================================
// P0 补齐功能域：任务执行 API
// ============================================================================

// getTasks 列出任务。GET /api/v1/tasks
// filter: {status, type, deviceID, ...}
export function getTasks(filter = {}) {
  return requestJSON('GET', '/api/v1/tasks' + buildQuery(filter)).then((d) => {
    return Array.isArray(d) ? d : (d && d.tasks ? d.tasks : []);
  });
}

// createTask 创建任务。POST /api/v1/tasks
// body: {name, type, deviceID, payload, priority, ...}
export function createTask(data) {
  return requestJSON('POST', '/api/v1/tasks', data);
}

// cancelTask 取消任务。POST /api/v1/tasks/{id}/cancel
export function cancelTask(id) {
  return requestJSON('POST', '/api/v1/tasks/' + encodeURIComponent(id) + '/cancel');
}

// getTaskResult 获取任务结果。GET /api/v1/tasks/{id}/result
export function getTaskResult(id) {
  return requestJSON('GET', '/api/v1/tasks/' + encodeURIComponent(id) + '/result');
}

// ============================================================================
// P0 补齐功能域：告警管理 API
// ============================================================================

// getAlerts 列出告警。GET /api/v1/alerts
// filter: {severity, state, source, ...}
export function getAlerts(filter = {}) {
  return requestJSON('GET', '/api/v1/alerts' + buildQuery(filter)).then((d) => {
    return Array.isArray(d) ? d : (d && d.alerts ? d.alerts : []);
  });
}

// ackAlert 确认告警。POST /api/v1/alerts/{id}/ack
export function ackAlert(id) {
  return requestJSON('POST', '/api/v1/alerts/' + encodeURIComponent(id) + '/ack');
}

// silenceAlert 静默告警。POST /api/v1/alerts/{id}/silence
// body: {duration, reason}
export function silenceAlert(id, data) {
  return requestJSON('POST', '/api/v1/alerts/' + encodeURIComponent(id) + '/silence', data || {});
}

// ============================================================================
// P0 补齐功能域：告警规则管理 API（规则 CRUD / 多条件引擎 / 静默规则）
// ============================================================================

// --- 告警规则 ---

// getAlertRules 列出告警规则。GET /api/v1/alert-rules
export function getAlertRules(filter = {}) {
  return requestJSON('GET', '/api/v1/alert-rules' + buildQuery(filter)).then((d) => {
    return Array.isArray(d) ? d : (d && d.rules ? d.rules : []);
  });
}

// createAlertRule 创建告警规则。POST /api/v1/alert-rules
export function createAlertRule(data) {
  return requestJSON('POST', '/api/v1/alert-rules', data);
}

// getAlertRule 获取告警规则详情。GET /api/v1/alert-rules/{id}
export function getAlertRule(id) {
  return requestJSON('GET', '/api/v1/alert-rules/' + encodeURIComponent(id));
}

// updateAlertRule 更新告警规则。PUT /api/v1/alert-rules/{id}
export function updateAlertRule(id, data) {
  return requestJSON('PUT', '/api/v1/alert-rules/' + encodeURIComponent(id), data);
}

// deleteAlertRule 删除告警规则。DELETE /api/v1/alert-rules/{id}
export function deleteAlertRule(id) {
  return requestJSON('DELETE', '/api/v1/alert-rules/' + encodeURIComponent(id));
}

// --- 多条件引擎 ---

// getAlertRulesEngine 列出多条件引擎规则。GET /api/v1/alert-rules-engine
export function getAlertRulesEngine(filter = {}) {
  return requestJSON('GET', '/api/v1/alert-rules-engine' + buildQuery(filter)).then((d) => {
    return Array.isArray(d) ? d : (d && d.rules ? d.rules : (d && d.engine ? d.engine : []));
  });
}

// createAlertRuleEngine 创建多条件引擎规则。POST /api/v1/alert-rules-engine
export function createAlertRuleEngine(data) {
  return requestJSON('POST', '/api/v1/alert-rules-engine', data);
}

// getAlertRuleEngine 获取多条件引擎规则详情。GET /api/v1/alert-rules-engine/{id}
export function getAlertRuleEngine(id) {
  return requestJSON('GET', '/api/v1/alert-rules-engine/' + encodeURIComponent(id));
}

// updateAlertRuleEngine 更新多条件引擎规则。PUT /api/v1/alert-rules-engine/{id}
export function updateAlertRuleEngine(id, data) {
  return requestJSON('PUT', '/api/v1/alert-rules-engine/' + encodeURIComponent(id), data);
}

// deleteAlertRuleEngine 删除多条件引擎规则。DELETE /api/v1/alert-rules-engine/{id}
export function deleteAlertRuleEngine(id) {
  return requestJSON('DELETE', '/api/v1/alert-rules-engine/' + encodeURIComponent(id));
}

// --- 静默规则 ---

// getAlertSilences 列出静默规则。GET /api/v1/alert-silences
export function getAlertSilences(filter = {}) {
  return requestJSON('GET', '/api/v1/alert-silences' + buildQuery(filter)).then((d) => {
    return Array.isArray(d) ? d : (d && d.silences ? d.silences : []);
  });
}

// createAlertSilence 创建静默规则。POST /api/v1/alert-silences
export function createAlertSilence(data) {
  return requestJSON('POST', '/api/v1/alert-silences', data);
}

// deleteAlertSilence 删除静默规则。DELETE /api/v1/alert-silences/{id}
export function deleteAlertSilence(id) {
  return requestJSON('DELETE', '/api/v1/alert-silences/' + encodeURIComponent(id));
}

// ============================================================================
// P0 补齐功能域：批量执行 API
// ============================================================================

// batchExec 批量执行任务（立即下发）。POST /api/v1/tasks/batch-exec
// body: {type, devices[], payload, concurrency, failThreshold, ...}
export function batchExec(data) {
  return requestJSON('POST', '/api/v1/tasks/batch-exec', data);
}

// getBatchStatus 查询批量任务状态。GET /api/v1/tasks/batch/{id}
export function getBatchStatus(id) {
  return requestJSON('GET', '/api/v1/tasks/batch/' + encodeURIComponent(id));
}

// batchCreate 创建批量任务（不立即执行）。POST /api/v1/tasks/batch
// body: {name, type, devices[], payload, concurrency, ...}
export function batchCreate(data) {
  return requestJSON('POST', '/api/v1/tasks/batch', data);
}

// getBatchList 列出最近批量任务。GET /api/v1/tasks/batch
// 注：用于批量列表 tab 展示最近批次；后端无明确文档时按 RESTful 约定推断。
export function getBatchList(filter = {}) {
  return requestJSON('GET', '/api/v1/tasks/batch' + buildQuery(filter)).then((d) => {
    return Array.isArray(d) ? d : (d && d.batches ? d.batches : (d && d.tasks ? d.tasks : []));
  });
}

// ============================================================================
// P1 补齐功能域：通知管理 API（通知渠道 / 通知模板）
// ============================================================================

// getNotifyChannels 列出通知渠道。GET /api/v1/notify-channels
export function getNotifyChannels(filter = {}) {
  return requestJSON('GET', '/api/v1/notify-channels' + buildQuery(filter)).then((d) => {
    return Array.isArray(d) ? d : (d && d.channels ? d.channels : []);
  });
}

// createNotifyChannel 创建通知渠道。POST /api/v1/notify-channels
// body: {name, type, config}
export function createNotifyChannel(data) {
  return requestJSON('POST', '/api/v1/notify-channels', data);
}

// updateNotifyChannel 更新通知渠道。PUT /api/v1/notify-channels/{id}
export function updateNotifyChannel(id, data) {
  return requestJSON('PUT', '/api/v1/notify-channels/' + encodeURIComponent(id), data);
}

// deleteNotifyChannel 删除通知渠道。DELETE /api/v1/notify-channels/{id}
export function deleteNotifyChannel(id) {
  return requestJSON('DELETE', '/api/v1/notify-channels/' + encodeURIComponent(id));
}

// getNotifyTemplates 列出通知模板。GET /api/v1/notify-templates
export function getNotifyTemplates(filter = {}) {
  return requestJSON('GET', '/api/v1/notify-templates' + buildQuery(filter)).then((d) => {
    return Array.isArray(d) ? d : (d && d.templates ? d.templates : []);
  });
}

// createNotifyTemplate 创建通知模板。POST /api/v1/notify-templates
// body: {name, type, subject, body}
export function createNotifyTemplate(data) {
  return requestJSON('POST', '/api/v1/notify-templates', data);
}

// deleteNotifyTemplate 删除通知模板。DELETE /api/v1/notify-templates/{id}
export function deleteNotifyTemplate(id) {
  return requestJSON('DELETE', '/api/v1/notify-templates/' + encodeURIComponent(id));
}

// ============================================================================
// P1 补齐功能域：日志检索 API
// ============================================================================

// searchLogs 检索日志。GET /api/v1/logs?query=&level=&from=&to=&limit=&offset=
// 返回 {logs: [LogEntry], total}
export function searchLogs(filter = {}) {
  return requestJSON('GET', '/api/v1/logs' + buildQuery(filter)).then((d) => {
    if (d && d.logs) return d;
    return { logs: Array.isArray(d) ? d : [], total: Array.isArray(d) ? d.length : 0 };
  });
}

// ============================================================================
// P1 补齐功能域：部署中心 API（部署 / 回滚 / 联邦部署）
// ============================================================================

// getDeploys 列出部署。GET /api/v1/deploys
export function getDeploys(filter = {}) {
  return requestJSON('GET', '/api/v1/deploys' + buildQuery(filter)).then((d) => {
    return Array.isArray(d) ? d : (d && d.deploys ? d.deploys : []);
  });
}

// createDeploy 创建部署。POST /api/v1/deploys
// body: {name, template, params, target}
export function createDeploy(data) {
  return requestJSON('POST', '/api/v1/deploys', data);
}

// rollbackDeploy 回滚部署。POST /api/v1/deploys/{id}/rollback
export function rollbackDeploy(id) {
  return requestJSON('POST', '/api/v1/deploys/' + encodeURIComponent(id) + '/rollback');
}

// getFederationDeploys 列出联邦部署。GET /api/v1/deploys/federation
export function getFederationDeploys(filter = {}) {
  return requestJSON('GET', '/api/v1/deploys/federation' + buildQuery(filter)).then((d) => {
    return Array.isArray(d) ? d : (d && d.deploys ? d.deploys : []);
  });
}

// ============================================================================
// P1 补齐功能域：作业编排 API（工作流 / 运行 / 状态）
// ============================================================================

// getWorkflows 列出工作流。GET /api/v1/workflows
export function getWorkflows(filter = {}) {
  return requestJSON('GET', '/api/v1/workflows' + buildQuery(filter)).then((d) => {
    return Array.isArray(d) ? d : (d && d.workflows ? d.workflows : []);
  });
}

// createWorkflow 创建工作流。POST /api/v1/workflows
// body: {name, steps}
export function createWorkflow(data) {
  return requestJSON('POST', '/api/v1/workflows', data);
}

// runWorkflow 运行工作流。POST /api/v1/workflows/{id}/run
// 返回 {taskID, status}
export function runWorkflow(id) {
  return requestJSON('POST', '/api/v1/workflows/' + encodeURIComponent(id) + '/run');
}

// getWorkflowStatus 查询工作流执行状态。GET /api/v1/workflows/{id}/status
// 返回 {status, steps: [{name, status, startedAt, finishedAt}]}
export function getWorkflowStatus(id) {
  return requestJSON('GET', '/api/v1/workflows/' + encodeURIComponent(id) + '/status');
}

// ============================================================================
// P1 补齐功能域：CMDB API（CI 类型 / CI 项 / 采集 / 变更）
// ============================================================================

// getCMDBCIs 列出 CI 项。GET /api/v1/cmdb/ci?type=
export function getCMDBCIs(filter = {}) {
  return requestJSON('GET', '/api/v1/cmdb/ci' + buildQuery(filter)).then((d) => {
    return Array.isArray(d) ? d : (d && d.items ? d.items : []);
  });
}

// getCMDBTypes 列出 CI 类型。GET /api/v1/cmdb/types
export function getCMDBTypes() {
  return requestJSON('GET', '/api/v1/cmdb/types').then((d) => {
    return Array.isArray(d) ? d : (d && d.types ? d.types : []);
  });
}

// collectCMDB 触发 CMDB 采集。POST /api/v1/cmdb/collect
// 返回 {collected, failed}
export function collectCMDB() {
  return requestJSON('POST', '/api/v1/cmdb/collect');
}

// getCMDBChanges 列出变更申请。GET /api/v1/cmdb/changes
export function getCMDBChanges(filter = {}) {
  return requestJSON('GET', '/api/v1/cmdb/changes' + buildQuery(filter)).then((d) => {
    return Array.isArray(d) ? d : (d && d.changes ? d.changes : []);
  });
}

// ============================================================================
// P1 补齐功能域：OS 优化 API（模板 / 执行）
// ============================================================================

// getOSTemplates 列出 OS 优化模板。GET /api/v1/os-templates
export function getOSTemplates(filter = {}) {
  return requestJSON('GET', '/api/v1/os-templates' + buildQuery(filter)).then((d) => {
    return Array.isArray(d) ? d : (d && d.templates ? d.templates : []);
  });
}

// executeOSTemplate 执行 OS 优化模板。POST /api/v1/os-templates/{id}/execute
// body: {agentID, params}
// 返回 {taskID, status}
export function executeOSTemplate(id, body) {
  return requestJSON('POST', '/api/v1/os-templates/' + encodeURIComponent(id) + '/execute', body || {});
}

// ============================================================================
// P1 补齐功能域：中间件部署 API（模板 / 部署 / 实例）
// ============================================================================

// getMiddlewareTemplates 列出中间件模板。GET /api/v1/middleware-templates
export function getMiddlewareTemplates(filter = {}) {
  return requestJSON('GET', '/api/v1/middleware-templates' + buildQuery(filter)).then((d) => {
    return Array.isArray(d) ? d : (d && d.templates ? d.templates : []);
  });
}

// deployMiddleware 部署中间件。POST /api/v1/middleware-templates/{id}/deploy
// body: {agentID, params, mode}
// 返回 {taskID, status}
export function deployMiddleware(id, body) {
  return requestJSON('POST', '/api/v1/middleware-templates/' + encodeURIComponent(id) + '/deploy', body || {});
}

// getMiddlewareInstances 列出中间件实例。GET /api/v1/middleware-instances
export function getMiddlewareInstances(filter = {}) {
  return requestJSON('GET', '/api/v1/middleware-instances' + buildQuery(filter)).then((d) => {
    return Array.isArray(d) ? d : (d && d.instances ? d.instances : []);
  });
}

// ============================================================================
// P1 补齐功能域：K8s 集群管理 API（集群 / 添加 / 测试连接）
// ============================================================================

// getK8sClusters 列出 K8s 集群。GET /api/v1/k8s/clusters
export function getK8sClusters(filter = {}) {
  return requestJSON('GET', '/api/v1/k8s/clusters' + buildQuery(filter)).then((d) => {
    return Array.isArray(d) ? d : (d && d.clusters ? d.clusters : []);
  });
}

// createK8sCluster 添加 K8s 集群。POST /api/v1/k8s/clusters
// body: {name, kubeconfig, server}
export function createK8sCluster(data) {
  return requestJSON('POST', '/api/v1/k8s/clusters', data);
}

// testK8sCluster 测试 K8s 集群连接。POST /api/v1/k8s/clusters/{id}/test
// 返回 {status: "ok|failed", error}
export function testK8sCluster(id) {
  return requestJSON('POST', '/api/v1/k8s/clusters/' + encodeURIComponent(id) + '/test');
}

// ============================================================================
// P2 补齐功能域：SSE 实时推送 API
// ============================================================================

// connectSSE 连接 SSE 事件流。GET /api/v1/events/stream
// onEvent: (event) => void，事件对象 {type, data}
// 连接失败时静默降级（不抛错），返回 EventSource 实例或 null。
export function connectSSE(onEvent) {
  try {
    if (typeof EventSource === 'undefined') return null;
    const url = '/api/v1/events/stream';
    const es = new EventSource(url);
    // 通用 message 事件
    es.onmessage = (ev) => {
      try {
        const data = JSON.parse(ev.data);
        if (onEvent) onEvent({ type: 'message', data });
      } catch (_) { /* 静默忽略解析失败 */ }
    };
    // 已知事件类型：task_status / alert_new / device_status
    ['task_status', 'alert_new', 'device_status'].forEach((type) => {
      es.addEventListener(type, (ev) => {
        try {
          const data = JSON.parse(ev.data);
          if (onEvent) onEvent({ type, data });
        } catch (_) { /* 静默 */ }
      });
    });
    // 连接失败时静默降级（不报错）
    es.onerror = () => { /* 静默降级到轮询 */ };
    return es;
  } catch (_) {
    return null;
  }
}

// ============================================================================
// P2 补齐功能域：自动纳管 API
// ============================================================================

// autoProvision 自动纳管。POST /api/v1/provision/auto
// body: {segment, agentVersion}
// 返回 {discovered, provisioned, failed, devices: [{ip, hostname, status}]}
export function autoProvision(data) {
  return requestJSON('POST', '/api/v1/provision/auto', data);
}

// ============================================================================
// P2 补齐功能域：ChatOps API（命令 / 历史 / 平台）
// ============================================================================

// sendBotCommand 发送 ChatOps 命令。POST /api/v1/bot/command
// body: {command, platform}
// 返回 {response, taskID}
export function sendBotCommand(data) {
  return requestJSON('POST', '/api/v1/bot/command', data);
}

// getBotHistory 获取 ChatOps 历史记录。GET /api/v1/bot/history
// 返回 {history: [{id, command, response, userID, createdAt}]}
export function getBotHistory() {
  return requestJSON('GET', '/api/v1/bot/history');
}

// getBotPlatforms 获取 ChatOps 平台列表。GET /api/v1/bot/platforms
// 返回 {platforms: [{name, enabled, webhookURL}]}
export function getBotPlatforms() {
  return requestJSON('GET', '/api/v1/bot/platforms').then((d) => {
    return Array.isArray(d) ? d : (d && d.platforms ? d.platforms : []);
  });
}

// ============================================================================
// P2 补齐功能域：控制面联邦 API（Peer / 设备聚合 / 任务转发）
// ============================================================================

// getFederationPeers 列出联邦 Peer。GET /api/v1/federation/peers
// 返回 {peers: [PeerStatus]}，PeerStatus {url, online, lastCheckAt, latencyMs}
export function getFederationPeers() {
  return requestJSON('GET', '/api/v1/federation/peers');
}

// forwardTask 转发任务到 peer。POST /api/v1/federation/forward/task
// body: {peerURL, taskType, command, deviceID, timeoutSec}
// 返回 {taskID, peerURL, status}
export function forwardTask(data) {
  return requestJSON('POST', '/api/v1/federation/forward/task', data);
}

// getFederationDevices 获取跨 peer 设备聚合视图。GET /api/v1/federation/devices
// 返回 {devices: [Device], peers: [{url, online, deviceCount}]}
export function getFederationDevices() {
  return requestJSON('GET', '/api/v1/federation/devices');
}

// ============================================================================
// P2 补齐功能域：定时任务 API（CRUD）
// ============================================================================

// getSchedules 列出定时任务。GET /api/v1/schedules
// 返回 {schedules: [Schedule]}，Schedule {id, name, cron, taskType, params, enabled, lastRunAt, nextRunAt, createdAt}
export function getSchedules() {
  return requestJSON('GET', '/api/v1/schedules');
}

// createSchedule 创建定时任务。POST /api/v1/schedules（201）
// body: {name, cron, taskType, params, enabled}
export function createSchedule(data) {
  return requestJSON('POST', '/api/v1/schedules', data);
}

// updateSchedule 更新定时任务。PUT /api/v1/schedules/{id}
export function updateSchedule(id, data) {
  return requestJSON('PUT', '/api/v1/schedules/' + encodeURIComponent(id), data);
}

// deleteSchedule 删除定时任务。DELETE /api/v1/schedules/{id}
// 返回 {status: "deleted"}
export function deleteSchedule(id) {
  return requestJSON('DELETE', '/api/v1/schedules/' + encodeURIComponent(id));
}

// ============================================================================
// P3 补齐功能域：审批流 API（流定义 / 审批请求 / approve / reject）
// ============================================================================

// getApprovalFlows 列出审批流定义。GET /api/v1/approval/flows
// 返回 {flows: [ApprovalFlow]}，ApprovalFlow {id, name, description, steps, createdAt}
export function getApprovalFlows() {
  return requestJSON('GET', '/api/v1/approval/flows').then((d) => {
    return Array.isArray(d) ? d : (d && d.flows ? d.flows : []);
  });
}

// createApprovalFlow 创建审批流。POST /api/v1/approval/flows（201）
// body: {name, description, steps}
export function createApprovalFlow(data) {
  return requestJSON('POST', '/api/v1/approval/flows', data);
}

// deleteApprovalFlow 删除审批流。DELETE /api/v1/approval/flows/{id}
// 返回 {status: "deleted"}
export function deleteApprovalFlow(id) {
  return requestJSON('DELETE', '/api/v1/approval/flows/' + encodeURIComponent(id));
}

// getApprovalRequests 列出审批请求。GET /api/v1/approval/requests?status=pending
// 返回 {requests: [ApprovalRequest]}
export function getApprovalRequests(filter = {}) {
  return requestJSON('GET', '/api/v1/approval/requests' + buildQuery(filter)).then((d) => {
    return Array.isArray(d) ? d : (d && d.requests ? d.requests : []);
  });
}

// approveRequest 批准审批请求。POST /api/v1/approval/requests/{id}/approve
export function approveRequest(id) {
  return requestJSON('POST', '/api/v1/approval/requests/' + encodeURIComponent(id) + '/approve');
}

// rejectRequest 驳回审批请求。POST /api/v1/approval/requests/{id}/reject
export function rejectRequest(id) {
  return requestJSON('POST', '/api/v1/approval/requests/' + encodeURIComponent(id) + '/reject');
}

// getPendingApprovals 获取待审批列表。GET /api/v1/approval/pending
// 返回 {requests: [ApprovalRequest]}
export function getPendingApprovals() {
  return requestJSON('GET', '/api/v1/approval/pending').then((d) => {
    return Array.isArray(d) ? d : (d && d.requests ? d.requests : []);
  });
}

// ============================================================================
// P3 补齐功能域：密钥管理 API（状态 / 测试 / 密钥列表）
// ============================================================================

// getSecretsStatus 获取密钥后端状态。GET /api/v1/secrets/status
// 返回 {backend, sealed, keys}
export function getSecretsStatus() {
  return requestJSON('GET', '/api/v1/secrets/status');
}

// testSecrets 测试密钥后端连通性。POST /api/v1/secrets/test
// 返回 {status: "ok|failed", error}
export function testSecrets() {
  return requestJSON('POST', '/api/v1/secrets/test');
}

// getSecretKeys 列出密钥。GET /api/v1/secrets/keys
// 返回 {keys: [{name, createdAt, rotatedAt}]}
export function getSecretKeys() {
  return requestJSON('GET', '/api/v1/secrets/keys').then((d) => {
    return Array.isArray(d) ? d : (d && d.keys ? d.keys : []);
  });
}

// ============================================================================
// P3 补齐功能域：Helm 应用商店 API（仓库 / Chart 搜索 / Release / 目录）
// ============================================================================

// getHelmRepos 列出 Helm 仓库。GET /api/v1/helm/repos
// 返回 {repos: [HelmRepo]}，HelmRepo {name, url, username, createdAt}
export function getHelmRepos() {
  return requestJSON('GET', '/api/v1/helm/repos').then((d) => {
    return Array.isArray(d) ? d : (d && d.repos ? d.repos : []);
  });
}

// createHelmRepo 添加 Helm 仓库。POST /api/v1/helm/repos（201）
// body: {name, url, username, password}
export function createHelmRepo(data) {
  return requestJSON('POST', '/api/v1/helm/repos', data);
}

// deleteHelmRepo 删除 Helm 仓库。DELETE /api/v1/helm/repos/{name}
// 返回 {status: "deleted"}
export function deleteHelmRepo(name) {
  return requestJSON('DELETE', '/api/v1/helm/repos/' + encodeURIComponent(name));
}

// searchHelmCharts 搜索 Helm Chart。GET /api/v1/helm/charts/search?q=xxx
// 返回 {charts: [Chart]}，Chart {name, repo, version, description, home, sources}
export function searchHelmCharts(q) {
  const query = q ? '?q=' + encodeURIComponent(q) : '';
  return requestJSON('GET', '/api/v1/helm/charts/search' + query).then((d) => {
    return Array.isArray(d) ? d : (d && d.charts ? d.charts : []);
  });
}

// getHelmReleases 列出 Helm Release。GET /api/v1/helm/releases
// 返回 {releases: [HelmRelease]}，HelmRelease {name, namespace, chart, version, status, updated}
export function getHelmReleases() {
  return requestJSON('GET', '/api/v1/helm/releases').then((d) => {
    return Array.isArray(d) ? d : (d && d.releases ? d.releases : []);
  });
}

// installHelmRelease 安装 Helm Release。POST /api/v1/helm/releases（201）
// body: {name, chart, namespace, values, repo}
export function installHelmRelease(data) {
  return requestJSON('POST', '/api/v1/helm/releases', data);
}

// uninstallHelmRelease 卸载 Helm Release。DELETE /api/v1/helm/releases/{name}
// 返回 {status: "deleted"}
export function uninstallHelmRelease(name) {
  return requestJSON('DELETE', '/api/v1/helm/releases/' + encodeURIComponent(name));
}

// getHelmCatalog 获取 Helm 应用目录。GET /api/v1/helm/catalog
// 返回 {categories: [CatalogCategory]}，CatalogCategory {name, description, count}
export function getHelmCatalog() {
  return requestJSON('GET', '/api/v1/helm/catalog').then((d) => {
    return Array.isArray(d) ? d : (d && d.categories ? d.categories : []);
  });
}