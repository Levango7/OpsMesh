// flow-platform.js — 平台配置编排（由 flow.js 拆分）。

// flow 子模块 — 由 flow.js 拆分而来。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, $ } from './flow-state.js';

function platformContent() { return $('platform-content'); }

// loadPlatform 加载平台配置页（配置 + 健康 + 指标）。
export async function loadPlatform() {
  state._platformLoaded = true;
  const content = platformContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const [config, health, metrics] = await Promise.all([
      api.getPlatformConfig().catch(() => null),
      api.getPlatformHealth().catch(() => null),
      api.getPlatformMetrics().catch(() => null),
    ]);
    state.platformConfig = config;
    state.platformHealth = health;
    state.platformMetrics = metrics;
    render.renderPlatformPage(content, config, health, metrics, {
      onSaveConfig: (data) => savePlatformConfig(data),
    });
  } catch (err) {
    render.renderError(content, t('platform.configLoadFailed') + ': ' + err.message);
  }
}

// savePlatformConfig 保存平台配置。
export async function savePlatformConfig(data) {
  try {
    await api.updatePlatformConfig(data);
    render.renderToast(t('platform.configSaved'), 'success');
    state.platformConfig = data;
  } catch (err) {
    render.renderToast(t('platform.configSaveFailed') + ': ' + err.message, 'error');
  }
}

// buildPlatformToolbar 构建平台配置工具栏。
export function buildPlatformToolbar() {
  const toolbar = $('platform-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => loadPlatform() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}
