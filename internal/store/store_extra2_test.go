// store_extra2_test.go 补全 SQLStore 的纯函数与不需要数据库连接的方法测试。
//
// 覆盖范围：
//   - SQLStore.IsLeader（只读 s.isLeader/leaseUntil，不需要 db）
//   - SQLStore.StoreDeviceMetrics/DeviceMetrics/DeviceMetricsHistory（内存环形缓冲）
//   - SQLStore.SaveLogs/AgentLogs（内存暂存）
//   - SQLStore.cacheAgent（rdb 为 nil 时直接返回）
//   - SQLStore.Provision 含 | 字符的早期校验路径
//   - claimEpochCond/claimEpochArgs 纯函数
//   - randSQLSilenceID/randSQLChannelID/randSQLTemplateID 纯函数
//   - scanSilence/scanNotifyChannel/scanNotifyTemplate/scanOSTemplate/scanMiddlewareTemplate/scanRefreshToken
//     用 mock rowScanner 测试成功与失败路径（mockRowScanner 已在 sql_test.go 定义）
//   - SQLStore.NewSQLStore 延迟连接语义
//
// 测试风格与 sql_constructor_test.go / sql_test.go 一致：白盒（package store）。
package store

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"opsmesh/internal/events"
	"opsmesh/internal/proto"
)

// errRowScanner 始终返回错误的 rowScanner（用于测试 scan* 函数的错误路径）。
type errRowScanner struct{}

func (errRowScanner) Scan(dest ...interface{}) error { return errors.New("scan error") }

// ============================================================================
// SQLStore.IsLeader（不需要 db）
// ============================================================================

// TestSQLStore_IsLeader_DefaultFalse 验证新构造的 SQLStore IsLeader 返回 false。
func TestSQLStore_IsLeader_DefaultFalse(t *testing.T) {
	s := newSQLStoreForTest()
	if s.IsLeader() {
		t.Fatal("新构造的 SQLStore IsLeader 应返回 false")
	}
}

// TestSQLStore_IsLeader_ExpiredLease 验证租约过期返回 false。
func TestSQLStore_IsLeader_ExpiredLease(t *testing.T) {
	s := newSQLStoreForTest()
	s.mu.Lock()
	s.isLeader = true
	s.leaseUntil = time.Now().Add(-time.Hour) // 已过期
	s.mu.Unlock()
	if s.IsLeader() {
		t.Fatal("租约过期应返回 false")
	}
}

// TestSQLStore_IsLeader_ValidLease 验证有效租约返回 true。
func TestSQLStore_IsLeader_ValidLease(t *testing.T) {
	s := newSQLStoreForTest()
	s.mu.Lock()
	s.isLeader = true
	s.leaseUntil = time.Now().Add(time.Hour) // 未过期
	s.mu.Unlock()
	if !s.IsLeader() {
		t.Fatal("有效租约应返回 true")
	}
}

// TestSQLStore_IsLeader_NotLeader 验证 isLeader=false 返回 false。
func TestSQLStore_IsLeader_NotLeader(t *testing.T) {
	s := newSQLStoreForTest()
	s.mu.Lock()
	s.isLeader = false
	s.leaseUntil = time.Now().Add(time.Hour) // 未过期但不是 leader
	s.mu.Unlock()
	if s.IsLeader() {
		t.Fatal("isLeader=false 应返回 false")
	}
}

// ============================================================================
// SQLStore.StoreDeviceMetrics / DeviceMetrics / DeviceMetricsHistory（内存环形缓冲）
// ============================================================================

// TestSQLStore_StoreDeviceMetrics_Basic 验证基本存储与查询。
func TestSQLStore_StoreDeviceMetrics_Basic(t *testing.T) {
	s := newSQLStoreForTest()
	m := &proto.DeviceMetrics{DeviceID: "d1", CPU: proto.CPUMetrics{Cores: 4}}
	s.StoreDeviceMetrics("d1", m)
	got := s.DeviceMetrics("d1")
	if got == nil || got.CPU.Cores != 4 {
		t.Fatalf("DeviceMetrics =: %+v, want Cores=4", got)
	}
}

// TestSQLStore_StoreDeviceMetrics_EmptyDeviceID 验证空 deviceID 不存储。
func TestSQLStore_StoreDeviceMetrics_EmptyDeviceID(t *testing.T) {
	s := newSQLStoreForTest()
	s.StoreDeviceMetrics("", &proto.DeviceMetrics{DeviceID: "d1"})
	if s.DeviceMetrics("") != nil {
		t.Fatal("空 deviceID 应返回 nil")
	}
}

// TestSQLStore_StoreDeviceMetrics_NilMetrics 验证 nil metrics 不存储。
func TestSQLStore_StoreDeviceMetrics_NilMetrics(t *testing.T) {
	s := newSQLStoreForTest()
	s.StoreDeviceMetrics("d1", nil)
	if s.DeviceMetrics("d1") != nil {
		t.Fatal("nil metrics 应返回 nil")
	}
}

// TestSQLStore_DeviceMetrics_NotFound 验证不存在设备返回 nil。
func TestSQLStore_DeviceMetrics_NotFound(t *testing.T) {
	s := newSQLStoreForTest()
	if s.DeviceMetrics("no-exist") != nil {
		t.Fatal("不存在设备应返回 nil")
	}
}

// TestSQLStore_DeviceMetricsHistory_Basic 验证历史查询。
func TestSQLStore_DeviceMetricsHistory_Basic(t *testing.T) {
	s := newSQLStoreForTest()
	base := time.Now()
	for i := 0; i < 3; i++ {
		s.StoreDeviceMetrics("d1", &proto.DeviceMetrics{
			DeviceID:    "d1",
			CPU:         proto.CPUMetrics{Cores: i + 1},
			CollectedAt: base.Add(time.Duration(i) * time.Second),
		})
	}
	got := s.DeviceMetricsHistory("d1", time.Time{})
	if len(got) != 3 {
		t.Fatalf("History len = %d, want 3", len(got))
	}
}

// TestSQLStore_DeviceMetricsHistory_NotFound 验证不存在设备返回 nil。
func TestSQLStore_DeviceMetricsHistory_NotFound(t *testing.T) {
	s := newSQLStoreForTest()
	if s.DeviceMetricsHistory("no-exist", time.Time{}) != nil {
		t.Fatal("不存在设备应返回 nil")
	}
}

// TestSQLStore_DeviceMetricsHistory_SinceFilter 验证 since 过滤。
func TestSQLStore_DeviceMetricsHistory_SinceFilter(t *testing.T) {
	s := newSQLStoreForTest()
	base := time.Now()
	for i := 0; i < 3; i++ {
		s.StoreDeviceMetrics("d1", &proto.DeviceMetrics{
			DeviceID:    "d1",
			CPU:         proto.CPUMetrics{Cores: i + 1},
			CollectedAt: base.Add(time.Duration(i) * time.Minute),
		})
	}
	got := s.DeviceMetricsHistory("d1", base.Add(time.Minute))
	if len(got) != 2 {
		t.Fatalf("History since filter len = %d, want 2", len(got))
	}
}

// TestSQLStore_DeviceMetrics_Overwrite 验证环形缓冲覆写。
func TestSQLStore_DeviceMetrics_Overwrite(t *testing.T) {
	s := newSQLStoreForTest()
	for i := 0; i < metricsRingDefaultCap+10; i++ {
		s.StoreDeviceMetrics("d1", &proto.DeviceMetrics{
			DeviceID:    "d1",
			CPU:         proto.CPUMetrics{Cores: i + 1},
			CollectedAt: time.Now(),
		})
	}
	got := s.DeviceMetricsHistory("d1", time.Time{})
	if len(got) != metricsRingDefaultCap {
		t.Fatalf("覆写后 History len = %d, want %d", len(got), metricsRingDefaultCap)
	}
}

// ============================================================================
// SQLStore.SaveLogs / AgentLogs（内存暂存）
// ============================================================================

// TestSQLStore_SaveLogs_AgentLogs_Basic 验证基本保存与查询。
func TestSQLStore_SaveLogs_AgentLogs_Basic(t *testing.T) {
	s := newSQLStoreForTest()
	report := &proto.LogReport{
		AgentID: "a1",
		LogName: "syslog",
		Lines:   []proto.LogLine{{Level: "INFO", Message: "hello"}},
	}
	if err := s.SaveLogs("t1", report); err != nil {
		t.Fatalf("SaveLogs 失败: %v", err)
	}
	got := s.AgentLogs("t1", "", "")
	if len(got) != 1 || got[0].TenantID != "t1" {
		t.Fatalf("AgentLogs = %+v, want 1 条 t1", got)
	}
}

// TestSQLStore_SaveLogs_Nil 验证 nil report 返回 nil。
func TestSQLStore_SaveLogs_Nil(t *testing.T) {
	s := newSQLStoreForTest()
	if err := s.SaveLogs("t1", nil); err != nil {
		t.Fatalf("SaveLogs nil 应返回 nil, got %v", err)
	}
}

// TestSQLStore_AgentLogs_Filter 验证各种过滤。
func TestSQLStore_AgentLogs_Filter(t *testing.T) {
	s := newSQLStoreForTest()
	s.SaveLogs("t1", &proto.LogReport{AgentID: "a1", LogName: "syslog"})
	s.SaveLogs("t1", &proto.LogReport{AgentID: "a2", LogName: "app.log"})
	s.SaveLogs("t2", &proto.LogReport{AgentID: "a1", LogName: "syslog"})

	if got := s.AgentLogs("", "", ""); len(got) != 3 {
		t.Fatalf("AgentLogs 全量 = %d, want 3", len(got))
	}
	if got := s.AgentLogs("t1", "", ""); len(got) != 2 {
		t.Fatalf("AgentLogs(t1) = %d, want 2", len(got))
	}
	if got := s.AgentLogs("t1", "a1", ""); len(got) != 1 {
		t.Fatalf("AgentLogs(t1,a1) = %d, want 1", len(got))
	}
	if got := s.AgentLogs("t1", "a1", "syslog"); len(got) != 1 {
		t.Fatalf("AgentLogs(t1,a1,syslog) = %d, want 1", len(got))
	}
	if got := s.AgentLogs("t1", "a1", "no-exist"); len(got) != 0 {
		t.Fatalf("AgentLogs(t1,a1,no-exist) = %d, want 0", len(got))
	}
}

// TestSQLStore_AgentLogs_DeepCopy 验证返回深拷贝。
func TestSQLStore_AgentLogs_DeepCopy(t *testing.T) {
	s := newSQLStoreForTest()
	s.SaveLogs("t1", &proto.LogReport{
		AgentID: "a1",
		Lines:   []proto.LogLine{{Level: "INFO", Message: "hello"}},
	})
	got := s.AgentLogs("t1", "", "")
	if len(got) != 1 || len(got[0].Lines) != 1 {
		t.Fatalf("AgentLogs = %+v", got)
	}
	got[0].Lines[0].Message = "modified"
	got2 := s.AgentLogs("t1", "", "")
	if got2[0].Lines[0].Message != "hello" {
		t.Fatal("深拷贝失效：外部修改污染了内部")
	}
}

// ============================================================================
// SQLStore.cacheAgent（rdb 为 nil 时直接返回）
// ============================================================================

// TestSQLStore_cacheAgent_NilRdb 验证 rdb 为 nil 时不 panic。
func TestSQLStore_cacheAgent_NilRdb(t *testing.T) {
	s := newSQLStoreForTest()
	s.cacheAgent(&proto.AgentInfo{AgentID: "a1"}) // 不应 panic
}

// ============================================================================
// SQLStore.Provision 含 | 字符的早期校验路径
// ============================================================================

// TestSQLStore_Provision_InvalidChar 验证 Provision 含 | 字符返回错误（不触达 db）。
func TestSQLStore_Provision_InvalidChar(t *testing.T) {
	s := newSQLStoreForTest()
	if _, _, err := s.Provision("dev|x", "host", "t1"); err == nil {
		t.Fatal("Provision 含 | 的 deviceID 应返回错误")
	}
	if _, _, err := s.Provision("dev1", "host", "t|1"); err == nil {
		t.Fatal("Provision 含 | 的 tenantID 应返回错误")
	}
}

// ============================================================================
// claimEpochCond / claimEpochArgs 纯函数
// ============================================================================

// TestClaimEpochCond 验证 claimEpochCond 各种输入。
func TestClaimEpochCond(t *testing.T) {
	if got := claimEpochCond(0); got != "" {
		t.Fatalf("claimEpochCond(0) = %q, want empty", got)
	}
	if got := claimEpochCond(1); got != ` AND claim_epoch=?` {
		t.Fatalf("claimEpochCond(1) = %q, want ' AND claim_epoch=?'", got)
	}
	if got := claimEpochCond(-1); got != "" {
		t.Fatalf("claimEpochCond(-1) = %q, want empty", got)
	}
	if got := claimEpochCond(100); got != ` AND claim_epoch=?` {
		t.Fatalf("claimEpochCond(100) = %q, want ' AND claim_epoch=?'", got)
	}
}

// TestClaimEpochArgs 验证 claimEpochArgs 各种输入。
func TestClaimEpochArgs(t *testing.T) {
	got := claimEpochArgs("task-1", 0)
	if len(got) != 1 || got[0] != "task-1" {
		t.Fatalf("claimEpochArgs(task-1,0) = %+v, want [task-1]", got)
	}
	got = claimEpochArgs("task-1", 5)
	if len(got) != 2 || got[0] != "task-1" || got[1] != int64(5) {
		t.Fatalf("claimEpochArgs(task-1,5) = %+v, want [task-1,5]", got)
	}
	got = claimEpochArgs("task-1", -1)
	if len(got) != 1 || got[0] != "task-1" {
		t.Fatalf("claimEpochArgs(task-1,-1) = %+v, want [task-1]", got)
	}
}

// ============================================================================
// randSQLSilenceID / randSQLChannelID / randSQLTemplateID 纯函数
// ============================================================================

// TestRandSQLSilenceID 验证 randSQLSilenceID 返回带前缀的 ID。
func TestRandSQLSilenceID(t *testing.T) {
	id := randSQLSilenceID()
	if !strings.HasPrefix(id, "silence-") {
		t.Fatalf("randSQLSilenceID = %q, want prefix silence-", id)
	}
	id2 := randSQLSilenceID()
	if id == id2 {
		t.Fatal("两次 randSQLSilenceID 不应相同")
	}
}

// TestRandSQLChannelID 验证 randSQLChannelID 返回带前缀的 ID。
func TestRandSQLChannelID(t *testing.T) {
	id := randSQLChannelID()
	if !strings.HasPrefix(id, "ch-") {
		t.Fatalf("randSQLChannelID = %q, want prefix ch-", id)
	}
	id2 := randSQLChannelID()
	if id == id2 {
		t.Fatal("两次 randSQLChannelID 不应相同")
	}
}

// TestRandSQLTemplateID 验证 randSQLTemplateID 返回带前缀的 ID。
func TestRandSQLTemplateID(t *testing.T) {
	id := randSQLTemplateID()
	if !strings.HasPrefix(id, "tpl-") {
		t.Fatalf("randSQLTemplateID = %q, want prefix tpl-", id)
	}
	id2 := randSQLTemplateID()
	if id == id2 {
		t.Fatal("两次 randSQLTemplateID 不应相同")
	}
}

// ============================================================================
// scan* 函数测试（用 mockRowScanner，已在 sql_test.go 定义）
// ============================================================================

// TestScanSilence_Success 验证 scanSilence 成功路径。
func TestScanSilence_Success(t *testing.T) {
	now := time.Now()
	row := &mockRowScanner{vals: []interface{}{
		"silence-1",         // ID
		"t1",                // TenantID
		[]byte(`{"k":"v"}`), // matchLabelsJSON
		sql.NullTime{Time: now, Valid: true},          // StartAt
		sql.NullTime{Time: now.Add(time.Hour), Valid: true}, // EndAt
		sql.NullString{String: "admin", Valid: true},  // CreatedBy
		"test reason",                                  // Reason
		sql.NullTime{Time: now, Valid: true},          // CreatedAt
	}}
	r := scanSilence(row)
	if r == nil {
		t.Fatal("scanSilence 应返回非 nil")
	}
	if r.ID != "silence-1" || r.TenantID != "t1" {
		t.Fatalf("scanSilence = %+v", r)
	}
	if r.MatchLabels == nil || r.MatchLabels["k"] != "v" {
		t.Fatalf("MatchLabels = %+v, want k=v", r.MatchLabels)
	}
	if r.CreatedBy != "admin" || r.Reason != "test reason" {
		t.Fatalf("CreatedBy/Reason = %q/%q", r.CreatedBy, r.Reason)
	}
}

// TestScanSilence_Error 验证 scanSilence 扫描失败返回 nil。
func TestScanSilence_Error(t *testing.T) {
	if r := scanSilence(errRowScanner{}); r != nil {
		t.Fatal("scanSilence 错误应返回 nil")
	}
}

// TestScanSilence_EmptyLabels 验证 scanSilence 空 matchLabels。
func TestScanSilence_EmptyLabels(t *testing.T) {
	now := time.Now()
	row := &mockRowScanner{vals: []interface{}{
		"silence-1", "t1", []byte{},
		sql.NullTime{Time: now, Valid: true},
		sql.NullTime{Time: now, Valid: true},
		sql.NullString{}, "", // CreatedBy 为空，Reason 为空字符串
		sql.NullTime{Time: now, Valid: true},
	}}
	r := scanSilence(row)
	if r == nil || r.MatchLabels != nil {
		t.Fatalf("scanSilence 空 labels: %+v", r)
	}
}

// TestScanNotifyChannel_Success 验证 scanNotifyChannel 成功路径。
func TestScanNotifyChannel_Success(t *testing.T) {
	now := time.Now()
	row := &mockRowScanner{vals: []interface{}{
		"ch-1",        // ID
		"t1",          // TenantID
		"dingtalk",    // Name
		"webhook",     // Type
		`{"url":"x"}`, // Config
		true,          // Enabled
		now,           // CreatedAt
		now,           // UpdatedAt
	}}
	c := scanNotifyChannel(row)
	if c == nil {
		t.Fatal("scanNotifyChannel 应返回非 nil")
	}
	if c.ID != "ch-1" || c.Name != "dingtalk" || c.Type != "webhook" {
		t.Fatalf("scanNotifyChannel = %+v", c)
	}
	if !c.Enabled {
		t.Fatal("Enabled 应为 true")
	}
}

// TestScanNotifyChannel_Error 验证 scanNotifyChannel 扫描失败返回 nil。
func TestScanNotifyChannel_Error(t *testing.T) {
	if c := scanNotifyChannel(errRowScanner{}); c != nil {
		t.Fatal("scanNotifyChannel 错误应返回 nil")
	}
}

// TestScanNotifyTemplate_Success 验证 scanNotifyTemplate 成功路径。
func TestScanNotifyTemplate_Success(t *testing.T) {
	now := time.Now()
	row := &mockRowScanner{vals: []interface{}{
		"tpl-1",     // ID
		"t1",        // TenantID
		"alert-tpl", // Name
		"alert",     // Type
		"Title",     // Title
		"Body",      // Body
		sql.NullString{String: "markdown", Valid: true}, // Format
		now,         // CreatedAt
		now,         // UpdatedAt
	}}
	tpl := scanNotifyTemplate(row)
	if tpl == nil {
		t.Fatal("scanNotifyTemplate 应返回非 nil")
	}
	if tpl.ID != "tpl-1" || tpl.Title != "Title" || tpl.Format != "markdown" {
		t.Fatalf("scanNotifyTemplate = %+v", tpl)
	}
}

// TestScanNotifyTemplate_Error 验证 scanNotifyTemplate 扫描失败返回 nil。
func TestScanNotifyTemplate_Error(t *testing.T) {
	if tpl := scanNotifyTemplate(errRowScanner{}); tpl != nil {
		t.Fatal("scanNotifyTemplate 错误应返回 nil")
	}
}

// TestScanOSTemplate_Success 验证 scanOSTemplate 成功路径。
func TestScanOSTemplate_Success(t *testing.T) {
	now := time.Now()
	row := &mockRowScanner{vals: []interface{}{
		"os-1",      // ID
		"t1",        // TenantID
		"centos-7",  // Name
		"centos",    // OS
		"7",         // Version
		"amd64",     // Arch
		"http://x",  // InstallURL
		`{"k":"v"}`, // Config
		now,         // CreatedAt
		now,         // UpdatedAt
	}}
	tpl := scanOSTemplate(row)
	if tpl == nil {
		t.Fatal("scanOSTemplate 应返回非 nil")
	}
	if tpl.ID != "os-1" || tpl.OS != "centos" || tpl.Arch != "amd64" {
		t.Fatalf("scanOSTemplate = %+v", tpl)
	}
}

// TestScanOSTemplate_Error 验证 scanOSTemplate 扫描失败返回 nil。
func TestScanOSTemplate_Error(t *testing.T) {
	if tpl := scanOSTemplate(errRowScanner{}); tpl != nil {
		t.Fatal("scanOSTemplate 错误应返回 nil")
	}
}

// TestScanMiddlewareTemplate_Success 验证 scanMiddlewareTemplate 成功路径。
func TestScanMiddlewareTemplate_Success(t *testing.T) {
	now := time.Now()
	row := &mockRowScanner{vals: []interface{}{
		"mw-1",          // ID
		"t1",            // TenantID
		"mysql-8",       // Name
		"mysql",         // Type
		"8.0.35",        // Version
		`{"port":3306}`, // Config
		now,             // CreatedAt
		now,             // UpdatedAt
	}}
	tpl := scanMiddlewareTemplate(row)
	if tpl == nil {
		t.Fatal("scanMiddlewareTemplate 应返回非 nil")
	}
	if tpl.ID != "mw-1" || tpl.Type != "mysql" || tpl.Version != "8.0.35" {
		t.Fatalf("scanMiddlewareTemplate = %+v", tpl)
	}
}

// TestScanMiddlewareTemplate_Error 验证 scanMiddlewareTemplate 扫描失败返回 nil。
func TestScanMiddlewareTemplate_Error(t *testing.T) {
	if tpl := scanMiddlewareTemplate(errRowScanner{}); tpl != nil {
		t.Fatal("scanMiddlewareTemplate 错误应返回 nil")
	}
}

// TestScanRefreshToken_Success 验证 scanRefreshToken 成功路径。
func TestScanRefreshToken_Success(t *testing.T) {
	now := time.Now()
	row := &mockRowScanner{vals: []interface{}{
		"hash-1",           // TokenHash
		"user-1",           // UserID
		"t1",               // TenantID
		"fp-1",             // DeviceFP
		now.Add(time.Hour), // ExpiresAt
		now,                // CreatedAt
	}}
	rt := scanRefreshToken(row)
	if rt == nil {
		t.Fatal("scanRefreshToken 应返回非 nil")
	}
	if rt.TokenHash != "hash-1" || rt.UserID != "user-1" || rt.DeviceFP != "fp-1" {
		t.Fatalf("scanRefreshToken = %+v", rt)
	}
}

// TestScanRefreshToken_Error 验证 scanRefreshToken 扫描失败返回 nil。
func TestScanRefreshToken_Error(t *testing.T) {
	if rt := scanRefreshToken(errRowScanner{}); rt != nil {
		t.Fatal("scanRefreshToken 错误应返回 nil")
	}
}

// ============================================================================
// SQLStore.publish 边界（补充 sql_constructor_test.go 未覆盖的路径）
// ============================================================================

// TestSQLStore_Publish_WithBus_Extra 验证 publish 通过 bus 发布事件。
func TestSQLStore_Publish_WithBus_Extra(t *testing.T) {
	s := newSQLStoreForTest()
	bus := &recordingBus{}
	s.WithBus(bus)
	s.publish(events.Event{Action: "test-extra", Target: "t1"})
	if len(bus.events) != 1 || bus.events[0].Action != "test-extra" {
		t.Fatalf("publish 未通过 bus 发布: %+v", bus.events)
	}
}

// TestSQLStore_NewSQLStore_InvalidDSN 验证 NewSQLStore 无效 DSN 立即返回错误。
func TestSQLStore_NewSQLStore_InvalidDSN(t *testing.T) {
	s, err := NewSQLStore("invalid-dsn", "")
	if err == nil {
		defer s.DB().Close()
		t.Fatal("NewSQLStore 无效 DSN 应返回错误")
	}
}
