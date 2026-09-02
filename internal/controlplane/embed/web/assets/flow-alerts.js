// flow-alerts.js — 告警管理编排（P0 补齐功能域）。

// flow 子模块 — 告警管理（活跃告警列表 / 确认 / 静默）。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, $ } from './flow-state.js';

// ============================================================================
// 告警管理
// ============================================================================

function alertsContent() { return $('alerts-content'); }

// loadAlerts 加载活跃告警列表（带当前过滤器）。
export async function loadAlerts(filter) {
  if (filter) state.alertFilter = Object.assign(state.alertFilter || {}, filter);
  const content = alertsContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const alerts = await api.getAlerts(state.alertFilter);
    state.alerts = alerts;
    render.renderAlertsTable(content, alerts, {
      onAck: (id) => ackAlert(id),
      onSilence: (id) => silenceAlert(id),
    });
  } catch (err) {
    render.renderError(content, t('alerts.loadFailed') + ': ' + err.message);
  }
}

// ackAlert 确认告警。
export async function ackAlert(id) {
  try {
    await api.ackAlert(id);
    render.renderToast(t('alerts.acked'), 'success');
    loadAlerts();
  } catch (err) {
    render.renderToast(t('alerts.ackFailed') + ': ' + err.message, 'error');
  }
}

// silenceAlert 打开静默告警表单。
export function silenceAlert(id) {
  const content = alertsContent();
  if (!content) return;
  render.renderAlertSilenceForm(content, id, {
    onSubmit: async (data) => {
      if (!data.duration) {
        render.renderToast(t('alerts.silenceDurationRequired'), 'warn');
        return;
      }
      try {
        await api.silenceAlert(id, data);
        render.renderToast(t('alerts.silenced'), 'success');
        loadAlerts();
      } catch (err) {
        render.renderToast(t('alerts.silenceFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => loadAlerts(),
  });
}

// buildAlertsToolbar 构建告警工具栏（过滤器 + 刷新）。
export function buildAlertsToolbar() {
  const toolbar = $('alerts-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  // 过滤器组
  const filterGroup = render.el('div', { class: 'filter-group' });
  // 严重级别过滤
  filterGroup.appendChild(buildSeverityFilter());
  // 状态过滤
  filterGroup.appendChild(buildStateFilter());
  // 刷新
  filterGroup.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => loadAlerts() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
  toolbar.appendChild(filterGroup);
}

// buildSeverityFilter 构建严重级别过滤下拉框。
function buildSeverityFilter() {
  const options = ['critical', 'high', 'medium', 'low'];
  return render.el('select', { class: 'filter-select', onchange: (e) => loadAlerts({ severity: e.target.value }) },
    render.el('option', { value: '', text: t('alerts.filter.severity') + ': ' + t('common.all') }),
    options.map((opt) => render.el('option', { value: opt, text: t('alerts.filter.severity') + ': ' + t('alerts.severity.' + opt) }))
  );
}

// buildStateFilter 构建状态过滤下拉框。
function buildStateFilter() {
  const options = ['firing', 'acked', 'silenced', 'resolved'];
  return render.el('select', { class: 'filter-select', onchange: (e) => loadAlerts({ state: e.target.value }) },
    render.el('option', { value: '', text: t('alerts.filter.state') + ': ' + t('common.all') }),
    options.map((opt) => render.el('option', { value: opt, text: t('alerts.filter.state') + ': ' + t('alerts.state.' + opt) }))
  );
}
