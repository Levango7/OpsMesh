// flow-tenant.js — 租户管理编排（由 flow.js 拆分）。

// flow 子模块 — 由 flow.js 拆分而来。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, \$, pageRoot } from './flow-state.js';

// ============================================================================
// Phase 6：平台化管理（租户 / API Key / 插件市场 / 计费订阅 / 平台配置）
// ============================================================================

// --- 租户管理 ---

function tenantContent() { return $('tenant-content'); }

// loadTenants 加载租户列表。
export async function loadTenants() {
  const content = tenantContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const tenants = await api.getTenants();
    state.tenants = tenants;
    render.renderTenantPage(content, tenants, {
      onEdit: (tn) => editTenant(tn),
      onSuspend: (id) => suspendTenant(id),
      onActivate: (id) => activateTenant(id),
      onDelete: (id) => deleteTenant(id),
    });
  } catch (err) {
    render.renderError(content, t('tenant.loadFailed') + ': ' + err.message);
  }
}

// createTenant 打开创建租户表单。
export function createTenant() {
  const content = tenantContent();
  if (!content) return;
  render.renderTenantForm(content, null, {
    onSubmit: async (data) => {
      if (!data.name) { render.renderToast(t('tenant.nameRequired'), 'warn'); return; }
      if (!data.code) { render.renderToast(t('tenant.codeRequired'), 'warn'); return; }
      try {
        await api.createTenant(data);
        render.renderToast(t('tenant.created'), 'success');
        loadTenants();
      } catch (err) {
        render.renderToast(t('tenant.createFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => loadTenants(),
  });
}

// editTenant 打开编辑租户表单（先拉详情）。
export async function editTenant(tn) {
  const content = tenantContent();
  if (!content) return;
  const id = tn && tn.id;
  if (!id) { render.renderToast(t('tenant.loadFailed'), 'error'); return; }
  render.renderLoading(content);
  try {
    const tenant = await api.getTenant(id);
    render.renderTenantForm(content, tenant, {
      onSubmit: async (data) => {
        if (!data.name) { render.renderToast(t('tenant.nameRequired'), 'warn'); return; }
        try {
          await api.updateTenant(id, data);
          render.renderToast(t('tenant.updated'), 'success');
          loadTenants();
        } catch (err) {
          render.renderToast(t('tenant.updateFailed') + ': ' + err.message, 'error');
        }
      },
      onCancel: () => loadTenants(),
    });
  } catch (err) {
    render.renderError(content, t('tenant.loadFailed') + ': ' + err.message);
  }
}

// suspendTenant 暂停租户。
export async function suspendTenant(id) {
  if (!window.confirm(t('tenant.confirmSuspend'))) return;
  try {
    await api.suspendTenant(id);
    render.renderToast(t('tenant.suspendDone'), 'success');
    loadTenants();
  } catch (err) {
    render.renderToast(t('tenant.suspendFailed') + ': ' + err.message, 'error');
  }
}

// activateTenant 激活租户。
export async function activateTenant(id) {
  try {
    await api.activateTenant(id);
    render.renderToast(t('tenant.activateDone'), 'success');
    loadTenants();
  } catch (err) {
    render.renderToast(t('tenant.activateFailed') + ': ' + err.message, 'error');
  }
}

// deleteTenant 删除租户。
export async function deleteTenant(id) {
  if (!window.confirm(t('tenant.confirmDelete'))) return;
  try {
    await api.deleteTenant(id);
    render.renderToast(t('tenant.deleted'), 'success');
    loadTenants();
  } catch (err) {
    render.renderToast(t('tenant.deleteFailed') + ': ' + err.message, 'error');
  }
}

// buildTenantToolbar 构建租户工具栏。
export function buildTenantToolbar() {
  const toolbar = $('tenant-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-primary', onclick: () => createTenant() },
      iconEl('plus', 16), render.el('span', { text: t('tenant.create') })
    )
  );
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => loadTenants() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}

// --- API Key 管理 ---

