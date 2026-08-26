package controlplane

// webhook.go 实现 Phase 5 Webhook 管理 HTTP handler（CRUD + 测试投递 + 投递记录）。
//
// API 端点：
//   - GET    /api/v1/webhooks           列出 Webhook
//   - POST   /api/v1/webhooks           创建 Webhook
//   - GET    /api/v1/webhooks/{id}      Webhook 详情
//   - PUT    /api/v1/webhooks/{id}      更新 Webhook
//   - DELETE /api/v1/webhooks/{id}      删除 Webhook
//   - POST   /api/v1/webhooks/{id}/test  测试投递（模拟发送一条事件）
//   - GET    /api/v1/webhooks/{id}/deliveries  投递记录
//
// 设计要点（与 automation.go 风格一致）：
//   - 用 s.k8sTenantFromRequest(w, r) 提取租户；
//   - 错误响应统一 {"error": "message"} 格式；
//   - 用 decodeJSONBody 解析请求体；
//   - 鉴权：需 webhook:read/webhook:write 权限。
//   - SSRF 防护：create/update 对 URL 复用 ValidateWebhookURL 校验（协议白名单 +
//     私网/loopback/链路本地/云元数据拦截 + DNS rebinding 防护），allowPrivate 开关取
//     s.cfg.WebhookAllowPrivate，与通知渠道 CRUD（server_alerts_m2.go 的
//     createNotifyChannel/updateNotifyChannel → validateNotifyChannelWebhook）同一配置来源，
//     消除同库双标（FIXPLAN-phase1-6.md M1）。
//   - test 端点模拟发送：构造一条投递记录（StatusCode=200, Response="test"），
//     不实际发起 HTTP 请求（避免 SSRF 风险，仅验证 Webhook 配置可达性占位）。

import (
	"net/http"
	"strings"

	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// handleWebhooks 统一处理 /api/v1/webhooks：
//   - GET：列出 Webhook
//   - POST：创建 Webhook
func (s *Server) handleWebhooks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListWebhooks(w, r)
	case http.MethodPost:
		s.handleCreateWebhook(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListWebhooks 处理 GET /api/v1/webhooks：列出 Webhook。
func (s *Server) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "webhook:read"); !ok {
		return
	}
	tenant, ok := s.k8sTenantFromRequest(w, r)
	if !ok {
		return
	}
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	webhooks := s.store.ListWebhooks(tenant)
	writeJSON(w, http.StatusOK, map[string]interface{}{"webhooks": webhooks})
}

// handleCreateWebhook 处理 POST /api/v1/webhooks：创建 Webhook。
func (s *Server) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.requirePermission(w, r, "webhook:write")
	if !ok {
		return
	}
	tenant, ok := s.k8sTenantFromRequest(w, r)
	if !ok {
		return
	}
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	var body store.Webhook
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if body.URL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "url is required"})
		return
	}
	// SSRF 防护（M1 修复）：与 notify-channels 同源复用 ValidateWebhookURL——
	// 拒绝 file:// 等非 http(s) 协议及私网/loopback/链路本地/云元数据地址；
	// allowPrivate 取 s.cfg.WebhookAllowPrivate（与 createNotifyChannel 同一开关，
	// 内网部署场景显式放行私网）。校验失败返回 400 + 明确错误信息。
	if err := ValidateWebhookURL(body.URL, s.cfg.WebhookAllowPrivate); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid webhook url: " + err.Error()})
		return
	}
	created := s.store.CreateWebhook(tenant, &body)
	if created == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "create webhook failed"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: tenant, UserID: caller.ID, Action: "webhook_create", Target: created.ID, Detail: sanitizeAuditDetail("name=" + created.Name),
	})
	writeJSON(w, http.StatusCreated, created)
}

// handleWebhookRouting 分派 /api/v1/webhooks/{id} 子路径：
//   - GET    /api/v1/webhooks/{id}        Webhook 详情
//   - PUT    /api/v1/webhooks/{id}        更新 Webhook
//   - DELETE /api/v1/webhooks/{id}        删除 Webhook
//   - POST   /api/v1/webhooks/{id}/test   测试投递
//   - GET    /api/v1/webhooks/{id}/deliveries  投递记录
func (s *Server) handleWebhookRouting(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/webhooks/")
	if rest == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "webhook id required"})
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "webhook id required"})
		return
	}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			s.handleGetWebhook(w, r, id)
		case http.MethodPut:
			s.handleUpdateWebhook(w, r, id)
		case http.MethodDelete:
			s.handleDeleteWebhook(w, r, id)
		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		return
	}
	action := parts[1]
	switch action {
	case "test":
		s.handleWebhookTest(w, r, id)
	case "deliveries":
		s.handleWebhookDeliveries(w, r, id)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-path: " + action})
	}
}

// handleGetWebhook 处理 GET /api/v1/webhooks/{id}：Webhook 详情。
func (s *Server) handleGetWebhook(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requirePermission(w, r, "webhook:read"); !ok {
		return
	}
	tenant, ok := s.k8sTenantFromRequest(w, r)
	if !ok {
		return
	}
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	wh, ok := s.store.GetWebhook(tenant, id)
	if !ok || wh == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "webhook not found"})
		return
	}
	writeJSON(w, http.StatusOK, wh)
}

// handleUpdateWebhook 处理 PUT /api/v1/webhooks/{id}：更新 Webhook。
func (s *Server) handleUpdateWebhook(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "webhook:write")
	if !ok {
		return
	}
	tenant, ok := s.k8sTenantFromRequest(w, r)
	if !ok {
		return
	}
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	var body store.Webhook
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	// SSRF 防护（M1 修复）：更新路径与创建路径同一校验语义，防止经 PUT 绕过
	// 创建期防护注入 file:///内网地址。URL 为空时跳过（对齐 notify-channels 的
	// validateNotifyChannelWebhook：空值交由上游契约处理，非空必须过 SSRF 校验）；
	// allowPrivate 同样取 s.cfg.WebhookAllowPrivate，两端联动。
	if body.URL != "" {
		if err := ValidateWebhookURL(body.URL, s.cfg.WebhookAllowPrivate); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid webhook url: " + err.Error()})
			return
		}
	}
	body.ID = id
	updated, ok := s.store.UpdateWebhook(tenant, &body)
	if !ok || updated == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "webhook not found"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: tenant, UserID: caller.ID, Action: "webhook_update", Target: id, Detail: sanitizeAuditDetail("name=" + updated.Name),
	})
	writeJSON(w, http.StatusOK, updated)
}

// handleDeleteWebhook 处理 DELETE /api/v1/webhooks/{id}：删除 Webhook。
func (s *Server) handleDeleteWebhook(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "webhook:write")
	if !ok {
		return
	}
	tenant, ok := s.k8sTenantFromRequest(w, r)
	if !ok {
		return
	}
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	if !s.store.DeleteWebhook(tenant, id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "webhook not found"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: tenant, UserID: caller.ID, Action: "webhook_delete", Target: id, Detail: "",
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleWebhookTest 处理 POST /api/v1/webhooks/{id}/test：测试投递。
// 模拟发送一条事件（不实际发起 HTTP 请求，避免 SSRF 风险），
// 记录一条投递记录（StatusCode=200, Response="test ok"）。
func (s *Server) handleWebhookTest(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "webhook:write")
	if !ok {
		return
	}
	tenant, ok := s.k8sTenantFromRequest(w, r)
	if !ok {
		return
	}
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	wh, ok := s.store.GetWebhook(tenant, id)
	if !ok || wh == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "webhook not found"})
		return
	}
	// 模拟投递：记录一条投递记录（不实际发起 HTTP 请求）。
	event := "test.event"
	payload := `{"event":"test.event","message":"webhook test delivery"}`
	// M3：直接经 Store 接口调用，消除对 *MemoryStore 的类型断言；
	// SQLStore（桩）返回 nil 时降级为模拟响应（不落库），MultiSchemaStore 委托路由到 per-tenant store。
	delivery := s.store.RecordWebhookDelivery(tenant, id, event, payload, 200, "test ok", "")
	_ = caller
	if delivery == nil {
		// store 返回 nil（如 SQLStore 桩未持久化）：返回模拟结果不落库。
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"webhookID":  id,
			"event":      event,
			"statusCode": 200,
			"response":   "test ok (simulated, not persisted)",
		})
		return
	}
	writeJSON(w, http.StatusOK, delivery)
}

// handleWebhookDeliveries 处理 GET /api/v1/webhooks/{id}/deliveries：投递记录。
func (s *Server) handleWebhookDeliveries(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requirePermission(w, r, "webhook:read"); !ok {
		return
	}
	tenant, ok := s.k8sTenantFromRequest(w, r)
	if !ok {
		return
	}
	if tenant == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing tenant context (X-Tenant-ID required)"})
		return
	}
	deliveries := s.store.ListWebhookDeliveries(tenant, id)
	writeJSON(w, http.StatusOK, map[string]interface{}{"deliveries": deliveries})
}
