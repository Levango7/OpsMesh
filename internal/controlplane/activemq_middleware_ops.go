// activemq_middleware_ops.go ActiveMQ 中间件运维管理。
package controlplane

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// ============================================================================
// ActiveMQ Queue/Topic 管理
// ============================================================================

// activeMQReq ActiveMQ 操作请求体。
type activeMQReq struct {
	DeviceID string `json:"deviceID"`
	Action   string `json:"action"` // create/delete/list
	Name     string `json:"name"`   // 队列/话题名
	Type     string `json:"type"`   // queue/topic
}

// handleActiveMQQueue 处理 POST /api/v1/middleware/activemq/queue。
func (s *Server) handleActiveMQQueue(w http.ResponseWriter, r *http.Request) {
	s.handleActiveMQResource(w, r, "queue")
}

// handleActiveMQTopic 处理 POST /api/v1/middleware/activemq/topic。
func (s *Server) handleActiveMQTopic(w http.ResponseWriter, r *http.Request) {
	s.handleActiveMQResource(w, r, "topic")
}

func (s *Server) handleActiveMQResource(w http.ResponseWriter, r *http.Request, resType string) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "system:write"); !ok {
		return
	}
	var req activeMQReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	req.Type = resType
	allowed := map[string]bool{"create": true, "delete": true, "list": true}
	if !allowed[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (create/delete/list)"})
		return
	}
	script := buildActiveMQScript(req)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "activemq_"+resType+"_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "middleware.activemq."+resType, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "activemq_" + resType + "_" + req.Action, Target: req.Name,
		Detail: sanitizeAuditDetail("deviceID=" + req.DeviceID),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildActiveMQScript(req activeMQReq) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	switch req.Action {
	case "create":
		if req.Name == "" {
			b.WriteString("echo 'create 需指定名称' && exit 1\n")
		} else {
			fmt.Fprintf(&b, "activemq create %s --type %s\n", shellQuote(req.Name), req.Type)
		}
	case "delete":
		fmt.Fprintf(&b, "activemq delete %s --type %s\n", shellQuote(req.Name), req.Type)
	case "list":
		fmt.Fprintf(&b, "activemq list %s\n", req.Type)
	}
	fmt.Fprintf(&b, "echo 'ActiveMQ %s %s 操作完成\n", req.Type, req.Action)
	return b.String()
}
