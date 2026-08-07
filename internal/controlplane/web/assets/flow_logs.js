// flow_logs.js — 日志检索（M6）
// 从 flow.js 拆分（P2-1）。职责：日志查询构建、搜索、分页、重置筛选。
// 依赖：api.js、render.js（esc/logLevelPill）、i18n.js。

import * as api from './api.js';
import { esc, logLevelPill } from './render.js';
import { t } from './i18n.js';

// ---------- 日志检索（M6） ----------
let logOffset = 0;
export function logMsg(s, ok) { const el = document.getElementById('logMsg'); if (el) { el.className = 'msg ' + (ok ? 'ok' : 'err'); el.textContent = (ok ? '[ok] ' : '[err] ') + s; } }

function buildLogQuery(offset) {
  const p = [];
  const d = document.getElementById('logDevice').value.trim();
  const a = document.getElementById('logAgent').value.trim();
  const lv = document.getElementById('logLevel').value;
  const src = document.getElementById('logSource').value;
  const kw = document.getElementById('logKeyword').value.trim();
  const f = document.getElementById('logFrom').value.trim();
  const t = document.getElementById('logTo').value.trim();
  const lim = document.getElementById('logLimit').value.trim() || '200';
  if (d) p.push('deviceID=' + encodeURIComponent(d));
  if (a) p.push('agentID=' + encodeURIComponent(a));
  if (lv) p.push('level=' + encodeURIComponent(lv));
  if (src) p.push('source=' + encodeURIComponent(src));
  if (kw) p.push('keyword=' + encodeURIComponent(kw));
  if (f) p.push('from=' + encodeURIComponent(f));
  if (t) p.push('to=' + encodeURIComponent(t));
  p.push('limit=' + encodeURIComponent(lim));
  p.push('offset=' + offset);
  return p.join('&');
}

export function searchLogs(offset) {
  logOffset = (offset || 0);
  document.getElementById('logLimitInfo').textContent = document.getElementById('logLimit').value || '200';
  api.getLogs(buildLogQuery(logOffset))
    .then(function (list) {
      if (!list || list.length === 0) { document.getElementById('logList').innerHTML = '<p class="muted">' + esc(t('flow.msg.noLogMatch')) + '</p>'; updateLogPage(0); return; }
      let html = '<div class="table-wrap"><table><colgroup><col style="width:15%"><col style="width:8%"><col style="width:8%"><col style="width:18%"><col style="width:18%"><col style="width:33%"></colgroup><thead><tr><th>' + esc(t('logs.col.time')) + '</th><th>' + esc(t('logs.col.level')) + '</th><th>' + esc(t('logs.col.source')) + '</th><th>' + esc(t('logs.col.device')) + '</th><th>' + esc(t('logs.col.agent')) + '</th><th>' + esc(t('logs.col.message')) + '</th></tr></thead><tbody>';
      list.forEach(function (e) {
        const ts = (e.timestamp || '').toString().replace('T', ' ').replace('Z', '');
        html += '<tr><td><small class="muted">' + esc(ts) + '</small></td><td>' + logLevelPill(e.level) + '</td><td>' + esc(e.source || '') + '</td><td><code title="' + esc(e.deviceID || '') + '">' + esc(e.deviceID || '') + '</code></td><td><code title="' + esc(e.agentID || '') + '">' + esc(e.agentID || '') + '</code></td><td class="wrap">' + esc(e.message || '') + '</td></tr>';
      });
      html += '</tbody></table></div>';
      document.getElementById('logList').innerHTML = html;
      updateLogPage(list.length);
    }).catch(function (err) { logMsg('error: ' + err, false); });
}

function updateLogPage(n) {
  const lim = parseInt(document.getElementById('logLimit').value || '200', 10);
  const cur = Math.floor(logOffset / lim) + 1;
  document.getElementById('logPageInfo').textContent = t('flow.msg.logPage', { cur: cur, n: n });
}
export function logPrev() { if (logOffset > 0) { searchLogs(Math.max(0, logOffset - parseInt(document.getElementById('logLimit').value || '200', 10))); } }
export function logNext() { searchLogs(logOffset + parseInt(document.getElementById('logLimit').value || '200', 10)); }
export function resetLogFilters() {
  ['logDevice', 'logAgent', 'logKeyword', 'logFrom', 'logTo'].forEach(function (id) { document.getElementById(id).value = ''; });
  document.getElementById('logLevel').value = ''; document.getElementById('logSource').value = '';
  document.getElementById('logList').innerHTML = '<p class="muted">' + esc(t('flow.msg.logCleared')) + '</p>';
}