// render-platform.js — 平台配置渲染（由 render.js 拆分）。

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

export function renderPlatformPage(container, config, health, metrics, handlers) {
  container.innerHTML = '';
  // 配置表单区
  const configHost = el('div', { class: 'content', style: { marginBottom: '1rem' } });
  configHost.appendChild(el('h3', { class: 'form-title', text: t('platform.config') }));
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const data = {};
    for (let i = 0; i < form.elements.length; i++) {
      const fe = form.elements[i];
      if (fe.name) data[fe.name] = fe.value;
    }
    handlers.onSaveConfig && handlers.onSaveConfig(data);
  } });
  const cfg = (config && typeof config === 'object') ? config : {};
  const cfgKeys = Object.keys(cfg);
  if (!cfgKeys.length) {
    form.appendChild(el('p', { class: 'metrics-hint', text: t('common.empty') }));
  } else {
    cfgKeys.forEach((k) => {
      form.appendChild(fieldRow(k, false,
        el('input', { name: k, type: 'text', value: String(cfg[k] != null ? cfg[k] : '') })
      ));
    });
  }
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' },
      iconEl('check', 16), el('span', { text: t('platform.saveConfig') })
    )
  ));
  configHost.appendChild(form);
  container.appendChild(configHost);

  // 健康检查区
  const healthHost = el('div', { class: 'content', style: { marginBottom: '1rem' } });
  healthHost.appendChild(el('h3', { class: 'form-title', text: t('platform.health') }));
  const healthItems = (health && (health.components || health.checks || (Array.isArray(health) ? health : null))) || [];
  if (health && !healthItems.length && health.status) {
    healthHost.appendChild(el('div', { class: 'form-row' },
      el('label', { class: 'form-label', text: t('platform.status') }),
      el('div', { class: 'form-control' }, healthStatusBadge(health.status))
    ));
  } else if (healthItems.length) {
    healthHost.appendChild(el('table', { class: 'data-table' },
      el('thead', null,
        el('tr', null,
          el('th', { text: t('platform.component') }),
          el('th', { text: t('platform.status') }),
          el('th', { text: t('platform.latency') })
        )
      ),
      el('tbody', null,
        healthItems.map((c) => el('tr', null,
          el('td', { class: 'cell-title', text: c.name || c.component || '-' }),
          el('td', null, healthStatusBadge(c.status)),
          el('td', { class: 'mono', text: String(c.latency != null ? c.latency : '-') })
        ))
      )
    ));
  } else {
    healthHost.appendChild(el('div', { class: 'state state-empty', text: t('common.empty') }));
  }
  container.appendChild(healthHost);

  // 指标区
  const metricsHost = el('div', { class: 'content' });
  metricsHost.appendChild(el('h3', { class: 'form-title', text: t('platform.metrics') }));
  const mObj = (metrics && typeof metrics === 'object') ? metrics : {};
  const mKeys = Object.keys(mObj);
  if (mKeys.length) {
    metricsHost.appendChild(el('table', { class: 'data-table' },
      el('thead', null,
        el('tr', null,
          el('th', { text: t('platform.metricName') }),
          el('th', { text: t('platform.metricValue') })
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
    metricsHost.appendChild(el('div', { class: 'state state-empty', text: t('common.empty') }));
  }
  container.appendChild(metricsHost);
}

