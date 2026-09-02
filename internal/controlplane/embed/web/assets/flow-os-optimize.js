// flow-os-optimize.js — OS 优化编排（P1 补齐功能域）。

// flow 子模块 — OS 优化（模板列表 / 执行表单 / 执行结果）。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, $, pageRoot } from './flow-state.js';

// ============================================================================
// OS 优化
// ============================================================================

function osOptimizeContent() { return $('os-optimize-content'); }

// loadOSTemplates 加载 OS 优化模板列表。
export async function loadOSTemplates() {
  const content = osOptimizeContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const templates = await api.getOSTemplates();
    state.osOptimize.templates = templates;
    render.renderOSTemplatesTable(content, templates, {
      onExecute: (id) => showOSTemplateExecForm(id),
    });
  } catch (err) {
    render.renderError(content, t('osOptimize.loadFailed') + ': ' + err.message);
  }
}

// showOSTemplateExecForm 打开 OS 优化模板执行表单。
export function showOSTemplateExecForm(id) {
  const content = osOptimizeContent();
  if (!content) return;
  const template = (state.osOptimize.templates || []).find((tp) => String(tp.id) === String(id));
  state.osOptimize.selectedId = id;
  render.renderOSTemplateExecForm(content, id, template, {
    onSubmit: async (data) => {
      if (!data.agentID || !data.agentID.trim()) {
        render.renderToast(t('osOptimize.agentRequired'), 'warn');
        return;
      }
      try {
        const result = await api.executeOSTemplate(id, data);
        render.renderToast(t('osOptimize.execSubmitted'), 'success');
        showOSExecResult(id, result);
      } catch (err) {
        render.renderToast(t('osOptimize.execFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => loadOSTemplates(),
  });
}

// showOSExecResult 显示执行结果。
export function showOSExecResult(templateID, result) {
  const content = osOptimizeContent();
  if (!content) return;
  render.renderOSExecResult(content, templateID, result, {
    onBack: () => loadOSTemplates(),
  });
}

// buildOSOptimizeToolbar 构建 OS 优化工具栏（刷新）。
export function buildOSOptimizeToolbar() {
  const toolbar = $('os-optimize-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  // 刷新
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => loadOSTemplates() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}