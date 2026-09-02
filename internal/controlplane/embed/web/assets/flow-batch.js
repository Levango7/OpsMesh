// flow-batch.js — 批量执行编排（P0 补齐功能域）。

// flow 子模块 — 批量执行（批量任务下发 / 状态查询）。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, $ } from './flow-state.js';

// ============================================================================
// 批量执行
// ============================================================================

function batchContent() { return $('batch-content'); }

// loadBatch 加载批量执行默认页（按当前子 tab）。
export function loadBatch() {
  refreshBatchSubTab();
}

// showBatchExecForm 打开批量执行表单。
export function showBatchExecForm() {
  const content = batchContent();
  if (!content) return;
  render.renderBatchExecForm(content, {
    onSubmit: async (data) => {
      if (!data.devices || !data.devices.length) {
        render.renderToast(t('batch.devicesRequired'), 'warn');
        return;
      }
      try {
        const result = await api.batchExec(data);
        render.renderToast(t('batch.execSubmitted'), 'success');
        // 提交后跳转到状态视图
        const bid = result && (result.id || result.batchID || result.batchId);
        if (bid) {
          state.batchSelectedId = bid;
          state.batchSubTab = 'status';
          buildBatchToolbar();
          showBatchStatus(bid);
        } else {
          refreshBatchSubTab();
        }
      } catch (err) {
        render.renderToast(t('batch.execFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => refreshBatchSubTab(),
  });
}

// showBatchCreateForm 打开批量任务创建表单。
export function showBatchCreateForm() {
  const content = batchContent();
  if (!content) return;
  render.renderBatchCreateForm(content, {
    onSubmit: async (data) => {
      if (!data.name || !data.name.trim()) {
        render.renderToast(t('batch.nameRequired'), 'warn');
        return;
      }
      if (!data.devices || !data.devices.length) {
        render.renderToast(t('batch.devicesRequired'), 'warn');
        return;
      }
      try {
        const result = await api.batchCreate(data);
        render.renderToast(t('batch.created'), 'success');
        const bid = result && (result.id || result.batchID || result.batchId);
        if (bid) {
          state.batchSelectedId = bid;
          state.batchSubTab = 'status';
          buildBatchToolbar();
          showBatchStatus(bid);
        } else {
          refreshBatchSubTab();
        }
      } catch (err) {
        render.renderToast(t('batch.createFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => refreshBatchSubTab(),
  });
}

// showBatchStatus 查看批量任务状态。
export async function showBatchStatus(id) {
  const content = batchContent();
  if (!content) return;
  const bid = id || state.batchSelectedId;
  if (!bid) {
    render.renderEmpty(content, t('batch.noSelected'));
    return;
  }
  state.batchSelectedId = bid;
  render.renderLoading(content);
  try {
    const batch = await api.getBatchStatus(bid);
    state.batchDetail = batch;
    render.renderBatchStatus(content, bid, batch, {
      onBack: () => { state.batchSubTab = 'list'; state.batchSelectedId = null; buildBatchToolbar(); loadBatchList(); },
      onRefresh: () => showBatchStatus(bid),
    });
  } catch (err) {
    render.renderError(content, t('batch.statusLoadFailed') + ': ' + err.message);
  }
}

// loadBatchList 加载批量任务列表（最近批次）。
export async function loadBatchList() {
  const content = batchContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const batches = await api.getBatchList();
    state.batches = batches;
    render.renderBatchList(content, batches, {
      onDetail: (id) => { state.batchSelectedId = id; state.batchSubTab = 'status'; buildBatchToolbar(); showBatchStatus(id); },
    });
  } catch (err) {
    render.renderError(content, t('batch.listLoadFailed') + ': ' + err.message);
  }
}

// refreshBatchSubTab 按当前子 tab 刷新批量执行页。
export function refreshBatchSubTab() {
  if (state.batchSubTab === 'exec') showBatchExecForm();
  else if (state.batchSubTab === 'status') showBatchStatus();
  else loadBatchList();
}

// buildBatchToolbar 构建批量执行工具栏（子 tab + 刷新）。
export function buildBatchToolbar() {
  const toolbar = $('batch-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  // 子 tab 切换组
  const subTabs = [
    { key: 'list', label: t('batch.tabList'), onActivate: () => loadBatchList() },
    { key: 'exec', label: t('batch.tabExec'), onActivate: () => showBatchExecForm() },
    { key: 'create', label: t('batch.tabCreate'), onActivate: () => showBatchCreateForm() },
  ];
  subTabs.forEach((st) => {
    toolbar.appendChild(
      render.el('button', {
        class: 'btn ' + (state.batchSubTab === st.key ? 'btn-secondary' : 'btn-ghost'),
        onclick: () => { state.batchSubTab = st.key; st.onActivate(); buildBatchToolbar(); },
      }, render.el('span', { text: st.label }))
    );
  });
  // 刷新
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => refreshBatchSubTab() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}
