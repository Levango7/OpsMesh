// i18n.js — OpsMesh Phase 1 前端国际化（中英文）。
//
// 设计要点：
//   - 支持 zh / en 两种语言，默认 zh（从 localStorage("opsmesh_lang") 读取）；
//   - t(key, fallback?) 查询翻译，缺失时回退到 fallback 或 key 本身；
//   - 翻译键命名：domain.field 或 domain.enum.value；
//   - setLang(lang) 切换语言并持久化；getLang() 读取当前语言。

const ZH = {
  // 应用通用
  'app.title': 'OpsMesh 网段运维中枢',
  'app.badge': 'OpsMesh',
  'app.enterprise': '进入企业版前端 →',
  'app.footer': 'OpsMesh 网段运维中枢 · 控制面运行中 ●',
  'app.capsHint': 'OpsMesh 网段运维中枢提供 6 大运维能力',
  'app.docsDesc': '· 项目简介、快速开始、API、架构与 FAQ 均可通过企业版前端的文档入口获取。',
  'app.caps': '监控告警、作业编排、K8s 管理、CMDB、批量运维等能力均已在企业版前端（Vue 3）提供。',
  'app.activeAlertsTitle': '活跃告警（M7）',

  // tab 导航
  'tab.tickets': '工单管理',
  'tab.dashboard': '监控仪表盘',
  'tab.slo': 'SLO 管理',
  'tab.enterprise': '企业版',

  // 通用
  'common.loading': '加载中…',
  'common.empty': '暂无数据',
  'common.error': '加载失败',
  'common.retry': '重试',
  'common.refresh': '刷新',
  'common.back': '返回',
  'common.create': '创建',
  'common.edit': '编辑',
  'common.delete': '删除',
  'common.close': '关闭',
  'common.save': '保存',
  'common.cancel': '取消',
  'common.search': '搜索',
  'common.confirm': '确认',
  'common.id': 'ID',
  'common.title': '标题',
  'common.status': '状态',
  'common.priority': '优先级',
  'common.category': '分类',
  'common.assignee': '指派人',
  'common.creator': '创建人',
  'common.createdAt': '创建时间',
  'common.updatedAt': '更新时间',
  'common.actions': '操作',
  'common.name': '名称',
  'common.description': '描述',
  'common.service': '服务',
  'common.target': '目标',
  'common.window': '窗口',
  'common.yes': '是',
  'common.no': '否',
  'common.all': '全部',

  // 工单管理
  'tickets.title': '工单管理',
  'tickets.subtitle': '服务台工单全生命周期管理',
  'tickets.list': '工单列表',
  'tickets.create': '新建工单',
  'tickets.detail': '工单详情',
  'tickets.edit': '编辑工单',
  'tickets.relatedDevice': '关联设备',
  'tickets.relatedTask': '关联任务',
  'tickets.tags': '标签',
  'tickets.resolvedAt': '解决时间',
  'tickets.filter.status': '状态过滤',
  'tickets.filter.priority': '优先级过滤',
  'tickets.filter.category': '分类过滤',
  'tickets.confirmClose': '确认关闭此工单？',
  'tickets.closed': '工单已关闭',
  'tickets.created': '工单已创建',
  'tickets.updated': '工单已更新',
  'tickets.deleted': '工单已删除',
  'tickets.loadFailed': '工单加载失败',
  'tickets.createFailed': '工单创建失败',
  'tickets.updateFailed': '工单更新失败',
  'tickets.closeFailed': '工单关闭失败',
  'tickets.titleRequired': '请输入工单标题',

  // 工单枚举
  'ticket.status.open': '待处理',
  'ticket.status.in_progress': '处理中',
  'ticket.status.resolved': '已解决',
  'ticket.status.closed': '已关闭',
  'ticket.priority.low': '低',
  'ticket.priority.medium': '中',
  'ticket.priority.high': '高',
  'ticket.priority.urgent': '紧急',
  'ticket.category.incident': '故障',
  'ticket.category.change': '变更',
  'ticket.category.request': '请求',
  'ticket.category.problem': '问题',

  // 监控仪表盘
  'dashboard.title': '监控仪表盘',
  'dashboard.subtitle': '设备/任务/告警/工单概览与 Prometheus 指标',
  'dashboard.overview': '概览',
  'dashboard.devices': '设备总数',
  'dashboard.tasks': '任务总数',
  'dashboard.alerts': '活跃告警',
  'dashboard.openTickets': '待处理工单',
  'dashboard.metrics': 'Prometheus 指标',
  'dashboard.metricsHint': 'Prometheus text exposition format，由后端 /metrics 端点输出',
  'dashboard.refresh': '刷新指标',
  'dashboard.metricsLoadFailed': '指标加载失败',
  'dashboard.copyMetrics': '复制指标',
  'dashboard.metricsCopied': '已复制到剪贴板',

  // SLO 管理
  'slo.title': 'SLO 管理',
  'slo.subtitle': '服务级别目标与指标管理',
  'slo.list': 'SLO 列表',
  'slo.create': '新建 SLO',
  'slo.detail': 'SLO 详情',
  'slo.edit': '编辑 SLO',
  'slo.slis': 'SLI 指标',
  'slo.sliName': '指标名',
  'slo.sliMetric': 'Prometheus 表达式',
  'slo.sliTarget': '目标值',
  'slo.sliOperator': '比较运算',
  'slo.sliStatus': 'SLI 状态',
  'slo.sliCurrent': '当前值',
  'slo.sliLastEvaluated': '最近评估',
  'slo.addSli': '添加 SLI',
  'slo.removeSli': '移除',
  'slo.confirmDelete': '确认删除此 SLO？',
  'slo.deleted': 'SLO 已删除',
  'slo.created': 'SLO 已创建',
  'slo.updated': 'SLO 已更新',
  'slo.loadFailed': 'SLO 加载失败',
  'slo.createFailed': 'SLO 创建失败',
  'slo.updateFailed': 'SLO 更新失败',
  'slo.deleteFailed': 'SLO 删除失败',
  'slo.nameRequired': '请输入 SLO 名称',
  'slo.statusTitle': 'SLI 实时状态',
  'slo.noStatus': '暂无 SLI 状态数据',

  // SLO 枚举
  'slo.status.met': '达标',
  'slo.status.breached': '违约',
  'slo.status.nodata': '无数据',
};

const EN = {
  // app common
  'app.title': 'OpsMesh Network Ops Hub',
  'app.badge': 'OpsMesh',
  'app.enterprise': 'Open Enterprise Frontend →',
  'app.footer': 'OpsMesh Control Plane Running ●',
  'app.capsHint': 'OpsMesh provides 6 major ops capabilities',
  'app.docsDesc': '· Project intro, quick start, API, architecture and FAQ are available via the enterprise frontend docs entry.',
  'app.caps': 'Alerts, workflows, K8s, CMDB, batch ops are all available in the enterprise frontend (Vue 3).',
  'app.activeAlertsTitle': 'Active Alerts (M7)',

  // tab nav
  'tab.tickets': 'Tickets',
  'tab.dashboard': 'Dashboard',
  'tab.slo': 'SLO',
  'tab.enterprise': 'Enterprise',

  // common
  'common.loading': 'Loading...',
  'common.empty': 'No data',
  'common.error': 'Load failed',
  'common.retry': 'Retry',
  'common.refresh': 'Refresh',
  'common.back': 'Back',
  'common.create': 'Create',
  'common.edit': 'Edit',
  'common.delete': 'Delete',
  'common.close': 'Close',
  'common.save': 'Save',
  'common.cancel': 'Cancel',
  'common.search': 'Search',
  'common.confirm': 'Confirm',
  'common.id': 'ID',
  'common.title': 'Title',
  'common.status': 'Status',
  'common.priority': 'Priority',
  'common.category': 'Category',
  'common.assignee': 'Assignee',
  'common.creator': 'Creator',
  'common.createdAt': 'Created At',
  'common.updatedAt': 'Updated At',
  'common.actions': 'Actions',
  'common.name': 'Name',
  'common.description': 'Description',
  'common.service': 'Service',
  'common.target': 'Target',
  'common.window': 'Window',
  'common.yes': 'Yes',
  'common.no': 'No',
  'common.all': 'All',

  // tickets
  'tickets.title': 'Ticket Management',
  'tickets.subtitle': 'Service desk ticket lifecycle management',
  'tickets.list': 'Ticket List',
  'tickets.create': 'New Ticket',
  'tickets.detail': 'Ticket Detail',
  'tickets.edit': 'Edit Ticket',
  'tickets.relatedDevice': 'Related Device',
  'tickets.relatedTask': 'Related Task',
  'tickets.tags': 'Tags',
  'tickets.resolvedAt': 'Resolved At',
  'tickets.filter.status': 'Status Filter',
  'tickets.filter.priority': 'Priority Filter',
  'tickets.filter.category': 'Category Filter',
  'tickets.confirmClose': 'Close this ticket?',
  'tickets.closed': 'Ticket closed',
  'tickets.created': 'Ticket created',
  'tickets.updated': 'Ticket updated',
  'tickets.deleted': 'Ticket deleted',
  'tickets.loadFailed': 'Tickets load failed',
  'tickets.createFailed': 'Ticket create failed',
  'tickets.updateFailed': 'Ticket update failed',
  'tickets.closeFailed': 'Ticket close failed',
  'tickets.titleRequired': 'Ticket title is required',

  // ticket enums
  'ticket.status.open': 'Open',
  'ticket.status.in_progress': 'In Progress',
  'ticket.status.resolved': 'Resolved',
  'ticket.status.closed': 'Closed',
  'ticket.priority.low': 'Low',
  'ticket.priority.medium': 'Medium',
  'ticket.priority.high': 'High',
  'ticket.priority.urgent': 'Urgent',
  'ticket.category.incident': 'Incident',
  'ticket.category.change': 'Change',
  'ticket.category.request': 'Request',
  'ticket.category.problem': 'Problem',

  // dashboard
  'dashboard.title': 'Monitoring Dashboard',
  'dashboard.subtitle': 'Overview of devices/tasks/alerts/tickets & Prometheus metrics',
  'dashboard.overview': 'Overview',
  'dashboard.devices': 'Total Devices',
  'dashboard.tasks': 'Total Tasks',
  'dashboard.alerts': 'Active Alerts',
  'dashboard.openTickets': 'Open Tickets',
  'dashboard.metrics': 'Prometheus Metrics',
  'dashboard.metricsHint': 'Prometheus text exposition format from /metrics endpoint',
  'dashboard.refresh': 'Refresh Metrics',
  'dashboard.metricsLoadFailed': 'Metrics load failed',
  'dashboard.copyMetrics': 'Copy Metrics',
  'dashboard.metricsCopied': 'Copied to clipboard',

  // slo
  'slo.title': 'SLO Management',
  'slo.subtitle': 'Service Level Objectives & Indicators',
  'slo.list': 'SLO List',
  'slo.create': 'New SLO',
  'slo.detail': 'SLO Detail',
  'slo.edit': 'Edit SLO',
  'slo.slis': 'SLI Indicators',
  'slo.sliName': 'Indicator Name',
  'slo.sliMetric': 'Prometheus Expression',
  'slo.sliTarget': 'Target Value',
  'slo.sliOperator': 'Operator',
  'slo.sliStatus': 'SLI Status',
  'slo.sliCurrent': 'Current Value',
  'slo.sliLastEvaluated': 'Last Evaluated',
  'slo.addSli': 'Add SLI',
  'slo.removeSli': 'Remove',
  'slo.confirmDelete': 'Delete this SLO?',
  'slo.deleted': 'SLO deleted',
  'slo.created': 'SLO created',
  'slo.updated': 'SLO updated',
  'slo.loadFailed': 'SLO load failed',
  'slo.createFailed': 'SLO create failed',
  'slo.updateFailed': 'SLO update failed',
  'slo.deleteFailed': 'SLO delete failed',
  'slo.nameRequired': 'SLO name is required',
  'slo.statusTitle': 'SLI Live Status',
  'slo.noStatus': 'No SLI status data',

  // slo enums
  'slo.status.met': 'Met',
  'slo.status.breached': 'Breached',
  'slo.status.nodata': 'No Data',
};

const DICTS = { zh: ZH, en: EN };

// getLang 读取当前语言（localStorage("opsmesh_lang")，默认 "zh"）。
export function getLang() {
  try {
    const l = localStorage.getItem('opsmesh_lang');
    if (l === 'zh' || l === 'en') return l;
  } catch (_) { /* 静默 */ }
  return 'zh';
}

// setLang 切换语言并持久化。
export function setLang(lang) {
  if (lang !== 'zh' && lang !== 'en') lang = 'zh';
  try { localStorage.setItem('opsmesh_lang', lang); } catch (_) { /* 静默 */ }
}

// t(key, fallback?) 查询翻译。缺失时回退到 fallback 或 key。
export function t(key, fallback) {
  const dict = DICTS[getLang()] || ZH;
  if (Object.prototype.hasOwnProperty.call(dict, key)) return dict[key];
  if (fallback !== undefined) return fallback;
  return key;
}

// 导出字典便于调试/批量渲染。
export const I18N_DICTS = DICTS;