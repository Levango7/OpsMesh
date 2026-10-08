// slo_eval.go 是 SLI 求值的**单一事实源**（TD-61 起位于 model 中性层）：支持的指标名、别名映射与判定入口。
//
// 为什么要单独一个文件（2026-10-03 的 P0 复盘）：内存后端与 SQL 后端各写了一份 SLI 状态，
// 内存那份把 CurrentValue 硬编码 99.5、Status 硬编码 "met"（注释自认"MVP 模拟值"），
// 而 /api/v1/slos/{id}/status 是对外承诺的 SLA 口径 ⇒ 客户拿到的是恒达标报告。
// 同时 metric 支持集只在 SQL 侧的 metricColumn 里定义，内存侧无从判断"这个指标我根本没有数据"，
// 于是文档示例里的 `metric: "up"` 这类写法会永远停在假状态而不是报错。
//
// 规则：
//   - 指标不在支持集内 ⇒ 创建/更新时拒绝（见 SupportedSLIMetrics 与 controlplane 的校验），
//     运行期若遇到历史遗留数据 ⇒ nodata，绝不返回编造值；
//   - 没有观测样本 ⇒ nodata（Current=-1），与"算出来是 0"严格区分。
package model

import "sort"

// sliMetricFields 把 SLI 的 metric 别名映射到内部规范键（同时也是 network_metrics 的列名）。
var sliMetricFields = map[string]string{
	"cpu_usage":    "cpu_usage",
	"cpu":          "cpu_usage",
	"memory_usage": "memory_usage",
	"mem_usage":    "memory_usage",
	"memory":       "memory_usage",
	"temperature":  "temperature",
	"temp":         "temperature",
	"uptime":       "uptime",
}

// metricFieldFor 返回 metric 的规范键；不支持时返回 ""。
func MetricFieldFor(metricName string) string { return sliMetricFields[metricName] }

// SupportedSLIMetrics 返回可创建的 SLI 指标名（供 API 校验与文档对齐）。
//
// 注意 uptime 只有 SQL 侧有列，内存后端没有相应聚合 ⇒ 内存形态下它会得到 nodata；
// 这仍然是诚实的（不是假值），但值得在文档里写明。
func SupportedSLIMetrics() []string {
	out := make([]string, 0, len(sliMetricFields))
	for k := range sliMetricFields {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// IsValidSLIMetric 判断 SLI 是否引用了有真实数据来源的指标（供 API 创建/更新校验）。
func IsValidSLIMetric(metric string) bool { return MetricFieldFor(metric) != "" }

// EvaluateSLI 比较当前值与目标值，返回 met/breached/nodata（current<0 视为无样本）。
//
// 原定义在 sql_slo.go（SQL 侧）但内存后端的 SLIStatus 也调用它——TD-61 与
// 指标映射表一并上提中性层，保证两后端同一套判定（此前的教训见本文件头注释）。
func EvaluateSLI(current, target float64, operator string) string {
	if current < 0 {
		return "nodata"
	}
	switch operator {
	case ">=":
		if current >= target {
			return "met"
		}
	case ">":
		if current > target {
			return "met"
		}
	case "<=":
		if current <= target {
			return "met"
		}
	case "<":
		if current < target {
			return "met"
		}
	case "==":
		if current == target {
			return "met"
		}
	default:
		// 默认 >=
		if current >= target {
			return "met"
		}
	}
	return "breached"
}
