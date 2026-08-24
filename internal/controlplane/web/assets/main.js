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

// i18n
export const t = i18n.t;
export const getLang = i18n.getLang;
export const setLang = i18n.setLang;

// icons
export const ICONS = icons.ICONS;
export const iconHtml = icons.iconHtml;
export const iconEl = icons.iconEl;
