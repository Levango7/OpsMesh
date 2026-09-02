// render-k8s.js — K8s 集群管理渲染（P1 补齐功能域）。

// 渲染子模块 — K8s 集群管理（集群列表 / 添加 / 测试连接）。
// 公共依赖：i18n（t）、icons（iconEl/iconHtml）、render-common（DOM/Badge/表单辅助）。

import { t } from './i18n.js';
import { iconEl, iconHtml } from './icons.js';
import {
  el, formatTime, formatNumber, badge,
  renderLoading, renderError, renderEmpty, renderToast,
  statusBadge, priorityBadge, categoryBadge, sloStatusBadge,
  detailItem, fieldRow,
} from './render-common.js';

// ============================================================================
// K8s 集群管理渲染
// ============================================================================

// clusterStatusBadge 集群状态 badge。
function clusterStatusBadge(status) {
  const s = String(status || '').toLowerCase();
  if (s === 'healthy' || s === 'ok' || s === 'active') return badge(t('k8s.status.healthy'), 'status-resolved');
  if (s === 'unhealthy' || s === 'failed' || s === 'error') return badge(t('k8s.status.unhealthy'), 'priority-urgent');
  if (s === 'unknown' || s === 'pending') return badge(t('k8s.status.unknown'), 'priority-medium');
  return badge(status || '-', 'status-in_progress');
}

// renderK8sClustersTable 渲染 K8s 集群列表表格。
// handlers: { onTest(id) }
export function renderK8sClustersTable(container, clusters, handlers) {
  container.innerHTML = '';
  if (!clusters || clusters.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('k8s.clusterName') }),
        el('th', { text: t('k8s.server') }),
        el('th', { text: t('common.status') }),
        el('th', { text: t('common.createdAt') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      clusters.map((c) => el('tr', null,
        el('td', { class: 'mono', text: c.id }),
        el('td', { class: 'cell-title', text: c.name || '-' }),
        el('td', { class: 'mono', text: c.server || '-' }),
        el('td', null, clusterStatusBadge(c.status)),
        el('td', { class: 'mono', text: formatTime(c.createdAt) }),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn-icon', title: t('k8s.test'), onclick: () => handlers.onTest(c.id) }, iconEl('test', 14))
        )
      ))
    )
  );
  container.appendChild(table);
}

// renderK8sClusterForm 渲染添加 K8s 集群表单。
// handlers: { onSubmit(data), onCancel() }
export function renderK8sClusterForm(container, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const data = {
      name: form.elements.name.value.trim(),
      kubeconfig: form.elements.kubeconfig.value,
      server: form.elements.server.value.trim(),
    };
    handlers.onSubmit(data);
  } });
  form.appendChild(el('h3', { class: 'form-title', text: t('k8s.create') }));
  // 集群名称（必填）
  form.appendChild(fieldRow(t('k8s.clusterName'), true,
    el('input', { type: 'text', name: 'name', required: 'true', placeholder: t('k8s.namePlaceholder') })
  ));
  // server（必填）
  form.appendChild(fieldRow(t('k8s.server'), true,
    el('input', { type: 'text', name: 'server', required: 'true', placeholder: 'https://1.2.3.4:6443' })
  ));
  // kubeconfig（必填）
  form.appendChild(fieldRow(t('k8s.kubeconfig'), true,
    el('textarea', { name: 'kubeconfig', rows: '8', required: 'true', placeholder: 'apiVersion: v1\nkind: Config\n...' }, '')
  ));
  // 操作按钮
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('check', 16), el('span', { text: t('common.save') })),
    el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => handlers.onCancel() }, el('span', { text: t('common.cancel') }))
  ));
  container.appendChild(form);
}

// renderK8sTestResult 渲染测试连接结果。
// handlers: { onBack() }
export function renderK8sTestResult(container, clusterID, result, handlers) {
  container.innerHTML = '';
  const card = el('div', { class: 'detail-card' });
  card.appendChild(el('div', { class: 'detail-head' },
    el('button', { class: 'btn btn-ghost', onclick: () => handlers.onBack() }, iconEl('back', 16), el('span', { text: t('common.back') })),
    el('h3', { class: 'detail-title', text: t('k8s.testResultTitle') + ' · ' + clusterID })
  ));
  const rObj = (result && typeof result === 'object') ? result : { value: result };
  const ok = String(rObj.status || '').toLowerCase() === 'ok';
  card.appendChild(el('div', { class: 'detail-grid' },
    detailItem(t('common.status'), ok ? badge(t('k8s.test.ok'), 'status-resolved') : badge(t('k8s.test.failed'), 'priority-urgent')),
    detailItem(t('k8s.test.error'), rObj.error || '-', true)
  ));
  container.appendChild(card);
}