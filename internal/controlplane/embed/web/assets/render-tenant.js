// render-tenant.js — 租户管理渲染（由 render.js 拆分）。

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
// Phase 6：平台化管理渲染（租户 / API Key / 插件市场 / 计费订阅 / 平台配置）
// ============================================================================

// --- 租户管理 ---

// tenantStatusBadge 租户状态 badge。
function tenantStatusBadge(status) {
  const s = String(status || '').toLowerCase();
  if (s === 'active' || s === 'activated') return badge(t('tenant.active'), 'status-resolved');
  if (s === 'suspended' || s === 'paused') return badge(t('tenant.suspended'), 'status-closed');
  return badge(status || '-', 'status-in_progress');
}

// renderTenantPage 渲染租户表格。
// handlers: { onEdit(t), onSuspend(id), onActivate(id), onDelete(id) }
export function renderTenantPage(container, tenants, handlers) {
  container.innerHTML = '';
  if (!tenants || !tenants.length) { renderEmpty(container, t('tenant.list')); return; }
  container.appendChild(el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('tenant.tenantName') }),
        el('th', { text: t('tenant.tenantCode') }),
        el('th', { text: t('tenant.plan') }),
        el('th', { text: t('tenant.status') }),
        el('th', { text: t('common.createdAt') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      tenants.map((tn) => {
        const suspended = String(tn.status || '').toLowerCase() === 'suspended';
        return el('tr', null,
          el('td', { class: 'mono', text: tn.id }),
          el('td', { class: 'cell-title', text: tn.name }),
          el('td', { class: 'mono', text: tn.code || '-' }),
          el('td', { text: tn.plan || '-' }),
          el('td', null, tenantStatusBadge(tn.status)),
          el('td', { class: 'mono', text: formatTime(tn.createdAt) }),
          el('td', { class: 'td-actions' },
            el('button', { class: 'btn btn-ghost', title: t('common.edit'), onclick: () => handlers.onEdit && handlers.onEdit(tn) },
              iconEl('edit', 14)
            ),
            suspended
              ? el('button', { class: 'btn btn-ghost', title: t('tenant.activate'), onclick: () => handlers.onActivate && handlers.onActivate(tn.id) },
                  iconEl('toggle_off', 14))
              : el('button', { class: 'btn btn-ghost', title: t('tenant.suspend'), onclick: () => handlers.onSuspend && handlers.onSuspend(tn.id) },
                  iconEl('toggle_on', 14)),
            el('button', { class: 'btn btn-ghost btn-icon-danger', title: t('common.delete'), onclick: () => handlers.onDelete && handlers.onDelete(tn.id) },
              iconEl('trash', 14))
          )
        );
      })
    )
  ));
}

// renderTenantForm 渲染租户创建/编辑表单。
// tenant: 编辑时传入，创建时传 null；handlers: { onSubmit(data), onCancel() }
export function renderTenantForm(container, tenant, handlers) {
  container.innerHTML = '';
  const isEdit = !!tenant;
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const data = {
      name: form.elements.name.value.trim(),
      code: form.elements.code.value.trim(),
      plan: form.elements.plan.value.trim(),
      description: form.elements.description.value.trim(),
    };
    handlers.onSubmit && handlers.onSubmit(data);
  } });
  form.appendChild(el('h3', { class: 'form-title', text: isEdit ? t('tenant.edit') : t('tenant.create') }));
  form.appendChild(fieldRow(t('tenant.tenantName'), true,
    el('input', { name: 'name', type: 'text', required: 'true', value: (tenant && tenant.name) || '', placeholder: t('tenant.nameRequired') })
  ));
  form.appendChild(fieldRow(t('tenant.tenantCode'), true,
    el('input', { name: 'code', type: 'text', required: 'true', value: (tenant && tenant.code) || '', placeholder: 'acme-corp' })
  ));
  form.appendChild(fieldRow(t('tenant.plan'), false,
    el('select', { name: 'plan' },
      ['', 'free', 'pro', 'enterprise'].map((p) =>
        el('option', { value: p, text: p || '-', selected: (tenant && tenant.plan === p) ? 'selected' : undefined })
      )
    )
  ));
  form.appendChild(fieldRow(t('common.description'), false,
    el('input', { name: 'description', type: 'text', value: (tenant && tenant.description) || '', placeholder: t('common.description') })
  ));
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' },
      iconEl('check', 16), el('span', { text: t('common.save') })
    ),
    el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => handlers.onCancel && handlers.onCancel() },
      el('span', { text: t('common.cancel') })
    )
  ));
  container.appendChild(form);
}

// --- API Key 管理 ---

// apiKeyStatusBadge API Key 状态 badge。
function apiKeyStatusBadge(status) {
  const s = String(status || '').toLowerCase();
  if (s === 'enabled' || s === 'active') return badge(t('common.enabled'), 'status-resolved');
  if (s === 'disabled' || s === 'inactive') return badge(t('common.disabled'), 'status-closed');
  return badge(status || '-', 'status-in_progress');
}

// renderAPIKeyPage 渲染 API Key 表格。
// handlers: { onEdit(k), onToggle(k), onDelete(id) }
