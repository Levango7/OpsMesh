// flow_audits.js — 审计日志
// 从 flow.js 拆分（P2-1）。职责：拉取并渲染审计日志列表，支持按动作、时间窗、条数过滤。
// 依赖：api.js、render.js（esc/fmtTime）、i18n.js。
// 后端：GET /api/v1/audits?action=&from=&to=&limit= → AuditEvent[]

import * as api from './api.js';
import { esc, fmtTime } from './render.js';
import { t } from './i18n.js';

// ---------- 审计日志 ----------
export function loadAudits() {
  const action = document.getElementById('auditActionFilter') ? document.getElementById('auditActionFilter').value : '';
  const fromInput = document.getElementById('auditFromInput');
  const toInput = document.getElementById('auditToInput');
  const limitInput = document.getElementById('auditLimitInput');
  let from = '', to = '';
  if (fromInput && fromInput.value) from = new Date(fromInput.value).toISOString();
  if (toInput && toInput.value) to = new Date(toInput.value).toISOString();
  let limit = 100;
  if (limitInput && limitInput.value) limit = parseInt(limitInput.value) || 100;

  const listEl = document.getElementById('auditList');
  if (listEl) listEl.innerHTML = '<p class="muted">' + esc(t('audits.loading')) + '</p>';
  api.getAudits(action, from, to, limit).then(function (list) {
    const el = document.getElementById('auditList');
    if (!el) return;
    if (!list || list.length === 0) {
      el.innerHTML = '<p class="muted">' + esc(t('audits.empty')) + '</p>';
      return;
    }
    let html = '<div class="table-wrap"><table><colgroup><col style="width:15%"><col style="width:12%"><col style="width:15%"><col style="width:20%"><col style="width:28%"><col style="width:10%"></colgroup><thead><tr><th>' + esc(t('audits.time')) + '</th><th>' + esc(t('audits.user')) + '</th><th>' + esc(t('audits.action')) + '</th><th>' + esc(t('audits.target')) + '</th><th>' + esc(t('audits.detail')) + '</th><th>' + esc(t('audits.tenant')) + '</th></tr></thead><tbody>';
    list.forEach(function (a) {
      html += '<tr><td>' + esc(fmtTime(a.createdAt)) + '</td><td>' + esc(a.userID || '-') + '</td><td><span class="badge">' + esc(a.action || '-') + '</span></td><td>' + esc(a.target || '-') + '</td><td>' + esc(a.detail || '-') + '</td><td>' + esc(a.tenantID || '-') + '</td></tr>';
    });
    html += '</tbody></table></div>';
    el.innerHTML = html;
  }).catch(function (e) {
    console.error('[audits]', e);
    const el = document.getElementById('auditList');
    if (el) el.innerHTML = '<p class="muted">' + esc(t('common.networkError')) + '</p>';
  });
}