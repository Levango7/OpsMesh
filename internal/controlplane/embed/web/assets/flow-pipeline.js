// flow-pipeline.js — CI/CD 流水线编排（由 flow.js 拆分）。

// flow 子模块 — 由 flow.js 拆分而来。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, \$, pageRoot } from './flow-state.js';

// ============================================================================
// Phase 2：CI/CD 流水线
// ============================================================================

function pipelineContent() { return $('pipeline-content'); }

// loadPipelineTemplates 加载流水线模板列表。
export async function loadPipelineTemplates() {
  const content = pipelineContent();
  if (!content) return;
  state.pipelineSubTab = 'templates';
  render.renderLoading(content);
  try {
    const templates = await api.getPipelineTemplates();
    state.pipelineTemplates = templates;
    render.renderPipelineTemplates(content, templates, {
      onRun: (id) => runPipeline(id),
      onDelete: (id) => deletePipelineTemplate(id),
    });
  } catch (err) {
    render.renderError(content, t('pipeline.loadFailed') + ': ' + err.message);
  }
}

// loadPipelineRuns 加载流水线运行记录。
export async function loadPipelineRuns() {
  const content = pipelineContent();
  if (!content) return;
  state.pipelineSubTab = 'runs';
  render.renderLoading(content);
  try {
    const runs = await api.getPipelineRuns();
    state.pipelineRuns = runs;
    render.renderPipelineRuns(content, runs);
  } catch (err) {
    render.renderError(content, t('pipeline.loadFailed') + ': ' + err.message);
  }
}

// loadArgoCDApps 加载 ArgoCD 应用列表。
export async function loadArgoCDApps() {
  const content = pipelineContent();
  if (!content) return;
  state.pipelineSubTab = 'argocd';
  render.renderLoading(content);
  try {
    const apps = await api.getArgoCDApps();
    state.argocdApps = apps;
    render.renderArgoCDApps(content, apps, {
      onSync: (id) => syncArgoCDApp(id),
      onDelete: (id) => deleteArgoCDApp(id),
    });
  } catch (err) {
    render.renderError(content, t('pipeline.loadFailed') + ': ' + err.message);
  }
}

// createPipelineTemplate 打开创建流水线模板表单。
export function createPipelineTemplate() {
  const content = pipelineContent();
  if (!content) return;
  render.renderPipelineTemplateForm(content, {
    onSubmit: async (data) => {
      if (!data.name) { render.renderToast(t('pipeline.nameRequired'), 'warn'); return; }
      try {
        await api.createPipelineTemplate(data);
        render.renderToast(t('pipeline.created'), 'success');
        loadPipelineTemplates();
      } catch (err) {
        render.renderToast(t('pipeline.createFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => loadPipelineTemplates(),
  });
}

// deletePipelineTemplate 删除流水线模板。
export async function deletePipelineTemplate(id) {
  if (!window.confirm(t('pipeline.confirmDelete'))) return;
  try {
    await api.deletePipelineTemplate(id);
    render.renderToast(t('pipeline.deleted'), 'success');
    loadPipelineTemplates();
  } catch (err) {
    render.renderToast(t('pipeline.deleteFailed') + ': ' + err.message, 'error');
  }
}

// runPipeline 触发流水线运行。
export async function runPipeline(id) {
  try {
    await api.runPipeline(id);
    render.renderToast(t('pipeline.running'), 'success');
    loadPipelineRuns();
  } catch (err) {
    render.renderToast(t('pipeline.runFailed') + ': ' + err.message, 'error');
  }
}

// syncArgoCDApp 同步 ArgoCD 应用。
export async function syncArgoCDApp(id) {
  try {
    await api.syncArgoCDApp(id);
    render.renderToast(t('pipeline.argoSynced'), 'success');
    loadArgoCDApps();
  } catch (err) {
    render.renderToast(t('pipeline.argoSyncFailed') + ': ' + err.message, 'error');
  }
}

// deleteArgoCDApp 删除 ArgoCD 应用。
export async function deleteArgoCDApp(id) {
  if (!window.confirm(t('pipeline.confirmDelete'))) return;
  try {
    await api.deleteArgoCDApp(id);
    render.renderToast(t('pipeline.deleted'), 'success');
    loadArgoCDApps();
  } catch (err) {
    render.renderToast(t('pipeline.deleteFailed') + ': ' + err.message, 'error');
  }
}

// buildPipelineToolbar 构建流水线工具栏（含子 tab 切换）。
export function buildPipelineToolbar() {
  const toolbar = $('pipeline-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  // 子 tab：模板 / 运行记录 / ArgoCD
  const subTabs = [
    { key: 'templates', label: t('pipeline.templates'), onclick: () => loadPipelineTemplates() },
    { key: 'runs',      label: t('pipeline.runs'),      onclick: () => loadPipelineRuns() },
    { key: 'argocd',    label: t('pipeline.argoApps'),  onclick: () => loadArgoCDApps() },
  ];
  const group = render.el('div', { class: 'filter-group' });
  subTabs.forEach((s) => {
    group.appendChild(
      render.el('button', {
        class: 'btn ' + (state.pipelineSubTab === s.key ? 'btn-secondary' : 'btn-ghost'),
        onclick: s.onclick,
      }, render.el('span', { text: s.label }))
    );
  });
  toolbar.appendChild(group);
  // 创建模板按钮（仅 templates 子 tab 显示）
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-primary', onclick: () => createPipelineTemplate() },
      iconEl('plus', 16), render.el('span', { text: t('pipeline.create') })
    )
  );
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => refreshPipelineSubTab() },
      iconEl('refresh', 14)
    )
  );
}

function refreshPipelineSubTab() {
  if (state.pipelineSubTab === 'templates') loadPipelineTemplates();
  else if (state.pipelineSubTab === 'runs') loadPipelineRuns();
  else if (state.pipelineSubTab === 'argocd') loadArgoCDApps();
  else loadPipelineTemplates();
}

