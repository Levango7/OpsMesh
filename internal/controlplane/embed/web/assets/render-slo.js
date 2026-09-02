// render-slo.js — SLO 渲染（由 render.js 拆分）。

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
// SLO 渲染
// ============================================================================

// renderSLOTable 渲染 SLO 列表表格。
// handlers: { onDetail(id), onDelete(id) }
export function renderSLOTable(container, slos, handlers) {
  container.innerHTML = '';
  if (!slos || slos.length === 0) {
    renderEmpty(container, t('common.empty'));
    return;
  }
  const table = el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('common.name') }),
        el('th', { text: t('common.service') }),
        el('th', { text: t('common.target') }),
        el('th', { text: t('common.window') }),
        el('th', { text: t('slo.slis') }),
        el('th', { text: t('common.createdAt') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      slos.map((slo) => el('tr', null,
        el('td', { class: 'mono', text: slo.id }),
        el('td', { class: 'cell-title', text: slo.name }),
        el('td', { text: slo.serviceName || '-' }),
        el('td', { class: 'mono', text: (slo.target != null ? slo.target + '%' : '-') }),
        el('td', { class: 'mono', text: slo.window || '-' }),
        el('td', { class: 'mono', text: (slo.slis && slo.slis.length) || 0 }),
        el('td', { class: 'mono', text: formatTime(slo.createdAt) }),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn-icon', title: t('slo.detail'), onclick: () => handlers.onDetail(slo.id) }, iconEl('search', 14)),
          el('button', { class: 'btn-icon btn-icon-danger', title: t('common.delete'), onclick: () => handlers.onDelete(slo.id) }, iconEl('trash', 14))
        )
      ))
    )
  );
  container.appendChild(table);
}

// renderSLOForm 渲染 SLO 创建/编辑表单。
// slo=null 表示创建；否则编辑（预填）。
// handlers: { onSubmit(data), onCancel() }
export function renderSLOForm(container, slo, handlers) {
  container.innerHTML = '';
  const isEdit = !!slo;
  const form = el('form', { class: 'form-card', onsubmit: (e) => { e.preventDefault(); handlers.onSubmit(collectSLOForm(form)); } });

  form.appendChild(el('h3', { class: 'form-title', text: isEdit ? t('slo.edit') : t('slo.create') }));

  // 名称（必填）
  form.appendChild(fieldRow(t('common.name'), true,
    el('input', { type: 'text', name: 'name', required: 'true', value: slo ? slo.name : '', placeholder: t('slo.nameRequired') })
  ));
  // 描述
  form.appendChild(fieldRow(t('common.description'), false,
    el('textarea', { name: 'description', rows: '2' }, slo ? slo.description : '')
  ));
  // 服务名 + 窗口
  form.appendChild(fieldRow(t('common.service'), false,
    el('input', { type: 'text', name: 'serviceName', value: slo ? slo.serviceName : '' })
  ));
  form.appendChild(fieldRow(t('common.window'), false,
    el('select', { name: 'window' },
      ['7d', '30d', '90d'].map((w) =>
        el('option', { value: w, selected: (slo ? slo.window : '30d') === w ? 'selected' : undefined, text: w })
      )
    )
  ));
  // 目标（百分比，0-100）
  form.appendChild(fieldRow(t('common.target'), false,
    el('input', { type: 'number', name: 'target', min: '0', max: '100', step: '0.01', value: slo ? slo.target : 99.9 })
  ));

  // SLI 列表（可增删）
  form.appendChild(el('div', { class: 'form-row form-row-sli' },
    el('label', { class: 'form-label', text: t('slo.slis') }),
    el('div', { class: 'form-control' },
      el('div', { id: 'sliList', class: 'sli-list' })
    )
  ));
  const sliList = form.querySelector('#sliList');
  const initialSlis = (slo && slo.slis) ? slo.slis : [];
  if (initialSlis.length === 0) {
    sliList.appendChild(buildSliRow({ name: '', metric: '', target: 0, operator: '>=' }));
  } else {
    initialSlis.forEach((sli) => sliList.appendChild(buildSliRow(sli)));
  }
  form.appendChild(el('div', { class: 'form-row' },
    el('div', { class: 'form-label' }),
    el('div', { class: 'form-control' },
      el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => sliList.appendChild(buildSliRow({ name: '', metric: '', target: 0, operator: '>=' })) },
        iconEl('plus', 14), el('span', { text: t('slo.addSli') })
      )
    )
  ));

  // 操作按钮
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' }, iconEl('check', 16), el('span', { text: t('common.save') })),
    el('button', { type: 'button', class: 'btn btn-ghost', onclick: () => handlers.onCancel() }, el('span', { text: t('common.cancel') }))
  ));

  container.appendChild(form);
}

function buildSliRow(sli) {
  const row = el('div', { class: 'sli-row' },
    el('input', { type: 'text', name: 'sliName', class: 'sli-name', value: sli.name || '', placeholder: t('slo.sliName') }),
    el('input', { type: 'text', name: 'sliMetric', class: 'sli-metric', value: sli.metric || '', placeholder: t('slo.sliMetric') }),
    el('select', { name: 'sliOperator', class: 'sli-op' },
      ['>=', '<=', '>', '<'].map((op) =>
        el('option', { value: op, selected: (sli.operator || '>=') === op ? 'selected' : undefined, text: op })
      )
    ),
    el('input', { type: 'number', name: 'sliTarget', class: 'sli-target', value: sli.target != null ? sli.target : 0, placeholder: t('slo.sliTarget'), step: '0.01' }),
    el('button', { type: 'button', class: 'btn-icon btn-icon-danger', title: t('slo.removeSli'), onclick: () => row.remove() }, iconEl('trash', 14))
  );
  return row;
}

function collectSLOForm(form) {
  const get = (name) => (form.elements[name] && form.elements[name].value) || '';
  const slis = [];
  const sliRows = form.querySelectorAll('.sli-row');
  sliRows.forEach((row) => {
    const name = row.querySelector('.sli-name').value.trim();
    const metric = row.querySelector('.sli-metric').value.trim();
    const operator = row.querySelector('.sli-op').value;
    const target = parseFloat(row.querySelector('.sli-target').value) || 0;
    if (name || metric) slis.push({ name, metric, operator, target });
  });
  return {
    name: get('name'),
    description: get('description'),
    serviceName: get('serviceName'),
    target: parseFloat(get('target')) || 0,
    window: get('window'),
    slis,
  };
}

// renderSLODetail 渲染 SLO 详情 + SLI 状态。
// statuses: [{sliName, currentValue, targetValue, status, lastEvaluated}]
// handlers: { onBack(), onEdit(), onDelete() }
export function renderSLODetail(container, slo, statuses, handlers) {
  container.innerHTML = '';
  if (!slo) { renderEmpty(container); return; }
  const card = el('div', { class: 'detail-card' });

  card.appendChild(el('div', { class: 'detail-head' },
    el('button', { class: 'btn btn-ghost', onclick: () => handlers.onBack() }, iconEl('back', 16), el('span', { text: t('common.back') })),
    el('h3', { class: 'detail-title', text: slo.name }),
    el('div', { class: 'detail-actions' },
      el('button', { class: 'btn btn-secondary', onclick: () => handlers.onEdit() }, iconEl('edit', 14), el('span', { text: t('common.edit') })),
      el('button', { class: 'btn btn-danger', onclick: () => handlers.onDelete() }, iconEl('trash', 14), el('span', { text: t('common.delete') }))
    )
  ));

  card.appendChild(el('div', { class: 'detail-grid' },
    detailItem(t('common.id'), slo.id, true),
    detailItem(t('common.service'), slo.serviceName || '-'),
    detailItem(t('common.target'), (slo.target != null ? slo.target + '%' : '-'), true),
    detailItem(t('common.window'), slo.window || '-', true),
    detailItem(t('common.createdAt'), formatTime(slo.createdAt), true),
    detailItem(t('common.updatedAt'), formatTime(slo.updatedAt), true)
  ));

  if (slo.description) {
    card.appendChild(el('div', { class: 'detail-section' },
      el('h4', { text: t('common.description') }),
      el('p', { class: 'detail-desc', text: slo.description })
    ));
  }

  // SLI 定义
  if (slo.slis && slo.slis.length) {
    card.appendChild(el('div', { class: 'detail-section' },
      el('h4', { text: t('slo.slis') }),
      el('table', { class: 'data-table data-table-compact' },
        el('thead', null,
          el('tr', null,
            el('th', { text: t('slo.sliName') }),
            el('th', { text: t('slo.sliMetric') }),
            el('th', { text: t('slo.sliOperator') }),
            el('th', { text: t('slo.sliTarget') })
          )
        ),
        el('tbody', null,
          slo.slis.map((sli) => el('tr', null,
            el('td', { text: sli.name || '-' }),
            el('td', { class: 'mono', text: sli.metric || '-' }),
            el('td', { class: 'mono', text: sli.operator || '-' }),
            el('td', { class: 'mono', text: String(sli.target != null ? sli.target : '-') })
          ))
        )
      )
    ));
  }

  // SLI 实时状态
  card.appendChild(el('div', { class: 'detail-section' },
    el('h4', { text: t('slo.statusTitle') }),
    (statuses && statuses.length)
      ? el('table', { class: 'data-table data-table-compact' },
          el('thead', null,
            el('tr', null,
              el('th', { text: t('slo.sliName') }),
              el('th', { text: t('slo.sliCurrent') }),
              el('th', { text: t('slo.sliTarget') }),
              el('th', { text: t('common.status') }),
              el('th', { text: t('slo.sliLastEvaluated') })
            )
          ),
          el('tbody', null,
            statuses.map((st) => el('tr', null,
              el('td', { text: st.sliName || '-' }),
              el('td', { class: 'mono', text: formatNumber(st.currentValue) }),
              el('td', { class: 'mono', text: formatNumber(st.targetValue) }),
              el('td', null, sloStatusBadge(st.status)),
              el('td', { class: 'mono', text: formatTime(st.lastEvaluated) })
            ))
          )
        )
      : el('p', { class: 'detail-desc', text: t('slo.noStatus') })
  ));

  container.appendChild(card);
}


