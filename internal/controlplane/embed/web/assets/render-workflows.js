// render-workflows.js — 作业编排渲染（P1 补齐功能域）。

// 渲染子模块 — 作业编排（工作流列表 / 创建 / 运行 / 查看执行状态）。
// 公共依赖：i18n（t）、icons（iconEl/iconHtml）、render-common（DOM/Badge/表单辅助）。

import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { el, formatTime, badge, renderEmpty, detailItem, fieldRow } from './render-common.js';

// ============================================================================
// 作业编排渲染
// ============================================================================

// workflowStatusBadge 工作流状态 badge。
function workflowStatusBadge(status) {
  const s = String(status || '').toLowerCase();
  if (s === 'succeeded' || s === 'success' || s === 'completed') return badge(t('workflows.status.succeeded'), 'status-resolved');
  if (s === 'failed' || s === 'error') return badge(t('workflows.status.failed'), 'priority-urgent');
  if (s === 'running' || s === 'in_progress') return badge(t('workflows.status.running'), 'status-in_progress');
  if (s === 'pending' || s === 'queued') return badge(t('workflows.status.pending'), 'priority-medium');
  return badge(status || '-', 'status-in_progress');
}

// stepStatusBadge 步骤状态 badge。
function stepStatusBadge(status) {
  const s = String(status || '').toLowerCase();
  if (s === 'succeeded' || s === 'success' || s === 'completed') return badge(t('workflows.step.succeeded'), 'status-resolved');
  if (s === 'failed' || s === 'error') return badge(t('workflows.step.failed'), 'priority-urgent');
  if (s === 'running' || s === 'in_progress') return badge(t('workflows.step.running'), 'status-in_progress');
  if (s === 'skipped') return badge(t('workflows.step.skipped'), 'status-closed');
  if (s === 'pending' || s === 'queued') return badge(t('workflows.step.pending'), 'priority-medium');
  return badge(status || '-', 'status-in_progress');
}

// renderWorkflowsTable 渲染工作流列表表格。
// handlers: { onRun(id), onStatus(id) }
export function renderWorkflowsTable(container, workflows, handlers) {
  container.innerHTML = '';
  if (!workflows || workflows.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('workflows.name') }),
        el('th', { text: t('common.status') }),
        el('th', { text: t('common.createdAt') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      workflows.map((w) => el('tr', null,
        el('td', { class: 'mono', text: w.id }),
        el('td', { class: 'cell-title', text: w.name || '-' }),
        el('td', null, workflowStatusBadge(w.status)),
        el('td', { class: 'mono', text: formatTime(w.createdAt) }),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn-icon', title: t('workflows.run'), onclick: () => handlers.onRun(w.id) }, iconEl('play', 14)),
          el('button', { class: 'btn-icon', title: t('workflows.viewStatus'), onclick: () => handlers.onStatus(w.id) }, iconEl('stats', 14))
        )
      ))
    )
  );
  container.appendChild(table);
}

// renderWorkflowForm 渲染创建工作流表单。
// handlers: { onSubmit(data), onCancel() }
export function renderWorkflowForm(container, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const data = {
      name: form.elements.name.value.trim(),
      steps: parseSteps(form.elements.steps.value),
    };
    handlers.onSubmit(data);
  } });
  form.appendChild(el('h3', { class: 'form-title', text: t('workflows.create') }));
  // 工作流名称（必填）
  form.appendChild(fieldRow(t('workflows.name'), true,
    el('input', { type: 'text', name: 'name', required: 'true', placeholder: t('workflows.namePlaceholder') })
  ));
  // 步骤（JSON 数组）
  form.appendChild(fieldRow(t('workflows.steps'), false,
    el('textarea', { name: 'steps', rows: '6', placeholder: '[{"name":"step1","action":"..."}]' }, '')
  ));
  // 操作按钮
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('check', 16), el('span', { text: t('common.save') })),
    el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => handlers.onCancel() }, el('span', { text: t('common.cancel') }))
  ));
  container.appendChild(form);
}

// parseSteps 解析步骤 JSON。
function parseSteps(text) {
  const raw = (text || '').trim();
  if (!raw) return null;
  try { return JSON.parse(raw); } catch (_) { return raw; }
}

// renderWorkflowStatus 渲染工作流执行状态（步骤进度）。
// handlers: { onBack(), onRefresh() }
export function renderWorkflowStatus(container, workflowID, status, handlers) {
  container.innerHTML = '';
  const card = el('div', { class: 'detail-card' });
  card.appendChild(el('div', { class: 'detail-head' },
    el('button', { class: 'btn btn-ghost', onclick: () => handlers.onBack() }, iconEl('back', 16), el('span', { text: t('common.back') })),
    el('h3', { class: 'detail-title', text: t('workflows.statusTitle') + ' · ' + workflowID }),
    el('div', { class: 'detail-actions' },
      el('button', { class: 'btn btn-secondary', onclick: () => handlers.onRefresh() }, iconEl('refresh', 14), el('span', { text: t('common.refresh') }))
    )
  ));
  const sObj = (status && typeof status === 'object') ? status : {};
  // 整体状态
  card.appendChild(el('div', { class: 'detail-grid' },
    detailItem(t('common.status'), workflowStatusBadge(sObj.status)),
    detailItem(t('workflows.taskID'), sObj.taskID || sObj.taskId || '-', true)
  ));
  // 步骤列表
  const steps = sObj.steps || [];
  if (steps.length) {
    card.appendChild(el('div', { class: 'detail-section' },
      el('h4', { text: t('workflows.stepsTitle') }),
      el('table', { class: 'data-table' },
        el('thead', null,
          el('tr', null,
            el('th', { text: t('workflows.stepName') }),
            el('th', { text: t('common.status') }),
            el('th', { text: t('workflows.startedAt') }),
            el('th', { text: t('workflows.finishedAt') })
          )
        ),
        el('tbody', null,
          steps.map((st) => el('tr', null,
            el('td', { class: 'cell-title', text: st.name || '-' }),
            el('td', null, stepStatusBadge(st.status)),
            el('td', { class: 'mono', text: formatTime(st.startedAt) }),
            el('td', { class: 'mono', text: formatTime(st.finishedAt) })
          ))
        )
      )
    ));
  } else {
    card.appendChild(el('div', { class: 'state state-empty', text: t('common.empty') }));
  }
  container.appendChild(card);
}
