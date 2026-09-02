// render-config-push.js — 配置热推渲染（由 render.js 拆分）。

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
// Phase 2：配置热推渲染
// ============================================================================

// renderConfigHotpushForm 渲染配置热推送表单。
// handlers: { onSubmit(data) }
export function renderConfigHotpushForm(container, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => { e.preventDefault(); handlers.onSubmit(collectConfigHotpushForm(form)); } });

  form.appendChild(el('h3', { class: 'form-title', text: t('configPush.hotpush') }));

  form.appendChild(fieldRow(t('configPush.deviceID'), true,
    el('input', { type: 'text', name: 'deviceID', required: 'true', placeholder: t('configPush.deviceRequired') })
  ));
  form.appendChild(fieldRow(t('configPush.configKey'), true,
    el('input', { type: 'text', name: 'key', required: 'true', placeholder: t('configPush.keyRequired') })
  ));
  form.appendChild(fieldRow(t('configPush.configValue'), false,
    el('textarea', { name: 'value', rows: '3' }, '')
  ));
  form.appendChild(fieldRow(t('configPush.configPath'), false,
    el('input', { type: 'text', name: 'path', placeholder: '/etc/opsmesh/config.yaml' })
  ));

  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('rocket', 16), el('span', { text: t('common.push') }))
  ));

  container.appendChild(form);
}

function collectConfigHotpushForm(form) {
  const get = (name) => (form.elements[name] && form.elements[name].value) || '';
  return {
    deviceID: get('deviceID').trim(),
    key: get('key').trim(),
    value: get('value'),
    path: get('path').trim(),
  };
}

// renderConfigCanaryForm 渲染灰度配置发布表单。
// handlers: { onSubmit(data) }
export function renderConfigCanaryForm(container, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => { e.preventDefault(); handlers.onSubmit(collectConfigCanaryForm(form)); } });

  form.appendChild(el('h3', { class: 'form-title', text: t('configPush.canary') }));

  form.appendChild(fieldRow(t('configPush.deviceList'), true,
    el('input', { type: 'text', name: 'devices', required: 'true', placeholder: 'dev1, dev2, dev3' })
  ));
  form.appendChild(fieldRow(t('configPush.canaryPercent'), false,
    el('input', { type: 'number', name: 'percent', min: '0', max: '100', value: '10' })
  ));
  form.appendChild(fieldRow(t('configPush.configKey'), false,
    el('input', { type: 'text', name: 'key' })
  ));
  form.appendChild(fieldRow(t('configPush.configContent'), false,
    el('textarea', { name: 'content', rows: '4' }, '')
  ));

  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('check', 16), el('span', { text: t('common.apply') }))
  ));

  container.appendChild(form);
}

function collectConfigCanaryForm(form) {
  const get = (name) => (form.elements[name] && form.elements[name].value) || '';
  const devices = get('devices').split(',').map((s) => s.trim()).filter(Boolean);
  return {
    devices,
    percent: parseInt(get('percent'), 10) || 0,
    key: get('key').trim(),
    content: get('content'),
  };
}

// renderConfigVersions 渲染配置版本历史表格。
export function renderConfigVersions(container, versions) {
  container.innerHTML = '';
  if (!versions || versions.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.version') }),
        el('th', { text: t('configPush.configKey') }),
        el('th', { text: t('common.value') }),
        el('th', { text: t('common.createdAt') })
      )
    ),
    el('tbody', null,
      versions.map((v) => el('tr', null,
        el('td', { class: 'mono', text: String(v.version != null ? v.version : (v.id || '-')) }),
        el('td', { class: 'mono', text: v.key || '-' }),
        el('td', { class: 'mono', text: String(v.value != null ? v.value : '-').slice(0, 60) }),
        el('td', { class: 'mono', text: formatTime(v.createdAt || v.timestamp) })
      ))
    )
  );
  container.appendChild(table);
}

