// flow-dashboard.js — 监控仪表盘编排（由 flow.js 拆分）。

// flow 子模块 — 由 flow.js 拆分而来。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, $ } from './flow-state.js';

// ============================================================================
// 监控仪表盘
// ============================================================================

// loadDashboardAll 加载仪表盘全部数据（概览 + 指标）。
export async function loadDashboardAll() {
  state._dashboardLoaded = true;
  await Promise.all([loadDashboardOverview(), loadMetrics()]);
}

// loadDashboardOverview 加载概览卡片（从 /metrics 解析）。
export async function loadDashboardOverview() {
  const host = $('dashboard-overview');
  if (!host) return;
  try {
    const text = await api.getMetrics();
    state.metricsText = text;
    const overview = parseOverviewFromMetrics(text);
    state.dashboardOverview = overview;
    render.renderDashboardOverview(host, overview);
  } catch (err) {
    render.renderError(host, t('dashboard.metricsLoadFailed') + ': ' + err.message);
  }
}

// parseOverviewFromMetrics 从 Prometheus 文本解析概览数值。
function parseOverviewFromMetrics(text) {
  const overview = { devices: 0, tasks: 0, alerts: 0, openTickets: 0 };
  if (!text) return overview;
  const lines = text.split('\n');
  for (const line of lines) {
    if (!line || line.startsWith('#')) continue;
    const sp = line.lastIndexOf(' ');
    if (sp <= 0) continue;
    const name = line.slice(0, sp);
    const value = parseInt(line.slice(sp + 1), 10);
    if (isNaN(value)) continue;
    if (name === 'opsmesh_devices_total') overview.devices = value;
    else if (name === 'opsmesh_tasks_total') overview.tasks = value;
    else if (name === 'opsmesh_alerts_active') overview.alerts = value;
    else if (name === 'opsmesh_tickets_open') overview.openTickets = value;
  }
  return overview;
}

// loadMetrics 加载 Prometheus 指标文本展示。
export async function loadMetrics() {
  const host = $('dashboard-metrics');
  if (!host) return;
  render.renderLoading(host);
  try {
    const text = state.metricsText || await api.getMetrics();
    state.metricsText = text;
    render.renderMetricsText(host, text);
  } catch (err) {
    render.renderError(host, t('dashboard.metricsLoadFailed') + ': ' + err.message);
  }
}

// buildDashboardToolbar 构建仪表盘工具栏（刷新按钮）。
export function buildDashboardToolbar() {
  const toolbar = $('dashboard-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', onclick: () => loadDashboardAll() },
      iconEl('refresh', 14), render.el('span', { text: t('dashboard.refresh') })
    )
  );
}

