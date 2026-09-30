package cron

import (
	"testing"
	"time"
)

func mustMatch(t *testing.T, expr string, tm time.Time, want bool) {
	t.Helper()
	got, err := Match(expr, tm)
	if err != nil {
		t.Fatalf("Match(%q) error: %v", expr, err)
	}
	if got != want {
		t.Fatalf("Match(%q, %v) = %v, want %v", expr, tm.Format("Mon 15:04"), got, want)
	}
}

func TestMatch_Basic(t *testing.T) {
	// 2026-07-26 是周日（dow=0/7）。
	// 周 7 等价 0（TD-60 §5.11，2026-10-01 修）：此前本实现把 7 判为永不匹配，
	// 而同一条表达式在 controlplane（internal/cron）会执行——同一计划两处相反答案。
	now := time.Date(2026, 7, 26, 10, 30, 0, 0, time.UTC)
	mustMatch(t, "* * * * *", now, true)
	mustMatch(t, "30 10 26 7 0", now, true)   // 精确匹配（周字段 0=周日）
	mustMatch(t, "30 10 26 7 7", now, true)   // 周 7=周日 等价 0
	mustMatch(t, "30 10 26 7 5-7", now, true) // 区间含端点 7 也等价周日
	mustMatch(t, "31 10 26 7 *", now, false)  // 分不匹配
	mustMatch(t, "30 11 26 7 *", now, false)  // 时不匹配
	mustMatch(t, "30 10 27 7 *", now, false)  // 日不匹配
	mustMatch(t, "30 10 26 8 *", now, false)  // 月不匹配
	mustMatch(t, "30 10 26 7 1", now, false)  // 周不匹配（周一）
}

func TestMatch_Step(t *testing.T) {
	mustMatch(t, "*/15 * * * *", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), true)
	mustMatch(t, "*/15 * * * *", time.Date(2026, 1, 1, 0, 30, 0, 0, time.UTC), true)
	mustMatch(t, "*/15 * * * *", time.Date(2026, 1, 1, 0, 7, 0, 0, time.UTC), false)
	mustMatch(t, "0-30/10 * * * *", time.Date(2026, 1, 1, 0, 20, 0, 0, time.UTC), true)
	mustMatch(t, "0-30/10 * * * *", time.Date(2026, 1, 1, 0, 40, 0, 0, time.UTC), false)
}

func TestMatch_RangeEnum(t *testing.T) {
	mustMatch(t, "0 9-17 * * 1-5", time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC), true)  // 周一 10 点
	mustMatch(t, "0 9-17 * * 1-5", time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC), false) // 周日
	mustMatch(t, "0 9,12,15 * * *", time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC), true)
	mustMatch(t, "0 9,12,15 * * *", time.Date(2026, 7, 26, 13, 0, 0, 0, time.UTC), false)
}

func TestMatch_Invalid(t *testing.T) {
	// 单值越界现在必须报错（TD-60 §5.11，2026-10-01 修）：先前只静默不匹配，
	// 调用方（task-svc fire/reclaim 闭包）把 err 与「不匹配」同等跳过，
	// 于是 "60 * * * *" 这类表达式在指标面上与「确实没到点」完全同形。
	invalidExprs := []string{
		"",              // 空：5 字段检查 fail
		"* * * *",       // 4 字段：5 字段检查 fail
		"abc * * * *",   // 非数字：atoi 失败
		"*/0 * * * *",   // 步长 0：s <= 0 检查
		"*/-1 * * * *",  // 步长负：s <= 0 检查
		"60-10 * * * *", // 区间左 > 右：a > b 检查
		"60 * * * *",    // 分越界：单值 lo/hi 检查
		"0 24 * * *",    // 时越界
		"0 0 32 * *",    // 日越界
		"0 0 * 13 *",    // 月越界
		"0 0 * * 8",     // 周越界（7 合法，8 不合法）
	}
	for _, c := range invalidExprs {
		if _, err := Match(c, time.Now()); err == nil {
			t.Fatalf("Match(%q) 期望错误，但未返回", c)
		}
	}
}
