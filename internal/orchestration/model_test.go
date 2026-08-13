package orchestration

import (
	"encoding/json"
	"strings"
	"testing"
)

// mustDAG 把节点切片编码为 DAG JSON 字段（测试辅助）。
func mustDAG(t *testing.T, ns []WorkflowNode) string {
	t.Helper()
	b, err := json.Marshal(ns)
	if err != nil {
		t.Fatalf("marshal dag: %v", err)
	}
	return string(b)
}

// TestWorkflowNode_ValidTypes 验证所有有效节点类型（shell/file/service/workflow/condition）
// 均能通过 ValidateNodes。workflow 需配 SubWorkflowID，condition 需配 Condition。
func TestWorkflowNode_ValidTypes(t *testing.T) {
	ns := []WorkflowNode{
		{ID: "n1", Type: NodeShell, Command: "echo hi"},
		{ID: "n2", Type: NodeFile, Path: "/tmp/x"},
		{ID: "n3", Type: NodeService, Command: "restart"},
		{ID: "n4", Type: NodeWorkflow, SubWorkflowID: 42},
		{ID: "n5", Type: NodeCondition, Condition: `${n1.status} == "success"`, ThenNodes: []string{"n4"}},
	}
	if err := validateNodes(ns); err != nil {
		t.Fatalf("valid types rejected: %v", err)
	}
	// 同时验证 WorkflowDef.ValidateNodes 走 DAG JSON 解析路径。
	wf := &WorkflowDef{DAG: mustDAG(t, ns)}
	if err := wf.ValidateNodes(); err != nil {
		t.Fatalf("ValidateNodes via DAG JSON rejected: %v", err)
	}
}

// TestWorkflowNode_InvalidType 验证非法节点类型被拒绝。
func TestWorkflowNode_InvalidType(t *testing.T) {
	ns := []WorkflowNode{{ID: "n1", Type: "bogus"}}
	err := validateNodes(ns)
	if err == nil {
		t.Fatal("invalid type accepted")
	}
	if !strings.Contains(err.Error(), "非法类型") {
		t.Fatalf("error mismatch, want 非法类型, got: %v", err)
	}
}

// TestWorkflowNode_WorkflowRequiresSubID 验证 type=workflow 必须指定 SubWorkflowID>0。
func TestWorkflowNode_WorkflowRequiresSubID(t *testing.T) {
	cases := []struct {
		name string
		sub  int64
	}{
		{"zero", 0},
		{"negative", -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ns := []WorkflowNode{{ID: "n1", Type: NodeWorkflow, SubWorkflowID: c.sub}}
			err := validateNodes(ns)
			if err == nil {
				t.Fatalf("workflow with subWorkflowID=%d accepted", c.sub)
			}
			if !strings.Contains(err.Error(), "subWorkflowID") {
				t.Fatalf("error mismatch, want subWorkflowID, got: %v", err)
			}
		})
	}
	// 正例：SubWorkflowID>0 应通过。
	if err := validateNodes([]WorkflowNode{{ID: "n1", Type: NodeWorkflow, SubWorkflowID: 1}}); err != nil {
		t.Fatalf("workflow with subWorkflowID=1 rejected: %v", err)
	}
}

// TestWorkflowNode_ConditionRequiresExpression 验证 type=condition 必须指定 Condition 表达式。
func TestWorkflowNode_ConditionRequiresExpression(t *testing.T) {
	ns := []WorkflowNode{{ID: "n1", Type: NodeCondition, Condition: ""}}
	err := validateNodes(ns)
	if err == nil {
		t.Fatal("condition with empty expression accepted")
	}
	if !strings.Contains(err.Error(), "condition") {
		t.Fatalf("error mismatch, want condition, got: %v", err)
	}
	// 正例：带表达式应通过。
	if err := validateNodes([]WorkflowNode{
		{ID: "n1", Type: NodeShell, Command: "true"},
		{ID: "n2", Type: NodeCondition, Condition: `${n1.exitCode} == 0`, ThenNodes: []string{"n1"}},
	}); err != nil {
		t.Fatalf("condition with expression rejected: %v", err)
	}
}

// TestWorkflowNode_TimeoutValidation 验证超时值校验：负值拒绝，0/正值通过。
func TestWorkflowNode_TimeoutValidation(t *testing.T) {
	if err := validateNodes([]WorkflowNode{{ID: "n1", Type: NodeShell, Timeout: -1}}); err == nil {
		t.Fatal("timeout=-1 accepted")
	}
	for _, to := range []int{0, 30, 3600} {
		ns := []WorkflowNode{{ID: "n1", Type: NodeShell, Timeout: to}}
		if err := validateNodes(ns); err != nil {
			t.Fatalf("timeout=%d rejected: %v", to, err)
		}
	}
}

// TestWorkflowNode_RetryValidation 验证重试参数校验：RetryCount/RetryDelay 负值拒绝，0/正值通过。
func TestWorkflowNode_RetryValidation(t *testing.T) {
	if err := validateNodes([]WorkflowNode{{ID: "n1", Type: NodeShell, RetryCount: -1}}); err == nil {
		t.Fatal("retryCount=-1 accepted")
	}
	if err := validateNodes([]WorkflowNode{{ID: "n1", Type: NodeShell, RetryDelay: -1}}); err == nil {
		t.Fatal("retryDelay=-1 accepted")
	}
	for _, rc := range []int{0, 1, 5} {
		for _, rd := range []int{0, 10, 60} {
			ns := []WorkflowNode{{ID: "n1", Type: NodeShell, RetryCount: rc, RetryDelay: rd}}
			if err := validateNodes(ns); err != nil {
				t.Fatalf("retryCount=%d retryDelay=%d rejected: %v", rc, rd, err)
			}
		}
	}
}

// TestWorkflowDef_DuplicateNodeID 验证重复节点 ID 被拒绝。
func TestWorkflowDef_DuplicateNodeID(t *testing.T) {
	ns := []WorkflowNode{
		{ID: "n1", Type: NodeShell, Command: "a"},
		{ID: "n1", Type: NodeShell, Command: "b"},
	}
	err := validateNodes(ns)
	if err == nil {
		t.Fatal("duplicate node id accepted")
	}
	if !strings.Contains(err.Error(), "重复") {
		t.Fatalf("error mismatch, want 重复, got: %v", err)
	}
	// 通过 WorkflowDef.ValidateNodes 走 DAG JSON 路径复验。
	wf := &WorkflowDef{DAG: mustDAG(t, ns)}
	if err := wf.ValidateNodes(); err == nil {
		t.Fatal("duplicate node id via DAG JSON accepted")
	}
}

// TestWorkflowDef_DanglingDependsOn 验证 DependsOn 引用不存在的节点被拒绝。
func TestWorkflowDef_DanglingDependsOn(t *testing.T) {
	ns := []WorkflowNode{
		{ID: "n1", Type: NodeShell, Command: "a"},
		{ID: "n2", Type: NodeShell, Command: "b", DependsOn: []string{"ghost"}},
	}
	err := validateNodes(ns)
	if err == nil {
		t.Fatal("dangling dependsOn accepted")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("error mismatch, want ghost, got: %v", err)
	}
	// 通过 WorkflowDef.ValidateNodes 走 DAG JSON 路径复验。
	wf := &WorkflowDef{DAG: mustDAG(t, ns)}
	if err := wf.ValidateNodes(); err == nil {
		t.Fatal("dangling dependsOn via DAG JSON accepted")
	}
}

// TestWorkflowDef_Backcompat 验证向后兼容：原有 shell/file/service 节点不带新字段时校验通过，
// 且 JSON 反序列化不受新字段影响（omitempty 保证旧 JSON 行为不变）。
func TestWorkflowDef_Backcompat(t *testing.T) {
	// 旧风格 JSON（无任何新字段）。
	legacyDAG := `[{"id":"n1","type":"shell","command":"echo a"},` +
		`{"id":"n2","type":"file","path":"/tmp/x"},` +
		`{"id":"n3","type":"service","command":"restart","dependsOn":["n1","n2"]}]`
	wf := &WorkflowDef{DAG: legacyDAG}
	if err := wf.ValidateNodes(); err != nil {
		t.Fatalf("legacy dag rejected: %v", err)
	}
	ns, err := wf.Nodes()
	if err != nil {
		t.Fatalf("parse legacy dag: %v", err)
	}
	if len(ns) != 3 {
		t.Fatalf("parsed %d nodes, want 3", len(ns))
	}
	// 新字段在旧 JSON 下应保持零值。
	for i, want := range []string{"n1", "n2", "n3"} {
		if ns[i].ID != want {
			t.Fatalf("node[%d].id=%q, want %q", i, ns[i].ID, want)
		}
		if ns[i].Timeout != 0 || ns[i].RetryCount != 0 || ns[i].RetryDelay != 0 {
			t.Fatalf("node %s: new fields non-zero on legacy json", want)
		}
		if ns[i].Condition != "" || ns[i].SubWorkflowID != 0 || len(ns[i].ThenNodes) != 0 || len(ns[i].ElseNodes) != 0 {
			t.Fatalf("node %s: new fields non-zero on legacy json", want)
		}
	}
}
