// 检索域动作目录（Elasticsearch/Kibana/Logstash/OpenSearch/Filebeat + 时序/文档库/数据管道）
// 由 infra-ops.js 合并导出；条目形状见 ./shared.js 注释。
//
// 契约来源（internal/controlplane，全部 POST，执行类返回 Task → taskType 'task'）：
//   elk_ops.go      — ES 9 端点（deploy/lifecycle/cluster/index/ilm/snapshot/reindex/security/upgrade）
//                     + kibana/logstash/opensearch/filebeat 主端点
//   elk_ext_ops.go  — ES 扩展 8 端点（pipeline/template/alias/datastream/maintenance/cluster-settings/ccr/watcher）
//                     + kibana/logstash/opensearch 的 /ext 端点
//   tsdb_ops.go     — /tsdb/{influxdb,tdengine,victoriametrics}/database（库/桶管理）
//   mongo_ops.go    — /mongo/database、/mongo/collection
//   data_ops.go     — /data/lake、/data/warehouse、/data/pipeline
//   monitor_ops.go  — /monitor/zabbix/item
import { T, deviceField, lifecycleActionOptions } from './shared'

export const SEARCH_DOMAINS = [
  // --------------------------------------------------------------------------
  // Elasticsearch（17 端点）
  {
    id: 'search-es',
    name: T('Elasticsearch', 'Elasticsearch'),
    icon: 'logs',
    actions: [
      {
        id: 'es-deploy', name: T('部署', 'Deploy'), method: 'POST', taskType: 'task',
        path: () => '/search/elasticsearch/deploy',
        fields: [
          deviceField(),
          { key: 'mode', label: T('模式', 'Mode'), type: 'select', options: ['single', 'cluster'] },
          { key: 'version', label: T('版本', 'Version'), type: 'text', placeholder: '8.13.0' },
          { key: 'port', label: T('端口', 'Port'), type: 'number', placeholder: '9200' },
          { key: 'memLimit', label: T('内存上限', 'Memory Limit'), type: 'text', placeholder: '512m' }
        ]
      },
      {
        id: 'es-lifecycle', name: T('启停/状态', 'Lifecycle'), method: 'POST', taskType: 'task',
        path: () => '/search/elasticsearch/lifecycle',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: lifecycleActionOptions }
        ]
      },
      {
        id: 'es-cluster', name: T('集群健康', 'Cluster'), method: 'POST', taskType: 'task',
        path: () => '/search/elasticsearch/cluster',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['health', 'nodes', 'stats', 'shards', 'allocation'] }
        ]
      },
      {
        id: 'es-index', name: T('索引管理', 'Index'), method: 'POST', taskType: 'task',
        path: () => '/search/elasticsearch/index',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'create', 'delete', 'mapping', 'settings', 'aliases'] },
          { key: 'index', label: T('索引', 'Index'), type: 'text',
            placeholder: 'logs-2026.10（create/delete/mapping/settings 必填）' },
          { key: 'body', label: T('请求体(JSON)', 'Body (JSON)'), type: 'textarea',
            placeholder: '{"settings":{"number_of_shards":1}}（create 用）' }
        ]
      },
      {
        id: 'es-ilm', name: T('ILM 策略', 'ILM Policy'), method: 'POST', taskType: 'task',
        path: () => '/search/elasticsearch/ilm',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'get', 'create', 'delete', 'explain'] },
          { key: 'policy', label: T('策略名', 'Policy'), type: 'text',
            placeholder: 'hot-warm-delete（get/create/delete/explain 必填）' },
          { key: 'body', label: T('策略体(JSON)', 'Body (JSON)'), type: 'textarea',
            placeholder: '{"phases":{"hot":{"actions":{}}}}（create 必填）' }
        ]
      },
      {
        id: 'es-template', name: T('索引模板', 'Template'), method: 'POST', taskType: 'task',
        path: () => '/search/elasticsearch/template',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'get', 'create', 'delete'] },
          { key: 'name', label: T('模板名', 'Template Name'), type: 'text',
            placeholder: 'logs-template（get/create/delete 必填）' },
          { key: 'body', label: T('模板体(JSON)', 'Body (JSON)'), type: 'textarea',
            placeholder: '{"index_patterns":["logs-*"]}（create 必填）' }
        ]
      },
      {
        id: 'es-pipeline', name: T('摄取管道', 'Ingest Pipeline'), method: 'POST', taskType: 'task',
        path: () => '/search/elasticsearch/pipeline',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'get', 'create', 'delete', 'simulate'] },
          { key: 'name', label: T('管道名', 'Pipeline Name'), type: 'text',
            placeholder: 'parse-logs（get/create/delete 必填）' },
          { key: 'body', label: T('管道体(JSON)', 'Body (JSON)'), type: 'textarea',
            placeholder: '{"processors":[]}（create/simulate 必填）' }
        ]
      },
      {
        id: 'es-alias', name: T('别名管理', 'Alias'), method: 'POST', taskType: 'task',
        path: () => '/search/elasticsearch/alias',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'add', 'remove'] },
          { key: 'alias', label: T('别名', 'Alias'), type: 'text', placeholder: 'logs-current（add/remove 必填）' },
          { key: 'index', label: T('索引', 'Index'), type: 'text', placeholder: 'logs-2026.10（add/remove 必填）' }
        ]
      },
      {
        id: 'es-datastream', name: T('数据流', 'Data Stream'), method: 'POST', taskType: 'task',
        path: () => '/search/elasticsearch/datastream',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'create', 'delete', 'rollover'] },
          { key: 'name', label: T('数据流名', 'Data Stream Name'), type: 'text',
            placeholder: 'logs-app（create/delete/rollover 必填）' }
        ]
      },
      {
        id: 'es-snapshot', name: T('快照/恢复', 'Snapshot'), method: 'POST', taskType: 'task',
        path: () => '/search/elasticsearch/snapshot',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['create-repo', 'create-snapshot', 'restore', 'list', 'delete'] },
          { key: 'repo', label: T('仓库名', 'Repository'), type: 'text', placeholder: 'backup-repo（除 create-repo 外必填）' },
          { key: 'snapshot', label: T('快照名', 'Snapshot'), type: 'text',
            placeholder: 'snap-20261027（create-snapshot/restore/delete 必填）' },
          { key: 'indices', label: T('索引(默认 *)', 'Indices'), type: 'text', placeholder: 'logs-*（create-snapshot 可选）' },
          { key: 'location', label: T('共享目录', 'Location'), type: 'text', placeholder: '/mnt/backups/es（create-repo 必填）' }
        ]
      },
      {
        id: 'es-reindex', name: T('重建索引', 'Reindex'), method: 'POST', taskType: 'task',
        path: () => '/search/elasticsearch/reindex',
        fields: [
          deviceField(),
          { key: 'source', label: T('源索引', 'Source Index'), type: 'text', required: true, placeholder: 'logs-old' },
          { key: 'dest', label: T('目标索引', 'Dest Index'), type: 'text', required: true, placeholder: 'logs-new' }
        ]
      },
      {
        id: 'es-security', name: T('用户/角色', 'Security'), method: 'POST', taskType: 'task',
        path: () => '/search/elasticsearch/security',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list-users', 'create-user', 'delete-user', 'list-roles', 'create-role'] },
          { key: 'user', label: T('用户名', 'User'), type: 'text', placeholder: 'ops（create-user/delete-user 必填）' },
          { key: 'password', label: T('密码', 'Password'), type: 'text', placeholder: '（create-user 必填）' },
          { key: 'role', label: T('角色名', 'Role'), type: 'text', placeholder: 'ops-role（create-role 必填）' },
          { key: 'roles', label: T('角色列表(JSON)', 'Roles (JSON)'), type: 'text',
            placeholder: '["ops-role"]（create-user 可选，默认 []）' }
        ]
      },
      {
        id: 'es-cluster-settings', name: T('集群设置', 'Cluster Settings'), method: 'POST', taskType: 'task',
        path: () => '/search/elasticsearch/cluster-settings',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['get', 'update'] },
          { key: 'body', label: T('设置体(JSON)', 'Body (JSON)'), type: 'textarea',
            placeholder: '{"persistent":{"cluster.routing.allocation.enable":"all"}}（update 必填）' }
        ]
      },
      {
        id: 'es-ccr', name: T('跨集群复制', 'CCR'), method: 'POST', taskType: 'task',
        path: () => '/search/elasticsearch/ccr',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['create-follower', 'list', 'pause', 'resume'] },
          { key: 'index', label: T('跟随索引', 'Follower Index'), type: 'text',
            placeholder: 'logs-follower（create-follower/pause/resume 必填）' },
          { key: 'leader', label: T('Leader 索引', 'Leader Index'), type: 'text',
            placeholder: 'logs-leader（create-follower 必填）' },
          { key: 'remote', label: T('远端集群', 'Remote Cluster'), type: 'text', placeholder: 'cluster-a（create-follower 用）' }
        ]
      },
      {
        id: 'es-maintenance', name: T('索引维护', 'Maintenance'), method: 'POST', taskType: 'task',
        path: () => '/search/elasticsearch/maintenance',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['force-merge', 'clear-cache', 'flush', 'refresh', 'shrink', 'split'] },
          { key: 'index', label: T('索引', 'Index'), type: 'text', placeholder: '_all（shrink/split 必填）' },
          { key: 'maxSegments', label: T('目标段数', 'Max Segments'), type: 'number', placeholder: '1（force-merge 用）' }
        ]
      },
      {
        id: 'es-watcher', name: T('Watcher 告警', 'Watcher'), method: 'POST', taskType: 'task',
        path: () => '/search/elasticsearch/watcher',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'get', 'create', 'delete', 'activate', 'deactivate'] },
          { key: 'watchId', label: T('Watch ID', 'Watch ID'), type: 'text',
            placeholder: 'disk-usage-watch（除 list 外必填）' },
          { key: 'body', label: T('Watch 体(JSON)', 'Body (JSON)'), type: 'textarea',
            placeholder: '{"trigger":{"schedule":{"interval":"1m"}}}（create 必填）' }
        ]
      },
      {
        id: 'es-upgrade', name: T('版本升级', 'Upgrade'), method: 'POST', taskType: 'task',
        path: () => '/search/elasticsearch/upgrade',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['plan', 'upgrade', 'rollback', 'version'] },
          { key: 'targetVersion', label: T('目标版本', 'Target Version'), type: 'text',
            placeholder: '8.13.0（upgrade 用）' }
        ]
      }
    ]
  },

  // --------------------------------------------------------------------------
  // Kibana / Logstash / OpenSearch / Filebeat（7 端点，含 3 个 /ext）
  {
    id: 'search-stack', name: T('日志栈组件', 'Log Stack'), icon: 'flow',
    actions: [
      {
        id: 'kibana', name: T('Kibana 管理', 'Kibana'), method: 'POST', taskType: 'task',
        path: () => '/search/kibana',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['deploy', 'status', 'spaces', 'dashboards', 'saved-objects'] },
          { key: 'version', label: T('版本', 'Version'), type: 'text', placeholder: '8.13.0（deploy 用）' },
          { key: 'esUrl', label: T('ES 地址', 'ES URL'), type: 'text', placeholder: 'http://localhost:9200（deploy 用）' }
        ]
      },
      {
        id: 'kibana-ext', name: T('Kibana 扩展', 'Kibana Ext'), method: 'POST', taskType: 'task',
        path: () => '/search/kibana/ext',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['status', 'plugins', 'alerting', 'cases', 'connectors', 'actions', 'settings'] }
        ]
      },
      {
        id: 'logstash', name: T('Logstash 管理', 'Logstash'), method: 'POST', taskType: 'task',
        path: () => '/search/logstash',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['deploy', 'pipeline', 'status', 'reload'] },
          { key: 'version', label: T('版本', 'Version'), type: 'text', placeholder: '8.13.0（deploy 用）' },
          { key: 'name', label: T('管道名', 'Pipeline Name'), type: 'text', placeholder: 'app-logs（pipeline 必填）' },
          { key: 'config', label: T('管道配置', 'Pipeline Config'), type: 'textarea',
            placeholder: 'input { beats { port => 5044 } }\noutput { elasticsearch { hosts => ["localhost:9200"] } }（pipeline 用，空取默认）' }
        ]
      },
      {
        id: 'logstash-ext', name: T('Logstash 扩展', 'Logstash Ext'), method: 'POST', taskType: 'task',
        path: () => '/search/logstash/ext',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['node-stats', 'pipeline-stats', 'plugins', 'validate'] }
        ]
      },
      {
        id: 'opensearch', name: T('OpenSearch 管理', 'OpenSearch'), method: 'POST', taskType: 'task',
        path: () => '/search/opensearch',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['deploy', 'status', 'index'] },
          { key: 'version', label: T('版本', 'Version'), type: 'text', placeholder: '2.13.0（deploy 用）' },
          { key: 'index', label: T('索引', 'Index'), type: 'text', placeholder: 'logs-2026.10（index 必填）' }
        ]
      },
      {
        id: 'opensearch-ext', name: T('OpenSearch 扩展', 'OpenSearch Ext'), method: 'POST', taskType: 'task',
        path: () => '/search/opensearch/ext',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['ilm', 'snapshot', 'security', 'upgrade'] }
        ]
      },
      {
        id: 'filebeat', name: T('Filebeat 采集', 'Filebeat'), method: 'POST', taskType: 'task',
        path: () => '/search/filebeat',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['deploy', 'status', 'modules', 'config'] },
          { key: 'version', label: T('版本', 'Version'), type: 'text', placeholder: '8.13.0（deploy 用）' }
        ]
      }
    ]
  },

  // --------------------------------------------------------------------------
  // 时序库/文档库/数据管道/监控子端点（9 端点）
  {
    id: 'tsdb-doc', name: T('时序/文档/数据管道', 'TSDB / NoSQL / Data'), icon: 'cmdb',
    actions: [
      {
        id: 'tsdb-influxdb-db', name: T('InfluxDB 桶管理', 'InfluxDB Bucket'), method: 'POST', taskType: 'task',
        path: () => '/tsdb/influxdb/database',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['create', 'delete', 'list'] },
          { key: 'name', label: T('桶名', 'Bucket'), type: 'text', placeholder: 'metrics（create/delete 必填，[a-zA-Z0-9_-] 1-64 位）' }
        ]
      },
      {
        id: 'tsdb-tdengine-db', name: T('TDengine 数据库', 'TDengine Database'), method: 'POST', taskType: 'task',
        path: () => '/tsdb/tdengine/database',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['create', 'delete', 'list'] },
          { key: 'name', label: T('数据库名', 'Database'), type: 'text', placeholder: 'monitor（create/delete 必填，[a-zA-Z0-9_-] 1-64 位）' }
        ]
      },
      {
        id: 'tsdb-vm-db', name: T('VictoriaMetrics 库', 'VictoriaMetrics Database'), method: 'POST', taskType: 'task',
        path: () => '/tsdb/victoriametrics/database',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['create', 'delete', 'list'] },
          { key: 'name', label: T('库名(VM 单一存储)', 'Name (VM single store)'), type: 'text',
            placeholder: 'VM 无库概念，list 时列出指标名' }
        ]
      },
      {
        id: 'mongo-db', name: T('MongoDB 数据库', 'MongoDB Database'), method: 'POST', taskType: 'task',
        path: () => '/mongo/database',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['create', 'delete', 'list'] },
          { key: 'database', label: T('数据库', 'Database'), type: 'text', placeholder: 'appdb（create/delete 必填）' }
        ]
      },
      {
        id: 'mongo-collection', name: T('MongoDB 集合', 'MongoDB Collection'), method: 'POST', taskType: 'task',
        path: () => '/mongo/collection',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['create', 'delete', 'list'] },
          { key: 'database', label: T('数据库', 'Database'), type: 'text', required: true, placeholder: 'appdb' },
          { key: 'collection', label: T('集合', 'Collection'), type: 'text', placeholder: 'events（create/delete 必填）' }
        ]
      },
      {
        id: 'data-pipeline', name: T('数据管道', 'Data Pipeline'), method: 'POST', taskType: 'task',
        path: () => '/data/pipeline',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['deploy-airflow', 'deploy-spark', 'deploy-flink', 'deploy-dbt', 'deploy-debezium', 'status'] },
          { key: 'version', label: T('版本', 'Version'), type: 'text', placeholder: '2.8.0 / 3.5.0 / 1.18.0（deploy 用）' }
        ]
      },
      {
        id: 'data-lake', name: T('数据湖', 'Data Lake'), method: 'POST', taskType: 'task',
        path: () => '/data/lake',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['deploy-hive', 'deploy-iceberg', 'deploy-delta', 'status'] },
          { key: 'version', label: T('版本', 'Version'), type: 'text', placeholder: '3.1.3（deploy-hive 用）' }
        ]
      },
      {
        id: 'data-warehouse', name: T('数据仓库', 'Data Warehouse'), method: 'POST', taskType: 'task',
        path: () => '/data/warehouse',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['deploy-starrocks', 'deploy-doris', 'deploy-clickhouse', 'status'] },
          { key: 'version', label: T('版本', 'Version'), type: 'text', placeholder: '3.2.0 / 2.1.0（deploy 用）' }
        ]
      },
      {
        id: 'zabbix-item', name: T('Zabbix 监控项', 'Zabbix Item'), method: 'POST', taskType: 'task',
        path: () => '/monitor/zabbix/item',
        fields: [
          deviceField(),
          { key: 'host', label: T('主机名', 'Host'), type: 'text', placeholder: 'web-01（可选）' },
          { key: 'search', label: T('监控项关键字', 'Search'), type: 'text', placeholder: 'cpu（默认取主机名）' }
        ]
      }
    ]
  }
]
