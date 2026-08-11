// flow_helm.js — task 242 M3 集成：Helm 应用商店页面交互逻辑
//
// 职责：
//   - loadHelmCatalog：加载预置应用目录并按分类渲染
//   - searchHelmCatalog：搜索应用
//   - showHelmInstallModal/confirmHelmInstall：安装弹窗
//   - loadHelmRepos/showAddHelmRepoModal/confirmAddHelmRepo/deleteHelmRepoConfirm：仓库管理
//   - loadHelmReleases：release 列表 + 升级/回滚/卸载/历史操作
//
// 依赖：api.js、render.js（esc/escAttr/fmtTime）、icons.js、i18n.js

import * as api from './api.js';
import { esc, escAttr, fmtTime, fmtBytes } from './render.js';
import { icon } from './icons.js';
import { t } from './i18n.js';

// 当前选中的目录分类（null=全部）。
let currentCategory = '';
// 当前安装选中的 catalog item（用于安装弹窗回填）。
let pendingInstallItem = null;

// ============================================================================
// Helm 应用商店目录
// ============================================================================

// loadHelmCatalog 加载预置应用目录并渲染到 #helmCatalogGrid。
export function loadHelmCatalog() {
  api.getHelmCatalog(currentCategory || null, null).then(function (data) {
    renderHelmCatalogCategories(data.categories || []);
    renderHelmCatalogItems(data.items || []);
  }).catch(function (e) { api.apiFail('helmCatalog', e); });
}

// renderHelmCatalogCategories 渲染分类筛选条。
function renderHelmCatalogCategories(categories) {
  const el = document.getElementById('helmCategoryBar');
  if (!el) return;
  let html = '<button class="btn btn-sm ' + (!currentCategory ? 'btn-primary' : '') + '" onclick="filterHelmCategory(\'\')">' + esc(t('helm.allCategories')) + '</button>';
  categories.forEach(function (c) {
    html += ' <button class="btn btn-sm ' + (currentCategory === c ? 'btn-primary' : '') + '" onclick="filterHelmCategory(\'' + escAttr(c) + '\')">' + esc(t('helm.category.' + c) || c) + '</button>';
  });
  el.innerHTML = html;
}

// renderHelmCatalogItems 渲染应用卡片网格。
function renderHelmCatalogItems(items) {
  const el = document.getElementById('helmCatalogGrid');
  if (!el) return;
  if (!items.length) {
    el.innerHTML = '<p class="muted">' + esc(t('helm.empty')) + '</p>';
    return;
  }
  let html = '<div class="helm-catalog-grid" style="display:grid;grid-template-columns:repeat(auto-fill,minmax(240px,1fr));gap:12px">';
  items.forEach(function (it) {
    const iconImg = it.icon
      ? '<img src="' + escAttr(it.icon) + '" alt="' + escAttr(it.name) + '" style="width:32px;height:32px;border-radius:4px;object-fit:contain" onerror="this.style.display=\'none\'">'
      : '<span class="icon" style="font-size:24px">' + icon('app', 24) + '</span>';
    html += '<div class="card helm-app-card" style="padding:12px;cursor:pointer" onclick="showHelmInstallModal(\'' + escAttr(it.id) + '\')">'
      + '<div style="display:flex;align-items:center;gap:8px;margin-bottom:8px">'
      + iconImg
      + '<div style="flex:1;min-width:0">'
      + '<div style="font-weight:600;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">' + esc(it.name) + '</div>'
      + '<div class="muted" style="font-size:11px">' + esc(t('helm.category.' + it.category) || it.category) + ' · ' + esc(it.repo) + '</div>'
      + '</div></div>'
      + '<p class="field-hint" style="margin:0 0 8px 0;font-size:12px;line-height:1.4;max-height:54px;overflow:hidden">' + esc(it.description || '') + '</p>'
      + '<div style="display:flex;justify-content:space-between;align-items:center;font-size:11px">'
      + '<span class="badge info">v' + esc(it.version || 'latest') + '</span>'
      + '<span class="muted">' + esc(it.maintainer || '') + '</span>'
      + '</div></div>';
  });
  html += '</div>';
  el.innerHTML = html;
}

// filterHelmCategory 切换分类。
export function filterHelmCategory(cat) {
  currentCategory = cat || '';
  loadHelmCatalog();
}

// searchHelmCatalog 搜索应用（按 q 查询后端 catalog）。
export function searchHelmCatalog() {
  const q = (document.getElementById('helmSearchInput') || {}).value || '';
  if (!q.trim()) { loadHelmCatalog(); return; }
  api.getHelmCatalog(null, q).then(function (data) {
    renderHelmCatalogItems(data.items || []);
  }).catch(function (e) { api.apiFail('helmSearch', e); });
}

// ============================================================================
// Helm 安装弹窗
// ============================================================================

// showHelmInstallModal 显示安装弹窗。itemId 由 catalog 卡片 onclick 传入。
// 同时缓存 catalog item 到 pendingInstallItem，用于回填默认 values。
export function showHelmInstallModal(itemId) {
  // 先从 catalog 取 item 详情（含 defaultValues）。
  api.getHelmCatalog(null, itemId).then(function (data) {
    const items = (data.items || []).filter(function (x) { return x.id === itemId; });
    if (!items.length) { alert(t('helm.appNotFound')); return; }
    pendingInstallItem = items[0];
    const it = pendingInstallItem;
    // 回填表单。
    const nameEl = document.getElementById('helmInstallName');
    if (nameEl) nameEl.value = it.id;
    const chartEl = document.getElementById('helmInstallChart');
    if (chartEl) chartEl.value = it.repo + '/' + it.chart;
    const verEl = document.getElementById('helmInstallVersion');
    if (verEl) verEl.value = it.version || '';
    const nsEl = document.getElementById('helmInstallNamespace');
    if (nsEl) nsEl.value = 'default';
    const valuesEl = document.getElementById('helmInstallValues');
    if (valuesEl) valuesEl.value = it.defaultValues ? JSON.stringify(it.defaultValues, null, 2) : '';
    // 标题。
    const titleEl = document.getElementById('helmInstallTitle');
    if (titleEl) titleEl.textContent = t('helm.installTitle') + ' · ' + it.name;
    // 显示弹窗。
    const m = document.getElementById('helmInstallModal');
    if (m) m.classList.add('open');
  }).catch(function (e) { api.apiFail('helmInstallLoad', e); });
}

// closeHelmInstallModal 关闭安装弹窗。
export function closeHelmInstallModal() {
  const m = document.getElementById('helmInstallModal');
  if (m) m.classList.remove('open');
  pendingInstallItem = null;
}

// confirmHelmInstall 提交安装。
export function confirmHelmInstall() {
  const ns = (document.getElementById('helmInstallNamespace') || {}).value || '';
  const name = (document.getElementById('helmInstallName') || {}).value || '';
  const chart = (document.getElementById('helmInstallChart') || {}).value || '';
  const version = (document.getElementById('helmInstallVersion') || {}).value || '';
  const valuesStr = (document.getElementById('helmInstallValues') || {}).value || '';
  if (!ns || !name || !chart) {
    setHelmInstallMsg(t('helm.requiredFields'), false);
    return;
  }
  let values = null;
  if (valuesStr.trim()) {
    try { values = JSON.parse(valuesStr); }
    catch (e) { setHelmInstallMsg(t('helm.invalidJson'), false); return; }
  }
  // 拼装 chart 引用（含版本）。
  const chartRef = version ? chart + ' --version ' + version : chart;
  const body = { namespace: ns, name: name, chart: chartRef, values: values };
  api.installHelmRelease(body).then(function (res) {
    if (res.s === 201 && res.j) {
      setHelmInstallMsg(t('helm.installOk'), true);
      setTimeout(function () { closeHelmInstallModal(); loadHelmReleases(); }, 800);
    } else {
      const msg = (res.j && res.j.error) ? res.j.error : ('HTTP ' + res.s);
      setHelmInstallMsg(t('helm.installFail') + msg, false);
    }
  }).catch(function (e) { setHelmInstallMsg(t('helm.installFail') + e.message, false); });
}

// setHelmInstallMsg 设置安装弹窗消息。
function setHelmInstallMsg(msg, isOk) {
  const el = document.getElementById('helmInstallMsg');
  if (!el) return;
  el.textContent = msg;
  el.className = 'msg ' + (isOk ? 'ok' : 'err');
}

// ============================================================================
// Helm 仓库管理
// ============================================================================

// loadHelmRepos 加载仓库列表并渲染到 #helmRepoList。
export function loadHelmRepos() {
  api.getHelmRepos().then(function (data) {
    renderHelmRepos(data.repos || []);
  }).catch(function (e) { api.apiFail('helmRepos', e); });
}

// renderHelmRepos 渲染仓库列表。
function renderHelmRepos(repos) {
  const el = document.getElementById('helmRepoList');
  if (!el) return;
  if (!repos.length) {
    el.innerHTML = '<p class="muted">' + esc(t('helm.noRepos')) + '</p>';
    return;
  }
  let html = '<table class="data-table"><thead><tr>'
    + '<th>' + esc(t('helm.repoName')) + '</th>'
    + '<th>' + esc(t('helm.repoUrl')) + '</th>'
    + '<th>' + esc(t('helm.repoType')) + '</th>'
    + '<th>' + esc(t('common.actions')) + '</th>'
    + '</tr></thead><tbody>';
  repos.forEach(function (r) {
    html += '<tr>'
      + '<td><b>' + esc(r.name) + '</b></td>'
      + '<td><small>' + esc(r.url) + '</small></td>'
      + '<td><span class="badge info">' + esc(r.type || 'http') + '</span></td>'
      + '<td>'
      + '<button class="btn xs" onclick="viewHelmRepoCharts(\'' + escAttr(r.name) + '\')">' + icon('list', 14) + ' ' + esc(t('helm.viewCharts')) + '</button> '
      + '<button class="btn xs outline" onclick="deleteHelmRepoConfirm(\'' + escAttr(r.name) + '\')">' + icon('delete', 14) + ' ' + esc(t('common.delete')) + '</button>'
      + '</td></tr>';
  });
  html += '</tbody></table>';
  el.innerHTML = html;
}

// viewHelmRepoCharts 查看仓库内 chart 列表。
export function viewHelmRepoCharts(name) {
  api.getHelmRepoCharts(name).then(function (data) {
    const charts = data.charts || [];
    let html = '<h4>' + esc(t('helm.chartsInRepo')) + ': ' + esc(name) + '</h4>';
    if (!charts.length) {
      html += '<p class="muted">' + esc(t('helm.noCharts')) + '</p>';
    } else {
      html += '<table class="data-table"><thead><tr>'
        + '<th>' + esc(t('helm.chartName')) + '</th>'
        + '<th>' + esc(t('helm.chartVersion')) + '</th>'
        + '<th>' + esc(t('helm.chartAppVersion')) + '</th>'
        + '<th>' + esc(t('common.description')) + '</th>'
        + '</tr></thead><tbody>';
      charts.forEach(function (c) {
        html += '<tr>'
          + '<td><b>' + esc(c.name) + '</b></td>'
          + '<td>' + esc(c.version || '') + '</td>'
          + '<td>' + esc(c.appVersion || '') + '</td>'
          + '<td><small class="muted">' + esc(c.description || '') + '</small></td>'
          + '</tr>';
      });
      html += '</tbody></table>';
    }
    // 用安装弹窗复用为通用弹窗（简化实现）。
    const titleEl = document.getElementById('helmInstallTitle');
    if (titleEl) titleEl.textContent = t('helm.chartsInRepo') + ': ' + name;
    const valuesEl = document.getElementById('helmInstallValues');
    if (valuesEl) valuesEl.style.display = 'none';
    // 直接 alert 展示（简化）；生产可改为独立弹窗。
    alert(html.replace(/<[^>]+>/g, '\n').replace(/\n+/g, '\n').trim());
  }).catch(function (e) { api.apiFail('helmRepoCharts', e); });
}

// showAddHelmRepoModal 显示添加仓库弹窗。
export function showAddHelmRepoModal() {
  const m = document.getElementById('helmAddRepoModal');
  if (m) m.classList.add('open');
}

// closeAddHelmRepoModal 关闭添加仓库弹窗。
export function closeAddHelmRepoModal() {
  const m = document.getElementById('helmAddRepoModal');
  if (m) m.classList.remove('open');
}

// confirmAddHelmRepo 提交添加仓库。
export function confirmAddHelmRepo() {
  const name = (document.getElementById('helmAddRepoName') || {}).value || '';
  const url = (document.getElementById('helmAddRepoUrl') || {}).value || '';
  const type = (document.getElementById('helmAddRepoType') || {}).value || 'http';
  if (!name || !url) {
    setHelmAddRepoMsg(t('helm.repoRequired'), false);
    return;
  }
  api.addHelmRepo({ name: name, url: url, type: type }).then(function (res) {
    if (res.s === 201) {
      setHelmAddRepoMsg(t('helm.repoAddOk'), true);
      setTimeout(function () { closeAddHelmRepoModal(); loadHelmRepos(); }, 600);
    } else {
      const msg = (res.j && res.j.error) ? res.j.error : ('HTTP ' + res.s);
      setHelmAddRepoMsg(t('helm.repoAddFail') + msg, false);
    }
  }).catch(function (e) { setHelmAddRepoMsg(t('helm.repoAddFail') + e.message, false); });
}

// setHelmAddRepoMsg 设置添加仓库弹窗消息。
function setHelmAddRepoMsg(msg, isOk) {
  const el = document.getElementById('helmAddRepoMsg');
  if (!el) return;
  el.textContent = msg;
  el.className = 'msg ' + (isOk ? 'ok' : 'err');
}

// deleteHelmRepoConfirm 删除仓库确认。
export function deleteHelmRepoConfirm(name) {
  if (!confirm(t('helm.confirmDeleteRepo') + ': ' + name + '?')) return;
  api.deleteHelmRepo(name).then(function (res) {
    if (res.s === 200) {
      loadHelmRepos();
    } else {
      alert(t('helm.repoDeleteFail') + ' (HTTP ' + res.s + ')');
    }
  }).catch(function (e) { alert(t('helm.repoDeleteFail') + ': ' + e.message); });
}

// ============================================================================
// Helm Release 管理
// ============================================================================

// 当前 release 列表查询的 namespace。
let releaseNamespace = '';

// loadHelmReleases 加载 release 列表并渲染到 #helmReleaseList。
export function loadHelmReleases() {
  api.getHelmReleases(releaseNamespace || null).then(function (data) {
    renderHelmReleases(data.releases || []);
  }).catch(function (e) { api.apiFail('helmReleases', e); });
}

// onHelmReleaseNamespaceKeyDown 处理 namespace 输入回车。
export function onHelmReleaseNamespaceKeyDown(ev) {
  if (ev.key === 'Enter') {
    releaseNamespace = (document.getElementById('helmReleaseNamespaceInput') || {}).value || '';
    loadHelmReleases();
  }
}

// renderHelmReleases 渲染 release 列表。
function renderHelmReleases(releases) {
  const el = document.getElementById('helmReleaseList');
  if (!el) return;
  if (!releases.length) {
    el.innerHTML = '<p class="muted">' + esc(t('helm.noReleases')) + '</p>';
    return;
  }
  let html = '<table class="data-table"><thead><tr>'
    + '<th>' + esc(t('helm.relName')) + '</th>'
    + '<th>' + esc(t('helm.relNamespace')) + '</th>'
    + '<th>' + esc(t('helm.relChart')) + '</th>'
    + '<th>' + esc(t('helm.relVersion')) + '</th>'
    + '<th>' + esc(t('common.status')) + '</th>'
    + '<th>' + esc(t('common.updatedAt')) + '</th>'
    + '<th>' + esc(t('common.actions')) + '</th>'
    + '</tr></thead><tbody>';
  releases.forEach(function (r) {
    const statusBadge = r.status === 'deployed' ? '<span class="badge ok">deployed</span>'
      : r.status === 'failed' ? '<span class="badge fail">failed</span>'
        : '<span class="badge warn">' + esc(r.status || '') + '</span>';
    html += '<tr>'
      + '<td><b>' + esc(r.name) + '</b></td>'
      + '<td>' + esc(r.namespace) + '</td>'
      + '<td>' + esc(r.chart) + '</td>'
      + '<td>v' + esc(r.version || '') + ' <small class="muted">(rev ' + (r.revision || 0) + ')</small></td>'
      + '<td>' + statusBadge + '</td>'
      + '<td><small class="muted">' + esc(fmtTime(r.updatedAt || r.createdAt)) + '</small></td>'
      + '<td>'
      + '<button class="btn xs" onclick="upgradeHelmReleasePrompt(\'' + escAttr(r.name) + '\',\'' + escAttr(r.namespace) + '\',\'' + escAttr(r.chart) + '\')">' + icon('upgrade', 14) + ' ' + esc(t('helm.upgrade')) + '</button> '
      + '<button class="btn xs" onclick="rollbackHelmReleasePrompt(\'' + escAttr(r.name) + '\',\'' + escAttr(r.namespace) + '\')">' + icon('rollback', 14) + ' ' + esc(t('helm.rollback')) + '</button> '
      + '<button class="btn xs" onclick="viewHelmReleaseHistory(\'' + escAttr(r.name) + '\',\'' + escAttr(r.namespace) + '\')">' + icon('history', 14) + ' ' + esc(t('helm.history')) + '</button> '
      + '<button class="btn xs outline" onclick="uninstallHelmReleaseConfirm(\'' + escAttr(r.name) + '\',\'' + escAttr(r.namespace) + '\')">' + icon('delete', 14) + ' ' + esc(t('helm.uninstall')) + '</button>'
      + '</td></tr>';
  });
  html += '</tbody></table>';
  el.innerHTML = html;
}

// upgradeHelmReleasePrompt 升级 release 弹窗。
export function upgradeHelmReleasePrompt(name, namespace, chart) {
  const chartRef = prompt(t('helm.upgradeChartPrompt') + ':', chart);
  if (chartRef === null) return;
  if (!chartRef) { alert(t('helm.chartRequired')); return; }
  const valuesStr = prompt(t('helm.upgradeValuesPrompt') + ' (JSON):', '');
  let values = null;
  if (valuesStr && valuesStr.trim()) {
    try { values = JSON.parse(valuesStr); }
    catch (e) { alert(t('helm.invalidJson')); return; }
  }
  api.upgradeHelmRelease(name, { namespace: namespace, chart: chartRef, values: values }).then(function (res) {
    if (res.s === 200) {
      loadHelmReleases();
    } else {
      const msg = (res.j && res.j.error) ? res.j.error : ('HTTP ' + res.s);
      alert(t('helm.upgradeFail') + ': ' + msg);
    }
  }).catch(function (e) { alert(t('helm.upgradeFail') + ': ' + e.message); });
}

// rollbackHelmReleasePrompt 回滚 release 弹窗。
export function rollbackHelmReleasePrompt(name, namespace) {
  const revStr = prompt(t('helm.rollbackRevPrompt') + ':', '0');
  if (revStr === null) return;
  const revision = parseInt(revStr, 10) || 0;
  api.rollbackHelmRelease(name, { namespace: namespace, revision: revision }).then(function (res) {
    if (res.s === 200) {
      loadHelmReleases();
    } else {
      const msg = (res.j && res.j.error) ? res.j.error : ('HTTP ' + res.s);
      alert(t('helm.rollbackFail') + ': ' + msg);
    }
  }).catch(function (e) { alert(t('helm.rollbackFail') + ': ' + e.message); });
}

// viewHelmReleaseHistory 查看 release 历史。
export function viewHelmReleaseHistory(name, namespace) {
  api.getHelmReleaseHistory(name, namespace).then(function (data) {
    const history = data.history || [];
    let msg = t('helm.historyTitle') + ': ' + name + '\n\n';
    if (!history.length) {
      msg += t('helm.noHistory');
    } else {
      history.forEach(function (h) {
        msg += 'rev ' + h.revision + ' · ' + h.status + ' · ' + (h.chart || '') + ' v' + (h.version || '') + ' · ' + fmtTime(h.updatedAt) + '\n  ' + (h.description || '') + '\n';
      });
    }
    alert(msg);
  }).catch(function (e) { alert(t('helm.historyFail') + ': ' + e.message); });
}

// uninstallHelmReleaseConfirm 卸载 release 确认。
export function uninstallHelmReleaseConfirm(name, namespace) {
  if (!confirm(t('helm.confirmUninstall') + ': ' + name + '?')) return;
  api.uninstallHelmRelease(name, namespace).then(function (res) {
    if (res.s === 200) {
      loadHelmReleases();
    } else {
      alert(t('helm.uninstallFail') + ' (HTTP ' + res.s + ')');
    }
  }).catch(function (e) { alert(t('helm.uninstallFail') + ': ' + e.message); });
}

// ============================================================================
// 页面加载入口
// ============================================================================

// loadHelmPage 加载 Helm 应用商店页面（catalog + repos + releases）。
export function loadHelmPage() {
  loadHelmCatalog();
  loadHelmRepos();
  loadHelmReleases();
}