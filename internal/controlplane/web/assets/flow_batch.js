// flow_batch.js — task 243 M5 集成：批量运维 + 灰度发布页面交互逻辑
//
// 职责：
//   - loadBatchPage：加载批量运维页面（设备多选 + 任务配置 + 执行历史）
//   - submitBatchExec：提交批量执行
//   - submitCanary：提交灰度发布
//   - pollBatchStatus / pollCanaryStatus：轮询批次/灰度状态
//
// 依赖：api.js、render.js（esc/escAttr/fmtTime/showModal）、i18n.js

import * as api from './api.js';
import { esc, escAttr, fmtTime, showModal, closeModal } from './render.js';
import { t } from './i18n.js';

// 当前批次/灰度 ID（用于状态轮询）。
let currentBatchID = '';
let currentCanaryID = '';
// 批量执行历史（内存，重启后丢失）。
const batchHistory = [];

// ============================================================================
// 页面加载
// ============================================================================

export function loadBatchPage() {
  // 初始化默认策略选项。
  const strategySel = document.getElementById('canaryStrategy');
  if (strategySel && !strategySel.dataset.init) {
    strategySel.dataset.init = '1';
    onCanaryStrategyChange();
  }
  // 渲染历史。
  renderBatchHistory();
}

// ============================================================================
// 批量执行
// ============================================================================

export function submitBatchExec() {
  const idsText = (document.getElementById('batchDeviceIds') || {}).value || '';
  const taskType = (document.getElementById('batchTaskType') || {}).value || 'shell';
  const command = (document.getElementById('batchCommand') || {}).value || '';
  const timeoutStr = (document.getElementById('batchTimeout') || {}).value || '0';
  const timeout = parseInt(timeoutStr, 10) || 0;

  const deviceIDs = parseDeviceIDs(idsText);
  if (deviceIDs.length === 0) {
    alert(t('batch.noDevices'));
    return;
  }
  if (!command) {
    alert(t('batch.command'));
    return;
  }

  api.batchExec({ deviceIDs: deviceIDs, taskType: taskType, command: command, timeout: timeout })
    .then(function (r) {
      if (r.s === 201 && r.j) {
        currentBatchID = r.j.batchID;
        batchHistory.unshift({
          batchID: r.j.batchID,
          taskType: taskType,
          command: command,
          createdAt: new Date().toISOString(),
          tasks: r.j.tasks || [],
        });
        if (batchHistory.length > 20) batchHistory.length = 20;
        renderBatchHistory();
        renderBatchDetail(r.j);
      } else {
        alert(t('batch.loadFail') + ': ' + (r.j && r.j.error ? r.j.error : 'HTTP ' + r.s));
      }
    })
    .catch(function (e) { alert(t('batch.loadFail') + ': ' + e.message); });
}

// parseDeviceIDs 解析设备 ID 输入（逗号/换行/空格分隔）。
function parseDeviceIDs(text) {
  return text.split(/[\s,\n]+/).map(function (s) { return s.trim(); }).filter(Boolean);
}

// renderBatchHistory 渲染批量执行历史列表。
function renderBatchHistory() {
  const el = document.getElementById('batchHistoryList');
  if (!el) return;
  if (!batchHistory.length) {
    el.innerHTML = '<p class="muted">' + esc(t('batch.empty')) + '</p>';
    return;
  }
  let html = '<table class="table"><thead><tr>'
    + '<th>' + esc(t('batch.batchID')) + '</th>'
    + '<th>' + esc(t('batch.taskType')) + '</th>'
    + '<th>' + esc(t('batch.command')) + '</th>'
    + '<th>' + esc(t('batch.status')) + '</th>'
    + '<th>' + esc(t('batch.refresh')) + '</th>'
    + '</tr></thead><tbody>';
  batchHistory.forEach(function (b) {
    const ok = b.tasks.filter(function (x) { return x.status === 'done'; }).length;
    const total = b.tasks.length;
    const status = ok + '/' + total;
    html += '<tr>'
      + '<td><code>' + esc(b.batchID) + '</code></td>'
      + '<td>' + esc(b.taskType) + '</td>'
      + '<td><code>' + esc(b.command) + '</code></td>'
      + '<td>' + esc(status) + '</td>'
      + '<td><button class="btn btn-sm" onclick="pollBatchStatus(\'' + escAttr(b.batchID) + '\')">' + esc(t('batch.refresh')) + '</button></td>'
      + '</tr>';
  });
  html += '</tbody></table>';
  el.innerHTML = html;
}

// renderBatchDetail 渲染批次详情（每设备任务状态）。
function renderBatchDetail(batch) {
  const el = document.getElementById('batchDetail');
  if (!el) return;
  let html = '<h4>' + esc(t('batch.batchID')) + ': <code>' + esc(batch.batchID) + '</code></h4>';
  html += '<table class="table"><thead><tr>'
    + '<th>' + esc(t('batch.deviceID')) + '</th>'
    + '<th>' + esc(t('batch.taskID')) + '</th>'
    + '<th>' + esc(t('batch.status')) + '</th>'
    + '<th>' + esc(t('batch.error')) + '</th>'
    + '</tr></thead><tbody>';
  (batch.tasks || []).forEach(function (it) {
    html += '<tr>'
      + '<td>' + esc(it.deviceID) + '</td>'
      + '<td><code>' + esc(it.taskID || '-') + '</code></td>'
      + '<td>' + esc(it.status) + '</td>'
      + '<td>' + esc(it.error || '') + '</td>'
      + '</tr>';
  });
  html += '</tbody></table>';
  el.innerHTML = html;
}

// pollBatchStatus 查询并刷新批次状态。
export function pollBatchStatus(batchID) {
  api.getBatchStatus(batchID).then(function (data) {
    renderBatchDetail(data);
    // 更新历史中的对应记录。
    for (let i = 0; i < batchHistory.length; i++) {
      if (batchHistory[i].batchID === batchID) {
        batchHistory[i].tasks = data.tasks || batchHistory[i].tasks;
        break;
      }
    }
    renderBatchHistory();
  }).catch(function (e) { alert(t('batch.statusFail') + ': ' + e.message); });
}

// ============================================================================
// 灰度发布
// ============================================================================

export function onCanaryStrategyChange() {
  const strategy = (document.getElementById('canaryStrategy') || {}).value || 'percentage';
  const pctEl = document.getElementById('canaryPercentageRow');
  const grpEl = document.getElementById('canaryGroupsRow');
  const lblEl = document.getElementById('canaryLabelsRow');
  if (pctEl) pctEl.style.display = strategy === 'percentage' ? '' : 'none';
  if (grpEl) grpEl.style.display = strategy === 'group' ? '' : 'none';
  if (lblEl) lblEl.style.display = strategy === 'label' ? '' : 'none';
}

export function submitCanary() {
  const idsText = (document.getElementById('canaryDeviceIds') || {}).value || '';
  const taskType = (document.getElementById('canaryTaskType') || {}).value || 'shell';
  const command = (document.getElementById('canaryCommand') || {}).value || '';
  const strategy = (document.getElementById('canaryStrategy') || {}).value || 'percentage';
  const percentage = parseInt((document.getElementById('canaryPercentage') || {}).value || '10', 10) || 10;
  const groupsText = (document.getElementById('canaryGroups') || {}).value || '';
  const labelsText = (document.getElementById('canaryLabels') || {}).value || '';

  const deviceIDs = parseDeviceIDs(idsText);
  if (deviceIDs.length === 0) {
    alert(t('batch.noDevices'));
    return;
  }
  if (!command) {
    alert(t('batch.command'));
    return;
  }

  const body = {
    deviceIDs: deviceIDs,
    taskType: taskType,
    command: command,
    strategy: strategy,
  };
  if (strategy === 'percentage') body.percentage = percentage;
  if (strategy === 'group') body.groups = groupsText.split(',').map(function (s) { return s.trim(); }).filter(Boolean);
  if (strategy === 'label') body.labels = parseLabels(labelsText);

  api.createCanary(body).then(function (r) {
    if (r.s === 201 && r.j) {
      currentCanaryID = r.j.canaryID;
      pollCanaryStatus(r.j.canaryID);
    } else {
      alert(t('batch.loadFail') + ': ' + (r.j && r.j.error ? r.j.error : 'HTTP ' + r.s));
    }
  }).catch(function (e) { alert(t('batch.loadFail') + ': ' + e.message); });
}

// parseLabels 解析 key=value,key2=value2 格式。
function parseLabels(text) {
  const out = {};
  text.split(',').forEach(function (kv) {
    const parts = kv.split('=');
    if (parts.length === 2) {
      out[parts[0].trim()] = parts[1].trim();
    }
  });
  return out;
}

// pollCanaryStatus 查询并渲染灰度发布状态。
export function pollCanaryStatus(canaryID) {
  api.getCanaryStatus(canaryID).then(function (data) {
    renderCanaryDetail(data);
  }).catch(function (e) { alert(t('batch.statusFail') + ': ' + e.message); });
}

// renderCanaryDetail 渲染灰度发布详情。
function renderCanaryDetail(canary) {
  const el = document.getElementById('canaryDetail');
  if (!el) return;
  let html = '<h4>' + esc(t('canary.canaryID')) + ': <code>' + esc(canary.canaryID) + '</code></h4>';
  html += '<p><b>' + esc(t('canary.strategy')) + '</b>: ' + esc(canary.strategy) + '</p>';
  html += '<h5>' + esc(t('canary.phases')) + '</h5>';
  (canary.phases || []).forEach(function (p) {
    html += '<div class="card" style="margin-bottom:12px;padding:12px">'
      + '<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:8px">'
      + '<b>' + esc(t('canary.phase')) + ' ' + p.phase + '</b>'
      + '<span class="badge">' + esc(p.status) + '</span>'
      + '</div>'
      + '<p class="muted">设备: ' + esc((p.deviceIDs || []).join(', ')) + '</p>';
    if (p.tasks && p.tasks.length) {
      html += '<table class="table"><thead><tr>'
        + '<th>' + esc(t('batch.deviceID')) + '</th>'
        + '<th>' + esc(t('batch.taskID')) + '</th>'
        + '<th>' + esc(t('batch.status')) + '</th>'
        + '</tr></thead><tbody>';
      p.tasks.forEach(function (it) {
        html += '<tr><td>' + esc(it.deviceID) + '</td>'
          + '<td><code>' + esc(it.taskID || '-') + '</code></td>'
          + '<td>' + esc(it.status) + '</td></tr>';
      });
      html += '</tbody></table>';
    }
    if (p.status === 'pending') {
      html += '<button class="btn btn-primary btn-sm" onclick="advanceCanary(\'' + escAttr(canary.canaryID) + '\')">' + esc(t('canary.advance')) + '</button>';
    }
    html += '</div>';
  });
  el.innerHTML = html;
}

export function advanceCanary(canaryID) {
  api.advanceCanary(canaryID).then(function (r) {
    if (r.s === 200) {
      pollCanaryStatus(canaryID);
    } else {
      alert(t('canary.noPendingPhase'));
    }
  }).catch(function (e) { alert(t('canary.noPendingPhase') + ': ' + e.message); });
}

// 暴露给 window 兼容层（main.js 会挂载）。
export { closeModal, showModal };