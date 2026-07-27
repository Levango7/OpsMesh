package orchestration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// TestOrchestration_CycleRejected 验证含自环/环路的 DAG 在 Trigger 时被 dag.Validate 拒绝。
func TestOrchestration_CycleRejected(t *testing.T) {
	st := store.NewMemoryStore()
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	wfs := NewMemory()
	h := NewHandler(wfs, st)

	wf := &WorkflowDef{
		Name: "cycle", AgentID: a.AgentID, TenantID: "t1",
		DAG: `[{"id":"x","type":"shell","command":"s","dependsOn":["x"]}]`,
	}
	if err := wfs.Create(context.Background(), wf); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Trigger(context.Background(), wf.ID, "t1"); err == nil {
		t.Fatal("expected cycle DAG rejected by Trigger, got nil")
	}
}

// TestOrchestration_RunAndReconcile 验证线性 DAG 展开 + per-agent releaseDeps 驱动 + reconcile 翻终态。
func TestOrchestration_RunAndReconcile(t *testing.T) {
	st := store.NewMemoryStore()
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	wfs := NewMemory()
	h := NewHandler(wfs, st)

	wf := &WorkflowDef{
		Name: "linear", AgentID: a.AgentID, TenantID: "t1",
		DAG: `[{"id":"n1","type":"shell","command":"s1"},` +
			`{"id":"n2","type":"shell","command":"s2","dependsOn":["n1"]},` +
			`{"id":"n3","type":"shell","command":"s3","dependsOn":["n2"]}]`,
	}
	if !wf.Valid() {
		t.Fatal("wf.Valid() = false")
	}
	if err := wfs.Create(context.Background(), wf); err != nil {
		t.Fatal(err)
	}

	got, err := h.Trigger(context.Background(), wf.ID, "t1")
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if got.Status != StatusRunning {
		t.Fatalf("after trigger status=%s, want running", got.Status)
	}

	tasks := st.TasksByParent("wf:" + strconv.FormatInt(wf.ID, 10))
	if len(tasks) != 3 {
		t.Fatalf("expanded %d tasks, got %d", 3, len(tasks))
	}
	pending, blocked := 0, 0
	for _, tk := range tasks {
		switch tk.Status {
		case "pending":
			pending++
		case "blocked":
			blocked++
		}
	}
	if pending != 1 || blocked != 2 {
		t.Fatalf("pending=%d blocked=%d, want 1/2", pending, blocked)
	}

	// 确定性 TaskID 由 Trigger 分配为 wf-<id>-<nodeID>，用其直接重查（避免持有 stale 副本）。
	prefix := "wf-" + strconv.FormatInt(wf.ID, 10) + "-"
	fresh := func() map[string]*proto.Task {
		m := make(map[string]*proto.Task)
		for _, tk := range st.TasksByParent("wf:" + strconv.FormatInt(wf.ID, 10)) {
			m[tk.TaskID] = tk
		}
		return m
	}

	// done n1 → n2 应被 releaseDeps 释放为 pending。
	st.SubmitResult(&proto.TaskResult{TaskID: prefix + "n1", AgentID: a.AgentID, ExitCode: 0})
	fm := fresh()
	if fm[prefix+"n2"].Status != "pending" {
		t.Fatalf("after n1 done: n2 status=%s, want pending", fm[prefix+"n2"].Status)
	}
	// 未全部完成，reconcile 不应 success。
	_ = h.Reconcile(context.Background(), wf.ID, "t1")
	if g, _ := wfs.Get(context.Background(), wf.ID, "t1"); g.LastRunStatus == StatusSuccess {
		t.Fatal("reconciled success too early")
	}

	st.SubmitResult(&proto.TaskResult{TaskID: prefix + "n2", AgentID: a.AgentID, ExitCode: 0})
	fm = fresh()
	if fm[prefix+"n3"].Status != "pending" {
		t.Fatalf("after n2 done: n3 status=%s, want pending", fm[prefix+"n3"].Status)
	}
	st.SubmitResult(&proto.TaskResult{TaskID: prefix + "n3", AgentID: a.AgentID, ExitCode: 0})

	if err := h.Reconcile(context.Background(), wf.ID, "t1"); err != nil {
		t.Fatal(err)
	}
	g, _ := wfs.Get(context.Background(), wf.ID, "t1")
	if g.LastRunStatus != StatusSuccess || g.Status != StatusActive {
		t.Fatalf("final status=%s runStatus=%s, want active/success", g.Status, g.LastRunStatus)
	}
}

// TestOrchestration_ScheduleValidation 验证 /schedule 拒绝非法 cron、接受合法表达式。
func TestOrchestration_ScheduleValidation(t *testing.T) {
	st := store.NewMemoryStore()
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	wfs := NewMemory()
	h := NewHandler(wfs, st)

	wf := &WorkflowDef{Name: "wf", AgentID: a.AgentID, TenantID: "t1"}
	if err := wfs.Create(context.Background(), wf); err != nil {
		t.Fatal(err)
	}
	// 非法 cron 应在 schedule 时拒绝（handler 内 cron.Match 校验）。
	if err := h.SetCron(context.Background(), wf.ID, "t1", "not-a-cron"); err == nil {
		t.Fatal("expected invalid cron rejected")
	}
	// 合法 5 字段表达式应被接受并置 active。
	if err := h.SetCron(context.Background(), wf.ID, "t1", "*/5 * * * *"); err != nil {
		t.Fatalf("valid cron rejected: %v", err)
	}
	g, _ := wfs.Get(context.Background(), wf.ID, "t1")
	if g.Cron != "*/5 * * * *" || g.Status != StatusActive {
		t.Fatalf("after schedule cron=%q status=%s, want '*/5 * * * *'/active", g.Cron, g.Status)
	}
}

// TestOrchestration_UpdateWorkflow 验证 PUT 端点部分更新 name/dag/cron，并复用既有校验。
func TestOrchestration_UpdateWorkflow(t *testing.T) {
	st := store.NewMemoryStore()
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	wfs := NewMemory()
	h := NewHandler(wfs, st)

	wf := &WorkflowDef{Name: "orig", AgentID: a.AgentID, TenantID: "t1"}
	if err := wfs.Create(context.Background(), wf); err != nil {
		t.Fatal(err)
	}

	// 非法 DAG 应在更新时被拒（validateDAG → dag.Validate 环检测）。
	bad := `{"name":"x","dag":"[{\"id\":\"x\",\"dependsOn\":[\"x\"]}]"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/workflows/"+strconv.FormatInt(wf.ID, 10), strings.NewReader(bad))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.updateWorkflow(rec, req, wf.ID, "t1")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid dag update = %d, want 400", rec.Code)
	}

	// 合法更新：改名 + 新 DAG + 合法 cron（draft → active）。
	good := `{"name":"renamed","dag":"[{\"id\":\"n1\",\"type\":\"shell\",\"command\":\"s1\"}]","cron":"0 * * * *"}`
	req = httptest.NewRequest(http.MethodPut, "/api/v1/workflows/"+strconv.FormatInt(wf.ID, 10), strings.NewReader(good))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.updateWorkflow(rec, req, wf.ID, "t1")
	if rec.Code != http.StatusOK {
		t.Fatalf("valid update = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	g, _ := wfs.Get(context.Background(), wf.ID, "t1")
	if g.Name != "renamed" || g.DAG == "" || g.Cron != "0 * * * *" || g.Status != StatusActive {
		t.Fatalf("after update name=%q dag=%q cron=%q status=%q", g.Name, g.DAG, g.Cron, g.Status)
	}
}
