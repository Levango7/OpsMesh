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
	// 2026-07-26 是周日（dow=0/7）
	now := time.Date(2026, 7, 26, 10, 30, 0, 0, time.UTC)
	mustMatch(t, "* * * * *", now, true)
	mustMatch(t, "30 10 26 7 0", now, true)  // 精确匹配
	mustMatch(t, "30 10 26 7 7", now, true)  // 周 7=周日 等价 0
	mustMatch(t, "31 10 26 7 *", now, false) // 分不匹配
	mustMatch(t, "30 11 26 7 *", now, false) // 时不匹配
	mustMatch(t, "30 10 27 7 *", now, false) // 日不匹配
	mustMatch(t, "30 10 26 8 *", now, false) // 月不匹配
	mustMatch(t, "30 10 26 7 1", now, false) // 周不匹配（周一）
}

func TestMatch_Step(t *testing.T) {
	// */15 分：0,15,30,45 命中
	mustMatch(t, "*/15 * * * *", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), true)
	mustMatch(t, "*/15 * * * *", time.Date(2026, 1, 1, 0, 30, 0, 0, time.UTC), true)
	mustMatch(t, "*/15 * * * *", time.Date(2026, 1, 1, 0, 7, 0, 0, time.UTC), false)
	// 0-30/10 分：0,10,20,30 命中
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
	cases := []string{"* * *", "* * * *", "a b c d e", "*/0 * * * *", "60 * * * *", "0 24 * * *"}
	for _, c := range cases {
		if _, err := Match(c, time.Now()); err == nil {
			t.Fatalf("Match(%q) 期望错误，但未返回", c)
		}
	}
}
