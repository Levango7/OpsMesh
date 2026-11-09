// 追加到 kafka_ops.go 的新 handler

package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/pkg/shellquote"
)

// ============================================================================
// Partition Rebalance (preferred leader election + reassignment)
// ============================================================================

// handleKafkaRebalance 处理 POST /api/v1/middleware/kafka/rebalance
func (s *Server) handleKafkaRebalance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`   // preferred-leader-election / reassign-partitions / cancel-reassignment
		Topics   string `json:"topics"`   // 逗号分隔的 topic 列表 (空=全部)
		Throttle int    `json:"throttle"` // 带宽限制 (bytes/sec, 0=不限)
		Timeout  int    `json:"timeout"`  // 超时 (秒)
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "preferred-leader-election":
		topicsFlag := "--all-topic-partitions"
		if req.Topics != "" {
			// 注入面（批次 10）：topics 是逗号分隔的 topic 列表，旧形态裸拼进
			// `--topic %s` ⇒ 逐项按 kafkaTopicRe 校验（挡「列表里一项是另一个选项」），
			// 再整体 Quote（挡 shell 层）。
			topics, err := kafkaTopicList(req.Topics)
			if err != nil {
				paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			topicsFlag = fmt.Sprintf("--topic %s", shellquote.Quote(topics))
		}
		script := fmt.Sprintf(`kafka-leader-election.sh --bootstrap-server localhost:9092 --election-type preferred %s`, topicsFlag)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rebalance_preferred-leader-election")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "preferred-leader-election"})

	case "reassign-partitions":
		if req.Topics == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "topics is required for reassignment"})
			return
		}
		// 生成 reassignment plan
		// 注入面（批次 10）：topics 被裸拼进 JSON 字面量（`{"topic":"%s"}`）⇒ 字段里的
		// `"` 会**破坏 JSON 结构**、`'` 会**破 shell 单引号段** ⇒ 改由 Go 层 map +
		// json.Marshal 构造（结构与旧形态一致），再整体 Quote。
		payload, err := opsJSONPayload(map[string]any{
			"topics":  []any{map[string]any{"topic": req.Topics}},
			"version": 1,
		})
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid topics: " + err.Error()})
			return
		}
		genScript := fmt.Sprintf(`kafka-reassign-partitions.sh --bootstrap-server localhost:9092 --topics-to-move-json-file <(echo %s) --broker-list "0,1,2" --generate 2>/dev/null`, payload)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, genScript, "rebalance_reassign-generate")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "reassign-generate"})

	case "execute-reassignment":
		// 执行 reassignment (需要 reassignment JSON 文件)
		//
		// 注入面（批次 10）：throttle 是 **int** 字段（JSON 解码阶段类型已定 ⇒
		// 不可能携带 shell 元字符，**不构成注入面**），但盘点器判据 C/D 是行级文本
		// 判据、不区分字段类型 ⇒ 用 opsPositiveInt 把「这是整数」显式写进代码。
		//
		// 语义与旧逻辑逐字一致：throttle<=0 ⇒ 不带 --throttle 参数。
		script := `kafka-reassign-partitions.sh --bootstrap-server localhost:9092 --reassignment-json-file /tmp/reassignment.json --execute 2>/dev/null`
		if throttle, ok := opsPositiveInt(req.Throttle); ok {
			script = fmt.Sprintf(`kafka-reassign-partitions.sh --bootstrap-server localhost:9092 --reassignment-json-file /tmp/reassignment.json --execute --throttle %d 2>/dev/null`, throttle)
		}
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rebalance_execute")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "execute-reassignment"})

	case "verify-reassignment":
		script := `kafka-reassign-partitions.sh --bootstrap-server localhost:9092 --reassignment-json-file /tmp/reassignment.json --verify 2>/dev/null`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rebalance_verify")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "verify-reassignment"})

	case "cancel-reassignment":
		script := `kafka-reassign-partitions.sh --bootstrap-server localhost:9092 --reassignment-json-file /tmp/reassignment.json --cancel 2>/dev/null`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rebalance_cancel")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "cancel-reassignment"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// Record Deletion (kafka-delete-records)
// ============================================================================

// handleKafkaDeleteRecords 处理 POST /api/v1/middleware/kafka/delete-records
func (s *Server) handleKafkaDeleteRecords(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID  string `json:"deviceID"`
		Topic     string `json:"topic"`
		Partition int    `json:"partition"`
		Offset    int64  `json:"offset"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if !kafkaTopicRe.MatchString(req.Topic) {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid topic name"})
		return
	}

	// 注入面（批次 10）：topic 被裸拼进 JSON 字面量 ⇒ Go 层构造后整体 Quote
	// （键序改由 json.Marshal 决定，语义等价）。
	payload, err := opsJSONPayload(map[string]any{
		"partitions": []any{map[string]any{
			"topic":     req.Topic,
			"partition": req.Partition,
			"offset":    req.Offset,
		}},
		"version": 1,
	})
	if err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid delete-records payload: " + err.Error()})
		return
	}
	script := fmt.Sprintf(`echo %s > /tmp/delete-records.json && kafka-delete-records.sh --bootstrap-server localhost:9092 --offset-json-file /tmp/delete-records.json`, payload)
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "delete-records")
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "delete-records"})
}

// ============================================================================
// Delegation Tokens (SASL)
// ============================================================================

// handleKafkaDelegationToken 处理 POST /api/v1/middleware/kafka/delegation-token
func (s *Server) handleKafkaDelegationToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID  string `json:"deviceID"`
		Action    string `json:"action"`    // create/describe/expire/renew
		Principal string `json:"principal"` // User:xxx
		TokenID   string `json:"tokenId"`
		Expiry    int64  `json:"expiry"` // ms
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "create":
		if req.Principal == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "principal is required"})
			return
		}
		// 注入面（批次 10）：principal 是**值** ⇒ 整体 Quote（shell 层封住）。
		script := fmt.Sprintf(`kafka-delegation-tokens.sh --bootstrap-server localhost:9092 --create --renewer-principal %s`, shellquote.Quote(req.Principal))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "delegation-token_create")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "create"})

	case "describe":
		script := `kafka-delegation-tokens.sh --bootstrap-server localhost:9092 --describe`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "delegation-token_describe")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "describe"})

	case "expire":
		if req.TokenID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "tokenId is required"})
			return
		}
		// 注入面（批次 10）：tokenId 是**值** ⇒ 整体 Quote。
		script := fmt.Sprintf(`kafka-delegation-tokens.sh --bootstrap-server localhost:9092 --expire --delegation-token %s`, shellquote.Quote(req.TokenID))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "delegation-token_expire")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "expire"})

	case "renew":
		if req.TokenID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "tokenId is required"})
			return
		}
		// 注入面（批次 10）：tokenId 是**值** ⇒ 整体 Quote。
		script := fmt.Sprintf(`kafka-delegation-tokens.sh --bootstrap-server localhost:9092 --renew --delegation-token %s`, shellquote.Quote(req.TokenID))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "delegation-token_renew")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "renew"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// Topic/Broker Config Alter (baseline repair + performance tuning)
// ============================================================================

// handleKafkaConfig 处理 POST /api/v1/middleware/kafka/config
func (s *Server) handleKafkaConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID   string `json:"deviceID"`
		EntityType string `json:"entityType"` // topics / brokers
		EntityName string `json:"entityName"` // topic name or broker id
		Action     string `json:"action"`     // describe / alter / delete
		Config     string `json:"config"`     // key=value,key=value
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if req.EntityType == "" {
		req.EntityType = "topics"
	}

	switch req.Action {
	case "describe":
		// 注入面（批次 10）：entity-type 与 entity-name 是 Kafka CLI 的**选项值** ⇒
		// 白名单（复用 kafkaResourceRe 的字符集）。
		entityType, err := kafkaResource(req.EntityType)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		entityName, err := kafkaResource(req.EntityName)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		script := fmt.Sprintf(`kafka-configs.sh --bootstrap-server localhost:9092 --entity-type %s --entity-name %s --describe`, shellquote.Quote(entityType), shellquote.Quote(entityName))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "config_describe")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "describe"})

	case "alter":
		if req.Config == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "config is required (key=value,key=value)"})
			return
		}
		// 注入面（批次 10）：entity-type / entity-name 是**选项值** ⇒ 白名单；
		// config 是**值**（key=value,…）⇒ 整体 Quote。
		entityType, err := kafkaResource(req.EntityType)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		entityName, err := kafkaResource(req.EntityName)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		script := fmt.Sprintf(`kafka-configs.sh --bootstrap-server localhost:9092 --entity-type %s --entity-name %s --alter --add-config %s`, shellquote.Quote(entityType), shellquote.Quote(entityName), shellquote.Quote(req.Config))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "config_alter")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "alter"})

	case "delete":
		if req.Config == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "config keys to delete required (comma-separated)"})
			return
		}
		// 注入面（批次 10）：entity-type / entity-name 是**选项值** ⇒ 白名单；
		// config 是**值** ⇒ 整体 Quote。
		entityType, err := kafkaResource(req.EntityType)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		entityName, err := kafkaResource(req.EntityName)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		script := fmt.Sprintf(`kafka-configs.sh --bootstrap-server localhost:9092 --entity-type %s --entity-name %s --alter --delete-config %s`, shellquote.Quote(entityType), shellquote.Quote(entityName), shellquote.Quote(req.Config))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "config_delete")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "delete"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// ISR / Under-Replicated 状态检查
// ============================================================================

// handleKafkaISR 处理 POST /api/v1/middleware/kafka/isr
func (s *Server) handleKafkaISR(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // check / under-replicated / offline-partitions
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "check":
		script := `kafka-topics.sh --bootstrap-server localhost:9092 --describe 2>/dev/null | grep -E "Isr:|Leader:"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "isr_check")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "check"})

	case "under-replicated":
		script := `kafka-topics.sh --bootstrap-server localhost:9092 --describe --under-replicated-partitions 2>/dev/null || echo "No under-replicated partitions found"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "isr_under-replicated")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "under-replicated"})

	case "offline-partitions":
		script := `kafka-topics.sh --bootstrap-server localhost:9092 --describe --unavailable-partitions 2>/dev/null || echo "No unavailable partitions found"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "isr_offline-partitions")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "offline-partitions"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// Cluster Health Check (基线验证)
// ============================================================================

// handleKafkaHealthCheck 处理 POST /api/v1/middleware/kafka/health-check
func (s *Server) handleKafkaHealthCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	script := `echo "=== Broker Status ===" && kafka-broker-api-versions.sh --bootstrap-server localhost:9092 2>/dev/null | head -5 && echo "=== Topic Count ===" && kafka-topics.sh --bootstrap-server localhost:9092 --list 2>/dev/null | wc -l && echo "=== Under-Replicated ===" && kafka-topics.sh --bootstrap-server localhost:9092 --describe --under-replicated-partitions 2>/dev/null && echo "=== Consumer Groups ===" && kafka-consumer-groups.sh --bootstrap-server localhost:9092 --list 2>/dev/null && echo "=== KRaft Controller ===" && kafka-metadata-quorum.sh --bootstrap-server localhost:9092 describe --status 2>/dev/null`
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "health-check")
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "health-check"})
}

// ============================================================================
// 路由注册 (追加)
// ============================================================================

// registerKafkaExtendedRoutes 注册 Kafka 扩展路由
func (s *Server) registerKafkaExtendedRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/middleware/kafka/rebalance", s.handleKafkaRebalance)
	mux.HandleFunc("/api/v1/middleware/kafka/delete-records", s.handleKafkaDeleteRecords)
	mux.HandleFunc("/api/v1/middleware/kafka/delegation-token", s.handleKafkaDelegationToken)
	mux.HandleFunc("/api/v1/middleware/kafka/config", s.handleKafkaConfig)
	mux.HandleFunc("/api/v1/middleware/kafka/isr", s.handleKafkaISR)
	mux.HandleFunc("/api/v1/middleware/kafka/health-check", s.handleKafkaHealthCheck)
}
