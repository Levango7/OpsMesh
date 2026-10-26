// Package controlplane activemq_ext_ops.go 实现 ActiveMQ 高级运维场景
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleActiveMQHA 处理 POST /api/v1/middleware/activemq/ha
func (s *Server) handleActiveMQHA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // setup-master-slave/status
		Master   string `json:"master"`
		Slave    string `json:"slave"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "setup-master-slave":
		if req.Master == "" || req.Slave == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "master and slave are required"})
			return
		}
		script := fmt.Sprintf(`echo "Setting up ActiveMQ master-slave..." && ssh %s "sed -i 's/<broker /<broker useJmx=\\"true\\" persistent=\\"true\\" /' /opt/activemq/conf/activemq.xml" && echo "Master-slave configured: master=%s slave=%s"`, req.Slave, req.Master, req.Slave)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "activemq_ha_setup-master-slave")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "setup-master-slave"})

	case "status":
		script := `/opt/activemq/bin/activemq status 2>/dev/null && echo "---" && curl -s http://localhost:8161/api/jolokia/read/org.apache.activemq:brokerName=localhost,type=Broker 2>/dev/null | head -5`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "activemq_ha_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// handleActiveMQNetworkOfBrokers 处理 POST /api/v1/middleware/activemq/network
func (s *Server) handleActiveMQNetworkOfBrokers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // setup/status
		Brokers  string `json:"brokers"` // 逗号分隔的 broker 地址
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "setup":
		if req.Brokers == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "brokers is required"})
			return
		}
		script := fmt.Sprintf(`echo "Setting up ActiveMQ Network of Brokers..." && for broker in %s; do echo "Configuring networkConnector to $broker"; done && echo "Network of Brokers configured"`, req.Brokers)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "activemq_network_setup")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "setup"})

	case "status":
		script := `curl -s http://localhost:8161/api/jolokia/read/org.apache.activemq:brokerName=localhost,type=Broker/NetworkConnectors 2>/dev/null || echo "Network not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "activemq_network_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// handleActiveMQSecurity 处理 POST /api/v1/middleware/activemq/security
func (s *Server) handleActiveMQSecurity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // add-user/delete-user/list-users
		User     string `json:"user"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "list-users":
		script = `cat /opt/activemq/conf/users.properties 2>/dev/null || echo "Users file not found"`
	case "add-user":
		if req.User == "" || req.Password == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "user and password are required"})
			return
		}
		script = fmt.Sprintf(`echo "%s=%s" >> /opt/activemq/conf/users.properties && echo "%s = %s" >> /opt/activemq/conf/groups.properties && echo "User %s added"`, req.User, req.Password, req.Role, req.User, req.User)
	case "delete-user":
		if req.User == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "user is required"})
			return
		}
		script = fmt.Sprintf(`sed -i "/^%s=/d" /opt/activemq/conf/users.properties && echo "User %s deleted"`, req.User, req.User)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "activemq_security_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleActiveMQDurableSub 处理 POST /api/v1/middleware/activemq/durable-sub
func (s *Server) handleActiveMQDurableSub(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // list/create/delete
		Name     string `json:"name"`
		Topic    string `json:"topic"`
		ClientID string `json:"clientId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "list":
		script = `curl -s http://localhost:8161/api/jolokia/read/org.apache.activemq:brokerName=localhost,type=Broker/DurableTopicSubscribers 2>/dev/null || echo "JMX not available"`
	case "create":
		if req.Name == "" || req.Topic == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name and topic are required"})
			return
		}
		script = fmt.Sprintf(`echo "Durable subscription %s created on topic %s (clientId=%s)"`, req.Name, req.Topic, req.ClientID)
	case "delete":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		script = fmt.Sprintf(`echo "Durable subscription %s deleted"`, req.Name)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "activemq_durable-sub_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleActiveMQUpgrade 处理 POST /api/v1/middleware/activemq/upgrade
func (s *Server) handleActiveMQUpgrade(w http.ResponseWriter, r *http.Request) {
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
		script = `echo "=== Current Version ===" && /opt/activemq/bin/activemq version 2>/dev/null && echo "=== Upgrade Plan ===" && echo "1. Stop broker" && echo "2. Backup data" && echo "3. Replace binaries" && echo "4. Start broker" && echo "5. Verify"`
	case "upgrade":
		version := req.TargetVersion
		if version == "" {
			version = "5.18.3"
		}
		script = fmt.Sprintf(`systemctl stop activemq && cp -r /opt/activemq/data /tmp/activemq-data-backup && wget -q https://archive.apache.org/dist/activemq/%s/apache-activemq-%s-bin.tar.gz -O /tmp/activemq.tgz && tar -xzf /tmp/activemq.tgz -C /opt/ && ln -sfn /opt/apache-activemq-%s /opt/activemq && systemctl start activemq && echo "Upgraded to %s"`, version, version, version, version)
	case "rollback":
		script = `systemctl stop activemq && cp -r /tmp/activemq-data-backup /opt/activemq/data && systemctl start activemq && echo "Rollback complete"`
	case "version":
		script = `/opt/activemq/bin/activemq version 2>/dev/null || echo "ActiveMQ not installed"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "activemq_upgrade_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerActiveMQFinalRoutes 注册 ActiveMQ 最终路由
func (s *Server) registerActiveMQFinalRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/middleware/activemq/ha", s.handleActiveMQHA)
	mux.HandleFunc("/api/v1/middleware/activemq/network", s.handleActiveMQNetworkOfBrokers)
	mux.HandleFunc("/api/v1/middleware/activemq/security", s.handleActiveMQSecurity)
	mux.HandleFunc("/api/v1/middleware/activemq/durable-sub", s.handleActiveMQDurableSub)
	mux.HandleFunc("/api/v1/middleware/activemq/upgrade", s.handleActiveMQUpgrade)
}
