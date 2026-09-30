package cron

import (
	"testing"
	"time"

	pkgcron "github.com/Levango7/OpsMesh/pkg/cron"
)

// TestParity_SharedGrammar 跨实现一致性守卫（TD-60 §5.11，2026-10-01）。
//
// 背景：internal/cron 与 pkg/cron 是同一份 5 字段求值器的两份副本（后者为
// services/task-svc 跨 go workspace 的迁出副本）。两份曾被登记为「故意差异、需手动同步」，
// 但 2026-10-01 复核发现差异里混着**真缺陷**：周字段 7 在 pkg 侧永不匹配、单值越界在
// pkg 侧静默不匹配——于是同一条 "0 3 * * 7" 在控制面（internal/cron）会执行，
// 在 task-svc（pkg/cron）静默不执行，两处给出相反答案。
//
// 本测试把「必须一致」从注释承诺变成 CI 事实：共同语法面上的每条表达式，
// 两份实现的结果（匹配与否）与是否报错都必须相同。任何一侧今后单独改动都会在此变红。
func TestParity_SharedGrammar(t *testing.T) {
	exprs := []string{
		"* * * * *",
		"30 10 26 7 *",
		"0 3 * * 7",    // 历史缺陷表达式：周 7
		"0 3 * * 0",    // 同一时刻的等价写法
		"0 3 * * 0,7",  // 枚举里两种写法共存
		"0 3 * * 5-7",  // 区间端点含 7
		"30 10 26 7 7", // 周 7 精确匹配
		"30 10 26 7 1", // 周一
		"*/15 * * * *",
		"0-30/10 * * * *",
		"0 9-17 * * 1-5",
		"0 9,12,15 * * *",
		"0 0 1 * *",
		"59 23 31 12 *",
	}
	times := []time.Time{
		time.Date(2026, 7, 26, 10, 30, 0, 0, time.UTC),  // 周日（既有用例钉住的日期）
		time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC),   // 周一
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),     // 分/时零点
		time.Date(2026, 12, 31, 23, 59, 0, 0, time.UTC), // 年月日时分全边界
		time.Date(2026, 3, 1, 3, 0, 0, 0, time.UTC),
		time.Date(2025, 2, 28, 23, 0, 0, 0, time.UTC),
	}

	agreed := 0
	for _, expr := range exprs {
		for _, tm := range times {
			gotI, errI := Match(expr, tm)
			gotP, errP := pkgcron.Match(expr, tm)
			if (errI == nil) != (errP == nil) {
				t.Errorf("表达式 %q 在 %s：错误与否不一致（internal err=%v，pkg err=%v）",
					expr, tm.Format(time.RFC3339), errI, errP)
				continue
			}
			if errI == nil && gotI != gotP {
				t.Errorf("表达式 %q 在 %s：两份实现结论相反（internal=%v，pkg=%v）",
					expr, tm.Format(time.RFC3339), gotI, gotP)
				continue
			}
			agreed++
		}
	}
	// 语料 × 时刻全部走完才算守卫有效（防止将来有人把语料清空后测试变「绿」）。
	if want := len(exprs) * len(times); agreed != want {
		t.Errorf("一致性比对覆盖不足：比对成功 %d 组，期望 %d 组", agreed, want)
	}
}

// TestParity_InvalidAligned 非法表达式两侧都必须报错——静默不匹配正是历史缺陷的形态。
func TestParity_InvalidAligned(t *testing.T) {
	invalid := []string{
		"", "* * * *", "abc * * * *", "*/0 * * * *", "*/-1 * * * *",
		"60-10 * * * *", // 区间左 > 右
		"60 * * * *",    // 分越界
		"0 24 * * *",    // 时越界
		"0 0 32 * *",    // 日越界
		"0 0 * 13 *",    // 月越界
		"0 0 * * 8",     // 周越界（7 合法，8 不合法）
	}
	now := time.Date(2026, 7, 26, 10, 30, 0, 0, time.UTC)
	for _, expr := range invalid {
		if _, err := Match(expr, now); err == nil {
			t.Errorf("internal/cron 对 %q 未报错（应拒绝）", expr)
		}
		if _, err := pkgcron.Match(expr, now); err == nil {
			t.Errorf("pkg/cron 对 %q 未报错（应拒绝）——静默不匹配即历史缺陷", expr)
		}
	}
}

// TestParity_SundaySevenIsSunday 钉住修复前的具体缺陷，避免任何一侧回退。
func TestParity_SundaySevenIsSunday(t *testing.T) {
	sunday := time.Date(2026, 7, 26, 3, 0, 0, 0, time.UTC)
	if sunday.Weekday() != time.Sunday {
		t.Fatalf("测试前置失效：%s 不是周日（实际 %s）", sunday.Format("2006-01-02"), sunday.Weekday())
	}
	impls := []struct {
		name  string
		match func(string, time.Time) (bool, error)
	}{
		{"internal/cron", Match},
		{"pkg/cron", pkgcron.Match},
	}
	for _, expr := range []string{"0 3 * * 7", "0 3 * * 0", "0 3 * * 0,7", "0 3 * * 5-7"} {
		for _, impl := range impls {
			got, err := impl.match(expr, sunday)
			if err != nil {
				t.Errorf("%s: Match(%q) 报错: %v", impl.name, expr, err)
				continue
			}
			if !got {
				t.Errorf("%s: Match(%q) 在周日应为 true（周 7=周日）", impl.name, expr)
			}
		}
	}
}

// TestParity_PKGGrammarSuperset 显式断言「有意保留」的差异面，防止把它误当回归。
//
// pkg/cron 允许区间/枚举/步长在同一字段内自由组合（分隔符粒度不同），internal/cron 只支持其一。
// 属**语法超集**而非语义分歧：两者在共同语言上结论一致（TestParity_SharedGrammar 已证）。
// 若将来收敛掉这一条（internal 也支持组合语法），请同时更新本测试与 TD-60 §5.11。
func TestParity_PKGGrammarSuperset(t *testing.T) {
	now := time.Date(2026, 7, 27, 15, 0, 0, 0, time.UTC) // 周一 15:00
	for _, expr := range []string{"1,3-10/2,15 * * * *", "*,5 * * * *"} {
		if _, err := Match(expr, now); err == nil {
			t.Errorf("internal/cron 对 %q 不再报错——若这是有意收敛，请同步更新本测试与 §5.11", expr)
		}
		if _, err := pkgcron.Match(expr, now); err != nil {
			t.Errorf("pkg/cron 对 %q 应接受（语法超集），却报错: %v", expr, err)
		}
	}
}
