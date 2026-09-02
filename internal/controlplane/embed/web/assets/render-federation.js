// render-federation.js — 控制面联邦渲染（P2 补齐功能域）。

// 渲染子模块 — 控制面联邦（Peer 管理 + 设备聚合视图 + 任务转发）。
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
// 控制面联邦渲染
// ============================================================================

// peerOnlineBadge peer 在线状态 badge。
function peerOnlineBadge(online) {
  if (online) return badge(t('federation.online'), 'status-resolved');
  return badge(t('federation.offline'), 'status-closed');
}

// renderFederationPeersTable 渲染 Peer 列表表格。
// peers: [{url, online, lastCheckAt, latencyMs}]
export function renderFederationPeersTable(container, peers) {
  container.innerHTML = '';
  if (!peers || peers.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('federation.peerURL') }),
        el('th', { text: t('common.status') }),
        el('th', { text: t('federation.lastCheckAt') }),
        el('th', { text: t('federation.latencyMs') })
      )
    ),
    el('tbody', null,
      peers.map((p) => el('tr', null,
        el('td', { class: 'mono', text: p.url || '-' }),
        el('td', null, peerOnlineBadge(p.online)),
        el('td', { class: 'mono', text: formatTime(p.lastCheckAt) }),
        el('td', { class: 'mono', text: (p.latencyMs != null ? String(p.latencyMs) : '-') })
      ))
    )
  );
  container.appendChild(table);
}

// renderFederationDevicesTable 渲染跨 peer 设备聚合视图表格。
// devices: [Device]，peerSummary: [{url, online, deviceCount}]
export function renderFederationDevicesTable(container, devices, peerSummary) {
  container.innerHTML = '';
  // peer 概览
  const summary = Array.isArray(peerSummary) ? peerSummary : [];
  if (summary.length > 0) {
    const summaryCard = el('div', { class: 'detail-card' });
    summaryCard.appendChild(el('h3', { class: 'detail-title', text: t('federation.peerSummary') }));
    summaryCard.appendChild(el('table', { class: 'data-table' },
      el('thead', null,
        el('tr', null,
          el('th', { text: t('federation.peerURL') }),
          el('th', { text: t('common.status') }),
          el('th', { text: t('federation.deviceCount') })
        )
      ),
      el('tbody', null,
        summary.map((s) => el('tr', null,
          el('td', { class: 'mono', text: s.url || '-' }),
          el('td', null, peerOnlineBadge(s.online)),
          el('td', { text: String(s.deviceCount != null ? s.deviceCount : '-') })
        ))
      )
    ));
    container.appendChild(summaryCard);
  }
  // 设备列表
  const list = Array.isArray(devices) ? devices : [];
  if (list.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('federation.deviceName') }),
        el('th', { text: t('federation.deviceIP') }),
        el('th', { text: t('common.status') }),
        el('th', { text: t('federation.sourcePeer') })
      )
    ),
    el('tbody', null,
      list.map((d) => el('tr', null,
        el('td', { class: 'mono', text: d.id || '-' }),
        el('td', { class: 'cell-title', text: d.name || d.hostname || '-' }),
        el('td', { class: 'mono', text: d.ip || d.address || '-' }),
        el('td', null, statusBadge(d.status)),
        el('td', { class: 'mono', text: d.peerURL || d.sourcePeer || '-' })
      ))
    )
  );
  container.appendChild(table);
}

// renderFederationForwardForm 渲染任务转发表单。
// peers: [{url, online, ...}] 用于选择目标 peer
// handlers: { onSubmit(data), onCancel() }
export function renderFederationForwardForm(container, peers, handlers) {
  container.innerHTML = '';
  const peerList = Array.isArray(peers) ? peers : [];
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const data = {
      peerURL: form.elements.peerURL.value,
      taskType: form.elements.taskType.value,
      command: form.elements.command.value,
      deviceID: form.elements.deviceID.value.trim(),
      timeoutSec: parseInt(form.elements.timeoutSec.value, 10) || 0,
    };
    handlers.onSubmit(data);
  } });
  form.appendChild(el('h3', { class: 'form-title', text: t('federation.forwardTitle') }));
  // 选择 peer（必填）
  form.appendChild(fieldRow(t('federation.peerURL'), true,
    el('select', { name: 'peerURL' },
      peerList.length === 0
        ? [el('option', { value: '', text: t('common.empty') })]
        : peerList.map((p) =>
            el('option', { value: p.url, text: p.url || '-' })
          )
    )
  ));
  // 任务类型
  form.appendChild(fieldRow(t('federation.taskType'), true,
    el('select', { name: 'taskType' },
      ['exec', 'config', 'restart', 'inspect'].map((tp) =>
        el('option', { value: tp, text: tp })
      )
    )
  ));
  // 命令
  form.appendChild(fieldRow(t('federation.command'), false,
    el('input', { type: 'text', name: 'command', placeholder: t('federation.commandPlaceholder') })
  ));
  // 设备 ID
  form.appendChild(fieldRow(t('federation.deviceID'), false,
    el('input', { type: 'text', name: 'deviceID', placeholder: t('federation.deviceIDPlaceholder') })
  ));
  // 超时（秒）
  form.appendChild(fieldRow(t('federation.timeoutSec'), false,
    el('input', { type: 'number', name: 'timeoutSec', min: '0', value: '30', placeholder: '30' })
  ));
  // 操作按钮
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('send', 16), el('span', { text: t('federation.forward') })),
    el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => handlers.onCancel() }, el('span', { text: t('common.cancel') }))
  ));
  container.appendChild(form);
}

// renderFederationForwardResult 渲染任务转发结果。
// result: {taskID, peerURL, status}
export function renderFederationForwardResult(container, result) {
  container.innerHTML = '';
  if (!result) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const card = el('div', { class: 'detail-card' });
  card.appendChild(el('h3', { class: 'detail-title', text: t('federation.forwardResultTitle') }));
  card.appendChild(el('div', { class: 'detail-grid' },
    el('div', { class: 'detail-item' },
      el('div', { class: 'detail-label', text: t('federation.taskID') }),
      el('div', { class: 'detail-value mono', text: String(result.taskID || '-') })
    ),
    el('div', { class: 'detail-item' },
      el('div', { class: 'detail-label', text: t('federation.peerURL') }),
      el('div', { class: 'detail-value mono', text: String(result.peerURL || '-') })
    ),
    el('div', { class: 'detail-item' },
      el('div', { class: 'detail-label', text: t('common.status') }),
      el('div', { class: 'detail-value' }, statusBadge(result.status))
    )
  ));
  container.appendChild(card);
}