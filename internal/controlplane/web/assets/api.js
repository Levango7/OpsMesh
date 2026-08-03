// api.js — REST 调用封装
// 职责：fetch 包装、租户头注入（X-Tenant-ID 由前置网关注入，前端只做兜底）、
//       统一错误处理、所有 API 端点封装函数。
// 不持有业务状态，仅 pollFailCount 用于退避计数（由 render.js 的 apiFail/apiOk 读写）。
// 新增：JWT token 管理（localStorage）、auth/users/roles/permissions API。

// ---------- JWT token 管理 ----------
const TOKEN_KEY = 'opsmesh-token';
export function getToken() { return localStorage.getItem(TOKEN_KEY) || ''; }
export function setToken(t) { if (t) localStorage.setItem(TOKEN_KEY, t); else localStorage.removeItem(TOKEN_KEY); }
export function isLoggedIn() { return !!getToken(); }

// ---------- 401 处理 ----------
// 当 API 返回 401 时，不自动跳转登录页，而是显示提示让用户手动重新登录。
// 通过自定义事件通知 UI 层，避免 api.js 反向依赖 main.js。
let authErrorShown = false;
export function handleAuthError() {
  // 避免短时间内重复弹窗
  if (authErrorShown) return;
  authErrorShown = true;
  // 5 秒后重置，允许再次提示
  setTimeout(function () { authErrorShown = false; }, 5000);
  // 派发事件，由 main.js 监听并显示提示
  try { document.dispatchEvent(new CustomEvent('opsmesh:auth-error')); } catch (_) {}
  // 同时 alert 兜底（确保用户一定能看到）
  try { alert('登录已失效，请重新登录'); } catch (_) {}
}

// ---------- 统一 fetch 包装 ----------
// 注：X-Tenant-ID / X-User / X-User-Roles 由前置网关注入，前端不主动设置；
// 此处仅做 JSON 解析与 {status, json} 形态归一，便于上层判断。
// 若已登录，自动附加 Authorization: Bearer <token> 头。
export async function request(url, opts) {
  const o = opts || {};
  // 注入 Authorization 头
  const token = getToken();
  if (token) {
    o.headers = Object.assign({}, o.headers || {}, { 'Authorization': 'Bearer ' + token });
  }
  const r = await fetch(url, o);
  let j = null;
  try { j = await r.json(); } catch (_) { j = null; }
  return { s: r.status, j: j };
}

export function jsonBody(body) {
  const h = { 'Content-Type': 'application/json' };
  const token = getToken();
  if (token) h['Authorization'] = 'Bearer ' + token;
  return {
    method: 'POST',
    headers: h,
    body: JSON.stringify(body),
  };
}

export function jsonMethod(method, body) {
  const h = { 'Content-Type': 'application/json' };
  const token = getToken();
  if (token) h['Authorization'] = 'Bearer ' + token;
  return {
    method: method,
    headers: h,
    body: JSON.stringify(body),
  };
}

// 带认证的 GET / DELETE 包装（无 body），返回 {s, j}
export function authFetch(url, method) {
  const h = {};
  const token = getToken();
  if (token) h['Authorization'] = 'Bearer ' + token;
  return fetch(url, { method: method || 'GET', headers: h })
    .then(function (r) { return r.json().then(function (j) { return { s: r.status, j: j }; }).catch(function () { return { s: r.status, j: null }; }); });
}

// 带认证的 GET，直接返回解析后的 json；非 2xx 时 throw Error（便于上层 catch 统一处理）
// 用于 getDevices / getTasks / getAlerts 等返回纯 json 的函数，确保携带 Authorization token。
// 401 时不自动跳转登录页，而是显示提示让用户手动重新登录。
function authGet(url) {
  const h = {};
  const token = getToken();
  if (token) h['Authorization'] = 'Bearer ' + token;
  return fetch(url, { method: 'GET', headers: h })
    .then(function (r) {
      if (!r.ok) {
        if (r.status === 401) { handleAuthError(); }
        const err = new Error('HTTP ' + r.status);
        err.status = r.status;
        throw err;
      }
      return r.json();
    });
}

// 带认证的 POST（无 body），返回 {s, j}；确保携带 Authorization token
function authPost(url) {
  const h = {};
  const token = getToken();
  if (token) h['Authorization'] = 'Bearer ' + token;
  return fetch(url, { method: 'POST', headers: h })
    .then(function (r) { return r.json().then(function (j) { return { s: r.status, j: j }; }).catch(function () { return { s: r.status, j: null }; }); });
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
    const badge = '<div class="poll-err">⚠ 连接异常（已重试 ' + pollFailCount[tag] + ' 次）</div>';
    // 仅在面板内容为空或已是错误提示时替换，避免覆盖已加载的有效数据
    if (!el.innerHTML || el.innerHTML.indexOf('poll-err') >= 0 || el.innerHTML.indexOf('加载中') >= 0) {
      el.innerHTML = badge;
    }
  }
}

// ---------- Agents ----------
export function getAgents() {
  return authGet('/api/v1/agents');
}

// ---------- Devices ----------
export function getDevices() {
  return authGet('/api/v1/devices');
}
export function getDevice(id) {
  return authGet('/api/v1/devices/' + encodeURIComponent(id));
}
export function provisionDevice(id) {
  return authPost('/api/v1/devices/' + encodeURIComponent(id) + '/provision');
}
// 设备监控指标：GET /api/v1/devices/{id}/metrics → 200 {deviceID, hostname, os, ..., cpu, memory, disks, network, services, ...}
export function apiDeviceMetrics(deviceID) {
  return authFetch('/api/v1/devices/' + encodeURIComponent(deviceID) + '/metrics', 'GET');
}

// ---------- Tasks ----------
export function getTasks(status) {
  const q = status ? '?status=' + encodeURIComponent(status) : '';
  return authGet('/api/v1/tasks' + q);
}
export function createTask(body) {
  return fetch('/api/v1/tasks', jsonBody(body))
    .then(function (r) { return r.json().then(function (j) { return { s: r.status, j: j }; }); });
}

// ---------- Alerts ----------
export function getAlerts() {
  return authGet('/api/v1/alerts');
}
export function ackAlert(id) {
  return authPost('/api/v1/alerts/' + encodeURIComponent(id) + '/ack');
}
export function silenceAlert(id, body) {
  return fetch('/api/v1/alerts/' + encodeURIComponent(id) + '/silence', jsonBody(body))
    .then(function (r) { return r.json().then(function (j) { return { s: r.status, j: j }; }); });
}

// ---------- CMDB ----------
export function getCMDBTypes() {
  return authGet('/api/v1/cmdb/types');
}
export function getCIs(type) {
  return authGet('/api/v1/cmdb/ci?type=' + encodeURIComponent(type));
}
export function createCI(body) {
  return fetch('/api/v1/cmdb/ci', jsonBody(body))
    .then(function (r) { return r.json().then(function (j) { return { s: r.status, j: j }; }); });
}
export function getCIGraph(id) {
  return authGet('/api/v1/cmdb/ci/' + encodeURIComponent(id) + '/graph');
}
export function getAttrTemplates(type) {
  return authGet('/api/v1/cmdb/attr-templates?type=' + encodeURIComponent(type));
}

// ---------- Workflows ----------
export function getWorkflows() {
  return authGet('/api/v1/workflows');
}
export function getWorkflow(id) {
  return authGet('/api/v1/workflows/' + encodeURIComponent(id));
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
  return authPost('/api/v1/workflows/' + id + '/run');
}
export function getWorkflowStatus(id) {
  return authGet('/api/v1/workflows/' + id + '/status');
}
export function scheduleWorkflow(id, cron) {
  return fetch('/api/v1/workflows/' + id + '/schedule', jsonBody({ cron: cron }))
    .then(function (r) { return r.json().then(function (j) { return { s: r.status, j: j }; }); });
}

// ---------- Deploys ----------
export function getDeploys(status) {
  const q = status ? '?status=' + encodeURIComponent(status) : '';
  return authGet('/api/v1/deploys' + q);
}
export function createDeploy(body) {
  return fetch('/api/v1/deploys', jsonBody(body))
    .then(function (r) { return r.json().then(function (j) { return { s: r.status, j: j }; }); });
}
export function executeDeploy(id) {
  return authPost('/api/v1/deploys/' + id + '/execute');
}
export function rollbackDeploy(id) {
  return authPost('/api/v1/deploys/' + id + '/rollback');
}
export function getDeploy(id) {
  return authGet('/api/v1/deploys/' + id);
}

// ---------- Logs ----------
export function getLogs(query) {
  return authGet('/api/v1/logs?' + query);
}

// ---------- Me（动态身份注入） ----------
export function getMe() {
  return authFetch('/api/v1/me', 'GET');
}

// ============================================================
// 认证 / 用户 / 角色 / 权限 API（新增）
// 契约：
//   POST /api/v1/auth/register  {username, password, email?} → 201 {token, user}
//   POST /api/v1/auth/login     {username, password} → 200 {token, user}
//   GET  /api/v1/auth/me        → 200 {user}
//   GET  /api/v1/users          → 200 {users: []}
//   POST /api/v1/users          {username, password, email?, role_ids[]} → 201 {user}
//   PUT  /api/v1/users/{id}     {email?, role_ids?, status?} → 200 {user}
//   DELETE /api/v1/users/{id}   → 204
//   GET  /api/v1/roles          → 200 {roles: []}
//   POST /api/v1/roles          {name, description, permissions[]} → 201 {role}
//   PUT  /api/v1/roles/{id}     {description?, permissions?} → 200 {role}
//   DELETE /api/v1/roles/{id}   → 204
//   GET  /api/v1/permissions    → 200 {permissions: []}
// ============================================================

// ---------- Auth ----------
// 登录：成功后存 token
export async function apiAuthLogin(username, password) {
  const r = await request('/api/v1/auth/login', jsonBody({ username: username, password: password }));
  if (r.s === 200 && r.j && r.j.token) {
    setToken(r.j.token);
  }
  return r;
}

// 注册：成功后存 token
export async function apiAuthRegister(username, password, email) {
  const body = { username: username, password: password };
  if (email) body.email = email;
  const r = await request('/api/v1/auth/register', jsonBody(body));
  if (r.s === 201 && r.j && r.j.token) {
    setToken(r.j.token);
  }
  return r;
}

// 获取当前登录用户信息
export function apiAuthMe() {
  return authFetch('/api/v1/auth/me', 'GET');
}

// 退出登录：清除本地 token
export function apiLogout() {
  setToken('');
}

// ---------- Users ----------
export function apiListUsers() {
  return authFetch('/api/v1/users', 'GET');
}

export async function apiCreateUser(username, password, email, roleIds) {
  const body = { username: username, password: password };
  if (email) body.email = email;
  if (roleIds) body.role_ids = roleIds;
  return await request('/api/v1/users', jsonBody(body));
}

export async function apiUpdateUser(id, patch) {
  // patch: {email?, role_ids?, status?}
  return await request('/api/v1/users/' + encodeURIComponent(id), jsonMethod('PUT', patch));
}

export function apiDeleteUser(id) {
  const h = {};
  const token = getToken();
  if (token) h['Authorization'] = 'Bearer ' + token;
  return fetch('/api/v1/users/' + encodeURIComponent(id), { method: 'DELETE', headers: h })
    .then(function (r) { return { s: r.status, j: null }; });
}

// ---------- Roles ----------
export function apiListRoles() {
  return authFetch('/api/v1/roles', 'GET');
}

export async function apiCreateRole(name, description, permissions) {
  const body = { name: name, description: description || '', permissions: permissions || [] };
  return await request('/api/v1/roles', jsonBody(body));
}

export async function apiUpdateRole(id, patch) {
  // patch: {description?, permissions?}
  return await request('/api/v1/roles/' + encodeURIComponent(id), jsonMethod('PUT', patch));
}

export function apiDeleteRole(id) {
  const h = {};
  const token = getToken();
  if (token) h['Authorization'] = 'Bearer ' + token;
  return fetch('/api/v1/roles/' + encodeURIComponent(id), { method: 'DELETE', headers: h })
    .then(function (r) { return { s: r.status, j: null }; });
}

// ---------- Permissions ----------
export function apiListPermissions() {
  return authFetch('/api/v1/permissions', 'GET');
}