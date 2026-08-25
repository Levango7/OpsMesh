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
  // Phase 2 状态
  trafficPolicies: [],
  pipelineTemplates: [],
  pipelineRuns: [],
  argocdApps: [],
  canaryReleases: [],
  selectedCanaryId: null,
  configVersions: [],
  pipelineSubTab: 'templates', // templates | runs | argocd
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
  const validTabs = ['tickets', 'dashboard', 'slo', 'traffic', 'pipeline', 'canary', 'config-push'];
  if (validTabs.indexOf(tab) === -1) return;
  state.currentTab = tab;
  // 更新 tab 按钮激活态
  document.querySelectorAll('.tab-btn').forEach((btn) => {
    btn.classList.toggle('tab-active', btn.dataset.tab === tab);
  });
  // 更新 page section 可见性
  validTabs.forEach((p) => {
    const root = $('page-' + p);
    if (root) root.classList.toggle('page-active', p === tab);
  });
  // 按需懒加载
  if (tab === 'tickets' && state.tickets.length === 0) loadTickets();
  if (tab === 'dashboard' && !state._dashboardLoaded) loadDashboardAll();
  if (tab === 'slo' && state.slos.length === 0) loadSLOs();
  // Phase 2 懒加载
  if (tab === 'traffic' && state.trafficPolicies.length === 0) loadTrafficPolicies();
  if (tab === 'pipeline' && state.pipelineTemplates.length === 0) loadPipelineTemplates();
  if (tab === 'canary' && state.canaryReleases.length === 0) loadCanaryReleases();
  if (tab === 'config-push' && !state._configPushLoaded) loadConfigVersions();
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
// Phase 2：服务治理
// ============================================================================

function trafficContent() { return $('traffic-content'); }

// loadTrafficPolicies 加载流量策略列表。
export async function loadTrafficPolicies() {
  const content = trafficContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const policies = await api.getTrafficPolicies();
    state.trafficPolicies = policies;
    render.renderTrafficTable(content, policies, {
      onEnable: (id) => enableTrafficPolicy(id),
      onDisable: (id) => disableTrafficPolicy(id),
      onDelete: (id) => deleteTrafficPolicy(id),
    });
  } catch (err) {
    render.renderError(content, t('traffic.loadFailed') + ': ' + err.message);
  }
}

// createTrafficPolicy 打开创建流量策略表单。
export function createTrafficPolicy() {
  const content = trafficContent();
  if (!content) return;
  render.renderTrafficForm(content, {
    onSubmit: async (data) => {
      if (!data.name) { render.renderToast(t('traffic.nameRequired'), 'warn'); return; }
      if (!data.service) { render.renderToast(t('traffic.serviceRequired'), 'warn'); return; }
      try {
        await api.createTrafficPolicy(data);
        render.renderToast(t('traffic.created'), 'success');
        loadTrafficPolicies();
      } catch (err) {
        render.renderToast(t('traffic.createFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => loadTrafficPolicies(),
  });
}

// enableTrafficPolicy 启用流量策略。
export async function enableTrafficPolicy(id) {
  try {
    await api.enableTrafficPolicy(id);
    render.renderToast(t('traffic.enabled'), 'success');
    loadTrafficPolicies();
  } catch (err) {
    render.renderToast(t('traffic.enableFailed') + ': ' + err.message, 'error');
  }
}

// disableTrafficPolicy 禁用流量策略。
export async function disableTrafficPolicy(id) {
  try {
    await api.disableTrafficPolicy(id);
    render.renderToast(t('traffic.disabled'), 'success');
    loadTrafficPolicies();
  } catch (err) {
    render.renderToast(t('traffic.disableFailed') + ': ' + err.message, 'error');
  }
}

// deleteTrafficPolicy 删除流量策略。
export async function deleteTrafficPolicy(id) {
  if (!window.confirm(t('traffic.confirmDelete'))) return;
  try {
    await api.deleteTrafficPolicy(id);
    render.renderToast(t('traffic.deleted'), 'success');
    loadTrafficPolicies();
  } catch (err) {
    render.renderToast(t('traffic.deleteFailed') + ': ' + err.message, 'error');
  }
}

// buildTrafficToolbar 构建服务治理工具栏。
export function buildTrafficToolbar() {
  const toolbar = $('traffic-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-primary', onclick: () => createTrafficPolicy() },
      iconEl('plus', 16), render.el('span', { text: t('traffic.create') })
    )
  );
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => loadTrafficPolicies() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}

// ============================================================================
// Phase 2：CI/CD 流水线
// ============================================================================

function pipelineContent() { return $('pipeline-content'); }

// loadPipelineTemplates 加载流水线模板列表。
export async function loadPipelineTemplates() {
  const content = pipelineContent();
  if (!content) return;
  state.pipelineSubTab = 'templates';
  render.renderLoading(content);
  try {
    const templates = await api.getPipelineTemplates();
    state.pipelineTemplates = templates;
    render.renderPipelineTemplates(content, templates, {
      onRun: (id) => runPipeline(id),
      onDelete: (id) => deletePipelineTemplate(id),
    });
  } catch (err) {
    render.renderError(content, t('pipeline.loadFailed') + ': ' + err.message);
  }
}

// loadPipelineRuns 加载流水线运行记录。
export async function loadPipelineRuns() {
  const content = pipelineContent();
  if (!content) return;
  state.pipelineSubTab = 'runs';
  render.renderLoading(content);
  try {
    const runs = await api.getPipelineRuns();
    state.pipelineRuns = runs;
    render.renderPipelineRuns(content, runs);
  } catch (err) {
    render.renderError(content, t('pipeline.loadFailed') + ': ' + err.message);
  }
}

// loadArgoCDApps 加载 ArgoCD 应用列表。
export async function loadArgoCDApps() {
  const content = pipelineContent();
  if (!content) return;
  state.pipelineSubTab = 'argocd';
  render.renderLoading(content);
  try {
    const apps = await api.getArgoCDApps();
    state.argocdApps = apps;
    render.renderArgoCDApps(content, apps, {
      onSync: (id) => syncArgoCDApp(id),
      onDelete: (id) => deleteArgoCDApp(id),
    });
  } catch (err) {
    render.renderError(content, t('pipeline.loadFailed') + ': ' + err.message);
  }
}

// createPipelineTemplate 打开创建流水线模板表单。
export function createPipelineTemplate() {
  const content = pipelineContent();
  if (!content) return;
  render.renderPipelineTemplateForm(content, {
    onSubmit: async (data) => {
      if (!data.name) { render.renderToast(t('pipeline.nameRequired'), 'warn'); return; }
      try {
        await api.createPipelineTemplate(data);
        render.renderToast(t('pipeline.created'), 'success');
        loadPipelineTemplates();
      } catch (err) {
        render.renderToast(t('pipeline.createFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => loadPipelineTemplates(),
  });
}

// deletePipelineTemplate 删除流水线模板。
export async function deletePipelineTemplate(id) {
  if (!window.confirm(t('pipeline.confirmDelete'))) return;
  try {
    await api.deletePipelineTemplate(id);
    render.renderToast(t('pipeline.deleted'), 'success');
    loadPipelineTemplates();
  } catch (err) {
    render.renderToast(t('pipeline.deleteFailed') + ': ' + err.message, 'error');
  }
}

// runPipeline 触发流水线运行。
export async function runPipeline(id) {
  try {
    await api.runPipeline(id);
    render.renderToast(t('pipeline.running'), 'success');
    loadPipelineRuns();
  } catch (err) {
    render.renderToast(t('pipeline.runFailed') + ': ' + err.message, 'error');
  }
}

// syncArgoCDApp 同步 ArgoCD 应用。
export async function syncArgoCDApp(id) {
  try {
    await api.syncArgoCDApp(id);
    render.renderToast(t('pipeline.argoSynced'), 'success');
    loadArgoCDApps();
  } catch (err) {
    render.renderToast(t('pipeline.argoSyncFailed') + ': ' + err.message, 'error');
  }
}

// deleteArgoCDApp 删除 ArgoCD 应用。
export async function deleteArgoCDApp(id) {
  if (!window.confirm(t('pipeline.confirmDelete'))) return;
  try {
    await api.deleteArgoCDApp(id);
    render.renderToast(t('pipeline.deleted'), 'success');
    loadArgoCDApps();
  } catch (err) {
    render.renderToast(t('pipeline.deleteFailed') + ': ' + err.message, 'error');
  }
}

// buildPipelineToolbar 构建流水线工具栏（含子 tab 切换）。
export function buildPipelineToolbar() {
  const toolbar = $('pipeline-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  // 子 tab：模板 / 运行记录 / ArgoCD
  const subTabs = [
    { key: 'templates', label: t('pipeline.templates'), onclick: () => loadPipelineTemplates() },
    { key: 'runs',      label: t('pipeline.runs'),      onclick: () => loadPipelineRuns() },
    { key: 'argocd',    label: t('pipeline.argoApps'),  onclick: () => loadArgoCDApps() },
  ];
  const group = render.el('div', { class: 'filter-group' });
  subTabs.forEach((s) => {
    group.appendChild(
      render.el('button', {
        class: 'btn ' + (state.pipelineSubTab === s.key ? 'btn-secondary' : 'btn-ghost'),
        onclick: s.onclick,
      }, render.el('span', { text: s.label }))
    );
  });
  toolbar.appendChild(group);
  // 创建模板按钮（仅 templates 子 tab 显示）
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-primary', onclick: () => createPipelineTemplate() },
      iconEl('plus', 16), render.el('span', { text: t('pipeline.create') })
    )
  );
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => refreshPipelineSubTab() },
      iconEl('refresh', 14)
    )
  );
}

function refreshPipelineSubTab() {
  if (state.pipelineSubTab === 'templates') loadPipelineTemplates();
  else if (state.pipelineSubTab === 'runs') loadPipelineRuns();
  else if (state.pipelineSubTab === 'argocd') loadArgoCDApps();
  else loadPipelineTemplates();
}

// ============================================================================
// Phase 2：灰度发布
// ============================================================================

function canaryContent() { return $('canary-content'); }

// loadCanaryReleases 加载灰度发布列表 + 默认选中第一个。
export async function loadCanaryReleases() {
  const content = canaryContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const releases = await api.getCanaryReleases();
    state.canaryReleases = releases;
    renderCanaryView(content, releases);
  } catch (err) {
    render.renderError(content, t('canary.loadFailed') + ': ' + err.message);
  }
}

// renderCanaryView 渲染灰度发布视图（列表 + 分割面板 + 指标）。
function renderCanaryView(content, releases) {
  content.innerHTML = '';
  // 上半：列表
  const listHost = render.el('div', { class: 'content', style: { marginBottom: '1rem' } });
  render.renderCanaryList(listHost, releases, {
    onSelect: (id) => selectCanary(id),
  });
  content.appendChild(listHost);
  // 下半：分割面板 + 指标
  const selected = releases.find((r) => r.id === state.selectedCanaryId) || releases[0];
  if (selected) {
    state.selectedCanaryId = selected.id;
    const splitHost = render.el('div', { class: 'content', style: { marginBottom: '1rem' } });
    render.renderCanarySplitPanel(splitHost, selected, {
      onApply: (percent) => applyTrafficSplit(selected.id, percent),
    });
    content.appendChild(splitHost);
    // 指标区
    const metricsHost = render.el('div', { class: 'content' });
    render.renderLoading(metricsHost);
    content.appendChild(metricsHost);
    api.getCanaryMetrics(selected.id)
      .then((metrics) => render.renderCanaryMetrics(metricsHost, metrics))
      .catch((err) => render.renderError(metricsHost, t('canary.metricsLoadFailed') + ': ' + err.message));
  }
}

// selectCanary 选中某个灰度发布，重新渲染视图。
function selectCanary(id) {
  state.selectedCanaryId = id;
  renderCanaryView(canaryContent(), state.canaryReleases);
}

// applyTrafficSplit 应用流量分割。
export async function applyTrafficSplit(id, percent) {
  try {
    await api.setTrafficSplit(id, percent);
    render.renderToast(t('canary.splitApplied') + ': ' + percent + '%', 'success');
    // 更新本地状态中的 percent
    const r = state.canaryReleases.find((x) => x.id === id);
    if (r) r.percent = percent;
    renderCanaryView(canaryContent(), state.canaryReleases);
  } catch (err) {
    render.renderToast(t('canary.splitFailed') + ': ' + err.message, 'error');
  }
}

// buildCanaryToolbar 构建灰度发布工具栏。
export function buildCanaryToolbar() {
  const toolbar = $('canary-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => loadCanaryReleases() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}

// ============================================================================
// Phase 2：配置热推
// ============================================================================

function configPushContent() { return $('config-push-content'); }

// loadConfigVersions 加载配置版本历史（默认空 key）。
export async function loadConfigVersions(key) {
  state._configPushLoaded = true;
  const content = configPushContent();
  if (!content) return;
  content.innerHTML = '';
  // 上：热推送表单
  const hotpushHost = render.el('div', { class: 'content', style: { marginBottom: '1rem' } });
  render.renderConfigHotpushForm(hotpushHost, {
    onSubmit: (data) => hotpushConfig(data),
  });
  content.appendChild(hotpushHost);
  // 中：灰度配置表单
  const canaryHost = render.el('div', { class: 'content', style: { marginBottom: '1rem' } });
  render.renderConfigCanaryForm(canaryHost, {
    onSubmit: (data) => canaryConfigPush(data),
  });
  content.appendChild(canaryHost);
  // 下：版本历史（含 Key 查询框）
  const versionHost = render.el('div', { class: 'content' });
  versionHost.appendChild(render.el('h3', { class: 'form-title', text: t('configPush.versions') }));
  // Key 查询行
  const queryRow = render.el('div', { class: 'form-row' },
    render.el('label', { class: 'form-label', text: t('configPush.queryKey') }),
    render.el('div', { class: 'form-control' },
      (() => {
        const input = render.el('input', { type: 'text', name: 'queryKey', value: key || '', placeholder: 'config key' });
        input.addEventListener('change', () => loadConfigVersions(input.value));
        return input;
      })()
    )
  );
  versionHost.appendChild(queryRow);
  const versionsHost = render.el('div');
  versionHost.appendChild(versionsHost);
  render.renderLoading(versionsHost);
  content.appendChild(versionHost);
  try {
    const versions = await api.getConfigVersions(key || '');
    state.configVersions = versions;
    render.renderConfigVersions(versionsHost, versions);
  } catch (err) {
    render.renderError(versionsHost, t('configPush.versionsLoadFailed') + ': ' + err.message);
  }
}

// hotpushConfig 触发配置热推送。
export async function hotpushConfig(data) {
  if (!data.deviceID) { render.renderToast(t('configPush.deviceRequired'), 'warn'); return; }
  if (!data.key) { render.renderToast(t('configPush.keyRequired'), 'warn'); return; }
  try {
    await api.hotpushConfig(data);
    render.renderToast(t('configPush.hotpushed'), 'success');
    loadConfigVersions(data.key);
  } catch (err) {
    render.renderToast(t('configPush.hotpushFailed') + ': ' + err.message, 'error');
  }
}

// canaryConfigPush 灰度配置发布。
export async function canaryConfigPush(data) {
  if (!data.devices || !data.devices.length) { render.renderToast(t('configPush.deviceRequired'), 'warn'); return; }
  try {
    await api.canaryConfig(data);
    render.renderToast(t('configPush.canaryApplied'), 'success');
    loadConfigVersions(data.key);
  } catch (err) {
    render.renderToast(t('configPush.canaryFailed') + ': ' + err.message, 'error');
  }
}

// buildConfigPushToolbar 构建配置热推工具栏。
export function buildConfigPushToolbar() {
  const toolbar = $('config-push-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => loadConfigVersions('') },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
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
  buildTrafficToolbar();
  buildPipelineToolbar();
  buildCanaryToolbar();
  buildConfigPushToolbar();

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
  buildTrafficToolbar();
  buildPipelineToolbar();
  buildCanaryToolbar();
  buildConfigPushToolbar();
  // 重新加载当前页数据
  if (state.currentTab === 'tickets') loadTickets();
  else if (state.currentTab === 'dashboard') loadDashboardAll();
  else if (state.currentTab === 'slo') loadSLOs();
  else if (state.currentTab === 'traffic') loadTrafficPolicies();
  else if (state.currentTab === 'pipeline') refreshPipelineSubTab();
  else if (state.currentTab === 'canary') loadCanaryReleases();
  else if (state.currentTab === 'config-push') loadConfigVersions('');
}
