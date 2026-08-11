package alertengine


import (
	"errors"
	"sync"
	"time"
)

// ErrSilenceNotFound 静默规则不存在时返回。
var ErrSilenceNotFound = errors.New("silence rule not found")

// ErrSilenceInvalid 静默规则字段非法时返回。
var ErrSilenceInvalid = errors.New("silence rule invalid")

// SilenceRule 静默/抑制规则。
//
// 在 [StartAt, EndAt] 时间窗口内，对 Labels 匹配 MatchLabels 的事件进行抑制
// （IsSilenced 返回 true）。MatchLabels 中每个键值对都需在事件 Labels 中存在且相等
// （AND 语义）；空 MatchLabels 表示匹配该租户下所有事件。
type SilenceRule struct {
	ID          string            // 静默规则 ID
	TenantID    string            // 所属租户
	MatchLabels map[string]string // 匹配标签（AND 语义）
	StartAt     time.Time         // 静默开始时间
	EndAt       time.Time         // 静默结束时间
	CreatedBy   string            // 创建人
	Reason      string            // 静默原因
}

// Validate 校验静默规则字段。
//   - ID / TenantID 非空
//   - EndAt 不早于 StartAt（零值允许：均为零时表示"立即生效到永久"）
func (s *SilenceRule) Validate() error {
	if s.ID == "" {
		return ErrSilenceInvalid
	}
	if s.TenantID == "" {
		return ErrSilenceInvalid
	}
	if !s.StartAt.IsZero() && !s.EndAt.IsZero() && s.EndAt.Before(s.StartAt) {
		return ErrSilenceInvalid
	}
	return nil
}

// Silencer 静默/抑制器。
//
// 持有 SilenceRule 集合，按 ID 索引。IsSilenced 遍历所有规则，
// 任一规则匹配且在时间窗口内即返回 true（抑制）。
//
// 线程安全：rules 经 mu 保护。时钟可注入便于测试。
type Silencer struct {
	mu    sync.RWMutex
	rules map[string]*SilenceRule
	now   func() time.Time
}

// NewSilencer 构造静默器。now 为 nil 时使用 time.Now。
func NewSilencer(now func() time.Time) *Silencer {
	if now == nil {
		now = time.Now
	}
	return &Silencer{
		rules: make(map[string]*SilenceRule),
		now:   now,
	}
}

// AddRule 新增静默规则。ID 已存在返回 ErrSilenceInvalid。
func (s *Silencer) AddRule(rule *SilenceRule) error {
	if rule == nil {
		return ErrSilenceInvalid
	}
	if err := rule.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.rules[rule.ID]; exists {
		return ErrSilenceInvalid
	}
	cp := *rule
	// 深拷贝 MatchLabels 隔离外部修改
	if rule.MatchLabels != nil {
		cp.MatchLabels = make(map[string]string, len(rule.MatchLabels))
		for k, v := range rule.MatchLabels {
			cp.MatchLabels[k] = v
		}
	}
	s.rules[rule.ID] = &cp
	return nil
}

// UpdateRule 更新静默规则。不存在返回 ErrSilenceNotFound。
func (s *Silencer) UpdateRule(rule *SilenceRule) error {
	if rule == nil {
		return ErrSilenceInvalid
	}
	if err := rule.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.rules[rule.ID]; !exists {
		return ErrSilenceNotFound
	}
	cp := *rule
	if rule.MatchLabels != nil {
		cp.MatchLabels = make(map[string]string, len(rule.MatchLabels))
		for k, v := range rule.MatchLabels {
			cp.MatchLabels[k] = v
		}
	}
	s.rules[rule.ID] = &cp
	return nil
}

// DeleteRule 删除静默规则。不存在返回 ErrSilenceNotFound。
func (s *Silencer) DeleteRule(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.rules[id]; !exists {
		return ErrSilenceNotFound
	}
	delete(s.rules, id)
	return nil
}

// GetRule 返回静默规则的拷贝。
func (s *Silencer) GetRule(id string) (*SilenceRule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, exists := s.rules[id]
	if !exists {
		return nil, ErrSilenceNotFound
	}
	return cloneSilence(r), nil
}

// ListRules 返回所有静默规则的拷贝，按 ID 升序。
func (s *Silencer) ListRules() []*SilenceRule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*SilenceRule, 0, len(s.rules))
	for _, r := range s.rules {
		out = append(out, cloneSilence(r))
	}
	return out
}

// cloneSilence 深拷贝静默规则。
func cloneSilence(r *SilenceRule) *SilenceRule {
	cp := *r
	if r.MatchLabels != nil {
		cp.MatchLabels = make(map[string]string, len(r.MatchLabels))
		for k, v := range r.MatchLabels {
			cp.MatchLabels[k] = v
		}
	}
	return &cp
}

// IsSilenced 判断事件是否被任一静默规则抑制。
//
// 匹配条件（AND）：
//  1. 规则 TenantID 与事件 TenantID 一致。
//  2. 当前时间在 [StartAt, EndAt] 内（零值边界：StartAt 零表示不限下界，EndAt 零表示不限上界）。
//  3. 规则 MatchLabels 中每个键值对在事件 Labels 中存在且相等。
//
// 任一规则匹配即返回 true；无匹配返回 false。
// 事件为 nil 直接返回 false（防御式）。
func (s *Silencer) IsSilenced(event *AlertEvent) bool {
	if event == nil {
		return false
	}
	now := s.now()
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.rules {
		if r.TenantID != event.TenantID {
			continue
		}
		if !r.StartAt.IsZero() && now.Before(r.StartAt) {
			continue
		}
		if !r.EndAt.IsZero() && now.After(r.EndAt) {
			continue
		}
		if !labelsMatch(event.Labels, r.MatchLabels) {
			continue
		}
		return true
	}
	return false
}

// labelsMatch 判断 event labels 是否包含 match 中所有键值对（AND 语义）。
//
// match 为空表示无标签约束，返回 true。
func labelsMatch(eventLabels, match map[string]string) bool {
	for k, v := range match {
		if eventLabels[k] != v {
			return false
		}
	}
	return true
}