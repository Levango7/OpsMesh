// store_extra5_test.go 补全 SQLStore 在 DB 不可达时的错误路径测试。
package store

import (
	"database/sql"
	"testing"
	"time"

	"opsmesh/internal/proto"
)

// newSQLStoreWithBadDB 构造一个 DB 指向不可达端口的 SQLStore。
func newSQLStoreWithBadDB() *SQLStore {
	db, _ := sql.Open("mysql", "root:@tcp(127.0.0.1:1)/test?parseTime=true&timeout=1s")
	return &SQLStore{
		db:               db,
		secret:           "test-secret",
		deviceMetrics:    make(map[string]*metricsRing),
		agentSecretCache: make(map[string]string),
	}
}

func TestSQLStore_GetUser_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if u := s.GetUser("u1"); u != nil {
		t.Fatal("GetUser DB 不可达应返回 nil")
	}
}

func TestSQLStore_GetUserByUsername_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if u := s.GetUserByUsername("admin"); u != nil {
		t.Fatal("GetUserByUsername DB 不可达应返回 nil")
	}
}

func TestSQLStore_ListUsers_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if u := s.ListUsers(); u != nil {
		t.Fatal("ListUsers DB 不可达应返回 nil")
	}
}

func TestSQLStore_CreateUser_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if u := s.CreateUser(&User{ID: "u1", Username: "test"}); u != nil {
		t.Fatal("CreateUser DB 不可达应返回 nil")
	}
}

func TestSQLStore_GetRole_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if r := s.GetRole("r1"); r != nil {
		t.Fatal("GetRole DB 不可达应返回 nil")
	}
}

func TestSQLStore_ListRoles_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if r := s.ListRoles(); r != nil {
		t.Fatal("ListRoles DB 不可达应返回 nil")
	}
}

func TestSQLStore_ListK8sClusters_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if c := s.ListK8sClusters("t1"); c != nil {
		t.Fatal("ListK8sClusters DB 不可达应返回 nil")
	}
}

func TestSQLStore_GetK8sCluster_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if c := s.GetK8sCluster("k1"); c != nil {
		t.Fatal("GetK8sCluster DB 不可达应返回 nil")
	}
}

func TestSQLStore_SaveK8sCluster_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if err := s.SaveK8sCluster(&K8sCluster{ID: "k1", TenantID: "t1"}); err == nil {
		t.Fatal("SaveK8sCluster DB 不可达应返回错误")
	}
}

func TestSQLStore_DeleteK8sCluster_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if s.DeleteK8sCluster("k1") {
		t.Fatal("DeleteK8sCluster DB 不可达应返回 false")
	}
}

func TestSQLStore_Agents_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if a := s.Agents("t1"); a != nil {
		t.Fatal("Agents DB 不可达应返回 nil")
	}
}

func TestSQLStore_Agent_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if a := s.Agent("a1"); a != nil {
		t.Fatal("Agent DB 不可达应返回 nil")
	}
}

func TestSQLStore_Device_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if d := s.Device("d1"); d != nil {
		t.Fatal("Device DB 不可达应返回 nil")
	}
}

func TestSQLStore_Alerts_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if a := s.Alerts("t1"); a != nil {
		t.Fatal("Alerts DB 不可达应返回 nil")
	}
}

func TestSQLStore_Alert_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if a := s.Alert("alert-1"); a != nil {
		t.Fatal("Alert DB 不可达应返回 nil")
	}
}

func TestSQLStore_ListAlertRules_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if r := s.ListAlertRules("t1"); r != nil {
		t.Fatal("ListAlertRules DB 不可达应返回 nil")
	}
}

func TestSQLStore_DeleteAlertRule_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if s.DeleteAlertRule("ar1") {
		t.Fatal("DeleteAlertRule DB 不可达应返回 false")
	}
}

func TestSQLStore_Audits_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if a := s.Audits(); a != nil {
		t.Fatal("Audits DB 不可达应返回 nil")
	}
}

func TestSQLStore_QueryAudits_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if a := s.QueryAudits("t1", "", time.Time{}, time.Time{}, 100); a != nil {
		t.Fatal("QueryAudits DB 不可达应返回 nil")
	}
}

func TestSQLStore_GetTasks_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if r := s.GetTasks("a1"); r != nil {
		t.Fatal("GetTasks DB 不可达应返回 nil")
	}
}

func TestSQLStore_AllTasks_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if r := s.AllTasks("t1"); r != nil {
		t.Fatal("AllTasks DB 不可达应返回 nil")
	}
}

func TestSQLStore_TaskByID_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if r := s.TaskByID("t1"); r != nil {
		t.Fatal("TaskByID DB 不可达应返回 nil")
	}
}

func TestSQLStore_Results_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if r := s.Results("a1"); r != nil {
		t.Fatal("Results DB 不可达应返回 nil")
	}
}

func TestSQLStore_ClaimTask_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if r := s.ClaimTask("a1"); r != nil {
		t.Fatal("ClaimTask DB 不可达应返回 nil")
	}
}

func TestSQLStore_PendingDepth_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if d := s.PendingDepth(); d != 0 {
		t.Fatalf("PendingDepth DB 不可达应返回 0, got %d", d)
	}
}

func TestSQLStore_ListOSTemplates_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if r := s.ListOSTemplates("t1"); r != nil {
		t.Fatal("ListOSTemplates DB 不可达应返回 nil")
	}
}

func TestSQLStore_GetOSTemplate_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if r := s.GetOSTemplate("os1"); r != nil {
		t.Fatal("GetOSTemplate DB 不可达应返回 nil")
	}
}

func TestSQLStore_SaveOSTemplate_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if err := s.SaveOSTemplate(&OSTemplate{ID: "os1", TenantID: "t1"}); err == nil {
		t.Fatal("SaveOSTemplate DB 不可达应返回错误")
	}
}

func TestSQLStore_ListMiddlewareTemplates_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if r := s.ListMiddlewareTemplates("t1"); r != nil {
		t.Fatal("ListMiddlewareTemplates DB 不可达应返回 nil")
	}
}

func TestSQLStore_GetMiddlewareTemplate_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if r := s.GetMiddlewareTemplate("mw1"); r != nil {
		t.Fatal("GetMiddlewareTemplate DB 不可达应返回 nil")
	}
}

func TestSQLStore_SaveMiddlewareTemplate_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if err := s.SaveMiddlewareTemplate(&MiddlewareTemplate{ID: "mw1", TenantID: "t1"}); err == nil {
		t.Fatal("SaveMiddlewareTemplate DB 不可达应返回错误")
	}
}

func TestSQLStore_SaveRefreshToken_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if err := s.SaveRefreshToken(&RefreshToken{TokenHash: "h1"}); err == nil {
		t.Fatal("SaveRefreshToken DB 不可达应返回错误")
	}
}

func TestSQLStore_GetRefreshToken_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if r := s.GetRefreshToken("h1"); r != nil {
		t.Fatal("GetRefreshToken DB 不可达应返回 nil")
	}
}

func TestSQLStore_DeleteRefreshToken_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if s.DeleteRefreshToken("h1") {
		t.Fatal("DeleteRefreshToken DB 不可达应返回 false")
	}
}

func TestSQLStore_ConsumeRefreshToken_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	rt, ok := s.ConsumeRefreshToken("h1")
	if rt != nil || ok {
		t.Fatal("ConsumeRefreshToken DB 不可达应返回 nil, false")
	}
}

func TestSQLStore_CreateSilence_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if r := s.CreateSilence(&SilenceRule{ID: "s1", TenantID: "t1"}); r != nil {
		t.Fatal("CreateSilence DB 不可达应返回 nil")
	}
}

func TestSQLStore_ListSilences_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if r := s.ListSilences("t1"); r != nil {
		t.Fatal("ListSilences DB 不可达应返回 nil")
	}
}

func TestSQLStore_GetSilence_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if r := s.GetSilence("s1"); r != nil {
		t.Fatal("GetSilence DB 不可达应返回 nil")
	}
}

func TestSQLStore_DeleteSilence_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if s.DeleteSilence("s1", "t1") {
		t.Fatal("DeleteSilence DB 不可达应返回 false")
	}
}

func TestSQLStore_CreateNotifyChannel_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if r := s.CreateNotifyChannel(&NotifyChannel{ID: "ch1", TenantID: "t1"}); r != nil {
		t.Fatal("CreateNotifyChannel DB 不可达应返回 nil")
	}
}

func TestSQLStore_ListNotifyChannels_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if r := s.ListNotifyChannels("t1"); r != nil {
		t.Fatal("ListNotifyChannels DB 不可达应返回 nil")
	}
}

func TestSQLStore_CreateNotifyTemplate_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if r := s.CreateNotifyTemplate(&NotifyTemplate{ID: "tpl1", TenantID: "t1"}); r != nil {
		t.Fatal("CreateNotifyTemplate DB 不可达应返回 nil")
	}
}

func TestSQLStore_ListNotifyTemplates_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if r := s.ListNotifyTemplates("t1"); r != nil {
		t.Fatal("ListNotifyTemplates DB 不可达应返回 nil")
	}
}

func TestSQLStore_Provision_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	_, _, err := s.Provision("d1", "host", "t1")
	if err == nil {
		t.Fatal("Provision DB 不可达应返回错误")
	}
}

func TestSQLStore_IssueToken_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	_, err := s.IssueToken("d1", "t1", 15*time.Minute)
	if err == nil {
		t.Fatal("IssueToken DB 不可达应返回错误")
	}
}

func TestSQLStore_ConsumeToken_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if _, _, ok := s.ConsumeToken("tok1"); ok {
		t.Fatal("ConsumeToken DB 不可达应返回 false")
	}
}

func TestSQLStore_GetQuota_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	cfg, err := s.GetQuota("t1")
	if cfg != nil || err != nil {
		t.Fatalf("GetQuota DB 不可达应返回 nil, nil, got %+v, %v", cfg, err)
	}
}

func TestSQLStore_SetQuota_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if err := s.SetQuota("t1", &QuotaConfig{}); err == nil {
		t.Fatal("SetQuota DB 不可达应返回错误")
	}
}

func TestSQLStore_CreateTask_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	tIn := &proto.Task{TaskID: "t1", AgentID: "a1", TenantID: "t1"}
	tOut := s.CreateTask(tIn)
	if tOut == nil {
		t.Fatal("CreateTask DB 不可达仍应返回 task")
	}
}

func TestSQLStore_ApproveTask_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if s.ApproveTask("t1", "t1", "admin") {
		t.Fatal("ApproveTask DB 不可达应返回 false")
	}
}

func TestSQLStore_RejectTask_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if s.RejectTask("t1", "t1", "admin") {
		t.Fatal("RejectTask DB 不可达应返回 false")
	}
}

func TestSQLStore_CancelTask_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if s.CancelTask("t1", "t1") {
		t.Fatal("CancelTask DB 不可达应返回 false")
	}
}

// ============================================================================
// 补充更多 BadDB 错误路径
// ============================================================================

func TestSQLStore_AddAlert_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	s.AddAlert(&proto.Alert{AlertID: "a1"}) // 不应 panic
}

func TestSQLStore_AckAlert_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if s.AckAlert("a1", "t1", "admin") {
		t.Fatal("AckAlert DB 不可达应返回 false")
	}
}

func TestSQLStore_SilenceAlert_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if s.SilenceAlert("a1", "t1", "admin", time.Now().Add(time.Hour), "comment") {
		t.Fatal("SilenceAlert DB 不可达应返回 false")
	}
}

func TestSQLStore_Audit_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	s.Audit(&proto.AuditEvent{Action: "test"}) // 不应 panic
}

func TestSQLStore_Register_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	a := s.Register(&proto.AgentInfo{AgentID: "a1"})
	if a == nil {
		t.Fatal("Register 应返回非 nil")
	}
}

func TestSQLStore_Heartbeat_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if s.Heartbeat("a1", "online", 0) {
		t.Fatal("Heartbeat DB 不可达应返回 false")
	}
}

func TestSQLStore_Snapshot_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if m := s.Snapshot("t1"); m != nil {
		t.Fatal("Snapshot DB 不可达应返回 nil")
	}
}

func TestSQLStore_AgentSecret_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if secret := s.AgentSecret("a1"); secret != "" {
		t.Fatal("AgentSecret DB 不可达应返回空")
	}
}

func TestSQLStore_UpsertDevice_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	s.UpsertDevice(&proto.DeviceInfo{DeviceID: "d1"}) // 不应 panic
}

func TestSQLStore_RetireDevice_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if s.RetireDevice("d1", "t1") {
		t.Fatal("RetireDevice DB 不可达应返回 false")
	}
}

func TestSQLStore_RetireStaleDevices_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if n := s.RetireStaleDevices(time.Hour); n != 0 {
		t.Fatalf("RetireStaleDevices DB 不可达应返回 0, got %d", n)
	}
}

func TestSQLStore_UpdateNotifyChannel_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if s.UpdateNotifyChannel(&NotifyChannel{ID: "ch1", TenantID: "t1"}) {
		t.Fatal("UpdateNotifyChannel DB 不可达应返回 false")
	}
}

func TestSQLStore_DeleteNotifyChannel_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if s.DeleteNotifyChannel("ch1", "t1") {
		t.Fatal("DeleteNotifyChannel DB 不可达应返回 false")
	}
}

func TestSQLStore_GetNotifyChannel_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if r := s.GetNotifyChannel("ch1"); r != nil {
		t.Fatal("GetNotifyChannel DB 不可达应返回 nil")
	}
}

func TestSQLStore_UpdateNotifyTemplate_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if s.UpdateNotifyTemplate(&NotifyTemplate{ID: "tpl1", TenantID: "t1"}) {
		t.Fatal("UpdateNotifyTemplate DB 不可达应返回 false")
	}
}

func TestSQLStore_DeleteNotifyTemplate_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if s.DeleteNotifyTemplate("tpl1", "t1") {
		t.Fatal("DeleteNotifyTemplate DB 不可达应返回 false")
	}
}

func TestSQLStore_GetNotifyTemplate_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if r := s.GetNotifyTemplate("tpl1"); r != nil {
		t.Fatal("GetNotifyTemplate DB 不可达应返回 nil")
	}
}

func TestSQLStore_GetAlertRule_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if r := s.GetAlertRule("ar1"); r != nil {
		t.Fatal("GetAlertRule DB 不可达应返回 nil")
	}
}

func TestSQLStore_UpdateAlertRule_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if s.UpdateAlertRule(&AlertRule{ID: "ar1", TenantID: "t1"}) {
		t.Fatal("UpdateAlertRule DB 不可达应返回 false")
	}
}

func TestSQLStore_UpdateUser_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if s.UpdateUser(&User{ID: "u1"}) {
		t.Fatal("UpdateUser DB 不可达应返回 false")
	}
}

func TestSQLStore_ChangePassword_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if s.ChangePassword("u1", "newhash") {
		t.Fatal("ChangePassword DB 不可达应返回 false")
	}
}

func TestSQLStore_DeleteUser_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if s.DeleteUser("u1") {
		t.Fatal("DeleteUser DB 不可达应返回 false")
	}
}

func TestSQLStore_CreateRole_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if r := s.CreateRole(&Role{ID: "r1"}); r != nil {
		t.Fatal("CreateRole DB 不可达应返回 nil")
	}
}

func TestSQLStore_UpdateRole_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if s.UpdateRole(&Role{ID: "r1"}) {
		t.Fatal("UpdateRole DB 不可达应返回 false")
	}
}

func TestSQLStore_DeleteRole_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if s.DeleteRole("r1") {
		t.Fatal("DeleteRole DB 不可达应返回 false")
	}
}

func TestSQLStore_ListPermissions_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if p := s.ListPermissions(); p != nil {
		t.Fatal("ListPermissions DB 不可达应返回 nil")
	}
}

func TestSQLStore_TasksByParent_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if r := s.TasksByParent("p1"); r != nil {
		t.Fatal("TasksByParent DB 不可达应返回 nil")
	}
}

func TestSQLStore_SubmitResult_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	s.SubmitResult(&proto.TaskResult{TaskID: "t1"}) // 不应 panic
}

func TestSQLStore_TaskResult_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if r := s.TaskResult("t1"); r != nil {
		t.Fatal("TaskResult DB 不可达应返回 nil")
	}
}

func TestSQLStore_CancelledTaskIDs_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if r := s.CancelledTaskIDs("a1"); r != nil {
		t.Fatal("CancelledTaskIDs DB 不可达应返回 nil")
	}
}

func TestSQLStore_DeleteOSTemplate_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if s.DeleteOSTemplate("os1") {
		t.Fatal("DeleteOSTemplate DB 不可达应返回 false")
	}
}

func TestSQLStore_DeleteMiddlewareTemplate_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if s.DeleteMiddlewareTemplate("mw1") {
		t.Fatal("DeleteMiddlewareTemplate DB 不可达应返回 false")
	}
}

func TestSQLStore_CleanupTokens_BadDB(t *testing.T) {
	s := newSQLStoreWithBadDB()
	if n := s.CleanupTokens(100); n != 0 {
		t.Fatalf("CleanupTokens DB 不可达应返回 0, got %d", n)
	}
}
