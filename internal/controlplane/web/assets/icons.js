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