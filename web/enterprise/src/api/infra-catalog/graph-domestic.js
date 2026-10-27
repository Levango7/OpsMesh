// 图数据库扩展 + 国产数据库/中间件动作目录（P7 目录缺口补齐）
// 由 infra-ops.js 合并导出；条目形状见 ./shared.js 注释。
//
// 契约来源（internal/controlplane/，2026-10-27 逐行核对请求 struct 与 action 分支）：
//   graph_ext_ops.go:12-72    handleGraphMonitor    POST /api/v1/graph/monitor     action: status/metrics/stores
//   graph_ext_ops.go:74-128   handleGraphScale      POST /api/v1/graph/scale       action: add-node/remove-node/status
//   graph_ext_ops.go:130-188  handleGraphUpgrade    POST /api/v1/graph/upgrade     action: plan/upgrade/rollback/version
//   graph_ext_ops.go:190-261  handleGraphUser       POST /api/v1/graph/user        action: list/create/delete
//   graph_ext_ops.go:263-333  handleGraphIndex      POST /api/v1/graph/index       action: create/delete/list
//   graph_ext_ops.go:335-405  handleGraphSchema     POST /api/v1/graph/schema      action: create-tag/create-edge/list-schema
//   domestic_ops.go:234-287   handleDomesticDB      POST /api/v1/db/domestic       action: status/deploy/backup
//   domestic_ops.go:293-363   handleDomesticMiddleware POST /api/v1/middleware/domestic action: status/deploy/start/stop
// （路由注册：graph_ext_ops.go:470-479、domestic_ops.go:369-377。）
// 全部端点仅接受 POST，一律走 dispatchKafkaTask 任务下发通道（202 + taskID），故 taskType 一律 'task'；
// list/status 等查询型动作同样经任务返回，在任务卡片中拉取结果。
//
// 注：graph/user 的 role、graph/index 的 database、db/domestic 的 mode 在请求 struct 中声明，
// 但当前 handler 各分支未读取，按契约收入并标注为“预留”。
import { T, deviceField } from './shared'

export const GRAPH_DOMESTIC_DOMAINS = [
  // --------------------------------------------------------------------------
  {
    id: 'graph-ext',
    name: T('图数据库扩展运维', 'Graph Extended Ops'),
    icon: 'cmdb',
    actions: [
      {
        id: 'graph-monitor', name: T('监控采集 (状态/指标/存储)', 'Graph Monitor'), method: 'POST', taskType: 'task',
        path: () => '/graph/monitor',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['neo4j', 'nebulagraph'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['status', 'metrics', 'stores'] }
        ]
      },
      {
        id: 'graph-scale', name: T('集群扩缩容 (增删节点)', 'Graph Scale'), method: 'POST', taskType: 'task',
        path: () => '/graph/scale',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['neo4j', 'nebulagraph'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['add-node', 'remove-node', 'status'] },
          { key: 'node', label: T('节点', 'Node'), type: 'text',
            placeholder: 'add-node/remove-node 需填；status 留空' }
        ]
      },
      {
        id: 'graph-upgrade', name: T('版本升级/回滚', 'Graph Upgrade'), method: 'POST', taskType: 'task',
        path: () => '/graph/upgrade',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['neo4j', 'nebulagraph'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['plan', 'upgrade', 'rollback', 'version'] },
          { key: 'targetVersion', label: T('目标版本', 'Target Version'), type: 'text',
            placeholder: 'upgrade 用，默认 5.0' }
        ]
      },
      {
        id: 'graph-user', name: T('用户管理', 'Graph Users'), method: 'POST', taskType: 'task',
        path: () => '/graph/user',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['neo4j', 'nebulagraph'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'create', 'delete'] },
          { key: 'user', label: T('用户名', 'User'), type: 'text', placeholder: 'create/delete 需填' },
          { key: 'password', label: T('密码', 'Password'), type: 'text', placeholder: 'create 用' },
          { key: 'role', label: T('角色', 'Role'), type: 'text', placeholder: 'create 预留（后端当前未读取）' }
        ]
      },
      {
        id: 'graph-index', name: T('索引管理', 'Graph Indexes'), method: 'POST', taskType: 'task',
        path: () => '/graph/index',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['neo4j', 'nebulagraph'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['create', 'delete', 'list'] },
          { key: 'database', label: T('数据库', 'Database'), type: 'text',
            placeholder: '预留（后端当前未读取）' },
          { key: 'index', label: T('索引名', 'Index'), type: 'text', placeholder: 'create/delete 需填' }
        ]
      },
      {
        id: 'graph-schema', name: T('Schema 管理 (Tag/Edge)', 'Graph Schema'), method: 'POST', taskType: 'task',
        path: () => '/graph/schema',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['neo4j', 'nebulagraph'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['create-tag', 'create-edge', 'list-schema'] },
          { key: 'name', label: T('名称', 'Name'), type: 'text', placeholder: 'create-tag/create-edge 需填' },
          { key: 'props', label: T('属性定义', 'Properties'), type: 'text',
            placeholder: '如 id string,name string；create-tag/create-edge 用（Neo4j create-tag 忽略）' }
        ]
      }
    ]
  },

  // --------------------------------------------------------------------------
  {
    id: 'domestic-db',
    name: T('国产数据库 (Kingbase/达梦)', 'Domestic DB'),
    icon: 'cmdb',
    actions: [
      {
        id: 'db-domestic', name: T('国产库运维', 'Domestic DB Ops'), method: 'POST', taskType: 'task',
        path: () => '/db/domestic',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['kingbase', 'dameng'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['status', 'deploy', 'backup'] },
          { key: 'version', label: T('版本', 'Version'), type: 'text', placeholder: 'deploy 用，默认 V9' },
          { key: 'mode', label: T('模式', 'Mode'), type: 'text', placeholder: '预留（后端当前未读取）' }
        ]
      }
    ]
  },

  // --------------------------------------------------------------------------
  {
    id: 'domestic-mw',
    name: T('国产中间件 (TongWeb/BES/InforSuite)', 'Domestic Middleware'),
    icon: 'deploy',
    actions: [
      {
        id: 'mw-domestic', name: T('国产中间件运维', 'Domestic Middleware Ops'), method: 'POST', taskType: 'task',
        path: () => '/middleware/domestic',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['tongweb', 'bes', 'inforsuite'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['status', 'deploy', 'start', 'stop'] },
          { key: 'version', label: T('版本', 'Version'), type: 'text', placeholder: 'deploy 用，默认 8.0' }
        ]
      }
    ]
  }
]
