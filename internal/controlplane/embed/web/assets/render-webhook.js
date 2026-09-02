// render-webhook.js — Webhook 渲染（由 render.js 拆分）。

// 渲染子模块 — 由 render.js 拆分而来。
// 公共依赖：i18n（t）、icons（iconEl/iconHtml）、render-common（DOM/Badge/表单辅助）。

import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { el, formatTime, badge, renderEmpty, fieldRow, webhookStatusBadge } from './render-common.js';

export function renderWebhooksTable(container, webhooks, handlers) {
  container.innerHTML = '';
  if (!webhooks || !webhooks.length) { renderEmpty(container, t('webhook.noWebhooks')); return; }
  container.appendChild(el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('webhook.name') }),
        el('th', { text: t('webhook.url') }),
        el('th', { text: t('webhook.event') }),
        el('th', { text: t('webhook.status') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      webhooks.map((w) => el('tr', null,
        el('td', { class: 'cell-title', text: w.name || w.id || '-' }),
        el('td', { class: 'mono', text: w.url || '-' }),
        el('td', { class: 'mono', text: w.event || w.events || '-' }),
        el('td', null, webhookStatusBadge(w.status)),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn btn-ghost', title: t('common.edit'), onclick: () => handlers.onEdit && handlers.onEdit(w) },
            iconEl('edit', 14)
          ),
          el('button', { class: 'btn btn-ghost', title: t('webhook.test'), onclick: () => handlers.onTest && handlers.onTest(w) },
            iconEl('send', 14)
          ),
          el('button', { class: 'btn btn-ghost', title: t('webhook.showDeliveries'), onclick: () => handlers.onDeliveries && handlers.onDeliveries(w) },
            iconEl('deliver', 14)
          ),
          el('button', { class: 'btn btn-ghost btn-icon-danger', title: t('common.delete'), onclick: () => handlers.onDelete && handlers.onDelete(w) },
            iconEl('trash', 14)
          )
        )
      ))
    )
  ));
}

// renderWebhookForm 渲染创建/编辑 Webhook 表单。
// wh: 编辑时传入现有 Webhook，创建时传 null；handlers: { onSubmit(data) }
export function renderWebhookForm(container, wh, handlers) {
  container.innerHTML = '';
  const isEdit = !!wh;
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const data = {
      name: form.elements.name.value.trim(),
      url: form.elements.url.value.trim(),
      event: form.elements.event.value.trim(),
      secret: form.elements.secret.value.trim(),
    };
    handlers.onSubmit && handlers.onSubmit(data);
  } });
  form.appendChild(el('h3', { class: 'form-title', text: isEdit ? t('webhook.edit') : t('webhook.create') }));
  form.appendChild(fieldRow(t('webhook.name'), true,
    el('input', { name: 'name', type: 'text', required: 'true', value: (wh && wh.name) || '', placeholder: 'ticket-created-hook' })
  ));
  form.appendChild(fieldRow(t('webhook.url'), true,
    el('input', { name: 'url', type: 'url', required: 'true', value: (wh && wh.url) || '', placeholder: t('webhook.urlPlaceholder') })
  ));
  form.appendChild(fieldRow(t('webhook.event'), true,
    el('input', { name: 'event', type: 'text', required: 'true', value: (wh && (wh.event || (Array.isArray(wh.events) ? wh.events.join(',') : wh.events))) || '', placeholder: t('webhook.eventPlaceholder') })
  ));
  form.appendChild(fieldRow(t('webhook.secret'), false,
    el('input', { name: 'secret', type: 'text', value: (wh && wh.secret) || '', placeholder: t('webhook.secretPlaceholder') })
  ));
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' },
      iconEl('check', 16), el('span', { text: isEdit ? t('webhook.edit') : t('webhook.create') })
    )
  ));
  container.appendChild(form);
}

// renderWebhookDeliveriesTable 渲染 Webhook 投递记录表格。
export function renderWebhookDeliveriesTable(container, deliveries) {
  container.innerHTML = '';
  if (!deliveries || !deliveries.length) { renderEmpty(container, t('webhook.noDeliveries')); return; }
  container.appendChild(el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('webhook.deliveryId') }),
        el('th', { text: t('webhook.deliveryStatus') }),
        el('th', { text: t('webhook.deliveryAttempts') }),
        el('th', { text: t('webhook.deliveryTime') }),
        el('th', { text: t('webhook.deliveryResponse') })
      )
    ),
    el('tbody', null,
      deliveries.map((d) => {
        const sc = d.statusCode || d.status || d.code;
        const ok = Number(sc) >= 200 && Number(sc) < 300;
        return el('tr', null,
          el('td', { class: 'cell-title mono', text: d.id || d.deliveryID || '-' }),
          el('td', null, badge(String(sc || '-'), ok ? 'badge-status-resolved' : 'badge-priority-urgent')),
          el('td', { text: String(d.attempts != null ? d.attempts : (d.retryCount || 0)) }),
          el('td', { text: formatTime(d.createdAt || d.time || d.deliveredAt) }),
          el('td', { class: 'mono', text: String(d.response || d.body || '').slice(0, 80) })
        );
      })
    )
  ));
}

// --- 自定义脚本 ---


// renderScriptsTable 渲染自定义脚本列表表格。
// handlers: { onEdit(s), onExecute(s), onExecutions(s), onDelete(s) }
