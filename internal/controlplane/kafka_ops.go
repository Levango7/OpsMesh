// Package controlplane kafka_ops.go 实现 Kafka 2.x/3.x 运维场景的 HTTP handler。
//
// 覆盖场景：
//   - Topic 管理：create/delete/list/describe/alter partitions/alter config
//   - Consumer Group：list/describe/reset-offsets/lag
//   - Broker/Cluster：describe cluster/log dirs/under-replicated partitions
//   - ACL：list/create/delete
//   - Kafka Connect：connector 管理 (list/create/delete/status)
//   - Schema Registry：schema 管理 (list/register/compatibility)
//   - KRaft：controller 状态/元数据
//
// 所有执行类接口返回 Task（异步下发到 agent），查询类直接返回结构化 JSON。
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)
// kafkaGroupRe 校验 Consumer Group 名称。
var kafkaGroupRe = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// kafkaResourceRe 校验资源名称（ACL/Connector/Schema 等）。
var kafkaResourceRe = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// dispatchKafkaTask creates a system task for a kafka operation and returns its task ID.
func (s *Server) dispatchKafkaTask(w http.ResponseWriter, r *http.Request, deviceID, script, action string) (string, bool) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return "", false
	}
	task, err := s.createSystemTask(actx.TenantID, deviceID, script, "kafka_"+action)
	if err != nil {
		writeInternalError(r.Context(), w, "dispatchKafkaTask", err)
		return "", false
	}
	return task.TaskID, true
}
// ============================================================================
// Broker / Cluster 管理
// ============================================================================

// handleKafkaBroker 处理 POST /api/v1/middleware/kafka/broker
func (s *Server) handleKafkaBroker(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "describe-cluster":
		script := `kafka-cluster.sh --bootstrap-server localhost:9092 --describe 2>/dev/null || kafka-broker-api-versions.sh --bootstrap-server localhost:9092`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "describe-cluster")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "describe-cluster"})

	case "list-brokers":
		script := `kafka-broker-api-versions.sh --bootstrap-server localhost:9092 2>/dev/null | head -20`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "list-brokers")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list-brokers"})

	case "under-replicated":
		script := `kafka-topics.sh --bootstrap-server localhost:9092 --describe --under-replicated-partitions 2>/dev/null || echo "No under-replicated partitions"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "under-replicated")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "under-replicated"})

	case "log-dirs":
		script := `kafka-log-dirs.sh --bootstrap-server localhost:9092 --describe --topic-list 2>/dev/null || echo "Log dirs not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "log-dirs")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "log-dirs"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// ACL 管理
// ============================================================================

// handleKafkaACL 处理 POST /api/v1/middleware/kafka/acl
func (s *Server) handleKafkaACL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID   string `json:"deviceID"`
		Action     string `json:"action"`
		Principal  string `json:"principal"`  // User:xxx
		Resource   string `json:"resource"`   // Topic/Group/Cluster
		Name       string `json:"name"`       // 资源名 (Topic 名/Group 名/*)
		Operation  string `json:"operation"`  // Read/Write/Create/Delete/Alter/Describe/All
		Permission string `json:"permission"` // Allow/Deny
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if req.Principal != "" && !kafkaResourceRe.MatchString(strings.TrimPrefix(req.Principal, "User:")) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid principal"})
		return
	}

	switch req.Action {
	case "list":
		script := `kafka-acls.sh --bootstrap-server localhost:9092 --list`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "list")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list"})

	case "create":
		if req.Principal == "" || req.Resource == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "principal and resource are required"})
			return
		}
		op := req.Operation
		if op == "" {
			op = "All"
		}
		perm := req.Permission
		if perm == "" {
			perm = "Allow"
		}
		name := req.Name
		if name == "" {
			name = "*"
		}
		script := fmt.Sprintf(`kafka-acls.sh --bootstrap-server localhost:9092 --add --allow-principal %s --operation %s --%s %s`, req.Principal, op, strings.ToLower(req.Resource), name)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "create")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "create"})

	case "delete":
		if req.Principal == "" || req.Resource == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "principal and resource are required"})
			return
		}
		name := req.Name
		if name == "" {
			name = "*"
		}
		script := fmt.Sprintf(`kafka-acls.sh --bootstrap-server localhost:9092 --remove --force --allow-principal %s --operation All --%s %s`, req.Principal, strings.ToLower(req.Resource), name)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "delete")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "delete"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// Kafka Connect 管理
// ============================================================================

// handleKafkaConnect 处理 POST /api/v1/middleware/kafka/connect
func (s *Server) handleKafkaConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID  string `json:"deviceID"`
		Action    string `json:"action"`
		Connector string `json:"connector"`
		Config    string `json:"config"` // JSON config
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if req.Connector != "" && !kafkaResourceRe.MatchString(req.Connector) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid connector name"})
		return
	}

	connectURL := "http://localhost:8083"

	switch req.Action {
	case "list":
		script := fmt.Sprintf(`curl -s %s/connectors`, connectURL)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "list")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list"})

	case "status":
		if req.Connector == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "connector is required"})
			return
		}
		script := fmt.Sprintf(`curl -s %s/connectors/%s/status`, connectURL, req.Connector)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	case "create":
		if req.Connector == "" || req.Config == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "connector and config are required"})
			return
		}
		script := fmt.Sprintf(`curl -s -X POST -H "Content-Type: application/json" --data '%s' %s/connectors`, req.Config, connectURL)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "create")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "create"})

	case "delete":
		if req.Connector == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "connector is required"})
			return
		}
		script := fmt.Sprintf(`curl -s -X DELETE %s/connectors/%s`, connectURL, req.Connector)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "delete")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "delete"})

	case "restart":
		if req.Connector == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "connector is required"})
			return
		}
		script := fmt.Sprintf(`curl -s -X POST %s/connectors/%s/restart`, connectURL, req.Connector)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "restart")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "restart"})

	case "pause":
		if req.Connector == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "connector is required"})
			return
		}
		script := fmt.Sprintf(`curl -s -X PUT %s/connectors/%s/pause`, connectURL, req.Connector)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "pause")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "pause"})

	case "resume":
		if req.Connector == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "connector is required"})
			return
		}
		script := fmt.Sprintf(`curl -s -X PUT %s/connectors/%s/resume`, connectURL, req.Connector)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "resume")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "resume"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// Schema Registry 管理
// ============================================================================

// handleKafkaSchemaRegistry 处理 POST /api/v1/middleware/kafka/schema-registry
func (s *Server) handleKafkaSchemaRegistry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`
		Subject  string `json:"subject"`
		Schema   string `json:"schema"` // JSON schema
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	srURL := "http://localhost:8081"

	switch req.Action {
	case "list-subjects":
		script := fmt.Sprintf(`curl -s %s/subjects`, srURL)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "list-subjects")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list-subjects"})

	case "get-schema":
		if req.Subject == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "subject is required"})
			return
		}
		script := fmt.Sprintf(`curl -s %s/subjects/%s/versions/latest`, srURL, req.Subject)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "get-schema")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "get-schema"})

	case "register":
		if req.Subject == "" || req.Schema == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "subject and schema are required"})
			return
		}
		script := fmt.Sprintf(`curl -s -X POST -H "Content-Type: application/vnd.schemaregistry.v1+json" --data '{"schema":%s}' %s/subjects/%s/versions`, req.Schema, srURL, req.Subject)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "register")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "register"})

	case "check-compat":
		if req.Subject == "" || req.Schema == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "subject and schema are required"})
			return
		}
		script := fmt.Sprintf(`curl -s -X POST -H "Content-Type: application/vnd.schemaregistry.v1+json" --data '{"schema":%s}' %s/compatibility/subjects/%s/versions/latest`, req.Schema, srURL, req.Subject)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "check-compat")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "check-compat"})

	case "list-versions":
		if req.Subject == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "subject is required"})
			return
		}
		script := fmt.Sprintf(`curl -s %s/subjects/%s/versions`, srURL, req.Subject)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "list-versions")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list-versions"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// KRaft 模式管理
// ============================================================================

// handleKafkaKRaft 处理 POST /api/v1/middleware/kafka/kraft
func (s *Server) handleKafkaKRaft(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "status":
		script := `kafka-metadata.sh --bootstrap-server localhost:9092 --describe 2>/dev/null || echo "KRaft metadata not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	case "controller":
		script := `kafka-metadata.sh --bootstrap-server localhost:9092 --controller 2>/dev/null || echo "Controller info not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "controller")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "controller"})

	case "metadata-quorum":
		script := `kafka-metadata-quorum.sh --bootstrap-server localhost:9092 describe --status 2>/dev/null || echo "Quorum status not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "metadata-quorum")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "metadata-quorum"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// Quota 管理
// ============================================================================

// handleKafkaQuota 处理 POST /api/v1/middleware/kafka/quota
func (s *Server) handleKafkaQuota(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID     string `json:"deviceID"`
		Action       string `json:"action"`
		Entity       string `json:"entity"`                 // user/client-id
		EntityName   string `json:"entityName"`             // 实体名称
		ProducerRate int64  `json:"producerRate,omitempty"` // bytes/sec
		ConsumerRate int64  `json:"consumerRate,omitempty"` // bytes/sec
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "list":
		script := `kafka-configs.sh --bootstrap-server localhost:9092 --describe --entity-type clients 2>/dev/null; kafka-configs.sh --bootstrap-server localhost:9092 --describe --entity-type users 2>/dev/null`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "list")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list"})

	case "set":
		if req.Entity == "" || req.EntityName == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "entity and entityName are required"})
			return
		}
		configs := ""
		if req.ProducerRate > 0 {
			configs += fmt.Sprintf("producer_byte_rate=%d,", req.ProducerRate)
		}
		if req.ConsumerRate > 0 {
			configs += fmt.Sprintf("consumer_byte_rate=%d,", req.ConsumerRate)
		}
		if configs == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "at least one rate limit is required"})
			return
		}
		configs = strings.TrimSuffix(configs, ",")
		script := fmt.Sprintf(`kafka-configs.sh --bootstrap-server localhost:9092 --entity-type %s --entity-name %s --alter --add-config %s`, req.Entity, req.EntityName, configs)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "set")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "set"})

	case "delete":
		if req.Entity == "" || req.EntityName == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "entity and entityName are required"})
			return
		}
		script := fmt.Sprintf(`kafka-configs.sh --bootstrap-server localhost:9092 --entity-type %s --entity-name %s --alter --delete-config producer_byte_rate,consumer_byte_rate`, req.Entity, req.EntityName)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "delete")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "delete"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// 路由注册
// ============================================================================

// registerKafkaRoutes 注册 Kafka 扩展路由 (在 server_lifecycle.go 中调用)
func (s *Server) registerKafkaRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/middleware/kafka/broker", s.handleKafkaBroker)
	mux.HandleFunc("/api/v1/middleware/kafka/acl", s.handleKafkaACL)
	mux.HandleFunc("/api/v1/middleware/kafka/connect", s.handleKafkaConnect)
	mux.HandleFunc("/api/v1/middleware/kafka/schema-registry", s.handleKafkaSchemaRegistry)
	mux.HandleFunc("/api/v1/middleware/kafka/kraft", s.handleKafkaKRaft)
	mux.HandleFunc("/api/v1/middleware/kafka/quota", s.handleKafkaQuota)
}
