// flow-provision.js — 自动纳管编排（P2 补齐功能域）。

// flow 子模块 — 自动纳管（网段输入 + agent 版本 + 结果统计 + 设备列表）。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, $, pageRoot } from './flow-state.js';

// ============================================================================
// 自动纳管
// ============================================================================

function provisionContent() { return $('provision-content'); }

// showProvisionForm 打开自动纳管触发表单。
export function showProvisionForm() {
  const content = provisionContent();
  if (!content) return;
  render.renderProvisionForm(content, {
    onSubmit: async (data) => {
      if (!data.segment) {
        render.renderToast(t('provision.segmentRequired'), 'warn');
        return;
      }
      state.provision.loading = true;
      state.provision.error = null;
      try {
        const result = await api.autoProvision(data);
        state.provision.result = result;
        state.provision.loading = false;
        render.renderProvisionResult(content, result);
        render.renderToast(t('provision.done'), 'success');
      } catch (err) {
        state.provision.loading = false;
        state.provision.error = err.message;
        render.renderToast(t('provision.failed') + ': ' + err.message, 'error');
        // 失败后回到表单
        showProvisionForm();
      }
    },
    onCancel: () => showProvisionForm(),
  });
}

// showProvisionResult 显示最近一次纳管结果。
export function showProvisionResult() {
  const content = provisionContent();
  if (!content) return;
  if (!state.provision.result) {
    showProvisionForm();
    return;
  }
  render.renderProvisionResult(content, state.provision.result);
}

// refreshProvisionSubTab 按当前子 tab 刷新自动纳管页。
export function refreshProvisionSubTab() {
  if (state.provision.result) showProvisionResult();
  else showProvisionForm();
}

// buildProvisionToolbar 构建自动纳管工具栏（重新纳管 + 刷新）。
export function buildProvisionToolbar() {
  const toolbar = $('provision-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  // 重新纳管按钮
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-primary', onclick: () => showProvisionForm() },
      iconEl('plus', 16), render.el('span', { text: t('provision.start') })
    )
  );
  // 查看上次结果
  if (state.provision.result) {
    toolbar.appendChild(
      render.el('button', { class: 'btn btn-ghost', onclick: () => showProvisionResult() },
        iconEl('history', 14), render.el('span', { text: t('provision.lastResult') })
      )
    );
  }
  // 刷新
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => refreshProvisionSubTab() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}