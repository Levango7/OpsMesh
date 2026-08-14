// nextrun.go 提供 cron 表达式的"下次执行时间"计算。
//
// NextRun(expr, from) 返回 from 之后第一个匹配 cron 的分钟整点。
// 采用逐分钟扫描法（最多扫 366*24*60=527040 分钟≈1 年），实现简单且足够准确；
// 复杂表达式（含 L/W 等扩展语法）不在本包支持范围。
//
// 用途：
//   - ScheduleEntry.NextRunAt 展示"下次执行时间"；
//   - SLA 监控预估任务时长。
package cron

import (
	"time"
)

// NextRun 返回 from 之后（不含 from 所在分钟）第一个匹配 expr 的分钟整点。
// expr 非法或扫描超过 1 年未命中返回零值（调用方应兜底）。
//
// 注意：本函数对秒级精度不敏感，按分钟对齐（秒归零）。
func NextRun(expr string, from time.Time) time.Time {
	// 从 from 下一分钟整点开始扫描。
	start := from.Truncate(time.Minute).Add(time.Minute)
	// 最多扫描 366 天（闰年）= 527040 分钟。
	limit := start.Add(366 * 24 * time.Hour)
	for t := start; t.Before(limit); t = t.Add(time.Minute) {
		ok, err := Match(expr, t)
		if err != nil {
			return time.Time{}
		}
		if ok {
			return t
		}
	}
	return time.Time{}
}
