package store

import (
	"testing"

	"github.com/Levango7/OpsMesh/services/device-svc/internal/models"
)

// UpdateDevice 的入参来自 proto/JSON，两者都不携带 retired 字段，
// 因此整对象覆盖会把已软删设备的 retired 写回 false（复活）。
// 这里钉住中止行为：退役后一律拒绝更新，且原记录内容不被覆盖。
func TestMemoryStoreUpdateDeviceRefusesRetiredDevice(t *testing.T) {
	m := NewMemoryStore()
	if got := m.RegisterDevice(&models.Device{ID: "dev-1", TenantID: "t1", Name: "web-1", Status: "online"}); got == nil {
		t.Fatal("RegisterDevice 返回 nil")
	}
	if !m.DeleteDevice("dev-1") {
		t.Fatal("DeleteDevice 返回 false")
	}

	updated, ok := m.UpdateDevice(&models.Device{ID: "dev-1", TenantID: "t1", Name: "revived", Status: "online"})
	if ok || updated != nil {
		t.Fatalf("UpdateDevice(已退役) = (%+v, %v), want (nil, false)", updated, ok)
	}
	if got := m.Device("dev-1"); got == nil || !got.Retired || got.Name != "web-1" {
		t.Fatalf("退役记录被更新覆盖: %+v", got)
	}
	if listed := m.ListDevices("t1", "", "", 0); len(listed) != 0 {
		t.Fatalf("已退役设备出现在活跃清单: %+v", listed)
	}
}

// 守卫不能误伤正常更新：活跃设备的更新仍然生效。
func TestMemoryStoreUpdateDeviceLiveDevice(t *testing.T) {
	m := NewMemoryStore()
	m.RegisterDevice(&models.Device{ID: "dev-2", TenantID: "t1", Name: "web-2", Status: "online"})

	updated, ok := m.UpdateDevice(&models.Device{ID: "dev-2", TenantID: "t1", Name: "web-2-renamed", IP: "10.0.0.9", Status: "offline"})
	if !ok || updated == nil {
		t.Fatalf("UpdateDevice(活跃) = (%+v, %v), want 成功", updated, ok)
	}
	got := m.Device("dev-2")
	if got == nil || got.Name != "web-2-renamed" || got.IP != "10.0.0.9" || got.Status != "offline" || got.Retired {
		t.Fatalf("更新未生效: %+v", got)
	}
}
