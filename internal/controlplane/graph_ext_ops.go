// Package controlplane graph_ext_ops.go 实现图数据库高级运维场景
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/pkg/shellquote"
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
		// 注入面（批次 10）：node 是用户字段，旧形态落在回显的**双引号段**内 ⇒
		// 段内 $ 与反引号照样展开 ⇒ 整条回显消息整体 Quote。
		script := "echo " + opsEcho("Adding node %s to graph cluster...", req.Node) + ` && echo "Node added"`
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
		// 注入面（批次 10）：同 add-node——回显段整体 Quote。
		script := "echo " + opsEcho("Removing node %s from graph cluster...", req.Node) + ` && echo "Node removed"`
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
		DeviceID      string `json:"deviceID"`
		Type          string `json:"type"`   // neo4j/nebulagraph
		Action        string `json:"action"` // plan/upgrade/rollback/version
		TargetVersion string `json:"targetVersion"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "plan":
		// 注入面（批次 10）：req.Type 在这里是**命令名**（要当二进制执行）⇒ 这是
		// **命令注入**（比 shell 注入更严重：载荷可以指向任意二进制/脚本），
		// Quote 挡不住 ⇒ 必须枚举白名单（复用 graph_ops.go 的 allowedGraphTypes）。
		gt, err := graphCommandType(req.Type)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		script := fmt.Sprintf(`echo "=== Current Version ===" && %s --version 2>/dev/null || echo "Not installed" && echo "=== Upgrade Plan ===" && echo "1. Backup data" && echo "2. Stop service" && echo "3. Replace binaries" && echo "4. Start service" && echo "5. Verify"`, shellquote.Quote(gt))
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
		// 注入面（批次 10）：req.Type 是命令名（须白名单），version 是回显里的用户
		// 字段（双引号段内 $ / 反引号照样展开）⇒ 两段都收口。
		gt, err := graphCommandType(req.Type)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		script := "echo " + opsEcho("Upgrading %s to %s...", gt, version) + ` && echo "Upgrade complete"`
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
		// 注入面（批次 10）：req.Type 在这里是**命令名** ⇒ 白名单；回显段同样整体 Quote。
		gt, err := graphCommandType(req.Type)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		script := gt + " --version 2>/dev/null || echo " + opsEcho("%s not installed", gt)
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
		// 注入面（批次 10）：**双层**注入面——user 是图库层的裸标识符、password 是
		// 图库层的字符串字面量，而整条语句又落在 shell 的双引号段里。三层各按各的
		// 规则收口：标识符/密钥走白名单（graphIdent / graphSecret），整条语句走
		// shellquote.Quote（cypherShell / nebulaConsole）。
		user, err := graphIdent(req.User)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		secret, err := graphSecret(req.Password)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		var script string
		if req.Type == "neo4j" {
			script = cypherShell(fmt.Sprintf("CREATE USER %s SET PASSWORD '%s' CHANGE NOT REQUIRED;", user, secret)) + " 2>/dev/null && echo " + opsEcho("User %s created", user)
		} else {
			script = nebulaConsole(fmt.Sprintf("CREATE USER %s WITH PASSWORD '%s';", user, secret)) + " 2>/dev/null && echo " + opsEcho("User %s created", user)
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
		// 注入面（批次 10）：user 是图库层裸标识符（无转义空间 ⇒ 白名单），
		// 整条语句走 shellquote.Quote。
		user, err := graphIdent(req.User)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		var script string
		if req.Type == "neo4j" {
			script = cypherShell(fmt.Sprintf("DROP USER %s;", user)) + " 2>/dev/null && echo " + opsEcho("User %s deleted", user)
		} else {
			script = nebulaConsole(fmt.Sprintf("DROP USER %s;", user)) + " 2>/dev/null && echo " + opsEcho("User %s deleted", user)
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
		// 注入面（批次 10）：index 是图库层裸标识符（无转义空间 ⇒ 白名单）。
		idx, err := graphIdent(req.Index)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		var script string
		if req.Type == "neo4j" {
			script = cypherShell(fmt.Sprintf("CREATE INDEX %s FOR (n:Node) ON (n.id);", idx)) + " 2>/dev/null && echo " + opsEcho("Index %s created", idx)
		} else {
			script = nebulaConsole(fmt.Sprintf("CREATE TAG INDEX %s ON Node(id);", idx)) + " 2>/dev/null && echo " + opsEcho("Index %s created", idx)
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
		// 注入面（批次 10）：index 是图库层裸标识符（无转义空间 ⇒ 白名单）。
		idx, err := graphIdent(req.Index)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		var script string
		if req.Type == "neo4j" {
			script = cypherShell(fmt.Sprintf("DROP INDEX %s;", idx)) + " 2>/dev/null && echo " + opsEcho("Index %s deleted", idx)
		} else {
			script = nebulaConsole(fmt.Sprintf("DROP TAG INDEX %s;", idx)) + " 2>/dev/null && echo " + opsEcho("Index %s deleted", idx)
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
		// 注入面（批次 10）：name 是图库层裸标识符（白名单）；props 是**属性定义
		// 片段**（如 `name string, age int`），它落在括号里，能改写语句结构的字符
		// （括号/分号/引号/美元号…）一律拒收（graphPropDefs）。
		name, err := graphIdent(req.Name)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		props, err := graphPropDefs(req.Props)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		var script string
		if req.Type == "neo4j" {
			script = cypherShell(fmt.Sprintf("CREATE CONSTRAINT %s IF NOT EXISTS FOR (n:%s) REQUIRE n.id IS UNIQUE;", name, name)) + " 2>/dev/null && echo " + opsEcho("Tag %s created", name)
		} else {
			script = nebulaConsole(fmt.Sprintf("CREATE TAG %s(%s);", name, props)) + " 2>/dev/null && echo " + opsEcho("Tag %s created", name)
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
		// 注入面（批次 10）：同 create-tag——name 是标识符、props 是属性定义片段。
		name, err := graphIdent(req.Name)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		props, err := graphPropDefs(req.Props)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		var script string
		if req.Type == "nebulagraph" {
			script = nebulaConsole(fmt.Sprintf("CREATE EDGE %s(%s);", name, props)) + " 2>/dev/null && echo " + opsEcho("Edge %s created", name)
		} else {
			script = `echo "Neo4j relationships are created inline in Cypher"`
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
		// 注入面（批次 10）：path 会进 nGQL 的字符串字面量（restore 分支）⇒ 走
		// 与方言无关的路径白名单（graphPath），再由 shellquote.Quote 收 shell 层。
		safePath, err := graphPath(path)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		var script string
		if req.Type == "neo4j" {
			script = fmt.Sprintf("neo4j-admin database dump --to-path=%s 2>/dev/null && echo \"Neo4j backup created\" || echo \"Neo4j not available\"", shellquote.Quote(safePath))
		} else {
			script = `nebula-console -e "BACKUP;" 2>/dev/null && echo "NebulaGraph backup created" || echo "NebulaGraph not available"`
		}
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "graph_backup_backup")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "backup"})

	case "restore":
		// 注入面（批次 10）：path 进 nGQL 字符串字面量 ⇒ 路径白名单 + shell Quote。
		safePath, err := graphPath(path)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		var script string
		if req.Type == "neo4j" {
			script = fmt.Sprintf("neo4j-admin database load --from-path=%s 2>/dev/null && echo \"Neo4j restored\" || echo \"Neo4j not available\"", shellquote.Quote(safePath))
		} else {
			script = nebulaConsole(fmt.Sprintf("RESTORE FROM '%s';", safePath)) + ` 2>/dev/null && echo "NebulaGraph restored" || echo "NebulaGraph not available"`
		}
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "graph_backup_restore")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "restore"})

	case "list":
		// 注入面（批次 10）：path 进 shell 命令 ⇒ 整体 Quote。
		script := fmt.Sprintf("ls -lh %s 2>/dev/null || echo \"No backup files found\"", shellquote.Quote(path))
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
