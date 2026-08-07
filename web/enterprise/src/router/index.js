// 路由表 — 概览 + 运维 + 资产 + 交付 + 观测 + 系统管理 + 登录/注册
import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const routes = [
  // 公共路由：登录 / 注册
  { path: '/login', name: 'login', component: () => import('@/views/LoginView.vue'), meta: { public: true, title: '登录' } },
  { path: '/register', name: 'register', component: () => import('@/views/RegisterView.vue'), meta: { public: true, title: '注册' } },

  // 概览
  { path: '/', redirect: '/overview' },
  { path: '/overview', name: 'overview', component: () => import('@/views/OverviewView.vue'), meta: { title: '总览', group: '概览', icon: 'home' } },

  // 运维管理
  { path: '/devices', name: 'devices', component: () => import('@/views/DevicesView.vue'), meta: { title: '设备纳管', group: '运维管理', icon: 'device' } },
  { path: '/devices/:id', name: 'device-detail', component: () => import('@/views/DeviceDetailView.vue'), meta: { title: '设备详情', group: '运维管理', icon: 'device' } },
  { path: '/tasks', name: 'tasks', component: () => import('@/views/TasksView.vue'), meta: { title: '任务下发', group: '运维管理', icon: 'task' } },
  { path: '/alerts', name: 'alerts', component: () => import('@/views/AlertsView.vue'), meta: { title: '监控告警', group: '运维管理', icon: 'alerts' } },
  { path: '/os-optimize', name: 'os-optimize', component: () => import('@/views/OSOptimizeView.vue'), meta: { title: 'OS 优化', group: '运维管理', icon: 'task' } },
  { path: '/middleware', name: 'middleware', component: () => import('@/views/MiddlewareDeployView.vue'), meta: { title: '中间件部署', group: '运维管理', icon: 'deploy' } },
  { path: '/k8s', name: 'k8s', component: () => import('@/views/K8sManageView.vue'), meta: { title: 'K8s 管理', group: '运维管理', icon: 'device' } },

  // 资产配置
  { path: '/cmdb', name: 'cmdb', component: () => import('@/views/CMDBView.vue'), meta: { title: '配置项 CMDB', group: '资产配置', icon: 'cmdb' } },

  // 交付中心
  { path: '/workflows', name: 'workflows', component: () => import('@/views/WorkflowsView.vue'), meta: { title: '作业编排', group: '交付中心', icon: 'flow' } },
  { path: '/deploys', name: 'deploys', component: () => import('@/views/DeploysView.vue'), meta: { title: '部署中心', group: '交付中心', icon: 'deploy' } },

  // 可观测性
  { path: '/logs', name: 'logs', component: () => import('@/views/LogsView.vue'), meta: { title: '日志检索', group: '可观测性', icon: 'logs' } },

  // 系统管理
  { path: '/users', name: 'users', component: () => import('@/views/UsersView.vue'), meta: { title: '用户中心', group: '系统管理', icon: 'users' } },
  { path: '/roles', name: 'roles', component: () => import('@/views/RolesView.vue'), meta: { title: '角色管理', group: '系统管理', icon: 'roles' } },
  { path: '/permissions', name: 'permissions', component: () => import('@/views/PermissionsView.vue'), meta: { title: '权限管理', group: '系统管理', icon: 'permissions' } }
]

const router = createRouter({
  history: createWebHistory('/enterprise/'),
  routes
})

// 全局前置守卫：未登录时重定向到 /login（public 路由除外）
// async 化：等待 auth store 完成首次会话恢复（fetchMe）后再判断 isLoggedIn，
// 避免冷启动时序竞争——刷新已登录页面时 user 初始为 null，若同步判断会误重定向到 /login。
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
  return true
})

router.afterEach((to) => {
  document.title = to.meta.title ? `${to.meta.title} · OpsMesh 企业版` : 'OpsMesh 企业版'
})

export default router
