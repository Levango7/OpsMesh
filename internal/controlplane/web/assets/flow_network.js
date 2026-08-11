// flow_network.js — task 244 M6 集成：网络拓扑发现 + 网络诊断工具 + 连通性检测 页面交互逻辑
//
// 职责：
//   - loadNetworkTopology：加载网络拓扑并渲染 SVG 拓扑图
//   - refreshNetworkTopology：强制刷新拓扑
//   - renderNetworkTopologySVG：用纯 SVG 渲染拓扑图（节点=圆形/方形，边=线段+延迟标注）
//   - showDiagnoseModal / closeDiagnoseModal / executeDiagnose：诊断工具弹窗 + 提交 + 轮询结果
//   - showConnectivityModal / closeConnectivityModal / executeConnectivity：连通性检测弹窗 + 提交 + 结果展示
//
// 设计要点：
//   - 拓扑图用纯 SVG 实现（不引入 d3/cytoscape 等第三方库）；
//   - 节点用力导向布局的简化版（圆形排列 + 边连接）；
//   - 支持拖拽节点、缩放（滚轮）；
//   - 在线节点用绿色实心圆，离线节点用灰色方形；
//   - 边用线段表示连通性，标注延迟 ms，loss>0 用红色虚线。
//
// 依赖：api.js、render.js（esc/escAttr/fmtTime）、icons.js、i18n.js

import * as api from './api.js';
import { esc, escAttr, fmtTime } from './render.js';
import { icon } from './icons.js';
import { t } from './i18n.js';

// ============================================================================
// 网络拓扑图
// ============================================================================

// 拓扑图状态（节点位置、缩放、拖拽状态）。
const topoState = {
  nodes: [],         // 节点数据
  edges: [],         // 边数据
  positions: {},     // {nodeId: {x, y}} 节点位置
  scale: 1,          // 缩放比例
  offsetX: 0,        // 平移偏移 X
  offsetY: 0,        // 平移偏移 Y
  dragging: null,    // 当前拖拽的节点 ID
  dragStartX: 0,     // 拖拽起始 X
  dragStartY: 0,     // 拖拽起始 Y
  generatedAt: null, // 拓扑生成时间
};

// loadNetworkTopology 加载网络拓扑并渲染到 #networkTopologyGraph。
export function loadNetworkTopology() {
  const el = document.getElementById('networkTopologyGraph');
  if (!el) return;
  el.innerHTML = '<p class="muted">' + esc(t('network.loading')) + '</p>';
  api.getNetworkTopology(false).then(function (topo) {
    renderNetworkTopology(topo || { nodes: [], edges: [] });
  }).catch(function (e) {
    api.apiFail('networkTopology', e);
    el.innerHTML = '<p class="muted">' + esc(t('network.loadFail')) + ': ' + esc(String(e.message || e)) + '</p>';
  });
}

// refreshNetworkTopology 强制刷新网络拓扑（?refresh=true）。
export function refreshNetworkTopology() {
  const el = document.getElementById('networkTopologyGraph');
  if (!el) return;
  el.innerHTML = '<p class="muted">' + esc(t('network.refreshing')) + '</p>';
  api.getNetworkTopology(true).then(function (topo) {
    renderNetworkTopology(topo || { nodes: [], edges: [] });
  }).catch(function (e) {
    api.apiFail('networkTopology', e);
    el.innerHTML = '<p class="muted">' + esc(t('network.loadFail')) + ': ' + esc(String(e.message || e)) + '</p>';
  });
}

// renderNetworkTopology 渲染网络拓扑图（SVG）。
function renderNetworkTopology(topo) {
  const container = document.getElementById('networkTopologyGraph');
  if (!container) return;
  const nodes = topo.nodes || [];
  const edges = topo.edges || [];
  topoState.nodes = nodes;
  topoState.edges = edges;
  topoState.generatedAt = topo.generatedAt || null;

  if (!nodes.length) {
    container.innerHTML = '<p class="muted">' + esc(t('network.empty')) + '</p>';
    return;
  }

  // 计算节点位置（圆形排列）。
  layoutNodes(nodes);

  // 渲染 SVG。
  const width = 800;
  const height = 500;
  let svg = '<svg id="networkTopologySVG" width="100%" height="' + height + '" viewBox="0 0 ' + width + ' ' + height + '" style="border:1px solid var(--border);background:var(--bg-1);cursor:default">';
  // 缩放组
  svg += '<g id="topoTransform" transform="translate(' + topoState.offsetX + ',' + topoState.offsetY + ') scale(' + topoState.scale + ')">';

  // 渲染边（先渲染边，使节点在上层）。
  edges.forEach(function (e) {
    const src = topoState.positions[e.source];
    const tgt = topoState.positions[e.target];
    if (!src || !tgt) return;
    const isLoss = e.loss > 0;
    const strokeColor = e.alive ? (isLoss ? '#e67e22' : '#2ecc71') : '#e74c3c';
    const strokeDash = isLoss ? '6,3' : '';
    const midX = (src.x + tgt.x) / 2;
    const midY = (src.y + tgt.y) / 2;
    const latencyText = e.alive ? (e.latencyMs >= 0 ? e.latencyMs.toFixed(1) + 'ms' : '?') : '✕';
    svg += '<line x1="' + src.x + '" y1="' + src.y + '" x2="' + tgt.x + '" y2="' + tgt.y + '"'
      + ' stroke="' + strokeColor + '" stroke-width="1.5"'
      + (strokeDash ? ' stroke-dasharray="' + strokeDash + '"' : '')
      + ' opacity="0.7" />';
    // 延迟标注（带背景）。
    svg += '<rect x="' + (midX - 22) + '" y="' + (midY - 9) + '" width="44" height="18" rx="3"'
      + ' fill="var(--bg-2)" stroke="' + strokeColor + '" stroke-width="0.5" opacity="0.9" />';
    svg += '<text x="' + midX + '" y="' + (midY + 4) + '" text-anchor="middle"'
      + ' font-size="10" fill="' + strokeColor + '">' + esc(latencyText) + '</text>';
  });

  // 渲染节点。
  nodes.forEach(function (n) {
    const pos = topoState.positions[n.id];
    if (!pos) return;
    const isOnline = n.status === 'online';
    const fill = isOnline ? '#2ecc71' : '#95a5a6';
    const shape = isOnline ? 'circle' : 'rect';
    if (shape === 'circle') {
      svg += '<circle cx="' + pos.x + '" cy="' + pos.y + '" r="18"'
        + ' fill="' + fill + '" stroke="var(--bg-1)" stroke-width="2"'
        + ' class="topo-node" data-node-id="' + escAttr(n.id) + '"'
        + ' style="cursor:move" />';
    } else {
      svg += '<rect x="' + (pos.x - 16) + '" y="' + (pos.y - 16) + '" width="32" height="32" rx="4"'
        + ' fill="' + fill + '" stroke="var(--bg-1)" stroke-width="2"'
        + ' class="topo-node" data-node-id="' + escAttr(n.id) + '"'
        + ' style="cursor:move" />';
    }
    // 节点标签（hostname + IP）。
    const label = n.hostname || n.id;
    const ipLabel = n.ip ? '\n' + n.ip : '';
    svg += '<text x="' + pos.x + '" y="' + (pos.y + 32) + '" text-anchor="middle"'
      + ' font-size="11" fill="var(--text-1)" font-weight="600">' + esc(label) + '</text>';
    if (n.ip) {
      svg += '<text x="' + pos.x + '" y="' + (pos.y + 46) + '" text-anchor="middle"'
        + ' font-size="9" fill="var(--text-3)">' + esc(n.ip) + '</text>';
    }
  });

  svg += '</g>';
  // 缩放控制按钮。
  svg += '<g transform="translate(' + (width - 100) + ',10)">';
  svg += '<rect x="0" y="0" width="90" height="28" rx="4" fill="var(--bg-2)" stroke="var(--border)" />';
  svg += '<text x="10" y="18" font-size="14" fill="var(--text-1)" style="cursor:pointer" onclick="zoomNetworkTopo(1.2)">＋</text>';
  svg += '<text x="35" y="18" font-size="14" fill="var(--text-1)" style="cursor:pointer" onclick="zoomNetworkTopo(0.8)">－</text>';
  svg += '<text x="55" y="18" font-size="11" fill="var(--text-1)" style="cursor:pointer" onclick="resetNetworkTopo()">重置</text>';
  svg += '</g>';
  svg += '</svg>';

  // 图例 + 信息。
  let legend = '<div style="margin-top:8px;display:flex;gap:16px;flex-wrap:wrap;align-items:center;font-size:12px;color:var(--text-3)">';
  legend += '<span><span style="display:inline-block;width:12px;height:12px;border-radius:50%;background:#2ecc71;vertical-align:middle"></span> ' + esc(t('network.online')) + '</span>';
  legend += '<span><span style="display:inline-block;width:12px;height:12px;background:#95a5a6;vertical-align:middle"></span> ' + esc(t('network.offline')) + '</span>';
  legend += '<span><span style="display:inline-block;width:16px;height:2px;background:#2ecc71;vertical-align:middle"></span> ' + esc(t('network.alive')) + '</span>';
  legend += '<span><span style="display:inline-block;width:16px;height:2px;background:#e67e22;border-top:2px dashed;vertical-align:middle"></span> ' + esc(t('network.packetLoss')) + '</span>';
  legend += '<span><span style="display:inline-block;width:16px;height:2px;background:#e74c3c;vertical-align:middle"></span> ' + esc(t('network.unreachable')) + '</span>';
  if (topoState.generatedAt) {
    legend += '<span style="margin-left:auto">' + esc(t('network.generatedAt')) + ': ' + esc(fmtTime(topoState.generatedAt)) + '</span>';
  }
  legend += '</div>';

  container.innerHTML = svg + legend;

  // 绑定节点拖拽事件。
  bindNodeDragEvents();
}

// layoutNodes 计算节点位置（圆形排列）。
function layoutNodes(nodes) {
  const n = nodes.length;
  const centerX = 400;
  const centerY = 250;
  const radius = n <= 1 ? 0 : Math.min(180, 60 + n * 8);
  // 保留已有位置（拖拽后的位置不丢失）。
  const newPositions = {};
  nodes.forEach(function (node, i) {
    if (topoState.positions[node.id]) {
      newPositions[node.id] = topoState.positions[node.id];
    } else {
      if (n === 1) {
        newPositions[node.id] = { x: centerX, y: centerY };
      } else {
        const angle = (2 * Math.PI * i) / n - Math.PI / 2;
        newPositions[node.id] = {
          x: centerX + radius * Math.cos(angle),
          y: centerY + radius * Math.sin(angle),
        };
      }
    }
  });
  topoState.positions = newPositions;
}

// bindNodeDragEvents 绑定节点拖拽事件。
function bindNodeDragEvents() {
  const svg = document.getElementById('networkTopologySVG');
  if (!svg) return;
  const nodes = svg.querySelectorAll('.topo-node');
  nodes.forEach(function (node) {
    node.addEventListener('mousedown', function (e) {
      const nodeId = node.getAttribute('data-node-id');
      topoState.dragging = nodeId;
      topoState.dragStartX = e.clientX;
      topoState.dragStartY = e.clientY;
      e.preventDefault();
    });
  });
  document.addEventListener('mousemove', onTopoMouseMove);
  document.addEventListener('mouseup', onTopoMouseUp);
  // 滚轮缩放。
  svg.addEventListener('wheel', function (e) {
    e.preventDefault();
    const delta = e.deltaY > 0 ? 0.9 : 1.1;
    zoomNetworkTopo(delta);
  });
}

// onTopoMouseMove 鼠标移动事件（拖拽节点）。
function onTopoMouseMove(e) {
  if (!topoState.dragging) return;
  const dx = (e.clientX - topoState.dragStartX) / topoState.scale;
  const dy = (e.clientY - topoState.dragStartY) / topoState.scale;
  const pos = topoState.positions[topoState.dragging];
  if (pos) {
    pos.x += dx;
    pos.y += dy;
  }
  topoState.dragStartX = e.clientX;
  topoState.dragStartY = e.clientY;
  // 重新渲染（仅更新位置）。
  renderNetworkTopology({ nodes: topoState.nodes, edges: topoState.edges, generatedAt: topoState.generatedAt });
}

// onTopoMouseUp 鼠标释放事件（结束拖拽）。
function onTopoMouseUp() {
  topoState.dragging = null;
}

// zoomNetworkTopo 缩放拓扑图。
export function zoomNetworkTopo(factor) {
  topoState.scale = Math.max(0.3, Math.min(3, topoState.scale * factor));
  const g = document.getElementById('topoTransform');
  if (g) {
    g.setAttribute('transform', 'translate(' + topoState.offsetX + ',' + topoState.offsetY + ') scale(' + topoState.scale + ')');
  }
}

// resetNetworkTopo 重置拓扑图（缩放 + 位置）。
export function resetNetworkTopo() {
  topoState.scale = 1;
  topoState.offsetX = 0;
  topoState.offsetY = 0;
  topoState.positions = {};
  renderNetworkTopology({ nodes: topoState.nodes, edges: topoState.edges, generatedAt: topoState.generatedAt });
}

// ============================================================================
// 网络诊断工具弹窗
// ============================================================================

// showDiagnoseModal 显示诊断工具弹窗。
export function showDiagnoseModal() {
  const modal = document.getElementById('networkDiagnoseModal');
  if (!modal) return;
  // 加载 agent 列表填充下拉框。
  api.getAgents().then(function (agents) {
    const select = document.getElementById('diagnoseAgentSelect');
    if (select) {
      let opts = '<option value="">' + esc(t('network.selectAgent')) + '</option>';
      (agents || []).forEach(function (a) {
        opts += '<option value="' + escAttr(a.agentID) + '">' + esc(a.hostname || a.agentID) + ' (' + esc(a.status || 'unknown') + ')</option>';
      });
      select.innerHTML = opts;
    }
  }).catch(function () {});
  // 重置表单。
  const form = document.getElementById('diagnoseForm');
  if (form) form.reset();
  // 默认值。
  setDiagnoseDefaults();
  // 显示弹窗。
  modal.style.display = 'flex';
}

// setDiagnoseDefaults 设置诊断表单默认值。
function setDiagnoseDefaults() {
  const tool = document.getElementById('diagnoseTool');
  if (tool && !tool.value) tool.value = 'ping';
  const count = document.getElementById('diagnoseCount');
  if (count && !count.value) count.value = '4';
  const timeout = document.getElementById('diagnoseTimeout');
  if (timeout && !timeout.value) timeout.value = '5';
  // 切换工具时更新参数可见性。
  onDiagnoseToolChange();
}

// closeDiagnoseModal 关闭诊断弹窗。
export function closeDiagnoseModal() {
  const modal = document.getElementById('networkDiagnoseModal');
  if (modal) modal.style.display = 'none';
}

// onDiagnoseToolChange 工具切换时更新参数可见性。
export function onDiagnoseToolChange() {
  const tool = document.getElementById('diagnoseTool');
  const portGroup = document.getElementById('diagnosePortGroup');
  const countGroup = document.getElementById('diagnoseCountGroup');
  if (!tool) return;
  const isTcping = tool.value === 'tcping';
  const isPing = tool.value === 'ping';
  if (portGroup) portGroup.style.display = isTcping ? '' : 'none';
  if (countGroup) countGroup.style.display = isPing ? '' : 'none';
}

// executeDiagnose 提交诊断任务并轮询结果。
export function executeDiagnose() {
  const agentId = document.getElementById('diagnoseAgentSelect') ? document.getElementById('diagnoseAgentSelect').value : '';
  const tool = document.getElementById('diagnoseTool') ? document.getElementById('diagnoseTool').value : '';
  const target = document.getElementById('diagnoseTarget') ? document.getElementById('diagnoseTarget').value.trim() : '';
  const count = parseInt(document.getElementById('diagnoseCount') ? document.getElementById('diagnoseCount').value : '4', 10);
  const timeout = parseInt(document.getElementById('diagnoseTimeout') ? document.getElementById('diagnoseTimeout').value : '5', 10);
  const port = parseInt(document.getElementById('diagnosePort') ? document.getElementById('diagnosePort').value : '0', 10);

  if (!agentId) { alert(t('network.selectAgentFirst')); return; }
  if (!tool) { alert(t('network.selectTool')); return; }
  if (!target) { alert(t('network.targetRequired')); return; }

  const options = { count: count, timeout: timeout };
  if (tool === 'tcping' && port > 0) options.port = port;

  // 显示执行中状态。
  const result = document.getElementById('diagnoseResult');
  if (result) {
    result.innerHTML = '<p class="muted">' + esc(t('network.executing')) + '</p>';
  }

  api.diagnoseNetwork(agentId, tool, target, options).then(function (resp) {
    if (resp.s !== 202 && resp.s !== 200 && resp.s !== 201) {
      throw new Error((resp.j && resp.j.error) || ('HTTP ' + resp.s));
    }
    const taskId = resp.j && resp.j.taskId;
    if (!taskId) throw new Error('no taskId returned');
    // 轮询结果。
    pollDiagnoseResult(taskId);
  }).catch(function (e) {
    if (result) {
      result.innerHTML = '<p class="fail">' + esc(t('network.diagnoseFail')) + ': ' + esc(String(e.message || e)) + '</p>';
    }
  });
}

// pollDiagnoseResult 轮询诊断任务结果（最多 30 秒）。
function pollDiagnoseResult(taskId) {
  const result = document.getElementById('diagnoseResult');
  if (!result) return;
  const maxAttempts = 60;
  let attempts = 0;
  const poll = function () {
    attempts++;
    if (attempts > maxAttempts) {
      result.innerHTML = '<p class="fail">' + esc(t('network.timeout')) + '</p>';
      return;
    }
    api.getDiagnoseResult(taskId).then(function (data) {
      if (data && data.pending) {
        // 仍在执行中，继续轮询。
        result.innerHTML = '<p class="muted">' + esc(t('network.executing')) + ' (' + attempts + ')</p>';
        setTimeout(poll, 500);
        return;
      }
      // 渲染结果。
      renderDiagnoseResult(data);
    }).catch(function (e) {
      result.innerHTML = '<p class="fail">' + esc(t('network.diagnoseFail')) + ': ' + esc(String(e.message || e)) + '</p>';
    });
  };
  poll();
}

// renderDiagnoseResult 渲染诊断结果。
function renderDiagnoseResult(data) {
  const result = document.getElementById('diagnoseResult');
  if (!result) return;
  if (!data) {
    result.innerHTML = '<p class="fail">' + esc(t('network.noResult')) + '</p>';
    return;
  }
  const exitCode = data.exitCode;
  const success = exitCode === 0;
  let html = '<div class="diagnose-result">';
  html += '<div style="margin-bottom:8px"><b>' + esc(t('network.exitCode')) + ':</b> '
    + (success ? '<span class="badge ok">' : '<span class="badge fail">') + exitCode + '</span>'
    + ' <b style="margin-left:12px">' + esc(t('network.duration')) + ':</b> ' + (data.durationMs || 0) + 'ms</div>';
  if (data.stdout) {
    html += '<div><b>' + esc(t('network.stdout')) + ':</b><pre class="output-pre">' + esc(data.stdout) + '</pre></div>';
  }
  if (data.stderr) {
    html += '<div style="margin-top:8px"><b>' + esc(t('network.stderr')) + ':</b><pre class="output-pre" style="color:var(--rose)">' + esc(data.stderr) + '</pre></div>';
  }
  html += '</div>';
  result.innerHTML = html;
}

// ============================================================================
// 连通性检测弹窗
// ============================================================================

// showConnectivityModal 显示连通性检测弹窗。
export function showConnectivityModal() {
  const modal = document.getElementById('networkConnectivityModal');
  if (!modal) return;
  // 加载 agent 列表。
  api.getAgents().then(function (agents) {
    const select = document.getElementById('connectivityAgentSelect');
    if (select) {
      let opts = '<option value="">' + esc(t('network.selectAgent')) + '</option>';
      (agents || []).forEach(function (a) {
        opts += '<option value="' + escAttr(a.agentID) + '">' + esc(a.hostname || a.agentID) + ' (' + esc(a.status || 'unknown') + ')</option>';
      });
      select.innerHTML = opts;
    }
  }).catch(function () {});
  // 重置表单。
  const form = document.getElementById('connectivityForm');
  if (form) form.reset();
  // 清空结果。
  const result = document.getElementById('connectivityResult');
  if (result) result.innerHTML = '';
  modal.style.display = 'flex';
}

// closeConnectivityModal 关闭连通性检测弹窗。
export function closeConnectivityModal() {
  const modal = document.getElementById('networkConnectivityModal');
  if (modal) modal.style.display = 'none';
}

// executeConnectivity 提交连通性检测。
export function executeConnectivity() {
  const agentId = document.getElementById('connectivityAgentSelect') ? document.getElementById('connectivityAgentSelect').value : '';
  const targetsText = document.getElementById('connectivityTargets') ? document.getElementById('connectivityTargets').value.trim() : '';

  if (!agentId) { alert(t('network.selectAgentFirst')); return; }
  if (!targetsText) { alert(t('network.targetsRequired')); return; }

  // 解析目标列表（每行一个，格式：ip 或 ip:port）。
  const targets = [];
  targetsText.split(/\n|,/).forEach(function (line) {
    line = line.trim();
    if (!line) return;
    let ip = line;
    let port = 0;
    if (line.indexOf(':') > 0) {
      const parts = line.split(':');
      ip = parts[0].trim();
      port = parseInt(parts[1], 10) || 0;
    }
    if (ip) targets.push({ ip: ip, port: port });
  });
  if (!targets.length) { alert(t('network.targetsRequired')); return; }

  // 显示执行中状态。
  const result = document.getElementById('connectivityResult');
  if (result) {
    result.innerHTML = '<p class="muted">' + esc(t('network.executing')) + '</p>';
  }

  api.checkConnectivity(agentId, targets).then(function (resp) {
    if (resp.s !== 200) {
      throw new Error((resp.j && resp.j.error) || ('HTTP ' + resp.s));
    }
    renderConnectivityResult(resp.j);
  }).catch(function (e) {
    if (result) {
      result.innerHTML = '<p class="fail">' + esc(t('network.connectivityFail')) + ': ' + esc(String(e.message || e)) + '</p>';
    }
  });
}

// renderConnectivityResult 渲染连通性检测结果。
function renderConnectivityResult(data) {
  const result = document.getElementById('connectivityResult');
  if (!result) return;
  const results = (data && data.results) || [];
  if (!results.length) {
    result.innerHTML = '<p class="muted">' + esc(t('network.noResult')) + '</p>';
    return;
  }
  let html = '<table class="data-table"><thead><tr>';
  html += '<th>' + esc(t('network.target')) + '</th>';
  html += '<th>' + esc(t('network.alive')) + '</th>';
  html += '<th>' + esc(t('network.latency')) + '</th>';
  html += '</tr></thead><tbody>';
  results.forEach(function (r) {
    const aliveBadge = r.alive
      ? '<span class="badge ok">' + esc(t('network.yes')) + '</span>'
      : '<span class="badge fail">' + esc(t('network.no')) + '</span>';
    const latencyText = r.alive ? (r.latencyMs >= 0 ? r.latencyMs.toFixed(1) + ' ms' : '—') : '—';
    html += '<tr>'
      + '<td><code>' + esc(r.target) + '</code></td>'
      + '<td>' + aliveBadge + '</td>'
      + '<td>' + latencyText + '</td>'
      + '</tr>';
  });
  html += '</tbody></table>';
  result.innerHTML = html;
}