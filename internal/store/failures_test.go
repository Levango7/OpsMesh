// failures_test.go 存储层失败可观测性的回归测试。
//
// 分两层：
//  1. 故障注入（真实吞错点）：用一个已关闭的 *sql.DB 让写入必然失败，断言这些
//     错误确实被记录——这正是 P0-4 修复前不可观测的那一层。
//  2. 记录器自身的语义：操作名提取、环形缓冲上界、排序、并发、复位。
//
// 全部用例不依赖 MySQL：sql.Open 不建立连接，Close 之后任何 Exec 都直接返回
// "sql: database is closed"，因此无需 OPSMESH_TEST_MYSQL_DSN 也能稳定复现写失败。
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Levango7/OpsMesh/internal/proto"
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
	ResetStoreFailures()
	s := &SQLStore{db: closedDB(t)}

	s.UpsertDevice(&proto.DeviceInfo{DeviceID: "dev-fault-1", TenantID: "t-1"})

	total, byOp := StoreFailureStats()
	if total != 1 {
		t.Fatalf("失败总数 = %d，期望 1（写失败未被记录即为本缺陷复发）", total)
	}
	if byOp["UpsertDevice"] != 1 {
		t.Fatalf("byOp[UpsertDevice] = %d，期望 1；实际 byOp=%v", byOp["UpsertDevice"], byOp)
	}

	recent := RecentStoreFailures()
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
	ResetStoreFailures()
	s := &SQLStore{db: closedDB(t)}

	if got := s.RetireDevice("dev-fault-2", "t-1"); got {
		t.Error("RetireDevice 在写失败时返回 true，期望 false")
	}

	total, byOp := StoreFailureStats()
	if total != 1 || byOp["RetireDevice"] != 1 {
		t.Fatalf("total=%d byOp=%v，期望 total=1 且 RetireDevice=1", total, byOp)
	}
}

// TestUpsertDevice_NoFailureOnEmptyInput 空入参提前返回，不应产生任何失败样本
// （否则故障计数会被业务性跳过污染，告警失去意义）。
func TestUpsertDevice_NoFailureOnEmptyInput(t *testing.T) {
	ResetStoreFailures()
	s := &SQLStore{db: closedDB(t)}

	s.UpsertDevice(nil)
	s.UpsertDevice(&proto.DeviceInfo{DeviceID: ""})

	if total, _ := StoreFailureStats(); total != 0 {
		t.Fatalf("空入参路径产生了 %d 条失败样本，期望 0", total)
	}
}

// TestExtractOpFromFormat 操作名提取：供指标按操作聚合，解析不出时归 unknown
// 而不是让指标串成一个巨型桶。

// TestRecentStoreFailures_RingBufferBounded 环形缓冲必须内存有界：写满后
// 覆盖最旧的样本，且最新的排在最前（人工排障先看最近发生的那条）。
func TestRecentStoreFailures_RingBufferBounded(t *testing.T) {
	ResetStoreFailures()

	const extra = 10
	for i := 0; i < failureRingSize+extra; i++ {
		recordStoreFailure("[store] UpsertDevice 失败 %s: %v", fmt.Sprintf("e%03d", i), errors.New("boom"))
	}

	recent := RecentStoreFailures()
	if len(recent) != failureRingSize {
		t.Fatalf("样本数 = %d，期望被上界截断为 %d", len(recent), failureRingSize)
	}
	if want := fmt.Sprintf("e%03d", failureRingSize+extra-1); recent[0].Context != want {
		t.Errorf("最新样本 Context = %q，期望 %q（时间倒序失效）", recent[0].Context, want)
	}
	if want := fmt.Sprintf("e%03d", extra); recent[len(recent)-1].Context != want {
		t.Errorf("最旧保留样本 Context = %q，期望 %q（应丢弃最早的 10 条）", recent[len(recent)-1].Context, want)
	}
	if total, _ := StoreFailureStats(); total != failureRingSize+extra {
		t.Errorf("累计总数 = %d，期望 %d（计数不应随环形缓冲覆盖而丢失）", total, failureRingSize+extra)
	}
}

// TestTopStoreFailureOps_SortedAndLimited 排序稳定：次数降序，同次数按名称升序。
func TestTopStoreFailureOps_SortedAndLimited(t *testing.T) {
	ResetStoreFailures()

	recordStoreFailure("[store] Aaa 失败: %v", errors.New("e")) // 2 次
	recordStoreFailure("[store] Aaa 失败: %v", errors.New("e"))
	recordStoreFailure("[store] Bbb 失败: %v", errors.New("e")) // 1 次
	recordStoreFailure("[store] Ccc 失败: %v", errors.New("e")) // 1 次
	recordStoreFailure("[store] Ddd 失败: %v", errors.New("e")) // 1 次

	top := TopStoreFailureOps(0)
	if len(top) != 4 || top[0].Op != "Aaa" || top[0].Count != 2 {
		t.Fatalf("TopStoreFailureOps(0) = %+v，期望首位 Aaa/2", top)
	}
	if got := []string{top[1].Op, top[2].Op, top[3].Op}; got[0] != "Bbb" || got[1] != "Ccc" || got[2] != "Ddd" {
		t.Errorf("同次数未按名称升序: %v", got)
	}

	if lim := TopStoreFailureOps(2); len(lim) != 2 {
		t.Errorf("TopStoreFailureOps(2) 长度 = %d，期望 2", len(lim))
	}
}

// TestStoreFailure_ConcurrentRecord 并发安全：生产上多个 HTTP handler 会在同一
// 时刻触发写失败，竞态由 -race 断言。
func TestStoreFailure_ConcurrentRecord(t *testing.T) {
	ResetStoreFailures()

	const workers, per = 8, 50
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < per; j++ {
				recordStoreFailure("[store] UpsertDevice 失败 %s: %v", "dev", errors.New("boom"))
				_, _ = StoreFailureStats()
				_ = RecentStoreFailures()
				_ = TopStoreFailureOps(5)
			}
		}()
	}
	wg.Wait()

	if total, byOp := StoreFailureStats(); total != workers*per || byOp["UpsertDevice"] != workers*per {
		t.Fatalf("total=%d byOp=%v，期望均为 %d（存在丢计数）", total, byOp, workers*per)
	}
}

// TestVerbSpans 动词下标必须与实参一一对应。数错下标会让租户号取成别的字段，
// 而错误的租户号比没有租户号更糟（排障时会查错租户的数据）。

// TestTenantVerbIndex 租户实参下标只由 format 里明写的标签决定，不做语义猜测。

// TestRecordStoreFailure_RealTenantCallSite 用真实调用点形态验证三件事同时成立：
// 操作名、租户号、上下文都取对。这是最容易「看起来对、其实取错」的一类断言。
func TestRecordStoreFailure_RealTenantCallSite(t *testing.T) {
	ResetStoreFailures()

	// 与 sql_apikeys.go 等处的既有形态一致。
	recordStoreFailure("[store] CreateAPIKey 插入失败 (tenant=%s apikey=%s): %v",
		"t-42", "ak-abc", errors.New("Duplicate entry"))
	f := RecentStoreFailures()[0]
	if f.Op != "CreateAPIKey" {
		t.Errorf("Op = %q，期望 %q", f.Op, "CreateAPIKey")
	}
	if f.TenantID != "t-42" {
		t.Errorf("TenantID = %q，期望 t-42", f.TenantID)
	}
	if f.Context != "t-42 ak-abc" {
		t.Errorf("Context = %q，期望 %q", f.Context, "t-42 ak-abc")
	}
	if f.Err != "Duplicate entry" {
		t.Errorf("Err = %q，期望 %q", f.Err, "Duplicate entry")
	}

	// 顺序反过来的 format 必须跟着反——否则会把实体 ID 当成租户号返回给排障的人。
	ResetStoreFailures()
	recordStoreFailure("[store] DeleteApp 失败 (id=%s tenant=%s): %v",
		"app-7", "t-42", errors.New("no such row"))
	f = RecentStoreFailures()[0]
	if f.TenantID != "t-42" {
		t.Errorf("反序调用点 TenantID = %q，期望 t-42（动词下标映射失效）", f.TenantID)
	}
	if f.Context != "app-7 t-42" {
		t.Errorf("Context = %q，期望 %q", f.Context, "app-7 t-42")
	}
}

// TestResetStoreFailures 清空后不应残留样本（用例隔离 + 运维手工重置的语义）。
func TestResetStoreFailures(t *testing.T) {
	recordStoreFailure("[store] Zzz 失败: %v", errors.New("e"))
	ResetStoreFailures()

	if total, byOp := StoreFailureStats(); total != 0 || len(byOp) != 0 {
		t.Errorf("复位后 total=%d byOp=%v，期望全空", total, byOp)
	}
	if recent := RecentStoreFailures(); len(recent) != 0 {
		t.Errorf("复位后仍有 %d 条样本，期望 0", len(recent))
	}
	if top := TopStoreFailureOps(5); len(top) != 0 {
		t.Errorf("复位后 TopStoreFailureOps 仍有 %d 项，期望 0", len(top))
	}
}

// TestStoreFailureStats_ReturnsCopy 调用方拿到的是副本，改它不应污染全局计数
// （否则管理端点一次序列化就能把线上计数改没）。
func TestStoreFailureStats_ReturnsCopy(t *testing.T) {
	ResetStoreFailures()
	recordStoreFailure("[store] Aaa 失败: %v", errors.New("e"))

	_, byOp := StoreFailureStats()
	byOp["Aaa"] = 9999
	byOp["注入"] = 1

	_, again := StoreFailureStats()
	if again["Aaa"] != 1 || len(again) != 1 {
		t.Errorf("全局计数被调用方修改污染: %v", again)
	}
}

// TestRecordStoreFailure_NonErrorTrailingArg 末位参数不是 error 时（例如旧调用点
// 传的是字符串）仍要留下可读的错误文本，不能记成空字符串。
func TestRecordStoreFailure_NonErrorTrailingArg(t *testing.T) {
	ResetStoreFailures()

	recordStoreFailure("[store] Foo 失败 %s: %s", "dev-1", "原因就是字符串")
	recent := RecentStoreFailures()
	if len(recent) != 1 {
		t.Fatalf("样本数 = %d，期望 1", len(recent))
	}
	if recent[0].Err != "原因就是字符串" {
		t.Errorf("Err = %q，期望 %q", recent[0].Err, "原因就是字符串")
	}
}
