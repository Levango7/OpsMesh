// notify_extra_test.go 补充 notify.go 未覆盖的函数：AlertAggregator、DetectChannelByURL、
// PostWebhook、SendEmail、sanitizeEmailField、buildRFC822、Channels.Push、slackBlockKit、wecomMarkdown。
package notify

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"opsmesh/internal/proto"
)

// =============================================================================
// severityRank / aggregateKey 纯函数
// =============================================================================

func TestSeverityRank(t *testing.T) {
	cases := []struct {
		sev  string
		want int
	}{
		{"critical", 2}, {"warning", 1}, {"info", 0}, {"", 0},
	}
	for _, c := range cases {
		if got := severityRank(c.sev); got != c.want {
			t.Fatalf("severityRank(%q)=%d, want %d", c.sev, got, c.want)
		}
	}
}

func TestAggregateKey(t *testing.T) {
	a := &proto.Alert{Metric: "cpu", DeviceID: "dev1"}
	if got := aggregateKey(a); got != "cpu:dev1" {
		t.Fatalf("got=%q, want cpu:dev1", got)
	}
	// metric 为空回退到 message。
	a2 := &proto.Alert{Message: "task failed", DeviceID: "dev2"}
	if got := aggregateKey(a2); got != "task failed:dev2" {
		t.Fatalf("got=%q, want 'task failed:dev2'", got)
	}
}

// =============================================================================
// AlertAggregator
// =============================================================================

func TestAlertAggregator_FirstPush(t *testing.T) {
	ag := NewAlertAggregator()
	a := &proto.Alert{Metric: "cpu", DeviceID: "dev1", Severity: "warning"}
	now := time.Now()
	if !ag.Allow(a, now) {
		t.Fatal("首次推送应放行")
	}
}

func TestAlertAggregator_SuppressSameSeverity(t *testing.T) {
	ag := NewAlertAggregator()
	a := &proto.Alert{Metric: "cpu", DeviceID: "dev1", Severity: "warning"}
	now := time.Now()
	ag.Allow(a, now)
	// 窗口内同级别应抑制。
	if ag.Allow(a, now.Add(10*time.Second)) {
		t.Fatal("窗口内同级别应抑制")
	}
}

func TestAlertAggregator_UpgradeAllowed(t *testing.T) {
	ag := NewAlertAggregator()
	warn := &proto.Alert{Metric: "cpu", DeviceID: "dev1", Severity: "warning"}
	crit := &proto.Alert{Metric: "cpu", DeviceID: "dev1", Severity: "critical"}
	now := time.Now()
	ag.Allow(warn, now)
	// 窗口内更高级别应放行（升级告警）。
	if !ag.Allow(crit, now.Add(10*time.Second)) {
		t.Fatal("窗口内更高级别应放行")
	}
}

func TestAlertAggregator_AfterWindowExpires(t *testing.T) {
	ag := NewAlertAggregator()
	a := &proto.Alert{Metric: "cpu", DeviceID: "dev1", Severity: "warning"}
	now := time.Now()
	ag.Allow(a, now)
	// 超过聚合窗口后应放行。
	if !ag.Allow(a, now.Add(AggregateWindow+1*time.Second)) {
		t.Fatal("超过聚合窗口后应放行")
	}
}

func TestAlertAggregator_Cleanup(t *testing.T) {
	ag := NewAlertAggregator()
	a1 := &proto.Alert{Metric: "cpu", DeviceID: "dev1", Severity: "warning"}
	a2 := &proto.Alert{Metric: "mem", DeviceID: "dev2", Severity: "warning"}
	now := time.Now()
	ag.Allow(a1, now)
	ag.Allow(a2, now.Add(1*time.Second))
	// 清理 now 之前的条目（a1 被清理，a2 保留）。
	ag.Cleanup(now.Add(500 * time.Millisecond))
	// a1 已清理，应放行。
	if !ag.Allow(a1, now.Add(2*time.Second)) {
		t.Fatal("a1 已清理，应放行")
	}
	// a2 未清理，窗口内应抑制。
	if ag.Allow(a2, now.Add(2*time.Second)) {
		t.Fatal("a2 未清理，窗口内应抑制")
	}
}

// =============================================================================
// matchDomain / DetectChannelByURL
// =============================================================================

func TestMatchDomain(t *testing.T) {
	if !matchDomain("slack.com", "slack.com") {
		t.Fatal("exact match should pass")
	}
	if !matchDomain("hooks.slack.com", "slack.com") {
		t.Fatal("subdomain match should pass")
	}
	if matchDomain("evil-slack.com", "slack.com") {
		t.Fatal("evil-slack.com should not match slack.com")
	}
	if matchDomain("slack.com.attacker.com", "slack.com") {
		t.Fatal("slack.com.attacker.com should not match slack.com")
	}
}

func TestDetectChannelByURL(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"", ""},
		{"https://hooks.slack.com/services/x/y", "slack"},
		{"https://qyapi.weixin.qq.com/cgi-bin/webhook/send", "wecom"},
		{"http://example.com/webhook", ""},
		{"://bad", ""},
	}
	for _, c := range cases {
		if got := DetectChannelByURL(c.url); got != c.want {
			t.Fatalf("DetectChannelByURL(%q)=%q, want %q", c.url, got, c.want)
		}
	}
}

// =============================================================================
// slackBlockKit / wecomMarkdown
// =============================================================================

func TestSlackBlockKit(t *testing.T) {
	a := &proto.Alert{Severity: "critical", DeviceID: "dev1", AgentID: "ag1", Message: "boom", CreatedAt: time.Now()}
	m := slackBlockKit(a)
	if m["blocks"] == nil {
		t.Fatal("blocks is nil")
	}
}

func TestSlackBlockKit_Warning(t *testing.T) {
	a := &proto.Alert{Severity: "warning", DeviceID: "dev1", AgentID: "ag1", Message: "ok", CreatedAt: time.Now()}
	m := slackBlockKit(a)
	if m["blocks"] == nil {
		t.Fatal("blocks is nil")
	}
}

func TestWecomMarkdown(t *testing.T) {
	a := &proto.Alert{Severity: "critical", DeviceID: "dev1", AgentID: "ag1", Message: "boom", CreatedAt: time.Now()}
	m := wecomMarkdown(a)
	if m["msgtype"] != "markdown" {
		t.Fatalf("msgtype=%v, want markdown", m["msgtype"])
	}
}

// =============================================================================
// PostWebhook
// =============================================================================

func TestPostWebhook_EmptyURL(t *testing.T) {
	a := &proto.Alert{Severity: "critical"}
	if err := PostWebhook("generic", "", a); err != nil {
		t.Fatalf("empty URL should return nil: %v", err)
	}
}

func TestPostWebhook_NilAlert(t *testing.T) {
	if err := PostWebhook("generic", "http://example.com", nil); err != nil {
		t.Fatalf("nil alert should return nil: %v", err)
	}
}

func TestPostWebhook_Slack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	a := &proto.Alert{Severity: "critical", DeviceID: "dev1", AgentID: "ag1", Message: "boom", CreatedAt: time.Now()}
	if err := PostWebhook("generic", srv.URL, a); err != nil {
		t.Fatalf("slack webhook should succeed: %v", err)
	}
}

func TestPostWebhook_Wecom(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	a := &proto.Alert{Severity: "critical", DeviceID: "dev1", AgentID: "ag1", Message: "boom", CreatedAt: time.Now()}
	if err := PostWebhook("generic", srv.URL+"/qyapi.weixin.qq.com", a); err != nil {
		t.Fatalf("wecom webhook should succeed: %v", err)
	}
}

func TestPostWebhook_GenericFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), "dev1") {
			t.Fatalf("body should contain device ID: %s", string(b))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	a := &proto.Alert{Severity: "critical", DeviceID: "dev1", AgentID: "ag1", Message: "boom", CreatedAt: time.Now()}
	if err := PostWebhook("generic", srv.URL, a); err != nil {
		t.Fatalf("generic fallback should succeed: %v", err)
	}
}

func TestPostWebhook_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	a := &proto.Alert{Severity: "critical", DeviceID: "dev1", AgentID: "ag1", Message: "boom", CreatedAt: time.Now()}
	if err := PostWebhook("generic", srv.URL, a); err == nil {
		t.Fatal("HTTP 500 should return error")
	}
}

// =============================================================================
// sanitizeEmailField / buildRFC822
// =============================================================================

func TestSanitizeEmailField(t *testing.T) {
	if got := sanitizeEmailField("hello\r\nworld"); got != "helloworld" {
		t.Fatalf("got=%q, want 'helloworld'", got)
	}
	if got := sanitizeEmailField("clean"); got != "clean" {
		t.Fatalf("got=%q, want 'clean'", got)
	}
}

func TestBuildRFC822(t *testing.T) {
	msg := buildRFC822("from@example.com", []string{"to1@example.com", "to2@example.com"}, "Test Subject", "Body text")
	if !strings.Contains(msg, "From: from@example.com") {
		t.Fatal("missing From header")
	}
	if !strings.Contains(msg, "To: to1@example.com, to2@example.com") {
		t.Fatal("missing To header")
	}
	if !strings.Contains(msg, "Subject: Test Subject") {
		t.Fatal("missing Subject header")
	}
	if !strings.Contains(msg, "MIME-Version: 1.0") {
		t.Fatal("missing MIME-Version header")
	}
	if !strings.Contains(msg, "Body text") {
		t.Fatal("missing body")
	}
	// 邮件头注入防护：\r\n 被移除，Bcc 不会成为独立头行。
	msg2 := buildRFC822("from@example.com", []string{"to@example.com"}, "Subject\r\nBcc: evil@x.com", "Body")
	if strings.Contains(msg2, "\r\nBcc:") {
		t.Fatal("email header injection not sanitized")
	}
}

// =============================================================================
// EmailConfig.Enabled
// =============================================================================

func TestEmailConfig_Enabled(t *testing.T) {
	if (&EmailConfig{}).Enabled() {
		t.Fatal("empty config should not be enabled")
	}
	c := &EmailConfig{Host: "smtp.example.com", Port: 25, From: "from@example.com", To: "to@example.com"}
	if !c.Enabled() {
		t.Fatal("fully configured should be enabled")
	}
	c.Port = 0
	if c.Enabled() {
		t.Fatal("port=0 should not be enabled")
	}
}

// =============================================================================
// SendEmail
// =============================================================================

func TestSendEmail_Disabled(t *testing.T) {
	a := &proto.Alert{Severity: "critical"}
	if err := SendEmail(&EmailConfig{}, a); err != nil {
		t.Fatalf("disabled config should return nil: %v", err)
	}
}

func TestSendEmail_NilAlert(t *testing.T) {
	c := &EmailConfig{Host: "smtp.example.com", Port: 25, From: "from@example.com", To: "to@example.com"}
	if err := SendEmail(c, nil); err != nil {
		t.Fatalf("nil alert should return nil: %v", err)
	}
}

func TestSendEmail_ConnectionFailed(t *testing.T) {
	c := &EmailConfig{Host: "127.0.0.1", Port: 1, From: "from@example.com", To: "to@example.com"}
	a := &proto.Alert{Severity: "critical", DeviceID: "dev1", Message: "boom", CreatedAt: time.Now()}
	// 端口 1 不可用，应返回错误（连接失败或超时）。
	if err := SendEmail(c, a); err == nil {
		t.Fatal("unreachable SMTP should return error")
	}
}

// =============================================================================
// Channels.Push
// =============================================================================

func TestChannels_Push_NilAlert(t *testing.T) {
	c := &Channels{WebhookURL: "http://example.com"}
	if err := c.Push(nil); err != nil {
		t.Fatalf("nil alert should return nil: %v", err)
	}
}

func TestChannels_Push_NoChannels(t *testing.T) {
	c := &Channels{}
	a := &proto.Alert{Severity: "critical"}
	if err := c.Push(a); err != nil {
		t.Fatalf("no channels should return nil: %v", err)
	}
}

func TestChannels_Push_WebhookOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := &Channels{NotifierType: "generic", WebhookURL: srv.URL}
	a := &proto.Alert{Severity: "critical", DeviceID: "dev1", CreatedAt: time.Now()}
	if err := c.Push(a); err != nil {
		t.Fatalf("webhook push should succeed: %v", err)
	}
}

func TestChannels_Push_WebhookError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := &Channels{NotifierType: "generic", WebhookURL: srv.URL}
	a := &proto.Alert{Severity: "critical", DeviceID: "dev1", CreatedAt: time.Now()}
	if err := c.Push(a); err == nil {
		t.Fatal("webhook error should return error")
	}
}

func TestChannels_Push_EmailOnly(t *testing.T) {
	c := &Channels{
		Email: &EmailConfig{Host: "127.0.0.1", Port: 1, From: "from@example.com", To: "to@example.com"},
	}
	a := &proto.Alert{Severity: "critical", DeviceID: "dev1", Message: "boom", CreatedAt: time.Now()}
	// SMTP 不可用，应返回错误。
	if err := c.Push(a); err == nil {
		t.Fatal("email error should return error")
	}
}