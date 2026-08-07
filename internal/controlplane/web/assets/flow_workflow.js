// flow_workflow.js — 作业编排（M5 DAG 画布）
// 从 flow.js 拆分（P2-1）。职责：作业流 DAG 编辑器（节点/边/布局/缩放/平移/撤销重做/保存运行/调度）。
// 依赖：api.js、render.js（esc/escAttr）、icons.js、i18n.js。

import * as api from './api.js';
import { esc, escAttr } from './render.js';
import { icon } from './icons.js';
import { t } from './i18n.js';

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
export function undo() { if (!history.length) return; future.push(JSON.stringify({ dag: flow.dag, pos: nodePos, sel: selectedNode, edge: selectedEdge })); const s = JSON.parse(history.pop()); flow.dag = s.dag; nodePos = s.pos; selectedNode = s.sel; selectedEdge = s.edge; renderFlow(); flowMsg(t('flow.msg.undone'), true); }
export function redo() { if (!future.length) return; history.push(JSON.stringify({ dag: flow.dag, pos: nodePos, sel: selectedNode, edge: selectedEdge })); const s = JSON.parse(future.pop()); flow.dag = s.dag; nodePos = s.pos; selectedNode = s.sel; selectedEdge = s.edge; renderFlow(); flowMsg(t('flow.msg.redone'), true); }
export function flowMsg(s, ok) { const el = document.getElementById('wfMsg'); if (el) { el.className = 'msg ' + (ok ? 'ok' : 'err'); el.textContent = (ok ? '[ok] ' : '[err] ') + s; } }

export function loadFlows() {
  api.getAgents().then(function (a) {
    const sel = document.getElementById('wfAgent'); if (!sel) return; sel.innerHTML = '';
    (a || []).forEach(function (x) { const o = document.createElement('option'); o.value = x.agentID; o.textContent = x.agentID + ' (' + x.hostname + ')'; sel.appendChild(o); });
  }).catch(function (e) { console.error(e); });
  api.getWorkflows().then(function (list) {
    const sel = document.getElementById('wfSelect'); if (!sel) return; sel.innerHTML = '<option value="">' + esc(t('cmdb.newBlankWorkflow')) + '</option>';
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
  nodePos = {}; renderFlow(); flowMsg(t('flow.msg.newBlank'), true);
}

export function loadDemo() {
  flow = {
    id: 0, name: t('flow.demo.name'), agentID: document.getElementById('wfAgent').value, cron: '', dag: [
      { id: 'n1', name: t('flow.demo.step1'), type: 'shell', command: 'docker pull nginx:latest', path: '', dependsOn: [] },
      { id: 'n2', name: t('flow.demo.step2'), type: 'shell', command: 'docker stop nginx', path: '', dependsOn: ['n1'] },
      { id: 'n3', name: t('flow.demo.step3'), type: 'service', command: 'nginx', path: '', dependsOn: ['n2'] },
    ], status: 'draft',
  };
  document.getElementById('wfName').value = flow.name;
  document.getElementById('wfCron').value = '';
  nodePos = {}; autoLayout(); renderFlow(); flowMsg(t('flow.msg.demoLoaded'), true);
}

export function addNode() {
  let id = 'n' + (flow.dag.length + 1);
  while (flow.dag.some(function (n) { return n.id === id; })) { id = 'n' + Math.floor(Math.random() * 1000); }
  snapshot();
  flow.dag.push({ id: id, name: t('flow.editor.stepPrefix') + id, type: 'shell', command: '', path: '', dependsOn: [] });
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
  const b = document.getElementById('linkBtn'); if (b) b.innerHTML = linking ? icon('link', 16) + ' ' + esc(t('flow.link.linking')) : icon('link', 16) + ' ' + esc(t('flow.link.link'));
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
  if (srcId === depId) { flowMsg(t('flow.msg.depSelf'), false); return false; }
  const src = flow.dag.find(function (n) { return n.id === srcId; });
  if (!src) return false;
  if ((src.dependsOn || []).indexOf(depId) >= 0) { flowMsg(t('flow.msg.depExists'), false); return false; }
  if (createsCycle(srcId, depId)) { flowMsg(t('flow.msg.depCycle'), false); return false; }
  src.dependsOn = src.dependsOn || []; src.dependsOn.push(depId);
  flowMsg(t('flow.msg.depAdded', { src: srcId, dst: depId }), true); return true;
}
// 注意：原 flow.js 中 linkClick/selectEdge/deleteEdge 有重复定义，
// 后一组（硬编码中文）覆盖前一组（i18n 翻译键）。此处仅保留实际生效的后一组，
// 以保持与原代码完全一致的行为（Node.js 25 严格模式不允许重复声明）。
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
  const b = document.getElementById('linkBtn'); if (b) b.innerHTML = icon('link', 16) + ' ' + esc(t('flow.link.linking'));
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
  const b = document.getElementById('linkBtn'); if (b) b.innerHTML = icon('link', 16) + ' ' + esc(t('flow.link.link'));
  const p = document.getElementById('tab-flow'); if (p) p.classList.remove('linkmode');
  if (target && target !== fromId) { snapshot(); addDep(target, fromId); }
  renderFlow();
}

function renderNodeList() {
  const el = document.getElementById('nodeList'); if (!el) return;
  if (!flow.dag.length) { el.innerHTML = '<p class="muted">' + esc(t('flow.msg.noStepHint')) + '</p>'; return; }
  el.innerHTML = flow.dag.map(function (n) {
    return '<div class="ci' + (selectedNode === n.id ? ' sel' : '') + '" onclick="selectNode(\'' + escAttr(n.id) + '\')">' + esc(n.name || n.id) + ' <small class="muted">' + esc(n.type) + '</small>' + (selectedNode === n.id ? ' ✦' : '') + '</div>';
  }).join('');
}

function renderNodeEditor() {
  const el = document.getElementById('nodeEditor'); if (!el) return;
  if (!selectedNode) { el.innerHTML = ''; el.style.display = 'none'; return; }
  const n = flow.dag.find(function (x) { return x.id === selectedNode; }); if (!n) { el.innerHTML = ''; el.style.display = 'none'; return; }
  const others = flow.dag.filter(function (x) { return x.id !== n.id; }).map(function (x) {
    return '<option value="' + esc(x.id) + '"' + (((n.dependsOn || []).indexOf(x.id) >= 0) ? ' selected' : '') + '>' + esc(x.name || x.id) + '</option>';
  }).join('');
  el.innerHTML = '<h4>' + esc(t('flow.editor.heading', { id: n.id })) + '</h4>'
    + '<label>' + esc(t('flow.editor.name')) + '<input id="nName" value="' + esc(n.name) + '" size="16"></label><br>'
    + '<label>' + esc(t('flow.editor.type')) + '<select id="nType"><option value="shell"' + (n.type === 'shell' ? ' selected' : '') + '>' + esc(t('flow.editor.typeShell')) + '</option><option value="file"' + (n.type === 'file' ? ' selected' : '') + '>' + esc(t('flow.editor.typeFile')) + '</option><option value="service"' + (n.type === 'service' ? ' selected' : '') + '>' + esc(t('flow.editor.typeService')) + '</option></select></label><br>'
    + '<label>' + esc(t('flow.editor.command')) + '<input id="nCmd" value="' + esc(n.command) + '" size="28" title="' + esc(t('flow.editor.commandTitle')) + '"></label><br>'
    + '<label>' + esc(t('flow.editor.path')) + '<input id="nPath" value="' + esc(n.path) + '" size="18"></label><br>'
    + '<label>' + esc(t('flow.editor.deps')) + '<select id="nDeps" multiple size="3" title="' + esc(t('flow.editor.depsTitle')) + '">' + others + '</select></label><br>'
    + '<div class="btnbar"><button onclick="applyNode()">' + esc(t('flow.editor.apply')) + '</button> <button onclick="deleteNode(\'' + escAttr(n.id) + '\')">' + esc(t('flow.editor.deleteStep')) + '</button></div>';
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
  if (!name || !agent) { flowMsg(t('flow.msg.needNameAgent'), false); return; }
  const dagStr = JSON.stringify(flow.dag);
  if (flow.id) {
    api.updateWorkflow(flow.id, { name: name, dag: dagStr, cron: cron })
      .then(function (x) { if (x.s >= 400) { flowMsg('[' + x.s + '] ' + (x.j.error || ''), false); } else { flow.id = x.j.id; flow.status = x.j.status; flow.cron = x.j.cron || ''; flowMsg(t('flow.msg.saved', { id: x.j.id }), true); loadFlows(); } });
  } else {
    api.createWorkflow({ name: name, agentID: agent, dag: dagStr, cron: cron })
      .then(function (x) { if (x.s >= 400) { flowMsg('[' + x.s + '] ' + (x.j.error || ''), false); } else { flow.id = x.j.id; flow.status = x.j.status; flow.cron = x.j.cron || ''; flowMsg(t('flow.msg.created', { id: x.j.id }), true); loadFlows(); } });
  }
}

export function runWorkflow() {
  if (!flow.id) { flowMsg(t('flow.msg.needSaveFirst'), false); return; }
  api.runWorkflow(flow.id)
    .then(function (x) { if (x.s >= 400) { flowMsg('[' + x.s + '] ' + (x.j.error || ''), false); } else { flowMsg(t('flow.msg.runTriggered', { id: flow.id }), true); pollStatus(); } });
}

export function alignSel(axis) {
  if (!selectedNode) { flowMsg(t('flow.msg.needAlignBase'), false); return; }
  const ref = nodePos[selectedNode]; if (!ref) return;
  const others = flow.dag.filter(function (n) { return n.id !== selectedNode; }).map(function (n) { return nodePos[n.id]; }).filter(Boolean);
  snapshot();
  if (!others.length) {
    if (axis === 'left') ref.x = 0; else if (axis === 'right') ref.x = snap(170 - 170); else if (axis === 'top') ref.y = 0; else if (axis === 'bottom') ref.y = 0;
    renderFlow(); flowMsg(t('flow.msg.alignedToCanvas'), true); return;
  }
  if (axis === 'left') { const x = Math.min.apply(null, others.map(function (p) { return p.x; })); ref.x = snap(x); }
  else if (axis === 'right') { const x = Math.max.apply(null, others.map(function (p) { return p.x + 170; })); ref.x = snap(x - 170); }
  else if (axis === 'top') { const y = Math.min.apply(null, others.map(function (p) { return p.y; })); ref.y = snap(y); }
  else if (axis === 'bottom') { const y = Math.max.apply(null, others.map(function (p) { return p.y + 66; })); ref.y = snap(y - 66); }
  renderFlow(); flowMsg(t('flow.msg.aligned', { axis: t('flow.msg.axis' + (axis.charAt(0).toUpperCase() + axis.slice(1))) }), true);
}

function paintRunState() {
  const el = document.getElementById('flowLegend'); if (!el) return;
  if (!flow.id || !Object.keys(nodeStatus).length) { el.className = 'flowLegend'; el.innerHTML = ''; return; }
  const c = {}; Object.keys(nodeStatus).forEach(function (k) { c[nodeStatus[k]] = (c[nodeStatus[k]] || 0) + 1; });
  const col = { 'done': '#059669', 'running': '#6366f1', 'pending': '#d97706', 'blocked': '#64748b', 'failed': '#e11d48' };
  const label = { 'done': t('flow.status.done'), 'running': t('flow.status.running'), 'pending': t('flow.status.pending'), 'blocked': t('flow.status.blocked'), 'failed': t('flow.status.failed') };
  let html = '<span class="muted">' + esc(t('flow.runState.label')) + '</span>';
  Object.keys(c).forEach(function (k) { html += '<span class="pill"><span class="dot" style="background:' + (col[k] || '#64748b') + '"></span>' + esc(label[k] || k) + ' ' + c[k] + '</span>'; });
  html += '<button onclick="switchTab(\'ops\')" title="' + esc(t('flow.runState.viewInOpsTitle')) + '">' + icon('search', 14) + ' ' + esc(t('flow.runState.viewInOps')) + '</button>';
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
    flowMsg(t('flow.msg.runState', { state: JSON.stringify(cnt) }), true);
    setTimeout(function () { const p = document.getElementById('tab-flow'); if (p && p.classList.contains('active')) pollStatus(); }, 3000);
  }).catch(function (e) { console.error(e); });
}

export function scheduleWorkflowPrompt() {
  if (!flow.id) { flowMsg(t('flow.msg.needSaveFirst'), false); return; }
  const cron = document.getElementById('wfCron').value.trim();
  api.scheduleWorkflow(flow.id, cron)
    .then(function (x) { if (x.s >= 400) { flowMsg('[' + x.s + '] ' + (x.j.error || ''), false); } else { flow.cron = x.j.cron || ''; flow.status = x.j.status; flowMsg(t('flow.msg.scheduleSet', { cron: x.j.cron || t('flow.msg.cronNone') }), true); loadFlows(); } });
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