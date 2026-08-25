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
  // Phase 3 状态
  complianceRules: [],
  complianceReports: [],
  complianceSubTab: 'rules', // rules | scan | audit
  auditEvents: [],
  haStatus: null,
  haInstances: [],
  haHealth: null,
  backups: [],
  haSubTab: 'status', // status | backup
  // Phase 4 状态
  networkDevices: [],
  networkDiscovered: [],
  networkSubTab: 'devices', // devices | discover | config
  networkSelectedDevice: null,
  automationRules: [],
  automationExecutions: [],
  automationSubTab: 'rules', // rules | executions
  automationEditingRule: null,
  // Phase 5 状态
  gatewayRoutes: [],
  gatewayStats: null,
  gatewaySubTab: 'routes', // routes | stats
  gatewayEditingRoute: null,
  webhooks: [],
  webhookSubTab: 'list', // list | deliveries
  webhookEditing: null,
  webhookSelectedId: null,
  scripts: [],
  scriptSubTab: 'list', // list | executions
  scriptEditing: null,
  scriptSelectedId: null,
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
  const validTabs = ['tickets', 'dashboard', 'slo', 'traffic', 'pipeline', 'canary', 'config-push', 'compliance', 'ha', 'network-mgmt', 'automation', 'gateway', 'webhook', 'script'];
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
  // Phase 3 懒加载
  if (tab === 'compliance' && state.complianceRules.length === 0) loadComplianceRules();
  if (tab === 'ha' && !state._haLoaded) loadHAStatus();
  // Phase 4 懒加载
  if (tab === 'network-mgmt' && state.networkDevices.length === 0) loadNetworkDevices();
  if (tab === 'automation' && state.automationRules.length === 0) loadAutomationRules();
  // Phase 5 懒加载
  if (tab === 'gateway' && state.gatewayRoutes.length === 0) loadGatewayRoutes();
  if (tab === 'webhook' && state.webhooks.length === 0) loadWebhooks();
  if (tab === 'script' && state.scripts.length === 0) loadScripts();
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
  buildComplianceToolbar();
  buildHAToolbar();
  buildNetworkToolbar();
  buildAutomationToolbar();
  buildGatewayToolbar();
  buildWebhookToolbar();
  buildScriptToolbar();

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
  buildComplianceToolbar();
  buildHAToolbar();
  buildNetworkToolbar();
  buildAutomationToolbar();
  buildGatewayToolbar();
  buildWebhookToolbar();
  buildScriptToolbar();
  // 重新加载当前页数据
  if (state.currentTab === 'tickets') loadTickets();
  else if (state.currentTab === 'dashboard') loadDashboardAll();
  else if (state.currentTab === 'slo') loadSLOs();
  else if (state.currentTab === 'traffic') loadTrafficPolicies();
  else if (state.currentTab === 'pipeline') refreshPipelineSubTab();
  else if (state.currentTab === 'canary') loadCanaryReleases();
  else if (state.currentTab === 'config-push') loadConfigVersions('');
  else if (state.currentTab === 'compliance') refreshComplianceSubTab();
  else if (state.currentTab === 'ha') refreshHASubTab();
  else if (state.currentTab === 'network-mgmt') refreshNetworkSubTab();
  else if (state.currentTab === 'automation') refreshAutomationSubTab();
  else if (state.currentTab === 'gateway') refreshGatewaySubTab();
  else if (state.currentTab === 'webhook') refreshWebhookSubTab();
  else if (state.currentTab === 'script') refreshScriptSubTab();
}

// ============================================================================
// Phase 3：安全合规
// ============================================================================

function complianceContent() { return $('compliance-content'); }

// loadComplianceRules 加载合规规则列表（含子 tab 切换 + 扫描表单 + 审计查询）。
export async function loadComplianceRules() {
  const content = complianceContent();
  if (!content) return;
  content.innerHTML = '';
  // 子 tab 切换条
  const subTabs = [
    { key: 'rules', label: t('compliance.rules'), onclick: () => { state.complianceSubTab = 'rules'; refreshComplianceSubTab(); } },
    { key: 'scan',  label: t('compliance.scan'),  onclick: () => { state.complianceSubTab = 'scan';  refreshComplianceSubTab(); } },
    { key: 'audit', label: t('compliance.audit'), onclick: () => { state.complianceSubTab = 'audit'; refreshComplianceSubTab(); } },
  ];
  const subBar = render.el('div', { class: 'toolbar' });
  subTabs.forEach((s) => {
    subBar.appendChild(render.el('button', {
      class: 'btn ' + (state.complianceSubTab === s.key ? 'btn-secondary' : 'btn-ghost'),
      onclick: s.onclick,
    }, render.el('span', { text: s.label })));
  });
  content.appendChild(subBar);
  // 规则列表区
  const rulesHost = render.el('div', { class: 'content', style: { marginTop: '1rem' } });
  content.appendChild(rulesHost);
  if (state.complianceSubTab !== 'rules') { refreshComplianceSubTab(); return; }
  render.renderLoading(rulesHost);
  try {
    const rules = await api.getComplianceRules();
    state.complianceRules = rules;
    render.renderComplianceRulesTable(rulesHost, rules, {
      onSelect: (r) => showComplianceRuleDetail(r.id || r.name, r),
    });
  } catch (err) {
    render.renderError(rulesHost, t('compliance.rulesLoadFailed') + ': ' + err.message);
  }
}

// showComplianceRuleDetail 显示合规规则详情。
export async function showComplianceRuleDetail(id, cached) {
  const content = complianceContent();
  if (!content) return;
  const host = render.el('div', { class: 'content' });
  content.innerHTML = '';
  content.appendChild(host);
  render.renderLoading(host);
  try {
    let rule = cached;
    if (!rule || !rule.checkScript) rule = await api.getComplianceRule(id);
    render.renderComplianceRuleDetail(host, rule, { onBack: () => loadComplianceRules() });
  } catch (err) {
    render.renderError(host, t('compliance.rulesLoadFailed') + ': ' + err.message);
  }
}

// scanCompliance 对指定设备发起合规扫描并展示报告。
export async function scanCompliance(deviceID) {
  if (!deviceID) { render.renderToast(t('compliance.deviceRequired'), 'warn'); return; }
  const content = complianceContent();
  if (!content) return;
  const reportHost = render.el('div', { class: 'content', style: { marginTop: '1rem' } });
  content.appendChild(reportHost);
  render.renderLoading(reportHost, t('compliance.scanning'));
  try {
    const report = await api.scanCompliance(deviceID);
    render.renderComplianceReport(reportHost, report);
    render.renderToast(t('compliance.scanDone'), 'success');
    // 刷新报告列表
    loadComplianceReports(true);
  } catch (err) {
    render.renderError(reportHost, t('compliance.scanFailed') + ': ' + err.message);
  }
}

// loadComplianceReports 加载合规报告列表（silent=true 时不渲染，仅刷新 state）。
export async function loadComplianceReports(silent) {
  try {
    const reports = await api.getComplianceReports();
    state.complianceReports = reports;
    if (!silent && state.complianceSubTab === 'scan') {
      // 在扫描子 tab 下追加报告列表
      const content = complianceContent();
      if (!content) return;
      const host = render.el('div', { class: 'content', style: { marginTop: '1rem' } });
      content.appendChild(host);
      render.renderComplianceReportsList(host, reports, {
        onView: (r) => showComplianceReportDetail(r.id),
      });
    }
  } catch (err) {
    if (!silent) render.renderToast(t('compliance.reportsLoadFailed') + ': ' + err.message, 'error');
  }
}

// showComplianceReportDetail 显示合规报告详情。
export async function showComplianceReportDetail(id) {
  const content = complianceContent();
  if (!content) return;
  const host = render.el('div', { class: 'content' });
  content.innerHTML = '';
  content.appendChild(host);
  render.renderLoading(host);
  try {
    const report = await api.getComplianceReport(id);
    render.renderComplianceReport(host, report);
  } catch (err) {
    render.renderError(host, t('compliance.reportsLoadFailed') + ': ' + err.message);
  }
}

// loadAuditEvents 查询审计事件。
export async function loadAuditEvents(params) {
  const content = complianceContent();
  if (!content) return;
  // 保留查询表单，追加结果区
  let resultHost = $('audit-events-result');
  if (!resultHost) {
    resultHost = render.el('div', { id: 'audit-events-result', class: 'content', style: { marginTop: '1rem' } });
    content.appendChild(resultHost);
  }
  render.renderLoading(resultHost);
  try {
    const events = await api.getAuditEvents(params);
    state.auditEvents = events;
    render.renderAuditEventsTable(resultHost, events);
  } catch (err) {
    render.renderError(resultHost, t('compliance.auditLoadFailed') + ': ' + err.message);
  }
}

// exportAuditLogs 导出审计日志。
export async function exportAuditLogs(params) {
  try {
    const data = await api.exportAuditLogs(params);
    render.renderToast(t('compliance.export') + ' OK', 'success');
    return data;
  } catch (err) {
    render.renderToast(t('compliance.exportFailed') + ': ' + err.message, 'error');
  }
}

// buildComplianceToolbar 构建合规工具栏。
export function buildComplianceToolbar() {
  const toolbar = $('compliance-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => refreshComplianceSubTab() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}

// refreshComplianceSubTab 根据当前子 tab 重新渲染合规页。
function refreshComplianceSubTab() {
  const sub = state.complianceSubTab;
  if (sub === 'rules') { loadComplianceRules(); return; }
  const content = complianceContent();
  if (!content) return;
  content.innerHTML = '';
  // 子 tab 切换条
  const subTabs = [
    { key: 'rules', label: t('compliance.rules') },
    { key: 'scan',  label: t('compliance.scan') },
    { key: 'audit', label: t('compliance.audit') },
  ];
  const subBar = render.el('div', { class: 'toolbar' });
  subTabs.forEach((s) => {
    subBar.appendChild(render.el('button', {
      class: 'btn ' + (sub === s.key ? 'btn-secondary' : 'btn-ghost'),
      onclick: () => { state.complianceSubTab = s.key; refreshComplianceSubTab(); },
    }, render.el('span', { text: s.label })));
  });
  content.appendChild(subBar);
  if (sub === 'scan') {
    // 扫描表单
    const formHost = render.el('div', { class: 'content', style: { marginTop: '1rem' } });
    content.appendChild(formHost);
    render.renderComplianceScanForm(formHost, { onScan: (deviceID) => scanCompliance(deviceID) });
    // 报告列表
    loadComplianceReports();
  } else if (sub === 'audit') {
    // 审计查询表单
    const formHost = render.el('div', { class: 'content', style: { marginTop: '1rem' } });
    content.appendChild(formHost);
    render.renderAuditQueryForm(formHost, {
      onQuery: (params) => loadAuditEvents(params),
      onExport: (params) => exportAuditLogs(params),
    });
  }
}

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

// ============================================================================
// Phase 4：网络管理
// ============================================================================

function networkContent() { return $('network-content'); }

// loadNetworkDevices 加载网络设备列表（含子 tab 切换：设备列表 / 网络发现 / 配置下发）。
export async function loadNetworkDevices() {
  const content = networkContent();
  if (!content) return;
  content.innerHTML = '';
  // 子 tab 切换条
  const subTabs = [
    { key: 'devices',  label: t('network.devices'),  onclick: () => { state.networkSubTab = 'devices';  refreshNetworkSubTab(); } },
    { key: 'discover', label: t('network.discover'), onclick: () => { state.networkSubTab = 'discover'; refreshNetworkSubTab(); } },
    { key: 'config',   label: t('network.config'),   onclick: () => { state.networkSubTab = 'config';   refreshNetworkSubTab(); } },
  ];
  const subBar = render.el('div', { class: 'toolbar' });
  subTabs.forEach((s) => {
    subBar.appendChild(render.el('button', {
      class: 'btn ' + (state.networkSubTab === s.key ? 'btn-secondary' : 'btn-ghost'),
      onclick: s.onclick,
    }, render.el('span', { text: s.label })));
  });
  content.appendChild(subBar);
  const host = render.el('div', { class: 'content', style: { marginTop: '1rem' } });
  content.appendChild(host);
  if (state.networkSubTab !== 'devices') { refreshNetworkSubTab(); return; }
  // 设备列表 + 添加设备表单
  const listHost = render.el('div', { id: 'network-devices-list' });
  host.appendChild(listHost);
  render.renderLoading(listHost);
  try {
    const devices = await api.getNetworkDevices();
    state.networkDevices = devices;
    render.renderNetworkDevicesTable(listHost, devices, {
      onDetail: (d) => showNetworkDeviceDetail(d.id || d.name),
      onDelete: (d) => deleteNetworkDevice(d.id || d.name),
    });
  } catch (err) {
    render.renderError(listHost, t('network.devicesLoadFailed') + ': ' + err.message);
  }
  // 添加设备表单
  const formHost = render.el('div', { id: 'network-device-form', style: { marginTop: '1rem' } });
  host.appendChild(formHost);
  render.renderNetworkDeviceForm(formHost, { onCreate: (data) => createNetworkDevice(data) });
}

// showNetworkDeviceDetail 显示网络设备详情（监控指标）。
export async function showNetworkDeviceDetail(id) {
  if (!id) return;
  const content = networkContent();
  if (!content) return;
  content.innerHTML = '';
  // 返回按钮
  const backBar = render.el('div', { class: 'toolbar' });
  backBar.appendChild(render.el('button', {
    class: 'btn btn-ghost',
    onclick: () => { state.networkSubTab = 'devices'; loadNetworkDevices(); },
  }, iconEl('back', 14), render.el('span', { text: t('network.devices') })));
  content.appendChild(backBar);
  const host = render.el('div', { class: 'content', style: { marginTop: '1rem' } });
  content.appendChild(host);
  render.renderLoading(host);
  try {
    const [device, metrics] = await Promise.all([
      api.getNetworkDevice(id).catch(() => null),
      api.getNetworkDeviceMetrics(id).catch(() => null),
    ]);
    // 设备基本信息
    if (device) {
      const infoCard = render.el('div', { class: 'content', style: { marginBottom: '1rem' } });
      infoCard.appendChild(render.el('h3', { class: 'form-title', text: t('network.deviceDetail') }));
      const fields = [
        { label: t('network.deviceName'), value: device.name || device.id },
        { label: t('network.deviceType'), value: device.type },
        { label: t('network.deviceIP'), value: device.ip || device.managementIP },
        { label: t('network.devicePort'), value: device.port },
        { label: t('network.vendor'), value: device.vendor },
        { label: t('network.model'), value: device.model },
        { label: t('network.status'), value: device.status },
      ];
      fields.forEach((f) => {
        if (f.value != null && f.value !== '') {
          infoCard.appendChild(render.el('div', { class: 'form-row' },
            render.el('label', { class: 'form-label', text: f.label }),
            render.el('div', { class: 'form-control', text: String(f.value) })
          ));
        }
      });
      host.appendChild(infoCard);
    }
    // 监控指标
    const metricsHost = render.el('div', { id: 'network-metrics' });
    host.appendChild(metricsHost);
    render.renderNetworkDeviceMetrics(metricsHost, metrics);
  } catch (err) {
    render.renderError(host, t('network.metricsLoadFailed') + ': ' + err.message);
  }
}

// createNetworkDevice 创建网络设备。
export async function createNetworkDevice(data) {
  if (!data || !data.name) { render.renderToast(t('network.nameRequired'), 'warn'); return; }
  if (!data.ip) { render.renderToast(t('network.ipRequired'), 'warn'); return; }
  try {
    await api.createNetworkDevice(data);
    render.renderToast(t('network.deviceCreated'), 'success');
    state.networkSubTab = 'devices';
    loadNetworkDevices();
  } catch (err) {
    render.renderToast(t('network.deviceCreateFailed') + ': ' + err.message, 'error');
  }
}

// deleteNetworkDevice 删除网络设备。
export async function deleteNetworkDevice(id) {
  if (!id) return;
  if (!window.confirm(t('network.deleteConfirm'))) return;
  try {
    await api.deleteNetworkDevice(id);
    render.renderToast(t('network.deviceDeleted'), 'success');
    loadNetworkDevices();
  } catch (err) {
    render.renderToast(t('network.deviceDeleteFailed') + ': ' + err.message, 'error');
  }
}

// discoverNetwork 执行网络发现。
export async function discoverNetwork(subnet) {
  if (!subnet) { render.renderToast(t('network.subnetRequired'), 'warn'); return; }
  const host = $('network-discover-result');
  if (host) render.renderLoading(host, t('network.discovering'));
  try {
    const devices = await api.discoverNetwork(subnet);
    state.networkDiscovered = devices;
    if (host) render.renderNetworkDiscoverResult(host, devices);
    render.renderToast(t('network.discoverResult') + ': ' + (devices.length || 0), 'success');
  } catch (err) {
    if (host) render.renderError(host, t('network.discoverFailed') + ': ' + err.message);
    render.renderToast(t('network.discoverFailed') + ': ' + err.message, 'error');
  }
}

// deployNetworkConfig 下发配置到网络设备。
export async function deployNetworkConfig(deviceId, config) {
  if (!deviceId) { render.renderToast(t('network.deviceRequired'), 'warn'); return; }
  if (!config) { render.renderToast(t('network.configRequired'), 'warn'); return; }
  render.renderToast(t('network.deploying'), 'info');
  try {
    await api.configNetworkDevice(deviceId, config);
    render.renderToast(t('network.configDeployed'), 'success');
  } catch (err) {
    render.renderToast(t('network.configDeployFailed') + ': ' + err.message, 'error');
  }
}

// buildNetworkToolbar 构建网络管理工具栏。
export function buildNetworkToolbar() {
  const toolbar = $('network-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => refreshNetworkSubTab() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}

// refreshNetworkSubTab 根据当前子 tab 重新渲染网络管理页。
function refreshNetworkSubTab() {
  const sub = state.networkSubTab;
  if (sub === 'devices') { loadNetworkDevices(); return; }
  const content = networkContent();
  if (!content) return;
  content.innerHTML = '';
  // 子 tab 切换条
  const subTabs = [
    { key: 'devices',  label: t('network.devices') },
    { key: 'discover', label: t('network.discover') },
    { key: 'config',   label: t('network.config') },
  ];
  const subBar = render.el('div', { class: 'toolbar' });
  subTabs.forEach((s) => {
    subBar.appendChild(render.el('button', {
      class: 'btn ' + (sub === s.key ? 'btn-secondary' : 'btn-ghost'),
      onclick: () => { state.networkSubTab = s.key; refreshNetworkSubTab(); },
    }, render.el('span', { text: s.label })));
  });
  content.appendChild(subBar);
  const host = render.el('div', { class: 'content', style: { marginTop: '1rem' } });
  content.appendChild(host);
  if (sub === 'discover') {
    // 网络发现表单 + 结果
    render.renderNetworkDiscoverForm(host, { onDiscover: (subnet) => discoverNetwork(subnet) });
    const resultHost = render.el('div', { id: 'network-discover-result', style: { marginTop: '1rem' } });
    host.appendChild(resultHost);
    if (state.networkDiscovered.length) {
      render.renderNetworkDiscoverResult(resultHost, state.networkDiscovered);
    }
  } else if (sub === 'config') {
    // 配置下发表单
    render.renderNetworkConfigForm(host, state.networkDevices, { onDeploy: (id, cfg) => deployNetworkConfig(id, cfg) });
  }
}

// ============================================================================
// Phase 4：自动化闭环
// ============================================================================

function automationContent() { return $('automation-content'); }

// loadAutomationRules 加载自动化规则列表（含子 tab 切换：规则列表 / 执行历史）。
export async function loadAutomationRules() {
  const content = automationContent();
  if (!content) return;
  content.innerHTML = '';
  // 子 tab 切换条
  const subTabs = [
    { key: 'rules',       label: t('automation.rules'),       onclick: () => { state.automationSubTab = 'rules';       refreshAutomationSubTab(); } },
    { key: 'executions',  label: t('automation.executions'),  onclick: () => { state.automationSubTab = 'executions';  refreshAutomationSubTab(); } },
  ];
  const subBar = render.el('div', { class: 'toolbar' });
  subTabs.forEach((s) => {
    subBar.appendChild(render.el('button', {
      class: 'btn ' + (state.automationSubTab === s.key ? 'btn-secondary' : 'btn-ghost'),
      onclick: s.onclick,
    }, render.el('span', { text: s.label })));
  });
  content.appendChild(subBar);
  const host = render.el('div', { class: 'content', style: { marginTop: '1rem' } });
  content.appendChild(host);
  if (state.automationSubTab !== 'rules') { refreshAutomationSubTab(); return; }
  // 规则列表 + 创建规则表单
  const listHost = render.el('div', { id: 'automation-rules-list' });
  host.appendChild(listHost);
  render.renderLoading(listHost);
  try {
    const rules = await api.getAutomationRules();
    state.automationRules = rules;
    render.renderAutomationRulesTable(listHost, rules, {
      onEdit: (r) => editAutomationRule(r),
      onEnable: (r) => enableAutomationRule(r.id || r.name),
      onDisable: (r) => disableAutomationRule(r.id || r.name),
      onTest: (r) => testAutomationRule(r.id || r.name),
      onDelete: (r) => deleteAutomationRule(r.id || r.name),
    });
  } catch (err) {
    render.renderError(listHost, t('automation.rulesLoadFailed') + ': ' + err.message);
  }
  // 创建规则表单
  const formHost = render.el('div', { id: 'automation-rule-form', style: { marginTop: '1rem' } });
  host.appendChild(formHost);
  render.renderAutomationRuleForm(formHost, null, { onSubmit: (data) => createAutomationRule(data) });
}

// createAutomationRule 创建自动化规则。
export async function createAutomationRule(data) {
  if (!data || !data.name) { render.renderToast(t('automation.nameRequired'), 'warn'); return; }
  if (!data.trigger) { render.renderToast(t('automation.triggerRequired'), 'warn'); return; }
  if (!data.action) { render.renderToast(t('automation.actionRequired'), 'warn'); return; }
  try {
    await api.createAutomationRule(data);
    render.renderToast(t('automation.ruleCreated'), 'success');
    state.automationSubTab = 'rules';
    loadAutomationRules();
  } catch (err) {
    render.renderToast(t('automation.ruleCreateFailed') + ': ' + err.message, 'error');
  }
}

// editAutomationRule 编辑自动化规则。
export async function editAutomationRule(rule) {
  if (!rule) return;
  state.automationEditingRule = rule;
  const content = automationContent();
  if (!content) return;
  content.innerHTML = '';
  // 返回按钮
  const backBar = render.el('div', { class: 'toolbar' });
  backBar.appendChild(render.el('button', {
    class: 'btn btn-ghost',
    onclick: () => { state.automationEditingRule = null; state.automationSubTab = 'rules'; loadAutomationRules(); },
  }, iconEl('back', 14), render.el('span', { text: t('automation.rules') })));
  content.appendChild(backBar);
  const host = render.el('div', { class: 'content', style: { marginTop: '1rem' } });
  content.appendChild(host);
  // 先尝试获取详情，失败则用列表数据
  let detail = rule;
  try {
    detail = await api.getAutomationRule(rule.id || rule.name);
  } catch (_) { /* 用列表数据 */ }
  render.renderAutomationRuleForm(host, detail, { onSubmit: (data) => updateAutomationRule(rule.id || rule.name, data) });
}

// updateAutomationRule 更新自动化规则。
export async function updateAutomationRule(id, data) {
  if (!id) return;
  if (!data || !data.name) { render.renderToast(t('automation.nameRequired'), 'warn'); return; }
  try {
    await api.updateAutomationRule(id, data);
    render.renderToast(t('automation.ruleUpdated'), 'success');
    state.automationEditingRule = null;
    state.automationSubTab = 'rules';
    loadAutomationRules();
  } catch (err) {
    render.renderToast(t('automation.ruleUpdateFailed') + ': ' + err.message, 'error');
  }
}

// deleteAutomationRule 删除自动化规则。
export async function deleteAutomationRule(id) {
  if (!id) return;
  if (!window.confirm(t('automation.deleteConfirm'))) return;
  try {
    await api.deleteAutomationRule(id);
    render.renderToast(t('automation.ruleDeleted'), 'success');
    loadAutomationRules();
  } catch (err) {
    render.renderToast(t('automation.ruleDeleteFailed') + ': ' + err.message, 'error');
  }
}

// enableAutomationRule 启用自动化规则。
export async function enableAutomationRule(id) {
  if (!id) return;
  try {
    await api.enableAutomationRule(id);
    render.renderToast(t('automation.ruleEnabled'), 'success');
    loadAutomationRules();
  } catch (err) {
    render.renderToast(t('automation.ruleEnableFailed') + ': ' + err.message, 'error');
  }
}

// disableAutomationRule 禁用自动化规则。
export async function disableAutomationRule(id) {
  if (!id) return;
  try {
    await api.disableAutomationRule(id);
    render.renderToast(t('automation.ruleDisabled'), 'success');
    loadAutomationRules();
  } catch (err) {
    render.renderToast(t('automation.ruleDisableFailed') + ': ' + err.message, 'error');
  }
}

// testAutomationRule 测试自动化规则。
export async function testAutomationRule(id) {
  if (!id) return;
  try {
    const result = await api.testAutomationRule(id);
    const output = (result && (result.output || result.message)) || JSON.stringify(result || {});
    render.renderToast(t('automation.ruleTested') + ': ' + String(output).slice(0, 100), 'success');
  } catch (err) {
    render.renderToast(t('automation.ruleTestFailed') + ': ' + err.message, 'error');
  }
}

// loadAutomationExecutions 加载自动化执行历史。
export async function loadAutomationExecutions() {
  const content = automationContent();
  if (!content) return;
  let host = $('automation-executions-list');
  if (!host) {
    host = render.el('div', { id: 'automation-executions-list', class: 'content', style: { marginTop: '1rem' } });
    content.appendChild(host);
  }
  render.renderLoading(host);
  try {
    const execs = await api.getAutomationExecutions();
    state.automationExecutions = execs;
    render.renderAutomationExecutionsTable(host, execs, {
      onDetail: (e) => showAutomationExecutionDetail(e.id || e.executionID),
    });
  } catch (err) {
    render.renderError(host, t('automation.executionsLoadFailed') + ': ' + err.message);
  }
}

// showAutomationExecutionDetail 显示自动化执行详情。
export async function showAutomationExecutionDetail(id) {
  if (!id) return;
  const content = automationContent();
  if (!content) return;
  content.innerHTML = '';
  // 返回按钮
  const backBar = render.el('div', { class: 'toolbar' });
  backBar.appendChild(render.el('button', {
    class: 'btn btn-ghost',
    onclick: () => { state.automationSubTab = 'executions'; refreshAutomationSubTab(); },
  }, iconEl('back', 14), render.el('span', { text: t('automation.executions') })));
  content.appendChild(backBar);
  const host = render.el('div', { class: 'content', style: { marginTop: '1rem' } });
  content.appendChild(host);
  render.renderLoading(host);
  try {
    const exec = await api.getAutomationExecution(id);
    render.renderAutomationExecutionDetail(host, exec);
  } catch (err) {
    render.renderError(host, t('automation.executionsLoadFailed') + ': ' + err.message);
  }
}

// buildAutomationToolbar 构建自动化工具栏。
export function buildAutomationToolbar() {
  const toolbar = $('automation-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => refreshAutomationSubTab() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}

// refreshAutomationSubTab 根据当前子 tab 重新渲染自动化页。
function refreshAutomationSubTab() {
  const sub = state.automationSubTab;
  if (sub === 'rules') { loadAutomationRules(); return; }
  const content = automationContent();
  if (!content) return;
  content.innerHTML = '';
  // 子 tab 切换条
  const subTabs = [
    { key: 'rules',      label: t('automation.rules') },
    { key: 'executions', label: t('automation.executions') },
  ];
  const subBar = render.el('div', { class: 'toolbar' });
  subTabs.forEach((s) => {
    subBar.appendChild(render.el('button', {
      class: 'btn ' + (sub === s.key ? 'btn-secondary' : 'btn-ghost'),
      onclick: () => { state.automationSubTab = s.key; refreshAutomationSubTab(); },
    }, render.el('span', { text: s.label })));
  });
  content.appendChild(subBar);
  if (sub === 'executions') {
    loadAutomationExecutions();
  }
}

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

function webhookContent() { return $('webhook-content'); }

// loadWebhooks 加载 Webhook 列表（含子 tab 切换：列表 / 投递记录）。
export async function loadWebhooks() {
  const content = webhookContent();
  if (!content) return;
  content.innerHTML = '';
  // 子 tab 切换条
  const subTabs = [
    { key: 'list',       label: t('webhook.list'),       onclick: () => { state.webhookSubTab = 'list';       refreshWebhookSubTab(); } },
    { key: 'deliveries', label: t('webhook.deliveries'), onclick: () => { state.webhookSubTab = 'deliveries'; refreshWebhookSubTab(); } },
  ];
  const subBar = render.el('div', { class: 'toolbar' });
  subTabs.forEach((s) => {
    subBar.appendChild(render.el('button', {
      class: 'btn ' + (state.webhookSubTab === s.key ? 'btn-secondary' : 'btn-ghost'),
      onclick: s.onclick,
    }, render.el('span', { text: s.label })));
  });
  content.appendChild(subBar);
  const host = render.el('div', { class: 'content', style: { marginTop: '1rem' } });
  content.appendChild(host);
  if (state.webhookSubTab === 'deliveries') {
    if (state.webhookSelectedId) { loadWebhookDeliveries(state.webhookSelectedId); return; }
    render.renderEmpty(host, t('webhook.noDeliveries'));
    return;
  }
  // Webhook 列表
  const listHost = render.el('div', { id: 'webhooks-list' });
  host.appendChild(listHost);
  render.renderLoading(listHost);
  try {
    const webhooks = await api.getWebhooks();
    state.webhooks = webhooks;
    render.renderWebhooksTable(listHost, webhooks, {
      onEdit: (w) => editWebhook(w),
      onTest: (w) => testWebhookSend(w.id || w.name),
      onDeliveries: (w) => { state.webhookSelectedId = w.id || w.name; state.webhookSubTab = 'deliveries'; refreshWebhookSubTab(); },
      onDelete: (w) => deleteWebhook(w.id || w.name),
    });
  } catch (err) {
    render.renderError(listHost, t('webhook.loadFailed') + ': ' + err.message);
  }
  // 创建 Webhook 表单
  const formHost = render.el('div', { id: 'webhook-form', style: { marginTop: '1rem' } });
  host.appendChild(formHost);
  render.renderWebhookForm(formHost, null, { onSubmit: (data) => createWebhook(data) });
}

// createWebhook 创建 Webhook。
export async function createWebhook(data) {
  if (!data || !data.name) { render.renderToast(t('webhook.nameRequired'), 'warn'); return; }
  if (!data.url) { render.renderToast(t('webhook.urlRequired'), 'warn'); return; }
  if (!data.event) { render.renderToast(t('webhook.eventRequired'), 'warn'); return; }
  try {
    await api.createWebhook(data);
    render.renderToast(t('webhook.created'), 'success');
    state.webhookSubTab = 'list';
    loadWebhooks();
  } catch (err) {
    render.renderToast(t('webhook.createFailed') + ': ' + err.message, 'error');
  }
}

// editWebhook 编辑 Webhook。
export async function editWebhook(wh) {
  if (!wh) return;
  state.webhookEditing = wh;
  const content = webhookContent();
  if (!content) return;
  content.innerHTML = '';
  // 返回按钮
  const backBar = render.el('div', { class: 'toolbar' });
  backBar.appendChild(render.el('button', {
    class: 'btn btn-ghost',
    onclick: () => { state.webhookEditing = null; state.webhookSubTab = 'list'; loadWebhooks(); },
  }, iconEl('back', 14), render.el('span', { text: t('webhook.list') })));
  content.appendChild(backBar);
  const host = render.el('div', { class: 'content', style: { marginTop: '1rem' } });
  content.appendChild(host);
  // 先尝试获取详情，失败则用列表数据
  let detail = wh;
  try {
    detail = await api.getWebhook(wh.id || wh.name);
  } catch (_) { /* 用列表数据 */ }
  render.renderWebhookForm(host, detail, { onSubmit: (data) => updateWebhook(wh.id || wh.name, data) });
}

// updateWebhook 更新 Webhook。
export async function updateWebhook(id, data) {
  if (!id) return;
  if (!data || !data.name) { render.renderToast(t('webhook.nameRequired'), 'warn'); return; }
  try {
    await api.updateWebhook(id, data);
    render.renderToast(t('webhook.updated'), 'success');
    state.webhookEditing = null;
    state.webhookSubTab = 'list';
    loadWebhooks();
  } catch (err) {
    render.renderToast(t('webhook.updateFailed') + ': ' + err.message, 'error');
  }
}

// deleteWebhook 删除 Webhook。
export async function deleteWebhook(id) {
  if (!id) return;
  if (!window.confirm(t('webhook.deleteConfirm'))) return;
  try {
    await api.deleteWebhook(id);
    render.renderToast(t('webhook.deleted'), 'success');
    loadWebhooks();
  } catch (err) {
    render.renderToast(t('webhook.deleteFailed') + ': ' + err.message, 'error');
  }
}

// testWebhookSend 测试发送 Webhook。
export async function testWebhookSend(id) {
  if (!id) return;
  try {
    const result = await api.testWebhook(id);
    const msg = (result && (result.message || result.status)) || t('webhook.testSent');
    render.renderToast(t('webhook.testSent') + ': ' + String(msg).slice(0, 100), 'success');
  } catch (err) {
    render.renderToast(t('webhook.testFailed') + ': ' + err.message, 'error');
  }
}

// loadWebhookDeliveries 加载 Webhook 投递记录。
export async function loadWebhookDeliveries(id) {
  if (!id) return;
  const content = webhookContent();
  if (!content) return;
  const host = render.el('div', { id: 'webhook-deliveries-list', class: 'content', style: { marginTop: '1rem' } });
  content.appendChild(host);
  render.renderLoading(host);
  try {
    const deliveries = await api.getWebhookDeliveries(id);
    render.renderWebhookDeliveriesTable(host, deliveries);
  } catch (err) {
    render.renderError(host, t('webhook.deliveriesLoadFailed') + ': ' + err.message);
  }
}

// buildWebhookToolbar 构建 Webhook 工具栏。
export function buildWebhookToolbar() {
  const toolbar = $('webhook-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => refreshWebhookSubTab() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}

// refreshWebhookSubTab 根据当前子 tab 重新渲染 Webhook 页。
function refreshWebhookSubTab() {
  loadWebhooks();
}

// --- 自定义脚本 ---

function scriptContent() { return $('script-content'); }

// loadScripts 加载脚本列表（含子 tab 切换：列表 / 执行历史）。
export async function loadScripts() {
  const content = scriptContent();
  if (!content) return;
  content.innerHTML = '';
  // 子 tab 切换条
  const subTabs = [
    { key: 'list',       label: t('script.list'),       onclick: () => { state.scriptSubTab = 'list';       refreshScriptSubTab(); } },
    { key: 'executions', label: t('script.executions'), onclick: () => { state.scriptSubTab = 'executions'; refreshScriptSubTab(); } },
  ];
  const subBar = render.el('div', { class: 'toolbar' });
  subTabs.forEach((s) => {
    subBar.appendChild(render.el('button', {
      class: 'btn ' + (state.scriptSubTab === s.key ? 'btn-secondary' : 'btn-ghost'),
      onclick: s.onclick,
    }, render.el('span', { text: s.label })));
  });
  content.appendChild(subBar);
  const host = render.el('div', { class: 'content', style: { marginTop: '1rem' } });
  content.appendChild(host);
  if (state.scriptSubTab === 'executions') {
    if (state.scriptSelectedId) { loadScriptExecutions(state.scriptSelectedId); return; }
    render.renderEmpty(host, t('script.noExecutions'));
    return;
  }
  // 脚本列表
  const listHost = render.el('div', { id: 'scripts-list' });
  host.appendChild(listHost);
  render.renderLoading(listHost);
  try {
    const scripts = await api.getScripts();
    state.scripts = scripts;
    render.renderScriptsTable(listHost, scripts, {
      onEdit: (s) => editScript(s),
      onExecute: (s) => showScriptExecuteForm(s),
      onExecutions: (s) => { state.scriptSelectedId = s.id || s.name; state.scriptSubTab = 'executions'; refreshScriptSubTab(); },
      onDelete: (s) => deleteScript(s.id || s.name),
    });
  } catch (err) {
    render.renderError(listHost, t('script.loadFailed') + ': ' + err.message);
  }
  // 创建脚本表单
  const formHost = render.el('div', { id: 'script-form', style: { marginTop: '1rem' } });
  host.appendChild(formHost);
  render.renderScriptForm(formHost, null, { onSubmit: (data) => createScript(data) });
}

// createScript 创建脚本。
export async function createScript(data) {
  if (!data || !data.name) { render.renderToast(t('script.nameRequired'), 'warn'); return; }
  if (!data.code) { render.renderToast(t('script.codeRequired'), 'warn'); return; }
  try {
    await api.createScript(data);
    render.renderToast(t('script.created'), 'success');
    state.scriptSubTab = 'list';
    loadScripts();
  } catch (err) {
    render.renderToast(t('script.createFailed') + ': ' + err.message, 'error');
  }
}

// editScript 编辑脚本。
export async function editScript(script) {
  if (!script) return;
  state.scriptEditing = script;
  const content = scriptContent();
  if (!content) return;
  content.innerHTML = '';
  // 返回按钮
  const backBar = render.el('div', { class: 'toolbar' });
  backBar.appendChild(render.el('button', {
    class: 'btn btn-ghost',
    onclick: () => { state.scriptEditing = null; state.scriptSubTab = 'list'; loadScripts(); },
  }, iconEl('back', 14), render.el('span', { text: t('script.list') })));
  content.appendChild(backBar);
  const host = render.el('div', { class: 'content', style: { marginTop: '1rem' } });
  content.appendChild(host);
  // 先尝试获取详情，失败则用列表数据
  let detail = script;
  try {
    detail = await api.getScript(script.id || script.name);
  } catch (_) { /* 用列表数据 */ }
  render.renderScriptForm(host, detail, { onSubmit: (data) => updateScript(script.id || script.name, data) });
}

// updateScript 更新脚本。
export async function updateScript(id, data) {
  if (!id) return;
  if (!data || !data.name) { render.renderToast(t('script.nameRequired'), 'warn'); return; }
  if (!data.code) { render.renderToast(t('script.codeRequired'), 'warn'); return; }
  try {
    await api.updateScript(id, data);
    render.renderToast(t('script.updated'), 'success');
    state.scriptEditing = null;
    state.scriptSubTab = 'list';
    loadScripts();
  } catch (err) {
    render.renderToast(t('script.updateFailed') + ': ' + err.message, 'error');
  }
}

// deleteScript 删除脚本。
export async function deleteScript(id) {
  if (!id) return;
  if (!window.confirm(t('script.deleteConfirm'))) return;
  try {
    await api.deleteScript(id);
    render.renderToast(t('script.deleted'), 'success');
    loadScripts();
  } catch (err) {
    render.renderToast(t('script.deleteFailed') + ': ' + err.message, 'error');
  }
}

// showScriptExecuteForm 显示脚本执行表单。
export function showScriptExecuteForm(script) {
  if (!script) return;
  const content = scriptContent();
  if (!content) return;
  content.innerHTML = '';
  // 返回按钮
  const backBar = render.el('div', { class: 'toolbar' });
  backBar.appendChild(render.el('button', {
    class: 'btn btn-ghost',
    onclick: () => { state.scriptSubTab = 'list'; loadScripts(); },
  }, iconEl('back', 14), render.el('span', { text: t('script.list') })));
  content.appendChild(backBar);
  const host = render.el('div', { class: 'content', style: { marginTop: '1rem' } });
  content.appendChild(host);
  render.renderScriptExecuteForm(host, script, { onExecute: (deviceId, params) => executeScriptOnDevice(script.id || script.name, deviceId, params) });
}

// executeScriptOnDevice 在设备上执行脚本。
export async function executeScriptOnDevice(id, deviceId, params) {
  if (!id) return;
  if (!deviceId) { render.renderToast(t('script.deviceRequired'), 'warn'); return; }
  try {
    const result = await api.executeScript(id, { deviceId: deviceId, params: params || {} });
    const msg = (result && (result.message || result.executionId || result.id)) || t('script.execSubmitted');
    render.renderToast(t('script.execSubmitted') + ': ' + String(msg).slice(0, 100), 'success');
  } catch (err) {
    render.renderToast(t('script.execFailed') + ': ' + err.message, 'error');
  }
}

// loadScriptExecutions 加载脚本执行历史。
export async function loadScriptExecutions(id) {
  if (!id) return;
  const content = scriptContent();
  if (!content) return;
  const host = render.el('div', { id: 'script-executions-list', class: 'content', style: { marginTop: '1rem' } });
  content.appendChild(host);
  render.renderLoading(host);
  try {
    const execs = await api.getScriptExecutions(id);
    render.renderScriptExecutionsTable(host, execs, {
      onDetail: (e) => showScriptExecutionDetail(e),
    });
  } catch (err) {
    render.renderError(host, t('script.executionsLoadFailed') + ': ' + err.message);
  }
}

// showScriptExecutionDetail 显示脚本执行详情。
export function showScriptExecutionDetail(exec) {
  if (!exec) return;
  const content = scriptContent();
  if (!content) return;
  content.innerHTML = '';
  // 返回按钮
  const backBar = render.el('div', { class: 'toolbar' });
  backBar.appendChild(render.el('button', {
    class: 'btn btn-ghost',
    onclick: () => { state.scriptSubTab = 'executions'; refreshScriptSubTab(); },
  }, iconEl('back', 14), render.el('span', { text: t('script.executions') })));
  content.appendChild(backBar);
  const host = render.el('div', { class: 'content', style: { marginTop: '1rem' } });
  content.appendChild(host);
  render.renderScriptExecutionDetail(host, exec);
}

// buildScriptToolbar 构建脚本工具栏。
export function buildScriptToolbar() {
  const toolbar = $('script-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => refreshScriptSubTab() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}

// refreshScriptSubTab 根据当前子 tab 重新渲染脚本页。
function refreshScriptSubTab() {
  loadScripts();
}
