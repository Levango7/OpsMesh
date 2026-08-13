package orchestration

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"

	"opsmesh/internal/proto"
)

// mockTaskEngine 是测试用的 TaskEngine mock，记录所有创建的任务（不处理依赖 blocked 状态）。
// 用于隔离测试 expandNodes 的展开逻辑，不引入 store.MemoryStore 的依赖引擎副作用。
type mockTaskEngine struct {
	mu    sync.Mutex
	tasks map[string]*proto.Task
}

func newMockTaskEngine() *mockTaskEngine {
	return &mockTaskEngine{tasks: make(map[string]*proto.Task)}
}

func (m *mockTaskEngine) CreateTask(t *proto.Task) *proto.Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t.Status == "" {
		t.Status = "pending"
	}
	m.tasks[t.TaskID] = t
	return t
}

func (m *mockTaskEngine) TasksByParent(parentID string) []*proto.Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*proto.Task
	for _, t := range m.tasks {
		if t.ParentID == parentID {
			out = append(out, t)
		}
	}
	return out
}

// hasTask 检查指定 ID 的任务是否被创建。
func (m *mockTaskEngine) hasTask(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.tasks[id]
	return ok
}

// taskCount 返回已创建任务总数。
func (m *mockTaskEngine) taskCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.tasks)
}

// getTask 返回指定 ID 的任务（不存在返回 nil）。
func (m *mockTaskEngine) getTask(id string) *proto.Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tasks[id]
}

// TestExpand_SubWorkflow 验证子工作流正确展开：子节点任务被创建，ID 带前缀，依赖正确映射。
// 父工作流 n1 → n2(workflow 引用 sub)，子工作流 s1 → s2，
// 展开后应产生 3 个任务：n1, n2-sub-s1（依赖 n1）, n2-sub-s2（依赖 n2-sub-s1）。
func TestExpand_SubWorkflow(t *testing.T) {
	wfs := NewMemory()
	eng := newMockTaskEngine()
	h := NewHandler(wfs, eng)
	ctx := context.Background()

	// 子工作流：s1 → s2
	sub := &WorkflowDef{
		Name: "sub", AgentID: "a1", TenantID: "t1",
		DAG: `[{"id":"s1","type":"shell","command":"echo sub1"},` +
			`{"id":"s2","type":"shell","command":"echo sub2","dependsOn":["s1"]}]`,
	}
	if err := wfs.Create(ctx, sub); err != nil {
		t.Fatal(err)
	}

	// 父工作流：n1 → n2(workflow 引用 sub)
	wf := &WorkflowDef{
		Name: "parent", AgentID: "a1", TenantID: "t1",
		DAG: `[{"id":"n1","type":"shell","command":"echo p1"},` +
			fmt.Sprintf(`{"id":"n2","type":"workflow","subWorkflowID":%d,"dependsOn":["n1"]}]`, sub.ID),
	}
	if err := wfs.Create(ctx, wf); err != nil {
		t.Fatal(err)
	}

	if _, err := h.Trigger(ctx, wf.ID, "t1"); err != nil {
		t.Fatalf("Trigger: %v", err)
	}

	prefix := "wf-" + strconv.FormatInt(wf.ID, 10) + "-"
	// 期望任务：n1, n2-sub-s1, n2-sub-s2
	expect := []string{prefix + "n1", prefix + "n2-sub-s1", prefix + "n2-sub-s2"}
	for _, id := range expect {
		if !eng.hasTask(id) {
			t.Errorf("expected task %s not created", id)
		}
	}
	if eng.taskCount() != 3 {
		t.Errorf("expected 3 tasks, got %d", eng.taskCount())
	}

	// s1 是子工作流入口节点，继承父节点 n2 的依赖 [n1]，映射后为 [prefix+n1]
	s1 := eng.getTask(prefix + "n2-sub-s1")
	if s1 == nil {
		t.Fatalf("s1 task not created")
	}
	if len(s1.DependsOn) != 1 || s1.DependsOn[0] != prefix+"n1" {
		t.Errorf("s1 deps=%v, want [%s]", s1.DependsOn, prefix+"n1")
	}
	// s2 依赖 [s1]，映射后为 [prefix+n2-sub-s1]
	s2 := eng.getTask(prefix + "n2-sub-s2")
	if s2 == nil {
		t.Fatalf("s2 task not created")
	}
	if len(s2.DependsOn) != 1 || s2.DependsOn[0] != prefix+"n2-sub-s1" {
		t.Errorf("s2 deps=%v, want [%s]", s2.DependsOn, prefix+"n2-sub-s1")
	}
}

// TestExpand_SubWorkflowNotFound 验证子工作流不存在时 Trigger 返回错误。
func TestExpand_SubWorkflowNotFound(t *testing.T) {
	wfs := NewMemory()
	eng := newMockTaskEngine()
	h := NewHandler(wfs, eng)
	ctx := context.Background()

	wf := &WorkflowDef{
		Name: "missing-sub", AgentID: "a1", TenantID: "t1",
		DAG: `[{"id":"n1","type":"workflow","subWorkflowID":9999}]`,
	}
	if err := wfs.Create(ctx, wf); err != nil {
		t.Fatal(err)
	}

	_, err := h.Trigger(ctx, wf.ID, "t1")
	if err == nil {
		t.Fatal("expected subworkflow not found error, got nil")
	}
	if !strings.Contains(err.Error(), "子工作流") {
		t.Fatalf("error mismatch, want 子工作流, got: %v", err)
	}
}

// TestExpand_SubWorkflowMaxDepth 验证超过最大递归深度时返回错误。
// 构造自引用工作流（DAG 中 workflow 节点引用自身），触发无限递归，由深度限制截断。
func TestExpand_SubWorkflowMaxDepth(t *testing.T) {
	wfs := NewMemory()
	eng := newMockTaskEngine()
	h := NewHandler(wfs, eng)
	ctx := context.Background()

	// 先创建空 DAG 的工作流以分配 ID，再更新 DAG 为自引用。
	wf := &WorkflowDef{Name: "self-ref", AgentID: "a1", TenantID: "t1"}
	if err := wfs.Create(ctx, wf); err != nil {
		t.Fatal(err)
	}
	wf.DAG = fmt.Sprintf(`[{"id":"n1","type":"workflow","subWorkflowID":%d}]`, wf.ID)
	if err := wfs.Update(ctx, wf); err != nil {
		t.Fatal(err)
	}

	_, err := h.Trigger(ctx, wf.ID, "t1")
	if err == nil {
		t.Fatal("expected max depth error, got nil")
	}
	if !strings.Contains(err.Error(), "递归深度") {
		t.Fatalf("error mismatch, want 递归深度, got: %v", err)
	}
}

// TestExpand_ConditionTrue 验证条件为 true 时 ThenNodes 被执行、ElseNodes 被跳过。
// 预先设置 n1 状态为 done，使 condition ${n1.status} == "done" 求值为 true。
func TestExpand_ConditionTrue(t *testing.T) {
	wfs := NewMemory()
	eng := newMockTaskEngine()
	h := NewHandler(wfs, eng)
	ctx := context.Background()

	wf := &WorkflowDef{
		Name: "cond-true", AgentID: "a1", TenantID: "t1",
		DAG: `[{"id":"n1","type":"shell","command":"echo"},` +
			`{"id":"c1","type":"condition","condition":"${n1.status} == \"done\"","thenNodes":["n2"],"elseNodes":["n3"]},` +
			`{"id":"n2","type":"shell","command":"echo then"},` +
			`{"id":"n3","type":"shell","command":"echo else"}]`,
	}
	if err := wfs.Create(ctx, wf); err != nil {
		t.Fatal(err)
	}

	prefix := "wf-" + strconv.FormatInt(wf.ID, 10) + "-"
	parent := "wf:" + strconv.FormatInt(wf.ID, 10)
	// 预先设置 n1 状态为 done，使 condition 求值为 true。
	// Trigger 收集 nodeStatuses 时读到 done，expandNodes 第一遍求值 condition 为 true。
	eng.tasks[prefix+"n1"] = &proto.Task{TaskID: prefix + "n1", Status: "done", ParentID: parent}

	if _, err := h.Trigger(ctx, wf.ID, "t1"); err != nil {
		t.Fatalf("Trigger: %v", err)
	}

	// condition 为 true：n2 被创建，n3 未被创建
	if !eng.hasTask(prefix + "n2") {
		t.Error("then node n2 not created")
	}
	if eng.hasTask(prefix + "n3") {
		t.Error("else node n3 should not be created")
	}
	// c1 是 condition 节点，不创建底层任务
	if eng.hasTask(prefix + "c1") {
		t.Error("condition node c1 should not create task")
	}
}

// TestExpand_ConditionFalse 验证条件为 false 时 ElseNodes 被执行、ThenNodes 被跳过。
// 不预先设置 n1 状态（nodeStatuses 为空），condition ${n1.status} == "done" 求值为 false。
func TestExpand_ConditionFalse(t *testing.T) {
	wfs := NewMemory()
	eng := newMockTaskEngine()
	h := NewHandler(wfs, eng)
	ctx := context.Background()

	wf := &WorkflowDef{
		Name: "cond-false", AgentID: "a1", TenantID: "t1",
		DAG: `[{"id":"n1","type":"shell","command":"echo"},` +
			`{"id":"c1","type":"condition","condition":"${n1.status} == \"done\"","thenNodes":["n2"],"elseNodes":["n3"]},` +
			`{"id":"n2","type":"shell","command":"echo then"},` +
			`{"id":"n3","type":"shell","command":"echo else"}]`,
	}
	if err := wfs.Create(ctx, wf); err != nil {
		t.Fatal(err)
	}

	if _, err := h.Trigger(ctx, wf.ID, "t1"); err != nil {
		t.Fatalf("Trigger: %v", err)
	}

	prefix := "wf-" + strconv.FormatInt(wf.ID, 10) + "-"
	// condition 为 false：n3 被创建，n2 未被创建
	if !eng.hasTask(prefix + "n3") {
		t.Error("else node n3 not created")
	}
	if eng.hasTask(prefix + "n2") {
		t.Error("then node n2 should not be created")
	}
	// c1 是 condition 节点，不创建底层任务
	if eng.hasTask(prefix + "c1") {
		t.Error("condition node c1 should not create task")
	}
}

// TestEvalCondition_SimpleStatus 验证 ${nodeID.status} == "success" 求值。
func TestEvalCondition_SimpleStatus(t *testing.T) {
	statuses := map[string]string{"n1": "done"}
	if !evalCondition(`${n1.status} == "done"`, statuses) {
		t.Error(`expected true for ${n1.status} == "done"`)
	}
	if evalCondition(`${n1.status} == "failed"`, statuses) {
		t.Error(`expected false for ${n1.status} == "failed"`)
	}
	// 节点不存在 → status 为空串
	if evalCondition(`${ghost.status} == "done"`, statuses) {
		t.Error(`expected false for non-existent node`)
	}
}

// TestEvalCondition_ExitCode 验证 ${nodeID.exitCode} == 0 求值。
// exitCode 从 status 推断：done=0, failed=1, 其他=空。
func TestEvalCondition_ExitCode(t *testing.T) {
	statuses := map[string]string{"n1": "done", "n2": "failed", "n3": "pending"}
	// done → exitCode 0
	if !evalCondition(`${n1.exitCode} == 0`, statuses) {
		t.Error("expected true for ${n1.exitCode} == 0 (done)")
	}
	// failed → exitCode 1
	if !evalCondition(`${n2.exitCode} == 1`, statuses) {
		t.Error("expected true for ${n2.exitCode} == 1 (failed)")
	}
	// pending → exitCode 空，不等于 0
	if evalCondition(`${n3.exitCode} == 0`, statuses) {
		t.Error("expected false for ${n3.exitCode} == 0 (pending)")
	}
	// done → exitCode 0，不等于 1
	if !evalCondition(`${n1.exitCode} != 1`, statuses) {
		t.Error("expected true for ${n1.exitCode} != 1 (done=0)")
	}
}

// TestEvalCondition_AndOr 验证 && 和 || 组合求值。
func TestEvalCondition_AndOr(t *testing.T) {
	statuses := map[string]string{"n1": "done", "n2": "failed"}
	// && 两者都为 true
	if !evalCondition(`${n1.status} == "done" && ${n2.status} == "failed"`, statuses) {
		t.Error("expected true for && both true")
	}
	// && 一真一假
	if evalCondition(`${n1.status} == "done" && ${n2.status} == "done"`, statuses) {
		t.Error("expected false for && one false")
	}
	// || 一真一假
	if !evalCondition(`${n1.status} == "done" || ${n2.status} == "done"`, statuses) {
		t.Error("expected true for || one true")
	}
	// || 两者都假
	if evalCondition(`${n1.status} == "failed" || ${n2.status} == "done"`, statuses) {
		t.Error("expected false for || both false")
	}
	// 三元组合：&& 优先于 ||
	if !evalCondition(`${n1.status} == "failed" && ${n2.status} == "done" || ${n1.status} == "done"`, statuses) {
		t.Error("expected true for (false && false) || true")
	}
}

// TestEvalCondition_NotEqual 验证 != 操作。
func TestEvalCondition_NotEqual(t *testing.T) {
	statuses := map[string]string{"n1": "done"}
	if !evalCondition(`${n1.status} != "failed"`, statuses) {
		t.Error(`expected true for ${n1.status} != "failed"`)
	}
	if evalCondition(`${n1.status} != "done"`, statuses) {
		t.Error(`expected false for ${n1.status} != "done"`)
	}
	// exitCode != 0 (done=0, 所以 != 0 为 false)
	if evalCondition(`${n1.exitCode} != 0`, statuses) {
		t.Error("expected false for ${n1.exitCode} != 0 (done=0)")
	}
	// exitCode != 1 (done=0, 所以 != 1 为 true)
	if !evalCondition(`${n1.exitCode} != 1`, statuses) {
		t.Error("expected true for ${n1.exitCode} != 1 (done=0)")
	}
}

// TestExpand_TimeoutRetryPassthrough 验证 expandNodes 把节点级 Timeout/RetryCount/RetryDelay
// 正确传递给底层任务（P2-B2 任务 261）。
// 节点 n1: timeout=30, retryCount=2, retryDelay=5 → 任务 Timeout=30, MaxRetries=2, RetryDelay=5
// 节点 n2: 全 0（默认）→ 任务全 0（用全局配置）
func TestExpand_TimeoutRetryPassthrough(t *testing.T) {
	wfs := NewMemory()
	eng := newMockTaskEngine()
	h := NewHandler(wfs, eng)
	ctx := context.Background()

	wf := &WorkflowDef{
		Name: "timeout-retry", AgentID: "a1", TenantID: "t1",
		DAG: `[{"id":"n1","type":"shell","command":"echo hi","timeout":30,"retryCount":2,"retryDelay":5},` +
			`{"id":"n2","type":"shell","command":"echo lo"}]`,
	}
	if err := wfs.Create(ctx, wf); err != nil {
		t.Fatal(err)
	}

	if _, err := h.Trigger(ctx, wf.ID, "t1"); err != nil {
		t.Fatalf("Trigger: %v", err)
	}

	prefix := "wf-" + strconv.FormatInt(wf.ID, 10) + "-"

	// n1: 节点级参数应原样透传到底层任务。
	n1 := eng.getTask(prefix + "n1")
	if n1 == nil {
		t.Fatalf("n1 task not created")
	}
	if n1.Timeout != 30 {
		t.Errorf("n1 timeout=%d, want 30", n1.Timeout)
	}
	if n1.MaxRetries != 2 {
		t.Errorf("n1 maxRetries=%d, want 2", n1.MaxRetries)
	}
	if n1.RetryDelay != 5 {
		t.Errorf("n1 retryDelay=%d, want 5", n1.RetryDelay)
	}

	// n2: 未设节点级参数，任务字段应为 0（agent 端回退全局 taskTimeout，store 不重试）。
	n2 := eng.getTask(prefix + "n2")
	if n2 == nil {
		t.Fatalf("n2 task not created")
	}
	if n2.Timeout != 0 || n2.MaxRetries != 0 || n2.RetryDelay != 0 {
		t.Errorf("n2 timeout/maxRetries/retryDelay = %d/%d/%d, want 0/0/0",
			n2.Timeout, n2.MaxRetries, n2.RetryDelay)
	}
}

// TestExpand_TimeoutRetryAllNodeTypes 验证 shell/file/service 三种节点类型均传递超时与重试参数。
func TestExpand_TimeoutRetryAllNodeTypes(t *testing.T) {
	wfs := NewMemory()
	eng := newMockTaskEngine()
	h := NewHandler(wfs, eng)
	ctx := context.Background()

	wf := &WorkflowDef{
		Name: "all-types", AgentID: "a1", TenantID: "t1",
		DAG: `[{"id":"sh","type":"shell","command":"echo","timeout":10,"retryCount":1,"retryDelay":2},` +
			`{"id":"fi","type":"file","path":"/tmp/x","timeout":20,"retryCount":3,"retryDelay":4},` +
			`{"id":"sv","type":"service","command":"restart","timeout":40,"retryCount":5,"retryDelay":6}]`,
	}
	if err := wfs.Create(ctx, wf); err != nil {
		t.Fatal(err)
	}

	if _, err := h.Trigger(ctx, wf.ID, "t1"); err != nil {
		t.Fatalf("Trigger: %v", err)
	}

	prefix := "wf-" + strconv.FormatInt(wf.ID, 10) + "-"
	cases := []struct {
		id                    string
		timeout, maxR, retryD int
	}{
		{"sh", 10, 1, 2},
		{"fi", 20, 3, 4},
		{"sv", 40, 5, 6},
	}
	for _, c := range cases {
		task := eng.getTask(prefix + c.id)
		if task == nil {
			t.Fatalf("%s task not created", c.id)
		}
		if task.Timeout != c.timeout || task.MaxRetries != c.maxR || task.RetryDelay != c.retryD {
			t.Errorf("node %s: timeout/maxRetries/retryDelay = %d/%d/%d, want %d/%d/%d",
				c.id, task.Timeout, task.MaxRetries, task.RetryDelay,
				c.timeout, c.maxR, c.retryD)
		}
	}
}

// TestExpand_TimeoutRetrySubWorkflow 验证子工作流节点的超时与重试参数也正确传递。
// 子工作流节点自身的 Timeout/RetryCount/RetryDelay 在展开时传递给子节点任务。
func TestExpand_TimeoutRetrySubWorkflow(t *testing.T) {
	wfs := NewMemory()
	eng := newMockTaskEngine()
	h := NewHandler(wfs, eng)
	ctx := context.Background()

	// 子工作流：s1 自带超时/重试参数
	sub := &WorkflowDef{
		Name: "sub", AgentID: "a1", TenantID: "t1",
		DAG: `[{"id":"s1","type":"shell","command":"echo sub","timeout":15,"retryCount":2,"retryDelay":3}]`,
	}
	if err := wfs.Create(ctx, sub); err != nil {
		t.Fatal(err)
	}

	// 父工作流：n1 → n2(workflow 引用 sub)
	wf := &WorkflowDef{
		Name: "parent", AgentID: "a1", TenantID: "t1",
		DAG: `[{"id":"n1","type":"shell","command":"echo p1","timeout":60,"retryCount":1,"retryDelay":1},` +
			fmt.Sprintf(`{"id":"n2","type":"workflow","subWorkflowID":%d,"dependsOn":["n1"]}]`, sub.ID),
	}
	if err := wfs.Create(ctx, wf); err != nil {
		t.Fatal(err)
	}

	if _, err := h.Trigger(ctx, wf.ID, "t1"); err != nil {
		t.Fatalf("Trigger: %v", err)
	}

	prefix := "wf-" + strconv.FormatInt(wf.ID, 10) + "-"

	// 父节点 n1 的超时/重试参数应透传。
	n1 := eng.getTask(prefix + "n1")
	if n1 == nil {
		t.Fatalf("n1 task not created")
	}
	if n1.Timeout != 60 || n1.MaxRetries != 1 || n1.RetryDelay != 1 {
		t.Errorf("n1 timeout/maxRetries/retryDelay = %d/%d/%d, want 60/1/1",
			n1.Timeout, n1.MaxRetries, n1.RetryDelay)
	}

	// 子节点 s1 的超时/重试参数应透传（来自子工作流定义中 s1 节点自身的设置）。
	s1 := eng.getTask(prefix + "n2-sub-s1")
	if s1 == nil {
		t.Fatalf("s1 task not created")
	}
	if s1.Timeout != 15 || s1.MaxRetries != 2 || s1.RetryDelay != 3 {
		t.Errorf("s1 timeout/maxRetries/retryDelay = %d/%d/%d, want 15/2/3",
			s1.Timeout, s1.MaxRetries, s1.RetryDelay)
	}
}

// TestExpand_TimeoutRetryConditionBranch 验证 condition 选中分支节点的超时/重试参数正确传递。
func TestExpand_TimeoutRetryConditionBranch(t *testing.T) {
	wfs := NewMemory()
	eng := newMockTaskEngine()
	h := NewHandler(wfs, eng)
	ctx := context.Background()

	wf := &WorkflowDef{
		Name: "cond-branch", AgentID: "a1", TenantID: "t1",
		DAG: `[{"id":"n1","type":"shell","command":"echo"},` +
			`{"id":"c1","type":"condition","condition":"${n1.status} == \"done\"","thenNodes":["n2"],"elseNodes":["n3"]},` +
			`{"id":"n2","type":"shell","command":"echo then","timeout":12,"retryCount":2,"retryDelay":3},` +
			`{"id":"n3","type":"shell","command":"echo else","timeout":45,"retryCount":5,"retryDelay":6}]`,
	}
	if err := wfs.Create(ctx, wf); err != nil {
		t.Fatal(err)
	}

	prefix := "wf-" + strconv.FormatInt(wf.ID, 10) + "-"
	parent := "wf:" + strconv.FormatInt(wf.ID, 10)
	// 预设 n1 状态为 done，使 condition 求值为 true，选中 then 分支 n2。
	eng.tasks[prefix+"n1"] = &proto.Task{TaskID: prefix + "n1", Status: "done", ParentID: parent}

	if _, err := h.Trigger(ctx, wf.ID, "t1"); err != nil {
		t.Fatalf("Trigger: %v", err)
	}

	// n2 被创建且超时/重试参数正确。
	n2 := eng.getTask(prefix + "n2")
	if n2 == nil {
		t.Fatalf("n2 task not created")
	}
	if n2.Timeout != 12 || n2.MaxRetries != 2 || n2.RetryDelay != 3 {
		t.Errorf("n2 timeout/maxRetries/retryDelay = %d/%d/%d, want 12/2/3",
			n2.Timeout, n2.MaxRetries, n2.RetryDelay)
	}
	// n3 未被创建（else 分支跳过）。
	if eng.hasTask(prefix + "n3") {
		t.Error("else node n3 should not be created")
	}
}
