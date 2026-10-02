// sql_alerts_null_test.go 守 alerts 表"全列可空 ⇒ 读侧必须逐列 Null*"这条接线。
//
// 缺陷实况（2026-10-02，v0.11.0 真机 MySQL）：migrations/001_initial.sql:134-148 里
// alerts 没有一列带 NOT NULL，写入侧又统一走 nullString()/nullTime()（零值 ⇒ NULL），
// 而 Alerts()/Alert() 的读侧把 silenced_until、updated_at 直接 Scan 进 time.Time。
// NULL 进值类型会让**整行** Scan 失败，而这两个函数对失败分别是 `continue` / `return nil`
// ——于是"从未被静默过的告警"（也就是绝大多数）在接口上凭空消失：
// 控制面日志 1844 次 `[store] Alerts 扫描失败`，`opsmesh_alerts_active` 恒为 0。
//
// 需要真实 MySQL：OPSMESH_TEST_MYSQL_DSN（CI 的 services job 提供 root DSN）。
package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/internal/proto"
)

func TestAlertsReadBackNullColumns(t *testing.T) {
	dsn := os.Getenv("OPSMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("OPSMESH_TEST_MYSQL_DSN 未设置：跳过 alerts 可空列的 MySQL 回归")
	}
	admin := stripDBName(dsn)
	dbName := fmt.Sprintf("test_alerts_null_%d", time.Now().UnixNano())
	adb, err := sql.Open("mysql", admin)
	if err != nil {
		t.Fatalf("open admin dsn: %v", err)
	}
	if _, err := adb.Exec("CREATE DATABASE " + dbName); err != nil {
		adb.Close()
		t.Fatalf("create temp db %s: %v", dbName, err)
	}
	// 连接必须在 Cleanup 里用，所以不 defer Close：t.Cleanup 的时序晚于同函数内的 defer，
	// 先 defer Close 会让删库那步拿到 "sql: database is closed"（本机实测）。
	t.Cleanup(func() {
		if _, err := adb.Exec("DROP DATABASE " + dbName); err != nil {
			t.Errorf("清理临时库 %s 失败: %v", dbName, err)
		}
		adb.Close()
	})

	s, err := NewSQLStore(withDBName(dsn, dbName), "", "")
	if err != nil {
		t.Fatalf("NewSQLStore: %v", err)
	}

	beforeBy := storeFailureByOp()
	// 一条"正常形态"的告警：没人确认过、没被静默过 ⇒ acknowledged_by / silenced_until /
	// comment / updated_at / tenant_id 全为 NULL。正是这行在旧代码里读不回来。
	id := "alert-null-roundtrip-1"
	s.addAlert(context.Background(), &proto.Alert{
		AlertID:  id,
		Severity: "critical",
		Message:  "磁盘水位 92%",
		Status:   proto.AlertStatusFiring,
	})

	list := s.Alerts("")
	var found *proto.Alert
	for _, a := range list {
		if a.AlertID == id {
			found = a
		}
	}
	if found == nil {
		t.Fatalf("Alerts(\"\") 只返回 %d 条且不含新写入的 %s —— NULL 列让整行 Scan 失败并被 continue 吞掉",
			len(list), id)
	}
	if found.CreatedAt.IsZero() {
		t.Errorf("created_at 读回为零值（addAlert 应补 time.Now()，读回却拿不到）")
	}
	if found.Severity != "critical" || found.Message != "磁盘水位 92%" {
		t.Errorf("字段读回错位：severity=%q message=%q", found.Severity, found.Message)
	}
	if !found.SilencedUntil.IsZero() {
		t.Errorf("silenced_until 本应是 NULL（未静默），读回 %v", found.SilencedUntil)
	}

	// 单条读回：旧代码在这里返回 nil，症状是"确认/静默点了没反应"。
	if one := s.Alert(id); one == nil {
		t.Fatalf("Alert(%q) 返回 nil：可空列读回失败会被记成吞错并静默返回空", id)
	}

	// 吞错增量：把"数据没了但也没报错"这一形态直接钉住（只盯本函数的两个操作，
	// 不受同进程其它用例的历史失败干扰）。
	afterBy := storeFailureByOp()
	for _, op := range []string{"Alerts", "Alert"} {
		if got := afterBy[op] - beforeBy[op]; got != 0 {
			t.Errorf("读回过程中 %s 新增了 %d 次存储吞错", op, got)
		}
	}
}

// storeFailureByOp 取当前按操作聚合的吞错计数快照。
func storeFailureByOp() map[string]uint64 {
	_, byOp := StoreFailureStats()
	return byOp
}
