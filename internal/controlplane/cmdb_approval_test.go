package controlplane

import (
	"testing"
	"time"

	"opsmesh/internal/cmdb"
	"opsmesh/internal/store"
)

// newTestCMDBApprovalManager 构造测试用 CMDBApprovalManager + MemoryStore + MemoryCiStore。
//
// 返回 manager、底层 store（用于断言审计日志）、ciStore（用于断言 CI）。
func newTestCMDBApprovalManager() (*CMDBApprovalManager, *store.MemoryStore, *cmdb.MemoryCiStore) {
	st := store.NewMemoryStore()
	ci := cmdb.NewMemoryCiStore()
	m := NewCMDBApprovalManager(st, ci)
	return m, st, ci
}

// --- TestSubmitChange ---

// TestSubmitChange 验证提交变更申请：
//   - 返回非空 changeID；
//   - 申请状态为 pending；
//   - ListPending 能查到该申请；
//   - 审计日志已产出（action=cmdb_change_submit）。
func TestSubmitChange(t *testing.T) {
	m, st, _ := newTestCMDBApprovalManager()

	req := &CMDBChangeRequest{
		TenantID:  "tenant-a",
		Action:    "create",
		CIType:    "machine",
		Requester: "alice",
		Reason:    "新增物理机 host-01",
		Changes: map[string]interface{}{
			"name": "host-01",
			"attrs": map[string]string{
				"ip": "10.0.0.1",
			},
		},
	}

	id, err := m.SubmitChange(req)
	if err != nil {
		t.Fatalf("SubmitChange 失败: %v", err)
	}
	if id == "" {
		t.Fatal("返回的 changeID 为空")
	}
	if req.ID != id {
		t.Errorf("申请 ID 不匹配: got %q, want %q", req.ID, id)
	}
	if req.Status != cmdbChangeStatusPending {
		t.Errorf("申请状态 = %q, want %q", req.Status, cmdbChangeStatusPending)
	}
	if req.CreatedAt.IsZero() {
		t.Error("CreatedAt 未设置")
	}

	// ListPending 应包含该申请。
	pending, err := m.ListPending("tenant-a")
	if err != nil {
		t.Fatalf("ListPending 失败: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("ListPending 返回 %d 条, want 1", len(pending))
	}
	if pending[0].ID != id {
		t.Errorf("ListPending 返回的 ID = %q, want %q", pending[0].ID, id)
	}

	// 审计日志应包含提交事件。
	audits := st.Audits()
	found := false
	for _, a := range audits {
		if a.Action == "cmdb_change_submit" && a.Target == id {
			found = true
			break
		}
	}
	if !found {
		t.Error("审计日志中未找到 cmdb_change_submit 事件")
	}
}

// --- TestApproveChange ---

// TestApproveChange 验证审批通过后执行变更：
//   - 状态翻转为 approved；
//   - 审批人和时间已记录；
//   - CI 已实际创建到 ciStore；
//   - ListPending 不再包含该申请；
//   - 审计日志已产出（action=cmdb_change_approve）。
func TestApproveChange(t *testing.T) {
	m, st, ci := newTestCMDBApprovalManager()

	req := &CMDBChangeRequest{
		TenantID:  "tenant-a",
		Action:    "create",
		CIType:    "machine",
		Requester: "alice",
		Reason:    "新增物理机 host-02",
		Changes: map[string]interface{}{
			"name": "host-02",
			"attrs": map[string]string{
				"ip": "10.0.0.2",
			},
		},
	}
	id, err := m.SubmitChange(req)
	if err != nil {
		t.Fatalf("SubmitChange 失败: %v", err)
	}

	// 审批通过。
	if err := m.ApproveChange(id, "bob", "同意新增"); err != nil {
		t.Fatalf("ApproveChange 失败: %v", err)
	}

	// 验证状态翻转。
	got, err := m.GetChange(id)
	if err != nil {
		t.Fatalf("GetChange 失败: %v", err)
	}
	if got.Status != cmdbChangeStatusApproved {
		t.Errorf("状态 = %q, want %q", got.Status, cmdbChangeStatusApproved)
	}
	if got.Approver != "bob" {
		t.Errorf("审批人 = %q, want %q", got.Approver, "bob")
	}
	if got.Comment != "同意新增" {
		t.Errorf("备注 = %q, want %q", got.Comment, "同意新增")
	}
	if got.ReviewedAt == nil || got.ReviewedAt.IsZero() {
		t.Error("ReviewedAt 未设置")
	}

	// 验证 CI 已实际创建。
	cis, err := ci.GetCIs(nil, "machine", "active", "tenant-a")
	if err != nil {
		t.Fatalf("GetCIs 失败: %v", err)
	}
	if len(cis) != 1 {
		t.Fatalf("CI 数量 = %d, want 1", len(cis))
	}
	if cis[0].Name != "host-02" {
		t.Errorf("CI name = %q, want %q", cis[0].Name, "host-02")
	}
	if cis[0].ApprovalStatus != cmdb.ApprovalApproved {
		t.Errorf("CI approvalStatus = %q, want %q", cis[0].ApprovalStatus, cmdb.ApprovalApproved)
	}

	// ListPending 不应再包含该申请。
	pending, _ := m.ListPending("tenant-a")
	if len(pending) != 0 {
		t.Errorf("ListPending 返回 %d 条, want 0", len(pending))
	}

	// 审计日志应包含审批通过事件。
	audits := st.Audits()
	found := false
	for _, a := range audits {
		if a.Action == "cmdb_change_approve" && a.Target == id {
			found = true
			break
		}
	}
	if !found {
		t.Error("审计日志中未找到 cmdb_change_approve 事件")
	}
}

// --- TestRejectChange ---

// TestRejectChange 验证驳回变更：
//   - 状态翻转为 rejected；
//   - 审批人和时间已记录；
//   - CI 未创建到 ciStore；
//   - ListPending 不再包含该申请；
//   - 审计日志已产出（action=cmdb_change_reject）。
func TestRejectChange(t *testing.T) {
	m, st, ci := newTestCMDBApprovalManager()

	req := &CMDBChangeRequest{
		TenantID:  "tenant-a",
		Action:    "create",
		CIType:    "machine",
		Requester: "alice",
		Reason:    "新增物理机 host-03",
		Changes: map[string]interface{}{
			"name": "host-03",
		},
	}
	id, err := m.SubmitChange(req)
	if err != nil {
		t.Fatalf("SubmitChange 失败: %v", err)
	}

	// 驳回。
	if err := m.RejectChange(id, "bob", "不需要此机器"); err != nil {
		t.Fatalf("RejectChange 失败: %v", err)
	}

	// 验证状态翻转。
	got, err := m.GetChange(id)
	if err != nil {
		t.Fatalf("GetChange 失败: %v", err)
	}
	if got.Status != cmdbChangeStatusRejected {
		t.Errorf("状态 = %q, want %q", got.Status, cmdbChangeStatusRejected)
	}
	if got.Approver != "bob" {
		t.Errorf("审批人 = %q, want %q", got.Approver, "bob")
	}
	if got.Comment != "不需要此机器" {
		t.Errorf("备注 = %q, want %q", got.Comment, "不需要此机器")
	}
	if got.ReviewedAt == nil || got.ReviewedAt.IsZero() {
		t.Error("ReviewedAt 未设置")
	}

	// CI 不应被创建。
	cis, _ := ci.GetCIs(nil, "machine", "active", "tenant-a")
	if len(cis) != 0 {
		t.Errorf("CI 数量 = %d, want 0（驳回不应创建 CI）", len(cis))
	}

	// ListPending 不应再包含该申请。
	pending, _ := m.ListPending("tenant-a")
	if len(pending) != 0 {
		t.Errorf("ListPending 返回 %d 条, want 0", len(pending))
	}

	// 审计日志应包含驳回事件。
	audits := st.Audits()
	found := false
	for _, a := range audits {
		if a.Action == "cmdb_change_reject" && a.Target == id {
			found = true
			break
		}
	}
	if !found {
		t.Error("审计日志中未找到 cmdb_change_reject 事件")
	}
}

// --- TestListPending ---

// TestListPending 验证列出待审批变更：
//   - 多租户隔离（只返回指定租户的 pending）；
//   - 已审批的不在 pending 列表中。
func TestListPending(t *testing.T) {
	m, _, _ := newTestCMDBApprovalManager()

	// 提交两个租户的变更。
	id1, _ := m.SubmitChange(&CMDBChangeRequest{
		TenantID: "tenant-a", Action: "create", CIType: "machine",
		Requester: "alice", Changes: map[string]interface{}{"name": "h1"},
	})
	id2, _ := m.SubmitChange(&CMDBChangeRequest{
		TenantID: "tenant-b", Action: "create", CIType: "machine",
		Requester: "bob", Changes: map[string]interface{}{"name": "h2"},
	})
	// 审批通过第一个（tenant-a）。
	if err := m.ApproveChange(id1, "reviewer", "ok"); err != nil {
		t.Fatalf("ApproveChange 失败: %v", err)
	}

	// tenant-a 的 pending 应为空（id1 已审批）。
	pendingA, _ := m.ListPending("tenant-a")
	if len(pendingA) != 0 {
		t.Errorf("tenant-a pending = %d, want 0", len(pendingA))
	}

	// tenant-b 的 pending 应只有 id2。
	pendingB, _ := m.ListPending("tenant-b")
	if len(pendingB) != 1 {
		t.Fatalf("tenant-b pending = %d, want 1", len(pendingB))
	}
	if pendingB[0].ID != id2 {
		t.Errorf("tenant-b pending ID = %q, want %q", pendingB[0].ID, id2)
	}

	// 空租户串返回全部 pending。
	pendingAll, _ := m.ListPending("")
	if len(pendingAll) != 1 {
		t.Errorf("全租户 pending = %d, want 1", len(pendingAll))
	}
}

// --- TestApproveChange_AlreadyApproved ---

// TestApproveChange_AlreadyApproved 验证重复审批报错：
//   - 已 approved 的变更再次 ApproveChange 应返回错误；
//   - 已 rejected 的变更再次 ApproveChange 应返回错误。
func TestApproveChange_AlreadyApproved(t *testing.T) {
	m, _, _ := newTestCMDBApprovalManager()

	id, _ := m.SubmitChange(&CMDBChangeRequest{
		TenantID: "tenant-a", Action: "create", CIType: "machine",
		Requester: "alice", Changes: map[string]interface{}{"name": "h1"},
	})
	// 首次审批通过。
	if err := m.ApproveChange(id, "bob", "ok"); err != nil {
		t.Fatalf("首次 ApproveChange 失败: %v", err)
	}
	// 重复审批应报错。
	if err := m.ApproveChange(id, "bob", "ok again"); err == nil {
		t.Error("重复审批应返回错误，实际返回 nil")
	}

	// 测试已 rejected 的变更再次审批。
	id2, _ := m.SubmitChange(&CMDBChangeRequest{
		TenantID: "tenant-a", Action: "create", CIType: "machine",
		Requester: "alice", Changes: map[string]interface{}{"name": "h2"},
	})
	if err := m.RejectChange(id2, "bob", "no"); err != nil {
		t.Fatalf("RejectChange 失败: %v", err)
	}
	if err := m.ApproveChange(id2, "bob", "try approve after reject"); err == nil {
		t.Error("已驳回的变更再次审批应返回错误，实际返回 nil")
	}
}

// --- TestApproveChange_NotFound ---

// TestApproveChange_NotFound 验证不存在的变更 ID 审批报错。
func TestApproveChange_NotFound(t *testing.T) {
	m, _, _ := newTestCMDBApprovalManager()

	if err := m.ApproveChange("chg-nonexistent", "bob", "ok"); err == nil {
		t.Error("不存在的变更 ID 审批应返回错误，实际返回 nil")
	}
	if err := m.RejectChange("chg-nonexistent", "bob", "no"); err == nil {
		t.Error("不存在的变更 ID 驳回应返回错误，实际返回 nil")
	}
	if _, err := m.GetChange("chg-nonexistent"); err == nil {
		t.Error("不存在的变更 ID 查询应返回错误，实际返回 nil")
	}
}

// --- TestSubmitChange_InvalidAction ---

// TestSubmitChange_InvalidAction 验证非法 action 报错。
func TestSubmitChange_InvalidAction(t *testing.T) {
	m, _, _ := newTestCMDBApprovalManager()

	_, err := m.SubmitChange(&CMDBChangeRequest{
		TenantID: "tenant-a", Action: "invalid", CIType: "machine",
	})
	if err == nil {
		t.Error("非法 action 应返回错误，实际返回 nil")
	}

	// update 缺少 ciID 应报错。
	_, err = m.SubmitChange(&CMDBChangeRequest{
		TenantID: "tenant-a", Action: "update", CIType: "machine",
	})
	if err == nil {
		t.Error("update 缺少 ciID 应返回错误，实际返回 nil")
	}

	// create 缺少 ciType 应报错。
	_, err = m.SubmitChange(&CMDBChangeRequest{
		TenantID: "tenant-a", Action: "create",
	})
	if err == nil {
		t.Error("create 缺少 ciType 应返回错误，实际返回 nil")
	}
}

// --- TestApproveChange_UpdateAndDelete ---

// TestApproveChange_UpdateAndDelete 验证审批通过 update/delete 变更后实际执行。
func TestApproveChange_UpdateAndDelete(t *testing.T) {
	m, _, ci := newTestCMDBApprovalManager()

	// 先创建一个 CI（通过审批 create）。
	createID, _ := m.SubmitChange(&CMDBChangeRequest{
		TenantID: "tenant-a", Action: "create", CIType: "machine",
		Requester: "alice",
		Changes: map[string]interface{}{
			"name":  "host-up",
			"attrs": map[string]string{"ip": "10.0.0.10"},
		},
	})
	if err := m.ApproveChange(createID, "bob", "ok"); err != nil {
		t.Fatalf("创建审批失败: %v", err)
	}
	// 取出创建的 CI ID。
	cis, _ := ci.GetCIs(nil, "machine", "active", "tenant-a")
	if len(cis) != 1 {
		t.Fatalf("创建后 CI 数量 = %d, want 1", len(cis))
	}
	ciID := cis[0].ID

	// 提交 update 变更并审批。
	updateID, _ := m.SubmitChange(&CMDBChangeRequest{
		TenantID: "tenant-a", Action: "update", CIType: "machine", CIID: ciID,
		Requester: "alice",
		Changes: map[string]interface{}{
			"name":  "host-up-renamed",
			"attrs": map[string]string{"ip": "10.0.0.20"},
		},
	})
	if err := m.ApproveChange(updateID, "bob", "ok update"); err != nil {
		t.Fatalf("更新审批失败: %v", err)
	}
	// 验证 CI 已更新。
	updated, err := ci.GetCI(nil, ciID, "tenant-a")
	if err != nil {
		t.Fatalf("GetCI 失败: %v", err)
	}
	if updated.Name != "host-up-renamed" {
		t.Errorf("更新后 name = %q, want %q", updated.Name, "host-up-renamed")
	}
	if updated.Version != 2 {
		t.Errorf("更新后 version = %d, want 2", updated.Version)
	}

	// 提交 delete 变更并审批。
	deleteID, _ := m.SubmitChange(&CMDBChangeRequest{
		TenantID: "tenant-a", Action: "delete", CIType: "machine", CIID: ciID,
		Requester: "alice",
	})
	if err := m.ApproveChange(deleteID, "bob", "ok delete"); err != nil {
		t.Fatalf("删除审批失败: %v", err)
	}
	// 验证 CI 已软删除（status=deleted）。
	deleted, err := ci.GetCI(nil, ciID, "tenant-a")
	if err != nil {
		t.Fatalf("GetCI 失败: %v", err)
	}
	if deleted.Status != "deleted" {
		t.Errorf("删除后 status = %q, want %q", deleted.Status, "deleted")
	}
}

// --- TestListAll ---

// TestListAll 验证列出全部变更（按状态过滤）。
func TestListAll(t *testing.T) {
	m, _, _ := newTestCMDBApprovalManager()

	id1, _ := m.SubmitChange(&CMDBChangeRequest{
		TenantID: "tenant-a", Action: "create", CIType: "machine",
		Changes: map[string]interface{}{"name": "h1"},
	})
	id2, _ := m.SubmitChange(&CMDBChangeRequest{
		TenantID: "tenant-a", Action: "create", CIType: "machine",
		Changes: map[string]interface{}{"name": "h2"},
	})
	_ = m.ApproveChange(id1, "bob", "ok")
	_ = m.RejectChange(id2, "bob", "no")

	// 全部变更。
	all, _ := m.ListAll("tenant-a", "")
	if len(all) != 2 {
		t.Errorf("ListAll 返回 %d 条, want 2", len(all))
	}

	// 仅 approved。
	approved, _ := m.ListAll("tenant-a", cmdbChangeStatusApproved)
	if len(approved) != 1 || approved[0].ID != id1 {
		t.Errorf("ListAll(approved) 返回 %v, want id=%s", approved, id1)
	}

	// 仅 rejected。
	rejected, _ := m.ListAll("tenant-a", cmdbChangeStatusRejected)
	if len(rejected) != 1 || rejected[0].ID != id2 {
		t.Errorf("ListAll(rejected) 返回 %v, want id=%s", rejected, id2)
	}

	// 仅 pending。
	pending, _ := m.ListAll("tenant-a", cmdbChangeStatusPending)
	if len(pending) != 0 {
		t.Errorf("ListAll(pending) 返回 %d 条, want 0", len(pending))
	}
}

// 确保 time 包被使用（避免未使用导入）。
var _ = time.Now
