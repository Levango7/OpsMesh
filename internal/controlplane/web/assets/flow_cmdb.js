// flow_cmdb.js — CMDB 操作
// 从 flow.js 拆分（P2-1）。职责：CMDB 类型加载、CI 列表轮询、属性模板轮询、
//       CI 关系图查看、CI 表单提交。
// 依赖：api.js、render.js（esc/escAttr）、i18n.js。

import * as api from './api.js';
import { esc, escAttr } from './render.js';
import { t } from './i18n.js';

// ---------- CMDB ----------
export function loadCMDBTypes() {
  api.getCMDBTypes().then(function (ts) {
    const ft = document.getElementById('ciTypeFilter'), nt = document.getElementById('ciTypeNew'), tt = document.getElementById('tmplTypeFilter');
    [ft, nt, tt].forEach(function (sel) { if (!sel) return; sel.innerHTML = '<option value="">' + esc(t('cmdb.selectTypeFirst')) + '</option>'; });
    (ts || []).forEach(function (t) {
      [ft, nt, tt].forEach(function (sel) { if (!sel) return; const o = document.createElement('option'); o.value = t.name; o.textContent = t.displayName + ' (' + t.name + ')'; sel.appendChild(o); });
    });
    if (ft) ft.addEventListener('change', pollCIs);
    if (tt) tt.addEventListener('change', pollTemplates);
  }).catch(function (e) { console.error(e); });
}

export function pollCIs() {
  const sel = document.getElementById('ciTypeFilter');
  const t = sel ? sel.value : '';
  if (!t) { document.getElementById('ciList').innerHTML = '<p class="muted">' + esc(t('flow.msg.ciTypeRequired')) + '</p>'; return; }
  api.getCIs(t).then(function (list) {
    if (!list || list.length === 0) { document.getElementById('ciList').innerHTML = '<p class="muted">' + esc(t('flow.msg.noCiForType')) + '</p>'; return; }
    let html = '<div class="table-wrap"><table><colgroup><col style="width:24%"><col style="width:24%"><col style="width:16%"><col style="width:18%"><col style="width:18%"></colgroup><thead><tr><th>' + esc(t('cmdb.col.id')) + '</th><th>' + esc(t('cmdb.col.name')) + '</th><th>' + esc(t('cmdb.col.status')) + '</th><th>' + esc(t('cmdb.col.source')) + '</th><th>' + esc(t('cmdb.col.version')) + '</th></tr></thead><tbody>';
    list.forEach(function (c) {
      html += '<tr class="ci" onclick="openCI(\'' + escAttr(c.id) + '\')"><td><code title="' + esc(c.id) + '">' + esc(c.id) + '</code></td><td>' + esc(c.name) + '</td><td>' + esc(c.status) + '</td><td>' + esc(c.source) + '</td><td>' + esc(c.version) + '</td></tr>';
    });
    html += '</tbody></table></div>';
    document.getElementById('ciList').innerHTML = html;
  }).catch(function (e) { console.error(e); });
}

export function pollTemplates() {
  const sel = document.getElementById('tmplTypeFilter');
  const t = sel ? sel.value : '';
  if (!t) { document.getElementById('tmplList').innerHTML = '<p class="muted">' + esc(t('flow.msg.ciTypeRequired')) + '</p>'; return; }
  api.getAttrTemplates(t).then(function (list) {
    if (!list || list.length === 0) { document.getElementById('tmplList').innerHTML = '<p class="muted">' + esc(t('flow.msg.noTemplateForType')) + '</p>'; return; }
    let html = '<div class="table-wrap"><table><colgroup><col style="width:25%"><col style="width:35%"><col style="width:25%"><col style="width:15%"></colgroup><thead><tr><th>Key</th><th>' + esc(t('cmdb.col.label')) + '</th><th>' + esc(t('cmdb.col.type')) + '</th><th>' + esc(t('cmdb.col.required')) + '</th></tr></thead><tbody>';
    list.forEach(function (x) {
      html += '<tr><td><code title="' + esc(x.attrKey) + '">' + esc(x.attrKey) + '</code></td><td>' + esc(x.label) + '</td><td>' + esc(x.attrType) + '</td><td>' + (x.required ? esc(t('metric.yes')) : esc(t('metric.no'))) + '</td></tr>';
    });
    html += '</tbody></table></div>';
    document.getElementById('tmplList').innerHTML = html;
  }).catch(function (e) { console.error(e); });
}

export function openCI(id) {
  api.getCIGraph(id).then(function (g) {
    if (g.error) { document.getElementById('ciDetail').innerHTML = '<p class="msg err">' + esc(g.error) + '</p>'; return; }
    const c = g.centerCI || {};
    let h = '<h4>' + esc(c.name) + ' <small class="muted">(' + esc(c.ciType) + ' / ' + esc(c.id) + ')</small></h4>';
    h += '<p>' + esc(t('cmdb.graph.stateSourceVersion', { state: c.status, source: c.source, version: c.version })) + '</p>';
    if (c.attrs && Object.keys(c.attrs).length) {
      h += '<p>' + esc(t('cmdb.graph.attrs')) + ' ' + Object.keys(c.attrs).map(function (k) { return '<code>' + esc(k) + '</code>=' + esc(c.attrs[k]); }).join('，') + '</p>';
    }
    h += '<h4>' + esc(t('cmdb.graph.relations', { n: g.relations ? g.relations.length : 0 })) + '</h4>';
    if (!g.relations || g.relations.length === 0) { h += '<p class="muted">' + esc(t('cmdb.graph.noRelation')) + '</p>'; }
    else {
      g.relations.forEach(function (r) {
        h += '<div class="rel"><b>' + esc(r.relationType) + '</b> → ' + esc(r.targetName) + ' <small class="muted">(' + esc(r.targetType) + ')</small></div>';
      });
    }
    document.getElementById('ciDetail').innerHTML = h;
  }).catch(function (e) { console.error(e); });
}

export function cmdbMsg(s, ok) {
  const el = document.getElementById('cmdbMsg');
  el.className = 'msg ' + (ok ? 'ok' : 'err');
  el.textContent = (ok ? '[ok] ' : '[err] ') + s;
}

export function submitCIForm() {
  const typeSel = document.getElementById('ciTypeNew');
  const type = typeSel ? typeSel.value : '';
  let attrs = {};
  const raw = document.getElementById('ciAttrs').value.trim();
  if (raw) { try { attrs = JSON.parse(raw); } catch (err) { cmdbMsg(t('flow.msg.attrJsonError', { err: err }), false); return; } }
  const body = { ciType: type, name: document.getElementById('ciName').value, attrs: attrs };
  api.createCI(body)
    .then(function (x) { cmdbMsg('[' + x.s + '] ' + JSON.stringify(x.j), x.s < 400); pollCIs(); })
    .catch(function (err) { cmdbMsg(t('flow.msg.errorPrefix', { err: err }), false); });
}