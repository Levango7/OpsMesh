// memory_abac_policy_test.go 内存后端 ABACPolicyStore 的持久化语义守卫（批次47）。
package memory

import "testing"

const abacTestPolicyID = "t1|read|alice|vm:42"

func abacTestRecord(tenantID string) ABACPolicyRecord {
	return ABACPolicyRecord{
		TenantID: tenantID,
		ID:       abacTestPolicyID,
		Subject:  "alice",
		Resource: "vm:42",
		Action:   "read",
		Effect:   "allow",
	}
}

// 写入即可读回；重复 upsert 版本自增且创建时间保持不变。
func TestABACPolicyRecords_RoundTripAndVersionBump(t *testing.T) {
	m := NewMemoryStore()
	stored, ok := m.UpsertABACPolicyRecord(abacTestRecord("t1"))
	if !ok {
		t.Fatal("首次 upsert 应成功")
	}
	if stored.Version != 1 {
		t.Fatalf("首次 upsert version=%d, want 1", stored.Version)
	}
	if stored.CreatedAt.IsZero() || stored.UpdatedAt.IsZero() {
		t.Fatalf("落库记录必须带时间戳: %+v", stored)
	}
	created := stored.CreatedAt

	updated := abacTestRecord("t1")
	updated.Subject = "bob"
	stored, ok = m.UpsertABACPolicyRecord(updated)
	if !ok {
		t.Fatal("重复 upsert 应成功")
	}
	if stored.Version != 2 {
		t.Fatalf("重复 upsert version=%d, want 2", stored.Version)
	}
	if !stored.CreatedAt.Equal(created) {
		t.Fatalf("更新不得改变 created_at: got %v want %v", stored.CreatedAt, created)
	}
	if stored.Subject != "bob" {
		t.Fatalf("更新后 subject=%q, want bob", stored.Subject)
	}

	list := m.ListABACPolicyRecords("t1")
	if len(list) != 1 {
		t.Fatalf("List 条数=%d, want 1", len(list))
	}
	if list[0].Version != 2 || list[0].Subject != "bob" {
		t.Fatalf("List 返回的不是最新版本: %+v", list[0])
	}
}

// 租户隔离：同 ID 在不同租户各自独立；跨租户视图按 (tenant, id) 确定性排序。
func TestABACPolicyRecords_TenantIsolation(t *testing.T) {
	m := NewMemoryStore()
	if _, ok := m.UpsertABACPolicyRecord(abacTestRecord("t1")); !ok {
		t.Fatal("t1 upsert 应成功")
	}
	if _, ok := m.UpsertABACPolicyRecord(abacTestRecord("t2")); !ok {
		t.Fatal("t2 upsert 应成功")
	}

	t1 := m.ListABACPolicyRecords("t1")
	if len(t1) != 1 || t1[0].TenantID != "t1" {
		t.Fatalf("t1 视图=%+v, want 仅 t1 一条", t1)
	}
	all := m.ListABACPolicyRecords("")
	if len(all) != 2 || all[0].TenantID != "t1" || all[1].TenantID != "t2" {
		t.Fatalf("跨租户视图排序/内容不对: %+v", all)
	}

	if !m.DeleteABACPolicyRecord("t1", abacTestPolicyID) {
		t.Fatal("删除 t1 策略应成功")
	}
	if len(m.ListABACPolicyRecords("t1")) != 0 {
		t.Fatal("t1 策略应已删除")
	}
	if got := m.ListABACPolicyRecords("t2"); len(got) != 1 {
		t.Fatalf("删除 t1 不得影响 t2，得到 %+v", got)
	}
}

// 删除不存在的记录返回 false（未知租户/未知 ID/重复删除）。
func TestABACPolicyRecords_DeleteUnknownFalse(t *testing.T) {
	m := NewMemoryStore()
	if m.DeleteABACPolicyRecord("t1", "no-such-id") {
		t.Fatal("未知租户删除应返回 false")
	}
	if _, ok := m.UpsertABACPolicyRecord(abacTestRecord("t1")); !ok {
		t.Fatal("upsert 应成功")
	}
	if m.DeleteABACPolicyRecord("t1", "no-such-id") {
		t.Fatal("未知 ID 删除应返回 false")
	}
	if m.DeleteABACPolicyRecord("t2", abacTestPolicyID) {
		t.Fatal("跨租户删除应返回 false（不存在）")
	}
	if !m.DeleteABACPolicyRecord("t1", abacTestPolicyID) {
		t.Fatal("删除已存在记录应返回 true")
	}
	if m.DeleteABACPolicyRecord("t1", abacTestPolicyID) {
		t.Fatal("重复删除应返回 false")
	}
}
