// flow-deploys.js — 部署中心编排（P1 补齐功能域）。

// flow 子模块 — 部署中心（部署列表 / 创建 / 回滚 / 联邦部署列表）。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, $ } from './flow-state.js';

// ============================================================================
// 部署中心
// ============================================================================

function deploysContent() { return $('deploys-content'); }

// loadDeploys 加载部署列表。
export async function loadDeploys() {
  const content = deploysContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const deploys = await api.getDeploys();
    state.deploys.list = deploys;
    render.renderDeploysTable(content, deploys, {
      onRollback: (id) => rollbackDeploy(id),
    });
  } catch (err) {
    render.renderError(content, t('deploys.loadFailed') + ': ' + err.message);
  }
}

// showDeployForm 打开创建部署表单。
export function showDeployForm() {
  const content = deploysContent();
  if (!content) return;
  render.renderDeployForm(content, {
    onSubmit: async (data) => {
      if (!data.name || !data.name.trim()) {
        render.renderToast(t('deploys.nameRequired'), 'warn');
        return;
      }
      if (!data.template || !data.template.trim()) {
        render.renderToast(t('deploys.templateRequired'), 'warn');
        return;
      }
      try {
        await api.createDeploy(data);
        render.renderToast(t('deploys.created'), 'success');
        loadDeploys();
      } catch (err) {
        render.renderToast(t('deploys.createFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => loadDeploys(),
  });
}

// rollbackDeploy 回滚部署（确认后调用 API）。
export async function rollbackDeploy(id) {
  if (!window.confirm(t('deploys.confirmRollback'))) return;
  try {
    await api.rollbackDeploy(id);
    render.renderToast(t('deploys.rolledBack'), 'success');
    loadDeploys();
  } catch (err) {
    render.renderToast(t('deploys.rollbackFailed') + ': ' + err.message, 'error');
  }
}

// loadFederationDeploys 加载联邦部署列表。
export async function loadFederationDeploys() {
  const content = deploysContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const deploys = await api.getFederationDeploys();
    state.deploys.federationList = deploys;
    render.renderFederationDeploysTable(content, deploys);
  } catch (err) {
    render.renderError(content, t('deploys.federationLoadFailed') + ': ' + err.message);
  }
}

// refreshDeploysSubTab 按当前子 tab 刷新部署页。
export function refreshDeploysSubTab() {
  if (state.deploysSubTab === 'federation') loadFederationDeploys();
  else if (state.deploysSubTab === 'create') showDeployForm();
  else loadDeploys();
}

// buildDeploysToolbar 构建部署工具栏（子 tab + 创建按钮 + 刷新）。
export function buildDeploysToolbar() {
  const toolbar = $('deploys-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  // 子 tab 切换组
  const subTabs = [
    { key: 'list', label: t('deploys.tabList'), onActivate: () => loadDeploys() },
    { key: 'federation', label: t('deploys.tabFederation'), onActivate: () => loadFederationDeploys() },
  ];
  subTabs.forEach((st) => {
    toolbar.appendChild(
      render.el('button', {
        class: 'btn ' + (state.deploysSubTab === st.key ? 'btn-secondary' : 'btn-ghost'),
        onclick: () => { state.deploysSubTab = st.key; st.onActivate(); buildDeploysToolbar(); },
      }, render.el('span', { text: st.label }))
    );
  });
  // 创建按钮
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-primary', onclick: () => { state.deploysSubTab = 'list'; showDeployForm(); buildDeploysToolbar(); } },
      iconEl('plus', 16), render.el('span', { text: t('deploys.create') })
    )
  );
  // 刷新
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => refreshDeploysSubTab() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}
