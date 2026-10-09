// failures_test.go 存储层写失败的故障注入（SQLStore 侧）。
//
// 自 internal/store/failures_test.go 拆出（TD-61 批次 3-sql）：用已关闭的 *sql.DB 让写入必然失败，
// 断言失败被 storefail 记录（记录器语义测试在 internal/store/storefail）。
package sqlstore

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/proto"
	"github.com/Levango7/OpsMesh/internal/store/storefail"
)

// closedDB 返回一个已关闭的 *sql.DB：任何写入都必然失败，且不触网。
func closedDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("mysql", "root:@tcp(127.0.0.1:1)/nonexistent")
	if err != nil {
		t.Fatalf("sql.Open 失败: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("关闭测试用 db 失败: %v", err)
	}
	return db
}

// TestUpsertDevice_WriteFailureIsRecorded 故障注入：UpsertDevice 的 SQL 写失败
// 不得再静默消失，必须出现在计数器、分类表与最近样本里。
func TestUpsertDevice_WriteFailureIsRecorded(t *testing.T) {
	storefail.ResetStoreFailures()
	s := &SQLStore{db: closedDB(t)}

	s.UpsertDevice(&proto.DeviceInfo{DeviceID: "dev-fault-1", TenantID: "t-1"})

	total, byOp := storefail.StoreFailureStats()
	if total != 1 {
		t.Fatalf("失败总数 = %d，期望 1（写失败未被记录即为本缺陷复发）", total)
	}
	if byOp["UpsertDevice"] != 1 {
		t.Fatalf("byOp[UpsertDevice] = %d，期望 1；实际 byOp=%v", byOp["UpsertDevice"], byOp)
	}

	recent := storefail.RecentStoreFailures()
	if len(recent) != 1 {
		t.Fatalf("最近样本数 = %d，期望 1", len(recent))
	}
	f := recent[0]
	if f.Op != "UpsertDevice" {
		t.Errorf("样本 Op = %q，期望 %q", f.Op, "UpsertDevice")
	}
	if f.Context != "dev-fault-1" {
		t.Errorf("样本 Context = %q，期望 %q", f.Context, "dev-fault-1")
	}
	if f.TenantID != "" {
		t.Errorf("样本 TenantID = %q，format 未显式标注 tenant 时应留空（不猜）", f.TenantID)
	}
	if !strings.Contains(f.Err, "closed") {
		t.Errorf("样本 Err = %q，期望包含底层错误（closed）", f.Err)
	}
	if f.At.IsZero() {
		t.Error("样本 At 为零值，时间戳未记录")
	}
}

// TestRetireDevice_WriteFailureIsRecorded 另一个真实吞错点：RetireDevice 的写失败。
// 该方法还返回 bool，调用方拿到 false 会走「不存在」分支，与「数据库挂了」混淆。
func TestRetireDevice_WriteFailureIsRecorded(t *testing.T) {
	storefail.ResetStoreFailures()
	s := &SQLStore{db: closedDB(t)}

	if got := s.RetireDevice("dev-fault-2", "t-1"); got {
		t.Error("RetireDevice 在写失败时返回 true，期望 false")
	}

	total, byOp := storefail.StoreFailureStats()
	if total != 1 || byOp["RetireDevice"] != 1 {
		t.Fatalf("total=%d byOp=%v，期望 total=1 且 RetireDevice=1", total, byOp)
	}
}

// TestUpsertDevice_NoFailureOnEmptyInput 空入参提前返回，不应产生任何失败样本
// （否则故障计数会被业务性跳过污染，告警失去意义）。
func TestUpsertDevice_NoFailureOnEmptyInput(t *testing.T) {
	storefail.ResetStoreFailures()
	s := &SQLStore{db: closedDB(t)}

	s.UpsertDevice(nil)
	s.UpsertDevice(&proto.DeviceInfo{DeviceID: ""})

	if total, _ := storefail.StoreFailureStats(); total != 0 {
		t.Fatalf("空入参路径产生了 %d 条失败样本，期望 0", total)
	}
}
