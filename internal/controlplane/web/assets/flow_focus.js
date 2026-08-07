// flow_focus.js — 跨模块联动（F1）：focus 状态
// 从 flow.js 拆分（P2-1）。职责：focus 状态管理 + 跨模块跳转 + setRenderDeps 注入。
// 依赖：poll.js（pollTasks）、flow_alerts.js（pollAlertsFull）、flow_deploys.js（pollDeploys）、
//       flow_tab.js（switchTab）、flow_logs.js（searchLogs）、render.js（setRenderDeps）。

import * as api from './api.js';
import { esc, escAttr, setRenderDeps } from './render.js';
import { pollTasks } from './poll.js';
import { icon } from './icons.js';
import { t } from './i18n.js';
import { pollAlertsFull } from './flow_alerts.js';
import { pollDeploys } from './flow_deploys.js';
import { switchTab } from './flow_tab.js';
import { searchLogs } from './flow_logs.js';

// ---------- 跨模块联动（F1）：focus 状态 ----------
let focusDevice = null;

export function getFocusDevice() { return focusDevice; }

export function setFocus(id, ip, agentID, segment) {
  focusDevice = { id: id, ip: ip || '', agentID: agentID || '', segment: segment || '' };
  const b = document.getElementById('ctxbar'); if (b) b.classList.add('show');
  const d = document.getElementById('ctxDev'); if (d) d.textContent = id + (ip ? (' (' + ip + ')') : '');
}

export function clearFocus() {
  focusDevice = null;
  const b = document.getElementById('ctxbar'); if (b) b.classList.remove('show');
  pollTasks(); pollAlertsFull(); pollDeploys();
}

export function applyFocus(list, kind) {
  if (!focusDevice || !list) return list;
  return list.filter(function (x) {
    if (kind === 'task') return x.agentID === focusDevice.agentID;
    if (kind === 'alert') return (x.deviceID === focusDevice.id) || (x.agentID === focusDevice.agentID);
    if (kind === 'deploy') { const ts = ((x.target_ids || '') + '').split(/[,\s]+/).map(function (s) { return s.trim(); }).filter(Boolean); return ts.indexOf(focusDevice.id) >= 0; }
    return true;
  });
}

export function jumpFocus(tab) {
  if (!focusDevice) return;
  if (tab === 'logs') { switchTab('logs'); const di = document.getElementById('logDevice'); if (di) di.value = focusDevice.id; searchLogs(0); return; }
  if (tab === 'cmdb') { focusCI(); return; }
  switchTab(tab);
}

export function focusCI() {
  switchTab('cmdb');
  if (!focusDevice) return;
  api.getCMDBTypes().then(function (ts) {
    Promise.all((ts || []).map(function (t) {
      return api.getCIs(t.type).then(function (arr) { return (arr || []).filter(function (c) { return c.deviceID === focusDevice.id; }); });
    })).then(function (groups) {
      const all = []; groups.forEach(function (g) { all.push.apply(all, g); });
      const el = document.getElementById('ciList');
      if (!all.length) { el.innerHTML = '<p class="muted">' + esc(t('flow.msg.noCiForDevice')) + '</p>'; return; }
      let html = '<p class="hint">' + icon('context', 14) + ' ' + t('flow.msg.filteredByDevice', { id: '<code>' + esc(focusDevice.id) + '</code>', n: all.length }) + '</p>';
      html += '<div class="table-wrap"><table><colgroup><col style="width:30%"><col style="width:30%"><col style="width:20%"><col style="width:20%"></colgroup><thead><tr><th>' + esc(t('cmdb.col.id')) + '</th><th>' + esc(t('cmdb.col.name')) + '</th><th>' + esc(t('cmdb.col.type')) + '</th><th>' + esc(t('cmdb.col.status')) + '</th></tr></thead><tbody>';
      all.forEach(function (c) { html += '<tr class="ci" onclick="openCI(\'' + escAttr(c.id) + '\')"><td><code title="' + esc(c.id) + '">' + esc(c.id) + '</code></td><td>' + esc(c.name) + '</td><td>' + esc(c.ciType) + '</td><td>' + esc(c.status) + '</td></tr>'; });
      html += '</tbody></table></div>';
      el.innerHTML = html;
    }).catch(function (e) { console.error(e); });
  }).catch(function (e) { console.error(e); });
}

// 将 focus 相关回调注入 render.js（避免循环依赖）
setRenderDeps({ setFocus: setFocus, openDevice: null, focusDevice: getFocusDevice, applyFocus: applyFocus });