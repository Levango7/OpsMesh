// flow-config-push.js — 配置热推编排（由 flow.js 拆分）。

// flow 子模块 — 由 flow.js 拆分而来。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, $ } from './flow-state.js';

// ============================================================================
// Phase 2：配置热推
// ============================================================================

function configPushContent() { return $('config-push-content'); }

// loadConfigVersions 加载配置版本历史（默认空 key）。
export async function loadConfigVersions(key) {
  state._configPushLoaded = true;
  const content = configPushContent();
  if (!content) return;
  content.innerHTML = '';
  // 上：热推送表单
  const hotpushHost = render.el('div', { class: 'content', style: { marginBottom: '1rem' } });
  render.renderConfigHotpushForm(hotpushHost, {
    onSubmit: (data) => hotpushConfig(data),
  });
  content.appendChild(hotpushHost);
  // 中：灰度配置表单
  const canaryHost = render.el('div', { class: 'content', style: { marginBottom: '1rem' } });
  render.renderConfigCanaryForm(canaryHost, {
    onSubmit: (data) => canaryConfigPush(data),
  });
  content.appendChild(canaryHost);
  // 下：版本历史（含 Key 查询框）
  const versionHost = render.el('div', { class: 'content' });
  versionHost.appendChild(render.el('h3', { class: 'form-title', text: t('configPush.versions') }));
  // Key 查询行
  const queryRow = render.el('div', { class: 'form-row' },
    render.el('label', { class: 'form-label', text: t('configPush.queryKey') }),
    render.el('div', { class: 'form-control' },
      (() => {
        const input = render.el('input', { type: 'text', name: 'queryKey', value: key || '', placeholder: 'config key' });
        input.addEventListener('change', () => loadConfigVersions(input.value));
        return input;
      })()
    )
  );
  versionHost.appendChild(queryRow);
  const versionsHost = render.el('div');
  versionHost.appendChild(versionsHost);
  render.renderLoading(versionsHost);
  content.appendChild(versionHost);
  try {
    const versions = await api.getConfigVersions(key || '');
    state.configVersions = versions;
    render.renderConfigVersions(versionsHost, versions);
  } catch (err) {
    render.renderError(versionsHost, t('configPush.versionsLoadFailed') + ': ' + err.message);
  }
}

// hotpushConfig 触发配置热推送。
export async function hotpushConfig(data) {
  if (!data.deviceID) { render.renderToast(t('configPush.deviceRequired'), 'warn'); return; }
  if (!data.key) { render.renderToast(t('configPush.keyRequired'), 'warn'); return; }
  try {
    await api.hotpushConfig(data);
    render.renderToast(t('configPush.hotpushed'), 'success');
    loadConfigVersions(data.key);
  } catch (err) {
    render.renderToast(t('configPush.hotpushFailed') + ': ' + err.message, 'error');
  }
}

// canaryConfigPush 灰度配置发布。
export async function canaryConfigPush(data) {
  if (!data.devices || !data.devices.length) { render.renderToast(t('configPush.deviceRequired'), 'warn'); return; }
  try {
    await api.canaryConfig(data);
    render.renderToast(t('configPush.canaryApplied'), 'success');
    loadConfigVersions(data.key);
  } catch (err) {
    render.renderToast(t('configPush.canaryFailed') + ': ' + err.message, 'error');
  }
}

// buildConfigPushToolbar 构建配置热推工具栏。
export function buildConfigPushToolbar() {
  const toolbar = $('config-push-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => loadConfigVersions('') },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}

