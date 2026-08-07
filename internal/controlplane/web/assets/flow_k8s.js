// flow_k8s.js — K8s 管理（Phase 3）
// 从 flow.js 拆分（P2-1）。职责：集群 CRUD、资源列表（namespace/pod/deployment/service/configmap/secret/node）、
//       pod 日志查看、pod 删除、deployment 扩缩容/重启。
// 依赖：api.js、render.js（esc/escAttr）、icons.js、i18n.js、flow_tab.js（switchTab）。

import * as api from './api.js';
import { esc, escAttr } from './render.js';
import { icon } from './icons.js';
import { t } from './i18n.js';
import { switchTab } from './flow_tab.js';

// ---------- K8s 模块内部状态 ----------
// 当前选中的集群 ID（资源管理用）
let k8sCurrentClusterID = '';
// 当前资源类型 tab（pods / deployments / services / configmaps / secrets / nodes）
let k8sCurrentResource = 'pods';
// 当前 namespace（资源列表过滤用，空字符串表示全部）
let k8sCurrentNamespace = '';

// ---------- K8s 集群管理 ----------
// 加载集群列表并渲染到 #k8sClusterList
export function loadK8sClusters() {
  const el = document.getElementById('k8sClusterList');
  if (el) el.innerHTML = '<p class="muted">' + esc(t('common.loading')) + '</p>';
  api.getK8sClusters().then(function (data) {
    const listEl = document.getElementById('k8sClusterList');
    if (!listEl) return;
    // 兼容 {clusters: []} 或 [] 两种返回形态
    let list = [];
    if (Array.isArray(data)) list = data;
    else if (data && Array.isArray(data.clusters)) list = data.clusters;
    // 同步填充资源管理区域的集群下拉
    const sel = document.getElementById('k8sClusterSelect');
    if (sel) {
      const curVal = k8sCurrentClusterID || sel.value || '';
      sel.innerHTML = '<option value="">— ' + esc(t('k8s.selectCluster')) + ' —</option>';
      list.forEach(function (c) {
        const o = document.createElement('option');
        o.value = c.id || '';
        o.textContent = (c.name || c.id || '') + (c.server ? (' (' + c.server + ')') : '');
        sel.appendChild(o);
      });
      sel.value = curVal;
    }
    if (!list.length) {
      listEl.innerHTML = '<p class="muted">' + esc(t('k8s.noClusterSelected')) + '</p>';
      return;
    }
    let html = '<div class="table-wrap"><table><colgroup>'
      + '<col style="width:18%"><col style="width:24%"><col style="width:14%"><col style="width:18%"><col style="width:26%">'
      + '</colgroup><thead><tr>'
      + '<th>' + esc(t('k8s.clusterName')) + '</th>'
      + '<th>' + esc(t('k8s.server')) + '</th>'
      + '<th>' + esc(t('k8s.status')) + '</th>'
      + '<th>' + esc(t('k8s.createdAt')) + '</th>'
      + '<th>' + esc(t('k8s.action')) + '</th>'
      + '</tr></thead><tbody>';
    list.forEach(function (c) {
      const cid = esc(c.id || '');
      const statusTxt = c.status === 'online' ? t('k8s.online') : (c.status === 'offline' ? t('k8s.offline') : esc(c.status || '-'));
      const statusCls = c.status === 'online' ? 'badge ok' : (c.status === 'offline' ? 'badge fail' : 'badge');
      html += '<tr>'
        + '<td><b>' + esc(c.name || '') + '</b><br><code style="font-size:11px;color:var(--text-3)">' + cid + '</code></td>'
        + '<td><code>' + esc(c.server || '-') + '</code></td>'
        + '<td><span class="' + statusCls + '">' + statusTxt + '</span></td>'
        + '<td>' + esc(c.createdAt || '-') + '</td>'
        + '<td>'
        + '<button class="btn btn-sm" onclick="testK8sClusterConnection(\'' + escAttr(c.id || '') + '\')" style="margin-right:6px">' + icon('link', 12) + ' ' + esc(t('k8s.test')) + '</button>'
        + '<button class="btn btn-primary btn-sm" onclick="loadK8sResources(\'' + escAttr(c.id || '') + '\')" style="margin-right:6px">' + icon('search', 12) + ' ' + esc(t('k8s.resources')) + '</button>'
        + '<button class="btn btn-sm" style="color:var(--fail)" onclick="deleteK8sClusterConfirm(\'' + escAttr(c.id || '') + '\')">' + icon('delete', 12) + ' ' + esc(t('k8s.delete')) + '</button>'
        + '</td>'
        + '</tr>';
    });
    html += '</tbody></table></div>';
    listEl.innerHTML = html;
  }).catch(function (e) {
    console.error('[k8s-clusters]', e);
    const listEl = document.getElementById('k8sClusterList');
    if (listEl) listEl.innerHTML = '<p class="muted">' + esc(t('common.networkError')) + '</p>';
  });
}

// 打开添加集群对话框
export function showAddK8sClusterModal() {
  const modal = document.getElementById('k8sAddClusterModal');
  if (!modal) return;
  // 重置表单
  const nameI = document.getElementById('k8sAddClusterName'); if (nameI) nameI.value = '';
  const serverI = document.getElementById('k8sAddClusterServer'); if (serverI) serverI.value = '';
  const cfgI = document.getElementById('k8sAddClusterKubeconfig'); if (cfgI) cfgI.value = '';
  const msg = document.getElementById('k8sAddClusterMsg');
  if (msg) { msg.textContent = ''; msg.className = 'msg'; }
  modal.classList.add('open');
}

// 关闭添加集群对话框
export function closeK8sAddClusterModal() {
  const modal = document.getElementById('k8sAddClusterModal');
  if (modal) modal.classList.remove('open');
}

// 确认添加集群
export function confirmAddK8sCluster() {
  const name = (document.getElementById('k8sAddClusterName').value || '').trim();
  const server = (document.getElementById('k8sAddClusterServer').value || '').trim();
  const kubeconfig = document.getElementById('k8sAddClusterKubeconfig').value || '';
  const msg = document.getElementById('k8sAddClusterMsg');
  if (!name || !server || !kubeconfig) {
    if (msg) { msg.textContent = t('k8s.addClusterFail') + ': ' + t('param.required'); msg.className = 'msg err'; }
    return;
  }
  if (msg) { msg.textContent = t('common.loading'); msg.className = 'msg'; }
  api.createK8sCluster(name, server, kubeconfig).then(function (r) {
    if (!msg) return;
    if (r.s >= 200 && r.s < 300 && r.j) {
      msg.textContent = t('k8s.addClusterSuccess');
      msg.className = 'msg ok';
      // 刷新集群列表
      loadK8sClusters();
      // 1.5 秒后关闭对话框
      setTimeout(closeK8sAddClusterModal, 1500);
    } else {
      msg.textContent = t('k8s.addClusterFail') + ': [' + (r.s || '?') + '] ' + (r.j && (r.j.error || r.j.message) || '');
      msg.className = 'msg err';
    }
  }).catch(function (e) {
    console.error('[k8s-add-cluster]', e);
    if (msg) { msg.textContent = t('k8s.addClusterFail') + ': ' + (e && e.message || e); msg.className = 'msg err'; }
  });
}

// 删除集群确认
export function deleteK8sClusterConfirm(id) {
  if (!confirm(t('k8s.deleteClusterConfirm'))) return;
  api.deleteK8sCluster(id).then(function (r) {
    if (r.s === 204 || (r.s >= 200 && r.s < 300)) {
      loadK8sClusters();
      // 若删除的是当前选中的集群，清空资源管理区域
      if (k8sCurrentClusterID === id) {
        k8sCurrentClusterID = '';
        const sel = document.getElementById('k8sClusterSelect');
        if (sel) sel.value = '';
        const resEl = document.getElementById('k8sResourceList');
        if (resEl) resEl.innerHTML = '<p class="muted">' + esc(t('k8s.noClusterSelected')) + '</p>';
      }
    } else {
      alert(t('common.error') + ': HTTP ' + r.s);
    }
  }).catch(function (e) {
    console.error('[k8s-delete-cluster]', e);
    alert(t('common.networkError') + ': ' + (e && e.message || e));
  });
}

// 测试集群连接
export function testK8sClusterConnection(id) {
  // 用临时提示行内显示，不阻塞 UI
  api.testK8sCluster(id).then(function (r) {
    if (r.s >= 200 && r.s < 300 && r.j) {
      const ok = r.j.status === 'ok' || r.j.status === 'online' || r.j.status === 'success';
      alert((ok ? t('k8s.testSuccess') : t('k8s.testFail')) + (r.j.message ? ('\n' + r.j.message) : ''));
    } else {
      alert(t('k8s.testFail') + ': HTTP ' + r.s);
    }
  }).catch(function (e) {
    console.error('[k8s-test-cluster]', e);
    alert(t('k8s.testFail') + ': ' + (e && e.message || e));
  });
}

// ---------- K8s 资源管理 ----------
// 加载资源管理页面：设置当前集群，初始化下拉，加载默认资源（pods）
export function loadK8sResources(clusterID) {
  if (!clusterID) {
    const resEl = document.getElementById('k8sResourceList');
    if (resEl) resEl.innerHTML = '<p class="muted">' + esc(t('k8s.noClusterSelected')) + '</p>';
    return;
  }
  k8sCurrentClusterID = clusterID;
  // 同步下拉
  const sel = document.getElementById('k8sClusterSelect');
  if (sel) sel.value = clusterID;
  // 切换到 K8s tab（若不在）
  switchTab('k8s');
  // 默认加载 pods
  k8sCurrentResource = 'pods';
  k8sCurrentNamespace = '';
  const nsI = document.getElementById('k8sNamespaceInput'); if (nsI) nsI.value = '';
  // 高亮当前资源 tab
  document.querySelectorAll('#k8sResourceTabs .k8s-res-tab').forEach(function (btn) {
    btn.classList.toggle('active', (btn.getAttribute('data-res') || '') === k8sCurrentResource);
  });
  loadK8sResourceList();
}

// 切换资源类型 tab
export function switchK8sResource(res) {
  k8sCurrentResource = res || 'pods';
  document.querySelectorAll('#k8sResourceTabs .k8s-res-tab').forEach(function (btn) {
    btn.classList.toggle('active', (btn.getAttribute('data-res') || '') === k8sCurrentResource);
  });
  loadK8sResourceList();
}

// 重新加载当前资源列表（namespace 变更或刷新按钮触发）
export function reloadK8sResources() {
  const nsI = document.getElementById('k8sNamespaceInput');
  if (nsI) k8sCurrentNamespace = (nsI.value || '').trim();
  loadK8sResourceList();
}

// 加载当前资源列表（按 k8sCurrentResource 分派）
function loadK8sResourceList() {
  const el = document.getElementById('k8sResourceList');
  if (!el) return;
  if (!k8sCurrentClusterID) {
    el.innerHTML = '<p class="muted">' + esc(t('k8s.noClusterSelected')) + '</p>';
    return;
  }
  switch (k8sCurrentResource) {
    case 'pods': loadK8sPods(k8sCurrentClusterID); break;
    case 'deployments': loadK8sDeployments(k8sCurrentClusterID); break;
    case 'services': loadK8sServices(k8sCurrentClusterID); break;
    case 'configmaps': loadK8sConfigMaps(k8sCurrentClusterID); break;
    case 'secrets': loadK8sSecrets(k8sCurrentClusterID); break;
    case 'nodes': loadK8sNodes(k8sCurrentClusterID); break;
    default: el.innerHTML = '<p class="muted">-</p>';
  }
}

// 集群选择下拉变更处理
export function onK8sClusterSelectChange() {
  const sel = document.getElementById('k8sClusterSelect');
  const id = sel ? sel.value : '';
  if (!id) {
    k8sCurrentClusterID = '';
    const el = document.getElementById('k8sResourceList');
    if (el) el.innerHTML = '<p class="muted">' + esc(t('k8s.noClusterSelected')) + '</p>';
    return;
  }
  loadK8sResources(id);
}

// namespace 输入框变更处理（按 Enter 触发）
export function onK8sNamespaceKeyDown(ev) {
  if (ev && ev.key === 'Enter') { reloadK8sResources(); }
}

// ---------- Pod 列表 ----------
export function loadK8sPods(clusterID) {
  const el = document.getElementById('k8sResourceList');
  if (!el) return;
  el.innerHTML = '<p class="muted">' + esc(t('common.loading')) + '</p>';
  api.getK8sPods(clusterID, k8sCurrentNamespace).then(function (data) {
    const listEl = document.getElementById('k8sResourceList');
    if (!listEl) return;
    let list = [];
    if (Array.isArray(data)) list = data;
    else if (data && Array.isArray(data.pods)) list = data.pods;
    if (!list.length) {
      listEl.innerHTML = '<p class="muted">' + esc(t('common.empty')) + '</p>';
      return;
    }
    let html = '<div class="table-wrap"><table><colgroup>'
      + '<col style="width:22%"><col style="width:14%"><col style="width:12%"><col style="width:14%"><col style="width:14%"><col style="width:8%"><col style="width:8%"><col style="width:14%">'
      + '</colgroup><thead><tr>'
      + '<th>' + esc(t('k8s.podName')) + '</th>'
      + '<th>' + esc(t('k8s.namespace')) + '</th>'
      + '<th>' + esc(t('k8s.podStatus')) + '</th>'
      + '<th>' + esc(t('k8s.podIP')) + '</th>'
      + '<th>' + esc(t('k8s.nodeIP')) + '</th>'
      + '<th>' + esc(t('k8s.restarts')) + '</th>'
      + '<th>' + esc(t('k8s.age')) + '</th>'
      + '<th>' + esc(t('k8s.action')) + '</th>'
      + '</tr></thead><tbody>';
    list.forEach(function (p) {
      const ns = esc(p.namespace || '');
      const name = esc(p.name || '');
      const statusTxt = esc(p.status || '-');
      const statusCls = p.status === 'Running' ? 'badge ok' : (p.status === 'Failed' || p.status === 'Error' ? 'badge fail' : 'badge');
      html += '<tr>'
        + '<td><code title="' + name + '">' + name + '</code></td>'
        + '<td>' + ns + '</td>'
        + '<td><span class="' + statusCls + '">' + statusTxt + '</span></td>'
        + '<td>' + esc(p.podIP || '-') + '</td>'
        + '<td>' + esc(p.nodeIP || '-') + '</td>'
        + '<td>' + esc(p.restarts != null ? p.restarts : '-') + '</td>'
        + '<td>' + esc(p.age || '-') + '</td>'
        + '<td>'
        + '<button class="btn btn-sm" onclick="showPodLogs(\'' + escAttr(k8sCurrentClusterID) + '\',\'' + escAttr(p.namespace || '') + '\',\'' + escAttr(p.name || '') + '\')" style="margin-right:6px">' + icon('logs', 12) + ' ' + esc(t('k8s.viewLogs')) + '</button>'
        + '<button class="btn btn-sm" style="color:var(--fail)" onclick="deletePodConfirm(\'' + escAttr(k8sCurrentClusterID) + '\',\'' + escAttr(p.namespace || '') + '\',\'' + escAttr(p.name || '') + '\')">' + icon('delete', 12) + ' ' + esc(t('k8s.delete')) + '</button>'
        + '</td>'
        + '</tr>';
    });
    html += '</tbody></table></div>';
    listEl.innerHTML = html;
  }).catch(function (e) {
    console.error('[k8s-pods]', e);
    const listEl = document.getElementById('k8sResourceList');
    if (listEl) listEl.innerHTML = '<p class="muted">' + esc(t('common.networkError')) + '</p>';
  });
}

// pod 日志上下文
let k8sPodLogsCtx = null;

// ---------- Pod 日志 ----------
export function showPodLogs(clusterID, ns, name) {
  const modal = document.getElementById('k8sPodLogsModal');
  if (!modal) return;
  // 设置标题
  const titleEl = document.getElementById('k8sPodLogsTitle');
  if (titleEl) titleEl.textContent = t('k8s.logs') + ' — ' + ns + '/' + name;
  const logEl = document.getElementById('k8sPodLogsContent');
  if (logEl) logEl.textContent = t('common.loading');
  // tailLines 输入框默认 100
  const tailI = document.getElementById('k8sPodLogsTail'); if (tailI && !tailI.value) tailI.value = '100';
  const containerI = document.getElementById('k8sPodLogsContainer'); if (containerI) containerI.value = '';
  // 保存上下文供刷新按钮使用
  k8sPodLogsCtx = { clusterID: clusterID, ns: ns, name: name };
  modal.classList.add('open');
  fetchPodLogs();
}

// 关闭 pod 日志对话框
export function closeK8sPodLogsModal() {
  const modal = document.getElementById('k8sPodLogsModal');
  if (modal) modal.classList.remove('open');
  k8sPodLogsCtx = null;
}

// 拉取 pod 日志并渲染
function fetchPodLogs() {
  if (!k8sPodLogsCtx) return;
  const logEl = document.getElementById('k8sPodLogsContent');
  if (logEl) logEl.textContent = t('common.loading');
  const tail = (document.getElementById('k8sPodLogsTail').value || '100').trim();
  const container = (document.getElementById('k8sPodLogsContainer').value || '').trim();
  api.getK8sPodLogs(k8sPodLogsCtx.clusterID, k8sPodLogsCtx.ns, k8sPodLogsCtx.name, tail, container || null).then(function (data) {
    const el = document.getElementById('k8sPodLogsContent');
    if (!el) return;
    let logs = '';
    if (typeof data === 'string') logs = data;
    else if (data && typeof data.logs === 'string') logs = data.logs;
    else if (data) logs = JSON.stringify(data, null, 2);
    el.textContent = logs || '(empty)';
  }).catch(function (e) {
    console.error('[k8s-pod-logs]', e);
    const el = document.getElementById('k8sPodLogsContent');
    if (el) el.textContent = t('common.networkError') + ': ' + (e && e.message || e);
  });
}

// pod 日志刷新按钮
export function refreshK8sPodLogs() {
  fetchPodLogs();
}

// 删除 pod 确认
export function deletePodConfirm(clusterID, ns, name) {
  if (!confirm(t('k8s.deletePodConfirm'))) return;
  api.deleteK8sPod(clusterID, ns, name).then(function (r) {
    if (r.s === 204 || (r.s >= 200 && r.s < 300)) {
      // 刷新 pod 列表
      loadK8sPods(clusterID);
    } else {
      alert(t('common.error') + ': HTTP ' + r.s);
    }
  }).catch(function (e) {
    console.error('[k8s-delete-pod]', e);
    alert(t('common.networkError') + ': ' + (e && e.message || e));
  });
}

// ---------- Deployment 列表 ----------
export function loadK8sDeployments(clusterID) {
  const el = document.getElementById('k8sResourceList');
  if (!el) return;
  el.innerHTML = '<p class="muted">' + esc(t('common.loading')) + '</p>';
  api.getK8sDeployments(clusterID, k8sCurrentNamespace).then(function (data) {
    const listEl = document.getElementById('k8sResourceList');
    if (!listEl) return;
    let list = [];
    if (Array.isArray(data)) list = data;
    else if (data && Array.isArray(data.deployments)) list = data.deployments;
    if (!list.length) {
      listEl.innerHTML = '<p class="muted">' + esc(t('common.empty')) + '</p>';
      return;
    }
    let html = '<div class="table-wrap"><table><colgroup>'
      + '<col style="width:22%"><col style="width:14%"><col style="width:14%"><col style="width:14%"><col style="width:22%"><col style="width:14%">'
      + '</colgroup><thead><tr>'
      + '<th>' + esc(t('k8s.name')) + '</th>'
      + '<th>' + esc(t('k8s.namespace')) + '</th>'
      + '<th>' + esc(t('k8s.replicas')) + '</th>'
      + '<th>' + esc(t('k8s.available')) + '</th>'
      + '<th>' + esc(t('k8s.image')) + '</th>'
      + '<th>' + esc(t('k8s.action')) + '</th>'
      + '</tr></thead><tbody>';
    list.forEach(function (d) {
      const ns = esc(d.namespace || '');
      const name = esc(d.name || '');
      const replicas = esc(d.replicas != null ? d.replicas : '-');
      const available = esc(d.availableReplicas != null ? d.availableReplicas : '-');
      html += '<tr>'
        + '<td><code title="' + name + '">' + name + '</code></td>'
        + '<td>' + ns + '</td>'
        + '<td>' + replicas + '</td>'
        + '<td>' + available + '</td>'
        + '<td><code style="font-size:11px;word-break:break-all">' + esc(d.image || '-') + '</code></td>'
        + '<td>'
        + '<button class="btn btn-sm" onclick="scaleDeployment(\'' + escAttr(k8sCurrentClusterID) + '\',\'' + escAttr(d.namespace || '') + '\',\'' + escAttr(d.name || '') + '\')" style="margin-right:6px">' + icon('edit', 12) + ' ' + esc(t('k8s.scale')) + '</button>'
        + '<button class="btn btn-sm" onclick="restartDeployment(\'' + escAttr(k8sCurrentClusterID) + '\',\'' + escAttr(d.namespace || '') + '\',\'' + escAttr(d.name || '') + '\')">' + icon('refresh', 12) + ' ' + esc(t('k8s.restart')) + '</button>'
        + '</td>'
        + '</tr>';
    });
    html += '</tbody></table></div>';
    listEl.innerHTML = html;
  }).catch(function (e) {
    console.error('[k8s-deployments]', e);
    const listEl = document.getElementById('k8sResourceList');
    if (listEl) listEl.innerHTML = '<p class="muted">' + esc(t('common.networkError')) + '</p>';
  });
}

// 扩缩容对话框
let k8sScaleCtx = null;
export function scaleDeployment(clusterID, ns, name) {
  const modal = document.getElementById('k8sScaleModal');
  if (!modal) return;
  const titleEl = document.getElementById('k8sScaleTitle');
  if (titleEl) titleEl.textContent = t('k8s.scale') + ' — ' + ns + '/' + name;
  const replicasI = document.getElementById('k8sScaleReplicas');
  if (replicasI) replicasI.value = '1';
  const msg = document.getElementById('k8sScaleMsg');
  if (msg) { msg.textContent = ''; msg.className = 'msg'; }
  k8sScaleCtx = { clusterID: clusterID, ns: ns, name: name };
  modal.classList.add('open');
}

// 关闭扩缩容对话框
export function closeK8sScaleModal() {
  const modal = document.getElementById('k8sScaleModal');
  if (modal) modal.classList.remove('open');
  k8sScaleCtx = null;
}

// 确认扩缩容
export function confirmScaleDeployment() {
  if (!k8sScaleCtx) return;
  const replicasVal = (document.getElementById('k8sScaleReplicas').value || '').trim();
  const replicas = parseInt(replicasVal, 10);
  const msg = document.getElementById('k8sScaleMsg');
  if (isNaN(replicas) || replicas < 0) {
    if (msg) { msg.textContent = t('param.required'); msg.className = 'msg err'; }
    return;
  }
  if (msg) { msg.textContent = t('common.loading'); msg.className = 'msg'; }
  api.scaleK8sDeployment(k8sScaleCtx.clusterID, k8sScaleCtx.ns, k8sScaleCtx.name, replicas).then(function (r) {
    if (!msg) return;
    if (r.s >= 200 && r.s < 300 && r.j) {
      msg.textContent = t('k8s.scale') + ' OK: ' + (r.j.name || '') + ' → ' + (r.j.replicas != null ? r.j.replicas : replicas);
      msg.className = 'msg ok';
      // 刷新 deployment 列表
      loadK8sDeployments(k8sScaleCtx.clusterID);
      setTimeout(closeK8sScaleModal, 1200);
    } else {
      msg.textContent = t('common.error') + ': [' + (r.s || '?') + '] ' + (r.j && (r.j.error || r.j.message) || '');
      msg.className = 'msg err';
    }
  }).catch(function (e) {
    console.error('[k8s-scale]', e);
    if (msg) { msg.textContent = t('common.networkError') + ': ' + (e && e.message || e); msg.className = 'msg err'; }
  });
}

// 重启 deployment
export function restartDeployment(clusterID, ns, name) {
  if (!confirm(t('k8s.restartConfirm'))) return;
  api.restartK8sDeployment(clusterID, ns, name).then(function (r) {
    if (r.s >= 200 && r.s < 300 && r.j) {
      alert(t('k8s.restart') + ' OK: ' + (r.j.restartedAt || ''));
      // 刷新 deployment 列表
      loadK8sDeployments(clusterID);
    } else {
      alert(t('common.error') + ': [' + (r.s || '?') + '] ' + (r.j && (r.j.error || r.j.message) || ''));
    }
  }).catch(function (e) {
    console.error('[k8s-restart]', e);
    alert(t('common.networkError') + ': ' + (e && e.message || e));
  });
}

// ---------- Service 列表 ----------
export function loadK8sServices(clusterID) {
  const el = document.getElementById('k8sResourceList');
  if (!el) return;
  el.innerHTML = '<p class="muted">' + esc(t('common.loading')) + '</p>';
  api.getK8sServices(clusterID, k8sCurrentNamespace).then(function (data) {
    const listEl = document.getElementById('k8sResourceList');
    if (!listEl) return;
    let list = [];
    if (Array.isArray(data)) list = data;
    else if (data && Array.isArray(data.services)) list = data.services;
    if (!list.length) {
      listEl.innerHTML = '<p class="muted">' + esc(t('common.empty')) + '</p>';
      return;
    }
    let html = '<div class="table-wrap"><table><colgroup>'
      + '<col style="width:20%"><col style="width:14%"><col style="width:12%"><col style="width:16%"><col style="width:16%"><col style="width:22%">'
      + '</colgroup><thead><tr>'
      + '<th>' + esc(t('k8s.name')) + '</th>'
      + '<th>' + esc(t('k8s.namespace')) + '</th>'
      + '<th>' + esc(t('k8s.serviceType')) + '</th>'
      + '<th>' + esc(t('k8s.clusterIP')) + '</th>'
      + '<th>' + esc(t('k8s.externalIP')) + '</th>'
      + '<th>' + esc(t('k8s.ports')) + '</th>'
      + '</tr></thead><tbody>';
    list.forEach(function (s) {
      html += '<tr>'
        + '<td><code>' + esc(s.name || '') + '</code></td>'
        + '<td>' + esc(s.namespace || '-') + '</td>'
        + '<td>' + esc(s.type || '-') + '</td>'
        + '<td>' + esc(s.clusterIP || '-') + '</td>'
        + '<td>' + esc(s.externalIP || '-') + '</td>'
        + '<td><code style="font-size:11px;word-break:break-all">' + esc(s.ports || '-') + '</code></td>'
        + '</tr>';
    });
    html += '</tbody></table></div>';
    listEl.innerHTML = html;
  }).catch(function (e) {
    console.error('[k8s-services]', e);
    const listEl = document.getElementById('k8sResourceList');
    if (listEl) listEl.innerHTML = '<p class="muted">' + esc(t('common.networkError')) + '</p>';
  });
}

// ---------- ConfigMap 列表 ----------
export function loadK8sConfigMaps(clusterID) {
  const el = document.getElementById('k8sResourceList');
  if (!el) return;
  el.innerHTML = '<p class="muted">' + esc(t('common.loading')) + '</p>';
  api.getK8sConfigMaps(clusterID, k8sCurrentNamespace).then(function (data) {
    const listEl = document.getElementById('k8sResourceList');
    if (!listEl) return;
    let list = [];
    if (Array.isArray(data)) list = data;
    else if (data && Array.isArray(data.configmaps)) list = data.configmaps;
    if (!list.length) {
      listEl.innerHTML = '<p class="muted">' + esc(t('common.empty')) + '</p>';
      return;
    }
    let html = '<div class="table-wrap"><table><colgroup>'
      + '<col style="width:30%"><col style="width:20%"><col style="width:50%">'
      + '</colgroup><thead><tr>'
      + '<th>' + esc(t('k8s.name')) + '</th>'
      + '<th>' + esc(t('k8s.namespace')) + '</th>'
      + '<th>' + esc(t('k8s.dataKeys')) + '</th>'
      + '</tr></thead><tbody>';
    list.forEach(function (c) {
      const keys = Array.isArray(c.dataKeys) ? c.dataKeys.join(', ') : (c.dataKeys || '-');
      html += '<tr>'
        + '<td><code>' + esc(c.name || '') + '</code></td>'
        + '<td>' + esc(c.namespace || '-') + '</td>'
        + '<td><code style="font-size:11px;word-break:break-all">' + esc(keys) + '</code></td>'
        + '</tr>';
    });
    html += '</tbody></table></div>';
    listEl.innerHTML = html;
  }).catch(function (e) {
    console.error('[k8s-configmaps]', e);
    const listEl = document.getElementById('k8sResourceList');
    if (listEl) listEl.innerHTML = '<p class="muted">' + esc(t('common.networkError')) + '</p>';
  });
}

// ---------- Secret 列表 ----------
export function loadK8sSecrets(clusterID) {
  const el = document.getElementById('k8sResourceList');
  if (!el) return;
  el.innerHTML = '<p class="muted">' + esc(t('common.loading')) + '</p>';
  api.getK8sSecrets(clusterID, k8sCurrentNamespace).then(function (data) {
    const listEl = document.getElementById('k8sResourceList');
    if (!listEl) return;
    let list = [];
    if (Array.isArray(data)) list = data;
    else if (data && Array.isArray(data.secrets)) list = data.secrets;
    if (!list.length) {
      listEl.innerHTML = '<p class="muted">' + esc(t('common.empty')) + '</p>';
      return;
    }
    let html = '<div class="table-wrap"><table><colgroup>'
      + '<col style="width:26%"><col style="width:18%"><col style="width:18%"><col style="width:38%">'
      + '</colgroup><thead><tr>'
      + '<th>' + esc(t('k8s.name')) + '</th>'
      + '<th>' + esc(t('k8s.namespace')) + '</th>'
      + '<th>' + esc(t('k8s.serviceType')) + '</th>'
      + '<th>' + esc(t('k8s.dataKeys')) + '</th>'
      + '</tr></thead><tbody>';
    list.forEach(function (s) {
      const keys = Array.isArray(s.dataKeys) ? s.dataKeys.join(', ') : (s.dataKeys || '-');
      html += '<tr>'
        + '<td><code>' + esc(s.name || '') + '</code></td>'
        + '<td>' + esc(s.namespace || '-') + '</td>'
        + '<td>' + esc(s.type || '-') + '</td>'
        + '<td><code style="font-size:11px;word-break:break-all">' + esc(keys) + '</code></td>'
        + '</tr>';
    });
    html += '</tbody></table></div>';
    listEl.innerHTML = html;
  }).catch(function (e) {
    console.error('[k8s-secrets]', e);
    const listEl = document.getElementById('k8sResourceList');
    if (listEl) listEl.innerHTML = '<p class="muted">' + esc(t('common.networkError')) + '</p>';
  });
}

// ---------- Node 列表 ----------
export function loadK8sNodes(clusterID) {
  const el = document.getElementById('k8sResourceList');
  if (!el) return;
  el.innerHTML = '<p class="muted">' + esc(t('common.loading')) + '</p>';
  api.getK8sNodes(clusterID).then(function (data) {
    const listEl = document.getElementById('k8sResourceList');
    if (!listEl) return;
    let list = [];
    if (Array.isArray(data)) list = data;
    else if (data && Array.isArray(data.nodes)) list = data.nodes;
    if (!list.length) {
      listEl.innerHTML = '<p class="muted">' + esc(t('common.empty')) + '</p>';
      return;
    }
    let html = '<div class="table-wrap"><table><colgroup>'
      + '<col style="width:14%"><col style="width:10%"><col style="width:14%"><col style="width:10%"><col style="width:14%"><col style="width:14%"><col style="width:8%"><col style="width:8%">'
      + '</colgroup><thead><tr>'
      + '<th>' + esc(t('k8s.name')) + '</th>'
      + '<th>' + esc(t('k8s.status')) + '</th>'
      + '<th>' + esc(t('k8s.roles')) + '</th>'
      + '<th>' + esc(t('k8s.version')) + '</th>'
      + '<th>' + esc(t('k8s.internalIP')) + '</th>'
      + '<th>' + esc(t('k8s.externalIP')) + '</th>'
      + '<th>' + esc(t('k8s.cpu')) + '</th>'
      + '<th>' + esc(t('k8s.memory')) + '</th>'
      + '</tr></thead><tbody>';
    list.forEach(function (n) {
      const statusTxt = esc(n.status || '-');
      const statusCls = n.status === 'Ready' ? 'badge ok' : 'badge';
      const roles = Array.isArray(n.roles) ? n.roles.join(', ') : (n.roles || '-');
      html += '<tr>'
        + '<td><code>' + esc(n.name || '') + '</code></td>'
        + '<td><span class="' + statusCls + '">' + statusTxt + '</span></td>'
        + '<td>' + esc(roles) + '</td>'
        + '<td>' + esc(n.version || '-') + '</td>'
        + '<td>' + esc(n.internalIP || '-') + '</td>'
        + '<td>' + esc(n.externalIP || '-') + '</td>'
        + '<td>' + esc(n.cpu != null ? n.cpu : '-') + '</td>'
        + '<td>' + esc(n.memory != null ? n.memory : '-') + '</td>'
        + '</tr>';
    });
    html += '</tbody></table></div>';
    listEl.innerHTML = html;
  }).catch(function (e) {
    console.error('[k8s-nodes]', e);
    const listEl = document.getElementById('k8sResourceList');
    if (listEl) listEl.innerHTML = '<p class="muted">' + esc(t('common.networkError')) + '</p>';
  });
}