// render-pipeline.js — CI/CD 流水线渲染（由 render.js 拆分）。

// 渲染子模块 — 由 render.js 拆分而来。
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
// Phase 2：CI/CD 流水线渲染
// ============================================================================

// renderPipelineTemplates 渲染流水线模板列表表格。
// handlers: { onRun(id), onDelete(id) }
export function renderPipelineTemplates(container, templates, handlers) {
  container.innerHTML = '';
  if (!templates || templates.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('pipeline.templateName') }),
        el('th', { text: t('pipeline.description') }),
        el('th', { text: t('pipeline.stages') }),
        el('th', { text: t('common.createdAt') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      templates.map((tp) => el('tr', null,
        el('td', { class: 'mono', text: tp.id }),
        el('td', { class: 'cell-title', text: tp.name }),
        el('td', { text: tp.description || '-' }),
        el('td', { class: 'mono', text: String((tp.stages && tp.stages.length) || 0) }),
        el('td', { class: 'mono', text: formatTime(tp.createdAt) }),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn-icon', title: t('pipeline.run'), onclick: () => handlers.onRun(tp.id) }, iconEl('play', 14)),
          el('button', { class: 'btn-icon btn-icon-danger', title: t('common.delete'), onclick: () => handlers.onDelete(tp.id) }, iconEl('trash', 14))
        )
      ))
    )
  );
  container.appendChild(table);
}

// renderPipelineRuns 渲染流水线运行记录表格。
export function renderPipelineRuns(container, runs) {
  container.innerHTML = '';
  if (!runs || runs.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('pipeline.runId') }),
        el('th', { text: t('pipeline.template') }),
        el('th', { text: t('pipeline.status') }),
        el('th', { text: t('pipeline.startedAt') }),
        el('th', { text: t('pipeline.finishedAt') })
      )
    ),
    el('tbody', null,
      runs.map((r) => el('tr', null,
        el('td', { class: 'mono', text: r.id }),
        el('td', { class: 'cell-title', text: r.templateName || r.templateID || '-' }),
        el('td', null, el('span', { class: 'badge badge-status-' + (r.status || 'open'), text: r.status || '-' })),
        el('td', { class: 'mono', text: formatTime(r.startedAt || r.createdAt) }),
        el('td', { class: 'mono', text: formatTime(r.finishedAt || r.updatedAt) })
      ))
    )
  );
  container.appendChild(table);
}

// renderPipelineTemplateForm 渲染流水线模板创建表单。
// handlers: { onSubmit(data), onCancel() }
export function renderPipelineTemplateForm(container, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => { e.preventDefault(); handlers.onSubmit(collectPipelineTemplateForm(form)); } });

  form.appendChild(el('h3', { class: 'form-title', text: t('pipeline.create') }));

  form.appendChild(fieldRow(t('pipeline.templateName'), true,
    el('input', { type: 'text', name: 'name', required: 'true', placeholder: t('pipeline.nameRequired') })
  ));
  form.appendChild(fieldRow(t('pipeline.description'), false,
    el('textarea', { name: 'description', rows: '2' }, '')
  ));
  form.appendChild(fieldRow(t('pipeline.stages'), false,
    el('textarea', { name: 'stages', rows: '4', placeholder: 'build -> test -> deploy' }, '')
  ));

  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('check', 16), el('span', { text: t('common.save') })),
    el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => handlers.onCancel() }, el('span', { text: t('common.cancel') }))
  ));

  container.appendChild(form);
}

function collectPipelineTemplateForm(form) {
  const get = (name) => (form.elements[name] && form.elements[name].value) || '';
  const stagesText = get('stages').trim();
  const stages = stagesText ? stagesText.split('->').map((s) => s.trim()).filter(Boolean) : [];
  return {
    name: get('name').trim(),
    description: get('description'),
    stages,
  };
}

// renderArgoCDApps 渲染 ArgoCD 应用列表。
// handlers: { onSync(id), onDelete(id) }
export function renderArgoCDApps(container, apps, handlers) {
  container.innerHTML = '';
  if (!apps || apps.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.name') }),
        el('th', { text: t('common.status') }),
        el('th', { text: 'repo' }),
        el('th', { text: 'target' }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      apps.map((a) => el('tr', null,
        el('td', { class: 'cell-title', text: a.name }),
        el('td', null, el('span', { class: 'badge badge-status-' + (a.syncStatus === 'Synced' ? 'resolved' : 'in_progress'), text: a.syncStatus || '-' })),
        el('td', { class: 'mono', text: a.repoURL || '-' }),
        el('td', { class: 'mono', text: a.targetRevision || '-' }),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn-icon', title: t('pipeline.argoSync'), onclick: () => handlers.onSync(a.name || a.id) }, iconEl('sync', 14)),
          el('button', { class: 'btn-icon btn-icon-danger', title: t('common.delete'), onclick: () => handlers.onDelete(a.name || a.id) }, iconEl('trash', 14))
        )
      ))
    )
  );
  container.appendChild(table);
}

