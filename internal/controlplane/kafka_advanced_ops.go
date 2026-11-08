// Package controlplane kafka_advanced_ops.go 实现 Kafka 高级运维场景：
// Share Groups (3.7+), MirrorMaker 2.0, Performance Tuning, OAuth/OIDC, FastDFS
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/pkg/shellquote"
)

// ============================================================================
// Kafka Share Groups (3.7+)
// ============================================================================

// handleKafkaShareGroup 处理 POST /api/v1/middleware/kafka/share-group
func (s *Server) handleKafkaShareGroup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // list/describe/create/delete
		Group    string `json:"group"`
		Topic    string `json:"topic"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "list":
		script := `kafka-consumer-groups.sh --bootstrap-server localhost:9092 --list 2>/dev/null | grep -i share || echo "No share groups found"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "share-group_list")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list"})

	case "describe":
		if req.Group == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "group is required"})
			return
		}
		// 注入面（批次 10）：group 名是**值** ⇒ 整体 Quote（取值语义不变）。
		script := fmt.Sprintf(`kafka-consumer-groups.sh --bootstrap-server localhost:9092 --describe --group %s --group-type share 2>/dev/null`, shellquote.Quote(req.Group))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "share-group_describe")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "describe"})

	case "create":
		if req.Group == "" || req.Topic == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "group and topic are required"})
			return
		}
		// 注入面（批次 10）：topic 与 group 都是**值** ⇒ 整体 Quote。
		script := fmt.Sprintf(`kafka-console-consumer.sh --bootstrap-server localhost:9092 --topic %s --group %s --group-type share --from-beginning --max-messages 1 --timeout-ms 5000 2>/dev/null && echo "Share group created"`, shellquote.Quote(req.Topic), shellquote.Quote(req.Group))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "share-group_create")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "create"})

	case "delete":
		if req.Group == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "group is required"})
			return
		}
		// 注入面（批次 10）：group 名是**值** ⇒ 整体 Quote。
		script := fmt.Sprintf(`kafka-consumer-groups.sh --bootstrap-server localhost:9092 --delete --group %s 2>/dev/null`, shellquote.Quote(req.Group))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "share-group_delete")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "delete"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// MirrorMaker 2.0 管理
// ============================================================================

// handleMirrorMaker 处理 POST /api/v1/middleware/kafka/mirror-maker
func (s *Server) handleMirrorMaker(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID      string `json:"deviceID"`
		Action        string `json:"action"`        // start/stop/status/list-configs
		SourceCluster string `json:"sourceCluster"` // 源集群 bootstrap
		TargetCluster string `json:"targetCluster"` // 目标集群 bootstrap
		Topics        string `json:"topics"`        // 逗号分隔的 topic 列表
		Config        string `json:"config"`        // MM2 config (properties 格式)
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "start":
		if req.SourceCluster == "" || req.TargetCluster == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "sourceCluster and targetCluster are required"})
			return
		}
		mm2Config := req.Config
		if mm2Config == "" {
			mm2Config = fmt.Sprintf(`clusters = source, target
source.bootstrap.servers = %s
target.bootstrap.servers = %s
source->target.enabled = true
source->target.topics = %s
replication.factor = 3
checkpoints.topic.replication.factor = 3
heartbeats.topic.replication.factor = 3
offset.syncs.topic.replication.factor = 3`, req.SourceCluster, req.TargetCluster, req.Topics)
		}
		// 注入面（批次 10）：**heredoc** 形态。内容在 `<< 'EOF'` 里本应字面，但载荷里
		// 出现**一行恰好等于 EOF** 就会让 heredoc **提前闭合** ⇒ 其后的内容被 shell
		// 当**命令**执行（已用真 sh 实测：标记文件真的落地）⇒ 整段交给 kafkaHeredoc。
		//
		// **顺带修一个既有 bug**：旧形态的 heredoc 写在 Go 的 **raw string** 里，其中的
		// `\n` 是**字面**反斜杠+n（不是换行）⇒ 分隔符被拼成 `EOFnclusters`，
		// heredoc 永不闭合 ⇒ 该功能**今天就跑不起来**（实测：cat 报 "No such file or
		// directory"、配置文件根本没生成）。本批改用真换行 ⇒ 功能首次可用；
		// 相应的「提前闭合」面随之显性化，已由 kafkaHeredoc 收口。
		heredoc, err := kafkaHeredoc("/tmp/mm2.properties", mm2Config)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		script := heredoc + "\nconnect-mirror-maker.sh /tmp/mm2.properties &"
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "mirror-maker_start")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "start"})

	case "stop":
		script := `pkill -f connect-mirror-maker 2>/dev/null || echo "MirrorMaker not running"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "mirror-maker_stop")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "stop"})

	case "status":
		script := `pgrep -f connect-mirror-maker > /dev/null && echo "MirrorMaker running" || echo "MirrorMaker not running"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "mirror-maker_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	case "list-configs":
		script := `ls -la /tmp/mm2*.properties 2>/dev/null || echo "No MM2 configs found"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "mirror-maker_list-configs")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list-configs"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// Kafka Performance Tuning
// ============================================================================

// handleKafkaTuning 处理 POST /api/v1/middleware/kafka/tuning
func (s *Server) handleKafkaTuning(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // analyze/apply/recommend
		Target   string `json:"target"` // broker/topic/producer/consumer
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "analyze":
		script := `echo "=== Broker Config ===" && kafka-configs.sh --bootstrap-server localhost:9092 --entity-type brokers --entity-name 0 --describe 2>/dev/null && echo "=== Topic Config ===" && kafka-topics.sh --bootstrap-server localhost:9092 --describe 2>/dev/null | head -20 && echo "=== Consumer Lag ===" && kafka-consumer-groups.sh --bootstrap-server localhost:9092 --list 2>/dev/null`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "tuning_analyze")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "analyze"})

	case "recommend":
		script := `echo "=== Performance Recommendations ===" && echo "1. num.network.threads: 8 (default 3)" && echo "2. num.io.threads: 16 (default 8)" && echo "3. socket.send.buffer.bytes: 102400" && echo "4. socket.receive.buffer.bytes: 102400" && echo "5. socket.request.max.bytes: 104857600" && echo "6. num.replica.fetchers: 4 (default 1)" && echo "7. replica.fetch.max.bytes: 1048576" && echo "8. message.max.bytes: 1048576" && echo "9. compression.type: lz4" && echo "10. log.segment.bytes: 1073741824"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "tuning_recommend")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "recommend"})

	case "apply":
		script := `kafka-configs.sh --bootstrap-server localhost:9092 --entity-type brokers --entity-name 0 --alter --add-config num.network.threads=8,num.io.threads=16,socket.send.buffer.bytes=102400,socket.receive.buffer.bytes=102400 2>/dev/null && echo "Performance tuning applied"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "tuning_apply")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "apply"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// OAuth/OIDC (Keycloak) 管理
// ============================================================================

// handleOAuth 处理 POST /api/v1/auth/oauth
func (s *Server) handleOAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // deploy/status/configure/create-realm/create-client
		Mode     string `json:"mode"`   // single/cluster
		Version  string `json:"version"`
		Realm    string `json:"realm"`
		ClientID string `json:"clientId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "deploy":
		version := req.Version
		if version == "" {
			version = "23.0"
		}
		// 注入面（批次 10）：version 落在**镜像 tag**位置 ⇒ 整体 Quote
		//（docker 收到的 tag 与旧形态逐字相同）。
		script := fmt.Sprintf(`docker run -d --name keycloak -p 8080:8080 -e KEYCLOAK_ADMIN=admin -e KEYCLOAK_ADMIN_PASSWORD=admin quay.io/keycloak/keycloak:%s start-dev 2>/dev/null && echo "Keycloak deployed"`, shellquote.Quote(version))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "oauth_deploy")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy"})

	case "status":
		script := `docker ps --filter name=keycloak --format "{{.Status}}" 2>/dev/null || echo "Keycloak not running"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "oauth_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	case "create-realm":
		if req.Realm == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "realm is required"})
			return
		}
		// 注入面（批次 10）：realm 被裸拼进 **JSON 字面量**（`-d '{"realm":"%s"}'`）⇒
		// 字段里的 `"` 会**破坏 JSON 结构**、`'` 会**破 shell 单引号段** ⇒
		// 改由 Go 层 map + json.Marshal 构造 JSON，再整体 Quote。
		// 非法 realm 名 ⇒ 400 拒收（旧形态是把垃圾原样发给 Keycloak，由它报错）。
		realm, err := kafkaResource(req.Realm)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		payload, err := opsJSONPayload(map[string]any{"realm": realm, "enabled": true})
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid realm payload: " + err.Error()})
			return
		}
		script := fmt.Sprintf(`curl -s -X POST -H "Content-Type: application/json" -H "Authorization: Bearer $(curl -s -X POST http://localhost:8080/realms/master/protocol/openid-connect/token -d "client_id=admin-cli" -d "username=admin" -d "password=admin" -d "grant_type=password" | jq -r .access_token)" --data %s %s`, payload, opsURL("http://localhost:8080", "admin/realms"))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "oauth_create-realm")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "create-realm"})

	case "create-client":
		if req.Realm == "" || req.ClientID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "realm and clientId are required"})
			return
		}
		// 注入面（批次 10）：clientId 裸拼进 **JSON 字面量**、realm 裸拼进 **URL 段**
		// ⇒ JSON 由 Go 层构造后整体 Quote；URL 在 Go 层拼完整后**整体** Quote
		//（不能把 Quote 结果塞回双引号段：双引号内单引号是字面量 ⇒ URL 会带 %27）。
		clientID, err := kafkaResource(req.ClientID)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		realm, err := kafkaResource(req.Realm)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		payload, err := opsJSONPayload(map[string]any{
			"clientId":     clientID,
			"enabled":      true,
			"protocol":     "openid-connect",
			"publicClient": true,
		})
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid client payload: " + err.Error()})
			return
		}
		script := fmt.Sprintf(`curl -s -X POST -H "Content-Type: application/json" -H "Authorization: Bearer $(curl -s -X POST http://localhost:8080/realms/master/protocol/openid-connect/token -d "client_id=admin-cli" -d "username=admin" -d "password=admin" -d "grant_type=password" | jq -r .access_token)" --data %s %s`, payload, opsURL("http://localhost:8080", "admin/realms/"+realm+"/clients"))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "oauth_create-client")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "create-client"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// 路由注册 (追加)
// ============================================================================

// registerKafkaAdvancedRoutes 注册 Kafka 高级路由
func (s *Server) registerKafkaAdvancedRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/middleware/kafka/share-group", s.handleKafkaShareGroup)
	mux.HandleFunc("/api/v1/middleware/kafka/mirror-maker", s.handleMirrorMaker)
	mux.HandleFunc("/api/v1/middleware/kafka/tuning", s.handleKafkaTuning)
	mux.HandleFunc("/api/v1/auth/oauth", s.handleOAuth)
}
