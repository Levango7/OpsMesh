// alert_channels.go — TD-92 收口：告警外发渠道的单一口径与装配。
//
// 背景（docs/tech-debt.md TD-92）：`notify.Notifier` 的两处生产构造（server.go:361 与
// :424 的 SecretProvider 重建）都只传 WithDedup/WithRetry/WithSecretProvider，
// **从不传 WithChannels** ⇒ Notifier.channels 恒空 ⇒ `sendToAll` 静默 return nil。
// 两个真实消费方全部中招：M2 聚合组通知（notifyAlertGroup）与自动化"发送通知"动作
// （automationExecutor.SendNotify）——调用方都收到"成功"，实际一条都没发出去。
//
// 裁定（单一口径，2026-10-12）：**自动告警外发的渠道只来自启动期配置**——
//
//	① CLI 单通道旗帜（--alert-webhook-url/--alert-notifier-type/--alert-email-*）
//	   ⇒ notify.Channels（M7 notifyLoop 发送，alert 形状，既有真机实测口径不变）；
//	② --notify-channels-config 多渠道文件（cfg.NotifyChannels，config.Validate 已校验）
//	   ⇒ notify.Notifier 的 channels（message 形状），经本文件装配。
//
// 两条都是"启动参数"这一侧的形态，合并为一个启动期渠道集；M7 notifyLoop 是唯一
// 自动告警发送点。store 的 notify-channels 表（前端「通知渠道」页）**保持只服务
// 测试按钮**（已知限制，另立项；前端文案本就如实）。
package controlplane

import (
	"context"
	"fmt"
	"time"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/internal/notify"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/secrets"
)

// buildNotifierChannels 把 --notify-channels-config 加载的多渠道配置构造成
// notify.Channel 实例。此前这份配置只被 redactNotifyChannels 用于诊断输出——
// 配置了却没有任何发送路径消费它。
//
// 单条失败（构造错误 / URL 未过 SSRF 准入）只跳过该条并打 Error（点名序号与类型），
// 不影响其余渠道，也不阻断启动：配置里一条写错不该让整个控制面起不来。
// webhook 系 URL 过与 M7 单通道同款的 SSRF 准入（ValidateWebhookURL），
// 私网收件端同样受 --webhook-allow-private 控制。
func buildNotifierChannels(cfg *config.Config) []notify.Channel {
	if cfg == nil {
		return nil
	}
	var chs []notify.Channel
	for i, c := range cfg.NotifyChannels {
		if c.Type != "email" && c.WebhookURL != "" {
			if err := ValidateWebhookURL(c.WebhookURL, cfg.WebhookAllowPrivate); err != nil {
				logx.Error(context.Background(), "通知渠道 URL 未通过 SSRF 准入，已跳过该渠道", err,
					"index", i, "type", c.Type,
					"webhookAllowPrivate", cfg.WebhookAllowPrivate,
					"hint", "内网收件端（钉钉/飞书网关、集群内自建服务）需显式设 --webhook-allow-private=true 或 OPSMESH_WEBHOOK_ALLOW_PRIVATE=true")
				continue
			}
		}
		ch, err := notify.NewChannel(notify.ChannelConfig{
			Type:       c.Type,
			WebhookURL: c.WebhookURL,
			Secret:     c.Secret,
			Channel:    c.Channel,
			SMTPHost:   c.SMTPHost,
			SMTPPort:   c.SMTPPort,
			Username:   c.Username,
			Password:   c.Password,
			From:       c.From,
			To:         c.To,
		})
		if err != nil || ch == nil {
			logx.Error(context.Background(), "通知渠道构造失败，已跳过该渠道", err, "index", i, "type", c.Type)
			continue
		}
		chs = append(chs, ch)
	}
	return chs
}

// newAlertNotifier 装配告警 Notifier：去重 + 重试 + 文件渠道 +（可选）密钥提供者。
//
// TD-92 起这是**唯一**的装配入口（server.go 两处构造与 SecretProvider 重建都走它），
// 渠道在此一次性注入、构造后只读（内部并发安全见 notify.Notifier 文档）。
// 已知未接线（记账不暗修）：cfg.NotifyDedupTTLMin / NotifyRetry* 三个旋钮仍按
// 既有硬编码值（5min / DefaultRetryPolicy）——WithDedup(0) 的语义是"用默认 5 分钟"
// 而非配置注释所说的"关闭"，径直接上会把 0 从"关闭"变成"默认"，属语义翻转，另立项。
func newAlertNotifier(cfg *config.Config, provider secrets.SecretProvider) *notify.Notifier {
	return notify.NewNotifier(
		notify.WithDedup(5*time.Minute),
		notify.WithRetry(nil),
		notify.WithChannels(buildNotifierChannels(cfg)...),
		notify.WithSecretProvider(provider),
	)
}

// alertToNotifyMessage 把 firing 告警转成 notify.Message（文件多渠道的载荷形状：
// message 形状，与 CLI 单通道的 alert 形状刻意不同——两条配置路由各自保持既定的
// 真机实测口径，不为"统一"引入第二套字段映射）。
func alertToNotifyMessage(a *proto.Alert) *notify.Message {
	if a == nil {
		return nil
	}
	severity := a.Severity
	if severity == "" {
		severity = "info"
	}
	return &notify.Message{
		Title:     fmt.Sprintf("[OpsMesh][%s] %s", severity, a.Message),
		Body:      fmt.Sprintf("告警 %s：%s（设备 %s，租户 %s）", a.AlertID, a.Message, a.DeviceID, a.TenantID),
		Format:    "text",
		Severity:  severity,
		Source:    a.DeviceID,
		Timestamp: a.CreatedAt,
		Data:      a,
	}
}

// deliverAlert 外发一条 firing 业务告警（M7 notifyLoop 唯一调用点）：
// CLI 单通道（alertChannels）+ 文件多渠道（alertNotifier 内的 notify.Channel）。
// 两条都没有 configured 时返回 nil（无渠道可发，与 notify.Channels.Push 的既有
// 语义一致）；任一路失败聚合返回，调用方据此撤销指纹标记下轮重试。
func (s *Server) deliverAlert(a *proto.Alert) error {
	if a == nil {
		return nil
	}
	var errs []error
	if s.alertChannels != nil {
		if err := s.alertChannels.Push(a); err != nil {
			errs = append(errs, fmt.Errorf("CLI 单通道: %w", err))
		}
	}
	if s.alertNotifier != nil && len(s.alertNotifier.Channels()) > 0 {
		if err := s.alertNotifier.Notify(alertToNotifyMessage(a)); err != nil {
			errs = append(errs, fmt.Errorf("文件多渠道: %w", err))
		}
	}
	if len(errs) == 0 {
		return nil
	}
	msg := ""
	for i, e := range errs {
		if i > 0 {
			msg += "; "
		}
		msg += e.Error()
	}
	return fmt.Errorf("告警外发失败: %s", msg)
}
