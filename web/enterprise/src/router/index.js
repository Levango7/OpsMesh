// 路由表 — 概览 + 运维 + 资产 + 交付 + 观测 + 系统管理 + 登录/注册
import { createRouter, createWebHistory } from 'vue-router'
import { watch, defineComponent, h } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { t, currentLang, loadRouteDomain } from '@/i18n'
import { toast } from '@/utils/toast'

const RouteSkeleton = defineComponent({
  name: 'RouteSkeleton',
  setup() {
    return () =>
      h('div', { class: 'route-skeleton', style: { padding: '22px' } }, [
        h('div', { class: 'skeleton-line', style: { width: '30%', height: '28px', marginBottom: '18px' } }),
        h('div', { class: 'skeleton-line', style: { width: '60%', height: '16px', marginBottom: '12px' } }),
        h('div', { class: 'skeleton-line', style: { width: '45%', height: '16px', marginBottom: '12px' } }),
        h('div', { class: 'skeleton-line', style: { width: '80%', height: '16px', marginBottom: '24px' } }),
        h('div', { class: 'skeleton-card', style: { height: '120px', marginBottom: '16px' } }),
        h('div', { class: 'skeleton-card', style: { height: '120px' } })
      ])
  }
})

const routes = [
  // 公共路由：登录 / 注册 / 修改密码
  { path: '/login', name: 'login', component: () => import('@/views/LoginView.vue'), meta: { public: true, title: 'login.title' } },
  { path: '/register', name: 'register', component: () => import('@/views/RegisterView.vue'), meta: { public: true, title: 'register.title' } },
  { path: '/change-password', name: 'change-password', component: () => import('@/views/ChangePasswordView.vue'), meta: { public: true, title: 'change_password.title' } },

  // 概览
  { path: '/', redirect: '/overview' },
  { path: '/overview', name: 'overview', component: () => import('@/views/OverviewView.vue'), meta: { title: 'nav.home', group: '概览', icon: 'home', requirePerm: '', prefetch: true } },

  // 运维管理
  { path: '/devices', name: 'devices', component: () => import('@/views/DevicesView.vue'), meta: { title: 'nav.devices', group: '运维管理', icon: 'device', requirePerm: 'device:read', prefetch: true } },
  { path: '/devices/:id', name: 'device-detail', component: () => import('@/views/DeviceDetailView.vue'), meta: { title: 'device_detail.title', group: '运维管理', icon: 'device', requirePerm: 'device:read' } },
  { path: '/tasks', name: 'tasks', component: () => import('@/views/TasksView.vue'), meta: { title: 'nav.tasks', group: '运维管理', icon: 'task', requirePerm: 'task:read' } },
  { path: '/alerts', name: 'alerts', component: () => import('@/views/AlertsView.vue'), meta: { title: 'nav.alerts', group: '运维管理', icon: 'alerts', requirePerm: 'alert:read' } },
  { path: '/os-optimize', name: 'os-optimize', component: () => import('@/views/OSOptimizeView.vue'), meta: { title: 'nav.osopt', group: '运维管理', icon: 'task', requirePerm: 'task:read' } },
  { path: '/middleware', name: 'middleware', component: () => import('@/views/MiddlewareDeployView.vue'), meta: { title: 'nav.mwdep', group: '运维管理', icon: 'deploy', requirePerm: 'deploy:read' } },
  { path: '/k8s', name: 'k8s', component: () => import('@/views/K8sManageView.vue'), meta: { title: 'nav.k8s', group: '运维管理', icon: 'device', requirePerm: 'device:read' } },
  // 批量运维（运维管理）：多设备批量下发 + 状态查看
  { path: '/batch', name: 'batch', component: () => import('@/views/BatchView.vue'), meta: { title: 'nav.batch', group: '运维管理', icon: 'task', requirePerm: 'task:read' } },

  // 资产配置
  { path: '/cmdb', name: 'cmdb', component: () => import('@/views/CMDBView.vue'), meta: { title: 'nav.cmdb', group: '资产配置', icon: 'cmdb', requirePerm: 'cmdb:read' } },

  // 交付中心
  { path: '/workflows', name: 'workflows', component: () => import('@/views/WorkflowsView.vue'), meta: { title: 'nav.workflows', group: '交付中心', icon: 'flow', requirePerm: 'workflow:read' } },
  { path: '/deploys', name: 'deploys', component: () => import('@/views/DeploysView.vue'), meta: { title: 'nav.deploys', group: '交付中心', icon: 'deploy', requirePerm: 'deploy:read' } },
  // 灰度发布（交付中心）：创建灰度 + 状态 + 推进 + 流量分割 + 指标
  { path: '/canary', name: 'canary', component: () => import('@/views/CanaryView.vue'), meta: { title: 'nav.canary', group: '交付中心', icon: 'flow', requirePerm: 'traffic:read' } },

  // 可观测性
  { path: '/logs', name: 'logs', component: () => import('@/views/LogsView.vue'), meta: { title: 'nav.logs', group: '可观测性', icon: 'logs', requirePerm: 'log:read' } },
  // 告警规则管理（可观测性）：规则 / 引擎 / 静默 三 Tab
  { path: '/alert-rules', name: 'alert-rules', component: () => import('@/views/AlertRulesView.vue'), meta: { title: 'nav.alertRules', group: '可观测性', icon: 'alerts', requirePerm: 'alert:read' } },

  // 系统管理
  { path: '/users', name: 'users', component: () => import('@/views/UsersView.vue'), meta: { title: 'nav.users', group: '系统管理', icon: 'users', requirePerm: 'user:read' } },
  { path: '/roles', name: 'roles', component: () => import('@/views/RolesView.vue'), meta: { title: 'nav.roles', group: '系统管理', icon: 'roles', requirePerm: 'role:read' } },
  { path: '/permissions', name: 'permissions', component: () => import('@/views/PermissionsView.vue'), meta: { title: 'nav.permissions', group: '系统管理', icon: 'permissions', requirePerm: 'role:read' } },
  { path: '/secrets', name: 'secrets', component: () => import('@/views/secrets/SecretsView.vue'), meta: { title: 'nav.secrets', group: '系统管理', icon: 'key', requirePerm: 'role:read' } },

  // ================= 插件市场 =================
  // 保留：后端有真实端点 /api/v1/marketplace/plugins（见 src/api/plugin.js），修复端点后可用。
  { path: '/plugins', name: 'plugins', component: () => import('@/views/PluginView.vue'), meta: { title: 'nav.plugins', group: '平台', icon: 'cmdb', requirePerm: 'plugin:read' } },

  // ======================================================================
  // 七域管理页（schedules/automation/webhooks/scripts/tickets/slos/traffic，
  // 后端 handler 在 internal/controlplane/{server_schedules,automation,webhook,
  // script,ticket,slo,traffic}.go 注册，API 契约见 src/api/*.js 注释头）。
  // ======================================================================
  // 定时任务（自动化）
  { path: '/schedules', name: 'schedules', component: () => import('@/views/SchedulesView.vue'), meta: { title: 'nav.schedules', group: '自动化', icon: 'task', requirePerm: 'automation:read' } },

  // 自动化规则（自动化）
  { path: '/automation', name: 'automation', component: () => import('@/views/AutomationView.vue'), meta: { title: 'nav.automation', group: '自动化', icon: 'flow', requirePerm: 'automation:read' } },

  // Webhook 管理（自动化）
  { path: '/webhooks', name: 'webhooks', component: () => import('@/views/WebhooksView.vue'), meta: { title: 'nav.webhooks', group: '自动化', icon: 'alerts', requirePerm: 'webhook:read' } },

  // 自定义脚本（自动化）
  { path: '/scripts', name: 'scripts', component: () => import('@/views/ScriptsView.vue'), meta: { title: 'nav.scripts', group: '自动化', icon: 'settings', requirePerm: 'script:read' } },

  // 工单（系统管理）
  { path: '/tickets', name: 'tickets', component: () => import('@/views/TicketsView.vue'), meta: { title: 'nav.tickets', group: '系统管理', icon: 'users', requirePerm: 'ticket:read' } },

  // SLO 目标（可观测性）
  { path: '/slos', name: 'slos', component: () => import('@/views/SLOsView.vue'), meta: { title: 'nav.slos', group: '可观测性', icon: 'alerts', requirePerm: 'slo:read' } },

  // 流量策略（交付中心）
  { path: '/traffic', name: 'traffic', component: () => import('@/views/TrafficPoliciesView.vue'), meta: { title: 'nav.traffic', group: '交付中心', icon: 'flow', requirePerm: 'traffic:read' } },

  // ======================================================================
  // 六域已接线（M13 聚合层补齐，2026-09）：
  //   gpu/runbooks/incidents/autoscaler/portal → controlplane service_proxy.go
  //     转发到 services/* 独立进程（后端地址 env 覆盖：*_SVC_URL）；
  //   bot → controlplane bot_bridge.go（Web 命令台，命令语法与 bot-svc IM
  //     webhook 一致：/opsmesh status|devices|alerts|ack|metrics|help）。
  // 后端服务未启动时代理返回 503 service unreachable（页面报错但不 404）。
  // ======================================================================
  // GPU 资源管理
  { path: '/gpu', name: 'gpu', component: () => import('@/views/GPUView.vue'), meta: { title: 'nav.gpu', group: 'AI 算力', icon: 'device', requirePerm: 'gpu:read' } },

  // ChatOps
  { path: '/bot', name: 'bot', component: () => import('@/views/BotView.vue'), meta: { title: 'nav.bot', group: 'AI 算力', icon: 'flow', requirePerm: 'bot:read' } },

  // Runbook 自动化
  { path: '/runbooks', name: 'runbooks', component: () => import('@/views/RunbookView.vue'), meta: { title: 'nav.runbooks', group: '自动化', icon: 'task', requirePerm: 'runbook:read' } },

  // Incident 管理
  { path: '/incidents', name: 'incidents', component: () => import('@/views/IncidentView.vue'), meta: { title: 'nav.incidents', group: '自动化', icon: 'alerts', requirePerm: 'incident:read' } },

  // 自动扩缩容
  { path: '/autoscaler', name: 'autoscaler', component: () => import('@/views/AutoscalerView.vue'), meta: { title: 'nav.autoscaler', group: '自动化', icon: 'settings', requirePerm: 'autoscaler:read' } },

  // 自助服务门户
  { path: '/portal', name: 'portal', component: () => import('@/views/PortalView.vue'), meta: { title: 'nav.portal', group: '平台', icon: 'users', requirePerm: 'portal:read' } },

  // ======================================================================
  // 七域管理页（pipeline/argocd/compliance/ha/backups/quotas/tenants，
  // 后端 handler 在 internal/controlplane/server_lifecycle.go 注册，
  // API 契约见 src/api/{pipeline,argocd,compliance,ha,backup,quota,tenant}.js 注释头）。
  // ======================================================================
  // CI/CD 流水线（交付中心）
  { path: '/pipeline', name: 'pipeline', component: () => import('@/views/PipelineView.vue'), meta: { title: 'nav.pipeline', group: '交付中心', icon: 'flow', requirePerm: 'pipeline:read' } },

  // ArgoCD 应用（交付中心）
  { path: '/argocd', name: 'argocd', component: () => import('@/views/ArgoCDView.vue'), meta: { title: 'nav.argocd', group: '交付中心', icon: 'deploy', requirePerm: 'argocd:read' } },

  // 合规管理（系统管理）
  { path: '/compliance', name: 'compliance', component: () => import('@/views/ComplianceView.vue'), meta: { title: 'nav.compliance', group: '系统管理', icon: 'permissions', requirePerm: 'compliance:read' } },

  // 高可用状态（系统管理）
  { path: '/ha', name: 'ha', component: () => import('@/views/HAView.vue'), meta: { title: 'nav.ha', group: '系统管理', icon: 'device', requirePerm: 'ha:read' } },

  // 灾备备份（系统管理）
  { path: '/backups', name: 'backups', component: () => import('@/views/BackupsView.vue'), meta: { title: 'nav.backups', group: '系统管理', icon: 'alerts', requirePerm: 'backup:read' } },

  // 租户配额（系统管理，quotas 无独立权限点，复用 tenant:read）
  { path: '/quotas', name: 'quotas', component: () => import('@/views/QuotasView.vue'), meta: { title: 'nav.quotas', group: '系统管理', icon: 'settings', requirePerm: 'tenant:read' } },

  // 租户管理（系统管理）
  { path: '/tenants', name: 'tenants', component: () => import('@/views/TenantsView.vue'), meta: { title: 'nav.tenants', group: '系统管理', icon: 'users', requirePerm: 'tenant:read' } },

  // ======================================================================
  // 平台运营五域管理页（billing/apikeys/gateway-routes/audit-events/
  // notify-channels，后端 handler 在 internal/controlplane/
  // {billing,apikey,gateway,audit_query,server_alerts_m2}.go 注册）。
  // ======================================================================
  // 计费管理（平台：订阅计划 / 订阅 / 账单 / 用量）
  { path: '/billing', name: 'billing', component: () => import('@/views/BillingView.vue'), meta: { title: 'nav.billing', group: '平台', icon: 'cmdb', requirePerm: 'billing:read' } },

  // API Key 管理（平台）
  { path: '/apikeys', name: 'apikeys', component: () => import('@/views/APIKeysView.vue'), meta: { title: 'nav.apikeys', group: '平台', icon: 'key', requirePerm: 'apikey:read' } },

  // API 网关路由（平台）
  { path: '/gateway-routes', name: 'gateway-routes', component: () => import('@/views/GatewayRoutesView.vue'), meta: { title: 'nav.gatewayRoutes', group: '平台', icon: 'flow', requirePerm: 'gateway:read' } },

  // 审计事件（系统管理：只读查询 + 导出）
  { path: '/audit-events', name: 'audit-events', component: () => import('@/views/AuditEventsView.vue'), meta: { title: 'nav.auditEvents', group: '系统管理', icon: 'alerts', requirePerm: 'audit:read' } },

  // 通知渠道（平台：渠道 + 模板；无独立权限点，复用告警权限）
  { path: '/notify-channels', name: 'notify-channels', component: () => import('@/views/NotifyChannelsView.vue'), meta: { title: 'nav.notifyChannels', group: '平台', icon: 'alerts', requirePerm: 'alert:read' } },

  // ======================================================================
  // P1 五子域管理页（platform/federation/deploys-federation/config/
  // cmdb-advanced，后端 handler 在 internal/controlplane/{platform,federation,
  // deploys_federation,config_hotpush,cmdb_advanced,cmdb_attr_templates}.go
  // 注册，API 契约见 src/api/*.js 注释头）。
  // ======================================================================
  // 平台配置（系统管理）：配置 / 健康检查 / 指标汇总
  { path: '/platform', name: 'platform', component: () => import('@/views/PlatformConfigView.vue'), meta: { title: 'nav.platform', group: '系统管理', icon: 'settings', requirePerm: '' } },

  // 控制面联邦（系统管理）：Peer 管理 / 设备聚合 / 任务转发
  { path: '/federation', name: 'federation', component: () => import('@/views/FederationView.vue'), meta: { title: 'nav.federation', group: '系统管理', icon: 'flow', requirePerm: '' } },

  // 多集群联邦部署（交付中心）：联邦部署列表 + 创建 + 详情
  { path: '/deploys-federation', name: 'deploys-federation', component: () => import('@/views/FederationDeploysView.vue'), meta: { title: 'nav.federationDeploys', group: '交付中心', icon: 'deploy', requirePerm: 'deploy:read' } },

  // 配置热推送（运维管理）：热推送 / 灰度发布 / 版本历史
  { path: '/config', name: 'config', component: () => import('@/views/ConfigHotpushView.vue'), meta: { title: 'nav.config', group: '运维管理', icon: 'task', requirePerm: 'task:read' } },

  // CMDB 变更审批（运维管理）：变更列表 + 详情 + approve/reject
  { path: '/cmdb-changes', name: 'cmdb-changes', component: () => import('@/views/CMDBChangesView.vue'), meta: { title: 'nav.cmdbChanges', group: '运维管理', icon: 'cmdb', requirePerm: 'cmdb:read' } },

  // CMDB 属性模板（运维管理）：模板 CRUD
  { path: '/cmdb-attr-templates', name: 'cmdb-attr-templates', component: () => import('@/views/CMDBAttrTemplatesView.vue'), meta: { title: 'nav.cmdbAttrTemplates', group: '运维管理', icon: 'cmdb', requirePerm: 'cmdb:read' } },

  // CMDB 采集管理（运维管理）：触发采集 + CI 导入导出 + 待审批 CI
  { path: '/cmdb-collect', name: 'cmdb-collect', component: () => import('@/views/CMDBCollectView.vue'), meta: { title: 'nav.cmdbCollect', group: '运维管理', icon: 'cmdb', requirePerm: 'cmdb:read' } },

  // ======================================================================
  // P2 四子域管理页（approval/network/audits/provision，后端 handler 在
  // internal/controlplane/{approval,network,audit_query,provision}.go 注册，
  // API 契约见 src/api/{approval,network,audit,provision}.js 注释头）。
  // ======================================================================
  // 审批流定义（系统管理）：审批流 CRUD + 多级审批节点配置
  { path: '/approval-flows', name: 'approval-flows', component: () => import('@/views/ApprovalFlowsView.vue'), meta: { title: 'nav.approvalFlows', group: '系统管理', icon: 'flow', requirePerm: '' } },

  // 审批请求（系统管理）：审批请求列表 + 待我审批（approve/reject/cancel + history）
  { path: '/approval-requests', name: 'approval-requests', component: () => import('@/views/ApprovalRequestsView.vue'), meta: { title: 'nav.approvalRequests', group: '系统管理', icon: 'task', requirePerm: '' } },

  // 网络拓扑（运维管理）：拓扑图展示 + 刷新 + 缓存
  { path: '/network-topology', name: 'network-topology', component: () => import('@/views/NetworkTopologyView.vue'), meta: { title: 'nav.networkTopology', group: '运维管理', icon: 'device', requirePerm: '' } },

  // 网络诊断（运维管理）：诊断工具表单 + 结果展示 + 批量连通性检测
  { path: '/network-diagnose', name: 'network-diagnose', component: () => import('@/views/NetworkDiagnoseView.vue'), meta: { title: 'nav.networkDiagnose', group: '运维管理', icon: 'task', requirePerm: '' } },

  // 网络设备（运维管理）：设备 CRUD + 指标查看 + 配置下发 + 网络发现
  { path: '/network-devices', name: 'network-devices', component: () => import('@/views/NetworkDevicesView.vue'), meta: { title: 'nav.networkDevices', group: '运维管理', icon: 'cmdb', requirePerm: '' } },

  // 审计检索（系统管理）：筛选 action/user/时间范围 + 分页列表
  { path: '/audits', name: 'audits', component: () => import('@/views/AuditsView.vue'), meta: { title: 'nav.audits', group: '系统管理', icon: 'logs', requirePerm: '' } },

  // 自动纳管（运维管理）：触发表单 + 结果展示
  { path: '/auto-provision', name: 'auto-provision', component: () => import('@/views/AutoProvisionView.vue'), meta: { title: 'nav.autoProvision', group: '运维管理', icon: 'device', requirePerm: '' } },

  // ======================================================================
  // P3 Helm 应用商店三子域管理页（helm-repos/helm-catalog/helm-releases，
  // 后端 handler 在 internal/controlplane/helm.go 注册，
  // API 契约见 src/api/helm.js 注释头）。
  // ======================================================================
  // Helm 仓库管理（交付中心）：仓库列表 + 添加 + 删除 + 查看仓库 Chart
  { path: '/helm-repos', name: 'helm-repos', component: () => import('@/views/HelmReposView.vue'), meta: { title: 'nav.helmRepos', group: '交付中心', icon: 'cmdb', requirePerm: '' } },

  // Helm 应用目录（交付中心）：预置分类 + Chart 搜索 + Chart 详情
  { path: '/helm-catalog', name: 'helm-catalog', component: () => import('@/views/HelmCatalogView.vue'), meta: { title: 'nav.helmCatalog', group: '交付中心', icon: 'cmdb', requirePerm: '' } },

  // Helm Release 管理（交付中心）：列表 + 安装 + 升级 + 卸载 + 回滚 + 历史
  { path: '/helm-releases', name: 'helm-releases', component: () => import('@/views/HelmReleasesView.vue'), meta: { title: 'nav.helmReleases', group: '交付中心', icon: 'deploy', requirePerm: '' } }
]

const router = createRouter({
  history: createWebHistory('/enterprise/'),
  routes,
  scrollBehavior(to, from, savedPosition) {
    if (savedPosition) return savedPosition
    return { top: 0 }
  }
})

router.beforeEach(async (to) => {
  const auth = useAuthStore()
  await auth.ready
  if (!to.meta.public && !auth.isLoggedIn) {
    return { name: 'login' }
  }
  if (to.meta.public && auth.isLoggedIn && (to.name === 'login' || to.name === 'register')) {
    return { name: 'overview' }
  }
  if (to.meta.requirePerm && !auth.hasPerm(to.meta.requirePerm)) {
    // 权限不足静默重定向曾让用户困惑；跳转前 toast 提示（toast 自带同文案节流）
    toast.warn(t('error.noPermission'))
    return { name: 'overview' }
  }
  // 按需加载当前路由对应的功能域翻译（i18n 模块拆分后路由切换时异步加载）
  await loadRouteDomain(to.name)
  return true
})

function updateTitle(to) {
  const pageTitle = to.meta.title ? t(to.meta.title) : ''
  const appTitle = t('app.title')
  document.title = pageTitle ? `${pageTitle} · ${appTitle}` : appTitle
}
router.afterEach(updateTitle)
watch(currentLang, () => {
  const route = router.currentRoute.value
  if (route) updateTitle(route)
})

router.afterEach((to) => {
  if (to.meta.prefetch) return
  const currentIndex = routes.findIndex((r) => r.name === to.name)
  if (currentIndex === -1) return
  const nextRoute = routes[currentIndex + 1]
  if (nextRoute?.meta.prefetch && typeof nextRoute.component === 'function') {
    nextRoute.component()
  }
})

export { RouteSkeleton }
export default router
