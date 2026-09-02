// flow-billing.js — 计费订阅编排（由 flow.js 拆分）。

// flow 子模块 — 由 flow.js 拆分而来。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, $ } from './flow-state.js';

function billingContent() { return $('billing-content'); }

// loadBilling 加载计费页（计划 + 订阅 + 账单）。
export async function loadBilling() {
  state._billingLoaded = true;
  const content = billingContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const [plans, subs, invoices] = await Promise.all([
      api.getBillingPlans(),
      api.getSubscriptions(),
      api.getInvoices(),
    ]);
    state.billingPlans = plans;
    state.billingSubs = subs;
    state.billingInvoices = invoices;
    render.renderBillingPage(content, plans, subs, invoices, {
      onCreatePlan: () => createBillingPlan(),
      onEditPlan: (p) => editBillingPlan(p),
      onDeletePlan: (id) => deleteBillingPlan(id),
      onCreateSub: () => createSubscription(),
      onEditSub: (s) => editSubscription(s),
      onDeleteSub: (id) => deleteSubscription(id),
    });
  } catch (err) {
    render.renderError(content, t('billing.plansLoadFailed') + ': ' + err.message);
  }
}

// createBillingPlan 打开创建计费计划表单。
export function createBillingPlan() {
  const content = billingContent();
  if (!content) return;
  render.renderBillingPlanForm(content, null, {
    onSubmit: async (data) => {
      if (!data.name) { render.renderToast(t('billing.planNameRequired'), 'warn'); return; }
      try {
        await api.createBillingPlan(data);
        render.renderToast(t('billing.planCreated'), 'success');
        loadBilling();
      } catch (err) {
        render.renderToast(t('billing.planCreateFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => loadBilling(),
  });
}

// editBillingPlan 打开编辑计费计划表单。
export async function editBillingPlan(p) {
  const content = billingContent();
  if (!content) return;
  const id = p && p.id;
  if (!id) return;
  render.renderLoading(content);
  try {
    const plan = await api.getBillingPlan(id);
    render.renderBillingPlanForm(content, plan, {
      onSubmit: async (data) => {
        if (!data.name) { render.renderToast(t('billing.planNameRequired'), 'warn'); return; }
        try {
          await api.updateBillingPlan(id, data);
          render.renderToast(t('billing.planUpdated'), 'success');
          loadBilling();
        } catch (err) {
          render.renderToast(t('billing.planUpdateFailed') + ': ' + err.message, 'error');
        }
      },
      onCancel: () => loadBilling(),
    });
  } catch (err) {
    render.renderError(content, t('billing.plansLoadFailed') + ': ' + err.message);
  }
}

// deleteBillingPlan 删除计费计划。
export async function deleteBillingPlan(id) {
  if (!window.confirm(t('billing.planConfirmDelete'))) return;
  try {
    await api.deleteBillingPlan(id);
    render.renderToast(t('billing.planDeleted'), 'success');
    loadBilling();
  } catch (err) {
    render.renderToast(t('billing.planDeleteFailed') + ': ' + err.message, 'error');
  }
}

// createSubscription 打开创建订阅表单。
export function createSubscription() {
  const content = billingContent();
  if (!content) return;
  render.renderSubscriptionForm(content, null, {
    onSubmit: async (data) => {
      if (!data.tenantID) { render.renderToast(t('tenant.codeRequired'), 'warn'); return; }
      try {
        await api.createSubscription(data);
        render.renderToast(t('billing.subCreated'), 'success');
        loadBilling();
      } catch (err) {
        render.renderToast(t('billing.subCreateFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => loadBilling(),
  });
}

// editSubscription 打开编辑订阅表单。
export async function editSubscription(s) {
  const content = billingContent();
  if (!content) return;
  const id = s && s.id;
  if (!id) return;
  render.renderLoading(content);
  try {
    const sub = await api.getSubscription(id);
    render.renderSubscriptionForm(content, sub, {
      onSubmit: async (data) => {
        try {
          await api.updateSubscription(id, data);
          render.renderToast(t('billing.subUpdated'), 'success');
          loadBilling();
        } catch (err) {
          render.renderToast(t('billing.subUpdateFailed') + ': ' + err.message, 'error');
        }
      },
      onCancel: () => loadBilling(),
    });
  } catch (err) {
    render.renderError(content, t('billing.subsLoadFailed') + ': ' + err.message);
  }
}

// deleteSubscription 删除订阅。
export async function deleteSubscription(id) {
  if (!window.confirm(t('billing.subConfirmDelete'))) return;
  try {
    await api.deleteSubscription(id);
    render.renderToast(t('billing.subDeleted'), 'success');
    loadBilling();
  } catch (err) {
    render.renderToast(t('billing.subDeleteFailed') + ': ' + err.message, 'error');
  }
}

// buildBillingToolbar 构建计费工具栏。
export function buildBillingToolbar() {
  const toolbar = $('billing-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-primary', onclick: () => createBillingPlan() },
      iconEl('plus', 16), render.el('span', { text: t('billing.createPlan') })
    )
  );
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-secondary', onclick: () => createSubscription() },
      iconEl('plus', 16), render.el('span', { text: t('billing.createSub') })
    )
  );
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => loadBilling() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}

// --- 平台配置 ---

