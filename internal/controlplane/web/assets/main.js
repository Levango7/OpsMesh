// main.js — OpsMesh Phase 1 前端入口（ES module）。
//
// 演进说明（v0.5.0）：原个人版引导页（重定向至 /enterprise/）已升级为
// Phase 1 原生 JS 仪表盘：工单管理 / 监控仪表盘 / SLO 管理 三大 tab。
// 企业版前端（Vue 3）仍可通过顶部"企业版"链接进入。
//
// 契约保留：
//   - 保留 import './theme.js' 语句（TestHandleDashboard_ServesEmbedded 断言 main.js 含 import）；
//   - 保留 ES module 形态。

import './theme.js';

import * as flow from './flow.js';
import * as api from './api.js';
import * as render from './render.js';
import * as i18n from './i18n.js';
import * as icons from './icons.js';

// DOMContentLoaded 时初始化仪表盘（绑定 tab + 加载默认页）。
if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', () => flow.init());
} else {
  flow.init();
}

// ============================================================================
// 导出（供外部调用 / 调试 / 集成测试）
// ============================================================================

// 编排层
export const init = flow.init;
export const switchTab = flow.switchTab;
export const loadTickets = flow.loadTickets;
export const createTicket = flow.createTicket;
export const editTicket = flow.editTicket;
export const showTicketDetail = flow.showTicketDetail;
export const closeTicket = flow.closeTicket;
export const loadSLOs = flow.loadSLOs;
export const createSLO = flow.createSLO;
export const editSLO = flow.editSLO;
export const showSLODetail = flow.showSLODetail;
export const deleteSLO = flow.deleteSLO;
export const loadMetrics = flow.loadMetrics;
export const loadDashboardOverview = flow.loadDashboardOverview;
export const loadDashboardAll = flow.loadDashboardAll;
// Phase 2 编排层
export const loadTrafficPolicies = flow.loadTrafficPolicies;
export const createTrafficPolicy = flow.createTrafficPolicy;
export const enableTrafficPolicy = flow.enableTrafficPolicy;
export const disableTrafficPolicy = flow.disableTrafficPolicy;
export const deleteTrafficPolicy = flow.deleteTrafficPolicy;
export const buildTrafficToolbar = flow.buildTrafficToolbar;
export const loadPipelineTemplates = flow.loadPipelineTemplates;
export const loadPipelineRuns = flow.loadPipelineRuns;
export const loadArgoCDApps = flow.loadArgoCDApps;
export const createPipelineTemplate = flow.createPipelineTemplate;
export const deletePipelineTemplate = flow.deletePipelineTemplate;
export const runPipeline = flow.runPipeline;
export const syncArgoCDApp = flow.syncArgoCDApp;
export const deleteArgoCDApp = flow.deleteArgoCDApp;
export const buildPipelineToolbar = flow.buildPipelineToolbar;
export const loadCanaryReleases = flow.loadCanaryReleases;
export const applyTrafficSplit = flow.applyTrafficSplit;
export const buildCanaryToolbar = flow.buildCanaryToolbar;
export const loadConfigVersions = flow.loadConfigVersions;
export const hotpushConfig = flow.hotpushConfig;
export const canaryConfigPush = flow.canaryConfigPush;
export const buildConfigPushToolbar = flow.buildConfigPushToolbar;
// Phase 3 编排层
export const loadComplianceRules = flow.loadComplianceRules;
export const showComplianceRuleDetail = flow.showComplianceRuleDetail;
export const scanCompliance = flow.scanCompliance;
export const loadComplianceReports = flow.loadComplianceReports;
export const showComplianceReportDetail = flow.showComplianceReportDetail;
export const loadAuditEvents = flow.loadAuditEvents;
export const exportAuditLogs = flow.exportAuditLogs;
export const buildComplianceToolbar = flow.buildComplianceToolbar;
export const loadHAStatus = flow.loadHAStatus;
export const failoverHA = flow.failoverHA;
export const loadBackups = flow.loadBackups;
export const createBackup = flow.createBackup;
export const restoreBackup = flow.restoreBackup;
export const deleteBackup = flow.deleteBackup;
export const buildHAToolbar = flow.buildHAToolbar;
// Phase 4 编排层
export const loadNetworkDevices = flow.loadNetworkDevices;
export const showNetworkDeviceDetail = flow.showNetworkDeviceDetail;
export const createNetworkDevice = flow.createNetworkDevice;
export const deleteNetworkDevice = flow.deleteNetworkDevice;
export const discoverNetwork = flow.discoverNetwork;
export const deployNetworkConfig = flow.deployNetworkConfig;
export const buildNetworkToolbar = flow.buildNetworkToolbar;
export const loadAutomationRules = flow.loadAutomationRules;
export const createAutomationRule = flow.createAutomationRule;
export const editAutomationRule = flow.editAutomationRule;
export const updateAutomationRule = flow.updateAutomationRule;
export const deleteAutomationRule = flow.deleteAutomationRule;
export const enableAutomationRule = flow.enableAutomationRule;
export const disableAutomationRule = flow.disableAutomationRule;
export const testAutomationRule = flow.testAutomationRule;
export const loadAutomationExecutions = flow.loadAutomationExecutions;
export const showAutomationExecutionDetail = flow.showAutomationExecutionDetail;
export const buildAutomationToolbar = flow.buildAutomationToolbar;
// Phase 5 编排层
export const loadGatewayRoutes = flow.loadGatewayRoutes;
export const loadGatewayStats = flow.loadGatewayStats;
export const createGatewayRoute = flow.createGatewayRoute;
export const editGatewayRoute = flow.editGatewayRoute;
export const updateGatewayRoute = flow.updateGatewayRoute;
export const deleteGatewayRoute = flow.deleteGatewayRoute;
export const toggleGatewayRoute = flow.toggleGatewayRoute;
export const buildGatewayToolbar = flow.buildGatewayToolbar;
export const loadWebhooks = flow.loadWebhooks;
export const createWebhook = flow.createWebhook;
export const editWebhook = flow.editWebhook;
export const updateWebhook = flow.updateWebhook;
export const deleteWebhook = flow.deleteWebhook;
export const testWebhookSend = flow.testWebhookSend;
export const loadWebhookDeliveries = flow.loadWebhookDeliveries;
export const buildWebhookToolbar = flow.buildWebhookToolbar;
export const loadScripts = flow.loadScripts;
export const createScript = flow.createScript;
export const editScript = flow.editScript;
export const updateScript = flow.updateScript;
export const deleteScript = flow.deleteScript;
export const showScriptExecuteForm = flow.showScriptExecuteForm;
export const executeScriptOnDevice = flow.executeScriptOnDevice;
export const loadScriptExecutions = flow.loadScriptExecutions;
export const showScriptExecutionDetail = flow.showScriptExecutionDetail;
export const buildScriptToolbar = flow.buildScriptToolbar;

// API 层
export const ApiError = api.ApiError;
export const getTenantID = api.getTenantID;
export const setTenantID = api.setTenantID;
export const getTickets = api.getTickets;
export const getTicket = api.getTicket;
export const updateTicket = api.updateTicket;
export const getSLOs = api.getSLOs;
export const getSLO = api.getSLO;
export const updateSLO = api.updateSLO;
export const deleteSLOApi = api.deleteSLO;
export const getSLOStatus = api.getSLOStatus;
// Phase 2 API 层
export const getTrafficPolicies = api.getTrafficPolicies;
export const createTrafficPolicyApi = api.createTrafficPolicy;
export const deleteTrafficPolicyApi = api.deleteTrafficPolicy;
export const enableTrafficPolicyApi = api.enableTrafficPolicy;
export const disableTrafficPolicyApi = api.disableTrafficPolicy;
export const getPipelineTemplates = api.getPipelineTemplates;
export const createPipelineTemplateApi = api.createPipelineTemplate;
export const deletePipelineTemplateApi = api.deletePipelineTemplate;
export const runPipelineApi = api.runPipeline;
export const getPipelineRuns = api.getPipelineRuns;
export const getArgoCDApps = api.getArgoCDApps;
export const createArgoCDApp = api.createArgoCDApp;
export const deleteArgoCDAppApi = api.deleteArgoCDApp;
export const syncArgoCDAppApi = api.syncArgoCDApp;
export const getCanaryReleases = api.getCanaryReleases;
export const setTrafficSplit = api.setTrafficSplit;
export const getCanaryMetrics = api.getCanaryMetrics;
export const hotpushConfigApi = api.hotpushConfig;
export const canaryConfigApi = api.canaryConfig;
export const getConfigVersions = api.getConfigVersions;
// Phase 3 API 层
export const getComplianceRules = api.getComplianceRules;
export const getComplianceRule = api.getComplianceRule;
export const scanComplianceApi = api.scanCompliance;
export const getComplianceReports = api.getComplianceReports;
export const getComplianceReport = api.getComplianceReport;
export const getAuditEvents = api.getAuditEvents;
export const exportAuditLogsApi = api.exportAuditLogs;
export const getHAStatus = api.getHAStatus;
export const getHAInstances = api.getHAInstances;
export const failoverHAApi = api.failoverHA;
export const getHAHealth = api.getHAHealth;
export const createBackupApi = api.createBackup;
export const listBackups = api.listBackups;
export const restoreBackupApi = api.restoreBackup;
export const deleteBackupApi = api.deleteBackup;
// Phase 4 API 层
export const getNetworkDevices = api.getNetworkDevices;
export const createNetworkDeviceApi = api.createNetworkDevice;
export const getNetworkDevice = api.getNetworkDevice;
export const deleteNetworkDeviceApi = api.deleteNetworkDevice;
export const getNetworkDeviceMetrics = api.getNetworkDeviceMetrics;
export const configNetworkDevice = api.configNetworkDevice;
export const discoverNetworkApi = api.discoverNetwork;
export const getAutomationRules = api.getAutomationRules;
export const createAutomationRuleApi = api.createAutomationRule;
export const getAutomationRule = api.getAutomationRule;
export const updateAutomationRuleApi = api.updateAutomationRule;
export const deleteAutomationRuleApi = api.deleteAutomationRule;
export const enableAutomationRuleApi = api.enableAutomationRule;
export const disableAutomationRuleApi = api.disableAutomationRule;
export const testAutomationRuleApi = api.testAutomationRule;
export const getAutomationExecutions = api.getAutomationExecutions;
export const getAutomationExecution = api.getAutomationExecution;
// Phase 5 API 层
export const getGatewayRoutes = api.getGatewayRoutes;
export const createGatewayRouteApi = api.createGatewayRoute;
export const getGatewayRoute = api.getGatewayRoute;
export const updateGatewayRouteApi = api.updateGatewayRoute;
export const deleteGatewayRouteApi = api.deleteGatewayRoute;
export const toggleGatewayRouteApi = api.toggleGatewayRoute;
export const getGatewayStats = api.getGatewayStats;
export const getWebhooks = api.getWebhooks;
export const createWebhookApi = api.createWebhook;
export const getWebhook = api.getWebhook;
export const updateWebhookApi = api.updateWebhook;
export const deleteWebhookApi = api.deleteWebhook;
export const testWebhookApi = api.testWebhook;
export const getWebhookDeliveries = api.getWebhookDeliveries;
export const getScripts = api.getScripts;
export const createScriptApi = api.createScript;
export const getScript = api.getScript;
export const updateScriptApi = api.updateScript;
export const deleteScriptApi = api.deleteScript;
export const executeScriptApi = api.executeScript;
export const getScriptExecutions = api.getScriptExecutions;

// 渲染层
export const el = render.el;
export const formatTime = render.formatTime;
export const renderToast = render.renderToast;
export const renderTicketTable = render.renderTicketTable;
export const renderTicketForm = render.renderTicketForm;
export const renderTicketDetail = render.renderTicketDetail;
export const renderSLOTable = render.renderSLOTable;
export const renderSLOForm = render.renderSLOForm;
export const renderSLODetail = render.renderSLODetail;
export const renderDashboardOverview = render.renderDashboardOverview;
export const renderMetricsText = render.renderMetricsText;
export const parseMetrics = render.parseMetrics;
// Phase 2 渲染层
export const renderTrafficTable = render.renderTrafficTable;
export const renderTrafficForm = render.renderTrafficForm;
export const renderPipelineTemplates = render.renderPipelineTemplates;
export const renderPipelineRuns = render.renderPipelineRuns;
export const renderPipelineTemplateForm = render.renderPipelineTemplateForm;
export const renderArgoCDApps = render.renderArgoCDApps;
export const renderCanaryList = render.renderCanaryList;
export const renderCanarySplitPanel = render.renderCanarySplitPanel;
export const renderCanaryMetrics = render.renderCanaryMetrics;
export const renderConfigHotpushForm = render.renderConfigHotpushForm;
export const renderConfigCanaryForm = render.renderConfigCanaryForm;
export const renderConfigVersions = render.renderConfigVersions;
export const renderApiEndpoints = render.renderApiEndpoints;
// Phase 3 渲染层
export const renderComplianceRulesTable = render.renderComplianceRulesTable;
export const renderComplianceRuleDetail = render.renderComplianceRuleDetail;
export const renderComplianceScanForm = render.renderComplianceScanForm;
export const renderComplianceReport = render.renderComplianceReport;
export const renderComplianceReportsList = render.renderComplianceReportsList;
export const renderAuditQueryForm = render.renderAuditQueryForm;
export const renderAuditEventsTable = render.renderAuditEventsTable;
export const renderHAStatus = render.renderHAStatus;
export const renderHAInstancesTable = render.renderHAInstancesTable;
export const renderHAHealth = render.renderHAHealth;
export const renderBackupsTable = render.renderBackupsTable;
export const renderCreateBackupForm = render.renderCreateBackupForm;
// Phase 4 渲染层
export const renderNetworkDevicesTable = render.renderNetworkDevicesTable;
export const renderNetworkDeviceForm = render.renderNetworkDeviceForm;
export const renderNetworkDeviceMetrics = render.renderNetworkDeviceMetrics;
export const renderNetworkDiscoverForm = render.renderNetworkDiscoverForm;
export const renderNetworkDiscoverResult = render.renderNetworkDiscoverResult;
export const renderNetworkConfigForm = render.renderNetworkConfigForm;
export const renderAutomationRulesTable = render.renderAutomationRulesTable;
export const renderAutomationRuleForm = render.renderAutomationRuleForm;
export const renderAutomationExecutionsTable = render.renderAutomationExecutionsTable;
export const renderAutomationExecutionDetail = render.renderAutomationExecutionDetail;
// Phase 5 渲染层
export const renderGatewayStats = render.renderGatewayStats;
export const renderGatewayRoutesTable = render.renderGatewayRoutesTable;
export const renderGatewayRouteForm = render.renderGatewayRouteForm;
export const renderWebhooksTable = render.renderWebhooksTable;
export const renderWebhookForm = render.renderWebhookForm;
export const renderWebhookDeliveriesTable = render.renderWebhookDeliveriesTable;
export const renderScriptsTable = render.renderScriptsTable;
export const renderScriptForm = render.renderScriptForm;
export const renderScriptExecuteForm = render.renderScriptExecuteForm;
export const renderScriptExecutionsTable = render.renderScriptExecutionsTable;
export const renderScriptExecutionDetail = render.renderScriptExecutionDetail;

// i18n
export const t = i18n.t;
export const getLang = i18n.getLang;
export const setLang = i18n.setLang;

// icons
export const ICONS = icons.ICONS;
export const iconHtml = icons.iconHtml;
export const iconEl = icons.iconEl;
