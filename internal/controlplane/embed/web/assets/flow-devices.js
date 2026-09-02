// flow-devices.js — 设备管理编排（P0 补齐功能域）。

// flow 子模块 — 设备管理（设备列表 / 退役 / 指标 / 代理列表）。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, $ } from './flow-state.js';

// ============================================================================
// 设备管理
// ============================================================================

function devicesContent() { return $('devices-content'); }

// loadDevices 加载设备列表。
export async function loadDevices() {
  const content = devicesContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const devices = await api.getDevices();
    state.devices = devices;
    render.renderDevicesTable(content, devices, {
      onMetrics: (id) => showDeviceMetrics(id),
      onRetire: (id) => retireDevice(id),
    });
  } catch (err) {
    render.renderError(content, t('devices.loadFailed') + ': ' + err.message);
  }
}

// showDeviceMetrics 查看设备指标。
export async function showDeviceMetrics(id) {
  const content = devicesContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const metrics = await api.getDeviceMetrics(id);
    render.renderDeviceMetrics(content, id, metrics, {
      onBack: () => loadDevices(),
    });
  } catch (err) {
    render.renderError(content, t('devices.metricsLoadFailed') + ': ' + err.message);
  }
}

// retireDevice 退役设备（确认后调用 API）。
export async function retireDevice(id) {
  if (!window.confirm(t('devices.confirmRetire'))) return;
  try {
    await api.retireDevice(id);
    render.renderToast(t('devices.retired'), 'success');
    loadDevices();
  } catch (err) {
    render.renderToast(t('devices.retireFailed') + ': ' + err.message, 'error');
  }
}

// loadAgents 加载代理列表。
export async function loadAgents() {
  const content = devicesContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const agents = await api.getAgents();
    state.agents = agents;
    render.renderAgentsTable(content, agents, {});
  } catch (err) {
    render.renderError(content, t('devices.agentsLoadFailed') + ': ' + err.message);
  }
}

// refreshDevicesSubTab 按当前子 tab 刷新设备页。
export function refreshDevicesSubTab() {
  if (state.devicesSubTab === 'agents') loadAgents();
  else loadDevices();
}

// buildDevicesToolbar 构建设备工具栏（子 tab 切换 + 刷新）。
export function buildDevicesToolbar() {
  const toolbar = $('devices-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  // 子 tab 切换组
  const subTabs = [
    { key: 'devices', label: t('devices.tabDevices'), onActivate: () => loadDevices() },
    { key: 'agents', label: t('devices.tabAgents'), onActivate: () => loadAgents() },
  ];
  subTabs.forEach((st) => {
    toolbar.appendChild(
      render.el('button', {
        class: 'btn ' + (state.devicesSubTab === st.key ? 'btn-secondary' : 'btn-ghost'),
        onclick: () => { state.devicesSubTab = st.key; st.onActivate(); buildDevicesToolbar(); },
      }, render.el('span', { text: st.label }))
    );
  });
  // 刷新
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => refreshDevicesSubTab() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}
