// api.js — REST 调用封装
// 职责：fetch 包装、租户头注入（X-Tenant-ID 由前置网关注入，前端只做兜底）、
//       统一错误处理、所有 API 端点封装函数。
// 不持有业务状态，仅 pollFailCount 用于退避计数（由 render.js 的 apiFail/apiOk 读写）。

// ---------- 统一 fetch 包装 ----------
// 注：X-Tenant-ID / X-User / X-User-Roles 由前置网关注入，前端不主动设置；
// 此处仅做 JSON 解析与 {status, json} 形态归一，便于上层判断。
export async function request(url, opts) {
  const r = await fetch(url, opts || {});
  let j = null;
  try { j = await r.json(); } catch (_) { j = null; }
  return { s: r.status, j: j };
}

export function jsonBody(body) {
  return {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  };
}

export function jsonMethod(method, body) {
  return {
    method: method,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  };
}

// ---------- 轮询失败退避计数（共享可变状态） ----------
export const pollFailCount = { devices: 0, tasks: 0, alerts: 0, deploys: 0, alertsFull: 0 };

export function apiOk(tag) {
  if (pollFailCount[tag]) { pollFailCount[tag] = 0; }
}

export function apiFail(tag, e) {
  console.error('[' + tag + ']', e);
  pollFailCount[tag] = (pollFailCount[tag] || 0) + 1;
  let el = null;
  if (tag === 'devices') el = document.getElementById('devices');
  else if (tag === 'tasks') el = document.getElementById('tasks');
  else if (tag === 'alerts') el = document.getElementById('alerts');
  else if (tag === 'deploys') el = document.getElementById('deployList');
  else if (tag === 'alertsFull') el = document.getElementById('alertsFull');
  if (el) {
    const badge = '<div class="poll-err">⚠️ 连接异常（已重试 ' + pollFailCount[tag] + ' 次）</div>';
    // 仅在面板内容为空或已是错误提示时替换，避免覆盖已加载的有效数据
    if (!el.innerHTML || el.innerHTML.indexOf('poll-err') >= 0 || el.innerHTML.indexOf('加载中') >= 0) {
      el.innerHTML = badge;
    }
  }
}

// ---------- Agents ----------
export function getAgents() {
  return fetch('/api/v1/agents').then(function (r) { return r.json(); });
}

// ---------- Devices ----------
export function getDevices() {
  return fetch('/api/v1/devices').then(function (r) { return r.json(); });
}
export function getDevice(id) {
  return fetch('/api/v1/devices/' + encodeURIComponent(id)).then(function (r) { return r.json(); });
}
export function provisionDevice(id) {
  return fetch('/api/v1/devices/' + encodeURIComponent(id) + '/provision', { method: 'POST' })
    .then(function (r) { return r.json().then(function (j) { return { s: r.status, j: j }; }); });
}

// ---------- Tasks ----------
export function getTasks(status) {
  const q = status ? '?status=' + encodeURIComponent(status) : '';
  return fetch('/api/v1/tasks' + q).then(function (r) { return r.json(); });
}
export function createTask(body) {
  return fetch('/api/v1/tasks', jsonBody(body))
    .then(function (r) { return r.json().then(function (j) { return { s: r.status, j: j }; }); });
}

// ---------- Alerts ----------
export function getAlerts() {
  return fetch('/api/v1/alerts').then(function (r) { return r.json(); });
}
export function ackAlert(id) {
  return fetch('/api/v1/alerts/' + encodeURIComponent(id) + '/ack', { method: 'POST' })
    .then(function (r) { return r.json().then(function (j) { return { s: r.status, j: j }; }); });
}
export function silenceAlert(id, body) {
  return fetch('/api/v1/alerts/' + encodeURIComponent(id) + '/silence', jsonBody(body))
    .then(function (r) { return r.json().then(function (j) { return { s: r.status, j: j }; }); });
}

// ---------- CMDB ----------
export function getCMDBTypes() {
  return fetch('/api/v1/cmdb/types').then(function (r) { return r.json(); });
}
export function getCIs(type) {
  return fetch('/api/v1/cmdb/ci?type=' + encodeURIComponent(type)).then(function (r) { return r.json(); });
}
export function createCI(body) {
  return fetch('/api/v1/cmdb/ci', jsonBody(body))
    .then(function (r) { return r.json().then(function (j) { return { s: r.status, j: j }; }); });
}
export function getCIGraph(id) {
  return fetch('/api/v1/cmdb/ci/' + encodeURIComponent(id) + '/graph').then(function (r) { return r.json(); });
}
export function getAttrTemplates(type) {
  return fetch('/api/v1/cmdb/attr-templates?type=' + encodeURIComponent(type)).then(function (r) { return r.json(); });
}

// ---------- Workflows ----------
export function getWorkflows() {
  return fetch('/api/v1/workflows').then(function (r) { return r.json(); });
}
export function getWorkflow(id) {
  return fetch('/api/v1/workflows/' + encodeURIComponent(id)).then(function (r) { return r.json(); });
}
export function createWorkflow(body) {
  return fetch('/api/v1/workflows', jsonBody(body))
    .then(function (r) { return r.json().then(function (j) { return { s: r.status, j: j }; }); });
}
export function updateWorkflow(id, body) {
  return fetch('/api/v1/workflows/' + id, jsonMethod('PUT', body))
    .then(function (r) { return r.json().then(function (j) { return { s: r.status, j: j }; }); });
}
export function runWorkflow(id) {
  return fetch('/api/v1/workflows/' + id + '/run', { method: 'POST' })
    .then(function (r) { return r.json().then(function (j) { return { s: r.status, j: j }; }); });
}
export function getWorkflowStatus(id) {
  return fetch('/api/v1/workflows/' + id + '/status').then(function (r) { return r.json(); });
}
export function scheduleWorkflow(id, cron) {
  return fetch('/api/v1/workflows/' + id + '/schedule', jsonBody({ cron: cron }))
    .then(function (r) { return r.json().then(function (j) { return { s: r.status, j: j }; }); });
}

// ---------- Deploys ----------
export function getDeploys(status) {
  const q = status ? '?status=' + encodeURIComponent(status) : '';
  return fetch('/api/v1/deploys' + q).then(function (r) { return r.json(); });
}
export function createDeploy(body) {
  return fetch('/api/v1/deploys', jsonBody(body))
    .then(function (r) { return r.json().then(function (j) { return { s: r.status, j: j }; }); });
}
export function executeDeploy(id) {
  return fetch('/api/v1/deploys/' + id + '/execute', { method: 'POST' })
    .then(function (r) { return r.json().then(function (j) { return { s: r.status, j: j }; }); });
}
export function rollbackDeploy(id) {
  return fetch('/api/v1/deploys/' + id + '/rollback', { method: 'POST' })
    .then(function (r) { return r.json().then(function (j) { return { s: r.status, j: j }; }); });
}
export function getDeploy(id) {
  return fetch('/api/v1/deploys/' + id).then(function (r) { return r.json(); });
}

// ---------- Logs ----------
export function getLogs(query) {
  return fetch('/api/v1/logs?' + query).then(function (r) { return r.json(); });
}

// ---------- Me（动态身份注入） ----------
export function getMe() {
  return fetch('/api/v1/me').then(function (r) { return r.json().then(function (j) { return { s: r.status, j: j }; }); });
}