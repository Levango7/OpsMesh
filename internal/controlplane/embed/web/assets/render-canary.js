// render-canary.js — 灰度发布渲染（由 render.js 拆分）。

// 渲染子模块 — 由 render.js 拆分而来。
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
// Phase 2：灰度发布渲染
// ============================================================================

// renderCanaryList 渲染灰度发布列表（带选择）。
// handlers: { onSelect(id) }
export function renderCanaryList(container, releases, handlers) {
  container.innerHTML = '';
  if (!releases || releases.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('common.name') }),
        el('th', { text: t('common.service') }),
        el('th', { text: t('canary.trafficPercent') }),
        el('th', { text: t('common.status') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      releases.map((r) => el('tr', null,
        el('td', { class: 'mono', text: r.id }),
        el('td', { class: 'cell-title', text: r.name || '-' }),
        el('td', { text: r.service || '-' }),
        el('td', { class: 'mono', text: String(r.percent != null ? r.percent : 0) + '%' }),
        el('td', null, el('span', { class: 'badge badge-status-' + (r.status || 'in_progress'), text: r.status || '-' })),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn-icon', title: t('canary.trafficSplit'), onclick: () => handlers.onSelect(r.id) }, iconEl('sliders', 14))
        )
      ))
    )
  );
  container.appendChild(table);
}

// renderCanarySplitPanel 渲染流量分割面板（滑块 + 应用按钮）。
// handlers: { onApply(percent) }
export function renderCanarySplitPanel(container, release, handlers) {
  container.innerHTML = '';
  if (!release) { renderEmpty(container, t('canary.select')); return; }
  const card = el('div', { class: 'detail-card' });
  card.appendChild(el('h3', { class: 'detail-title', text: t('canary.trafficSplit') + ' · ' + (release.name || release.id) }));

  const currentPercent = release.percent != null ? release.percent : 0;
  const sliderRow = el('div', { class: 'form-row' },
    el('label', { class: 'form-label', text: t('canary.trafficPercent') }),
    el('div', { class: 'form-control' },
      el('input', { type: 'range', name: 'percent', min: '0', max: '100', step: '1', value: String(currentPercent), style: { width: '60%', verticalAlign: 'middle' } }),
      el('span', { class: 'mono', id: 'canaryPercentLabel', text: ' ' + currentPercent + '%', style: { marginLeft: '0.6rem' } })
    )
  );
  card.appendChild(sliderRow);

  // 滑块实时更新标签
  const slider = sliderRow.querySelector('input[name=percent]');
  const label = sliderRow.querySelector('#canaryPercentLabel');
  slider.addEventListener('input', () => {
    if (label) label.textContent = ' ' + slider.value + '%';
  });

  card.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'button', class: 'btn btn-primary', onclick: () => handlers.onApply(parseInt(slider.value, 10) || 0) },
      iconEl('check', 16), el('span', { text: t('canary.applySplit') })
    )
  ));

  container.appendChild(card);
}

// renderCanaryMetrics 渲染灰度指标对比表格。
// metrics: { old: {qps, latency, errorRate}, new: {qps, latency, errorRate} }
export function renderCanaryMetrics(container, metrics) {
  container.innerHTML = '';
  if (!metrics) { renderEmpty(container, t('common.empty')); return; }
  const oldM = metrics.old || metrics.baseline || {};
  const newM = metrics.new || metrics.canary || {};
  const card = el('div', { class: 'detail-card' });
  card.appendChild(el('h3', { class: 'detail-title', text: t('canary.metrics') }));
  card.appendChild(el('table', { class: 'data-table data-table-compact' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.name') }),
        el('th', { text: t('canary.oldVersion') }),
        el('th', { text: t('canary.newVersion') })
      )
    ),
    el('tbody', null,
      el('tr', null,
        el('td', { text: t('canary.metricQps') }),
        el('td', { class: 'mono', text: formatNumber(oldM.qps) }),
        el('td', { class: 'mono', text: formatNumber(newM.qps) })
      ),
      el('tr', null,
        el('td', { text: t('canary.metricLatency') }),
        el('td', { class: 'mono', text: formatNumber(oldM.latency) }),
        el('td', { class: 'mono', text: formatNumber(newM.latency) })
      ),
      el('tr', null,
        el('td', { text: t('canary.metricErrorRate') }),
        el('td', { class: 'mono', text: formatNumber(oldM.errorRate) }),
        el('td', { class: 'mono', text: formatNumber(newM.errorRate) })
      )
    )
  ));
  container.appendChild(card);
}

