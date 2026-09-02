// render-billing.js — 计费订阅渲染（由 render.js 拆分）。

// 渲染子模块 — 由 render.js 拆分而来。
// 公共依赖：i18n（t）、icons（iconEl/iconHtml）、render-common（DOM/Badge/表单辅助）。

import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { el, formatTime, badge, fieldRow } from './render-common.js';

export function renderBillingPage(container, plans, subscriptions, invoices, handlers) {
  container.innerHTML = '';
  // 计划区
  const plansHost = el('div', { class: 'content', style: { marginBottom: '1rem' } });
  plansHost.appendChild(el('h3', { class: 'form-title', text: t('billing.plans') }));
  if (!plans || !plans.length) {
    plansHost.appendChild(el('div', { class: 'state state-empty', text: t('common.empty') }));
  } else {
    plansHost.appendChild(el('table', { class: 'data-table' },
      el('thead', null,
        el('tr', null,
          el('th', { text: t('common.id') }),
          el('th', { text: t('billing.planName') }),
          el('th', { text: t('billing.price') }),
          el('th', { text: t('billing.interval') }),
          el('th', { class: 'th-actions', text: t('common.actions') })
        )
      ),
      el('tbody', null,
        plans.map((p) => el('tr', null,
          el('td', { class: 'mono', text: p.id }),
          el('td', { class: 'cell-title', text: p.name }),
          el('td', { class: 'mono', text: String(p.price != null ? p.price : '-') }),
          el('td', { text: p.interval || '-' }),
          el('td', { class: 'td-actions' },
            el('button', { class: 'btn btn-ghost', title: t('common.edit'), onclick: () => handlers.onEditPlan && handlers.onEditPlan(p) },
              iconEl('edit', 14)),
            el('button', { class: 'btn-icon btn-icon-danger', title: t('common.delete'), onclick: () => handlers.onDeletePlan && handlers.onDeletePlan(p.id) },
              iconEl('trash', 14))
          )
        ))
      )
    ));
  }
  container.appendChild(plansHost);

  // 订阅区
  const subsHost = el('div', { class: 'content', style: { marginBottom: '1rem' } });
  subsHost.appendChild(el('h3', { class: 'form-title', text: t('billing.subscriptions') }));
  if (!subscriptions || !subscriptions.length) {
    subsHost.appendChild(el('div', { class: 'state state-empty', text: t('common.empty') }));
  } else {
    subsHost.appendChild(el('table', { class: 'data-table' },
      el('thead', null,
        el('tr', null,
          el('th', { text: t('common.id') }),
          el('th', { text: t('billing.subTenant') }),
          el('th', { text: t('billing.subPlan') }),
          el('th', { text: t('billing.subStatus') }),
          el('th', { text: t('billing.subStart') }),
          el('th', { text: t('billing.subEnd') }),
          el('th', { class: 'th-actions', text: t('common.actions') })
        )
      ),
      el('tbody', null,
        subscriptions.map((s) => el('tr', null,
          el('td', { class: 'mono', text: s.id }),
          el('td', { class: 'mono', text: s.tenantID || s.tenantId || '-' }),
          el('td', { text: s.planID || s.planId || s.plan || '-' }),
          el('td', null, badge(s.status || '-', 'status-in_progress')),
          el('td', { class: 'mono', text: formatTime(s.startedAt || s.start) }),
          el('td', { class: 'mono', text: formatTime(s.endedAt || s.end) }),
          el('td', { class: 'td-actions' },
            el('button', { class: 'btn btn-ghost', title: t('common.edit'), onclick: () => handlers.onEditSub && handlers.onEditSub(s) },
              iconEl('edit', 14)),
            el('button', { class: 'btn-icon btn-icon-danger', title: t('common.delete'), onclick: () => handlers.onDeleteSub && handlers.onDeleteSub(s.id) },
              iconEl('trash', 14))
          )
        ))
      )
    ));
  }
  container.appendChild(subsHost);

  // 账单区
  const invHost = el('div', { class: 'content' });
  invHost.appendChild(el('h3', { class: 'form-title', text: t('billing.invoices') }));
  if (!invoices || !invoices.length) {
    invHost.appendChild(el('div', { class: 'state state-empty', text: t('common.empty') }));
  } else {
    invHost.appendChild(el('table', { class: 'data-table' },
      el('thead', null,
        el('tr', null,
          el('th', { text: t('billing.invoiceNo') }),
          el('th', { text: t('billing.subTenant') }),
          el('th', { text: t('billing.invoiceAmount') }),
          el('th', { text: t('billing.invoiceStatus') }),
          el('th', { text: t('billing.invoicePeriod') })
        )
      ),
      el('tbody', null,
        invoices.map((iv) => {
          const paid = String(iv.status || '').toLowerCase() === 'paid';
          return el('tr', null,
            el('td', { class: 'mono', text: iv.id || iv.number || '-' }),
            el('td', { class: 'mono', text: iv.tenantID || iv.tenantId || '-' }),
            el('td', { class: 'mono', text: String(iv.amount != null ? iv.amount : '-') }),
            el('td', null, badge(paid ? t('billing.paid') : t('billing.unpaid'), paid ? 'status-resolved' : 'status-closed')),
            el('td', { class: 'mono', text: formatTime(iv.period || iv.createdAt) })
          );
        })
      )
    ));
  }
  container.appendChild(invHost);
}

// renderBillingPlanForm 渲染计费计划创建/编辑表单。
// plan: 编辑时传入；handlers: { onSubmit(data), onCancel() }
export function renderBillingPlanForm(container, plan, handlers) {
  container.innerHTML = '';
  const isEdit = !!plan;
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const data = {
      name: form.elements.name.value.trim(),
      price: parseFloat(form.elements.price.value) || 0,
      interval: form.elements.interval.value.trim(),
      features: form.elements.features.value.trim(),
    };
    handlers.onSubmit && handlers.onSubmit(data);
  } });
  form.appendChild(el('h3', { class: 'form-title', text: isEdit ? t('billing.editPlan') : t('billing.createPlan') }));
  form.appendChild(fieldRow(t('billing.planName'), true,
    el('input', { name: 'name', type: 'text', required: 'true', value: (plan && plan.name) || '', placeholder: t('billing.planNameRequired') })
  ));
  form.appendChild(fieldRow(t('billing.price'), false,
    el('input', { name: 'price', type: 'number', min: '0', step: '0.01', value: String((plan && plan.price) != null ? (plan && plan.price) : 0) })
  ));
  form.appendChild(fieldRow(t('billing.interval'), false,
    el('select', { name: 'interval' },
      ['monthly', 'yearly', 'daily'].map((iv) =>
        el('option', { value: iv, text: iv, selected: (plan && plan.interval === iv) ? 'selected' : undefined })
      )
    )
  ));
  form.appendChild(fieldRow(t('billing.features'), false,
    el('input', { name: 'features', type: 'text', value: (plan && plan.features) || '', placeholder: 'feature1,feature2' })
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

// renderSubscriptionForm 渲染订阅创建/编辑表单。
// sub: 编辑时传入；handlers: { onSubmit(data), onCancel() }
export function renderSubscriptionForm(container, sub, handlers) {
  container.innerHTML = '';
  const isEdit = !!sub;
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const data = {
      tenantID: form.elements.tenantID.value.trim(),
      planID: form.elements.planID.value.trim(),
    };
    handlers.onSubmit && handlers.onSubmit(data);
  } });
  form.appendChild(el('h3', { class: 'form-title', text: isEdit ? t('billing.editSub') : t('billing.createSub') }));
  form.appendChild(fieldRow(t('billing.subTenant'), true,
    el('input', { name: 'tenantID', type: 'text', required: 'true', value: (sub && (sub.tenantID || sub.tenantId)) || '', placeholder: 'tenant id' })
  ));
  form.appendChild(fieldRow(t('billing.subPlan'), true,
    el('input', { name: 'planID', type: 'text', required: 'true', value: (sub && (sub.planID || sub.planId)) || '', placeholder: 'plan id' })
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

// --- 平台配置 ---


// renderPlatformPage 渲染平台配置页（配置表单 + 健康状态 + 指标仪表盘）。
// handlers: { onSaveConfig(data) }
