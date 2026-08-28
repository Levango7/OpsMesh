// 路由表 — 概览 + 运维 + 资产 + 交付 + 观测 + 系统管理 + 登录/注册
import { createRouter, createWebHistory } from 'vue-router'
import { watch } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { t, currentLang } from '@/i18n'

const routes = [
  // 公共路由：登录 / 注册
  { path: '/login', name: 'login', component: () => import('@/views/LoginView.vue'), meta: { public: true, title: 'login.title' } },
  { path: '/register', name: 'register', component: () => import('@/views/RegisterView.vue'), meta: { public: true, title: 'register.title' } },

  // 概览
  { path: '/', redirect: '/overview' },
  { path: '/overview', name: 'overview', component: () => import('@/views/OverviewView.vue'), meta: { title: 'nav.home', group: '概览', icon: 'home', requirePerm: '' } },

  // 运维管理
  { path: '/devices', name: 'devices', component: () => import('@/views/DevicesView.vue'), meta: { title: 'nav.devices', group: '运维管理', icon: 'device', requirePerm: 'device:read' } },
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
  // 密钥管理：查看 provider 状态 + 测试连接 + 配置 Vault 地址
  { path: '/secrets', name: 'secrets', component: () => import('@/views/secrets/SecretsView.vue'), meta: { title: 'nav.secrets', group: '系统管理', icon: 'key', requirePerm: 'role:read' } },

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

  // 插件市场
  { path: '/plugins', name: 'plugins', component: () => import('@/views/PluginView.vue'), meta: { title: 'nav.plugins', group: '平台', icon: 'cmdb', requirePerm: 'plugin:read' } },

  // 自助服务门户
  { path: '/portal', name: 'portal', component: () => import('@/views/PortalView.vue'), meta: { title: 'nav.portal', group: '平台', icon: 'users', requirePerm: 'portal:read' } }
]

const router = createRouter({
  history: createWebHistory('/enterprise/'),
  routes
})

// 全局前置守卫：未登录时重定向到 /login（public 路由除外）
// async 化：等待 auth store 完成首次会话恢复（fetchMe）后再判断 isLoggedIn，
// 避免冷启动时序竞争——刷新已登录页面时 user 初始为 null，若同步判断会误重定向到 /login。
// 权限检查：meta.requirePerm 指定该路由所需权限（与 App.vue 侧栏 navGroups required 同源）；
//           空字符串表示无权限门槛（登录即可见）；未授权时重定向到 /overview。
router.beforeEach(async (to) => {
  const auth = useAuthStore()
  // 等待会话恢复完成（首次导航）；后续导航 ready 已 resolve，无额外开销
  await auth.ready
  if (!to.meta.public && !auth.isLoggedIn) {
    return { name: 'login' }
  }
  // 已登录访问登录/注册页时重定向到总览
  if (to.meta.public && auth.isLoggedIn && (to.name === 'login' || to.name === 'register')) {
    return { name: 'overview' }
  }
  // 权限检查：meta.requirePerm 存在且非空时，校验当前用户是否拥有该权限
  // hasPerm 对空权限集合放宽（demo 模式），与侧栏过滤逻辑一致
  if (to.meta.requirePerm && !auth.hasPerm(to.meta.requirePerm)) {
    return { name: 'overview' }
  }
  return true
})

// document.title 跟随当前语言：afterEach 设置初始 title，
// watch currentLang 确保语言切换时 title 同步更新（afterEach 仅在路由变更时触发）。
function updateTitle(to) {
  const pageTitle = to.meta.title ? t(to.meta.title) : ''
  const appTitle = t('app.title')
  document.title = pageTitle ? `${pageTitle} · ${appTitle}` : appTitle
}
router.afterEach(updateTitle)
// 语言切换时重新设置当前路由的 title（afterEach 不会因语言变化而重新触发）
watch(currentLang, () => {
  const route = router.currentRoute.value
  if (route) updateTitle(route)
})

export default router
