// Package controlplane elk_ext_ops.go 实现 ELK 扩展运维场景
// Elasticsearch: ingest pipeline, template, alias, data stream, force merge, clear cache, flush, refresh, rollover, cluster settings, monitoring, CCR
// Logstash: node stats, pipeline stats, plugin management, config validation
// Kibana: alerting, cases, connectors, actions, fleet
// Filebeat: modules enable/disable, autodiscover, inputs
// OpenSearch: ILM, snapshot, security (mirror ES)
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// ============================================================================
// Elasticsearch Ingest Pipeline
// ============================================================================

// handleElasticsearchPipeline 处理 POST /api/v1/search/elasticsearch/pipeline
func (s *Server) handleElasticsearchPipeline(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // list/get/create/delete/simulate
		Name     string `json:"name"`
		Body     string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "list":
		script = `curl -s http://localhost:9200/_ingest/pipeline?pretty 2>/dev/null || echo "ES not available"`
	case "get":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		script = fmt.Sprintf(`curl -s %s 2>/dev/null`, esAPIURL("_ingest/pipeline/"+req.Name+"?pretty"))
	case "create":
		if req.Name == "" || req.Body == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name and body are required"})
			return
		}
		// 注入面（批次 9）：URL 整体 Quote；payload 是用户 JSON，校验后整体 Quote
		// （旧形态 -d '%s' 遇单引号即破段 ⇒ 可执行注入）。
		payload, err := esQuoteJSONText(req.Body)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body: " + err.Error()})
			return
		}
		script = fmt.Sprintf(`curl -s -X PUT %s -H "Content-Type: application/json" -d %s 2>/dev/null`, esAPIURL("_ingest/pipeline/"+req.Name), payload)
	case "delete":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		script = fmt.Sprintf(`curl -s -X DELETE %s 2>/dev/null`, esAPIURL("_ingest/pipeline/"+req.Name))
	case "simulate":
		if req.Body == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "body is required"})
			return
		}
		payload, err := esQuoteJSONText(req.Body)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body: " + err.Error()})
			return
		}
		script = fmt.Sprintf(`curl -s -X POST %s -H "Content-Type: application/json" -d %s 2>/dev/null`, esAPIURL("_ingest/pipeline/_simulate"), payload)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "elasticsearch_pipeline_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// Elasticsearch Template 管理
// ============================================================================

// handleElasticsearchTemplate 处理 POST /api/v1/search/elasticsearch/template
func (s *Server) handleElasticsearchTemplate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // list/get/create/delete
		Name     string `json:"name"`
		Body     string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "list":
		script = `curl -s http://localhost:9200/_index_template?pretty 2>/dev/null || echo "ES not available"`
	case "get":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		script = fmt.Sprintf(`curl -s %s 2>/dev/null`, esAPIURL("_index_template/"+req.Name+"?pretty"))
	case "create":
		if req.Name == "" || req.Body == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name and body are required"})
			return
		}
		payload, err := esQuoteJSONText(req.Body)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body: " + err.Error()})
			return
		}
		script = fmt.Sprintf(`curl -s -X PUT %s -H "Content-Type: application/json" -d %s 2>/dev/null`, esAPIURL("_index_template/"+req.Name), payload)
	case "delete":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		script = fmt.Sprintf(`curl -s -X DELETE %s 2>/dev/null`, esAPIURL("_index_template/"+req.Name))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "elasticsearch_template_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// Elasticsearch Alias 管理
// ============================================================================

// handleElasticsearchAlias 处理 POST /api/v1/search/elasticsearch/alias
func (s *Server) handleElasticsearchAlias(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // list/add/remove
		Alias    string `json:"alias"`
		Index    string `json:"index"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "list":
		script = `curl -s http://localhost:9200/_aliases?pretty 2>/dev/null || echo "ES not available"`
	case "add":
		if req.Alias == "" || req.Index == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "alias and index are required"})
			return
		}
		// index / alias 嵌在 JSON 里：Go 层构造（JSON 转义交给标准库）后整体 Quote。
		payload, err := esJSONPayload(map[string]any{
			"actions": []any{map[string]any{"add": map[string]any{"index": req.Index, "alias": req.Alias}}},
		})
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid alias: " + err.Error()})
			return
		}
		script = fmt.Sprintf(`curl -s -X POST %s -H "Content-Type: application/json" -d %s 2>/dev/null`, esAPIURL("_aliases"), payload)
	case "remove":
		if req.Alias == "" || req.Index == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "alias and index are required"})
			return
		}
		payload, err := esJSONPayload(map[string]any{
			"actions": []any{map[string]any{"remove": map[string]any{"index": req.Index, "alias": req.Alias}}},
		})
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid alias: " + err.Error()})
			return
		}
		script = fmt.Sprintf(`curl -s -X POST %s -H "Content-Type: application/json" -d %s 2>/dev/null`, esAPIURL("_aliases"), payload)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "elasticsearch_alias_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// Elasticsearch Data Stream 管理
// ============================================================================

// handleElasticsearchDataStream 处理 POST /api/v1/search/elasticsearch/datastream
func (s *Server) handleElasticsearchDataStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // list/create/delete/rollover
		Name     string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "list":
		script = `curl -s http://localhost:9200/_data_stream?pretty 2>/dev/null || echo "ES not available"`
	case "create":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		script = fmt.Sprintf(`curl -s -X PUT %s 2>/dev/null`, esAPIURL("_data_stream/"+req.Name))
	case "delete":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		script = fmt.Sprintf(`curl -s -X DELETE %s 2>/dev/null`, esAPIURL("_data_stream/"+req.Name))
	case "rollover":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		script = fmt.Sprintf(`curl -s -X POST %s 2>/dev/null`, esAPIURL(req.Name+"/_rollover"))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "elasticsearch_datastream_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// Elasticsearch Force Merge / Clear Cache / Flush / Refresh
// ============================================================================

// handleElasticsearchMaintenance 处理 POST /api/v1/search/elasticsearch/maintenance
func (s *Server) handleElasticsearchMaintenance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID    string `json:"deviceID"`
		Action      string `json:"action"` // force-merge/clear-cache/flush/refresh/shrink/split
		Index       string `json:"index"`
		MaxSegments int    `json:"maxSegments"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	index := req.Index
	if index == "" {
		index = "_all"
	}

	var script string
	switch req.Action {
	case "force-merge":
		segments := req.MaxSegments
		if segments <= 0 {
			segments = 1
		}
		script = fmt.Sprintf(`curl -s -X POST %s 2>/dev/null`, esAPIURL(fmt.Sprintf("%s/_forcemerge?max_num_segments=%d", index, segments)))
	case "clear-cache":
		script = fmt.Sprintf(`curl -s -X POST %s 2>/dev/null`, esAPIURL(index+"/_cache/clear"))
	case "flush":
		script = fmt.Sprintf(`curl -s -X POST %s 2>/dev/null`, esAPIURL(index+"/_flush"))
	case "refresh":
		script = fmt.Sprintf(`curl -s -X POST %s 2>/dev/null`, esAPIURL(index+"/_refresh"))
	case "shrink":
		if index == "_all" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "index is required for shrink"})
			return
		}
		script = fmt.Sprintf(`curl -s -X POST %s 2>/dev/null`, esAPIURL(index+"/_shrink/"+index+"_shrunk"))
	case "split":
		if index == "_all" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "index is required for split"})
			return
		}
		script = fmt.Sprintf(`curl -s -X POST %s 2>/dev/null`, esAPIURL(index+"/_split/"+index+"_split"))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "elasticsearch_maintenance_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// Elasticsearch Cluster Settings
// ============================================================================

// handleElasticsearchClusterSettings 处理 POST /api/v1/search/elasticsearch/cluster-settings
func (s *Server) handleElasticsearchClusterSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // get/update
		Body     string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "get":
		script = `curl -s http://localhost:9200/_cluster/settings?pretty 2>/dev/null || echo "ES not available"`
	case "update":
		if req.Body == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "body is required"})
			return
		}
		payload, err := esQuoteJSONText(req.Body)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body: " + err.Error()})
			return
		}
		script = fmt.Sprintf(`curl -s -X PUT %s -H "Content-Type: application/json" -d %s 2>/dev/null`, esAPIURL("_cluster/settings"), payload)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "elasticsearch_cluster-settings_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// Elasticsearch CCR (Cross-Cluster Replication)
// ============================================================================

// handleElasticsearchCCR 处理 POST /api/v1/search/elasticsearch/ccr
func (s *Server) handleElasticsearchCCR(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // create-follower/list/pause/resume
		Index    string `json:"index"`
		Leader   string `json:"leader"` // leader index name
		Remote   string `json:"remote"` // remote cluster name
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "create-follower":
		if req.Index == "" || req.Leader == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "index and leader are required"})
			return
		}
		// remote / leader 嵌在 JSON 里：Go 层构造后整体 Quote。
		payload, err := esJSONPayload(map[string]any{
			"remote_cluster": req.Remote,
			"leader_index":   req.Leader,
		})
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid ccr: " + err.Error()})
			return
		}
		script = fmt.Sprintf(`curl -s -X PUT %s -H "Content-Type: application/json" -d %s 2>/dev/null`, esAPIURL(req.Index+"/_ccr/follow?wait_for_active_shards=1"), payload)
	case "list":
		script = `curl -s http://localhost:9200/_ccr/stats?pretty 2>/dev/null || echo "CCR not available"`
	case "pause":
		if req.Index == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "index is required"})
			return
		}
		script = fmt.Sprintf(`curl -s -X POST %s 2>/dev/null`, esAPIURL(req.Index+"/_ccr/pause_follow"))
	case "resume":
		if req.Index == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "index is required"})
			return
		}
		script = fmt.Sprintf(`curl -s -X POST %s 2>/dev/null`, esAPIURL(req.Index+"/_ccr/resume_follow"))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "elasticsearch_ccr_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// Elasticsearch Watcher/Alerting
// ============================================================================

// handleElasticsearchWatcher 处理 POST /api/v1/search/elasticsearch/watcher
func (s *Server) handleElasticsearchWatcher(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // list/get/create/delete/activate/deactivate
		WatchID  string `json:"watchId"`
		Body     string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "list":
		script = `curl -s http://localhost:9200/_watcher/watch/_query?pretty 2>/dev/null || echo "Watcher not available"`
	case "get":
		if req.WatchID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "watchId is required"})
			return
		}
		script = fmt.Sprintf(`curl -s %s 2>/dev/null`, esAPIURL("_watcher/watch/"+req.WatchID+"?pretty"))
	case "create":
		if req.WatchID == "" || req.Body == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "watchId and body are required"})
			return
		}
		payload, err := esQuoteJSONText(req.Body)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body: " + err.Error()})
			return
		}
		script = fmt.Sprintf(`curl -s -X PUT %s -H "Content-Type: application/json" -d %s 2>/dev/null`, esAPIURL("_watcher/watch/"+req.WatchID), payload)
	case "delete":
		if req.WatchID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "watchId is required"})
			return
		}
		script = fmt.Sprintf(`curl -s -X DELETE %s 2>/dev/null`, esAPIURL("_watcher/watch/"+req.WatchID))
	case "activate":
		if req.WatchID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "watchId is required"})
			return
		}
		script = fmt.Sprintf(`curl -s -X PUT %s 2>/dev/null`, esAPIURL("_watcher/watch/"+req.WatchID+"/_activate"))
	case "deactivate":
		if req.WatchID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "watchId is required"})
			return
		}
		script = fmt.Sprintf(`curl -s -X PUT %s 2>/dev/null`, esAPIURL("_watcher/watch/"+req.WatchID+"/_deactivate"))
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "elasticsearch_watcher_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// Logstash 扩展
// ============================================================================

// handleLogstashExt 处理 POST /api/v1/search/logstash/ext
func (s *Server) handleLogstashExt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // node-stats/pipeline-stats/plugins/validate
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "node-stats":
		script = `curl -s http://localhost:9600/_node/stats 2>/dev/null | head -30 || echo "Logstash not available"`
	case "pipeline-stats":
		script = `curl -s http://localhost:9600/_node/stats/pipelines 2>/dev/null | head -30 || echo "Logstash not available"`
	case "plugins":
		script = `curl -s http://localhost:9600/_node/plugins 2>/dev/null | head -30 || echo "Logstash not available"`
	case "validate":
		script = `logstash --config.test_and_exit -f /etc/logstash/conf.d/ 2>&1 | tail -5 || echo "Validation failed"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "logstash_ext_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// Kibana 扩展
// ============================================================================

// handleKibanaExt 处理 POST /api/v1/search/kibana/ext
func (s *Server) handleKibanaExt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/plugins/alerting/cases/connectors/actions/settings
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		script = `curl -s http://localhost:5601/api/status 2>/dev/null | head -20 || echo "Kibana not available"`
	case "plugins":
		script = `curl -s http://localhost:5601/api/status 2>/dev/null | grep -i plugin | head -20 || echo "Kibana not available"`
	case "alerting":
		script = `curl -s http://localhost:5601/api/alerting/rules/_find 2>/dev/null | head -20 || echo "Alerting not available"`
	case "cases":
		script = `curl -s http://localhost:5601/api/cases/_find 2>/dev/null | head -20 || echo "Cases not available"`
	case "connectors":
		script = `curl -s http://localhost:5601/api/actions/connectors 2>/dev/null | head -20 || echo "Connectors not available"`
	case "actions":
		script = `curl -s http://localhost:5601/api/actions 2>/dev/null | head -20 || echo "Actions not available"`
	case "settings":
		script = `curl -s http://localhost:5601/api/kibana/settings 2>/dev/null | head -20 || echo "Settings not available"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "kibana_ext_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// OpenSearch 扩展
// ============================================================================

// handleOpenSearchExt 处理 POST /api/v1/search/opensearch/ext
func (s *Server) handleOpenSearchExt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // ilm/snapshot/security/upgrade
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "ilm":
		script = `curl -s http://localhost:9200/_plugins/_ism/policies?pretty 2>/dev/null || echo "OpenSearch ISM not available"`
	case "snapshot":
		script = `curl -s http://localhost:9200/_snapshot/_all?pretty 2>/dev/null || echo "OpenSearch snapshot not available"`
	case "security":
		script = `curl -s http://localhost:9200/_plugins/_security/api/internalusers 2>/dev/null || echo "OpenSearch security not available"`
	case "upgrade":
		script = `echo "OpenSearch upgrade: docker pull opensearchproject/opensearch:latest && docker restart opensearch"`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "opensearch_ext_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// ============================================================================
// 路由注册
// ============================================================================

// registerELKExtendedRoutes 注册 ELK 扩展路由
func (s *Server) registerELKExtendedRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/search/elasticsearch/pipeline", s.handleElasticsearchPipeline)
	mux.HandleFunc("/api/v1/search/elasticsearch/template", s.handleElasticsearchTemplate)
	mux.HandleFunc("/api/v1/search/elasticsearch/alias", s.handleElasticsearchAlias)
	mux.HandleFunc("/api/v1/search/elasticsearch/datastream", s.handleElasticsearchDataStream)
	mux.HandleFunc("/api/v1/search/elasticsearch/maintenance", s.handleElasticsearchMaintenance)
	mux.HandleFunc("/api/v1/search/elasticsearch/cluster-settings", s.handleElasticsearchClusterSettings)
	mux.HandleFunc("/api/v1/search/elasticsearch/ccr", s.handleElasticsearchCCR)
	mux.HandleFunc("/api/v1/search/elasticsearch/watcher", s.handleElasticsearchWatcher)
	mux.HandleFunc("/api/v1/search/logstash/ext", s.handleLogstashExt)
	mux.HandleFunc("/api/v1/search/kibana/ext", s.handleKibanaExt)
	mux.HandleFunc("/api/v1/search/opensearch/ext", s.handleOpenSearchExt)
	s.registerElasticsearchExtRoutes(mux)
}
