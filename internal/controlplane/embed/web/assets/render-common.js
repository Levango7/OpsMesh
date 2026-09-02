// render-common.js — OpsMesh 前端渲染公共工具（由 render.js 拆分）。
// 职责：DOM 构建、格式化、通用渲染、Badge 辅助、表单字段行、详情项。

import { t } from './i18n.js';
import { iconEl } from './icons.js';

// ============================================================================
// DOM 构建辅助
// ============================================================================

// el(tag, props, ...children) 语义化 DOM 构建。
// props 支持：class/className, text, html, style(object), on*(event), dataset, 以及任意 attribute。
export function el(tag, props, ...children) {
  const node = document.createElement(tag);
  if (props) {
    for (const k of Object.keys(props)) {
      const v = props[k];
      if (v === undefined || v === null) continue;
      if (k === 'class' || k === 'className') node.className = v;
      else if (k === 'text') node.textContent = String(v);
      else if (k === 'html') node.innerHTML = v;
      else if (k === 'style' && typeof v === 'object') Object.assign(node.style, v);
      else if (k === 'dataset' && typeof v === 'object') Object.assign(node.dataset, v);
      else if (k.startsWith('on') && typeof v === 'function') node.addEventListener(k.slice(2).toLowerCase(), v);
      else node.setAttribute(k, String(v));
    }
  }
  appendChildren(node, children);
  return node;
}

function appendChildren(node, children) {
  for (const c of children) {
    if (c == null || c === false) continue;
    if (typeof c === 'string' || typeof c === 'number') node.appendChild(document.createTextNode(String(c)));
    else if (Array.isArray(c)) appendChildren(node, c);
    else if (c instanceof Node) node.appendChild(c);
  }
}

// formatTime 格式化 ISO 时间为 "YYYY-MM-DD HH:mm"。
export function formatTime(ts) {
  if (!ts) return '-';
  const d = new Date(ts);
  if (isNaN(d.getTime())) return '-';
  const pad = (n) => String(n).padStart(2, '0');
  return d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate()) +
    ' ' + pad(d.getHours()) + ':' + pad(d.getMinutes());
}

// ============================================================================
// 通用渲染
// ============================================================================

export function renderLoading(container, msg) {
  container.innerHTML = '';
  container.appendChild(el('div', { class: 'state state-loading' },
    el('span', { class: 'spinner', 'aria-hidden': 'true' }),
    el('span', { text: msg || t('common.loading') })
  ));
}

export function renderError(container, msg) {
  container.innerHTML = '';
  container.appendChild(el('div', { class: 'state state-error' },
    iconEl('alert', 18),
    el('span', { text: msg || t('common.error') })
  ));
}

export function renderEmpty(container, msg) {
  container.innerHTML = '';
  container.appendChild(el('div', { class: 'state state-empty' },
    el('span', { text: msg || t('common.empty') })
  ));
}

// renderToast 显示临时提示（3 秒后自动消失）。
export function renderToast(message, kind) {
  const host = document.getElementById('toastHost') || (() => {
    const h = el('div', { id: 'toastHost', class: 'toast-host', 'aria-live': 'polite' });
    document.body.appendChild(h);
    return h;
  })();
  const item = el('div', { class: 'toast toast-' + (kind || 'info'), text: message });
  host.appendChild(item);
  setTimeout(() => {
    item.classList.add('toast-leave');
    setTimeout(() => item.remove(), 200);
  }, 3000);
}

// ============================================================================
// Badge 辅助
// ============================================================================

export function badge(text, cls) {
  return el('span', { class: 'badge badge-' + cls, text });
}

export function statusBadge(status) {
  return badge(t('ticket.status.' + status, status), 'status-' + status);
}

export function priorityBadge(priority) {
  return badge(t('ticket.priority.' + priority, priority), 'priority-' + priority);
}

export function categoryBadge(category) {
  return badge(t('ticket.category.' + category, category), 'category-' + category);
}

export function sloStatusBadge(status) {
  return badge(t('slo.status.' + status, status), 'slo-status-' + status);
}

// detailItem 详情项（label + value），跨域共用。
export function detailItem(label, value, mono) {
  const valNode = (value instanceof Node) ? value : el('span', { class: mono ? 'mono' : '', text: String(value) });
  return el('div', { class: 'detail-item' },
    el('span', { class: 'detail-label', text: label }),
    el('span', { class: 'detail-value' + (mono ? ' mono' : '') }, valNode)
  );
}

// fieldRow 表单字段行（label + control）。
export function fieldRow(label, required, control) {
  return el('div', { class: 'form-row' },
    el('label', { class: 'form-label' + (required ? ' required' : ''), text: label }),
    el('div', { class: 'form-control' }, control)
  );
}

// formatNumber 数字格式化，跨域共用。
export function formatNumber(n) {
  if (n == null || isNaN(n)) return '-';
  return String(n);
}

// ============================================================================
// 业务域 Badge 辅助（跨域共用，集中维护以避免重复定义与未定义引用）
// ============================================================================

// apiKeyStatusBadge API Key 状态 badge。
export function apiKeyStatusBadge(status) {
  const s = String(status || '').toLowerCase();
  if (s === 'enabled' || s === 'active') return badge(t('common.enabled'), 'status-resolved');
  if (s === 'disabled' || s === 'inactive') return badge(t('common.disabled'), 'status-closed');
  return badge(status || '-', 'status-in_progress');
}

// healthStatusBadge 健康状态 badge。
export function healthStatusBadge(status) {
  const s = String(status || '').toLowerCase();
  if (s === 'healthy' || s === 'ok' || s === 'up') return badge(t('platform.healthy'), 'status-resolved');
  if (s === 'unhealthy' || s === 'down' || s === 'error') return badge(t('platform.unhealthy'), 'status-closed');
  if (s === 'degraded' || s === 'warn') return badge(t('platform.degraded'), 'status-in_progress');
  return badge(status || '-', 'status-in_progress');
}

// webhookStatusBadge Webhook 状态 badge。
export function webhookStatusBadge(status) {
  const s = String(status || '').toLowerCase();
  if (s === 'enabled' || s === 'active') return badge(t('webhook.enabled'), 'badge-status-resolved');
  if (s === 'disabled' || s === 'inactive') return badge(t('webhook.disabled'), 'badge-status-closed');
  return badge(status || '-', 'badge-status-in_progress');
}

// scriptStatusBadge 脚本状态 badge。
export function scriptStatusBadge(status) {
  const s = String(status || '').toLowerCase();
  if (s === 'enabled' || s === 'active') return badge(t('script.enabled'), 'badge-status-resolved');
  if (s === 'disabled' || s === 'inactive') return badge(t('script.disabled'), 'badge-status-closed');
  return badge(status || '-', 'badge-status-in_progress');
}

// pluginInstallBadge 插件安装状态 badge。
export function pluginInstallBadge(status) {
  const s = String(status || '').toLowerCase();
  if (s === 'installed') return badge(t('plugin.installed'), 'status-resolved');
  if (s === 'notinstalled' || s === 'not_installed' || s === '' ) return badge(t('plugin.notInstalled'), 'status-closed');
  return badge(status || '-', 'status-in_progress');
}
