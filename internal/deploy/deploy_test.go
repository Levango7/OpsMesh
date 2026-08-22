package deploy

import (
	"context"
	"fmt"
	"testing"

	"opsmesh/internal/proto"
)

// fakeDisp 是 Dispatcher 的测试桩：记录派发的任务并提供可控的设备/状态。
type fakeDisp struct {
	tasks   map[string]*proto.Task
	devices map[string]*proto.DeviceInfo
}

func newFakeDisp() *fakeDisp {
	return &fakeDisp{tasks: make(map[string]*proto.Task), devices: make(map[string]*proto.DeviceInfo)}
}

func (f *fakeDisp) CreateTask(t *proto.Task) *proto.Task {
	cp := *t
	cp.TaskID = fmt.Sprintf("task-%d", len(f.tasks)+1)
	f.tasks[cp.TaskID] = &cp
	return &cp
}

func (f *fakeDisp) Device(id string) *proto.DeviceInfo { return f.devices[id] }

func (f *fakeDisp) TaskStates(ids []string, _ string) map[string]string {
	out := make(map[string]string)
	for _, id := range ids {
		if t, ok := f.tasks[id]; ok {
			out[id] = t.Status
		}
	}
	return out
}

func newDeploy(name, typ, targets, tenant string) *DeployTask {
	return &DeployTask{Name: name, Type: typ, TargetIDs: targets, TenantID: tenant, CreatedBy: "tester"}
}

func TestMemoryCreateGetTenant(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	dt, err := st.Create(ctx, newDeploy("web", TypeScript, "dev-1", "t1"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if dt.ID == 0 || dt.Status != StatusCreated {
		t.Fatalf("unexpected: %#v", dt)
	}
	// 同租户可读。
	got, err := st.Get(ctx, dt.ID, "t1")
	if err != nil || got.Name != "web" {
		t.Fatalf("get: %v %#v", err, got)
	}
	// 跨租户拒绝。
	if _, err := st.Get(ctx, dt.ID, "t2"); err != ErrTenantMismatch {
		t.Fatalf("want tenant mismatch, got %v", err)
	}
}

func TestExecuteDispatchesAndReconcile(t *testing.T) {
	ctx := context.Background()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "agentA"}
	disp.devices["dev-2"] = &proto.DeviceInfo{DeviceID: "dev-2", AgentID: "agentB"}
	st := NewMemory()
	h := NewHandler(st, disp)

	dt, err := st.Create(ctx, newDeploy("svc", TypeScript, "dev-1, dev-2", "t1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Execute(ctx, dt.ID, "t1"); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got, _ := st.Get(ctx, dt.ID, "t1")
	if got.Status != StatusRunning {
		t.Fatalf("want running, got %s", got.Status)
	}
	if len(disp.tasks) != 2 {
		t.Fatalf("want 2 dispatched tasks, got %d", len(disp.tasks))
	}
	// 把底层任务标 done，再对账 -> success。
	for id := range disp.tasks {
		disp.tasks[id].Status = "done"
	}
	if err := h.Reconcile(ctx, dt.ID, "t1"); err != nil {
		t.Fatal(err)
	}
	got, _ = st.Get(ctx, dt.ID, "t1")
	if got.Status != StatusSuccess {
		t.Fatalf("want success after reconcile, got %s", got.Status)
	}
	// 回滚。
	if err := h.Rollback(ctx, dt.ID, "t1"); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	got, _ = st.Get(ctx, dt.ID, "t1")
	if got.Status != StatusRolledBack {
		t.Fatalf("want rolledback, got %s", got.Status)
	}
}

func TestExecuteFailsWhenNoTarget(t *testing.T) {
	ctx := context.Background()
	disp := newFakeDisp() // 无设备
	st := NewMemory()
	h := NewHandler(st, disp)
	dt, _ := st.Create(ctx, newDeploy("x", TypeFile, "ghost", "t1"))
	if err := h.Execute(ctx, dt.ID, "t1"); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(ctx, dt.ID, "t1")
	if got.Status != StatusFailed {
		t.Fatalf("want failed when no agent, got %s", got.Status)
	}
}

func TestExecuteRejectsNonCreated(t *testing.T) {
	ctx := context.Background()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	st := NewMemory()
	h := NewHandler(st, disp)
	dt, _ := st.Create(ctx, newDeploy("x", TypeScript, "dev-1", "t1"))
	_ = h.Execute(ctx, dt.ID, "t1")
	// 已 running，再 execute 应拒绝。
	if err := h.Execute(ctx, dt.ID, "t1"); err == nil {
		t.Fatal("want error executing non-created deploy")
	}
}

func TestReconcileDetectsFailed(t *testing.T) {
	ctx := context.Background()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	st := NewMemory()
	h := NewHandler(st, disp)
	dt, _ := st.Create(ctx, newDeploy("x", TypeScript, "dev-1", "t1"))
	_ = h.Execute(ctx, dt.ID, "t1")
	// 底层任务标 failed -> 对账后部署 failed。
	for id := range disp.tasks {
		disp.tasks[id].Status = "failed"
	}
	_ = h.Reconcile(ctx, dt.ID, "t1")
	got, _ := st.Get(ctx, dt.ID, "t1")
	if got.Status != StatusFailed {
		t.Fatalf("want failed after reconcile, got %s", got.Status)
	}
}

// TestValidateRepoURL 验证 ：RepoURL 命令注入防护。
func TestValidateRepoURL(t *testing.T) {
	// 合法仓库地址应通过。
	for _, ok := range []string{
		"https://git.example.com/ops/nginx.git",
		"http://gitlab.local/repo.git",
		"git@github.com:org/repo.git",
		"ssh://git@git.internal/repo.git",
		"/opt/scripts/deploy.sh",
	} {
		if err := validateRepoURL(ok); err != nil {
			t.Errorf("合法 RepoURL %q 不应被拒绝: %v", ok, err)
		}
	}
	// 含 shell 元字符的注入载荷应被拒绝。
	for _, bad := range []string{
		"https://x.git; rm -rf /",
		"repo`whoami`.git",
		"https://x.git && curl evil",
		"$(reboot)",
		"a|b",
		"https://x.git\nrm -rf /",
	} {
		if err := validateRepoURL(bad); err == nil {
			t.Errorf("RepoURL %q 应被拒绝", bad)
		}
	}
	// 非法 scheme 应被拒绝。
	if err := validateRepoURL("evil.example.com/repo"); err == nil {
		t.Error("无 scheme 的 RepoURL 应被拒绝")
	}
}

// TestCreateRejectsMaliciousRepoURL 端到端：恶意 RepoURL 在创建时被拒绝。
func TestCreateRejectsMaliciousRepoURL(t *testing.T) {
	st := NewMemory()
	dt := newDeploy("evil", TypeScript, "dev-1", "t1")
	dt.RepoURL = "https://x.git; reboot"
	if _, err := st.Create(context.Background(), dt); err == nil {
		t.Fatal("含 shell 元字符的 RepoURL 应在创建时被拒绝")
	}
}
