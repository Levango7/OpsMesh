package logstore

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func sampleEntry(tenant, agent, level, source, msg string) *Entry {
	return &Entry{
		TenantID:  tenant,
		AgentID:   agent,
		Level:     level,
		Source:    source,
		Message:   msg,
		Timestamp: time.Now(),
	}
}

func TestMemoryAppendAndQuery(t *testing.T) {
	ctx := context.Background()
	ls := NewMemory(0)
	if err := ls.Append(ctx, sampleEntry("t1", "a1", "info", "task", "hello world")); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := ls.Append(ctx, sampleEntry("t1", "a1", "error", "task", "boom")); err != nil {
		t.Fatalf("append: %v", err)
	}
	// 无租户过滤条件时（dev 模式）Query 仍要求显式 TenantID？不——Query 不强制，空串=不过滤。
	out, err := ls.Query(ctx, Query{TenantID: "t1"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2 entries, got %d", len(out))
	}
	// 倒序：最新在前（boom 在最后写入）。
	if out[0].Message != "boom" {
		t.Fatalf("want newest first, got %q", out[0].Message)
	}
	// ID 单调递增。
	if out[0].ID <= out[1].ID {
		t.Fatalf("ID 应单调递增：%d <= %d", out[0].ID, out[1].ID)
	}
}

func TestMemoryTenantIsolation(t *testing.T) {
	ctx := context.Background()
	ls := NewMemory(0)
	_ = ls.Append(ctx, sampleEntry("t1", "a1", "info", "task", "belongs t1"))
	_ = ls.Append(ctx, sampleEntry("t2", "a2", "info", "task", "belongs t2"))
	out, err := ls.Query(ctx, Query{TenantID: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].TenantID != "t1" {
		t.Fatalf("租户隔离失败：%#v", out)
	}
}

func TestMemoryFilters(t *testing.T) {
	ctx := context.Background()
	ls := NewMemory(0)
	base := time.Now()
	// 构造可控时间序：手动设时间戳。
	e1 := sampleEntry("t1", "a1", "info", "agent", "disk full")
	e1.Timestamp = base.Add(-2 * time.Hour)
	e2 := sampleEntry("t1", "a2", "error", "task", "oom killed")
	e2.Timestamp = base.Add(-1 * time.Hour)
	e3 := sampleEntry("t1", "a1", "warn", "system", "cpu high")
	e3.Timestamp = base
	_ = ls.Append(ctx, e1)
	_ = ls.Append(ctx, e2)
	_ = ls.Append(ctx, e3)

	// 按 level 过滤。
	lv, _ := ls.Query(ctx, Query{TenantID: "t1", Level: "error"})
	if len(lv) != 1 || lv[0].Message != "oom killed" {
		t.Fatalf("level 过滤失败：%#v", lv)
	}
	// 按 agent 过滤。
	ag, _ := ls.Query(ctx, Query{TenantID: "t1", AgentID: "a1"})
	if len(ag) != 2 {
		t.Fatalf("agent 过滤失败：%d", len(ag))
	}
	// 关键字。
	kw, _ := ls.Query(ctx, Query{TenantID: "t1", Keyword: "full"})
	if len(kw) != 1 || kw[0].Message != "disk full" {
		t.Fatalf("keyword 过滤失败：%#v", kw)
	}
	// 时间窗。
	from := base.Add(-90 * time.Minute)
	to := base.Add(-30 * time.Minute)
	tw, _ := ls.Query(ctx, Query{TenantID: "t1", From: from, To: to})
	if len(tw) != 1 || tw[0].Message != "oom killed" {
		t.Fatalf("时间窗过滤失败：%#v", tw)
	}
}

func TestMemoryRingTruncate(t *testing.T) {
	ctx := context.Background()
	ls := NewMemory(2) // 仅保留 2 条
	for i := 0; i < 5; i++ {
		if err := ls.Append(ctx, sampleEntry("t1", "a1", "info", "task", "m")); err != nil {
			t.Fatal(err)
		}
	}
	out, _ := ls.Query(ctx, Query{TenantID: "t1"})
	if len(out) != 2 {
		t.Fatalf("环形裁剪失败：want 2 got %d", len(out))
	}
	// 最新两条应为第 4、5 条（ID 4、5）。
	if out[0].ID != 5 || out[1].ID != 4 {
		t.Fatalf("裁剪后应为最新两条，got %d,%d", out[0].ID, out[1].ID)
	}
}

func TestMemoryDefaultLimit(t *testing.T) {
	ctx := context.Background()
	ls := NewMemory(0)
	for i := 0; i < 10; i++ {
		_ = ls.Append(ctx, sampleEntry("t1", "a1", "info", "task", "m"))
	}
	out, _ := ls.Query(ctx, Query{TenantID: "t1"}) // 不传 limit
	if len(out) != 10 {
		t.Fatalf("不足 10 条时全返，got %d", len(out))
	}
	// 上限保护：limit 超过 maxQueryLimit 时截断（这里直接用大值验证不 panic）。
	big, _ := ls.Query(ctx, Query{TenantID: "t1", Limit: 999999})
	if len(big) != 10 { // 总量仅 10，受数据量限制
		t.Fatalf("got %d", len(big))
	}
}

func TestMemoryPagination(t *testing.T) {
	ctx := context.Background()
	ls := NewMemory(0)
	base := time.Now()
	// 5 条，m1 最旧、m5 最新；倒序检索时顺序为 m5,m4,m3,m2,m1。
	for i := 1; i <= 5; i++ {
		e := sampleEntry("t1", "a1", "info", "task", fmt.Sprintf("m%d", i))
		e.Timestamp = base.Add(time.Duration(i) * time.Minute)
		if err := ls.Append(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	// 第 1 页：limit=2 offset=0 -> m5,m4。
	p1, _ := ls.Query(ctx, Query{TenantID: "t1", Limit: 2, Offset: 0})
	if len(p1) != 2 || p1[0].Message != "m5" || p1[1].Message != "m4" {
		t.Fatalf("page1 失败：%#v", p1)
	}
	// 第 2 页：limit=2 offset=2 -> m3,m2。
	p2, _ := ls.Query(ctx, Query{TenantID: "t1", Limit: 2, Offset: 2})
	if len(p2) != 2 || p2[0].Message != "m3" || p2[1].Message != "m2" {
		t.Fatalf("page2 失败：%#v", p2)
	}
	// 末页残留：limit=10 offset=4 -> 仅 m1。
	p3, _ := ls.Query(ctx, Query{TenantID: "t1", Limit: 10, Offset: 4})
	if len(p3) != 1 || p3[0].Message != "m1" {
		t.Fatalf("page3 失败：%#v", p3)
	}
	// offset 越界：空结果不 panic。
	p4, _ := ls.Query(ctx, Query{TenantID: "t1", Limit: 2, Offset: 100})
	if len(p4) != 0 {
		t.Fatalf("offset 越界应返回空，got %d", len(p4))
	}
}
