// lakehouse_ops.go 湖仓表维护 + 批/流统一作业 API。
//
// 路由：
//   - POST /api/v1/lakehouse/tables     表 CRUD
//   - POST /api/v1/lakehouse/maintenance 维护作业 CRUD + 执行
//   - POST /api/v1/lakehouse/jobs       统一作业 CRUD + 运行
//   - GET  /api/v1/lakehouse/jobs      作业列表
//   - GET  /api/v1/lakehouse/runs      运行记录
package controlplane

import (
	"net/http"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/lakehouse"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// registerLakehouseRoutes 注册湖仓路由。
func (s *Server) registerLakehouseRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/lakehouse/tables", s.handleLakehouseTables)
	mux.HandleFunc("/api/v1/lakehouse/maintenance", s.handleLakehouseMaintenance)
	mux.HandleFunc("/api/v1/lakehouse/jobs", s.handleLakehouseJobs)
	mux.HandleFunc("/api/v1/lakehouse/jobs/", s.handleLakehouseJobRouting)
	mux.HandleFunc("/api/v1/lakehouse/runs", s.handleLakehouseRuns)
}

// lakehouseStore 返回进程内湖仓存储（惰性单例）。
func (s *Server) lakehouseStore() *lakehouse.MemoryStore {
	if s.lakehouseMemStore == nil {
		s.lakehouseMemStore = lakehouse.NewMemoryStore()
	}
	return s.lakehouseMemStore
}

func (s *Server) handleLakehouseTables(w http.ResponseWriter, r *http.Request) {
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
			"tables": s.lakehouseStore().ListTables(actx.TenantID),
		})
		return
	}
	var req struct {
		Action string                `json:"action"`
		Table  lakehouse.TableConfig `json:"table"`
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
		req.Table.TenantID = actx.TenantID
		if req.Table.Name == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		if !lakehouse.IsValidFormat(req.Table.Format) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported format: " + req.Table.Format})
			return
		}
		created, err := s.lakehouseStore().CreateTable(req.Table)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		paginate.WriteJSON(w, http.StatusCreated, created)
	case "get":
		actx, ok := s.subsystemAuthorize(w, r, "task:read")
		if !ok {
			return
		}
		id := strings.TrimSpace(req.Table.ID)
		if id == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"})
			return
		}
		t, err := s.lakehouseStore().GetTable(actx.TenantID, id)
		if err != nil {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		paginate.WriteJSON(w, http.StatusOK, t)
	case "update":
		actx, ok := s.subsystemAuthorize(w, r, "task:write")
		if !ok {
			return
		}
		if req.Table.ID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"})
			return
		}
		req.Table.TenantID = actx.TenantID
		updated, err := s.lakehouseStore().UpdateTable(req.Table)
		if err != nil {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		paginate.WriteJSON(w, http.StatusOK, updated)
	case "delete":
		actx, ok := s.subsystemAuthorize(w, r, "task:write")
		if !ok {
			return
		}
		id := strings.TrimSpace(req.Table.ID)
		if id == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"})
			return
		}
		if err := s.lakehouseStore().DeleteTable(actx.TenantID, id); err != nil {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	case "list":
		actx, ok := s.subsystemAuthorize(w, r, "task:read")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"tables": s.lakehouseStore().ListTables(actx.TenantID),
		})
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

func (s *Server) handleLakehouseMaintenance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		Action   string                   `json:"action"`
		Job      lakehouse.MaintenanceJob `json:"job"`
		DryRun   bool                     `json:"dryRun"`
		DeviceID string                   `json:"deviceID"`
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
		if req.Job.TableName == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "tableName is required"})
			return
		}
		if !lakehouse.IsValidFormat(req.Job.Format) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported format: " + req.Job.Format})
			return
		}
		if !lakehouse.IsValidAction(req.Job.Action) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action: " + req.Job.Action})
			return
		}
		created, err := s.lakehouseStore().CreateMaintenanceJob(req.Job)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		// build maintenance script
		script := buildLakehouseMaintenanceScript(created)
		if req.DryRun {
			paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
				"dryRun": true,
				"job":    created,
				"script": script,
			})
			return
		}
		deviceID := strings.TrimSpace(req.DeviceID)
		if deviceID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID required"})
			return
		}
		dev := s.store.Device(deviceID)
		if dev != nil && dev.TenantID != "" && actx.TenantID != "" && dev.TenantID != actx.TenantID {
			paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
			return
		}
		task, err := s.createSystemTask(actx.TenantID, deviceID, script, "lakehouse_maint_"+created.Action)
		if err != nil {
			writeInternalError(r.Context(), w, "lakehouse_maint", err)
			return
		}
		created.Status = "running"
		if _, err := s.lakehouseStore().UpdateMaintenanceJob(created); err != nil {
			writeInternalError(r.Context(), w, "lakehouse_maintenance", err)
			return
		}
		s.audit(r.Context(), &proto.AuditEvent{
			TenantID: actx.TenantID, UserID: actx.UserID, Action: "lakehouse_maintenance",
			Target: created.TableName, Detail: sanitizeAuditDetail("action=" + created.Action + " format=" + created.Format),
		})
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{
			"jobID":    created.ID,
			"taskID":   task.TaskID,
			"deviceID": deviceID,
			"script":   script,
		})
	case "list":
		actx, ok := s.subsystemAuthorize(w, r, "task:read")
		if !ok {
			return
		}
		tableID := strings.TrimSpace(req.Job.TableID)
		paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"jobs": s.lakehouseStore().ListMaintenanceJobs(actx.TenantID, tableID),
		})
	case "get":
		actx, ok := s.subsystemAuthorize(w, r, "task:read")
		if !ok {
			return
		}
		id := strings.TrimSpace(req.Job.ID)
		if id == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"})
			return
		}
		j, err := s.lakehouseStore().GetMaintenanceJob(actx.TenantID, id)
		if err != nil {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		paginate.WriteJSON(w, http.StatusOK, j)
	case "delete":
		actx, ok := s.subsystemAuthorize(w, r, "task:write")
		if !ok {
			return
		}
		id := strings.TrimSpace(req.Job.ID)
		if id == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"})
			return
		}
		j, err := s.lakehouseStore().GetMaintenanceJob(actx.TenantID, id)
		if err != nil {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		_ = j
		paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

func buildLakehouseMaintenanceScript(j lakehouse.MaintenanceJob) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("# lakehouse maintenance: " + j.Action + " on " + j.TableName + " (" + j.Format + ")\n")
	switch j.Format {
	case "iceberg":
		switch j.Action {
		case "compact":
			b.WriteString("spark-submit --class org.apache.iceberg.spark.actions.SparkActions /opt/iceberg-spark-runtime.jar --table " + j.TableName + " --action compact\n")
		case "expire-snapshots":
			b.WriteString("spark-submit --class org.apache.iceberg.spark.actions.SparkActions /opt/iceberg-spark-runtime.jar --table " + j.TableName + " --action expire_snapshots --older-than " + j.Params + "\n")
		case "remove-orphans":
			b.WriteString("spark-submit --class org.apache.iceberg.spark.actions.SparkActions /opt/iceberg-spark-runtime.jar --table " + j.TableName + " --action remove_orphan_files\n")
		case "rewrite-manifests":
			b.WriteString("spark-submit --class org.apache.iceberg.spark.actions.SparkActions /opt/iceberg-spark-runtime.jar --table " + j.TableName + " --action rewrite_manifests\n")
		}
	case "delta":
		switch j.Action {
		case "compact":
			b.WriteString("spark-submit --class io.delta.tables.DeltaTable /opt/delta-spark.jar --table " + j.TableName + " --action compact\n")
		case "vacuum":
			b.WriteString("spark-submit --class io.delta.tables.DeltaTable /opt/delta-spark.jar --table " + j.TableName + " --action vacuum --retention " + j.Params + "\n")
		}
	case "hudi":
		switch j.Action {
		case "compact":
			b.WriteString("spark-submit --class org.apache.hudi.utilities.HoodieCompactor /opt/hudi-utilities.jar --table " + j.TableName + " --action compact\n")
		case "clean":
			b.WriteString("spark-submit --class org.apache.hudi.utilities.HoodieCleaner /opt/hudi-utilities.jar --table " + j.TableName + " --action clean\n")
		}
	}
	b.WriteString("echo 'lakehouse maintenance " + j.Action + " on " + j.TableName + " done'\n")
	return b.String()
}

func (s *Server) handleLakehouseJobs(w http.ResponseWriter, r *http.Request) {
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
			"jobs": s.lakehouseStore().ListUnifiedJobs(actx.TenantID),
		})
		return
	}
	var req struct {
		Action   string               `json:"action"`
		Job      lakehouse.UnifiedJob `json:"job"`
		DeviceID string               `json:"deviceID"`
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
		if !lakehouse.IsValidJobKind(req.Job.Kind) {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported kind: " + req.Job.Kind})
			return
		}
		created, err := s.lakehouseStore().CreateUnifiedJob(req.Job)
		if err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		paginate.WriteJSON(w, http.StatusCreated, created)
	case "get":
		actx, ok := s.subsystemAuthorize(w, r, "task:read")
		if !ok {
			return
		}
		id := strings.TrimSpace(req.Job.ID)
		if id == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"})
			return
		}
		j, err := s.lakehouseStore().GetUnifiedJob(actx.TenantID, id)
		if err != nil {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		paginate.WriteJSON(w, http.StatusOK, j)
	case "update":
		actx, ok := s.subsystemAuthorize(w, r, "task:write")
		if !ok {
			return
		}
		if req.Job.ID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"})
			return
		}
		req.Job.TenantID = actx.TenantID
		updated, err := s.lakehouseStore().UpdateUnifiedJob(req.Job)
		if err != nil {
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
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"})
			return
		}
		if err := s.lakehouseStore().DeleteUnifiedJob(actx.TenantID, id); err != nil {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	case "list":
		actx, ok := s.subsystemAuthorize(w, r, "task:read")
		if !ok {
			return
		}
		paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"jobs": s.lakehouseStore().ListUnifiedJobs(actx.TenantID),
		})
	case "run":
		actx, ok := s.subsystemAuthorize(w, r, "task:write")
		if !ok {
			return
		}
		id := strings.TrimSpace(req.Job.ID)
		if id == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"})
			return
		}
		job, err := s.lakehouseStore().GetUnifiedJob(actx.TenantID, id)
		if err != nil {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
			return
		}
		deviceID := strings.TrimSpace(req.DeviceID)
		if deviceID == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID required"})
			return
		}
		dev := s.store.Device(deviceID)
		if dev != nil && dev.TenantID != "" && actx.TenantID != "" && dev.TenantID != actx.TenantID {
			paginate.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
			return
		}
		run := lakehouse.JobRun{JobID: job.ID, TenantID: actx.TenantID, Status: lakehouse.JobStatusPending}
		run, err = s.lakehouseStore().CreateJobRun(run)
		if err != nil {
			writeInternalError(r.Context(), w, "lakehouse_job", err)
			return
		}
		script := "set -e\necho 'lakehouse job " + job.Name + " (" + job.Kind + ") run " + run.ID + " done'\n"
		task, err := s.createSystemTask(actx.TenantID, deviceID, script, "lakehouse_job_"+job.ID)
		if err != nil {
			writeInternalError(r.Context(), w, "lakehouse_job", err)
			return
		}
		s.audit(r.Context(), &proto.AuditEvent{
			TenantID: actx.TenantID, UserID: actx.UserID, Action: "lakehouse_job_run",
			Target: job.ID, Detail: sanitizeAuditDetail("runID=" + run.ID + " kind=" + job.Kind),
		})
		paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{
			"runID":  run.ID,
			"taskID": task.TaskID,
			"jobID":  job.ID,
			"kind":   job.Kind,
		})
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}

func (s *Server) handleLakehouseJobRouting(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	s.handleLakehouseJobs(w, r)
}

func (s *Server) handleLakehouseRuns(w http.ResponseWriter, r *http.Request) {
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
			"runs": s.lakehouseStore().ListJobRuns(actx.TenantID, jobID),
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
		run, err := s.lakehouseStore().GetJobRun(actx.TenantID, req.RunID)
		if err != nil {
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
		updated, err := s.lakehouseStore().UpdateJobRun(run)
		if err != nil {
			writeInternalError(r.Context(), w, "lakehouse_run_update", err)
			return
		}
		paginate.WriteJSON(w, http.StatusOK, updated)
	case "get":
		actx, ok := s.subsystemAuthorize(w, r, "task:read")
		if !ok {
			return
		}
		run, err := s.lakehouseStore().GetJobRun(actx.TenantID, req.RunID)
		if err != nil {
			paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "run not found"})
			return
		}
		paginate.WriteJSON(w, http.StatusOK, run)
	default:
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action: " + req.Action})
	}
}
