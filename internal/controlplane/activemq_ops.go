// Package controlplane activemq_ops.go 实现 ActiveMQ 全面运维场景
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleActiveMQDeploy 处理 POST /api/v1/middleware/activemq/deploy
func (s *Server) handleActiveMQDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Mode     string `json:"mode"`    // single/master-slave/network
		Version  string `json:"version"` // e.g. 5.18.3
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	version := req.Version
	if version == "" {
		version = "5.18.3"
	}

	script := fmt.Sprintf(`wget -q https://archive.apache.org/dist/activemq/%s/apache-activemq-%s-bin.tar.gz -O /tmp/activemq.tgz && tar -xzf /tmp/activemq.tgz -C /opt/ && ln -sfn /opt/apache-activemq-%s /opt/activemq && /opt/activemq/bin/activemq start && echo "ActiveMQ %s deployed"`, version, version, version, version)
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "activemq_deploy")
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy"})
}

// handleActiveMQLifecycle 处理 POST /api/v1/middleware/activemq/lifecycle
func (s *Server) handleActiveMQLifecycle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // start/stop/restart/status
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "start":
		script = "/opt/activemq/bin/activemq start && echo 'ActiveMQ started'"
	case "stop":
		script = "/opt/activemq/bin/activemq stop && echo 'ActiveMQ stopped'"
	case "restart":
		script = "/opt/activemq/bin/activemq restart && echo 'ActiveMQ restarted'"
	case "status":
		script = "/opt/activemq/bin/activemq status 2>/dev/null || echo 'ActiveMQ not running'"
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "activemq_lifecycle_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleActiveMQMonitor 处理 POST /api/v1/middleware/activemq/monitor
func (s *Server) handleActiveMQMonitor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/queues/topics/connections
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `/opt/activemq/bin/activemq status 2>/dev/null || echo "ActiveMQ not running"`
	case "queues":
		script = `curl -s http://localhost:8161/api/jolokia/read/org.apache.activemq:brokerName=localhost,destinationType=Queue 2>/dev/null || echo "JMX not available"`
	case "topics":
		script = `curl -s http://localhost:8161/api/jolokia/read/org.apache.activemq:brokerName=localhost,destinationType=Topic 2>/dev/null || echo "JMX not available"`
	case "connections":
		script = `curl -s http://localhost:8161/api/jolokia/read/org.apache.activemq:brokerName=localhost,type=Broker/TotalConnectionsCount 2>/dev/null || echo "JMX not available"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "activemq_monitor_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerActiveMQExtendedRoutes 注册 ActiveMQ 扩展路由
func (s *Server) registerActiveMQExtendedRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/middleware/activemq/deploy", s.handleActiveMQDeploy)
	mux.HandleFunc("/api/v1/middleware/activemq/lifecycle", s.handleActiveMQLifecycle)
	mux.HandleFunc("/api/v1/middleware/activemq/monitor", s.handleActiveMQMonitor)
}
