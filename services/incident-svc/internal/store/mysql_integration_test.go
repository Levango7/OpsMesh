package store

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/services/incident-svc/internal/models"
)

// TestIncidentStoreMySQLRoundTrip 是 incident-svc MySQL 后端的真库往返校验（TD-65「集成测试补齐」）。
//
// 写入走一个 store 实例、读回走**另一个全新实例**（新连接、无实例内状态），
// 以此证明数据真的落库而不是停在实例内存里。
//
// 重点覆盖 occurred_at：它是 MTTD 的起点（TD-60 §5.10 取证：该列缺失时 MTTD 恒 0），
// 2026-09-30 才贯通「模型 → INSERT → SELECT → Scan」——这里把「写进去能原值读回来」
// 变成可复跑的断言，防止后续改动把它再改回静默零值。
func TestIncidentStoreMySQLRoundTrip(t *testing.T) {
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("skipping MySQL integration smoke: missing OPSMESH_TEST_MYSQL_DSN env; set it to a real MySQL DSN to run")
	}

	w, err := NewMySQLStore(dsn)
	if err != nil {
		t.Fatalf("NewMySQLStore(writer): %v", err)
	}
	r, err := NewMySQLStore(dsn)
	if err != nil {
		t.Fatalf("NewMySQLStore(reader): %v", err)
	}

	id := fmt.Sprintf("itest-inc-%d", time.Now().UnixNano())
	occurred := time.Now().UTC().Truncate(time.Second).Add(-10 * time.Minute)
	detected := time.Now().UTC().Truncate(time.Second)
	defer func() {
		// DeleteIncident 自带级联（incidents + timeline_events），失败时再兜底清一次时间线。
		if !w.DeleteIncident(id) {
			t.Logf("cleanup: DeleteIncident(%s) 返回 false（可能本就没写成功）", id)
		}
	}()

	created := w.CreateIncident(&models.Incident{
		ID:          id,
		Title:       "itest roundtrip",
		Description: "真库往返校验",
		Severity:    models.SeverityHigh,
		Status:      models.StatusDetected,
		AlertIDs:    []string{"alert-a", "alert-b"},
		DeviceIDs:   []string{"dev-1"},
		Tags:        map[string]string{"env": "itest"},
		OccurredAt:  &occurred,
		DetectedAt:  detected,
	})
	if created == nil {
		t.Fatal("CreateIncident 返回 nil")
	}

	got := r.GetIncident(id)
	if got == nil {
		t.Fatal("新实例 GetIncident 未命中——事件没有真的落库")
	}
	if got.Title != "itest roundtrip" || got.Severity != models.SeverityHigh || got.Status != models.StatusDetected {
		t.Fatalf("读回 = {title:%q severity:%q status:%q}，与写入不一致", got.Title, got.Severity, got.Status)
	}
	if len(got.DeviceIDs) != 1 || got.DeviceIDs[0] != "dev-1" {
		t.Fatalf("DeviceIDs 读回 = %v, want [dev-1]（JSON 列往返失败）", got.DeviceIDs)
	}
	if got.Tags["env"] != "itest" {
		t.Fatalf("Tags 读回 = %v, want env=itest", got.Tags)
	}
	if got.OccurredAt == nil {
		t.Fatal("OccurredAt 读回为 nil——MTTD 起点丢失（该列贯通是 2026-09-30 的修复，此处守卫它不被改回）")
	}
	if !got.OccurredAt.Equal(occurred) {
		t.Fatalf("OccurredAt 读回 = %v, want %v", got.OccurredAt, occurred)
	}

	// 时间线：事件写入后，新实例读回——时间线是与 incidents 分开的落库路径。
	ev := w.AddTimelineEvent(&models.TimelineEvent{
		ID:          id + "-tl1",
		IncidentID:  id,
		Timestamp:   detected,
		Type:        "itest",
		Description: "timeline roundtrip",
		Author:      "itest",
	})
	if ev == nil {
		t.Fatal("AddTimelineEvent 返回 nil")
	}
	tl := r.GetTimeline(id)
	if len(tl) == 0 {
		t.Fatal("新实例 GetTimeline 为空——时间线没有落库")
	}
	if tl[0].Description != "timeline roundtrip" {
		t.Fatalf("时间线首条描述 = %q, want %q", tl[0].Description, "timeline roundtrip")
	}

	// 状态流转回写：resolve 后必须能在新实例读到新状态。
	got.Status = models.StatusResolved
	resolvedAt := time.Now().UTC().Truncate(time.Second)
	got.ResolvedAt = &resolvedAt
	if upd := w.UpdateIncident(got); upd == nil {
		t.Fatal("UpdateIncident 返回 nil")
	}
	again := r.GetIncident(id)
	if again == nil {
		t.Fatal("更新后新实例 GetIncident 未命中")
	}
	if again.Status != models.StatusResolved {
		t.Fatalf("状态读回 = %q, want resolved（UPDATE 未落库）", again.Status)
	}
	if again.ResolvedAt == nil || !again.ResolvedAt.Equal(resolvedAt) {
		t.Fatalf("ResolvedAt 读回 = %v, want %v", again.ResolvedAt, resolvedAt)
	}

	// 删除语义：删掉后新实例必须查不到。
	if !w.DeleteIncident(id) {
		t.Fatal("DeleteIncident 返回 false")
	}
	if r.GetIncident(id) != nil {
		t.Fatal("删除后仍能查到事件——DELETE 没有落到库")
	}
	if len(r.GetTimeline(id)) != 0 {
		t.Fatal("删除后仍能查到时间线——级联删除没有生效")
	}

	t.Logf("ROUNDTRIP_OK: incidents（含 occurred_at/JSON 列）+ timeline_events 均验证『写入→新实例读回』")
}
