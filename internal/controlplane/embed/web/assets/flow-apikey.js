// flow-apikey.js — API Key 管理编排（由 flow.js 拆分）。

// flow 子模块 — 由 flow.js 拆分而来。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, $ } from './flow-state.js';

function apikeyContent() { return $('apikey-content'); }

// loadAPIKeys 加载 API Key 列表。
export async function loadAPIKeys() {
  const content = apikeyContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const apikeys = await api.getAPIKeys();
    state.apikeys = apikeys;
    render.renderAPIKeyPage(content, apikeys, {
      onEdit: (k) => editAPIKey(k),
      onToggle: (k) => toggleAPIKey(k),
      onDelete: (id) => deleteAPIKey(id),
    });
  } catch (err) {
    render.renderError(content, t('apikey.loadFailed') + ': ' + err.message);
  }
}

// createAPIKey 打开创建 API Key 表单。
export function createAPIKey() {
  const content = apikeyContent();
  if (!content) return;
  render.renderAPIKeyForm(content, {
    onSubmit: async (data) => {
      if (!data.name) { render.renderToast(t('apikey.nameRequired'), 'warn'); return; }
      try {
        const result = await api.createAPIKey(data);
        render.renderToast(t('apikey.created'), 'success');
        // 显示生成的密钥
        render.renderAPIKeyGenerated(content, result, {
          onDone: () => loadAPIKeys(),
        });
      } catch (err) {
        render.renderToast(t('apikey.createFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => loadAPIKeys(),
  });
}

// editAPIKey 打开编辑 API Key 表单（先拉详情）。
export async function editAPIKey(k) {
  const content = apikeyContent();
  if (!content) return;
  const id = k && k.id;
  if (!id) { render.renderToast(t('apikey.loadFailed'), 'error'); return; }
  render.renderLoading(content);
  try {
    const key = await api.getAPIKey(id);
    // 复用表单（仅允许编辑 name/scopes；启停用 enable/disable 端点，不在此提交）
    const form = render.el('form', { class: 'form-card', onsubmit: (e) => {
      e.preventDefault();
      // 仅提交白名单可编辑字段（name/scopes）；enabled 走 enable/disable 端点。
      const scopesRaw = form.elements.scopes.value.trim();
      const scopes = scopesRaw
        ? scopesRaw.split(',').map((s) => s.trim()).filter(Boolean)
        : [];
      const data = {
        name: form.elements.name.value.trim(),
        scopes: scopes,
      };
      api.updateAPIKey(id, data)
        .then(() => { render.renderToast(t('apikey.updated'), 'success'); loadAPIKeys(); })
        .catch((err) => render.renderToast(t('apikey.updateFailed') + ': ' + err.message, 'error'));
    } });
    form.appendChild(render.el('h3', { class: 'form-title', text: t('apikey.edit') }));
    form.appendChild(render.el('div', { class: 'form-row' },
      render.el('label', { class: 'form-label required', text: t('apikey.keyName') }),
      render.el('div', { class: 'form-control' },
        render.el('input', { name: 'name', type: 'text', required: 'true', value: key.name || '' })
      )
    ));
    form.appendChild(render.el('div', { class: 'form-row' },
      render.el('label', { class: 'form-label', text: t('apikey.scopes') }),
      render.el('div', { class: 'form-control' },
        render.el('input', { name: 'scopes', type: 'text', value: Array.isArray(key.scopes) ? key.scopes.join(',') : (key.scopes || '') })
      )
    ));

    form.appendChild(render.el('div', { class: 'form-actions' },
      render.el('button', { type: 'submit', class: 'btn btn-primary' },
        iconEl('check', 16), render.el('span', { text: t('common.save') })
      ),
      render.el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => loadAPIKeys() },
        render.el('span', { text: t('common.cancel') })
      )
    ));
    content.innerHTML = '';
    content.appendChild(form);
  } catch (err) {
    render.renderError(content, t('apikey.loadFailed') + ': ' + err.message);
  }
}

// toggleAPIKey 启用/禁用 API Key。
export async function toggleAPIKey(k) {
  const id = k && k.id;
  if (!id) return;
  const enabled = String(k.status || '').toLowerCase() !== 'disabled';
  const action = enabled ? 'disable' : 'enable';
  try {
    await api.toggleAPIKey(id, action);
    render.renderToast(enabled ? t('apikey.disabled') : t('apikey.enabled'), 'success');
    loadAPIKeys();
  } catch (err) {
    render.renderToast((enabled ? t('apikey.disableFailed') : t('apikey.enableFailed')) + ': ' + err.message, 'error');
  }
}

// deleteAPIKey 删除 API Key。
export async function deleteAPIKey(id) {
  if (!window.confirm(t('apikey.confirmDelete'))) return;
  try {
    await api.deleteAPIKey(id);
    render.renderToast(t('apikey.deleted'), 'success');
    loadAPIKeys();
  } catch (err) {
    render.renderToast(t('apikey.deleteFailed') + ': ' + err.message, 'error');
  }
}

// buildAPIKeyToolbar 构建 API Key 工具栏。
export function buildAPIKeyToolbar() {
  const toolbar = $('apikey-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-primary', onclick: () => createAPIKey() },
      iconEl('plus', 16), render.el('span', { text: t('apikey.create') })
    )
  );
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => loadAPIKeys() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}

// --- 插件市场 ---

