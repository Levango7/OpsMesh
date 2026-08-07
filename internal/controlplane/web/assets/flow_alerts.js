// flow_alerts.js — 监控告警（M7，独立 Tab）
// 从 flow.js 拆分（P2-1）。职责：告警列表轮询、确认告警、静默告警。
// 依赖：api.js、render.js（esc/escAttr/fmtTime）、icons.js、i18n.js、poll.js（pollAlerts）、
//       flow_focus.js（applyFocus/getFocusDevice/setFocus）。

import * as api from './api.js';
import { esc, escAttr, fmtTime } from './render.js';
import { icon } from './icons.js';
import { t } from './i18n.js';
import { pollAlerts } from './poll.js';
import { applyFocus, getFocusDevice, setFocus } from './flow_focus.js';

// ---------- 监控告警（M7，独立 Tab） ----------
export function pollAlertsFull() {
  api.getAlerts().then(function (list) {
    const fl = applyFocus(list || [], 'alert');
    const focusDevice = getFocusDevice();
    let crit = 0, warn = 0;
    fl.forEach(function (a) { if (a.severity === 'critical') crit++; else warn++; });
    const sc = document.getElementById('statCritical'); if (sc) sc.textContent = crit;
    const sw = document.getElementById('statWarning'); if (sw) sw.textContent = warn;
    const stEl = document.getElementById('statTotalAlerts'); if (stEl) stEl.textContent = fl.length;
    const note = focusDevice ? '<p class="hint">' + icon('context', 14) + ' ' + esc(t('render.linkFiltered')) + ' <code>' + esc(focusDevice.id) + '</code> ' + esc(t('render.filtered')) + '（' + fl.length + '）</p>' : '';
    if (fl.length === 0) { document.getElementById('alertsFull').innerHTML = note + '<p class="muted">' + esc(t('render.noAlerts')) + '</p>'; return; }
    let html = note;
    fl.forEach(function (a) {
      const cls = a.severity === 'critical' ? 'alert' : 'alert warn';
      const ast = a.status || 'firing';
      const badge = ast === 'acknowledged' ? '<span class="badge ok">' + esc(t('alerts.acknowledged')) + '</span>'
        : ast === 'silenced' ? '<span class="badge info">' + esc(t('alerts.silenced')) + '</span>'
          : '<span class="badge fail">' + esc(t('alerts.pending')) + '</span>';
      let actions = '';
      if (ast === 'firing') {
        actions = '<div class="alert-actions">'
          + '<button class="btn xs" onclick="ackAlert(\'' + escAttr(a.alertID) + '\')">' + icon('check', 14) + ' ' + esc(t('alerts.ack')) + '</button>'
          + '<button class="btn xs outline" onclick="silenceAlert(\'' + escAttr(a.alertID) + '\')">' + icon('close', 14) + ' ' + esc(t('alerts.silence')) + '</button>'
          + '</div>';
      } else {
        let meta = esc(a.acknowledgedBy || '');
        if (ast === 'silenced' && a.silencedUntil) { meta += ' · ' + esc(a.silencedUntil); }
        actions = '<div class="alert-actions"><span class="muted" style="font-size:12px">' + esc(t('alerts.handler')) + (meta || '—') + '</span></div>';
      }
      html += '<div class="' + cls + '">'
        + '<div class="alert-head"><b>[' + esc(a.severity) + ']</b> ' + badge + '</div>'
        + esc(t('alerts.device')) + ' ' + esc(a.deviceID) + ' ｜ Agent ' + esc(a.agentID)
        + (a.comment ? '<br><small class="muted">' + esc(t('alerts.comment')) + esc(a.comment) + '</small>' : '')
        + '<br>' + esc(a.message)
        + '<br><small class="muted">' + fmtTime(a.createdAt) + '</small>'
        + actions
        + '<button class="jbtn" style="margin-top:6px" onclick="setFocus(\'' + escAttr(a.deviceID) + '\',\'\',\'\',\'\');switchTab(\'alerts\')">' + icon('context', 14) + ' ' + esc(t('render.contextLink')) + '</button>'
        + '</div>';
    });
    document.getElementById('alertsFull').innerHTML = html;
  }).catch(function (e) { api.apiFail('alertsFull', e); });
}

// 确认告警（M7）：POST /api/v1/alerts/{id}/ack
export function ackAlert(id) {
  api.ackAlert(id).then(function (x) {
    if (x.s < 400) { pollAlertsFull(); pollAlerts(); }
    else { alert(t('alerts.ackFail') + (x.j.error || x.s)); }
  }).catch(function (err) { alert(t('alerts.ackFail') + err); });
}

// 静默告警（M7）：POST /api/v1/alerts/{id}/silence（默认 24h）
export function silenceAlert(id) {
  const dur = prompt(t('alerts.silencePrompt'), '1440');
  if (dur === null) return;
  let minutes = parseInt(dur, 10); if (isNaN(minutes) || minutes <= 0) minutes = 1440;
  const comment = prompt(t('alerts.commentPrompt'), '') || '';
  api.silenceAlert(id, { durationMinutes: minutes, comment: comment })
    .then(function (x) {
      if (x.s < 400) { pollAlertsFull(); pollAlerts(); }
      else { alert(t('alerts.silenceFail') + (x.j.error || x.s)); }
    }).catch(function (err) { alert(t('alerts.silenceFail') + err); });
}