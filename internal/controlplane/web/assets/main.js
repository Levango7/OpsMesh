// main.js — 入口与模块装配
// 职责：初始化、事件绑定、启动轮询、装配各模块。
// 作为入口被 index.html 以 <script type="module" src="/assets/main.js"> 引用。
//
// 兼容层说明：index.html 中保留了大量内联 onclick="xxx()" 调用，ES module 作用域内
// 的函数对内联事件不可见。此处将所有需被 HTML 内联调用的函数挂到 window 上，作为
// 模块化拆分与零功能改动的兼容层。模块间通信仍走 import/export，不通过 window。

import { startPolling, stopPolling, setAuxCallbacks, pollDevices, pollTasks, pollAlerts, startSSE, stopSSE, isSSEActive } from './poll.js';
import { paintStats } from './render.js';
import {
  switchTab, toggleGuide, openDevice, provision, closeDrawer,
  loadCMDBTypes, pollCIs, pollTemplates, openCI, submitCIForm,
  loadFlows, openWorkflow, newWorkflow, loadDemo, addNode, deleteNode, selectNode, toggleLink,
  autoLayout, applyNode, saveWorkflow, runWorkflow, scheduleWorkflowPrompt, alignSel,
  undo, redo, zoomBy, fitView, resetView, flowKey, svgMouseDown, svgWheel,
  loadDeployDemo, pollDeploys, execDeploy, rollbackDeploy, openDeploy, submitDeployForm,
  searchLogs, logPrev, logNext, resetLogFilters,
  pollAlertsFull, ackAlert, silenceAlert,
  setFocus, clearFocus, jumpFocus,
  fetchMe, loadAgents, submitTaskForm,
} from './flow.js';

// ---------- window 兼容层：供 index.html 内联 onclick 调用 ----------
const w = window;
// 标签 / 抽屉 / 纳管
w.switchTab = switchTab;
w.toggleGuide = toggleGuide;
w.openDevice = openDevice;
w.provision = provision;
w.closeDrawer = closeDrawer;
// CMDB
w.loadCMDBTypes = loadCMDBTypes;
w.pollCIs = pollCIs;
w.pollTemplates = pollTemplates;
w.openCI = openCI;
// 作业编排
w.loadFlows = loadFlows;
w.openWorkflow = openWorkflow;
w.newWorkflow = newWorkflow;
w.loadDemo = loadDemo;
w.addNode = addNode;
w.deleteNode = deleteNode;
w.selectNode = selectNode;
w.toggleLink = toggleLink;
w.autoLayout = autoLayout;
w.applyNode = applyNode;
w.saveWorkflow = saveWorkflow;
w.runWorkflow = runWorkflow;
w.scheduleWorkflowPrompt = scheduleWorkflowPrompt;
w.alignSel = alignSel;
w.undo = undo;
w.redo = redo;
w.zoomBy = zoomBy;
w.fitView = fitView;
w.resetView = resetView;
// 部署
w.loadDeployDemo = loadDeployDemo;
w.pollDeploys = pollDeploys;
w.execDeploy = execDeploy;
w.rollbackDeploy = rollbackDeploy;
w.openDeploy = openDeploy;
// 日志
w.searchLogs = searchLogs;
w.logPrev = logPrev;
w.logNext = logNext;
w.resetLogFilters = resetLogFilters;
// 告警
w.pollAlertsFull = pollAlertsFull;
w.ackAlert = ackAlert;
w.silenceAlert = silenceAlert;
// 跨模块联动
w.setFocus = setFocus;
w.clearFocus = clearFocus;
w.jumpFocus = jumpFocus;

// ---------- 注入辅助轮询回调（poll.js 不反向依赖 flow.js） ----------
setAuxCallbacks({ pollAlertsFull: pollAlertsFull, pollDeploys: pollDeploys });

// ---------- 初始化 ----------
function init() {
  loadAgents();
  // 立即拉一次首屏数据（SSE 仅推送增量，首屏仍需全量拉取）
  pollDevices(); pollTasks(); pollAlerts();
  paintStats();
  fetchMe();

  // M3-2B：优先启动 SSE 实时推送，失败自动降级回退轮询（startSSE 内部处理降级）。
  // startSSE 在浏览器不支持 EventSource 或连接持续失败时调 startPolling，故无需额外分支。
  startSSE();

  // ---------- 事件绑定（原 window.onload 内的逻辑） ----------
  // 任务状态筛选
  const sf = document.getElementById('statusFilter');
  if (sf) sf.addEventListener('change', pollTasks);

  // 任务下发表单
  const tf = document.getElementById('taskForm');
  if (tf) tf.addEventListener('submit', function (e) { e.preventDefault(); submitTaskForm(); });

  // CI 创建表单
  const cf = document.getElementById('ciForm');
  if (cf) cf.addEventListener('submit', function (e) { e.preventDefault(); submitCIForm(); });

  // 部署登记表单
  const df = document.getElementById('deployForm');
  if (df) df.addEventListener('submit', function (e) { e.preventDefault(); submitDeployForm(); });

  // 画布：平移 / 滚轮缩放
  const cv = document.getElementById('canvas');
  if (cv) {
    cv.addEventListener('mousedown', svgMouseDown);
    cv.addEventListener('wheel', svgWheel, { passive: false });
  }

  // 快捷键（仅在作业编排 tab 激活时生效）
  document.addEventListener('keydown', flowKey);
}

// 暴露 stopPolling / SSE 调试入口（可选）
w.__stopPolling = stopPolling;
w.__stopSSE = stopSSE;
w.__isSSEActive = isSSEActive;

// DOMContentLoaded 兼容：脚本以 type=module 引入时默认 defer，DOM 已就绪；
// 但为稳妥起见仍判断 readyState。
if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', init);
} else {
  init();
}
