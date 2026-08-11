// flow_approval.js — task 243 M5 集成：审批管理页面交互逻辑
//
// 职责：
//   - loadApprovalPage：加载审批管理页面（默认显示待我审批）
//   - loadApprovalFlows / showCreateFlowModal / confirmCreateFlow / deleteFlowConfirm：审批流 CRUD
//   - loadPendingApprovals / approveRequest / rejectRequest / cancelRequest：审批请求操作
//   - loadApprovalHistory：审批历史时间线
//
// 依赖：api.js、render.js（esc/escAttr/fmtTime/showModal/closeModal）、i18n.js

import * as api from './api.js';
import { esc, escAttr, fmtTime, showModal, closeModal } from './render.js';
import { t } from './i18n.js';

// 当前编辑的审批流 ID。
let editingFlowID = '';
// 当前查看历史的请求 ID。
let historyRequestID = '';

// ============================================================================
// 页面加载
// ============================================================================

export function loadApprovalPage() {
  // 默认加载待我审批。
  switchApprovalTab('pending');
}

export function switchApprovalTab(tab) {
  // 切换子标签 active。
  ['flows', 'pending', 'history'].forEach(function (x) {
    const el = document.getElementById('approvalTab-' + x);
    if (el) el.classList.toggle('active', x === tab);
  });
  // 切换 panel 显示。
  const flowsPanel = document.getElementById('approvalFlowsPanel');
  const pendingPanel = document.getElementById('approvalPendingPanel');
  const historyPanel = document.getElementById('approvalHistoryPanel');
  if (flowsPanel) flowsPanel.style.display = tab === 'flows' ? '' : 'none';
  if (pendingPanel) pendingPanel.style.display = tab === 'pending' ? '' : 'none';
  if (historyPanel) historyPanel.style.display = tab === 'history' ? '' : 'none';
  if (tab === 'flows') loadApprovalFlows();
  if (tab === 'pending') loadPendingApprovals();
  if (tab === 'history') loadApprovalHistoryList();
}

// ============================================================================
// 审批流 CRUD
// ============================================================================

export function loadApprovalFlows() {
  api.getApprovalFlows().then(function (data) {
    renderApprovalFlows(data.flows || []);
  }).catch(function (e) { api.apiFail('approvalFlows', e); });
}

function renderApprovalFlows(flows) {
  const el = document.getElementById('approvalFlowsList');
  if (!el) return;
  if (!flows.length) {
    el.innerHTML = '<p class="muted">' + esc(t('approval.flowEmpty')) + '</p>';
    return;
  }
  let html = '<table class="table"><thead><tr>'
    + '<th>' + esc(t('approval.flowName')) + '</th>'
    + '<th>' + esc(t('approval.flowTrigger')) + '</th>'
    + '<th>' + esc(t('approval.flowSteps')) + '</th>'
    + '<th>' + esc(t('approval.flowEnabled')) + '</th>'
    + '<th>' + esc(t('schedule.actions')) + '</th>'
    + '</tr></thead><tbody>';
  flows.forEach(function (f) {
    const stepSummary = (f.steps || []).map(function (s) {
      return esc(s.name || s.id) + ' [' + esc(s.mode) + ']';
    }).join(' → ');
    html += '<tr>'
      + '<td>' + esc(f.name) + '</td>'
      + '<td><code>' + esc(f.triggerType) + '</code></td>'
      + '<td>' + stepSummary + '</td>'
      + '<td>' + (f.enabled ? '✓' : '✗') + '</td>'
      + '<td>'
      + ' <button class="btn btn-sm" onclick="showEditFlowModal(\'' + escAttr(f.id) + '\')">' + esc(t('schedule.edit')) + '</button>'
      + ' <button class="btn btn-sm btn-danger" onclick="deleteFlowConfirm(\'' + escAttr(f.id) + '\')">' + esc(t('schedule.delete')) + '</button>'
      + '</td>'
      + '</tr>';
  });
  html += '</tbody></table>';
  el.innerHTML = html;
}

export function showCreateFlowModal() {
  editingFlowID = '';
  const body = ''
    + '<div class="form-group"><label>' + esc(t('approval.flowName')) + '</label>'
    + '<input type="text" id="flowName" class="form-control"></div>'
    + '<div class="form-group"><label>' + esc(t('approval.flowTrigger')) + '</label>'
    + '<select id="flowTrigger" class="form-control">'
    + '<option value="shell">shell</option>'
    + '<option value="batch_restart">batch_restart</option>'
    + '<option value="config_change">config_change</option>'
    + '<option value="k8s_delete">k8s_delete</option>'
    + '<option value="deploy">deploy</option>'
    + '</select></div>'
    + '<div class="form-group"><label>' + esc(t('approval.stepMode')) + '</label>'
    + '<select id="stepMode" class="form-control">'
    + '<option value="sequential">' + esc(t('approval.stepSequential')) + '</option>'
    + '<option value="countersign">' + esc(t('approval.stepCountersign')) + '</option>'
    + '<option value="anyof">' + esc(t('approval.stepAnyOf')) + '</option>'
    + '</select></div>'
    + '<div class="form-group"><label>' + esc(t('approval.stepApprovers')) + '</label>'
    + '<input type="text" id="stepApprovers" class="form-control" placeholder="' + escAttr(t('approval.stepApproversPh')) + '"></div>'
    + '<div class="form-group"><label>' + esc(t('approval.stepTimeout')) + '</label>'
    + '<input type="number" id="stepTimeout" class="form-control" value="30"></div>';
  const footer = ''
    + '<button class="btn" onclick="closeModal()">' + esc(t('approval.cancel')) + '</button>'
    + '<button class="btn btn-primary" onclick="confirmCreateFlow()">' + esc(t('approval.flowCreate')) + '</button>';
  showModal(t('approval.flowCreate'), body, footer);
}

export function confirmCreateFlow() {
  const name = (document.getElementById('flowName') || {}).value || '';
  const trigger = (document.getElementById('flowTrigger') || {}).value || 'shell';
  const mode = (document.getElementById('stepMode') || {}).value || 'sequential';
  const approvers = (document.getElementById('stepApprovers') || {}).value || '';
  const timeoutMin = parseInt((document.getElementById('stepTimeout') || {}).value || '30', 10) || 30;
  if (!name || !approvers) {
    alert(t('schedule.createFail') + ': name + approvers required');
    return;
  }
  const approverList = approvers.split(',').map(function (s) { return s.trim(); }).filter(Boolean);
  const body = {
    name: name,
    triggerType: trigger,
    enabled: true,
    steps: [{
      id: trigger + '-1',
      name: name,
      order: 1,
      mode: mode,
      approvers: approverList,
      timeout: timeoutMin * 60 * 1000000000, // 分钟 → 纳秒（Go time.Duration）
    }],
  };
  api.createApprovalFlow(body).then(function (r) {
    if (r.s === 201) {
      closeModal();
      loadApprovalFlows();
    } else {
      alert(t('schedule.createFail') + ': ' + (r.j && r.j.error ? r.j.error : 'HTTP ' + r.s));
    }
  }).catch(function (e) { alert(t('schedule.createFail') + ': ' + e.message); });
}

export function showEditFlowModal(id) {
  editingFlowID = id;
  api.getApprovalFlows().then(function (data) {
    const f = (data.flows || []).find(function (x) { return x.id === id; });
    if (!f) { alert(t('approval.flowLoadFail')); return; }
    const step = (f.steps || [])[0] || {};
    const body = ''
      + '<div class="form-group"><label>' + esc(t('approval.flowName')) + '</label>'
      + '<input type="text" id="flowName" class="form-control" value="' + escAttr(f.name) + '"></div>'
      + '<div class="form-group"><label>' + esc(t('approval.flowTrigger')) + '</label>'
      + '<input type="text" class="form-control" value="' + escAttr(f.triggerType) + '" disabled></div>'
      + '<div class="form-group"><label>' + esc(t('approval.stepMode')) + '</label>'
      + '<select id="stepMode" class="form-control">'
      + '<option value="sequential"' + (step.mode === 'sequential' ? ' selected' : '') + '>' + esc(t('approval.stepSequential')) + '</option>'
      + '<option value="countersign"' + (step.mode === 'countersign' ? ' selected' : '') + '>' + esc(t('approval.stepCountersign')) + '</option>'
      + '<option value="anyof"' + (step.mode === 'anyof' ? ' selected' : '') + '>' + esc(t('approval.stepAnyOf')) + '</option>'
      + '</select></div>'
      + '<div class="form-group"><label>' + esc(t('approval.stepApprovers')) + '</label>'
      + '<input type="text" id="stepApprovers" class="form-control" value="' + escAttr((step.approvers || []).join(',')) + '"></div>'
      + '<div class="form-group"><label>' + esc(t('approval.stepTimeout')) + '</label>'
      + '<input type="number" id="stepTimeout" class="form-control" value="' + (step.timeout ? Math.round(step.timeout / 60000000000) : 30) + '"></div>';
    const footer = ''
      + '<button class="btn" onclick="closeModal()">' + esc(t('approval.cancel')) + '</button>'
      + '<button class="btn btn-primary" onclick="confirmEditFlow()">' + esc(t('schedule.edit')) + '</button>';
    showModal(t('approval.flowEdit'), body, footer);
  }).catch(function (e) { alert(t('approval.flowLoadFail') + ': ' + e.message); });
}

export function confirmEditFlow() {
  const name = (document.getElementById('flowName') || {}).value || '';
  const mode = (document.getElementById('stepMode') || {}).value || 'sequential';
  const approvers = (document.getElementById('stepApprovers') || {}).value || '';
  const timeoutMin = parseInt((document.getElementById('stepTimeout') || {}).value || '30', 10) || 30;
  const approverList = approvers.split(',').map(function (s) { return s.trim(); }).filter(Boolean);
  const body = {
    name: name,
    enabled: true,
    steps: [{
      id: 'step-1',
      name: name,
      order: 1,
      mode: mode,
      approvers: approverList,
      timeout: timeoutMin * 60 * 1000000000,
    }],
  };
  api.updateApprovalFlow(editingFlowID, body).then(function (r) {
    if (r.s === 200) {
      closeModal();
      loadApprovalFlows();
    } else {
      alert(t('schedule.saveFail') + ': ' + (r.j && r.j.error ? r.j.error : 'HTTP ' + r.s));
    }
  }).catch(function (e) { alert(t('schedule.saveFail') + ': ' + e.message); });
}

export function deleteFlowConfirm(id) {
  if (!confirm(t('approval.flowDeleteConfirm'))) return;
  api.deleteApprovalFlow(id).then(function (r) {
    if (r.s === 200) loadApprovalFlows();
    else alert(t('schedule.saveFail'));
  }).catch(function (e) { alert(t('schedule.saveFail') + ': ' + e.message); });
}

// ============================================================================
// 待我审批
// ============================================================================

export function loadPendingApprovals() {
  api.getPendingApprovals().then(function (data) {
    renderPendingApprovals(data.pending || []);
  }).catch(function (e) { api.apiFail('pendingApprovals', e); });
}

function renderPendingApprovals(list) {
  const el = document.getElementById('approvalPendingList');
  if (!el) return;
  if (!list.length) {
    el.innerHTML = '<p class="muted">' + esc(t('approval.pendingEmpty')) + '</p>';
    return;
  }
  let html = '<table class="table"><thead><tr>'
    + '<th>ID</th>'
    + '<th>' + esc(t('approval.flowTrigger')) + '</th>'
    + '<th>' + esc(t('approval.requestTarget')) + '</th>'
    + '<th>' + esc(t('approval.requestOperator')) + '</th>'
    + '<th>' + esc(t('approval.requestRisk')) + '</th>'
    + '<th>' + esc(t('approval.requestCreatedAt')) + '</th>'
    + '<th>' + esc(t('schedule.actions')) + '</th>'
    + '</tr></thead><tbody>';
  list.forEach(function (r) {
    html += '<tr>'
      + '<td><code>' + esc(r.id) + '</code></td>'
      + '<td>' + esc(r.triggerType) + '</td>'
      + '<td>' + esc(r.target || '-') + '</td>'
      + '<td>' + esc(r.operator) + '</td>'
      + '<td>' + esc(t('approval.risk' + cap(r.risk || 'low'))) + '</td>'
      + '<td>' + esc(fmtTime(r.createdAt)) + '</td>'
      + '<td>'
      + ' <button class="btn btn-sm btn-primary" onclick="approveRequest(\'' + escAttr(r.id) + '\')">' + esc(t('approval.approve')) + '</button>'
      + ' <button class="btn btn-sm btn-danger" onclick="rejectRequest(\'' + escAttr(r.id) + '\')">' + esc(t('approval.reject')) + '</button>'
      + ' <button class="btn btn-sm" onclick="viewRequestHistory(\'' + escAttr(r.id) + '\')">' + esc(t('approval.timeline')) + '</button>'
      + '</td>'
      + '</tr>';
  });
  html += '</tbody></table>';
  el.innerHTML = html;
}

function cap(s) {
  return s.charAt(0).toUpperCase() + s.slice(1);
}

export function approveRequest(id) {
  if (!confirm(t('approval.approveConfirm'))) return;
  api.approveApprovalRequest(id, '').then(function (r) {
    if (r.s === 200) loadPendingApprovals();
    else alert(t('schedule.saveFail'));
  }).catch(function (e) { alert(t('schedule.saveFail') + ': ' + e.message); });
}

export function rejectRequest(id) {
  const reason = prompt(t('approval.rejectReason'), '');
  if (reason === null) return;
  api.rejectApprovalRequest(id, reason || '').then(function (r) {
    if (r.s === 200) loadPendingApprovals();
    else alert(t('schedule.saveFail'));
  }).catch(function (e) { alert(t('schedule.saveFail') + ': ' + e.message); });
}

export function cancelRequest(id) {
  if (!confirm(t('approval.cancel') + '?')) return;
  api.cancelApprovalRequest(id).then(function (r) {
    if (r.s === 200) loadPendingApprovals();
    else alert(t('schedule.saveFail'));
  }).catch(function (e) { alert(t('schedule.saveFail') + ': ' + e.message); });
}

// ============================================================================
// 审批历史
// ============================================================================

export function loadApprovalHistoryList() {
  api.getApprovalRequests('').then(function (data) {
    renderApprovalHistoryList(data.requests || []);
  }).catch(function (e) { api.apiFail('approvalHistory', e); });
}

function renderApprovalHistoryList(list) {
  const el = document.getElementById('approvalHistoryList');
  if (!el) return;
  if (!list.length) {
    el.innerHTML = '<p class="muted">' + esc(t('approval.historyEmpty')) + '</p>';
    return;
  }
  let html = '<table class="table"><thead><tr>'
    + '<th>ID</th>'
    + '<th>' + esc(t('approval.flowTrigger')) + '</th>'
    + '<th>' + esc(t('approval.requestTarget')) + '</th>'
    + '<th>' + esc(t('approval.requestStatus')) + '</th>'
    + '<th>' + esc(t('approval.requestCreatedAt')) + '</th>'
    + '<th>' + esc(t('approval.timeline')) + '</th>'
    + '</tr></thead><tbody>';
  list.forEach(function (r) {
    html += '<tr>'
      + '<td><code>' + esc(r.id) + '</code></td>'
      + '<td>' + esc(r.triggerType) + '</td>'
      + '<td>' + esc(r.target || '-') + '</td>'
      + '<td>' + statusBadgeApproval(r.status) + '</td>'
      + '<td>' + esc(fmtTime(r.createdAt)) + '</td>'
      + '<td><button class="btn btn-sm" onclick="viewRequestHistory(\'' + escAttr(r.id) + '\')">' + esc(t('approval.timeline')) + '</button></td>'
      + '</tr>';
  });
  html += '</tbody></table>';
  el.innerHTML = html;
}

function statusBadgeApproval(status) {
  const map = {
    pending: 'badge-warning',
    approved: 'badge-success',
    rejected: 'badge-danger',
    timeout: 'badge-secondary',
    cancelled: 'badge-secondary',
  };
  const cls = map[status] || 'badge-secondary';
  return '<span class="badge ' + cls + '">' + esc(t('approval.status' + cap(status)) || status) + '</span>';
}

export function viewRequestHistory(id) {
  historyRequestID = id;
  api.getApprovalHistory(id).then(function (data) {
    renderHistoryTimeline(data);
  }).catch(function (e) { alert(t('approval.historyLoadFail') + ': ' + e.message); });
}

function renderHistoryTimeline(hist) {
  const timeline = (hist && hist.timeline) || [];
  let body = '<div class="timeline">';
  if (!timeline.length) {
    body += '<p class="muted">' + esc(t('approval.historyEmpty')) + '</p>';
  } else {
    timeline.forEach(function (e) {
      const actionLabel = t('approval.action' + cap(e.action)) || e.action;
      body += '<div class="timeline-item">'
        + '<div class="timeline-time">' + esc(fmtTime(e.timestamp)) + '</div>'
        + '<div class="timeline-content">'
        + '<b>' + esc(actionLabel) + '</b>'
        + (e.userID ? ' <span class="muted">by ' + esc(e.userID) + '</span>' : '')
        + (e.stepID ? ' <span class="muted">step: ' + esc(e.stepID) + '</span>' : '')
        + (e.comment ? '<p>' + esc(e.comment) + '</p>' : '')
        + '</div></div>';
    });
  }
  body += '</div>';
  showModal(t('approval.timeline') + ' - ' + historyRequestID, body, '<button class="btn" onclick="closeModal()">' + esc(t('approval.cancel')) + '</button>');
}