package notify

import (
	"testing"
	"time"
)

// TestDeduplicator_IsDuplicate_SameMessage 验证相同消息在 TTL 内去重。
func TestDeduplicator_IsDuplicate_SameMessage(t *testing.T) {
	d := NewDeduplicator(5 * time.Minute)
	msg := &Message{Title: "alert", Body: "CPU 95%", Severity: "critical", Source: "dev-1"}

	if d.IsDuplicate(msg) {
		t.Fatal("first send should not be duplicate")
	}
	if !d.IsDuplicate(msg) {
		t.Fatal("second send should be duplicate (within TTL)")
	}
	if !d.IsDuplicate(msg) {
		t.Fatal("third send should still be duplicate (within TTL)")
	}
}

// TestDeduplicator_IsDuplicate_DifferentMessage 验证不同消息不去重。
func TestDeduplicator_IsDuplicate_DifferentMessage(t *testing.T) {
	d := NewDeduplicator(5 * time.Minute)
	msg1 := &Message{Title: "alert1", Body: "CPU 95%"}
	msg2 := &Message{Title: "alert2", Body: "Mem 90%"}

	if d.IsDuplicate(msg1) {
		t.Fatal("msg1 first send should not be duplicate")
	}
	if d.IsDuplicate(msg2) {
		t.Fatal("msg2 first send should not be duplicate (different message)")
	}
}

// TestDeduplicator_IsDuplicate_TTLExpiry 验证 TTL 过期后不去重。
func TestDeduplicator_IsDuplicate_TTLExpiry(t *testing.T) {
	d := NewDeduplicator(50 * time.Millisecond)
	msg := &Message{Title: "alert", Body: "CPU 95%"}

	if d.IsDuplicate(msg) {
		t.Fatal("first send should not be duplicate")
	}
	if !d.IsDuplicate(msg) {
		t.Fatal("second send should be duplicate (within TTL)")
	}
	// 等待 TTL 过期
	time.Sleep(60 * time.Millisecond)
	if d.IsDuplicate(msg) {
		t.Fatal("third send after TTL expiry should not be duplicate")
	}
}

// TestDeduplicator_IsDuplicate_NilMsg 验证 nil msg 不视为重复。
func TestDeduplicator_IsDuplicate_NilMsg(t *testing.T) {
	d := NewDeduplicator(5 * time.Minute)
	if d.IsDuplicate(nil) {
		t.Fatal("nil msg should not be duplicate")
	}
}

// TestDeduplicator_IsDuplicate_TimestampNotInHash 验证 Timestamp 不参与指纹计算。
// 同一告警不同时间戳应被视为重复（避免时间戳绕过去重）。
func TestDeduplicator_IsDuplicate_TimestampNotInHash(t *testing.T) {
	d := NewDeduplicator(5 * time.Minute)
	base := &Message{Title: "alert", Body: "CPU 95%", Severity: "critical", Source: "dev-1"}
	msg1 := *base
	msg1.Timestamp = time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	msg2 := *base
	msg2.Timestamp = time.Date(2026, 8, 11, 13, 0, 0, 0, time.UTC) // 不同时间戳

	if d.IsDuplicate(&msg1) {
		t.Fatal("first send should not be duplicate")
	}
	if !d.IsDuplicate(&msg2) {
		t.Fatal("different timestamp but same content should be duplicate")
	}
}

// TestDeduplicator_IsDuplicateStrict 验证固定窗口语义（重复不续期）。
func TestDeduplicator_IsDuplicateStrict(t *testing.T) {
	d := NewDeduplicator(50 * time.Millisecond)
	msg := &Message{Title: "alert", Body: "CPU 95%"}

	if d.IsDuplicateStrict(msg) {
		t.Fatal("first send should not be duplicate")
	}
	if !d.IsDuplicateStrict(msg) {
		t.Fatal("second send should be duplicate")
	}
	// 固定窗口：即使重复也不续期，TTL 过期后放行
	time.Sleep(60 * time.Millisecond)
	if d.IsDuplicateStrict(msg) {
		t.Fatal("after TTL expiry should not be duplicate (fixed window)")
	}
}

// TestDeduplicator_Cleanup 验证清理过期条目。
func TestDeduplicator_Cleanup(t *testing.T) {
	d := NewDeduplicator(20 * time.Millisecond)
	msg1 := &Message{Title: "a"}
	msg2 := &Message{Title: "b"}

	d.IsDuplicate(msg1)
	d.IsDuplicate(msg2)
	if d.Size() != 2 {
		t.Fatalf("Size = %d, want 2", d.Size())
	}

	time.Sleep(30 * time.Millisecond)
	removed := d.Cleanup()
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
	if d.Size() != 0 {
		t.Fatalf("Size after cleanup = %d, want 0", d.Size())
	}
}

// TestDeduplicator_Cleanup_PartialExpiry 验证部分过期清理。
func TestDeduplicator_Cleanup_PartialExpiry(t *testing.T) {
	d := NewDeduplicator(40 * time.Millisecond)
	msg1 := &Message{Title: "a"}
	d.IsDuplicate(msg1)

	time.Sleep(30 * time.Millisecond)
	msg2 := &Message{Title: "b"}
	d.IsDuplicate(msg2) // msg2 比 msg1 晚 30ms 记录

	time.Sleep(20 * time.Millisecond) // msg1 已过期（50ms > 40ms），msg2 未过期（20ms < 40ms）
	removed := d.Cleanup()
	if removed != 1 {
		t.Fatalf("removed = %d, want 1 (only msg1 expired)", removed)
	}
	if d.Size() != 1 {
		t.Fatalf("Size = %d, want 1 (msg2 still valid)", d.Size())
	}
}

// TestDeduplicator_Reset 验证清空所有缓存。
func TestDeduplicator_Reset(t *testing.T) {
	d := NewDeduplicator(5 * time.Minute)
	d.IsDuplicate(&Message{Title: "a"})
	d.IsDuplicate(&Message{Title: "b"})
	if d.Size() != 2 {
		t.Fatalf("Size = %d, want 2", d.Size())
	}
	d.Reset()
	if d.Size() != 0 {
		t.Fatalf("Size after reset = %d, want 0", d.Size())
	}
}

// TestDeduplicator_DefaultTTL 验证 ttl<=0 时使用默认 5 分钟。
func TestDeduplicator_DefaultTTL(t *testing.T) {
	d := NewDeduplicator(0)
	if d.ttl != 5*time.Minute {
		t.Fatalf("ttl = %v, want 5m (default)", d.ttl)
	}
	d2 := NewDeduplicator(-1)
	if d2.ttl != 5*time.Minute {
		t.Fatalf("ttl = %v, want 5m (default)", d2.ttl)
	}
}

// TestDeduplicator_SlidingWindow 验证滑动窗口语义（重复续期）。
func TestDeduplicator_SlidingWindow(t *testing.T) {
	d := NewDeduplicator(100 * time.Millisecond)
	msg := &Message{Title: "alert", Body: "x"}

	d.IsDuplicate(msg) // 首次记录，expiry = now + 100ms
	// 在 80ms 时重复（应续期到 80ms + 100ms = 180ms）
	time.Sleep(80 * time.Millisecond)
	if !d.IsDuplicate(msg) {
		t.Fatal("at 80ms should be duplicate (within 100ms TTL)")
	}
	// 在 120ms 时，固定窗口下应已过期（120 > 100），但滑动窗口下未过期（120 < 180）
	time.Sleep(40 * time.Millisecond)
	if !d.IsDuplicate(msg) {
		t.Fatal("at 120ms should still be duplicate (sliding window renewed to 180ms)")
	}
}
