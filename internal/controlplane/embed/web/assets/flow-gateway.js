// flow-gateway.js — API 网关编排（由 flow.js 拆分）。

// flow 子模块 — 由 flow.js 拆分而来。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, $ } from './flow-state.js';

// ============================================================================
// Phase 5：扩展能力（API 网关 / Webhook / 自定义脚本）
// ============================================================================

// --- API 网关 ---

function gatewayContent() { return $('gateway-content'); }

// loadGatewayRoutes 加载网关路由列表（含子 tab 切换：路由列表 / 统计）。
export async function loadGatewayRoutes() {
  const content = gatewayContent();
  if (!content) return;
  content.innerHTML = '';
  // 子 tab 切换条
  const subTabs = [
    { key: 'routes', label: t('gateway.routes'), onclick: () => { state.gatewaySubTab = 'routes'; refreshGatewaySubTab(); } },
    { key: 'stats',  label: t('gateway.stats'),  onclick: () => { state.gatewaySubTab = 'stats';  refreshGatewaySubTab(); } },
  ];
  const subBar = render.el('div', { class: 'toolbar' });
  subTabs.forEach((s) => {
    subBar.appendChild(render.el('button', {
      class: 'btn ' + (state.gatewaySubTab === s.key ? 'btn-secondary' : 'btn-ghost'),
      onclick: s.onclick,
    }, render.el('span', { text: s.label })));
  });
  content.appendChild(subBar);
  const host = render.el('div', { class: 'content', style: { marginTop: '1rem' } });
  content.appendChild(host);
  if (state.gatewaySubTab === 'stats') { loadGatewayStats(); return; }
  // 路由列表
  const listHost = render.el('div', { id: 'gateway-routes-list' });
  host.appendChild(listHost);
  render.renderLoading(listHost);
  try {
    const routes = await api.getGatewayRoutes();
    state.gatewayRoutes = routes;
    render.renderGatewayRoutesTable(listHost, routes, {
      onEdit: (r) => editGatewayRoute(r),
      onToggle: (r, action) => toggleGatewayRoute(r.id || r.name, action),
      onDelete: (r) => deleteGatewayRoute(r.id || r.name),
    });
  } catch (err) {
    render.renderError(listHost, t('gateway.routesLoadFailed') + ': ' + err.message);
  }
  // 创建路由表单
  const formHost = render.el('div', { id: 'gateway-route-form', style: { marginTop: '1rem' } });
  host.appendChild(formHost);
  render.renderGatewayRouteForm(formHost, null, { onSubmit: (data) => createGatewayRoute(data) });
}

// loadGatewayStats 加载网关统计。
export async function loadGatewayStats() {
  const host = render.el('div', { id: 'gateway-stats-host', style: { marginTop: '1rem' } });
  const content = gatewayContent();
  if (!content) return;
  content.appendChild(host);
  render.renderLoading(host);
  try {
    const stats = await api.getGatewayStats();
    state.gatewayStats = stats;
    render.renderGatewayStats(host, stats);
  } catch (err) {
    render.renderError(host, t('gateway.routesLoadFailed') + ': ' + err.message);
  }
}

// createGatewayRoute 创建网关路由。
export async function createGatewayRoute(data) {
  if (!data || !data.name) { render.renderToast(t('gateway.nameRequired'), 'warn'); return; }
  if (!data.path) { render.renderToast(t('gateway.pathRequired'), 'warn'); return; }
  if (!data.target) { render.renderToast(t('gateway.targetRequired'), 'warn'); return; }
  try {
    await api.createGatewayRoute(data);
    render.renderToast(t('gateway.routeCreated'), 'success');
    state.gatewaySubTab = 'routes';
    loadGatewayRoutes();
  } catch (err) {
    render.renderToast(t('gateway.routeCreateFailed') + ': ' + err.message, 'error');
  }
}

// editGatewayRoute 编辑网关路由。
export async function editGatewayRoute(route) {
  if (!route) return;
  state.gatewayEditingRoute = route;
  const content = gatewayContent();
  if (!content) return;
  content.innerHTML = '';
  // 返回按钮
  const backBar = render.el('div', { class: 'toolbar' });
  backBar.appendChild(render.el('button', {
    class: 'btn btn-ghost',
    onclick: () => { state.gatewayEditingRoute = null; state.gatewaySubTab = 'routes'; loadGatewayRoutes(); },
  }, iconEl('back', 14), render.el('span', { text: t('gateway.routes') })));
  content.appendChild(backBar);
  const host = render.el('div', { class: 'content', style: { marginTop: '1rem' } });
  content.appendChild(host);
  // 先尝试获取详情，失败则用列表数据
  let detail = route;
  try {
    detail = await api.getGatewayRoute(route.id || route.name);
  } catch (_) { /* 用列表数据 */ }
  render.renderGatewayRouteForm(host, detail, { onSubmit: (data) => updateGatewayRoute(route.id || route.name, data) });
}

// updateGatewayRoute 更新网关路由。
export async function updateGatewayRoute(id, data) {
  if (!id) return;
  if (!data || !data.name) { render.renderToast(t('gateway.nameRequired'), 'warn'); return; }
  try {
    await api.updateGatewayRoute(id, data);
    render.renderToast(t('gateway.routeUpdated'), 'success');
    state.gatewayEditingRoute = null;
    state.gatewaySubTab = 'routes';
    loadGatewayRoutes();
  } catch (err) {
    render.renderToast(t('gateway.routeUpdateFailed') + ': ' + err.message, 'error');
  }
}

// deleteGatewayRoute 删除网关路由。
export async function deleteGatewayRoute(id) {
  if (!id) return;
  if (!window.confirm(t('gateway.deleteConfirm'))) return;
  try {
    await api.deleteGatewayRoute(id);
    render.renderToast(t('gateway.routeDeleted'), 'success');
    loadGatewayRoutes();
  } catch (err) {
    render.renderToast(t('gateway.routeDeleteFailed') + ': ' + err.message, 'error');
  }
}

// toggleGatewayRoute 启用/禁用网关路由。
export async function toggleGatewayRoute(id, action) {
  if (!id) return;
  const act = action === 'disable' ? 'disable' : 'enable';
  try {
    await api.toggleGatewayRoute(id, act);
    render.renderToast(act === 'enable' ? t('gateway.routeEnabledOk') : t('gateway.routeDisabledOk'), 'success');
    loadGatewayRoutes();
  } catch (err) {
    render.renderToast((act === 'enable' ? t('gateway.routeEnableFailed') : t('gateway.routeDisableFailed')) + ': ' + err.message, 'error');
  }
}

// buildGatewayToolbar 构建网关工具栏。
export function buildGatewayToolbar() {
  const toolbar = $('gateway-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => refreshGatewaySubTab() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}

// refreshGatewaySubTab 根据当前子 tab 重新渲染网关页。
function refreshGatewaySubTab() {
  loadGatewayRoutes();
}

// --- Webhook ---

