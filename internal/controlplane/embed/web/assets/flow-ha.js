// flow-ha.js — 高可用编排（由 flow.js 拆分）。

// flow 子模块 — 由 flow.js 拆分而来。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, $ } from './flow-state.js';

// ============================================================================
// Phase 3：高可用 + 灾备恢复
// ============================================================================

function haContent() { return $('ha-content'); }

// loadHAStatus 加载 HA 状态（含实例列表 + 健康 + 灾备）。
export async function loadHAStatus() {
  state._haLoaded = true;
  const content = haContent();
  if (!content) return;
  content.innerHTML = '';
  // 子 tab 切换条
  const subTabs = [
    { key: 'status', label: t('ha.status'), onclick: () => { state.haSubTab = 'status'; refreshHASubTab(); } },
    { key: 'backup', label: t('ha.backup'), onclick: () => { state.haSubTab = 'backup'; refreshHASubTab(); } },
  ];
  const subBar = render.el('div', { class: 'toolbar' });
  subTabs.forEach((s) => {
    subBar.appendChild(render.el('button', {
      class: 'btn ' + (state.haSubTab === s.key ? 'btn-secondary' : 'btn-ghost'),
      onclick: s.onclick,
    }, render.el('span', { text: s.label })));
  });
  content.appendChild(subBar);
  if (state.haSubTab !== 'status') { refreshHASubTab(); return; }
  // HA 状态卡片
  const statusHost = render.el('div', { class: 'content', style: { marginTop: '1rem' } });
  content.appendChild(statusHost);
  render.renderLoading(statusHost);
  try {
    const status = await api.getHAStatus();
    state.haStatus = status;
    render.renderHAStatus(statusHost, status);
  } catch (err) {
    render.renderError(statusHost, t('ha.statusLoadFailed') + ': ' + err.message);
  }
  // failover 按钮
  const foHost = render.el('div', { style: { marginTop: '1rem' } });
  content.appendChild(foHost);
  foHost.appendChild(render.el('button', {
    class: 'btn btn-secondary',
    onclick: () => failoverHA(),
  }, iconEl('failover', 16), render.el('span', { text: t('ha.failover') })));
  // 实例列表
  const insHost = render.el('div', { class: 'content', style: { marginTop: '1rem' } });
  content.appendChild(insHost);
  render.renderLoading(insHost);
  try {
    const instances = await api.getHAInstances();
    state.haInstances = instances;
    render.renderHAInstancesTable(insHost, instances);
  } catch (err) {
    render.renderError(insHost, t('ha.statusLoadFailed') + ': ' + err.message);
  }
  // 健康状态
  const healthHost = render.el('div', { class: 'content', style: { marginTop: '1rem' } });
  content.appendChild(healthHost);
  render.renderLoading(healthHost);
  try {
    const health = await api.getHAHealth();
    state.haHealth = health;
    render.renderHAHealth(healthHost, health);
  } catch (err) {
    render.renderError(healthHost, t('ha.healthLoadFailed') + ': ' + err.message);
  }
}

// failoverHA 手动 failover。
export async function failoverHA() {
  if (!window.confirm(t('ha.failoverConfirm'))) return;
  try {
    await api.failoverHA();
    render.renderToast(t('ha.failoverDone'), 'success');
    loadHAStatus();
  } catch (err) {
    render.renderToast(t('ha.failoverFailed') + ': ' + err.message, 'error');
  }
}

// loadBackups 加载备份列表。
export async function loadBackups() {
  const content = haContent();
  if (!content) return;
  let host = $('ha-backups-list');
  if (!host) {
    host = render.el('div', { id: 'ha-backups-list', class: 'content', style: { marginTop: '1rem' } });
    content.appendChild(host);
  }
  render.renderLoading(host);
  try {
    const backups = await api.listBackups();
    state.backups = backups;
    render.renderBackupsTable(host, backups, {
      onRestore: (b) => restoreBackup(b.id),
      onDelete: (b) => deleteBackup(b.id),
    });
  } catch (err) {
    render.renderError(host, t('ha.backupsLoadFailed') + ': ' + err.message);
  }
}

// createBackup 创建备份。
export async function createBackup(type) {
  if (!type) { render.renderToast(t('ha.typeRequired'), 'warn'); return; }
  try {
    await api.createBackup(type);
    render.renderToast(t('ha.created'), 'success');
    loadBackups();
  } catch (err) {
    render.renderToast(t('ha.createFailed') + ': ' + err.message, 'error');
  }
}

// restoreBackup 恢复备份。
export async function restoreBackup(id) {
  if (!id) return;
  if (!window.confirm(t('ha.restoreConfirm'))) return;
  try {
    await api.restoreBackup(id);
    render.renderToast(t('ha.restoreDone'), 'success');
  } catch (err) {
    render.renderToast(t('ha.restoreFailed') + ': ' + err.message, 'error');
  }
}

// deleteBackup 删除备份。
export async function deleteBackup(id) {
  if (!id) return;
  if (!window.confirm(t('ha.deleteConfirm'))) return;
  try {
    await api.deleteBackup(id);
    render.renderToast(t('ha.deleted'), 'success');
    loadBackups();
  } catch (err) {
    render.renderToast(t('ha.deleteFailed') + ': ' + err.message, 'error');
  }
}

// buildHAToolbar 构建 HA 工具栏。
export function buildHAToolbar() {
  const toolbar = $('ha-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => refreshHASubTab() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}

// refreshHASubTab 根据当前子 tab 重新渲染 HA 页。
function refreshHASubTab() {
  const sub = state.haSubTab;
  if (sub === 'status') { loadHAStatus(); return; }
  const content = haContent();
  if (!content) return;
  content.innerHTML = '';
  // 子 tab 切换条
  const subTabs = [
    { key: 'status', label: t('ha.status') },
    { key: 'backup', label: t('ha.backup') },
  ];
  const subBar = render.el('div', { class: 'toolbar' });
  subTabs.forEach((s) => {
    subBar.appendChild(render.el('button', {
      class: 'btn ' + (sub === s.key ? 'btn-secondary' : 'btn-ghost'),
      onclick: () => { state.haSubTab = s.key; refreshHASubTab(); },
    }, render.el('span', { text: s.label })));
  });
  content.appendChild(subBar);
  if (sub === 'backup') {
    // 创建备份表单
    const formHost = render.el('div', { class: 'content', style: { marginTop: '1rem' } });
    content.appendChild(formHost);
    render.renderCreateBackupForm(formHost, { onCreate: (type) => createBackup(type) });
    // 备份列表
    loadBackups();
  }
}

