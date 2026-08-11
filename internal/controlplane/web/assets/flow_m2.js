// flow_m2.js — task 241 M2 集成：告警规则引擎 + 静默规则 + 通知渠道 + 通知模板 页面交互逻辑
//
// 职责：
//   - loadAlertRulesEngine：加载告警规则列表并渲染
//   - showCreateAlertRuleModal/showEditAlertRuleModal：创建/编辑规则弹窗
//   - confirmCreateAlertRule/confirmEditAlertRule：提交表单
//   - deleteAlertRuleEngineConfirm：删除确认
//   - loadAlertSilences：加载静默规则列表
//   - showCreateSilenceModal/confirmCreateSilence：创建静默
//   - deleteAlertSilenceConfirm：删除静默
//   - loadNotifyChannels：加载通知渠道列表
//   - showCreateChannelModal/showEditChannelModal：创建/编辑渠道
//   - confirmCreateChannel/confirmEditChannel：提交
//   - deleteNotifyChannelConfirm/testNotifyChannelSend：删除/测试
//   - loadNotifyTemplates：加载通知模板列表
//   - showCreateTemplateModal/showEditTemplateModal：创建/编辑模板
//   - confirmCreateTemplate/confirmEditTemplate：提交
//   - deleteNotifyTemplateConfirm：删除
//
// 依赖：api.js、render.js（esc/escAttr/fmtTime）、icons.js、i18n.js

import * as api from './api.js';
import { esc, escAttr, fmtTime } from './render.js';
import { icon } from './icons.js';
import { t } from './i18n.js';

// ============================================================================
// 告警规则引擎（alertengine.AlertRule 多条件）
// ============================================================================

// loadAlertRulesEngine 加载告警规则列表并渲染到 #alertRulesList。
export function loadAlertRulesEngine() {
  api.getAlertRulesEngine().then(function (list) {
    renderAlertRulesList(list || []);
  }).catch(function (e) { api.apiFail('alertRulesList', e); });
}

// renderAlertRulesList 渲染告警规则列表。
function renderAlertRulesList(list) {
  const el = document.getElementById('alertRulesList');
  if (!el) return;
  if (!list.length) {
    el.innerHTML = '<p class="muted">' + esc(t('alertRules.empty')) + '</p>';
    return;
  }
  let html = '<table class="data-table"><thead><tr>'
    + '<th>' + esc(t('alertRules.name')) + '</th>'
    + '<th>' + esc(t('alertRules.conditions')) + '</th>'
    + '<th>' + esc(t('alertRules.logic')) + '</th>'
    + '<th>' + esc(t('alertRules.severity')) + '</th>'
    + '<th>' + esc(t('alertRules.enabled')) + '</th>'
    + '<th>' + esc(t('notifyChannels.status')) + '</th>'
    + '</tr></thead><tbody>';
  list.forEach(function (r) {
    const sevBadge = r.severity === 'critical' ? '<span class="badge fail">' + esc(t('alertRules.severityCritical')) + '</span>'
      : r.severity === 'warning' ? '<span class="badge warn">' + esc(t('alertRules.severityWarning')) + '</span>'
        : '<span class="badge info">' + esc(t('alertRules.severityInfo')) + '</span>';
    const enabledBadge = r.enabled ? '<span class="badge ok">' + esc(t('alertRules.enabled')) + '</span>' : '<span class="badge">' + esc(t('alertRules.disabled')) + '</span>';
    const condSummary = (r.conditions || []).map(function (c) {
      return esc(c.metric) + ' ' + esc(c.operator) + ' ' + c.threshold;
    }).join('；');
    html += '<tr>'
      + '<td><b>' + esc(r.name || r.id) + '</b><br><small class="muted">' + esc(r.id) + '</small></td>'
      + '<td><small>' + condSummary + '</small></td>'
      + '<td>' + esc(r.logic || 'AND') + '</td>'
      + '<td>' + sevBadge + '</td>'
      + '<td>' + enabledBadge + '</td>'
      + '<td>'
      + '<button class="btn xs" onclick="editAlertRuleEngine(\'' + escAttr(r.id) + '\')">' + icon('edit', 14) + ' ' + esc(t('alertRules.edit')) + '</button> '
      + '<button class="btn xs outline" onclick="deleteAlertRuleEngineConfirm(\'' + escAttr(r.id) + '\')">' + icon('delete', 14) + ' ' + esc(t('alertRules.delete')) + '</button>'
      + '</td>'
      + '</tr>';
  });
  html += '</tbody></table>';
  el.innerHTML = html;
}

// showCreateAlertRuleModal 显示创建告警规则弹窗。
export function showCreateAlertRuleModal() {
  showAlertRuleModal(null);
}

// showEditAlertRuleModal 显示编辑告警规则弹窗。
export function showEditAlertRuleModal(id) {
  api.getAlertRuleEngine(id).then(function (rule) {
    showAlertRuleModal(rule);
  }).catch(function (e) { alert(t('alertRules.loadFail') + e); });
}

// showAlertRuleModal 显示告警规则弹窗（rule 为 null 时创建，否则编辑）。
function showAlertRuleModal(rule) {
  const isEdit = !!rule;
  const r = rule || { name: '', conditions: [{ metric: 'cpu_usage', operator: '>', threshold: 80, window: 0 }], logic: 'AND', duration: 0, severity: 'warning', enabled: true, notifyChannels: [] };
  const modal = document.getElementById('alertRuleModal');
  if (!modal) return;
  // 构造条件编辑器
  let condHtml = '';
  (r.conditions || []).forEach(function (c, i) {
    condHtml += '<div class="cond-row" data-idx="' + i + '">'
      + '<input type="text" class="cond-metric" value="' + escAttr(c.metric || '') + '" placeholder="' + escAttr(t('alertRules.metric')) + '">'
      + '<select class="cond-op">'
      + ['>', '>=', '<', '<=', '==', '!='].map(function (op) {
        return '<option value="' + op + '"' + (c.operator === op ? ' selected' : '') + '>' + op + '</option>';
      }).join('')
      + '</select>'
      + '<input type="number" step="any" class="cond-threshold" value="' + (c.threshold || 0) + '" placeholder="' + escAttr(t('alertRules.threshold')) + '">'
      + '<input type="number" class="cond-window" value="' + (c.window || 0) + '" placeholder="' + escAttr(t('alertRules.window')) + '">'
      + '<button class="btn xs outline" onclick="this.parentElement.remove()">×</button>'
      + '</div>';
  });
  modal.innerHTML = '<div class="modal-backdrop" onclick="closeAlertRuleModal()"></div>'
    + '<div class="modal-card">'
    + '<h3>' + esc(isEdit ? t('alertRules.edit') : t('alertRules.create')) + '</h3>'
    + '<div class="form-row"><label>' + esc(t('alertRules.name')) + '</label><input type="text" id="alertRuleName" value="' + escAttr(r.name || '') + '"></div>'
    + '<div class="form-row"><label>' + esc(t('alertRules.conditions')) + '</label>'
    + '<div id="condList">' + condHtml + '</div>'
    + '<button class="btn xs" id="addCondBtn">' + esc(t('alertRules.addCondition')) + '</button>'
    + '</div>'
    + '<div class="form-row"><label>' + esc(t('alertRules.logic')) + '</label>'
    + '<select id="alertRuleLogic">'
    + '<option value="AND"' + (r.logic === 'AND' ? ' selected' : '') + '>' + esc(t('alertRules.logicAnd')) + '</option>'
    + '<option value="OR"' + (r.logic === 'OR' ? ' selected' : '') + '>' + esc(t('alertRules.logicOr')) + '</option>'
    + '<option value="NOT"' + (r.logic === 'NOT' ? ' selected' : '') + '>' + esc(t('alertRules.logicNot')) + '</option>'
    + '</select></div>'
    + '<div class="form-row"><label>' + esc(t('alertRules.duration')) + '</label><input type="number" id="alertRuleDuration" value="' + (r.duration || 0) + '"></div>'
    + '<div class="form-row"><label>' + esc(t('alertRules.severity')) + '</label>'
    + '<select id="alertRuleSeverity">'
    + '<option value="critical"' + (r.severity === 'critical' ? ' selected' : '') + '>' + esc(t('alertRules.severityCritical')) + '</option>'
    + '<option value="warning"' + (r.severity === 'warning' ? ' selected' : '') + '>' + esc(t('alertRules.severityWarning')) + '</option>'
    + '<option value="info"' + (r.severity === 'info' ? ' selected' : '') + '>' + esc(t('alertRules.severityInfo')) + '</option>'
    + '</select></div>'
    + '<div class="form-row"><label>' + esc(t('alertRules.enabled')) + '</label><input type="checkbox" id="alertRuleEnabled"' + (r.enabled ? ' checked' : '') + '></div>'
    + '<div class="form-row"><label>' + esc(t('alertRules.notifyChannels')) + '</label><input type="text" id="alertRuleChannels" value="' + escAttr((r.notifyChannels || []).join(',')) + '" placeholder="ch-xxx,ch-yyy"></div>'
    + '<div class="modal-actions">'
    + '<button class="btn primary" id="alertRuleSaveBtn">' + esc(t('alertRules.save')) + '</button> '
    + '<button class="btn ghost" onclick="closeAlertRuleModal()">' + esc(t('alertRules.cancel')) + '</button>'
    + '</div>'
    + '</div>';
  modal.style.display = 'block';
  // 绑定添加条件按钮
  const addBtn = document.getElementById('addCondBtn');
  if (addBtn) {
    addBtn.onclick = function () {
      const condList = document.getElementById('condList');
      const div = document.createElement('div');
      div.className = 'cond-row';
      div.innerHTML = '<input type="text" class="cond-metric" value="cpu_usage" placeholder="' + escAttr(t('alertRules.metric')) + '">'
        + '<select class="cond-op"><option value=">">&gt;</option><option value=">=">&gt;=</option><option value="<">&lt;</option><option value="<=">&lt;=</option><option value="==">==</option><option value="!=">!=</option></select>'
        + '<input type="number" step="any" class="cond-threshold" value="80" placeholder="' + escAttr(t('alertRules.threshold')) + '">'
        + '<input type="number" class="cond-window" value="0" placeholder="' + escAttr(t('alertRules.window')) + '">'
        + '<button class="btn xs outline" onclick="this.parentElement.remove()">×</button>';
      condList.appendChild(div);
    };
  }
  // 绑定保存按钮
  const saveBtn = document.getElementById('alertRuleSaveBtn');
  if (saveBtn) {
    saveBtn.onclick = function () { isEdit ? confirmEditAlertRule(id) : confirmCreateAlertRule(); };
  }
}

// collectAlertRuleForm 从弹窗收集表单数据。
function collectAlertRuleForm() {
  const name = document.getElementById('alertRuleName').value.trim();
  if (!name) { alert(t('alertRules.nameRequired')); return null; }
  const condRows = document.querySelectorAll('#condList .cond-row');
  if (!condRows.length) { alert(t('alertRules.conditionsRequired')); return null; }
  const conditions = [];
  condRows.forEach(function (row) {
    const metric = row.querySelector('.cond-metric').value.trim();
    if (!metric) { alert(t('alertRules.metricRequired')); return; }
    conditions.push({
      metric: metric,
      operator: row.querySelector('.cond-op').value,
      threshold: parseFloat(row.querySelector('.cond-threshold').value) || 0,
      window: parseInt(row.querySelector('.cond-window').value, 10) || 0,
    });
  });
  if (!conditions.length) return null;
  // window 转纳秒（Go time.Duration 单位为纳秒，前端输入秒）
  conditions.forEach(function (c) { c.window = c.window * 1000000000; });
  const durationSec = parseInt(document.getElementById('alertRuleDuration').value, 10) || 0;
  const channels = document.getElementById('alertRuleChannels').value.trim().split(',').filter(function (s) { return s.trim(); }).map(function (s) { return s.trim(); });
  return {
    name: name,
    conditions: conditions,
    logic: document.getElementById('alertRuleLogic').value,
    duration: durationSec * 1000000000, // 秒 → 纳秒
    severity: document.getElementById('alertRuleSeverity').value,
    enabled: document.getElementById('alertRuleEnabled').checked,
    notifyChannels: channels,
  };
}

// confirmCreateAlertRule 提交创建告警规则。
function confirmCreateAlertRule() {
  const body = collectAlertRuleForm();
  if (!body) return;
  api.createAlertRuleEngine(body).then(function (x) {
    if (x.s < 400) { closeAlertRuleModal(); loadAlertRulesEngine(); }
    else { alert(t('alertRules.saveFail') + (x.j.error || x.s)); }
  }).catch(function (e) { alert(t('alertRules.saveFail') + e); });
}

// confirmEditAlertRule 提交编辑告警规则。
function confirmEditAlertRule(id) {
  const body = collectAlertRuleForm();
  if (!body) return;
  api.updateAlertRuleEngine(id, body).then(function (x) {
    if (x.s < 400) { closeAlertRuleModal(); loadAlertRulesEngine(); }
    else { alert(t('alertRules.saveFail') + (x.j.error || x.s)); }
  }).catch(function (e) { alert(t('alertRules.saveFail') + e); });
}

// closeAlertRuleModal 关闭告警规则弹窗。
export function closeAlertRuleModal() {
  const modal = document.getElementById('alertRuleModal');
  if (modal) { modal.style.display = 'none'; modal.innerHTML = ''; }
}

// deleteAlertRuleEngineConfirm 删除告警规则确认。
export function deleteAlertRuleEngineConfirm(id) {
  if (!confirm(t('alertRules.deleteConfirm'))) return;
  api.deleteAlertRuleEngine(id).then(function (x) {
    if (x.s < 400) { loadAlertRulesEngine(); }
    else { alert(t('alertRules.deleteFail') + (x.j && x.j.error || x.s)); }
  }).catch(function (e) { alert(t('alertRules.deleteFail') + e); });
}

// ============================================================================
// 静默规则
// ============================================================================

// loadAlertSilences 加载静默规则列表。
export function loadAlertSilences() {
  api.getAlertSilences().then(function (list) {
    renderAlertSilencesList(list || []);
  }).catch(function (e) { api.apiFail('alertSilencesList', e); });
}

// renderAlertSilencesList 渲染静默规则列表。
function renderAlertSilencesList(list) {
  const el = document.getElementById('alertSilencesList');
  if (!el) return;
  if (!list.length) {
    el.innerHTML = '<p class="muted">' + esc(t('alertSilences.empty')) + '</p>';
    return;
  }
  let html = '<table class="data-table"><thead><tr>'
    + '<th>ID</th><th>' + esc(t('alertSilences.matchLabels')) + '</th><th>' + esc(t('alertSilences.startAt')) + '</th><th>' + esc(t('alertSilences.endAt')) + '</th><th>' + esc(t('alertSilences.reason')) + '</th><th>' + esc(t('notifyChannels.status')) + '</th>'
    + '</tr></thead><tbody>';
  list.forEach(function (s) {
    const labels = s.matchLabels ? JSON.stringify(s.matchLabels) : '{}';
    html += '<tr>'
      + '<td><small>' + esc(s.id) + '</small></td>'
      + '<td><code>' + esc(labels) + '</code></td>'
      + '<td><small>' + esc(s.startAt ? fmtTime(s.startAt) : '—') + '</small></td>'
      + '<td><small>' + esc(s.endAt ? fmtTime(s.endAt) : '—') + '</small></td>'
      + '<td>' + esc(s.reason || '—') + '</td>'
      + '<td><button class="btn xs outline" onclick="deleteAlertSilenceConfirm(\'' + escAttr(s.id) + '\')">' + esc(t('alertSilences.delete')) + '</button></td>'
      + '</tr>';
  });
  html += '</tbody></table>';
  el.innerHTML = html;
}

// showCreateSilenceModal 显示创建静默弹窗。
export function showCreateSilenceModal() {
  const modal = document.getElementById('alertSilenceModal');
  if (!modal) return;
  modal.innerHTML = '<div class="modal-backdrop" onclick="closeAlertSilenceModal()"></div>'
    + '<div class="modal-card">'
    + '<h3>' + esc(t('alertSilences.create')) + '</h3>'
    + '<div class="form-row"><label>' + esc(t('alertSilences.matchLabels')) + '</label><textarea id="silenceLabels" rows="3" placeholder=\'{"severity":"critical"}\'></textarea><small class="muted">' + esc(t('alertSilences.matchLabelsHint')) + '</small></div>'
    + '<div class="form-row"><label>' + esc(t('alertSilences.startAt')) + '</label><input type="datetime-local" id="silenceStart"></div>'
    + '<div class="form-row"><label>' + esc(t('alertSilences.endAt')) + '</label><input type="datetime-local" id="silenceEnd"></div>'
    + '<div class="form-row"><label>' + esc(t('alertSilences.reason')) + '</label><input type="text" id="silenceReason"></div>'
    + '<div class="modal-actions"><button class="btn primary" id="silenceSaveBtn">' + esc(t('alertSilences.save')) + '</button> <button class="btn ghost" onclick="closeAlertSilenceModal()">' + esc(t('alertRules.cancel')) + '</button></div>'
    + '</div>';
  modal.style.display = 'block';
  const saveBtn = document.getElementById('silenceSaveBtn');
  if (saveBtn) saveBtn.onclick = confirmCreateSilence;
}

// confirmCreateSilence 提交创建静默。
function confirmCreateSilence() {
  const labelsStr = document.getElementById('silenceLabels').value.trim();
  let matchLabels = {};
  if (labelsStr) {
    try { matchLabels = JSON.parse(labelsStr); } catch (e) { alert(t('alertSilences.saveFail') + 'invalid JSON'); return; }
  }
  const startAt = document.getElementById('silenceStart').value;
  const endAt = document.getElementById('silenceEnd').value;
  const reason = document.getElementById('silenceReason').value.trim();
  api.createAlertSilence({ matchLabels: matchLabels, startAt: startAt || null, endAt: endAt || null, reason: reason }).then(function (x) {
    if (x.s < 400) { closeAlertSilenceModal(); loadAlertSilences(); }
    else { alert(t('alertSilences.saveFail') + (x.j.error || x.s)); }
  }).catch(function (e) { alert(t('alertSilences.saveFail') + e); });
}

// closeAlertSilenceModal 关闭静默弹窗。
export function closeAlertSilenceModal() {
  const modal = document.getElementById('alertSilenceModal');
  if (modal) { modal.style.display = 'none'; modal.innerHTML = ''; }
}

// deleteAlertSilenceConfirm 删除静默确认。
export function deleteAlertSilenceConfirm(id) {
  if (!confirm(t('alertSilences.deleteConfirm'))) return;
  api.deleteAlertSilence(id).then(function (x) {
    if (x.s < 400) { loadAlertSilences(); }
    else { alert(t('alertSilences.deleteFail') + (x.j && x.j.error || x.s)); }
  }).catch(function (e) { alert(t('alertSilences.deleteFail') + e); });
}

// ============================================================================
// 通知渠道
// ============================================================================

// loadNotifyChannels 加载通知渠道列表。
export function loadNotifyChannels() {
  api.getNotifyChannels().then(function (list) {
    renderNotifyChannelsList(list || []);
  }).catch(function (e) { api.apiFail('notifyChannelsList', e); });
}

// renderNotifyChannelsList 渲染通知渠道列表。
function renderNotifyChannelsList(list) {
  const el = document.getElementById('notifyChannelsList');
  if (!el) return;
  if (!list.length) {
    el.innerHTML = '<p class="muted">' + esc(t('notifyChannels.empty')) + '</p>';
    return;
  }
  const typeLabels = { dingtalk: 'typeDingtalk', wecom: 'typeWecom', feishu: 'typeFeishu', slack: 'typeSlack', email: 'typeEmail', webhook: 'typeWebhook' };
  let html = '<table class="data-table"><thead><tr>'
    + '<th>' + esc(t('notifyChannels.name')) + '</th><th>' + esc(t('notifyChannels.type')) + '</th><th>' + esc(t('notifyChannels.enabled')) + '</th><th>' + esc(t('notifyChannels.status')) + '</th>'
    + '</tr></thead><tbody>';
  list.forEach(function (c) {
    const typeLabel = typeLabels[c.type] ? t('notifyChannels.' + typeLabels[c.type]) : c.type;
    const enabledBadge = c.enabled ? '<span class="badge ok">' + esc(t('alertRules.enabled')) + '</span>' : '<span class="badge">' + esc(t('alertRules.disabled')) + '</span>';
    html += '<tr>'
      + '<td><b>' + esc(c.name) + '</b><br><small class="muted">' + esc(c.id) + '</small></td>'
      + '<td>' + esc(typeLabel) + '</td>'
      + '<td>' + enabledBadge + '</td>'
      + '<td>'
      + '<button class="btn xs" onclick="editNotifyChannel(\'' + escAttr(c.id) + '\')">' + icon('edit', 14) + ' ' + esc(t('notifyChannels.edit')) + '</button> '
      + '<button class="btn xs outline" onclick="testNotifyChannelSend(\'' + escAttr(c.id) + '\')">' + icon('send', 14) + ' ' + esc(t('notifyChannels.test')) + '</button> '
      + '<button class="btn xs outline" onclick="deleteNotifyChannelConfirm(\'' + escAttr(c.id) + '\')">' + icon('delete', 14) + ' ' + esc(t('notifyChannels.delete')) + '</button>'
      + '</td>'
      + '</tr>';
  });
  html += '</tbody></table>';
  el.innerHTML = html;
}

// showCreateChannelModal 显示创建渠道弹窗。
export function showCreateChannelModal() {
  showChannelModal(null);
}

// showEditChannelModal 显示编辑渠道弹窗。
export function showEditChannelModal(id) {
  // 编辑需获取完整 Config（列表已脱敏），此处直接用列表数据或重新获取
  // 简化：用列表数据（Config 已脱敏，编辑时显示脱敏值，用户可重新填写）
  api.getNotifyChannels().then(function (list) {
    const c = (list || []).find(function (x) { return x.id === id; });
    showChannelModal(c);
  }).catch(function (e) { alert(t('notifyChannels.saveFail') + e); });
}

// showChannelModal 显示渠道弹窗。
function showChannelModal(channel) {
  const isEdit = !!channel;
  const c = channel || { name: '', type: 'dingtalk', config: '', enabled: true };
  const modal = document.getElementById('notifyChannelModal');
  if (!modal) return;
  modal.innerHTML = '<div class="modal-backdrop" onclick="closeNotifyChannelModal()"></div>'
    + '<div class="modal-card">'
    + '<h3>' + esc(isEdit ? t('notifyChannels.edit') : t('notifyChannels.create')) + '</h3>'
    + '<div class="form-row"><label>' + esc(t('notifyChannels.name')) + '</label><input type="text" id="channelName" value="' + escAttr(c.name || '') + '"></div>'
    + '<div class="form-row"><label>' + esc(t('notifyChannels.type')) + '</label>'
    + '<select id="channelType">'
    + ['dingtalk', 'wecom', 'feishu', 'slack', 'email', 'webhook'].map(function (tp) {
      const labels = { dingtalk: 'typeDingtalk', wecom: 'typeWecom', feishu: 'typeFeishu', slack: 'typeSlack', email: 'typeEmail', webhook: 'typeWebhook' };
      return '<option value="' + tp + '"' + (c.type === tp ? ' selected' : '') + '>' + esc(t('notifyChannels.' + labels[tp])) + '</option>';
    }).join('')
    + '</select></div>'
    + '<div class="form-row"><label>' + esc(t('notifyChannels.config')) + '</label><textarea id="channelConfig" rows="4">' + escAttr(c.config || '') + '</textarea><small class="muted">' + esc(t('notifyChannels.configHint')) + '</small></div>'
    + '<div class="form-row"><label>' + esc(t('notifyChannels.enabled')) + '</label><input type="checkbox" id="channelEnabled"' + (c.enabled ? ' checked' : '') + '></div>'
    + '<div class="modal-actions"><button class="btn primary" id="channelSaveBtn">' + esc(t('notifyChannels.save')) + '</button> <button class="btn ghost" onclick="closeNotifyChannelModal()">' + esc(t('notifyChannels.cancel')) + '</button></div>'
    + '</div>';
  modal.style.display = 'block';
  const saveBtn = document.getElementById('channelSaveBtn');
  if (saveBtn) saveBtn.onclick = function () { isEdit ? confirmEditChannel(c.id) : confirmCreateChannel(); };
}

// collectChannelForm 收集渠道表单。
function collectChannelForm() {
  const name = document.getElementById('channelName').value.trim();
  if (!name) { alert(t('notifyChannels.nameRequired')); return null; }
  return {
    name: name,
    type: document.getElementById('channelType').value,
    config: document.getElementById('channelConfig').value.trim(),
    enabled: document.getElementById('channelEnabled').checked,
  };
}

// confirmCreateChannel 提交创建渠道。
function confirmCreateChannel() {
  const body = collectChannelForm();
  if (!body) return;
  api.createNotifyChannel(body).then(function (x) {
    if (x.s < 400) { closeNotifyChannelModal(); loadNotifyChannels(); }
    else { alert(t('notifyChannels.saveFail') + (x.j.error || x.s)); }
  }).catch(function (e) { alert(t('notifyChannels.saveFail') + e); });
}

// confirmEditChannel 提交编辑渠道。
function confirmEditChannel(id) {
  const body = collectChannelForm();
  if (!body) return;
  api.updateNotifyChannel(id, body).then(function (x) {
    if (x.s < 400) { closeNotifyChannelModal(); loadNotifyChannels(); }
    else { alert(t('notifyChannels.saveFail') + (x.j.error || x.s)); }
  }).catch(function (e) { alert(t('notifyChannels.saveFail') + e); });
}

// closeNotifyChannelModal 关闭渠道弹窗。
export function closeNotifyChannelModal() {
  const modal = document.getElementById('notifyChannelModal');
  if (modal) { modal.style.display = 'none'; modal.innerHTML = ''; }
}

// deleteNotifyChannelConfirm 删除渠道确认。
export function deleteNotifyChannelConfirm(id) {
  if (!confirm(t('notifyChannels.deleteConfirm'))) return;
  api.deleteNotifyChannel(id).then(function (x) {
    if (x.s < 400) { loadNotifyChannels(); }
    else { alert(t('notifyChannels.deleteFail') + (x.j && x.j.error || x.s)); }
  }).catch(function (e) { alert(t('notifyChannels.deleteFail') + e); });
}

// testNotifyChannelSend 测试发送。
export function testNotifyChannelSend(id) {
  api.testNotifyChannel(id).then(function (x) {
    if (x.s < 400 && x.j && x.j.status === 'ok') { alert(t('notifyChannels.testOk')); }
    else { alert(t('notifyChannels.testFail') + (x.j && (x.j.error || x.j.message) || x.s)); }
  }).catch(function (e) { alert(t('notifyChannels.testFail') + e); });
}

// ============================================================================
// 通知模板
// ============================================================================

// loadNotifyTemplates 加载通知模板列表。
export function loadNotifyTemplates() {
  api.getNotifyTemplates().then(function (list) {
    renderNotifyTemplatesList(list || []);
  }).catch(function (e) { api.apiFail('notifyTemplatesList', e); });
}

// renderNotifyTemplatesList 渲染通知模板列表。
function renderNotifyTemplatesList(list) {
  const el = document.getElementById('notifyTemplatesList');
  if (!el) return;
  if (!list.length) {
    el.innerHTML = '<p class="muted">' + esc(t('notifyTemplates.empty')) + '</p>';
    return;
  }
  let html = '<table class="data-table"><thead><tr>'
    + '<th>' + esc(t('notifyTemplates.name')) + '</th><th>' + esc(t('notifyTemplates.type')) + '</th><th>' + esc(t('notifyTemplates.format')) + '</th><th>' + esc(t('notifyChannels.status')) + '</th>'
    + '</tr></thead><tbody>';
  list.forEach(function (tp) {
    html += '<tr>'
      + '<td><b>' + esc(tp.name) + '</b><br><small class="muted">' + esc(tp.id) + '</small></td>'
      + '<td>' + esc(tp.type) + '</td>'
      + '<td>' + esc(tp.format) + '</td>'
      + '<td>'
      + '<button class="btn xs" onclick="editNotifyTemplate(\'' + escAttr(tp.id) + '\')">' + icon('edit', 14) + ' ' + esc(t('notifyTemplates.edit')) + '</button> '
      + '<button class="btn xs outline" onclick="deleteNotifyTemplateConfirm(\'' + escAttr(tp.id) + '\')">' + icon('delete', 14) + ' ' + esc(t('notifyTemplates.delete')) + '</button>'
      + '</td>'
      + '</tr>';
  });
  html += '</tbody></table>';
  el.innerHTML = html;
}

// showCreateTemplateModal 显示创建模板弹窗。
export function showCreateTemplateModal() {
  showTemplateModal(null);
}

// showEditTemplateModal 显示编辑模板弹窗。
export function showEditTemplateModal(id) {
  api.getNotifyTemplates().then(function (list) {
    const tp = (list || []).find(function (x) { return x.id === id; });
    showTemplateModal(tp);
  }).catch(function (e) { alert(t('notifyTemplates.saveFail') + e); });
}

// showTemplateModal 显示模板弹窗。
function showTemplateModal(template) {
  const isEdit = !!template;
  const tp = template || { name: '', type: 'alert', title: '', body: '', format: 'markdown' };
  const modal = document.getElementById('notifyTemplateModal');
  if (!modal) return;
  modal.innerHTML = '<div class="modal-backdrop" onclick="closeNotifyTemplateModal()"></div>'
    + '<div class="modal-card">'
    + '<h3>' + esc(isEdit ? t('notifyTemplates.edit') : t('notifyTemplates.create')) + '</h3>'
    + '<div class="form-row"><label>' + esc(t('notifyTemplates.name')) + '</label><input type="text" id="tplName" value="' + escAttr(tp.name || '') + '"></div>'
    + '<div class="form-row"><label>' + esc(t('notifyTemplates.type')) + '</label>'
    + '<select id="tplType">'
    + [['alert', 'typeAlert'], ['task', 'typeTask'], ['device', 'typeDevice'], ['system', 'typeSystem']].map(function (x) {
      return '<option value="' + x[0] + '"' + (tp.type === x[0] ? ' selected' : '') + '>' + esc(t('notifyTemplates.' + x[1])) + '</option>';
    }).join('')
    + '</select></div>'
    + '<div class="form-row"><label>' + esc(t('notifyTemplates.titleField')) + '</label><input type="text" id="tplTitle" value="' + escAttr(tp.title || '') + '"></div>'
    + '<div class="form-row"><label>' + esc(t('notifyTemplates.body')) + '</label><textarea id="tplBody" rows="5">' + escAttr(tp.body || '') + '</textarea></div>'
    + '<div class="form-row"><label>' + esc(t('notifyTemplates.format')) + '</label>'
    + '<select id="tplFormat">'
    + [['markdown', 'formatMarkdown'], ['text', 'formatText'], ['html', 'formatHtml']].map(function (x) {
      return '<option value="' + x[0] + '"' + (tp.format === x[0] ? ' selected' : '') + '>' + esc(t('notifyTemplates.' + x[1])) + '</option>';
    }).join('')
    + '</select></div>'
    + '<div class="modal-actions"><button class="btn primary" id="tplSaveBtn">' + esc(t('notifyTemplates.save')) + '</button> <button class="btn ghost" onclick="closeNotifyTemplateModal()">' + esc(t('notifyTemplates.cancel')) + '</button></div>'
    + '</div>';
  modal.style.display = 'block';
  const saveBtn = document.getElementById('tplSaveBtn');
  if (saveBtn) saveBtn.onclick = function () { isEdit ? confirmEditTemplate(tp.id) : confirmCreateTemplate(); };
}

// collectTemplateForm 收集模板表单。
function collectTemplateForm() {
  const name = document.getElementById('tplName').value.trim();
  if (!name) { alert(t('notifyTemplates.nameRequired')); return null; }
  return {
    name: name,
    type: document.getElementById('tplType').value,
    title: document.getElementById('tplTitle').value,
    body: document.getElementById('tplBody').value,
    format: document.getElementById('tplFormat').value,
  };
}

// confirmCreateTemplate 提交创建模板。
function confirmCreateTemplate() {
  const body = collectTemplateForm();
  if (!body) return;
  api.createNotifyTemplate(body).then(function (x) {
    if (x.s < 400) { closeNotifyTemplateModal(); loadNotifyTemplates(); }
    else { alert(t('notifyTemplates.saveFail') + (x.j.error || x.s)); }
  }).catch(function (e) { alert(t('notifyTemplates.saveFail') + e); });
}

// confirmEditTemplate 提交编辑模板。
function confirmEditTemplate(id) {
  const body = collectTemplateForm();
  if (!body) return;
  api.updateNotifyTemplate(id, body).then(function (x) {
    if (x.s < 400) { closeNotifyTemplateModal(); loadNotifyTemplates(); }
    else { alert(t('notifyTemplates.saveFail') + (x.j.error || x.s)); }
  }).catch(function (e) { alert(t('notifyTemplates.saveFail') + e); });
}

// closeNotifyTemplateModal 关闭模板弹窗。
export function closeNotifyTemplateModal() {
  const modal = document.getElementById('notifyTemplateModal');
  if (modal) { modal.style.display = 'none'; modal.innerHTML = ''; }
}

// deleteNotifyTemplateConfirm 删除模板确认。
export function deleteNotifyTemplateConfirm(id) {
  if (!confirm(t('notifyTemplates.deleteConfirm'))) return;
  api.deleteNotifyTemplate(id).then(function (x) {
    if (x.s < 400) { loadNotifyTemplates(); }
    else { alert(t('notifyTemplates.deleteFail') + (x.j && x.j.error || x.s)); }
  }).catch(function (e) { alert(t('notifyTemplates.deleteFail') + e); });
}

// ============================================================================
// 通知配置页面统一加载（switchTab 时调用）
// ============================================================================

// loadNotifyConfig 加载通知配置页面：渠道 + 模板。
export function loadNotifyConfig() {
  loadNotifyChannels();
  loadNotifyTemplates();
}

// loadAlertRulesPage 加载告警规则页面：规则 + 静默。
export function loadAlertRulesPage() {
  loadAlertRulesEngine();
  loadAlertSilences();
}