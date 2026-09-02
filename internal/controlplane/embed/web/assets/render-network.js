// render-network.js — 网络管理渲染（由 render.js 拆分）。

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
// Phase 4：网络管理渲染
// ============================================================================

// networkStatusBadge 网络设备状态 badge。
function networkStatusBadge(status) {
  const s = String(status || '').toLowerCase();
  const cls = s === 'online' || s === 'up' || s === 'healthy' ? 'badge-status-resolved'
    : s === 'offline' || s === 'down' ? 'badge-priority-urgent'
    : 'badge-status-in_progress';
  return badge(status || t('network.unknown'), cls);
}

// renderNetworkDevicesTable 渲染网络设备列表表格。
// handlers: { onDetail(device), onDelete(device) }
export function renderNetworkDevicesTable(container, devices, handlers) {
  container.innerHTML = '';
  if (!devices || !devices.length) { renderEmpty(container, t('network.noDevices')); return; }
  container.appendChild(el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('network.deviceName') }),
        el('th', { text: t('network.deviceType') }),
        el('th', { text: t('network.deviceIP') }),
        el('th', { text: t('network.status') }),
        el('th', { text: t('network.bandwidth') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      devices.map((d) => el('tr', null,
        el('td', { class: 'cell-title', text: d.name || d.id || '-' }),
        el('td', null, badge(d.type || '-', 'badge-category-change')),
        el('td', { class: 'mono', text: d.ip || d.managementIP || '-' }),
        el('td', null, networkStatusBadge(d.status)),
        el('td', { text: d.bandwidth != null ? (d.bandwidth + '%') : '-' }),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn btn-ghost', title: t('network.deviceDetail'), onclick: () => handlers.onDetail && handlers.onDetail(d) },
            iconEl('search', 14)
          ),
          el('button', { class: 'btn btn-ghost btn-icon-danger', title: t('common.delete'), onclick: () => handlers.onDelete && handlers.onDelete(d) },
            iconEl('trash', 14)
          )
        )
      ))
    )
  ));
}

// renderNetworkDeviceForm 渲染添加网络设备表单。
// handlers: { onCreate(data) }
export function renderNetworkDeviceForm(container, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const data = {
      name: form.elements.name.value.trim(),
      type: form.elements.type.value.trim(),
      ip: form.elements.ip.value.trim(),
      port: parseInt(form.elements.port.value, 10) || 22,
      vendor: form.elements.vendor.value.trim(),
      model: form.elements.model.value.trim(),
    };
    handlers.onCreate && handlers.onCreate(data);
  } });
  form.appendChild(el('h3', { class: 'form-title', text: t('network.addDevice') }));
  form.appendChild(fieldRow(t('network.deviceName'), true,
    el('input', { name: 'name', type: 'text', required: 'true', placeholder: 'switch-01' })
  ));
  form.appendChild(fieldRow(t('network.deviceType'), true,
    el('select', { name: 'type', required: 'true' },
      el('option', { value: 'switch', text: 'switch' }),
      el('option', { value: 'router', text: 'router' }),
      el('option', { value: 'firewall', text: 'firewall' }),
      el('option', { value: 'loadbalancer', text: 'loadbalancer' }),
      el('option', { value: 'ap', text: 'AP' })
    )
  ));
  form.appendChild(fieldRow(t('network.deviceIP'), true,
    el('input', { name: 'ip', type: 'text', required: 'true', placeholder: '192.168.1.1' })
  ));
  form.appendChild(fieldRow(t('network.devicePort'), false,
    el('input', { name: 'port', type: 'number', value: '22', min: '1', max: '65535' })
  ));
  form.appendChild(fieldRow(t('network.vendor'), false,
    el('input', { name: 'vendor', type: 'text', placeholder: 'Cisco/Huawei/Juniper' })
  ));
  form.appendChild(fieldRow(t('network.model'), false,
    el('input', { name: 'model', type: 'text', placeholder: 'Catalyst 2960' })
  ));
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' },
      iconEl('plus', 16), el('span', { text: t('network.addDevice') })
    )
  ));
  container.appendChild(form);
}

// renderNetworkDeviceMetrics 渲染网络设备监控指标。
export function renderNetworkDeviceMetrics(container, metrics) {
  container.innerHTML = '';
  if (!metrics) { renderEmpty(container); return; }
  const card = el('div', { class: 'content' });
  card.appendChild(el('h3', { class: 'form-title', text: t('network.metrics') }));
  const fields = [
    { key: 'bandwidth', label: t('network.bandwidthUsage'), suffix: '%' },
    { key: 'latency', label: t('network.latency'), suffix: 'ms' },
    { key: 'packetLoss', label: t('network.packetLoss'), suffix: '%' },
    { key: 'cpu', label: 'CPU', suffix: '%' },
    { key: 'memory', label: 'Memory', suffix: '%' },
    { key: 'uptime', label: 'Uptime', suffix: '' },
  ];
  fields.forEach((f) => {
    const v = metrics[f.key];
    if (v != null && v !== '') {
      card.appendChild(el('div', { class: 'form-row' },
        el('label', { class: 'form-label', text: f.label }),
        el('div', { class: 'form-control', text: String(v) + f.suffix })
      ));
    }
  });
  container.appendChild(card);
}

// renderNetworkDiscoverForm 渲染网络发现表单。
// handlers: { onDiscover(subnet) }
export function renderNetworkDiscoverForm(container, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    handlers.onDiscover && handlers.onDiscover(form.elements.subnet.value.trim());
  } });
  form.appendChild(el('h3', { class: 'form-title', text: t('network.discover') }));
  form.appendChild(fieldRow(t('network.subnet'), true,
    el('input', { name: 'subnet', type: 'text', required: 'true', placeholder: t('network.subnetPlaceholder') })
  ));
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' },
      iconEl('discover', 16), el('span', { text: t('network.startDiscover') })
    )
  ));
  container.appendChild(form);
}

// renderNetworkDiscoverResult 渲染网络发现结果。
export function renderNetworkDiscoverResult(container, devices) {
  container.innerHTML = '';
  if (!devices || !devices.length) { renderEmpty(container, t('network.noDiscovered')); return; }
  container.appendChild(el('h3', { class: 'form-title', text: t('network.discoveredDevices') }));
  container.appendChild(el('table', { class: 'data-table data-table-compact' },
    el('thead', null,
      el('tr', null,
        el('th', { text: 'IP' }),
        el('th', { text: t('network.deviceType') }),
        el('th', { text: t('network.vendor') }),
        el('th', { text: t('network.status') })
      )
    ),
    el('tbody', null,
      devices.map((d) => el('tr', null,
        el('td', { class: 'cell-title mono', text: d.ip || d.ipAddress || '-' }),
        el('td', { text: d.type || '-' }),
        el('td', { text: d.vendor || d.vendorName || '-' }),
        el('td', null, networkStatusBadge(d.status))
      ))
    )
  ));
}

// renderNetworkConfigForm 渲染配置下发表单。
// devices: 可选设备列表；handlers: { onDeploy(deviceId, config) }
export function renderNetworkConfigForm(container, devices, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const deviceId = form.elements.device.value;
    const config = form.elements.config.value;
    handlers.onDeploy && handlers.onDeploy(deviceId, config);
  } });
  form.appendChild(el('h3', { class: 'form-title', text: t('network.config') }));
  form.appendChild(fieldRow(t('network.selectDevice'), true,
    el('select', { name: 'device', required: 'true' },
      el('option', { value: '', text: '-- ' + t('network.selectDevice') + ' --' }),
      (devices || []).map((d) => el('option', { value: d.id || d.name, text: (d.name || d.id) + ' (' + (d.ip || '-') + ')' }))
    )
  ));
  form.appendChild(fieldRow(t('network.configContent'), true,
    el('textarea', { name: 'config', rows: '6', required: 'true', placeholder: t('network.configPlaceholder'), style: { width: '100%', fontFamily: 'monospace' } })
  ));
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' },
      iconEl('config_deploy', 16), el('span', { text: t('network.deployConfig') })
    )
  ));
  container.appendChild(form);
}

