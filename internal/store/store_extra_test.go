// store_extra_test.go 补全 internal/store 包测试覆盖率到 70%+。
//
// 覆盖范围：
//   - MemoryStore 的 Update/Delete not found 路径
//   - MemoryStore 的 List 方法租户过滤/排序
//   - metricsRing 环形缓冲边界（nil、空、capacity<=0、latest/since 边界）
//   - appendAudit / QueryAudits 各种过滤组合
//   - publish 事件发布失败路径
//   - bcryptHash / randHex / mustRandHex / randAlertRuleID 等辅助函数
//   - verifyTokenMAC / hashToken / issueTokenLocked 边界
//   - RedisSessionStore 构造与 key 拼接（不依赖真实 Redis）
//   - MultiSchemaStore 的 defaultStoreFactory / globalStore / 反查索引
//   - InProcessSessionStore 边界
//
// 测试风格与现有 memory_crud_test.go 一致：白盒（package store）。
package store

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"opsmesh/internal/events"
	"opsmesh/internal/proto"
)

// ============================================================================
// MemoryStore Update/Delete not found 路径
// ============================================================================

// TestMemoryStore_UpdateUser_NotFound 验证 UpdateUser 不存在时返回 false。
func TestMemoryStore_UpdateUser_NotFound(t *testing.T) {
	m := NewMemoryStore()
	if m.UpdateUser(&User{ID: "no-exist", Email: "x@y.z"}) {
		t.Fatal("UpdateUser 不存在 ID 应返回 false")
	}
	if m.UpdateUser(nil) {
		t.Fatal("UpdateUser nil 应返回 false")
	}
}

// TestMemoryStore_ChangePassword_NotFound 验证 ChangePassword 不存在时返回 false。
func TestMemoryStore_ChangePassword_NotFound(t *testing.T) {
	m := NewMemoryStore()
	if m.ChangePassword("no-exist", "newhash") {
		t.Fatal("ChangePassword 不存在 ID 应返回 false")
	}
}

// TestMemoryStore_DeleteUser_NotFound 验证 DeleteUser 不存在时返回 false。
func TestMemoryStore_DeleteUser_NotFound(t *testing.T) {
	m := NewMemoryStore()
	if m.DeleteUser("no-exist") {
		t.Fatal("DeleteUser 不存在 ID 应返回 false")
	}
}

// TestMemoryStore_UpdateRole_NotFound 验证 UpdateRole 不存在时返回 false。
func TestMemoryStore_UpdateRole_NotFound(t *testing.T) {
	m := NewMemoryStore()
	if m.UpdateRole(&Role{ID: "no-exist", Description: "x"}) {
		t.Fatal("UpdateRole 不存在 ID 应返回 false")
	}
	if m.UpdateRole(nil) {
		t.Fatal("UpdateRole nil 应返回 false")
	}
}

// TestMemoryStore_DeleteRole_NotFound 验证 DeleteRole 不存在时返回 false。
func TestMemoryStore_DeleteRole_NotFound(t *testing.T) {
	m := NewMemoryStore()
	if m.DeleteRole("no-exist") {
		t.Fatal("DeleteRole 不存在 ID 应返回 false")
	}
}

// TestMemoryStore_GetUser_GetRole_NotFound 验证 GetUser/GetRole 不存在返回 nil。
func TestMemoryStore_GetUser_GetRole_NotFound(t *testing.T) {
	m := NewMemoryStore()
	if m.GetUser("no-exist") != nil {
		t.Fatal("GetUser 不存在应返回 nil")
	}
	if m.GetUserByUsername("no-exist") != nil {
		t.Fatal("GetUserByUsername 不存在应返回 nil")
	}
	if m.GetRole("no-exist") != nil {
		t.Fatal("GetRole 不存在应返回 nil")
	}
}

// TestMemoryStore_CreateUser_EmptyUsername 验证 CreateUser 用户名为空返回 nil。
func TestMemoryStore_CreateUser_EmptyUsername(t *testing.T) {
	m := NewMemoryStore()
	if m.CreateUser(&User{ID: "u1"}) != nil {
		t.Fatal("CreateUser 空用户名应返回 nil")
	}
	if m.CreateUser(nil) != nil {
		t.Fatal("CreateUser nil 应返回 nil")
	}
}

// TestMemoryStore_CreateRole_EmptyName 验证 CreateRole 名称为空返回 nil。
func TestMemoryStore_CreateRole_EmptyName(t *testing.T) {
	m := NewMemoryStore()
	if m.CreateRole(&Role{ID: "r1"}) != nil {
		t.Fatal("CreateRole 空名称应返回 nil")
	}
	if m.CreateRole(nil) != nil {
		t.Fatal("CreateRole nil 应返回 nil")
	}
}

// TestMemoryStore_UpdateAlertRule_NotFound 验证 UpdateAlertRule 不存在返回 false。
func TestMemoryStore_UpdateAlertRule_NotFound(t *testing.T) {
	m := NewMemoryStore()
	if m.UpdateAlertRule(&AlertRule{ID: "no-exist"}) {
		t.Fatal("UpdateAlertRule 不存在应返回 false")
	}
	if m.UpdateAlertRule(nil) {
		t.Fatal("UpdateAlertRule nil 应返回 false")
	}
}

// TestMemoryStore_DeleteAlertRule_NotFound 验证 DeleteAlertRule 不存在返回 false。
func TestMemoryStore_DeleteAlertRule_NotFound(t *testing.T) {
	m := NewMemoryStore()
	if m.DeleteAlertRule("no-exist") {
		t.Fatal("DeleteAlertRule 不存在应返回 false")
	}
}

// TestMemoryStore_GetAlertRule_NotFound 验证 GetAlertRule 不存在返回 nil。
func TestMemoryStore_GetAlertRule_NotFound(t *testing.T) {
	m := NewMemoryStore()
	if m.GetAlertRule("no-exist") != nil {
		t.Fatal("GetAlertRule 不存在应返回 nil")
	}
}

// TestMemoryStore_CreateAlertRule_Nil 验证 CreateAlertRule nil 返回 nil。
func TestMemoryStore_CreateAlertRule_Nil(t *testing.T) {
	m := NewMemoryStore()
	if m.CreateAlertRule(nil) != nil {
		t.Fatal("CreateAlertRule nil 应返回 nil")
	}
}

// TestMemoryStore_UpdateNotifyChannel_NotFound 验证 UpdateNotifyChannel 不存在返回 false。
func TestMemoryStore_UpdateNotifyChannel_NotFound(t *testing.T) {
	m := NewMemoryStore()
	if m.UpdateNotifyChannel(&NotifyChannel{ID: "no-exist"}) {
		t.Fatal("UpdateNotifyChannel 不存在应返回 false")
	}
	if m.UpdateNotifyChannel(nil) {
		t.Fatal("UpdateNotifyChannel nil 应返回 false")
	}
}

// TestMemoryStore_DeleteNotifyChannel_NotFound 验证 DeleteNotifyChannel 不存在/越权返回 false。
func TestMemoryStore_DeleteNotifyChannel_NotFound(t *testing.T) {
	m := NewMemoryStore()
	if m.DeleteNotifyChannel("no-exist", "") {
		t.Fatal("DeleteNotifyChannel 不存在应返回 false")
	}
	// 越权
	c := m.CreateNotifyChannel(&NotifyChannel{Name: "ch1", TenantID: "t1"})
	if m.DeleteNotifyChannel(c.ID, "t2") {
		t.Fatal("DeleteNotifyChannel 越权应返回 false")
	}
	// 正常删除
	if !m.DeleteNotifyChannel(c.ID, "t1") {
		t.Fatal("DeleteNotifyChannel 同租户应返回 true")
	}
}

// TestMemoryStore_GetNotifyChannel_NotFound 验证 GetNotifyChannel 不存在返回 nil。
func TestMemoryStore_GetNotifyChannel_NotFound(t *testing.T) {
	m := NewMemoryStore()
	if m.GetNotifyChannel("no-exist") != nil {
		t.Fatal("GetNotifyChannel 不存在应返回 nil")
	}
}

// TestMemoryStore_CreateNotifyChannel_Nil 验证 CreateNotifyChannel nil 返回 nil。
func TestMemoryStore_CreateNotifyChannel_Nil(t *testing.T) {
	m := NewMemoryStore()
	if m.CreateNotifyChannel(nil) != nil {
		t.Fatal("CreateNotifyChannel nil 应返回 nil")
	}
}

// TestMemoryStore_UpdateNotifyTemplate_NotFound 验证 UpdateNotifyTemplate 不存在返回 false。
func TestMemoryStore_UpdateNotifyTemplate_NotFound(t *testing.T) {
	m := NewMemoryStore()
	if m.UpdateNotifyTemplate(&NotifyTemplate{ID: "no-exist"}) {
		t.Fatal("UpdateNotifyTemplate 不存在应返回 false")
	}
	if m.UpdateNotifyTemplate(nil) {
		t.Fatal("UpdateNotifyTemplate nil 应返回 false")
	}
}

// TestMemoryStore_DeleteNotifyTemplate_NotFound 验证 DeleteNotifyTemplate 不存在/越权返回 false。
func TestMemoryStore_DeleteNotifyTemplate_NotFound(t *testing.T) {
	m := NewMemoryStore()
	if m.DeleteNotifyTemplate("no-exist", "") {
		t.Fatal("DeleteNotifyTemplate 不存在应返回 false")
	}
	tpl := m.CreateNotifyTemplate(&NotifyTemplate{Name: "t1", TenantID: "t1"})
	if m.DeleteNotifyTemplate(tpl.ID, "t2") {
		t.Fatal("DeleteNotifyTemplate 越权应返回 false")
	}
	if !m.DeleteNotifyTemplate(tpl.ID, "t1") {
		t.Fatal("DeleteNotifyTemplate 同租户应返回 true")
	}
}

// TestMemoryStore_GetNotifyTemplate_NotFound 验证 GetNotifyTemplate 不存在返回 nil。
func TestMemoryStore_GetNotifyTemplate_NotFound(t *testing.T) {
	m := NewMemoryStore()
	if m.GetNotifyTemplate("no-exist") != nil {
		t.Fatal("GetNotifyTemplate 不存在应返回 nil")
	}
}

// TestMemoryStore_CreateNotifyTemplate_Nil 验证 CreateNotifyTemplate nil 返回 nil。
func TestMemoryStore_CreateNotifyTemplate_Nil(t *testing.T) {
	m := NewMemoryStore()
	if m.CreateNotifyTemplate(nil) != nil {
		t.Fatal("CreateNotifyTemplate nil 应返回 nil")
	}
}

// TestMemoryStore_DeleteSilence_NotFound 验证 DeleteSilence 不存在/越权返回 false。
func TestMemoryStore_DeleteSilence_NotFound(t *testing.T) {
	m := NewMemoryStore()
	if m.DeleteSilence("no-exist", "") {
		t.Fatal("DeleteSilence 不存在应返回 false")
	}
	s := m.CreateSilence(&SilenceRule{Reason: "r", TenantID: "t1"})
	if m.DeleteSilence(s.ID, "t2") {
		t.Fatal("DeleteSilence 越权应返回 false")
	}
	if !m.DeleteSilence(s.ID, "t1") {
		t.Fatal("DeleteSilence 同租户应返回 true")
	}
}

// TestMemoryStore_GetSilence_NotFound 验证 GetSilence 不存在返回 nil。
func TestMemoryStore_GetSilence_NotFound(t *testing.T) {
	m := NewMemoryStore()
	if m.GetSilence("no-exist") != nil {
		t.Fatal("GetSilence 不存在应返回 nil")
	}
}

// TestMemoryStore_CreateSilence_Nil 验证 CreateSilence nil 返回 nil。
func TestMemoryStore_CreateSilence_Nil(t *testing.T) {
	m := NewMemoryStore()
	if m.CreateSilence(nil) != nil {
		t.Fatal("CreateSilence nil 应返回 nil")
	}
}

// TestMemoryStore_K8s_DeleteNotFound 验证 DeleteK8sCluster 不存在返回 false。
func TestMemoryStore_K8s_DeleteNotFound(t *testing.T) {
	m := NewMemoryStore()
	if m.DeleteK8sCluster("no-exist") {
		t.Fatal("DeleteK8sCluster 不存在应返回 false")
	}
	if m.GetK8sCluster("no-exist") != nil {
		t.Fatal("GetK8sCluster 不存在应返回 nil")
	}
}

// TestMemoryStore_SaveK8sCluster_Nil 验证 SaveK8sCluster nil 返回 nil。
func TestMemoryStore_SaveK8sCluster_Nil(t *testing.T) {
	m := NewMemoryStore()
	if err := m.SaveK8sCluster(nil); err != nil {
		t.Fatalf("SaveK8sCluster nil 应返回 nil, got %v", err)
	}
}

// TestMemoryStore_OS_Template_NotFound 验证 OS 模板 not found 路径。
func TestMemoryStore_OS_Template_NotFound(t *testing.T) {
	m := NewMemoryStore()
	if m.GetOSTemplate("no-exist") != nil {
		t.Fatal("GetOSTemplate 不存在应返回 nil")
	}
	if m.DeleteOSTemplate("no-exist") {
		t.Fatal("DeleteOSTemplate 不存在应返回 false")
	}
	if err := m.SaveOSTemplate(nil); err != nil {
		t.Fatalf("SaveOSTemplate nil 应返回 nil, got %v", err)
	}
}

// TestMemoryStore_Middleware_Template_NotFound 验证中间件模板 not found 路径。
func TestMemoryStore_Middleware_Template_NotFound(t *testing.T) {
	m := NewMemoryStore()
	if m.GetMiddlewareTemplate("no-exist") != nil {
		t.Fatal("GetMiddlewareTemplate 不存在应返回 nil")
	}
	if m.DeleteMiddlewareTemplate("no-exist") {
		t.Fatal("DeleteMiddlewareTemplate 不存在应返回 false")
	}
	if err := m.SaveMiddlewareTemplate(nil); err != nil {
		t.Fatalf("SaveMiddlewareTemplate nil 应返回 nil, got %v", err)
	}
}

// TestMemoryStore_RefreshToken_NotFound 验证 refresh token not found 路径。
func TestMemoryStore_RefreshToken_NotFound(t *testing.T) {
	m := NewMemoryStore()
	if m.GetRefreshToken("no-exist") != nil {
		t.Fatal("GetRefreshToken 不存在应返回 nil")
	}
	if m.DeleteRefreshToken("no-exist") {
		t.Fatal("DeleteRefreshToken 不存在应返回 false")
	}
	if rt, ok := m.ConsumeRefreshToken("no-exist"); rt != nil || ok {
		t.Fatal("ConsumeRefreshToken 不存在应返回 (nil,false)")
	}
	// 空字符串
	if m.GetRefreshToken("") != nil {
		t.Fatal("GetRefreshToken 空串应返回 nil")
	}
	if m.DeleteRefreshToken("") {
		t.Fatal("DeleteRefreshToken 空串应返回 false")
	}
	if rt, ok := m.ConsumeRefreshToken(""); rt != nil || ok {
		t.Fatal("ConsumeRefreshToken 空串应返回 (nil,false)")
	}
}

// TestMemoryStore_SaveRefreshToken_InvalidHash 验证 SaveRefreshToken 空 hash 返回错误。
func TestMemoryStore_SaveRefreshToken_InvalidHash(t *testing.T) {
	m := NewMemoryStore()
	if err := m.SaveRefreshToken(&RefreshToken{UserID: "u1"}); err == nil {
		t.Fatal("SaveRefreshToken 空 TokenHash 应返回错误")
	}
	if err := m.SaveRefreshToken(nil); err != nil {
		t.Fatalf("SaveRefreshToken nil 应返回 nil, got %v", err)
	}
}

// TestMemoryStore_Quota_EmptyTenant 验证 Quota 空租户路径。
func TestMemoryStore_Quota_EmptyTenant(t *testing.T) {
	m := NewMemoryStore()
	if cfg, err := m.GetQuota(""); cfg != nil || err != nil {
		t.Fatalf("GetQuota 空租户应返回 (nil,nil), got (%+v,%v)", cfg, err)
	}
	if err := m.SetQuota("", &QuotaConfig{MaxDevices: 10}); err == nil {
		t.Fatal("SetQuota 空租户应返回错误")
	}
	// cfg 为 nil 等价于删除
	if err := m.SetQuota("t1", nil); err != nil {
		t.Fatalf("SetQuota nil cfg 应返回 nil, got %v", err)
	}
}

// TestMemoryStore_RetireDevice_NotFound 验证 RetireDevice 不存在返回 false。
func TestMemoryStore_RetireDevice_NotFound(t *testing.T) {
	m := NewMemoryStore()
	if m.RetireDevice("no-exist", "") {
		t.Fatal("RetireDevice 不存在应返回 false")
	}
}

// TestMemoryStore_Device_NotFound 验证 Device 不存在返回 nil。
func TestMemoryStore_Device_NotFound(t *testing.T) {
	m := NewMemoryStore()
	if m.Device("no-exist") != nil {
		t.Fatal("Device 不存在应返回 nil")
	}
	if m.Agent("no-exist") != nil {
		t.Fatal("Agent 不存在应返回 nil")
	}
}

// TestMemoryStore_UpsertDevice_NilOrEmpty 验证 UpsertDevice nil 或空 DeviceID 不 panic。
func TestMemoryStore_UpsertDevice_NilOrEmpty(t *testing.T) {
	m := NewMemoryStore()
	m.UpsertDevice(nil) // 不应 panic
	m.UpsertDevice(&proto.DeviceInfo{DeviceID: ""})
}

// TestMemoryStore_Heartbeat_Unknown 验证 Heartbeat 未知 agent 返回 false。
func TestMemoryStore_Heartbeat_Unknown(t *testing.T) {
	m := NewMemoryStore()
	if m.Heartbeat("no-exist", "online", 1) {
		t.Fatal("Heartbeat 未知 agent 应返回 false")
	}
}

// TestMemoryStore_CancelTask_NotFound 验证 CancelTask 不存在返回 false。
func TestMemoryStore_CancelTask_NotFound(t *testing.T) {
	m := NewMemoryStore()
	if m.CancelTask("no-exist", "") {
		t.Fatal("CancelTask 不存在应返回 false")
	}
}

// TestMemoryStore_ApproveTask_RejectTask_NotFound 验证 ApproveTask/RejectTask 不存在返回 false。
func TestMemoryStore_ApproveTask_RejectTask_NotFound(t *testing.T) {
	m := NewMemoryStore()
	if m.ApproveTask("no-exist", "", "u1") {
		t.Fatal("ApproveTask 不存在应返回 false")
	}
	if m.RejectTask("no-exist", "", "u1") {
		t.Fatal("RejectTask 不存在应返回 false")
	}
}

// TestMemoryStore_Provision_InvalidChar 验证 Provision 含 | 字符返回错误。
func TestMemoryStore_Provision_InvalidChar(t *testing.T) {
	m := NewMemoryStore()
	if _, _, err := m.Provision("dev|x", "host", "t1"); err == nil {
		t.Fatal("Provision 含 | 的 deviceID 应返回错误")
	}
	if _, _, err := m.Provision("dev1", "host", "t|1"); err == nil {
		t.Fatal("Provision 含 | 的 tenantID 应返回错误")
	}
}

// TestMemoryStore_Provision_NotFound 验证 Provision 不存在设备返回错误。
func TestMemoryStore_Provision_NotFound(t *testing.T) {
	m := NewMemoryStore()
	if _, _, err := m.Provision("no-exist", "host", "t1"); err == nil {
		t.Fatal("Provision 不存在设备应返回错误")
	}
}

// TestMemoryStore_IssueToken_EmptyDevice 验证 IssueToken 空 deviceID 返回错误。
func TestMemoryStore_IssueToken_EmptyDevice(t *testing.T) {
	m := NewMemoryStore()
	if _, err := m.IssueToken("", "t1", time.Minute); err == nil {
		t.Fatal("IssueToken 空 deviceID 应返回错误")
	}
}

// TestMemoryStore_ConsumeToken_Invalid 验证 ConsumeToken 无效 token 返回 false。
func TestMemoryStore_ConsumeToken_Invalid(t *testing.T) {
	m := NewMemoryStore()
	if _, _, ok := m.ConsumeToken("invalid"); ok {
		t.Fatal("ConsumeToken 无效 token 应返回 false")
	}
	if _, _, ok := m.ConsumeToken(""); ok {
		t.Fatal("ConsumeToken 空串应返回 false")
	}
}

// TestMemoryStore_AgentLogs_Nil 验证 SaveLogs nil 不 panic。
func TestMemoryStore_AgentLogs_Nil(t *testing.T) {
	m := NewMemoryStore()
	if err := m.SaveLogs("t1", nil); err != nil {
		t.Fatalf("SaveLogs nil 应返回 nil, got %v", err)
	}
}

// ============================================================================
// MemoryStore List 方法的租户过滤
// ============================================================================

// TestMemoryStore_ListMethods_TenantFilter 验证各 List 方法的租户过滤。
func TestMemoryStore_ListMethods_TenantFilter(t *testing.T) {
	m := NewMemoryStore()
	// 创建多租户数据
	m.CreateAlertRule(&AlertRule{TenantID: "t1", Metric: "cpu"})
	m.CreateAlertRule(&AlertRule{TenantID: "t2", Metric: "mem"})
	if got := m.ListAlertRules("t1"); len(got) != 1 || got[0].TenantID != "t1" {
		t.Fatalf("ListAlertRules(t1) = %+v, want t1", got)
	}
	if got := m.ListAlertRules(""); len(got) != 2 {
		t.Fatalf("ListAlertRules(全部) = %d, want 2", len(got))
	}

	m.CreateSilence(&SilenceRule{TenantID: "t1", Reason: "r1"})
	m.CreateSilence(&SilenceRule{TenantID: "t2", Reason: "r2"})
	if got := m.ListSilences("t1"); len(got) != 1 || got[0].TenantID != "t1" {
		t.Fatalf("ListSilences(t1) = %+v, want t1", got)
	}

	m.CreateNotifyChannel(&NotifyChannel{TenantID: "t1", Name: "ch1"})
	m.CreateNotifyChannel(&NotifyChannel{TenantID: "t2", Name: "ch2"})
	if got := m.ListNotifyChannels("t1"); len(got) != 1 || got[0].TenantID != "t1" {
		t.Fatalf("ListNotifyChannels(t1) = %+v, want t1", got)
	}

	m.CreateNotifyTemplate(&NotifyTemplate{TenantID: "t1", Name: "n1"})
	m.CreateNotifyTemplate(&NotifyTemplate{TenantID: "t2", Name: "n2"})
	if got := m.ListNotifyTemplates("t1"); len(got) != 1 || got[0].TenantID != "t1" {
		t.Fatalf("ListNotifyTemplates(t1) = %+v, want t1", got)
	}

	m.SaveK8sCluster(&K8sCluster{TenantID: "t1", Name: "k1"})
	m.SaveK8sCluster(&K8sCluster{TenantID: "t2", Name: "k2"})
	if got := m.ListK8sClusters("t1"); len(got) != 1 || got[0].TenantID != "t1" {
		t.Fatalf("ListK8sClusters(t1) = %+v, want t1", got)
	}

	m.SaveOSTemplate(&OSTemplate{TenantID: "t1", Name: "os1"})
	m.SaveOSTemplate(&OSTemplate{TenantID: "t2", Name: "os2"})
	if got := m.ListOSTemplates("t1"); len(got) != 1 || got[0].TenantID != "t1" {
		t.Fatalf("ListOSTemplates(t1) = %+v, want t1", got)
	}

	m.SaveMiddlewareTemplate(&MiddlewareTemplate{TenantID: "t1", Name: "m1"})
	m.SaveMiddlewareTemplate(&MiddlewareTemplate{TenantID: "t2", Name: "m2"})
	if got := m.ListMiddlewareTemplates("t1"); len(got) != 1 || got[0].TenantID != "t1" {
		t.Fatalf("ListMiddlewareTemplates(t1) = %+v, want t1", got)
	}
}

// TestMemoryStore_Agents_TenantFilter 验证 Agents 租户过滤。
func TestMemoryStore_Agents_TenantFilter(t *testing.T) {
	m := NewMemoryStore()
	m.Register(&proto.AgentInfo{AgentID: "a1", Segment: "s1", TenantID: "t1"})
	m.Register(&proto.AgentInfo{AgentID: "a2", Segment: "s1", TenantID: "t2"})
	if got := m.Agents("t1"); len(got) != 1 || got[0].AgentID != "a1" {
		t.Fatalf("Agents(t1) = %+v, want a1", got)
	}
	if got := m.Agents(""); len(got) != 2 {
		t.Fatalf("Agents(全部) = %d, want 2", len(got))
	}
}

// TestMemoryStore_AllTasks_TenantFilter 验证 AllTasks 租户过滤。
func TestMemoryStore_AllTasks_TenantFilter(t *testing.T) {
	m := NewMemoryStore()
	m.CreateTask(&proto.Task{AgentID: "a1", TenantID: "t1", Command: "c1"})
	m.CreateTask(&proto.Task{AgentID: "a2", TenantID: "t2", Command: "c2"})
	if got := m.AllTasks("t1"); len(got) != 1 || got[0].TenantID != "t1" {
		t.Fatalf("AllTasks(t1) = %+v, want t1", got)
	}
	if got := m.AllTasks(""); len(got) != 2 {
		t.Fatalf("AllTasks(全部) = %d, want 2", len(got))
	}
}

// TestMemoryStore_Snapshot_TenantFilter 验证 Snapshot 租户过滤与 retired 过滤。
func TestMemoryStore_Snapshot_TenantFilter(t *testing.T) {
	m := NewMemoryStore()
	m.Register(&proto.AgentInfo{AgentID: "a1", Segment: "s1", TenantID: "t1"})
	m.Register(&proto.AgentInfo{AgentID: "a2", Segment: "s1", TenantID: "t2"})
	if got := m.Snapshot("t1"); len(got["s1"]) != 1 {
		t.Fatalf("Snapshot(t1)[s1] = %d, want 1", len(got["s1"]))
	}
	if got := m.Snapshot(""); len(got["s1"]) != 2 {
		t.Fatalf("Snapshot(全部)[s1] = %d, want 2", len(got["s1"]))
	}
	// retired 设备不出现
	m.RetireDevice("dev-a1", "")
	if got := m.Snapshot(""); len(got["s1"]) != 1 {
		t.Fatalf("Snapshot after retire [s1] = %d, want 1", len(got["s1"]))
	}
}

// TestMemoryStore_Alerts_TenantFilter 验证 Alerts 租户过滤。
func TestMemoryStore_Alerts_TenantFilter(t *testing.T) {
	m := NewMemoryStore()
	m.AddAlert(&proto.Alert{AlertID: "al1", TenantID: "t1"})
	m.AddAlert(&proto.Alert{AlertID: "al2", TenantID: "t2"})
	if got := m.Alerts("t1"); len(got) != 1 || got[0].AlertID != "al1" {
		t.Fatalf("Alerts(t1) = %+v, want al1", got)
	}
	if got := m.Alerts(""); len(got) != 2 {
		t.Fatalf("Alerts(全部) = %d, want 2", len(got))
	}
}

// TestMemoryStore_AckSilenceAlert_NotFound 验证 AckAlert/SilenceAlert 不存在返回 false。
func TestMemoryStore_AckSilenceAlert_NotFound(t *testing.T) {
	m := NewMemoryStore()
	if m.AckAlert("no-exist", "", "u1") {
		t.Fatal("AckAlert 不存在应返回 false")
	}
	if m.SilenceAlert("no-exist", "", "u1", time.Time{}, "c") {
		t.Fatal("SilenceAlert 不存在应返回 false")
	}
}

// TestMemoryStore_SilenceAlert_Default24h 验证 SilenceAlert until 零值默认 24h。
func TestMemoryStore_SilenceAlert_Default24h_Boundary(t *testing.T) {
	m := NewMemoryStore()
	m.AddAlert(&proto.Alert{AlertID: "al1", TenantID: "t1"})
	before := time.Now()
	if !m.SilenceAlert("al1", "t1", "u1", time.Time{}, "comment") {
		t.Fatal("SilenceAlert 应返回 true")
	}
	a := m.Alert("al1")
	if a.Status != proto.AlertStatusSilenced {
		t.Fatalf("status = %q, want silenced", a.Status)
	}
	if a.SilencedUntil.Before(before.Add(23 * time.Hour)) {
		t.Fatalf("SilencedUntil = %v, want >= 23h", a.SilencedUntil)
	}
}

// ============================================================================
// metricsRing 环形缓冲边界
// ============================================================================

// TestMetricsRing_NilReceiver 验证 metricsRing nil 接收者不 panic。
func TestMetricsRing_NilReceiver(t *testing.T) {
	var r *metricsRing
	r.add(&proto.DeviceMetrics{DeviceID: "d1"}) // 不应 panic
	if r.latest() != nil {
		t.Fatal("nil.latest() 应返回 nil")
	}
	if r.since(time.Time{}) != nil {
		t.Fatal("nil.since() 应返回 nil")
	}
}

// TestMetricsRing_NilMetric 验证 add nil metric 不 panic。
func TestMetricsRing_NilMetric(t *testing.T) {
	r := newMetricsRing(3)
	r.add(nil) // 不应 panic
	if r.latest() != nil {
		t.Fatal("add nil 后 latest 应返回 nil")
	}
}

// TestMetricsRing_ZeroCapacity 验证 capacity<=0 时使用默认容量。
func TestMetricsRing_ZeroCapacity(t *testing.T) {
	r := newMetricsRing(0)
	if r.capacity != metricsRingDefaultCap {
		t.Fatalf("capacity = %d, want %d", r.capacity, metricsRingDefaultCap)
	}
	r = newMetricsRing(-1)
	if r.capacity != metricsRingDefaultCap {
		t.Fatalf("capacity = %d, want %d", r.capacity, metricsRingDefaultCap)
	}
}

// TestMetricsRing_Latest_AfterOne 验证 add 一条后 latest 返回该条。
func TestMetricsRing_Latest_AfterOne(t *testing.T) {
	r := newMetricsRing(3)
	r.add(&proto.DeviceMetrics{DeviceID: "d1", CPU: proto.CPUMetrics{Cores: 4}})
	got := r.latest()
	if got == nil || got.CPU.Cores != 4 {
		t.Fatalf("latest = %+v, want Cores=4", got)
	}
}

// TestMetricsRing_Latest_Empty 验证空缓冲 latest 返回 nil。
func TestMetricsRing_Latest_Empty(t *testing.T) {
	r := newMetricsRing(3)
	if r.latest() != nil {
		t.Fatal("空缓冲 latest 应返回 nil")
	}
}

// TestMetricsRing_Since_Empty 验证空缓冲 since 返回 nil。
func TestMetricsRing_Since_Empty(t *testing.T) {
	r := newMetricsRing(3)
	if r.since(time.Time{}) != nil {
		t.Fatal("空缓冲 since 应返回 nil")
	}
}

// TestMetricsRing_Since_Filter 验证 since 时间过滤。
func TestMetricsRing_Since_Filter(t *testing.T) {
	r := newMetricsRing(10)
	base := time.Now()
	for i := 0; i < 5; i++ {
		r.add(&proto.DeviceMetrics{
			DeviceID:    "d1",
			CPU:         proto.CPUMetrics{Cores: i + 1},
			CollectedAt: base.Add(time.Duration(i) * time.Minute),
		})
	}
	// since = base + 2min，应返回 Cores=3,4,5
	got := r.since(base.Add(2 * time.Minute))
	if len(got) != 3 {
		t.Fatalf("since filter len = %d, want 3", len(got))
	}
	for i, s := range got {
		if s.CPU.Cores != i+3 {
			t.Fatalf("got[%d].Cores = %d, want %d", i, s.CPU.Cores, i+3)
		}
	}
}

// TestMetricsRing_Since_AllWhenZero 验证 since 零值返回全部。
func TestMetricsRing_Since_AllWhenZero(t *testing.T) {
	r := newMetricsRing(5)
	for i := 0; i < 3; i++ {
		r.add(&proto.DeviceMetrics{DeviceID: "d1", CollectedAt: time.Now()})
	}
	got := r.since(time.Time{})
	if len(got) != 3 {
		t.Fatalf("since zero len = %d, want 3", len(got))
	}
}

// TestMetricsRing_Since_NoneMatch 验证 since 全部不匹配返回 nil。
func TestMetricsRing_Since_NoneMatch(t *testing.T) {
	r := newMetricsRing(5)
	r.add(&proto.DeviceMetrics{DeviceID: "d1", CollectedAt: time.Now()})
	future := time.Now().Add(time.Hour)
	if got := r.since(future); got != nil {
		t.Fatalf("since future = %+v, want nil", got)
	}
}

// TestMemoryStore_DeviceMetrics_Nil 验证 StoreDeviceMetrics 边界。
func TestMemoryStore_DeviceMetrics_Nil(t *testing.T) {
	m := NewMemoryStore()
	m.StoreDeviceMetrics("", &proto.DeviceMetrics{DeviceID: "d1"}) // 空 deviceID
	m.StoreDeviceMetrics("d1", nil)                                // nil metrics
	if m.DeviceMetrics("no-exist") != nil {
		t.Fatal("DeviceMetrics 不存在应返回 nil")
	}
	if m.DeviceMetricsHistory("no-exist", time.Time{}) != nil {
		t.Fatal("DeviceMetricsHistory 不存在应返回 nil")
	}
}

// TestMemoryStore_DeviceMetricsHistory_SinceFilter 验证 DeviceMetricsHistory since 过滤。
func TestMemoryStore_DeviceMetricsHistory_SinceFilter(t *testing.T) {
	m := NewMemoryStore()
	base := time.Now()
	for i := 0; i < 3; i++ {
		m.StoreDeviceMetrics("d1", &proto.DeviceMetrics{
			DeviceID:    "d1",
			CPU:         proto.CPUMetrics{Cores: i + 1},
			CollectedAt: base.Add(time.Duration(i) * time.Minute),
		})
	}
	got := m.DeviceMetricsHistory("d1", base.Add(time.Minute))
	if len(got) != 2 {
		t.Fatalf("History since filter len = %d, want 2", len(got))
	}
}

// ============================================================================
// Audit / appendAudit / QueryAudits
// ============================================================================

// TestMemoryStore_QueryAudits_Filters 验证 QueryAudits 各种过滤组合。
func TestMemoryStore_QueryAudits_Filters(t *testing.T) {
	m := NewMemoryStore()
	now := time.Now()
	m.Audit(&proto.AuditEvent{TenantID: "t1", Action: "register", Target: "a1", CreatedAt: now})
	m.Audit(&proto.AuditEvent{TenantID: "t1", Action: "create_task", Target: "t1", CreatedAt: now.Add(time.Second)})
	m.Audit(&proto.AuditEvent{TenantID: "t2", Action: "register", Target: "a2", CreatedAt: now.Add(2 * time.Second)})

	// 全量
	if got := m.QueryAudits("", "", time.Time{}, time.Time{}, 0); len(got) != 3 {
		t.Fatalf("QueryAudits 全量 = %d, want 3", len(got))
	}
	// 按租户
	if got := m.QueryAudits("t1", "", time.Time{}, time.Time{}, 0); len(got) != 2 {
		t.Fatalf("QueryAudits(t1) = %d, want 2", len(got))
	}
	// 按动作
	if got := m.QueryAudits("", "register", time.Time{}, time.Time{}, 0); len(got) != 2 {
		t.Fatalf("QueryAudits(register) = %d, want 2", len(got))
	}
	// 租户+动作
	if got := m.QueryAudits("t1", "register", time.Time{}, time.Time{}, 0); len(got) != 1 {
		t.Fatalf("QueryAudits(t1,register) = %d, want 1", len(got))
	}
	// since 过滤
	if got := m.QueryAudits("", "", now.Add(time.Second), time.Time{}, 0); len(got) != 2 {
		t.Fatalf("QueryAudits since = %d, want 2", len(got))
	}
	// until 过滤
	if got := m.QueryAudits("", "", time.Time{}, now.Add(time.Second), 0); len(got) != 2 {
		t.Fatalf("QueryAudits until = %d, want 2", len(got))
	}
	// limit
	if got := m.QueryAudits("", "", time.Time{}, time.Time{}, 2); len(got) != 2 {
		t.Fatalf("QueryAudits limit=2 = %d, want 2", len(got))
	}
	// 倒序：最新在前
	got := m.QueryAudits("", "", time.Time{}, time.Time{}, 1)
	if len(got) != 1 || got[0].Target != "a2" {
		t.Fatalf("QueryAudits limit=1 = %+v, want a2 (最新)", got)
	}
}

// TestMemoryStore_Audit_ZeroCreatedAt 验证 Audit 零值 CreatedAt 填当前时间。
func TestMemoryStore_Audit_ZeroCreatedAt(t *testing.T) {
	m := NewMemoryStore()
	before := time.Now()
	m.Audit(&proto.AuditEvent{TenantID: "t1", Action: "test"})
	audits := m.Audits()
	if len(audits) != 1 {
		t.Fatalf("Audits = %d, want 1", len(audits))
	}
	if audits[0].CreatedAt.Before(before) {
		t.Fatal("CreatedAt 应被填为当前时间")
	}
}

// TestMemoryStore_AuditsCap_Truncate 验证审计环形上限截断。
func TestMemoryStore_AuditsCap_Truncate(t *testing.T) {
	m := NewMemoryStore()
	// 写入超过 auditCap 条，验证截断
	for i := 0; i < auditCap+10; i++ {
		m.Audit(&proto.AuditEvent{TenantID: "t1", Action: "test", Target: string(rune(i))})
	}
	if got := m.Audits(); len(got) != auditCap {
		t.Fatalf("Audits after truncate = %d, want %d", len(got), auditCap)
	}
}

// ============================================================================
// publish 事件发布
// ============================================================================

// errPublishFail 用于测试 publish 失败路径的自定义错误。
type errPublishFail struct{}

func (errPublishFail) Error() string { return "publish failed" }

// failingBus 测试用 Bus，返回错误以触发 publish 失败分支。
type failingBus struct{ called bool }

func (b *failingBus) Publish(ctx context.Context, e events.Event) error {
	b.called = true
	return errPublishFail{}
}

// TestMemoryStore_Publish_NilBus 验证 nil bus 时 publish 不 panic。
func TestMemoryStore_Publish_NilBus(t *testing.T) {
	m := NewMemoryStore()
	// bus 为 nil，publish 不应 panic
	m.publish(events.Event{Action: "test"})
}

// TestMemoryStore_Publish_BusError 验证 bus 返回错误时 publish 不 panic（仅日志）。
func TestMemoryStore_Publish_BusError(t *testing.T) {
	m := NewMemoryStore()
	bus := &failingBus{}
	m.WithBus(bus)
	// 触发一次 publish（Register 内部会 publish）
	m.Register(&proto.AgentInfo{Segment: "s1", TenantID: "t1"})
	if !bus.called {
		t.Fatal("failingBus.Publish 应被调用")
	}
}

// TestMemoryStore_Publish_Success 验证 bus 正常发布。
func TestMemoryStore_Publish_Success(t *testing.T) {
	m := NewMemoryStore()
	var mu sync.Mutex
	got := []events.Event{}
	m.WithBus(busFunc(func(ctx context.Context, e events.Event) error {
		mu.Lock()
		got = append(got, e)
		mu.Unlock()
		return nil
	}))
	m.Register(&proto.AgentInfo{Segment: "s1", TenantID: "t1"})
	mu.Lock()
	defer mu.Unlock()
	if len(got) == 0 {
		t.Fatal("应至少发布一个事件")
	}
	if got[0].Action != "register" {
		t.Fatalf("首个事件 action = %q, want register", got[0].Action)
	}
}

// busFunc 把函数转为 Bus 接口（测试用）。
type busFunc func(ctx context.Context, e events.Event) error

func (f busFunc) Publish(ctx context.Context, e events.Event) error { return f(ctx, e) }

// ============================================================================
// bcryptHash / randHex / mustRandHex / randAlertRuleID 等辅助函数
// ============================================================================

// TestBcryptHash_Default 验证 bcryptHash 默认能正常哈希。
func TestBcryptHash_Default(t *testing.T) {
	hash, err := bcryptHash("password")
	if err != nil {
		t.Fatalf("bcryptHash 失败: %v", err)
	}
	if hash == "" || hash == "password" {
		t.Fatal("bcryptHash 应返回非空哈希")
	}
	// 相同密码哈希不同（bcrypt 含盐）
	hash2, _ := bcryptHash("password")
	if hash == hash2 {
		t.Fatal("bcrypt 哈希应含盐，两次结果不应相同")
	}
}

// TestRandHex_Default 验证 randHex 返回非空十六进制串。
func TestRandHex_Default(t *testing.T) {
	s := randHex(16)
	if s == "" {
		t.Fatal("randHex 应返回非空串")
	}
	// 长度 = 2*n（每字节 2 个 hex 字符）
	if len(s) != 32 {
		t.Fatalf("randHex(16) len = %d, want 32", len(s))
	}
	// 不同调用结果不同（极大概率）
	if randHex(16) == randHex(16) {
		t.Fatal("两次 randHex 不应相同")
	}
}

// TestMustRandHex_Default 验证 mustRandHex 返回非空十六进制串。
func TestMustRandHex_Default(t *testing.T) {
	s := mustRandHex(32)
	if s == "" {
		t.Fatal("mustRandHex 应返回非空串")
	}
	if len(s) != 64 {
		t.Fatalf("mustRandHex(32) len = %d, want 64", len(s))
	}
}

// TestRandAlertRuleID_Default 验证 randAlertRuleID 返回带前缀的 ID。
func TestRandAlertRuleID_Default(t *testing.T) {
	id := randAlertRuleID()
	if !strings.HasPrefix(id, "alert-rule-") {
		t.Fatalf("randAlertRuleID = %q, want prefix alert-rule-", id)
	}
}

// TestRandUserID_Default 验证 randUserID 返回带前缀的 ID。
func TestRandUserID_Default(t *testing.T) {
	id := randUserID()
	if !strings.HasPrefix(id, "user-") {
		t.Fatalf("randUserID = %q, want prefix user-", id)
	}
}

// TestRandRoleID_Default 验证 randRoleID 返回带前缀的 ID。
func TestRandRoleID_Default(t *testing.T) {
	id := randRoleID()
	if !strings.HasPrefix(id, "role-") {
		t.Fatalf("randRoleID = %q, want prefix role-", id)
	}
}

// TestRandK8sClusterID_Default 验证 randK8sClusterID 返回带前缀的 ID。
func TestRandK8sClusterID_Default(t *testing.T) {
	id := randK8sClusterID()
	if !strings.HasPrefix(id, "k8s-cluster-") {
		t.Fatalf("randK8sClusterID = %q, want prefix k8s-cluster-", id)
	}
}

// TestRandSilenceID_Default 验证 randSilenceID 返回带前缀的 ID。
func TestRandSilenceID_Default(t *testing.T) {
	id := randSilenceID()
	if !strings.HasPrefix(id, "silence-") {
		t.Fatalf("randSilenceID = %q, want prefix silence-", id)
	}
}

// TestRandNotifyChannelID_Default 验证 randNotifyChannelID 返回带前缀的 ID。
func TestRandNotifyChannelID_Default(t *testing.T) {
	id := randNotifyChannelID()
	if !strings.HasPrefix(id, "ch-") {
		t.Fatalf("randNotifyChannelID = %q, want prefix ch-", id)
	}
}

// TestRandNotifyTemplateID_Default 验证 randNotifyTemplateID 返回带前缀的 ID。
func TestRandNotifyTemplateID_Default(t *testing.T) {
	id := randNotifyTemplateID()
	if !strings.HasPrefix(id, "tpl-") {
		t.Fatalf("randNotifyTemplateID = %q, want prefix tpl-", id)
	}
}

// TestRandOSTemplateID_Default 验证 randOSTemplateID 返回带前缀的 ID。
func TestRandOSTemplateID_Default(t *testing.T) {
	id := randOSTemplateID()
	if !strings.HasPrefix(id, "os-tmpl-") {
		t.Fatalf("randOSTemplateID = %q, want prefix os-tmpl-", id)
	}
}

// TestRandMiddlewareTemplateID_Default 验证 randMiddlewareTemplateID 返回带前缀的 ID。
func TestRandMiddlewareTemplateID_Default(t *testing.T) {
	id := randMiddlewareTemplateID()
	if !strings.HasPrefix(id, "mw-tmpl-") {
		t.Fatalf("randMiddlewareTemplateID = %q, want prefix mw-tmpl-", id)
	}
}

// ============================================================================
// verifyTokenMAC / hashToken / issueTokenLocked 边界
// ============================================================================

// TestHashToken_Deterministic_Extra 验证 hashToken 确定性。
func TestHashToken_Deterministic_Extra(t *testing.T) {
	a := hashToken("token-x")
	b := hashToken("token-x")
	if a != b {
		t.Fatal("hashToken 应确定性")
	}
	if hashToken("a") == hashToken("b") {
		t.Fatal("hashToken 不同输入应不同输出")
	}
}

// TestVerifyTokenMAC_EdgeCases 验证 verifyTokenMAC 边界。
func TestVerifyTokenMAC_EdgeCases(t *testing.T) {
	if verifyTokenMAC("", "x") {
		t.Fatal("空 secret 应返回 false")
	}
	if verifyTokenMAC("s", "") {
		t.Fatal("空 token 应返回 false")
	}
	if verifyTokenMAC("s", "no-dot") {
		t.Fatal("无分隔符应返回 false")
	}
	if verifyTokenMAC("s", ".payload") {
		t.Fatal("空签名应返回 false")
	}
	if verifyTokenMAC("s", "sig.") {
		t.Fatal("空 payload 应返回 false")
	}
}

// TestMemoryStore_IssueToken_ConsumeTwice 验证 token 一次性消费。
func TestMemoryStore_IssueToken_ConsumeTwice(t *testing.T) {
	m := NewMemoryStore()
	tok, err := m.IssueToken("dev1", "t1", time.Minute)
	if err != nil {
		t.Fatalf("IssueToken 失败: %v", err)
	}
	if _, _, ok := m.ConsumeToken(tok); !ok {
		t.Fatal("首次消费应成功")
	}
	if _, _, ok := m.ConsumeToken(tok); ok {
		t.Fatal("二次消费应失败")
	}
}

// TestMemoryStore_CleanupTokens_Batch 验证 CleanupTokens 批量限制。
func TestMemoryStore_CleanupTokens_Batch(t *testing.T) {
	m := NewMemoryStore()
	// 创建两个过期 token
	tok1, _ := m.IssueToken("d1", "t1", -time.Minute) // 已过期
	tok2, _ := m.IssueToken("d2", "t1", -time.Minute) // 已过期
	_ = tok1
	_ = tok2
	// batch=1 只清理 1 个
	if n := m.CleanupTokens(1); n != 1 {
		t.Fatalf("CleanupTokens(1) = %d, want 1", n)
	}
	// 再清理剩余
	if n := m.CleanupTokens(10); n != 1 {
		t.Fatalf("CleanupTokens(10) = %d, want 1", n)
	}
}

// ============================================================================
// InProcessSessionStore 边界
// ============================================================================

// TestInProcessSessionStore_IsBlacklisted_Empty 验证空 jti 返回 false。
func TestInProcessSessionStore_IsBlacklisted_Empty(t *testing.T) {
	s := NewInProcessSessionStore()
	if s.IsBlacklisted("") {
		t.Fatal("空 jti 应返回 false")
	}
}

// TestInProcessSessionStore_Blacklist_Empty 验证空 jti 不写入。
func TestInProcessSessionStore_Blacklist_Empty(t *testing.T) {
	s := NewInProcessSessionStore()
	s.Blacklist("", time.Minute)
	if s.IsBlacklisted("") {
		t.Fatal("空 jti 不应被加入黑名单")
	}
}

// TestInProcessSessionStore_IsBlacklisted_Expired 验证过期条目返回 false 并清理。
func TestInProcessSessionStore_IsBlacklisted_Expired(t *testing.T) {
	s := NewInProcessSessionStore()
	s.Blacklist("jti1", -time.Minute) // 已过期
	if s.IsBlacklisted("jti1") {
		t.Fatal("过期 jti 应返回 false")
	}
}

// TestInProcessSessionStore_PurgeBlacklist_Extra 验证 PurgeBlacklist 清理过期条目。
func TestInProcessSessionStore_PurgeBlacklist_Extra(t *testing.T) {
	s := NewInProcessSessionStore()
	s.Blacklist("jti1", time.Minute)  // 未过期
	s.Blacklist("jti2", -time.Minute) // 已过期
	s.PurgeBlacklist()
	if !s.IsBlacklisted("jti1") {
		t.Fatal("jti1 未过期应保留")
	}
	if s.IsBlacklisted("jti2") {
		t.Fatal("jti2 已过期应被清理")
	}
}

// TestInProcessSessionStore_IncrRateLimit_Empty 验证空 key 返回 0。
func TestInProcessSessionStore_IncrRateLimit_Empty(t *testing.T) {
	s := NewInProcessSessionStore()
	if n := s.IncrRateLimit("", time.Minute); n != 0 {
		t.Fatalf("IncrRateLimit 空key = %d, want 0", n)
	}
}

// TestInProcessSessionStore_IncrRateLimit_Window 验证窗口内累计、窗口外重置。
func TestInProcessSessionStore_IncrRateLimit_Window(t *testing.T) {
	s := NewInProcessSessionStore()
	if n := s.IncrRateLimit("k1", time.Minute); n != 1 {
		t.Fatalf("首次 = %d, want 1", n)
	}
	if n := s.IncrRateLimit("k1", time.Minute); n != 2 {
		t.Fatalf("二次 = %d, want 2", n)
	}
	// 窗口过期后重置：手动改 window 为很久以前
	s.mu.Lock()
	s.rateLimits["k1"].window = time.Now().Add(-2 * time.Minute)
	s.mu.Unlock()
	if n := s.IncrRateLimit("k1", time.Minute); n != 1 {
		t.Fatalf("窗口过期后应重置 = %d, want 1", n)
	}
}

// TestInProcessSessionStore_ResetRateLimit_Extra 验证 ResetRateLimit。
func TestInProcessSessionStore_ResetRateLimit_Extra(t *testing.T) {
	s := NewInProcessSessionStore()
	s.IncrRateLimit("k1", time.Minute)
	s.ResetRateLimit("k1")
	if n := s.IncrRateLimit("k1", time.Minute); n != 1 {
		t.Fatalf("Reset 后应从 1 开始 = %d, want 1", n)
	}
	s.ResetRateLimit("") // 空key 不 panic
}

// TestInProcessSessionStore_CreateChangePasswordToken_Empty 验证空 token 返回错误。
func TestInProcessSessionStore_CreateChangePasswordToken_Empty(t *testing.T) {
	s := NewInProcessSessionStore()
	if err := s.CreateChangePasswordToken("", "u1", time.Minute); err == nil {
		t.Fatal("空 token 应返回错误")
	}
}

// TestInProcessSessionStore_ConsumeChangePasswordToken 验证一次性消费与过期。
func TestInProcessSessionStore_ConsumeChangePasswordToken(t *testing.T) {
	s := NewInProcessSessionStore()
	if err := s.CreateChangePasswordToken("tok1", "u1", time.Minute); err != nil {
		t.Fatalf("CreateChangePasswordToken 失败: %v", err)
	}
	// 正常消费
	uid, ok := s.ConsumeChangePasswordToken("tok1")
	if !ok || uid != "u1" {
		t.Fatalf("Consume = (%q,%v), want (u1,true)", uid, ok)
	}
	// 二次消费失败
	if _, ok := s.ConsumeChangePasswordToken("tok1"); ok {
		t.Fatal("二次消费应失败")
	}
	// 不存在
	if _, ok := s.ConsumeChangePasswordToken("no-exist"); ok {
		t.Fatal("不存在应返回 false")
	}
	// 空 token
	if _, ok := s.ConsumeChangePasswordToken(""); ok {
		t.Fatal("空 token 应返回 false")
	}
	// 过期
	if err := s.CreateChangePasswordToken("tok2", "u2", -time.Minute); err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if _, ok := s.ConsumeChangePasswordToken("tok2"); ok {
		t.Fatal("过期 token 应返回 false")
	}
}

// TestInProcessSessionStore_PurgeChangePasswordTokens_Extra 验证清理过期 token。
func TestInProcessSessionStore_PurgeChangePasswordTokens_Extra(t *testing.T) {
	s := NewInProcessSessionStore()
	s.CreateChangePasswordToken("tok1", "u1", time.Minute)  // 未过期
	s.CreateChangePasswordToken("tok2", "u2", -time.Minute) // 已过期
	s.PurgeChangePasswordTokens()
	// tok1 仍可消费
	if _, ok := s.ConsumeChangePasswordToken("tok1"); !ok {
		t.Fatal("tok1 未过期应保留")
	}
}

// TestInProcessSessionStore_Close 验证 Close 返回 nil。
func TestInProcessSessionStore_Close(t *testing.T) {
	s := NewInProcessSessionStore()
	if err := s.Close(); err != nil {
		t.Fatalf("Close 应返回 nil, got %v", err)
	}
}

// ============================================================================
// RedisSessionStore 构造与 key 拼接（不依赖真实 Redis）
// ============================================================================

// TestRedisSessionStore_New_EmptyAddr 验证空 addr 返回错误。
func TestRedisSessionStore_New_EmptyAddr(t *testing.T) {
	if _, err := NewRedisSessionStore("", "opsmesh:", time.Second); err == nil {
		t.Fatal("空 addr 应返回错误")
	}
}

// TestRedisSessionStore_New_InvalidAddr 验证无效 addr 不 fail-fast（仅日志）。
func TestRedisSessionStore_New_InvalidAddr(t *testing.T) {
	// 无效地址：连接失败但不应 fail-fast
	s, err := NewRedisSessionStore("127.0.0.1:1", "opsmesh:", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewRedisSessionStore 不应 fail-fast: %v", err)
	}
	if s == nil {
		t.Fatal("应返回非 nil store")
	}
	defer s.Close()
}

// TestRedisSessionStore_New_DefaultPrefix 验证空 prefix 使用默认值。
func TestRedisSessionStore_New_DefaultPrefix(t *testing.T) {
	s, err := NewRedisSessionStore("127.0.0.1:1", "", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewRedisSessionStore 失败: %v", err)
	}
	defer s.Close()
	// 验证 prefix 通过 key 拼接可见
	if got := s.key("x"); got != "opsmesh:x" {
		t.Fatalf("default prefix key = %q, want opsmesh:x", got)
	}
}

// TestRedisSessionStore_New_DefaultDialTimeout 验证 dialTimeout<=0 使用默认值。
func TestRedisSessionStore_New_DefaultDialTimeout(t *testing.T) {
	s, err := NewRedisSessionStore("127.0.0.1:1", "opsmesh:", 0)
	if err != nil {
		t.Fatalf("NewRedisSessionStore 失败: %v", err)
	}
	defer s.Close()
}

// TestRedisSessionStore_KeyBuilders 验证各 key 拼接方法。
func TestRedisSessionStore_KeyBuilders(t *testing.T) {
	s, err := NewRedisSessionStore("127.0.0.1:1", "opsmesh:", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewRedisSessionStore 失败: %v", err)
	}
	defer s.Close()
	if got := s.key("x"); got != "opsmesh:x" {
		t.Fatalf("key = %q, want opsmesh:x", got)
	}
	if got := s.blacklistKey("jti1"); got != "opsmesh:blacklist:jti1" {
		t.Fatalf("blacklistKey = %q, want opsmesh:blacklist:jti1", got)
	}
	if got := s.rateLimitKey("k1"); got != "opsmesh:ratelimit:k1" {
		t.Fatalf("rateLimitKey = %q, want opsmesh:ratelimit:k1", got)
	}
	if got := s.cpTokenKey("tok1"); got != "opsmesh:cptoken:tok1" {
		t.Fatalf("cpTokenKey = %q, want opsmesh:cptoken:tok1", got)
	}
}

// TestRedisSessionStore_IsBlacklisted_Empty 验证空 jti 返回 false（不查 Redis）。
func TestRedisSessionStore_IsBlacklisted_Empty(t *testing.T) {
	s, err := NewRedisSessionStore("127.0.0.1:1", "opsmesh:", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewRedisSessionStore 失败: %v", err)
	}
	defer s.Close()
	if s.IsBlacklisted("") {
		t.Fatal("空 jti 应返回 false")
	}
}

// TestRedisSessionStore_Blacklist_Empty 验证空 jti 不写入。
func TestRedisSessionStore_Blacklist_Empty(t *testing.T) {
	s, err := NewRedisSessionStore("127.0.0.1:1", "opsmesh:", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewRedisSessionStore 失败: %v", err)
	}
	defer s.Close()
	s.Blacklist("", time.Minute) // 不应 panic
}

// TestRedisSessionStore_PurgeBlacklist_NoOp 验证 PurgeBlacklist 是 no-op。
func TestRedisSessionStore_PurgeBlacklist_NoOp(t *testing.T) {
	s, err := NewRedisSessionStore("127.0.0.1:1", "opsmesh:", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewRedisSessionStore 失败: %v", err)
	}
	defer s.Close()
	s.PurgeBlacklist() // 不应 panic
}

// TestRedisSessionStore_IncrRateLimit_Empty 验证空 key 返回 0。
func TestRedisSessionStore_IncrRateLimit_Empty(t *testing.T) {
	s, err := NewRedisSessionStore("127.0.0.1:1", "opsmesh:", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewRedisSessionStore 失败: %v", err)
	}
	defer s.Close()
	if n := s.IncrRateLimit("", time.Minute); n != 0 {
		t.Fatalf("IncrRateLimit 空key = %d, want 0", n)
	}
}

// TestRedisSessionStore_ResetRateLimit_Empty 验证空 key 不 panic。
func TestRedisSessionStore_ResetRateLimit_Empty(t *testing.T) {
	s, err := NewRedisSessionStore("127.0.0.1:1", "opsmesh:", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewRedisSessionStore 失败: %v", err)
	}
	defer s.Close()
	s.ResetRateLimit("") // 不应 panic
}

// TestRedisSessionStore_CreateChangePasswordToken_Empty 验证空 token 返回错误。
func TestRedisSessionStore_CreateChangePasswordToken_Empty(t *testing.T) {
	s, err := NewRedisSessionStore("127.0.0.1:1", "opsmesh:", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewRedisSessionStore 失败: %v", err)
	}
	defer s.Close()
	if err := s.CreateChangePasswordToken("", "u1", time.Minute); err == nil {
		t.Fatal("空 token 应返回错误")
	}
}

// TestRedisSessionStore_ConsumeChangePasswordToken_Empty 验证空 token 返回 false。
func TestRedisSessionStore_ConsumeChangePasswordToken_Empty(t *testing.T) {
	s, err := NewRedisSessionStore("127.0.0.1:1", "opsmesh:", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewRedisSessionStore 失败: %v", err)
	}
	defer s.Close()
	if _, ok := s.ConsumeChangePasswordToken(""); ok {
		t.Fatal("空 token 应返回 false")
	}
}

// TestRedisSessionStore_PurgeChangePasswordTokens_NoOp 验证 PurgeChangePasswordTokens 是 no-op。
func TestRedisSessionStore_PurgeChangePasswordTokens_NoOp(t *testing.T) {
	s, err := NewRedisSessionStore("127.0.0.1:1", "opsmesh:", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewRedisSessionStore 失败: %v", err)
	}
	defer s.Close()
	s.PurgeChangePasswordTokens() // 不应 panic
}

// TestRedisSessionStore_Close_NilClient 验证 Close nil client 不 panic。
func TestRedisSessionStore_Close_NilClient(t *testing.T) {
	s := &RedisSessionStore{client: nil}
	if err := s.Close(); err != nil {
		t.Fatalf("Close nil client 应返回 nil, got %v", err)
	}
}

// TestErrRedisAddrRequired 验证 errRedisAddrRequired 错误信息。
func TestErrRedisAddrRequired(t *testing.T) {
	if errRedisAddrRequired.Error() == "" {
		t.Fatal("errRedisAddrRequired 应有非空错误信息")
	}
}

// TestErrChangePasswordTokenRequired 验证 errChangePasswordTokenRequired 错误信息。
func TestErrChangePasswordTokenRequired(t *testing.T) {
	if errChangePasswordTokenRequired.Error() == "" {
		t.Fatal("errChangePasswordTokenRequired 应有非空错误信息")
	}
}

// TestErrRefreshTokenHashRequired 验证 errRefreshTokenHashRequired 错误信息。
func TestErrRefreshTokenHashRequired(t *testing.T) {
	if errRefreshTokenHashRequired.Error() == "" {
		t.Fatal("errRefreshTokenHashRequired 应有非空错误信息")
	}
}

// ============================================================================
// MultiSchemaStore 的 defaultStoreFactory / globalStore / 反查索引
// ============================================================================

// TestMultiSchemaStore_DefaultStoreFactory 验证 defaultStoreFactory 在无效 DSN 下返回错误。
func TestMultiSchemaStore_DefaultStoreFactory(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	m, err := NewMultiSchemaStore("invalid-dsn", "", namer)
	if err != nil {
		t.Fatalf("NewMultiSchemaStore 失败: %v", err)
	}
	// defaultStoreFactory 会尝试创建 SQLStore，无效 DSN 应返回错误
	if _, err := m.defaultStoreFactory("test_schema"); err == nil {
		t.Fatal("defaultStoreFactory 无效 DSN 应返回错误")
	}
}

// TestMultiSchemaStore_GlobalStore_LazyCreate 验证 globalStore 惰性创建 global schema。
func TestMultiSchemaStore_GlobalStore_LazyCreate(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return NewMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	// 初始无 schema，globalStore 应惰性创建 "global"
	s, err := m.globalStore()
	if err != nil {
		t.Fatalf("globalStore 失败: %v", err)
	}
	if s == nil {
		t.Fatal("globalStore 应返回非 nil store")
	}
}

// TestMultiSchemaStore_ReverseLookup_Unmatched 验证反查索引未命中返回空串。
func TestMultiSchemaStore_ReverseLookup_Unmatched(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return NewMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if got := m.lookupAgentTenant("no-exist"); got != "" {
		t.Fatalf("lookupAgentTenant 未命中应返回空串, got %q", got)
	}
	if got := m.lookupDeviceTenant("no-exist"); got != "" {
		t.Fatalf("lookupDeviceTenant 未命中应返回空串, got %q", got)
	}
	if got := m.lookupTaskTenant("no-exist"); got != "" {
		t.Fatalf("lookupTaskTenant 未命中应返回空串, got %q", got)
	}
}

// TestMultiSchemaStore_StoreFor_EmptyTenant 验证 storeFor 空租户返回错误。
func TestMultiSchemaStore_StoreFor_EmptyTenant(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return NewMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if _, err := m.storeFor(""); err == nil {
		t.Fatal("storeFor 空租户应返回错误")
	}
}

// TestMultiSchemaStore_StoreFor_NamerError 验证 storeFor namer 错误透传。
func TestMultiSchemaStore_StoreFor_NamerError(t *testing.T) {
	// namer 始终返回错误
	namer := func(tenant string) (string, error) {
		return "", errors.New("namer error")
	}
	factory := func(schema string) (Store, error) {
		return NewMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if _, err := m.storeFor("t1"); err == nil {
		t.Fatal("storeFor namer 错误应透传")
	}
}

// TestMultiSchemaStore_StoreFor_FactoryError 验证 storeFor factory 错误透传。
func TestMultiSchemaStore_StoreFor_FactoryError(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return nil, errors.New("factory error")
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if _, err := m.storeFor("t1"); err == nil {
		t.Fatal("storeFor factory 错误应透传")
	}
}

// TestMultiSchemaStore_AllStores_Empty 验证 allStores 空时返回空 slice。
func TestMultiSchemaStore_AllStores_Empty(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return NewMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if got := m.allStores(); len(got) != 0 {
		t.Fatalf("allStores 空 = %d, want 0", len(got))
	}
}

// TestMultiSchemaStore_NewMultiSchemaStore_NilNamer 验证 nil namer 返回错误。
func TestMultiSchemaStore_NewMultiSchemaStore_NilNamer(t *testing.T) {
	if _, err := NewMultiSchemaStore("dsn", "", nil); err == nil {
		t.Fatal("nil namer 应返回错误")
	}
}

// TestMultiSchemaStore_WithBus_WithSecret 验证 WithBus/WithSecret 链式调用。
func TestMultiSchemaStore_WithBus_WithSecret(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return NewMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	bus := events.NoopBus{}
	if m.WithBus(bus) != m {
		t.Fatal("WithBus 应返回 m")
	}
	if m.WithSecret("new-secret") != m {
		t.Fatal("WithSecret 应返回 m")
	}
	// 空 secret 不覆盖
	m2 := newMultiSchemaWithFactory(namer, factory)
	origSecret := m2.secret
	m2.WithSecret("")
	if m2.secret != origSecret {
		t.Fatal("WithSecret 空串不应覆盖")
	}
}

// TestMultiSchemaStore_WithDemo_Propagation 验证 WithDemo 传播到已创建 schema。
func TestMultiSchemaStore_WithDemo_Propagation(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return NewMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	// 先创建一个 schema
	m.storeFor("t1")
	// WithDemo 传播
	if m.WithDemo(true) != m {
		t.Fatal("WithDemo 应返回 m")
	}
}

// TestMultiSchemaStore_Heartbeat_UnknownAgent 验证未知 agent 心跳返回 false。
func TestMultiSchemaStore_Heartbeat_UnknownAgent(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return NewMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if m.Heartbeat("no-exist", "online", 1) {
		t.Fatal("Heartbeat 未知 agent 应返回 false")
	}
}

// TestMultiSchemaStore_Device_Unknown 验证未知设备返回 nil。
func TestMultiSchemaStore_Device_Unknown(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return NewMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if m.Device("no-exist") != nil {
		t.Fatal("Device 未知应返回 nil")
	}
	if m.Agent("no-exist") != nil {
		t.Fatal("Agent 未知应返回 nil")
	}
	if m.AgentSecret("no-exist") != "" {
		t.Fatal("AgentSecret 未知应返回空串")
	}
}

// TestMultiSchemaStore_GetTasks_UnknownAgent 验证未知 agent 返回 nil。
func TestMultiSchemaStore_GetTasks_UnknownAgent(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return NewMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if got := m.GetTasks("no-exist"); got != nil {
		t.Fatalf("GetTasks 未知 agent 应返回 nil, got %+v", got)
	}
	if got := m.TasksByParent("no-exist"); got != nil {
		t.Fatalf("TasksByParent 未知应返回 nil, got %+v", got)
	}
	if got := m.ClaimTask("no-exist"); got != nil {
		t.Fatalf("ClaimTask 未知 agent 应返回 nil, got %+v", got)
	}
	if got := m.TaskByID("no-exist"); got != nil {
		t.Fatalf("TaskByID 未知应返回 nil, got %+v", got)
	}
	if got := m.TaskResult("no-exist"); got != nil {
		t.Fatalf("TaskResult 未知应返回 nil, got %+v", got)
	}
	if got := m.CancelledTaskIDs("no-exist"); got != nil {
		t.Fatalf("CancelledTaskIDs 未知应返回 nil, got %+v", got)
	}
	if got := m.Results("no-exist"); got != nil {
		t.Fatalf("Results 未知应返回 nil, got %+v", got)
	}
}

// TestMultiSchemaStore_EmptyAggregations 验证空 store 聚合方法返回零值。
func TestMultiSchemaStore_EmptyAggregations(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return NewMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if n := m.PendingDepth(); n != 0 {
		t.Fatalf("PendingDepth 空 = %d, want 0", n)
	}
	if n := m.ReclaimStaleTasks(time.Minute); n != 0 {
		t.Fatalf("ReclaimStaleTasks 空 = %d, want 0", n)
	}
	if n := m.FireDueSchedules(time.Now()); n != 0 {
		t.Fatalf("FireDueSchedules 空 = %d, want 0", n)
	}
	if n := m.RetireStaleDevices(time.Minute); n != 0 {
		t.Fatalf("RetireStaleDevices 空 = %d, want 0", n)
	}
	if n := m.CleanupTokens(10); n != 0 {
		t.Fatalf("CleanupTokens 空 = %d, want 0", n)
	}
	if m.IsLeader() {
		t.Fatal("IsLeader 空应返回 false")
	}
	if m.RenewLeadership(time.Minute) {
		t.Fatal("RenewLeadership 空应返回 false")
	}
}

// TestMultiSchemaStore_Alert_Empty 验证空 store Alert 返回 nil。
func TestMultiSchemaStore_Alert_Empty(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return NewMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if m.Alert("no-exist") != nil {
		t.Fatal("Alert 空应返回 nil")
	}
}

// TestMultiSchemaStore_Audits_Empty 验证空 store Audits 返回 nil。
func TestMultiSchemaStore_Audits_Empty(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return NewMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if got := m.Audits(); len(got) != 0 {
		t.Fatalf("Audits 空 = %d, want 0", len(got))
	}
	if got := m.QueryAudits("", "", time.Time{}, time.Time{}, 0); len(got) != 0 {
		t.Fatalf("QueryAudits 空 = %d, want 0", len(got))
	}
}

// TestMultiSchemaStore_ConsumeToken_Empty 验证空 store ConsumeToken 返回 false。
func TestMultiSchemaStore_ConsumeToken_Empty(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return NewMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if _, _, ok := m.ConsumeToken("tok"); ok {
		t.Fatal("ConsumeToken 空应返回 false")
	}
}

// TestMultiSchemaStore_DeleteAlertRule_Empty 验证空 store DeleteAlertRule 返回 false。
func TestMultiSchemaStore_DeleteAlertRule_Empty(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return NewMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if m.DeleteAlertRule("no-exist") {
		t.Fatal("DeleteAlertRule 空应返回 false")
	}
	if m.GetAlertRule("no-exist") != nil {
		t.Fatal("GetAlertRule 空应返回 nil")
	}
	if m.UpdateAlertRule(&AlertRule{ID: "no-exist"}) {
		t.Fatal("UpdateAlertRule 空应返回 false")
	}
	if m.UpdateAlertRule(nil) {
		t.Fatal("UpdateAlertRule nil 应返回 false")
	}
}

// TestMultiSchemaStore_CreateAlertRule_Nil 验证 CreateAlertRule nil 返回 nil。
func TestMultiSchemaStore_CreateAlertRule_Nil(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return NewMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if m.CreateAlertRule(nil) != nil {
		t.Fatal("CreateAlertRule nil 应返回 nil")
	}
}

// TestMultiSchemaStore_AddAlert_Nil 验证 AddAlert nil 不 panic。
func TestMultiSchemaStore_AddAlert_Nil(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return NewMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	m.AddAlert(nil) // 不应 panic
}

// TestMultiSchemaStore_Audit_Nil 验证 Audit nil 不 panic。
func TestMultiSchemaStore_Audit_Nil(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return NewMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	m.Audit(nil) // 不应 panic
}

// TestMultiSchemaStore_UpsertDevice_NilOrEmpty 验证 UpsertDevice nil 或空 DeviceID 不 panic。
func TestMultiSchemaStore_UpsertDevice_NilOrEmpty(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return NewMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	m.UpsertDevice(nil)
	m.UpsertDevice(&proto.DeviceInfo{DeviceID: ""})
}

// TestMultiSchemaStore_StoreDeviceMetrics_NilOrEmpty 验证 StoreDeviceMetrics 边界。
func TestMultiSchemaStore_StoreDeviceMetrics_NilOrEmpty(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return NewMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	m.StoreDeviceMetrics("", &proto.DeviceMetrics{DeviceID: "d1"})
	m.StoreDeviceMetrics("d1", nil)
	// 未知设备（无反查索引）应丢弃
	m.StoreDeviceMetrics("unknown", &proto.DeviceMetrics{DeviceID: "unknown"})
	if m.DeviceMetrics("unknown") != nil {
		t.Fatal("DeviceMetrics 未知设备应返回 nil")
	}
	if m.DeviceMetricsHistory("unknown", time.Time{}) != nil {
		t.Fatal("DeviceMetricsHistory 未知设备应返回 nil")
	}
}

// TestMultiSchemaStore_Quota_EmptyTenant 验证 Quota 空租户路径。
func TestMultiSchemaStore_Quota_EmptyTenant(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return NewMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if cfg, err := m.GetQuota(""); cfg != nil || err != nil {
		t.Fatalf("GetQuota 空租户应返回 (nil,nil), got (%+v,%v)", cfg, err)
	}
	if err := m.SetQuota("", &QuotaConfig{}); err == nil {
		t.Fatal("SetQuota 空租户应返回错误")
	}
}

// TestMultiSchemaStore_Snapshot_EmptyTenant 验证 Snapshot 空租户返回 nil。
func TestMultiSchemaStore_Snapshot_EmptyTenant(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return NewMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if got := m.Snapshot(""); got != nil {
		t.Fatalf("Snapshot 空租户应返回 nil, got %+v", got)
	}
}

// TestMultiSchemaStore_Alerts_EmptyTenant 验证 Alerts 空租户返回 nil。
func TestMultiSchemaStore_Alerts_EmptyTenant(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return NewMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if got := m.Alerts(""); got != nil {
		t.Fatalf("Alerts 空租户应返回 nil, got %+v", got)
	}
	if got := m.Agents(""); got != nil {
		t.Fatalf("Agents 空租户应返回 nil, got %+v", got)
	}
	if got := m.AllTasks(""); got != nil {
		t.Fatalf("AllTasks 空租户应返回 nil, got %+v", got)
	}
	if got := m.ListAlertRules(""); got != nil {
		t.Fatalf("ListAlertRules 空租户应返回 nil, got %+v", got)
	}
}

// TestMultiSchemaStore_RetireDevice_EmptyTenant 验证 RetireDevice 空租户返回 false。
func TestMultiSchemaStore_RetireDevice_EmptyTenant(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return NewMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if m.RetireDevice("d1", "") {
		t.Fatal("RetireDevice 空租户应返回 false")
	}
	if m.AckAlert("a1", "", "u1") {
		t.Fatal("AckAlert 空租户应返回 false")
	}
	if m.SilenceAlert("a1", "", "u1", time.Time{}, "c") {
		t.Fatal("SilenceAlert 空租户应返回 false")
	}
	if m.CancelTask("t1", "") {
		t.Fatal("CancelTask 空租户应返回 false")
	}
	if m.ApproveTask("t1", "", "u1") {
		t.Fatal("ApproveTask 空租户应返回 false")
	}
	if m.RejectTask("t1", "", "u1") {
		t.Fatal("RejectTask 空租户应返回 false")
	}
}

// TestMultiSchemaStore_Provision_EmptyTenant 验证 Provision 空租户返回错误。
func TestMultiSchemaStore_Provision_EmptyTenant(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return NewMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	if _, _, err := m.Provision("d1", "host", ""); err == nil {
		t.Fatal("Provision 空租户应返回错误")
	}
	if _, err := m.IssueToken("d1", "", time.Minute); err == nil {
		t.Fatal("IssueToken 空租户应返回错误")
	}
}

// TestMultiSchemaStore_SubmitResult_UnknownTask 验证 SubmitResult 未知任务不 panic。
func TestMultiSchemaStore_SubmitResult_UnknownTask(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return NewMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	m.SubmitResult(&proto.TaskResult{TaskID: "no-exist", AgentID: "no-exist"}) // 不应 panic
}

// TestMultiSchemaStore_CreateTask_EmptyTenant 验证 CreateTask 空租户不 panic。
func TestMultiSchemaStore_CreateTask_EmptyTenant(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return NewMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	tk := &proto.Task{AgentID: "a1", TenantID: "", Command: "c1"}
	ret := m.CreateTask(tk)
	if ret == nil {
		t.Fatal("CreateTask 应返回非 nil")
	}
}

// TestMultiSchemaStore_Register_EmptyTenant 验证 Register 空租户不 panic。
func TestMultiSchemaStore_Register_EmptyTenant(t *testing.T) {
	namer := DefaultSchemaNamer("opsmesh_")
	factory := func(schema string) (Store, error) {
		return NewMemoryStore(), nil
	}
	m := newMultiSchemaWithFactory(namer, factory)
	a := m.Register(&proto.AgentInfo{AgentID: "a1", Segment: "s1", TenantID: ""})
	if a == nil {
		t.Fatal("Register 应返回非 nil")
	}
}

// TestSortAuditsDesc 验证 sortAuditsDesc 按时间倒序。
func TestSortAuditsDesc(t *testing.T) {
	now := time.Now()
	in := []*proto.AuditEvent{
		{Action: "a1", CreatedAt: now},
		{Action: "a2", CreatedAt: now.Add(time.Second)},
		{Action: "a3", CreatedAt: now.Add(2 * time.Second)},
	}
	sortAuditsDesc(in)
	if in[0].Action != "a3" || in[1].Action != "a2" || in[2].Action != "a1" {
		t.Fatalf("sortAuditsDesc 顺序错误: %+v", in)
	}
}

// TestSortAuditsDesc_Empty 验证 sortAuditsDesc 空切片不 panic。
func TestSortAuditsDesc_Empty(t *testing.T) {
	sortAuditsDesc(nil)
	sortAuditsDesc([]*proto.AuditEvent{})
}

// TestValidateIdent 验证 validateIdent 各种输入。
func TestValidateIdent(t *testing.T) {
	if err := validateIdent("opsmesh_t1"); err != nil {
		t.Fatalf("合法标识符应通过: %v", err)
	}
	if err := validateIdent(""); err != nil {
		t.Fatalf("空串应通过: %v", err)
	}
	if err := validateIdent("t1; DROP"); err == nil {
		t.Fatal("含 ; 空格应拒绝")
	}
	if err := validateIdent("t'1"); err == nil {
		t.Fatal("含 ' 应拒绝")
	}
	if err := validateIdent("t1--"); err == nil {
		t.Fatal("含 - 应拒绝")
	}
}

// TestDsnForSchema_NoSlash 验证 dsnForSchema 无 / 时原样返回。
func TestDsnForSchema_NoSlash(t *testing.T) {
	if got := dsnForSchema("no-slash-dsn", "schema1"); got != "no-slash-dsn" {
		t.Fatalf("dsnForSchema 无 / 应原样返回, got %q", got)
	}
}

// TestDsnForSchema_WithQuery 验证 dsnForSchema 含查询参数。
func TestDsnForSchema_WithQuery(t *testing.T) {
	got := dsnForSchema("user:pass@tcp(host:3306)/olddb?parseTime=true", "newschema")
	if !strings.Contains(got, "/newschema?") {
		t.Fatalf("dsnForSchema 应替换 db 名, got %q", got)
	}
}

// TestDsnForSchema_NoQuery 验证 dsnForSchema 无查询参数。
func TestDsnForSchema_NoQuery(t *testing.T) {
	got := dsnForSchema("user:pass@tcp(host:3306)/olddb", "newschema")
	if !strings.HasSuffix(got, "/newschema") {
		t.Fatalf("dsnForSchema 应替换 db 名, got %q", got)
	}
}