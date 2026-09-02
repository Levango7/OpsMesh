// flow-schedules.js — 定时任务编排（P2 补齐功能域）。

// flow 子模块 — 定时任务（列表 / 创建 / 编辑 / 删除 / 启用禁用）。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, $, pageRoot } from './flow-state.js';

// ============================================================================
// 定时任务
// ============================================================================

function schedulesContent() { return $('schedules-content'); }

// loadSchedules 加载定时任务列表。
export async function loadSchedules() {
  const content = schedulesContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const data = await api.getSchedules();
    const list = (data && data.schedules) ? data.schedules : (Array.isArray(data) ? data : []);
    state.schedules.list = list;
    render.renderSchedulesTable(content, list, {
      onEdit: (id) => editSchedule(id),
      onDelete: (id) => deleteSchedule(id),
      onToggle: (id, enabled) => toggleSchedule(id, enabled),
    });
  } catch (err) {
    render.renderError(content, t('schedules.loadFailed') + ': ' + err.message);
  }
}

// showScheduleForm 打开创建定时任务表单。
export function showScheduleForm() {
  const content = schedulesContent();
  if (!content) return;
  render.renderScheduleForm(content, null, {
    onSubmit: async (data) => {
      if (!data.name) {
        render.renderToast(t('schedules.nameRequired'), 'warn');
        return;
      }
      if (!data.cron) {
        render.renderToast(t('schedules.cronRequired'), 'warn');
        return;
      }
      try {
        await api.createSchedule(data);
        render.renderToast(t('schedules.created'), 'success');
        loadSchedules();
      } catch (err) {
        render.renderToast(t('schedules.createFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => loadSchedules(),
  });
}

// editSchedule 打开编辑定时任务表单。
export function editSchedule(id) {
  const content = schedulesContent();
  if (!content) return;
  const schedule = (state.schedules.list || []).find((s) => String(s.id) === String(id));
  if (!schedule) {
    render.renderToast(t('schedules.notFound'), 'warn');
    return;
  }
  render.renderScheduleForm(content, schedule, {
    onSubmit: async (data) => {
      if (!data.name) {
        render.renderToast(t('schedules.nameRequired'), 'warn');
        return;
      }
      if (!data.cron) {
        render.renderToast(t('schedules.cronRequired'), 'warn');
        return;
      }
      try {
        await api.updateSchedule(id, data);
        render.renderToast(t('schedules.updated'), 'success');
        loadSchedules();
      } catch (err) {
        render.renderToast(t('schedules.updateFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => loadSchedules(),
  });
}

// deleteSchedule 删除定时任务（确认后调用 API）。
export async function deleteSchedule(id) {
  if (!window.confirm(t('schedules.confirmDelete'))) return;
  try {
    await api.deleteSchedule(id);
    render.renderToast(t('schedules.deleted'), 'success');
    loadSchedules();
  } catch (err) {
    render.renderToast(t('schedules.deleteFailed') + ': ' + err.message, 'error');
  }
}

// toggleSchedule 启用/禁用定时任务。
export async function toggleSchedule(id, enabled) {
  try {
    // 通过 update 切换 enabled 字段
    const schedule = (state.schedules.list || []).find((s) => String(s.id) === String(id));
    if (!schedule) {
      render.renderToast(t('schedules.notFound'), 'warn');
      return;
    }
    await api.updateSchedule(id, Object.assign({}, schedule, { enabled: !!enabled }));
    render.renderToast(enabled ? t('schedules.enabled') : t('schedules.disabled'), 'success');
    loadSchedules();
  } catch (err) {
    render.renderToast(t('schedules.toggleFailed') + ': ' + err.message, 'error');
  }
}

// refreshSchedulesSubTab 刷新定时任务页（仅列表视图）。
export function refreshSchedulesSubTab() {
  loadSchedules();
}

// buildSchedulesToolbar 构建定时任务工具栏（创建按钮 + 刷新）。
export function buildSchedulesToolbar() {
  const toolbar = $('schedules-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  // 创建按钮
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-primary', onclick: () => showScheduleForm() },
      iconEl('plus', 16), render.el('span', { text: t('schedules.create') })
    )
  );
  // 刷新
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => loadSchedules() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}