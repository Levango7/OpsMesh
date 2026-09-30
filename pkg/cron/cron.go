// Package cron 提供轻量 5 字段 cron 表达式求值（分 时 日 月 周），
// 用于内核定时/周期任务调度（F4），避免引入重型第三方 cron 依赖。
// 字段语法：*（任意）、*/step（步长）、a-b（区间）、a,b,c（枚举）、纯数字。
// 周字段 0-7（0=周日，7 等价 0，与 internal/cron 一致）。任意字段非法即返回 error。
//
// 位置历史：原 internal/cron/cron.go（2026-09 A-1 阶段 2 第一批任务期间迁出）。
// 迁出原因：controlplane 与 services/task-svc 的 go workspace 隔离使 internal 路径
// 不可跨模块访问；task-svc 调度循环需要 Match 派生模板任务。
// 迁出最小范围：仅本文件（无 internal 私有依赖）；internal/cron 下的其他文件
// （schedule/manager/dag/sla）保留原位，其 proto 依赖与 controlplane 内部实现强相关。
//
// 与 internal/cron 的一致性（TD-60 §5.11，2026-10-01 收敛）：
// 两份实现曾被登记为「故意差异」，但差异里混着一个真缺陷——本实现把周字段 7 判为
// **永不匹配**（文件头注释却声称 7=周日），于是同一条 "0 3 * * 7" 在控制面会执行、
// 在 task-svc 静默不执行；单值越界（如 "60 * * * *"）也只静默不匹配而不报错。
// 两处均已修（对齐 internal/cron 语义），并新增跨实现守卫测试
// internal/cron/parity_test.go：同一表达式语料在两份实现上必须给出相同结论，
// 差异面由「需手动同步」收敛为「CI 强制一致」。
// 仍保留的一处差异：本实现支持区间/枚举/步长在同一字段内自由组合
// （如 "1,3-10/2,15"），internal/cron 只支持其一——属**语法超集**，不是语义分歧，
// 已在守卫测试里显式断言；彻底合并为单实现待 A-2 阶段评估（需先解决跨模块访问路径）。
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
	// 五个字段**全部求值**后再合并结论：任何字段非法都必须报错，不能因为前面的字段
	// 已不匹配就跳过后面的校验——否则 "0 24 * * *" 只在整点前 1 分钟才报错，
	// 其余时间会被当成「还没到点」（与 internal/cron 的语义一致）。
	m, err := matchField(fields[0], t.Minute(), 0, 59)
	if err != nil {
		return false, err
	}
	h, err := matchField(fields[1], t.Hour(), 0, 23)
	if err != nil {
		return false, err
	}
	d, err := matchField(fields[2], t.Day(), 1, 31)
	if err != nil {
		return false, err
	}
	mo, err := matchField(fields[3], int(t.Month()), 1, 12)
	if err != nil {
		return false, err
	}
	dow := int(t.Weekday()) // 0=周日
	w, err := matchField(fields[4], dow, 0, 7)
	if err != nil {
		return false, err
	}
	// 周日（dow==0）同时匹配字段里的 0 与 7（兼容两种写法，与 internal/cron 语义一致）。
	if dow == 0 && !w {
		if w7, err7 := matchField(fields[4], 7, 0, 7); err7 == nil {
			w = w7
		}
	}
	return m && h && d && mo && w, nil
}

// matchField 判断单字段是否匹配。
//
// 字段语法：
//   - "*"           任意
//   - "a"            纯数字
//   - "a-b"          区间（含端点）
//   - "a-b/step"     区间带步长（步长起点固定为 a）
//   - "*/step"       全区间带步长
//   - "a,b,c"        枚举
//
// 区间 / 步长 / 枚举可组合（如 "1,3-10/2,15"）。
func matchField(field string, val, lo, hi int) (bool, error) {
	for _, part := range strings.Split(field, ",") {
		step := 1
		if i := strings.Index(part, "/"); i >= 0 {
			s, err := strconv.Atoi(part[i+1:])
			if err != nil {
				return false, fmt.Errorf("cron 步长非法 %q: %v", part, err)
			}
			if s <= 0 {
				return false, fmt.Errorf("cron 步长必须 > 0: %q", part)
			}
			step = s
			part = part[:i]
		}
		// 步长前缀的全区间（*/step）
		if part == "*" {
			// lo..hi 步长 step，val 在范围内且 (val - lo) % step == 0
			if val < lo || val > hi {
				return false, nil
			}
			return (val-lo)%step == 0, nil
		}
		// 区间 a-b（可与 step 组合，上面已剥出 step）
		if i := strings.Index(part, "-"); i >= 0 {
			a, err := strconv.Atoi(part[:i])
			if err != nil {
				return false, fmt.Errorf("cron 区间左值非法 %q: %v", part, err)
			}
			b, err := strconv.Atoi(part[i+1:])
			if err != nil {
				return false, fmt.Errorf("cron 区间右值非法 %q: %v", part, err)
			}
			if a < lo || b > hi || a > b {
				return false, fmt.Errorf("cron 区间 %d-%d 越界 [%d,%d]", a, b, lo, hi)
			}
			// 步长起点固定为 a：val ∈ [a, b] 且 (val - a) % step == 0
			if val < a || val > b {
				continue
			}
			return (val-a)%step == 0, nil
		}
		// 单值
		n, err := strconv.Atoi(part)
		if err != nil {
			return false, fmt.Errorf("cron 值非法 %q: %v", part, err)
		}
		// 越界必须报错而非静默不匹配：静默会让 "60 * * * *" 看起来只是「还没到点」，
		// 实际永远到不了点（与 internal/cron 的边界校验一致）。
		if n < lo || n > hi {
			return false, fmt.Errorf("cron 字段越界 %d（应为 %d-%d）: %q", n, lo, hi, part)
		}
		if n == val {
			return true, nil
		}
	}
	return false, nil
}
