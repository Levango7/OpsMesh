// render-plugin.js — 插件市场渲染（由 render.js 拆分）。

// 渲染子模块 — 由 render.js 拆分而来。
// 公共依赖：i18n（t）、icons（iconEl/iconHtml）、render-common（DOM/Badge/表单辅助）。

import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { el, renderEmpty, fieldRow, pluginInstallBadge, apiKeyStatusBadge } from './render-common.js';

export function renderPluginPage(container, plugins, handlers) {
  container.innerHTML = '';
  if (!plugins || !plugins.length) { renderEmpty(container, t('plugin.list')); return; }
  container.appendChild(el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('plugin.pluginName') }),
        el('th', { text: t('plugin.version') }),
        el('th', { text: t('plugin.source') }),
        el('th', { text: t('plugin.installStatus') }),
        el('th', { text: t('common.status') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      plugins.map((p) => {
        const installed = String(p.installStatus || p.installed || '').toLowerCase() === 'installed';
        const enabled = String(p.status || '').toLowerCase() !== 'disabled';
        return el('tr', null,
          el('td', { class: 'mono', text: p.id }),
          el('td', { class: 'cell-title', text: p.name }),
          el('td', { class: 'mono', text: p.version || '-' }),
          el('td', { class: 'mono', text: p.source || '-' }),
          el('td', null, pluginInstallBadge(p.installStatus || (installed ? 'installed' : 'notinstalled'))),
          el('td', null, apiKeyStatusBadge(p.status)),
          el('td', { class: 'td-actions' },
            installed
              ? el('button', { class: 'btn btn-ghost', title: t('plugin.uninstall'), onclick: () => handlers.onUninstall && handlers.onUninstall(p.id) },
                  iconEl('trash', 14))
              : el('button', { class: 'btn btn-ghost', title: t('plugin.install'), onclick: () => handlers.onInstall && handlers.onInstall(p.id) },
                  iconEl('download', 14)),
            installed
              ? (enabled
                  ? el('button', { class: 'btn-icon', title: t('common.disable'), onclick: () => handlers.onToggle && handlers.onToggle(p) },
                      iconEl('toggle_on', 14))
                  : el('button', { class: 'btn-icon', title: t('common.enable'), onclick: () => handlers.onToggle && handlers.onToggle(p) },
                      iconEl('toggle_off', 14)))
              : null,
            el('button', { class: 'btn-icon btn-icon-danger', title: t('common.delete'), onclick: () => handlers.onDelete && handlers.onDelete(p.id) },
              iconEl('trash', 14))
          )
        );
      })
    )
  ));
}

// renderPluginForm 渲染插件注册表单。
// handlers: { onSubmit(data), onCancel() }
export function renderPluginForm(container, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const data = {
      name: form.elements.name.value.trim(),
      version: form.elements.version.value.trim(),
      source: form.elements.source.value.trim(),
      description: form.elements.description.value.trim(),
    };
    handlers.onSubmit && handlers.onSubmit(data);
  } });
  form.appendChild(el('h3', { class: 'form-title', text: t('plugin.register') }));
  form.appendChild(fieldRow(t('plugin.pluginName'), true,
    el('input', { name: 'name', type: 'text', required: 'true', placeholder: t('plugin.nameRequired') })
  ));
  form.appendChild(fieldRow(t('plugin.version'), false,
    el('input', { name: 'version', type: 'text', placeholder: '1.0.0' })
  ));
  form.appendChild(fieldRow(t('plugin.source'), false,
    el('input', { name: 'source', type: 'text', placeholder: 'registry / git url' })
  ));
  form.appendChild(fieldRow(t('common.description'), false,
    el('input', { name: 'description', type: 'text', placeholder: t('common.description') })
  ));
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' },
      iconEl('check', 16), el('span', { text: t('common.save') })
    ),
    el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => handlers.onCancel && handlers.onCancel() },
      el('span', { text: t('common.cancel') })
    )
  ));
  container.appendChild(form);
}

// --- 计费订阅 ---

// renderBillingPage 渲染计费页（计划 / 订阅 / 账单 三子区域）。
// handlers: { onCreatePlan(), onEditPlan(p), onDeletePlan(id), onCreateSub(), onEditSub(s), onDeleteSub(id) }
