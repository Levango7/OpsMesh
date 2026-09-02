// render-deploys.js — 部署中心渲染（P1 补齐功能域）。

// 渲染子模块 — 部署中心（部署列表 / 创建 / 回滚 / 联邦部署列表）。
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
// 部署中心渲染
// ============================================================================

// deployStatusBadge 部署状态 badge。
function deployStatusBadge(status) {
  const s = String(status || '').toLowerCase();
  if (s === 'succeeded' || s === 'success' || s === 'completed') return badge(t('deploys.status.succeeded'), 'status-resolved');
  if (s === 'failed' || s === 'error') return badge(t('deploys.status.failed'), 'priority-urgent');
  if (s === 'running' || s === 'in_progress') return badge(t('deploys.status.running'), 'status-in_progress');
  if (s === 'pending' || s === 'queued') return badge(t('deploys.status.pending'), 'priority-medium');
  if (s === 'rolled_back' || s === 'rollback') return badge(t('deploys.status.rolledBack'), 'status-closed');
  return badge(status || '-', 'status-in_progress');
}

// renderDeploysTable 渲染部署列表表格。
// handlers: { onRollback(id) }
export function renderDeploysTable(container, deploys, handlers) {
  container.innerHTML = '';
  if (!deploys || deploys.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('deploys.deployName') }),
        el('th', { text: t('deploys.template') }),
        el('th', { text: t('common.status') }),
        el('th', { text: t('common.createdAt') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      deploys.map((d) => {
        const canRollback = ['succeeded', 'success', 'completed', 'failed', 'error'].indexOf(String(d.status || '').toLowerCase()) !== -1;
        return el('tr', null,
          el('td', { class: 'mono', text: d.id }),
          el('td', { class: 'cell-title', text: d.name || '-' }),
          el('td', { text: d.template || d.templateName || '-' }),
          el('td', null, deployStatusBadge(d.status)),
          el('td', { class: 'mono', text: formatTime(d.createdAt) }),
          el('td', { class: 'td-actions' },
            canRollback
              ? el('button', { class: 'btn-icon btn-icon-danger', title: t('deploys.rollback'), onclick: () => handlers.onRollback(d.id) }, iconEl('back', 14))
              : null
          )
        );
      })
    )
  );
  container.appendChild(table);
}

// renderDeployForm 渲染创建部署表单。
// handlers: { onSubmit(data), onCancel() }
export function renderDeployForm(container, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const data = {
      name: form.elements.name.value.trim(),
      template: form.elements.template.value.trim(),
      params: parseParams(form.elements.params.value),
      target: form.elements.target.value.trim(),
    };
    handlers.onSubmit(data);
  } });
  form.appendChild(el('h3', { class: 'form-title', text: t('deploys.create') }));
  // 部署名称（必填）
  form.appendChild(fieldRow(t('deploys.deployName'), true,
    el('input', { type: 'text', name: 'name', required: 'true', placeholder: t('deploys.namePlaceholder') })
  ));
  // 模板（必填）
  form.appendChild(fieldRow(t('deploys.template'), true,
    el('input', { type: 'text', name: 'template', required: 'true', placeholder: t('deploys.templatePlaceholder') })
  ));
  // 参数（JSON）
  form.appendChild(fieldRow(t('deploys.params'), false,
    el('textarea', { name: 'params', rows: '4', placeholder: '{"key":"value"}' }, '')
  ));
  // 目标（必填）
  form.appendChild(fieldRow(t('deploys.target'), true,
    el('input', { type: 'text', name: 'target', required: 'true', placeholder: t('deploys.targetPlaceholder') })
  ));
  // 操作按钮
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('check', 16), el('span', { text: t('common.save') })),
    el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => handlers.onCancel() }, el('span', { text: t('common.cancel') }))
  ));
  container.appendChild(form);
}

// parseParams 解析参数 JSON。
function parseParams(text) {
  const raw = (text || '').trim();
  if (!raw) return null;
  try { return JSON.parse(raw); } catch (_) { return raw; }
}

// renderFederationDeploysTable 渲染联邦部署列表表格。
export function renderFederationDeploysTable(container, deploys) {
  container.innerHTML = '';
  if (!deploys || deploys.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('deploys.deployName') }),
        el('th', { text: t('deploys.template') }),
        el('th', { text: t('common.status') }),
        el('th', { text: t('deploys.cluster') }),
        el('th', { text: t('common.createdAt') })
      )
    ),
    el('tbody', null,
      deploys.map((d) => el('tr', null,
        el('td', { class: 'mono', text: d.id }),
        el('td', { class: 'cell-title', text: d.name || '-' }),
        el('td', { text: d.template || d.templateName || '-' }),
        el('td', null, deployStatusBadge(d.status)),
        el('td', { class: 'mono', text: d.cluster || d.clusterID || d.clusterId || '-' }),
        el('td', { class: 'mono', text: formatTime(d.createdAt) })
      ))
    )
  );
  container.appendChild(table);
}