// apache_ext_ops2.go Apache 数据工程组件运维支持（Superset/DolphinScheduler/SeaTunnel）。
package controlplane

import (
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// handleApacheSuperset 处理 POST /api/v1/middleware/apache/superset
func (s *Server) handleApacheSuperset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requireProd(w, r, "task:write"); !ok {
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/deploy/init-db/load-examples/health
		Version  string `json:"version"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Version == "" {
		req.Version = "4.0.2"
	}

	var script string
	switch req.Action {
	case "status":
		script = `echo "=== Superset Status ===" && (superset --version 2>/dev/null || docker ps | grep superset || echo "superset not available")`
	case "deploy":
		script = `echo "=== Deploy Superset ===" && (pip install apache-superset 2>/dev/null || docker run apache/superset) ; echo "superset deploy attempted"`
	case "init-db":
		script = `superset db upgrade && superset init`
	case "load-examples":
		script = `superset load_examples`
	case "health":
		script = `curl localhost:8088/health`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "apache_superset_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleApacheDolphinScheduler 处理 POST /api/v1/middleware/apache/dolphinscheduler
func (s *Server) handleApacheDolphinScheduler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requireProd(w, r, "task:write"); !ok {
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/deploy/start/stop/project/task/workflow
		Version  string `json:"version"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Version == "" {
		req.Version = "3.2.1"
	}

	var script string
	switch req.Action {
	case "status":
		script = `dolphinscheduler-daemon.sh status`
	case "deploy":
		script = `wget dolphinscheduler binary 2>/dev/null || docker run apache/dolphinscheduler`
	case "start":
		script = `dolphinscheduler-daemon.sh start`
	case "stop":
		script = `dolphinscheduler-daemon.sh stop`
	case "project":
		script = `dolphinscheduler project create`
	case "task":
		script = `dolphinscheduler task create`
	case "workflow":
		script = `dolphinscheduler workflow create`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "apache_dolphinscheduler_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// handleApacheSeaTunnel 处理 POST /api/v1/middleware/apache/seattunnel
func (s *Server) handleApacheSeaTunnel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requireProd(w, r, "task:write"); !ok {
		return
	}

	var req struct {
		DeviceID string `json:"deviceID"`
		Action   string `json:"action"` // status/deploy/job/run/list
		Version  string `json:"version"`
		Config   string `json:"config"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Version == "" {
		req.Version = "2.3.5"
	}
	if req.Config == "" {
		req.Config = "<config>"
	}

	var script string
	switch req.Action {
	case "status":
		script = `seatunnel --version`
	case "deploy":
		script = `wget seattunnel binary`
	case "job":
		script = fmt.Sprintf(`seatunnel.sh --config %s`, shellQuote(req.Config))
	case "run":
		script = fmt.Sprintf(`seatunnel.sh -c %s`, shellQuote(req.Config))
	case "list":
		script = `seatunnel.sh list`
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
		return
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "apache_seattunnel_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerApacheExt2Routes 注册 Apache 数据工程扩展路由
func (s *Server) registerApacheExt2Routes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/middleware/apache/superset", s.handleApacheSuperset)
	mux.HandleFunc("/api/v1/middleware/apache/dolphinscheduler", s.handleApacheDolphinScheduler)
	mux.HandleFunc("/api/v1/middleware/apache/seattunnel", s.handleApacheSeaTunnel)
}
