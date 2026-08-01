// 路由表 — 6 大功能页 + 概览首页
import { createRouter, createWebHistory } from 'vue-router'

const routes = [
  { path: '/', redirect: '/devices' },
  { path: '/devices', name: 'devices', component: () => import('@/views/DevicesView.vue'), meta: { title: '设备纳管', group: '运维' } },
  { path: '/tasks', name: 'tasks', component: () => import('@/views/TasksView.vue'), meta: { title: '任务下发', group: '运维' } },
  { path: '/alerts', name: 'alerts', component: () => import('@/views/AlertsView.vue'), meta: { title: '监控告警', group: '运维' } },
  { path: '/cmdb', name: 'cmdb', component: () => import('@/views/CMDBView.vue'), meta: { title: '配置项 CMDB', group: '资产' } },
  { path: '/workflows', name: 'workflows', component: () => import('@/views/WorkflowsView.vue'), meta: { title: '作业编排', group: '编排' } },
  { path: '/deploys', name: 'deploys', component: () => import('@/views/DeploysView.vue'), meta: { title: '部署中心', group: '编排' } },
  { path: '/logs', name: 'logs', component: () => import('@/views/LogsView.vue'), meta: { title: '日志检索', group: '观测' } }
]

const router = createRouter({
  history: createWebHistory('/enterprise/'),
  routes
})

router.afterEach((to) => {
  document.title = to.meta.title ? `${to.meta.title} · OpsMesh 企业版` : 'OpsMesh 企业版'
})

export default router