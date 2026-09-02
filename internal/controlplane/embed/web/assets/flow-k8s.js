// flow-k8s.js — K8s 集群管理编排（P1 补齐功能域）。

// flow 子模块 — K8s 集群管理（集群列表 / 添加 / 测试连接）。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, $, pageRoot } from './flow-state.js';

// ============================================================================
// K8s 集群管理
// ============================================================================

function k8sContent() { return $('k8s-content'); }

// loadK8sClusters 加载 K8s 集群列表。
export async function loadK8sClusters() {
  const content = k8sContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const clusters = await api.getK8sClusters();
    state.k8s.clusters = clusters;
    render.renderK8sClustersTable(content, clusters, {
      onTest: (id) => testK8sCluster(id),
    });
  } catch (err) {
    render.renderError(content, t('k8s.loadFailed') + ': ' + err.message);
  }
}

// showK8sClusterForm 打开添加 K8s 集群表单。
export function showK8sClusterForm() {
  const content = k8sContent();
  if (!content) return;
  render.renderK8sClusterForm(content, {
    onSubmit: async (data) => {
      if (!data.name || !data.name.trim()) {
        render.renderToast(t('k8s.nameRequired'), 'warn');
        return;
      }
      if (!data.server || !data.server.trim()) {
        render.renderToast(t('k8s.serverRequired'), 'warn');
        return;
      }
      if (!data.kubeconfig || !data.kubeconfig.trim()) {
        render.renderToast(t('k8s.kubeconfigRequired'), 'warn');
        return;
      }
      try {
        await api.createK8sCluster(data);
        render.renderToast(t('k8s.created'), 'success');
        loadK8sClusters();
      } catch (err) {
        render.renderToast(t('k8s.createFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => loadK8sClusters(),
  });
}

// testK8sCluster 测试 K8s 集群连接。
export async function testK8sCluster(id) {
  const content = k8sContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const result = await api.testK8sCluster(id);
    render.renderK8sTestResult(content, id, result, {
      onBack: () => loadK8sClusters(),
    });
  } catch (err) {
    render.renderError(content, t('k8s.testFailed') + ': ' + err.message);
  }
}

// buildK8sToolbar 构建 K8s 工具栏（添加按钮 + 刷新）。
export function buildK8sToolbar() {
  const toolbar = $('k8s-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  // 添加按钮
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-primary', onclick: () => showK8sClusterForm() },
      iconEl('plus', 16), render.el('span', { text: t('k8s.create') })
    )
  );
  // 刷新
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => loadK8sClusters() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}