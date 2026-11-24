// data_etl_ops.go ETL 作业管理 API。
//
// 路由：
//   - POST /api/v1/etl/jobs            作业 CRUD + 运行
//   - POST /api/v1/etl/runs            运行记录查询
package controlplane

import (
	"net/http"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/dataetl"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// registerETLRoutes 注册 ETL 路由。
func (s *Server) registerETLRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/etl/jobs", s.handleETLJobs)
	mux.HandleFunc("/api/v1/etl/jobs/", s.handleETLJobRouting)
	mux.HandleFunc("/api/v1/etl/runs", s.handleETLRuns)
}

// etlStore 返回进程内 ETL 存储（惰性单例）。
func (s *Server) etlStore() *dataetl.MemoryStore {
	if s.etlMemStore == nil {
		s.etlMemStore = dataetl.NewMemoryStore()
	}
	return s.etlMemStore
}

func (s *Server) handleETLJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if r.Method == http.MethodGet {
		actx, ok := s.subsystemAuthorize(w, r, "task:read")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"jobs": s.etlStore().ListJobs(actx.TenantID),
		})
		return
	}
	var req struct {
		Action string         `json:"action"`
		Job    dataetl.EtlJob `json:"job"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action == "" {
		action = "list"
	}
	switch action {
	case "create":
		actx, ok := s.subsystemAuthorize(w, r, "task:write")
		if !ok {
			return
		}
		req.Job.TenantID = actx.TenantID
		if req.Job.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		if !dataetl.IsValidEngine(req.Job.Engine) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported engine: " + req.Job.Engine})
			return
		}
		created, err := s.etlStore().CreateJob(req.Job)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		paginate.WriteJSON(w, http.StatusCreated, created)
	case "update":
		actx, ok := s.subsystemAuthorize(w, r, "task:write")
		if !ok {
			return
		}
		if req.Job.ID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "job id required"})
			return
		}
		req.Job.TenantID = actx.TenantID
		updated, err := s.etlStore().UpdateJob(req.Job)
		if err != nil {
			if err == dataetl.ErrTenantMismatch {
				paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
				return
			}
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		paginate.WriteJSON(w, http.StatusOK, updated)
	case "delete":
		actx, ok := s.subsystemAuthorize(w, r, "task:write")
		if !ok {
			return
		}
		id := strings.TrimSpace(req.Job.ID)
		if id == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "job id required"})
			return
		}
		if err := s.etlStore().DeleteJob(actx.TenantID, id); err != nil {
			if err == dataetl.ErrTenantMismatch {
				paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
				return
			}
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	case "get":
		actx, ok := s.subsystemAuthorize(w, r, "task:read")
		if !ok {
			return
		}
		id := strings.TrimSpace(req.Job.ID)
		if id == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "job id required"})
			return
		}
		job, err := s.etlStore().GetJob(actx.TenantID, id)
		if err != nil {
			if err == dataetl.ErrTenantMismatch {
				paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
				return
			}
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		paginate.WriteJSON(w, http.StatusOK, job)
	case "list":
		actx, ok := s.subsystemAuthorize(w, r, "task:read")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"jobs": s.etlStore().ListJobs(actx.TenantID),
		})
	case "run":
		actx, ok := s.subsystemAuthorize(w, r, "task:write")
		if !ok {
			return
		}
		id := strings.TrimSpace(req.Job.ID)
		if id == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "job id required"})
			return
		}
		job, err := s.etlStore().GetJob(actx.TenantID, id)
		if err != nil {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
			return
		}
		adapter, err := dataetl.AdapterFor(job.Engine)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		cmd, err := adapter.BuildCommand(job)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		deviceID := strings.TrimSpace(req.Job.Params["deviceID"])
		if deviceID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID required in params"})
			return
		}
		dev := s.store.Device(deviceID)
		if dev != nil && dev.TenantID != "" && actx.TenantID != "" && dev.TenantID != actx.TenantID {
			paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
			return
		}
		run := dataetl.EtlRun{JobID: job.ID, TenantID: actx.TenantID, Status: dataetl.StatusPending}
		run, _ = s.etlStore().CreateRun(run)
		script := "set -e\n" + cmd + "\necho 'etl run " + run.ID + " done'\n"
		task, err := s.createSystemTask(actx.TenantID, deviceID, script, "etl_run_"+job.ID)
		if err != nil {
			writeInternalError(r.Context(), w, "etl_run", err)
			return
		}
		s.audit(r.Context(), &proto.AuditEvent{
			TenantID: actx.TenantID, UserID: actx.UserID, Action: "etl_run",
			Target: job.ID, Detail: sanitizeAuditDetail("runID=" + run.ID + " engine=" + job.Engine),
		})
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{
			"runID":  run.ID,
			"taskID": task.TaskID,
			"jobID":  job.ID,
			"engine": job.Engine,
		})
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

func (s *Server) handleETLJobRouting(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	s.handleETLJobs(w, r)
}

func (s *Server) handleETLRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if r.Method == http.MethodGet {
		actx, ok := s.subsystemAuthorize(w, r, "task:read")
		if !ok {
			return
		}
		jobID := r.URL.Query().Get("jobID")
		paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"runs": s.etlStore().ListRuns(actx.TenantID, jobID),
		})
		return
	}
	var req struct {
		Action string `json:"action"`
		RunID  string `json:"runID"`
		Status string `json:"status"`
		Logs   string `json:"logs"`
		Error  string `json:"error"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action == "" {
		action = "update"
	}
	switch action {
	case "update":
		actx, ok := s.subsystemAuthorize(w, r, "task:write")
		if !ok {
			return
		}
		run, err := s.etlStore().GetRun(actx.TenantID, req.RunID)
		if err != nil {
			if err == dataetl.ErrTenantMismatch {
				paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
				return
			}
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "run not found"})
			return
		}
		if req.Status != "" {
			run.Status = req.Status
		}
		if req.Logs != "" {
			run.Logs = req.Logs
		}
		if req.Error != "" {
			run.Error = req.Error
		}
		updated, err := s.etlStore().UpdateRun(run)
		if err != nil {
			writeInternalError(r.Context(), w, "etl_run_update", err)
			return
		}
		paginate.WriteJSON(w, http.StatusOK, updated)
	case "get":
		actx, ok := s.subsystemAuthorize(w, r, "task:read")
		if !ok {
			return
		}
		run, err := s.etlStore().GetRun(actx.TenantID, req.RunID)
		if err != nil {
			if err == dataetl.ErrTenantMismatch {
				paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
				return
			}
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "run not found"})
			return
		}
		paginate.WriteJSON(w, http.StatusOK, run)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}
