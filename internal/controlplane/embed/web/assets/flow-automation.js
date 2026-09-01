// flow-automation.js — 自动化闭环编排（由 flow.js 拆分）。

// flow 子模块 — 由 flow.js 拆分而来。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, \$, pageRoot } from './flow-state.js';

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

