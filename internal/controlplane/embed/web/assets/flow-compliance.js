// flow-compliance.js — 安全合规编排（由 flow.js 拆分）。

// flow 子模块 — 由 flow.js 拆分而来。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, \$, pageRoot } from './flow-state.js';

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

