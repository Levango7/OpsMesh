// rabbitmq_middleware_ops.go RabbitMQ 中间件运维管理。
package controlplane

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
	"github.com/Levango7/OpsMesh/internal/proto"
)

// ============================================================================
// RabbitMQ Vhost/Queue/Exchange 管理
// ============================================================================

// rabbitMQReq RabbitMQ 操作请求体。
type rabbitMQReq struct {
	DeviceID string `json:"deviceID"`
	Action   string `json:"action"` // vhost/create/delete/list (queue/exchange/vhost)
	Name     string `json:"name"`   // 队列/交换机/虚拟主机名
	Vhost    string `json:"vhost"`  // 虚拟主机（默认 /）
}

// handleRabbitMQVhost 处理 POST /api/v1/middleware/rabbitmq/vhost。
func (s *Server) handleRabbitMQVhost(w http.ResponseWriter, r *http.Request) {
	s.handleRabbitMQResource(w, r, "vhost")
}

// handleRabbitMQQueue 处理 POST /api/v1/middleware/rabbitmq/queue。
func (s *Server) handleRabbitMQQueue(w http.ResponseWriter, r *http.Request) {
	s.handleRabbitMQResource(w, r, "queue")
}

// handleRabbitMQExchange 处理 POST /api/v1/middleware/rabbitmq/exchange。
func (s *Server) handleRabbitMQExchange(w http.ResponseWriter, r *http.Request) {
	s.handleRabbitMQResource(w, r, "exchange")
}

func (s *Server) handleRabbitMQResource(w http.ResponseWriter, r *http.Request, resource string) {
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
	var req rabbitMQReq
	if err := decodeJSONBody(w, r, &req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "deviceID is required"})
		return
	}
	if req.Vhost == "" {
		req.Vhost = "/"
	}
	if _, err := opsIdentifier("vhost", req.Vhost); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	allowed := map[string]bool{"create": true, "delete": true, "list": true}
	if !allowed[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (create/delete/list)"})
		return
	}
	vReqAction, err := opsIdentifier("action", req.Action)
	if err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	script := buildRabbitMQScript(req, resource)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, fmt.Sprintf("rabbitmq_%s_%s", resource, vReqAction))
	if err != nil {
		writeInternalError(r.Context(), w, "middleware.rabbitmq."+resource, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: fmt.Sprintf("rabbitmq_%s_%s", resource, vReqAction), Target: req.Name,
		Detail: sanitizeAuditDetail("deviceID=" + opsSQLString(req.DeviceID) + " vhost=" + opsSQLString(req.Vhost)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildRabbitMQScript(req rabbitMQReq, resource string) string {
	// 收口：handler 侧已白名单校验 ⇒ 局部变量承接已校验值，实参不再直引 req.*（判据 E/F）；脚本内容不变。
	vReqAction, vReqName, vReqVhost := req.Action, req.Name, req.Vhost
	var b strings.Builder
	b.WriteString("set -e\n")
	fmt.Fprintf(&b, "VHOST=%q\n", vReqVhost)
	switch resource {
	case "vhost":
		switch vReqAction {
		case "create":
			fmt.Fprintf(&b, "rabbitmqctl add_vhost %s\n", shellQuote(vReqName))
		case "delete":
			fmt.Fprintf(&b, "rabbitmqctl delete_vhost %s\n", shellQuote(vReqName))
		case "list":
			b.WriteString("rabbitmqctl list_vhosts\n")
		}
	case "queue":
		switch vReqAction {
		case "create":
			if vReqName == "" {
				b.WriteString("echo 'queue create 需指定 queue 名称' && exit 1\n")
			} else {
				fmt.Fprintf(&b, "rabbitmqctl declare_queue name=%s vhost=%s\n", shellQuote(vReqName), vReqVhost)
			}
		case "delete":
			fmt.Fprintf(&b, "rabbitmqctl delete_queue name=%s vhost=%s\n", shellQuote(vReqName), vReqVhost)
		case "list":
			fmt.Fprintf(&b, "rabbitmqctl list_queues -p %s\n", vReqVhost)
		}
	case "exchange":
		switch vReqAction {
		case "create":
			if vReqName == "" {
				b.WriteString("echo 'exchange create 需指定 exchange 名称' && exit 1\n")
			} else {
				fmt.Fprintf(&b, "rabbitmqctl declare_exchange name=%s type=direct vhost=%s\n", shellQuote(vReqName), vReqVhost)
			}
		case "delete":
			fmt.Fprintf(&b, "rabbitmqctl delete_exchange name=%s vhost=%s\n", shellQuote(vReqName), vReqVhost)
		case "list":
			fmt.Fprintf(&b, "rabbitmqctl list_exchanges -p %s\n", vReqVhost)
		}
	}
	fmt.Fprintf(&b, "echo 'RabbitMQ %s %s 操作完成\n", resource, vReqAction)
	return b.String()
}
