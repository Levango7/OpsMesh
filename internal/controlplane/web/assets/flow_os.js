// flow_os.js — OS 基础环境优化
// 从 flow.js 拆分（P2-1）。职责：OS 优化模板列表加载、分类筛选、详情查看、执行、
//       通用任务结果轮询（pollTaskResult）、参数验证辅助函数。
// 依赖：api.js、render.js（esc/escAttr）、icons.js、i18n.js。
// 后端契约：
//   GET  /api/v1/os-templates[?category=] → OSTemplate[]
//   GET  /api/v1/os-templates/{id}         → OSTemplate
//   POST /api/v1/os-templates/{id}/execute {agentID, params[]} → task

import * as api from './api.js';
import { esc, escAttr } from './render.js';
import { icon } from './icons.js';
import { t } from './i18n.js';

// 当前分类筛选（模块级状态）
let osoptCurrentCat = '';
// 当前待执行的模板 ID（执行对话框用）
let osoptExecId = '';
// 当前待执行的模板详情（用于参数动态渲染，Phase 2）
let osoptExecTpl = null;
// 当前执行轮询定时器（用于在关闭对话框时停止轮询）
let osoptExecTimer = null;

// ---------- 任务结果轮询（执行/部署/卸载日志展示） ----------
// 通用轮询：每 interval 毫秒调用 api.getTaskDetail(taskID) 获取任务状态与输出，
// 将 stdout/stderr 写入 logEl；任务完成（completed/failed）或达到最大轮询次数后停止。
// 参数：
//   taskID   - 任务 ID
//   logEl    - 日志展示 DOM 元素（<pre>）
//   interval - 轮询间隔（毫秒），默认 3000
//   onDone   - 完成回调（可选），签名 function(status) 其中 status 为 completed/failed/timeout
// 返回：定时器句柄（可 clearInterval）
//
// Phase 2 增强：在日志头部展示任务状态（pending/running/completed/failed），
// 并保留 output 主体；状态行使用 osopt.taskStatus.* 翻译键。
export function pollTaskResult(taskID, logEl, interval, onDone) {
  let pollCount = 0;
  const maxPolls = 40; // 最多轮询 40 次（约 120 秒，适配长任务）
  // 状态 → 翻译键映射（含图标前缀，便于一眼识别）
  function statusLine(st) {
    const key = 'osopt.taskStatus.' + (st || 'pending');
    const label = t(key);
    if (st === 'completed') return '✓ [' + label + ']';
    if (st === 'failed')    return '✗ [' + label + ']';
    if (st === 'running')   return '⏳ [' + label + ']';
    return '… [' + label + ']';
  }
  const timer = setInterval(function () {
    pollCount++;
    api.getTaskDetail(taskID).then(function (r) {
      // getTaskDetail 通过 authGet 返回解析后的 json（任务对象）
      const task = r && r.task ? r.task : r;
      if (!task) return;
      const st = task.status || '';
      // 拼接日志：状态行 + output（stdout/stderr 拼接）
      if (logEl) {
        const output = String(task.output || '');
        logEl.textContent = statusLine(st) + '\n' + output;
      }
      // 检查是否结束
      if (st === 'completed' || st === 'failed' || pollCount >= maxPolls) {
        clearInterval(timer);
        if (logEl) {
          if (st === 'completed') {
            logEl.textContent += '\n✓ ' + t('osopt.execSuccess');
          } else if (st === 'failed') {
            logEl.textContent += '\n✗ ' + t('osopt.execFailed');
          } else {
            logEl.textContent += '\n⚠ ' + t('osopt.pollTimeout');
          }
        }
        if (typeof onDone === 'function') {
          onDone(st === 'completed' ? 'completed' : (st === 'failed' ? 'failed' : 'timeout'));
        }
      }
    }).catch(function (e) {
      // 网络错误，继续轮询直到 maxPolls
      console.error('[pollTask]', e);
      if (pollCount >= maxPolls) {
        clearInterval(timer);
        if (logEl) {
          logEl.textContent += '\n⚠ ' + t('osopt.pollTimeout');
        }
        if (typeof onDone === 'function') onDone('timeout');
      }
    });
  }, interval || 3000);
  return timer;
}

// ---------- 参数验证辅助函数 ----------
// 判断参数名是否为端口类型（包含 port）
function isPortParam(name) {
  return (name || '').toLowerCase().indexOf('port') >= 0;
}
// 判断参数名是否为路径类型（包含 dir 或 path）
function isPathParam(name) {
  const n = (name || '').toLowerCase();
  return n.indexOf('dir') >= 0 || n.indexOf('path') >= 0;
}
// 判断参数名是否为密码类型（包含 password 或 pwd）
function isPasswordParam(name) {
  const n = (name || '').toLowerCase();
  return n.indexOf('password') >= 0 || n.indexOf('pwd') >= 0;
}

// Phase 2：密码强度计算
// 规则：长度<8 → weak；长度>=8 且含大小写+数字 → medium；含大小写+数字+特殊字符 且长度>=8 → strong
// 返回 'weak' / 'medium' / 'strong' / null（空值返回 null）
function passwordStrength(v) {
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

// Phase 2：参数输入实时反馈（oninput 触发）
// - 必填：未填写时红色边框 + 提示，填写后恢复
// - 密码：实时显示强度（弱/中/强）
// - 端口：超出范围时红色边框
// 通过 window._opsmeshParamOnInput 暴露给内联 oninput 调用
function paramInputOnInput(inp) {
  if (!inp) return;
  const name = inp.getAttribute('data-pname') || '';
  const v = inp.value || '';
  // 必填红色边框
  const required = inp.hasAttribute('required');
  if (required && !v.trim()) {
    inp.style.borderColor = 'var(--fail)';
    inp.style.borderWidth = '2px';
  } else {
    inp.style.borderColor = '';
    inp.style.borderWidth = '';
  }
  // 端口范围红色边框
  if (isPortParam(name) && v) {
    const n = parseInt(v, 10);
    if (isNaN(n) || n < 1 || n > 65535) {
      inp.style.borderColor = 'var(--fail)';
      inp.style.borderWidth = '2px';
    }
  }
  // 密码强度实时提示
  if (isPasswordParam(name)) {
    const strengthId = inp.id + '_strength';
    const el = document.getElementById(strengthId);
    if (el) {
      const s = passwordStrength(v);
      if (!s) {
        el.textContent = '';
        el.style.color = '';
      } else if (s === 'weak') {
        el.textContent = t('param.passwordWeak');
        el.style.color = 'var(--fail)';
      } else if (s === 'medium') {
        el.textContent = t('param.passwordMedium');
        el.style.color = 'var(--accent)';
      } else {
        el.textContent = t('param.passwordStrong');
        el.style.color = 'var(--green)';
      }
    }
  }
}
// 暴露到 window 供内联 oninput 调用（ES6 模块作用域隔离）
try { window._opsmeshParamOnInput = paramInputOnInput; } catch (_) {}

// 验证单个参数值：返回 null 表示通过，否则返回错误消息
function validateParamValue(p, value) {
  const name = p.name || '';
  const v = (value || '').trim();
  // 必填校验
  if (p.required && !v) {
    return t('param.required');
  }
  if (!v) return null; // 非必填且为空，跳过后续校验
  // 端口校验
  if (isPortParam(name) || p.type === 'int') {
    if (!/^-?\d+$/.test(v)) return t('osopt.invalidPort');
    const n = parseInt(v, 10);
    if (isNaN(n) || n < 1 || n > 65535) return t('osopt.invalidPort');
  }
  // 路径校验
  if (isPathParam(name)) {
    if (v.charAt(0) !== '/') return t('osopt.invalidPath');
  }
  // 密码强度校验：必填密码至少要求 medium（避免弱密码提交）
  if ((p.type === 'password' || isPasswordParam(name)) && p.required) {
    const s = passwordStrength(v);
    if (s === 'weak') return t('param.passwordWeak');
  }
  return null;
}

// 渲染单个参数输入框（含 label / hint / required 标记 / 实时验证反馈）
// 用于 OS 优化执行对话框与中间件部署对话框的参数表单。
// prefix - DOM id 前缀，避免重复（'os' / 'mw'）
function renderParamInput(p, prefix) {
  const name = p.name || '';
  const def = p.default != null ? String(p.default) : '';
  const required = p.required ? ' <span style="color:var(--fail)">*</span>' : '';
  const placeholder = p.description || '';
  const type = p.type || 'text';
  // 输入框类型：int → number；password → password（Phase 2 暴露密码强度提示）
  let inputType = 'text';
  if (type === 'int' || isPortParam(name)) inputType = 'number';
  else if (type === 'password' || isPasswordParam(name)) inputType = 'password';
  // 端口范围 min/max
  let rangeAttr = '';
  if (inputType === 'number' && isPortParam(name)) {
    rangeAttr = ' min="1" max="65535"';
  }
  // 提示文本
  const hints = [];
  if (isPortParam(name)) hints.push(t('param.portRange'));
  if (isPathParam(name)) hints.push(t('osopt.pathHint'));
  if (isPasswordParam(name)) hints.push(t('osopt.passwordHint'));
  const hintHtml = hints.length ? ' <span class="field-hint" style="margin-left:4px;color:var(--text-3)">' + esc(hints.join(' · ')) + '</span>' : '';
  // 必填属性
  const requiredAttr = p.required ? ' required' : '';
  // 唯一 id（用于 label/input 关联与验证读取）
  const inputId = prefix + 'Param_' + esc(name).replace(/[^a-zA-Z0-9_]/g, '_');
  // Phase 2：密码强度提示元素（实时更新）
  const isPwd = (type === 'password' || isPasswordParam(name));
  const strengthHtml = isPwd
    ? ' <span id="' + inputId + '_strength" style="margin-left:6px;font-size:12px;font-weight:600"></span>'
    : '';
  // Phase 2：oninput 实时验证（必填红色边框 + 密码强度 + 端口范围）
  const onInputAttr = ' oninput="window._opsmeshParamOnInput && window._opsmeshParamOnInput(this)"';
  let html = '<div style="margin-bottom:8px">'
    + '<label style="display:block;margin-bottom:2px" for="' + inputId + '"><code>' + esc(name) + '</code>' + required + hintHtml + strengthHtml + '</label>'
    + '<input type="' + inputType + '"' + rangeAttr + requiredAttr + onInputAttr + ' class="form-control" id="' + inputId + '" data-pname="' + esc(name) + '" value="' + esc(def) + '" placeholder="' + esc(placeholder) + '">'
    + '</div>';
  return html;
}

// 收集参数表单值并执行校验。
//   paramsEl - 包含 input[data-pname] 的容器
//   params   - 模板参数定义数组（用于校验）
// 返回：{ok: true, values: {...}} 或 {ok: false, errors: ['xxx: msg', ...]}
// Phase 2：校验失败时对对应输入框添加红色边框，通过时清除红色边框
function collectAndValidateParams(paramsEl, params) {
  const values = {};
  const errors = [];
  if (!paramsEl || !params) return { ok: true, values: values };
  const inputs = paramsEl.querySelectorAll('input[data-pname]');
  inputs.forEach(function (inp) {
    const k = inp.getAttribute('data-pname');
    if (!k) return;
    values[k] = inp.value;
  });
  // 对每个定义的参数执行校验
  (params || []).forEach(function (p) {
    const name = p.name || '';
    const err = validateParamValue(p, values[name] || '');
    if (err) errors.push(name + ': ' + err);
  });
  // Phase 2：校验失败时对对应输入框添加红色边框，通过时清除
  if (paramsEl) {
    (params || []).forEach(function (p) {
      const name = p.name || '';
      const inputId = (paramsEl.querySelector('input[data-pname="' + name + '"]'));
      if (inputId) {
        const err = validateParamValue(p, values[name] || '');
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

// 风险等级 → 颜色 + 文本键
function osoptRiskStyle(risk) {
  if (risk === 'low') return { color: 'var(--green)', bg: 'var(--green-soft, #e6f9ee)' };
  if (risk === 'high') return { color: 'var(--fail)', bg: 'var(--fail-soft, #fde8e8)' };
  return { color: 'var(--accent)', bg: 'var(--accent-soft)' };
}

// 渲染风险等级标签
function osoptRiskBadge(risk) {
  const s = osoptRiskStyle(risk);
  return '<span class="badge" style="background:' + s.bg + ';color:' + s.color + ';border:1px solid ' + s.color + '">' + esc(t('osopt.risk.' + (risk || 'medium'))) + '</span>';
}

// 渲染分类标签
function osoptCatBadge(cat) {
  const key = 'osopt.category.' + (cat || 'all');
  return '<span class="badge" style="background:var(--bg-2);color:var(--text-2)">' + esc(t(key)) + '</span>';
}

// 加载 OS 优化模板列表（按当前分类筛选）
export function loadOSTemplates() {
  const listEl = document.getElementById('osTemplateList');
  if (listEl) listEl.innerHTML = '<p class="muted">' + esc(t('osopt.loading')) + '</p>';
  // 隐藏详情面板
  const detailEl = document.getElementById('osTemplateDetail');
  if (detailEl) detailEl.style.display = 'none';
  api.getOSTemplates(osoptCurrentCat).then(function (list) {
    const el = document.getElementById('osTemplateList');
    if (!el) return;
    if (!list || list.length === 0) {
      el.innerHTML = '<p class="muted">' + esc(t('osopt.noTemplates')) + '</p>';
      return;
    }
    let html = '<div class="table-wrap"><table><colgroup>'
      + '<col style="width:18%"><col style="width:26%"><col style="width:14%"><col style="width:14%"><col style="width:28%">'
      + '</colgroup><thead><tr>'
      + '<th>' + esc(t('osopt.col.id')) + '</th>'
      + '<th>' + esc(t('osopt.col.name')) + '</th>'
      + '<th>' + esc(t('osopt.col.category')) + '</th>'
      + '<th>' + esc(t('osopt.col.risk')) + '</th>'
      + '<th>' + esc(t('osopt.col.action')) + '</th>'
      + '</tr></thead><tbody>';
    list.forEach(function (tpl) {
      const tid = esc(tpl.id || '');
      html += '<tr>'
        + '<td><code title="' + tid + '">' + tid + '</code></td>'
        + '<td>' + esc(tpl.name || '') + '</td>'
        + '<td>' + osoptCatBadge(tpl.category) + '</td>'
        + '<td>' + osoptRiskBadge(tpl.risk) + '</td>'
        + '<td>'
        + '<button class="btn btn-sm" onclick="showOSTemplateDetail(\'' + escAttr(tpl.id || '') + '\')" style="margin-right:6px">' + icon('search', 12) + ' ' + esc(t('osopt.view')) + '</button>'
        + '<button class="btn btn-primary btn-sm" onclick="executeOSOptimize(\'' + escAttr(tpl.id || '') + '\')">' + icon('task', 12) + ' ' + esc(t('osopt.execute')) + '</button>'
        + '</td>'
        + '</tr>';
    });
    html += '</tbody></table></div>';
    el.innerHTML = html;
  }).catch(function (e) {
    console.error('[os-templates]', e);
    const el = document.getElementById('osTemplateList');
    if (el) el.innerHTML = '<p class="muted">' + esc(t('osopt.networkError')) + '</p>';
  });
}

// 分类筛选：切换分类并重新加载
export function filterOSTemplates(category) {
  osoptCurrentCat = category || '';
  // 更新按钮 active 状态
  document.querySelectorAll('.osopt-cat-btn').forEach(function (btn) {
    btn.classList.toggle('active', (btn.getAttribute('data-cat') || '') === osoptCurrentCat);
  });
  loadOSTemplates();
}

// 显示模板详情
export function showOSTemplateDetail(id) {
  const detailEl = document.getElementById('osTemplateDetail');
  if (!detailEl) return;
  detailEl.style.display = '';
  detailEl.innerHTML = '<p class="muted">' + esc(t('osopt.loading')) + '</p>';
  api.getOSTemplate(id).then(function (tpl) {
    const el = document.getElementById('osTemplateDetail');
    if (!el) return;
    if (!tpl || !tpl.id) {
      el.innerHTML = '<p class="muted">' + esc(t('osopt.noTemplates')) + '</p>';
      return;
    }
    const tags = (tpl.tags || []).map(function (tag) { return '<span class="badge" style="background:var(--bg-2);color:var(--text-2);margin-right:4px">' + esc(tag) + '</span>'; }).join('');
    let html = '<div style="display:flex;justify-content:space-between;align-items:flex-start;flex-wrap:wrap;gap:8px;margin-bottom:12px">'
      + '<h4 style="margin:0">' + esc(tpl.name || tpl.id) + ' <code style="font-size:12px;color:var(--text-3)">' + esc(tpl.id) + '</code></h4>'
      + '<button class="btn btn-sm" onclick="hideOSTemplateDetail()">' + icon('close', 12) + ' ' + esc(t('osopt.detailClose')) + '</button>'
      + '</div>';
    html += '<div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:12px;margin-bottom:12px">'
      + '<div><div class="field-hint">' + esc(t('osopt.col.category')) + '</div><div>' + osoptCatBadge(tpl.category) + '</div></div>'
      + '<div><div class="field-hint">' + esc(t('osopt.col.risk')) + '</div><div>' + osoptRiskBadge(tpl.risk) + '</div></div>'
      + '<div><div class="field-hint">' + esc(t('osopt.detailOs')) + '</div><div>' + esc(tpl.os || 'all') + '</div></div>'
      + '<div><div class="field-hint">' + esc(t('osopt.detailTags')) + '</div><div>' + (tags || '-') + '</div></div>'
      + '</div>';
    html += '<div style="margin-bottom:12px"><div class="field-hint">' + esc(t('osopt.detailDesc')) + '</div><div>' + esc(tpl.description || '-') + '</div></div>';
    html += '<div><div class="field-hint" style="margin-bottom:4px">' + esc(t('osopt.detailCommands')) + '</div>'
      + '<pre style="background:var(--bg-2);color:var(--text-1);padding:12px;border-radius:6px;overflow:auto;max-height:320px;font-size:12px;line-height:1.5;white-space:pre-wrap;word-break:break-all">' + esc(tpl.commands || '') + '</pre>'
      + '</div>';
    html += '<div style="margin-top:12px;text-align:right">'
      + '<button class="btn btn-primary btn-sm" onclick="executeOSOptimize(\'' + escAttr(tpl.id) + '\')">' + icon('task', 12) + ' ' + esc(t('osopt.execute')) + '</button>'
      + '</div>';
    el.innerHTML = html;
  }).catch(function (e) {
    console.error('[os-template-detail]', e);
    const el = document.getElementById('osTemplateDetail');
    if (el) el.innerHTML = '<p class="muted">' + esc(t('osopt.networkError')) + '</p>';
  });
}

// 隐藏模板详情
export function hideOSTemplateDetail() {
  const el = document.getElementById('osTemplateDetail');
  if (el) { el.style.display = 'none'; el.innerHTML = ''; }
}

// 打开执行对话框：加载设备列表供选择 + 加载模板详情用于动态生成参数表单
export function executeOSOptimize(id) {
  osoptExecId = id;
  osoptExecTpl = null;
  const modal = document.getElementById('osExecModal');
  if (!modal) return;
  // 重置结果区
  const resultEl = document.getElementById('osExecResult');
  if (resultEl) { resultEl.innerHTML = ''; resultEl.className = ''; }
  // 重置日志区（Phase 2）
  const logEl = document.getElementById('osExecLog');
  if (logEl) logEl.textContent = '';
  // 停止上一次的轮询
  if (osoptExecTimer) { clearInterval(osoptExecTimer); osoptExecTimer = null; }
  // 加载模板详情用于动态生成参数表单（Phase 2 参数验证 UI）
  api.getOSTemplate(id).then(function (tpl) {
    osoptExecTpl = tpl || null;
    // 渲染动态参数表单
    renderOSExecParams();
  }).catch(function (e) {
    console.error('[os-exec template]', e);
    osoptExecTpl = null;
    renderOSExecParams();
  });
  // 加载设备列表到下拉
  const agentSel = document.getElementById('osExecAgent');
  if (agentSel) {
    agentSel.innerHTML = '<option value="">— ' + esc(t('osopt.selectAgent')) + ' —</option>';
    api.getDevices().then(function (devs) {
      if (!agentSel) return;
      // devs 可能是数组或按网段分组的对象，统一拍平为数组
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
    }).catch(function (e) { console.error('[os-exec devices]', e); });
  }
  modal.classList.add('open');
}

// 渲染 OS 优化执行参数表单（根据当前模板的 Params 定义动态生成）
function renderOSExecParams() {
  const paramsEl = document.getElementById('osExecParams');
  if (!paramsEl) return;
  const tpl = osoptExecTpl;
  if (!tpl || !tpl.params || tpl.params.length === 0) {
    // 无参数定义：保留 textarea 兼容旧契约（每行一个）
    paramsEl.innerHTML = '<textarea id="osExecParamsRaw" class="form-control" rows="3" placeholder="param1&#10;param2"></textarea>';
    return;
  }
  // 有参数定义：动态生成输入框（含验证提示）
  let html = '';
  tpl.params.forEach(function (p) {
    html += renderParamInput(p, 'os');
  });
  paramsEl.innerHTML = html;
}

// 关闭执行对话框：停止轮询并清理状态
export function closeOSExecModal() {
  const modal = document.getElementById('osExecModal');
  if (modal) modal.classList.remove('open');
  if (osoptExecTimer) { clearInterval(osoptExecTimer); osoptExecTimer = null; }
  osoptExecId = '';
  osoptExecTpl = null;
}

// 确认执行：调用 API 在选定设备上执行模板
// Phase 2 改造：提交前参数验证 + 提交后轮询任务状态 + 日志展示
export function confirmOSExec() {
  const resultEl = document.getElementById('osExecResult');
  if (!osoptExecId) {
    if (resultEl) { resultEl.className = 'msg err'; resultEl.textContent = t('osopt.execFail') + ': no template'; }
    return;
  }
  const agentSel = document.getElementById('osExecAgent');
  const agentID = agentSel ? agentSel.value : '';
  if (!agentID) {
    if (resultEl) { resultEl.className = 'msg err'; resultEl.textContent = t('osopt.noAgent'); }
    return;
  }
  // 收集参数 + 验证
  const paramsEl = document.getElementById('osExecParams');
  let params = [];
  if (osoptExecTpl && osoptExecTpl.params && osoptExecTpl.params.length > 0) {
    // 动态参数模式：收集并校验
    const r = collectAndValidateParams(paramsEl, osoptExecTpl.params);
    if (!r.ok) {
      if (resultEl) {
        resultEl.className = 'msg err';
        resultEl.textContent = t('osopt.paramInvalid') + '：' + r.errors.join('；');
      }
      return;
    }
    // 转为数组（按模板定义顺序）
    osoptExecTpl.params.forEach(function (p) {
      params.push(r.values[p.name] != null ? String(r.values[p.name]) : '');
    });
  } else {
    // 兼容旧契约：textarea 每行一个
    const rawEl = document.getElementById('osExecParamsRaw');
    const paramsText = rawEl ? rawEl.value : (paramsEl ? paramsEl.value : '');
    params = paramsText.split('\n').map(function (s) { return s.trim(); }).filter(function (s) { return s.length > 0; });
  }

  if (resultEl) { resultEl.className = 'msg'; resultEl.textContent = t('osopt.loading'); }
  // 清空日志区
  const logEl = document.getElementById('osExecLog');
  if (logEl) logEl.textContent = '';
  api.executeOSTemplate(osoptExecId, agentID, params).then(function (r) {
    if (!resultEl) return;
    if (r && r.s && r.s < 400 && r.j) {
      const taskId = (r.j.taskID || r.j.id || r.j.taskId || '');
      resultEl.className = 'msg ok';
      resultEl.textContent = t('osopt.execSuccess') + (taskId || JSON.stringify(r.j));
      // Phase 2：自动轮询任务状态并展示日志
      if (taskId && logEl) {
        logEl.textContent = t('osopt.polling') + '\n';
        if (osoptExecTimer) clearInterval(osoptExecTimer);
        osoptExecTimer = pollTaskResult(taskId, logEl, 3000, function (status) {
          if (status === 'completed') {
            resultEl.className = 'msg ok';
            resultEl.textContent = t('osopt.execSuccess') + taskId;
          } else if (status === 'failed') {
            resultEl.className = 'msg err';
            resultEl.textContent = t('osopt.execFailed') + ' (task: ' + taskId + ')';
          }
          osoptExecTimer = null;
        });
      } else {
        // 无 taskID 或无日志区，延迟关闭对话框
        setTimeout(closeOSExecModal, 2000);
      }
    } else {
      resultEl.className = 'msg err';
      resultEl.textContent = t('osopt.execFail') + ': [' + (r && r.s || '?') + '] ' + (r && r.j ? JSON.stringify(r.j) : '');
    }
  }).catch(function (e) {
    console.error('[os-exec]', e);
    if (resultEl) {
      resultEl.className = 'msg err';
      resultEl.textContent = t('osopt.execFail') + ': ' + (e && e.message || e);
    }
  });
}