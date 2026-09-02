// flow-helm.js — Helm 应用商店编排（P3 补齐功能域）。

// flow 子模块 — Helm 应用商店（仓库管理 + 应用目录 + Release 管理）。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, $ } from './flow-state.js';

// ============================================================================
// Helm 应用商店
// ============================================================================

function helmContent() { return $('helm-content'); }

// renderHelmPanel 渲染 Helm 主面板（仓库 + 目录 + Release）。
function renderHelmPanel(content) {
  content.innerHTML = '';
  // 子面板 1：仓库管理
  const reposCard = render.el('div', { class: 'detail-card' });
  reposCard.appendChild(render.el('h3', { class: 'detail-title', text: t('helm.reposTitle') }));
  const reposBody = render.el('div', { class: 'card-body' });
  render.renderHelmReposTable(reposBody, state.helm.repos, {
    onDelete: (name) => deleteHelmRepo(name),
  });
  reposCard.appendChild(reposBody);
  content.appendChild(reposCard);
  // 子面板 2：应用目录
  const catalogCard = render.el('div', { class: 'detail-card' });
  catalogCard.appendChild(render.el('h3', { class: 'detail-title', text: t('helm.catalogTitle') }));
  const catalogBody = render.el('div', { class: 'card-body' });
  render.renderHelmCatalog(catalogBody, state.helm.catalog);
  catalogCard.appendChild(catalogBody);
  content.appendChild(catalogCard);
  // 子面板 2.1：Chart 搜索
  const searchCard = render.el('div', { class: 'detail-card' });
  const searchBody = render.el('div', { class: 'card-body' });
  render.renderHelmChartsSearchForm(searchBody, {
    onSearch: (q) => searchHelmCharts(q),
  });
  searchCard.appendChild(searchBody);
  content.appendChild(searchCard);
  // 子面板 2.2：搜索结果（有结果时显示）
  if (state.helm.charts && state.helm.charts.length > 0) {
    const chartsCard = render.el('div', { class: 'detail-card' });
    chartsCard.appendChild(render.el('h3', { class: 'detail-title', text: t('helm.searchResult') }));
    const chartsBody = render.el('div', { class: 'card-body' });
    render.renderHelmChartsTable(chartsBody, state.helm.charts);
    chartsCard.appendChild(chartsBody);
    content.appendChild(chartsCard);
  }
  // 子面板 3：Release 管理
  const releasesCard = render.el('div', { class: 'detail-card' });
  releasesCard.appendChild(render.el('h3', { class: 'detail-title', text: t('helm.releasesTitle') }));
  const releasesBody = render.el('div', { class: 'card-body' });
  render.renderHelmReleasesTable(releasesBody, state.helm.releases, {
    onUninstall: (name) => uninstallHelmRelease(name),
  });
  releasesCard.appendChild(releasesBody);
  content.appendChild(releasesCard);
}

// loadHelmRepos 加载 Helm 仓库列表。
export async function loadHelmRepos() {
  const content = helmContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const data = await api.getHelmRepos();
    const list = (data && data.repos) ? data.repos : (Array.isArray(data) ? data : []);
    state.helm.repos = list;
    renderHelmPanel(content);
  } catch (err) {
    render.renderError(content, t('helm.reposLoadFailed') + ': ' + err.message);
  }
}

// showHelmRepoForm 打开添加 Helm 仓库表单。
export function showHelmRepoForm() {
  const content = helmContent();
  if (!content) return;
  render.renderHelmRepoForm(content, {
    onSubmit: async (data) => {
      if (!data.name) {
        render.renderToast(t('helm.repoNameRequired'), 'warn');
        return;
      }
      if (!data.url) {
        render.renderToast(t('helm.repoURLRequired'), 'warn');
        return;
      }
      try {
        await api.createHelmRepo(data);
        render.renderToast(t('helm.repoAdded'), 'success');
        loadHelmAll();
      } catch (err) {
        render.renderToast(t('helm.repoAddFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => loadHelmAll(),
  });
}

// deleteHelmRepo 删除 Helm 仓库（确认后调用 API）。
export async function deleteHelmRepo(name) {
  if (!window.confirm(t('helm.confirmDeleteRepo'))) return;
  try {
    await api.deleteHelmRepo(name);
    render.renderToast(t('helm.repoDeleted'), 'success');
    loadHelmAll();
  } catch (err) {
    render.renderToast(t('helm.repoDeleteFailed') + ': ' + err.message, 'error');
  }
}

// searchHelmCharts 搜索 Helm Chart。
export async function searchHelmCharts(q) {
  const content = helmContent();
  if (!content) return;
  if (!q) {
    render.renderToast(t('helm.searchRequired'), 'warn');
    return;
  }
  try {
    render.renderToast(t('helm.searching'), 'info');
    const data = await api.searchHelmCharts(q);
    const list = (data && data.charts) ? data.charts : (Array.isArray(data) ? data : []);
    state.helm.charts = list;
    renderHelmPanel(content);
    render.renderToast(t('helm.searchDone'), 'success');
  } catch (err) {
    render.renderToast(t('helm.searchFailed') + ': ' + err.message, 'error');
  }
}

// loadHelmReleases 加载 Helm Release 列表。
export async function loadHelmReleases() {
  const content = helmContent();
  if (!content) return;
  try {
    const data = await api.getHelmReleases();
    const list = (data && data.releases) ? data.releases : (Array.isArray(data) ? data : []);
    state.helm.releases = list;
    renderHelmPanel(content);
  } catch (err) {
    render.renderToast(t('helm.releasesLoadFailed') + ': ' + err.message, 'error');
  }
}

// showHelmReleaseForm 打开安装 Helm Release 表单。
export function showHelmReleaseForm() {
  const content = helmContent();
  if (!content) return;
  const repoNames = (state.helm.repos || []).map((r) => (r && r.name) || '');
  render.renderHelmReleaseForm(content, repoNames, {
    onSubmit: async (data) => {
      if (!data.name) {
        render.renderToast(t('helm.releaseNameRequired'), 'warn');
        return;
      }
      if (!data.chart) {
        render.renderToast(t('helm.chartNameRequired'), 'warn');
        return;
      }
      try {
        await api.installHelmRelease(data);
        render.renderToast(t('helm.releaseInstalled'), 'success');
        loadHelmAll();
      } catch (err) {
        render.renderToast(t('helm.releaseInstallFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => loadHelmAll(),
  });
}

// uninstallHelmRelease 卸载 Helm Release（确认后调用 API）。
export async function uninstallHelmRelease(name) {
  if (!window.confirm(t('helm.confirmUninstall'))) return;
  try {
    await api.uninstallHelmRelease(name);
    render.renderToast(t('helm.releaseUninstalled'), 'success');
    loadHelmAll();
  } catch (err) {
    render.renderToast(t('helm.releaseUninstallFailed') + ': ' + err.message, 'error');
  }
}

// loadHelmAll 加载 Helm 全部数据（仓库 + 目录 + Release）。
export async function loadHelmAll() {
  const content = helmContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const [reposData, catalogData, releasesData] = await Promise.all([
      api.getHelmRepos(),
      api.getHelmCatalog(),
      api.getHelmReleases(),
    ]);
    state.helm.repos = (reposData && reposData.repos) ? reposData.repos : (Array.isArray(reposData) ? reposData : []);
    state.helm.catalog = (catalogData && catalogData.categories) ? catalogData.categories : (Array.isArray(catalogData) ? catalogData : []);
    state.helm.releases = (releasesData && releasesData.releases) ? releasesData.releases : (Array.isArray(releasesData) ? releasesData : []);
    renderHelmPanel(content);
  } catch (err) {
    render.renderError(content, t('helm.reposLoadFailed') + ': ' + err.message);
  }
}

// refreshHelmSubTab 刷新 Helm 页（重新加载全部数据）。
export function refreshHelmSubTab() {
  loadHelmAll();
}

// buildHelmToolbar 构建 Helm 工具栏（添加仓库 + 安装 Release + 刷新）。
export function buildHelmToolbar() {
  const toolbar = $('helm-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  // 添加仓库按钮
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-primary', onclick: () => showHelmRepoForm() },
      iconEl('plus', 16), render.el('span', { text: t('helm.addRepo') })
    )
  );
  // 安装 Release 按钮
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-primary', onclick: () => showHelmReleaseForm() },
      iconEl('rocket', 16), render.el('span', { text: t('helm.installRelease') })
    )
  );
  // 刷新
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => loadHelmAll() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}
