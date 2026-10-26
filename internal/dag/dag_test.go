package dag

import (
	"testing"

	"github.com/Levango7/OpsMesh/internal/proto"
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
	if pos["a"] >= pos["c"] || pos["b"] >= pos["c"] {
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

// ---------------------------------------------------------------------------
// AllDepsTerminal / AnyDepFailed（依赖终态判定，修复"上游失败后下游永久 blocked"）
// ---------------------------------------------------------------------------

// TestAllDepsDoneVsTerminal 区分"全部 done"与"全部达终态"两种口径。
func TestAllDepsDoneVsTerminal(t *testing.T) {
	a := mk("a")
	b := mk("b", "a")
	byID := indexByID([]*proto.Task{a, b})

	// 上游 pending：两口径都不成立。
	if AllDepsDone(b, byID) || AllDepsTerminal(b, byID) {
		t.Fatal("上游 pending：Done/Terminal 都应 false")
	}

	// 上游 done：Done=true 且 Terminal=true。
	a.Status = "done"
	if !AllDepsDone(b, byID) || !AllDepsTerminal(b, byID) {
		t.Fatal("上游 done：Done/Terminal 都应 true")
	}

	// 上游 failed：Done=false（旧口径卡死），Terminal=true（新口径可判定终局）。
	a.Status = "failed"
	if AllDepsDone(b, byID) {
		t.Fatal("上游 failed：AllDepsDone 应 false")
	}
	if !AllDepsTerminal(b, byID) {
		t.Fatal("上游 failed：AllDepsTerminal 应 true")
	}

	// 上游 cancelled：同样 Done=false、Terminal=true。
	a.Status = "cancelled"
	if AllDepsDone(b, byID) {
		t.Fatal("上游 cancelled：AllDepsDone 应 false")
	}
	if !AllDepsTerminal(b, byID) {
		t.Fatal("上游 cancelled：AllDepsTerminal 应 true")
	}
}

// TestAllDepsTerminal_MissingDep 验证缺失依赖仍判未就绪（与 AllDepsDone 保守口径一致）。
func TestAllDepsTerminal_MissingDep(t *testing.T) {
	b := mk("b", "ghost")
	byID := indexByID([]*proto.Task{b})
	if AllDepsTerminal(b, byID) {
		t.Fatal("缺失依赖不应判终态就绪")
	}
	if AllDepsDone(b, byID) {
		t.Fatal("缺失依赖 AllDepsDone 应 false")
	}
}

// TestAnyDepFailed 验证失败/取消依赖的识别，以及全部 done 时无失败依赖。
func TestAnyDepFailed(t *testing.T) {
	// 单依赖 failed ⇒ 报出该依赖 ID。
	a := &proto.Task{TaskID: "a", Status: "failed"}
	b := mk("b", "a")
	byID := indexByID([]*proto.Task{a, b})
	id, failed := AnyDepFailed(b, byID)
	if !failed || id != "a" {
		t.Fatalf("AnyDepFailed = (%q,%v), want (\"a\",true)", id, failed)
	}

	// 单依赖 cancelled ⇒ 同样报出（取消也是"上游没走到 done"）。
	a.Status = "cancelled"
	if id, failed := AnyDepFailed(b, byID); !failed || id != "a" {
		t.Fatalf("AnyDepFailed(cancelled) = (%q,%v), want (\"a\",true)", id, failed)
	}

	// 全部 done ⇒ 无失败依赖。
	a.Status = "done"
	if id, failed := AnyDepFailed(b, byID); failed || id != "" {
		t.Fatalf("AnyDepFailed(done) = (%q,%v), want (\"\",false)", id, failed)
	}

	// 依赖仍在运行（非终态）⇒ 不算失败（由 AllDepsTerminal 卡住就绪判定）。
	a.Status = "running"
	if id, failed := AnyDepFailed(b, byID); failed || id != "" {
		t.Fatalf("AnyDepFailed(running) = (%q,%v), want (\"\",false)", id, failed)
	}
}

// TestAnyDepFailed_FanIn 验证 fan-in：任一上游失败即报失败，全部 done 才放行。
func TestAnyDepFailed_FanIn(t *testing.T) {
	a := mk("a")
	b := mk("b")
	c := mk("c", "a", "b")
	byID := indexByID([]*proto.Task{a, b, c})
	if _, failed := AnyDepFailed(c, byID); failed {
		t.Fatal("上游均未达终态：不应报失败")
	}
	a.Status = "done"
	b.Status = "failed"
	id, failed := AnyDepFailed(c, byID)
	if !failed || id != "b" {
		t.Fatalf("fan-in 任一失败 = (%q,%v), want (\"b\",true)", id, failed)
	}
	b.Status = "done"
	if _, failed := AnyDepFailed(c, byID); failed {
		t.Fatal("全部 done：不应报失败")
	}
}

// TestAnyDepFailed_NoDeps 验证无依赖任务不报失败。
func TestAnyDepFailed_NoDeps(t *testing.T) {
	x := mk("x")
	byID := indexByID([]*proto.Task{x})
	if id, failed := AnyDepFailed(x, byID); failed || id != "" {
		t.Fatalf("无依赖 = (%q,%v), want (\"\",false)", id, failed)
	}
}
