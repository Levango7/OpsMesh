// flow.js — OpsMesh Phase 1 前端编排层。
//
// 职责：调用 api.js 获取数据 → 调用 render.js 渲染 → 处理用户交互。
// 导出：
//   - init()：初始化（绑定 tab 切换 + 默认页加载）
//   - loadTickets(filter) / createTicket() / showTicketDetail(id) / editTicket(id) / closeTicket(id)
//   - loadSLOs() / createSLO() / showSLODetail(id) / editSLO(id) / deleteSLO(id)
//   - loadMetrics() / loadDashboardOverview()
//   - switchTab(tab)
//
// 设计要点：
//   - 单页多 tab，每个 tab 一个根容器（#page-tickets / #page-dashboard / #page-slo）；
//   - 工单/SLO 内部有 list / form / detail 三种视图，用状态变量记录；
//   - 错误统一 catch → renderToast + renderError；
//   - 工具栏（创建按钮 + 过滤器）由 buildToolbar 构建。

import * as api from './api.js';
import * as render from './render.js';
import { t, setLang as i18nSetLang } from './i18n.js';
import { iconEl } from './icons.js';

// ============================================================================
// UI 状态
// ============================================================================

const state = {
  currentTab: 'tickets',
  ticketFilter: { status: '', priority: '', category: '' },
  tickets: [],
  slos: [],
  metricsText: '',
  dashboardOverview: { devices: 0, tasks: 0, alerts: 0, openTickets: 0 },
};

// ============================================================================
// 通用辅助
// ============================================================================

// $(id) getElementById 简写。
function $(id) { return document.getElementById(id); }

// pageRoot(tab) 返回当前 tab 的根容器。
function pageRoot(tab) {
  return $('page-' + (tab || state.currentTab));
}

// contentArea 返回当前页内容区（toolbar 之外）。
function ticketsContent() { return $('tickets-content'); }
function sloContent() { return $('slo-content'); }

// ============================================================================
// Tab 切换
// ============================================================================

export function switchTab(tab) {
  if (tab !== 'tickets' && tab !== 'dashboard' && tab !== 'slo') return;
  state.currentTab = tab;
  // 更新 tab 按钮激活态
  document.querySelectorAll('.tab-btn').forEach((btn) => {
    btn.classList.toggle('tab-active', btn.dataset.tab === tab);
  });
  // 更新 page section 可见性
  ['tickets', 'dashboard', 'slo'].forEach((p) => {
    const root = $('page-' + p);
    if (root) root.classList.toggle('page-active', p === tab);
  });
  // 按需懒加载
  if (tab === 'tickets' && state.tickets.length === 0) loadTickets();
  if (tab === 'dashboard' && !state._dashboardLoaded) loadDashboardAll();
  if (tab === 'slo' && state.slos.length === 0) loadSLOs();
}

// ============================================================================
// 工单管理
// ============================================================================

// loadTickets 加载工单列表（带当前过滤器）。
export async function loadTickets(filter) {
  if (filter) state.ticketFilter = Object.assign(state.ticketFilter, filter);
  const content = ticketsContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const tickets = await api.getTickets(state.ticketFilter);
    state.tickets = tickets;
    render.renderTicketTable(content, tickets, {
      onEdit: (id) => editTicket(id),
      onClose: (id) => closeTicket(id),
    });
  } catch (err) {
    render.renderError(content, t('tickets.loadFailed') + ': ' + err.message);
  }
}

// createTicket 打开创建工单表单。
export function createTicket() {
  const content = ticketsContent();
  if (!content) return;
  render.renderTicketForm(content, null, {
    onSubmit: async (data) => {
      if (!data.title || !data.title.trim()) {
        render.renderToast(t('tickets.titleRequired'), 'warn');
        return;
      }
      try {
        await api.createTicket(data);
        render.renderToast(t('tickets.created'), 'success');
        loadTickets();
      } catch (err) {
        render.renderToast(t('tickets.createFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => loadTickets(),
  });
}

// editTicket 打开编辑工单表单（先拉详情）。
export async function editTicket(id) {
  const content = ticketsContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const ticket = await api.getTicket(id);
    render.renderTicketForm(content, ticket, {
      onSubmit: async (data) => {
        if (!data.title || !data.title.trim()) {
          render.renderToast(t('tickets.titleRequired'), 'warn');
          return;
        }
        try {
          await api.updateTicket(id, data);
          render.renderToast(t('tickets.updated'), 'success');
          loadTickets();
        } catch (err) {
          render.renderToast(t('tickets.updateFailed') + ': ' + err.message, 'error');
        }
      },
      onCancel: () => loadTickets(),
    });
  } catch (err) {
    render.renderError(content, t('tickets.loadFailed') + ': ' + err.message);
  }
}

// showTicketDetail 查看工单详情。
export async function showTicketDetail(id) {
  const content = ticketsContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const ticket = await api.getTicket(id);
    render.renderTicketDetail(content, ticket, {
      onBack: () => loadTickets(),
      onEdit: () => editTicket(id),
      onClose: () => closeTicket(id),
    });
  } catch (err) {
    render.renderError(content, t('tickets.loadFailed') + ': ' + err.message);
  }
}

// closeTicket 关闭工单（确认后调用 API）。
export async function closeTicket(id) {
  if (!window.confirm(t('tickets.confirmClose'))) return;
  try {
    await api.closeTicket(id);
    render.renderToast(t('tickets.closed'), 'success');
    loadTickets();
  } catch (err) {
    render.renderToast(t('tickets.closeFailed') + ': ' + err.message, 'error');
  }
}

// buildTicketsToolbar 构建工单工具栏（创建按钮 + 过滤器）。
export function buildTicketsToolbar() {
  const toolbar = $('tickets-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  // 创建按钮
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-primary', onclick: () => createTicket() },
      iconEl('plus', 16), render.el('span', { text: t('tickets.create') })
    )
  );
  // 过滤器组
  const filterGroup = render.el('div', { class: 'filter-group' });
  // 状态过滤
  filterGroup.appendChild(buildFilterSelect('status', ['open', 'in_progress', 'resolved', 'closed'], (v) => loadTickets({ status: v })));
  // 优先级过滤
  filterGroup.appendChild(buildFilterSelect('priority', ['low', 'medium', 'high', 'urgent'], (v) => loadTickets({ priority: v })));
  // 分类过滤
  filterGroup.appendChild(buildFilterSelect('category', ['incident', 'change', 'request', 'problem'], (v) => loadTickets({ category: v })));
  // 刷新
  filterGroup.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => loadTickets() },
      iconEl('refresh', 14)
    )
  );
  toolbar.appendChild(filterGroup);
}

// buildFilterSelect 构建过滤下拉框。
function buildFilterSelect(kind, options, onChange) {
  const labelMap = {
    status: { label: t('tickets.filter.status'), prefix: 'ticket.status.' },
    priority: { label: t('tickets.filter.priority'), prefix: 'ticket.priority.' },
    category: { label: t('tickets.filter.category'), prefix: 'ticket.category.' },
  };
  const meta = labelMap[kind];
  const select = render.el('select', { class: 'filter-select', dataset: { filter: kind }, onchange: (e) => onChange(e.target.value) },
    render.el('option', { value: '', text: meta.label + ': ' + t('common.all') }),
    options.map((opt) => render.el('option', { value: opt, text: meta.label + ': ' + t(meta.prefix + opt) }))
  );
  return select;
}

// ============================================================================
// SLO 管理
// ============================================================================

// loadSLOs 加载 SLO 列表。
export async function loadSLOs() {
  const content = sloContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const slos = await api.getSLOs();
    state.slos = slos;
    render.renderSLOTable(content, slos, {
      onDetail: (id) => showSLODetail(id),
      onDelete: (id) => deleteSLO(id),
    });
  } catch (err) {
    render.renderError(content, t('slo.loadFailed') + ': ' + err.message);
  }
}

// createSLO 打开创建 SLO 表单。
export function createSLO() {
  const content = sloContent();
  if (!content) return;
  render.renderSLOForm(content, null, {
    onSubmit: async (data) => {
      if (!data.name || !data.name.trim()) {
        render.renderToast(t('slo.nameRequired'), 'warn');
        return;
      }
      try {
        await api.createSLO(data);
        render.renderToast(t('slo.created'), 'success');
        loadSLOs();
      } catch (err) {
        render.renderToast(t('slo.createFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => loadSLOs(),
  });
}

// editSLO 打开编辑 SLO 表单（先拉详情）。
export async function editSLO(id) {
  const content = sloContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const slo = await api.getSLO(id);
    render.renderSLOForm(content, slo, {
      onSubmit: async (data) => {
        if (!data.name || !data.name.trim()) {
          render.renderToast(t('slo.nameRequired'), 'warn');
          return;
        }
        try {
          await api.updateSLO(id, data);
          render.renderToast(t('slo.updated'), 'success');
          loadSLOs();
        } catch (err) {
          render.renderToast(t('slo.updateFailed') + ': ' + err.message, 'error');
        }
      },
      onCancel: () => loadSLOs(),
    });
  } catch (err) {
    render.renderError(content, t('slo.loadFailed') + ': ' + err.message);
  }
}

// showSLODetail 查看 SLO 详情 + SLI 状态。
export async function showSLODetail(id) {
  const content = sloContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const [slo, statuses] = await Promise.all([
      api.getSLO(id),
      api.getSLOStatus(id).catch(() => []), // 状态获取失败不阻塞详情
    ]);
    render.renderSLODetail(content, slo, statuses, {
      onBack: () => loadSLOs(),
      onEdit: () => editSLO(id),
      onDelete: () => deleteSLO(id),
    });
  } catch (err) {
    render.renderError(content, t('slo.loadFailed') + ': ' + err.message);
  }
}

// deleteSLO 删除 SLO（确认后调用 API）。
export async function deleteSLO(id) {
  if (!window.confirm(t('slo.confirmDelete'))) return;
  try {
    await api.deleteSLO(id);
    render.renderToast(t('slo.deleted'), 'success');
    loadSLOs();
  } catch (err) {
    render.renderToast(t('slo.deleteFailed') + ': ' + err.message, 'error');
  }
}

// buildSLOToolbar 构建 SLO 工具栏（创建按钮 + 刷新）。
export function buildSLOToolbar() {
  const toolbar = $('slo-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-primary', onclick: () => createSLO() },
      iconEl('plus', 16), render.el('span', { text: t('slo.create') })
    )
  );
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => loadSLOs() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}

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

// ============================================================================
// 初始化
// ============================================================================

export function init() {
  // 渲染静态图标（[data-icon] 占位符 → 真实 SVG）。
  document.querySelectorAll('[data-icon]').forEach((holder) => {
    const name = holder.dataset.icon;
    holder.innerHTML = '';
    holder.appendChild(iconEl(name, 18));
  });

  // 绑定语言切换
  document.querySelectorAll('.lang-switch button[data-lang]').forEach((btn) => {
    btn.addEventListener('click', () => {
      i18nSetLang(btn.dataset.lang);
      // 更新激活态
      document.querySelectorAll('.lang-switch button[data-lang]').forEach((b) => {
        b.classList.toggle('lang-active', b.dataset.lang === btn.dataset.lang);
      });
      // 刷新当前页（重新渲染以应用新语言）
      refreshCurrentPage();
    });
  });

  // 构建各页工具栏
  buildTicketsToolbar();
  buildSLOToolbar();
  buildDashboardToolbar();

  // 绑定 tab 切换
  document.querySelectorAll('.tab-btn').forEach((btn) => {
    btn.addEventListener('click', () => switchTab(btn.dataset.tab));
  });

  // 默认加载工单列表
  loadTickets();
}

// refreshCurrentPage 刷新当前页（语言切换后重新渲染）。
function refreshCurrentPage() {
  // 重建工具栏（含翻译文本）
  buildTicketsToolbar();
  buildSLOToolbar();
  buildDashboardToolbar();
  // 重新加载当前页数据
  if (state.currentTab === 'tickets') loadTickets();
  else if (state.currentTab === 'dashboard') loadDashboardAll();
  else if (state.currentTab === 'slo') loadSLOs();
}
