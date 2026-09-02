// render-gateway.js — API 网关渲染（由 render.js 拆分）。

// 渲染子模块 — 由 render.js 拆分而来。
// 公共依赖：i18n（t）、icons（iconEl/iconHtml）、render-common（DOM/Badge/表单辅助）。

import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { el, badge, renderEmpty, fieldRow } from './render-common.js';

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


// renderWebhooksTable 渲染 Webhook 列表表格。
// handlers: { onEdit(wh), onTest(wh), onDeliveries(wh), onDelete(wh) }
