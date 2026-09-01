// flow-webhook.js — Webhook 编排（由 flow.js 拆分）。

// flow 子模块 — 由 flow.js 拆分而来。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, \$, pageRoot } from './flow-state.js';

function webhookContent() { return $('webhook-content'); }

// loadWebhooks 加载 Webhook 列表（含子 tab 切换：列表 / 投递记录）。
export async function loadWebhooks() {
  const content = webhookContent();
  if (!content) return;
  content.innerHTML = '';
  // 子 tab 切换条
  const subTabs = [
    { key: 'list',       label: t('webhook.list'),       onclick: () => { state.webhookSubTab = 'list';       refreshWebhookSubTab(); } },
    { key: 'deliveries', label: t('webhook.deliveries'), onclick: () => { state.webhookSubTab = 'deliveries'; refreshWebhookSubTab(); } },
  ];
  const subBar = render.el('div', { class: 'toolbar' });
  subTabs.forEach((s) => {
    subBar.appendChild(render.el('button', {
      class: 'btn ' + (state.webhookSubTab === s.key ? 'btn-secondary' : 'btn-ghost'),
      onclick: s.onclick,
    }, render.el('span', { text: s.label })));
  });
  content.appendChild(subBar);
  const host = render.el('div', { class: 'content', style: { marginTop: '1rem' } });
  content.appendChild(host);
  if (state.webhookSubTab === 'deliveries') {
    if (state.webhookSelectedId) { loadWebhookDeliveries(state.webhookSelectedId); return; }
    render.renderEmpty(host, t('webhook.noDeliveries'));
    return;
  }
  // Webhook 列表
  const listHost = render.el('div', { id: 'webhooks-list' });
  host.appendChild(listHost);
  render.renderLoading(listHost);
  try {
    const webhooks = await api.getWebhooks();
    state.webhooks = webhooks;
    render.renderWebhooksTable(listHost, webhooks, {
      onEdit: (w) => editWebhook(w),
      onTest: (w) => testWebhookSend(w.id || w.name),
      onDeliveries: (w) => { state.webhookSelectedId = w.id || w.name; state.webhookSubTab = 'deliveries'; refreshWebhookSubTab(); },
      onDelete: (w) => deleteWebhook(w.id || w.name),
    });
  } catch (err) {
    render.renderError(listHost, t('webhook.loadFailed') + ': ' + err.message);
  }
  // 创建 Webhook 表单
  const formHost = render.el('div', { id: 'webhook-form', style: { marginTop: '1rem' } });
  host.appendChild(formHost);
  render.renderWebhookForm(formHost, null, { onSubmit: (data) => createWebhook(data) });
}

// createWebhook 创建 Webhook。
export async function createWebhook(data) {
  if (!data || !data.name) { render.renderToast(t('webhook.nameRequired'), 'warn'); return; }
  if (!data.url) { render.renderToast(t('webhook.urlRequired'), 'warn'); return; }
  if (!data.event) { render.renderToast(t('webhook.eventRequired'), 'warn'); return; }
  try {
    await api.createWebhook(data);
    render.renderToast(t('webhook.created'), 'success');
    state.webhookSubTab = 'list';
    loadWebhooks();
  } catch (err) {
    render.renderToast(t('webhook.createFailed') + ': ' + err.message, 'error');
  }
}

// editWebhook 编辑 Webhook。
export async function editWebhook(wh) {
  if (!wh) return;
  state.webhookEditing = wh;
  const content = webhookContent();
  if (!content) return;
  content.innerHTML = '';
  // 返回按钮
  const backBar = render.el('div', { class: 'toolbar' });
  backBar.appendChild(render.el('button', {
    class: 'btn btn-ghost',
    onclick: () => { state.webhookEditing = null; state.webhookSubTab = 'list'; loadWebhooks(); },
  }, iconEl('back', 14), render.el('span', { text: t('webhook.list') })));
  content.appendChild(backBar);
  const host = render.el('div', { class: 'content', style: { marginTop: '1rem' } });
  content.appendChild(host);
  // 先尝试获取详情，失败则用列表数据
  let detail = wh;
  try {
    detail = await api.getWebhook(wh.id || wh.name);
  } catch (_) { /* 用列表数据 */ }
  render.renderWebhookForm(host, detail, { onSubmit: (data) => updateWebhook(wh.id || wh.name, data) });
}

// updateWebhook 更新 Webhook。
export async function updateWebhook(id, data) {
  if (!id) return;
  if (!data || !data.name) { render.renderToast(t('webhook.nameRequired'), 'warn'); return; }
  try {
    await api.updateWebhook(id, data);
    render.renderToast(t('webhook.updated'), 'success');
    state.webhookEditing = null;
    state.webhookSubTab = 'list';
    loadWebhooks();
  } catch (err) {
    render.renderToast(t('webhook.updateFailed') + ': ' + err.message, 'error');
  }
}

// deleteWebhook 删除 Webhook。
export async function deleteWebhook(id) {
  if (!id) return;
  if (!window.confirm(t('webhook.deleteConfirm'))) return;
  try {
    await api.deleteWebhook(id);
    render.renderToast(t('webhook.deleted'), 'success');
    loadWebhooks();
  } catch (err) {
    render.renderToast(t('webhook.deleteFailed') + ': ' + err.message, 'error');
  }
}

// testWebhookSend 测试发送 Webhook。
export async function testWebhookSend(id) {
  if (!id) return;
  try {
    const result = await api.testWebhook(id);
    const msg = (result && (result.message || result.status)) || t('webhook.testSent');
    render.renderToast(t('webhook.testSent') + ': ' + String(msg).slice(0, 100), 'success');
  } catch (err) {
    render.renderToast(t('webhook.testFailed') + ': ' + err.message, 'error');
  }
}

// loadWebhookDeliveries 加载 Webhook 投递记录。
export async function loadWebhookDeliveries(id) {
  if (!id) return;
  const content = webhookContent();
  if (!content) return;
  const host = render.el('div', { id: 'webhook-deliveries-list', class: 'content', style: { marginTop: '1rem' } });
  content.appendChild(host);
  render.renderLoading(host);
  try {
    const deliveries = await api.getWebhookDeliveries(id);
    render.renderWebhookDeliveriesTable(host, deliveries);
  } catch (err) {
    render.renderError(host, t('webhook.deliveriesLoadFailed') + ': ' + err.message);
  }
}

// buildWebhookToolbar 构建 Webhook 工具栏。
export function buildWebhookToolbar() {
  const toolbar = $('webhook-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => refreshWebhookSubTab() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}

// refreshWebhookSubTab 根据当前子 tab 重新渲染 Webhook 页。
function refreshWebhookSubTab() {
  loadWebhooks();
}

// --- 自定义脚本 ---

