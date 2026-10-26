// Package controlplane rabbitmq_ext_ops.go 实现 RabbitMQ 高级运维场景
// HA clustering, scaling, version upgrade, backup/restore, performance tuning, security, observability
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// ============================================================================
// RabbitMQ HA 集群 (自动化)
// ============================================================================

// handleRabbitMQHA 处理 POST /api/v1/middleware/rabbitmq/ha
func (s *Server) handleRabbitMQHA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // setup/status/join/leave
		Node     string `json:"node"`
		Nodes    string `json:"nodes"` // 逗号分隔
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "setup":
		if req.Nodes == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "nodes is required"})
			return
		}
		script := fmt.Sprintf(`echo "Setting up RabbitMQ HA cluster..." && rabbitmqctl stop_app && for node in %s; do ssh $node "rabbitmqctl stop_app && rabbitmqctl join_cluster rabbit@$(hostname -s) && rabbitmqctl start_app" ; done && rabbitmqctl start_app && echo "HA cluster setup complete"`, req.Nodes)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rabbitmq_ha_setup")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "setup"})

	case "status":
		script := `rabbitmqctl cluster_status 2>/dev/null && echo "---" && rabbitmqctl list_nodes 2>/dev/null`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rabbitmq_ha_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	case "join":
		if req.Node == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "node is required"})
			return
		}
		script := fmt.Sprintf(`rabbitmqctl stop_app && rabbitmqctl join_cluster %s && rabbitmqctl start_app && echo "Joined cluster: %s"`, req.Node, req.Node)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rabbitmq_ha_join")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "join"})

	case "leave":
		script := `rabbitmqctl stop_app && rabbitmqctl reset && rabbitmqctl start_app && echo "Left cluster"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rabbitmq_ha_leave")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "leave"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// RabbitMQ 扩缩容
// ============================================================================

// handleRabbitMQScale 处理 POST /api/v1/middleware/rabbitmq/scale
func (s *Server) handleRabbitMQScale(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // add-node/remove-node/list-nodes
		Node     string `json:"node"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "add-node":
		if req.Node == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "node is required"})
			return
		}
		script := fmt.Sprintf(`ssh %s "apt-get install -y rabbitmq-server && systemctl enable rabbitmq-server && systemctl start rabbitmq-server" && rabbitmqctl stop_app && rabbitmqctl join_cluster rabbit@%s && rabbitmqctl start_app && echo "Node %s added"`, req.Node, req.Node, req.Node)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rabbitmq_scale_add-node")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "add-node"})

	case "remove-node":
		if req.Node == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "node is required"})
			return
		}
		script := fmt.Sprintf(`rabbitmqctl forget_cluster_node %s && echo "Node %s removed"`, req.Node, req.Node)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rabbitmq_scale_remove-node")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "remove-node"})

	case "list-nodes":
		script := `rabbitmqctl list_nodes name type running memory disk_free`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rabbitmq_scale_list-nodes")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list-nodes"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// RabbitMQ 版本升级
// ============================================================================

// handleRabbitMQUpgrade 处理 POST /api/v1/middleware/rabbitmq/upgrade
func (s *Server) handleRabbitMQUpgrade(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID     string `json:"deviceID"`
		Action       string `json:"action"`       // plan/upgrade/rollback/version
		TargetVersion string `json:"targetVersion"` // e.g. 3.13.0
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "plan":
		script := `echo "=== Current Version ===" && rabbitmqctl status 2>/dev/null | grep -i "rabbitmq version" && echo "=== Upgrade Plan ===" && echo "1. Backup definitions" && echo "2. Stop node" && echo "3. Upgrade package" && echo "4. Start node" && echo "5. Verify" && echo "6. Repeat for next node"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rabbitmq_upgrade_plan")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "plan"})

	case "upgrade":
		version := req.TargetVersion
		if version == "" {
			version = "3.13.0"
		}
		script := fmt.Sprintf(`rabbitmqctl export_definitions /tmp/rabbitmq_defs.json && systemctl stop rabbitmq-server && apt-get install -y rabbitmq-server=%s 2>/dev/null || yum install -y rabbitmq-server-%s 2>/dev/null && systemctl start rabbitmq-server && rabbitmqctl import_definitions /tmp/rabbitmq_defs.json && echo "Upgraded to %s"`, version, version, version)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rabbitmq_upgrade_apply")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "upgrade"})

	case "rollback":
		script := `systemctl stop rabbitmq-server && apt-get install -y --allow-downgrades rabbitmq-server 2>/dev/null && systemctl start rabbitmq-server && rabbitmqctl import_definitions /tmp/rabbitmq_defs.json && echo "Rollback complete"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rabbitmq_upgrade_rollback")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "rollback"})

	case "version":
		script := `rabbitmqctl status 2>/dev/null | grep -i "rabbitmq version" || echo "RabbitMQ not running"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rabbitmq_upgrade_version")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "version"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// RabbitMQ 备份/恢复
// ============================================================================

// handleRabbitMQBackup 处理 POST /api/v1/middleware/rabbitmq/backup
func (s *Server) handleRabbitMQBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // export/import/list
		Path     string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	path := req.Path
	if path == "" {
		path = "/tmp/rabbitmq_backup.json"
	}

	switch req.Action {
	case "export":
		script := fmt.Sprintf(`rabbitmqctl export_definitions %s && echo "Definitions exported to %s"`, path, path)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rabbitmq_backup_export")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "export"})

	case "import":
		script := fmt.Sprintf(`rabbitmqctl import_definitions %s && echo "Definitions imported from %s"`, path, path)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rabbitmq_backup_import")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "import"})

	case "list":
		script := fmt.Sprintf(`ls -lh %s 2>/dev/null || echo "No backup files found"`, path)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rabbitmq_backup_list")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// RabbitMQ 性能调优
// ============================================================================

// handleRabbitMQTuning 处理 POST /api/v1/middleware/rabbitmq/tuning
func (s *Server) handleRabbitMQTuning(w http.ResponseWriter, r *http.Request) {
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

	switch req.Action {
	case "analyze":
		script := `echo "=== Memory ===" && rabbitmqctl status 2>/dev/null | grep -i "memory\|disk\|fd" && echo "=== Queues ===" && rabbitmqctl list_queues name messages messages_ready messages_unacknowledged 2>/dev/null | head -20 && echo "=== Connections ===" && rabbitmqctl list_connections 2>/dev/null | wc -l`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rabbitmq_tuning_analyze")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "analyze"})

	case "recommend":
		script := `echo "=== Recommendations ===" && echo "1. vm_memory_high_watermark: 0.6 (default 0.4)" && echo "2. disk_free_limit: 2GB (default 50MB)" && echo "3. channel_max: 2047" && echo "4. frame_max: 131072" && echo "5. heartbeat: 60s" && echo "6. Enable lazy queues for large backlogs" && echo "7. Enable quorum queues for HA"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rabbitmq_tuning_recommend")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "recommend"})

	case "apply":
		script := `rabbitmqctl set_vm_memory_high_watermark 0.6 && rabbitmqctl set_disk_free_limit 2GB && echo "Performance tuning applied"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rabbitmq_tuning_apply")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "apply"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// RabbitMQ 安全 (TLS/SASL)
// ============================================================================

// handleRabbitMQSecurity 处理 POST /api/v1/middleware/rabbitmq/security
func (s *Server) handleRabbitMQSecurity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // enable-tls/status/configure
		CertPath string `json:"certPath"`
		KeyPath  string `json:"keyPath"`
		CACert   string `json:"caCert"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "enable-tls":
		script := fmt.Sprintf(`cat >> /etc/rabbitmq/rabbitmq.conf << 'EOF'
listeners.ssl.default = 5671
ssl_options.cacertfile = %s
ssl_options.certfile = %s
ssl_options.keyfile = %s
ssl_options.verify = verify_peer
ssl_options.fail_if_no_peer_cert = true
EOF
systemctl restart rabbitmq-server && echo "TLS enabled"`, req.CACert, req.CertPath, req.KeyPath)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rabbitmq_security_enable-tls")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "enable-tls"})

	case "status":
		script := `rabbitmqctl status 2>/dev/null | grep -i "ssl\|tls\|listeners" && echo "---" && cat /etc/rabbitmq/rabbitmq.conf 2>/dev/null | grep -i "ssl\|tls" || echo "No TLS config"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rabbitmq_security_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	case "configure":
		script := `echo "RabbitMQ Security Configuration:" && echo "1. TLS: listeners.ssl.default = 5671" && echo "2. SASL: auth_mechanisms.1 = PLAIN, auth_mechanisms.2 = AMQPLAIN" && echo "3. OAuth2: auth_oauth2.resource_server_id = rabbitmq" && echo "4. LDAP: auth_backends.1 = ldap" && echo "5. Firewall: restrict ports 5672/5671/15672"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rabbitmq_security_configure")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "configure"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// RabbitMQ Observability (Prometheus)
// ============================================================================

// handleRabbitMQObservability 处理 POST /api/v1/middleware/rabbitmq/observability
func (s *Server) handleRabbitMQObservability(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // enable-prometheus/status
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "enable-prometheus":
		script := `rabbitmq-plugins enable rabbitmq_prometheus && systemctl restart rabbitmq-server && echo "Prometheus metrics enabled on port 15692"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rabbitmq_obs_enable-prometheus")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "enable-prometheus"})

	case "status":
		script := `curl -s http://localhost:15692/metrics 2>/dev/null | head -5 || echo "Prometheus metrics not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rabbitmq_obs_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// 路由注册
// ============================================================================

// registerRabbitMQFinalRoutes 注册 RabbitMQ 最终路由
func (s *Server) registerRabbitMQFinalRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/middleware/rabbitmq/ha", s.handleRabbitMQHA)
	mux.HandleFunc("/api/v1/middleware/rabbitmq/scale", s.handleRabbitMQScale)
	mux.HandleFunc("/api/v1/middleware/rabbitmq/upgrade", s.handleRabbitMQUpgrade)
	mux.HandleFunc("/api/v1/middleware/rabbitmq/backup", s.handleRabbitMQBackup)
	mux.HandleFunc("/api/v1/middleware/rabbitmq/tuning", s.handleRabbitMQTuning)
	mux.HandleFunc("/api/v1/middleware/rabbitmq/security", s.handleRabbitMQSecurity)
	mux.HandleFunc("/api/v1/middleware/rabbitmq/observability", s.handleRabbitMQObservability)
}
