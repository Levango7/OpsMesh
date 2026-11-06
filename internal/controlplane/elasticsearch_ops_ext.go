package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/pkg/shellquote"
)

var esExtRouteMu sync.Mutex
var esExtRouteRegistered = map[*http.ServeMux]bool{}

func (s *Server) handleElasticsearchNodeDrain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requireProd(w, r, "task:write"); !ok {
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`
		Node     string `json:"node"`
		NodeName string `json:"nodeName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	node := req.Node
	if node == "" {
		node = req.NodeName
	}

	var script string
	switch req.Action {
	case "drain":
		if node == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "node is required"})
			return
		}
		// 白名单而非转义：node 嵌在 shell 单引号内的 JSON 文本里（`-d '{"...":"%s"}'`），
		// 加引号会破坏外层、且 JSON 语法还要求转义 " 与 \——那里没有合法转义空间，
		// 只能限字符集（2026-11-05 修正，原实现裸拼可经 ' 或 $( ) 注入）。
		if err := shellquote.ValidateName(node, 255); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		script = fmt.Sprintf(`curl -X PUT "http://localhost:9200/_cluster/settings" -d '{"transient":{"cluster.routing.allocation.exclude._name":"%s"}}'`, node)
	case "undrain":
		script = `curl -X PUT "http://localhost:9200/_cluster/settings" -d '{"transient":{"cluster.routing.allocation.exclude._name":null}}'`
	case "status":
		script = `echo "=== Nodes ===" && curl -s http://localhost:9200/_cat/nodes?v && echo "=== Allocation Exclude ===" && curl -s http://localhost:9200/_cluster/settings?pretty | grep -A2 exclude`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "elasticsearch_node-drain_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

func (s *Server) handleElasticsearchShardRelocate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requireProd(w, r, "task:write"); !ok {
		return
	}

	var req struct {
		DeviceID  string `json:"deviceID"`
		Action    string `json:"action"`
		Index     string `json:"index"`
		Shard     int    `json:"shard"`
		From      string `json:"from"`
		FromNode  string `json:"fromNode"`
		FromSnake string `json:"from_node"`
		To        string `json:"to"`
		ToNode    string `json:"toNode"`
		ToSnake   string `json:"to_node"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	from := req.From
	if from == "" {
		from = req.FromNode
	}
	if from == "" {
		from = req.FromSnake
	}
	to := req.To
	if to == "" {
		to = req.ToNode
	}
	if to == "" {
		to = req.ToSnake
	}

	var script string
	switch req.Action {
	case "relocate":
		if req.Index == "" || from == "" || to == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "index, from and to are required"})
			return
		}
		// 同 drain：三值都嵌在 shell 单引号内的 JSON 文本里，只能白名单。
		if err := shellquote.ValidateName(req.Index, 255); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "index: " + err.Error()})
			return
		}
		if err := shellquote.ValidateName(from, 255); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "from: " + err.Error()})
			return
		}
		if err := shellquote.ValidateName(to, 255); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "to: " + err.Error()})
			return
		}
		script = fmt.Sprintf(`curl -X POST "http://localhost:9200/_cluster/reroute" -d '{"commands":[{"move":{"index":"%s","shard":%d,"from_node":"%s","to_node":"%s"}}]}'`, req.Index, req.Shard, from, to)
	case "suggest":
		script = `curl -s http://localhost:9200/_cluster/allocation/explain?pretty`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "elasticsearch_shard-relocate_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

func (s *Server) handleElasticsearchDiagnostic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requireProd(w, r, "task:write"); !ok {
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "bundle":
		script = `D=/tmp/es-diag-$(date +%s); mkdir -p $D; curl -s http://localhost:9200/_cluster/health?pretty > $D/cluster_health.json; curl -s http://localhost:9200/_nodes/stats?pretty > $D/node_stats.json; curl -s http://localhost:9200/_nodes/hot_threads > $D/hot_threads.txt; curl -s http://localhost:9200/_tasks?pretty > $D/tasks.json; curl -s http://localhost:9200/_ilm/policy?pretty > $D/ilm.json; curl -s http://localhost:9200/_snapshot/_all?pretty > $D/snapshots.json; curl -s "http://localhost:9200/_cat/shards?v" > $D/cat_shards.txt; curl -s "http://localhost:9200/_cat/indices?v" > $D/cat_indices.txt; curl -s http://localhost:9200/_cluster/settings?pretty > $D/settings.json; tar -czf /tmp/es-diagnostic-bundle.tar.gz -C /tmp $(basename $D) && echo "Diagnostic bundle created: /tmp/es-diagnostic-bundle.tar.gz"; rm -rf $D`
	case "health-check":
		script = `echo "=== Cluster Health ===" && curl -s http://localhost:9200/_cluster/health?pretty && echo "=== Node Status ===" && curl -s http://localhost:9200/_cat/nodes?v && echo "=== Unassigned Shards ===" && curl -s "http://localhost:9200/_cat/shards?v" | grep UNASSIGNED; echo "=== Pending Tasks ===" && curl -s http://localhost:9200/_cluster/pending_tasks?pretty`
	case "shard-allocation-explain":
		script = `curl -s http://localhost:9200/_cluster/allocation/explain?pretty`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "elasticsearch_diagnostic_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

func (s *Server) registerElasticsearchExtRoutes(mux *http.ServeMux) {
	esExtRouteMu.Lock()
	defer esExtRouteMu.Unlock()
	if esExtRouteRegistered[mux] {
		return
	}
	esExtRouteRegistered[mux] = true
	mux.HandleFunc("/api/v1/search/elasticsearch/node-drain", s.handleElasticsearchNodeDrain)
	mux.HandleFunc("/api/v1/search/elasticsearch/shard-relocate", s.handleElasticsearchShardRelocate)
	mux.HandleFunc("/api/v1/search/elasticsearch/diagnostic", s.handleElasticsearchDiagnostic)
}
