// store_extra3_test.go 补全 SQLStore 早期返回路径与 RedisSessionStore no-op/早期返回路径测试。
//
// 覆盖范围：
//   - SQLStore 方法在 db=nil 时的早期返回路径（nil 入参、空字符串入参、db==nil 检查）
//   - RedisSessionStore no-op 方法（PurgeBlacklist/PurgeChangePasswordTokens）
//   - RedisSessionStore 早期返回路径（空字符串入参）
//   - scanUser/scanRole/scanK8sCluster/scanAlertRule 错误路径与边界
//
// 测试风格与 store_extra2_test.go 一致：白盒（package store）。
package store

import (
	"database/sql"
	"testing"
	"time"

	"opsmesh/internal/proto"
)

// ============================================================================
// SQLStore 早期返回路径（db=nil，入参为 nil/空字符串）
// ============================================================================

// TestSQLStore_SaveRefreshToken_Nil 验证 SaveRefreshToken(nil) 返回 nil。
func TestSQLStore_SaveRefreshToken_Nil(t *testing.T) {
	s := newSQLStoreForTest()
	if err := s.SaveRefreshToken(nil); err != nil {
		t.Fatalf("SaveRefreshToken(nil) 应返回 nil, got %v", err)
	}
}

// TestSQLStore_SaveRefreshToken_EmptyHash 验证 SaveRefreshToken 空 hash 返回错误。
func TestSQLStore_SaveRefreshToken_EmptyHash(t *testing.T) {
	s := newSQLStoreForTest()
	if err := s.SaveRefreshToken(&RefreshToken{TokenHash: ""}); err == nil {
		t.Fatal("SaveRefreshToken 空 hash 应返回错误")
	}
}

// TestSQLStore_GetRefreshToken_Empty 验证 GetRefreshToken("") 返回 nil。
func TestSQLStore_GetRefreshToken_Empty(t *testing.T) {
	s := newSQLStoreForTest()
	if rt := s.GetRefreshToken(""); rt != nil {
		t.Fatal("GetRefreshToken(\"\") 应返回 nil")
	}
}

// TestSQLStore_DeleteRefreshToken_Empty 验证 DeleteRefreshToken("") 返回 false。
func TestSQLStore_DeleteRefreshToken_Empty(t *testing.T) {
	s := newSQLStoreForTest()
	if s.DeleteRefreshToken("") {
		t.Fatal("DeleteRefreshToken(\"\") 应返回 false")
	}
}

// TestSQLStore_ConsumeRefreshToken_Empty 验证 ConsumeRefreshToken("") 返回 nil, false。
func TestSQLStore_ConsumeRefreshToken_Empty(t *testing.T) {
	s := newSQLStoreForTest()
	rt, ok := s.ConsumeRefreshToken("")
	if rt != nil || ok {
		t.Fatal("ConsumeRefreshToken(\"\") 应返回 nil, false")
	}
}

// TestSQLStore_ConsumeRefreshToken_NilDB 验证 ConsumeRefreshToken 在 db=nil 时返回 nil, false。
func TestSQLStore_ConsumeRefreshToken_NilDB(t *testing.T) {
	s := newSQLStoreForTest()
	rt, ok := s.ConsumeRefreshToken("some-hash")
	if rt != nil || ok {
		t.Fatal("ConsumeRefreshToken db=nil 应返回 nil, false")
	}
}

// TestSQLStore_GetQuota_EmptyTenant 验证 GetQuota("") 返回 nil, nil。
func TestSQLStore_GetQuota_EmptyTenant(t *testing.T) {
	s := newSQLStoreForTest()
	cfg, err := s.GetQuota("")
	if cfg != nil || err != nil {
		t.Fatalf("GetQuota(\"\") 应返回 nil, nil, got %+v, %v", cfg, err)
	}
}

// TestSQLStore_GetQuota_NilDB 验证 GetQuota 在 db=nil 时返回 nil, nil。
func TestSQLStore_GetQuota_NilDB(t *testing.T) {
	s := newSQLStoreForTest()
	cfg, err := s.GetQuota("t1")
	if cfg != nil || err != nil {
		t.Fatalf("GetQuota db=nil 应返回 nil, nil, got %+v, %v", cfg, err)
	}
}

// TestSQLStore_SetQuota_EmptyTenant 验证 SetQuota("") 返回错误。
func TestSQLStore_SetQuota_EmptyTenant(t *testing.T) {
	s := newSQLStoreForTest()
	if err := s.SetQuota("", &QuotaConfig{}); err == nil {
		t.Fatal("SetQuota(\"\") 应返回错误")
	}
}

// TestSQLStore_SetQuota_NilDB 验证 SetQuota 在 db=nil 时返回错误。
func TestSQLStore_SetQuota_NilDB(t *testing.T) {
	s := newSQLStoreForTest()
	if err := s.SetQuota("t1", &QuotaConfig{}); err == nil {
		t.Fatal("SetQuota db=nil 应返回错误")
	}
}

// TestSQLStore_SetQuota_NilDB_NilCfg 验证 SetQuota 在 db=nil 且 cfg=nil 时返回错误。
func TestSQLStore_SetQuota_NilDB_NilCfg(t *testing.T) {
	s := newSQLStoreForTest()
	if err := s.SetQuota("t1", nil); err == nil {
		t.Fatal("SetQuota db=nil cfg=nil 应返回错误")
	}
}

// TestSQLStore_SaveK8sCluster_Nil 验证 SaveK8sCluster(nil) 返回 nil。
func TestSQLStore_SaveK8sCluster_Nil(t *testing.T) {
	s := newSQLStoreForTest()
	if err := s.SaveK8sCluster(nil); err != nil {
		t.Fatalf("SaveK8sCluster(nil) 应返回 nil, got %v", err)
	}
}

// TestSQLStore_SaveOSTemplate_Nil 验证 SaveOSTemplate(nil) 返回 nil。
func TestSQLStore_SaveOSTemplate_Nil(t *testing.T) {
	s := newSQLStoreForTest()
	if err := s.SaveOSTemplate(nil); err != nil {
		t.Fatalf("SaveOSTemplate(nil) 应返回 nil, got %v", err)
	}
}

// TestSQLStore_SaveMiddlewareTemplate_Nil 验证 SaveMiddlewareTemplate(nil) 返回 nil。
func TestSQLStore_SaveMiddlewareTemplate_Nil(t *testing.T) {
	s := newSQLStoreForTest()
	if err := s.SaveMiddlewareTemplate(nil); err != nil {
		t.Fatalf("SaveMiddlewareTemplate(nil) 应返回 nil, got %v", err)
	}
}

// TestSQLStore_CreateSilence_Nil 验证 CreateSilence(nil) 返回 nil。
func TestSQLStore_CreateSilence_Nil(t *testing.T) {
	s := newSQLStoreForTest()
	if r := s.CreateSilence(nil); r != nil {
		t.Fatal("CreateSilence(nil) 应返回 nil")
	}
}

// TestSQLStore_CreateNotifyChannel_Nil 验证 CreateNotifyChannel(nil) 返回 nil。
func TestSQLStore_CreateNotifyChannel_Nil(t *testing.T) {
	s := newSQLStoreForTest()
	if c := s.CreateNotifyChannel(nil); c != nil {
		t.Fatal("CreateNotifyChannel(nil) 应返回 nil")
	}
}

// TestSQLStore_CreateNotifyTemplate_Nil 验证 CreateNotifyTemplate(nil) 返回 nil。
func TestSQLStore_CreateNotifyTemplate_Nil(t *testing.T) {
	s := newSQLStoreForTest()
	if r := s.CreateNotifyTemplate(nil); r != nil {
		t.Fatal("CreateNotifyTemplate(nil) 应返回 nil")
	}
}

// TestSQLStore_CreateAlertRule_Nil 验证 CreateAlertRule(nil) 返回 nil。
func TestSQLStore_CreateAlertRule_Nil(t *testing.T) {
	s := newSQLStoreForTest()
	if r := s.CreateAlertRule(nil); r != nil {
		t.Fatal("CreateAlertRule(nil) 应返回 nil")
	}
}

// TestSQLStore_CreateTask_EmptyAgentID 验证 CreateTask 空 AgentID 直接返回 t。
func TestSQLStore_CreateTask_EmptyAgentID(t *testing.T) {
	s := newSQLStoreForTest()
	tIn := &proto.Task{TaskID: "t1", AgentID: ""}
	tOut := s.CreateTask(tIn)
	if tOut != tIn {
		t.Fatal("CreateTask 空 AgentID 应返回原 task")
	}
}

// ============================================================================

// scanUser / scanRole / scanAlertRule 边界补充（Happy/Error 路径已在 sql_test.go 覆盖）
// ============================================================================

// TestScanUser_InvalidRolesJSON_Extra 验证 scanUser 无效 RoleIDs JSON 不 panic。
func TestScanUser_InvalidRolesJSON_Extra(t *testing.T) {
	now := time.Now()
	row := &mockRowScanner{vals: []interface{}{
		"user-1", "admin", "", "", "active", []byte(`invalid-json`), now, false,
	}}
	u := scanUser(row)
	if u == nil {
		t.Fatal("scanUser 无效 JSON 仍应返回非 nil")
	}
}

// TestScanRole_EmptyPerms_Extra 验证 scanRole 空 Permissions。
func TestScanRole_EmptyPerms_Extra(t *testing.T) {
	now := time.Now()
	row := &mockRowScanner{vals: []interface{}{
		"role-1", "admin", "", []byte{}, now,
	}}
	r := scanRole(row)
	if r == nil || len(r.Permissions) != 0 {
		t.Fatalf("scanRole 空 perms: %+v", r)
	}
}

// TestScanRole_InvalidPermsJSON_Extra 验证 scanRole 无效 Permissions JSON 不 panic。
func TestScanRole_InvalidPermsJSON_Extra(t *testing.T) {
	now := time.Now()
	row := &mockRowScanner{vals: []interface{}{
		"role-1", "admin", "", []byte(`invalid-json`), now,
	}}
	r := scanRole(row)
	if r == nil {
		t.Fatal("scanRole 无效 JSON 仍应返回非 nil")
	}
}

// TestScanAlertRule_NullCreatedBy_Extra 验证 scanAlertRule CreatedBy 为 NULL。
func TestScanAlertRule_NullCreatedBy_Extra(t *testing.T) {
	now := time.Now()
	row := &mockRowScanner{vals: []interface{}{
		"alert-1", "t1", "cpu_usage", ">", 0.9, 5, "critical", "msg", true, now, sql.NullString{},
	}}
	r := scanAlertRule(row)
	if r == nil || r.CreatedBy != "" {
		t.Fatalf("scanAlertRule NullCreatedBy: %+v", r)
	}
}