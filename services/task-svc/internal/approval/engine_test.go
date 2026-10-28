package approval

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// testTime 是测试用的固定基准时间（UTC，避免依赖本机时区）。
func testTime() time.Time {
	return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
}

// fakeClock 是可推进的虚拟时钟，注入 Engine 的 WithNow 缝，
// 让超时/过期断言不依赖真实等待。
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: testTime()}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// actionRecorder 记录通知回调收到的动作序列。
type actionRecorder struct {
	mu  sync.Mutex
	got []string
}

func (r *actionRecorder) record(_ *ApprovalRequest, action string) {
	r.mu.Lock()
	r.got = append(r.got, action)
	r.mu.Unlock()
}

func (r *actionRecorder) actions() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.got...)
}

func newFlow(id string, steps ...ApprovalStep) *ApprovalFlow {
	return &ApprovalFlow{
		ID: id, Name: "测试流", TenantID: "t1", TriggerType: TriggerDeploy, Enabled: true, Steps: steps,
	}
}

func newStep(id string, order int, mode StepMode, timeout time.Duration, approvers ...string) ApprovalStep {
	return ApprovalStep{ID: id, Order: order, Mode: mode, Approvers: approvers, Timeout: timeout}
}

func newRequest(id, flowID string) *ApprovalRequest {
	return &ApprovalRequest{
		ID: id, FlowID: flowID, TenantID: "t1", TriggerType: TriggerDeploy, Operator: "op",
	}
}

func mustCreateFlow(t *testing.T, e *Engine, f *ApprovalFlow) {
	t.Helper()
	if err := e.CreateFlow(f); err != nil {
		t.Fatalf("CreateFlow(%s): %v", f.ID, err)
	}
}

func mustSubmit(t *testing.T, e *Engine, r *ApprovalRequest) {
	t.Helper()
	if err := e.Submit(r); err != nil {
		t.Fatalf("Submit(%s): %v", r.ID, err)
	}
}

func mustGetRequest(t *testing.T, e *Engine, id string) *ApprovalRequest {
	t.Helper()
	r, err := e.GetRequest(id)
	if err != nil {
		t.Fatalf("GetRequest(%s): %v", id, err)
	}
	return r
}

func TestCreateFlow_TimestampsAndDuplicate(t *testing.T) {
	clock := newFakeClock()
	e := New(WithNow(clock.Now))

	bad := newFlow("f-bad", newStep("s1", 1, StepAnyOf, 0, "u1"))
	bad.Name = ""
	if err := e.CreateFlow(bad); err == nil {
		t.Fatal("非法流定义应被拒绝")
	}

	f := newFlow("f1", newStep("s1", 1, StepAnyOf, 0, "u1"))
	mustCreateFlow(t, e, f)

	got, err := e.GetFlow("f1")
	if err != nil {
		t.Fatalf("GetFlow: %v", err)
	}
	if !got.CreatedAt.Equal(clock.Now()) || !got.UpdatedAt.Equal(clock.Now()) {
		t.Errorf("CreatedAt/UpdatedAt 未按注入时钟填充：%+v", got)
	}

	if err := e.CreateFlow(newFlow("f1", newStep("s1", 1, StepAnyOf, 0, "u1"))); !errors.Is(err, ErrFlowExists) {
		t.Fatalf("重复 ID 应返回 ErrFlowExists，得到 %v", err)
	}
}

func TestFlowCRUD_NotFoundAndUpdate(t *testing.T) {
	clock := newFakeClock()
	e := New(WithNow(clock.Now))

	if err := e.UpdateFlow(newFlow("nope", newStep("s1", 1, StepAnyOf, 0, "u1"))); !errors.Is(err, ErrFlowNotFound) {
		t.Errorf("UpdateFlow 不存在应返回 ErrFlowNotFound，得到 %v", err)
	}
	if err := e.DeleteFlow("nope"); !errors.Is(err, ErrFlowNotFound) {
		t.Errorf("DeleteFlow 不存在应返回 ErrFlowNotFound，得到 %v", err)
	}
	if _, err := e.GetFlow("nope"); !errors.Is(err, ErrFlowNotFound) {
		t.Errorf("GetFlow 不存在应返回 ErrFlowNotFound，得到 %v", err)
	}

	created := clock.Now()
	f := newFlow("f1", newStep("s1", 1, StepAnyOf, 0, "u1"))
	mustCreateFlow(t, e, f)

	clock.Advance(time.Minute)
	f.Name = "改名"
	if err := e.UpdateFlow(f); err != nil {
		t.Fatalf("UpdateFlow: %v", err)
	}
	got, _ := e.GetFlow("f1")
	if got.Name != "改名" {
		t.Errorf("UpdateFlow 未生效：Name=%q", got.Name)
	}
	if !got.CreatedAt.Equal(created) {
		t.Errorf("UpdateFlow 应保留 CreatedAt，得到 %v（期望 %v）", got.CreatedAt, created)
	}
	if !got.UpdatedAt.Equal(clock.Now()) {
		t.Errorf("UpdateFlow 应刷新 UpdatedAt，得到 %v", got.UpdatedAt)
	}

	if err := e.DeleteFlow("f1"); err != nil {
		t.Fatalf("DeleteFlow: %v", err)
	}
	if _, err := e.GetFlow("f1"); !errors.Is(err, ErrFlowNotFound) {
		t.Errorf("删除后 GetFlow 应返回 ErrFlowNotFound，得到 %v", err)
	}
}

func TestListFlows_TenantFilterAndOrder(t *testing.T) {
	e := New()
	fb := newFlow("f-b", newStep("s1", 1, StepAnyOf, 0, "u1"))
	fa := newFlow("f-a", newStep("s1", 1, StepAnyOf, 0, "u1"))
	fc := newFlow("f-c", newStep("s1", 1, StepAnyOf, 0, "u1"))
	fc.TenantID = "t2"
	for _, f := range []*ApprovalFlow{fb, fa, fc} {
		mustCreateFlow(t, e, f)
	}

	got := e.ListFlows("t1")
	if len(got) != 2 || got[0].ID != "f-a" || got[1].ID != "f-b" {
		t.Fatalf("ListFlows(t1) = %v，期望按 ID 升序 [f-a f-b]", idsOf(got))
	}
	if n := len(e.ListFlows("")); n != 3 {
		t.Errorf("ListFlows(\"\") 数量 = %d，期望 3", n)
	}
}

func idsOf(flows []*ApprovalFlow) []string {
	out := make([]string, len(flows))
	for i, f := range flows {
		out[i] = f.ID
	}
	return out
}

func TestSubmit_RejectsInvalid(t *testing.T) {
	e := New()
	mustCreateFlow(t, e, newFlow("f1", newStep("s1", 1, StepAnyOf, 0, "u1")))

	disabled := newFlow("f2", newStep("s1", 1, StepAnyOf, 0, "u1"))
	disabled.Enabled = false
	mustCreateFlow(t, e, disabled)

	missingOperator := newRequest("r1", "f1")
	missingOperator.Operator = ""
	if err := e.Submit(missingOperator); err == nil {
		t.Error("缺 Operator 应被 Validate 拒绝")
	}

	if err := e.Submit(newRequest("r1", "no-flow")); !errors.Is(err, ErrFlowNotFound) {
		t.Errorf("未知流应返回 ErrFlowNotFound，得到 %v", err)
	}
	if err := e.Submit(newRequest("r2", "f2")); err == nil {
		t.Error("停用的流应拒绝提交")
	}

	mustSubmit(t, e, newRequest("r3", "f1"))
	if err := e.Submit(newRequest("r3", "f1")); !errors.Is(err, ErrRequestExists) {
		t.Errorf("重复请求 ID 应返回 ErrRequestExists，得到 %v", err)
	}
}

func TestSubmit_InitializesState(t *testing.T) {
	clock := newFakeClock()
	rec := &actionRecorder{}
	e := New(WithNow(clock.Now), WithNotifier(rec.record))
	mustCreateFlow(t, e, newFlow("f1",
		newStep("s1", 1, StepAnyOf, 10*time.Minute, "u1"),
		newStep("s2", 2, StepSequential, 0, "u2", "u3"),
	))

	req := newRequest("r1", "f1")
	req.Status = StatusApproved // 调用方误设终态：Submit 必须按规则强制为 pending
	mustSubmit(t, e, req)

	got := mustGetRequest(t, e, "r1")
	if got.Status != StatusPending {
		t.Errorf("提交后 Status = %q，期望 pending", got.Status)
	}
	if got.CurrentStep != 1 {
		t.Errorf("CurrentStep = %d，期望 1", got.CurrentStep)
	}
	if len(got.Steps) != 2 {
		t.Fatalf("步骤快照数量 = %d，期望 2", len(got.Steps))
	}
	if !got.Steps[0].StartedAt.Equal(clock.Now()) {
		t.Errorf("第一步 StartedAt = %v，期望提交时刻 %v", got.Steps[0].StartedAt, clock.Now())
	}
	if !got.Steps[1].StartedAt.IsZero() {
		t.Errorf("第二步尚未开始，StartedAt 应为零值，得到 %v", got.Steps[1].StartedAt)
	}

	hist, err := e.GetHistory("r1")
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if len(hist.Timeline) != 1 || hist.Timeline[0].Action != HistorySubmit {
		t.Fatalf("提交历史 = %+v，期望单条 submit", hist.Timeline)
	}
	if hist.Timeline[0].UserID != "op" {
		t.Errorf("submit 历史 UserID = %q，期望发起人 op", hist.Timeline[0].UserID)
	}

	if got := rec.actions(); len(got) != 1 || got[0] != HistorySubmit {
		t.Errorf("通知动作 = %v，期望 [submit]", got)
	}
}

func TestApproveSequential_AdvancesSteps(t *testing.T) {
	clock := newFakeClock()
	rec := &actionRecorder{}
	e := New(WithNow(clock.Now), WithNotifier(rec.record))
	mustCreateFlow(t, e, newFlow("f1",
		newStep("s1", 1, StepSequential, 0, "u1", "u2"),
		newStep("s2", 2, StepSequential, 0, "u3"),
	))
	mustSubmit(t, e, newRequest("r1", "f1"))

	if err := e.Approve("r1", "u2", ""); !errors.Is(err, ErrOutOfOrder) {
		t.Fatalf("第二位审批人越序审批应返回 ErrOutOfOrder，得到 %v", err)
	}
	if err := e.Approve("r1", "stranger", ""); !errors.Is(err, ErrNotApprover) {
		t.Fatalf("非审批人应返回 ErrNotApprover，得到 %v", err)
	}
	if err := e.Approve("no-such-request", "u1", ""); !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("未知请求应返回 ErrRequestNotFound，得到 %v", err)
	}

	clock.Advance(time.Minute)
	if err := e.Approve("r1", "u1", "第一级同意"); err != nil {
		t.Fatalf("u1 审批失败：%v", err)
	}
	got := mustGetRequest(t, e, "r1")
	if got.Status != StatusPending {
		t.Errorf("第一级尚未批完整体状态应为 pending，得到 %q", got.Status)
	}
	if got.CurrentStep != 1 {
		t.Errorf("sequential 第一人通过后仍应停在本步，CurrentStep = %d", got.CurrentStep)
	}
	if got.Steps[0].Status != StatusPending {
		t.Errorf("步骤状态 = %q，期望仍 pending（还有 u2 未批）", got.Steps[0].Status)
	}
	if len(got.Steps[0].Decisions) != 1 || got.Steps[0].Decisions[0].UserID != "u1" {
		t.Errorf("第一步决策记录 = %+v", got.Steps[0].Decisions)
	}
	if !got.Steps[1].StartedAt.IsZero() {
		t.Errorf("尚未推进到第二步，其 StartedAt 应为零值")
	}

	clock.Advance(time.Minute)
	if err := e.Approve("r1", "u2", "第二级同意"); err != nil {
		t.Fatalf("u2 审批失败：%v", err)
	}
	got = mustGetRequest(t, e, "r1")
	if got.Status != StatusPending {
		t.Errorf("第一级通过后整体状态应为 pending，得到 %q", got.Status)
	}
	if got.CurrentStep != 2 {
		t.Errorf("第一级通过后 CurrentStep = %d，期望 2", got.CurrentStep)
	}
	if got.Steps[0].Status != StatusApproved {
		t.Errorf("第一步状态 = %q，期望 approved", got.Steps[0].Status)
	}
	if !got.Steps[1].StartedAt.Equal(clock.Now()) {
		t.Errorf("推进后第二步 StartedAt = %v，期望推进时刻 %v", got.Steps[1].StartedAt, clock.Now())
	}

	hist, _ := e.GetHistory("r1")
	last, ok := hist.Last()
	if !ok || last.Action != HistoryStepAdvance || last.StepID != "s1" {
		t.Errorf("推进后历史末条 = (%+v, %v)，期望 step_advance/s1", last, ok)
	}

	if err := e.Approve("r1", "u3", ""); err != nil {
		t.Fatalf("u3 终审失败：%v", err)
	}
	got = mustGetRequest(t, e, "r1")
	if got.Status != StatusApproved {
		t.Errorf("终审后状态 = %q，期望 approved", got.Status)
	}
	if got.CurrentStep != 3 { // LastOrder + 1
		t.Errorf("终审后 CurrentStep = %d，期望 3（越界标记完成）", got.CurrentStep)
	}
	if err := e.Approve("r1", "u3", ""); !errors.Is(err, ErrNotPending) {
		t.Errorf("终态再审批应返回 ErrNotPending，得到 %v", err)
	}

	wantActions := []string{HistorySubmit, HistoryApprove, HistoryStepAdvance, HistoryApprove}
	if got := rec.actions(); strings.Join(got, ",") != strings.Join(wantActions, ",") {
		t.Errorf("通知动作 = %v，期望 %v", got, wantActions)
	}
}

func TestApproveCountersign_AllMustDecide(t *testing.T) {
	e := New()
	mustCreateFlow(t, e, newFlow("f1", newStep("s1", 1, StepCountersign, 0, "u1", "u2")))
	mustSubmit(t, e, newRequest("r1", "f1"))

	if err := e.Approve("r1", "u1", ""); err != nil {
		t.Fatalf("u1 审批失败：%v", err)
	}
	if got := mustGetRequest(t, e, "r1"); got.Status != StatusPending {
		t.Errorf("会签仅一人同意时状态 = %q，期望仍 pending", got.Status)
	}
	if err := e.Approve("r1", "u1", ""); !errors.Is(err, ErrStepAlreadyDecided) {
		t.Fatalf("同一步骤重复决策应返回 ErrStepAlreadyDecided，得到 %v", err)
	}
	if err := e.Approve("r1", "u2", ""); err != nil {
		t.Fatalf("u2 审批失败：%v", err)
	}
	if got := mustGetRequest(t, e, "r1"); got.Status != StatusApproved {
		t.Errorf("会签全员同意后状态 = %q，期望 approved", got.Status)
	}
}

func TestApproveAnyOf_FirstDecisionWins(t *testing.T) {
	t.Run("任一人同意即通过", func(t *testing.T) {
		e := New()
		mustCreateFlow(t, e, newFlow("f1", newStep("s1", 1, StepAnyOf, 0, "u1", "u2")))
		mustSubmit(t, e, newRequest("r1", "f1"))

		if err := e.Approve("r1", "u2", ""); err != nil {
			t.Fatalf("u2 审批失败：%v", err)
		}
		if got := mustGetRequest(t, e, "r1"); got.Status != StatusApproved {
			t.Errorf("或签一人同意后状态 = %q，期望 approved", got.Status)
		}
	})

	t.Run("任一人拒绝即拒绝", func(t *testing.T) {
		e := New()
		mustCreateFlow(t, e, newFlow("f1", newStep("s1", 1, StepAnyOf, 0, "u1", "u2")))
		mustSubmit(t, e, newRequest("r1", "f1"))

		if err := e.Reject("r1", "u1", "不合规"); err != nil {
			t.Fatalf("u1 拒绝失败：%v", err)
		}
		got := mustGetRequest(t, e, "r1")
		if got.Status != StatusRejected {
			t.Errorf("或签一人拒绝后状态 = %q，期望 rejected", got.Status)
		}
		if got.Steps[0].Status != StatusRejected {
			t.Errorf("步骤状态 = %q，期望 rejected", got.Steps[0].Status)
		}
	})
}

func TestReject_TerminatesRequest(t *testing.T) {
	e := New()
	mustCreateFlow(t, e, newFlow("f1",
		newStep("s1", 1, StepSequential, 0, "u1"),
		newStep("s2", 2, StepSequential, 0, "u2"),
	))
	mustSubmit(t, e, newRequest("r1", "f1"))

	if err := e.Reject("r1", "u1", "风险过高"); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	got := mustGetRequest(t, e, "r1")
	if got.Status != StatusRejected {
		t.Errorf("拒绝后状态 = %q，期望 rejected", got.Status)
	}
	if got.CurrentStep != 1 {
		t.Errorf("拒绝后 CurrentStep = %d，期望停在 1", got.CurrentStep)
	}
	hist, _ := e.GetHistory("r1")
	last, _ := hist.Last()
	if last.Action != HistoryReject || last.StepID != "s1" || last.Comment != "风险过高" {
		t.Errorf("拒绝历史末条 = %+v", last)
	}
	if err := e.Approve("r1", "u2", ""); !errors.Is(err, ErrNotPending) {
		t.Errorf("拒绝后继续审批应返回 ErrNotPending，得到 %v", err)
	}
}

func TestCancel(t *testing.T) {
	e := New()
	mustCreateFlow(t, e, newFlow("f1", newStep("s1", 1, StepAnyOf, 0, "u1")))
	mustSubmit(t, e, newRequest("r1", "f1"))

	if err := e.Cancel("r1", "op"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	got := mustGetRequest(t, e, "r1")
	if got.Status != StatusCancelled {
		t.Errorf("取消后状态 = %q，期望 cancelled", got.Status)
	}
	hist, _ := e.GetHistory("r1")
	last, _ := hist.Last()
	if last.Action != HistoryCancel || last.UserID != "op" {
		t.Errorf("取消历史末条 = %+v", last)
	}

	if err := e.Cancel("r1", "op"); !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("重复取消应返回 ErrInvalidTransition，得到 %v", err)
	}
	if err := e.Approve("r1", "u1", ""); !errors.Is(err, ErrNotPending) {
		t.Errorf("取消后审批应返回 ErrNotPending，得到 %v", err)
	}
	if err := e.Cancel("no-such-request", "op"); !errors.Is(err, ErrRequestNotFound) {
		t.Errorf("未知请求应返回 ErrRequestNotFound，得到 %v", err)
	}
}

func TestCheckTimeout_StepTimeout(t *testing.T) {
	clock := newFakeClock()
	e := New(WithNow(clock.Now))
	mustCreateFlow(t, e, newFlow("f1", newStep("s1", 1, StepAnyOf, 10*time.Minute, "u1")))
	mustSubmit(t, e, newRequest("r1", "f1"))

	clock.Advance(10 * time.Minute) // 恰好到点：严格大于才算超时
	if timedOut, err := e.CheckTimeout("r1"); err != nil || timedOut {
		t.Fatalf("恰好到超时点应不触发，得到 (%v, %v)", timedOut, err)
	}

	clock.Advance(time.Nanosecond)
	timedOut, err := e.CheckTimeout("r1")
	if err != nil || !timedOut {
		t.Fatalf("超过超时点应触发，得到 (%v, %v)", timedOut, err)
	}
	got := mustGetRequest(t, e, "r1")
	if got.Status != StatusRejected {
		t.Errorf("步骤超时后整体状态 = %q，期望 rejected（步骤超时按拒绝处理）", got.Status)
	}
	if got.Steps[0].Status != StatusRejected {
		t.Errorf("步骤状态 = %q，期望 rejected", got.Steps[0].Status)
	}
	hist, _ := e.GetHistory("r1")
	last, _ := hist.Last()
	if last.Action != HistoryTimeout || last.StepID != "s1" {
		t.Errorf("超时历史末条 = %+v，期望 timeout/s1", last)
	}

	if timedOut, err := e.CheckTimeout("r1"); err != nil || timedOut {
		t.Errorf("终态请求不应再次触发超时，得到 (%v, %v)", timedOut, err)
	}
}

func TestCheckTimeout_OverallExpiry(t *testing.T) {
	clock := newFakeClock()
	e := New(WithNow(clock.Now))
	mustCreateFlow(t, e, newFlow("f1", newStep("s1", 1, StepAnyOf, 0, "u1")))

	req := newRequest("r1", "f1")
	req.ExpireAt = clock.Now().Add(5 * time.Minute)
	mustSubmit(t, e, req)

	clock.Advance(6 * time.Minute)
	timedOut, err := e.CheckTimeout("r1")
	if err != nil || !timedOut {
		t.Fatalf("整体过期应触发超时，( %v, %v)", timedOut, err)
	}
	got := mustGetRequest(t, e, "r1")
	if got.Status != StatusTimeout {
		t.Errorf("整体过期后状态 = %q，期望 timeout", got.Status)
	}
	hist, _ := e.GetHistory("r1")
	last, _ := hist.Last()
	if last.Action != HistoryTimeout || last.StepID != "" {
		t.Errorf("整体过期历史末条 = %+v，期望 timeout 且无 StepID", last)
	}
}

func TestCheckTimeout_NotFoundAndNoFlowCase(t *testing.T) {
	e := New()
	if _, err := e.CheckTimeout("no-such-request"); !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("未知请求应返回 ErrRequestNotFound，得到 %v", err)
	}
}

func TestApprove_ExpiredRequest(t *testing.T) {
	clock := newFakeClock()
	e := New(WithNow(clock.Now))
	mustCreateFlow(t, e, newFlow("f1", newStep("s1", 1, StepAnyOf, 0, "u1")))

	req := newRequest("r1", "f1")
	req.ExpireAt = clock.Now().Add(time.Minute)
	mustSubmit(t, e, req)

	clock.Advance(2 * time.Minute)
	if err := e.Approve("r1", "u1", ""); !errors.Is(err, ErrRequestExpired) {
		t.Fatalf("过期请求审批应返回 ErrRequestExpired，得到 %v", err)
	}
	if got := mustGetRequest(t, e, "r1"); got.Status != StatusPending {
		t.Errorf("过期但未 CheckTimeout 的请求仍应保持 pending，得到 %q", got.Status)
	}
}

func TestListRequests_FilterAndOrder(t *testing.T) {
	clock := newFakeClock()
	e := New(WithNow(clock.Now))
	mustCreateFlow(t, e, newFlow("f1", newStep("s1", 1, StepAnyOf, 0, "u1")))

	r1 := newRequest("r1", "f1")
	mustSubmit(t, e, r1)
	clock.Advance(time.Second)
	r2 := newRequest("r2", "f1")
	mustSubmit(t, e, r2)
	clock.Advance(time.Second)
	r3 := newRequest("r3", "f1")
	r3.TenantID = "t2"
	mustSubmit(t, e, r3)

	got := e.ListRequests("", "")
	if len(got) != 3 || got[0].ID != "r3" || got[1].ID != "r2" || got[2].ID != "r1" {
		t.Fatalf("ListRequests 全量应按 CreatedAt 降序，得到 %v", requestIDs(got))
	}
	if got := e.ListRequests("t1", ""); len(got) != 2 {
		t.Errorf("租户过滤数量 = %d，期望 2", len(got))
	}

	if err := e.Approve("r1", "u1", ""); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if got := e.ListRequests("t1", string(StatusApproved)); len(got) != 1 || got[0].ID != "r1" {
		t.Errorf("状态过滤应只返回 r1，得到 %v", requestIDs(got))
	}
	if got := e.ListRequests("", string(StatusTimeout)); len(got) != 0 {
		t.Errorf("无匹配状态应为空，得到 %v", requestIDs(got))
	}
}

func requestIDs(rs []*ApprovalRequest) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.ID
	}
	return out
}

func TestListPendingApprovals_SequentialVisibility(t *testing.T) {
	clock := newFakeClock()
	e := New(WithNow(clock.Now))
	mustCreateFlow(t, e, newFlow("f1", newStep("s1", 1, StepSequential, 0, "u1", "u2")))

	mustSubmit(t, e, newRequest("r1", "f1"))
	clock.Advance(time.Second)
	mustSubmit(t, e, newRequest("r2", "f1"))

	if got := e.ListPendingApprovals("u1"); len(got) != 2 || got[0].ID != "r1" || got[1].ID != "r2" {
		t.Fatalf("u1 待办应按先提交先审批，得到 %v", requestIDs(got))
	}
	if got := e.ListPendingApprovals("u2"); len(got) != 0 {
		t.Errorf("sequential 未轮到 u2，待办应为空，得到 %v", requestIDs(got))
	}

	if err := e.Approve("r1", "u1", ""); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if got := e.ListPendingApprovals("u1"); len(got) != 1 || got[0].ID != "r2" {
		t.Errorf("u1 已批 r1，待办应剩 r2，得到 %v", requestIDs(got))
	}
	if got := e.ListPendingApprovals("u2"); len(got) != 1 || got[0].ID != "r1" {
		t.Errorf("轮到 u2 时待办应为 r1，得到 %v", requestIDs(got))
	}

	if err := e.Approve("r1", "u2", ""); err != nil {
		t.Fatalf("u2 Approve: %v", err)
	}
	if got := e.ListPendingApprovals("u2"); len(got) != 0 {
		t.Errorf("r1 已通过，不应再出现在待办，得到 %v", requestIDs(got))
	}
}

func TestGetRequest_ReturnsCopy(t *testing.T) {
	e := New()
	mustCreateFlow(t, e, newFlow("f1", newStep("s1", 1, StepAnyOf, 0, "u1")))
	mustSubmit(t, e, newRequest("r1", "f1"))

	got := mustGetRequest(t, e, "r1")
	got.Status = StatusApproved
	got.Steps[0].Status = StatusRejected

	again := mustGetRequest(t, e, "r1")
	if again.Status != StatusPending || again.Steps[0].Status != StatusPending {
		t.Error("GetRequest 返回值被调用方修改后影响了引擎内部状态（应返回深拷贝）")
	}
}

func TestGetHistory_ReturnsCopyAndNotFound(t *testing.T) {
	e := New()
	if _, err := e.GetHistory("no-such-request"); !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("未知请求应返回 ErrRequestNotFound，得到 %v", err)
	}

	mustCreateFlow(t, e, newFlow("f1", newStep("s1", 1, StepAnyOf, 0, "u1")))
	mustSubmit(t, e, newRequest("r1", "f1"))

	hist, err := e.GetHistory("r1")
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	hist.Timeline[0].Action = "tampered"

	again, _ := e.GetHistory("r1")
	if again.Timeline[0].Action != HistorySubmit {
		t.Error("GetHistory 返回值被调用方修改后影响了引擎内部历史（应返回深拷贝）")
	}
}

// TestNotifier_CanCallEngine 钉住"回调在锁外执行"的契约：
// 通知函数里再次调用 Engine 方法（常见做法是查详情后推送）不能死锁。
func TestNotifier_CanCallEngine(t *testing.T) {
	var (
		e      *Engine
		called int
		mu     sync.Mutex
	)
	e = New(WithNotifier(func(req *ApprovalRequest, _ string) {
		// 锁内回调的典型死锁点：GetRequest / GetHistory / ListFlows 都需要读锁。
		if _, err := e.GetRequest(req.ID); err != nil {
			t.Errorf("回调内 GetRequest 失败：%v", err)
		}
		e.ListFlows("t1")
		mu.Lock()
		called++
		mu.Unlock()
	}))
	mustCreateFlow(t, e, newFlow("f1", newStep("s1", 1, StepAnyOf, 0, "u1")))
	mustSubmit(t, e, newRequest("r1", "f1"))

	mu.Lock()
	defer mu.Unlock()
	if called != 1 {
		t.Fatalf("回调调用次数 = %d，期望 1", called)
	}
}

func TestConcurrentApprovals(t *testing.T) {
	const approvers = 8
	names := make([]string, approvers)
	for i := range names {
		names[i] = "u" + string(rune('a'+i))
	}

	e := New()
	mustCreateFlow(t, e, newFlow("f1", newStep("s1", 1, StepCountersign, 0, names...)))
	mustSubmit(t, e, newRequest("r1", "f1"))

	var wg sync.WaitGroup
	errs := make([]error, approvers)
	wg.Add(approvers)
	for i, u := range names {
		go func(i int, u string) {
			defer wg.Done()
			errs[i] = e.Approve("r1", u, "")
		}(i, u)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("并发审批 %s 失败：%v", names[i], err)
		}
	}
	got := mustGetRequest(t, e, "r1")
	if got.Status != StatusApproved {
		t.Fatalf("会签全员并发同意后状态 = %q，期望 approved", got.Status)
	}
	if n := len(got.Steps[0].Decisions); n != approvers {
		t.Errorf("决策条数 = %d，期望 %d（并发下不应丢决策）", n, approvers)
	}
}
