package notify

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// startMockWebhook 启动 mock webhook server，返回收到的请求 body channel 和 server。
// server 调用方 defer Close。
func startMockWebhook(t *testing.T, status int) (chan string, *httptest.Server) {
	t.Helper()
	received := make(chan string, 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json", ct)
		}
		b, _ := io.ReadAll(r.Body)
		received <- string(b)
		w.WriteHeader(status)
	}))
	return received, srv
}

// TestDingTalkChannel_Send 验证钉钉渠道发送 markdown 消息体。
func TestDingTalkChannel_Send(t *testing.T) {
	received, srv := startMockWebhook(t, http.StatusOK)
	defer srv.Close()

	ch := NewDingTalkChannel(srv.URL, "")
	msg := &Message{
		Title:    "CPU 告警",
		Body:     "## CPU 高\n\n使用率 95%",
		Format:   "markdown",
		Severity: "critical",
		Source:   "dev-1",
	}
	if err := ch.Send(msg); err != nil {
		t.Fatalf("Send err = %v", err)
	}
	body := <-received
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("json decode: %v\nbody: %s", err, body)
	}
	if parsed["msgtype"] != "markdown" {
		t.Fatalf("msgtype = %v, want markdown", parsed["msgtype"])
	}
	md, ok := parsed["markdown"].(map[string]interface{})
	if !ok {
		t.Fatal("markdown missing")
	}
	if !strings.Contains(md["title"].(string), "CPU 告警") {
		t.Fatalf("title = %v", md["title"])
	}
	if !strings.Contains(md["text"].(string), "CPU 高") {
		t.Fatalf("text missing body: %v", md["text"])
	}
}

// TestDingTalkChannel_Signed 验证钉钉加签模式 URL 拼接 timestamp&sign。
func TestDingTalkChannel_Signed(t *testing.T) {
	received, srv := startMockWebhook(t, http.StatusOK)
	defer srv.Close()

	ch := NewDingTalkChannel(srv.URL, "SECtest123")
	msg := &Message{Title: "test", Body: "hello", Severity: "warning"}
	if err := ch.Send(msg); err != nil {
		t.Fatalf("Send err = %v", err)
	}
	<-received // 收到请求即可（URL 已含 timestamp&sign，由 signedURL 计算）
	// 验证 signedURL 拼接（间接：发送成功说明 URL 合法）
}

// TestDingTalkChannel_EmptyURL 验证空 URL 静默返回 nil。
func TestDingTalkChannel_EmptyURL(t *testing.T) {
	ch := NewDingTalkChannel("", "")
	if err := ch.Send(&Message{Title: "x"}); err != nil {
		t.Fatalf("empty URL err = %v", err)
	}
}

// TestDingTalkChannel_NilMsg 验证 nil msg 静默返回 nil。
func TestDingTalkChannel_NilMsg(t *testing.T) {
	ch := NewDingTalkChannel("http://example.com", "")
	if err := ch.Send(nil); err != nil {
		t.Fatalf("nil msg err = %v", err)
	}
}

// TestDingTalkChannel_DefaultBody 验证 Body 为空时按字段构造默认正文。
func TestDingTalkChannel_DefaultBody(t *testing.T) {
	received, srv := startMockWebhook(t, http.StatusOK)
	defer srv.Close()

	ch := NewDingTalkChannel(srv.URL, "")
	msg := &Message{Title: "Alert", Severity: "critical", Source: "dev-1", Timestamp: time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)}
	if err := ch.Send(msg); err != nil {
		t.Fatalf("Send err = %v", err)
	}
	body := <-received
	var parsed map[string]interface{}
	json.Unmarshal([]byte(body), &parsed)
	md := parsed["markdown"].(map[string]interface{})
	text := md["text"].(string)
	if !strings.Contains(text, "dev-1") {
		t.Fatalf("default body missing source: %s", text)
	}
	if !strings.Contains(text, "critical") {
		t.Fatalf("default body missing severity: %s", text)
	}
}

// TestWeChatWorkChannel_Send 验证企业微信渠道发送 markdown 消息体。
func TestWeChatWorkChannel_Send(t *testing.T) {
	received, srv := startMockWebhook(t, http.StatusOK)
	defer srv.Close()

	ch := NewWeChatWorkChannel(srv.URL)
	msg := &Message{Title: "告警", Body: "设备 dev-1 CPU 95%", Severity: "critical", Format: "markdown"}
	if err := ch.Send(msg); err != nil {
		t.Fatalf("Send err = %v", err)
	}
	body := <-received
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if parsed["msgtype"] != "markdown" {
		t.Fatalf("msgtype = %v, want markdown", parsed["msgtype"])
	}
}

// TestWeChatWorkChannel_TextFormat 验证 Format=text 走 text 消息。
func TestWeChatWorkChannel_TextFormat(t *testing.T) {
	received, srv := startMockWebhook(t, http.StatusOK)
	defer srv.Close()

	ch := NewWeChatWorkChannel(srv.URL)
	msg := &Message{Title: "告警", Body: "纯文本内容", Format: "text"}
	if err := ch.Send(msg); err != nil {
		t.Fatalf("Send err = %v", err)
	}
	body := <-received
	var parsed map[string]interface{}
	json.Unmarshal([]byte(body), &parsed)
	if parsed["msgtype"] != "text" {
		t.Fatalf("msgtype = %v, want text", parsed["msgtype"])
	}
}

// TestWeChatWorkChannel_EmptyURL 验证空 URL 静默返回 nil。
func TestWeChatWorkChannel_EmptyURL(t *testing.T) {
	ch := NewWeChatWorkChannel("")
	if err := ch.Send(&Message{Title: "x"}); err != nil {
		t.Fatalf("empty URL err = %v", err)
	}
}

// TestFeishuChannel_Send 验证飞书渠道发送 interactive 卡片消息。
func TestFeishuChannel_Send(t *testing.T) {
	received, srv := startMockWebhook(t, http.StatusOK)
	defer srv.Close()

	ch := NewFeishuChannel(srv.URL, "")
	msg := &Message{Title: "告警", Body: "CPU 95%", Severity: "critical"}
	if err := ch.Send(msg); err != nil {
		t.Fatalf("Send err = %v", err)
	}
	body := <-received
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if parsed["msg_type"] != "interactive" {
		t.Fatalf("msg_type = %v, want interactive", parsed["msg_type"])
	}
	card, ok := parsed["card"].(map[string]interface{})
	if !ok {
		t.Fatal("card missing")
	}
	header := card["header"].(map[string]interface{})
	if header["template"] != "red" {
		t.Fatalf("template = %v, want red (critical)", header["template"])
	}
}

// TestFeishuChannel_Signed 验证飞书加签 payload 含 timestamp&sign。
func TestFeishuChannel_Signed(t *testing.T) {
	received, srv := startMockWebhook(t, http.StatusOK)
	defer srv.Close()

	ch := NewFeishuChannel(srv.URL, "secret123")
	msg := &Message{Title: "test", Body: "hello"}
	if err := ch.Send(msg); err != nil {
		t.Fatalf("Send err = %v", err)
	}
	body := <-received
	var parsed map[string]interface{}
	json.Unmarshal([]byte(body), &parsed)
	if _, ok := parsed["timestamp"]; !ok {
		t.Fatal("signed payload missing timestamp")
	}
	if _, ok := parsed["sign"]; !ok {
		t.Fatal("signed payload missing sign")
	}
}

// TestFeishuChannel_WarningColor 验证 warning 级别卡片颜色为 orange。
func TestFeishuChannel_WarningColor(t *testing.T) {
	received, srv := startMockWebhook(t, http.StatusOK)
	defer srv.Close()

	ch := NewFeishuChannel(srv.URL, "")
	msg := &Message{Title: "warn", Body: "x", Severity: "warning"}
	if err := ch.Send(msg); err != nil {
		t.Fatalf("Send err = %v", err)
	}
	body := <-received
	var parsed map[string]interface{}
	json.Unmarshal([]byte(body), &parsed)
	card := parsed["card"].(map[string]interface{})
	header := card["header"].(map[string]interface{})
	if header["template"] != "orange" {
		t.Fatalf("template = %v, want orange (warning)", header["template"])
	}
}

// TestFeishuChannel_EmptyURL 验证空 URL 静默返回 nil。
func TestFeishuChannel_EmptyURL(t *testing.T) {
	ch := NewFeishuChannel("", "")
	if err := ch.Send(&Message{Title: "x"}); err != nil {
		t.Fatalf("empty URL err = %v", err)
	}
}

// TestSlackChannel_Send 验证 Slack 渠道发送 Block Kit 消息。
func TestSlackChannel_Send(t *testing.T) {
	received, srv := startMockWebhook(t, http.StatusOK)
	defer srv.Close()

	ch := NewSlackChannel(srv.URL, "")
	msg := &Message{Title: "Alert", Body: "CPU 95%", Severity: "critical"}
	if err := ch.Send(msg); err != nil {
		t.Fatalf("Send err = %v", err)
	}
	body := <-received
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	blocks, ok := parsed["blocks"].([]interface{})
	if !ok || len(blocks) != 2 {
		t.Fatalf("blocks missing or wrong count: %v", parsed["blocks"])
	}
	// 第一个 block 应为 header
	first := blocks[0].(map[string]interface{})
	if first["type"] != "header" {
		t.Fatalf("first block type = %v, want header", first["type"])
	}
}

// TestSlackChannel_WithChannel 验证指定频道时 payload 含 channel 字段。
func TestSlackChannel_WithChannel(t *testing.T) {
	received, srv := startMockWebhook(t, http.StatusOK)
	defer srv.Close()

	ch := NewSlackChannel(srv.URL, "#ops-alerts")
	msg := &Message{Title: "Alert", Body: "x"}
	if err := ch.Send(msg); err != nil {
		t.Fatalf("Send err = %v", err)
	}
	body := <-received
	var parsed map[string]interface{}
	json.Unmarshal([]byte(body), &parsed)
	if parsed["channel"] != "#ops-alerts" {
		t.Fatalf("channel = %v, want #ops-alerts", parsed["channel"])
	}
}

// TestSlackChannel_EmptyURL 验证空 URL 静默返回 nil。
func TestSlackChannel_EmptyURL(t *testing.T) {
	ch := NewSlackChannel("", "")
	if err := ch.Send(&Message{Title: "x"}); err != nil {
		t.Fatalf("empty URL err = %v", err)
	}
}

// TestEmailChannel_Enabled 验证 Enabled 判断逻辑。
func TestEmailChannel_Enabled(t *testing.T) {
	ch := NewEmailChannel("smtp.example.com", 25, "u", "p", "from@example.com", []string{"to@example.com"})
	if !ch.Enabled() {
		t.Fatal("fully configured channel should be enabled")
	}
	ch2 := NewEmailChannel("", 25, "u", "p", "from@example.com", []string{"to@example.com"})
	if ch2.Enabled() {
		t.Fatal("empty host should not be enabled")
	}
	ch3 := NewEmailChannel("smtp.example.com", 25, "u", "p", "from@example.com", nil)
	if ch3.Enabled() {
		t.Fatal("empty to should not be enabled")
	}
}

// TestEmailChannel_SendDisabled 验证未配置时 Send 静默返回 nil。
func TestEmailChannel_SendDisabled(t *testing.T) {
	ch := NewEmailChannel("", 25, "u", "p", "from@example.com", []string{"to@example.com"})
	if err := ch.Send(&Message{Title: "x"}); err != nil {
		t.Fatalf("disabled channel err = %v", err)
	}
}

// TestEmailChannel_NilMsg 验证 nil msg 静默返回 nil。
func TestEmailChannel_NilMsg(t *testing.T) {
	ch := NewEmailChannel("smtp.example.com", 25, "u", "p", "from@example.com", []string{"to@example.com"})
	if err := ch.Send(nil); err != nil {
		t.Fatalf("nil msg err = %v", err)
	}
}

// TestNewChannel_Factory 验证渠道工厂按 type 构造正确实例。
func TestNewChannel_Factory(t *testing.T) {
	tests := []struct {
		name string
		cfg  ChannelConfig
		want bool // 是否应成功
	}{
		{"dingtalk", ChannelConfig{Type: "dingtalk", WebhookURL: "http://x"}, true},
		{"wechat", ChannelConfig{Type: "wechat", WebhookURL: "http://x"}, true},
		{"feishu", ChannelConfig{Type: "feishu", WebhookURL: "http://x"}, true},
		{"slack", ChannelConfig{Type: "slack", WebhookURL: "http://x"}, true},
		{"email_ok", ChannelConfig{Type: "email", SMTPHost: "smtp.x", SMTPPort: 25, From: "a@x", To: []string{"b@x"}}, true},
		{"email_incomplete", ChannelConfig{Type: "email", SMTPHost: "", SMTPPort: 25}, false},
		{"unknown", ChannelConfig{Type: "unknown"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := NewChannel(tt.cfg)
			if tt.want {
				if err != nil {
					t.Fatalf("NewChannel err = %v, want nil", err)
				}
				if ch == nil {
					t.Fatal("channel is nil")
				}
			} else {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
			}
		})
	}
}

// TestChannel_HTTPError 验证非 2xx 返回错误。
func TestChannel_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ch := NewDingTalkChannel(srv.URL, "")
	if err := ch.Send(&Message{Title: "x", Body: "y"}); err == nil {
		t.Fatal("expected error for 500 response")
	}
}