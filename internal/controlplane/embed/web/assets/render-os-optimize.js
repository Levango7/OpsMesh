// render-os-optimize.js — OS 优化渲染（P1 补齐功能域）。

// 渲染子模块 — OS 优化（模板列表 / 执行表单 / 执行结果）。
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
// OS 优化渲染
// ============================================================================

// riskLevelBadge 风险等级 badge。
function riskLevelBadge(level) {
  const s = String(level || '').toLowerCase();
  if (s === 'high') return badge(t('osOptimize.risk.high'), 'priority-urgent');
  if (s === 'medium') return badge(t('osOptimize.risk.medium'), 'priority-high');
  if (s === 'low') return badge(t('osOptimize.risk.low'), 'priority-low');
  return badge(level || '-', 'priority-low');
}

// renderOSTemplatesTable 渲染 OS 优化模板列表表格。
// handlers: { onExecute(id) }
export function renderOSTemplatesTable(container, templates, handlers) {
  container.innerHTML = '';
  if (!templates || templates.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('osOptimize.templateName') }),
        el('th', { text: t('common.description') }),
        el('th', { text: t('osOptimize.category') }),
        el('th', { text: t('osOptimize.riskLevel') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      templates.map((tp) => el('tr', null,
        el('td', { class: 'mono', text: tp.id }),
        el('td', { class: 'cell-title', text: tp.name || '-' }),
        el('td', { text: tp.description || '-' }),
        el('td', { text: tp.category || '-' }),
        el('td', null, riskLevelBadge(tp.riskLevel || tp.risk)),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn-icon', title: t('osOptimize.execute'), onclick: () => handlers.onExecute(tp.id) }, iconEl('play', 14))
        )
      ))
    )
  );
  container.appendChild(table);
}

// renderOSTemplateExecForm 渲染 OS 优化模板执行表单。
// template: 模板信息（含参数定义）。
// handlers: { onSubmit(data), onCancel() }
export function renderOSTemplateExecForm(container, templateID, template, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const data = {
      agentID: form.elements.agentID.value.trim(),
      params: parseParams(form.elements.params.value),
    };
    handlers.onSubmit(data);
  } });
  const name = template ? (template.name || templateID) : templateID;
  form.appendChild(el('h3', { class: 'form-title', text: t('osOptimize.execTitle') + ' · ' + name }));
  // 目标 agent（必填）
  form.appendChild(fieldRow(t('osOptimize.agentID'), true,
    el('input', { type: 'text', name: 'agentID', required: 'true', placeholder: 'agent-id' })
  ));
  // 参数（JSON）
  form.appendChild(fieldRow(t('osOptimize.params'), false,
    el('textarea', { name: 'params', rows: '4', placeholder: '{"key":"value"}' }, '')
  ));
  // 操作按钮
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('play', 16), el('span', { text: t('osOptimize.execute') })),
    el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => handlers.onCancel() }, el('span', { text: t('common.cancel') }))
  ));
  container.appendChild(form);
}

// parseParams 解析参数 JSON。
function parseParams(text) {
  const raw = (text || '').trim();
  if (!raw) return null;
  try { return JSON.parse(raw); } catch (_) { return raw; }
}

// renderOSExecResult 渲染执行结果。
// handlers: { onBack() }
export function renderOSExecResult(container, templateID, result, handlers) {
  container.innerHTML = '';
  const card = el('div', { class: 'detail-card' });
  card.appendChild(el('div', { class: 'detail-head' },
    el('button', { class: 'btn btn-ghost', onclick: () => handlers.onBack() }, iconEl('back', 16), el('span', { text: t('common.back') })),
    el('h3', { class: 'detail-title', text: t('osOptimize.resultTitle') + ' · ' + templateID })
  ));
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

// formatResultValue 格式化结果值。
function formatResultValue(v) {
  if (v == null) return '-';
  if (typeof v === 'object') {
    try { return JSON.stringify(v); } catch (_) { return String(v); }
  }
  return String(v);
}