// tenant_test.go 测试租户管理引擎（platform.TenantManager）。
package platform

import (
	"testing"

	"opsmesh/internal/store"
)

// newTestTenantManager 构造测试用 TenantManager。
func newTestTenantManager() *TenantManager {
	return NewTenantManager(store.NewMemoryStore())
}

// TestValidateTenant_Valid 验证合法租户校验通过。
func TestValidateTenant_Valid(t *testing.T) {
	m := newTestTenantManager()
	tenant := &store.Tenant{
		ID:     "t1",
		Name:   "acme",
		Status: store.TenantStatusActive,
	}
	if err := m.ValidateTenant(tenant); err != nil {
		t.Fatalf("ValidateTenant failed: %v", err)
	}
	if tenant.DisplayName != "acme" {
		t.Fatalf("DisplayName=%q, want acme (default to Name)", tenant.DisplayName)
	}
}

// TestValidateTenant_Nil 验证 nil 租户返回错误。
func TestValidateTenant_Nil(t *testing.T) {
	m := newTestTenantManager()
	if err := m.ValidateTenant(nil); err == nil {
		t.Fatal("ValidateTenant(nil) should return error")
	}
}

// TestValidateTenant_EmptyID 验证空 ID 返回错误。
func TestValidateTenant_EmptyID(t *testing.T) {
	m := newTestTenantManager()
	tenant := &store.Tenant{Name: "acme", Status: store.TenantStatusActive}
	if err := m.ValidateTenant(tenant); err == nil {
		t.Fatal("ValidateTenant with empty ID should return error")
	}
}

// TestValidateTenant_EmptyName 验证空 Name 返回错误。
func TestValidateTenant_EmptyName(t *testing.T) {
	m := newTestTenantManager()
	tenant := &store.Tenant{ID: "t1", Status: store.TenantStatusActive}
	if err := m.ValidateTenant(tenant); err == nil {
		t.Fatal("ValidateTenant with empty Name should return error")
	}
}

// TestValidateTenant_InvalidStatus 验证非法状态返回错误。
func TestValidateTenant_InvalidStatus(t *testing.T) {
	m := newTestTenantManager()
	tenant := &store.Tenant{ID: "t1", Name: "acme", Status: "unknown"}
	if err := m.ValidateTenant(tenant); err == nil {
		t.Fatal("ValidateTenant with invalid status should return error")
	}
}

// TestCheckQuota_NoLimit 验证配额为 0（不限）时通过。
func TestCheckQuota_NoLimit(t *testing.T) {
	m := newTestTenantManager()
	tenant := &store.Tenant{
		ID:     "t1",
		Name:   "acme",
		Status: store.TenantStatusActive,
		Quota:  store.TenantQuota{MaxDevices: 0}, // 0=不限
	}
	m.store.CreateTenant(tenant)
	if err := m.CheckQuota("t1", "devices", 999); err != nil {
		t.Fatalf("CheckQuota with no limit should pass: %v", err)
	}
}

// TestCheckQuota_WithinLimit 验证未超额时通过。
func TestCheckQuota_WithinLimit(t *testing.T) {
	m := newTestTenantManager()
	tenant := &store.Tenant{
		ID:     "t1",
		Name:   "acme",
		Status: store.TenantStatusActive,
		Quota:  store.TenantQuota{MaxDevices: 10},
	}
	m.store.CreateTenant(tenant)
	if err := m.CheckQuota("t1", "devices", 5); err != nil {
		t.Fatalf("CheckQuota within limit should pass: %v", err)
	}
}

// TestCheckQuota_Exceeded 验证超额时返回错误。
func TestCheckQuota_Exceeded(t *testing.T) {
	m := newTestTenantManager()
	tenant := &store.Tenant{
		ID:     "t1",
		Name:   "acme",
		Status: store.TenantStatusActive,
		Quota:  store.TenantQuota{MaxDevices: 5},
	}
	m.store.CreateTenant(tenant)
	if err := m.CheckQuota("t1", "devices", 5); err == nil {
		t.Fatal("CheckQuota exceeded should return error")
	}
}

// TestCheckQuota_SuspendedTenant 验证暂停租户拒绝新增资源。
func TestCheckQuota_SuspendedTenant(t *testing.T) {
	m := newTestTenantManager()
	tenant := &store.Tenant{
		ID:     "t1",
		Name:   "acme",
		Status: store.TenantStatusSuspended,
		Quota:  store.TenantQuota{MaxDevices: 10},
	}
	m.store.CreateTenant(tenant)
	if err := m.CheckQuota("t1", "devices", 1); err == nil {
		t.Fatal("CheckQuota for suspended tenant should return error")
	}
}

// TestCheckQuota_TenantNotFound 验证租户不存在返回错误。
func TestCheckQuota_TenantNotFound(t *testing.T) {
	m := newTestTenantManager()
	if err := m.CheckQuota("nonexistent", "devices", 0); err == nil {
		t.Fatal("CheckQuota for nonexistent tenant should return error")
	}
}

// TestCheckQuota_UnknownResourceType 验证未知资源类型返回错误。
func TestCheckQuota_UnknownResourceType(t *testing.T) {
	m := newTestTenantManager()
	tenant := &store.Tenant{
		ID:     "t1",
		Name:   "acme",
		Status: store.TenantStatusActive,
	}
	m.store.CreateTenant(tenant)
	if err := m.CheckQuota("t1", "unknown", 0); err == nil {
		t.Fatal("CheckQuota with unknown resource type should return error")
	}
}