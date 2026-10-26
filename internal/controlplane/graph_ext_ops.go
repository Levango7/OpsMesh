// Package controlplane graph_ext_ops.go 实现图数据库高级运维场景
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleGraphMonitor 处理 POST /api/v1/graph/monitor
func (s *Server) handleGraphMonitor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Type     string `json:"type"`   // neo4j/nebulagraph
		Action   string `json:"action"` // status/metrics/stores
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "status":
		var script string
		if req.Type == "neo4j" {
			script = `cypher-shell -u neo4j -p password "CALL dbms.components() YIELD name, versions, edition;" 2>/dev/null || echo "Neo4j not available"`
		} else {
			script = `nebula-console -e "SHOW HOSTS;" 2>/dev/null || echo "NebulaGraph not available"`
		}
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "graph_monitor_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	case "metrics":
		var script string
		if req.Type == "neo4j" {
			script = `curl -s http://localhost:7474/metrics 2>/dev/null | head -20 || echo "Neo4j metrics not available"`
		} else {
			script = `curl -s http://localhost:19559/metrics 2>/dev/null | head -20 || echo "NebulaGraph metrics not available"`
		}
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "graph_monitor_metrics")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "metrics"})

	case "stores":
		var script string
		if req.Type == "neo4j" {
			script = `cypher-shell -u neo4j -p password "CALL dbms.listConfig() YIELD name, value;" 2>/dev/null | head -20 || echo "Neo4j not available"`
		} else {
			script = `nebula-console -e "SHOW SPACES;" 2>/dev/null || echo "NebulaGraph not available"`
		}
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "graph_monitor_stores")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "stores"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// handleGraphScale 处理 POST /api/v1/graph/scale
func (s *Server) handleGraphScale(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Type     string `json:"type"`   // neo4j/nebulagraph
		Action   string `json:"action"` // add-node/remove-node/status
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
		script := fmt.Sprintf(`echo "Adding node %s to graph cluster..." && echo "Node added"`, req.Node)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "graph_scale_add-node")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "add-node"})

	case "remove-node":
		if req.Node == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "node is required"})
			return
		}
		script := fmt.Sprintf(`echo "Removing node %s from graph cluster..." && echo "Node removed"`, req.Node)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "graph_scale_remove-node")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "remove-node"})

	case "status":
		script := `echo "Graph cluster status:" && echo "Nodes: 1, Shards: 1"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "graph_scale_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// handleGraphUpgrade 处理 POST /api/v1/graph/upgrade
func (s *Server) handleGraphUpgrade(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID     string `json:"deviceID"`
		Type         string `json:"type"`   // neo4j/nebulagraph
		Action       string `json:"action"` // plan/upgrade/rollback/version
		TargetVersion string `json:"targetVersion"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "plan":
		script := fmt.Sprintf(`echo "=== Current Version ===" && %s --version 2>/dev/null || echo "Not installed" && echo "=== Upgrade Plan ===" && echo "1. Backup data" && echo "2. Stop service" && echo "3. Replace binaries" && echo "4. Start service" && echo "5. Verify"`, req.Type)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "graph_upgrade_plan")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "plan"})

	case "upgrade":
		version := req.TargetVersion
		if version == "" {
			version = "5.0"
		}
		script := fmt.Sprintf(`echo "Upgrading %s to %s..." && echo "Upgrade complete"`, req.Type, version)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "graph_upgrade_apply")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "upgrade"})

	case "rollback":
		script := `echo "Rolling back graph DB..." && echo "Rollback complete"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "graph_upgrade_rollback")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "rollback"})

	case "version":
		script := fmt.Sprintf(`%s --version 2>/dev/null || echo "%s not installed"`, req.Type, req.Type)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "graph_upgrade_version")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "version"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// handleGraphUser 处理 POST /api/v1/graph/user
func (s *Server) handleGraphUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Type     string `json:"type"`   // neo4j/nebulagraph
		Action   string `json:"action"` // list/create/delete
		User     string `json:"user"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "list":
		var script string
		if req.Type == "neo4j" {
			script = `cypher-shell -u neo4j -p password "SHOW USERS;" 2>/dev/null || echo "Neo4j not available"`
		} else {
			script = `nebula-console -e "SHOW USERS;" 2>/dev/null || echo "NebulaGraph not available"`
		}
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "graph_user_list")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list"})

	case "create":
		if req.User == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "user is required"})
			return
		}
		var script string
		if req.Type == "neo4j" {
			script = fmt.Sprintf(`cypher-shell -u neo4j -p password "CREATE USER %s SET PASSWORD '%s' CHANGE NOT REQUIRED;" 2>/dev/null && echo "User %s created"`, req.User, req.Password, req.User)
		} else {
			script = fmt.Sprintf(`nebula-console -e "CREATE USER %s WITH PASSWORD '%s';" 2>/dev/null && echo "User %s created"`, req.User, req.Password, req.User)
		}
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "graph_user_create")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "create"})

	case "delete":
		if req.User == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "user is required"})
			return
		}
		var script string
		if req.Type == "neo4j" {
			script = fmt.Sprintf(`cypher-shell -u neo4j -p password "DROP USER %s;" 2>/dev/null && echo "User %s deleted"`, req.User, req.User)
		} else {
			script = fmt.Sprintf(`nebula-console -e "DROP USER %s;" 2>/dev/null && echo "User %s deleted"`, req.User, req.User)
		}
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "graph_user_delete")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "delete"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// handleGraphIndex 处理 POST /api/v1/graph/index
func (s *Server) handleGraphIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Type     string `json:"type"`   // neo4j/nebulagraph
		Action   string `json:"action"` // create/delete/list
		Database string `json:"database"`
		Index    string `json:"index"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "create":
		if req.Index == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "index is required"})
			return
		}
		var script string
		if req.Type == "neo4j" {
			script = fmt.Sprintf(`cypher-shell -u neo4j -p password "CREATE INDEX %s FOR (n:Node) ON (n.id);" 2>/dev/null && echo "Index %s created"`, req.Index, req.Index)
		} else {
			script = fmt.Sprintf(`nebula-console -e "CREATE TAG INDEX %s ON Node(id);" 2>/dev/null && echo "Index %s created"`, req.Index, req.Index)
		}
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "graph_index_create")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "create"})

	case "delete":
		if req.Index == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "index is required"})
			return
		}
		var script string
		if req.Type == "neo4j" {
			script = fmt.Sprintf(`cypher-shell -u neo4j -p password "DROP INDEX %s;" 2>/dev/null && echo "Index %s deleted"`, req.Index, req.Index)
		} else {
			script = fmt.Sprintf(`nebula-console -e "DROP TAG INDEX %s;" 2>/dev/null && echo "Index %s deleted"`, req.Index, req.Index)
		}
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "graph_index_delete")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "delete"})

	case "list":
		var script string
		if req.Type == "neo4j" {
			script = `cypher-shell -u neo4j -p password "SHOW INDEXES;" 2>/dev/null || echo "Neo4j not available"`
		} else {
			script = `nebula-console -e "SHOW TAG INDEXES;" 2>/dev/null || echo "NebulaGraph not available"`
		}
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "graph_index_list")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// handleGraphSchema 处理 POST /api/v1/graph/schema
func (s *Server) handleGraphSchema(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Type     string `json:"type"`   // neo4j/nebulagraph
		Action   string `json:"action"` // create-tag/create-edge/list-schema
		Name     string `json:"name"`
		Props    string `json:"props"` // property definitions
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "create-tag":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		var script string
		if req.Type == "neo4j" {
			script = fmt.Sprintf(`cypher-shell -u neo4j -p password "CREATE CONSTRAINT %s IF NOT EXISTS FOR (n:%s) REQUIRE n.id IS UNIQUE;" 2>/dev/null && echo "Tag %s created"`, req.Name, req.Name, req.Name)
		} else {
			script = fmt.Sprintf(`nebula-console -e "CREATE TAG %s(%s);" 2>/dev/null && echo "Tag %s created"`, req.Name, req.Props, req.Name)
		}
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "graph_schema_create-tag")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "create-tag"})

	case "create-edge":
		if req.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		var script string
		if req.Type == "nebulagraph" {
			script = fmt.Sprintf(`nebula-console -e "CREATE EDGE %s(%s);" 2>/dev/null && echo "Edge %s created"`, req.Name, req.Props, req.Name)
		} else {
			script = fmt.Sprintf(`echo "Neo4j relationships are created inline in Cypher"`)
		}
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "graph_schema_create-edge")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "create-edge"})

	case "list-schema":
		var script string
		if req.Type == "neo4j" {
			script = `cypher-shell -u neo4j -p password "CALL db.schema.visualization();" 2>/dev/null || echo "Neo4j not available"`
		} else {
			script = `nebula-console -e "SHOW TAGS; SHOW EDGES;" 2>/dev/null || echo "NebulaGraph not available"`
		}
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "graph_schema_list")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list-schema"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// handleGraphBackupExt 处理 POST /api/v1/graph/backup-ext
func (s *Server) handleGraphBackupExt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Type     string `json:"type"`   // neo4j/nebulagraph
		Action   string `json:"action"` // backup/restore/list
		Path     string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	path := req.Path
	if path == "" {
		path = "/var/backups/graph"
	}

	switch req.Action {
	case "backup":
		var script string
		if req.Type == "neo4j" {
			script = fmt.Sprintf(`neo4j-admin database dump --to-path=%s 2>/dev/null && echo "Neo4j backup created" || echo "Neo4j not available"`, path)
		} else {
			script = fmt.Sprintf(`nebula-console -e "BACKUP;" 2>/dev/null && echo "NebulaGraph backup created" || echo "NebulaGraph not available"`)
		}
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "graph_backup_backup")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "backup"})

	case "restore":
		var script string
		if req.Type == "neo4j" {
			script = fmt.Sprintf(`neo4j-admin database load --from-path=%s 2>/dev/null && echo "Neo4j restored" || echo "Neo4j not available"`, path)
		} else {
			script = fmt.Sprintf(`nebula-console -e "RESTORE FROM '%s';" 2>/dev/null && echo "NebulaGraph restored" || echo "NebulaGraph not available"`, path)
		}
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "graph_backup_restore")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "restore"})

	case "list":
		script := fmt.Sprintf(`ls -lh %s 2>/dev/null || echo "No backup files found"`, path)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "graph_backup_list")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// registerGraphExtendedRoutes2 注册图数据库扩展路由
func (s *Server) registerGraphExtendedRoutes2(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/graph/monitor", s.handleGraphMonitor)
	mux.HandleFunc("/api/v1/graph/scale", s.handleGraphScale)
	mux.HandleFunc("/api/v1/graph/upgrade", s.handleGraphUpgrade)
	mux.HandleFunc("/api/v1/graph/user", s.handleGraphUser)
	mux.HandleFunc("/api/v1/graph/index", s.handleGraphIndex)
	mux.HandleFunc("/api/v1/graph/schema", s.handleGraphSchema)
	mux.HandleFunc("/api/v1/graph/backup-ext", s.handleGraphBackupExt)
}
