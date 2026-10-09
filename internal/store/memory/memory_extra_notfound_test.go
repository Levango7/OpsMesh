// memory_extra_notfound_test.go MemoryStore 的边界/NotFound 用例（TD-61 末批：自父包
// store_extra_test.go 前半段回流——它们测的是内存后端实现行为，与 memory 子包内既有用例同族）。
package memory

import (
	"strings"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store/model"
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

// TestRandUserID_Default 验证 randUserID 返回带前缀的 ID。
func TestRandUserID_Default(t *testing.T) {
	id := model.RandUserID()
	if !strings.HasPrefix(id, "user-") {
		t.Fatalf("randUserID = %q, want prefix user-", id)
	}
}

// TestRandRoleID_Default 验证 randRoleID 返回带前缀的 ID。
func TestRandRoleID_Default(t *testing.T) {
	id := model.RandRoleID()
	if !strings.HasPrefix(id, "role-") {
		t.Fatalf("randRoleID = %q, want prefix role-", id)
	}
}

// TestRandK8sClusterID_Default 验证 randK8sClusterID 返回带前缀的 ID。
func TestRandK8sClusterID_Default(t *testing.T) {
	id := model.RandK8sClusterID()
	if !strings.HasPrefix(id, "k8s-cluster-") {
		t.Fatalf("randK8sClusterID = %q, want prefix k8s-cluster-", id)
	}
}

// TestRandSilenceID_Default 验证 randSilenceID 返回带前缀的 ID。
func TestRandSilenceID_Default(t *testing.T) {
	id := model.RandSilenceID()
	if !strings.HasPrefix(id, "silence-") {
		t.Fatalf("randSilenceID = %q, want prefix silence-", id)
	}
}

// TestRandNotifyChannelID_Default 验证 randNotifyChannelID 返回带前缀的 ID。
func TestRandNotifyChannelID_Default(t *testing.T) {
	id := model.RandNotifyChannelID()
	if !strings.HasPrefix(id, "ch-") {
		t.Fatalf("randNotifyChannelID = %q, want prefix ch-", id)
	}
}

// TestRandNotifyTemplateID_Default 验证 randNotifyTemplateID 返回带前缀的 ID。
func TestRandNotifyTemplateID_Default(t *testing.T) {
	id := model.RandNotifyTemplateID()
	if !strings.HasPrefix(id, "tpl-") {
		t.Fatalf("randNotifyTemplateID = %q, want prefix tpl-", id)
	}
}

// TestRandOSTemplateID_Default 验证 randOSTemplateID 返回带前缀的 ID。
func TestRandOSTemplateID_Default(t *testing.T) {
	id := model.RandOSTemplateID()
	if !strings.HasPrefix(id, "os-tmpl-") {
		t.Fatalf("randOSTemplateID = %q, want prefix os-tmpl-", id)
	}
}

// TestRandMiddlewareTemplateID_Default 验证 randMiddlewareTemplateID 返回带前缀的 ID。
func TestRandMiddlewareTemplateID_Default(t *testing.T) {
	id := model.RandMiddlewareTemplateID()
	if !strings.HasPrefix(id, "mw-tmpl-") {
		t.Fatalf("randMiddlewareTemplateID = %q, want prefix mw-tmpl-", id)
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
