// flow_schedules.js — task 243 M5 集成：定时任务管理页面交互逻辑
//
// 职责：
//   - loadSchedulesPage：加载定时任务列表
//   - showCreateScheduleModal / confirmCreateSchedule：创建定时任务
//   - showEditScheduleModal / confirmEditSchedule：编辑定时任务
//   - pauseSchedule / resumeSchedule / deleteScheduleConfirm：暂停/恢复/删除
//
// 依赖：api.js、render.js（esc/escAttr/fmtTime/showModal/closeModal）、i18n.js

import * as api from './api.js';
import { esc, escAttr, fmtTime, showModal, closeModal } from './render.js';
import { t } from './i18n.js';

// 当前编辑的定时任务 ID（空=新建）。
let editingScheduleID = '';

// ============================================================================
// 列表加载
// ============================================================================

export function loadSchedulesPage() {
  api.getSchedules('').then(function (data) {
    renderSchedules(data.schedules || []);
  }).catch(function (e) { api.apiFail('schedules', e); });
}

function renderSchedules(list) {
  const el = document.getElementById('schedulesList');
  if (!el) return;
  if (!list.length) {
    el.innerHTML = '<p class="muted">' + esc(t('schedule.empty')) + '</p>';
    return;
  }
  let html = '<table class="table"><thead><tr>'
    + '<th>' + esc(t('schedule.name')) + '</th>'
    + '<th>' + esc(t('schedule.cronExpr')) + '</th>'
    + '<th>' + esc(t('schedule.taskID')) + '</th>'
    + '<th>' + esc(t('schedule.status')) + '</th>'
    + '<th>' + esc(t('schedule.lastRun')) + '</th>'
    + '<th>' + esc(t('schedule.nextRun')) + '</th>'
    + '<th>' + esc(t('schedule.actions')) + '</th>'
    + '</tr></thead><tbody>';
  list.forEach(function (s) {
    html += '<tr>'
      + '<td>' + esc(s.name || '-') + '</td>'
      + '<td><code>' + esc(s.cronExpr) + '</code></td>'
      + '<td><code>' + esc(s.taskID) + '</code></td>'
      + '<td>' + statusBadge(s.status) + '</td>'
      + '<td>' + (s.lastRunAt && s.lastRunAt.indexOf('0001') < 0 ? esc(fmtTime(s.lastRunAt)) : esc(t('schedule.never'))) + '</td>'
      + '<td>' + (s.nextRunAt && s.nextRunAt.indexOf('0001') < 0 ? esc(fmtTime(s.nextRunAt)) : '-') + '</td>'
      + '<td>' + scheduleActions(s) + '</td>'
      + '</tr>';
  });
  html += '</tbody></table>';
  el.innerHTML = html;
}

function statusBadge(status) {
  const map = {
    active: 'badge-success',
    paused: 'badge-warning',
    deleted: 'badge-secondary',
  };
  const cls = map[status] || 'badge-secondary';
  return '<span class="badge ' + cls + '">' + esc(t('schedule.' + status) || status) + '</span>';
}

function scheduleActions(s) {
  let html = '';
  if (s.status === 'active') {
    html += ' <button class="btn btn-sm" onclick="pauseSchedule(\'' + escAttr(s.id) + '\')">' + esc(t('schedule.pause')) + '</button>';
  } else if (s.status === 'paused') {
    html += ' <button class="btn btn-sm btn-primary" onclick="resumeSchedule(\'' + escAttr(s.id) + '\')">' + esc(t('schedule.resume')) + '</button>';
  }
  if (s.status !== 'deleted') {
    html += ' <button class="btn btn-sm" onclick="showEditScheduleModal(\'' + escAttr(s.id) + '\')">' + esc(t('schedule.edit')) + '</button>';
    html += ' <button class="btn btn-sm btn-danger" onclick="deleteScheduleConfirm(\'' + escAttr(s.id) + '\')">' + esc(t('schedule.delete')) + '</button>';
  }
  return html;
}

// ============================================================================
// 创建 / 编辑
// ============================================================================

export function showCreateScheduleModal() {
  editingScheduleID = '';
  const body = ''
    + '<div class="form-group"><label>' + esc(t('schedule.name')) + '</label>'
    + '<input type="text" id="schName" class="form-control" placeholder="' + escAttr(t('schedule.namePh')) + '"></div>'
    + '<div class="form-group"><label>' + esc(t('schedule.cronExpr')) + '</label>'
    + '<input type="text" id="schCron" class="form-control" placeholder="' + escAttr(t('schedule.cronExprPh')) + '">'
    + '<small class="muted">' + esc(t('schedule.cronHelp')) + '</small></div>'
    + '<div class="form-group"><label>' + esc(t('schedule.taskID')) + '</label>'
    + '<input type="text" id="schTaskID" class="form-control" placeholder="task-xxx"></div>';
  const footer = ''
    + '<button class="btn" onclick="closeModal()">' + esc(t('approval.cancel')) + '</button>'
    + '<button class="btn btn-primary" onclick="confirmCreateSchedule()">' + esc(t('schedule.create')) + '</button>';
  showModal(t('schedule.create'), body, footer);
}

export function confirmCreateSchedule() {
  const name = (document.getElementById('schName') || {}).value || '';
  const cronExpr = (document.getElementById('schCron') || {}).value || '';
  const taskID = (document.getElementById('schTaskID') || {}).value || '';
  if (!cronExpr || !taskID) {
    alert(t('schedule.createFail') + ': cron + taskID required');
    return;
  }
  api.createSchedule({ name: name, cronExpr: cronExpr, taskID: taskID }).then(function (r) {
    if (r.s === 201) {
      closeModal();
      loadSchedulesPage();
    } else {
      alert(t('schedule.createFail') + ': ' + (r.j && r.j.error ? r.j.error : 'HTTP ' + r.s));
    }
  }).catch(function (e) { alert(t('schedule.createFail') + ': ' + e.message); });
}

export function showEditScheduleModal(id) {
  editingScheduleID = id;
  api.getSchedules('').then(function (data) {
    const s = (data.schedules || []).find(function (x) { return x.id === id; });
    if (!s) { alert(t('schedule.loadFail')); return; }
    const body = ''
      + '<div class="form-group"><label>' + esc(t('schedule.name')) + '</label>'
      + '<input type="text" id="schName" class="form-control" value="' + escAttr(s.name || '') + '"></div>'
      + '<div class="form-group"><label>' + esc(t('schedule.cronExpr')) + '</label>'
      + '<input type="text" id="schCron" class="form-control" value="' + escAttr(s.cronExpr) + '">'
      + '<small class="muted">' + esc(t('schedule.cronHelp')) + '</small></div>';
    const footer = ''
      + '<button class="btn" onclick="closeModal()">' + esc(t('approval.cancel')) + '</button>'
      + '<button class="btn btn-primary" onclick="confirmEditSchedule()">' + esc(t('schedule.edit')) + '</button>';
    showModal(t('schedule.edit'), body, footer);
  }).catch(function (e) { alert(t('schedule.loadFail') + ': ' + e.message); });
}

export function confirmEditSchedule() {
  const name = (document.getElementById('schName') || {}).value || '';
  const cronExpr = (document.getElementById('schCron') || {}).value || '';
  api.updateSchedule(editingScheduleID, { name: name, cronExpr: cronExpr }).then(function (r) {
    if (r.s === 200) {
      closeModal();
      loadSchedulesPage();
    } else {
      alert(t('schedule.saveFail') + ': ' + (r.j && r.j.error ? r.j.error : 'HTTP ' + r.s));
    }
  }).catch(function (e) { alert(t('schedule.saveFail') + ': ' + e.message); });
}

// ============================================================================
// 暂停 / 恢复 / 删除
// ============================================================================

export function pauseSchedule(id) {
  api.pauseSchedule(id).then(function (r) {
    if (r.s === 200) loadSchedulesPage();
    else alert(t('schedule.saveFail'));
  }).catch(function (e) { alert(t('schedule.saveFail') + ': ' + e.message); });
}

export function resumeSchedule(id) {
  api.resumeSchedule(id).then(function (r) {
    if (r.s === 200) loadSchedulesPage();
    else alert(t('schedule.saveFail'));
  }).catch(function (e) { alert(t('schedule.saveFail') + ': ' + e.message); });
}

export function deleteScheduleConfirm(id) {
  if (!confirm(t('schedule.deleteConfirm'))) return;
  api.deleteSchedule(id).then(function (r) {
    if (r.s === 200) loadSchedulesPage();
    else alert(t('schedule.saveFail'));
  }).catch(function (e) { alert(t('schedule.saveFail') + ': ' + e.message); });
}