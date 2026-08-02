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
  usersPrevPage, usersNextPage, usersSetSearch, rolesPrevPage, rolesNextPage,
  paintUsersTable, paintRolesTable, paintPermsList,
  renderDeviceDetail, closeDeviceDetail,
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
} from './flow.js';
import { icon } from './icons.js';
import { initTheme, toggleTheme, getTheme } from './theme.js';
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
    'navIconPerms': 'permissions',
    // pane-intro
    'introIconHome': 'home', 'introIconOps': 'ops', 'introIconCmdb': 'cmdb',
    'introIconFlow': 'flow', 'introIconDeploy': 'deploy', 'introIconLogs': 'logs',
    'introIconAlerts': 'alerts', 'introIconUsers': 'users', 'introIconRoles': 'roles',
    'introIconPerms': 'permissions',
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
  // 用户菜单下拉
  setText('menuUsersBtn', t('nav.users'), true);
  setText('menuRolesBtn', t('nav.roles'), true);
  setText('menuPermsBtn', t('nav.permissions'), true);
  setText('menuLogoutBtn', t('topbar.logout'), true);
  // 用户中心页
  setText('usersTitle', t('users.title'));
  setText('usersDesc', t('users.desc'));
  setText('rolesTitle', t('roles.title'));
  setText('rolesDesc', t('roles.desc'));
  setText('permsTitle', t('perms.title'));
  setText('permsDesc', t('perms.desc'));
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

// ---------- 初始化主界面 ----------
function initMain() {
  loadAgents();
  // 立即拉一次首屏数据（SSE 仅推送增量，首屏仍需全量拉取）
  pollDevices(); pollTasks(); pollAlerts();
  paintStats();
  fetchMe();

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
  });

  // 检查登录状态
  if (api.isLoggedIn()) {
    // 已登录：验证 token 有效性
    api.apiAuthMe().then(function (r) {
      if (r.s === 200 && r.j && r.j.user) {
        enterApp(r.j.user);
      } else {
        // token 无效，清除并显示登录页
        api.apiLogout();
        showAuthPage();
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
