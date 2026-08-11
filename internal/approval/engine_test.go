package approval

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// newTestEngine 构造带虚拟时钟的引擎，返回引擎与可推进的时钟闭包。
func newTestEngine() (*Engine, func(time.Duration)) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := t0
	var mu sync.Mutex
	e := New(WithNow(func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}))
	advance := func(d time.Duration) {
		mu.Lock()
		now = now.Add(d)
		mu.Unlock()
	}
	return e, advance
}

// newFlow 构造单步骤审批流。
func newFlow(id, tenant, trigger string, mode StepMode, approvers ...string) *ApprovalFlow {
	return &ApprovalFlow{
		ID:          id,
		Name:        id,
		TenantID:    tenant,
		TriggerType: trigger,
		Steps: []ApprovalStep{
			{ID: "s1", Name: "s1", Order: 1, Mode: mode, Approvers: approvers},
		},
		Enabled: true,
	}
}

// newMultiStepFlow 构造两步骤流，步骤1 mode1，步骤2 mode2。
func newMultiStepFlow(id, tenant, trigger string, mode1 StepMode, approvers1 []string, mode2 StepMode, approvers2 []string) *ApprovalFlow {
	return &ApprovalFlow{
		ID:          id,
		Name:        id,
		TenantID:    tenant,
		TriggerType: trigger,
		Steps: []ApprovalStep{
			{ID: "s1", Name: "s1", Order: 1, Mode: mode1, Approvers: approvers1},
			{ID: "s2", Name: "s2", Order: 2, Mode: mode2, Approvers: approvers2},
		},
		Enabled: true,
	}
}

func newRequest(id, flowID, tenant, trigger, operator string) *ApprovalRequest {
	return &ApprovalRequest{
		ID:          id,
		FlowID:      flowID,
		TenantID:    tenant,
		TriggerType: trigger,
		Operator:    operator,
		Target:      "host-1",
		Detail:      "restart nginx",
		Risk:        RiskHigh,
	}
}

// ========== 审批流 CRUD ==========

func TestCreateFlowAndGet(t *testing.T) {
	e, _ := newTestEngine()
	f := newFlow("f1", "t1", TriggerShell, StepAnyOf, "alice")
	if err := e.CreateFlow(f); err != nil {
		t.Fatalf("CreateFlow: %v", err)
	}
	got, err := e.GetFlow("f1")
	if err != nil {
		t.Fatalf("GetFlow: %v", err)
	}
	if got.ID != "f1" || len(got.Steps) != 1 {
		t.Errorf("GetFlow returned %+v", got)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}

	// 重复 ID。
	if err := e.CreateFlow(f); err != ErrFlowExists {
		t.Errorf("duplicate CreateFlow: %v want %v", err, ErrFlowExists)
	}

	// 不存在。
	if _, err := e.GetFlow("nope"); err != ErrFlowNotFound {
		t.Errorf("GetFlow missing: %v want %v", err, ErrFlowNotFound)
	}
}

func TestCreateFlowInvalid(t *testing.T) {
	e, _ := newTestEngine()
	bad := newFlow("", "t1", TriggerShell, StepAnyOf, "alice")
	if err := e.CreateFlow(bad); err == nil {
		t.Error("invalid flow should be rejected")
	}
}

func TestUpdateFlow(t *testing.T) {
	e, _ := newTestEngine()
	f := newFlow("f1", "t1", TriggerShell, StepAnyOf, "alice")
	if err := e.CreateFlow(f); err != nil {
		t.Fatalf("CreateFlow: %v", err)
	}
	orig, _ := e.GetFlow("f1")

	f.Name = "updated"
	f.Steps[0].Approvers = []string{"alice", "bob"}
	if err := e.UpdateFlow(f); err != nil {
		t.Fatalf("UpdateFlow: %v", err)
	}
	got, _ := e.GetFlow("f1")
	if got.Name != "updated" {
		t.Errorf("Name = %q", got.Name)
	}
	if len(got.Steps[0].Approvers) != 2 {
		t.Errorf("Approvers len = %d", len(got.Steps[0].Approvers))
	}
	if !got.CreatedAt.Equal(orig.CreatedAt) {
		t.Error("CreatedAt should be preserved")
	}

	// 不存在。
	if err := e.UpdateFlow(newFlow("nope", "t1", TriggerShell, StepAnyOf, "alice")); err != ErrFlowNotFound {
		t.Errorf("UpdateFlow missing: %v want %v", err, ErrFlowNotFound)
	}
}

func TestDeleteFlow(t *testing.T) {
	e, _ := newTestEngine()
	f := newFlow("f1", "t1", TriggerShell, StepAnyOf, "alice")
	_ = e.CreateFlow(f)
	if err := e.DeleteFlow("f1"); err != nil {
		t.Fatalf("DeleteFlow: %v", err)
	}
	if _, err := e.GetFlow("f1"); err != ErrFlowNotFound {
		t.Errorf("after delete: %v want %v", err, ErrFlowNotFound)
	}
	if err := e.DeleteFlow("f1"); err != ErrFlowNotFound {
		t.Errorf("delete missing: %v want %v", err, ErrFlowNotFound)
	}
}

func TestListFlows(t *testing.T) {
	e, _ := newTestEngine()
	_ = e.CreateFlow(newFlow("f2", "t1", TriggerShell, StepAnyOf, "alice"))
	_ = e.CreateFlow(newFlow("f1", "t1", TriggerBatchRestart, StepAnyOf, "alice"))
	_ = e.CreateFlow(newFlow("f3", "t2", TriggerShell, StepAnyOf, "alice"))

	all := e.ListFlows("")
	if len(all) != 3 {
		t.Fatalf("ListFlows all = %d want 3", len(all))
	}
	if all[0].ID != "f1" || all[1].ID != "f2" || all[2].ID != "f3" {
		t.Errorf("ListFlows not sorted: %v %v %v", all[0].ID, all[1].ID, all[2].ID)
	}

	t1 := e.ListFlows("t1")
	if len(t1) != 2 {
		t.Errorf("ListFlows t1 = %d want 2", len(t1))
	}
}

// ========== 提交 / 审批 / 拒绝 / 取消 ==========

func TestSubmitAndApproveAnyOf(t *testing.T) {
	e, _ := newTestEngine()
	_ = e.CreateFlow(newFlow("f1", "t1", TriggerShell, StepAnyOf, "alice", "bob"))

	req := newRequest("r1", "f1", "t1", TriggerShell, "ops")
	if err := e.Submit(req); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	got, _ := e.GetRequest("r1")
	if got.Status != StatusPending {
		t.Errorf("after submit status = %q want pending", got.Status)
	}
	if got.CurrentStep != 1 {
		t.Errorf("CurrentStep = %d want 1", got.CurrentStep)
	}

	// alice 同意 → anyof 立即通过。
	if err := e.Approve("r1", "alice", "ok"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	got, _ = e.GetRequest("r1")
	if got.Status != StatusApproved {
		t.Errorf("after approve status = %q want approved", got.Status)
	}
	if got.CurrentStep != 2 { // 越界标记已完成
		t.Errorf("CurrentStep = %d want 2", got.CurrentStep)
	}
	if got.Steps[0].Status != StatusApproved {
		t.Errorf("step status = %q want approved", got.Steps[0].Status)
	}
}

func TestSubmitDuplicate(t *testing.T) {
	e, _ := newTestEngine()
	_ = e.CreateFlow(newFlow("f1", "t1", TriggerShell, StepAnyOf, "alice"))
	req := newRequest("r1", "f1", "t1", TriggerShell, "ops")
	_ = e.Submit(req)
	if err := e.Submit(req); err != ErrRequestExists {
		t.Errorf("duplicate submit: %v want %v", err, ErrRequestExists)
	}
}

func TestSubmitFlowNotFound(t *testing.T) {
	e, _ := newTestEngine()
	req := newRequest("r1", "nope", "t1", TriggerShell, "ops")
	if err := e.Submit(req); err != ErrFlowNotFound {
		t.Errorf("submit missing flow: %v want %v", err, ErrFlowNotFound)
	}
}

func TestSubmitDisabledFlow(t *testing.T) {
	e, _ := newTestEngine()
	f := newFlow("f1", "t1", TriggerShell, StepAnyOf, "alice")
	f.Enabled = false
	_ = e.CreateFlow(f)
	req := newRequest("r1", "f1", "t1", TriggerShell, "ops")
	if err := e.Submit(req); err == nil {
		t.Error("submit disabled flow should fail")
	}
}

func TestRejectAnyOf(t *testing.T) {
	e, _ := newTestEngine()
	_ = e.CreateFlow(newFlow("f1", "t1", TriggerShell, StepAnyOf, "alice", "bob"))
	_ = e.Submit(newRequest("r1", "f1", "t1", TriggerShell, "ops"))

	if err := e.Reject("r1", "alice", "no"); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	got, _ := e.GetRequest("r1")
	if got.Status != StatusRejected {
		t.Errorf("status = %q want rejected", got.Status)
	}
}

func TestCancel(t *testing.T) {
	e, _ := newTestEngine()
	_ = e.CreateFlow(newFlow("f1", "t1", TriggerShell, StepAnyOf, "alice"))
	_ = e.Submit(newRequest("r1", "f1", "t1", TriggerShell, "ops"))

	if err := e.Cancel("r1", "ops"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	got, _ := e.GetRequest("r1")
	if got.Status != StatusCancelled {
		t.Errorf("status = %q want cancelled", got.Status)
	}

	// 已取消再取消：非法转换。
	if err := e.Cancel("r1", "ops"); err != ErrInvalidTransition {
		t.Errorf("cancel again: %v want %v", err, ErrInvalidTransition)
	}
}

func TestApproveNotApprover(t *testing.T) {
	e, _ := newTestEngine()
	_ = e.CreateFlow(newFlow("f1", "t1", TriggerShell, StepAnyOf, "alice"))
	_ = e.Submit(newRequest("r1", "f1", "t1", TriggerShell, "ops"))
	if err := e.Approve("r1", "carol", "ok"); err != ErrNotApprover {
		t.Errorf("approve by non-approver: %v want %v", err, ErrNotApprover)
	}
}

func TestApproveAlreadyDecided(t *testing.T) {
	e, _ := newTestEngine()
	_ = e.CreateFlow(newFlow("f1", "t1", TriggerShell, StepCountersign, "alice", "bob"))
	_ = e.Submit(newRequest("r1", "f1", "t1", TriggerShell, "ops"))
	_ = e.Approve("r1", "alice", "ok")
	if err := e.Approve("r1", "alice", "again"); err != ErrStepAlreadyDecided {
		t.Errorf("re-approve: %v want %v", err, ErrStepAlreadyDecided)
	}
}

func TestApproveAfterTerminal(t *testing.T) {
	e, _ := newTestEngine()
	_ = e.CreateFlow(newFlow("f1", "t1", TriggerShell, StepAnyOf, "alice", "bob"))
	_ = e.Submit(newRequest("r1", "f1", "t1", TriggerShell, "ops"))
	_ = e.Approve("r1", "alice", "ok") // 终态
	if err := e.Approve("r1", "bob", "late"); err != ErrNotPending {
		t.Errorf("approve after terminal: %v want %v", err, ErrNotPending)
	}
}

// ========== 多级审批 ==========

func TestMultiStepSequentialApprove(t *testing.T) {
	e, _ := newTestEngine()
	// 步骤1: sequential [alice, bob]；步骤2: anyof [carol]
	f := newMultiStepFlow("f1", "t1", TriggerDeploy, StepSequential, []string{"alice", "bob"}, StepAnyOf, []string{"carol"})
	_ = e.CreateFlow(f)
	_ = e.Submit(newRequest("r1", "f1", "t1", TriggerDeploy, "ops"))

	// 顺序错误：bob 不能先。
	if err := e.Approve("r1", "bob", ""); err != ErrOutOfOrder {
		t.Errorf("bob first: %v want %v", err, ErrOutOfOrder)
	}
	// alice 同意。
	if err := e.Approve("r1", "alice", ""); err != nil {
		t.Fatalf("alice approve: %v", err)
	}
	got, _ := e.GetRequest("r1")
	if got.Status != StatusPending || got.CurrentStep != 1 {
		t.Errorf("after alice: status=%q step=%d", got.Status, got.CurrentStep)
	}
	// bob 同意 → 步骤1 完成，推进到步骤2。
	if err := e.Approve("r1", "bob", ""); err != nil {
		t.Fatalf("bob approve: %v", err)
	}
	got, _ = e.GetRequest("r1")
	if got.Status != StatusPending || got.CurrentStep != 2 {
		t.Errorf("after bob: status=%q step=%d want pending/2", got.Status, got.CurrentStep)
	}
	// carol 同意 → 整体通过。
	if err := e.Approve("r1", "carol", ""); err != nil {
		t.Fatalf("carol approve: %v", err)
	}
	got, _ = e.GetRequest("r1")
	if got.Status != StatusApproved {
		t.Errorf("final status=%q want approved", got.Status)
	}
}

func TestMultiStepRejectAtStep1(t *testing.T) {
	e, _ := newTestEngine()
	f := newMultiStepFlow("f1", "t1", TriggerDeploy, StepSequential, []string{"alice"}, StepAnyOf, []string{"bob"})
	_ = e.CreateFlow(f)
	_ = e.Submit(newRequest("r1", "f1", "t1", TriggerDeploy, "ops"))

	if err := e.Reject("r1", "alice", "no"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	got, _ := e.GetRequest("r1")
	if got.Status != StatusRejected {
		t.Errorf("status=%q want rejected", got.Status)
	}
	if got.CurrentStep != 1 {
		t.Errorf("CurrentStep=%d want 1 (no advance on reject)", got.CurrentStep)
	}
}

func TestCountersignAllApprove(t *testing.T) {
	e, _ := newTestEngine()
	_ = e.CreateFlow(newFlow("f1", "t1", TriggerK8sDelete, StepCountersign, "alice", "bob", "carol"))
	_ = e.Submit(newRequest("r1", "f1", "t1", TriggerK8sDelete, "ops"))

	_ = e.Approve("r1", "carol", "")
	_ = e.Approve("r1", "alice", "")
	got, _ := e.GetRequest("r1")
	if got.Status != StatusPending {
		t.Errorf("after 2/3 approves status=%q want pending", got.Status)
	}
	_ = e.Approve("r1", "bob", "")
	got, _ = e.GetRequest("r1")
	if got.Status != StatusApproved {
		t.Errorf("after 3/3 approves status=%q want approved", got.Status)
	}
}

func TestCountersignOneReject(t *testing.T) {
	e, _ := newTestEngine()
	_ = e.CreateFlow(newFlow("f1", "t1", TriggerK8sDelete, StepCountersign, "alice", "bob"))
	_ = e.Submit(newRequest("r1", "f1", "t1", TriggerK8sDelete, "ops"))

	_ = e.Approve("r1", "alice", "")
	if err := e.Reject("r1", "bob", "no"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	got, _ := e.GetRequest("r1")
	if got.Status != StatusRejected {
		t.Errorf("status=%q want rejected", got.Status)
	}
}

func TestAnyOfApproveWins(t *testing.T) {
	e, _ := newTestEngine()
	_ = e.CreateFlow(newFlow("f1", "t1", TriggerShell, StepAnyOf, "alice", "bob"))
	_ = e.Submit(newRequest("r1", "f1", "t1", TriggerShell, "ops"))

	// alice 同意先到 → 通过；bob 后续决策应被拒绝（已终态）。
	if err := e.Approve("r1", "alice", ""); err != nil {
		t.Fatalf("alice approve: %v", err)
	}
	if err := e.Approve("r1", "bob", "late"); err != ErrNotPending {
		t.Errorf("bob after terminal: %v want %v", err, ErrNotPending)
	}
}

// ========== 查询 ==========

func TestListRequests(t *testing.T) {
	e, advance := newTestEngine()
	_ = e.CreateFlow(newFlow("f1", "t1", TriggerShell, StepAnyOf, "alice"))
	_ = e.CreateFlow(newFlow("f2", "t2", TriggerShell, StepAnyOf, "alice"))

	_ = e.Submit(newRequest("r1", "f1", "t1", TriggerShell, "ops"))
	advance(time.Second)
	_ = e.Submit(newRequest("r2", "f1", "t1", TriggerShell, "ops"))
	advance(time.Second)
	_ = e.Submit(newRequest("r3", "f2", "t2", TriggerShell, "ops"))
	_ = e.Approve("r1", "alice", "")

	all := e.ListRequests("", "")
	if len(all) != 3 {
		t.Fatalf("ListRequests all = %d want 3", len(all))
	}
	// 降序：r3 (CreatedAt 最大) 在前。
	if all[0].ID != "r3" {
		t.Errorf("first = %q want r3", all[0].ID)
	}

	t1 := e.ListRequests("t1", "")
	if len(t1) != 2 {
		t.Errorf("t1 count = %d want 2", len(t1))
	}

	pending := e.ListRequests("t1", string(StatusPending))
	if len(pending) != 1 {
		t.Errorf("t1 pending = %d want 1", len(pending))
	}
	approved := e.ListRequests("t1", string(StatusApproved))
	if len(approved) != 1 {
		t.Errorf("t1 approved = %d want 1", len(approved))
	}
}

func TestListPendingApprovals(t *testing.T) {
	e, _ := newTestEngine()
	// sequential [alice, bob]：alice 先。
	_ = e.CreateFlow(newFlow("f1", "t1", TriggerShell, StepSequential, "alice", "bob"))
	_ = e.Submit(newRequest("r1", "f1", "t1", TriggerShell, "ops"))

	// alice 应见 r1。
	if got := e.ListPendingApprovals("alice"); len(got) != 1 {
		t.Errorf("alice pending = %d want 1", len(got))
	}
	// bob 不应见（未轮到）。
	if got := e.ListPendingApprovals("bob"); len(got) != 0 {
		t.Errorf("bob pending = %d want 0 (sequential not yet)", len(got))
	}
	// carol 不应见。
	if got := e.ListPendingApprovals("carol"); len(got) != 0 {
		t.Errorf("carol pending = %d want 0", len(got))
	}

	// alice 同意后 bob 应见。
	_ = e.Approve("r1", "alice", "")
	if got := e.ListPendingApprovals("bob"); len(got) != 1 {
		t.Errorf("bob pending after alice = %d want 1", len(got))
	}
	if got := e.ListPendingApprovals("alice"); len(got) != 0 {
		t.Errorf("alice pending after decide = %d want 0", len(got))
	}
}

func TestListPendingApprovalsCountersign(t *testing.T) {
	e, _ := newTestEngine()
	_ = e.CreateFlow(newFlow("f1", "t1", TriggerK8sDelete, StepCountersign, "alice", "bob"))
	_ = e.Submit(newRequest("r1", "f1", "t1", TriggerK8sDelete, "ops"))

	// 两人都应见。
	if got := e.ListPendingApprovals("alice"); len(got) != 1 {
		t.Errorf("alice pending = %d want 1", len(got))
	}
	if got := e.ListPendingApprovals("bob"); len(got) != 1 {
		t.Errorf("bob pending = %d want 1", len(got))
	}
	// alice 决策后只剩 bob。
	_ = e.Approve("r1", "alice", "")
	if got := e.ListPendingApprovals("alice"); len(got) != 0 {
		t.Errorf("alice pending after decide = %d want 0", len(got))
	}
	if got := e.ListPendingApprovals("bob"); len(got) != 1 {
		t.Errorf("bob pending after alice = %d want 1", len(got))
	}
}

// ========== 历史 ==========

func TestHistorySubmitApprove(t *testing.T) {
	e, _ := newTestEngine()
	_ = e.CreateFlow(newFlow("f1", "t1", TriggerShell, StepAnyOf, "alice"))
	_ = e.Submit(newRequest("r1", "f1", "t1", TriggerShell, "ops"))
	_ = e.Approve("r1", "alice", "ok")

	h, err := e.GetHistory("r1")
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	// submit + approve = 2 条。
	if len(h.Timeline) != 2 {
		t.Fatalf("timeline len = %d want 2", len(h.Timeline))
	}
	if h.Timeline[0].Action != HistorySubmit {
		t.Errorf("entry0 action = %q want submit", h.Timeline[0].Action)
	}
	if h.Timeline[1].Action != HistoryApprove {
		t.Errorf("entry1 action = %q want approve", h.Timeline[1].Action)
	}
	if h.Timeline[1].UserID != "alice" {
		t.Errorf("entry1 user = %q want alice", h.Timeline[1].UserID)
	}
	if h.Timeline[1].Comment != "ok" {
		t.Errorf("entry1 comment = %q want ok", h.Timeline[1].Comment)
	}
}

func TestHistoryMultiStepAdvance(t *testing.T) {
	e, _ := newTestEngine()
	f := newMultiStepFlow("f1", "t1", TriggerDeploy, StepSequential, []string{"alice"}, StepAnyOf, []string{"bob"})
	_ = e.CreateFlow(f)
	_ = e.Submit(newRequest("r1", "f1", "t1", TriggerDeploy, "ops"))
	_ = e.Approve("r1", "alice", "")
	_ = e.Approve("r1", "bob", "")

	h, _ := e.GetHistory("r1")
	// submit + step_advance(alice) + approve(bob) = 3 条。
	if len(h.Timeline) != 3 {
		t.Fatalf("timeline len = %d want 3: %+v", len(h.Timeline), h.Timeline)
	}
	if h.Timeline[1].Action != HistoryStepAdvance {
		t.Errorf("entry1 action = %q want step_advance", h.Timeline[1].Action)
	}
	if h.Timeline[2].Action != HistoryApprove {
		t.Errorf("entry2 action = %q want approve", h.Timeline[2].Action)
	}
}

func TestHistoryCancelAndReject(t *testing.T) {
	e, _ := newTestEngine()
	_ = e.CreateFlow(newFlow("f1", "t1", TriggerShell, StepAnyOf, "alice"))

	_ = e.Submit(newRequest("r1", "f1", "t1", TriggerShell, "ops"))
	_ = e.Cancel("r1", "ops")
	h1, _ := e.GetHistory("r1")
	if last, ok := h1.Last(); !ok || last.Action != HistoryCancel {
		t.Errorf("cancel history last = %+v", last)
	}

	_ = e.Submit(newRequest("r2", "f1", "t1", TriggerShell, "ops"))
	_ = e.Reject("r2", "alice", "no")
	h2, _ := e.GetHistory("r2")
	if last, ok := h2.Last(); !ok || last.Action != HistoryReject {
		t.Errorf("reject history last = %+v", last)
	}
}

func TestHistoryFilterByAction(t *testing.T) {
	e, _ := newTestEngine()
	_ = e.CreateFlow(newFlow("f1", "t1", TriggerShell, StepAnyOf, "alice"))
	_ = e.Submit(newRequest("r1", "f1", "t1", TriggerShell, "ops"))
	_ = e.Approve("r1", "alice", "")

	h, _ := e.GetHistory("r1")
	if got := h.FilterByAction(HistorySubmit); len(got) != 1 {
		t.Errorf("submit filter = %d want 1", len(got))
	}
	if got := h.FilterByAction(HistoryApprove); len(got) != 1 {
		t.Errorf("approve filter = %d want 1", len(got))
	}
	if got := h.FilterByAction(HistoryCancel); len(got) != 0 {
		t.Errorf("cancel filter = %d want 0", len(got))
	}
}

func TestGetHistoryMissingRequest(t *testing.T) {
	e, _ := newTestEngine()
	if _, err := e.GetHistory("nope"); err != ErrRequestNotFound {
		t.Errorf("GetHistory missing: %v want %v", err, ErrRequestNotFound)
	}
}

// ========== 通知回调 ==========

func TestNotifier(t *testing.T) {
	var mu sync.Mutex
	var actions []string
	var statuses []RequestStatus
	e, _ := newTestEngine()
	e.SetNotifier(func(req *ApprovalRequest, action string) {
		mu.Lock()
		defer mu.Unlock()
		actions = append(actions, action)
		statuses = append(statuses, req.Status)
	})
	_ = e.CreateFlow(newFlow("f1", "t1", TriggerShell, StepAnyOf, "alice"))
	_ = e.Submit(newRequest("r1", "f1", "t1", TriggerShell, "ops"))
	_ = e.Approve("r1", "alice", "")

	mu.Lock()
	defer mu.Unlock()
	want := []string{HistorySubmit, HistoryApprove}
	if len(actions) != len(want) {
		t.Fatalf("actions = %v want %v", actions, want)
	}
	for i := range want {
		if actions[i] != want[i] {
			t.Errorf("actions[%d] = %q want %q", i, actions[i], want[i])
		}
	}
	// 提交通知时状态 pending；通过通知时状态 approved。
	if statuses[0] != StatusPending || statuses[1] != StatusApproved {
		t.Errorf("statuses = %v want [pending approved]", statuses)
	}
}

func TestNotifierMultiStepAdvance(t *testing.T) {
	var mu sync.Mutex
	var actions []string
	e, _ := newTestEngine()
	e.SetNotifier(func(req *ApprovalRequest, action string) {
		mu.Lock()
		defer mu.Unlock()
		actions = append(actions, action)
	})
	f := newMultiStepFlow("f1", "t1", TriggerDeploy, StepSequential, []string{"alice"}, StepAnyOf, []string{"bob"})
	_ = e.CreateFlow(f)
	_ = e.Submit(newRequest("r1", "f1", "t1", TriggerDeploy, "ops"))
	_ = e.Approve("r1", "alice", "")
	_ = e.Approve("r1", "bob", "")

	mu.Lock()
	defer mu.Unlock()
	want := []string{HistorySubmit, HistoryStepAdvance, HistoryApprove}
	if len(actions) != 3 {
		t.Fatalf("actions = %v want %v", actions, want)
	}
	for i := range want {
		if actions[i] != want[i] {
			t.Errorf("actions[%d] = %q want %q", i, actions[i], want[i])
		}
	}
}

// ========== 并发安全 ==========

func TestEngineConcurrent(t *testing.T) {
	e, _ := newTestEngine()
	_ = e.CreateFlow(newFlow("f1", "t1", TriggerShell, StepAnyOf, "alice", "bob", "carol"))

	var wg sync.WaitGroup
	errs := make(chan error, 30)
	// 10 个并发提交。
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := newRequest("r"+itoa(i), "f1", "t1", TriggerShell, "ops")
			errs <- e.Submit(req)
		}(i)
	}
	wg.Wait()
	close(errs)
	submitOK := 0
	for err := range errs {
		if err == nil {
			submitOK++
		}
	}
	if submitOK != 10 {
		t.Errorf("concurrent submit ok = %d want 10", submitOK)
	}

	// 10 个并发审批同一请求（anyof，仅一个能成功）。
	_ = e.Submit(newRequest("rx", "f1", "t1", TriggerShell, "ops"))
	var wg2 sync.WaitGroup
	approveOK := int32(0)
	for _, user := range []string{"alice", "bob", "carol"} {
		for i := 0; i < 10; i++ {
			wg2.Add(1)
			go func(u string) {
				defer wg2.Done()
				if err := e.Approve("rx", u, ""); err == nil {
					approveOK++
				}
			}(user)
		}
	}
	wg2.Wait()
	if approveOK != 1 {
		t.Errorf("concurrent approve ok = %d want exactly 1", approveOK)
	}
}

// itoa 简易整数转字符串（避免引入 strconv 增加依赖）。
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// 确保特定错误类型不被误判。
func TestErrorSentinels(t *testing.T) {
	if !errors.Is(ErrFlowNotFound, ErrFlowNotFound) {
		t.Error("ErrFlowNotFound identity")
	}
	if !errors.Is(ErrInvalidTransition, ErrInvalidTransition) {
		t.Error("ErrInvalidTransition identity")
	}
}