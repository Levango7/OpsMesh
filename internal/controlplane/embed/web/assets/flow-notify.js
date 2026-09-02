// flow-notify.js — 通知管理编排（P1 补齐功能域）。

// flow 子模块 — 通知管理（通知渠道列表 / 创建 / 编辑 / 删除 / 通知模板列表 / 创建 / 删除）。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, $, pageRoot } from './flow-state.js';

// ============================================================================
// 通知管理
// ============================================================================

function notifyContent() { return $('notify-content'); }

// loadNotifyChannels 加载通知渠道列表。
export async function loadNotifyChannels() {
  const content = notifyContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const channels = await api.getNotifyChannels();
    state.notify.channels = channels;
    render.renderNotifyChannelsTable(content, channels, {
      onEdit: (id) => editNotifyChannel(id),
      onDelete: (id) => deleteNotifyChannel(id),
    });
  } catch (err) {
    render.renderError(content, t('notify.loadFailed') + ': ' + err.message);
  }
}

// showNotifyChannelForm 打开创建通知渠道表单。
export function showNotifyChannelForm() {
  const content = notifyContent();
  if (!content) return;
  render.renderNotifyChannelForm(content, null, {
    onSubmit: async (data) => {
      if (!data.name) {
        render.renderToast(t('notify.nameRequired'), 'warn');
        return;
      }
      try {
        await api.createNotifyChannel(data);
        render.renderToast(t('notify.created'), 'success');
        loadNotifyChannels();
      } catch (err) {
        render.renderToast(t('notify.createFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => loadNotifyChannels(),
  });
}

// editNotifyChannel 打开编辑通知渠道表单。
export function editNotifyChannel(id) {
  const content = notifyContent();
  if (!content) return;
  const channel = (state.notify.channels || []).find((c) => String(c.id) === String(id));
  if (!channel) {
    render.renderToast(t('notify.notFound'), 'warn');
    return;
  }
  render.renderNotifyChannelForm(content, channel, {
    onSubmit: async (data) => {
      if (!data.name) {
        render.renderToast(t('notify.nameRequired'), 'warn');
        return;
      }
      try {
        await api.updateNotifyChannel(id, data);
        render.renderToast(t('notify.updated'), 'success');
        loadNotifyChannels();
      } catch (err) {
        render.renderToast(t('notify.updateFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => loadNotifyChannels(),
  });
}

// deleteNotifyChannel 删除通知渠道（确认后调用 API）。
export async function deleteNotifyChannel(id) {
  if (!window.confirm(t('notify.confirmDelete'))) return;
  try {
    await api.deleteNotifyChannel(id);
    render.renderToast(t('notify.deleted'), 'success');
    loadNotifyChannels();
  } catch (err) {
    render.renderToast(t('notify.deleteFailed') + ': ' + err.message, 'error');
  }
}

// loadNotifyTemplates 加载通知模板列表。
export async function loadNotifyTemplates() {
  const content = notifyContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const templates = await api.getNotifyTemplates();
    state.notify.templates = templates;
    render.renderNotifyTemplatesTable(content, templates, {
      onDelete: (id) => deleteNotifyTemplate(id),
    });
  } catch (err) {
    render.renderError(content, t('notify.templatesLoadFailed') + ': ' + err.message);
  }
}

// showNotifyTemplateForm 打开创建通知模板表单。
export function showNotifyTemplateForm() {
  const content = notifyContent();
  if (!content) return;
  render.renderNotifyTemplateForm(content, {
    onSubmit: async (data) => {
      if (!data.name) {
        render.renderToast(t('notify.templateNameRequired'), 'warn');
        return;
      }
      try {
        await api.createNotifyTemplate(data);
        render.renderToast(t('notify.templateCreated'), 'success');
        loadNotifyTemplates();
      } catch (err) {
        render.renderToast(t('notify.templateCreateFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => loadNotifyTemplates(),
  });
}

// deleteNotifyTemplate 删除通知模板（确认后调用 API）。
export async function deleteNotifyTemplate(id) {
  if (!window.confirm(t('notify.templateConfirmDelete'))) return;
  try {
    await api.deleteNotifyTemplate(id);
    render.renderToast(t('notify.templateDeleted'), 'success');
    loadNotifyTemplates();
  } catch (err) {
    render.renderToast(t('notify.templateDeleteFailed') + ': ' + err.message, 'error');
  }
}

// refreshNotifySubTab 按当前子 tab 刷新通知页。
export function refreshNotifySubTab() {
  if (state.notifySubTab === 'templates') loadNotifyTemplates();
  else loadNotifyChannels();
}

// buildNotifyToolbar 构建通知工具栏（子 tab + 创建按钮 + 刷新）。
export function buildNotifyToolbar() {
  const toolbar = $('notify-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  // 子 tab 切换组
  const subTabs = [
    { key: 'channels', label: t('notify.tabChannels'), onActivate: () => loadNotifyChannels() },
    { key: 'templates', label: t('notify.tabTemplates'), onActivate: () => loadNotifyTemplates() },
  ];
  subTabs.forEach((st) => {
    toolbar.appendChild(
      render.el('button', {
        class: 'btn ' + (state.notifySubTab === st.key ? 'btn-secondary' : 'btn-ghost'),
        onclick: () => { state.notifySubTab = st.key; st.onActivate(); buildNotifyToolbar(); },
      }, render.el('span', { text: st.label }))
    );
  });
  // 创建按钮
  if (state.notifySubTab === 'templates') {
    toolbar.appendChild(
      render.el('button', { class: 'btn btn-primary', onclick: () => showNotifyTemplateForm() },
        iconEl('plus', 16), render.el('span', { text: t('notify.createTemplate') })
      )
    );
  } else {
    toolbar.appendChild(
      render.el('button', { class: 'btn btn-primary', onclick: () => showNotifyChannelForm() },
        iconEl('plus', 16), render.el('span', { text: t('notify.createChannel') })
      )
    );
  }
  // 刷新
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => refreshNotifySubTab() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}