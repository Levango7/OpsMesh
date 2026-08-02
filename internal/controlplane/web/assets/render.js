// render.js — 渲染逻辑
// 职责：表格渲染（设备表/任务表）、详情抽屉、状态徽章、空态、时间格式化、HTML 转义。
// 持有共享业务状态：lastDevices / lastTasks / lastAlerts（供 flow.js / poll.js 读写）。
// 新增：用户中心 / 角色管理 / 权限管理 渲染、弹窗、分页。

import { getCMDBTypes, getCIs } from './api.js';
import { icon } from './icons.js';
import { t, getLang } from './i18n.js';

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
  if (total === 0) { el.innerHTML = '<p class="muted">' + esc(t('render.noTaskData')) + '</p>'; return; }
  const pct = function (n) { return (n / total * 100).toFixed(1); };
  el.innerHTML =
    '<div style="display:flex;height:22px;border-radius:6px;overflow:hidden;margin:6px 0 10px">'
    + '<div style="width:' + pct(done) + '%;background:var(--green)" title="成功 ' + done + '"></div>'
    + '<div style="width:' + pct(failed) + '%;background:var(--fail)" title="失败 ' + failed + '"></div>'
    + '<div style="width:' + pct(other) + '%;background:var(--border-2)" title="进行中/排队 ' + other + '"></div>'
    + '</div>'
    + '<div style="display:flex;gap:16px;font-size:12px;color:var(--text-2)">'
    + '<span><span class="dot ok" style="display:inline-block;margin-right:5px"></span>' + (getLang() === 'zh' ? '成功 ' : 'Success ') + done + '</span>'
    + '<span><span class="dot fail" style="display:inline-block;margin-right:5px"></span>' + (getLang() === 'zh' ? '失败 ' : 'Failed ') + failed + '</span>'
    + '<span><span class="dot" style="display:inline-block;background:var(--border-2);margin-right:5px"></span>' + (getLang() === 'zh' ? '其余 ' : 'Other ') + other + '</span>'
    + '</div>';
}

export function drawAlertDonut(elId, c, w) {
  const el = document.getElementById(elId); if (!el) return;
  const total = c + w;
  if (total === 0) { el.innerHTML = '<p class="muted">' + esc(t('render.noAlerts')) + '</p>'; return; }
  const R = 42, C = 2 * Math.PI * R, critLen = c / total * C, warnLen = w / total * C;
  const activeLbl = getLang() === 'zh' ? '活跃告警' : 'Active';
  const critLbl = getLang() === 'zh' ? '严重 ' : 'Critical ';
  const warnLbl = getLang() === 'zh' ? '警告 ' : 'Warning ';
  el.innerHTML =
    '<svg width="120" height="120" viewBox="0 0 120 120">'
    + '<circle cx="60" cy="60" r="' + R + '" fill="none" stroke="var(--fail)" stroke-width="14" stroke-dasharray="' + critLen + ' ' + (C - critLen) + '" stroke-dashoffset="0" transform="rotate(-90 60 60)"></circle>'
    + '<circle cx="60" cy="60" r="' + R + '" fill="none" stroke="var(--warn)" stroke-width="14" stroke-dasharray="' + warnLen + ' ' + C + '" stroke-dashoffset="' + (-critLen) + '" transform="rotate(-90 60 60)"></circle>'
    + '<text x="60" y="58" text-anchor="middle" font-size="20" font-weight="600" fill="var(--text)">' + total + '</text>'
    + '<text x="60" y="74" text-anchor="middle" font-size="11" fill="var(--text-3)">' + activeLbl + '</text>'
    + '</svg>'
    + '<div style="font-size:12px;color:var(--text-2);margin-top:6px"><span style="color:var(--fail);font-weight:600">' + critLbl + c + '</span> ｜ <span style="color:var(--warn);font-weight:600">' + warnLbl + w + '</span></div>';
}

// ---------- 设备 / 任务 / 告警 表渲染 ----------
// 注：setFocus / openDevice 由 flow.js 注入（避免循环依赖），通过 renderDeps 传入。
let renderDeps = { setFocus: null, openDevice: null, focusDevice: null, applyFocus: null };

export function setRenderDeps(deps) {
  renderDeps = Object.assign(renderDeps, deps);
}

export function renderDevices(snap) {
  state.lastDevices = snap || {};
  const el = document.getElementById('devices');
  if (!el) { paintStats(); return; }
  const keys = Object.keys(snap || {});
  if (keys.length === 0) {
    el.innerHTML = '<p class="muted">' + esc(t('render.noDevices')) + '</p>';
    paintStats();
    return;
  }
  let html = '';
  keys.forEach(function (seg) {
    const devs = snap[seg] || [];
    html += '<h3>' + esc(t('render.segment')) + ' ' + esc(seg) + '（' + devs.length + ' ' + esc(t('render.devicesUnit')) + '）</h3>';
    html += '<div class="table-wrap"><table><colgroup><col style="width:26%"><col style="width:14%"><col style="width:14%"><col style="width:12%"><col style="width:14%"><col style="width:20%"></colgroup><thead><tr><th>主机名</th><th>网段</th><th>IP</th><th>状态</th><th>OS</th><th>操作</th></tr></thead><tbody>';
    devs.forEach(function (d) {
      const rowCls = 'device' + (d.lastResult === 'failed' ? ' fail' : '');
      // 状态徽章：managed/online=绿、discovered=蓝、offline/lost=红、其余=灰
      const st = d.state || '';
      let stateBadge;
      if (st === 'managed' || st === 'online') {
        stateBadge = '<span class="pill ok">' + esc(st) + '</span>';
      } else if (st === 'discovered') {
        stateBadge = '<span class="pill info">' + esc(st) + '</span>';
      } else if (st === 'offline' || st === 'lost') {
        stateBadge = '<span class="pill fail">' + esc(st) + '</span>';
      } else {
        stateBadge = '<span class="pill">' + esc(st || '–') + '</span>';
      }
      const hostname = d.hostname || d.deviceID || '–';
      const osInfo = d.os ? esc(d.os) + (d.arch ? ' / ' + esc(d.arch) : '') : '–';
      // 行点击：设置上下文 + 打开设备详情抽屉（保留原有联动）
      const rowClick = "setFocus('" + esc(d.deviceID) + "','" + esc(d.ip) + "','" + esc(d.agentID) + "','" + esc(seg) + "');openDevice('" + esc(d.deviceID) + "')";
      html += '<tr class="' + rowCls + '" onclick="' + rowClick + '">'
        + '<td class="cell-stack"><b>' + esc(hostname) + '</b><code title="' + esc(d.deviceID) + '">' + esc(d.deviceID) + '</code></td>'
        + '<td>' + esc(seg) + '</td>'
        + '<td>' + esc(d.ip || '–') + '</td>'
        + '<td>' + stateBadge + '</td>'
        + '<td>' + osInfo + '</td>'
        + '<td class="row-actions-cell" onclick="event.stopPropagation()">'
        + '<button class="ghost" title="查看监控指标详情" onclick="showDeviceDetail(\'' + esc(d.deviceID) + '\')">' + icon('device', 14) + ' 详情</button> '
        + '<button class="ghost" title="下发任务到该设备" onclick="setFocus(\'' + esc(d.deviceID) + '\',\'' + esc(d.ip) + '\',\'' + esc(d.agentID) + '\',\'' + esc(seg) + '\');switchTab(\'ops\')">' + icon('task', 14) + ' 下发</button>'
        + '</td>'
        + '</tr>';
    });
    html += '</tbody></table></div>';
  });
  el.innerHTML = html;
  paintStats();
}

export function renderAlerts(list) {
  state.lastAlerts = list || [];
  if (!list || list.length === 0) {
    document.getElementById('alerts').innerHTML = '<p class="muted">' + esc(t('render.noAlerts')) + '</p>';
    paintStats();
    return;
  }
  const fl = renderDeps.applyFocus(state.lastAlerts, 'alert');
  const focusDevice = renderDeps.focusDevice();
  const note = focusDevice ? '<p class="hint">' + icon('context', 14) + ' ' + esc(t('render.linkFiltered')) + ' <code>' + esc(focusDevice.id) + '</code> ' + esc(t('render.filtered')) + '（' + fl.length + '）</p>' : '';
  if (fl.length === 0) {
    document.getElementById('alerts').innerHTML = note + '<p class="muted">' + esc(t('render.noAlertsForDev')) + '</p>';
    paintStats();
    return;
  }
  let html = note;
  fl.forEach(function (a) {
    const cls = a.severity === 'critical' ? 'alert' : 'alert warn';
    html += '<div class="' + cls + '"><b>[' + esc(a.severity) + ']</b> ' + (getLang() === 'zh' ? '设备' : 'Device') + ' ' + esc(a.deviceID) + '<br>' + esc(a.message) + '<br><small class="muted">' + fmtTime(a.createdAt) + '</small>'
      + '<br><button class="jbtn" style="margin-top:6px" onclick="setFocus(\'' + esc(a.deviceID) + '\',\'\',\'\',\'\');switchTab(\'alerts\')">' + icon('context', 14) + ' ' + esc(t('render.contextLink')) + '</button></div>';
  });
  document.getElementById('alerts').innerHTML = html;
  paintStats();
}

export function renderTasks(tasks) {
  state.lastTasks = tasks || [];
  if (!tasks || tasks.length === 0) {
    document.getElementById('tasks').innerHTML = '<p class="muted">' + esc(t('render.noTasks')) + '</p>';
    paintStats();
    return;
  }
  const list = renderDeps.applyFocus(state.lastTasks, 'task');
  const focusDevice = renderDeps.focusDevice();
  const note = focusDevice ? '<p class="hint">' + icon('context', 14) + ' ' + esc(t('render.linkFiltered')) + ' <code>' + esc(focusDevice.id) + '</code> ' + esc(t('render.filtered')) + '（' + list.length + '）</p>' : '';
  if (list.length === 0) {
    document.getElementById('tasks').innerHTML = note + '<p class="muted">' + esc(t('render.noTasksForDev')) + '</p>';
    paintStats();
    return;
  }
  let html = note + '<div class="table-wrap"><table><colgroup><col style="width:18%"><col style="width:18%"><col style="width:10%"><col style="width:40%"><col style="width:14%"></colgroup><thead><tr><th>TaskID</th><th>采集端</th><th>类型</th><th>命令</th><th>状态</th></tr></thead><tbody>';
  list.forEach(function (t) {
    html += '<tr><td><code title="' + esc(t.taskID) + '">' + esc(t.taskID) + '</code></td><td>' + esc(t.agentID) + '</td><td>' + esc(t.type) + '</td><td><code title="' + esc(t.command) + '">' + esc(t.command) + '</code></td><td>' + esc(t.status) + '</td></tr>';
  });
  html += '</tbody></table></div>';
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
  if (!has) { el.innerHTML = '<p class="muted">' + esc(t('render.noTrend')) + '</p>'; return; }
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
  if (segs.length === 0) { el.innerHTML = '<p class="muted">' + esc(t('render.noTopo')) + '</p>'; return; }
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
    svg += '<text class="topo-count" x="' + (nx + 38) + '" y="' + (ny + nh / 2 + 15) + '" fill="' + s.color + '">' + s.count + (getLang() === 'zh' ? ' 台设备' : ' devices') + '</text>';
  });
  svg += '</svg>';
  el.innerHTML = svg;
}

// ============================================================
// 用户中心 / 角色管理 / 权限管理 渲染（新增）
// ============================================================

// ---------- 分页状态 ----------
export const mgmtState = {
  users: { list: [], page: 1, pageSize: 10, search: '' },
  roles: { list: [], page: 1, pageSize: 10 },
  perms: { list: [] },
  // 缓存角色/权限，供用户编辑弹窗使用
  rolesCache: [],
  permsCache: [],
};

// ---------- 通用：弹窗 ----------
export function showModal(title, bodyHtml, footerHtml) {
  closeModal(); // 先清理已有弹窗
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay';
  overlay.id = 'modalOverlay';
  overlay.innerHTML =
    '<div class="modal" role="dialog" aria-modal="true">'
    + '<div class="modal-header"><h3>' + esc(title) + '</h3>'
    + '<button class="close-btn" onclick="closeModal()" aria-label="close">' + icon('close', 18) + '</button></div>'
    + '<div class="modal-body">' + bodyHtml + '</div>'
    + (footerHtml ? '<div class="modal-footer">' + footerHtml + '</div>' : '')
    + '</div>';
  // 点击遮罩关闭
  overlay.addEventListener('click', function (e) { if (e.target === overlay) closeModal(); });
  document.body.appendChild(overlay);
}

export function closeModal() {
  const o = document.getElementById('modalOverlay');
  if (o) o.remove();
}

// ---------- 用户管理渲染 ----------
export function renderUsers(users) {
  mgmtState.users.list = users || [];
  paintUsersTable();
}

export function paintUsersTable() {
  const el = document.getElementById('usersTable');
  if (!el) return;
  const s = mgmtState.users;
  // 搜索过滤
  let list = s.list;
  if (s.search) {
    const kw = s.search.toLowerCase();
    list = list.filter(function (u) {
      return (u.username || '').toLowerCase().indexOf(kw) >= 0
        || (u.email || '').toLowerCase().indexOf(kw) >= 0;
    });
  }
  const total = list.length;
  const totalPages = Math.max(1, Math.ceil(total / s.pageSize));
  if (s.page > totalPages) s.page = totalPages;
  const start = (s.page - 1) * s.pageSize;
  const pageList = list.slice(start, start + s.pageSize);

  if (total === 0) {
    el.innerHTML = '<p class="muted">' + esc(t('users.empty')) + '</p>';
    return;
  }

  let html = '<div class="table-wrap"><table><colgroup><col style="width:16%"><col style="width:22%"><col style="width:22%"><col style="width:10%"><col style="width:16%"><col style="width:14%"></colgroup><thead><tr>'
    + '<th>' + esc(t('users.col.username')) + '</th>'
    + '<th>' + esc(t('users.col.email')) + '</th>'
    + '<th>' + esc(t('users.col.roles')) + '</th>'
    + '<th>' + esc(t('users.col.status')) + '</th>'
    + '<th>' + esc(t('users.col.createdAt')) + '</th>'
    + '<th>' + esc(t('users.col.actions')) + '</th>'
    + '</tr></thead><tbody>';
  pageList.forEach(function (u) {
    const roleBadges = (u.role_ids || []).map(function (rid) {
      const r = mgmtState.rolesCache.find(function (x) { return String(x.id) === String(rid); });
      const name = r ? r.name : rid;
      return '<span class="pill info">' + esc(name) + '</span>';
    }).join(' ');
    const statusPill = u.status === 'disabled'
      ? '<span class="pill fail">' + esc(t('users.status.disabled')) + '</span>'
      : '<span class="pill ok">' + esc(t('users.status.active')) + '</span>';
    html += '<tr>'
      + '<td><b>' + esc(u.username) + '</b></td>'
      + '<td>' + esc(u.email || '–') + '</td>'
      + '<td><div class="role-badges">' + (roleBadges || '<span class="muted">–</span>') + '</div></td>'
      + '<td>' + statusPill + '</td>'
      + '<td>' + fmtTime(u.created_at) + '</td>'
      + '<td><div class="row-actions">'
      + '<button class="ghost" onclick="editUser(\'' + esc(u.id) + '\')">' + icon('edit', 14) + esc(t('common.edit')) + '</button>'
      + '<button class="ghost" onclick="deleteUser(\'' + esc(u.id) + '\')">' + icon('delete', 14) + esc(t('common.delete')) + '</button>'
      + '</div></td>'
      + '</tr>';
  });
  html += '</tbody></table></div>';
  // 分页
  html += '<div class="mgmt-pagination">'
    + '<span>' + esc(t('common.total', { n: total })) + '</span>'
    + '<div class="pager">'
    + '<button onclick="usersPrevPage()" ' + (s.page <= 1 ? 'disabled' : '') + '>' + icon('prev', 14) + esc(t('common.prev')) + '</button>'
    + '<span>' + esc(t('common.page', { cur: s.page, total: totalPages })) + '</span>'
    + '<button onclick="usersNextPage()" ' + (s.page >= totalPages ? 'disabled' : '') + '>' + esc(t('common.next')) + icon('next', 14) + '</button>'
    + '</div></div>';
  el.innerHTML = html;
}

export function usersPrevPage() {
  if (mgmtState.users.page > 1) { mgmtState.users.page--; paintUsersTable(); }
}
export function usersNextPage() {
  const s = mgmtState.users;
  const totalPages = Math.max(1, Math.ceil(s.list.length / s.pageSize));
  if (s.page < totalPages) { s.page++; paintUsersTable(); }
}
export function usersSetSearch(kw) {
  mgmtState.users.search = kw || '';
  mgmtState.users.page = 1;
  paintUsersTable();
}

// ---------- 角色管理渲染 ----------
export function renderRoles(roles) {
  mgmtState.roles.list = roles || [];
  mgmtState.rolesCache = roles || [];
  paintRolesTable();
}

export function paintRolesTable() {
  const el = document.getElementById('rolesTable');
  if (!el) return;
  const s = mgmtState.roles;
  const list = s.list;
  const total = list.length;
  const totalPages = Math.max(1, Math.ceil(total / s.pageSize));
  if (s.page > totalPages) s.page = totalPages;
  const start = (s.page - 1) * s.pageSize;
  const pageList = list.slice(start, start + s.pageSize);

  if (total === 0) {
    el.innerHTML = '<p class="muted">' + esc(t('roles.empty')) + '</p>';
    return;
  }

  let html = '<div class="table-wrap"><table><colgroup><col style="width:18%"><col style="width:38%"><col style="width:12%"><col style="width:18%"><col style="width:14%"></colgroup><thead><tr>'
    + '<th>' + esc(t('roles.col.name')) + '</th>'
    + '<th>' + esc(t('roles.col.description')) + '</th>'
    + '<th>' + esc(t('roles.col.permCount')) + '</th>'
    + '<th>' + esc(t('roles.col.createdAt')) + '</th>'
    + '<th>' + esc(t('roles.col.actions')) + '</th>'
    + '</tr></thead><tbody>';
  pageList.forEach(function (r) {
    const permCount = (r.permissions || []).length;
    html += '<tr>'
      + '<td><b>' + esc(r.name) + '</b></td>'
      + '<td class="wrap">' + esc(r.description || '–') + '</td>'
      + '<td>' + permCount + '</td>'
      + '<td>' + fmtTime(r.created_at) + '</td>'
      + '<td><div class="row-actions">'
      + '<button class="ghost" onclick="editRole(\'' + esc(r.id) + '\')">' + icon('edit', 14) + esc(t('common.edit')) + '</button>'
      + '<button class="ghost" onclick="deleteRole(\'' + esc(r.id) + '\')">' + icon('delete', 14) + esc(t('common.delete')) + '</button>'
      + '</div></td>'
      + '</tr>';
  });
  html += '</tbody></table></div>';
  html += '<div class="mgmt-pagination">'
    + '<span>' + esc(t('common.total', { n: total })) + '</span>'
    + '<div class="pager">'
    + '<button onclick="rolesPrevPage()" ' + (s.page <= 1 ? 'disabled' : '') + '>' + icon('prev', 14) + esc(t('common.prev')) + '</button>'
    + '<span>' + esc(t('common.page', { cur: s.page, total: totalPages })) + '</span>'
    + '<button onclick="rolesNextPage()" ' + (s.page >= totalPages ? 'disabled' : '') + '>' + esc(t('common.next')) + icon('next', 14) + '</button>'
    + '</div></div>';
  el.innerHTML = html;
}

export function rolesPrevPage() {
  if (mgmtState.roles.page > 1) { mgmtState.roles.page--; paintRolesTable(); }
}
export function rolesNextPage() {
  const s = mgmtState.roles;
  const totalPages = Math.max(1, Math.ceil(s.list.length / s.pageSize));
  if (s.page < totalPages) { s.page++; paintRolesTable(); }
}

// ---------- 权限管理渲染（按 group 分组展示） ----------
export function renderPermissions(perms) {
  mgmtState.perms.list = perms || [];
  mgmtState.permsCache = perms || [];
  paintPermsList();
}

export function paintPermsList() {
  const el = document.getElementById('permsList');
  if (!el) return;
  const list = mgmtState.perms.list;
  if (!list.length) {
    el.innerHTML = '<p class="muted">' + esc(t('perms.empty')) + '</p>';
    return;
  }
  // 按 group 分组
  const groups = {};
  list.forEach(function (p) {
    const g = p.group || (getLang() === 'zh' ? '未分组' : 'Ungrouped');
    if (!groups[g]) groups[g] = [];
    groups[g].push(p);
  });
  let html = '';
  Object.keys(groups).sort().forEach(function (g) {
    html += '<div class="card"><h3>' + esc(g) + ' <span class="muted">(' + groups[g].length + ')</span></h3>';
    html += '<div class="table-wrap"><table><colgroup><col style="width:35%"><col style="width:65%"></colgroup><thead><tr>'
      + '<th>' + esc(t('perms.col.name')) + '</th>'
      + '<th>' + esc(t('perms.col.description')) + '</th>'
      + '</tr></thead><tbody>';
    groups[g].forEach(function (p) {
      html += '<tr><td><code title="' + esc(p.name) + '">' + esc(p.name) + '</code></td><td class="wrap">' + esc(p.description || '–') + '</td></tr>';
    });
    html += '</tbody></table></div></div>';
  });
  el.innerHTML = html;
}

// ---------- 用户编辑/新增弹窗 ----------
// rolesCache / permsCache 已由 renderRoles / renderPermissions 填充；
// 若未加载，调用方应先加载角色列表。
export function showUserModal(user) {
  const isEdit = !!user;
  const u = user || { username: '', email: '', role_ids: [], status: 'active' };
  const roles = mgmtState.rolesCache || [];
  // 角色复选框
  let roleCheckboxes = '';
  if (roles.length === 0) {
    roleCheckboxes = '<p class="muted">' + esc(t('roles.empty')) + '</p>';
  } else {
    roleCheckboxes = '<div style="display:flex;flex-direction:column;gap:6px;max-height:180px;overflow:auto">';
    roles.forEach(function (r) {
      const checked = (u.role_ids || []).some(function (rid) { return String(rid) === String(r.id); });
      roleCheckboxes += '<label style="margin:0"><input type="checkbox" name="role_ids" value="' + esc(r.id) + '" ' + (checked ? 'checked' : '') + '> ' + esc(r.name) + ' <span class="muted">— ' + esc(r.description || '') + '</span></label>';
    });
    roleCheckboxes += '</div>';
  }
  const body =
    '<div class="field"><label>' + esc(t('users.col.username')) + '</label>'
    + (isEdit
      ? '<input type="text" value="' + esc(u.username) + '" disabled>'
      : '<input type="text" id="modalUsername" value="' + esc(u.username) + '" placeholder="' + esc(t('register.usernamePlaceholder')) + '">')
    + '</div>'
    + (isEdit ? '' : '<div class="field"><label>' + esc(t('register.password')) + '</label><input type="password" id="modalPassword" placeholder="' + esc(t('register.passwordPlaceholder')) + '"></div>')
    + '<div class="field"><label>' + esc(t('users.col.email')) + '</label><input type="email" id="modalEmail" value="' + esc(u.email || '') + '" placeholder="' + esc(t('register.emailPlaceholder')) + '"></div>'
    + '<div class="field"><label>' + esc(t('users.col.roles')) + '</label>' + roleCheckboxes + '</div>'
    + (isEdit ? '<div class="field"><label>' + esc(t('users.col.status')) + '</label><select id="modalStatus"><option value="active" ' + (u.status === 'active' ? 'selected' : '') + '>' + esc(t('users.status.active')) + '</option><option value="disabled" ' + (u.status === 'disabled' ? 'selected' : '') + '>' + esc(t('users.status.disabled')) + '</option></select></div>' : '')
    + '<div class="modal-msg" id="modalMsg"></div>';
  const footer =
    '<button class="ghost" onclick="closeModal()">' + esc(t('common.cancel')) + '</button>'
    + '<button class="primary" onclick="submitUserModal(' + (isEdit ? '\'' + esc(u.id) + '\'' : 'null') + ')">' + esc(t('common.save')) + '</button>';
  showModal(isEdit ? t('users.edit') : t('users.add'), body, footer);
}

// ---------- 角色编辑/新增弹窗 ----------
export function showRoleModal(role) {
  const isEdit = !!role;
  const r = role || { name: '', description: '', permissions: [] };
  const perms = mgmtState.permsCache || [];
  // 权限按 group 分组展示
  const groups = {};
  perms.forEach(function (p) {
    const g = p.group || (getLang() === 'zh' ? '未分组' : 'Ungrouped');
    if (!groups[g]) groups[g] = [];
    groups[g].push(p);
  });
  let permSection = '';
  if (perms.length === 0) {
    permSection = '<p class="muted">' + esc(t('perms.empty')) + '</p>';
  } else {
    Object.keys(groups).sort().forEach(function (g) {
      permSection += '<div class="perm-group"><div class="perm-group-title">' + esc(g) + '</div><div class="perm-list">';
      groups[g].forEach(function (p) {
        const checked = (r.permissions || []).some(function (pid) { return String(pid) === String(p.id); });
        permSection += '<div class="perm-item"><label style="margin:0"><input type="checkbox" name="perm_ids" value="' + esc(p.id) + '" ' + (checked ? 'checked' : '') + '> <code>' + esc(p.name) + '</code></label><span class="perm-desc">' + esc(p.description || '') + '</span></div>';
      });
      permSection += '</div></div>';
    });
  }
  const body =
    '<div class="field"><label>' + esc(t('roles.col.name')) + '</label>'
    + (isEdit
      ? '<input type="text" value="' + esc(r.name) + '" disabled>'
      : '<input type="text" id="modalRoleName" value="' + esc(r.name) + '" placeholder="admin / operator / viewer">')
    + '</div>'
    + '<div class="field"><label>' + esc(t('roles.col.description')) + '</label><textarea id="modalRoleDesc">' + esc(r.description || '') + '</textarea></div>'
    + '<div class="field"><label>' + esc(t('roles.assignPerms')) + '</label>' + permSection + '</div>'
    + '<div class="modal-msg" id="modalMsg"></div>';
  const footer =
    '<button class="ghost" onclick="closeModal()">' + esc(t('common.cancel')) + '</button>'
    + '<button class="primary" onclick="submitRoleModal(' + (isEdit ? '\'' + esc(r.id) + '\'' : 'null') + ')">' + esc(t('common.save')) + '</button>';
  showModal(isEdit ? t('roles.edit') : t('roles.add'), body, footer);
}

// ---------- 弹窗内消息 ----------
export function setModalMsg(msg, isOk) {
  const el = document.getElementById('modalMsg');
  if (!el) return;
  el.textContent = msg || '';
  el.className = 'modal-msg ' + (isOk ? 'ok' : 'err');
}

// ============================================================
// 设备详情 · 监控指标渲染（CPU / 内存 / 磁盘 / 网络 / 服务 / 进程）
// 契约：GET /api/v1/devices/{id}/metrics → 200 {
//   deviceID, hostname, os, osVersion, kernel, arch, uptime,
//   cpu: {cores, usage, model},
//   memory: {total, used, available, usage},
//   disks: [{mount, total, used, free, usage, type}],
//   network: [{name, ip, mac, rxBytes, txBytes, status, speed}],
//   services: [{name, status, enabled}],
//   processCount, collectedAt
// }
// ============================================================

// 字节数格式化：B/KB/MB/GB/TB 自动适配
export function fmtBytes(n) {
  if (n == null || isNaN(n)) return '–';
  n = Number(n);
  if (n < 0) return '–';
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  let i = 0;
  while (n >= 1024 && i < units.length - 1) { n /= 1024; i++; }
  return n.toFixed(i === 0 ? 0 : 1) + ' ' + units[i];
}

// 运行时长格式化：X天X小时X分钟
export function fmtUptime(sec) {
  if (sec == null || isNaN(sec)) return '–';
  sec = Number(sec);
  if (sec < 0) return '–';
  const d = Math.floor(sec / 86400);
  const h = Math.floor((sec % 86400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const parts = [];
  if (d > 0) parts.push(d + '天');
  if (h > 0 || d > 0) parts.push(h + '小时');
  parts.push(m + '分钟');
  return parts.join('');
}

// 使用率颜色：<70% 绿色、70-90% 橙色、>90% 红色
function usageColor(pct) {
  if (pct == null || isNaN(pct)) return 'var(--text-3)';
  if (pct >= 90) return 'var(--fail)';
  if (pct >= 70) return 'var(--warn)';
  return 'var(--ok)';
}

// CSS 进度条
function progressBar(pct) {
  const v = (pct == null || isNaN(pct)) ? 0 : Math.max(0, Math.min(100, Number(pct)));
  const color = usageColor(v);
  return '<div class="metric-bar"><div class="metric-bar-fill" style="width:' + v.toFixed(1) + '%;background:' + color + '"></div></div>';
}

// 基本信息键值对
function infoItem(label, value, isCode) {
  const v = (value == null || value === '') ? '–' : String(value);
  return '<div class="info-item"><span class="info-label">' + esc(label) + '</span>'
    + (isCode ? '<span class="info-val"><code>' + esc(v) + '</code></span>' : '<span class="info-val">' + esc(v) + '</span>')
    + '</div>';
}

// CPU 监控卡片
export function renderCPUCard(cpu) {
  if (!cpu) return '';
  const usage = cpu.usage != null ? Number(cpu.usage) : null;
  const usageTxt = usage != null ? usage.toFixed(1) + '%' : '–';
  return '<div class="metric-card accent-indigo">'
    + '<div class="metric-card-head"><span class="icon">' + icon('ops', 16) + '</span> CPU</div>'
    + '<div class="metric-row"><span class="metric-label">型号</span><span class="metric-val">' + esc(cpu.model || '–') + '</span></div>'
    + '<div class="metric-row"><span class="metric-label">核心数</span><span class="metric-val">' + esc(cpu.cores != null ? cpu.cores : '–') + '</span></div>'
    + '<div class="metric-row"><span class="metric-label">使用率</span><span class="metric-val" style="color:' + usageColor(usage) + ';font-weight:600">' + usageTxt + '</span></div>'
    + progressBar(usage)
    + '</div>';
}

// 内存监控卡片
export function renderMemCard(mem) {
  if (!mem) return '';
  const usage = mem.usage != null ? Number(mem.usage) : null;
  const usageTxt = usage != null ? usage.toFixed(1) + '%' : '–';
  return '<div class="metric-card accent-teal">'
    + '<div class="metric-card-head"><span class="icon">' + icon('cmdb', 16) + '</span> 内存</div>'
    + '<div class="metric-row"><span class="metric-label">总内存</span><span class="metric-val">' + fmtBytes(mem.total) + '</span></div>'
    + '<div class="metric-row"><span class="metric-label">已用</span><span class="metric-val">' + fmtBytes(mem.used) + '</span></div>'
    + '<div class="metric-row"><span class="metric-label">可用</span><span class="metric-val">' + fmtBytes(mem.available) + '</span></div>'
    + '<div class="metric-row"><span class="metric-label">使用率</span><span class="metric-val" style="color:' + usageColor(usage) + ';font-weight:600">' + usageTxt + '</span></div>'
    + progressBar(usage)
    + '</div>';
}

// 磁盘监控卡片（多分区列表）
export function renderDiskCards(disks) {
  if (!disks || !disks.length) return '';
  let html = '<div class="metric-card accent-amber"><div class="metric-card-head"><span class="icon">' + icon('deploy', 16) + '</span> 磁盘</div>';
  disks.forEach(function (d) {
    const usage = d.usage != null ? Number(d.usage) : null;
    const usageTxt = usage != null ? usage.toFixed(1) + '%' : '–';
    html += '<div class="disk-item">'
      + '<div class="disk-head"><b>' + esc(d.mount || '–') + '</b> <span class="muted">(' + esc(d.type || '–') + ')</span></div>'
      + '<div class="metric-row"><span class="metric-label">总容量</span><span class="metric-val">' + fmtBytes(d.total) + '</span></div>'
      + '<div class="metric-row"><span class="metric-label">已用</span><span class="metric-val">' + fmtBytes(d.used) + '</span></div>'
      + '<div class="metric-row"><span class="metric-label">可用</span><span class="metric-val">' + fmtBytes(d.free) + '</span></div>'
      + '<div class="metric-row"><span class="metric-label">使用率</span><span class="metric-val" style="color:' + usageColor(usage) + ';font-weight:600">' + usageTxt + '</span></div>'
      + progressBar(usage)
      + '</div>';
  });
  html += '</div>';
  return html;
}

// 网络监控卡片（多网卡列表）
export function renderNetCards(network) {
  if (!network || !network.length) return '';
  let html = '<div class="metric-card accent-sky"><div class="metric-card-head"><span class="icon">' + icon('context', 16) + '</span> 网络</div>';
  network.forEach(function (n) {
    const up = (n.status === 'up' || n.status === 'UP');
    const statusPill = up ? '<span class="pill ok">up</span>' : '<span class="pill fail">' + esc(n.status || 'down') + '</span>';
    html += '<div class="net-item">'
      + '<div class="net-head"><b>' + esc(n.name || '–') + '</b> ' + statusPill + '</div>'
      + '<div class="metric-row"><span class="metric-label">IP</span><span class="metric-val">' + esc(n.ip || '–') + '</span></div>'
      + '<div class="metric-row"><span class="metric-label">MAC</span><span class="metric-val">' + esc(n.mac || '–') + '</span></div>'
      + '<div class="metric-row"><span class="metric-label">速率</span><span class="metric-val">' + esc(n.speed != null ? n.speed + ' Mbps' : '–') + '</span></div>'
      + '<div class="metric-row"><span class="metric-label">接收</span><span class="metric-val">' + fmtBytes(n.rxBytes) + '</span></div>'
      + '<div class="metric-row"><span class="metric-label">发送</span><span class="metric-val">' + fmtBytes(n.txBytes) + '</span></div>'
      + '</div>';
  });
  html += '</div>';
  return html;
}

// 服务状态卡片（running=绿/stopped=红 + 开机自启）
export function renderServiceCards(services) {
  if (!services || !services.length) return '';
  let html = '<div class="metric-card accent-violet"><div class="metric-card-head"><span class="icon">' + icon('settings', 16) + '</span> 服务状态</div>';
  html += '<div class="table-wrap"><table class="svc-table"><colgroup><col style="width:50%"><col style="width:25%"><col style="width:25%"></colgroup><thead><tr><th>服务名</th><th>状态</th><th>开机自启</th></tr></thead><tbody>';
  services.forEach(function (s) {
    const running = (s.status === 'running' || s.status === 'active');
    const statusPill = running ? '<span class="pill ok">running</span>' : '<span class="pill fail">' + esc(s.status || 'stopped') + '</span>';
    const enabledPill = s.enabled ? '<span class="pill info">是</span>' : '<span class="pill">否</span>';
    html += '<tr><td><b>' + esc(s.name || '–') + '</b></td><td>' + statusPill + '</td><td>' + enabledPill + '</td></tr>';
  });
  html += '</tbody></table></div></div>';
  return html;
}

// 设备详情主渲染函数：渲染到 #deviceDetailBody 并显示弹窗
export function renderDeviceDetail(metrics) {
  const m = metrics || {};
  const body = document.getElementById('deviceDetailBody');
  if (!body) return;
  // 基本信息卡片
  let html = '<div class="metric-card">'
    + '<div class="metric-card-head"><span class="icon">' + icon('device', 16) + '</span> 基本信息</div>'
    + '<div class="info-grid">'
    + infoItem('主机名', m.hostname)
    + infoItem('设备ID', m.deviceID, true)
    + infoItem('操作系统', m.os)
    + infoItem('系统版本', m.osVersion)
    + infoItem('内核', m.kernel)
    + infoItem('架构', m.arch)
    + infoItem('运行时长', fmtUptime(m.uptime))
    + infoItem('进程数', m.processCount)
    + infoItem('采集时间', fmtTime(m.collectedAt))
    + '</div></div>';
  // CPU + 内存（并排）
  html += '<div class="metric-grid">';
  html += renderCPUCard(m.cpu);
  html += renderMemCard(m.memory);
  html += '</div>';
  // 磁盘 / 网络 / 服务（纵向堆叠）
  html += renderDiskCards(m.disks);
  html += renderNetCards(m.network);
  html += renderServiceCards(m.services);
  body.innerHTML = html;
  // 显示弹窗
  const modal = document.getElementById('deviceDetailModal');
  if (modal) modal.classList.add('open');
}

// 关闭设备详情弹窗
export function closeDeviceDetail() {
  const modal = document.getElementById('deviceDetailModal');
  if (modal) modal.classList.remove('open');
}