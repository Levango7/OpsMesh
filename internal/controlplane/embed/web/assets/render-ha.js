// render-ha.js — 高可用渲染（由 render.js 拆分）。

// 渲染子模块 — 由 render.js 拆分而来。
// 公共依赖：i18n（t）、icons（iconEl/iconHtml）、render-common（DOM/Badge/表单辅助）。

import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { el, formatTime, badge, renderEmpty, fieldRow } from './render-common.js';

// ============================================================================
// Phase 3：高可用渲染
// ============================================================================

// renderHAStatus 渲染 HA 状态卡片。
export function renderHAStatus(container, status) {
  container.innerHTML = '';
  if (!status) { renderEmpty(container); return; }
  const card = el('div', { class: 'content' });
  card.appendChild(el('h3', { class: 'form-title', text: t('ha.status') }));
  card.appendChild(el('div', { class: 'form-row' },
    el('label', { class: 'form-label', text: t('ha.leader') }),
    el('div', { class: 'form-control', text: status.leader || status.leaderID || '-' })
  ));
  if (status.mode) {
    card.appendChild(el('div', { class: 'form-row' },
      el('label', { class: 'form-label', text: 'Mode' }),
      el('div', { class: 'form-control', text: status.mode })
    ));
  }
  if (status.status) {
    card.appendChild(el('div', { class: 'form-row' },
      el('label', { class: 'form-label', text: t('common.status') }),
      el('div', { class: 'form-control' }, badge(status.status, 'badge-status-resolved'))
    ));
  }
  container.appendChild(card);
}

// renderHAInstancesTable 渲染 HA 实例列表。
export function renderHAInstancesTable(container, instances) {
  container.innerHTML = '';
  if (!instances || !instances.length) { renderEmpty(container); return; }
  container.appendChild(el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('ha.role') }),
        el('th', { text: t('ha.isLeader') }),
        el('th', { text: t('common.status') }),
        el('th', { text: t('common.createdAt') })
      )
    ),
    el('tbody', null,
      instances.map((ins) => el('tr', null,
        el('td', { class: 'cell-title mono', text: ins.id || ins.instanceID || '-' }),
        el('td', null, badge(ins.role || '-', 'badge-category-change')),
        el('td', null, ins.isLeader ? badge('Leader', 'badge-status-resolved') : el('span', { text: '-' })),
        el('td', null, badge(ins.status || '-', ins.status === 'healthy' ? 'badge-status-resolved' : 'badge-priority-urgent')),
        el('td', { text: formatTime(ins.createdAt || ins.joinedAt) })
      ))
    )
  ));
}

// renderHAHealth 渲染 HA 健康状态。
export function renderHAHealth(container, health) {
  container.innerHTML = '';
  if (!health) { renderEmpty(container); return; }
  const card = el('div', { class: 'content' });
  card.appendChild(el('h3', { class: 'form-title', text: t('ha.health') }));
  const fields = ['status', 'leader', 'quorum', 'uptime', 'lastCheck'];
  fields.forEach((f) => {
    if (health[f] != null && health[f] !== '') {
      card.appendChild(el('div', { class: 'form-row' },
        el('label', { class: 'form-label', text: f }),
        el('div', { class: 'form-control', text: String(health[f]) })
      ));
    }
  });
  container.appendChild(card);
}

// renderBackupsTable 渲染备份列表。
// handlers: { onRestore(b), onDelete(b) }
export function renderBackupsTable(container, backups, handlers) {
  container.innerHTML = '';
  if (!backups || !backups.length) { renderEmpty(container); return; }
  container.appendChild(el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('ha.backupId') }),
        el('th', { text: t('ha.backupType') }),
        el('th', { text: t('ha.backupTime') }),
        el('th', { text: t('ha.backupSize') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      backups.map((b) => el('tr', null,
        el('td', { class: 'cell-title mono', text: b.id || '-' }),
        el('td', null, badge(b.type || '-', 'badge-category-change')),
        el('td', { text: formatTime(b.createdAt || b.time) }),
        el('td', { text: b.size != null ? b.size : '-' }),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn btn-ghost', title: t('ha.restore'), onclick: () => handlers.onRestore && handlers.onRestore(b) },
            iconEl('restore', 14)
          ),
          el('button', { class: 'btn btn-ghost btn-icon-danger', title: t('ha.deleteBackup'), onclick: () => handlers.onDelete && handlers.onDelete(b) },
            iconEl('trash', 14)
          )
        )
      ))
    )
  ));
}

// renderCreateBackupForm 渲染创建备份表单。
// handlers: { onCreate(type) }
export function renderCreateBackupForm(container, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => { e.preventDefault(); handlers.onCreate && handlers.onCreate(form.elements.type.value.trim()); } });
  form.appendChild(el('h3', { class: 'form-title', text: t('ha.createBackup') }));
  form.appendChild(fieldRow(t('ha.backupType'), true,
    el('select', { name: 'type', required: 'true' },
      el('option', { value: 'full', text: 'full' }),
      el('option', { value: 'incremental', text: 'incremental' }),
      el('option', { value: 'config', text: 'config' })
    )
  ));
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' },
      iconEl('backup', 16), el('span', { text: t('ha.createBackup') })
    )
  ));
  container.appendChild(form);
}

