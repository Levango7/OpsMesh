package dag

import (
	"testing"

	"opsmesh/internal/proto"
)

func mk(id string, deps ...string) *proto.Task {
	return &proto.Task{TaskID: id, DependsOn: deps, Status: "pending"}
}

func TestReadyIDsNoDeps(t *testing.T) {
	tasks := []*proto.Task{mk("a"), mk("b"), mk("c")}
	got := ReadyIDs(tasks)
	if len(got) != 3 {
		t.Fatalf("expect 3 ready (no deps), got %v", got)
	}
}

func TestReadyIDsBlockedUntilDone(t *testing.T) {
	a := mk("a")
	b := mk("b", "a") // b depends on a
	tasks := []*proto.Task{a, b}
	if got := ReadyIDs(tasks); len(got) != 1 || got[0] != "a" {
		t.Fatalf("before a done: expect only [a], got %v", got)
	}
	a.Status = "done"
	if got := ReadyIDs(tasks); len(got) != 2 {
		t.Fatalf("after a done: expect [a,b], got %v", got)
	}
}

func TestTopoOrderLinear(t *testing.T) {
	tasks := []*proto.Task{mk("a"), mk("b", "a"), mk("c", "b")}
	order, err := TopoOrder(tasks)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(order) != 3 || order[0] != "a" || order[1] != "b" || order[2] != "c" {
		t.Fatalf("bad order: %v", order)
	}
}

func TestTopoOrderFanIn(t *testing.T) {
	tasks := []*proto.Task{mk("a"), mk("b"), mk("c", "a", "b")}
	order, err := TopoOrder(tasks)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	pos := map[string]int{}
	for i, id := range order {
		pos[id] = i
	}
	if !(pos["a"] < pos["c"] && pos["b"] < pos["c"]) {
		t.Fatalf("fan-in order wrong: %v", order)
	}
}

func TestTopoOrderCycle(t *testing.T) {
	tasks := []*proto.Task{mk("a", "b"), mk("b", "a")}
	if _, err := TopoOrder(tasks); err == nil {
		t.Fatal("expect cycle error")
	}
}

func TestValidateSelfDep(t *testing.T) {
	if err := Validate([]*proto.Task{mk("a", "a")}); err == nil {
		t.Fatal("expect self-dep error")
	}
}

func TestValidateMissingDep(t *testing.T) {
	if err := Validate([]*proto.Task{mk("a", "ghost")}); err == nil {
		t.Fatal("expect missing-dep error")
	}
}

func TestValidateOK(t *testing.T) {
	tasks := []*proto.Task{mk("a"), mk("b", "a"), mk("c", "a", "b")}
	if err := Validate(tasks); err != nil {
		t.Fatalf("expect nil, got %v", err)
	}
}
