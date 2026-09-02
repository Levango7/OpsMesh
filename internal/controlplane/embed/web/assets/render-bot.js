// render-bot.js — ChatOps 渲染（P2 补齐功能域）。

// 渲染子模块 — ChatOps（命令输入 + 响应展示 + 历史记录 + 平台列表）。
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
// ChatOps 渲染
// ============================================================================

// platformEnabledBadge 平台启用状态 badge。
function platformEnabledBadge(enabled) {
  if (enabled) return badge(t('common.enabled'), 'status-resolved');
  return badge(t('common.disabled'), 'status-closed');
}

// renderBotCommandForm 渲染 ChatOps 命令输入表单。
// platforms: [{name, enabled, webhookURL}] 用于平台选择
// handlers: { onSubmit(data), onCancel() }
export function renderBotCommandForm(container, platforms, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const data = {
      command: form.elements.command.value,
      platform: form.elements.platform.value,
    };
    handlers.onSubmit(data);
  } });
  form.appendChild(el('h3', { class: 'form-title', text: t('bot.commandTitle') }));
  // 平台选择
  const platList = Array.isArray(platforms) ? platforms : [];
  form.appendChild(fieldRow(t('bot.platform'), true,
    el('select', { name: 'platform' },
      platList.length === 0
        ? [el('option', { value: '', text: t('common.empty') })]
        : platList.map((p) =>
            el('option', { value: p.name, text: p.name || '-' })
          )
    )
  ));
  // 命令输入（必填）
  form.appendChild(fieldRow(t('bot.command'), true,
    el('textarea', { name: 'command', rows: '3', required: 'true', placeholder: t('bot.commandPlaceholder') }, '')
  ));
  // 操作按钮
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('send', 16), el('span', { text: t('bot.send') })),
    el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => handlers.onCancel() }, el('span', { text: t('common.cancel') }))
  ));
  container.appendChild(form);
}

// renderBotResponse 渲染 ChatOps 命令响应。
// resp: {response, taskID}
export function renderBotResponse(container, resp) {
  container.innerHTML = '';
  if (!resp) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const card = el('div', { class: 'detail-card' });
  card.appendChild(el('h3', { class: 'detail-title', text: t('bot.responseTitle') }));
  // 任务 ID
  if (resp.taskID) {
    card.appendChild(el('div', { class: 'detail-grid' },
      el('div', { class: 'detail-item' },
        el('div', { class: 'detail-label', text: t('bot.taskID') }),
        el('div', { class: 'detail-value mono', text: String(resp.taskID) })
      )
    ));
  }
  // 响应内容
  card.appendChild(el('div', { class: 'detail-section' },
    el('h4', { text: t('bot.response') }),
    el('p', { class: 'detail-desc', text: String(resp.response || '-') })
  ));
  container.appendChild(card);
}

// renderBotHistoryTable 渲染 ChatOps 历史记录表格。
// history: [{id, command, response, userID, createdAt}]
export function renderBotHistoryTable(container, history) {
  container.innerHTML = '';
  if (!history || history.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('bot.command') }),
        el('th', { text: t('bot.response') }),
        el('th', { text: t('bot.userID') }),
        el('th', { text: t('common.createdAt') })
      )
    ),
    el('tbody', null,
      history.map((h) => el('tr', null,
        el('td', { class: 'mono', text: h.id || '-' }),
        el('td', { class: 'cell-title', text: String(h.command || '-') }),
        el('td', { class: 'mono', text: truncate(String(h.response || '-'), 60) }),
        el('td', { class: 'mono', text: h.userID || '-' }),
        el('td', { class: 'mono', text: formatTime(h.createdAt) })
      ))
    )
  );
  container.appendChild(table);
}

// truncate 截断字符串并加省略号。
function truncate(s, max) {
  if (!s) return s;
  return s.length > max ? (s.slice(0, max) + '…') : s;
}

// renderBotPlatformsTable 渲染 ChatOps 平台列表表格。
// platforms: [{name, enabled, webhookURL}]
export function renderBotPlatformsTable(container, platforms) {
  container.innerHTML = '';
  if (!platforms || platforms.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('bot.platformName') }),
        el('th', { text: t('common.status') }),
        el('th', { text: t('bot.webhookURL') })
      )
    ),
    el('tbody', null,
      platforms.map((p) => el('tr', null,
        el('td', { class: 'cell-title', text: p.name || '-' }),
        el('td', null, platformEnabledBadge(p.enabled)),
        el('td', { class: 'mono', text: p.webhookURL || '-' })
      ))
    )
  );
  container.appendChild(table);
}