// store_extra4_test.go 补全 MultiSchemaStore 错误路径测试。
//
// 覆盖范围：
//   - globalStore() 失败时各用户/角色/K8s/模板/刷新令牌/静默/通知渠道/通知模板方法的错误返回
//   - storeFor() 失败时各租户隔离方法的错误返回
//
// 测试风格：白盒（package store），使用 newMultiSchemaWithFactory 注入失败工厂。
package store

import (
	"errors"
	"testing"

	"opsmesh/internal/proto"
)

// failStoreFactory 始终返回错误的 store 工厂。
func failStoreFactory(schema string) (Store, error) {
	return nil, errors.New("factory failed")
}

// newMultiSchemaWithFailFactory 创建一个使用失败工厂的 MultiSchemaStore。
func newMultiSchemaWithFailFactory() *MultiSchemaStore {
	return newMultiSchemaWithFactory(DefaultSchemaNamer("opsmesh_tenant_"), failStoreFactory)
}

// ============================================================================
// globalStore() 失败时的错误路径（用户中心 / K8s / 模板 / 刷新令牌 / 静默 / 通知）
// ============================================================================

// TestMultiSchema_GetUser_FailFactory 验证 GetUser 在工厂失败时返回 nil。
func TestMultiSchema_GetUser_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if u := m.GetUser("u1"); u != nil {
		t.Fatal("GetUser 工厂失败应返回 nil")
	}
}

// TestMultiSchema_GetUserByUsername_FailFactory 验证 GetUserByUsername 在工厂失败时返回 nil。
func TestMultiSchema_GetUserByUsername_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if u := m.GetUserByUsername("admin"); u != nil {
		t.Fatal("GetUserByUsername 工厂失败应返回 nil")
	}
}

// TestMultiSchema_ListUsers_FailFactory 验证 ListUsers 在工厂失败时返回 nil。
func TestMultiSchema_ListUsers_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if u := m.ListUsers(); u != nil {
		t.Fatal("ListUsers 工厂失败应返回 nil")
	}
}

// TestMultiSchema_CreateUser_FailFactory 验证 CreateUser 在工厂失败时返回 nil。
func TestMultiSchema_CreateUser_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if u := m.CreateUser(&User{ID: "u1"}); u != nil {
		t.Fatal("CreateUser 工厂失败应返回 nil")
	}
}

// TestMultiSchema_UpdateUser_FailFactory 验证 UpdateUser 在工厂失败时返回 false。
func TestMultiSchema_UpdateUser_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if m.UpdateUser(&User{ID: "u1"}) {
		t.Fatal("UpdateUser 工厂失败应返回 false")
	}
}

// TestMultiSchema_DeleteUser_FailFactory 验证 DeleteUser 在工厂失败时返回 false。
func TestMultiSchema_DeleteUser_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if m.DeleteUser("u1") {
		t.Fatal("DeleteUser 工厂失败应返回 false")
	}
}

// TestMultiSchema_ChangePassword_FailFactory 验证 ChangePassword 在工厂失败时返回 false。
func TestMultiSchema_ChangePassword_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if m.ChangePassword("u1", "newhash") {
		t.Fatal("ChangePassword 工厂失败应返回 false")
	}
}

// TestMultiSchema_GetRole_FailFactory 验证 GetRole 在工厂失败时返回 nil。
func TestMultiSchema_GetRole_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if r := m.GetRole("r1"); r != nil {
		t.Fatal("GetRole 工厂失败应返回 nil")
	}
}

// TestMultiSchema_ListRoles_FailFactory 验证 ListRoles 在工厂失败时返回 nil。
func TestMultiSchema_ListRoles_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if r := m.ListRoles(); r != nil {
		t.Fatal("ListRoles 工厂失败应返回 nil")
	}
}

// TestMultiSchema_CreateRole_FailFactory 验证 CreateRole 在工厂失败时返回 nil。
func TestMultiSchema_CreateRole_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if r := m.CreateRole(&Role{ID: "r1"}); r != nil {
		t.Fatal("CreateRole 工厂失败应返回 nil")
	}
}

// TestMultiSchema_UpdateRole_FailFactory 验证 UpdateRole 在工厂失败时返回 false。
func TestMultiSchema_UpdateRole_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if m.UpdateRole(&Role{ID: "r1"}) {
		t.Fatal("UpdateRole 工厂失败应返回 false")
	}
}

// TestMultiSchema_DeleteRole_FailFactory 验证 DeleteRole 在工厂失败时返回 false。
func TestMultiSchema_DeleteRole_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if m.DeleteRole("r1") {
		t.Fatal("DeleteRole 工厂失败应返回 false")
	}
}

// TestMultiSchema_ListPermissions_FailFactory 验证 ListPermissions 在工厂失败时返回 nil。
func TestMultiSchema_ListPermissions_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if p := m.ListPermissions(); p != nil {
		t.Fatal("ListPermissions 工厂失败应返回 nil")
	}
}

// TestMultiSchema_ListK8sClusters_FailFactory 验证 ListK8sClusters 在工厂失败时返回 nil。
func TestMultiSchema_ListK8sClusters_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if c := m.ListK8sClusters("t1"); c != nil {
		t.Fatal("ListK8sClusters 工厂失败应返回 nil")
	}
}

// TestMultiSchema_GetK8sCluster_FailFactory 验证 GetK8sCluster 在工厂失败时返回 nil。
func TestMultiSchema_GetK8sCluster_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if c := m.GetK8sCluster("k1"); c != nil {
		t.Fatal("GetK8sCluster 工厂失败应返回 nil")
	}
}

// TestMultiSchema_SaveK8sCluster_FailFactory 验证 SaveK8sCluster 在工厂失败时返回错误。
func TestMultiSchema_SaveK8sCluster_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if err := m.SaveK8sCluster(&K8sCluster{ID: "k1"}); err == nil {
		t.Fatal("SaveK8sCluster 工厂失败应返回错误")
	}
}

// TestMultiSchema_DeleteK8sCluster_FailFactory 验证 DeleteK8sCluster 在工厂失败时返回 false。
func TestMultiSchema_DeleteK8sCluster_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if m.DeleteK8sCluster("k1") {
		t.Fatal("DeleteK8sCluster 工厂失败应返回 false")
	}
}

// TestMultiSchema_SaveOSTemplate_FailFactory 验证 SaveOSTemplate 在工厂失败时返回错误。
func TestMultiSchema_SaveOSTemplate_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if err := m.SaveOSTemplate(&OSTemplate{ID: "os1"}); err == nil {
		t.Fatal("SaveOSTemplate 工厂失败应返回错误")
	}
}

// TestMultiSchema_ListOSTemplates_FailFactory 验证 ListOSTemplates 在工厂失败时返回 nil。
func TestMultiSchema_ListOSTemplates_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if r := m.ListOSTemplates("t1"); r != nil {
		t.Fatal("ListOSTemplates 工厂失败应返回 nil")
	}
}

// TestMultiSchema_GetOSTemplate_FailFactory 验证 GetOSTemplate 在工厂失败时返回 nil。
func TestMultiSchema_GetOSTemplate_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if r := m.GetOSTemplate("os1"); r != nil {
		t.Fatal("GetOSTemplate 工厂失败应返回 nil")
	}
}

// TestMultiSchema_DeleteOSTemplate_FailFactory 验证 DeleteOSTemplate 在工厂失败时返回 false。
func TestMultiSchema_DeleteOSTemplate_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if m.DeleteOSTemplate("os1") {
		t.Fatal("DeleteOSTemplate 工厂失败应返回 false")
	}
}

// TestMultiSchema_SaveMiddlewareTemplate_FailFactory 验证 SaveMiddlewareTemplate 在工厂失败时返回错误。
func TestMultiSchema_SaveMiddlewareTemplate_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if err := m.SaveMiddlewareTemplate(&MiddlewareTemplate{ID: "mw1"}); err == nil {
		t.Fatal("SaveMiddlewareTemplate 工厂失败应返回错误")
	}
}

// TestMultiSchema_ListMiddlewareTemplates_FailFactory 验证 ListMiddlewareTemplates 在工厂失败时返回 nil。
func TestMultiSchema_ListMiddlewareTemplates_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if r := m.ListMiddlewareTemplates("t1"); r != nil {
		t.Fatal("ListMiddlewareTemplates 工厂失败应返回 nil")
	}
}

// TestMultiSchema_GetMiddlewareTemplate_FailFactory 验证 GetMiddlewareTemplate 在工厂失败时返回 nil。
func TestMultiSchema_GetMiddlewareTemplate_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if r := m.GetMiddlewareTemplate("mw1"); r != nil {
		t.Fatal("GetMiddlewareTemplate 工厂失败应返回 nil")
	}
}

// TestMultiSchema_DeleteMiddlewareTemplate_FailFactory 验证 DeleteMiddlewareTemplate 在工厂失败时返回 false。
func TestMultiSchema_DeleteMiddlewareTemplate_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if m.DeleteMiddlewareTemplate("mw1") {
		t.Fatal("DeleteMiddlewareTemplate 工厂失败应返回 false")
	}
}

// TestMultiSchema_SaveRefreshToken_FailFactory 验证 SaveRefreshToken 在工厂失败时返回错误。
func TestMultiSchema_SaveRefreshToken_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if err := m.SaveRefreshToken(&RefreshToken{TokenHash: "h1"}); err == nil {
		t.Fatal("SaveRefreshToken 工厂失败应返回错误")
	}
}

// TestMultiSchema_GetRefreshToken_FailFactory 验证 GetRefreshToken 在工厂失败时返回 nil。
func TestMultiSchema_GetRefreshToken_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if r := m.GetRefreshToken("h1"); r != nil {
		t.Fatal("GetRefreshToken 工厂失败应返回 nil")
	}
}

// TestMultiSchema_DeleteRefreshToken_FailFactory 验证 DeleteRefreshToken 在工厂失败时返回 false。
func TestMultiSchema_DeleteRefreshToken_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if m.DeleteRefreshToken("h1") {
		t.Fatal("DeleteRefreshToken 工厂失败应返回 false")
	}
}

// TestMultiSchema_ConsumeRefreshToken_FailFactory 验证 ConsumeRefreshToken 在工厂失败时返回 nil, false。
func TestMultiSchema_ConsumeRefreshToken_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	rt, ok := m.ConsumeRefreshToken("h1")
	if rt != nil || ok {
		t.Fatal("ConsumeRefreshToken 工厂失败应返回 nil, false")
	}
}

// TestMultiSchema_CreateSilence_FailFactory 验证 CreateSilence 在工厂失败时返回 nil。
func TestMultiSchema_CreateSilence_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if r := m.CreateSilence(&SilenceRule{ID: "s1"}); r != nil {
		t.Fatal("CreateSilence 工厂失败应返回 nil")
	}
}

// TestMultiSchema_DeleteSilence_FailFactory 验证 DeleteSilence 在工厂失败时返回 false。
func TestMultiSchema_DeleteSilence_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if m.DeleteSilence("s1", "t1") {
		t.Fatal("DeleteSilence 工厂失败应返回 false")
	}
}

// TestMultiSchema_ListSilences_FailFactory 验证 ListSilences 在工厂失败时返回 nil。
func TestMultiSchema_ListSilences_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if r := m.ListSilences("t1"); r != nil {
		t.Fatal("ListSilences 工厂失败应返回 nil")
	}
}

// TestMultiSchema_GetSilence_FailFactory 验证 GetSilence 在工厂失败时返回 nil。
func TestMultiSchema_GetSilence_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if r := m.GetSilence("s1"); r != nil {
		t.Fatal("GetSilence 工厂失败应返回 nil")
	}
}

// TestMultiSchema_CreateNotifyChannel_FailFactory 验证 CreateNotifyChannel 在工厂失败时返回 nil。
func TestMultiSchema_CreateNotifyChannel_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if r := m.CreateNotifyChannel(&NotifyChannel{ID: "ch1"}); r != nil {
		t.Fatal("CreateNotifyChannel 工厂失败应返回 nil")
	}
}

// TestMultiSchema_UpdateNotifyChannel_FailFactory 验证 UpdateNotifyChannel 在工厂失败时返回 false。
func TestMultiSchema_UpdateNotifyChannel_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if m.UpdateNotifyChannel(&NotifyChannel{ID: "ch1"}) {
		t.Fatal("UpdateNotifyChannel 工厂失败应返回 false")
	}
}

// TestMultiSchema_DeleteNotifyChannel_FailFactory 验证 DeleteNotifyChannel 在工厂失败时返回 false。
func TestMultiSchema_DeleteNotifyChannel_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if m.DeleteNotifyChannel("ch1", "t1") {
		t.Fatal("DeleteNotifyChannel 工厂失败应返回 false")
	}
}

// TestMultiSchema_GetNotifyChannel_FailFactory 验证 GetNotifyChannel 在工厂失败时返回 nil。
func TestMultiSchema_GetNotifyChannel_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if r := m.GetNotifyChannel("ch1"); r != nil {
		t.Fatal("GetNotifyChannel 工厂失败应返回 nil")
	}
}

// TestMultiSchema_ListNotifyChannels_FailFactory 验证 ListNotifyChannels 在工厂失败时返回 nil。
func TestMultiSchema_ListNotifyChannels_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if r := m.ListNotifyChannels("t1"); r != nil {
		t.Fatal("ListNotifyChannels 工厂失败应返回 nil")
	}
}

// TestMultiSchema_CreateNotifyTemplate_FailFactory 验证 CreateNotifyTemplate 在工厂失败时返回 nil。
func TestMultiSchema_CreateNotifyTemplate_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if r := m.CreateNotifyTemplate(&NotifyTemplate{ID: "tpl1"}); r != nil {
		t.Fatal("CreateNotifyTemplate 工厂失败应返回 nil")
	}
}

// TestMultiSchema_UpdateNotifyTemplate_FailFactory 验证 UpdateNotifyTemplate 在工厂失败时返回 false。
func TestMultiSchema_UpdateNotifyTemplate_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if m.UpdateNotifyTemplate(&NotifyTemplate{ID: "tpl1"}) {
		t.Fatal("UpdateNotifyTemplate 工厂失败应返回 false")
	}
}

// TestMultiSchema_DeleteNotifyTemplate_FailFactory 验证 DeleteNotifyTemplate 在工厂失败时返回 false。
func TestMultiSchema_DeleteNotifyTemplate_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if m.DeleteNotifyTemplate("tpl1", "t1") {
		t.Fatal("DeleteNotifyTemplate 工厂失败应返回 false")
	}
}

// TestMultiSchema_GetNotifyTemplate_FailFactory 验证 GetNotifyTemplate 在工厂失败时返回 nil。
func TestMultiSchema_GetNotifyTemplate_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if r := m.GetNotifyTemplate("tpl1"); r != nil {
		t.Fatal("GetNotifyTemplate 工厂失败应返回 nil")
	}
}

// TestMultiSchema_ListNotifyTemplates_FailFactory 验证 ListNotifyTemplates 在工厂失败时返回 nil。
func TestMultiSchema_ListNotifyTemplates_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if r := m.ListNotifyTemplates("t1"); r != nil {
		t.Fatal("ListNotifyTemplates 工厂失败应返回 nil")
	}
}

// ============================================================================
// storeFor() 失败时的错误路径（租户隔离方法）
// ============================================================================

// TestMultiSchema_CreateTask_FailFactory 验证 CreateTask 在工厂失败时返回 t。
func TestMultiSchema_CreateTask_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	tIn := &proto.Task{TaskID: "t1", AgentID: "a1", TenantID: "t1"}
	tOut := m.CreateTask(tIn)
	if tOut != tIn {
		t.Fatal("CreateTask 工厂失败应返回原 task")
	}
}

// TestMultiSchema_SaveLogs_FailFactory 验证 SaveLogs 在工厂失败时返回错误。
func TestMultiSchema_SaveLogs_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if err := m.SaveLogs("t1", &proto.LogReport{AgentID: "a1"}); err == nil {
		t.Fatal("SaveLogs 工厂失败应返回错误")
	}
}

// TestMultiSchema_AgentLogs_FailFactory 验证 AgentLogs 在工厂失败时返回 nil。
func TestMultiSchema_AgentLogs_FailFactory(t *testing.T) {
	m := newMultiSchemaWithFailFactory()
	if r := m.AgentLogs("t1", "", ""); r != nil {
		t.Fatal("AgentLogs 工厂失败应返回 nil")
	}
}
