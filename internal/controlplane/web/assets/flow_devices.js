// flow_devices.js — 设备纳管操作 + 身份注入 + Agent 加载 + 任务下发
// 从 flow.js 拆分（P2-1）。职责：openDevice/provision 设备纳管、fetchMe 身份注入、
//       loadAgents Agent 下拉、submitTaskForm 任务下发表单。
// 依赖：api.js、render.js（setRenderDeps/esc/escAttr/fmtTime）、poll.js（pollDevices/pollTasks）。

import * as api from './api.js';
import { esc, escAttr, fmtTime, setRenderDeps } from './render.js';
import { pollDevices, pollTasks } from './poll.js';
import { t } from './i18n.js';

// ---------- 设备详情抽屉 / 纳管 ----------
export function openDevice(id) {
  api.getDevice(id).then(function (d) {
    const dev = d.device || {};
    let h = '<h3>' + esc(t('device.detail.heading', { id: dev.deviceID })) + '</h3>';
    h += '<p>' + esc(t('device.detail.ipAgentTenant', { ip: dev.ip, agent: dev.agentID, tenant: dev.tenantID })) + '</p>';
    h += '<p>' + esc(t('device.detail.stateTask', { state: dev.state, taskState: dev.taskState })) + '</p>';
    if (dev.lastResult) {
      const c = dev.lastResult === 'failed' ? 'warn' : 'ok';
      h += '<p class="msg ' + c + '">' + esc(t('device.detail.lastResult', { result: dev.lastResult, time: fmtTime(dev.lastResultAt) })) + '</p>';
    }
    if (dev.state === 'discovered') {
      h += '<button onclick="provision(\'' + escAttr(dev.deviceID) + '\')">' + esc(t('device.detail.provisionBtn')) + '</button> ';
    }
    h += '<h4>' + esc(t('device.detail.tasksHeading')) + '</h4><div class="table-wrap"><table><colgroup><col style="width:50%"><col style="width:25%"><col style="width:25%"></colgroup><thead><tr><th>ID</th><th>' + esc(t('cmdb.col.type')) + '</th><th>' + esc(t('cmdb.col.status')) + '</th></tr></thead><tbody>';
    (d.tasks || []).forEach(function (t) { h += '<tr><td><code title="' + esc(t.taskID) + '">' + esc(t.taskID) + '</code></td><td>' + esc(t.type) + '</td><td>' + esc(t.status) + '</td></tr>'; });
    h += '</tbody></table></div>';
    h += '<h4>' + esc(t('device.detail.recentResults')) + '</h4><div class="table-wrap"><table><colgroup><col style="width:30%"><col style="width:15%"><col style="width:55%"></colgroup><thead><tr><th>' + esc(t('device.detail.col.task')) + '</th><th>' + esc(t('device.detail.col.exitCode')) + '</th><th>' + esc(t('device.detail.col.output')) + '</th></tr></thead><tbody>';
    (d.results || []).slice(0, 5).forEach(function (r) { h += '<tr><td><code title="' + esc(r.taskID) + '">' + esc(r.taskID) + '</code></td><td>' + esc(r.exitCode) + '</td><td><code title="' + esc(r.stdout) + '">' + esc(r.stdout) + '</code></td></tr>'; });
    h += '</tbody></table></div>';
    document.getElementById('drawerBody').innerHTML = h;
    document.getElementById('drawer').classList.add('open');
  }).catch(function (e) { console.error(e); });
}
setRenderDeps({ openDevice: openDevice });

export function provision(id) {
  api.provisionDevice(id).then(function (x) {
    document.getElementById('drawerBody').insertAdjacentHTML('beforeend', '<p class="msg ok">[' + x.s + '] ' + esc(JSON.stringify(x.j)) + '</p>');
    pollDevices();
  }).catch(function (e) { console.error(e); });
}

// ---------- 动态身份注入（F3） ----------
export function fetchMe() {
  api.getMe().then(function (x) {
    if (x.s !== 200) return;
    const t = x.j.tenantID || 'default'; const u = x.j.userID || 'local';
    const role = (x.j.roles && x.j.roles.length) ? x.j.roles.join('/') : 'admin';
    const te = document.getElementById('idTenant'); if (te) te.textContent = t;
    const re = document.getElementById('idRole'); if (re) re.textContent = role;
    const chip = document.getElementById('identityChip'); if (chip) chip.title = t('device.detail.identityTooltip', { tenant: t, user: u });
  }).catch(function (e) { console.error('me', e); });
}

// ---------- Agents 下拉加载 ----------
export function loadAgents() {
  api.getAgents().then(function (a) {
    const sel = document.getElementById('agentID'); sel.innerHTML = '';
    (a || []).forEach(function (x) { const o = document.createElement('option'); o.value = x.agentID; o.textContent = x.agentID + ' (' + x.hostname + ')'; sel.appendChild(o); });
  }).catch(function (e) { console.error(e); });
}

// ---------- 任务下发表单提交 ----------
export function submitTaskForm() {
  const body = {
    agentID: document.getElementById('agentID').value,
    type: document.getElementById('type').value,
    command: document.getElementById('command').value,
    path: document.getElementById('path').value,
    content: document.getElementById('content').value,
  };
  api.createTask(body)
    .then(function (x) { const el = document.getElementById('taskResult'); el.className = 'msg ' + (x.s < 400 ? 'ok' : 'err'); el.textContent = '[' + x.s + '] ' + JSON.stringify(x.j); pollTasks(); pollDevices(); })
    .catch(function (err) { const el = document.getElementById('taskResult'); el.className = 'msg err'; el.textContent = t('flow.msg.errorPrefix', { err: err }); });
}