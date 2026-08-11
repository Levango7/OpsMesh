package alertengine

import (
	"sort"
	"strings"
)

// 默认每组最大告警数（<=0 时使用此默认值）。
const defaultMaxGroup = 100

// Aggregator 告警聚合器。
//
// 按 groupBy 指定的标签字段对事件分组，每组最多保留 maxGroup 条。
// groupBy 字段从 AlertEvent.Labels 取值；常见字段：deviceID/severity/ruleID/tenantID。
//
// 用途：避免单次评估产出过多相似告警淹没通知渠道；按设备/严重度等维度归并展示。
type Aggregator struct {
	groupBy  []string
	maxGroup int
}

// NewAggregator 构造聚合器。
//
//   - groupBy：分组字段名列表（如 []string{"deviceID","severity"}）；空表示单组全聚合。
//   - maxGroup：每组最大事件数，<=0 时使用 defaultMaxGroup。
func NewAggregator(groupBy []string, maxGroup int) *Aggregator {
	if maxGroup <= 0 {
		maxGroup = defaultMaxGroup
	}
	// 拷贝 groupBy 隔离外部修改
	gb := make([]string, len(groupBy))
	copy(gb, groupBy)
	return &Aggregator{groupBy: gb, maxGroup: maxGroup}
}

// groupKey 拼接事件在 groupBy 字段上的分组键。
//
// 格式："field1=v1|field2=v2"；字段在 Labels 缺失时取空串。
func (a *Aggregator) groupKey(ev *AlertEvent) string {
	if len(a.groupBy) == 0 {
		return ""
	}
	parts := make([]string, 0, len(a.groupBy))
	for _, f := range a.groupBy {
		parts = append(parts, f+"="+ev.Labels[f])
	}
	return strings.Join(parts, "|")
}

// Aggregate 对事件列表分组聚合，返回分组列表。
//
// 返回的 AlertGroup 列表按 Key 升序；组内事件按原顺序保留前 maxGroup 条。
// 输入为 nil/空时返回空切片（非 nil）。
func (a *Aggregator) Aggregate(events []*AlertEvent) []*AlertGroup {
	groups := make(map[string][]*AlertEvent)
	order := make([]string, 0) // 首次出现顺序，便于稳定输出

	for _, ev := range events {
		if ev == nil {
			continue
		}
		key := a.groupKey(ev)
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], ev)
	}

	out := make([]*AlertGroup, 0, len(order))
	for _, key := range order {
		evs := groups[key]
		if len(evs) > a.maxGroup {
			evs = evs[:a.maxGroup]
		}
		out = append(out, &AlertGroup{Key: key, Events: evs})
	}
	// 按 Key 升序，便于调用方稳定处理
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
