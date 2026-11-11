// Package controlplane elk_ops.go 实现 ELK (Elasticsearch/Logstash/Kibana) 全面运维场景
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/pkg/shellquote"
)

// ============================================================================
// Elasticsearch 部署
// ============================================================================

// handleElasticsearchDeploy 处理 POST /api/v1/search/elasticsearch/deploy
func (s *Server) handleElasticsearchDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Mode     string `json:"mode"`    // single/cluster
		Version  string `json:"version"` // e.g. 8.13.0
		Port     int    `json:"port"`
		MemLimit string `json:"memLimit"` // e.g. 512m
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	version := req.Version
	if version == "" {
		version = "8.13.0"
	}
	mode := req.Mode
	if mode == "" {
		mode = "single"
	}
	port := req.Port
	if port == 0 {
		port = 9200
	}
	memLimit := req.MemLimit
	if memLimit == "" {
		memLimit = "512m"
	}

	// 注入面（批次 9）：version / memLimit 是用户可控字段。
	// - ES_JAVA_OPTS 的值旧形态在**双引号段**内 ⇒ 直接塞 Quote 结果会让单引号变字面量
	//   （双引号内单引号是字面量），故整个 env 值在 Go 层拼好后整体 Quote。
	// - 镜像 tag 与回显文本同样要防：裸拼可被执行；回显的双引号段内 $ / 反引号会展开。
	javaOpts := "ES_JAVA_OPTS=-Xms" + opsSQLString(memLimit) + " -Xmx" + opsSQLString(memLimit)
	var script string
	if mode == "cluster" {
		script = fmt.Sprintf(`docker run -d --name elasticsearch -p %d:9200 -p 9300:9300 -e "discovery.type=zen" -e %s -e "xpack.security.enabled=false" docker.elastic.co/elasticsearch/elasticsearch:%s 2>/dev/null && %s`, port, shellquote.Quote(javaOpts), shellquote.Quote(version), esEcho("Elasticsearch "+version+" deployed (cluster mode)"))
	} else {
		script = fmt.Sprintf(`docker run -d --name elasticsearch -p %d:9200 -e "discovery.type=single-node" -e %s -e "xpack.security.enabled=false" docker.elastic.co/elasticsearch/elasticsearch:%s 2>/dev/null && %s`, port, shellquote.Quote(javaOpts), shellquote.Quote(version), esEcho("Elasticsearch "+version+" deployed (single mode)"))
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "elasticsearch_deploy")
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy"})
}

// handleElasticsearchLifecycle 处理 POST /api/v1/search/elasticsearch/lifecycle
func (s *Server) handleElasticsearchLifecycle(w http.ResponseWriter, r *http.Request) {
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
		script = "docker start elasticsearch 2>/dev/null || systemctl start elasticsearch && echo 'Elasticsearch started'"
	case "stop":
		script = "docker stop elasticsearch 2>/dev/null || systemctl stop elasticsearch && echo 'Elasticsearch stopped'"
	case "restart":
		script = "docker restart elasticsearch 2>/dev/null || systemctl restart elasticsearch && echo 'Elasticsearch restarted'"
	case "status":
		script = "curl -s http://localhost:9200/_cluster/health 2>/dev/null || echo 'Elasticsearch not running'"
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "elasticsearch_lifecycle_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// Elasticsearch 集群健康
// ============================================================================

// handleElasticsearchCluster 处理 POST /api/v1/search/elasticsearch/cluster
func (s *Server) handleElasticsearchCluster(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // health/nodes/stats/shards/allocation
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "health":
		script = `curl -s http://localhost:9200/_cluster/health?pretty 2>/dev/null || echo "ES not available"`
	case "nodes":
		script = `curl -s http://localhost:9200/_cat/nodes?v 2>/dev/null || echo "ES not available"`
	case "stats":
		script = `curl -s http://localhost:9200/_stats 2>/dev/null | head -30 || echo "ES not available"`
	case "shards":
		script = `curl -s http://localhost:9200/_cat/shards?v 2>/dev/null || echo "ES not available"`
	case "allocation":
		script = `curl -s http://localhost:9200/_cluster/allocation/explain?pretty 2>/dev/null || echo "ES not available"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "elasticsearch_cluster_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// Elasticsearch 索引管理
// ============================================================================

// handleElasticsearchIndex 处理 POST /api/v1/search/elasticsearch/index
func (s *Server) handleElasticsearchIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // list/create/delete/mapping/settings/aliases
		Index    string `json:"index"`
		Body     string `json:"body"` // JSON body for create/mapping
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "list":
		script = `curl -s http://localhost:9200/_cat/indices?v 2>/dev/null || echo "ES not available"`
	case "create":
		if req.Index == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "index is required"})
			return
		}
		body := req.Body
		if body == "" {
			body = "{}"
		}
		// 注入面（批次 9）：URL 整体 Quote（不能把 Quote 结果塞回双引号段——双引号内
		// 单引号是字面量，URL 会带上 %27）；payload 是用户 JSON，先校验合法性再整体 Quote
		// （旧形态 -d '%s' 遇单引号即破段 ⇒ 可执行注入）。
		payload, err := esQuoteJSONText(body)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body: " + err.Error()})
			return
		}
		script = fmt.Sprintf(`curl -s -X PUT %s -H "Content-Type: application/json" -d %s 2>/dev/null`, esAPIURL(req.Index), payload)
	case "delete":
		if req.Index == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "index is required"})
			return
		}
		script = fmt.Sprintf(`curl -s -X DELETE %s 2>/dev/null`, esAPIURL(req.Index))
	case "mapping":
		if req.Index == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "index is required"})
			return
		}
		script = fmt.Sprintf(`curl -s %s 2>/dev/null`, esAPIURL(req.Index+"/_mapping?pretty"))
	case "settings":
		if req.Index == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "index is required"})
			return
		}
		script = fmt.Sprintf(`curl -s %s 2>/dev/null`, esAPIURL(req.Index+"/_settings?pretty"))
	case "aliases":
		script = `curl -s http://localhost:9200/_aliases?pretty 2>/dev/null || echo "ES not available"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "elasticsearch_index_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// Elasticsearch ILM (Index Lifecycle Management)
// ============================================================================

// handleElasticsearchILM 处理 POST /api/v1/search/elasticsearch/ilm
func (s *Server) handleElasticsearchILM(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // list/get/create/delete/explain
		Policy   string `json:"policy"`
		Body     string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "list":
		script = `curl -s http://localhost:9200/_ilm/policy?pretty 2>/dev/null || echo "ES not available"`
	case "get":
		if req.Policy == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "policy is required"})
			return
		}
		script = fmt.Sprintf(`curl -s %s 2>/dev/null`, esAPIURL("_ilm/policy/"+req.Policy+"?pretty"))
	case "create":
		if req.Policy == "" || req.Body == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "policy and body are required"})
			return
		}
		payload, err := esQuoteJSONText(req.Body)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body: " + err.Error()})
			return
		}
		script = fmt.Sprintf(`curl -s -X PUT %s -H "Content-Type: application/json" -d %s 2>/dev/null`, esAPIURL("_ilm/policy/"+req.Policy), payload)
	case "delete":
		if req.Policy == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "policy is required"})
			return
		}
		script = fmt.Sprintf(`curl -s -X DELETE %s 2>/dev/null`, esAPIURL("_ilm/policy/"+req.Policy))
	case "explain":
		if req.Policy == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "policy is required"})
			return
		}
		script = `curl -s "http://localhost:9200/*/_ilm/explain?pretty" 2>/dev/null | head -30`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "elasticsearch_ilm_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// Elasticsearch 快照/恢复
// ============================================================================

// handleElasticsearchSnapshot 处理 POST /api/v1/search/elasticsearch/snapshot
func (s *Server) handleElasticsearchSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // create-repo/create-snapshot/restore/list/delete
		Repo     string `json:"repo"`
		Snapshot string `json:"snapshot"`
		Indices  string `json:"indices"`
		Location string `json:"location"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "create-repo":
		if req.Repo == "" || req.Location == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "repo and location are required"})
			return
		}
		// location 嵌在 JSON 里：改由 Go 层构造（JSON 转义交给标准库），再整体 Quote。
		payload, err := esJSONPayload(map[string]any{
			"type":     "fs",
			"settings": map[string]any{"location": req.Location},
		})
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid location: " + err.Error()})
			return
		}
		script = fmt.Sprintf(`curl -s -X PUT %s -H "Content-Type: application/json" -d %s 2>/dev/null`, esAPIURL("_snapshot/"+req.Repo), payload)
	case "create-snapshot":
		if req.Repo == "" || req.Snapshot == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "repo and snapshot are required"})
			return
		}
		indices := req.Indices
		if indices == "" {
			indices = "*"
		}
		payload, err := esJSONPayload(map[string]any{"indices": indices})
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid indices: " + err.Error()})
			return
		}
		script = fmt.Sprintf(`curl -s -X PUT %s -H "Content-Type: application/json" -d %s 2>/dev/null`, esAPIURL("_snapshot/"+req.Repo+"/"+req.Snapshot+"?wait_for_completion=true"), payload)
	case "restore":
		if req.Repo == "" || req.Snapshot == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "repo and snapshot are required"})
			return
		}
		script = fmt.Sprintf(`curl -s -X POST %s 2>/dev/null`, esAPIURL("_snapshot/"+req.Repo+"/"+req.Snapshot+"/_restore"))
	case "list":
		if req.Repo == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "repo is required"})
			return
		}
		script = fmt.Sprintf(`curl -s %s 2>/dev/null`, esAPIURL("_snapshot/"+req.Repo+"/_all?pretty"))
	case "delete":
		if req.Repo == "" || req.Snapshot == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "repo and snapshot are required"})
			return
		}
		script = fmt.Sprintf(`curl -s -X DELETE %s 2>/dev/null`, esAPIURL("_snapshot/"+req.Repo+"/"+req.Snapshot))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "elasticsearch_snapshot_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// Elasticsearch Reindex
// ============================================================================

// handleElasticsearchReindex 处理 POST /api/v1/search/elasticsearch/reindex
func (s *Server) handleElasticsearchReindex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Source   string `json:"source"`
		Dest     string `json:"dest"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if req.Source == "" || req.Dest == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "source and dest are required"})
		return
	}

	payload, err := esJSONPayload(map[string]any{
		"source": map[string]any{"index": req.Source},
		"dest":   map[string]any{"index": req.Dest},
	})
	if err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid reindex: " + err.Error()})
		return
	}
	script := fmt.Sprintf(`curl -s -X POST %s -H "Content-Type: application/json" -d %s 2>/dev/null`, esAPIURL("_reindex"), payload)
	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "elasticsearch_reindex")
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "reindex"})
}

// ============================================================================
// Elasticsearch 用户/角色/安全
// ============================================================================

// handleElasticsearchSecurity 处理 POST /api/v1/search/elasticsearch/security
func (s *Server) handleElasticsearchSecurity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // list-users/create-user/delete-user/list-roles/create-role
		User     string `json:"user"`
		Password string `json:"password"`
		Role     string `json:"role"`
		Roles    string `json:"roles"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "list-users":
		script = `curl -s http://localhost:9200/_security/user?pretty 2>/dev/null || echo "Security not enabled"`
	case "create-user":
		if req.User == "" || req.Password == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "user and password are required"})
			return
		}
		roles := req.Roles
		if roles == "" {
			roles = "[]"
		}
		// roles 是 JSON 数组文本：先校验合法性（非法 ⇒ 400），再作为 RawMessage 原样嵌入
		// （旧形态是裸插进 JSON 字面量，用户给个 `"` 就能破坏 JSON 结构）。
		if !json.Valid([]byte(roles)) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid roles: not valid JSON"})
			return
		}
		payload, err := esJSONPayload(map[string]any{
			"password": req.Password,
			"roles":    json.RawMessage(roles),
		})
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid user payload: " + err.Error()})
			return
		}
		script = fmt.Sprintf(`curl -s -X POST %s -H "Content-Type: application/json" -d %s 2>/dev/null`, esAPIURL("_security/user/"+req.User), payload)
	case "delete-user":
		if req.User == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "user is required"})
			return
		}
		script = fmt.Sprintf(`curl -s -X DELETE %s 2>/dev/null`, esAPIURL("_security/user/"+req.User))
	case "list-roles":
		script = `curl -s http://localhost:9200/_security/role?pretty 2>/dev/null || echo "Security not enabled"`
	case "create-role":
		if req.Role == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "role is required"})
			return
		}
		// 载荷是固定字面量（不含用户字段），只有 URL 需要 Quote。
		script = fmt.Sprintf(`curl -s -X POST %s -H "Content-Type: application/json" -d '{"cluster":["all"],"indices":[{"names":["*"],"privileges":["all"]}]}' 2>/dev/null`, esAPIURL("_security/role/"+req.Role))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "elasticsearch_security_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// Elasticsearch 升级
// ============================================================================

// handleElasticsearchUpgrade 处理 POST /api/v1/search/elasticsearch/upgrade
func (s *Server) handleElasticsearchUpgrade(w http.ResponseWriter, r *http.Request) {
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
		script = `echo "=== Current Version ===" && curl -s http://localhost:9200 2>/dev/null | grep number && echo "=== Upgrade Plan ===" && echo "1. Disable shard allocation" && echo "2. Stop node" && echo "3. Upgrade package" && echo "4. Start node" && echo "5. Re-enable shard allocation" && echo "6. Verify"`
	case "upgrade":
		version := req.TargetVersion
		if version == "" {
			version = "8.13.0"
		}
		script = fmt.Sprintf(`curl -s -X PUT "http://localhost:9200/_cluster/settings" -H "Content-Type: application/json" -d '{"transient":{"cluster.routing.allocation.enable":"none"}}' 2>/dev/null && docker stop elasticsearch && docker rm elasticsearch && docker run -d --name elasticsearch -p 9200:9200 -e "discovery.type=single-node" docker.elastic.co/elasticsearch/elasticsearch:%s && %s`, shellquote.Quote(version), esEcho("Upgraded to "+version))
	case "rollback":
		script = `docker stop elasticsearch && docker rm elasticsearch && docker run -d --name elasticsearch -p 9200:9200 -e "discovery.type=single-node" docker.elastic.co/elasticsearch/elasticsearch:8.12.0 && echo "Rollback complete"`
	case "version":
		script = `curl -s http://localhost:9200 2>/dev/null | grep number || echo "ES not running"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "elasticsearch_upgrade_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// Logstash 管理
// ============================================================================

// handleLogstash 处理 POST /api/v1/search/logstash
func (s *Server) handleLogstash(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // deploy/lifecycle/pipeline/status
		Mode     string `json:"mode"`
		Version  string `json:"version"`
		Name     string `json:"name"`
		Config   string `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "deploy":
		version := req.Version
		if version == "" {
			version = "8.13.0"
		}
		script := fmt.Sprintf(`docker run -d --name logstash -p 5044:5044 -p 9600:9600 -v /etc/logstash:/usr/share/logstash/config docker.elastic.co/logstash/logstash:%s 2>/dev/null && %s`, shellquote.Quote(version), esEcho("Logstash "+version+" deployed"))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "logstash_deploy")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy"})

	case "lifecycle":
		// handled by generic middleware action
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "use /middleware/{type}/{action} for lifecycle"})

	case "pipeline":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		config := req.Config
		if config == "" {
			config = "input { beats { port => 5044 } } output { elasticsearch { hosts => [\"localhost:9200\"] } }"
		}
		// config 是 logstash 配置文本（不是 JSON）：只能整体 Quote（Quote 会正确处理其中的
		// 单引号——旧形态 `echo '%s'` 遇单引号即破段 ⇒ 可执行注入）。
		// 文件名路径段同样 Quote（shell 剥引号后路径不变）。
		script := fmt.Sprintf(`%s > /etc/logstash/conf.d/%s.conf && %s`, esEcho(config), shellquote.Quote(req.Name), esEcho("Pipeline "+req.Name+" created"))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "logstash_pipeline")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "pipeline"})

	case "status":
		script := `curl -s http://localhost:9600/_node/stats 2>/dev/null | head -20 || echo "Logstash not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "logstash_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	case "reload":
		script := `curl -s -X POST http://localhost:9600/_node/reload 2>/dev/null && echo "Config reloaded" || echo "Reload not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "logstash_reload")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "reload"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// Kibana 管理
// ============================================================================

// handleKibana 处理 POST /api/v1/search/kibana
func (s *Server) handleKibana(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // deploy/lifecycle/status/spaces/dashboards/saved-objects
		Mode     string `json:"mode"`
		Version  string `json:"version"`
		ESUrl    string `json:"esUrl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "deploy":
		version := req.Version
		if version == "" {
			version = "8.13.0"
		}
		esURL := req.ESUrl
		if esURL == "" {
			esURL = "http://localhost:9200"
		}
		// ELASTICSEARCH_HOSTS 的值旧形态在双引号段内 ⇒ 整个 env 值整体 Quote。
		script := fmt.Sprintf(`docker run -d --name kibana -p 5601:5601 -e %s docker.elastic.co/kibana/kibana:%s 2>/dev/null && %s`, shellquote.Quote("ELASTICSEARCH_HOSTS="+esURL), shellquote.Quote(version), esEcho("Kibana "+version+" deployed"))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "kibana_deploy")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy"})

	case "lifecycle":
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "use /middleware/{type}/{action} for lifecycle"})

	case "status":
		script := `curl -s http://localhost:5601/api/status 2>/dev/null | head -20 || echo "Kibana not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "kibana_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	case "spaces":
		script := `curl -s http://localhost:5601/api/spaces/space 2>/dev/null | head -20 || echo "Kibana not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "kibana_spaces")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "spaces"})

	case "dashboards":
		script := `curl -s http://localhost:5601/api/saved_objects/_find?type=dashboard 2>/dev/null | head -20 || echo "Kibana not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "kibana_dashboards")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "dashboards"})

	case "saved-objects":
		script := `curl -s http://localhost:5601/api/saved_objects/_find 2>/dev/null | head -20 || echo "Kibana not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "kibana_saved-objects")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "saved-objects"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// Filebeat 管理
// ============================================================================

// handleFilebeat 处理 POST /api/v1/search/filebeat
func (s *Server) handleFilebeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // deploy/status/modules/config
		Version  string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "deploy":
		version := req.Version
		if version == "" {
			version = "8.13.0"
		}
		script := fmt.Sprintf(`docker run -d --name filebeat --user=root -v /var/log:/var/log:ro -v /etc/filebeat:/usr/share/filebeat docker.elastic.co/beats/filebeat:%s 2>/dev/null && %s`, shellquote.Quote(version), esEcho("Filebeat "+version+" deployed"))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "filebeat_deploy")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy"})

	case "status":
		script := `filebeat version 2>/dev/null || echo "Filebeat not installed" && echo "---" && systemctl status filebeat 2>/dev/null || echo "Filebeat not running"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "filebeat_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	case "modules":
		script := `filebeat modules list 2>/dev/null || echo "Filebeat not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "filebeat_modules")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "modules"})

	case "config":
		script := `cat /etc/filebeat/filebeat.yml 2>/dev/null || echo "Config not found"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "filebeat_config")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "config"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// OpenSearch 管理
// ============================================================================

// handleOpenSearch 处理 POST /api/v1/search/opensearch
func (s *Server) handleOpenSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // deploy/lifecycle/status/index
		Mode     string `json:"mode"`
		Version  string `json:"version"`
		Index    string `json:"index"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "deploy":
		version := req.Version
		if version == "" {
			version = "2.13.0"
		}
		script := fmt.Sprintf(`docker run -d --name opensearch -p 9200:9200 -p 9600:9600 -e "discovery.type=single-node" -e "OPENSEARCH_INITIAL_ADMIN_PASSWORD=admin" opensearchproject/opensearch:%s 2>/dev/null && %s`, shellquote.Quote(version), esEcho("OpenSearch "+version+" deployed"))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "opensearch_deploy")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "deploy"})

	case "lifecycle":
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "use /middleware/{type}/{action} for lifecycle"})

	case "status":
		script := `curl -s http://localhost:9200/_cluster/health 2>/dev/null || echo "OpenSearch not available"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "opensearch_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	case "index":
		if req.Index == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "index is required"})
			return
		}
		script := fmt.Sprintf(`curl -s -X PUT %s 2>/dev/null && %s`, esAPIURL(req.Index), esEcho("Index "+req.Index+" created"))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "opensearch_index")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "index"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// ============================================================================
// 路由注册
// ============================================================================

// registerELKRoutes 注册 ELK 路由
func (s *Server) registerELKRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/search/elasticsearch/deploy", s.handleElasticsearchDeploy)
	mux.HandleFunc("/api/v1/search/elasticsearch/lifecycle", s.handleElasticsearchLifecycle)
	mux.HandleFunc("/api/v1/search/elasticsearch/cluster", s.handleElasticsearchCluster)
	mux.HandleFunc("/api/v1/search/elasticsearch/index", s.handleElasticsearchIndex)
	mux.HandleFunc("/api/v1/search/elasticsearch/ilm", s.handleElasticsearchILM)
	mux.HandleFunc("/api/v1/search/elasticsearch/snapshot", s.handleElasticsearchSnapshot)
	mux.HandleFunc("/api/v1/search/elasticsearch/reindex", s.handleElasticsearchReindex)
	mux.HandleFunc("/api/v1/search/elasticsearch/security", s.handleElasticsearchSecurity)
	mux.HandleFunc("/api/v1/search/elasticsearch/upgrade", s.handleElasticsearchUpgrade)
	mux.HandleFunc("/api/v1/search/logstash", s.handleLogstash)
	mux.HandleFunc("/api/v1/search/kibana", s.handleKibana)
	mux.HandleFunc("/api/v1/search/filebeat", s.handleFilebeat)
	mux.HandleFunc("/api/v1/search/opensearch", s.handleOpenSearch)
}
