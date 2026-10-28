package approval

import (
	"testing"
	"time"
)

func TestHistoryAppendKeepsAscendingOrder(t *testing.T) {
	base := testTime()
	h := &History{RequestID: "r1"}
	h.Append(HistoryEntry{Timestamp: base.Add(2 * time.Second), Action: "c"})
	h.Append(HistoryEntry{Timestamp: base, Action: "a"})
	h.Append(HistoryEntry{Timestamp: base.Add(time.Second), Action: "b"})

	want := []string{"a", "b", "c"}
	if len(h.Timeline) != len(want) {
		t.Fatalf("Timeline 长度 = %d，期望 %d", len(h.Timeline), len(want))
	}
	for i, w := range want {
		if h.Timeline[i].Action != w {
			t.Errorf("Timeline[%d] = %q，期望 %q（Append 必须保持时间升序）", i, h.Timeline[i].Action, w)
		}
	}
}

func TestHistoryAppendFillsZeroTimestamp(t *testing.T) {
	h := &History{}
	h.Append(HistoryEntry{Action: "a"})
	if h.Timeline[0].Timestamp.IsZero() {
		t.Error("零值 Timestamp 应被填为当前时间")
	}
}

func TestHistoryLast(t *testing.T) {
	h := &History{}
	if _, ok := h.Last(); ok {
		t.Error("空历史 Last() 应返回 false")
	}

	base := testTime()
	h.Append(HistoryEntry{Timestamp: base, Action: "a"})
	h.Append(HistoryEntry{Timestamp: base.Add(time.Second), Action: "b"})
	last, ok := h.Last()
	if !ok || last.Action != "b" {
		t.Errorf("Last() = (%+v, %v)，期望 b/true", last, ok)
	}
}

func TestHistoryFilterByAction(t *testing.T) {
	base := testTime()
	h := &History{}
	h.Append(HistoryEntry{Timestamp: base, Action: HistoryApprove, UserID: "u1"})
	h.Append(HistoryEntry{Timestamp: base.Add(time.Second), Action: HistoryReject, UserID: "u2"})
	h.Append(HistoryEntry{Timestamp: base.Add(2 * time.Second), Action: HistoryApprove, UserID: "u3"})

	got := h.FilterByAction(HistoryApprove)
	if len(got) != 2 {
		t.Fatalf("FilterByAction(approve) 数量 = %d，期望 2", len(got))
	}
	if got[0].UserID != "u1" || got[1].UserID != "u3" {
		t.Errorf("FilterByAction 未保持原顺序：%+v", got)
	}
	if n := len(h.FilterByAction(HistoryTimeout)); n != 0 {
		t.Errorf("无匹配动作应返回空集，得到 %d", n)
	}
}
