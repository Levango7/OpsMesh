// render-approval.js — 审批流渲染（P3 补齐功能域）。

// 渲染子模块 — 审批流（流定义列表 + 审批请求列表 + approve/reject 操作）。
// 公共依赖：i18n（t）、icons（iconEl/iconHtml）、render-common（DOM/Badge/表单辅助）。

import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { el, formatTime, formatNumber, badge, renderEmpty, fieldRow } from './render-common.js';

// ============================================================================
// 审批流渲染
// ============================================================================

// approvalRequestStatusBadge 审批请求状态 badge。
function approvalRequestStatusBadge(status) {
  const s = String(status || '').toLowerCase();
  if (s === 'approved') return badge(t('approval.status.approved'), 'status-resolved');
  if (s === 'rejected') return badge(t('approval.status.rejected'), 'status-closed');
  if (s === 'pending') return badge(t('approval.status.pending'), 'status-in_progress');
  return badge(status || '-', 'status-in_progress');
}

// renderApprovalFlowsTable 渲染审批流定义列表表格。
// flows: [{id, name, description, steps, createdAt}]
// handlers: { onDelete(id) }
export function renderApprovalFlowsTable(container, flows, handlers) {
  container.innerHTML = '';
  if (!flows || flows.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('approval.flowName') }),
        el('th', { text: t('common.description') }),
        el('th', { text: t('approval.steps') }),
        el('th', { text: t('common.createdAt') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      flows.map((f) => el('tr', null,
        el('td', { class: 'mono', text: f.id }),
        el('td', { class: 'cell-title', text: f.name || '-' }),
        el('td', { text: f.description || '-' }),
        el('td', { text: String(formatNumber(f.steps || 0)) }),
        el('td', { class: 'mono', text: formatTime(f.createdAt) }),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn-icon btn-icon-danger', title: t('common.delete'), onclick: () => handlers.onDelete(f.id) }, iconEl('trash', 14))
        )
      ))
    )
  );
  container.appendChild(table);
}

// renderApprovalFlowForm 渲染创建审批流表单。
// handlers: { onSubmit(data), onCancel() }
export function renderApprovalFlowForm(container, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const data = {
      name: form.elements.name.value.trim(),
      description: form.elements.description.value.trim(),
      steps: parseSteps(form.elements.steps.value),
    };
    handlers.onSubmit(data);
  } });
  form.appendChild(el('h3', { class: 'form-title', text: t('approval.createFlow') }));
  // 流名称（必填）
  form.appendChild(fieldRow(t('approval.flowName'), true,
    el('input', { type: 'text', name: 'name', required: 'true', placeholder: t('approval.flowNamePlaceholder') })
  ));
  // 描述
  form.appendChild(fieldRow(t('common.description'), false,
    el('input', { type: 'text', name: 'description', placeholder: t('approval.descriptionPlaceholder') })
  ));
  // 步骤数
  form.appendChild(fieldRow(t('approval.steps'), false,
    el('input', { type: 'number', name: 'steps', min: '1', value: '1', placeholder: t('approval.stepsPlaceholder') })
  ));
  // 操作按钮
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('check', 16), el('span', { text: t('common.save') })),
    el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => handlers.onCancel() }, el('span', { text: t('common.cancel') }))
  ));
  container.appendChild(form);
}

// renderApprovalRequestsTable 渲染审批请求列表表格。
// requests: [{id, flowID, title, requester, status, createdAt, updatedAt}]
// handlers: { onApprove(id), onReject(id) }
export function renderApprovalRequestsTable(container, requests, handlers) {
  container.innerHTML = '';
  if (!requests || requests.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('approval.requestTitle') }),
        el('th', { text: t('approval.flowID') }),
        el('th', { text: t('approval.requester') }),
        el('th', { text: t('common.status') }),
        el('th', { text: t('common.createdAt') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      requests.map((r) => el('tr', null,
        el('td', { class: 'mono', text: r.id }),
        el('td', { class: 'cell-title', text: r.title || '-' }),
        el('td', { class: 'mono', text: r.flowID || '-' }),
        el('td', { text: r.requester || '-' }),
        el('td', null, approvalRequestStatusBadge(r.status)),
        el('td', { class: 'mono', text: formatTime(r.createdAt) }),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn-icon', title: t('approval.approve'), onclick: () => handlers.onApprove(r.id) }, iconEl('check', 14)),
          el('button', { class: 'btn-icon btn-icon-danger', title: t('approval.reject'), onclick: () => handlers.onReject(r.id) }, iconEl('close', 14))
        )
      ))
    )
  );
  container.appendChild(table);
}

// parseSteps 解析步骤数（解析失败回退到 1）。
function parseSteps(text) {
  const n = parseInt(text, 10);
  if (isNaN(n) || n < 1) return 1;
  return n;
}
