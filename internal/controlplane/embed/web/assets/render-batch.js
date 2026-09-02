// render-batch.js — 批量执行渲染（P0 补齐功能域）。

// 渲染子模块 — 批量执行（批量任务下发 / 状态查询）。
// 公共依赖：i18n（t）、icons（iconEl/iconHtml）、render-common（DOM/Badge/表单辅助）。

import { t } from './i18n.js';
import { iconEl, iconHtml } from './icons.js';
import {
  el, formatTime, formatNumber, badge,
  renderLoading, renderError, renderEmpty, renderToast,
  statusBadge, priorityBadge, categoryBadge, sloStatusBadge,
  detailItem, fieldRow,
} from './render-common.js';
import { alertStateBadge } from './render-alerts.js';

// ============================================================================
// 批量执行渲染
// ============================================================================

// batchStatusBadge 批量状态 badge。
function batchStatusBadge(status) {
  const s = String(status || '').toLowerCase();
  if (s === 'running' || s === 'in_progress') return badge(t('batch.status.running'), 'status-in_progress');
  if (s === 'succeeded' || s === 'success' || s === 'completed') return badge(t('batch.status.succeeded'), 'status-resolved');
  if (s === 'failed' || s === 'error') return badge(t('batch.status.failed'), 'priority-urgent');
  if (s === 'partial' || s === 'partial_failed') return badge(t('batch.status.partial'), 'priority-high');
  if (s === 'pending' || s === 'queued') return badge(t('batch.status.pending'), 'priority-medium');
  return badge(status || '-', 'status-in_progress');
}

// renderBatchExecForm 渲染批量执行表单（直接下发执行）。
// handlers: { onSubmit(data), onCancel() }
export function renderBatchExecForm(container, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => { e.preventDefault(); handlers.onSubmit(collectBatchForm(form)); } });
  form.appendChild(el('h3', { class: 'form-title', text: t('batch.execTitle') }));
  // 任务类型
  form.appendChild(fieldRow(t('tasks.taskType'), true,
    el('select', { name: 'type' },
      ['exec', 'script', 'config', 'inspect', 'restart'].map((tp) =>
        el('option', { value: tp, text: t('tasks.type.' + tp) })
      )
    )
  ));
  // 目标设备列表（逗号分隔，必填）
  form.appendChild(fieldRow(t('batch.devices'), true,
    el('textarea', { name: 'devices', rows: '3', required: 'true', placeholder: 'device-1, device-2, device-3' }, '')
  ));
  // 负载（JSON）
  form.appendChild(fieldRow(t('tasks.payload'), false,
    el('textarea', { name: 'payload', rows: '4', placeholder: '{"key":"value"}' }, '')
  ));
  // 并发数
  form.appendChild(fieldRow(t('batch.concurrency'), false,
    el('input', { type: 'number', name: 'concurrency', value: '5', min: '1', max: '100' })
  ));
  // 失败阈值（百分比）
  form.appendChild(fieldRow(t('batch.failThreshold'), false,
    el('input', { type: 'number', name: 'failThreshold', value: '0', min: '0', max: '100', placeholder: '0-100' })
  ));
  // 操作按钮
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('rocket', 16), el('span', { text: t('batch.exec') })),
    el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => handlers.onCancel() }, el('span', { text: t('common.cancel') }))
  ));
  container.appendChild(form);
}

// renderBatchCreateForm 渲染批量任务创建表单（创建但不立即执行）。
// handlers: { onSubmit(data), onCancel() }
export function renderBatchCreateForm(container, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => { e.preventDefault(); handlers.onSubmit(collectBatchForm(form)); } });
  form.appendChild(el('h3', { class: 'form-title', text: t('batch.createTitle') }));
  // 批次名称（必填）
  form.appendChild(fieldRow(t('batch.batchName'), true,
    el('input', { type: 'text', name: 'name', required: 'true', placeholder: t('batch.nameRequired') })
  ));
  // 任务类型
  form.appendChild(fieldRow(t('tasks.taskType'), true,
    el('select', { name: 'type' },
      ['exec', 'script', 'config', 'inspect', 'restart'].map((tp) =>
        el('option', { value: tp, text: t('tasks.type.' + tp) })
      )
    )
  ));
  // 目标设备列表（逗号分隔，必填）
  form.appendChild(fieldRow(t('batch.devices'), true,
    el('textarea', { name: 'devices', rows: '3', required: 'true', placeholder: 'device-1, device-2, device-3' }, '')
  ));
  // 负载（JSON）
  form.appendChild(fieldRow(t('tasks.payload'), false,
    el('textarea', { name: 'payload', rows: '4', placeholder: '{"key":"value"}' }, '')
  ));
  // 并发数
  form.appendChild(fieldRow(t('batch.concurrency'), false,
    el('input', { type: 'number', name: 'concurrency', value: '5', min: '1', max: '100' })
  ));
  // 操作按钮
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('check', 16), el('span', { text: t('common.save') })),
    el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => handlers.onCancel() }, el('span', { text: t('common.cancel') }))
  ));
  container.appendChild(form);
}

// collectBatchForm 从表单收集数据。
function collectBatchForm(form) {
  const get = (name) => (form.elements[name] && form.elements[name].value) || '';
  const devices = get('devices').split(/[,\n]/).map((s) => s.trim()).filter(Boolean);
  const rawPayload = get('payload').trim();
  let payload = null;
  if (rawPayload) {
    try { payload = JSON.parse(rawPayload); } catch (_) { payload = rawPayload; }
  }
  const data = {
    type: get('type'),
    devices: devices,
    payload: payload,
    concurrency: parseInt(get('concurrency'), 10) || 5,
  };
  const name = get('name');
  if (name) data.name = name;
  const ft = parseInt(get('failThreshold'), 10);
  if (!isNaN(ft)) data.failThreshold = ft;
  return data;
}

// renderBatchStatus 渲染批量任务状态详情。
// handlers: { onBack(), onRefresh() }
export function renderBatchStatus(container, batchID, batch, handlers) {
  container.innerHTML = '';
  const card = el('div', { class: 'detail-card' });
  card.appendChild(el('div', { class: 'detail-head' },
    el('button', { class: 'btn btn-ghost', onclick: () => handlers.onBack() }, iconEl('back', 16), el('span', { text: t('common.back') })),
    el('h3', { class: 'detail-title', text: t('batch.statusTitle') + ' · ' + batchID }),
    el('div', { class: 'detail-actions' },
      el('button', { class: 'btn btn-secondary', onclick: () => handlers.onRefresh() }, iconEl('refresh', 14), el('span', { text: t('common.refresh') }))
    )
  ));
  if (!batch) { card.appendChild(el('div', { class: 'state state-empty', text: t('common.empty') })); container.appendChild(card); return; }
  // 概要信息
  const total = batch.total != null ? batch.total : (batch.devices ? batch.devices.length : 0);
  const succeeded = batch.succeeded != null ? batch.succeeded : 0;
  const failed = batch.failed != null ? batch.failed : 0;
  const pending = batch.pending != null ? batch.pending : (total - succeeded - failed);
  card.appendChild(el('div', { class: 'detail-grid' },
    detailItem(t('common.id'), batch.id || batchID, true),
    detailItem(t('batch.batchName'), batch.name || '-', true),
    detailItem(t('common.status'), batchStatusBadge(batch.status)),
    detailItem(t('batch.total'), String(total)),
    detailItem(t('batch.succeeded'), String(succeeded)),
    detailItem(t('batch.failed'), String(failed)),
    detailItem(t('batch.pending'), String(pending)),
    detailItem(t('common.createdAt'), formatTime(batch.createdAt), true),
    detailItem(t('common.updatedAt'), formatTime(batch.updatedAt || batch.finishedAt), true)
  ));
  // 设备级执行明细
  const items = batch.items || batch.results || batch.executions;
  if (items && items.length) {
    card.appendChild(el('div', { class: 'detail-section' },
      el('h4', { text: t('batch.execDetail') }),
      el('table', { class: 'data-table data-table-compact' },
        el('thead', null,
          el('tr', null,
            el('th', { text: t('common.device') }),
            el('th', { text: t('common.status') }),
            el('th', { text: t('batch.execMessage') })
          )
        ),
        el('tbody', null,
          items.map((it) => el('tr', null,
            el('td', { class: 'mono', text: it.deviceID || it.deviceId || it.device || '-' }),
            el('td', null, batchStatusBadge(it.status)),
            el('td', { text: it.message || it.error || '-' })
          ))
        )
      )
    ));
  }
  container.appendChild(card);
}

// renderBatchList 渲染批量任务列表（最近批次）。
// handlers: { onDetail(id) }
export function renderBatchList(container, batches, handlers) {
  container.innerHTML = '';
  if (!batches || batches.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('batch.batchName') }),
        el('th', { text: t('common.status') }),
        el('th', { text: t('batch.total') }),
        el('th', { text: t('batch.succeeded') }),
        el('th', { text: t('batch.failed') }),
        el('th', { text: t('common.createdAt') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      batches.map((b) => el('tr', null,
        el('td', { class: 'mono', text: b.id }),
        el('td', { class: 'cell-title', text: b.name || '-' }),
        el('td', null, batchStatusBadge(b.status)),
        el('td', { text: String(b.total != null ? b.total : '-') }),
        el('td', { text: String(b.succeeded != null ? b.succeeded : '-') }),
        el('td', { text: String(b.failed != null ? b.failed : '-') }),
        el('td', { class: 'mono', text: formatTime(b.createdAt) }),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn-icon', title: t('batch.viewStatus'), onclick: () => handlers.onDetail(b.id) }, iconEl('stats', 14))
        )
      ))
    )
  );
  container.appendChild(table);
}