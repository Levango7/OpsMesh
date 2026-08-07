// flow_middleware.js — 中间件部署
// 从 flow.js 拆分（P2-1）。职责：中间件模板列表加载、分类筛选、详情查看、部署、实例列表、卸载。
// 依赖：api.js、render.js（esc/escAttr/fmtTime）、icons.js、i18n.js、flow_os.js（pollTaskResult）。
// 后端契约：
//   GET  /api/v1/middleware-templates[?category=] → MiddlewareTemplate[]
//   GET  /api/v1/middleware-templates/{id}        → MiddlewareTemplate
//   POST /api/v1/middleware-templates/{id}/deploy {agentID, deployType, params} → {taskID}
//   GET  /api/v1/middleware-instances             → MiddlewareInstance[]
// MiddlewareTemplate 字段：
//   id, name, category, version, description, deployTypes[], params[], scripts{docker,systemd}, risk, tags[]
// scripts.docker / scripts.systemd 各含 {deploy, verify, uninstall}

import * as api from './api.js';
import { esc, escAttr, fmtTime } from './render.js';
import { icon } from './icons.js';
import { t } from './i18n.js';
import { pollTaskResult } from './flow_os.js';

// 当前分类筛选（模块级状态）
let mwdepCurrentCat = '';
// 当前待部署的模板（部署对话框用）
let mwdepDeployTpl = null;
// 当前部署轮询定时器（用于在关闭对话框时停止轮询）
let mwdepDeployTimer = null;
// 当前待卸载的实例（卸载对话框用）
let mwdepUninstallIns = null;
// 当前卸载轮询定时器
let mwdepUninstallTimer = null;

// 风险等级 → 颜色 + 文本键
function mwdepRiskStyle(risk) {
  if (risk === 'low') return { color: 'var(--green)', bg: 'var(--green-soft, #e6f9ee)' };
  if (risk === 'high') return { color: 'var(--fail)', bg: 'var(--fail-soft, #fde8e8)' };
  return { color: 'var(--accent)', bg: 'var(--accent-soft)' };
}

// 渲染风险等级标签
function mwdepRiskBadge(risk) {
  const s = mwdepRiskStyle(risk);
  return '<span class="badge" style="background:' + s.bg + ';color:' + s.color + ';border:1px solid ' + s.color + '">' + esc(t('mwdep.risk.' + (risk || 'medium'))) + '</span>';
}

// 渲染分类标签
function mwdepCatBadge(cat) {
  const key = 'mwdep.category.' + (cat || 'all');
  return '<span class="badge" style="background:var(--bg-2);color:var(--text-2)">' + esc(t(key)) + '</span>';
}

// 渲染部署方式标签
function mwdepDeployTypeBadge(dt) {
  return '<span class="badge" style="background:var(--bg-2);color:var(--text-2);margin-right:4px">' + esc(t('mwdep.deployType.' + (dt || 'docker'))) + '</span>';
}

// 加载中间件部署模板列表（按当前分类筛选）
export function loadMiddlewareTemplates() {
  const listEl = document.getElementById('mwTemplateList');
  if (listEl) listEl.innerHTML = '<p class="muted">' + esc(t('mwdep.loading')) + '</p>';
  // 隐藏详情面板
  const detailEl = document.getElementById('mwTemplateDetail');
  if (detailEl) detailEl.style.display = 'none';
  api.getMiddlewareTemplates(mwdepCurrentCat).then(function (list) {
    const el = document.getElementById('mwTemplateList');
    if (!el) return;
    if (!list || list.length === 0) {
      el.innerHTML = '<p class="muted">' + esc(t('mwdep.noTemplates')) + '</p>';
      return;
    }
    let html = '<div class="table-wrap"><table><colgroup>'
      + '<col style="width:14%"><col style="width:20%"><col style="width:12%"><col style="width:10%"><col style="width:16%"><col style="width:10%"><col style="width:18%">'
      + '</colgroup><thead><tr>'
      + '<th>' + esc(t('mwdep.col.id')) + '</th>'
      + '<th>' + esc(t('mwdep.col.name')) + '</th>'
      + '<th>' + esc(t('mwdep.col.category')) + '</th>'
      + '<th>' + esc(t('mwdep.col.version')) + '</th>'
      + '<th>' + esc(t('mwdep.col.deployTypes')) + '</th>'
      + '<th>' + esc(t('mwdep.col.risk')) + '</th>'
      + '<th>' + esc(t('mwdep.col.action')) + '</th>'
      + '</tr></thead><tbody>';
    list.forEach(function (tpl) {
      const tid = esc(tpl.id || '');
      const deployTypesHtml = (tpl.deployTypes || []).map(function (dt) { return mwdepDeployTypeBadge(dt); }).join('');
      html += '<tr>'
        + '<td><code title="' + tid + '">' + tid + '</code></td>'
        + '<td>' + esc(tpl.name || '') + '</td>'
        + '<td>' + mwdepCatBadge(tpl.category) + '</td>'
        + '<td>' + esc(tpl.version || '-') + '</td>'
        + '<td>' + (deployTypesHtml || '-') + '</td>'
        + '<td>' + mwdepRiskBadge(tpl.risk) + '</td>'
        + '<td>'
        + '<button class="btn btn-sm" onclick="showMiddlewareDetail(\'' + escAttr(tpl.id || '') + '\')" style="margin-right:6px">' + icon('search', 12) + ' ' + esc(t('mwdep.view')) + '</button>'
        + '<button class="btn btn-primary btn-sm" onclick="deployMiddleware(\'' + escAttr(tpl.id || '') + '\')">' + icon('deploy', 12) + ' ' + esc(t('mwdep.deploy')) + '</button>'
        + '</td>'
        + '</tr>';
    });
    html += '</tbody></table></div>';
    el.innerHTML = html;
  }).catch(function (e) {
    console.error('[mw-templates]', e);
    const el = document.getElementById('mwTemplateList');
    if (el) el.innerHTML = '<p class="muted">' + esc(t('mwdep.networkError')) + '</p>';
  });
}

// 分类筛选：切换分类并重新加载
export function filterMiddlewareTemplates(category) {
  mwdepCurrentCat = category || '';
  // 更新按钮 active 状态
  document.querySelectorAll('#mwdepCatFilter .osopt-cat-btn').forEach(function (btn) {
    btn.classList.toggle('active', (btn.getAttribute('data-cat') || '') === mwdepCurrentCat);
  });
  loadMiddlewareTemplates();
}

// 显示模板详情
export function showMiddlewareDetail(id) {
  const detailEl = document.getElementById('mwTemplateDetail');
  if (!detailEl) return;
  detailEl.style.display = '';
  detailEl.innerHTML = '<p class="muted">' + esc(t('mwdep.loading')) + '</p>';
  api.getMiddlewareTemplate(id).then(function (tpl) {
    const el = document.getElementById('mwTemplateDetail');
    if (!el) return;
    if (!tpl || !tpl.id) {
      el.innerHTML = '<p class="muted">' + esc(t('mwdep.noTemplates')) + '</p>';
      return;
    }
    const tags = (tpl.tags || []).map(function (tag) { return '<span class="badge" style="background:var(--bg-2);color:var(--text-2);margin-right:4px">' + esc(tag) + '</span>'; }).join('');
    const deployTypesHtml = (tpl.deployTypes || []).map(function (dt) { return mwdepDeployTypeBadge(dt); }).join('');
    let html = '<div style="display:flex;justify-content:space-between;align-items:flex-start;flex-wrap:wrap;gap:8px;margin-bottom:12px">'
      + '<h4 style="margin:0">' + esc(tpl.name || tpl.id) + ' <code style="font-size:12px;color:var(--text-3)">' + esc(tpl.id) + '</code></h4>'
      + '<button class="btn btn-sm" onclick="hideMiddlewareDetail()">' + icon('close', 12) + ' ' + esc(t('mwdep.detailClose')) + '</button>'
      + '</div>';
    html += '<div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:12px;margin-bottom:12px">'
      + '<div><div class="field-hint">' + esc(t('mwdep.col.category')) + '</div><div>' + mwdepCatBadge(tpl.category) + '</div></div>'
      + '<div><div class="field-hint">' + esc(t('mwdep.detailVersion')) + '</div><div>' + esc(tpl.version || '-') + '</div></div>'
      + '<div><div class="field-hint">' + esc(t('mwdep.col.risk')) + '</div><div>' + mwdepRiskBadge(tpl.risk) + '</div></div>'
      + '<div><div class="field-hint">' + esc(t('mwdep.col.deployTypes')) + '</div><div>' + (deployTypesHtml || '-') + '</div></div>'
      + '<div><div class="field-hint">' + esc(t('mwdep.detailTags')) + '</div><div>' + (tags || '-') + '</div></div>'
      + '</div>';
    html += '<div style="margin-bottom:12px"><div class="field-hint">' + esc(t('mwdep.detailDesc')) + '</div><div>' + esc(tpl.description || '-') + '</div></div>';
    // 参数列表
    const params = tpl.params || [];
    if (params.length > 0) {
      html += '<div style="margin-bottom:12px"><div class="field-hint" style="margin-bottom:4px">' + esc(t('mwdep.detailParams')) + '</div>';
      html += '<div class="table-wrap"><table><colgroup><col style="width:20%"><col style="width:14%"><col style="width:14%"><col style="width:14%"><col style="width:38%"></colgroup><thead><tr><th>Name</th><th>Type</th><th>Default</th><th>Required</th><th>Description</th></tr></thead><tbody>';
      params.forEach(function (p) {
        html += '<tr>'
          + '<td><code>' + esc(p.name || '') + '</code></td>'
          + '<td>' + esc(p.type || '-') + '</td>'
          + '<td>' + esc(p.default != null ? String(p.default) : '-') + '</td>'
          + '<td>' + (p.required ? '<span class="badge" style="background:var(--fail-soft);color:var(--fail)">' + esc(t('mwdep.param.required')) + '</span>' : '<span class="badge" style="background:var(--bg-2);color:var(--text-2)">' + esc(t('mwdep.param.optional')) + '</span>') + '</td>'
          + '<td>' + esc(p.description || '-') + '</td>'
          + '</tr>';
      });
      html += '</tbody></table></div></div>';
    }
    // 脚本：按部署方式分组显示
    const scripts = tpl.scripts || {};
    const scriptTypes = Object.keys(scripts);
    if (scriptTypes.length > 0) {
      html += '<div><div class="field-hint" style="margin-bottom:4px">' + esc(t('mwdep.detailScripts')) + '</div>';
      scriptTypes.forEach(function (st) {
        const sc = scripts[st] || {};
        html += '<div style="margin-bottom:12px">'
          + '<div style="font-weight:600;margin-bottom:4px">' + mwdepDeployTypeBadge(st) + '</div>'
          + '<div style="margin-bottom:6px"><div class="field-hint">' + esc(t('mwdep.detailDeployScript')) + '</div>'
          + '<pre style="background:var(--bg-2);color:var(--text-1);padding:12px;border-radius:6px;overflow:auto;max-height:200px;font-size:12px;line-height:1.5;white-space:pre-wrap;word-break:break-all">' + esc(sc.deploy || '') + '</pre></div>'
          + '<div style="margin-bottom:6px"><div class="field-hint">' + esc(t('mwdep.detailVerifyScript')) + '</div>'
          + '<pre style="background:var(--bg-2);color:var(--text-1);padding:12px;border-radius:6px;overflow:auto;max-height:160px;font-size:12px;line-height:1.5;white-space:pre-wrap;word-break:break-all">' + esc(sc.verify || '') + '</pre></div>'
          + '<div><div class="field-hint">' + esc(t('mwdep.detailUninstallScript')) + '</div>'
          + '<pre style="background:var(--bg-2);color:var(--text-1);padding:12px;border-radius:6px;overflow:auto;max-height:160px;font-size:12px;line-height:1.5;white-space:pre-wrap;word-break:break-all">' + esc(sc.uninstall || '') + '</pre></div>'
          + '</div>';
      });
      html += '</div>';
    }
    html += '<div style="margin-top:12px;text-align:right">'
      + '<button class="btn btn-primary btn-sm" onclick="deployMiddleware(\'' + escAttr(tpl.id) + '\')">' + icon('deploy', 12) + ' ' + esc(t('mwdep.deploy')) + '</button>'
      + '</div>';
    el.innerHTML = html;
  }).catch(function (e) {
    console.error('[mw-template-detail]', e);
    const el = document.getElementById('mwTemplateDetail');
    if (el) el.innerHTML = '<p class="muted">' + esc(t('mwdep.networkError')) + '</p>';
  });
}

// 隐藏模板详情
export function hideMiddlewareDetail() {
  const el = document.getElementById('mwTemplateDetail');
  if (el) { el.style.display = 'none'; el.innerHTML = ''; }
}

// 打开部署对话框：加载模板详情 + 设备列表 + 部署方式
export function deployMiddleware(id) {
  const modal = document.getElementById('mwDeployModal');
  if (!modal) return;
  // 重置结果区
  const resultEl = document.getElementById('mwDeployResult');
  if (resultEl) { resultEl.innerHTML = ''; resultEl.className = ''; }
  // 先加载模板详情（用于生成参数表单）
  api.getMiddlewareTemplate(id).then(function (tpl) {
    if (!tpl || !tpl.id) {
      if (resultEl) { resultEl.className = 'msg err'; resultEl.textContent = t('mwdep.deployFail') + ': template not found'; }
      return;
    }
    mwdepDeployTpl = tpl;
    // 部署方式下拉
    const typeSel = document.getElementById('mwDeployType');
    if (typeSel) {
      typeSel.innerHTML = '';
      (tpl.deployTypes || ['docker', 'systemd']).forEach(function (dt) {
        const o = document.createElement('option');
        o.value = dt;
        o.textContent = t('mwdep.deployType.' + dt);
        typeSel.appendChild(o);
      });
    }
    // 参数表单
    renderMwDeployParams();
    // 设备下拉
    const agentSel = document.getElementById('mwDeployAgent');
    if (agentSel) {
      agentSel.innerHTML = '<option value="">— ' + esc(t('mwdep.selectAgent')) + ' —</option>';
      api.getDevices().then(function (devs) {
        if (!agentSel) return;
        let arr = [];
        if (Array.isArray(devs)) {
          arr = devs;
        } else if (devs && typeof devs === 'object') {
          Object.keys(devs).forEach(function (seg) {
            const v = devs[seg];
            if (Array.isArray(v)) { v.forEach(function (d) { arr.push(d); }); }
            else { arr.push(v); }
          });
        }
        arr.forEach(function (d) {
          const aid = d.agentID || d.id || '';
          if (!aid) return;
          const label = aid + ' (' + esc(d.hostname || d.ip || d.deviceID || '') + ')';
          const o = document.createElement('option');
          o.value = aid;
          o.textContent = label;
          agentSel.appendChild(o);
        });
      }).catch(function (e) { console.error('[mw-deploy devices]', e); });
    }
    modal.classList.add('open');
  }).catch(function (e) {
    console.error('[mw-deploy template]', e);
    if (resultEl) { resultEl.className = 'msg err'; resultEl.textContent = t('mwdep.networkError'); }
    modal.classList.add('open');
  });
}

// 渲染部署参数表单（根据当前模板与部署方式）
// Phase 2 改造：使用通用 renderParamInput，添加端口范围 / 密码强度 / 路径提示 + required
function renderMwDeployParams() {
  const paramsEl = document.getElementById('mwDeployParams');
  if (!paramsEl || !mwdepDeployTpl) return;
  const params = mwdepDeployTpl.params || [];
  if (params.length === 0) {
    paramsEl.innerHTML = '<p class="muted">—</p>';
    return;
  }
  // 复用 flow_os.js 暴露的参数渲染函数（通过 window._opsmeshParamOnInput 已暴露验证逻辑）
  // 这里直接内联渲染，保持与原 flow.js 行为一致
  let html = '';
  params.forEach(function (p) {
    html += renderParamInputLocal(p, 'mw');
  });
  paramsEl.innerHTML = html;
}

// 本地参数输入渲染（与 flow_os.js 的 renderParamInput 逻辑一致）
function renderParamInputLocal(p, prefix) {
  const name = p.name || '';
  const def = p.default != null ? String(p.default) : '';
  const required = p.required ? ' <span style="color:var(--fail)">*</span>' : '';
  const placeholder = p.description || '';
  const type = p.type || 'text';
  let inputType = 'text';
  const nLower = (name || '').toLowerCase();
  const isPort = nLower.indexOf('port') >= 0;
  const isPath = nLower.indexOf('dir') >= 0 || nLower.indexOf('path') >= 0;
  const isPwd = nLower.indexOf('password') >= 0 || nLower.indexOf('pwd') >= 0 || type === 'password';
  if (type === 'int' || isPort) inputType = 'number';
  else if (isPwd) inputType = 'password';
  let rangeAttr = '';
  if (inputType === 'number' && isPort) {
    rangeAttr = ' min="1" max="65535"';
  }
  const hints = [];
  if (isPort) hints.push(t('param.portRange'));
  if (isPath) hints.push(t('osopt.pathHint'));
  if (isPwd) hints.push(t('osopt.passwordHint'));
  const hintHtml = hints.length ? ' <span class="field-hint" style="margin-left:4px;color:var(--text-3)">' + esc(hints.join(' · ')) + '</span>' : '';
  const requiredAttr = p.required ? ' required' : '';
  const inputId = prefix + 'Param_' + esc(name).replace(/[^a-zA-Z0-9_]/g, '_');
  const strengthHtml = isPwd
    ? ' <span id="' + inputId + '_strength" style="margin-left:6px;font-size:12px;font-weight:600"></span>'
    : '';
  const onInputAttr = ' oninput="window._opsmeshParamOnInput && window._opsmeshParamOnInput(this)"';
  let html = '<div style="margin-bottom:8px">'
    + '<label style="display:block;margin-bottom:2px" for="' + inputId + '"><code>' + esc(name) + '</code>' + required + hintHtml + strengthHtml + '</label>'
    + '<input type="' + inputType + '"' + rangeAttr + requiredAttr + onInputAttr + ' class="form-control" id="' + inputId + '" data-pname="' + esc(name) + '" value="' + esc(def) + '" placeholder="' + esc(placeholder) + '">'
    + '</div>';
  return html;
}

// 部署方式切换：当前模板不变，仅刷新参数表单（参数不随部署方式变化）
export function onMwDeployTypeChange() {
  // 参数由模板决定，与部署方式无关，无需重新渲染
}

// 关闭部署对话框：停止轮询并清理状态
export function closeMwDeployModal() {
  const modal = document.getElementById('mwDeployModal');
  if (modal) modal.classList.remove('open');
  if (mwdepDeployTimer) { clearInterval(mwdepDeployTimer); mwdepDeployTimer = null; }
  mwdepDeployTpl = null;
}

// 确认部署：调用 API 在选定设备上以指定方式部署中间件
// Phase 2 改造：提交前参数验证 + 提交后轮询任务状态 + 日志展示
export function confirmMwDeploy() {
  const resultEl = document.getElementById('mwDeployResult');
  if (!mwdepDeployTpl || !mwdepDeployTpl.id) {
    if (resultEl) { resultEl.className = 'msg err'; resultEl.textContent = t('mwdep.deployFail') + ': no template'; }
    return;
  }
  const typeSel = document.getElementById('mwDeployType');
  const deployType = typeSel ? typeSel.value : '';
  if (!deployType) {
    if (resultEl) { resultEl.className = 'msg err'; resultEl.textContent = t('mwdep.noDeployType'); }
    return;
  }
  const agentSel = document.getElementById('mwDeployAgent');
  const agentID = agentSel ? agentSel.value : '';
  if (!agentID) {
    if (resultEl) { resultEl.className = 'msg err'; resultEl.textContent = t('mwdep.noAgent'); }
    return;
  }
  // 收集参数 + 验证
  const paramsEl = document.getElementById('mwDeployParams');
  const r = collectAndValidateParamsLocal(paramsEl, (mwdepDeployTpl.params || []));
  if (!r.ok) {
    if (resultEl) {
      resultEl.className = 'msg err';
      resultEl.textContent = t('mwdep.paramInvalid') + '：' + r.errors.join('；');
    }
    return;
  }
  const params = r.values;

  if (resultEl) { resultEl.className = 'msg'; resultEl.textContent = t('mwdep.loading'); }
  // 清空日志区
  const logEl = document.getElementById('mwDeployLog');
  if (logEl) logEl.textContent = '';
  api.deployMiddleware(mwdepDeployTpl.id, agentID, deployType, params).then(function (r) {
    if (!resultEl) return;
    if (r && r.s && r.s < 400 && r.j) {
      const taskId = (r.j.taskID || r.j.id || r.j.taskId || '');
      resultEl.className = 'msg ok';
      resultEl.textContent = t('mwdep.deploySuccess') + (taskId || JSON.stringify(r.j));
      // Phase 2：自动轮询任务状态并展示部署日志
      if (taskId && logEl) {
        logEl.textContent = t('osopt.polling') + '\n';
        if (mwdepDeployTimer) clearInterval(mwdepDeployTimer);
        mwdepDeployTimer = pollTaskResult(taskId, logEl, 3000, function (status) {
          if (status === 'completed') {
            resultEl.className = 'msg ok';
            resultEl.textContent = t('mwdep.deploySuccess') + taskId;
            // 部署成功后刷新实例列表
            loadMiddlewareInstances();
          } else if (status === 'failed') {
            resultEl.className = 'msg err';
            resultEl.textContent = t('mwdep.deployFail') + ' (task: ' + taskId + ')';
          }
          mwdepDeployTimer = null;
        });
      } else {
        // 无 taskID 或无日志区，延迟关闭对话框并刷新实例列表
        setTimeout(function () { closeMwDeployModal(); loadMiddlewareInstances(); }, 2000);
      }
    } else {
      resultEl.className = 'msg err';
      resultEl.textContent = t('mwdep.deployFail') + ': [' + (r && r.s || '?') + '] ' + (r && r.j ? JSON.stringify(r.j) : '');
    }
  }).catch(function (e) {
    console.error('[mw-deploy]', e);
    if (resultEl) {
      resultEl.className = 'msg err';
      resultEl.textContent = t('mwdep.deployFail') + ': ' + (e && e.message || e);
    }
  });
}

// 本地参数收集与校验（与 flow_os.js 的 collectAndValidateParams 逻辑一致）
function collectAndValidateParamsLocal(paramsEl, params) {
  const values = {};
  const errors = [];
  if (!paramsEl || !params) return { ok: true, values: values };
  const inputs = paramsEl.querySelectorAll('input[data-pname]');
  inputs.forEach(function (inp) {
    const k = inp.getAttribute('data-pname');
    if (!k) return;
    values[k] = inp.value;
  });
  (params || []).forEach(function (p) {
    const name = p.name || '';
    const err = validateParamValueLocal(p, values[name] || '');
    if (err) errors.push(name + ': ' + err);
  });
  if (paramsEl) {
    (params || []).forEach(function (p) {
      const name = p.name || '';
      const inputId = (paramsEl.querySelector('input[data-pname="' + name + '"]'));
      if (inputId) {
        const err = validateParamValueLocal(p, values[name] || '');
        if (err) {
          inputId.style.borderColor = 'var(--fail)';
          inputId.style.borderWidth = '2px';
        } else {
          inputId.style.borderColor = '';
          inputId.style.borderWidth = '';
        }
      }
    });
  }
  return { ok: errors.length === 0, values: values, errors: errors };
}

// 本地参数值校验（与 flow_os.js 的 validateParamValue 逻辑一致）
function validateParamValueLocal(p, value) {
  const name = p.name || '';
  const v = (value || '').trim();
  if (p.required && !v) {
    return t('param.required');
  }
  if (!v) return null;
  const nLower = (name || '').toLowerCase();
  const isPort = nLower.indexOf('port') >= 0;
  const isPath = nLower.indexOf('dir') >= 0 || nLower.indexOf('path') >= 0;
  const isPwd = nLower.indexOf('password') >= 0 || nLower.indexOf('pwd') >= 0;
  if (isPort || p.type === 'int') {
    if (!/^-?\d+$/.test(v)) return t('osopt.invalidPort');
    const n = parseInt(v, 10);
    if (isNaN(n) || n < 1 || n > 65535) return t('osopt.invalidPort');
  }
  if (isPath) {
    if (v.charAt(0) !== '/') return t('osopt.invalidPath');
  }
  if ((p.type === 'password' || isPwd) && p.required) {
    // 密码强度检查
    const s = passwordStrengthLocal(v);
    if (s === 'weak') return t('param.passwordWeak');
  }
  return null;
}

// 本地密码强度计算（与 flow_os.js 的 passwordStrength 逻辑一致）
function passwordStrengthLocal(v) {
  if (!v) return null;
  const hasLower = /[a-z]/.test(v);
  const hasUpper = /[A-Z]/.test(v);
  const hasDigit = /\d/.test(v);
  const hasSpecial = /[^a-zA-Z0-9]/.test(v);
  if (v.length < 8) return 'weak';
  if (hasLower && hasUpper && hasDigit && hasSpecial) return 'strong';
  if (hasLower && hasUpper && hasDigit) return 'medium';
  return 'weak';
}

// 加载已部署实例列表
// Phase 2 改造：在每行添加"卸载"按钮
export function loadMiddlewareInstances() {
  const listEl = document.getElementById('mwInstanceList');
  if (!listEl) return;
  listEl.innerHTML = '<p class="muted">' + esc(t('mwdep.loading')) + '</p>';
  api.getMiddlewareInstances().then(function (list) {
    const el = document.getElementById('mwInstanceList');
    if (!el) return;
    if (!list || list.length === 0) {
      el.innerHTML = '<p class="muted">' + esc(t('mwdep.noInstances')) + '</p>';
      return;
    }
    let html = '<div class="table-wrap"><table><colgroup>'
      + '<col style="width:16%"><col style="width:12%"><col style="width:16%"><col style="width:12%"><col style="width:12%"><col style="width:18%"><col style="width:14%">'
      + '</colgroup><thead><tr>'
      + '<th>' + esc(t('mwdep.instance.col.id')) + '</th>'
      + '<th>' + esc(t('mwdep.instance.col.template')) + '</th>'
      + '<th>' + esc(t('mwdep.instance.col.agent')) + '</th>'
      + '<th>' + esc(t('mwdep.instance.col.deployType')) + '</th>'
      + '<th>' + esc(t('mwdep.instance.col.status')) + '</th>'
      + '<th>' + esc(t('mwdep.instance.col.createdAt')) + '</th>'
      + '<th>' + esc(t('mwdep.instance.col.action')) + '</th>'
      + '</tr></thead><tbody>';
    list.forEach(function (ins) {
      const status = ins.status || '-';
      const statusBadge = status === 'running' || status === 'ok' || status === 'success'
        ? '<span class="badge" style="background:var(--green-soft,#e6f9ee);color:var(--green)">' + esc(status) + '</span>'
        : (status === 'failed' || status === 'error'
          ? '<span class="badge" style="background:var(--fail-soft,#fde8e8);color:var(--fail)">' + esc(status) + '</span>'
          : '<span class="badge" style="background:var(--bg-2);color:var(--text-2)">' + esc(status) + '</span>');
      // 卸载按钮：仅对运行中/成功的实例显示，避免对已卸载/失败的实例再次卸载
      const canUninstall = status === 'running' || status === 'ok' || status === 'success' || status === 'deployed' || status === 'installed';
      const insId = esc(ins.id || '');
      const agentID = esc(ins.agentID || ins.agentId || '');
      const deployType = esc(ins.deployType || ins.deploy_type || '');
      const actionHtml = canUninstall
        ? '<button class="btn btn-sm" style="color:var(--fail);border:1px solid var(--fail)" onclick="uninstallMiddlewareInstance(\'' + escAttr(ins.id || '') + '\',\'' + escAttr(ins.agentID || ins.agentId || '') + '\',\'' + escAttr(ins.deployType || ins.deploy_type || '') + '\')">' + icon('close', 12) + ' ' + esc(t('mwdep.uninstall')) + '</button>'
        : '<span class="muted">—</span>';
      html += '<tr>'
        + '<td><code title="' + insId + '">' + insId + '</code></td>'
        + '<td>' + esc(ins.templateID || ins.templateId || ins.template || '-') + '</td>'
        + '<td>' + agentID + '</td>'
        + '<td>' + mwdepDeployTypeBadge(ins.deployType || ins.deploy_type) + '</td>'
        + '<td>' + statusBadge + '</td>'
        + '<td>' + esc(fmtTime(ins.createdAt || ins.created_at || '')) + '</td>'
        + '<td>' + actionHtml + '</td>'
        + '</tr>';
    });
    html += '</tbody></table></div>';
    el.innerHTML = html;
  }).catch(function (e) {
    console.error('[mw-instances]', e);
    const el = document.getElementById('mwInstanceList');
    if (el) el.innerHTML = '<p class="muted">' + esc(t('mwdep.networkError')) + '</p>';
  });
}

// ---------- 中间件卸载（Phase 2） ----------
// 打开卸载对话框：传入实例 ID / agentID / deployType，弹出确认对话框
export function uninstallMiddlewareInstance(instanceID, agentID, deployType) {
  mwdepUninstallIns = { id: instanceID, agentID: agentID, deployType: deployType };
  const modal = document.getElementById('mwUninstallModal');
  if (!modal) {
    // 兜底：若 HTML 未提供卸载对话框，使用 confirm 直接卸载
    if (!confirm(t('mwdep.uninstallConfirm'))) return;
    doUninstallMiddleware();
    return;
  }
  // 重置结果区与日志区
  const resultEl = document.getElementById('mwUninstallResult');
  if (resultEl) { resultEl.innerHTML = ''; resultEl.className = ''; }
  const logEl = document.getElementById('mwUninstallLog');
  if (logEl) logEl.textContent = '';
  // 填充实例信息
  const idEl = document.getElementById('mwUninstallInsId');
  if (idEl) idEl.textContent = instanceID || '-';
  const agentEl = document.getElementById('mwUninstallAgentId');
  if (agentEl) agentEl.textContent = agentID || '-';
  const typeEl = document.getElementById('mwUninstallDeployType');
  if (typeEl) typeEl.textContent = deployType || '-';
  modal.classList.add('open');
}

// 关闭卸载对话框
export function closeMwUninstallModal() {
  const modal = document.getElementById('mwUninstallModal');
  if (modal) modal.classList.remove('open');
  if (mwdepUninstallTimer) { clearInterval(mwdepUninstallTimer); mwdepUninstallTimer = null; }
  mwdepUninstallIns = null;
}

// 执行卸载：调用 API 并轮询任务状态
function doUninstallMiddleware() {
  if (!mwdepUninstallIns || !mwdepUninstallIns.id) return;
  const resultEl = document.getElementById('mwUninstallResult');
  const logEl = document.getElementById('mwUninstallLog');
  if (resultEl) { resultEl.className = 'msg'; resultEl.textContent = t('mwdep.uninstalling'); }
  if (logEl) logEl.textContent = '';
  api.uninstallMiddleware(mwdepUninstallIns.id, mwdepUninstallIns.agentID, mwdepUninstallIns.deployType).then(function (r) {
    if (!resultEl) return;
    if (r && r.s && r.s < 400 && r.j) {
      const taskId = (r.j.taskID || r.j.id || r.j.taskId || '');
      resultEl.className = 'msg ok';
      resultEl.textContent = t('mwdep.uninstalling') + (taskId ? (' (task: ' + taskId + ')') : '');
      // 轮询卸载任务状态
      if (taskId && logEl) {
        logEl.textContent = t('osopt.polling') + '\n';
        if (mwdepUninstallTimer) clearInterval(mwdepUninstallTimer);
        mwdepUninstallTimer = pollTaskResult(taskId, logEl, 3000, function (status) {
          if (status === 'completed') {
            resultEl.className = 'msg ok';
            resultEl.textContent = t('mwdep.uninstallSuccess');
            loadMiddlewareInstances();
          } else if (status === 'failed') {
            resultEl.className = 'msg err';
            resultEl.textContent = t('mwdep.uninstallFailed') + ' (task: ' + taskId + ')';
          }
          mwdepUninstallTimer = null;
        });
      } else {
        // 无 taskID：延迟关闭并刷新
        setTimeout(function () { closeMwUninstallModal(); loadMiddlewareInstances(); }, 1500);
      }
    } else {
      resultEl.className = 'msg err';
      resultEl.textContent = t('mwdep.uninstallFailed') + ': [' + (r && r.s || '?') + '] ' + (r && r.j ? JSON.stringify(r.j) : '');
    }
  }).catch(function (e) {
    console.error('[mw-uninstall]', e);
    if (resultEl) {
      resultEl.className = 'msg err';
      resultEl.textContent = t('mwdep.uninstallFailed') + ': ' + (e && e.message || e);
    }
  });
}

// 确认卸载：从对话框触发
export function confirmMwUninstall() {
  doUninstallMiddleware();
}