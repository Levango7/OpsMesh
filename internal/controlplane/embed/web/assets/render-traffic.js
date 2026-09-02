// render-traffic.js — 服务治理渲染（由 render.js 拆分）。

// 渲染子模块 — 由 render.js 拆分而来。
// 公共依赖：i18n（t）、icons（iconEl/iconHtml）、render-common（DOM/Badge/表单辅助）。

import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { el, renderEmpty, fieldRow } from './render-common.js';

// ============================================================================
// Phase 2：服务治理渲染
// ============================================================================

// renderTrafficTable 渲染流量策略列表表格。
// handlers: { onEnable(id), onDisable(id), onDelete(id) }
export function renderTrafficTable(container, policies, handlers) {
  container.innerHTML = '';
  if (!policies || policies.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('traffic.policyName') }),
        el('th', { text: t('traffic.service') }),
        el('th', { text: t('traffic.policyType') }),
        el('th', { text: t('traffic.timeout') }),
        el('th', { text: t('traffic.retries') }),
        el('th', { text: t('common.status') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      policies.map((p) => {
        const enabled = p.enabled !== false && p.status !== 'disabled';
        return el('tr', null,
          el('td', { class: 'mono', text: p.id }),
          el('td', { class: 'cell-title', text: p.name }),
          el('td', { text: p.service || '-' }),
          el('td', null, el('span', { class: 'badge badge-priority-medium', text: t('traffic.type.' + (p.type || 'timeout'), p.type || '-') })),
          el('td', { class: 'mono', text: String(p.timeout != null ? p.timeout : '-') }),
          el('td', { class: 'mono', text: String(p.retries != null ? p.retries : '-') }),
          el('td', null, el('span', { class: 'badge badge-status-' + (enabled ? 'resolved' : 'closed'), text: enabled ? t('common.enabled') : t('common.disabled') })),
          el('td', { class: 'td-actions' },
            enabled
              ? el('button', { class: 'btn-icon', title: t('common.disable'), onclick: () => handlers.onDisable(p.id) }, iconEl('toggle_on', 14))
              : el('button', { class: 'btn-icon', title: t('common.enable'), onclick: () => handlers.onEnable(p.id) }, iconEl('toggle_off', 14)),
            el('button', { class: 'btn-icon btn-icon-danger', title: t('common.delete'), onclick: () => handlers.onDelete(p.id) }, iconEl('trash', 14))
          )
        );
      })
    )
  );
  container.appendChild(table);
}

// renderTrafficForm 渲染流量策略创建表单。
// handlers: { onSubmit(data), onCancel() }
export function renderTrafficForm(container, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => { e.preventDefault(); handlers.onSubmit(collectTrafficForm(form)); } });

  form.appendChild(el('h3', { class: 'form-title', text: t('traffic.create') }));

  form.appendChild(fieldRow(t('traffic.policyName'), true,
    el('input', { type: 'text', name: 'name', required: 'true', placeholder: t('traffic.nameRequired') })
  ));
  form.appendChild(fieldRow(t('traffic.service'), true,
    el('input', { type: 'text', name: 'service', required: 'true', placeholder: t('traffic.serviceRequired') })
  ));
  form.appendChild(fieldRow(t('traffic.policyType'), false,
    el('select', { name: 'type' },
      ['timeout', 'retry', 'circuitbreaker', 'ratelimit'].map((tp) =>
        el('option', { value: tp, text: t('traffic.type.' + tp) })
      )
    )
  ));
  form.appendChild(fieldRow(t('traffic.timeout'), false,
    el('input', { type: 'number', name: 'timeout', min: '0', value: '3000' })
  ));
  form.appendChild(fieldRow(t('traffic.retries'), false,
    el('input', { type: 'number', name: 'retries', min: '0', max: '10', value: '3' })
  ));

  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('check', 16), el('span', { text: t('common.save') })),
    el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => handlers.onCancel() }, el('span', { text: t('common.cancel') }))
  ));

  container.appendChild(form);
}

function collectTrafficForm(form) {
  const get = (name) => (form.elements[name] && form.elements[name].value) || '';
  return {
    name: get('name').trim(),
    service: get('service').trim(),
    type: get('type'),
    timeout: parseInt(get('timeout'), 10) || 0,
    retries: parseInt(get('retries'), 10) || 0,
  };
}

