// render-cmdb.js — CMDB 渲染（P1 补齐功能域）。

// 渲染子模块 — CMDB（CI 类型列表 / CI 项列表 / 触发采集 / 变更申请列表）。
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
// CMDB 渲染
// ============================================================================

// renderCMDBTypesTable 渲染 CI 类型列表表格。
// handlers: { onSelect(type) }
export function renderCMDBTypesTable(container, types, handlers) {
  container.innerHTML = '';
  if (!types || types.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('cmdb.typeName') }),
        el('th', { text: t('cmdb.typeCode') }),
        el('th', { text: t('cmdb.itemCount') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      types.map((tp) => el('tr', null,
        el('td', { class: 'cell-title', text: tp.name || '-' }),
        el('td', { class: 'mono', text: tp.code || tp.id || '-' }),
        el('td', { text: String(tp.itemCount != null ? tp.itemCount : '-') }),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn-icon', title: t('cmdb.viewItems'), onclick: () => handlers.onSelect(tp.code || tp.id || tp.name) }, iconEl('search', 14))
        )
      ))
    )
  );
  container.appendChild(table);
}

// renderCMDBItemsTable 渲染 CI 项列表表格。
export function renderCMDBItemsTable(container, items, type) {
  container.innerHTML = '';
  if (type) {
    container.appendChild(el('div', { class: 'list-meta', text: t('cmdb.typeName') + ': ' + type }));
  }
  if (!items || items.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('cmdb.itemName') }),
        el('th', { text: t('cmdb.typeName') }),
        el('th', { text: t('common.status') }),
        el('th', { text: t('common.updatedAt') })
      )
    ),
    el('tbody', null,
      items.map((it) => el('tr', null,
        el('td', { class: 'mono', text: it.id }),
        el('td', { class: 'cell-title', text: it.name || '-' }),
        el('td', { text: it.type || it.typeCode || '-' }),
        el('td', null, statusBadge(it.status || 'active')),
        el('td', { class: 'mono', text: formatTime(it.updatedAt || it.createdAt) })
      ))
    )
  );
  container.appendChild(table);
}

// renderCMDBCollectResult 渲染采集结果。
// handlers: { onBack() }
export function renderCMDBCollectResult(container, result, handlers) {
  container.innerHTML = '';
  const card = el('div', { class: 'detail-card' });
  card.appendChild(el('div', { class: 'detail-head' },
    el('button', { class: 'btn btn-ghost', onclick: () => handlers.onBack() }, iconEl('back', 16), el('span', { text: t('common.back') })),
    el('h3', { class: 'detail-title', text: t('cmdb.collectTitle') })
  ));
  const rObj = (result && typeof result === 'object') ? result : {};
  card.appendChild(el('div', { class: 'detail-grid' },
    detailItem(t('cmdb.collected'), String(rObj.collected != null ? rObj.collected : '-'), true),
    detailItem(t('cmdb.failed'), String(rObj.failed != null ? rObj.failed : '-'), true)
  ));
  container.appendChild(card);
}

// renderCMDBChangesTable 渲染变更申请列表表格。
export function renderCMDBChangesTable(container, changes) {
  container.innerHTML = '';
  if (!changes || changes.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('cmdb.changeTitle') }),
        el('th', { text: t('common.status') }),
        el('th', { text: t('cmdb.changeType') }),
        el('th', { text: t('common.createdAt') })
      )
    ),
    el('tbody', null,
      changes.map((c) => el('tr', null,
        el('td', { class: 'mono', text: c.id }),
        el('td', { class: 'cell-title', text: c.title || c.name || '-' }),
        el('td', null, statusBadge(c.status || 'pending')),
        el('td', { text: c.type || c.changeType || '-' }),
        el('td', { class: 'mono', text: formatTime(c.createdAt) })
      ))
    )
  );
  container.appendChild(table);
}