// server_schedules.go 实现 M5 定时任务管理 API：
//   - POST   /api/v1/schedules            创建定时任务
//   - GET    /api/v1/schedules            列表定时任务
//   - PUT    /api/v1/schedules/{id}       更新定时任务
//   - DELETE /api/v1/schedules/{id}       删除定时任务
//   - POST   /api/v1/schedules/{id}/pause 暂停
//   - POST   /api/v1/schedules/{id}/resume 恢复
//
// 依赖 internal/cron 包（Manager/ScheduleEntry）。
// Server 持有 *cron.Manager 实例（在 NewServer 中构造）。
package controlplane

import (
	"net/http"
	"strings"
	"time"

	"opsmesh/internal/cron"
	"opsmesh/internal/events"
	"opsmesh/internal/proto"
)

// ============================================================================
// 定时任务 CRUD
// ============================================================================

// handleSchedules 处理 /api/v1/schedules（GET 列表 / POST 创建）。
func (s *Server) handleSchedules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.scheduleList(w, r)
	case http.MethodPost:
		s.scheduleCreate(w, r)
	default:
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) scheduleList(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "schedule:read"); !ok {
		return
	}
	status := cron.EntryStatus(r.URL.Query().Get("status"))
	entries := s.scheduleMgr.List(actx.TenantID, status)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"schedules": entries,
		"total":     len(entries),
	})
}

func (s *Server) scheduleCreate(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "schedule:write"); !ok {
		return
	}
	var body struct {
		TaskID   string `json:"taskID"`
		Name     string `json:"name"`
		CronExpr string `json:"cronExpr"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.TaskID == "" {
		// 当 taskID 为空时，自动创建一个模板任务（带 Schedule）。
		// 模板任务 AgentID 留空，由具体派生实例时填充——但当前 Scheduler 派生
		// 实例时复制模板的 AgentID，故模板需有 AgentID。这里要求调用方提供 taskID。
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "taskID is required (pointing to a template task with Schedule set)"})
		return
	}
	entry := &cron.ScheduleEntry{
		TaskID:    body.TaskID,
		TenantID:  actx.TenantID,
		Name:      body.Name,
		CronExpr:  body.CronExpr,
		Status:    cron.EntryActive,
		CreatedBy: actx.UserID,
	}
	created, err := s.scheduleMgr.Create(entry)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "schedule_create", Target: created.ID,
		Detail: sanitizeAuditDetail("cron: " + body.CronExpr),
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: actx.TenantID, UserID: actx.UserID,
			Action: "schedule_create", Target: created.ID, Level: events.LevelInfo,
			Detail: sanitizeAuditDetail("cron: " + body.CronExpr),
		})
	}
	writeJSON(w, http.StatusCreated, created)
}

// handleScheduleRouting 处理 /api/v1/schedules/{id}[/pause|/resume]。
func (s *Server) handleScheduleRouting(w http.ResponseWriter, r *http.Request) {
	idAndRest := strings.TrimPrefix(r.URL.Path, "/api/v1/schedules/")
	if idAndRest == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "schedule id required"})
		return
	}
	parts := strings.SplitN(idAndRest, "/", 2)
	id := parts[0]
	if len(parts) == 1 {
		// /api/v1/schedules/{id}
		switch r.Method {
		case http.MethodGet:
			s.scheduleGet(w, r, id)
		case http.MethodPut:
			s.scheduleUpdate(w, r, id)
		case http.MethodDelete:
			s.scheduleDelete(w, r, id)
		default:
			jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	switch parts[1] {
	case "pause":
		if r.Method != http.MethodPost {
			jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.schedulePause(w, r, id)
	case "resume":
		if r.Method != http.MethodPost {
			jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.scheduleResume(w, r, id)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found", "path": r.URL.Path})
	}
}

func (s *Server) scheduleGet(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "schedule:read"); !ok {
		return
	}
	e, err := s.scheduleMgr.Get(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	if actx.TenantID != "" && e.TenantID != actx.TenantID {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "tenant mismatch"})
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (s *Server) scheduleUpdate(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "schedule:write"); !ok {
		return
	}
	var body struct {
		Name     string `json:"name"`
		CronExpr string `json:"cronExpr"`
		Status   string `json:"status"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	patch := &cron.ScheduleEntry{
		Name:     body.Name,
		CronExpr: body.CronExpr,
		Status:   cron.EntryStatus(body.Status),
	}
	updated, err := s.scheduleMgr.Update(id, patch)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "schedule_update", Target: id,
		Detail: "update schedule",
	})
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) scheduleDelete(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "schedule:write"); !ok {
		return
	}
	if err := s.scheduleMgr.Delete(id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "schedule_delete", Target: id,
		Detail: "delete schedule",
	})
	if s.bus != nil {
		s.bus.Publish(r.Context(), events.Event{
			TenantID: actx.TenantID, UserID: actx.UserID,
			Action: "schedule_delete", Target: id, Level: events.LevelInfo,
		})
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "id": id})
}

func (s *Server) schedulePause(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "schedule:write"); !ok {
		return
	}
	e, err := s.scheduleMgr.Pause(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "schedule_pause", Target: id,
		Detail: "pause schedule",
	})
	s.publishEvent(r.Context(), "schedule_status", actx.TenantID, map[string]string{
		"scheduleID": id, "status": "paused",
	})
	writeJSON(w, http.StatusOK, e)
}

func (s *Server) scheduleResume(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "schedule:write"); !ok {
		return
	}
	e, err := s.scheduleMgr.Resume(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "schedule_resume", Target: id,
		Detail: "resume schedule",
	})
	s.publishEvent(r.Context(), "schedule_status", actx.TenantID, map[string]string{
		"scheduleID": id, "status": "active",
	})
	writeJSON(w, http.StatusOK, e)
}

// suppress unused import warning.
var _ = time.Now
