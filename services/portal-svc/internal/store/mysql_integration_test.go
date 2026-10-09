package store

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/services/portal-svc/internal/models"
)

// TestPortalStoreMySQLRoundTrip 是 portal-svc MySQL 后端的真库往返校验（TD-65「集成测试补齐」）。
//
// 写入走一个 store 实例、读回走**另一个全新实例**（新连接、无实例内状态），
// 以此证明数据真的落库。覆盖三条独立落库路径：资源申请（含数值列）、配额 upsert、活动流水。
func TestPortalStoreMySQLRoundTrip(t *testing.T) {
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("skipping MySQL integration smoke: missing OPSMESH_TEST_MYSQL_DSN env; set it to a real MySQL DSN to run")
	}

	w, err := NewMySQLStore(dsn)
	if err != nil {
		t.Fatalf("NewMySQLStore(writer): %v", err)
	}
	r, err := NewMySQLStore(dsn)
	if err != nil {
		t.Fatalf("NewMySQLStore(reader): %v", err)
	}

	suffix := time.Now().UnixNano()
	id := fmt.Sprintf("itest-req-%d", suffix)
	tenant := fmt.Sprintf("itest-portal-%d", suffix)
	defer func() {
		if w.DeleteRequest(id) {
			return
		}
		t.Logf("cleanup: DeleteRequest(%s) 返回 false（可能本就没写成功）", id)
	}()

	// 1) 资源申请：数值列必须原值往返（int 与 float 各有精度坑，分开断言）。
	created := w.CreateRequest(&models.ResourceRequest{
		ID:           id,
		TenantID:     tenant,
		Requester:    "itest",
		Title:        "8 卡 GPU 申请",
		Description:  "真库往返校验",
		ResourceType: "gpu",
		CPU:          8,
		MemoryGB:     32,
		StorageGB:    100,
		CostEstimate: 12.5,
		Status:       models.StatusDraft,
	})
	if created == nil {
		t.Fatal("CreateRequest 返回 nil")
	}

	got := r.GetRequest(id)
	if got == nil {
		t.Fatal("新实例 GetRequest 未命中——申请没有真的落库")
	}
	if got.CPU != 8 || got.MemoryGB != 32 || got.StorageGB != 100 {
		t.Fatalf("数值列读回 = {cpu:%d mem:%d storage:%d}, want {8 32 100}", got.CPU, got.MemoryGB, got.StorageGB)
	}
	if got.CostEstimate != 12.5 {
		t.Fatalf("CostEstimate 读回 = %v, want 12.5", got.CostEstimate)
	}
	if got.Status != models.StatusDraft || got.ResourceType != "gpu" {
		t.Fatalf("读回 = {status:%q type:%q}, want {draft gpu}", got.Status, got.ResourceType)
	}

	// 2) 列表按租户 + 状态过滤，必须能在新实例里看到刚写入的这条。
	listed := r.ListRequests(tenant, "draft")
	found := false
	for _, req := range listed {
		if req != nil && req.ID == id {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ListRequests(%s, draft) 未包含刚写入的 %s", tenant, id)
	}

	// 3) 状态流转回写：审批后新实例必须读到新状态。
	got.Status = models.StatusApproved
	got.Approver = "itest-approver"
	if !w.UpdateRequest(got) {
		t.Fatal("UpdateRequest 返回 false")
	}
	again := r.GetRequest(id)
	if again == nil {
		t.Fatal("更新后新实例 GetRequest 未命中")
	}
	if again.Status != models.StatusApproved || again.Approver != "itest-approver" {
		t.Fatalf("读回 = {status:%q approver:%q}, want {approved itest-approver}（UPDATE 未落库）", again.Status, again.Approver)
	}

	// 4) 配额 upsert：先写后改，两次都必须能在新实例读到（覆盖 INSERT 与 UPDATE 两条分支）。
	if err := w.SetQuota(tenant, &models.Quota{TenantID: tenant, MaxCPU: 64, MaxMemoryGB: 256, MaxStorageGB: 512, MaxRequests: 20}); err != nil {
		t.Fatalf("SetQuota 首次写入: %v", err)
	}
	q := r.GetQuota(tenant)
	if q == nil {
		t.Fatal("新实例 GetQuota 未命中——配额没有真的落库")
	}
	if q.MaxCPU != 64 || q.MaxMemoryGB != 256 || q.MaxStorageGB != 512 || q.MaxRequests != 20 {
		t.Fatalf("配额读回 = %+v, want {64 256 512 20}", q)
	}
	if err := w.SetQuota(tenant, &models.Quota{TenantID: tenant, MaxCPU: 128, MaxMemoryGB: 256, MaxStorageGB: 512, MaxRequests: 40}); err != nil {
		t.Fatalf("SetQuota 覆写: %v", err)
	}
	if q2 := r.GetQuota(tenant); q2 == nil || q2.MaxCPU != 128 || q2.MaxRequests != 40 {
		t.Fatalf("配额覆写读回 = %+v, want max_cpu=128 max_requests=40（upsert 的 UPDATE 分支未落库）", q2)
	}

	// 5) 活动流水：写入后新实例按租户可读回。
	w.AddActivity(&models.ActivityEvent{
		ID:       fmt.Sprintf("itest-act-%d", suffix),
		TenantID: tenant,
		UserID:   "itest",
		Action:   "itest.activity",
		Target:   "roundtrip",
		Detail:   "activity roundtrip",
	})
	acts := r.ListActivity(tenant, 10)
	if len(acts) == 0 {
		t.Fatal("新实例 ListActivity 为空——活动流水没有落库")
	}
	if acts[0].Detail != "activity roundtrip" || acts[0].Action != "itest.activity" {
		t.Fatalf("活动首条 = {action:%q detail:%q}, want {itest.activity activity roundtrip}", acts[0].Action, acts[0].Detail)
	}

	// 6) 删除语义：删掉后新实例必须查不到。
	if !w.DeleteRequest(id) {
		t.Fatal("DeleteRequest 返回 false")
	}
	if r.GetRequest(id) != nil {
		t.Fatal("删除后仍能查到申请——DELETE 没有落到库")
	}

	t.Logf("ROUNDTRIP_OK: resource_requests（含数值列）+ quotas（upsert 两分支）+ activities 均验证『写入→新实例读回』")
}
