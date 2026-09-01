// flow-network.js — 网络管理编排（由 flow.js 拆分）。

// flow 子模块 — 由 flow.js 拆分而来。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, \$, pageRoot } from './flow-state.js';

// ============================================================================
// Phase 4：网络管理
// ============================================================================

function networkContent() { return $('network-content'); }

// loadNetworkDevices 加载网络设备列表（含子 tab 切换：设备列表 / 网络发现 / 配置下发）。
export async function loadNetworkDevices() {
  const content = networkContent();
  if (!content) return;
  content.innerHTML = '';
  // 子 tab 切换条
  const subTabs = [
    { key: 'devices',  label: t('network.devices'),  onclick: () => { state.networkSubTab = 'devices';  refreshNetworkSubTab(); } },
    { key: 'discover', label: t('network.discover'), onclick: () => { state.networkSubTab = 'discover'; refreshNetworkSubTab(); } },
    { key: 'config',   label: t('network.config'),   onclick: () => { state.networkSubTab = 'config';   refreshNetworkSubTab(); } },
  ];
  const subBar = render.el('div', { class: 'toolbar' });
  subTabs.forEach((s) => {
    subBar.appendChild(render.el('button', {
      class: 'btn ' + (state.networkSubTab === s.key ? 'btn-secondary' : 'btn-ghost'),
      onclick: s.onclick,
    }, render.el('span', { text: s.label })));
  });
  content.appendChild(subBar);
  const host = render.el('div', { class: 'content', style: { marginTop: '1rem' } });
  content.appendChild(host);
  if (state.networkSubTab !== 'devices') { refreshNetworkSubTab(); return; }
  // 设备列表 + 添加设备表单
  const listHost = render.el('div', { id: 'network-devices-list' });
  host.appendChild(listHost);
  render.renderLoading(listHost);
  try {
    const devices = await api.getNetworkDevices();
    state.networkDevices = devices;
    render.renderNetworkDevicesTable(listHost, devices, {
      onDetail: (d) => showNetworkDeviceDetail(d.id || d.name),
      onDelete: (d) => deleteNetworkDevice(d.id || d.name),
    });
  } catch (err) {
    render.renderError(listHost, t('network.devicesLoadFailed') + ': ' + err.message);
  }
  // 添加设备表单
  const formHost = render.el('div', { id: 'network-device-form', style: { marginTop: '1rem' } });
  host.appendChild(formHost);
  render.renderNetworkDeviceForm(formHost, { onCreate: (data) => createNetworkDevice(data) });
}

// showNetworkDeviceDetail 显示网络设备详情（监控指标）。
export async function showNetworkDeviceDetail(id) {
  if (!id) return;
  const content = networkContent();
  if (!content) return;
  content.innerHTML = '';
  // 返回按钮
  const backBar = render.el('div', { class: 'toolbar' });
  backBar.appendChild(render.el('button', {
    class: 'btn btn-ghost',
    onclick: () => { state.networkSubTab = 'devices'; loadNetworkDevices(); },
  }, iconEl('back', 14), render.el('span', { text: t('network.devices') })));
  content.appendChild(backBar);
  const host = render.el('div', { class: 'content', style: { marginTop: '1rem' } });
  content.appendChild(host);
  render.renderLoading(host);
  try {
    const [device, metrics] = await Promise.all([
      api.getNetworkDevice(id).catch(() => null),
      api.getNetworkDeviceMetrics(id).catch(() => null),
    ]);
    // 设备基本信息
    if (device) {
      const infoCard = render.el('div', { class: 'content', style: { marginBottom: '1rem' } });
      infoCard.appendChild(render.el('h3', { class: 'form-title', text: t('network.deviceDetail') }));
      const fields = [
        { label: t('network.deviceName'), value: device.name || device.id },
        { label: t('network.deviceType'), value: device.type },
        { label: t('network.deviceIP'), value: device.ip || device.managementIP },
        { label: t('network.devicePort'), value: device.port },
        { label: t('network.vendor'), value: device.vendor },
        { label: t('network.model'), value: device.model },
        { label: t('network.status'), value: device.status },
      ];
      fields.forEach((f) => {
        if (f.value != null && f.value !== '') {
          infoCard.appendChild(render.el('div', { class: 'form-row' },
            render.el('label', { class: 'form-label', text: f.label }),
            render.el('div', { class: 'form-control', text: String(f.value) })
          ));
        }
      });
      host.appendChild(infoCard);
    }
    // 监控指标
    const metricsHost = render.el('div', { id: 'network-metrics' });
    host.appendChild(metricsHost);
    render.renderNetworkDeviceMetrics(metricsHost, metrics);
  } catch (err) {
    render.renderError(host, t('network.metricsLoadFailed') + ': ' + err.message);
  }
}

// createNetworkDevice 创建网络设备。
export async function createNetworkDevice(data) {
  if (!data || !data.name) { render.renderToast(t('network.nameRequired'), 'warn'); return; }
  if (!data.ip) { render.renderToast(t('network.ipRequired'), 'warn'); return; }
  try {
    await api.createNetworkDevice(data);
    render.renderToast(t('network.deviceCreated'), 'success');
    state.networkSubTab = 'devices';
    loadNetworkDevices();
  } catch (err) {
    render.renderToast(t('network.deviceCreateFailed') + ': ' + err.message, 'error');
  }
}

// deleteNetworkDevice 删除网络设备。
export async function deleteNetworkDevice(id) {
  if (!id) return;
  if (!window.confirm(t('network.deleteConfirm'))) return;
  try {
    await api.deleteNetworkDevice(id);
    render.renderToast(t('network.deviceDeleted'), 'success');
    loadNetworkDevices();
  } catch (err) {
    render.renderToast(t('network.deviceDeleteFailed') + ': ' + err.message, 'error');
  }
}

// discoverNetwork 执行网络发现。
export async function discoverNetwork(subnet) {
  if (!subnet) { render.renderToast(t('network.subnetRequired'), 'warn'); return; }
  const host = $('network-discover-result');
  if (host) render.renderLoading(host, t('network.discovering'));
  try {
    const devices = await api.discoverNetwork(subnet);
    state.networkDiscovered = devices;
    if (host) render.renderNetworkDiscoverResult(host, devices);
    render.renderToast(t('network.discoverResult') + ': ' + (devices.length || 0), 'success');
  } catch (err) {
    if (host) render.renderError(host, t('network.discoverFailed') + ': ' + err.message);
    render.renderToast(t('network.discoverFailed') + ': ' + err.message, 'error');
  }
}

// deployNetworkConfig 下发配置到网络设备。
export async function deployNetworkConfig(deviceId, config) {
  if (!deviceId) { render.renderToast(t('network.deviceRequired'), 'warn'); return; }
  if (!config) { render.renderToast(t('network.configRequired'), 'warn'); return; }
  render.renderToast(t('network.deploying'), 'info');
  try {
    await api.configNetworkDevice(deviceId, config);
    render.renderToast(t('network.configDeployed'), 'success');
  } catch (err) {
    render.renderToast(t('network.configDeployFailed') + ': ' + err.message, 'error');
  }
}

// buildNetworkToolbar 构建网络管理工具栏。
export function buildNetworkToolbar() {
  const toolbar = $('network-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => refreshNetworkSubTab() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}

// refreshNetworkSubTab 根据当前子 tab 重新渲染网络管理页。
function refreshNetworkSubTab() {
  const sub = state.networkSubTab;
  if (sub === 'devices') { loadNetworkDevices(); return; }
  const content = networkContent();
  if (!content) return;
  content.innerHTML = '';
  // 子 tab 切换条
  const subTabs = [
    { key: 'devices',  label: t('network.devices') },
    { key: 'discover', label: t('network.discover') },
    { key: 'config',   label: t('network.config') },
  ];
  const subBar = render.el('div', { class: 'toolbar' });
  subTabs.forEach((s) => {
    subBar.appendChild(render.el('button', {
      class: 'btn ' + (sub === s.key ? 'btn-secondary' : 'btn-ghost'),
      onclick: () => { state.networkSubTab = s.key; refreshNetworkSubTab(); },
    }, render.el('span', { text: s.label })));
  });
  content.appendChild(subBar);
  const host = render.el('div', { class: 'content', style: { marginTop: '1rem' } });
  content.appendChild(host);
  if (sub === 'discover') {
    // 网络发现表单 + 结果
    render.renderNetworkDiscoverForm(host, { onDiscover: (subnet) => discoverNetwork(subnet) });
    const resultHost = render.el('div', { id: 'network-discover-result', style: { marginTop: '1rem' } });
    host.appendChild(resultHost);
    if (state.networkDiscovered.length) {
      render.renderNetworkDiscoverResult(resultHost, state.networkDiscovered);
    }
  } else if (sub === 'config') {
    // 配置下发表单
    render.renderNetworkConfigForm(host, state.networkDevices, { onDeploy: (id, cfg) => deployNetworkConfig(id, cfg) });
  }
}

