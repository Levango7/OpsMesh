// render-logs.js — 日志检索渲染（P1 补齐功能域）。

// 渲染子模块 — 日志检索（检索表单 + 结果列表）。
// 公共依赖：i18n（t）、icons（iconEl/iconHtml）、render-common（DOM/Badge/表单辅助）。

import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { el, formatTime, formatNumber, badge, renderEmpty, fieldRow } from './render-common.js';

// ============================================================================
// 日志检索渲染
// ============================================================================

// logLevelBadge 日志级别 badge。
export function logLevelBadge(level) {
  const s = String(level || '').toLowerCase();
  if (s === 'error' || s === 'fatal') return badge(t('logs.level.error'), 'priority-urgent');
  if (s === 'warn' || s === 'warning') return badge(t('logs.level.warn'), 'priority-high');
  if (s === 'info') return badge(t('logs.level.info'), 'status-in_progress');
  if (s === 'debug') return badge(t('logs.level.debug'), 'priority-low');
  if (s === 'trace') return badge(t('logs.level.trace'), 'priority-low');
  return badge(level || '-', 'status-in_progress');
}

// renderLogsSearchForm 渲染日志检索表单。
// filter: 当前过滤条件（query/level/from/to/limit）。
// handlers: { onSearch(data), onReset() }
export function renderLogsSearchForm(container, filter, handlers) {
  container.innerHTML = '';
  const f = filter || {};
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const data = {
      query: form.elements.query.value.trim(),
      level: form.elements.level.value,
      from: form.elements.from.value,
      to: form.elements.to.value,
      limit: form.elements.limit.value,
    };
    handlers.onSearch(data);
  } });
  form.appendChild(el('h3', { class: 'form-title', text: t('logs.searchTitle') }));
  // 关键词
  form.appendChild(fieldRow(t('logs.query'), false,
    el('input', { type: 'text', name: 'query', value: f.query || '', placeholder: t('logs.queryPlaceholder') })
  ));
  // 级别
  form.appendChild(fieldRow(t('logs.level.label'), false,
    el('select', { name: 'level' },
      el('option', { value: '', text: t('common.all') }),
      ['trace', 'debug', 'info', 'warn', 'error', 'fatal'].map((lv) =>
        el('option', { value: lv, selected: f.level === lv ? 'selected' : undefined, text: t('logs.level.' + lv) })
      )
    )
  ));
  // 起始时间
  form.appendChild(fieldRow(t('logs.from'), false,
    el('input', { type: 'datetime-local', name: 'from', value: f.from || '' })
  ));
  // 结束时间
  form.appendChild(fieldRow(t('logs.to'), false,
    el('input', { type: 'datetime-local', name: 'to', value: f.to || '' })
  ));
  // limit
  form.appendChild(fieldRow(t('logs.limit'), false,
    el('input', { type: 'number', name: 'limit', value: f.limit || '100', min: '1', max: '10000' })
  ));
  // 操作按钮
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('search', 16), el('span', { text: t('common.search') })),
    el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => handlers.onReset() }, el('span', { text: t('logs.reset') }))
  ));
  container.appendChild(form);
}

// renderLogsTable 渲染日志结果列表表格。
export function renderLogsTable(container, logs, total) {
  container.innerHTML = '';
  // 总数提示
  if (total != null) {
    container.appendChild(el('div', { class: 'list-meta', text: t('logs.total') + ': ' + formatNumber(total) }));
  }
  if (!logs || logs.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('logs.timestamp') }),
        el('th', { text: t('logs.level.label') }),
        el('th', { text: t('logs.source') }),
        el('th', { text: t('logs.message') })
      )
    ),
    el('tbody', null,
      logs.map((lg) => el('tr', null,
        el('td', { class: 'mono', text: formatTime(lg.timestamp || lg.time || lg.ts) }),
        el('td', null, logLevelBadge(lg.level)),
        el('td', { class: 'mono', text: lg.source || lg.service || '-' }),
        el('td', { class: 'cell-title', text: lg.message || lg.msg || '-' })
      ))
    )
  );
  container.appendChild(table);
}
