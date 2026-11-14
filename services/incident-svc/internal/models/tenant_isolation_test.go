package models

import "testing"

// TestMemoryStoreTenantIsolation 是 D4（issue #27 裁决②）的存储层钉子：
// 跨租户读/列/改/删/时间线一律不可见、不可写；同租户一切正常；空租户拒绝写入；
// 跨租户同 ID 不得覆盖已有单。任一条被改弱都会在这里红。
func TestMemoryStoreTenantIsolation(t *testing.T) {
	store := NewMemoryStore()

	// 空租户拒绝写入（无主数据不落库）
	if got := store.CreateIncident(&Incident{ID: "no-tenant"}); got != nil {
		t.Fatal("无租户写入应被拒绝")
	}

	a := store.CreateIncident(&Incident{TenantID: "A", ID: "inc-1", Status: StatusDetected})
	if a == nil {
		t.Fatal("A 建单失败")
	}
	if ev := store.AddTimelineEvent(&TimelineEvent{ID: "ev-a", IncidentID: "inc-1"}, "A"); ev == nil {
		t.Fatal("A 写时间线失败")
	}

	// B 的视角：全部不可见 / 不可写
	if got := store.GetIncident("inc-1", "B"); got != nil {
		t.Fatalf("B 读到了 A 的单: %+v", got)
	}
	if list := store.ListIncidents("", "", "B"); len(list) != 0 {
		t.Fatalf("B 列表看到 %d 条（应 0）", len(list))
	}
	if got := store.UpdateIncident(&Incident{TenantID: "B", ID: "inc-1", Title: "hijack"}, "B"); got != nil {
		t.Fatal("B 改到了 A 的单")
	}
	if store.DeleteIncident("inc-1", "B") {
		t.Fatal("B 删掉了 A 的单")
	}
	if tl := store.GetTimeline("inc-1", "B"); len(tl) != 0 {
		t.Fatalf("B 看到了 A 的时间线: %+v", tl)
	}
	if ev := store.AddTimelineEvent(&TimelineEvent{ID: "ev-b", IncidentID: "inc-1"}, "B"); ev != nil {
		t.Fatal("B 往 A 的单里写了时间线")
	}

	// A 自己一切正常
	if got := store.GetIncident("inc-1", "A"); got == nil || got.TenantID != "A" {
		t.Fatal("A 自己的单读不到 / 租户错")
	}
	if tl := store.GetTimeline("inc-1", "A"); len(tl) != 1 {
		t.Fatalf("A 的时间线应为 1 条, got %d", len(tl))
	}

	// 跨租户同 ID 建单：不覆盖（A 的单不受影响）
	if dup := store.CreateIncident(&Incident{TenantID: "B", ID: "inc-1", Title: "B 的伪单"}); dup != nil {
		t.Fatal("跨租户同 ID 建单不应成功")
	}
	if got := store.GetIncident("inc-1", "A"); got == nil || got.Title != "" {
		t.Fatalf("A 的单被跨租户 ID 覆盖: %+v", got)
	}
}
