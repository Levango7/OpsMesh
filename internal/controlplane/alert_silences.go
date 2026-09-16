// alert_silences.go M2 静默规则 API（SilenceRule 标签匹配 + 时间窗口抑制）。
package controlplane

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"opsmesh/internal/controlplane/paginate"

	"opsmesh/internal/alertengine"
	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// handleAlertSilences 处理 /api/v1/alert-silences：GET 列表 / POST 创建。
func (s *Server) handleAlertSilences(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listAlertSilences(w, r)
	case http.MethodPost:
		s.createAlertSilence(w, r)
	default:
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// listAlertSilences 返回当前租户的静默规则列表。
func (s *Server) listAlertSilences(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "alert:read"); !ok {
		return
	}
	silences := s.store.ListSilences(actx.TenantID)
	paginate.WriteJSON(w, http.StatusOK, silences)
}

// createAlertSilence 创建一条静默规则。
func (s *Server) createAlertSilence(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "alert:write"); !ok {
		return
	}
	var sr store.SilenceRule
	if err := decodeJSONBody(w, r, &sr); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	sr.TenantID = actx.TenantID
	sr.CreatedBy = actx.UserID
	created := s.store.CreateSilence(&sr)
	// 同步注入 alertengine.Silencer（使评估循环立即应用新静默规则）
	if created != nil {
		_ = s.alertSilencer.AddRule(&alertengine.SilenceRule{
			ID:          created.ID,
			TenantID:    created.TenantID,
			MatchLabels: created.MatchLabels,
			StartAt:     created.StartAt,
			EndAt:       created.EndAt,
			CreatedBy:   created.CreatedBy,
			Reason:      created.Reason,
		})
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "create_alert_silence", Target: created.ID,
		Detail: sanitizeAuditDetail(fmt.Sprintf("reason=%s startAt=%s endAt=%s", created.Reason, created.StartAt.Format(time.RFC3339), created.EndAt.Format(time.RFC3339))),
	})
	paginate.WriteJSON(w, http.StatusCreated, created)
}

// handleAlertSilenceRouting 分派 /api/v1/alert-silences/{id} 子路径：DELETE 删除。
func (s *Server) handleAlertSilenceRouting(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/alert-silences/")
	if id == "" {
		paginate.JSONError(w, http.StatusBadRequest, "silence id required")
		return
	}
	switch r.Method {
	case http.MethodDelete:
		s.deleteAlertSilence(w, r, id)
	default:
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// deleteAlertSilence 删除一条静默规则。
func (s *Server) deleteAlertSilence(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "alert:write"); !ok {
		return
	}
	if !s.store.DeleteSilence(id, actx.TenantID) {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "silence not found or tenant mismatch"})
		return
	}
	// 同步从 alertengine.Silencer 移除
	_ = s.alertSilencer.DeleteRule(id)
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "delete_alert_silence", Target: id,
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted", "id": id})
}
