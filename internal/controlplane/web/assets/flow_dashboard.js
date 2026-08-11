// flow_dashboard.js — task 242 M3 集成：集群监控仪表盘页面交互逻辑
//
// 职责：
//   - loadClusterDashboardPage：加载集群仪表盘页面（集群选择器 + dashboard 汇总）
//   - renderClusterDashboard：渲染 CPU/内存/存储 圆环图 + 节点/Pod/Deployment 状态
//   - loadClusterHealth：渲染集群健康检查
//   - viewNodeMetrics：查看节点指标
//
// 依赖：api.js、render.js（esc/escAttr/fmtBytes）、icons.js、i18n.js

import * as api from './api.js';
import { esc, escAttr, fmtBytes } from './render.js';
import { icon } from './icons.js';
import { t } from './i18n.js';

// 当前选中的集群 ID。
let dashboardClusterID = '';

// ============================================================================
// 集群仪表盘页面入口
// ============================================================================

// loadClusterDashboardPage 加载集群仪表盘页面。
// 流程：先加载集群列表填充选择器，再加载首个集群的 dashboard。
export function loadClusterDashboardPage() {
  api.getK8sClusters().then(function (clusters) {
    renderDashboardClusterSelect(clusters || []);
    // 默认选第一个集群。
    if (clusters && clusters.length) {
      dashboardClusterID = clusters[0].id;
      const sel = document.getElementById('dashboardClusterSelect');
      if (sel) sel.value = dashboardClusterID;
      loadClusterDashboard();
    } else {
      const el = document.getElementById('clusterDashboardContent');
      if (el) el.innerHTML = '<p class="muted">' + esc(t('dashboard.noClusters')) + '</p>';
    }
  }).catch(function (e) { api.apiFail('dashboardClusters', e); });
}

// renderDashboardClusterSelect 渲染集群选择器。
function renderDashboardClusterSelect(clusters) {
  const sel = document.getElementById('dashboardClusterSelect');
  if (!sel) return;
  let html = '<option value="">— ' + esc(t('dashboard.selectCluster')) + ' —</option>';
  clusters.forEach(function (c) {
    html += '<option value="' + escAttr(c.id) + '">' + esc(c.name) + '</option>';
  });
  sel.innerHTML = html;
}

// onDashboardClusterSelectChange 集群选择变更。
export function onDashboardClusterSelectChange() {
  const sel = document.getElementById('dashboardClusterSelect');
  dashboardClusterID = sel ? sel.value : '';
  if (dashboardClusterID) {
    loadClusterDashboard();
  } else {
    const el = document.getElementById('clusterDashboardContent');
    if (el) el.innerHTML = '';
  }
}

// loadClusterDashboard 加载并渲染集群仪表盘汇总。
export function loadClusterDashboard() {
  if (!dashboardClusterID) return;
  const content = document.getElementById('clusterDashboardContent');
  if (content) content.innerHTML = '<p class="muted">' + esc(t('common.loading')) + '</p>';
  api.getK8sClusterDashboard(dashboardClusterID).then(function (dash) {
    renderClusterDashboard(dash || {});
  }).catch(function (e) {
    if (content) content.innerHTML = '<p class="err">' + esc(t('dashboard.loadFail') + ': ' + e.message) + '</p>';
  });
}

// renderClusterDashboard 渲染仪表盘。
function renderClusterDashboard(dash) {
  const el = document.getElementById('clusterDashboardContent');
  if (!el) return;
  const nodes = dash.nodes || {};
  const pods = dash.pods || {};
  const deps = dash.deployments || {};
  const cpu = dash.cpu || {};
  const mem = dash.memory || {};
  const stor = dash.storage || {};

  let html = '<div class="row-single" style="margin-bottom:16px">';

  // ---- 资源使用率圆环图 ----
  html += '<div class="card" style="padding:16px;margin-bottom:12px">'
    + '<h4 style="margin:0 0 12px 0">' + esc(t('dashboard.resourceUsage')) + '</h4>'
    + '<div style="display:grid;grid-template-columns:repeat(3,1fr);gap:16px">'
    + renderUsageDonut('CPU', cpu.usagePercent || 0, (cpu.total || 0).toFixed(2) + ' cores', (cpu.used || 0).toFixed(2) + ' cores')
    + renderUsageDonut(t('dashboard.memory'), mem.usagePercent || 0, fmtBytes(mem.total || 0), fmtBytes(mem.used || 0))
    + renderUsageDonut(t('dashboard.storage'), stor.usagePercent || 0, fmtBytes(stor.total || 0), fmtBytes(stor.used || 0))
    + '</div></div>';

  // ---- 节点状态 ----
  html += '<div class="card" style="padding:16px;margin-bottom:12px">'
    + '<h4 style="margin:0 0 12px 0">' + esc(t('dashboard.nodes')) + ' <span class="muted" style="font-size:12px">(' + (nodes.total || 0) + ')</span></h4>'
    + '<div style="display:flex;gap:16px;flex-wrap:wrap">'
    + '<div><span class="badge ok">' + esc(t('dashboard.ready')) + '</span> <b>' + (nodes.ready || 0) + '</b></div>'
    + '<div><span class="badge fail">' + esc(t('dashboard.notReady')) + '</span> <b>' + (nodes.notReady || 0) + '</b></div>'
    + '</div></div>';

  // ---- Pod 状态分布 ----
  html += '<div class="card" style="padding:16px;margin-bottom:12px">'
    + '<h4 style="margin:0 0 12px 0">' + esc(t('dashboard.pods')) + ' <span class="muted" style="font-size:12px">(' + (pods.total || 0) + ')</span></h4>'
    + '<div style="display:flex;gap:16px;flex-wrap:wrap">'
    + '<div><span class="badge ok">Running</span> <b>' + (pods.running || 0) + '</b></div>'
    + '<div><span class="badge warn">Pending</span> <b>' + (pods.pending || 0) + '</b></div>'
    + '<div><span class="badge fail">Failed</span> <b>' + (pods.failed || 0) + '</b></div>'
    + '<div><span class="badge info">Succeeded</span> <b>' + (pods.succeeded || 0) + '</b></div>'
    + '</div></div>';

  // ---- Deployment 状态 ----
  html += '<div class="card" style="padding:16px;margin-bottom:12px">'
    + '<h4 style="margin:0 0 12px 0">' + esc(t('dashboard.deployments')) + ' <span class="muted" style="font-size:12px">(' + (deps.total || 0) + ')</span></h4>'
    + '<div style="display:flex;gap:16px;flex-wrap:wrap">'
    + '<div><span class="badge ok">' + esc(t('dashboard.available')) + '</span> <b>' + (deps.available || 0) + '</b></div>'
    + '<div><span class="badge fail">' + esc(t('dashboard.unavailable')) + '</span> <b>' + (deps.unavailable || 0) + '</b></div>'
    + '</div></div>';

  html += '</div>';

  // ---- 操作按钮 ----
  html += '<div style="margin-top:12px">'
    + '<button class="btn btn-sm" onclick="loadClusterDashboard()" style="margin-right:8px">' + icon('refresh', 14) + ' ' + esc(t('common.refresh')) + '</button>'
    + '<button class="btn btn-sm" onclick="loadClusterHealth()" style="margin-right:8px">' + icon('health', 14) + ' ' + esc(t('dashboard.healthCheck')) + '</button>'
    + '<button class="btn btn-sm" onclick="loadDashboardNodes()">' + icon('list', 14) + ' ' + esc(t('dashboard.nodeList')) + '</button>'
    + '</div>';

  // ---- 节点详情列表区 ----
  html += '<div id="dashboardNodeList" style="margin-top:16px"></div>';
  // ---- 健康检查结果区 ----
  html += '<div id="dashboardHealthResult" style="margin-top:16px"></div>';

  el.innerHTML = html;

  // 自动加载节点列表。
  loadDashboardNodes();
}

// renderUsageDonut 渲染单个使用率圆环图（SVG）。
// name: 资源名；percent: 使用率百分比；totalStr/usedStr: 文本展示。
function renderUsageDonut(name, percent, totalStr, usedStr) {
  const pct = Math.max(0, Math.min(100, percent));
  // SVG 圆环：r=40, 周长=2πr≈251.3。
  const r = 40;
  const circ = 2 * Math.PI * r;
  const offset = circ * (1 - pct / 100);
  const color = pct >= 80 ? 'var(--danger)' : pct >= 60 ? 'var(--warning)' : 'var(--success)';
  return '<div style="text-align:center">'
    + '<svg width="100" height="100" viewBox="0 0 100 100" style="margin:0 auto">'
    + '<circle cx="50" cy="50" r="' + r + '" fill="none" stroke="var(--bg-2)" stroke-width="8"/>'
    + '<circle cx="50" cy="50" r="' + r + '" fill="none" stroke="' + color + '" stroke-width="8" '
    + 'stroke-dasharray="' + circ.toFixed(1) + '" stroke-dashoffset="' + offset.toFixed(1) + '" '
    + 'transform="rotate(-90 50 50)" stroke-linecap="round"/>'
    + '<text x="50" y="50" text-anchor="middle" dominant-baseline="central" font-size="16" font-weight="600" fill="var(--text-1)">' + pct.toFixed(1) + '%</text>'
    + '</svg>'
    + '<div style="margin-top:6px;font-weight:600">' + esc(name) + '</div>'
    + '<div class="muted" style="font-size:11px">' + esc(usedStr + ' / ' + totalStr) + '</div>'
    + '</div>';
}

// ============================================================================
// 节点列表 + 节点指标
// ============================================================================

// loadDashboardNodes 加载并渲染节点列表。
export function loadDashboardNodes() {
  if (!dashboardClusterID) return;
  api.getK8sNodes(dashboardClusterID).then(function (data) {
    renderDashboardNodes(data.nodes || []);
  }).catch(function (e) {
    const el = document.getElementById('dashboardNodeList');
    if (el) el.innerHTML = '<p class="err">' + esc(t('dashboard.nodesLoadFail') + ': ' + e.message) + '</p>';
  });
}

// renderDashboardNodes 渲染节点列表。
function renderDashboardNodes(nodes) {
  const el = document.getElementById('dashboardNodeList');
  if (!el) return;
  if (!nodes.length) {
    el.innerHTML = '<p class="muted">' + esc(t('dashboard.noNodes')) + '</p>';
    return;
  }
  let html = '<div class="card" style="padding:16px">'
    + '<h4 style="margin:0 0 12px 0">' + esc(t('dashboard.nodeList')) + '</h4>'
    + '<table class="data-table"><thead><tr>'
    + '<th>' + esc(t('dashboard.nodeName')) + '</th>'
    + '<th>' + esc(t('common.status')) + '</th>'
    + '<th>' + esc(t('dashboard.roles')) + '</th>'
    + '<th>' + esc(t('dashboard.version')) + '</th>'
    + '<th>' + esc(t('dashboard.internalIP')) + '</th>'
    + '<th>' + esc(t('dashboard.cpu')) + '</th>'
    + '<th>' + esc(t('dashboard.memory')) + '</th>'
    + '<th>' + esc(t('common.actions')) + '</th>'
    + '</tr></thead><tbody>';
  nodes.forEach(function (n) {
    const statusBadge = n.status === 'Ready'
      ? '<span class="badge ok">Ready</span>'
      : '<span class="badge fail">NotReady</span>';
    const roles = (n.roles || []).join(', ');
    html += '<tr>'
      + '<td><b>' + esc(n.name) + '</b></td>'
      + '<td>' + statusBadge + '</td>'
      + '<td><small>' + esc(roles) + '</small></td>'
      + '<td><small class="muted">' + esc(n.version || '') + '</small></td>'
      + '<td>' + esc(n.internalIP || '') + '</td>'
      + '<td>' + esc(n.cpu || '') + '</td>'
      + '<td>' + esc(fmtBytes(parseMemBytes(n.memory || '0'))) + '</td>'
      + '<td><button class="btn xs" onclick="viewNodeMetrics(\'' + escAttr(n.name) + '\')">' + icon('metrics', 14) + ' ' + esc(t('dashboard.metrics')) + '</button></td>'
      + '</tr>';
  });
  html += '</tbody></table></div>';
  el.innerHTML = html;
}

// parseMemBytes 将 K8s 内存字符串（如 "8157Mi"）解析为字节数。
function parseMemBytes(s) {
  if (!s) return 0;
  // 简化处理：支持 Ki/Mi/Gi/Ti 与 K/M/G/T。
  const m = /^(\d+(?:\.\d+)?)(Ki|Mi|Gi|Ti|K|M|G|T)?$/.exec(s);
  if (!m) return 0;
  const v = parseFloat(m[1]);
  switch (m[2]) {
    case 'Ki': return v * 1024;
    case 'Mi': return v * 1024 * 1024;
    case 'Gi': return v * 1024 * 1024 * 1024;
    case 'Ti': return v * 1024 * 1024 * 1024 * 1024;
    case 'K': return v * 1000;
    case 'M': return v * 1000 * 1000;
    case 'G': return v * 1000 * 1000 * 1000;
    case 'T': return v * 1000 * 1000 * 1000 * 1000;
    default: return v;
  }
}

// viewNodeMetrics 查看节点指标。
export function viewNodeMetrics(nodeName) {
  if (!dashboardClusterID) return;
  api.getK8sNodeMetrics(dashboardClusterID, nodeName).then(function (m) {
    const cpu = m.cpu || {};
    const mem = m.memory || {};
    let msg = t('dashboard.nodeMetricsTitle') + ': ' + (m.name || nodeName) + '\n\n';
    msg += 'Roles: ' + ((m.roles || []).join(', ')) + '\n';
    msg += 'CPU: ' + (cpu.used || 0).toFixed(2) + ' / ' + (cpu.total || 0).toFixed(2) + ' cores (' + (cpu.usagePercent || 0) + '%)\n';
    msg += 'Memory: ' + fmtBytes(mem.used || 0) + ' / ' + fmtBytes(mem.total || 0) + ' (' + (mem.usagePercent || 0) + '%)\n';
    alert(msg);
  }).catch(function (e) { alert(t('dashboard.metricsLoadFail') + ': ' + e.message); });
}

// ============================================================================
// 集群健康检查
// ============================================================================

// loadClusterHealth 加载并渲染集群健康检查。
export function loadClusterHealth() {
  if (!dashboardClusterID) return;
  const el = document.getElementById('dashboardHealthResult');
  if (el) el.innerHTML = '<p class="muted">' + esc(t('common.loading')) + '</p>';
  api.getK8sClusterHealth(dashboardClusterID).then(function (health) {
    renderClusterHealth(health || {});
  }).catch(function (e) {
    if (el) el.innerHTML = '<p class="err">' + esc(t('dashboard.healthLoadFail') + ': ' + e.message) + '</p>';
  });
}

// renderClusterHealth 渲染健康检查结果。
function renderClusterHealth(health) {
  const el = document.getElementById('dashboardHealthResult');
  if (!el) return;
  const statusBadge = health.status === 'healthy' ? '<span class="badge ok">' + esc(t('dashboard.healthy')) + '</span>'
    : health.status === 'degraded' ? '<span class="badge warn">' + esc(t('dashboard.degraded')) + '</span>'
      : '<span class="badge fail">' + esc(t('dashboard.unhealthy')) + '</span>';
  let html = '<div class="card" style="padding:16px">'
    + '<h4 style="margin:0 0 12px 0">' + esc(t('dashboard.healthCheck')) + ' ' + statusBadge + '</h4>';

  // 检查项。
  if (health.checks && health.checks.length) {
    html += '<h5 style="margin:8px 0 4px 0">' + esc(t('dashboard.checks')) + '</h5><ul>';
    health.checks.forEach(function (c) {
      const icon_ = c.passed ? '✅' : '❌';
      html += '<li>' + icon_ + ' <b>' + esc(c.name) + '</b>: <small class="muted">' + esc(c.message || '') + '</small></li>';
    });
    html += '</ul>';
  }

  // 控制面组件。
  if (health.components && health.components.length) {
    html += '<h5 style="margin:8px 0 4px 0">' + esc(t('dashboard.components')) + '</h5><ul>';
    health.components.forEach(function (c) {
      const icon_ = c.healthy ? '✅' : '❌';
      html += '<li>' + icon_ + ' <b>' + esc(c.name) + '</b>: <small class="muted">' + esc(c.message || '') + '</small></li>';
    });
    html += '</ul>';
  }

  html += '</div>';
  el.innerHTML = html;
}