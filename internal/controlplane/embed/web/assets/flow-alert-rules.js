// flow-alert-rules.js — 告警规则管理编排（P0 补齐功能域）。

// flow 子模块 — 告警规则管理（规则 CRUD / 多条件引擎 / 静默规则）。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, \$, pageRoot } from './flow-state.js';

// ============================================================================
// 告警规则管理
// ============================================================================

function alertRulesContent() { return $('alert-rules-content'); }

// --- 告警规则 ---

// loadAlertRules 加载告警规则列表。
export async function loadAlertRules() {
  const content = alertRulesContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const rules = await api.getAlertRules();
    state.alertRules = rules;
    render.renderAlertRulesTable(content, rules, {
      onEdit: (r) => editAlertRule(r),
      onDelete: (id) => deleteAlertRule(id),
    });
  } catch (err) {
    render.renderError(content, t('alertRules.loadFailed') + ': ' + err.message);
  }
}

// createAlertRule 打开创建告警规则表单。
export function createAlertRule() {
  const content = alertRulesContent();
  if (!content) return;
  render.renderAlertRuleForm(content, null, {
    onSubmit: async (data) => {
      if (!data.name || !data.name.trim()) {
        render.renderToast(t('alertRules.nameRequired'), 'warn');
        return;
      }
      if (!data.expr || !data.expr.trim()) {
        render.renderToast(t('alertRules.exprRequired'), 'warn');
        return;
      }
      try {
        await api.createAlertRule(data);
        render.renderToast(t('alertRules.created'), 'success');
        loadAlertRules();
      } catch (err) {
        render.renderToast(t('alertRules.createFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => loadAlertRules(),
  });
}

// editAlertRule 打开编辑告警规则表单（先拉详情）。
export async function editAlertRule(rule) {
  const content = alertRulesContent();
  if (!content) return;
  const id = rule && rule.id;
  if (!id) { render.renderToast(t('alertRules.loadFailed'), 'error'); return; }
  render.renderLoading(content);
  try {
    const detail = await api.getAlertRule(id);
    render.renderAlertRuleForm(content, detail, {
      onSubmit: async (data) => {
        if (!data.name || !data.name.trim()) {
          render.renderToast(t('alertRules.nameRequired'), 'warn');
          return;
        }
        try {
          await api.updateAlertRule(id, data);
          render.renderToast(t('alertRules.updated'), 'success');
          loadAlertRules();
        } catch (err) {
          render.renderToast(t('alertRules.updateFailed') + ': ' + err.message, 'error');
        }
      },
      onCancel: () => loadAlertRules(),
    });
  } catch (err) {
    render.renderError(content, t('alertRules.loadFailed') + ': ' + err.message);
  }
}

// deleteAlertRule 删除告警规则。
export async function deleteAlertRule(id) {
  if (!window.confirm(t('alertRules.confirmDelete'))) return;
  try {
    await api.deleteAlertRule(id);
    render.renderToast(t('alertRules.deleted'), 'success');
    loadAlertRules();
  } catch (err) {
    render.renderToast(t('alertRules.deleteFailed') + ': ' + err.message, 'error');
  }
}

// --- 多条件引擎 ---

// loadAlertEngine 加载多条件引擎规则列表。
export async function loadAlertEngine() {
  const content = alertRulesContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const rules = await api.getAlertRulesEngine();
    state.alertRulesEngine = rules;
    render.renderAlertEngineTable(content, rules, {
      onEdit: (r) => editAlertEngine(r),
      onDelete: (id) => deleteAlertEngine(id),
    });
  } catch (err) {
    render.renderError(content, t('alertRules.engineLoadFailed') + ': ' + err.message);
  }
}

// createAlertEngine 打开创建引擎规则表单。
export function createAlertEngine() {
  const content = alertRulesContent();
  if (!content) return;
  render.renderAlertEngineForm(content, null, {
    onSubmit: async (data) => {
      if (!data.name || !data.name.trim()) {
        render.renderToast(t('alertRules.nameRequired'), 'warn');
        return;
      }
      try {
        await api.createAlertRuleEngine(data);
        render.renderToast(t('alertRules.engineCreated'), 'success');
        loadAlertEngine();
      } catch (err) {
        render.renderToast(t('alertRules.engineCreateFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => loadAlertEngine(),
  });
}

// editAlertEngine 打开编辑引擎规则表单（先拉详情）。
export async function editAlertEngine(rule) {
  const content = alertRulesContent();
  if (!content) return;
  const id = rule && rule.id;
  if (!id) { render.renderToast(t('alertRules.engineLoadFailed'), 'error'); return; }
  render.renderLoading(content);
  try {
    const detail = await api.getAlertRuleEngine(id);
    render.renderAlertEngineForm(content, detail, {
      onSubmit: async (data) => {
        if (!data.name || !data.name.trim()) {
          render.renderToast(t('alertRules.nameRequired'), 'warn');
          return;
        }
        try {
          await api.updateAlertRuleEngine(id, data);
          render.renderToast(t('alertRules.engineUpdated'), 'success');
          loadAlertEngine();
        } catch (err) {
          render.renderToast(t('alertRules.engineUpdateFailed') + ': ' + err.message, 'error');
        }
      },
      onCancel: () => loadAlertEngine(),
    });
  } catch (err) {
    render.renderError(content, t('alertRules.engineLoadFailed') + ': ' + err.message);
  }
}

// deleteAlertEngine 删除引擎规则。
export async function deleteAlertEngine(id) {
  if (!window.confirm(t('alertRules.confirmDelete'))) return;
  try {
    await api.deleteAlertRuleEngine(id);
    render.renderToast(t('alertRules.engineDeleted'), 'success');
    loadAlertEngine();
  } catch (err) {
    render.renderToast(t('alertRules.engineDeleteFailed') + ': ' + err.message, 'error');
  }
}

// --- 静默规则 ---

// loadAlertSilences 加载静默规则列表。
export async function loadAlertSilences() {
  const content = alertRulesContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const silences = await api.getAlertSilences();
    state.alertSilences = silences;
    render.renderAlertSilencesTable(content, silences, {
      onDelete: (id) => deleteAlertSilence(id),
    });
  } catch (err) {
    render.renderError(content, t('alertRules.silenceLoadFailed') + ': ' + err.message);
  }
}

// createAlertSilence 打开创建静默规则表单。
export function createAlertSilence() {
  const content = alertRulesContent();
  if (!content) return;
  render.renderAlertSilenceCreateForm(content, {
    onSubmit: async (data) => {
      if (!data.matchers || !data.matchers.length) {
        render.renderToast(t('alertRules.silenceMatchersRequired'), 'warn');
        return;
      }
      if (!data.endsAt) {
        render.renderToast(t('alertRules.silenceEndRequired'), 'warn');
        return;
      }
      try {
        await api.createAlertSilence(data);
        render.renderToast(t('alertRules.silenceCreated'), 'success');
        loadAlertSilences();
      } catch (err) {
        render.renderToast(t('alertRules.silenceCreateFailed') + ': ' + err.message, 'error');
      }
    },
    onCancel: () => loadAlertSilences(),
  });
}

// deleteAlertSilence 删除静默规则。
export async function deleteAlertSilence(id) {
  if (!window.confirm(t('alertRules.silenceConfirmDelete'))) return;
  try {
    await api.deleteAlertSilence(id);
    render.renderToast(t('alertRules.silenceDeleted'), 'success');
    loadAlertSilences();
  } catch (err) {
    render.renderToast(t('alertRules.silenceDeleteFailed') + ': ' + err.message, 'error');
  }
}

// --- 工具栏与子 tab ---

// refreshAlertRulesSubTab 按当前子 tab 刷新告警规则页。
export function refreshAlertRulesSubTab() {
  if (state.alertRulesSubTab === 'engine') loadAlertEngine();
  else if (state.alertRulesSubTab === 'silences') loadAlertSilences();
  else loadAlertRules();
}

// buildAlertRulesToolbar 构建告警规则工具栏（子 tab + 创建 + 刷新）。
export function buildAlertRulesToolbar() {
  const toolbar = $('alert-rules-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  // 子 tab 切换组
  const subTabs = [
    { key: 'rules', label: t('alertRules.tabRules'), onActivate: () => loadAlertRules(), onCreate: () => createAlertRule() },
    { key: 'engine', label: t('alertRules.tabEngine'), onActivate: () => loadAlertEngine(), onCreate: () => createAlertEngine() },
    { key: 'silences', label: t('alertRules.tabSilences'), onActivate: () => loadAlertSilences(), onCreate: () => createAlertSilence() },
  ];
  subTabs.forEach((st) => {
    toolbar.appendChild(
      render.el('button', {
        class: 'btn ' + (state.alertRulesSubTab === st.key ? 'btn-secondary' : 'btn-ghost'),
        onclick: () => { state.alertRulesSubTab = st.key; st.onActivate(); buildAlertRulesToolbar(); },
      }, render.el('span', { text: st.label }))
    );
  });
  // 创建按钮（按当前子 tab）
  const current = subTabs.find((st) => st.key === state.alertRulesSubTab) || subTabs[0];
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-primary', onclick: () => current.onCreate() },
      iconEl('plus', 16), render.el('span', { text: t('common.create') })
    )
  );
  // 刷新
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => refreshAlertRulesSubTab() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}