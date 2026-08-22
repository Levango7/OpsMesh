// secret_test.go 告警通道密钥外置测试。
//
// 覆盖：
//   - ResolveSecret 对明文/引用的处理（与 secrets 包的单元测试互补，此处验证 notify 集成路径）。
//   - NewDingTalkChannelWithSecret 三种模式：明文 / 引用解析 / nil provider。
//   - Notifier.WithSecretProvider 选项注入后正常工作。

package notify

import (
	"testing"
	"time"

	"opsmesh/internal/proto"
	"opsmesh/internal/secrets"
)

// mockSecretProvider 基于 map 的测试用 SecretProvider。
// 不依赖外部 env/file/vault，纯内存实现，测试快速且确定。
type mockSecretProvider struct {
	store map[string]string
}

// newMockSecretProvider 构造 mock provider。
func newMockSecretProvider(kvs map[string]string) *mockSecretProvider {
	return &mockSecretProvider{store: kvs}
}

// Get 实现 SecretProvider 接口。
func (m *mockSecretProvider) Get(key string) (string, error) {
	if v, ok := m.store[key]; ok {
		return v, nil
	}
	return "", secrets.ErrSecretNotFound
}

// Name 实现 SecretProvider 接口。
func (m *mockSecretProvider) Name() string { return "mock" }

// TestResolveSecret_PlainWebhookURL 明文 URL 不解析，直接返回原值。
// 验证向后兼容：未启用密钥外置时，明文配置应原样透传。
func TestResolveSecret_PlainWebhookURL(t *testing.T) {
	provider := newMockSecretProvider(map[string]string{
		"notify/dingtalk/webhook": "https://oapi.dingtalk.com/robot/send?access_token=secret",
	})
	plain := "https://oapi.dingtalk.com/robot/send?access_token=plain"
	got, err := secrets.ResolveSecret(plain, provider)
	if err != nil {
		t.Fatalf("明文 URL 解析失败: %v", err)
	}
	if got != plain {
		t.Errorf("明文 URL 应原样返回: got=%q want=%q", got, plain)
	}
}

// TestResolveSecret_ReferencedWebhookURL ${key} 格式从 provider 解析。
// 验证密钥引用能正确解析为 provider 中的值。
func TestResolveSecret_ReferencedWebhookURL(t *testing.T) {
	want := "https://oapi.dingtalk.com/robot/send?access_token=resolved-token"
	provider := newMockSecretProvider(map[string]string{
		"notify/dingtalk/webhook": want,
	})
	ref := "${notify/dingtalk/webhook}"
	got, err := secrets.ResolveSecret(ref, provider)
	if err != nil {
		t.Fatalf("引用 URL 解析失败: %v", err)
	}
	if got != want {
		t.Errorf("引用 URL 解析结果不符: got=%q want=%q", got, want)
	}
}

// TestNewDingTalkChannelWithSecret_Plain 明文参数构造，行为与原版 NewDingTalkChannel 一致。
// 验证 WithSecret 版本在明文输入下不改变渠道行为。
func TestNewDingTalkChannelWithSecret_Plain(t *testing.T) {
	provider := newMockSecretProvider(nil)
	url := "https://oapi.dingtalk.com/robot/send?access_token=plain"
	secret := "SECplain123"
	ch, err := NewDingTalkChannelWithSecret(url, secret, provider)
	if err != nil {
		t.Fatalf("明文构造失败: %v", err)
	}
	if ch.webhookURL != url {
		t.Errorf("webhookURL 不符: got=%q want=%q", ch.webhookURL, url)
	}
	if ch.secret != secret {
		t.Errorf("secret 不符: got=%q want=%q", ch.secret, secret)
	}
	// 与原版对比：构造结果应完全一致。
	orig := NewDingTalkChannel(url, secret)
	if orig.webhookURL != ch.webhookURL || orig.secret != ch.secret {
		t.Errorf("WithSecret 明文构造与原版不一致: withSecret=%+v orig=%+v", ch, orig)
	}
}

// TestNewDingTalkChannelWithSecret_Resolved 密钥引用解析后构造。
// 验证 ${key} 引用被解析为 provider 中的值后再构造渠道。
func TestNewDingTalkChannelWithSecret_Resolved(t *testing.T) {
	wantURL := "https://oapi.dingtalk.com/robot/send?access_token=resolved"
	wantSecret := "SECresolved456"
	provider := newMockSecretProvider(map[string]string{
		"notify/dingtalk/webhook": wantURL,
		"notify/dingtalk/secret":  wantSecret,
	})
	ch, err := NewDingTalkChannelWithSecret(
		"${notify/dingtalk/webhook}",
		"${notify/dingtalk/secret}",
		provider,
	)
	if err != nil {
		t.Fatalf("引用构造失败: %v", err)
	}
	if ch.webhookURL != wantURL {
		t.Errorf("webhookURL 解析不符: got=%q want=%q", ch.webhookURL, wantURL)
	}
	if ch.secret != wantSecret {
		t.Errorf("secret 解析不符: got=%q want=%q", ch.secret, wantSecret)
	}
}

// TestNewDingTalkChannelWithSecret_NilProvider provider=nil 时退化为明文。
// 验证向后兼容：未注入 provider 时，引用串原样保留（不解析也不报错）。
// 注意：这是 secrets.ResolveSecret 的契约——provider 为 nil 时引用串原样返回。
// 调用方应在启用密钥外置时确保 provider 非 nil。
func TestNewDingTalkChannelWithSecret_NilProvider(t *testing.T) {
	url := "https://oapi.dingtalk.com/robot/send?access_token=plain"
	secret := "SECplain789"
	ch, err := NewDingTalkChannelWithSecret(url, secret, nil)
	if err != nil {
		t.Fatalf("nil provider 构造失败: %v", err)
	}
	if ch.webhookURL != url {
		t.Errorf("nil provider 下明文 webhookURL 应原样保留: got=%q want=%q", ch.webhookURL, url)
	}
	if ch.secret != secret {
		t.Errorf("nil provider 下明文 secret 应原样保留: got=%q want=%q", ch.secret, secret)
	}
}

// TestNotifier_WithSecretProvider Notifier 设置 secretProvider 后正常工作。
// 验证：
//   - WithSecretProvider 选项正确注入 provider 到 Notifier；
//   - SecretProvider() 访问器返回注入的 provider；
//   - BuildChannel 在 provider 非空时用 WithSecret 版本构造渠道（解析引用）；
//   - Notifier.Notify 流程不因 secretProvider 注入而异常。
func TestNotifier_WithSecretProvider(t *testing.T) {
	wantURL := "https://oapi.dingtalk.com/robot/send?access_token=resolved"
	provider := newMockSecretProvider(map[string]string{
		"notify/dingtalk/webhook": wantURL,
	})
	n := NewNotifier(
		WithDedup(5*time.Minute),
		WithSecretProvider(provider),
	)
	// 验证访问器。
	got := n.SecretProvider()
	if got == nil {
		t.Fatal("SecretProvider() 返回 nil，注入未生效")
	}
	if got.Name() != "mock" {
		t.Errorf("provider Name 不符: got=%q want=mock", got.Name())
	}
	// 验证 BuildChannel 用 WithSecret 版本解析引用。
	ch, err := n.BuildChannel(ChannelConfig{
		Type:       "dingtalk",
		WebhookURL: "${notify/dingtalk/webhook}",
		Secret:     "",
	})
	if err != nil {
		t.Fatalf("BuildChannel 失败: %v", err)
	}
	ding, ok := ch.(*DingTalkChannel)
	if !ok {
		t.Fatalf("BuildChannel 返回类型不符: got=%T want=*DingTalkChannel", ch)
	}
	if ding.webhookURL != wantURL {
		t.Errorf("BuildChannel 解析引用失败: got=%q want=%q", ding.webhookURL, wantURL)
	}
	// 验证 Notifier.Notify 流程不因 secretProvider 注入而异常。
	// 无渠道时 Notify 静默返回 nil（secretProvider 不影响无渠道行为）。
	msg := &Message{
		Title:     "test",
		Body:      "test body",
		Format:    "markdown",
		Severity:  "info",
		Source:    "test",
		Timestamp: time.Now(),
	}
	if err := n.Notify(msg); err != nil {
		t.Errorf("无渠道 Notify 应返回 nil: got=%v", err)
	}
	// 追加一个真实渠道（指向 httptest 不必要，webhookURL 为空时 Send 静默返回 nil）。
	n.AddChannel(NewDingTalkChannel("", ""))
	if err := n.Notify(msg); err != nil {
		t.Errorf("空 webhookURL 渠道 Notify 应返回 nil: got=%v", err)
	}
}

// TestNewChannelWithSecret_EmailPasswordResolved 验证 email 渠道 password 支持密钥引用解析。
// 补充覆盖 NewChannelWithSecret 工厂对 email 类型的密钥外置支持。
func TestNewChannelWithSecret_EmailPasswordResolved(t *testing.T) {
	wantPass := "smtp-resolved-password"
	provider := newMockSecretProvider(map[string]string{
		"notify/email/pass": wantPass,
	})
	ch, err := NewChannelWithSecret(ChannelConfig{
		Type:     "email",
		SMTPHost: "smtp.example.com",
		SMTPPort: 587,
		Username: "user@example.com",
		Password: "${notify/email/pass}",
		From:     "opsmesh@example.com",
		To:       []string{"ops@example.com"},
	}, provider)
	if err != nil {
		t.Fatalf("email 渠道构造失败: %v", err)
	}
	email, ok := ch.(*EmailChannel)
	if !ok {
		t.Fatalf("返回类型不符: got=%T want=*EmailChannel", ch)
	}
	if email.password != wantPass {
		t.Errorf("email password 解析不符: got=%q want=%q", email.password, wantPass)
	}
}

// TestNewChannelWithSecret_NilProviderBackwardCompatible 验证 provider=nil 时退化为 NewChannel。
// 确保未启用密钥外置的部署升级后行为不变。
func TestNewChannelWithSecret_NilProviderBackwardCompatible(t *testing.T) {
	url := "https://oapi.dingtalk.com/robot/send?access_token=plain"
	secret := "SECplain"
	ch, err := NewChannelWithSecret(ChannelConfig{
		Type:       "dingtalk",
		WebhookURL: url,
		Secret:     secret,
	}, nil)
	if err != nil {
		t.Fatalf("nil provider 构造失败: %v", err)
	}
	ding, ok := ch.(*DingTalkChannel)
	if !ok {
		t.Fatalf("返回类型不符: got=%T want=*DingTalkChannel", ch)
	}
	if ding.webhookURL != url || ding.secret != secret {
		t.Errorf("nil provider 下应原样保留: got webhookURL=%q secret=%q", ding.webhookURL, ding.secret)
	}
}

// TestAlertToMessage_WithSecretProviderNotAffected 验证 AlertToMessage 不受 secretProvider 影响。
// AlertToMessage 是纯转换函数，不涉及密钥，确保密钥外置改造未误伤转换路径。
func TestAlertToMessage_WithSecretProviderNotAffected(t *testing.T) {
	alert := &proto.Alert{
		AlertID:   "alert-1",
		Severity:  "critical",
		DeviceID:  "dev-1",
		AgentID:   "agent-1",
		Message:   "test message",
		CreatedAt: time.Now(),
	}
	msg := AlertToMessage(alert)
	if msg == nil {
		t.Fatal("AlertToMessage 返回 nil")
	}
	if msg.Severity != "critical" {
		t.Errorf("Severity 不符: got=%q want=critical", msg.Severity)
	}
	if msg.Source != "dev-1" {
		t.Errorf("Source 不符: got=%q want=dev-1", msg.Source)
	}
}
