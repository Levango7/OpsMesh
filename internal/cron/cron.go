// Package cron 提供轻量 5 字段 cron 表达式求值（分 时 日 月 周），
// 用于内核定时/周期任务调度（F4），避免引入重型第三方 cron 依赖。
// 字段语法：*（任意）、*/step（步长）、a-b（区间）、a,b,c（枚举）、纯数字。
// 周字段 0-6（0=周日），7 也按周日处理。任意字段非法即返回 error。
package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Match 判断 5 字段 cron 表达式是否匹配给定时间 t。
func Match(expr string, t time.Time) (bool, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return false, fmt.Errorf("cron 需 5 字段，得到 %d: %q", len(fields), expr)
	}
	m, err := matchField(fields[0], t.Minute(), 0, 59)
	if err != nil {
		return false, err
	}
	h, err := matchField(fields[1], t.Hour(), 0, 23)
	if err != nil {
		return false, err
	}
	dom, err := matchField(fields[2], t.Day(), 1, 31)
	if err != nil {
		return false, err
	}
	mon, err := matchField(fields[3], int(t.Month()), 1, 12)
	if err != nil {
		return false, err
	}
	dow := int(t.Weekday()) // 0=周日
	d1, err := matchField(fields[4], dow, 0, 7)
	if err != nil {
		return false, err
	}
	// 周日（dow==0）同时匹配字段里的 0 与 7（兼容两种写法）。
	if dow == 0 && !d1 {
		if d2, err2 := matchField(fields[4], 7, 0, 7); err2 == nil {
			d1 = d2
		}
	}
	return m && h && dom && mon && d1, nil
}

// matchField 判断单字段是否匹配 val（[lo,hi] 为合法取值范围）。
// 越界或非法字段一律返回 error（避免静默误调度）。
func matchField(field string, val, lo, hi int) (bool, error) {
	if field == "" {
		return false, fmt.Errorf("空字段")
	}
	if field == "*" {
		return true, nil
	}
	// 步长：*/n 或 a-b/n
	if strings.Contains(field, "/") {
		parts := strings.SplitN(field, "/", 2)
		step, err := strconv.Atoi(parts[1])
		if err != nil || step <= 0 {
			return false, fmt.Errorf("非法步长 %q", field)
		}
		baseLo, baseHi := lo, hi
		if parts[0] != "*" {
			rng := strings.SplitN(parts[0], "-", 2)
			if len(rng) != 2 {
				return false, fmt.Errorf("非法步长前缀 %q", field)
			}
			if baseLo, err = strconv.Atoi(rng[0]); err != nil {
				return false, fmt.Errorf("非法区间起点 %q", field)
			}
			if baseHi, err = strconv.Atoi(rng[1]); err != nil {
				return false, fmt.Errorf("非法区间终点 %q", field)
			}
		}
		if val < baseLo || val > baseHi {
			return false, nil
		}
		return (val-baseLo)%step == 0, nil
	}
	// 枚举 / 区间：a,b,c 或 a-b
	for _, tok := range strings.Split(field, ",") {
		if rng := strings.SplitN(tok, "-", 2); len(rng) == 2 {
			a, err1 := strconv.Atoi(rng[0])
			b, err2 := strconv.Atoi(rng[1])
			if err1 != nil || err2 != nil {
				return false, fmt.Errorf("非法区间 %q", tok)
			}
			if a < lo || a > hi || b < lo || b > hi {
				return false, fmt.Errorf("区间越界 %q（应为 %d-%d）", tok, lo, hi)
			}
			if val >= a && val <= b {
				return true, nil
			}
			continue
		}
		n, err := strconv.Atoi(tok)
		if err != nil {
			return false, fmt.Errorf("非法字段 %q", tok)
		}
		if n < lo || n > hi {
			return false, fmt.Errorf("字段越界 %q（应为 %d-%d）", tok, lo, hi)
		}
		if n == val {
			return true, nil
		}
	}
	return false, nil
}
