// flow-middleware.js — 中间件部署编排（P1 补齐功能域）。

// flow 子模块 — 中间件部署（模板列表 / 部署表单 / 实例列表）。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, $, pageRoot } from './flow-state.js';

// ============================================================================
// 中间件部署
// ============================================================================

function middlewareContent() { return $('middleware-content'); }

// loadMiddlewareTemplates 加载中间件模板列表。
export async function loadMiddlewareTemplates() {
  const content = middlewareContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const templates = await api.getMiddlewareTemplates();
    state.middleware.templates = templates;
    render.renderMiddlewareTemplatesTable(content, templates, {
      onDeploy: (id) => showMiddlewareDeployForm(id),
    });
  } catch (err) {
    render.renderError(content, t('middleware.loadFailed') + ': ' + err.message);
  }
}

// showMiddlewareDeployForm 打开中间件部署表单。
export function showMiddlewareDeployForm(id) {
  const content = middlewareContent();
  if (!content) return;
  const template = (state.middleware.templates || []).find((tp) => String(tp.id) === String(id));
  state.middleware.selectedId = id;
  render.renderMiddlewareDeployForm(content, id, template, {
    onSubmit: async (data) => {
      if (!data.agentID || !data.agentID.trim()) {
        render.renderToast(t('middleware.agentRequired'), 'warn');
        return;
      }
      try {
        const result = await api.deployMiddleware(id, data);
        const taskID = result && (result.taskID || result.taskId);
        render.renderToast(t('middleware.deploySubmitted') + (taskID ? ' · ' + taskID : ''), 'success');
        // 部署后跳转实例列表
        state.middlewareSubTab = 'instances';
        buildMiddlewareToolbar();
        loadMiddlewareInstances();
      } catch (err) {
        render.renderToast(t('middleware.deployFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => loadMiddlewareTemplates(),
  });
}

// loadMiddlewareInstances 加载中间件实例列表。
export async function loadMiddlewareInstances() {
  const content = middlewareContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const instances = await api.getMiddlewareInstances();
    state.middleware.instances = instances;
    render.renderMiddlewareInstancesTable(content, instances);
  } catch (err) {
    render.renderError(content, t('middleware.instancesLoadFailed') + ': ' + err.message);
  }
}

// refreshMiddlewareSubTab 按当前子 tab 刷新中间件页。
export function refreshMiddlewareSubTab() {
  if (state.middlewareSubTab === 'instances') loadMiddlewareInstances();
  else loadMiddlewareTemplates();
}

// buildMiddlewareToolbar 构建中间件工具栏（子 tab + 刷新）。
export function buildMiddlewareToolbar() {
  const toolbar = $('middleware-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  // 子 tab 切换组
  const subTabs = [
    { key: 'templates', label: t('middleware.tabTemplates'), onActivate: () => loadMiddlewareTemplates() },
    { key: 'instances', label: t('middleware.tabInstances'), onActivate: () => loadMiddlewareInstances() },
  ];
  subTabs.forEach((st) => {
    toolbar.appendChild(
      render.el('button', {
        class: 'btn ' + (state.middlewareSubTab === st.key ? 'btn-secondary' : 'btn-ghost'),
        onclick: () => { state.middlewareSubTab = st.key; st.onActivate(); buildMiddlewareToolbar(); },
      }, render.el('span', { text: st.label }))
    );
  });
  // 刷新
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => refreshMiddlewareSubTab() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}