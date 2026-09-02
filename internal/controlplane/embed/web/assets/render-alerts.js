// render-alerts.js — 告警管理渲染（P0 补齐功能域）。

// 渲染子模块 — 告警管理（活跃告警列表 / 确认 / 静默）。
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
// 告警管理渲染
// ============================================================================

// alertSeverityBadge 告警严重级别 badge。
export function alertSeverityBadge(severity) {
  const s = String(severity || '').toLowerCase();
  if (s === 'critical' || s === 'urgent') return badge(t('alerts.severity.critical'), 'priority-urgent');
  if (s === 'high') return badge(t('alerts.severity.high'), 'priority-high');
  if (s === 'medium') return badge(t('alerts.severity.medium'), 'priority-medium');
  if (s === 'low' || s === 'info') return badge(t('alerts.severity.low'), 'priority-low');
  return badge(severity || '-', 'priority-low');
}

// alertStateBadge 告警状态 badge。
export function alertStateBadge(state) {
  const s = String(state || '').toLowerCase();
  if (s === 'firing' || s === 'active') return badge(t('alerts.state.firing'), 'priority-urgent');
  if (s === 'acked' || s === 'acknowledged') return badge(t('alerts.state.acked'), 'status-in_progress');
  if (s === 'silenced' || s === 'silent') return badge(t('alerts.state.silenced'), 'status-closed');
  if (s === 'resolved' || s === 'closed') return badge(t('alerts.state.resolved'), 'status-resolved');
  return badge(state || '-', 'status-in_progress');
}

// renderAlertsTable 渲染活跃告警列表表格。
// handlers: { onAck(id), onSilence(id) }
export function renderAlertsTable(container, alerts, handlers) {
  container.innerHTML = '';
  if (!alerts || alerts.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('alerts.alertName') }),
        el('th', { text: t('alerts.severity') }),
        el('th', { text: t('common.status') }),
        el('th', { text: t('alerts.source') }),
        el('th', { text: t('alerts.firedAt') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      alerts.map((a) => {
        const st = String(a.status || a.state || '').toLowerCase();
        const canAck = st === 'firing' || st === 'active';
        const canSilence = st === 'firing' || st === 'active' || st === 'acked' || st === 'acknowledged';
        return el('tr', null,
          el('td', { class: 'mono', text: a.id }),
          el('td', { class: 'cell-title', text: a.name || a.title || a.alertName || '-' }),
          el('td', null, alertSeverityBadge(a.severity)),
          el('td', null, alertStateBadge(a.status || a.state)),
          el('td', { class: 'mono', text: a.source || a.service || '-' }),
          el('td', { class: 'mono', text: formatTime(a.firedAt || a.startsAt || a.createdAt) }),
          el('td', { class: 'td-actions' },
            canAck
              ? el('button', { class: 'btn-icon', title: t('alerts.ack'), onclick: () => handlers.onAck(a.id) }, iconEl('check', 14))
              : null,
            canSilence
              ? el('button', { class: 'btn-icon', title: t('alerts.silence'), onclick: () => handlers.onSilence(a.id) }, iconEl('pause', 14))
              : null
          )
        );
      })
    )
  );
  container.appendChild(table);
}

// renderAlertSilenceForm 渲染告警静默表单。
// handlers: { onSubmit(data), onCancel() }
export function renderAlertSilenceForm(container, alertID, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const data = {
      duration: form.elements.duration.value.trim(),
      reason: form.elements.reason.value.trim(),
    };
    handlers.onSubmit(data);
  } });
  form.appendChild(el('h3', { class: 'form-title', text: t('alerts.silenceTitle') + ' · ' + alertID }));
  // 静默时长（必填，支持 "30m"/"2h"/"1d"）
  form.appendChild(fieldRow(t('alerts.silenceDuration'), true,
    el('input', { type: 'text', name: 'duration', required: 'true', value: '1h', placeholder: '30m / 2h / 1d' })
  ));
  // 静默原因
  form.appendChild(fieldRow(t('alerts.silenceReason'), false,
    el('textarea', { name: 'reason', rows: '3', placeholder: t('alerts.silenceReasonPlaceholder') }, '')
  ));
  // 操作按钮
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('check', 16), el('span', { text: t('common.confirm') })),
    el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => handlers.onCancel() }, el('span', { text: t('common.cancel') }))
  ));
  container.appendChild(form);
}

// renderAlertDetail 渲染告警详情。
// handlers: { onBack(), onAck(), onSilence() }
export function renderAlertDetail(container, alert, handlers) {
  container.innerHTML = '';
  if (!alert) { renderEmpty(container); return; }
  const card = el('div', { class: 'detail-card' });
  card.appendChild(el('div', { class: 'detail-head' },
    el('button', { class: 'btn btn-ghost', onclick: () => handlers.onBack() }, iconEl('back', 16), el('span', { text: t('common.back') })),
    el('h3', { class: 'detail-title', text: alert.name || alert.title || alert.id }),
    el('div', { class: 'detail-actions' },
      el('button', { class: 'btn btn-secondary', onclick: () => handlers.onAck() }, iconEl('check', 14), el('span', { text: t('alerts.ack') })),
      el('button', { class: 'btn btn-secondary', onclick: () => handlers.onSilence() }, iconEl('pause', 14), el('span', { text: t('alerts.silence') }))
    )
  ));
  card.appendChild(el('div', { class: 'detail-grid' },
    detailItem(t('common.id'), alert.id, true),
    detailItem(t('alerts.severity'), alertSeverityBadge(alert.severity)),
    detailItem(t('common.status'), alertStateBadge(alert.status || alert.state)),
    detailItem(t('alerts.source'), alert.source || alert.service || '-'),
    detailItem(t('alerts.firedAt'), formatTime(alert.firedAt || alert.startsAt || alert.createdAt), true),
    detailItem(t('common.updatedAt'), formatTime(alert.updatedAt || alert.endsAt), true)
  ));
  if (alert.description || alert.message) {
    card.appendChild(el('div', { class: 'detail-section' },
      el('h4', { text: t('common.description') }),
      el('p', { class: 'detail-desc', text: alert.description || alert.message })
    ));
  }
  if (alert.labels && typeof alert.labels === 'object') {
    const lKeys = Object.keys(alert.labels);
    if (lKeys.length) {
      card.appendChild(el('div', { class: 'detail-section' },
        el('h4', { text: t('alerts.labels') }),
        el('div', { class: 'tag-list' },
          lKeys.map((k) => el('span', { class: 'tag' }, el('span', { text: k + '=' + String(alert.labels[k]) })))
        )
      ));
    }
  }
  container.appendChild(card);
}