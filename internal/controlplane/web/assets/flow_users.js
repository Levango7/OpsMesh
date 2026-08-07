// flow_users.js — 改密对话框（安全债 85）+ 用户相关
// 从 flow.js 拆分（P2-1）。职责：预置弱口令首登强制改密对话框。
// 依赖：api.js、render.js（esc/showModal/setModalMsg/closeModal）、i18n.js。
// 契约：登录响应 mustChangePassword=true 时由 main.js 调用 showChangePasswordModal(onOk)，
//       用户改密成功后回调 onOk() 继续进入主界面；取消则退出登录回登录页。

import * as api from './api.js';
import { esc, showModal, setModalMsg, closeModal } from './render.js';
import { t } from './i18n.js';

// showChangePasswordModal 弹出改密对话框。
// onOk: 改密成功后的回调（继续进入主界面）；onCancel: 取消回调（退出登录）。
export function showChangePasswordModal(onOk, onCancel) {
  const bodyHtml =
    '<p class="muted" style="margin-bottom:12px">' + esc(t('changePassword.hint')) + '</p>'
    + '<div class="form-row"><label>' + esc(t('changePassword.oldPassword')) + '</label>'
    + '<input id="cpOldPwd" type="password" autocomplete="current-password" /></div>'
    + '<div class="form-row"><label>' + esc(t('changePassword.newPassword')) + '</label>'
    + '<input id="cpNewPwd" type="password" autocomplete="new-password" /></div>'
    + '<div class="form-row"><label>' + esc(t('changePassword.confirmPassword')) + '</label>'
    + '<input id="cpConfirmPwd" type="password" autocomplete="new-password" /></div>'
    + '<p id="modalMsg" class="modal-msg"></p>'
    + '<p class="muted" style="margin-top:8px;font-size:12px">' + esc(t('changePassword.strengthHint')) + '</p>';
  const footerHtml =
    '<button class="btn btn-secondary" onclick="cancelChangePassword()">' + esc(t('changePassword.cancel')) + '</button>'
    + '<button class="btn btn-primary" onclick="submitChangePassword()">' + esc(t('changePassword.submit')) + '</button>';
  showModal(t('changePassword.title'), bodyHtml, footerHtml);
  // 把回调挂到 window，供内联 onclick 调用。
  window.__cpOnOk = typeof onOk === 'function' ? onOk : function () {};
  window.__cpOnCancel = typeof onCancel === 'function' ? onCancel : function () {};
}

// submitChangePassword 提交改密：前端校验 + 调用 API。
window.submitChangePassword = async function () {
  const oldPwd = document.getElementById('cpOldPwd').value;
  const newPwd = document.getElementById('cpNewPwd').value;
  const confirmPwd = document.getElementById('cpConfirmPwd').value;
  if (!oldPwd || !newPwd || !confirmPwd) {
    setModalMsg(t('changePassword.validationRequired'), false);
    return;
  }
  if (newPwd !== confirmPwd) {
    setModalMsg(t('changePassword.validationMismatch'), false);
    return;
  }
  if (newPwd.length < 8) {
    setModalMsg(t('changePassword.validationTooShort'), false);
    return;
  }
  if (!/[A-Z]/.test(newPwd) || !/[a-z]/.test(newPwd) || !/[0-9]/.test(newPwd)) {
    setModalMsg(t('changePassword.validationWeak'), false);
    return;
  }
  if (oldPwd === newPwd) {
    setModalMsg(t('changePassword.validationSame'), false);
    return;
  }
  setModalMsg(t('common.loading'), false);
  try {
    const r = await api.apiAuthChangePassword(oldPwd, newPwd);
    if (r.s === 200) {
      setModalMsg(t('changePassword.success'), true);
      const onOk = window.__cpOnOk;
      window.__cpOnOk = null; window.__cpOnCancel = null;
      setTimeout(function () { closeModal(); onOk(); }, 600);
    } else {
      const errMsg = (r.j && (r.j.error || r.j.message)) || ('HTTP ' + r.s);
      setModalMsg(t('changePassword.error') + errMsg, false);
    }
  } catch (e) {
    setModalMsg(t('common.networkError'), false);
    console.error('[change-password]', e);
  }
};

// cancelChangePassword 取消改密：关闭对话框并触发 onCancel 回调。
window.cancelChangePassword = function () {
  const onCancel = window.__cpOnCancel;
  window.__cpOnOk = null; window.__cpOnCancel = null;
  closeModal();
  if (typeof onCancel === 'function') onCancel();
};