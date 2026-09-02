// flow-logs.js — 日志检索编排（P1 补齐功能域）。

// flow 子模块 — 日志检索（检索表单 + 结果列表）。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, $, pageRoot } from './flow-state.js';

// ============================================================================
// 日志检索
// ============================================================================

function logsContent() { return $('logs-content'); }

// loadLogs 加载日志检索页（表单 + 空结果）。
export function loadLogs() {
  const content = logsContent();
  if (!content) return;
  content.innerHTML = '';
  // 渲染检索表单
  const formWrap = render.el('div', { class: 'logs-form-wrap' });
  content.appendChild(formWrap);
  render.renderLogsSearchForm(formWrap, state.logs.filter, {
    onSearch: (data) => searchLogs(data),
    onReset: () => resetLogsSearch(),
  });
  // 渲染结果区
  const resultWrap = render.el('div', { class: 'logs-result-wrap' });
  content.appendChild(resultWrap);
  render.renderEmpty(resultWrap, t('logs.searchHint'));
}

// searchLogs 执行日志检索。
export async function searchLogs(filter) {
  state.logs.filter = Object.assign(state.logs.filter || {}, filter);
  const content = logsContent();
  if (!content) return;
  const resultWrap = content.querySelector('.logs-result-wrap');
  if (!resultWrap) return;
  render.renderLoading(resultWrap);
  try {
    const resp = await api.searchLogs(state.logs.filter);
    const entries = (resp && resp.logs) ? resp.logs : (Array.isArray(resp) ? resp : []);
    const total = (resp && resp.total != null) ? resp.total : entries.length;
    state.logs.entries = entries;
    state.logs.total = total;
    render.renderLogsTable(resultWrap, entries, total);
  } catch (err) {
    render.renderError(resultWrap, t('logs.loadFailed') + ': ' + err.message);
  }
}

// resetLogsSearch 重置检索条件并清空结果。
export function resetLogsSearch() {
  state.logs.filter = { query: '', level: '', from: '', to: '', limit: '100' };
  loadLogs();
}

// buildLogsToolbar 构建日志工具栏（刷新）。
export function buildLogsToolbar() {
  const toolbar = $('logs-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  // 刷新
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => loadLogs() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}