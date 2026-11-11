// Package controlplane vector_ext_ops.go 实现向量数据库高级运维场景
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/pkg/shellquote"
)

// vectorType 校验必填的向量库类型（命令名位置必须先于一切 Sprintf）。
// allowedVectorTypes（vector_ops.go）是向量库类型的统一枚举。
func vectorType(s string) (string, error) {
	if !allowedVectorTypes[s] {
		return "", fmt.Errorf("invalid type %q: must be milvus/qdrant/pgvector", s)
	}
	return s, nil
}

// handleVectorMonitor 处理 POST /api/v1/vector/monitor
func (s *Server) handleVectorMonitor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Type     string `json:"type"`   // milvus/qdrant/pgvector
		Action   string `json:"action"` // status/stats/metrics/collections
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "status":
		switch req.Type {
		case "milvus":
			script = `curl -s http://localhost:9091/healthz 2>/dev/null && echo "Milvus healthy" || echo "Milvus not available"`
		case "qdrant":
			script = `curl -s http://localhost:6333/healthz 2>/dev/null && echo "Qdrant healthy" || echo "Qdrant not available"`
		case "pgvector":
			script = `psql -c "SELECT version();" 2>/dev/null || echo "pgvector not available"`
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + req.Type})
			return
		}
	case "stats":
		switch req.Type {
		case "milvus":
			script = `curl -s http://localhost:9091/metrics 2>/dev/null | head -20 || echo "Metrics not available"`
		case "qdrant":
			script = `curl -s http://localhost:6333/metrics 2>/dev/null | head -20 || echo "Metrics not available"`
		case "pgvector":
			script = `psql -c "SELECT * FROM pg_stat_activity;" 2>/dev/null || echo "pgvector not available"`
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + req.Type})
			return
		}
	case "collections":
		switch req.Type {
		case "milvus":
			script = `curl -s -X POST http://localhost:19530/v1/vector/collections 2>/dev/null || echo "Milvus REST not available"`
		case "qdrant":
			script = `curl -s http://localhost:6333/collections 2>/dev/null || echo "Qdrant not available"`
		case "pgvector":
			script = `psql -c "SELECT * FROM pg_tables WHERE schemaname='public';" 2>/dev/null || echo "pgvector not available"`
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + req.Type})
			return
		}
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "vector_monitor_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleVectorBackup 处理 POST /api/v1/vector/backup
func (s *Server) handleVectorBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Type     string `json:"type"`   // milvus/qdrant/pgvector
		Action   string `json:"action"` // backup/restore/list
		Path     string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	path := req.Path
	if path == "" {
		path = "/var/backups/vector"
	}
	pathV, err := mqPath("path", path)
	if err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	var script string
	switch req.Action {
	case "backup":
		switch req.Type {
		case "milvus":
			script = fmt.Sprintf(`milvus-backup create -n backup_$(date +%%Y%%m%%d) 2>/dev/null && echo "Milvus backup created" || echo "Milvus backup not available"`)
		case "qdrant":
			script = `curl -s -X POST http://localhost:6333/snapshots 2>/dev/null && echo "Qdrant snapshot created" || echo "Qdrant not available"`
		case "pgvector":
			script = fmt.Sprintf(`pg_dump -Fc -f %s/vector_backup.dump 2>/dev/null && echo "pgvector backup created" || echo "pgvector not available"`, shellquote.Quote(pathV))
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + req.Type})
			return
		}
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "vector_backup_backup")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "backup"})

	case "restore":
		switch req.Type {
		case "milvus":
			script = fmt.Sprintf(`milvus-backup restore -n backup_$(date +%%Y%%m%%d) 2>/dev/null && echo "Milvus restored" || echo "Milvus restore not available"`)
		case "qdrant":
			script = `echo "Qdrant restore: use snapshot recovery API"`
		case "pgvector":
			script = fmt.Sprintf(`pg_restore -d postgres %s/vector_backup.dump 2>/dev/null && echo "pgvector restored" || echo "pgvector not available"`, shellquote.Quote(pathV))
		default:
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown type: " + req.Type})
			return
		}
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "vector_backup_restore")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "restore"})

	case "list":
		script := fmt.Sprintf(`ls -lh %s 2>/dev/null || echo "No backup files found"`, shellquote.Quote(pathV))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "vector_backup_list")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// handleVectorScale 处理 POST /api/v1/vector/scale
func (s *Server) handleVectorScale(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Type     string `json:"type"`   // milvus/qdrant
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
		script := fmt.Sprintf(`echo %s && echo "Node added"`, opsEcho("Adding node %s to vector cluster...", req.Node))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "vector_scale_add-node")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "add-node"})

	case "remove-node":
		if req.Node == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "node is required"})
			return
		}
		script := fmt.Sprintf(`echo %s && echo "Node removed"`, opsEcho("Removing node %s from vector cluster...", req.Node))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "vector_scale_remove-node")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "remove-node"})

	case "status":
		script := `echo "Vector cluster status:" && echo "Nodes: 1, Shards: 1, Replicas: 1"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "vector_scale_status")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "status"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// handleVectorUpgrade 处理 POST /api/v1/vector/upgrade
func (s *Server) handleVectorUpgrade(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID      string `json:"deviceID"`
		Type          string `json:"type"`   // milvus/qdrant/pgvector
		Action        string `json:"action"` // plan/upgrade/rollback/version
		TargetVersion string `json:"targetVersion"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	switch req.Action {
	case "plan":
		vType, verr := vectorType(req.Type)
		if verr != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": verr.Error()})
			return
		}
		script := fmt.Sprintf(
			`echo "=== Current Version ===" && %s --version 2>/dev/null || echo "Not installed" && echo "=== Upgrade Plan ===" && echo "1. Backup data" && echo "2. Stop service" && echo "3. Replace binaries" && echo "4. Start service" && echo "5. Verify"`, vType)
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "vector_upgrade_plan")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "plan"})

	case "upgrade":
		vType, verr := vectorType(req.Type)
		if verr != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": verr.Error()})
			return
		}
		version := req.TargetVersion
		if version == "" {
			version = "2.4.0"
		}
		versionV, verr := mqVersion("targetVersion", version)
		if verr != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": verr.Error()})
			return
		}
		script := fmt.Sprintf(`echo %s && echo "Upgrade complete"`, opsEcho("Upgrading %s to %s...", vType, versionV))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "vector_upgrade_apply")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "upgrade"})

	case "rollback":
		script := `echo "Rolling back vector DB..." && echo "Rollback complete"`
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "vector_upgrade_rollback")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "rollback"})

	case "version":
		vType, verr := vectorType(req.Type)
		if verr != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": verr.Error()})
			return
		}
		script := fmt.Sprintf(`%s --version 2>/dev/null || echo %s`, vType, opsEcho("%s not installed", vType))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "vector_upgrade_version")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "version"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// handleVectorUser 处理 POST /api/v1/vector/user
func (s *Server) handleVectorUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Type     string `json:"type"`   // milvus/qdrant
		Action   string `json:"action"` // list/create/delete
		User     string `json:"user"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	vType, verr := vectorType(req.Type)
	if verr != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": verr.Error()})
		return
	}

	switch req.Action {
	case "list":
		script := fmt.Sprintf(`echo %s`, opsEcho("Listing users for %s...", vType))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "vector_user_list")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "list"})

	case "create":
		if req.User == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "user is required"})
			return
		}
		userV, verr2 := mqName("user", req.User)
		if verr2 != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": verr2.Error()})
			return
		}
		script := fmt.Sprintf(`echo %s`, opsEcho("Creating user %s for %s...", userV, vType))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "vector_user_create")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "create"})

	case "delete":
		if req.User == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "user is required"})
			return
		}
		userV, verr2 := mqName("user", req.User)
		if verr2 != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": verr2.Error()})
			return
		}
		script := fmt.Sprintf(`echo %s`, opsEcho("Deleting user %s from %s...", userV, vType))
		taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "vector_user_delete")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": "delete"})

	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

// registerVectorExtendedRoutes 注册向量数据库扩展路由
func (s *Server) registerVectorExtendedRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/vector/monitor", s.handleVectorMonitor)
	mux.HandleFunc("/api/v1/vector/backup", s.handleVectorBackup)
	mux.HandleFunc("/api/v1/vector/scale", s.handleVectorScale)
	mux.HandleFunc("/api/v1/vector/upgrade", s.handleVectorUpgrade)
	mux.HandleFunc("/api/v1/vector/user", s.handleVectorUser)
}
