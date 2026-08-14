// auto_advance_test.go 测试灰度自动推进管理器：门禁评估（通过/失败率/延迟/样本不足）
// 与推进决策（advance/rollback/promote）。
package deploy

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"opsmesh/internal/proto"
)

// =============================================================================
// 测试辅助：fakeTaskResultProvider 可控的任务结果提供器
// =============================================================================

// fakeTaskResults 实现 TaskResultProvider，按 taskID 返回预设结果。
type fakeTaskResults struct {
	results map[string]*proto.TaskResult
}

func newFakeTaskResults() *fakeTaskResults {
	return &fakeTaskResults{results: make(map[string]*proto.TaskResult)}
}

func (f *fakeTaskResults) TaskResult(taskID string) *proto.TaskResult {
	return f.results[taskID]
}

// setResult 批量设置任务结果（exitCode=0 成功，非 0 失败；durationMs 延迟）。
func (f *fakeTaskResults) setResult(taskID string, exitCode int, durationMs int64) {
	f.results[taskID] = &proto.TaskResult{
		TaskID:     taskID,
		ExitCode:   exitCode,
		DurationMs: durationMs,
		FinishedAt: time.Now(),
	}
}

// newAutoAdvanceManagerForTest 构造测试用 manager，注入可控回调。
// 返回 manager + advance/promote/rollback 的调用计数器。
func newAutoAdvanceManagerForTest(t *testing.T, deploys DeployStore, tasks TaskResultProvider) (*AutoAdvanceManager, *int32, *int32, *int32) {
	t.Helper()
	cfg := AutoAdvanceConfig{
		Enabled:              true,
		CheckInterval:        10 * time.Millisecond, // 测试用短间隔
		FailureRateThreshold: 0.05,
		LatencyThreshold:     500 * time.Millisecond,
		MinSampleSize:        2, // 测试用小样本
		AdvanceRatio:         0.2,
		MaxRatio:             1.0,
	}
	var advanceCnt, promoteCnt, rollbackCnt int32
	m := NewAutoAdvanceManager(cfg, deploys, tasks,
		func(ctx context.Context, id int64, tenant string, w int) error {
			atomic.AddInt32(&advanceCnt, 1)
			// 更新部署的 CanaryWeight（模拟 advance 生效）。
			dt, err := deploys.Get(ctx, id, tenant)
			if err != nil {
				return err
			}
			dt.CanaryWeight = w
			return deploys.Update(ctx, dt)
		},
		func(ctx context.Context, id int64, tenant string) error {
			atomic.AddInt32(&promoteCnt, 1)
			dt, err := deploys.Get(ctx, id, tenant)
			if err != nil {
				return err
			}
			dt.Status = StatusPromoting
			return deploys.Update(ctx, dt)
		},
		func(ctx context.Context, id int64, tenant string) error {
			atomic.AddInt32(&rollbackCnt, 1)
			dt, err := deploys.Get(ctx, id, tenant)
			if err != nil {
				return err
			}
			dt.Status = StatusRolledBack
			return deploys.Update(ctx, dt)
		},
	)
	return m, &advanceCnt, &promoteCnt, &rollbackCnt
}

// setupCanaryDeploy 创建一个金丝雀部署并执行到 canary 状态，返回部署 ID。
func setupCanaryDeploy(t *testing.T, deploys DeployStore, disp *fakeDisp, taskResults *fakeTaskResults, weight int) int64 {
	t.Helper()
	ctx := context.Background()
	dt, err := deploys.Create(ctx, &DeployTask{
		Name:         "svc",
		Type:         TypeScript,
		TargetIDs:    "dev-1,dev-2,dev-3,dev-4,dev-5",
		TenantID:     "t1",
		CreatedBy:    "tester",
		Strategy:     StrategyCanary,
		CanaryWeight: weight,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	h := NewHandler(deploys, disp)
	if err := h.Execute(ctx, dt.ID, "t1"); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got, _ := deploys.Get(ctx, dt.ID, "t1")
	if got.Status != StatusCanary {
		t.Fatalf("want canary, got %s", got.Status)
	}
	return dt.ID
}

// =============================================================================
// evaluateGate 门禁评估
// =============================================================================

func TestAutoAdvanceManager_EvaluateGate_Pass(t *testing.T) {
	ctx := context.Background()
	deploys := NewMemory()
	disp := newFakeDisp()
	for i := 1; i <= 5; i++ {
		disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	}
	tr := newFakeTaskResults()
	deployID := setupCanaryDeploy(t, deploys, disp, tr, 20)

	// 设置已派发任务的结果：全部成功，延迟 100ms（达标）。
	got, _ := deploys.Get(ctx, deployID, "t1")
	for _, tid := range SplitIDs(got.TaskIDs) {
		tr.setResult(tid, 0, 100)
	}

	m, _, _, _ := newAutoAdvanceManagerForTest(t, deploys, tr)
	result, err := m.evaluateGate(deployID)
	if err != nil {
		t.Fatalf("evaluateGate: %v", err)
	}
	if !result.Passed {
		t.Fatalf("gate should pass, got reason: %s", result.Reason)
	}
	if result.SampleSize == 0 {
		t.Fatal("sample size should be > 0")
	}
	if result.FailureRate > 0.0001 {
		t.Fatalf("failure rate should be 0, got %.4f", result.FailureRate)
	}
	if result.AvgLatency != 100*time.Millisecond {
		t.Fatalf("avg latency should be 100ms, got %s", result.AvgLatency)
	}
}

func TestAutoAdvanceManager_EvaluateGate_Fail(t *testing.T) {
	ctx := context.Background()
	deploys := NewMemory()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	disp.devices["dev-2"] = &proto.DeviceInfo{DeviceID: "dev-2", AgentID: "a2"}
	disp.devices["dev-3"] = &proto.DeviceInfo{DeviceID: "dev-3", AgentID: "a3"}
	disp.devices["dev-4"] = &proto.DeviceInfo{DeviceID: "dev-4", AgentID: "a4"}
	disp.devices["dev-5"] = &proto.DeviceInfo{DeviceID: "dev-5", AgentID: "a5"}
	tr := newFakeTaskResults()
	deployID := setupCanaryDeploy(t, deploys, disp, tr, 20)

	// 设置结果：50% 失败（远超 5% 阈值）。
	got, _ := deploys.Get(ctx, deployID, "t1")
	taskIDs := SplitIDs(got.TaskIDs)
	for i, tid := range taskIDs {
		if i%2 == 0 {
			tr.setResult(tid, 1, 100) // 失败
		} else {
			tr.setResult(tid, 0, 100) // 成功
		}
	}

	m, _, _, _ := newAutoAdvanceManagerForTest(t, deploys, tr)
	result, err := m.evaluateGate(deployID)
	if err != nil {
		t.Fatalf("evaluateGate: %v", err)
	}
	if result.Passed {
		t.Fatal("gate should fail with 50% failure rate")
	}
	if result.FailureRate < 0.4 {
		t.Fatalf("failure rate should be ~0.5, got %.4f", result.FailureRate)
	}
}

func TestAutoAdvanceManager_EvaluateGate_LatencyFail(t *testing.T) {
	ctx := context.Background()
	deploys := NewMemory()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	disp.devices["dev-2"] = &proto.DeviceInfo{DeviceID: "dev-2", AgentID: "a2"}
	disp.devices["dev-3"] = &proto.DeviceInfo{DeviceID: "dev-3", AgentID: "a3"}
	disp.devices["dev-4"] = &proto.DeviceInfo{DeviceID: "dev-4", AgentID: "a4"}
	disp.devices["dev-5"] = &proto.DeviceInfo{DeviceID: "dev-5", AgentID: "a5"}
	tr := newFakeTaskResults()
	deployID := setupCanaryDeploy(t, deploys, disp, tr, 20)

	// 设置结果：全部成功，但延迟 800ms（超过 500ms 阈值）。
	got, _ := deploys.Get(ctx, deployID, "t1")
	for _, tid := range SplitIDs(got.TaskIDs) {
		tr.setResult(tid, 0, 800)
	}

	m, _, _, _ := newAutoAdvanceManagerForTest(t, deploys, tr)
	result, err := m.evaluateGate(deployID)
	if err != nil {
		t.Fatalf("evaluateGate: %v", err)
	}
	if result.Passed {
		t.Fatal("gate should fail with 800ms latency > 500ms threshold")
	}
	if result.AvgLatency != 800*time.Millisecond {
		t.Fatalf("avg latency should be 800ms, got %s", result.AvgLatency)
	}
}

func TestAutoAdvanceManager_EvaluateGate_InsufficientSamples(t *testing.T) {
	ctx := context.Background()
	deploys := NewMemory()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	disp.devices["dev-2"] = &proto.DeviceInfo{DeviceID: "dev-2", AgentID: "a2"}
	disp.devices["dev-3"] = &proto.DeviceInfo{DeviceID: "dev-3", AgentID: "a3"}
	disp.devices["dev-4"] = &proto.DeviceInfo{DeviceID: "dev-4", AgentID: "a4"}
	disp.devices["dev-5"] = &proto.DeviceInfo{DeviceID: "dev-5", AgentID: "a5"}
	tr := newFakeTaskResults()
	deployID := setupCanaryDeploy(t, deploys, disp, tr, 20)

	// 不设置任何结果（样本数=0），或只设置 1 个（< MinSampleSize=2）。
	got, _ := deploys.Get(ctx, deployID, "t1")
	taskIDs := SplitIDs(got.TaskIDs)
	if len(taskIDs) == 0 {
		t.Fatal("expected at least 1 dispatched task")
	}
	tr.setResult(taskIDs[0], 0, 100) // 仅 1 个样本

	m, _, _, _ := newAutoAdvanceManagerForTest(t, deploys, tr)
	result, err := m.evaluateGate(deployID)
	if err != nil {
		t.Fatalf("evaluateGate: %v", err)
	}
	// 样本不足时 evaluateGate 仍返回结果（Passed 取决于指标），但 checkAndAdvance 会据此等待。
	if result.SampleSize != 1 {
		t.Fatalf("sample size should be 1, got %d", result.SampleSize)
	}
	// 验证 checkAndAdvance 在样本不足时选择 wait。
	st := &monitorState{}
	if err := m.checkAndAdvance(context.Background(), deployID, st); err != nil {
		t.Fatalf("checkAndAdvance: %v", err)
	}
	if st.lastAction != "wait" {
		t.Fatalf("should wait for insufficient samples, got action %s", st.lastAction)
	}
}

// =============================================================================
// checkAndAdvance 推进决策
// =============================================================================

func TestAutoAdvanceManager_CheckAndAdvance_Advance(t *testing.T) {
	ctx := context.Background()
	deploys := NewMemory()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	disp.devices["dev-2"] = &proto.DeviceInfo{DeviceID: "dev-2", AgentID: "a2"}
	disp.devices["dev-3"] = &proto.DeviceInfo{DeviceID: "dev-3", AgentID: "a3"}
	disp.devices["dev-4"] = &proto.DeviceInfo{DeviceID: "dev-4", AgentID: "a4"}
	disp.devices["dev-5"] = &proto.DeviceInfo{DeviceID: "dev-5", AgentID: "a5"}
	tr := newFakeTaskResults()
	// CanaryWeight=40 → 5*40/100=2 个目标派发，样本数=2 >= MinSampleSize=2。
	deployID := setupCanaryDeploy(t, deploys, disp, tr, 40)

	// 设置结果：全部成功，延迟 100ms（达标）。
	got, _ := deploys.Get(ctx, deployID, "t1")
	for _, tid := range SplitIDs(got.TaskIDs) {
		tr.setResult(tid, 0, 100)
	}

	m, advanceCnt, promoteCnt, rollbackCnt := newAutoAdvanceManagerForTest(t, deploys, tr)
	st := &monitorState{}
	if err := m.checkAndAdvance(ctx, deployID, st); err != nil {
		t.Fatalf("checkAndAdvance: %v", err)
	}
	if st.lastAction != "advance" {
		t.Fatalf("should advance, got %s", st.lastAction)
	}
	if atomic.LoadInt32(advanceCnt) != 1 {
		t.Fatalf("advance should be called once, got %d", *advanceCnt)
	}
	if atomic.LoadInt32(promoteCnt) != 0 || atomic.LoadInt32(rollbackCnt) != 0 {
		t.Fatalf("promote/rollback should not be called")
	}
	// 验证 CanaryWeight 已增长（40 + 20 = 60）。
	updated, _ := deploys.Get(ctx, deployID, "t1")
	if updated.CanaryWeight != 60 {
		t.Fatalf("canary weight should be 60, got %d", updated.CanaryWeight)
	}
}

func TestAutoAdvanceManager_CheckAndAdvance_Rollback(t *testing.T) {
	ctx := context.Background()
	deploys := NewMemory()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	disp.devices["dev-2"] = &proto.DeviceInfo{DeviceID: "dev-2", AgentID: "a2"}
	disp.devices["dev-3"] = &proto.DeviceInfo{DeviceID: "dev-3", AgentID: "a3"}
	disp.devices["dev-4"] = &proto.DeviceInfo{DeviceID: "dev-4", AgentID: "a4"}
	disp.devices["dev-5"] = &proto.DeviceInfo{DeviceID: "dev-5", AgentID: "a5"}
	tr := newFakeTaskResults()
	// CanaryWeight=40 → 2 个目标派发，样本数=2 >= MinSampleSize=2。
	deployID := setupCanaryDeploy(t, deploys, disp, tr, 40)

	// 设置结果：全部失败（100% 失败率，远超 5% 阈值）。
	got, _ := deploys.Get(ctx, deployID, "t1")
	for _, tid := range SplitIDs(got.TaskIDs) {
		tr.setResult(tid, 1, 100)
	}

	m, advanceCnt, promoteCnt, rollbackCnt := newAutoAdvanceManagerForTest(t, deploys, tr)
	st := &monitorState{}
	if err := m.checkAndAdvance(ctx, deployID, st); err != nil {
		t.Fatalf("checkAndAdvance: %v", err)
	}
	if st.lastAction != "rollback" {
		t.Fatalf("should rollback, got %s", st.lastAction)
	}
	if atomic.LoadInt32(rollbackCnt) != 1 {
		t.Fatalf("rollback should be called once, got %d", *rollbackCnt)
	}
	if atomic.LoadInt32(advanceCnt) != 0 || atomic.LoadInt32(promoteCnt) != 0 {
		t.Fatalf("advance/promote should not be called")
	}
	// 验证状态已回滚。
	updated, _ := deploys.Get(ctx, deployID, "t1")
	if updated.Status != StatusRolledBack {
		t.Fatalf("status should be rolledback, got %s", updated.Status)
	}
}

func TestAutoAdvanceManager_CheckAndAdvance_Promote(t *testing.T) {
	ctx := context.Background()
	deploys := NewMemory()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	disp.devices["dev-2"] = &proto.DeviceInfo{DeviceID: "dev-2", AgentID: "a2"}
	disp.devices["dev-3"] = &proto.DeviceInfo{DeviceID: "dev-3", AgentID: "a3"}
	disp.devices["dev-4"] = &proto.DeviceInfo{DeviceID: "dev-4", AgentID: "a4"}
	disp.devices["dev-5"] = &proto.DeviceInfo{DeviceID: "dev-5", AgentID: "a5"}
	tr := newFakeTaskResults()
	// 初始灰度 90%，AdvanceRatio=0.2 → 90+20=110 >= MaxRatio=100 → promote。
	deployID := setupCanaryDeploy(t, deploys, disp, tr, 90)

	// 设置结果：全部成功，延迟 100ms（达标）。
	got, _ := deploys.Get(ctx, deployID, "t1")
	for _, tid := range SplitIDs(got.TaskIDs) {
		tr.setResult(tid, 0, 100)
	}

	m, advanceCnt, promoteCnt, rollbackCnt := newAutoAdvanceManagerForTest(t, deploys, tr)
	st := &monitorState{}
	if err := m.checkAndAdvance(ctx, deployID, st); err != nil {
		t.Fatalf("checkAndAdvance: %v", err)
	}
	if st.lastAction != "promote" {
		t.Fatalf("should promote, got %s", st.lastAction)
	}
	if atomic.LoadInt32(promoteCnt) != 1 {
		t.Fatalf("promote should be called once, got %d", *promoteCnt)
	}
	if atomic.LoadInt32(advanceCnt) != 0 || atomic.LoadInt32(rollbackCnt) != 0 {
		t.Fatalf("advance/rollback should not be called")
	}
	// 验证状态已晋级。
	updated, _ := deploys.Get(ctx, deployID, "t1")
	if updated.Status != StatusPromoting {
		t.Fatalf("status should be promoting, got %s", updated.Status)
	}
}

// =============================================================================
// Monitor / Stop / Status 生命周期
// =============================================================================

func TestAutoAdvanceManager_Monitor_AlreadyMonitoring(t *testing.T) {
	ctx := context.Background()
	deploys := NewMemory()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	disp.devices["dev-2"] = &proto.DeviceInfo{DeviceID: "dev-2", AgentID: "a2"}
	disp.devices["dev-3"] = &proto.DeviceInfo{DeviceID: "dev-3", AgentID: "a3"}
	disp.devices["dev-4"] = &proto.DeviceInfo{DeviceID: "dev-4", AgentID: "a4"}
	disp.devices["dev-5"] = &proto.DeviceInfo{DeviceID: "dev-5", AgentID: "a5"}
	tr := newFakeTaskResults()
	deployID := setupCanaryDeploy(t, deploys, disp, tr, 20)

	m, _, _, _ := newAutoAdvanceManagerForTest(t, deploys, tr)

	// 启动监控（异步）。
	monCtx, cancel := context.WithCancel(ctx)
	go m.Monitor(monCtx, deployID)
	// 等待注册。
	time.Sleep(100 * time.Millisecond)

	// 重复调用应返回 ErrAlreadyMonitoring。
	if err := m.Monitor(context.Background(), deployID); err != ErrAlreadyMonitoring {
		t.Fatalf("want ErrAlreadyMonitoring, got %v", err)
	}

	cancel()
	time.Sleep(100 * time.Millisecond)
}

func TestAutoAdvanceManager_Stop(t *testing.T) {
	ctx := context.Background()
	deploys := NewMemory()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	disp.devices["dev-2"] = &proto.DeviceInfo{DeviceID: "dev-2", AgentID: "a2"}
	disp.devices["dev-3"] = &proto.DeviceInfo{DeviceID: "dev-3", AgentID: "a3"}
	disp.devices["dev-4"] = &proto.DeviceInfo{DeviceID: "dev-4", AgentID: "a4"}
	disp.devices["dev-5"] = &proto.DeviceInfo{DeviceID: "dev-5", AgentID: "a5"}
	tr := newFakeTaskResults()
	deployID := setupCanaryDeploy(t, deploys, disp, tr, 20)

	m, _, _, _ := newAutoAdvanceManagerForTest(t, deploys, tr)

	// 未启动时 Stop 返回 false。
	if m.Stop(deployID) {
		t.Fatal("Stop on non-monitored deploy should return false")
	}

	// 启动监控。
	monCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go m.Monitor(monCtx, deployID)
	time.Sleep(100 * time.Millisecond)

	// 启动后 Stop 返回 true。
	if !m.Stop(deployID) {
		t.Fatal("Stop on monitored deploy should return true")
	}
	time.Sleep(100 * time.Millisecond)

	// Status 应显示未运行。
	if status := m.Status(deployID); status.Running {
		t.Fatal("status should show not running after stop")
	}
}

func TestAutoAdvanceManager_Status(t *testing.T) {
	ctx := context.Background()
	deploys := NewMemory()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	disp.devices["dev-2"] = &proto.DeviceInfo{DeviceID: "dev-2", AgentID: "a2"}
	disp.devices["dev-3"] = &proto.DeviceInfo{DeviceID: "dev-3", AgentID: "a3"}
	disp.devices["dev-4"] = &proto.DeviceInfo{DeviceID: "dev-4", AgentID: "a4"}
	disp.devices["dev-5"] = &proto.DeviceInfo{DeviceID: "dev-5", AgentID: "a5"}
	tr := newFakeTaskResults()
	deployID := setupCanaryDeploy(t, deploys, disp, tr, 20)

	// 设置达标结果。
	got, _ := deploys.Get(ctx, deployID, "t1")
	for _, tid := range SplitIDs(got.TaskIDs) {
		tr.setResult(tid, 0, 100)
	}

	m, _, _, _ := newAutoAdvanceManagerForTest(t, deploys, tr)

	// 未启动时 Status 返回 Running=false。
	if status := m.Status(deployID); status.Running {
		t.Fatal("status should show not running before monitor")
	}

	// 启动监控。
	monCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go m.Monitor(monCtx, deployID)
	time.Sleep(200 * time.Millisecond)

	// 启动后 Status 应显示运行中。
	status := m.Status(deployID)
	if !status.Running {
		t.Fatal("status should show running")
	}
	if status.StartedAt.IsZero() {
		t.Fatal("started_at should be set")
	}
}

// =============================================================================
// DefaultAutoAdvanceConfig
// =============================================================================

func TestDefaultAutoAdvanceConfig(t *testing.T) {
	cfg := DefaultAutoAdvanceConfig()
	if !cfg.Enabled {
		t.Fatal("default should be enabled")
	}
	if cfg.CheckInterval != 30*time.Second {
		t.Fatalf("default check interval should be 30s, got %s", cfg.CheckInterval)
	}
	if cfg.FailureRateThreshold != 0.05 {
		t.Fatalf("default failure rate threshold should be 0.05, got %f", cfg.FailureRateThreshold)
	}
	if cfg.LatencyThreshold != 500*time.Millisecond {
		t.Fatalf("default latency threshold should be 500ms, got %s", cfg.LatencyThreshold)
	}
	if cfg.MinSampleSize != 100 {
		t.Fatalf("default min sample size should be 100, got %d", cfg.MinSampleSize)
	}
	if cfg.AdvanceRatio != 0.1 {
		t.Fatalf("default advance ratio should be 0.1, got %f", cfg.AdvanceRatio)
	}
	if cfg.MaxRatio != 1.0 {
		t.Fatalf("default max ratio should be 1.0, got %f", cfg.MaxRatio)
	}
}
