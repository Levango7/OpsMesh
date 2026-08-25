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

// ============================================================================
// Phase 2：服务治理渲染
// ============================================================================

// renderTrafficTable 渲染流量策略列表表格。
// handlers: { onEnable(id), onDisable(id), onDelete(id) }
export function renderTrafficTable(container, policies, handlers) {
  container.innerHTML = '';
  if (!policies || policies.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('traffic.policyName') }),
        el('th', { text: t('traffic.service') }),
        el('th', { text: t('traffic.policyType') }),
        el('th', { text: t('traffic.timeout') }),
        el('th', { text: t('traffic.retries') }),
        el('th', { text: t('common.status') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      policies.map((p) => {
        const enabled = p.enabled !== false && p.status !== 'disabled';
        return el('tr', null,
          el('td', { class: 'mono', text: p.id }),
          el('td', { class: 'cell-title', text: p.name }),
          el('td', { text: p.service || '-' }),
          el('td', null, el('span', { class: 'badge badge-priority-medium', text: t('traffic.type.' + (p.type || 'timeout'), p.type || '-') })),
          el('td', { class: 'mono', text: String(p.timeout != null ? p.timeout : '-') }),
          el('td', { class: 'mono', text: String(p.retries != null ? p.retries : '-') }),
          el('td', null, el('span', { class: 'badge badge-status-' + (enabled ? 'resolved' : 'closed'), text: enabled ? t('common.enabled') : t('common.disabled') })),
          el('td', { class: 'td-actions' },
            enabled
              ? el('button', { class: 'btn-icon', title: t('common.disable'), onclick: () => handlers.onDisable(p.id) }, iconEl('toggle_on', 14))
              : el('button', { class: 'btn-icon', title: t('common.enable'), onclick: () => handlers.onEnable(p.id) }, iconEl('toggle_off', 14)),
            el('button', { class: 'btn-icon btn-icon-danger', title: t('common.delete'), onclick: () => handlers.onDelete(p.id) }, iconEl('trash', 14))
          )
        );
      })
    )
  );
  container.appendChild(table);
}

// renderTrafficForm 渲染流量策略创建表单。
// handlers: { onSubmit(data), onCancel() }
export function renderTrafficForm(container, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => { e.preventDefault(); handlers.onSubmit(collectTrafficForm(form)); } });

  form.appendChild(el('h3', { class: 'form-title', text: t('traffic.create') }));

  form.appendChild(fieldRow(t('traffic.policyName'), true,
    el('input', { type: 'text', name: 'name', required: 'true', placeholder: t('traffic.nameRequired') })
  ));
  form.appendChild(fieldRow(t('traffic.service'), true,
    el('input', { type: 'text', name: 'service', required: 'true', placeholder: t('traffic.serviceRequired') })
  ));
  form.appendChild(fieldRow(t('traffic.policyType'), false,
    el('select', { name: 'type' },
      ['timeout', 'retry', 'circuitbreaker', 'ratelimit'].map((tp) =>
        el('option', { value: tp, text: t('traffic.type.' + tp) })
      )
    )
  ));
  form.appendChild(fieldRow(t('traffic.timeout'), false,
    el('input', { type: 'number', name: 'timeout', min: '0', value: '3000' })
  ));
  form.appendChild(fieldRow(t('traffic.retries'), false,
    el('input', { type: 'number', name: 'retries', min: '0', max: '10', value: '3' })
  ));

  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('check', 16), el('span', { text: t('common.save') })),
    el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => handlers.onCancel() }, el('span', { text: t('common.cancel') }))
  ));

  container.appendChild(form);
}

function collectTrafficForm(form) {
  const get = (name) => (form.elements[name] && form.elements[name].value) || '';
  return {
    name: get('name').trim(),
    service: get('service').trim(),
    type: get('type'),
    timeout: parseInt(get('timeout'), 10) || 0,
    retries: parseInt(get('retries'), 10) || 0,
  };
}

// ============================================================================
// Phase 2：CI/CD 流水线渲染
// ============================================================================

// renderPipelineTemplates 渲染流水线模板列表表格。
// handlers: { onRun(id), onDelete(id) }
export function renderPipelineTemplates(container, templates, handlers) {
  container.innerHTML = '';
  if (!templates || templates.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('pipeline.templateName') }),
        el('th', { text: t('pipeline.description') }),
        el('th', { text: t('pipeline.stages') }),
        el('th', { text: t('common.createdAt') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      templates.map((tp) => el('tr', null,
        el('td', { class: 'mono', text: tp.id }),
        el('td', { class: 'cell-title', text: tp.name }),
        el('td', { text: tp.description || '-' }),
        el('td', { class: 'mono', text: String((tp.stages && tp.stages.length) || 0) }),
        el('td', { class: 'mono', text: formatTime(tp.createdAt) }),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn-icon', title: t('pipeline.run'), onclick: () => handlers.onRun(tp.id) }, iconEl('play', 14)),
          el('button', { class: 'btn-icon btn-icon-danger', title: t('common.delete'), onclick: () => handlers.onDelete(tp.id) }, iconEl('trash', 14))
        )
      ))
    )
  );
  container.appendChild(table);
}

// renderPipelineRuns 渲染流水线运行记录表格。
export function renderPipelineRuns(container, runs) {
  container.innerHTML = '';
  if (!runs || runs.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('pipeline.runId') }),
        el('th', { text: t('pipeline.template') }),
        el('th', { text: t('pipeline.status') }),
        el('th', { text: t('pipeline.startedAt') }),
        el('th', { text: t('pipeline.finishedAt') })
      )
    ),
    el('tbody', null,
      runs.map((r) => el('tr', null,
        el('td', { class: 'mono', text: r.id }),
        el('td', { class: 'cell-title', text: r.templateName || r.templateID || '-' }),
        el('td', null, el('span', { class: 'badge badge-status-' + (r.status || 'open'), text: r.status || '-' })),
        el('td', { class: 'mono', text: formatTime(r.startedAt || r.createdAt) }),
        el('td', { class: 'mono', text: formatTime(r.finishedAt || r.updatedAt) })
      ))
    )
  );
  container.appendChild(table);
}

// renderPipelineTemplateForm 渲染流水线模板创建表单。
// handlers: { onSubmit(data), onCancel() }
export function renderPipelineTemplateForm(container, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => { e.preventDefault(); handlers.onSubmit(collectPipelineTemplateForm(form)); } });

  form.appendChild(el('h3', { class: 'form-title', text: t('pipeline.create') }));

  form.appendChild(fieldRow(t('pipeline.templateName'), true,
    el('input', { type: 'text', name: 'name', required: 'true', placeholder: t('pipeline.nameRequired') })
  ));
  form.appendChild(fieldRow(t('pipeline.description'), false,
    el('textarea', { name: 'description', rows: '2' }, '')
  ));
  form.appendChild(fieldRow(t('pipeline.stages'), false,
    el('textarea', { name: 'stages', rows: '4', placeholder: 'build -> test -> deploy' }, '')
  ));

  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('check', 16), el('span', { text: t('common.save') })),
    el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => handlers.onCancel() }, el('span', { text: t('common.cancel') }))
  ));

  container.appendChild(form);
}

function collectPipelineTemplateForm(form) {
  const get = (name) => (form.elements[name] && form.elements[name].value) || '';
  const stagesText = get('stages').trim();
  const stages = stagesText ? stagesText.split('->').map((s) => s.trim()).filter(Boolean) : [];
  return {
    name: get('name').trim(),
    description: get('description'),
    stages,
  };
}

// renderArgoCDApps 渲染 ArgoCD 应用列表。
// handlers: { onSync(id), onDelete(id) }
export function renderArgoCDApps(container, apps, handlers) {
  container.innerHTML = '';
  if (!apps || apps.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.name') }),
        el('th', { text: t('common.status') }),
        el('th', { text: 'repo' }),
        el('th', { text: 'target' }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      apps.map((a) => el('tr', null,
        el('td', { class: 'cell-title', text: a.name }),
        el('td', null, el('span', { class: 'badge badge-status-' + (a.syncStatus === 'Synced' ? 'resolved' : 'in_progress'), text: a.syncStatus || '-' })),
        el('td', { class: 'mono', text: a.repoURL || '-' }),
        el('td', { class: 'mono', text: a.targetRevision || '-' }),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn-icon', title: t('pipeline.argoSync'), onclick: () => handlers.onSync(a.name || a.id) }, iconEl('sync', 14)),
          el('button', { class: 'btn-icon btn-icon-danger', title: t('common.delete'), onclick: () => handlers.onDelete(a.name || a.id) }, iconEl('trash', 14))
        )
      ))
    )
  );
  container.appendChild(table);
}

// ============================================================================
// Phase 2：灰度发布渲染
// ============================================================================

// renderCanaryList 渲染灰度发布列表（带选择）。
// handlers: { onSelect(id) }
export function renderCanaryList(container, releases, handlers) {
  container.innerHTML = '';
  if (!releases || releases.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('common.name') }),
        el('th', { text: t('common.service') }),
        el('th', { text: t('canary.trafficPercent') }),
        el('th', { text: t('common.status') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      releases.map((r) => el('tr', null,
        el('td', { class: 'mono', text: r.id }),
        el('td', { class: 'cell-title', text: r.name || '-' }),
        el('td', { text: r.service || '-' }),
        el('td', { class: 'mono', text: String(r.percent != null ? r.percent : 0) + '%' }),
        el('td', null, el('span', { class: 'badge badge-status-' + (r.status || 'in_progress'), text: r.status || '-' })),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn-icon', title: t('canary.trafficSplit'), onclick: () => handlers.onSelect(r.id) }, iconEl('sliders', 14))
        )
      ))
    )
  );
  container.appendChild(table);
}

// renderCanarySplitPanel 渲染流量分割面板（滑块 + 应用按钮）。
// handlers: { onApply(percent) }
export function renderCanarySplitPanel(container, release, handlers) {
  container.innerHTML = '';
  if (!release) { renderEmpty(container, t('canary.select')); return; }
  const card = el('div', { class: 'detail-card' });
  card.appendChild(el('h3', { class: 'detail-title', text: t('canary.trafficSplit') + ' · ' + (release.name || release.id) }));

  const currentPercent = release.percent != null ? release.percent : 0;
  const sliderRow = el('div', { class: 'form-row' },
    el('label', { class: 'form-label', text: t('canary.trafficPercent') }),
    el('div', { class: 'form-control' },
      el('input', { type: 'range', name: 'percent', min: '0', max: '100', step: '1', value: String(currentPercent), style: { width: '60%', verticalAlign: 'middle' } }),
      el('span', { class: 'mono', id: 'canaryPercentLabel', text: ' ' + currentPercent + '%', style: { marginLeft: '0.6rem' } })
    )
  );
  card.appendChild(sliderRow);

  // 滑块实时更新标签
  const slider = sliderRow.querySelector('input[name=percent]');
  const label = sliderRow.querySelector('#canaryPercentLabel');
  slider.addEventListener('input', () => {
    if (label) label.textContent = ' ' + slider.value + '%';
  });

  card.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'button', class: 'btn btn-primary', onclick: () => handlers.onApply(parseInt(slider.value, 10) || 0) },
      iconEl('check', 16), el('span', { text: t('canary.applySplit') })
    )
  ));

  container.appendChild(card);
}

// renderCanaryMetrics 渲染灰度指标对比表格。
// metrics: { old: {qps, latency, errorRate}, new: {qps, latency, errorRate} }
export function renderCanaryMetrics(container, metrics) {
  container.innerHTML = '';
  if (!metrics) { renderEmpty(container, t('common.empty')); return; }
  const oldM = metrics.old || metrics.baseline || {};
  const newM = metrics.new || metrics.canary || {};
  const card = el('div', { class: 'detail-card' });
  card.appendChild(el('h3', { class: 'detail-title', text: t('canary.metrics') }));
  card.appendChild(el('table', { class: 'data-table data-table-compact' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.name') }),
        el('th', { text: t('canary.oldVersion') }),
        el('th', { text: t('canary.newVersion') })
      )
    ),
    el('tbody', null,
      el('tr', null,
        el('td', { text: t('canary.metricQps') }),
        el('td', { class: 'mono', text: formatNumber(oldM.qps) }),
        el('td', { class: 'mono', text: formatNumber(newM.qps) })
      ),
      el('tr', null,
        el('td', { text: t('canary.metricLatency') }),
        el('td', { class: 'mono', text: formatNumber(oldM.latency) }),
        el('td', { class: 'mono', text: formatNumber(newM.latency) })
      ),
      el('tr', null,
        el('td', { text: t('canary.metricErrorRate') }),
        el('td', { class: 'mono', text: formatNumber(oldM.errorRate) }),
        el('td', { class: 'mono', text: formatNumber(newM.errorRate) })
      )
    )
  ));
  container.appendChild(card);
}

// ============================================================================
// Phase 2：配置热推渲染
// ============================================================================

// renderConfigHotpushForm 渲染配置热推送表单。
// handlers: { onSubmit(data) }
export function renderConfigHotpushForm(container, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => { e.preventDefault(); handlers.onSubmit(collectConfigHotpushForm(form)); } });

  form.appendChild(el('h3', { class: 'form-title', text: t('configPush.hotpush') }));

  form.appendChild(fieldRow(t('configPush.deviceID'), true,
    el('input', { type: 'text', name: 'deviceID', required: 'true', placeholder: t('configPush.deviceRequired') })
  ));
  form.appendChild(fieldRow(t('configPush.configKey'), true,
    el('input', { type: 'text', name: 'key', required: 'true', placeholder: t('configPush.keyRequired') })
  ));
  form.appendChild(fieldRow(t('configPush.configValue'), false,
    el('textarea', { name: 'value', rows: '3' }, '')
  ));
  form.appendChild(fieldRow(t('configPush.configPath'), false,
    el('input', { type: 'text', name: 'path', placeholder: '/etc/opsmesh/config.yaml' })
  ));

  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('rocket', 16), el('span', { text: t('common.push') }))
  ));

  container.appendChild(form);
}

function collectConfigHotpushForm(form) {
  const get = (name) => (form.elements[name] && form.elements[name].value) || '';
  return {
    deviceID: get('deviceID').trim(),
    key: get('key').trim(),
    value: get('value'),
    path: get('path').trim(),
  };
}

// renderConfigCanaryForm 渲染灰度配置发布表单。
// handlers: { onSubmit(data) }
export function renderConfigCanaryForm(container, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => { e.preventDefault(); handlers.onSubmit(collectConfigCanaryForm(form)); } });

  form.appendChild(el('h3', { class: 'form-title', text: t('configPush.canary') }));

  form.appendChild(fieldRow(t('configPush.deviceList'), true,
    el('input', { type: 'text', name: 'devices', required: 'true', placeholder: 'dev1, dev2, dev3' })
  ));
  form.appendChild(fieldRow(t('configPush.canaryPercent'), false,
    el('input', { type: 'number', name: 'percent', min: '0', max: '100', value: '10' })
  ));
  form.appendChild(fieldRow(t('configPush.configKey'), false,
    el('input', { type: 'text', name: 'key' })
  ));
  form.appendChild(fieldRow(t('configPush.configContent'), false,
    el('textarea', { name: 'content', rows: '4' }, '')
  ));

  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('check', 16), el('span', { text: t('common.apply') }))
  ));

  container.appendChild(form);
}

function collectConfigCanaryForm(form) {
  const get = (name) => (form.elements[name] && form.elements[name].value) || '';
  const devices = get('devices').split(',').map((s) => s.trim()).filter(Boolean);
  return {
    devices,
    percent: parseInt(get('percent'), 10) || 0,
    key: get('key').trim(),
    content: get('content'),
  };
}

// renderConfigVersions 渲染配置版本历史表格。
export function renderConfigVersions(container, versions) {
  container.innerHTML = '';
  if (!versions || versions.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.version') }),
        el('th', { text: t('configPush.configKey') }),
        el('th', { text: t('common.value') }),
        el('th', { text: t('common.createdAt') })
      )
    ),
    el('tbody', null,
      versions.map((v) => el('tr', null,
        el('td', { class: 'mono', text: String(v.version != null ? v.version : (v.id || '-')) }),
        el('td', { class: 'mono', text: v.key || '-' }),
        el('td', { class: 'mono', text: String(v.value != null ? v.value : '-').slice(0, 60) }),
        el('td', { class: 'mono', text: formatTime(v.createdAt || v.timestamp) })
      ))
    )
  );
  container.appendChild(table);
}

// ============================================================================
// Phase 2：API 端点汇总（render 层提供，便于调试/文档展示）
// ============================================================================

// renderApiEndpoints 渲染 Phase 2 新增 API 端点列表。
export function renderApiEndpoints(container) {
  container.innerHTML = '';
  const endpoints = [
    { method: 'GET',    path: '/api/v1/traffic/policies',                desc: 'list traffic policies' },
    { method: 'POST',   path: '/api/v1/traffic/policies',                desc: 'create traffic policy' },
    { method: 'DELETE', path: '/api/v1/traffic/policies/{id}',           desc: 'delete traffic policy' },
    { method: 'POST',   path: '/api/v1/traffic/policies/{id}/enable',    desc: 'enable traffic policy' },
    { method: 'POST',   path: '/api/v1/traffic/policies/{id}/disable',   desc: 'disable traffic policy' },
    { method: 'GET',    path: '/api/v1/pipeline/templates',              desc: 'list pipeline templates' },
    { method: 'POST',   path: '/api/v1/pipeline/templates',              desc: 'create pipeline template' },
    { method: 'DELETE', path: '/api/v1/pipeline/templates/{id}',         desc: 'delete pipeline template' },
    { method: 'POST',   path: '/api/v1/pipeline/templates/{id}/run',     desc: 'trigger pipeline run' },
    { method: 'GET',    path: '/api/v1/pipeline/runs',                   desc: 'list pipeline runs' },
    { method: 'GET',    path: '/api/v1/argocd/apps',                     desc: 'list argocd apps' },
    { method: 'POST',   path: '/api/v1/argocd/apps',                     desc: 'create argocd app' },
    { method: 'DELETE', path: '/api/v1/argocd/apps/{id}',                desc: 'delete argocd app' },
    { method: 'POST',   path: '/api/v1/argocd/apps/{id}/sync',           desc: 'sync argocd app' },
    { method: 'GET',    path: '/api/v1/canary/releases',                 desc: 'list canary releases' },
    { method: 'POST',   path: '/api/v1/canary/{id}/traffic-split',       desc: 'set canary traffic split' },
    { method: 'GET',    path: '/api/v1/canary/{id}/metrics',             desc: 'get canary metrics' },
    { method: 'POST', path: '/api/v1/config/hotpush',                  desc: 'hotpush config' },
    { method: 'POST', path: '/api/v1/config/canary',                   desc: 'canary config' },
    { method: 'GET',  path: '/api/v1/config/versions?key=',            desc: 'list config versions' },
    { method: 'GET',  path: '/api/v1/compliance/rules',                desc: 'list compliance rules' },
    { method: 'GET',  path: '/api/v1/compliance/rules/{id}',           desc: 'get compliance rule' },
    { method: 'POST', path: '/api/v1/compliance/scan',                 desc: 'scan compliance' },
    { method: 'GET',  path: '/api/v1/compliance/reports',              desc: 'list compliance reports' },
    { method: 'GET',  path: '/api/v1/compliance/reports/{id}',         desc: 'get compliance report' },
    { method: 'GET',  path: '/api/v1/audit/events?',                   desc: 'query audit events' },
    { method: 'GET',  path: '/api/v1/audit/export?',                   desc: 'export audit logs' },
    { method: 'GET',  path: '/api/v1/ha/status',                       desc: 'get ha status' },
    { method: 'GET',  path: '/api/v1/ha/instances',                    desc: 'list ha instances' },
    { method: 'POST', path: '/api/v1/ha/failover',                     desc: 'manual failover' },
    { method: 'GET',  path: '/api/v1/ha/health',                       desc: 'get ha health' },
    { method: 'POST', path: '/api/v1/backup/create',                   desc: 'create backup' },
    { method: 'GET',  path: '/api/v1/backup/list',                     desc: 'list backups' },
    { method: 'POST', path: '/api/v1/backup/restore',                  desc: 'restore backup' },
    { method: 'DELETE', path: '/api/v1/backup/{id}',                   desc: 'delete backup' },
    { method: 'GET',    path: '/api/v1/network/devices',               desc: 'list network devices' },
    { method: 'POST',   path: '/api/v1/network/devices',               desc: 'create network device' },
    { method: 'GET',    path: '/api/v1/network/devices/{id}',          desc: 'get network device' },
    { method: 'DELETE', path: '/api/v1/network/devices/{id}',          desc: 'delete network device' },
    { method: 'GET',    path: '/api/v1/network/devices/{id}/metrics',  desc: 'get network device metrics' },
    { method: 'POST',   path: '/api/v1/network/devices/{id}/config',   desc: 'deploy config to device' },
    { method: 'POST',   path: '/api/v1/network/discover',              desc: 'discover network subnet' },
    { method: 'GET',    path: '/api/v1/automation/rules',              desc: 'list automation rules' },
    { method: 'POST',   path: '/api/v1/automation/rules',              desc: 'create automation rule' },
    { method: 'GET',    path: '/api/v1/automation/rules/{id}',         desc: 'get automation rule' },
    { method: 'PUT',    path: '/api/v1/automation/rules/{id}',         desc: 'update automation rule' },
    { method: 'DELETE', path: '/api/v1/automation/rules/{id}',         desc: 'delete automation rule' },
    { method: 'POST',   path: '/api/v1/automation/rules/{id}/enable',  desc: 'enable automation rule' },
    { method: 'POST',   path: '/api/v1/automation/rules/{id}/disable', desc: 'disable automation rule' },
    { method: 'POST',   path: '/api/v1/automation/rules/{id}/test',    desc: 'test automation rule' },
    { method: 'GET',    path: '/api/v1/automation/executions',         desc: 'list automation executions' },
    { method: 'GET',    path: '/api/v1/automation/executions/{id}',    desc: 'get automation execution' },
    { method: 'GET',    path: '/api/v1/gateway/routes',                desc: 'list gateway routes' },
    { method: 'POST',   path: '/api/v1/gateway/routes',                desc: 'create gateway route' },
    { method: 'GET',    path: '/api/v1/gateway/routes/{id}',           desc: 'get gateway route' },
    { method: 'PUT',    path: '/api/v1/gateway/routes/{id}',           desc: 'update gateway route' },
    { method: 'DELETE', path: '/api/v1/gateway/routes/{id}',           desc: 'delete gateway route' },
    { method: 'POST',   path: '/api/v1/gateway/routes/{id}/enable',    desc: 'enable gateway route' },
    { method: 'POST',   path: '/api/v1/gateway/routes/{id}/disable',   desc: 'disable gateway route' },
    { method: 'GET',    path: '/api/v1/gateway/stats',                 desc: 'get gateway stats' },
    { method: 'GET',    path: '/api/v1/webhooks',                      desc: 'list webhooks' },
    { method: 'POST',   path: '/api/v1/webhooks',                      desc: 'create webhook' },
    { method: 'GET',    path: '/api/v1/webhooks/{id}',                 desc: 'get webhook' },
    { method: 'PUT',    path: '/api/v1/webhooks/{id}',                 desc: 'update webhook' },
    { method: 'DELETE', path: '/api/v1/webhooks/{id}',                 desc: 'delete webhook' },
    { method: 'POST',   path: '/api/v1/webhooks/{id}/test',            desc: 'test webhook' },
    { method: 'GET',    path: '/api/v1/webhooks/{id}/deliveries',      desc: 'list webhook deliveries' },
    { method: 'GET',    path: '/api/v1/scripts',                       desc: 'list scripts' },
    { method: 'POST',   path: '/api/v1/scripts',                       desc: 'create script' },
    { method: 'GET',    path: '/api/v1/scripts/{id}',                  desc: 'get script' },
    { method: 'PUT',    path: '/api/v1/scripts/{id}',                  desc: 'update script' },
    { method: 'DELETE', path: '/api/v1/scripts/{id}',                  desc: 'delete script' },
    { method: 'POST',   path: '/api/v1/scripts/{id}/execute',          desc: 'execute script on device' },
    { method: 'GET',    path: '/api/v1/scripts/{id}/executions',       desc: 'list script executions' },
  ];
  const methodColor = { GET: 'badge-status-resolved', POST: 'badge-status-open', DELETE: 'badge-priority-urgent', PUT: 'badge-status-in_progress' };
  container.appendChild(el('table', { class: 'data-table data-table-compact' },
    el('thead', null,
      el('tr', null,
        el('th', { text: 'method' }),
        el('th', { text: 'path' }),
        el('th', { text: 'desc' })
      )
    ),
    el('tbody', null,
      endpoints.map((e) => el('tr', null,
        el('td', null, el('span', { class: 'badge ' + (methodColor[e.method] || 'badge-status-closed'), text: e.method })),
        el('td', { class: 'mono', text: e.path }),
        el('td', { text: e.desc })
      ))
    )
  ));
}

// ============================================================================
// Phase 3：安全合规渲染
// ============================================================================

// severityBadge 严重级别 badge。
function severityBadge(level) {
  const map = { low: 'badge-priority-low', medium: 'badge-priority-medium', high: 'badge-priority-high', critical: 'badge-priority-urgent' };
  return badge(level || '-', map[level] || 'badge-priority-low');
}

// renderComplianceRulesTable 渲染合规规则表格。
// handlers: { onSelect(rule) }
export function renderComplianceRulesTable(container, rules, handlers) {
  container.innerHTML = '';
  if (!rules || !rules.length) { renderEmpty(container); return; }
  container.appendChild(el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('compliance.ruleName') }),
        el('th', { text: t('compliance.ruleCategory') }),
        el('th', { text: t('compliance.severity') }),
        el('th', { text: t('compliance.ruleDesc') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      rules.map((r) => el('tr', null,
        el('td', { class: 'cell-title', text: r.name || r.id }),
        el('td', null, badge(r.category || '-', 'badge-category-change')),
        el('td', null, severityBadge(r.severity)),
        el('td', { text: r.description || '-' }),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn btn-ghost', onclick: () => handlers.onSelect && handlers.onSelect(r) },
            iconEl('search', 14), el('span', { text: t('compliance.ruleDetail') })
          )
        )
      ))
    )
  ));
}

// renderComplianceRuleDetail 渲染合规规则详情。
// handlers: { onBack() }
export function renderComplianceRuleDetail(container, rule, handlers) {
  container.innerHTML = '';
  const card = el('div', { class: 'content' });
  card.appendChild(el('h3', { class: 'form-title', text: rule.name || rule.id }));
  card.appendChild(el('p', { class: 'metrics-hint', text: (rule.category || '-') + ' · ' + (rule.severity || '-') }));
  if (rule.description) {
    card.appendChild(el('div', { class: 'form-row' },
      el('label', { class: 'form-label', text: t('compliance.ruleDesc') }),
      el('div', { class: 'form-control', text: rule.description })
    ));
  }
  if (rule.checkScript) {
    card.appendChild(el('div', { class: 'form-row' },
      el('label', { class: 'form-label', text: t('compliance.checkScript') }),
      el('pre', { class: 'mono' }, rule.checkScript)
    ));
  }
  if (rule.fixAdvice) {
    card.appendChild(el('div', { class: 'form-row' },
      el('label', { class: 'form-label', text: t('compliance.fixAdvice') }),
      el('div', { class: 'form-control', text: rule.fixAdvice })
    ));
  }
  card.appendChild(el('div', { class: 'form-actions' },
    el('button', { class: 'btn btn-ghost', onclick: () => handlers.onBack && handlers.onBack() },
      iconEl('back', 14), el('span', { text: t('common.back') })
    )
  ));
  container.appendChild(card);
}

// renderComplianceScanForm 渲染合规扫描表单。
// handlers: { onScan(deviceID) }
export function renderComplianceScanForm(container, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => { e.preventDefault(); handlers.onScan && handlers.onScan(form.elements.deviceID.value.trim()); } });
  form.appendChild(el('h3', { class: 'form-title', text: t('compliance.scan') }));
  form.appendChild(fieldRow(t('compliance.selectDevice'), true,
    el('input', { type: 'text', name: 'deviceID', required: 'true', placeholder: 'device-id' })
  ));
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' },
      iconEl('scan', 16), el('span', { text: t('compliance.startScan') })
    )
  ));
  container.appendChild(form);
}

// renderComplianceReport 渲染合规扫描报告。
export function renderComplianceReport(container, report) {
  container.innerHTML = '';
  if (!report) { renderEmpty(container); return; }
  const card = el('div', { class: 'content' });
  card.appendChild(el('h3', { class: 'form-title', text: t('compliance.reportDetail') }));
  // 概览行
  const overview = el('div', { class: 'form-row' });
  overview.appendChild(el('span', { class: 'badge badge-status-resolved', text: t('compliance.passedRules') + ': ' + (report.passedCount != null ? report.passedCount : '-') }));
  overview.appendChild(el('span', { class: 'badge badge-priority-urgent', text: t('compliance.failedRules') + ': ' + (report.failedCount != null ? report.failedCount : '-') }));
  if (report.score != null) {
    overview.appendChild(el('span', { class: 'badge badge-status-open', text: t('compliance.score') + ': ' + report.score }));
  }
  card.appendChild(overview);
  // 详细结果
  const results = (report.results || report.items || []);
  if (results.length) {
    card.appendChild(el('h4', { text: t('compliance.result') }));
    card.appendChild(el('table', { class: 'data-table data-table-compact' },
      el('thead', null,
        el('tr', null,
          el('th', { text: t('compliance.ruleName') }),
          el('th', { text: t('common.status') }),
          el('th', { text: t('compliance.ruleDesc') })
        )
      ),
      el('tbody', null,
        results.map((it) => el('tr', null,
          el('td', { class: 'cell-title', text: it.rule || it.name || it.id || '-' }),
          el('td', null, badge(it.passed ? t('compliance.passed') : t('compliance.failed'),
            it.passed ? 'badge-status-resolved' : 'badge-priority-urgent')),
          el('td', { text: it.message || it.detail || '-' })
        ))
      )
    ));
  }
  container.appendChild(card);
}

// renderComplianceReportsList 渲染合规报告列表。
// handlers: { onView(report) }
export function renderComplianceReportsList(container, reports, handlers) {
  container.innerHTML = '';
  if (!reports || !reports.length) { renderEmpty(container); return; }
  container.appendChild(el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('compliance.score') }),
        el('th', { text: t('compliance.passedRules') }),
        el('th', { text: t('compliance.failedRules') }),
        el('th', { text: t('common.createdAt') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      reports.map((r) => el('tr', null,
        el('td', { class: 'cell-title mono', text: r.id || '-' }),
        el('td', null, badge(r.score != null ? String(r.score) : '-', 'badge-status-open')),
        el('td', { text: r.passedCount != null ? r.passedCount : '-' }),
        el('td', { text: r.failedCount != null ? r.failedCount : '-' }),
        el('td', { text: formatTime(r.createdAt || r.time) }),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn btn-ghost', onclick: () => handlers.onView && handlers.onView(r) },
            iconEl('search', 14), el('span', { text: t('compliance.reportDetail') })
          )
        )
      ))
    )
  ));
}

// renderAuditQueryForm 渲染审计日志查询表单。
// handlers: { onQuery(params), onExport(params) }
export function renderAuditQueryForm(container, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => { e.preventDefault(); handlers.onQuery && handlers.onQuery(collectAuditQuery(form)); } });
  form.appendChild(el('h3', { class: 'form-title', text: t('compliance.audit') }));
  form.appendChild(fieldRow(t('compliance.auditFrom'), false,
    el('input', { type: 'datetime-local', name: 'from' })
  ));
  form.appendChild(fieldRow(t('compliance.auditTo'), false,
    el('input', { type: 'datetime-local', name: 'to' })
  ));
  form.appendChild(fieldRow(t('compliance.auditUser'), false,
    el('input', { type: 'text', name: 'user', placeholder: 'user id' })
  ));
  form.appendChild(fieldRow(t('compliance.auditAction'), false,
    el('input', { type: 'text', name: 'action', placeholder: 'login / create / delete …' })
  ));
  form.appendChild(fieldRow(t('compliance.auditLimit'), false,
    el('input', { type: 'number', name: 'limit', min: '1', max: '1000', value: '100' })
  ));
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' },
      iconEl('search', 16), el('span', { text: t('compliance.query') })
    ),
    el('button', { type: 'button', class: 'btn btn-secondary', onclick: () => handlers.onExport && handlers.onExport(collectAuditQuery(form)) },
      iconEl('download', 16), el('span', { text: t('compliance.export') })
    )
  ));
  container.appendChild(form);
}

function collectAuditQuery(form) {
  const get = (name) => (form.elements[name] && form.elements[name].value) || '';
  return {
    from: get('from').trim(),
    to: get('to').trim(),
    user: get('user').trim(),
    action: get('action').trim(),
    limit: get('limit').trim(),
  };
}

// renderAuditEventsTable 渲染审计事件表格。
export function renderAuditEventsTable(container, events) {
  container.innerHTML = '';
  if (!events || !events.length) { renderEmpty(container); return; }
  container.appendChild(el('table', { class: 'data-table data-table-compact' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('compliance.auditTime') }),
        el('th', { text: t('compliance.auditUser') }),
        el('th', { text: t('compliance.auditAction') }),
        el('th', { text: t('compliance.auditTarget') }),
        el('th', { text: t('compliance.auditDetail') })
      )
    ),
    el('tbody', null,
      events.map((e) => el('tr', null,
        el('td', { text: formatTime(e.createdAt || e.time) }),
        el('td', { class: 'cell-title', text: e.userID || e.user || '-' }),
        el('td', null, badge(e.action || '-', 'badge-category-change')),
        el('td', { class: 'mono', text: e.target || '-' }),
        el('td', { text: e.detail || '-' })
      ))
    )
  ));
}

// ============================================================================
// Phase 3：高可用渲染
// ============================================================================

// renderHAStatus 渲染 HA 状态卡片。
export function renderHAStatus(container, status) {
  container.innerHTML = '';
  if (!status) { renderEmpty(container); return; }
  const card = el('div', { class: 'content' });
  card.appendChild(el('h3', { class: 'form-title', text: t('ha.status') }));
  card.appendChild(el('div', { class: 'form-row' },
    el('label', { class: 'form-label', text: t('ha.leader') }),
    el('div', { class: 'form-control', text: status.leader || status.leaderID || '-' })
  ));
  if (status.mode) {
    card.appendChild(el('div', { class: 'form-row' },
      el('label', { class: 'form-label', text: 'Mode' }),
      el('div', { class: 'form-control', text: status.mode })
    ));
  }
  if (status.status) {
    card.appendChild(el('div', { class: 'form-row' },
      el('label', { class: 'form-label', text: t('common.status') }),
      el('div', { class: 'form-control' }, badge(status.status, 'badge-status-resolved'))
    ));
  }
  container.appendChild(card);
}

// renderHAInstancesTable 渲染 HA 实例列表。
export function renderHAInstancesTable(container, instances) {
  container.innerHTML = '';
  if (!instances || !instances.length) { renderEmpty(container); return; }
  container.appendChild(el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('ha.role') }),
        el('th', { text: t('ha.isLeader') }),
        el('th', { text: t('common.status') }),
        el('th', { text: t('common.createdAt') })
      )
    ),
    el('tbody', null,
      instances.map((ins) => el('tr', null,
        el('td', { class: 'cell-title mono', text: ins.id || ins.instanceID || '-' }),
        el('td', null, badge(ins.role || '-', 'badge-category-change')),
        el('td', null, ins.isLeader ? badge('Leader', 'badge-status-resolved') : el('span', { text: '-' })),
        el('td', null, badge(ins.status || '-', ins.status === 'healthy' ? 'badge-status-resolved' : 'badge-priority-urgent')),
        el('td', { text: formatTime(ins.createdAt || ins.joinedAt) })
      ))
    )
  ));
}

// renderHAHealth 渲染 HA 健康状态。
export function renderHAHealth(container, health) {
  container.innerHTML = '';
  if (!health) { renderEmpty(container); return; }
  const card = el('div', { class: 'content' });
  card.appendChild(el('h3', { class: 'form-title', text: t('ha.health') }));
  const fields = ['status', 'leader', 'quorum', 'uptime', 'lastCheck'];
  fields.forEach((f) => {
    if (health[f] != null && health[f] !== '') {
      card.appendChild(el('div', { class: 'form-row' },
        el('label', { class: 'form-label', text: f }),
        el('div', { class: 'form-control', text: String(health[f]) })
      ));
    }
  });
  container.appendChild(card);
}

// renderBackupsTable 渲染备份列表。
// handlers: { onRestore(b), onDelete(b) }
export function renderBackupsTable(container, backups, handlers) {
  container.innerHTML = '';
  if (!backups || !backups.length) { renderEmpty(container); return; }
  container.appendChild(el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('ha.backupId') }),
        el('th', { text: t('ha.backupType') }),
        el('th', { text: t('ha.backupTime') }),
        el('th', { text: t('ha.backupSize') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      backups.map((b) => el('tr', null,
        el('td', { class: 'cell-title mono', text: b.id || '-' }),
        el('td', null, badge(b.type || '-', 'badge-category-change')),
        el('td', { text: formatTime(b.createdAt || b.time) }),
        el('td', { text: b.size != null ? b.size : '-' }),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn btn-ghost', title: t('ha.restore'), onclick: () => handlers.onRestore && handlers.onRestore(b) },
            iconEl('restore', 14)
          ),
          el('button', { class: 'btn btn-ghost btn-icon-danger', title: t('ha.deleteBackup'), onclick: () => handlers.onDelete && handlers.onDelete(b) },
            iconEl('trash', 14)
          )
        )
      ))
    )
  ));
}

// renderCreateBackupForm 渲染创建备份表单。
// handlers: { onCreate(type) }
export function renderCreateBackupForm(container, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => { e.preventDefault(); handlers.onCreate && handlers.onCreate(form.elements.type.value.trim()); } });
  form.appendChild(el('h3', { class: 'form-title', text: t('ha.createBackup') }));
  form.appendChild(fieldRow(t('ha.backupType'), true,
    el('select', { name: 'type', required: 'true' },
      el('option', { value: 'full', text: 'full' }),
      el('option', { value: 'incremental', text: 'incremental' }),
      el('option', { value: 'config', text: 'config' })
    )
  ));
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' },
      iconEl('backup', 16), el('span', { text: t('ha.createBackup') })
    )
  ));
  container.appendChild(form);
}

// ============================================================================
// Phase 4：网络管理渲染
// ============================================================================

// networkStatusBadge 网络设备状态 badge。
function networkStatusBadge(status) {
  const s = String(status || '').toLowerCase();
  const cls = s === 'online' || s === 'up' || s === 'healthy' ? 'badge-status-resolved'
    : s === 'offline' || s === 'down' ? 'badge-priority-urgent'
    : 'badge-status-in_progress';
  return badge(status || t('network.unknown'), cls);
}

// renderNetworkDevicesTable 渲染网络设备列表表格。
// handlers: { onDetail(device), onDelete(device) }
export function renderNetworkDevicesTable(container, devices, handlers) {
  container.innerHTML = '';
  if (!devices || !devices.length) { renderEmpty(container, t('network.noDevices')); return; }
  container.appendChild(el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('network.deviceName') }),
        el('th', { text: t('network.deviceType') }),
        el('th', { text: t('network.deviceIP') }),
        el('th', { text: t('network.status') }),
        el('th', { text: t('network.bandwidth') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      devices.map((d) => el('tr', null,
        el('td', { class: 'cell-title', text: d.name || d.id || '-' }),
        el('td', null, badge(d.type || '-', 'badge-category-change')),
        el('td', { class: 'mono', text: d.ip || d.managementIP || '-' }),
        el('td', null, networkStatusBadge(d.status)),
        el('td', { text: d.bandwidth != null ? (d.bandwidth + '%') : '-' }),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn btn-ghost', title: t('network.deviceDetail'), onclick: () => handlers.onDetail && handlers.onDetail(d) },
            iconEl('search', 14)
          ),
          el('button', { class: 'btn btn-ghost btn-icon-danger', title: t('common.delete'), onclick: () => handlers.onDelete && handlers.onDelete(d) },
            iconEl('trash', 14)
          )
        )
      ))
    )
  ));
}

// renderNetworkDeviceForm 渲染添加网络设备表单。
// handlers: { onCreate(data) }
export function renderNetworkDeviceForm(container, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const data = {
      name: form.elements.name.value.trim(),
      type: form.elements.type.value.trim(),
      ip: form.elements.ip.value.trim(),
      port: parseInt(form.elements.port.value, 10) || 22,
      vendor: form.elements.vendor.value.trim(),
      model: form.elements.model.value.trim(),
    };
    handlers.onCreate && handlers.onCreate(data);
  } });
  form.appendChild(el('h3', { class: 'form-title', text: t('network.addDevice') }));
  form.appendChild(fieldRow(t('network.deviceName'), true,
    el('input', { name: 'name', type: 'text', required: 'true', placeholder: 'switch-01' })
  ));
  form.appendChild(fieldRow(t('network.deviceType'), true,
    el('select', { name: 'type', required: 'true' },
      el('option', { value: 'switch', text: 'switch' }),
      el('option', { value: 'router', text: 'router' }),
      el('option', { value: 'firewall', text: 'firewall' }),
      el('option', { value: 'loadbalancer', text: 'loadbalancer' }),
      el('option', { value: 'ap', text: 'AP' })
    )
  ));
  form.appendChild(fieldRow(t('network.deviceIP'), true,
    el('input', { name: 'ip', type: 'text', required: 'true', placeholder: '192.168.1.1' })
  ));
  form.appendChild(fieldRow(t('network.devicePort'), false,
    el('input', { name: 'port', type: 'number', value: '22', min: '1', max: '65535' })
  ));
  form.appendChild(fieldRow(t('network.vendor'), false,
    el('input', { name: 'vendor', type: 'text', placeholder: 'Cisco/Huawei/Juniper' })
  ));
  form.appendChild(fieldRow(t('network.model'), false,
    el('input', { name: 'model', type: 'text', placeholder: 'Catalyst 2960' })
  ));
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' },
      iconEl('plus', 16), el('span', { text: t('network.addDevice') })
    )
  ));
  container.appendChild(form);
}

// renderNetworkDeviceMetrics 渲染网络设备监控指标。
export function renderNetworkDeviceMetrics(container, metrics) {
  container.innerHTML = '';
  if (!metrics) { renderEmpty(container); return; }
  const card = el('div', { class: 'content' });
  card.appendChild(el('h3', { class: 'form-title', text: t('network.metrics') }));
  const fields = [
    { key: 'bandwidth', label: t('network.bandwidthUsage'), suffix: '%' },
    { key: 'latency', label: t('network.latency'), suffix: 'ms' },
    { key: 'packetLoss', label: t('network.packetLoss'), suffix: '%' },
    { key: 'cpu', label: 'CPU', suffix: '%' },
    { key: 'memory', label: 'Memory', suffix: '%' },
    { key: 'uptime', label: 'Uptime', suffix: '' },
  ];
  fields.forEach((f) => {
    const v = metrics[f.key];
    if (v != null && v !== '') {
      card.appendChild(el('div', { class: 'form-row' },
        el('label', { class: 'form-label', text: f.label }),
        el('div', { class: 'form-control', text: String(v) + f.suffix })
      ));
    }
  });
  container.appendChild(card);
}

// renderNetworkDiscoverForm 渲染网络发现表单。
// handlers: { onDiscover(subnet) }
export function renderNetworkDiscoverForm(container, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    handlers.onDiscover && handlers.onDiscover(form.elements.subnet.value.trim());
  } });
  form.appendChild(el('h3', { class: 'form-title', text: t('network.discover') }));
  form.appendChild(fieldRow(t('network.subnet'), true,
    el('input', { name: 'subnet', type: 'text', required: 'true', placeholder: t('network.subnetPlaceholder') })
  ));
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' },
      iconEl('discover', 16), el('span', { text: t('network.startDiscover') })
    )
  ));
  container.appendChild(form);
}

// renderNetworkDiscoverResult 渲染网络发现结果。
export function renderNetworkDiscoverResult(container, devices) {
  container.innerHTML = '';
  if (!devices || !devices.length) { renderEmpty(container, t('network.noDiscovered')); return; }
  container.appendChild(el('h3', { class: 'form-title', text: t('network.discoveredDevices') }));
  container.appendChild(el('table', { class: 'data-table data-table-compact' },
    el('thead', null,
      el('tr', null,
        el('th', { text: 'IP' }),
        el('th', { text: t('network.deviceType') }),
        el('th', { text: t('network.vendor') }),
        el('th', { text: t('network.status') })
      )
    ),
    el('tbody', null,
      devices.map((d) => el('tr', null,
        el('td', { class: 'cell-title mono', text: d.ip || d.ipAddress || '-' }),
        el('td', { text: d.type || '-' }),
        el('td', { text: d.vendor || d.vendorName || '-' }),
        el('td', null, networkStatusBadge(d.status))
      ))
    )
  ));
}

// renderNetworkConfigForm 渲染配置下发表单。
// devices: 可选设备列表；handlers: { onDeploy(deviceId, config) }
export function renderNetworkConfigForm(container, devices, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const deviceId = form.elements.device.value;
    const config = form.elements.config.value;
    handlers.onDeploy && handlers.onDeploy(deviceId, config);
  } });
  form.appendChild(el('h3', { class: 'form-title', text: t('network.config') }));
  form.appendChild(fieldRow(t('network.selectDevice'), true,
    el('select', { name: 'device', required: 'true' },
      el('option', { value: '', text: '-- ' + t('network.selectDevice') + ' --' }),
      (devices || []).map((d) => el('option', { value: d.id || d.name, text: (d.name || d.id) + ' (' + (d.ip || '-') + ')' }))
    )
  ));
  form.appendChild(fieldRow(t('network.configContent'), true,
    el('textarea', { name: 'config', rows: '6', required: 'true', placeholder: t('network.configPlaceholder'), style: { width: '100%', fontFamily: 'monospace' } })
  ));
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' },
      iconEl('config_deploy', 16), el('span', { text: t('network.deployConfig') })
    )
  ));
  container.appendChild(form);
}

// ============================================================================
// Phase 4：自动化闭环渲染
// ============================================================================

// automationStatusBadge 自动化规则状态 badge。
function automationStatusBadge(status) {
  const s = String(status || '').toLowerCase();
  if (s === 'enabled' || s === 'active') return badge(t('automation.enabled'), 'badge-status-resolved');
  if (s === 'disabled' || s === 'inactive') return badge(t('automation.disabled'), 'badge-status-closed');
  return badge(status || '-', 'badge-status-in_progress');
}

// renderAutomationRulesTable 渲染自动化规则列表表格。
// handlers: { onEdit(rule), onEnable(rule), onDisable(rule), onTest(rule), onDelete(rule) }
export function renderAutomationRulesTable(container, rules, handlers) {
  container.innerHTML = '';
  if (!rules || !rules.length) { renderEmpty(container, t('automation.noRules')); return; }
  container.appendChild(el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('automation.ruleName') }),
        el('th', { text: t('automation.trigger') }),
        el('th', { text: t('automation.action') }),
        el('th', { text: t('automation.status') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      rules.map((r) => {
        const enabled = String(r.status || '').toLowerCase() === 'enabled' || String(r.status || '').toLowerCase() === 'active';
        return el('tr', null,
          el('td', { class: 'cell-title', text: r.name || r.id || '-' }),
          el('td', { class: 'mono', text: r.trigger || '-' }),
          el('td', { class: 'mono', text: r.action || '-' }),
          el('td', null, automationStatusBadge(r.status)),
          el('td', { class: 'td-actions' },
            el('button', { class: 'btn btn-ghost', title: t('automation.edit'), onclick: () => handlers.onEdit && handlers.onEdit(r) },
              iconEl('edit', 14)
            ),
            enabled
              ? el('button', { class: 'btn btn-ghost', title: t('automation.disable'), onclick: () => handlers.onDisable && handlers.onDisable(r) },
                  iconEl('disable', 14)
                )
              : el('button', { class: 'btn btn-ghost', title: t('automation.enable'), onclick: () => handlers.onEnable && handlers.onEnable(r) },
                  iconEl('enable', 14)
                ),
            el('button', { class: 'btn btn-ghost', title: t('automation.test'), onclick: () => handlers.onTest && handlers.onTest(r) },
              iconEl('test', 14)
            ),
            el('button', { class: 'btn btn-ghost btn-icon-danger', title: t('automation.delete'), onclick: () => handlers.onDelete && handlers.onDelete(r) },
              iconEl('trash', 14)
            )
          )
        );
      })
    )
  ));
}

// renderAutomationRuleForm 渲染创建/编辑自动化规则表单。
// rule: 编辑时传入现有规则，创建时传 null；handlers: { onSubmit(data) }
export function renderAutomationRuleForm(container, rule, handlers) {
  container.innerHTML = '';
  const isEdit = !!rule;
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const data = {
      name: form.elements.name.value.trim(),
      description: form.elements.description.value.trim(),
      trigger: form.elements.trigger.value.trim(),
      condition: form.elements.condition.value.trim(),
      action: form.elements.action.value.trim(),
      params: form.elements.params.value.trim(),
    };
    handlers.onSubmit && handlers.onSubmit(data);
  } });
  form.appendChild(el('h3', { class: 'form-title', text: isEdit ? t('automation.editRule') : t('automation.createRule') }));
  form.appendChild(fieldRow(t('automation.ruleName'), true,
    el('input', { name: 'name', type: 'text', required: 'true', value: (rule && rule.name) || '', placeholder: 'high-cpu-scale-up' })
  ));
  form.appendChild(fieldRow(t('automation.ruleDesc'), false,
    el('input', { name: 'description', type: 'text', value: (rule && rule.description) || '', placeholder: 'CPU > 80% 自动扩容' })
  ));
  form.appendChild(fieldRow(t('automation.trigger'), true,
    el('input', { name: 'trigger', type: 'text', required: 'true', value: (rule && rule.trigger) || '', placeholder: t('automation.triggerPlaceholder') })
  ));
  form.appendChild(fieldRow(t('automation.condition'), false,
    el('input', { name: 'condition', type: 'text', value: (rule && rule.condition) || '', placeholder: t('automation.conditionPlaceholder') })
  ));
  form.appendChild(fieldRow(t('automation.action'), true,
    el('input', { name: 'action', type: 'text', required: 'true', value: (rule && rule.action) || '', placeholder: t('automation.actionPlaceholder') })
  ));
  form.appendChild(fieldRow(t('automation.params'), false,
    el('input', { name: 'params', type: 'text', value: (rule && rule.params) || '', placeholder: t('automation.paramsPlaceholder') })
  ));
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' },
      iconEl('check', 16), el('span', { text: isEdit ? t('automation.editRule') : t('automation.createRule') })
    )
  ));
  container.appendChild(form);
}

// renderAutomationExecutionsTable 渲染自动化执行历史表格。
// handlers: { onDetail(exec) }
export function renderAutomationExecutionsTable(container, execs, handlers) {
  container.innerHTML = '';
  if (!execs || !execs.length) { renderEmpty(container, t('automation.noExecutions')); return; }
  container.appendChild(el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('automation.execId') }),
        el('th', { text: t('automation.execRule') }),
        el('th', { text: t('automation.execStatus') }),
        el('th', { text: t('automation.execTime') }),
        el('th', { text: t('automation.execDuration') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      execs.map((e) => {
        const s = String(e.status || '').toLowerCase();
        const statusCls = s === 'success' || s === 'succeeded' ? 'badge-status-resolved'
          : s === 'failed' || s === 'error' ? 'badge-priority-urgent'
          : s === 'running' ? 'badge-status-in_progress'
          : 'badge-status-closed';
        return el('tr', null,
          el('td', { class: 'cell-title mono', text: e.id || e.executionID || '-' }),
          el('td', { text: e.ruleName || e.rule || '-' }),
          el('td', null, badge(e.status || '-', statusCls)),
          el('td', { text: formatTime(e.createdAt || e.time || e.startedAt) }),
          el('td', { text: e.duration != null ? (e.duration + 'ms') : '-' }),
          el('td', { class: 'td-actions' },
            el('button', { class: 'btn btn-ghost', title: t('automation.execOutput'), onclick: () => handlers.onDetail && handlers.onDetail(e) },
              iconEl('search', 14)
            )
          )
        );
      })
    )
  ));
}

// renderAutomationExecutionDetail 渲染自动化执行详情。
export function renderAutomationExecutionDetail(container, exec) {
  container.innerHTML = '';
  if (!exec) { renderEmpty(container); return; }
  const card = el('div', { class: 'content' });
  card.appendChild(el('h3', { class: 'form-title', text: t('automation.execOutput') }));
  card.appendChild(el('div', { class: 'form-row' },
    el('label', { class: 'form-label', text: t('automation.execId') }),
    el('div', { class: 'form-control mono', text: exec.id || exec.executionID || '-' })
  ));
  card.appendChild(el('div', { class: 'form-row' },
    el('label', { class: 'form-label', text: t('automation.execRule') }),
    el('div', { class: 'form-control', text: exec.ruleName || exec.rule || '-' })
  ));
  card.appendChild(el('div', { class: 'form-row' },
    el('label', { class: 'form-label', text: t('automation.execStatus') }),
    el('div', { class: 'form-control' }, badge(exec.status || '-', 'badge-status-in_progress'))
  ));
  if (exec.output != null) {
    card.appendChild(el('div', { class: 'form-row' },
      el('label', { class: 'form-label', text: t('automation.execOutput') }),
      el('pre', { class: 'form-control', style: { whiteSpace: 'pre-wrap', fontFamily: 'monospace', fontSize: '.85rem' }, text: String(exec.output) })
    ));
  }
  if (exec.error != null && exec.error !== '') {
    card.appendChild(el('div', { class: 'form-row' },
      el('label', { class: 'form-label', text: 'Error' }),
      el('pre', { class: 'form-control', style: { whiteSpace: 'pre-wrap', fontFamily: 'monospace', fontSize: '.85rem', color: 'var(--danger, #c0392b)' }, text: String(exec.error) })
    ));
  }
  container.appendChild(card);
}

// ============================================================================
// Phase 5：扩展能力渲染（API 网关 / Webhook / 自定义脚本）
// ============================================================================

// --- API 网关 ---

// gatewayRouteStatusBadge 网关路由状态 badge。
function gatewayRouteStatusBadge(status) {
  const s = String(status || '').toLowerCase();
  if (s === 'enabled' || s === 'active') return badge(t('gateway.routeEnabled'), 'badge-status-resolved');
  if (s === 'disabled' || s === 'inactive') return badge(t('gateway.routeDisabled'), 'badge-status-closed');
  return badge(status || '-', 'badge-status-in_progress');
}

// renderGatewayStats 渲染网关统计卡片。
export function renderGatewayStats(container, stats) {
  container.innerHTML = '';
  if (!stats) { renderEmpty(container); return; }
  const cards = [
    { label: t('gateway.statsTotal'),  value: stats.totalRequests != null ? stats.totalRequests : (stats.total || 0),     icon: 'stats' },
    { label: t('gateway.statsActive'), value: stats.activeRoutes != null ? stats.activeRoutes : (stats.active || 0),       icon: 'route' },
    { label: t('gateway.statsQps'),    value: stats.qps != null ? stats.qps : (stats.currentQps || 0),                     icon: 'dashboard' },
    { label: t('gateway.statsErrors'), value: stats.totalErrors != null ? stats.totalErrors : (stats.errors || 0),         icon: 'alert' },
  ];
  const grid = el('div', { class: 'stats-grid', style: { display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: '1rem', marginBottom: '1rem' } });
  cards.forEach((c) => {
    grid.appendChild(el('div', { class: 'stat-card content' },
      el('div', { class: 'stat-card-head', style: { display: 'flex', alignItems: 'center', gap: '.5rem', color: 'var(--text-muted, #6b7280)' } },
        iconEl(c.icon, 16),
        el('span', { text: c.label })
      ),
      el('div', { class: 'stat-card-value', style: { fontSize: '1.5rem', fontWeight: '600', marginTop: '.5rem' }, text: String(c.value) })
    ));
  });
  container.appendChild(grid);
}

// renderGatewayRoutesTable 渲染网关路由表格。
// handlers: { onEdit(route), onToggle(route), onDelete(route) }
export function renderGatewayRoutesTable(container, routes, handlers) {
  container.innerHTML = '';
  if (!routes || !routes.length) { renderEmpty(container, t('gateway.noRoutes')); return; }
  container.appendChild(el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('gateway.routeName') }),
        el('th', { text: t('gateway.routeMethod') }),
        el('th', { text: t('gateway.routePath') }),
        el('th', { text: t('gateway.routeTarget') }),
        el('th', { text: t('gateway.routeStatus') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      routes.map((r) => {
        const enabled = String(r.status || '').toLowerCase() === 'enabled' || String(r.status || '').toLowerCase() === 'active';
        return el('tr', null,
          el('td', { class: 'cell-title', text: r.name || r.id || '-' }),
          el('td', null, el('span', { class: 'badge badge-status-in_progress', text: r.method || '*' })),
          el('td', { class: 'mono', text: r.path || '-' }),
          el('td', { class: 'mono', text: r.target || r.upstream || '-' }),
          el('td', null, gatewayRouteStatusBadge(r.status)),
          el('td', { class: 'td-actions' },
            el('button', { class: 'btn btn-ghost', title: t('common.edit'), onclick: () => handlers.onEdit && handlers.onEdit(r) },
              iconEl('edit', 14)
            ),
            enabled
              ? el('button', { class: 'btn btn-ghost', title: t('gateway.disable'), onclick: () => handlers.onToggle && handlers.onToggle(r, 'disable') },
                  iconEl('disable', 14)
                )
              : el('button', { class: 'btn btn-ghost', title: t('gateway.enable'), onclick: () => handlers.onToggle && handlers.onToggle(r, 'enable') },
                  iconEl('enable', 14)
                ),
            el('button', { class: 'btn btn-ghost btn-icon-danger', title: t('common.delete'), onclick: () => handlers.onDelete && handlers.onDelete(r) },
              iconEl('trash', 14)
            )
          )
        );
      })
    )
  ));
}

// renderGatewayRouteForm 渲染创建/编辑网关路由表单。
// route: 编辑时传入现有路由，创建时传 null；handlers: { onSubmit(data) }
export function renderGatewayRouteForm(container, route, handlers) {
  container.innerHTML = '';
  const isEdit = !!route;
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const data = {
      name: form.elements.name.value.trim(),
      method: form.elements.method.value.trim(),
      path: form.elements.path.value.trim(),
      target: form.elements.target.value.trim(),
      description: form.elements.description.value.trim(),
    };
    handlers.onSubmit && handlers.onSubmit(data);
  } });
  form.appendChild(el('h3', { class: 'form-title', text: isEdit ? t('gateway.editRoute') : t('gateway.createRoute') }));
  form.appendChild(fieldRow(t('gateway.routeName'), true,
    el('input', { name: 'name', type: 'text', required: 'true', value: (route && route.name) || '', placeholder: 'user-api-route' })
  ));
  form.appendChild(fieldRow(t('gateway.routeMethod'), false,
    el('input', { name: 'method', type: 'text', value: (route && route.method) || '*', placeholder: t('gateway.methodPlaceholder') })
  ));
  form.appendChild(fieldRow(t('gateway.routePath'), true,
    el('input', { name: 'path', type: 'text', required: 'true', value: (route && route.path) || '', placeholder: t('gateway.pathPlaceholder') })
  ));
  form.appendChild(fieldRow(t('gateway.routeTarget'), true,
    el('input', { name: 'target', type: 'text', required: 'true', value: (route && (route.target || route.upstream)) || '', placeholder: t('gateway.targetPlaceholder') })
  ));
  form.appendChild(fieldRow(t('common.description'), false,
    el('input', { name: 'description', type: 'text', value: (route && route.description) || '', placeholder: t('common.description') })
  ));
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' },
      iconEl('check', 16), el('span', { text: isEdit ? t('gateway.editRoute') : t('gateway.createRoute') })
    )
  ));
  container.appendChild(form);
}

// --- Webhook ---

// webhookStatusBadge Webhook 状态 badge。
function webhookStatusBadge(status) {
  const s = String(status || '').toLowerCase();
  if (s === 'enabled' || s === 'active') return badge(t('webhook.enabled'), 'badge-status-resolved');
  if (s === 'disabled' || s === 'inactive') return badge(t('webhook.disabled'), 'badge-status-closed');
  return badge(status || '-', 'badge-status-in_progress');
}

// renderWebhooksTable 渲染 Webhook 列表表格。
// handlers: { onEdit(wh), onTest(wh), onDeliveries(wh), onDelete(wh) }
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

// scriptStatusBadge 脚本状态 badge。
function scriptStatusBadge(status) {
  const s = String(status || '').toLowerCase();
  if (s === 'enabled' || s === 'active') return badge(t('script.enabled'), 'badge-status-resolved');
  if (s === 'disabled' || s === 'inactive') return badge(t('script.disabled'), 'badge-status-closed');
  return badge(status || '-', 'badge-status-in_progress');
}

// renderScriptsTable 渲染自定义脚本列表表格。
// handlers: { onEdit(s), onExecute(s), onExecutions(s), onDelete(s) }
export function renderScriptsTable(container, scripts, handlers) {
  container.innerHTML = '';
  if (!scripts || !scripts.length) { renderEmpty(container, t('script.noScripts')); return; }
  container.appendChild(el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('script.name') }),
        el('th', { text: t('script.runtime') }),
        el('th', { text: t('script.desc2') }),
        el('th', { text: t('script.status') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      scripts.map((s) => el('tr', null,
        el('td', { class: 'cell-title', text: s.name || s.id || '-' }),
        el('td', { class: 'mono', text: s.runtime || s.language || '-' }),
        el('td', { text: s.description || '-' }),
        el('td', null, scriptStatusBadge(s.status)),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn btn-ghost', title: t('common.edit'), onclick: () => handlers.onEdit && handlers.onEdit(s) },
            iconEl('edit', 14)
          ),
          el('button', { class: 'btn btn-ghost', title: t('script.execute'), onclick: () => handlers.onExecute && handlers.onExecute(s) },
            iconEl('execute', 14)
          ),
          el('button', { class: 'btn btn-ghost', title: t('script.showExecutions'), onclick: () => handlers.onExecutions && handlers.onExecutions(s) },
            iconEl('history', 14)
          ),
          el('button', { class: 'btn btn-ghost btn-icon-danger', title: t('common.delete'), onclick: () => handlers.onDelete && handlers.onDelete(s) },
            iconEl('trash', 14)
          )
        )
      ))
    )
  ));
}

// renderScriptForm 渲染创建/编辑脚本表单（含代码编辑区）。
// script: 编辑时传入现有脚本，创建时传 null；handlers: { onSubmit(data) }
export function renderScriptForm(container, script, handlers) {
  container.innerHTML = '';
  const isEdit = !!script;
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const data = {
      name: form.elements.name.value.trim(),
      runtime: form.elements.runtime.value.trim(),
      description: form.elements.description.value.trim(),
      code: form.elements.code.value,
    };
    handlers.onSubmit && handlers.onSubmit(data);
  } });
  form.appendChild(el('h3', { class: 'form-title', text: isEdit ? t('script.edit') : t('script.create') }));
  form.appendChild(fieldRow(t('script.name'), true,
    el('input', { name: 'name', type: 'text', required: 'true', value: (script && script.name) || '', placeholder: 'check-disk-usage' })
  ));
  form.appendChild(fieldRow(t('script.runtime'), true,
    el('input', { name: 'runtime', type: 'text', required: 'true', value: (script && (script.runtime || script.language)) || 'python3', placeholder: t('script.runtimePlaceholder') })
  ));
  form.appendChild(fieldRow(t('script.desc2'), false,
    el('input', { name: 'description', type: 'text', value: (script && script.description) || '', placeholder: t('script.desc2') })
  ));
  form.appendChild(fieldRow(t('script.code'), true,
    el('textarea', { name: 'code', rows: '10', required: 'true', placeholder: t('script.codePlaceholder'), style: { width: '100%', fontFamily: 'monospace', fontSize: '.85rem' } }, (script && script.code) || '')
  ));
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' },
      iconEl('check', 16), el('span', { text: isEdit ? t('script.edit') : t('script.create') })
    )
  ));
  container.appendChild(form);
}

// renderScriptExecuteForm 渲染脚本执行表单（输入 deviceId + params）。
// handlers: { onExecute(deviceId, params) }
export function renderScriptExecuteForm(container, script, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const deviceId = form.elements.deviceId.value.trim();
    let params = {};
    try { params = JSON.parse(form.elements.params.value || '{}'); } catch (_) { params = {}; }
    handlers.onExecute && handlers.onExecute(deviceId, params);
  } });
  form.appendChild(el('h3', { class: 'form-title', text: t('script.executeOnDevice') + (script && script.name ? ' · ' + script.name : '') }));
  form.appendChild(fieldRow(t('script.execDeviceId'), true,
    el('input', { name: 'deviceId', type: 'text', required: 'true', placeholder: 'device-001' })
  ));
  form.appendChild(fieldRow(t('script.execParams'), false,
    el('textarea', { name: 'params', rows: '4', placeholder: t('script.execParamsPlaceholder'), style: { width: '100%', fontFamily: 'monospace', fontSize: '.85rem' } }, '{}')
  ));
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' },
      iconEl('execute', 16), el('span', { text: t('script.execute') })
    )
  ));
  container.appendChild(form);
}

// renderScriptExecutionsTable 渲染脚本执行历史表格。
// handlers: { onDetail(exec) }
export function renderScriptExecutionsTable(container, execs, handlers) {
  container.innerHTML = '';
  if (!execs || !execs.length) { renderEmpty(container, t('script.noExecutions')); return; }
  container.appendChild(el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('script.execId') }),
        el('th', { text: t('script.execStatus') }),
        el('th', { text: t('script.execTime') }),
        el('th', { text: t('script.execDuration') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      execs.map((e) => {
        const s = String(e.status || '').toLowerCase();
        const statusCls = s === 'success' || s === 'succeeded' ? 'badge-status-resolved'
          : s === 'failed' || s === 'error' ? 'badge-priority-urgent'
          : s === 'running' ? 'badge-status-in_progress'
          : 'badge-status-closed';
        return el('tr', null,
          el('td', { class: 'cell-title mono', text: e.id || e.executionID || '-' }),
          el('td', null, badge(e.status || '-', statusCls)),
          el('td', { text: formatTime(e.createdAt || e.time || e.startedAt) }),
          el('td', { text: e.duration != null ? (e.duration + 'ms') : '-' }),
          el('td', { class: 'td-actions' },
            el('button', { class: 'btn btn-ghost', title: t('script.execOutput'), onclick: () => handlers.onDetail && handlers.onDetail(e) },
              iconEl('search', 14)
            )
          )
        );
      })
    )
  ));
}

// renderScriptExecutionDetail 渲染脚本执行详情。
export function renderScriptExecutionDetail(container, exec) {
  container.innerHTML = '';
  if (!exec) { renderEmpty(container); return; }
  const card = el('div', { class: 'content' });
  card.appendChild(el('h3', { class: 'form-title', text: t('script.execOutput') }));
  card.appendChild(el('div', { class: 'form-row' },
    el('label', { class: 'form-label', text: t('script.execId') }),
    el('div', { class: 'form-control mono', text: exec.id || exec.executionID || '-' })
  ));
  card.appendChild(el('div', { class: 'form-row' },
    el('label', { class: 'form-label', text: t('script.execStatus') }),
    el('div', { class: 'form-control' }, badge(exec.status || '-', 'badge-status-in_progress'))
  ));
  if (exec.output != null) {
    card.appendChild(el('div', { class: 'form-row' },
      el('label', { class: 'form-label', text: t('script.execOutput') }),
      el('pre', { class: 'form-control', style: { whiteSpace: 'pre-wrap', fontFamily: 'monospace', fontSize: '.85rem' }, text: String(exec.output) })
    ));
  }
  if (exec.error != null && exec.error !== '') {
    card.appendChild(el('div', { class: 'form-row' },
      el('label', { class: 'form-label', text: 'Error' }),
      el('pre', { class: 'form-control', style: { whiteSpace: 'pre-wrap', fontFamily: 'monospace', fontSize: '.85rem', color: 'var(--danger, #c0392b)' }, text: String(exec.error) })
    ));
  }
  container.appendChild(card);
}