// theme.js — light/dark 主题切换
// 职责：读取/切换/持久化主题，通过 [data-theme] 属性切换 CSS 变量集。
// localStorage 键：opsmesh-theme（值为 'light' 或 'dark'）
// 默认主题：light（与现有 :root 变量一致）。

import { icon } from './icons.js';

const STORAGE_KEY = 'opsmesh-theme';
const THEMES = ['light', 'dark'];

// ---------- 获取当前主题 ----------
export function getTheme() {
  let t = localStorage.getItem(STORAGE_KEY);
  if (THEMES.indexOf(t) < 0) {
    // 首次访问：跟随系统偏好
    if (window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches) {
      t = 'dark';
    } else {
      t = 'light';
    }
  }
  return t;
}

// ---------- 设置主题 ----------
export function setTheme(theme) {
  if (THEMES.indexOf(theme) < 0) theme = 'light';
  document.documentElement.setAttribute('data-theme', theme);
  localStorage.setItem(STORAGE_KEY, theme);
  updateToggleButton(theme);
}

// ---------- 切换主题 ----------
export function toggleTheme() {
  const next = getTheme() === 'light' ? 'dark' : 'light';
  setTheme(next);
}

// ---------- 更新切换按钮图标 ----------
function updateToggleButton(theme) {
  const btn = document.getElementById('themeToggleBtn');
  if (!btn) return;
  // light 时显示月亮（点击切到 dark），dark 时显示太阳（点击切到 light）
  const iconName = theme === 'light' ? 'theme-dark' : 'theme-light';
  btn.innerHTML = icon(iconName, 18);
  btn.setAttribute('title', theme === 'light' ? '切换到暗色主题' : '切换到亮色主题');
  btn.setAttribute('aria-label', theme === 'light' ? '切换到暗色主题' : '切换到亮色主题');
}

// ---------- 初始化主题 ----------
// 在 DOMContentLoaded 前尽早调用，避免闪烁（FOUC）。
export function initTheme() {
  setTheme(getTheme());
  // 监听系统主题变化（仅当用户未手动选择时跟随）
  if (window.matchMedia) {
    window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', function (e) {
      // 仅在用户未显式设置时跟随系统
      if (!localStorage.getItem(STORAGE_KEY)) {
        setTheme(e.matches ? 'dark' : 'light');
      }
    });
  }
  // 绑定切换按钮
  const btn = document.getElementById('themeToggleBtn');
  if (btn) {
    btn.addEventListener('click', toggleTheme);
  }
}