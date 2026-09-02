// render-middleware.js — 中间件部署渲染（P1 补齐功能域）。

// 渲染子模块 — 中间件部署（模板列表 / 部署表单 / 实例列表）。
// 公共依赖：i18n（t）、icons（iconEl/iconHtml）、render-common（DOM/Badge/表单辅助）。

import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { el, formatTime, renderEmpty, statusBadge, fieldRow } from './render-common.js';

// ============================================================================
// 中间件部署渲染
// ============================================================================

// renderMiddlewareTemplatesTable 渲染中间件模板列表表格。
// handlers: { onDeploy(id) }
export function renderMiddlewareTemplatesTable(container, templates, handlers) {
  container.innerHTML = '';
  if (!templates || templates.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('middleware.templateName') }),
        el('th', { text: t('middleware.type') }),
        el('th', { text: t('middleware.deployMode') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      templates.map((tp) => el('tr', null,
        el('td', { class: 'mono', text: tp.id }),
        el('td', { class: 'cell-title', text: tp.name || '-' }),
        el('td', { text: tp.type || '-' }),
        el('td', { text: tp.deployMode || tp.mode || '-' }),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn-icon', title: t('middleware.deploy'), onclick: () => handlers.onDeploy(tp.id) }, iconEl('rocket', 14))
        )
      ))
    )
  );
  container.appendChild(table);
}

// renderMiddlewareDeployForm 渲染中间件部署表单。
// handlers: { onSubmit(data), onCancel() }
export function renderMiddlewareDeployForm(container, templateID, template, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const data = {
      agentID: form.elements.agentID.value.trim(),
      params: parseParams(form.elements.params.value),
      mode: form.elements.mode.value,
    };
    handlers.onSubmit(data);
  } });
  const name = template ? (template.name || templateID) : templateID;
  form.appendChild(el('h3', { class: 'form-title', text: t('middleware.deployTitle') + ' · ' + name }));
  // 目标 agent（必填）
  form.appendChild(fieldRow(t('middleware.agentID'), true,
    el('input', { type: 'text', name: 'agentID', required: 'true', placeholder: 'agent-id' })
  ));
  // 部署方式
  form.appendChild(fieldRow(t('middleware.deployMode'), true,
    el('select', { name: 'mode' },
      ['standalone', 'cluster', 'replica'].map((m) =>
        el('option', { value: m, text: t('middleware.mode.' + m) })
      )
    )
  ));
  // 参数（JSON）
  form.appendChild(fieldRow(t('middleware.params'), false,
    el('textarea', { name: 'params', rows: '4', placeholder: '{"key":"value"}' }, '')
  ));
  // 操作按钮
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('rocket', 16), el('span', { text: t('middleware.deploy') })),
    el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => handlers.onCancel() }, el('span', { text: t('common.cancel') }))
  ));
  container.appendChild(form);
}

// parseParams 解析参数 JSON。
function parseParams(text) {
  const raw = (text || '').trim();
  if (!raw) return null;
  try { return JSON.parse(raw); } catch (_) { return raw; }
}

// renderMiddlewareInstancesTable 渲染中间件实例列表表格。
export function renderMiddlewareInstancesTable(container, instances) {
  container.innerHTML = '';
  if (!instances || instances.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('middleware.instanceName') }),
        el('th', { text: t('middleware.type') }),
        el('th', { text: t('common.status') }),
        el('th', { text: t('middleware.agentID') }),
        el('th', { text: t('common.createdAt') })
      )
    ),
    el('tbody', null,
      instances.map((ins) => el('tr', null,
        el('td', { class: 'mono', text: ins.id }),
        el('td', { class: 'cell-title', text: ins.name || '-' }),
        el('td', { text: ins.type || '-' }),
        el('td', null, statusBadge(ins.status || 'active')),
        el('td', { class: 'mono', text: ins.agentID || ins.agentId || '-' }),
        el('td', { class: 'mono', text: formatTime(ins.createdAt) })
      ))
    )
  );
  container.appendChild(table);
}
