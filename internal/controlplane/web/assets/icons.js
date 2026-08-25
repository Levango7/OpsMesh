// icons.js — OpsMesh Phase 1 前端图标集。
//
// 设计原则（与用户偏好对齐）：
//   - 统一化、唯一化、简约小众，拒绝花里胡哨；
//   - 全部 24×24 viewBox，stroke 线性风格，stroke-width=1.8；
//   - 使用 currentColor 继承父级 color，便于主题切换；
//   - 命名 iconXxx，导出 ICONS 字典 + iconHtml(name) 辅助函数。
//
// 仅覆盖 Phase 1 所需图标：工单 / 仪表盘 / SLO / 设备 / 任务 / 告警 /
// 创建 / 编辑 / 删除 / 关闭 / 刷新 / 搜索 / 返回 / 完成 / 时钟 / 用户。

// raw 返回 SVG 内部 path/group 标签（不含 <svg> 外壳），由 iconHtml 包裹。
const RAW = {
  // 顶部 tab 图标
  ticket:
    '<path d="M6 3h12a1 1 0 0 1 1 1v16a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1z"/>' +
    '<path d="M8 7h8M8 11h8M8 15h5"/>',
  dashboard:
    '<rect x="3" y="3" width="7" height="7" rx="1"/>' +
    '<rect x="14" y="3" width="7" height="7" rx="1"/>' +
    '<rect x="3" y="14" width="7" height="7" rx="1"/>' +
    '<rect x="14" y="14" width="7" height="7" rx="1"/>',
  slo:
    '<path d="M12 2l8 4v6c0 5-3.5 8.5-8 10-4.5-1.5-8-5-8-10V6l8-4z"/>' +
    '<path d="M8.5 12l2.5 2.5 4.5-5"/>',
  // 概览卡片图标
  device:
    '<rect x="3" y="4" width="18" height="12" rx="1.5"/>' +
    '<path d="M8 20h8M12 16v4"/>',
  task:
    '<rect x="4" y="4" width="16" height="16" rx="2"/>' +
    '<path d="M8 12l3 3 5-6"/>',
  alert:
    '<path d="M12 3l9 16H3l9-16z"/>' +
    '<path d="M12 9v5M12 17h.01"/>',
  // 操作图标
  plus: '<path d="M12 5v14M5 12h14"/>',
  edit:
    '<path d="M4 20h4l11-11-4-4L4 16v4z"/>' +
    '<path d="M14 6l4 4"/>',
  trash:
    '<path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13"/>' +
    '<path d="M10 11v6M14 11v6"/>',
  close: '<path d="M6 6l12 12M18 6L6 18"/>',
  refresh:
    '<path d="M4 12a8 8 0 0 1 14-5.3L20 8"/>' +
    '<path d="M20 4v4h-4"/>' +
    '<path d="M20 12a8 8 0 0 1-14 5.3L4 16"/>' +
    '<path d="M4 20v-4h4"/>',
  search:
    '<circle cx="11" cy="11" r="7"/>' +
    '<path d="M21 21l-4.3-4.3"/>',
  back: '<path d="M19 12H5M11 6l-6 6 6 6"/>',
  check: '<path d="M5 12l5 5 9-11"/>',
  clock:
    '<circle cx="12" cy="12" r="9"/>' +
    '<path d="M12 7v5l3 2"/>',
  user:
    '<circle cx="12" cy="8" r="4"/>' +
    '<path d="M4 21c0-4 4-7 8-7s8 3 8 7"/>',
  tag:
    '<path d="M3 12l9-9 9 9-9 9-9-9z"/>' +
    '<circle cx="9" cy="9" r="1.2"/>',
  // Phase 2 tab 图标
  traffic:
    '<path d="M3 6h18M3 12h12M3 18h18"/>' +
    '<circle cx="17" cy="12" r="2"/>',
  pipeline:
    '<circle cx="6" cy="6" r="2.5"/>' +
    '<circle cx="18" cy="6" r="2.5"/>' +
    '<circle cx="6" cy="18" r="2.5"/>' +
    '<circle cx="18" cy="18" r="2.5"/>' +
    '<path d="M8.5 6h7M6 8.5v7M18 8.5v7M8 8l8 8"/>',
  canary:
    '<path d="M4 19V5M4 19h16"/>' +
    '<path d="M7 16l3-5 3 3 4-7"/>' +
    '<circle cx="7" cy="16" r="1.2"/>' +
    '<circle cx="10" cy="11" r="1.2"/>' +
    '<circle cx="13" cy="14" r="1.2"/>' +
    '<circle cx="17" cy="7" r="1.2"/>',
  'config-push':
    '<rect x="3" y="4" width="18" height="6" rx="1"/>' +
    '<rect x="3" y="14" width="18" height="6" rx="1"/>' +
    '<path d="M7 7h.01M7 17h.01"/>' +
    '<path d="M17 5l3 3-3 3M14 8h6"/>' +
    '<path d="M17 15l3 3-3 3M14 18h6"/>',
  // Phase 2 操作图标
  play:
    '<path d="M6 4l14 8-14 8V4z"/>',
  pause:
    '<path d="M8 4v16M16 4v16"/>',
  sync:
    '<path d="M4 12a8 8 0 0 1 13-5.3L20 8M20 4v4h-4"/>' +
    '<path d="M20 12a8 8 0 0 1-13 5.3L4 16M4 20v-4h4"/>',
  toggle_on:
    '<rect x="2" y="6" width="20" height="12" rx="6"/>' +
    '<circle cx="16" cy="12" r="3" fill="currentColor" stroke="none"/>',
  toggle_off:
    '<rect x="2" y="6" width="20" height="12" rx="6"/>' +
    '<circle cx="8" cy="12" r="3" fill="currentColor" stroke="none"/>',
  history:
    '<path d="M3 12a9 9 0 1 0 3-6.7L3 8"/>' +
    '<path d="M3 4v4h4"/>' +
    '<path d="M12 8v4l3 2"/>',
  rocket:
    '<path d="M5 15c-1 1-1 4-1 4s3 0 4-1M9 11l4 4M14 6l4 4-7 7-4-1-1-4 8-6z"/>' +
    '<circle cx="15" cy="9" r="1"/>',
  sliders:
    '<path d="M4 8h16M4 16h16"/>' +
    '<circle cx="9" cy="8" r="2"/>' +
    '<circle cx="15" cy="16" r="2"/>',
  git:
    '<circle cx="6" cy="6" r="2.5"/>' +
    '<circle cx="6" cy="18" r="2.5"/>' +
    '<circle cx="18" cy="12" r="2.5"/>' +
    '<path d="M6 8.5v7M8 6h6a4 4 0 0 1 4 4"/>',
  // Phase 3 tab 图标
  compliance:
    '<path d="M12 2l8 4v6c0 5-3.5 8.5-8 10-4.5-1.5-8-5-8-10V6l8-4z"/>' +
    '<path d="M9 12l2 2 4-4"/>',
  ha:
    '<path d="M3 12h4l2-5 4 10 2-5h6"/>' +
    '<circle cx="3" cy="12" r="1.2" fill="currentColor" stroke="none"/>' +
    '<circle cx="21" cy="12" r="1.2" fill="currentColor" stroke="none"/>',
  // Phase 3 操作图标
  shield:
    '<path d="M12 2l8 4v6c0 5-3.5 8.5-8 10-4.5-1.5-8-5-8-10V6l8-4z"/>',
  scan:
    '<path d="M4 4v4M4 4h4M20 4v4M20 4h-4M4 20v-4M4 20h4M20 20v-4M20 20h-4"/>' +
    '<path d="M4 12h16"/>',
  audit:
    '<rect x="4" y="3" width="16" height="18" rx="1.5"/>' +
    '<path d="M8 7h8M8 11h8M8 15h5"/>' +
    '<circle cx="16" cy="16" r="2"/>' +
    '<path d="M16 18v2"/>',
  failover:
    '<path d="M4 12a8 8 0 0 1 8-8"/>' +
    '<path d="M12 4l2 2-2 2"/>' +
    '<path d="M20 12a8 8 0 0 1-8 8"/>' +
    '<path d="M12 20l-2-2 2-2"/>',
  backup:
    '<rect x="4" y="3" width="16" height="4" rx="1"/>' +
    '<rect x="4" y="9" width="16" height="11" rx="1"/>' +
    '<path d="M12 13v4M10 15h4"/>',
  restore:
    '<rect x="4" y="3" width="16" height="4" rx="1"/>' +
    '<rect x="4" y="9" width="16" height="11" rx="1"/>' +
    '<path d="M12 17v-4M9 14h6"/>',
  heartbeat:
    '<path d="M2 12h4l2-5 4 10 2-5h8"/>' +
    '<circle cx="2" cy="12" r="1.2" fill="currentColor" stroke="none"/>' +
    '<circle cx="22" cy="12" r="1.2" fill="currentColor" stroke="none"/>',
  leader:
    '<circle cx="12" cy="8" r="3"/>' +
    '<path d="M12 11v7M9 18h6"/>',
  download:
    '<path d="M12 3v12"/>' +
    '<path d="M7 10l5 5 5-5"/>' +
    '<path d="M4 19h16"/>',
  // Phase 4 tab 图标
  'network-mgmt':
    '<circle cx="12" cy="12" r="3"/>' +
    '<path d="M12 9V5M12 19v-4M9 12H5M19 12h-4"/>' +
    '<circle cx="5" cy="5" r="1.5"/>' +
    '<circle cx="19" cy="5" r="1.5"/>' +
    '<circle cx="5" cy="19" r="1.5"/>' +
    '<circle cx="19" cy="19" r="1.5"/>' +
    '<path d="M6.5 6.5l2.5 2.5M17.5 6.5l-2.5 2.5M6.5 17.5l2.5-2.5M17.5 17.5l-2.5-2.5"/>',
  automation:
    '<circle cx="12" cy="12" r="3"/>' +
    '<path d="M12 2v3M12 19v3M2 12h3M19 12h3M4.9 4.9l2.1 2.1M17 17l2.1 2.1M4.9 19.1l2.1-2.1M17 7l2.1-2.1"/>',
  // Phase 4 操作图标
  network:
    '<circle cx="12" cy="12" r="3"/>' +
    '<path d="M12 9V5M12 19v-4M9 12H5M19 12h-4"/>',
  discover:
    '<circle cx="11" cy="11" r="7"/>' +
    '<path d="M21 21l-4.3-4.3"/>' +
    '<path d="M11 8v6M8 11h6"/>',
  config_deploy:
    '<rect x="3" y="4" width="18" height="6" rx="1"/>' +
    '<rect x="3" y="14" width="18" height="6" rx="1"/>' +
    '<path d="M17 7l3 0M17 17l3 0"/>',
  rule:
    '<rect x="4" y="3" width="16" height="18" rx="1.5"/>' +
    '<path d="M8 7h8M8 11h8M8 15h5"/>',
  trigger:
    '<path d="M6 4l14 8-14 8V4z"/>' +
    '<circle cx="6" cy="12" r="1.5" fill="currentColor" stroke="none"/>',
  action:
    '<path d="M13 2l-2 2v4l2 2h4l2-2V4l-2-2h-4z"/>' +
    '<path d="M15 10v8M11 18h8"/>' +
    '<circle cx="15" cy="18" r="2"/>',
  test:
    '<path d="M4 4v4M4 4h4M20 4v4M20 4h-4M4 20v-4M4 20h4M20 20v-4M20 20h-4"/>' +
    '<path d="M4 12h16"/>' +
    '<circle cx="12" cy="12" r="1.5" fill="currentColor" stroke="none"/>',
  enable:
    '<rect x="2" y="6" width="20" height="12" rx="6"/>' +
    '<circle cx="16" cy="12" r="3" fill="currentColor" stroke="none"/>',
  disable:
    '<rect x="2" y="6" width="20" height="12" rx="6"/>' +
    '<circle cx="8" cy="12" r="3" fill="currentColor" stroke="none"/>',
  // Phase 5 tab 图标
  gateway:
    '<path d="M3 6h18M3 18h18"/>' +
    '<circle cx="7" cy="6" r="1.6" fill="currentColor" stroke="none"/>' +
    '<circle cx="17" cy="18" r="1.6" fill="currentColor" stroke="none"/>' +
    '<path d="M7 8v6a2 2 0 0 0 2 2h6a2 2 0 0 1 2 2"/>',
  webhook:
    '<circle cx="6" cy="18" r="2.5"/>' +
    '<circle cx="18" cy="6" r="2.5"/>' +
    '<path d="M8 18h6a4 4 0 0 0 0-8H10a4 4 0 0 1 0-8h6"/>',
  script:
    '<rect x="4" y="3" width="16" height="18" rx="1.5"/>' +
    '<path d="M8 7l-2 2 2 2M16 7l2 2-2 2M8 15h8"/>',
  // Phase 5 操作图标
  route:
    '<path d="M5 19V5M5 19h14"/>' +
    '<path d="M5 12h6l3-4 3 4h2"/>' +
    '<circle cx="11" cy="12" r="1.2" fill="currentColor" stroke="none"/>' +
    '<circle cx="14" cy="8" r="1.2" fill="currentColor" stroke="none"/>' +
    '<circle cx="17" cy="12" r="1.2" fill="currentColor" stroke="none"/>',
  send:
    '<path d="M4 12l16-8-6 16-3-7-7-1z"/>' +
    '<path d="M11 13l5-5"/>',
  deliver:
    '<rect x="3" y="6" width="13" height="12" rx="1"/>' +
    '<path d="M16 9h4l2 3v3h-6"/>' +
    '<circle cx="7" cy="18" r="1.8"/>' +
    '<circle cx="18" cy="18" r="1.8"/>' +
    '<path d="M7 9h6M7 12h4"/>',
  code:
    '<path d="M9 8l-4 4 4 4M15 8l4 4-4 4"/>' +
    '<path d="M13 6l-2 12"/>',
  execute:
    '<rect x="3" y="4" width="18" height="16" rx="1.5"/>' +
    '<path d="M7 9l3 3-3 3M13 9l3 3-3 3"/>',
  stats:
    '<path d="M4 20V4M4 20h16"/>' +
    '<rect x="7" y="12" width="3" height="6"/>' +
    '<rect x="12" y="8" width="3" height="10"/>' +
    '<rect x="17" y="14" width="3" height="4"/>',
  // Phase 6 tab 图标（平台化管理）
  tenant:
    '<rect x="3" y="8" width="18" height="13" rx="1"/>' +
    '<path d="M3 8l9-5 9 5"/>' +
    '<rect x="9" y="13" width="6" height="8"/>' +
    '<path d="M7 21v-3M17 21v-3"/>',
  apikey:
    '<circle cx="8" cy="8" r="4"/>' +
    '<path d="M11 11l9 9"/>' +
    '<path d="M16 16l2-2M18 18l2-2"/>',
  plugin:
    '<path d="M9 3v4M15 3v4M9 17v4M15 17v4"/>' +
    '<rect x="6" y="7" width="12" height="10" rx="1.5"/>' +
    '<path d="M10 11h4"/>',
  billing:
    '<rect x="2" y="5" width="20" height="14" rx="2"/>' +
    '<path d="M2 10h20"/>' +
    '<path d="M6 15h4"/>',
  platform:
    '<rect x="3" y="4" width="18" height="16" rx="2"/>' +
    '<circle cx="8" cy="10" r="1.5"/>' +
    '<circle cx="16" cy="10" r="1.5"/>' +
    '<path d="M8 14v2M16 14v2M12 12v4"/>',
};

// ICONS 对外暴露的图标字典（name -> raw inner SVG）。
export const ICONS = RAW;

// iconHtml(name, size=18) 返回完整 <svg> 字符串，便于 innerHTML 拼接。
// 统一 stroke 风格：fill=none stroke=currentColor stroke-width=1.8 stroke-linecap=round。
export function iconHtml(name, size = 18) {
  const inner = RAW[name];
  if (!inner) return '';
  return (
    '<svg xmlns="http://www.w3.org/2000/svg" width="' + size + '" height="' + size + '" ' +
    'viewBox="0 0 24 24" fill="none" stroke="currentColor" ' +
    'stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" ' +
    'aria-hidden="true" focusable="false">' + inner + '</svg>'
  );
}

// iconEl(name, size=18) 返回 HTMLElement（span.icon > svg），便于 DOM API 插入。
export function iconEl(name, size = 18) {
  const span = document.createElement('span');
  span.className = 'icon';
  span.setAttribute('aria-hidden', 'true');
  span.innerHTML = iconHtml(name, size);
  return span;
}