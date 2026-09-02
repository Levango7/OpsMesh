// render-provision.js — 自动纳管渲染（P2 补齐功能域）。

// 渲染子模块 — 自动纳管（网段输入 + agent 版本选择 + 结果统计 + 设备列表）。
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
// 自动纳管渲染
// ============================================================================

// provisionStatusBadge 纳管状态 badge。
function provisionStatusBadge(status) {
  const s = String(status || '').toLowerCase();
  if (s === 'provisioned' || s === 'success' || s === 'ok') return badge(t('provision.status.provisioned'), 'status-resolved');
  if (s === 'failed' || s === 'error') return badge(t('provision.status.failed'), 'status-closed');
  if (s === 'discovered' || s === 'pending') return badge(t('provision.status.discovered'), 'status-in_progress');
  return badge(status || '-', 'status-in_progress');
}

// renderProvisionForm 渲染自动纳管触发表单。
// handlers: { onSubmit(data), onCancel() }
export function renderProvisionForm(container, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const data = {
      segment: form.elements.segment.value.trim(),
      agentVersion: form.elements.agentVersion.value.trim(),
    };
    handlers.onSubmit(data);
  } });
  form.appendChild(el('h3', { class: 'form-title', text: t('provision.title') }));
  // 网段（必填）
  form.appendChild(fieldRow(t('provision.segment'), true,
    el('input', { type: 'text', name: 'segment', required: 'true', placeholder: t('provision.segmentPlaceholder') })
  ));
  // agent 版本
  form.appendChild(fieldRow(t('provision.agentVersion'), false,
    el('input', { type: 'text', name: 'agentVersion', placeholder: t('provision.agentVersionPlaceholder') })
  ));
  // 操作按钮
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('rocket', 16), el('span', { text: t('provision.start') })),
    el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => handlers.onCancel() }, el('span', { text: t('common.cancel') }))
  ));
  container.appendChild(form);
}

// renderProvisionResult 渲染自动纳管结果（统计 + 设备列表）。
// result: {discovered, provisioned, failed, devices: [{ip, hostname, status}]}
export function renderProvisionResult(container, result) {
  container.innerHTML = '';
  if (!result) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const card = el('div', { class: 'detail-card' });
  // 统计概览
  card.appendChild(el('h3', { class: 'detail-title', text: t('provision.resultTitle') }));
  card.appendChild(el('div', { class: 'overview-grid' },
    el('div', { class: 'metric-card metric-devices' },
      el('div', { class: 'metric-icon' }, iconEl('discover', 22)),
      el('div', { class: 'metric-body' },
        el('div', { class: 'metric-value', text: String(formatNumber(result.discovered || 0)) }),
        el('div', { class: 'metric-label', text: t('provision.discovered') })
      )
    ),
    el('div', { class: 'metric-card metric-tasks' },
      el('div', { class: 'metric-icon' }, iconEl('check', 22)),
      el('div', { class: 'metric-body' },
        el('div', { class: 'metric-value', text: String(formatNumber(result.provisioned || 0)) }),
        el('div', { class: 'metric-label', text: t('provision.provisioned') })
      )
    ),
    el('div', { class: 'metric-card metric-alerts' },
      el('div', { class: 'metric-icon' }, iconEl('alert', 22)),
      el('div', { class: 'metric-body' },
        el('div', { class: 'metric-value', text: String(formatNumber(result.failed || 0)) }),
        el('div', { class: 'metric-label', text: t('provision.failed') })
      )
    )
  ));
  // 设备列表
  const devices = Array.isArray(result.devices) ? result.devices : [];
  if (devices.length === 0) {
    card.appendChild(el('div', { class: 'state state-empty', text: t('common.empty') }));
  } else {
    card.appendChild(el('table', { class: 'data-table' },
      el('thead', null,
        el('tr', null,
          el('th', { text: t('provision.deviceIP') }),
          el('th', { text: t('provision.hostname') }),
          el('th', { text: t('common.status') })
        )
      ),
      el('tbody', null,
        devices.map((d) => el('tr', null,
          el('td', { class: 'mono', text: d.ip || '-' }),
          el('td', { class: 'cell-title', text: d.hostname || '-' }),
          el('td', null, provisionStatusBadge(d.status))
        ))
      )
    ));
  }
  container.appendChild(card);
}