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