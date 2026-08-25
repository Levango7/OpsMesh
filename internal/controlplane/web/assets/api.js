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