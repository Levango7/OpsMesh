// render-tasks.js — 任务执行渲染（P0 补齐功能域）。

// 渲染子模块 — 任务执行（任务列表 / 创建 / 取消 / 结果查看）。
// 公共依赖：i18n（t）、icons（iconEl/iconHtml）、render-common（DOM/Badge/表单辅助）。

import { t } from './i18n.js';
import { iconEl, iconHtml } from './icons.js';
import {
  el, formatTime, formatNumber, badge,
  renderLoading, renderError, renderEmpty, renderToast,
  statusBadge, priorityBadge, categoryBadge, sloStatusBadge,
  detailItem, fieldRow,
} from './render-common.js';

// ============================================================================
// 任务执行渲染
// ============================================================================

// taskStatusBadge 任务状态 badge。
function taskStatusBadge(status) {
  const s = String(status || '').toLowerCase();
  if (s === 'running' || s === 'in_progress') return badge(t('tasks.status.running'), 'status-in_progress');
  if (s === 'succeeded' || s === 'success' || s === 'completed') return badge(t('tasks.status.succeeded'), 'status-resolved');
  if (s === 'failed' || s === 'error') return badge(t('tasks.status.failed'), 'priority-urgent');
  if (s === 'cancelled' || s === 'canceled') return badge(t('tasks.status.cancelled'), 'status-closed');
  if (s === 'pending' || s === 'queued') return badge(t('tasks.status.pending'), 'priority-medium');
  return badge(status || '-', 'status-in_progress');
}

// renderTasksTable 渲染任务列表表格。
// handlers: { onResult(id), onCancel(id) }
export function renderTasksTable(container, tasks, handlers) {
  container.innerHTML = '';
  if (!tasks || tasks.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('tasks.taskName') }),
        el('th', { text: t('tasks.taskType') }),
        el('th', { text: t('common.status') }),
        el('th', { text: t('tasks.device') }),
        el('th', { text: t('common.createdAt') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      tasks.map((tk) => {
        const cancellable = ['running', 'in_progress', 'pending', 'queued'].indexOf(String(tk.status || '').toLowerCase()) !== -1;
        return el('tr', null,
          el('td', { class: 'mono', text: tk.id }),
          el('td', { class: 'cell-title', text: tk.name || tk.title || '-' }),
          el('td', { text: tk.type || tk.taskType || '-' }),
          el('td', null, taskStatusBadge(tk.status)),
          el('td', { class: 'mono', text: tk.deviceID || tk.deviceId || '-' }),
          el('td', { class: 'mono', text: formatTime(tk.createdAt) }),
          el('td', { class: 'td-actions' },
            el('button', { class: 'btn-icon', title: t('tasks.viewResult'), onclick: () => handlers.onResult(tk.id) }, iconEl('stats', 14)),
            cancellable
              ? el('button', { class: 'btn-icon btn-icon-danger', title: t('tasks.cancel'), onclick: () => handlers.onCancel(tk.id) }, iconEl('close', 14))
              : null
          )
        );
      })
    )
  );
  container.appendChild(table);
}

// renderTaskForm 渲染任务创建表单。
// handlers: { onSubmit(data), onCancel() }
export function renderTaskForm(container, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => { e.preventDefault(); handlers.onSubmit(collectTaskForm(form)); } });
  form.appendChild(el('h3', { class: 'form-title', text: t('tasks.create') }));
  // 任务名称（必填）
  form.appendChild(fieldRow(t('tasks.taskName'), true,
    el('input', { type: 'text', name: 'name', required: 'true', placeholder: t('tasks.nameRequired') })
  ));
  // 任务类型
  form.appendChild(fieldRow(t('tasks.taskType'), true,
    el('select', { name: 'type' },
      ['exec', 'script', 'config', 'inspect', 'restart'].map((tp) =>
        el('option', { value: tp, text: t('tasks.type.' + tp) })
      )
    )
  ));
  // 目标设备（必填）
  form.appendChild(fieldRow(t('tasks.device'), true,
    el('input', { type: 'text', name: 'deviceID', required: 'true', placeholder: 'device-id' })
  ));
  // 负载（JSON）
  form.appendChild(fieldRow(t('tasks.payload'), false,
    el('textarea', { name: 'payload', rows: '4', placeholder: '{"key":"value"}' }, '')
  ));
  // 优先级
  form.appendChild(fieldRow(t('common.priority'), false,
    el('select', { name: 'priority' },
      ['low', 'medium', 'high', 'urgent'].map((p) =>
        el('option', { value: p, selected: p === 'medium' ? 'selected' : undefined, text: t('ticket.priority.' + p) })
      )
    )
  ));
  // 操作按钮
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('check', 16), el('span', { text: t('common.save') })),
    el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => handlers.onCancel() }, el('span', { text: t('common.cancel') }))
  ));
  container.appendChild(form);
}

// collectTaskForm 从表单收集数据。
function collectTaskForm(form) {
  const get = (name) => (form.elements[name] && form.elements[name].value) || '';
  const rawPayload = get('payload').trim();
  let payload = null;
  if (rawPayload) {
    try { payload = JSON.parse(rawPayload); } catch (_) { payload = rawPayload; }
  }
  return {
    name: get('name'),
    type: get('type'),
    deviceID: get('deviceID'),
    payload: payload,
    priority: get('priority'),
  };
}

// renderTaskResult 渲染任务结果。
// handlers: { onBack() }
export function renderTaskResult(container, taskID, result, handlers) {
  container.innerHTML = '';
  const card = el('div', { class: 'detail-card' });
  card.appendChild(el('div', { class: 'detail-head' },
    el('button', { class: 'btn btn-ghost', onclick: () => handlers.onBack() }, iconEl('back', 16), el('span', { text: t('common.back') })),
    el('h3', { class: 'detail-title', text: t('tasks.resultTitle') + ' · ' + taskID })
  ));
  // 结果展示：优先结构化字段，否则 JSON 化
  const rObj = (result && typeof result === 'object') ? result : { value: result };
  const rKeys = Object.keys(rObj);
  if (rKeys.length) {
    card.appendChild(el('div', { class: 'detail-grid' },
      rKeys.map((k) => detailItem(k, formatResultValue(rObj[k]), true))
    ));
  } else {
    card.appendChild(el('div', { class: 'state state-empty', text: t('common.empty') }));
  }
  container.appendChild(card);
}

// formatResultValue 格式化结果值（对象/数组 JSON 化）。
function formatResultValue(v) {
  if (v == null) return '-';
  if (typeof v === 'object') {
    try { return JSON.stringify(v); } catch (_) { return String(v); }
  }
  return String(v);
}