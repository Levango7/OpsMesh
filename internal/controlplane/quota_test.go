// quota_test.go — 多租户资源配额与计费测试。
//
// 覆盖 QuotaManager 的核心行为：
//   - SetQuota/GetQuota 配额设置/获取
//   - CheckDevice/CheckTask/CheckAlert 配额检查（未超/超限/0=不限）
//   - Usage 用量统计
//   - 未设置配额时不限制（回退默认配额）
//
// 用 MemoryStore + 构造测试数据（设备/任务/告警）验证配额检查逻辑。
package controlplane

import (
	"errors"
	"testing"

	"opsmesh/internal/proto"
	"opsmesh/internal/store"
)

// newTestQuotaManager 构造测试用 QuotaManager（启用配额检查 + 指定默认配额）。
func newTestQuotaManager(t *testing.T, defaultQuota *store.QuotaConfig) (*QuotaManager, *store.MemoryStore) {
	t.Helper()
	st := store.NewMemoryStore()
	qm := NewQuotaManager(st, true, defaultQuota)
	return qm, st
}

// quotaDeviceSeq 用于 seedDevicesForQuota 生成全局唯一 DeviceID 的递增计数器。
// 避免多次调用 seedDevicesForQuota 时 DeviceID 重复导致 UpsertDevice 覆盖而非新增。
var quotaDeviceSeq int

// seedDevicesForQuota 向 store 注入 n 台指定租户的设备（用于配额检查测试）。
// 使用全局递增计数器保证 DeviceID 唯一，多次调用累加而非覆盖。
func seedDevicesForQuota(st *store.MemoryStore, tenantID string, n int) {
	for i := 0; i < n; i++ {
		quotaDeviceSeq++
		st.UpsertDevice(&proto.DeviceInfo{
			DeviceID: "dev-" + tenantID + "-" + itoa(quotaDeviceSeq),
			Segment:  "seg-test",
			TenantID: tenantID,
			IP:       "10.0.0.1",
			AgentID:  "agent-test",
			State:    "online",
			Managed:  true,
			Hostname: "host-" + itoa(quotaDeviceSeq),
		})
	}
}

// seedTasks 向 store 注入 n 个指定租户的任务（用于配额检查测试）。
func seedTasks(st *store.MemoryStore, tenantID string, n int) {
	agent := st.Register(&proto.AgentInfo{Segment: "seg-test", TenantID: tenantID})
	for i := 0; i < n; i++ {
		st.CreateTask(&proto.Task{
			AgentID:  agent.AgentID,
			TenantID: tenantID,
			Type:     "shell",
			Command:  "echo " + itoa(i),
		})
	}
}

// seedAlerts 向 store 注入 n 条指定租户的告警（用于配额检查测试）。
func seedAlerts(st *store.MemoryStore, tenantID string, n int) {
	for i := 0; i < n; i++ {
		st.AddAlert(&proto.Alert{
			AlertID:  "alert-" + tenantID + "-" + itoa(i),
			TenantID: tenantID,
			Severity: "warning",
			Message:  "test alert " + itoa(i),
		})
	}
}

// itoa 简单的 int → string 转换（避免引入 strconv 增加依赖）。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := make([]byte, 0, 11)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}

// TestQuotaManager_SetGet 设置/获取配额。
func TestQuotaManager_SetGet(t *testing.T) {
	qm, _ := newTestQuotaManager(t, &store.QuotaConfig{MaxDevices: 10, MaxTasks: 20, MaxAlerts: 30})

	// 设置配额。
	cfg := &store.QuotaConfig{MaxDevices: 5, MaxTasks: 15, MaxAlerts: 25}
	if err := qm.SetQuota("t1", cfg); err != nil {
		t.Fatalf("SetQuota failed: %v", err)
	}

	// 获取配额（应返回设置的值）。
	got := qm.GetQuota("t1")
	if got.MaxDevices != 5 || got.MaxTasks != 15 || got.MaxAlerts != 25 {
		t.Fatalf("GetQuota = %+v, want {5,15,25}", got)
	}

	// 未设置配额的租户应回退到默认配额。
	gotDefault := qm.GetQuota("t2")
	if gotDefault.MaxDevices != 10 || gotDefault.MaxTasks != 20 || gotDefault.MaxAlerts != 30 {
		t.Fatalf("GetQuota (default) = %+v, want {10,20,30}", gotDefault)
	}

	// 清除配额（nil）后应回退到默认配额。
	if err := qm.SetQuota("t1", nil); err != nil {
		t.Fatalf("SetQuota(nil) failed: %v", err)
	}
	gotAfterClear := qm.GetQuota("t1")
	if gotAfterClear.MaxDevices != 10 || gotAfterClear.MaxTasks != 20 || gotAfterClear.MaxAlerts != 30 {
		t.Fatalf("GetQuota after clear = %+v, want default {10,20,30}", gotAfterClear)
	}
}

// TestQuotaManager_CheckDevice 设备配额检查：未超/超限/0=不限。
func TestQuotaManager_CheckDevice(t *testing.T) {
	// 场景 1：未超限（当前 5 台，配额 10）。
	qm, st := newTestQuotaManager(t, &store.QuotaConfig{MaxDevices: 10})
	seedDevicesForQuota(st, "t1", 5)
	if err := qm.CheckDevice("t1"); err != nil {
		t.Fatalf("CheckDevice (5/10) should pass, got: %v", err)
	}

	// 场景 2：超限（当前 10 台，配额 10，再创建 1 台超额）。
	seedDevicesForQuota(st, "t1", 5) // 总共 10 台
	if err := qm.CheckDevice("t1"); err == nil {
		t.Fatal("CheckDevice (10/10) should fail with ErrQuotaExceeded")
	} else if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("CheckDevice should return ErrQuotaExceeded, got: %v", err)
	}

	// 场景 3：0=不限（配额 0 表示不限制）。
	qm2, st2 := newTestQuotaManager(t, &store.QuotaConfig{MaxDevices: 0})
	seedDevicesForQuota(st2, "t1", 100)
	if err := qm2.CheckDevice("t1"); err != nil {
		t.Fatalf("CheckDevice (100/0=unlimited) should pass, got: %v", err)
	}

	// 场景 4：显式设置配额覆盖默认配额。
	qm3, st3 := newTestQuotaManager(t, &store.QuotaConfig{MaxDevices: 100})
	if err := qm3.SetQuota("t1", &store.QuotaConfig{MaxDevices: 3}); err != nil {
		t.Fatalf("SetQuota failed: %v", err)
	}
	seedDevicesForQuota(st3, "t1", 3)
	if err := qm3.CheckDevice("t1"); err == nil {
		t.Fatal("CheckDevice (3/3) should fail with ErrQuotaExceeded")
	} else if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("CheckDevice should return ErrQuotaExceeded, got: %v", err)
	}
}

// TestQuotaManager_CheckTask 任务配额检查：未超/超限/0=不限。
func TestQuotaManager_CheckTask(t *testing.T) {
	// 场景 1：未超限（当前 3 个任务，配额 10）。
	qm, st := newTestQuotaManager(t, &store.QuotaConfig{MaxTasks: 10})
	seedTasks(st, "t1", 3)
	if err := qm.CheckTask("t1"); err != nil {
		t.Fatalf("CheckTask (3/10) should pass, got: %v", err)
	}

	// 场景 2：超限（当前 10 个任务，配额 10，再创建 1 个超额）。
	seedTasks(st, "t1", 7) // 总共 10 个任务
	if err := qm.CheckTask("t1"); err == nil {
		t.Fatal("CheckTask (10/10) should fail with ErrQuotaExceeded")
	} else if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("CheckTask should return ErrQuotaExceeded, got: %v", err)
	}

	// 场景 3：0=不限。
	qm2, st2 := newTestQuotaManager(t, &store.QuotaConfig{MaxTasks: 0})
	seedTasks(st2, "t1", 100)
	if err := qm2.CheckTask("t1"); err != nil {
		t.Fatalf("CheckTask (100/0=unlimited) should pass, got: %v", err)
	}
}

// TestQuotaManager_CheckAlert 告警配额检查：未超/超限/0=不限。
func TestQuotaManager_CheckAlert(t *testing.T) {
	// 场景 1：未超限（当前 4 条告警，配额 10）。
	qm, st := newTestQuotaManager(t, &store.QuotaConfig{MaxAlerts: 10})
	seedAlerts(st, "t1", 4)
	if err := qm.CheckAlert("t1"); err != nil {
		t.Fatalf("CheckAlert (4/10) should pass, got: %v", err)
	}

	// 场景 2：超限（当前 10 条告警，配额 10，再创建 1 条超额）。
	seedAlerts(st, "t1", 6) // 总共 10 条告警
	if err := qm.CheckAlert("t1"); err == nil {
		t.Fatal("CheckAlert (10/10) should fail with ErrQuotaExceeded")
	} else if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("CheckAlert should return ErrQuotaExceeded, got: %v", err)
	}

	// 场景 3：0=不限。
	qm2, st2 := newTestQuotaManager(t, &store.QuotaConfig{MaxAlerts: 0})
	seedAlerts(st2, "t1", 100)
	if err := qm2.CheckAlert("t1"); err != nil {
		t.Fatalf("CheckAlert (100/0=unlimited) should pass, got: %v", err)
	}
}

// TestQuotaManager_Usage 用量统计：返回当前设备/任务/告警数 + 生效配额。
func TestQuotaManager_Usage(t *testing.T) {
	qm, st := newTestQuotaManager(t, &store.QuotaConfig{MaxDevices: 100, MaxTasks: 200, MaxAlerts: 300})

	// 注入测试数据。
	seedDevicesForQuota(st, "t1", 7)
	seedTasks(st, "t1", 13) // Register 会自动创建 1 台占位设备（store 行为），故设备数 = 7+1 = 8
	seedAlerts(st, "t1", 5)

	// 查询用量。
	usage, err := qm.Usage("t1")
	if err != nil {
		t.Fatalf("Usage failed: %v", err)
	}
	// 设备数 = 7（显式 seed）+ 1（seedTasks 的 Register 占位设备）= 8。
	if usage.Devices != 8 {
		t.Fatalf("Devices = %d, want 8", usage.Devices)
	}
	if usage.Tasks != 13 {
		t.Fatalf("Tasks = %d, want 13", usage.Tasks)
	}
	if usage.Alerts != 5 {
		t.Fatalf("Alerts = %d, want 5", usage.Alerts)
	}
	// 应返回默认配额（未显式设置）。
	if usage.Quota.MaxDevices != 100 || usage.Quota.MaxTasks != 200 || usage.Quota.MaxAlerts != 300 {
		t.Fatalf("Quota = %+v, want {100,200,300}", usage.Quota)
	}

	// 设置配额后用量统计应反映新配额。
	if err := qm.SetQuota("t1", &store.QuotaConfig{MaxDevices: 50, MaxTasks: 60, MaxAlerts: 70}); err != nil {
		t.Fatalf("SetQuota failed: %v", err)
	}
	usage2, err := qm.Usage("t1")
	if err != nil {
		t.Fatalf("Usage after SetQuota failed: %v", err)
	}
	if usage2.Quota.MaxDevices != 50 || usage2.Quota.MaxTasks != 60 || usage2.Quota.MaxAlerts != 70 {
		t.Fatalf("Quota after SetQuota = %+v, want {50,60,70}", usage2.Quota)
	}
	// 用量数应不变。
	if usage2.Devices != 8 || usage2.Tasks != 13 || usage2.Alerts != 5 {
		t.Fatalf("Usage counts changed: %+v, want {8,13,5}", usage2)
	}
}

// TestQuotaManager_NoQuota 未设置配额时不限制（默认配额 0=不限）。
func TestQuotaManager_NoQuota(t *testing.T) {
	// 默认配额全 0（不限）。
	qm, st := newTestQuotaManager(t, &store.QuotaConfig{})

	// 注入大量数据，应全部通过（不限）。
	seedDevicesForQuota(st, "t1", 1000)
	seedTasks(st, "t1", 1000)
	seedAlerts(st, "t1", 1000)

	if err := qm.CheckDevice("t1"); err != nil {
		t.Fatalf("CheckDevice (no quota) should pass, got: %v", err)
	}
	if err := qm.CheckTask("t1"); err != nil {
		t.Fatalf("CheckTask (no quota) should pass, got: %v", err)
	}
	if err := qm.CheckAlert("t1"); err != nil {
		t.Fatalf("CheckAlert (no quota) should pass, got: %v", err)
	}
}

// TestQuotaManager_Disabled 配额检查未启用时直接放行（向后兼容）。
func TestQuotaManager_Disabled(t *testing.T) {
	st := store.NewMemoryStore()
	// enabled=false：所有 Check 方法直接放行。
	qm := NewQuotaManager(st, false, &store.QuotaConfig{MaxDevices: 1, MaxTasks: 1, MaxAlerts: 1})

	seedDevicesForQuota(st, "t1", 100)
	seedTasks(st, "t1", 100)
	seedAlerts(st, "t1", 100)

	if err := qm.CheckDevice("t1"); err != nil {
		t.Fatalf("CheckDevice (disabled) should pass, got: %v", err)
	}
	if err := qm.CheckTask("t1"); err != nil {
		t.Fatalf("CheckTask (disabled) should pass, got: %v", err)
	}
	if err := qm.CheckAlert("t1"); err != nil {
		t.Fatalf("CheckAlert (disabled) should pass, got: %v", err)
	}
	if qm.Enabled() {
		t.Fatal("Enabled() should be false")
	}
}

// TestQuotaManager_NilManager QuotaManager 为 nil 时所有方法安全放行（防御式编程）。
func TestQuotaManager_NilManager(t *testing.T) {
	var qm *QuotaManager

	if err := qm.CheckDevice("t1"); err != nil {
		t.Fatalf("nil.CheckDevice should pass, got: %v", err)
	}
	if err := qm.CheckTask("t1"); err != nil {
		t.Fatalf("nil.CheckTask should pass, got: %v", err)
	}
	if err := qm.CheckAlert("t1"); err != nil {
		t.Fatalf("nil.CheckAlert should pass, got: %v", err)
	}
	if cfg := qm.GetQuota("t1"); cfg.MaxDevices != 0 || cfg.MaxTasks != 0 || cfg.MaxAlerts != 0 {
		t.Fatalf("nil.GetQuota = %+v, want zero", cfg)
	}
	usage, err := qm.Usage("t1")
	if err != nil {
		t.Fatalf("nil.Usage should not error, got: %v", err)
	}
	if usage == nil || usage.Quota == nil {
		t.Fatal("nil.Usage should return non-nil result")
	}
}

// TestQuotaManager_TenantIsolation 配额按租户隔离：t1 超限不影响 t2。
func TestQuotaManager_TenantIsolation(t *testing.T) {
	qm, st := newTestQuotaManager(t, &store.QuotaConfig{MaxDevices: 5})

	// t1 满配（5 台），t2 空。
	seedDevicesForQuota(st, "t1", 5)
	seedDevicesForQuota(st, "t2", 0)

	// t1 超限。
	if err := qm.CheckDevice("t1"); err == nil {
		t.Fatal("CheckDevice (t1 5/5) should fail")
	}
	// t2 不受影响。
	if err := qm.CheckDevice("t2"); err != nil {
		t.Fatalf("CheckDevice (t2 0/5) should pass, got: %v", err)
	}
}

// TestQuotaManager_EmptyTenant 空租户 ID 时直接放行（防御式）。
func TestQuotaManager_EmptyTenant(t *testing.T) {
	qm, _ := newTestQuotaManager(t, &store.QuotaConfig{MaxDevices: 1, MaxTasks: 1, MaxAlerts: 1})

	if err := qm.CheckDevice(""); err != nil {
		t.Fatalf("CheckDevice(\"\") should pass, got: %v", err)
	}
	if err := qm.CheckTask(""); err != nil {
		t.Fatalf("CheckTask(\"\") should pass, got: %v", err)
	}
	if err := qm.CheckAlert(""); err != nil {
		t.Fatalf("CheckAlert(\"\") should pass, got: %v", err)
	}
}
