// flow.js — OpsMesh 前端编排层（聚合入口）。
//
// 演进说明：原单文件 109KB 已按业务域拆分为多个 ES module：
//   - flow-state.js          共享状态（state/$/pageRoot）
//   - flow-ticket.js         工单
//   - flow-slo.js            SLO
//   - flow-dashboard.js      仪表盘
//   - flow-traffic.js        服务治理
//   - flow-pipeline.js       CI/CD 流水线
//   - flow-canary.js         灰度发布
//   - flow-config-push.js    配置热推
//   - flow-compliance.js     安全合规
//   - flow-ha.js             高可用
//   - flow-network.js        网络管理
//   - flow-automation.js     自动化闭环
//   - flow-gateway.js        API 网关
//   - flow-webhook.js        Webhook
//   - flow-script.js         自定义脚本
//   - flow-tenant.js         租户
//   - flow-apikey.js         API Key
//   - flow-plugin.js         插件
//   - flow-billing.js        计费
//   - flow-platform.js       平台配置
//
// 本文件保留 init/switchTab/refreshCurrentPage（跨域编排），
// 并 re-export 所有子模块的导出函数，保持原有导出契约不变。

import { state, $ } from './flow-state.js';
import { iconEl } from './icons.js';
import { setLang as i18nSetLang } from './i18n.js';

// 各功能域子模块（命名空间导入）
import * as flowTicket      from './flow-ticket.js';
import * as flowSLO         from './flow-slo.js';
import * as flowDashboard   from './flow-dashboard.js';
import * as flowTraffic     from './flow-traffic.js';
import * as flowPipeline    from './flow-pipeline.js';
import * as flowCanary      from './flow-canary.js';
import * as flowConfigPush  from './flow-config-push.js';
import * as flowCompliance  from './flow-compliance.js';
import * as flowHA          from './flow-ha.js';
import * as flowNetwork     from './flow-network.js';
import * as flowAutomation   from './flow-automation.js';
import * as flowGateway     from './flow-gateway.js';
import * as flowWebhook     from './flow-webhook.js';
import * as flowScript      from './flow-script.js';
import * as flowTenant      from './flow-tenant.js';
import * as flowAPIKey      from './flow-apikey.js';
import * as flowPlugin      from './flow-plugin.js';
import * as flowBilling     from './flow-billing.js';
import * as flowPlatform    from './flow-platform.js';

// ============================================================================
// Tab 切换
// ============================================================================

export function switchTab(tab) {
  const validTabs = ['tickets', 'dashboard', 'slo', 'traffic', 'pipeline', 'canary', 'config-push', 'compliance', 'ha', 'network-mgmt', 'automation', 'gateway', 'webhook', 'script', 'tenant', 'apikey', 'plugin', 'billing', 'platform'];
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
  if (tab === 'tickets' && state.tickets.length === 0) flowTicket.loadTickets();
  if (tab === 'dashboard' && !state._dashboardLoaded) flowDashboard.loadDashboardAll();
  if (tab === 'slo' && state.slos.length === 0) flowSLO.loadSLOs();
  // Phase 2 懒加载
  if (tab === 'traffic' && state.trafficPolicies.length === 0) flowTraffic.loadTrafficPolicies();
  if (tab === 'pipeline' && state.pipelineTemplates.length === 0) flowPipeline.loadPipelineTemplates();
  if (tab === 'canary' && state.canaryReleases.length === 0) flowCanary.loadCanaryReleases();
  if (tab === 'config-push' && !state._configPushLoaded) flowConfigPush.loadConfigVersions();
  // Phase 3 懒加载
  if (tab === 'compliance' && state.complianceRules.length === 0) flowCompliance.loadComplianceRules();
  if (tab === 'ha' && !state._haLoaded) flowHA.loadHAStatus();
  // Phase 4 懒加载
  if (tab === 'network-mgmt' && state.networkDevices.length === 0) flowNetwork.loadNetworkDevices();
  if (tab === 'automation' && state.automationRules.length === 0) flowAutomation.loadAutomationRules();
  // Phase 5 懒加载
  if (tab === 'gateway' && state.gatewayRoutes.length === 0) flowGateway.loadGatewayRoutes();
  if (tab === 'webhook' && state.webhooks.length === 0) flowWebhook.loadWebhooks();
  if (tab === 'script' && state.scripts.length === 0) flowScript.loadScripts();
  // Phase 6 懒加载
  if (tab === 'tenant' && state.tenants.length === 0) flowTenant.loadTenants();
  if (tab === 'apikey' && state.apikeys.length === 0) flowAPIKey.loadAPIKeys();
  if (tab === 'plugin' && state.plugins.length === 0) flowPlugin.loadPlugins();
  if (tab === 'billing' && !state._billingLoaded) flowBilling.loadBilling();
  if (tab === 'platform' && !state._platformLoaded) flowPlatform.loadPlatform();
}

// ============================================================================
// 初始化与刷新
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
  flowTicket.buildTicketsToolbar();
  flowSLO.buildSLOToolbar();
  flowDashboard.buildDashboardToolbar();
  flowTraffic.buildTrafficToolbar();
  flowPipeline.buildPipelineToolbar();
  flowCanary.buildCanaryToolbar();
  flowConfigPush.buildConfigPushToolbar();
  flowCompliance.buildComplianceToolbar();
  flowHA.buildHAToolbar();
  flowNetwork.buildNetworkToolbar();
  flowAutomation.buildAutomationToolbar();
  flowGateway.buildGatewayToolbar();
  flowWebhook.buildWebhookToolbar();
  flowScript.buildScriptToolbar();
  flowTenant.buildTenantToolbar();
  flowAPIKey.buildAPIKeyToolbar();
  flowPlugin.buildPluginToolbar();
  flowBilling.buildBillingToolbar();
  flowPlatform.buildPlatformToolbar();

  // 绑定 tab 切换
  document.querySelectorAll('.tab-btn').forEach((btn) => {
    btn.addEventListener('click', () => switchTab(btn.dataset.tab));
  });

  // 默认加载工单列表
  flowTicket.loadTickets();
}

function refreshCurrentPage() {
  // 重建工具栏（含翻译文本）
  flowTicket.buildTicketsToolbar();
  flowSLO.buildSLOToolbar();
  flowDashboard.buildDashboardToolbar();
  flowTraffic.buildTrafficToolbar();
  flowPipeline.buildPipelineToolbar();
  flowCanary.buildCanaryToolbar();
  flowConfigPush.buildConfigPushToolbar();
  flowCompliance.buildComplianceToolbar();
  flowHA.buildHAToolbar();
  flowNetwork.buildNetworkToolbar();
  flowAutomation.buildAutomationToolbar();
  flowGateway.buildGatewayToolbar();
  flowWebhook.buildWebhookToolbar();
  flowScript.buildScriptToolbar();
  flowTenant.buildTenantToolbar();
  flowAPIKey.buildAPIKeyToolbar();
  flowPlugin.buildPluginToolbar();
  flowBilling.buildBillingToolbar();
  flowPlatform.buildPlatformToolbar();
  // 重新加载当前页数据
  if (state.currentTab === 'tickets') flowTicket.loadTickets();
  else if (state.currentTab === 'dashboard') flowDashboard.loadDashboardAll();
  else if (state.currentTab === 'slo') flowSLO.loadSLOs();
  else if (state.currentTab === 'traffic') flowTraffic.loadTrafficPolicies();
  else if (state.currentTab === 'pipeline') flowPipeline.refreshPipelineSubTab();
  else if (state.currentTab === 'canary') flowCanary.loadCanaryReleases();
  else if (state.currentTab === 'config-push') flowConfigPush.loadConfigVersions('');
  else if (state.currentTab === 'compliance') flowCompliance.refreshComplianceSubTab();
  else if (state.currentTab === 'ha') flowHA.refreshHASubTab();
  else if (state.currentTab === 'network-mgmt') flowNetwork.refreshNetworkSubTab();
  else if (state.currentTab === 'automation') flowAutomation.refreshAutomationSubTab();
  else if (state.currentTab === 'gateway') flowGateway.refreshGatewaySubTab();
  else if (state.currentTab === 'webhook') flowWebhook.refreshWebhookSubTab();
  else if (state.currentTab === 'script') flowScript.refreshScriptSubTab();
  else if (state.currentTab === 'tenant') flowTenant.loadTenants();
  else if (state.currentTab === 'apikey') flowAPIKey.loadAPIKeys();
  else if (state.currentTab === 'plugin') flowPlugin.loadPlugins();
  else if (state.currentTab === 'billing') flowBilling.loadBilling();
  else if (state.currentTab === 'platform') flowPlatform.loadPlatform();
}

// ============================================================================
// 聚合 re-export（保持原有导出契约不变）
// ============================================================================

export * from './flow-ticket.js';
export * from './flow-slo.js';
export * from './flow-dashboard.js';
export * from './flow-traffic.js';
export * from './flow-pipeline.js';
export * from './flow-canary.js';
export * from './flow-config-push.js';
export * from './flow-compliance.js';
export * from './flow-ha.js';
export * from './flow-network.js';
export * from './flow-automation.js';
export * from './flow-gateway.js';
export * from './flow-webhook.js';
export * from './flow-script.js';
export * from './flow-tenant.js';
export * from './flow-apikey.js';
export * from './flow-plugin.js';
export * from './flow-billing.js';
export * from './flow-platform.js';

// init/switchTab 也需要导出
export { init, switchTab };
