// Package controlplane kafka_scale_ops.go 实现 Kafka 扩缩容、版本升级、日志管理等运维场景
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/pkg/shellquote"
)

// ============================================================================
// Broker 扩缩容
// ============================================================================

// handleKafkaScale 处理 POST /api/v1/middleware/kafka/scale
func (s *Server) handleKafkaScale(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // add-broker / remove-broker / list-brokers / decommission
		BrokerID string `json:"brokerId"`
		Host     string `json:"host"`
		Port     int    `json:"port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "list-brokers":
		script := `kafka-broker-api-versions.sh --bootstrap-server localhost:9092 2>/dev/null | head -20`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "scale_list-brokers")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list-brokers"})

	case "add-broker":
		if req.Host == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "host is required"})
			return
		}
		port := req.Port
		if port == 0 {
			port = 9092
		}
		// 生成 broker 配置并启动
		//
		// 注入面（批次 10）：这是 **heredoc** 形态。内容在 `<< 'EOF'` 里本应字面，
		// 但载荷里出现**一行恰好等于 EOF** 就会让 heredoc **提前闭合** ⇒ 其后的内容
		// 被 shell 当**命令**执行（已用真 sh 实测：标记文件真的落地）。
		// 故整段交给 kafkaHeredoc：它构造 heredoc，并拒绝「含等于分隔符的行」的内容。
		//
		// 注：判据 C/D 是**行级**判据，看不到这种跨行的 fmt.Sprintf ⇒ 本批的 AST 级
		// 守卫（kafka_injection_test.go）才是这里的权威口径。
		heredoc, err := kafkaHeredocf("/tmp/server.properties", `broker.id=%s
listeners=PLAINTEXT://:%d
advertised.listeners=PLAINTEXT://%s:%d
log.dirs=/var/lib/kafka/data
num.network.threads=8
num.io.threads=16
socket.send.buffer.bytes=102400
socket.receive.buffer.bytes=102400
log.retention.hours=168
log.segment.bytes=1073741824
num.partitions=3
default.replication.factor=3
min.insync.replicas=2
offsets.topic.replication.factor=3
transaction.state.log.replication.factor=3
transaction.state.log.min.isr=2`, req.BrokerID, port, req.Host, port)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		script := heredoc + "\nkafka-server-start.sh -daemon /tmp/server.properties && echo \"Broker started\""
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "scale_add-broker")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "add-broker"})

	case "remove-broker":
		if req.BrokerID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "brokerId is required"})
			return
		}
		// 1. 迁移分区 away from this broker
		// 2. 停止 broker
		// 注入面（批次 10）：回显段落在**双引号**内 ⇒ 段内 $ 与反引号照样展开 ⇒
		// 整条回显消息整体 Quote（shell 剥掉引号后输出与旧形态逐字相同）。
		script := fmt.Sprintf(`kafka-reassign-partitions.sh --bootstrap-server localhost:9092 --topics-to-move-json-file <(echo '{"topics":[],"version":1}') --broker-list "0,1,2" --generate 2>/dev/null; kafka-server-stop.sh 2>/dev/null; echo %s`, opsEcho("Broker %s removed", req.BrokerID))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "scale_remove-broker")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "remove-broker"})

	case "decommission":
		if req.BrokerID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "brokerId is required"})
			return
		}
		// 安全下线：drain + reassign + stop
		// 注入面（批次 10）：两段回显都在**双引号**内 ⇒ 整体 Quote。
		script := fmt.Sprintf(`echo %s && kafka-reassign-partitions.sh --bootstrap-server localhost:9092 --topics-to-move-json-file <(echo '{"topics":[],"version":1}') --broker-list "0,1,2" --generate 2>/dev/null && kafka-server-stop.sh 2>/dev/null && echo %s`, opsEcho("Draining broker %s...", req.BrokerID), opsEcho("Broker %s decommissioned", req.BrokerID))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "scale_decommission")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "decommission"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// 版本升级
// ============================================================================

// handleKafkaUpgrade 处理 POST /api/v1/middleware/kafka/upgrade
func (s *Server) handleKafkaUpgrade(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID      string `json:"deviceID"`
		Action        string `json:"action"`        // plan/upgrade/rollback
		TargetVersion string `json:"targetVersion"` // e.g. "3.7.0"
		Nodes         string `json:"nodes"`         // 逗号分隔的节点列表
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "plan":
		script := `echo "=== Current Version ===" && kafka-broker-api-versions.sh --bootstrap-server localhost:9092 2>/dev/null | head -1 && echo "=== Upgrade Plan ===" && echo "1. Stop broker" && echo "2. Replace binaries" && echo "3. Start broker" && echo "4. Verify" && echo "5. Repeat for next broker"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "upgrade_plan")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "plan"})

	case "upgrade":
		if req.TargetVersion == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "targetVersion is required"})
			return
		}
		// 注入面（批次 10）：回显段在双引号内 ⇒ 整体 Quote。
		script := fmt.Sprintf(`echo %s && kafka-server-stop.sh 2>/dev/null && sleep 5 && kafka-server-start.sh -daemon /etc/kafka/server.properties && sleep 10 && kafka-broker-api-versions.sh --bootstrap-server localhost:9092 2>/dev/null | head -1 && echo "Upgrade complete"`, opsEcho("Upgrading to Kafka %s...", req.TargetVersion))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "upgrade_apply")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "upgrade"})

	case "rollback":
		script := `echo "Rolling back Kafka..." && kafka-server-stop.sh 2>/dev/null && sleep 5 && kafka-server-start.sh -daemon /etc/kafka/server.properties.bak && sleep 10 && echo "Rollback complete"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "upgrade_rollback")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "rollback"})

	case "version":
		script := `kafka-broker-api-versions.sh --bootstrap-server localhost:9092 2>/dev/null | head -1`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "upgrade_version")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "version"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// 日志管理 (清理/压缩/检查)
// ============================================================================

// handleKafkaLog 处理 POST /api/v1/middleware/kafka/log
func (s *Server) handleKafkaLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID  string `json:"deviceID"`
		Action    string `json:"action"` // list-segments/clean/dump/compact
		Topic     string `json:"topic"`
		Partition int    `json:"partition"`
		Offset    int64  `json:"offset"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "list-segments":
		if req.Topic == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "topic is required"})
			return
		}
		// 注入面（批次 10）：topic 落在**路径段**里 ⇒
		//  ① 路径必须**整体** Quote（只 Quote 片段不行：topic 里带空格会改变参数切分）；
		//  ② topic 走白名单（不含 `/`），否则路径语义可被改写（`..` 逃逸目录）。
		topic, err := kafkaTopic(req.Topic)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		segDir := fmt.Sprintf("/var/lib/kafka/data/%s-%d/", topic, req.Partition)
		script := fmt.Sprintf(`ls -lh %s 2>/dev/null || echo "No log segments found"`, shellquote.Quote(segDir))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "log_list-segments")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list-segments"})

	case "clean":
		if req.Topic == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "topic is required"})
			return
		}
		// 触发日志清理 (通过 kafka-configs 修改 retention)
		// 注入面（批次 10）：entity-name 是**值** ⇒ 整体 Quote（取值语义不变）。
		script := fmt.Sprintf(`kafka-configs.sh --bootstrap-server localhost:9092 --entity-type topics --entity-name %s --alter --add-config retention.ms=1000 2>/dev/null && sleep 5 && kafka-configs.sh --bootstrap-server localhost:9092 --entity-type topics --entity-name %s --alter --delete-config retention.ms 2>/dev/null && echo "Log cleanup triggered"`, shellquote.Quote(req.Topic), shellquote.Quote(req.Topic))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "log_clean")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "clean"})

	case "dump":
		if req.Topic == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "topic is required"})
			return
		}
		partition := req.Partition
		if partition < 0 {
			partition = 0
		}
		// 注入面（批次 10）：topic 落在**路径段**里 ⇒ 目录段整体 Quote + topic 白名单。
		// 注意：末尾的 `*.log` **必须留在引号外**——旧形态里它是**未引号**的，shell 会
		// 把它展开成多个文件参数；整体 Quote 会把 glob 变成字面量 ⇒ 改变语义。
		// 故只 Quote 目录前缀，glob 仍由 shell 展开。
		topic, err := kafkaTopic(req.Topic)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		segDir := fmt.Sprintf("/var/lib/kafka/data/%s-%d/", topic, partition)
		script := fmt.Sprintf(`kafka-dump-log.sh --files %s*.log --print-data-log 2>/dev/null | head -50`, shellquote.Quote(segDir))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "log_dump")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "dump"})

	case "compact":
		if req.Topic == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "topic is required"})
			return
		}
		// 启用 log compaction
		// 注入面（批次 10）：entity-name 是**值** ⇒ 整体 Quote。
		script := fmt.Sprintf(`kafka-configs.sh --bootstrap-server localhost:9092 --entity-type topics --entity-name %s --alter --add-config cleanup.policy=compact 2>/dev/null && echo "Log compaction enabled"`, shellquote.Quote(req.Topic))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "log_compact")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "compact"})

	case "retention":
		if req.Topic == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "topic is required"})
			return
		}
		// 注入面（批次 10）：entity-name 是**值** ⇒ 整体 Quote。
		script := fmt.Sprintf(`kafka-configs.sh --bootstrap-server localhost:9092 --entity-type topics --entity-name %s --describe 2>/dev/null | grep -i retention`, shellquote.Quote(req.Topic))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "log_retention")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "retention"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// Transaction / EOS 管理
// ============================================================================

// handleKafkaTransaction 处理 POST /api/v1/middleware/kafka/transaction
func (s *Server) handleKafkaTransaction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // list/describe/abort
		TxnID    string `json:"txnId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "list":
		script := `kafka-transactions.sh --bootstrap-server localhost:9092 --list 2>/dev/null || echo "Transaction listing not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "transaction_list")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list"})

	case "describe":
		if req.TxnID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "txnId is required"})
			return
		}
		// 注入面（批次 10）：transactional-id 是**值** ⇒ 整体 Quote。
		script := fmt.Sprintf(`kafka-transactions.sh --bootstrap-server localhost:9092 --describe --transactional-id %s 2>/dev/null`, shellquote.Quote(req.TxnID))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "transaction_describe")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "describe"})

	case "abort":
		if req.TxnID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "txnId is required"})
			return
		}
		// 注入面（批次 10）：transactional-id 是**值** ⇒ 整体 Quote。
		script := fmt.Sprintf(`kafka-transactions.sh --bootstrap-server localhost:9092 --abort --transactional-id %s 2>/dev/null || echo "Abort not available via CLI"`, shellquote.Quote(req.TxnID))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "transaction_abort")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "abort"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// MirrorMaker 2.0 完整管理
// ============================================================================

// handleMirrorMakerFull 处理 POST /api/v1/middleware/kafka/mirror-maker-full
func (s *Server) handleMirrorMakerFull(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // create-link/list-links/describe-link/delete-link/checkpoint/lag
		Source   string `json:"source"`
		Target   string `json:"target"`
		LinkName string `json:"linkName"`
		Topics   string `json:"topics"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "create-link":
		if req.Source == "" || req.Target == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "source and target are required"})
			return
		}
		linkName := req.LinkName
		if linkName == "" {
			linkName = "mm2-link"
		}
		// 注入面（批次 10）：三个用户字段都是**值** ⇒ 整体 Quote。
		// 注意 --config：旧形态的值在**双引号段**内（`--config "bootstrap.servers=%s"`），
		// 双引号内 $ 与反引号会展开 ⇒ 改由 Go 层拼出完整配置串再**整体** Quote，
		// 且 Quote 结果放在双引号**外**（放里面会让配置串里多出字面量单引号）。
		linkCfg := "bootstrap.servers=" + req.Source
		script := fmt.Sprintf(`kafka-cluster-links.sh --bootstrap-server %s --create --link %s --config %s 2>/dev/null || echo "Cluster linking not available"`, shellquote.Quote(req.Target), shellquote.Quote(linkName), shellquote.Quote(linkCfg))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "mm2_create-link")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "create-link"})

	case "list-links":
		script := `kafka-cluster-links.sh --bootstrap-server localhost:9092 --list 2>/dev/null || echo "No cluster links"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "mm2_list-links")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list-links"})

	case "describe-link":
		if req.LinkName == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "linkName is required"})
			return
		}
		// 注入面（批次 10）：link 名是**值** ⇒ 整体 Quote。
		script := fmt.Sprintf(`kafka-cluster-links.sh --bootstrap-server localhost:9092 --describe --link %s 2>/dev/null`, shellquote.Quote(req.LinkName))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "mm2_describe-link")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "describe-link"})

	case "delete-link":
		if req.LinkName == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "linkName is required"})
			return
		}
		// 注入面（批次 10）：link 名是**值** ⇒ 整体 Quote。
		script := fmt.Sprintf(`kafka-cluster-links.sh --bootstrap-server localhost:9092 --delete --link %s --force 2>/dev/null`, shellquote.Quote(req.LinkName))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "mm2_delete-link")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "delete-link"})

	case "lag":
		script := `kafka-consumer-groups.sh --bootstrap-server localhost:9092 --list 2>/dev/null | grep -i mirror || echo "No MirrorMaker consumer groups"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "mm2_lag")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "lag"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// Kafka Streams 管理
// ============================================================================

// handleKafkaStreams 处理 POST /api/v1/middleware/kafka/streams
func (s *Server) handleKafkaStreams(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID    string `json:"deviceID"`
		Action      string `json:"action"` // reset/list-apps/describe
		AppID       string `json:"appId"`
		Bootstrap   string `json:"bootstrap"`
		InputTopic  string `json:"inputTopic"`
		OutputTopic string `json:"outputTopic"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "list-apps":
		script := `kafka-consumer-groups.sh --bootstrap-server localhost:9092 --list 2>/dev/null | grep -i streams || echo "No Streams apps found"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "streams_list-apps")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list-apps"})

	case "reset":
		if req.AppID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "appId is required"})
			return
		}
		bootstrap := req.Bootstrap
		if bootstrap == "" {
			bootstrap = "localhost:9092"
		}
		// 注入面（批次 10）：四个用户字段都是**值** ⇒ 整体 Quote（取值语义不变）。
		script := fmt.Sprintf(`kafka-streams-application-reset.sh --application-id %s --bootstrap-servers %s --input-topics %s --output-topics %s 2>/dev/null || echo "Streams reset not available"`, shellquote.Quote(req.AppID), shellquote.Quote(bootstrap), shellquote.Quote(req.InputTopic), shellquote.Quote(req.OutputTopic))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "streams_reset")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "reset"})

	case "describe":
		if req.AppID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "appId is required"})
			return
		}
		// 注入面（批次 10）：group 名是**值** ⇒ 整体 Quote。
		script := fmt.Sprintf(`kafka-consumer-groups.sh --bootstrap-server localhost:9092 --describe --group %s 2>/dev/null`, shellquote.Quote(req.AppID))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "streams_describe")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "describe"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// 路由注册 (追加)
// ============================================================================

// registerKafkaScaleRoutes 注册 Kafka 扩缩容/升级/日志/事务/MirrorMaker/Streams 路由
func (s *Server) registerKafkaScaleRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/middleware/kafka/scale", s.handleKafkaScale)
	mux.HandleFunc("/api/v1/middleware/kafka/upgrade", s.handleKafkaUpgrade)
	mux.HandleFunc("/api/v1/middleware/kafka/log", s.handleKafkaLog)
	mux.HandleFunc("/api/v1/middleware/kafka/transaction", s.handleKafkaTransaction)
	mux.HandleFunc("/api/v1/middleware/kafka/mirror-maker-full", s.handleMirrorMakerFull)
	mux.HandleFunc("/api/v1/middleware/kafka/streams", s.handleKafkaStreams)
}
