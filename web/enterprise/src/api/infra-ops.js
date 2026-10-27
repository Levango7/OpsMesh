// 基础设施运维 API — P4/P5/P6 新增能力的统一前端接入层。
//
// 设计：数据驱动的动作目录（INFRA_DOMAINS）——
// 每个 domain 声明其动作（method/path/fields），由 InfraConsoleView 动态渲染表单并提交，
// 无需为每个 domain 单独写视图。新增能力只需在此追加目录条目。
//
// 契约（internal/controlplane/*_ops.go）：所有执行类接口返回 Task（异步下发到 agent），
// 结果经 GET /tasks/{id}/result 查询；查询类接口直接返回结构化 JSON。
import { getJSON, postJSON, deleteJSONBody } from './request'
import { T, deviceField, lifecycleActionOptions } from './infra-catalog/shared'
import { OS_DOMAINS } from './infra-catalog/os'
import { SEARCH_DOMAINS } from './infra-catalog/search'
import { MQ_DEEP_DOMAINS } from './infra-catalog/mq-deep'
import { CACHE_GPU_DOMAINS } from './infra-catalog/cache-gpu'
import { OPS_SUITE_DOMAINS } from './infra-catalog/ops-suite'
import { GRAPH_DOMESTIC_DOMAINS } from './infra-catalog/graph-domestic'

// 字段类型枚举（视图层动态渲染）：
//   device  — 设备下拉（从 /devices 拉取）
//   text    — 单行文本
//   number  — 数字
//   select  — 下拉（options: [{value,label}] 或 string[]）
//   bool    — 复选框
//   textarea- 多行文本（json/sql/playbook 内容）
//
// 动作结构：
//   { id, name:{zh,en}, method:'GET'|'POST'|'DELETE', path:(f)=>string,
//     fields:[{key, label:{zh,en}, type, required, options, placeholder}],
//     taskType: 'task'|'query'  — task=显示任务卡片; query=直接显示 JSON 结果 }







// ============================================================================
// Domain 目录
// ============================================================================

const CORE_DOMAINS = [
  // --------------------------------------------------------------------------
  {
    id: 'middleware',
    name: T('消息队列/中间件', 'Middleware'),
    icon: 'deploy',
    actions: [
      {
        id: 'status', name: T('状态查询', 'Status'), method: 'GET', taskType: 'query',
        path: (f) => `/middleware/${encodeURIComponent(f.deviceID)}`,
        fields: [deviceField()]
      },
      {
        id: 'deploy', name: T('部署', 'Deploy'), method: 'POST',
        path: () => '/middleware/deploy',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['redis', 'kafka', 'zookeeper', 'rabbitmq', 'activemq'] },
          { key: 'mode', label: T('模式', 'Mode'), type: 'select', required: true,
            options: ['standalone', 'master-slave', 'sentinel', 'cluster', 'single', 'multi'] },
          { key: 'version', label: T('版本', 'Version'), type: 'text', placeholder: '如 7.0' }
        ]
      },
      {
        id: 'lifecycle', name: T('启停/状态', 'Lifecycle'), method: 'POST',
        path: (f) => `/middleware/${encodeURIComponent(f.type)}/${encodeURIComponent(f.action)}`,
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['redis', 'kafka', 'zookeeper', 'rabbitmq', 'activemq'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: lifecycleActionOptions }
        ]
      },
      {
        id: 'kafka-topic', name: T('Kafka Topic', 'Kafka Topic'), method: 'POST',
        path: () => '/middleware/kafka/topic',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: [{ value: 'list', label: 'list' }, { value: 'create', label: 'create' }, { value: 'delete', label: 'delete' }] },
          { key: 'topic', label: T('Topic 名', 'Topic'), type: 'text', required: true },
          { key: 'partitions', label: T('分区数', 'Partitions'), type: 'number' },
          { key: 'replicas', label: T('副本数', 'Replicas'), type: 'number' }
        ]
      },
      {
        id: 'kafka-group', name: T('Kafka 消费组', 'Kafka Consumer Group'), method: 'POST',
        path: () => '/middleware/kafka/consumer-group',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: [{ value: 'list', label: 'list' }, { value: 'describe', label: 'describe' }] },
          { key: 'group', label: T('消费组', 'Group'), type: 'text' }
        ]
      },
      {
        id: 'redis-sentinel', name: T('Redis Sentinel', 'Redis Sentinel'), method: 'POST',
        path: () => '/middleware/redis/sentinel',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: [{ value: 'monitor', label: 'monitor' }, { value: 'failover', label: 'failover' }] },
          { key: 'master', label: T('Master 名', 'Master'), type: 'text', required: true },
          { key: 'host', label: T('Sentinel 地址', 'Sentinel Host'), type: 'text', placeholder: 'localhost:26379' }
        ]
      },
      {
        id: 'zookeeper-cmd', name: T('Zookeeper 四字命令', 'Zookeeper 4lw'), method: 'POST',
        path: () => '/middleware/zookeeper/cmd',
        fields: [
          deviceField(),
          { key: 'cmd', label: T('命令', 'Command'), type: 'select', required: true,
            options: ['stat', 'ruok', 'mntr', 'cons', 'envi', 'conf', 'dump', 'wchs'] },
          { key: 'host', label: T('地址', 'Host'), type: 'text', placeholder: 'localhost:2181' }
        ]
      },
      {
        id: 'rabbitmq', name: T('RabbitMQ 管理', 'RabbitMQ'), method: 'POST',
        path: (f) => `/middleware/rabbitmq/${encodeURIComponent(f.resource)}`,
        fields: [
          deviceField(),
          { key: 'resource', label: T('资源', 'Resource'), type: 'select', required: true,
            options: ['vhost', 'queue', 'exchange'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: [{ value: 'list', label: 'list' }, { value: 'create', label: 'create' }, { value: 'delete', label: 'delete' }] },
          { key: 'name', label: T('名称', 'Name'), type: 'text' },
          { key: 'vhost', label: T('Vhost', 'Vhost'), type: 'text', placeholder: '/' }
        ]
      }
    ]
  },

  // --------------------------------------------------------------------------
  {
    id: 'db',
    name: T('数据库（企业级）', 'Databases'),
    icon: 'cmdb',
    actions: [
      {
        id: 'status', name: T('状态查询', 'Status'), method: 'GET', taskType: 'query',
        path: (f) => `/db/${encodeURIComponent(f.deviceID)}`,
        fields: [deviceField()]
      },
      {
        id: 'deploy', name: T('部署', 'Deploy'), method: 'POST',
        path: () => '/db/deploy',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['postgresql', 'oracle', 'tidb', 'oceanbase'] },
          { key: 'mode', label: T('模式', 'Mode'), type: 'select', required: true,
            options: ['single', 'ha', 'rac', 'cluster'] },
          { key: 'version', label: T('版本', 'Version'), type: 'text' }
        ]
      },
      {
        id: 'lifecycle', name: T('启停/状态', 'Lifecycle'), method: 'POST',
        path: (f) => `/db/${encodeURIComponent(f.type)}/${encodeURIComponent(f.action)}`,
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['postgresql', 'oracle', 'tidb', 'oceanbase'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: lifecycleActionOptions }
        ]
      },
      {
        id: 'instance', name: T('实例管理', 'Instance'), method: 'POST',
        path: (f) => `/db/${encodeURIComponent(f.type)}/instance`,
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['postgresql', 'oracle', 'tidb', 'oceanbase'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: [{ value: 'list', label: 'list' }, { value: 'create', label: 'create' }, { value: 'drop', label: 'drop' }] },
          { key: 'name', label: T('库名', 'DB Name'), type: 'text' }
        ]
      },
      {
        id: 'mongo-lifecycle', name: T('MongoDB 启停', 'MongoDB Lifecycle'), method: 'POST',
        path: () => '/mongo/lifecycle',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: lifecycleActionOptions }
        ]
      },
      {
        id: 'mongo-deploy', name: T('MongoDB 部署', 'MongoDB Deploy'), method: 'POST',
        path: () => '/mongo/deploy',
        fields: [
          deviceField(),
          { key: 'mode', label: T('模式', 'Mode'), type: 'select', required: true,
            options: ['single', 'replicaSet', 'sharded'] },
          { key: 'version', label: T('版本', 'Version'), type: 'text' },
          { key: 'port', label: T('端口', 'Port'), type: 'number' }
        ]
      },
      {
        id: 'mongo-query', name: T('MongoDB 查询', 'MongoDB Query'), method: 'POST',
        path: () => '/mongo/query',
        fields: [
          deviceField(),
          { key: 'database', label: T('数据库', 'Database'), type: 'text', required: true },
          { key: 'collection', label: T('集合', 'Collection'), type: 'text', required: true },
          { key: 'filter', label: T('过滤条件(JSON)', 'Filter (JSON)'), type: 'text', placeholder: '{}' },
          { key: 'limit', label: T('返回条数', 'Limit'), type: 'number' }
        ]
      },
      {
        id: 'mongo-backup', name: T('MongoDB 备份', 'MongoDB Backup'), method: 'POST',
        path: () => '/mongo/backup',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: [{ value: 'export', label: 'export' }, { value: 'import', label: 'import' }] },
          { key: 'database', label: T('数据库', 'Database'), type: 'text', required: true },
          { key: 'path', label: T('备份路径', 'Path'), type: 'text', placeholder: '/backup/mongodb' }
        ]
      }
    ]
  },

  // --------------------------------------------------------------------------
  {
    id: 'olap',
    name: T('OLAP 分析引擎', 'OLAP'),
    icon: 'cmdb',
    actions: [
      {
        id: 'status', name: T('状态查询', 'Status'), method: 'GET', taskType: 'query',
        path: (f) => `/olap/${encodeURIComponent(f.deviceID)}`,
        fields: [deviceField()]
      },
      {
        id: 'deploy', name: T('部署', 'Deploy'), method: 'POST',
        path: () => '/olap/deploy',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['clickhouse', 'doris', 'trino'] },
          { key: 'mode', label: T('模式', 'Mode'), type: 'select', required: true,
            options: ['single', 'cluster'] },
          { key: 'version', label: T('版本', 'Version'), type: 'text' }
        ]
      },
      {
        id: 'lifecycle', name: T('启停/状态', 'Lifecycle'), method: 'POST',
        path: (f) => `/olap/${encodeURIComponent(f.type)}/${encodeURIComponent(f.action)}`,
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['clickhouse', 'doris', 'trino'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: lifecycleActionOptions }
        ]
      },
      {
        id: 'query', name: T('SQL 查询', 'SQL Query'), method: 'POST',
        path: (f) => `/olap/${encodeURIComponent(f.type)}/query`,
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['clickhouse', 'doris', 'trino'] },
          { key: 'database', label: T('库名', 'Database'), type: 'text' },
          { key: 'query', label: T('SQL', 'SQL'), type: 'text', required: true, placeholder: 'SELECT version()' }
        ]
      },
      {
        id: 'database', name: T('库管理', 'Database'), method: 'POST',
        path: (f) => `/olap/${encodeURIComponent(f.type)}/database`,
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['clickhouse', 'doris', 'trino'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: [{ value: 'list', label: 'list' }, { value: 'create', label: 'create' }, { value: 'delete', label: 'delete' }] },
          { key: 'name', label: T('库名', 'Name'), type: 'text' }
        ]
      }
    ]
  },

  // --------------------------------------------------------------------------
  {
    id: 'tsdb',
    name: T('时序/流数据存储', 'Time-Series DB'),
    icon: 'alerts',
    actions: [
      {
        id: 'status', name: T('状态查询', 'Status'), method: 'GET', taskType: 'query',
        path: (f) => `/tsdb/${encodeURIComponent(f.deviceID)}`,
        fields: [deviceField()]
      },
      {
        id: 'deploy', name: T('部署', 'Deploy'), method: 'POST',
        path: () => '/tsdb/deploy',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['influxdb', 'tdengine', 'victoriametrics'] },
          { key: 'version', label: T('版本', 'Version'), type: 'text' }
        ]
      },
      {
        id: 'lifecycle', name: T('启停/状态', 'Lifecycle'), method: 'POST',
        path: (f) => `/tsdb/${encodeURIComponent(f.type)}/${encodeURIComponent(f.action)}`,
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['influxdb', 'tdengine', 'victoriametrics'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: lifecycleActionOptions }
        ]
      },
      {
        id: 'query', name: T('查询', 'Query'), method: 'POST',
        path: (f) => `/tsdb/${encodeURIComponent(f.type)}/query`,
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['influxdb', 'tdengine', 'victoriametrics'] },
          { key: 'database', label: T('数据库', 'Database'), type: 'text' },
          { key: 'query', label: T('查询语句', 'Query'), type: 'text', required: true,
            placeholder: 'InfluxQL/SQL/PromQL' }
        ]
      },
      {
        id: 'write', name: T('写入', 'Write'), method: 'POST',
        path: (f) => `/tsdb/${encodeURIComponent(f.type)}/write`,
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['influxdb', 'tdengine', 'victoriametrics'] },
          { key: 'database', label: T('数据库', 'Database'), type: 'text' },
          { key: 'line', label: T('数据点', 'Data Point'), type: 'textarea', required: true,
            placeholder: 'measurement,tag=value field=1 (line protocol / SQL VALUES)' }
        ]
      }
    ]
  },

  // --------------------------------------------------------------------------
  {
    id: 'vector',
    name: T('向量数据库', 'Vector DB'),
    icon: 'gpu',
    actions: [
      {
        id: 'status', name: T('状态查询', 'Status'), method: 'GET', taskType: 'query',
        path: (f) => `/vector/${encodeURIComponent(f.deviceID)}`,
        fields: [deviceField()]
      },
      {
        id: 'deploy', name: T('部署', 'Deploy'), method: 'POST',
        path: () => '/vector/deploy',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['milvus', 'qdrant', 'pgvector'] },
          { key: 'mode', label: T('模式', 'Mode'), type: 'select', required: true,
            options: ['single', 'cluster'] },
          { key: 'version', label: T('版本', 'Version'), type: 'text' }
        ]
      },
      {
        id: 'lifecycle', name: T('启停/扩缩容', 'Lifecycle/Scale'), method: 'POST',
        path: (f) => `/vector/${encodeURIComponent(f.type)}/${encodeURIComponent(f.action)}`,
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['milvus', 'qdrant', 'pgvector'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: [...lifecycleActionOptions, { value: 'scale', label: 'scale' }] },
          { key: 'replicas', label: T('副本数(scale)', 'Replicas'), type: 'number' }
        ]
      },
      {
        id: 'collection', name: T('集合管理', 'Collection'), method: 'POST',
        path: (f) => `/vector/${encodeURIComponent(f.type)}/collection`,
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['milvus', 'qdrant', 'pgvector'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: [{ value: 'list', label: 'list' }, { value: 'create', label: 'create' }, { value: 'delete', label: 'delete' }] },
          { key: 'collection', label: T('集合名', 'Collection'), type: 'text' },
          { key: 'dimension', label: T('维度', 'Dimension'), type: 'number', placeholder: '如 768' },
          { key: 'metric', label: T('距离度量', 'Metric'), type: 'select',
            options: ['COSINE', 'L2', 'IP', 'Cosine', 'Euclid', 'Dot'] }
        ]
      },
      {
        id: 'search', name: T('向量检索', 'Vector Search'), method: 'POST',
        path: (f) => `/vector/${encodeURIComponent(f.type)}/search`,
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['milvus', 'qdrant', 'pgvector'] },
          { key: 'collection', label: T('集合名', 'Collection'), type: 'text', required: true },
          { key: 'vector', label: T('查询向量(JSON 数组)', 'Vector (JSON)'), type: 'textarea', required: true,
            placeholder: '[0.1, 0.2, 0.3, ...]' },
          { key: 'topK', label: T('TopK', 'TopK'), type: 'number' }
        ]
      },
      {
        id: 'migrate', name: T('迁移', 'Migrate'), method: 'POST',
        path: (f) => `/vector/${encodeURIComponent(f.type)}/migrate`,
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['milvus', 'qdrant', 'pgvector'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: [{ value: 'export', label: 'export' }, { value: 'import', label: 'import' }] },
          { key: 'collection', label: T('集合名', 'Collection'), type: 'text', required: true }
        ]
      }
    ]
  },

  // --------------------------------------------------------------------------
  {
    id: 'graph',
    name: T('图数据库', 'Graph DB'),
    icon: 'cmdb',
    actions: [
      {
        id: 'status', name: T('状态查询', 'Status'), method: 'GET', taskType: 'query',
        path: (f) => `/graph/${encodeURIComponent(f.deviceID)}`,
        fields: [deviceField()]
      },
      {
        id: 'deploy', name: T('部署', 'Deploy'), method: 'POST',
        path: () => '/graph/deploy',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['neo4j', 'nebulagraph'] },
          { key: 'mode', label: T('模式', 'Mode'), type: 'select', required: true,
            options: ['single', 'cluster'] },
          { key: 'version', label: T('版本', 'Version'), type: 'text' }
        ]
      },
      {
        id: 'lifecycle', name: T('启停/状态', 'Lifecycle'), method: 'POST',
        path: (f) => `/graph/${encodeURIComponent(f.type)}/${encodeURIComponent(f.action)}`,
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['neo4j', 'nebulagraph'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: lifecycleActionOptions }
        ]
      },
      {
        id: 'query', name: T('图查询', 'Graph Query'), method: 'POST',
        path: (f) => `/graph/${encodeURIComponent(f.type)}/cypher`,
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['neo4j', 'nebulagraph'] },
          { key: 'database', label: T('库/图空间', 'Database/Space'), type: 'text' },
          { key: 'query', label: T('查询', 'Query'), type: 'text', required: true,
            placeholder: 'Cypher / nGQL' }
        ]
      },
      {
        id: 'backup', name: T('备份/恢复', 'Backup'), method: 'POST',
        path: (f) => `/graph/${encodeURIComponent(f.type)}/backup`,
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['neo4j', 'nebulagraph'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: [{ value: 'export', label: 'export' }, { value: 'import', label: 'import' }] },
          { key: 'database', label: T('库/图空间', 'Database'), type: 'text' }
        ]
      }
    ]
  },

  // --------------------------------------------------------------------------
  {
    id: 'lb',
    name: T('负载均衡', 'Load Balancer'),
    icon: 'flow',
    actions: [
      {
        id: 'status', name: T('状态查询', 'Status'), method: 'GET', taskType: 'query',
        path: (f) => `/lb/${encodeURIComponent(f.deviceID)}`,
        fields: [deviceField()]
      },
      {
        id: 'deploy', name: T('部署', 'Deploy'), method: 'POST',
        path: () => '/lb/deploy',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['haproxy', 'lvs', 'keepalived'] },
          { key: 'mode', label: T('模式', 'Mode'), type: 'select', required: true,
            options: ['single', 'ha', 'nat', 'dr', 'tun', 'vrrp'] }
        ]
      },
      {
        id: 'lifecycle', name: T('启停/状态', 'Lifecycle'), method: 'POST',
        path: (f) => `/lb/${encodeURIComponent(f.type)}/${encodeURIComponent(f.action)}`,
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['haproxy', 'lvs', 'keepalived'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: lifecycleActionOptions }
        ]
      },
      {
        id: 'backend', name: T('后端管理', 'Backend'), method: 'POST',
        path: (f) => `/lb/${encodeURIComponent(f.type)}/backend`,
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['haproxy', 'lvs'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'add', 'delete', 'enable', 'disable'] },
          { key: 'backend', label: T('后端地址', 'Backend'), type: 'text', placeholder: 'host:port' },
          { key: 'weight', label: T('权重', 'Weight'), type: 'number' }
        ]
      }
    ]
  },

  // --------------------------------------------------------------------------
  {
    id: 'storage',
    name: T('对象存储', 'Object Storage'),
    icon: 'cmdb',
    actions: [
      {
        id: 'status', name: T('状态查询', 'Status'), method: 'GET', taskType: 'query',
        path: (f) => `/storage/${encodeURIComponent(f.deviceID)}`,
        fields: [deviceField()]
      },
      {
        id: 'deploy', name: T('部署 MinIO', 'Deploy MinIO'), method: 'POST',
        path: () => '/storage/deploy',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['minio', 's3', 'oss'] },
          { key: 'mode', label: T('模式', 'Mode'), type: 'select', options: ['single', 'cluster'] }
        ]
      },
      {
        id: 'bucket', name: T('桶管理', 'Bucket'), method: 'POST',
        path: (f) => `/storage/${encodeURIComponent(f.type)}/bucket`,
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['s3', 'oss'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: [{ value: 'list', label: 'list' }, { value: 'create', label: 'create' }, { value: 'delete', label: 'delete' }] },
          { key: 'bucket', label: T('桶名', 'Bucket'), type: 'text' }
        ]
      },
      {
        id: 'object', name: T('对象管理', 'Object'), method: 'POST',
        path: (f) => `/storage/${encodeURIComponent(f.type)}/object`,
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['s3', 'oss'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'upload', 'download', 'delete'] },
          { key: 'bucket', label: T('桶名', 'Bucket'), type: 'text', required: true },
          { key: 'object', label: T('对象键', 'Object Key'), type: 'text' },
          { key: 'local', label: T('本地路径', 'Local Path'), type: 'text' }
        ]
      }
    ]
  },

  // --------------------------------------------------------------------------
  {
    id: 'harbor',
    name: T('镜像仓库 Harbor', 'Harbor'),
    icon: 'deploy',
    actions: [
      {
        id: 'status', name: T('状态查询', 'Status'), method: 'GET', taskType: 'query',
        path: (f) => `/harbor/${encodeURIComponent(f.deviceID)}`,
        fields: [deviceField()]
      },
      {
        id: 'deploy', name: T('部署', 'Deploy'), method: 'POST',
        path: () => '/harbor/deploy',
        fields: [
          deviceField(),
          { key: 'mode', label: T('模式', 'Mode'), type: 'select', options: ['single', 'ha'] },
          { key: 'version', label: T('版本', 'Version'), type: 'text', placeholder: 'v2.8.0' }
        ]
      },
      {
        id: 'lifecycle', name: T('启停/状态', 'Lifecycle'), method: 'POST',
        path: (f) => `/harbor/${encodeURIComponent(f.action)}`,
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: lifecycleActionOptions }
        ]
      },
      {
        id: 'project', name: T('项目管理', 'Project'), method: 'POST',
        path: () => '/harbor/project',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: [{ value: 'list', label: 'list' }, { value: 'create', label: 'create' }, { value: 'delete', label: 'delete' }] },
          { key: 'project', label: T('项目名', 'Project'), type: 'text' }
        ]
      },
      {
        id: 'repository', name: T('仓库/镜像', 'Repository'), method: 'POST',
        path: () => '/harbor/repository',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: [{ value: 'list', label: 'list' }, { value: 'delete', label: 'delete' }] },
          { key: 'project', label: T('项目名', 'Project'), type: 'text', required: true },
          { key: 'repository', label: T('仓库名', 'Repository'), type: 'text' }
        ]
      }
    ]
  },

  // --------------------------------------------------------------------------
  {
    id: 'monitor',
    name: T('监控系统兼容', 'Monitoring'),
    icon: 'alerts',
    actions: [
      {
        id: 'status', name: T('状态查询', 'Status'), method: 'GET', taskType: 'query',
        path: (f) => `/monitor/${encodeURIComponent(f.deviceID)}`,
        fields: [deviceField()]
      },
      {
        id: 'deploy', name: T('部署', 'Deploy'), method: 'POST',
        path: () => '/monitor/deploy',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['zabbix', 'nightingale', 'skywalking'] },
          { key: 'mode', label: T('模式', 'Mode'), type: 'select', options: ['single', 'cluster'] },
          { key: 'version', label: T('版本', 'Version'), type: 'text' }
        ]
      },
      {
        id: 'lifecycle', name: T('启停/状态', 'Lifecycle'), method: 'POST',
        path: (f) => `/monitor/${encodeURIComponent(f.type)}/${encodeURIComponent(f.action)}`,
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['zabbix', 'nightingale', 'skywalking'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: lifecycleActionOptions }
        ]
      },
      {
        id: 'zabbix-host', name: T('Zabbix 主机', 'Zabbix Host'), method: 'POST',
        path: () => '/monitor/zabbix/host',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: [{ value: 'list', label: 'list' }, { value: 'create', label: 'create' }, { value: 'delete', label: 'delete' }] },
          { key: 'host', label: T('主机名', 'Host'), type: 'text' },
          { key: 'hostIP', label: T('主机 IP', 'Host IP'), type: 'text' }
        ]
      },
      {
        id: 'n9e-query', name: T('夜莺查询', 'Nightingale Query'), method: 'POST',
        path: () => '/monitor/nightingale/query',
        fields: [
          deviceField(),
          { key: 'query', label: T('PromQL', 'PromQL'), type: 'text', required: true, placeholder: 'up{job="node"}' },
          { key: 'start', label: T('起始时间戳', 'Start'), type: 'number' },
          { key: 'end', label: T('结束时间戳', 'End'), type: 'number' },
          { key: 'step', label: T('步长(秒)', 'Step'), type: 'number' }
        ]
      },
      {
        id: 'sw-trace', name: T('SkyWalking 链路', 'SkyWalking Trace'), method: 'POST',
        path: () => '/monitor/skywalking/trace',
        fields: [
          deviceField(),
          { key: 'traceID', label: T('链路 ID', 'Trace ID'), type: 'text' },
          { key: 'serviceID', label: T('服务 ID', 'Service ID'), type: 'text' }
        ]
      }
    ]
  },

  // --------------------------------------------------------------------------
  {
    id: 'kernel',
    name: T('内核管理', 'Kernel'),
    icon: 'settings',
    actions: [
      {
        id: 'info', name: T('内核信息', 'Info'), method: 'GET', taskType: 'query',
        path: (f) => `/kernel/${encodeURIComponent(f.deviceID)}`,
        fields: [deviceField()]
      },
      {
        id: 'param', name: T('参数管理', 'Parameters'), method: 'POST',
        path: () => '/kernel/param',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: [{ value: 'query', label: 'query' }, { value: 'set', label: 'set' }] },
          { key: 'name', label: T('参数名', 'Name'), type: 'text', placeholder: 'net.ipv4.ip_forward' },
          { key: 'value', label: T('值(set)', 'Value'), type: 'text', placeholder: '0' }
        ]
      },
      {
        id: 'module', name: T('模块管理', 'Modules'), method: 'POST',
        path: () => '/kernel/module',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: [{ value: 'list', label: 'list' }, { value: 'load', label: 'load' }, { value: 'unload', label: 'unload' }] },
          { key: 'name', label: T('模块名', 'Module'), type: 'text', placeholder: 'br_netfilter' }
        ]
      },
      {
        id: 'upgrade', name: T('内核升级', 'Upgrade'), method: 'POST',
        path: () => '/kernel/upgrade',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: [{ value: 'list', label: 'list' }, { value: 'install', label: 'install' }] },
          { key: 'version', label: T('目标版本', 'Version'), type: 'text', placeholder: '5.15.0-91' }
        ]
      },
      {
        id: 'grub', name: T('GRUB 管理', 'GRUB'), method: 'POST',
        path: () => '/kernel/grub',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: [{ value: 'list', label: 'list' }, { value: 'set-default', label: 'set-default' }] },
          { key: 'index', label: T('条目索引', 'Index'), type: 'number' }
        ]
      }
    ]
  },

  // --------------------------------------------------------------------------
  {
    id: 'security',
    name: T('安全运维', 'Security'),
    icon: 'permissions',
    actions: [
      {
        id: 'info', name: T('安全态势', 'Posture'), method: 'GET', taskType: 'query',
        path: (f) => `/security/${encodeURIComponent(f.deviceID)}`,
        fields: [deviceField()]
      },
      {
        id: 'scan', name: T('漏洞扫描', 'CVE Scan'), method: 'POST',
        path: () => '/security/scan',
        fields: [
          deviceField(),
          { key: 'cve', label: T('CVE ID（可选）', 'CVE ID (optional)'), type: 'text', placeholder: 'CVE-2021-44228' }
        ]
      },
      {
        id: 'baseline', name: T('基线检查/修复', 'Baseline'), method: 'POST',
        path: () => '/security/baseline',
        fields: [
          deviceField(),
          { key: 'fix', label: T('执行修复', 'Apply Fix'), type: 'bool' },
          { key: 'ruleIDs', label: T('规则 ID（逗号分隔，空=全部）', 'Rule IDs'), type: 'text',
            array: true, placeholder: 'cis-ssh-01,cis-kernel-01' }
        ]
      },
      {
        id: 'patch', name: T('组件更新', 'Patch'), method: 'POST',
        path: () => '/security/patch',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: [{ value: 'check', label: 'check' }, { value: 'update', label: 'update' }] },
          { key: 'packages', label: T('包名（逗号分隔）', 'Packages'), type: 'text',
            array: true, placeholder: 'log4j-core,httpd,openssl' },
          { key: 'securityOnly', label: T('仅安全更新', 'Security Only'), type: 'bool' }
        ]
      },
      {
        id: 'rollback', name: T('更新回滚', 'Rollback'), method: 'POST',
        path: () => '/security/rollback',
        fields: [
          deviceField(),
          { key: 'transactionID', label: T('事务 ID', 'Transaction ID'), type: 'text', required: true,
            placeholder: 'yum history 事务号' }
        ]
      }
    ]
  },

  // --------------------------------------------------------------------------
  {
    id: 'virt',
    name: T('虚拟化', 'Virtualization'),
    icon: 'device',
    actions: [
      {
        id: 'status', name: T('状态查询', 'Status'), method: 'GET', taskType: 'query',
        path: (f) => `/virt/${encodeURIComponent(f.deviceID)}`,
        fields: [deviceField()]
      },
      {
        id: 'deploy', name: T('部署/配置', 'Deploy'), method: 'POST',
        path: () => '/virt/deploy',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['kvm', 'vsphere', 'openstack', 'proxmox'] },
          { key: 'mode', label: T('模式', 'Mode'), type: 'select', required: true,
            options: ['host', 'client'] }
        ]
      },
      {
        id: 'lifecycle', name: T('平台启停', 'Lifecycle'), method: 'POST',
        path: (f) => `/virt/${encodeURIComponent(f.type)}/${encodeURIComponent(f.action)}`,
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['kvm', 'vsphere', 'openstack', 'proxmox'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: lifecycleActionOptions }
        ]
      },
      {
        id: 'vm', name: T('虚拟机管理', 'VM'), method: 'POST',
        path: (f) => `/virt/${encodeURIComponent(f.type)}/vm`,
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['kvm', 'vsphere', 'openstack', 'proxmox'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'start', 'stop', 'restart', 'delete'] },
          { key: 'name', label: T('VM 名/ID', 'VM Name/ID'), type: 'text' }
        ]
      },
      {
        id: 'snapshot', name: T('快照管理', 'Snapshot'), method: 'POST',
        path: (f) => `/virt/${encodeURIComponent(f.type)}/snapshot`,
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['kvm', 'proxmox'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'create', 'revert', 'delete'] },
          { key: 'name', label: T('VM 名', 'VM'), type: 'text', required: true },
          { key: 'snapshot', label: T('快照名', 'Snapshot'), type: 'text' }
        ]
      }
    ]
  },

  // --------------------------------------------------------------------------
  {
    id: 'ansible',
    name: T('Ansible 自动化', 'Ansible'),
    icon: 'flow',
    actions: [
      {
        id: 'status', name: T('状态查询', 'Status'), method: 'GET', taskType: 'query',
        path: (f) => `/ansible/${encodeURIComponent(f.deviceID)}`,
        fields: [deviceField()]
      },
      {
        id: 'deploy', name: T('安装 Ansible', 'Install'), method: 'POST',
        path: () => '/ansible/deploy',
        fields: [deviceField()]
      },
      {
        id: 'adhoc', name: T('Ad-hoc 执行', 'Ad-hoc'), method: 'POST',
        path: () => '/ansible/adhoc',
        fields: [
          deviceField(),
          { key: 'pattern', label: T('主机模式', 'Pattern'), type: 'text', placeholder: 'all' },
          { key: 'module', label: T('模块', 'Module'), type: 'select', required: true,
            options: ['ping', 'command', 'shell', 'copy', 'file', 'service', 'systemd', 'yum', 'apt', 'setup', 'user', 'group', 'cron', 'lineinfile', 'get_url', 'mount', 'debug'] },
          { key: 'args', label: T('参数', 'Args'), type: 'text', placeholder: 'cmd=uptime' }
        ]
      },
      {
        id: 'playbook', name: T('Playbook 执行', 'Playbook'), method: 'POST',
        path: () => '/ansible/playbook',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: [{ value: 'run', label: 'run' }, { value: 'check', label: 'check' }, { value: 'syntax', label: 'syntax' }] },
          { key: 'name', label: T('剧本名', 'Name'), type: 'text', required: true },
          { key: 'content', label: T('剧本内容(YAML)', 'Content (YAML)'), type: 'textarea',
            placeholder: '- hosts: all\n  tasks:\n    - name: ping\n      ping:' }
        ]
      },
      {
        id: 'inventory', name: T('Inventory 管理', 'Inventory'), method: 'POST',
        path: () => '/ansible/inventory',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: [{ value: 'list', label: 'list' }, { value: 'sync', label: 'sync' }] },
          { key: 'hosts', label: T('主机清单(INI)', 'Hosts (INI)'), type: 'textarea',
            placeholder: '[web]\n192.168.1.10\n192.168.1.11' }
        ]
      }
    ]
  },

  // --------------------------------------------------------------------------
  {
    id: 'packages',
    name: T('包管理', 'Packages'),
    icon: 'deploy',
    actions: [
      {
        id: 'install', name: T('安装/升级', 'Install/Upgrade'), method: 'POST',
        path: () => '/system/packages',
        fields: [
          deviceField(),
          { key: 'package', label: T('包名', 'Package'), type: 'text', required: true, placeholder: 'nginx' },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: [{ value: 'install', label: 'install' }, { value: 'upgrade', label: 'upgrade' }] },
          { key: 'manager', label: T('包管理器', 'Manager'), type: 'select', options: ['auto', 'yum', 'apt'] }
        ]
      },
      {
        id: 'remove', name: T('卸载', 'Remove'), method: 'POST',
        path: () => '/system/packages',
        fields: [
          deviceField(),
          { key: 'package', label: T('包名', 'Package'), type: 'text', required: true },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: [{ value: 'remove', label: 'remove' }] },
          { key: 'manager', label: T('包管理器', 'Manager'), type: 'select', options: ['auto', 'yum', 'apt'] }
        ]
      }
    ]
  },

  // --------------------------------------------------------------------------
  {
    id: 'env',
    name: T('环境依赖', 'Runtime Envs'),
    icon: 'settings',
    actions: [
      {
        id: 'info', name: T('环境查询', 'Info'), method: 'GET', taskType: 'query',
        path: (f) => `/env/${encodeURIComponent(f.deviceID)}`,
        fields: [deviceField()]
      },
      {
        id: 'install', name: T('安装运行时', 'Install Runtime'), method: 'POST',
        path: () => '/env/install',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['java', 'nodejs', 'python2', 'python3', 'go'] },
          { key: 'version', label: T('版本', 'Version'), type: 'text', required: true, placeholder: '17 / 20.10.0 / 3.11' }
        ]
      },
      {
        id: 'version', name: T('版本查询', 'Version'), method: 'POST',
        path: (f) => `/env/${encodeURIComponent(f.type)}/version`,
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['java', 'nodejs', 'python2', 'python3', 'go'] }
        ]
      },
      {
        id: 'fdlimit', name: T('fd-limit', 'fd-limit'), method: 'POST',
        path: () => '/env/fdlimit',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: [{ value: 'query', label: 'query' }, { value: 'set', label: 'set' }] },
          { key: 'limit', label: T('限制值(set)', 'Limit'), type: 'number', placeholder: '65535' }
        ]
      },
      {
        id: 'ipvs', name: T('IPVS', 'IPVS'), method: 'POST',
        path: () => '/env/ipvs',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: [{ value: 'list', label: 'list' }, { value: 'add', label: 'add' }, { value: 'del', label: 'del' }] },
          { key: 'vip', label: T('VIP', 'VIP'), type: 'text', placeholder: '192.168.1.100' },
          { key: 'port', label: T('端口', 'Port'), type: 'number' },
          { key: 'protocol', label: T('协议', 'Protocol'), type: 'select', options: ['tcp', 'udp'] }
        ]
      }
    ]
  },

  // --------------------------------------------------------------------------
  {
    id: 'k8s-provision',
    name: T('K8s 一键部署', 'K8s Provision'),
    icon: 'device',
    actions: [
      {
        id: 'provision', name: T('部署集群', 'Provision'), method: 'POST',
        path: () => '/k8s/provision',
        fields: [
          deviceField(),
          { key: 'tool', label: T('工具', 'Tool'), type: 'select', required: true,
            options: ['kubeadm', 'kind', 'k3s'] },
          { key: 'mode', label: T('模式', 'Mode'), type: 'select', required: true,
            options: ['single', 'ha', 'multi', 'server-agent'] },
          { key: 'version', label: T('K8s 版本', 'Version'), type: 'text', placeholder: 'v1.29.0' },
          { key: 'clusterName', label: T('集群名', 'Cluster Name'), type: 'text' },
          { key: 'podCIDR', label: T('Pod 网段', 'Pod CIDR'), type: 'text', placeholder: '10.244.0.0/16' },
          { key: 'nodes', label: T('节点数', 'Nodes'), type: 'number' }
        ]
      },
      {
        id: 'lifecycle', name: T('集群启停/删除', 'Lifecycle'), method: 'POST',
        path: () => '/k8s/provision/lifecycle',
        fields: [
          deviceField(),
          { key: 'tool', label: T('工具', 'Tool'), type: 'select', required: true,
            options: ['kubeadm', 'kind', 'k3s'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['start', 'stop', 'delete', 'status'] },
          { key: 'clusterName', label: T('集群名', 'Cluster Name'), type: 'text' }
        ]
      },
      {
        id: 'join', name: T('加入节点', 'Join Node'), method: 'POST',
        path: () => '/k8s/provision/join',
        fields: [
          deviceField(),
          { key: 'tool', label: T('工具', 'Tool'), type: 'select', required: true,
            options: ['kubeadm', 'k3s'] },
          { key: 'role', label: T('角色', 'Role'), type: 'select', required: true,
            options: ['worker', 'controlplane', 'agent'] },
          { key: 'masterAddr', label: T('Master 地址', 'Master Addr'), type: 'text', required: true },
          { key: 'token', label: T('Token', 'Token'), type: 'text', required: true },
          { key: 'certHash', label: T('CA 哈希', 'Cert Hash'), type: 'text', placeholder: 'sha256:...' }
        ]
      },
      {
        id: 'kubeconfig', name: T('导出 kubeconfig', 'Kubeconfig'), method: 'POST',
        path: () => '/k8s/provision/kubeconfig',
        fields: [
          deviceField(),
          { key: 'tool', label: T('工具', 'Tool'), type: 'select', required: true,
            options: ['kubeadm', 'kind', 'k3s'] },
          { key: 'clusterName', label: T('集群名', 'Cluster Name'), type: 'text' }
        ]
      }
    ]
  },

  // --------------------------------------------------------------------------
  {
    id: 'ssh-trust',
    name: T('SSH 互信', 'SSH Trust'),
    icon: 'key',
    actions: [
      {
        id: 'keygen', name: T('生成密钥对', 'Keygen'), method: 'POST',
        path: () => '/ssh-trust/keygen',
        fields: [
          deviceField(),
          { key: 'user', label: T('SSH 用户', 'SSH User'), type: 'text', placeholder: 'root' },
          { key: 'keyType', label: T('密钥类型', 'Key Type'), type: 'select',
            options: ['ed25519', 'rsa', 'ecdsa'] }
        ]
      },
      {
        id: 'authorize', name: T('分发公钥', 'Authorize Key'), method: 'POST',
        path: () => '/ssh-trust/authorize',
        fields: [
          deviceField(),
          { key: 'user', label: T('SSH 用户', 'SSH User'), type: 'text', placeholder: 'root' },
          { key: 'publicKey', label: T('公钥内容', 'Public Key'), type: 'textarea', required: true,
            placeholder: 'ssh-ed25519 AAAA... opsmesh@host' }
        ]
      },
      {
        id: 'revoke', name: T('撤销公钥', 'Revoke Key'), method: 'POST',
        path: () => '/ssh-trust/revoke',
        fields: [
          deviceField(),
          { key: 'user', label: T('SSH 用户', 'SSH User'), type: 'text', placeholder: 'root' },
          { key: 'publicKey', label: T('公钥内容', 'Public Key'), type: 'textarea', required: true }
        ]
      },
      {
        id: 'list', name: T('授权清单', 'List Keys'), method: 'POST',
        path: () => '/ssh-trust/list',
        fields: [
          deviceField(),
          { key: 'user', label: T('SSH 用户', 'SSH User'), type: 'text', placeholder: 'root' }
        ]
      },
      {
        id: 'test', name: T('免密测试', 'Test Trust'), method: 'POST',
        path: () => '/ssh-trust/test',
        fields: [
          deviceField(),
          { key: 'user', label: T('SSH 用户', 'SSH User'), type: 'text', placeholder: 'root' },
          { key: 'host', label: T('目标主机', 'Target Host'), type: 'text', required: true, placeholder: '10.0.0.2' },
          { key: 'port', label: T('端口', 'Port'), type: 'number', placeholder: '22' }
        ]
      }
    ]
  },

  // --------------------------------------------------------------------------
  {
    id: 'ntp',
    name: T('时间同步 NTP', 'NTP'),
    icon: 'task',
    actions: [
      {
        id: 'status', name: T('时间状态', 'Time Status'), method: 'POST',
        path: () => '/ntp/status',
        fields: [deviceField()]
      },
      {
        id: 'sync', name: T('立即校时', 'Force Sync'), method: 'POST',
        path: () => '/ntp/sync',
        fields: [deviceField()]
      },
      {
        id: 'config', name: T('配置时间源', 'Configure Sources'), method: 'POST',
        path: () => '/ntp/config',
        fields: [
          deviceField(),
          { key: 'servers', label: T('时间源(逗号分隔)', 'Servers (comma-separated)'), type: 'text', required: true,
            array: true, placeholder: 'ntp.aliyun.com,n1.internal.local' }
        ]
      },
      {
        id: 'timezone', name: T('设置时区', 'Timezone'), method: 'POST',
        path: () => '/ntp/timezone',
        fields: [
          deviceField(),
          { key: 'timezone', label: T('时区', 'Timezone'), type: 'text', required: true, placeholder: 'Asia/Shanghai' }
        ]
      },
      {
        id: 'deploy', name: T('部署时间服务端', 'Deploy Server'), method: 'POST',
        path: () => '/ntp/deploy',
        fields: [
          deviceField(),
          { key: 'servers', label: T('上游时间源(逗号分隔)', 'Upstream Servers'), type: 'text', required: true,
            array: true, placeholder: 'ntp.aliyun.com' },
          { key: 'allowCIDR', label: T('允许同步网段', 'Allow CIDR'), type: 'text', placeholder: '10.0.0.0/8' }
        ]
      },
      {
        id: 'service', name: T('服务管理', 'Service'), method: 'POST',
        path: () => '/ntp/service',
        fields: [
          deviceField(),
          { key: 'service', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['status', 'start', 'stop', 'restart'] }
        ]
      }
    ]
  },

  // --------------------------------------------------------------------------
  {
    id: 'dns',
    name: T('DNS 服务', 'DNS'),
    icon: 'flow',
    actions: [
      {
        id: 'status', name: T('服务状态', 'Status'), method: 'POST',
        path: () => '/dns/status',
        fields: [
          deviceField(),
          { key: 'zone', label: T('托管 Zone', 'Managed Zone'), type: 'text', placeholder: 'opsmesh.internal' }
        ]
      },
      {
        id: 'deploy', name: T('部署 DNS', 'Deploy'), method: 'POST',
        path: () => '/dns/deploy',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['dnsmasq', 'bind'] },
          { key: 'zone', label: T('托管 Zone', 'Managed Zone'), type: 'text', placeholder: 'opsmesh.internal' },
          { key: 'forwarders', label: T('上游 DNS(逗号分隔)', 'Forwarders'), type: 'text',
            array: true, placeholder: '223.5.5.5,8.8.8.8' }
        ]
      },
      {
        id: 'record', name: T('记录管理', 'Records'), method: 'POST',
        path: () => '/dns/record',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['dnsmasq', 'bind'] },
          { key: 'op', label: T('操作', 'Op'), type: 'select', required: true,
            options: ['list', 'add', 'delete'] },
          { key: 'zone', label: T('Zone', 'Zone'), type: 'text', placeholder: 'opsmesh.internal' },
          { key: 'name', label: T('记录名', 'Name'), type: 'text', placeholder: 'db.internal.local' },
          { key: 'recordType', label: T('记录类型', 'Record Type'), type: 'select',
            options: ['A', 'AAAA', 'CNAME', 'MX', 'TXT'] },
          { key: 'value', label: T('记录值', 'Value'), type: 'text', placeholder: '10.0.0.5' },
          { key: 'ttl', label: T('TTL', 'TTL'), type: 'number', placeholder: '300' }
        ]
      },
      {
        id: 'query', name: T('解析测试', 'Query Test'), method: 'POST',
        path: () => '/dns/query',
        fields: [
          deviceField(),
          { key: 'name', label: T('查询域名', 'Query Name'), type: 'text', required: true, placeholder: 'db.internal.local' },
          { key: 'recordType', label: T('记录类型', 'Record Type'), type: 'select', options: ['A', 'AAAA', 'CNAME', 'MX', 'TXT'] },
          { key: 'server', label: T('DNS 服务器', 'DNS Server'), type: 'text', placeholder: '127.0.0.1' }
        ]
      },
      {
        id: 'service', name: T('服务管理', 'Service'), method: 'POST',
        path: () => '/dns/service',
        fields: [
          deviceField(),
          { key: 'service', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['status', 'start', 'stop', 'restart'] }
        ]
      }
    ]
  },

  // --------------------------------------------------------------------------
  {
    id: 'fileshare',
    name: T('文件共享', 'File Share'),
    icon: 'cmdb',
    actions: [
      {
        id: 'status', name: T('服务状态', 'Status'), method: 'POST',
        path: () => '/fileshare/status',
        fields: [deviceField()]
      },
      {
        id: 'deploy', name: T('部署服务', 'Deploy'), method: 'POST',
        path: () => '/fileshare/deploy',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['nfs', 'samba', 'vsftpd'] },
          { key: 'anonymousOK', label: T('允许匿名(FTP)', 'Anonymous (FTP)'), type: 'bool' }
        ]
      },
      {
        id: 'share', name: T('共享管理', 'Shares'), method: 'POST',
        path: () => '/fileshare/share',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['nfs', 'samba'] },
          { key: 'op', label: T('操作', 'Op'), type: 'select', required: true,
            options: ['list', 'add', 'delete'] },
          { key: 'name', label: T('共享名(Samba)', 'Share Name'), type: 'text' },
          { key: 'path', label: T('共享目录', 'Path'), type: 'text', placeholder: '/data/share' },
          { key: 'options', label: T('选项', 'Options'), type: 'text',
            placeholder: 'rw,sync,no_root_squash （NFS）' }
        ]
      },
      {
        id: 'mount', name: T('NFS 挂载', 'NFS Mount'), method: 'POST',
        path: () => '/fileshare/mount',
        fields: [
          deviceField(),
          { key: 'op', label: T('操作', 'Op'), type: 'select', required: true,
            options: ['list', 'mount', 'umount'] },
          { key: 'server', label: T('服务端', 'Server'), type: 'text', placeholder: '10.0.0.1' },
          { key: 'exportPath', label: T('导出路径', 'Export Path'), type: 'text', placeholder: '/data/share' },
          { key: 'mountPoint', label: T('挂载点', 'Mount Point'), type: 'text', placeholder: '/mnt/share' },
          { key: 'mountOpts', label: T('挂载参数', 'Mount Options'), type: 'text',
            placeholder: 'rw,hard,intr,noatime' }
        ]
      },
      {
        id: 'service', name: T('服务管理', 'Service'), method: 'POST',
        path: () => '/fileshare/service',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['nfs', 'samba', 'vsftpd'] },
          { key: 'service', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['status', 'start', 'stop', 'restart'] }
        ]
      }
    ]
  },
  // --------------------------------------------------------------------------
  {
    id: 'netdiag',
    name: T('网络诊断', 'Network Diagnostics'),
    icon: 'flow',
    actions: [
      {
        id: 'run', name: T('执行', 'Run'), method: 'POST',
        path: () => '/netdiag',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['tcpdump', 'mtr', 'nmap', 'dig', 'host', 'arp', 'route', 'iftop', 'nethogs', 'ss', 'netstat', 'traceroute', 'ping', 'status-code'] },
          { key: 'target', label: T('目标', 'Target'), type: 'text' },
          { key: 'count', label: T('次数', 'Count'), type: 'number' },
          { key: 'port', label: T('端口', 'Port'), type: 'number' },
          { key: 'protocol', label: T('协议', 'Protocol'), type: 'select', options: ['tcp', 'udp', 'icmp'] }
        ]
      }
    ]
  },
  // --------------------------------------------------------------------------
  {
    id: 'abac',
    name: T('ABAC 访问控制', 'ABAC'),
    icon: 'permissions',
    actions: [
      {
        id: 'run', name: T('执行', 'Run'), method: 'POST',
        path: () => '/abac',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['policy-list', 'policy-create', 'policy-delete', 'evaluate'] },
          { key: 'subject', label: T('主体属性(JSON)', 'Subject (JSON)'), type: 'textarea' },
          { key: 'resource', label: T('资源属性(JSON)', 'Resource (JSON)'), type: 'textarea' },
          { key: 'actionAttr', label: T('动作属性', 'Action Attr'), type: 'text' },
          { key: 'context', label: T('环境上下文(JSON)', 'Context (JSON)'), type: 'textarea' }
        ]
      }
    ]
  },
  // --------------------------------------------------------------------------
  {
    id: 'raid-ext',
    name: T('RAID 高级管理', 'RAID Extended'),
    icon: 'cmdb',
    actions: [
      {
        id: 'run', name: T('执行', 'Run'), method: 'POST',
        path: () => '/os/raid-ext',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['status', 'rebuild', 'health', 'add-spare', 'remove-disk', 'mdadm', 'zfs'] },
          { key: 'controller', label: T('控制器', 'Controller'), type: 'select',
            options: ['megacli', 'storcli', 'hpssacli', 'mdadm', 'zfs'] },
          { key: 'target', label: T('目标', 'Target'), type: 'text' }
        ]
      }
    ]
  },
  // --------------------------------------------------------------------------
  {
    id: 'k8s-gateway-api',
    name: T('K8s Gateway API', 'K8s Gateway API'),
    icon: 'device',
    actions: [
      {
        id: 'run', name: T('执行', 'Run'), method: 'POST',
        path: () => '/k8s/gateway-api',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list-gatewayclasses', 'list-gateways', 'list-httproutes', 'list-grpcroutes', 'list-tcproutes', 'status', 'install'] },
          { key: 'name', label: T('名称', 'Name'), type: 'text' },
          { key: 'namespace', label: T('命名空间', 'Namespace'), type: 'text' }
        ]
      }
    ]
  },
  // --------------------------------------------------------------------------
  {
    id: 'microservice',
    name: T('微服务治理', 'Microservice'),
    icon: 'flow',
    actions: [
      {
        id: 'run', name: T('执行', 'Run'), method: 'POST',
        path: () => '/microservice',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['dubbo', 'spring-cloud', 'apollo', 'nacos', 'sentinel', 'seata'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['status', 'services', 'configs', 'routes', 'config', 'refresh', 'namespaces', 'releases', 'transactions', 'rules'] },
          { key: 'service', label: T('服务', 'Service'), type: 'text' }
        ]
      }
    ]
  },
  // --------------------------------------------------------------------------
  {
    id: 'redis-cache-scenario',
    name: T('Redis 缓存场景', 'Redis Cache Scenarios'),
    icon: 'cmdb',
    actions: [
      {
        id: 'run', name: T('执行', 'Run'), method: 'POST',
        path: () => '/cache/redis/scenario',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['detect-penetration', 'detect-avalanche', 'detect-breakdown', 'config-rdb', 'config-aof', 'slice-cluster', 'scale', 'failover'] }
        ]
      }
    ]
  },
  // --------------------------------------------------------------------------
  {
    id: 'db-perf',
    name: T('数据库性能', 'DB Performance'),
    icon: 'cmdb',
    actions: [
      {
        id: 'run', name: T('执行', 'Run'), method: 'POST',
        path: () => '/db/perf',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['mysql', 'postgresql', 'mongodb', 'redis'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['qps', 'tps', 'rt', 'concurrency', 'throughput', 'slow-queries', 'locks', 'connections'] }
        ]
      }
    ]
  },
  // --------------------------------------------------------------------------
  {
    id: 'gateway-mgmt',
    name: T('API 网关管理', 'Gateway Management'),
    icon: 'flow',
    actions: [
      {
        id: 'run', name: T('执行', 'Run'), method: 'POST',
        path: () => '/gateway/mgmt',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['kong', 'apisix', 'nginx'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['status', 'routes', 'services', 'plugins', 'consumers', 'reload', 'upstreams', 'vhosts'] },
          { key: 'name', label: T('名称', 'Name'), type: 'text' }
        ]
      }
    ]
  },

  {
    id: 'ascend-gpu',
    name: T('昇腾 (Ascend) GPU', 'Ascend GPU'),
    icon: 'gpu',
    actions: [
      {
        id: 'ascend-driver', name: T('驱动/CANN 管理', 'Driver/CANN'), method: 'POST',
        path: () => '/gpu/ascend/driver',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['info', 'install', 'upgrade', 'remove'] },
          { key: 'version', label: T('版本', 'Version'), type: 'text', placeholder: '如 8.0.RC2' }
        ]
      },
      {
        id: 'ascend-monitor', name: T('监控/拓扑', 'Monitor/Topology'), method: 'POST',
        path: () => '/gpu/ascend/monitor',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['status', 'metrics', 'topo'] }
        ]
      },
      {
        id: 'ascend-virt', name: T('虚拟化 (vNPU)', 'Virtualization (vNPU)'), method: 'POST',
        path: () => '/gpu/ascend/virt',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['enable', 'disable', 'list', 'create', 'delete'] },
          { key: 'profile', label: T('vNPU Profile', 'vNPU Profile'), type: 'text', placeholder: '如 vir04' }
        ]
      },
      {
        id: 'ascend-power', name: T('电源管理', 'Power Management'), method: 'POST',
        path: () => '/gpu/ascend/power',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['status', 'set-limit'] },
          { key: 'limit', label: T('功率上限(W)', 'Power Limit (W)'), type: 'number' }
        ]
      }
    ]
  }
]

// ============================================================================
// 提交器
// ============================================================================

// 按动作定义提交请求：GET 走 query（直接返回数据），POST/DELETE 走 task（返回任务）。
//
// 取值清洗：
//   - bool 字段仅在 true 时携带（避免 false 污染有默认值的后端字段）
//   - number 字段空值不携带；number 字符串转数字
//   - textarea/数组字段（ruleIDs/packages）在动作定义里声明 split:true 时转数组
export async function submitInfraAction(domain, action, values) {
  const payload = {}
  for (const f of action.fields || []) {
    let v = values[f.key]
    if (f.required && (v === undefined || v === null || v === '')) {
      throw new Error(`${f.label?.zh || f.key} 为必填`)
    }
    if (v === undefined || v === null || v === '') continue
    if (f.type === 'bool') {
      if (v) payload[f.key] = true
      continue
    }
    if (f.type === 'number') {
      const n = Number(v)
      if (!Number.isNaN(n)) payload[f.key] = n
      continue
    }
    // 逗号分隔的数组字段（ruleIDs/packages）
    if (f.array) {
      payload[f.key] = String(v).split(',').map((s) => s.trim()).filter(Boolean)
      continue
    }
    payload[f.key] = v
  }
  // vector 检索的 vector 字段：JSON 数组文本 → 数组
  if (action.id === 'search' && typeof payload.vector === 'string') {
    try {
      payload.vector = JSON.parse(payload.vector)
    } catch {
      throw new Error('向量须为 JSON 数组，如 [0.1, 0.2]')
    }
  }

  const path = action.path(payload)
  if (action.method === 'GET') {
    return { kind: 'query', data: await getJSON(path) }
  }
  if (action.method === 'DELETE') {
    return { kind: 'task', data: await deleteJSONBody(path, payload) }
  }
  return { kind: 'task', data: await postJSON(path, payload) }
}

// 查询任务执行结果（任务下发后轮询用）。
export const getInfraTaskResult = (taskID) => getJSON(`/tasks/${encodeURIComponent(taskID)}/result`)

// 按域拆分后的合并导出（视图与测试只依赖这一个符号，形状不变）。
// 新增能力优先入对应域文件；legacy 核心域（含 kilo 线 netdiag/abac 等追加）保留在本文件 CORE_DOMAINS。
export const INFRA_DOMAINS = [
  ...CORE_DOMAINS,
  ...OS_DOMAINS,
  ...SEARCH_DOMAINS,
  ...MQ_DEEP_DOMAINS,
  ...CACHE_GPU_DOMAINS,
  ...OPS_SUITE_DOMAINS,
  ...GRAPH_DOMESTIC_DOMAINS
]
