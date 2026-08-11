// 共享 mock 数据 — 模拟后端 /api/v1/* 响应
// 所有 spec 通过 mockApi(page) 安装路由拦截，使 E2E 测试无需真实后端即可运行。
// 数据形态与 src/api/*.js 契约保持一致。

// 当前 mock 用户（登录后由 /auth/me 返回）
export const mockUser = {
  id: 'u-admin',
  username: 'admin',
  email: 'admin@opsmesh.local',
  status: 'active',
  role_ids: ['r-admin'],
  permissions: [
    'device:read', 'device:write',
    'task:read', 'task:write', 'task:cancel',
    'alert:read', 'alert:write',
    'cmdb:read',
    'workflow:read',
    'deploy:read',
    'log:read',
    'user:read', 'role:read'
  ]
}

// 设备列表：按网段分组（与 getDevices 契约一致）
export const mockDevices = {
  '10.0.1.0/24': [
    {
      deviceID: 'dev-001', hostname: 'web-node-1', ip: '10.0.1.11',
      state: 'managed', os: 'CentOS 7', agentID: 'agent-001',
      taskState: 'idle', lastResult: 'success', lastResultAt: '2026-08-10T10:00:00Z',
      tenantID: 't-default'
    },
    {
      deviceID: 'dev-002', hostname: 'web-node-2', ip: '10.0.1.12',
      state: 'discovered', os: 'Ubuntu 22.04', agentID: '',
      taskState: 'idle', lastResult: '', lastResultAt: '',
      tenantID: 't-default'
    }
  ],
  '10.0.2.0/24': [
    {
      deviceID: 'dev-003', hostname: 'db-node-1', ip: '10.0.2.21',
      state: 'managed', os: 'Rocky 9', agentID: 'agent-002',
      taskState: 'running', lastResult: 'failed', lastResultAt: '2026-08-10T11:30:00Z',
      tenantID: 't-default'
    }
  ]
}

// 设备详情（getDevice 契约）
export const mockDeviceDetail = {
  device: {
    deviceID: 'dev-001', hostname: 'web-node-1', ip: '10.0.1.11',
    state: 'managed', os: 'CentOS 7', agentID: 'agent-001',
    taskState: 'idle', lastResult: 'success', lastResultAt: '2026-08-10T10:00:00Z',
    tenantID: 't-default'
  },
  tasks: [
    { taskID: 't-001', type: 'shell', status: 'completed' },
    { taskID: 't-002', type: 'file', status: 'running' }
  ],
  results: [
    { taskID: 't-001', exitCode: 0, stdout: 'ok' }
  ]
}

// dev-002 详情（discovered 状态，用于测试纳管流程）
export const mockDeviceDetailDiscovered = {
  device: {
    deviceID: 'dev-002', hostname: 'web-node-2', ip: '10.0.1.12',
    state: 'discovered', os: 'Ubuntu 22.04', agentID: '',
    taskState: 'idle', lastResult: '', lastResultAt: '',
    tenantID: 't-default'
  },
  tasks: [],
  results: []
}

// Agent 列表（getAgents 契约）
export const mockAgents = [
  { agentID: 'agent-001', hostname: 'web-node-1' },
  { agentID: 'agent-002', hostname: 'db-node-1' }
]

// 任务列表（getTasks 契约）
export const mockTasks = [
  { taskID: 't-001', agentID: 'agent-001', type: 'shell', command: 'uptime', status: 'completed' },
  { taskID: 't-002', agentID: 'agent-002', type: 'shell', command: 'df -h', status: 'running' },
  { taskID: 't-003', agentID: 'agent-001', type: 'file', command: 'push nginx.conf', status: 'pending' }
]

// 告警列表（getAlerts 契约）
export const mockAlerts = [
  {
    alertID: 'a-001', severity: 'critical', status: 'firing',
    deviceID: 'dev-003', agentID: 'agent-002',
    message: '磁盘使用率 > 90%', createdAt: '2026-08-10T11:30:00Z',
    comment: '', acknowledgedBy: '', silencedUntil: ''
  },
  {
    alertID: 'a-002', severity: 'warning', status: 'firing',
    deviceID: 'dev-001', agentID: 'agent-001',
    message: 'CPU 使用率 > 80%', createdAt: '2026-08-10T10:00:00Z',
    comment: '', acknowledgedBy: '', silencedUntil: ''
  },
  {
    alertID: 'a-003', severity: 'warning', status: 'acknowledged',
    deviceID: 'dev-002', agentID: '',
    message: '设备离线', createdAt: '2026-08-09T08:00:00Z',
    comment: '已派人处理', acknowledgedBy: 'ops-li', silencedUntil: ''
  }
]

// K8s 集群列表（getK8sClusters 契约）
export const mockClusters = {
  clusters: [
    {
      id: 'c-prod', name: 'prod-cluster', server: 'https://10.10.0.1:6443',
      status: 'online', createdAt: '2026-07-01T00:00:00Z', updatedAt: '2026-08-01T00:00:00Z'
    },
    {
      id: 'c-staging', name: 'staging-cluster', server: 'https://10.20.0.1:6443',
      status: 'offline', createdAt: '2026-07-15T00:00:00Z', updatedAt: '2026-08-05T00:00:00Z'
    }
  ]
}

// K8s namespace 列表
export const mockNamespaces = {
  namespaces: [
    { name: 'default', status: 'Active', createdAt: '2026-07-01T00:00:00Z' },
    { name: 'kube-system', status: 'Active', createdAt: '2026-07-01T00:00:00Z' },
    { name: 'opsmesh', status: 'Active', createdAt: '2026-07-01T00:00:00Z' }
  ]
}

// K8s Pod 列表
export const mockPods = {
  pods: [
    { name: 'nginx-7b8f-x4k2z', namespace: 'default', status: 'Running', podIP: '10.244.0.5', nodeIP: '10.10.0.2', restarts: 0, age: '2d' },
    { name: 'api-6c9d-mn8pq', namespace: 'default', status: 'Running', podIP: '10.244.0.6', nodeIP: '10.10.0.3', restarts: 1, age: '1d' },
    { name: 'worker-failed', namespace: 'default', status: 'CrashLoopBackOff', podIP: '10.244.0.7', nodeIP: '10.10.0.2', restarts: 5, age: '3h' }
  ]
}

// K8s Deployment 列表
export const mockDeployments = {
  deployments: [
    { name: 'nginx-deploy', namespace: 'default', replicas: 3, availableReplicas: 3, image: 'nginx:1.25' },
    { name: 'api-deploy', namespace: 'default', replicas: 2, availableReplicas: 1, image: 'opsmesh/api:v1.2' }
  ]
}

// K8s Node 列表
export const mockNodes = {
  nodes: [
    { name: 'node-1', status: 'Ready', roles: ['master'], version: 'v1.28.0', internalIP: '10.10.0.1', externalIP: '-', cpu: '8', memory: '16Gi' },
    { name: 'node-2', status: 'Ready', roles: ['worker'], version: 'v1.28.0', internalIP: '10.10.0.2', externalIP: '-', cpu: '4', memory: '8Gi' }
  ]
}

// Pod 日志
export const mockPodLogs = { logs: '[2026-08-11] server started\n[2026-08-11] handled 100 reqs' }