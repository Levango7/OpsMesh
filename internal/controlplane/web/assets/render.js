// render.js — OpsMesh Phase 1 前端渲染层。
//
// 职责：纯 DOM 构建，不直接调用 API（由 flow.js 编排）。
// 导出：
//   - 通用：el / formatTime / renderLoading / renderError / renderEmpty / renderToast
//   - 工单：renderTicketTable / renderTicketForm / renderTicketDetail
//   - SLO：renderSLOTable / renderSLOForm / renderSLODetail
//   - 仪表盘：renderDashboardOverview / renderMetricsText
//
// 设计要点：
//   - 用户输入字段用 textContent（防 XSS）；图标用 innerHTML（受控 SVG）；
//   - 表格语义化 <table>，表单语义化 <form>，card 布局；
//   - 状态/优先级/分类用 badge 样式（CSS 类 status-xxx/priority-xxx/category-xxx）；
//   - 时间统一 formatTime 格式化为 "YYYY-MM-DD HH:mm"。

import { t } from './i18n.js';
import { iconEl, iconHtml } from './icons.js';

// ============================================================================
// DOM 构建辅助
// ============================================================================

// el(tag, props, ...children) 语义化 DOM 构建。
// props 支持：class/className, text, html, style(object), on*(event), dataset, 以及任意 attribute。
export function el(tag, props, ...children) {
  const node = document.createElement(tag);
  if (props) {
    for (const k of Object.keys(props)) {
      const v = props[k];
      if (v === undefined || v === null) continue;
      if (k === 'class' || k === 'className') node.className = v;
      else if (k === 'text') node.textContent = String(v);
      else if (k === 'html') node.innerHTML = v;
      else if (k === 'style' && typeof v === 'object') Object.assign(node.style, v);
      else if (k === 'dataset' && typeof v === 'object') Object.assign(node.dataset, v);
      else if (k.startsWith('on') && typeof v === 'function') node.addEventListener(k.slice(2).toLowerCase(), v);
      else node.setAttribute(k, String(v));
    }
  }
  appendChildren(node, children);
  return node;
}

function appendChildren(node, children) {
  for (const c of children) {
    if (c == null || c === false) continue;
    if (typeof c === 'string' || typeof c === 'number') node.appendChild(document.createTextNode(String(c)));
    else if (Array.isArray(c)) appendChildren(node, c);
    else if (c instanceof Node) node.appendChild(c);
  }
}

// formatTime 格式化 ISO 时间为 "YYYY-MM-DD HH:mm"。
export function formatTime(ts) {
  if (!ts) return '-';
  const d = new Date(ts);
  if (isNaN(d.getTime())) return '-';
  const pad = (n) => String(n).padStart(2, '0');
  return d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate()) +
    ' ' + pad(d.getHours()) + ':' + pad(d.getMinutes());
}

// ============================================================================
// 通用渲染
// ============================================================================

export function renderLoading(container, msg) {
  container.innerHTML = '';
  container.appendChild(el('div', { class: 'state state-loading' },
    el('span', { class: 'spinner', 'aria-hidden': 'true' }),
    el('span', { text: msg || t('common.loading') })
  ));
}

export function renderError(container, msg) {
  container.innerHTML = '';
  container.appendChild(el('div', { class: 'state state-error' },
    iconEl('alert', 18),
    el('span', { text: msg || t('common.error') })
  ));
}

export function renderEmpty(container, msg) {
  container.innerHTML = '';
  container.appendChild(el('div', { class: 'state state-empty' },
    el('span', { text: msg || t('common.empty') })
  ));
}

// renderToast 显示临时提示（3 秒后自动消失）。
export function renderToast(message, kind) {
  const host = document.getElementById('toastHost') || (() => {
    const h = el('div', { id: 'toastHost', class: 'toast-host', 'aria-live': 'polite' });
    document.body.appendChild(h);
    return h;
  })();
  const item = el('div', { class: 'toast toast-' + (kind || 'info'), text: message });
  host.appendChild(item);
  setTimeout(() => {
    item.classList.add('toast-leave');
    setTimeout(() => item.remove(), 200);
  }, 3000);
}

// ============================================================================
// Badge 辅助
// ============================================================================

function badge(text, cls) {
  return el('span', { class: 'badge badge-' + cls, text });
}

export function statusBadge(status) {
  return badge(t('ticket.status.' + status, status), 'status-' + status);
}

export function priorityBadge(priority) {
  return badge(t('ticket.priority.' + priority, priority), 'priority-' + priority);
}

export function categoryBadge(category) {
  return badge(t('ticket.category.' + category, category), 'category-' + category);
}

export function sloStatusBadge(status) {
  return badge(t('slo.status.' + status, status), 'slo-status-' + status);
}

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

function detailItem(label, value, mono) {
  const valNode = (value instanceof Node) ? value : el('span', { class: mono ? 'mono' : '', text: String(value) });
  return el('div', { class: 'detail-item' },
    el('span', { class: 'detail-label', text: label }),
    el('span', { class: 'detail-value' + (mono ? ' mono' : '') }, valNode)
  );
}

// fieldRow 表单字段行（label + control）。
function fieldRow(label, required, control) {
  return el('div', { class: 'form-row' },
    el('label', { class: 'form-label' + (required ? ' required' : ''), text: label }),
    el('div', { class: 'form-control' }, control)
  );
}

// ============================================================================
// SLO 渲染
// ============================================================================

// renderSLOTable 渲染 SLO 列表表格。
// handlers: { onDetail(id), onDelete(id) }
export function renderSLOTable(container, slos, handlers) {
  container.innerHTML = '';
  if (!slos || slos.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('common.name') }),
        el('th', { text: t('common.service') }),
        el('th', { text: t('common.target') }),
        el('th', { text: t('common.window') }),
        el('th', { text: t('slo.slis') }),
        el('th', { text: t('common.createdAt') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      slos.map((slo) => el('tr', null,
        el('td', { class: 'mono', text: slo.id }),
        el('td', { class: 'cell-title', text: slo.name }),
        el('td', { text: slo.serviceName || '-' }),
        el('td', { class: 'mono', text: (slo.target != null ? slo.target + '%' : '-') }),
        el('td', { class: 'mono', text: slo.window || '-' }),
        el('td', { class: 'mono', text: (slo.slis && slo.slis.length) || 0 }),
        el('td', { class: 'mono', text: formatTime(slo.createdAt) }),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn-icon', title: t('slo.detail'), onclick: () => handlers.onDetail(slo.id) }, iconEl('search', 14)),
          el('button', { class: 'btn-icon btn-icon-danger', title: t('common.delete'), onclick: () => handlers.onDelete(slo.id) }, iconEl('trash', 14))
        )
      ))
    )
  );
  container.appendChild(table);
}

// renderSLOForm 渲染 SLO 创建/编辑表单。
// slo=null 表示创建；否则编辑（预填）。
// handlers: { onSubmit(data), onCancel() }
export function renderSLOForm(container, slo, handlers) {
  container.innerHTML = '';
  const isEdit = !!slo;
  const form = el('form', { class: 'form-card', onsubmit: (e) => { e.preventDefault(); handlers.onSubmit(collectSLOForm(form)); } });

  form.appendChild(el('h3', { class: 'form-title', text: isEdit ? t('slo.edit') : t('slo.create') }));

  // 名称（必填）
  form.appendChild(fieldRow(t('common.name'), true,
    el('input', { type: 'text', name: 'name', required: 'true', value: slo ? slo.name : '', placeholder: t('slo.nameRequired') })
  ));
  // 描述
  form.appendChild(fieldRow(t('common.description'), false,
    el('textarea', { name: 'description', rows: '2' }, slo ? slo.description : '')
  ));
  // 服务名 + 窗口
  form.appendChild(fieldRow(t('common.service'), false,
    el('input', { type: 'text', name: 'serviceName', value: slo ? slo.serviceName : '' })
  ));
  form.appendChild(fieldRow(t('common.window'), false,
    el('select', { name: 'window' },
      ['7d', '30d', '90d'].map((w) =>
        el('option', { value: w, selected: (slo ? slo.window : '30d') === w ? 'selected' : undefined, text: w })
      )
    )
  ));
  // 目标（百分比，0-100）
  form.appendChild(fieldRow(t('common.target'), false,
    el('input', { type: 'number', name: 'target', min: '0', max: '100', step: '0.01', value: slo ? slo.target : 99.9 })
  ));

  // SLI 列表（可增删）
  form.appendChild(el('div', { class: 'form-row form-row-sli' },
    el('label', { class: 'form-label', text: t('slo.slis') }),
    el('div', { class: 'form-control' },
      el('div', { id: 'sliList', class: 'sli-list' })
    )
  ));
  const sliList = form.querySelector('#sliList');
  const initialSlis = (slo && slo.slis) ? slo.slis : [];
  if (initialSlis.length === 0) {
    sliList.appendChild(buildSliRow({ name: '', metric: '', target: 0, operator: '>=' }));
  } else {
    initialSlis.forEach((sli) => sliList.appendChild(buildSliRow(sli)));
  }
  form.appendChild(el('div', { class: 'form-row' },
    el('div', { class: 'form-label' }),
    el('div', { class: 'form-control' },
      el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => sliList.appendChild(buildSliRow({ name: '', metric: '', target: 0, operator: '>=' })) },
        iconEl('plus', 14), el('span', { text: t('slo.addSli') })
      )
    )
  ));

  // 操作按钮
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('check', 16), el('span', { text: t('common.save') })),
    el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => handlers.onCancel() }, el('span', { text: t('common.cancel') }))
  ));

  container.appendChild(form);
}

function buildSliRow(sli) {
  const row = el('div', { class: 'sli-row' },
    el('input', { type: 'text', name: 'sliName', class: 'sli-name', value: sli.name || '', placeholder: t('slo.sliName') }),
    el('input', { type: 'text', name: 'sliMetric', class: 'sli-metric', value: sli.metric || '', placeholder: t('slo.sliMetric') }),
    el('select', { name: 'sliOperator', class: 'sli-op' },
      ['>=', '<=', '>', '<'].map((op) =>
        el('option', { value: op, selected: (sli.operator || '>=') === op ? 'selected' : undefined, text: op })
      )
    ),
    el('input', { type: 'number', name: 'sliTarget', class: 'sli-target', value: sli.target != null ? sli.target : 0, placeholder: t('slo.sliTarget'), step: '0.01' }),
    el('button', { type: 'button', class: 'btn-icon btn-icon-danger', title: t('slo.removeSli'), onclick: () => row.remove() }, iconEl('trash', 14))
  );
  return row;
}

function collectSLOForm(form) {
  const get = (name) => (form.elements[name] && form.elements[name].value) || '';
  const slis = [];
  const sliRows = form.querySelectorAll('.sli-row');
  sliRows.forEach((row) => {
    const name = row.querySelector('.sli-name').value.trim();
    const metric = row.querySelector('.sli-metric').value.trim();
    const operator = row.querySelector('.sli-op').value;
    const target = parseFloat(row.querySelector('.sli-target').value) || 0;
    if (name || metric) slis.push({ name, metric, operator, target });
  });
  return {
    name: get('name'),
    description: get('description'),
    serviceName: get('serviceName'),
    target: parseFloat(get('target')) || 0,
    window: get('window'),
    slis,
  };
}

// renderSLODetail 渲染 SLO 详情 + SLI 状态。
// statuses: [{sliName, currentValue, targetValue, status, lastEvaluated}]
// handlers: { onBack(), onEdit(), onDelete() }
export function renderSLODetail(container, slo, statuses, handlers) {
  container.innerHTML = '';
  if (!slo) { renderEmpty(container); return; }
  const card = el('div', { class: 'detail-card' });

  card.appendChild(el('div', { class: 'detail-head' },
    el('button', { class: 'btn btn-ghost', onclick: () => handlers.onBack() }, iconEl('back', 16), el('span', { text: t('common.back') })),
    el('h3', { class: 'detail-title', text: slo.name }),
    el('div', { class: 'detail-actions' },
      el('button', { class: 'btn btn-secondary', onclick: () => handlers.onEdit() }, iconEl('edit', 14), el('span', { text: t('common.edit') })),
      el('button', { class: 'btn btn-danger', onclick: () => handlers.onDelete() }, iconEl('trash', 14), el('span', { text: t('common.delete') }))
    )
  ));

  card.appendChild(el('div', { class: 'detail-grid' },
    detailItem(t('common.id'), slo.id, true),
    detailItem(t('common.service'), slo.serviceName || '-'),
    detailItem(t('common.target'), (slo.target != null ? slo.target + '%' : '-'), true),
    detailItem(t('common.window'), slo.window || '-', true),
    detailItem(t('common.createdAt'), formatTime(slo.createdAt), true),
    detailItem(t('common.updatedAt'), formatTime(slo.updatedAt), true)
  ));

  if (slo.description) {
    card.appendChild(el('div', { class: 'detail-section' },
      el('h4', { text: t('common.description') }),
      el('p', { class: 'detail-desc', text: slo.description })
    ));
  }

  // SLI 定义
  if (slo.slis && slo.slis.length) {
    card.appendChild(el('div', { class: 'detail-section' },
      el('h4', { text: t('slo.slis') }),
      el('table', { class: 'data-table data-table-compact' },
        el('thead', null,
          el('tr', null,
            el('th', { text: t('slo.sliName') }),
            el('th', { text: t('slo.sliMetric') }),
            el('th', { text: t('slo.sliOperator') }),
            el('th', { text: t('slo.sliTarget') })
          )
        ),
        el('tbody', null,
          slo.slis.map((sli) => el('tr', null,
            el('td', { text: sli.name || '-' }),
            el('td', { class: 'mono', text: sli.metric || '-' }),
            el('td', { class: 'mono', text: sli.operator || '-' }),
            el('td', { class: 'mono', text: String(sli.target != null ? sli.target : '-') })
          ))
        )
      )
    ));
  }

  // SLI 实时状态
  card.appendChild(el('div', { class: 'detail-section' },
    el('h4', { text: t('slo.statusTitle') }),
    (statuses && statuses.length)
      ? el('table', { class: 'data-table data-table-compact' },
          el('thead', null,
            el('tr', null,
              el('th', { text: t('slo.sliName') }),
              el('th', { text: t('slo.sliCurrent') }),
              el('th', { text: t('slo.sliTarget') }),
              el('th', { text: t('common.status') }),
              el('th', { text: t('slo.sliLastEvaluated') })
            )
          ),
          el('tbody', null,
            statuses.map((st) => el('tr', null,
              el('td', { text: st.sliName || '-' }),
              el('td', { class: 'mono', text: formatNumber(st.currentValue) }),
              el('td', { class: 'mono', text: formatNumber(st.targetValue) }),
              el('td', null, sloStatusBadge(st.status)),
              el('td', { class: 'mono', text: formatTime(st.lastEvaluated) })
            ))
          )
        )
      : el('p', { class: 'detail-desc', text: t('slo.noStatus') })
  ));

  container.appendChild(card);
}

function formatNumber(n) {
  if (n == null || isNaN(n)) return '-';
  return String(n);
}

// ============================================================================
// 监控仪表盘渲染
// ============================================================================

// renderDashboardOverview 渲染概览卡片（4 个指标）。
// overview: { devices, tasks, alerts, openTickets }
export function renderDashboardOverview(container, overview) {
  container.innerHTML = '';
  const cards = [
    { icon: 'device', label: t('dashboard.devices'), value: overview.devices, cls: 'metric-devices' },
    { icon: 'task', label: t('dashboard.tasks'), value: overview.tasks, cls: 'metric-tasks' },
    { icon: 'alert', label: t('dashboard.alerts'), value: overview.alerts, cls: 'metric-alerts' },
    { icon: 'ticket', label: t('dashboard.openTickets'), value: overview.openTickets, cls: 'metric-tickets' },
  ];
  container.appendChild(el('div', { class: 'overview-grid' },
    cards.map((c) => el('div', { class: 'metric-card ' + c.cls },
      el('div', { class: 'metric-icon' }, iconEl(c.icon, 22)),
      el('div', { class: 'metric-body' },
        el('div', { class: 'metric-value', text: String(c.value) }),
        el('div', { class: 'metric-label', text: c.label })
      )
    ))
  ));
}

// parseMetrics 解析 Prometheus text exposition format，返回 { name, help, type, value } 数组。
export function parseMetrics(text) {
  if (!text) return [];
  const lines = text.split('\n');
  const result = [];
  let cur = null;
  for (const line of lines) {
    if (!line) continue;
    if (line.startsWith('# HELP ')) {
      const rest = line.slice(7);
      const sp = rest.indexOf(' ');
      cur = { name: rest.slice(0, sp), help: rest.slice(sp + 1), type: '', value: null };
      result.push(cur);
    } else if (line.startsWith('# TYPE ')) {
      const rest = line.slice(7);
      const sp = rest.indexOf(' ');
      if (cur && cur.name === rest.slice(0, sp)) cur.type = rest.slice(sp + 1);
    } else if (!line.startsWith('#')) {
      const sp = line.lastIndexOf(' ');
      if (sp > 0) {
        const name = line.slice(0, sp);
        const value = line.slice(sp + 1);
        const found = result.find((r) => r.name === name);
        if (found) found.value = value;
        else result.push({ name, help: '', type: '', value });
      }
    }
  }
  return result;
}

// renderMetricsText 渲染 Prometheus 指标文本展示。
export function renderMetricsText(container, text) {
  container.innerHTML = '';
  if (!text) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  // 解析为结构化卡片 + 原始文本。
  const metrics = parseMetrics(text);
  if (metrics.length) {
    container.appendChild(el('div', { class: 'metrics-cards' },
      metrics.map((m) => el('div', { class: 'metric-line' },
        el('div', { class: 'metric-line-head' },
          el('span', { class: 'metric-line-name mono', text: m.name }),
          m.type ? el('span', { class: 'metric-line-type', text: m.type }) : null
        ),
        m.help ? el('div', { class: 'metric-line-help', text: m.help }) : null,
        el('div', { class: 'metric-line-value mono', text: m.value != null ? m.value : '-' })
      ))
    ));
  }
  // 原始文本（<pre> 保留格式，便于复制）。
  container.appendChild(el('pre', { class: 'metrics-raw', text: text }));
}