// api.js — REST 调用封装
// 职责：fetch 包装、租户头注入（X-Tenant-ID 由前置网关注入，前端只做兜底）、
//       统一错误处理、所有 API 端点封装函数。
// 不持有业务状态，仅 pollFailCount 用于退避计数（由 render.js 的 apiFail/apiOk 读写）。
// 新增：JWT token 管理（localStorage）、auth/users/roles/permissions API。

// ---------- JWT token 管理（task 94：内存持有 + HttpOnly Cookie 会话） ----------
// token 仅存内存，不再写 localStorage——localStorage 可被任意脚本（XSS）读取；
// 服务端登录时同时下发 HttpOnly Cookie（opsmesh_token），刷新后浏览器自动携带维持会话。
let memoryToken = '';
export function getToken() { return memoryToken; }
export function setToken(t) {
  memoryToken = t || '';
  // 清理旧版本遗留的 localStorage token（XSS 风险面，一次性迁移）。
  try { localStorage.removeItem('opsmesh-token'); } catch (_) {}
}
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
  // 防御性检查：type 为 undefined/null/空字符串时直接返回空数组，避免发出
  // /api/v1/cmdb/ci?type=undefined 这类无效请求。
  if (type == null || type === '') return Promise.resolve([]);
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

// 改密（安全债 85）：POST /api/v1/auth/change-password {oldPassword, newPassword} → 200 {message}
// 鉴权：须携带当前 token。新密码强度由后端校验（≥8 字符且含大小写字母与数字）。
export function apiAuthChangePassword(oldPassword, newPassword) {
  return request('/api/v1/auth/change-password', jsonMethod('POST', { oldPassword: oldPassword, newPassword: newPassword }));
}

// 退出登录（task 94）：清内存 token + 调后端清除 HttpOnly Cookie。
// JWT 无状态，服务端不做黑名单；Cookie 清除即终止浏览器会话，token 24h 自然过期。
export function apiLogout() {
  setToken('');
  return fetch('/api/v1/auth/logout', { method: 'POST' }).catch(function () {});
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

// ---------- Audits ----------
// 审计日志查询：GET /api/v1/audits → 200 AuditEvent[]
// 查询参数：action（过滤动作类型）、from/to（RFC3339 时间窗）、limit（默认 100，上限 1000）。
export function getAudits(action, from, to, limit) {
  let qs = [];
  if (action) qs.push('action=' + encodeURIComponent(action));
  if (from) qs.push('from=' + encodeURIComponent(from));
  if (to) qs.push('to=' + encodeURIComponent(to));
  if (limit) qs.push('limit=' + encodeURIComponent(limit));
  return authGet('/api/v1/audits' + (qs.length ? '?' + qs.join('&') : ''));
}

// ---------- OS 优化模板 ----------
// 契约：
//   GET  /api/v1/os-templates            → 200 OSTemplate[]（可选 ?category= 过滤）
//   GET  /api/v1/os-templates/{id}       → 200 OSTemplate
//   POST /api/v1/os-templates/{id}/execute  {agentID, params[]} → 200 task 信息
// OSTemplate 字段：id, name, category, description, commands, risk(low/medium/high), tags[], os
export function getOSTemplates(category) {
  const qs = category ? '?category=' + encodeURIComponent(category) : '';
  return authGet('/api/v1/os-templates' + qs);
}

export function getOSTemplate(id) {
  return authGet('/api/v1/os-templates/' + encodeURIComponent(id));
}

// 执行模板：在指定 agent 上执行 OS 优化模板，返回创建的 task 信息。
// 由于 authPost 不带 body，这里用 request + jsonBody 发送 POST 请求。
export async function executeOSTemplate(id, agentID, params) {
  const body = { agentID: agentID, params: params || [] };
  return await request('/api/v1/os-templates/' + encodeURIComponent(id) + '/execute', jsonBody(body));
}

// ---------- 中间件部署模板 ----------
// 契约：
//   GET  /api/v1/middleware-templates            → 200 MiddlewareTemplate[]（可选 ?category= 过滤）
//   GET  /api/v1/middleware-templates/{id}       → 200 MiddlewareTemplate
//   POST /api/v1/middleware-templates/{id}/deploy  {agentID, deployType, params} → 200 {taskID}
//   GET  /api/v1/middleware-instances            → 200 MiddlewareInstance[]
// MiddlewareTemplate 字段：
//   id, name, category, version, description, deployTypes[], params[], scripts{docker,systemd}, risk, tags[]
// MiddlewareInstance 字段：{id, templateID, agentID, deployType, status, createdAt, ...}
export function getMiddlewareTemplates(category) {
  const qs = category ? '?category=' + encodeURIComponent(category) : '';
  return authGet('/api/v1/middleware-templates' + qs);
}

export function getMiddlewareTemplate(id) {
  return authGet('/api/v1/middleware-templates/' + encodeURIComponent(id));
}

// 部署中间件：在指定 agent 上以指定部署方式（docker/systemd）部署中间件模板。
// params 为对象，例如 {name, port, password, ...}，由模板 params 定义。
// 返回 {s, j}，其中 j 形如 {taskID}。
export async function deployMiddleware(id, agentID, deployType, params) {
  const body = { agentID: agentID, deployType: deployType, params: params || {} };
  return await request('/api/v1/middleware-templates/' + encodeURIComponent(id) + '/deploy', jsonBody(body));
}

// 查询已部署的中间件实例列表。
export function getMiddlewareInstances() {
  return authGet('/api/v1/middleware-instances');
}

// ---------- 任务详情查询（用于执行/部署/卸载日志轮询） ----------
// 契约：GET /api/v1/tasks/{taskID} → 200 {taskID, status, output, ...}
//   status: pending / running / completed / failed
//   output: 任务执行 stdout/stderr 拼接文本
// 用于前端轮询任务状态、展示执行/部署/卸载日志。
export function getTaskDetail(taskID) {
  return authGet('/api/v1/tasks/' + encodeURIComponent(taskID));
}

// ---------- 中间件卸载 ----------
// 契约：POST /api/v1/middleware-instances/{instanceID}/uninstall
//   请求体：{agentID, deployType}
//   返回：{s, j}，j 形如 {taskID} 或 {taskID, status, ...}
// 用于在指定 agent 上卸载已部署的中间件实例，返回卸载任务 ID 供前端轮询。
export async function uninstallMiddleware(instanceID, agentID, deployType) {
  return await request('/api/v1/middleware-instances/' + encodeURIComponent(instanceID) + '/uninstall', jsonBody({ agentID: agentID, deployType: deployType }));
}

// ============================================================
// K8s 集群 / 资源管理 API（Phase 3）
// 契约：
//   GET    /api/v1/k8s/clusters                                  → 200 {clusters: [{id,name,server,status,createdAt,updatedAt}]}
//   POST   /api/v1/k8s/clusters          {name,server,kubeconfig} → 200 Cluster
//   DELETE /api/v1/k8s/clusters/{id}                             → 204
//   POST   /api/v1/k8s/clusters/{id}/test                        → 200 {status,message}
//   GET    /api/v1/k8s/clusters/{id}/namespaces                  → 200 {namespaces: [{name,status,createdAt}]}
//   GET    /api/v1/k8s/clusters/{id}/pods?namespace={ns}         → 200 {pods: [{name,namespace,status,podIP,nodeIP,restarts,age}]}
//   GET    /api/v1/k8s/clusters/{id}/pods/{ns}/{name}/logs       → 200 {logs: "..."}
//   DELETE /api/v1/k8s/clusters/{id}/pods/{ns}/{name}            → 204
//   GET    /api/v1/k8s/clusters/{id}/deployments?namespace={ns}  → 200 {deployments: [{name,namespace,replicas,availableReplicas,image}]}
//   POST   /api/v1/k8s/clusters/{id}/deployments/{ns}/{name}/scale  {replicas} → 200 {name,replicas}
//   POST   /api/v1/k8s/clusters/{id}/deployments/{ns}/{name}/restart        → 200 {status,restartedAt}
//   GET    /api/v1/k8s/clusters/{id}/services?namespace={ns}     → 200 {services: [{name,namespace,type,clusterIP,externalIP,ports}]}
//   GET    /api/v1/k8s/clusters/{id}/configmaps?namespace={ns}   → 200 {configmaps: [{name,namespace,dataKeys}]}
//   GET    /api/v1/k8s/clusters/{id}/secrets?namespace={ns}      → 200 {secrets: [{name,namespace,type,dataKeys}]}
//   GET    /api/v1/k8s/clusters/{id}/nodes                       → 200 {nodes: [{name,status,roles,version,internalIP,externalIP,cpu,memory}]}
// ============================================================

// ---------- K8s 集群管理 ----------
// 列出所有集群（kubeconfig 已脱敏为 ***）
export function getK8sClusters() {
  return authGet('/api/v1/k8s/clusters');
}

// 添加集群
export async function createK8sCluster(name, server, kubeconfig) {
  return await request('/api/v1/k8s/clusters', jsonBody({ name: name, server: server, kubeconfig: kubeconfig }));
}

// 删除集群：返回 {s, j}（j 为 null，204）
export function deleteK8sCluster(id) {
  const h = {};
  const token = getToken();
  if (token) h['Authorization'] = 'Bearer ' + token;
  return fetch('/api/v1/k8s/clusters/' + encodeURIComponent(id), { method: 'DELETE', headers: h })
    .then(function (r) { return { s: r.status, j: null }; });
}

// 测试集群连接：返回 {s, j}，j 形如 {status, message}
export function testK8sCluster(id) {
  return authPost('/api/v1/k8s/clusters/' + encodeURIComponent(id) + '/test');
}

// ---------- K8s 资源管理 ----------
// 列出 namespace
export function getK8sNamespaces(clusterID) {
  return authGet('/api/v1/k8s/clusters/' + encodeURIComponent(clusterID) + '/namespaces');
}

// 列出 pod（按 namespace 过滤）
export function getK8sPods(clusterID, namespace) {
  const qs = namespace ? '?namespace=' + encodeURIComponent(namespace) : '';
  return authGet('/api/v1/k8s/clusters/' + encodeURIComponent(clusterID) + '/pods' + qs);
}

// 获取 pod 日志（tailLines 默认 100，container 可选）
export function getK8sPodLogs(clusterID, ns, name, tailLines, container) {
  let qs = [];
  if (tailLines) qs.push('tailLines=' + encodeURIComponent(tailLines));
  if (container) qs.push('container=' + encodeURIComponent(container));
  const q = qs.length ? '?' + qs.join('&') : '';
  return authGet('/api/v1/k8s/clusters/' + encodeURIComponent(clusterID) + '/pods/' + encodeURIComponent(ns) + '/' + encodeURIComponent(name) + '/logs' + q);
}

// 删除 pod：返回 {s, j}（j 为 null，204）
export function deleteK8sPod(clusterID, ns, name) {
  const h = {};
  const token = getToken();
  if (token) h['Authorization'] = 'Bearer ' + token;
  return fetch('/api/v1/k8s/clusters/' + encodeURIComponent(clusterID) + '/pods/' + encodeURIComponent(ns) + '/' + encodeURIComponent(name), { method: 'DELETE', headers: h })
    .then(function (r) { return { s: r.status, j: null }; });
}

// 列出 deployment（按 namespace 过滤）
export function getK8sDeployments(clusterID, namespace) {
  const qs = namespace ? '?namespace=' + encodeURIComponent(namespace) : '';
  return authGet('/api/v1/k8s/clusters/' + encodeURIComponent(clusterID) + '/deployments' + qs);
}

// 扩缩容 deployment：body {replicas}，返回 {s, j}，j 形如 {name, replicas}
export async function scaleK8sDeployment(clusterID, ns, name, replicas) {
  return await request('/api/v1/k8s/clusters/' + encodeURIComponent(clusterID) + '/deployments/' + encodeURIComponent(ns) + '/' + encodeURIComponent(name) + '/scale', jsonBody({ replicas: replicas }));
}

// 重启 deployment：返回 {s, j}，j 形如 {status, restartedAt}
export async function restartK8sDeployment(clusterID, ns, name) {
  return await request('/api/v1/k8s/clusters/' + encodeURIComponent(clusterID) + '/deployments/' + encodeURIComponent(ns) + '/' + encodeURIComponent(name) + '/restart', jsonBody({}));
}

// 列出 service（按 namespace 过滤）
export function getK8sServices(clusterID, namespace) {
  const qs = namespace ? '?namespace=' + encodeURIComponent(namespace) : '';
  return authGet('/api/v1/k8s/clusters/' + encodeURIComponent(clusterID) + '/services' + qs);
}

// 列出 configmap（按 namespace 过滤）
export function getK8sConfigMaps(clusterID, namespace) {
  const qs = namespace ? '?namespace=' + encodeURIComponent(namespace) : '';
  return authGet('/api/v1/k8s/clusters/' + encodeURIComponent(clusterID) + '/configmaps' + qs);
}

// 列出 secret（按 namespace 过滤）
export function getK8sSecrets(clusterID, namespace) {
  const qs = namespace ? '?namespace=' + encodeURIComponent(namespace) : '';
  return authGet('/api/v1/k8s/clusters/' + encodeURIComponent(clusterID) + '/secrets' + qs);
}

// 列出 node
export function getK8sNodes(clusterID) {
  return authGet('/api/v1/k8s/clusters/' + encodeURIComponent(clusterID) + '/nodes');
}

// ============================================================
// task 241 M2 集成：告警规则引擎 + 静默规则 + 通知渠道 + 通知模板 API
// ============================================================
//
// 告警规则引擎（alertengine.AlertRule 多条件）：
//   GET    /api/v1/alert-rules-engine           → 200 [AlertRule]
//   POST   /api/v1/alert-rules-engine           → 201 AlertRule
//   GET    /api/v1/alert-rules-engine/{id}      → 200 AlertRule
//   PUT    /api/v1/alert-rules-engine/{id}      → 200 AlertRule
//   DELETE /api/v1/alert-rules-engine/{id}      → 200 {status}
//
// 静默规则：
//   GET    /api/v1/alert-silences               → 200 [SilenceRule]
//   POST   /api/v1/alert-silences               → 201 SilenceRule
//   DELETE /api/v1/alert-silences/{id}          → 200 {status}
//
// 通知渠道：
//   GET    /api/v1/notify-channels              → 200 [NotifyChannel]
//   POST   /api/v1/notify-channels              → 201 NotifyChannel
//   PUT    /api/v1/notify-channels/{id}         → 200 NotifyChannel
//   DELETE /api/v1/notify-channels/{id}         → 200 {status}
//   POST   /api/v1/notify-channels/{id}/test    → 200 {status}
//
// 通知模板：
//   GET    /api/v1/notify-templates             → 200 [NotifyTemplate]
//   POST   /api/v1/notify-templates             → 201 NotifyTemplate
//   PUT    /api/v1/notify-templates/{id}        → 200 NotifyTemplate
//   DELETE /api/v1/notify-templates/{id}        → 200 {status}
// ============================================================

// ---------- 告警规则引擎（多条件） ----------
// 列出告警规则
export function getAlertRulesEngine() {
  return authGet('/api/v1/alert-rules-engine');
}

// 创建告警规则
export async function createAlertRuleEngine(body) {
  return await request('/api/v1/alert-rules-engine', jsonBody(body));
}

// 获取单条告警规则
export function getAlertRuleEngine(id) {
  return authGet('/api/v1/alert-rules-engine/' + encodeURIComponent(id));
}

// 更新告警规则
export async function updateAlertRuleEngine(id, body) {
  return await request('/api/v1/alert-rules-engine/' + encodeURIComponent(id), jsonMethod('PUT', body));
}

// 删除告警规则：返回 {s, j}
export function deleteAlertRuleEngine(id) {
  const h = {};
  const token = getToken();
  if (token) h['Authorization'] = 'Bearer ' + token;
  return fetch('/api/v1/alert-rules-engine/' + encodeURIComponent(id), { method: 'DELETE', headers: h })
    .then(function (r) { return { s: r.status, j: null }; });
}

// ---------- 静默规则 ----------
// 列出静默规则
export function getAlertSilences() {
  return authGet('/api/v1/alert-silences');
}

// 创建静默规则
export async function createAlertSilence(body) {
  return await request('/api/v1/alert-silences', jsonBody(body));
}

// 删除静默规则
export function deleteAlertSilence(id) {
  const h = {};
  const token = getToken();
  if (token) h['Authorization'] = 'Bearer ' + token;
  return fetch('/api/v1/alert-silences/' + encodeURIComponent(id), { method: 'DELETE', headers: h })
    .then(function (r) { return { s: r.status, j: null }; });
}

// ---------- 通知渠道 ----------
// 列出通知渠道
export function getNotifyChannels() {
  return authGet('/api/v1/notify-channels');
}

// 创建通知渠道
export async function createNotifyChannel(body) {
  return await request('/api/v1/notify-channels', jsonBody(body));
}

// 更新通知渠道
export async function updateNotifyChannel(id, body) {
  return await request('/api/v1/notify-channels/' + encodeURIComponent(id), jsonMethod('PUT', body));
}

// 删除通知渠道
export function deleteNotifyChannel(id) {
  const h = {};
  const token = getToken();
  if (token) h['Authorization'] = 'Bearer ' + token;
  return fetch('/api/v1/notify-channels/' + encodeURIComponent(id), { method: 'DELETE', headers: h })
    .then(function (r) { return { s: r.status, j: null }; });
}

// 测试通知渠道
export function testNotifyChannel(id) {
  return authPost('/api/v1/notify-channels/' + encodeURIComponent(id) + '/test');
}

// ---------- 通知模板 ----------
// 列出通知模板
export function getNotifyTemplates() {
  return authGet('/api/v1/notify-templates');
}

// 创建通知模板
export async function createNotifyTemplate(body) {
  return await request('/api/v1/notify-templates', jsonBody(body));
}

// 更新通知模板
export async function updateNotifyTemplate(id, body) {
  return await request('/api/v1/notify-templates/' + encodeURIComponent(id), jsonMethod('PUT', body));
}

// 删除通知模板
export function deleteNotifyTemplate(id) {
  const h = {};
  const token = getToken();
  if (token) h['Authorization'] = 'Bearer ' + token;
  return fetch('/api/v1/notify-templates/' + encodeURIComponent(id), { method: 'DELETE', headers: h })
    .then(function (r) { return { s: r.status, j: null }; });
}

// ============================================================
// task 242 M3 集成：Helm 应用商店 API + 集群监控仪表盘 API
// ============================================================
//
// Helm 仓库管理：
//   GET    /api/v1/helm/repos                  → 200 {repos: [ChartRepo]}
//   POST   /api/v1/helm/repos                  → 201 ChartRepo
//   DELETE /api/v1/helm/repos/{name}           → 200 {status}
//
// Helm Chart 搜索：
//   GET    /api/v1/helm/charts/search?q=xxx    → 200 {charts: [ChartInfo], query}
//   GET    /api/v1/helm/repos/{name}/charts    → 200 {charts: [ChartInfo]}
//
// Helm Release 管理：
//   GET    /api/v1/helm/releases?namespace=xxx → 200 {releases: [Release]}
//   POST   /api/v1/helm/releases               → 201 Release
//   PUT    /api/v1/helm/releases/{name}        → 200 Release（body: {namespace, chart, values?}）
//   DELETE /api/v1/helm/releases/{name}?namespace=xxx → 200 {status}
//   POST   /api/v1/helm/releases/{name}/rollback → 200 Release（body: {namespace, revision?}）
//   GET    /api/v1/helm/releases/{name}/history?namespace=xxx → 200 {history: [Release], name}
//
// Helm 应用商店：
//   GET    /api/v1/helm/catalog                → 200 {items, categories, stats}
//   GET    /api/v1/helm/catalog?category=xxx   → 200 {items, category}
//   GET    /api/v1/helm/catalog?q=xxx          → 200 {items, query}
//
// 集群监控仪表盘：
//   GET    /api/v1/k8s/clusters/{id}/dashboard → 200 ClusterDashboard
//   GET    /api/v1/k8s/clusters/{id}/nodes/{node}/metrics → 200 {name, roles, cpu, memory}
//   GET    /api/v1/k8s/clusters/{id}/health    → 200 ClusterHealth
// ============================================================

// ---------- Helm 仓库管理 ----------
// 列出所有 Helm 仓库
export function getHelmRepos() {
  return authGet('/api/v1/helm/repos');
}

// 添加 Helm 仓库
export async function addHelmRepo(body) {
  return await request('/api/v1/helm/repos', jsonBody(body));
}

// 删除 Helm 仓库
export function deleteHelmRepo(name) {
  const h = {};
  const token = getToken();
  if (token) h['Authorization'] = 'Bearer ' + token;
  return fetch('/api/v1/helm/repos/' + encodeURIComponent(name), { method: 'DELETE', headers: h })
    .then(function (r) { return { s: r.status, j: null }; });
}

// 列出仓库内所有 chart
export function getHelmRepoCharts(name) {
  return authGet('/api/v1/helm/repos/' + encodeURIComponent(name) + '/charts');
}

// ---------- Helm Chart 搜索 ----------
// 搜索 chart（跨所有仓库）
export function searchHelmCharts(q) {
  return authGet('/api/v1/helm/charts/search?q=' + encodeURIComponent(q));
}

// ---------- Helm Release 管理 ----------
// 列出 release（namespace 空则列所有 namespace）
export function getHelmReleases(namespace) {
  const qs = namespace ? '?namespace=' + encodeURIComponent(namespace) : '';
  return authGet('/api/v1/helm/releases' + qs);
}

// 安装 release
export async function installHelmRelease(body) {
  return await request('/api/v1/helm/releases', jsonBody(body));
}

// 升级 release
export async function upgradeHelmRelease(name, body) {
  return await request('/api/v1/helm/releases/' + encodeURIComponent(name), jsonMethod('PUT', body));
}

// 卸载 release
export function uninstallHelmRelease(name, namespace) {
  const h = {};
  const token = getToken();
  if (token) h['Authorization'] = 'Bearer ' + token;
  const qs = namespace ? '?namespace=' + encodeURIComponent(namespace) : '';
  return fetch('/api/v1/helm/releases/' + encodeURIComponent(name) + qs, { method: 'DELETE', headers: h })
    .then(function (r) { return { s: r.status, j: null }; });
}

// 回滚 release
export async function rollbackHelmRelease(name, body) {
  return await request('/api/v1/helm/releases/' + encodeURIComponent(name) + '/rollback', jsonBody(body));
}

// 获取 release 历史
export function getHelmReleaseHistory(name, namespace) {
  const qs = namespace ? '?namespace=' + encodeURIComponent(namespace) : '';
  return authGet('/api/v1/helm/releases/' + encodeURIComponent(name) + '/history' + qs);
}

// ---------- Helm 应用商店目录 ----------
// 获取预置应用目录（category 或 q 可选）
export function getHelmCatalog(category, q) {
  const qs = [];
  if (category) qs.push('category=' + encodeURIComponent(category));
  if (q) qs.push('q=' + encodeURIComponent(q));
  const query = qs.length ? '?' + qs.join('&') : '';
  return authGet('/api/v1/helm/catalog' + query);
}

// ---------- 集群监控仪表盘 ----------
// 获取集群仪表盘汇总
export function getK8sClusterDashboard(clusterID) {
  return authGet('/api/v1/k8s/clusters/' + encodeURIComponent(clusterID) + '/dashboard');
}

// 获取节点指标
export function getK8sNodeMetrics(clusterID, nodeName) {
  return authGet('/api/v1/k8s/clusters/' + encodeURIComponent(clusterID) + '/nodes/' + encodeURIComponent(nodeName) + '/metrics');
}

// 获取集群健康检查
export function getK8sClusterHealth(clusterID) {
  return authGet('/api/v1/k8s/clusters/' + encodeURIComponent(clusterID) + '/health');
}

// ============================================================
// task 243 M5 集成：批量运维 / 灰度发布 / 定时任务 / 审批 API
//
// 批量运维：
//   POST   /api/v1/tasks/batch-exec          → 201 {batchID, tasks:[]}
//   GET    /api/v1/tasks/batch/{id}          → 200 批量状态
//   POST   /api/v1/tasks/canary              → 201 {canaryID, phases:[]}
//   GET    /api/v1/tasks/canary/{id}         → 200 灰度状态
//   POST   /api/v1/tasks/canary/{id}/advance → 200 推进下一阶段
//
// 定时任务：
//   POST   /api/v1/schedules                 → 201 ScheduleEntry
//   GET    /api/v1/schedules                 → 200 [ScheduleEntry]
//   PUT    /api/v1/schedules/{id}            → 200 ScheduleEntry
//   DELETE /api/v1/schedules/{id}            → 200
//   POST   /api/v1/schedules/{id}/pause      → 200 ScheduleEntry
//   POST   /api/v1/schedules/{id}/resume     → 200 ScheduleEntry
//
// 审批：
//   GET    /api/v1/approval/flows            → 200 [ApprovalFlow]
//   POST   /api/v1/approval/flows            → 201 ApprovalFlow
//   PUT    /api/v1/approval/flows/{id}       → 200 ApprovalFlow
//   DELETE /api/v1/approval/flows/{id}       → 200
//   GET    /api/v1/approval/requests         → 200 [ApprovalRequest]
//   POST   /api/v1/approval/requests         → 201 ApprovalRequest
//   GET    /api/v1/approval/requests/{id}    → 200 ApprovalRequest
//   POST   /api/v1/approval/requests/{id}/approve → 200
//   POST   /api/v1/approval/requests/{id}/reject  → 200
//   POST   /api/v1/approval/requests/{id}/cancel  → 200
//   GET    /api/v1/approval/pending          → 200 [ApprovalRequest]
//   GET    /api/v1/approval/requests/{id}/history → 200 History
// ============================================================

// ---------- 批量运维 ----------
// 批量执行
export async function batchExec(body) {
  return await request('/api/v1/tasks/batch-exec', jsonBody(body));
}

// 查询批量状态
export function getBatchStatus(batchID) {
  return authGet('/api/v1/tasks/batch/' + encodeURIComponent(batchID));
}

// ---------- 灰度发布 ----------
// 创建灰度发布
export async function createCanary(body) {
  return await request('/api/v1/tasks/canary', jsonBody(body));
}

// 查询灰度状态
export function getCanaryStatus(canaryID) {
  return authGet('/api/v1/tasks/canary/' + encodeURIComponent(canaryID));
}

// 推进灰度到下一阶段
export function advanceCanary(canaryID) {
  return authPost('/api/v1/tasks/canary/' + encodeURIComponent(canaryID) + '/advance');
}

// ---------- 定时任务管理 ----------
// 列表定时任务
export function getSchedules(status) {
  const qs = status ? '?status=' + encodeURIComponent(status) : '';
  return authGet('/api/v1/schedules' + qs);
}

// 创建定时任务
export async function createSchedule(body) {
  return await request('/api/v1/schedules', jsonBody(body));
}

// 更新定时任务
export async function updateSchedule(id, body) {
  return await request('/api/v1/schedules/' + encodeURIComponent(id), jsonMethod('PUT', body));
}

// 删除定时任务
export function deleteSchedule(id) {
  const h = {};
  const token = getToken();
  if (token) h['Authorization'] = 'Bearer ' + token;
  return fetch('/api/v1/schedules/' + encodeURIComponent(id), { method: 'DELETE', headers: h })
    .then(function (r) { return { s: r.status, j: null }; });
}

// 暂停定时任务
export function pauseSchedule(id) {
  return authPost('/api/v1/schedules/' + encodeURIComponent(id) + '/pause');
}

// 恢复定时任务
export function resumeSchedule(id) {
  return authPost('/api/v1/schedules/' + encodeURIComponent(id) + '/resume');
}

// ---------- 审批流管理 ----------
// 列表审批流
export function getApprovalFlows() {
  return authGet('/api/v1/approval/flows');
}

// 创建审批流
export async function createApprovalFlow(body) {
  return await request('/api/v1/approval/flows', jsonBody(body));
}

// 更新审批流
export async function updateApprovalFlow(id, body) {
  return await request('/api/v1/approval/flows/' + encodeURIComponent(id), jsonMethod('PUT', body));
}

// 删除审批流
export function deleteApprovalFlow(id) {
  const h = {};
  const token = getToken();
  if (token) h['Authorization'] = 'Bearer ' + token;
  return fetch('/api/v1/approval/flows/' + encodeURIComponent(id), { method: 'DELETE', headers: h })
    .then(function (r) { return { s: r.status, j: null }; });
}

// ---------- 审批请求 ----------
// 列表审批请求（status 可选过滤）
export function getApprovalRequests(status) {
  const qs = status ? '?status=' + encodeURIComponent(status) : '';
  return authGet('/api/v1/approval/requests' + qs);
}

// 提交审批请求
export async function submitApprovalRequest(body) {
  return await request('/api/v1/approval/requests', jsonBody(body));
}

// 审批请求详情
export function getApprovalRequest(id) {
  return authGet('/api/v1/approval/requests/' + encodeURIComponent(id));
}

// 审批通过
export async function approveApprovalRequest(id, comment) {
  return await request('/api/v1/approval/requests/' + encodeURIComponent(id) + '/approve', jsonBody({ comment: comment || '' }));
}

// 审批拒绝
export async function rejectApprovalRequest(id, comment) {
  return await request('/api/v1/approval/requests/' + encodeURIComponent(id) + '/reject', jsonBody({ comment: comment || '' }));
}

// 取消审批
export async function cancelApprovalRequest(id) {
  return await request('/api/v1/approval/requests/' + encodeURIComponent(id) + '/cancel', jsonBody({}));
}

// 待我审批列表
export function getPendingApprovals() {
  return authGet('/api/v1/approval/pending');
}

// 审批历史
export function getApprovalHistory(id) {
  return authGet('/api/v1/approval/requests/' + encodeURIComponent(id) + '/history');
}

// ---------- 网络拓扑与诊断（task 244 M6 集成） ----------
// GET /api/v1/network/topology[?refresh=true] — 返回网络拓扑图（节点=设备/agent，边=连通性+延迟）
// GET /api/v1/network/topology/cache         — 返回最近一次缓存的拓扑（不触发探测）
// POST /api/v1/network/diagnose              — 发起网络诊断任务（ping/traceroute/tcping/nslookup/curl）
// GET /api/v1/network/diagnose/{taskId}      — 查询诊断任务结果
// POST /api/v1/network/connectivity          — 批量连通性检测

// 获取网络拓扑（refresh=true 强制刷新缓存并重新探测）
export function getNetworkTopology(refresh) {
  const q = refresh ? '?refresh=true' : '';
  return authGet('/api/v1/network/topology' + q);
}

// 获取缓存的网络拓扑（不触发探测）
export function getNetworkTopologyCache() {
  return authGet('/api/v1/network/topology/cache');
}

// 发起网络诊断任务，返回 { taskId }
export async function diagnoseNetwork(agentId, tool, target, options) {
  const body = {
    agentId: agentId,
    tool: tool,
    target: target,
    options: options || {},
  };
  return await request('/api/v1/network/diagnose', jsonBody(body));
}

// 查询诊断任务结果（轮询用）
export function getDiagnoseResult(taskId) {
  return authGet('/api/v1/network/diagnose/' + encodeURIComponent(taskId));
}

// 批量连通性检测
export async function checkConnectivity(sourceAgentId, targets) {
  const body = {
    sourceAgentId: sourceAgentId,
    targets: targets || [],
  };
  return await request('/api/v1/network/connectivity', jsonBody(body));
}