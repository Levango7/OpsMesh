// render-secrets.js — 密钥管理渲染（P3 补齐功能域）。

// 渲染子模块 — 密钥管理（后端状态展示 + 测试按钮 + 密钥列表）。
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
// 密钥管理渲染
// ============================================================================

// secretsSealedBadge 密钥后端 sealed 状态 badge。
function secretsSealedBadge(sealed) {
  if (sealed) return badge(t('common.sealed'), 'status-closed');
  return badge(t('common.unsealed'), 'status-resolved');
}

// renderSecretsStatus 渲染密钥后端状态概览。
// status: {backend, sealed, keys}
export function renderSecretsStatus(container, status) {
  container.innerHTML = '';
  if (!status) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const card = el('div', { class: 'detail-card' });
  card.appendChild(el('h3', { class: 'detail-title', text: t('secrets.statusTitle') }));
  card.appendChild(el('div', { class: 'overview-grid' },
    el('div', { class: 'metric-card metric-devices' },
      el('div', { class: 'metric-icon' }, iconEl('platform', 22)),
      el('div', { class: 'metric-body' },
        el('div', { class: 'metric-value', text: String(status.backend || '-') }),
        el('div', { class: 'metric-label', text: t('secrets.backend') })
      )
    ),
    el('div', { class: 'metric-card metric-tasks' },
      el('div', { class: 'metric-icon' }, iconEl(sealedIcon(status.sealed), 22)),
      el('div', { class: 'metric-body' },
        el('div', { class: 'metric-value' }, secretsSealedBadge(status.sealed)),
        el('div', { class: 'metric-label', text: t('secrets.sealedStatus') })
      )
    ),
    el('div', { class: 'metric-card metric-alerts' },
      el('div', { class: 'metric-icon' }, iconEl('apikey', 22)),
      el('div', { class: 'metric-body' },
        el('div', { class: 'metric-value', text: String(formatNumber(status.keys || 0)) }),
        el('div', { class: 'metric-label', text: t('secrets.keyCount') })
      )
    )
  ));
  container.appendChild(card);
}

// sealedIcon 根据sealed状态选择图标。
function sealedIcon(sealed) {
  return sealed ? 'shield' : 'check';
}

// renderSecretsTestResult 渲染密钥测试结果。
// result: {status, error}
export function renderSecretsTestResult(container, result) {
  container.innerHTML = '';
  if (!result) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const card = el('div', { class: 'detail-card' });
  card.appendChild(el('h3', { class: 'detail-title', text: t('secrets.testResult') }));
  const ok = String(result.status || '').toLowerCase() === 'ok';
  card.appendChild(el('div', { class: 'state ' + (ok ? 'state-success' : 'state-error') },
    el('span', { text: ok ? t('secrets.testOk') : t('secrets.testFailed') })
  ));
  if (result.error) {
    card.appendChild(el('div', { class: 'error-detail', text: String(result.error) }));
  }
  container.appendChild(card);
}

// renderSecretKeysTable 渲染密钥列表表格。
// keys: [{name, createdAt, rotatedAt}]
export function renderSecretKeysTable(container, keys) {
  container.innerHTML = '';
  if (!keys || keys.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('secrets.keyName') }),
        el('th', { text: t('common.createdAt') }),
        el('th', { text: t('secrets.rotatedAt') })
      )
    ),
    el('tbody', null,
      keys.map((k) => el('tr', null,
        el('td', { class: 'mono', text: k.name || '-' }),
        el('td', { class: 'mono', text: formatTime(k.createdAt) }),
        el('td', { class: 'mono', text: formatTime(k.rotatedAt) })
      ))
    )
  );
  container.appendChild(table);
}