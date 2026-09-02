// flow-secrets.js — 密钥管理编排（P3 补齐功能域）。

// flow 子模块 — 密钥管理（后端状态展示 + 测试按钮 + 密钥列表）。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, $, pageRoot } from './flow-state.js';

// ============================================================================
// 密钥管理
// ============================================================================

function secretsContent() { return $('secrets-content'); }

// renderSecretsPanel 渲染密钥管理主面板（状态 + 密钥列表）。
function renderSecretsPanel(content) {
  content.innerHTML = '';
  // 子面板 1：后端状态
  const statusCard = render.el('div', { class: 'detail-card' });
  const statusBody = render.el('div', { class: 'card-body' });
  render.renderSecretsStatus(statusBody, state.secrets.status);
  statusCard.appendChild(statusBody);
  content.appendChild(statusCard);
  // 子面板 2：密钥列表
  const keysCard = render.el('div', { class: 'detail-card' });
  keysCard.appendChild(render.el('h3', { class: 'detail-title', text: t('secrets.keysTitle') }));
  const keysBody = render.el('div', { class: 'card-body' });
  render.renderSecretKeysTable(keysBody, state.secrets.keys);
  keysCard.appendChild(keysBody);
  content.appendChild(keysCard);
}

// loadSecretsStatus 加载密钥后端状态。
export async function loadSecretsStatus() {
  const content = secretsContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const data = await api.getSecretsStatus();
    state.secrets.status = data;
    renderSecretsPanel(content);
  } catch (err) {
    render.renderError(content, t('secrets.statusLoadFailed') + ': ' + err.message);
  }
}

// loadSecretKeys 加载密钥列表。
export async function loadSecretKeys() {
  const content = secretsContent();
  if (!content) return;
  try {
    const data = await api.getSecretKeys();
    const list = (data && data.keys) ? data.keys : (Array.isArray(data) ? data : []);
    state.secrets.keys = list;
    renderSecretsPanel(content);
  } catch (err) {
    render.renderToast(t('secrets.keysLoadFailed') + ': ' + err.message, 'error');
  }
}

// testSecrets 测试密钥后端连通性。
export async function testSecrets() {
  const content = secretsContent();
  if (!content) return;
  try {
    render.renderToast(t('secrets.testing'), 'info');
    const result = await api.testSecrets();
    const ok = String(result && result.status || '').toLowerCase() === 'ok';
    render.renderToast(ok ? t('secrets.testOk') : t('secrets.testFailed'), ok ? 'success' : 'error');
    // 在状态卡片下方显示测试结果
    renderSecretsTestResultInline(content, result);
  } catch (err) {
    render.renderToast(t('secrets.testFailed') + ': ' + err.message, 'error');
  }
}

// renderSecretsTestResultInline 在面板内显示测试结果。
function renderSecretsTestResultInline(content, result) {
  // 在现有面板后追加测试结果卡片
  const existing = content.querySelector('.secrets-test-result');
  if (existing) existing.remove();
  const card = render.el('div', { class: 'detail-card secrets-test-result' });
  const body = render.el('div', { class: 'card-body' });
  render.renderSecretsTestResult(body, result);
  card.appendChild(body);
  content.appendChild(card);
}

// loadSecretsAll 加载密钥管理全部数据（状态 + 密钥列表）。
export async function loadSecretsAll() {
  const content = secretsContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const [statusData, keysData] = await Promise.all([
      api.getSecretsStatus(),
      api.getSecretKeys(),
    ]);
    state.secrets.status = statusData;
    state.secrets.keys = (keysData && keysData.keys) ? keysData.keys : (Array.isArray(keysData) ? keysData : []);
    renderSecretsPanel(content);
  } catch (err) {
    render.renderError(content, t('secrets.statusLoadFailed') + ': ' + err.message);
  }
}

// refreshSecretsSubTab 刷新密钥管理页（重新加载状态 + 密钥列表）。
export function refreshSecretsSubTab() {
  loadSecretsAll();
}

// buildSecretsToolbar 构建密钥管理工具栏（测试 + 刷新）。
export function buildSecretsToolbar() {
  const toolbar = $('secrets-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  // 测试按钮
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-primary', onclick: () => testSecrets() },
      iconEl('check', 16), render.el('span', { text: t('secrets.test') })
    )
  );
  // 刷新
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => loadSecretsAll() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}