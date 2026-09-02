// flow-federation.js — 控制面联邦编排（P2 补齐功能域）。

// flow 子模块 — 控制面联邦（Peer 管理 + 设备聚合视图 + 任务转发）。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, $, pageRoot } from './flow-state.js';

// ============================================================================
// 控制面联邦
// ============================================================================

function federationContent() { return $('federation-content'); }

// loadFederationPeers 加载 Peer 列表。
export async function loadFederationPeers() {
  const content = federationContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const data = await api.getFederationPeers();
    const peers = (data && data.peers) ? data.peers : (Array.isArray(data) ? data : []);
    state.federation.peers = peers;
    render.renderFederationPeersTable(content, peers);
  } catch (err) {
    render.renderError(content, t('federation.peersLoadFailed') + ': ' + err.message);
  }
}

// loadFederationDevices 加载跨 peer 设备聚合视图。
export async function loadFederationDevices() {
  const content = federationContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const data = await api.getFederationDevices();
    const devices = (data && data.devices) ? data.devices : [];
    const peerSummary = (data && data.peers) ? data.peers : [];
    state.federation.devices = devices;
    render.renderFederationDevicesTable(content, devices, peerSummary);
  } catch (err) {
    render.renderError(content, t('federation.devicesLoadFailed') + ': ' + err.message);
  }
}

// showFederationForwardForm 打开任务转发表单。
export function showFederationForwardForm() {
  const content = federationContent();
  if (!content) return;
  // 确保 peer 列表已加载
  const ensurePeers = async () => {
    if (state.federation.peers.length === 0) {
      try {
        const data = await api.getFederationPeers();
        state.federation.peers = (data && data.peers) ? data.peers : (Array.isArray(data) ? data : []);
      } catch (_) { /* 静默，使用空列表 */ }
    }
    return state.federation.peers;
  };
  ensurePeers().then((peers) => {
    render.renderFederationForwardForm(content, peers, {
      onSubmit: async (data) => {
        if (!data.peerURL) {
          render.renderToast(t('federation.peerRequired'), 'warn');
          return;
        }
        state.federation.loading = true;
        state.federation.error = null;
        try {
          const result = await api.forwardTask(data);
          state.federation.loading = false;
          render.renderFederationForwardResult(content, result);
          render.renderToast(t('federation.forwarded'), 'success');
        } catch (err) {
          state.federation.loading = false;
          state.federation.error = err.message;
          render.renderToast(t('federation.forwardFailed') + ': ' + err.message, 'error');
          // 失败后回到表单
          showFederationForwardForm();
        }
      },
      onCancel: () => showFederationForwardForm(),
    });
  });
}

// refreshFederationSubTab 按当前子 tab 刷新联邦页。
export function refreshFederationSubTab() {
  const sub = state.federationSubTab;
  if (sub === 'devices') loadFederationDevices();
  else if (sub === 'forward') showFederationForwardForm();
  else loadFederationPeers();
}

// buildFederationToolbar 构建联邦工具栏（子 tab + 刷新）。
export function buildFederationToolbar() {
  const toolbar = $('federation-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  // 子 tab 切换组
  const subTabs = [
    { key: 'peers', label: t('federation.tabPeers'), onActivate: () => loadFederationPeers() },
    { key: 'devices', label: t('federation.tabDevices'), onActivate: () => loadFederationDevices() },
    { key: 'forward', label: t('federation.tabForward'), onActivate: () => showFederationForwardForm() },
  ];
  subTabs.forEach((st) => {
    toolbar.appendChild(
      render.el('button', {
        class: 'btn ' + (state.federationSubTab === st.key ? 'btn-secondary' : 'btn-ghost'),
        onclick: () => { state.federationSubTab = st.key; st.onActivate(); buildFederationToolbar(); },
      }, render.el('span', { text: st.label }))
    );
  });
  // 刷新
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => refreshFederationSubTab() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}