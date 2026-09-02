// flow-approval.js — 审批流编排（P3 补齐功能域）。

// flow 子模块 — 审批流（流定义列表 + 审批请求列表 + approve/reject 操作）。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, $ } from './flow-state.js';

// ============================================================================
// 审批流
// ============================================================================

function approvalContent() { return $('approval-content'); }

// renderApprovalPanel 渲染审批流主面板（流定义 + 审批请求）。
function renderApprovalPanel(content) {
  content.innerHTML = '';
  // 子面板 1：审批流定义
  const flowsCard = render.el('div', { class: 'detail-card' });
  flowsCard.appendChild(render.el('h3', { class: 'detail-title', text: t('approval.flowsTitle') }));
  const flowsBody = render.el('div', { class: 'card-body' });
  render.renderApprovalFlowsTable(flowsBody, state.approval.flows, {
    onDelete: (id) => deleteApprovalFlow(id),
  });
  flowsCard.appendChild(flowsBody);
  content.appendChild(flowsCard);
  // 子面板 2：审批请求
  const reqCard = render.el('div', { class: 'detail-card' });
  reqCard.appendChild(render.el('h3', { class: 'detail-title', text: t('approval.requestsTitle') }));
  const reqBody = render.el('div', { class: 'card-body' });
  render.renderApprovalRequestsTable(reqBody, state.approval.requests, {
    onApprove: (id) => approveRequest(id),
    onReject: (id) => rejectRequest(id),
  });
  reqCard.appendChild(reqBody);
  content.appendChild(reqCard);
}

// loadApprovalFlows 加载审批流定义列表。
export async function loadApprovalFlows() {
  const content = approvalContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const data = await api.getApprovalFlows();
    const list = (data && data.flows) ? data.flows : (Array.isArray(data) ? data : []);
    state.approval.flows = list;
    renderApprovalPanel(content);
  } catch (err) {
    render.renderError(content, t('approval.flowsLoadFailed') + ': ' + err.message);
  }
}

// loadApprovalRequests 加载审批请求列表。
export async function loadApprovalRequests() {
  const content = approvalContent();
  if (!content) return;
  try {
    const data = await api.getApprovalRequests({ status: 'pending' });
    const list = (data && data.requests) ? data.requests : (Array.isArray(data) ? data : []);
    state.approval.requests = list;
    renderApprovalPanel(content);
  } catch (err) {
    render.renderToast(t('approval.requestsLoadFailed') + ': ' + err.message, 'error');
  }
}

// showApprovalFlowForm 打开创建审批流表单。
export function showApprovalFlowForm() {
  const content = approvalContent();
  if (!content) return;
  render.renderApprovalFlowForm(content, {
    onSubmit: async (data) => {
      if (!data.name) {
        render.renderToast(t('approval.flowNameRequired'), 'warn');
        return;
      }
      try {
        await api.createApprovalFlow(data);
        render.renderToast(t('approval.flowCreated'), 'success');
        loadApprovalFlows();
      } catch (err) {
        render.renderToast(t('approval.flowCreateFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => loadApprovalFlows(),
  });
}

// deleteApprovalFlow 删除审批流（确认后调用 API）。
export async function deleteApprovalFlow(id) {
  if (!window.confirm(t('approval.confirmDeleteFlow'))) return;
  try {
    await api.deleteApprovalFlow(id);
    render.renderToast(t('approval.flowDeleted'), 'success');
    loadApprovalFlows();
  } catch (err) {
    render.renderToast(t('approval.flowDeleteFailed') + ': ' + err.message, 'error');
  }
}

// approveRequest 批准审批请求。
export async function approveRequest(id) {
  try {
    await api.approveRequest(id);
    render.renderToast(t('approval.approved'), 'success');
    loadApprovalRequests();
  } catch (err) {
    render.renderToast(t('approval.approveFailed') + ': ' + err.message, 'error');
  }
}

// rejectRequest 驳回审批请求。
export async function rejectRequest(id) {
  if (!window.confirm(t('approval.confirmReject'))) return;
  try {
    await api.rejectRequest(id);
    render.renderToast(t('approval.rejected'), 'success');
    loadApprovalRequests();
  } catch (err) {
    render.renderToast(t('approval.rejectFailed') + ': ' + err.message, 'error');
  }
}

// loadApprovalAll 加载审批流全部数据（流定义 + 请求）。
export async function loadApprovalAll() {
  const content = approvalContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const [flowsData, reqData] = await Promise.all([
      api.getApprovalFlows(),
      api.getApprovalRequests({ status: 'pending' }),
    ]);
    state.approval.flows = (flowsData && flowsData.flows) ? flowsData.flows : (Array.isArray(flowsData) ? flowsData : []);
    state.approval.requests = (reqData && reqData.requests) ? reqData.requests : (Array.isArray(reqData) ? reqData : []);
    renderApprovalPanel(content);
  } catch (err) {
    render.renderError(content, t('approval.flowsLoadFailed') + ': ' + err.message);
  }
}

// refreshApprovalSubTab 刷新审批流页（重新加载流定义 + 请求）。
export function refreshApprovalSubTab() {
  loadApprovalAll();
}

// buildApprovalToolbar 构建审批流工具栏（创建流 + 刷新）。
export function buildApprovalToolbar() {
  const toolbar = $('approval-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  // 创建审批流按钮
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-primary', onclick: () => showApprovalFlowForm() },
      iconEl('plus', 16), render.el('span', { text: t('approval.createFlow') })
    )
  );
  // 刷新
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => loadApprovalAll() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}
