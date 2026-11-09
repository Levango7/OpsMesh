// Package controlplane rabbitmq_ops.go 实现 RabbitMQ 全面运维场景
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/pkg/shellquote"
)

// ============================================================================
// RabbitMQ 部署
// ============================================================================

// handleRabbitMQDeploy 处理 POST /api/v1/middleware/rabbitmq/deploy
func (s *Server) handleRabbitMQDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Mode     string `json:"mode"`    // single/cluster
		Version  string `json:"version"` // e.g. 3.13
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	version, err := mqVersion("version", req.Version)
	if err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if version == "" {
		version = "3.13"
	}
	mode := req.Mode
	if mode == "" {
		mode = "single"
	}

	var script string
	if mode == "cluster" {
		script = fmt.Sprintf(`apt-get install -y rabbitmq-server 2>/dev/null || yum install -y rabbitmq-server 2>/dev/null && systemctl enable rabbitmq-server && systemctl start rabbitmq-server && rabbitmq-plugins enable rabbitmq_management && echo %s`, opsEcho("RabbitMQ %s deployed (cluster mode: join with rabbitmqctl join_cluster)", version))
	} else {
		script = fmt.Sprintf(`apt-get install -y rabbitmq-server 2>/dev/null || yum install -y rabbitmq-server 2>/dev/null && systemctl enable rabbitmq-server && systemctl start rabbitmq-server && rabbitmq-plugins enable rabbitmq_management && echo %s`, opsEcho("RabbitMQ %s deployed (single mode)", version))
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rabbitmq_deploy")
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy"})
}

// ============================================================================
// RabbitMQ 生命周期
// ============================================================================

// handleRabbitMQLifecycle 处理 POST /api/v1/middleware/rabbitmq/lifecycle
func (s *Server) handleRabbitMQLifecycle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // start/stop/restart/status/enable-plugins
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "start":
		script = "systemctl start rabbitmq-server && echo 'RabbitMQ started'"
	case "stop":
		script = "systemctl stop rabbitmq-server && echo 'RabbitMQ stopped'"
	case "restart":
		script = "systemctl restart rabbitmq-server && echo 'RabbitMQ restarted'"
	case "status":
		script = "systemctl status rabbitmq-server 2>/dev/null || rabbitmqctl status"
	case "enable-plugins":
		script = "rabbitmq-plugins enable rabbitmq_management rabbitmq_shovel rabbitmq_federation rabbitmq_prometheus && systemctl restart rabbitmq-server && echo 'Plugins enabled'"
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rabbitmq_lifecycle_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// RabbitMQ Vhost 管理
// ============================================================================

// ============================================================================
// RabbitMQ Queue 管理
// ============================================================================

// ============================================================================
// RabbitMQ Exchange 管理
// ============================================================================

// ============================================================================
// RabbitMQ 用户/权限管理
// ============================================================================

// handleRabbitMQUser 处理 POST /api/v1/middleware/rabbitmq/user
func (s *Server) handleRabbitMQUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // list/add/delete/set-permissions
		User     string `json:"user"`
		Password string `json:"password"`
		Vhost    string `json:"vhost"`
		Conf     string `json:"conf"`  // configure permission
		Write    string `json:"write"` // write permission
		Read     string `json:"read"`  // read permission
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	vhost := req.Vhost
	if vhost == "" {
		vhost = "/"
	}

	var script string
	switch req.Action {
	case "list":
		script = `rabbitmqctl list_users`
	case "add":
		if req.User == "" || req.Password == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "user and password are required"})
			return
		}
		user, err := mqName("user", req.User)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		script = fmt.Sprintf(`rabbitmqctl add_user %s %s && echo %s`, shellquote.Quote(user), shellquote.Quote(req.Password), opsEcho("User %s created", user))
	case "delete":
		if req.User == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "user is required"})
			return
		}
		user, err := mqName("user", req.User)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		script = fmt.Sprintf(`rabbitmqctl delete_user %s && echo %s`, shellquote.Quote(user), opsEcho("User %s deleted", user))
	case "set-permissions":
		if req.User == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "user is required"})
			return
		}
		conf := req.Conf
		if conf == "" {
			conf = ".*"
		}
		write := req.Write
		if write == "" {
			write = ".*"
		}
		read := req.Read
		if read == "" {
			read = ".*"
		}
		user, err := mqName("user", req.User)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		script = fmt.Sprintf(`rabbitmqctl set_permissions -p %s %s %s %s %s && echo %s`, shellquote.Quote(vhost), shellquote.Quote(user), shellquote.Quote(conf), shellquote.Quote(write), shellquote.Quote(read), opsEcho("Permissions set for %s", user))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rabbitmq_user_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// RabbitMQ HA 集群
// ============================================================================

// handleRabbitMQCluster 处理 POST /api/v1/middleware/rabbitmq/cluster
func (s *Server) handleRabbitMQCluster(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // join/status/list-nodes
		Node     string `json:"node"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "join":
		if req.Node == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "node is required"})
			return
		}
		node, err := mqNode("node", req.Node)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		script = fmt.Sprintf(`rabbitmqctl stop_app && rabbitmqctl join_cluster %s && rabbitmqctl start_app && echo %s`, shellquote.Quote(node), opsEcho("Joined cluster node %s", node))
	case "status":
		script = `rabbitmqctl cluster_status`
	case "list-nodes":
		script = `rabbitmqctl list_nodes`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rabbitmq_cluster_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// RabbitMQ Policy 管理
// ============================================================================

// handleRabbitMQPolicy 处理 POST /api/v1/middleware/rabbitmq/policy
func (s *Server) handleRabbitMQPolicy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // set/clear/list
		Name     string `json:"name"`
		Pattern  string `json:"pattern"`
		Def      string `json:"def"` // JSON definition
		Priority int    `json:"priority"`
		Vhost    string `json:"vhost"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	vhost := req.Vhost
	if vhost == "" {
		vhost = "/"
	}

	var script string
	switch req.Action {
	case "list":
		script = fmt.Sprintf(`rabbitmqctl list_policies -p %s`, shellquote.Quote(vhost))
	case "set":
		if req.Name == "" || req.Pattern == "" || req.Def == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name, pattern, and def are required"})
			return
		}
		name, err := mqName("name", req.Name)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		def, err := opsQuoteJSONText(req.Def)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		priority := req.Priority
		if priority == 0 {
			priority = 0
		}
		script = fmt.Sprintf(`rabbitmqctl set_policy -p %s %s %s %s --priority %d && echo %s`, shellquote.Quote(vhost), shellquote.Quote(name), shellquote.Quote(req.Pattern), def, priority, opsEcho("Policy %s set", name))
	case "clear":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		name, err := mqName("name", req.Name)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		script = fmt.Sprintf(`rabbitmqctl clear_policy -p %s %s && echo %s`, shellquote.Quote(vhost), shellquote.Quote(name), opsEcho("Policy %s cleared", name))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rabbitmq_policy_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// RabbitMQ Shovel 管理
// ============================================================================

// handleRabbitMQShovel 处理 POST /api/v1/middleware/rabbitmq/shovel
func (s *Server) handleRabbitMQShovel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID  string `json:"deviceID"`
		Action    string `json:"action"` // list/add/delete
		Name      string `json:"name"`
		SrcURI    string `json:"srcUri"`
		DestURI   string `json:"destUri"`
		SrcQueue  string `json:"srcQueue"`
		DestQueue string `json:"destQueue"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "list":
		script = `rabbitmqctl list_shovels`
	case "add":
		if req.Name == "" || req.SrcURI == "" || req.DestURI == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name, srcUri, and destUri are required"})
			return
		}
		name, err := mqName("name", req.Name)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		payload, err := opsJSONPayload(map[string]string{
			"src-uri":    req.SrcURI,
			"src-queue":  req.SrcQueue,
			"dest-uri":   req.DestURI,
			"dest-queue": req.DestQueue,
		})
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		script = fmt.Sprintf(`rabbitmqctl set_parameter shovel %s %s && echo %s`, shellquote.Quote(name), payload, opsEcho("Shovel %s added", name))
	case "delete":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		name, err := mqName("name", req.Name)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		script = fmt.Sprintf(`rabbitmqctl clear_parameter shovel %s && echo %s`, shellquote.Quote(name), opsEcho("Shovel %s deleted", name))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rabbitmq_shovel_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// RabbitMQ Federation 管理
// ============================================================================

// handleRabbitMQFederation 处理 POST /api/v1/middleware/rabbitmq/federation
func (s *Server) handleRabbitMQFederation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // list-upstreams/add-upstream/remove-upstream
		Name     string `json:"name"`
		URI      string `json:"uri"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "list-upstreams":
		script = `rabbitmqctl list_parameters federation-upstream`
	case "add-upstream":
		if req.Name == "" || req.URI == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name and uri are required"})
			return
		}
		name, err := mqName("name", req.Name)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		payload, err := opsJSONPayload(map[string]string{"uri": req.URI})
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		script = fmt.Sprintf(`rabbitmqctl set_parameter federation-upstream %s %s && echo %s`, shellquote.Quote(name), payload, opsEcho("Upstream %s added", name))
	case "remove-upstream":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		name, err := mqName("name", req.Name)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		script = fmt.Sprintf(`rabbitmqctl clear_parameter federation-upstream %s && echo %s`, shellquote.Quote(name), opsEcho("Upstream %s removed", name))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rabbitmq_federation_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// RabbitMQ 消息操作
// ============================================================================

// handleRabbitMQMessage 处理 POST /api/v1/middleware/rabbitmq/message
func (s *Server) handleRabbitMQMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // publish/get/count
		Queue    string `json:"queue"`
		Vhost    string `json:"vhost"`
		Message  string `json:"message"`
		Count    int    `json:"count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	vhost := req.Vhost
	if vhost == "" {
		vhost = "/"
	}

	var script string
	switch req.Action {
	case "publish":
		if req.Queue == "" || req.Message == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "queue and message are required"})
			return
		}
		queue, err := mqName("queue", req.Queue)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		script = fmt.Sprintf(`echo %s | rabbitmqadmin publish exchange=amq.default routing_key=%s 2>/dev/null || echo "rabbitmqadmin not available"`, shellquote.Quote(req.Message), shellquote.Quote(queue))
	case "get":
		if req.Queue == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "queue is required"})
			return
		}
		queue, err := mqName("queue", req.Queue)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		count := req.Count
		if count <= 0 {
			count = 1
		}
		script = fmt.Sprintf(`rabbitmqadmin get queue=%s count=%d 2>/dev/null || rabbitmqctl list_messages %s -p %s 2>/dev/null`, shellquote.Quote(queue), count, shellquote.Quote(queue), shellquote.Quote(vhost))
	case "count":
		if req.Queue == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "queue is required"})
			return
		}
		queue, err := mqName("queue", req.Queue)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		script = fmt.Sprintf(`rabbitmqctl list_queues -p %s name messages 2>/dev/null | grep %s`, shellquote.Quote(vhost), shellquote.Quote(queue))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rabbitmq_message_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// RabbitMQ 监控
// ============================================================================

// handleRabbitMQMonitor 处理 POST /api/v1/middleware/rabbitmq/monitor
func (s *Server) handleRabbitMQMonitor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // overview/queues/nodes/channels
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "overview":
		script = `rabbitmqctl status 2>/dev/null | head -20 && echo "---" && rabbitmqctl list_queues name messages consumers 2>/dev/null | head -20`
	case "queues":
		script = `rabbitmqctl list_queues name messages messages_ready messages_unacknowledged consumers memory 2>/dev/null`
	case "nodes":
		script = `rabbitmqctl list_nodes name type running memory disk_free 2>/dev/null`
	case "channels":
		script = `rabbitmqctl list_channels 2>/dev/null | head -30`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "rabbitmq_monitor_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// 路由注册
// ============================================================================

// registerRabbitMQExtendedRoutes 注册 RabbitMQ 扩展路由
func (s *Server) registerRabbitMQExtendedRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/middleware/rabbitmq/deploy", s.handleRabbitMQDeploy)
	mux.HandleFunc("/api/v1/middleware/rabbitmq/lifecycle", s.handleRabbitMQLifecycle)
	mux.HandleFunc("/api/v1/middleware/rabbitmq/user", s.handleRabbitMQUser)
	mux.HandleFunc("/api/v1/middleware/rabbitmq/cluster", s.handleRabbitMQCluster)
	mux.HandleFunc("/api/v1/middleware/rabbitmq/policy", s.handleRabbitMQPolicy)
	mux.HandleFunc("/api/v1/middleware/rabbitmq/shovel", s.handleRabbitMQShovel)
	mux.HandleFunc("/api/v1/middleware/rabbitmq/federation", s.handleRabbitMQFederation)
	mux.HandleFunc("/api/v1/middleware/rabbitmq/message", s.handleRabbitMQMessage)
	mux.HandleFunc("/api/v1/middleware/rabbitmq/monitor", s.handleRabbitMQMonitor)
}
