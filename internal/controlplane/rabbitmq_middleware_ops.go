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
	allowed := map[string]bool{"create": true, "delete": true, "list": true}
	if !allowed[req.Action] {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported action (create/delete/list)"})
		return
	}
	script := buildRabbitMQScript(req, resource)
	task, err := s.createSystemTask(actx.TenantID, req.DeviceID, script, "rabbitmq_"+resource+"_"+req.Action)
	if err != nil {
		writeInternalError(r.Context(), w, "middleware.rabbitmq."+resource, err)
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "rabbitmq_" + resource + "_" + req.Action, Target: req.Name,
		Detail: sanitizeAuditDetail(fmt.Sprintf("deviceID=%s vhost=%s", req.DeviceID, req.Vhost)),
	})
	paginate.WriteJSON(w, http.StatusOK, task)
}

func buildRabbitMQScript(req rabbitMQReq, resource string) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	fmt.Fprintf(&b, "VHOST=%q\n", req.Vhost)
	switch resource {
	case "vhost":
		switch req.Action {
		case "create":
			fmt.Fprintf(&b, "rabbitmqctl add_vhost %s\n", shellQuote(req.Name))
		case "delete":
			fmt.Fprintf(&b, "rabbitmqctl delete_vhost %s\n", shellQuote(req.Name))
		case "list":
			b.WriteString("rabbitmqctl list_vhosts\n")
		}
	case "queue":
		switch req.Action {
		case "create":
			if req.Name == "" {
				b.WriteString("echo 'queue create 需指定 queue 名称' && exit 1\n")
			} else {
				fmt.Fprintf(&b, "rabbitmqctl declare_queue name=%s vhost=%s\n", shellQuote(req.Name), req.Vhost)
			}
		case "delete":
			fmt.Fprintf(&b, "rabbitmqctl delete_queue name=%s vhost=%s\n", shellQuote(req.Name), req.Vhost)
		case "list":
			fmt.Fprintf(&b, "rabbitmqctl list_queues -p %s\n", req.Vhost)
		}
	case "exchange":
		switch req.Action {
		case "create":
			if req.Name == "" {
				b.WriteString("echo 'exchange create 需指定 exchange 名称' && exit 1\n")
			} else {
				fmt.Fprintf(&b, "rabbitmqctl declare_exchange name=%s type=direct vhost=%s\n", shellQuote(req.Name), req.Vhost)
			}
		case "delete":
			fmt.Fprintf(&b, "rabbitmqctl delete_exchange name=%s vhost=%s\n", shellQuote(req.Name), req.Vhost)
		case "list":
			fmt.Fprintf(&b, "rabbitmqctl list_exchanges -p %s\n", req.Vhost)
		}
	}
	fmt.Fprintf(&b, "echo 'RabbitMQ %s %s 操作完成\n", resource, req.Action)
	return b.String()
}
