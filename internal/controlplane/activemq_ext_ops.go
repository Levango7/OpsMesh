// Package controlplane activemq_ext_ops.go 实现 ActiveMQ 高级运维场景
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/pkg/shellquote"
)

// isVersionLike 校验形如 x.y.z 的版本号（每段纯数字，最多四段）。
// 用于会被拼进 wget URL / ln 路径的版本字段：这类字段的合法域是已知且窄的，
// 用白名单比转义更严——转义仍允许 `$(curl ...)` 之类出现在 URL 里。
func isVersionLike(v string) bool {
	parts := strings.Split(v, ".")
	if len(parts) < 2 || len(parts) > 4 {
		return false
	}
	for _, p := range parts {
		if p == "" || len(p) > 4 {
			return false
		}
		for i := 0; i < len(p); i++ {
			if p[i] < '0' || p[i] > '9' {
				return false
			}
		}
	}
	return true
}

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
		// 主机名字段：白名单拒绝而非转义。这里 master/slave 会被拼进 `ssh <host>`
		// —— 若允许任意串，`x'; rm -rf /; echo '` 之类可注入整条命令（见
		// docs/opsmesh-authz-audit-2026-11-04.md §3）。合法域就是主机名，直接卡死。
		for _, h := range []string{req.Master, req.Slave} {
			if err := shellquote.ValidateHost(h); err != nil {
				paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
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
		Action   string `json:"action"`  // setup/status
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
		// 逗号分隔 broker 列表：逐个走主机名白名单。整串直接进 `for broker in %s`
		// 会让任何元字符（; | $() `` 等）直达 shell。
		for _, b := range strings.Split(req.Brokers, ",") {
			b = strings.TrimSpace(b)
			if b == "" {
				paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "brokers contains an empty entry"})
				return
			}
			if err := shellquote.ValidateHost(b); err != nil {
				paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
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
		// user/role 是标识符（窄域）⇒ 白名单；password 会含任意字符 ⇒ 单引号转义。
		// 原先三者也走 %s 直拼，实测 user 传 `x"; curl ...; echo "` 即注入
		// （docs/opsmesh-authz-audit-2026-11-04.md §3）。
		for k, v := range map[string]string{"user": req.User, "role": req.Role} {
			if v != "" {
				if err := shellquote.ValidateIdent(v); err != nil {
					paginate.WriteJSON(w, http.StatusBadRequest,
						map[string]string{"error": fmt.Sprintf("%s: %v", k, err)})
					return
				}
			}
		}
		script = fmt.Sprintf(`echo %s=%s >> /opt/activemq/conf/users.properties &&
echo %s = %s >> /opt/activemq/conf/groups.properties && echo %s added`,
			shellquote.Quote(req.User), shellquote.Quote(req.Password),
			shellquote.Quote(req.Role), shellquote.Quote(req.User),
			shellquote.Quote(req.User))
	case "delete-user":
		if req.User == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "user is required"})
			return
		}
		if err := shellquote.ValidateIdent(req.User); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("user: %v", err)})
			return
		}
		script = fmt.Sprintf(`sed -i %s /opt/activemq/conf/users.properties && echo %s deleted`,
			shellquote.Quote("/^"+req.User+"=/d"), shellquote.Quote("User "+req.User+" deleted"))
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
		// name/topic/clientId 拼进 echo 的字面参数。这里用转义而非白名单：
		// 它们是 ActiveMQ 侧的业务名（可含 . - _ 之外的字符取决于部署），
		// 白名单会误拒合法命名；但绝不能再 %s 裸拼。
		script = fmt.Sprintf(`echo %s created on topic %s (clientId=%s)`,
			shellquote.Quote("Durable subscription "+req.Name),
			shellquote.Quote(req.Topic), shellquote.Quote(req.ClientID))
	case "delete":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		script = fmt.Sprintf(`echo %s deleted`, shellquote.Quote("Durable subscription "+req.Name))
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
		script = `echo "=== Current Version ===" && /opt/activemq/bin/activemq version 2>/dev/null && echo "=== Upgrade Plan ===" && echo "1. Stop broker" && echo "2. Backup data" && echo "3. Replace binaries" && echo "4. Start broker" && echo "5. Verify"`
	case "upgrade":
		version := req.TargetVersion
		if version == "" {
			version = "5.18.3"
		}
		// version 会进 wget URL 与 ln 的目标路径：`$(...)` 之类可注入，
		// 且能借 URL 把任意文件落到任意路径 ⇒ 白名单卡版本号形态（x.y.z）。
		if !isVersionLike(version) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{
				"error": "invalid targetVersion: expected x.y.z, got " + version})
			return
		}
		script = fmt.Sprintf(`systemctl stop activemq && cp -r /opt/activemq/data /tmp/activemq-data-backup && wget -q https://archive.apache.org/dist/activemq/%s/apache-activemq-%s-bin.tar.gz -O /tmp/activemq.tgz && tar -xzf /tmp/activemq.tgz -C /opt/ && ln -sfn /opt/apache-activemq-%s /opt/activemq && systemctl start activemq && echo %s`, version, version, version, shellquote.Quote("Upgraded to "+version))
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
