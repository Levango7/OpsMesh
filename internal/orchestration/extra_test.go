package orchestration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// newWFRequest 构造带租户头的 HTTP 请求（辅助函数）。
func newWFRequest(method, url, body, tenantID string) *http.Request {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, url, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, url, nil)
	}
	r.Header.Set("x-tenant-id", tenantID)
	r.Header.Set("Content-Type", "application/json")
	return r
}

// TestExtra_MultiAgentDispatch 验证多 agent 调度：不同工作流的任务分配到各自 agent，互不干扰。
func TestExtra_MultiAgentDispatch(t *testing.T) {
	st := store.NewMemoryStore()
	a1 := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	a2 := st.Register(&proto.AgentInfo{Segment: "seg-b", TenantID: "t1"})
	wfs := NewMemory()
	h := NewHandler(wfs, st)

	wf1 := &WorkflowDef{Name: "wf-a", AgentID: a1.AgentID, TenantID: "t1",
		DAG: `[{"id":"n1","type":"shell","command":"echo a"}]`}
	wf2 := &WorkflowDef{Name: "wf-b", AgentID: a2.AgentID, TenantID: "t1",
		DAG: `[{"id":"n1","type":"shell","command":"echo b"}]`}
	if err := wfs.Create(context.Background(), wf1); err != nil {
		t.Fatal(err)
	}
	if err := wfs.Create(context.Background(), wf2); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Trigger(context.Background(), wf1.ID, "t1"); err != nil {
		t.Fatalf("trigger wf1: %v", err)
	}
	if _, err := h.Trigger(context.Background(), wf2.ID, "t1"); err != nil {
		t.Fatalf("trigger wf2: %v", err)
	}

	tasks1 := st.TasksByParent("wf:" + strconv.FormatInt(wf1.ID, 10))
	tasks2 := st.TasksByParent("wf:" + strconv.FormatInt(wf2.ID, 10))
	if len(tasks1) != 1 || tasks1[0].AgentID != a1.AgentID {
		t.Fatalf("wf1 task agent=%q, want %q", tasks1[0].AgentID, a1.AgentID)
	}
	if len(tasks2) != 1 || tasks2[0].AgentID != a2.AgentID {
		t.Fatalf("wf2 task agent=%q, want %q", tasks2[0].AgentID, a2.AgentID)
	}

	// 各 agent 只能领取自己的任务
	tk1 := st.ClaimTask(a1.AgentID)
	tk2 := st.ClaimTask(a2.AgentID)
	if tk1 == nil || tk1.AgentID != a1.AgentID {
		t.Fatalf("a1 claim failed: %v", tk1)
	}
	if tk2 == nil || tk2.AgentID != a2.AgentID {
		t.Fatalf("a2 claim failed: %v", tk2)
	}
	if st.ClaimTask(a1.AgentID) != nil {
		t.Fatal("a1 should have no more tasks")
	}
}

// TestExtra_TaskRetryAndDeadLetter 验证失败任务的重试逻辑与死信：达 MaxRetries 后置 failed + DeadLetter。
func TestExtra_TaskRetryAndDeadLetter(t *testing.T) {
	st := store.NewMemoryStore()
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	task := st.CreateTask(&proto.Task{
		AgentID: a.AgentID, TenantID: "t1",
		Type: "shell", Command: "false",
		MaxRetries: 2,
	})

	// 第 1 次失败 → retry 1，重回 pending
	st.ClaimTask(a.AgentID)
	st.SubmitResult(&proto.TaskResult{TaskID: task.TaskID, AgentID: a.AgentID, ExitCode: 1})
	cur := st.TaskByID(task.TaskID)
	if cur.Status != "pending" || cur.RetryCount != 1 {
		t.Fatalf("after 1st fail: status=%s retry=%d, want pending/1", cur.Status, cur.RetryCount)
	}

	// 第 2 次失败 → retry 2
	st.ClaimTask(a.AgentID)
	st.SubmitResult(&proto.TaskResult{TaskID: task.TaskID, AgentID: a.AgentID, ExitCode: 1})
	cur = st.TaskByID(task.TaskID)
	if cur.Status != "pending" || cur.RetryCount != 2 {
		t.Fatalf("after 2nd fail: status=%s retry=%d, want pending/2", cur.Status, cur.RetryCount)
	}

	// 第 3 次失败 → 达上限，死信
	st.ClaimTask(a.AgentID)
	st.SubmitResult(&proto.TaskResult{TaskID: task.TaskID, AgentID: a.AgentID, ExitCode: 1})
	cur = st.TaskByID(task.TaskID)
	if cur.Status != "failed" || !cur.DeadLetter {
		t.Fatalf("after 3rd fail: status=%s deadLetter=%v, want failed/true", cur.Status, cur.DeadLetter)
	}
}

// TestExtra_LeaseRenewal 验证 leader 选举租约续期：MemoryStore 恒为 leader，多次续租保持。
func TestExtra_LeaseRenewal(t *testing.T) {
	st := store.NewMemoryStore()
	if !st.IsLeader() {
		t.Fatal("IsLeader = false, want true")
	}
	if !st.RenewLeadership(10 * time.Second) {
		t.Fatal("RenewLeadership = false, want true")
	}
	for i := 0; i < 3; i++ {
		if !st.RenewLeadership(time.Minute) {
			t.Fatalf("renew %d = false", i)
		}
	}
	if !st.IsLeader() {
		t.Fatal("after renew IsLeader = false")
	}
}

// TestExtra_DAGParallelExecution 验证无依赖节点的并行执行：Trigger 后全部 pending，可并发领取。
func TestExtra_DAGParallelExecution(t *testing.T) {
	st := store.NewMemoryStore()
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	wfs := NewMemory()
	h := NewHandler(wfs, st)

	wf := &WorkflowDef{Name: "parallel", AgentID: a.AgentID, TenantID: "t1",
		DAG: `[{"id":"a","type":"shell","command":"a"},` +
			`{"id":"b","type":"shell","command":"b"},` +
			`{"id":"c","type":"shell","command":"c"}]`}
	if err := wfs.Create(context.Background(), wf); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Trigger(context.Background(), wf.ID, "t1"); err != nil {
		t.Fatal(err)
	}

	tasks := st.TasksByParent("wf:" + strconv.FormatInt(wf.ID, 10))
	if len(tasks) != 3 {
		t.Fatalf("expanded %d tasks, want 3", len(tasks))
	}
	for _, tk := range tasks {
		if tk.Status != "pending" {
			t.Fatalf("task %s status=%s, want pending", tk.TaskID, tk.Status)
		}
	}

	// 并发领取（模拟并行 worker）
	var wg sync.WaitGroup
	claimed := make([]string, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if tk := st.ClaimTask(a.AgentID); tk != nil {
				claimed[idx] = tk.TaskID
			}
		}(i)
	}
	wg.Wait()
	seen := make(map[string]bool)
	for _, id := range claimed {
		if id != "" {
			seen[id] = true
		}
	}
	if len(seen) != 3 {
		t.Fatalf("distinct claimed = %d, want 3 (parallel)", len(seen))
	}
}

// TestExtra_DAGSerialExecution 验证有依赖节点的串行执行：blocked → 依赖 done 后释放为 pending。
func TestExtra_DAGSerialExecution(t *testing.T) {
	st := store.NewMemoryStore()
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	wfs := NewMemory()
	h := NewHandler(wfs, st)

	wf := &WorkflowDef{Name: "serial", AgentID: a.AgentID, TenantID: "t1",
		DAG: `[{"id":"n1","type":"shell","command":"s1"},` +
			`{"id":"n2","type":"shell","command":"s2","dependsOn":["n1"]},` +
			`{"id":"n3","type":"shell","command":"s3","dependsOn":["n2"]}]`}
	if err := wfs.Create(context.Background(), wf); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Trigger(context.Background(), wf.ID, "t1"); err != nil {
		t.Fatal(err)
	}

	prefix := "wf-" + strconv.FormatInt(wf.ID, 10) + "-"
	fresh := func() map[string]*proto.Task {
		m := make(map[string]*proto.Task)
		for _, tk := range st.TasksByParent("wf:" + strconv.FormatInt(wf.ID, 10)) {
			m[tk.TaskID] = tk
		}
		return m
	}

	fm := fresh()
	if fm[prefix+"n1"].Status != "pending" {
		t.Fatalf("n1=%s, want pending", fm[prefix+"n1"].Status)
	}
	if fm[prefix+"n2"].Status != "blocked" || fm[prefix+"n3"].Status != "blocked" {
		t.Fatalf("n2=%s n3=%s, want blocked/blocked", fm[prefix+"n2"].Status, fm[prefix+"n3"].Status)
	}

	// done n1 → n2 释放
	st.ClaimTask(a.AgentID)
	st.SubmitResult(&proto.TaskResult{TaskID: prefix + "n1", AgentID: a.AgentID, ExitCode: 0})
	fm = fresh()
	if fm[prefix+"n2"].Status != "pending" || fm[prefix+"n3"].Status != "blocked" {
		t.Fatalf("after n1: n2=%s n3=%s, want pending/blocked", fm[prefix+"n2"].Status, fm[prefix+"n3"].Status)
	}

	// done n2 → n3 释放
	st.ClaimTask(a.AgentID)
	st.SubmitResult(&proto.TaskResult{TaskID: prefix + "n2", AgentID: a.AgentID, ExitCode: 0})
	fm = fresh()
	if fm[prefix+"n3"].Status != "pending" {
		t.Fatalf("after n2: n3=%s, want pending", fm[prefix+"n3"].Status)
	}

	// done n3 → reconcile success
	st.ClaimTask(a.AgentID)
	st.SubmitResult(&proto.TaskResult{TaskID: prefix + "n3", AgentID: a.AgentID, ExitCode: 0})
	if err := h.Reconcile(context.Background(), wf.ID, "t1"); err != nil {
		t.Fatal(err)
	}
	g, _ := wfs.Get(context.Background(), wf.ID, "t1")
	if g.LastRunStatus != StatusSuccess || g.Status != StatusActive {
		t.Fatalf("final status=%s runStatus=%s, want active/success", g.Status, g.LastRunStatus)
	}
}

// TestExtra_TaskCancellation 验证任务取消：pending/running 可取消，done 不可，跨租户拒绝。
func TestExtra_TaskCancellation(t *testing.T) {
	st := store.NewMemoryStore()
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})

	// 取消 pending
	t1 := st.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "sleep"})
	if !st.CancelTask(t1.TaskID, "t1") {
		t.Fatal("cancel pending failed")
	}
	if cur := st.TaskByID(t1.TaskID); cur.Status != "cancelled" {
		t.Fatalf("after cancel: status=%s, want cancelled", cur.Status)
	}
	ids := st.CancelledTaskIDs(a.AgentID)
	if len(ids) != 1 || ids[0] != t1.TaskID {
		t.Fatalf("cancelled ids=%v, want [%s]", ids, t1.TaskID)
	}

	// 取消 running
	t2 := st.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "sleep"})
	st.ClaimTask(a.AgentID)
	if !st.CancelTask(t2.TaskID, "t1") {
		t.Fatal("cancel running failed")
	}

	// done 不可取消
	t3 := st.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "true"})
	st.ClaimTask(a.AgentID)
	st.SubmitResult(&proto.TaskResult{TaskID: t3.TaskID, AgentID: a.AgentID, ExitCode: 0})
	if st.CancelTask(t3.TaskID, "t1") {
		t.Fatal("cancel done should fail")
	}

	// 跨租户拒绝
	t4 := st.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "x"})
	if st.CancelTask(t4.TaskID, "other") {
		t.Fatal("cross-tenant cancel should fail")
	}

	// cancelled 任务不会被 ClaimTask 领取；t4 仍 pending 应被领取
	tk := st.ClaimTask(a.AgentID)
	if tk == nil {
		t.Fatal("t4 should be claimed")
	}
	if tk.TaskID != t4.TaskID {
		t.Fatalf("claimed %s, want %s", tk.TaskID, t4.TaskID)
	}
}

// TestExtra_TaskCancellationWithContext 验证用 context cancel 取消运行中任务。
func TestExtra_TaskCancellationWithContext(t *testing.T) {
	st := store.NewMemoryStore()
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})

	ctx, cancel := context.WithCancel(context.Background())

	task := st.CreateTask(&proto.Task{AgentID: a.AgentID, TenantID: "t1", Type: "shell", Command: "sleep 100"})
	st.ClaimTask(a.AgentID)

	// 模拟客户端取消：cancel context 后调用 CancelTask
	cancel()
	if ctx.Err() != context.Canceled {
		t.Fatalf("ctx.Err() = %v, want Canceled", ctx.Err())
	}
	if !st.CancelTask(task.TaskID, "t1") {
		t.Fatal("cancel after ctx cancel failed")
	}
	if cur := st.TaskByID(task.TaskID); cur.Status != "cancelled" {
		t.Fatalf("after cancel: status=%s, want cancelled", cur.Status)
	}
}

// TestExtra_ScheduleLoopWithCancel 验证调度循环响应 context cancel 停止。
func TestExtra_ScheduleLoopWithCancel(t *testing.T) {
	st := store.NewMemoryStore()
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	wfs := NewMemory()
	h := NewHandler(wfs, st)

	wf := &WorkflowDef{Name: "wf", AgentID: a.AgentID, TenantID: "t1"}
	if err := wfs.Create(context.Background(), wf); err != nil {
		t.Fatal(err)
	}
	if err := h.SetCron(context.Background(), wf.ID, "t1", "* * * * *"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	loopCount := 0
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				active, _ := h.ListActive(ctx)
				if len(active) > 0 {
					mu.Lock()
					loopCount++
					mu.Unlock()
				}
			}
		}
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if loopCount == 0 {
		t.Fatal("schedule loop never observed active wf")
	}
}

// TestExtra_HTTPWorkflowsEndpoint 验证 POST/GET /api/v1/workflows 端点。
func TestExtra_HTTPWorkflowsEndpoint(t *testing.T) {
	st := store.NewMemoryStore()
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	wfs := NewMemory()
	h := NewHandler(wfs, st)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// POST 创建
	body := `{"name":"wf-http","agentID":"` + a.AgentID + `","dag":"[{\"id\":\"n1\",\"type\":\"shell\",\"command\":\"echo\"}]"}`
	req := newWFRequest(http.MethodPost, "/api/v1/workflows", body, "t1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var created WorkflowDef
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Name != "wf-http" || created.ID == 0 {
		t.Fatalf("created = %+v", created)
	}

	// GET 列表
	req = newWFRequest(http.MethodGet, "/api/v1/workflows", "", "t1")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d, want 200", rec.Code)
	}
	var list []WorkflowDef
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "wf-http" {
		t.Fatalf("list = %+v", list)
	}

	// POST 非法 JSON
	req = newWFRequest(http.MethodPost, "/api/v1/workflows", "not json", "t1")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid json = %d, want 400", rec.Code)
	}

	// POST 缺名称
	req = newWFRequest(http.MethodPost, "/api/v1/workflows", `{"agentID":"x"}`, "t1")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing name = %d, want 400", rec.Code)
	}

	// POST 非法 DAG（自依赖）
	req = newWFRequest(http.MethodPost, "/api/v1/workflows", `{"name":"x","agentID":"`+a.AgentID+`","dag":"[{\"id\":\"x\",\"dependsOn\":[\"x\"]}]"}`, "t1")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid dag = %d, want 400", rec.Code)
	}

	// 不允许的方法
	req = newWFRequest(http.MethodDelete, "/api/v1/workflows", "", "t1")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("delete = %d, want 405", rec.Code)
	}
}

// TestExtra_HTTPWorkflowByIDEndpoints 验证 /api/v1/workflows/{id} 子操作路由。
func TestExtra_HTTPWorkflowByIDEndpoints(t *testing.T) {
	st := store.NewMemoryStore()
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	wfs := NewMemory()
	h := NewHandler(wfs, st)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	wf := &WorkflowDef{Name: "wf", AgentID: a.AgentID, TenantID: "t1",
		DAG: `[{"id":"n1","type":"shell","command":"s1"}]`}
	if err := wfs.Create(context.Background(), wf); err != nil {
		t.Fatal(err)
	}
	idStr := strconv.FormatInt(wf.ID, 10)

	// GET 单个
	req := newWFRequest(http.MethodGet, "/api/v1/workflows/"+idStr, "", "t1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// GET 不存在
	req = newWFRequest(http.MethodGet, "/api/v1/workflows/9999", "", "t1")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get not exist = %d, want 404", rec.Code)
	}

	// GET 跨租户
	req = newWFRequest(http.MethodGet, "/api/v1/workflows/"+idStr, "", "other")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("get cross tenant = %d, want 403", rec.Code)
	}

	// POST schedule 合法 cron
	req = newWFRequest(http.MethodPost, "/api/v1/workflows/"+idStr+"/schedule", `{"cron":"*/5 * * * *"}`, "t1")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("schedule = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// GET schedule
	req = newWFRequest(http.MethodGet, "/api/v1/workflows/"+idStr+"/schedule", "", "t1")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get schedule = %d, want 200", rec.Code)
	}

	// POST run
	req = newWFRequest(http.MethodPost, "/api/v1/workflows/"+idStr+"/run", "", "t1")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("run = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// GET status
	req = newWFRequest(http.MethodGet, "/api/v1/workflows/"+idStr+"/status", "", "t1")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var statusResp struct {
		Workflow  *WorkflowDef      `json:"workflow"`
		NodeTasks map[string]string `json:"nodeTasks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &statusResp); err != nil {
		t.Fatal(err)
	}
	if len(statusResp.NodeTasks) != 1 {
		t.Fatalf("nodeTasks = %v, want 1 entry", statusResp.NodeTasks)
	}

	// 非法 ID
	req = newWFRequest(http.MethodGet, "/api/v1/workflows/abc", "", "t1")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid id = %d, want 400", rec.Code)
	}

	// POST schedule 非法 cron
	req = newWFRequest(http.MethodPost, "/api/v1/workflows/"+idStr+"/schedule", `{"cron":"bad"}`, "t1")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("schedule bad cron = %d, want 400", rec.Code)
	}

	// POST schedule 非法 JSON
	req = newWFRequest(http.MethodPost, "/api/v1/workflows/"+idStr+"/schedule", "not json", "t1")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("schedule bad json = %d, want 400", rec.Code)
	}

	// run 不存在 → Get 失败 → 404
	req = newWFRequest(http.MethodPost, "/api/v1/workflows/9999/run", "", "t1")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("run not exist = %d, want 404", rec.Code)
	}

	// status 不存在 → 404
	req = newWFRequest(http.MethodGet, "/api/v1/workflows/9999/status", "", "t1")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status not exist = %d, want 404", rec.Code)
	}

	// schedule 不存在 → 404
	req = newWFRequest(http.MethodPost, "/api/v1/workflows/9999/schedule", `{"cron":"*/5 * * * *"}`, "t1")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("schedule not exist = %d, want 404", rec.Code)
	}
}

// TestExtra_StoreListAndDelete 验证 MemoryWorkflowStore 的 List/Delete。
func TestExtra_StoreListAndDelete(t *testing.T) {
	wfs := NewMemory()
	wf1 := &WorkflowDef{Name: "wf1", AgentID: "a1", TenantID: "t1"}
	wf2 := &WorkflowDef{Name: "wf2", AgentID: "a2", TenantID: "t2"}
	if err := wfs.Create(context.Background(), wf1); err != nil {
		t.Fatal(err)
	}
	if err := wfs.Create(context.Background(), wf2); err != nil {
		t.Fatal(err)
	}

	all, err := wfs.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("list all = %d, want 2", len(all))
	}

	t1Only, err := wfs.List(context.Background(), "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(t1Only) != 1 || t1Only[0].Name != "wf1" {
		t.Fatalf("list t1 = %+v, want [wf1]", t1Only)
	}

	if err := wfs.Delete(context.Background(), wf1.ID, "t1"); err != nil {
		t.Fatal(err)
	}
	all, _ = wfs.List(context.Background(), "")
	if len(all) != 1 {
		t.Fatalf("after delete = %d, want 1", len(all))
	}

	if err := wfs.Delete(context.Background(), 9999, "t1"); err != ErrWFNotFound {
		t.Fatalf("delete not exist = %v, want ErrWFNotFound", err)
	}
	if err := wfs.Delete(context.Background(), wf2.ID, "t1"); err != ErrWFTenantMismatch {
		t.Fatalf("delete cross tenant = %v, want ErrWFTenantMismatch", err)
	}
}

// TestExtra_WorkflowNodesParsing 验证 WorkflowDef.Nodes 边界。
func TestExtra_WorkflowNodesParsing(t *testing.T) {
	wf := &WorkflowDef{}
	ns, err := wf.Nodes()
	if err != nil || ns != nil {
		t.Fatalf("empty dag: ns=%v err=%v", ns, err)
	}
	wf.DAG = `[{"id":"n1","type":"shell","command":"echo"}]`
	ns, err = wf.Nodes()
	if err != nil || len(ns) != 1 {
		t.Fatalf("valid dag: ns=%v err=%v", ns, err)
	}
	wf.DAG = "not json"
	if _, err := wf.Nodes(); err == nil {
		t.Fatal("invalid json should error")
	}
}

// TestExtra_ReconcileScenarios 验证 Reconcile 的 failed/running/no-tasks 场景。
func TestExtra_ReconcileScenarios(t *testing.T) {
	st := store.NewMemoryStore()
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	wfs := NewMemory()
	h := NewHandler(wfs, st)

	// 场景 1：无任务 → Reconcile 直接返回
	wf0 := &WorkflowDef{Name: "empty", AgentID: a.AgentID, TenantID: "t1"}
	if err := wfs.Create(context.Background(), wf0); err != nil {
		t.Fatal(err)
	}
	if err := h.Reconcile(context.Background(), wf0.ID, "t1"); err != nil {
		t.Fatal(err)
	}

	// 场景 2：失败 → failed
	wf1 := &WorkflowDef{Name: "fail", AgentID: a.AgentID, TenantID: "t1",
		DAG: `[{"id":"n1","type":"shell","command":"false"}]`}
	if err := wfs.Create(context.Background(), wf1); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Trigger(context.Background(), wf1.ID, "t1"); err != nil {
		t.Fatal(err)
	}
	prefix := "wf-" + strconv.FormatInt(wf1.ID, 10) + "-"
	st.ClaimTask(a.AgentID)
	st.SubmitResult(&proto.TaskResult{TaskID: prefix + "n1", AgentID: a.AgentID, ExitCode: 1})
	if err := h.Reconcile(context.Background(), wf1.ID, "t1"); err != nil {
		t.Fatal(err)
	}
	g, _ := wfs.Get(context.Background(), wf1.ID, "t1")
	if g.LastRunStatus != StatusFailed {
		t.Fatalf("reconcile failed: runStatus=%s, want failed", g.LastRunStatus)
	}

	// 场景 3：部分完成 → running
	wf2 := &WorkflowDef{Name: "partial", AgentID: a.AgentID, TenantID: "t1",
		DAG: `[{"id":"n1","type":"shell","command":"s1"},` +
			`{"id":"n2","type":"shell","command":"s2"}]`}
	if err := wfs.Create(context.Background(), wf2); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Trigger(context.Background(), wf2.ID, "t1"); err != nil {
		t.Fatal(err)
	}
	prefix2 := "wf-" + strconv.FormatInt(wf2.ID, 10) + "-"
	st.ClaimTask(a.AgentID)
	st.SubmitResult(&proto.TaskResult{TaskID: prefix2 + "n1", AgentID: a.AgentID, ExitCode: 0})
	if err := h.Reconcile(context.Background(), wf2.ID, "t1"); err != nil {
		t.Fatal(err)
	}
	g, _ = wfs.Get(context.Background(), wf2.ID, "t1")
	if g.LastRunStatus != StatusRunning {
		t.Fatalf("reconcile partial: runStatus=%s, want running", g.LastRunStatus)
	}
}

// TestExtra_TriggerAndSetCronErrors 验证 Trigger/SetCron 错误场景。
func TestExtra_TriggerAndSetCronErrors(t *testing.T) {
	st := store.NewMemoryStore()
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	wfs := NewMemory()
	h := NewHandler(wfs, st)

	// Trigger 不存在
	if _, err := h.Trigger(context.Background(), 9999, "t1"); err != ErrWFNotFound {
		t.Fatalf("trigger not exist = %v, want ErrWFNotFound", err)
	}

	// Trigger 缺失依赖
	wf := &WorkflowDef{Name: "bad", AgentID: a.AgentID, TenantID: "t1",
		DAG: `[{"id":"x","dependsOn":["y"]}]`}
	if err := wfs.Create(context.Background(), wf); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Trigger(context.Background(), wf.ID, "t1"); err == nil {
		t.Fatal("trigger missing dep should error")
	}

	// Trigger 非法 JSON
	wf2 := &WorkflowDef{Name: "badjson", AgentID: a.AgentID, TenantID: "t1", DAG: "not json"}
	if err := wfs.Create(context.Background(), wf2); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Trigger(context.Background(), wf2.ID, "t1"); err == nil {
		t.Fatal("trigger invalid json should error")
	}

	// SetCron 不存在
	if err := h.SetCron(context.Background(), 9999, "t1", "*/5 * * * *"); err != ErrWFNotFound {
		t.Fatalf("setcron not exist = %v, want ErrWFNotFound", err)
	}

	// SetCron 空清除
	wf3 := &WorkflowDef{Name: "wf", AgentID: a.AgentID, TenantID: "t1"}
	if err := wfs.Create(context.Background(), wf3); err != nil {
		t.Fatal(err)
	}
	if err := h.SetCron(context.Background(), wf3.ID, "t1", ""); err != nil {
		t.Fatalf("setcron empty = %v", err)
	}
	g, _ := wfs.Get(context.Background(), wf3.ID, "t1")
	if g.Cron != "" {
		t.Fatalf("after setcron empty: cron=%q", g.Cron)
	}
}

// TestExtra_UpdateWorkflowErrors 验证 updateWorkflow 错误分支。
func TestExtra_UpdateWorkflowErrors(t *testing.T) {
	st := store.NewMemoryStore()
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	wfs := NewMemory()
	h := NewHandler(wfs, st)

	wf := &WorkflowDef{Name: "wf", AgentID: a.AgentID, TenantID: "t1"}
	if err := wfs.Create(context.Background(), wf); err != nil {
		t.Fatal(err)
	}

	// 不存在
	req := newWFRequest(http.MethodPut, "/api/v1/workflows/9999", `{"name":"x"}`, "t1")
	rec := httptest.NewRecorder()
	h.updateWorkflow(rec, req, 9999, "t1")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("update not exist = %d, want 404", rec.Code)
	}

	// 跨租户
	req = newWFRequest(http.MethodPut, "/api/v1/workflows/"+strconv.FormatInt(wf.ID, 10), `{"name":"x"}`, "other")
	rec = httptest.NewRecorder()
	h.updateWorkflow(rec, req, wf.ID, "other")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("update cross tenant = %d, want 403", rec.Code)
	}

	// 错误方法
	req = newWFRequest(http.MethodGet, "/api/v1/workflows/"+strconv.FormatInt(wf.ID, 10), "", "t1")
	rec = httptest.NewRecorder()
	h.updateWorkflow(rec, req, wf.ID, "t1")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("update wrong method = %d, want 405", rec.Code)
	}

	// 非法 JSON
	req = newWFRequest(http.MethodPut, "/api/v1/workflows/"+strconv.FormatInt(wf.ID, 10), "not json", "t1")
	rec = httptest.NewRecorder()
	h.updateWorkflow(rec, req, wf.ID, "t1")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("update invalid json = %d, want 400", rec.Code)
	}

	// 非法 DAG JSON（parseNodes 错误分支）
	req = newWFRequest(http.MethodPut, "/api/v1/workflows/"+strconv.FormatInt(wf.ID, 10), `{"dag":"not json"}`, "t1")
	rec = httptest.NewRecorder()
	h.updateWorkflow(rec, req, wf.ID, "t1")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("update invalid dag json = %d, want 400", rec.Code)
	}

	// 非法 cron
	req = newWFRequest(http.MethodPut, "/api/v1/workflows/"+strconv.FormatInt(wf.ID, 10), `{"cron":"bad"}`, "t1")
	rec = httptest.NewRecorder()
	h.updateWorkflow(rec, req, wf.ID, "t1")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("update invalid cron = %d, want 400", rec.Code)
	}
}

// TestExtra_GetWorkflowErrors 验证 getWorkflow 错误分支。
func TestExtra_GetWorkflowErrors(t *testing.T) {
	st := store.NewMemoryStore()
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	wfs := NewMemory()
	h := NewHandler(wfs, st)

	wf := &WorkflowDef{Name: "wf", AgentID: a.AgentID, TenantID: "t1"}
	if err := wfs.Create(context.Background(), wf); err != nil {
		t.Fatal(err)
	}

	// 错误方法
	req := newWFRequest(http.MethodPost, "/api/v1/workflows/"+strconv.FormatInt(wf.ID, 10), "", "t1")
	rec := httptest.NewRecorder()
	h.getWorkflow(rec, req, wf.ID, "t1")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("get wrong method = %d, want 405", rec.Code)
	}

	// 不存在
	req = newWFRequest(http.MethodGet, "/api/v1/workflows/9999", "", "t1")
	rec = httptest.NewRecorder()
	h.getWorkflow(rec, req, 9999, "t1")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get not exist = %d, want 404", rec.Code)
	}
}

// TestExtra_HandlerMethodNotAllowed 验证 run/schedule/status 方法错误分支。
func TestExtra_HandlerMethodNotAllowed(t *testing.T) {
	st := store.NewMemoryStore()
	a := st.Register(&proto.AgentInfo{Segment: "seg-a", TenantID: "t1"})
	wfs := NewMemory()
	h := NewHandler(wfs, st)

	wf := &WorkflowDef{Name: "wf", AgentID: a.AgentID, TenantID: "t1"}
	if err := wfs.Create(context.Background(), wf); err != nil {
		t.Fatal(err)
	}

	// run 错误方法
	req := newWFRequest(http.MethodGet, "/api/v1/workflows/"+strconv.FormatInt(wf.ID, 10)+"/run", "", "t1")
	rec := httptest.NewRecorder()
	h.runWorkflow(rec, req, wf.ID, "t1")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("run get = %d, want 405", rec.Code)
	}

	// status 错误方法
	req = newWFRequest(http.MethodPost, "/api/v1/workflows/"+strconv.FormatInt(wf.ID, 10)+"/status", "", "t1")
	rec = httptest.NewRecorder()
	h.statusWorkflow(rec, req, wf.ID, "t1")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status post = %d, want 405", rec.Code)
	}

	// schedule 错误方法
	req = newWFRequest(http.MethodDelete, "/api/v1/workflows/"+strconv.FormatInt(wf.ID, 10)+"/schedule", "", "t1")
	rec = httptest.NewRecorder()
	h.scheduleWorkflow(rec, req, wf.ID, "t1")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("schedule delete = %d, want 405", rec.Code)
	}
}
