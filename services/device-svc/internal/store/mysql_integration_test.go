package store

import (
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/Levango7/OpsMesh/services/device-svc/internal/models"
)

// 真实 MySQL 集成冒烟（对齐 internal/store 的既有约定）。
//
//	OPSMESH_TEST_MYSQL_DSN="opsmesh:pass@tcp(127.0.0.1:3306)/opsmesh_device?parseTime=true" \
//	  go test ./internal/store/ -run TestMySQLStoreIntegration -v
//
// 未设置 DSN 时 t.Skip 并打印原因（不静默跳过）。
//
// 覆盖面：NewMySQLStore 建表 → devices 全字段写入/读取/列表/软删。
// 其中 retired 列曾缺失于 schema.sql，任何 devices 查询都报
// "Unknown column 'retired'"，故 RoundTrip 必须走通才算通过。
func TestMySQLStoreIntegration(t *testing.T) {
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("SKIP reason=missing OPSMESH_TEST_MYSQL_DSN; 真实 MySQL 集成冒烟需设置该环境变量")
	}

	s, err := NewMySQLStore(dsn)
	if err != nil {
		t.Fatalf("NewMySQLStore: %v", err)
	}
	defer func() { _ = s.Close() }()

	// 1) 建表：5 张表齐备（此前一张都不建）。
	assertTablesExist(t, s.db)

	// 2) devices 往返：写入 → 按 id 读 → 列表可见。
	id := "itest-dev-" + time.Now().Format("20060102150405.000000")
	now := time.Now().Truncate(time.Second)
	dev := &models.Device{
		ID: id, TenantID: "itest-tenant", Name: "集成测试设备",
		IP: "10.99.0.1", MAC: "00:11:22:33:44:55", OS: "linux", Arch: "amd64",
		Status: "online", AgentID: "itest-agent",
		Tags: []string{"itest"}, Labels: map[string]string{"env": "itest"},
		Group: "itest-group", CreatedAt: now, UpdatedAt: now,
	}
	if got := s.RegisterDevice(dev); got == nil {
		t.Fatal("RegisterDevice 返回 nil")
	}
	got := s.Device(id)
	if got == nil {
		t.Fatal("Device(id) 返回 nil（写入后读不到）")
	}
	if got.Retired {
		t.Error("新建设备不应是 retired")
	}
	if got.Name != dev.Name || got.IP != dev.IP || got.Group != dev.Group {
		t.Errorf("字段往返不一致: name=%q ip=%q group=%q", got.Name, got.IP, got.Group)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "itest" {
		t.Errorf("Tags 往返不一致: %v", got.Tags)
	}
	if got.Labels["env"] != "itest" {
		t.Errorf("Labels 往返不一致: %v", got.Labels)
	}

	listed := s.ListDevices("itest-tenant", "", "", 50)
	found := false
	for _, d := range listed {
		if d.ID == id {
			found = true
			break
		}
	}
	if !found {
		t.Error("ListDevices 未返回刚写入的设备")
	}

	// 3) 软删（retired=true）后应从活跃清单消失，但按 id 仍可查。
	if !s.DeleteDevice(id) {
		t.Fatal("DeleteDevice 返回 false")
	}
	for _, d := range s.ListDevices("itest-tenant", "", "", 50) {
		if d.ID == id {
			t.Error("已 retired 的设备仍出现在活跃清单")
		}
	}
	if after := s.Device(id); after == nil || !after.Retired {
		t.Errorf("退役后按 id 查询应 retired=true, got=%+v", after)
	}

	cleanupDevice(t, s.db, id)
}

// assertTablesExist 校验 schema.sql 声明的 5 张表均已建出。
func assertTablesExist(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, table := range []string{"devices", "agents", "ci_items", "ci_relations", "discovery_jobs"} {
		var n int
		err := db.QueryRow(
			"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name=?",
			table,
		).Scan(&n)
		if err != nil {
			t.Fatalf("查询 %s 是否存在失败: %v", table, err)
		}
		if n == 0 {
			t.Errorf("表 %s 未被创建（迁移未执行或 schema.sql 缺该表）", table)
		}
	}
}

// cleanupDevice 删除测试数据，避免污染共享测试库。
func cleanupDevice(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	if _, err := db.Exec("DELETE FROM devices WHERE id = ?", id); err != nil {
		t.Logf("清理测试设备 %s 失败（不影响结论）: %v", id, err)
	}
}
