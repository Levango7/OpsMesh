// 告警抑制器（Alert Inhibition）。
//
// 本文件实现告警抑制规则：父告警活跃时抑制子告警，避免告警风暴。
// 典型场景：当"主机宕机"告警活跃时，抑制该主机上的所有"服务不可用"告警——
// 因为根因是主机宕机，服务告警是派生的，通知它们只会淹没根因告警。
//
// 与 Silencer 的区别：
//   - Silencer：基于时间窗口的静态抑制（运维主动配置静默规则）。
//   - Inhibitor：基于活跃告警状态的动态抑制（父告警存在时自动抑制子告警）。
//
// 线程安全：rules 与 activeAlerts 经 mu 保护。时钟可注入便于测试。

package alertengine

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"opsmesh/internal/proto"
)

// defaultInhibitTTL 默认活跃告警存活时间。
//
// 超过此时间未刷新的活跃告警将被 Cleanup 清理，避免长期下线设备的告警永久抑制。
// 30 分钟与告警聚合窗口（5 分钟）量级匹配，覆盖多次评估周期。
const defaultInhibitTTL = 30 * time.Minute

// InhibitRule 抑制规则：当 source 告警活跃时，抑制 target 告警。
//
// 语义：当存在活跃告警 P 满足 SourceMatch，且待判定告警 T 满足 TargetMatch，
// 且 P 与 T 在 Equal 列出的所有标签上取值相等时，T 被抑制。
//
// 典型场景：主机宕机告警活跃时，抑制该主机上的服务不可用告警。
//   - SourceMatch = {"metric": "host_status", "severity": "critical"}
//   - TargetMatch = {"metric": "service_status"}
//   - Equal       = ["device_id"]  // 同一主机
//
// 匹配语义：
//   - SourceMatch / TargetMatch：AND 语义，所有键值对需在告警标签中存在且相等。
//   - Equal：parent 与 target 的对应标签值必须相等（缺失视为空串，双方都缺失也算相等）。
type InhibitRule struct {
	Name string // 规则名（如 "host-down-suppress-service-down"）

	// SourceMatch 父告警需满足的标签匹配（AND 语义）。
	SourceMatch map[string]string

	// TargetMatch 子告警需满足的标签匹配（AND 语义）。
	TargetMatch map[string]string

	// Equal 标签键列表：source 和 target 的这些标签值必须相等才抑制。
	Equal []string
}

// AlertInhibitor 告警抑制器。
//
// 持有抑制规则集合与当前活跃告警（按 AlertID 索引）。IsInhibited 遍历所有规则，
// 对每条规则检查待判定告警是否匹配 TargetMatch，若匹配则查找是否有活跃父告警
// 匹配 SourceMatch 且 Equal 标签值相等，找到即抑制。
//
// 线程安全：rules 与 activeAlerts 经 mu 保护。时钟可注入便于测试。
type AlertInhibitor struct {
	rules []InhibitRule
	mu    sync.RWMutex

	// activeAlerts 记录当前活跃的告警（用于抑制判定），key = AlertID。
	// 存储的是告警的拷贝，隔离外部修改。
	activeAlerts map[string]*proto.Alert

	// trackedAt 记录告警被 TrackActive 的时间，用于 Cleanup 过期清理，key = AlertID。
	trackedAt map[string]time.Time

	// ttl 活跃告警的存活时间；超过后 Cleanup 清理。
	ttl time.Duration

	// now 可注入时钟，nil 时使用 time.Now。
	now func() time.Time
}

// NewAlertInhibitor 构造抑制器，使用默认 TTL（30 分钟）与 time.Now。
//
// rules 会被深拷贝，隔离外部对规则 map/slice 的修改。
func NewAlertInhibitor(rules []InhibitRule) *AlertInhibitor {
	return newAlertInhibitor(rules, defaultInhibitTTL, time.Now)
}

// newAlertInhibitor 内部构造，可注入 TTL 与时钟（测试用）。
//
//   - ttl<=0 时使用 defaultInhibitTTL。
//   - now 为 nil 时使用 time.Now。
func newAlertInhibitor(rules []InhibitRule, ttl time.Duration, now func() time.Time) *AlertInhibitor {
	if ttl <= 0 {
		ttl = defaultInhibitTTL
	}
	if now == nil {
		now = time.Now
	}
	cp := make([]InhibitRule, len(rules))
	for i, r := range rules {
		cp[i] = cloneInhibitRule(r)
	}
	return &AlertInhibitor{
		rules:        cp,
		activeAlerts: make(map[string]*proto.Alert),
		trackedAt:    make(map[string]time.Time),
		ttl:          ttl,
		now:          now,
	}
}

// cloneInhibitRule 深拷贝抑制规则，隔离外部对 SourceMatch/TargetMatch/Equal 的修改。
func cloneInhibitRule(r InhibitRule) InhibitRule {
	cp := r
	if r.SourceMatch != nil {
		cp.SourceMatch = make(map[string]string, len(r.SourceMatch))
		for k, v := range r.SourceMatch {
			cp.SourceMatch[k] = v
		}
	}
	if r.TargetMatch != nil {
		cp.TargetMatch = make(map[string]string, len(r.TargetMatch))
		for k, v := range r.TargetMatch {
			cp.TargetMatch[k] = v
		}
	}
	if r.Equal != nil {
		cp.Equal = make([]string, len(r.Equal))
		copy(cp.Equal, r.Equal)
	}
	return cp
}

// alertLabels 从 proto.Alert 构建标签 map，用于规则匹配。
//
// 标签键约定：
//   - metric：指标名（如 host_status / service_status）
//   - device_id：设备 ID
//   - severity：严重度（critical / warning / info）
//   - status：告警状态（firing / acknowledged / silenced）
func alertLabels(a *proto.Alert) map[string]string {
	return map[string]string{
		"metric":    a.Metric,
		"device_id": a.DeviceID,
		"severity":  a.Severity,
		"status":    a.Status,
	}
}

// IsInhibited 判断告警是否应被抑制。
//
// 遍历所有规则，对每条规则：
//  1. 检查 alert 是否匹配 TargetMatch；不匹配跳过该规则。
//  2. 遍历活跃告警，查找是否有父告警匹配 SourceMatch 且 Equal 标签值相等。
//  3. 找到匹配的父告警即返回 true（抑制）。
//
// 无匹配则返回 false（不抑制）。alert 为 nil 直接返回 false（防御式）。
func (in *AlertInhibitor) IsInhibited(alert *proto.Alert) bool {
	if alert == nil {
		return false
	}
	targetLabels := alertLabels(alert)
	in.mu.RLock()
	defer in.mu.RUnlock()
	for _, rule := range in.rules {
		// 子告警需匹配 TargetMatch
		if !labelsMatch(targetLabels, rule.TargetMatch) {
			continue
		}
		// 查找匹配的活跃父告警
		for _, parent := range in.activeAlerts {
			if parent == nil {
				continue
			}
			parentLabels := alertLabels(parent)
			if !labelsMatch(parentLabels, rule.SourceMatch) {
				continue
			}
			if !equalLabelsMatch(parentLabels, targetLabels, rule.Equal) {
				continue
			}
			return true
		}
	}
	return false
}

// equalLabelsMatch 检查 parent 与 target 在 equal 列出的所有标签上取值是否相等。
//
// equal 为空表示无相等约束，返回 true。
// 标签缺失视为空串比较（即双方都缺失也视为相等）。
func equalLabelsMatch(parent, target map[string]string, equal []string) bool {
	for _, k := range equal {
		if parent[k] != target[k] {
			return false
		}
	}
	return true
}

// TrackActive 记录活跃告警（告警评估后调用）。
//
// alert 为 nil 或 AlertID 为空时跳过（防御式）。重复 TrackActive 同一 AlertID
// 会刷新 trackedAt（滑动窗口语义），延长其存活时间，避免持续活跃的告警被 Cleanup 误清。
func (in *AlertInhibitor) TrackActive(alert *proto.Alert) {
	if alert == nil || alert.AlertID == "" {
		return
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	// 拷贝一份存入，隔离外部修改
	cp := *alert
	in.activeAlerts[alert.AlertID] = &cp
	in.trackedAt[alert.AlertID] = in.now()
}

// RemoveActive 移除已恢复的告警。
//
// alertID 不存在时静默忽略（幂等）。告警恢复（status 从 firing 变为 resolved）
// 时调用，使子告警不再被该父告警抑制。
func (in *AlertInhibitor) RemoveActive(alertID string) {
	if alertID == "" {
		return
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	delete(in.activeAlerts, alertID)
	delete(in.trackedAt, alertID)
}

// Cleanup 清理过期的活跃告警（超过 TTL 未刷新）。
//
// 返回清理的条目数（调试/监控用）。周期调用防止内存泄漏（如每分钟一次）。
// 注意：清理后对应的父告警不再抑制子告警。
func (in *AlertInhibitor) Cleanup() int {
	now := in.now()
	in.mu.Lock()
	defer in.mu.Unlock()
	count := 0
	for id, tracked := range in.trackedAt {
		if now.Sub(tracked) > in.ttl {
			delete(in.activeAlerts, id)
			delete(in.trackedAt, id)
			count++
		}
	}
	return count
}

// ActiveCount 返回当前活跃告警数（含已过期但未 Cleanup 清理的；调试/监控用）。
func (in *AlertInhibitor) ActiveCount() int {
	in.mu.RLock()
	defer in.mu.RUnlock()
	return len(in.activeAlerts)
}

// ============================================================================
// 抑制规则加载
// ============================================================================

// inhibitRuleJSON JSON 中间结构（snake_case 字段名），用于反序列化后转换为 InhibitRule。
//
// InhibitRule 自身使用 Go 风格字段名（Name/SourceMatch/TargetMatch/Equal），
// 而 JSON 配置文件使用 snake_case（name/source_match/target_match/equal），
// 通过此中间结构做字段名映射，避免在 InhibitRule 上加 json tag 污染主结构。
type inhibitRuleJSON struct {
	Name        string            `json:"name"`
	SourceMatch map[string]string `json:"source_match"`
	TargetMatch map[string]string `json:"target_match"`
	Equal       []string          `json:"equal"`
}

// LoadInhibitRules 从 JSON 文件加载抑制规则。
//
// 文件格式（snake_case，顶层为 JSON 数组）：
//
//	[
//	  {
//	    "name": "host-down-suppress-service-down",
//	    "source_match": {"metric": "host_status", "severity": "critical"},
//	    "target_match": {"metric": "service_status"},
//	    "equal": ["device_id"]
//	  }
//	]
//
// 语义：
//   - path 为空返回 nil, nil（不视为错误，调用方据此跳过抑制初始化）。
//   - 文件不存在或不可读返回 error（包装读错误）。
//   - JSON 解析失败返回 error（包装解析错误）。
//   - 空数组（[]）返回空切片 + nil（不视为错误，仅表示无抑制规则）。
//   - 顶层非数组（如 {} 或单对象）返回 error。
//
// 返回的规则切片为独立拷贝，调用方修改不影响内部解析结果。
func LoadInhibitRules(path string) ([]InhibitRule, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取抑制规则文件 %q 失败: %w", path, err)
	}
	var raws []inhibitRuleJSON
	if err := json.Unmarshal(data, &raws); err != nil {
		return nil, fmt.Errorf("解析抑制规则文件 %q 失败: %w", path, err)
	}
	rules := make([]InhibitRule, len(raws))
	for i, r := range raws {
		rules[i] = InhibitRule{
			Name:        r.Name,
			SourceMatch: r.SourceMatch,
			TargetMatch: r.TargetMatch,
			Equal:       r.Equal,
		}
	}
	return rules, nil
}