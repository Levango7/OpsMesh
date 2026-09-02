// render-notify.js — 通知管理渲染（P1 补齐功能域）。

// 渲染子模块 — 通知管理（通知渠道列表 / 创建 / 编辑 / 删除 / 通知模板列表 / 创建 / 删除）。
// 公共依赖：i18n（t）、icons（iconEl/iconHtml）、render-common（DOM/Badge/表单辅助）。

import { t } from './i18n.js';
import { iconEl, iconHtml } from './icons.js';
import {
  el, formatTime, formatNumber, badge,
  renderLoading, renderError, renderEmpty, renderToast,
  statusBadge, priorityBadge, categoryBadge, sloStatusBadge,
  detailItem, fieldRow,
} from './render-common.js';

// ============================================================================
// 通知管理渲染
// ============================================================================

// channelTypeBadge 通知渠道类型 badge。
function channelTypeBadge(type) {
  const s = String(type || '').toLowerCase();
  if (s === 'email') return badge(t('notify.type.email'), 'category-request');
  if (s === 'webhook' || s === 'dingtalk' || s === 'feishu' || s === 'slack') return badge(t('notify.type.webhook'), 'category-change');
  if (s === 'sms') return badge(t('notify.type.sms'), 'priority-medium');
  return badge(type || '-', 'status-in_progress');
}

// renderNotifyChannelsTable 渲染通知渠道列表表格。
// handlers: { onEdit(id), onDelete(id) }
export function renderNotifyChannelsTable(container, channels, handlers) {
  container.innerHTML = '';
  if (!channels || channels.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('notify.channelName') }),
        el('th', { text: t('notify.channelType') }),
        el('th', { text: t('notify.config') }),
        el('th', { text: t('common.createdAt') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      channels.map((c) => el('tr', null,
        el('td', { class: 'mono', text: c.id }),
        el('td', { class: 'cell-title', text: c.name || '-' }),
        el('td', null, channelTypeBadge(c.type)),
        el('td', { class: 'mono', text: formatConfig(c.config) }),
        el('td', { class: 'mono', text: formatTime(c.createdAt) }),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn-icon', title: t('common.edit'), onclick: () => handlers.onEdit(c.id) }, iconEl('edit', 14)),
          el('button', { class: 'btn-icon btn-icon-danger', title: t('common.delete'), onclick: () => handlers.onDelete(c.id) }, iconEl('trash', 14))
        )
      ))
    )
  );
  container.appendChild(table);
}

// formatConfig 格式化渠道配置（对象 JSON 化）。
function formatConfig(cfg) {
  if (cfg == null) return '-';
  if (typeof cfg === 'object') {
    try { return JSON.stringify(cfg); } catch (_) { return String(cfg); }
  }
  return String(cfg);
}

// renderNotifyChannelForm 渲染通知渠道创建/编辑表单。
// channel: 已有渠道（编辑模式）或 null（创建模式）。
// handlers: { onSubmit(data), onCancel() }
export function renderNotifyChannelForm(container, channel, handlers) {
  container.innerHTML = '';
  const isEdit = !!channel;
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const data = {
      name: form.elements.name.value.trim(),
      type: form.elements.type.value,
      config: parseConfig(form.elements.config.value),
    };
    handlers.onSubmit(data);
  } });
  form.appendChild(el('h3', { class: 'form-title', text: isEdit ? t('notify.editChannel') : t('notify.createChannel') }));
  // 渠道名称（必填）
  form.appendChild(fieldRow(t('notify.channelName'), true,
    el('input', { type: 'text', name: 'name', required: 'true', value: channel ? (channel.name || '') : '', placeholder: t('notify.channelNamePlaceholder') })
  ));
  // 渠道类型
  form.appendChild(fieldRow(t('notify.channelType'), true,
    el('select', { name: 'type' },
      ['email', 'webhook', 'sms', 'dingtalk', 'feishu', 'slack'].map((tp) =>
        el('option', { value: tp, selected: channel && channel.type === tp ? 'selected' : undefined, text: t('notify.type.' + tp) })
      )
    )
  ));
  // 配置（JSON）
  form.appendChild(fieldRow(t('notify.config'), false,
    el('textarea', { name: 'config', rows: '4', placeholder: '{"url":"https://...","token":"xxx"}' },
      channel ? formatConfig(channel.config) : '')
  ));
  // 操作按钮
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('check', 16), el('span', { text: t('common.save') })),
    el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => handlers.onCancel() }, el('span', { text: t('common.cancel') }))
  ));
  container.appendChild(form);
}

// parseConfig 解析配置 JSON（解析失败保留原字符串）。
function parseConfig(text) {
  const raw = (text || '').trim();
  if (!raw) return null;
  try { return JSON.parse(raw); } catch (_) { return raw; }
}

// renderNotifyTemplatesTable 渲染通知模板列表表格。
// handlers: { onDelete(id) }
export function renderNotifyTemplatesTable(container, templates, handlers) {
  container.innerHTML = '';
  if (!templates || templates.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('notify.templateName') }),
        el('th', { text: t('notify.templateType') }),
        el('th', { text: t('common.createdAt') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      templates.map((tp) => el('tr', null,
        el('td', { class: 'mono', text: tp.id }),
        el('td', { class: 'cell-title', text: tp.name || '-' }),
        el('td', null, channelTypeBadge(tp.type)),
        el('td', { class: 'mono', text: formatTime(tp.createdAt) }),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn-icon btn-icon-danger', title: t('common.delete'), onclick: () => handlers.onDelete(tp.id) }, iconEl('trash', 14))
        )
      ))
    )
  );
  container.appendChild(table);
}

// renderNotifyTemplateForm 渲染通知模板创建表单。
// handlers: { onSubmit(data), onCancel() }
export function renderNotifyTemplateForm(container, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const data = {
      name: form.elements.name.value.trim(),
      type: form.elements.type.value,
      subject: form.elements.subject.value.trim(),
      body: form.elements.body.value,
    };
    handlers.onSubmit(data);
  } });
  form.appendChild(el('h3', { class: 'form-title', text: t('notify.createTemplate') }));
  // 模板名称（必填）
  form.appendChild(fieldRow(t('notify.templateName'), true,
    el('input', { type: 'text', name: 'name', required: 'true', placeholder: t('notify.templateNamePlaceholder') })
  ));
  // 模板类型
  form.appendChild(fieldRow(t('notify.templateType'), true,
    el('select', { name: 'type' },
      ['email', 'webhook', 'sms'].map((tp) =>
        el('option', { value: tp, text: t('notify.type.' + tp) })
      )
    )
  ));
  // 主题
  form.appendChild(fieldRow(t('notify.templateSubject'), false,
    el('input', { type: 'text', name: 'subject', placeholder: t('notify.templateSubjectPlaceholder') })
  ));
  // 内容
  form.appendChild(fieldRow(t('notify.templateBody'), false,
    el('textarea', { name: 'body', rows: '6', placeholder: t('notify.templateBodyPlaceholder') }, '')
  ));
  // 操作按钮
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('check', 16), el('span', { text: t('common.save') })),
    el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => handlers.onCancel() }, el('span', { text: t('common.cancel') }))
  ));
  container.appendChild(form);
}