// Package controlplane elk_ops.go 实现 ELK (Elasticsearch/Logstash/Kibana) 全面运维场景
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
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

	var script string
	if mode == "cluster" {
		script = fmt.Sprintf(`docker run -d --name elasticsearch -p %d:9200 -p 9300:9300 -e "discovery.type=zen" -e "ES_JAVA_OPTS=-Xms%s -Xmx%s" -e "xpack.security.enabled=false" docker.elastic.co/elasticsearch/elasticsearch:%s 2>/dev/null && echo "Elasticsearch %s deployed (cluster mode)"`, port, memLimit, memLimit, version, version)
	} else {
		script = fmt.Sprintf(`docker run -d --name elasticsearch -p %d:9200 -e "discovery.type=single-node" -e "ES_JAVA_OPTS=-Xms%s -Xmx%s" -e "xpack.security.enabled=false" docker.elastic.co/elasticsearch/elasticsearch:%s 2>/dev/null && echo "Elasticsearch %s deployed (single mode)"`, port, memLimit, memLimit, version, version)
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
		script = fmt.Sprintf(`curl -s -X PUT "http://localhost:9200/%s" -H "Content-Type: application/json" -d '%s' 2>/dev/null`, req.Index, body)
	case "delete":
		if req.Index == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "index is required"})
			return
		}
		script = fmt.Sprintf(`curl -s -X DELETE "http://localhost:9200/%s" 2>/dev/null`, req.Index)
	case "mapping":
		if req.Index == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "index is required"})
			return
		}
		script = fmt.Sprintf(`curl -s "http://localhost:9200/%s/_mapping?pretty" 2>/dev/null`, req.Index)
	case "settings":
		if req.Index == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "index is required"})
			return
		}
		script = fmt.Sprintf(`curl -s "http://localhost:9200/%s/_settings?pretty" 2>/dev/null`, req.Index)
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
		script = fmt.Sprintf(`curl -s http://localhost:9200/_ilm/policy/%s?pretty 2>/dev/null`, req.Policy)
	case "create":
		if req.Policy == "" || req.Body == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "policy and body are required"})
			return
		}
		script = fmt.Sprintf(`curl -s -X PUT "http://localhost:9200/_ilm/policy/%s" -H "Content-Type: application/json" -d '%s' 2>/dev/null`, req.Policy, req.Body)
	case "delete":
		if req.Policy == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "policy is required"})
			return
		}
		script = fmt.Sprintf(`curl -s -X DELETE "http://localhost:9200/_ilm/policy/%s" 2>/dev/null`, req.Policy)
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
		script = fmt.Sprintf(`curl -s -X PUT "http://localhost:9200/_snapshot/%s" -H "Content-Type: application/json" -d '{"type":"fs","settings":{"location":"%s"}}' 2>/dev/null`, req.Repo, req.Location)
	case "create-snapshot":
		if req.Repo == "" || req.Snapshot == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "repo and snapshot are required"})
			return
		}
		indices := req.Indices
		if indices == "" {
			indices = "*"
		}
		script = fmt.Sprintf(`curl -s -X PUT "http://localhost:9200/_snapshot/%s/%s?wait_for_completion=true" -H "Content-Type: application/json" -d '{"indices":"%s"}' 2>/dev/null`, req.Repo, req.Snapshot, indices)
	case "restore":
		if req.Repo == "" || req.Snapshot == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "repo and snapshot are required"})
			return
		}
		script = fmt.Sprintf(`curl -s -X POST "http://localhost:9200/_snapshot/%s/%s/_restore" 2>/dev/null`, req.Repo, req.Snapshot)
	case "list":
		if req.Repo == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "repo is required"})
			return
		}
		script = fmt.Sprintf(`curl -s "http://localhost:9200/_snapshot/%s/_all?pretty" 2>/dev/null`, req.Repo)
	case "delete":
		if req.Repo == "" || req.Snapshot == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "repo and snapshot are required"})
			return
		}
		script = fmt.Sprintf(`curl -s -X DELETE "http://localhost:9200/_snapshot/%s/%s" 2>/dev/null`, req.Repo, req.Snapshot)
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

	script := fmt.Sprintf(`curl -s -X POST "http://localhost:9200/_reindex" -H "Content-Type: application/json" -d '{"source":{"index":"%s"},"dest":{"index":"%s"}}' 2>/dev/null`, req.Source, req.Dest)
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
		script = fmt.Sprintf(`curl -s -X POST "http://localhost:9200/_security/user/%s" -H "Content-Type: application/json" -d '{"password":"%s","roles":%s}' 2>/dev/null`, req.User, req.Password, roles)
	case "delete-user":
		if req.User == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "user is required"})
			return
		}
		script = fmt.Sprintf(`curl -s -X DELETE "http://localhost:9200/_security/user/%s" 2>/dev/null`, req.User)
	case "list-roles":
		script = `curl -s http://localhost:9200/_security/role?pretty 2>/dev/null || echo "Security not enabled"`
	case "create-role":
		if req.Role == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "role is required"})
			return
		}
		script = fmt.Sprintf(`curl -s -X POST "http://localhost:9200/_security/role/%s" -H "Content-Type: application/json" -d '{"cluster":["all"],"indices":[{"names":["*"],"privileges":["all"]}]}' 2>/dev/null`, req.Role)
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
		script = fmt.Sprintf(`curl -s -X PUT "http://localhost:9200/_cluster/settings" -H "Content-Type: application/json" -d '{"transient":{"cluster.routing.allocation.enable":"none"}}' 2>/dev/null && docker stop elasticsearch && docker rm elasticsearch && docker run -d --name elasticsearch -p 9200:9200 -e "discovery.type=single-node" docker.elastic.co/elasticsearch/elasticsearch:%s && echo "Upgraded to %s"`, version, version)
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
		script := fmt.Sprintf(`docker run -d --name logstash -p 5044:5044 -p 9600:9600 -v /etc/logstash:/usr/share/logstash/config docker.elastic.co/logstash/logstash:%s 2>/dev/null && echo "Logstash %s deployed"`, version, version)
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
		script := fmt.Sprintf(`echo '%s' > /etc/logstash/conf.d/%s.conf && echo "Pipeline %s created"`, config, req.Name, req.Name)
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
		script := fmt.Sprintf(`docker run -d --name kibana -p 5601:5601 -e "ELASTICSEARCH_HOSTS=%s" docker.elastic.co/kibana/kibana:%s 2>/dev/null && echo "Kibana %s deployed"`, esURL, version, version)
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
		script := fmt.Sprintf(`docker run -d --name filebeat --user=root -v /var/log:/var/log:ro -v /etc/filebeat:/usr/share/filebeat docker.elastic.co/beats/filebeat:%s 2>/dev/null && echo "Filebeat %s deployed"`, version, version)
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
		script := fmt.Sprintf(`docker run -d --name opensearch -p 9200:9200 -p 9600:9600 -e "discovery.type=single-node" -e "OPENSEARCH_INITIAL_ADMIN_PASSWORD=admin" opensearchproject/opensearch:%s 2>/dev/null && echo "OpenSearch %s deployed"`, version, version)
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
		script := fmt.Sprintf(`curl -s -X PUT "http://localhost:9200/%s" 2>/dev/null && echo "Index %s created"`, req.Index, req.Index)
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
