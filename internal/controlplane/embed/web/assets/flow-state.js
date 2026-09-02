// flow-state.js — OpsMesh 前端编排层共享状态（由 flow.js 拆分）。
//
// 职责：提供跨功能域共享的 state 对象与 DOM 辅助函数（$/pageRoot）。
// 设计要点：state 为单例，所有子模块共享同一引用。

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
  // Phase 6 状态（平台化管理）
  tenants: [],
  apikeys: [],
  plugins: [],
  billingPlans: [],
  billingSubs: [],
  billingInvoices: [],
  billingSubTab: 'plans', // plans | subscriptions | invoices
  platformConfig: null,
  platformHealth: null,
  platformMetrics: null,
  // P0 补齐功能域状态
  // 设备管理
  devices: [],
  agents: [],
  devicesSubTab: 'devices', // devices | agents
  // 任务执行
  tasks: [],
  taskFilter: { status: '' },
  // 告警管理
  alerts: [],
  alertFilter: { severity: '', state: '' },
  // 告警规则管理
  alertRules: [],
  alertRulesEngine: [],
  alertSilences: [],
  alertRulesSubTab: 'rules', // rules | engine | silences
  // 批量执行
  batches: [],
  batchDetail: null,
  batchSelectedId: null,
  batchSubTab: 'list', // list | exec | create | status
};


// ============================================================================
// 通用辅助
// ============================================================================

function $(id) { return document.getElementById(id); }

// pageRoot(tab) 返回当前 tab 的根容器。
export function pageRoot(tab) {
  return $('page-' + (tab || state.currentTab));
}

export { state, $ };
