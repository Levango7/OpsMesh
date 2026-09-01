// flow-slo.js — SLO 管理编排（由 flow.js 拆分）。

// flow 子模块 — 由 flow.js 拆分而来。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, \$, pageRoot } from './flow-state.js';

// ============================================================================
// SLO 管理
// ============================================================================

// loadSLOs 加载 SLO 列表。
export async function loadSLOs() {
  const content = sloContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const slos = await api.getSLOs();
    state.slos = slos;
    render.renderSLOTable(content, slos, {
      onDetail: (id) => showSLODetail(id),
      onDelete: (id) => deleteSLO(id),
    });
  } catch (err) {
    render.renderError(content, t('slo.loadFailed') + ': ' + err.message);
  }
}

// createSLO 打开创建 SLO 表单。
export function createSLO() {
  const content = sloContent();
  if (!content) return;
  render.renderSLOForm(content, null, {
    onSubmit: async (data) => {
      if (!data.name || !data.name.trim()) {
        render.renderToast(t('slo.nameRequired'), 'warn');
        return;
      }
      try {
        await api.createSLO(data);
        render.renderToast(t('slo.created'), 'success');
        loadSLOs();
      } catch (err) {
        render.renderToast(t('slo.createFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => loadSLOs(),
  });
}

// editSLO 打开编辑 SLO 表单（先拉详情）。
export async function editSLO(id) {
  const content = sloContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const slo = await api.getSLO(id);
    render.renderSLOForm(content, slo, {
      onSubmit: async (data) => {
        if (!data.name || !data.name.trim()) {
          render.renderToast(t('slo.nameRequired'), 'warn');
          return;
        }
        try {
          await api.updateSLO(id, data);
          render.renderToast(t('slo.updated'), 'success');
          loadSLOs();
        } catch (err) {
          render.renderToast(t('slo.updateFailed') + ': ' + err.message, 'error');
        }
      },
      onCancel: () => loadSLOs(),
    });
  } catch (err) {
    render.renderError(content, t('slo.loadFailed') + ': ' + err.message);
  }
}

// showSLODetail 查看 SLO 详情 + SLI 状态。
export async function showSLODetail(id) {
  const content = sloContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const [slo, statuses] = await Promise.all([
      api.getSLO(id),
      api.getSLOStatus(id).catch(() => []), // 状态获取失败不阻塞详情
    ]);
    render.renderSLODetail(content, slo, statuses, {
      onBack: () => loadSLOs(),
      onEdit: () => editSLO(id),
      onDelete: () => deleteSLO(id),
    });
  } catch (err) {
    render.renderError(content, t('slo.loadFailed') + ': ' + err.message);
  }
}

// deleteSLO 删除 SLO（确认后调用 API）。
export async function deleteSLO(id) {
  if (!window.confirm(t('slo.confirmDelete'))) return;
  try {
    await api.deleteSLO(id);
    render.renderToast(t('slo.deleted'), 'success');
    loadSLOs();
  } catch (err) {
    render.renderToast(t('slo.deleteFailed') + ': ' + err.message, 'error');
  }
}

// buildSLOToolbar 构建 SLO 工具栏（创建按钮 + 刷新）。
export function buildSLOToolbar() {
  const toolbar = $('slo-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-primary', onclick: () => createSLO() },
      iconEl('plus', 16), render.el('span', { text: t('slo.create') })
    )
  );
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => loadSLOs() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}

