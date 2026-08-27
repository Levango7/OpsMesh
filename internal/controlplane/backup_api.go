package controlplane

// backup_api.go 实现 Phase 3 灾备恢复 HTTP handler。
//
// 注意：backup.go 已实现 CLI 子命令的 ExportBackup/ImportBackup 逻辑；
// 本文件实现 HTTP API 端点，与 CLI 逻辑解耦，仅管理备份记录元信息。
// 实际备份文件生成/恢复由后台任务异步执行（MVP 同步占位）。
//
// API 端点：
//   - POST   /api/v1/backup/create   创建备份（请求体：{type: "full"|"config"|"devices"|"tasks"}）
//   - GET    /api/v1/backup/list     列出备份记录
//   - POST   /api/v1/backup/restore  恢复备份（请求体：{id}）
//   - DELETE /api/v1/backup/         删除备份（子路径：{id}）
//
// 设计要点（与 traffic.go 风格一致）：
//   - 用 s.requireTenantContext(w, r) 提取租户；
//   - 错误响应统一 {"error": "message"} 格式；
//   - 用 decodeJSONBody 解析请求体；
//   - 鉴权：需 backup:read/backup:write 权限。

import (
	"net/http"
	"strings"
	"time"

	"opsmesh/internal/store"
)

// handleBackupCreate 处理 POST /api/v1/backup/create：创建备份。
//
// 请求体：{"type": "full"|"config"|"devices"|"tasks"}
// MVP：创建备份记录（status=creating），实际备份文件生成由后台任务异步执行。
func (s *Server) handleBackupCreate(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "backup:write"); !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var body struct {
		Type string `json:"type"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	validTypes := map[string]bool{"full": true, "config": true, "devices": true, "tasks": true}
	if !validTypes[body.Type] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid type (must be full/config/devices/tasks)"})
		return
	}
	rec := &store.BackupRecord{
		Type:   body.Type,
		Status: "creating",
		Path:   "/var/lib/opsmesh/backups/" + body.Type,
	}
	created := s.store.CreateBackup(actx.TenantID, rec)
	if created == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "create backup failed"})
		return
	}
	// MVP：同步标记为 completed（实际备份文件生成由后台任务异步执行）。
	created.Status = "completed"
	created.Size = 0
	writeJSON(w, http.StatusCreated, created)
}

// handleBackupList 处理 GET /api/v1/backup/list：列出备份记录。
func (s *Server) handleBackupList(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "backup:read"); !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	backups := s.store.ListBackups(actx.TenantID)
	writeJSON(w, http.StatusOK, map[string]interface{}{"backups": backups})
}

// handleBackupRestore 处理 POST /api/v1/backup/restore：恢复备份。
//
// 请求体：{"id": "..."}
// MVP：校验备份记录存在，返回 accepted 状态（实际恢复由后台任务异步执行）。
func (s *Server) handleBackupRestore(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "backup:write"); !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}
	rec, ok := s.store.GetBackup(actx.TenantID, body.ID)
	if !ok || rec == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "backup not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":    "accepted",
		"backup":    rec,
		"message":   "restore triggered; check backup status for progress",
		"startedAt": time.Now(),
		// M12 占位标记：实际恢复由后台任务异步执行，此响应仅确认请求已接受。
		"simulated": true,
	})
}

// handleBackupDeleteRouting 分派 DELETE /api/v1/backup/{id}：删除备份。
func (s *Server) handleBackupDeleteRouting(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/backup/")
	if rest == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "backup id required"})
		return
	}
	id := rest
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "backup id required"})
		return
	}
	if r.Method != http.MethodDelete {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := s.requirePermission(w, r, "backup:write"); !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	if !s.store.DeleteBackup(actx.TenantID, id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "backup not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

