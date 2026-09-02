// render-devices.js — 设备管理渲染（P0 补齐功能域）。

// 渲染子模块 — 设备管理（设备列表 / 退役 / 指标 / 代理列表）。
// 公共依赖：i18n（t）、icons（iconEl/iconHtml）、render-common（DOM/Badge/表单辅助）。

import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { el, formatTime, badge, renderEmpty } from './render-common.js';

// ============================================================================
// 设备管理渲染
// ============================================================================

// deviceStatusBadge 设备状态 badge。
function deviceStatusBadge(status) {
  const s = String(status || '').toLowerCase();
  if (s === 'online' || s === 'active' || s === 'up') return badge(t('devices.status.online'), 'status-resolved');
  if (s === 'offline' || s === 'down' || s === 'retired') return badge(t('devices.status.offline'), 'status-closed');
  if (s === 'maintenance' || s === 'maintaining') return badge(t('devices.status.maintenance'), 'status-in_progress');
  return badge(status || '-', 'status-in_progress');
}

// renderDevicesTable 渲染设备列表表格。
// handlers: { onMetrics(id), onRetire(id) }
export function renderDevicesTable(container, devices, handlers) {
  container.innerHTML = '';
  if (!devices || devices.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('devices.deviceName') }),
        el('th', { text: t('devices.ip') }),
        el('th', { text: t('devices.type') }),
        el('th', { text: t('common.status') }),
        el('th', { text: t('devices.agent') }),
        el('th', { text: t('common.createdAt') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      devices.map((d) => el('tr', null,
        el('td', { class: 'mono', text: d.id }),
        el('td', { class: 'cell-title', text: d.name || d.hostname || '-' }),
        el('td', { class: 'mono', text: d.ip || d.address || '-' }),
        el('td', { text: d.type || d.deviceType || '-' }),
        el('td', null, deviceStatusBadge(d.status)),
        el('td', { class: 'mono', text: d.agentID || d.agentId || '-' }),
        el('td', { class: 'mono', text: formatTime(d.createdAt || d.registeredAt) }),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn-icon', title: t('devices.viewMetrics'), onclick: () => handlers.onMetrics(d.id) }, iconEl('stats', 14)),
          el('button', { class: 'btn-icon btn-icon-danger', title: t('devices.retire'), onclick: () => handlers.onRetire(d.id) }, iconEl('trash', 14))
        )
      ))
    )
  );
  container.appendChild(table);
}

// renderDeviceMetrics 渲染设备指标。
// handlers: { onBack() }
export function renderDeviceMetrics(container, deviceID, metrics, handlers) {
  container.innerHTML = '';
  const card = el('div', { class: 'detail-card' });
  card.appendChild(el('div', { class: 'detail-head' },
    el('button', { class: 'btn btn-ghost', onclick: () => handlers.onBack() }, iconEl('back', 16), el('span', { text: t('common.back') })),
    el('h3', { class: 'detail-title', text: t('devices.metricsTitle') + ' · ' + deviceID })
  ));
  const mObj = (metrics && typeof metrics === 'object') ? metrics : {};
  const mKeys = Object.keys(mObj);
  if (mKeys.length) {
    card.appendChild(el('table', { class: 'data-table' },
      el('thead', null,
        el('tr', null,
          el('th', { text: t('devices.metricName') }),
          el('th', { text: t('devices.metricValue') })
        )
      ),
      el('tbody', null,
        mKeys.map((mk) => el('tr', null,
          el('td', { class: 'cell-title', text: mk }),
          el('td', { class: 'mono', text: String(mObj[mk]) })
        ))
      )
    ));
  } else {
    card.appendChild(el('div', { class: 'state state-empty', text: t('common.empty') }));
  }
  container.appendChild(card);
}

// renderAgentsTable 渲染代理列表表格。
// handlers: { onDetail(id) }
export function renderAgentsTable(container, agents, _handlers) {
  container.innerHTML = '';
  if (!agents || agents.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('devices.agentName') }),
        el('th', { text: t('devices.version') }),
        el('th', { text: t('common.status') }),
        el('th', { text: t('devices.lastHeartbeat') }),
        el('th', { text: t('devices.deviceCount') })
      )
    ),
    el('tbody', null,
      agents.map((a) => el('tr', null,
        el('td', { class: 'mono', text: a.id }),
        el('td', { class: 'cell-title', text: a.name || a.hostname || '-' }),
        el('td', { class: 'mono', text: a.version || '-' }),
        el('td', null, deviceStatusBadge(a.status)),
        el('td', { class: 'mono', text: formatTime(a.lastHeartbeat || a.heartbeatAt) }),
        el('td', { text: String(a.deviceCount != null ? a.deviceCount : '-') })
      ))
    )
  );
  container.appendChild(table);
}
