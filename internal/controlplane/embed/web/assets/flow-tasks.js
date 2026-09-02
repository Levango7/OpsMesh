// flow-tasks.js — 任务执行编排（P0 补齐功能域）。

// flow 子模块 — 任务执行（任务列表 / 创建 / 取消 / 结果查看）。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, \$, pageRoot } from './flow-state.js';

// ============================================================================
// 任务执行
// ============================================================================

function tasksContent() { return $('tasks-content'); }

// loadTasks 加载任务列表（带当前过滤器）。
export async function loadTasks(filter) {
  if (filter) state.taskFilter = Object.assign(state.taskFilter || {}, filter);
  const content = tasksContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const tasks = await api.getTasks(state.taskFilter);
    state.tasks = tasks;
    render.renderTasksTable(content, tasks, {
      onResult: (id) => showTaskResult(id),
      onCancel: (id) => cancelTask(id),
    });
  } catch (err) {
    render.renderError(content, t('tasks.loadFailed') + ': ' + err.message);
  }
}

// createTask 打开创建任务表单。
export function createTask() {
  const content = tasksContent();
  if (!content) return;
  render.renderTaskForm(content, {
    onSubmit: async (data) => {
      if (!data.name || !data.name.trim()) {
        render.renderToast(t('tasks.nameRequired'), 'warn');
        return;
      }
      if (!data.deviceID || !data.deviceID.trim()) {
        render.renderToast(t('tasks.deviceRequired'), 'warn');
        return;
      }
      try {
        await api.createTask(data);
        render.renderToast(t('tasks.created'), 'success');
        loadTasks();
      } catch (err) {
        render.renderToast(t('tasks.createFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => loadTasks(),
  });
}

// cancelTask 取消任务（确认后调用 API）。
export async function cancelTask(id) {
  if (!window.confirm(t('tasks.confirmCancel'))) return;
  try {
    await api.cancelTask(id);
    render.renderToast(t('tasks.cancelled'), 'success');
    loadTasks();
  } catch (err) {
    render.renderToast(t('tasks.cancelFailed') + ': ' + err.message, 'error');
  }
}

// showTaskResult 查看任务结果。
export async function showTaskResult(id) {
  const content = tasksContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const result = await api.getTaskResult(id);
    render.renderTaskResult(content, id, result, {
      onBack: () => loadTasks(),
    });
  } catch (err) {
    render.renderError(content, t('tasks.resultLoadFailed') + ': ' + err.message);
  }
}

// buildTasksToolbar 构建任务工具栏（创建按钮 + 过滤器 + 刷新）。
export function buildTasksToolbar() {
  const toolbar = $('tasks-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  // 创建按钮
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-primary', onclick: () => createTask() },
      iconEl('plus', 16), render.el('span', { text: t('tasks.create') })
    )
  );
  // 过滤器组
  const filterGroup = render.el('div', { class: 'filter-group' });
  // 状态过滤
  filterGroup.appendChild(buildStatusFilter());
  // 刷新
  filterGroup.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => loadTasks() },
      iconEl('refresh', 14)
    )
  );
  toolbar.appendChild(filterGroup);
}

// buildStatusFilter 构建状态过滤下拉框。
function buildStatusFilter() {
  const options = ['pending', 'running', 'succeeded', 'failed', 'cancelled'];
  return render.el('select', { class: 'filter-select', onchange: (e) => loadTasks({ status: e.target.value }) },
    render.el('option', { value: '', text: t('tasks.filter.status') + ': ' + t('common.all') }),
    options.map((opt) => render.el('option', { value: opt, text: t('tasks.filter.status') + ': ' + t('tasks.status.' + opt) }))
  );
}