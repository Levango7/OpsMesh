// render-alert-rules.js — 告警规则管理渲染（P0 补齐功能域）。

// 渲染子模块 — 告警规则管理（规则 CRUD / 多条件引擎 / 静默规则）。
// 公共依赖：i18n（t）、icons（iconEl/iconHtml）、render-common（DOM/Badge/表单辅助）。

import { t } from './i18n.js';
import { iconEl, iconHtml } from './icons.js';
import {
  el, formatTime, formatNumber, badge,
  renderLoading, renderError, renderEmpty, renderToast,
  statusBadge, priorityBadge, categoryBadge, sloStatusBadge,
  detailItem, fieldRow,
} from './render-common.js';
import { alertSeverityBadge, alertStateBadge } from './render-alerts.js';

// ============================================================================
// 告警规则管理渲染
// ============================================================================

// ruleEnabledBadge 规则启用状态 badge。
function ruleEnabledBadge(enabled) {
  if (enabled === true || enabled === 'true' || enabled === 1) return badge(t('common.enabled'), 'status-resolved');
  return badge(t('common.disabled'), 'status-closed');
}

// --- 告警规则 ---

// renderAlertRulesTable 渲染告警规则列表表格。
// handlers: { onEdit(rule), onDelete(id) }
export function renderAlertRulesTable(container, rules, handlers) {
  container.innerHTML = '';
  if (!rules || rules.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('alertRules.ruleName') }),
        el('th', { text: t('alertRules.severity') }),
        el('th', { text: t('alertRules.enabled') }),
        el('th', { text: t('common.updatedAt') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      rules.map((r) => el('tr', null,
        el('td', { class: 'mono', text: r.id }),
        el('td', { class: 'cell-title', text: r.name || '-' }),
        el('td', null, alertSeverityBadge(r.severity)),
        el('td', null, ruleEnabledBadge(r.enabled)),
        el('td', { class: 'mono', text: formatTime(r.updatedAt || r.createdAt) }),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn-icon', title: t('common.edit'), onclick: () => handlers.onEdit(r) }, iconEl('edit', 14)),
          el('button', { class: 'btn-icon btn-icon-danger', title: t('common.delete'), onclick: () => handlers.onDelete(r.id) }, iconEl('trash', 14))
        )
      ))
    )
  );
  container.appendChild(table);
}

// renderAlertRuleForm 渲染告警规则创建/编辑表单。
// rule=null 表示创建；否则编辑（预填）。
// handlers: { onSubmit(data), onCancel() }
export function renderAlertRuleForm(container, rule, handlers) {
  container.innerHTML = '';
  const isEdit = !!rule;
  const form = el('form', { class: 'form-card', onsubmit: (e) => { e.preventDefault(); handlers.onSubmit(collectAlertRuleForm(form)); } });
  form.appendChild(el('h3', { class: 'form-title', text: isEdit ? t('alertRules.edit') : t('alertRules.create') }));
  // 规则名称（必填）
  form.appendChild(fieldRow(t('alertRules.ruleName'), true,
    el('input', { type: 'text', name: 'name', required: 'true', value: rule ? rule.name : '', placeholder: t('alertRules.nameRequired') })
  ));
  // 严重级别
  form.appendChild(fieldRow(t('alertRules.severity'), false,
    el('select', { name: 'severity' },
      ['critical', 'high', 'medium', 'low'].map((s) =>
        el('option', { value: s, selected: (rule ? rule.severity : 'medium') === s ? 'selected' : undefined, text: t('alerts.severity.' + s) })
      )
    )
  ));
  // 指标表达式（PromQL）
  form.appendChild(fieldRow(t('alertRules.expr'), true,
    el('input', { type: 'text', name: 'expr', required: 'true', value: rule ? rule.expr || rule.expression || '' : '', placeholder: 'rate(http_requests_total[5m]) > 0.1' })
  ));
  // 持续时长
  form.appendChild(fieldRow(t('alertRules.forDuration'), false,
    el('input', { type: 'text', name: 'for', value: rule ? rule.for || rule.forDuration || '' : '5m', placeholder: '5m / 1h' })
  ));
  // 描述
  form.appendChild(fieldRow(t('common.description'), false,
    el('textarea', { name: 'description', rows: '2' }, rule ? rule.description || '' : '')
  ));
  // 启用
  form.appendChild(fieldRow(t('alertRules.enabled'), false,
    el('select', { name: 'enabled' },
      el('option', { value: 'true', selected: (!rule || rule.enabled) ? 'selected' : undefined, text: t('common.enabled') }),
      el('option', { value: 'false', selected: (rule && !rule.enabled) ? 'selected' : undefined, text: t('common.disabled') })
    )
  ));
  // 操作按钮
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('check', 16), el('span', { text: t('common.save') })),
    el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => handlers.onCancel() }, el('span', { text: t('common.cancel') }))
  ));
  container.appendChild(form);
}

// collectAlertRuleForm 从表单收集数据。
function collectAlertRuleForm(form) {
  const get = (name) => (form.elements[name] && form.elements[name].value) || '';
  return {
    name: get('name'),
    severity: get('severity'),
    expr: get('expr'),
    for: get('for'),
    description: get('description'),
    enabled: get('enabled') !== 'false',
  };
}

// --- 多条件引擎规则 ---

// renderAlertEngineTable 渲染多条件引擎规则列表表格。
// handlers: { onEdit(rule), onDelete(id) }
export function renderAlertEngineTable(container, rules, handlers) {
  container.innerHTML = '';
  if (!rules || rules.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('alertRules.engineName') }),
        el('th', { text: t('alertRules.conditions') }),
        el('th', { text: t('alertRules.enabled') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      rules.map((r) => el('tr', null,
        el('td', { class: 'mono', text: r.id }),
        el('td', { class: 'cell-title', text: r.name || '-' }),
        el('td', { class: 'mono', text: String((r.conditions && r.conditions.length) || r.conditionCount || 0) }),
        el('td', null, ruleEnabledBadge(r.enabled)),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn-icon', title: t('common.edit'), onclick: () => handlers.onEdit(r) }, iconEl('edit', 14)),
          el('button', { class: 'btn-icon btn-icon-danger', title: t('common.delete'), onclick: () => handlers.onDelete(r.id) }, iconEl('trash', 14))
        )
      ))
    )
  );
  container.appendChild(table);
}

// renderAlertEngineForm 渲染多条件引擎规则创建/编辑表单。
// rule=null 表示创建；否则编辑（预填）。
// handlers: { onSubmit(data), onCancel() }
export function renderAlertEngineForm(container, rule, handlers) {
  container.innerHTML = '';
  const isEdit = !!rule;
  const form = el('form', { class: 'form-card', onsubmit: (e) => { e.preventDefault(); handlers.onSubmit(collectEngineForm(form)); } });
  form.appendChild(el('h3', { class: 'form-title', text: isEdit ? t('alertRules.engineEdit') : t('alertRules.engineCreate') }));
  // 引擎规则名称（必填）
  form.appendChild(fieldRow(t('alertRules.engineName'), true,
    el('input', { type: 'text', name: 'name', required: 'true', value: rule ? rule.name : '', placeholder: t('alertRules.nameRequired') })
  ));
  // 条件列表（JSON 数组）
  form.appendChild(fieldRow(t('alertRules.conditions'), false,
    el('textarea', { name: 'conditions', rows: '5', placeholder: '[{"metric":"cpu","op":">","value":90},{"metric":"mem","op":">","value":80}]' },
      rule && rule.conditions ? JSON.stringify(rule.conditions, null, 2) : '')
  ));
  // 逻辑组合（AND/OR）
  form.appendChild(fieldRow(t('alertRules.logic'), false,
    el('select', { name: 'logic' },
      ['and', 'or'].map((l) =>
        el('option', { value: l, selected: (rule ? rule.logic : 'and') === l ? 'selected' : undefined, text: l.toUpperCase() })
      )
    )
  ));
  // 启用
  form.appendChild(fieldRow(t('alertRules.enabled'), false,
    el('select', { name: 'enabled' },
      el('option', { value: 'true', selected: (!rule || rule.enabled) ? 'selected' : undefined, text: t('common.enabled') }),
      el('option', { value: 'false', selected: (rule && !rule.enabled) ? 'selected' : undefined, text: t('common.disabled') })
    )
  ));
  // 操作按钮
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('check', 16), el('span', { text: t('common.save') })),
    el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => handlers.onCancel() }, el('span', { text: t('common.cancel') }))
  ));
  container.appendChild(form);
}

// collectEngineForm 从表单收集数据。
function collectEngineForm(form) {
  const get = (name) => (form.elements[name] && form.elements[name].value) || '';
  const rawConditions = get('conditions').trim();
  let conditions = [];
  if (rawConditions) {
    try { conditions = JSON.parse(rawConditions); } catch (_) { conditions = []; }
  }
  return {
    name: get('name'),
    conditions: conditions,
    logic: get('logic'),
    enabled: get('enabled') !== 'false',
  };
}

// --- 静默规则 ---

// renderAlertSilencesTable 渲染静默规则列表表格。
// handlers: { onDelete(id) }
export function renderAlertSilencesTable(container, silences, handlers) {
  container.innerHTML = '';
  if (!silences || silences.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('alertRules.silenceMatch') }),
        el('th', { text: t('alertRules.silenceStart') }),
        el('th', { text: t('alertRules.silenceEnd') }),
        el('th', { text: t('alertRules.silenceCreatedBy') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      silences.map((s) => el('tr', null,
        el('td', { class: 'mono', text: s.id }),
        el('td', { class: 'cell-title', text: s.matchers ? JSON.stringify(s.matchers) : (s.match || '-') }),
        el('td', { class: 'mono', text: formatTime(s.startsAt || s.start) }),
        el('td', { class: 'mono', text: formatTime(s.endsAt || s.end) }),
        el('td', { text: s.createdBy || '-' }),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn-icon btn-icon-danger', title: t('common.delete'), onclick: () => handlers.onDelete(s.id) }, iconEl('trash', 14))
        )
      ))
    )
  );
  container.appendChild(table);
}

// renderAlertSilenceCreateForm 渲染静默规则创建表单。
// handlers: { onSubmit(data), onCancel() }
export function renderAlertSilenceCreateForm(container, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => { e.preventDefault(); handlers.onSubmit(collectSilenceForm(form)); } });
  form.appendChild(el('h3', { class: 'form-title', text: t('alertRules.silenceCreate') }));
  // 匹配标签（JSON 数组）
  form.appendChild(fieldRow(t('alertRules.silenceMatchers'), true,
    el('textarea', { name: 'matchers', rows: '4', required: 'true', placeholder: '[{"name":"alertname","value":"HighCPU","isRegex":false}]' }, '')
  ));
  // 起始时间
  form.appendChild(fieldRow(t('alertRules.silenceStart'), false,
    el('input', { type: 'text', name: 'startsAt', placeholder: 'YYYY-MM-DD HH:mm（留空表示立即）' }, '')
  ));
  // 结束时间（必填）
  form.appendChild(fieldRow(t('alertRules.silenceEnd'), true,
    el('input', { type: 'text', name: 'endsAt', required: 'true', placeholder: 'YYYY-MM-DD HH:mm' }, '')
  ));
  // 创建人
  form.appendChild(fieldRow(t('alertRules.silenceCreatedBy'), false,
    el('input', { type: 'text', name: 'createdBy', placeholder: t('alertRules.silenceCreatedBy') }, '')
  ));
  // 原因
  form.appendChild(fieldRow(t('common.description'), false,
    el('textarea', { name: 'reason', rows: '2' }, '')
  ));
  // 操作按钮
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('check', 16), el('span', { text: t('common.save') })),
    el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => handlers.onCancel() }, el('span', { text: t('common.cancel') }))
  ));
  container.appendChild(form);
}

// collectSilenceForm 从表单收集数据。
function collectSilenceForm(form) {
  const get = (name) => (form.elements[name] && form.elements[name].value) || '';
  const rawMatchers = get('matchers').trim();
  let matchers = [];
  if (rawMatchers) {
    try { matchers = JSON.parse(rawMatchers); } catch (_) { matchers = []; }
  }
  return {
    matchers: matchers,
    startsAt: get('startsAt') || undefined,
    endsAt: get('endsAt'),
    createdBy: get('createdBy'),
    reason: get('reason'),
  };
}