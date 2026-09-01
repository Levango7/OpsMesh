// flow-ticket.js — 工单管理编排（由 flow.js 拆分）。

// flow 子模块 — 由 flow.js 拆分而来。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, \$, pageRoot } from './flow-state.js';

// ============================================================================
// 工单管理
// ============================================================================

// loadTickets 加载工单列表（带当前过滤器）。
export async function loadTickets(filter) {
  if (filter) state.ticketFilter = Object.assign(state.ticketFilter, filter);
  const content = ticketsContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const tickets = await api.getTickets(state.ticketFilter);
    state.tickets = tickets;
    render.renderTicketTable(content, tickets, {
      onEdit: (id) => editTicket(id),
      onClose: (id) => closeTicket(id),
    });
  } catch (err) {
    render.renderError(content, t('tickets.loadFailed') + ': ' + err.message);
  }
}

// createTicket 打开创建工单表单。
export function createTicket() {
  const content = ticketsContent();
  if (!content) return;
  render.renderTicketForm(content, null, {
    onSubmit: async (data) => {
      if (!data.title || !data.title.trim()) {
        render.renderToast(t('tickets.titleRequired'), 'warn');
        return;
      }
      try {
        await api.createTicket(data);
        render.renderToast(t('tickets.created'), 'success');
        loadTickets();
      } catch (err) {
        render.renderToast(t('tickets.createFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => loadTickets(),
  });
}

// editTicket 打开编辑工单表单（先拉详情）。
export async function editTicket(id) {
  const content = ticketsContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const ticket = await api.getTicket(id);
    render.renderTicketForm(content, ticket, {
      onSubmit: async (data) => {
        if (!data.title || !data.title.trim()) {
          render.renderToast(t('tickets.titleRequired'), 'warn');
          return;
        }
        try {
          await api.updateTicket(id, data);
          render.renderToast(t('tickets.updated'), 'success');
          loadTickets();
        } catch (err) {
          render.renderToast(t('tickets.updateFailed') + ': ' + err.message, 'error');
        }
      },
      onCancel: () => loadTickets(),
    });
  } catch (err) {
    render.renderError(content, t('tickets.loadFailed') + ': ' + err.message);
  }
}

// showTicketDetail 查看工单详情。
export async function showTicketDetail(id) {
  const content = ticketsContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const ticket = await api.getTicket(id);
    render.renderTicketDetail(content, ticket, {
      onBack: () => loadTickets(),
      onEdit: () => editTicket(id),
      onClose: () => closeTicket(id),
    });
  } catch (err) {
    render.renderError(content, t('tickets.loadFailed') + ': ' + err.message);
  }
}

// closeTicket 关闭工单（确认后调用 API）。
export async function closeTicket(id) {
  if (!window.confirm(t('tickets.confirmClose'))) return;
  try {
    await api.closeTicket(id);
    render.renderToast(t('tickets.closed'), 'success');
    loadTickets();
  } catch (err) {
    render.renderToast(t('tickets.closeFailed') + ': ' + err.message, 'error');
  }
}

// buildTicketsToolbar 构建工单工具栏（创建按钮 + 过滤器）。
export function buildTicketsToolbar() {
  const toolbar = $('tickets-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  // 创建按钮
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-primary', onclick: () => createTicket() },
      iconEl('plus', 16), render.el('span', { text: t('tickets.create') })
    )
  );
  // 过滤器组
  const filterGroup = render.el('div', { class: 'filter-group' });
  // 状态过滤
  filterGroup.appendChild(buildFilterSelect('status', ['open', 'in_progress', 'resolved', 'closed'], (v) => loadTickets({ status: v })));
  // 优先级过滤
  filterGroup.appendChild(buildFilterSelect('priority', ['low', 'medium', 'high', 'urgent'], (v) => loadTickets({ priority: v })));
  // 分类过滤
  filterGroup.appendChild(buildFilterSelect('category', ['incident', 'change', 'request', 'problem'], (v) => loadTickets({ category: v })));
  // 刷新
  filterGroup.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => loadTickets() },
      iconEl('refresh', 14)
    )
  );
  toolbar.appendChild(filterGroup);
}

// buildFilterSelect 构建过滤下拉框。
function buildFilterSelect(kind, options, onChange) {
  const labelMap = {
    status: { label: t('tickets.filter.status'), prefix: 'ticket.status.' },
    priority: { label: t('tickets.filter.priority'), prefix: 'ticket.priority.' },
    category: { label: t('tickets.filter.category'), prefix: 'ticket.category.' },
  };
  const meta = labelMap[kind];
  const select = render.el('select', { class: 'filter-select', dataset: { filter: kind }, onchange: (e) => onChange(e.target.value) },
    render.el('option', { value: '', text: meta.label + ': ' + t('common.all') }),
    options.map((opt) => render.el('option', { value: opt, text: meta.label + ': ' + t(meta.prefix + opt) }))
  );
  return select;
}

