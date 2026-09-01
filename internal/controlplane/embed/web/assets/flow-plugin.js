// flow-plugin.js — 插件市场编排（由 flow.js 拆分）。

// flow 子模块 — 由 flow.js 拆分而来。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, \$, pageRoot } from './flow-state.js';

function pluginContent() { return $('plugin-content'); }

// loadPlugins 加载插件列表。
export async function loadPlugins() {
  const content = pluginContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const plugins = await api.getPlugins();
    state.plugins = plugins;
    render.renderPluginPage(content, plugins, {
      onInstall: (id) => installPlugin(id),
      onUninstall: (id) => uninstallPlugin(id),
      onToggle: (p) => togglePlugin(p),
      onDelete: (id) => deletePlugin(id),
    });
  } catch (err) {
    render.renderError(content, t('plugin.loadFailed') + ': ' + err.message);
  }
}

// createPlugin 打开插件注册表单。
export function createPlugin() {
  const content = pluginContent();
  if (!content) return;
  render.renderPluginForm(content, {
    onSubmit: async (data) => {
      if (!data.name) { render.renderToast(t('plugin.nameRequired'), 'warn'); return; }
      try {
        await api.createPlugin(data);
        render.renderToast(t('plugin.registered'), 'success');
        loadPlugins();
      } catch (err) {
        render.renderToast(t('plugin.registerFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => loadPlugins(),
  });
}

// installPlugin 安装插件。
export async function installPlugin(id) {
  try {
    await api.installPlugin(id);
    render.renderToast(t('plugin.installedDone'), 'success');
    loadPlugins();
  } catch (err) {
    render.renderToast(t('plugin.installFailed') + ': ' + err.message, 'error');
  }
}

// uninstallPlugin 卸载插件。
export async function uninstallPlugin(id) {
  if (!window.confirm(t('plugin.confirmUninstall'))) return;
  try {
    await api.uninstallPlugin(id);
    render.renderToast(t('plugin.uninstalledDone'), 'success');
    loadPlugins();
  } catch (err) {
    render.renderToast(t('plugin.uninstallFailed') + ': ' + err.message, 'error');
  }
}

// togglePlugin 启用/禁用插件。
export async function togglePlugin(p) {
  const id = p && p.id;
  if (!id) return;
  const enabled = String(p.status || '').toLowerCase() !== 'disabled';
  const action = enabled ? 'disable' : 'enable';
  try {
    await api.togglePlugin(id, action);
    render.renderToast(enabled ? t('plugin.disabled') : t('plugin.enabled'), 'success');
    loadPlugins();
  } catch (err) {
    render.renderToast((enabled ? t('plugin.disableFailed') : t('plugin.enableFailed')) + ': ' + err.message, 'error');
  }
}

// deletePlugin 删除插件。
export async function deletePlugin(id) {
  if (!window.confirm(t('plugin.confirmDelete'))) return;
  try {
    await api.deletePlugin(id);
    render.renderToast(t('plugin.deleted'), 'success');
    loadPlugins();
  } catch (err) {
    render.renderToast(t('plugin.deleteFailed') + ': ' + err.message, 'error');
  }
}

// buildPluginToolbar 构建插件工具栏。
export function buildPluginToolbar() {
  const toolbar = $('plugin-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-primary', onclick: () => createPlugin() },
      iconEl('plus', 16), render.el('span', { text: t('plugin.register') })
    )
  );
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => loadPlugins() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}

// --- 计费订阅 ---

