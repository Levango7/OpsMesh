// federation_test.go 测试多集群联邦发布协调器（task 280）：存储 CRUD、计划校验、
// Start/Promote/Reconcile/Rollback/Status 状态流转（sequential/parallel）、自动回滚、HTTP API 集成。
package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"opsmesh/internal/proto"
)

// =============================================================================
// 测试辅助：fakeDeployExecutor 可控的成员子部署派发桩
// =============================================================================

// fakeDeployExecutor 实现 DeployExecutor，可控返回子部署 ID 与状态。
type fakeDeployExecutor struct {
	mu         sync.Mutex
	nextID     int64
	statuses   map[int64]string // deployID -> status
	promoted   []int64
	rolledback []int64
	createErr  error
	promoteErr error
}

func newFakeExec() *fakeDeployExecutor {
	return &fakeDeployExecutor{statuses: make(map[int64]string)}
}

func (f *fakeDeployExecutor) CreateAndExecute(ctx context.Context, template *DeployTask, targetIDs, tenantID string) (int64, error) {
	if f.createErr != nil {
		return 0, f.createErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	id := f.nextID
	f.statuses[id] = StatusRunning
	return id, nil
}

func (f *fakeDeployExecutor) MemberStatus(ctx context.Context, deployID int64, tenantID string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok := f.statuses[deployID]
	if !ok {
		return "", ErrNotFound
	}
	return st, nil
}

func (f *fakeDeployExecutor) PromoteMember(ctx context.Context, deployID int64, tenantID string) error {
	if f.promoteErr != nil {
		return f.promoteErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.promoted = append(f.promoted, deployID)
	f.statuses[deployID] = StatusSuccess
	return nil
}

func (f *fakeDeployExecutor) RollbackMember(ctx context.Context, deployID int64, tenantID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rolledback = append(f.rolledback, deployID)
	f.statuses[deployID] = StatusRolledBack
	return nil
}

// setStatus 外部设置成员子部署状态（模拟成员执行终态）。
func (f *fakeDeployExecutor) setStatus(deployID int64, status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses[deployID] = status
}

// newFederationPlan 构造测试用联邦发布计划（3 成员 sequential，Order 0/1/2）。
func newFederationPlan(name, tenant string) *FederationDeploy {
	return &FederationDeploy{
		Name:     name,
		TenantID: tenant,
		Mode:     FedModeSequential,
		Template: DeployTask{
			Name: "svc",
			Type: TypeScript,
		},
		Members: []FederationMember{
			{ClusterID: "cluster-a", Name: "A", TargetIDs: "dev-1", Order: 0, Weight: 100},
			{ClusterID: "cluster-b", Name: "B", TargetIDs: "dev-2", Order: 1, Weight: 100},
			{ClusterID: "cluster-c", Name: "C", TargetIDs: "dev-3", Order: 2, Weight: 100},
		},
	}
}

// =============================================================================
// MemoryFederationStore CRUD
// =============================================================================

func TestMemoryFederationStore_CRUD(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryFederationStore()

	f, err := s.Create(ctx, newFederationPlan("fed-1", "t1"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if f.ID == 0 || f.Status != FedStatusPending {
		t.Fatalf("unexpected: %#v", f)
	}
	if len(f.Members) != 3 {
		t.Fatalf("want 3 members, got %d", len(f.Members))
	}

	// 同租户可读。
	got, err := s.Get(ctx, f.ID, "t1")
	if err != nil || got.Name != "fed-1" {
		t.Fatalf("get: %v %#v", err, got)
	}
	// 跨租户拒绝。
	if _, err := s.Get(ctx, f.ID, "t2"); err != ErrFedTenantMismatch {
		t.Fatalf("want tenant mismatch, got %v", err)
	}

	// Update。
	got.Status = FedStatusCanary
	if err := s.Update(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	got2, _ := s.Get(ctx, f.ID, "t1")
	if got2.Status != FedStatusCanary {
		t.Fatalf("want canary, got %s", got2.Status)
	}

	// List。
	list, err := s.List(ctx, "t1", "")
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v len=%d", err, len(list))
	}
	listByStatus, _ := s.List(ctx, "t1", FedStatusCanary)
	if len(listByStatus) != 1 {
		t.Fatalf("list by status canary want 1, got %d", len(listByStatus))
	}

	// Delete。
	if err := s.Delete(ctx, f.ID, "t1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Get(ctx, f.ID, "t1"); err != ErrFedNotFound {
		t.Fatalf("want not found after delete, got %v", err)
	}
}

func TestMemoryFederationStore_DeepCopyMembers(t *testing.T) {
	// 验证 Update 不会因外部修改切片而污染存储（深拷贝 Members）。
	ctx := context.Background()
	s := NewMemoryFederationStore()
	f, _ := s.Create(ctx, newFederationPlan("fed", "t1"))

	// 外部修改 Get 返回的 Members。
	got, _ := s.Get(ctx, f.ID, "t1")
	got.Members[0].Status = StatusSuccess
	// 再次 Get 应不受影响。
	got2, _ := s.Get(ctx, f.ID, "t1")
	if got2.Members[0].Status == StatusSuccess {
		t.Fatal("Members should be deep-copied, external mutation leaked into store")
	}
}

// =============================================================================
// FederationDeploy.Valid
// =============================================================================

func TestFederationDeploy_Valid(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryFederationStore()

	cases := []struct {
		name string
		mut  func(*FederationDeploy)
		wantErr bool
	}{
		{"valid", func(f *FederationDeploy) {}, false},
		{"empty name", func(f *FederationDeploy) { f.Name = "" }, true},
		{"bad mode", func(f *FederationDeploy) { f.Mode = "bad" }, true},
		{"no members", func(f *FederationDeploy) { f.Members = nil }, true},
		{"dup cluster", func(f *FederationDeploy) {
			f.Members[1].ClusterID = f.Members[0].ClusterID
		}, true},
		{"empty target", func(f *FederationDeploy) { f.Members[0].TargetIDs = "" }, true},
		{"bad weight", func(f *FederationDeploy) { f.Members[0].Weight = 101 }, true},
		{"bad gate", func(f *FederationDeploy) {
			f.Gate = &GateConfig{SuccessRate: 200}
		}, true},
		{"parallel ok", func(f *FederationDeploy) { f.Mode = FedModeParallel }, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFederationPlan("x", "t1")
			c.mut(f)
			_, err := s.Create(ctx, f)
			if c.wantErr && err == nil {
				t.Fatal("want error, got nil")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("want ok, got %v", err)
			}
		})
	}
}

// =============================================================================
// FederationCoordinator Start / Reconcile / Promote（sequential）
// =============================================================================

func TestFederationCoordinator_Start_Sequential(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryFederationStore()
	exec := newFakeExec()
	c := NewFederationCoordinator(s, exec)

	f, _ := s.Create(ctx, newFederationPlan("fed", "t1"))
	if err := c.Start(ctx, f.ID, "t1"); err != nil {
		t.Fatalf("start: %v", err)
	}
	got, _ := s.Get(ctx, f.ID, "t1")
	// sequential 首批仅派发 Order=0 的 cluster-a。
	if got.Members[0].DeployID == 0 || got.Members[1].DeployID != 0 {
		t.Fatalf("only first-batch should be dispatched: %#v", got.Members)
	}
	if got.Status != FedStatusCanary {
		t.Fatalf("want fed_canary, got %s", got.Status)
	}
}

func TestFederationCoordinator_Start_WrongStatus(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryFederationStore()
	exec := newFakeExec()
	c := NewFederationCoordinator(s, exec)

	f, _ := s.Create(ctx, newFederationPlan("fed", "t1"))
	_ = c.Start(ctx, f.ID, "t1")
	// 重复 Start 应失败。
	if err := c.Start(ctx, f.ID, "t1"); err == nil {
		t.Fatal("start from non-pending should fail")
	}
}

func TestFederationCoordinator_Reconcile_GatedThenPromote_Sequential(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryFederationStore()
	exec := newFakeExec()
	c := NewFederationCoordinator(s, exec)

	f, _ := s.Create(ctx, newFederationPlan("fed", "t1"))
	_ = c.Start(ctx, f.ID, "t1")
	got, _ := s.Get(ctx, f.ID, "t1")
	firstDeployID := got.Members[0].DeployID

	// 模拟首批成员成功。
	exec.setStatus(firstDeployID, StatusSuccess)
	if err := c.Reconcile(ctx, f.ID, "t1"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got, _ = s.Get(ctx, f.ID, "t1")
	if got.Status != FedStatusGated {
		t.Fatalf("want fed_gated after first batch success, got %s", got.Status)
	}

	// Promote 推进到下一批（cluster-b）。
	if err := c.Promote(ctx, f.ID, "t1"); err != nil {
		t.Fatalf("promote: %v", err)
	}
	got, _ = s.Get(ctx, f.ID, "t1")
	if got.Members[1].DeployID == 0 {
		t.Fatal("second member should be dispatched after promote")
	}
	if got.Status != FedStatusPromoting {
		t.Fatalf("want fed_promoting, got %s", got.Status)
	}

	// 第二批成功 -> gated。
	exec.setStatus(got.Members[1].DeployID, StatusSuccess)
	_ = c.Reconcile(ctx, f.ID, "t1")
	got, _ = s.Get(ctx, f.ID, "t1")
	if got.Status != FedStatusGated {
		t.Fatalf("want fed_gated after second batch, got %s", got.Status)
	}

	// Promote 推进到第三批（cluster-c）。
	_ = c.Promote(ctx, f.ID, "t1")
	got, _ = s.Get(ctx, f.ID, "t1")
	if got.Members[2].DeployID == 0 {
		t.Fatal("third member should be dispatched")
	}
	// 第三批成功 -> 全部成功 -> fed_success。
	exec.setStatus(got.Members[2].DeployID, StatusSuccess)
	_ = c.Reconcile(ctx, f.ID, "t1")
	got, _ = s.Get(ctx, f.ID, "t1")
	if got.Status != FedStatusSuccess {
		t.Fatalf("want fed_success, got %s", got.Status)
	}
}

func TestFederationCoordinator_Promote_GateNotPassed(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryFederationStore()
	exec := newFakeExec()
	c := NewFederationCoordinator(s, exec)

	f, _ := s.Create(ctx, newFederationPlan("fed", "t1"))
	_ = c.Start(ctx, f.ID, "t1")
	got, _ := s.Get(ctx, f.ID, "t1")
	// 首批成员仍 running（未终态），门禁不通过。
	if err := c.Promote(ctx, f.ID, "t1"); err == nil {
		t.Fatal("promote should fail when gate not passed (member not terminal)")
	}
	// 标记首批失败，reconcile -> fed_failed。
	exec.setStatus(got.Members[0].DeployID, StatusFailed)
	_ = c.Reconcile(ctx, f.ID, "t1")
	got, _ = s.Get(ctx, f.ID, "t1")
	if got.Status != FedStatusFailed {
		t.Fatalf("want fed_failed, got %s", got.Status)
	}
}

func TestFederationCoordinator_Reconcile_AutoRollback(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryFederationStore()
	exec := newFakeExec()
	c := NewFederationCoordinator(s, exec)

	f, _ := s.Create(ctx, newFederationPlan("fed", "t1"))
	f.AutoRollback = true
	_ = s.Update(ctx, f)
	_ = c.Start(ctx, f.ID, "t1")
	got, _ := s.Get(ctx, f.ID, "t1")
	// 首批失败 -> 自动回滚。
	exec.setStatus(got.Members[0].DeployID, StatusFailed)
	if err := c.Reconcile(ctx, f.ID, "t1"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got, _ = s.Get(ctx, f.ID, "t1")
	if got.Status != FedStatusRolledBack {
		t.Fatalf("want fed_rolledback, got %s", got.Status)
	}
	if got.Members[0].Status != StatusRolledBack {
		t.Fatalf("member should be rolledback, got %s", got.Members[0].Status)
	}
	if len(exec.rolledback) != 1 {
		t.Fatalf("rollback should be called once, got %d", len(exec.rolledback))
	}
}

func TestFederationCoordinator_Rollback_Manual(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryFederationStore()
	exec := newFakeExec()
	c := NewFederationCoordinator(s, exec)

	f, _ := s.Create(ctx, newFederationPlan("fed", "t1"))
	_ = c.Start(ctx, f.ID, "t1")
	if err := c.Rollback(ctx, f.ID, "t1"); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	got, _ := s.Get(ctx, f.ID, "t1")
	if got.Status != FedStatusRolledBack {
		t.Fatalf("want fed_rolledback, got %s", got.Status)
	}
	if len(exec.rolledback) != 1 {
		t.Fatalf("rollback should be called for dispatched member, got %d", len(exec.rolledback))
	}
	// 从 rolledback 状态再次回滚应失败。
	if err := c.Rollback(ctx, f.ID, "t1"); err == nil {
		t.Fatal("rollback from rolledback should fail")
	}
}

func TestFederationCoordinator_Status(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryFederationStore()
	exec := newFakeExec()
	c := NewFederationCoordinator(s, exec)

	f, _ := s.Create(ctx, newFederationPlan("fed", "t1"))
	_ = c.Start(ctx, f.ID, "t1")
	got, _ := s.Get(ctx, f.ID, "t1")
	exec.setStatus(got.Members[0].DeployID, StatusSuccess)
	_ = c.Reconcile(ctx, f.ID, "t1")

	st, err := c.Status(ctx, f.ID, "t1")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.TotalMembers != 3 {
		t.Fatalf("want 3 total, got %d", st.TotalMembers)
	}
	if st.DoneMembers != 1 {
		t.Fatalf("want 1 done, got %d", st.DoneMembers)
	}
	if st.PendingMembers != 2 {
		t.Fatalf("want 2 pending, got %d", st.PendingMembers)
	}
	if st.OverallStatus != FedStatusGated {
		t.Fatalf("want fed_gated, got %s", st.OverallStatus)
	}
}

// =============================================================================
// Parallel 模式
// =============================================================================

func TestFederationCoordinator_Parallel(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryFederationStore()
	exec := newFakeExec()
	c := NewFederationCoordinator(s, exec)

	plan := newFederationPlan("fed", "t1")
	plan.Mode = FedModeParallel
	// parallel 模式成员内部用 canary 策略，promote 时晋级成员子部署。
	plan.Template.Strategy = StrategyCanary
	plan.Template.CanaryWeight = 50
	f, _ := s.Create(ctx, plan)

	// Start：parallel 派发全部成员。
	if err := c.Start(ctx, f.ID, "t1"); err != nil {
		t.Fatalf("start: %v", err)
	}
	got, _ := s.Get(ctx, f.ID, "t1")
	for i, m := range got.Members {
		if m.DeployID == 0 {
			t.Fatalf("member %d should be dispatched in parallel mode", i)
		}
	}

	// 模拟全部成员子部署进入 canary（成员内部灰度）。
	for _, m := range got.Members {
		exec.setStatus(m.DeployID, StatusCanary)
	}
	_ = c.Reconcile(ctx, f.ID, "t1")
	got, _ = s.Get(ctx, f.ID, "t1")
	// 全部成员 canary（未 success），未达 gated/success，状态保持进行中。
	if got.Status == FedStatusSuccess || got.Status == FedStatusFailed {
		t.Fatalf("unexpected terminal status %s", got.Status)
	}

	// 模拟全部成员成功 -> fed_success。
	for _, m := range got.Members {
		exec.setStatus(m.DeployID, StatusSuccess)
	}
	_ = c.Reconcile(ctx, f.ID, "t1")
	got, _ = s.Get(ctx, f.ID, "t1")
	if got.Status != FedStatusSuccess {
		t.Fatalf("want fed_success, got %s", got.Status)
	}
}

func TestFederationCoordinator_Parallel_Promote(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryFederationStore()
	exec := newFakeExec()
	c := NewFederationCoordinator(s, exec)

	plan := newFederationPlan("fed", "t1")
	plan.Mode = FedModeParallel
	plan.Template.Strategy = StrategyCanary
	plan.Template.CanaryWeight = 50
	f, _ := s.Create(ctx, plan)
	_ = c.Start(ctx, f.ID, "t1")
	got, _ := s.Get(ctx, f.ID, "t1")

	// 成员子部署进入 gated（成员内部门禁通过）。
	for _, m := range got.Members {
		exec.setStatus(m.DeployID, StatusGated)
	}
	_ = c.Reconcile(ctx, f.ID, "t1")

	// Promote：parallel 全量晋级成员子部署。
	if err := c.Promote(ctx, f.ID, "t1"); err != nil {
		t.Fatalf("promote parallel: %v", err)
	}
	if len(exec.promoted) != 3 {
		t.Fatalf("promote should be called for all 3 members, got %d", len(exec.promoted))
	}
	got, _ = s.Get(ctx, f.ID, "t1")
	if got.Status != FedStatusPromoting {
		t.Fatalf("want fed_promoting, got %s", got.Status)
	}
}

// =============================================================================
// ReconcileAll
// =============================================================================

func TestFederationCoordinator_ReconcileAll(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryFederationStore()
	exec := newFakeExec()
	c := NewFederationCoordinator(s, exec)

	// 创建两个进行中联邦发布。
	f1, _ := s.Create(ctx, newFederationPlan("fed-1", "t1"))
	_ = c.Start(ctx, f1.ID, "t1")
	f2, _ := s.Create(ctx, newFederationPlan("fed-2", "t1"))
	_ = c.Start(ctx, f2.ID, "t1")

	g1, _ := s.Get(ctx, f1.ID, "t1")
	g2, _ := s.Get(ctx, f2.ID, "t1")
	exec.setStatus(g1.Members[0].DeployID, StatusSuccess)
	exec.setStatus(g2.Members[0].DeployID, StatusSuccess)

	n := c.ReconcileAll(ctx, "t1")
	if n != 2 {
		t.Fatalf("want 2 reconciled, got %d", n)
	}
}

// =============================================================================
// HTTP API 集成
// =============================================================================

// newFederationAPIHandler 构造带联邦能力的 Handler + fakeDisp（设备已纳管）。
func newFederationAPIHandler() *Handler {
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	disp.devices["dev-2"] = &proto.DeviceInfo{DeviceID: "dev-2", AgentID: "a2"}
	disp.devices["dev-3"] = &proto.DeviceInfo{DeviceID: "dev-3", AgentID: "a3"}
	return NewHandler(NewMemory(), disp)
}

func doRequest(t *testing.T, h http.Handler, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("X-Tenant-ID", "t1")
	req.Header.Set("X-User-ID", "tester")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestFederationAPI_CreateListGet(t *testing.T) {
	h := newFederationAPIHandler()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// POST 创建。
	plan := newFederationPlan("fed-api", "t1")
	w := doRequest(t, mux, http.MethodPost, "/api/v1/deploys/federation", plan)
	if w.Code != http.StatusCreated {
		t.Fatalf("create want 201, got %d: %s", w.Code, w.Body.String())
	}
	var created FederationDeploy
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.ID == 0 || created.Status != FedStatusPending {
		t.Fatalf("unexpected created: %#v", created)
	}

	// GET 列表。
	w = doRequest(t, mux, http.MethodGet, "/api/v1/deploys/federation", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list want 200, got %d", w.Code)
	}
	var list []FederationDeploy
	json.NewDecoder(w.Body).Decode(&list)
	if len(list) != 1 {
		t.Fatalf("want 1 item, got %d", len(list))
	}

	// GET 详情。
	w = doRequest(t, mux, http.MethodGet, "/api/v1/deploys/federation/"+fmtID(created.ID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get want 200, got %d", w.Code)
	}
}

func TestFederationAPI_ExecuteStatusRollback(t *testing.T) {
	h := newFederationAPIHandler()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	plan := newFederationPlan("fed-api", "t1")
	// 用单成员简化：execute 后首批即全部，成员成功后 reconcile -> fed_success。
	plan.Members = []FederationMember{
		{ClusterID: "cluster-a", Name: "A", TargetIDs: "dev-1", Order: 0, Weight: 100},
	}
	w := doRequest(t, mux, http.MethodPost, "/api/v1/deploys/federation", plan)
	var created FederationDeploy
	json.NewDecoder(w.Body).Decode(&created)

	// execute 启动。
	w = doRequest(t, mux, http.MethodPost, "/api/v1/deploys/federation/"+fmtID(created.ID)+"/execute", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("execute want 200, got %d: %s", w.Code, w.Body.String())
	}
	var started FederationDeploy
	json.NewDecoder(w.Body).Decode(&started)
	if started.Status != FedStatusCanary && started.Status != FedStatusRunning {
		t.Fatalf("want canary/running after execute, got %s", started.Status)
	}
	if started.Members[0].DeployID == 0 {
		t.Fatal("member should be dispatched")
	}

	// status 查询。
	w = doRequest(t, mux, http.MethodGet, "/api/v1/deploys/federation/"+fmtID(created.ID)+"/status", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status want 200, got %d", w.Code)
	}
	var st FederationStatus
	json.NewDecoder(w.Body).Decode(&st)
	if st.TotalMembers != 1 {
		t.Fatalf("want 1 total, got %d", st.TotalMembers)
	}

	// rollback。
	w = doRequest(t, mux, http.MethodPost, "/api/v1/deploys/federation/"+fmtID(created.ID)+"/rollback", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("rollback want 200, got %d: %s", w.Code, w.Body.String())
	}
	var rb FederationDeploy
	json.NewDecoder(w.Body).Decode(&rb)
	if rb.Status != FedStatusRolledBack {
		t.Fatalf("want rolledback, got %s", rb.Status)
	}
}

func TestFederationAPI_NotFound(t *testing.T) {
	h := newFederationAPIHandler()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	w := doRequest(t, mux, http.MethodGet, "/api/v1/deploys/federation/99999", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

func TestFederationAPI_InvalidJSON(t *testing.T) {
	h := newFederationAPIHandler()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/deploys/federation", bytes.NewReader([]byte("{bad json")))
	req.Header.Set("X-Tenant-ID", "t1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

// =============================================================================
// Handler 实现 DeployExecutor（真实派发链路）
// =============================================================================

func TestHandler_DeployExecutor_CreateAndExecute(t *testing.T) {
	ctx := context.Background()
	disp := newFakeDisp()
	disp.devices["dev-1"] = &proto.DeviceInfo{DeviceID: "dev-1", AgentID: "a1"}
	h := NewHandler(NewMemory(), disp)

	tpl := &DeployTask{Name: "svc", Type: TypeScript, TenantID: "t1"}
	id, err := h.CreateAndExecute(ctx, tpl, "dev-1", "t1")
	if err != nil {
		t.Fatalf("createAndExecute: %v", err)
	}
	if id == 0 {
		t.Fatal("deploy id should be non-zero")
	}
	st, err := h.MemberStatus(ctx, id, "t1")
	if err != nil {
		t.Fatalf("memberStatus: %v", err)
	}
	if st != StatusRunning {
		t.Fatalf("want running, got %s", st)
	}
}

// fmtID 将 int64 ID 格式化为字符串（避免引入 strconv 仅此一处）。
func fmtID(id int64) string {
	if id == 0 {
		return "0"
	}
	neg := id < 0
	if neg {
		id = -id
	}
	var buf [20]byte
	pos := len(buf)
	for id > 0 {
		pos--
		buf[pos] = byte('0' + id%10)
		id /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// 编译期断言：测试用例使用 time 包（避免未使用导入）。
var _ = time.Second