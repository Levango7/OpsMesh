// flow_deploys.js — 部署中心（M3）
// 从 flow.js 拆分（P2-1）。职责：部署列表轮询、执行/回滚、详情查看、表单提交。
// 依赖：api.js、render.js（esc/escAttr/fmtTime/dpStatusPill）、icons.js、i18n.js、
//       flow_focus.js（applyFocus/getFocusDevice）。

import * as api from './api.js';
import { esc, escAttr, fmtTime, dpStatusPill } from './render.js';
import { icon } from './icons.js';
import { t } from './i18n.js';
import { applyFocus, getFocusDevice } from './flow_focus.js';

// ---------- 部署中心（M3） ----------
export function deployMsg(s, ok) { const el = document.getElementById('deployMsg'); if (el) { el.className = 'msg ' + (ok ? 'ok' : 'err'); el.textContent = (ok ? '[ok] ' : '[err] ') + s; } }

export function loadDeployDemo() {
  document.getElementById('dpName').value = 'deploy-nginx';
  document.getElementById('dpType').value = 'script';
  document.getElementById('dpRepo').value = 'https://git.example.com/ops/nginx-deploy.git';
  document.getElementById('dpContent').value = '';
  document.getElementById('dpPath').value = '';
  document.getElementById('dpTargets').value = 'dev-10.0.0.1, dev-10.0.0.2';
  deployMsg(t('flow.msg.demoLoadedDeploy'), true);
}

export function pollDeploys() {
  const sf = document.getElementById('dpStatusFilter');
  const st = sf ? sf.value : '';
  api.getDeploys(st)
    .then(function (list) {
      const fl = applyFocus(list || [], 'deploy');
      const focusDevice = getFocusDevice();
      const note = focusDevice ? '<p class="hint">' + icon('context', 14) + ' ' + t('flow.msg.filteredByDevice', { id: '<code>' + esc(focusDevice.id) + '</code>', n: fl.length }) + '</p>' : '';
      if (!fl || fl.length === 0) { document.getElementById('deployList').innerHTML = note + '<p class="muted">' + esc(t('flow.msg.noDeploy')) + '</p>'; return; }
      let html = note + '<div class="table-wrap"><table><colgroup><col style="width:16%"><col style="width:18%"><col style="width:12%"><col style="width:24%"><col style="width:12%"><col style="width:18%"></colgroup><thead><tr><th>' + esc(t('cmdb.col.id')) + '</th><th>' + esc(t('cmdb.col.name')) + '</th><th>' + esc(t('cmdb.col.type')) + '</th><th>' + esc(t('cmdb.col.target')) + '</th><th>' + esc(t('cmdb.col.status')) + '</th><th>' + esc(t('cmdb.col.action')) + '</th></tr></thead><tbody>';
      fl.forEach(function (d) {
        const targets = (d.target_ids || '').replace(/,/g, ', ');
        html += '<tr><td><code title="' + esc(d.id) + '">' + esc(d.id) + '</code></td><td>' + esc(d.name) + '</td><td>' + esc(d.type) + '</td>'
          + '<td><code title="' + esc(targets) + '">' + esc(targets) + '</code></td><td>' + dpStatusPill(d.status) + '</td>'
          + '<td class="row-actions-cell"><button onclick="execDeploy(' + escAttr(d.id) + ')">' + esc(t('deploy.action.execute')) + '</button> <button onclick="rollbackDeploy(' + escAttr(d.id) + ')">' + esc(t('deploy.action.rollback')) + '</button> <button onclick="openDeploy(' + escAttr(d.id) + ')">' + esc(t('deploy.action.detail')) + '</button></td></tr>';
      });
      html += '</tbody></table></div>';
      document.getElementById('deployList').innerHTML = html;
    }).catch(function (e) { api.apiFail('deploys', e); });
}

export function execDeploy(id) {
  api.executeDeploy(id)
    .then(function (x) { deployMsg('[' + x.s + '] ' + (x.j.error || t('flow.msg.execTriggered', { id: id })), x.s < 400); pollDeploys(); })
    .catch(function (e) { deployMsg(t('flow.msg.errorPrefix', { err: e }), false); });
}

export function rollbackDeploy(id) {
  api.rollbackDeploy(id)
    .then(function (x) { deployMsg('[' + x.s + '] ' + (x.j.error || t('flow.msg.rolledBack', { id: id })), x.s < 400); pollDeploys(); })
    .catch(function (e) { deployMsg(t('flow.msg.errorPrefix', { err: e }), false); });
}

export function openDeploy(id) {
  api.getDeploy(id).then(function (d) {
    let h = '<h3>' + esc(t('deploy.detail.heading', { id: d.id, name: d.name })) + '</h3>';
    h += '<p>' + t('deploy.detail.typeStatus', { type: esc(d.type), status: '' }) + dpStatusPill(d.status) + '</p>';
    h += '<p>' + t('deploy.detail.targets', { targets: '<code>' + esc((d.target_ids || '').replace(/,/g, ', ')) + '</code>' }) + '</p>';
    if (d.repo_url) h += '<p>' + t('deploy.detail.repo', { repo: '<code>' + esc(d.repo_url) + '</code>' }) + '</p>';
    if (d.path) h += '<p>' + t('deploy.detail.path', { path: '<code>' + esc(d.path) + '</code>' }) + '</p>';
    if (d.content) h += '<p>' + t('deploy.detail.content', { content: '<code>' + esc(d.content) + '</code>' }) + '</p>';
    h += '<p class="muted">' + t('deploy.detail.creator', { creator: esc(d.created_by), created: fmtTime(d.created_at), updated: fmtTime(d.updated_at) }) + '</p>';
    if (d.task_ids) h += '<p>' + t('deploy.detail.taskIds', { ids: '<code>' + esc((d.task_ids || '').replace(/,/g, ', ')) + '</code>' }) + '</p>';
    document.getElementById('drawerBody').innerHTML = h;
    document.getElementById('drawer').classList.add('open');
  }).catch(function (e) { console.error(e); });
}

export function submitDeployForm() {
  const body = {
    name: document.getElementById('dpName').value.trim(),
    type: document.getElementById('dpType').value,
    repo_url: document.getElementById('dpRepo').value.trim(),
    content: document.getElementById('dpContent').value,
    path: document.getElementById('dpPath').value.trim(),
    target_ids: document.getElementById('dpTargets').value.trim(),
  };
  if (!body.name || !body.target_ids) { deployMsg(t('flow.msg.needNameTargets'), false); return; }
  api.createDeploy(body)
    .then(function (x) { deployMsg('[' + x.s + '] ' + (x.j.error || t('flow.msg.registered', { id: (x.j && x.j.id) })), x.s < 400); if (x.s < 400) pollDeploys(); })
    .catch(function (err) { deployMsg(t('flow.msg.errorPrefix', { err: err }), false); });
}