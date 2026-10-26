# Kafka 3-Node Cluster (KRaft Mode)

轻量级 Kafka 集群，无 ZooKeeper 依赖，适合开发/测试。

## 快速启动

```bash
# 启动集群
docker compose -f docker-compose.kafka.yml up -d

# 等待健康检查通过 (约 30-60s)
docker compose -f docker-compose.kafka.yml ps

# 查看日志
docker compose -f docker-compose.kafka.yml logs -f kafka-1
```

## 连接信息

| 服务 | 地址 | 说明 |
|------|------|------|
| Kafka Broker 1 | localhost:9092 | Controller + Broker |
| Kafka Broker 2 | localhost:9093 | Controller + Broker |
| Kafka Broker 3 | localhost:9094 | Controller + Broker |
| Kafka UI | http://localhost:8080 | Web 管理界面 |

## 基本操作

### 创建 Topic
```bash
docker exec kafka-1 kafka-topics.sh --bootstrap-server localhost:9092 --create --topic ops-events --partitions 3 --replication-factor 3
```

### 列出 Topic
```bash
docker exec kafka-1 kafka-topics.sh --bootstrap-server localhost:9092 --list
```

### 查看 Topic 详情
```bash
docker exec kafka-1 kafka-topics.sh --bootstrap-server localhost:9092 --describe --topic ops-events
```

### 发送消息 (Python)
```bash
pip install kafka-python
python scripts/kafka-producer.py --topic ops-events --count 20 --interval 0.3
```

### 消费消息 (Python)
```bash
python scripts/kafka-consumer.py --topic ops-events --group ops-consumer-group --from-beginning --max 20
```

### 查看消费者组
```bash
docker exec kafka-1 kafka-consumer-groups.sh --bootstrap-server localhost:9092 --list
docker exec kafka-1 kafka-consumer-groups.sh --bootstrap-server localhost:9092 --describe --group ops-consumer-group
```

## 集群管理

### 停止集群
```bash
docker compose -f docker-compose.kafka.yml down
```

### 清理数据 (重新开始)
```bash
docker compose -f docker-compose.kafka.yml down -v
```

### 扩容 (添加 Broker)
```bash
# 修改 docker-compose.kafka.yml 添加 kafka-4 服务
# KAFKA_NODE_ID: 4
# KAFKA_CONTROLLER_QUORUM_VOTERS: "1@kafka-1:9093,2@kafka-2:9093,3@kafka-3:9093,4@kafka-4:9093"
# 然后 docker compose up -d kafka-4
```

## 配置说明

| 参数 | 值 | 说明 |
|------|-----|------|
| KAFKA_NODE_ID | 1/2/3 | 节点 ID |
| KAFKA_PROCESS_ROLES | broker,controller | KRaft 模式 (无 ZK) |
| KAFKA_DEFAULT_REPLICATION_FACTOR | 3 | 默认副本数 |
| KAFKA_MIN_INSYNC_REPLICAS | 2 | 最小同步副本 |
| KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR | 3 | __consumer_offsets 副本数 |
| KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR | 3 | 事务日志副本数 |
| KAFKA_AUTO_CREATE_TOPICS_ENABLE | true | 自动创建 Topic |
| KAFKA_LOG_RETENTION_HOURS | 168 | 日志保留 7 天 |
| KAFKA_NUM_PARTITIONS | 3 | 默认分区数 |

## 与 OpsMesh 集成

OpsMesh 的 Kafka 运维功能通过 `/api/v1/middleware/kafka/*` 端点管理此集群：

```bash
# 通过 OpsMesh API 创建 Topic
curl -X POST http://localhost:8080/api/v1/middleware/kafka/topic \
  -H "Content-Type: application/json" \
  -d '{"deviceID":"kafka-1","action":"create","topic":"ops-events","partitions":3,"replicas":3}'

# 通过 OpsMesh API 列出 Topic
curl -X POST http://localhost:9092/api/v1/middleware/kafka/topic \
  -H "Content-Type: application/json" \
  -d '{"deviceID":"kafka-1","action":"list"}'
```

## 资源占用

| 容器 | 内存 | CPU |
|------|------|-----|
| kafka-1/2/3 | ~512MB each | ~0.5 core each |
| kafka-ui | ~256MB | ~0.25 core |
| **总计** | **~1.8GB** | **~1.75 cores** |
