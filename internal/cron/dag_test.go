package cron

import (
	"testing"
	"time"
)

func TestDAG_NoCycle_LinearChain(t *testing.T) {
	d := NewDAG()
	// a → b → c（a 依赖 b，b 依赖 c）
	if err := d.AddEdge("a", "b"); err != nil {
		t.Fatal(err)
	}
	if err := d.AddEdge("b", "c"); err != nil {
		t.Fatal(err)
	}
	if d.HasCycle() {
		t.Fatal("linear chain should not have cycle")
	}
	order, err := d.TopoSort()
	if err != nil {
		t.Fatal(err)
	}
	// 拓扑序：c 在 b 前，b 在 a 前。
	want := []string{"c", "b", "a"}
	if len(order) != len(want) {
		t.Fatalf("topo len = %d, want %d", len(order), len(want))
	}
	for i, w := range want {
		if order[i] != w {
			t.Errorf("topo[%d] = %s, want %s", i, order[i], w)
		}
	}
}

func TestDAG_CycleRejected(t *testing.T) {
	d := NewDAG()
	if err := d.AddEdge("a", "b"); err != nil {
		t.Fatal(err)
	}
	if err := d.AddEdge("b", "c"); err != nil {
		t.Fatal(err)
	}
	// c → a 形成环，应拒绝。
	if err := d.AddEdge("c", "a"); err != ErrDAGCycle {
		t.Fatalf("expect ErrDAGCycle, got %v", err)
	}
}

func TestDAG_SelfLoopRejected(t *testing.T) {
	d := NewDAG()
	if err := d.AddEdge("x", "x"); err != ErrDAGCycle {
		t.Fatalf("expect ErrDAGCycle for self-loop, got %v", err)
	}
}

func TestDAG_ReadyNodes(t *testing.T) {
	d := NewDAG()
	_ = d.AddEdge("a", "b")
	_ = d.AddEdge("a", "c")
	_ = d.AddEdge("b", "c")
	// 初始：仅 c 无前置依赖，可执行。
	done := map[string]struct{}{}
	ready := d.ReadyNodes(done)
	if len(ready) != 1 || ready[0] != "c" {
		t.Fatalf("initial ready = %v, want [c]", ready)
	}
	// c 完成后：b 可执行。
	done["c"] = struct{}{}
	ready = d.ReadyNodes(done)
	if len(ready) != 1 || ready[0] != "b" {
		t.Fatalf("after c done, ready = %v, want [b]", ready)
	}
	// b 完成后：a 可执行。
	done["b"] = struct{}{}
	ready = d.ReadyNodes(done)
	if len(ready) != 1 || ready[0] != "a" {
		t.Fatalf("after b done, ready = %v, want [a]", ready)
	}
}

func TestNextRun_MinuteStep(t *testing.T) {
	from := time.Date(2026, 8, 11, 10, 30, 0, 0, time.UTC)
	// */5 分钟：下次 10:35。
	next := NextRun("*/5 * * * *", from)
	want := time.Date(2026, 8, 11, 10, 35, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("NextRun(*/5) = %v, want %v", next, want)
	}
}

func TestNextRun_DailyAt2AM(t *testing.T) {
	from := time.Date(2026, 8, 11, 10, 30, 0, 0, time.UTC)
	// 0 2 * * *：每天 02:00，下次 2026-08-12 02:00。
	next := NextRun("0 2 * * *", from)
	want := time.Date(2026, 8, 12, 2, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("NextRun(0 2 * * *) = %v, want %v", next, want)
	}
}

func TestNextRun_InvalidExpr(t *testing.T) {
	next := NextRun("invalid", time.Now())
	if !next.IsZero() {
		t.Errorf("NextRun(invalid) should be zero, got %v", next)
	}
}