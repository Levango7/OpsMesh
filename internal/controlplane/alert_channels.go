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
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/internal/notify"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/secrets"
	"github.com/Levango7/OpsMesh/internal/store"
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

// notifyDedupRetryOptions 按 cfg 产出去重/重试两个选项（纯函数，便于行为测试组装）。
// 语义见 newAlertNotifier 注释：0=关闭（不挂选项），非 0 按 cfg 值交给选项自身归一化。
func notifyDedupRetryOptions(cfg *config.Config) []notify.NotifierOption {
	var opts []notify.NotifierOption
	if cfg.NotifyDedupTTLMin == 0 {
		logx.Info(context.Background(), "通知去重已关闭（--notify-dedup-ttl-min=0）", "scope", "alertNotifier")
	} else {
		opts = append(opts, notify.WithDedup(time.Duration(cfg.NotifyDedupTTLMin)*time.Minute))
	}
	if cfg.NotifyRetryMaxAttempts == 0 {
		logx.Info(context.Background(), "通知重试已关闭（--notify-retry-max-attempts=0，单次发送）", "scope", "alertNotifier")
	} else {
		opts = append(opts, notify.WithRetry(&notify.RetryPolicy{
			MaxAttempts: cfg.NotifyRetryMaxAttempts,
			Interval:    cfg.NotifyRetryInterval,
			Backoff:     cfg.NotifyRetryBackoff,
		}))
	}
	return opts
}

// newAlertNotifier 装配告警 Notifier：去重 + 重试 + 文件渠道 +（可选）密钥提供者。
//
// TD-92 起这是**唯一**的装配入口（server.go 两处构造与 SecretProvider 重建都走它），
// 渠道在此一次性注入、构造后只读（内部并发安全见 notify.Notifier 文档）。
//
// 去重/重试旋钮（--notify-dedup-ttl-min / --notify-retry-*，2026-10-12 接线）：
// 配置语义是「0=关闭」，而 notify 的两个选项表达不了"关闭"——WithDedup(<=0) 是
// "用默认 5min"、WithRetry(nil) 是"用默认策略"（RetryPolicy.normalize 还把
// MaxAttempts<=0 归一成 3）。因此关闭时**索性不挂对应选项**（Notifier 零值
// dedup=nil 即不去重、retry=nil 即单次发送），非 0 时按 cfg 值构造；
// 负值等非法输入落进选项自身的归一化（回退默认），不静默变成"关闭"。
// 默认值（5min/3 次/5s/2.0）与接线前的硬编码行为逐字一致。
func newAlertNotifier(cfg *config.Config, provider secrets.SecretProvider) *notify.Notifier {
	opts := notifyDedupRetryOptions(cfg)
	opts = append(opts,
		notify.WithChannels(buildNotifierChannels(cfg)...),
		notify.WithSecretProvider(provider),
	)
	return notify.NewNotifier(opts...)
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
// CLI 单通道（alertChannels）+ 文件多渠道（alertNotifier 内的 notify.Channel）
// + **UI 渠道**（notify-channels 表，租户隔离 + 按规则选渠道）。
// 三条都没 configured 时返回 nil（无渠道可发，与 notify.Channels.Push 的既有
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
	// UI 渠道（notify-channels 表）：TD-92 第二批——2026-10-12 起真正参与自动外发
	// （此前该表只有测试按钮一个消费方）。
	for _, sc := range s.storeChannelsFor(a) {
		if err := sc.Send(alertToNotifyMessage(a)); err != nil {
			errs = append(errs, fmt.Errorf("UI 渠道 %s: %w", s.storeChannelName(sc), err))
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

// storeChannelName 返回渠道的展示标识（用于错误信息；构造失败/无名时退回类型）。
func (s *Server) storeChannelName(ch notify.Channel) string {
	type namer interface{ Name() string }
	if n, ok := ch.(namer); ok {
		return n.Name()
	}
	return "unknown"
}

// storeChannelsFor 选出这条告警该送达的 **UI 渠道**（notify-channels 表）：
//   - 租户隔离：只收 c.TenantID 与告警租户相同的 enabled 渠道（空串归一为 default，
//     与 store 保存侧一致）——多租户下 A 的 webhook 不能收到 B 的告警；
//   - 按规则选渠道：告警来自 M2 规则且该规则 NotifyChannels 非空时，只送列表内的
//     （选择意图由 notifyAlertGroup 在落库时记进 alertChannelSel，见其注释）；
//     无记录 = 不过滤（设备/任务类告警与未配选择的规则都送全部 enabled 渠道）；
//   - 构造失败（配置 JSON 坏/类型不支持/${key} 解析失败）的渠道跳过并记日志——
//     一条坏配置不该让整轮外发失败（与启动期加载文件渠道同款策略）。
func (s *Server) storeChannelsFor(a *proto.Alert) []notify.Channel {
	tenant := a.TenantID
	if tenant == "" {
		tenant = "default"
	}
	selected, hasSelection := s.alertChannelSelection(a.AlertID)
	var out []notify.Channel
	for _, c := range s.store.ListNotifyChannels("") {
		if c == nil || !c.Enabled {
			continue
		}
		cTenant := c.TenantID
		if cTenant == "" {
			cTenant = "default"
		}
		if cTenant != tenant {
			continue
		}
		if hasSelection && !containsString(selected, c.ID) {
			continue
		}
		ch, err := buildChannel(c, s.secretProvider)
		if err != nil || ch == nil {
			logx.Error(context.Background(), "UI 渠道构造失败，本轮跳过", err, "channelID", c.ID, "type", c.Type)
			continue
		}
		out = append(out, ch)
	}
	return out
}

func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// alertChannelSelEntry 是一条"按规则选渠道"的意图登记：ids=规则选的渠道 ID 列表，
// at=登记时间（刷新即重写，与 alertSent 同用 24h  cutoff 清理）。
type alertChannelSelEntry struct {
	ids []string
	at  time.Time
}

// alertChannelSelection 读取落库时记下的"按规则选渠道"意图（AlertID → 渠道 ID 列表）。
// 第二条返回值=false 表示无记录（= 不过滤）。
func (s *Server) alertChannelSelection(alertID string) ([]string, bool) {
	s.alertChannelSelMu.Lock()
	defer s.alertChannelSelMu.Unlock()
	if s.alertChannelSel == nil || alertID == "" {
		return nil, false
	}
	e, ok := s.alertChannelSel[alertID]
	return e.ids, ok
}

// noteAlertChannelSelection 登记一条告警的渠道选择意图（仅当规则显式选了渠道）。
// M2 每轮评估会重复落库同一条告警（AlertID 确定性生成）⇒ 时间戳随之刷新，
// 只要告警还在 firing 窗口内意图就不会过期。
func (s *Server) noteAlertChannelSelection(alertID string, ids []string) {
	if alertID == "" || len(ids) == 0 {
		return
	}
	s.alertChannelSelMu.Lock()
	defer s.alertChannelSelMu.Unlock()
	if s.alertChannelSel == nil {
		s.alertChannelSel = make(map[string]alertChannelSelEntry)
	}
	s.alertChannelSel[alertID] = alertChannelSelEntry{ids: ids, at: time.Now()}
}

// pruneAlertChannelSel 清理早于 cutoff 的意图登记（与 alertSent 同 cutoff 同调用点；
// 自带时间戳而不是"跟着 alertSent 在不在"走——推送失败重试期间 alertSent 里没有该
// 告警，但意图必须留着，否则重推就不过滤了）。
func (s *Server) pruneAlertChannelSel(cutoff time.Time) {
	s.alertChannelSelMu.Lock()
	defer s.alertChannelSelMu.Unlock()
	for id, e := range s.alertChannelSel {
		if e.at.Before(cutoff) {
			delete(s.alertChannelSel, id)
		}
	}
}

// ============================================================================
// TD-88 全链路：通知渠道明文凭据的写入期告警 + 启动期存量扫描
// ============================================================================

// warnPlaintextChannel 写入期告警：渠道配置里的敏感字段是明文时点名（不阻断，保持
// 向后兼容）。为什么连 webhookURL 也算敏感：多数 webhook 的 URL 里直接内嵌 token
// （钉钉/企微/飞书/Slack 皆然），拿到 URL 即等于拿到凭据；email 的 pass 亦同。
// 调用点：createNotifyChannel / updateNotifyChannel（写一次告警一次，不再在每次
// 发送时重复——见 buildChannel 注释）。
func warnPlaintextChannel(c *store.NotifyChannel, provider secrets.SecretProvider) {
	if c == nil {
		return
	}
	var cfg map[string]string
	if c.Config != "" {
		if err := json.Unmarshal([]byte(c.Config), &cfg); err != nil {
			return // 配置坏JSON由构造/校验路径报错，这里不重复
		}
	}
	fields := PlaintextSensitiveFields(c.Type, cfg)
	if len(fields) == 0 {
		return
	}
	providerState := "已配置 --secret-provider"
	if provider == nil {
		providerState = "未配置 --secret-provider（明文只能从配置里读）"
	}
	logx.Warn(context.Background(),
		"通知渠道存在明文凭据（建议改为 ${provider:key} 引用，使密钥不随库/配置同机落盘）",
		"channelID", c.ID, "type", c.Type, "fields", strings.Join(fields, ","), "provider", providerState)
}

// notifyChannelLister 是启动扫描所需的最小接口面（同 secretLister 先例：便于替身单测）。
type notifyChannelLister interface {
	ListNotifyChannels(tenantID string) []*store.NotifyChannel
}

// plaintextNotifyChannelKeys 返回 notify-channels 表里**含明文敏感字段**的渠道标识
// （"tenant/id(field,...)"，升序），供启动告警使用。纯函数（谓词可测，同
// plaintextSecretKeys 先例）："什么叫明文"的判据随演进容易漂移，用替身表驱动钉住。
func plaintextNotifyChannelKeys(st notifyChannelLister) []string {
	var plain []string
	for _, c := range st.ListNotifyChannels("") {
		if c == nil {
			continue
		}
		// 注意：**不按 Enabled 过滤**——存量就是存量，停用的渠道里躺着明文凭据
		// 同样该被点名（启用与否是投递语义，不是安全扫描语义）。
		var cfg map[string]string
		if c.Config != "" {
			if err := json.Unmarshal([]byte(c.Config), &cfg); err != nil {
				tenant := c.TenantID
				if tenant == "" {
					tenant = "default"
				}
				plain = append(plain, tenant+"/"+c.ID+"(config-unparsable)")
				continue
			}
		}
		if fields := PlaintextSensitiveFields(c.Type, cfg); len(fields) > 0 {
			tenant := c.TenantID
			if tenant == "" {
				tenant = "default"
			}
			plain = append(plain, tenant+"/"+c.ID+"("+strings.Join(fields, ",")+")")
		}
	}
	sort.Strings(plain)
	return plain
}

// warnPlaintextNotifyChannels 启动扫描：notify-channels 表存在明文凭据时告警
// （TD-88 全链路的启动期那一环；写入期那一环是 warnPlaintextChannel）。
// 只告警不阻断：存量是历史数据，阻断会让升级直接失败；该表自 TD-92 第二批起真正
// 参与自动外发，告警把"该换引用"变成可观测事件。
func warnPlaintextNotifyChannels(st notifyChannelLister) {
	plain := plaintextNotifyChannelKeys(st)
	if len(plain) == 0 {
		return
	}
	const maxShown = 5
	shown := plain
	if len(shown) > maxShown {
		shown = shown[:maxShown]
	}
	logx.Warn(context.Background(), "notify-channels 表存在明文凭据存量（建议改为 ${provider:key} 引用）",
		"count", len(plain), "sample", strings.Join(shown, ","))
}
