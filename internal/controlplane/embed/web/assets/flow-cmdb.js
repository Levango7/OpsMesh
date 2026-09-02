// flow-cmdb.js — CMDB 编排（P1 补齐功能域）。

// flow 子模块 — CMDB（CI 类型列表 / CI 项列表 / 触发采集 / 变更申请列表）。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, $ } from './flow-state.js';

// ============================================================================
// CMDB
// ============================================================================

function cmdbContent() { return $('cmdb-content'); }

// loadCMDBTypes 加载 CI 类型列表。
export async function loadCMDBTypes() {
  const content = cmdbContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const types = await api.getCMDBTypes();
    state.cmdb.types = types;
    render.renderCMDBTypesTable(content, types, {
      onSelect: (type) => { state.cmdb.selectedType = type; loadCMDBItems(type); },
    });
  } catch (err) {
    render.renderError(content, t('cmdb.typesLoadFailed') + ': ' + err.message);
  }
}

// loadCMDBItems 加载 CI 项列表（按类型筛选）。
export async function loadCMDBItems(type) {
  const content = cmdbContent();
  if (!content) return;
  const filterType = type || state.cmdb.selectedType;
  state.cmdb.selectedType = filterType;
  render.renderLoading(content);
  try {
    const items = await api.getCMDBCIs(filterType ? { type: filterType } : {});
    state.cmdb.items = items;
    render.renderCMDBItemsTable(content, items, filterType);
  } catch (err) {
    render.renderError(content, t('cmdb.itemsLoadFailed') + ': ' + err.message);
  }
}

// collectCMDB 触发 CMDB 采集。
export async function collectCMDB() {
  const content = cmdbContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const result = await api.collectCMDB();
    render.renderCMDBCollectResult(content, result, {
      onBack: () => loadCMDBTypes(),
    });
  } catch (err) {
    render.renderError(content, t('cmdb.collectFailed') + ': ' + err.message);
  }
}

// loadCMDBChanges 加载变更申请列表。
export async function loadCMDBChanges() {
  const content = cmdbContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const changes = await api.getCMDBChanges();
    state.cmdb.changes = changes;
    render.renderCMDBChangesTable(content, changes);
  } catch (err) {
    render.renderError(content, t('cmdb.changesLoadFailed') + ': ' + err.message);
  }
}

// refreshCMDBSubTab 按当前子 tab 刷新 CMDB 页。
export function refreshCMDBSubTab() {
  if (state.cmdbSubTab === 'items') loadCMDBItems();
  else if (state.cmdbSubTab === 'changes') loadCMDBChanges();
  else loadCMDBTypes();
}

// buildCMDBToolbar 构建 CMDB 工具栏（子 tab + 采集按钮 + 刷新）。
export function buildCMDBToolbar() {
  const toolbar = $('cmdb-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  // 子 tab 切换组
  const subTabs = [
    { key: 'types', label: t('cmdb.tabTypes'), onActivate: () => loadCMDBTypes() },
    { key: 'items', label: t('cmdb.tabItems'), onActivate: () => loadCMDBItems() },
    { key: 'changes', label: t('cmdb.tabChanges'), onActivate: () => loadCMDBChanges() },
  ];
  subTabs.forEach((st) => {
    toolbar.appendChild(
      render.el('button', {
        class: 'btn ' + (state.cmdbSubTab === st.key ? 'btn-secondary' : 'btn-ghost'),
        onclick: () => { state.cmdbSubTab = st.key; st.onActivate(); buildCMDBToolbar(); },
      }, render.el('span', { text: st.label }))
    );
  });
  // 采集按钮
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-primary', onclick: () => collectCMDB() },
      iconEl('sync', 16), render.el('span', { text: t('cmdb.collect') })
    )
  );
  // 刷新
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => refreshCMDBSubTab() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}
