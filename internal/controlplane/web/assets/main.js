// main.js — 入口与模块装配
// 职责：初始化、事件绑定、启动轮询、装配各模块、主题/i18n/登录态管理。
// 作为入口被 index.html 以 <script type="module" src="/assets/main.js"> 引用。
//
// 兼容层说明：index.html 中保留了大量内联 onclick="xxx()" 调用，ES module 作用域内
// 的函数对内联事件不可见。此处将所有需被 HTML 内联调用的函数挂到 window 上，作为
// 模块化拆分与零功能改动的兼容层。模块间通信仍走 import/export，不通过 window。

import { startPolling, stopPolling, setAuxCallbacks, pollDevices, pollTasks, pollAlerts, startSSE, stopSSE, isSSEActive } from './poll.js';
import {
  paintStats, renderUsers, renderRoles, renderPermissions,
  mgmtState, showModal, closeModal, showUserModal, showRoleModal, setModalMsg,
  usersPrevPage, usersNextPage, usersSetSearch, paintUsersTable, paintRolesTable, paintPermsList,
  permsSetSearch, switchDocPanel,
  rolesPrevPage, rolesNextPage,
  renderDeviceDetail, closeDeviceDetail,
  renderPageNotes, renderSettings, renderDocs,
  renderDevices, state as renderState,
} from './render.js';
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
  loadAudits,
  loadOSTemplates, filterOSTemplates, showOSTemplateDetail, hideOSTemplateDetail,
  executeOSOptimize, closeOSExecModal, confirmOSExec,
} from './flow.js';
import { icon } from './icons.js';
import { initTheme, toggleTheme, getTheme, setTheme } from './theme.js';
import { initI18n, t, getLang, setLang, toggleLang } from './i18n.js';
import * as api from './api.js';

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
// 审计日志
w.loadAudits = loadAudits;
// OS 基础环境优化
w.loadOSTemplates = loadOSTemplates;
w.filterOSTemplates = filterOSTemplates;
w.showOSTemplateDetail = showOSTemplateDetail;
w.hideOSTemplateDetail = hideOSTemplateDetail;
w.executeOSOptimize = executeOSOptimize;
w.closeOSExecModal = closeOSExecModal;
w.confirmOSExec = confirmOSExec;

// ---------- 主题 / 语言 ----------
w.toggleTheme = toggleTheme;
w.toggleLang = toggleLang;

// ---------- 登录 / 注册 ----------
let authMode = 'login'; // 'login' or 'register'

// 切换登录/注册模式
w.switchAuthMode = function (mode) {
  authMode = mode;
  const emailField = document.getElementById('authEmailField');
  const submitBtn = document.getElementById('authSubmitBtn');
  const subtitle = document.getElementById('authSubtitle');
  const switchText = document.getElementById('authSwitchText');
  const switchLink = document.getElementById('authSwitchLink');
  const msg = document.getElementById('authMsg');
  if (mode === 'register') {
    emailField.style.display = '';
    submitBtn.textContent = t('register.submit');
    subtitle.textContent = t('register.subtitle');
    switchText.textContent = t('register.toLogin').split('去登录')[0] || t('register.toLogin');
    switchLink.textContent = t('login.title');
    switchLink.onclick = function () { switchAuthMode('login'); };
  } else {
    emailField.style.display = 'none';
    submitBtn.textContent = t('login.submit');
    subtitle.textContent = t('login.subtitle');
    switchText.textContent = t('login.toRegister').split('去注册')[0] || t('login.toRegister');
    switchLink.textContent = t('register.title');
    switchLink.onclick = function () { switchAuthMode('register'); };
  }
  if (msg) { msg.textContent = ''; msg.className = 'auth-msg'; }
};

// 提交登录/注册表单
w.submitAuth = async function () {
  const username = document.getElementById('authUsername').value.trim();
  const password = document.getElementById('authPassword').value;
  const email = document.getElementById('authEmail').value.trim();
  const msg = document.getElementById('authMsg');
  if (!username || !password) {
    msg.textContent = authMode === 'login' ? t('login.validation') : t('register.validation');
    msg.className = 'auth-msg err';
    return;
  }
  msg.textContent = t('common.loading');
  msg.className = 'auth-msg';
  try {
    let r;
    if (authMode === 'login') {
      r = await api.apiAuthLogin(username, password);
    } else {
      r = await api.apiAuthRegister(username, password, email || undefined);
    }
    // P1-7 注册安全：注册成功后区分两种情况：
    //   - demo 模式：返回 token，直接进入主界面（兼容旧行为）；
    //   - 非 demo 模式：返回 {message, userId, status}（无 token），显示待审批提示，不自动登录，切回登录页。
    if (authMode === 'register' && (r.s === 201) && r.j && !r.j.token && r.j.message) {
      msg.textContent = t('register.pending');
      msg.className = 'auth-msg ok';
      // 清空密码字段，切回登录模式（不自动登录）。
      document.getElementById('authPassword').value = '';
      setTimeout(function () { switchAuthMode('login'); }, 2000);
      return;
    }
    if ((r.s === 200 || r.s === 201) && r.j && r.j.token) {
      msg.textContent = authMode === 'register' ? t('register.success') : '';
      msg.className = 'auth-msg ok';
      // 进入主界面
      enterApp(r.j.user);
    } else {
      const errMsg = (r.j && (r.j.error || r.j.message)) || ('HTTP ' + r.s);
      msg.textContent = (authMode === 'login' ? t('login.error') : t('register.error')) + errMsg;
      msg.className = 'auth-msg err';
    }
  } catch (e) {
    msg.textContent = t('common.networkError');
    msg.className = 'auth-msg err';
    console.error('[auth]', e);
  }
};

// 显示登录页
function showAuthPage() {
  document.getElementById('authPage').style.display = '';
  document.getElementById('appMain').style.display = 'none';
  // 重置表单
  authMode = 'login';
  document.getElementById('authUsername').value = '';
  document.getElementById('authPassword').value = '';
  document.getElementById('authEmail').value = '';
  document.getElementById('authEmailField').style.display = 'none';
  // 应用 i18n
  applyAuthI18n();
}

// 应用登录页 i18n
function applyAuthI18n() {
  document.getElementById('authBrand').textContent = t('brand.name');
  document.getElementById('authUsernameLabel').textContent = t(authMode === 'login' ? 'login.username' : 'register.username');
  document.getElementById('authPasswordLabel').textContent = t(authMode === 'login' ? 'login.password' : 'register.password');
  document.getElementById('authEmailLabel').textContent = t('register.email');
  document.getElementById('authUsername').placeholder = t(authMode === 'login' ? 'login.usernamePlaceholder' : 'register.usernamePlaceholder');
  document.getElementById('authPassword').placeholder = t(authMode === 'login' ? 'login.passwordPlaceholder' : 'register.passwordPlaceholder');
  document.getElementById('authEmail').placeholder = t('register.emailPlaceholder');
  document.getElementById('authSubmitBtn').textContent = authMode === 'login' ? t('login.submit') : t('register.submit');
  document.getElementById('authSubtitle').textContent = authMode === 'login' ? t('login.subtitle') : t('register.subtitle');
  const switchLink = document.getElementById('authSwitchLink');
  const switchText = document.getElementById('authSwitchText');
  if (authMode === 'login') {
    switchText.textContent = t('login.toRegister').replace(/去注册.*$/, '').replace(/Register.*$/, '') || '';
    switchLink.textContent = t('register.title');
    switchLink.onclick = function () { switchAuthMode('register'); };
  } else {
    switchText.textContent = t('register.toLogin').replace(/去登录.*$/, '').replace(/Login.*$/, '') || '';
    switchLink.textContent = t('login.title');
    switchLink.onclick = function () { switchAuthMode('login'); };
  }
}

// 进入主界面
function enterApp(user) {
  document.getElementById('authPage').style.display = 'none';
  document.getElementById('appMain').style.display = '';
  // 更新用户菜单
  if (user) {
    document.getElementById('userMenuName').textContent = user.username || '–';
  }
  // 初始化主界面
  initMain();
}

// 退出登录
w.doLogout = function () {
  if (!confirm(t('user.logoutConfirm'))) return;
  api.apiLogout();
  // 停止轮询/SSE
  try { stopSSE(); } catch (_) {}
  try { stopPolling(); } catch (_) {}
  showAuthPage();
};

// 用户菜单切换
w.toggleUserMenu = function () {
  const dd = document.getElementById('userDropdown');
  dd.style.display = dd.style.display === 'none' ? '' : 'none';
};

// ---------- 用户中心 / 角色管理 / 权限管理 ----------
// 拉取用户列表
w.pollUsers = function () {
  const el = document.getElementById('usersTable');
  if (el) el.innerHTML = '<p class="muted">' + esc(t('users.loading')) + '</p>';
  // 确保角色已加载（用于显示用户角色名）
  const loadRoles = (!mgmtState.rolesCache || mgmtState.rolesCache.length === 0)
    ? api.apiListRoles().then(function (r) {
        if (r.s === 200 && r.j && r.j.roles) {
          mgmtState.rolesCache = r.j.roles;
        }
      }).catch(function () {})
    : Promise.resolve();
  loadRoles.then(function () {
    api.apiListUsers().then(function (r) {
      if (r.s === 200 && r.j && r.j.users) {
        renderUsers(r.j.users);
      } else {
        el.innerHTML = '<p class="muted">' + esc(t('common.error')) + (r.j && (r.j.error || r.j.message) || ('HTTP ' + r.s)) + '</p>';
      }
    }).catch(function (e) {
      el.innerHTML = '<p class="muted">' + esc(t('common.networkError')) + '</p>';
      console.error('[users]', e);
    });
  });
};

// 拉取角色列表
w.pollRoles = function () {
  const el = document.getElementById('rolesTable');
  if (el) el.innerHTML = '<p class="muted">' + esc(t('roles.loading')) + '</p>';
  api.apiListRoles().then(function (r) {
    if (r.s === 200 && r.j && r.j.roles) {
      renderRoles(r.j.roles);
    } else {
      el.innerHTML = '<p class="muted">' + esc(t('common.error')) + (r.j && (r.j.error || r.j.message) || ('HTTP ' + r.s)) + '</p>';
    }
  }).catch(function (e) {
    el.innerHTML = '<p class="muted">' + esc(t('common.networkError')) + '</p>';
    console.error('[roles]', e);
  });
};

// 拉取权限列表
w.pollPermissions = function () {
  const el = document.getElementById('permsList');
  if (el) el.innerHTML = '<p class="muted">' + esc(t('perms.loading')) + '</p>';
  api.apiListPermissions().then(function (r) {
    if (r.s === 200 && r.j && r.j.permissions) {
      renderPermissions(r.j.permissions);
    } else {
      el.innerHTML = '<p class="muted">' + esc(t('common.error')) + (r.j && (r.j.error || r.j.message) || ('HTTP ' + r.s)) + '</p>';
    }
  }).catch(function (e) {
    el.innerHTML = '<p class="muted">' + esc(t('common.networkError')) + '</p>';
    console.error('[perms]', e);
  });
};

// 打开新增用户弹窗
w.openAddUserModal = function () {
  // 确保角色已加载（用于复选框）
  if (!mgmtState.rolesCache || mgmtState.rolesCache.length === 0) {
    api.apiListRoles().then(function (r) {
      if (r.s === 200 && r.j && r.j.roles) {
        mgmtState.rolesCache = r.j.roles;
        showUserModal(null);
      } else {
        showUserModal(null);
      }
    }).catch(function () { showUserModal(null); });
  } else {
    showUserModal(null);
  }
};

// 编辑用户弹窗
w.editUser = function (id) {
  const u = (mgmtState.users.list || []).find(function (x) { return String(x.id) === String(id); });
  if (!u) return;
  // 确保角色已加载
  if (!mgmtState.rolesCache || mgmtState.rolesCache.length === 0) {
    api.apiListRoles().then(function (r) {
      if (r.s === 200 && r.j && r.j.roles) {
        mgmtState.rolesCache = r.j.roles;
        showUserModal(u);
      } else {
        showUserModal(u);
      }
    }).catch(function () { showUserModal(u); });
  } else {
    showUserModal(u);
  }
};

// 删除用户
w.deleteUser = async function (id) {
  if (!confirm(t('users.confirmDelete'))) return;
  try {
    const r = await api.apiDeleteUser(id);
    if (r.s === 204 || r.s === 200) {
      pollUsers();
    } else {
      alert(t('common.error') + (r.j && (r.j.error || r.j.message) || ('HTTP ' + r.s)));
    }
  } catch (e) {
    alert(t('common.networkError'));
    console.error('[deleteUser]', e);
  }
};

// 提交用户弹窗
w.submitUserModal = async function (id) {
  const isEdit = !!id;
  const username = document.getElementById('modalUsername') ? document.getElementById('modalUsername').value.trim() : '';
  const password = document.getElementById('modalPassword') ? document.getElementById('modalPassword').value : '';
  const email = document.getElementById('modalEmail') ? document.getElementById('modalEmail').value.trim() : '';
  // 收集选中的角色
  const roleEls = document.querySelectorAll('input[name="role_ids"]:checked');
  const roleIds = Array.prototype.map.call(roleEls, function (el) { return el.value; });
  if (!isEdit && (!username || !password)) {
    setModalMsg(t('register.validation'), false);
    return;
  }
  try {
    let r;
    if (isEdit) {
      const patch = { email: email || undefined, role_ids: roleIds };
      const statusEl = document.getElementById('modalStatus');
      if (statusEl) patch.status = statusEl.value;
      r = await api.apiUpdateUser(id, patch);
    } else {
      r = await api.apiCreateUser(username, password, email || undefined, roleIds);
    }
    if (r.s === 200 || r.s === 201) {
      setModalMsg(t('common.success'), true);
      setTimeout(function () { closeModal(); pollUsers(); }, 600);
    } else {
      setModalMsg(t('common.error') + (r.j && (r.j.error || r.j.message) || ('HTTP ' + r.s)), false);
    }
  } catch (e) {
    setModalMsg(t('common.networkError'), false);
    console.error('[submitUser]', e);
  }
};

// 打开新增角色弹窗
w.openAddRoleModal = function () {
  // 确保权限已加载
  if (!mgmtState.permsCache || mgmtState.permsCache.length === 0) {
    api.apiListPermissions().then(function (r) {
      if (r.s === 200 && r.j && r.j.permissions) {
        mgmtState.permsCache = r.j.permissions;
        showRoleModal(null);
      } else {
        showRoleModal(null);
      }
    }).catch(function () { showRoleModal(null); });
  } else {
    showRoleModal(null);
  }
};

// 编辑角色弹窗
w.editRole = function (id) {
  const r = (mgmtState.roles.list || []).find(function (x) { return String(x.id) === String(id); });
  if (!r) return;
  // 确保权限已加载
  if (!mgmtState.permsCache || mgmtState.permsCache.length === 0) {
    api.apiListPermissions().then(function (res) {
      if (res.s === 200 && res.j && res.j.permissions) {
        mgmtState.permsCache = res.j.permissions;
        showRoleModal(r);
      } else {
        showRoleModal(r);
      }
    }).catch(function () { showRoleModal(r); });
  } else {
    showRoleModal(r);
  }
};

// 删除角色
w.deleteRole = async function (id) {
  if (!confirm(t('roles.confirmDelete'))) return;
  try {
    const r = await api.apiDeleteRole(id);
    if (r.s === 204 || r.s === 200) {
      pollRoles();
    } else {
      alert(t('common.error') + (r.j && (r.j.error || r.j.message) || ('HTTP ' + r.s)));
    }
  } catch (e) {
    alert(t('common.networkError'));
    console.error('[deleteRole]', e);
  }
};

// 提交角色弹窗
w.submitRoleModal = async function (id) {
  const isEdit = !!id;
  const name = document.getElementById('modalRoleName') ? document.getElementById('modalRoleName').value.trim() : '';
  const desc = document.getElementById('modalRoleDesc') ? document.getElementById('modalRoleDesc').value.trim() : '';
  const permEls = document.querySelectorAll('input[name="perm_ids"]:checked');
  const permIds = Array.prototype.map.call(permEls, function (el) { return el.value; });
  if (!isEdit && !name) {
    setModalMsg(t('register.validation'), false);
    return;
  }
  try {
    let r;
    if (isEdit) {
      r = await api.apiUpdateRole(id, { description: desc, permissions: permIds });
    } else {
      r = await api.apiCreateRole(name, desc, permIds);
    }
    if (r.s === 200 || r.s === 201) {
      setModalMsg(t('common.success'), true);
      setTimeout(function () { closeModal(); pollRoles(); }, 600);
    } else {
      setModalMsg(t('common.error') + (r.j && (r.j.error || r.j.message) || ('HTTP ' + r.s)), false);
    }
  } catch (e) {
    setModalMsg(t('common.networkError'), false);
    console.error('[submitRole]', e);
  }
};

// 弹窗关闭
w.closeModal = closeModal;

// ---------- 系统设置 / 文档 ----------
w.pollSettings = function () { renderSettings(); };
w.pollDocs = function () { renderDocs(); };

// 保存系统设置（前端持久化到 localStorage）
w.saveSettings = function () {
  // 主题
  const themeBtns = document.querySelectorAll('#themeToggleGroup button');
  let theme = 'light';
  themeBtns.forEach(function (b) { if (b.classList.contains('active')) theme = b.getAttribute('data-val'); });
  if (theme === 'dark') {
    document.documentElement.setAttribute('data-theme', 'dark');
    localStorage.setItem('opsmesh-theme', 'dark');
  } else {
    document.documentElement.removeAttribute('data-theme');
    localStorage.setItem('opsmesh-theme', 'light');
  }
  // 语言
  const langBtns = document.querySelectorAll('#langToggleGroup button');
  let lang = 'zh';
  langBtns.forEach(function (b) { if (b.classList.contains('active')) lang = b.getAttribute('data-val'); });
  setLang(lang);
  // 通知 / 告警 / 任务
  const val = function (id) { const e = document.getElementById(id); return e ? e.value : ''; };
  localStorage.setItem('opsmesh-notify-email', val('setNotifyEmail'));
  localStorage.setItem('opsmesh-notify-webhook', val('setWebhookUrl'));
  localStorage.setItem('opsmesh-alert-critical', val('setAlertCritical'));
  localStorage.setItem('opsmesh-alert-warning', val('setAlertWarning'));
  localStorage.setItem('opsmesh-alert-silence', val('setAlertSilence'));
  localStorage.setItem('opsmesh-task-timeout', val('setTaskTimeout'));
  localStorage.setItem('opsmesh-task-retry', val('setTaskRetry'));
  localStorage.setItem('opsmesh-task-concurrency', val('setTaskConc'));
  const msg = document.getElementById('settingsMsg');
  if (msg) { msg.textContent = t('settings.saved'); msg.className = 'settings-msg ok'; }
};

// 主题/语言切换按钮组点击
document.addEventListener('click', function (e) {
  const tg = e.target.closest('#themeToggleGroup button');
  if (tg) {
    tg.parentNode.querySelectorAll('button').forEach(function (b) { b.classList.remove('active'); });
    tg.classList.add('active');
    // 立即应用主题切换
    const val = tg.getAttribute('data-val');
    if (val === 'dark') {
      setTheme('dark');
    } else {
      setTheme('light');
    }
    return;
  }
  const lg = e.target.closest('#langToggleGroup button');
  if (lg) {
    lg.parentNode.querySelectorAll('button').forEach(function (b) { b.classList.remove('active'); });
    lg.classList.add('active');
    // 立即应用语言切换
    const lang = lg.getAttribute('data-val');
    setLang(lang);
    return;
  }
});

// ---------- 设备详情 · 监控指标弹窗 ----------
// showDeviceDetail：调用 API 获取设备监控指标 → 渲染详情 → 显示弹窗
w.showDeviceDetail = async function (deviceID) {
  const body = document.getElementById('deviceDetailBody');
  const modal = document.getElementById('deviceDetailModal');
  // 先显示弹窗 + loading 态
  if (body) body.innerHTML = '<p class="muted">加载监控指标中…</p>';
  if (modal) modal.classList.add('open');
  try {
    const r = await api.apiDeviceMetrics(deviceID);
    if (r.s === 200 && r.j) {
      renderDeviceDetail(r.j);
    } else {
      const errMsg = (r.j && (r.j.error || r.j.message)) || ('HTTP ' + r.s);
      if (body) body.innerHTML = '<p class="muted">无法获取监控指标：' + esc(errMsg) + '</p>';
    }
  } catch (e) {
    console.error('[deviceDetail]', e);
    if (body) body.innerHTML = '<p class="muted">获取监控指标失败：' + esc(e.message || String(e)) + '</p>';
  }
};
// closeDeviceDetail：关闭设备详情弹窗
w.closeDeviceDetail = closeDeviceDetail;

// 分页
w.usersPrevPage = usersPrevPage;
w.usersNextPage = usersNextPage;
w.usersSetSearch = usersSetSearch;
w.rolesPrevPage = rolesPrevPage;
w.rolesNextPage = rolesNextPage;
w.permsSetSearch = permsSetSearch;
w.switchDocPanel = switchDocPanel;

// ---------- 简易 esc（避免在 main.js 中 import render.js 的 esc 循环） ----------
function esc(s) {
  return (s == null ? '' : String(s)).replace(/[&<>"']/g, function (c) {
    return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
  });
}

// ---------- 注入辅助轮询回调（poll.js 不反向依赖 flow.js） ----------
setAuxCallbacks({ pollAlertsFull: pollAlertsFull, pollDeploys: pollDeploys });

// ---------- 初始化静态图标 ----------
// 在 DOM 上预填所有图标（避免每个图标都写 JS）
function initStaticIcons() {
  const map = {
    // 导航
    'navIconHome': 'home', 'navIconOps': 'ops', 'navIconCmdb': 'cmdb',
    'navIconDeploy': 'deploy', 'navIconFlow': 'flow', 'navIconLogs': 'logs',
    'navIconAlerts': 'alerts', 'navIconUsers': 'users', 'navIconRoles': 'roles',
    'navIconPerms': 'permissions', 'navIconAudits': 'audit', 'navIconSettings': 'settings', 'navIconDocs': 'info',
    'navIconOsOpt': 'osopt',
    // pane-intro
    'introIconHome': 'home', 'introIconOps': 'ops', 'introIconCmdb': 'cmdb',
    'introIconFlow': 'flow', 'introIconDeploy': 'deploy', 'introIconLogs': 'logs',
    'introIconAlerts': 'alerts', 'introIconUsers': 'users', 'introIconRoles': 'roles',
    'introIconPerms': 'permissions', 'introIconAudits': 'audit', 'introIconSettings': 'settings', 'introIconDocs': 'info',
    'introIconOsOpt': 'osopt',
    // 上下文
    'ctxIcon': 'context',
    // 按钮
    'iconDispatch': 'deploy', 'iconWfNew': 'add', 'iconWfDemo': 'task', 'iconWfSave': 'success',
    'iconWfRun': 'refresh', 'iconWfSchedule': 'settings', 'iconAddNode': 'add',
    'iconLinkHint': 'link', 'iconLink': 'link', 'iconAutoLayout': 'flow',
    'iconUndo': 'edit', 'iconRedo': 'edit',
    'iconDeployAdd': 'add',
    'iconLogSearch': 'search',
    'iconAlertTip': 'info',
    // 用户中心工具栏
    'iconUsersSearch': 'search', 'iconUsersAdd': 'add', 'iconUsersRefresh': 'refresh',
    'iconRolesAdd': 'add', 'iconRolesRefresh': 'refresh',
    // 用户菜单
    'userMenuIcon': 'user', 'userMenuArrow': 'menu',
    // 运维能力方向（总览页 6 大能力卡片）
    'capIconDevice': 'device', 'capIconTask': 'task', 'capIconCmdb': 'cmdb',
    'capIconService': 'settings', 'capIconFile': 'deploy', 'capIconMonitor': 'alerts',
  };
  Object.keys(map).forEach(function (id) {
    const el = document.getElementById(id);
    if (el) el.innerHTML = icon(map[id], 16);
  });
  // 用户菜单图标稍大
  const umi = document.getElementById('userMenuIcon');
  if (umi) umi.innerHTML = icon('user', 18);
}

// ---------- 应用 i18n 到静态 DOM ----------
function applyI18nToDOM() {
  // 顶栏
  setText('brandSub', t('brand.sub'));
  setText('statusOkTxt', t('topbar.statusOk'));
  setText('helpChip', t('topbar.help'));
  // 导航分组
  setText('navGroupOverview', t('nav.group.overview'));
  setText('navGroupOps', t('nav.group.ops'));
  setText('navGroupDelivery', t('nav.group.delivery'));
  setText('navGroupObservability', t('nav.group.observability'));
  setText('navGroupSystem', t('nav.group.system'));
  // 导航项
  setText('tab-home-btn', t('nav.home'), true);
  setText('tab-ops-btn', t('nav.ops'), true);
  setText('tab-cmdb-btn', t('nav.cmdb'), true);
  setText('tab-deploy-btn', t('nav.deploy'), true);
  setText('tab-flow-btn', t('nav.flow'), true);
  setText('tab-logs-btn', t('nav.logs'), true);
  setText('tab-alerts-btn', t('nav.alerts'), true);
  setText('tab-users-btn', t('nav.users'), true);
  setText('tab-roles-btn', t('nav.roles'), true);
  setText('tab-permissions-btn', t('nav.permissions'), true);
  setText('tab-audits-btn', t('nav.audits'), true);
  setText('tab-settings-btn', t('nav.settings'), true);
  setText('tab-docs-btn', t('nav.docs'), true);
  setText('navGroupHelp', t('nav.group.help'));
  // 用户菜单下拉
  setText('menuUsersBtn', t('nav.users'), true);
  setText('menuRolesBtn', t('nav.roles'), true);
  setText('menuPermsBtn', t('nav.permissions'), true);
  setText('menuSettingsBtn', t('nav.settings'), true);
  setText('menuLogoutBtn', t('topbar.logout'), true);
  // 上下文条
  setText('ctxTitle', t('ctx.title'));
  setText('ctxDeviceLabel', t('ctx.device'));
  setText('ctxOps', t('ctx.ops'));
  setText('ctxCmdb', t('ctx.cmdb'));
  setText('ctxDeploy', t('ctx.deploy'));
  setText('ctxLogs', t('ctx.logs'));
  setText('ctxAlerts', t('ctx.alerts'));
  setText('ctxClear', t('ctx.clearBtn'));
  // 用户中心页
  setText('usersTitle', t('users.title'));
  setText('usersDesc', t('users.desc'));
  setText('rolesTitle', t('roles.title'));
  setText('rolesDesc', t('roles.desc'));
  setText('permsTitle', t('perms.title'));
  setText('permsDesc', t('perms.desc'));
  // 审计日志页
  setText('auditsTitle', t('audits.title'));
  setText('auditsDesc', t('audits.desc'));
  setText('auditActionLabelText', t('audits.action'));
  setText('auditFromLabelText', t('audits.from'));
  setText('auditToLabelText', t('audits.to'));
  setText('auditLimitLabelText', t('audits.limit'));
  setText('auditSearchBtn', t('audits.search'));
  // OS 基础环境优化页
  setText('tab-osopt-btn', t('nav.osopt'), true);
  setHTML('osoptTitle', t('osopt.title'));
  setHTML('osoptDesc', t('osopt.desc'));
  setText('osoptCatAll', t('osopt.category.all'));
  setText('osoptCatKernel', t('osopt.category.kernel'));
  setText('osoptCatNetwork', t('osopt.category.network'));
  setText('osoptCatSecurity', t('osopt.category.security'));
  setText('osoptCatTime', t('osopt.category.time'));
  setText('osoptCatSsh', t('osopt.category.ssh'));
  setText('osoptCatDisk', t('osopt.category.disk'));
  setText('osoptCatSystem', t('osopt.category.system'));
  setText('osoptCatUser', t('osopt.category.user'));
  setText('osoptRefreshBtn', t('osopt.refresh'));
  setText('osExecTitle', t('osopt.execTitle'));
  setText('osExecHint', t('osopt.execHint'));
  setText('osExecAgentLabel', t('osopt.selectAgent'));
  setText('osExecParamsLabel', t('osopt.params'));
  setText('osExecCancelBtn', t('osopt.cancel'));
  setText('osExecConfirmBtn', t('osopt.confirm'));
  // 系统设置 / 文档
  setText('settingsTitle', t('settings.title'));
  setText('settingsDesc', t('settings.desc'));
  setText('docsTitle', t('docs.title'));
  setText('docsDesc', t('docs.desc'));
  // 设备详情弹窗
  setText('deviceDetailTitle', t('device.detail.title'));
  // pane-intro 标题与描述（含 HTML 标签，用 innerHTML）
  setHTML('introHomeTitle', t('home.title'));
  setHTML('introHomeDesc', t('home.desc'));
  setText('introHomeHowToUse', t('home.howToUse'));
  setHTML('introOpsTitle', t('ops.title'));
  setHTML('introOpsDesc', t('ops.desc'));
  setText('introOpsHowToUse', t('home.howToUse'));
  setHTML('introCmdbTitle', t('cmdb.title'));
  setHTML('introCmdbDesc', t('cmdb.desc'));
  setHTML('introFlowTitle', t('flow.title'));
  setHTML('introFlowDesc', t('flow.desc'));
  setText('introFlowLoadDemo', t('flow.loadDemo'));
  setHTML('introDeployTitle', t('deploy.title'));
  setHTML('introDeployDesc', t('deploy.desc'));
  setText('introDeployLoadDemo', t('flow.loadDemo'));
  setHTML('introLogsTitle', t('logs.title'));
  setHTML('introLogsDesc', t('logs.desc'));
  setHTML('introAlertsTitle', t('alerts.title'));
  setHTML('introAlertsDesc', t('alerts.desc'));
  setText('introAlertsRefresh', t('cmdb.refresh'));
  // stats 卡片标签
  setText('ovDevicesLabel', t('home.stat.devices'));
  setText('ovCIsLabel', t('home.stat.cis'));
  setText('ovTasksLabel', t('home.stat.tasks'));
  setText('ovAlertsLabel', t('home.stat.alerts'));
  setText('statDevicesLabel', t('ops.stat.devices'));
  setText('statManagedLabel', t('ops.stat.managed'));
  setText('statTasksLabel', t('ops.stat.tasks'));
  setText('statAlertsLabel', t('ops.stat.alerts'));
  setText('statCriticalLabel', t('alerts.stat.critical'));
  setText('statWarningLabel', t('alerts.stat.warning'));
  setText('statTotalAlertsLabel', t('alerts.stat.total'));
  setText('statMonPortLabel', t('alerts.stat.port'));
  // home 运维能力方向
  setText('homeCapsTitle', t('home.capsTitle'));
  setText('homeCapsHint', t('home.capsHint'));
  setText('capDeviceName', t('home.cap.device'));
  setText('capDeviceDesc', t('home.cap.device.desc'));
  setText('capTaskName', t('home.cap.task'));
  setText('capTaskDesc', t('home.cap.task.desc'));
  setText('capCmdbName', t('home.cap.cmdb'));
  setText('capCmdbDesc', t('home.cap.cmdb.desc'));
  setText('capServiceName', t('home.cap.service'));
  setText('capServiceDesc', t('home.cap.service.desc'));
  setText('capFileName', t('home.cap.file'));
  setText('capFileDesc', t('home.cap.file.desc'));
  setText('capMonitorName', t('home.cap.monitor'));
  setText('capMonitorDesc', t('home.cap.monitor.desc'));
  // home card 标题与描述
  setText('homeQuickEntry', t('home.quickEntry'));
  setText('homeQuickEntryHint', t('home.quickEntryHint'));
  setText('homeQuickOps', t('nav.ops'));
  setText('homeQuickCmdb', t('nav.cmdb'));
  setText('homeQuickDeploy', t('nav.deploy'));
  setText('homeQuickFlow', t('nav.flow'));
  setText('homeQuickLogs', t('nav.logs'));
  setText('homeQuickAlerts', t('nav.alerts'));
  setText('homeDataSource', t('home.dataSource'));
  setText('homeDataSourceDesc', t('home.dataSourceDesc'));
  setText('homeTaskHealth', t('home.taskHealth'));
  setText('homeTaskHealthHint', t('home.taskHealthHint'));
  setText('homeTrend', t('home.trend'));
  setText('homeTrendHint', t('home.trendHint'));
  setText('homeAlertLevel', t('home.alertLevel'));
  setText('homeAlertLevelHint', t('home.alertLevelHint'));
  setText('homeResType', t('home.resType'));
  setText('homeResTypeHint', t('home.resTypeHint'));
  setText('homeTopo', t('home.topo'));
  setText('homeTopoHint', t('home.topoHint'));
  // ops 页面 card
  setText('opsDispatchTitle', t('ops.dispatch'));
  setText('opsDispatchHint', t('ops.dispatchHint'));
  setText('opsAgentLabelText', t('ops.agentLabel'));
  setText('opsTypeLabelText', t('ops.typeLabel'));
  setText('opsCommandLabelText', t('ops.commandLabel'));
  setText('opsPathLabelText', t('ops.pathLabel'));
  setText('opsContentLabelText', t('ops.contentLabel'));
  setText('opsDispatchBtn', t('ops.dispatchBtn'));
  setText('opsNetworkTitle', t('ops.network'));
  setText('opsNetworkHint', t('ops.networkHint'));
  setText('opsTasksTitle', t('ops.tasks'));
  setText('opsStatusFilterLabelText', t('ops.statusFilterLabel'));
  setText('opsActiveAlertsTitle', t('ops.activeAlerts'));
  setText('opsActiveAlertsHint', t('ops.activeAlertsHint'));
  // cmdb 页面 card
  setText('cmdbCiInstancesTitle', t('cmdb.ciInstances'));
  setText('cmdbCiTypeLabelText', t('cmdb.ciTypeLabel'));
  setText('cmdbRefreshBtn1', t('cmdb.refresh'));
  setText('cmdbAttrTemplateTitle', t('cmdb.attrTemplate'));
  setText('cmdbTmplTypeLabelText', t('cmdb.ciTypeLabel'));
  setText('cmdbRefreshBtn2', t('cmdb.refresh'));
  setText('cmdbNewCITitle', t('cmdb.newCI'));
  setHTML('cmdbNewCIHint', t('cmdb.newCIHint'));
  setText('cmdbNewCITypeLabelText', t('cmdb.newCITypeLabel'));
  setText('cmdbNewCINameLabelText', t('cmdb.newCINameLabel'));
  setText('cmdbNewCIAttrsLabelText', t('cmdb.newCIAttrsLabel'));
  setText('cmdbCreateBtn', t('cmdb.create'));
  setText('cmdbCiGraphTitle', t('cmdb.ciGraph'));
  setText('cmdbCiGraphHint', t('cmdb.ciGraphHint'));
  // flow 页面 card
  setText('flowWorkflowTitle', t('flow.workflow'));
  setText('flowWorkflowHint', t('flow.workflowHint'));
  setText('flowSelectLabelText', t('flow.selectLabel'));
  setText('flowNameLabelText', t('flow.nameLabel'));
  setText('flowAgentLabelText', t('flow.agentLabel'));
  setText('flowCronLabelText', t('flow.cronLabel'));
  setText('flowNewBtn', t('flow.new'));
  setText('flowDemoBtn', t('flow.demo'));
  setText('flowSaveBtn', t('flow.save'));
  setText('flowRunBtn', t('flow.run'));
  setText('flowScheduleBtn', t('flow.schedule'));
  setText('flowNodesTitle', t('flow.nodes'));
  setText('flowAddNodeBtn', t('flow.addNode'));
  setText('flowCanvasTitle', t('flow.canvas'));
  setText('flowCanvasHint', t('flow.canvasHint'));
  setText('flowLinkBtn', t('flow.link'));
  setText('flowAutoLayoutBtn', t('flow.autoLayout'));
  setText('flowUndoBtn', t('flow.undo'));
  setText('flowRedoBtn', t('flow.redo'));
  setText('flowFitBtn', t('flow.fit'));
  setText('flowResetBtn', t('flow.reset'));
  setText('flowAlignSelLabel', t('flow.alignSel'));
  setText('flowAlignLeftBtn', t('flow.alignLeft'));
  setText('flowAlignRightBtn', t('flow.alignRight'));
  setText('flowAlignTopBtn', t('flow.alignTop'));
  setText('flowAlignBottomBtn', t('flow.alignBottom'));
  // deploy 页面 card
  setText('deployRegisterTitle', t('deploy.register'));
  setHTML('deployRegisterHint', t('deploy.registerHint'));
  setText('deployNameLabelText', t('deploy.nameLabel'));
  setText('deployTypeLabelText', t('deploy.typeLabel'));
  setText('deployRepoLabelText', t('deploy.repoLabel'));
  setText('deployContentLabelText', t('deploy.contentLabel'));
  setText('deployPathLabelText', t('deploy.pathLabel'));
  setText('deployTargetsLabelText', t('deploy.targetsLabel'));
  setText('deployAddBtn', t('deploy.addBtn'));
  setText('deployListTitle', t('deploy.list'));
  setText('deployListHint', t('deploy.listHint'));
  setText('deployStatusFilterLabelText', t('deploy.statusFilterLabel'));
  setText('deployRefreshBtn', t('cmdb.refresh'));
  // logs 页面 card
  setText('logsQueryTitle', t('logs.query'));
  setHTML('logsQueryHint', t('logs.queryHint'));
  setText('logsDeviceLabelText', t('logs.deviceLabel'));
  setText('logsAgentLabelText', t('logs.agentLabel'));
  setText('logsLevelLabelText', t('logs.levelLabel'));
  setText('logsSourceLabelText', t('logs.sourceLabel'));
  setText('logsKeywordLabelText', t('logs.keywordLabel'));
  setText('logsFromLabelText', t('logs.fromLabel'));
  setText('logsToLabelText', t('logs.toLabel'));
  setText('logsLimitLabelText', t('logs.limitLabel'));
  setText('logsSearchBtn', t('logs.searchBtn'));
  setText('logsResetBtn', t('logs.resetBtn'));
  setText('logsResultTitle', t('logs.result'));
  setHTML('logsResultHint', t('logs.resultHint'));
  setText('logsPrevBtn', t('logs.prevBtn'));
  setText('logsNextBtn', t('logs.nextBtn'));
  // alerts 页面 card
  setText('alertsActiveTitle', t('alerts.active'));
  setText('alertsRefreshBtn', t('alerts.refreshBtn'));
  setText('alertsActiveHint', t('alerts.activeHint'));
  setText('alertsMetricsTitle', t('alerts.metrics'));
  setText('alertsMetricsHint', t('alerts.metricsHint'));
  setText('alertsMetricsNote', t('alerts.metricsNote'));
  setHTML('alertsMetricsTip', t('alerts.metricsTip'));
  // users / roles 按钮
  setText('usersAddBtn', t('users.addBtn'));
  setText('usersRefreshBtn', t('users.refreshBtn'));
  setText('rolesAddBtn', t('roles.addBtn'));
  setText('rolesRefreshBtn', t('roles.refreshBtn'));
  // 搜索框 placeholder
  const usi = document.getElementById('usersSearchInput');
  if (usi) usi.placeholder = t('users.searchPlaceholder');
  // 页脚
  setText('footerRunning', t('footer.running'));
  setText('footerApi', t('footer.api'));
  // 登录页（若可见）
  applyAuthI18n();
}

// setText: 设置元素文本；keepIcon=true 时保留第一个 .icon 子元素
function setText(id, text, keepIcon) {
  const el = document.getElementById(id);
  if (!el) return;
  if (keepIcon) {
    // 保留 .icon 子元素，替换后续文本节点
    const iconEl = el.querySelector('.icon');
    el.innerHTML = '';
    if (iconEl) el.appendChild(iconEl);
    el.appendChild(document.createTextNode(' ' + text));
  } else {
    el.textContent = text;
  }
}

// setHTML: 设置元素 innerHTML（用于含 <b> 等标签的翻译文本）
function setHTML(id, html) {
  const el = document.getElementById(id);
  if (el) el.innerHTML = html;
}

// ---------- 初始化主界面 ----------
function initMain() {
  loadAgents();
  // 立即拉一次首屏数据（SSE 仅推送增量，首屏仍需全量拉取）
  pollDevices(); pollTasks(); pollAlerts();
  paintStats();
  fetchMe();

  // 渲染各页面底部说明
  renderPageNotes();

  // M3-2B：优先启动 SSE 实时推送，失败自动降级回退轮询（startSSE 内部处理降级）。
  startSSE();

  // ---------- 事件绑定 ----------
  const sf = document.getElementById('statusFilter');
  if (sf) sf.addEventListener('change', pollTasks);

  const tf = document.getElementById('taskForm');
  if (tf) tf.addEventListener('submit', function (e) { e.preventDefault(); submitTaskForm(); });

  const cf = document.getElementById('ciForm');
  if (cf) cf.addEventListener('submit', function (e) { e.preventDefault(); submitCIForm(); });

  const df = document.getElementById('deployForm');
  if (df) df.addEventListener('submit', function (e) { e.preventDefault(); submitDeployForm(); });

  const cv = document.getElementById('canvas');
  if (cv) {
    cv.addEventListener('mousedown', svgMouseDown);
    cv.addEventListener('wheel', svgWheel, { passive: false });
  }

  document.addEventListener('keydown', flowKey);

  // 点击外部关闭用户下拉
  document.addEventListener('click', function (e) {
    const dd = document.getElementById('userDropdown');
    const btn = document.getElementById('userMenuBtn');
    if (dd && dd.style.display !== 'none' && btn && !btn.contains(e.target) && !dd.contains(e.target)) {
      dd.style.display = 'none';
    }
  });
}

// ---------- 初始化 ----------
function init() {
  // 初始化主题与语言（尽早，避免闪烁）
  initTheme();
  initI18n();
  // 填充静态图标
  initStaticIcons();
  // 应用 i18n 到静态 DOM
  applyI18nToDOM();
  // 监听语言切换事件，重渲染静态文本
  document.addEventListener('langchange', function () {
    applyI18nToDOM();
    // 若用户/角色/权限表已加载，重渲染（表格内文字也需 i18n）
    if (mgmtState.users.list.length) paintUsersTable();
    if (mgmtState.roles.list.length) paintRolesTable();
    if (mgmtState.perms.list.length) paintPermsList();
    // 重渲染设备列表（"网段 X（N 台设备）"等文本需 i18n）
    if (renderState && renderState.lastDevices && Object.keys(renderState.lastDevices).length) {
      renderDevices(renderState.lastDevices);
    }
    // 重渲染页面底部说明 / 系统*设置 / 文档（若已渲染）
    renderPageNotes();
    if (document.getElementById('settingsBody') && document.getElementById('settingsBody').innerHTML) {
      renderSettings();
    }
    if (document.getElementById('docsBody') && document.getElementById('docsBody').innerHTML) {
      renderDocs();
    }
  });

  // 检查登录状态
  if (api.isLoggedIn()) {
    // 已登录：验证 token 有效性
    api.apiAuthMe().then(function (r) {
      if (r.s === 200 && r.j && r.j.user) {
        enterApp(r.j.user);
      } else if (r.s === 401) {
        // token 已过期：显示提示，让用户手动决定是否重新登录
        // 不立即清除 token，避免误判（可能后端暂时性故障）
        alert('登录已失效，请重新登录');
        api.apiLogout();
        showAuthPage();
      } else {
        // 其他非 200 状态：仍尝试进入主界面（可能后端暂未启用认证或暂时性故障）
        enterApp(null);
      }
    }).catch(function () {
      // 网络异常：仍尝试进入主界面（可能后端暂未启用认证）
      enterApp(null);
    });
  } else {
    // 未登录：显示登录页
    showAuthPage();
  }
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
