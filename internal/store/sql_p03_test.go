package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// sql_p03_test.go 测试 P0.3 SQL 实现（secret/config/discovery）的扫描函数 + MySQL 集成测试。
//
// 测试分两层：
//  1. 扫描函数层（mockRowScanner）：验证 scanSecretMeta / scanConfigItem / scanServiceInstance
//     的字段映射与 JSON 列解析正确性；
//  2. MySQL 集成层（需 OPSMESH_TEST_MYSQL_DSN）：验证 CRUD + 版本历史 + 租户隔离。
//
// 扫描函数测试无需 MySQL，始终运行；集成测试在 OPSMESH_TEST_MYSQL_DSN 未设置时 t.Skip。
// mockRowScanner 已在 sql_test.go 中定义，此处直接复用。

// ============================================================================
// scanSecretMeta：密钥元信息行扫描（sql_secret.go）
// 列顺序：tenant_id, key_name, key_type, version, created_at, updated_at
// ============================================================================

func TestScanSecretMeta_Happy(t *testing.T) {
	created := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 5, 10, 12, 30, 0, 0, time.UTC)
	row := &mockRowScanner{vals: []interface{}{
		"t1", "app/db/password", "passphrase", 3, created, updated,
	}}
	m := scanSecretMeta(row)
	if m == nil {
		t.Fatal("scanSecretMeta 返回 nil")
	}
	if m.TenantID != "t1" || m.Key != "app/db/password" || m.KeyType != "passphrase" {
		t.Fatalf("基础字段映射错误: %+v", m)
	}
	if m.Version != 3 {
		t.Fatalf("Version 错误: got=%d want=3", m.Version)
	}
	if !m.CreatedAt.Equal(created) || !m.UpdatedAt.Equal(updated) {
		t.Fatalf("时间戳错误: created=%v updated=%v", m.CreatedAt, m.UpdatedAt)
	}
}

func TestScanSecretMeta_ScanError(t *testing.T) {
	row := &mockRowScanner{err: errors.New("db connection lost")}
	if m := scanSecretMeta(row); m != nil {
		t.Fatal("Scan 出错时应返回 nil")
	}
}

// ============================================================================
// scanConfigItem：配置项行扫描（sql_config.go）
// 列顺序：key_name, value, format, version, description, tenant_id, updated_by, updated_at
// ============================================================================

func TestScanConfigItem_Happy(t *testing.T) {
	updated := time.Date(2026, 6, 15, 9, 45, 0, 0, time.UTC)
	row := &mockRowScanner{vals: []interface{}{
		"app/db/pool", "size=10\nmaxIdle=5", "properties", 2,
		"DB 连接池配置", "t1", "user-1", updated,
	}}
	c := scanConfigItem(row)
	if c == nil {
		t.Fatal("scanConfigItem 返回 nil")
	}
	if c.Key != "app/db/pool" || c.Value != "size=10\nmaxIdle=5" || c.Format != "properties" {
		t.Fatalf("基础字段映射错误: %+v", c)
	}
	if c.Version != 2 || c.Description != "DB 连接池配置" {
		t.Fatalf("版本/描述错误: %+v", c)
	}
	if c.TenantID != "t1" || c.UpdatedBy != "user-1" {
		t.Fatalf("租户/更新人错误: %+v", c)
	}
	if !c.UpdatedAt.Equal(updated) {
		t.Fatalf("UpdatedAt 错误: got=%v want=%v", c.UpdatedAt, updated)
	}
}

func TestScanConfigItem_ScanError(t *testing.T) {
	row := &mockRowScanner{err: errors.New("column mismatch")}
	if c := scanConfigItem(row); c != nil {
		t.Fatal("Scan 出错时应返回 nil")
	}
}

// ============================================================================
// scanServiceInstance：服务实例行扫描（sql_discovery.go）
// 列顺序：service_id, tenant_id, service_name, address, port, metadata(JSON),
//
//	status, last_heartbeat, created_at
//
// metadata 列为 JSON 文本，反序列化为 map[string]string。
// ============================================================================

func TestScanServiceInstance_Happy(t *testing.T) {
	hb := time.Date(2026, 7, 20, 14, 0, 0, 0, time.UTC)
	created := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	metadataJSON := `{"region":"us-east","weight":"100","zone":"a"}`
	row := &mockRowScanner{vals: []interface{}{
		"svc-1", "t1", "user-api", "10.0.0.1", 8080, metadataJSON, "healthy", hb, created,
	}}
	s := scanServiceInstance(row)
	if s == nil {
		t.Fatal("scanServiceInstance 返回 nil")
	}
	if s.ServiceID != "svc-1" || s.TenantID != "t1" || s.ServiceName != "user-api" {
		t.Fatalf("基础字段映射错误: %+v", s)
	}
	if s.Address != "10.0.0.1" || s.Port != 8080 || s.Status != "healthy" {
		t.Fatalf("地址/端口/状态错误: %+v", s)
	}
	if !s.LastHeartbeat.Equal(hb) || !s.CreatedAt.Equal(created) {
		t.Fatalf("时间戳错误: hb=%v created=%v", s.LastHeartbeat, s.CreatedAt)
	}
	// 验证 Metadata JSON 反序列化
	if s.Metadata == nil {
		t.Fatal("Metadata 不应为 nil")
	}
	if s.Metadata["region"] != "us-east" || s.Metadata["weight"] != "100" || s.Metadata["zone"] != "a" {
		t.Fatalf("Metadata 解析错误: %+v", s.Metadata)
	}
}

func TestScanServiceInstance_EmptyMetadata(t *testing.T) {
	row := &mockRowScanner{vals: []interface{}{
		"svc-2", "t1", "order-api", "10.0.0.2", 9090, "", "healthy", time.Time{}, time.Time{},
	}}
	s := scanServiceInstance(row)
	if s == nil {
		t.Fatal("scanServiceInstance 返回 nil")
	}
	if s.Metadata != nil {
		t.Fatalf("空 metadata 字符串应解析为 nil；got=%+v", s.Metadata)
	}
}

func TestScanServiceInstance_InvalidMetadata(t *testing.T) {
	// 非法 JSON：反序列化失败时应保留 nil Metadata，不阻断读取。
	row := &mockRowScanner{vals: []interface{}{
		"svc-3", "t1", "pay-api", "10.0.0.3", 7070, "{invalid-json", "unhealthy",
		time.Time{}, time.Time{},
	}}
	s := scanServiceInstance(row)
	if s == nil {
		t.Fatal("scanServiceInstance 返回 nil")
	}
	if s.Metadata != nil {
		t.Fatalf("非法 JSON 应保留 nil Metadata；got=%+v", s.Metadata)
	}
	if s.ServiceID != "svc-3" || s.Status != "unhealthy" {
		t.Fatalf("非法 metadata 不应阻断其他字段读取: %+v", s)
	}
}

func TestScanServiceInstance_ScanError(t *testing.T) {
	row := &mockRowScanner{err: errors.New("db error")}
	if s := scanServiceInstance(row); s != nil {
		t.Fatal("Scan 出错时应返回 nil")
	}
}

// ============================================================================
// MySQL 集成测试（需 OPSMESH_TEST_MYSQL_DSN，默认跳过）
// ============================================================================

// clearP03Tables 清理 P0.3 三域相关表，避免历史数据干扰集成测试。
// 表由 NewSQLStore 迁移建表保证存在；清理失败视为 schema 不完整，测试终止。
func clearP03Tables(t *testing.T, s *SQLStore) {
	t.Helper()
	for _, tbl := range []string{"secrets", "configs", "config_history", "services"} {
		if _, err := s.db.ExecContext(context.Background(), "DELETE FROM "+tbl); err != nil {
			t.Fatalf("清理表 %s 失败: %v", tbl, err)
		}
	}
}

// TestSQLStore_P03Secret 验证 Secret 域 CRUD + 版本递增 + 版本历史 + 租户隔离。
//
// 运行：
//
//	OPSMESH_TEST_MYSQL_DSN="user:pass@tcp(127.0.0.1:3306)/opsmesh?parseTime=true" \
//	go test ./internal/store/ -run TestSQLStore_P03Secret -v
func TestSQLStore_P03Secret(t *testing.T) {
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPSMESH_TEST_MYSQL_DSN not set; skipping P0.3 SQL integration test")
	}
	s, err := NewSQLStore(dsn, "")
	if err != nil {
		t.Fatalf("NewSQLStore: %v", err)
	}
	defer s.db.Close()
	clearP03Tables(t, s)

	// 1. SetSecret → GetSecret → 验证值一致
	meta1 := s.SetSecret(&SecretItem{Key: "app/db/password", Value: "p@ss-1", KeyType: "passphrase"}, "tenant-A")
	if meta1 == nil {
		t.Fatal("SetSecret 返回 nil")
	}
	if meta1.Version != 1 {
		t.Fatalf("首次写入版本应为 1；got=%d", meta1.Version)
	}
	got, ok := s.GetSecret("tenant-A", "app/db/password")
	if !ok || got == nil {
		t.Fatal("GetSecret 应返回已写入的密钥")
	}
	if got.Value != "p@ss-1" || got.KeyType != "passphrase" {
		t.Fatalf("GetSecret 值不一致: %+v", got)
	}

	// 2. RotateSecret → 验证版本递增 + KeyType 保留
	meta2 := s.RotateSecret("tenant-A", "app/db/password", "p@ss-2")
	if meta2 == nil {
		t.Fatal("RotateSecret 返回 nil")
	}
	if meta2.Version != 2 {
		t.Fatalf("轮换后版本应为 2；got=%d", meta2.Version)
	}
	if meta2.KeyType != "passphrase" {
		t.Fatalf("轮换应保留 KeyType=passphrase；got=%q", meta2.KeyType)
	}
	got2, _ := s.GetSecret("tenant-A", "app/db/password")
	if got2.Value != "p@ss-2" {
		t.Fatalf("轮换后值应为 p@ss-2；got=%q", got2.Value)
	}

	// 3. ListSecrets → 验证列表
	s.SetSecret(&SecretItem{Key: "app/api/token", Value: "tok-1", KeyType: "hmac"}, "tenant-A")
	list := s.ListSecrets("tenant-A")
	if len(list) != 2 {
		t.Fatalf("ListSecrets 应返回 2 个密钥；got=%d", len(list))
	}

	// 4. SecretVersions → 验证版本历史（按 version 升序）
	versions := s.SecretVersions("tenant-A", "app/db/password")
	if len(versions) != 2 {
		t.Fatalf("SecretVersions 应返回 2 个版本；got=%d", len(versions))
	}
	if versions[0].Version != 1 || versions[1].Version != 2 {
		t.Fatalf("版本历史顺序错误: %+v", versions)
	}

	// 5. DeleteSecret → 验证删除成功
	if !s.DeleteSecret("tenant-A", "app/db/password") {
		t.Fatal("DeleteSecret 应返回 true")
	}
	if _, ok := s.GetSecret("tenant-A", "app/db/password"); ok {
		t.Fatal("删除后 GetSecret 应返回 false")
	}

	// 6. 租户隔离：tenant-A 的 secret 对 tenant-B 不可见
	s.SetSecret(&SecretItem{Key: "isolated/key", Value: "secret-A", KeyType: "passphrase"}, "tenant-A")
	if _, ok := s.GetSecret("tenant-B", "isolated/key"); ok {
		t.Fatal("tenant-A 的密钥不应对 tenant-B 可见")
	}
	if listB := s.ListSecrets("tenant-B"); len(listB) != 0 {
		t.Fatalf("tenant-B 不应看到 tenant-A 的密钥；got=%d", len(listB))
	}
}

// TestSQLStore_P03Config 验证 Config 域 CRUD + 版本递增 + 历史记录 + 租户隔离。
func TestSQLStore_P03Config(t *testing.T) {
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPSMESH_TEST_MYSQL_DSN not set; skipping P0.3 SQL integration test")
	}
	s, err := NewSQLStore(dsn, "")
	if err != nil {
		t.Fatalf("NewSQLStore: %v", err)
	}
	defer s.db.Close()
	clearP03Tables(t, s)

	// 1. SetConfig → GetConfig → 验证值一致
	c1 := s.SetConfig(&ConfigItem{
		Key: "app/db/pool", Value: "size=10", Format: "properties",
		Description: "DB 连接池", TenantID: "tenant-A", UpdatedBy: "user-1",
	})
	if c1 == nil {
		t.Fatal("SetConfig 返回 nil")
	}
	if c1.Version != 1 {
		t.Fatalf("首次写入版本应为 1；got=%d", c1.Version)
	}
	got, ok := s.GetConfig("tenant-A", "app/db/pool")
	if !ok || got == nil {
		t.Fatal("GetConfig 应返回已写入的配置")
	}
	if got.Value != "size=10" || got.Format != "properties" {
		t.Fatalf("GetConfig 值不一致: %+v", got)
	}

	// 2. SetConfig 再次 → 验证版本递增 + ConfigHistory
	c2 := s.SetConfig(&ConfigItem{
		Key: "app/db/pool", Value: "size=20", Format: "properties",
		Description: "DB 连接池扩容", TenantID: "tenant-A", UpdatedBy: "user-2",
	})
	if c2 == nil {
		t.Fatal("SetConfig 第二次返回 nil")
	}
	if c2.Version != 2 {
		t.Fatalf("二次写入版本应为 2；got=%d", c2.Version)
	}
	hist := s.ConfigHistory("tenant-A", "app/db/pool")
	if len(hist) != 1 {
		t.Fatalf("ConfigHistory 应返回 1 个历史版本；got=%d", len(hist))
	}
	if hist[0].Version != 1 || hist[0].Value != "size=10" {
		t.Fatalf("历史版本错误: %+v", hist[0])
	}

	// 3. ListConfigs → 验证列表
	s.SetConfig(&ConfigItem{
		Key: "app/log/level", Value: "info", Format: "text",
		TenantID: "tenant-A", UpdatedBy: "user-1",
	})
	list := s.ListConfigs("tenant-A")
	if len(list) != 2 {
		t.Fatalf("ListConfigs 应返回 2 个配置；got=%d", len(list))
	}

	// 4. DeleteConfig → 验证删除成功 + 历史清空
	if !s.DeleteConfig("tenant-A", "app/db/pool") {
		t.Fatal("DeleteConfig 应返回 true")
	}
	if _, ok := s.GetConfig("tenant-A", "app/db/pool"); ok {
		t.Fatal("删除后 GetConfig 应返回 false")
	}
	if hist := s.ConfigHistory("tenant-A", "app/db/pool"); len(hist) != 0 {
		t.Fatalf("删除后历史应清空；got=%d", len(hist))
	}

	// 5. 租户隔离：tenant-A 的 config 对 tenant-B 不可见
	s.SetConfig(&ConfigItem{
		Key: "isolated/cfg", Value: "v-A", Format: "text",
		TenantID: "tenant-A", UpdatedBy: "user-1",
	})
	if _, ok := s.GetConfig("tenant-B", "isolated/cfg"); ok {
		t.Fatal("tenant-A 的配置不应对 tenant-B 可见")
	}
	if listB := s.ListConfigs("tenant-B"); len(listB) != 0 {
		t.Fatalf("tenant-B 不应看到 tenant-A 的配置；got=%d", len(listB))
	}
}

// TestSQLStore_P03Discovery 验证 Discovery 域 CRUD + 心跳 + 租户隔离。
func TestSQLStore_P03Discovery(t *testing.T) {
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPSMESH_TEST_MYSQL_DSN not set; skipping P0.3 SQL integration test")
	}
	s, err := NewSQLStore(dsn, "")
	if err != nil {
		t.Fatalf("NewSQLStore: %v", err)
	}
	defer s.db.Close()
	clearP03Tables(t, s)

	// 1. RegisterService → ServiceInstances → 验证实例 + Metadata 解析
	inst1 := &ServiceInstance{
		ServiceID: "svc-1", TenantID: "tenant-A", ServiceName: "user-api",
		Address: "10.0.0.1", Port: 8080, Metadata: map[string]string{"region": "us-east"},
		Status: "healthy",
	}
	if ret := s.RegisterService(inst1); ret == nil {
		t.Fatal("RegisterService 返回 nil")
	}
	instances := s.ServiceInstances("tenant-A", "user-api")
	if len(instances) != 1 {
		t.Fatalf("ServiceInstances 应返回 1 个实例；got=%d", len(instances))
	}
	if instances[0].Address != "10.0.0.1" || instances[0].Port != 8080 {
		t.Fatalf("实例字段错误: %+v", instances[0])
	}
	if instances[0].Metadata["region"] != "us-east" {
		t.Fatalf("Metadata 解析错误: %+v", instances[0].Metadata)
	}

	// 2. HeartbeatService → 验证心跳更新成功
	if !s.HeartbeatService("tenant-A", "svc-1", "healthy") {
		t.Fatal("HeartbeatService 应返回 true")
	}

	// 3. AllServices → 验证列表（按租户隔离）
	s.RegisterService(&ServiceInstance{
		ServiceID: "svc-2", TenantID: "tenant-A", ServiceName: "order-api",
		Address: "10.0.0.2", Port: 9090, Status: "healthy",
	})
	all := s.AllServices("tenant-A")
	if len(all) != 2 {
		t.Fatalf("AllServices(tenant-A) 应返回 2 个实例；got=%d", len(all))
	}

	// 4. DeregisterService → 验证删除
	if !s.DeregisterService("tenant-A", "svc-1") {
		t.Fatal("DeregisterService 应返回 true")
	}
	if instances := s.ServiceInstances("tenant-A", "user-api"); len(instances) != 0 {
		t.Fatalf("删除后 ServiceInstances 应为空；got=%d", len(instances))
	}

	// 5. 租户隔离：tenant-A 的服务对 tenant-B 不可见
	s.RegisterService(&ServiceInstance{
		ServiceID: "svc-iso", TenantID: "tenant-A", ServiceName: "iso-api",
		Address: "10.0.0.9", Port: 7777, Status: "healthy",
	})
	if listB := s.AllServices("tenant-B"); len(listB) != 0 {
		t.Fatalf("tenant-B 不应看到 tenant-A 的服务；got=%d", len(listB))
	}
	if instances := s.ServiceInstances("tenant-B", "iso-api"); len(instances) != 0 {
		t.Fatalf("tenant-B 不应查到 tenant-A 的服务实例；got=%d", len(instances))
	}
}
