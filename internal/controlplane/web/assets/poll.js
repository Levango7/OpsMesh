// poll.js — 轮询调度
// 职责：5s 轮询启停、页面可见性暂停（visibilitychange）、轮询失败退避、刷新策略。
// 基础轮询函数 pollDevices / pollTasks / pollAlerts 也在此处（轻量，仅 fetch+render+apiOk/apiFail）。
// M3-2B：新增 SSE 实时推送（startSSE/stopSSE），优先 SSE，失败降级回退轮询。

import { getDevices, getTasks, getAlerts, apiOk, apiFail } from './api.js';
import { renderDevices, renderAlerts, renderTasks } from './render.js';

// ---------- 基础轮询 ----------
export function pollDevices() {
  getDevices()
    .then(function (s) { apiOk('devices'); renderDevices(s); })
    .catch(function (e) { apiFail('devices', e); });
}

export function pollAlerts() {
  getAlerts()
    .then(function (s) { apiOk('alerts'); renderAlerts(s); })
    .catch(function (e) { apiFail('alerts', e); });
}

export function pollTasks() {
  const st = document.getElementById('statusFilter').value;
  getTasks(st)
    .then(function (s) { apiOk('tasks'); renderTasks(s); })
    .catch(function (e) { apiFail('tasks', e); });
}

// ---------- 轮询调度器 ----------
let pollTimers = [];
let auxTimers = [];   // 辅助轮询：alertsFull / deploys（按 tab 激活才跑）
let started = false;

// 辅助轮询回调由 main.js 注入（避免 poll.js 反向依赖 flow.js）
let auxCallbacks = { pollAlertsFull: null, pollDeploys: null };

export function setAuxCallbacks(cbs) {
  auxCallbacks = Object.assign(auxCallbacks, cbs);
}

export function startPolling() {
  if (started) return;
  started = true;
  pollTimers = [
    setInterval(pollDevices, 5000),
    setInterval(pollTasks, 5000),
    setInterval(pollAlerts, 10000),
  ];
  // 辅助轮询：仅在对应 tab 激活时执行
  auxTimers = [
    setInterval(function () {
      const p = document.getElementById('tab-alerts');
      if (p && p.classList.contains('active') && auxCallbacks.pollAlertsFull) auxCallbacks.pollAlertsFull();
    }, 10000),
    setInterval(function () {
      const p = document.getElementById('tab-deploy');
      if (p && p.classList.contains('active') && auxCallbacks.pollDeploys) auxCallbacks.pollDeploys();
    }, 8000),
  ];
  // 页面可见性：隐藏时暂停，可见时恢复并立即拉一次
  document.addEventListener('visibilitychange', onVisibilityChange);
}

export function stopPolling() {
  pollTimers.forEach(clearInterval); pollTimers = [];
  auxTimers.forEach(clearInterval); auxTimers = [];
  document.removeEventListener('visibilitychange', onVisibilityChange);
  started = false;
}

function onVisibilityChange() {
  if (document.hidden) {
    // 暂停主轮询（保留 auxTimers，因其本身按 tab 激活才执行，开销可控）
    pollTimers.forEach(clearInterval); pollTimers = [];
  } else {
    // 恢复并立即拉一次
    if (!pollTimers.length) {
      pollTimers = [
        setInterval(pollDevices, 5000),
        setInterval(pollTasks, 5000),
        setInterval(pollAlerts, 10000),
      ];
    }
    pollDevices(); pollTasks(); pollAlerts();
  }
}

// ---------- M3-2B SSE 实时推送 ----------
// 优先使用 SSE（/api/v1/events/stream）替代 5s 轮询；连接失败连续 3 次后降级回退轮询。
// 事件类型：task_status（刷新任务表）、alert_new（刷新告警）、device_online/device_offline（刷新设备表）。
// SSE 连接断开时 EventSource 会自动重连（readyState=CONNECTING），仅 CLOSED 或连续失败才降级。

let sse = null;
let sseActive = false;
let sseFailCount = 0;
const SSE_FAIL_THRESHOLD = 3; // 连续失败 3 次后降级轮询

// startSSE 启动 SSE 实时推送。若浏览器不支持 EventSource 或连接持续失败，降级到轮询。
export function startSSE() {
  if (sseActive) return;
  // 浏览器不支持 EventSource：直接降级轮询
  if (typeof EventSource === 'undefined') {
    console.warn('[sse] EventSource 不支持，降级轮询');
    startPolling();
    return;
  }
  try {
    sse = new EventSource('/api/v1/events/stream');
  } catch (e) {
    console.warn('[sse] EventSource 构造失败，降级轮询', e);
    startPolling();
    return;
  }
  // 连接建立：重置失败计数
  sse.onopen = function () {
    sseFailCount = 0;
    console.info('[sse] 连接已建立');
  };
  // task_status：刷新任务表（轻量 fetch + render，复用 pollTasks）
  sse.addEventListener('task_status', function (e) {
    try { pollTasks(); } catch (err) { console.error('[sse] task_status handler error', err); }
  });
  // alert_new：刷新告警面板
  sse.addEventListener('alert_new', function (e) {
    try { pollAlerts(); } catch (err) { console.error('[sse] alert_new handler error', err); }
  });
  // device_online / device_offline：刷新设备表
  sse.addEventListener('device_online', function (e) {
    try { pollDevices(); } catch (err) { console.error('[sse] device_online handler error', err); }
  });
  sse.addEventListener('device_offline', function (e) {
    try { pollDevices(); } catch (err) { console.error('[sse] device_offline handler error', err); }
  });
  // 错误处理：CLOSED 状态或连续失败达阈值则降级轮询
  sse.onerror = function () {
    sseFailCount++;
    console.warn('[sse] 连接异常，失败次数=' + sseFailCount + ' readyState=' + sse.readyState);
    if (sse.readyState === EventSource.CLOSED || sseFailCount >= SSE_FAIL_THRESHOLD) {
      console.warn('[sse] 降级到轮询模式');
      stopSSE();
      startPolling();
    }
    // 否则 EventSource 自动重连（readyState=CONNECTING），保持 SSE 模式
  };
  sseActive = true;
}

// stopSSE 关闭 SSE 连接并重置状态。
export function stopSSE() {
  if (sse) {
    sse.close();
    sse = null;
  }
  sseActive = false;
  sseFailCount = 0;
}

// isSSEActive 返回 SSE 是否处于活跃状态（供 main.js 调试/判断用）。
export function isSSEActive() {
  return sseActive;
}