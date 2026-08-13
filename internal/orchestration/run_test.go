package orchestration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// TestWorkflowRun_CreateAndList 验证 CreateRun/ListRuns/UpdateRun 的基本存储语义：
// 自增 ID、租户隔离、按 workflowID 过滤、UpdateRun 终态回写。
func TestWorkflowRun_CreateAndList(t *testing.T) {
	wfs := NewMemory()
	ctx := context.Background()

	// 创建两条不同工作流的运行记录。
	r1 := &WorkflowRun{WorkflowID: 1, TenantID: "t1", Status: StatusRunning,
		NodeStates: map[string]string{"n1": "pending"}}
	r2 := &WorkflowRun{WorkflowID: 1, TenantID: "t1", Status: StatusRunning,
		NodeStates: map[string]string{"n1": "done"}}
	r3 := &WorkflowRun{WorkflowID: 2, TenantID: "t1", Status: StatusRunning}
	if err := wfs.CreateRun(ctx, r1); err != nil {
		t.Fatalf("CreateRun r1: %v", err)
	}
	if err := wfs.CreateRun(ctx, r2); err != nil {
		t.Fatalf("CreateRun r2: %v", err)
	}
	if err := wfs.CreateRun(ctx, r3); err != nil {
		t.Fatalf("CreateRun r3: %v", err)
	}
	if r1.ID != 1 || r2.ID != 2 || r3.ID != 3 {
		t.Fatalf("auto IDs = %d/%d/%d, want 1/2/3", r1.ID, r2.ID, r3.ID)
	}
	if r1.StartedAt.IsZero() {
		t.Fatal("StartedAt should default to now")
	}

	// ListRuns 按 workflowID 过滤。
	runs, err := wfs.ListRuns(ctx, 1, "t1")
	if err != nil {
		t.Fatalf("ListRuns wf1: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("ListRuns wf1 len=%d, want 2", len(runs))
	}
	if runs[0].ID != 1 || runs[1].ID != 2 {
		t.Fatalf("ListRuns order = %d/%d, want 1/2", runs[0].ID, runs[1].ID)
	}

	// 租户隔离：t2 看不到 t1 的记录。
	runsT2, _ := wfs.ListRuns(ctx, 1, "t2")
	if len(runsT2) != 0 {
		t.Fatalf("t2 sees %d runs, want 0 (tenant isolation)", len(runsT2))
	}

	// workflowID=2 只有一条。
	runs2, _ := wfs.ListRuns(ctx, 2, "t1")
	if len(runs2) != 1 || runs2[0].ID != 3 {
		t.Fatalf("ListRuns wf2 = %v, want [3]", runs2)
	}

	// UpdateRun 终态回写。
	r1.Status = StatusSuccess
	r1.FinishedAt = time.Now()
	r1.NodeStates = map[string]string{"n1": "done"}
	if err := wfs.UpdateRun(ctx, r1); err != nil {
		t.Fatalf("UpdateRun: %v", err)
	}
	got, _ := wfs.ListRuns(ctx, 1, "t1")
	if got[0].Status != StatusSuccess || got[0].FinishedAt.IsZero() {
		t.Fatalf("after UpdateRun: status=%s finishedAt zero=%v", got[0].Status, got[0].FinishedAt.IsZero())
	}
	if got[0].NodeStates["n1"] != "done" {
		t.Fatalf("after UpdateRun nodeStates[n1]=%q, want done", got[0].NodeStates["n1"])
	}

	// UpdateRun 不存在 ID 应报错。
	if err := wfs.UpdateRun(ctx, &WorkflowRun{ID: 999, WorkflowID: 1, TenantID: "t1"}); err == nil {
		t.Fatal("UpdateRun non-existent id accepted")
	}
}

// TestListRuns_API 验证 GET /api/v1/workflows/{id}/runs 通过 HTTP 返回执行历史列表。
// 覆盖：空列表返回 []、有数据返回数组、租户隔离、POST 方法拒绝。
func TestListRuns_API(t *testing.T) {
	st := store.NewMemoryStore()
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	wfs := NewMemory()
	h := NewHandler(wfs, st)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	wf := &WorkflowDef{Name: "wf", AgentID: a.AgentID, TenantID: "t1",
		DAG: `[{"id":"n1","type":"shell","command":"echo a"}]`}
	if err := wfs.Create(context.Background(), wf); err != nil {
		t.Fatal(err)
	}

	// 空列表：未触发过应返回 []。
	rec := httptest.NewRecorder()
	req := newWFRequest(http.MethodGet, "/api/v1/workflows/"+strconv.FormatInt(wf.ID, 10)+"/runs", "", "t1")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("empty list status=%d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var empty []WorkflowRun
	if err := json.Unmarshal(rec.Body.Bytes(), &empty); err != nil {
		t.Fatalf("empty list unmarshal: %v; body=%s", err, rec.Body.String())
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty list = %v, want []", empty)
	}

	// Trigger 后应有 1 条。
	if _, err := h.Trigger(context.Background(), wf.ID, "t1"); err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	rec = httptest.NewRecorder()
	req = newWFRequest(http.MethodGet, "/api/v1/workflows/"+strconv.FormatInt(wf.ID, 10)+"/runs", "", "t1")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list after trigger status=%d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var runs []WorkflowRun
	if err := json.Unmarshal(rec.Body.Bytes(), &runs); err != nil {
		t.Fatalf("list unmarshal: %v; body=%s", err, rec.Body.String())
	}
	if len(runs) != 1 {
		t.Fatalf("after trigger len=%d, want 1; body=%s", len(runs), rec.Body.String())
	}
	if runs[0].WorkflowID != wf.ID || runs[0].Status != StatusRunning {
		t.Fatalf("run = %+v", runs[0])
	}
	if runs[0].NodeStates["n1"] == "" {
		t.Fatalf("run nodeStates missing n1: %+v", runs[0])
	}

	// 租户隔离：t2 应看到空列表（不报 403，与 List 语义一致）。
	rec = httptest.NewRecorder()
	req = newWFRequest(http.MethodGet, "/api/v1/workflows/"+strconv.FormatInt(wf.ID, 10)+"/runs", "", "t2")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("t2 list status=%d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var runsT2 []WorkflowRun
	_ = json.Unmarshal(rec.Body.Bytes(), &runsT2)
	if len(runsT2) != 0 {
		t.Fatalf("t2 sees %d runs, want 0 (tenant isolation)", len(runsT2))
	}

	// POST 方法应被拒绝。
	rec = httptest.NewRecorder()
	req = newWFRequest(http.MethodPost, "/api/v1/workflows/"+strconv.FormatInt(wf.ID, 10)+"/runs", "", "t1")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST runs status=%d, want 405", rec.Code)
	}
}

// TestTrigger_CreatesRun 验证 Trigger 后自动创建 WorkflowRun 记录：
// Status=running、NodeStates 从已创建任务中收集、StartedAt 非零、FinishedAt 零。
// 多次 Trigger 应创建多条记录。
func TestTrigger_CreatesRun(t *testing.T) {
	st := store.NewMemoryStore()
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	wfs := NewMemory()
	h := NewHandler(wfs, st)

	wf := &WorkflowDef{Name: "wf", AgentID: a.AgentID, TenantID: "t1",
		DAG: `[{"id":"n1","type":"shell","command":"s1"},` +
			`{"id":"n2","type":"shell","command":"s2","dependsOn":["n1"]}]`}
	if err := wfs.Create(context.Background(), wf); err != nil {
		t.Fatal(err)
	}

	if _, err := h.Trigger(context.Background(), wf.ID, "t1"); err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	runs, err := wfs.ListRuns(context.Background(), wf.ID, "t1")
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("after trigger len=%d, want 1", len(runs))
	}
	r := runs[0]
	if r.Status != StatusRunning {
		t.Fatalf("run status=%q, want running", r.Status)
	}
	if r.StartedAt.IsZero() {
		t.Fatal("run startedAt is zero")
	}
	if !r.FinishedAt.IsZero() {
		t.Fatalf("run finishedAt non-zero: %v", r.FinishedAt)
	}
	if r.WorkflowID != wf.ID || r.TenantID != "t1" {
		t.Fatalf("run ids = %d/%q, want %d/t1", r.WorkflowID, r.TenantID, wf.ID)
	}
	// NodeStates 应包含 n1/n2 两个节点。
	if len(r.NodeStates) != 2 {
		t.Fatalf("run nodeStates len=%d, want 2; got=%v", len(r.NodeStates), r.NodeStates)
	}
	if _, ok := r.NodeStates["n1"]; !ok {
		t.Fatalf("run nodeStates missing n1: %v", r.NodeStates)
	}
	if _, ok := r.NodeStates["n2"]; !ok {
		t.Fatalf("run nodeStates missing n2: %v", r.NodeStates)
	}

	// 再次 Trigger 应创建第二条记录。
	if _, err := h.Trigger(context.Background(), wf.ID, "t1"); err != nil {
		t.Fatalf("Trigger 2: %v", err)
	}
	runs, _ = wfs.ListRuns(context.Background(), wf.ID, "t1")
	if len(runs) != 2 {
		t.Fatalf("after 2 triggers len=%d, want 2", len(runs))
	}
	if runs[1].ID <= runs[0].ID {
		t.Fatalf("run IDs not increasing: %d -> %d", runs[0].ID, runs[1].ID)
	}
}

// TestReconcile_UpdatesRun 验证 Reconcile 在终态时更新最近一条 WorkflowRun：
// 全 done → success + FinishedAt 非零；有 failed → failed + FinishedAt 非零；
// 非终态（running）不更新历史记录的 FinishedAt。
// 每个子用例使用独立 store，避免 Trigger 追加语义导致旧任务状态干扰新运行判定。
func TestReconcile_UpdatesRun(t *testing.T) {
	// === 子用例 1：非终态 → 终态 success ===
	t.Run("success", func(t *testing.T) {
		st := store.NewMemoryStore()
		a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
		wfs := NewMemory()
		h := NewHandler(wfs, st)

		wf := &WorkflowDef{Name: "wf", AgentID: a.AgentID, TenantID: "t1",
			DAG: `[{"id":"n1","type":"shell","command":"s1"},` +
				`{"id":"n2","type":"shell","command":"s2","dependsOn":["n1"]}]`}
		if err := wfs.Create(context.Background(), wf); err != nil {
			t.Fatal(err)
		}
		if _, err := h.Trigger(context.Background(), wf.ID, "t1"); err != nil {
			t.Fatalf("Trigger: %v", err)
		}
		prefix := "wf-" + strconv.FormatInt(wf.ID, 10) + "-"

		// 非终态：完成 n1，n2 仍 pending，Reconcile 应保持 running，不写 FinishedAt。
		st.ClaimTask(a.AgentID)
		st.SubmitResult(&proto.TaskResult{TaskID: prefix + "n1", AgentID: a.AgentID, ExitCode: 0})
		if err := h.Reconcile(context.Background(), wf.ID, "t1"); err != nil {
			t.Fatalf("Reconcile mid: %v", err)
		}
		runs, _ := wfs.ListRuns(context.Background(), wf.ID, "t1")
		if len(runs) != 1 {
			t.Fatalf("runs len=%d, want 1", len(runs))
		}
		if runs[0].Status != StatusRunning {
			t.Fatalf("mid reconcile run status=%q, want running", runs[0].Status)
		}
		if !runs[0].FinishedAt.IsZero() {
			t.Fatalf("mid reconcile finishedAt non-zero: %v", runs[0].FinishedAt)
		}

		// 终态：完成 n2，Reconcile 应置 success + FinishedAt 非零。
		st.ClaimTask(a.AgentID)
		st.SubmitResult(&proto.TaskResult{TaskID: prefix + "n2", AgentID: a.AgentID, ExitCode: 0})
		if err := h.Reconcile(context.Background(), wf.ID, "t1"); err != nil {
			t.Fatalf("Reconcile final: %v", err)
		}
		runs, _ = wfs.ListRuns(context.Background(), wf.ID, "t1")
		if runs[0].Status != StatusSuccess {
			t.Fatalf("final run status=%q, want success", runs[0].Status)
		}
		if runs[0].FinishedAt.IsZero() {
			t.Fatal("final run finishedAt is zero")
		}
		if runs[0].NodeStates["n1"] != "done" || runs[0].NodeStates["n2"] != "done" {
			t.Fatalf("final run nodeStates=%v, want all done", runs[0].NodeStates)
		}
	})

	// === 子用例 2：终态 failed ===
	t.Run("failed", func(t *testing.T) {
		st := store.NewMemoryStore()
		a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
		wfs := NewMemory()
		h := NewHandler(wfs, st)

		wf := &WorkflowDef{Name: "wf", AgentID: a.AgentID, TenantID: "t1",
			DAG: `[{"id":"n1","type":"shell","command":"s1"},` +
				`{"id":"n2","type":"shell","command":"s2","dependsOn":["n1"]}]`}
		if err := wfs.Create(context.Background(), wf); err != nil {
			t.Fatal(err)
		}
		if _, err := h.Trigger(context.Background(), wf.ID, "t1"); err != nil {
			t.Fatalf("Trigger: %v", err)
		}
		prefix := "wf-" + strconv.FormatInt(wf.ID, 10) + "-"

		// n1 成功，n2 失败 → Reconcile 应置 failed + FinishedAt 非零。
		st.ClaimTask(a.AgentID)
		st.SubmitResult(&proto.TaskResult{TaskID: prefix + "n1", AgentID: a.AgentID, ExitCode: 0})
		st.ClaimTask(a.AgentID)
		st.SubmitResult(&proto.TaskResult{TaskID: prefix + "n2", AgentID: a.AgentID, ExitCode: 1})
		if err := h.Reconcile(context.Background(), wf.ID, "t1"); err != nil {
			t.Fatalf("Reconcile failed: %v", err)
		}
		runs, _ := wfs.ListRuns(context.Background(), wf.ID, "t1")
		if len(runs) != 1 {
			t.Fatalf("runs len=%d, want 1", len(runs))
		}
		if runs[0].Status != StatusFailed {
			t.Fatalf("failed run status=%q, want failed", runs[0].Status)
		}
		if runs[0].FinishedAt.IsZero() {
			t.Fatal("failed run finishedAt is zero")
		}
		if runs[0].NodeStates["n2"] != "failed" {
			t.Fatalf("failed run nodeStates[n2]=%q, want failed", runs[0].NodeStates["n2"])
		}
	})
}

// TestListRuns_EmptyForMissingWorkflow 验证查询不存在的工作流返回空列表而非 404（与 List 语义一致）。
func TestListRuns_EmptyForMissingWorkflow(t *testing.T) {
	wfs := NewMemory()
	runs, err := wfs.ListRuns(context.Background(), 99999, "t1")
	if err != nil {
		t.Fatalf("ListRuns missing wf: %v", err)
	}
	if runs == nil || len(runs) != 0 {
		t.Fatalf("ListRuns missing wf = %v, want []", runs)
	}
}

// TestWorkflowRun_JSONRoundtrip 验证 WorkflowRun 的 JSON 序列化字段名与零值行为（向后兼容）。
func TestWorkflowRun_JSONRoundtrip(t *testing.T) {
	r := WorkflowRun{
		ID:         7,
		WorkflowID: 42,
		TenantID:   "t1",
		StartedAt:  time.Date(2026, 8, 13, 14, 0, 0, 0, time.UTC),
		Status:     StatusSuccess,
		NodeStates: map[string]string{"n1": "done", "n2": "done"},
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	for _, key := range []string{`"id":7`, `"workflowID":42`, `"tenantID":"t1"`, `"status":"success"`, `"nodeStates":`} {
		if !strings.Contains(s, key) {
			t.Fatalf("json missing %q: %s", key, s)
		}
	}
	// 反序列化应保留所有字段。
	var got WorkflowRun
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != 7 || got.WorkflowID != 42 || got.TenantID != "t1" || got.Status != StatusSuccess {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
	if got.NodeStates["n1"] != "done" || got.NodeStates["n2"] != "done" {
		t.Fatalf("roundtrip nodeStates mismatch: %v", got.NodeStates)
	}
}
