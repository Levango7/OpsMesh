package alertengine

// Package alertengine 实现自包含的告警规则引擎，支持多条件组合（AND/OR/NOT）、
// 阈值窗口（duration）、持续时长评估、规则 CRUD 与规则匹配。
//
// 设计目标：
//   - 自包含：不依赖外部 store/server，可被控制面或 agent 复用。
//   - 线程安全：Engine/Silencer/Evaluator 内部以 sync.RWMutex 保护共享状态。
//   - 可注入时钟与指标提供者，便于单元测试。
//
// 评估流程：
//
//	Engine.Evaluate(deviceID) 遍历启用规则 → MatchRule 逐条件比对 →
//	Evaluator.ShouldFire 判断持续时长 → 触发 AlertEvent → 可选 Silencer 抑制 / Aggregator 聚合。

import (
	"errors"
	"time"
)

// LogicOp 条件之间的逻辑组合算子。
//
// 取值：
//   - LogicAnd：所有条件均满足才命中。
//   - LogicOr：任一条件满足即命中。
//   - LogicNot：所有条件均不满足才命中（即对每个条件取反后做 AND）。
//
// 空值或未知值按 LogicAnd 处理（最严格语义）。
type LogicOp string

const (
	LogicAnd LogicOp = "AND" // 全部满足
	LogicOr  LogicOp = "OR"  // 任一满足
	LogicNot LogicOp = "NOT" // 全部不满足（取反 AND）
)

// Severity 等级常量。
const (
	SeverityCritical = "critical" // 严重
	SeverityWarning  = "warning"  // 警告
	SeverityInfo     = "info"     // 提示
)

// 支持的比较算子。
const (
	OpGT = ">"  // 大于
	OpLT = "<"  // 小于
	OpGE = ">=" // 大于等于
	OpLE = "<=" // 小于等于
	OpEQ = "==" // 等于
	OpNE = "!=" // 不等于
)

// ErrRuleNotFound 规则不存在时返回。
var ErrRuleNotFound = errors.New("alert rule not found")

// ErrRuleInvalid 规则字段非法时返回（如 ID 空、条件空、算子不支持）。
var ErrRuleInvalid = errors.New("alert rule invalid")

// Condition 单个阈值条件。
//
// 语义：对设备指标 Metric 在时间窗口 Window 内的聚合值（通常为平均值）
// 应用 Operator 与 Threshold 比较。Window<=0 表示取即时值（窗口退化为 0）。
type Condition struct {
	Metric    string        // 指标名（cpu_usage/mem_usage/disk_usage/net_in/net_out 等）
	Operator  string        // 比较算子：>/</>=/<=/==/!=
	Threshold float64       // 阈值
	Window    time.Duration // 采样窗口（如最近 5 分钟平均值）；<=0 表示即时值
}

// AlertRule 一条告警规则。
//
// 多个 Condition 按 Logic 组合；当组合结果持续满足 Duration 时间，
// 引擎对该设备产出一条 AlertEvent。Duration<=0 表示立即触发（无持续时长要求）。
type AlertRule struct {
	ID             string        // 规则唯一 ID（租户内唯一）
	Name           string        // 规则名（展示用）
	TenantID       string        // 所属租户
	Enabled        bool          // 是否启用
	Conditions     []Condition   // 条件列表（至少 1 条）
	Logic          LogicOp       // 条件组合算子（AND/OR/NOT）
	Duration       time.Duration // 持续时长（条件需持续满足多久才触发）；<=0 立即触发
	Severity       string        // critical/warning/info
	NotifyChannels []string      // 通知渠道 ID 列表
	SilenceID      string        // 关联的静默规则 ID（可选）
	CreatedAt      time.Time     // 创建时间
	UpdatedAt      time.Time     // 最近更新时间
}

// AlertEvent 规则触发产出的告警事件。
//
// 本包自定义事件结构（不修改 proto.Alert），供聚合/静默/通知链消费。
// Labels 用于 Aggregator 分组与 Silencer 标签匹配。
type AlertEvent struct {
	RuleID   string             // 触发规则 ID
	TenantID string             // 租户
	DeviceID string             // 设备 ID
	Severity string             // 严重度
	Message  string             // 人读消息
	Labels   map[string]string  // 标签（默认含 ruleID/deviceID/severity）
	FiredAt  time.Time          // 触发时刻
	Values   map[string]float64 // 各 Condition.Metric 实际值（便于排查）
}

// AlertGroup 聚合后的告警分组。
type AlertGroup struct {
	Key    string        // 分组键拼接（如 "deviceID=d1|severity=critical"）
	Events []*AlertEvent // 组内事件
}

// Validate 校验规则字段合法性。返回 ErrRuleInvalid 子类错误。
//
// 检查项：
//   - ID / TenantID 非空
//   - Conditions 至少 1 条
//   - 每个 Condition 的 Metric 非空、Operator 受支持
//   - Logic 空值规范化为 LogicAnd
func (r *AlertRule) Validate() error {
	if r.ID == "" {
		return ErrRuleInvalid
	}
	if r.TenantID == "" {
		return ErrRuleInvalid
	}
	if len(r.Conditions) == 0 {
		return ErrRuleInvalid
	}
	for i, c := range r.Conditions {
		if c.Metric == "" {
			return ErrRuleInvalid
		}
		if !isSupportedOp(c.Operator) {
			return ErrRuleInvalid
		}
		_ = i
	}
	if r.Logic == "" {
		r.Logic = LogicAnd
	}
	if r.Severity == "" {
		r.Severity = SeverityWarning
	}
	return nil
}

// isSupportedOp 判断算子是否受支持。
func isSupportedOp(op string) bool {
	switch op {
	case OpGT, OpLT, OpGE, OpLE, OpEQ, OpNE:
		return true
	}
	return false
}

// compare 按 op 比较 actual 与 threshold，返回是否满足条件。
//
// 未知 op 返回 false（防御式，Validate 已拦截）。
func compare(op string, actual, threshold float64) bool {
	switch op {
	case OpGT:
		return actual > threshold
	case OpLT:
		return actual < threshold
	case OpGE:
		return actual >= threshold
	case OpLE:
		return actual <= threshold
	case OpEQ:
		return actual == threshold
	case OpNE:
		return actual != threshold
	}
	return false
}

// combine 按 LogicOp 组合条件布尔结果切片。
//
//   - LogicAnd：全部为 true。
//   - LogicOr：任一为 true。
//   - LogicNot：全部为 false（即对每个条件取反后 AND）。
//   - 未知 / 空：按 LogicAnd。
//
// 空 results 返回 false（无条件的规则不应通过 Validate，此处防御式返回 false）。
func combine(logic LogicOp, results []bool) bool {
	if len(results) == 0 {
		return false
	}
	switch logic {
	case LogicOr:
		for _, r := range results {
			if r {
				return true
			}
		}
		return false
	case LogicNot:
		for _, r := range results {
			if r {
				return false
			}
		}
		return true
	default: // LogicAnd 及未知
		for _, r := range results {
			if !r {
				return false
			}
		}
		return true
	}
}
