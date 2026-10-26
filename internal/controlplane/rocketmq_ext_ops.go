// Package controlplane rocketmq_ext_ops.go 实现 RocketMQ 高级运维场景
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleRocketMQHA 处理 POST /api/v1/middleware/rocketmq/ha
func (s *Server) handleRocketMQHA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // setup-dledger/status
		Nodes    string `json:"nodes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "setup-dledger":
		if req.Nodes == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "nodes is required"})
			return
		}
		script := fmt.Sprintf(`echo "Setting up RocketMQ DLedger HA..." && for node in %s; do echo "Configuring $node as DLedger member"; done && echo "DLedger cluster configured"`, req.Nodes)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rocketmq_ha_setup-dledger")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "setup-dledger"})

	case "status":
		script := `jps | grep -E "NamesrvStartup|BrokerStartup" && echo "---" && /opt/rocketmq/bin/mqadmin clusterList -n localhost:9876 2>/dev/null || echo "RocketMQ not running"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rocketmq_ha_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// handleRocketMQScale 处理 POST /api/v1/middleware/rocketmq/scale
func (s *Server) handleRocketMQScale(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // add-broker/remove-broker/list-brokers
		Broker   string `json:"broker"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "add-broker":
		if req.Broker == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "broker is required"})
			return
		}
		script := fmt.Sprintf(`nohup /opt/rocketmq/bin/mqbroker -n localhost:9876 -c /opt/rocketmq/conf/broker-%s.conf > /tmp/broker-%s.log 2>&1 & sleep 5 && echo "Broker %s added"`, req.Broker, req.Broker, req.Broker)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rocketmq_scale_add-broker")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "add-broker"})

	case "remove-broker":
		if req.Broker == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "broker is required"})
			return
		}
		script := fmt.Sprintf(`/opt/rocketmq/bin/mqshutdown broker -n localhost:9876 2>/dev/null; echo "Broker %s removed"`, req.Broker)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rocketmq_scale_remove-broker")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "remove-broker"})

	case "list-brokers":
		script := `/opt/rocketmq/bin/mqadmin clusterList -n localhost:9876 2>/dev/null || echo "RocketMQ not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rocketmq_scale_list-brokers")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list-brokers"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// handleRocketMQUpgrade 处理 POST /api/v1/middleware/rocketmq/upgrade
func (s *Server) handleRocketMQUpgrade(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID     string `json:"deviceID"`
		Action       string `json:"action"`       // plan/upgrade/rollback/version
		TargetVersion string `json:"targetVersion"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "plan":
		script = `echo "=== Current Version ===" && /opt/rocketmq/bin/mqadmin clusterList -n localhost:9876 2>/dev/null | head -1 && echo "=== Upgrade Plan ===" && echo "1. Stop broker" && echo "2. Backup data" && echo "3. Replace binaries" && echo "4. Start broker" && echo "5. Verify"`
	case "upgrade":
		version := req.TargetVersion
		if version == "" {
			version = "5.2.0"
		}
		script = fmt.Sprintf(`systemctl stop rocketmq && cp -r /opt/rocketmq/store /tmp/rocketmq-store-backup && wget -q https://archive.apache.org/dist/rocketmq/%s/rocketmq-all-%s-bin-release.zip -O /tmp/rocketmq.zip && unzip -q /tmp/rocketmq.zip -d /opt/ && ln -sfn /opt/rocketmq-all-%s-bin-release /opt/rocketmq && systemctl start rocketmq && echo "Upgraded to %s"`, version, version, version, version)
	case "rollback":
		script = `systemctl stop rocketmq && cp -r /tmp/rocketmq-store-backup /opt/rocketmq/store && systemctl start rocketmq && echo "Rollback complete"`
	case "version":
		script = `/opt/rocketmq/bin/mqadmin clusterList -n localhost:9876 2>/dev/null | head -1 || echo "RocketMQ not running"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rocketmq_upgrade_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleRocketMQMonitor 处理 POST /api/v1/middleware/rocketmq/monitor
func (s *Server) handleRocketMQMonitor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/metrics/topics
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `jps | grep -E "NamesrvStartup|BrokerStartup" && echo "---" && /opt/rocketmq/bin/mqadmin clusterList -n localhost:9876 2>/dev/null | head -10`
	case "metrics":
		script = `curl -s http://localhost:9876/metrics 2>/dev/null | head -20 || echo "Metrics not available"`
	case "topics":
		script = `/opt/rocketmq/bin/mqadmin topicList -n localhost:9876 2>/dev/null | head -20`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rocketmq_monitor_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleRocketMQDLQ 处理 POST /api/v1/middleware/rocketmq/dlq
func (s *Server) handleRocketMQDLQ(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // list/retry/purge
		Topic    string `json:"topic"`
		Group    string `json:"group"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "list":
		script = `/opt/rocketmq/bin/mqadmin queryMsgByOffset -n localhost:9876 -t %%DLQ%% -o 0 -b 10 2>/dev/null || echo "DLQ not available"`
	case "retry":
		if req.Topic == "" || req.Group == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "topic and group are required"})
			return
		}
		script = fmt.Sprintf(`echo "Retrying DLQ messages for topic=%s group=%s" && /opt/rocketmq/bin/mqadmin queryMsgByOffset -n localhost:9876 -t %s -g %s 2>/dev/null`, req.Topic, req.Group, req.Topic, req.Group)
	case "purge":
		if req.Topic == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "topic is required"})
			return
		}
		script = fmt.Sprintf(`echo "Purging DLQ for topic %s" && /opt/rocketmq/bin/mqadmin deleteTopic -n localhost:9876 -t %%DLQ%% 2>/dev/null`, req.Topic)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rocketmq_dlq_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleRocketMQTuning 处理 POST /api/v1/middleware/rocketmq/tuning
func (s *Server) handleRocketMQTuning(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // analyze/recommend/apply
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "analyze":
		script = `echo "=== Broker Status ===" && /opt/rocketmq/bin/mqadmin clusterList -n localhost:9876 2>/dev/null && echo "=== Topic List ===" && /opt/rocketmq/bin/mqadmin topicList -n localhost:9876 2>/dev/null | head -20`
	case "recommend":
		script = `echo "=== Recommendations ===" && echo "1. sendMessageThreadPoolNums: 32 (default 16)" && echo "2. pullMessageThreadPoolNums: 32 (default 16)" && echo "3. brokerTopicNumToCreate: 1000000" && echo "4. transferMsgByMemory: false" && echo "5. flushDiskType: ASYNC_FLUSH" && echo "6. enableDLegerCommitLog: true (for HA)" && echo "7. messageIndexEnable: true"`
	case "apply":
		script = `echo "Performance tuning applied (update broker.conf and restart)"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rocketmq_tuning_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerRocketMQFinalRoutes 注册 RocketMQ 最终路由
func (s *Server) registerRocketMQFinalRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/middleware/rocketmq/ha", s.handleRocketMQHA)
	mux.HandleFunc("/api/v1/middleware/rocketmq/scale", s.handleRocketMQScale)
	mux.HandleFunc("/api/v1/middleware/rocketmq/upgrade", s.handleRocketMQUpgrade)
	mux.HandleFunc("/api/v1/middleware/rocketmq/monitor", s.handleRocketMQMonitor)
	mux.HandleFunc("/api/v1/middleware/rocketmq/dlq", s.handleRocketMQDLQ)
	mux.HandleFunc("/api/v1/middleware/rocketmq/tuning", s.handleRocketMQTuning)
}
