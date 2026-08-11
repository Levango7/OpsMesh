// flow.js — barrel re-export（P2-1 拆分后）
// 原 2714 行按业务域拆分为 flow_*.js 模块，本文件仅 re-export 保持向后兼容。
// main.js 的 import { ... } from './flow.js' 无需修改。
//
// 拆分清单：
//   flow_focus.js      — 跨模块联动（focus 状态）
//   flow_tab.js        — 标签切换 + 导航 + 抽屉
//   flow_devices.js    — 设备纳管 + 身份注入 + Agent 加载 + 任务下发
//   flow_cmdb.js       — CMDB 操作
//   flow_workflow.js   — 作业流 DAG 编辑器
//   flow_deploys.js    — 部署编排
//   flow_alerts.js     — 监控告警
//   flow_audits.js     — 审计日志
//   flow_os.js         — OS 基础优化 + 通用任务轮询 + 参数验证
//   flow_middleware.js — 中间件部署
//   flow_k8s.js        — K8s 集群与资源管理
//   flow_logs.js       — 日志检索
//   flow_users.js      — 改密对话框

export * from './flow_focus.js';
export * from './flow_tab.js';
export * from './flow_devices.js';
export * from './flow_cmdb.js';
export * from './flow_workflow.js';
export * from './flow_deploys.js';
export * from './flow_alerts.js';
export * from './flow_audits.js';
export * from './flow_os.js';
export * from './flow_middleware.js';
export * from './flow_k8s.js';
export * from './flow_logs.js';
export * from './flow_users.js';
export * from './flow_m2.js'; // task 241 M2 集成：告警规则引擎 + 静默 + 通知渠道 + 通知模板
export * from './flow_helm.js'; // task 242 M3 集成：Helm 应用商店
export * from './flow_dashboard.js'; // task 242 M3 集成：集群监控仪表盘
export * from './flow_batch.js'; // task 243 M5 集成：批量运维 + 灰度发布
export * from './flow_schedules.js'; // task 243 M5 集成：定时任务管理
export * from './flow_approval.js'; // task 243 M5 集成：审批管理
export * from './flow_network.js'; // task 244 M6 集成：网络拓扑 + 诊断 + 连通性检测
