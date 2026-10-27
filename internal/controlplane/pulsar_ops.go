// Package controlplane pulsar_ops.go 实现 Apache Pulsar 全面运维场景
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handlePulsarDeploy 处理 POST /api/v1/middleware/pulsar/deploy
func (s *Server) handlePulsarDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Mode     string `json:"mode"`    // single/cluster
		Version  string `json:"version"` // e.g. 3.2.0
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	version := req.Version
	if version == "" {
		version = "3.2.0"
	}

	script := fmt.Sprintf(`wget -q https://archive.apache.org/dist/pulsar/pulsar-%s/apache-pulsar-%s-bin.tar.gz -O /tmp/pulsar.tgz && tar -xzf /tmp/pulsar.tgz -C /opt/ && ln -sfn /opt/apache-pulsar-%s /opt/pulsar && /opt/pulsar/bin/pulsar standalone & echo "Pulsar %s deployed"`, version, version, version, version)
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "pulsar_deploy")
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy"})
}

// handlePulsarLifecycle 处理 POST /api/v1/middleware/pulsar/lifecycle
func (s *Server) handlePulsarLifecycle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // start/stop/status
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "start":
		script = "/opt/pulsar/bin/pulsar standalone & echo 'Pulsar started'"
	case "stop":
		script = "pkill -f pulsar 2>/dev/null && echo 'Pulsar stopped'"
	case "status":
		script = "pgrep -f pulsar > /dev/null && echo 'Pulsar running' || echo 'Pulsar not running'"
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "pulsar_lifecycle_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handlePulsarTopic 处理 POST /api/v1/middleware/pulsar/topic
func (s *Server) handlePulsarTopic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID  string `json:"deviceID"`
		Action    string `json:"action"` // list/create/delete
		Topic     string `json:"topic"`
		Tenant    string `json:"tenant"`
		Namespace string `json:"namespace"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	tenant := req.Tenant
	if tenant == "" {
		tenant = "public"
	}
	ns := req.Namespace
	if ns == "" {
		ns = "default"
	}

	var script string
	switch req.Action {
	case "list":
		script = `pulsar-admin topics list public/default 2>/dev/null || echo "Pulsar not available"`
	case "create":
		if req.Topic == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "topic is required"})
			return
		}
		script = fmt.Sprintf(`pulsar-admin topics create persistent://%s/%s/%s 2>/dev/null && echo "Topic %s created"`, tenant, ns, req.Topic, req.Topic)
	case "delete":
		if req.Topic == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "topic is required"})
			return
		}
		script = fmt.Sprintf(`pulsar-admin topics delete persistent://%s/%s/%s 2>/dev/null && echo "Topic %s deleted"`, tenant, ns, req.Topic, req.Topic)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "pulsar_topic_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handlePulsarSubscription 处理 POST /api/v1/middleware/pulsar/subscription
func (s *Server) handlePulsarSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // list/create/delete
		Topic    string `json:"topic"`
		SubName  string `json:"subName"`
		SubType  string `json:"subType"` // Exclusive/Shared/Failover/Key_Shared
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "list":
		script = `pulsar-admin topics list-subscriptions public/default 2>/dev/null || echo "Pulsar not available"`
	case "create":
		if req.Topic == "" || req.SubName == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "topic and subName are required"})
			return
		}
		subType := req.SubType
		if subType == "" {
			subType = "Exclusive"
		}
		script = fmt.Sprintf(`pulsar-admin topics create-subscription --subscription %s --topic persistent://public/default/%s --messageId earliest 2>/dev/null && echo "Subscription %s created (%s)"`, req.SubName, req.Topic, req.SubName, subType)
	case "delete":
		if req.Topic == "" || req.SubName == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "topic and subName are required"})
			return
		}
		script = fmt.Sprintf(`pulsar-admin topics delete-subscription --subscription %s --topic persistent://public/default/%s 2>/dev/null && echo "Subscription %s deleted"`, req.SubName, req.Topic, req.SubName)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "pulsar_subscription_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handlePulsarTenant 处理 POST /api/v1/middleware/pulsar/tenant
func (s *Server) handlePulsarTenant(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // list/create/delete
		Tenant   string `json:"tenant"`
		Clusters string `json:"clusters"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "list":
		script = `pulsar-admin tenants list 2>/dev/null || echo "Pulsar not available"`
	case "create":
		if req.Tenant == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant is required"})
			return
		}
		clusters := req.Clusters
		if clusters == "" {
			clusters = "standalone"
		}
		script = fmt.Sprintf(`pulsar-admin tenants create %s --allowed-clusters %s 2>/dev/null && echo "Tenant %s created"`, req.Tenant, clusters, req.Tenant)
	case "delete":
		if req.Tenant == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant is required"})
			return
		}
		script = fmt.Sprintf(`pulsar-admin tenants delete %s 2>/dev/null && echo "Tenant %s deleted"`, req.Tenant, req.Tenant)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "pulsar_tenant_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handlePulsarNamespace 处理 POST /api/v1/middleware/pulsar/namespace
func (s *Server) handlePulsarNamespace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID  string `json:"deviceID"`
		Action    string `json:"action"` // list/create/delete
		Tenant    string `json:"tenant"`
		Namespace string `json:"namespace"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	tenant := req.Tenant
	if tenant == "" {
		tenant = "public"
	}

	var script string
	switch req.Action {
	case "list":
		script = fmt.Sprintf(`pulsar-admin namespaces list %s 2>/dev/null || echo "Pulsar not available"`, tenant)
	case "create":
		if req.Namespace == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "namespace is required"})
			return
		}
		script = fmt.Sprintf(`pulsar-admin namespaces create %s/%s 2>/dev/null && echo "Namespace %s/%s created"`, tenant, req.Namespace, tenant, req.Namespace)
	case "delete":
		if req.Namespace == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "namespace is required"})
			return
		}
		script = fmt.Sprintf(`pulsar-admin namespaces delete %s/%s 2>/dev/null && echo "Namespace %s/%s deleted"`, tenant, req.Namespace, tenant, req.Namespace)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "pulsar_namespace_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handlePulsarFunction 处理 POST /api/v1/middleware/pulsar/function
func (s *Server) handlePulsarFunction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID  string `json:"deviceID"`
		Action    string `json:"action"` // list/create/delete/status
		Tenant    string `json:"tenant"`
		NS        string `json:"namespace"`
		Name      string `json:"name"`
		ClassName string `json:"className"`
		Inputs    string `json:"inputs"`
		Outputs   string `json:"outputs"`
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
		script = fmt.Sprintf(`pulsar-admin functions list --tenant %s --namespace %s 2>/dev/null || echo "Pulsar not available"`, tenant, ns)
	case "create":
		if req.Name == "" || req.ClassName == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name and className are required"})
			return
		}
		script = fmt.Sprintf(`pulsar-admin functions create --tenant %s --namespace %s --name %s --classname %s --inputs %s --output %s 2>/dev/null && echo "Function %s created"`, tenant, ns, req.Name, req.ClassName, req.Inputs, req.Outputs, req.Name)
	case "delete":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		script = fmt.Sprintf(`pulsar-admin functions delete --tenant %s --namespace %s --name %s 2>/dev/null && echo "Function %s deleted"`, tenant, ns, req.Name, req.Name)
	case "status":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		script = fmt.Sprintf(`pulsar-admin functions status --tenant %s --namespace %s --name %s 2>/dev/null`, tenant, ns, req.Name)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "pulsar_function_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handlePulsarMonitor 处理 POST /api/v1/middleware/pulsar/monitor
func (s *Server) handlePulsarMonitor(w http.ResponseWriter, r *http.Request) {
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
		script = `pulsar-admin brokers healthcheck 2>/dev/null && echo "Pulsar healthy" || echo "Pulsar not available"`
	case "metrics":
		script = `curl -s http://localhost:8080/metrics 2>/dev/null | head -20 || echo "Metrics not available"`
	case "topics":
		script = `pulsar-admin topics list public/default 2>/dev/null || echo "Pulsar not available"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "pulsar_monitor_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerPulsarRoutes 注册 Pulsar 路由
func (s *Server) registerPulsarRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/middleware/pulsar/deploy", s.handlePulsarDeploy)
	mux.HandleFunc("/api/v1/middleware/pulsar/lifecycle", s.handlePulsarLifecycle)
	mux.HandleFunc("/api/v1/middleware/pulsar/topic", s.handlePulsarTopic)
	mux.HandleFunc("/api/v1/middleware/pulsar/subscription", s.handlePulsarSubscription)
	mux.HandleFunc("/api/v1/middleware/pulsar/tenant", s.handlePulsarTenant)
	mux.HandleFunc("/api/v1/middleware/pulsar/namespace", s.handlePulsarNamespace)
	mux.HandleFunc("/api/v1/middleware/pulsar/function", s.handlePulsarFunction)
	mux.HandleFunc("/api/v1/middleware/pulsar/monitor", s.handlePulsarMonitor)
}

// registerRocketMQExtendedRoutes 注册 RocketMQ 扩展路由
func (s *Server) registerRocketMQExtendedRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/middleware/rocketmq/deploy", s.handleRocketMQDeploy)
	mux.HandleFunc("/api/v1/middleware/rocketmq/lifecycle", s.handleRocketMQLifecycle)
	mux.HandleFunc("/api/v1/middleware/rocketmq/topic", s.handleRocketMQTopic)
	mux.HandleFunc("/api/v1/middleware/rocketmq/consumer-group", s.handleRocketMQConsumerGroup)
	mux.HandleFunc("/api/v1/middleware/rocketmq/acl", s.handleRocketMQACL)
}
