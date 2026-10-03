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
//   - 用 s.requireTenantContext(w, r) 提取租户；
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
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"

	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store"
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
		paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleListWebhooks 处理 GET /api/v1/webhooks：列出 Webhook。
func (s *Server) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "webhook:read"); !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	webhooks := s.store.ListWebhooks(actx.TenantID)
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"webhooks": webhooks})
}

// handleCreateWebhook 处理 POST /api/v1/webhooks：创建 Webhook。
func (s *Server) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.requirePermission(w, r, "webhook:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	var body store.Webhook
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Name == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if body.URL == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "url is required"})
		return
	}
	// SSRF 防护（M1 修复）：与 notify-channels 同源复用 ValidateWebhookURL——
	// 拒绝 file:// 等非 http(s) 协议及私网/loopback/链路本地/云元数据地址；
	// allowPrivate 取 s.cfg.WebhookAllowPrivate（与 createNotifyChannel 同一开关，
	// 内网部署场景显式放行私网）。校验失败返回 400 + 明确错误信息。
	if err := ValidateWebhookURL(body.URL, s.cfg.WebhookAllowPrivate); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid webhook url: " + err.Error()})
		return
	}
	created := s.store.CreateWebhook(actx.TenantID, &body)
	if created == nil {
		paginate.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "create webhook failed"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "webhook_create", Target: created.ID, Detail: sanitizeAuditDetail("name=" + created.Name),
	})
	paginate.WriteJSON(w, http.StatusCreated, created)
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
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "webhook id required"})
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	if id == "" {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "webhook id required"})
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
			paginate.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
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
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-path: " + action})
	}
}

// handleGetWebhook 处理 GET /api/v1/webhooks/{id}：Webhook 详情。
func (s *Server) handleGetWebhook(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requirePermission(w, r, "webhook:read"); !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	wh, ok := s.store.GetWebhook(actx.TenantID, id)
	if !ok || wh == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "webhook not found"})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, wh)
}

// handleUpdateWebhook 处理 PUT /api/v1/webhooks/{id}：更新 Webhook。
func (s *Server) handleUpdateWebhook(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "webhook:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	var body store.Webhook
	if err := decodeJSONBody(w, r, &body); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	// SSRF 防护（M1 修复）：更新路径与创建路径同一校验语义，防止经 PUT 绕过
	// 创建期防护注入 file:///内网地址。URL 为空时跳过（对齐 notify-channels 的
	// validateNotifyChannelWebhook：空值交由上游契约处理，非空必须过 SSRF 校验）；
	// allowPrivate 同样取 s.cfg.WebhookAllowPrivate，两端联动。
	if body.URL != "" {
		if err := ValidateWebhookURL(body.URL, s.cfg.WebhookAllowPrivate); err != nil {
			paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid webhook url: " + err.Error()})
			return
		}
	}
	body.ID = id
	updated, ok := s.store.UpdateWebhook(actx.TenantID, &body)
	if !ok || updated == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "webhook not found"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "webhook_update", Target: id, Detail: sanitizeAuditDetail("name=" + updated.Name),
	})
	paginate.WriteJSON(w, http.StatusOK, updated)
}

// handleDeleteWebhook 处理 DELETE /api/v1/webhooks/{id}：删除 Webhook。
func (s *Server) handleDeleteWebhook(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "webhook:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	if !s.store.DeleteWebhook(actx.TenantID, id) {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "webhook not found"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: caller.ID, Action: "webhook_delete", Target: id, Detail: "",
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleWebhookTest 处理 POST /api/v1/webhooks/{id}/test：测试投递。
// 真实投递：HTTP POST 到 webhook URL，记录真实响应（SSRF 校验）。
func (s *Server) handleWebhookTest(w http.ResponseWriter, r *http.Request, id string) {
	caller, ok := s.requirePermission(w, r, "webhook:write")
	if !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	wh, ok := s.store.GetWebhook(actx.TenantID, id)
	if !ok || wh == nil {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "webhook not found"})
		return
	}
	event := "test.event"
	payload := `{"event":"test.event","message":"webhook test delivery"}`

	// SSRF 校验：默认拒绝私网/元数据地址；--webhook-allow-private 开启时放行内网收件端
	// （但链路本地/云元数据/0.0.0.0/8 恒拒，见 ValidateWebhookURL）。
	allowPrivate := s.cfg != nil && s.cfg.WebhookAllowPrivate
	if err := ValidateWebhookURL(wh.URL, allowPrivate); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("webhook URL rejected: %v", err)})
		return
	}

	// 真实投递：HTTP POST。
	statusCode, respBody, err := deliverWebhook(wh.URL, event, payload, allowPrivate)
	if err != nil {
		// 投递失败：记录失败状态。
		delivery := s.store.RecordWebhookDelivery(actx.TenantID, id, event, payload, 0, err.Error(), err.Error())
		_ = caller
		_ = delivery
		paginate.WriteJSON(w, http.StatusBadGateway, map[string]interface{}{
			"webhookID": id,
			"event":     event,
			"error":     fmt.Sprintf("delivery failed: %v", err),
		})
		return
	}

	// 投递成功：记录真实响应。
	delivery := s.store.RecordWebhookDelivery(actx.TenantID, id, event, payload, statusCode, respBody, "")
	_ = caller
	if delivery == nil {
		paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"webhookID":  id,
			"event":      event,
			"statusCode": statusCode,
			"response":   respBody,
		})
		return
	}
	paginate.WriteJSON(w, http.StatusOK, delivery)
}

// deliverWebhook 执行真实的 HTTP POST 投递。
//
// allowPrivate 与 ValidateWebhookURL 同义（--webhook-allow-private）：内网收件端场景
// 需为 true，但链路本地/云元数据/0.0.0.0/8 恒拒。
//
// 用 newEgressClient 而非裸 http.Client 的原因：webhook URL 在**保存时**校验过，
// 但 DNS 记录此后可被改动（rebinding），且裸 client 默认跟随最多 10 次重定向——
// 两者叠加即可让"校验通过的外网 URL"在投递时打到 169.254.169.254。
// newEgressClient 在建连时逐 IP 复检，并对每一跳重定向重新做 URL 级校验。
func deliverWebhook(rawURL, event, payload string, allowPrivate bool) (int, string, error) {
	// 投递期二次校验：配置是持久化的，保存时的结论不能代表此刻仍然安全。
	if err := ValidateWebhookURL(rawURL, allowPrivate); err != nil {
		return 0, "", fmt.Errorf("webhook URL rejected at delivery time: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, rawURL, strings.NewReader(payload))
	if err != nil {
		return 0, "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "OpsMesh-Webhook/1.0")
	req.Header.Set("X-OpsMesh-Event", event)

	client := newEgressClient(10*time.Second, allowPrivate)
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("HTTP POST: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, "", fmt.Errorf("read response: %w", err)
	}
	return resp.StatusCode, string(body), nil
}

// handleWebhookDeliveries 处理 GET /api/v1/webhooks/{id}/deliveries：投递记录。
func (s *Server) handleWebhookDeliveries(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := s.requirePermission(w, r, "webhook:read"); !ok {
		return
	}
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if actx.TenantID == "" {
		paginate.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing actx.TenantID context (X-Tenant-ID required)"})
		return
	}
	deliveries := s.store.ListWebhookDeliveries(actx.TenantID, id)
	paginate.WriteJSON(w, http.StatusOK, map[string]interface{}{"deliveries": deliveries})
}
