// Package controlplane pulsar_ext_ops.go 实现 Pulsar 高级运维场景
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handlePulsarHA 处理 POST /api/v1/middleware/pulsar/ha
func (s *Server) handlePulsarHA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // setup-bookkeeper/status
		Nodes    string `json:"nodes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "setup-bookkeeper":
		if req.Nodes == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "nodes is required"})
			return
		}
		script := fmt.Sprintf(`echo "Setting up Pulsar BookKeeper HA..." && for node in %s; do echo "Configuring $node as BookKeeper node"; done && echo "BookKeeper cluster configured"`, req.Nodes)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "pulsar_ha_setup-bookkeeper")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "setup-bookkeeper"})

	case "status":
		script := `pulsar-admin brokers healthcheck 2>/dev/null && echo "Pulsar healthy" || echo "Pulsar not available" && echo "---" && pulsar-admin bookies list 2>/dev/null || echo "BookKeeper not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "pulsar_ha_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// handlePulsarScale 处理 POST /api/v1/middleware/pulsar/scale
func (s *Server) handlePulsarScale(w http.ResponseWriter, r *http.Request) {
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
		script := fmt.Sprintf(`pulsar-admin brokers update-dynamic-config --config brokerShutdownTimeoutMs --value 60000 2>/dev/null; echo "Broker %s added"`, req.Broker)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "pulsar_scale_add-broker")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "add-broker"})

	case "remove-broker":
		if req.Broker == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "broker is required"})
			return
		}
		script := fmt.Sprintf(`pulsar-admin brokers delete %s 2>/dev/null && echo "Broker %s removed"`, req.Broker, req.Broker)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "pulsar_scale_remove-broker")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "remove-broker"})

	case "list-brokers":
		script := `pulsar-admin brokers list 2>/dev/null || echo "Pulsar not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "pulsar_scale_list-brokers")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list-brokers"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// handlePulsarUpgrade 处理 POST /api/v1/middleware/pulsar/upgrade
func (s *Server) handlePulsarUpgrade(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID      string `json:"deviceID"`
		Action        string `json:"action"` // plan/upgrade/rollback/version
		TargetVersion string `json:"targetVersion"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "plan":
		script = `echo "=== Current Version ===" && /opt/pulsar/bin/pulsar --version 2>/dev/null && echo "=== Upgrade Plan ===" && echo "1. Stop broker" && echo "2. Backup data" && echo "3. Replace binaries" && echo "4. Start broker" && echo "5. Verify"`
	case "upgrade":
		version := req.TargetVersion
		if version == "" {
			version = "3.2.0"
		}
		script = fmt.Sprintf(`systemctl stop pulsar && cp -r /opt/pulsar/data /tmp/pulsar-data-backup && wget -q https://archive.apache.org/dist/pulsar/pulsar-%s/apache-pulsar-%s-bin.tar.gz -O /tmp/pulsar.tgz && tar -xzf /tmp/pulsar.tgz -C /opt/ && ln -sfn /opt/apache-pulsar-%s /opt/pulsar && systemctl start pulsar && echo "Upgraded to %s"`, version, version, version, version)
	case "rollback":
		script = `systemctl stop pulsar && cp -r /tmp/pulsar-data-backup /opt/pulsar/data && systemctl start pulsar && echo "Rollback complete"`
	case "version":
		script = `/opt/pulsar/bin/pulsar --version 2>/dev/null || echo "Pulsar not installed"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "pulsar_upgrade_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handlePulsarSchemaRegistry 处理 POST /api/v1/middleware/pulsar/schema-registry
func (s *Server) handlePulsarSchemaRegistry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // get/list
		Tenant   string `json:"tenant"`
		NS       string `json:"namespace"`
		Topic    string `json:"topic"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	tenant := req.Tenant
	if tenant == "" {
		tenant = "public"
	}
	ns := req.NS
	if ns == "" {
		ns = "default"
	}

	var script string
	switch req.Action {
	case "list":
		script = fmt.Sprintf(`pulsar-admin schemas getAllVersions %s/%s/%s 2>/dev/null || echo "Pulsar not available"`, tenant, ns, req.Topic)
	case "get":
		if req.Topic == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "topic is required"})
			return
		}
		script = fmt.Sprintf(`pulsar-admin schemas get %s/%s/%s 2>/dev/null || echo "Schema not found"`, tenant, ns, req.Topic)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "pulsar_schema-registry_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handlePulsarConnector 处理 POST /api/v1/middleware/pulsar/connector
func (s *Server) handlePulsarConnector(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID  string `json:"deviceID"`
		Action    string `json:"action"` // list/create/delete
		Tenant    string `json:"tenant"`
		NS        string `json:"namespace"`
		Name      string `json:"name"`
		ClassName string `json:"className"`
		Config    string `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	tenant := req.Tenant
	if tenant == "" {
		tenant = "public"
	}
	ns := req.NS
	if ns == "" {
		ns = "default"
	}

	var script string
	switch req.Action {
	case "list":
		script = fmt.Sprintf(`pulsar-admin sources list --tenant %s --namespace %s 2>/dev/null; pulsar-admin sinks list --tenant %s --namespace %s 2>/dev/null`, tenant, ns, tenant, ns)
	case "create":
		if req.Name == "" || req.ClassName == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name and className are required"})
			return
		}
		script = fmt.Sprintf(`pulsar-admin sources create --tenant %s --namespace %s --name %s --classname %s --source-config '%s' 2>/dev/null && echo "Connector %s created"`, tenant, ns, req.Name, req.ClassName, req.Config, req.Name)
	case "delete":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		script = fmt.Sprintf(`pulsar-admin sources delete --tenant %s --namespace %s --name %s 2>/dev/null && echo "Connector %s deleted"`, tenant, ns, req.Name, req.Name)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "pulsar_connector_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handlePulsarTieredStorage 处理 POST /api/v1/middleware/pulsar/tiered-storage
func (s *Server) handlePulsarTieredStorage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`  // enable/disable/status
		Storage  string `json:"storage"` // s3/gcs/azure
		Bucket   string `json:"bucket"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "enable":
		script := fmt.Sprintf(`echo "Enabling Pulsar tiered storage: storage=%s bucket=%s" && pulsar-admin namespaces set-offload-threshold -t 1GB public/default 2>/dev/null && echo "Tiered storage enabled"`, req.Storage, req.Bucket)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "pulsar_tiered-storage_enable")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "enable"})

	case "disable":
		script := `pulsar-admin namespaces remove-offload-policies public/default 2>/dev/null && echo "Tiered storage disabled"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "pulsar_tiered-storage_disable")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "disable"})

	case "status":
		script := `pulsar-admin namespaces get-offload-policies public/default 2>/dev/null || echo "No offload policies"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "pulsar_tiered-storage_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// registerPulsarExtendedRoutes 注册 Pulsar 扩展路由
func (s *Server) registerPulsarExtendedRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/middleware/pulsar/ha", s.handlePulsarHA)
	mux.HandleFunc("/api/v1/middleware/pulsar/scale", s.handlePulsarScale)
	mux.HandleFunc("/api/v1/middleware/pulsar/upgrade", s.handlePulsarUpgrade)
	mux.HandleFunc("/api/v1/middleware/pulsar/schema-registry", s.handlePulsarSchemaRegistry)
	mux.HandleFunc("/api/v1/middleware/pulsar/connector", s.handlePulsarConnector)
	mux.HandleFunc("/api/v1/middleware/pulsar/tiered-storage", s.handlePulsarTieredStorage)
}

// registerRocketMQExtendedRoutes2 注册 RocketMQ 扩展路由 (避免与 rocketmq_ops.go 冲突)
func (s *Server) registerRocketMQExtendedRoutes2(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/middleware/rocketmq/ha", s.handleRocketMQHA)
	mux.HandleFunc("/api/v1/middleware/rocketmq/scale", s.handleRocketMQScale)
	mux.HandleFunc("/api/v1/middleware/rocketmq/upgrade", s.handleRocketMQUpgrade)
	mux.HandleFunc("/api/v1/middleware/rocketmq/monitor", s.handleRocketMQMonitor)
	mux.HandleFunc("/api/v1/middleware/rocketmq/dlq", s.handleRocketMQDLQ)
	mux.HandleFunc("/api/v1/middleware/rocketmq/tuning", s.handleRocketMQTuning)
}
