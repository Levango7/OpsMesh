package notify

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"opsmesh/internal/proto"
)

// TestNotifier_NoChannels 验证无渠道时静默返回 nil。
func TestNotifier_NoChannels(t *testing.T) {
	n := NewNotifier()
	if err := n.Notify(&Message{Title: "x"}); err != nil {
		t.Fatalf("Notify err = %v", err)
	}
}

// TestNotifier_NilMsg 验证 nil msg 静默返回 nil。
func TestNotifier_NilMsg(t *testing.T) {
	n := NewNotifier(WithChannels(&mockChannel{}))
	if err := n.Notify(nil); err != nil {
		t.Fatalf("Notify(nil) err = %v", err)
	}
}

// TestNotifier_MultiChannel 验证多渠道推送。
func TestNotifier_MultiChannel(t *testing.T) {
	ch1 := &mockChannel{failCount: 0}
	ch2 := &mockChannel{failCount: 0}
	n := NewNotifier(WithChannels(ch1, ch2))

	if err := n.Notify(&Message{Title: "x"}); err != nil {
		t.Fatalf("Notify err = %v", err)
	}
	if atomic.LoadInt32(&ch1.calls) != 1 {
		t.Fatalf("ch1 calls = %d, want 1", ch1.calls)
	}
	if atomic.LoadInt32(&ch2.calls) != 1 {
		t.Fatalf("ch2 calls = %d, want 1", ch2.calls)
	}
}

// TestNotifier_PartialFailure 验证部分渠道失败不影响其他渠道。
func TestNotifier_PartialFailure(t *testing.T) {
	ch1 := &mockChannel{failCount: -1} // 永远失败
	ch2 := &mockChannel{failCount: 0}  // 成功
	n := NewNotifier(WithChannels(ch1, ch2))

	err := n.Notify(&Message{Title: "x"})
	if err == nil {
		t.Fatal("expected error from failed channel")
	}
	if !strings.Contains(err.Error(), "channel[0]") {
		t.Fatalf("error should mention channel[0]: %v", err)
	}
	// ch2 应该仍然被调用
	if atomic.LoadInt32(&ch2.calls) != 1 {
		t.Fatalf("ch2 calls = %d, want 1 (should still be called)", ch2.calls)
	}
}

// TestNotifier_Dedup 验证去重：相同消息第二次发送被跳过。
func TestNotifier_Dedup(t *testing.T) {
	ch := &mockChannel{failCount: 0}
	n := NewNotifier(WithChannels(ch), WithDedup(5*time.Minute))
	msg := &Message{Title: "alert", Body: "CPU 95%"}

	if err := n.Notify(msg); err != nil {
		t.Fatalf("first Notify err = %v", err)
	}
	if err := n.Notify(msg); err != nil {
		t.Fatalf("second Notify err = %v", err)
	}
	// 去重应导致第二次不发送
	if atomic.LoadInt32(&ch.calls) != 1 {
		t.Fatalf("ch calls = %d, want 1 (second deduped)", ch.calls)
	}
}

// TestNotifier_DedupDisabled 验证未启用去重时重复消息正常发送。
func TestNotifier_DedupDisabled(t *testing.T) {
	ch := &mockChannel{failCount: 0}
	n := NewNotifier(WithChannels(ch)) // 无 WithDedup
	msg := &Message{Title: "alert", Body: "CPU 95%"}

	n.Notify(msg)
	n.Notify(msg)
	if atomic.LoadInt32(&ch.calls) != 2 {
		t.Fatalf("ch calls = %d, want 2 (no dedup)", ch.calls)
	}
}

// TestNotifier_Retry 验证重试：失败后重试成功。
func TestNotifier_Retry(t *testing.T) {
	ch := &mockChannel{failCount: 1} // 第 1 次失败，第 2 次成功
	policy := &RetryPolicy{MaxAttempts: 3, Interval: 1 * time.Millisecond, Backoff: 2.0}
	n := NewNotifier(WithChannels(ch), WithRetry(policy))

	if err := n.Notify(&Message{Title: "x"}); err != nil {
		t.Fatalf("Notify err = %v", err)
	}
	if atomic.LoadInt32(&ch.calls) != 2 {
		t.Fatalf("ch calls = %d, want 2 (1 failure + 1 retry success)", ch.calls)
	}
}

// TestNotifier_RetryAllFail 验证重试全部失败返回错误。
func TestNotifier_RetryAllFail(t *testing.T) {
	ch := &mockChannel{failCount: -1} // 永远失败
	policy := &RetryPolicy{MaxAttempts: 2, Interval: 1 * time.Millisecond, Backoff: 2.0}
	n := NewNotifier(WithChannels(ch), WithRetry(policy))

	err := n.Notify(&Message{Title: "x"})
	if err == nil {
		t.Fatal("expected error after all retries failed")
	}
	if atomic.LoadInt32(&ch.calls) != 2 {
		t.Fatalf("ch calls = %d, want 2 (max attempts)", ch.calls)
	}
}

// TestNotifier_AddChannel 验证运行期追加渠道。
func TestNotifier_AddChannel(t *testing.T) {
	n := NewNotifier()
	ch := &mockChannel{failCount: 0}
	n.AddChannel(ch)

	if err := n.Notify(&Message{Title: "x"}); err != nil {
		t.Fatalf("Notify err = %v", err)
	}
	if atomic.LoadInt32(&ch.calls) != 1 {
		t.Fatalf("ch calls = %d, want 1", ch.calls)
	}
}

// TestNotifier_AddNilChannel 验证追加 nil 渠道静默跳过。
func TestNotifier_AddNilChannel(t *testing.T) {
	n := NewNotifier()
	n.AddChannel(nil) // 不应 panic
	if len(n.Channels()) != 0 {
		t.Fatalf("Channels len = %d, want 0", len(n.Channels()))
	}
}

// TestNotifier_NotifyWithTemplate 验证模板渲染后推送。
func TestNotifier_NotifyWithTemplate(t *testing.T) {
	ch := &mockChannel{failCount: 0}
	store := NewTemplateStore()
	store.Add(&Template{ID: "t1", Title: "[{{.Level}}]", Body: "{{.Msg}}", Format: "markdown"})
	n := NewNotifier(WithChannels(ch), WithTemplates(store))

	err := n.NotifyWithTemplate("t1", map[string]interface{}{"Level": "warn", "Msg": "hello"})
	if err != nil {
		t.Fatalf("NotifyWithTemplate err = %v", err)
	}
	if atomic.LoadInt32(&ch.calls) != 1 {
		t.Fatalf("ch calls = %d, want 1", ch.calls)
	}
}

// TestNotifier_NotifyWithTemplate_NotFound 验证模板不存在返回 error。
func TestNotifier_NotifyWithTemplate_NotFound(t *testing.T) {
	store := NewTemplateStore()
	n := NewNotifier(WithTemplates(store))

	err := n.NotifyWithTemplate("nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for nonexistent template")
	}
}

// TestNotifier_NotifyWithTemplate_NoStore 验证未配置模板存储返回 error。
func TestNotifier_NotifyWithTemplate_NoStore(t *testing.T) {
	n := NewNotifier()
	err := n.NotifyWithTemplate("any", nil)
	if err == nil {
		t.Fatal("expected error when no template store configured")
	}
}

// TestNotifier_CleanupDedup 验证去重清理。
func TestNotifier_CleanupDedup(t *testing.T) {
	n := NewNotifier(WithDedup(20 * time.Millisecond))
	n.Notify(&Message{Title: "a"})
	n.Notify(&Message{Title: "b"})

	time.Sleep(30 * time.Millisecond)
	removed := n.CleanupDedup()
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
}

// TestNotifier_CleanupDedup_Disabled 验证未启用去重时 CleanupDedup 返回 0。
func TestNotifier_CleanupDedup_Disabled(t *testing.T) {
	n := NewNotifier()
	if removed := n.CleanupDedup(); removed != 0 {
		t.Fatalf("removed = %d, want 0 (dedup disabled)", removed)
	}
}

// TestAlertToMessage 验证 Alert 转 Message。
func TestAlertToMessage(t *testing.T) {
	a := &proto.Alert{
		AlertID:   "a1",
		Severity:  "critical",
		DeviceID:  "dev-1",
		AgentID:   "agent-1",
		Message:   "CPU 95%",
		CreatedAt: time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC),
	}
	msg := AlertToMessage(a)
	if msg == nil {
		t.Fatal("AlertToMessage returned nil")
	}
	if !strings.Contains(msg.Title, "critical") {
		t.Fatalf("Title missing severity: %s", msg.Title)
	}
	if !strings.Contains(msg.Title, "dev-1") {
		t.Fatalf("Title missing device: %s", msg.Title)
	}
	if !strings.Contains(msg.Body, "CPU 95%") {
		t.Fatalf("Body missing message: %s", msg.Body)
	}
	if msg.Severity != "critical" {
		t.Fatalf("Severity = %q, want critical", msg.Severity)
	}
	if msg.Source != "dev-1" {
		t.Fatalf("Source = %q, want dev-1", msg.Source)
	}
	if msg.Format != "markdown" {
		t.Fatalf("Format = %q, want markdown", msg.Format)
	}
}

// TestAlertToMessage_Nil 验证 nil alert 返回 nil。
func TestAlertToMessage_Nil(t *testing.T) {
	if msg := AlertToMessage(nil); msg != nil {
		t.Fatal("AlertToMessage(nil) should return nil")
	}
}

// TestWithRetry_NilPolicy 验证 WithRetry(nil) 使用默认策略。
func TestWithRetry_NilPolicy(t *testing.T) {
	n := NewNotifier(WithRetry(nil))
	if n.retry == nil {
		t.Fatal("retry should not be nil after WithRetry(nil)")
	}
	if n.retry.MaxAttempts != 3 {
		t.Fatalf("MaxAttempts = %d, want 3 (default)", n.retry.MaxAttempts)
	}
}

// TestNotifier_ChannelsAndTemplates 验证访问器方法。
func TestNotifier_ChannelsAndTemplates(t *testing.T) {
	ch := &mockChannel{}
	store := NewTemplateStore()
	n := NewNotifier(WithChannels(ch), WithTemplates(store))

	if len(n.Channels()) != 1 {
		t.Fatalf("Channels len = %d, want 1", len(n.Channels()))
	}
	if n.Templates() != store {
		t.Fatal("Templates() should return the store")
	}
}
