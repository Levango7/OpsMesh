// flow.js — 纳管流程与任务下发编排
// 职责：纳管操作流程、批量下发、取消操作、告警 ack/silence、CMDB/作业流/部署/日志/告警业务编排、
//       DAG 画布交互、跨模块联动（focus）。
// 调用 api.js 的函数并触发 render.js / poll.js 重渲染。

import * as api from './api.js';
import { esc, fmtTime, paintStats, dpStatusPill, logLevelPill, setRenderDeps } from './render.js';
import { pollDevices, pollTasks, pollAlerts } from './poll.js';
import { icon } from './icons.js';
import { t } from './i18n.js';

// ---------- 跨模块联动（F1）：focus 状态 ----------
let focusDevice = null;

export function getFocusDevice() { return focusDevice; }

export function setFocus(id, ip, agentID, segment) {
  focusDevice = { id: id, ip: ip || '', agentID: agentID || '', segment: segment || '' };
  const b = document.getElementById('ctxbar'); if (b) b.classList.add('show');
  const d = document.getElementById('ctxDev'); if (d) d.textContent = id + (ip ? (' (' + ip + ')') : '');
}

export function clearFocus() {
  focusDevice = null;
  const b = document.getElementById('ctxbar'); if (b) b.classList.remove('show');
  pollTasks(); pollAlertsFull(); pollDeploys();
}

export function applyFocus(list, kind) {
  if (!focusDevice || !list) return list;
  return list.filter(function (x) {
    if (kind === 'task') return x.agentID === focusDevice.agentID;
    if (kind === 'alert') return (x.deviceID === focusDevice.id) || (x.agentID === focusDevice.agentID);
    if (kind === 'deploy') { const ts = ((x.target_ids || '') + '').split(/[,\s]+/).map(function (s) { return s.trim(); }).filter(Boolean); return ts.indexOf(focusDevice.id) >= 0; }
    return true;
  });
}

export function jumpFocus(tab) {
  if (!focusDevice) return;
  if (tab === 'logs') { switchTab('logs'); const di = document.getElementById('logDevice'); if (di) di.value = focusDevice.id; searchLogs(0); return; }
  if (tab === 'cmdb') { focusCI(); return; }
  switchTab(tab);
}

export function focusCI() {
  switchTab('cmdb');
  if (!focusDevice) return;
  api.getCMDBTypes().then(function (ts) {
    Promise.all((ts || []).map(function (t) {
      return api.getCIs(t.type).then(function (arr) { return (arr || []).filter(function (c) { return c.deviceID === focusDevice.id; }); });
    })).then(function (groups) {
      const all = []; groups.forEach(function (g) { all.push.apply(all, g); });
      const el = document.getElementById('ciList');
      if (!all.length) { el.innerHTML = '<p class="muted">配置库中无关联该设备的配置项。</p>'; return; }
      let html = '<p class="hint">' + icon('context', 14) + ' 已按设备 <code>' + esc(focusDevice.id) + '</code> 过滤（' + all.length + ' 条）</p>';
      html += '<div class="table-wrap"><table><colgroup><col style="width:30%"><col style="width:30%"><col style="width:20%"><col style="width:20%"></colgroup><thead><tr><th>ID</th><th>名称</th><th>类型</th><th>状态</th></tr></thead><tbody>';
      all.forEach(function (c) { html += '<tr class="ci" onclick="openCI(\'' + esc(c.id) + '\')"><td><code title="' + esc(c.id) + '">' + esc(c.id) + '</code></td><td>' + esc(c.name) + '</td><td>' + esc(c.ciType) + '</td><td>' + esc(c.status) + '</td></tr>'; });
      html += '</tbody></table></div>';
      el.innerHTML = html;
    }).catch(function (e) { console.error(e); });
  }).catch(function (e) { console.error(e); });
}

// 将 focus 相关回调注入 render.js（避免循环依赖）
setRenderDeps({ setFocus: setFocus, openDevice: null, focusDevice: getFocusDevice, applyFocus: applyFocus });

// ---------- 标签切换 ----------
export function switchTab(name) {
  ['home', 'ops', 'cmdb', 'osopt', 'mwdep', 'deploy', 'flow', 'logs', 'alerts', 'users', 'roles', 'permission', 'audits', 'settings', 'docs'].forEach(function (t) {
    const p = document.getElementById('tab-' + t); if (p) p.classList.toggle('active', t === name);
    const b = document.getElementById('tab-' + t + '-btn'); if (b) b.classList.toggle('active', t === name);
  });
  if (name === 'ops') { pollTasks(); }
  if (name === 'cmdb') { loadCMDBTypes(); }
  if (name === 'osopt') { loadOSTemplates(); }
  if (name === 'mwdep') { loadMiddlewareTemplates(); loadMiddlewareInstances(); }
  if (name === 'flow') { loadFlows(); }
  if (name === 'deploy') { pollDeploys(); }
  if (name === 'alerts') { pollAlertsFull(); }
  if (name === 'home') { paintStats(); }
  if (name === 'audits') { loadAudits(); }
  // 用户/角色/权限管理：通过 window 兼容层调用（避免 flow.js → main.js 循环依赖）
  if (name === 'users' && typeof window.pollUsers === 'function') { window.pollUsers(); }
  if (name === 'roles' && typeof window.pollRoles === 'function') { window.pollRoles(); }
  if (name === 'permissions' && typeof window.pollPermissions === 'function') { window.pollPermissions(); }
  // 系统设置 / 文档：通过 window 兼容层调用
  if (name === 'settings' && typeof window.pollSettings === 'function') { window.pollSettings(); }
  if (name === 'docs' && typeof window.pollDocs === 'function') { window.pollDocs(); }
}

// ---------- 审计日志 ----------
// 拉取并渲染审计日志列表。支持按动作、时间窗、条数过滤。
// 后端：GET /api/v1/audits?action=&from=&to=&limit= → AuditEvent[]
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

export function toggleGuide() {
  // 切换当前活跃 tab 内的 .guide-pop 元素；
  // 优先按 id（guide-<tab>）查找，回退到当前活跃 pane 内首个 .guide-pop。
  const activePane = document.querySelector('.pane.active');
  if (!activePane) return;
  let guide = null;
  if (activePane.id && activePane.id.indexOf('tab-') === 0) {
    guide = document.getElementById('guide-' + activePane.id.slice(4));
  }
  if (!guide) guide = activePane.querySelector('.guide-pop');
  if (guide) guide.classList.toggle('open');
}

// ---------- 设备详情抽屉 / 纳管 ----------
export function openDevice(id) {
  api.getDevice(id).then(function (d) {
    const dev = d.device || {};
    let h = '<h3>设备 ' + esc(dev.deviceID) + '</h3>';
    h += '<p>IP: ' + esc(dev.ip) + ' ｜ 采集端: ' + esc(dev.agentID) + ' ｜ 租户: ' + esc(dev.tenantID) + '</p>';
    h += '<p>状态: ' + esc(dev.state) + ' ｜ 任务态: ' + esc(dev.taskState) + '</p>';
    if (dev.lastResult) {
      const c = dev.lastResult === 'failed' ? 'warn' : 'ok';
      h += '<p class="msg ' + c + '">LastResult: ' + esc(dev.lastResult) + ' @ ' + fmtTime(dev.lastResultAt) + '</p>';
    }
    if (dev.state === 'discovered') {
      h += '<button onclick="provision(\'' + esc(dev.deviceID) + '\')">推送 Agent 纳管（B1）</button> ';
    }
    h += '<h4>任务</h4><div class="table-wrap"><table><colgroup><col style="width:50%"><col style="width:25%"><col style="width:25%"></colgroup><thead><tr><th>ID</th><th>类型</th><th>状态</th></tr></thead><tbody>';
    (d.tasks || []).forEach(function (t) { h += '<tr><td><code title="' + esc(t.taskID) + '">' + esc(t.taskID) + '</code></td><td>' + esc(t.type) + '</td><td>' + esc(t.status) + '</td></tr>'; });
    h += '</tbody></table></div>';
    h += '<h4>最近结果</h4><div class="table-wrap"><table><colgroup><col style="width:30%"><col style="width:15%"><col style="width:55%"></colgroup><thead><tr><th>任务</th><th>退出码</th><th>输出</th></tr></thead><tbody>';
    (d.results || []).slice(0, 5).forEach(function (r) { h += '<tr><td><code title="' + esc(r.taskID) + '">' + esc(r.taskID) + '</code></td><td>' + esc(r.exitCode) + '</td><td><code title="' + esc(r.stdout) + '">' + esc(r.stdout) + '</code></td></tr>'; });
    h += '</tbody></table></div>';
    document.getElementById('drawerBody').innerHTML = h;
    document.getElementById('drawer').classList.add('open');
  }).catch(function (e) { console.error(e); });
}
setRenderDeps({ openDevice: openDevice });

export function provision(id) {
  api.provisionDevice(id).then(function (x) {
    document.getElementById('drawerBody').insertAdjacentHTML('beforeend', '<p class="msg ok">[' + x.s + '] ' + esc(JSON.stringify(x.j)) + '</p>');
    pollDevices();
  }).catch(function (e) { console.error(e); });
}

export function closeDrawer() { document.getElementById('drawer').classList.remove('open'); }

// ---------- CMDB ----------
export function loadCMDBTypes() {
  api.getCMDBTypes().then(function (ts) {
    const ft = document.getElementById('ciTypeFilter'), nt = document.getElementById('ciTypeNew'), tt = document.getElementById('tmplTypeFilter');
    [ft, nt, tt].forEach(function (sel) { if (!sel) return; sel.innerHTML = '<option value="">（先选一个类型）</option>'; });
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
  if (!t) { document.getElementById('ciList').innerHTML = '<p class="muted">请先选择一个类型</p>'; return; }
  api.getCIs(t).then(function (list) {
    if (!list || list.length === 0) { document.getElementById('ciList').innerHTML = '<p class="muted">该类型暂无配置项</p>'; return; }
    let html = '<div class="table-wrap"><table><colgroup><col style="width:24%"><col style="width:24%"><col style="width:16%"><col style="width:18%"><col style="width:18%"></colgroup><thead><tr><th>ID</th><th>名称</th><th>状态</th><th>来源</th><th>版本</th></tr></thead><tbody>';
    list.forEach(function (c) {
      html += '<tr class="ci" onclick="openCI(\'' + esc(c.id) + '\')"><td><code title="' + esc(c.id) + '">' + esc(c.id) + '</code></td><td>' + esc(c.name) + '</td><td>' + esc(c.status) + '</td><td>' + esc(c.source) + '</td><td>' + esc(c.version) + '</td></tr>';
    });
    html += '</tbody></table></div>';
    document.getElementById('ciList').innerHTML = html;
  }).catch(function (e) { console.error(e); });
}

export function pollTemplates() {
  const sel = document.getElementById('tmplTypeFilter');
  const t = sel ? sel.value : '';
  if (!t) { document.getElementById('tmplList').innerHTML = '<p class="muted">请先选择一个类型</p>'; return; }
  api.getAttrTemplates(t).then(function (list) {
    if (!list || list.length === 0) { document.getElementById('tmplList').innerHTML = '<p class="muted">该类型暂无属性模板</p>'; return; }
    let html = '<div class="table-wrap"><table><colgroup><col style="width:25%"><col style="width:35%"><col style="width:25%"><col style="width:15%"></colgroup><thead><tr><th>Key</th><th>标签</th><th>类型</th><th>必填</th></tr></thead><tbody>';
    list.forEach(function (x) {
      html += '<tr><td><code title="' + esc(x.attrKey) + '">' + esc(x.attrKey) + '</code></td><td>' + esc(x.label) + '</td><td>' + esc(x.attrType) + '</td><td>' + (x.required ? '是' : '否') + '</td></tr>';
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
    h += '<p>状态: ' + esc(c.status) + ' ｜ 来源: ' + esc(c.source) + ' ｜ 版本: ' + esc(c.version) + '</p>';
    if (c.attrs && Object.keys(c.attrs).length) {
      h += '<p>属性: ' + Object.keys(c.attrs).map(function (k) { return '<code>' + esc(k) + '</code>=' + esc(c.attrs[k]); }).join('，') + '</p>';
    }
    h += '<h4>关系拓扑（' + (g.relations ? g.relations.length : 0) + '）</h4>';
    if (!g.relations || g.relations.length === 0) { h += '<p class="muted">无关系</p>'; }
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
  if (raw) { try { attrs = JSON.parse(raw); } catch (err) { cmdbMsg('属性 JSON 解析失败: ' + err, false); return; } }
  const body = { ciType: type, name: document.getElementById('ciName').value, attrs: attrs };
  api.createCI(body)
    .then(function (x) { cmdbMsg('[' + x.s + '] ' + JSON.stringify(x.j), x.s < 400); pollCIs(); })
    .catch(function (err) { cmdbMsg('error: ' + err, false); });
}

// ---------- 作业编排（M5 DAG 画布） ----------
const SVGNS = 'http://www.w3.org/2000/svg';
let flow = { id: 0, name: '', agentID: '', cron: '', dag: [], status: '' };
let nodePos = {}, selectedNode = null, linking = false, linkSrc = null, nodeStatus = {};
let selectedEdge = null;            // {src,dst} 当前选中的依赖边
let view = { scale: 1, tx: 0, ty: 0 };  // 画布视口变换（缩放/平移）
let history = [], future = [];       // 撤销 / 重做 栈
let panning = null, linkDrag = null;  // 平移 / 拖拽连线 临时态
let drag = null;
const GRID = 20;                     // 网格吸附粒度
const TYPE_COLOR = { 'shell': '#6366f1', 'file': '#0d9488', 'service': '#d97706' };
const TYPE_SOFT = { 'shell': '#eceaff', 'file': '#d8f3ef', 'service': '#fef3e2' };

function snap(v) { return Math.round(v / GRID) * GRID; }
function snapshot() { history.push(JSON.stringify({ dag: flow.dag, pos: nodePos, sel: selectedNode, edge: selectedEdge })); if (history.length > 60) history.shift(); future = []; }
export function undo() { if (!history.length) return; future.push(JSON.stringify({ dag: flow.dag, pos: nodePos, sel: selectedNode, edge: selectedEdge })); const s = JSON.parse(history.pop()); flow.dag = s.dag; nodePos = s.pos; selectedNode = s.sel; selectedEdge = s.edge; renderFlow(); flowMsg('已撤销', true); }
export function redo() { if (!future.length) return; history.push(JSON.stringify({ dag: flow.dag, pos: nodePos, sel: selectedNode, edge: selectedEdge })); const s = JSON.parse(future.pop()); flow.dag = s.dag; nodePos = s.pos; selectedNode = s.sel; selectedEdge = s.edge; renderFlow(); flowMsg('已重做', true); }
export function flowMsg(s, ok) { const el = document.getElementById('wfMsg'); if (el) { el.className = 'msg ' + (ok ? 'ok' : 'err'); el.textContent = (ok ? '[ok] ' : '[err] ') + s; } }

export function loadFlows() {
  api.getAgents().then(function (a) {
    const sel = document.getElementById('wfAgent'); if (!sel) return; sel.innerHTML = '';
    (a || []).forEach(function (x) { const o = document.createElement('option'); o.value = x.agentID; o.textContent = x.agentID + ' (' + x.hostname + ')'; sel.appendChild(o); });
  }).catch(function (e) { console.error(e); });
  api.getWorkflows().then(function (list) {
    const sel = document.getElementById('wfSelect'); if (!sel) return; sel.innerHTML = '<option value="">（新建空白作业流）</option>';
    (list || []).forEach(function (w) { const o = document.createElement('option'); o.value = w.id; o.textContent = '#' + w.id + ' ' + w.name + ' [' + w.status + ']'; sel.appendChild(o); });
  }).catch(function (e) { console.error(e); });
}

export function openWorkflow() {
  const id = document.getElementById('wfSelect').value;
  if (!id) { newWorkflow(); return; }
  api.getWorkflow(id).then(function (w) {
    if (w.error) { flowMsg(w.error, false); return; }
    flow = { id: w.id, name: w.name, agentID: w.agentID, cron: w.cron || '', dag: [], status: w.status };
    try { flow.dag = w.dag ? JSON.parse(w.dag) : []; } catch (e) { flow.dag = []; }
    document.getElementById('wfName').value = w.name || '';
    document.getElementById('wfCron').value = w.cron || '';
    const asel = document.getElementById('wfAgent');
    if (w.agentID) { for (let i = 0; i < asel.options.length; i++) { if (asel.options[i].value === w.agentID) { asel.selectedIndex = i; break; } } }
    nodePos = {}; autoLayout(); renderFlow();
  }).catch(function (e) { console.error(e); });
}

export function newWorkflow() {
  flow = { id: 0, name: '', agentID: document.getElementById('wfAgent').value, cron: '', dag: [], status: 'draft' };
  document.getElementById('wfName').value = '';
  document.getElementById('wfCron').value = '';
  nodePos = {}; renderFlow(); flowMsg('已新建空白作业流，添加步骤后保存', true);
}

export function loadDemo() {
  flow = {
    id: 0, name: '示例-nginx发布', agentID: document.getElementById('wfAgent').value, cron: '', dag: [
      { id: 'n1', name: '拉取镜像', type: 'shell', command: 'docker pull nginx:latest', path: '', dependsOn: [] },
      { id: 'n2', name: '停旧容器', type: 'shell', command: 'docker stop nginx', path: '', dependsOn: ['n1'] },
      { id: 'n3', name: '起新容器', type: 'service', command: 'nginx', path: '', dependsOn: ['n2'] },
    ], status: 'draft',
  };
  document.getElementById('wfName').value = flow.name;
  document.getElementById('wfCron').value = '';
  nodePos = {}; autoLayout(); renderFlow(); flowMsg('已载入示例作业流（尚未保存，可改动后点「保存」）', true);
}

export function addNode() {
  let id = 'n' + (flow.dag.length + 1);
  while (flow.dag.some(function (n) { return n.id === id; })) { id = 'n' + Math.floor(Math.random() * 1000); }
  snapshot();
  flow.dag.push({ id: id, name: '步骤' + id, type: 'shell', command: '', path: '', dependsOn: [] });
  nodePos[id] = { x: snap(60 + Math.random() * 40), y: snap(60 + flow.dag.length * 70) };
  selectNode(id); renderFlow();
}

export function deleteNode(id) {
  snapshot();
  flow.dag = flow.dag.filter(function (n) { return n.id !== id; });
  flow.dag.forEach(function (n) { n.dependsOn = (n.dependsOn || []).filter(function (d) { return d !== id; }); });
  delete nodePos[id]; if (selectedNode === id) selectedNode = null; if (selectedEdge && selectedEdge.src === id) selectedEdge = null; renderFlow();
}

export function selectNode(id) { selectedNode = id; renderFlow(); }

export function toggleLink() {
  linking = !linking; linkSrc = null;
  const b = document.getElementById('linkBtn'); if (b) b.innerHTML = linking ? icon('link', 16) + ' 连线中…(点两节点)' : icon('link', 16) + ' 连线';
  const p = document.getElementById('tab-flow'); if (p) p.classList.toggle('linkmode', linking);
}

function createsCycle(src, dst) {
  const adj = {}; flow.dag.forEach(function (n) { adj[n.id] = n.dependsOn || []; });
  const seen = {}, stack = [src];
  while (stack.length) { const c = stack.pop(); if (c === dst) return true; if (seen[c]) continue; seen[c] = 1; (adj[c] || []).forEach(function (x) { stack.push(x); }); }
  return false;
}

export function autoLayout() {
  snapshot();
  const indeg = {}, adj = {};
  flow.dag.forEach(function (n) { indeg[n.id] = 0; adj[n.id] = []; });
  flow.dag.forEach(function (n) { (n.dependsOn || []).forEach(function (d) { if (indeg[d] !== undefined) { indeg[d]++; adj[n.id].push(d); } }); });
  const level = {}, q = [];
  flow.dag.forEach(function (n) { if (indeg[n.id] === 0) { level[n.id] = 0; q.push(n.id); } });
  while (q.length) { const cur = q.shift(); (adj[cur] || []).forEach(function (p) { if (level[p] === undefined || level[cur] + 1 > level[p]) level[p] = level[cur] + 1; indeg[p]--; if (indeg[p] === 0) q.push(p); }); }
  flow.dag.forEach(function (n) { if (level[n.id] === undefined) level[n.id] = 0; });
  const per = {};
  flow.dag.forEach(function (n) { const L = level[n.id]; per[L] = per[L] || 0; const idx = per[L]++; nodePos[n.id] = { x: snap(60 + L * 230), y: snap(50 + idx * 100) }; });
  renderFlow();
}

export function renderFlow() {
  const svg = document.getElementById('canvas'); if (!svg) return;
  while (svg.firstChild) svg.removeChild(svg.firstChild);
  const W = 170, H = 66;
  // defs：网格 + 管头
  const defs = document.createElementNS(SVGNS, 'defs');
  const pat = document.createElementNS(SVGNS, 'pattern');
  pat.setAttribute('id', 'grid'); pat.setAttribute('width', '26'); pat.setAttribute('height', '26'); pat.setAttribute('patternUnits', 'userSpaceOnUse');
  const pc = document.createElementNS(SVGNS, 'circle'); pc.setAttribute('cx', '2'); pc.setAttribute('cy', '2'); pc.setAttribute('r', '1'); pc.setAttribute('fill', '#dbe2f1');
  pat.appendChild(pc); defs.appendChild(pat);
  const mk = document.createElementNS(SVGNS, 'marker');
  mk.setAttribute('id', 'arrow'); mk.setAttribute('markerWidth', '10'); mk.setAttribute('markerHeight', '10');
  mk.setAttribute('refX', '8'); mk.setAttribute('refY', '3'); mk.setAttribute('orient', 'auto'); mk.setAttribute('markerUnits', 'strokeWidth');
  const mp = document.createElementNS(SVGNS, 'path'); mp.setAttribute('d', 'M0,0 L8,3 L0,6 Z'); mp.setAttribute('fill', '#94a3b8');
  mk.appendChild(mp); defs.appendChild(mk); svg.appendChild(defs);
  // 视口组（承载缩放/平移变换）
  const vp = document.createElementNS(SVGNS, 'g'); vp.setAttribute('id', 'viewport');
  vp.setAttribute('transform', 'translate(' + view.tx + ',' + view.ty + ') scale(' + view.scale + ')');
  svg.appendChild(vp);
  // 背景网格（随视口移动/缩放）
  const bg = document.createElementNS(SVGNS, 'rect'); bg.setAttribute('x', -3000); bg.setAttribute('y', -3000); bg.setAttribute('width', 8000); bg.setAttribute('height', 8000); bg.setAttribute('fill', 'url(#grid)'); bg.setAttribute('class', 'canvasbg');
  vp.appendChild(bg);
  // 边（先画，节点覆盖其上便于拖拽）
  const EG = document.createElementNS(SVGNS, 'g');
  flow.dag.forEach(function (n) {
    (n.dependsOn || []).forEach(function (d) {
      const s = nodePos[d], t = nodePos[n.id]; if (!s || !t) return;
      const x1 = s.x + W / 2, y1 = s.y + H, x2 = t.x + W / 2, y2 = t.y;
      const sel = (selectedEdge && selectedEdge.src === d && selectedEdge.dst === n.id);
      const ln = document.createElementNS(SVGNS, 'line');
      ln.setAttribute('class', 'edge' + (sel ? ' sel' : ''));
      ln.setAttribute('x1', x1); ln.setAttribute('y1', y1); ln.setAttribute('x2', x2); ln.setAttribute('y2', y2);
      ln.setAttribute('marker-end', 'url(#arrow)');
      EG.appendChild(ln);
      const hit = document.createElementNS(SVGNS, 'line');
      hit.setAttribute('class', 'edge-hit'); hit.setAttribute('x1', x1); hit.setAttribute('y1', y1); hit.setAttribute('x2', x2); hit.setAttribute('y2', y2);
      hit.addEventListener('click', function (ev) { ev.stopPropagation(); if (linking) return; selectEdge(d, n.id); });
      EG.appendChild(hit);
    });
  });
  vp.appendChild(EG);
  // 拖拽连线预览线
  if (linkDrag && linkDrag.from) { const p0 = nodePos[linkDrag.from]; if (p0) { const pv = document.createElementNS(SVGNS, 'line'); pv.setAttribute('class', 'edgeprev'); pv.setAttribute('x1', p0.x + W); pv.setAttribute('y1', p0.y + H / 2); pv.setAttribute('x2', linkDrag.x); pv.setAttribute('y2', linkDrag.y); vp.appendChild(pv); } }
  // 节点卡片
  const NG = document.createElementNS(SVGNS, 'g');
  flow.dag.forEach(function (n) {
    const p = nodePos[n.id] || { x: 60, y: 60 };
    const cls = 'node' + (selectedNode === n.id ? ' sel' : '') + (nodeStatus[n.id] === 'running' ? ' run' : '') + (nodeStatus[n.id] === 'failed' ? ' fail' : '');
    const g = document.createElementNS(SVGNS, 'g');
    g.setAttribute('class', cls);
    g.setAttribute('transform', 'translate(' + p.x + ',' + p.y + ')');
    const rc = document.createElementNS(SVGNS, 'rect');
    rc.setAttribute('class', 'card'); rc.setAttribute('width', W); rc.setAttribute('height', H); rc.setAttribute('rx', '10'); rc.setAttribute('ry', '10');
    if (nodeStatus[n.id]) { const c = { 'done': '#059669', 'running': '#6366f1', 'pending': '#d97706', 'blocked': '#64748b', 'failed': '#e11d48' }[nodeStatus[n.id]]; if (c) rc.setAttribute('stroke', c); }
    g.appendChild(rc);
    const bar = document.createElementNS(SVGNS, 'rect'); bar.setAttribute('width', '4'); bar.setAttribute('height', H); bar.setAttribute('rx', '2'); bar.setAttribute('fill', TYPE_COLOR[n.type] || '#6366f1');
    g.appendChild(bar);
    const t1 = document.createElementNS(SVGNS, 'text'); t1.setAttribute('x', '14'); t1.setAttribute('y', '24'); t1.setAttribute('class', 'ntitle'); t1.textContent = (n.name || n.id); g.appendChild(t1);
    const t2 = document.createElementNS(SVGNS, 'text'); t2.setAttribute('x', '14'); t2.setAttribute('y', '44'); t2.setAttribute('class', 'ncmd'); t2.textContent = '▸ ' + (n.command || '(无命令)').slice(0, 20); g.appendChild(t2);
    const pill = document.createElementNS(SVGNS, 'rect'); pill.setAttribute('x', (W - 48)); pill.setAttribute('y', '9'); pill.setAttribute('width', '40'); pill.setAttribute('height', '16'); pill.setAttribute('rx', '8'); pill.setAttribute('fill', TYPE_SOFT[n.type] || '#eceaff'); g.appendChild(pill);
    const pt = document.createElementNS(SVGNS, 'text'); pt.setAttribute('x', (W - 28)); pt.setAttribute('y', '21'); pt.setAttribute('class', 'ntype'); pt.setAttribute('fill', TYPE_COLOR[n.type] || '#6366f1'); pt.textContent = n.type; g.appendChild(pt);
    const port = document.createElementNS(SVGNS, 'circle'); port.setAttribute('class', 'port'); port.setAttribute('cx', W); port.setAttribute('cy', H / 2); port.setAttribute('r', '6');
    port.addEventListener('mousedown', function (ev) { ev.stopPropagation(); startLinkDrag(ev, n.id); });
    port.addEventListener('click', function (ev) { ev.stopPropagation(); });
    g.appendChild(port);
    g.addEventListener('mousedown', function (ev) { ev.preventDefault(); nodeMouseDown(ev, n.id, p); });
    g.addEventListener('click', function (ev) { ev.stopPropagation(); if (linking) { linkClick(n.id); } else { selectNode(n.id); } });
    NG.appendChild(g);
  });
  vp.appendChild(NG);
  updateZoomLabel();
  renderNodeList(); renderNodeEditor();
}

function flowPoint(ev) {
  const vp = document.getElementById('viewport'); if (!vp) return { x: 0, y: 0 };
  const ctm = vp.getScreenCTM(); if (!ctm) return { x: 0, y: 0 };
  const svg = document.getElementById('canvas'); const sp = svg.createSVGPoint(); sp.x = ev.clientX; sp.y = ev.clientY;
  const q = sp.matrixTransform(ctm.inverse());
  return { x: q.x, y: q.y };
}

function nodeMouseDown(ev, id, pos) {
  const c = flowPoint(ev);
  drag = { id: id, ox: c.x - pos.x, oy: c.y - pos.y, moved: false };
  document.addEventListener('mousemove', nodeMouseMove);
  document.addEventListener('mouseup', nodeMouseUp);
}
function nodeMouseMove(ev) {
  if (!drag) return;
  const c = flowPoint(ev);
  if (!drag.moved) { drag.moved = true; snapshot(); }
  nodePos[drag.id] = { x: snap(c.x - drag.ox), y: snap(c.y - drag.oy) };
  renderFlow();
}
function nodeMouseUp() { drag = null; document.removeEventListener('mousemove', nodeMouseMove); document.removeEventListener('mouseup', nodeMouseUp); }

export function svgMouseDown(ev) {
  if (linking) return;
  if (ev.target && ev.target.closest && ev.target.closest('.node')) return;
  if (ev.target && ev.target.classList && ev.target.classList.contains('edge-hit')) return;
  panning = { sx: ev.clientX, sy: ev.clientY, tx: view.tx, ty: view.ty };
  const svg = document.getElementById('canvas'); if (svg) svg.classList.add('panning');
  document.addEventListener('mousemove', panMove);
  document.addEventListener('mouseup', panUp);
}
function panMove(ev) { if (!panning) return; view.tx = panning.tx + (ev.clientX - panning.sx); view.ty = panning.ty + (ev.clientY - panning.sy); applyView(); }
function panUp() { panning = null; const svg = document.getElementById('canvas'); if (svg) svg.classList.remove('panning'); document.removeEventListener('mousemove', panMove); document.removeEventListener('mouseup', panUp); }

export function svgWheel(ev) {
  ev.preventDefault();
  const svg = document.getElementById('canvas'); const r = svg.getBoundingClientRect();
  const mx = ev.clientX - r.left, my = ev.clientY - r.top;
  const cx = (mx - view.tx) / view.scale, cy = (my - view.ty) / view.scale;
  const f = ev.deltaY < 0 ? 1.12 : 0.89;
  const ns = Math.max(0.3, Math.min(2.5, view.scale * f));
  view.scale = ns; view.tx = mx - cx * ns; view.ty = my - cy * ns; applyView();
}
function applyView() { const vp = document.getElementById('viewport'); if (vp) vp.setAttribute('transform', 'translate(' + view.tx + ',' + view.ty + ') scale(' + view.scale + ')'); updateZoomLabel(); }
function updateZoomLabel() { const z = document.getElementById('zoomVal'); if (z) z.textContent = Math.round(view.scale * 100) + '%'; }
export function zoomBy(f) { const svg = document.getElementById('canvas'); const r = svg.getBoundingClientRect(); const mx = r.width / 2, my = r.height / 2; const cx = (mx - view.tx) / view.scale, cy = (my - view.ty) / view.scale; const ns = Math.max(0.3, Math.min(2.5, view.scale * f)); view.scale = ns; view.tx = mx - cx * ns; view.ty = my - cy * ns; applyView(); }
export function fitView() {
  const ids = Object.keys(nodePos); if (!ids.length) { view = { scale: 1, tx: 0, ty: 0 }; applyView(); return; }
  let minx = 1e9, miny = 1e9, maxx = -1e9, maxy = -1e9;
  ids.forEach(function (id) { const p = nodePos[id]; if (!p) return; minx = Math.min(minx, p.x); miny = Math.min(miny, p.y); maxx = Math.max(maxx, p.x + 170); maxy = Math.max(maxy, p.y + 66); });
  const svg = document.getElementById('canvas'); const r = svg.getBoundingClientRect();
  const pad = 50; const bw = (maxx - minx) + pad * 2, bh = (maxy - miny) + pad * 2;
  let s = Math.min(r.width / bw, r.height / bh, 2); if (s < 0.2) s = 0.2;
  view.scale = s; view.tx = (r.width - bw * s) / 2 - (minx - pad) * s; view.ty = (r.height - bh * s) / 2 - (miny - pad) * s; applyView();
}
export function resetView() { view = { scale: 1, tx: 0, ty: 0 }; applyView(); }

function addDep(srcId, depId) {
  if (srcId === depId) { flowMsg('不能依赖自身', false); return false; }
  const src = flow.dag.find(function (n) { return n.id === srcId; });
  if (!src) return false;
  if ((src.dependsOn || []).indexOf(depId) >= 0) { flowMsg('依赖已存在', false); return false; }
  if (createsCycle(srcId, depId)) { flowMsg('该依赖会形成环，已忽略', false); return false; }
  src.dependsOn = src.dependsOn || []; src.dependsOn.push(depId);
  flowMsg('已添加依赖: ' + srcId + ' → ' + depId, true); return true;
}
function linkClick(id) {
  if (!linkSrc) { linkSrc = id; flowMsg('连线源: ' + id + '，点击目标步骤', true); return; }
  if (linkSrc === id) { flowMsg('不能依赖自身', false); linkSrc = null; return; }
  snapshot();
  addDep(linkSrc, id);
  linkSrc = null; renderFlow();
}
function selectEdge(src, dst) { selectedEdge = { src: src, dst: dst }; renderFlow(); flowMsg('已选中依赖 ' + src + ' → ' + dst + '（按 Delete 或 Esc 取消）', true); }
function deleteEdge(src, dst) {
  const n = flow.dag.find(function (x) { return x.id === dst; }); if (!n) return;
  snapshot();
  n.dependsOn = (n.dependsOn || []).filter(function (d) { return d !== src; });
  selectedEdge = null; renderFlow(); flowMsg('已删除依赖 ' + src + ' → ' + dst, true);
}
function startLinkDrag(ev, id) {
  linking = true; linkSrc = id;
  const c = flowPoint(ev); linkDrag = { from: id, x: c.x, y: c.y };
  const b = document.getElementById('linkBtn'); if (b) b.innerHTML = icon('link', 16) + ' 拖拽连线中…';
  const p = document.getElementById('tab-flow'); if (p) p.classList.add('linkmode');
  document.addEventListener('mousemove', linkDragMove);
  document.addEventListener('mouseup', linkDragUp);
}
function linkDragMove(ev) { if (!linkDrag) return; const c = flowPoint(ev); linkDrag.x = c.x; linkDrag.y = c.y; renderFlow(); }
function linkDragUp(ev) {
  document.removeEventListener('mousemove', linkDragMove);
  document.removeEventListener('mouseup', linkDragUp);
  const c = flowPoint(ev);
  let target = null;
  flow.dag.forEach(function (n) { const p = nodePos[n.id]; if (!p) return; if (c.x >= p.x && c.x <= p.x + 170 && c.y >= p.y && c.y <= p.y + 66) target = n.id; });
  const fromId = linkDrag ? linkDrag.from : null;
  linking = false; linkSrc = null; linkDrag = null;
  const b = document.getElementById('linkBtn'); if (b) b.innerHTML = icon('link', 16) + ' 连线';
  const p = document.getElementById('tab-flow'); if (p) p.classList.remove('linkmode');
  if (target && target !== fromId) { snapshot(); addDep(target, fromId); }
  renderFlow();
}

function renderNodeList() {
  const el = document.getElementById('nodeList'); if (!el) return;
  if (!flow.dag.length) { el.innerHTML = '<p class="muted">暂无步骤。点「＋ 添加步骤」开始，或点右上「载入示例」。</p>'; return; }
  el.innerHTML = flow.dag.map(function (n) {
    return '<div class="ci' + (selectedNode === n.id ? ' sel' : '') + '" onclick="selectNode(\'' + esc(n.id) + '\')">' + esc(n.name || n.id) + ' <small class="muted">' + esc(n.type) + '</small>' + (selectedNode === n.id ? ' ✦' : '') + '</div>';
  }).join('');
}

function renderNodeEditor() {
  const el = document.getElementById('nodeEditor'); if (!el) return;
  if (!selectedNode) { el.innerHTML = ''; el.style.display = 'none'; return; }
  const n = flow.dag.find(function (x) { return x.id === selectedNode; }); if (!n) { el.innerHTML = ''; el.style.display = 'none'; return; }
  const others = flow.dag.filter(function (x) { return x.id !== n.id; }).map(function (x) {
    return '<option value="' + esc(x.id) + '"' + (((n.dependsOn || []).indexOf(x.id) >= 0) ? ' selected' : '') + '>' + esc(x.name || x.id) + '</option>';
  }).join('');
  el.innerHTML = '<h4>编辑步骤: ' + esc(n.id) + '</h4>'
    + '<label>名称:<input id="nName" value="' + esc(n.name) + '" size="16"></label><br>'
    + '<label>类型:<select id="nType"><option value="shell"' + (n.type === 'shell' ? ' selected' : '') + '>shell（执行命令）</option><option value="file"' + (n.type === 'file' ? ' selected' : '') + '>file（下发文件）</option><option value="service"' + (n.type === 'service' ? ' selected' : '') + '>service（启停服务）</option></select></label><br>'
    + '<label>命令/动作:<input id="nCmd" value="' + esc(n.command) + '" size="28" title="该步骤要执行的内容"></label><br>'
    + '<label>路径(path):<input id="nPath" value="' + esc(n.path) + '" size="18"></label><br>'
    + '<label>依赖(多选):<select id="nDeps" multiple size="3" title="本步骤开始前应完成的其它步骤">' + others + '</select></label><br>'
    + '<div class="btnbar"><button onclick="applyNode()">应用</button> <button onclick="deleteNode(\'' + esc(n.id) + '\')">删除步骤</button></div>';
  el.style.display = 'block';
}

export function applyNode() {
  const n = flow.dag.find(function (x) { return x.id === selectedNode; }); if (!n) return;
  snapshot();
  n.name = document.getElementById('nName').value;
  n.type = document.getElementById('nType').value;
  n.command = document.getElementById('nCmd').value;
  n.path = document.getElementById('nPath').value;
  const sel = document.getElementById('nDeps'); const deps = [];
  for (let i = 0; i < sel.options.length; i++) { if (sel.options[i].selected) deps.push(sel.options[i].value); }
  n.dependsOn = deps.filter(function (d) { return d !== n.id && !createsCycle(n.id, d); });
  renderFlow();
}

export function saveWorkflow() {
  const name = document.getElementById('wfName').value.trim();
  const agent = document.getElementById('wfAgent').value;
  const cron = document.getElementById('wfCron').value.trim();
  if (!name || !agent) { flowMsg('请填写名称和采集端', false); return; }
  const dagStr = JSON.stringify(flow.dag);
  if (flow.id) {
    api.updateWorkflow(flow.id, { name: name, dag: dagStr, cron: cron })
      .then(function (x) { if (x.s >= 400) { flowMsg('[' + x.s + '] ' + (x.j.error || ''), false); } else { flow.id = x.j.id; flow.status = x.j.status; flow.cron = x.j.cron || ''; flowMsg('已保存 #' + x.j.id, true); loadFlows(); } });
  } else {
    api.createWorkflow({ name: name, agentID: agent, dag: dagStr, cron: cron })
      .then(function (x) { if (x.s >= 400) { flowMsg('[' + x.s + '] ' + (x.j.error || ''), false); } else { flow.id = x.j.id; flow.status = x.j.status; flow.cron = x.j.cron || ''; flowMsg('已创建 #' + x.j.id, true); loadFlows(); } });
  }
}

export function runWorkflow() {
  if (!flow.id) { flowMsg('请先保存作业流', false); return; }
  api.runWorkflow(flow.id)
    .then(function (x) { if (x.s >= 400) { flowMsg('[' + x.s + '] ' + (x.j.error || ''), false); } else { flowMsg('已触发运行 #' + flow.id, true); pollStatus(); } });
}

export function alignSel(axis) {
  if (!selectedNode) { flowMsg('先选中一个步骤作为对齐基准', false); return; }
  const ref = nodePos[selectedNode]; if (!ref) return;
  const others = flow.dag.filter(function (n) { return n.id !== selectedNode; }).map(function (n) { return nodePos[n.id]; }).filter(Boolean);
  snapshot();
  if (!others.length) {
    if (axis === 'left') ref.x = 0; else if (axis === 'right') ref.x = snap(170 - 170); else if (axis === 'top') ref.y = 0; else if (axis === 'bottom') ref.y = 0;
    renderFlow(); flowMsg('已对齐到画布', true); return;
  }
  if (axis === 'left') { const x = Math.min.apply(null, others.map(function (p) { return p.x; })); ref.x = snap(x); }
  else if (axis === 'right') { const x = Math.max.apply(null, others.map(function (p) { return p.x + 170; })); ref.x = snap(x - 170); }
  else if (axis === 'top') { const y = Math.min.apply(null, others.map(function (p) { return p.y; })); ref.y = snap(y); }
  else if (axis === 'bottom') { const y = Math.max.apply(null, others.map(function (p) { return p.y + 66; })); ref.y = snap(y - 66); }
  renderFlow(); flowMsg('已' + (axis === 'left' ? '左' : axis === 'right' ? '右' : axis === 'top' ? '上' : '下') + '对齐', true);
}

function paintRunState() {
  const el = document.getElementById('flowLegend'); if (!el) return;
  if (!flow.id || !Object.keys(nodeStatus).length) { el.className = 'flowLegend'; el.innerHTML = ''; return; }
  const c = {}; Object.keys(nodeStatus).forEach(function (k) { c[nodeStatus[k]] = (c[nodeStatus[k]] || 0) + 1; });
  const col = { 'done': '#059669', 'running': '#6366f1', 'pending': '#d97706', 'blocked': '#64748b', 'failed': '#e11d48' };
  const label = { 'done': '成功', 'running': '运行中', 'pending': '等待', 'blocked': '阻塞', 'failed': '失败' };
  let html = '<span class="muted">运行态:</span>';
  Object.keys(c).forEach(function (k) { html += '<span class="pill"><span class="dot" style="background:' + (col[k] || '#64748b') + '"></span>' + (label[k] || k) + ' ' + c[k] + '</span>'; });
  html += '<button onclick="switchTab(\'ops\')" title="跳到运维中枢查看任务执行详情">' + icon('search', 14) + ' 在运维中枢查看</button>';
  el.className = 'flowLegend show'; el.innerHTML = html;
}

function pollStatus() {
  if (!flow.id) return;
  api.getWorkflowStatus(flow.id).then(function (x) {
    if (x.error) return;
    const nt = x.nodeTasks || {}; nodeStatus = {};
    Object.keys(nt).forEach(function (k) { const nid = k.replace(/^wf-\d+-/, ''); nodeStatus[nid] = nt[k]; });
    renderFlow(); paintRunState();
    const cnt = {}; Object.keys(nodeStatus).forEach(function (k) { cnt[nodeStatus[k]] = (cnt[nodeStatus[k]] || 0) + 1; });
    flowMsg('运行态: ' + JSON.stringify(cnt), true);
    setTimeout(function () { const p = document.getElementById('tab-flow'); if (p && p.classList.contains('active')) pollStatus(); }, 3000);
  }).catch(function (e) { console.error(e); });
}

export function scheduleWorkflowPrompt() {
  if (!flow.id) { flowMsg('请先保存作业流', false); return; }
  const cron = document.getElementById('wfCron').value.trim();
  api.scheduleWorkflow(flow.id, cron)
    .then(function (x) { if (x.s >= 400) { flowMsg('[' + x.s + '] ' + (x.j.error || ''), false); } else { flow.cron = x.j.cron || ''; flow.status = x.j.status; flowMsg('已设置定时: ' + (x.j.cron || '(无)'), true); loadFlows(); } });
}

export function flowKey(e) {
  const pf = document.getElementById('tab-flow'); if (!pf || !pf.classList.contains('active')) return;
  const t = (document.activeElement && document.activeElement.tagName) || '';
  if (t === 'INPUT' || t === 'SELECT' || t === 'TEXTAREA') return;
  if (e.key === 'Delete' || e.key === 'Backspace') {
    if (selectedEdge) { deleteEdge(selectedEdge.src, selectedEdge.dst); }
    else if (selectedNode) { deleteNode(selectedNode); }
    e.preventDefault();
  } else if (e.key === 'Escape') {
    if (linking) toggleLink(); selectedEdge = null; selectedNode = null; renderFlow();
  } else if ((e.ctrlKey || e.metaKey) && (e.key === 'z' || e.key === 'Z')) {
    if (e.shiftKey) redo(); else undo(); e.preventDefault();
  } else if ((e.ctrlKey || e.metaKey) && (e.key === 'y' || e.key === 'Y')) {
    redo(); e.preventDefault();
  } else if ((e.ctrlKey || e.metaKey) && (e.key === 's' || e.key === 'S')) {
    saveWorkflow(); e.preventDefault();
  }
}

// ---------- 部署中心（M3） ----------
export function deployMsg(s, ok) { const el = document.getElementById('deployMsg'); if (el) { el.className = 'msg ' + (ok ? 'ok' : 'err'); el.textContent = (ok ? '[ok] ' : '[err] ') + s; } }

export function loadDeployDemo() {
  document.getElementById('dpName').value = 'deploy-nginx';
  document.getElementById('dpType').value = 'script';
  document.getElementById('dpRepo').value = 'https://git.example.com/ops/nginx-deploy.git';
  document.getElementById('dpContent').value = '';
  document.getElementById('dpPath').value = '';
  document.getElementById('dpTargets').value = 'dev-10.0.0.1, dev-10.0.0.2';
  deployMsg('已载入示例，可改后点「登记部署」', true);
}

export function pollDeploys() {
  const sf = document.getElementById('dpStatusFilter');
  const st = sf ? sf.value : '';
  api.getDeploys(st)
    .then(function (list) {
      const fl = applyFocus(list || [], 'deploy');
      const note = focusDevice ? '<p class="hint">' + icon('context', 14) + ' 已按设备 <code>' + esc(focusDevice.id) + '</code> 过滤（' + fl.length + ' 条）</p>' : '';
      if (!fl || fl.length === 0) { document.getElementById('deployList').innerHTML = note + '<p class="muted">暂无部署任务。在左侧登记一个吧。</p>'; return; }
      let html = note + '<div class="table-wrap"><table><colgroup><col style="width:16%"><col style="width:18%"><col style="width:12%"><col style="width:24%"><col style="width:12%"><col style="width:18%"></colgroup><thead><tr><th>ID</th><th>名称</th><th>类型</th><th>目标设备</th><th>状态</th><th>操作</th></tr></thead><tbody>';
      fl.forEach(function (d) {
        const targets = (d.target_ids || '').replace(/,/g, ', ');
        html += '<tr><td><code title="' + esc(d.id) + '">' + esc(d.id) + '</code></td><td>' + esc(d.name) + '</td><td>' + esc(d.type) + '</td>'
          + '<td><code title="' + esc(targets) + '">' + esc(targets) + '</code></td><td>' + dpStatusPill(d.status) + '</td>'
          + '<td class="row-actions-cell"><button onclick="execDeploy(' + d.id + ')">▶ 执行</button> <button onclick="rollbackDeploy(' + d.id + ')">↩ 回滚</button> <button onclick="openDeploy(' + d.id + ')">详情</button></td></tr>';
      });
      html += '</tbody></table></div>';
      document.getElementById('deployList').innerHTML = html;
    }).catch(function (e) { api.apiFail('deploys', e); });
}

export function execDeploy(id) {
  api.executeDeploy(id)
    .then(function (x) { deployMsg('[' + x.s + '] ' + (x.j.error || '已触发执行 #' + id), x.s < 400); pollDeploys(); })
    .catch(function (e) { deployMsg('error: ' + e, false); });
}

export function rollbackDeploy(id) {
  api.rollbackDeploy(id)
    .then(function (x) { deployMsg('[' + x.s + '] ' + (x.j.error || '已回滚 #' + id), x.s < 400); pollDeploys(); })
    .catch(function (e) { deployMsg('error: ' + e, false); });
}

export function openDeploy(id) {
  api.getDeploy(id).then(function (d) {
    let h = '<h3>部署 #' + esc(d.id) + ' · ' + esc(d.name) + '</h3>';
    h += '<p>类型: ' + esc(d.type) + ' ｜ 状态: ' + dpStatusPill(d.status) + '</p>';
    h += '<p>目标设备: <code>' + esc((d.target_ids || '').replace(/,/g, ', ')) + '</code></p>';
    if (d.repo_url) h += '<p>仓库: <code>' + esc(d.repo_url) + '</code></p>';
    if (d.path) h += '<p>路径: <code>' + esc(d.path) + '</code></p>';
    if (d.content) h += '<p>内容: <code>' + esc(d.content) + '</code></p>';
    h += '<p class="muted">创建人: ' + esc(d.created_by) + ' ｜ 创建: ' + fmtTime(d.created_at) + ' ｜ 更新: ' + fmtTime(d.updated_at) + '</p>';
    if (d.task_ids) h += '<p>派发任务: <code>' + esc((d.task_ids || '').replace(/,/g, ', ')) + '</code></p>';
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
  if (!body.name || !body.target_ids) { deployMsg('请填写名称和至少一个目标设备', false); return; }
  api.createDeploy(body)
    .then(function (x) { deployMsg('[' + x.s + '] ' + (x.j.error || '已登记 #' + (x.j && x.j.id)), x.s < 400); if (x.s < 400) pollDeploys(); })
    .catch(function (err) { deployMsg('error: ' + err, false); });
}

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
      if (!list || list.length === 0) { document.getElementById('logList').innerHTML = '<p class="muted">没有匹配的日志。</p>'; updateLogPage(0); return; }
      let html = '<div class="table-wrap"><table><colgroup><col style="width:15%"><col style="width:8%"><col style="width:8%"><col style="width:18%"><col style="width:18%"><col style="width:33%"></colgroup><thead><tr><th>时间</th><th>级别</th><th>来源</th><th>设备</th><th>Agent</th><th>消息</th></tr></thead><tbody>';
      list.forEach(function (e) {
        const ts = (e.timestamp || '').toString().replace('T', ' ').replace('Z', '');
        html += '<tr><td><small class="muted">' + esc(ts) + '</small></td><td>' + logLevelPill(e.level) + '</td><td>' + esc(e.source || '') + '</td><td><code title="' + esc(e.deviceID || '') + '">' + (e.deviceID || '') + '</code></td><td><code title="' + esc(e.agentID || '') + '">' + (e.agentID || '') + '</code></td><td class="wrap">' + esc(e.message || '') + '</td></tr>';
      });
      html += '</tbody></table></div>';
      document.getElementById('logList').innerHTML = html;
      updateLogPage(list.length);
    }).catch(function (err) { logMsg('error: ' + err, false); });
}

function updateLogPage(n) {
  const lim = parseInt(document.getElementById('logLimit').value || '200', 10);
  const cur = Math.floor(logOffset / lim) + 1;
  document.getElementById('logPageInfo').textContent = '第 ' + cur + ' 页（本页 ' + n + ' 条）';
}
export function logPrev() { if (logOffset > 0) { searchLogs(Math.max(0, logOffset - parseInt(document.getElementById('logLimit').value || '200', 10))); } }
export function logNext() { searchLogs(logOffset + parseInt(document.getElementById('logLimit').value || '200', 10)); }
export function resetLogFilters() {
  ['logDevice', 'logAgent', 'logKeyword', 'logFrom', 'logTo'].forEach(function (id) { document.getElementById(id).value = ''; });
  document.getElementById('logLevel').value = ''; document.getElementById('logSource').value = '';
  document.getElementById('logList').innerHTML = '<p class="muted">已清空，填写条件后点「查询」。</p>';
}

// ---------- 监控告警（M7，独立 Tab） ----------
export function pollAlertsFull() {
  api.getAlerts().then(function (list) {
    const fl = applyFocus(list || [], 'alert');
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
          + '<button class="btn xs" onclick="ackAlert(\'' + esc(a.alertID) + '\')">' + icon('check', 14) + ' ' + esc(t('alerts.ack')) + '</button>'
          + '<button class="btn xs outline" onclick="silenceAlert(\'' + esc(a.alertID) + '\')">' + icon('close', 14) + ' ' + esc(t('alerts.silence')) + '</button>'
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
        + '<button class="jbtn" style="margin-top:6px" onclick="setFocus(\'' + esc(a.deviceID) + '\',\'\',\'\',\'\');switchTab(\'alerts\')">' + icon('context', 14) + ' ' + esc(t('render.contextLink')) + '</button>'
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

// ---------- 动态身份注入（F3） ----------
export function fetchMe() {
  api.getMe().then(function (x) {
    if (x.s !== 200) return;
    const t = x.j.tenantID || 'default'; const u = x.j.userID || 'local';
    const role = (x.j.roles && x.j.roles.length) ? x.j.roles.join('/') : 'admin';
    const te = document.getElementById('idTenant'); if (te) te.textContent = t;
    const re = document.getElementById('idRole'); if (re) re.textContent = role;
    const chip = document.getElementById('identityChip'); if (chip) chip.title = '身份由前置网关注入（X-Tenant / X-User / X-User-Roles）；当前：租户 ' + t + ' · 用户 ' + u;
  }).catch(function (e) { console.error('me', e); });
}

// ---------- Agents 下拉加载 ----------
export function loadAgents() {
  api.getAgents().then(function (a) {
    const sel = document.getElementById('agentID'); sel.innerHTML = '';
    (a || []).forEach(function (x) { const o = document.createElement('option'); o.value = x.agentID; o.textContent = x.agentID + ' (' + x.hostname + ')'; sel.appendChild(o); });
  }).catch(function (e) { console.error(e); });
}

// ---------- 任务下发表单提交 ----------
export function submitTaskForm() {
  const body = {
    agentID: document.getElementById('agentID').value,
    type: document.getElementById('type').value,
    command: document.getElementById('command').value,
    path: document.getElementById('path').value,
    content: document.getElementById('content').value,
  };
  api.createTask(body)
    .then(function (x) { const el = document.getElementById('taskResult'); el.className = 'msg ' + (x.s < 400 ? 'ok' : 'err'); el.textContent = '[' + x.s + '] ' + JSON.stringify(x.j); pollTasks(); pollDevices(); })
    .catch(function (err) { const el = document.getElementById('taskResult'); el.className = 'msg err'; el.textContent = 'error: ' + err; });
}

// ============================================================
// OS 基础环境优化
// ============================================================
// 模板列表加载、分类筛选、详情查看、执行。
// 后端契约：
//   GET  /api/v1/os-templates[?category=] → OSTemplate[]
//   GET  /api/v1/os-templates/{id}         → OSTemplate
//   POST /api/v1/os-templates/{id}/execute {agentID, params[]} → task

// 当前分类筛选（模块级状态）
let osoptCurrentCat = '';
// 当前待执行的模板 ID（执行对话框用）
let osoptExecId = '';
// 当前待执行的模板详情（用于参数动态渲染，Phase 2）
let osoptExecTpl = null;
// 当前执行轮询定时器（用于在关闭对话框时停止轮询）
let osoptExecTimer = null;

// ---------- 任务结果轮询（执行/部署/卸载日志展示） ----------
// 通用轮询：每 interval 毫秒调用 api.getTaskDetail(taskID) 获取任务状态与输出，
// 将 stdout/stderr 写入 logEl；任务完成（completed/failed）或达到最大轮询次数后停止。
// 参数：
//   taskID   - 任务 ID
//   logEl    - 日志展示 DOM 元素（<pre>）
//   interval - 轮询间隔（毫秒），默认 3000
//   onDone   - 完成回调（可选），签名 function(status) 其中 status 为 completed/failed/timeout
// 返回：定时器句柄（可 clearInterval）
//
// Phase 2 增强：在日志头部展示任务状态（pending/running/completed/failed），
// 并保留 output 主体；状态行使用 osopt.taskStatus.* 翻译键。
export function pollTaskResult(taskID, logEl, interval, onDone) {
  let pollCount = 0;
  const maxPolls = 40; // 最多轮询 40 次（约 120 秒，适配长任务）
  // 状态 → 翻译键映射（含图标前缀，便于一眼识别）
  function statusLine(st) {
    const key = 'osopt.taskStatus.' + (st || 'pending');
    const label = t(key);
    if (st === 'completed') return '✓ [' + label + ']';
    if (st === 'failed')    return '✗ [' + label + ']';
    if (st === 'running')   return '⏳ [' + label + ']';
    return '… [' + label + ']';
  }
  const timer = setInterval(function () {
    pollCount++;
    api.getTaskDetail(taskID).then(function (r) {
      // getTaskDetail 通过 authGet 返回解析后的 json（任务对象）
      const task = r && r.task ? r.task : r;
      if (!task) return;
      const st = task.status || '';
      // 拼接日志：状态行 + output（stdout/stderr 拼接）
      if (logEl) {
        const output = String(task.output || '');
        logEl.textContent = statusLine(st) + '\n' + output;
      }
      // 检查是否结束
      if (st === 'completed' || st === 'failed' || pollCount >= maxPolls) {
        clearInterval(timer);
        if (logEl) {
          if (st === 'completed') {
            logEl.textContent += '\n✓ ' + t('osopt.execSuccess');
          } else if (st === 'failed') {
            logEl.textContent += '\n✗ ' + t('osopt.execFailed');
          } else {
            logEl.textContent += '\n⚠ ' + t('osopt.pollTimeout');
          }
        }
        if (typeof onDone === 'function') {
          onDone(st === 'completed' ? 'completed' : (st === 'failed' ? 'failed' : 'timeout'));
        }
      }
    }).catch(function (e) {
      // 网络错误，继续轮询直到 maxPolls
      console.error('[pollTask]', e);
      if (pollCount >= maxPolls) {
        clearInterval(timer);
        if (logEl) {
          logEl.textContent += '\n⚠ ' + t('osopt.pollTimeout');
        }
        if (typeof onDone === 'function') onDone('timeout');
      }
    });
  }, interval || 3000);
  return timer;
}

// ---------- 参数验证辅助函数 ----------
// 判断参数名是否为端口类型（包含 port）
function isPortParam(name) {
  return (name || '').toLowerCase().indexOf('port') >= 0;
}
// 判断参数名是否为路径类型（包含 dir 或 path）
function isPathParam(name) {
  const n = (name || '').toLowerCase();
  return n.indexOf('dir') >= 0 || n.indexOf('path') >= 0;
}
// 判断参数名是否为密码类型（包含 password 或 pwd）
function isPasswordParam(name) {
  const n = (name || '').toLowerCase();
  return n.indexOf('password') >= 0 || n.indexOf('pwd') >= 0;
}

// Phase 2：密码强度计算
// 规则：长度<8 → weak；长度>=8 且含大小写+数字 → medium；含大小写+数字+特殊字符 且长度>=8 → strong
// 返回 'weak' / 'medium' / 'strong' / null（空值返回 null）
function passwordStrength(v) {
  if (!v) return null;
  const hasLower = /[a-z]/.test(v);
  const hasUpper = /[A-Z]/.test(v);
  const hasDigit = /\d/.test(v);
  const hasSpecial = /[^a-zA-Z0-9]/.test(v);
  if (v.length < 8) return 'weak';
  if (hasLower && hasUpper && hasDigit && hasSpecial) return 'strong';
  if (hasLower && hasUpper && hasDigit) return 'medium';
  return 'weak';
}

// Phase 2：参数输入实时反馈（oninput 触发）
// - 必填：未填写时红色边框 + 提示，填写后恢复
// - 密码：实时显示强度（弱/中/强）
// - 端口：超出范围时红色边框
// 通过 window._opsmeshParamOnInput 暴露给内联 oninput 调用
function paramInputOnInput(inp) {
  if (!inp) return;
  const name = inp.getAttribute('data-pname') || '';
  const v = inp.value || '';
  // 必填红色边框
  const required = inp.hasAttribute('required');
  if (required && !v.trim()) {
    inp.style.borderColor = 'var(--fail)';
    inp.style.borderWidth = '2px';
  } else {
    inp.style.borderColor = '';
    inp.style.borderWidth = '';
  }
  // 端口范围红色边框
  if (isPortParam(name) && v) {
    const n = parseInt(v, 10);
    if (isNaN(n) || n < 1 || n > 65535) {
      inp.style.borderColor = 'var(--fail)';
      inp.style.borderWidth = '2px';
    }
  }
  // 密码强度实时提示
  if (isPasswordParam(name)) {
    const strengthId = inp.id + '_strength';
    const el = document.getElementById(strengthId);
    if (el) {
      const s = passwordStrength(v);
      if (!s) {
        el.textContent = '';
        el.style.color = '';
      } else if (s === 'weak') {
        el.textContent = t('param.passwordWeak');
        el.style.color = 'var(--fail)';
      } else if (s === 'medium') {
        el.textContent = t('param.passwordMedium');
        el.style.color = 'var(--accent)';
      } else {
        el.textContent = t('param.passwordStrong');
        el.style.color = 'var(--green)';
      }
    }
  }
}
// 暴露到 window 供内联 oninput 调用（ES6 模块作用域隔离）
try { window._opsmeshParamOnInput = paramInputOnInput; } catch (_) {}

// 验证单个参数值：返回 null 表示通过，否则返回错误消息
function validateParamValue(p, value) {
  const name = p.name || '';
  const v = (value || '').trim();
  // 必填校验
  if (p.required && !v) {
    return t('param.required');
  }
  if (!v) return null; // 非必填且为空，跳过后续校验
  // 端口校验
  if (isPortParam(name) || p.type === 'int') {
    if (!/^-?\d+$/.test(v)) return t('osopt.invalidPort');
    const n = parseInt(v, 10);
    if (isNaN(n) || n < 1 || n > 65535) return t('osopt.invalidPort');
  }
  // 路径校验
  if (isPathParam(name)) {
    if (v.charAt(0) !== '/') return t('osopt.invalidPath');
  }
  // 密码强度校验：必填密码至少要求 medium（避免弱密码提交）
  if ((p.type === 'password' || isPasswordParam(name)) && p.required) {
    const s = passwordStrength(v);
    if (s === 'weak') return t('param.passwordWeak');
  }
  return null;
}

// 渲染单个参数输入框（含 label / hint / required 标记 / 实时验证反馈）
// 用于 OS 优化执行对话框与中间件部署对话框的参数表单。
// prefix - DOM id 前缀，避免重复（'os' / 'mw'）
function renderParamInput(p, prefix) {
  const name = p.name || '';
  const def = p.default != null ? String(p.default) : '';
  const required = p.required ? ' <span style="color:var(--fail)">*</span>' : '';
  const placeholder = p.description || '';
  const type = p.type || 'text';
  // 输入框类型：int → number；password → password（Phase 2 暴露密码强度提示）
  let inputType = 'text';
  if (type === 'int' || isPortParam(name)) inputType = 'number';
  else if (type === 'password' || isPasswordParam(name)) inputType = 'password';
  // 端口范围 min/max
  let rangeAttr = '';
  if (inputType === 'number' && isPortParam(name)) {
    rangeAttr = ' min="1" max="65535"';
  }
  // 提示文本
  const hints = [];
  if (isPortParam(name)) hints.push(t('param.portRange'));
  if (isPathParam(name)) hints.push(t('osopt.pathHint'));
  if (isPasswordParam(name)) hints.push(t('osopt.passwordHint'));
  const hintHtml = hints.length ? ' <span class="field-hint" style="margin-left:4px;color:var(--text-3)">' + esc(hints.join(' · ')) + '</span>' : '';
  // 必填属性
  const requiredAttr = p.required ? ' required' : '';
  // 唯一 id（用于 label/input 关联与验证读取）
  const inputId = prefix + 'Param_' + esc(name).replace(/[^a-zA-Z0-9_]/g, '_');
  // Phase 2：密码强度提示元素（实时更新）
  const isPwd = (type === 'password' || isPasswordParam(name));
  const strengthHtml = isPwd
    ? ' <span id="' + inputId + '_strength" style="margin-left:6px;font-size:12px;font-weight:600"></span>'
    : '';
  // Phase 2：oninput 实时验证（必填红色边框 + 密码强度 + 端口范围）
  const onInputAttr = ' oninput="window._opsmeshParamOnInput && window._opsmeshParamOnInput(this)"';
  let html = '<div style="margin-bottom:8px">'
    + '<label style="display:block;margin-bottom:2px" for="' + inputId + '"><code>' + esc(name) + '</code>' + required + hintHtml + strengthHtml + '</label>'
    + '<input type="' + inputType + '"' + rangeAttr + requiredAttr + onInputAttr + ' class="form-control" id="' + inputId + '" data-pname="' + esc(name) + '" value="' + esc(def) + '" placeholder="' + esc(placeholder) + '">'
    + '</div>';
  return html;
}

// 收集参数表单值并执行校验。
//   paramsEl - 包含 input[data-pname] 的容器
//   params   - 模板参数定义数组（用于校验）
// 返回：{ok: true, values: {...}} 或 {ok: false, errors: ['xxx: msg', ...]}
// Phase 2：校验失败时对对应输入框添加红色边框，通过时清除红色边框
function collectAndValidateParams(paramsEl, params) {
  const values = {};
  const errors = [];
  if (!paramsEl || !params) return { ok: true, values: values };
  const inputs = paramsEl.querySelectorAll('input[data-pname]');
  inputs.forEach(function (inp) {
    const k = inp.getAttribute('data-pname');
    if (!k) return;
    values[k] = inp.value;
  });
  // 对每个定义的参数执行校验
  (params || []).forEach(function (p) {
    const name = p.name || '';
    const err = validateParamValue(p, values[name] || '');
    if (err) errors.push(name + ': ' + err);
  });
  // Phase 2：校验失败时对对应输入框添加红色边框，通过时清除
  if (paramsEl) {
    (params || []).forEach(function (p) {
      const name = p.name || '';
      const inputId = (paramsEl.querySelector('input[data-pname="' + name + '"]'));
      if (inputId) {
        const err = validateParamValue(p, values[name] || '');
        if (err) {
          inputId.style.borderColor = 'var(--fail)';
          inputId.style.borderWidth = '2px';
        } else {
          inputId.style.borderColor = '';
          inputId.style.borderWidth = '';
        }
      }
    });
  }
  return { ok: errors.length === 0, values: values, errors: errors };
}

// 风险等级 → 颜色 + 文本键
function osoptRiskStyle(risk) {
  if (risk === 'low') return { color: 'var(--green)', bg: 'var(--green-soft, #e6f9ee)' };
  if (risk === 'high') return { color: 'var(--fail)', bg: 'var(--fail-soft, #fde8e8)' };
  return { color: 'var(--accent)', bg: 'var(--accent-soft)' };
}

// 渲染风险等级标签
function osoptRiskBadge(risk) {
  const s = osoptRiskStyle(risk);
  return '<span class="badge" style="background:' + s.bg + ';color:' + s.color + ';border:1px solid ' + s.color + '">' + esc(t('osopt.risk.' + (risk || 'medium'))) + '</span>';
}

// 渲染分类标签
function osoptCatBadge(cat) {
  const key = 'osopt.category.' + (cat || 'all');
  return '<span class="badge" style="background:var(--bg-2);color:var(--text-2)">' + esc(t(key)) + '</span>';
}

// 加载 OS 优化模板列表（按当前分类筛选）
export function loadOSTemplates() {
  const listEl = document.getElementById('osTemplateList');
  if (listEl) listEl.innerHTML = '<p class="muted">' + esc(t('osopt.loading')) + '</p>';
  // 隐藏详情面板
  const detailEl = document.getElementById('osTemplateDetail');
  if (detailEl) detailEl.style.display = 'none';
  api.getOSTemplates(osoptCurrentCat).then(function (list) {
    const el = document.getElementById('osTemplateList');
    if (!el) return;
    if (!list || list.length === 0) {
      el.innerHTML = '<p class="muted">' + esc(t('osopt.noTemplates')) + '</p>';
      return;
    }
    let html = '<div class="table-wrap"><table><colgroup>'
      + '<col style="width:18%"><col style="width:26%"><col style="width:14%"><col style="width:14%"><col style="width:28%">'
      + '</colgroup><thead><tr>'
      + '<th>' + esc(t('osopt.col.id')) + '</th>'
      + '<th>' + esc(t('osopt.col.name')) + '</th>'
      + '<th>' + esc(t('osopt.col.category')) + '</th>'
      + '<th>' + esc(t('osopt.col.risk')) + '</th>'
      + '<th>' + esc(t('osopt.col.action')) + '</th>'
      + '</tr></thead><tbody>';
    list.forEach(function (tpl) {
      const tid = esc(tpl.id || '');
      html += '<tr>'
        + '<td><code title="' + tid + '">' + tid + '</code></td>'
        + '<td>' + esc(tpl.name || '') + '</td>'
        + '<td>' + osoptCatBadge(tpl.category) + '</td>'
        + '<td>' + osoptRiskBadge(tpl.risk) + '</td>'
        + '<td>'
        + '<button class="btn btn-sm" onclick="showOSTemplateDetail(\'' + tid + '\')" style="margin-right:6px">' + icon('search', 12) + ' ' + esc(t('osopt.view')) + '</button>'
        + '<button class="btn btn-primary btn-sm" onclick="executeOSOptimize(\'' + tid + '\')">' + icon('task', 12) + ' ' + esc(t('osopt.execute')) + '</button>'
        + '</td>'
        + '</tr>';
    });
    html += '</tbody></table></div>';
    el.innerHTML = html;
  }).catch(function (e) {
    console.error('[os-templates]', e);
    const el = document.getElementById('osTemplateList');
    if (el) el.innerHTML = '<p class="muted">' + esc(t('osopt.networkError')) + '</p>';
  });
}

// 分类筛选：切换分类并重新加载
export function filterOSTemplates(category) {
  osoptCurrentCat = category || '';
  // 更新按钮 active 状态
  document.querySelectorAll('.osopt-cat-btn').forEach(function (btn) {
    btn.classList.toggle('active', (btn.getAttribute('data-cat') || '') === osoptCurrentCat);
  });
  loadOSTemplates();
}

// 显示模板详情
export function showOSTemplateDetail(id) {
  const detailEl = document.getElementById('osTemplateDetail');
  if (!detailEl) return;
  detailEl.style.display = '';
  detailEl.innerHTML = '<p class="muted">' + esc(t('osopt.loading')) + '</p>';
  api.getOSTemplate(id).then(function (tpl) {
    const el = document.getElementById('osTemplateDetail');
    if (!el) return;
    if (!tpl || !tpl.id) {
      el.innerHTML = '<p class="muted">' + esc(t('osopt.noTemplates')) + '</p>';
      return;
    }
    const tags = (tpl.tags || []).map(function (tag) { return '<span class="badge" style="background:var(--bg-2);color:var(--text-2);margin-right:4px">' + esc(tag) + '</span>'; }).join('');
    let html = '<div style="display:flex;justify-content:space-between;align-items:flex-start;flex-wrap:wrap;gap:8px;margin-bottom:12px">'
      + '<h4 style="margin:0">' + esc(tpl.name || tpl.id) + ' <code style="font-size:12px;color:var(--text-3)">' + esc(tpl.id) + '</code></h4>'
      + '<button class="btn btn-sm" onclick="hideOSTemplateDetail()">' + icon('close', 12) + ' ' + esc(t('osopt.detailClose')) + '</button>'
      + '</div>';
    html += '<div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:12px;margin-bottom:12px">'
      + '<div><div class="field-hint">' + esc(t('osopt.col.category')) + '</div><div>' + osoptCatBadge(tpl.category) + '</div></div>'
      + '<div><div class="field-hint">' + esc(t('osopt.col.risk')) + '</div><div>' + osoptRiskBadge(tpl.risk) + '</div></div>'
      + '<div><div class="field-hint">' + esc(t('osopt.detailOs')) + '</div><div>' + esc(tpl.os || 'all') + '</div></div>'
      + '<div><div class="field-hint">' + esc(t('osopt.detailTags')) + '</div><div>' + (tags || '-') + '</div></div>'
      + '</div>';
    html += '<div style="margin-bottom:12px"><div class="field-hint">' + esc(t('osopt.detailDesc')) + '</div><div>' + esc(tpl.description || '-') + '</div></div>';
    html += '<div><div class="field-hint" style="margin-bottom:4px">' + esc(t('osopt.detailCommands')) + '</div>'
      + '<pre style="background:var(--bg-2);color:var(--text-1);padding:12px;border-radius:6px;overflow:auto;max-height:320px;font-size:12px;line-height:1.5;white-space:pre-wrap;word-break:break-all">' + esc(tpl.commands || '') + '</pre>'
      + '</div>';
    html += '<div style="margin-top:12px;text-align:right">'
      + '<button class="btn btn-primary btn-sm" onclick="executeOSOptimize(\'' + esc(tpl.id) + '\')">' + icon('task', 12) + ' ' + esc(t('osopt.execute')) + '</button>'
      + '</div>';
    el.innerHTML = html;
  }).catch(function (e) {
    console.error('[os-template-detail]', e);
    const el = document.getElementById('osTemplateDetail');
    if (el) el.innerHTML = '<p class="muted">' + esc(t('osopt.networkError')) + '</p>';
  });
}

// 隐藏模板详情
export function hideOSTemplateDetail() {
  const el = document.getElementById('osTemplateDetail');
  if (el) { el.style.display = 'none'; el.innerHTML = ''; }
}

// 打开执行对话框：加载设备列表供选择 + 加载模板详情用于动态生成参数表单
export function executeOSOptimize(id) {
  osoptExecId = id;
  osoptExecTpl = null;
  const modal = document.getElementById('osExecModal');
  if (!modal) return;
  // 重置结果区
  const resultEl = document.getElementById('osExecResult');
  if (resultEl) { resultEl.innerHTML = ''; resultEl.className = ''; }
  // 重置日志区（Phase 2）
  const logEl = document.getElementById('osExecLog');
  if (logEl) logEl.textContent = '';
  // 停止上一次的轮询
  if (osoptExecTimer) { clearInterval(osoptExecTimer); osoptExecTimer = null; }
  // 加载模板详情用于动态生成参数表单（Phase 2 参数验证 UI）
  api.getOSTemplate(id).then(function (tpl) {
    osoptExecTpl = tpl || null;
    // 渲染动态参数表单
    renderOSExecParams();
  }).catch(function (e) {
    console.error('[os-exec template]', e);
    osoptExecTpl = null;
    renderOSExecParams();
  });
  // 加载设备列表到下拉
  const agentSel = document.getElementById('osExecAgent');
  if (agentSel) {
    agentSel.innerHTML = '<option value="">— ' + esc(t('osopt.selectAgent')) + ' —</option>';
    api.getDevices().then(function (devs) {
      if (!agentSel) return;
      // devs 可能是数组或按网段分组的对象，统一拍平为数组
      let arr = [];
      if (Array.isArray(devs)) {
        arr = devs;
      } else if (devs && typeof devs === 'object') {
        Object.keys(devs).forEach(function (seg) {
          const v = devs[seg];
          if (Array.isArray(v)) { v.forEach(function (d) { arr.push(d); }); }
          else { arr.push(v); }
        });
      }
      arr.forEach(function (d) {
        const aid = d.agentID || d.id || '';
        if (!aid) return;
        const label = aid + ' (' + esc(d.hostname || d.ip || d.deviceID || '') + ')';
        const o = document.createElement('option');
        o.value = aid;
        o.textContent = label;
        agentSel.appendChild(o);
      });
    }).catch(function (e) { console.error('[os-exec devices]', e); });
  }
  modal.classList.add('open');
}

// 渲染 OS 优化执行参数表单（根据当前模板的 Params 定义动态生成）
function renderOSExecParams() {
  const paramsEl = document.getElementById('osExecParams');
  if (!paramsEl) return;
  const tpl = osoptExecTpl;
  if (!tpl || !tpl.params || tpl.params.length === 0) {
    // 无参数定义：保留 textarea 兼容旧契约（每行一个）
    paramsEl.innerHTML = '<textarea id="osExecParamsRaw" class="form-control" rows="3" placeholder="param1&#10;param2"></textarea>';
    return;
  }
  // 有参数定义：动态生成输入框（含验证提示）
  let html = '';
  tpl.params.forEach(function (p) {
    html += renderParamInput(p, 'os');
  });
  paramsEl.innerHTML = html;
}

// 关闭执行对话框：停止轮询并清理状态
export function closeOSExecModal() {
  const modal = document.getElementById('osExecModal');
  if (modal) modal.classList.remove('open');
  if (osoptExecTimer) { clearInterval(osoptExecTimer); osoptExecTimer = null; }
  osoptExecId = '';
  osoptExecTpl = null;
}

// 确认执行：调用 API 在选定设备上执行模板
// Phase 2 改造：提交前参数验证 + 提交后轮询任务状态 + 日志展示
export function confirmOSExec() {
  const resultEl = document.getElementById('osExecResult');
  if (!osoptExecId) {
    if (resultEl) { resultEl.className = 'msg err'; resultEl.textContent = t('osopt.execFail') + ': no template'; }
    return;
  }
  const agentSel = document.getElementById('osExecAgent');
  const agentID = agentSel ? agentSel.value : '';
  if (!agentID) {
    if (resultEl) { resultEl.className = 'msg err'; resultEl.textContent = t('osopt.noAgent'); }
    return;
  }
  // 收集参数 + 验证
  const paramsEl = document.getElementById('osExecParams');
  let params = [];
  if (osoptExecTpl && osoptExecTpl.params && osoptExecTpl.params.length > 0) {
    // 动态参数模式：收集并校验
    const r = collectAndValidateParams(paramsEl, osoptExecTpl.params);
    if (!r.ok) {
      if (resultEl) {
        resultEl.className = 'msg err';
        resultEl.textContent = t('osopt.paramInvalid') + '：' + r.errors.join('；');
      }
      return;
    }
    // 转为数组（按模板定义顺序）
    osoptExecTpl.params.forEach(function (p) {
      params.push(r.values[p.name] != null ? String(r.values[p.name]) : '');
    });
  } else {
    // 兼容旧契约：textarea 每行一个
    const rawEl = document.getElementById('osExecParamsRaw');
    const paramsText = rawEl ? rawEl.value : (paramsEl ? paramsEl.value : '');
    params = paramsText.split('\n').map(function (s) { return s.trim(); }).filter(function (s) { return s.length > 0; });
  }

  if (resultEl) { resultEl.className = 'msg'; resultEl.textContent = t('osopt.loading'); }
  // 清空日志区
  const logEl = document.getElementById('osExecLog');
  if (logEl) logEl.textContent = '';
  api.executeOSTemplate(osoptExecId, agentID, params).then(function (r) {
    if (!resultEl) return;
    if (r && r.s && r.s < 400 && r.j) {
      const taskId = (r.j.taskID || r.j.id || r.j.taskId || '');
      resultEl.className = 'msg ok';
      resultEl.textContent = t('osopt.execSuccess') + (taskId || JSON.stringify(r.j));
      // Phase 2：自动轮询任务状态并展示日志
      if (taskId && logEl) {
        logEl.textContent = t('osopt.polling') + '\n';
        if (osoptExecTimer) clearInterval(osoptExecTimer);
        osoptExecTimer = pollTaskResult(taskId, logEl, 3000, function (status) {
          if (status === 'completed') {
            resultEl.className = 'msg ok';
            resultEl.textContent = t('osopt.execSuccess') + taskId;
          } else if (status === 'failed') {
            resultEl.className = 'msg err';
            resultEl.textContent = t('osopt.execFailed') + ' (task: ' + taskId + ')';
          }
          osoptExecTimer = null;
        });
      } else {
        // 无 taskID 或无日志区，延迟关闭对话框
        setTimeout(closeOSExecModal, 2000);
      }
    } else {
      resultEl.className = 'msg err';
      resultEl.textContent = t('osopt.execFail') + ': [' + (r && r.s || '?') + '] ' + (r && r.j ? JSON.stringify(r.j) : '');
    }
  }).catch(function (e) {
    console.error('[os-exec]', e);
    if (resultEl) {
      resultEl.className = 'msg err';
      resultEl.textContent = t('osopt.execFail') + ': ' + (e && e.message || e);
    }
  });
}

// ============================================================
// 中间件部署
// ============================================================
// 模板列表加载、分类筛选、详情查看、部署、实例列表。
// 后端契约：
//   GET  /api/v1/middleware-templates[?category=] → MiddlewareTemplate[]
//   GET  /api/v1/middleware-templates/{id}        → MiddlewareTemplate
//   POST /api/v1/middleware-templates/{id}/deploy {agentID, deployType, params} → {taskID}
//   GET  /api/v1/middleware-instances             → MiddlewareInstance[]
// MiddlewareTemplate 字段：
//   id, name, category, version, description, deployTypes[], params[], scripts{docker,systemd}, risk, tags[]
// scripts.docker / scripts.systemd 各含 {deploy, verify, uninstall}

// 当前分类筛选（模块级状态）
let mwdepCurrentCat = '';
// 当前待部署的模板（部署对话框用）
let mwdepDeployTpl = null;
// 当前部署轮询定时器（用于在关闭对话框时停止轮询）
let mwdepDeployTimer = null;
// 当前待卸载的实例（卸载对话框用）
let mwdepUninstallIns = null;
// 当前卸载轮询定时器
let mwdepUninstallTimer = null;

// 风险等级 → 颜色 + 文本键
function mwdepRiskStyle(risk) {
  if (risk === 'low') return { color: 'var(--green)', bg: 'var(--green-soft, #e6f9ee)' };
  if (risk === 'high') return { color: 'var(--fail)', bg: 'var(--fail-soft, #fde8e8)' };
  return { color: 'var(--accent)', bg: 'var(--accent-soft)' };
}

// 渲染风险等级标签
function mwdepRiskBadge(risk) {
  const s = mwdepRiskStyle(risk);
  return '<span class="badge" style="background:' + s.bg + ';color:' + s.color + ';border:1px solid ' + s.color + '">' + esc(t('mwdep.risk.' + (risk || 'medium'))) + '</span>';
}

// 渲染分类标签
function mwdepCatBadge(cat) {
  const key = 'mwdep.category.' + (cat || 'all');
  return '<span class="badge" style="background:var(--bg-2);color:var(--text-2)">' + esc(t(key)) + '</span>';
}

// 渲染部署方式标签
function mwdepDeployTypeBadge(dt) {
  return '<span class="badge" style="background:var(--bg-2);color:var(--text-2);margin-right:4px">' + esc(t('mwdep.deployType.' + (dt || 'docker'))) + '</span>';
}

// 加载中间件部署模板列表（按当前分类筛选）
export function loadMiddlewareTemplates() {
  const listEl = document.getElementById('mwTemplateList');
  if (listEl) listEl.innerHTML = '<p class="muted">' + esc(t('mwdep.loading')) + '</p>';
  // 隐藏详情面板
  const detailEl = document.getElementById('mwTemplateDetail');
  if (detailEl) detailEl.style.display = 'none';
  api.getMiddlewareTemplates(mwdepCurrentCat).then(function (list) {
    const el = document.getElementById('mwTemplateList');
    if (!el) return;
    if (!list || list.length === 0) {
      el.innerHTML = '<p class="muted">' + esc(t('mwdep.noTemplates')) + '</p>';
      return;
    }
    let html = '<div class="table-wrap"><table><colgroup>'
      + '<col style="width:14%"><col style="width:20%"><col style="width:12%"><col style="width:10%"><col style="width:16%"><col style="width:10%"><col style="width:18%">'
      + '</colgroup><thead><tr>'
      + '<th>' + esc(t('mwdep.col.id')) + '</th>'
      + '<th>' + esc(t('mwdep.col.name')) + '</th>'
      + '<th>' + esc(t('mwdep.col.category')) + '</th>'
      + '<th>' + esc(t('mwdep.col.version')) + '</th>'
      + '<th>' + esc(t('mwdep.col.deployTypes')) + '</th>'
      + '<th>' + esc(t('mwdep.col.risk')) + '</th>'
      + '<th>' + esc(t('mwdep.col.action')) + '</th>'
      + '</tr></thead><tbody>';
    list.forEach(function (tpl) {
      const tid = esc(tpl.id || '');
      const deployTypesHtml = (tpl.deployTypes || []).map(function (dt) { return mwdepDeployTypeBadge(dt); }).join('');
      html += '<tr>'
        + '<td><code title="' + tid + '">' + tid + '</code></td>'
        + '<td>' + esc(tpl.name || '') + '</td>'
        + '<td>' + mwdepCatBadge(tpl.category) + '</td>'
        + '<td>' + esc(tpl.version || '-') + '</td>'
        + '<td>' + (deployTypesHtml || '-') + '</td>'
        + '<td>' + mwdepRiskBadge(tpl.risk) + '</td>'
        + '<td>'
        + '<button class="btn btn-sm" onclick="showMiddlewareDetail(\'' + tid + '\')" style="margin-right:6px">' + icon('search', 12) + ' ' + esc(t('mwdep.view')) + '</button>'
        + '<button class="btn btn-primary btn-sm" onclick="deployMiddleware(\'' + tid + '\')">' + icon('deploy', 12) + ' ' + esc(t('mwdep.deploy')) + '</button>'
        + '</td>'
        + '</tr>';
    });
    html += '</tbody></table></div>';
    el.innerHTML = html;
  }).catch(function (e) {
    console.error('[mw-templates]', e);
    const el = document.getElementById('mwTemplateList');
    if (el) el.innerHTML = '<p class="muted">' + esc(t('mwdep.networkError')) + '</p>';
  });
}

// 分类筛选：切换分类并重新加载
export function filterMiddlewareTemplates(category) {
  mwdepCurrentCat = category || '';
  // 更新按钮 active 状态
  document.querySelectorAll('#mwdepCatFilter .osopt-cat-btn').forEach(function (btn) {
    btn.classList.toggle('active', (btn.getAttribute('data-cat') || '') === mwdepCurrentCat);
  });
  loadMiddlewareTemplates();
}

// 显示模板详情
export function showMiddlewareDetail(id) {
  const detailEl = document.getElementById('mwTemplateDetail');
  if (!detailEl) return;
  detailEl.style.display = '';
  detailEl.innerHTML = '<p class="muted">' + esc(t('mwdep.loading')) + '</p>';
  api.getMiddlewareTemplate(id).then(function (tpl) {
    const el = document.getElementById('mwTemplateDetail');
    if (!el) return;
    if (!tpl || !tpl.id) {
      el.innerHTML = '<p class="muted">' + esc(t('mwdep.noTemplates')) + '</p>';
      return;
    }
    const tags = (tpl.tags || []).map(function (tag) { return '<span class="badge" style="background:var(--bg-2);color:var(--text-2);margin-right:4px">' + esc(tag) + '</span>'; }).join('');
    const deployTypesHtml = (tpl.deployTypes || []).map(function (dt) { return mwdepDeployTypeBadge(dt); }).join('');
    let html = '<div style="display:flex;justify-content:space-between;align-items:flex-start;flex-wrap:wrap;gap:8px;margin-bottom:12px">'
      + '<h4 style="margin:0">' + esc(tpl.name || tpl.id) + ' <code style="font-size:12px;color:var(--text-3)">' + esc(tpl.id) + '</code></h4>'
      + '<button class="btn btn-sm" onclick="hideMiddlewareDetail()">' + icon('close', 12) + ' ' + esc(t('mwdep.detailClose')) + '</button>'
      + '</div>';
    html += '<div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:12px;margin-bottom:12px">'
      + '<div><div class="field-hint">' + esc(t('mwdep.col.category')) + '</div><div>' + mwdepCatBadge(tpl.category) + '</div></div>'
      + '<div><div class="field-hint">' + esc(t('mwdep.detailVersion')) + '</div><div>' + esc(tpl.version || '-') + '</div></div>'
      + '<div><div class="field-hint">' + esc(t('mwdep.col.risk')) + '</div><div>' + mwdepRiskBadge(tpl.risk) + '</div></div>'
      + '<div><div class="field-hint">' + esc(t('mwdep.col.deployTypes')) + '</div><div>' + (deployTypesHtml || '-') + '</div></div>'
      + '<div><div class="field-hint">' + esc(t('mwdep.detailTags')) + '</div><div>' + (tags || '-') + '</div></div>'
      + '</div>';
    html += '<div style="margin-bottom:12px"><div class="field-hint">' + esc(t('mwdep.detailDesc')) + '</div><div>' + esc(tpl.description || '-') + '</div></div>';
    // 参数列表
    const params = tpl.params || [];
    if (params.length > 0) {
      html += '<div style="margin-bottom:12px"><div class="field-hint" style="margin-bottom:4px">' + esc(t('mwdep.detailParams')) + '</div>';
      html += '<div class="table-wrap"><table><colgroup><col style="width:20%"><col style="width:14%"><col style="width:14%"><col style="width:14%"><col style="width:38%"></colgroup><thead><tr><th>Name</th><th>Type</th><th>Default</th><th>Required</th><th>Description</th></tr></thead><tbody>';
      params.forEach(function (p) {
        html += '<tr>'
          + '<td><code>' + esc(p.name || '') + '</code></td>'
          + '<td>' + esc(p.type || '-') + '</td>'
          + '<td>' + esc(p.default != null ? String(p.default) : '-') + '</td>'
          + '<td>' + (p.required ? '<span class="badge" style="background:var(--fail-soft);color:var(--fail)">' + esc(t('mwdep.param.required')) + '</span>' : '<span class="badge" style="background:var(--bg-2);color:var(--text-2)">' + esc(t('mwdep.param.optional')) + '</span>') + '</td>'
          + '<td>' + esc(p.description || '-') + '</td>'
          + '</tr>';
      });
      html += '</tbody></table></div></div>';
    }
    // 脚本：按部署方式分组显示
    const scripts = tpl.scripts || {};
    const scriptTypes = Object.keys(scripts);
    if (scriptTypes.length > 0) {
      html += '<div><div class="field-hint" style="margin-bottom:4px">' + esc(t('mwdep.detailScripts')) + '</div>';
      scriptTypes.forEach(function (st) {
        const sc = scripts[st] || {};
        html += '<div style="margin-bottom:12px">'
          + '<div style="font-weight:600;margin-bottom:4px">' + mwdepDeployTypeBadge(st) + '</div>'
          + '<div style="margin-bottom:6px"><div class="field-hint">' + esc(t('mwdep.detailDeployScript')) + '</div>'
          + '<pre style="background:var(--bg-2);color:var(--text-1);padding:12px;border-radius:6px;overflow:auto;max-height:200px;font-size:12px;line-height:1.5;white-space:pre-wrap;word-break:break-all">' + esc(sc.deploy || '') + '</pre></div>'
          + '<div style="margin-bottom:6px"><div class="field-hint">' + esc(t('mwdep.detailVerifyScript')) + '</div>'
          + '<pre style="background:var(--bg-2);color:var(--text-1);padding:12px;border-radius:6px;overflow:auto;max-height:160px;font-size:12px;line-height:1.5;white-space:pre-wrap;word-break:break-all">' + esc(sc.verify || '') + '</pre></div>'
          + '<div><div class="field-hint">' + esc(t('mwdep.detailUninstallScript')) + '</div>'
          + '<pre style="background:var(--bg-2);color:var(--text-1);padding:12px;border-radius:6px;overflow:auto;max-height:160px;font-size:12px;line-height:1.5;white-space:pre-wrap;word-break:break-all">' + esc(sc.uninstall || '') + '</pre></div>'
          + '</div>';
      });
      html += '</div>';
    }
    html += '<div style="margin-top:12px;text-align:right">'
      + '<button class="btn btn-primary btn-sm" onclick="deployMiddleware(\'' + esc(tpl.id) + '\')">' + icon('deploy', 12) + ' ' + esc(t('mwdep.deploy')) + '</button>'
      + '</div>';
    el.innerHTML = html;
  }).catch(function (e) {
    console.error('[mw-template-detail]', e);
    const el = document.getElementById('mwTemplateDetail');
    if (el) el.innerHTML = '<p class="muted">' + esc(t('mwdep.networkError')) + '</p>';
  });
}

// 隐藏模板详情
export function hideMiddlewareDetail() {
  const el = document.getElementById('mwTemplateDetail');
  if (el) { el.style.display = 'none'; el.innerHTML = ''; }
}

// 打开部署对话框：加载模板详情 + 设备列表 + 部署方式
export function deployMiddleware(id) {
  const modal = document.getElementById('mwDeployModal');
  if (!modal) return;
  // 重置结果区
  const resultEl = document.getElementById('mwDeployResult');
  if (resultEl) { resultEl.innerHTML = ''; resultEl.className = ''; }
  // 先加载模板详情（用于生成参数表单）
  api.getMiddlewareTemplate(id).then(function (tpl) {
    if (!tpl || !tpl.id) {
      if (resultEl) { resultEl.className = 'msg err'; resultEl.textContent = t('mwdep.deployFail') + ': template not found'; }
      return;
    }
    mwdepDeployTpl = tpl;
    // 部署方式下拉
    const typeSel = document.getElementById('mwDeployType');
    if (typeSel) {
      typeSel.innerHTML = '';
      (tpl.deployTypes || ['docker', 'systemd']).forEach(function (dt) {
        const o = document.createElement('option');
        o.value = dt;
        o.textContent = t('mwdep.deployType.' + dt);
        typeSel.appendChild(o);
      });
    }
    // 参数表单
    renderMwDeployParams();
    // 设备下拉
    const agentSel = document.getElementById('mwDeployAgent');
    if (agentSel) {
      agentSel.innerHTML = '<option value="">— ' + esc(t('mwdep.selectAgent')) + ' —</option>';
      api.getDevices().then(function (devs) {
        if (!agentSel) return;
        let arr = [];
        if (Array.isArray(devs)) {
          arr = devs;
        } else if (devs && typeof devs === 'object') {
          Object.keys(devs).forEach(function (seg) {
            const v = devs[seg];
            if (Array.isArray(v)) { v.forEach(function (d) { arr.push(d); }); }
            else { arr.push(v); }
          });
        }
        arr.forEach(function (d) {
          const aid = d.agentID || d.id || '';
          if (!aid) return;
          const label = aid + ' (' + esc(d.hostname || d.ip || d.deviceID || '') + ')';
          const o = document.createElement('option');
          o.value = aid;
          o.textContent = label;
          agentSel.appendChild(o);
        });
      }).catch(function (e) { console.error('[mw-deploy devices]', e); });
    }
    modal.classList.add('open');
  }).catch(function (e) {
    console.error('[mw-deploy template]', e);
    if (resultEl) { resultEl.className = 'msg err'; resultEl.textContent = t('mwdep.networkError'); }
    modal.classList.add('open');
  });
}

// 渲染部署参数表单（根据当前模板与部署方式）
// Phase 2 改造：使用通用 renderParamInput，添加端口范围 / 密码强度 / 路径提示 + required
function renderMwDeployParams() {
  const paramsEl = document.getElementById('mwDeployParams');
  if (!paramsEl || !mwdepDeployTpl) return;
  const params = mwdepDeployTpl.params || [];
  if (params.length === 0) {
    paramsEl.innerHTML = '<p class="muted">—</p>';
    return;
  }
  let html = '';
  params.forEach(function (p) {
    html += renderParamInput(p, 'mw');
  });
  paramsEl.innerHTML = html;
}

// 部署方式切换：当前模板不变，仅刷新参数表单（参数不随部署方式变化）
export function onMwDeployTypeChange() {
  // 参数由模板决定，与部署方式无关，无需重新渲染
}

// 关闭部署对话框：停止轮询并清理状态
export function closeMwDeployModal() {
  const modal = document.getElementById('mwDeployModal');
  if (modal) modal.classList.remove('open');
  if (mwdepDeployTimer) { clearInterval(mwdepDeployTimer); mwdepDeployTimer = null; }
  mwdepDeployTpl = null;
}

// 确认部署：调用 API 在选定设备上以指定方式部署中间件
// Phase 2 改造：提交前参数验证 + 提交后轮询任务状态 + 日志展示
export function confirmMwDeploy() {
  const resultEl = document.getElementById('mwDeployResult');
  if (!mwdepDeployTpl || !mwdepDeployTpl.id) {
    if (resultEl) { resultEl.className = 'msg err'; resultEl.textContent = t('mwdep.deployFail') + ': no template'; }
    return;
  }
  const typeSel = document.getElementById('mwDeployType');
  const deployType = typeSel ? typeSel.value : '';
  if (!deployType) {
    if (resultEl) { resultEl.className = 'msg err'; resultEl.textContent = t('mwdep.noDeployType'); }
    return;
  }
  const agentSel = document.getElementById('mwDeployAgent');
  const agentID = agentSel ? agentSel.value : '';
  if (!agentID) {
    if (resultEl) { resultEl.className = 'msg err'; resultEl.textContent = t('mwdep.noAgent'); }
    return;
  }
  // 收集参数 + 验证
  const paramsEl = document.getElementById('mwDeployParams');
  const r = collectAndValidateParams(paramsEl, (mwdepDeployTpl.params || []));
  if (!r.ok) {
    if (resultEl) {
      resultEl.className = 'msg err';
      resultEl.textContent = t('mwdep.paramInvalid') + '：' + r.errors.join('；');
    }
    return;
  }
  const params = r.values;

  if (resultEl) { resultEl.className = 'msg'; resultEl.textContent = t('mwdep.loading'); }
  // 清空日志区
  const logEl = document.getElementById('mwDeployLog');
  if (logEl) logEl.textContent = '';
  api.deployMiddleware(mwdepDeployTpl.id, agentID, deployType, params).then(function (r) {
    if (!resultEl) return;
    if (r && r.s && r.s < 400 && r.j) {
      const taskId = (r.j.taskID || r.j.id || r.j.taskId || '');
      resultEl.className = 'msg ok';
      resultEl.textContent = t('mwdep.deploySuccess') + (taskId || JSON.stringify(r.j));
      // Phase 2：自动轮询任务状态并展示部署日志
      if (taskId && logEl) {
        logEl.textContent = t('osopt.polling') + '\n';
        if (mwdepDeployTimer) clearInterval(mwdepDeployTimer);
        mwdepDeployTimer = pollTaskResult(taskId, logEl, 3000, function (status) {
          if (status === 'completed') {
            resultEl.className = 'msg ok';
            resultEl.textContent = t('mwdep.deploySuccess') + taskId;
            // 部署成功后刷新实例列表
            loadMiddlewareInstances();
          } else if (status === 'failed') {
            resultEl.className = 'msg err';
            resultEl.textContent = t('mwdep.deployFail') + ' (task: ' + taskId + ')';
          }
          mwdepDeployTimer = null;
        });
      } else {
        // 无 taskID 或无日志区，延迟关闭对话框并刷新实例列表
        setTimeout(function () { closeMwDeployModal(); loadMiddlewareInstances(); }, 2000);
      }
    } else {
      resultEl.className = 'msg err';
      resultEl.textContent = t('mwdep.deployFail') + ': [' + (r && r.s || '?') + '] ' + (r && r.j ? JSON.stringify(r.j) : '');
    }
  }).catch(function (e) {
    console.error('[mw-deploy]', e);
    if (resultEl) {
      resultEl.className = 'msg err';
      resultEl.textContent = t('mwdep.deployFail') + ': ' + (e && e.message || e);
    }
  });
}

// 加载已部署实例列表
// Phase 2 改造：在每行添加"卸载"按钮
export function loadMiddlewareInstances() {
  const listEl = document.getElementById('mwInstanceList');
  if (!listEl) return;
  listEl.innerHTML = '<p class="muted">' + esc(t('mwdep.loading')) + '</p>';
  api.getMiddlewareInstances().then(function (list) {
    const el = document.getElementById('mwInstanceList');
    if (!el) return;
    if (!list || list.length === 0) {
      el.innerHTML = '<p class="muted">' + esc(t('mwdep.noInstances')) + '</p>';
      return;
    }
    let html = '<div class="table-wrap"><table><colgroup>'
      + '<col style="width:16%"><col style="width:12%"><col style="width:16%"><col style="width:12%"><col style="width:12%"><col style="width:18%"><col style="width:14%">'
      + '</colgroup><thead><tr>'
      + '<th>' + esc(t('mwdep.instance.col.id')) + '</th>'
      + '<th>' + esc(t('mwdep.instance.col.template')) + '</th>'
      + '<th>' + esc(t('mwdep.instance.col.agent')) + '</th>'
      + '<th>' + esc(t('mwdep.instance.col.deployType')) + '</th>'
      + '<th>' + esc(t('mwdep.instance.col.status')) + '</th>'
      + '<th>' + esc(t('mwdep.instance.col.createdAt')) + '</th>'
      + '<th>' + esc(t('mwdep.instance.col.action')) + '</th>'
      + '</tr></thead><tbody>';
    list.forEach(function (ins) {
      const status = ins.status || '-';
      const statusBadge = status === 'running' || status === 'ok' || status === 'success'
        ? '<span class="badge" style="background:var(--green-soft,#e6f9ee);color:var(--green)">' + esc(status) + '</span>'
        : (status === 'failed' || status === 'error'
          ? '<span class="badge" style="background:var(--fail-soft,#fde8e8);color:var(--fail)">' + esc(status) + '</span>'
          : '<span class="badge" style="background:var(--bg-2);color:var(--text-2)">' + esc(status) + '</span>');
      // 卸载按钮：仅对运行中/成功的实例显示，避免对已卸载/失败的实例再次卸载
      const canUninstall = status === 'running' || status === 'ok' || status === 'success' || status === 'deployed' || status === 'installed';
      const insId = esc(ins.id || '');
      const agentID = esc(ins.agentID || ins.agentId || '');
      const deployType = esc(ins.deployType || ins.deploy_type || '');
      const actionHtml = canUninstall
        ? '<button class="btn btn-sm" style="color:var(--fail);border:1px solid var(--fail)" onclick="uninstallMiddlewareInstance(\'' + insId + '\',\'' + agentID + '\',\'' + deployType + '\')">' + icon('close', 12) + ' ' + esc(t('mwdep.uninstall')) + '</button>'
        : '<span class="muted">—</span>';
      html += '<tr>'
        + '<td><code title="' + insId + '">' + insId + '</code></td>'
        + '<td>' + esc(ins.templateID || ins.templateId || ins.template || '-') + '</td>'
        + '<td>' + agentID + '</td>'
        + '<td>' + mwdepDeployTypeBadge(ins.deployType || ins.deploy_type) + '</td>'
        + '<td>' + statusBadge + '</td>'
        + '<td>' + esc(fmtTime(ins.createdAt || ins.created_at || '')) + '</td>'
        + '<td>' + actionHtml + '</td>'
        + '</tr>';
    });
    html += '</tbody></table></div>';
    el.innerHTML = html;
  }).catch(function (e) {
    console.error('[mw-instances]', e);
    const el = document.getElementById('mwInstanceList');
    if (el) el.innerHTML = '<p class="muted">' + esc(t('mwdep.networkError')) + '</p>';
  });
}

// ---------- 中间件卸载（Phase 2） ----------
// 打开卸载对话框：传入实例 ID / agentID / deployType，弹出确认对话框
export function uninstallMiddlewareInstance(instanceID, agentID, deployType) {
  mwdepUninstallIns = { id: instanceID, agentID: agentID, deployType: deployType };
  const modal = document.getElementById('mwUninstallModal');
  if (!modal) {
    // 兜底：若 HTML 未提供卸载对话框，使用 confirm 直接卸载
    if (!confirm(t('mwdep.uninstallConfirm'))) return;
    doUninstallMiddleware();
    return;
  }
  // 重置结果区与日志区
  const resultEl = document.getElementById('mwUninstallResult');
  if (resultEl) { resultEl.innerHTML = ''; resultEl.className = ''; }
  const logEl = document.getElementById('mwUninstallLog');
  if (logEl) logEl.textContent = '';
  // 填充实例信息
  const idEl = document.getElementById('mwUninstallInsId');
  if (idEl) idEl.textContent = instanceID || '-';
  const agentEl = document.getElementById('mwUninstallAgentId');
  if (agentEl) agentEl.textContent = agentID || '-';
  const typeEl = document.getElementById('mwUninstallDeployType');
  if (typeEl) typeEl.textContent = deployType || '-';
  modal.classList.add('open');
}

// 关闭卸载对话框
export function closeMwUninstallModal() {
  const modal = document.getElementById('mwUninstallModal');
  if (modal) modal.classList.remove('open');
  if (mwdepUninstallTimer) { clearInterval(mwdepUninstallTimer); mwdepUninstallTimer = null; }
  mwdepUninstallIns = null;
}

// 执行卸载：调用 API 并轮询任务状态
function doUninstallMiddleware() {
  if (!mwdepUninstallIns || !mwdepUninstallIns.id) return;
  const resultEl = document.getElementById('mwUninstallResult');
  const logEl = document.getElementById('mwUninstallLog');
  if (resultEl) { resultEl.className = 'msg'; resultEl.textContent = t('mwdep.uninstalling'); }
  if (logEl) logEl.textContent = '';
  api.uninstallMiddleware(mwdepUninstallIns.id, mwdepUninstallIns.agentID, mwdepUninstallIns.deployType).then(function (r) {
    if (!resultEl) return;
    if (r && r.s && r.s < 400 && r.j) {
      const taskId = (r.j.taskID || r.j.id || r.j.taskId || '');
      resultEl.className = 'msg ok';
      resultEl.textContent = t('mwdep.uninstalling') + (taskId ? (' (task: ' + taskId + ')') : '');
      // 轮询卸载任务状态
      if (taskId && logEl) {
        logEl.textContent = t('osopt.polling') + '\n';
        if (mwdepUninstallTimer) clearInterval(mwdepUninstallTimer);
        mwdepUninstallTimer = pollTaskResult(taskId, logEl, 3000, function (status) {
          if (status === 'completed') {
            resultEl.className = 'msg ok';
            resultEl.textContent = t('mwdep.uninstallSuccess');
            loadMiddlewareInstances();
          } else if (status === 'failed') {
            resultEl.className = 'msg err';
            resultEl.textContent = t('mwdep.uninstallFailed') + ' (task: ' + taskId + ')';
          }
          mwdepUninstallTimer = null;
        });
      } else {
        // 无 taskID：延迟关闭并刷新
        setTimeout(function () { closeMwUninstallModal(); loadMiddlewareInstances(); }, 1500);
      }
    } else {
      resultEl.className = 'msg err';
      resultEl.textContent = t('mwdep.uninstallFailed') + ': [' + (r && r.s || '?') + '] ' + (r && r.j ? JSON.stringify(r.j) : '');
    }
  }).catch(function (e) {
    console.error('[mw-uninstall]', e);
    if (resultEl) {
      resultEl.className = 'msg err';
      resultEl.textContent = t('mwdep.uninstallFailed') + ': ' + (e && e.message || e);
    }
  });
}

// 确认卸载：从对话框触发
export function confirmMwUninstall() {
  doUninstallMiddleware();
}