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

// getCanaryReleases 列出灰度发布。GET /api/v1/canary/releases
export function getCanaryReleases() {
  return requestJSON('GET', '/api/v1/canary/releases').then((d) => {
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