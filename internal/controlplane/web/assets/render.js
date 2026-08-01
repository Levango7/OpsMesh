// render.js — 渲染逻辑
// 职责：表格渲染（设备表/任务表）、详情抽屉、状态徽章、空态、时间格式化、HTML 转义。
// 持有共享业务状态：lastDevices / lastTasks / lastAlerts（供 flow.js / poll.js 读写）。

import { getCMDBTypes, getCIs } from './api.js';

// ---------- 共享业务状态 ----------
export const state = {
  lastDevices: {},
  lastTasks: [],
  lastAlerts: [],
};

// ---------- XSS 转义 / 时间格式化 ----------
export function esc(s) {
  return (s == null ? '' : String(s)).replace(/[&<>"']/g, function (c) {
    return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
  });
}
// 时间格式化：能解析为 Date 则按 zh-CN 24h 制展示，否则原样转义
export function fmtTime(s) {
  if (!s) return '';
  const d = new Date(s);
  if (isNaN(d.getTime())) return esc(s);
  return d.toLocaleString('zh-CN', { hour12: false });
}

export function setText(id, v) {
  const e = document.getElementById(id);
  if (e) e.textContent = v;
}

// ---------- 运维中枢统计 / 总览 ----------
export function paintStats() {
  const devs = state.lastDevices || {};
  let total = 0, managed = 0;
  Object.keys(devs).forEach(function (seg) {
    (devs[seg] || []).forEach(function (d) {
      total++;
      if (d.state === 'managed' || d.agentID) managed++;
    });
  });
  const sd = document.getElementById('statDevices'); if (sd) sd.textContent = total;
  const sm = document.getElementById('statManaged'); if (sm) sm.textContent = managed;
  const st = document.getElementById('statTasks'); if (st) st.textContent = (state.lastTasks || []).length;
  const sa = document.getElementById('statAlerts'); if (sa) sa.textContent = (state.lastAlerts || []).length;
  paintOverview();
}

export function paintOverview() {
  const devs = state.lastDevices || {};
  let total = 0, managed = 0;
  Object.keys(devs).forEach(function (seg) {
    (devs[seg] || []).forEach(function (d) {
      total++;
      if (d.state === 'managed' || d.agentID) managed++;
    });
  });
  setText('ovDevices', total);
  const tasks = state.lastTasks || [];
  let td = 0, tf = 0;
  tasks.forEach(function (t) { if (t.state === 'done') td++; else if (t.state === 'failed') tf++; });
  setText('ovTasks', tasks.length);
  const alerts = state.lastAlerts || [];
  let ac = 0, aw = 0;
  alerts.forEach(function (a) { if (a.severity === 'critical') ac++; else if (a.severity === 'warning') aw++; });
  setText('ovAlerts', alerts.length);
  drawTaskBar('ovTaskChart', td, tf, tasks.length - td - tf);
  drawAlertDonut('ovAlertChart', ac, aw);
  paintTrend();
  paintTopo();
  // 配置项：拉各类型 CI 计数汇总
  getCMDBTypes().then(function (ts) {
    const list = (ts || []);
    if (list.length === 0) { setText('ovCIs', 0); return; }
    Promise.all(list.map(function (t) {
      return getCIs(t.type).then(function (arr) { return (arr || []).length; });
    })).then(function (ns) {
      const s = ns.reduce(function (a, b) { return a + b; }, 0);
      setText('ovCIs', s);
    }).catch(function (e) { console.error(e); });
  }).catch(function (e) { console.error(e); });
}

export function drawTaskBar(elId, done, failed, other) {
  const el = document.getElementById(elId); if (!el) return;
  const total = done + failed + other;
  if (total === 0) { el.innerHTML = '<p class="muted">暂无任务数据。</p>'; return; }
  const pct = function (n) { return (n / total * 100).toFixed(1); };
  el.innerHTML =
    '<div style="display:flex;height:22px;border-radius:6px;overflow:hidden;margin:6px 0 10px">'
    + '<div style="width:' + pct(done) + '%;background:var(--green)" title="成功 ' + done + '"></div>'
    + '<div style="width:' + pct(failed) + '%;background:var(--fail)" title="失败 ' + failed + '"></div>'
    + '<div style="width:' + pct(other) + '%;background:var(--border-2)" title="进行中/排队 ' + other + '"></div>'
    + '</div>'
    + '<div style="display:flex;gap:16px;font-size:12px;color:var(--text-2)">'
    + '<span><span class="dot ok" style="display:inline-block;margin-right:5px"></span>成功 ' + done + '</span>'
    + '<span><span class="dot fail" style="display:inline-block;margin-right:5px"></span>失败 ' + failed + '</span>'
    + '<span><span class="dot" style="display:inline-block;background:var(--border-2);margin-right:5px"></span>其余 ' + other + '</span>'
    + '</div>';
}

export function drawAlertDonut(elId, c, w) {
  const el = document.getElementById(elId); if (!el) return;
  const total = c + w;
  if (total === 0) { el.innerHTML = '<p class="muted">无活跃告警，一切正常 ✅</p>'; return; }
  const R = 42, C = 2 * Math.PI * R, critLen = c / total * C, warnLen = w / total * C;
  el.innerHTML =
    '<svg width="120" height="120" viewBox="0 0 120 120">'
    + '<circle cx="60" cy="60" r="' + R + '" fill="none" stroke="var(--fail)" stroke-width="14" stroke-dasharray="' + critLen + ' ' + (C - critLen) + '" stroke-dashoffset="0" transform="rotate(-90 60 60)"></circle>'
    + '<circle cx="60" cy="60" r="' + R + '" fill="none" stroke="var(--warn)" stroke-width="14" stroke-dasharray="' + warnLen + ' ' + C + '" stroke-dashoffset="' + (-critLen) + '" transform="rotate(-90 60 60)"></circle>'
    + '<text x="60" y="58" text-anchor="middle" font-size="20" font-weight="600" fill="var(--text)">' + total + '</text>'
    + '<text x="60" y="74" text-anchor="middle" font-size="11" fill="var(--text-3)">活跃告警</text>'
    + '</svg>'
    + '<div style="font-size:12px;color:var(--text-2);margin-top:6px"><span style="color:var(--fail);font-weight:600">严重 ' + c + '</span> ｜ <span style="color:var(--warn);font-weight:600">警告 ' + w + '</span></div>';
}

// ---------- 设备 / 任务 / 告警 表渲染 ----------
// 注：setFocus / openDevice 由 flow.js 注入（避免循环依赖），通过 renderDeps 传入。
let renderDeps = { setFocus: null, openDevice: null, focusDevice: null, applyFocus: null };

export function setRenderDeps(deps) {
  renderDeps = Object.assign(renderDeps, deps);
}

export function renderDevices(snap) {
  state.lastDevices = snap || {};
  let html = '';
  const keys = Object.keys(snap || {});
  if (keys.length === 0) {
    document.getElementById('devices').innerHTML = '<p class="muted">暂无纳管设备。设备接入网段后会被自动发现并纳管。</p>';
    paintStats();
    return;
  }
  keys.forEach(function (seg) {
    const devs = snap[seg];
    html += '<h3>网段 ' + esc(seg) + '（' + devs.length + ' 台设备）</h3>';
    html += '<table><tr><th>DeviceID</th><th>IP</th><th>采集端</th><th>状态</th><th>任务态</th><th>LastResult</th></tr>';
    devs.forEach(function (d) {
      const rowCls = 'device' + (d.lastResult === 'failed' ? ' fail' : '');
      let badge = '';
      if (d.lastResult === 'failed') { badge = '<span class="badge fail">failed</span>'; }
      else if (d.lastResult === 'success') { badge = '<span class="badge ok">ok</span>'; }
      html += '<tr class="' + rowCls + '" onclick="setFocus(\'' + esc(d.deviceID) + '\',\'' + esc(d.ip) + '\',\'' + esc(d.agentID) + '\',\'' + esc(seg) + '\');openDevice(\'' + esc(d.deviceID) + '\')"><td><code>' + esc(d.deviceID) + '</code></td><td>' + esc(d.ip) + '</td><td>' + esc(d.agentID) + '</td><td>' + esc(d.state) + '</td><td>' + esc(d.taskState) + '</td><td>' + badge + '</td></tr>';
    });
    html += '</table>';
  });
  document.getElementById('devices').innerHTML = html;
  paintStats();
}

export function renderAlerts(list) {
  state.lastAlerts = list || [];
  if (!list || list.length === 0) {
    document.getElementById('alerts').innerHTML = '<p class="muted">暂无告警，一切正常 ✅</p>';
    paintStats();
    return;
  }
  const fl = renderDeps.applyFocus(state.lastAlerts, 'alert');
  const focusDevice = renderDeps.focusDevice();
  const note = focusDevice ? '<p class="hint">🔗 已按设备 <code>' + esc(focusDevice.id) + '</code> 过滤（' + fl.length + ' 条）</p>' : '';
  if (fl.length === 0) {
    document.getElementById('alerts').innerHTML = note + '<p class="muted">该设备暂无告警。</p>';
    paintStats();
    return;
  }
  let html = note;
  fl.forEach(function (a) {
    const cls = a.severity === 'critical' ? 'alert' : 'alert warn';
    html += '<div class="' + cls + '"><b>[' + esc(a.severity) + ']</b> 设备 ' + esc(a.deviceID) + '<br>' + esc(a.message) + '<br><small class="muted">' + fmtTime(a.createdAt) + '</small>'
      + '<br><button class="jbtn" style="margin-top:6px" onclick="setFocus(\'' + esc(a.deviceID) + '\',\'\',\'\',\'\');switchTab(\'alerts\')">🔗 上下文串联</button></div>';
  });
  document.getElementById('alerts').innerHTML = html;
  paintStats();
}

export function renderTasks(tasks) {
  state.lastTasks = tasks || [];
  if (!tasks || tasks.length === 0) {
    document.getElementById('tasks').innerHTML = '<p class="muted">暂无任务。在上方「下发任务」创建一条吧。</p>';
    paintStats();
    return;
  }
  const list = renderDeps.applyFocus(state.lastTasks, 'task');
  const focusDevice = renderDeps.focusDevice();
  const note = focusDevice ? '<p class="hint">🔗 已按设备 <code>' + esc(focusDevice.id) + '</code> 过滤（' + list.length + ' 条）</p>' : '';
  if (list.length === 0) {
    document.getElementById('tasks').innerHTML = note + '<p class="muted">该设备暂无任务。</p>';
    paintStats();
    return;
  }
  let html = note + '<table><tr><th>TaskID</th><th>采集端</th><th>类型</th><th>命令</th><th>状态</th></tr>';
  list.forEach(function (t) {
    html += '<tr><td><code>' + esc(t.taskID) + '</code></td><td>' + esc(t.agentID) + '</td><td>' + esc(t.type) + '</td><td><code>' + esc(t.command) + '</code></td><td>' + esc(t.status) + '</td></tr>';
  });
  html += '</table>';
  document.getElementById('tasks').innerHTML = html;
  paintStats();
}

// ---------- 部署状态 / 日志级别 徽章 ----------
export function dpStatusPill(st) {
  const cls = { 'created': 'info', 'running': 'warn', 'success': 'ok', 'failed': 'fail', 'rolledback': 'warn' }[st] || 'info';
  return '<span class="pill ' + cls + '">' + esc(st) + '</span>';
}

export function logLevelPill(lv) {
  const cls = { 'error': 'fail', 'warn': 'warn', 'info': 'info' }[lv] || 'info';
  return '<span class="pill ' + cls + '">' + esc(lv) + '</span>';
}

// ---------- 今日成功率趋势线（F2） ----------
export function paintTrend() {
  const el = document.getElementById('ovTrend'); if (!el) return;
  const tasks = state.lastTasks || [];
  const now = new Date();
  const buckets = {};
  for (let h = 0; h < 24; h++) buckets[h] = { done: 0, fail: 0 };
  let has = false;
  tasks.forEach(function (t) {
    if (t.status !== 'done' && t.status !== 'failed') return;
    const d = new Date(t.createdAt); if (isNaN(d.getTime())) return;
    if (d.getFullYear() !== now.getFullYear() || d.getMonth() !== now.getMonth() || d.getDate() !== now.getDate()) return;
    const h = d.getHours(); buckets[h][t.status === 'done' ? 'done' : 'fail']++; has = true;
  });
  if (!has) { el.innerHTML = '<p class="muted">今日暂无终态任务。下发的任务完成后会在此显示成功率趋势。</p>'; return; }
  const W = 720, H = 180, padL = 34, padR = 14, padT = 14, padB = 24;
  const plotW = W - padL - padR, plotH = H - padT - padB;
  const hrs = []; for (let h = 0; h < 24; h++) { if (buckets[h].done + buckets[h].fail > 0) hrs.push(h); }
  const n = hrs.length;
  const x = function (i) { return padL + (n === 1 ? plotW / 2 : (plotW * i / (n - 1))); };
  let maxCnt = 1; hrs.forEach(function (h) { const c = buckets[h].done + buckets[h].fail; if (c > maxCnt) maxCnt = c; });
  const yRate = function (rate) { return padT + plotH * (1 - rate / 100); };
  let bars = '';
  hrs.forEach(function (h, i) { const c = buckets[h].done + buckets[h].fail; const bx = x(i) - 9, bh = plotH * (c / maxCnt); bars += '<rect x="' + bx + '" y="' + (padT + plotH - bh) + '" width="18" height="' + bh + '" rx="3" fill="rgba(13,148,136,.18)"></rect>'; });
  let area = '', line = '', dots = '', labels = '';
  hrs.forEach(function (h, i) {
    const c = buckets[h].done + buckets[h].fail; const rate = c ? (buckets[h].done / c * 100) : 0;
    const px = x(i), py = yRate(rate);
    if (i === 0) area += 'M' + px + ' ' + (padT + plotH);
    area += ' L' + px + ' ' + py;
    line += (i === 0 ? 'M' : 'L') + px + ' ' + py + ' ';
    dots += '<circle class="trend-pt" cx="' + px + '" cy="' + py + '" r="4" title="' + h + '时：成功 ' + buckets[h].done + ' / 失败 ' + buckets[h].fail + '（成功率 ' + rate.toFixed(0) + '%）"></circle>';
    labels += '<text x="' + px + '" y="' + (H - 7) + '" text-anchor="middle" font-size="10" fill="var(--text-3)">' + h + 'h</text>';
  });
  area += ' L' + x(n - 1) + ' ' + (padT + plotH) + ' Z';
  el.innerHTML = '<svg viewBox="0 0 ' + W + ' ' + H + '" width="100%" style="display:block">'
    + '<line x1="' + padL + '" y1="' + (padT + plotH) + '" x2="' + (W - padR) + '" y2="' + (padT + plotH) + '" stroke="var(--border-2)" stroke-width="1"/>'
    + bars + '<path class="trend-area" d="' + area + '"/>' + '<path class="trend-line" d="' + line + '"/>' + dots + labels
    + '</svg>';
}

// ---------- 网段拓扑（F2） ----------
const SEG_META = [
  { cidr: '10.20.0.0/24', name: 'mgmt-net（管理网）', color: 'var(--indigo)' },
  { cidr: '10.21.0.0/24', name: 'data-net（数据网）', color: 'var(--teal)' },
  { cidr: '10.22.0.0/24', name: 'soc-net（安全网）', color: 'var(--amber)' },
  { cidr: '10.30.0.0/16', name: 'seg-net（业务网）', color: 'var(--rose)' },
];

function ipToInt(ip) {
  const p = (ip || '').split('.');
  if (p.length !== 4) return -1;
  return ((+p[0]) << 24) + ((+p[1]) << 16) + ((+p[2]) << 8) + (+p[3]);
}
function cidrMatch(ip, cidr) {
  const m = cidr.split('/');
  if (m.length !== 2) return false;
  const base = ipToInt(m[0]), addr = ipToInt(ip), pre = +m[1];
  if (base < 0 || addr < 0) return false;
  const mask = pre === 0 ? 0 : (0xFFFFFFFF << (32 - pre));
  return (addr & mask) === (base & mask);
}

export function paintTopo() {
  const el = document.getElementById('ovTopo'); if (!el) return;
  const devs = state.lastDevices || {};
  const counts = {};
  SEG_META.forEach(function (m) { counts[m.cidr] = 0; });
  Object.keys(devs).forEach(function (seg) {
    (devs[seg] || []).forEach(function (d) {
      SEG_META.forEach(function (m) { if (cidrMatch(d.ip, m.cidr)) counts[m.cidr]++; });
    });
  });
  const segs = SEG_META.map(function (m) { return { name: m.name, color: m.color, count: counts[m.cidr] || 0 }; });
  if (segs.length === 0) { el.innerHTML = '<p class="muted">暂无网段设备。</p>'; return; }
  const W = 720, H = Math.max(170, 30 + segs.length * 46), padL = 20, padT = 20;
  const cpX = 30, cpY = H / 2, cpW = 150, cpH = 58;
  let svg = '<svg viewBox="0 0 ' + W + ' ' + H + '" width="100%" style="display:block">';
  svg += '<rect x="' + cpX + '" y="' + (cpY - cpH / 2) + '" width="' + cpW + '" height="' + cpH + '" rx="12" fill="var(--surface-2)" stroke="var(--accent)" stroke-width="2"/>';
  svg += '<text x="' + (cpX + cpW / 2) + '" y="' + (cpY - 4) + '" text-anchor="middle" font-size="13" font-weight="600" fill="var(--text)">控制面</text>';
  svg += '<text x="' + (cpX + cpW / 2) + '" y="' + (cpY + 14) + '" text-anchor="middle" font-size="11" fill="var(--text-3)">OpsMesh</text>';
  const nx = cpX + cpW + 120, nw = W - nx - 20, nh = 40;
  segs.forEach(function (s, i) {
    const ny = padT + i * 46;
    svg += '<line class="topo-edge" x1="' + (cpX + cpW) + '" y1="' + cpY + '" x2="' + nx + '" y2="' + (ny + nh / 2) + '"/>';
    svg += '<rect x="' + nx + '" y="' + ny + '" width="' + nw + '" height="' + nh + '" rx="10" fill="var(--surface-2)" stroke="' + s.color + '" stroke-width="2"/>';
    svg += '<circle cx="' + (nx + 20) + '" cy="' + (ny + nh / 2) + '" r="7" fill="' + s.color + '"/>';
    svg += '<text class="topo-label" x="' + (nx + 38) + '" y="' + (ny + nh / 2 - 3) + '">' + esc(s.name) + '</text>';
    svg += '<text class="topo-count" x="' + (nx + 38) + '" y="' + (ny + nh / 2 + 15) + '" fill="' + s.color + '">' + s.count + ' 台设备</text>';
  });
  svg += '</svg>';
  el.innerHTML = svg;
}