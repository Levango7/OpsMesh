// deploy_extra_test.go 补充 deploy 包未覆盖的函数：evaluateGate、selectCanaryTargets、
// DeployTask.Valid/ResolvedGate/EffectiveStrategy/EffectiveCanaryWeight、SplitIDs、
// Promote、Rollback、ReconcileAll、canary Execute/Reconcile。
package deploy

import (
	"context"
	"testing"

	"opsmesh/internal/proto"
)

// =============================================================================
// evaluateGate 纯函数
// =============================================================================

func TestEvaluateGate_Default(t *testing.T) {
	// 默认门禁（全零值）：要求 100% 成功。
	gate := GateConfig{}
	if !evaluateGate(gate, 2, 0, 2) {
		t.Fatal("2 done 0 failed 2 total should pass default gate")
	}
	if evaluateGate(gate, 1, 1, 2) {
		t.Fatal("1 done 1 failed 2 total should fail default gate")
	}
}

func TestEvaluateGate_TotalZero(t *testing.T) {
	gate := GateConfig{}
	if evaluateGate(gate, 0, 0, 0) {
		t.Fatal("total=0 should fail")
	}
}

func TestEvaluateGate_SuccessRate(t *testing.T) {
	gate := GateConfig{SuccessRate: 50}
	if !evaluateGate(gate, 1, 1, 2) {
		t.Fatal("50% success rate should pass with 1/2 done")
	}
	if evaluateGate(gate, 0, 2, 2) {
		t.Fatal("0% success rate should fail with SuccessRate=50")
	}
}

func TestEvaluateGate_MaxFailRate(t *testing.T) {
	gate := GateConfig{MaxFailRate: 10}
	// 仅设 MaxFailRate 时补默认 SuccessRate=1，避免 0% 成功率被放行。
	if evaluateGate(gate, 0, 2, 2) {
		t.Fatal("0% success with MaxFailRate=10 should fail (default SuccessRate=1)")
	}
	if !evaluateGate(gate, 2, 0, 2) {
		t.Fatal("100% success with MaxFailRate=10 should pass")
	}
}

func TestEvaluateGate_MinSuccessCount(t *testing.T) {
	gate := GateConfig{MinSuccessCount: 2}
	if evaluateGate(gate, 1, 0, 3) {
		t.Fatal("done=1 < MinSuccessCount=2 should fail")
	}
	if !evaluateGate(gate, 2, 0, 3) {
		t.Fatal("done=2 >= MinSuccessCount=2 should pass")
	}
}

// =============================================================================
// selectCanaryTargets 纯函数
// =============================================================================

func TestSelectCanaryTargets_EmptyList(t *testing.T) {
	if got := selectCanaryTargets(nil, 50); got != nil {
		t.Fatalf("empty list should return nil, got %v", got)
	}
}

func TestSelectCanaryTargets_WeightZero(t *testing.T) {
	if got := selectCanaryTargets([]string{"a", "b"}, 0); got != nil {
		t.Fatalf("weight=0 should return nil, got %v", got)
	}
}

func TestSelectCanaryTargets_WeightFull(t *testing.T) {
	all := []string{"a", "b", "c"}
	got := selectCanaryTargets(all, 100)
	if len(got) != 3 {
		t.Fatalf("weight=100 should return all, got %v", got)
	}
}

func TestSelectCanaryTargets_WeightPartial(t *testing.T) {
	all := []string{"a", "b", "c", "d"}
	got := selectCanaryTargets(all, 50)
	if len(got) != 2 {
		t.Fatalf("weight=50 of 4 should return 2, got %v", got)
	}
	if got[0] != "a" || got[1] != "b" {
		t.Fatalf("should return first 2, got %v", got)
	}
}

func TestSelectCanaryTargets_WeightSmall(t *testing.T) {
	all := []string{"a", "b", "c", "d", "e"}
	got := selectCanaryTargets(all, 10)
	// 5 * 10 / 100 = 0, 但 k < 1 时 k=1。
	if len(got) != 1 {
		t.Fatalf("weight=10 of 5 should return at least 1, got %v", got)
	}
}

// =============================================================================
// DeployTask.Valid
// =============================================================================

func TestDeployTask_Valid(t *testing.T) {
	// 合法部署。
	d := &DeployTask{Name: "x", Type: TypeScript, TargetIDs: "dev-1"}
	if err := d.Valid(); err != nil {
		t.Fatalf("valid deploy should pass: %v", err)
	}
	// 缺 name。
	d2 := &DeployTask{Type: TypeScript, TargetIDs: "dev-1"}
	if err := d2.Valid(); err == nil {
		t.Fatal("missing name should fail")
	}
	// 非法 type。
	d3 := &DeployTask{Name: "x", Type: "bogus", TargetIDs: "dev-1"}
	if err := d3.Valid(); err == nil {
		t.Fatal("invalid type should fail")
	}
	// 缺 target。
	d4 := &DeployTask{Name: "x", Type: TypeScript}
	if err := d4.Valid(); err == nil {
		t.Fatal("missing targets should fail")
	}
	// 非法 strategy。
	d5 := &DeployTask{Name: "x", Type: TypeScript, TargetIDs: "dev-1", Strategy: "bogus"}
	if err := d5.Valid(); err == nil {
		t.Fatal("invalid strategy should fail")
	}
	// CanaryWeight 越界。
	d6 := &DeployTask{Name: "x", Type: TypeScript, TargetIDs: "dev-1", CanaryWeight: 101}
	if err := d6.Valid(); err == nil {
		t.Fatal("canary_weight=101 should fail")
	}
	d7 := &DeployTask{Name: "x", Type: TypeScript, TargetIDs: "dev-1", CanaryWeight: -1}
	if err := d7.Valid(); err == nil {
		t.Fatal("canary_weight=-1 should fail")
	}
	// Gate 阈值越界。
	d8 := &DeployTask{Name: "x", Type: TypeScript, TargetIDs: "dev-1", Gate: &GateConfig{SuccessRate: 150}}
	if err := d8.Valid(); err == nil {
		t.Fatal("gate.success_rate=150 should fail")
	}
	d9 := &DeployTask{Name: "x", Type: TypeScript, TargetIDs: "dev-1", Gate: &GateConfig{MaxFailRate: -1}}
	if err := d9.Valid(); err == nil {
		t.Fatal("gate.max_fail_rate=-1 should fail")
	}
	// 合法 strategy + canary + gate。
	d10 := &DeployTask{Name: "x", Type: TypeK8s, TargetIDs: "dev-1", Strategy: StrategyCanary, CanaryWeight: 30, Gate: &GateConfig{SuccessRate: 80}}
	if err := d10.Valid(); err != nil {
		t.Fatalf("valid canary deploy should pass: %v", err)
	}
}

// =============================================================================
// DeployTask.ResolvedGate / EffectiveStrategy / EffectiveCanaryWeight
// =============================================================================

func TestResolvedGate_NilGate(t *testing.T) {
	d := &DeployTask{}
	g := d.ResolvedGate()
	if g.SuccessRate != defaultGateSuccessRate || g.MaxFailRate != defaultGateMaxFailRate {
		t.Fatalf("nil gate should return defaults, got %+v", g)
	}
}

func TestResolvedGate_EmptyGate(t *testing.T) {
	d := &DeployTask{Gate: &GateConfig{}}
	g := d.ResolvedGate()
	if g.SuccessRate != defaultGateSuccessRate {
		t.Fatalf("empty gate should fallback to default success rate, got %+v", g)
	}
}

func TestResolvedGate_CustomGate(t *testing.T) {
	d := &DeployTask{Gate: &GateConfig{SuccessRate: 50, MaxFailRate: 10}}
	g := d.ResolvedGate()
	if g.SuccessRate != 50 || g.MaxFailRate != 10 {
		t.Fatalf("custom gate should be preserved, got %+v", g)
	}
}

func TestEffectiveStrategy(t *testing.T) {
	d := &DeployTask{}
	if d.EffectiveStrategy() != StrategyRolling {
		t.Fatal("empty strategy should default to rolling")
	}
	d.Strategy = StrategyCanary
	if d.EffectiveStrategy() != StrategyCanary {
		t.Fatal("canary strategy should be preserved")
	}
}

func TestEffectiveCanaryWeight(t *testing.T) {
	d := &DeployTask{}
	if d.EffectiveCanaryWeight() != defaultCanaryWeight {
		t.Fatal("zero weight should default")
	}
	d.CanaryWeight = 50
	if d.EffectiveCanaryWeight() != 50 {
		t.Fatal("50 should be preserved")
	}
	d.CanaryWeight = 200
	if d.EffectiveCanaryWeight() != defaultCanaryWeight {
		t.Fatal(">100 should default")
	}
	d.CanaryWeight = -5
	if d.EffectiveCanaryWeight() != defaultCanaryWeight {
		t.Fatal("negative should default")
	}
}

// =============================================================================
// SplitIDs
// =============================================================================

func TestSplitIDs(t *testing.T) {
	if got := SplitIDs(""); got != nil {
		t.Fatalf("empty should return nil, got %v", got)
	}
	if got := SplitIDs("a"); len(got) != 1 || got[0] != "a" {
		t.Fatalf("single: got %v", got)
	}
	got := SplitIDs("a,b,c")
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("comma separated: got %v", got)
	}
	got = SplitIDs("a b c")
	if len(got) != 3 {
		t.Fatalf("space separated: got %v", got)
	}
	got = SplitIDs("a, b , c")
	if len(got) != 3 {
		t.Fatalf("mixed with whitespace: got %v", got)
	}
	got = SplitIDs("a,,b")
	if len(got) != 2 {
		t.Fatalf("empty entries should be skipped: got %v", got)
	}
}

// =============================================================================
// Promote
// =============================================================================

func TestPromote_Canary(t *testing.T) {
	ctx := context.Background()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	disp.devices["dev-2"] = &proto.DeviceInfo{DeviceID: "dev-2", AgentID: "a2"}
	disp.devices["dev-3"] = &proto.DeviceInfo{DeviceID: "dev-3", AgentID: "a3"}
	st := NewMemory()
	h := NewHandler(st, disp)

	dt, _ := st.Create(ctx, &DeployTask{
		Name: "svc", Type: TypeScript, TargetIDs: "dev-1,dev-2,dev-3", TenantID: "t1", CreatedBy: "tester",
		Strategy: StrategyCanary, CanaryWeight: 33,
	})
	_ = h.Execute(ctx, dt.ID, "t1")
	got, _ := st.Get(ctx, dt.ID, "t1")
	if got.Status != StatusCanary {
		t.Fatalf("want canary, got %s", got.Status)
	}
	// 晋级。
	if err := h.Promote(ctx, dt.ID, "t1"); err != nil {
		t.Fatalf("promote canary: %v", err)
	}
	got, _ = st.Get(ctx, dt.ID, "t1")
	if got.Status != StatusPromoting {
		t.Fatalf("want promoting, got %s", got.Status)
	}
}

func TestPromote_BlueGreen(t *testing.T) {
	ctx := context.Background()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	st := NewMemory()
	h := NewHandler(st, disp)

	dt, _ := st.Create(ctx, &DeployTask{
		Name: "svc", Type: TypeScript, TargetIDs: "dev-1", TenantID: "t1", CreatedBy: "tester",
		Strategy: StrategyBlueGreen,
	})
	_ = h.Execute(ctx, dt.ID, "t1")
	if err := h.Promote(ctx, dt.ID, "t1"); err != nil {
		t.Fatalf("promote bluegreen: %v", err)
	}
	got, _ := st.Get(ctx, dt.ID, "t1")
	if got.Status != StatusSuccess {
		t.Fatalf("want success, got %s", got.Status)
	}
}

func TestPromote_RollingReject(t *testing.T) {
	ctx := context.Background()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	st := NewMemory()
	h := NewHandler(st, disp)

	dt, _ := st.Create(ctx, newDeploy("x", TypeScript, "dev-1", "t1"))
	_ = h.Execute(ctx, dt.ID, "t1")
	if err := h.Promote(ctx, dt.ID, "t1"); err == nil {
		t.Fatal("rolling promote should fail")
	}
}

func TestPromote_WrongStatus(t *testing.T) {
	ctx := context.Background()
	disp := newFakeDisp()
	st := NewMemory()
	h := NewHandler(st, disp)
	dt, _ := st.Create(ctx, newDeploy("x", TypeScript, "dev-1", "t1"))
	// created 状态不可晋级。
	if err := h.Promote(ctx, dt.ID, "t1"); err == nil {
		t.Fatal("promote from created should fail")
	}
}

// =============================================================================
// Rollback
// =============================================================================

func TestRollback_FromSuccess(t *testing.T) {
	ctx := context.Background()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	st := NewMemory()
	h := NewHandler(st, disp)
	dt, _ := st.Create(ctx, newDeploy("x", TypeScript, "dev-1", "t1"))
	_ = h.Execute(ctx, dt.ID, "t1")
	for id := range disp.tasks {
		disp.tasks[id].Status = "done"
	}
	_ = h.Reconcile(ctx, dt.ID, "t1")
	if err := h.Rollback(ctx, dt.ID, "t1"); err != nil {
		t.Fatalf("rollback from success: %v", err)
	}
	got, _ := st.Get(ctx, dt.ID, "t1")
	if got.Status != StatusRolledBack {
		t.Fatalf("want rolledback, got %s", got.Status)
	}
}

func TestRollback_WrongStatus(t *testing.T) {
	ctx := context.Background()
	disp := newFakeDisp()
	st := NewMemory()
	h := NewHandler(st, disp)
	dt, _ := st.Create(ctx, newDeploy("x", TypeScript, "dev-1", "t1"))
	// created 状态不可回滚。
	if err := h.Rollback(ctx, dt.ID, "t1"); err == nil {
		t.Fatal("rollback from created should fail")
	}
}

// =============================================================================
// Reconcile canary + gate
// =============================================================================

func TestReconcile_CanaryGatePassed(t *testing.T) {
	ctx := context.Background()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	disp.devices["dev-2"] = &proto.DeviceInfo{DeviceID: "dev-2", AgentID: "a2"}
	disp.devices["dev-3"] = &proto.DeviceInfo{DeviceID: "dev-3", AgentID: "a3"}
	st := NewMemory()
	h := NewHandler(st, disp)

	dt, _ := st.Create(ctx, &DeployTask{
		Name: "svc", Type: TypeScript, TargetIDs: "dev-1,dev-2,dev-3", TenantID: "t1", CreatedBy: "tester",
		Strategy: StrategyCanary, CanaryWeight: 33,
	})
	_ = h.Execute(ctx, dt.ID, "t1")
	// canary 阶段任务全 done -> 门禁通过 -> gated。
	for id := range disp.tasks {
		disp.tasks[id].Status = "done"
	}
	if err := h.Reconcile(ctx, dt.ID, "t1"); err != nil {
		t.Fatalf("reconcile canary: %v", err)
	}
	got, _ := st.Get(ctx, dt.ID, "t1")
	if got.Status != StatusGated {
		t.Fatalf("want gated, got %s", got.Status)
	}
}

func TestReconcile_CanaryGateFailedWithAutoRollback(t *testing.T) {
	ctx := context.Background()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	disp.devices["dev-2"] = &proto.DeviceInfo{DeviceID: "dev-2", AgentID: "a2"}
	disp.devices["dev-3"] = &proto.DeviceInfo{DeviceID: "dev-3", AgentID: "a3"}
	st := NewMemory()
	h := NewHandler(st, disp)

	dt, _ := st.Create(ctx, &DeployTask{
		Name: "svc", Type: TypeScript, TargetIDs: "dev-1,dev-2,dev-3", TenantID: "t1", CreatedBy: "tester",
		Strategy: StrategyCanary, CanaryWeight: 33, AutoRollback: true,
	})
	_ = h.Execute(ctx, dt.ID, "t1")
	// canary 阶段任务全 failed -> 门禁不通过 -> failed -> auto rollback。
	for id := range disp.tasks {
		disp.tasks[id].Status = "failed"
	}
	_ = h.Reconcile(ctx, dt.ID, "t1")
	got, _ := st.Get(ctx, dt.ID, "t1")
	if got.Status != StatusRolledBack {
		t.Fatalf("want rolledback (auto), got %s", got.Status)
	}
}

func TestReconcile_CanaryGateFailedNoAutoRollback(t *testing.T) {
	ctx := context.Background()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	disp.devices["dev-2"] = &proto.DeviceInfo{DeviceID: "dev-2", AgentID: "a2"}
	disp.devices["dev-3"] = &proto.DeviceInfo{DeviceID: "dev-3", AgentID: "a3"}
	st := NewMemory()
	h := NewHandler(st, disp)

	dt, _ := st.Create(ctx, &DeployTask{
		Name: "svc", Type: TypeScript, TargetIDs: "dev-1,dev-2,dev-3", TenantID: "t1", CreatedBy: "tester",
		Strategy: StrategyCanary, CanaryWeight: 33, AutoRollback: false,
	})
	_ = h.Execute(ctx, dt.ID, "t1")
	for id := range disp.tasks {
		disp.tasks[id].Status = "failed"
	}
	_ = h.Reconcile(ctx, dt.ID, "t1")
	got, _ := st.Get(ctx, dt.ID, "t1")
	if got.Status != StatusFailed {
		t.Fatalf("want failed (no auto rollback), got %s", got.Status)
	}
}

func TestReconcile_CanaryStillRunning(t *testing.T) {
	ctx := context.Background()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	disp.devices["dev-2"] = &proto.DeviceInfo{DeviceID: "dev-2", AgentID: "a2"}
	disp.devices["dev-3"] = &proto.DeviceInfo{DeviceID: "dev-3", AgentID: "a3"}
	st := NewMemory()
	h := NewHandler(st, disp)

	dt, _ := st.Create(ctx, &DeployTask{
		Name: "svc", Type: TypeScript, TargetIDs: "dev-1,dev-2,dev-3", TenantID: "t1", CreatedBy: "tester",
		Strategy: StrategyCanary, CanaryWeight: 33,
	})
	_ = h.Execute(ctx, dt.ID, "t1")
	// 部分任务仍在 pending（未终态），应等待。
	for id := range disp.tasks {
		disp.tasks[id].Status = "pending"
	}
	_ = h.Reconcile(ctx, dt.ID, "t1")
	got, _ := st.Get(ctx, dt.ID, "t1")
	if got.Status != StatusCanary {
		t.Fatalf("want canary (still running), got %s", got.Status)
	}
}

// =============================================================================
// ReconcileAll
// =============================================================================

func TestReconcileAll(t *testing.T) {
	ctx := context.Background()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	disp.devices["dev-2"] = &proto.DeviceInfo{DeviceID: "dev-2", AgentID: "a2"}
	st := NewMemory()
	h := NewHandler(st, disp)

	dt1, _ := st.Create(ctx, newDeploy("svc1", TypeScript, "dev-1", "t1"))
	_ = h.Execute(ctx, dt1.ID, "t1")
	dt2, _ := st.Create(ctx, newDeploy("svc2", TypeScript, "dev-2", "t1"))
	_ = h.Execute(ctx, dt2.ID, "t1")

	// 全部 done。
	for id := range disp.tasks {
		disp.tasks[id].Status = "done"
	}
	n := h.ReconcileAll(ctx, "t1")
	if n < 2 {
		t.Fatalf("want at least 2 reconciled, got %d", n)
	}
}

// =============================================================================
// deployTypeToTaskType
// =============================================================================

func TestDeployTypeToTaskType(t *testing.T) {
	if deployTypeToTaskType(TypeScript) != proto.TaskTypeShell {
		t.Fatal("script should map to shell")
	}
	if deployTypeToTaskType(TypeFile) != proto.TaskTypeFile {
		t.Fatal("file should map to file")
	}
	if deployTypeToTaskType(TypeK8s) != proto.TaskTypeShell {
		t.Fatal("k8s should map to shell")
	}
	if deployTypeToTaskType("unknown") != proto.TaskTypeShell {
		t.Fatal("unknown should default to shell")
	}
}