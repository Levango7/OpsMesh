// 缓存/GPU 深度/OLAP Doris/LB 动作目录（Redis 全套/Ignite/Memcached、DCGM/CUDA/MIG 等、Doris FE-BE、HAProxy 前端）
// 由 infra-ops.js 合并导出；条目形状见 ./shared.js 注释。
//
// 契约来源（internal/controlplane/*.go，2026-10-27 逐条核对）：
//   cache_ops.go（/cache/*）、gpu_ext_ops.go（/gpu/*）、olap_ext_ops.go（/olap/doris/*、/olap/clickhouse/*）
//   lb_ops.go（/lb/haproxy/frontend）
// 全部为 POST + 异步任务（返回 taskID → GET /tasks/{id}/result 取输出），taskType 统一 'task'。
import { T, deviceField } from './shared'

export const CACHE_GPU_DOMAINS = [
  // ==========================================================================
  // 缓存（11 条：/cache/*）
  // ==========================================================================
  {
    id: 'cache',
    name: T('缓存（Redis/Ignite/Memcached）', 'Cache'),
    icon: 'cmdb',
    actions: [
      {
        id: 'cache-deploy', name: T('部署', 'Deploy'), method: 'POST', taskType: 'task',
        path: () => '/cache/deploy',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', options: ['redis', 'memcached', 'ignite'] },
          { key: 'mode', label: T('模式', 'Mode'), type: 'select',
            options: ['standalone', 'master-slave', 'sentinel', 'cluster'] },
          { key: 'version', label: T('版本', 'Version'), type: 'text', placeholder: '7.2' },
          { key: 'port', label: T('端口', 'Port'), type: 'number' }
        ]
      },
      {
        id: 'cache-lifecycle', name: T('启停/状态', 'Lifecycle'), method: 'POST', taskType: 'task',
        path: () => '/cache/lifecycle',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', options: ['redis', 'memcached', 'ignite'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['start', 'stop', 'restart', 'status'] }
        ]
      },
      {
        id: 'cache-memcached', name: T('Memcached 管理', 'Memcached'), method: 'POST', taskType: 'task',
        path: () => '/cache/memcached',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['stats', 'slabs', 'items', 'flush'] }
        ]
      },
      {
        id: 'cache-ignite', name: T('Apache Ignite', 'Apache Ignite'), method: 'POST', taskType: 'task',
        path: () => '/cache/ignite',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['status', 'caches', 'list-nodes'] }
        ]
      },
      {
        id: 'redis-backup', name: T('Redis 备份/恢复', 'Redis Backup/Restore'), method: 'POST', taskType: 'task',
        path: () => '/cache/redis/backup',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['backup', 'restore', 'list'] },
          { key: 'path', label: T('备份路径', 'Path'), type: 'text', placeholder: '/var/lib/redis/dump.rdb' }
        ]
      },
      {
        id: 'redis-cluster', name: T('Redis Cluster', 'Redis Cluster'), method: 'POST', taskType: 'task',
        path: () => '/cache/redis/cluster',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['create', 'add-node', 'remove-node', 'reshard', 'nodes', 'slots', 'rebalance', 'failover'] },
          { key: 'nodes', label: T('节点列表', 'Nodes'), type: 'text', placeholder: 'host:port,host:port' },
          { key: 'replicas', label: T('副本数', 'Replicas'), type: 'number' },
          { key: 'slot', label: T('Slot 数', 'Slots'), type: 'number' },
          { key: 'fromNode', label: T('源节点', 'From Node'), type: 'text', placeholder: 'node-id' },
          { key: 'toNode', label: T('目标节点', 'To Node'), type: 'text', placeholder: 'node-id' }
        ]
      },
      {
        id: 'redis-monitor', name: T('Redis 监控', 'Redis Monitor'), method: 'POST', taskType: 'task',
        path: () => '/cache/redis/monitor',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['info', 'slowlog', 'stats', 'clients', 'replication', 'memory'] }
        ]
      },
      {
        id: 'redis-persistence', name: T('Redis 持久化', 'Redis Persistence'), method: 'POST', taskType: 'task',
        path: () => '/cache/redis/persistence',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['rdb-save', 'rdb-bgsave', 'aof-rewrite', 'aof-status', 'config'] }
        ]
      },
      {
        id: 'redis-security', name: T('Redis ACL', 'Redis ACL'), method: 'POST', taskType: 'task',
        path: () => '/cache/redis/security',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list-users', 'add-user', 'delete-user', 'set-acl'] },
          { key: 'user', label: T('用户', 'User'), type: 'text' },
          { key: 'password', label: T('密码', 'Password'), type: 'text' },
          { key: 'rules', label: T('ACL 规则', 'ACL Rules'), type: 'text', placeholder: '+@all ~*' }
        ]
      },
      {
        id: 'redis-sentinel', name: T('Redis Sentinel', 'Redis Sentinel'), method: 'POST', taskType: 'task',
        path: () => '/cache/redis/sentinel',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['monitor', 'failover', 'status', 'masters', 'slaves'] },
          { key: 'master', label: T('Master 名', 'Master'), type: 'text' },
          { key: 'host', label: T('Master 地址', 'Host'), type: 'text', placeholder: '127.0.0.1' },
          { key: 'port', label: T('Master 端口', 'Port'), type: 'number', placeholder: '6379' },
          { key: 'quorum', label: T('Quorum', 'Quorum'), type: 'number', placeholder: '2' }
        ]
      },
      {
        id: 'redis-tuning', name: T('Redis 调优', 'Redis Tuning'), method: 'POST', taskType: 'task',
        path: () => '/cache/redis/tuning',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['get', 'set', 'analyze', 'recommend'] },
          { key: 'maxMemory', label: T('最大内存', 'Max Memory'), type: 'text', placeholder: '2gb' },
          { key: 'eviction', label: T('淘汰策略', 'Eviction'), type: 'select',
            options: ['noeviction', 'allkeys-lru', 'allkeys-lfu', 'allkeys-random',
              'volatile-lru', 'volatile-lfu', 'volatile-random', 'volatile-ttl'] },
          { key: 'maxClients', label: T('最大连接数', 'Max Clients'), type: 'number', placeholder: '10000' },
          { key: 'ioThreads', label: T('IO 线程数', 'IO Threads'), type: 'number', placeholder: '4' }
        ]
      }
    ]
  },

  // ==========================================================================
  // GPU 深度（9 条：/gpu/*）
  // ==========================================================================
  {
    id: 'gpu-ext',
    name: T('GPU 深度运维', 'GPU Advanced'),
    icon: 'device',
    actions: [
      {
        id: 'gpu-anomaly', name: T('异常检测', 'Anomaly'), method: 'POST', taskType: 'task',
        path: () => '/gpu/anomaly',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['detect', 'history', 'health'] },
          { key: 'nodeId', label: T('节点', 'Node ID'), type: 'text' }
        ]
      },
      {
        id: 'gpu-cost', name: T('成本归因', 'Cost'), method: 'POST', taskType: 'task',
        path: () => '/gpu/cost',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['usage', 'billing', 'report'] },
          { key: 'tenantId', label: T('租户 ID', 'Tenant ID'), type: 'text' }
        ]
      },
      {
        id: 'gpu-cuda', name: T('CUDA Toolkit', 'CUDA Toolkit'), method: 'POST', taskType: 'task',
        path: () => '/gpu/cuda',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['info', 'install', 'upgrade'] },
          { key: 'version', label: T('版本', 'Version'), type: 'text', placeholder: '12.4' }
        ]
      },
      {
        id: 'gpu-dcgm', name: T('DCGM 管理', 'DCGM'), method: 'POST', taskType: 'task',
        path: () => '/gpu/dcgm',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['deploy', 'status', 'metrics', 'diag'] }
        ]
      },
      {
        id: 'gpu-driver', name: T('驱动管理', 'Driver'), method: 'POST', taskType: 'task',
        path: () => '/gpu/driver',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['info', 'install', 'upgrade', 'remove'] },
          { key: 'version', label: T('驱动版本', 'Version'), type: 'text', placeholder: '550.54.15' }
        ]
      },
      {
        id: 'gpu-mig', name: T('MIG 分区', 'MIG'), method: 'POST', taskType: 'task',
        path: () => '/gpu/mig',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['enable', 'disable', 'list', 'create', 'delete'] },
          { key: 'gpu', label: T('GPU 序号', 'GPU Index'), type: 'number', placeholder: '0' },
          { key: 'profile', label: T('Profile', 'Profile'), type: 'text', placeholder: '1g.5gb / 2g.10gb / 7g.40gb' },
          { key: 'migId', label: T('MIG 实例 ID', 'MIG ID'), type: 'text' }
        ]
      },
      {
        id: 'gpu-node-mgmt', name: T('节点管理', 'Node Management'), method: 'POST', taskType: 'task',
        path: () => '/gpu/node-mgmt',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['cordon', 'drain', 'uncordon', 'status'] },
          { key: 'nodeId', label: T('节点', 'Node ID'), type: 'text' }
        ]
      },
      {
        id: 'gpu-power', name: T('电源/功耗', 'Power'), method: 'POST', taskType: 'task',
        path: () => '/gpu/power',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['status', 'set-limit', 'reset'] },
          { key: 'gpu', label: T('GPU 序号', 'GPU Index'), type: 'number', placeholder: '0' },
          { key: 'limit', label: T('功耗上限(W)', 'Limit (W)'), type: 'number', placeholder: '250' }
        ]
      },
      {
        id: 'gpu-topology', name: T('拓扑/NUMA', 'Topology/NUMA'), method: 'POST', taskType: 'task',
        path: () => '/gpu/topology',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['topo', 'nvlink', 'numa', 'pcie'] }
        ]
      }
    ]
  },

  // ==========================================================================
  // Doris/ClickHouse 深度（8 条：/olap/doris/*、/olap/clickhouse/materialized-view）
  // ==========================================================================
  {
    id: 'olap-doris-more',
    name: T('OLAP · Doris/ClickHouse', 'OLAP · Doris/ClickHouse'),
    icon: 'cmdb',
    actions: [
      {
        id: 'olap-doris-backup', name: T('Doris 备份/恢复', 'Doris Backup/Restore'), method: 'POST', taskType: 'task',
        path: () => '/olap/doris/backup',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['backup', 'restore', 'list'] },
          { key: 'db', label: T('库名', 'Database'), type: 'text', placeholder: 'default' },
          { key: 'table', label: T('表名', 'Table'), type: 'text' },
          { key: 'path', label: T('备份路径', 'Path'), type: 'text', placeholder: '/var/backups/doris' }
        ]
      },
      {
        id: 'olap-doris-be', name: T('Doris BE 节点', 'Doris BE'), method: 'POST', taskType: 'task',
        path: () => '/olap/doris/be',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['status', 'add', 'remove', 'list'] },
          { key: 'host', label: T('主机', 'Host'), type: 'text' },
          { key: 'port', label: T('端口', 'Port'), type: 'number', placeholder: '9050' }
        ]
      },
      {
        id: 'olap-doris-fe', name: T('Doris FE 节点', 'Doris FE'), method: 'POST', taskType: 'task',
        path: () => '/olap/doris/fe',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['status', 'add', 'remove', 'list'] },
          { key: 'host', label: T('主机', 'Host'), type: 'text' },
          { key: 'port', label: T('端口', 'Port'), type: 'number', placeholder: '9010' }
        ]
      },
      {
        id: 'olap-doris-materialized-view', name: T('Doris 物化视图', 'Doris Materialized View'), method: 'POST', taskType: 'task',
        path: () => '/olap/doris/materialized-view',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['create', 'list', 'drop'] },
          { key: 'db', label: T('库名', 'Database'), type: 'text', placeholder: 'default' },
          { key: 'name', label: T('视图名', 'Name'), type: 'text' },
          { key: 'sql', label: T('定义 SQL', 'SQL'), type: 'textarea', placeholder: 'SELECT ...' }
        ]
      },
      {
        id: 'olap-doris-monitor', name: T('Doris 监控', 'Doris Monitor'), method: 'POST', taskType: 'task',
        path: () => '/olap/doris/monitor',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['status', 'metrics', 'compaction'] }
        ]
      },
      {
        id: 'olap-doris-upgrade', name: T('Doris 版本升级', 'Doris Upgrade'), method: 'POST', taskType: 'task',
        path: () => '/olap/doris/upgrade',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['plan', 'upgrade', 'rollback', 'version'] },
          { key: 'targetVersion', label: T('目标版本', 'Target Version'), type: 'text', placeholder: '2.1.0' }
        ]
      },
      {
        id: 'olap-doris-user', name: T('Doris 用户', 'Doris Users'), method: 'POST', taskType: 'task',
        path: () => '/olap/doris/user',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'create', 'delete'] },
          { key: 'user', label: T('用户', 'User'), type: 'text' },
          { key: 'password', label: T('密码', 'Password'), type: 'text' },
          { key: 'role', label: T('角色', 'Role'), type: 'text' }
        ]
      },
      {
        id: 'olap-clickhouse-materialized-view', name: T('ClickHouse 物化视图', 'ClickHouse Materialized View'), method: 'POST', taskType: 'task',
        path: () => '/olap/clickhouse/materialized-view',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['create', 'list', 'drop'] },
          { key: 'db', label: T('库名', 'Database'), type: 'text', placeholder: 'default' },
          { key: 'name', label: T('视图名', 'Name'), type: 'text' },
          { key: 'sql', label: T('定义 SQL', 'SQL'), type: 'textarea', placeholder: 'SELECT ...' }
        ]
      }
    ]
  },

  // ==========================================================================
  // LB 前端（1 条：/lb/haproxy/frontend）
  // ==========================================================================
  {
    id: 'lb-ext',
    name: T('负载均衡 · HAProxy 前端', 'LB · HAProxy Frontend'),
    icon: 'ops',
    actions: [
      {
        id: 'lb-haproxy-frontend', name: T('HAProxy 前端监听', 'HAProxy Frontend'), method: 'POST', taskType: 'task',
        path: () => '/lb/haproxy/frontend',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['add', 'delete', 'list'] },
          { key: 'frontend', label: T('前端地址', 'Frontend (host:port)'), type: 'text', placeholder: '0.0.0.0:80' }
        ]
      }
    ]
  }
]
