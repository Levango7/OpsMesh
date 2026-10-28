package models

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestStatusAndSeverityLiteralValues(t *testing.T) {
	statuses := map[string]IncidentStatus{
		"detected":      StatusDetected,
		"investigating": StatusInvestigating,
		"mitigating":    StatusMitigating,
		"resolved":      StatusResolved,
		"closed":        StatusClosed,
	}
	for want, got := range statuses {
		if string(got) != want {
			t.Fatalf("IncidentStatus 字面值 = %q, want %q（API/持久化契约）", got, want)
		}
	}

	severities := map[string]Severity{
		"critical": SeverityCritical,
		"high":     SeverityHigh,
		"medium":   SeverityMedium,
		"low":      SeverityLow,
		"info":     SeverityInfo,
	}
	for want, got := range severities {
		if string(got) != want {
			t.Fatalf("Severity 字面值 = %q, want %q（API/持久化契约）", got, want)
		}
	}
}

func TestMemoryStoreCreateAndGet(t *testing.T) {
	store := NewMemoryStore()
	if got := store.CreateIncident(nil); got != nil {
		t.Fatalf("CreateIncident(nil) = %v, want nil", got)
	}

	before := time.Now()
	inc := &Incident{ID: "inc-1", Title: "db down", Status: StatusDetected, Severity: SeverityCritical}
	created := store.CreateIncident(inc)
	if created == nil {
		t.Fatal("CreateIncident 返回 nil")
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatal("CreateIncident 未补时间戳")
	}
	if created.UpdatedAt.Before(created.CreatedAt) {
		t.Fatalf("UpdatedAt(%v) 早于 CreatedAt(%v)", created.UpdatedAt, created.CreatedAt)
	}
	if created.CreatedAt.Before(before.Add(-time.Second)) {
		t.Fatalf("CreatedAt 明显偏离当前时间: %v", created.CreatedAt)
	}

	got := store.GetIncident("inc-1")
	if got == nil || got.ID != "inc-1" || got.Title != "db down" {
		t.Fatalf("GetIncident = %+v", got)
	}
	if store.GetIncident("missing") != nil {
		t.Fatal("缺失 ID 应返回 nil")
	}

	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	kept := store.CreateIncident(&Incident{ID: "inc-2", CreatedAt: fixed, UpdatedAt: fixed})
	if !kept.CreatedAt.Equal(fixed) || !kept.UpdatedAt.Equal(fixed) {
		t.Fatalf("已有时间戳被覆盖: %v/%v", kept.CreatedAt, kept.UpdatedAt)
	}
}

func TestMemoryStoreUpdate(t *testing.T) {
	store := NewMemoryStore()
	if got := store.UpdateIncident(nil); got != nil {
		t.Fatalf("UpdateIncident(nil) = %v", got)
	}
	if got := store.UpdateIncident(&Incident{ID: "missing"}); got != nil {
		t.Fatalf("更新不存在的 ID 应返回 nil, got %v", got)
	}

	oldUpdated := time.Unix(0, 0)
	store.CreateIncident(&Incident{ID: "inc-1", Title: "old", UpdatedAt: oldUpdated})
	updated := store.UpdateIncident(&Incident{ID: "inc-1", Title: "new", Status: StatusResolved})
	if updated == nil || updated.Title != "new" || updated.Status != StatusResolved {
		t.Fatalf("UpdateIncident = %+v", updated)
	}
	if !updated.UpdatedAt.After(oldUpdated) {
		t.Fatalf("UpdatedAt 未刷新: %v", updated.UpdatedAt)
	}
	if got := store.GetIncident("inc-1"); got == nil || got.Title != "new" {
		t.Fatalf("更新未落库: %+v", got)
	}
}

func TestMemoryStoreDeleteAlsoDropsTimeline(t *testing.T) {
	store := NewMemoryStore()
	store.CreateIncident(&Incident{ID: "inc-1"})
	store.AddTimelineEvent(&TimelineEvent{ID: "ev-1", IncidentID: "inc-1"})

	if !store.DeleteIncident("inc-1") {
		t.Fatal("删除已存在事件应返回 true")
	}
	if store.DeleteIncident("inc-1") {
		t.Fatal("重复删除应返回 false")
	}
	if got := store.GetIncident("inc-1"); got != nil {
		t.Fatalf("删除后仍可取到: %+v", got)
	}
	if got := store.GetTimeline("inc-1"); len(got) != 0 {
		t.Fatalf("删除事件后时间线未清理: %v", got)
	}
}

func TestMemoryStoreListFilters(t *testing.T) {
	store := NewMemoryStore()
	for _, inc := range []*Incident{
		{ID: "i1", Status: StatusDetected, Severity: SeverityCritical},
		{ID: "i2", Status: StatusDetected, Severity: SeverityHigh},
		{ID: "i3", Status: StatusResolved, Severity: SeverityCritical},
		{ID: "i4", Status: StatusClosed, Severity: SeverityInfo},
	} {
		store.CreateIncident(inc)
	}

	cases := []struct {
		status   string
		severity Severity
		want     int
	}{
		{"", "", 4},
		{string(StatusDetected), "", 2},
		{"", SeverityCritical, 2},
		{string(StatusDetected), SeverityHigh, 1},
		{string(StatusResolved), SeverityCritical, 1},
		{string(StatusResolved), SeverityHigh, 0},
		{"unknown", "", 0},
	}
	for _, tc := range cases {
		got := store.ListIncidents(tc.status, tc.severity)
		if len(got) != tc.want {
			t.Fatalf("ListIncidents(%q,%q) = %d 条, want %d", tc.status, tc.severity, len(got), tc.want)
		}
		for _, inc := range got {
			if tc.status != "" && string(inc.Status) != tc.status {
				t.Fatalf("状态过滤混入 %+v", inc)
			}
			if tc.severity != "" && inc.Severity != tc.severity {
				t.Fatalf("严重度过滤混入 %+v", inc)
			}
		}
	}
	if got := store.Incidents(); len(got) != 4 {
		t.Fatalf("Incidents() = %d 条, want 4", len(got))
	}
}

func TestMemoryStoreTimeline(t *testing.T) {
	store := NewMemoryStore()
	if got := store.AddTimelineEvent(nil); got != nil {
		t.Fatalf("AddTimelineEvent(nil) = %v", got)
	}

	ev := &TimelineEvent{ID: "ev-1", IncidentID: "inc-1", Type: "note", Description: "first"}
	added := store.AddTimelineEvent(ev)
	if added == nil || added.Timestamp.IsZero() {
		t.Fatalf("事件时间戳未补: %+v", added)
	}
	fixed := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	store.AddTimelineEvent(&TimelineEvent{ID: "ev-2", IncidentID: "inc-1", Timestamp: fixed, Type: "status"})

	timeline := store.GetTimeline("inc-1")
	if len(timeline) != 2 || timeline[0].ID != "ev-1" || timeline[1].ID != "ev-2" {
		t.Fatalf("时间线顺序/内容 = %+v", timeline)
	}
	if !timeline[1].Timestamp.Equal(fixed) {
		t.Fatalf("已有时间戳被覆盖: %v", timeline[1].Timestamp)
	}

	// GetTimeline 返回副本：调用方修改不应影响存储。
	timeline[0].Type = "mutated"
	if got := store.GetTimeline("inc-1"); got[0].Type != "note" {
		t.Fatalf("返回切片与存储共享底层数组: %v", got)
	}
	if got := store.GetTimeline("missing"); len(got) != 0 {
		t.Fatalf("缺失事件的时间线应为空, got %v", got)
	}
}

func TestMemoryStoreConcurrentAccess(t *testing.T) {
	store := NewMemoryStore()
	const workers = 8

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("inc-%d", i)
			store.CreateIncident(&Incident{ID: id, Status: StatusDetected})
			_ = store.GetIncident(id)
			_ = store.ListIncidents(string(StatusDetected), "")
			_ = store.Incidents()
			store.AddTimelineEvent(&TimelineEvent{ID: fmt.Sprintf("ev-%d", i), IncidentID: id})
			_ = store.GetTimeline(id)
		}(i)
	}
	wg.Wait()

	if got := store.Incidents(); len(got) != workers {
		t.Fatalf("并发创建后事件数 = %d, want %d", len(got), workers)
	}
}

func assertKeys(t *testing.T, obj map[string]any, want ...string) {
	t.Helper()
	got := make([]string, 0, len(obj))
	for k := range obj {
		got = append(got, k)
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("键集合 = %v, want %v", got, want)
	}
}

func TestIncidentJSONContract(t *testing.T) {
	occurred := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	inc := Incident{
		ID:         "inc-1",
		Title:      "db down",
		Severity:   SeverityHigh,
		Status:     StatusInvestigating,
		AlertIDs:   []string{"a1"},
		DeviceIDs:  []string{"d1"},
		Assignee:   "alice",
		Tags:       map[string]string{"env": "prod"},
		OccurredAt: &occurred,
		DetectedAt: occurred.Add(time.Minute),
		CreatedAt:  occurred,
		UpdatedAt:  occurred,
	}

	raw, err := json.Marshal(inc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	assertKeys(t, obj, "id", "title", "description", "severity", "status", "alert_ids",
		"device_ids", "assignee", "tags", "occurred_at", "detected_at", "created_at", "updated_at")
	if _, ok := obj["resolved_at"]; ok {
		t.Fatal("resolved_at 为空时应省略")
	}
	if _, ok := obj["closed_at"]; ok {
		t.Fatal("closed_at 为空时应省略")
	}

	var back Incident
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("回解: %v", err)
	}
	raw2, err := json.Marshal(back)
	if err != nil {
		t.Fatalf("复 marshal: %v", err)
	}
	if string(raw) != string(raw2) {
		t.Fatalf("往返不等价:\n%s\n%s", raw, raw2)
	}
}

func TestResponseMetricsDurationIsNanoseconds(t *testing.T) {
	raw, err := json.Marshal(ResponseMetrics{TotalIncidents: 3, AvgMTTD: 90 * time.Second})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	assertKeys(t, obj, "total_incidents", "active_incidents", "resolved_incidents",
		"avg_mttd", "avg_mttr", "avg_mttf")
	if obj["avg_mttd"] != float64(90*time.Second) {
		t.Fatalf("avg_mttd = %v, want 纳秒整数 %v", obj["avg_mttd"], float64(90*time.Second))
	}
}
