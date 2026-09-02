// flow-bot.js — ChatOps 编排（P2 补齐功能域）。

// flow 子模块 — ChatOps（命令输入 + 响应 + 历史记录 + 平台列表）。
// 公共依赖：flow-state（state/$/pageRoot）、api、render、i18n、icons。

import * as api from './api.js';
import * as render from './render.js';
import { t } from './i18n.js';
import { iconEl } from './icons.js';
import { state, $, pageRoot } from './flow-state.js';

// ============================================================================
// ChatOps
// ============================================================================

function botContent() { return $('bot-content'); }

// loadBotPlatforms 加载 ChatOps 平台列表。
export async function loadBotPlatforms() {
  try {
    const platforms = await api.getBotPlatforms();
    state.bot.platforms = platforms;
    return platforms;
  } catch (err) {
    state.bot.error = err.message;
    render.renderToast(t('bot.platformsLoadFailed') + ': ' + err.message, 'error');
    return [];
  }
}

// loadBotHistory 加载 ChatOps 历史记录。
export async function loadBotHistory() {
  const content = botContent();
  if (!content) return;
  render.renderLoading(content);
  try {
    const data = await api.getBotHistory();
    const history = (data && data.history) ? data.history : (Array.isArray(data) ? data : []);
    state.bot.history = history;
    render.renderBotHistoryTable(content, history);
  } catch (err) {
    render.renderError(content, t('bot.historyLoadFailed') + ': ' + err.message);
  }
}

// showBotCommandForm 打开 ChatOps 命令输入表单。
export function showBotCommandForm() {
  const content = botContent();
  if (!content) return;
  // 确保平台列表已加载
  const ensurePlatforms = async () => {
    if (state.bot.platforms.length === 0) {
      await loadBotPlatforms();
    }
    return state.bot.platforms;
  };
  ensurePlatforms().then((platforms) => {
    render.renderBotCommandForm(content, platforms, {
      onSubmit: async (data) => {
        if (!data.command) {
          render.renderToast(t('bot.commandRequired'), 'warn');
          return;
        }
        state.bot.loading = true;
        state.bot.error = null;
        try {
          const resp = await api.sendBotCommand(data);
          state.bot.response = resp;
          state.bot.loading = false;
          render.renderBotResponse(content, resp);
          render.renderToast(t('bot.sent'), 'success');
        } catch (err) {
          state.bot.loading = false;
          state.bot.error = err.message;
          render.renderToast(t('bot.sendFailed') + ': ' + err.message, 'error');
          // 失败后回到表单
          showBotCommandForm();
        }
      },
      onCancel: () => showBotCommandForm(),
    });
  });
}

// showBotResponse 显示最近一次命令响应。
export function showBotResponse() {
  const content = botContent();
  if (!content) return;
  if (!state.bot.response) {
    showBotCommandForm();
    return;
  }
  render.renderBotResponse(content, state.bot.response);
}

// showBotPlatforms 显示平台列表。
export async function showBotPlatforms() {
  const content = botContent();
  if (!content) return;
  render.renderLoading(content);
  const platforms = await loadBotPlatforms();
  render.renderBotPlatformsTable(content, platforms);
}

// refreshBotSubTab 按当前子 tab 刷新 ChatOps 页。
export function refreshBotSubTab() {
  const sub = state.botSubTab;
  if (sub === 'history') loadBotHistory();
  else if (sub === 'platforms') showBotPlatforms();
  else if (sub === 'response') showBotResponse();
  else showBotCommandForm();
}

// buildBotToolbar 构建 ChatOps 工具栏（子 tab + 刷新）。
export function buildBotToolbar() {
  const toolbar = $('bot-toolbar');
  if (!toolbar) return;
  toolbar.innerHTML = '';
  // 子 tab 切换组
  const subTabs = [
    { key: 'command', label: t('bot.tabCommand'), onActivate: () => showBotCommandForm() },
    { key: 'history', label: t('bot.tabHistory'), onActivate: () => loadBotHistory() },
    { key: 'platforms', label: t('bot.tabPlatforms'), onActivate: () => showBotPlatforms() },
  ];
  subTabs.forEach((st) => {
    toolbar.appendChild(
      render.el('button', {
        class: 'btn ' + (state.botSubTab === st.key ? 'btn-secondary' : 'btn-ghost'),
        onclick: () => { state.botSubTab = st.key; st.onActivate(); buildBotToolbar(); },
      }, render.el('span', { text: st.label }))
    );
  });
  // 刷新
  toolbar.appendChild(
    render.el('button', { class: 'btn btn-ghost', title: t('common.refresh'), onclick: () => refreshBotSubTab() },
      iconEl('refresh', 14), render.el('span', { text: t('common.refresh') })
    )
  );
}