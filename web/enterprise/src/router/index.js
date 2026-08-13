// 路由表 — 概览 + 运维 + 资产 + 交付 + 观测 + 系统管理 + 登录/注册
import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const routes = [
  // 公共路由：登录 / 注册
  { path: '/login', name: 'login', component: () => import('@/views/LoginView.vue'), meta: { public: true, title: '登录' } },
  { path: '/register', name: 'register', component: () => import('@/views/RegisterView.vue'), meta: { public: true, title: '注册' } },

  // 概览
  { path: '/', redirect: '/overview' },
  { path: '/overview', name: 'overview', component: () => import('@/views/OverviewView.vue'), meta: { title: '总览', group: '概览', icon: 'home', requirePerm: '' } },

  // 运维管理
  { path: '/devices', name: 'devices', component: () => import('@/views/DevicesView.vue'), meta: { title: '设备纳管', group: '运维管理', icon: 'device', requirePerm: 'device:read' } },
  { path: '/devices/:id', name: 'device-detail', component: () => import('@/views/DeviceDetailView.vue'), meta: { title: '设备详情', group: '运维管理', icon: 'device', requirePerm: 'device:read' } },
  { path: '/tasks', name: 'tasks', component: () => import('@/views/TasksView.vue'), meta: { title: '任务下发', group: '运维管理', icon: 'task', requirePerm: 'task:read' } },
  { path: '/alerts', name: 'alerts', component: () => import('@/views/AlertsView.vue'), meta: { title: '监控告警', group: '运维管理', icon: 'alerts', requirePerm: 'alert:read' } },
  { path: '/os-optimize', name: 'os-optimize', component: () => import('@/views/OSOptimizeView.vue'), meta: { title: 'OS 优化', group: '运维管理', icon: 'task', requirePerm: 'task:read' } },
  { path: '/middleware', name: 'middleware', component: () => import('@/views/MiddlewareDeployView.vue'), meta: { title: '中间件部署', group: '运维管理', icon: 'deploy', requirePerm: 'deploy:read' } },
  { path: '/k8s', name: 'k8s', component: () => import('@/views/K8sManageView.vue'), meta: { title: 'K8s 管理', group: '运维管理', icon: 'device', requirePerm: 'device:read' } },

  // 资产配置
  { path: '/cmdb', name: 'cmdb', component: () => import('@/views/CMDBView.vue'), meta: { title: '配置项 CMDB', group: '资产配置', icon: 'cmdb', requirePerm: 'cmdb:read' } },

  // 交付中心
  { path: '/workflows', name: 'workflows', component: () => import('@/views/WorkflowsView.vue'), meta: { title: '作业编排', group: '交付中心', icon: 'flow', requirePerm: 'workflow:read' } },
  { path: '/deploys', name: 'deploys', component: () => import('@/views/DeploysView.vue'), meta: { title: '部署中心', group: '交付中心', icon: 'deploy', requirePerm: 'deploy:read' } },

  // 可观测性
  { path: '/logs', name: 'logs', component: () => import('@/views/LogsView.vue'), meta: { title: '日志检索', group: '可观测性', icon: 'logs', requirePerm: 'log:read' } },

  // 系统管理
  { path: '/users', name: 'users', component: () => import('@/views/UsersView.vue'), meta: { title: '用户中心', group: '系统管理', icon: 'users', requirePerm: 'user:read' } },
  { path: '/roles', name: 'roles', component: () => import('@/views/RolesView.vue'), meta: { title: '角色管理', group: '系统管理', icon: 'roles', requirePerm: 'role:read' } },
  { path: '/permissions', name: 'permissions', component: () => import('@/views/PermissionsView.vue'), meta: { title: '权限管理', group: '系统管理', icon: 'permissions', requirePerm: 'role:read' } },
  // task 267 密钥管理：查看 provider 状态 + 测试连接 + 配置 Vault 地址
  { path: '/secrets', name: 'secrets', component: () => import('@/views/secrets/SecretsView.vue'), meta: { title: '密钥管理', group: '系统管理', icon: 'key', requirePerm: 'role:read' } }
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

router.afterEach((to) => {
  document.title = to.meta.title ? `${to.meta.title} · OpsMesh 企业版` : 'OpsMesh 企业版'
})

export default router
