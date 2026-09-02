// flow-script.js — 自定义脚本编排（由 flow.js 拆分）。

// flow 子模块 — 由 flow.js 拆分而来。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, $ } from './flow-state.js';

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

