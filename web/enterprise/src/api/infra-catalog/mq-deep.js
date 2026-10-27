// 消息队列深度动作目录（Kafka/Pulsar/RabbitMQ/RocketMQ/ActiveMQ 的 HA/扩缩容/升级/备份/调优/安全/可观测）
// 由 infra-ops.js 合并导出；条目形状见 ./shared.js 注释。
//
// 契约来源（internal/controlplane/*.go，2026-10-27 逐条核对）：
//   kafka_ops.go / kafka_ops_ext.go / kafka_advanced_ops.go / kafka_final_ops.go / kafka_scale_ops.go
//   pulsar_ops.go / mq_ext_ops.go / rabbitmq_ops.go / rabbitmq_ext_ops.go
//   rocketmq_ops.go / rocketmq_ext_ops.go / activemq_ops.go / activemq_ext_ops.go
//   kafka_middleware_ops.go / activemq_middleware_ops.go / redis_middleware_ops.go
// 全部为 POST + 异步任务（返回 taskID → GET /tasks/{id}/result 取输出），taskType 统一 'task'。
import { T, deviceField } from './shared'

export const MQ_DEEP_DOMAINS = [
  // ==========================================================================
  // Kafka（27 条 /middleware/kafka/*）
  // ==========================================================================
  {
    id: 'mq-kafka',
    name: T('消息队列 · Kafka', 'MQ · Kafka'),
    icon: 'deploy',
    actions: [
      {
        id: 'kafka-broker', name: T('Broker/集群', 'Broker/Cluster'), method: 'POST', taskType: 'task',
        path: () => '/middleware/kafka/broker',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['describe-cluster', 'list-brokers', 'under-replicated', 'log-dirs'] }
        ]
      },
      {
        id: 'kafka-acl', name: T('ACL 管理', 'ACL'), method: 'POST', taskType: 'task',
        path: () => '/middleware/kafka/acl',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'create', 'delete'] },
          { key: 'principal', label: T('Principal', 'Principal'), type: 'text',
            placeholder: 'User:alice' },
          { key: 'resource', label: T('资源类型', 'Resource'), type: 'select',
            options: ['Topic', 'Group', 'Cluster'] },
          { key: 'name', label: T('资源名', 'Resource Name'), type: 'text', placeholder: 'my-topic 或 *' },
          { key: 'operation', label: T('操作权限', 'Operation'), type: 'select',
            options: ['Read', 'Write', 'Create', 'Delete', 'Alter', 'Describe', 'All'] },
          { key: 'permission', label: T('许可', 'Permission'), type: 'select',
            options: ['Allow', 'Deny'] }
        ]
      },
      {
        id: 'kafka-config', name: T('Topic/Broker 配置', 'Config Alter'), method: 'POST', taskType: 'task',
        path: () => '/middleware/kafka/config',
        fields: [
          deviceField(),
          { key: 'entityType', label: T('实体类型', 'Entity Type'), type: 'select',
            options: ['topics', 'brokers'] },
          { key: 'entityName', label: T('实体名', 'Entity Name'), type: 'text',
            placeholder: 'topic 名 或 broker id' },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['describe', 'alter', 'delete'] },
          { key: 'config', label: T('配置项', 'Config'), type: 'text',
            placeholder: 'key=value,key=value' }
        ]
      },
      {
        id: 'kafka-connect', name: T('Kafka Connect', 'Kafka Connect'), method: 'POST', taskType: 'task',
        path: () => '/middleware/kafka/connect',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'status', 'create', 'delete', 'restart', 'pause', 'resume'] },
          { key: 'connector', label: T('Connector 名', 'Connector'), type: 'text' },
          { key: 'config', label: T('Connector 配置(JSON)', 'Connector Config (JSON)'), type: 'textarea',
            placeholder: '{"name":"...","config":{...}}' }
        ]
      },
      {
        id: 'kafka-delegation-token', name: T('委派令牌', 'Delegation Token'), method: 'POST', taskType: 'task',
        path: () => '/middleware/kafka/delegation-token',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['create', 'describe', 'expire', 'renew'] },
          { key: 'principal', label: T('Renewer Principal', 'Renewer Principal'), type: 'text',
            placeholder: 'User:alice' },
          { key: 'tokenId', label: T('Token ID', 'Token ID'), type: 'text' },
          { key: 'expiry', label: T('有效期(ms)', 'Expiry (ms)'), type: 'number' }
        ]
      },
      {
        id: 'kafka-delete-records', name: T('按位点删记录', 'Delete Records'), method: 'POST', taskType: 'task',
        path: () => '/middleware/kafka/delete-records',
        fields: [
          deviceField(),
          { key: 'topic', label: T('Topic', 'Topic'), type: 'text', required: true },
          { key: 'partition', label: T('分区', 'Partition'), type: 'number', required: true },
          { key: 'offset', label: T('删除至位点', 'Offset'), type: 'number', required: true }
        ]
      },
      {
        id: 'kafka-eos', name: T('精确一次(EOS)', 'Exactly-Once (EOS)'), method: 'POST', taskType: 'task',
        path: () => '/middleware/kafka/eos',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['enable', 'disable', 'status', 'configure'] }
        ]
      },
      {
        id: 'kafka-health-check', name: T('集群健康检查', 'Health Check'), method: 'POST', taskType: 'task',
        path: () => '/middleware/kafka/health-check',
        fields: [deviceField()]
      },
      {
        id: 'kafka-isr', name: T('ISR/分区状态', 'ISR Check'), method: 'POST', taskType: 'task',
        path: () => '/middleware/kafka/isr',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['check', 'under-replicated', 'offline-partitions'] }
        ]
      },
      {
        id: 'kafka-keda', name: T('KEDA 自动扩缩', 'KEDA Autoscale'), method: 'POST', taskType: 'task',
        path: () => '/middleware/kafka/keda',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['create-scaledobject', 'list', 'delete'] },
          { key: 'consumerGroup', label: T('消费组', 'Consumer Group'), type: 'text' },
          { key: 'topic', label: T('Topic', 'Topic'), type: 'text' },
          { key: 'minReplicas', label: T('最小副本', 'Min Replicas'), type: 'number' },
          { key: 'maxReplicas', label: T('最大副本', 'Max Replicas'), type: 'number' },
          { key: 'lagThreshold', label: T('Lag 阈值', 'Lag Threshold'), type: 'number' }
        ]
      },
      {
        id: 'kafka-kraft', name: T('KRaft 模式', 'KRaft'), method: 'POST', taskType: 'task',
        path: () => '/middleware/kafka/kraft',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['status', 'controller', 'metadata-quorum'] }
        ]
      },
      {
        id: 'kafka-kraft-migration', name: T('ZK→KRaft 迁移', 'KRaft Migration'), method: 'POST', taskType: 'task',
        path: () => '/middleware/kafka/kraft-migration',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['check', 'status', 'migrate', 'verify'] },
          { key: 'zkConnect', label: T('Zookeeper 地址', 'ZK Connect'), type: 'text',
            placeholder: 'localhost:2181' }
        ]
      },
      {
        id: 'kafka-log', name: T('日志管理', 'Log Segments'), method: 'POST', taskType: 'task',
        path: () => '/middleware/kafka/log',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list-segments', 'clean', 'dump', 'compact', 'retention'] },
          { key: 'topic', label: T('Topic', 'Topic'), type: 'text' },
          { key: 'partition', label: T('分区', 'Partition'), type: 'number' },
          { key: 'offset', label: T('位点', 'Offset'), type: 'number' }
        ]
      },
      {
        id: 'kafka-mirror-maker', name: T('MirrorMaker', 'MirrorMaker'), method: 'POST', taskType: 'task',
        path: () => '/middleware/kafka/mirror-maker',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['start', 'stop', 'status', 'list-configs'] },
          { key: 'sourceCluster', label: T('源集群', 'Source Bootstrap'), type: 'text',
            placeholder: 'src:9092' },
          { key: 'targetCluster', label: T('目标集群', 'Target Bootstrap'), type: 'text',
            placeholder: 'dst:9092' },
          { key: 'topics', label: T('Topics', 'Topics'), type: 'text', placeholder: 'a,b（逗号分隔）' },
          { key: 'config', label: T('MM2 配置(properties)', 'MM2 Config (properties)'), type: 'textarea' }
        ]
      },
      {
        id: 'kafka-mirror-maker-full', name: T('集群链接(MM2)', 'Cluster Links (MM2)'), method: 'POST', taskType: 'task',
        path: () => '/middleware/kafka/mirror-maker-full',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['create-link', 'list-links', 'describe-link', 'delete-link', 'lag'] },
          { key: 'source', label: T('源集群', 'Source'), type: 'text' },
          { key: 'target', label: T('目标集群', 'Target'), type: 'text' },
          { key: 'linkName', label: T('链接名', 'Link Name'), type: 'text' },
          { key: 'topics', label: T('Topics', 'Topics'), type: 'text', placeholder: 'a,b（逗号分隔）' }
        ]
      },
      {
        id: 'kafka-otel', name: T('OpenTelemetry', 'OpenTelemetry'), method: 'POST', taskType: 'task',
        path: () => '/middleware/kafka/otel',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['enable', 'disable', 'status', 'configure'] },
          { key: 'endpoint', label: T('OTLP 端点', 'OTLP Endpoint'), type: 'text',
            placeholder: 'http://localhost:4317' }
        ]
      },
      {
        id: 'kafka-quota', name: T('配额管理', 'Client Quota'), method: 'POST', taskType: 'task',
        path: () => '/middleware/kafka/quota',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'set', 'delete'] },
          { key: 'entity', label: T('实体类型', 'Entity Type'), type: 'select',
            options: ['users', 'clients'] },
          { key: 'entityName', label: T('实体名', 'Entity Name'), type: 'text' },
          { key: 'producerRate', label: T('生产速率(B/s)', 'Producer Rate (B/s)'), type: 'number' },
          { key: 'consumerRate', label: T('消费速率(B/s)', 'Consumer Rate (B/s)'), type: 'number' }
        ]
      },
      {
        id: 'kafka-rebalance', name: T('分区重平衡', 'Rebalance'), method: 'POST', taskType: 'task',
        path: () => '/middleware/kafka/rebalance',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['preferred-leader-election', 'reassign-partitions', 'execute-reassignment', 'verify-reassignment', 'cancel-reassignment'] },
          { key: 'topics', label: T('Topics', 'Topics'), type: 'text', placeholder: 'a,b（空=全部）' },
          { key: 'throttle', label: T('限流(B/s)', 'Throttle (B/s)'), type: 'number' },
          { key: 'timeout', label: T('超时(秒)', 'Timeout (s)'), type: 'number' }
        ]
      },
      {
        id: 'kafka-scale', name: T('Broker 扩缩容', 'Broker Scale'), method: 'POST', taskType: 'task',
        path: () => '/middleware/kafka/scale',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list-brokers', 'add-broker', 'remove-broker', 'decommission'] },
          { key: 'brokerId', label: T('Broker ID', 'Broker ID'), type: 'text' },
          { key: 'host', label: T('主机', 'Host'), type: 'text' },
          { key: 'port', label: T('端口', 'Port'), type: 'number', placeholder: '9092' }
        ]
      },
      {
        id: 'kafka-schema-registry', name: T('Schema Registry', 'Schema Registry'), method: 'POST', taskType: 'task',
        path: () => '/middleware/kafka/schema-registry',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list-subjects', 'get-schema', 'register', 'check-compat', 'list-versions'] },
          { key: 'subject', label: T('Subject', 'Subject'), type: 'text' },
          { key: 'schema', label: T('Schema(JSON)', 'Schema (JSON)'), type: 'textarea',
            placeholder: '{"type":"string"}' }
        ]
      },
      {
        id: 'kafka-share-group', name: T('Share Group', 'Share Group'), method: 'POST', taskType: 'task',
        path: () => '/middleware/kafka/share-group',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'describe', 'create', 'delete'] },
          { key: 'group', label: T('共享组', 'Group'), type: 'text' },
          { key: 'topic', label: T('Topic', 'Topic'), type: 'text' }
        ]
      },
      {
        id: 'kafka-streams', name: T('Streams 管理', 'Streams'), method: 'POST', taskType: 'task',
        path: () => '/middleware/kafka/streams',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list-apps', 'reset', 'describe'] },
          { key: 'appId', label: T('应用 ID', 'App ID'), type: 'text' },
          { key: 'bootstrap', label: T('Bootstrap', 'Bootstrap'), type: 'text', placeholder: 'localhost:9092' },
          { key: 'inputTopic', label: T('输入 Topic', 'Input Topics'), type: 'text' },
          { key: 'outputTopic', label: T('输出 Topic', 'Output Topics'), type: 'text' }
        ]
      },
      {
        id: 'kafka-streams-full', name: T('Streams 完整', 'Streams Full'), method: 'POST', taskType: 'task',
        path: () => '/middleware/kafka/streams-full',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list-apps', 'describe', 'reset', 'topology'] },
          { key: 'appId', label: T('应用 ID', 'App ID'), type: 'text' },
          { key: 'bootstrap', label: T('Bootstrap', 'Bootstrap'), type: 'text', placeholder: 'localhost:9092' },
          { key: 'inputTopic', label: T('输入 Topic', 'Input Topics'), type: 'text' },
          { key: 'outputTopic', label: T('输出 Topic', 'Output Topics'), type: 'text' },
          { key: 'toEarliest', label: T('重置到最早位点', 'Reset to Earliest'), type: 'bool' }
        ]
      },
      {
        id: 'kafka-tiered-storage', name: T('分层存储', 'Tiered Storage'), method: 'POST', taskType: 'task',
        path: () => '/middleware/kafka/tiered-storage',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['enable', 'configure', 'status', 'disable'] },
          { key: 'remoteStorage', label: T('远端存储', 'Remote Storage'), type: 'select',
            options: ['s3', 'gcs', 'azure'] },
          { key: 'bucket', label: T('Bucket', 'Bucket'), type: 'text' },
          { key: 'region', label: T('区域', 'Region'), type: 'text', placeholder: 'us-east-1' },
          { key: 'credentials', label: T('凭据(JSON)', 'Credentials (JSON)'), type: 'textarea' }
        ]
      },
      {
        id: 'kafka-transaction', name: T('事务管理', 'Transactions'), method: 'POST', taskType: 'task',
        path: () => '/middleware/kafka/transaction',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'describe', 'abort'] },
          { key: 'txnId', label: T('事务 ID', 'Transactional ID'), type: 'text' }
        ]
      },
      {
        id: 'kafka-tuning', name: T('性能调优', 'Tuning'), method: 'POST', taskType: 'task',
        path: () => '/middleware/kafka/tuning',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['analyze', 'recommend', 'apply'] },
          { key: 'target', label: T('调优对象', 'Target'), type: 'select',
            options: ['broker', 'topic', 'producer', 'consumer'] }
        ]
      },
      {
        id: 'kafka-upgrade', name: T('版本升级', 'Upgrade'), method: 'POST', taskType: 'task',
        path: () => '/middleware/kafka/upgrade',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['plan', 'upgrade', 'rollback', 'version'] },
          { key: 'targetVersion', label: T('目标版本', 'Target Version'), type: 'text', placeholder: '3.7.0' },
          { key: 'nodes', label: T('节点列表', 'Nodes'), type: 'text', placeholder: 'a,b（逗号分隔）' }
        ]
      }
    ]
  },

  // ==========================================================================
  // Pulsar（14 条 /middleware/pulsar/*）
  // ==========================================================================
  {
    id: 'mq-pulsar',
    name: T('消息队列 · Pulsar', 'MQ · Pulsar'),
    icon: 'deploy',
    actions: [
      {
        id: 'pulsar-deploy', name: T('部署', 'Deploy'), method: 'POST', taskType: 'task',
        path: () => '/middleware/pulsar/deploy',
        fields: [
          deviceField(),
          { key: 'mode', label: T('模式', 'Mode'), type: 'select', options: ['single', 'cluster'] },
          { key: 'version', label: T('版本', 'Version'), type: 'text', placeholder: '3.2.0' }
        ]
      },
      {
        id: 'pulsar-lifecycle', name: T('启停/状态', 'Lifecycle'), method: 'POST', taskType: 'task',
        path: () => '/middleware/pulsar/lifecycle',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['start', 'stop', 'status'] }
        ]
      },
      {
        id: 'pulsar-topic', name: T('Topic 管理', 'Topic'), method: 'POST', taskType: 'task',
        path: () => '/middleware/pulsar/topic',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'create', 'delete'] },
          { key: 'topic', label: T('Topic', 'Topic'), type: 'text' },
          { key: 'tenant', label: T('Tenant', 'Tenant'), type: 'text', placeholder: 'public' },
          { key: 'namespace', label: T('Namespace', 'Namespace'), type: 'text', placeholder: 'default' }
        ]
      },
      {
        id: 'pulsar-subscription', name: T('订阅管理', 'Subscription'), method: 'POST', taskType: 'task',
        path: () => '/middleware/pulsar/subscription',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'create', 'delete'] },
          { key: 'topic', label: T('Topic', 'Topic'), type: 'text' },
          { key: 'subName', label: T('订阅名', 'Subscription'), type: 'text' },
          { key: 'subType', label: T('订阅类型', 'Sub Type'), type: 'select',
            options: ['Exclusive', 'Shared', 'Failover', 'Key_Shared'] }
        ]
      },
      {
        id: 'pulsar-tenant', name: T('Tenant 管理', 'Tenant'), method: 'POST', taskType: 'task',
        path: () => '/middleware/pulsar/tenant',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'create', 'delete'] },
          { key: 'tenant', label: T('Tenant', 'Tenant'), type: 'text' },
          { key: 'clusters', label: T('允许集群', 'Allowed Clusters'), type: 'text', placeholder: 'standalone' }
        ]
      },
      {
        id: 'pulsar-namespace', name: T('Namespace 管理', 'Namespace'), method: 'POST', taskType: 'task',
        path: () => '/middleware/pulsar/namespace',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'create', 'delete'] },
          { key: 'tenant', label: T('Tenant', 'Tenant'), type: 'text', placeholder: 'public' },
          { key: 'namespace', label: T('Namespace', 'Namespace'), type: 'text' }
        ]
      },
      {
        id: 'pulsar-function', name: T('Function 管理', 'Function'), method: 'POST', taskType: 'task',
        path: () => '/middleware/pulsar/function',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'create', 'delete', 'status'] },
          { key: 'tenant', label: T('Tenant', 'Tenant'), type: 'text', placeholder: 'public' },
          { key: 'namespace', label: T('Namespace', 'Namespace'), type: 'text', placeholder: 'default' },
          { key: 'name', label: T('函数名', 'Name'), type: 'text' },
          { key: 'className', label: T('类名', 'Class Name'), type: 'text' },
          { key: 'inputs', label: T('输入 Topics', 'Inputs'), type: 'text' },
          { key: 'outputs', label: T('输出 Topic', 'Output'), type: 'text' }
        ]
      },
      {
        id: 'pulsar-monitor', name: T('监控', 'Monitor'), method: 'POST', taskType: 'task',
        path: () => '/middleware/pulsar/monitor',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['status', 'metrics', 'topics'] }
        ]
      },
      {
        id: 'pulsar-ha', name: T('HA/BookKeeper', 'HA/BookKeeper'), method: 'POST', taskType: 'task',
        path: () => '/middleware/pulsar/ha',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['setup-bookkeeper', 'status'] },
          { key: 'nodes', label: T('节点列表', 'Nodes'), type: 'text', placeholder: 'a,b（逗号分隔）' }
        ]
      },
      {
        id: 'pulsar-scale', name: T('Broker 扩缩容', 'Broker Scale'), method: 'POST', taskType: 'task',
        path: () => '/middleware/pulsar/scale',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['add-broker', 'remove-broker', 'list-brokers'] },
          { key: 'broker', label: T('Broker', 'Broker'), type: 'text' }
        ]
      },
      {
        id: 'pulsar-upgrade', name: T('版本升级', 'Upgrade'), method: 'POST', taskType: 'task',
        path: () => '/middleware/pulsar/upgrade',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['plan', 'upgrade', 'rollback', 'version'] },
          { key: 'targetVersion', label: T('目标版本', 'Target Version'), type: 'text', placeholder: '3.2.0' }
        ]
      },
      {
        id: 'pulsar-schema-registry', name: T('Schema 管理', 'Schema Registry'), method: 'POST', taskType: 'task',
        path: () => '/middleware/pulsar/schema-registry',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['get', 'list'] },
          { key: 'tenant', label: T('Tenant', 'Tenant'), type: 'text', placeholder: 'public' },
          { key: 'namespace', label: T('Namespace', 'Namespace'), type: 'text', placeholder: 'default' },
          { key: 'topic', label: T('Topic', 'Topic'), type: 'text' }
        ]
      },
      {
        id: 'pulsar-connector', name: T('Connector 管理', 'Connector'), method: 'POST', taskType: 'task',
        path: () => '/middleware/pulsar/connector',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'create', 'delete'] },
          { key: 'tenant', label: T('Tenant', 'Tenant'), type: 'text', placeholder: 'public' },
          { key: 'namespace', label: T('Namespace', 'Namespace'), type: 'text', placeholder: 'default' },
          { key: 'name', label: T('名称', 'Name'), type: 'text' },
          { key: 'className', label: T('类名', 'Class Name'), type: 'text' },
          { key: 'config', label: T('Source 配置', 'Source Config'), type: 'textarea' }
        ]
      },
      {
        id: 'pulsar-tiered-storage', name: T('分层存储', 'Tiered Storage'), method: 'POST', taskType: 'task',
        path: () => '/middleware/pulsar/tiered-storage',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['enable', 'disable', 'status'] },
          { key: 'storage', label: T('存储类型', 'Storage'), type: 'select', options: ['s3', 'gcs', 'azure'] },
          { key: 'bucket', label: T('Bucket', 'Bucket'), type: 'text' }
        ]
      }
    ]
  },

  // ==========================================================================
  // RabbitMQ（16 条 /middleware/rabbitmq/*）
  // ==========================================================================
  {
    id: 'mq-rabbitmq',
    name: T('消息队列 · RabbitMQ', 'MQ · RabbitMQ'),
    icon: 'deploy',
    actions: [
      {
        id: 'rabbitmq-deploy', name: T('部署', 'Deploy'), method: 'POST', taskType: 'task',
        path: () => '/middleware/rabbitmq/deploy',
        fields: [
          deviceField(),
          { key: 'mode', label: T('模式', 'Mode'), type: 'select', options: ['single', 'cluster'] },
          { key: 'version', label: T('版本', 'Version'), type: 'text', placeholder: '3.13' }
        ]
      },
      {
        id: 'rabbitmq-lifecycle', name: T('启停/插件', 'Lifecycle'), method: 'POST', taskType: 'task',
        path: () => '/middleware/rabbitmq/lifecycle',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['start', 'stop', 'restart', 'status', 'enable-plugins'] }
        ]
      },
      {
        id: 'rabbitmq-user', name: T('用户/权限', 'User & Permissions'), method: 'POST', taskType: 'task',
        path: () => '/middleware/rabbitmq/user',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'add', 'delete', 'set-permissions'] },
          { key: 'user', label: T('用户', 'User'), type: 'text' },
          { key: 'password', label: T('密码', 'Password'), type: 'text' },
          { key: 'vhost', label: T('Vhost', 'Vhost'), type: 'text', placeholder: '/' },
          { key: 'conf', label: T('Configure 权限', 'Configure'), type: 'text', placeholder: '.*' },
          { key: 'write', label: T('Write 权限', 'Write'), type: 'text', placeholder: '.*' },
          { key: 'read', label: T('Read 权限', 'Read'), type: 'text', placeholder: '.*' }
        ]
      },
      {
        id: 'rabbitmq-cluster', name: T('集群管理', 'Cluster'), method: 'POST', taskType: 'task',
        path: () => '/middleware/rabbitmq/cluster',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['join', 'status', 'list-nodes'] },
          { key: 'node', label: T('节点', 'Node'), type: 'text', placeholder: 'rabbit@node1' }
        ]
      },
      {
        id: 'rabbitmq-policy', name: T('Policy 管理', 'Policy'), method: 'POST', taskType: 'task',
        path: () => '/middleware/rabbitmq/policy',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'set', 'clear'] },
          { key: 'name', label: T('策略名', 'Name'), type: 'text' },
          { key: 'pattern', label: T('匹配模式', 'Pattern'), type: 'text', placeholder: '^amq\\.' },
          { key: 'def', label: T('策略定义(JSON)', 'Definition (JSON)'), type: 'text',
            placeholder: '{"ha-mode":"all"}' },
          { key: 'priority', label: T('优先级', 'Priority'), type: 'number' },
          { key: 'vhost', label: T('Vhost', 'Vhost'), type: 'text', placeholder: '/' }
        ]
      },
      {
        id: 'rabbitmq-shovel', name: T('Shovel', 'Shovel'), method: 'POST', taskType: 'task',
        path: () => '/middleware/rabbitmq/shovel',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'add', 'delete'] },
          { key: 'name', label: T('名称', 'Name'), type: 'text' },
          { key: 'srcUri', label: T('源 URI', 'Source URI'), type: 'text', placeholder: 'amqp://user:pass@host/' },
          { key: 'destUri', label: T('目标 URI', 'Dest URI'), type: 'text', placeholder: 'amqp://user:pass@host/' },
          { key: 'srcQueue', label: T('源队列', 'Source Queue'), type: 'text' },
          { key: 'destQueue', label: T('目标队列', 'Dest Queue'), type: 'text' }
        ]
      },
      {
        id: 'rabbitmq-federation', name: T('Federation', 'Federation'), method: 'POST', taskType: 'task',
        path: () => '/middleware/rabbitmq/federation',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list-upstreams', 'add-upstream', 'remove-upstream'] },
          { key: 'name', label: T('Upstream 名', 'Name'), type: 'text' },
          { key: 'uri', label: T('上游 URI', 'URI'), type: 'text', placeholder: 'amqp://host/' }
        ]
      },
      {
        id: 'rabbitmq-message', name: T('消息操作', 'Messages'), method: 'POST', taskType: 'task',
        path: () => '/middleware/rabbitmq/message',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['publish', 'get', 'count'] },
          { key: 'queue', label: T('队列', 'Queue'), type: 'text' },
          { key: 'vhost', label: T('Vhost', 'Vhost'), type: 'text', placeholder: '/' },
          { key: 'message', label: T('消息体', 'Message'), type: 'textarea' },
          { key: 'count', label: T('条数', 'Count'), type: 'number' }
        ]
      },
      {
        id: 'rabbitmq-monitor', name: T('监控', 'Monitor'), method: 'POST', taskType: 'task',
        path: () => '/middleware/rabbitmq/monitor',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['overview', 'queues', 'nodes', 'channels'] }
        ]
      },
      {
        id: 'rabbitmq-ha', name: T('HA 集群(自动化)', 'HA Cluster'), method: 'POST', taskType: 'task',
        path: () => '/middleware/rabbitmq/ha',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['setup', 'status', 'join', 'leave'] },
          { key: 'node', label: T('节点', 'Node'), type: 'text', placeholder: 'rabbit@node1' },
          { key: 'nodes', label: T('节点列表', 'Nodes'), type: 'text', placeholder: 'a,b（逗号分隔）' }
        ]
      },
      {
        id: 'rabbitmq-scale', name: T('扩缩容', 'Scale'), method: 'POST', taskType: 'task',
        path: () => '/middleware/rabbitmq/scale',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['add-node', 'remove-node', 'list-nodes'] },
          { key: 'node', label: T('节点', 'Node'), type: 'text', placeholder: 'rabbit@node1' }
        ]
      },
      {
        id: 'rabbitmq-upgrade', name: T('版本升级', 'Upgrade'), method: 'POST', taskType: 'task',
        path: () => '/middleware/rabbitmq/upgrade',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['plan', 'upgrade', 'rollback', 'version'] },
          { key: 'targetVersion', label: T('目标版本', 'Target Version'), type: 'text', placeholder: '3.13.0' }
        ]
      },
      {
        id: 'rabbitmq-backup', name: T('备份/恢复', 'Backup/Restore'), method: 'POST', taskType: 'task',
        path: () => '/middleware/rabbitmq/backup',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['export', 'import', 'list'] },
          { key: 'path', label: T('定义文件路径', 'Path'), type: 'text',
            placeholder: '/tmp/rabbitmq_backup.json' }
        ]
      },
      {
        id: 'rabbitmq-tuning', name: T('性能调优', 'Tuning'), method: 'POST', taskType: 'task',
        path: () => '/middleware/rabbitmq/tuning',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['analyze', 'recommend', 'apply'] }
        ]
      },
      {
        id: 'rabbitmq-security', name: T('安全(TLS)', 'Security (TLS)'), method: 'POST', taskType: 'task',
        path: () => '/middleware/rabbitmq/security',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['enable-tls', 'status', 'configure'] },
          { key: 'certPath', label: T('证书路径', 'Cert Path'), type: 'text' },
          { key: 'keyPath', label: T('私钥路径', 'Key Path'), type: 'text' },
          { key: 'caCert', label: T('CA 证书路径', 'CA Cert'), type: 'text' }
        ]
      },
      {
        id: 'rabbitmq-observability', name: T('可观测(Prometheus)', 'Observability'), method: 'POST', taskType: 'task',
        path: () => '/middleware/rabbitmq/observability',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['enable-prometheus', 'status'] }
        ]
      }
    ]
  },

  // ==========================================================================
  // RocketMQ（11 条 /middleware/rocketmq/*）
  // ==========================================================================
  {
    id: 'mq-rocketmq',
    name: T('消息队列 · RocketMQ', 'MQ · RocketMQ'),
    icon: 'deploy',
    actions: [
      {
        id: 'rocketmq-deploy', name: T('部署', 'Deploy'), method: 'POST', taskType: 'task',
        path: () => '/middleware/rocketmq/deploy',
        fields: [
          deviceField(),
          { key: 'mode', label: T('模式', 'Mode'), type: 'select', options: ['single', 'cluster'] },
          { key: 'version', label: T('版本', 'Version'), type: 'text', placeholder: '5.2.0' }
        ]
      },
      {
        id: 'rocketmq-lifecycle', name: T('启停/状态', 'Lifecycle'), method: 'POST', taskType: 'task',
        path: () => '/middleware/rocketmq/lifecycle',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['start-namesrv', 'start-broker', 'stop', 'status'] }
        ]
      },
      {
        id: 'rocketmq-topic', name: T('Topic 管理', 'Topic'), method: 'POST', taskType: 'task',
        path: () => '/middleware/rocketmq/topic',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'create', 'delete'] },
          { key: 'topic', label: T('Topic', 'Topic'), type: 'text' }
        ]
      },
      {
        id: 'rocketmq-consumer-group', name: T('消费组', 'Consumer Group'), method: 'POST', taskType: 'task',
        path: () => '/middleware/rocketmq/consumer-group',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'describe'] },
          { key: 'group', label: T('消费组', 'Group'), type: 'text' }
        ]
      },
      {
        id: 'rocketmq-acl', name: T('ACL 管理', 'ACL'), method: 'POST', taskType: 'task',
        path: () => '/middleware/rocketmq/acl',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'create', 'delete'] },
          { key: 'subject', label: T('Subject', 'Subject'), type: 'text' },
          { key: 'topic', label: T('Topic', 'Topic'), type: 'text' },
          { key: 'group', label: T('消费组', 'Group'), type: 'text' }
        ]
      },
      {
        id: 'rocketmq-ha', name: T('HA(DLedger)', 'HA (DLedger)'), method: 'POST', taskType: 'task',
        path: () => '/middleware/rocketmq/ha',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['setup-dledger', 'status'] },
          { key: 'nodes', label: T('节点列表', 'Nodes'), type: 'text', placeholder: 'a,b（逗号分隔）' }
        ]
      },
      {
        id: 'rocketmq-scale', name: T('Broker 扩缩容', 'Broker Scale'), method: 'POST', taskType: 'task',
        path: () => '/middleware/rocketmq/scale',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['add-broker', 'remove-broker', 'list-brokers'] },
          { key: 'broker', label: T('Broker', 'Broker'), type: 'text' }
        ]
      },
      {
        id: 'rocketmq-upgrade', name: T('版本升级', 'Upgrade'), method: 'POST', taskType: 'task',
        path: () => '/middleware/rocketmq/upgrade',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['plan', 'upgrade', 'rollback', 'version'] },
          { key: 'targetVersion', label: T('目标版本', 'Target Version'), type: 'text', placeholder: '5.2.0' }
        ]
      },
      {
        id: 'rocketmq-monitor', name: T('监控', 'Monitor'), method: 'POST', taskType: 'task',
        path: () => '/middleware/rocketmq/monitor',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['status', 'metrics', 'topics'] }
        ]
      },
      {
        id: 'rocketmq-dlq', name: T('死信队列', 'DLQ'), method: 'POST', taskType: 'task',
        path: () => '/middleware/rocketmq/dlq',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'retry', 'purge'] },
          { key: 'topic', label: T('Topic', 'Topic'), type: 'text' },
          { key: 'group', label: T('消费组', 'Group'), type: 'text' }
        ]
      },
      {
        id: 'rocketmq-tuning', name: T('性能调优', 'Tuning'), method: 'POST', taskType: 'task',
        path: () => '/middleware/rocketmq/tuning',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['analyze', 'recommend', 'apply'] }
        ]
      }
    ]
  },

  // ==========================================================================
  // ActiveMQ（10 条 /middleware/activemq/*）
  // ==========================================================================
  {
    id: 'mq-activemq',
    name: T('消息队列 · ActiveMQ', 'MQ · ActiveMQ'),
    icon: 'deploy',
    actions: [
      {
        id: 'activemq-deploy', name: T('部署', 'Deploy'), method: 'POST', taskType: 'task',
        path: () => '/middleware/activemq/deploy',
        fields: [
          deviceField(),
          { key: 'mode', label: T('模式', 'Mode'), type: 'select',
            options: ['single', 'master-slave', 'network'] },
          { key: 'version', label: T('版本', 'Version'), type: 'text', placeholder: '5.18.3' }
        ]
      },
      {
        id: 'activemq-lifecycle', name: T('启停/状态', 'Lifecycle'), method: 'POST', taskType: 'task',
        path: () => '/middleware/activemq/lifecycle',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['start', 'stop', 'restart', 'status'] }
        ]
      },
      {
        id: 'activemq-monitor', name: T('监控', 'Monitor'), method: 'POST', taskType: 'task',
        path: () => '/middleware/activemq/monitor',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['status', 'queues', 'topics', 'connections'] }
        ]
      },
      {
        id: 'activemq-ha', name: T('主从 HA', 'Master-Slave HA'), method: 'POST', taskType: 'task',
        path: () => '/middleware/activemq/ha',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['setup-master-slave', 'status'] },
          { key: 'master', label: T('Master', 'Master'), type: 'text' },
          { key: 'slave', label: T('Slave', 'Slave'), type: 'text' }
        ]
      },
      {
        id: 'activemq-network', name: T('Broker 网络', 'Network of Brokers'), method: 'POST', taskType: 'task',
        path: () => '/middleware/activemq/network',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['setup', 'status'] },
          { key: 'brokers', label: T('Broker 列表', 'Brokers'), type: 'text', placeholder: 'tcp://a:61616,tcp://b:61616' }
        ]
      },
      {
        id: 'activemq-security', name: T('用户管理', 'Users'), method: 'POST', taskType: 'task',
        path: () => '/middleware/activemq/security',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list-users', 'add-user', 'delete-user'] },
          { key: 'user', label: T('用户', 'User'), type: 'text' },
          { key: 'password', label: T('密码', 'Password'), type: 'text' },
          { key: 'role', label: T('角色', 'Role'), type: 'text' }
        ]
      },
      {
        id: 'activemq-durable-sub', name: T('持久订阅', 'Durable Subscription'), method: 'POST', taskType: 'task',
        path: () => '/middleware/activemq/durable-sub',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'create', 'delete'] },
          { key: 'name', label: T('订阅名', 'Name'), type: 'text' },
          { key: 'topic', label: T('Topic', 'Topic'), type: 'text' },
          { key: 'clientId', label: T('Client ID', 'Client ID'), type: 'text' }
        ]
      },
      {
        id: 'activemq-upgrade', name: T('版本升级', 'Upgrade'), method: 'POST', taskType: 'task',
        path: () => '/middleware/activemq/upgrade',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['plan', 'upgrade', 'rollback', 'version'] },
          { key: 'targetVersion', label: T('目标版本', 'Target Version'), type: 'text', placeholder: '5.18.3' }
        ]
      },
      {
        id: 'activemq-queue', name: T('队列管理', 'Queue'), method: 'POST', taskType: 'task',
        path: () => '/middleware/activemq/queue',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'create', 'delete'] },
          { key: 'name', label: T('队列名', 'Queue Name'), type: 'text' }
        ]
      },
      {
        id: 'activemq-topic', name: T('Topic 管理', 'Topic'), method: 'POST', taskType: 'task',
        path: () => '/middleware/activemq/topic',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'create', 'delete'] },
          { key: 'name', label: T('Topic 名', 'Topic Name'), type: 'text' }
        ]
      }
    ]
  },

  // ==========================================================================
  // Redis 集群（中间件层 1 条 /middleware/redis/cluster）
  // ==========================================================================
  {
    id: 'mq-redis',
    name: T('消息队列 · Redis 集群', 'MQ · Redis Cluster'),
    icon: 'deploy',
    actions: [
      {
        id: 'redis-middleware-cluster', name: T('集群操作', 'Cluster Ops'), method: 'POST', taskType: 'task',
        path: () => '/middleware/redis/cluster',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['nodes', 'slots', 'rebalance'] },
          { key: 'host', label: T('节点地址', 'Host'), type: 'text', placeholder: 'localhost:6379' }
        ]
      }
    ]
  }
]
