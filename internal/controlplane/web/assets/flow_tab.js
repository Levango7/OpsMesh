// flow_tab.js — 标签切换 + 导航 + 抽屉
// 从 flow.js 拆分（P2-1）。职责：switchTab 路由各业务域加载函数、toggleGuide、closeDrawer。
// 依赖：各业务域模块的加载函数 + render.js（paintStats）+ poll.js（pollTasks）。

import { paintStats } from './render.js';
import { pollTasks } from './poll.js';
import { loadCMDBTypes } from './flow_cmdb.js';
import { loadOSTemplates } from './flow_os.js';
import { loadMiddlewareTemplates, loadMiddlewareInstances } from './flow_middleware.js';
import { loadK8sClusters } from './flow_k8s.js';
import { loadFlows } from './flow_workflow.js';
import { pollDeploys } from './flow_deploys.js';
import { pollAlertsFull } from './flow_alerts.js';
import { loadAudits } from './flow_audits.js';

// ---------- 标签切换 ----------
export function switchTab(name) {
  ['home', 'ops', 'cmdb', 'osopt', 'mwdep', 'k8s', 'deploy', 'flow', 'logs', 'alerts', 'users', 'roles', 'permission', 'audits', 'settings', 'docs'].forEach(function (t) {
    const p = document.getElementById('tab-' + t); if (p) p.classList.toggle('active', t === name);
    const b = document.getElementById('tab-' + t + '-btn'); if (b) b.classList.toggle('active', t === name);
  });
  if (name === 'ops') { pollTasks(); }
  if (name === 'cmdb') { loadCMDBTypes(); }
  if (name === 'osopt') { loadOSTemplates(); }
  if (name === 'mwdep') { loadMiddlewareTemplates(); loadMiddlewareInstances(); }
  if (name === 'k8s') { loadK8sClusters(); }
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

export function closeDrawer() { document.getElementById('drawer').classList.remove('open'); }