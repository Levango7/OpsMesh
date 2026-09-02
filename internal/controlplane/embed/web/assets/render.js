// render.js — OpsMesh 前端渲染层（聚合入口）。
//
// 演进说明：原单文件 140KB 已按功能域拆分为多个 ES module：
//   - render-common.js       公共工具（el/formatTime/Badge/表单辅助）
//   - render-ticket.js       工单渲染
//   - render-slo.js          SLO 渲染
//   - render-dashboard.js    仪表盘渲染
//   - render-traffic.js      服务治理渲染
//   - render-pipeline.js     CI/CD 流水线渲染
//   - render-canary.js       灰度发布渲染
//   - render-config-push.js  配置热推渲染
//   - render-api-endpoints.js API 端点汇总
//   - render-compliance.js   安全合规渲染
//   - render-ha.js           高可用渲染
//   - render-network.js      网络管理渲染
//   - render-automation.js   自动化闭环渲染
//   - render-tenant.js       租户管理渲染
//   - render-apikey.js       API Key 渲染
//   - render-plugin.js       插件市场渲染
//   - render-billing.js      计费订阅渲染
//   - render-platform.js     平台配置渲染
//   - render-gateway.js      API 网关渲染
//   - render-webhook.js      Webhook 渲染
//   - render-script.js       自定义脚本渲染
//
// 本文件仅做 re-export，保持原有导出契约不变（main.js 通过 import * as render 引入）。
// 设计要点：
//   - 用户输入字段用 textContent（防 XSS）；图标用 innerHTML（受控 SVG）；
//   - 表格语义化 <table>，表单语义化 <form>，card 布局；
//   - 状态/优先级/分类用 badge 样式（CSS 类 status-xxx/priority-xxx/category-xxx）；
//   - 时间统一 formatTime 格式化为 "YYYY-MM-DD HH:mm"。

// ============================================================================
// 公共工具（仅 re-export 原导出契约中的符号；badge/detailItem/fieldRow/formatNumber
// 为内部辅助，保留在 render-common.js 供子模块导入，不对外暴露）
// ============================================================================
export {
  el, formatTime,
  renderLoading, renderError, renderEmpty, renderToast,
  statusBadge, priorityBadge, categoryBadge, sloStatusBadge,
} from './render-common.js';

// ============================================================================
// 工单 / SLO / 仪表盘
// ============================================================================
export { renderTicketTable, renderTicketForm, renderTicketDetail } from './render-ticket.js';
export { renderSLOTable, renderSLOForm, renderSLODetail } from './render-slo.js';
export { renderDashboardOverview, parseMetrics, renderMetricsText } from './render-dashboard.js';

// ============================================================================
// Phase 2：服务治理 / CI/CD / 灰度 / 配置热推 / API 端点
// ============================================================================
export { renderTrafficTable, renderTrafficForm } from './render-traffic.js';
export { renderPipelineTemplates, renderPipelineRuns, renderPipelineTemplateForm, renderArgoCDApps } from './render-pipeline.js';
export { renderCanaryList, renderCanarySplitPanel, renderCanaryMetrics } from './render-canary.js';
export { renderConfigHotpushForm, renderConfigCanaryForm, renderConfigVersions } from './render-config-push.js';
export { renderApiEndpoints } from './render-api-endpoints.js';

// ============================================================================
// Phase 3：安全合规 / 高可用
// ============================================================================
export {
  renderComplianceRulesTable, renderComplianceRuleDetail, renderComplianceScanForm,
  renderComplianceReport, renderComplianceReportsList, renderAuditQueryForm, renderAuditEventsTable,
} from './render-compliance.js';
export { renderHAStatus, renderHAInstancesTable, renderHAHealth, renderBackupsTable, renderCreateBackupForm } from './render-ha.js';

// ============================================================================
// Phase 4：网络管理 / 自动化闭环
// ============================================================================
export {
  renderNetworkDevicesTable, renderNetworkDeviceForm, renderNetworkDeviceMetrics,
  renderNetworkDiscoverForm, renderNetworkDiscoverResult, renderNetworkConfigForm,
} from './render-network.js';
export {
  renderAutomationRulesTable, renderAutomationRuleForm,
  renderAutomationExecutionsTable, renderAutomationExecutionDetail,
} from './render-automation.js';

// ============================================================================
// Phase 6：平台化管理（租户 / API Key / 插件 / 计费 / 平台）
// ============================================================================
export { renderTenantPage, renderTenantForm } from './render-tenant.js';
export { renderAPIKeyPage, renderAPIKeyForm, renderAPIKeyGenerated } from './render-apikey.js';
export { renderPluginPage, renderPluginForm } from './render-plugin.js';
export { renderBillingPage, renderBillingPlanForm, renderSubscriptionForm } from './render-billing.js';
export { renderPlatformPage } from './render-platform.js';

// ============================================================================
// Phase 5：扩展能力（API 网关 / Webhook / 自定义脚本）
// ============================================================================
export { renderGatewayStats, renderGatewayRoutesTable, renderGatewayRouteForm } from './render-gateway.js';
export { renderWebhooksTable, renderWebhookForm, renderWebhookDeliveriesTable } from './render-webhook.js';
export {
  renderScriptsTable, renderScriptForm, renderScriptExecuteForm,
  renderScriptExecutionsTable, renderScriptExecutionDetail,
} from './render-script.js';

// ============================================================================
// P0 补齐功能域：设备管理 / 任务执行 / 告警管理 / 告警规则管理 / 批量执行
// ============================================================================
export { renderDevicesTable, renderDeviceMetrics, renderAgentsTable } from './render-devices.js';
export {
  renderTasksTable, renderTaskForm, renderTaskResult,
} from './render-tasks.js';
export {
  renderAlertsTable, renderAlertSilenceForm, renderAlertDetail,
  alertSeverityBadge, alertStateBadge,
} from './render-alerts.js';
export {
  renderAlertRulesTable, renderAlertRuleForm,
  renderAlertEngineTable, renderAlertEngineForm,
  renderAlertSilencesTable, renderAlertSilenceCreateForm,
} from './render-alert-rules.js';
export {
  renderBatchExecForm, renderBatchCreateForm, renderBatchStatus, renderBatchList,
} from './render-batch.js';
