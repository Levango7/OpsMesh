// flow-canary.js — 灰度发布编排（由 flow.js 拆分）。

// flow 子模块 — 由 flow.js 拆分而来。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, \$, pageRoot } from './flow-state.js';

// ============================================================================
// Phase 2：灰度发布
// ============================================================================

function canaryContent() { return $('canary-content'); }

// loadCanaryReleases 加载灰度发布列表 + 默认选中第一个。
export async function loadCanaryReleases() {
  const content = canaryContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const releases = await api.getCanaryReleases();
    state.canaryReleases = releases;
    renderCanaryView(content, releases);
  } catch (err) {
    render.renderError(content, t('canary.loadFailed') + ': ' + err.message);
  }
}

// renderCanaryView 渲染灰度发布视图（列表 + 分割面板 + 指标）。
function renderCanaryView(content, releases) {
  content.innerHTML = '';
  // 上半：列表
  const listHost = render.el('div', { class: 'content', style: { marginBottom: '1rem' } });
  render.renderCanaryList(listHost, releases, {
    onSelect: (id) => selectCanary(id),
  });
  content.appendChild(listHost);
  // 下半：分割面板 + 指标
  const selected = releases.find((r) => r.id === state.selectedCanaryId) || releases[0];
  if (selected) {
    state.selectedCanaryId = selected.id;
    const splitHost = render.el('div', { class: 'content', style: { marginBottom: '1rem' } });
    render.renderCanarySplitPanel(splitHost, selected, {
      onApply: (percent) => applyTrafficSplit(selected.id, percent),
    });
    content.appendChild(splitHost);
    // 指标区
    const metricsHost = render.el('div', { class: 'content' });
    render.renderLoading(metricsHost);
    content.appendChild(metricsHost);
    api.getCanaryMetrics(selected.id)
      .then((metrics) => render.renderCanaryMetrics(metricsHost, metrics))
      .catch((err) => render.renderError(metricsHost, t('canary.metricsLoadFailed') + ': ' + err.message));
  }
}

// selectCanary 选中某个灰度发布，重新渲染视图。
function selectCanary(id) {
  state.selectedCanaryId = id;
  renderCanaryView(canaryContent(), state.canaryReleases);
}

// applyTrafficSplit 应用流量分割。
export async function applyTrafficSplit(id, percent) {
  try {
    await api.setTrafficSplit(id, percent);
    render.renderToast(t('canary.splitApplied') + ': ' + percent + '%', 'success');
    // 更新本地状态中的 percent
    const r = state.canaryReleases.find((x) => x.id === id);
    if (r) r.percent = percent;
    renderCanaryView(canaryContent(), state.canaryReleases);
  } catch (err) {
    render.renderToast(t('canary.splitFailed') + ': ' + err.message, 'error');
  }
}

// buildCanaryToolbar 构建灰度发布工具栏。
export function buildCanaryToolbar() {
  const toolbar = $('canary-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => loadCanaryReleases() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}

