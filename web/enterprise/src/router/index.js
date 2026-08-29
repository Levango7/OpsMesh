// 路由表 — 概览 + 运维 + 资产 + 交付 + 观测 + 系统管理 + 登录/注册
import { createRouter, createWebHistory } from 'vue-router'
import { watch, defineComponent, h } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { t, currentLang } from '@/i18n'

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

  // 资产配置
  { path: '/cmdb', name: 'cmdb', component: () => import('@/views/CMDBView.vue'), meta: { title: 'nav.cmdb', group: '资产配置', icon: 'cmdb', requirePerm: 'cmdb:read' } },

  // 交付中心
  { path: '/workflows', name: 'workflows', component: () => import('@/views/WorkflowsView.vue'), meta: { title: 'nav.workflows', group: '交付中心', icon: 'flow', requirePerm: 'workflow:read' } },
  { path: '/deploys', name: 'deploys', component: () => import('@/views/DeploysView.vue'), meta: { title: 'nav.deploys', group: '交付中心', icon: 'deploy', requirePerm: 'deploy:read' } },

  // 可观测性
  { path: '/logs', name: 'logs', component: () => import('@/views/LogsView.vue'), meta: { title: 'nav.logs', group: '可观测性', icon: 'logs', requirePerm: 'log:read' } },

  // 系统管理
  { path: '/users', name: 'users', component: () => import('@/views/UsersView.vue'), meta: { title: 'nav.users', group: '系统管理', icon: 'users', requirePerm: 'user:read' } },
  { path: '/roles', name: 'roles', component: () => import('@/views/RolesView.vue'), meta: { title: 'nav.roles', group: '系统管理', icon: 'roles', requirePerm: 'role:read' } },
  { path: '/permissions', name: 'permissions', component: () => import('@/views/PermissionsView.vue'), meta: { title: 'nav.permissions', group: '系统管理', icon: 'permissions', requirePerm: 'role:read' } },
  { path: '/secrets', name: 'secrets', component: () => import('@/views/secrets/SecretsView.vue'), meta: { title: 'nav.secrets', group: '系统管理', icon: 'key', requirePerm: 'role:read' } },

  // ================= 插件市场 =================
  // 保留：后端有真实端点 /api/v1/marketplace/plugins（见 src/api/plugin.js），修复端点后可用。
  { path: '/plugins', name: 'plugins', component: () => import('@/views/PluginView.vue'), meta: { title: 'nav.plugins', group: '平台', icon: 'cmdb', requirePerm: 'plugin:read' } }

  // ======================================================================
  // 以下路由【已停用】：
  //   /gpu /bot /runbooks /incidents /autoscaler /portal
  // 后端 controlplane 目前没有这些模块的路由（均已核实 server_lifecycle.go
  // 全量路由），前端注册会导致打开即 404。保留视图与 API 文件，待后端
  // 路由就绪后取消注释即可恢复。恢复时取消注释：
  // ======================================================================
  // // GPU 资源管理
  // { path: '/gpu', name: 'gpu', component: () => import('@/views/GPUView.vue'), meta: { title: 'nav.gpu', group: 'AI 算力', icon: 'device', requirePerm: 'gpu:read' } },
  //
  // // ChatOps
  // { path: '/bot', name: 'bot', component: () => import('@/views/BotView.vue'), meta: { title: 'nav.bot', group: 'AI 算力', icon: 'flow', requirePerm: 'bot:read' } },
  //
  // // Runbook 自动化
  // { path: '/runbooks', name: 'runbooks', component: () => import('@/views/RunbookView.vue'), meta: { title: 'nav.runbooks', group: '自动化', icon: 'task', requirePerm: 'runbook:read' } },
  //
  // // Incident 管理
  // { path: '/incidents', name: 'incidents', component: () => import('@/views/IncidentView.vue'), meta: { title: 'nav.incidents', group: '自动化', icon: 'alerts', requirePerm: 'incident:read' } },
  //
  // // 自动扩缩容
  // { path: '/autoscaler', name: 'autoscaler', component: () => import('@/views/AutoscalerView.vue'), meta: { title: 'nav.autoscaler', group: '自动化', icon: 'settings', requirePerm: 'autoscaler:read' } },
  //
  // // 自助服务门户
  // { path: '/portal', name: 'portal', component: () => import('@/views/PortalView.vue'), meta: { title: 'nav.portal', group: '平台', icon: 'users', requirePerm: 'portal:read' } }
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
    return { name: 'overview' }
  }
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
