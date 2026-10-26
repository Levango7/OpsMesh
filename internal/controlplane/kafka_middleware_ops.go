// kafka_middleware_ops.go Kafka 中间件运维管理。
package controlplane

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// ============================================================================
// Kafka Consumer Group 管理（版本感知）
// ============================================================================

// kafkaConsumerGroupReq Kafka Consumer Group 操作请求体。
type kafkaConsumerGroupReq struct {
	DeviceID string `json:"deviceID"`
	Action   string `json:"action"` // list/describe
	Group    string `json:"group"`  // 消费组名称（describe 时必填）
}

// handleKafkaConsumerGroup 处理 POST /api/v1/middleware/kafka/consumer-group。
func (s *Server) handleKafkaConsumerGroup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "system:write"); !ok {
		return
	}
	var req kafkaConsumerGroupReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	allowed := map[string]bool{"list": true, "describe": true}
	if !allowed[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (list/describe)"})
		return
	}
	if req.Action == "describe" && req.Group == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "describe 需指定 group"})
		return
	}
	script := buildKafkaConsumerGroupScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "kafka_consumergroup_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "middleware.kafka.consumergroup", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "kafka_consumergroup_" + req.Action, Target: req.Group,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildKafkaConsumerGroupScript(req kafkaConsumerGroupReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("if [ -z \"$KAFKA_OPTS\" ]; then KAFKA_OPTS=\"--bootstrap-server localhost:9092\"; fi\n")
	switch req.Action {
	case "list":
		b.WriteString("kafka-consumer-groups.sh $KAFKA_OPTS --list\n")
	case "describe":
		fmt.Fprintf(&b, "kafka-consumer-groups.sh $KAFKA_OPTS --describe --group %s\n", req.Group)
	}
	fmt.Fprintf(&b, "echo 'Kafka Consumer Group %s 操作完成\n", req.Action)
	return b.String()
}

// ============================================================================
// Kafka Topic 管理
// ============================================================================

// kafkaTopicReq Kafka Topic 操作请求体。
type kafkaTopicReq struct {
	DeviceID   string `json:"deviceID"`
	Action     string `json:"action"` // create/delete/list
	Topic      string `json:"topic"`
	Partitions int    `json:"partitions"`
	Replicas   int    `json:"replicas"`
}

// handleKafkaTopic 处理 POST /api/v1/middleware/kafka/topic。
func (s *Server) handleKafkaTopic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "system:write"); !ok {
		return
	}
	var req kafkaTopicReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if !kafkaTopicRe.MatchString(req.Topic) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid topic name"})
		return
	}
	allowed := map[string]bool{"create": true, "delete": true, "list": true}
	if !allowed[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (create/delete/list)"})
		return
	}
	script := buildKafkaTopicScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "kafka_topic_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "middleware.kafka.topic", err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "kafka_topic_" + req.Action, Target: req.Topic,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildKafkaTopicScript(req kafkaTopicReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("BOOTSTRAP=localhost:9092\n")
	switch req.Action {
	case "create":
		if req.Partitions <= 0 {
			req.Partitions = 1
		}
		if req.Replicas <= 0 {
			req.Replicas = 1
		}
		fmt.Fprintf(&b, "kafka-topics.sh --create --topic %s --partitions %d --replication-factor %d --bootstrap-server $BOOTSTRAP\n", req.Topic, req.Partitions, req.Replicas)
	case "delete":
		fmt.Fprintf(&b, "kafka-topics.sh --delete --topic %s --bootstrap-server $BOOTSTRAP\n", req.Topic)
	case "list":
		b.WriteString("kafka-topics.sh --list --bootstrap-server $BOOTSTRAP\n")
	}
	fmt.Fprintf(&b, "echo 'Kafka Topic %s 操作完成\n", req.Action)
	return b.String()
}

// ============================================================================
// 路由注册
// ============================================================================

// registerMiddlewareRoutes 注册中间件运维路由。
// 固定路径（kafka/topic、kafka/consumer-group、zookeeper/cmd、redis/sentinel、redis/cluster、
// rabbitmq/{vhost|queue|exchange}、activemq/{queue|topic}）优先注册，
// 让 ServeMux 精确匹配优先于前缀泛路由 /api/v1/middleware/。
func (s *Server) registerMiddlewareRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/middleware/kafka/consumer-group", s.handleKafkaConsumerGroup) // POST Consumer Group list/describe
	mux.HandleFunc("/api/v1/middleware/zookeeper/cmd", s.handleZookeeperCmd)              // POST Zookeeper 四字命令
	mux.HandleFunc("/api/v1/middleware/redis/sentinel", s.handleMiddlewareRedisSentinel)  // POST Redis Sentinel monitor/failover
	mux.HandleFunc("/api/v1/middleware/redis/cluster", s.handleMiddlewareRedisCluster)    // POST Redis Cluster nodes/slots/rebalance
	mux.HandleFunc("/api/v1/middleware/rabbitmq/vhost", s.handleRabbitMQVhost)            // POST RabbitMQ Vhost 管理
	mux.HandleFunc("/api/v1/middleware/rabbitmq/queue", s.handleRabbitMQQueue)            // POST RabbitMQ Queue 管理
	mux.HandleFunc("/api/v1/middleware/rabbitmq/exchange", s.handleRabbitMQExchange)      // POST RabbitMQ Exchange 管理
	mux.HandleFunc("/api/v1/middleware/activemq/queue", s.handleActiveMQQueue)            // POST ActiveMQ Queue 管理
	mux.HandleFunc("/api/v1/middleware/activemq/topic", s.handleActiveMQTopic)            // POST ActiveMQ Topic 管理
	mux.HandleFunc("/api/v1/middleware/", s.handleMiddlewareRouting)
}
