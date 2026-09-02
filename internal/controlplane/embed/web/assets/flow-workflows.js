// flow-workflows.js — 作业编排编排（P1 补齐功能域）。

// flow 子模块 — 作业编排（工作流列表 / 创建 / 运行 / 查看执行状态）。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, $ } from './flow-state.js';

// ============================================================================
// 作业编排
// ============================================================================

function workflowsContent() { return $('workflows-content'); }

// loadWorkflows 加载工作流列表。
export async function loadWorkflows() {
  const content = workflowsContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const workflows = await api.getWorkflows();
    state.workflows.list = workflows;
    render.renderWorkflowsTable(content, workflows, {
      onRun: (id) => runWorkflow(id),
      onStatus: (id) => showWorkflowStatus(id),
    });
  } catch (err) {
    render.renderError(content, t('workflows.loadFailed') + ': ' + err.message);
  }
}

// showWorkflowForm 打开创建工作流表单。
export function showWorkflowForm() {
  const content = workflowsContent();
  if (!content) return;
  render.renderWorkflowForm(content, {
    onSubmit: async (data) => {
      if (!data.name || !data.name.trim()) {
        render.renderToast(t('workflows.nameRequired'), 'warn');
        return;
      }
      try {
        await api.createWorkflow(data);
        render.renderToast(t('workflows.created'), 'success');
        loadWorkflows();
      } catch (err) {
        render.renderToast(t('workflows.createFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => loadWorkflows(),
  });
}

// runWorkflow 运行工作流。
export async function runWorkflow(id) {
  try {
    const result = await api.runWorkflow(id);
    const taskID = result && (result.taskID || result.taskId);
    render.renderToast(t('workflows.runSubmitted') + (taskID ? ' · ' + taskID : ''), 'success');
    // 运行后跳转状态视图
    if (taskID || id) {
      state.workflows.selectedId = id;
      showWorkflowStatus(id);
    } else {
      loadWorkflows();
    }
  } catch (err) {
    render.renderToast(t('workflows.runFailed') + ': ' + err.message, 'error');
  }
}

// showWorkflowStatus 查看工作流执行状态。
export async function showWorkflowStatus(id) {
  const content = workflowsContent();
  if (!content) return;
  const wid = id || state.workflows.selectedId;
  if (!wid) {
    render.renderEmpty(content, t('workflows.noSelected'));
    return;
  }
  state.workflows.selectedId = wid;
  render.renderLoading(content);
  try {
    const status = await api.getWorkflowStatus(wid);
    state.workflows.currentStatus = status;
    render.renderWorkflowStatus(content, wid, status, {
      onBack: () => { state.workflows.selectedId = null; loadWorkflows(); },
      onRefresh: () => showWorkflowStatus(wid),
    });
  } catch (err) {
    render.renderError(content, t('workflows.statusLoadFailed') + ': ' + err.message);
  }
}

// buildWorkflowsToolbar 构建工作流工具栏（创建按钮 + 刷新）。
export function buildWorkflowsToolbar() {
  const toolbar = $('workflows-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  // 创建按钮
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-primary', onclick: () => showWorkflowForm() },
      iconEl('plus', 16), render.el('span', { text: t('workflows.create') })
    )
  );
  // 刷新
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => loadWorkflows() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}
