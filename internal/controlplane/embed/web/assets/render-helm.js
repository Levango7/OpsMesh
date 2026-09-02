// render-helm.js — Helm 应用商店渲染（P3 补齐功能域）。

// 渲染子模块 — Helm 应用商店（仓库管理 + 应用目录 + Release 管理）。
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
// Helm 应用商店渲染
// ============================================================================

// helmReleaseStatusBadge Helm Release 状态 badge。
function helmReleaseStatusBadge(status) {
  const s = String(status || '').toLowerCase();
  if (s === 'deployed' || s === 'running' || s === 'ok') return badge(t('helm.status.deployed'), 'status-resolved');
  if (s === 'failed' || s === 'error') return badge(t('helm.status.failed'), 'status-closed');
  if (s === 'pending' || s === 'installing') return badge(t('helm.status.pending'), 'status-in_progress');
  return badge(status || '-', 'status-in_progress');
}

// renderHelmReposTable 渲染 Helm 仓库列表表格。
// repos: [{name, url, username, createdAt}]
// handlers: { onDelete(name) }
export function renderHelmReposTable(container, repos, handlers) {
  container.innerHTML = '';
  if (!repos || repos.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('helm.repoName') }),
        el('th', { text: t('helm.repoURL') }),
        el('th', { text: t('common.createdAt') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      repos.map((r) => el('tr', null,
        el('td', { class: 'cell-title', text: r.name || '-' }),
        el('td', { class: 'mono', text: r.url || '-' }),
        el('td', { class: 'mono', text: formatTime(r.createdAt) }),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn-icon btn-icon-danger', title: t('common.delete'), onclick: () => handlers.onDelete(r.name) }, iconEl('trash', 14))
        )
      ))
    )
  );
  container.appendChild(table);
}

// renderHelmRepoForm 渲染添加 Helm 仓库表单。
// handlers: { onSubmit(data), onCancel() }
export function renderHelmRepoForm(container, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const data = {
      name: form.elements.name.value.trim(),
      url: form.elements.url.value.trim(),
      username: form.elements.username.value.trim(),
      password: form.elements.password.value,
    };
    handlers.onSubmit(data);
  } });
  form.appendChild(el('h3', { class: 'form-title', text: t('helm.addRepo') }));
  // 仓库名称（必填）
  form.appendChild(fieldRow(t('helm.repoName'), true,
    el('input', { type: 'text', name: 'name', required: 'true', placeholder: t('helm.repoNamePlaceholder') })
  ));
  // 仓库 URL（必填）
  form.appendChild(fieldRow(t('helm.repoURL'), true,
    el('input', { type: 'text', name: 'url', required: 'true', placeholder: t('helm.repoURLPlaceholder') })
  ));
  // 用户名（可选）
  form.appendChild(fieldRow(t('helm.username'), false,
    el('input', { type: 'text', name: 'username', placeholder: t('helm.usernamePlaceholder') })
  ));
  // 密码（可选）
  form.appendChild(fieldRow(t('helm.password'), false,
    el('input', { type: 'password', name: 'password', placeholder: t('helm.passwordPlaceholder') })
  ));
  // 操作按钮
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('plus', 16), el('span', { text: t('common.save') })),
    el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => handlers.onCancel() }, el('span', { text: t('common.cancel') }))
  ));
  container.appendChild(form);
}

// renderHelmCatalog 渲染应用目录（预置分类列表）。
// categories: [{name, description, count}]
export function renderHelmCatalog(container, categories) {
  container.innerHTML = '';
  if (!categories || categories.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const grid = el('div', { class: 'overview-grid' });
  categories.forEach((c) => {
    grid.appendChild(el('div', { class: 'metric-card' },
      el('div', { class: 'metric-icon' }, iconEl('tag', 22)),
      el('div', { class: 'metric-body' },
        el('div', { class: 'metric-value', text: c.name || '-' }),
        el('div', { class: 'metric-label', text: (c.description || '') + ' (' + formatNumber(c.count || 0) + ')' })
      )
    ));
  });
  container.appendChild(grid);
}

// renderHelmChartsTable 渲染 Chart 搜索结果表格。
// charts: [{name, repo, version, description, home, sources}]
export function renderHelmChartsTable(container, charts) {
  container.innerHTML = '';
  if (!charts || charts.length === 0) {
    renderEmpty(container, t('helm.noCharts'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('helm.chartName') }),
        el('th', { text: t('helm.chartVersion') }),
        el('th', { text: t('helm.repoName') }),
        el('th', { text: t('common.description') })
      )
    ),
    el('tbody', null,
      charts.map((c) => el('tr', null,
        el('td', { class: 'cell-title', text: c.name || '-' }),
        el('td', { class: 'mono', text: c.version || '-' }),
        el('td', { text: c.repo || '-' }),
        el('td', { text: c.description || '-' })
      ))
    )
  );
  container.appendChild(table);
}

// renderHelmChartsSearchForm 渲染 Chart 搜索表单。
// handlers: { onSearch(q) }
export function renderHelmChartsSearchForm(container, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const q = form.elements.q.value.trim();
    handlers.onSearch(q);
  } });
  form.appendChild(el('h3', { class: 'form-title', text: t('helm.searchCharts') }));
  form.appendChild(fieldRow(t('common.search'), true,
    el('input', { type: 'text', name: 'q', placeholder: t('helm.searchPlaceholder') })
  ));
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('search', 16), el('span', { text: t('common.search') }))
  ));
  container.appendChild(form);
}

// renderHelmReleasesTable 渲染 Helm Release 列表表格。
// releases: [{name, namespace, chart, version, status, updated}]
// handlers: { onUninstall(name) }
export function renderHelmReleasesTable(container, releases, handlers) {
  container.innerHTML = '';
  if (!releases || releases.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('helm.releaseName') }),
        el('th', { text: t('helm.namespace') }),
        el('th', { text: t('helm.chartName') }),
        el('th', { text: t('helm.chartVersion') }),
        el('th', { text: t('common.status') }),
        el('th', { text: t('common.updatedAt') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      releases.map((r) => el('tr', null,
        el('td', { class: 'cell-title', text: r.name || '-' }),
        el('td', { class: 'mono', text: r.namespace || '-' }),
        el('td', { text: r.chart || '-' }),
        el('td', { class: 'mono', text: r.version || '-' }),
        el('td', null, helmReleaseStatusBadge(r.status)),
        el('td', { class: 'mono', text: formatTime(r.updated) }),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn-icon btn-icon-danger', title: t('helm.uninstall'), onclick: () => handlers.onUninstall(r.name) }, iconEl('trash', 14))
        )
      ))
    )
  );
  container.appendChild(table);
}

// renderHelmReleaseForm 渲染安装 Helm Release 表单。
// repos: [String] 可选仓库列表（用于 select）
// handlers: { onSubmit(data), onCancel() }
export function renderHelmReleaseForm(container, repos, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const data = {
      name: form.elements.name.value.trim(),
      chart: form.elements.chart.value.trim(),
      namespace: form.elements.namespace.value.trim(),
      values: parseValues(form.elements.values.value),
      repo: form.elements.repo.value,
    };
    handlers.onSubmit(data);
  } });
  form.appendChild(el('h3', { class: 'form-title', text: t('helm.installRelease') }));
  // Release 名称（必填）
  form.appendChild(fieldRow(t('helm.releaseName'), true,
    el('input', { type: 'text', name: 'name', required: 'true', placeholder: t('helm.releaseNamePlaceholder') })
  ));
  // Chart 名称（必填）
  form.appendChild(fieldRow(t('helm.chartName'), true,
    el('input', { type: 'text', name: 'chart', required: 'true', placeholder: t('helm.chartNamePlaceholder') })
  ));
  // 命名空间
  form.appendChild(fieldRow(t('helm.namespace'), false,
    el('input', { type: 'text', name: 'namespace', value: 'default', placeholder: t('helm.namespacePlaceholder') })
  ));
  // 仓库（下拉选择）
  const repoSelect = el('select', { name: 'repo' },
    el('option', { value: '', text: t('helm.selectRepo') })
  );
  (repos || []).forEach((r) => {
    const name = (typeof r === 'string') ? r : (r && r.name);
    if (name) repoSelect.appendChild(el('option', { value: name, text: name }));
  });
  form.appendChild(fieldRow(t('helm.repoName'), false, repoSelect));
  // values（JSON）
  form.appendChild(fieldRow(t('helm.values'), false,
    el('textarea', { name: 'values', rows: '4', placeholder: '{"key":"value"}' }, '')
  ));
  // 操作按钮
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('rocket', 16), el('span', { text: t('helm.install') })),
    el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => handlers.onCancel() }, el('span', { text: t('common.cancel') }))
  ));
  container.appendChild(form);
}

// parseValues 解析 values JSON（解析失败保留原字符串）。
function parseValues(text) {
  const raw = (text || '').trim();
  if (!raw) return null;
  try { return JSON.parse(raw); } catch (_) { return raw; }
}