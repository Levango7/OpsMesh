// render-compliance.js — 安全合规渲染（由 render.js 拆分）。

// 渲染子模块 — 由 render.js 拆分而来。
// 公共依赖：i18n（t）、icons（iconEl/iconHtml）、render-common（DOM/Badge/表单辅助）。

import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { el, formatTime, badge, renderEmpty, fieldRow } from './render-common.js';

// ============================================================================
// Phase 3：安全合规渲染
// ============================================================================

// severityBadge 严重级别 badge。
function severityBadge(level) {
  const map = { low: 'badge-priority-low', medium: 'badge-priority-medium', high: 'badge-priority-high', critical: 'badge-priority-urgent' };
  return badge(level || '-', map[level] || 'badge-priority-low');
}

// renderComplianceRulesTable 渲染合规规则表格。
// handlers: { onSelect(rule) }
export function renderComplianceRulesTable(container, rules, handlers) {
  container.innerHTML = '';
  if (!rules || !rules.length) { renderEmpty(container); return; }
  container.appendChild(el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('compliance.ruleName') }),
        el('th', { text: t('compliance.ruleCategory') }),
        el('th', { text: t('compliance.severity') }),
        el('th', { text: t('compliance.ruleDesc') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      rules.map((r) => el('tr', null,
        el('td', { class: 'cell-title', text: r.name || r.id }),
        el('td', null, badge(r.category || '-', 'badge-category-change')),
        el('td', null, severityBadge(r.severity)),
        el('td', { text: r.description || '-' }),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn btn-ghost', onclick: () => handlers.onSelect && handlers.onSelect(r) },
            iconEl('search', 14), el('span', { text: t('compliance.ruleDetail') })
          )
        )
      ))
    )
  ));
}

// renderComplianceRuleDetail 渲染合规规则详情。
// handlers: { onBack() }
export function renderComplianceRuleDetail(container, rule, handlers) {
  container.innerHTML = '';
  const card = el('div', { class: 'content' });
  card.appendChild(el('h3', { class: 'form-title', text: rule.name || rule.id }));
  card.appendChild(el('p', { class: 'metrics-hint', text: (rule.category || '-') + ' · ' + (rule.severity || '-') }));
  if (rule.description) {
    card.appendChild(el('div', { class: 'form-row' },
      el('label', { class: 'form-label', text: t('compliance.ruleDesc') }),
      el('div', { class: 'form-control', text: rule.description })
    ));
  }
  if (rule.checkScript) {
    card.appendChild(el('div', { class: 'form-row' },
      el('label', { class: 'form-label', text: t('compliance.checkScript') }),
      el('pre', { class: 'mono' }, rule.checkScript)
    ));
  }
  if (rule.fixAdvice) {
    card.appendChild(el('div', { class: 'form-row' },
      el('label', { class: 'form-label', text: t('compliance.fixAdvice') }),
      el('div', { class: 'form-control', text: rule.fixAdvice })
    ));
  }
  card.appendChild(el('div', { class: 'form-actions' },
    el('button', { class: 'btn btn-ghost', onclick: () => handlers.onBack && handlers.onBack() },
      iconEl('back', 14), el('span', { text: t('common.back') })
    )
  ));
  container.appendChild(card);
}

// renderComplianceScanForm 渲染合规扫描表单。
// handlers: { onScan(deviceID) }
export function renderComplianceScanForm(container, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => { e.preventDefault(); handlers.onScan && handlers.onScan(form.elements.deviceID.value.trim()); } });
  form.appendChild(el('h3', { class: 'form-title', text: t('compliance.scan') }));
  form.appendChild(fieldRow(t('compliance.selectDevice'), true,
    el('input', { type: 'text', name: 'deviceID', required: 'true', placeholder: 'device-id' })
  ));
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' },
      iconEl('scan', 16), el('span', { text: t('compliance.startScan') })
    )
  ));
  container.appendChild(form);
}

// renderComplianceReport 渲染合规扫描报告。
export function renderComplianceReport(container, report) {
  container.innerHTML = '';
  if (!report) { renderEmpty(container); return; }
  const card = el('div', { class: 'content' });
  card.appendChild(el('h3', { class: 'form-title', text: t('compliance.reportDetail') }));
  // 概览行
  const overview = el('div', { class: 'form-row' });
  overview.appendChild(el('span', { class: 'badge badge-status-resolved', text: t('compliance.passedRules') + ': ' + (report.passedCount != null ? report.passedCount : '-') }));
  overview.appendChild(el('span', { class: 'badge badge-priority-urgent', text: t('compliance.failedRules') + ': ' + (report.failedCount != null ? report.failedCount : '-') }));
  if (report.score != null) {
    overview.appendChild(el('span', { class: 'badge badge-status-open', text: t('compliance.score') + ': ' + report.score }));
  }
  card.appendChild(overview);
  // 详细结果
  const results = (report.results || report.items || []);
  if (results.length) {
    card.appendChild(el('h4', { text: t('compliance.result') }));
    card.appendChild(el('table', { class: 'data-table data-table-compact' },
      el('thead', null,
        el('tr', null,
          el('th', { text: t('compliance.ruleName') }),
          el('th', { text: t('common.status') }),
          el('th', { text: t('compliance.ruleDesc') })
        )
      ),
      el('tbody', null,
        results.map((it) => el('tr', null,
          el('td', { class: 'cell-title', text: it.rule || it.name || it.id || '-' }),
          el('td', null, badge(it.passed ? t('compliance.passed') : t('compliance.failed'),
            it.passed ? 'badge-status-resolved' : 'badge-priority-urgent')),
          el('td', { text: it.message || it.detail || '-' })
        ))
      )
    ));
  }
  container.appendChild(card);
}

// renderComplianceReportsList 渲染合规报告列表。
// handlers: { onView(report) }
export function renderComplianceReportsList(container, reports, handlers) {
  container.innerHTML = '';
  if (!reports || !reports.length) { renderEmpty(container); return; }
  container.appendChild(el('table', { class: 'data-table' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('common.id') }),
        el('th', { text: t('compliance.score') }),
        el('th', { text: t('compliance.passedRules') }),
        el('th', { text: t('compliance.failedRules') }),
        el('th', { text: t('common.createdAt') }),
        el('th', { class: 'th-actions', text: t('common.actions') })
      )
    ),
    el('tbody', null,
      reports.map((r) => el('tr', null,
        el('td', { class: 'cell-title mono', text: r.id || '-' }),
        el('td', null, badge(r.score != null ? String(r.score) : '-', 'badge-status-open')),
        el('td', { text: r.passedCount != null ? r.passedCount : '-' }),
        el('td', { text: r.failedCount != null ? r.failedCount : '-' }),
        el('td', { text: formatTime(r.createdAt || r.time) }),
        el('td', { class: 'td-actions' },
          el('button', { class: 'btn btn-ghost', onclick: () => handlers.onView && handlers.onView(r) },
            iconEl('search', 14), el('span', { text: t('compliance.reportDetail') })
          )
        )
      ))
    )
  ));
}

// renderAuditQueryForm 渲染审计日志查询表单。
// handlers: { onQuery(params), onExport(params) }
export function renderAuditQueryForm(container, handlers) {
  container.innerHTML = '';
  const form = el('form', { class: 'form-card', onsubmit: (e) => { e.preventDefault(); handlers.onQuery && handlers.onQuery(collectAuditQuery(form)); } });
  form.appendChild(el('h3', { class: 'form-title', text: t('compliance.audit') }));
  form.appendChild(fieldRow(t('compliance.auditFrom'), false,
    el('input', { type: 'datetime-local', name: 'from' })
  ));
  form.appendChild(fieldRow(t('compliance.auditTo'), false,
    el('input', { type: 'datetime-local', name: 'to' })
  ));
  form.appendChild(fieldRow(t('compliance.auditUser'), false,
    el('input', { type: 'text', name: 'user', placeholder: 'user id' })
  ));
  form.appendChild(fieldRow(t('compliance.auditAction'), false,
    el('input', { type: 'text', name: 'action', placeholder: 'login / create / delete …' })
  ));
  form.appendChild(fieldRow(t('compliance.auditLimit'), false,
    el('input', { type: 'number', name: 'limit', min: '1', max: '1000', value: '100' })
  ));
  form.appendChild(el('div', { class: 'form-actions' },
    el('button', { type: 'submit', class: 'btn btn-primary' },
      iconEl('search', 16), el('span', { text: t('compliance.query') })
    ),
    el('button', { type: 'button', class: 'btn btn-secondary', onclick: () => handlers.onExport && handlers.onExport(collectAuditQuery(form)) },
      iconEl('download', 16), el('span', { text: t('compliance.export') })
    )
  ));
  container.appendChild(form);
}

function collectAuditQuery(form) {
  const get = (name) => (form.elements[name] && form.elements[name].value) || '';
  return {
    from: get('from').trim(),
    to: get('to').trim(),
    user: get('user').trim(),
    action: get('action').trim(),
    limit: get('limit').trim(),
  };
}

// renderAuditEventsTable 渲染审计事件表格。
export function renderAuditEventsTable(container, events) {
  container.innerHTML = '';
  if (!events || !events.length) { renderEmpty(container); return; }
  container.appendChild(el('table', { class: 'data-table data-table-compact' },
    el('thead', null,
      el('tr', null,
        el('th', { text: t('compliance.auditTime') }),
        el('th', { text: t('compliance.auditUser') }),
        el('th', { text: t('compliance.auditAction') }),
        el('th', { text: t('compliance.auditTarget') }),
        el('th', { text: t('compliance.auditDetail') })
      )
    ),
    el('tbody', null,
      events.map((e) => el('tr', null,
        el('td', { text: formatTime(e.createdAt || e.time) }),
        el('td', { class: 'cell-title', text: e.userID || e.user || '-' }),
        el('td', null, badge(e.action || '-', 'badge-category-change')),
        el('td', { class: 'mono', text: e.target || '-' }),
        el('td', { text: e.detail || '-' })
      ))
    )
  ));
}

