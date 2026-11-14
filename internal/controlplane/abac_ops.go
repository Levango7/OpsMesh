// Package controlplane abac_ops.go 实现 ABAC (基于属性的访问控制)
package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Levango7/OpsMesh/internal/authctx"
	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"
)

// buildABACScript 把 action + 用户字段组装成下发脚本（纯函数，供变异反证测试直接调）。
// 返回 (script, 0, "") 表示可下发；否则 ( "", status, errMsg) 表示拒绝。
func buildABACScript(action, subject, resource, actionAttr, context string) (string, int, string) {
	switch action {
	case "policy-list":
		return `echo "=== ABAC Policies ===" && echo "No ABAC policies configured (RBAC is the default)" && echo "---" && echo "To enable ABAC, configure policy rules in /etc/opsmesh/abac/policies.json"`, 0, ""
	case "policy-delete":
		return `echo "ABAC policy deletion (manual edit required)"`, 0, ""
	case "policy-create":
		if subject == "" || resource == "" || actionAttr == "" {
			return "", http.StatusBadRequest, "subject, resource, and actionAttr are required"
		}
		// 两层编码，顺序不可换：先 JSON（encoding/json 保证合法 JSON + 引号/换行转义），
		// 再 shell（shellquote.Quote 单引号字面量）。旧实现把用户字节直接拼进
		// echo '…' 的单引号 JSON 里——请求体带 ' 即闭合引号注入任意命令（A1 审计 BA-04，P0）。
		doc, err := json.Marshal(map[string]string{
			"subject":  subject,
			"resource": resource,
			"action":   actionAttr,
			"context":  context,
		})
		if err != nil {
			return "", http.StatusBadRequest, "subject/resource/context must be JSON-serializable: " + err.Error()
		}
		return fmt.Sprintf(`mkdir -p /etc/opsmesh/abac && echo %s >> /etc/opsmesh/abac/policies.json && echo "ABAC policy created"`, shellQuote(string(doc))), 0, ""
	case "evaluate":
		if subject == "" || resource == "" || actionAttr == "" {
			return "", http.StatusBadRequest, "subject, resource, and actionAttr are required"
		}
		// 旧实现把「Result: ALLOWED (default policy)」硬编码进 echo——不查任何策略引擎，
		// 任何输入都"允许"，是假判定（A1 审计 BA-04）。按 owner 已裁口径（未实现=显式 501，
		// 不造假数据，AI Agent 工具同先例）改为 501：强制层是 RBAC（requirePermission），
		// ABAC 评估属产品待立项。
		return "", http.StatusNotImplemented, "ABAC evaluation is not implemented; enforcement is RBAC-based (requirePermission). Policy files are advisory only."
	default:
		return "", http.StatusBadRequest, "unknown action: " + action
	}
}

// handleABAC 处理 POST /api/v1/abac
func (s *Server) handleABAC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		DeviceID   string `json:"deviceID"`
		Action     string `json:"action"`     // policy-list/policy-create/policy-delete/evaluate
		Subject    string `json:"subject"`    // subject attributes (JSON)
		Resource   string `json:"resource"`   // resource attributes (JSON)
		ActionAttr string `json:"actionAttr"` // action attribute
		Context    string `json:"context"`    // environment context (JSON)
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	// 审计整改批次17：evaluate / policy-list(无 deviceID) 走服务端真实引擎；
	// 其余动作（policy-create/policy-delete/带 deviceID 的 policy-list）保持设备下发。
	if req.Action == "evaluate" {
		if req.Subject == "" || req.Resource == "" || req.ActionAttr == "" {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "subject, resource, and actionAttr are required"})
			return
		}
		tenantID := authctx.FromContext(r.Context()).TenantID
		allowed, policyID, reason := s.evaluateABAC(tenantID, req.Subject, req.Resource, req.ActionAttr, req.Context)
		decision := "deny"
		if allowed {
			decision = "allow"
		}
		paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"decision":        decision,
			"allowed":         allowed,
			"matchedPolicyID": policyID,
			"reason":          reason,
		})
		return
	}
	if req.Action == "policy-list" && req.DeviceID == "" {
		paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"policies": s.listABACPolicies(authctx.FromContext(r.Context()).TenantID),
			"note":     "控制面策略镜像（设备侧文件仍由 policy-create 下发）",
		})
		return
	}
	if req.Action == "policy-delete" {
		s.removeABACPolicy(req.ActionAttr + "|" + req.Subject + "|" + req.Resource)
	}

	script, status, errMsg := buildABACScript(req.Action, req.Subject, req.Resource, req.ActionAttr, req.Context)
	if errMsg != "" {
		paginate.WriteJSON(w, status, map[string]string{"error": errMsg})
		return
	}

	// policy-create：同步登记控制面策略镜像（供 evaluate 使用）。
	if req.Action == "policy-create" {
		s.addABACPolicy(abacPolicy{
			ID:       req.ActionAttr + "|" + req.Subject + "|" + req.Resource,
			TenantID: authctx.FromContext(r.Context()).TenantID,
			Subject:  req.Subject,
			Resource: req.Resource,
			Action:   req.ActionAttr,
			Effect:   "allow",
		})
	}

	taskID, ok := s.dispatchKafkaTask(w, r, req.DeviceID, script, "abac_"+req.Action)
	if !ok {
		return
	}
	paginate.WriteJSON(w, http.StatusAccepted, map[string]interface{}{"taskID": taskID, "action": req.Action})
}

// registerABACRoutes 注册 ABAC 路由
func (s *Server) registerABACRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/abac", s.handleABAC)
}
