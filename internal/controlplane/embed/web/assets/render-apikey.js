// render-apikey.js — API Key 管理渲染（由 render.js 拆分）。

// 渲染子模块 — 由 render.js 拆分而来。
// 公共依赖：i18n（t）、icons（iconEl/iconHtml）、render-common（DOM/Badge/表单辅助）。

import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { el, formatTime, renderEmpty, renderToast, fieldRow, apiKeyStatusBadge } from './render-common.js';

export function renderAPIKeyPage(container, apikeys, handlers) {
  container.innerHTML = '';
  if (!apikeys || !apikeys.length) { renderEmpty(container, t('apikey.list')); return; }
  container.appendChild(el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('apikey.keyName') }),
        el('th', { text: t('apikey.keyPrefix') }),
        el('th', { text: t('apikey.scopes') }),
        el('th', { text: t('common.status') }),
        el('th', { text: t('apikey.expiresAt') }),
        el('th', { text: t('apikey.lastUsedAt') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      apikeys.map((k) => {
        const enabled = String(k.status || '').toLowerCase() !== 'disabled';
        return el('tr', null,
          el('td', { class: 'mono', text: k.id }),
          el('td', { class: 'cell-title', text: k.name }),
          el('td', { class: 'mono', text: k.prefix || (k.key ? String(k.key).slice(0, 8) + '…' : '-') }),
          el('td', { class: 'mono', text: Array.isArray(k.scopes) ? k.scopes.join(',') : (k.scopes || '-') }),
          el('td', null, apiKeyStatusBadge(k.status)),
          el('td', { class: 'mono', text: formatTime(k.expiresAt) }),
          el('td', { class: 'mono', text: formatTime(k.lastUsedAt) }),
          el('td', { class: 'td-actions' },
            el('button', { class: 'btn btn-ghost', title: t('common.edit'), onclick: () => handlers.onEdit && handlers.onEdit(k) },
              iconEl('edit', 14)
            ),
            enabled
              ? el('button', { class: 'btn-icon', title: t('common.disable'), onclick: () => handlers.onToggle && handlers.onToggle(k) },
                  iconEl('toggle_on', 14))
              : el('button', { class: 'btn-icon', title: t('common.enable'), onclick: () => handlers.onToggle && handlers.onToggle(k) },
                  iconEl('toggle_off', 14)),
            el('button', { class: 'btn-icon btn-icon-danger', title: t('common.delete'), onclick: () => handlers.onDelete && handlers.onDelete(k.id) },
              iconEl('trash', 14))
          )
        );
      })
    )
  ));
}

// renderAPIKeyForm 渲染创建 API Key 表单。
// handlers: { onSubmit(data), onCancel() }
export function renderAPIKeyForm(container, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => {
    e.preventDefault();
    const data = {
      name: form.elements.name.value.trim(),
      scopes: form.elements.scopes.value.trim(),
      expiresAt: form.elements.expiresAt.value.trim(),
    };
    handlers.onSubmit && handlers.onSubmit(data);
  } });
  form.appendChild(el('h3', { class: 'form-title', text: t('apikey.create') }));
  form.appendChild(fieldRow(t('apikey.keyName'), true,
    el('input', { name: 'name', type: 'text', required: 'true', placeholder: t('apikey.nameRequired') })
  ));
  form.appendChild(fieldRow(t('apikey.scopes'), false,
    el('input', { name: 'scopes', type: 'text', placeholder: 'read,write,admin' })
  ));
  form.appendChild(fieldRow(t('apikey.expiresAt'), false,
    el('input', { name: 'expiresAt', type: 'datetime-local' })
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

// renderAPIKeyGenerated 渲染创建后生成的密钥展示（含复制按钮）。
// handlers: { onDone() }
export function renderAPIKeyGenerated(container, key, handlers) {
  container.innerHTML = '';
  const card = el('div', { class: 'form-card' });
  card.appendChild(el('h3', { class: 'form-title', text: t('apikey.generatedKey') }));
  card.appendChild(el('p', { class: 'metrics-hint', text: t('apikey.generatedHint') }));
  const keyValue = (key && (key.key || key.apiKey || key.secret)) || String(key || '');
  card.appendChild(el('div', { class: 'form-row' },
    el('label', { class: 'form-label', text: t('apikey.generatedKey') }),
    el('div', { class: 'form-control' },
      el('pre', { style: { whiteSpace: 'pre-wrap', fontFamily: 'monospace', fontSize: '.85rem', margin: '0 0 .5rem' }, text: keyValue })
    )
  ));
  card.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'button', class: 'btn btn-primary', onclick: () => {
      try { navigator.clipboard.writeText(keyValue); } catch (_) { /* 静默 */ }
      renderToast(t('apikey.keyCopied'), 'success');
    } }, iconEl('check', 16), el('span', { text: t('apikey.copyKey') })),
    el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => handlers.onDone && handlers.onDone() },
      el('span', { text: t('common.back') })
    )
  ));
  container.appendChild(card);
}

// --- 插件市场 ---


// renderPluginPage 渲染插件表格。
// handlers: { onInstall(id), onUninstall(id), onToggle(p), onDelete(id) }
