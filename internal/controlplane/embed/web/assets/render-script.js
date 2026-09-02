// render-script.js — 自定义脚本渲染（由 render.js 拆分）。

// 渲染子模块 — 由 render.js 拆分而来。
// 公共依赖：i18n（t）、icons（iconEl/iconHtml）、render-common（DOM/Badge/表单辅助）。

import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { el, formatTime, badge, renderEmpty, fieldRow, scriptStatusBadge } from './render-common.js';

export function renderScriptsTable(container, scripts, handlers) {
  container.innerHTML = '';
  if (!scripts || !scripts.length) { renderEmpty(container, t('script.noScripts')); return; }
  container.appendChild(el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('script.name') }),
        el('th', { text: t('script.runtime') }),
        el('th', { text: t('script.desc2') }),
        el('th', { text: t('script.status') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      scripts.map((s) => el('tr', null,
        el('td', { class: 'cell-title', text: s.name || s.id || '-' }),
        el('td', { class: 'mono', text: s.runtime || s.language || '-' }),
        el('td', { text: s.description || '-' }),
        el('td', null, scriptStatusBadge(s.status)),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn btn-ghost', title: t('common.edit'), onclick: () => handlers.onEdit && handlers.onEdit(s) },
            iconEl('edit', 14)
          ),
          el('button', { class: 'btn btn-ghost', title: t('script.execute'), onclick: () => handlers.onExecute && handlers.onExecute(s) },
            iconEl('execute', 14)
          ),
          el('button', { class: 'btn btn-ghost', title: t('script.showExecutions'), onclick: () => handlers.onExecutions && handlers.onExecutions(s) },
            iconEl('history', 14)
          ),
          el('button', { class: 'btn btn-ghost btn-icon-danger', title: t('common.delete'), onclick: () => handlers.onDelete && handlers.onDelete(s) },
            iconEl('trash', 14)
          )
        )
      ))
    )
  ));
}

// renderScriptForm 渲染创建/编辑脚本表单（含代码编辑区）。
// script: 编辑时传入现有脚本，创建时传 null；handlers: { onSubmit(data) }
export function renderScriptForm(container, script, handlers) {
  container.innerHTML = '';
  const isEdit = !!script;
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const data = {
      name: form.elements.name.value.trim(),
      runtime: form.elements.runtime.value.trim(),
      description: form.elements.description.value.trim(),
      code: form.elements.code.value,
    };
    handlers.onSubmit && handlers.onSubmit(data);
  } });
  form.appendChild(el('h3', { class: 'form-title', text: isEdit ? t('script.edit') : t('script.create') }));
  form.appendChild(fieldRow(t('script.name'), true,
    el('input', { name: 'name', type: 'text', required: 'true', value: (script && script.name) || '', placeholder: 'check-disk-usage' })
  ));
  form.appendChild(fieldRow(t('script.runtime'), true,
    el('input', { name: 'runtime', type: 'text', required: 'true', value: (script && (script.runtime || script.language)) || 'python3', placeholder: t('script.runtimePlaceholder') })
  ));
  form.appendChild(fieldRow(t('script.desc2'), false,
    el('input', { name: 'description', type: 'text', value: (script && script.description) || '', placeholder: t('script.desc2') })
  ));
  form.appendChild(fieldRow(t('script.code'), true,
    el('textarea', { name: 'code', rows: '10', required: 'true', placeholder: t('script.codePlaceholder'), style: { width: '100%', fontFamily: 'monospace', fontSize: '.85rem' } }, (script && script.code) || '')
  ));
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' },
      iconEl('check', 16), el('span', { text: isEdit ? t('script.edit') : t('script.create') })
    )
  ));
  container.appendChild(form);
}

// renderScriptExecuteForm 渲染脚本执行表单（输入 deviceId + params）。
// handlers: { onExecute(deviceId, params) }
export function renderScriptExecuteForm(container, script, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const deviceId = form.elements.deviceId.value.trim();
    let params = {};
    try { params = JSON.parse(form.elements.params.value || '{}'); } catch (_) { params = {}; }
    handlers.onExecute && handlers.onExecute(deviceId, params);
  } });
  form.appendChild(el('h3', { class: 'form-title', text: t('script.executeOnDevice') + (script && script.name ? ' · ' + script.name : '') }));
  form.appendChild(fieldRow(t('script.execDeviceId'), true,
    el('input', { name: 'deviceId', type: 'text', required: 'true', placeholder: 'device-001' })
  ));
  form.appendChild(fieldRow(t('script.execParams'), false,
    el('textarea', { name: 'params', rows: '4', placeholder: t('script.execParamsPlaceholder'), style: { width: '100%', fontFamily: 'monospace', fontSize: '.85rem' } }, '{}')
  ));
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' },
      iconEl('execute', 16), el('span', { text: t('script.execute') })
    )
  ));
  container.appendChild(form);
}

// renderScriptExecutionsTable 渲染脚本执行历史表格。
// handlers: { onDetail(exec) }
export function renderScriptExecutionsTable(container, execs, handlers) {
  container.innerHTML = '';
  if (!execs || !execs.length) { renderEmpty(container, t('script.noExecutions')); return; }
  container.appendChild(el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('script.execId') }),
        el('th', { text: t('script.execStatus') }),
        el('th', { text: t('script.execTime') }),
        el('th', { text: t('script.execDuration') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      execs.map((e) => {
        const s = String(e.status || '').toLowerCase();
        const statusCls = s === 'success' || s === 'succeeded' ? 'badge-status-resolved'
          : s === 'failed' || s === 'error' ? 'badge-priority-urgent'
          : s === 'running' ? 'badge-status-in_progress'
          : 'badge-status-closed';
        return el('tr', null,
          el('td', { class: 'cell-title mono', text: e.id || e.executionID || '-' }),
          el('td', null, badge(e.status || '-', statusCls)),
          el('td', { text: formatTime(e.createdAt || e.time || e.startedAt) }),
          el('td', { text: e.duration != null ? (e.duration + 'ms') : '-' }),
          el('td', { class: 'td-actions' },
            el('button', { class: 'btn btn-ghost', title: t('script.execOutput'), onclick: () => handlers.onDetail && handlers.onDetail(e) },
              iconEl('search', 14)
            )
          )
        );
      })
    )
  ));
}

// renderScriptExecutionDetail 渲染脚本执行详情。
export function renderScriptExecutionDetail(container, exec) {
  container.innerHTML = '';
  if (!exec) { renderEmpty(container); return; }
  const card = el('div', { class: 'content' });
  card.appendChild(el('h3', { class: 'form-title', text: t('script.execOutput') }));
  card.appendChild(el('div', { class: 'form-row' },
    el('label', { class: 'form-label', text: t('script.execId') }),
    el('div', { class: 'form-control mono', text: exec.id || exec.executionID || '-' })
  ));
  card.appendChild(el('div', { class: 'form-row' },
    el('label', { class: 'form-label', text: t('script.execStatus') }),
    el('div', { class: 'form-control' }, badge(exec.status || '-', 'badge-status-in_progress'))
  ));
  if (exec.output != null) {
    card.appendChild(el('div', { class: 'form-row' },
      el('label', { class: 'form-label', text: t('script.execOutput') }),
      el('pre', { class: 'form-control', style: { whiteSpace: 'pre-wrap', fontFamily: 'monospace', fontSize: '.85rem' }, text: String(exec.output) })
    ));
  }
  if (exec.error != null && exec.error !== '') {
    card.appendChild(el('div', { class: 'form-row' },
      el('label', { class: 'form-label', text: 'Error' }),
      el('pre', { class: 'form-control', style: { whiteSpace: 'pre-wrap', fontFamily: 'monospace', fontSize: '.85rem', color: 'var(--danger, #c0392b)' }, text: String(exec.error) })
    ));
  }
  container.appendChild(card);
}
