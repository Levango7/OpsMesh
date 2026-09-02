// flow-traffic.js — 服务治理编排（由 flow.js 拆分）。

// flow 子模块 — 由 flow.js 拆分而来。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, $ } from './flow-state.js';

// ============================================================================
// Phase 2：服务治理
// ============================================================================

function trafficContent() { return $('traffic-content'); }

// loadTrafficPolicies 加载流量策略列表。
export async function loadTrafficPolicies() {
  const content = trafficContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const policies = await api.getTrafficPolicies();
    state.trafficPolicies = policies;
    render.renderTrafficTable(content, policies, {
      onEnable: (id) => enableTrafficPolicy(id),
      onDisable: (id) => disableTrafficPolicy(id),
      onDelete: (id) => deleteTrafficPolicy(id),
    });
  } catch (err) {
    render.renderError(content, t('traffic.loadFailed') + ': ' + err.message);
  }
}

// createTrafficPolicy 打开创建流量策略表单。
export function createTrafficPolicy() {
  const content = trafficContent();
  if (!content) return;
  render.renderTrafficForm(content, {
    onSubmit: async (data) => {
      if (!data.name) { render.renderToast(t('traffic.nameRequired'), 'warn'); return; }
      if (!data.service) { render.renderToast(t('traffic.serviceRequired'), 'warn'); return; }
      try {
        await api.createTrafficPolicy(data);
        render.renderToast(t('traffic.created'), 'success');
        loadTrafficPolicies();
      } catch (err) {
        render.renderToast(t('traffic.createFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => loadTrafficPolicies(),
  });
}

// enableTrafficPolicy 启用流量策略。
export async function enableTrafficPolicy(id) {
  try {
    await api.enableTrafficPolicy(id);
    render.renderToast(t('traffic.enabled'), 'success');
    loadTrafficPolicies();
  } catch (err) {
    render.renderToast(t('traffic.enableFailed') + ': ' + err.message, 'error');
  }
}

// disableTrafficPolicy 禁用流量策略。
export async function disableTrafficPolicy(id) {
  try {
    await api.disableTrafficPolicy(id);
    render.renderToast(t('traffic.disabled'), 'success');
    loadTrafficPolicies();
  } catch (err) {
    render.renderToast(t('traffic.disableFailed') + ': ' + err.message, 'error');
  }
}

// deleteTrafficPolicy 删除流量策略。
export async function deleteTrafficPolicy(id) {
  if (!window.confirm(t('traffic.confirmDelete'))) return;
  try {
    await api.deleteTrafficPolicy(id);
    render.renderToast(t('traffic.deleted'), 'success');
    loadTrafficPolicies();
  } catch (err) {
    render.renderToast(t('traffic.deleteFailed') + ': ' + err.message, 'error');
  }
}

// buildTrafficToolbar 构建服务治理工具栏。
export function buildTrafficToolbar() {
  const toolbar = $('traffic-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-primary', onclick: () => createTrafficPolicy() },
      iconEl('plus', 16), render.el('span', { text: t('traffic.create') })
    )
  );
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => loadTrafficPolicies() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}

