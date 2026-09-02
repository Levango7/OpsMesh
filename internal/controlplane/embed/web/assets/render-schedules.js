// render-schedules.js — 定时任务渲染（P2 补齐功能域）。

// 渲染子模块 — 定时任务（列表 / 创建 / 编辑 / 删除 / 启用禁用）。
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
// 定时任务渲染
// ============================================================================

// scheduleEnabledBadge 定时任务启用状态 badge。
function scheduleEnabledBadge(enabled) {
  if (enabled) return badge(t('common.enabled'), 'status-resolved');
  return badge(t('common.disabled'), 'status-closed');
}

// renderSchedulesTable 渲染定时任务列表表格。
// schedules: [{id, name, cron, taskType, params, enabled, lastRunAt, nextRunAt, createdAt}]
// handlers: { onEdit(id), onDelete(id), onToggle(id, enabled) }
export function renderSchedulesTable(container, schedules, handlers) {
  container.innerHTML = '';
  if (!schedules || schedules.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('schedules.name') }),
        el('th', { text: t('schedules.cron') }),
        el('th', { text: t('schedules.taskType') }),
        el('th', { text: t('common.status') }),
        el('th', { text: t('schedules.lastRunAt') }),
        el('th', { text: t('schedules.nextRunAt') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      schedules.map((s) => el('tr', null,
        el('td', { class: 'mono', text: s.id }),
        el('td', { class: 'cell-title', text: s.name || '-' }),
        el('td', { class: 'mono', text: s.cron || '-' }),
        el('td', { text: s.taskType || '-' }),
        el('td', null, scheduleEnabledBadge(s.enabled)),
        el('td', { class: 'mono', text: formatTime(s.lastRunAt) }),
        el('td', { class: 'mono', text: formatTime(s.nextRunAt) }),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn-icon', title: t('common.edit'), onclick: () => handlers.onEdit(s.id) }, iconEl('edit', 14)),
          el('button', { class: 'btn-icon', title: (s.enabled ? t('common.disable') : t('common.enable')), onclick: () => handlers.onToggle(s.id, !s.enabled) },
            s.enabled ? iconEl('toggle_on', 14) : iconEl('toggle_off', 14)
          ),
          el('button', { class: 'btn-icon btn-icon-danger', title: t('common.delete'), onclick: () => handlers.onDelete(s.id) }, iconEl('trash', 14))
        )
      ))
    )
  );
  container.appendChild(table);
}

// renderScheduleForm 渲染定时任务创建/编辑表单。
// schedule: 已有任务（编辑模式）或 null（创建模式）。
// handlers: { onSubmit(data), onCancel() }
export function renderScheduleForm(container, schedule, handlers) {
  container.innerHTML = '';
  const isEdit = !!schedule;
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const data = {
      name: form.elements.name.value.trim(),
      cron: form.elements.cron.value.trim(),
      taskType: form.elements.taskType.value,
      params: parseParams(form.elements.params.value),
      enabled: form.elements.enabled.checked,
    };
    handlers.onSubmit(data);
  } });
  form.appendChild(el('h3', { class: 'form-title', text: isEdit ? t('schedules.edit') : t('schedules.create') }));
  // 任务名称（必填）
  form.appendChild(fieldRow(t('schedules.name'), true,
    el('input', { type: 'text', name: 'name', required: 'true', value: schedule ? (schedule.name || '') : '', placeholder: t('schedules.namePlaceholder') })
  ));
  // cron 表达式（必填）
  form.appendChild(fieldRow(t('schedules.cron'), true,
    el('input', { type: 'text', name: 'cron', required: 'true', value: schedule ? (schedule.cron || '') : '', placeholder: t('schedules.cronPlaceholder') })
  ));
  // 任务类型
  form.appendChild(fieldRow(t('schedules.taskType'), true,
    el('select', { name: 'taskType' },
      ['exec', 'config', 'restart', 'inspect', 'batch'].map((tp) =>
        el('option', { value: tp, selected: schedule && schedule.taskType === tp ? 'selected' : undefined, text: tp })
      )
    )
  ));
  // 参数（JSON）
  form.appendChild(fieldRow(t('schedules.params'), false,
    el('textarea', { name: 'params', rows: '4', placeholder: '{"key":"value"}' },
      schedule ? formatParams(schedule.params) : '')
  ));
  // 启用
  form.appendChild(fieldRow(t('common.enabled'), false,
    el('input', { type: 'checkbox', name: 'enabled', checked: schedule ? (!!schedule.enabled) : true })
  ));
  // 操作按钮
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('check', 16), el('span', { text: t('common.save') })),
    el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => handlers.onCancel() }, el('span', { text: t('common.cancel') }))
  ));
  container.appendChild(form);
}

// formatParams 格式化参数（对象 JSON 化）。
function formatParams(p) {
  if (p == null) return '';
  if (typeof p === 'object') {
    try { return JSON.stringify(p); } catch (_) { return String(p); }
  }
  return String(p);
}

// parseParams 解析参数 JSON（解析失败保留原字符串）。
function parseParams(text) {
  const raw = (text || '').trim();
  if (!raw) return null;
  try { return JSON.parse(raw); } catch (_) { return raw; }
}