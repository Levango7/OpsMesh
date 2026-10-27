// Package controlplane rocketmq_ops.go 实现 RocketMQ 全面运维场景
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleRocketMQDeploy 处理 POST /api/v1/middleware/rocketmq/deploy
func (s *Server) handleRocketMQDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Mode     string `json:"mode"`    // single/cluster
		Version  string `json:"version"` // e.g. 5.2.0
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	version := req.Version
	if version == "" {
		version = "5.2.0"
	}

	script := fmt.Sprintf(`wget -q https://archive.apache.org/dist/rocketmq/%s/rocketmq-all-%s-bin-release.zip -O /tmp/rocketmq.zip && unzip -q /tmp/rocketmq.zip -d /opt/ && ln -sfn /opt/rocketmq-all-%s-bin-release /opt/rocketmq && echo "RocketMQ %s deployed"`, version, version, version, version)
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rocketmq_deploy")
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy"})
}

// handleRocketMQLifecycle 处理 POST /api/v1/middleware/rocketmq/lifecycle
func (s *Server) handleRocketMQLifecycle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // start-namesrv/start-broker/stop/status
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "start-namesrv":
		script = "nohup /opt/rocketmq/bin/mqnamesrv > /tmp/namesrv.log 2>&1 & sleep 3 && echo 'NameServer started'"
	case "start-broker":
		script = "nohup /opt/rocketmq/bin/mqbroker -n localhost:9876 > /tmp/broker.log 2>&1 & sleep 5 && echo 'Broker started'"
	case "stop":
		script = "/opt/rocketmq/bin/mqshutdown broker && /opt/rocketmq/bin/mqshutdown namesrv && echo 'RocketMQ stopped'"
	case "status":
		script = "jps | grep -E 'NamesrvStartup|BrokerStartup' || echo 'RocketMQ not running'"
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rocketmq_lifecycle_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleRocketMQTopic 处理 POST /api/v1/middleware/rocketmq/topic
func (s *Server) handleRocketMQTopic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // list/create/delete
		Topic    string `json:"topic"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "list":
		script = `/opt/rocketmq/bin/mqadmin topicList -n localhost:9876 2>/dev/null || echo "RocketMQ not available"`
	case "create":
		if req.Topic == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "topic is required"})
			return
		}
		script = fmt.Sprintf(`/opt/rocketmq/bin/mqadmin updateTopic -n localhost:9876 -t %s -c DefaultCluster 2>/dev/null && echo "Topic %s created"`, req.Topic, req.Topic)
	case "delete":
		if req.Topic == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "topic is required"})
			return
		}
		script = fmt.Sprintf(`/opt/rocketmq/bin/mqadmin deleteTopic -n localhost:9876 -t %s 2>/dev/null && echo "Topic %s deleted"`, req.Topic, req.Topic)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rocketmq_topic_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleRocketMQConsumerGroup 处理 POST /api/v1/middleware/rocketmq/consumer-group
func (s *Server) handleRocketMQConsumerGroup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // list/describe
		Group    string `json:"group"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "list":
		script = `/opt/rocketmq/bin/mqadmin consumerProgress -n localhost:9876 2>/dev/null | head -20 || echo "RocketMQ not available"`
	case "describe":
		if req.Group == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "group is required"})
			return
		}
		script = fmt.Sprintf(`/opt/rocketmq/bin/mqadmin consumerProgress -n localhost:9876 -g %s 2>/dev/null`, req.Group)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rocketmq_consumer-group_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleRocketMQACL 处理 POST /api/v1/middleware/rocketmq/acl
func (s *Server) handleRocketMQACL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // list/create/delete
		Subject  string `json:"subject"`
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
		script = `/opt/rocketmq/bin/mqadmin getAclConfig -n localhost:9876 2>/dev/null || echo "ACL not available"`
	case "create":
		if req.Subject == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "subject is required"})
			return
		}
		script = fmt.Sprintf(`/opt/rocketmq/bin/mqadmin updateAclConfig -n localhost:9876 -s %s -t %s -g %s 2>/dev/null && echo "ACL created"`, req.Subject, req.Topic, req.Group)
	case "delete":
		if req.Subject == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "subject is required"})
			return
		}
		script = fmt.Sprintf(`/opt/rocketmq/bin/mqadmin deleteAclConfig -n localhost:9876 -s %s 2>/dev/null && echo "ACL deleted"`, req.Subject)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rocketmq_acl_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}
