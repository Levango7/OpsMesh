// render-dashboard.js — 监控仪表盘渲染（由 render.js 拆分）。

// 渲染子模块 — 由 render.js 拆分而来。
// 公共依赖：i18n（t）、icons（iconEl/iconHtml）、render-common（DOM/Badge/表单辅助）。

import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { el, renderEmpty } from './render-common.js';

// ============================================================================
// 监控仪表盘渲染
// ============================================================================

// renderDashboardOverview 渲染概览卡片（4 个指标）。
// overview: { devices, tasks, alerts, openTickets }
export function renderDashboardOverview(container, overview) {
  container.innerHTML = '';
  const cards = [
    { icon: 'device', label: t('dashboard.devices'), value: overview.devices, cls: 'metric-devices' },
    { icon: 'task', label: t('dashboard.tasks'), value: overview.tasks, cls: 'metric-tasks' },
    { icon: 'alert', label: t('dashboard.alerts'), value: overview.alerts, cls: 'metric-alerts' },
    { icon: 'ticket', label: t('dashboard.openTickets'), value: overview.openTickets, cls: 'metric-tickets' },
  ];
  container.appendChild(el('div', { class: 'overview-grid' },
    cards.map((c) => el('div', { class: 'metric-card ' + c.cls },
      el('div', { class: 'metric-icon' }, iconEl(c.icon, 22)),
      el('div', { class: 'metric-body' },
        el('div', { class: 'metric-value', text: String(c.value) }),
        el('div', { class: 'metric-label', text: c.label })
      )
    ))
  ));
}

// parseMetrics 解析 Prometheus text exposition format，返回 { name, help, type, value } 数组。
export function parseMetrics(text) {
  if (!text) return [];
  const lines = text.split('\n');
  const result = [];
  let cur = null;
  for (const line of lines) {
    if (!line) continue;
    if (line.startsWith('# HELP ')) {
      const rest = line.slice(7);
      const sp = rest.indexOf(' ');
      cur = { name: rest.slice(0, sp), help: rest.slice(sp + 1), type: '', value: null };
      result.push(cur);
    } else if (line.startsWith('# TYPE ')) {
      const rest = line.slice(7);
      const sp = rest.indexOf(' ');
      if (cur && cur.name === rest.slice(0, sp)) cur.type = rest.slice(sp + 1);
    } else if (!line.startsWith('#')) {
      const sp = line.lastIndexOf(' ');
      if (sp > 0) {
        const name = line.slice(0, sp);
        const value = line.slice(sp + 1);
        const found = result.find((r) => r.name === name);
        if (found) found.value = value;
        else result.push({ name, help: '', type: '', value });
      }
    }
  }
  return result;
}

// renderMetricsText 渲染 Prometheus 指标文本展示。
export function renderMetricsText(container, text) {
  container.innerHTML = '';
  if (!text) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  // 解析为结构化卡片 + 原始文本。
  const metrics = parseMetrics(text);
  if (metrics.length) {
    container.appendChild(el('div', { class: 'metrics-cards' },
      metrics.map((m) => el('div', { class: 'metric-line' },
        el('div', { class: 'metric-line-head' },
          el('span', { class: 'metric-line-name mono', text: m.name }),
          m.type ? el('span', { class: 'metric-line-type', text: m.type }) : null
        ),
        m.help ? el('div', { class: 'metric-line-help', text: m.help }) : null,
        el('div', { class: 'metric-line-value mono', text: m.value != null ? m.value : '-' })
      ))
    ));
  }
  // 原始文本（<pre> 保留格式，便于复制）。
  container.appendChild(el('pre', { class: 'metrics-raw', text: text }));
}

