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
  ['home', 'ops', 'cmdb', 'osopt', 'deploy', 'flow', 'logs', 'alerts', 'users', 'roles', 'permission', 'audits', 'settings', 'docs'].forEach(function (t) {
    const p = document.getElementById('tab-' + t); if (p) p.classList.toggle('active', t === name);
    const b = document.getElementById('tab-' + t + '-btn'); if (b) b.classList.toggle('active', t === name);
  });
  if (name === 'ops') { pollTasks(); }
  if (name === 'cmdb') { loadCMDBTypes(); }
  if (name === 'osopt') { loadOSTemplates(); }
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

// 打开执行对话框：加载设备列表供选择
export function executeOSOptimize(id) {
  osoptExecId = id;
  const modal = document.getElementById('osExecModal');
  if (!modal) return;
  // 重置结果区
  const resultEl = document.getElementById('osExecResult');
  if (resultEl) { resultEl.innerHTML = ''; resultEl.className = ''; }
  // 清空参数
  const paramsEl = document.getElementById('osExecParams');
  if (paramsEl) paramsEl.value = '';
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

// 关闭执行对话框
export function closeOSExecModal() {
  const modal = document.getElementById('osExecModal');
  if (modal) modal.classList.remove('open');
  osoptExecId = '';
}

// 确认执行：调用 API 在选定设备上执行模板
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
  const paramsEl = document.getElementById('osExecParams');
  const paramsText = paramsEl ? paramsEl.value : '';
  const params = paramsText.split('\n').map(function (s) { return s.trim(); }).filter(function (s) { return s.length > 0; });

  if (resultEl) { resultEl.className = 'msg'; resultEl.textContent = t('osopt.loading'); }
  api.executeOSTemplate(osoptExecId, agentID, params).then(function (r) {
    if (!resultEl) return;
    if (r && r.s && r.s < 400 && r.j) {
      const taskId = (r.j.taskID || r.j.id || r.j.taskId || '');
      resultEl.className = 'msg ok';
      resultEl.textContent = t('osopt.execSuccess') + (taskId || JSON.stringify(r.j));
      // 执行成功后延迟关闭对话框
      setTimeout(closeOSExecModal, 2000);
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