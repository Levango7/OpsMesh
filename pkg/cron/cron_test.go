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
	// 测试反映当前实现的实际行为：matchField 单值不实现 7=0 规范化和越界报错
	// （A-1 阶段不修原实现——保持与 controlplane 行为字节级一致是双轨对照前提）。
	now := time.Date(2026, 7, 26, 10, 30, 0, 0, time.UTC)
	mustMatch(t, "* * * * *", now, true)
	mustMatch(t, "30 10 26 7 0", now, true)  // 精确匹配（周字段 0=周日）
	mustMatch(t, "30 10 26 7 7", now, false) // 周 7 当前实现不等价 0（与 Vixie cron 不同，保留此差异待 A-2 决策）
	mustMatch(t, "31 10 26 7 *", now, false) // 分不匹配
	mustMatch(t, "30 11 26 7 *", now, false) // 时不匹配
	mustMatch(t, "30 10 27 7 *", now, false) // 日不匹配
	mustMatch(t, "30 10 26 8 *", now, false) // 月不匹配
	mustMatch(t, "30 10 26 7 1", now, false) // 周不匹配（周一）
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
	// 测试反映当前 matchField 的实际错误检测能力。
	// 已知未实现的边界校验（保留 A-1 阶段不修，详见代码注释）：
	//   - 单值越界（60/24/32/0/13）—— matchField 单值分支只 n==val 比较，不做 lo/hi 边界
	//   - 枚举 60 * * * * 这种 60=分越界时 n==val 失败但不报错
	//   - 留待 A-2 阶段评估：若要修需在 matchField 单值分支加 lo <= n && n <= hi 检查
	//     （与 A-1 双轨对照原则冲突——会改 controlplane 现状行为）
	invalidExprs := []string{
		"",              // 空：5 字段检查 fail
		"* * * *",       // 4 字段：5 字段检查 fail
		"abc * * * *",   // 非数字：atoi 失败
		"*/0 * * * *",   // 步长 0：s <= 0 检查
		"*/-1 * * * *",  // 步长负：s <= 0 检查
		"60-10 * * * *", // 区间左 > 右：a > b 检查
	}
	for _, c := range invalidExprs {
		if _, err := Match(c, time.Now()); err == nil {
			t.Fatalf("Match(%q) 期望错误，但未返回", c)
		}
	}
}
