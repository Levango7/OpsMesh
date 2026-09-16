// notify_channels.go M2 通知渠道 API（NotifyChannel CRUD + 测试发送）。
package controlplane

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/internal/controlplane/paginate"

	"github.com/Levango7/OpsMesh/internal/notify"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/secrets"
	"github.com/Levango7/OpsMesh/internal/store"
)

// handleNotifyChannels 处理 /api/v1/notify-channels：GET 列表 / POST 创建。
func (s *Server) handleNotifyChannels(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listNotifyChannels(w, r)
	case http.MethodPost:
		s.createNotifyChannel(w, r)
	default:
		paginate.JSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// listNotifyChannels 返回当前租户的通知渠道列表（Config 脱敏）。
func (s *Server) listNotifyChannels(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "alert:read"); !ok {
		return
	}
	channels := s.store.ListNotifyChannels(actx.TenantID)
	// 脱敏：Config 中的敏感字段（webhook URL/secret/SMTP 密码）替换为 ***
	for _, c := range channels {
		c.Config = maskSensitiveConfig(c.Config)
	}
	paginate.WriteJSON(w, http.StatusOK, channels)
}

// createNotifyChannel 创建一条通知渠道。
func (s *Server) createNotifyChannel(w http.ResponseWriter, r *http.Request) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "alert:write"); !ok {
		return
	}
	var c store.NotifyChannel
	if err := decodeJSONBody(w, r, &c); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// SSRF 防护：保存前校验 webhook URL，防通知渠道被利用做 SSRF
	// （如配置 webhookURL=http://169.254.169.254/latest/meta-data/ 访问云元数据）。
	// 校验失败返回 400 + 错误信息（不泄露内部校验细节，仅返回安全错误）。
	if err := validateNotifyChannelWebhook(&c, s.cfg.WebhookAllowPrivate); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("webhook URL SSRF validation failed: %v", err)})
		return
	}
	c.TenantID = actx.TenantID
	created := s.store.CreateNotifyChannel(&c)
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "create_notify_channel", Target: created.ID,
		Detail: sanitizeAuditDetail(fmt.Sprintf("name=%s type=%s enabled=%v", created.Name, created.Type, created.Enabled)),
	})
	// 返回时脱敏
	created.Config = maskSensitiveConfig(created.Config)
	paginate.WriteJSON(w, http.StatusCreated, created)
}

// handleNotifyChannelRouting 分派 /api/v1/notify-channels/{id} 子路径：
//   - PUT    /api/v1/notify-channels/{id}      — 更新渠道
//   - DELETE /api/v1/notify-channels/{id}      — 删除渠道
//   - POST   /api/v1/notify-channels/{id}/test — 测试发送
func (s *Server) handleNotifyChannelRouting(w http.ResponseWriter, r *http.Request) {
	idAndRest := strings.TrimPrefix(r.URL.Path, "/api/v1/notify-channels/")
	parts := strings.SplitN(idAndRest, "/", 2)
	id := parts[0]
	if id == "" {
		paginate.JSONError(w, http.StatusBadRequest, "channel id required")
		return
	}
	switch {
	case len(parts) == 1 && r.Method == http.MethodPut:
		s.updateNotifyChannel(w, r, id)
	case len(parts) == 1 && r.Method == http.MethodDelete:
		s.deleteNotifyChannel(w, r, id)
	case len(parts) == 2 && parts[1] == "test" && r.Method == http.MethodPost:
		s.testNotifyChannel(w, r, id)
	default:
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found", "path": r.URL.Path})
	}
}

// updateNotifyChannel 更新一条通知渠道。
func (s *Server) updateNotifyChannel(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "alert:write"); !ok {
		return
	}
	var c store.NotifyChannel
	if err := decodeJSONBody(w, r, &c); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// SSRF 防护：更新前校验 webhook URL（与 createNotifyChannel 一致）。
	if err := validateNotifyChannelWebhook(&c, s.cfg.WebhookAllowPrivate); err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("webhook URL SSRF validation failed: %v", err)})
		return
	}
	c.ID = id
	c.TenantID = actx.TenantID
	if !s.store.UpdateNotifyChannel(&c) {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "channel not found"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "update_notify_channel", Target: id,
	})
	paginate.WriteJSON(w, http.StatusOK, c)
}

// deleteNotifyChannel 删除一条通知渠道。
func (s *Server) deleteNotifyChannel(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "alert:write"); !ok {
		return
	}
	if !s.store.DeleteNotifyChannel(id, actx.TenantID) {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "channel not found or tenant mismatch"})
		return
	}
	s.audit(r.Context(), &proto.AuditEvent{
		TenantID: actx.TenantID, UserID: actx.UserID, Action: "delete_notify_channel", Target: id,
	})
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted", "id": id})
}

// testNotifyChannel 测试发送一条通知到指定渠道。
// 请求体（可选）：{"title":"测试","body":"这是一条测试通知"}；缺省用内置测试消息。
func (s *Server) testNotifyChannel(w http.ResponseWriter, r *http.Request, id string) {
	actx, ok := s.requireTenantContext(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireProd(w, r, "alert:read"); !ok {
		return
	}
	c := s.store.GetNotifyChannel(id)
	if c == nil || c.TenantID != actx.TenantID {
		paginate.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "channel not found"})
		return
	}
	var body struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil && err != io.EOF {
		log.Printf("controlplane: testNotifyChannel 解析请求体失败: %v", err)
	}
	if body.Title == "" {
		body.Title = "OpsMesh 测试通知"
	}
	if body.Body == "" {
		body.Body = fmt.Sprintf("来自渠道 %s（类型 %s）的测试通知，发送时间 %s", c.Name, c.Type, time.Now().Format("2006-01-02 15:04:05"))
	}
	// 构造渠道实例并发送
	ch, err := buildChannel(c, s.secretProvider)
	if err != nil {
		paginate.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	msg := &notify.Message{
		Title:     body.Title,
		Body:      body.Body,
		Format:    "markdown",
		Severity:  "info",
		Source:    "test",
		Timestamp: time.Now(),
	}
	if err := ch.Send(msg); err != nil {
		// 发送失败属于服务端/上游渠道故障，不再用 200 携带错误体（HTTP 语义错误，
		// 会让前端与网关把失败当成功）。原始 err 含 SMTP 地址/内部错误细节，仅记日志。
		writeInternalError(r.Context(), w, "alerts.testNotifyChannel.send", err)
		return
	}
	paginate.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "test notification sent"})
}

// buildChannel 根据 NotifyChannel 构造 notify.Channel 实例。
// Config 为 JSON 字符串，按渠道 Type 解析对应配置。
// provider 非空时用 WithSecret 版本构造渠道，解析 ${key} 格式密钥引用；
// 为空时退化为明文构造（向后兼容）。
func buildChannel(c *store.NotifyChannel, provider secrets.SecretProvider) (notify.Channel, error) {
	var cfg map[string]string
	if c.Config != "" {
		if err := json.Unmarshal([]byte(c.Config), &cfg); err != nil {
			return nil, fmt.Errorf("parse channel config: %w", err)
		}
	}
	switch c.Type {
	case "dingtalk":
		return notify.NewDingTalkChannelWithSecret(cfg["webhookURL"], cfg["secret"], provider)
	case "wecom", "wechat":
		return notify.NewWeChatWorkChannelWithSecret(cfg["webhookURL"], provider)
	case "feishu", "lark":
		return notify.NewFeishuChannelWithSecret(cfg["webhookURL"], cfg["secret"], provider)
	case "slack":
		return notify.NewSlackChannelWithSecret(cfg["webhookURL"], cfg["channel"], provider)
	case "email":
		port := 25
		if p := cfg["port"]; p != "" {
			if n, scanErr := fmt.Sscanf(p, "%d", &port); scanErr != nil || n != 1 {
				port = 25
			}
		}
		var to []string
		if t := cfg["to"]; t != "" {
			to = strings.Split(t, ",")
		}
		// SMTP 密码属于敏感信息，支持密钥引用解析。
		resolvedPass, err := secrets.ResolveSecret(cfg["pass"], provider)
		if err != nil {
			return nil, fmt.Errorf("resolve email password: %w", err)
		}
		return notify.NewEmailChannel(cfg["host"], port, cfg["user"], resolvedPass, cfg["from"], to), nil
	case "webhook", "generic":
		// 通用 webhook 复用钉钉渠道（发送 markdown 消息，适用于大多数 webhook 接收器）
		return notify.NewDingTalkChannelWithSecret(cfg["webhookURL"], "", provider)
	default:
		return nil, fmt.Errorf("unsupported channel type: %s", c.Type)
	}
}

// validateNotifyChannelWebhook 校验通知渠道的 webhook URL 防 SSRF。
//
// 对 webhook 类型渠道（dingtalk/wechat/feishu/slack/webhook/generic），
// 从 Config JSON 提取 webhookURL 字段并调用 ValidateWebhookURL 校验：
//   - 协议必须 http/https
//   - 默认拒绝私网/loopback/链路本地/云元数据地址
//   - allowPrivate=true 时放行内网（内网部署场景）
//
// email 类型渠道无 URL，跳过校验。
// 校验失败返回 error（调用方应返回 400 + 错误信息）。
func validateNotifyChannelWebhook(c *store.NotifyChannel, allowPrivate bool) error {
	// 仅 webhook 类型渠道需要校验 URL。
	switch c.Type {
	case "dingtalk", "wecom", "wechat", "feishu", "lark", "slack", "webhook", "generic":
		// 提取 Config JSON 中的 webhookURL 字段。
		var cfg map[string]string
		if c.Config != "" {
			if err := json.Unmarshal([]byte(c.Config), &cfg); err != nil {
				return fmt.Errorf("parse channel config for SSRF validation: %w", err)
			}
		}
		webhookURL := cfg["webhookURL"]
		if webhookURL == "" {
			// webhookURL 为空由上游 store 层校验（CreateNotifyChannel/UpdateNotifyChannel），
			// 此处不重复校验空值，仅校验非空 URL 的 SSRF 安全性。
			return nil
		}
		return ValidateWebhookURL(webhookURL, allowPrivate)
	case "email":
		// email 渠道无 URL，跳过 SSRF 校验。
		return nil
	default:
		// 未知类型由 buildChannel 上游校验，此处放行（不阻塞未知类型校验流程）。
		return nil
	}
}
