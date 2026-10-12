package controlplane

// td92_alert_channels_test.go — TD-92 收口守卫：告警外发渠道的单一口径。
//
// 锁住四件事：
//  1. --notify-channels-config 的文件渠道**真的被注入** alertNotifier（此前生产两处
//     构造从不传 WithChannels ⇒ channels 恒空 ⇒ Notify 静默丢弃）；
//  2. SecretProvider 重建（server.go 密钥外置分支）**不丢渠道**（旧代码裸 NewNotifier，
//     重建即丢）；
//  3. 单渠道 URL 未过 SSRF 准入时**跳过该条**而不是让配置整体失效/静默全发；
//  4. notifyAlertGroup **不再直调 Notify**（外发统一走 store → notifyLoop →
//     deliverAlert；两边同时发会重复投递），automation SendNotify 无渠道时
//     **明确报错**而不是静默成功。

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/Levango7/OpsMesh/internal/alertengine"
	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/notify"
	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/secrets"
	"github.com/Levango7/OpsMesh/internal/store"
)

// fakeNotifyChannel 记录收到的消息；failWith 非空时 Send 返回该错误。
type fakeNotifyChannel struct {
	mu       sync.Mutex
	messages []*notify.Message
	failWith error
}

func (f *fakeNotifyChannel) Send(msg *notify.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, msg)
	return f.failWith
}

func (f *fakeNotifyChannel) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.messages)
}

// emailChannelCfg 构造一个合法 email 渠道配置（不走 webhook URL 校验，无网络依赖）。
func emailChannelCfg() config.NotifyChannelConfig {
	return config.NotifyChannelConfig{
		Type: "email", SMTPHost: "smtp.example.com", SMTPPort: 25,
		From: "ops@example.com", To: []string{"ops1@example.com"},
	}
}

// TestNewAlertNotifier_InjectsFileChannels 文件渠道必须进入 Notifier。
// 这是 TD-92 的核心判据：此前 --notify-channels-config 只被 redactNotifyChannels
// 诊断输出消费，没有任何发送路径用它。
func TestNewAlertNotifier_InjectsFileChannels(t *testing.T) {
	cfg := &config.Config{NotifyChannels: []config.NotifyChannelConfig{emailChannelCfg(), emailChannelCfg()}}
	n := newAlertNotifier(cfg, nil)
	if got := len(n.Channels()); got != 2 {
		t.Fatalf("文件渠道应全部注入，实得 %d，want 2", got)
	}
}

// TestNewAlertNotifier_SkipsSSRFBlockedChannel 私网 URL 在未开 --webhook-allow-private
// 时必须被跳过（与 M7 单通道同款准入），其余渠道不受影响、不阻断启动。
func TestNewAlertNotifier_SkipsSSRFBlockedChannel(t *testing.T) {
	cfg := &config.Config{
		WebhookAllowPrivate: false,
		NotifyChannels: []config.NotifyChannelConfig{
			{Type: "dingtalk", WebhookURL: "http://127.0.0.1:8080/hook"},
			emailChannelCfg(),
		},
	}
	n := newAlertNotifier(cfg, nil)
	if got := len(n.Channels()); got != 1 {
		t.Fatalf("SSRF 未过的渠道应被跳过、合法渠道保留，实得 %d，want 1", got)
	}
}

// TestNewAlertNotifier_SecretProviderRebuildKeepsChannels 密钥外置重建不得丢渠道：
// server.go 的 secretProvider 分支走同一 helper（旧代码在那里裸 NewNotifier，
// 只在 WithSecretProvider 重建，渠道静默消失）。
func TestNewAlertNotifier_SecretProviderRebuildKeepsChannels(t *testing.T) {
	cfg := &config.Config{NotifyChannels: []config.NotifyChannelConfig{emailChannelCfg()}}
	n := newAlertNotifier(cfg, nil)
	if len(n.Channels()) != 1 {
		t.Fatalf("前置：无 provider 时应有 1 个渠道，实得 %d", len(n.Channels()))
	}
	// 直接对同一 cfg 走"重建"路径（与 server.go:424 相同调用形态）。
	rebuilt := newAlertNotifier(cfg, secrets.NewEnvProvider("OPSMESH_"))
	if got := len(rebuilt.Channels()); got != 1 {
		t.Fatalf("重建后渠道丢失：实得 %d，want 1（TD-92 回归点）", got)
	}
	if rebuilt.SecretProvider() == nil {
		t.Error("重建后 SecretProvider 应可用（渠道 ${key} 引用解析依赖它）")
	}
}

// TestDeliverAlert_FileChannelsReceiveMessage 文件多渠道经 deliverAlert 收到
// message 形状的消息，且 Data 携带原始告警（渠道可定制渲染）。
func TestDeliverAlert_FileChannelsReceiveMessage(t *testing.T) {
	fake := &fakeNotifyChannel{}
	s := &Server{
		store:         store.NewMemoryStore(),
		alertNotifier: notify.NewNotifier(notify.WithChannels(fake)),
	}
	a := &proto.Alert{AlertID: "al-1", TenantID: "t1", DeviceID: "d1", Severity: "critical", Message: "cpu high"}
	if err := s.deliverAlert(a); err != nil {
		t.Fatalf("deliverAlert: %v", err)
	}
	if fake.count() != 1 {
		t.Fatalf("文件渠道应收到 1 条消息，实得 %d", fake.count())
	}
	msg := fake.messages[0]
	if !strings.Contains(msg.Title, "cpu high") {
		t.Errorf("消息标题应含告警内容，实得 %q", msg.Title)
	}
	if msg.Data != a {
		t.Error("msg.Data 应携带原始 *proto.Alert（渠道定制渲染用）")
	}
}

// TestDeliverAlert_NoChannelsNoError 两条路径都没配置时不得报错、不得 panic
// （notifyLoop 在无通道部署下照常轮转，与 notify.Channels.Push 既有语义一致）。
func TestDeliverAlert_NoChannelsNoError(t *testing.T) {
	s := &Server{store: store.NewMemoryStore()}
	a := &proto.Alert{AlertID: "al-2", Severity: "warning", Message: "disk"}
	if err := s.deliverAlert(a); err != nil {
		t.Fatalf("无渠道时 deliverAlert 应返回 nil，实得 %v", err)
	}
	if err := s.deliverAlert(nil); err != nil {
		t.Fatalf("nil 告警应直接返回 nil，实得 %v", err)
	}
}

// TestDeliverAlert_AggregatesChannelErrors 任一路失败要聚合报错（notifyLoop 据此
// 撤销指纹下轮重试，不会永久丢通知）。
func TestDeliverAlert_AggregatesChannelErrors(t *testing.T) {
	fake := &fakeNotifyChannel{failWith: context.DeadlineExceeded}
	s := &Server{
		store:         store.NewMemoryStore(),
		alertNotifier: notify.NewNotifier(notify.WithChannels(fake)),
	}
	a := &proto.Alert{AlertID: "al-3", Severity: "critical", Message: "boom"}
	if err := s.deliverAlert(a); err == nil {
		t.Fatal("渠道发送失败应报错（调用方要撤销指纹重试）")
	}
}

// TestNotifyAlertGroup_DoesNotNotifyDirectly M2 聚合组不再直调 Notify（TD-92）：
// 外发统一由 store → notifyLoop → deliverAlert 完成。若本测试判红，说明双发路径
// 回归（组消息直发 + notifyLoop 再发 = 重复投递）。
func TestNotifyAlertGroup_DoesNotNotifyDirectly(t *testing.T) {
	fake := &fakeNotifyChannel{}
	s := newAlertsTestServer()
	s.alertNotifier = notify.NewNotifier(notify.WithChannels(fake))
	g := &alertengine.AlertGroup{
		Key: "deviceID=d1|severity=critical",
		Events: []*alertengine.AlertEvent{
			{RuleID: "r1", TenantID: "t1", DeviceID: "d1", Severity: "critical",
				Message: "cpu high", Labels: map[string]string{"ruleID": "r1", "deviceID": "d1", "severity": "critical"}},
		},
	}
	s.notifyAlertGroup(context.Background(), g)
	if fake.count() != 0 {
		t.Errorf("notifyAlertGroup 不应直调 Notify（双发路径回归），实得 %d 条", fake.count())
	}
	// 但事件必须落库（notifyLoop 的发送源）。
	if alerts := s.store.Alerts("t1"); len(alerts) != 1 {
		t.Fatalf("聚合组应写入 store，实得 %d 条", len(alerts))
	}
}

// TestAutomationSendNotify_NoChannelsFailsLoudly 无渠道时明确报错而不是静默成功
// （TD-92 根因：调用方此前收到"成功"，实际一条都没发）。
func TestAutomationSendNotify_NoChannelsFailsLoudly(t *testing.T) {
	ex := &automationExecutor{
		store:    store.NewMemoryStore(),
		notifier: notify.NewNotifier(notify.WithDedup(0)),
	}
	err := ex.SendNotify("t1", "dingtalk", "disk almost full", map[string]string{"severity": "warning"})
	if err == nil {
		t.Fatal("无渠道时 SendNotify 必须报错（不得静默返回 nil）")
	}
	if !strings.Contains(err.Error(), "notify-channels-config") {
		t.Errorf("错误应指引配置入口 --notify-channels-config，实得 %q", err.Error())
	}
}

// TestAutomationSendNotify_DeliversViaFileChannels 正路径：注入了文件渠道的
// notifier 真实收到自动化通知（message 形状）。
func TestAutomationSendNotify_DeliversViaFileChannels(t *testing.T) {
	fake := &fakeNotifyChannel{}
	ex := &automationExecutor{
		store:    store.NewMemoryStore(),
		notifier: notify.NewNotifier(notify.WithChannels(fake)),
	}
	if err := ex.SendNotify("t1", "dingtalk", "disk almost full", map[string]string{"severity": "warning"}); err != nil {
		t.Fatalf("SendNotify: %v", err)
	}
	if fake.count() != 1 {
		t.Fatalf("自动化通知应送达文件渠道，实得 %d 条", fake.count())
	}
	if !strings.Contains(fake.messages[0].Body, "disk almost full") {
		t.Errorf("通知正文应透传，实得 %q", fake.messages[0].Body)
	}
}
