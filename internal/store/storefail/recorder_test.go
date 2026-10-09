// recorder_test.go 记录器自身语义（环形缓冲上界、排序、并发、复位、副本、非 error 末参）。
//
// 自 internal/store/failures_test.go 迁入（TD-61 批次 3）：被测面就是本包。
package storefail

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

// TestRecentStoreFailures_RingBufferBounded 环形缓冲必须内存有界：写满后
// 覆盖最旧的样本，且最新的排在最前（人工排障先看最近发生的那条）。
func TestRecentStoreFailures_RingBufferBounded(t *testing.T) {
	ResetStoreFailures()

	const extra = 10
	for i := 0; i < FailureRingSize+extra; i++ {
		Record("[store] UpsertDevice 失败 %s: %v", fmt.Sprintf("e%03d", i), errors.New("boom"))
	}

	recent := RecentStoreFailures()
	if len(recent) != FailureRingSize {
		t.Fatalf("样本数 = %d，期望被上界截断为 %d", len(recent), FailureRingSize)
	}
	if want := fmt.Sprintf("e%03d", FailureRingSize+extra-1); recent[0].Context != want {
		t.Errorf("最新样本 Context = %q，期望 %q（时间倒序失效）", recent[0].Context, want)
	}
	if want := fmt.Sprintf("e%03d", extra); recent[len(recent)-1].Context != want {
		t.Errorf("最旧保留样本 Context = %q，期望 %q（应丢弃最早的 10 条）", recent[len(recent)-1].Context, want)
	}
	if total, _ := StoreFailureStats(); total != FailureRingSize+extra {
		t.Errorf("累计总数 = %d，期望 %d（计数不应随环形缓冲覆盖而丢失）", total, FailureRingSize+extra)
	}
}

// TestTopStoreFailureOps_SortedAndLimited 排序稳定：次数降序，同次数按名称升序。
func TestTopStoreFailureOps_SortedAndLimited(t *testing.T) {
	ResetStoreFailures()

	Record("[store] Aaa 失败: %v", errors.New("e")) // 2 次
	Record("[store] Aaa 失败: %v", errors.New("e"))
	Record("[store] Bbb 失败: %v", errors.New("e")) // 1 次
	Record("[store] Ccc 失败: %v", errors.New("e")) // 1 次
	Record("[store] Ddd 失败: %v", errors.New("e")) // 1 次

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
				Record("[store] UpsertDevice 失败 %s: %v", "dev", errors.New("boom"))
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

// TestTenantVerbIndex 租户实参下标只由 format 里明写的标签决定，不做语义猜测。

// TestRecordStoreFailure_RealTenantCallSite 用真实调用点形态验证三件事同时成立：
// 操作名、租户号、上下文都取对。这是最容易「看起来对、其实取错」的一类断言。
func TestRecordStoreFailure_RealTenantCallSite(t *testing.T) {
	ResetStoreFailures()

	// 与 sql_apikeys.go 等处的既有形态一致。
	Record("[store] CreateAPIKey 插入失败 (tenant=%s apikey=%s): %v",
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
	Record("[store] DeleteApp 失败 (id=%s tenant=%s): %v",
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
	Record("[store] Zzz 失败: %v", errors.New("e"))
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
	Record("[store] Aaa 失败: %v", errors.New("e"))

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

	Record("[store] Foo 失败 %s: %s", "dev-1", "原因就是字符串")
	recent := RecentStoreFailures()
	if len(recent) != 1 {
		t.Fatalf("样本数 = %d，期望 1", len(recent))
	}
	if recent[0].Err != "原因就是字符串" {
		t.Errorf("Err = %q，期望 %q", recent[0].Err, "原因就是字符串")
	}
}
