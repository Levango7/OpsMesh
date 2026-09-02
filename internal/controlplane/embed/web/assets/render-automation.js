// render-automation.js — 自动化闭环渲染（由 render.js 拆分）。

// 渲染子模块 — 由 render.js 拆分而来。
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
// Phase 4：自动化闭环渲染
// ============================================================================

// automationStatusBadge 自动化规则状态 badge。
function automationStatusBadge(status) {
  const s = String(status || '').toLowerCase();
  if (s === 'enabled' || s === 'active') return badge(t('automation.enabled'), 'badge-status-resolved');
  if (s === 'disabled' || s === 'inactive') return badge(t('automation.disabled'), 'badge-status-closed');
  return badge(status || '-', 'badge-status-in_progress');
}

// renderAutomationRulesTable 渲染自动化规则列表表格。
// handlers: { onEdit(rule), onEnable(rule), onDisable(rule), onTest(rule), onDelete(rule) }
export function renderAutomationRulesTable(container, rules, handlers) {
  container.innerHTML = '';
  if (!rules || !rules.length) { renderEmpty(container, t('automation.noRules')); return; }
  container.appendChild(el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('automation.ruleName') }),
        el('th', { text: t('automation.trigger') }),
        el('th', { text: t('automation.action') }),
        el('th', { text: t('automation.status') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      rules.map((r) => {
        const enabled = String(r.status || '').toLowerCase() === 'enabled' || String(r.status || '').toLowerCase() === 'active';
        return el('tr', null,
          el('td', { class: 'cell-title', text: r.name || r.id || '-' }),
          el('td', { class: 'mono', text: r.trigger || '-' }),
          el('td', { class: 'mono', text: r.action || '-' }),
          el('td', null, automationStatusBadge(r.status)),
          el('td', { class: 'td-actions' },
            el('button', { class: 'btn btn-ghost', title: t('automation.edit'), onclick: () => handlers.onEdit && handlers.onEdit(r) },
              iconEl('edit', 14)
            ),
            enabled
              ? el('button', { class: 'btn btn-ghost', title: t('automation.disable'), onclick: () => handlers.onDisable && handlers.onDisable(r) },
                  iconEl('disable', 14)
                )
              : el('button', { class: 'btn btn-ghost', title: t('automation.enable'), onclick: () => handlers.onEnable && handlers.onEnable(r) },
                  iconEl('enable', 14)
                ),
            el('button', { class: 'btn btn-ghost', title: t('automation.test'), onclick: () => handlers.onTest && handlers.onTest(r) },
              iconEl('test', 14)
            ),
            el('button', { class: 'btn btn-ghost btn-icon-danger', title: t('automation.delete'), onclick: () => handlers.onDelete && handlers.onDelete(r) },
              iconEl('trash', 14)
            )
          )
        );
      })
    )
  ));
}

// renderAutomationRuleForm 渲染创建/编辑自动化规则表单。
// rule: 编辑时传入现有规则，创建时传 null；handlers: { onSubmit(data) }
export function renderAutomationRuleForm(container, rule, handlers) {
  container.innerHTML = '';
  const isEdit = !!rule;
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const data = {
      name: form.elements.name.value.trim(),
      description: form.elements.description.value.trim(),
      trigger: form.elements.trigger.value.trim(),
      condition: form.elements.condition.value.trim(),
      action: form.elements.action.value.trim(),
      params: form.elements.params.value.trim(),
    };
    handlers.onSubmit && handlers.onSubmit(data);
  } });
  form.appendChild(el('h3', { class: 'form-title', text: isEdit ? t('automation.editRule') : t('automation.createRule') }));
  form.appendChild(fieldRow(t('automation.ruleName'), true,
    el('input', { name: 'name', type: 'text', required: 'true', value: (rule && rule.name) || '', placeholder: 'high-cpu-scale-up' })
  ));
  form.appendChild(fieldRow(t('automation.ruleDesc'), false,
    el('input', { name: 'description', type: 'text', value: (rule && rule.description) || '', placeholder: 'CPU > 80% 自动扩容' })
  ));
  form.appendChild(fieldRow(t('automation.trigger'), true,
    el('input', { name: 'trigger', type: 'text', required: 'true', value: (rule && rule.trigger) || '', placeholder: t('automation.triggerPlaceholder') })
  ));
  form.appendChild(fieldRow(t('automation.condition'), false,
    el('input', { name: 'condition', type: 'text', value: (rule && rule.condition) || '', placeholder: t('automation.conditionPlaceholder') })
  ));
  form.appendChild(fieldRow(t('automation.action'), true,
    el('input', { name: 'action', type: 'text', required: 'true', value: (rule && rule.action) || '', placeholder: t('automation.actionPlaceholder') })
  ));
  form.appendChild(fieldRow(t('automation.params'), false,
    el('input', { name: 'params', type: 'text', value: (rule && rule.params) || '', placeholder: t('automation.paramsPlaceholder') })
  ));
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' },
      iconEl('check', 16), el('span', { text: isEdit ? t('automation.editRule') : t('automation.createRule') })
    )
  ));
  container.appendChild(form);
}

// renderAutomationExecutionsTable 渲染自动化执行历史表格。
// handlers: { onDetail(exec) }
export function renderAutomationExecutionsTable(container, execs, handlers) {
  container.innerHTML = '';
  if (!execs || !execs.length) { renderEmpty(container, t('automation.noExecutions')); return; }
  container.appendChild(el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('automation.execId') }),
        el('th', { text: t('automation.execRule') }),
        el('th', { text: t('automation.execStatus') }),
        el('th', { text: t('automation.execTime') }),
        el('th', { text: t('automation.execDuration') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      execs.map((e) => {
        const s = String(e.status || '').toLowerCase();
        const statusCls = s === 'success' || s === 'succeeded' ? 'badge-status-resolved'
          : s === 'failed' || s === 'error' ? 'badge-priority-urgent'
          : s === 'running' ? 'badge-status-in_progress'
          : 'badge-status-closed';
        return el('tr', null,
          el('td', { class: 'cell-title mono', text: e.id || e.executionID || '-' }),
          el('td', { text: e.ruleName || e.rule || '-' }),
          el('td', null, badge(e.status || '-', statusCls)),
          el('td', { text: formatTime(e.createdAt || e.time || e.startedAt) }),
          el('td', { text: e.duration != null ? (e.duration + 'ms') : '-' }),
          el('td', { class: 'td-actions' },
            el('button', { class: 'btn btn-ghost', title: t('automation.execOutput'), onclick: () => handlers.onDetail && handlers.onDetail(e) },
              iconEl('search', 14)
            )
          )
        );
      })
    )
  ));
}

// renderAutomationExecutionDetail 渲染自动化执行详情。
export function renderAutomationExecutionDetail(container, exec) {
  container.innerHTML = '';
  if (!exec) { renderEmpty(container); return; }
  const card = el('div', { class: 'content' });
  card.appendChild(el('h3', { class: 'form-title', text: t('automation.execOutput') }));
  card.appendChild(el('div', { class: 'form-row' },
    el('label', { class: 'form-label', text: t('automation.execId') }),
    el('div', { class: 'form-control mono', text: exec.id || exec.executionID || '-' })
  ));
  card.appendChild(el('div', { class: 'form-row' },
    el('label', { class: 'form-label', text: t('automation.execRule') }),
    el('div', { class: 'form-control', text: exec.ruleName || exec.rule || '-' })
  ));
  card.appendChild(el('div', { class: 'form-row' },
    el('label', { class: 'form-label', text: t('automation.execStatus') }),
    el('div', { class: 'form-control' }, badge(exec.status || '-', 'badge-status-in_progress'))
  ));
  if (exec.output != null) {
    card.appendChild(el('div', { class: 'form-row' },
      el('label', { class: 'form-label', text: t('automation.execOutput') }),
      el('pre', { class: 'form-control', style: { whiteSpace: 'pre-wrap', fontFamily: 'monospace', fontSize: '.85rem' }, text: String(exec.output) })
    ));
  }
  if (exec.error != null && exec.error !== '') {
    card.appendChild(el('div', { class: 'form-row' },
      el('label', { class: 'form-label', text: 'Error' }),
      el('pre', { class: 'form-control', style: { whiteSpace: 'pre-wrap', fontFamily: 'monospace', fontSize: '.85rem', color: 'var(--danger, #c0392b)' }, text: String(exec.error) })
    ));
  }
  container.appendChild(card);
}

