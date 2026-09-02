// render-ticket.js — 工单渲染（由 render.js 拆分）。

// 渲染子模块 — 由 render.js 拆分而来。
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
// 工单渲染
// ============================================================================

// renderTicketTable 渲染工单列表表格。
// handlers: { onEdit(id), onClose(id) }
export function renderTicketTable(container, tickets, handlers) {
  container.innerHTML = '';
  if (!tickets || tickets.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('common.title') }),
        el('th', { text: t('common.status') }),
        el('th', { text: t('common.priority') }),
        el('th', { text: t('common.category') }),
        el('th', { text: t('common.assignee') }),
        el('th', { text: t('common.createdAt') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      tickets.map((tk) => el('tr', null,
        el('td', { class: 'mono', text: tk.id }),
        el('td', { class: 'cell-title', text: tk.title }),
        el('td', null, statusBadge(tk.status)),
        el('td', null, priorityBadge(tk.priority)),
        el('td', null, categoryBadge(tk.category)),
        el('td', { text: tk.assigneeID || '-' }),
        el('td', { class: 'mono', text: formatTime(tk.createdAt) }),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn-icon', title: t('common.edit'), onclick: () => handlers.onEdit(tk.id) }, iconEl('edit', 14)),
          el('button', { class: 'btn-icon btn-icon-danger', title: t('common.close'), onclick: () => handlers.onClose(tk.id) }, iconEl('close', 14))
        )
      ))
    )
  );
  container.appendChild(table);
}

// renderTicketForm 渲染工单创建/编辑表单。
// ticket=null 表示创建；否则编辑（预填）。
// handlers: { onSubmit(data), onCancel() }
export function renderTicketForm(container, ticket, handlers) {
  container.innerHTML = '';
  const isEdit = !!ticket;
  const form = el('form', { class: 'form-card', onsubmit: (e) => { e.preventDefault(); handlers.onSubmit(collectTicketForm(form)); } });

  form.appendChild(el('h3', { class: 'form-title', text: isEdit ? t('tickets.edit') : t('tickets.create') }));

  // 标题（必填）
  form.appendChild(fieldRow(t('common.title'), true,
    el('input', { type: 'text', name: 'title', required: 'true', value: ticket ? ticket.title : '', placeholder: t('tickets.titleRequired') })
  ));
  // 描述
  form.appendChild(fieldRow(t('common.description'), false,
    el('textarea', { name: 'description', rows: '3' }, ticket ? ticket.description : '')
  ));
  // 状态（仅编辑可改）+ 优先级
  const statusSelect = el('select', { name: 'status' },
    ['open', 'in_progress', 'resolved', 'closed'].map((s) =>
      el('option', { value: s, selected: ticket && ticket.status === s ? 'selected' : undefined, text: t('ticket.status.' + s) })
    )
  );
  if (!isEdit) statusSelect.disabled = true;
  form.appendChild(fieldRow(t('common.status'), false, statusSelect));

  form.appendChild(fieldRow(t('common.priority'), false,
    el('select', { name: 'priority' },
      ['low', 'medium', 'high', 'urgent'].map((p) =>
        el('option', { value: p, selected: (ticket ? ticket.priority : 'medium') === p ? 'selected' : undefined, text: t('ticket.priority.' + p) })
      )
    )
  ));
  // 分类
  form.appendChild(fieldRow(t('common.category'), false,
    el('select', { name: 'category' },
      ['incident', 'change', 'request', 'problem'].map((c) =>
        el('option', { value: c, selected: (ticket ? ticket.category : 'incident') === c ? 'selected' : undefined, text: t('ticket.category.' + c) })
      )
    )
  ));
  // 指派人 + 创建人（创建人不显示，后端自动填充）
  form.appendChild(fieldRow(t('common.assignee'), false,
    el('input', { type: 'text', name: 'assigneeID', value: ticket ? ticket.assigneeID : '' })
  ));
  // 关联设备 + 关联任务
  form.appendChild(fieldRow(t('tickets.relatedDevice'), false,
    el('input', { type: 'text', name: 'relatedDevice', value: ticket ? ticket.relatedDevice : '' })
  ));
  form.appendChild(fieldRow(t('tickets.relatedTask'), false,
    el('input', { type: 'text', name: 'relatedTask', value: ticket ? ticket.relatedTask : '' })
  ));
  // 标签（逗号分隔）
  form.appendChild(fieldRow(t('tickets.tags'), false,
    el('input', { type: 'text', name: 'tags', value: ticket && ticket.tags ? ticket.tags.join(', ') : '', placeholder: 'tag1, tag2' })
  ));

  // 操作按钮
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('check', 16), el('span', { text: t('common.save') })),
    el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => handlers.onCancel() }, el('span', { text: t('common.cancel') }))
  ));

  container.appendChild(form);
}

// collectTicketForm 从表单收集数据。
function collectTicketForm(form) {
  const get = (name) => (form.elements[name] && form.elements[name].value) || '';
  const tags = get('tags').split(',').map((s) => s.trim()).filter(Boolean);
  return {
    title: get('title'),
    description: get('description'),
    status: get('status'),
    priority: get('priority'),
    category: get('category'),
    assigneeID: get('assigneeID'),
    relatedDevice: get('relatedDevice'),
    relatedTask: get('relatedTask'),
    tags,
  };
}

// renderTicketDetail 渲染工单详情。
// handlers: { onBack(), onEdit(), onClose() }
export function renderTicketDetail(container, ticket, handlers) {
  container.innerHTML = '';
  if (!ticket) { renderEmpty(container); return; }
  const card = el('div', { class: 'detail-card' });

  // 头部：返回 + 标题 + 操作
  card.appendChild(el('div', { class: 'detail-head' },
    el('button', { class: 'btn btn-ghost', onclick: () => handlers.onBack() }, iconEl('back', 16), el('span', { text: t('common.back') })),
    el('h3', { class: 'detail-title', text: ticket.title }),
    el('div', { class: 'detail-actions' },
      el('button', { class: 'btn btn-secondary', onclick: () => handlers.onEdit() }, iconEl('edit', 14), el('span', { text: t('common.edit') })),
      ticket.status !== 'closed'
        ? el('button', { class: 'btn btn-danger', onclick: () => handlers.onClose() }, iconEl('close', 14), el('span', { text: t('common.close') }))
        : null
    )
  ));

  // 元信息网格
  card.appendChild(el('div', { class: 'detail-grid' },
    detailItem(t('common.id'), ticket.id, true),
    detailItem(t('common.status'), statusBadge(ticket.status)),
    detailItem(t('common.priority'), priorityBadge(ticket.priority)),
    detailItem(t('common.category'), categoryBadge(ticket.category)),
    detailItem(t('common.assignee'), ticket.assigneeID || '-'),
    detailItem(t('common.creator'), ticket.creatorID || '-'),
    detailItem(t('tickets.relatedDevice'), ticket.relatedDevice || '-'),
    detailItem(t('tickets.relatedTask'), ticket.relatedTask || '-'),
    detailItem(t('common.createdAt'), formatTime(ticket.createdAt), true),
    detailItem(t('common.updatedAt'), formatTime(ticket.updatedAt), true),
    detailItem(t('tickets.resolvedAt'), formatTime(ticket.resolvedAt), true)
  ));

  // 描述
  if (ticket.description) {
    card.appendChild(el('div', { class: 'detail-section' },
      el('h4', { text: t('common.description') }),
      el('p', { class: 'detail-desc', text: ticket.description })
    ));
  }

  // 标签
  if (ticket.tags && ticket.tags.length) {
    card.appendChild(el('div', { class: 'detail-section' },
      el('h4', { text: t('tickets.tags') }),
      el('div', { class: 'tag-list' },
        ticket.tags.map((tag) => el('span', { class: 'tag' }, iconEl('tag', 12), el('span', { text: tag })))
      )
    ));
  }

  container.appendChild(card);
}


